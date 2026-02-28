package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryContentConviction_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.ContentConviction(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}

func TestQueryContentConviction_InvalidTargetType(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Initiative is not a content conviction type
	_, err := qs.ContentConviction(f.ctx, &types.QueryContentConvictionRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_INITIATIVE),
		TargetId:   1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "content conviction type")
}

func TestQueryContentConviction_AuthorBondTypeRejected(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Author bond is not a content conviction type
	_, err := qs.ContentConviction(f.ctx, &types.QueryContentConvictionRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:   1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "content conviction type")
}

func TestQueryContentConviction_NoStakes(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.ContentConviction(f.ctx, &types.QueryContentConvictionRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT),
		TargetId:   1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.TotalConviction.IsZero())
	require.Equal(t, uint64(0), resp.StakerCount)
	require.True(t, resp.TotalStaked.IsZero())
}

func TestQueryContentConviction_WithStakes(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create a staker member
	stakerAddr := sdk.AccAddress([]byte("content_staker______"))
	stakerMember := types.Member{
		Address:          stakerAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"general": "100.0"},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, stakerMember.Address, stakerMember))

	// Create a content conviction stake
	stakeAmount := math.NewInt(200000000) // 200 DREAM
	_, err := f.keeper.CreateStake(
		f.ctx,
		stakerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		1,
		"", // no author self-stake check for test (targetIdentifier empty = no author)
		stakeAmount,
	)
	require.NoError(t, err)

	resp, err := qs.ContentConviction(f.ctx, &types.QueryContentConvictionRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT),
		TargetId:   1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, uint64(1), resp.StakerCount)
	require.Equal(t, stakeAmount, resp.TotalStaked)
	// Conviction may be zero at block time 0 due to CalculateContentConviction
	// but TotalStaked should be correct
}

func TestQueryContentConviction_AllContentTypes(t *testing.T) {
	contentTypes := []types.StakeTargetType{
		types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT,
	}

	for _, contentType := range contentTypes {
		t.Run(contentType.String(), func(t *testing.T) {
			f := initFixture(t)
			qs := keeper.NewQueryServerImpl(f.keeper)

			resp, err := qs.ContentConviction(f.ctx, &types.QueryContentConvictionRequest{
				TargetType: uint64(contentType),
				TargetId:   1,
			})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.True(t, resp.TotalConviction.IsZero())
			require.Equal(t, uint64(0), resp.StakerCount)
		})
	}
}
