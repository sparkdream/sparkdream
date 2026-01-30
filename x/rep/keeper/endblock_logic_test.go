package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestIsEpochEnd tests epoch boundary detection
func TestIsEpochEnd(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	params, _ := k.Params.Get(ctx)

	// At block 0, not end of epoch
	isEnd, err := k.IsEpochEnd(ctx)
	require.NoError(t, err)
	require.True(t, isEnd, "Block 0 is divisible by EpochBlocks (0 % n == 0)")

	// At block EpochBlocks, is end of epoch
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks)
	ctx = sdkCtx

	isEnd, err = k.IsEpochEnd(ctx)
	require.NoError(t, err)
	require.True(t, isEnd)

	// At block EpochBlocks - 1, not end
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks - 1)
	ctx = sdkCtx

	isEnd, err = k.IsEpochEnd(ctx)
	require.NoError(t, err)
	require.False(t, isEnd)

	// At block EpochBlocks * 2, is end
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks * 2)
	ctx = sdkCtx

	isEnd, err = k.IsEpochEnd(ctx)
	require.NoError(t, err)
	require.True(t, isEnd)
}

// TestIsEpochEnd_ZeroEpochBlocks tests zero division protection
func TestIsEpochEnd_ZeroEpochBlocks(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Set EpochBlocks to 0
	params, _ := k.Params.Get(ctx)
	params.EpochBlocks = 0
	k.Params.Set(ctx, params)

	// Should return false without error
	isEnd, err := k.IsEpochEnd(ctx)
	require.NoError(t, err)
	require.False(t, isEnd)
}

// TestUpdateInitiativeConviction tests conviction update wrapper
func TestUpdateInitiativeConviction(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member with enough reputation for EXPERT tier (min 100)
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "150.0"},
	})

	// Create project
	projectID, _ := k.CreateProject(ctx, member, "Test Project", "Description", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.ZeroInt())

	// Approve project first
	project, _ := k.GetProject(ctx, projectID)
	project.Status = types.ProjectStatus_PROJECT_STATUS_ACTIVE
	project.ApprovedBudget = PtrInt(math.NewInt(10000))
	k.UpdateProject(ctx, project)

	// Create initiative (using EXPERT tier which allows up to 2000 DREAM)
	initiativeID, err := k.CreateInitiative(ctx, member, projectID, "Test", "Test", []string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_EXPERT, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"", math.NewInt(1000))
	require.NoError(t, err)

	// Update conviction (wrapper should call UpdateInitiativeConvictionLazy)
	err = k.UpdateInitiativeConviction(ctx, initiativeID)
	require.NoError(t, err)

	// Verify initiative still exists
	initiative, err := k.GetInitiative(ctx, initiativeID)
	require.NoError(t, err)
	require.Equal(t, initiativeID, initiative.Id)
}

// TestTransitionToChallengePeriod tests status transition to IN_REVIEW
func TestTransitionToChallengePeriod(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member with enough reputation for EXPERT tier (min 100)
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "150.0"},
	})

	// Create project
	projectID, _ := k.CreateProject(ctx, member, "Test Project", "Description", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.ZeroInt())

	// Approve project first
	project, _ := k.GetProject(ctx, projectID)
	project.Status = types.ProjectStatus_PROJECT_STATUS_ACTIVE
	project.ApprovedBudget = PtrInt(math.NewInt(10000))
	k.UpdateProject(ctx, project)

	// Create initiative (using EXPERT tier which allows up to 2000 DREAM)
	initiativeID, err := k.CreateInitiative(ctx, member, projectID, "Test", "Test", []string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_EXPERT, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"", math.NewInt(1000))
	require.NoError(t, err)

	// Update status to SUBMITTED manually since CreateInitiative sets it to OPEN
	initiative, _ := k.GetInitiative(ctx, initiativeID)
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
	err = k.UpdateInitiative(ctx, initiative)
	require.NoError(t, err)

	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// Transition to challenge period
	err = k.TransitionToChallengePeriod(ctx, initiativeID)
	require.NoError(t, err)

	// Verify status changed
	initiative, err = k.GetInitiative(ctx, initiativeID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, initiative.Status)

	// Verify review period end set
	expectedReviewEnd := currentHeight + (params.DefaultReviewPeriodEpochs * params.EpochBlocks)
	require.Equal(t, expectedReviewEnd, initiative.ReviewPeriodEnd)

	// Verify challenge period end set
	expectedChallengeEnd := expectedReviewEnd + (params.DefaultChallengePeriodEpochs * params.EpochBlocks)
	require.Equal(t, expectedChallengeEnd, initiative.ChallengePeriodEnd)
}

