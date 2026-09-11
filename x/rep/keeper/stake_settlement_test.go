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

	_, mErr := k.AccumulateMemberStakeRevenue(f.ctx, target, math.NewInt(100_000_000))
	require.NoError(t, mErr)

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
	_, mErr := k.AccumulateMemberStakeRevenue(ctx, target, math.NewInt(100_000_000))
	require.NoError(t, mErr)

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
		_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2_000))
		require.NoError(t, err, "tranche %d should be accepted", i)
	}

	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(2_000))
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
	_, mErr := k.AccumulateMemberStakeRevenue(f.ctx, target, math.NewInt(100_000_000))
	require.NoError(t, mErr)

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
	// InitGenesis seeds season 1, and with no mint history to size against the
	// budget falls back to the schedule ceiling:
	// staking_pool_cap_base * (1 + 1) * staking_pool_cap_rate.
	expected := params.StakingPoolCapRate.
		MulInt(params.StakingPoolCapBase).
		MulInt64(2).
		TruncateInt()
	require.Equal(t, expected.String(), remaining.String(),
		"InitGenesis must seed the seasonal reward budget")
	require.True(t, expected.LT(params.MaxStakingRewardsPerSeason),
		"the schedule ceiling should bind below the absolute maximum")

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

// TestSettlement_CompleteProjectPaysAccruedStakeRewards is the regression for
// stranded project-stake rewards: CompleteProject used to flip the status
// without settling, and the frozen branch of settleStake pays nothing, so
// everything a staker had accrued while the project was ACTIVE became
// unpayable by any code path. The terminal transition must harvest and mint it.
func TestSettlement_CompleteProjectPaysAccruedStakeRewards(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_pcomp_creator", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "settle_pcomp_staker", math.NewInt(5_000_000_000))
	projectID := newActiveProject(t, k, f.ctx, creator)

	amount := math.NewInt(2_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", amount)
	require.NoError(t, err)
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	pending, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive(), "precondition: the stake accrued something while ACTIVE")

	balanceBefore := *mustMember(t, f, staker).DreamBalance
	spendableBefore := spendable(t, f, staker)
	preTotal, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)

	require.NoError(t, k.CompleteProject(f.ctx, projectID))

	balanceAfter := *mustMember(t, f, staker).DreamBalance
	require.Equal(t, pending.String(), balanceAfter.Sub(balanceBefore).String(),
		"completion must mint exactly the rewards accrued up to the transition")

	// Rewards plus the unlocked principal: completion retires the position
	// rather than leaving it locked against finished work.
	require.Equal(t, pending.Add(amount).String(), spendable(t, f, staker).Sub(spendableBefore).String(),
		"completion must free the principal along with the rewards")

	_, err = k.GetStake(f.ctx, stakeID)
	require.Error(t, err, "the released stake must be deleted")
	require.Equal(t, "0", mustMember(t, f, staker).StakedDream.String())

	postTotal, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)
	require.Equal(t, preTotal.Sub(amount).String(), postTotal.String(),
		"the released principal must leave the seasonal divisor")
}

// TestSettlement_CancelProjectPaysAccruedStakeRewards mirrors the completion
// regression for cancellation: cancelling an ACTIVE project must settle its
// stakers before the status freezes their claims.
func TestSettlement_CancelProjectPaysAccruedStakeRewards(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_pcxl_creator_", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "settle_pcxl_staker_", math.NewInt(5_000_000_000))
	projectID := newActiveProject(t, k, f.ctx, creator)

	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", math.NewInt(2_000_000))
	require.NoError(t, err)
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	pending, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive())

	balanceBefore := *mustMember(t, f, staker).DreamBalance
	spendableBefore := spendable(t, f, staker)
	preTotal, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)

	require.NoError(t, k.CancelProject(f.ctx, projectID, "test cancel"))

	balanceAfter := *mustMember(t, f, staker).DreamBalance
	require.Equal(t, pending.String(), balanceAfter.Sub(balanceBefore).String(),
		"cancellation must mint exactly the rewards accrued up to the transition")

	// Cancelling is a retirement, not a confiscation: the principal is freed
	// with the rewards rather than waiting on a manual unstake that may never
	// come.
	require.Equal(t, pending.Add(math.NewInt(2_000_000)).String(), spendable(t, f, staker).Sub(spendableBefore).String(),
		"cancellation must free the principal along with the rewards")

	_, err = k.GetStake(f.ctx, stakeID)
	require.Error(t, err, "the released stake must be deleted")
	require.Equal(t, "0", mustMember(t, f, staker).StakedDream.String())

	postTotal, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)
	require.Equal(t, preTotal.Sub(math.NewInt(2_000_000)).String(), postTotal.String(),
		"the released principal must leave the seasonal divisor")
}

