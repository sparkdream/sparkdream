package keeper_test

import (
	"fmt"
	stdmath "math"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestCreateInitiative(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Create a project
	creator := sdk.AccAddress([]byte("creator"))
	projectID, err := k.CreateProject(
		ctx,
		creator,
		"Test Project",
		"Description",
		[]string{"backend", "frontend"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(10000),
		math.NewInt(1000),
	)
	require.NoError(t, err)

	// Approve project
	approver := sdk.AccAddress([]byte("approver"))
	err = k.ApproveProject(ctx, projectID, approver, math.NewInt(10000), math.NewInt(1000))
	require.NoError(t, err)

	// Create member
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0", "frontend": "30.0"},
	})

	// Test: Create initiative
	budget := math.NewInt(500)
	initID, err := k.CreateInitiative(
		ctx,
		creator,
		projectID,
		"Build API endpoint",
		"Implement REST API for user management",
		[]string{"backend"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"template123",
		budget,
	)
	require.NoError(t, err)

	// Verify initiative
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, projectID, initiative.ProjectId)
	require.Equal(t, "Build API endpoint", initiative.Title)
	require.Equal(t, []string{"backend"}, initiative.Tags)
	require.Equal(t, types.InitiativeTier_INITIATIVE_TIER_STANDARD, initiative.Tier)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_OPEN, initiative.Status)

	// Verify required conviction calculation
	// Formula: required_conviction = conviction_per_dream × sqrt(budget)
	params, _ := k.Params.Get(ctx)
	sqrtBudget := math.LegacyMustNewDecFromStr(fmt.Sprintf("%.18f", stdmath.Sqrt(float64(budget.Uint64()))))
	expectedConviction := params.ConvictionPerDream.Mul(sqrtBudget)
	require.Equal(t, expectedConviction.String(), initiative.RequiredConviction.String())

	// Verify budget was allocated from project
	project, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, budget.String(), project.AllocatedBudget.String())
}

func TestAssignInitiative(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	initID, _ := k.CreateInitiative(
		ctx,
		creator,
		projectID,
		"Task",
		"Description",
		[]string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"",
		math.NewInt(100),
	)

	// Create assignee with sufficient reputation
	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	// Test: Assign initiative
	err := k.AssignInitiativeToMember(ctx, initID, assignee)
	require.NoError(t, err)

	// Verify assignment
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, assignee.String(), initiative.Assignee)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED, initiative.Status)
}

func TestSubmitInitiativeWork(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	})

	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
	k.AssignInitiativeToMember(ctx, initID, assignee)

	// Test: Submit work
	err := k.SubmitInitiativeWork(ctx, initID, assignee, "https://github.com/repo/pr/123")
	require.NoError(t, err)

	// Verify submission
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, initiative.Status)
	require.Equal(t, "https://github.com/repo/pr/123", initiative.DeliverableUri)
}

func TestAbandonInitiative(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
	})

	budget := math.NewInt(100)
	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
	k.AssignInitiativeToMember(ctx, initID, assignee)

	// Test: Abandon initiative
	err := k.AbandonInitiative(ctx, initID, assignee, "No longer needed")
	require.NoError(t, err)

	// Verify abandonment
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED, initiative.Status)

	// Verify budget was returned
	project, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), project.AllocatedBudget.String())
}

func TestCompleteInitiative(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup project and initiative
	creator := sdk.AccAddress([]byte("creator"))
	projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})

	budget := math.NewInt(100)
	initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
	k.AssignInitiativeToMember(ctx, initID, assignee)
	k.SubmitInitiativeWork(ctx, initID, assignee, "deliverable")

	// Create external staker to meet external conviction requirement
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	})

	// Stake enough to meet conviction requirements
	stakeAmount := math.NewInt(1000)
	_, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", stakeAmount)
	require.NoError(t, err)

	// Force update conviction (normally happens in EndBlocker)
	// We manually set conviction to ensure it passes the threshold for completion testing
	initiative, _ := k.GetInitiative(ctx, initID)
	// Required conviction for 100 DREAM budget is likely 100 (assuming 1 DREAM = 1 Conviction param)
	// We verify params first to be sure
	params, _ := k.Params.Get(ctx)
	reqConv := params.ConvictionPerDream.MulInt(budget)

	// Set conviction to > required
	currentConv := reqConv.Mul(math.LegacyNewDec(2))
	initiative.CurrentConviction = PtrDec(currentConv)

	// Set external conviction (assignee != staker if staker is new)
	// Staker created above is new, so it counts as external.
	// But we manually set it to be safe.
	initiative.ExternalConviction = PtrDec(currentConv)

	k.UpdateInitiative(ctx, initiative)

	// Test: Complete initiative
	err = k.CompleteInitiative(ctx, initID)
	require.NoError(t, err)
}

func TestSeasonInitiativeRewardsCap(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Set a low per-season initiative reward cap: 150 micro-DREAM
	params, _ := k.Params.Get(ctx)
	params.MaxInitiativeRewardsPerSeason = math.NewInt(150)
	k.Params.Set(ctx, params)

	// Initialize the seasonal pool (resets counters)
	k.InitSeasonalPool(ctx, 1)

	// Helper to create a completable initiative with the given budget
	createCompletable := func(budget math.Int, suffix string) uint64 {
		creator := sdk.AccAddress([]byte("creator" + suffix))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address: creator.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"backend": "50.0"},
		})
		assignee := sdk.AccAddress([]byte("assignee" + suffix))
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address: assignee.String(), DreamBalance: PtrInt(math.ZeroInt()),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"backend": "100.0"},
			TrustLevel: types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		})
		staker := sdk.AccAddress([]byte("staker" + suffix))
		k.Member.Set(ctx, staker.String(), types.Member{
			Address: staker.String(), DreamBalance: PtrInt(math.NewInt(100000)),
			StakedDream: PtrInt(math.ZeroInt()), LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()), ReputationScores: map[string]string{"backend": "100.0"},
		})

		projID, _ := k.CreateProject(ctx, creator, "P"+suffix, "D", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(100000), math.NewInt(1000))
		k.ApproveProject(ctx, projID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000))
		initID, _ := k.CreateInitiative(ctx, creator, projID, "T"+suffix, "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_APPRENTICE, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "deliverable")
		k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(10000))

		// Force conviction to meet threshold
		init, _ := k.GetInitiative(ctx, initID)
		init.CurrentConviction = PtrDec(DerefDec(init.RequiredConviction).Mul(math.LegacyNewDec(3)))
		init.ExternalConviction = PtrDec(DerefDec(init.RequiredConviction).Mul(math.LegacyNewDec(3)))
		k.UpdateInitiative(ctx, init)
		return initID
	}

	// First initiative: 100 micro-DREAM budget → 90 completer reward (90% share)
	// 90 < 150 cap → should succeed
	initID1 := createCompletable(math.NewInt(100), "_a")
	err := k.CompleteInitiative(ctx, initID1)
	require.NoError(t, err)

	// Verify counter was tracked
	minted, err := k.GetSeasonInitiativeRewardsMinted(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(90).String(), minted.String()) // 100 * 0.9 = 90

	// Second initiative: 100 micro-DREAM budget → 90 completer reward
	// 90 + 90 = 180 > 150 cap → should fail
	initID2 := createCompletable(math.NewInt(100), "_b")
	err = k.CompleteInitiative(ctx, initID2)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInitiativeRewardCapReached)

	// Counter should still be 90 (second completion was rejected)
	minted, err = k.GetSeasonInitiativeRewardsMinted(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(90).String(), minted.String())
}
