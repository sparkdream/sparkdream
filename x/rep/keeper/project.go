package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateProject creates a new project proposal (budget-backed) or a permissionless project.
// When permissionless is true, the project skips committee approval and becomes ACTIVE immediately.
func (k Keeper) CreateProject(
	ctx context.Context,
	creator sdk.AccAddress,
	name, description string,
	tags []string,
	category types.ProjectCategory,
	council string,
	requestedBudget, requestedSpark math.Int,
	permissionless bool,
) (uint64, error) {
	// Get next project ID
	projectID, err := k.ProjectSeq.Next(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get next project ID: %w", err)
	}

	status := types.ProjectStatus_PROJECT_STATUS_PROPOSED
	if permissionless {
		status = types.ProjectStatus_PROJECT_STATUS_ACTIVE
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Non-permissionless projects sit in PROPOSED until a committee/council
	// approves them. Stamp an absolute expiry deadline so the EndBlocker can
	// reap stale proposals. Permissionless projects skip approval entirely
	// (status = ACTIVE on creation) and have no expiry.
	var expiryHeight int64
	if !permissionless {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to get params: %w", err)
		}
		expiryHeight = sdkCtx.BlockHeight() + params.ProposedProjectExpiryBlocks
	}

	// Create project
	project := types.Project{
		Id:                projectID,
		Name:              name,
		Description:       description,
		Creator:           creator.String(),
		Tags:              tags,
		Category:          category,
		Council:           council,
		ApprovedBudget:    PtrInt(math.ZeroInt()),
		AllocatedBudget:   PtrInt(math.ZeroInt()),
		SpentBudget:       PtrInt(math.ZeroInt()),
		ApprovedSpark:     PtrInt(math.ZeroInt()),
		SpentSpark:        PtrInt(math.ZeroInt()),
		Status:            status,
		Permissionless:    permissionless,
		ExpiryBlockHeight: expiryHeight,
	}

	// Store project
	if err := k.Project.Set(ctx, projectID, project); err != nil {
		return 0, fmt.Errorf("failed to store project: %w", err)
	}

	// Maintain the by-status index so the EndBlocker expiry sweep can find
	// PROPOSED projects in O(expired) instead of scanning the full table.
	if err := k.AddProjectToStatusIndex(ctx, project); err != nil {
		return 0, fmt.Errorf("failed to index project by status: %w", err)
	}

	// Emit event
	eventType := "project_proposed"
	if permissionless {
		eventType = "project_created"
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			eventType,
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("creator", creator.String()),
			sdk.NewAttribute("council", council),
			sdk.NewAttribute("permissionless", fmt.Sprintf("%t", permissionless)),
			sdk.NewAttribute("requested_budget", requestedBudget.String()),
			sdk.NewAttribute("requested_spark", requestedSpark.String()),
		),
	)

	return projectID, nil
}

// GetProject retrieves a project by ID
func (k Keeper) GetProject(ctx context.Context, projectID uint64) (types.Project, error) {
	project, err := k.Project.Get(ctx, projectID)
	if err != nil {
		if err == collections.ErrNotFound {
			return types.Project{}, fmt.Errorf("project %d not found", projectID)
		}
		return types.Project{}, err
	}
	return project, nil
}

// UpdateProject updates an existing project
func (k Keeper) UpdateProject(ctx context.Context, project types.Project) error {
	return k.Project.Set(ctx, project.Id, project)
}

