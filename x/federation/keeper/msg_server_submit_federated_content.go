package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"slices"

	commontypes "sparkdream/x/common/types"
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
	bridge, err := k.BridgeBindings.Get(ctx, bridgeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not registered for peer %s", msg.Operator, msg.PeerId)
	}
	if bridge.Suspended {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotActive, "bridge is suspended")
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

	// 3b. Open content: the chain accepts only public-domain content. The
	//     license is the operator's claim about the source; verifiers check
	//     it against the fetched post before vouching for the record.
	if !commontypes.IsUnencumberedLicense(msg.License) {
		return nil, errorsmod.Wrapf(types.ErrLicenseNotAccepted, "license %q", msg.License)
	}

	// 4. Verify creator_identity is not in blocked_identities
	if slices.Contains(policy.BlockedIdentities, msg.CreatorIdentity) {
		return nil, errorsmod.Wrapf(types.ErrIdentityBlocked, "identity %q is blocked for peer %s", msg.CreatorIdentity, msg.PeerId)
	}

	// 4b. Provenance: on an ActivityPub peer a content_uri must live on the
	//     peer's own host or one of its policy content_hosts. Otherwise a
	//     bridge bonded for this peer could anchor another instance's posts
	//     under this peer's name. AT Protocol ids are at:// URIs whose
	//     authority is a DID, not the peer domain, so the rule does not
	//     apply there. An empty content_uri is still accepted: nothing can
	//     fetch it, so no honest verifier can ever confirm it.
	if peer.Type == types.PeerType_PEER_TYPE_ACTIVITYPUB && msg.ContentUri != "" {
		if err := types.ContentURIHostAllowed(msg.PeerId, policy.ContentHosts, msg.ContentUri); err != nil {
			return nil, errorsmod.Wrap(types.ErrContentHostMismatch, err.Error())
		}
	}

	// 4c. The same rule for the attribution: a creator_identity's host must
	//     belong to the peer, or a bridge could anchor this peer's post as
	//     someone else's on another instance. Empty stays accepted
	//     (unattributed), like an empty content_uri.
	if peer.Type == types.PeerType_PEER_TYPE_ACTIVITYPUB && msg.CreatorIdentity != "" {
		if err := types.CreatorIdentityHostAllowed(msg.PeerId, policy.ContentHosts, msg.CreatorIdentity); err != nil {
			return nil, errorsmod.Wrap(types.ErrCreatorHostMismatch, err.Error())
		}
	}

	// 4d. Author curation: the community decides whose content a bridge may
	//     anchor for this peer (allowed_identities and/or a curated
	//     collection); both gates must pass.
	if err := k.CheckAuthorAdmitted(ctx, policy, msg.CreatorIdentity); err != nil {
		return nil, err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 5a. Global per-block cap (spec §10.2). Checked before the per-peer
	//     window so a chain-wide flood from one peer can't waste a slot
	//     on every other peer's counter.
	if err := k.checkAndRecordInboundPerBlock(ctx, sdkCtx.BlockHeight(), params.MaxInboundPerBlock); err != nil {
		return nil, errorsmod.Wrap(err, "global per-block inbound cap")
	}

	// 5b. Per-peer sliding-window rate limit (spec §10.2). Zero policy
	//     limit or non-positive window disables the check.
	if err := k.checkAndRecordInboundRate(
		ctx,
		msg.PeerId,
		blockTime,
		int64(params.RateLimitWindow.Seconds()),
		policy.InboundRateLimitPerEpoch,
	); err != nil {
		return nil, errorsmod.Wrapf(err, "peer %s", msg.PeerId)
	}

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

	// 8b. A supersede must name an earlier record of the SAME content_uri,
	//     anchored by the SAME operator for the SAME peer, that nothing has
	//     superseded yet. An operator can only retire its own records.
	var predecessor *types.FederatedContent
	if msg.Supersedes != nil {
		prev, err := k.Content.Get(ctx, msg.Supersedes.ContentId)
		switch {
		case err != nil:
			return nil, errorsmod.Wrapf(types.ErrInvalidSupersede, "content %d not found", msg.Supersedes.ContentId)
		case prev.SubmittedBy != msg.Operator:
			return nil, errorsmod.Wrapf(types.ErrInvalidSupersede, "content %d was submitted by another operator", prev.Id)
		case prev.PeerId != msg.PeerId:
			return nil, errorsmod.Wrapf(types.ErrInvalidSupersede, "content %d belongs to peer %s", prev.Id, prev.PeerId)
		case contentUri == "" || prev.ContentUri != contentUri:
			return nil, errorsmod.Wrapf(types.ErrInvalidSupersede, "content %d has content_uri %q, not %q", prev.Id, prev.ContentUri, contentUri)
		case prev.SupersededBy != 0:
			return nil, errorsmod.Wrapf(types.ErrInvalidSupersede, "content %d is already superseded by %d", prev.Id, prev.SupersededBy)
		}
		predecessor = &prev
	}

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
		Supersedes:       msg.Supersedes,
		License:          msg.License,
	}
	applyContentMediaLabels(&content)

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

	// 12b. Retire the predecessor. Still PENDING_VERIFICATION, it can never
	//      be verified (its bytes are gone at the source), so it moves to
	//      SUPERSEDED: the verification-expiry sweep only touches PENDING
	//      records, which keeps it from counting against the operator as
	//      unverified. Past verification, it keeps its status and only
	//      gains the forward link.
	if predecessor != nil {
		retired := predecessor.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION
		if retired {
			predecessor.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_SUPERSEDED
		}
		predecessor.SupersededBy = contentID
		if err := k.Content.Set(ctx, predecessor.Id, *predecessor); err != nil {
			return nil, err
		}
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeContentSuperseded,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", predecessor.Id)),
				sdk.NewAttribute(types.AttributeKeySupersededBy, fmt.Sprintf("%d", contentID)),
				sdk.NewAttribute(types.AttributeKeyNewStatus, predecessor.Status.String())),
		)
	}

	// 13. Update bridge stats
	bridge.ContentSubmitted++
	bridge.EpochSubmitted++
	bridge.LastSubmissionAt = blockTime
	if err := k.BridgeBindings.Set(ctx, bridgeKey, bridge); err != nil {
		return nil, err
	}

	// 14. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeFederatedContentReceived,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyContentType, msg.ContentType),
			sdk.NewAttribute(types.AttributeKeyCreatorIdentity, msg.CreatorIdentity),
			sdk.NewAttribute(types.AttributeKeyLicense, msg.License)),
	)

	return &types.MsgSubmitFederatedContentResponse{ContentId: contentID}, nil
}
