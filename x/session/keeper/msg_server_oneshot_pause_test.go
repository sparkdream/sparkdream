package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// underfundedSend makes the mock bank's SendCoins fail the way x/bank
// does for an underfunded sender.
func underfundedSend(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
	return sdkerrors.ErrInsufficientFunds.Wrap("spendable balance 0uspark is smaller than 5000000uspark")
}

// TestFire_Transfer_InsufficientFundsPauses: a fire-time insufficient-
// funds failure pauses the grant instead of firing it. The deposit stays
// in escrow, the active slot stays pinned, and the paused-TTL index is
// keyed on the pause time.
func TestFire_Transfer_InsufficientFundsPauses(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id, deposit := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 5_000_000),
		sdkCtx.BlockTime().Add(2*time.Hour).Unix(),
		sdkCtx.BlockTime().Add(24*time.Hour))

	f.bankKeeper.SendCoinsFn = underfundedSend
	var toFeeCollector int
	f.bankKeeper.SendCoinsFromModuleToModuleFn = func(_ context.Context, _, _ string, _ sdk.Coins) error {
		toFeeCollector++
		return nil
	}

	pauseTime := sdkCtx.BlockTime().Add(3 * time.Hour)
	pauseCtx := sdk.UnwrapSDKContext(withBlockTime(f.ctx, pauseTime))
	require.NoError(t, f.keeper.EndBlocker(pauseCtx))

	g, err := f.keeper.Grants.Get(pauseCtx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS, g.Status)
	so := g.GetScheduledOneshot()
	require.Equal(t, pauseTime.Unix(), so.FireAt, "fire_at records the pause time")
	require.Contains(t, so.FireError, "insufficient funds")

	// Deposit kept, nothing sent to the fee collector.
	require.Zero(t, toFeeCollector)
	held, err := f.keeper.OneshotGasDeposit.Get(pauseCtx, id)
	require.NoError(t, err)
	require.True(t, held.Equal(deposit))

	// Active slot still pinned.
	count, err := f.keeper.CountActiveGrants(pauseCtx, granter, types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	// Paused-TTL index keyed on the pause time.
	has, err := f.keeper.PausedOneshotByPauseTime.Has(pauseCtx, collections.Join(pauseTime.Unix(), id))
	require.NoError(t, err)
	require.True(t, has)

	var sawPause bool
	for _, ev := range pauseCtx.EventManager().Events() {
		if ev.Type == "grant_paused_underfunded" {
			sawPause = true
		}
	}
	require.True(t, sawPause)

	// A paused grant is not re-fired by later fire passes.
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
		t.Fatal("paused oneshot must not fire without a retry")
		return nil
	}
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(withBlockTime(f.ctx, pauseTime.Add(time.Minute)))))
}

// TestRetryScheduledOneshot_FiresAfterTopUp: retry flips the paused
// grant back to ACTIVE, the same block's fire pass fires it, and the
// paused-TTL index entry is gone.
func TestRetryScheduledOneshot_FiresAfterTopUp(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 5_000_000),
		sdkCtx.BlockTime().Add(2*time.Hour).Unix(),
		sdkCtx.BlockTime().Add(24*time.Hour))

	f.bankKeeper.SendCoinsFn = underfundedSend
	pauseTime := sdkCtx.BlockTime().Add(3 * time.Hour)
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(withBlockTime(f.ctx, pauseTime))))

	// Granter tops up; grantee retries an hour later.
	var sends []sdk.Coins
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, amt sdk.Coins) error {
		sends = append(sends, amt)
		return nil
	}
	var toFeeCollector int
	f.bankKeeper.SendCoinsFromModuleToModuleFn = func(_ context.Context, _, _ string, _ sdk.Coins) error {
		toFeeCollector++
		return nil
	}
	retryTime := pauseTime.Add(time.Hour)
	retryCtx := sdk.UnwrapSDKContext(withBlockTime(f.ctx, retryTime))
	resp, err := ms.RetryScheduledOneshot(retryCtx, &types.MsgRetryScheduledOneshot{
		Caller:  grantee,
		GrantId: id,
	})
	require.NoError(t, err)
	require.Equal(t, retryTime.Unix(), resp.NewFireAt)

	has, err := f.keeper.PausedOneshotByPauseTime.Has(retryCtx, collections.Join(pauseTime.Unix(), id))
	require.NoError(t, err)
	require.False(t, has, "retry drops the paused-TTL index entry")

	require.NoError(t, f.keeper.EndBlocker(retryCtx))

	g, err := f.keeper.Grants.Get(retryCtx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_FIRED, g.Status)
	require.Empty(t, g.GetScheduledOneshot().FireError)
	require.Len(t, sends, 1)
	require.True(t, sends[0].Equal(sdk.NewCoins(sdk.NewInt64Coin("uspark", 5_000_000))))
	require.Equal(t, 1, toFeeCollector, "deposit pays for the successful attempt")

	count, err := f.keeper.CountActiveGrants(retryCtx, granter, types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT)
	require.NoError(t, err)
	require.Zero(t, count)
}

