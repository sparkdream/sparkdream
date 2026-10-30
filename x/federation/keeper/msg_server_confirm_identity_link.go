package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ConfirmIdentityLink(ctx context.Context, msg *types.MsgConfirmIdentityLink) (*types.MsgConfirmIdentityLinkResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// 1. Look up PendingIdentityChallenge for (creator, claimant_chain_peer_id)
	challengeKey := collections.Join(msg.Creator, msg.ClaimantChainPeerId)
	challenge, err := k.PendingIdChallenges.Get(ctx, challengeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrNoPendingChallenge, "no pending challenge for %s from peer %s", msg.Creator, msg.ClaimantChainPeerId)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 2. Verify challenge has not expired
	if blockTime > challenge.ExpiresAt {
		return nil, errorsmod.Wrapf(types.ErrChallengeExpired, "challenge expired at %d, current time %d", challenge.ExpiresAt, blockTime)
	}

	// 2b. The peer must still be ACTIVE: a suspended or removed peer gets
	//     no confirmations (spec §6.3).
	if _, err := k.GetPeerRequireActive(ctx, msg.ClaimantChainPeerId); err != nil {
		return nil, errorsmod.Wrapf(err, "peer %q", msg.ClaimantChainPeerId)
	}

	// 3. Send IdentityVerificationConfirmPacket via IBC
	// The fact that creator signed this tx proves they own the private key for claimed_address.
	packetData := &types.FederationPacketData{
		Packet: &types.FederationPacketData_IdentityConfirmation{
			IdentityConfirmation: &types.IdentityVerificationConfirmPacket{
				ClaimedAddress:  msg.Creator,
				ClaimantAddress: challenge.ClaimantAddress,
				Challenge:       challenge.Challenge,
				Confirmed:       true,
			},
		},
	}
	// A failed send fails the tx: the confirmation would otherwise be lost
	// silently while the user believes it went out.
	if _, err := k.SendFederationPacket(ctx, msg.ClaimantChainPeerId, packetData); err != nil {
		return nil, errorsmod.Wrap(err, "send identity confirmation")
	}

	// 4. The PendingIdentityChallenge is deliberately kept until it expires
	//    (EndBlocker Phase 4). If the confirmation packet times out, the
	//    user re-submits MsgConfirmIdentityLink and a new packet goes out
	//    (spec §8.3, Phase 2 timeout); the claimant chain treats a repeat
	//    confirmation of an already-verified link as a no-op.

	// 5. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeIdentityChallengeConfirmed,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.ClaimantChainPeerId)),
	)

	return &types.MsgConfirmIdentityLinkResponse{}, nil
}
