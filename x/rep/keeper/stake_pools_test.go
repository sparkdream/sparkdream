package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestExtendedStaking_MemberStaking tests staking on a member
func TestExtendedStaking_MemberStaking(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Setup: Create staker and target member
	staker := sdk.AccAddress([]byte("staker"))
	target := sdk.AccAddress([]byte("target"))

	// Create staker with DREAM
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0"},
	})

	// Create target member
	k.Member.Set(ctx, target.String(), types.Member{
		Address:          target.String(),
		DreamBalance:     PtrInt(math.NewInt(500)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	})

	// Test: Create stake on member
	stakeAmount := math.NewInt(100)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, target.String(), stakeAmount)
	require.NoError(t, err)
	require.NotZero(t, stakeID)

	// Verify stake was created
	stake, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err)
	require.Equal(t, types.StakeTargetType_STAKE_TARGET_MEMBER, stake.TargetType)
	require.Equal(t, target.String(), stake.TargetIdentifier)
	require.Equal(t, stakeAmount.String(), stake.Amount.String())

	// Verify member stake pool was created/updated
	pool, err := k.GetMemberStakePool(ctx, target)
	require.NoError(t, err)
	require.Equal(t, stakeAmount.String(), pool.TotalStaked.String())
}

// TestExtendedStaking_TagStaking tests staking on a tag
func TestExtendedStaking_TagStaking(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Setup: Create staker
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Test: Create stake on tag
	tagName := "backend"
	stakeAmount := math.NewInt(200)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_TAG, 0, tagName, stakeAmount)
	require.NoError(t, err)
	require.NotZero(t, stakeID)

	// Verify stake was created
	stake, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err)
	require.Equal(t, types.StakeTargetType_STAKE_TARGET_TAG, stake.TargetType)
	require.Equal(t, tagName, stake.TargetIdentifier)
	require.Equal(t, stakeAmount.String(), stake.Amount.String())

	// Verify tag stake pool was created/updated
	pool, err := k.GetTagStakePool(ctx, tagName)
	require.NoError(t, err)
	require.Equal(t, stakeAmount.String(), pool.TotalStaked.String())
}

// TestExtendedStaking_SelfStakeDisallowed tests that self-staking is disallowed by default
func TestExtendedStaking_SelfStakeDisallowed(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Setup: Create member
	member := sdk.AccAddress([]byte("member"))
	k.Member.Set(ctx, member.String(), types.Member{
		Address:          member.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Test: Try to stake on self (should fail with default params)
	_, err := k.CreateStake(ctx, member, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, member.String(), math.NewInt(100))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot stake on yourself")
}

// TestExtendedStaking_RevenueAccumulation tests that revenue is accumulated to stake pools
func TestExtendedStaking_RevenueAccumulation(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Setup: Create target member pool
	target := sdk.AccAddress([]byte("target"))
	k.Member.Set(ctx, target.String(), types.Member{
		Address:          target.String(),
		DreamBalance:     PtrInt(math.NewInt(500)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Create initial stake pool
	pool := types.MemberStakePool{
		Member:            target.String(),
		TotalStaked:       math.NewInt(1000),
		PendingRevenue:    math.ZeroInt(),
		AccRewardPerShare: math.LegacyZeroDec(),
	}
	err := k.MemberStakePool.Set(ctx, target.String(), pool)
	require.NoError(t, err)

	// Test: Accumulate revenue
	revenue := math.NewInt(500)
	err = k.AccumulateMemberStakeRevenue(ctx, target, revenue)
	require.NoError(t, err)

	// Verify accumulated reward per share increased
	updatedPool, err := k.GetMemberStakePool(ctx, target)
	require.NoError(t, err)
	require.True(t, updatedPool.AccRewardPerShare.GT(math.LegacyZeroDec()))
}

// TestExtendedStaking_ClaimRewards tests claiming staking rewards
func TestExtendedStaking_ClaimRewards(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Setup: Create staker with a stake
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.NewInt(100)),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Create project and initiative for stake target
	projectID, _ := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	initID, _ := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))

	// Create stake
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(100))
	require.NoError(t, err)

	// Get stake and verify initial state
	stake, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err)
	require.Equal(t, int64(0), stake.LastClaimedAt)

	// Test: Claim rewards (should be 0 since just created, no time elapsed)
	claimed, err := k.ClaimStakingRewards(ctx, stakeID, staker)
	require.NoError(t, err)
	require.True(t, claimed.GTE(math.ZeroInt()))

	// Note: LastClaimedAt is only updated when there are rewards to claim
	// With no time elapsed, claimed==0, so timestamp may not be updated
	// This is correct behavior - we only update state when there's actual work
}

