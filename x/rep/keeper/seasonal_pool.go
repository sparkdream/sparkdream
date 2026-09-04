package keeper

import (
	"context"
	"errors"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ---------------------------------------------------------------------------
// Seasonal staking reward pool — MasterChef-style accumulator helpers
// ---------------------------------------------------------------------------

// getSeasonalPoolAccPerShare reads the accumulated reward per share from the store.
// Returns zero if the value has not been set.
func (k Keeper) getSeasonalPoolAccPerShare(ctx context.Context) (math.LegacyDec, error) {
	str, err := k.SeasonalPoolAccPerShare.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.LegacyZeroDec(), nil
		}
		return math.LegacyDec{}, err
	}
	dec, err := math.LegacyNewDecFromStr(str)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("invalid seasonal pool acc_per_share %q: %w", str, err)
	}
	return dec, nil
}

// getSeasonalPoolRemaining reads the remaining DREAM in the seasonal pool.
// Returns zero if the value has not been set.
func (k Keeper) getSeasonalPoolRemaining(ctx context.Context) (math.Int, error) {
	str, err := k.SeasonalPoolRemaining.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	val, ok := math.NewIntFromString(str)
	if !ok {
		return math.Int{}, fmt.Errorf("invalid seasonal pool remaining %q", str)
	}
	return val, nil
}

// getSeasonalPoolTotalStaked reads the total DREAM staked across all initiatives
// and projects. Returns zero if the value has not been set.
func (k Keeper) getSeasonalPoolTotalStaked(ctx context.Context) (math.Int, error) {
	str, err := k.SeasonalPoolTotalStaked.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	val, ok := math.NewIntFromString(str)
	if !ok {
		return math.Int{}, fmt.Errorf("invalid seasonal pool total_staked %q", str)
	}
	return val, nil
}

// GetSeasonalPoolTotalStaked returns the total DREAM staked across all
// initiatives and projects — the denominator the seasonal pool divides each
// epoch's reward slice by.
func (k Keeper) GetSeasonalPoolTotalStaked(ctx context.Context) (math.Int, error) {
	return k.getSeasonalPoolTotalStaked(ctx)
}

// GetSeasonalPoolAccPerShare returns the seasonal pool's accumulated reward per
// share. Monotonic for the life of the chain; see InitSeasonalPool.
func (k Keeper) GetSeasonalPoolAccPerShare(ctx context.Context) (math.LegacyDec, error) {
	return k.getSeasonalPoolAccPerShare(ctx)
}

// GetSeasonalPoolRemaining returns the DREAM left in this season's staking
// reward budget.
func (k Keeper) GetSeasonalPoolRemaining(ctx context.Context) (math.Int, error) {
	return k.getSeasonalPoolRemaining(ctx)
}

// setSeasonalPoolAccPerShare persists the accumulated reward per share.
func (k Keeper) setSeasonalPoolAccPerShare(ctx context.Context, val math.LegacyDec) error {
	return k.SeasonalPoolAccPerShare.Set(ctx, val.String())
}

// setSeasonalPoolRemaining persists the remaining DREAM in the seasonal pool.
func (k Keeper) setSeasonalPoolRemaining(ctx context.Context, val math.Int) error {
	return k.SeasonalPoolRemaining.Set(ctx, val.String())
}

// setSeasonalPoolTotalStaked persists the total DREAM staked.
func (k Keeper) setSeasonalPoolTotalStaked(ctx context.Context, val math.Int) error {
	return k.SeasonalPoolTotalStaked.Set(ctx, val.String())
}

// getSeasonalPoolStartEpoch reads the epoch at which the current pool was
// initialized. Returns zero if the value has not been set — which reads as
// "initialized at genesis" and matches the height-modulo behaviour this
// anchor replaced, so state written before the field existed drains on the
// schedule it was always assumed to have.
func (k Keeper) getSeasonalPoolStartEpoch(ctx context.Context) (uint64, error) {
	return k.SeasonalPoolStartEpoch.Get(ctx)
}