// TestSettlement_TerminalProjectRejectsNewStakes pins the CreateStake guard:
// a terminal project can never accrue again, so locking fresh DREAM against
// one would strand it in a principal-only position. PROPOSED projects stay
// open for early conviction.
func TestSettlement_TerminalProjectRejectsNewStakes(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_ptrm_creator_", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "settle_ptrm_staker_", math.NewInt(5_000_000_000))

	completedID := newActiveProject(t, k, f.ctx, creator)
	require.NoError(t, k.CompleteProject(f.ctx, completedID))
	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, completedID, "", math.NewInt(1_000_000))
	require.ErrorIs(t, err, types.ErrProjectTerminal)

	cancelledID := newActiveProject(t, k, f.ctx, creator)
	require.NoError(t, k.CancelProject(f.ctx, cancelledID, "test"))
	_, err = k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, cancelledID, "", math.NewInt(1_000_000))
	require.ErrorIs(t, err, types.ErrProjectTerminal)

	// PROPOSED projects accept early-conviction stakes.
	proposedID, err := k.CreateProject(
		f.ctx, creator, "PtrmProp", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100000), math.NewInt(1000), false,
	)
	require.NoError(t, err)
	_, err = k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, proposedID, "", math.NewInt(1_000_000))
	require.NoError(t, err, "staking on a PROPOSED project must stay allowed")
}

// TestSettlement_ApprovalRebasesProposedStakeDebts pins the approval rebase: a
// stake placed while the project was PROPOSED must not harvest the
// PROPOSED-window accumulator growth retroactively once the project completes —
// it accrues only from approval onward.
func TestSettlement_ApprovalRebasesProposedStakeDebts(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "settle_papr_creator", math.NewInt(5_000_000_000))
	live := newStakerMember(t, f, "settle_papr_live___", math.NewInt(5_000_000_000))
	early := newStakerMember(t, f, "settle_papr_early__", math.NewInt(5_000_000_000))

	// A live initiative staker makes the shared accumulator advance; the
	// PROPOSED-project stake dilutes the denominator but accrues nothing.
	initID := newActiveInitiative(t, f, creator, "papr")
	_, err := k.CreateStake(f.ctx, live, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	proposedID, err := k.CreateProject(
		f.ctx, creator, "PaprProp", "Desc", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100000), math.NewInt(1000), false,
	)
	require.NoError(t, err)
	earlyStake, err := k.CreateStake(f.ctx, early, types.StakeTargetType_STAKE_TARGET_PROJECT, proposedID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	// Distribution during the PROPOSED window: the early stake earns nothing.
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))
	pending, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, earlyStake))
	require.NoError(t, err)
	require.True(t, pending.IsZero(), "a stake on a PROPOSED project must not accrue")

	// Approval rebases the debt to the current accumulator.
	require.NoError(t, k.ApproveProject(f.ctx, proposedID, sdk.AccAddress([]byte("approver")), math.NewInt(100000), math.NewInt(1000)))
	pending, err = k.GetPendingStakingRewards(f.ctx, mustStake(t, f, earlyStake))
	require.NoError(t, err)
	require.True(t, pending.IsZero(), "the rebase must wipe the retroactive claim, not pay it")

	// Post-approval distribution accrues to the early stake.
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))
	pending, err = k.GetPendingStakingRewards(f.ctx, mustStake(t, f, earlyStake))
	require.NoError(t, err)
	require.True(t, pending.IsPositive(), "after approval the stake accrues like any live staker")
}

