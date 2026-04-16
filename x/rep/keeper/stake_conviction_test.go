package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestCalculateStakingReward(t *testing.T) {
	t.Run("no pool initialized returns zero reward", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		stake := types.Stake{
			Id:        1,
			Staker:    "staker",
			Amount:    math.NewInt(1000),
			CreatedAt: 1000000,
		}

		reward, err := k.CalculateStakingReward(f.ctx, stake)
		require.NoError(t, err)
		require.True(t, reward.IsZero(), "expected zero reward when pool not initialized, got %s", reward.String())
	})

	t.Run("reward after pool distribution", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx

		// Initialize seasonal pool and register staked amount
		stakeAmount := math.NewInt(1000000)
		require.NoError(t, k.InitSeasonalPool(ctx, 1))
		require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, stakeAmount))

		// Distribute one epoch's rewards
		require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))

		// Compute expected reward:
		// epochSlice = MaxStakingRewardsPerSeason / SeasonDurationEpochs
		//            = 25,000,000,000,000 / 150 = 166,666,666,666
		// accPerShare = epochSlice / totalStaked = 166,666,666,666 / 1,000,000
		// reward = stakeAmount * accPerShare - rewardDebt(0)
		stake := types.Stake{
			Id:        1,
			Staker:    "staker",
			Amount:    stakeAmount,
			CreatedAt: 1000000,
		}

		reward, err := k.CalculateStakingReward(ctx, stake)
		require.NoError(t, err)
		// epochSlice (166,666,666,666) is distributed to the sole staker
		require.True(t, reward.IsPositive(), "expected positive reward after distribution, got %s", reward.String())
		require.Equal(t, math.NewInt(166666666666), reward)
	})

	t.Run("reward proportional to stake share", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx

		// Two stakers: 2x and 1x shares
		totalStaked := math.NewInt(3000000)
		require.NoError(t, k.InitSeasonalPool(ctx, 1))
		require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, totalStaked))
		require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))

		staker1 := types.Stake{Id: 1, Staker: "s1", Amount: math.NewInt(2000000)}
		staker2 := types.Stake{Id: 2, Staker: "s2", Amount: math.NewInt(1000000)}

		r1, err := k.CalculateStakingReward(ctx, staker1)
		require.NoError(t, err)
		r2, err := k.CalculateStakingReward(ctx, staker2)
		require.NoError(t, err)

		// Staker1 should get 2x staker2's reward
		require.True(t, r1.IsPositive())
		require.True(t, r2.IsPositive())
		require.True(t, r1.GT(r2), "staker1 (2x) should earn more than staker2 (1x)")
	})

	t.Run("rewardDebt subtracts from gross", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx

		stakeAmount := math.NewInt(1000000)
		require.NoError(t, k.InitSeasonalPool(ctx, 1))
		require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, stakeAmount))
		require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))

		// Stake with rewardDebt set (simulating a previously claimed stake)
		stake := types.Stake{
			Id:         1,
			Staker:     "staker",
			Amount:     stakeAmount,
			RewardDebt: math.NewInt(100000000000), // already claimed 100B
		}

		reward, err := k.CalculateStakingReward(ctx, stake)
		require.NoError(t, err)
		// Gross reward = 166,666,666,666 minus debt 100,000,000,000 = 66,666,666,666
		require.Equal(t, math.NewInt(66666666666), reward)
	})
}

func TestIsStakerExternal(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	assignee := "cosmos1assignee"
	creator := "cosmos1creator"

	t.Run("staker is assignee - not external", func(t *testing.T) {
		result := k.IsStakerExternal(assignee, assignee, creator)
		require.False(t, result, "assignee should not be external")
	})

	t.Run("staker is creator - not external", func(t *testing.T) {
		result := k.IsStakerExternal(creator, assignee, creator)
		require.False(t, result, "creator should not be external")
	})

	t.Run("staker is neither assignee nor creator - external", func(t *testing.T) {
		outsider := "cosmos1outsider"
		result := k.IsStakerExternal(outsider, assignee, creator)
		require.True(t, result, "unaffiliated staker should be external")
	})

	t.Run("staker is both assignee and creator - not external", func(t *testing.T) {
		// When assignee == creator == staker
		same := "cosmos1same"
		result := k.IsStakerExternal(same, same, same)
		require.False(t, result, "staker who is both assignee and creator should not be external")
	})
}