// TestTransitionToChallengePeriod_NonExistent tests with invalid initiative
func TestTransitionToChallengePeriod_NonExistent(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Try to transition non-existent initiative
	err := k.TransitionToChallengePeriod(ctx, 999)
	require.Error(t, err)
}

// TestApplyDecay tests mass decay application
func TestApplyDecay(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create multiple members with balances
	members := []string{"member1", "member2", "member3"}
	for _, name := range members {
		addr := sdk.AccAddress([]byte(name))
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:        addr.String(),
			DreamBalance:   PtrInt(math.NewInt(1000)),
			StakedDream:    PtrInt(math.NewInt(0)),
			LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()),
			LastDecayEpoch: 0,
		})
	}

	// Move to epoch 1
	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks)
	ctx = sdkCtx

	// Apply decay to all members
	err := k.ApplyDecay(ctx)
	require.NoError(t, err)

	// Verify all members had decay applied
	for _, name := range members {
		addr := sdk.AccAddress([]byte(name))
		member, err := k.Member.Get(ctx, addr.String())
		require.NoError(t, err)

		// Should have decayed: 1000 * 0.99 = 990
		expectedBalance := math.NewInt(990)
		require.Equal(t, expectedBalance.String(), member.DreamBalance.String())
		require.Equal(t, int64(1), member.LastDecayEpoch)
	}
}

// TestApplyDecay_EmptyMemberList tests with no members
func TestApplyDecay_EmptyMemberList(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Apply decay with no members
	err := k.ApplyDecay(ctx)
	require.NoError(t, err)
}

// TestApplyDecay_MixedStakingLevels tests decay with different staking amounts
func TestApplyDecay_MixedStakingLevels(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create members with different staking levels
	testCases := []struct {
		name            string
		totalBalance    math.Int
		stakedBalance   math.Int
		expectedBalance math.Int
	}{
		{"all_staked", math.NewInt(1000), math.NewInt(1000), math.NewInt(1000)}, // No decay
		{"half_staked", math.NewInt(1000), math.NewInt(500), math.NewInt(995)},  // 500 decays to 495
		{"none_staked", math.NewInt(1000), math.NewInt(0), math.NewInt(990)},    // All decays
	}

	for _, tc := range testCases {
		addr := sdk.AccAddress([]byte(tc.name))
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:        addr.String(),
			DreamBalance:   PtrInt(tc.totalBalance),
			StakedDream:    PtrInt(tc.stakedBalance),
			LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()),
			LastDecayEpoch: 0,
		})
	}

	// Move to epoch 1
	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks)
	ctx = sdkCtx

	// Apply decay
	err := k.ApplyDecay(ctx)
	require.NoError(t, err)

	// Verify expected balances
	for _, tc := range testCases {
		addr := sdk.AccAddress([]byte(tc.name))
		member, err := k.Member.Get(ctx, addr.String())
		require.NoError(t, err)
		require.Equal(t, tc.expectedBalance.String(), member.DreamBalance.String(), "Failed for %s", tc.name)
	}
}

// TestDistributeEpochStakingRewards tests staking rewards distribution
func TestDistributeEpochStakingRewards(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Currently a no-op, just verify it doesn't error
	err := k.DistributeEpochStakingRewards(ctx)
	require.NoError(t, err)
}

// TestUpdateAllTrustLevels tests mass trust level updates
func TestUpdateAllTrustLevels(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create members
	members := []string{"member1", "member2"}
	for _, name := range members {
		addr := sdk.AccAddress([]byte(name))
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"technical": "50.0"},
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_TRUSTED,
		})
	}

	// Update all trust levels
	err := k.UpdateAllTrustLevels(ctx)
	require.NoError(t, err)

	// Verify members still exist (trust level calculation may not change them)
	for _, name := range members {
		addr := sdk.AccAddress([]byte(name))
		member, err := k.Member.Get(ctx, addr.String())
		require.NoError(t, err)
		require.Equal(t, addr.String(), member.Address)
	}
}

// TestUpdateAllTrustLevels_EmptyMemberList tests with no members
func TestUpdateAllTrustLevels_EmptyMemberList(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Should not error with empty member list
	err := k.UpdateAllTrustLevels(ctx)
	require.NoError(t, err)
}