// spendable is the DREAM a member can actually move: LockDREAM does not debit
// DreamBalance, it raises StakedDream against it, so a released principal shows
// up here rather than as a balance increase.
func spendable(t *testing.T, f *fixture, addr sdk.AccAddress) math.Int {
	t.Helper()
	m := mustMember(t, f, addr)
	return m.DreamBalance.Sub(*m.StakedDream)
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

// TestSettlement_ClosedInitiativeReleasesStake is the initiative-side twin of
// the terminal-project regression.
//
// CompleteInitiative settles, unlocks, and deletes the stakes it pays out.
// CloseInitiative and the challenge-REJECTED path used to settle only, leaving
// the principal locked and the record in place. That principal could not earn
// (stakeAccruing is false on terminal work) and could not signal, but it stayed
// inside seasonal_pool/total_staked forever, diluting the yield of everyone
// still backing live work — the exact ratchet CompleteInitiative's
// updateStakePoolTotals call exists to prevent.
//
// The contract on every terminal path: rewards accrued while the work was live
// are paid, the principal comes back, and the denominator shrinks by it.
func TestSettlement_ClosedInitiativeReleasesStake(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "close_accrue_creator", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "close_accrue_staker_", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "closeaccrue")

	preStaked := *mustMember(t, f, staker).StakedDream
	preTotal, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	// One epoch of seasonal accrual while the work is live.
	require.NoError(t, k.InitSeasonalPool(f.ctx, 1))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	pending, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive(), "precondition: the stake accrued while the initiative was live")

	preEarned := *mustMember(t, f, staker).LifetimeEarned

	require.NoError(t, k.CloseInitiative(f.ctx, initID, "retired"))

	// Settled at the transition, not stranded by the status flip.
	postEarned := *mustMember(t, f, staker).LifetimeEarned
	require.Equal(t, pending.String(), postEarned.Sub(preEarned).String(),
		"closing must pay what the stake accrued while the work was live")

	// Principal returned: nothing in the lifecycle is entitled to it, and
	// closing is a retirement rather than a confiscation.
	require.Equal(t, preStaked.String(), mustMember(t, f, staker).StakedDream.String(),
		"closing must unlock the staked principal")
	_, err = k.GetStake(f.ctx, stakeID)
	require.Error(t, err, "the released stake record must be deleted")

	// And the denominator shrinks with it. Without this the seasonal pool goes
	// on dividing revenue by DREAM that has already left.
	postTotal, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)
	require.Equal(t, preTotal.String(), postTotal.String(),
		"closing must shrink total_staked by the released principal")
}

// TestCreateStake_RejectsTerminalInitiative mirrors ErrProjectTerminal: locking
// fresh DREAM against an initiative that can never accrue again strands it in a
// position that only ever returns principal.
func TestCreateStake_RejectsTerminalInitiative(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "term_init_creator___", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "term_init_staker____", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "terminit")

	require.NoError(t, k.CloseInitiative(f.ctx, initID, "retired"))

	_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.ErrorIs(t, err, types.ErrInitiativeTerminal)
}

