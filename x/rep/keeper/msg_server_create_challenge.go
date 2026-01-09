package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) CreateChallenge(ctx context.Context, msg *types.MsgCreateChallenge) (*types.MsgCreateChallengeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Challenger); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgCreateChallengeResponse{}, nil
}
