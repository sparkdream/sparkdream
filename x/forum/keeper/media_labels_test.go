package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestPostMediaLabelsAndWithholding(t *testing.T) {
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

	post, err := f.keeper.Post.Get(f.ctx, postID)
	require.NoError(t, err)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_INLINE_DATA), post.MediaFlags)
	require.Equal(t, commontypes.MediaRulesVersion, post.MediaRulesVersion)
	require.Equal(t, commontypes.BodyHash(inline), post.BodyHash)

	got, err := qs.GetPost(f.ctx, &types.QueryGetPostRequest{PostId: postID})
	require.NoError(t, err)
	require.Empty(t, got.Post.Content)
	require.Equal(t, post.BodyHash, got.Post.BodyHash)

	threads, err := qs.Posts(f.ctx, &types.QueryPostsRequest{CategoryId: cat.CategoryId})
	require.NoError(t, err)
	require.Len(t, threads.Posts, 1)
	require.Empty(t, threads.Posts[0].Content)

	thread, err := qs.Thread(f.ctx, &types.QueryThreadRequest{RootId: postID})
	require.NoError(t, err)
	require.Empty(t, thread.Posts[0].Content)

	user, err := qs.UserPosts(f.ctx, &types.QueryUserPostsRequest{Author: testCreator})
	require.NoError(t, err)
	require.Empty(t, user.Posts[0].Content)

	all, err := qs.ListPost(f.ctx, &types.QueryAllPostRequest{})
	require.NoError(t, err)
	require.Empty(t, all.Post[0].Content)

	content, err := qs.PostContent(f.ctx, &types.QueryPostContentRequest{PostId: postID})
	require.NoError(t, err)
	require.Equal(t, inline, content.Content)
	require.Equal(t, commontypes.ContentType_CONTENT_TYPE_HTML, content.ContentType)
	require.Equal(t, post.MediaFlags, content.MediaFlags)
	require.Equal(t, post.BodyHash, content.BodyHash)

	_, err = qs.PostContent(f.ctx, &types.QueryPostContentRequest{PostId: 9999})
	require.Error(t, err)

	// Editing to plain text clears the label and the content shows again.
	_, err = f.msgServer.EditPost(f.ctx, &types.MsgEditPost{
		Creator:     testCreator,
		PostId:      postID,
		NewContent:  "Data: 5 records, all plain",
		ContentType: commontypes.ContentType_CONTENT_TYPE_TEXT,
	})
	require.NoError(t, err)
	got, err = qs.GetPost(f.ctx, &types.QueryGetPostRequest{PostId: postID})
	require.NoError(t, err)
	require.Zero(t, got.Post.MediaFlags)
	require.Equal(t, "Data: 5 records, all plain", got.Post.Content)
}

func TestDeletedPostPlaceholderIsNotMedia(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")

	_, err := f.msgServer.CreatePost(f.ctx, &types.MsgCreatePost{
		Creator:     testCreator,
		CategoryId:  cat.CategoryId,
		Content:     "bafyphoenix",
		ContentType: commontypes.ContentType_CONTENT_TYPE_IPFS,
	})
	require.NoError(t, err)
	postID := lastPostID(t, f)
	post, _ := f.keeper.Post.Get(f.ctx, postID)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_OFFCHAIN_REF), post.MediaFlags)

	_, err = f.msgServer.DeletePost(f.ctx, &types.MsgDeletePost{Creator: testCreator, PostId: postID})
	require.NoError(t, err)
	post, _ = f.keeper.Post.Get(f.ctx, postID)
	require.Equal(t, "[deleted]", post.Content)
	require.Equal(t, commontypes.ContentType_CONTENT_TYPE_IPFS, post.ContentType)
	require.Zero(t, post.MediaFlags, "the chain's own placeholder is never media")
	require.Equal(t, commontypes.BodyHash("[deleted]"), post.BodyHash)
}

func TestGenesisRecomputesPostMediaLabels(t *testing.T) {
	f := initFixture(t)

	gen := types.DefaultGenesis()
	gen.PostMap = []types.Post{
		{PostId: 1, Content: "H4sI", ContentType: commontypes.ContentType_CONTENT_TYPE_GZIP},
		{PostId: 2, Content: "[deleted]", ContentType: commontypes.ContentType_CONTENT_TYPE_IPFS, Status: types.PostStatus_POST_STATUS_DELETED},
		{PostId: 3, Content: "plain", MediaFlags: uint32(commontypes.MediaFlag_MEDIA_FLAG_INLINE_DATA)},
	}
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gen))

	p1, _ := f.keeper.Post.Get(f.ctx, 1)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_COMPRESSED), p1.MediaFlags)
	p2, _ := f.keeper.Post.Get(f.ctx, 2)
	require.Zero(t, p2.MediaFlags)
	p3, _ := f.keeper.Post.Get(f.ctx, 3)
	require.Zero(t, p3.MediaFlags, "imported labels are recomputed, not trusted")
}

// lastPostID returns the id of the most recently created post
// (MsgCreatePostResponse carries none).
func lastPostID(t *testing.T, f *fixture) uint64 {
	t.Helper()
	next, err := f.keeper.PostSeq.Peek(f.ctx)
	require.NoError(t, err)
	return next - 1
}

func TestForumMediaPostingRules(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")
	media := &types.MsgCreatePost{
		Creator: testCreator, CategoryId: cat.CategoryId,
		Content: "bafyzenith", ContentType: commontypes.ContentType_CONTENT_TYPE_IPFS,
	}

	// Low trust without a bond is refused; a big enough bond admits it.
	f.repKeeper.GetTrustLevelFn = func(sdk.AccAddress) uint64 { return 0 }
	_, err := f.msgServer.CreatePost(f.ctx, media)
	require.ErrorIs(t, err, types.ErrMediaNotPermitted)
	bond := commontypes.DefaultMediaAuthorBondMin
	withBond := *media
	withBond.AuthorBond = &bond
	_, err = f.msgServer.CreatePost(f.ctx, &withBond)
	require.NoError(t, err)

	// Edits into media need trust (no bond path).
	_, err = f.msgServer.CreatePost(f.ctx, &types.MsgCreatePost{Creator: testCreator, CategoryId: cat.CategoryId, Content: "plain"})
	require.NoError(t, err)
	plainID := lastPostID(t, f)
	_, err = f.msgServer.EditPost(f.ctx, &types.MsgEditPost{
		Creator: testCreator, PostId: plainID, NewContent: "data:,x", ContentType: commontypes.ContentType_CONTENT_TYPE_TEXT,
	})
	require.ErrorIs(t, err, types.ErrMediaNotPermitted)

	// Non-members cannot post media at all.
	f.repKeeper.GetTrustLevelFn = nil
	f.repKeeper.IsMemberFn = func(context.Context, sdk.AccAddress) bool { return false }
	_, err = f.msgServer.CreatePost(f.ctx, media)
	require.ErrorIs(t, err, types.ErrMediaNotPermitted)
}