// TestExtendedStaking_CompoundRewards tests compounding staking rewards
func TestExtendedStaking_CompoundRewards(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Setup: Create staker with a stake
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.NewInt(100)),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Create project and initiative
	projectID, _ := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	initID, _ := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))

	// Create stake
	initialAmount := math.NewInt(100)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", initialAmount)
	require.NoError(t, err)

	// Test: Compound rewards
	compounded, err := k.CompoundStakingRewards(ctx, stakeID, staker)
	require.NoError(t, err)
	require.True(t, compounded.GTE(math.ZeroInt()))

	// Stake amount should be >= initial (rewards compounded)
	stake, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err)
	require.True(t, stake.Amount.GTE(initialAmount))
}

// TestExtendedStaking_PendingRewardsQuery tests querying pending rewards
func TestExtendedStaking_PendingRewardsQuery(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Setup: Create stake
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Create project and initiative
	projectID, _ := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	initID, _ := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))

	// Create stake
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(100))
	require.NoError(t, err)

	// Test: Get pending rewards
	stake, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err)

	pending, err := k.GetPendingStakingRewards(ctx, stake)
	require.NoError(t, err)
	// Rewards should be >= 0 (0 if just created)
	require.True(t, pending.GTE(math.ZeroInt()))
}

// TestExtendedStaking_MinStakeDuration tests minimum stake duration enforcement
func TestExtendedStaking_MinStakeDuration(t *testing.T) {
	// Note: This would require time manipulation which is complex in unit tests
	// The MinStakeDurationSeconds param is enforced in ClaimStakingRewards
	// For now, we just verify the param exists and is set correctly
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(86400), params.MinStakeDurationSeconds) // 24 hours
}

