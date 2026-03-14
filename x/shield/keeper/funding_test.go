package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func TestDayFundingZeroValue(t *testing.T) {
	f := initFixture(t)

	// Explicitly set zero funding
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 1, math.ZeroInt()))

	amount := f.keeper.GetDayFunding(f.ctx, 1)
	require.True(t, amount.IsZero())
}

func TestDayFundingLargeValues(t *testing.T) {
	f := initFixture(t)

	large := math.NewInt(999_999_999_999)
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 100000, large))

	amount := f.keeper.GetDayFunding(f.ctx, 100000)
	require.Equal(t, large, amount)
}

func TestPruneDayFundingsEmpty(t *testing.T) {
	f := initFixture(t)

	// Prune on empty state should not error
	err := f.keeper.PruneDayFundings(f.ctx, 100)
	require.NoError(t, err)
}

func TestPruneDayFundingsAll(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 1, math.NewInt(100)))
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 2, math.NewInt(200)))
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 3, math.NewInt(300)))

	// Prune everything
	err := f.keeper.PruneDayFundings(f.ctx, 999)
	require.NoError(t, err)

	require.True(t, f.keeper.GetDayFunding(f.ctx, 1).IsZero())
	require.True(t, f.keeper.GetDayFunding(f.ctx, 2).IsZero())
	require.True(t, f.keeper.GetDayFunding(f.ctx, 3).IsZero())
}

func TestPruneDayFundingsExactBoundary(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 5, math.NewInt(500)))
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 6, math.NewInt(600)))

	// cutoffDay=6 should prune day 5 but keep day 6
	err := f.keeper.PruneDayFundings(f.ctx, 6)
	require.NoError(t, err)

	require.True(t, f.keeper.GetDayFunding(f.ctx, 5).IsZero())
	require.Equal(t, math.NewInt(600), f.keeper.GetDayFunding(f.ctx, 6))
}