// UpdateSeasonalPoolTotalStaked adds delta to the total staked amount.
// delta may be negative (e.g. when a user unstakes). Called from every site
// that mutates an INITIATIVE or PROJECT stake amount, via updateStakePoolTotals.
func (k Keeper) UpdateSeasonalPoolTotalStaked(ctx context.Context, delta math.Int) error {
	current, err := k.getSeasonalPoolTotalStaked(ctx)
	if err != nil {
		return err
	}
	return k.setSeasonalPoolTotalStaked(ctx, clampPoolTotal(ctx, "seasonal", current, delta))
}

// InitSeasonalPool initialises the seasonal staking reward pool for a new
// season. It sizes the season's budget from the outgoing season's production
// (see seasonalPoolBudget), records the season number, and resets the
// per-season economic counters.
//
// The accumulator is deliberately NOT reset. accPerShare is monotonic for the
// life of the chain, and each stake's reward_debt is a snapshot of it taken at
// join time. Zeroing the accumulator at a season boundary would leave every
// surviving stake holding a debt larger than the accumulator it is measured
// against, so `amount * accPerShare - reward_debt` would clamp to zero until
// the new season's accumulator climbed back past the old value — silently
// paying nothing to anyone who held across the rollover. Per-season budgeting
// is enforced by SeasonalPoolRemaining alone.
//
// Unspent budget carries over. The yield cap withholds a slice whenever the
// staked base is thin, and that withheld DREAM used to be discarded here by
// the plain overwrite below — punishing exactly the seasons that opened too
// quiet to drain their pool, and making the cap's "pays out later if staking
// picks up" promise true only within a single season. The carried remainder
// is added to the new budget and the total is re-capped at
// MaxStakingRewardsPerSeason, which stays the hard bound on what one season
// can ever pay out. Carryover is not new emission: the carried DREAM was
// budgeted in a prior season and only becomes supply when distributed.
//
// The pool's start epoch is recorded here as the drain-schedule anchor; see
// DistributeEpochStakingRewardsFromPool.
func (k Keeper) InitSeasonalPool(ctx context.Context, season uint64) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	// Sized BEFORE the counter resets below: seasonalPoolBudget reads them for
	// the outgoing season's production.
	budget, err := k.seasonalPoolBudget(ctx, params, season)
	if err != nil {
		return fmt.Errorf("failed to size seasonal pool: %w", err)
	}

	// Carry whatever the outgoing season failed to distribute, floored at
	// zero (a negative remainder would be an accounting bug, not a debt to
	// collect from the new season).
	leftover, err := k.getSeasonalPoolRemaining(ctx)
	if err != nil {
		return fmt.Errorf("failed to read seasonal pool remaining: %w", err)
	}
	if leftover.IsNegative() {
		leftover = math.ZeroInt()
	}
	total := budget.Add(leftover)
	if ceiling := params.MaxStakingRewardsPerSeason; !ceiling.IsNil() && total.GT(ceiling) {
		total = ceiling
	}
	if err := k.setSeasonalPoolRemaining(ctx, total); err != nil {
		return fmt.Errorf("failed to set seasonal pool remaining: %w", err)
	}
	if err := k.SeasonalPoolSeason.Set(ctx, season); err != nil {
		return fmt.Errorf("failed to set seasonal pool season: %w", err)
	}

	// Anchor the drain schedule to the epoch this pool actually opened at.
	// InitSeasonalPool is invoked by x/season's transition (and by genesis),
	// so the anchor tracks the real season boundary rather than an assumption
	// that boundaries fall on multiples of SeasonDurationEpochs from height 0.
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return fmt.Errorf("failed to compute current epoch for pool anchor: %w", err)
	}
	if err := k.SeasonalPoolStartEpoch.Set(ctx, uint64(currentEpoch)); err != nil {
		return fmt.Errorf("failed to set seasonal pool start epoch: %w", err)
	}

	// Reset per-season economic counters.
	if err := k.SeasonMinted.Set(ctx, "0"); err != nil {
		return fmt.Errorf("failed to reset season minted: %w", err)
	}
	if err := k.SeasonBurned.Set(ctx, "0"); err != nil {
		return fmt.Errorf("failed to reset season burned: %w", err)
	}
	if err := k.SeasonInitiativeRewardsMinted.Set(ctx, "0"); err != nil {
		return fmt.Errorf("failed to reset season initiative rewards minted: %w", err)
	}
	if err := k.SeasonStakingRewardsMinted.Set(ctx, "0"); err != nil {
		return fmt.Errorf("failed to reset season staking rewards minted: %w", err)
	}
	if err := k.SeasonInterimRewardsMinted.Set(ctx, "0"); err != nil {
		return fmt.Errorf("failed to reset season interim rewards minted: %w", err)
	}
	if err := k.SeasonTreasuryInflow.Set(ctx, "0"); err != nil {
		return fmt.Errorf("failed to reset season treasury inflow: %w", err)
	}
	if err := k.SeasonTreasuryOutflow.Set(ctx, "0"); err != nil {
		return fmt.Errorf("failed to reset season treasury outflow: %w", err)
	}

	return nil
}