// TestAutoRevokePaused_TTLCountsFromPause: a oneshot scheduled further
// out than paused_oneshot_ttl_seconds is NOT revoked right after it
// pauses; it is revoked once the TTL has elapsed since the pause, with
// the deposit refunded and the active slot released.
func TestAutoRevokePaused_TTLCountsFromPause(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	ttl := time.Duration(params.PausedOneshotTtlSeconds) * time.Second

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	// Fire 10 days out — beyond the 7-day TTL measured from creation.
	start := sdkCtx.BlockTime()
	fireAt := start.Add(10 * 24 * time.Hour)
	id, deposit := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 5_000_000),
		fireAt.Unix(),
		start.Add(30*24*time.Hour))

	f.bankKeeper.SendCoinsFn = underfundedSend
	var refunds []sdk.Coins
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, amt sdk.Coins) error {
		refunds = append(refunds, amt)
		return nil
	}

	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(withBlockTime(f.ctx, fireAt))))

	// The next block (and one just short of the TTL) leaves it paused.
	for _, at := range []time.Time{fireAt.Add(time.Minute), fireAt.Add(ttl - time.Second)} {
		ctx := sdk.UnwrapSDKContext(withBlockTime(f.ctx, at))
		require.NoError(t, f.keeper.EndBlocker(ctx))
		g, err := f.keeper.Grants.Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS, g.Status)
	}
	require.Empty(t, refunds)

	revokeCtx := sdk.UnwrapSDKContext(withBlockTime(f.ctx, fireAt.Add(ttl)))
	require.NoError(t, f.keeper.EndBlocker(revokeCtx))

	_, err = f.keeper.Grants.Get(revokeCtx, id)
	require.ErrorIs(t, err, collections.ErrNotFound)
	require.Len(t, refunds, 1)
	require.True(t, refunds[0].Equal(sdk.NewCoins(deposit)))
	_, err = f.keeper.OneshotGasDeposit.Get(revokeCtx, id)
	require.ErrorIs(t, err, collections.ErrNotFound)

	has, err := f.keeper.PausedOneshotByPauseTime.Has(revokeCtx, collections.Join(fireAt.Unix(), id))
	require.NoError(t, err)
	require.False(t, has)

	count, err := f.keeper.CountActiveGrants(revokeCtx, granter, types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT)
	require.NoError(t, err)
	require.Zero(t, count, "auto-revoke releases the paused grant's active slot")

	var sawRevoke bool
	for _, ev := range revokeCtx.EventManager().Events() {
		if ev.Type == "grant_auto_revoked" {
			sawRevoke = true
		}
	}
	require.True(t, sawRevoke)
}

// TestFire_Transfer_PausedCapFallsBackToFired: once a granter holds
// max_paused_oneshots_per_granter paused oneshots, a further underfunded
// fire is terminal (FIRED-with-error, deposit to the fee collector).
func TestFire_Transfer_PausedCapFallsBackToFired(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxPausedOneshotsPerGranter = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	amount := sdk.NewInt64Coin("uspark", 5_000_000)
	expiresAt := sdkCtx.BlockTime().Add(24 * time.Hour)
	first, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient, amount,
		sdkCtx.BlockTime().Add(2*time.Hour).Unix(), expiresAt)
	second, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient, amount,
		sdkCtx.BlockTime().Add(2*time.Hour+time.Minute).Unix(), expiresAt)

	f.bankKeeper.SendCoinsFn = underfundedSend
	var toFeeCollector int
	f.bankKeeper.SendCoinsFromModuleToModuleFn = func(_ context.Context, _, _ string, _ sdk.Coins) error {
		toFeeCollector++
		return nil
	}

	ctx := sdk.UnwrapSDKContext(withBlockTime(f.ctx, sdkCtx.BlockTime().Add(3*time.Hour)))
	require.NoError(t, f.keeper.EndBlocker(ctx))

	g1, err := f.keeper.Grants.Get(ctx, first)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS, g1.Status)

	g2, err := f.keeper.Grants.Get(ctx, second)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_FIRED, g2.Status)
	require.Contains(t, g2.GetScheduledOneshot().FireError, "insufficient funds")
	require.Equal(t, 1, toFeeCollector)
}

// TestRevokeGrant_PausedOneshotClearsPauseIndex: revoking a paused
// oneshot drops its paused-TTL index entry along with the grant.
func TestRevokeGrant_PausedOneshotClearsPauseIndex(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 5_000_000),
		sdkCtx.BlockTime().Add(2*time.Hour).Unix(),
		sdkCtx.BlockTime().Add(24*time.Hour))

	f.bankKeeper.SendCoinsFn = underfundedSend
	pauseTime := sdkCtx.BlockTime().Add(3 * time.Hour)
	ctx := sdk.UnwrapSDKContext(withBlockTime(f.ctx, pauseTime))
	require.NoError(t, f.keeper.EndBlocker(ctx))

	_, err := ms.RevokeGrant(ctx, &types.MsgRevokeGrant{Granter: granter, GrantId: id})
	require.NoError(t, err)

	has, err := f.keeper.PausedOneshotByPauseTime.Has(ctx, collections.Join(pauseTime.Unix(), id))
	require.NoError(t, err)
	require.False(t, has)
	count, err := f.keeper.CountActiveGrants(ctx, granter, types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT)
	require.NoError(t, err)
	require.Zero(t, count)
}

// TestGenesis_RebuildsPausedOneshotIndex: the paused-TTL index is not
// exported; InitGenesis rebuilds it from the paused grant's fire_at.
func TestGenesis_RebuildsPausedOneshotIndex(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id, _ := createTransferOneshot(t, f, ms, granter, grantee, recipient,
		sdk.NewInt64Coin("uspark", 5_000_000),
		sdkCtx.BlockTime().Add(2*time.Hour).Unix(),
		sdkCtx.BlockTime().Add(24*time.Hour))

	f.bankKeeper.SendCoinsFn = underfundedSend
	pauseTime := sdkCtx.BlockTime().Add(3 * time.Hour)
	require.NoError(t, f.keeper.EndBlocker(sdk.UnwrapSDKContext(withBlockTime(f.ctx, pauseTime))))

	gen, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)

	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *gen))
	has, err := f2.keeper.PausedOneshotByPauseTime.Has(f2.ctx, collections.Join(pauseTime.Unix(), id))
	require.NoError(t, err)
	require.True(t, has)
}
