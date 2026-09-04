package keeper_test

import (
	"context"

	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
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
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "50.0"},
	})

	// Create target member
	k.Member.Set(ctx, target.String(), types.Member{
		Address:          target.String(),
		DreamBalance:     PtrInt(math.NewInt(5000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	})

	// Test: Create stake on member
	stakeAmount := math.NewInt(2000)
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
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Test: Create stake on tag
	tagName := "backend"
	stakeAmount := math.NewInt(2000)
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
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Test: Try to stake on self (should fail with default params)
	_, err := k.CreateStake(ctx, member, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, member.String(), math.NewInt(2000))
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
		DreamBalance:     PtrInt(math.NewInt(5000)),
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
	_, err = k.AccumulateMemberStakeRevenue(ctx, target, revenue)
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
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.NewInt(100)),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Create project and initiative for stake target
	projectID, _ := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	initID, _ := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

	// Create stake
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2000))
	require.NoError(t, err)

	// Get stake and verify initial state
	stake, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err)
	require.Equal(t, int64(0), stake.LastClaimedAt)

	// A freshly created stake has not met MinStakeDurationSeconds, so rewards
	// are not yet collectable. Nothing is forfeited — the debt is untouched and
	// the rewards keep accruing.
	_, err = k.ClaimStakingRewards(ctx, stakeID, staker)
	require.ErrorIs(t, err, types.ErrMinStakeDuration)

	// Past the minimum duration the claim succeeds; with a zero accumulator
	// there is nothing to pay out yet.
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	matureCtx := sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(time.Duration(params.MinStakeDurationSeconds+1) * time.Second))

	claimed, err := k.ClaimStakingRewards(matureCtx, stakeID, staker)
	require.NoError(t, err)
	require.True(t, claimed.IsZero())

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
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.NewInt(100)),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Create project and initiative
	projectID, _ := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	initID, _ := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

	// Create stake
	initialAmount := math.NewInt(2000)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", initialAmount)
	require.NoError(t, err)

	// Compounding is rejected for initiative stakes: growing the principal in
	// place would hand the new DREAM the maturity of the original created_at.
	_, err = k.CompoundStakingRewards(ctx, stakeID, staker)
	require.ErrorIs(t, err, types.ErrCompoundNotSupported)

	// The stake is untouched.
	stake, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err)
	require.Equal(t, initialAmount, stake.Amount)
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
		DreamBalance:     PtrInt(math.NewInt(10000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})

	// Create project and initiative
	projectID, _ := k.CreateProject(ctx, staker, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
	initID, _ := k.CreateInitiative(ctx, staker, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))

	// Create stake
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2000))
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
		DreamBalance:     PtrInt(math.NewInt(10000)),
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
	stakeAmount := math.NewInt(5000)
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
	_, err = k.AccumulateTagStakeRevenue(ctx, tags, totalRevenue)
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
	_, err = k.AccumulateTagStakeRevenue(ctx, []string{"go"}, totalRevenue)
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
		"technical", math.NewInt(10000000), math.NewInt(10000), false)
	require.NoError(t, err)

	approver := sdk.AccAddress([]byte("approver____________"))
	err = k.ApproveProject(ctx, projectID, approver, math.NewInt(10000000), math.NewInt(10000))
	require.NoError(t, err)

	// Create initiative. The budget sets required_conviction, and the bonus is
	// paid on the same capped conviction the completion gate counts, so it has
	// to be large enough that max_conviction_share_per_member does not swallow
	// both stakers and flatten the proportionality asserted below:
	// cap = conviction_per_dream * sqrt(budget) * 0.35 = 99 at 2 DREAM, above
	// sqrt(8000) = 89.4.
	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "Do the work",
		[]string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(2000000))
	require.NoError(t, err)

	// Staker1 stakes 2000 micro-DREAM, staker2 stakes 8000
	_, err = k.CreateStake(ctx, staker1, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2000))
	require.NoError(t, err)
	_, err = k.CreateStake(ctx, staker2, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(8000))
	require.NoError(t, err)

	// Creator (internal) stakes 1000 micro-DREAM
	_, err = k.CreateStake(ctx, creator, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2000))
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

	// Verify that staker2 (8000) received more than staker1 (2000)
	// since conviction scales with stake amount
	member1, _ := k.Member.Get(ctx, staker1.String())
	member2, _ := k.Member.Get(ctx, staker2.String())
	received1 := member1.DreamBalance.Sub(preBalances[staker1.String()])
	received2 := member2.DreamBalance.Sub(preBalances[staker2.String()])
	require.True(t, received2.GT(received1),
		"staker2 (8000 stake) should receive more bonus than staker1 (2000 stake): got %s vs %s",
		received2.String(), received1.String())

	// The creator authored both the project and the initiative, so they are an
	// affiliate and the bonus — which exists to reward independent vouching —
	// is withheld. Their staked principal is untouched; only this extra mint is
	// skipped, and the skipped share is never minted rather than redistributed.
	memberCreator, _ := k.Member.Get(ctx, creator.String())
	receivedCreator := memberCreator.DreamBalance.Sub(preBalances[creator.String()])
	require.True(t, receivedCreator.IsZero(),
		"creator is an affiliate and must not receive a completion bonus, got %s",
		receivedCreator.String())

	// Test: No stakes returns without error
	// Create a separate initiative with no stakes
	initID2, err := k.CreateInitiative(ctx, creator, projectID, "Empty", "No stakes",
		[]string{"backend"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
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

	// Create stakes on the project: stakerA = 3000, stakerB = 7000
	_, err = k.CreateStake(ctx, stakerA, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", math.NewInt(3000))
	require.NoError(t, err)
	_, err = k.CreateStake(ctx, stakerB, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", math.NewInt(7000))
	require.NoError(t, err)

	// Verify project stake info
	info, err := k.GetProjectStakeInfo(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(10000).String(), info.TotalStaked.String())

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
	_, err = k.DistributeProjectCompletionBonus(ctx, projectID, finalBudget)
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

	// The bonus is paid out in full immediately, so nothing is escrowed:
	// CompletionBonusPool stays at zero rather than tracking a liability that
	// no code path would ever pay from.
	updatedInfo, err := k.GetProjectStakeInfo(ctx, projectID)
	require.NoError(t, err)
	require.True(t, updatedInfo.CompletionBonusPool.IsZero())

	// Test: Zero budget returns without error
	_, err = k.DistributeProjectCompletionBonus(ctx, projectID, math.ZeroInt())
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

	_, err = k.DistributeProjectCompletionBonus(ctx, projectID2, math.NewInt(10000))
	require.NoError(t, err)
}

// bonusFixture builds a project + initiative with `budget` and returns the
// initiative id, ready for external stakers to be added.
func bonusFixture(t *testing.T, k keeper.Keeper, ctx sdk.Context, creator sdk.AccAddress, budget math.Int) uint64 {
	t.Helper()
	k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.NewInt(100_000_000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"backend": "100.0"},
	})
	projectID, err := k.CreateProject(ctx, creator, "P", "D", []string{"backend"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100_000_000), math.NewInt(10000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver____________")),
		math.NewInt(100_000_000), math.NewInt(10000)))
	initID, err := k.CreateInitiative(ctx, creator, projectID, "T", "D", []string{"backend"},
		types.InitiativeTier_INITIATIVE_TIER_EXPERT,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)
	require.NoError(t, err)
	return initID
}

func bonusStaker(t *testing.T, k keeper.Keeper, ctx sdk.Context, name string, balance math.Int) sdk.AccAddress {
	t.Helper()
	addr := sdk.AccAddress([]byte(name))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:          addr.String(),
		DreamBalance:     PtrInt(balance),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	})
	return addr
}

