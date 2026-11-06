package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/testutil/contentguard"
	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
)

// TestEveryContentQueryWithholdsFlaggedBodies is the content-withholding guard
// (docs/content-scanning.md §3.4, review theme 5 of CSC-102): it walks the
// blog Query service, and every query whose response holds a Post or Reply
// must be exercised here against media-flagged records and return them with
// their bodies blanked. A new query that returns posts or replies fails until
// a case is added. PostBody and ReplyBody serve the body and return no
// record.
func TestEveryContentQueryWithholdsFlaggedBodies(t *testing.T) {
	k, ms, ctx, _, rep := setupMsgServerWithRep(t)
	qs := keeper.NewQueryServerImpl(k)
	rep.KnownTags = map[string]bool{"aurora": true}

	inline := `look ![x](data:image/png;base64,iVBORw0KGgo=)`
	post, err := ms.CreatePost(ctx, &types.MsgCreatePost{
		Creator:     mediaTestCreator,
		Title:       "Aurora",
		Body:        inline,
		ContentType: commontypes.ContentType_CONTENT_TYPE_MARKDOWN,
		Tags:        []string{"aurora"},
	})
	require.NoError(t, err)
	reply, err := ms.CreateReply(ctx, &types.MsgCreateReply{
		Creator:     mediaTestCreator,
		PostId:      post.Id,
		Body:        inline,
		ContentType: commontypes.ContentType_CONTENT_TYPE_MARKDOWN,
	})
	require.NoError(t, err)
	k.AddToExpiryIndex(ctx, 1_000, "post", post.Id)
	k.AddToExpiryIndex(ctx, 1_000, "reply", reply.Id)

	cases := map[string]func() (any, error){
		"ShowPost": func() (any, error) { return qs.ShowPost(ctx, &types.QueryShowPostRequest{Id: post.Id}) },
		"ListPost": func() (any, error) { return qs.ListPost(ctx, &types.QueryListPostRequest{}) },
		"ListPostsByCreator": func() (any, error) {
			return qs.ListPostsByCreator(ctx, &types.QueryListPostsByCreatorRequest{Creator: mediaTestCreator})
		},
		"ListPostsByTag": func() (any, error) {
			return qs.ListPostsByTag(ctx, &types.QueryListPostsByTagRequest{Tag: "aurora"})
		},
		"ShowReply": func() (any, error) { return qs.ShowReply(ctx, &types.QueryShowReplyRequest{Id: reply.Id}) },
		"ListReplies": func() (any, error) {
			return qs.ListReplies(ctx, &types.QueryListRepliesRequest{PostId: post.Id})
		},
		"ListExpiringContent": func() (any, error) {
			return qs.ListExpiringContent(ctx, &types.QueryListExpiringContentRequest{})
		},
	}

	methods := contentguard.QueriesReturning(t, "sparkdream.blog.v1.Query",
		"sparkdream.blog.v1.Post", "sparkdream.blog.v1.Reply")
	require.NotEmpty(t, methods)
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			call, ok := cases[m]
			require.True(t, ok, "query %s returns blog posts or replies: add a withholding case for it", m)
			resp, err := call()
			require.NoError(t, err)
			n := contentguard.Withheld(t, resp,
				func(p types.Post) bool { return p.MediaFlags != 0 },
				func(p types.Post) string { return p.Body })
			n += contentguard.Withheld(t, resp,
				func(r types.Reply) bool { return r.MediaFlags != 0 },
				func(r types.Reply) string { return r.Body })
			require.Positive(t, n, "query %s did not return the flagged record", m)
		})
	}
}
