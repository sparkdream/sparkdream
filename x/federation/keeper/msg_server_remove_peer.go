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

	// 1. Verify authority is governance or Commons Council
	if !bytes.Equal(k.authority, authorityBytes) {
		if k.late.commonsKeeper == nil || !k.late.commonsKeeper.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Commons Council")
		}
	}

	// 2. Verify peer exists and is not already REMOVED
	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	if peer.Status == types.PeerStatus_PEER_STATUS_REMOVED {
		return nil, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q is already removed", msg.PeerId)
	}

	// 2a. Block removal if there are still bindings (Phase 5 of the
	// federation→service migration plan, peer-removal block decision).
	// Operators must unbond via service.MsgUnbondOperator first; the
	// AfterOperatorRetired hook will then prune the bindings, after
	// which this message can run. For abandoned peers (operators never
	// respond), the abandoned-peer escape hatch is to bundle
	// MsgReportOperator(T1_SLASH, dissolve=true) for each bridge into
	// the same gov proposal as MsgRemovePeer — those reports dissolve
	// the operators atomically, fire AfterOperatorDissolved → prune
	// bindings → and then RemovePeer succeeds.
	bridgeCount, err := k.countBridgesForPeer(ctx, msg.PeerId)
	if err != nil {
		return nil, err
	}
	if bridgeCount > 0 {
		return nil, errorsmod.Wrapf(types.ErrPeerHasActiveBridges,
			"peer %q still has %d active bridges; operators must unbond via service.MsgUnbondOperator first (or bundle MsgReportOperator dissolutions into the same proposal for abandoned peers — see migration plan)",
			msg.PeerId, bridgeCount)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 3. Set peer status to REMOVED
	peer.Status = types.PeerStatus_PEER_STATUS_REMOVED
	peer.RemovedAt = blockTime
	if err := k.Peers.Set(ctx, msg.PeerId, peer); err != nil {
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
