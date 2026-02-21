package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestCreateProject(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	// Test: Create project
	proposedBudget := math.NewInt(5000)
	proposedSpark := math.NewInt(500)

	projectID, err := k.CreateProject(
		ctx,
		creator,
		"DeFi Dashboard",
		"Build a decentralized finance dashboard with real-time data",
		[]string{"frontend", "web3", "analytics"},
		types.ProjectCategory_PROJECT_CATEGORY_ECOSYSTEM,
		"technical",
		proposedBudget,
		proposedSpark,
	)
	require.NoError(t, err)

	// Verify project
	project, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, "DeFi Dashboard", project.Name)
	require.Equal(t, creator.String(), project.Creator)
	require.Equal(t, "technical", project.Council)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, project.Status)
	require.Equal(t, []string{"frontend", "web3", "analytics"}, project.Tags)
}

func TestApproveProject(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	projectID, _ := k.CreateProject(
		ctx,
		creator,
		"Test Project",
		"Description",
		[]string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(5000),
		math.NewInt(500),
	)

	// Test: Approve project
	approver := sdk.AccAddress([]byte("approver"))
	approvedDream := math.NewInt(4000) // Less than proposed
	approvedSpark := math.NewInt(400)

	err := k.ApproveProject(ctx, projectID, approver, approvedDream, approvedSpark)
	require.NoError(t, err)

	// Verify approval
	project, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, project.Status)
	require.Equal(t, approvedDream.String(), project.ApprovedBudget.String())
	require.Equal(t, approvedSpark.String(), project.ApprovedSpark.String())
	require.Equal(t, math.ZeroInt().String(), project.AllocatedBudget.String())
	require.Equal(t, math.ZeroInt().String(), project.SpentBudget.String())
	require.NotZero(t, project.ApprovedAt)
}

func TestAllocateBudget(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	approvedBudget := math.NewInt(10000)
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_OPERATIONS, "technical", approvedBudget, math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), approvedBudget, math.NewInt(1000))

	// Test: Allocate budget
	allocAmount := math.NewInt(3000)
	err := k.AllocateBudget(ctx, projectID, allocAmount)
	require.NoError(t, err)

	// Verify allocation
	project, _ := k.GetProject(ctx, projectID)
	require.Equal(t, allocAmount.String(), project.AllocatedBudget.String())

	// Test: Allocate more
	err = k.AllocateBudget(ctx, projectID, math.NewInt(2000))
	require.NoError(t, err)

	project, _ = k.GetProject(ctx, projectID)
	require.Equal(t, math.NewInt(5000).String(), project.AllocatedBudget.String())

	// Test: Over-allocation (should fail)
	err = k.AllocateBudget(ctx, projectID, math.NewInt(10000)) // Would exceed approved budget
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient budget")
}

func TestReturnBudget(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	approvedBudget := math.NewInt(10000)
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_RESEARCH, "technical", approvedBudget, math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), approvedBudget, math.NewInt(1000))

	// Allocate budget
	allocAmount := math.NewInt(5000)
	k.AllocateBudget(ctx, projectID, allocAmount)

	// Test: Return budget
	returnAmount := math.NewInt(2000)
	err := k.ReturnBudget(ctx, projectID, returnAmount)
	require.NoError(t, err)

	// Verify return
	project, _ := k.GetProject(ctx, projectID)
	expectedAllocated := allocAmount.Sub(returnAmount)
	require.Equal(t, expectedAllocated.String(), project.AllocatedBudget.String())
}

func TestSpendBudget(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	approvedBudget := math.NewInt(10000)
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_CREATIVE, "technical", approvedBudget, math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), approvedBudget, math.NewInt(1000))

	// Allocate budget
	k.AllocateBudget(ctx, projectID, math.NewInt(5000))

	// Test: Spend budget
	spendAmount := math.NewInt(3000)
	err := k.SpendBudget(ctx, projectID, spendAmount)
	require.NoError(t, err)

	// Verify spending
	project, _ := k.GetProject(ctx, projectID)
	require.Equal(t, spendAmount.String(), project.SpentBudget.String())
	require.Equal(t, math.NewInt(5000).String(), project.AllocatedBudget.String()) // allocated remains same (not returned)
}

