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

	total, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(110), total)

	// An under-run would mean a decrement site ran without a matching
	// increment. Failing the tx would strand the staker's DREAM, so the total
	// is floored at zero instead (and the discrepancy logged).
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, math.NewInt(-1_000_000)))
	total, err = k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	require.True(t, total.IsZero())
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
	// 1 DREAM staked, so StakingRewardYieldPerEpoch (0.001 * 1,000,000 = 1,000)
	// leaves the 200-per-epoch calendar slice untouched and the pool drains on
	// the schedule this test is about.
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(sdkCtx, math.NewInt(1_000_000)))

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

// TestDistributeEpochStakingRewardsFromPool_YieldCapBindsOnDustStake is the
// regression for the failure this cap exists to prevent.
//
// A devnet ran three 0.49 DREAM stakes as the entire staked base of the chain.
// The epoch slice was budget/remaining_epochs regardless, so acc_per_share
// climbed until each of those stakes was owed more DREAM than
// max_dream_mint_per_epoch would ever mint. Settling one reverted, and because
// completion settles every stake it touches, the initiative they were staked on
// could never complete — it sat IN_REVIEW past its challenge window while the
// EndBlocker retried and failed every block.
func TestDistributeEpochStakingRewardsFromPool_YieldCapBindsOnDustStake(t *testing.T) {
	// Reproduce the pool the devnet actually ran: the full flat budget, with
	// neither the schedule ceiling nor the mint share binding, so that the
	// per-epoch yield cap is the only thing standing between a dust staked base
	// and an unpayable accumulator.
	params := types.DefaultParams()
	params.StakingPoolCapBase = params.MaxStakingRewardsPerSeason
	params.StakingPoolCapRate = math.LegacyOneDec()
	params.StakingPoolMintShare = math.LegacyOneDec()
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	// The whole chain: 1.47 DREAM staked.
	const dustStaked = 1_470_000
	require.NoError(t, k.TrackMint(ctx, params.MaxStakingRewardsPerSeason))
	require.NoError(t, k.InitSeasonalPool(ctx, 1))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, math.NewInt(dustStaked)))

	seeded, err := k.GetSeasonalPoolRemaining(ctx)
	require.NoError(t, err)
	require.Equal(t, params.MaxStakingRewardsPerSeason.String(), seeded.String(),
		"this test needs the full flat budget to be the thing under test")

	before := seeded

	// Run a full season of epochs, then another, then another.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for epoch := int64(0); epoch < params.SeasonDurationEpochs*3; epoch++ {
		require.NoError(t, k.DistributeEpochStakingRewardsFromPool(
			sdkCtx.WithBlockHeight(epoch*params.EpochBlocks)))
	}

	accPerShare, err := k.GetSeasonalPoolAccPerShare(ctx)
	require.NoError(t, err)

	// What the dust stakes are collectively owed after three seasons.
	accrued := accPerShare.MulInt64(dustStaked).TruncateInt()
	require.True(t, accrued.LT(params.MaxDreamMintPerEpoch),
		"a staked base this small must never accrue past the per-epoch mint cap: accrued %s, cap %s",
		accrued, params.MaxDreamMintPerEpoch)

	// Each epoch pays at most the yield on what is staked.
	perEpoch := params.StakingRewardYieldPerEpoch.MulInt64(dustStaked).TruncateInt()
	require.True(t, accrued.LTE(perEpoch.MulRaw(params.SeasonDurationEpochs*3)),
		"payout should be bounded by the per-epoch yield, got %s", accrued)

	// And what the cap withheld is still in the pool, not burned: the budget
	// drained by exactly what was distributed.
	after, err := k.GetSeasonalPoolRemaining(ctx)
	require.NoError(t, err)
	require.Equal(t, before.Sub(accrued).String(), after.String(),
		"the pool must fall by exactly what was distributed")
}

