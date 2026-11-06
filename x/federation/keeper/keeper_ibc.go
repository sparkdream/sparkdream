package keeper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	ibcchannelkeeper "github.com/cosmos/ibc-go/v10/modules/core/04-channel/keeper"
)

// getIBCChannelKeeper returns the raw IBC channel keeper for SendPacket calls.
// We use the concrete type to avoid interface mismatches (sdk.Context vs context.Context).
// Returns nil only if the IBC keeper hasn't been wired yet (test fixtures).
func (k Keeper) getIBCChannelKeeper() *ibcchannelkeeper.Keeper {
	if k.late == nil || k.late.ibcKeeperFn == nil {
		return nil
	}
	ibcK := k.late.ibcKeeperFn()
	if ibcK == nil {
		return nil
	}
	return ibcK.ChannelKeeper
}

// SendFederationPacket marshals and sends a FederationPacketData to the specified peer's IBC channel.
// Returns an error in every failure case — including IBC keeper not wired — so callers cannot
// silently swallow a missed packet send. (Earlier versions returned (0, nil) when IBC was not
// wired, which made the broken state look like success and meant SendPacket failures emitted
// no events for relayers like Hermes to act on. See the multichain suite's
// test_crosschain_content.sh "Silent SendPacket regression" test.)
func (k Keeper) SendFederationPacket(ctx context.Context, peerID string, packetData *types.FederationPacketData) (uint64, error) {
	if k.late != nil && k.late.sendPacketOverride != nil {
		data, err := packetData.Marshal()
		if err != nil {
			return 0, errorsmod.Wrap(err, "failed to marshal packet data")
		}
		return k.late.sendPacketOverride(ctx, peerID, data)
	}
	channelKeeper := k.getIBCChannelKeeper()
	if channelKeeper == nil {
		return 0, errorsmod.Wrap(types.ErrIBCNotAvailable,
			"IBC channel keeper not wired into x/federation; app.go must call FederationKeeper.SetIBCKeeperFn(...)")
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
	timeoutTimestamp := commontypes.Saturate[uint64](sdkCtx.BlockTime().Add(timeoutDuration).UnixNano())

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

// --- Inbound peer resolution ---

// boundPort is the port this module is bound to (types.PortID unless
// genesis set another).
func (k Keeper) boundPort(ctx context.Context) string {
	port, err := k.Port.Get(ctx)
	if err != nil || port == "" {
		return types.PortID
	}
	return port
}

// inboundPeer identifies the peer an incoming packet came from.
//
// It MUST be called with the packet's DESTINATION port and channel — this
// chain's end of the channel, which is what Peer.ibc_channel_id stores and
// what SendFederationPacket sends on. The packet's source channel is the
// sending chain's own channel id: any chain can open a channel to the
// federation port and arrange for its end to carry a registered peer's
// channel number, so matching on it would let that chain pose as the peer
// (and would mis-attribute every packet between honest peers whose two
// channel ends are numbered differently).
//
// Only an ACTIVE peer is accepted: a SUSPENDED peer gets an error
// acknowledgement (spec §6.3, suspension blocks all inbound federation),
// and a REMOVED or unknown channel is refused outright.
func (k Keeper) inboundPeer(ctx context.Context, destPort, destChannel string) (types.Peer, error) {
	if destPort != k.boundPort(ctx) {
		return types.Peer{}, errorsmod.Wrapf(types.ErrUnknownChannel, "port %q is not the federation port", destPort)
	}
	peerID, err := k.findPeerByChannel(ctx, destChannel)
	if err != nil {
		return types.Peer{}, errorsmod.Wrapf(types.ErrUnknownChannel, "channel %s: %s", destChannel, err)
	}
	peer, err := k.Peers.Get(ctx, peerID)
	if err != nil {
		return types.Peer{}, errorsmod.Wrapf(types.ErrUnknownChannel, "peer %q not found", peerID)
	}
	if peer.Status != types.PeerStatus_PEER_STATUS_ACTIVE {
		return types.Peer{}, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q is %s", peerID, peer.Status)
	}
	return peer, nil
}

// --- OnRecvPacket Handlers ---

// OnRecvContentPacket processes an incoming ContentPacket from a remote
// peer. destPort/destChannel are this chain's end of the channel (see
// inboundPeer).
//
// The IBC path applies the same admission rules as the bridge path
// (MsgSubmitFederatedContent): the peer's blocked identities, the global
// per-block and per-peer inbound rate limits, and storage truncation of the
// body, URI and metadata. Fields with no param (title, creator name) are
// truncated to compiled caps, and identifiers that would be wrong if cut
// (creator, remote_content_id) are refused when oversized. The content hash
// must be a 32-byte SHA-256 digest: an empty or arbitrary one would make
// unrelated packets collide in the dedup index.
func (k Keeper) OnRecvContentPacket(ctx context.Context, destPort, destChannel string, packet *types.ContentPacket) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	peer, err := k.inboundPeer(ctx, destPort, destChannel)
	if err != nil {
		return err
	}
	peerID := peer.Id

	// Check policy allows this content type inbound
	policy, err := k.PeerPolicies.Get(ctx, peerID)
	if err != nil {
		return errorsmod.Wrapf(err, "no policy for peer %s", peerID)
	}
	if !slices.Contains(policy.InboundContentTypes, packet.ContentType) {
		return errorsmod.Wrapf(types.ErrContentTypeNotAllowed, "content type %q not accepted from peer %s", packet.ContentType, peerID)
	}

	// Open content: only public-domain content enters the chain. A sister
	// chain stamps its own content license; one that is missing (a peer on
	// a binary predating the field) or encumbered is refused.
	if !commontypes.IsUnencumberedLicense(packet.License) {
		return errorsmod.Wrapf(types.ErrLicenseNotAccepted, "license %q from peer %s", packet.License, peerID)
	}

	// Operations Committee identity blocks apply federation-wide (spec
	// §14.4), not only to bridged content.
	if slices.Contains(policy.BlockedIdentities, packet.Creator) {
		return errorsmod.Wrapf(types.ErrIdentityBlocked, "identity %q is blocked for peer %s", packet.Creator, peerID)
	}
	if len(packet.Creator) > types.MaxInboundIdentifierSize || len(packet.RemoteContentId) > types.MaxInboundIdentifierSize {
		return errorsmod.Wrapf(types.ErrInvalidInboundField, "creator / remote_content_id longer than %d bytes", types.MaxInboundIdentifierSize)
	}
	if len(packet.ContentHash) != types.ContentHashSize {
		return errorsmod.Wrapf(types.ErrInvalidContentHash, "got %d bytes", len(packet.ContentHash))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	blockTime := sdkCtx.BlockTime().Unix()

	// The per-peer sliding window is charged first, before the duplicate
	// check, so a peer replaying hashes it already sent spends its own
	// inbound budget on them (each attempt still costs a peer walk and a
	// relay-fee refund). The global per-block cap is charged only for
	// content that is actually new (FED-205): otherwise one peer's replays
	// would consume the whole block's budget and starve every other peer.
	// A rejected packet gets an error acknowledgement; the counters roll
	// back with the rest of the failed receive (spec §10.2).
	if err := k.checkAndRecordInboundRate(ctx, peerID, blockTime,
		int64(params.RateLimitWindow.Seconds()), policy.InboundRateLimitPerEpoch); err != nil {
		return errorsmod.Wrapf(err, "peer %s", peerID)
	}

	// Deduplicate by content hash (against live records only; a pruned
	// record's hash may be received again)
	hashHex := fmt.Sprintf("%x", packet.ContentHash)
	if k.contentHashAnchored(ctx, hashHex) {
		return nil // Already have this content — idempotent success
	}

	if err := k.checkAndRecordInboundPerBlock(ctx, sdkCtx.BlockHeight(), params.MaxInboundPerBlock); err != nil {
		return errorsmod.Wrap(err, "global per-block inbound cap")
	}

	// Store as FederatedContent
	contentID, err := k.ContentSeq.Next(ctx)
	if err != nil {
		return err
	}

	expiresAt := blockTime + int64(params.ContentTtl/time.Second)

	// A peer under require_review lands its content HIDDEN; an Operations
	// Committee restore (MsgModerateContent to ACTIVE) publishes it.
	status := types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE
	if policy.RequireReview {
		status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN
	}

	content := types.FederatedContent{
		Id:               contentID,
		PeerId:           peerID,
		ContentType:      packet.ContentType,
		RemoteContentId:  packet.RemoteContentId,
		CreatorIdentity:  packet.Creator,
		CreatorName:      types.TruncateUTF8(packet.CreatorName, types.MaxInboundCreatorNameSize),
		Title:            types.TruncateUTF8(packet.Title, types.MaxInboundTitleSize),
		Body:             types.TruncateUTF8(packet.Body, params.MaxContentBodySize),
		ContentUri:       types.TruncateUTF8(packet.ContentUri, params.MaxContentUriSize),
		ProtocolMetadata: truncateBytes(packet.ProtocolMetadata, params.MaxProtocolMetadataSize),
		RemoteCreatedAt:  packet.CreatedAt,
		ContentHash:      packet.ContentHash,
		ReceivedAt:       blockTime,
		ExpiresAt:        expiresAt,
		Status:           status,
		License:          packet.License,
	}
	applyContentMediaLabels(&content)
	if err := k.Content.Set(ctx, contentID, content); err != nil {
		return err
	}

	// Update indexes
	if err := k.ContentByPeer.Set(ctx, collections.Join(peerID, contentID)); err != nil {
		return err
	}
	if err := k.ContentByType.Set(ctx, collections.Join(packet.ContentType, contentID)); err != nil {
		return err
	}
	if err := k.ContentByCreator.Set(ctx, collections.Join(packet.Creator, contentID)); err != nil {
		return err
	}
	if err := k.ContentByHash.Set(ctx, hashHex, contentID); err != nil {
		return err
	}
	if err := k.ContentExpiration.Set(ctx, collections.Join(expiresAt, contentID)); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeFederatedContentReceived,
		sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
		sdk.NewAttribute(types.AttributeKeyContentType, packet.ContentType),
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute(types.AttributeKeyCreator, packet.Creator),
		sdk.NewAttribute(types.AttributeKeyLicense, packet.License),
	))
	if policy.RequireReview {
		emitContentAwaitingReview(ctx, contentID, peerID)
	}

	return nil
}

