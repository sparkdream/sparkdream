package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func TestListPostsByTagNilRequest(t *testing.T) {
	k, _, ctx, _, _ := setupMsgServerWithRep(t)
	qs := keeper.NewQueryServerImpl(k)

	_, err := qs.ListPostsByTag(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}

func TestListPostsByTagEmptyTag(t *testing.T) {
	k, _, ctx, _, _ := setupMsgServerWithRep(t)
	qs := keeper.NewQueryServerImpl(k)

	_, err := qs.ListPostsByTag(ctx, &types.QueryListPostsByTagRequest{Tag: ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "tag cannot be empty")
}

func TestListPostsByTagUnknownTag(t *testing.T) {
	k, _, ctx, _, rep := setupMsgServerWithRep(t)
	qs := keeper.NewQueryServerImpl(k)
	rep.KnownTags = map[string]bool{"gov": true}

	resp, err := qs.ListPostsByTag(ctx, &types.QueryListPostsByTagRequest{Tag: "nonexistent"})
	require.NoError(t, err)
	require.Empty(t, resp.Posts)
}

func TestListPostsByTagPaginationLimit(t *testing.T) {
	k, ms, ctx, _, rep := setupMsgServerWithRep(t)
	qs := keeper.NewQueryServerImpl(k)
	rep.KnownTags = map[string]bool{"shared": true}

	// Create 3 posts with the same tag.
	for i := 0; i < 3; i++ {
		_, err := ms.CreatePost(ctx, &types.MsgCreatePost{
			Creator: tagTestCreator,
			Title:   "Post",
			Body:    "body",
			Tags:    []string{"shared"},
		})
		require.NoError(t, err)
	}

	// Page 1: limit=2.
	page1, err := qs.ListPostsByTag(ctx, &types.QueryListPostsByTagRequest{
		Tag:        "shared",
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, page1.Posts, 2)
	require.NotNil(t, page1.Pagination)
	require.NotEmpty(t, page1.Pagination.NextKey, "expected NextKey for continuation")

	// Page 2: use NextKey to fetch remaining.
	page2, err := qs.ListPostsByTag(ctx, &types.QueryListPostsByTagRequest{
		Tag:        "shared",
		Pagination: &query.PageRequest{Key: page1.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, page2.Posts, 1)
	require.Empty(t, page2.Pagination.NextKey, "expected no NextKey on last page")
}

// Tags sharing a common prefix (e.g. "gov" and "gov2") must not bleed into each
// other's ByTag result because of the '/' separator in tagPostIndexKey.
func TestListPostsByTagPrefixIsolation(t *testing.T) {
	k, ms, ctx, _, rep := setupMsgServerWithRep(t)
	qs := keeper.NewQueryServerImpl(k)
	rep.KnownTags = map[string]bool{"gov": true, "gov2": true}

	r1, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "P1",
		Body:    "body",
		Tags:    []string{"gov"},
	})
	require.NoError(t, err)
	r2, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: tagTestCreator,
		Title:   "P2",
		Body:    "body",
		Tags:    []string{"gov2"},
	})
	require.NoError(t, err)

	govResp, err := qs.ListPostsByTag(ctx, &types.QueryListPostsByTagRequest{Tag: "gov"})
	require.NoError(t, err)
	require.Len(t, govResp.Posts, 1)
	require.Equal(t, r1.Id, govResp.Posts[0].Id)

	gov2Resp, err := qs.ListPostsByTag(ctx, &types.QueryListPostsByTagRequest{Tag: "gov2"})
	require.NoError(t, err)
	require.Len(t, gov2Resp.Posts, 1)
	require.Equal(t, r2.Id, gov2Resp.Posts[0].Id)
}