func TestSeasonalPoolBudget_SizedFromPreviousSeasonProduction(t *testing.T) {
	params := types.DefaultParams()
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	// A season that minted 10,000 DREAM, 4,000 of it staking rewards.
	require.NoError(t, k.TrackMint(ctx, math.NewInt(10_000_000_000)))
	require.NoError(t, k.TrackStakingRewardMint(ctx, math.NewInt(4_000_000_000)))

	// Drain the genesis-seeded pool first: InitSeasonalPool now carries
	// unspent budget over, and this test isolates the sizing formula.
	require.NoError(t, k.SetSeasonalPoolRemainingForTest(ctx, math.ZeroInt()))
	require.NoError(t, k.InitSeasonalPool(ctx, 1))

	// base = 10,000 - 4,000 = 6,000 DREAM; share 5% => 300 DREAM.
	remaining, err := k.GetSeasonalPoolRemaining(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(300_000_000).String(), remaining.String(),
		"staking rewards must be excluded from the base that sizes the next season")

	// The counter that made that subtraction possible is reset with the rest.
	stakingMinted, err := k.GetSeasonStakingRewardsMinted(ctx)
	require.NoError(t, err)
	require.True(t, stakingMinted.IsZero())
}

func TestSeasonalPoolBudget_ScheduleCapBindsOnAMintSpike(t *testing.T) {
	params := types.DefaultParams()
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	// A season that minted far more than the schedule allows to be recycled.
	require.NoError(t, k.TrackMint(ctx, math.NewInt(500_000_000_000)))
	require.NoError(t, k.SetSeasonalPoolRemainingForTest(ctx, math.ZeroInt()))
	require.NoError(t, k.InitSeasonalPool(ctx, 3))

	// 5% of 500,000 DREAM would be 25,000 DREAM; the schedule ceiling for
	// season 3 is cap_base * 4 * 0.05 = 25,000 * 4 * 0.05 = 5,000 DREAM.
	remaining, err := k.GetSeasonalPoolRemaining(ctx)
	require.NoError(t, err)
	expected := params.StakingPoolCapRate.MulInt(params.StakingPoolCapBase).MulInt64(4).TruncateInt()
	require.Equal(t, expected.String(), remaining.String())
	require.Equal(t, math.NewInt(5_000_000_000).String(), remaining.String())
}

func TestSeasonalPoolBudget_NoHistoryFallsBackToSchedule(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Nothing minted yet, as at genesis. Sizing from production would leave the
	// first season with no budget at all.
	require.NoError(t, k.SetSeasonalPoolRemainingForTest(ctx, math.ZeroInt()))
	require.NoError(t, k.InitSeasonalPool(ctx, 0))

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	remaining, err := k.GetSeasonalPoolRemaining(ctx)
	require.NoError(t, err)
	expected := params.StakingPoolCapRate.MulInt(params.StakingPoolCapBase).TruncateInt()
	require.Equal(t, expected.String(), remaining.String())
	require.True(t, remaining.IsPositive())
}

// TestInitSeasonalPool_CarriesOverUnspentBudget pins the rollover semantics:
// what the yield cap withheld in a quiet season is added to the incoming
// season's budget instead of being discarded, and the ceiling stays the hard
// bound on what a single season can ever pay out.
func TestInitSeasonalPool_CarriesOverUnspentBudget(t *testing.T) {
	params := types.DefaultParams()
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	// The outgoing season still holds 2,000 DREAM it never distributed, and
	// produced enough that the incoming budget is the full 25,000 ceiling.
	require.NoError(t, k.TrackMint(ctx, math.NewInt(500_000_000_000)))
	require.NoError(t, k.SetSeasonalPoolRemainingForTest(ctx, math.NewInt(2_000_000_000)))

	require.NoError(t, k.InitSeasonalPool(ctx, 3))

	remaining, err := k.GetSeasonalPoolRemaining(ctx)
	require.NoError(t, err)
	// budget 5,000 (schedule cap) + carried 2,000 = 7,000, under the ceiling.
	require.Equal(t, math.NewInt(7_000_000_000).String(), remaining.String(),
		"unspent budget must carry into the next season")

	// Second rollover: same production, but the carried total would push past
	// the 25,000 ceiling, so the ceiling binds and the excess is dropped.
	require.NoError(t, k.SetSeasonalPoolRemainingForTest(ctx, math.NewInt(24_000_000_000)))
	require.NoError(t, k.InitSeasonalPool(ctx, 4))
	remaining, err = k.GetSeasonalPoolRemaining(ctx)
	require.NoError(t, err)
	require.Equal(t, params.MaxStakingRewardsPerSeason.String(), remaining.String(),
		"carried budget cannot lift a season past the ceiling")
}

