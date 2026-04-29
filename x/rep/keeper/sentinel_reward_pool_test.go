package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// TestSentinelRewardPool_Defaults verifies the new Stage A params propagate via
// DefaultParams / DefaultRepOperationalParams.
func TestSentinelRewardPool_Defaults(t *testing.T) {
	p := types.DefaultParams()

	require.Equal(t, "100000000000", p.MaxSentinelRewardPool.String(),
		"expected 100,000 SPARK in uspark")
	require.Equal(t, "0.500000000000000000", p.SentinelRewardPoolOverflowBurnRatio.String())
	require.Greater(t, p.SentinelRewardEpochBlocks, uint64(0),
		"sentinel reward epoch blocks must be positive")
	require.Equal(t, "0.700000000000000000", p.MinSentinelAccuracy.String())
	require.Equal(t, uint64(10), p.MinAppealsForAccuracy)
	require.Equal(t, uint64(1), p.MinEpochActivityForReward)
	require.Equal(t, "0.050000000000000000", p.MinAppealRate.String())

	op := types.DefaultRepOperationalParams()
	require.Equal(t, p.MaxSentinelRewardPool, op.MaxSentinelRewardPool)
	require.Equal(t, p.SentinelRewardPoolOverflowBurnRatio, op.SentinelRewardPoolOverflowBurnRatio)
	require.Equal(t, p.SentinelRewardEpochBlocks, op.SentinelRewardEpochBlocks)
	require.Equal(t, p.MinSentinelAccuracy, op.MinSentinelAccuracy)
	require.Equal(t, p.MinAppealsForAccuracy, op.MinAppealsForAccuracy)
	require.Equal(t, p.MinEpochActivityForReward, op.MinEpochActivityForReward)
	require.Equal(t, p.MinAppealRate, op.MinAppealRate)

	// Round-trip through ApplyOperationalParams / ExtractOperationalParams.
	extracted := types.DefaultParams().ExtractOperationalParams()
	require.Equal(t, op, extracted)

	reapplied := (types.Params{}).ApplyOperationalParams(op)
	require.Equal(t, op.MaxSentinelRewardPool, reapplied.MaxSentinelRewardPool)
	require.Equal(t, op.SentinelRewardEpochBlocks, reapplied.SentinelRewardEpochBlocks)
}

// TestSentinelRewardPool_GetReadsBankBalance ensures GetSentinelRewardPool is a
// pure read of the sentinel reward pool sub-address's uspark bank balance.
func TestSentinelRewardPool_GetReadsBankBalance(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Reconfigure the mock bank so GetBalance returns a known amount for the
	// sentinel reward pool sub-address (REP-S2-4 partition).
	poolAddr := keeper.SentinelRewardPoolAddress()
	fixture.bankKeeper.GetBalanceFn = func(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
		require.True(t, addr.Equals(poolAddr), "pool read must target sentinel sub-address")
		require.Equal(t, types.RewardDenom, denom, "pool should read uspark")
		return sdk.NewCoin(denom, math.NewInt(12345))
	}

	got := k.GetSentinelRewardPool(ctx)
	require.Equal(t, math.NewInt(12345), got)
}

// TestSentinelRewardPool_AddTransfersToSubAddress verifies AddToSentinelRewardPool
// routes funds via SendCoins to the sentinel reward pool sub-address (REP-S2-4).
func TestSentinelRewardPool_AddTransfersToSubAddress(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	var called bool
	var gotTo sdk.AccAddress
	var gotCoins sdk.Coins
	fixture.bankKeeper.SendCoinsFn = func(
		_ context.Context, _ sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins,
	) error {
		called = true
		gotTo = toAddr
		gotCoins = amt
		return nil
	}

	sender := sdk.AccAddress([]byte("sender______________"))
	err := k.AddToSentinelRewardPool(ctx, sender, math.NewInt(1_000))
	require.NoError(t, err)
	require.True(t, called)
	require.True(t, gotTo.Equals(keeper.SentinelRewardPoolAddress()))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(types.RewardDenom, math.NewInt(1_000))), gotCoins)
}

