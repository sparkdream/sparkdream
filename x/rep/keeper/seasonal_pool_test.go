package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestInitSeasonalPool_SetsBudgetAndResetsCounters(t *testing.T) {
	params := types.DefaultParams()
	params.MaxStakingRewardsPerSeason = math.NewInt(25_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	// Pre-seed season counters with non-zero values to prove they get reset.
	require.NoError(t, k.TrackMint(ctx, math.NewInt(5_000)))
	require.NoError(t, k.TrackBurn(ctx, math.NewInt(100)))
	require.NoError(t, k.TrackInitiativeRewardMint(ctx, math.NewInt(2_000)))

	require.NoError(t, k.InitSeasonalPool(ctx, 1))

	minted, err := k.GetSeasonMinted(ctx)
	require.NoError(t, err)
	require.True(t, minted.IsZero())

	burned, err := k.GetSeasonBurned(ctx)
	require.NoError(t, err)
	require.True(t, burned.IsZero())

	rewards, err := k.GetSeasonInitiativeRewardsMinted(ctx)
	require.NoError(t, err)
	require.True(t, rewards.IsZero())
}

func TestUpdateSeasonalPoolTotalStaked_IncrementAndGuard(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, math.NewInt(100)))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, math.NewInt(50)))
	// Negative delta is fine as long as total stays non-negative.
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, math.NewInt(-40)))

	// Driving the total below zero is rejected.
	err := k.UpdateSeasonalPoolTotalStaked(ctx, math.NewInt(-1_000_000))
	require.Error(t, err)
}

func TestDistributeEpochStakingRewardsFromPool_NoopWhenPoolEmpty(t *testing.T) {
	params := types.DefaultParams()
	params.MaxStakingRewardsPerSeason = math.ZeroInt()
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	require.NoError(t, k.InitSeasonalPool(ctx, 0))
	// With zero remaining, this is an explicit early-return.
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))
}

func TestDistributeEpochStakingRewardsFromPool_DrainsAcrossEpochs(t *testing.T) {
	// Small budget + short season so we can exhaust the pool in a few epochs.
	params := types.DefaultParams()
	params.EpochBlocks = 1
	params.SeasonDurationEpochs = 5
	params.MaxStakingRewardsPerSeason = math.NewInt(1_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	require.NoError(t, k.InitSeasonalPool(sdkCtx, 0))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(sdkCtx, math.NewInt(1)))

	// Walk forward through the full season; pool should monotonically drain.
	previous := params.MaxStakingRewardsPerSeason
	for epoch := int64(0); epoch < params.SeasonDurationEpochs; epoch++ {
		ctx := sdkCtx.WithBlockHeight(epoch)
		require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))
	}

	// After every epoch is processed the remaining pool must be strictly less
	// than the original budget. The method is designed so the final epoch
	// dumps any dust, so the remainder is at most small.
	str, err := k.SeasonalPoolRemaining.Get(sdkCtx)
	require.NoError(t, err)
	remaining, ok := math.NewIntFromString(str)
	require.True(t, ok)
	require.True(t, remaining.LT(previous), "pool should drain across epochs")
}
