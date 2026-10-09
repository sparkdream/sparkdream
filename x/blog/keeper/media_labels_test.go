package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

const mediaTestCreator = "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

func TestPostMediaLabelsAndWithholding(t *testing.T) {
	k, ms, ctx, _ := setupMsgServer(t)
	qs := keeper.NewQueryServerImpl(k)

	inlineBody := `look ![x](data:image/png;base64,iVBORw0KGgo=)`
	resp, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator:     mediaTestCreator,
		Title:       "Aurora",
		Body:        inlineBody,
		ContentType: commontypes.ContentType_CONTENT_TYPE_MARKDOWN,
	})
	require.NoError(t, err)

	post, found := k.GetPost(ctx, resp.Id)
	require.True(t, found)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_INLINE_DATA), post.MediaFlags)
	require.Equal(t, commontypes.MediaRulesVersion, post.MediaRulesVersion)
	require.Equal(t, commontypes.BodyHash(inlineBody), post.BodyHash)

	// Show and list withhold the flagged body; the body query returns it.
	show, err := qs.ShowPost(ctx, &types.QueryShowPostRequest{Id: resp.Id})
	require.NoError(t, err)
	require.Empty(t, show.Post.Body)
	require.Equal(t, "Aurora", show.Post.Title)
	require.Equal(t, post.BodyHash, show.Post.BodyHash)

	list, err := qs.ListPost(ctx, &types.QueryListPostRequest{})
	require.NoError(t, err)
	require.Len(t, list.Post, 1)
	require.Empty(t, list.Post[0].Body)

	body, err := qs.PostBody(ctx, &types.QueryPostBodyRequest{Id: resp.Id})
	require.NoError(t, err)
	require.Equal(t, inlineBody, body.Body)
	require.Equal(t, commontypes.ContentType_CONTENT_TYPE_MARKDOWN, body.ContentType)
	require.Equal(t, post.MediaFlags, body.MediaFlags)
	require.Equal(t, post.BodyHash, body.BodyHash)

	// Editing to plain text clears the label and the body shows again.
	_, err = ms.UpdatePost(ctx, &types.MsgUpdatePost{
		Creator:        mediaTestCreator,
		Id:             resp.Id,
		Title:          "Aurora",
		Body:           "metadata: nothing inline here",
		ContentType:    commontypes.ContentType_CONTENT_TYPE_TEXT,
		RepliesEnabled: true,
	})
	require.NoError(t, err)
	show, err = qs.ShowPost(ctx, &types.QueryShowPostRequest{Id: resp.Id})
	require.NoError(t, err)
	require.Zero(t, show.Post.MediaFlags)
	require.Equal(t, "metadata: nothing inline here", show.Post.Body)
	require.Equal(t, commontypes.BodyHash("metadata: nothing inline here"), show.Post.BodyHash)

	// Editing to an off-chain reference labels it from content_type alone.
	_, err = ms.UpdatePost(ctx, &types.MsgUpdatePost{
		Creator:        mediaTestCreator,
		Id:             resp.Id,
		Title:          "Aurora",
		Body:           "bafyzenith",
		ContentType:    commontypes.ContentType_CONTENT_TYPE_IPFS,
		RepliesEnabled: true,
	})
	require.NoError(t, err)
	post, _ = k.GetPost(ctx, resp.Id)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_OFFCHAIN_REF), post.MediaFlags)

	// Deleting tombstones the body, which is never media.
	_, err = ms.DeletePost(ctx, &types.MsgDeletePost{Creator: mediaTestCreator, Id: resp.Id})
	require.NoError(t, err)
	post, _ = k.GetPost(ctx, resp.Id)
	require.Zero(t, post.MediaFlags)
	require.Nil(t, post.BodyHash)
}