// TestProcessExpiredAccountability tests accountability expiry processing
func TestProcessExpiredAccountability(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Currently a no-op, just verify it doesn't error
	err := k.ProcessExpiredAccountability(ctx)
	require.NoError(t, err)
}

// TestUpdateInitiativeConviction_NonExistent tests updating non-existent initiative
func TestUpdateInitiativeConviction_NonExistent(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Should error for non-existent initiative
	err := k.UpdateInitiativeConviction(ctx, 999)
	require.Error(t, err)
}

// TestTransitionToChallengePeriod_PeriodCalculation tests period calculation accuracy
func TestTransitionToChallengePeriod_PeriodCalculation(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Set custom period values
	params, _ := k.Params.Get(ctx)
	params.DefaultReviewPeriodEpochs = 2
	params.DefaultChallengePeriodEpochs = 3
	params.EpochBlocks = 100
	k.Params.Set(ctx, params)

	// Create member with enough reputation for EXPERT tier (min 100)
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"technical": "150.0"},
	})

	// Create project
	projectID, _ := k.CreateProject(ctx, member, "Test", "Test", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.ZeroInt())

	// Approve project
	project, _ := k.GetProject(ctx, projectID)
	project.Status = types.ProjectStatus_PROJECT_STATUS_ACTIVE
	project.ApprovedBudget = PtrInt(math.NewInt(10000))
	k.UpdateProject(ctx, project)

	// Create initiative at specific block height (using EXPERT tier which allows up to 2000 DREAM)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(1000)
	ctx = sdkCtx

	initiativeID, err := k.CreateInitiative(ctx, member, projectID, "Test", "Test", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_EXPERT, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"", math.NewInt(1000))
	require.NoError(t, err)

	// Update status to SUBMITTED
	initiative, _ := k.GetInitiative(ctx, initiativeID)
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
	k.UpdateInitiative(ctx, initiative)

	// Transition
	err = k.TransitionToChallengePeriod(ctx, initiativeID)
	require.NoError(t, err)

	// Verify exact calculations
	updatedInitiative, _ := k.GetInitiative(ctx, initiativeID)

	// ReviewPeriodEnd = 1000 + (2 epochs * 100 blocks) = 1200
	require.Equal(t, int64(1200), updatedInitiative.ReviewPeriodEnd)

	// ChallengePeriodEnd = 1200 + (3 epochs * 100 blocks) = 1500
	require.Equal(t, int64(1500), updatedInitiative.ChallengePeriodEnd)
}

// TestApplyDecay_PreservesOtherFields tests that decay doesn't corrupt other fields
func TestApplyDecay_PreservesOtherFields(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	originalMember := types.Member{
		Address:            addr.String(),
		DreamBalance:       PtrInt(math.NewInt(1000)),
		StakedDream:        PtrInt(math.NewInt(0)),
		LifetimeEarned:     PtrInt(math.NewInt(5000)),
		LifetimeBurned:     PtrInt(math.NewInt(100)),
		ReputationScores:   map[string]string{"technical": "75.5", "audit": "60.0"},
		TrustLevel:         types.TrustLevel_TRUST_LEVEL_CORE,
		InvitedBy:          "cosmos1inviter",
		LastDecayEpoch:     0,
		TipsGivenThisEpoch: 5,
		LastTipEpoch:       0,
	}
	k.Member.Set(ctx, addr.String(), originalMember)

	// Move to epoch 1
	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks)
	ctx = sdkCtx

	// Apply decay
	err := k.ApplyDecay(ctx)
	require.NoError(t, err)

	// Verify only balance and lifetime burned changed
	member, _ := k.Member.Get(ctx, addr.String())
	require.Equal(t, math.NewInt(990).String(), member.DreamBalance.String())   // Decayed
	require.Equal(t, math.NewInt(110).String(), member.LifetimeBurned.String()) // Increased
	require.Equal(t, int64(1), member.LastDecayEpoch)                           // Updated

	// Everything else preserved
	require.Equal(t, math.NewInt(5000).String(), member.LifetimeEarned.String())
	require.Equal(t, originalMember.ReputationScores, member.ReputationScores)
	require.Equal(t, originalMember.TrustLevel, member.TrustLevel)
	require.Equal(t, originalMember.InvitedBy, member.InvitedBy)
	require.Equal(t, originalMember.TipsGivenThisEpoch, member.TipsGivenThisEpoch)
}
