package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryPendingStakeRewards(t *testing.T) {
	t.Run("valid request returns pending rewards", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create staker member with DREAM
		staker := sdk.AccAddress([]byte("staker"))
		k.Member.Set(ctx, staker.String(), types.Member{
			Address:          staker.String(),
			DreamBalance:     PtrInt(math.NewInt(10000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "50.0"},
		})

		// Create project and initiative
		projectID, err := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
		require.NoError(t, err)
		err = k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
		require.NoError(t, err)
		initID, err := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
		require.NoError(t, err)

		// Create stake
		stakeAmount := math.NewInt(1000)
		stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", stakeAmount)
		require.NoError(t, err)

		// Advance time by 30 days
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		thirtyDays := time.Duration(30*24) * time.Hour
		ctx = sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(thirtyDays))

		// Query pending rewards
		resp, err := qs.PendingStakeRewards(ctx, &types.QueryPendingStakeRewardsRequest{
			StakeId: stakeID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify pending rewards are positive after 30 days
		require.True(t, resp.PendingRewards.GT(math.ZeroInt()),
			"pending rewards should be positive after 30 days, got %s", resp.PendingRewards.String())

		// Verify response includes stake metadata
		require.Equal(t, stakeAmount.String(), resp.StakeAmount.String())
		require.Equal(t, types.StakeTargetType_STAKE_TARGET_INITIATIVE, resp.TargetType)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)

		_, err := qs.PendingStakeRewards(f.ctx, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "request cannot be empty")
	})
}

func TestQueryGetMemberStakePool(t *testing.T) {
	t.Run("existing pool returns pool data", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create a member stake pool directly
		member := sdk.AccAddress([]byte("member"))
		pool := types.MemberStakePool{
			Member:            member.String(),
			TotalStaked:       math.NewInt(5000),
			PendingRevenue:    math.NewInt(200),
			AccRewardPerShare: math.LegacyNewDecWithPrec(5, 2), // 0.05
			LastUpdated:       100,
		}
		err := k.MemberStakePool.Set(ctx, member.String(), pool)
		require.NoError(t, err)

		// Query
		resp, err := qs.GetMemberStakePool(ctx, &types.QueryGetMemberStakePoolRequest{
			Member: member.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, member.String(), resp.Pool.Member)
		require.Equal(t, math.NewInt(5000).String(), resp.Pool.TotalStaked.String())
		require.Equal(t, math.NewInt(200).String(), resp.Pool.PendingRevenue.String())
		require.Equal(t, int64(100), resp.Pool.LastUpdated)
	})

	t.Run("non-existing member returns empty pool", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)

		nonExistent := sdk.AccAddress([]byte("nonexistent"))

		resp, err := qs.GetMemberStakePool(f.ctx, &types.QueryGetMemberStakePoolRequest{
			Member: nonExistent.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Should return empty pool with the member field populated
		require.Equal(t, nonExistent.String(), resp.Pool.Member)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)

		_, err := qs.GetMemberStakePool(f.ctx, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "request cannot be empty")
	})
}

func TestQueryGetTagStakePool(t *testing.T) {
	t.Run("existing pool returns pool data", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create a tag stake pool directly
		tag := "golang"
		pool := types.TagStakePool{
			Tag:               tag,
			TotalStaked:       math.NewInt(8000),
			AccRewardPerShare: math.LegacyNewDecWithPrec(1, 1), // 0.1
			LastUpdated:       200,
		}
		err := k.TagStakePool.Set(ctx, tag, pool)
		require.NoError(t, err)

		// Query
		resp, err := qs.GetTagStakePool(ctx, &types.QueryGetTagStakePoolRequest{
			Tag: tag,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, tag, resp.Pool.Tag)
		require.Equal(t, math.NewInt(8000).String(), resp.Pool.TotalStaked.String())
		require.Equal(t, int64(200), resp.Pool.LastUpdated)
	})

	t.Run("non-existing tag returns empty pool", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)

		resp, err := qs.GetTagStakePool(f.ctx, &types.QueryGetTagStakePoolRequest{
			Tag: "nonexistent-tag",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Should return empty pool with the tag field populated
		require.Equal(t, "nonexistent-tag", resp.Pool.Tag)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)

		_, err := qs.GetTagStakePool(f.ctx, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "request cannot be empty")
	})
}

func TestQueryGetProjectStakeInfo(t *testing.T) {
	t.Run("existing project returns info", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project stake info directly
		projectID := uint64(42)
		info := types.ProjectStakeInfo{
			ProjectId:           projectID,
			TotalStaked:         math.NewInt(15000),
			CompletionBonusPool: math.NewInt(750),
		}
		err := k.ProjectStakeInfo.Set(ctx, projectID, info)
		require.NoError(t, err)

		// Query
		resp, err := qs.GetProjectStakeInfo(ctx, &types.QueryGetProjectStakeInfoRequest{
			ProjectId: projectID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, projectID, resp.Info.ProjectId)
		require.Equal(t, math.NewInt(15000).String(), resp.Info.TotalStaked.String())
		require.Equal(t, math.NewInt(750).String(), resp.Info.CompletionBonusPool.String())
	})

	t.Run("non-existing project returns empty info", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)

		resp, err := qs.GetProjectStakeInfo(f.ctx, &types.QueryGetProjectStakeInfoRequest{
			ProjectId: 99999,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Should return empty info with the project ID populated
		require.Equal(t, uint64(99999), resp.Info.ProjectId)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		f := initFixture(t)
		qs := keeper.NewQueryServerImpl(f.keeper)

		_, err := qs.GetProjectStakeInfo(f.ctx, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "request cannot be empty")
	})
}
