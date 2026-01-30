package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AbortSeasonTransition(ctx context.Context, msg *types.MsgAbortSeasonTransition) (*types.MsgAbortSeasonTransitionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgAbortSeasonTransitionResponse{}, nil
}
