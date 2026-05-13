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

	// Validate status
	if project.Status == types.ProjectStatus_PROJECT_STATUS_COMPLETED || project.Status == types.ProjectStatus_PROJECT_STATUS_CANCELLED {
		return fmt.Errorf("project already completed or cancelled")
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

	// Distribute 5% completion bonus to project stakers
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.DistributeProjectCompletionBonus(ctx, projectID, spentBudget); err != nil {
		sdkCtx.Logger().Debug("failed to distribute project completion bonus", "error", err, "project_id", projectID)
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
