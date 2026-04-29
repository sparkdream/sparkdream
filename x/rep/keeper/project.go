package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
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

	// Create project
	project := types.Project{
		Id:              projectID,
		Name:            name,
		Description:     description,
		Creator:         creator.String(),
		Tags:            tags,
		Category:        category,
		Council:         council,
		ApprovedBudget:  PtrInt(math.ZeroInt()),
		AllocatedBudget: PtrInt(math.ZeroInt()),
		SpentBudget:     PtrInt(math.ZeroInt()),
		ApprovedSpark:   PtrInt(math.ZeroInt()),
		SpentSpark:      PtrInt(math.ZeroInt()),
		Status:          status,
		Permissionless:  permissionless,
	}

	// Store project
	if err := k.Project.Set(ctx, projectID, project); err != nil {
		return 0, fmt.Errorf("failed to store project: %w", err)
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
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

// ApproveProject approves a project with specified budget
func (k Keeper) ApproveProject(
	ctx context.Context,
	projectID uint64,
	approver sdk.AccAddress,
	approvedBudget, approvedSpark math.Int,
) error {
	// Get project
	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return err
	}

	// Validate status
	if project.Status != types.ProjectStatus_PROJECT_STATUS_PROPOSED {
		return fmt.Errorf("project must be in PROPOSED status, got %s", project.Status.String())
	}

	// Validate approver has authority (Operations Committee member or council policy address).
	// commonsKeeper is required: a nil keeper is a configuration error, not an authorization bypass.
	if k.commonsKeeper == nil {
		return fmt.Errorf("commons keeper not wired; cannot approve project")
	}
	isCommittee := k.IsOperationsCommittee(ctx, approver)
	isCouncilAuth := k.commonsKeeper.IsCouncilAuthorized(ctx, approver.String(), project.Council, "operations")
	if !isCommittee && !isCouncilAuth {
		return fmt.Errorf("approver %s is not authorized (requires Operations Committee or council proposal)", approver.String())
	}

	// Update project
	project.ApprovedBudget = PtrInt(approvedBudget)
	project.ApprovedSpark = PtrInt(approvedSpark)
	project.Status = types.ProjectStatus_PROJECT_STATUS_ACTIVE
	project.ApprovedBy = approver.String()
	project.ApprovedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	// Store updated project
	if err := k.UpdateProject(ctx, project); err != nil {
		return err
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
	project.Status = types.ProjectStatus_PROJECT_STATUS_CANCELLED

	// Store updated project
	if err := k.UpdateProject(ctx, project); err != nil {
		return err
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
	project.Status = types.ProjectStatus_PROJECT_STATUS_COMPLETED

	// Store updated project
	if err := k.UpdateProject(ctx, project); err != nil {
		return err
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