// ApproveProject approves a project with specified budget.
//
// Authorization is tier-aware and locked to the picked council:
//   - budget ≤ params.LargeProjectBudgetThreshold: an individual member of the
//     picked council's operations committee is sufficient.
//   - budget > params.LargeProjectBudgetThreshold: requires a passed council or
//     operations-committee proposal (executor's policy address) or the gov
//     authority — individual committee members cannot approve large budgets.
//
// The global Technical / Commons Operations Committee fallback that previously
// let any operations-committee member approve any project (regardless of which
// council the project was pointed at) has been removed: cross-council
// unilateral approval is no longer permitted.
func (k Keeper) ApproveProject(
	ctx context.Context,
	projectID uint64,
	approver sdk.AccAddress,
	approvedBudget, approvedSpark math.Int,
) error {
	// Get project. Wrap the collections-level not-found so callers can match
	// on types.ErrProjectNotFound (and external API errors stay stable).
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return errorsmod.Wrap(types.ErrProjectNotFound, err.Error())
	}

	// Validate status
	if project.Status != types.ProjectStatus_PROJECT_STATUS_PROPOSED {
		return fmt.Errorf("project must be in PROPOSED status, got %s", project.Status.String())
	}

	// Authorization is required: commonsKeeper must be wired. Treat a nil
	// keeper as a configuration error, not an authorization bypass.
	if k.commonsKeeper == nil {
		return fmt.Errorf("commons keeper not wired; cannot approve project")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	approverStr, err := k.addressCodec.BytesToString(approver)
	if err != nil {
		return fmt.Errorf("invalid approver address: %w", err)
	}

	isMember, _ := k.commonsKeeper.IsCommitteeMember(ctx, approver, project.Council, "operations")
	if approvedBudget.GT(params.LargeProjectBudgetThreshold) {
		// Large budget: a personal committee member is not enough. When a
		// council/committee proposal executes, the message's approver is the
		// group's policy address (not a person) — so reject when the approver
		// is a plain member, then accept only if they're council-authorized.
		if isMember {
			return errorsmod.Wrapf(types.ErrLargeProjectNeedsCouncil,
				"budget %s exceeds threshold %s; individual committee members cannot approve — submit via council proposal",
				approvedBudget.String(), params.LargeProjectBudgetThreshold.String())
		}
		if !k.commonsKeeper.IsCouncilAuthorized(ctx, approverStr, project.Council, "operations") {
			return errorsmod.Wrapf(types.ErrLargeProjectNeedsCouncil,
				"budget %s exceeds threshold %s; submit via council proposal",
				approvedBudget.String(), params.LargeProjectBudgetThreshold.String())
		}
	} else {
		// Small budget: an individual member of the picked council's
		// operations committee suffices.
		if !isMember {
			return errorsmod.Wrapf(types.ErrUnauthorized,
				"approver must be a member of the Operations Committee for council '%s'",
				project.Council)
		}
	}

	// Update project
	oldStatus := project.Status
	project.ApprovedBudget = PtrInt(approvedBudget)
	project.ApprovedSpark = PtrInt(approvedSpark)
	project.Status = types.ProjectStatus_PROJECT_STATUS_ACTIVE
	project.ApprovedBy = approver.String()
	project.ApprovedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	// Project is no longer eligible for EndBlocker expiry — clear the deadline
	// so a stale value can't accidentally be acted on if the status is ever
	// reverted (defense-in-depth; current code has no such revert path).
	project.ExpiryBlockHeight = 0

	// Store updated project
	if err := k.UpdateProject(ctx, project); err != nil {
		return err
	}

	// Stakes placed while the project was PROPOSED hold reward-debt snapshots
	// from their join-time accumulator. Rebase them to the current one so
	// accrual starts from approval — without this, those stakes would harvest
	// the whole PROPOSED-window growth retroactively at completion.
	if err := k.rebaseProjectStakeDebts(ctx, projectID); err != nil {
		return err
	}

	// Shift the by-status index entry (PROPOSED -> ACTIVE) so the expiry sweep
	// no longer considers this project.
	if err := k.UpdateProjectStatusIndex(ctx, oldStatus, project.Status, project.Id); err != nil {
		return fmt.Errorf("failed to update project status index: %w", err)
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"project_approved",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("approver", approver.String()),
			sdk.NewAttribute("approved_budget", approvedBudget.String()),
			sdk.NewAttribute("approved_spark", approvedSpark.String()),
		),
	)

	return nil
}

