package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// generateLinkChallenge derives an unpredictable, deterministic-per-validator
// challenge from consensus state plus the link key. Including app_hash and
// block_hash means an off-chain observer cannot precompute the challenge from
// the public tx, defeating the front-run / replay class of attacks called out
// in FEDERATION-S2-1 / S2-2.
func generateLinkChallenge(sdkCtx sdk.Context, creator, peerID, remote string) []byte {
	h := sha256.New()
	header := sdkCtx.BlockHeader()
	var heightBuf [8]byte
	binary.BigEndian.PutUint64(heightBuf[:], commontypes.Saturate[uint64](sdkCtx.BlockHeight()))
	h.Write(heightBuf[:])
	var timeBuf [8]byte
	binary.BigEndian.PutUint64(timeBuf[:], commontypes.Saturate[uint64](sdkCtx.BlockTime().UnixNano()))
	h.Write(timeBuf[:])
	h.Write(header.AppHash)
	h.Write(header.LastBlockId.Hash)
	h.Write([]byte(creator))
	h.Write([]byte{0})
	h.Write([]byte(peerID))
	h.Write([]byte{0})
	h.Write([]byte(remote))
	return h.Sum(nil)
}

func (k msgServer) LinkIdentity(ctx context.Context, msg *types.MsgLinkIdentity) (*types.MsgLinkIdentityResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// 1. Verify peer exists and is ACTIVE
	peer, err := k.GetPeerRequireActive(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "peer %q", msg.PeerId)
	}

	// 2. Verify no existing link for (creator, peer_id)
	linkKey := collections.Join(msg.Creator, msg.PeerId)
	_, err = k.IdentityLinks.Get(ctx, linkKey)
	if err == nil {
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkExists, "link already exists for %s on peer %s", msg.Creator, msg.PeerId)
	}

	// 3. Verify the remote identity is not VERIFIED to another local
	//    address. An unverified claim by someone else does not block this
	//    one: claims are cheap, so letting the first one reserve the
	//    identity would let anyone squat other people's handles. Several
	//    unverified claims may coexist; verification picks the owner.
	remoteKey := collections.Join(msg.PeerId, msg.RemoteIdentity)
	_, err = k.IdentityLinksByRemote.Get(ctx, remoteKey)
	if err == nil {
		return nil, errorsmod.Wrapf(types.ErrRemoteIdentityAlreadyClaimed, "remote identity %s already verified on peer %s", msg.RemoteIdentity, msg.PeerId)
	}

	// 4. Verify max_identity_links_per_user not exceeded
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	currentCount, getErr := k.IdentityLinkCount.Get(ctx, msg.Creator)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	if currentCount >= params.MaxIdentityLinksPerUser {
		return nil, errorsmod.Wrapf(types.ErrMaxIdentityLinksExceeded, "address %s has %d links (max %d)", msg.Creator, currentCount, params.MaxIdentityLinksPerUser)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()
	challengeData := generateLinkChallenge(sdkCtx, msg.Creator, msg.PeerId, msg.RemoteIdentity)

	// 5. Create IdentityLink with status UNVERIFIED. The challenge is stored
	// so OnRecvIdentityConfirmPacket can echo-check it when the peer's
	// confirmation arrives.
	// The expiry is stored on the link so the Phase 3 sweep can tell its
	// own queue entry from a stale one left by an earlier, unlinked link.
	expiry := blockTime + int64(params.UnverifiedLinkTtl.Seconds())
	link := types.IdentityLink{
		LocalAddress:         msg.Creator,
		PeerId:               msg.PeerId,
		RemoteIdentity:       msg.RemoteIdentity,
		Status:               types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED,
		LinkedAt:             blockTime,
		Challenge:            challengeData,
		VerificationAttempts: 1,
		UnverifiedExpiresAt:  expiry,
	}
	if err := k.IdentityLinks.Set(ctx, linkKey, link); err != nil {
		return nil, err
	}

	// 6. Add to UnverifiedLinkExpirationQueue
	if err := k.UnverifiedLinkExp.Set(ctx, collections.Join3(expiry, msg.Creator, msg.PeerId)); err != nil {
		return nil, err
	}

	// 7. Increment IdentityLinkCount
	if err := k.IdentityLinkCount.Set(ctx, msg.Creator, currentCount+1); err != nil {
		return nil, err
	}

	// 8. Index the claim (not the verified reverse index; see step 3)
	if err := k.addIdentityLinkIndexes(ctx, link); err != nil {
		return nil, err
	}

	// 9. Send the IdentityVerificationPacket (Phase 1 challenge) to a Spark
	//    Dream peer. A send failure fails the tx: a link whose challenge
	//    never left would sit UNVERIFIED with no timeout to retry it. Bridge
	//    peers (ActivityPub, AT Protocol, NOSTR, Lens) have no federation
	//    channel: the member publishes the link proof string from the remote
	//    identity and a bridge operator bound to the peer verifies it with
	//    MsgAttestIdentityLink before unverified_link_ttl prunes the claim.
	if peer.Type == types.PeerType_PEER_TYPE_SPARK_DREAM {
		packetData := &types.FederationPacketData{
			Packet: &types.FederationPacketData_IdentityVerification{
				IdentityVerification: &types.IdentityVerificationPacket{
					ClaimedAddress:  msg.RemoteIdentity,
					ClaimantAddress: msg.Creator,
					Challenge:       challengeData,
				},
			},
		}
		if _, err := k.SendFederationPacket(ctx, msg.PeerId, packetData); err != nil {
			return nil, errorsmod.Wrap(err, "send identity verification challenge")
		}
	}

	// 10. Emit event. For a bridge peer it carries the proof string the
	//     member must publish from the remote identity (spec §3.4.1).
	attrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator),
		sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
		sdk.NewAttribute(types.AttributeKeyRemoteIdentity, msg.RemoteIdentity),
	}
	if peer.Type != types.PeerType_PEER_TYPE_SPARK_DREAM {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyLinkProof,
			types.IdentityLinkProofString(sdkCtx.ChainID(), msg.Creator, challengeData)))
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityLinked, attrs...))

	return &types.MsgLinkIdentityResponse{}, nil
}
