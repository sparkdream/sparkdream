package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"slices"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) SubmitFederatedContent(ctx context.Context, msg *types.MsgSubmitFederatedContent) (*types.MsgSubmitFederatedContentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Operator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid operator address")
	}

	// 1. Verify operator is a registered, ACTIVE bridge for this peer
	bridgeKey := collections.Join(msg.Operator, msg.PeerId)
	bridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not registered for peer %s", msg.Operator, msg.PeerId)
	}
	if bridge.Status != types.BridgeStatus_BRIDGE_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotActive, "bridge status is %s", bridge.Status)
	}

	// 2. Verify peer is ACTIVE
	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	if peer.Status != types.PeerStatus_PEER_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q status is %s", msg.PeerId, peer.Status)
	}

	// 3. Verify content_type is in peer policy's inbound_content_types
	policy, err := k.PeerPolicies.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(policy.InboundContentTypes, msg.ContentType) {
		return nil, errorsmod.Wrapf(types.ErrContentTypeNotAllowed, "content type %q not allowed for peer %s", msg.ContentType, msg.PeerId)
	}

	// 4. Verify creator_identity is not in blocked_identities
	if slices.Contains(policy.BlockedIdentities, msg.CreatorIdentity) {
		return nil, errorsmod.Wrapf(types.ErrIdentityBlocked, "identity %q is blocked for peer %s", msg.CreatorIdentity, msg.PeerId)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 5. (Rate limits checked elsewhere — TODO: implement sliding window check)

	// 6. Content hash is REQUIRED
	if len(msg.ContentHash) == 0 {
		return nil, types.ErrContentHashRequired
	}

	// 7. Truncate fields for storage (hash covers full source content, not truncated body)
	body := msg.Body
	if uint64(len(body)) > params.MaxContentBodySize {
		body = body[:params.MaxContentBodySize]
	}
	contentUri := msg.ContentUri
	if uint64(len(contentUri)) > params.MaxContentUriSize {
		contentUri = contentUri[:params.MaxContentUriSize]
	}
	protocolMetadata := msg.ProtocolMetadata
	if uint64(len(protocolMetadata)) > params.MaxProtocolMetadataSize {
		protocolMetadata = protocolMetadata[:params.MaxProtocolMetadataSize]
	}

	// 8. Check ContentByHash for duplicates
	hashHex := hex.EncodeToString(msg.ContentHash)
	_, err = k.ContentByHash.Get(ctx, hashHex)
	if err == nil {
		return nil, errorsmod.Wrapf(types.ErrDuplicateContent, "content with hash %s already exists", hashHex)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 9. Allocate content ID
	contentID, err := k.ContentSeq.Next(ctx)
	if err != nil {
		return nil, err
	}

	// 10. Set status to PENDING_VERIFICATION
	expiresAt := blockTime + int64(params.ContentTtl.Seconds())

	content := types.FederatedContent{
		Id:               contentID,
		PeerId:           msg.PeerId,
		RemoteContentId:  msg.RemoteContentId,
		ContentType:      msg.ContentType,
		CreatorIdentity:  msg.CreatorIdentity,
		CreatorName:      msg.CreatorName,
		Title:            msg.Title,
		Body:             body,
		ContentUri:       contentUri,
		ProtocolMetadata: protocolMetadata,
		RemoteCreatedAt:  msg.RemoteCreatedAt,
		ReceivedAt:       blockTime,
		SubmittedBy:      msg.Operator,
		Status:           types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION,
		ExpiresAt:        expiresAt,
		ContentHash:      msg.ContentHash,
	}

	// 11. Store content and indexes
	if err := k.Content.Set(ctx, contentID, content); err != nil {
		return nil, err
	}
	if err := k.ContentByPeer.Set(ctx, collections.Join(msg.PeerId, contentID)); err != nil {
		return nil, err
	}
	if err := k.ContentByType.Set(ctx, collections.Join(msg.ContentType, contentID)); err != nil {
		return nil, err
	}
	if msg.CreatorIdentity != "" {
		if err := k.ContentByCreator.Set(ctx, collections.Join(msg.CreatorIdentity, contentID)); err != nil {
			return nil, err
		}
	}
	if err := k.ContentByHash.Set(ctx, hashHex, contentID); err != nil {
		return nil, err
	}
	if err := k.ContentExpiration.Set(ctx, collections.Join(expiresAt, contentID)); err != nil {
		return nil, err
	}

	// 12. Add to VerificationWindowQueue
	verificationDeadline := blockTime + int64(params.VerificationWindow.Seconds())
	if err := k.VerificationWindow.Set(ctx, collections.Join(verificationDeadline, contentID)); err != nil {
		return nil, err
	}

	// 13. Update bridge stats
	bridge.ContentSubmitted++
	bridge.LastSubmissionAt = blockTime
	if err := k.BridgeOperators.Set(ctx, bridgeKey, bridge); err != nil {
		return nil, err
	}

	// 14. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeFederatedContentReceived,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyContentType, msg.ContentType),
			sdk.NewAttribute(types.AttributeKeyCreatorIdentity, msg.CreatorIdentity)),
	)

	return &types.MsgSubmitFederatedContentResponse{ContentId: contentID}, nil
}
