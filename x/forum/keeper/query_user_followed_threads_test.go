package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryUserFollowedThreads(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.UserFollowedThreads(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty user address", func(t *testing.T) {
		_, err := qs.UserFollowedThreads(f.ctx, &types.QueryUserFollowedThreadsRequest{User: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("user follows no threads", func(t *testing.T) {
		resp, err := qs.UserFollowedThreads(f.ctx, &types.QueryUserFollowedThreadsRequest{User: testCreator})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.ThreadId)
	})

	t.Run("user follows threads", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Create thread follow (key format: "address:threadId")
		followKey := fmt.Sprintf("%s:%d", testCreator2, post.PostId)
		follow := types.ThreadFollow{
			ThreadId:   post.PostId,
			Follower:   testCreator2,
			FollowedAt: f.sdkCtx().BlockTime().Unix(),
		}
		f.keeper.ThreadFollow.Set(f.ctx, followKey, follow)
		require.NoError(t, f.keeper.FollowersByThread.Set(f.ctx, collections.Join(post.PostId, testCreator2)))
		require.NoError(t, f.keeper.ThreadsByFollower.Set(f.ctx, collections.Join(testCreator2, post.PostId)))

		resp, err := qs.UserFollowedThreads(f.ctx, &types.QueryUserFollowedThreadsRequest{User: testCreator2})
		require.NoError(t, err)
		require.Equal(t, post.PostId, resp.ThreadId)
	})
}
