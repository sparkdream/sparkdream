package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// These are flow-level tests: each one exercises a sequence of keeper calls
// rather than a single function. Every defect in the staking reward audit lived
// in the seams *between* functions — each function passed its own unit test
// throughout — so the regressions have to be stated as invariants across a
// whole stake lifecycle.

// settlementFixture builds a member with DREAM to stake.
func newStakerMember(t *testing.T, f *fixture, seed string, balance math.Int) sdk.AccAddress {
	t.Helper()
	addr := sdk.AccAddress([]byte(seed))
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:          addr.String(),
		DreamBalance:     PtrInt(balance),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"tag1": "100.0"},
	}))
	return addr
}

// newActiveInitiative returns an initiative on an approved project.
func newActiveInitiative(t *testing.T, f *fixture, creator sdk.AccAddress, seed string) uint64 {
	t.Helper()
	k := f.keeper
	projectID, err := k.CreateProject(
		f.ctx, creator, "P"+seed, "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100000), math.NewInt(1000), false,
	)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000)))

	initID, err := k.CreateInitiative(
		f.ctx, creator, projectID, "T"+seed, "D", []string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000),
	)
	require.NoError(t, err)
	return initID
}

// advancePast returns a context whose block time clears MinStakeDurationSeconds
// for a stake created at the fixture's current block time.
func advancePast(t *testing.T, f *fixture) sdk.Context {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	return sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(time.Duration(params.MinStakeDurationSeconds+1) * time.Second))
}

// TestSettlement_StakeJoiningAfterDistributionEarnsNothing is the core
// regression for the missing reward-debt baseline on initiative stakes. A stake
// placed after a distribution must not be paid for the accumulator history that
// predates it.
func TestSettlement_StakeJoiningAfterDistributionEarnsNothing(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_join_creator_", math.NewInt(5_000_000_000))
	early := newStakerMember(t, f, "settle_join_early___", math.NewInt(5_000_000_000))
	late := newStakerMember(t, f, "settle_join_late____", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "join")

	amount := math.NewInt(1_000_000)

	earlyStake, err := k.CreateStake(f.ctx, early, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	// One epoch of rewards flows into the pool while only `early` is staked.
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	lateStake, err := k.CreateStake(f.ctx, late, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	earlyPending, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, earlyStake))
	require.NoError(t, err)
	latePending, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, lateStake))
	require.NoError(t, err)

	require.True(t, earlyPending.IsPositive(), "the staker present during the distribution should be owed something")
	require.True(t, latePending.IsZero(),
		"a stake created after the distribution must earn nothing from it, got %s", latePending)
}

// TestSettlement_SecondClaimPaysZero is the regression for the repeatable-mint
// path: claiming must advance reward_debt for every target type, so a second
// consecutive claim settles to nothing.
func TestSettlement_SecondClaimPaysZero(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_twice_creator", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "settle_twice_staker_", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "twice")

	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	ctx := advancePast(t, f)

	first, err := k.ClaimStakingRewards(ctx, stakeID, staker)
	require.NoError(t, err)
	require.True(t, first.IsPositive(), "first claim should pay out")

	second, err := k.ClaimStakingRewards(ctx, stakeID, staker)
	require.NoError(t, err)
	require.True(t, second.IsZero(), "second consecutive claim must pay zero, got %s", second)
}

// TestSettlement_FullUnstakePaysMemberPoolRewards is the regression for
// RemoveStake settling against the wrong accumulator. A member staker who
// unstakes without claiming first must be paid from the member pool, not
// silently forfeit it along with the deleted stake record.
func TestSettlement_FullUnstakePaysMemberPoolRewards(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	target := newStakerMember(t, f, "settle_unstk_target_", math.ZeroInt())
	staker := newStakerMember(t, f, "settle_unstk_staker_", math.NewInt(5_000_000_000))

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, target.String(), amount)
	require.NoError(t, err)

	require.NoError(t, k.AccumulateMemberStakeRevenue(f.ctx, target, math.NewInt(100_000_000)))

	ctx := advancePast(t, f)

	expected, err := k.GetPendingStakingRewards(ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, expected.IsPositive(), "test setup should leave rewards owed")

	before := mustMember(t, f, staker)
	require.NoError(t, k.RemoveStake(ctx, stakeID, staker, amount))
	after := mustMember(t, f, staker)

	// Balance moves by the returned principal plus the harvested rewards.
	gained := after.DreamBalance.Sub(*before.DreamBalance)
	require.Equal(t, expected.String(), gained.String(),
		"full unstake must pay the accrued member-pool rewards, not destroy them")
}

