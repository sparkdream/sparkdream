package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
)

// FederationServiceHooks implements x/service's ServiceHooks interface.
// Phase 5 of the federation→service migration plan. Each callback runs in
// a cache context with a recover (runIsolated): a panic never rolls back
// the service-side transaction that triggered the hook — bridge slashing
// must remain unblockable even if federation has a bug — and never leaves
// a half-applied prune behind either. Out-of-gas still fails the tx.
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
// keep their endpoint binding around. The identity links the operator
// attested on those peers lose their backing bond, so each (operator,
// peer) with attested links is queued for EndBlocker Phase 3b, which
// returns them to UNVERIFIED in budgeted batches (the number of links one
// operator attested is unbounded, and running out of gas here would fail
// the slash itself).
func (h *FederationServiceHooks) AfterOperatorDissolved(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	h.runIsolated(ctx, "AfterOperatorDissolved", operator, serviceType, func(ctx context.Context) {
		peerIDs := h.pruneBindingsFor(ctx, operator, serviceType, "dissolved")
		h.queueAttesterRevocations(ctx, operator, peerIDs)
	})
}

// queueAttesterRevocations queues (operator, peer) for every peer in
// peerIDs on which the operator attested at least one identity link.
func (h *FederationServiceHooks) queueAttesterRevocations(ctx context.Context, operator sdk.AccAddress, peerIDs []string) {
	addrStr, err := h.k.addressCodec.BytesToString(operator)
	if err != nil {
		panic(fmt.Errorf("addressCodec.BytesToString: %w", err))
	}
	for _, peerID := range peerIDs {
		iter, err := h.k.IdentityLinksByAttester.Iterate(ctx,
			collections.NewSuperPrefixedTripleRange[string, string, string](addrStr, peerID))
		if err != nil {
			panic(fmt.Errorf("IdentityLinksByAttester.Iterate: %w", err))
		}
		has := iter.Valid()
		_ = iter.Close()
		if has {
			if err := h.k.AttesterRevocations.Set(ctx, collections.Join(addrStr, peerID)); err != nil {
				panic(fmt.Errorf("AttesterRevocations.Set: %w", err))
			}
		}
	}
}

// AfterOperatorRetired prunes the same bindings as Dissolved (the
// operator has voluntarily exited; their bindings are no longer
// claimable). Differs from Dissolved only in the emitted-event reason
// attribute for downstream auditing.
func (h *FederationServiceHooks) AfterOperatorRetired(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	h.runIsolated(ctx, "AfterOperatorRetired", operator, serviceType, func(ctx context.Context) {
		h.pruneBindingsFor(ctx, operator, serviceType, "retired")
	})
}

// AfterOperatorUnderfunded marks all bindings for the operator at the
// given service_type as suspended. Federation refuses new content
// submissions / outbound attestations from suspended bindings until
// the operator tops up and AfterOperatorReFunded clears the flag.
func (h *FederationServiceHooks) AfterOperatorUnderfunded(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	h.runIsolated(ctx, "AfterOperatorUnderfunded", operator, serviceType, func(ctx context.Context) {
		h.setSuspendedFor(ctx, operator, serviceType, true)
	})
}

// AfterOperatorReFunded clears the suspended flag on all bindings for
// the operator at the given service_type.
func (h *FederationServiceHooks) AfterOperatorReFunded(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	h.runIsolated(ctx, "AfterOperatorReFunded", operator, serviceType, func(ctx context.Context) {
		h.setSuspendedFor(ctx, operator, serviceType, false)
	})
}

// pruneBindingsFor iterates BindingsByOperator at prefix (service_type,
// operator_addr) and deletes every binding it finds (along with the
// peer-side BridgesByPeer index entry and the reverse-index entry
// itself). Used by both Dissolved and Retired callbacks. Returns the
// peers whose bindings it removed.
func (h *FederationServiceHooks) pruneBindingsFor(ctx context.Context, operator sdk.AccAddress, serviceType string, reason string) []string {
	addrStr, err := h.k.addressCodec.BytesToString(operator)
	if err != nil {
		// Address codec failure: log via panic so runIsolated
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
		if err := h.k.BridgeBindings.Remove(ctx, bindingKey); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneBindingsFor: BridgeBindings.Remove failed", "err", err)
		}
		if err := h.k.BridgesByPeer.Remove(ctx, collections.Join(peerID, addrStr)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneBindingsFor: BridgesByPeer.Remove failed", "err", err)
		}
		if err := h.k.BindingsByOperator.Remove(ctx, collections.Join3(serviceType, addrStr, peerID)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneBindingsFor: BindingsByOperator.Remove failed", "err", err)
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeBridgeUnbound,
				sdk.NewAttribute(types.AttributeKeyOperator, addrStr),
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
				sdk.NewAttribute("reason", reason),
			),
		)
	}
	return peerIDs
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
		if err := h.k.BridgeBindings.Set(ctx, bindingKey, binding); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("setSuspendedFor: BridgeBindings.Set failed", "err", err)
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(eventType,
				sdk.NewAttribute(types.AttributeKeyOperator, addrStr),
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
			),
		)
	}
}

// runIsolated runs one hook body in a cache context and commits its writes
// only if it finishes. A panic part-way through a prune or suspend loop
// would otherwise leave some bindings changed and their reverse indexes
// inconsistent; now the hook either applies whole or not at all. Out-of-gas
// is re-panicked rather than swallowed: it is the enclosing transaction's
// limit, not a federation bug, and must fail that transaction.
func (h *FederationServiceHooks) runIsolated(ctx context.Context, hookName string, operator sdk.AccAddress, serviceType string, fn func(ctx context.Context)) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()
	completed := func() (ok bool) {
		defer func() {
			if r := recover(); r != nil {
				if _, oog := r.(storetypes.ErrorOutOfGas); oog {
					panic(r)
				}
				h.reportHookPanic(ctx, hookName, operator, serviceType, r)
				ok = false
			}
		}()
		fn(cacheCtx)
		return true
	}()
	if completed {
		write()
	}
}

// reportHookPanic logs a recovered hook panic and emits
// federation_hook_failure so off-chain monitors can alert. The panic is not
// propagated: it would roll back the service slash/topup that fired the
// hook, bricking bridge slashing chain-wide on a federation bug.
func (h *FederationServiceHooks) reportHookPanic(ctx context.Context, hookName string, operator sdk.AccAddress, serviceType string, r any) {
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
