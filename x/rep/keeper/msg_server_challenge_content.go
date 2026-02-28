package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) ChallengeContent(ctx context.Context, msg *types.MsgChallengeContent) (*types.MsgChallengeContentResponse, error) {
	challengerAddr, err := k.addressCodec.StringToBytes(msg.Challenger)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid challenger address")
	}

	if msg.StakedDream == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "staked DREAM is required")
	}

	ccID, err := k.Keeper.CreateContentChallenge(
		ctx,
		challengerAddr,
		types.StakeTargetType(msg.TargetType),
		msg.TargetId,
		msg.Reason,
		msg.Evidence,
		*msg.StakedDream,
	)
	if err != nil {
		return nil, err
	}

	return &types.MsgChallengeContentResponse{
		ContentChallengeId: ccID,
	}, nil
}
