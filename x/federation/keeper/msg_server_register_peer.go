package keeper

import (
	"bytes"
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RegisterPeer(ctx context.Context, msg *types.MsgRegisterPeer) (*types.MsgRegisterPeerResponse, error) {
	authorityBytes, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// 1. Verify authority is governance or Commons Council policy address
	if !bytes.Equal(k.authority, authorityBytes) {
		if k.late.commonsKeeper == nil || !k.late.commonsKeeper.IsCouncilAuthorized(ctx, msg.Authority, "commons", "") {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Commons Council")
		}
	}

	// 2. Validate peer ID format
	if !types.ValidatePeerID(msg.PeerId) {
		return nil, errorsmod.Wrapf(types.ErrInvalidPeerID, "peer ID %q must be lowercase alphanumeric + hyphens + dots, 3-64 chars", msg.PeerId)
	}

	// 3. Validate peer type
	if msg.Type == types.PeerType_PEER_TYPE_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrPeerTypeMismatch, "peer type must be specified")
	}

	// 4. Check peer doesn't already exist (or is REMOVED — allow re-registration)
	existingPeer, err := k.Peers.Get(ctx, msg.PeerId)
	if err == nil {
		if existingPeer.Status != types.PeerStatus_PEER_STATUS_REMOVED {
			return nil, errorsmod.Wrapf(types.ErrPeerAlreadyExists, "peer %q already exists with status %s", msg.PeerId, existingPeer.Status)
		}
		// If REMOVED, check it's not still in cleanup queue
		hasRemoval, _ := k.PeerRemovalQueue.Has(ctx, msg.PeerId)
		if hasRemoval {
			return nil, errorsmod.Wrapf(types.ErrPeerCleanupInProgress, "peer %q removal cleanup still in progress", msg.PeerId)
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 5. Create peer with PENDING status
	peer := types.Peer{
		Id:           msg.PeerId,
		DisplayName:  msg.DisplayName,
		Type:         msg.Type,
		Status:       types.PeerStatus_PEER_STATUS_PENDING,
		IbcChannelId: msg.IbcChannelId,
		RegisteredAt: blockTime,
		RegisteredBy: msg.Authority,
		Metadata:     msg.Metadata,
	}

	if err := k.Peers.Set(ctx, msg.PeerId, peer); err != nil {
		return nil, err
	}

	// 6. Create default PeerPolicy
	policy := types.PeerPolicy{
		PeerId: msg.PeerId,
	}
	if err := k.PeerPolicies.Set(ctx, msg.PeerId, policy); err != nil {
		return nil, err
	}

	// 7. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypePeerRegistered,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyPeerType, msg.Type.String()),
			sdk.NewAttribute(types.AttributeKeyDisplayName, msg.DisplayName),
			sdk.NewAttribute(types.AttributeKeyRegisteredBy, msg.Authority),
		),
	)

	return &types.MsgRegisterPeerResponse{}, nil
}
