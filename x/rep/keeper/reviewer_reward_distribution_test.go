package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

// The DREAM review fee pays for the act of reviewing and is deliberately
// outcome-blind. This pool pays for reviewing *well* — gated on windowed
// accuracy from challenge outcomes — so a reviewer who files plausible verdicts
// without reading the deliverable collects the fee and nothing more.

// installLedger gives the stub bank keeper a real in-memory balance so pool
// funding, payout and burn can be observed rather than asserted about a no-op.
func installLedger(f *fixture) map[string]math.Int {
	ledger := map[string]math.Int{}
	get := func(a sdk.AccAddress) math.Int {
		if v, ok := ledger[a.String()]; ok {
			return v
		}
		return math.ZeroInt()
	}
	f.bankKeeper.GetBalanceFn = func(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, get(addr))
	}
	f.bankKeeper.SendCoinsFn = func(_ context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
		v := amt[0].Amount
		if get(from).LT(v) {
			return fmt.Errorf("insufficient funds")
		}
		ledger[from.String()] = get(from).Sub(v)
		ledger[to.String()] = get(to).Add(v)
		return nil
	}
	// The overflow burns route through SendCoinsFromAccountToModule rather than
	// a plain SendCoins: a plain send to the raw module address creates a
	// BaseAccount there and BurnCoins then panics resolving it as a module
	// account. The ledger must model the same hop or the burn tests assert
	// against an accounting the code no longer performs.
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(_ context.Context, from sdk.AccAddress, module string, amt sdk.Coins) error {
		to := authtypes.NewModuleAddress(module)
		v := amt[0].Amount
		if get(from).LT(v) {
			return fmt.Errorf("insufficient funds")
		}
		ledger[from.String()] = get(from).Sub(v)
		ledger[to.String()] = get(to).Add(v)
		return nil
	}
	f.bankKeeper.BurnCoinsFn = func(_ context.Context, module string, amt sdk.Coins) error {
		addr := authtypes.NewModuleAddress(module)
		ledger[addr.String()] = get(addr).Sub(amt[0].Amount)
		return nil
	}
	return ledger
}

// fundReviewerPool credits the pool the way a council policy would: an ordinary
// bank balance at a deterministic sub-address, no bespoke funding message.
func fundReviewerPool(f *fixture, ledger map[string]math.Int, amount math.Int) {
	ledger[keeper.ReviewerRewardPoolAddress().String()] = amount
}

// mkScoredReviewer bonds an address as a NORMAL reviewer and gives it a
// windowed accuracy record of upheld/overturned verdicts.
func mkScoredReviewer(t *testing.T, f *fixture, addr sdk.AccAddress, upheld, overturned int) {
	t.Helper()
	bondReviewer(t, f.keeper, f.ctx, addr, 100_000_000)
	for i := 0; i < upheld; i++ {
		require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx,
			types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, addr.String(), "review", true))
	}
	for i := 0; i < overturned; i++ {
		require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx,
			types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, addr.String(), "review", false))
	}
}

func TestReviewerPoolPaysOnlyAboveTheAccuracyBar(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	accurate := sdk.AccAddress([]byte("rw-accurate-----"))
	sloppy := sdk.AccAddress([]byte("rw-sloppy-------"))
	mkReviewMember(t, k, f.ctx, accurate, "500.0")
	mkReviewMember(t, k, f.ctx, sloppy, "500.0")
	mkScoredReviewer(t, f, accurate, 9, 1) // 0.90, clears the 0.70 bar
	mkScoredReviewer(t, f, sloppy, 1, 9)   // 0.10, does not

	ledger := installLedger(f)
	fundReviewerPool(f, ledger, math.NewInt(1_000_000))

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	// Land exactly on a distribution boundary.
	ctx := f.ctx.WithBlockHeight(int64(params.ReviewerRewardEpochBlocks))
	require.True(t, k.IsReviewerRewardEpoch(ctx))

	denom := k.BondDenom(ctx)
	before := f.bankKeeper.GetBalance(ctx, accurate, denom).Amount

	require.NoError(t, k.DistributeReviewerRewards(ctx))

	require.True(t, f.bankKeeper.GetBalance(ctx, accurate, denom).Amount.GT(before),
		"a reviewer above the accuracy bar earns from the pool")
	require.True(t, f.bankKeeper.GetBalance(ctx, sloppy, denom).Amount.IsZero(),
		"a reviewer below min_reviewer_accuracy earns nothing")
}

