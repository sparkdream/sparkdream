package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// TestPromotionQueueDrain exercises the EndBlocker membership-promotion
// drainer: enqueued authors have their ephemeral posts/replies promoted to
// permanent, capped at MaxPromotionsPerBlock per block.
func TestPromotionQueueDrain(t *testing.T) {
	creator := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"

	createEphemerals := func(t *testing.T, n int) (keeper.Keeper, types.MsgServer, sdk.Context, []uint64) {
		t.Helper()
		k, msgServer, ctx, _ := setupMsgServer(t)
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		ids := make([]uint64, 0, n)
		for i := 0; i < n; i++ {
			resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
				Creator: creator,
				Title:   "Ephemeral",
				Body:    "...",
			})
			require.NoError(t, err)
			post, _ := k.GetPost(ctx, resp.Id)
			post.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
			k.SetPost(ctx, post)
			k.AddToExpiryIndex(ctx, post.ExpiresAt, "post", post.Id)
			k.AddEphemeralAuthorIndex(ctx, creator, keeper.EphemeralKindPost, post.Id)
			ids = append(ids, post.Id)
		}
		return k, msgServer, ctx, ids
	}

	t.Run("drains author's ephemerals to permanent and clears queue", func(t *testing.T) {
		k, _, ctx, ids := createEphemerals(t, 3)

		k.EnqueueAuthorForPromotion(ctx, creator)
		require.True(t, k.IsAuthorQueuedForPromotion(ctx, creator))

		err := k.EndBlock(ctx)
		require.NoError(t, err)

		for _, id := range ids {
			post, found := k.GetPost(ctx, id)
			require.True(t, found)
			require.Equal(t, int64(0), post.ExpiresAt, "post %d should be promoted", id)
		}
		require.False(t, k.IsAuthorQueuedForPromotion(ctx, creator),
			"author must be dequeued after all ephemerals are drained")
	})

	t.Run("budget caps per-block work; remaining drains next block", func(t *testing.T) {
		k, _, ctx, ids := createEphemerals(t, 5)

		// Lower the per-block cap so the drain takes multiple blocks.
		params, err := k.Params.Get(ctx)
		require.NoError(t, err)
		params.MaxPromotionsPerBlock = 2
		require.NoError(t, k.Params.Set(ctx, params))

		k.EnqueueAuthorForPromotion(ctx, creator)

		// First block: 2 promoted, 3 remain ephemeral, author still queued.
		require.NoError(t, k.EndBlock(ctx))

		var permanentCount int
		for _, id := range ids {
			post, _ := k.GetPost(ctx, id)
			if post.ExpiresAt == 0 {
				permanentCount++
			}
		}
		require.Equal(t, 2, permanentCount, "first block should promote exactly MaxPromotionsPerBlock posts")
		require.True(t, k.IsAuthorQueuedForPromotion(ctx, creator),
			"author must stay queued while ephemerals remain")

		// Second block: 2 more promoted, 1 remains, still queued.
		require.NoError(t, k.EndBlock(ctx))
		permanentCount = 0
		for _, id := range ids {
			post, _ := k.GetPost(ctx, id)
			if post.ExpiresAt == 0 {
				permanentCount++
			}
		}
		require.Equal(t, 4, permanentCount)
		require.True(t, k.IsAuthorQueuedForPromotion(ctx, creator))

		// Third block: last one promoted, queue cleared.
		require.NoError(t, k.EndBlock(ctx))
		permanentCount = 0
		for _, id := range ids {
			post, _ := k.GetPost(ctx, id)
			if post.ExpiresAt == 0 {
				permanentCount++
			}
		}
		require.Equal(t, 5, permanentCount)
		require.False(t, k.IsAuthorQueuedForPromotion(ctx, creator))
	})

	t.Run("MaxPromotionsPerBlock=0 disables the drain", func(t *testing.T) {
		k, _, ctx, ids := createEphemerals(t, 2)

		params, err := k.Params.Get(ctx)
		require.NoError(t, err)
		params.MaxPromotionsPerBlock = 0
		require.NoError(t, k.Params.Set(ctx, params))

		k.EnqueueAuthorForPromotion(ctx, creator)
		require.NoError(t, k.EndBlock(ctx))

		// Author stays queued; ephemerals stay ephemeral.
		require.True(t, k.IsAuthorQueuedForPromotion(ctx, creator))
		for _, id := range ids {
			post, _ := k.GetPost(ctx, id)
			require.Greater(t, post.ExpiresAt, int64(0))
		}
	})

	t.Run("queue dequeues even if author has no ephemerals (re-invitation case)", func(t *testing.T) {
		k, _, ctx, _ := setupMsgServer(t)

		k.EnqueueAuthorForPromotion(ctx, creator)
		require.NoError(t, k.EndBlock(ctx))

		require.False(t, k.IsAuthorQueuedForPromotion(ctx, creator),
			"queue entry must be dropped when there's no ephemeral content to promote")
	})

	t.Run("does not touch posts belonging to other authors", func(t *testing.T) {
		k, msgServer, ctx, ids := createEphemerals(t, 1)
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		// Add an ephemeral post belonging to a different author.
		otherCreator := "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y"
		resp, err := msgServer.CreatePost(ctx, &types.MsgCreatePost{
			Creator: otherCreator,
			Title:   "Other ephemeral",
			Body:    "...",
		})
		require.NoError(t, err)
		other, _ := k.GetPost(ctx, resp.Id)
		other.ExpiresAt = sdkCtx.BlockTime().Unix() + 604800
		k.SetPost(ctx, other)
		k.AddToExpiryIndex(ctx, other.ExpiresAt, "post", other.Id)
		k.AddEphemeralAuthorIndex(ctx, otherCreator, keeper.EphemeralKindPost, other.Id)

		// Enqueue only `creator`. Drain.
		k.EnqueueAuthorForPromotion(ctx, creator)
		require.NoError(t, k.EndBlock(ctx))

		// creator's post promoted, other author's post untouched.
		post, _ := k.GetPost(ctx, ids[0])
		require.Equal(t, int64(0), post.ExpiresAt)
		otherPost, _ := k.GetPost(ctx, resp.Id)
		require.Greater(t, otherPost.ExpiresAt, int64(0),
			"non-queued author's ephemeral content must NOT be promoted")
	})
}

// TestBlogRepHooks_AfterMemberAdmitted verifies that the hook implementation
// enqueues the new member into the promotion queue.
func TestBlogRepHooks_AfterMemberAdmitted(t *testing.T) {
	k, _, ctx, _ := setupMsgServer(t)

	addrStr := "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan"
	addr, err := sdk.AccAddressFromBech32(addrStr)
	require.NoError(t, err)

	require.False(t, k.IsAuthorQueuedForPromotion(ctx, addrStr))

	hooks := keeper.NewBlogRepHooks(&k)
	require.NoError(t, hooks.AfterMemberAdmitted(ctx, addr))

	require.True(t, k.IsAuthorQueuedForPromotion(ctx, addrStr),
		"AfterMemberAdmitted must enqueue the new member for promotion")
}