func bonusPaid(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, pre math.Int) math.Int {
	t.Helper()
	m, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err)
	return m.DreamBalance.Sub(pre)
}

// TestCompletionBonus_SplittingAStakeGivesNoAdvantage is the regression for the
// stake-splitting hole in the completion bonus.
//
// The completion gate has aggregated raw conviction per staker before taking
// the sqrt since it was written, precisely so that splitting a position across
// tranches buys nothing. The payout beside it took the sqrt per stake record,
// so ten tranches of A/10 weighed sqrt(10) times one stake of A — the exact
// exploit the gate refuses to reward, rewarded one function later.
//
// Two stakers put up identical capital; one splits it across the maximum
// tranches. They must be paid the same.
func TestCompletionBonus_SplittingAStakeGivesNoAdvantage(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := sdk.AccAddress([]byte("bonus_creator_______"))
	initID := bonusFixture(t, k, ctx, creator, math.NewInt(50_000_000))

	const capital = 100_000
	whole := bonusStaker(t, k, ctx, "whole_staker________", math.NewInt(capital*4))
	split := bonusStaker(t, k, ctx, "split_staker________", math.NewInt(capital*4))

	_, err := k.CreateStake(ctx, whole, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(capital))
	require.NoError(t, err)

	tranches := int64(types.MaxStakeTranchesPerTarget)
	for i := int64(0); i < tranches; i++ {
		_, err := k.CreateStake(ctx, split, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "",
			math.NewInt(capital/tranches))
		require.NoError(t, err, "tranche %d", i)
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(14 * 24 * time.Hour))

	preWhole, err := k.Member.Get(ctx, whole.String())
	require.NoError(t, err)
	preSplit, err := k.Member.Get(ctx, split.String())
	require.NoError(t, err)

	require.NoError(t, k.DistributeInitiativeCompletionBonus(ctx, initID, math.NewInt(50_000_000)))

	paidWhole := bonusPaid(t, k, ctx, whole, *preWhole.DreamBalance)
	paidSplit := bonusPaid(t, k, ctx, split, *preSplit.DreamBalance)

	require.True(t, paidWhole.IsPositive(), "the single-stake holder must be paid at all, got %s", paidWhole)
	// Equal capital, equal pay — within the one-unit truncation each share takes.
	diff := paidWhole.Sub(paidSplit).Abs()
	require.True(t, diff.LTE(math.NewInt(1)),
		"splitting %d across %d tranches must not change the bonus: whole=%s split=%s",
		capital, tranches, paidWhole, paidSplit)
}

