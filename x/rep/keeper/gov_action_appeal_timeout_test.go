package keeper_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestTimeoutExpiredAppeals_NoPending(t *testing.T) {
	f := initFixture(t)
	// Empty store — just exercise the happy path.
	require.NoError(t, f.keeper.TimeoutExpiredAppeals(f.ctx))
}

func TestTimeoutExpiredAppeals_TransitionsAndSplitsBond(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	// Track bank interactions from the timeout path.
	var refunded sdk.Coins
	var burned sdk.Coins
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, module string, _ sdk.AccAddress, amt sdk.Coins) error {
		require.Equal(t, types.ModuleName, module)
		refunded = amt
		return nil
	}
	f.bankKeeper.BurnCoinsFn = func(_ context.Context, module string, amt sdk.Coins) error {
		require.Equal(t, types.ModuleName, module)
		burned = amt
		return nil
	}

	// Advance block time so the expired appeal is past its deadline.
	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(2000, 0))

	appellant := sdk.AccAddress([]byte("appellant")).String()
	expired := types.GovActionAppeal{
		Appellant:  appellant,
		Status:     types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING,
		Deadline:   1000,  // < 2000, should time out
		AppealBond: "100", // half refunded, half burned
	}
	require.NoError(t, k.GovActionAppeal.Set(sdkCtx, 1, expired))

	notYet := types.GovActionAppeal{
		Appellant:  appellant,
		Status:     types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING,
		Deadline:   3000, // > 2000, should stay pending
		AppealBond: "50",
	}
	require.NoError(t, k.GovActionAppeal.Set(sdkCtx, 2, notYet))

	resolved := types.GovActionAppeal{
		Appellant:  appellant,
		Status:     types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD,
		Deadline:   1000, // expired but already resolved — must be skipped
		AppealBond: "200",
	}
	require.NoError(t, k.GovActionAppeal.Set(sdkCtx, 3, resolved))

	require.NoError(t, k.TimeoutExpiredAppeals(sdkCtx))

	got, err := k.GovActionAppeal.Get(sdkCtx, 1)
	require.NoError(t, err)
	require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT, got.Status)

	got, err = k.GovActionAppeal.Get(sdkCtx, 2)
	require.NoError(t, err)
	require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING, got.Status, "future deadline must be untouched")

	got, err = k.GovActionAppeal.Get(sdkCtx, 3)
	require.NoError(t, err)
	require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD, got.Status, "non-pending appeals are skipped")

	require.Len(t, refunded, 1)
	require.Equal(t, "50", refunded[0].Amount.String(), "half of bond is refunded")
	require.Equal(t, types.RewardDenom, refunded[0].Denom)

	require.Len(t, burned, 1)
	require.Equal(t, "50", burned[0].Amount.String(), "half of bond is burned")
	require.Equal(t, types.RewardDenom, burned[0].Denom)
}

func TestTimeoutExpiredAppeals_ZeroBondSkipsBankCalls(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	// If the split-bond path is exercised for a zero bond we want the test to
	// fail loudly — neither bank method should be invoked.
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(context.Context, string, sdk.AccAddress, sdk.Coins) error {
		t.Fatalf("refund should not run for zero bond")
		return nil
	}
	f.bankKeeper.BurnCoinsFn = func(context.Context, string, sdk.Coins) error {
		t.Fatalf("burn should not run for zero bond")
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(2000, 0))

	require.NoError(t, k.GovActionAppeal.Set(sdkCtx, 1, types.GovActionAppeal{
		Appellant:  sdk.AccAddress([]byte("appellant")).String(),
		Status:     types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING,
		Deadline:   1000,
		AppealBond: "0",
	}))

	require.NoError(t, k.TimeoutExpiredAppeals(sdkCtx))

	got, err := k.GovActionAppeal.Get(sdkCtx, 1)
	require.NoError(t, err)
	require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT, got.Status)
}