// emitContentAwaitingReview records content that landed HIDDEN because its
// peer's policy has require_review set.
func emitContentAwaitingReview(ctx context.Context, contentID uint64, peerID string) {
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(types.EventTypeContentAwaitingReview,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
	))
}

// truncateBytes cuts b to at most max bytes (storage truncation for opaque
// protocol metadata; zero disables metadata entirely).
func truncateBytes(b []byte, max uint64) []byte {
	if uint64(len(b)) <= max {
		return b
	}
	return b[:max]
}

// OnRecvReputationQueryPacket responds to a reputation query from a remote
// peer. destPort/destChannel are this chain's end of the channel.
func (k Keeper) OnRecvReputationQueryPacket(ctx context.Context, destPort, destChannel string, packet *types.ReputationQueryPacket) (*types.ReputationResponseData, error) {
	peer, err := k.inboundPeer(ctx, destPort, destChannel)
	if err != nil {
		return nil, err
	}
	peerID := peer.Id
	if peer.Type != types.PeerType_PEER_TYPE_SPARK_DREAM {
		return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "reputation queries only allowed from Spark Dream peers")
	}

	policy, err := k.PeerPolicies.Get(ctx, peerID)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "no policy for peer %s", peerID)
	}
	if !policy.AllowReputationQueries {
		return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "peer %s does not allow reputation queries", peerID)
	}

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
	resp.TrustLevel = commontypes.Saturate[uint32](trustLevel)

	// Active status, join time and per-tag reputation come from the member
	// record. A zeroed or inactive member reports is_active = false, so a
	// peer never credits standing this chain has withdrawn.
	if rm, ok := k.repMembers(); ok {
		resp.IsActive = rm.IsActiveMember(ctx, addr)
		if member, merr := rm.GetMember(ctx, addr); merr == nil {
			resp.MemberSince = member.JoinedAt
			resp.Reputations = tagReputations(member.ReputationScores)
		}
	}

	return resp, nil
}

