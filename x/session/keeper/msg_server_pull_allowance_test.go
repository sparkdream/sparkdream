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

// createAllowance is a test helper that drives MsgCreateGrant for a
// SpendingAllowance payload and returns the allocated grant id.
func createAllowance(
	t *testing.T,
	f *fixture,
	ms types.MsgServer,
	granter, grantee string,
	maxPerPeriod sdk.Coin,
	periodSeconds int64,
	allowedRecipients []string,
	expiresAt time.Time,
) uint64 {
	t.Helper()
	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: expiresAt,
		Payload: &types.MsgCreateGrant_SpendingAllowance{
			SpendingAllowance: &types.SpendingAllowancePayload{
				MaxPerPeriod:      maxPerPeriod,
				PeriodSeconds:     periodSeconds,
				AllowedRecipients: allowedRecipients,
				Denom:             maxPerPeriod.Denom,
			},
		},
	})
	require.NoError(t, err)
	require.Greater(t, resp.GrantId, uint64(0))
	return resp.GrantId
}

func TestCreateGrant_SpendingAllowance_HappyPath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 1_000_000),
		3_600, // 1h
		[]string{testAddr("rec_a", f.addressCodec), testAddr("rec_b", f.addressCodec)},
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	g, err := f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE, g.Type)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)

	sa := g.GetSpendingAllowance()
	require.NotNil(t, sa)
	// Current period anchored to block time.
	require.Equal(t, sdkCtx.BlockTime().Unix(), sa.CurrentPeriodStart)
	// Spent zeroed.
	require.True(t, sa.SpentInCurrentPeriod.Amount.IsZero())
	// Denom locked.
	require.Equal(t, "uspark", sa.Denom)

	// Active counter bumped.
	count, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE)
	require.NoError(t, err)
	require.Equal(t, uint32(1), count)
}

func TestCreateGrant_SpendingAllowance_Validation(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	futureExp := sdkCtx.BlockTime().Add(72 * time.Hour)

	tests := []struct {
		name        string
		payload     *types.SpendingAllowancePayload
		errContains string
	}{
		{
			name: "period too short",
			payload: &types.SpendingAllowancePayload{
				MaxPerPeriod:  sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds: 60, // below 3600 default
			},
			errContains: "below params.min_allowance_period_seconds",
		},
		{
			name: "non-positive max_per_period",
			payload: &types.SpendingAllowancePayload{
				MaxPerPeriod:  sdk.NewInt64Coin("uspark", 0),
				PeriodSeconds: 3_600,
			},
			errContains: "max_per_period must be positive",
		},
		{
			name: "dream forbidden",
			payload: &types.SpendingAllowancePayload{
				MaxPerPeriod:  sdk.NewInt64Coin("dream", 1_000_000),
				PeriodSeconds: 3_600,
			},
			errContains: "dream denom is forbidden",
		},
		{
			name: "denom not allowed",
			payload: &types.SpendingAllowancePayload{
				MaxPerPeriod:  sdk.NewInt64Coin("uatom", 1_000_000),
				PeriodSeconds: 3_600,
			},
			errContains: "denom is not in",
		},
		{
			name: "denom field mismatch",
			payload: &types.SpendingAllowancePayload{
				MaxPerPeriod:  sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds: 3_600,
				Denom:         "udream",
			},
			errContains: "denom field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
				Granter:   granter,
				Grantee:   grantee,
				ExpiresAt: futureExp,
				Payload:   &types.MsgCreateGrant_SpendingAllowance{SpendingAllowance: tt.payload},
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestCreateGrant_SpendingAllowance_RecipientListTooLong(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// Generate 51 addresses to exceed the default 50-entry cap.
	rec := make([]string, 51)
	for i := range rec {
		rec[i] = testAddr("rec"+string(rune('a'+i%26))+string(rune('a'+i/26)), f.addressCodec)
	}

	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(24 * time.Hour),
		Payload: &types.MsgCreateGrant_SpendingAllowance{
			SpendingAllowance: &types.SpendingAllowancePayload{
				MaxPerPeriod:      sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:     3_600,
				AllowedRecipients: rec,
				Denom:             "uspark",
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds params.max_allowance_recipient_list")
}

func TestPullAllowance_HappyPath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 5_000_000),
		3_600,
		nil, // empty whitelist = unrestricted
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	var sends []sdk.Coins
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, amt sdk.Coins) error {
		sends = append(sends, amt)
		return nil
	}

	resp, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    sdk.NewInt64Coin("uspark", 1_500_000),
	})
	require.NoError(t, err)
	require.Len(t, sends, 1)
	require.True(t, sends[0].Equal(sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_500_000))))
	require.Equal(t, "1500000uspark", resp.SpentInPeriod.String())

	g, err := f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	sa := g.GetSpendingAllowance()
	require.Equal(t, "1500000", sa.SpentInCurrentPeriod.Amount.String())
}

