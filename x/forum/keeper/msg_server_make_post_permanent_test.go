package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

// TestMsgServerMakePostPermanent covers the lifecycle-promotion message.
// Forum's Post unifies root posts and replies, so the same Msg works on
// either kind of target. Pin markers are not touched.
func TestMsgServerMakePostPermanent(t *testing.T) {
	// createEphemeral pre-seeds the EphemeralByAuthor and ExpirationQueue
	// entries so the cleanup paths exercised by MakePostPermanent have
	// something to remove.
	createEphemeral := func(t *testing.T, f *fixture, parentId uint64) types.Post {
		t.Helper()
		post := f.createTestPost(t, testCreator, parentId, 0)
		post.ExpirationTime = f.sdkCtx().BlockTime().Unix() + 604800
		require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))
		require.NoError(t, f.keeper.ExpirationQueue.Set(f.ctx, collections.Join(post.ExpirationTime, post.PostId)))
		require.NoError(t, f.keeper.AddEphemeralAuthorIndex(f.ctx, testCreator, post.PostId))
		return post
	}

	t.Run("successful promotion of ephemeral root post", func(t *testing.T) {
		f := initFixture(t)
		post := createEphemeral(t, f, 0)

		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.NoError(t, err)

		got, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, int64(0), got.ExpirationTime, "ExpirationTime must be cleared")
		require.False(t, got.Pinned, "Pin markers must NOT be touched — strict separation")
		require.Empty(t, got.PinnedBy)

		// ExpirationQueue + EphemeralByAuthor entries scrubbed.
		hasQ, err := f.keeper.ExpirationQueue.Has(f.ctx, collections.Join(post.ExpirationTime, post.PostId))
		require.NoError(t, err)
		require.False(t, hasQ)
		hasE, err := f.keeper.EphemeralByAuthor.Has(f.ctx, collections.Join(testCreator, post.PostId))
		require.NoError(t, err)
		require.False(t, hasE)
	})

	t.Run("works on ephemeral reply (forum unifies posts and replies)", func(t *testing.T) {
		f := initFixture(t)
		root := f.createTestPost(t, testCreator, 0, 0)
		reply := createEphemeral(t, f, root.PostId)

		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  reply.PostId,
		})
		require.NoError(t, err)

		got, err := f.keeper.Post.Get(f.ctx, reply.PostId)
		require.NoError(t, err)
		require.Equal(t, int64(0), got.ExpirationTime)
	})

	t.Run("idempotent on already-permanent post", func(t *testing.T) {
		f := initFixture(t)
		post := f.createTestPost(t, testCreator, 0, 0) // permanent by default

		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.NoError(t, err, "must succeed (idempotent) on already-permanent post")

		got, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, int64(0), got.ExpirationTime)
	})

	t.Run("clears conviction_sustained flag on promotion", func(t *testing.T) {
		f := initFixture(t)
		post := createEphemeral(t, f, 0)
		post.ConvictionSustained = true
		require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))

		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.NoError(t, err)

		got, _ := f.keeper.Post.Get(f.ctx, post.PostId)
		require.False(t, got.ConvictionSustained, "promotion must clear conviction_sustained")
	})

	t.Run("rejects post not found", func(t *testing.T) {
		f := initFixture(t)
		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  99999,
		})
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("rejects deleted post", func(t *testing.T) {
		f := initFixture(t)
		post := createEphemeral(t, f, 0)
		post.Status = types.PostStatus_POST_STATUS_DELETED
		require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))

		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.ErrorIs(t, err, types.ErrPostDeleted)
	})

	t.Run("rejects hidden post", func(t *testing.T) {
		f := initFixture(t)
		post := createEphemeral(t, f, 0)
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		require.NoError(t, f.keeper.Post.Set(f.ctx, post.PostId, post))

		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.ErrorIs(t, err, types.ErrPostAlreadyHidden)
	})

	t.Run("rejects already-expired ephemeral post", func(t *testing.T) {
		f := initFixture(t)
		post := createEphemeral(t, f, 0)
		// Move block time forward past the post's expiration.
		f.ctx = f.sdkCtx().WithBlockTime(time.Unix(post.ExpirationTime+10, 0))

		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.ErrorIs(t, err, types.ErrPostExpired)
	})

	t.Run("rejects invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: "not-a-bech32",
			PostId:  1,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("post is no-longer-pinned by MakePermanent (strict separation)", func(t *testing.T) {
		f := initFixture(t)
		post := createEphemeral(t, f, 0)

		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.NoError(t, err)

		got, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.False(t, got.Pinned)
		require.Empty(t, got.PinnedBy)
		require.Equal(t, int64(0), got.PinnedAt)

		// Sanity: now that the post is permanent, a gov-pin should succeed.
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		_, err = f.msgServer.PinPost(f.ctx, &types.MsgPinPost{
			Creator:  authority,
			PostId:   post.PostId,
			Priority: 1,
		})
		require.NoError(t, err)
		got, _ = f.keeper.Post.Get(f.ctx, post.PostId)
		require.True(t, got.Pinned)
	})

	// Make-permanent is gated on trust level. A caller whose trust is below
	// params.MakePermanentMinTrustLevel must be rejected. The mock RepKeeper
	// reports ESTABLISHED by default (well above the PROVISIONAL=1 default),
	// so we force a lower level via the per-call closure.
	t.Run("rejects insufficient trust level", func(t *testing.T) {
		f := initFixture(t)
		// Drive trust below PROVISIONAL (1) — the default gate.
		f.repKeeper.GetTrustLevelFn = func(_ sdk.AccAddress) uint64 { return 0 }
		post := createEphemeral(t, f, 0)

		_, err := f.msgServer.MakePostPermanent(f.ctx, &types.MsgMakePostPermanent{
			Creator: testCreator,
			PostId:  post.PostId,
		})
		require.ErrorIs(t, err, types.ErrInsufficientTrustLevel)
	})
}
