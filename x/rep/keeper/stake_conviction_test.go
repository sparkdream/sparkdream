package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestCalculateStakingReward(t *testing.T) {
	t.Run("zero duration returns zero reward", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		// Set block time to the same as stake creation time
		createdAt := int64(1000000)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		ctx := sdkCtx.WithBlockTime(time.Unix(createdAt, 0))

		stake := types.Stake{
			Id:        1,
			Staker:    "staker",
			Amount:    math.NewInt(1000),
			CreatedAt: createdAt,
		}

		reward, err := k.CalculateStakingReward(ctx, stake)
		require.NoError(t, err)
		require.True(t, reward.IsZero(), "expected zero reward for zero duration, got %s", reward.String())
	})

	t.Run("negative duration returns zero reward", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		// Set block time before stake creation (shouldn't happen, but defensive)
		createdAt := int64(1000000)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		ctx := sdkCtx.WithBlockTime(time.Unix(createdAt-100, 0))

		stake := types.Stake{
			Id:        1,
			Staker:    "staker",
			Amount:    math.NewInt(1000),
			CreatedAt: createdAt,
		}

		reward, err := k.CalculateStakingReward(ctx, stake)
		require.NoError(t, err)
		require.True(t, reward.IsZero(), "expected zero reward for negative duration, got %s", reward.String())
	})

	t.Run("positive duration uses CreatedAt when LastClaimedAt is zero", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		// Default StakingApy is 10% (0.10)
		// secondsPerYear = 31,557,600
		// reward = amount * apy * duration / secondsPerYear
		// For amount=1,000,000, duration=31,557,600 (1 year), apy=0.10:
		// reward = 1,000,000 * 0.10 * 31,557,600 / 31,557,600 = 100,000
		createdAt := int64(1000000)
		duration := int64(31557600) // exactly 1 year
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		ctx := sdkCtx.WithBlockTime(time.Unix(createdAt+duration, 0))

		stake := types.Stake{
			Id:            1,
			Staker:        "staker",
			Amount:        math.NewInt(1000000),
			CreatedAt:     createdAt,
			LastClaimedAt: 0, // not set
		}

		reward, err := k.CalculateStakingReward(ctx, stake)
		require.NoError(t, err)
		// 1,000,000 * 0.10 * 1.0 = 100,000
		require.Equal(t, math.NewInt(100000), reward)
	})

	t.Run("uses LastClaimedAt when set", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		// CreatedAt is far in the past, but LastClaimedAt is more recent
		createdAt := int64(1000000)
		lastClaimed := int64(2000000)
		duration := int64(31557600) // 1 year after LastClaimedAt
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		ctx := sdkCtx.WithBlockTime(time.Unix(lastClaimed+duration, 0))

		stake := types.Stake{
			Id:            1,
			Staker:        "staker",
			Amount:        math.NewInt(1000000),
			CreatedAt:     createdAt,
			LastClaimedAt: lastClaimed,
		}

		reward, err := k.CalculateStakingReward(ctx, stake)
		require.NoError(t, err)
		// Duration is from LastClaimedAt (not CreatedAt), so exactly 1 year
		// 1,000,000 * 0.10 * 1.0 = 100,000
		require.Equal(t, math.NewInt(100000), reward)
	})

	t.Run("partial year calculation", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		// Half a year: reward = 1,000,000 * 0.10 * 0.5 = 50,000
		createdAt := int64(1000000)
		halfYear := int64(31557600 / 2)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		ctx := sdkCtx.WithBlockTime(time.Unix(createdAt+halfYear, 0))

		stake := types.Stake{
			Id:        1,
			Staker:    "staker",
			Amount:    math.NewInt(1000000),
			CreatedAt: createdAt,
		}

		reward, err := k.CalculateStakingReward(ctx, stake)
		require.NoError(t, err)
		require.Equal(t, math.NewInt(50000), reward)
	})

	t.Run("small amount truncates to zero", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		// Very small stake for a short time: reward truncates to zero
		// amount=1, duration=1s, apy=0.10
		// reward = 1 * 0.10 * 1 / 31,557,600 = ~0.000000003 -> truncates to 0
		createdAt := int64(1000000)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		ctx := sdkCtx.WithBlockTime(time.Unix(createdAt+1, 0))

		stake := types.Stake{
			Id:        1,
			Staker:    "staker",
			Amount:    math.NewInt(1),
			CreatedAt: createdAt,
		}

		reward, err := k.CalculateStakingReward(ctx, stake)
		require.NoError(t, err)
		require.True(t, reward.IsZero(), "expected truncated zero reward for tiny stake, got %s", reward.String())
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

		projectID, err := k.CreateProject(ctx, creator, "TestProj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
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
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(500))   // less than required
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
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(1500))  // above required
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
		err = k.Challenge.Set(ctx, challengeID, types.Challenge{
			Id:           challengeID,
			InitiativeId: initID,
			Challenger:   "cosmos1challenger",
			Reason:       "quality concern",
			Status:       types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		})
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
		initiative.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(100))  // exactly at threshold
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
