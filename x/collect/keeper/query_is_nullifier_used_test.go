package keeper_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryIsCollectNullifierUsed_NotUsed(t *testing.T) {
	f := initTestFixture(t)

	resp, err := f.queryServer.IsCollectNullifierUsed(f.ctx, &types.QueryIsCollectNullifierUsedRequest{
		NullifierHex: "abcdef1234567890",
		Domain:       6,
		Scope:        100,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.False(t, resp.Used)
}

func TestQueryIsCollectNullifierUsed_Used(t *testing.T) {
	f := initTestFixture(t)

	// Mark a nullifier as used
	nullifierHex := hex.EncodeToString([]byte("test_null"))
	f.keeper.SetNullifierUsed(f.ctx, 6, 100, nullifierHex, types.AnonNullifierEntry{
		UsedAt: 50,
		Domain: 6,
		Scope:  100,
	})

	resp, err := f.queryServer.IsCollectNullifierUsed(f.ctx, &types.QueryIsCollectNullifierUsedRequest{
		NullifierHex: nullifierHex,
		Domain:       6,
		Scope:        100,
	})
	require.NoError(t, err)
	require.True(t, resp.Used)
}

func TestQueryIsCollectNullifierUsed_DifferentDomain(t *testing.T) {
	f := initTestFixture(t)

	nullifierHex := hex.EncodeToString([]byte("domain_test"))
	f.keeper.SetNullifierUsed(f.ctx, 6, 100, nullifierHex, types.AnonNullifierEntry{
		UsedAt: 50,
		Domain: 6,
		Scope:  100,
	})

	// Same nullifier, different domain
	resp, err := f.queryServer.IsCollectNullifierUsed(f.ctx, &types.QueryIsCollectNullifierUsedRequest{
		NullifierHex: nullifierHex,
		Domain:       7, // different
		Scope:        100,
	})
	require.NoError(t, err)
	require.False(t, resp.Used)
}

func TestQueryIsCollectNullifierUsed_DifferentScope(t *testing.T) {
	f := initTestFixture(t)

	nullifierHex := hex.EncodeToString([]byte("scope_test"))
	f.keeper.SetNullifierUsed(f.ctx, 6, 100, nullifierHex, types.AnonNullifierEntry{
		UsedAt: 50,
		Domain: 6,
		Scope:  100,
	})

	// Same nullifier, different scope
	resp, err := f.queryServer.IsCollectNullifierUsed(f.ctx, &types.QueryIsCollectNullifierUsedRequest{
		NullifierHex: nullifierHex,
		Domain:       6,
		Scope:        200, // different
	})
	require.NoError(t, err)
	require.False(t, resp.Used)
}

func TestQueryIsCollectNullifierUsed_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.IsCollectNullifierUsed(f.ctx, nil)
	require.Error(t, err)
}

func TestQueryIsCollectNullifierUsed_EmptyNullifier(t *testing.T) {
	f := initTestFixture(t)

	// Empty nullifier hex should be valid (just returns false since it's not in store)
	resp, err := f.queryServer.IsCollectNullifierUsed(f.ctx, &types.QueryIsCollectNullifierUsedRequest{
		NullifierHex: "",
		Domain:       6,
		Scope:        100,
	})
	require.NoError(t, err)
	require.False(t, resp.Used)
}

func TestQueryIsCollectNullifierUsed_ZeroDomainAndScope(t *testing.T) {
	f := initTestFixture(t)

	nullifierHex := "zero_domain_scope"
	f.keeper.SetNullifierUsed(f.ctx, 0, 0, nullifierHex, types.AnonNullifierEntry{
		UsedAt: 1,
		Domain: 0,
		Scope:  0,
	})

	resp, err := f.queryServer.IsCollectNullifierUsed(f.ctx, &types.QueryIsCollectNullifierUsedRequest{
		NullifierHex: nullifierHex,
		Domain:       0,
		Scope:        0,
	})
	require.NoError(t, err)
	require.True(t, resp.Used)
}
