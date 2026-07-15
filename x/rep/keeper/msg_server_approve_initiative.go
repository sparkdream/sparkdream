package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ApproveInitiative(ctx context.Context, msg *types.MsgApproveInitiative) (*types.MsgApproveInitiativeResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
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

	// Conflict-of-interest exclusion: neither the assignee nor the parent
	// project's creator may approve/disapprove the initiative, regardless of
	// stake or committee membership (mirrors x/reveal's contributor
	// exclusion — conflicted parties may do the work, never judge it).
	project, err := k.Keeper.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get parent project")
	}
	if msg.Creator == initiative.Assignee || msg.Creator == project.Creator {
		return nil, errorsmod.Wrap(types.ErrConflictOfInterest, "initiative assignee and project creator cannot approve their own initiative")
	}

	// Authorization: caller must have an active stake on this initiative OR be on the Commons Operations Committee.
	stakes, err := k.Keeper.GetInitiativeStakes(ctx, msg.InitiativeId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get initiative stakes")
	}
	authorized := false
	for _, s := range stakes {
		if s.Staker == msg.Creator {
			authorized = true
			break
		}
	}
	if !authorized {
		isOps, opsErr := k.Keeper.commonsKeeper.IsCommitteeMember(ctx, sdk.AccAddress(creatorBytes), "commons", "operations")
		if opsErr != nil {
			return nil, errorsmod.Wrap(opsErr, "failed to check operations committee membership")
		}
		if !isOps {
			return nil, errorsmod.Wrap(types.ErrUnauthorized, "only active stakers or operations committee may approve/disapprove this initiative")
		}
	}

	// This is a staker approval - store the criteria votes and comments
	// In a full implementation, this would track individual staker approvals
	// For now, we just validate that the approver is a staker

	// If disapproved, mark initiative as rejected/abandoned
	if !msg.Approved {
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED

		// Return any self-assign bond — disapproval is not an upheld
		// challenge; the assignee simply doesn't get paid.
		if err := k.Keeper.ReleaseSelfAssignBond(ctx, &initiative); err != nil {
			return nil, errorsmod.Wrap(err, "failed to release self-assign bond")
		}

		// Return budget to project (skip for permissionless — no pre-allocated budget)
		if !project.Permissionless {
			if err := k.Keeper.ReturnBudget(ctx, initiative.ProjectId, DerefInt(initiative.Budget)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to return budget")
			}
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