// CancelProject cancels a project
func (k Keeper) CancelProject(ctx context.Context, projectID uint64, reason string) error {
	// Get project
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return err
	}

	// Validate status - a project can only be cancelled from a live state
	// (PROPOSED or ACTIVE). COMPLETED, CANCELLED, and EXPIRED are terminal, so
	// re-cancelling them is rejected (cancelling an EXPIRED project would
	// otherwise relabel its audit trail and lose the "expired through inaction"
	// signal).
	if project.Status == types.ProjectStatus_PROJECT_STATUS_COMPLETED ||
		project.Status == types.ProjectStatus_PROJECT_STATUS_CANCELLED ||
		project.Status == types.ProjectStatus_PROJECT_STATUS_EXPIRED {
		return fmt.Errorf("project is already in a terminal state (%s)", project.Status.String())
	}

	// Cascade: retire every non-terminal initiative under this project. OPEN
	// listings, assigned work, submitted deliverables, and challenged work all
	// move to CANCELLED with their reserved budget returned, self-assign bonds
	// released, and any active challenge voided (refunding the challenger).
	// Run before flipping the project status so each ReturnBudget applies
	// against the still-live project.
	if err := k.cancelInitiativesForProjectCancel(ctx, projectID, reason); err != nil {
		return fmt.Errorf("failed to cascade-cancel initiatives: %w", err)
	}

	// Re-read after the cascade: cancelling the open initiatives writes the
	// project's AllocatedBudget back down, and we must not clobber that with
	// the pre-cascade snapshot when we persist the status change below.
	project, err = k.GetProject(ctx, projectID)
	if err != nil {
		return err
	}

	// Settle the project's stakes while the project is still ACTIVE: past this
	// point the frozen branch of settleStake deliberately pays nothing, so
	// anything accrued but unclaimed at cancellation would be stranded forever.
	if err := k.settleProjectStakes(ctx, projectID); err != nil {
		return err
	}

	// Update project
	oldStatus := project.Status
	project.Status = types.ProjectStatus_PROJECT_STATUS_CANCELLED
	project.ExpiryBlockHeight = 0

	// Store updated project
	if err := k.UpdateProject(ctx, project); err != nil {
		return err
	}

	// Shift the by-status index entry off PROPOSED/ACTIVE so the EndBlocker
	// expiry sweep doesn't act on a cancelled project.
	if err := k.UpdateProjectStatusIndex(ctx, oldStatus, project.Status, project.Id); err != nil {
		return fmt.Errorf("failed to update project status index: %w", err)
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"project_cancelled",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("reason", reason),
		),
	)

	return nil
}

// cancelInitiativesForProjectCancel retires every non-terminal initiative
// belonging to the given project as part of the project's cancellation. IDs are
// collected before mutating so the walk is not invalidated by the writes.
func (k Keeper) cancelInitiativesForProjectCancel(ctx context.Context, projectID uint64, reason string) error {
	var ids []uint64
	err := k.Initiative.Walk(ctx, nil, func(id uint64, initiative types.Initiative) (bool, error) {
		if initiative.ProjectId == projectID && !types.IsInitiativeTerminal(initiative.Status) {
			ids = append(ids, id)
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, id := range ids {
		initiative, gerr := k.GetInitiative(ctx, id)
		if gerr != nil {
			return fmt.Errorf("initiative %d: %w", id, gerr)
		}
		if err := k.terminateInitiativeForProjectCancel(ctx, initiative, reason); err != nil {
			return fmt.Errorf("initiative %d: %w", id, err)
		}
	}
	return nil
}

// terminateInitiativeForProjectCancel moves a single non-terminal initiative to
// CLOSED because its parent project is being cancelled: it voids any active
// challenge (refunding the challenger), returns the reserved budget, releases
// the assignee's self-assign bond, and emits initiative_closed. Safe for OPEN
// initiatives too — they simply carry no challenge, assignee, or bond.
func (k Keeper) terminateInitiativeForProjectCancel(ctx context.Context, initiative types.Initiative, reason string) error {
	// Void any unresolved challenge first — leaving one live would let the
	// EndBlocker later tally a verdict on (and resurrect) a cancelled
	// initiative. This refunds the challenger's stake in full.
	if err := k.voidActiveChallengesForInitiative(ctx, initiative.Id); err != nil {
		return err
	}

	// Return the reserved budget to the still-live project (non-permissionless).
	// Clamp to the project's currently-allocated amount: production never needs
	// this (allocation always covers every non-terminal initiative's budget),
	// but it makes the whole-project cascade resilient to any pre-existing
	// allocation drift rather than aborting the cancel midway.
	project, projErr := k.GetProject(ctx, initiative.ProjectId)
	if projErr == nil && !project.Permissionless {
		toReturn := DerefInt(initiative.Budget)
		if allocated := DerefInt(project.AllocatedBudget); allocated.LT(toReturn) {
			toReturn = allocated
		}
		if toReturn.IsPositive() {
			if err := k.ReturnBudget(ctx, initiative.ProjectId, toReturn); err != nil {
				return fmt.Errorf("failed to return budget: %w", err)
			}
		}
	}

	// Release any self-assign bond (no-op when none is held) — no upheld
	// challenge occurred, so the bond is returned, not burned.
	if err := k.ReleaseSelfAssignBond(ctx, &initiative); err != nil {
		return err
	}

	// Drop any escalation flag, for the same reason the challenge above is
	// voided: the escalation sweep walks its own keyset, not the status index,
	// and would later reject a round on (and resurrect) a closed initiative.
	if err := k.EscalatedReviews.Remove(ctx, initiative.Id); err != nil {
		return err
	}

	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_CLOSED
	if err := k.UpdateInitiative(ctx, initiative); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"initiative_closed",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiative.Id)),
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", initiative.ProjectId)),
			sdk.NewAttribute("reason", reason),
		),
	)
	return nil
}

