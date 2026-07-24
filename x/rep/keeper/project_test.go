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
		false,
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
		false,
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
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_OPERATIONS, "technical", approvedBudget, math.NewInt(1000), false)
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
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_RESEARCH, "technical", approvedBudget, math.NewInt(1000), false)
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
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_CREATIVE, "technical", approvedBudget, math.NewInt(1000), false)
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

	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	// Test: Cancel project
	err := k.CancelProject(ctx, projectID, "No longer needed")
	require.NoError(t, err)

	// Verify cancellation
	project, _ := k.GetProject(ctx, projectID)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_CANCELLED, project.Status)

	// Test: cannot re-cancel a terminal (CANCELLED) project.
	err = k.CancelProject(ctx, projectID, "again")
	require.Error(t, err)
	require.Contains(t, err.Error(), "terminal state")
}

func TestCancelProjectRejectsTerminalStates(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	creator := sdk.AccAddress([]byte("creator"))

	// An EXPIRED project (PROPOSED that was never approved before its TTL)
	// cannot be cancelled — that would relabel its audit trail.
	expiredID, err := k.CreateProject(ctx, creator, "Expired", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ExpireProject(ctx, expiredID))

	got, err := k.GetProject(ctx, expiredID)
	require.NoError(t, err)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_EXPIRED, got.Status)

	err = k.CancelProject(ctx, expiredID, "retire")
	require.Error(t, err)
	require.Contains(t, err.Error(), "terminal state")

	// Status is unchanged by the rejected cancel.
	got, err = k.GetProject(ctx, expiredID)
	require.NoError(t, err)
	require.Equal(t, types.ProjectStatus_PROJECT_STATUS_EXPIRED, got.Status)
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
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_ECOSYSTEM, "technical", math.NewInt(10000), math.NewInt(1000), false)

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
		projectID, err := k.CreateProject(ctx, creator, "Zenith Project", "A test project for completion", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", approvedBudget, math.NewInt(1000), false)
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
		projectID, err := k.CreateProject(ctx, creator, "Aurora Project", "A proposed project", []string{"frontend"}, types.ProjectCategory_PROJECT_CATEGORY_ECOSYSTEM, "technical", math.NewInt(5000), math.NewInt(500), false)
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
		projectID, err := k.CreateProject(ctx, creator, "Nova Project", "Complete workflow test", []string{"backend", "frontend"}, types.ProjectCategory_PROJECT_CATEGORY_CREATIVE, "technical", approvedBudget, math.NewInt(2000), false)
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

// TestProjectExpiry covers the EndBlocker-driven TTL on PROPOSED projects:
// CreateProject stamps an expiry deadline, ApproveProject clears it, the
// EndBlocker sweep transitions stale proposals to EXPIRED, and ExpireProject
// is a no-op against non-PROPOSED projects.
func TestProjectExpiry(t *testing.T) {
	memberOf := func(addr sdk.AccAddress) types.Member {
		return types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
		}
	}

	t.Run("CreateProject stamps expiry for non-permissionless, zero for permissionless", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		params, err := k.Params.Get(ctx)
		require.NoError(t, err)
		params.ProposedProjectExpiryBlocks = 500
		require.NoError(t, k.Params.Set(ctx, params))

		creator := sdk.AccAddress([]byte("creator-exp"))
		require.NoError(t, k.Member.Set(ctx, creator.String(), memberOf(creator)))

		// Non-permissionless: expiry = blockHeight + 500.
		budgetID, err := k.CreateProject(ctx, creator, "Budgeted", "desc", []string{"infra"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(100), math.NewInt(10), false)
		require.NoError(t, err)

		p, err := k.GetProject(ctx, budgetID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, p.Status)
		require.Equal(t, sdkCtx.BlockHeight()+500, p.ExpiryBlockHeight,
			"expiry should be current height + ProposedProjectExpiryBlocks")

		// Permissionless: ACTIVE on creation, no expiry to set.
		permID, err := k.CreateProject(ctx, creator, "Open", "desc", []string{"infra"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.ZeroInt(), math.ZeroInt(), true)
		require.NoError(t, err)

		p, err = k.GetProject(ctx, permID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, p.Status)
		require.Zero(t, p.ExpiryBlockHeight, "permissionless projects must not carry an expiry")
	})

	t.Run("ApproveProject clears ExpiryBlockHeight", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator-app"))
		require.NoError(t, k.Member.Set(ctx, creator.String(), memberOf(creator)))

		id, err := k.CreateProject(ctx, creator, "Approve me", "desc", []string{"infra"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(100), math.NewInt(10), false)
		require.NoError(t, err)

		p, err := k.GetProject(ctx, id)
		require.NoError(t, err)
		require.NotZero(t, p.ExpiryBlockHeight)

		approver := sdk.AccAddress([]byte("approver-app"))
		require.NoError(t, k.ApproveProject(ctx, id, approver, math.NewInt(100), math.NewInt(10)))

		p, err = k.GetProject(ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, p.Status)
		require.Zero(t, p.ExpiryBlockHeight, "ApproveProject must clear ExpiryBlockHeight")
	})

	t.Run("ExpireProject transitions PROPOSED -> EXPIRED and is a no-op otherwise", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator-exp2"))
		require.NoError(t, k.Member.Set(ctx, creator.String(), memberOf(creator)))

		// PROPOSED -> EXPIRED.
		id, err := k.CreateProject(ctx, creator, "Expire me", "desc", []string{"infra"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(100), math.NewInt(10), false)
		require.NoError(t, err)
		require.NoError(t, k.ExpireProject(ctx, id))

		p, err := k.GetProject(ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_EXPIRED, p.Status)
		require.Zero(t, p.ExpiryBlockHeight)

		// ACTIVE -> ExpireProject must be a no-op (covers the race where a
		// project is approved in the same block the EndBlocker would have
		// expired it).
		liveID, err := k.CreateProject(ctx, creator, "Live", "desc", []string{"infra"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(100), math.NewInt(10), false)
		require.NoError(t, err)
		require.NoError(t, k.ApproveProject(ctx, liveID, sdk.AccAddress([]byte("approver-live")),
			math.NewInt(100), math.NewInt(10)))
		require.NoError(t, k.ExpireProject(ctx, liveID))

		p, err = k.GetProject(ctx, liveID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_ACTIVE, p.Status,
			"ExpireProject must leave a non-PROPOSED project alone")
	})

	t.Run("EndBlocker expires stale proposals and leaves fresh ones alone", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		// Short window so the test can step past it cheaply.
		params, err := k.Params.Get(ctx)
		require.NoError(t, err)
		params.ProposedProjectExpiryBlocks = 10
		require.NoError(t, k.Params.Set(ctx, params))

		creator := sdk.AccAddress([]byte("creator-sweep"))
		require.NoError(t, k.Member.Set(ctx, creator.String(), memberOf(creator)))

		// Stale proposal created at height 0 (expiry = 10).
		staleID, err := k.CreateProject(ctx, creator, "Stale", "desc", []string{"infra"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(100), math.NewInt(10), false)
		require.NoError(t, err)

		// Advance height to 20 and create a fresh proposal at that height
		// (expiry = 30). Then run EndBlocker — only the stale one should expire.
		ctxAt20 := sdkCtx.WithBlockHeight(20)
		freshID, err := k.CreateProject(ctxAt20, creator, "Fresh", "desc", []string{"infra"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(100), math.NewInt(10), false)
		require.NoError(t, err)

		require.NoError(t, k.EndBlocker(ctxAt20))

		stale, err := k.GetProject(ctxAt20, staleID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_EXPIRED, stale.Status)

		fresh, err := k.GetProject(ctxAt20, freshID)
		require.NoError(t, err)
		require.Equal(t, types.ProjectStatus_PROJECT_STATUS_PROPOSED, fresh.Status,
			"a proposal whose expiry is still in the future must survive the sweep")
	})
}