func TestUncontestedReviewerEarnsNothingFromThePool(t *testing.T) {
	// Unchallenged work is not evidence of accuracy. Counting it would pay most
	// for reviewing whatever nobody bothers to contest — and the DREAM fee
	// already covers merely showing up.
	f := initFixture(t)
	k := f.keeper

	quiet := sdk.AccAddress([]byte("rw-quiet--------"))
	mkReviewMember(t, k, f.ctx, quiet, "500.0")
	mkScoredReviewer(t, f, quiet, 0, 0) // bonded, but no contested verdict

	ledger := installLedger(f)
	fundReviewerPool(f, ledger, math.NewInt(1_000_000))
	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	ctx := f.ctx.WithBlockHeight(int64(params.ReviewerRewardEpochBlocks))

	require.NoError(t, k.DistributeReviewerRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, quiet, k.BondDenom(ctx)).Amount.IsZero())
	require.Equal(t, math.NewInt(1_000_000), k.GetReviewerRewardPool(ctx),
		"with nobody eligible the pool is left intact, not burned or stranded")
}

func TestReviewerPoolIsSeparateFromTheSentinelPool(t *testing.T) {
	// Separate pools are the point of a separate role: a wrong approval mints
	// DREAM where a wrong hide costs a post some visibility, so neither may draw
	// on the other's funds.
	f := initFixture(t)
	ledger := installLedger(f)
	fundReviewerPool(f, ledger, math.NewInt(750_000))
	require.Equal(t, math.NewInt(750_000), f.keeper.GetReviewerRewardPool(f.ctx))
	require.True(t, f.keeper.GetSentinelRewardPool(f.ctx).IsZero(),
		"funding the reviewer pool must not touch the sentinel pool")
	require.NotEqual(t, keeper.ReviewerRewardPoolAddress().String(),
		keeper.SentinelRewardPoolAddress().String())
}

func TestReviewerPoolOverflowIsBurned(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	cap := params.MaxReviewerRewardPool
	// Twice the cap: half the overflow burns at the default 0.5 ratio.
	ledger := installLedger(f)
	fundReviewerPool(f, ledger, cap.MulRaw(2))

	require.NoError(t, k.BurnReviewerRewardPoolOverflow(f.ctx))

	after := k.GetReviewerRewardPool(f.ctx)
	expected := cap.MulRaw(2).Sub(cap.Quo(math.NewInt(2)))
	require.Equal(t, expected.String(), after.String(),
		"half the overflow burns; the rest stays to be distributed")
}

