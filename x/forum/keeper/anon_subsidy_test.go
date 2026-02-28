package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

// --- GetAnonSubsidyUsed / SetAnonSubsidyUsed ---

func TestAnonSubsidy_GetSetRoundTrip(t *testing.T) {
	f := initFixture(t)

	// Initially should return zero
	used := f.keeper.GetAnonSubsidyUsed(f.ctx, 1)
	require.True(t, used.Amount.IsZero(), "expected zero subsidy used for fresh epoch")
	require.Equal(t, types.DefaultFeeDenom, used.Denom)

	// Set a value and read it back
	coin := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(5000))
	f.keeper.SetAnonSubsidyUsed(f.ctx, 1, coin)

	got := f.keeper.GetAnonSubsidyUsed(f.ctx, 1)
	require.Equal(t, coin, got)

	// Different epoch should still be zero
	got2 := f.keeper.GetAnonSubsidyUsed(f.ctx, 2)
	require.True(t, got2.Amount.IsZero())
}

func TestAnonSubsidy_GetSetMultipleEpochs(t *testing.T) {
	f := initFixture(t)

	coin1 := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(100))
	coin2 := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(200))

	f.keeper.SetAnonSubsidyUsed(f.ctx, 10, coin1)
	f.keeper.SetAnonSubsidyUsed(f.ctx, 20, coin2)

	require.Equal(t, coin1, f.keeper.GetAnonSubsidyUsed(f.ctx, 10))
	require.Equal(t, coin2, f.keeper.GetAnonSubsidyUsed(f.ctx, 20))
}

// --- IsApprovedRelay ---

func TestIsApprovedRelay(t *testing.T) {
	f := initFixture(t)

	tests := []struct {
		name     string
		relays   []string
		addr     string
		expected bool
	}{
		{
			name:     "approved relay found",
			relays:   []string{testCreator, testCreator2},
			addr:     testCreator,
			expected: true,
		},
		{
			name:     "approved relay second entry",
			relays:   []string{testCreator, testCreator2},
			addr:     testCreator2,
			expected: true,
		},
		{
			name:     "not approved relay",
			relays:   []string{testCreator},
			addr:     testCreator2,
			expected: false,
		},
		{
			name:     "empty relays list",
			relays:   nil,
			addr:     testCreator,
			expected: false,
		},
		{
			name:     "empty address",
			relays:   []string{testCreator},
			addr:     "",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := types.DefaultParams()
			params.AnonSubsidyApprovedRelays = tc.relays
			result := f.keeper.IsApprovedRelay(params, tc.addr)
			require.Equal(t, tc.expected, result)
		})
	}
}

// --- TrySubsidizeAnonymousAction ---

func TestTrySubsidize_NotApprovedRelay(t *testing.T) {
	f := initFixture(t)

	params := types.DefaultParams()
	params.AnonSubsidyBudgetPerEpoch = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(100000))
	params.AnonSubsidyMaxPerPost = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(10000))
	params.AnonSubsidyApprovedRelays = []string{testCreator}

	feeCost := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(5000))

	// testCreator2 is not an approved relay
	result := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator2, feeCost, 1)
	require.True(t, result.Amount.IsZero(), "non-approved relay should get zero subsidy")
}

func TestTrySubsidize_BudgetNotConfigured(t *testing.T) {
	f := initFixture(t)

	params := types.DefaultParams()
	// Default budget is zero
	params.AnonSubsidyApprovedRelays = []string{testCreator}

	feeCost := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(5000))

	result := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator, feeCost, 1)
	require.True(t, result.Amount.IsZero(), "zero budget should return zero subsidy")
}