// tagReputations converts a member's per-tag reputation map (decimal
// strings) into the wire form: whole-number scores, sorted by tag so every
// validator acks identical bytes, capped at MaxReputationResponseTags.
func tagReputations(scores map[string]string) []types.TagReputation {
	tags := make([]string, 0, len(scores))
	for tag := range scores {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	var out []types.TagReputation
	for _, tag := range tags {
		if len(out) >= types.MaxReputationResponseTags {
			break
		}
		dec, err := math.LegacyNewDecFromStr(scores[tag])
		if err != nil || !dec.IsPositive() {
			continue
		}
		out = append(out, types.TagReputation{Tag: tag, Score: dec.TruncateInt()})
	}
	return out
}

// OnRecvIdentityVerificationPacket stores a pending identity challenge from
// a remote peer. destPort/destChannel are this chain's end of the channel.
//
// The claimed address must be a well-formed local address with an account
// in x/auth; otherwise the ack says exists = false (spec §8.3 step 1) and
// nothing is stored. Each peer may have at most MaxPendingChallengesPerPeer
// challenges pending, and the walk that expires them is keyed by expiry, so
// a flood of challenges costs the EndBlocker nothing until they expire.
func (k Keeper) OnRecvIdentityVerificationPacket(ctx context.Context, destPort, destChannel string, packet *types.IdentityVerificationPacket) (*types.IdentityVerificationAck, error) {
	peer, err := k.inboundPeer(ctx, destPort, destChannel)
	if err != nil {
		return &types.IdentityVerificationAck{Exists: false}, err
	}
	peerID := peer.Id

	if len(packet.ClaimantAddress) == 0 || len(packet.ClaimantAddress) > types.MaxInboundIdentifierSize ||
		len(packet.Challenge) == 0 || len(packet.Challenge) > 64 {
		return &types.IdentityVerificationAck{Exists: false},
			errorsmod.Wrap(types.ErrInvalidInboundField, "claimant address or challenge malformed")
	}

	claimedBytes, err := k.addressCodec.StringToBytes(packet.ClaimedAddress)
	if err != nil || k.authKeeper.GetAccount(ctx, sdk.AccAddress(claimedBytes)) == nil {
		// Not an account on this chain: answer exists = false so the
		// claimant's chain revokes the link. No state is written.
		return &types.IdentityVerificationAck{Exists: false}, nil
	}

	challengeKey := collections.Join(packet.ClaimedAddress, peerID)
	if has, _ := k.PendingIdChallenges.Has(ctx, challengeKey); !has &&
		k.pendingChallengeCount(ctx, peerID) >= types.MaxPendingChallengesPerPeer {
		return &types.IdentityVerificationAck{Exists: false},
			errorsmod.Wrapf(types.ErrTooManyChallenges, "peer %s has %d pending", peerID, types.MaxPendingChallengesPerPeer)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(ctx).Logger().Error("OnRecvIdentityVerificationPacket: Params.Get failed", "err", getErr)
	}

	// Store the pending challenge for the claimed local address
	challenge := types.PendingIdentityChallenge{
		ClaimedAddress:      packet.ClaimedAddress,
		ClaimantChainPeerId: peerID,
		ClaimantAddress:     packet.ClaimantAddress,
		Challenge:           packet.Challenge,
		ReceivedAt:          sdkCtx.BlockTime().Unix(),
		ExpiresAt:           sdkCtx.BlockTime().Unix() + int64(params.ChallengeTtl/time.Second),
	}
	if err := k.setPendingChallenge(ctx, challenge); err != nil {
		return &types.IdentityVerificationAck{Exists: false}, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeIdentityChallengeReceived,
		sdk.NewAttribute(types.AttributeKeyLocalAddress, packet.ClaimedAddress),
		sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
	))

	return &types.IdentityVerificationAck{Exists: true}, nil
}

