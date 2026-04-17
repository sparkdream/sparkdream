package keeper

import (
	"context"
	"fmt"
	"time"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	ibcchannelkeeper "github.com/cosmos/ibc-go/v10/modules/core/04-channel/keeper"
)

// getIBCChannelKeeper returns the raw IBC channel keeper for SendPacket calls.
// We use the concrete type to avoid interface mismatches (sdk.Context vs context.Context).
func (k Keeper) getIBCChannelKeeper() *ibcchannelkeeper.Keeper {
	if k.ibcKeeperFn == nil {
		return nil
	}
	ibcK := k.ibcKeeperFn()
	if ibcK == nil {
		return nil
	}
	return ibcK.ChannelKeeper
}

// SendFederationPacket marshals and sends a FederationPacketData to the specified peer's IBC channel.
// Returns (0, nil) if IBC is not available (e.g., in unit tests without a full IBC stack).
func (k Keeper) SendFederationPacket(ctx context.Context, peerID string, packetData *types.FederationPacketData) (uint64, error) {
	channelKeeper := k.getIBCChannelKeeper()
	if channelKeeper == nil {
		return 0, nil // IBC not wired — skip send (test/development mode)
	}

	peer, err := k.Peers.Get(ctx, peerID)
	if err != nil {
		return 0, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", peerID)
	}
	if peer.IbcChannelId == "" {
		return 0, errorsmod.Wrapf(types.ErrIBCNotAvailable, "peer %q has no IBC channel", peerID)
	}

	port, err := k.Port.Get(ctx)
	if err != nil {
		port = types.PortID
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	// Compute timeout timestamp from params
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	timeoutDuration := params.IbcPacketTimeout
	if timeoutDuration == 0 {
		timeoutDuration = 10 * time.Minute
	}
	timeoutTimestamp := uint64(sdkCtx.BlockTime().Add(timeoutDuration).UnixNano())

	data, err := packetData.Marshal()
	if err != nil {
		return 0, errorsmod.Wrap(err, "failed to marshal packet data")
	}

	seq, err := channelKeeper.SendPacket(
		sdkCtx,
		port,
		peer.IbcChannelId,
		clienttypes.ZeroHeight(), // no height timeout — use timestamp only
		timeoutTimestamp,
		data,
	)
	if err != nil {
		return 0, errorsmod.Wrap(err, "failed to send IBC packet")
	}

	return seq, nil
}

// --- OnRecvPacket Handlers ---

// OnRecvContentPacket processes an incoming ContentPacket from a remote peer.
func (k Keeper) OnRecvContentPacket(ctx context.Context, sourcePort, sourceChannel string, packet *types.ContentPacket) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Find peer by channel
	peerID, err := k.findPeerByChannel(ctx, sourceChannel)
	if err != nil {
		return errorsmod.Wrap(err, "unknown source channel")
	}

	// Check policy allows this content type inbound
	policy, err := k.PeerPolicies.Get(ctx, peerID)
	if err != nil {
		return errorsmod.Wrapf(err, "no policy for peer %s", peerID)
	}
	allowed := false
	for _, ct := range policy.InboundContentTypes {
		if ct == packet.ContentType {
			allowed = true
			break
		}
	}
	if !allowed {
		return errorsmod.Wrapf(types.ErrContentTypeNotAllowed, "content type %q not accepted from peer %s", packet.ContentType, peerID)
	}

	// Deduplicate by content hash
	hashHex := fmt.Sprintf("%x", packet.ContentHash)
	if _, err := k.ContentByHash.Get(ctx, hashHex); err == nil {
		return nil // Already have this content — idempotent success
	}

	// Store as FederatedContent
	contentID, err := k.ContentSeq.Next(ctx)
	if err != nil {
		return err
	}

	blockTime := sdkCtx.BlockTime().Unix()
	params, _ := k.Params.Get(ctx)
	expiresAt := blockTime + int64(params.ContentTtl/time.Second)

	content := types.FederatedContent{
		Id:              contentID,
		PeerId:          peerID,
		ContentType:     packet.ContentType,
		RemoteContentId: packet.RemoteContentId,
		CreatorIdentity: packet.Creator,
		CreatorName:     packet.CreatorName,
		Title:           packet.Title,
		Body:            packet.Body,
		ContentUri:      packet.ContentUri,
		ContentHash:     packet.ContentHash,
		ReceivedAt:      blockTime,
		ExpiresAt:       expiresAt,
		Status:          types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE,
	}
	if err := k.Content.Set(ctx, contentID, content); err != nil {
		return err
	}

	// Update indexes
	_ = k.ContentByPeer.Set(ctx, collections.Join(peerID, contentID))
	_ = k.ContentByType.Set(ctx, collections.Join(packet.ContentType, contentID))
	_ = k.ContentByCreator.Set(ctx, collections.Join(packet.Creator, contentID))
	_ = k.ContentByHash.Set(ctx, hashHex, contentID)
	_ = k.ContentExpiration.Set(ctx, collections.Join(expiresAt, contentID))

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeFederatedContentReceived,
		sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
		sdk.NewAttribute(types.AttributeKeyContentType, packet.ContentType),
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute(types.AttributeKeyCreator, packet.Creator),
	))

	return nil
}

