package keeper_test

import (
	"testing"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestQueryIsNullifierUsed_Used(t *testing.T) {
	f := initFixture(t)

	// Record a nullifier as used
	f.keeper.SetNullifierUsed(f.ctx, 3, 100, "abc123", types.AnonNullifierEntry{
		UsedAt: 12345,
		Domain: 3,
		Scope:  100,
	})

	queryServer := keeper.NewQueryServerImpl(f.keeper)
	resp, err := queryServer.IsNullifierUsed(f.ctx, &types.QueryIsNullifierUsedRequest{
		NullifierHex: "abc123",
		Domain:        3,
		Scope:         100,
	})
	require.NoError(t, err)
	require.True(t, resp.Used)
}

func TestQueryIsNullifierUsed_NotUsed(t *testing.T) {
	f := initFixture(t)

	queryServer := keeper.NewQueryServerImpl(f.keeper)
	resp, err := queryServer.IsNullifierUsed(f.ctx, &types.QueryIsNullifierUsedRequest{
		NullifierHex: "notused",
		Domain:        3,
		Scope:         100,
	})
	require.NoError(t, err)
	require.False(t, resp.Used)
}

func TestQueryIsNullifierUsed_NilRequest(t *testing.T) {
	f := initFixture(t)

	queryServer := keeper.NewQueryServerImpl(f.keeper)
	_, err := queryServer.IsNullifierUsed(f.ctx, nil)
	require.Error(t, err)
}

func TestQueryIsNullifierUsed_DifferentDomainScope(t *testing.T) {
	f := initFixture(t)

	// Record nullifier in domain=3, scope=100
	f.keeper.SetNullifierUsed(f.ctx, 3, 100, "abc123", types.AnonNullifierEntry{
		UsedAt: 12345,
		Domain: 3,
		Scope:  100,
	})

	queryServer := keeper.NewQueryServerImpl(f.keeper)

	// Same nullifier but different domain should not be found
	resp, err := queryServer.IsNullifierUsed(f.ctx, &types.QueryIsNullifierUsedRequest{
		NullifierHex: "abc123",
		Domain:        4,
		Scope:         100,
	})
	require.NoError(t, err)
	require.False(t, resp.Used)

	// Same nullifier but different scope should not be found
	resp, err = queryServer.IsNullifierUsed(f.ctx, &types.QueryIsNullifierUsedRequest{
		NullifierHex: "abc123",
		Domain:        3,
		Scope:         200,
	})
	require.NoError(t, err)
	require.False(t, resp.Used)
}
