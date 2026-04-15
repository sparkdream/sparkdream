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

	// 3. Send IdentityVerificationConfirmPacket via IBC (TODO: implement IBC packet sending)
	// The fact that creator signed this tx proves they own the private key for claimed_address.

	// 4. Delete the PendingIdentityChallenge
	if err := k.PendingIdChallenges.Remove(ctx, challengeKey); err != nil {
		return nil, err
	}

	// 5. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeIdentityChallengeConfirmed,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.ClaimantChainPeerId)),
	)

	return &types.MsgConfirmIdentityLinkResponse{}, nil
}
