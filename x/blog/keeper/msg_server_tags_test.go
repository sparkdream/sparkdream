package keeper_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

const tagTestCreator = "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

func TestCreatePostTags(t *testing.T) {
	tests := []struct {
		name        string
		knownTags   map[string]bool
		reserved    map[string]bool
		tags        []string
		expectErr   bool
		errContains string
	}{
		{
			name:      "valid tags accepted",
			knownTags: map[string]bool{"governance": true, "treasury": true},
			tags:      []string{"governance", "treasury"},
		},
		{
			name:      "empty tag list accepted",
			knownTags: map[string]bool{"governance": true},
			tags:      nil,
		},
		{
			name:        "unknown tag rejected",
			knownTags:   map[string]bool{"governance": true},
			tags:        []string{"not-registered"},
			expectErr:   true,
			errContains: "tag not found",
		},
		{
			name:        "reserved tag rejected",
			knownTags:   map[string]bool{"governance": true, "admin": true},
			reserved:    map[string]bool{"admin": true},
			tags:        []string{"admin"},
			expectErr:   true,
			errContains: "reserved",
		},
		{
			name:        "duplicate tag rejected",
			knownTags:   map[string]bool{"governance": true},
			tags:        []string{"governance", "governance"},
			expectErr:   true,
			errContains: "duplicate tag",
		},
		{
			name:        "too many tags rejected",
			knownTags:   map[string]bool{"a": true, "b": true, "c": true, "d": true, "e": true, "f": true},
			tags:        []string{"a", "b", "c", "d", "e", "f"},
			expectErr:   true,
			errContains: "tag limit exceeded",
		},
		{
			name:        "tag too long rejected",
			knownTags:   map[string]bool{strings.Repeat("a", 33): true},
			tags:        []string{strings.Repeat("a", 33)},
			expectErr:   true,
			errContains: "exceeds max length",
		},
		{
			name:        "malformed tag rejected (uppercase)",
			knownTags:   map[string]bool{"Governance": true},
			tags:        []string{"Governance"},
			expectErr:   true,
			errContains: "required format",
		},
		{
			name:        "malformed tag rejected (starts with hyphen)",
			knownTags:   map[string]bool{"-gov": true},
			tags:        []string{"-gov"},
			expectErr:   true,
			errContains: "required format",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ms, ctx, _, rep := setupMsgServerWithRep(t)
			rep.KnownTags = tc.knownTags
			rep.ReservedTags = tc.reserved

			resp, err := ms.CreatePost(ctx, &types.MsgCreatePost{
				Creator: tagTestCreator,
				Title:   "Tag Test",
				Body:    "Body",
				Tags:    tc.tags,
			})

			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)

			// IncrementTagUsage called once per tag on create
			require.Len(t, rep.IncrementTagUsageCalls, len(tc.tags))
			for i, tag := range tc.tags {
				require.Equal(t, tag, rep.IncrementTagUsageCalls[i].Name)
			}
		})
	}
}

func TestUpdatePostTagsDiff(t *testing.T) {
	k, ms, ctx, _, rep := setupMsgServerWithRep(t)
	qs := keeper.NewQueryServerImpl(k)
	rep.KnownTags = map[string]bool{"a": true, "b": true, "c": true}

	// Create a post with tags a, b
	createResp, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "Original",
		Body:    "Body",
		Tags:    []string{"a", "b"},
	})
	require.NoError(t, err)
	require.Len(t, rep.IncrementTagUsageCalls, 2)

	// Pre-update: index should contain a and b entries for this post.
	requireListByTag(t, qs, ctx, "a", []uint64{createResp.Id})
	requireListByTag(t, qs, ctx, "b", []uint64{createResp.Id})
	requireListByTag(t, qs, ctx, "c", nil)

	// Update: drop b, add c. Keep a.
	_, err = ms.UpdatePost(ctx, &types.MsgUpdatePost{
		Creator:        tagTestCreator,
		Id:             createResp.Id,
		Title:          "Updated",
		Body:           "Body updated",
		RepliesEnabled: true,
		Tags:           []string{"a", "c"},
	})
	require.NoError(t, err)

	// Post now carries a, c.
	post, found := k.GetPost(ctx, createResp.Id)
	require.True(t, found)
	require.Equal(t, []string{"a", "c"}, post.Tags)

	// Index reflects the diff.
	requireListByTag(t, qs, ctx, "a", []uint64{createResp.Id})
	requireListByTag(t, qs, ctx, "b", nil)
	requireListByTag(t, qs, ctx, "c", []uint64{createResp.Id})

	// IncrementTagUsage fires only for newly-added tags on edit (BLOG-S2-3).
	// Initial create incremented a, b (2); update added c only (+1).
	require.Len(t, rep.IncrementTagUsageCalls, 2+1)

	// DecrementTagUsage fires for tags dropped from the post — without this
	// the rep registry's UsageCount drifts upward over edits and ExpireTags
	// can never reclaim the slot. (Was a regression: the diff used to run
	// only inside `if len(msg.Tags) > 0`, and only handled the add direction.)
	require.Equal(t, []string{"b"}, rep.DecrementTagUsageCalls)
}

