package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// setPostConvictionStake writes a stake plus both index entries the way the
// keeper does (see post_conviction.go), so the by-staker/by-post prefix walks
// resolve back to it.
func setPostConvictionStake(t *testing.T, k keeper.Keeper, ctx context.Context, id, postID uint64, staker string) types.PostConvictionStake {
	t.Helper()
	stake := types.PostConvictionStake{
		Id:        id,
		Staker:    staker,
		PostId:    postID,
		Amount:    math.NewInt(int64(1000 + id)),
		StakedAt:  int64(id),
		UnlocksAt: int64(id) + 100,
	}
	require.NoError(t, k.PostConvictionStake.Set(ctx, id, stake))
	require.NoError(t, k.PostConvictionStakesByPost.Set(ctx, collections.Join(postID, id)))
	require.NoError(t, k.PostConvictionStakesByStaker.Set(ctx, collections.Join(staker, id)))
	return stake
}

func TestQueryGetPostConvictionStake(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	want := setPostConvictionStake(t, f.keeper, f.ctx, 1, 10, testCreator)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.GetPostConvictionStake(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("hit", func(t *testing.T) {
		resp, err := qs.GetPostConvictionStake(f.ctx, &types.QueryGetPostConvictionStakeRequest{Id: 1})
		require.NoError(t, err)
		require.EqualExportedValues(t, want, resp.Stake)
	})

	t.Run("miss", func(t *testing.T) {
		_, err := qs.GetPostConvictionStake(f.ctx, &types.QueryGetPostConvictionStakeRequest{Id: 999})
		require.ErrorIs(t, err, sdkerrors.ErrKeyNotFound)
	})
}

func TestQueryPostConvictionStakesByStaker(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// testCreator stakes on posts 10 and 11; testCreator2 stakes on post 10.
	s1 := setPostConvictionStake(t, f.keeper, f.ctx, 1, 10, testCreator)
	s2 := setPostConvictionStake(t, f.keeper, f.ctx, 2, 11, testCreator)
	setPostConvictionStake(t, f.keeper, f.ctx, 3, 10, testCreator2)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.PostConvictionStakesByStaker(f.ctx, nil)
		require.Error(t, err)
		st, _ := status.FromError(err)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty staker", func(t *testing.T) {
		_, err := qs.PostConvictionStakesByStaker(f.ctx, &types.QueryPostConvictionStakesByStakerRequest{Staker: ""})
		require.Error(t, err)
		st, _ := status.FromError(err)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("returns only that staker's stakes", func(t *testing.T) {
		resp, err := qs.PostConvictionStakesByStaker(f.ctx, &types.QueryPostConvictionStakesByStakerRequest{Staker: testCreator})
		require.NoError(t, err)
		require.Len(t, resp.Stakes, 2)
		require.Subset(t, []types.PostConvictionStake{s1, s2}, resp.Stakes)
		for _, s := range resp.Stakes {
			require.Equal(t, testCreator, s.Staker)
		}
	})

	t.Run("empty result for unknown staker", func(t *testing.T) {
		resp, err := qs.PostConvictionStakesByStaker(f.ctx, &types.QueryPostConvictionStakesByStakerRequest{Staker: testSentinel})
		require.NoError(t, err)
		require.Empty(t, resp.Stakes)
	})
}

func TestQueryPostConvictionStakesByPost(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Post 10 has two stakes (different stakers); post 11 has one.
	p1 := setPostConvictionStake(t, f.keeper, f.ctx, 1, 10, testCreator)
	p2 := setPostConvictionStake(t, f.keeper, f.ctx, 2, 10, testCreator2)
	setPostConvictionStake(t, f.keeper, f.ctx, 3, 11, testCreator)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.PostConvictionStakesByPost(f.ctx, nil)
		require.Error(t, err)
		st, _ := status.FromError(err)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("returns all stakes on a post", func(t *testing.T) {
		resp, err := qs.PostConvictionStakesByPost(f.ctx, &types.QueryPostConvictionStakesByPostRequest{PostId: 10})
		require.NoError(t, err)
		require.Len(t, resp.Stakes, 2)
		require.Subset(t, []types.PostConvictionStake{p1, p2}, resp.Stakes)
		for _, s := range resp.Stakes {
			require.Equal(t, uint64(10), s.PostId)
		}
	})

	t.Run("empty result for post with no stakes", func(t *testing.T) {
		resp, err := qs.PostConvictionStakesByPost(f.ctx, &types.QueryPostConvictionStakesByPostRequest{PostId: 999})
		require.NoError(t, err)
		require.Empty(t, resp.Stakes)
	})
}