// OnRecvIdentityConfirmPacket processes a confirmation from the remote chain
// that the identity owner confirmed ownership via MsgConfirmIdentityLink.
// destPort/destChannel are this chain's end of the channel.
func (k Keeper) OnRecvIdentityConfirmPacket(ctx context.Context, destPort, destChannel string, packet *types.IdentityVerificationConfirmPacket) error {
	peer, err := k.inboundPeer(ctx, destPort, destChannel)
	if err != nil {
		return err
	}
	peerID := peer.Id

	if !packet.Confirmed {
		return nil // Remote user declined — no action needed
	}

	// Find the identity link for the claimant on this peer
	linkKey := collections.Join(packet.ClaimantAddress, peerID)
	link, err := k.IdentityLinks.Get(ctx, linkKey)
	if err != nil {
		return errorsmod.Wrapf(types.ErrIdentityLinkNotFound, "no link for %s on peer %s", packet.ClaimantAddress, peerID)
	}

	// FEDERATION-S2-1: defense-in-depth checks against malformed / stale /
	// hijacked confirmation packets. The remote signer mechanism already
	// proves ownership of ClaimedAddress on the peer chain; these checks
	// ensure the packet is bound to the link that actually requested it.
	if packet.ClaimedAddress != link.RemoteIdentity {
		return errorsmod.Wrapf(types.ErrIdentityLinkNotFound,
			"confirmation claimed_address %q does not match link remote_identity %q",
			packet.ClaimedAddress, link.RemoteIdentity)
	}
	if !bytes.Equal(packet.Challenge, link.Challenge) {
		return errorsmod.Wrap(types.ErrIdentityLinkNotFound,
			"confirmation challenge does not match originally-issued challenge")
	}
	if link.Status == types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED {
		return nil // A re-sent confirmation for a link already verified.
	}
	if link.Status != types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED {
		return errorsmod.Wrapf(types.ErrIdentityLinkNotFound, "link is %s", link.Status)
	}

	// One verified owner per remote identity. Other members may hold
	// unverified claims to it, but a remote identity already verified to
	// someone else stays theirs (it has to be unlinked first).
	remoteKey := collections.Join(peerID, link.RemoteIdentity)
	if owner, oerr := k.IdentityLinksByRemote.Get(ctx, remoteKey); oerr == nil && owner != packet.ClaimantAddress {
		return errorsmod.Wrapf(types.ErrRemoteIdentityAlreadyClaimed,
			"remote identity %s on peer %s is already verified to another address", link.RemoteIdentity, peerID)
	}

	// Remove from unverified expiration queue (keyed by the expiry the
	// link was written with) before the status flips.
	if err := k.UnverifiedLinkExp.Remove(ctx, collections.Join3(k.linkUnverifiedExpiry(ctx, link), packet.ClaimantAddress, peerID)); err != nil {
		return err
	}

	// Mark as verified
	link.Status = types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED
	link.VerifiedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if err := k.IdentityLinks.Set(ctx, linkKey, link); err != nil {
		return err
	}
	if err := k.IdentityLinksByRemote.Set(ctx, remoteKey, packet.ClaimantAddress); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeIdentityVerified,
		sdk.NewAttribute(types.AttributeKeyLocalAddress, packet.ClaimantAddress),
		sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
		sdk.NewAttribute(types.AttributeKeyRemoteIdentity, packet.ClaimedAddress),
	))

	return nil
}

