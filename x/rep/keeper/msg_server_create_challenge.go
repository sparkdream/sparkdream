package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) CreateChallenge(ctx context.Context, msg *types.MsgCreateChallenge) (*types.MsgCreateChallengeResponse, error) {
	challengerAddr, err := k.addressCodec.StringToBytes(msg.Challenger)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid challenger address")
	}

	// Validate staked DREAM amount
	if msg.StakedDream == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "staked DREAM is required")
	}

	// Create the challenge
	_, err = k.Keeper.CreateChallenge(
		ctx,
		challengerAddr,
		msg.InitiativeId,
		msg.Reason,
		msg.Evidence,
		*msg.StakedDream,
	)
	if err != nil {
		return nil, err
	}

	return &types.MsgCreateChallengeResponse{}, nil
}
