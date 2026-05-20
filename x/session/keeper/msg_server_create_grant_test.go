package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestCreateGrant_SessionKey(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	futureExp := sdkCtx.BlockTime().Add(1 * time.Hour)

	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: futureExp,
		Note:      "test session key via umbrella",
		Payload: &types.MsgCreateGrant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: types.DefaultAllowedMsgTypes[:1],
				SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
				MaxExecCount:    100,
			},
		},
	})
	require.NoError(t, err)
	require.Greater(t, resp.GrantId, uint64(0))

	// SessionKey lookup populated.
	id, err := f.keeper.SessionKeyByPair.Get(f.ctx, collections.Join(granter, grantee))
	require.NoError(t, err)
	require.Equal(t, resp.GrantId, id)

	// Grant has SESSION_KEY type.
	g, err := f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.GrantType_GRANT_TYPE_SESSION_KEY, g.Type)
	require.Equal(t, "test session key via umbrella", g.Note)
}

func TestCreateGrant_RecurringPull_HappyPath(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	// Allow space for at least one full period plus an expiry margin.
	futureExp := sdkCtx.BlockTime().Add(72 * time.Hour)

	resp, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: futureExp,
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400, // 1 day
			},
		},
	})
	require.NoError(t, err)
	require.Greater(t, resp.GrantId, uint64(0))

	g, err := f.keeper.Grants.Get(f.ctx, resp.GrantId)
	require.NoError(t, err)
	require.Equal(t, types.GrantType_GRANT_TYPE_RECURRING_PULL, g.Type)
	require.Equal(t, types.GrantStatus_GRANT_STATUS_ACTIVE, g.Status)

	rp := g.GetRecurringPull()
	require.NotNil(t, rp)
	// start_time normalized to block_time when caller left it zero.
	require.Equal(t, sdkCtx.BlockTime().Unix(), rp.StartTime)
	require.Equal(t, rp.StartTime, rp.LastClaimAdvance)
	// max_per_epoch defaulted to 10x amount_per_period.
	require.Equal(t, "10000000", rp.MaxPerEpochUspark)

	// Active grant counter bumped for the RECURRING_PULL slot.
	count, err := f.keeper.CountActiveGrants(f.ctx, granter, types.GrantType_GRANT_TYPE_RECURRING_PULL)
	require.NoError(t, err)
	require.Equal(t, uint32(1), count)
}

func TestCreateGrant_RecurringPull_Validation(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Bump max_grant_lifetime_seconds so the "duration too long" case
	// trips the per-type cap, not the universal lifetime cap.
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxGrantLifetimeSeconds = 2 * 365 * 24 * 60 * 60 // 2 years
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	futureExp := sdkCtx.BlockTime().Add(72 * time.Hour)
	validAmount := sdk.NewInt64Coin("uspark", 1_000_000)

	type tc struct {
		name        string
		payload     *types.RecurringPullPayload
		expiresAt   time.Time
		granter     string
		grantee     string
		errContains string
	}
	tests := []tc{
		{
			name: "period too short",
			payload: &types.RecurringPullPayload{
				AmountPerPeriod: validAmount,
				// testparams build lowers the floor to 5s; pick 1s
				// so the test still catches a below-min period.
				PeriodSeconds: 1,
			},
			expiresAt: futureExp, granter: granter, grantee: grantee,
			errContains: "below params.min_recurring_period_seconds",
		},
		{
			name: "duration too long",
			payload: &types.RecurringPullPayload{
				AmountPerPeriod: validAmount,
				PeriodSeconds:   86_400,
			},
			expiresAt: sdkCtx.BlockTime().Add(400 * 24 * time.Hour),
			granter:   granter, grantee: grantee,
			errContains: "exceeds params.max_recurring_duration_seconds",
		},
		{
			name: "amount zero",
			payload: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 0),
				PeriodSeconds:   86_400,
			},
			expiresAt: futureExp, granter: granter, grantee: grantee,
			errContains: "amount_per_period must be a positive coin",
		},
		{
			name: "denom not allowed",
			payload: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uatom", 1_000_000),
				PeriodSeconds:   86_400,
			},
			expiresAt: futureExp, granter: granter, grantee: grantee,
			errContains: "denom is not in",
		},
		{
			name: "dream denom forbidden",
			payload: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("dream", 1_000_000),
				PeriodSeconds:   86_400,
			},
			expiresAt: futureExp, granter: granter, grantee: grantee,
			errContains: "dream denom is forbidden",
		},
		{
			name: "max_per_epoch below amount",
			payload: &types.RecurringPullPayload{
				AmountPerPeriod:   validAmount,
				PeriodSeconds:     86_400,
				MaxPerEpochUspark: "500", // below 1_000_000
			},
			expiresAt: futureExp, granter: granter, grantee: grantee,
			errContains: "max_per_epoch_uspark below",
		},
		{
			name: "max_per_epoch unparseable",
			payload: &types.RecurringPullPayload{
				AmountPerPeriod:   validAmount,
				PeriodSeconds:     86_400,
				MaxPerEpochUspark: "not-a-number",
			},
			expiresAt: futureExp, granter: granter, grantee: grantee,
			errContains: "max_per_epoch_uspark must parse",
		},
		{
			name: "self delegation",
			payload: &types.RecurringPullPayload{
				AmountPerPeriod: validAmount,
				PeriodSeconds:   86_400,
			},
			expiresAt: futureExp, granter: granter, grantee: granter,
			errContains: "granter == grantee",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
				Granter:   tt.granter,
				Grantee:   tt.grantee,
				ExpiresAt: tt.expiresAt,
				Payload:   &types.MsgCreateGrant_RecurringPull{RecurringPull: tt.payload},
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestCreateGrant_NoteTooLong(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}

	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(72 * time.Hour),
		Note:      string(long),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds 256 characters")
}

func TestCreateGrant_LifetimeTooLong(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// 2 years > 1 year default lifetime cap.
	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: sdkCtx.BlockTime().Add(2 * 365 * 24 * time.Hour),
		Payload: &types.MsgCreateGrant_RecurringPull{
			RecurringPull: &types.RecurringPullPayload{
				AmountPerPeriod: sdk.NewInt64Coin("uspark", 1_000_000),
				PeriodSeconds:   86_400,
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_grant_lifetime_seconds")
}

func TestCreateGrant_MissingPayload(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	futureExp := sdkCtx.BlockTime().Add(24 * time.Hour)

	// Empty payload oneof should reject at the umbrella.
	_, err := ms.CreateGrant(f.ctx, &types.MsgCreateGrant{
		Granter:   granter,
		Grantee:   grantee,
		ExpiresAt: futureExp,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "payload oneof must be set")
}