// TestCompletionBonus_CapsAtAMultipleOfTheStakeBehindIt is the regression for
// the return multiple that grew with the staker count.
//
// Priced off the budget alone, the bonus pays 2.5*N times each staker's stake,
// because clearing the conviction gate costs conviction_per_dream^2 * budget/N
// in total across N stakers while the budget share stays fixed. Pricing it off
// capital at risk holds the return at max_completion_bonus_stake_multiple no
// matter how many stakers split it.
func TestCompletionBonus_CapsAtAMultipleOfTheStakeBehindIt(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	multiple := params.MaxCompletionBonusStakeMultiple
	require.True(t, multiple.IsPositive())

	creator := sdk.AccAddress([]byte("cap_creator_________"))
	// A large budget against deliberately small stakes: 10% of it dwarfs any
	// multiple of the capital behind it, so the stake term is what binds.
	budget := math.NewInt(50_000_000)
	initID := bonusFixture(t, k, ctx, creator, budget)

	const perStaker = 2_000
	stakers := []sdk.AccAddress{
		bonusStaker(t, k, ctx, "cap_staker_one______", math.NewInt(perStaker*4)),
		bonusStaker(t, k, ctx, "cap_staker_two______", math.NewInt(perStaker*4)),
		bonusStaker(t, k, ctx, "cap_staker_three____", math.NewInt(perStaker*4)),
	}
	externalStake := math.NewInt(perStaker * int64(len(stakers)))

	pre := make([]math.Int, len(stakers))
	for i, s := range stakers {
		_, err := k.CreateStake(ctx, s, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(perStaker))
		require.NoError(t, err)
		m, err := k.Member.Get(ctx, s.String())
		require.NoError(t, err)
		pre[i] = *m.DreamBalance
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(14 * 24 * time.Hour))

	// The uncapped pool would be 10% of budget — far more than the stake allows.
	uncapped := k.InitiativeCompletionBonusPool(ctx, budget)
	stakeCap := multiple.MulInt(externalStake).TruncateInt()
	require.True(t, stakeCap.LT(uncapped),
		"fixture must put the stake term below the rate term: cap=%s rate=%s", stakeCap, uncapped)

	require.NoError(t, k.DistributeInitiativeCompletionBonus(ctx, initID, budget))

	totalPaid := math.ZeroInt()
	for i, s := range stakers {
		totalPaid = totalPaid.Add(bonusPaid(t, k, ctx, s, pre[i]))
	}

	require.True(t, totalPaid.IsPositive(), "the bonus must still pay something")
	require.True(t, totalPaid.LTE(stakeCap),
		"bonus %s exceeded %s x the %s staked behind it (cap %s)",
		totalPaid, multiple, externalStake, stakeCap)
	// And the return each staker sees is bounded by the multiple, not by N.
	require.True(t, totalPaid.LTE(multiple.MulInt(externalStake).TruncateInt()),
		"per-staker return must not scale with the staker count")
}

