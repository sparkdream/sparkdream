package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// TestMakePostPermanent covers the moved-out ephemeral-to-permanent
// promotion logic. Pin markers are not affected; the expiry index and the
// EphemeralByAuthor index are both cleared.
func TestMakePostPermanent(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	createEphemeralPost := func(t *testing.T) (keeper.Keeper, types.MsgServer, sdk.Context, uint64) {
		t.Helper()
		k, msgServer, ctx, _ := setupMsgServer(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Ephemeral Post",
			Body:    "...",
		})
		require.NoError(t, err)
		post, _ := k.GetPost(ctx, resp.Id)
		post.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
		k.SetPost(ctx, post)
		k.AddToExpiryIndex(ctx, post.ExpiresAt, "post", post.Id)
		k.AddEphemeralAuthorIndex(ctx, creator, keeper.EphemeralKindPost, post.Id)
		return k, msgServer, ctx, resp.Id
	}

	t.Run("successful promotion of ephemeral post", func(t *testing.T) {
		k, msgServer, ctx, postId := createEphemeralPost(t)

		_, err := msgServer.MakePostPermanent(ctx, &types.MsgMakePostPermanent{
			Creator: creator,
			Id:      postId,
		})
		require.NoError(t, err)

		post, found := k.GetPost(ctx, postId)
		require.True(t, found)
		require.Equal(t, int64(0), post.ExpiresAt, "ExpiresAt must be cleared")
		require.Empty(t, post.PinnedBy, "PinnedBy must NOT be set — strict separation")
		require.Equal(t, int64(0), post.PinnedAt, "PinnedAt must NOT be set")
	})

	t.Run("idempotent on already-permanent post", func(t *testing.T) {
		k, msgServer, ctx, _ := setupMsgServer(t)
		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: creator,
			Title:   "Permanent",
			Body:    "...",
		})
		require.NoError(t, err)

		_, err = msgServer.MakePostPermanent(ctx, &types.MsgMakePostPermanent{
			Creator: creator,
			Id:      resp.Id,
		})
		require.NoError(t, err, "MakePostPermanent on an already-permanent post must succeed (idempotent)")

		post, _ := k.GetPost(ctx, resp.Id)
		require.Equal(t, int64(0), post.ExpiresAt)
	})

	t.Run("rejects post not found", func(t *testing.T) {
		_, msgServer, ctx, _ := setupMsgServer(t)
		_, err := msgServer.MakePostPermanent(ctx, &types.MsgMakePostPermanent{
			Creator: creator,
			Id:      9999,
		})
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("rejects deleted post", func(t *testing.T) {
		k, msgServer, ctx, postId := createEphemeralPost(t)
		post, _ := k.GetPost(ctx, postId)
		post.Status = types.PostStatus_POST_STATUS_DELETED
		k.SetPost(ctx, post)

		_, err := msgServer.MakePostPermanent(ctx, &types.MsgMakePostPermanent{
			Creator: creator,
			Id:      postId,
		})
		require.ErrorIs(t, err, types.ErrPostDeleted)
	})

	t.Run("rejects hidden post", func(t *testing.T) {
		k, msgServer, ctx, postId := createEphemeralPost(t)
		post, _ := k.GetPost(ctx, postId)
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		k.SetPost(ctx, post)

		_, err := msgServer.MakePostPermanent(ctx, &types.MsgMakePostPermanent{
			Creator: creator,
			Id:      postId,
		})
		require.ErrorIs(t, err, types.ErrPostHidden)
	})

	t.Run("rejects already-expired ephemeral post", func(t *testing.T) {
		k, msgServer, ctx, postId := createEphemeralPost(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		post, _ := k.GetPost(ctx, postId)
		post.ExpiresAt = sdkCtx.BlockTime().Unix() - 1
		k.SetPost(ctx, post)

		_, err := msgServer.MakePostPermanent(ctx, &types.MsgMakePostPermanent{
			Creator: creator,
			Id:      postId,
		})
		require.ErrorIs(t, err, types.ErrPostExpired)
	})

	t.Run("promotion clears conviction_sustained flag", func(t *testing.T) {
		k, msgServer, ctx, postId := createEphemeralPost(t)
		post, _ := k.GetPost(ctx, postId)
		post.ConvictionSustained = true
		k.SetPost(ctx, post)

		_, err := msgServer.MakePostPermanent(ctx, &types.MsgMakePostPermanent{
			Creator: creator,
			Id:      postId,
		})
		require.NoError(t, err)

		post, _ = k.GetPost(ctx, postId)
		require.False(t, post.ConvictionSustained, "promotion must clear conviction_sustained")
	})
}
