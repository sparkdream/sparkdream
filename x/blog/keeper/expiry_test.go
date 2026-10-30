package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestAddAndRemoveFromExpiryIndex(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create a real post so the query can find it
	postId := k.AppendPost(ctx, types.Post{
		Creator: creator,
		Title:   "Expiring Post",
		Body:    "This post will expire",
		Status:  types.PostStatus_POST_STATUS_ACTIVE,
	})

	expiresAt := int64(1000)
	k.AddToExpiryIndex(ctx, expiresAt, "post", postId)

	t.Run("entry exists after adding", func(t *testing.T) {
		resp, err := qs.ListExpiringContent(ctx, &types.QueryListExpiringContentRequest{
			ExpiresBefore: 2000,
		})
		require.NoError(t, err)
		require.Len(t, resp.Posts, 1)
		require.Equal(t, postId, resp.Posts[0].Id)
		require.Equal(t, "Expiring Post", resp.Posts[0].Title)
	})

	t.Run("entry gone after removing", func(t *testing.T) {
		k.RemoveFromExpiryIndex(ctx, expiresAt, "post", postId)

		resp, err := qs.ListExpiringContent(ctx, &types.QueryListExpiringContentRequest{
			ExpiresBefore: 2000,
		})
		require.NoError(t, err)
		require.Len(t, resp.Posts, 0)
	})
}

func TestMultipleExpiryEntries(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create posts with different expiry timestamps
	postId1 := k.AppendPost(ctx, types.Post{
		Creator: creator,
		Title:   "Post Early Expiry",
		Body:    "Expires early",
		Status:  types.PostStatus_POST_STATUS_ACTIVE,
	})
	postId2 := k.AppendPost(ctx, types.Post{
		Creator: creator,
		Title:   "Post Late Expiry",
		Body:    "Expires late",
		Status:  types.PostStatus_POST_STATUS_ACTIVE,
	})

	// Create a reply with its own expiry
	replyId := k.AppendReply(ctx, types.Reply{
		PostId:  postId1,
		Creator: creator,
		Body:    "Expiring reply",
		Status:  types.ReplyStatus_REPLY_STATUS_ACTIVE,
	})

	k.AddToExpiryIndex(ctx, 500, "post", postId1)
	k.AddToExpiryIndex(ctx, 1500, "post", postId2)
	k.AddToExpiryIndex(ctx, 1000, "reply", replyId)

	tests := []struct {
		name            string
		expiresBefore   int64
		contentType     string
		expectedPosts   int
		expectedReplies int
	}{
		{
			name:            "all content before 2000",
			expiresBefore:   2000,
			contentType:     "",
			expectedPosts:   2,
			expectedReplies: 1,
		},
		{
			name:            "content before 600 gets only early post",
			expiresBefore:   600,
			contentType:     "",
			expectedPosts:   1,
			expectedReplies: 0,
		},
		{
			name:            "content before 1100 gets early post and reply",
			expiresBefore:   1100,
			contentType:     "",
			expectedPosts:   1,
			expectedReplies: 1,
		},
		{
			name:            "content before 100 gets nothing",
			expiresBefore:   100,
			contentType:     "",
			expectedPosts:   0,
			expectedReplies: 0,
		},
		{
			name:            "filter by post type only",
			expiresBefore:   2000,
			contentType:     "post",
			expectedPosts:   2,
			expectedReplies: 0,
		},
		{
			name:            "filter by reply type only",
			expiresBefore:   2000,
			contentType:     "reply",
			expectedPosts:   0,
			expectedReplies: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := qs.ListExpiringContent(ctx, &types.QueryListExpiringContentRequest{
				ExpiresBefore: tt.expiresBefore,
				ContentType:   tt.contentType,
			})
			require.NoError(t, err)
			require.Len(t, resp.Posts, tt.expectedPosts, "unexpected number of posts")
			require.Len(t, resp.Replies, tt.expectedReplies, "unexpected number of replies")
		})
	}
}

func TestExpiryIndexDeletedContentSkipped(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	// Create a post and mark it as deleted
	postId := k.AppendPost(ctx, types.Post{
		Creator: creator,
		Title:   "Deleted Post",
		Body:    "This post is deleted",
		Status:  types.PostStatus_POST_STATUS_DELETED,
	})

	k.AddToExpiryIndex(ctx, 500, "post", postId)

	t.Run("deleted posts not returned in expiry query", func(t *testing.T) {
		resp, err := qs.ListExpiringContent(ctx, &types.QueryListExpiringContentRequest{
			ExpiresBefore: 1000,
		})
		require.NoError(t, err)
		require.Len(t, resp.Posts, 0, "deleted posts should be excluded")
	})

	// Same for a deleted reply
	replyId := k.AppendReply(ctx, types.Reply{
		PostId:  postId,
		Creator: creator,
		Body:    "Deleted reply",
		Status:  types.ReplyStatus_REPLY_STATUS_DELETED,
	})

	k.AddToExpiryIndex(ctx, 500, "reply", replyId)

	t.Run("deleted replies not returned in expiry query", func(t *testing.T) {
		resp, err := qs.ListExpiringContent(ctx, &types.QueryListExpiringContentRequest{
			ExpiresBefore: 1000,
		})
		require.NoError(t, err)
		require.Len(t, resp.Replies, 0, "deleted replies should be excluded")
	})
}

func TestExpiryIndexNilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.ListExpiringContent(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}

// BLG-04: ListExpiringContent pages with next_key, and filtered or deleted
// entries count against a scan budget, so no page walks the whole index.
func TestListExpiringContentPaginates(t *testing.T) {
	f := initFixture(t)
	ctx := f.ctx
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	for i := 0; i < 25; i++ {
		id := k.AppendPost(ctx, types.Post{Creator: creator, Title: "aurora", Body: "b", Status: types.PostStatus_POST_STATUS_ACTIVE})
		k.AddToExpiryIndex(ctx, int64(1000+i), "post", id)
	}

	seen := map[uint64]bool{}
	var key []byte
	for page := 0; ; page++ {
		require.Less(t, page, 20)
		resp, err := qs.ListExpiringContent(ctx, &types.QueryListExpiringContentRequest{
			Pagination: &query.PageRequest{Key: key, Limit: 10},
		})
		require.NoError(t, err)
		require.LessOrEqual(t, len(resp.Posts), 10)
		for _, p := range resp.Posts {
			require.False(t, seen[p.Id], "post %d on two pages", p.Id)
			seen[p.Id] = true
		}
		key = resp.Pagination.NextKey
		if len(key) == 0 {
			break
		}
	}
	require.Len(t, seen, 25)

	// Every entry filtered out: the page stops at the scan budget (limit x
	// 10) with a resume key instead of scanning on.
	resp, err := qs.ListExpiringContent(ctx, &types.QueryListExpiringContentRequest{
		ContentType: "reply",
		Pagination:  &query.PageRequest{Limit: 1},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Replies)
	require.NotEmpty(t, resp.Pagination.NextKey)
}
