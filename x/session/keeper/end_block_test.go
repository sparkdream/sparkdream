package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/session/types"
)

func TestEndBlockerNoExpiredSessions(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// Session expiring in the future
	futureExp := sdkCtx.BlockTime().Add(24 * time.Hour)
	createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], futureExp)

	// Run EndBlocker
	err := f.keeper.EndBlocker(sdkCtx)
	require.NoError(t, err)

	// Session should still exist
	_, err = f.keeper.GetSession(f.ctx, granter, grantee)
	require.NoError(t, err)
}

func TestEndBlockerPrunesExpiredSession(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	// Session already expired
	pastExp := sdkCtx.BlockTime().Add(-1 * time.Hour)
	createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], pastExp)

	// Run EndBlocker
	err := f.keeper.EndBlocker(sdkCtx)
	require.NoError(t, err)

	// Session should be deleted
	_, err = f.keeper.GetSession(f.ctx, granter, grantee)
	require.Error(t, err)

	// All indexes should be cleaned
	has, err := f.keeper.SessionsByGranter.Has(f.ctx, makeGranterKey(granter, grantee))
	require.NoError(t, err)
	require.False(t, has)

	has, err = f.keeper.SessionsByGrantee.Has(f.ctx, makeGranteeKey(grantee, granter))
	require.NoError(t, err)
	require.False(t, has)

	has, err = f.keeper.SessionsByExpiration.Has(f.ctx, makeExpKey(pastExp.Unix(), granter, grantee))
	require.NoError(t, err)
	require.False(t, has)
}

func TestEndBlockerPrunesMultipleExpiredSessions(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)

	// Create 5 expired sessions with different grantees
	for i := 0; i < 5; i++ {
		grantee := testAddr("grantee"+string(rune('a'+i)), f.addressCodec)
		pastExp := sdkCtx.BlockTime().Add(-time.Duration(i+1) * time.Hour)
		createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], pastExp)
	}

	// Run EndBlocker
	err := f.keeper.EndBlocker(sdkCtx)
	require.NoError(t, err)

	// All expired sessions should be deleted
	for i := 0; i < 5; i++ {
		grantee := testAddr("grantee"+string(rune('a'+i)), f.addressCodec)
		_, err := f.keeper.GetSession(f.ctx, granter, grantee)
		require.Error(t, err, "session %d should be deleted", i)
	}
}

func TestEndBlockerMixedExpiredAndActive(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	expiredGrantee := testAddr("expired_grantee", f.addressCodec)
	activeGrantee := testAddr("active_grantee", f.addressCodec)

	// One expired, one active
	pastExp := sdkCtx.BlockTime().Add(-1 * time.Hour)
	futureExp := sdkCtx.BlockTime().Add(24 * time.Hour)

	createTestSession(t, f, granter, expiredGrantee, types.DefaultAllowedMsgTypes[:1], pastExp)
	createTestSession(t, f, granter, activeGrantee, types.DefaultAllowedMsgTypes[:1], futureExp)

	// Run EndBlocker
	err := f.keeper.EndBlocker(sdkCtx)
	require.NoError(t, err)

	// Expired session should be deleted
	_, err = f.keeper.GetSession(f.ctx, granter, expiredGrantee)
	require.Error(t, err)

	// Active session should still exist
	_, err = f.keeper.GetSession(f.ctx, granter, activeGrantee)
	require.NoError(t, err)
}

func TestEndBlockerEmitsEvents(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	granter := testAddr("granter", f.addressCodec)
	grantee := testAddr("grantee", f.addressCodec)

	pastExp := sdkCtx.BlockTime().Add(-1 * time.Hour)
	createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], pastExp)

	err := f.keeper.EndBlocker(sdkCtx)
	require.NoError(t, err)

	events := sdkCtx.EventManager().Events()
	found := false
	for _, e := range events {
		if e.Type == "session_expired" {
			found = true
			break
		}
	}
	require.True(t, found, "expected session_expired event")
}

func TestEndBlockerMaxPrunePerBlock(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Create 105 expired sessions (exceeds maxPrunePerBlock=100)
	for i := 0; i < 105; i++ {
		granter := testAddr("granter"+string(rune(i/26+'a'))+string(rune(i%26+'a')), f.addressCodec)
		grantee := testAddr("grantee"+string(rune(i/26+'a'))+string(rune(i%26+'a')), f.addressCodec)
		pastExp := sdkCtx.BlockTime().Add(-time.Duration(i+1) * time.Second)
		createTestSession(t, f, granter, grantee, types.DefaultAllowedMsgTypes[:1], pastExp)
	}

	// Run EndBlocker - should prune at most 100
	err := f.keeper.EndBlocker(sdkCtx)
	require.NoError(t, err)

	// Count remaining sessions
	remaining := 0
	err = f.keeper.Sessions.Walk(f.ctx, nil, func(_ collections.Pair[string, string], _ types.Session) (bool, error) {
		remaining++
		return false, nil
	})
	require.NoError(t, err)

	// Should have exactly 5 remaining (105 - 100 = 5)
	require.Equal(t, 5, remaining, "should have 5 sessions remaining after pruning 100")
}
