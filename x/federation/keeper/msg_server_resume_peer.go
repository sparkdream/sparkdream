package keeper

import (
	"bytes"
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ResumePeer(ctx context.Context, msg *types.MsgResumePeer) (*types.MsgResumePeerResponse, error) {
	authorityBytes, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	if !bytes.Equal(k.authority, authorityBytes) {
		if k.late.commonsKeeper == nil || !k.late.commonsKeeper.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Commons Council")
		}
	}

	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	// Allow resuming SUSPENDED peers and activating PENDING peers (council-gated activation)
	if peer.Status != types.PeerStatus_PEER_STATUS_SUSPENDED && peer.Status != types.PeerStatus_PEER_STATUS_PENDING {
		return nil, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q is not suspended or pending (status: %s)", msg.PeerId, peer.Status)
	}

	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	if err := k.Peers.Set(ctx, msg.PeerId, peer); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypePeerResumed,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
		),
	)

	return &types.MsgResumePeerResponse{}, nil
}
