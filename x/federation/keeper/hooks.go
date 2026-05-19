package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
)

// FederationServiceHooks implements x/service's ServiceHooks interface.
// Phase 5 of the federation→service migration plan. Each callback is
// wrapped in a defer-recover so a panic here never rolls back the
// service-side transaction that triggered the hook — bridge slashing
// must remain unblockable even if federation has a bug. The orphan-
// binding invariant + MsgPruneOrphanBindings recover state if a panic
// leaves bindings inconsistent.
//
// Hook ordering is enforced at the composite-wrapper level in
// app/service_adapters.go: federation hooks fire BEFORE commons
// (federation cleans bindings first; commons cancels recurring spends
// independently).
type FederationServiceHooks struct {
	k Keeper
}

// NewFederationServiceHooks wraps the federation keeper. Returned value
// satisfies servicetypes.ServiceHooks via duck-typing — the import-time
// `var _` assertion is in service_adapters.go alongside the wiring.
func NewFederationServiceHooks(k Keeper) *FederationServiceHooks {
	return &FederationServiceHooks{k: k}
}

// AfterOperatorDissolved prunes all federation bindings for the
// operator at the given service_type. Slashed operators don't get to
// keep their endpoint binding around.
func (h *FederationServiceHooks) AfterOperatorDissolved(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	defer h.recoverHookPanic(ctx, "AfterOperatorDissolved", operator, serviceType)
	h.pruneBindingsFor(ctx, operator, serviceType, "dissolved")
}

// AfterOperatorRetired prunes the same bindings as Dissolved (the
// operator has voluntarily exited; their bindings are no longer
// claimable). Differs from Dissolved only in the emitted-event reason
// attribute for downstream auditing.
func (h *FederationServiceHooks) AfterOperatorRetired(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	defer h.recoverHookPanic(ctx, "AfterOperatorRetired", operator, serviceType)
	h.pruneBindingsFor(ctx, operator, serviceType, "retired")
}

// AfterOperatorUnderfunded marks all bindings for the operator at the
// given service_type as suspended. Federation refuses new content
// submissions / outbound attestations from suspended bindings until
// the operator tops up and AfterOperatorReFunded clears the flag.
func (h *FederationServiceHooks) AfterOperatorUnderfunded(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	defer h.recoverHookPanic(ctx, "AfterOperatorUnderfunded", operator, serviceType)
	h.setSuspendedFor(ctx, operator, serviceType, true)
}

// AfterOperatorReFunded clears the suspended flag on all bindings for
// the operator at the given service_type.
func (h *FederationServiceHooks) AfterOperatorReFunded(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	defer h.recoverHookPanic(ctx, "AfterOperatorReFunded", operator, serviceType)
	h.setSuspendedFor(ctx, operator, serviceType, false)
}

// pruneBindingsFor iterates BindingsByOperator at prefix (service_type,
// operator_addr) and deletes every binding it finds (along with the
// peer-side BridgesByPeer index entry and the reverse-index entry
// itself). Used by both Dissolved and Retired callbacks.
func (h *FederationServiceHooks) pruneBindingsFor(ctx context.Context, operator sdk.AccAddress, serviceType string, reason string) {
	addrStr, err := h.k.addressCodec.BytesToString(operator)
	if err != nil {
		// Address codec failure: log via panic so recoverHookPanic
		// captures it without crashing the slash tx.
		panic(fmt.Errorf("addressCodec.BytesToString: %w", err))
	}

	// Iterate the reverse index at prefix (service_type, addrStr).
	// Collect peer_ids first so we don't mutate the index while
	// iterating it.
	prefix := collections.NewSuperPrefixedTripleRange[string, string, string](serviceType, addrStr)
	var peerIDs []string
	iter, err := h.k.BindingsByOperator.Iterate(ctx, prefix)
	if err != nil {
		panic(fmt.Errorf("BindingsByOperator.Iterate: %w", err))
	}
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			_ = iter.Close()
			panic(fmt.Errorf("BindingsByOperator iter key: %w", err))
		}
		peerIDs = append(peerIDs, key.K3())
	}
	_ = iter.Close()

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, peerID := range peerIDs {
		bindingKey := collections.Join(addrStr, peerID)
		_ = h.k.BridgeBindings.Remove(ctx, bindingKey)
		_ = h.k.BridgesByPeer.Remove(ctx, collections.Join(peerID, addrStr))
		_ = h.k.BindingsByOperator.Remove(ctx, collections.Join3(serviceType, addrStr, peerID))

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeBridgeUnbound,
				sdk.NewAttribute(types.AttributeKeyOperator, addrStr),
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
				sdk.NewAttribute("reason", reason),
			),
		)
	}
}

// setSuspendedFor flips the Suspended flag on every binding owned by
// (operator, service_type). Used by Underfunded / ReFunded callbacks.
func (h *FederationServiceHooks) setSuspendedFor(ctx context.Context, operator sdk.AccAddress, serviceType string, suspended bool) {
	addrStr, err := h.k.addressCodec.BytesToString(operator)
	if err != nil {
		panic(fmt.Errorf("addressCodec.BytesToString: %w", err))
	}

	prefix := collections.NewSuperPrefixedTripleRange[string, string, string](serviceType, addrStr)
	iter, err := h.k.BindingsByOperator.Iterate(ctx, prefix)
	if err != nil {
		panic(fmt.Errorf("BindingsByOperator.Iterate: %w", err))
	}
	defer iter.Close()

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	eventType := types.EventTypeBridgeSuspended
	if !suspended {
		eventType = types.EventTypeBridgeResumed
	}

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			panic(fmt.Errorf("BindingsByOperator iter key: %w", err))
		}
		peerID := key.K3()
		bindingKey := collections.Join(addrStr, peerID)

		binding, err := h.k.BridgeBindings.Get(ctx, bindingKey)
		if err != nil {
			// Reverse-index entry exists but binding doesn't — orphan.
			// Don't panic; the orphan invariant + prune machinery
			// catches this on its own schedule.
			continue
		}
		if binding.Suspended == suspended {
			continue
		}
		binding.Suspended = suspended
		_ = h.k.BridgeBindings.Set(ctx, bindingKey, binding)

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(eventType,
				sdk.NewAttribute(types.AttributeKeyOperator, addrStr),
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
			),
		)
	}
}

// recoverHookPanic is the fail-soft tail-call for every hook. A panic
// here would otherwise propagate up through the keeper's hook fire and
// roll back the service slash/topup — that would brick all bridge
// slashing chain-wide on a federation bug. Instead we log, emit a
// federation_hook_failure event so off-chain monitors can alert, and
// let the service tx commit.
func (h *FederationServiceHooks) recoverHookPanic(ctx context.Context, hookName string, operator sdk.AccAddress, serviceType string) {
	r := recover()
	if r == nil {
		return
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.Logger().Error("federation service hook panic recovered",
		"hook", hookName,
		"operator", operator.String(),
		"service_type", serviceType,
		"panic", fmt.Sprintf("%v", r),
	)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeFederationHookFailure,
			sdk.NewAttribute("hook", hookName),
			sdk.NewAttribute(types.AttributeKeyOperator, operator.String()),
			sdk.NewAttribute(types.AttributeKeyServiceType, serviceType),
			sdk.NewAttribute("panic", fmt.Sprintf("%v", r)),
		),
	)
}
