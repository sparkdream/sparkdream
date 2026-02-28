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

	// Set up block time
	createdAt := int64(1000000)
	duration := int64(31557600) // 1 year
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	ctx := sdkCtx.WithBlockTime(time.Unix(createdAt+duration, 0))

	stake := types.Stake{
		Id:         1,
		Staker:     "staker",
		TargetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		Amount:     math.NewInt(1000000), // 1 DREAM
		CreatedAt:  createdAt,
	}

	reward, err := k.GetPendingStakingRewards(ctx, stake)
	require.NoError(t, err)
	// 10% APY for 1 year on 1,000,000 = 100,000
	require.Equal(t, math.NewInt(100000), reward)
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

	// Set time for reward calculation
	createdAt := int64(1000000)
	duration := int64(31557600) // 1 year
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	ctx := sdkCtx.WithBlockTime(time.Unix(createdAt+duration, 0))

	stake := types.Stake{
		Id:         1,
		Staker:     "staker",
		TargetType: types.StakeTargetType_STAKE_TARGET_PROJECT,
		TargetId:   projectID,
		Amount:     math.NewInt(1000000),
		CreatedAt:  createdAt,
	}

	reward, err := k.GetPendingStakingRewards(ctx, stake)
	require.NoError(t, err)
	// 8% APY (ProjectStakingApy) for 1 year on 1,000,000 = 80,000
	require.Equal(t, math.NewInt(80000), reward)
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

	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_PROJECT,
		projectID, "",
		math.NewInt(1000000),
	)
	require.NoError(t, err)

	// Advance time by 1 year
	createdStake, err := k.GetStake(f.ctx, stakeID)
	require.NoError(t, err)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	newCtx := sdkCtx.WithBlockTime(time.Unix(createdStake.CreatedAt+31557600, 0))

	rewards, err := k.ClaimStakingRewards(newCtx, stakeID, stakerAddr)
	require.NoError(t, err)
	require.True(t, rewards.IsPositive(), "should have positive rewards after 1 year")

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
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
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
