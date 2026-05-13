package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) ApproveProjectBudget(ctx context.Context, msg *types.MsgApproveProjectBudget) (*types.MsgApproveProjectBudgetResponse, error) {
	approverAddr, err := k.addressCodec.StringToBytes(msg.Approver)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid approver address")
	}

	// All authorization — tier-aware council/committee membership and the
	// large-budget proposal-required gate — lives in keeper.ApproveProject so
	// the rule cannot be bypassed by other code paths that call the keeper
	// directly.
	if err := k.ApproveProject(
		ctx,
		msg.ProjectId,
		approverAddr,
		*msg.ApprovedBudget,
		*msg.ApprovedSpark,
	); err != nil {
		return nil, errorsmod.Wrap(err, "failed to approve project")
	}

	return &types.MsgApproveProjectBudgetResponse{}, nil
}