func TestPullAllowance_RecipientNotWhitelisted(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	whitelisted := testAddr("whitelisted", f.addressCodec)
	stranger := testAddr("stranger", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 5_000_000),
		3_600,
		[]string{whitelisted},
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: stranger,
		Amount:    sdk.NewInt64Coin("uspark", 1_500_000),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in the grant's allowed_recipients")
}

func TestPullAllowance_RecipientIsGranter(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 5_000_000),
		3_600,
		nil,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: granter, // self-roundtrip
		Amount:    sdk.NewInt64Coin("uspark", 1_500_000),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "recipient must not be the granter")
}

func TestPullAllowance_BelowMinPullAmount(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 5_000_000),
		3_600,
		nil,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	// 500 < default 1000.
	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    sdk.NewInt64Coin("uspark", 500),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "below params.min_pull_amount")
}

func TestPullAllowance_BudgetExceeded(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 2_000_000),
		3_600,
		nil,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	// First pull: 1.5M, fine.
	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    sdk.NewInt64Coin("uspark", 1_500_000),
	})
	require.NoError(t, err)

	// Second pull: 1M same window → 2.5M total > 2M cap.
	_, err = ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    sdk.NewInt64Coin("uspark", 1_000_000),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_per_period")
}

func TestPullAllowance_RollingWindowReset(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 2_000_000),
		3_600, // 1h window
		nil,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	// First pull at block_time.
	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    sdk.NewInt64Coin("uspark", 1_500_000),
	})
	require.NoError(t, err)

	// Advance 2h — window rolls over.
	futureCtx := withBlockTime(f.ctx, sdkCtx.BlockTime().Add(2*time.Hour))
	resp, err := ms.PullAllowance(futureCtx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    sdk.NewInt64Coin("uspark", 1_500_000),
	})
	require.NoError(t, err)
	// spent_in_period reset, so it's only 1.5M after this pull (not 3M).
	require.Equal(t, "1500000uspark", resp.SpentInPeriod.String())
}

func TestPullAllowance_PausedOnUnderfunded(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 5_000_000),
		3_600,
		nil,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	// First send fails, second succeeds.
	calls := 0
	f.bankKeeper.SendCoinsFn = func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) error {
		calls++
		if calls == 1 {
			return errors.New("insufficient funds")
		}
		return nil
	}

	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    sdk.NewInt64Coin("uspark", 1_500_000),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient")

	g, err := f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS, g.Status)
	// Period clock NOT reset on failure (defends against malicious-recipient reset).
	require.Equal(t, sdkCtx.BlockTime().Unix(), g.GetSpendingAllowance().CurrentPeriodStart)

	// Retry — succeeds.
	_, err = ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   grantee,
		GrantId:   id,
		Recipient: recipient,
		Amount:    sdk.NewInt64Coin("uspark", 1_500_000),
	})
	require.NoError(t, err)

	g, err = f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)
}

func TestPullAllowance_UnauthorizedGrantee(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	imposter := testAddr("imposter", f.addressCodec)
	recipient := testAddr("recipient", f.addressCodec)

	id := createAllowance(t, f, ms, granter, grantee,
		sdk.NewInt64Coin("uspark", 5_000_000),
		3_600,
		nil,
		sdkCtx.BlockTime().Add(30*24*time.Hour))

	_, err := ms.PullAllowance(f.ctx, &types.MsgPullAllowance{
		Grantee:   imposter,
		GrantId:   id,
		Recipient: recipient,
		Amount:    sdk.NewInt64Coin("uspark", 1_500_000),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "caller")
}