// TestSentinelRewardPool_AddRejectsNonPositive checks the amount guard.
func TestSentinelRewardPool_AddRejectsNonPositive(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	sender := sdk.AccAddress([]byte("sender______________"))
	require.Error(t, k.AddToSentinelRewardPool(ctx, sender, math.ZeroInt()))
	require.Error(t, k.AddToSentinelRewardPool(ctx, sender, math.NewInt(-1)))
}

// TestBurnSentinelRewardPoolOverflow_UnderCapNoOp ensures no burn when pool <= max.
func TestBurnSentinelRewardPoolOverflow_UnderCapNoOp(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Pool below the cap.
	maxPool := types.DefaultParams().MaxSentinelRewardPool
	fixture.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, maxPool.Sub(math.NewInt(1)))
	}
	burnCalled := false
	fixture.bankKeeper.BurnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	require.NoError(t, k.BurnSentinelRewardPoolOverflow(ctx))
	require.False(t, burnCalled, "expected no burn when pool <= max")

	// Exactly at the cap — still no-op.
	fixture.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, maxPool)
	}
	require.NoError(t, k.BurnSentinelRewardPoolOverflow(ctx))
	require.False(t, burnCalled, "expected no burn when pool == max")
}

// TestBurnSentinelRewardPoolOverflow_AboveCapBurnsRatio ensures burn amount is
// ratio * overflow (truncated), is moved out of the sentinel sub-address into
// the rep module account, and then burned from the module account.
func TestBurnSentinelRewardPoolOverflow_AboveCapBurnsRatio(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Pool is maxPool + 1000 uspark over. With ratio 0.5 we expect 500 burned.
	maxPool := types.DefaultParams().MaxSentinelRewardPool
	overflow := math.NewInt(1000)
	fixture.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, maxPool.Add(overflow))
	}

	var movedFrom sdk.AccAddress
	var movedCoins sdk.Coins
	moveCount := 0
	fixture.bankKeeper.SendCoinsFn = func(_ context.Context, from sdk.AccAddress, _ sdk.AccAddress, amt sdk.Coins) error {
		movedFrom = from
		movedCoins = amt
		moveCount++
		return nil
	}

	var burnedModule string
	var burnedCoins sdk.Coins
	burnCount := 0
	fixture.bankKeeper.BurnCoinsFn = func(_ context.Context, moduleName string, amt sdk.Coins) error {
		burnedModule = moduleName
		burnedCoins = amt
		burnCount++
		return nil
	}

	require.NoError(t, k.BurnSentinelRewardPoolOverflow(ctx))
	require.Equal(t, 1, moveCount, "expected exactly one move out of sentinel sub-address")
	require.True(t, movedFrom.Equals(keeper.SentinelRewardPoolAddress()))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(types.RewardDenom, math.NewInt(500))), movedCoins)
	require.Equal(t, 1, burnCount, "expected exactly one burn call")
	require.Equal(t, types.ModuleName, burnedModule)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(types.RewardDenom, math.NewInt(500))), burnedCoins)
}

// TestBurnSentinelRewardPoolOverflow_TinyOverflowNoBurn verifies a tiny overflow
// that truncates to zero results in no burn call.
func TestBurnSentinelRewardPoolOverflow_TinyOverflowNoBurn(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Overflow of 1 with ratio 0.5 truncates to 0 -> no burn.
	maxPool := types.DefaultParams().MaxSentinelRewardPool
	fixture.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, maxPool.Add(math.NewInt(1)))
	}
	burnCalled := false
	fixture.bankKeeper.BurnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}
	require.NoError(t, k.BurnSentinelRewardPoolOverflow(ctx))
	require.False(t, burnCalled)
}