// TestSettlement_UpheldChallengeReleasesStake covers the second terminal
// transition that used to leave its stake records in place.
//
// CloseInitiative is the retirement path; UpholdChallenge is the failure path,
// moving the initiative to REJECTED. Both had the same defect and both take the
// same fix, but only closure was covered — and the two live in different files,
// so a future edit to one would not be caught by the other's test.
//
// The contract is deliberately identical to closure's: rewards accrued while
// the work was live are paid, and the principal comes back, because the outcome
// does not retroactively unearn either. Punishing the stake itself is what
// slashing is for. What the upheld challenge burns is the assignee's
// self-assign bond, not the DREAM of the members who backed the work.
func TestSettlement_UpheldChallengeReleasesStake(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	_, initID, _ := setupSubmittedInitiative(t, f)

	staker := newStakerMember(t, f, "uphold_stake_backer_", math.NewInt(5_000_000_000))
	preStaked := *mustMember(t, f, staker).StakedDream
	preTotal, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	// One epoch of seasonal accrual while the initiative is still live.
	require.NoError(t, k.InitSeasonalPool(ctx, 1))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))

	pending, err := k.GetPendingStakingRewards(ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive(), "precondition: the stake accrued while the work was live")

	challenger := newStakerMember(t, f, "uphold_challenger___", math.NewInt(5_000_000_000))
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	challengeID, err := k.CreateChallenge(ctx, challenger, initID, "bad work",
		[]string{"evidence"}, params.MinChallengeStake)
	require.NoError(t, err)

	preEarned := *mustMember(t, f, staker).LifetimeEarned

	require.NoError(t, k.UpholdChallenge(ctx, challengeID))

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_REJECTED, initiative.Status,
		"precondition: upholding must reject the initiative")

	postEarned := *mustMember(t, f, staker).LifetimeEarned
	require.Equal(t, pending.String(), postEarned.Sub(preEarned).String(),
		"upholding a challenge must still pay what the stake accrued while the work was live")

	// And the backer's principal comes back, exactly as on the close path.
	require.Equal(t, preStaked.String(), mustMember(t, f, staker).StakedDream.String(),
		"an upheld challenge must not confiscate a backer's principal")
	_, err = k.GetStake(ctx, stakeID)
	require.Error(t, err, "the released stake record must be deleted")

	postTotal, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	require.Equal(t, preTotal.String(), postTotal.String(),
		"rejection must shrink total_staked by the released principal")
}

// TestSettlement_ReleaseIsolatesPerStakeFailure pins the fault-isolation
// contract of releaseInitiativeStakes.
//
// Three of its four callers are reachable from the EndBlocker
// (resolveSilentEscalations -> rejectReviewRound -> CloseInitiative), and
// UnlockDREAM has real error modes — here, a staker whose member record has
// gone. If one such stake could abort the transition, the initiative would be
// unable to retire at all, which is the failure that stranded devnet initiative
// #1 in IN_REVIEW for ~6,000 blocks.
//
// So: the healthy stake is released, the initiative reaches CLOSED regardless,
// and the failed stake is left intact rather than silently destroyed — leaving
// its owner the manual withdrawal, which the next test exercises.
func TestSettlement_ReleaseIsolatesPerStakeFailure(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "isolate_creator_____", math.NewInt(5_000_000_000))
	healthy := newStakerMember(t, f, "isolate_healthy_____", math.NewInt(5_000_000_000))
	broken := newStakerMember(t, f, "isolate_broken______", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "isolate")

	amount := math.NewInt(1_000_000)
	healthyStake, err := k.CreateStake(f.ctx, healthy, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)
	brokenStake, err := k.CreateStake(f.ctx, broken, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	// Break the unlock for one staker only. No seasonal distribution has run,
	// so nothing is pending and the failure is isolated to UnlockDREAM.
	require.NoError(t, k.Member.Remove(f.ctx, broken.String()))

	require.NoError(t, k.CloseInitiative(f.ctx, initID, "retired"),
		"one unreleasable stake must not block the retirement")

	initiative, err := k.GetInitiative(f.ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CLOSED, initiative.Status)

	// The healthy position was released in full.
	_, err = k.GetStake(f.ctx, healthyStake)
	require.Error(t, err, "the healthy stake must be released")
	require.Equal(t, "0", mustMember(t, f, healthy).StakedDream.String())

	// The broken one survives, so its owner still holds the claim ticket.
	surviving, err := k.GetStake(f.ctx, brokenStake)
	require.NoError(t, err, "a stake that could not be unlocked must be left in place, not destroyed")
	require.Equal(t, amount.String(), surviving.Amount.String())
}

