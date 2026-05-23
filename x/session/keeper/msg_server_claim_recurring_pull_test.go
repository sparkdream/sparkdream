package keeper_test

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// createRecurringPullGrant is a test helper that drives MsgCreateGrant
// and returns the allocated grant id.
func createRecurringPullGrant(t *testing.T, f *fixture, ms types.MsgServer, granter, grantee string, amount sdk.Coin, periodSeconds int64, expiresAt time.Time) uint64 {
	t.Helper()
	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: expiresAt,
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: amount,
				PeriodSeconds:   periodSeconds,
			},
		},
	})
	require.NoError(t, err)
	require.Greater(t, resp.GrantId, uint64(0))
	return resp.GrantId
}

// withBlockTime returns a fresh ctx whose BlockTime is `t`. Used by the
// claim tests to simulate the period elapsing.
func withBlockTime(ctx context.Context, t time.Time) context.Context {
	return sdk.UnwrapSDKContext(ctx).WithBlockTime(t)
}

func TestClaimRecurringPull_NotDue(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)

	id := createRecurringPullGrant(t, f, ms, granter, grantee, amount, 86_400, sdkCtx.BlockTime().Add(30*24*time.Hour))

	// Claim immediately — should fail (first window opens at start + period).
	_, err := ms.ClaimRecurringPull(f.ctx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: id,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "next claim window opens at")
}

func TestClaimRecurringPull_HappyPath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)
	period := int64(86_400)
	expiresAt := sdkCtx.BlockTime().Add(30 * 24 * time.Hour)

	id := createRecurringPullGrant(t, f, ms, granter, grantee, amount, period, expiresAt)

	// Track bank SendCoins calls.
	var sends []sdk.Coins
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, amt sdk.Coins) error {
		sends = append(sends, amt)
		return nil
	}

	// Advance block time by one period + 1s.
	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))

	resp, err := ms.ClaimRecurringPull(futureCtx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: id,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp.ClaimNumber)
	require.Len(t, sends, 1)
	require.True(t, sends[0].Equal(sdk.NewCoins(amount)))

	g, err := f.keeper.Grants.Get(futureCtx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)
	rp := g.GetRecurringPull()
	require.NotNil(t, rp)
	require.Equal(t, uint64(1), rp.ClaimsMade)
	// LastClaimAdvance advanced by exactly one period from start_time.
	require.Equal(t, sdkCtx.BlockTime().Unix()+period, rp.LastClaimAdvance)
}

func TestClaimRecurringPull_UnauthorizedCaller(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	imposter := testAddr("imposter", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)
	period := int64(86_400)

	id := createRecurringPullGrant(t, f, ms, granter, grantee, amount, period, sdkCtx.BlockTime().Add(30*24*time.Hour))

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))

	_, err := ms.ClaimRecurringPull(futureCtx, &types.MsgClaimRecurringPull{
		Grantee: imposter,
		GrantId: id,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "caller")
}

func TestClaimRecurringPull_PausedOnUnderfunded(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)
	period := int64(86_400)

	id := createRecurringPullGrant(t, f, ms, granter, grantee, amount, period, sdkCtx.BlockTime().Add(30*24*time.Hour))

	// First send fails (granter underfunded), subsequent succeeds.
	sendCalls := 0
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
		sendCalls++
		if sendCalls == 1 {
			return errors.New("insufficient funds: 0uspark")
		}
		return nil
	}

	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))
	_, err := ms.ClaimRecurringPull(futureCtx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: id,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient")

	// Grant should be PAUSED.
	g, err := f.keeper.Grants.Get(futureCtx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS, g.Status)

	// Retry from the paused state — succeeds, flips back to ACTIVE.
	resp, err := ms.ClaimRecurringPull(futureCtx, &types.MsgClaimRecurringPull{
		Grantee: grantee,
		GrantId: id,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp.ClaimNumber)

	g, err = f.keeper.Grants.Get(futureCtx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)
}

func TestClaimRecurringPull_EpochCeilingExceeded(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	amount := sdk.NewInt64Coin("uspark", 1_000_000)
	period := int64(60 * 60) // 1h — short period so we can drive multiple claims within one UTC day
	expiresAt := sdkCtx.BlockTime().Add(48 * time.Hour)

	// Override min period for this test.
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MinRecurringPeriodSeconds = 60
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: expiresAt,
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: amount,
				PeriodSeconds:   period,
				MaxPerEpoch:     "1500000", // ceiling = 1.5 * amount, so 2nd claim same day breaches
			},
		},
	})
	require.NoError(t, err)
	id := resp.GrantId

	// First claim (one period later).
	ctx1 := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(period+1)*time.Second))
	_, err = ms.ClaimRecurringPull(ctx1, &types.MsgClaimRecurringPull{Grantee: grantee, GrantId: id})
	require.NoError(t, err)

	// Second claim same UTC day — should breach max_per_epoch.
	ctx2 := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(time.Duration(2*period+1)*time.Second))
	_, err = ms.ClaimRecurringPull(ctx2, &types.MsgClaimRecurringPull{Grantee: grantee, GrantId: id})
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_per_epoch")
}