// DistributeEpochStakingRewardsFromPool is called once per epoch. It computes
// the epoch's reward slice from the remaining pool, increments the global
// accPerShare accumulator, and decrements the remaining pool balance.
//
// Algorithm:
//
//	epochSlice = remaining / remainingEpochs
//	accPerShare += epochSlice / totalStaked   (if totalStaked > 0)
//	remaining  -= epochSlice
func (k Keeper) DistributeEpochStakingRewardsFromPool(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	remaining, err := k.getSeasonalPoolRemaining(ctx)
	if err != nil {
		return fmt.Errorf("failed to get seasonal pool remaining: %w", err)
	}
	if remaining.IsZero() {
		return nil // nothing left to distribute
	}

	totalStaked, err := k.getSeasonalPoolTotalStaked(ctx)
	if err != nil {
		return fmt.Errorf("failed to get seasonal pool total staked: %w", err)
	}

	// Determine how many epochs remain in the season so that the budget is
	// spread evenly across the rest of the season.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if params.EpochBlocks <= 0 {
		return fmt.Errorf("epoch_blocks must be positive, got %d", params.EpochBlocks)
	}
	currentEpoch := sdkCtx.BlockHeight() / params.EpochBlocks

	// The drain schedule is anchored to the epoch this pool was initialized
	// at (set by InitSeasonalPool), not to a modulo of the current epoch.
	// Seasons are driven by x/season, whose SeasonDurationEpochs is a separate
	// param from this module's; the two are expected to hold the same value
	// but nothing enforces it across two governance surfaces, and the modulo
	// silently assumed boundaries land on multiples of THIS module's duration
	// counted from height zero. Anchoring to the stored start epoch makes the
	// schedule follow wherever the boundary actually fell, and it re-anchors
	// at every rollover. A start epoch in the future (imported state, clock
	// oddities) reads as elapsed 0.
	startEpoch, err := k.getSeasonalPoolStartEpoch(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("failed to get seasonal pool start epoch: %w", err)
	}
	elapsed := int64(0)
	if currentEpoch > int64(startEpoch) {
		elapsed = currentEpoch - int64(startEpoch)
	}

	// SeasonDurationEpochs is the total number of epochs in a season.
	// remainingEpochs is at least 1 to avoid division by zero; if we are
	// past the expected season end, dump whatever is left this epoch.
	remainingEpochs := params.SeasonDurationEpochs - elapsed
	if remainingEpochs <= 0 {
		remainingEpochs = 1
	}

	// epochSlice = remaining / remainingEpochs  (integer division)
	epochSlice := remaining.Quo(math.NewInt(remainingEpochs))
	if epochSlice.IsZero() {
		// Pool nearly exhausted — distribute whatever dust remains.
		epochSlice = remaining
	}

	// Nothing staked: the slice has no denominator to divide by, so leave it in
	// the pool. It used to be subtracted from `remaining` and lost, which
	// silently shrank the budget of any season that opened before anyone staked.
	if !totalStaked.IsPositive() {
		return nil
	}

	// Cap the slice at a yield on the DREAM actually staked.
	//
	// Spread over the calendar alone, yield per staked DREAM is
	// pool/total_staked, which diverges as the staked base shrinks: with 1.47
	// DREAM staked chain-wide the entire season budget accrues to three dust
	// stakes, acc_per_share climbs without bound, and settling any one of them
	// eventually exceeds max_dream_mint_per_epoch — at which point every payout
	// path that settles a stake reverts, including initiative completion, and
	// the stake can no longer even be withdrawn.
	//
	// What the cap withholds stays in `remaining`, so a season that opens quiet
	// can still pay out in full if staking picks up before it closes.
	if yield := params.StakingRewardYieldPerEpoch; !yield.IsNil() && yield.IsPositive() {
		if capped := yield.MulInt(totalStaked).TruncateInt(); capped.LT(epochSlice) {
			epochSlice = capped
		}
	}
	if !epochSlice.IsPositive() {
		return nil
	}

	accPerShare, err := k.getSeasonalPoolAccPerShare(ctx)
	if err != nil {
		return fmt.Errorf("failed to get seasonal pool acc_per_share: %w", err)
	}
	// increment = epochSlice / totalStaked  (precise decimal division)
	increment := math.LegacyNewDecFromInt(epochSlice).Quo(math.LegacyNewDecFromInt(totalStaked))
	accPerShare = accPerShare.Add(increment)
	if err := k.setSeasonalPoolAccPerShare(ctx, accPerShare); err != nil {
		return fmt.Errorf("failed to update seasonal pool acc_per_share: %w", err)
	}

	// Decrement the pool by what was actually distributed, never by more.
	remaining = remaining.Sub(epochSlice)
	if err := k.setSeasonalPoolRemaining(ctx, remaining); err != nil {
		return fmt.Errorf("failed to update seasonal pool remaining: %w", err)
	}

	return nil
}

