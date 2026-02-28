package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryAuthorBond_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.AuthorBond(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}

func TestQueryAuthorBond_InvalidTargetType(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Initiative is not an author bond type
	_, err := qs.AuthorBond(f.ctx, &types.QueryAuthorBondRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_INITIATIVE),
		TargetId:   1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "author bond type")
}

func TestQueryAuthorBond_NotFound(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.AuthorBond(f.ctx, &types.QueryAuthorBondRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:   999,
	})
	// Query returns zero bond (not error) when not found
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.BondAmount.IsZero())
	require.Equal(t, "", resp.Author)
	require.Equal(t, uint64(0), resp.StakeId)
}

func TestQueryAuthorBond_Found(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	bondAmount := math.NewInt(500000000)
	stakeID, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		bondAmount,
	)
	require.NoError(t, err)

	resp, err := qs.AuthorBond(f.ctx, &types.QueryAuthorBondRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:   1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, bondAmount, resp.BondAmount)
	require.Equal(t, authorAddr.String(), resp.Author)
	require.Equal(t, stakeID, resp.StakeId)
}

func TestQueryAuthorBond_ForumType(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	authorAddr := sdk.AccAddress([]byte("forum_bond_author___"))
	member := types.Member{
		Address:          authorAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, member.Address, member))

	bondAmount := math.NewInt(300000000)
	_, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
		42,
		bondAmount,
	)
	require.NoError(t, err)

	resp, err := qs.AuthorBond(f.ctx, &types.QueryAuthorBondRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND),
		TargetId:   42,
	})
	require.NoError(t, err)
	require.Equal(t, bondAmount, resp.BondAmount)
	require.Equal(t, authorAddr.String(), resp.Author)
}