func TestCancelProject(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	// Test: Cancel project
	err := k.CancelProject(ctx, projectID, "No longer needed")
	require.NoError(t, err)

	// Verify cancellation
	project, _ := k.GetProject(ctx, projectID)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_CANCELLED, project.Status)
}

func TestProjectInvalidStateTransitions(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})

	// Test: Cannot allocate budget from proposed project
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_ECOSYSTEM, "technical", math.NewInt(10000), math.NewInt(1000))

	err := k.AllocateBudget(ctx, projectID, math.NewInt(1000))
	require.Error(t, err) // Should fail - project not active

	// Approve project
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	// Test: Cannot approve again
	err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	require.Error(t, err)
	require.Contains(t, err.Error(), "project must be in PROPOSED status")

	// Cancel project
	k.CancelProject(ctx, projectID, "reason")

	// Test: Cannot allocate from cancelled project
	err = k.AllocateBudget(ctx, projectID, math.NewInt(1000))
	require.Error(t, err)
}

func TestCompleteProject(t *testing.T) {
	t.Run("active project can be completed", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
		})

		// Create and approve project
		approvedBudget := math.NewInt(10000)
		projectID, err := k.CreateProject(ctx, creator, "Zenith Project", "A test project for completion", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", approvedBudget, math.NewInt(1000))
		require.NoError(t, err)
		err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), approvedBudget, math.NewInt(1000))
		require.NoError(t, err)

		// Allocate and spend some budget
		k.AllocateBudget(ctx, projectID, math.NewInt(5000))
		spendAmount := math.NewInt(3000)
		err = k.SpendBudget(ctx, projectID, spendAmount)
		require.NoError(t, err)

		// Complete the project
		err = k.CompleteProject(ctx, projectID)
		require.NoError(t, err)

		// Verify status is COMPLETED
		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_COMPLETED, project.Status)

		// Verify SpentBudget is preserved
		require.Equal(t, spendAmount.String(), project.SpentBudget.String(),
			"spent budget should be preserved after completion")
	})

	t.Run("non-active project cannot be completed", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
		})

		// Create a project but do NOT approve it (status is PROPOSED)
		projectID, err := k.CreateProject(ctx, creator, "Aurora Project", "A proposed project", []string{"frontend"}, types.ProjectCategory_PROJECT_CATEGORY_ECOSYSTEM, "technical", math.NewInt(5000), math.NewInt(500))
		require.NoError(t, err)

		// Attempt to complete a PROPOSED project - should fail
		err = k.CompleteProject(ctx, projectID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "ACTIVE")

		// Now approve and cancel the project
		err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(5000), math.NewInt(500))
		require.NoError(t, err)
		err = k.CancelProject(ctx, projectID, "no longer needed")
		require.NoError(t, err)

		// Attempt to complete a CANCELLED project - should fail
		err = k.CompleteProject(ctx, projectID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "ACTIVE")
	})

	t.Run("completed project preserves spent budget", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
		})

		// Create, approve, allocate, spend, and complete
		approvedBudget := math.NewInt(20000)
		projectID, err := k.CreateProject(ctx, creator, "Nova Project", "Complete workflow test", []string{"backend", "frontend"}, types.ProjectCategory_PROJECT_CATEGORY_CREATIVE, "technical", approvedBudget, math.NewInt(2000))
		require.NoError(t, err)
		err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), approvedBudget, math.NewInt(2000))
		require.NoError(t, err)

		// Allocate and spend in multiple steps
		err = k.AllocateBudget(ctx, projectID, math.NewInt(8000))
		require.NoError(t, err)
		err = k.SpendBudget(ctx, projectID, math.NewInt(3000))
		require.NoError(t, err)
		err = k.SpendBudget(ctx, projectID, math.NewInt(2500))
		require.NoError(t, err)

		totalSpent := math.NewInt(5500) // 3000 + 2500

		// Complete the project
		err = k.CompleteProject(ctx, projectID)
		require.NoError(t, err)

		// Verify status and preserved budget
		project, err := k.GetProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_COMPLETED, project.Status)
		require.Equal(t, totalSpent.String(), project.SpentBudget.String(),
			"spent budget should reflect cumulative spending")
		require.Equal(t, math.NewInt(8000).String(), project.AllocatedBudget.String(),
			"allocated budget should be preserved")
	})
}