// RebaseStakeRewardDebt re-measures every initiative and project stake's
// reward_debt against the live seasonal accumulator, so that each stake starts
// from zero pending rather than from a debt taken against some other
// accumulator. Returns the number of stakes rewritten.
//
// Called from InitGenesis, because an import is exactly where the two halves of
// the MasterChef pair come apart. Stakes are exported verbatim, reward_debt
// included, but the accumulator is derived state that genesis does not carry at
// all — so an imported chain resumes with acc_per_share back at zero while every
// stake still holds the debt it accrued against the old one. Two things follow,
// and this fixes both:
//
//   - A stake with a positive debt earns nothing until the new accumulator
//     climbs back past its stale figure, silently, for as long as that takes.
//     This is the same failure InitSeasonalPool refuses to cause at a season
//     boundary; an import caused it anyway.
//   - A stake whose debt is zero because it predates any distribution keeps
//     claiming from zero against whatever the accumulator later becomes. On a
//     chain whose staked base is small enough for the accumulator to run far
//     ahead of it, that is the difference between a stake worth its principal
//     and one owed more DREAM than max_dream_mint_per_epoch will ever mint —
//     at which point settling it reverts, and every path that settles it
//     (completion, claim, unstake) reverts with it.
//
// Rebasing forfeits pending rewards that the reset accumulator had already made
// unclaimable; it does not take anything an import would otherwise have paid.
func (k Keeper) RebaseStakeRewardDebt(ctx context.Context) (int, error) {
	accPerShare, err := k.getSeasonalPoolAccPerShare(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get seasonal pool acc_per_share: %w", err)
	}

	type rebased struct {
		id   uint64
		debt math.Int
	}
	var pending []rebased

	if err := k.Stake.Walk(ctx, nil, func(id uint64, stake types.Stake) (bool, error) {
		switch stake.TargetType {
		case types.StakeTargetType_STAKE_TARGET_INITIATIVE,
			types.StakeTargetType_STAKE_TARGET_PROJECT:
		default:
			// Member and tag stakes draw on their own per-pool accumulators,
			// which ARE exported and imported with the pool records
			// (MemberStakePoolList / TagStakePoolList) — so their debts stay
			// measured against the accumulator they were taken from and need no
			// rebase. That was asserted here before the export existed, which
			// made this skip a silent forfeiture: the pools reset to zero on
			// import while the stakes kept their debts, so pending rewards
			// clamped to zero until the fresh accumulator climbed back past a
			// stale figure. Content conviction and author bonds have no
			// accumulator at all.
			return false, nil
		}
		if stake.Amount.IsNil() || !stake.Amount.IsPositive() {
			return false, nil
		}
		debt := math.LegacyNewDecFromInt(stake.Amount).Mul(accPerShare).TruncateInt()
		if !stakeRewardDebt(stake).Equal(debt) {
			pending = append(pending, rebased{id: id, debt: debt})
		}
		return false, nil
	}); err != nil {
		return 0, fmt.Errorf("failed to walk stakes for reward debt rebase: %w", err)
	}

	// Collected first, then rewritten: writing values inside the walk would
	// also be safe — only values change, never the walked key range — but a
	// read-only pass plus a key-addressed write phase needs no reasoning
	// about iterator semantics to audit.
	for _, r := range pending {
		stake, err := k.Stake.Get(ctx, r.id)
		if err != nil {
			return 0, fmt.Errorf("failed to load stake %d for reward debt rebase: %w", r.id, err)
		}
		stake.RewardDebt = r.debt
		if err := k.Stake.Set(ctx, r.id, stake); err != nil {
			return 0, fmt.Errorf("failed to rebase reward debt on stake %d: %w", r.id, err)
		}
	}
	return len(pending), nil
}