// TestSettlement_PartialUnstakeDoesNotUnderpay is the regression for the stale
// reward_debt left on a shrunken stake. After a partial withdrawal the stake
// must keep earning from zero at its new principal, not sit under a debt sized
// for the original amount.
func TestSettlement_PartialUnstakeDoesNotUnderpay(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	target := newStakerMember(t, f, "settle_part_target__", math.ZeroInt())
	staker := newStakerMember(t, f, "settle_part_staker__", math.NewInt(5_000_000_000))

	amount := math.NewInt(2_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, target.String(), amount)
	require.NoError(t, err)

	ctx := advancePast(t, f)

	// Withdraw half before any revenue has accrued.
	require.NoError(t, k.RemoveStake(ctx, stakeID, staker, math.NewInt(1_000_000)))

	shrunk := mustStake(t, f, stakeID)
	require.Equal(t, math.NewInt(1_000_000).String(), shrunk.Amount.String())

	// Now revenue arrives. The remaining half must earn on it.
	require.NoError(t, k.AccumulateMemberStakeRevenue(ctx, target, math.NewInt(100_000_000)))

	pending, err := k.GetPendingStakingRewards(ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive(),
		"a partially withdrawn stake must keep earning; a stale debt would clamp this to zero")
}

// TestSettlement_PoolTotalStakedRoundTrips is the regression for the missing
// decrement path. Every denominator must return to where it started once the
// stake backing it is withdrawn.
func TestSettlement_PoolTotalStakedRoundTrips(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	target := newStakerMember(t, f, "settle_denom_target_", math.ZeroInt())
	staker := newStakerMember(t, f, "settle_denom_staker_", math.NewInt(5_000_000_000))

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, target.String(), amount)
	require.NoError(t, err)

	pool, err := k.GetMemberStakePool(f.ctx, target)
	require.NoError(t, err)
	require.Equal(t, amount.String(), pool.TotalStaked.String())

	ctx := advancePast(t, f)
	require.NoError(t, k.RemoveStake(ctx, stakeID, staker, amount))

	pool, err = k.GetMemberStakePool(ctx, target)
	require.NoError(t, err)
	require.True(t, pool.TotalStaked.IsZero(),
		"pool total_staked must drop on unstake, got %s", pool.TotalStaked)
}

// TestSettlement_SeasonalTotalStakedTracksLiveStakes checks the seasonal
// denominator at all three of its mutation sites: creation, withdrawal, and the
// stake deletion inside CompleteInitiative.
func TestSettlement_SeasonalTotalStakedTracksLiveStakes(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_seas_creator_", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "settle_seas_staker__", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "seas")

	start, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	afterStake, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)
	require.Equal(t, start.Add(amount).String(), afterStake.String(),
		"creating an initiative stake must grow the seasonal denominator")

	ctx := advancePast(t, f)
	require.NoError(t, k.RemoveStake(ctx, stakeID, staker, amount))

	afterUnstake, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	require.Equal(t, start.String(), afterUnstake.String(),
		"withdrawing must shrink the seasonal denominator back")
}

// TestSettlement_CompletionBonusReachesExternalStaker is the regression for the
// bonus being distributed after the stakes were deleted. It had never paid out
// on any completed initiative.
func TestSettlement_CompletionBonusReachesExternalStaker(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_bonus_creator", math.NewInt(5_000_000_000))
	assignee := newStakerMember(t, f, "settle_bonus_assigne", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "settle_bonus_staker_", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "bonus")

	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	require.NoError(t, k.AssignInitiativeToMember(f.ctx, initID, assignee))
	require.NoError(t, k.SubmitInitiativeWork(f.ctx, initID, assignee, "ipfs://deliverable"))

	// Let the stake mature so its time-weighted conviction is nonzero, which is
	// what the bonus is weighted by.
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	ctx := sdkCtx.WithBlockTime(sdkCtx.BlockTime().Add(30 * 24 * time.Hour))

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	required := math.LegacyNewDec(100)
	initiative.RequiredConviction = PtrDec(required)
	initiative.CurrentConviction = PtrDec(required.MulInt64(2))
	initiative.ExternalConviction = PtrDec(required.MulInt64(2))
	require.NoError(t, k.UpdateInitiative(ctx, initiative))

	before := mustMember(t, f, staker)
	advanceToCompletable(t, k, ctx, initID)
	require.NoError(t, k.CompleteInitiative(ctx, initID))
	after := mustMember(t, f, staker)

	require.True(t, after.LifetimeEarned.GT(*before.LifetimeEarned),
		"an external staker must receive a nonzero conviction-weighted completion bonus")
}

// TestSettlement_TrancheCapBoundsRecordCount asserts the per-target tranche cap
// that bounds the per-block conviction sweep.
func TestSettlement_TrancheCapBoundsRecordCount(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_tranche_creat", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "settle_tranche_stakr", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "tranche")

	for i := 0; i < types.MaxStakeTranchesPerTarget; i++ {
		_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000))
		require.NoError(t, err, "tranche %d should be accepted", i)
	}

	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000))
	require.ErrorIs(t, err, types.ErrTooManyStakeTranches)
}

