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

	// Helper to create a test session as a SESSION_KEY grant.
	setupSession := func(allowedTypes []string, exp time.Time, maxExec uint64) {
		// Wipe any prior pair so the create-path's one-active-per-pair
		// invariant holds across test runs.
		cleanupSessionPair(t, f, granter, grantee)
		_ = createTestSessionWithExec(t, f, granter, grantee, allowedTypes, exp, maxExec)
	}

	// Cleanup session between tests
	cleanupSession := func() {
		cleanupSessionPair(t, f, granter, grantee)
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
				id, _ := f.keeper.SessionKeyByPair.Get(f.ctx, collections.Join(granter, grantee))
				g, _ := f.keeper.Grants.Get(f.ctx, id)
				sk := g.GetSessionKey()
				sk.ExecCount = 5
				g.Payload = &types.Grant_SessionKey{SessionKey: sk}
				_ = f.keeper.Grants.Set(f.ctx, id, g)
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

	// Create grant with MaxExecCount = 0 (unlimited) and high ExecCount.
	_ = createTestSessionWithExec(t, f, granter, grantee, []string{blogType}, futureExp, 0)
	id, err := f.keeper.SessionKeyByPair.Get(f.ctx, collections.Join(granter, grantee))
	require.NoError(t, err)
	g, err := f.keeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	sk := g.GetSessionKey()
	sk.ExecCount = 99999
	g.Payload = &types.Grant_SessionKey{SessionKey: sk}
	require.NoError(t, f.keeper.Grants.Set(f.ctx, id, g))

	// Should pass exec count check (unlimited) but fail at unpack/router
	_, err = ms.ExecSession(f.ctx, &types.MsgExecSession{
		Granter: granter,
		Grantee: grantee,
		Msgs:    []*gogoany.Any{{TypeUrl: blogType}},
	})
	// Error should NOT be about exec count
	require.Error(t, err)
	require.NotContains(t, err.Error(), "execution cap reached")
}
