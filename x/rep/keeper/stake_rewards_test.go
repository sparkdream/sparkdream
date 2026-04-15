package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestGetPendingStakingRewards_Initiative(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stakeAmount := math.NewInt(1000000) // 1 DREAM

	// Initialize the seasonal pool so the MasterChef accumulator is populated.
	require.NoError(t, k.InitSeasonalPool(f.ctx, 1))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(f.ctx, stakeAmount))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	stake := types.Stake{
		Id:         1,
		Staker:     "staker",
		TargetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		Amount:     stakeAmount,
		CreatedAt:  1000000,
	}

	reward, err := k.GetPendingStakingRewards(f.ctx, stake)
	require.NoError(t, err)
	// Rewards come from the seasonal pool MasterChef accumulator.
	// epochSlice = MaxStakingRewardsPerSeason / SeasonDurationEpochs = 25000000000000 / 150 = 166666666666
	// accPerShare = epochSlice / totalStaked; reward = stakeAmount * accPerShare
	// When stakeAmount == totalStaked, reward == epochSlice.
	require.True(t, reward.IsPositive(), "expected positive reward, got %s", reward)
	require.Equal(t, math.NewInt(166666666666), reward)
}

func TestGetPendingStakingRewards_ContentStake(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stake := types.Stake{
		Id:         1,
		Staker:     "staker",
		TargetType: types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		Amount:     math.NewInt(1000000),
		CreatedAt:  1000000,
	}

	// Content conviction stakes earn no DREAM rewards
	reward, err := k.GetPendingStakingRewards(f.ctx, stake)
	require.NoError(t, err)
	require.True(t, reward.IsZero())
}

func TestGetPendingStakingRewards_AuthorBondStake(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	authorBondTypes := []types.StakeTargetType{
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND,
	}

	for _, bondType := range authorBondTypes {
		t.Run(bondType.String(), func(t *testing.T) {
			stake := types.Stake{
				Id:         1,
				Staker:     "staker",
				TargetType: bondType,
				Amount:     math.NewInt(1000000),
				CreatedAt:  1000000,
			}

			reward, err := k.GetPendingStakingRewards(f.ctx, stake)
			require.NoError(t, err)
			require.True(t, reward.IsZero(), "author bond stakes should earn no rewards")
		})
	}
}

func TestGetPendingStakingRewards_ContentTypes(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	contentTypes := []types.StakeTargetType{
		types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT,
	}

	for _, contentType := range contentTypes {
		t.Run(contentType.String(), func(t *testing.T) {
			stake := types.Stake{
				Id:         1,
				Staker:     "staker",
				TargetType: contentType,
				Amount:     math.NewInt(1000000),
				CreatedAt:  1000000,
			}

			reward, err := k.GetPendingStakingRewards(f.ctx, stake)
			require.NoError(t, err)
			require.True(t, reward.IsZero(), "content stakes should earn no rewards")
		})
	}
}

func TestGetPendingStakingRewards_Project(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	// Create an active project
	projectID, err := k.CreateProject(
		f.ctx,
		sdk.AccAddress([]byte("proj_rewards_creator")),
		"Rewards Project",
		"Description",
		[]string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(1000),
		math.NewInt(100),
	)
	require.NoError(t, err)

	approver := sdk.AccAddress([]byte("approver"))
	err = k.ApproveProject(f.ctx, projectID, approver, math.NewInt(1000), math.NewInt(100))
	require.NoError(t, err)

	stakeAmount := math.NewInt(1000000)

	// Initialize the seasonal pool so the MasterChef accumulator is populated.
	require.NoError(t, k.InitSeasonalPool(f.ctx, 1))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(f.ctx, stakeAmount))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	stake := types.Stake{
		Id:         1,
		Staker:     "staker",
		TargetType: types.StakeTargetType_STAKE_TARGET_PROJECT,
		TargetId:   projectID,
		Amount:     stakeAmount,
		CreatedAt:  1000000,
	}

	reward, err := k.GetPendingStakingRewards(f.ctx, stake)
	require.NoError(t, err)
	// Project stakes share the same seasonal pool as initiative stakes.
	// epochSlice = MaxStakingRewardsPerSeason / SeasonDurationEpochs = 25000000000000 / 150 = 166666666666
	// When stakeAmount == totalStaked, reward == epochSlice.
	require.True(t, reward.IsPositive(), "expected positive reward, got %s", reward)
	require.Equal(t, math.NewInt(166666666666), reward)
}

