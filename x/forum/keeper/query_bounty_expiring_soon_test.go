package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryBountyExpiringSoon(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.BountyExpiringSoon(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no bounties expiring soon", func(t *testing.T) {
		resp, err := qs.BountyExpiringSoon(f.ctx, &types.QueryBountyExpiringSoonRequest{
			WithinSeconds: 3600, // 1 hour
		})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.BountyId)
	})

	t.Run("has bounty expiring soon", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		now := f.sdkCtx().BlockTime().Unix()

		// Create bounty expiring in 30 minutes
		bounty := types.Bounty{
			Id:        100,
			Creator:   testCreator,
			ThreadId:  post.PostId,
			Amount:    "1000000",
			Status:    types.BountyStatus_BOUNTY_STATUS_ACTIVE,
			ExpiresAt: now + 1800, // 30 minutes
			CreatedAt: now,
		}
		f.keeper.Bounty.Set(f.ctx, bounty.Id, bounty)
		require.NoError(t, f.keeper.BountiesByExpiry.Set(f.ctx, collections.Join(bounty.ExpiresAt, bounty.Id)))

		resp, err := qs.BountyExpiringSoon(f.ctx, &types.QueryBountyExpiringSoonRequest{
			WithinSeconds: 3600, // 1 hour
		})
		require.NoError(t, err)
		require.Equal(t, bounty.Id, resp.BountyId)
		require.Equal(t, post.PostId, resp.ThreadId)
	})

	t.Run("ignores expired bounties", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create an already-expired bounty
		expiredBounty := types.Bounty{
			Id:        200,
			Creator:   testCreator,
			ThreadId:  1,
			Amount:    "500000",
			Status:    types.BountyStatus_BOUNTY_STATUS_ACTIVE,
			ExpiresAt: now - 100, // Already expired
		}
		f.keeper.Bounty.Set(f.ctx, expiredBounty.Id, expiredBounty)

		resp, err := qs.BountyExpiringSoon(f.ctx, &types.QueryBountyExpiringSoonRequest{
			WithinSeconds: 3600,
		})
		require.NoError(t, err)
		// Should not return the expired bounty
		require.NotEqual(t, expiredBounty.Id, resp.BountyId)
	})

	t.Run("ignores non-active bounties", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create a cancelled bounty expiring soon
		cancelledBounty := types.Bounty{
			Id:        300,
			Creator:   testCreator,
			ThreadId:  1,
			Amount:    "500000",
			Status:    types.BountyStatus_BOUNTY_STATUS_CANCELLED,
			ExpiresAt: now + 1800,
		}
		f.keeper.Bounty.Set(f.ctx, cancelledBounty.Id, cancelledBounty)

		resp, err := qs.BountyExpiringSoon(f.ctx, &types.QueryBountyExpiringSoonRequest{
			WithinSeconds: 3600,
		})
		require.NoError(t, err)
		// Should not return the cancelled bounty
		require.NotEqual(t, cancelledBounty.Id, resp.BountyId)
	})
}
