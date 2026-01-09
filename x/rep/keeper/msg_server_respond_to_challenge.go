package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) RespondToChallenge(ctx context.Context, msg *types.MsgRespondToChallenge) (*types.MsgRespondToChallengeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Assignee); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgRespondToChallengeResponse{}, nil
}
