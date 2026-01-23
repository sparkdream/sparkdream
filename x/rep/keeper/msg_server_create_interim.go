package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) CreateInterim(ctx context.Context, msg *types.MsgCreateInterim) (*types.MsgCreateInterimResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Create interim work with single assignee (creator)
	_, err := k.Keeper.CreateInterimWork(
		ctx,
		msg.InterimType,
		[]string{msg.Creator},
		"", // Committee will be determined based on interim type
		msg.ReferenceId,
		msg.ReferenceType,
		msg.Complexity,
		msg.Deadline,
	)
	if err != nil {
		return nil, err
	}

	return &types.MsgCreateInterimResponse{}, nil
}
