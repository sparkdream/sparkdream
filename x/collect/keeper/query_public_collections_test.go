package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	query "github.com/cosmos/cosmos-sdk/types/query"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

func TestQueryPublicCollections(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture)
		expLen int
	}{
		{
			name:   "empty",
			setup:  nil,
			expLen: 0,
		},
		{
			name: "returns public active collection",
			setup: func(f *testFixture) {
				// createCollection creates a PUBLIC ACTIVE collection by default (owner is member)
				f.createCollection(t, f.owner)
			},
			expLen: 1,
		},
		{
			name: "excludes pending collections",
			setup: func(f *testFixture) {
				// PENDING (non-member) collections should NOT be returned
				f.createPendingCollection(t)
			},
			expLen: 0,
		},
		{
			name: "multiple public active collections",
			setup: func(f *testFixture) {
				f.createCollection(t, f.owner)
				f.createCollection(t, f.owner)
			},
			expLen: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.queryServer.PublicCollections(f.ctx, &types.QueryPublicCollectionsRequest{})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Collections, tc.expLen)
		})
	}
}

func TestQueryPublicCollections_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.PublicCollections(f.ctx, nil)
	require.Error(t, err)
}

// pin flips the Pinned flag and re-points the status index, mirroring what
// MsgPinCollection does so the index-native pinned-first ordering holds.
func (f *testFixture) pin(t *testing.T, id uint64) {
	t.Helper()
	coll, err := f.keeper.Collection.Get(f.ctx, id)
	require.NoError(t, err)
	require.NoError(t, f.keeper.MoveCollectionStatusIndex(f.ctx, coll.Status, coll.Pinned, coll.Status, true, id))
	coll.Pinned = true
	require.NoError(t, f.keeper.Collection.Set(f.ctx, id, coll))
}

func TestQueryPublicCollections_PinnedFirst(t *testing.T) {
	f := initTestFixture(t)
	// Create three collections; pin the last-created (highest ID) one so it would
	// naturally sort last by ID. It must come back first.
	c1 := f.createCollection(t, f.owner)
	c2 := f.createCollection(t, f.owner)
	c3 := f.createCollection(t, f.owner)
	f.pin(t, c3)

	resp, err := f.queryServer.PublicCollections(f.ctx, &types.QueryPublicCollectionsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 3)
	// Pinned first, then the rest in ID order.
	require.Equal(t, c3, resp.Collections[0].Id)
	require.Equal(t, c1, resp.Collections[1].Id)
	require.Equal(t, c2, resp.Collections[2].Id)
}

// unpin clears the Pinned flag and re-points the status index, mirroring
// MsgUnpinCollection.
func (f *testFixture) unpin(t *testing.T, id uint64) {
	t.Helper()
	coll, err := f.keeper.Collection.Get(f.ctx, id)
	require.NoError(t, err)
	require.NoError(t, f.keeper.MoveCollectionStatusIndex(f.ctx, coll.Status, coll.Pinned, coll.Status, false, id))
	coll.Pinned = false
	require.NoError(t, f.keeper.Collection.Set(f.ctx, id, coll))
}

// TestQueryPublicCollections_UnpinRestoresOrder verifies the index round-trips:
// after unpin, ordering falls back to plain ID order, and the status-index
// consistency invariant holds at each step (it keys on pinned-rank too).
func TestQueryPublicCollections_UnpinRestoresOrder(t *testing.T) {
	f := initTestFixture(t)
	c1 := f.createCollection(t, f.owner)
	c2 := f.createCollection(t, f.owner)

	invariant := keeper.StatusIndexConsistencyInvariant(f.keeper)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	f.pin(t, c2)
	_, broken := invariant(sdkCtx)
	require.False(t, broken, "invariant must hold while pinned")

	resp, err := f.queryServer.PublicCollections(f.ctx, &types.QueryPublicCollectionsRequest{})
	require.NoError(t, err)
	require.Equal(t, c2, resp.Collections[0].Id) // pinned first

	f.unpin(t, c2)
	_, broken = invariant(sdkCtx)
	require.False(t, broken, "invariant must hold after unpin")

	resp, err = f.queryServer.PublicCollections(f.ctx, &types.QueryPublicCollectionsRequest{})
	require.NoError(t, err)
	require.Equal(t, c1, resp.Collections[0].Id) // back to ID order
	require.Equal(t, c2, resp.Collections[1].Id)
}

// TestQueryPublicCollections_PinnedAcrossPages guards the chain gap this change
// fixes: a pinned collection whose ID lands on a later page must surface on the
// first page once ordering is applied across the whole set before slicing.
func TestQueryPublicCollections_PinnedAcrossPages(t *testing.T) {
	f := initTestFixture(t)
	c1 := f.createCollection(t, f.owner)
	_ = f.createCollection(t, f.owner)
	c3 := f.createCollection(t, f.owner)
	f.pin(t, c3) // highest ID, would fall on page 2 without pinned-first ordering

	resp, err := f.queryServer.PublicCollections(f.ctx, &types.QueryPublicCollectionsRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 2)
	require.Equal(t, c3, resp.Collections[0].Id) // pinned floats onto page 1
	require.Equal(t, c1, resp.Collections[1].Id)
	require.Equal(t, uint64(3), resp.Pagination.Total)
}

// TestQueryPublicCollections_HugeLimitNoPanic guards against a slice-bounds
// panic from offset+limit overflow: an attacker-supplied limit near MaxUint64
// must be clamped (and paginate must stay overflow-safe) rather than panicking.
func TestQueryPublicCollections_HugeLimitNoPanic(t *testing.T) {
	f := initTestFixture(t)
	c1 := f.createCollection(t, f.owner)
	c2 := f.createCollection(t, f.owner)

	resp, err := f.queryServer.PublicCollections(f.ctx, &types.QueryPublicCollectionsRequest{
		Pagination: &query.PageRequest{Offset: 1, Limit: math.MaxUint64},
	})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 1) // offset past the first, clamped tail
	require.Equal(t, c2, resp.Collections[0].Id)
	require.Equal(t, uint64(2), resp.Pagination.Total)
	_ = c1
}
