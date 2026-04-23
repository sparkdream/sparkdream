package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestCreateCollectionTagValidation(t *testing.T) {
	tests := []struct {
		name        string
		knownTags   map[string]bool
		reserved    map[string]bool
		tags        []string
		expectErr   bool
		errContains string
	}{
		{
			name:      "valid tags accepted and incremented",
			knownTags: map[string]bool{"alpha": true, "beta": true},
			tags:      []string{"alpha", "beta"},
		},
		{
			name:      "no tags accepted",
			knownTags: map[string]bool{"alpha": true},
			tags:      nil,
		},
		{
			name:        "unknown tag rejected",
			knownTags:   map[string]bool{"alpha": true},
			tags:        []string{"ghost"},
			expectErr:   true,
			errContains: "not found",
		},
		{
			name:        "reserved tag rejected",
			knownTags:   map[string]bool{"alpha": true, "admin": true},
			reserved:    map[string]bool{"admin": true},
			tags:        []string{"admin"},
			expectErr:   true,
			errContains: "reserved",
		},
		{
			name:        "duplicate tag rejected",
			knownTags:   map[string]bool{"alpha": true},
			tags:        []string{"alpha", "alpha"},
			expectErr:   true,
			errContains: "duplicate",
		},
		{
			name:        "malformed tag rejected",
			knownTags:   map[string]bool{"Alpha": true},
			tags:        []string{"Alpha"},
			expectErr:   true,
			errContains: "format",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			f.repKeeper.knownTags = tc.knownTags
			f.repKeeper.reservedTags = tc.reserved

			resp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
				Creator:    f.owner,
				Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
				Visibility: types.Visibility_VISIBILITY_PUBLIC,
				Name:       "tagged-collection",
				Tags:       tc.tags,
			})

			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)

			// IncrementTagUsage called exactly once per tag on create.
			require.Len(t, f.repKeeper.incrementTagUsageCalls, len(tc.tags))
			for i, tag := range tc.tags {
				require.Equal(t, tag, f.repKeeper.incrementTagUsageCalls[i].Name)
			}

			// Secondary index reflects the tags.
			for _, tag := range tc.tags {
				listResp, qerr := f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: tag})
				require.NoError(t, qerr)
				require.Len(t, listResp.Collections, 1)
				require.Equal(t, resp.Id, listResp.Collections[0].Id)
			}
		})
	}
}

func TestUpdateCollectionTagDiff(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.knownTags = map[string]bool{"a": true, "b": true, "c": true}

	// Create a collection with tags a, b.
	createResp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "tagged-collection",
		Tags:       []string{"a", "b"},
	})
	require.NoError(t, err)
	require.Len(t, f.repKeeper.incrementTagUsageCalls, 2)

	// Update: drop b, add c (keep a).
	_, err = f.msgServer.UpdateCollection(f.ctx, &types.MsgUpdateCollection{
		Creator:    f.owner,
		Id:         createResp.Id,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Name:       "tagged-collection",
		Tags:       []string{"a", "c"},
	})
	require.NoError(t, err)

	// Only the newly-added "c" should bump usage — "a" was already present.
	require.Len(t, f.repKeeper.incrementTagUsageCalls, 3)
	require.Equal(t, "c", f.repKeeper.incrementTagUsageCalls[2].Name)

	// Index reflects the diff.
	resp, err := f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: "a"})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 1)
	resp, err = f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: "b"})
	require.NoError(t, err)
	require.Empty(t, resp.Collections)
	resp, err = f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: "c"})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 1)
}

func TestDeleteCollectionClearsTagIndex(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.knownTags = map[string]bool{"a": true}

	createResp, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "tagged-collection",
		Tags:       []string{"a"},
	})
	require.NoError(t, err)

	resp, err := f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: "a"})
	require.NoError(t, err)
	require.Len(t, resp.Collections, 1)

	_, err = f.msgServer.DeleteCollection(f.ctx, &types.MsgDeleteCollection{
		Creator: f.owner,
		Id:      createResp.Id,
	})
	require.NoError(t, err)

	resp, err = f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: "a"})
	require.NoError(t, err)
	require.Empty(t, resp.Collections)
}

func TestListCollectionsByTagEmptyTag(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.ListCollectionsByTag(f.ctx, &types.QueryListCollectionsByTagRequest{Tag: ""})
	require.Error(t, err)
}