// OnRecvReputationQueryPacket responds to a reputation query from a remote peer.
func (k Keeper) OnRecvReputationQueryPacket(ctx context.Context, packet *types.ReputationQueryPacket) (*types.ReputationResponseData, error) {
	resp := &types.ReputationResponseData{
		Address: packet.QueriedAddress,
	}

	if k.late.repKeeper == nil {
		return resp, nil // Return empty response if rep module not wired
	}

	addr, err := sdk.AccAddressFromBech32(packet.QueriedAddress)
	if err != nil {
		return resp, nil // Invalid address — return empty
	}

	trustLevel, err := k.late.repKeeper.GetTrustLevel(ctx, addr)
	if err != nil {
		return resp, nil // Member not found — return empty
	}

	resp.TrustLevel = uint32(trustLevel)
	resp.IsActive = true

	return resp, nil
}

// OnRecvIdentityVerificationPacket stores a pending identity challenge from a remote peer.
func (k Keeper) OnRecvIdentityVerificationPacket(ctx context.Context, sourceChannel string, packet *types.IdentityVerificationPacket) (*types.IdentityVerificationAck, error) {
	peerID, err := k.findPeerByChannel(ctx, sourceChannel)
	if err != nil {
		return &types.IdentityVerificationAck{Exists: false}, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, _ := k.Params.Get(ctx)

	// Store the pending challenge for the claimed local address
	challengeKey := collections.Join(packet.ClaimedAddress, peerID)
	challenge := types.PendingIdentityChallenge{
		ClaimedAddress:      packet.ClaimedAddress,
		ClaimantChainPeerId: peerID,
		ClaimantAddress:     packet.ClaimantAddress,
		Challenge:           packet.Challenge,
		ReceivedAt:          sdkCtx.BlockTime().Unix(),
		ExpiresAt:           sdkCtx.BlockTime().Unix() + int64(params.ChallengeTtl/time.Second),
	}
	if err := k.PendingIdChallenges.Set(ctx, challengeKey, challenge); err != nil {
		return &types.IdentityVerificationAck{Exists: false}, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeIdentityChallengeReceived,
		sdk.NewAttribute(types.AttributeKeyLocalAddress, packet.ClaimedAddress),
		sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
	))

	return &types.IdentityVerificationAck{Exists: true}, nil
}