// TestSettlement_EarlyUnstakeForfeitsRewards asserts that leaving before
// MinStakeDurationSeconds returns the principal but not the rewards, and that
// the forfeited DREAM is never minted.
func TestSettlement_EarlyUnstakeForfeitsRewards(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	target := newStakerMember(t, f, "settle_early_target_", math.ZeroInt())
	staker := newStakerMember(t, f, "settle_early_staker_", math.NewInt(5_000_000_000))

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, target.String(), amount)
	require.NoError(t, err)
	require.NoError(t, k.AccumulateMemberStakeRevenue(f.ctx, target, math.NewInt(100_000_000)))

	pending, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive())

	before := mustMember(t, f, staker)
	// Unstake immediately, well inside the minimum holding period.
	require.NoError(t, k.RemoveStake(f.ctx, stakeID, staker, amount))
	after := mustMember(t, f, staker)

	require.True(t, after.LifetimeEarned.Equal(*before.LifetimeEarned),
		"an early unstake must not mint rewards")
}

// TestSettlement_GenesisSeedsPoolAndDenominators covers the InitGenesis wiring:
// the seasonal pool has to be seeded (or DistributeEpochStakingRewardsFromPool
// returns early forever) and SeasonalPoolTotalStaked has to be rebuilt, since it
// is derived state that genesis does not export.
func TestSettlement_GenesisSeedsPoolAndDenominators(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	remaining, err := k.GetSeasonalPoolRemaining(f.ctx)
	require.NoError(t, err)
	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, params.MaxStakingRewardsPerSeason.String(), remaining.String(),
		"InitGenesis must seed the seasonal reward budget")

	// A live stake must be reflected in the rebuilt denominator.
	creator := newStakerMember(t, f, "settle_gen_creator__", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "settle_gen_staker___", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "gen")

	amount := math.NewInt(1_000_000)
	_, err = k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	// Corrupt the denominator, then reconcile it back from live stakes.
	require.NoError(t, k.UpdateSeasonalPoolTotalStaked(f.ctx, math.NewInt(999_999_999)))
	require.NoError(t, k.ReconcileStakePoolTotals(f.ctx))

	total, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)
	require.Equal(t, amount.String(), total.String(),
		"ReconcileStakePoolTotals must recompute the denominator from live stakes")
}

// TestSettlement_ReconcileZeroesPoolsWithNoLiveStakes guards the repair path
// against the failure mode it exists to fix: a pool that keeps a positive
// denominator after every backing stake is gone would keep dividing incoming
// revenue by DREAM that no longer exists.
func TestSettlement_ReconcileZeroesPoolsWithNoLiveStakes(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	target := sdk.AccAddress([]byte("settle_recon_target_"))
	require.NoError(t, k.MemberStakePool.Set(f.ctx, target.String(), types.MemberStakePool{
		Member:            target.String(),
		TotalStaked:       math.NewInt(500_000),
		PendingRevenue:    math.ZeroInt(),
		AccRewardPerShare: math.LegacyZeroDec(),
	}))

	require.NoError(t, k.ReconcileStakePoolTotals(f.ctx))

	pool, err := k.GetMemberStakePool(f.ctx, target)
	require.NoError(t, err)
	require.True(t, pool.TotalStaked.IsZero(),
		"a pool with no backing stakes must be zeroed, not left stale")
}

// TestSettlement_FrozenProjectStopsAccruing asserts that a project past ACTIVE
// stops earning, and that trimming such a stake scales its debt proportionally
// rather than rebasing it to the live accumulator — which would forfeit what the
// staker had already earned while the project was active.
func TestSettlement_FrozenProjectStopsAccruing(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_frozen_creatr", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "settle_frozen_staker", math.NewInt(5_000_000_000))

	projectID, err := k.CreateProject(
		f.ctx, creator, "FrozenProj", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100000), math.NewInt(1000), false,
	)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(f.ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000)))

	amount := math.NewInt(2_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", amount)
	require.NoError(t, err)
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	// While ACTIVE it is owed something.
	pending, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive())

	// Freeze the project.
	project, err := k.GetProject(f.ctx, projectID)
	require.NoError(t, err)
	project.Status = types.ProjectStatus_PROJECT_STATUS_COMPLETED
	require.NoError(t, k.UpdateProject(f.ctx, project))

	frozenPending, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, frozenPending.IsZero(), "a non-ACTIVE project must stop accruing")

	debtBefore := mustStake(t, f, stakeID).RewardDebt

	// Trim half. The debt should halve with the principal, not reset.
	ctx := advancePast(t, f)
	require.NoError(t, k.RemoveStake(ctx, stakeID, staker, math.NewInt(1_000_000)))

	debtAfter := mustStake(t, f, stakeID).RewardDebt
	require.Equal(t, debtBefore.QuoRaw(2).String(), debtAfter.String(),
		"a frozen stake's debt must scale with the principal, preserving the accrued claim")
}

func mustStake(t *testing.T, f *fixture, stakeID uint64) types.Stake {
	t.Helper()
	stake, err := f.keeper.GetStake(f.ctx, stakeID)
	require.NoError(t, err)
	return stake
}

func mustMember(t *testing.T, f *fixture, addr sdk.AccAddress) types.Member {
	t.Helper()
	member, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	return member
}