// ExpireProject transitions a PROPOSED project to EXPIRED. Called only by the
// EndBlocker sweep over PROPOSED projects past their expiry_block_height.
// Idempotent w.r.t. non-PROPOSED status: a project that has been concurrently
// approved/cancelled in the same block is left alone (and the stale index
// entry, if any, will be reconciled on the next status update).
func (k Keeper) ExpireProject(ctx context.Context, projectID uint64) error {
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	if project.Status != types.ProjectStatus_PROJECT_STATUS_PROPOSED {
		return nil
	}

	oldStatus := project.Status
	project.Status = types.ProjectStatus_PROJECT_STATUS_EXPIRED
	project.ExpiryBlockHeight = 0

	if err := k.UpdateProject(ctx, project); err != nil {
		return err
	}
	if err := k.UpdateProjectStatusIndex(ctx, oldStatus, project.Status, project.Id); err != nil {
		return fmt.Errorf("failed to update project status index: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"project_expired",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("creator", project.Creator),
			sdk.NewAttribute("council", project.Council),
		),
	)
	return nil
}

// settleProjectStakes harvests every stake on a project, pays out what it has
// accrued from the seasonal pool, and rebases its reward debt so the stake
// holds no further claim. Called at the project's terminal transitions —
// cancel and complete — while the project is still ACTIVE.
//
// Why at the transition and not lazily: once the project leaves ACTIVE,
// stakeAccruing reports false and the frozen branch of settleStake pays
// nothing — correctly, because the shared accumulator keeps advancing on the
// strength of the still-live stakers and a frozen stake must not credit that
// post-terminal growth. Before this settle existed, everything accrued up to
// the flip was stranded: the stakes stayed on the books, returned their
// principal on unstake, and could never collect their rewards.
//
// Mirrors CompleteInitiative's payout loop: settleStake with forfeit=false,
// since the staker did not choose to exit early and MinStakeDurationSeconds
// is an early-withdrawal penalty, not a settlement gate.
//
// A per-stake mint failure (e.g. the per-epoch mint cap) must not block the
// transition: that stake keeps its old debt and, being frozen afterwards,
// forfeits the pending — no worse than the status quo before this settle —
// while every other staker still gets paid. Logged loudly, since it points at
// cap pressure worth investigating.
func (k Keeper) settleProjectStakes(ctx context.Context, projectID uint64) error {
	stakes, err := k.GetProjectStakes(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to load stakes of project %d for settlement: %w", projectID, err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, stake := range stakes {
		settled, settlement, err := k.settleStake(ctx, stake, stake.Amount, false)
		if err != nil {
			sdkCtx.Logger().Error("failed to settle project stake at terminal transition; pending forfeited",
				"project_id", projectID, "stake_id", stake.Id, "staker", stake.Staker, "error", err)
			continue
		}
		if err := k.Stake.Set(ctx, stake.Id, settled); err != nil {
			return fmt.Errorf("failed to persist settled stake %d: %w", stake.Id, err)
		}
		if settlement.Minted.IsPositive() {
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"project_stake_settled",
					sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
					sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stake.Id)),
					sdk.NewAttribute("staker", stake.Staker),
					sdk.NewAttribute("rewards", settlement.Minted.String()),
				),
			)
		}
	}
	return nil
}

// rebaseProjectStakeDebts re-measures every stake placed while a project was
// PROPOSED against the live seasonal accumulator. Stakes on a non-ACTIVE
// project accrue nothing, but their reward-debt snapshot was taken at the
// (lower) accumulator of join time; without this rebase at approval they
// would harvest the entire PROPOSED-window growth retroactively once the
// project settles. Nothing is owed at this moment — the project has been
// frozen since each stake was placed — so the rebase forfeits nothing.
func (k Keeper) rebaseProjectStakeDebts(ctx context.Context, projectID uint64) error {
	accPerShare, err := k.getSeasonalPoolAccPerShare(ctx)
	if err != nil {
		return fmt.Errorf("failed to read seasonal accumulator for project %d debt rebase: %w", projectID, err)
	}

	stakes, err := k.GetProjectStakes(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to load stakes of project %d for debt rebase: %w", projectID, err)
	}
	for _, stake := range stakes {
		if stake.Amount.IsNil() || !stake.Amount.IsPositive() {
			continue
		}
		debt := math.LegacyNewDecFromInt(stake.Amount).Mul(accPerShare).TruncateInt()
		if stakeRewardDebt(stake).Equal(debt) {
			continue
		}
		stake.RewardDebt = debt
		if err := k.Stake.Set(ctx, stake.Id, stake); err != nil {
			return fmt.Errorf("failed to persist rebased stake %d: %w", stake.Id, err)
		}
	}
	return nil
}