// TestDistributeEpochStakingRewardsFromPool_AnchoredToInitEpoch pins the
// drain schedule to the stored init epoch: the slice is remaining divided by
// (duration - elapsed-since-init), not by a modulo of the raw epoch counter.
// Seasons are driven by x/season and need not be aligned with any multiple of
// SeasonDurationEpochs counted from height zero.
func TestDistributeEpochStakingRewardsFromPool_AnchoredToInitEpoch(t *testing.T) {
	params := types.DefaultParams()
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	// A pool initialized at epoch 10 with a known budget and a staked base
	// large enough that the yield cap does not bind.
	require.NoError(t, k.SetSeasonalPoolRemainingForTest(ctx, math.NewInt(150_000)))
	require.NoError(t, k.SetSeasonalPoolStartEpochForTest(ctx, 10))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, math.NewInt(1_000_000_000)))

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// One epoch after init: 149 epochs remain, so the slice is 150000/149.
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(
		sdkCtx.WithBlockHeight(11*params.EpochBlocks)))

	remaining, err := k.GetSeasonalPoolRemaining(ctx)
	require.NoError(t, err)
	slice := math.NewInt(150_000).Quo(math.NewInt(149))
	require.Equal(t, math.NewInt(150_000).Sub(slice).String(), remaining.String(),
		"slice must be sized by epochs since the pool's own init, not by epoch %% duration")

	// The modulo reading at epoch 11 would have been 150-11 = 139 epochs left
	// and therefore a smaller slice; prove the anchor wins.
	moduloSlice := math.NewInt(150_000).Quo(math.NewInt(139))
	if !moduloSlice.Equal(slice) {
		require.NotEqual(t, math.NewInt(150_000).Sub(moduloSlice).String(), remaining.String(),
			"an identical result under the modulo reading would make this test vacuous")
	}
}

func TestRebaseStakeRewardDebt_ZeroesPendingAgainstTheLiveAccumulator(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// A stake carrying no debt while the accumulator has already run: exactly
	// the shape an import produces, and the shape that lets a dust stake claim
	// the whole accumulator.
	stake := types.Stake{
		Id:         41,
		Staker:     "staker",
		TargetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		TargetId:   1,
		Amount:     math.NewInt(490_000),
		RewardDebt: math.ZeroInt(),
	}
	require.NoError(t, k.Stake.Set(ctx, stake.Id, stake))
	// A content stake has no accumulator and must be left alone.
	content := types.Stake{
		Id:         42,
		Staker:     "staker",
		TargetType: types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		Amount:     math.NewInt(490_000),
		RewardDebt: math.ZeroInt(),
	}
	require.NoError(t, k.Stake.Set(ctx, content.Id, content))

	require.NoError(t, k.InitSeasonalPool(ctx, 1))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(ctx, stake.Amount))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))

	before, err := k.CalculateStakingReward(ctx, stake)
	require.NoError(t, err)
	require.True(t, before.IsPositive(), "the stake should be owed something before the rebase")

	rebased, err := k.RebaseStakeRewardDebt(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, rebased, "only the seasonal-pool stake should be rewritten")

	got, err := k.Stake.Get(ctx, stake.Id)
	require.NoError(t, err)
	after, err := k.CalculateStakingReward(ctx, got)
	require.NoError(t, err)
	require.True(t, after.IsZero(), "the rebase must leave the stake earning from zero, got %s", after)

	gotContent, err := k.Stake.Get(ctx, content.Id)
	require.NoError(t, err)
	require.True(t, gotContent.RewardDebt.IsZero(), "a content stake has no accumulator to rebase against")
}