// The accuracy ring is stamped with an epoch number when a verdict resolves and
// read back as a window at distribution time. Those two numbers have to be in
// the same units. Reviewers and sentinels have independently committee-editable
// cadences, so "the same units" is not something the defaults can be trusted to
// keep true.
func TestReviewerAccuracyRingSurvivesADivergentCadence(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	// A committee shortens the reviewer cadence without touching the sentinel
	// one. Nothing about reviewer pay should depend on the sentinel dial.
	params.SentinelRewardEpochBlocks = 400
	params.ReviewerRewardEpochBlocks = 100
	require.NoError(t, k.Params.Set(f.ctx, params))

	reviewer := sdk.AccAddress([]byte("rw-cadence------"))
	mkReviewMember(t, k, f.ctx, reviewer, "500.0")

	// Verdicts resolve well into the chain's life, not at height 0, so the two
	// epoch counters have had a chance to diverge.
	recordCtx := f.ctx.WithBlockHeight(1000)
	bondReviewer(t, k, recordCtx, reviewer, 100_000_000)
	for i := 0; i < 9; i++ {
		require.NoError(t, k.RecordRoleOutcome(recordCtx,
			types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, reviewer.String(), "review", true))
	}
	require.NoError(t, k.RecordRoleOutcome(recordCtx,
		types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, reviewer.String(), "review", false))

	// Distribution reads the window in reviewer-epoch units. With the ring
	// stamped in sentinel-epoch units the stamps land outside the window and
	// this reviewer looks like they have never been challenged.
	upheld, overturned := k.GetRoleWindowedAccuracy(recordCtx,
		types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, reviewer.String(),
		k.CurrentReviewerRewardEpoch(recordCtx), params.ReviewerAccuracyWindowEpochs)
	require.Equal(t, uint64(9), upheld, "verdicts must be visible in the reviewer's own epoch window")
	require.Equal(t, uint64(1), overturned)

	ledger := installLedger(f)
	fundReviewerPool(f, ledger, math.NewInt(1_000_000))
	payCtx := recordCtx.WithBlockHeight(1000)
	require.True(t, k.IsReviewerRewardEpoch(payCtx))
	require.NoError(t, k.DistributeReviewerRewards(payCtx))
	require.True(t, f.bankKeeper.GetBalance(payCtx, reviewer, k.BondDenom(payCtx)).Amount.IsPositive(),
		"a 0.90-accuracy reviewer must be paid regardless of the sentinel cadence")
}

func TestReviewerPoolPaysOnlyBondedNormalReviewers(t *testing.T) {
	// The pool pays for liability actually standing behind new verdicts.
	// RECOVERY, UNBONDING and DEMOTED all mean it is not.
	f := initFixture(t)
	k := f.keeper

	normal := sdk.AccAddress([]byte("rw-normal-------"))
	recovering := sdk.AccAddress([]byte("rw-recovering---"))
	unbonding := sdk.AccAddress([]byte("rw-unbonding----"))
	for _, a := range []sdk.AccAddress{normal, recovering, unbonding} {
		mkReviewMember(t, k, f.ctx, a, "500.0")
		mkScoredReviewer(t, f, a, 9, 1) // identical, excellent records
	}
	require.NoError(t, k.SetBondStatus(f.ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER,
		recovering.String(), types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY, 0))
	require.NoError(t, k.SetBondStatus(f.ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER,
		unbonding.String(), types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING, 0))

	ledger := installLedger(f)
	fundReviewerPool(f, ledger, math.NewInt(1_000_000))
	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	ctx := f.ctx.WithBlockHeight(int64(params.ReviewerRewardEpochBlocks))

	require.NoError(t, k.DistributeReviewerRewards(ctx))

	denom := k.BondDenom(ctx)
	require.True(t, f.bankKeeper.GetBalance(ctx, normal, denom).Amount.IsPositive())
	require.True(t, f.bankKeeper.GetBalance(ctx, recovering, denom).Amount.IsZero(),
		"RECOVERY is not backing new verdicts and must not draw from the pool")
	require.True(t, f.bankKeeper.GetBalance(ctx, unbonding, denom).Amount.IsZero(),
		"a reviewer on the way out must not draw from the pool")
	// The sole eligible reviewer takes the whole pool, so nothing is stranded.
	require.Equal(t, math.NewInt(1_000_000), f.bankKeeper.GetBalance(ctx, normal, denom).Amount)
}

