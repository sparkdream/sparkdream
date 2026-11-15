package keeper_test

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

const pinTestCreator = "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

func TestListPinnedPostsNilRequest(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)
	_, err := keeper.NewQueryServerImpl(k).ListPinnedPosts(ctx, nil)
	require.Error(t, err)
}

// Pins list newest first, page by page; unpinned, hidden and deleted posts
// are left out; the invariant holds throughout.
func TestListPinnedPosts(t *testing.T) {
	k, ms, ctx, _ := setupMsgServer(t)
	qs := keeper.NewQueryServerImpl(k)
	base := ctx.BlockTime()

	var ids []uint64
	for i := 0; i < 5; i++ {
		resp, err := ms.CreatePost(ctx, &types.MsgCreatePost{Creator: pinTestCreator, Title: "Post", Body: "body"})
		require.NoError(t, err)
		ids = append(ids, resp.Id)
	}
	// Pin 0..3 a second apart; 4 stays unpinned.
	for i, id := range ids[:4] {
		_, err := ms.PinPost(ctx.WithBlockTime(base.Add(time.Duration(i)*time.Second)), &types.MsgPinPost{Creator: pinTestCreator, Id: id})
		require.NoError(t, err)
	}

	list := func(req *query.PageRequest) ([]uint64, *query.PageResponse) {
		resp, err := qs.ListPinnedPosts(ctx, &types.QueryListPinnedPostsRequest{Pagination: req})
		require.NoError(t, err)
		var got []uint64
		for _, p := range resp.Posts {
			got = append(got, p.Id)
		}
		return got, resp.Pagination
	}

	got, _ := list(nil)
	require.Equal(t, []uint64{ids[3], ids[2], ids[1], ids[0]}, got)

	page1, pr := list(&query.PageRequest{Limit: 3})
	require.Equal(t, []uint64{ids[3], ids[2], ids[1]}, page1)
	require.NotEmpty(t, pr.NextKey)
	page2, pr := list(&query.PageRequest{Key: pr.NextKey, Limit: 3})
	require.Equal(t, []uint64{ids[0]}, page2)
	require.Empty(t, pr.NextKey)

	_, err := ms.UnpinPost(ctx, &types.MsgUnpinPost{Creator: pinTestCreator, Id: ids[2]})
	require.NoError(t, err)
	hidden, _ := k.GetPost(ctx, ids[1])
	hidden.Status = types.PostStatus_POST_STATUS_HIDDEN
	k.SetPost(ctx, hidden)
	deleted, _ := k.GetPost(ctx, ids[0])
	deleted.Status = types.PostStatus_POST_STATUS_DELETED
	k.SetPost(ctx, deleted)

	got, _ = list(nil)
	require.Equal(t, []uint64{ids[3]}, got)

	msg, broken := keeper.PinnedIndexInvariant(k)(ctx)
	require.False(t, broken, msg)

	// Re-pinning after an unpin indexes the post under its new pinned_at.
	_, err = ms.PinPost(ctx.WithBlockTime(base.Add(time.Hour)), &types.MsgPinPost{Creator: pinTestCreator, Id: ids[2]})
	require.NoError(t, err)
	got, _ = list(nil)
	require.Equal(t, []uint64{ids[2], ids[3]}, got)
}

// The v1 -> v2 migration indexes posts pinned before the index existed and
// drops entries that match no pinned post.
func TestRebuildPinnedPostIndex(t *testing.T) {
	k, ms, ctx, _ := setupMsgServer(t)
	qs := keeper.NewQueryServerImpl(k)

	var ids []uint64
	for i := 0; i < 3; i++ {
		resp, err := ms.CreatePost(ctx, &types.MsgCreatePost{Creator: pinTestCreator, Title: "Post", Body: "body"})
		require.NoError(t, err)
		ids = append(ids, resp.Id)
	}
	// Pinned the pre-index way: fields set, nothing indexed.
	for i, id := range ids[:2] {
		p, _ := k.GetPost(ctx, id)
		p.PinnedBy = pinTestCreator
		p.PinnedAt = int64(100 + i)
		k.SetPost(ctx, p)
	}
	_, broken := keeper.PinnedIndexInvariant(k)(ctx)
	require.True(t, broken)

	require.NoError(t, k.RebuildPinnedPostIndex(ctx))
	// Idempotent.
	require.NoError(t, k.RebuildPinnedPostIndex(ctx))

	msg, broken := keeper.PinnedIndexInvariant(k)(ctx)
	require.False(t, broken, msg)
	resp, err := qs.ListPinnedPosts(ctx, &types.QueryListPinnedPostsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Posts, 2)
	require.Equal(t, ids[1], resp.Posts[0].Id)
	require.Equal(t, ids[0], resp.Posts[1].Id)
}

// Genesis import rebuilds the index from the posts' pinned fields.
func TestPinnedIndexGenesisRoundTrip(t *testing.T) {
	k, ms, ctx, _ := setupMsgServer(t)
	resp, err := ms.CreatePost(ctx, &types.MsgCreatePost{Creator: pinTestCreator, Title: "Post", Body: "body"})
	require.NoError(t, err)
	_, err = ms.PinPost(ctx, &types.MsgPinPost{Creator: pinTestCreator, Id: resp.Id})
	require.NoError(t, err)
	gen, err := k.ExportGenesis(ctx)
	require.NoError(t, err)

	k2, _, ctx2, _ := setupMsgServer(t)
	require.NoError(t, k2.InitGenesis(ctx2, *gen))
	msg, broken := keeper.PinnedIndexInvariant(k2)(ctx2)
	require.False(t, broken, msg)
	out, err := keeper.NewQueryServerImpl(k2).ListPinnedPosts(ctx2, &types.QueryListPinnedPostsRequest{})
	require.NoError(t, err)
	require.Len(t, out.Posts, 1)
}
