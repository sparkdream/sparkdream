package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/testutil/contentguard"
	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// TestEveryPostQueryWithholdsFlaggedContent is the CSC-102 guard
// (docs/content-scanning.md §3.4): it walks the forum Query service, and
// every query whose response holds a Post must be exercised here against a
// media-flagged post and return it with its content blanked. A new query
// that returns posts fails until a case is added. PostContent is the one
// query that serves the body; it returns no Post record.
func TestEveryPostQueryWithholdsFlaggedContent(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	cat := f.createTestCategory(t, "General")

	inline := `<p>hi</p><img src="data:image/png;base64,iVBORw0KGgo=">`
	_, err := f.msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator:     testCreator,
		CategoryId:  cat.CategoryId,
		Content:     inline,
		ContentType: commontypes.ContentType_CONTENT_TYPE_HTML,
	})
	require.NoError(t, err)
	postID := lastPostID(t, f)

	// Make the root reachable from every index-backed list query.
	post, err := f.keeper.Post.Get(f.ctx, postID)
	require.NoError(t, err)
	require.NotZero(t, post.MediaFlags)
	post.Pinned, post.Locked = true, true
	require.NoError(t, f.keeper.Post.Set(f.ctx, postID, post))
	require.NoError(t, f.keeper.PostsByPinned.Set(f.ctx, collections.Join(post.CategoryId, postID)))
	require.NoError(t, f.keeper.LockedRoots.Set(f.ctx, postID))

	cases := map[string]func() (any, error){
		"GetPost": func() (any, error) {
			return qs.GetPost(f.ctx, &types.QueryGetPostRequest{PostId: postID})
		},
		"ListPost": func() (any, error) { return qs.ListPost(f.ctx, &types.QueryAllPostRequest{}) },
		"Posts": func() (any, error) {
			return qs.Posts(f.ctx, &types.QueryPostsRequest{CategoryId: cat.CategoryId})
		},
		"Thread": func() (any, error) {
			return qs.Thread(f.ctx, &types.QueryThreadRequest{RootId: postID})
		},
		"UserPosts": func() (any, error) {
			return qs.UserPosts(f.ctx, &types.QueryUserPostsRequest{Author: testCreator})
		},
		"TopPosts":      func() (any, error) { return qs.TopPosts(f.ctx, &types.QueryTopPostsRequest{}) },
		"PinnedPosts":   func() (any, error) { return qs.PinnedPosts(f.ctx, &types.QueryPinnedPostsRequest{}) },
		"LockedThreads": func() (any, error) { return qs.LockedThreads(f.ctx, &types.QueryLockedThreadsRequest{}) },
	}

	methods := contentguard.QueriesReturning(t, "sparkdream.forum.v1.Query", "sparkdream.forum.v1.Post")
	require.NotEmpty(t, methods)
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			call, ok := cases[m]
			require.True(t, ok, "query %s returns forum Posts: add a withholding case for it", m)
			resp, err := call()
			require.NoError(t, err)
			n := contentguard.Withheld(t, resp,
				func(p types.Post) bool { return p.MediaFlags != 0 },
				func(p types.Post) string { return p.Content })
			require.Positive(t, n, "query %s did not return the flagged post", m)
		})
	}
}