// Regression: an update that clears all tags (msg.Tags == nil/empty) must
// still decrement usage on every previously attached tag. The pre-fix code
// gated the entire diff on `len(msg.Tags) > 0` and silently leaked usage.
func TestUpdatePostTagsDiff_ClearAll(t *testing.T) {
	_, ms, ctx, _, rep := setupMsgServerWithRep(t)
	rep.KnownTags = map[string]bool{"a": true, "b": true}

	createResp, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "Original",
		Body:    "Body",
		Tags:    []string{"a", "b"},
	})
	require.NoError(t, err)
	require.Len(t, rep.IncrementTagUsageCalls, 2)

	_, err = ms.UpdatePost(ctx, &types.MsgUpdatePost{
		Creator:        tagTestCreator,
		Id:             createResp.Id,
		Title:          "Updated",
		Body:           "Body updated",
		RepliesEnabled: true,
		// Tags: nil — explicit clear.
	})
	require.NoError(t, err)

	require.Len(t, rep.IncrementTagUsageCalls, 2, "no new increments on a clear")
	require.ElementsMatch(t, []string{"a", "b"}, rep.DecrementTagUsageCalls)
}

func TestDeletePostClearsTagIndex(t *testing.T) {
	k, ms, ctx, _, rep := setupMsgServerWithRep(t)
	qs := keeper.NewQueryServerImpl(k)
	rep.KnownTags = map[string]bool{"a": true}

	createResp, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "Post",
		Body:    "Body",
		Tags:    []string{"a"},
	})
	require.NoError(t, err)

	requireListByTag(t, qs, ctx, "a", []uint64{createResp.Id})

	_, err = ms.DeletePost(ctx, &types.MsgDeletePost{
		Creator: tagTestCreator,
		Id:      createResp.Id,
	})
	require.NoError(t, err)

	// Tombstoned posts must not appear in ListPostsByTag.
	requireListByTag(t, qs, ctx, "a", nil)
}

// Regression: DeletePost must call DecrementTagUsage for every tag the post
// carried — without this, repeated create/delete cycles inflate UsageCount
// in the rep registry and ExpireTags loses its ability to reclaim slots.
// Cousin of the update-side BLOG-S2-4 / FORUM-S2-3 / COLLECT-S2-7 fixes.
func TestDeletePostDecrementsTagUsage(t *testing.T) {
	_, ms, ctx, _, rep := setupMsgServerWithRep(t)
	rep.KnownTags = map[string]bool{"a": true, "b": true, "c": true}

	createResp, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "Multi-tag",
		Body:    "Body",
		Tags:    []string{"a", "b", "c"},
	})
	require.NoError(t, err)
	require.Len(t, rep.IncrementTagUsageCalls, 3)
	require.Empty(t, rep.DecrementTagUsageCalls)

	_, err = ms.DeletePost(ctx, &types.MsgDeletePost{
		Creator: tagTestCreator,
		Id:      createResp.Id,
	})
	require.NoError(t, err)

	// One decrement per tag the post carried — order doesn't matter.
	require.ElementsMatch(t, []string{"a", "b", "c"}, rep.DecrementTagUsageCalls,
		"every tag on the deleted post must be decremented exactly once")
}

// Regression: deleting an untagged post must NOT call DecrementTagUsage at
// all — a no-op decrement could surface ErrNotFound from the rep registry
// or, worse, double-decrement a separately-tracked tag if some upstream
// caller accidentally passes an empty/duplicate list.
func TestDeletePostNoTags_NoDecrement(t *testing.T) {
	_, ms, ctx, _, rep := setupMsgServerWithRep(t)

	createResp, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "No tags",
		Body:    "Body",
		// Tags: nil
	})
	require.NoError(t, err)
	require.Empty(t, rep.IncrementTagUsageCalls)

	_, err = ms.DeletePost(ctx, &types.MsgDeletePost{
		Creator: tagTestCreator,
		Id:      createResp.Id,
	})
	require.NoError(t, err)
	require.Empty(t, rep.DecrementTagUsageCalls,
		"deleting an untagged post must not call DecrementTagUsage")
}

func TestListPostsByTagPagination(t *testing.T) {
	k, ms, ctx, _, rep := setupMsgServerWithRep(t)
	qs := keeper.NewQueryServerImpl(k)
	rep.KnownTags = map[string]bool{"shared": true, "alone": true}

	// Two posts carry "shared", one carries "alone".
	r1, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "One",
		Body:    "b",
		Tags:    []string{"shared"},
	})
	require.NoError(t, err)
	r2, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "Two",
		Body:    "b",
		Tags:    []string{"shared"},
	})
	require.NoError(t, err)
	_, err = ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "Three",
		Body:    "b",
		Tags:    []string{"alone"},
	})
	require.NoError(t, err)

	requireListByTag(t, qs, ctx, "shared", []uint64{r1.Id, r2.Id})
	requireListByTag(t, qs, ctx, "nonexistent", nil)

	// Empty tag rejected.
	_, err = qs.ListPostsByTag(ctx, &types.QueryListPostsByTagRequest{Tag: ""})
	require.Error(t, err)
}

func requireListByTag(t *testing.T, qs types.QueryServer, ctx context.Context, tag string, expect []uint64) {
	t.Helper()
	resp, err := qs.ListPostsByTag(ctx, &types.QueryListPostsByTagRequest{Tag: tag})
	require.NoError(t, err)
	if len(expect) == 0 {
		require.Empty(t, resp.Posts, "tag %q", tag)
		return
	}
	got := make([]uint64, 0, len(resp.Posts))
	for _, p := range resp.Posts {
		got = append(got, p.Id)
	}
	require.ElementsMatch(t, expect, got, "tag %q", tag)
}
