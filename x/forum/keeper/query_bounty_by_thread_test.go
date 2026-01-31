package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryBountyByThread(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.BountyByThread(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero thread_id", func(t *testing.T) {
		_, err := qs.BountyByThread(f.ctx, &types.QueryBountyByThreadRequest{ThreadId: 0})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no bounty for thread", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.BountyByThread(f.ctx, &types.QueryBountyByThreadRequest{ThreadId: post.PostId})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.BountyId)
	})

	t.Run("has bounty", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		resp, err := qs.BountyByThread(f.ctx, &types.QueryBountyByThreadRequest{ThreadId: post.PostId})
		require.NoError(t, err)
		require.Equal(t, bounty.Id, resp.BountyId)
		require.Equal(t, "1000000", resp.Amount)
		require.Equal(t, uint64(types.BountyStatus_BOUNTY_STATUS_ACTIVE), resp.Status)
	})
}