func TestReplyMediaLabelsAndWithholding(t *testing.T) {
	k, ms, ctx, _ := setupMsgServer(t)
	qs := keeper.NewQueryServerImpl(k)

	postResp, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator: mediaTestCreator,
		Title:   "Zenith",
		Body:    "plain post",
	})
	require.NoError(t, err)

	post, _ := k.GetPost(ctx, postResp.Id)
	require.Zero(t, post.MediaFlags)
	show, err := qs.ShowPost(ctx, &types.QueryShowPostRequest{Id: postResp.Id})
	require.NoError(t, err)
	require.Equal(t, "plain post", show.Post.Body, "plain text is never withheld")

	replyResp, err := ms.CreateReply(ctx, &types.MsgCreateReply{
		Creator:     mediaTestCreator,
		PostId:      postResp.Id,
		Body:        "H4sIAAAAAAAA",
		ContentType: commontypes.ContentType_CONTENT_TYPE_GZIP,
	})
	require.NoError(t, err)

	reply, found := k.GetReply(ctx, replyResp.Id)
	require.True(t, found)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_COMPRESSED), reply.MediaFlags)

	showReply, err := qs.ShowReply(ctx, &types.QueryShowReplyRequest{Id: replyResp.Id})
	require.NoError(t, err)
	require.Empty(t, showReply.Reply.Body)

	replies, err := qs.ListReplies(ctx, &types.QueryListRepliesRequest{PostId: postResp.Id})
	require.NoError(t, err)
	require.Len(t, replies.Replies, 1)
	require.Empty(t, replies.Replies[0].Body)

	body, err := qs.ReplyBody(ctx, &types.QueryReplyBodyRequest{Id: replyResp.Id})
	require.NoError(t, err)
	require.Equal(t, "H4sIAAAAAAAA", body.Body)
	require.Equal(t, reply.BodyHash, body.BodyHash)

	_, err = qs.ReplyBody(ctx, &types.QueryReplyBodyRequest{Id: 9999})
	require.Error(t, err)
}

func TestGenesisRecomputesMediaLabels(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)

	// A record exported by a binary that predates labels carries none.
	gen := types.DefaultGenesis()
	gen.Posts = []types.Post{{Id: 1, Creator: mediaTestCreator, Body: "data:,x", ContentType: commontypes.ContentType_CONTENT_TYPE_TEXT}}
	gen.PostCount = 2
	gen.Replies = []types.Reply{{Id: 1, PostId: 1, Creator: mediaTestCreator, Body: "cid", ContentType: commontypes.ContentType_CONTENT_TYPE_JACKAL, MediaFlags: 0}}
	gen.ReplyCount = 2
	require.NoError(t, k.InitGenesis(ctx, *gen))

	post, _ := k.GetPost(ctx, 1)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_INLINE_DATA), post.MediaFlags)
	require.Equal(t, commontypes.BodyHash("data:,x"), post.BodyHash)
	reply, _ := k.GetReply(ctx, 1)
	require.Equal(t, uint32(commontypes.MediaFlag_MEDIA_FLAG_OFFCHAIN_REF), reply.MediaFlags)
}

