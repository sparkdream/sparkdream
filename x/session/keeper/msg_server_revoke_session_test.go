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

	// Verify session exists in all indexes before revoke
	_, err := f.keeper.GetSession(f.ctx, granter, grantee)
	require.NoError(t, err)

	has, err := f.keeper.SessionsByGranter.Has(f.ctx, makeGranterKey(granter, grantee))
	require.NoError(t, err)
	require.True(t, has)

	has, err = f.keeper.SessionsByGrantee.Has(f.ctx, makeGranteeKey(grantee, granter))
	require.NoError(t, err)
	require.True(t, has)

	has, err = f.keeper.SessionsByExpiration.Has(f.ctx, makeExpKey(exp.Unix(), granter, grantee))
	require.NoError(t, err)
	require.True(t, has)

	// Revoke
	_, err = ms.RevokeSession(f.ctx, &types.MsgRevokeSession{
		Granter: granter,
		Grantee: grantee,
	})
	require.NoError(t, err)

	// Verify all indexes cleaned
	_, err = f.keeper.GetSession(f.ctx, granter, grantee)
	require.Error(t, err)

	has, err = f.keeper.SessionsByGranter.Has(f.ctx, makeGranterKey(granter, grantee))
	require.NoError(t, err)
	require.False(t, has)

	has, err = f.keeper.SessionsByGrantee.Has(f.ctx, makeGranteeKey(grantee, granter))
	require.NoError(t, err)
	require.False(t, has)

	has, err = f.keeper.SessionsByExpiration.Has(f.ctx, makeExpKey(exp.Unix(), granter, grantee))
	require.NoError(t, err)
	require.False(t, has)
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

// helper to create collections key pairs
func makeGranterKey(granter, grantee string) collections.Pair[string, string] {
	return collections.Join(granter, grantee)
}

func makeGranteeKey(grantee, granter string) collections.Pair[string, string] {
	return collections.Join(grantee, granter)
}

func makeExpKey(expUnix int64, granter, grantee string) collections.Triple[int64, string, string] {
	return collections.Join3(expUnix, granter, grantee)
}
