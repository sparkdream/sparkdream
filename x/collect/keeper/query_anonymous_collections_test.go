package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
)

func TestQueryAnonymousCollections_Empty(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	resp, err := f.queryServer.AnonymousCollections(f.ctx, &types.QueryAnonymousCollectionsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.Collections)
}

func TestQueryAnonymousCollections_ReturnsAnonymous(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Create an anonymous collection
	msg := validCreateAnonCollMsg(f.member)
	createResp, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)

	resp, err := f.queryServer.AnonymousCollections(f.ctx, &types.QueryAnonymousCollectionsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 1)
	require.Equal(t, createResp.Id, resp.Collections[0].Id)
}

func TestQueryAnonymousCollections_ExcludesRegular(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Create a regular collection (owned by f.owner, not module account)
	f.createCollection(t, f.owner)

	// Create an anonymous collection
	msg := validCreateAnonCollMsg(f.member)
	_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
	require.NoError(t, err)

	resp, err := f.queryServer.AnonymousCollections(f.ctx, &types.QueryAnonymousCollectionsRequest{})
	require.NoError(t, err)
	// Only the anonymous collection should appear (not the regular one)
	require.Len(t, resp.Collections, 1)
}

func TestQueryAnonymousCollections_Pagination(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Create 3 anonymous collections
	for i := 0; i < 3; i++ {
		msg := validCreateAnonCollMsg(f.member)
		msg.Nullifier = []byte{byte(i + 1)}
		mgmtKey := make([]byte, 32)
		mgmtKey[0] = byte(i + 1)
		msg.ManagementPublicKey = mgmtKey
		_, err := f.msgServer.CreateAnonymousCollection(f.ctx, msg)
		require.NoError(t, err)
	}

	// Query with limit=2
	resp, err := f.queryServer.AnonymousCollections(f.ctx, &types.QueryAnonymousCollectionsRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 2)

	// Query with offset=2
	resp, err = f.queryServer.AnonymousCollections(f.ctx, &types.QueryAnonymousCollectionsRequest{
		Pagination: &query.PageRequest{Offset: 2, Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 1)
}

func TestQueryAnonymousCollections_NilRequest(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)

	_, err := f.queryServer.AnonymousCollections(f.ctx, nil)
	require.Error(t, err)
}

// Suppress unused import warning
var _ = keeper.NewQueryServerImpl