// seasonalPoolBudget sizes the staking reward budget for the incoming season:
//
//	activity = staking_pool_mint_share * (season_minted - season_staking_rewards_minted)
//	schedule = staking_pool_cap_base * (season + 1) * staking_pool_cap_rate
//	budget   = min(activity, schedule, max_staking_rewards_per_season)
//
// The per-season counters still hold the OUTGOING season's totals when this
// runs — InitSeasonalPool resets them immediately afterwards — so `activity` is
// last season's production, known and fixed at the moment the budget has to be
// set. Sizing from the incoming season's own mints is not available: the epoch
// slice is remaining/remaining_epochs from its first epoch onward, and a figure
// that is only complete at a season's end cannot fund the season it measures.
//
// Staking rewards are subtracted from the base so a season's emission cannot
// fund the next season's; see TrackStakingRewardMint.
//
// A chain with no history at all — genesis, where nothing has been minted yet —
// falls back to the schedule ceiling so the first season is not stillborn. That
// is safe in a way it would not have been before the per-epoch yield cap: an
// oversized `remaining` now simply goes unspent rather than being force-fed to
// whoever happens to be staked.
func (k Keeper) seasonalPoolBudget(ctx context.Context, params types.Params, season uint64) (math.Int, error) {
	ceiling := params.MaxStakingRewardsPerSeason
	if ceiling.IsNil() || !ceiling.IsPositive() {
		return math.ZeroInt(), nil
	}

	// Unreachable through the shipped writers: AppModule.ValidateGenesis runs
	// Params.Validate (which rejects nils) before InitGenesis in the standard
	// flow, and both param-update msg servers validate before Set. Keeper
	// InitGenesis itself does not re-validate, so a direct caller with
	// pre-rule params (hand-built test genesis, future tooling) can still land
	// here; degrade to the old refill-to-the-ceiling behaviour rather than
	// reading a nil as a deliberate zero and silently ending staking rewards.
	if params.StakingPoolMintShare.IsNil() || params.StakingPoolCapBase.IsNil() ||
		params.StakingPoolCapRate.IsNil() {
		return ceiling, nil
	}

	budget := ceiling
	seasonFactor := math.NewIntFromUint64(season).AddRaw(1)
	if scheduleCap := params.StakingPoolCapRate.
		MulInt(params.StakingPoolCapBase).
		MulInt(seasonFactor).
		TruncateInt(); scheduleCap.LT(budget) {
		budget = scheduleCap
	}

	minted, err := k.GetSeasonMinted(ctx)
	if err != nil {
		return math.Int{}, err
	}
	if minted.IsZero() {
		// No history to size against; see the fallback note above.
		return budget, nil
	}
	stakingMinted, err := k.GetSeasonStakingRewardsMinted(ctx)
	if err != nil {
		return math.Int{}, err
	}
	base := minted.Sub(stakingMinted)
	if base.IsNegative() {
		base = math.ZeroInt()
	}
	if activity := params.StakingPoolMintShare.MulInt(base).TruncateInt(); activity.LT(budget) {
		budget = activity
	}
	return budget, nil
}
