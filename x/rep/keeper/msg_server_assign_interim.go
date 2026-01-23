package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AssignInterim(ctx context.Context, msg *types.MsgAssignInterim) (*types.MsgAssignInterimResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	assigneeAddr, err := k.addressCodec.StringToBytes(msg.Assignee)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid assignee address")
	}

	// Assign the interim using the keeper method
	if err := k.Keeper.AssignInterimToMember(ctx, msg.InterimId, assigneeAddr); err != nil {
		return nil, errorsmod.Wrap(err, "failed to assign interim")
	}

	return &types.MsgAssignInterimResponse{}, nil
}
