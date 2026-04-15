package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogoany "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestExecSessionValidation(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	blogType := types.DefaultAllowedMsgTypes[0] // "/sparkdream.blog.v1.MsgCreatePost"
	futureExp := sdkCtx.BlockTime().Add(24 * time.Hour)

	// Helper to create a test session with specific params
	setupSession := func(allowedTypes []string, exp time.Time, maxExec uint64) {
		session := types.Session{
			Granter:         granter,
			Grantee:         grantee,
			AllowedMsgTypes: allowedTypes,
			SpendLimit:      sdk.NewInt64Coin("uspark", 10_000_000),
			Spent:           sdk.NewInt64Coin("uspark", 0),
			Expiration:      exp,
			CreatedAt:       sdkCtx.BlockTime(),
			LastUsedAt:      sdkCtx.BlockTime(),
			ExecCount:       0,
			MaxExecCount:    maxExec,
		}
		key := collections.Join(granter, grantee)
		require.NoError(t, f.keeper.Sessions.Set(f.ctx, key, session))
		require.NoError(t, f.keeper.SessionsByGranter.Set(f.ctx, collections.Join(granter, grantee)))
		require.NoError(t, f.keeper.SessionsByGrantee.Set(f.ctx, collections.Join(grantee, granter)))
		require.NoError(t, f.keeper.SessionsByExpiration.Set(f.ctx, collections.Join3(exp.Unix(), granter, grantee)))
	}

	// Cleanup session between tests
	cleanupSession := func() {
		key := collections.Join(granter, grantee)
		_ = f.keeper.Sessions.Remove(f.ctx, key)
		_ = f.keeper.SessionsByGranter.Remove(f.ctx, collections.Join(granter, grantee))
		_ = f.keeper.SessionsByGrantee.Remove(f.ctx, collections.Join(grantee, granter))
	}

	tests := []struct {
		name        string
		msg         *types.MsgExecSession
		setup       func()
		cleanup     func()
		expectError bool
		errContains string
	}{
		{
			name: "empty msgs",
			msg: &types.MsgExecSession{
				Granter: granter,
				Grantee: grantee,
				Msgs:    nil,
			},
			expectError: true,
			errContains: "at least one inner message",
		},
		{
			name: "too many msgs",
			msg: func() *types.MsgExecSession {
				msgs := make([]*gogoany.Any, 11)
				for i := range msgs {
					msgs[i] = &gogoany.Any{TypeUrl: blogType}
				}
				return &types.MsgExecSession{
					Granter: granter,
					Grantee: grantee,
					Msgs:    msgs,
				}
			}(),
			expectError: true,
			errContains: "too many inner messages",
		},
		{
			name: "session not found",
			msg: &types.MsgExecSession{
				Granter: granter,
				Grantee: grantee,
				Msgs:    []*gogoany.Any{{TypeUrl: blogType}},
			},
			expectError: true,
			errContains: "no active session",
		},
		{
			name: "session expired",
			msg: &types.MsgExecSession{
				Granter: granter,
				Grantee: grantee,
				Msgs:    []*gogoany.Any{{TypeUrl: blogType}},
			},
			setup: func() {
				// Session with expiration in the past
				setupSession([]string{blogType}, sdkCtx.BlockTime().Add(-1*time.Hour), 0)
			},
			cleanup:     func() { cleanupSession() },
			expectError: true,
			errContains: "passed its expiration time",
		},
		{
			name: "exec count exceeded",
			msg: &types.MsgExecSession{
				Granter: granter,
				Grantee: grantee,
				Msgs:    []*gogoany.Any{{TypeUrl: blogType}},
			},
			setup: func() {
				setupSession([]string{blogType}, futureExp, 5)
				// Set exec count to max
				key := collections.Join(granter, grantee)
				session, _ := f.keeper.Sessions.Get(f.ctx, key)
				session.ExecCount = 5
				_ = f.keeper.Sessions.Set(f.ctx, key, session)
			},
			cleanup:     func() { cleanupSession() },
			expectError: true,
			errContains: "execution cap reached",
		},
		{
			name: "nested exec - non-delegable",
			msg: &types.MsgExecSession{
				Granter: granter,
				Grantee: grantee,
				Msgs:    []*gogoany.Any{{TypeUrl: "/sparkdream.session.v1.MsgExecSession"}},
			},
			setup: func() {
				setupSession([]string{blogType}, futureExp, 0)
			},
			cleanup:     func() { cleanupSession() },
			expectError: true,
			errContains: "MsgExecSession cannot contain MsgExecSession",
		},
		{
			name: "msg type not in session allowlist",
			msg: &types.MsgExecSession{
				Granter: granter,
				Grantee: grantee,
				Msgs:    []*gogoany.Any{{TypeUrl: "/sparkdream.forum.v1.MsgCreatePost"}},
			},
			setup: func() {
				// Session only allows blog types, not forum
				setupSession([]string{blogType}, futureExp, 0)
			},
			cleanup:     func() { cleanupSession() },
			expectError: true,
			errContains: "not in session's allowed list",
		},
		{
			name: "msg type not in global allowlist",
			msg: &types.MsgExecSession{
				Granter: granter,
				Grantee: grantee,
				Msgs:    []*gogoany.Any{{TypeUrl: blogType}},
			},
			setup: func() {
				// Session allows blog type but global params does not
				setupSession([]string{blogType}, futureExp, 0)
				params, _ := f.keeper.Params.Get(f.ctx)
				params.AllowedMsgTypes = []string{"/sparkdream.forum.v1.MsgCreatePost"}
				params.MaxAllowedMsgTypes = []string{"/sparkdream.forum.v1.MsgCreatePost", blogType}
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			cleanup: func() {
				cleanupSession()
				_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			},
			expectError: true,
			errContains: "not in current Params.allowed_msg_types",
		},
		{
			name: "other non-delegable session msgs",
			msg: &types.MsgExecSession{
				Granter: granter,
				Grantee: grantee,
				Msgs:    []*gogoany.Any{{TypeUrl: "/sparkdream.session.v1.MsgCreateSession"}},
			},
			setup: func() {
				setupSession([]string{blogType}, futureExp, 0)
			},
			cleanup:     func() { cleanupSession() },
			expectError: true,
			errContains: "MsgExecSession cannot contain MsgExecSession",
		},
		{
			name: "router not set - valid msg passes validation but fails at dispatch",
			msg: &types.MsgExecSession{
				Granter: granter,
				Grantee: grantee,
				Msgs:    []*gogoany.Any{{TypeUrl: blogType}},
			},
			setup: func() {
				setupSession([]string{blogType}, futureExp, 0)
			},
			cleanup:     func() { cleanupSession() },
			expectError: true,
			// Will fail at UnpackAny or router — both are after validation
			errContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			if tt.cleanup != nil {
				defer tt.cleanup()
			}

			_, err := ms.ExecSession(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestExecSessionExecCountUnlimited(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	blogType := types.DefaultAllowedMsgTypes[0]
	futureExp := sdkCtx.BlockTime().Add(24 * time.Hour)

	// Create session with MaxExecCount = 0 (unlimited) and high ExecCount
	session := types.Session{
		Granter:         granter,
		Grantee:         grantee,
		AllowedMsgTypes: []string{blogType},
		SpendLimit:      sdk.NewInt64Coin("uspark", 10_000_000),
		Spent:           sdk.NewInt64Coin("uspark", 0),
		Expiration:      futureExp,
		CreatedAt:       sdkCtx.BlockTime(),
		LastUsedAt:      sdkCtx.BlockTime(),
		ExecCount:       99999,
		MaxExecCount:    0, // unlimited
	}
	key := collections.Join(granter, grantee)
	require.NoError(t, f.keeper.Sessions.Set(f.ctx, key, session))
	require.NoError(t, f.keeper.SessionsByGranter.Set(f.ctx, collections.Join(granter, grantee)))
	require.NoError(t, f.keeper.SessionsByGrantee.Set(f.ctx, collections.Join(grantee, granter)))
	require.NoError(t, f.keeper.SessionsByExpiration.Set(f.ctx, collections.Join3(futureExp.Unix(), granter, grantee)))

	// Should pass exec count check (unlimited) but fail at unpack/router
	_, err := ms.ExecSession(f.ctx, &types.MsgExecSession{
		Granter: granter,
		Grantee: grantee,
		Msgs:    []*gogoany.Any{{TypeUrl: blogType}},
	})
	// Error should NOT be about exec count
	require.Error(t, err)
	require.NotContains(t, err.Error(), "execution cap reached")
}