// TestCreateStake_RejectsDustBelowMinimum covers the floor that did not exist:
// every weighting a stake feeds is per-record or sqrt-scaled, so a
// one-micro-DREAM stake bought a full participant slot for nothing.
func TestCreateStake_RejectsDustBelowMinimum(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	require.True(t, params.MinStakeAmount.IsPositive(), "default params must carry a floor")

	creator := sdk.AccAddress([]byte("dust_creator________"))
	initID := bonusFixture(t, k, ctx, creator, math.NewInt(1_000_000))
	staker := bonusStaker(t, k, ctx, "dust_staker_________", math.NewInt(10_000_000))

	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "",
		params.MinStakeAmount.SubRaw(1))
	require.ErrorIs(t, err, types.ErrStakeBelowMinimum)

	// Exactly at the floor is accepted.
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "",
		params.MinStakeAmount)
	require.NoError(t, err)
}

// projectBonusFixture builds an approved project with `creator` and returns
// its id, mirroring the member setup TestDistributeProjectCompletionBonus
// builds by hand.
func projectBonusFixture(t *testing.T, k keeper.Keeper, ctx context.Context, creator sdk.AccAddress) uint64 {
	t.Helper()
	require.NoError(t, k.Member.Set(ctx, creator.String(), types.Member{
		Address:          creator.String(),
		DreamBalance:     PtrInt(math.NewInt(50_000_000_000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	}))

	projectID, err := k.CreateProject(ctx, creator, "BonusCapProj", "Capped bonus",
		[]string{"infra"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical", math.NewInt(100000), math.NewInt(5000), false)
	require.NoError(t, err)

	approver := sdk.AccAddress([]byte("cap_approver_________"))
	require.NoError(t, k.ApproveProject(ctx, projectID, approver, math.NewInt(100000), math.NewInt(5000)))
	return projectID
}

func projectBonusStaker(t *testing.T, k keeper.Keeper, ctx context.Context, name string, balance math.Int) sdk.AccAddress {
	t.Helper()
	addr := sdk.AccAddress([]byte(name))
	require.NoError(t, k.Member.Set(ctx, addr.String(), types.Member{
		Address:          addr.String(),
		DreamBalance:     PtrInt(balance),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{},
	}))
	return addr
}

// TestProjectBonus_CappedByStakeMultiple covers the dust-capture fix: the
// bonus pool is bounded at max_completion_bonus_stake_multiple x total staked,
// so a minimum-size stake on a completing project can no longer collect 5% of
// its whole spent budget.
func TestProjectBonus_CappedByStakeMultiple(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := sdk.AccAddress([]byte("cap_creator_________"))
	projectID := projectBonusFixture(t, k, ctx, creator)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	staker := projectBonusStaker(t, k, ctx, "cap_bonus_staker_____", math.NewInt(10_000_000_000))
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "",
		params.MinStakeAmount)
	require.NoError(t, err)

	pre, _ := k.Member.Get(ctx, staker.String())
	preBal := *pre.DreamBalance

	// Spent budget of 2 DREAM would rate a 5% pool of 100,000 micro; the
	// stake-multiple cap holds the payout to multiple x min_stake_amount.
	_, err = k.DistributeProjectCompletionBonus(ctx, projectID, math.NewInt(2_000_000))
	require.NoError(t, err)

	post, _ := k.Member.Get(ctx, staker.String())
	received := post.DreamBalance.Sub(preBal)
	expected := params.MaxCompletionBonusStakeMultiple.MulInt(params.MinStakeAmount).TruncateInt()
	require.Equal(t, expected.String(), received.String(),
		"a dust stake must receive at most the multiple cap, not 5%% of the budget")
}

// TestProjectBonus_ExcludesCreator covers the insider fix: the project creator
// (and their invitation neighborhood) is not paid a completion bonus on their
// own project, mirroring the external-only rule on the initiative side.
func TestProjectBonus_ExcludesCreator(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := sdk.AccAddress([]byte("cap_creator_________"))
	projectID := projectBonusFixture(t, k, ctx, creator)

	external := projectBonusStaker(t, k, ctx, "cap_external_staker_", math.NewInt(10_000_000_000))

	// Creator stakes 90% of the pool, an arm's-length member 10%.
	_, err := k.CreateStake(ctx, creator, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", math.NewInt(18_000))
	require.NoError(t, err)
	_, err = k.CreateStake(ctx, external, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", math.NewInt(2000))
	require.NoError(t, err)

	preCreator, _ := k.Member.Get(ctx, creator.String())
	preExternal, _ := k.Member.Get(ctx, external.String())

	// Rate pool = 5% of 20,000 = 1,000; the stake-multiple cap (>= 2 x 20,000)
	// does not bind.
	// The payout is pro-rata by principal over the total staked, and the
	// creator's 90% share is withheld rather than redistributed — so only the
	// external staker's 10% (100) is ever minted.
	minted, err := k.DistributeProjectCompletionBonus(ctx, projectID, math.NewInt(20_000))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100).String(), minted.String())

	postCreator, _ := k.Member.Get(ctx, creator.String())
	postExternal, _ := k.Member.Get(ctx, external.String())
	require.True(t, postCreator.DreamBalance.Equal(*preCreator.DreamBalance),
		"the creator must not be paid a bonus for staking on their own project")
	require.Equal(t, math.NewInt(100).String(),
		postExternal.DreamBalance.Sub(*preExternal.DreamBalance).String(),
		"the external staker receives exactly their pro-rata share")
}

// TestProjectBonus_ClampedToSeasonHeadroom covers the season-cap accounting
// fix: the bonus can never push initiative-completion minting past
// max_initiative_rewards_per_season.
func TestProjectBonus_ClampedToSeasonHeadroom(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := sdk.AccAddress([]byte("cap_creator_________"))
	projectID := projectBonusFixture(t, k, ctx, creator)

	staker := projectBonusStaker(t, k, ctx, "cap_headroom_staker_", math.NewInt(10_000_000_000))
	_, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	// Leave exactly 500 micro of season headroom; the 5% rate on a 20,000
	// micro budget would want to mint 1,000.
	require.NoError(t, k.TrackInitiativeRewardMint(ctx, params.MaxInitiativeRewardsPerSeason.SubRaw(500)))

	minted, err := k.DistributeProjectCompletionBonus(ctx, projectID, math.NewInt(20_000))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500).String(), minted.String(),
		"the bonus must clamp to the remaining season headroom")

	// And an exhausted season mints nothing rather than going over.
	require.NoError(t, k.TrackInitiativeRewardMint(ctx, math.NewInt(500)))
	minted, err = k.DistributeProjectCompletionBonus(ctx, projectID, math.NewInt(20_000))
	require.NoError(t, err)
	require.True(t, minted.IsZero())
}

// TestCreateStake_ContentAggregateCap covers the anti-park bound: content
// stakes across every item combined are capped at
// max_total_content_stake_per_member, enforced against the member's live
// aggregate, and unstaking frees room under the cap.
func TestCreateStake_ContentAggregateCap(t *testing.T) {
	params := types.DefaultParams()
	params.MaxContentStakePerMember = math.NewInt(10_000)
	// 2,000 of headroom above the two stakes below, so the boundary probes can
	// sit either side of the cap while still clearing min_stake_amount.
	params.MaxTotalContentStakePerMember = math.NewInt(16_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	staker := projectBonusStaker(t, k, ctx, "content_park_staker_", math.NewInt(1_000_000))

	// Two items, fully under the aggregate.
	_, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, 1, "", math.NewInt(10_000))
	require.NoError(t, err)
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT, 2, "", math.NewInt(4_000))
	require.NoError(t, err)

	member, err := k.Member.Get(ctx, staker.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(14_000).String(), (*member.ContentStakedDream).String(),
		"the member aggregate must track content stakes across items")

	// A third stake would push the aggregate one over the cap.
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT, 3, "", math.NewInt(2_001))
	require.ErrorIs(t, err, types.ErrContentStakeCap)

	// Exactly at the cap is allowed.
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT, 3, "", math.NewInt(2_000))
	require.NoError(t, err)

	// Unstaking frees room under the cap.
	stakes, err := k.GetStakesByTarget(ctx, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, 1)
	require.NoError(t, err)
	require.Len(t, stakes, 1)
	require.NoError(t, k.RemoveStake(ctx, stakes[0].Id, staker, math.NewInt(10_000)))

	member, err = k.Member.Get(ctx, staker.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(6_000).String(), (*member.ContentStakedDream).String(),
		"removing a content stake must shrink the aggregate")

	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, 4, "", math.NewInt(9_999))
	require.NoError(t, err)
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, 5, "", math.NewInt(2_000))
	require.ErrorIs(t, err, types.ErrContentStakeCap,
		"17,999 would exceed the 16,000 aggregate; the floor check must not shadow the cap")
}
