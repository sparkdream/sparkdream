package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) RetrySeasonTransition(ctx context.Context, msg *types.MsgRetrySeasonTransition) (*types.MsgRetrySeasonTransitionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgRetrySeasonTransitionResponse{}, nil
}