// --- OnAcknowledgementPacket / OnTimeoutPacket Handlers ---
//
// These run on packets THIS chain sent, so the packet's SOURCE channel is
// this chain's end and identifies the peer. They never return an error for
// a malformed or unattributable acknowledgement: an error from
// OnAcknowledgementPacket fails MsgAcknowledgement, and a received packet's
// commitment can then never be cleared (it cannot time out either). Such
// acks are dropped with an event instead.

// emitAckDropped records an acknowledgement that could not be applied.
func emitAckDropped(ctx context.Context, packetKind, sourceChannel, reason string) {
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(types.EventTypeAckDropped,
		sdk.NewAttribute("packet", packetKind),
		sdk.NewAttribute("source_channel", sourceChannel),
		sdk.NewAttribute(types.AttributeKeyReason, reason),
	))
}

// OnAckReputationQuery stores the peer's answer as a ReputationAttestation.
//
// sourceChannel identifies which peer answered. It has to be passed in: the
// ReputationQueryPacket carries only the requester and the queried address,
// so without the channel the handler cannot tell which peer a reply came
// from -- which is how attestations previously ended up keyed by
// (requester, queried_address), colliding across peers and leaving
// ReputationAttestation.peer_id unset.
//
// The attestation is written only while everything that justified the
// query still holds when the answer lands: the peer is ACTIVE, the answer
// is about the address that was asked about, and the requester's identity
// link to that address is still VERIFIED. Otherwise the ack is dropped.
func (k Keeper) OnAckReputationQuery(ctx context.Context, sourceChannel string, originalPacket *types.ReputationQueryPacket, ackData []byte) error {
	var resp types.ReputationResponseData
	if err := resp.Unmarshal(ackData); err != nil {
		emitAckDropped(ctx, "reputation_query", sourceChannel, "undecodable reputation response")
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// The peer that answered, resolved from the channel the ack arrived on.
	peerID, err := k.findPeerByChannel(ctx, sourceChannel)
	if err != nil {
		emitAckDropped(ctx, "reputation_query", sourceChannel, "no peer bound to channel")
		return nil
	}
	if peer, perr := k.Peers.Get(ctx, peerID); perr != nil || peer.Status != types.PeerStatus_PEER_STATUS_ACTIVE {
		emitAckDropped(ctx, "reputation_query", sourceChannel, "peer not active")
		return nil
	} else if peer.Type != types.PeerType_PEER_TYPE_SPARK_DREAM {
		emitAckDropped(ctx, "reputation_query", sourceChannel, "peer is not a Spark Dream chain")
		return nil
	}
	// The Operations Committee's kill switch applies to answers still in
	// flight: an ack that lands after accept_reputation_attestations was
	// turned off is dropped, like a request made after it would be refused.
	if policy, perr := k.PeerPolicies.Get(ctx, peerID); perr != nil || !policy.AcceptReputationAttestations {
		emitAckDropped(ctx, "reputation_query", sourceChannel, "peer policy no longer accepts reputation attestations")
		return nil
	}
	if resp.Address != originalPacket.QueriedAddress {
		emitAckDropped(ctx, "reputation_query", sourceChannel, "response is about a different address")
		return nil
	}
	link, lerr := k.IdentityLinks.Get(ctx, collections.Join(originalPacket.Requester, peerID))
	if lerr != nil || link.Status != types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED ||
		link.RemoteIdentity != originalPacket.QueriedAddress {
		emitAckDropped(ctx, "reputation_query", sourceChannel, "requester's identity link is no longer verified")
		return nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	ttl := int64(params.AttestationTtl.Seconds())

	// Cap the peer's claim at what this chain is willing to credit. Applied on
	// write, so the stored value is already safe for any reader: a consumer
	// that forgot to cap would otherwise import a peer's own trust level.
	// global_max_trust_credit is the single knob -- defaults to 1, i.e. at
	// most PROVISIONAL-equivalent, however high the peer rates them. An
	// inactive member earns no credit at all.
	discountedTrust := resp.TrustLevel
	if discountedTrust > params.GlobalMaxTrustCredit {
		discountedTrust = params.GlobalMaxTrustCredit
	}
	if !resp.IsActive {
		discountedTrust = 0
	}

	// Keyed by (local_address, peer_id), matching QueryGetReputationAttestation
	// and the one-identity-link-per-peer invariant MsgLinkIdentity enforces.
	// A refreshed attestation replaces the old one and its expiry entry.
	// The per-tag scores are kept for display, re-capped and validated on
	// this side (sanitizeRemoteReputations): the peer is not bound by our
	// responder's cap.
	attestation := types.ReputationAttestation{
		LocalAddress:      originalPacket.Requester,
		PeerId:            peerID,
		RemoteAddress:     resp.Address,
		RemoteTrustLevel:  resp.TrustLevel,
		LocalTrustCredit:  discountedTrust,
		RemoteReputations: sanitizeRemoteReputations(resp.Reputations),
		AttestedAt:        blockTime,
		ExpiresAt:         blockTime + ttl,
	}
	if err := k.setRepAttestation(ctx, attestation); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeReputationAttested,
		sdk.NewAttribute(types.AttributeKeyLocalAddress, originalPacket.Requester),
		sdk.NewAttribute("queried_address", originalPacket.QueriedAddress),
		sdk.NewAttribute("trust_level", fmt.Sprintf("%d", discountedTrust)),
	))

	return nil
}

