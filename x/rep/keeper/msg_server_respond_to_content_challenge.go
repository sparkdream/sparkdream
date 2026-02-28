package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) RespondToContentChallenge(ctx context.Context, msg *types.MsgRespondToContentChallenge) (*types.MsgRespondToContentChallengeResponse, error) {
	authorAddr, err := k.addressCodec.StringToBytes(msg.Author)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid author address")
	}

	err = k.Keeper.RespondToContentChallenge(
		ctx,
		msg.ContentChallengeId,
		authorAddr,
		msg.Response,
		msg.Evidence,
	)
	if err != nil {
		return nil, err
	}

	return &types.MsgRespondToContentChallengeResponse{}, nil
}
