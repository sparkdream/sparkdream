package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) ApproveInitiative(ctx context.Context, msg *types.MsgApproveInitiative) (*types.MsgApproveInitiativeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get initiative
	initiative, err := k.Keeper.GetInitiative(ctx, msg.InitiativeId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get initiative")
	}

	// Validate status - must be SUBMITTED
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED {
		return nil, errorsmod.Wrap(types.ErrInvalidInitiativeStatus, "initiative must be in SUBMITTED status")
	}

	// This is a staker approval - store the criteria votes and comments
	// In a full implementation, this would track individual staker approvals
	// For now, we just validate that the approver is a staker

	// If disapproved, mark initiative as rejected/abandoned
	if !msg.Approved {
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED

		// Return budget to project
		if err := k.Keeper.ReturnBudget(ctx, initiative.ProjectId, DerefInt(initiative.Budget)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to return budget")
		}

		if err := k.Keeper.UpdateInitiative(ctx, initiative); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update initiative")
		}
	} else {
		// Add approver to approvals list
		if initiative.Approvals == nil {
			initiative.Approvals = []string{}
		}
		initiative.Approvals = append(initiative.Approvals, msg.Creator)

		if err := k.Keeper.UpdateInitiative(ctx, initiative); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update initiative")
		}
	}

	// The completion will be handled by CompleteInitiative when conviction is sufficient

	return &types.MsgApproveInitiativeResponse{}, nil
}
