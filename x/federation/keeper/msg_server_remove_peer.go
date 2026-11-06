package keeper

import (
	"bytes"
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RemovePeer(ctx context.Context, msg *types.MsgRemovePeer) (*types.MsgRemovePeerResponse, error) {
	authorityBytes, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// 1. Verify authority is governance or the Commons Council / Operations
	//    Committee policy address (spec §6.2). Removal starts the
	//    irreversible Phase 9 cleanup of the peer's content, links and
	//    attestations, so a single committee member cannot do it.
	if !bytes.Equal(k.authority, authorityBytes) && !k.isCouncilPolicy(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or the Commons Council / Operations Committee policy address")
	}

	// 2. Verify peer exists and is not already REMOVED
	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	if peer.Status == types.PeerStatus_PEER_STATUS_REMOVED {
		return nil, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q is already removed", msg.PeerId)
	}

	// 2a. An ACTIVE peer with bridge bindings is not removed: its
	// operators unbond via service.MsgUnbondOperator first (the
	// AfterOperatorRetired hook prunes the bindings). Once the peer is
	// SUSPENDED (or still PENDING) no binding can do anything, so removal
	// goes ahead and Phase 9 prunes the remaining bindings. This is the
	// stranded-peer escape hatch (spec §6.2): one proposal bundles
	// MsgSuspendPeer and MsgRemovePeer, and an operator that never unbonds
	// can no longer hold the peer hostage. Operators keep their x/service
	// bond and status; only the federation binding goes.
	if peer.Status == types.PeerStatus_PEER_STATUS_ACTIVE {
		bridgeCount, err := k.countBridgesForPeer(ctx, msg.PeerId)
		if err != nil {
			return nil, err
		}
		if bridgeCount > 0 {
			return nil, errorsmod.Wrapf(types.ErrPeerHasActiveBridges,
				"peer %q still has %d bridges; operators must unbond via service.MsgUnbondOperator first, or suspend the peer (MsgSuspendPeer, which may be bundled into the same proposal) and its bindings are pruned on removal",
				msg.PeerId, bridgeCount)
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 3. Set peer status to REMOVED
	peer.Status = types.PeerStatus_PEER_STATUS_REMOVED
	peer.RemovedAt = blockTime
	if err := k.SetPeer(ctx, peer); err != nil {
		return nil, err
	}

	// 4. Add to PeerRemovalQueue for EndBlocker cleanup
	removalState := types.PeerRemovalState{
		RemovedAt: blockTime,
	}
	if err := k.PeerRemovalQueue.Set(ctx, msg.PeerId, removalState); err != nil {
		return nil, err
	}

	// 5. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypePeerRemoved,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyReason, msg.Reason),
		),
	)

	return &types.MsgRemovePeerResponse{}, nil
}