func TestClaimStakingRewards_Success(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stakerAddr := sdk.AccAddress([]byte("claim_staker________"))
	stakerMember := types.Member{
		Address:          stakerAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"tag1": "100.0"},
	}
	require.NoError(t, k.Member.Set(f.ctx, stakerMember.Address, stakerMember))

	// Create project and stake
	projectID, err := k.CreateProject(
		f.ctx,
		sdk.AccAddress([]byte("proj_claim_creator__")),
		"Claim Project",
		"Description",
		[]string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(1000),
		math.NewInt(100),
	)
	require.NoError(t, err)
	k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	stakeAmount := math.NewInt(1000000)
	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_PROJECT,
		projectID, "",
		stakeAmount,
	)
	require.NoError(t, err)

	// Initialize the seasonal pool and distribute rewards so accPerShare > 0.
	require.NoError(t, k.InitSeasonalPool(f.ctx, 1))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(f.ctx, stakeAmount))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	// Advance time so LastClaimedAt reflects forward progress
	createdStake, err := k.GetStake(f.ctx, stakeID)
	require.NoError(t, err)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	newCtx := sdkCtx.WithBlockTime(time.Unix(createdStake.CreatedAt+31557600, 0))

	rewards, err := k.ClaimStakingRewards(newCtx, stakeID, stakerAddr)
	require.NoError(t, err)
	require.True(t, rewards.IsPositive(), "should have positive rewards after pool distribution")

	// Verify stake's LastClaimedAt was updated
	updatedStake, err := k.GetStake(newCtx, stakeID)
	require.NoError(t, err)
	require.Equal(t, time.Unix(createdStake.CreatedAt+31557600, 0).Unix(), updatedStake.LastClaimedAt)
}

func TestClaimStakingRewards_NotStakeOwner(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stakerAddr := sdk.AccAddress([]byte("claim_owner_staker__"))
	otherAddr := sdk.AccAddress([]byte("claim_owner_other___"))

	stakerMember := types.Member{
		Address:          stakerAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"tag1": "100.0"},
	}
	require.NoError(t, k.Member.Set(f.ctx, stakerMember.Address, stakerMember))

	projectID, err := k.CreateProject(
		f.ctx, sdk.AccAddress([]byte("proj_notowner_creat_")),
		"Project", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1000), math.NewInt(100),
	)
	require.NoError(t, err)
	k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_PROJECT,
		projectID, "", math.NewInt(1000000),
	)
	require.NoError(t, err)

	// Non-owner tries to claim
	_, err = k.ClaimStakingRewards(f.ctx, stakeID, otherAddr)
	require.Error(t, err)
	require.Contains(t, err.Error(), "only stake owner can claim rewards")
}

func TestClaimStakingRewards_StakeNotFound(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stakerAddr := sdk.AccAddress([]byte("claim_notfound______"))

	_, err := k.ClaimStakingRewards(f.ctx, 999, stakerAddr)
	require.Error(t, err)
}

func TestClaimStakingRewards_ZeroRewards(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stakerAddr := sdk.AccAddress([]byte("claim_zero_staker___"))
	stakerMember := types.Member{
		Address:          stakerAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"tag1": "100.0"},
	}
	require.NoError(t, k.Member.Set(f.ctx, stakerMember.Address, stakerMember))

	projectID, err := k.CreateProject(
		f.ctx, sdk.AccAddress([]byte("proj_zero_creator___")),
		"Project", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1000), math.NewInt(100),
	)
	require.NoError(t, err)
	k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_PROJECT,
		projectID, "", math.NewInt(1000000),
	)
	require.NoError(t, err)

	// Claim immediately (zero time elapsed -> zero rewards)
	rewards, err := k.ClaimStakingRewards(f.ctx, stakeID, stakerAddr)
	require.NoError(t, err)
	require.True(t, rewards.IsZero())
}

