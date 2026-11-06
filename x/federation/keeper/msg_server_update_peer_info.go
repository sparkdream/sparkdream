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
// correct them; identity fields (id, type, channels) stay fixed. A
// non-empty expected_chain_id replaces the peer's channel pin, but only on
// a Spark Dream peer that is not ACTIVE: the pin is what MsgResumePeer
// verifies the channel against (FED-106), so moving it on an active peer
// would re-trust a channel without the activation vote.
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

	if err := types.ValidatePeerInfo(msg.DisplayName, msg.Metadata); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, err.Error())
	}

	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	if peer.Status == types.PeerStatus_PEER_STATUS_REMOVED {
		return nil, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q is removed", msg.PeerId)
	}

	if msg.ExpectedChainId != "" {
		if peer.Type != types.PeerType_PEER_TYPE_SPARK_DREAM {
			return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "expected_chain_id is only for Spark Dream peers (peer %q is %s)", msg.PeerId, peer.Type)
		}
		if peer.Status == types.PeerStatus_PEER_STATUS_ACTIVE {
			return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "expected_chain_id of ACTIVE peer %q cannot change; suspend it first", msg.PeerId)
		}
		if err := types.ValidateChainID(msg.ExpectedChainId); err != nil {
			return nil, errorsmod.Wrap(types.ErrInvalidRequest, err.Error())
		}
		peer.ExpectedChainId = msg.ExpectedChainId
	}

	peer.DisplayName = msg.DisplayName
	peer.Metadata = msg.Metadata
	if err := k.SetPeer(ctx, peer); err != nil {
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
