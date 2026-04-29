package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/binary"

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
	binary.BigEndian.PutUint64(heightBuf[:], uint64(sdkCtx.BlockHeight()))
	h.Write(heightBuf[:])
	var timeBuf [8]byte
	binary.BigEndian.PutUint64(timeBuf[:], uint64(sdkCtx.BlockTime().UnixNano()))
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
	if _, err := k.GetPeerRequireActive(ctx, msg.PeerId); err != nil {
		return nil, errorsmod.Wrapf(err, "peer %q", msg.PeerId)
	}

	// 2. Verify no existing link for (creator, peer_id)
	linkKey := collections.Join(msg.Creator, msg.PeerId)
	_, err := k.IdentityLinks.Get(ctx, linkKey)
	if err == nil {
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkExists, "link already exists for %s on peer %s", msg.Creator, msg.PeerId)
	}

	// 3. Verify no existing link for (peer_id, remote_identity) by any local address
	remoteKey := collections.Join(msg.PeerId, msg.RemoteIdentity)
	_, err = k.IdentityLinksByRemote.Get(ctx, remoteKey)
	if err == nil {
		return nil, errorsmod.Wrapf(types.ErrRemoteIdentityAlreadyClaimed, "remote identity %s already claimed on peer %s", msg.RemoteIdentity, msg.PeerId)
	}

	// 4. Verify max_identity_links_per_user not exceeded
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	currentCount, _ := k.IdentityLinkCount.Get(ctx, msg.Creator)
	if currentCount >= params.MaxIdentityLinksPerUser {
		return nil, errorsmod.Wrapf(types.ErrMaxIdentityLinksExceeded, "address %s has %d links (max %d)", msg.Creator, currentCount, params.MaxIdentityLinksPerUser)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()
	challengeData := generateLinkChallenge(sdkCtx, msg.Creator, msg.PeerId, msg.RemoteIdentity)

	// 5. Create IdentityLink with status UNVERIFIED. The challenge is stored
	// so OnRecvIdentityConfirmPacket can echo-check it when the peer's
	// confirmation arrives.
	link := types.IdentityLink{
		LocalAddress:   msg.Creator,
		PeerId:         msg.PeerId,
		RemoteIdentity: msg.RemoteIdentity,
		Status:         types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED,
		LinkedAt:       blockTime,
		Challenge:      challengeData,
	}
	if err := k.IdentityLinks.Set(ctx, linkKey, link); err != nil {
		return nil, err
	}

	// 6. Add to UnverifiedLinkExpirationQueue
	expiry := blockTime + int64(params.UnverifiedLinkTtl.Seconds())
	if err := k.UnverifiedLinkExp.Set(ctx, collections.Join3(expiry, msg.Creator, msg.PeerId)); err != nil {
		return nil, err
	}

	// 7. Increment IdentityLinkCount
	if err := k.IdentityLinkCount.Set(ctx, msg.Creator, currentCount+1); err != nil {
		return nil, err
	}

	// 8. Update reverse index
	if err := k.IdentityLinksByRemote.Set(ctx, remoteKey, msg.Creator); err != nil {
		return nil, err
	}

	// 9. Send IdentityVerificationPacket via IBC (Phase 1 challenge).
	packetData := &types.FederationPacketData{
		Packet: &types.FederationPacketData_IdentityVerification{
			IdentityVerification: &types.IdentityVerificationPacket{
				ClaimedAddress:  msg.RemoteIdentity,
				ClaimantAddress: msg.Creator,
				Challenge:       challengeData,
			},
		},
	}
	// Best-effort send — link is created regardless (can be verified later)
	_, _ = k.SendFederationPacket(ctx, msg.PeerId, packetData)

	// 10. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeIdentityLinked,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyRemoteIdentity, msg.RemoteIdentity)),
	)

	return &types.MsgLinkIdentityResponse{}, nil
}
