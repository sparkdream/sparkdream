package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryUserBounties(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.UserBounties(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty user", func(t *testing.T) {
		_, err := qs.UserBounties(f.ctx, &types.QueryUserBountiesRequest{User: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no bounties", func(t *testing.T) {
		resp, err := qs.UserBounties(f.ctx, &types.QueryUserBountiesRequest{User: testCreator})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.BountyId)
	})

	t.Run("has bounties", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		resp, err := qs.UserBounties(f.ctx, &types.QueryUserBountiesRequest{User: testCreator})
		require.NoError(t, err)
		require.Equal(t, bounty.Id, resp.BountyId)
	})
}
