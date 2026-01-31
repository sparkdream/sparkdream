package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryActiveBounties(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ActiveBounties(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no active bounties", func(t *testing.T) {
		resp, err := qs.ActiveBounties(f.ctx, &types.QueryActiveBountiesRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.BountyId)
	})

	t.Run("has active bounty", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "1000000")

		resp, err := qs.ActiveBounties(f.ctx, &types.QueryActiveBountiesRequest{})
		require.NoError(t, err)
		require.Equal(t, bounty.Id, resp.BountyId)
		require.Equal(t, post.PostId, resp.ThreadId)
		require.Equal(t, "1000000", resp.Amount)
	})

	t.Run("inactive bounty not returned", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		bounty := f.createTestBounty(t, testCreator, post.PostId, "500000")

		// Mark bounty as cancelled
		bounty.Status = types.BountyStatus_BOUNTY_STATUS_CANCELLED
		f.keeper.Bounty.Set(f.ctx, bounty.Id, bounty)

		// Should not find the cancelled bounty
		resp, err := qs.ActiveBounties(f.ctx, &types.QueryActiveBountiesRequest{})
		require.NoError(t, err)
		// Will either be 0 or an earlier active bounty
		if resp.BountyId == bounty.Id {
			t.Error("should not return cancelled bounty")
		}
	})
}