// TestSettlement_LeftBehindStakeStaysWithdrawable is the other half of the
// isolation contract: the recovery path has to actually work.
//
// RemoveStake carries no status gate, so a terminal initiative is no obstacle
// to withdrawing manually. This is also the path that drains the positions
// stranded by the old settle-only behaviour on already-closed initiatives.
func TestSettlement_LeftBehindStakeStaysWithdrawable(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "leftover_creator____", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "leftover_staker_____", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "leftover")

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	// Simulate the release having failed for this stake: close the initiative
	// with the member record gone, then restore it.
	member := mustMember(t, f, staker)
	require.NoError(t, k.Member.Remove(f.ctx, staker.String()))
	require.NoError(t, k.CloseInitiative(f.ctx, initID, "retired"))
	require.NoError(t, k.Member.Set(f.ctx, staker.String(), member))

	require.Equal(t, amount.String(), mustStake(t, f, stakeID).Amount.String(),
		"precondition: the stake survived the transition")

	require.NoError(t, k.RemoveStake(advancePast(t, f), stakeID, staker, amount),
		"a stake on a terminal initiative must still be withdrawable")
	require.Equal(t, "0", mustMember(t, f, staker).StakedDream.String())
	_, err = k.GetStake(f.ctx, stakeID)
	require.Error(t, err)
}

// TestSettlement_LeftBehindStakeCannotDoubleClaimReward pins the half of the
// unlock-failure contract the missing-member test above cannot reach.
//
// Deleting the member record (as ReleaseIsolatesPerStakeFailure does) breaks
// MintDREAM first, so the reward is forfeited and never minted — there is no
// double-claim window to close. The interesting failure is the one
// UnlockDREAM's clamp exists for: the staked aggregate has drifted to zero
// while the member record is intact. Then settleStake harvests and MINTS the
// reward, UnlockDREAM refuses, and the stake is persisted with its reward debt
// rebased to zero. A later manual RemoveStake must return the principal
// without minting that reward a second time — the rebased debt is the only
// thing standing between the staker and a double payout.
func TestSettlement_LeftBehindStakeCannotDoubleClaimReward(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := newStakerMember(t, f, "dblclaim_creator___", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "dblclaim_staker____", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "dblclaim")

	preTotal, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	// One epoch of seasonal accrual while the work is still live.
	require.NoError(t, k.InitSeasonalPool(ctx, 1))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))

	pending, err := k.GetPendingStakingRewards(ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive(), "precondition: the stake accrued while the work was live")

	preEarned := *mustMember(t, f, staker).LifetimeEarned

	// Drift the staked aggregate to zero but keep the member record: the mint
	// below still succeeds, and only the unlock fails.
	member := mustMember(t, f, staker)
	member.StakedDream = PtrInt(math.ZeroInt())
	require.NoError(t, k.Member.Set(ctx, staker.String(), member))

	require.NoError(t, k.CloseInitiative(ctx, initID, "retired"))

	// The reward was minted at the transition, and the record survived with its
	// debt rebased to zero — the persisted proof that it was already paid.
	postEarned := *mustMember(t, f, staker).LifetimeEarned
	require.Equal(t, pending.String(), postEarned.Sub(preEarned).String(),
		"the transition must still pay what accrued while the work was live")
	surviving, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err, "a stake whose unlock failed must be left in place")
	require.Equal(t, math.ZeroInt().String(), surviving.RewardDebt.String(),
		"the persisted record must carry the rebased debt")

	// Repair the aggregate and withdraw through the documented recovery path.
	member = mustMember(t, f, staker)
	member.StakedDream = PtrInt(amount)
	require.NoError(t, k.Member.Set(ctx, staker.String(), member))

	require.NoError(t, k.RemoveStake(advancePast(t, f), stakeID, staker, amount),
		"the manual withdrawal path must work once the aggregate is repaired")

	// Principal only: the transition's reward must not be minted twice.
	finalEarned := *mustMember(t, f, staker).LifetimeEarned
	require.Equal(t, postEarned.String(), finalEarned.String(),
		"RemoveStake must not mint the transition's reward a second time")
	_, err = k.GetStake(ctx, stakeID)
	require.Error(t, err, "the recovered stake must be deleted")

	// And the recovery closes the denominator loop: the manual withdrawal
	// shrinks total_staked by the principal the failed unlock had left behind.
	postTotal, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	require.Equal(t, preTotal.String(), postTotal.String(),
		"the manual withdrawal must shrink total_staked by the released principal")
}

