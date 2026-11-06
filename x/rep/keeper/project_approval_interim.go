package keeper

import (
	"context"
	"fmt"
	"strings"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Project review interims.
//
// A budget-backed project proposal (MsgProposeProject with a DREAM or SPARK
// request) asks the Operations Committee of the council it is pointed at to
// review it, and the spec pays that review through an interim:
// PROJECT_APPROVAL (STANDARD budget), or BUDGET_REVIEW (COMPLEX budget) when
// the requested DREAM is at or above params.budget_review_threshold. The
// interim is system-owned end to end:
//
//   - opened here at proposal time, assigned to every current committee
//     member, with the project as its reference, the project's expiry as its
//     deadline and the id stored on project.approval_interim_id;
//   - paid and COMPLETED by MsgApproveProjectBudget, the decision that is the
//     committee's work (settleProjectApprovalInterim) — paid only to the
//     assignees who took part in that decision, never to the proposer;
//   - EXPIRED unpaid when the proposal is cancelled or expires before a
//     decision (cancelProjectApprovalInterim);
//   - never created, submitted, approved or completed by a member
//     (IsSystemOnlyInterimType / IsProjectReviewInterimType); an assignee who
//     abandons it gives up only their own seat (MsgAbandonInterim).
//
// None of it is allowed to fail the member's or the committee's own
// transaction: the proposal and the approval are the actions, the interim is
// the committee's pay for them. Failures are logged and emitted as events.

// projectReviewReferenceType is the Interim.reference_type of a project
// review interim; reference_id is the project id.
const projectReviewReferenceType = "project"

// projectReviewCommittee is the Interim.committee tag of a project review
// interim: the Operations Committee of the council the project is pointed
// at ("commons_operations", "technical_operations", ...).
func projectReviewCommittee(council string) string {
	return strings.ToLower(council) + "_operations"
}

// openProjectApprovalInterim opens the review interim for a freshly proposed
// budget-backed project and records its id on the project. It never returns
// an error: a committee with no members skips the interim
// (project_approval_interim_skipped) and any failure is logged and emitted
// (project_approval_interim_failed), because the proposal is the member's
// action and the review pay is the committee's concern.
func (k Keeper) openProjectApprovalInterim(ctx context.Context, projectID uint64, params types.Params) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	emitSkipped := func(reason string) {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"project_approval_interim_skipped",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("reason", reason),
		))
	}
	emitFailed := func(err error) {
		sdkCtx.Logger().Error("failed to open project review interim",
			"project_id", projectID, "error", err)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"project_approval_interim_failed",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("error", err.Error()),
		))
	}

	if k.commonsKeeper == nil {
		emitSkipped("commons keeper not wired")
		return
	}
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		emitFailed(err)
		return
	}
	// The committee that approves the project (ApproveProject resolves
	// authority against project.council) is the one that reviews it and is
	// paid for the review.
	assignees, err := k.commonsKeeper.ListCommitteeMembers(ctx, project.Council, "operations")
	if err != nil {
		emitFailed(fmt.Errorf("list Operations Committee members of council %q: %w", project.Council, err))
		return
	}
	if len(assignees) == 0 {
		emitSkipped(fmt.Sprintf("Operations Committee of council %q has no members", project.Council))
		return
	}

	interimType := types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL
	complexity := types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD
	requestedBudget := DerefInt(project.RequestedBudget)
	if !params.BudgetReviewThreshold.IsNil() && requestedBudget.GTE(params.BudgetReviewThreshold) {
		interimType = types.InterimType_INTERIM_TYPE_BUDGET_REVIEW
		complexity = types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX
	}
	// The review lives as long as the proposal (REP-E-205): a decision
	// anywhere in the proposal window pays. The interim_deadline_epochs
	// default only backs a project with no expiry on record.
	deadline := project.ExpiryBlockHeight
	if deadline <= sdkCtx.BlockHeight() {
		deadline = sdkCtx.BlockHeight() + params.InterimDeadlineEpochs*params.EpochBlocks
	}

	// All or nothing: a half-written interim (stored but not indexed, or
	// indexed but not recorded on the project) would be unreachable by the
	// settlement below and sit in the pending sweep until it expires.
	cacheCtx, writeCache := sdkCtx.CacheContext()
	interimID, err := k.CreateSystemInterimWork(cacheCtx, interimType, assignees, projectReviewCommittee(project.Council),
		projectID, projectReviewReferenceType, complexity, deadline)
	if err != nil {
		emitFailed(err)
		return
	}
	project.ApprovalInterimId = interimID
	if err := k.UpdateProject(cacheCtx, project); err != nil {
		emitFailed(err)
		return
	}
	writeCache()

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"project_approval_interim_opened",
		sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
		sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interimID)),
		sdk.NewAttribute("type", interimType.String()),
		sdk.NewAttribute("assignees", fmt.Sprintf("%d", len(assignees))),
	))
}

// liveProjectApprovalInterim returns the project's review interim if one was
// opened and is still PENDING or IN_PROGRESS. A missing or finalized interim
// (already paid, expired by the deadline sweep, or abandoned) returns ok=false.
func (k Keeper) liveProjectApprovalInterim(ctx context.Context, project types.Project) (types.Interim, bool, error) {
	if project.ApprovalInterimId == 0 {
		return types.Interim{}, false, nil
	}
	interim, err := k.GetInterim(ctx, project.ApprovalInterimId)
	if err != nil {
		return types.Interim{}, false, err
	}
	if interim.Status != types.InterimStatus_INTERIM_STATUS_PENDING &&
		interim.Status != types.InterimStatus_INTERIM_STATUS_IN_PROGRESS {
		return types.Interim{}, false, nil
	}
	return interim, true, nil
}