func TestCompoundStakingRewards_Success(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stakerAddr := sdk.AccAddress([]byte("compound_staker_____"))
	stakerMember := types.Member{
		Address:          stakerAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(500000000000000)), // 500,000 DREAM — large enough for epoch reward compounding
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"tag1": "100.0"},
	}
	require.NoError(t, k.Member.Set(f.ctx, stakerMember.Address, stakerMember))

	projectID, err := k.CreateProject(
		f.ctx, sdk.AccAddress([]byte("proj_compound_creat_")),
		"Compound Project", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1000), math.NewInt(100),
	)
	require.NoError(t, err)
	k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	originalAmount := math.NewInt(1000000)
	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_PROJECT,
		projectID, "", originalAmount,
	)
	require.NoError(t, err)

	// Initialize the seasonal pool and distribute rewards so accPerShare > 0.
	require.NoError(t, k.InitSeasonalPool(f.ctx, 1))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(f.ctx, originalAmount))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	// Advance time by 1 year
	createdStake, err := k.GetStake(f.ctx, stakeID)
	require.NoError(t, err)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	newCtx := sdkCtx.WithBlockTime(time.Unix(createdStake.CreatedAt+31557600, 0))

	compounded, err := k.CompoundStakingRewards(newCtx, stakeID, stakerAddr)
	require.NoError(t, err)
	require.True(t, compounded.IsPositive())

	// Verify stake principal increased
	updatedStake, err := k.GetStake(newCtx, stakeID)
	require.NoError(t, err)
	require.True(t, updatedStake.Amount.GT(originalAmount))
	require.Equal(t, originalAmount.Add(compounded), updatedStake.Amount)
}

func TestCompoundStakingRewards_NotStakeOwner(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stakerAddr := sdk.AccAddress([]byte("compound_own_staker_"))
	otherAddr := sdk.AccAddress([]byte("compound_own_other__"))

	stakerMember := types.Member{
		Address:          stakerAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"tag1": "100.0"},
	}
	require.NoError(t, k.Member.Set(f.ctx, stakerMember.Address, stakerMember))

	projectID, err := k.CreateProject(
		f.ctx, sdk.AccAddress([]byte("proj_cmpnotowncrt___")),
		"Project", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1000), math.NewInt(100),
	)
	require.NoError(t, err)
	k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_PROJECT,
		projectID, "", math.NewInt(1000000),
	)
	require.NoError(t, err)

	// Non-owner tries to compound
	_, err = k.CompoundStakingRewards(f.ctx, stakeID, otherAddr)
	require.Error(t, err)
	require.Contains(t, err.Error(), "only stake owner can compound rewards")
}

func TestCompoundStakingRewards_ZeroRewards(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stakerAddr := sdk.AccAddress([]byte("compound_zero_stkr__"))
	stakerMember := types.Member{
		Address:          stakerAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"tag1": "100.0"},
	}
	require.NoError(t, k.Member.Set(f.ctx, stakerMember.Address, stakerMember))

	projectID, err := k.CreateProject(
		f.ctx, sdk.AccAddress([]byte("proj_cmpzero_creat__")),
		"Project", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1000), math.NewInt(100),
	)
	require.NoError(t, err)
	k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_PROJECT,
		projectID, "", math.NewInt(1000000),
	)
	require.NoError(t, err)

	// Compound immediately (zero time -> zero rewards)
	compounded, err := k.CompoundStakingRewards(f.ctx, stakeID, stakerAddr)
	require.NoError(t, err)
	require.True(t, compounded.IsZero())
}
