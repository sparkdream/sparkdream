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

func TestQueryThreadFollowers(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ThreadFollowers(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero root_id", func(t *testing.T) {
		_, err := qs.ThreadFollowers(f.ctx, &types.QueryThreadFollowersRequest{ThreadId: 0})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no followers", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.ThreadFollowers(f.ctx, &types.QueryThreadFollowersRequest{ThreadId: post.PostId})
		require.NoError(t, err)
		require.Empty(t, resp.Follower)
	})

	t.Run("has followers", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		// Create thread follow (key format: "address:threadId")
		followKey := fmt.Sprintf("%s:%d", testCreator2, post.PostId)
		follow := types.ThreadFollow{
			ThreadId:   post.PostId,
			Follower:   testCreator2,
			FollowedAt: f.sdkCtx().BlockTime().Unix(),
		}
		f.keeper.ThreadFollow.Set(f.ctx, followKey, follow)

		resp, err := qs.ThreadFollowers(f.ctx, &types.QueryThreadFollowersRequest{ThreadId: post.PostId})
		require.NoError(t, err)
		require.Equal(t, testCreator2, resp.Follower)
	})
}