// OnRecvIdentityConfirmPacket processes a confirmation from the remote chain that the
// identity owner confirmed ownership via MsgConfirmIdentityLink.
func (k Keeper) OnRecvIdentityConfirmPacket(ctx context.Context, sourceChannel string, packet *types.IdentityVerificationConfirmPacket) error {
	peerID, err := k.findPeerByChannel(ctx, sourceChannel)
	if err != nil {
		return err
	}

	if !packet.Confirmed {
		return nil // Remote user declined — no action needed
	}

	// Find the identity link for the claimant on this peer
	linkKey := collections.Join(packet.ClaimantAddress, peerID)
	link, err := k.IdentityLinks.Get(ctx, linkKey)
	if err != nil {
		return errorsmod.Wrapf(types.ErrIdentityLinkNotFound, "no link for %s on peer %s", packet.ClaimantAddress, peerID)
	}

	// Mark as verified
	link.Status = types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED
	link.VerifiedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if err := k.IdentityLinks.Set(ctx, linkKey, link); err != nil {
		return err
	}

	// Remove from unverified expiration queue
	_ = k.UnverifiedLinkExp.Remove(ctx, collections.Join3(link.LinkedAt+int64(7*24*3600), packet.ClaimantAddress, peerID))

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeIdentityVerified,
		sdk.NewAttribute(types.AttributeKeyLocalAddress, packet.ClaimantAddress),
		sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
		sdk.NewAttribute(types.AttributeKeyRemoteIdentity, packet.ClaimedAddress),
	))

	return nil
}

// --- OnAcknowledgementPacket Handlers ---

// OnAckReputationQuery processes the acknowledgement for a reputation query,
// storing the response as a ReputationAttestation with appropriate discounting.
func (k Keeper) OnAckReputationQuery(ctx context.Context, originalPacket *types.ReputationQueryPacket, ackData []byte) error {
	var resp types.ReputationResponseData
	if err := resp.Unmarshal(ackData); err != nil {
		return errorsmod.Wrap(err, "failed to unmarshal reputation response")
	}

	// Apply discount: cap trust level at PROVISIONAL (1), TTL of 30 days
	discountedTrust := resp.TrustLevel
	if discountedTrust > 1 {
		discountedTrust = 1 // Cap at PROVISIONAL
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()
	ttl := int64(30 * 24 * 3600) // 30 days

	// We need to find which peer this query was for — use the requester to look up context
	// For now, store with a generic peer key based on the queried address
	attestKey := collections.Join(originalPacket.Requester, originalPacket.QueriedAddress)
	attestation := types.ReputationAttestation{
		LocalAddress:     originalPacket.Requester,
		RemoteAddress:    resp.Address,
		RemoteTrustLevel: resp.TrustLevel,
		LocalTrustCredit: discountedTrust,
		AttestedAt:       blockTime,
		ExpiresAt:        blockTime + ttl,
	}
	if err := k.RepAttestations.Set(ctx, attestKey, attestation); err != nil {
		return err
	}

	_ = k.AttestationExp.Set(ctx, collections.Join3(blockTime+ttl, originalPacket.Requester, originalPacket.QueriedAddress))

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeReputationAttested,
		sdk.NewAttribute(types.AttributeKeyLocalAddress, originalPacket.Requester),
		sdk.NewAttribute("queried_address", originalPacket.QueriedAddress),
		sdk.NewAttribute("trust_level", fmt.Sprintf("%d", discountedTrust)),
	))

	return nil
}

// --- Helpers ---

// findPeerByChannel looks up the peer ID for a given IBC channel ID.
func (k Keeper) findPeerByChannel(ctx context.Context, channelID string) (string, error) {
	var foundPeerID string
	err := k.Peers.Walk(ctx, nil, func(peerID string, peer types.Peer) (bool, error) {
		if peer.IbcChannelId == channelID {
			foundPeerID = peerID
			return true, nil // stop
		}
		return false, nil
	})
	if err != nil {
		return "", err
	}
	if foundPeerID == "" {
		return "", fmt.Errorf("no peer found for channel %s", channelID)
	}
	return foundPeerID, nil
}
