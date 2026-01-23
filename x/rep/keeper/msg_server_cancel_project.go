package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) CancelProject(ctx context.Context, msg *types.MsgCancelProject) (*types.MsgCancelProjectResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Cancel the project using the keeper method
	if err := k.Keeper.CancelProject(ctx, msg.ProjectId, msg.Reason); err != nil {
		return nil, errorsmod.Wrap(err, "failed to cancel project")
	}

	return &types.MsgCancelProjectResponse{}, nil
}