// TestSettlement_MintCapAbortsReleaseAndRetryPaysInFull pins the line between
// the two failure classes in releaseInitiativeStakes.
//
// A settle failure against the per-epoch DREAM mint cap is transient: the cap
// clears on its own. Forfeiting the reward for it would destroy a payable claim
// because the block happened to be busy, and would do it order-dependently —
// stakes earlier in the slice get paid, later ones do not. So the whole
// transition aborts instead, and the retry pays in full.
//
// Nothing pre-checks this: CompleteInitiative's projected-mint gate covers the
// season cap only, and the completer reward, treasury share, completion bonus
// and review fees have already drawn on the epoch's budget before the stakes
// are settled.
//
// The abort is only recoverable behind a rollback boundary, which is why every
// caller has one — tx rollback for the three msg-server paths, the CacheContext
// in resolveSilentEscalations for the EndBlocker one. CloseInitiative returns
// the initiative's budget before it reaches the stakes, and that return is not
// idempotent ("cannot return 1000: only 0 allocated" on a second pass), so a
// retry over committed partial writes does not merely re-do work — it fails on
// different grounds. This test runs the aborted attempt on a discarded cache
// branch to model that boundary.
func TestSettlement_MintCapAbortsReleaseAndRetryPaysInFull(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := newStakerMember(t, f, "mintcap_creator_____", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "mintcap_staker______", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "mintcap")

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	// One epoch of seasonal accrual while the work is still live.
	require.NoError(t, k.InitSeasonalPool(ctx, 1))
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(ctx))

	pending, err := k.GetPendingStakingRewards(ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.True(t, pending.IsPositive(), "precondition: the stake accrued while the work was live")

	preEarned := *mustMember(t, f, staker).LifetimeEarned
	preTotal, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	live, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	liveStatus := live.Status

	// Squeeze the epoch budget below what this stake is owed.
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	fullCap := params.MaxDreamMintPerEpoch
	params.MaxDreamMintPerEpoch = math.OneInt()
	require.NoError(t, k.Params.Set(ctx, params))

	// The aborted attempt, on a branch the caller discards.
	cacheCtx, _ := sdk.UnwrapSDKContext(ctx).CacheContext()
	err = k.CloseInitiative(cacheCtx, initID, "retired")
	require.Error(t, err, "a transient cap hit must abort the transition, not forfeit the reward")
	require.ErrorIs(t, err, types.ErrDreamMintCapExceeded)

	// Even inside the branch, the abort came before anything irreversible: the
	// status was not flipped and the stake was not deleted.
	abortedInitiative, err := k.GetInitiative(cacheCtx, initID)
	require.NoError(t, err)
	require.Equal(t, liveStatus, abortedInitiative.Status,
		"the status flip must not happen when the release aborted")
	abortedStake, err := k.GetStake(cacheCtx, stakeID)
	require.NoError(t, err, "the stake must survive an aborted release")
	require.Equal(t, amount.String(), abortedStake.Amount.String())

	// And the discard leaves committed state untouched.
	surviving, err := k.GetStake(ctx, stakeID)
	require.NoError(t, err)
	require.Equal(t, amount.String(), surviving.Amount.String())
	require.Equal(t, preEarned.String(), (*mustMember(t, f, staker).LifetimeEarned).String(),
		"nothing may be minted on the aborted path")

	stillPending, err := k.GetPendingStakingRewards(ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.Equal(t, pending.String(), stillPending.String(),
		"the abort must leave the reward debt un-rebased, so the retry still owes the full amount")

	// The cap clears; the retry settles the initiative in full.
	params.MaxDreamMintPerEpoch = fullCap
	require.NoError(t, k.Params.Set(ctx, params))

	require.NoError(t, k.CloseInitiative(ctx, initID, "retired"),
		"the retry must succeed once the epoch budget is available")

	closed, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CLOSED, closed.Status)
	require.Equal(t, pending.String(), (*mustMember(t, f, staker).LifetimeEarned).Sub(preEarned).String(),
		"the retry must pay exactly what accrued while the work was live")
	_, err = k.GetStake(ctx, stakeID)
	require.Error(t, err, "the released stake must be deleted")
	require.Equal(t, "0", mustMember(t, f, staker).StakedDream.String())

	postTotal, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	require.Equal(t, preTotal.Sub(amount).String(), postTotal.String(),
		"the released principal must leave the denominator")
}