// CompleteProject marks a project as completed and distributes completion bonuses
func (k Keeper) CompleteProject(ctx context.Context, projectID uint64) error {
	// Get project
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return err
	}

	// Validate status - must be ACTIVE
	if project.Status != types.ProjectStatus_PROJECT_STATUS_ACTIVE {
		return fmt.Errorf("project must be in ACTIVE status to complete, got %s", project.Status.String())
	}

	// Calculate final budget (what was actually spent)
	spentBudget := DerefInt(project.SpentBudget)

	// Settle the project's stakes while it is still ACTIVE. CompleteInitiative
	// does the same for every stake it touches; the project's own terminal
	// transition used to skip it, leaving each staker's accrued seasonal
	// rewards claimable by no code path at all — the stakes survived as
	// records, withdrew as principal, and paid nothing.
	if err := k.settleProjectStakes(ctx, projectID); err != nil {
		return err
	}

	// Distribute 5% completion bonus to project stakers, capped and
	// external-only (see DistributeProjectCompletionBonus), then count what
	// was actually minted against the per-season initiative reward cap — the
	// same accounting CompleteInitiative applies to its completer, treasury,
	// bonus and review-fee mints.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	bonusMinted, err := k.DistributeProjectCompletionBonus(ctx, projectID, spentBudget)
	if err != nil {
		sdkCtx.Logger().Debug("failed to distribute project completion bonus", "error", err, "project_id", projectID)
	} else if bonusMinted.IsPositive() {
		if err := k.TrackInitiativeRewardMint(ctx, bonusMinted); err != nil {
			return fmt.Errorf("failed to track project completion bonus mint: %w", err)
		}
	}

	// Update project status
	oldStatus := project.Status
	project.Status = types.ProjectStatus_PROJECT_STATUS_COMPLETED

	// Store updated project
	if err := k.UpdateProject(ctx, project); err != nil {
		return err
	}

	// Keep the by-status index in sync. ACTIVE -> COMPLETED is a noop for the
	// expiry sweep (which only walks PROPOSED) but stays consistent for any
	// future iteration over completed projects.
	if err := k.UpdateProjectStatusIndex(ctx, oldStatus, project.Status, project.Id); err != nil {
		return fmt.Errorf("failed to update project status index: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"project_completed",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("spent_budget", spentBudget.String()),
		),
	)

	return nil
}

// AllocateBudget allocates budget to an initiative from a project
func (k Keeper) AllocateBudget(ctx context.Context, projectID uint64, amount math.Int) error {
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return err
	}

	// Validate status - must be ACTIVE
	if project.Status != types.ProjectStatus_PROJECT_STATUS_ACTIVE {
		return fmt.Errorf("project must be in ACTIVE status, got %s", project.Status.String())
	}

	// Check if enough budget available
	available := DerefInt(project.ApprovedBudget).Sub(DerefInt(project.AllocatedBudget))
	if available.LT(amount) {
		return fmt.Errorf("insufficient budget: available %s, requested %s", available.String(), amount.String())
	}

	// Update allocated budget
	project.AllocatedBudget = PtrInt(DerefInt(project.AllocatedBudget).Add(amount))

	return k.UpdateProject(ctx, project)
}

// SpendBudget marks budget as spent when an initiative is completed
func (k Keeper) SpendBudget(ctx context.Context, projectID uint64, amount math.Int) error {
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return err
	}

	// Update spent budget
	project.SpentBudget = PtrInt(DerefInt(project.SpentBudget).Add(amount))

	return k.UpdateProject(ctx, project)
}

// ReturnBudget returns unspent budget when an initiative is abandoned
func (k Keeper) ReturnBudget(ctx context.Context, projectID uint64, amount math.Int) error {
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return err
	}

	// Guard against driving AllocatedBudget negative (e.g., double-return)
	allocated := DerefInt(project.AllocatedBudget)
	if allocated.LT(amount) {
		return fmt.Errorf("cannot return %s: only %s allocated", amount.String(), allocated.String())
	}

	// Return allocated budget
	project.AllocatedBudget = PtrInt(allocated.Sub(amount))

	return k.UpdateProject(ctx, project)
}