// OnAckIdentityVerification applies the Phase 1 acknowledgement (spec §8.3).
// exists = false means the claimed address has no account on the peer, so
// the link is revoked: deleted with its indexes and its share of the
// per-user link count, so the user can link a correct address. exists =
// true leaves the link UNVERIFIED, waiting for the Phase 2 confirmation.
func (k Keeper) OnAckIdentityVerification(ctx context.Context, sourceChannel string, packet *types.IdentityVerificationPacket, result []byte) {
	var ack types.IdentityVerificationAck
	if err := ack.Unmarshal(result); err != nil {
		emitAckDropped(ctx, "identity_verification", sourceChannel, "undecodable identity verification ack")
		return
	}
	if ack.Exists {
		return
	}
	peerID, err := k.findPeerByChannel(ctx, sourceChannel)
	if err != nil {
		emitAckDropped(ctx, "identity_verification", sourceChannel, "no peer bound to channel")
		return
	}
	link, ok := k.matchPendingLink(ctx, peerID, packet)
	if !ok {
		return
	}
	k.revokeIdentityLink(ctx, link, "remote address does not exist")
}

// OnTimeoutIdentityVerification re-sends a Phase 1 challenge whose delivery
// timed out, up to MaxVerificationAttempts deliveries in all (spec §8.4).
// After the last attempt it emits identity_verification_failed and leaves
// the link UNVERIFIED until the unverified-link TTL prunes it.
func (k Keeper) OnTimeoutIdentityVerification(ctx context.Context, sourceChannel string, packet *types.IdentityVerificationPacket) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityVerificationTimeout,
		sdk.NewAttribute(types.AttributeKeyLocalAddress, packet.ClaimantAddress),
		sdk.NewAttribute(types.AttributeKeyRemoteIdentity, packet.ClaimedAddress)))

	peerID, err := k.findPeerByChannel(ctx, sourceChannel)
	if err != nil {
		return
	}
	link, ok := k.matchPendingLink(ctx, peerID, packet)
	if !ok {
		return
	}
	if link.VerificationAttempts >= types.MaxVerificationAttempts {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityVerificationFailed,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, link.LocalAddress),
			sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
			sdk.NewAttribute("error", "challenge delivery timed out on every attempt")))
		return
	}
	if peer, perr := k.Peers.Get(ctx, peerID); perr != nil || peer.Status != types.PeerStatus_PEER_STATUS_ACTIVE {
		return
	}
	link.VerificationAttempts++
	if err := k.IdentityLinks.Set(ctx, collections.Join(link.LocalAddress, peerID), link); err != nil {
		return
	}
	retry := &types.FederationPacketData{
		Packet: &types.FederationPacketData_IdentityVerification{IdentityVerification: packet},
	}
	if _, err := k.SendFederationPacket(ctx, peerID, retry); err != nil {
		sdkCtx.Logger().With("module", "x/federation").Error(
			"identity verification retry: SendFederationPacket failed",
			"peer_id", peerID, "claimant", link.LocalAddress, "error", err)
	}
}

