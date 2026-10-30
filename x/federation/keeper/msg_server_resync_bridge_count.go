package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/federation/types"
)

// ResyncBridgeCount is a dual-authority message (Operations Committee
// OR gov) that re-counts BridgesByPeer for the given peer and returns the
// count. It is read-only by design: there is no stored per-peer bridge
// counter to resync — max_bridges_per_peer is enforced by counting the
// index (countBridgesForPeer), so the index itself is the count. Index
// repair is MsgPruneOrphanBindings' job.
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
