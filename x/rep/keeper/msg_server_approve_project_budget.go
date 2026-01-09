package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) ApproveProjectBudget(ctx context.Context, msg *types.MsgApproveProjectBudget) (*types.MsgApproveProjectBudgetResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Approver); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgApproveProjectBudgetResponse{}, nil
}
