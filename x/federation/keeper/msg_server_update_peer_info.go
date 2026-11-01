package keeper

import (
	"bytes"
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UpdatePeerInfo replaces a peer's display_name and metadata. These are
// descriptive only, so the same authorities that maintain peer policy may
// correct them; identity fields (id, type, channels) stay fixed.
func (k msgServer) UpdatePeerInfo(ctx context.Context, msg *types.MsgUpdatePeerInfo) (*types.MsgUpdatePeerInfoResponse, error) {
	authorityBytes, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	if !bytes.Equal(k.authority, authorityBytes) {
		if k.late.commonsKeeper == nil || !k.late.commonsKeeper.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance, Commons Council or Operations Committee")
		}
	}

	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	if peer.Status == types.PeerStatus_PEER_STATUS_REMOVED {
		return nil, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q is removed", msg.PeerId)
	}

	peer.DisplayName = msg.DisplayName
	peer.Metadata = msg.Metadata
	if err := k.Peers.Set(ctx, msg.PeerId, peer); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypePeerInfoUpdated,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyDisplayName, msg.DisplayName),
			sdk.NewAttribute(types.AttributeKeyUpdatedBy, msg.Authority),
		),
	)

	return &types.MsgUpdatePeerInfoResponse{}, nil
}
