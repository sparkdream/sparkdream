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

	// A live initiative to point at: stakeAccruing resolves the target and
	// stops paying once it reaches a terminal status, so the reward math can
	// no longer be exercised against a dangling target id.
	creator := newStakerMember(t, f, "pending_init_creator", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "pendinit")

	// Initialize the seasonal pool so the MasterChef accumulator is populated.
	require.NoError(t, k.InitSeasonalPool(f.ctx, 1))
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(f.ctx, stakeAmount))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	stake := types.Stake{
		Id:         1,
		Staker:     "staker",
		TargetType: types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		TargetId:   initID,
		Amount:     stakeAmount,
		CreatedAt:  1000000,
	}

	reward, err := k.GetPendingStakingRewards(f.ctx, stake)
	require.NoError(t, err)
	// Rewards come from the seasonal pool MasterChef accumulator. The epoch
	// slice is the lesser of the calendar share (budget/SeasonDurationEpochs =
	// 16,666,666) and StakingRewardYieldPerEpoch on the staked base
	// (0.0005 * 1,000,000 = 500); with 1 DREAM staked the yield cap binds.
	// When stakeAmount == totalStaked, reward == the slice.
	require.True(t, reward.IsPositive(), "expected positive reward, got %s", reward)
	require.Equal(t, math.NewInt(500), reward)
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
		false,
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
	// Project stakes share the same seasonal pool, and the same per-epoch yield
	// cap, as initiative stakes: 0.0005 * 1,000,000 staked = 500.
	// When stakeAmount == totalStaked, reward == the slice.
	require.True(t, reward.IsPositive(), "expected positive reward, got %s", reward)
	require.Equal(t, math.NewInt(500), reward)
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
		false,
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
		false,
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
		false,
	)
	require.NoError(t, err)
	k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_PROJECT,
		projectID, "", math.NewInt(1000000),
	)
	require.NoError(t, err)

	// Claiming before MinStakeDurationSeconds is rejected outright.
	_, err = k.ClaimStakingRewards(f.ctx, stakeID, stakerAddr)
	require.ErrorIs(t, err, types.ErrMinStakeDuration)

	// Past the minimum, the claim succeeds and pays nothing — the accumulator
	// never advanced, so there is genuinely nothing owed.
	rewards, err := k.ClaimStakingRewards(matureCtx(f, stakeID), stakeID, stakerAddr)
	require.NoError(t, err)
	require.True(t, rewards.IsZero())
}

// matureCtx returns a context whose block time is past the stake's minimum
// holding period, so reward claims are eligible.
func matureCtx(f *fixture, stakeID uint64) sdk.Context {
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	params, err := f.keeper.Params.Get(f.ctx)
	if err != nil {
		panic(err)
	}
	stake, err := f.keeper.GetStake(f.ctx, stakeID)
	if err != nil {
		panic(err)
	}
	return sdkCtx.WithBlockTime(time.Unix(stake.CreatedAt+params.MinStakeDurationSeconds+1, 0))
}

// TestCompoundStakingRewards_SeasonalPoolTypesRejected asserts that initiative
// and project stakes cannot compound. Their principal carries a conviction
// maturity clock keyed on created_at, so growing the amount in place would give
// the added DREAM full maturity instantly.
func TestCompoundStakingRewards_SeasonalPoolTypesRejected(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	stakerAddr := sdk.AccAddress([]byte("compound_reject_stkr"))
	require.NoError(t, k.Member.Set(f.ctx, stakerAddr.String(), types.Member{
		Address:          stakerAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"tag1": "100.0"},
	}))

	projectID, err := k.CreateProject(
		f.ctx, sdk.AccAddress([]byte("proj_cmp_reject_crt_")),
		"Project", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(1000), math.NewInt(100),
		false,
	)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100)))

	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_PROJECT,
		projectID, "", math.NewInt(1000000),
	)
	require.NoError(t, err)

	_, err = k.CompoundStakingRewards(f.ctx, stakeID, stakerAddr)
	require.ErrorIs(t, err, types.ErrCompoundNotSupported)
}

// TestCompoundStakingRewards_Success covers the member-stake path, the only
// target type family where compounding is allowed.
func TestCompoundStakingRewards_Success(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	targetAddr := sdk.AccAddress([]byte("compound_target_____"))
	require.NoError(t, k.Member.Set(f.ctx, targetAddr.String(), types.Member{
		Address:          targetAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{},
	}))

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

	originalAmount := math.NewInt(1000000)
	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_MEMBER,
		0, targetAddr.String(), originalAmount,
	)
	require.NoError(t, err)

	// Push revenue through the member pool so its accumulator is nonzero.
	_, mErr := k.AccumulateMemberStakeRevenue(f.ctx, targetAddr, math.NewInt(10000000))
	require.NoError(t, mErr)

	// Advance past the minimum holding period.
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	createdStake, err := k.GetStake(f.ctx, stakeID)
	require.NoError(t, err)
	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	newCtx := sdkCtx.WithBlockTime(time.Unix(createdStake.CreatedAt+params.MinStakeDurationSeconds+1, 0))

	poolBefore, err := k.GetMemberStakePool(newCtx, targetAddr)
	require.NoError(t, err)

	compounded, err := k.CompoundStakingRewards(newCtx, stakeID, stakerAddr)
	require.NoError(t, err)
	require.True(t, compounded.IsPositive())

	// Verify stake principal increased
	updatedStake, err := k.GetStake(newCtx, stakeID)
	require.NoError(t, err)
	require.True(t, updatedStake.Amount.GT(originalAmount))
	require.Equal(t, originalAmount.Add(compounded), updatedStake.Amount)

	// The compounded principal must dilute the pool it now earns from.
	poolAfter, err := k.GetMemberStakePool(newCtx, targetAddr)
	require.NoError(t, err)
	require.Equal(t, poolBefore.TotalStaked.Add(compounded), poolAfter.TotalStaked)

	// A second compound immediately after settles to zero — the debt was
	// rebased, so there is nothing left to harvest.
	again, err := k.CompoundStakingRewards(newCtx, stakeID, stakerAddr)
	require.NoError(t, err)
	require.True(t, again.IsZero())
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
		false,
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
		false,
	)
	require.NoError(t, err)
	k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(1000), math.NewInt(100))

	stakeID, err := k.CreateStake(
		f.ctx, stakerAddr,
		types.StakeTargetType_STAKE_TARGET_MEMBER,
		0, sdk.AccAddress([]byte("compound_zero_targt_")).String(), math.NewInt(1000000),
	)
	require.NoError(t, err)

	// No revenue has ever reached the pool, so there is nothing to compound.
	compounded, err := k.CompoundStakingRewards(matureCtx(f, stakeID), stakeID, stakerAddr)
	require.NoError(t, err)
	require.True(t, compounded.IsZero())
}
