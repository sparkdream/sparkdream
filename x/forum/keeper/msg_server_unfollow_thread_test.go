package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

func TestMsgServerUnfollowThread(t *testing.T) {
	f := initFixture(t)

	t.Run("invalid creator address", func(t *testing.T) {
		msg := &types.MsgUnfollowThread{
			Creator:  "invalid",
			ThreadId: 1,
		}
		_, err := f.msgServer.UnfollowThread(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("not following", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		msg := &types.MsgUnfollowThread{
			Creator:  testCreator,
			ThreadId: post.PostId,
		}
		_, err := f.msgServer.UnfollowThread(f.ctx, msg)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotFollowing)
	})

	t.Run("successful unfollow", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Create follow record
		followKey := fmt.Sprintf("%s:%d", testCreator2, post.PostId)
		follow := types.ThreadFollow{
			Follower:   testCreator2,
			ThreadId:   post.PostId,
			FollowedAt: f.sdkCtx().BlockTime().Unix(),
		}
		f.keeper.ThreadFollow.Set(f.ctx, followKey, follow)

		// Create follow count
		followCount := types.ThreadFollowCount{
			ThreadId:      post.PostId,
			FollowerCount: 1,
		}
		f.keeper.ThreadFollowCount.Set(f.ctx, post.PostId, followCount)

		msg := &types.MsgUnfollowThread{
			Creator:  testCreator2,
			ThreadId: post.PostId,
		}
		_, err := f.msgServer.UnfollowThread(f.ctx, msg)
		require.NoError(t, err)

		// Verify follow record removed
		_, err = f.keeper.ThreadFollow.Get(f.ctx, followKey)
		require.Error(t, err)

		// Verify follow count decremented
		updatedCount, err := f.keeper.ThreadFollowCount.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, uint64(0), updatedCount.FollowerCount)
	})
}
