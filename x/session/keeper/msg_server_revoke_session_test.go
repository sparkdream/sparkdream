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

func TestRevokeSession(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	exp := time.Now().Add(24 * time.Hour).UTC()

	tests := []struct {
		name        string
		msg         *types.MsgRevokeSession
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "session not found",
			msg: &types.MsgRevokeSession{
				Granter: granter,
				Grantee: grantee,
			},
			expectError: true,
			errContains: "no active session",
		},
		{
			name: "success",
			msg: &types.MsgRevokeSession{
				Granter: granter,
				Grantee: grantee,
			},
			setup: func() {
				createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], exp)
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			_, err := ms.RevokeSession(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRevokeSessionCleansIndexes(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	exp := time.Now().Add(24 * time.Hour).UTC()

	createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], exp)

	// Verify session exists before revoke
	_, err := f.keeper.GetSession(f.ctx, granter, grantee)
	require.NoError(t, err)
	require.True(t, hasSessionGrantPair(t, f, granter, grantee))

	id, err := f.keeper.SessionKeyByPair.Get(f.ctx, collections.Join(granter, grantee))
	require.NoError(t, err)

	hasGranter, err := f.keeper.GrantsByGranter.Has(f.ctx, collections.Join(granter, id))
	require.NoError(t, err)
	require.True(t, hasGranter)

	hasGrantee, err := f.keeper.GrantsByGrantee.Has(f.ctx, collections.Join(grantee, id))
	require.NoError(t, err)
	require.True(t, hasGrantee)

	hasExp, err := f.keeper.GrantsByExpiration.Has(f.ctx, collections.Join(exp.Unix(), id))
	require.NoError(t, err)
	require.True(t, hasExp)

	// Revoke
	_, err = ms.RevokeSession(f.ctx, &types.MsgRevokeSession{
		Granter: granter,
		Grantee: grantee,
	})
	require.NoError(t, err)

	// Verify all indexes cleaned
	_, err = f.keeper.GetSession(f.ctx, granter, grantee)
	require.Error(t, err)
	require.False(t, hasSessionGrantPair(t, f, granter, grantee))

	hasGranter, err = f.keeper.GrantsByGranter.Has(f.ctx, collections.Join(granter, id))
	require.NoError(t, err)
	require.False(t, hasGranter)

	hasGrantee, err = f.keeper.GrantsByGrantee.Has(f.ctx, collections.Join(grantee, id))
	require.NoError(t, err)
	require.False(t, hasGrantee)

	hasExp, err = f.keeper.GrantsByExpiration.Has(f.ctx, collections.Join(exp.Unix(), id))
	require.NoError(t, err)
	require.False(t, hasExp)
}

func TestRevokeSessionEmitsEvent(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)
	exp := time.Now().Add(24 * time.Hour).UTC()

	createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], exp)

	_, err := ms.RevokeSession(f.ctx, &types.MsgRevokeSession{
		Granter: granter,
		Grantee: grantee,
	})
	require.NoError(t, err)

	events := sdkCtx.EventManager().Events()
	found := false
	for _, e := range events {
		if e.Type == "session_revoked" {
			found = true
			break
		}
	}
	require.True(t, found, "expected session_revoked event")
}

// hasSessionGrantPair reports whether there's a SESSION_KEY grant
// registered under the (granter, grantee) lookup. Stand-in for the legacy
// `SessionsByGranter.Has(...) && SessionsByGrantee.Has(...) &&
// SessionsByExpiration.Has(...)` triple — if the lookup is present, the
// grant is in the primary store and (because writeGrant fans out
// atomically) every secondary index too.
func hasSessionGrantPair(t *testing.T, f *fixture, granter, grantee string) bool {
	t.Helper()
	has, err := f.keeper.SessionKeyByPair.Has(f.ctx, collections.Join(granter, grantee))
	require.NoError(t, err)
	return has
}