func TestMediaPostingRules(t *testing.T) {
	gzipBody := "H4sIAAAAAAAA"
	newMsg := func(creator string, bond *math.Int) *types.MsgCreatePost {
		return &types.MsgCreatePost{
			Creator: creator, Title: "Phoenix", Body: gzipBody,
			ContentType: commontypes.ContentType_CONTENT_TYPE_GZIP, AuthorBond: bond,
		}
	}

	t.Run("plain text is never gated", func(t *testing.T) {
		_, ms, ctx, _, rep := setupMsgServerWithRep(t)
		rep.IsActiveMemberFn = func(context.Context, sdk.AccAddress) bool { return false }
		_, err := ms.CreatePost(ctx, &types.MsgCreatePost{Creator: mediaTestCreator, Title: "t", Body: "Data: 5 records, plain"})
		require.NoError(t, err)
	})

	t.Run("non-members cannot post media", func(t *testing.T) {
		_, ms, ctx, _, rep := setupMsgServerWithRep(t)
		rep.IsActiveMemberFn = func(context.Context, sdk.AccAddress) bool { return false }
		_, err := ms.CreatePost(ctx, newMsg(mediaTestCreator, nil))
		require.ErrorIs(t, err, types.ErrMediaNotPermitted)
	})

	t.Run("low trust needs a bond", func(t *testing.T) {
		_, ms, ctx, _, rep := setupMsgServerWithRep(t)
		rep.GetTrustLevelFn = func(context.Context, sdk.AccAddress) (reptypes.TrustLevel, error) {
			return reptypes.TrustLevel_TRUST_LEVEL_NEW, nil
		}
		_, err := ms.CreatePost(ctx, newMsg(mediaTestCreator, nil))
		require.ErrorIs(t, err, types.ErrMediaNotPermitted)

		small := commontypes.DefaultMediaAuthorBondMin.SubRaw(1)
		_, err = ms.CreatePost(ctx, newMsg(mediaTestCreator, &small))
		require.ErrorIs(t, err, types.ErrMediaNotPermitted)

		enough := commontypes.DefaultMediaAuthorBondMin
		_, err = ms.CreatePost(ctx, newMsg(mediaTestCreator, &enough))
		require.NoError(t, err)

		// Replies follow the same rule; edits have no bond path.
		_, err = ms.CreateReply(ctx, &types.MsgCreateReply{
			Creator: mediaTestCreator, PostId: 1, Body: "data:,x", ContentType: commontypes.ContentType_CONTENT_TYPE_TEXT,
		})
		require.ErrorIs(t, err, types.ErrMediaNotPermitted)
	})

	t.Run("edits into media need trust", func(t *testing.T) {
		_, ms, ctx, _, rep := setupMsgServerWithRep(t)
		resp, err := ms.CreatePost(ctx, &types.MsgCreatePost{Creator: mediaTestCreator, Title: "t", Body: "plain"})
		require.NoError(t, err)
		rep.GetTrustLevelFn = func(context.Context, sdk.AccAddress) (reptypes.TrustLevel, error) {
			return reptypes.TrustLevel_TRUST_LEVEL_NEW, nil
		}
		_, err = ms.UpdatePost(ctx, &types.MsgUpdatePost{
			Creator: mediaTestCreator, Id: resp.Id, Title: "t", Body: "bafyaurora",
			ContentType: commontypes.ContentType_CONTENT_TYPE_IPFS, RepliesEnabled: true,
		})
		require.ErrorIs(t, err, types.ErrMediaNotPermitted)
	})

	t.Run("anonymous content cannot carry media", func(t *testing.T) {
		_, ms, ctx, _ := setupMsgServer(t)
		shield := sdk.MustBech32ifyAddressBytes("sprkdrm", authtypes.NewModuleAddress("shield"))
		_, err := ms.CreatePost(ctx, newMsg(shield, nil))
		require.ErrorIs(t, err, types.ErrMediaNotPermitted)
	})

	t.Run("scan fee is charged and burned for media only", func(t *testing.T) {
		k, ms, ctx, bk := setupMsgServer(t)
		params, err := k.Params.Get(ctx)
		require.NoError(t, err)
		params.MediaScanFee = math.NewInt(777)
		params.CostPerByteExempt = true
		require.NoError(t, k.Params.Set(ctx, params))

		_, err = ms.CreatePost(ctx, &types.MsgCreatePost{Creator: mediaTestCreator, Title: "t", Body: "plain"})
		require.NoError(t, err)
		require.Empty(t, bk.BurnCoinsCalls, "plain text pays no scan fee")

		_, err = ms.CreatePost(ctx, newMsg(mediaTestCreator, nil))
		require.NoError(t, err)
		require.Len(t, bk.BurnCoinsCalls, 1)
		require.Equal(t, int64(777), bk.BurnCoinsCalls[0].Amt[0].Amount.Int64())
	})
}