func TestReviewerPoolDampsVolumeBySquareRoot(t *testing.T) {
	// Volume should count, but sub-linearly, or the pool concentrates on
	// whoever files most rather than on whoever files well.
	f := initFixture(t)
	k := f.keeper

	prolific := sdk.AccAddress([]byte("rw-prolific-----"))
	selective := sdk.AccAddress([]byte("rw-selective----"))
	mkReviewMember(t, k, f.ctx, prolific, "500.0")
	mkReviewMember(t, k, f.ctx, selective, "500.0")
	// Same accuracy (1.0), 16x the volume. Linear weighting would pay 16:1.
	mkScoredReviewer(t, f, prolific, 16, 0)
	mkScoredReviewer(t, f, selective, 1, 0)

	ledger := installLedger(f)
	fundReviewerPool(f, ledger, math.NewInt(1_000_000))
	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	ctx := f.ctx.WithBlockHeight(int64(params.ReviewerRewardEpochBlocks))

	require.NoError(t, k.DistributeReviewerRewards(ctx))

	denom := k.BondDenom(ctx)
	big := f.bankKeeper.GetBalance(ctx, prolific, denom).Amount
	small := f.bankKeeper.GetBalance(ctx, selective, denom).Amount
	require.True(t, big.GT(small), "more decided verdicts must still earn more")
	// sqrt(16):sqrt(1) = 4:1, not 16:1.
	require.Equal(t, math.NewInt(800_000), big)
	require.Equal(t, math.NewInt(200_000), small)
}

func TestReviewerEpochCountersResetForIneligibleReviewersToo(t *testing.T) {
	// An ineligible reviewer that keeps its epoch counters carries stale
	// activity into the next window.
	f := initFixture(t)
	k := f.keeper

	sloppy := sdk.AccAddress([]byte("rw-stale--------"))
	mkReviewMember(t, k, f.ctx, sloppy, "500.0")
	mkScoredReviewer(t, f, sloppy, 1, 9) // below the accuracy bar; earns nothing

	ra, err := k.GetRoleActivity(f.ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, sloppy.String())
	require.NoError(t, err)
	require.Equal(t, uint64(10), ra.EpochAppealsResolved)

	ledger := installLedger(f)
	fundReviewerPool(f, ledger, math.NewInt(1_000_000))
	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	ctx := f.ctx.WithBlockHeight(int64(params.ReviewerRewardEpochBlocks))
	require.NoError(t, k.DistributeReviewerRewards(ctx))

	ra, err = k.GetRoleActivity(ctx, types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, sloppy.String())
	require.NoError(t, err)
	require.Zero(t, ra.EpochAppealsResolved, "epoch counters reset even for a reviewer that earned nothing")
	// The rolling ring is lifetime state and must survive the reset, or the
	// accuracy bar would forget every overturn at each epoch boundary.
	upheld, overturned := k.GetRoleWindowedAccuracy(ctx,
		types.RoleType_ROLE_TYPE_INITIATIVE_REVIEWER, sloppy.String(),
		k.CurrentReviewerRewardEpoch(ctx), params.ReviewerAccuracyWindowEpochs)
	require.Equal(t, uint64(10), upheld+overturned, "the accuracy ring is not per-epoch state")
}

func TestReviewerRewardsOnlyRunOnEpochBoundaries(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	reviewer := sdk.AccAddress([]byte("rw-offepoch-----"))
	mkReviewMember(t, k, f.ctx, reviewer, "500.0")
	mkScoredReviewer(t, f, reviewer, 9, 1)

	ledger := installLedger(f)
	fundReviewerPool(f, ledger, math.NewInt(1_000_000))
	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)

	// One block short of the boundary: nothing moves.
	ctx := f.ctx.WithBlockHeight(int64(params.ReviewerRewardEpochBlocks) - 1)
	require.False(t, k.IsReviewerRewardEpoch(ctx))
	require.NoError(t, k.DistributeReviewerRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, reviewer, k.BondDenom(ctx)).Amount.IsZero())
	require.Equal(t, math.NewInt(1_000_000), k.GetReviewerRewardPool(ctx))

	// On the boundary it pays.
	ctx = f.ctx.WithBlockHeight(int64(params.ReviewerRewardEpochBlocks))
	require.NoError(t, k.DistributeReviewerRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, reviewer, k.BondDenom(ctx)).Amount.IsPositive())
}
