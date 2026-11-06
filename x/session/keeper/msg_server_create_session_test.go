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

func TestCreateSession(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	validTypes := types.DefaultAllowedMsgTypes[:2]
	futureExp := sdkCtx.BlockTime().Add(1 * time.Hour)
	spendLimit := sdk.NewInt64Coin("uspark", 10_000_000)

	// The grantee allows the granter: the sessions below are ACTIVE at
	// creation and count toward max_sessions_per_granter.
	allowGranter(t, f, grantee, granter)

	tests := []struct {
		name        string
		msg         *types.MsgCreateSession
		setup       func()
		expectError bool
		errContains string
	}{
		{
			// SES-203: an empty allowlist could execute nothing, yet the fee
			// ante would charge the granter for every failing exec. Runs
			// before "success" so the (granter, grantee) pair is still free.
			name: "no message types",
			msg: &types.MsgCreateSession{
				Granter:         granter,
				Grantee:         grantee,
				AllowedMsgTypes: nil,
				SpendLimit:      spendLimit,
				Expiration:      futureExp,
			},
			expectError: true,
			errContains: "at least one message type",
		},
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
				require.NoError(t, f.keeper.Params.Set(f.ctx, params))
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
				require.NoError(t, f.keeper.Params.Set(f.ctx, params))
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
				require.NoError(t, f.keeper.Params.Set(f.ctx, params))
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
				require.NoError(t, f.keeper.Params.Set(f.ctx, params))
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
				require.NoError(t, f.keeper.Params.Set(f.ctx, types.DefaultParams()))
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
				Expiration:      sdkCtx.BlockTime().Add(31 * 24 * time.Hour), // 31 days > 30-day narrow max
			},
			expectError: true,
			errContains: "exceeds max_expiration",
		},
		{
			name: "no message types",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter6c", f.addressCodec),
				Grantee:         testAddr("grantee6c", f.addressCodec),
				AllowedMsgTypes: nil,
				SpendLimit:      spendLimit,
				Expiration:      futureExp,
			},
			expectError: true,
			errContains: "at least one message type",
		},
		{
			name: "broad session past 7 days",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter6b", f.addressCodec),
				Grantee:         testAddr("grantee6b", f.addressCodec),
				AllowedMsgTypes: types.DefaultAllowedMsgTypes[:3],
				SpendLimit:      spendLimit,
				Expiration:      sdkCtx.BlockTime().Add(8 * 24 * time.Hour), // 3 types: 7-day max
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
			errContains: "spend limit exceeds max_spend_limit_amount",
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
			errContains: "denom does not match the chain's bond denom",
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
		{
			name: "zero max_exec_count is rejected",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter10", f.addressCodec),
				Grantee:         testAddr("grantee10", f.addressCodec),
				AllowedMsgTypes: validTypes,
				SpendLimit:      spendLimit,
				Expiration:      futureExp,
				MaxExecCount:    0,
			},
			expectError: true,
			errContains: "max_exec_count must be positive",
		},
		{
			name: "max_exec_count over params cap is rejected",
			msg: &types.MsgCreateSession{
				Granter:         testAddr("granter11", f.addressCodec),
				Grantee:         testAddr("grantee11", f.addressCodec),
				AllowedMsgTypes: validTypes,
				SpendLimit:      spendLimit,
				Expiration:      futureExp,
				MaxExecCount:    999_999_999, // way above DefaultMaxExecCount (10_000)
			},
			expectError: true,
			errContains: "exceeds params.max_exec_count",
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
	allowGranter(t, f, grantee, granter)

	_, err := ms.CreateSession(f.ctx, &types.MsgCreateSession{
		Granter:         granter,
		Grantee:         grantee,
		AllowedMsgTypes: types.DefaultAllowedMsgTypes[:1],
		SpendLimit:      sdk.NewInt64Coin("uspark", 1_000_000),
		Expiration:      futureExp,
		MaxExecCount:    100,
	})
	require.NoError(t, err)

	// Verify SessionKey lookup is populated.
	id, err := f.keeper.SessionKeyByPair.Get(f.ctx, collections.Join(granter, grantee))
	require.NoError(t, err)

	// Verify all secondary indexes are populated.
	hasGranter, err := f.keeper.GrantsByGranter.Has(f.ctx, collections.Join(granter, id))
	require.NoError(t, err)
	require.True(t, hasGranter)

	hasGrantee, err := f.keeper.GrantsByGrantee.Has(f.ctx, collections.Join(grantee, id))
	require.NoError(t, err)
	require.True(t, hasGrantee)

	hasExp, err := f.keeper.GrantsByExpiration.Has(f.ctx, collections.Join(futureExp.Unix(), id))
	require.NoError(t, err)
	require.True(t, hasExp)
}
