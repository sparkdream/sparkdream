package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
)

func TestQueryCollectionsByContent_Empty(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	resp, err := f.queryServer.CollectionsByContent(f.ctx, &types.QueryCollectionsByContentRequest{
		Module:     "blog",
		EntityType: "post",
		EntityId:   "42",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.Collections)
}

func TestQueryCollectionsByContent_FindsCollection(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create a collection with an item that has an on-chain reference
	collID := f.createCollection(t, f.owner)

	// Add an item with an on-chain reference manually
	// (since AddItem sets the index only when OnChain ref is provided)
	itemID := f.addItem(t, collID, f.owner)

	// Manually set the on-chain ref index
	refKey := "blog:post:42"
	f.keeper.ItemsByOnChainRef.Set(f.ctx, collections.Join(refKey, itemID)) //nolint:errcheck

	// Also update the item to have on-chain ref so the query can verify
	item, _ := f.keeper.Item.Get(f.ctx, itemID)
	item.ReferenceType = types.ReferenceType_REFERENCE_TYPE_ON_CHAIN
	item.OnChain = &types.OnChainReference{
		Module:     "blog",
		EntityType: "post",
		EntityId:   "42",
	}
	f.keeper.Item.Set(f.ctx, itemID, item) //nolint:errcheck

	resp, err := f.queryServer.CollectionsByContent(f.ctx, &types.QueryCollectionsByContentRequest{
		Module:     "blog",
		EntityType: "post",
		EntityId:   "42",
	})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 1)
	require.Equal(t, collID, resp.Collections[0].Id)
}

func TestQueryCollectionsByContent_Deduplicates(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create a collection with two items referencing the same content
	collID := f.createCollection(t, f.owner)
	itemID1 := f.addItem(t, collID, f.owner)
	itemID2 := f.addItem(t, collID, f.owner)

	refKey := "blog:post:99"
	f.keeper.ItemsByOnChainRef.Set(f.ctx, collections.Join(refKey, itemID1)) //nolint:errcheck
	f.keeper.ItemsByOnChainRef.Set(f.ctx, collections.Join(refKey, itemID2)) //nolint:errcheck

	resp, err := f.queryServer.CollectionsByContent(f.ctx, &types.QueryCollectionsByContentRequest{
		Module:     "blog",
		EntityType: "post",
		EntityId:   "99",
	})
	require.NoError(t, err)
	// Should return only 1 collection (deduplicated)
	require.Len(t, resp.Collections, 1)
	require.Equal(t, collID, resp.Collections[0].Id)
}

func TestQueryCollectionsByContent_MultipleCollections(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create two collections, each with an item referencing the same content
	collID1 := f.createCollection(t, f.owner)
	collID2 := f.createCollection(t, f.owner)

	itemID1 := f.addItem(t, collID1, f.owner)
	itemID2 := f.addItem(t, collID2, f.owner)

	refKey := "forum:post:7"
	f.keeper.ItemsByOnChainRef.Set(f.ctx, collections.Join(refKey, itemID1)) //nolint:errcheck
	f.keeper.ItemsByOnChainRef.Set(f.ctx, collections.Join(refKey, itemID2)) //nolint:errcheck

	resp, err := f.queryServer.CollectionsByContent(f.ctx, &types.QueryCollectionsByContentRequest{
		Module:     "forum",
		EntityType: "post",
		EntityId:   "7",
	})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 2)

	// Collect IDs
	ids := make(map[uint64]bool)
	for _, c := range resp.Collections {
		ids[c.Id] = true
	}
	require.True(t, ids[collID1])
	require.True(t, ids[collID2])
}

func TestQueryCollectionsByContent_Pagination(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create 3 collections with items referencing the same content
	var collIDs []uint64
	refKey := "blog:post:1"
	for i := 0; i < 3; i++ {
		collID := f.createCollection(t, f.owner)
		itemID := f.addItem(t, collID, f.owner)
		f.keeper.ItemsByOnChainRef.Set(f.ctx, collections.Join(refKey, itemID)) //nolint:errcheck
		collIDs = append(collIDs, collID)
	}

	// Query with limit=2
	resp, err := f.queryServer.CollectionsByContent(f.ctx, &types.QueryCollectionsByContentRequest{
		Module:     "blog",
		EntityType: "post",
		EntityId:   "1",
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 2)
}

func TestQueryCollectionsByContent_MissingFields(t *testing.T) {
	f := initTestFixture(t)

	tests := []struct {
		name string
		req  *types.QueryCollectionsByContentRequest
	}{
		{"nil request", nil},
		{"empty module", &types.QueryCollectionsByContentRequest{Module: "", EntityType: "post", EntityId: "1"}},
		{"empty entity_type", &types.QueryCollectionsByContentRequest{Module: "blog", EntityType: "", EntityId: "1"}},
		{"empty entity_id", &types.QueryCollectionsByContentRequest{Module: "blog", EntityType: "post", EntityId: ""}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.queryServer.CollectionsByContent(f.ctx, tc.req)
			require.Error(t, err)
		})
	}
}

func TestQueryCollectionsByContent_DeletedItem(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	itemID := f.addItem(t, collID, f.owner)

	refKey := "blog:post:50"
	f.keeper.ItemsByOnChainRef.Set(f.ctx, collections.Join(refKey, itemID)) //nolint:errcheck

	// Delete the item but leave the index (simulates stale index)
	f.keeper.Item.Remove(f.ctx, itemID) //nolint:errcheck

	resp, err := f.queryServer.CollectionsByContent(f.ctx, &types.QueryCollectionsByContentRequest{
		Module:     "blog",
		EntityType: "post",
		EntityId:   "50",
	})
	require.NoError(t, err)
	// Should return empty since the item was deleted
	require.Empty(t, resp.Collections)
}
