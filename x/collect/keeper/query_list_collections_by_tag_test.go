package keeper_test

import (
	"testing"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestListCollectionsByTagNilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.ListCollectionsByTag(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}

// Pagination honors Offset and Limit against the CollectionsByTag index.
func TestListCollectionsByTagPagination(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.knownTags = map[string]bool{"shared": true}

	ids := make([]uint64, 0, 3)
	for i := 0; i < 3; i++ {
		resp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
			Creator:    f.owner,
			Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
			Visibility: types.Visibility_VISIBILITY_PUBLIC,
			Name:       "c",
			Tags:       []string{"shared"},
		})
		require.NoError(t, err)
		ids = append(ids, resp.Id)
	}

	// Limit=2 returns first 2.
	page1, err := f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{
		Tag:        "shared",
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, page1.Collections, 2)

	// Offset=2 returns the remaining one.
	page2, err := f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{
		Tag:        "shared",
		Pagination: &query.PageRequest{Limit: 2, Offset: 2},
	})
	require.NoError(t, err)
	require.Len(t, page2.Collections, 1)
	require.Equal(t, ids[2], page2.Collections[0].Id)
}

// Nil pagination uses a default limit and returns all matching collections.
func TestListCollectionsByTagNilPagination(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.knownTags = map[string]bool{"solo": true}

	for i := 0; i < 2; i++ {
		_, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
			Creator:    f.owner,
			Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
			Visibility: types.Visibility_VISIBILITY_PUBLIC,
			Name:       "c",
			Tags:       []string{"solo"},
		})
		require.NoError(t, err)
	}

	resp, err := f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: "solo"})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 2)
	require.NotNil(t, resp.Pagination)
}

// Tags sharing a prefix ("art" vs "arts") must not bleed into each other.
func TestListCollectionsByTagPrefixIsolation(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.knownTags = map[string]bool{"art": true, "arts": true}

	artResp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "art-coll",
		Tags:       []string{"art"},
	})
	require.NoError(t, err)

	artsResp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "arts-coll",
		Tags:       []string{"arts"},
	})
	require.NoError(t, err)

	resp, err := f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: "art"})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 1)
	require.Equal(t, artResp.Id, resp.Collections[0].Id)

	resp, err = f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: "arts"})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 1)
	require.Equal(t, artsResp.Id, resp.Collections[0].Id)
}
