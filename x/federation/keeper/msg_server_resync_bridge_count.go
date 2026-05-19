package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/federation/types"
)

// ResyncBridgeCount is a dual-authority message (Operations Committee
// OR gov) that re-counts BridgesByPeer for the given peer. Pure
// cleanup; can't be abused to mutate operator economic state.
//
// Phase 1 of the federation→service migration. Currently a placeholder:
// the peer-level bridges_count counter the migration plan references
// will be added when the orphan-binding invariant lands (Phase 5). For
// now this is a no-op that returns the observed count without
// persisting one.
func (k msgServer) ResyncBridgeCount(ctx context.Context, msg *types.MsgResyncBridgeCount) (*types.MsgResyncBridgeCountResponse, error) {
	if !k.IsGovAuthority(msg.Authority) && !k.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be x/gov or Operations Committee")
	}

	count, err := k.countBridgesForPeer(ctx, msg.PeerId)
	if err != nil {
		return nil, err
	}

	return &types.MsgResyncBridgeCountResponse{NewCount: count}, nil
}
