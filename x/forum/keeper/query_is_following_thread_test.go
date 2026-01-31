package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryIsFollowingThread(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.IsFollowingThread(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero thread_id", func(t *testing.T) {
		_, err := qs.IsFollowingThread(f.ctx, &types.QueryIsFollowingThreadRequest{ThreadId: 0, User: testCreator})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty user", func(t *testing.T) {
		_, err := qs.IsFollowingThread(f.ctx, &types.QueryIsFollowingThreadRequest{ThreadId: 1, User: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("not following", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.IsFollowingThread(f.ctx, &types.QueryIsFollowingThreadRequest{
			ThreadId: post.PostId,
			User:     testCreator2,
		})
		require.NoError(t, err)
		require.False(t, resp.IsFollowing)
		require.Equal(t, int64(0), resp.FollowedAt)
	})

	t.Run("is following", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		now := f.sdkCtx().BlockTime().Unix()

		// Create thread follow (key format: "address:threadId")
		followKey := fmt.Sprintf("%d:%s", post.PostId, testCreator2)
		follow := types.ThreadFollow{
			ThreadId:   post.PostId,
			Follower:   testCreator2,
			FollowedAt: now,
		}
		f.keeper.ThreadFollow.Set(f.ctx, followKey, follow)

		resp, err := qs.IsFollowingThread(f.ctx, &types.QueryIsFollowingThreadRequest{
			ThreadId: post.PostId,
			User:     testCreator2,
		})
		require.NoError(t, err)
		require.True(t, resp.IsFollowing)
		require.Equal(t, now, resp.FollowedAt)
	})
}
