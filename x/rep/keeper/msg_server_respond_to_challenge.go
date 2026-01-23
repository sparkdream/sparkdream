package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) RespondToChallenge(ctx context.Context, msg *types.MsgRespondToChallenge) (*types.MsgRespondToChallengeResponse, error) {
	assigneeAddr, err := k.addressCodec.StringToBytes(msg.Assignee)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid assignee address")
	}

	// Respond to the challenge
	if err := k.Keeper.RespondToChallenge(ctx, msg.ChallengeId, assigneeAddr, msg.Response, msg.Evidence); err != nil {
		return nil, err
	}

	return &types.MsgRespondToChallengeResponse{}, nil
}
