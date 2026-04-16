package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestCreateSession(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	validTypes := types.DefaultAllowedMsgTypes[:2]
	futureExp := sdkCtx.BlockTime().Add(1 * time.Hour)
	spendLimit := sdk.NewInt64Coin("uspark", 10_000_000)

	tests := []struct {
		name        string
		msg         *types.MsgCreateSession
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "success",
			msg: &types.MsgCreateSession{
				Granter:         granter,
				Grantee:         grantee,
				AllowedMsgTypes: validTypes,
				SpendLimit:      spendLimit,
				Expiration:      futureExp,
				MaxExecCount:    100,
			},
			expectError: false,
		},
		{
			name: "self delegation",
			msg: &types.MsgCreateSession{
				Granter:         granter,
				Grantee:         granter,
				AllowedMsgTypes: validTypes,
				SpendLimit:      spendLimit,
				Expiration:      futureExp,
			},
			expectError: true,
			errContains: "granter == grantee",
		},
		{
			name: "session already exists",
			msg: &types.MsgCreateSession{
				Granter:         granter,
				Grantee:         grantee,
				AllowedMsgTypes: validTypes,
				SpendLimit:      spendLimit,
				Expiration:      futureExp,
			},
			setup: func() {
				// First session was created by the success test above
			},
			expectError: true,
			errContains: "session already exists",
		},
		{
			name: "max sessions exceeded",
			msg: func() *types.MsgCreateSession {
				newGrantee := testAddr("grantee_overflow", f.addressCodec)
				return &types.MsgCreateSession{
					Granter:         granter,
					Grantee:         newGrantee,
					AllowedMsgTypes: validTypes,
					SpendLimit:      spendLimit,
					Expiration:      futureExp,
				}
			}(),
			setup: func() {
				// Set max to 1 so the existing session from "success" test blocks new ones
				params, _ := f.keeper.Params.Get(f.ctx)
				params.MaxSessionsPerGranter = 1
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "too many active sessions",
		},
		{
			name: "msg types exceed per-session limit",
			msg: func() *types.MsgCreateSession {
				return &types.MsgCreateSession{
					Granter:         testAddr("granter2", f.addressCodec),
					Grantee:         testAddr("grantee2", f.addressCodec),
					AllowedMsgTypes: types.DefaultAllowedMsgTypes, // 18 types
					SpendLimit:      spendLimit,
					Expiration:      futureExp,
				}
			}(),
			setup: func() {
				params, _ := f.keeper.Params.Get(f.ctx)
				params.MaxSessionsPerGranter = 10
				params.MaxMsgTypesPerSession = 2 // only allow 2
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "too many message types",
		},
		{
			name: "msg type not in allowlist",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter3", f.addressCodec),
				Grantee:         testAddr("grantee3", f.addressCodec),
				AllowedMsgTypes: []string{"/sparkdream.unknown.v1.MsgFoo"},
				SpendLimit:      spendLimit,
				Expiration:      futureExp,
			},
			setup: func() {
				params, _ := f.keeper.Params.Get(f.ctx)
				params.MaxMsgTypesPerSession = 20
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "not in current Params.allowed_msg_types",
		},
		{
			name: "non-delegable msg type",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter4", f.addressCodec),
				Grantee:         testAddr("grantee4", f.addressCodec),
				AllowedMsgTypes: []string{"/sparkdream.session.v1.MsgExecSession"},
				SpendLimit:      spendLimit,
				Expiration:      futureExp,
			},
			setup: func() {
				// Temporarily add to allowlist to bypass allowlist check;
				// NonDelegableSessionMsgs check is separate
				params, _ := f.keeper.Params.Get(f.ctx)
				params.AllowedMsgTypes = append(params.AllowedMsgTypes, "/sparkdream.session.v1.MsgExecSession")
				params.MaxAllowedMsgTypes = append(params.MaxAllowedMsgTypes, "/sparkdream.session.v1.MsgExecSession")
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "NonDelegableSessionMsgs",
		},
		{
			name: "expiration in the past",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter5", f.addressCodec),
				Grantee:         testAddr("grantee5", f.addressCodec),
				AllowedMsgTypes: validTypes,
				SpendLimit:      spendLimit,
				Expiration:      sdkCtx.BlockTime().Add(-1 * time.Hour),
			},
			setup: func() {
				_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			},
			expectError: true,
			errContains: "expiration is in the past",
		},
		{
			name: "expiration too far in future",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter6", f.addressCodec),
				Grantee:         testAddr("grantee6", f.addressCodec),
				AllowedMsgTypes: validTypes,
				SpendLimit:      spendLimit,
				Expiration:      sdkCtx.BlockTime().Add(30 * 24 * time.Hour), // 30 days > 7 day max
			},
			expectError: true,
			errContains: "exceeds max_expiration",
		},
		{
			name: "spend limit too high",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter7", f.addressCodec),
				Grantee:         testAddr("grantee7", f.addressCodec),
				AllowedMsgTypes: validTypes,
				SpendLimit:      sdk.NewInt64Coin("uspark", 999_000_000_000), // way over 100 SPARK
				Expiration:      futureExp,
			},
			expectError: true,
			errContains: "spend limit exceeds max_spend_limit",
		},
		{
			name: "invalid denom",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter8", f.addressCodec),
				Grantee:         testAddr("grantee8", f.addressCodec),
				AllowedMsgTypes: validTypes,
				SpendLimit:      sdk.NewInt64Coin("udream", 1_000_000),
				Expiration:      futureExp,
			},
			expectError: true,
			errContains: "denom is not uspark",
		},
		{
			name: "zero spend limit is rejected (SESSION-4 fix)",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter9", f.addressCodec),
				Grantee:         testAddr("grantee9", f.addressCodec),
				AllowedMsgTypes: validTypes,
				SpendLimit:      sdk.NewInt64Coin("uspark", 0),
				Expiration:      futureExp,
			},
			expectError: true,
			errContains: "spend_limit must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			_, err := ms.CreateSession(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)

				// Verify session was stored
				session, err := f.keeper.GetSession(f.ctx, tt.msg.Granter, tt.msg.Grantee)
				require.NoError(t, err)
				require.Equal(t, tt.msg.Granter, session.Granter)
				require.Equal(t, tt.msg.Grantee, session.Grantee)
				require.Equal(t, tt.msg.AllowedMsgTypes, session.AllowedMsgTypes)
				require.Equal(t, uint64(0), session.ExecCount)
			}
		})
	}
}

func TestCreateSessionIndexes(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	futureExp := sdkCtx.BlockTime().Add(1 * time.Hour)

	_, err := ms.CreateSession(f.ctx, &types.MsgCreateSession{
		Granter:         granter,
		Grantee:         grantee,
		AllowedMsgTypes: types.DefaultAllowedMsgTypes[:1],
		SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
		Expiration:      futureExp,
	})
	require.NoError(t, err)

	// Verify granter index
	has, err := f.keeper.SessionsByGranter.Has(f.ctx, makeGranterKey(granter, grantee))
	require.NoError(t, err)
	require.True(t, has)

	// Verify grantee index
	has, err = f.keeper.SessionsByGrantee.Has(f.ctx, makeGranteeKey(grantee, granter))
	require.NoError(t, err)
	require.True(t, has)

	// Verify expiration index
	has, err = f.keeper.SessionsByExpiration.Has(f.ctx, makeExpKey(futureExp.Unix(), granter, grantee))
	require.NoError(t, err)
	require.True(t, has)
}
