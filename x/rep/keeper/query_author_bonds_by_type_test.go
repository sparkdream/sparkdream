package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// createIndexedStake stores a stake AND adds it to the target index, which
// AuthorBondsByType paginates over (unlike StakesByTarget, which walks the
// Stake collection directly).
func createIndexedStake(t *testing.T, k keeper.Keeper, ctx context.Context, id uint64, staker string, targetType types.StakeTargetType, targetID uint64, amount int64) types.Stake {
	t.Helper()
	stake := types.Stake{
		Id:         id,
		Staker:     staker,
		TargetType: targetType,
		TargetId:   targetID,
		Amount:     math.NewInt(amount),
		CreatedAt:  int64(id * 1000),
		RewardDebt: math.ZeroInt(),
	}
	require.NoError(t, k.Stake.Set(ctx, id, stake))
	require.NoError(t, k.StakeSeq.Set(ctx, id))
	require.NoError(t, k.AddStakeToTargetIndex(ctx, stake))
	return stake
}

func TestAuthorBondsByType(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Blog author bonds on three different posts.
	createIndexedStake(t, f.keeper, f.ctx, 1, "author1", types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 10, 1_000_000)
	createIndexedStake(t, f.keeper, f.ctx, 2, "author2", types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 20, 5_000_000)
	createIndexedStake(t, f.keeper, f.ctx, 3, "author3", types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 30, 2_000_000)
	// A forum bond and a content conviction stake must NOT appear in blog results.
	createIndexedStake(t, f.keeper, f.ctx, 4, "author4", types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, 10, 3_000_000)
	createIndexedStake(t, f.keeper, f.ctx, 5, "fan1", types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, 10, 7_000_000)

	resp, err := qs.AuthorBondsByType(f.ctx, &types.QueryAuthorBondsByTypeRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
	})
	require.NoError(t, err)
	require.Len(t, resp.Bonds, 3)
	for _, bond := range resp.Bonds {
		require.Equal(t, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, bond.TargetType)
	}
	// Index iterates by (target_type, target_id, stake_id): post 10, 20, 30.
	require.Equal(t, uint64(10), resp.Bonds[0].TargetId)
	require.Equal(t, "author1", resp.Bonds[0].Staker)
	require.Equal(t, "1000000", resp.Bonds[0].Amount.String())
	require.Equal(t, uint64(20), resp.Bonds[1].TargetId)
	require.Equal(t, uint64(30), resp.Bonds[2].TargetId)

	// Forum bond type returns only the forum bond.
	resp, err = qs.AuthorBondsByType(f.ctx, &types.QueryAuthorBondsByTypeRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND),
	})
	require.NoError(t, err)
	require.Len(t, resp.Bonds, 1)
	require.Equal(t, "author4", resp.Bonds[0].Staker)
}

func TestAuthorBondsByType_Pagination(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	for i := uint64(1); i <= 5; i++ {
		createIndexedStake(t, f.keeper, f.ctx, i, "author", types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, i*10, int64(i)*1_000_000)
	}

	// First page of 2.
	resp, err := qs.AuthorBondsByType(f.ctx, &types.QueryAuthorBondsByTypeRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		Pagination: &query.PageRequest{Limit: 2, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, resp.Bonds, 2)
	require.Equal(t, uint64(5), resp.Pagination.Total)
	require.NotNil(t, resp.Pagination.NextKey)
	require.Equal(t, uint64(10), resp.Bonds[0].TargetId)
	require.Equal(t, uint64(20), resp.Bonds[1].TargetId)

	// Second page via next_key.
	resp, err = qs.AuthorBondsByType(f.ctx, &types.QueryAuthorBondsByTypeRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		Pagination: &query.PageRequest{Limit: 2, Key: resp.Pagination.NextKey},
	})
	require.NoError(t, err)
	require.Len(t, resp.Bonds, 2)
	require.Equal(t, uint64(30), resp.Bonds[0].TargetId)
	require.Equal(t, uint64(40), resp.Bonds[1].TargetId)
}

func TestAuthorBondsByType_EmptyAndErrors(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// No bonds at all: empty result, no error.
	resp, err := qs.AuthorBondsByType(f.ctx, &types.QueryAuthorBondsByTypeRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND),
	})
	require.NoError(t, err)
	require.Empty(t, resp.Bonds)

	// Nil request.
	_, err = qs.AuthorBondsByType(f.ctx, nil)
	require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))

	// Non-bond target types are rejected.
	for _, tt := range []types.StakeTargetType{
		types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		types.StakeTargetType_STAKE_TARGET_MEMBER,
		types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
	} {
		_, err = qs.AuthorBondsByType(f.ctx, &types.QueryAuthorBondsByTypeRequest{TargetType: uint64(tt)})
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
}

func TestAuthorBondsByType_SkipsStaleIndexEntries(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	createIndexedStake(t, f.keeper, f.ctx, 1, "author1", types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 10, 1_000_000)
	stale := createIndexedStake(t, f.keeper, f.ctx, 2, "author2", types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 20, 2_000_000)
	// Remove the stake record but leave the index entry behind.
	require.NoError(t, f.keeper.Stake.Remove(f.ctx, stale.Id))

	resp, err := qs.AuthorBondsByType(f.ctx, &types.QueryAuthorBondsByTypeRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
	})
	require.NoError(t, err)
	require.Len(t, resp.Bonds, 1)
	require.Equal(t, "author1", resp.Bonds[0].Staker)
}
