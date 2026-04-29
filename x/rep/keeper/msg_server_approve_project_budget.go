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

	// Get params for large project budget threshold
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Authorization is mandatory: commonsKeeper must be wired. Treat a nil keeper as a
	// configuration error, not a bypass, to prevent unauthenticated approvals.
	if k.commonsKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "commons keeper not wired; cannot approve project budget")
	}
	if msg.ApprovedBudget.GT(params.LargeProjectBudgetThreshold) {
		// Large budget: requires council proposal (policy address or governance authority),
		// not a plain committee member. When a council proposal executes, the message's
		// approver is the policy address — not a personal address.
		//
		// Logic: if the approver is a committee member, they're a person → reject.
		// If they're council-authorized but NOT a member, they're a policy address → allow.
		isMember, _ := k.commonsKeeper.IsCommitteeMember(ctx, approverAddr, project.Council, "operations")
		if isMember {
			return nil, errorsmod.Wrapf(types.ErrLargeProjectNeedsCouncil,
				"budget %s exceeds threshold %s; individual committee members cannot approve — submit via council proposal",
				msg.ApprovedBudget.String(), params.LargeProjectBudgetThreshold.String())
		}
		// Not a plain member — check if they're a policy address or governance authority
		isCouncilAuth := k.commonsKeeper.IsCouncilAuthorized(ctx, msg.Approver, project.Council, "operations")
		if !isCouncilAuth {
			return nil, errorsmod.Wrapf(types.ErrLargeProjectNeedsCouncil,
				"budget %s exceeds threshold %s; submit via council proposal",
				msg.ApprovedBudget.String(), params.LargeProjectBudgetThreshold.String())
		}
		// Approved: caller is a policy address or governance authority
	} else {
		// Small budget: single Operations Committee member can approve
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
