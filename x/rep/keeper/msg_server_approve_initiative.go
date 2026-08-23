package keeper

import (
	"context"
	"fmt"
	"slices"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ApproveInitiative records one reviewer's verdict on submitted work.
//
// Approval is advisory: it is recorded on the initiative so the endorsement is
// visible, but nothing consults the list — conviction remains the only gate on
// completion. Disapproval is not advisory, and its authority is split:
//
//   - The Operations Committee abandons the initiative outright. It is the same
//     standing that already lets the committee cancel an unstarted initiative.
//   - A staker casts a weighted vote. The initiative is abandoned only once
//     stakers holding InitiativeDisapprovalThreshold of the DREAM staked on it
//     have voted against. Until then the disapproval is recorded and the work
//     stands.
//
// The weighting is what makes staker disapproval safe to expose. A single
// minimum stake used to be enough to destroy submitted work outright, which
// made the cost of griefing an assignee's payout a refundable deposit.
func (k msgServer) ApproveInitiative(ctx context.Context, msg *types.MsgApproveInitiative) (*types.MsgApproveInitiativeResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	initiative, err := k.Keeper.GetInitiative(ctx, msg.InitiativeId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get initiative")
	}

	// SUBMITTED and IN_REVIEW are both open to review. IN_REVIEW is the review
	// period: excluding it left well-backed work reviewable for as little as
	// one block, since the EndBlocker transitions an initiative out of
	// SUBMITTED as soon as its conviction gates are met.
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED &&
		initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW {
		return nil, errorsmod.Wrapf(types.ErrInvalidInitiativeStatus,
			"initiative must be in SUBMITTED or IN_REVIEW status, got %s", initiative.Status)
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

	// Standing: an active stake on this initiative, or a seat on the Commons
	// Operations Committee. Both are resolved up front because disapproval
	// branches on which one the caller holds.
	stakes, err := k.Keeper.GetInitiativeStakes(ctx, msg.InitiativeId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get initiative stakes")
	}
	isStaker := slices.ContainsFunc(stakes, func(s types.Stake) bool { return s.Staker == msg.Creator })

	isOps, err := k.Keeper.commonsKeeper.IsCommitteeMember(ctx, sdk.AccAddress(creatorBytes), "commons", "operations")
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check operations committee membership")
	}
	if !isStaker && !isOps {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only active stakers or operations committee may approve/disapprove this initiative")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// abandon terminates the initiative without penalty to the assignee.
	// Disapproval is not an upheld challenge: the self-assign bond comes back
	// and the reserved budget returns to the project, the assignee simply
	// isn't paid.
	abandon := func(reason string, weight string) error {
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED
		if err := k.Keeper.ReleaseSelfAssignBond(ctx, &initiative); err != nil {
			return errorsmod.Wrap(err, "failed to release self-assign bond")
		}
		// Permissionless projects hold no pre-allocated budget to return.
		if !project.Permissionless {
			if err := k.Keeper.ReturnBudget(ctx, initiative.ProjectId, DerefInt(initiative.Budget)); err != nil {
				return errorsmod.Wrap(err, "failed to return budget")
			}
		}
		if err := k.Keeper.UpdateInitiative(ctx, initiative); err != nil {
			return errorsmod.Wrap(err, "failed to update initiative")
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"initiative_disapproved",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", msg.InitiativeId)),
			sdk.NewAttribute("resolved_by", reason),
			sdk.NewAttribute("disapproving_stake", weight),
		))
		return nil
	}

	if msg.Approved {
		// Approval is advisory: recorded and displayed, consulted by nothing.
		// Re-approving is idempotent — appending on every signature let one
		// staker pad the list without bound for the price of repeated gas.
		if !slices.Contains(initiative.Approvals, msg.Creator) {
			initiative.Approvals = append(initiative.Approvals, msg.Creator)
		}
		if err := k.Keeper.UpdateInitiative(ctx, initiative); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update initiative")
		}
		return &types.MsgApproveInitiativeResponse{}, nil
	}

	initiative.Approvals = removeAddress(initiative.Approvals, msg.Creator)

	// Disapproval is committee-only. The stake-weighted staker veto that used
	// to live here is retired: stakers are paid on completion, so the veto was
	// held by exactly the people who lost money using it, and backing a proposal
	// is a different judgement from evaluating a deliverable. Quality is the
	// bonded reviewer's question now (see Initiative Review in the spec), and
	// conviction is the stakers'.
	//
	// Stakers keep a real exit regardless: conviction is recomputed from live
	// stake records and completion needs both the total and external thresholds,
	// so withdrawing a stake blocks completion within about one refresh
	// interval. Voting with your feet is a functioning veto, not a gesture.
	if !isOps {
		return nil, errorsmod.Wrap(types.ErrUnauthorized,
			"only the Operations Committee may disapprove; stakers withdraw their stake instead")
	}
	if err := abandon("operations_committee", "0"); err != nil {
		return nil, err
	}
	return &types.MsgApproveInitiativeResponse{}, nil
}

// removeAddress returns addrs without addr, preserving order. Returns the input
// untouched when addr is absent, which is the common case.
func removeAddress(addrs []string, addr string) []string {
	idx := slices.Index(addrs, addr)
	if idx < 0 {
		return addrs
	}
	return slices.Delete(slices.Clone(addrs), idx, idx+1)
}