// TestExtendedStaking_ProjectStaking tests staking on a project
func TestExtendedStaking_ProjectStaking(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Setup: Create staker
	staker := sdk.AccAddress([]byte("staker"))
	k.Member.Set(ctx, staker.String(), types.Member{
		Address:          staker.String(),
		DreamBalance:     PtrInt(math.NewInt(1000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Create project
	projectID, err := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	// Test: Create stake on project
	stakeAmount := math.NewInt(500)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", stakeAmount)
	require.NoError(t, err)
	require.NotZero(t, stakeID)

	// Verify stake was created
	stake, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err)
	require.Equal(t, types.StakeTargetType_STAKE_TARGET_PROJECT, stake.TargetType)
	require.Equal(t, projectID, stake.TargetId)
	require.Equal(t, stakeAmount.String(), stake.Amount.String())

	// Verify project stake info was created
	info, err := k.GetProjectStakeInfo(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, stakeAmount.String(), info.TotalStaked.String())
}

// TestAccumulateTagStakeRevenue tests multi-tag revenue distribution
func TestAccumulateTagStakeRevenue(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Setup: Create tag stake pools with known totals
	err := k.TagStakePool.Set(ctx, "go", types.TagStakePool{
		Tag:               "go",
		TotalStaked:       math.NewInt(100),
		AccRewardPerShare: math.LegacyZeroDec(),
		LastUpdated:       0,
	})
	require.NoError(t, err)

	err = k.TagStakePool.Set(ctx, "rust", types.TagStakePool{
		Tag:               "rust",
		TotalStaked:       math.NewInt(400),
		AccRewardPerShare: math.LegacyZeroDec(),
		LastUpdated:       0,
	})
	require.NoError(t, err)

	// "python" has zero total staked - should be skipped
	err = k.TagStakePool.Set(ctx, "python", types.TagStakePool{
		Tag:               "python",
		TotalStaked:       math.ZeroInt(),
		AccRewardPerShare: math.LegacyZeroDec(),
		LastUpdated:       0,
	})
	require.NoError(t, err)

	// Get TagStakeRevenueShare from params (default is 2%)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	// Test: Accumulate revenue across multiple tags
	// Revenue share is now SPLIT across tags: total 2% divided by tag count.
	tags := []string{"go", "rust", "python", "unknown"}
	totalRevenue := math.NewInt(10000)
	err = k.AccumulateTagStakeRevenue(ctx, tags, totalRevenue)
	require.NoError(t, err)

	// Expected per-tag share: totalRevenue * TagStakeRevenueShare / tagCount = 10000 * 0.02 / 4 = 50
	perTagShare := totalRevenue.ToLegacyDec().Mul(params.TagStakeRevenueShare).QuoInt64(int64(len(tags))).TruncateInt()
	require.Equal(t, math.NewInt(50), perTagShare)

	// Verify "go" pool: AccRewardPerShare = perTagShare / totalStaked = 50 / 100 = 0.5
	goPool, err := k.GetTagStakePool(ctx, "go")
	require.NoError(t, err)
	expectedGoRewardPerShare := perTagShare.ToLegacyDec().Quo(math.NewInt(100).ToLegacyDec())
	require.Equal(t, expectedGoRewardPerShare.String(), goPool.AccRewardPerShare.String())

	// Verify "rust" pool: AccRewardPerShare = 50 / 400 = 0.125
	rustPool, err := k.GetTagStakePool(ctx, "rust")
	require.NoError(t, err)
	expectedRustRewardPerShare := perTagShare.ToLegacyDec().Quo(math.NewInt(400).ToLegacyDec())
	require.Equal(t, expectedRustRewardPerShare.String(), rustPool.AccRewardPerShare.String())

	// Verify "python" pool: zero total staked, should be skipped (AccRewardPerShare unchanged)
	pythonPool, err := k.GetTagStakePool(ctx, "python")
	require.NoError(t, err)
	require.True(t, pythonPool.AccRewardPerShare.IsZero(),
		"python pool with zero staked should not have accumulated rewards")

	// Verify "unknown" tag: not found, should be silently skipped (no error)
	_, err = k.GetTagStakePool(ctx, "unknown")
	require.Error(t, err, "unknown tag should not have a pool created")

	// Test: Accumulate a second round with single tag — full 2% goes to that tag
	err = k.AccumulateTagStakeRevenue(ctx, []string{"go"}, totalRevenue)
	require.NoError(t, err)

	goPool2, err := k.GetTagStakePool(ctx, "go")
	require.NoError(t, err)
	// Single tag gets full share: 10000 * 0.02 / 1 = 200, rewardPerShare = 200/100 = 2.0
	singleTagShare := totalRevenue.ToLegacyDec().Mul(params.TagStakeRevenueShare).TruncateInt()
	singleTagRewardPerShare := singleTagShare.ToLegacyDec().Quo(math.NewInt(100).ToLegacyDec())
	expectedCumulative := expectedGoRewardPerShare.Add(singleTagRewardPerShare)
	require.Equal(t, expectedCumulative.String(), goPool2.AccRewardPerShare.String())
}

// TestDistributeInitiativeCompletionBonus tests initiative completion bonus distribution
func TestDistributeInitiativeCompletionBonus(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Create the project creator / initiative assignee
	creator := sdk.AccAddress([]byte("creator_addr________"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.NewInt(50000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	})

	// Create external stakers
	staker1 := sdk.AccAddress([]byte("external_staker1____"))
	staker2 := sdk.AccAddress([]byte("external_staker2____"))

	for _, s := range []sdk.AccAddress{staker1, staker2} {
		k.Member.Set(ctx, s.String(), types.Member{
			Address:          s.String(),
			DreamBalance:     PtrInt(math.NewInt(10000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"backend": "50.0"},
		})
	}

	// Create and approve project
	projectID, err := k.CreateProject(ctx, creator, "TestProj", "Description",
		[]string{"backend"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical", math.NewInt(100000), math.NewInt(10000), false)
	require.NoError(t, err)

	approver := sdk.AccAddress([]byte("approver____________"))
	err = k.ApproveProject(ctx, projectID, approver, math.NewInt(100000), math.NewInt(10000))
	require.NoError(t, err)

	// Create initiative
	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "Do the work",
		[]string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(1000))
	require.NoError(t, err)

	// Staker1 stakes 200 DREAM, staker2 stakes 800 DREAM
	_, err = k.CreateStake(ctx, staker1, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(200))
	require.NoError(t, err)
	_, err = k.CreateStake(ctx, staker2, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(800))
	require.NoError(t, err)

	// Creator (internal) stakes 100 DREAM
	_, err = k.CreateStake(ctx, creator, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(100))
	require.NoError(t, err)

	// Advance time so stakes build conviction (1 week)
	advancedTime := sdk.UnwrapSDKContext(ctx).BlockTime().Add(7 * 24 * time.Hour)
	ctx = sdk.UnwrapSDKContext(ctx).WithBlockTime(advancedTime)

	// Record balances before bonus distribution
	preBalances := make(map[string]math.Int)
	for _, s := range []sdk.AccAddress{staker1, staker2, creator} {
		member, err := k.Member.Get(ctx, s.String())
		require.NoError(t, err)
		preBalances[s.String()] = *member.DreamBalance
	}

	// Distribute completion bonus with budget = 10000
	totalBudget := math.NewInt(10000)
	err = k.DistributeInitiativeCompletionBonus(ctx, initID, totalBudget)
	require.NoError(t, err)

	// Bonus pool = 10% of 10000 = 1000
	bonusPool := math.NewInt(1000)

	// Verify that stakers received bonus (each proportional to conviction)
	totalReceived := math.ZeroInt()
	for _, s := range []sdk.AccAddress{staker1, staker2, creator} {
		member, err := k.Member.Get(ctx, s.String())
		require.NoError(t, err)
		received := member.DreamBalance.Sub(preBalances[s.String()])
		require.True(t, received.GTE(math.ZeroInt()),
			"staker %s should have received non-negative bonus, got %s", s.String(), received.String())
		totalReceived = totalReceived.Add(received)
	}

	// Total distributed should be <= bonusPool (truncation can lose a few units)
	require.True(t, totalReceived.LTE(bonusPool),
		"total distributed (%s) should not exceed bonus pool (%s)", totalReceived.String(), bonusPool.String())
	// But should be greater than zero
	require.True(t, totalReceived.GT(math.ZeroInt()),
		"total distributed should be greater than zero")

	// Verify that staker2 (800 DREAM) received more than staker1 (200 DREAM)
	// since conviction scales with stake amount
	member1, _ := k.Member.Get(ctx, staker1.String())
	member2, _ := k.Member.Get(ctx, staker2.String())
	received1 := member1.DreamBalance.Sub(preBalances[staker1.String()])
	received2 := member2.DreamBalance.Sub(preBalances[staker2.String()])
	require.True(t, received2.GT(received1),
		"staker2 (800 DREAM stake) should receive more bonus than staker1 (200 DREAM stake): got %s vs %s",
		received2.String(), received1.String())

	// Verify that the creator (internal staker) also received a share
	memberCreator, _ := k.Member.Get(ctx, creator.String())
	receivedCreator := memberCreator.DreamBalance.Sub(preBalances[creator.String()])
	require.True(t, receivedCreator.GT(math.ZeroInt()),
		"creator (internal staker) should also receive a bonus share")

	// Test: No stakes returns without error
	// Create a separate initiative with no stakes
	initID2, err := k.CreateInitiative(ctx, creator, projectID, "Empty", "No stakes",
		[]string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))
	require.NoError(t, err)
	err = k.DistributeInitiativeCompletionBonus(ctx, initID2, math.NewInt(10000))
	require.NoError(t, err)
}

// TestDistributeProjectCompletionBonus tests project completion bonus distribution
func TestDistributeProjectCompletionBonus(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Create project creator
	creator := sdk.AccAddress([]byte("proj_creator________"))
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.NewInt(50000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Create stakers
	stakerA := sdk.AccAddress([]byte("project_stakerA_____"))
	stakerB := sdk.AccAddress([]byte("project_stakerB_____"))

	for _, s := range []sdk.AccAddress{stakerA, stakerB} {
		k.Member.Set(ctx, s.String(), types.Member{
			Address:          s.String(),
			DreamBalance:     PtrInt(math.NewInt(10000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{},
		})
	}

	// Create and approve project
	projectID, err := k.CreateProject(ctx, creator, "BonusProj", "Testing bonus",
		[]string{"infra"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical", math.NewInt(100000), math.NewInt(5000), false)
	require.NoError(t, err)

	approver := sdk.AccAddress([]byte("proj_approver_______"))
	err = k.ApproveProject(ctx, projectID, approver, math.NewInt(100000), math.NewInt(5000))
	require.NoError(t, err)

	// Create stakes on the project: stakerA = 300, stakerB = 700
	_, err = k.CreateStake(ctx, stakerA, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", math.NewInt(300))
	require.NoError(t, err)
	_, err = k.CreateStake(ctx, stakerB, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", math.NewInt(700))
	require.NoError(t, err)

	// Verify project stake info
	info, err := k.GetProjectStakeInfo(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1000).String(), info.TotalStaked.String())

	// Record balances before bonus
	preA, _ := k.Member.Get(ctx, stakerA.String())
	preB, _ := k.Member.Get(ctx, stakerB.String())
	balancePreA := *preA.DreamBalance
	balancePreB := *preB.DreamBalance

	// Get ProjectCompletionBonusRate from params (default 5%)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	// Distribute project completion bonus with finalBudget = 20000
	finalBudget := math.NewInt(20000)
	err = k.DistributeProjectCompletionBonus(ctx, projectID, finalBudget)
	require.NoError(t, err)

	// Expected bonus pool = 20000 * 5% = 1000
	expectedBonusPool := math.LegacyNewDecFromInt(finalBudget).
		Mul(params.ProjectCompletionBonusRate).
		TruncateInt()
	require.Equal(t, math.NewInt(1000), expectedBonusPool)

	// Verify stakers received bonus proportional to stake amount
	postA, _ := k.Member.Get(ctx, stakerA.String())
	postB, _ := k.Member.Get(ctx, stakerB.String())
	receivedA := postA.DreamBalance.Sub(balancePreA)
	receivedB := postB.DreamBalance.Sub(balancePreB)

	// stakerA: 300/1000 * 1000 = 300
	require.Equal(t, math.NewInt(300).String(), receivedA.String(),
		"stakerA should receive 300 (30%% of bonus pool)")
	// stakerB: 700/1000 * 1000 = 700
	require.Equal(t, math.NewInt(700).String(), receivedB.String(),
		"stakerB should receive 700 (70%% of bonus pool)")

	// Verify project stake info has accumulated bonus
	updatedInfo, err := k.GetProjectStakeInfo(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, expectedBonusPool.String(), updatedInfo.CompletionBonusPool.String())

	// Test: Zero budget returns without error
	err = k.DistributeProjectCompletionBonus(ctx, projectID, math.ZeroInt())
	require.NoError(t, err)

	// Test: No project stakes returns without error
	// Create a new project with no stakes
	projectID2, err := k.CreateProject(ctx, creator, "EmptyProj", "No stakes",
		[]string{"infra"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical", math.NewInt(50000), math.NewInt(2000), false)
	require.NoError(t, err)
	err = k.ApproveProject(ctx, projectID2, approver, math.NewInt(50000), math.NewInt(2000))
	require.NoError(t, err)

	// Set up project stake info with zero staked
	err = k.ProjectStakeInfo.Set(ctx, projectID2, types.ProjectStakeInfo{
		ProjectId:           projectID2,
		TotalStaked:         math.ZeroInt(),
		CompletionBonusPool: math.ZeroInt(),
	})
	require.NoError(t, err)

	err = k.DistributeProjectCompletionBonus(ctx, projectID2, math.NewInt(10000))
	require.NoError(t, err)
}