// matchPendingLink returns the claimant's link to peerID when it is still
// the UNVERIFIED link this Phase 1 packet was sent for (same remote
// identity, same challenge). A link that was unlinked, re-created or
// already verified since is left alone.
func (k Keeper) matchPendingLink(ctx context.Context, peerID string, packet *types.IdentityVerificationPacket) (types.IdentityLink, bool) {
	link, err := k.IdentityLinks.Get(ctx, collections.Join(packet.ClaimantAddress, peerID))
	if err != nil {
		return types.IdentityLink{}, false
	}
	if link.Status != types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED ||
		link.RemoteIdentity != packet.ClaimedAddress || !bytes.Equal(link.Challenge, packet.Challenge) {
		return types.IdentityLink{}, false
	}
	return link, true
}

// revokeIdentityLink deletes a link with its reverse-index entry, unverified
// expiry entry and share of the per-user count, and emits
// identity_link_revoked.
func (k Keeper) revokeIdentityLink(ctx context.Context, link types.IdentityLink, reason string) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(ctx).Logger().Error("revokeIdentityLink: Params.Get failed", "err", getErr)
	}
	k.removeIdentityLink(ctx, sdkCtx, link.LocalAddress, link.PeerId, params)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityLinkRevoked,
		sdk.NewAttribute(types.AttributeKeyLocalAddress, link.LocalAddress),
		sdk.NewAttribute(types.AttributeKeyPeerID, link.PeerId),
		sdk.NewAttribute(types.AttributeKeyRemoteIdentity, link.RemoteIdentity),
		sdk.NewAttribute(types.AttributeKeyReason, reason)))
}