func TestCanCompleteInitiative(t *testing.T) {
	// Helper to set up a project and initiative in the fixture
	setupInitiative := func(t *testing.T, f *fixture) uint64 {
		t.Helper()
		k := f.keeper
		ctx := f.ctx

		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "100.0"},
		})

		projectID, err := k.CreateProject(ctx, creator, "TestProj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		require.NoError(t, err)

		err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
		require.NoError(t, err)

		budget := math.NewInt(100)
		initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "Desc", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", budget)
		require.NoError(t, err)

		return initID
	}

	t.Run("wrong status - OPEN cannot complete", func(t *testing.T) {
		f := initFixture(t)
		initID := setupInitiative(t, f)

		// Initiative starts in OPEN status - should not be completable
		canComplete, err := f.keeper.CanCompleteInitiative(f.ctx, initID)
		require.NoError(t, err)
		require.False(t, canComplete, "OPEN initiative should not be completable")
	})

	t.Run("wrong status - ASSIGNED cannot complete", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		initID := setupInitiative(t, f)

		// Move to ASSIGNED status
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		canComplete, err := k.CanCompleteInitiative(ctx, initID)
		require.NoError(t, err)
		require.False(t, canComplete, "ASSIGNED initiative should not be completable")
	})

	t.Run("wrong status - COMPLETED cannot complete again", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		initID := setupInitiative(t, f)

		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		canComplete, err := k.CanCompleteInitiative(ctx, initID)
		require.NoError(t, err)
		require.False(t, canComplete, "already COMPLETED initiative should not be completable")
	})

	t.Run("insufficient total conviction", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		initID := setupInitiative(t, f)

		// Set to SUBMITTED with high required conviction but low current
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
		initiative.RequiredConviction = keeper.PtrDec(math.LegacyNewDec(1000))
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(500))  // less than required
		initiative.ExternalConviction = keeper.PtrDec(math.LegacyNewDec(500)) // enough external
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		canComplete, err := k.CanCompleteInitiative(ctx, initID)
		require.NoError(t, err)
		require.False(t, canComplete, "should not complete with insufficient total conviction")
	})

	t.Run("insufficient external conviction", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		initID := setupInitiative(t, f)

		// Set to SUBMITTED with enough total conviction but not enough external
		// ExternalConvictionRatio is 0.50, so need at least 50% of RequiredConviction from external
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
		initiative.RequiredConviction = keeper.PtrDec(math.LegacyNewDec(1000))
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(1500)) // above required
		initiative.ExternalConviction = keeper.PtrDec(math.LegacyNewDec(400)) // below 50% of 1000 = 500
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		canComplete, err := k.CanCompleteInitiative(ctx, initID)
		require.NoError(t, err)
		require.False(t, canComplete, "should not complete with insufficient external conviction")
	})

	t.Run("has active challenges blocks completion", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		initID := setupInitiative(t, f)

		// Set initiative to SUBMITTED with sufficient conviction
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
		initiative.RequiredConviction = keeper.PtrDec(math.LegacyNewDec(100))
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(200))
		initiative.ExternalConviction = keeper.PtrDec(math.LegacyNewDec(100)) // 100 >= 50% of 100
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		// Create an active challenge for this initiative
		challengeID, err := k.ChallengeSeq.Next(ctx)
		require.NoError(t, err)
		challenge := types.Challenge{
			Id:           challengeID,
			InitiativeId: initID,
			Challenger:   "cosmos1challenger",
			Reason:       "quality concern",
			Status:       types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		}
		err = k.Challenge.Set(ctx, challengeID, challenge)
		require.NoError(t, err)
		err = k.AddChallengeToStatusIndex(ctx, challenge)
		require.NoError(t, err)

		canComplete, err := k.CanCompleteInitiative(ctx, initID)
		require.NoError(t, err)
		require.False(t, canComplete, "should not complete with active challenges")
	})

	t.Run("all requirements met - SUBMITTED status", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		initID := setupInitiative(t, f)

		// Set initiative to SUBMITTED with sufficient conviction, no challenges
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
		initiative.RequiredConviction = keeper.PtrDec(math.LegacyNewDec(100))
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(200))
		initiative.ExternalConviction = keeper.PtrDec(math.LegacyNewDec(100)) // 100 >= 50% of 100
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		canComplete, err := k.CanCompleteInitiative(ctx, initID)
		require.NoError(t, err)
		require.True(t, canComplete, "should be completable when all requirements are met with SUBMITTED status")
	})

	t.Run("all requirements met - IN_REVIEW status", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		initID := setupInitiative(t, f)

		// Set initiative to IN_REVIEW with sufficient conviction, no challenges
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW
		initiative.RequiredConviction = keeper.PtrDec(math.LegacyNewDec(100))
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(150))
		initiative.ExternalConviction = keeper.PtrDec(math.LegacyNewDec(60)) // 60 >= 50% of 100
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		canComplete, err := k.CanCompleteInitiative(ctx, initID)
		require.NoError(t, err)
		require.True(t, canComplete, "should be completable when all requirements are met with IN_REVIEW status")
	})

	t.Run("exactly at conviction threshold succeeds", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		initID := setupInitiative(t, f)

		// Exactly at the boundary: CurrentConviction == RequiredConviction
		// ExternalConviction == 50% of RequiredConviction
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
		initiative.RequiredConviction = keeper.PtrDec(math.LegacyNewDec(100))
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(100)) // exactly at threshold
		initiative.ExternalConviction = keeper.PtrDec(math.LegacyNewDec(50)) // exactly 50%
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		canComplete, err := k.CanCompleteInitiative(ctx, initID)
		require.NoError(t, err)
		require.True(t, canComplete, "should be completable when exactly at conviction thresholds")
	})

	t.Run("resolved challenge does not block completion", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx
		initID := setupInitiative(t, f)

		// Set initiative to SUBMITTED with sufficient conviction
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
		initiative.RequiredConviction = keeper.PtrDec(math.LegacyNewDec(100))
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(200))
		initiative.ExternalConviction = keeper.PtrDec(math.LegacyNewDec(100))
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		// Create a resolved (REJECTED) challenge - this should NOT block completion
		challengeID, err := k.ChallengeSeq.Next(ctx)
		require.NoError(t, err)
		err = k.Challenge.Set(ctx, challengeID, types.Challenge{
			Id:           challengeID,
			InitiativeId: initID,
			Challenger:   "cosmos1challenger",
			Reason:       "spurious concern",
			Status:       types.ChallengeStatus_CHALLENGE_STATUS_REJECTED,
		})
		require.NoError(t, err)

		canComplete, err := k.CanCompleteInitiative(ctx, initID)
		require.NoError(t, err)
		require.True(t, canComplete, "resolved challenge should not block completion")
	})
}
