package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
)

// PruneOrphanBindings is a dual-authority message (Operations Committee
// OR gov) that prunes BridgeBindings whose referenced service.Operator
// is in a terminal state (SLASHED/RETIRED/missing). Recovery path when
// the fail-soft hook pattern swallowed a panic and left an orphan.
// Pure cleanup, no value mutation.
//
// Phase 5b of the federation→service migration plan. Iterates the
// BridgesByPeer index for the specified peer, derives the service_type
// from peer.type, queries serviceKeeper.GetOperator for each binding's
// address, and prunes any binding whose Operator returns !exists.
//
// Returns the number of orphan bindings pruned. If service keeper is
// not wired (standalone-mode tests), the message is a no-op and
// returns 0.
func (k msgServer) PruneOrphanBindings(ctx context.Context, msg *types.MsgPruneOrphanBindings) (*types.MsgPruneOrphanBindingsResponse, error) {
	if !k.IsGovAuthority(msg.Authority) && !k.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be x/gov or Operations Committee")
	}

	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}

	sk := k.serviceKeeper()
	if sk == nil {
		return &types.MsgPruneOrphanBindingsResponse{Pruned: 0}, nil
	}

	serviceType, err := serviceTypeForPeer(peer.Type)
	if err != nil {
		// SPARK_DREAM peers don't have service-backed bridges; nothing
		// to prune. Not an error condition.
		return &types.MsgPruneOrphanBindingsResponse{Pruned: 0}, nil
	}

	// Walk BridgesByPeer for the given peer, checking each operator's
	// service.Operator status. Collect orphan addresses first (don't
	// mutate the index during iteration).
	var orphanAddrs []string
	rng := collections.NewPrefixedPairRange[string, string](msg.PeerId)
	if err := k.BridgesByPeer.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		operatorAddr := key.K2()
		operatorBytes, addrErr := k.addressCodec.StringToBytes(operatorAddr)
		if addrErr != nil {
			// Malformed address in the index — treat as orphan.
			orphanAddrs = append(orphanAddrs, operatorAddr)
			return false, nil
		}
		if _, exists := sk.GetOperator(ctx, operatorBytes, serviceType); !exists {
			orphanAddrs = append(orphanAddrs, operatorAddr)
		}
		return false, nil
	}); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	pruned := uint64(0)
	for _, addr := range orphanAddrs {
		bindingKey := collections.Join(addr, msg.PeerId)
		_ = k.BridgeBindings.Remove(ctx, bindingKey)
		_ = k.BridgesByPeer.Remove(ctx, collections.Join(msg.PeerId, addr))
		_ = k.BindingsByOperator.Remove(ctx, collections.Join3(serviceType, addr, msg.PeerId))
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeBridgeUnbound,
				sdk.NewAttribute(types.AttributeKeyOperator, addr),
				sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
				sdk.NewAttribute("reason", "orphan_pruned"),
			),
		)
		pruned++
	}

	return &types.MsgPruneOrphanBindingsResponse{Pruned: pruned}, nil
}