// projectReviewPayees returns the assignees of a project's review interim
// who are paid for the approval (REP-E-206): the active assignees who took
// part in the decision, never the project's own creator.
//
// Taking part means having voted (any option) on the council or committee
// proposal that executed the approval, read through the executing-proposal
// context x/commons sets, through every recent alias of the assignee; on a
// secret ballot commons reports every member as having voted. An approval
// signed by a single committee member (small budget, no proposal) is that
// member's review alone. An approval with no proposal and no assignee as
// signer (the gov authority) pays nobody. An unvoted seat is unpaid and its
// share is not redistributed.
func (k Keeper) projectReviewPayees(ctx context.Context, project types.Project, interim types.Interim) []sdk.AccAddress {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	skip := func(assignee, reason string) {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"interim_assignee_skipped",
			sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interim.Id)),
			sdk.NewAttribute("assignee", assignee),
			sdk.NewAttribute("reason", reason),
		))
	}
	proposalID, viaProposal := commontypes.ExecutingProposalID(ctx)
	payees := make([]sdk.AccAddress, 0, len(interim.Assignees))
	for _, addr := range k.payableInterimAssignees(ctx, interim) {
		assignee := addr.String()
		if k.SameMember(ctx, assignee, project.Creator) {
			skip(assignee, "proposed the project")
			continue
		}
		voted := false
		switch {
		case viaProposal:
			for _, a := range k.RecentAddressesOf(ctx, assignee) {
				if k.commonsKeeper.HasVoted(ctx, proposalID, a) {
					voted = true
					break
				}
			}
		default:
			voted = k.CanActAs(ctx, project.ApprovedBy, assignee)
		}
		if !voted {
			skip(assignee, "did not take part in the approval")
			continue
		}
		payees = append(payees, addr)
	}
	return payees
}

// settleProjectApprovalInterim pays and completes a project's review interim
// once the project is approved. The approval is the committee's work, so this
// is the one payout path for PROJECT_APPROVAL / BUDGET_REVIEW interims; it
// pays the assignees who took part in the approval (projectReviewPayees)
// through the same payout every interim uses (season cap, treasury-first,
// active-member filter). With nobody to pay the interim ends EXPIRED. Nothing
// here fails the approval: a payout refused by the season cap or by a store
// error is logged and emitted (project_approval_interim_settlement_failed)
// and the interim is left live, where the deadline sweep expires it unpaid.
func (k Keeper) settleProjectApprovalInterim(ctx context.Context, project types.Project) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	emitFailed := func(interimID uint64, err error) {
		sdkCtx.Logger().Error("failed to settle project review interim",
			"project_id", project.Id, "interim_id", interimID, "error", err)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"project_approval_interim_settlement_failed",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", project.Id)),
			sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interimID)),
			sdk.NewAttribute("error", err.Error()),
		))
	}

	interim, ok, err := k.liveProjectApprovalInterim(ctx, project)
	if err != nil {
		emitFailed(project.ApprovalInterimId, err)
		return
	}
	if !ok {
		return
	}

	// Pay and complete together or not at all: a payout that charged the
	// season cap and paid some assignees before failing must not persist.
	cacheCtx, writeCache := sdkCtx.CacheContext()
	payees := k.projectReviewPayees(cacheCtx, project, interim)
	if err := k.payInterimAssigneesTo(cacheCtx, interim, payees); err != nil {
		emitFailed(interim.Id, err)
		return
	}
	if len(payees) == 0 {
		interim.Status = types.InterimStatus_INTERIM_STATUS_EXPIRED
		interim.CompletionNotes = fmt.Sprintf("project %d approved; no assignee took part in the approval", project.Id)
	} else {
		interim.Status = types.InterimStatus_INTERIM_STATUS_COMPLETED
		interim.CompletedAt = sdkCtx.BlockTime().Unix()
		interim.CompletionNotes = fmt.Sprintf("project %d approved", project.Id)
	}
	if err := k.UpdateInterim(cacheCtx, interim); err != nil {
		emitFailed(interim.Id, err)
		return
	}
	writeCache()

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"project_approval_interim_settled",
		sdk.NewAttribute("project_id", fmt.Sprintf("%d", project.Id)),
		sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interim.Id)),
		sdk.NewAttribute("budget", DerefInt(interim.Budget).String()),
		sdk.NewAttribute("paid_assignees", fmt.Sprintf("%d", len(payees))),
	))
}

// cancelProjectApprovalInterim expires a project's review interim unpaid
// because the proposal ended without an approval (cancelled by its creator or
// a council body, or expired by the EndBlocker). A review that reached no
// decision earns nothing. No-op when no live interim exists. Store errors are
// returned: the callers are the project's own terminal transition, which
// should not leave a live interim pointing at a dead proposal.
func (k Keeper) cancelProjectApprovalInterim(ctx context.Context, project types.Project, reason string) error {
	interim, ok, err := k.liveProjectApprovalInterim(ctx, project)
	if err != nil {
		return fmt.Errorf("failed to load review interim %d of project %d: %w", project.ApprovalInterimId, project.Id, err)
	}
	if !ok {
		return nil
	}
	interim.Status = types.InterimStatus_INTERIM_STATUS_EXPIRED
	interim.CompletionNotes = reason
	if err := k.UpdateInterim(ctx, interim); err != nil {
		return fmt.Errorf("failed to expire review interim %d of project %d: %w", interim.Id, project.Id, err)
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"project_approval_interim_cancelled",
		sdk.NewAttribute("project_id", fmt.Sprintf("%d", project.Id)),
		sdk.NewAttribute("interim_id", fmt.Sprintf("%d", interim.Id)),
		sdk.NewAttribute("reason", reason),
	))
	return nil
}
