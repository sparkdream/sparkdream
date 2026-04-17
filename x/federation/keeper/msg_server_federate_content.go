package keeper

import (
	"context"
	"slices"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) FederateContent(ctx context.Context, msg *types.MsgFederateContent) (*types.MsgFederateContentResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	_ = creatorBytes

	// NOTE: This handler does not verify that msg.Creator owns the local content
	// referenced by msg.LocalContentId. Enforcing ownership would require cross-module
	// calls (e.g., blogKeeper.GetPost, forumKeeper.GetPost) that are not currently
	// wired into x/federation. Until those dependencies are added, any member meeting
	// the trust level threshold can federate any local content. This is a known
	// limitation and should be addressed when cross-module content ownership queries
	// are available.

	// 1. Verify peer exists, is ACTIVE, and is type SPARK_DREAM (IBC only)
	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	if peer.Status != types.PeerStatus_PEER_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q status is %s", msg.PeerId, peer.Status)
	}
	if peer.Type != types.PeerType_PEER_TYPE_SPARK_DREAM {
		return nil, errorsmod.Wrapf(types.ErrPeerTypeMismatch, "MsgFederateContent only for IBC peers, peer %q is %s", msg.PeerId, peer.Type)
	}

	// 2. Verify content_type is in outbound_content_types
	policy, err := k.PeerPolicies.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(policy.OutboundContentTypes, msg.ContentType) {
		return nil, errorsmod.Wrapf(types.ErrContentTypeNotAllowed, "content type %q not in outbound types for peer %s", msg.ContentType, msg.PeerId)
	}

	// 3. Verify creator meets min_outbound_trust_level (via x/rep)
	if k.late.repKeeper != nil && policy.MinOutboundTrustLevel > 0 {
		trustLevel, err := k.late.repKeeper.GetTrustLevel(ctx, sdk.AccAddress(creatorBytes))
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrTrustLevelInsufficient, "failed to get trust level")
		}
		if uint32(trustLevel) < policy.MinOutboundTrustLevel {
			return nil, errorsmod.Wrapf(types.ErrTrustLevelInsufficient, "trust level %d < required %d", trustLevel, policy.MinOutboundTrustLevel)
		}
	}

	// 4. Send ContentPacket via IBC
	packetData := &types.FederationPacketData{
		Packet: &types.FederationPacketData_Content{
			Content: &types.ContentPacket{
				ContentType:     msg.ContentType,
				RemoteContentId: msg.LocalContentId,
				Creator:         msg.Creator,
				Title:           msg.Title,
				Body:            msg.Body,
				ContentUri:      msg.ContentUri,
				ContentHash:     msg.ContentHash,
			},
		},
	}
	// Best-effort: the outbound attestation is recorded locally regardless.
	// Content delivery completes when the remote chain acknowledges the packet.
	_, _ = k.SendFederationPacket(ctx, msg.PeerId, packetData)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 6. Store OutboundAttestation
	attestID, err := k.OutboundAttestSeq.Next(ctx)
	if err != nil {
		return nil, err
	}
	attestation := types.OutboundAttestation{
		Id:             attestID,
		PeerId:         msg.PeerId,
		ContentType:    msg.ContentType,
		LocalContentId: msg.LocalContentId,
		Creator:        msg.Creator,
		PublishedAt:    blockTime,
	}
	if err := k.OutboundAttestations.Set(ctx, attestID, attestation); err != nil {
		return nil, err
	}

	// 7. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeContentFederated,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyContentType, msg.ContentType),
			sdk.NewAttribute(types.AttributeKeyLocalContentID, msg.LocalContentId),
			sdk.NewAttribute(types.AttributeKeyCreator, msg.Creator)),
	)

	return &types.MsgFederateContentResponse{}, nil
}