func TestTrySubsidize_SuccessfulSubsidy(t *testing.T) {
	f := initFixture(t)

	params := types.DefaultParams()
	params.AnonSubsidyBudgetPerEpoch = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(100000))
	params.AnonSubsidyMaxPerPost = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(10000))
	params.AnonSubsidyApprovedRelays = []string{testCreator}

	feeCost := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(5000))

	result := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator, feeCost, 1)
	require.Equal(t, math.NewInt(5000), result.Amount, "should subsidize full fee cost when under max and budget")

	// Check that subsidy used was updated
	used := f.keeper.GetAnonSubsidyUsed(f.ctx, 1)
	require.Equal(t, math.NewInt(5000), used.Amount)
}

func TestTrySubsidize_CappedByMaxPerPost(t *testing.T) {
	f := initFixture(t)

	params := types.DefaultParams()
	params.AnonSubsidyBudgetPerEpoch = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(100000))
	params.AnonSubsidyMaxPerPost = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(3000))
	params.AnonSubsidyApprovedRelays = []string{testCreator}

	feeCost := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(5000))

	result := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator, feeCost, 1)
	require.Equal(t, math.NewInt(3000), result.Amount, "subsidy should be capped by max per post")

	// Verify subsidy tracking
	used := f.keeper.GetAnonSubsidyUsed(f.ctx, 1)
	require.Equal(t, math.NewInt(3000), used.Amount)
}

func TestTrySubsidize_BudgetExhausted(t *testing.T) {
	f := initFixture(t)

	params := types.DefaultParams()
	params.AnonSubsidyBudgetPerEpoch = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(10000))
	params.AnonSubsidyMaxPerPost = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(10000))
	params.AnonSubsidyApprovedRelays = []string{testCreator}

	// Pre-exhaust the budget
	f.keeper.SetAnonSubsidyUsed(f.ctx, 1, sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(10000)))

	feeCost := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(5000))

	result := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator, feeCost, 1)
	require.True(t, result.Amount.IsZero(), "exhausted budget should return zero subsidy")
}

func TestTrySubsidize_CappedByRemainingBudget(t *testing.T) {
	f := initFixture(t)

	params := types.DefaultParams()
	params.AnonSubsidyBudgetPerEpoch = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(10000))
	params.AnonSubsidyMaxPerPost = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(8000))
	params.AnonSubsidyApprovedRelays = []string{testCreator}

	// Use up 7000 of 10000 budget
	f.keeper.SetAnonSubsidyUsed(f.ctx, 1, sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(7000)))

	feeCost := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(5000))

	result := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator, feeCost, 1)
	// Remaining budget is 3000, fee is 5000, max per post is 8000 -> capped to 3000
	require.Equal(t, math.NewInt(3000), result.Amount, "subsidy should be capped by remaining budget")

	// Total used should now be 10000
	used := f.keeper.GetAnonSubsidyUsed(f.ctx, 1)
	require.Equal(t, math.NewInt(10000), used.Amount)
}

func TestTrySubsidize_MultipleActionsDeductCorrectly(t *testing.T) {
	f := initFixture(t)

	params := types.DefaultParams()
	params.AnonSubsidyBudgetPerEpoch = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(15000))
	params.AnonSubsidyMaxPerPost = sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(6000))
	params.AnonSubsidyApprovedRelays = []string{testCreator}

	feeCost := sdk.NewCoin(types.DefaultFeeDenom, math.NewInt(5000))

	// First action: 5000 subsidized
	result1 := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator, feeCost, 1)
	require.Equal(t, math.NewInt(5000), result1.Amount)

	// Second action: 5000 subsidized (10000 total used)
	result2 := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator, feeCost, 1)
	require.Equal(t, math.NewInt(5000), result2.Amount)

	// Third action: only 5000 remaining, fee is 5000 -> 5000 subsidized
	result3 := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator, feeCost, 1)
	require.Equal(t, math.NewInt(5000), result3.Amount)

	// Fourth action: budget exhausted
	result4 := f.keeper.TrySubsidizeAnonymousAction(f.ctx, params, testCreator, feeCost, 1)
	require.True(t, result4.Amount.IsZero(), "budget should be exhausted after 3 actions")
}