// TestSettlement_CancelledProjectStakeLeavesTheDivisor is the project-side twin
// of TestSettlement_ClosedInitiativeReleasesStake.
//
// A terminal project's stakes earn nothing — stakeAccruing is false once the
// project leaves ACTIVE — but the settle-only version left their principal
// locked inside both the per-project total and the seasonal total_staked
// divisor, so retired work went on diluting the yield of everyone still backing
// live work until each staker happened to unstake by hand. That is the same
// ratchet releaseInitiativeStakes closes for the four initiative transitions.
func TestSettlement_CancelledProjectStakeLeavesTheDivisor(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := newStakerMember(t, f, "pdilute_creator_____", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "pdilute_staker______", math.NewInt(5_000_000_000))
	projectID := newActiveProject(t, k, ctx, creator)

	preTotal, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", amount)
	require.NoError(t, err)

	staked, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	require.Equal(t, preTotal.Add(amount).String(), staked.String(),
		"precondition: the stake is in the divisor while the project is live")
	info, err := k.GetProjectStakeInfo(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, amount.String(), info.TotalStaked.String(),
		"precondition: and in the project's own total")

	require.NoError(t, k.CancelProject(ctx, projectID, "abandoned"))

	postTotal, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	require.Equal(t, preTotal.String(), postTotal.String(),
		"a cancelled project's principal must leave the seasonal divisor, not dilute it forever")

	info, err = k.GetProjectStakeInfo(ctx, projectID)
	if err == nil {
		require.Equal(t, "0", info.TotalStaked.String(),
			"and must leave the project's own total too")
	}

	_, err = k.GetStake(ctx, stakeID)
	require.Error(t, err, "the released stake must be deleted")
	require.Equal(t, "0", mustMember(t, f, staker).StakedDream.String(),
		"the staker's DREAM must be unlocked, not left waiting on a manual unstake")
}

// TestSettlement_CompleteProjectPaysBonusBeforeReleasing pins the ordering
// constraint the release is bracketed by.
//
// DistributeProjectCompletionBonus is weighted by each stake's principal and
// reads the very records releaseProjectStakes deletes, so the release has to
// run below it; stakeAccruing stops paying the moment the status goes terminal,
// so it also has to run above the status flip. Moving it above the bonus would
// silently zero every completion bonus — GetProjectStakeInfo would report no
// stake and the function returns early — which no status assertion would catch.
func TestSettlement_CompleteProjectPaysBonusBeforeReleasing(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	creator := newStakerMember(t, f, "pbonus_creator______", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "pbonus_staker_______", math.NewInt(5_000_000_000))
	projectID := newActiveProject(t, k, ctx, creator)

	amount := math.NewInt(4_000_000)
	stakeID, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID, "", amount)
	require.NoError(t, err)

	// A completion bonus is a rate on the budget actually spent, so give the
	// project something to have spent.
	project, err := k.GetProject(ctx, projectID)
	require.NoError(t, err)
	project.SpentBudget = PtrInt(math.NewInt(20_000))
	require.NoError(t, k.UpdateProject(ctx, project))

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	require.True(t, params.ProjectCompletionBonusRate.IsPositive(),
		"precondition: the bonus rate is on, or this test proves nothing")

	earnedBefore := *mustMember(t, f, staker).LifetimeEarned
	require.NoError(t, k.CompleteProject(ctx, projectID))

	// The bonus landed — it could only have been computed while the stake
	// records were still there.
	earnedAfter := *mustMember(t, f, staker).LifetimeEarned
	require.True(t, earnedAfter.GT(earnedBefore),
		"the completion bonus must be distributed before the stakes are released")

	// And the release still happened, on the same transition.
	_, err = k.GetStake(ctx, stakeID)
	require.Error(t, err, "the released stake must be deleted")
	require.Equal(t, "0", mustMember(t, f, staker).StakedDream.String())
}
