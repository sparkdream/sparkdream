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

	// Get project to check council
	project, err := k.GetProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProjectNotFound, err.Error())
	}

	// Check if approver is Operations Committee member for the project's council
	// If commonsKeeper is not available, skip committee check (simulation mode)
	if k.commonsKeeper != nil {
		isMember, err := k.commonsKeeper.IsCommitteeMember(ctx, approverAddr, project.Council, "operations")
		if err != nil {
			return nil, errorsmod.Wrapf(err, "failed to check committee membership")
		}
		if !isMember {
			return nil, errorsmod.Wrapf(
				types.ErrUnauthorized,
				"approver must be a member of the Operations Committee for council '%s'",
				project.Council,
			)
		}
	}

	// Approve project with budget
	err = k.ApproveProject(
		ctx,
		msg.ProjectId,
		approverAddr,
		*msg.ApprovedBudget,
		*msg.ApprovedSpark,
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to approve project")
	}

	return &types.MsgApproveProjectBudgetResponse{}, nil
}