// --- Helpers ---

// findPeerByChannel looks up the peer whose federation channel (this
// chain's end) is channelID. REMOVED peers are skipped: a channel id can be
// re-bound to a new peer once the old one is removed, and packets on it
// belong to the new binding.
func (k Keeper) findPeerByChannel(ctx context.Context, channelID string) (string, error) {
	if channelID == "" {
		return "", fmt.Errorf("empty channel id")
	}
	foundPeerID, err := k.peerForChannel(ctx, channelID)
	if err != nil {
		return "", err
	}
	if foundPeerID == "" {
		return "", fmt.Errorf("no peer found for channel %s", channelID)
	}
	return foundPeerID, nil
}

// IsActivePeerChannel reports whether channelID is this chain's end of the
// federation or transfer channel of an ACTIVE Spark Dream peer. The app's
// relay fee refund (app/relayrefund) covers only such channels: a channel
// anyone can open would otherwise let them fill blocks with junk packets
// from a chain they control, for free.
func (k Keeper) IsActivePeerChannel(ctx context.Context, channelID string) bool {
	if channelID == "" {
		return false
	}
	// one bounded prefix read of the derived index; no peer record is
	// decoded, so the cost does not depend on the registry
	iter, err := k.ActivePeerChannels.Iterate(ctx, collections.NewPrefixedPairRange[string, string](channelID))
	if err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("IsActivePeerChannel: index iterate failed", "err", err)
		return false
	}
	defer iter.Close()
	return iter.Valid()
}
