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
// Stake reward settlement — the single choke point for MasterChef accounting
// ---------------------------------------------------------------------------
//
// Every site that mutates a reward-bearing stake's amount, or pays it out, goes
// through settleStake. Reward accounting is dispatched on target type in
// exactly one place, so a flow can no longer silently cover a subset of the
// types (which is how initiative and project stakes ended up with a permanently
// zero reward_debt while member and tag stakes were maintained correctly).
//
// The accumulators are:
//
//	INITIATIVE, PROJECT -> SeasonalPoolAccPerShare (one shared seasonal pool)
//	MEMBER              -> MemberStakePool.AccRewardPerShare (per target member)
//	TAG                 -> TagStakePool.AccRewardPerShare    (per target tag)
//
// Content conviction and author bond stakes earn no DREAM and are not settled.

// stakeAccumulator returns the reward-per-share accumulator that governs the
// given stake. The second return value is false for target types that earn no
// DREAM (content conviction, author bonds), in which case the accumulator is
// meaningless and callers must not settle.
//
// A missing pool is reported as a zero accumulator rather than an error: the
// pool is created lazily on first stake, and a stake placed before any revenue
// has ever accrued legitimately has nothing to settle.
func (k Keeper) stakeAccumulator(ctx context.Context, stake types.Stake) (math.LegacyDec, bool, error) {
	switch stake.TargetType {
	case types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		types.StakeTargetType_STAKE_TARGET_PROJECT:
		acc, err := k.getSeasonalPoolAccPerShare(ctx)
		if err != nil {
			return math.LegacyZeroDec(), false, err
		}
		return acc, true, nil

	case types.StakeTargetType_STAKE_TARGET_MEMBER:
		addr, err := sdk.AccAddressFromBech32(stake.TargetIdentifier)
		if err != nil {
			return math.LegacyZeroDec(), false, fmt.Errorf("invalid member stake target %q: %w", stake.TargetIdentifier, err)
		}
		pool, err := k.GetMemberStakePool(ctx, addr)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return math.LegacyZeroDec(), true, nil
			}
			return math.LegacyZeroDec(), false, err
		}
		return pool.AccRewardPerShare, true, nil

	case types.StakeTargetType_STAKE_TARGET_TAG:
		pool, err := k.GetTagStakePool(ctx, stake.TargetIdentifier)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return math.LegacyZeroDec(), true, nil
			}
			return math.LegacyZeroDec(), false, err
		}
		return pool.AccRewardPerShare, true, nil
	}

	return math.LegacyZeroDec(), false, nil
}

// stakeAccruing reports whether the stake is currently earning from its pool.
// Only project stakes can be frozen: a project that is no longer ACTIVE stops
// accruing, matching the long-standing behaviour of getPendingProjectRewards.
func (k Keeper) stakeAccruing(ctx context.Context, stake types.Stake) (bool, error) {
	if stake.TargetType != types.StakeTargetType_STAKE_TARGET_PROJECT {
		return true, nil
	}
	project, err := k.GetProject(ctx, stake.TargetId)
	if err != nil {
		return false, err
	}
	return project.Status == types.ProjectStatus_PROJECT_STATUS_ACTIVE, nil
}

// stakeRewardDebt reads a stake's reward debt, normalising the nil Int that
// stakes restored from older genesis exports can carry.
func stakeRewardDebt(stake types.Stake) math.Int {
	if stake.RewardDebt.IsNil() {
		return math.ZeroInt()
	}
	return stake.RewardDebt
}

// pendingAgainst applies the MasterChef formula:
//
//	pending = amount * accPerShare - rewardDebt
//
// clamped at zero. A negative result means the debt was taken against a larger
// accumulator than the current one, which can only happen through a rebase we
// have already paid for.
func pendingAgainst(amount, rewardDebt math.Int, accPerShare math.LegacyDec) math.Int {
	gross := math.LegacyNewDecFromInt(amount).Mul(accPerShare).TruncateInt()
	pending := gross.Sub(rewardDebt)
	if pending.IsNegative() {
		return math.ZeroInt()
	}
	return pending
}

// stakeSettlement reports what settleStake did.
type stakeSettlement struct {
	// Pending is what the stake had accrued at its pre-settlement amount.
	Pending math.Int
	// Minted is what was actually paid to the staker — zero when the rewards
	// were forfeited for withdrawing before MinStakeDurationSeconds.
	Minted math.Int
	// Forfeited is true when Pending was positive but nothing was minted.
	Forfeited bool
}

// settleStake harvests everything the stake has accrued at its current amount,
// mints it to the staker, and rebases reward_debt so the stake starts earning
// again from zero at `newAmount`.
//
// `newAmount` is the amount the caller is about to write back: unchanged for a
// claim, grown for a compound, reduced for a partial withdrawal, and zero for a
// full withdrawal or a completion payout. Rebasing against the caller's target
// amount (rather than the stake's current one) is what keeps a shrunken stake
// from carrying a debt sized for its original principal.
//
// When `forfeit` is set the pending amount is computed and the debt is rebased,
// but nothing is minted — the DREAM simply stays in the pool. This is the
// early-withdrawal penalty; see MinStakeDurationSeconds in RemoveStake.
//
// The returned stake is NOT persisted; the caller writes it back (or deletes
// it) as part of its own state transition.
func (k Keeper) settleStake(
	ctx context.Context,
	stake types.Stake,
	newAmount math.Int,
	forfeit bool,
) (types.Stake, stakeSettlement, error) {
	empty := stakeSettlement{Pending: math.ZeroInt(), Minted: math.ZeroInt()}

	accPerShare, rewardBearing, err := k.stakeAccumulator(ctx, stake)
	if err != nil {
		return stake, empty, err
	}
	if !rewardBearing {
		// Content conviction and author bond stakes have no accumulator; keep
		// their debt at zero so it can never be mistaken for a real baseline.
		stake.RewardDebt = math.ZeroInt()
		return stake, empty, nil
	}

	accruing, err := k.stakeAccruing(ctx, stake)
	if err != nil {
		return stake, empty, err
	}
	if !accruing {
		// Accrual is frozen (non-ACTIVE project). Scale the existing debt to the
		// new principal instead of rebasing to the live accumulator, so a staker
		// who trims their position keeps a proportional claim on what they had
		// already earned rather than forfeiting it.
		stake.RewardDebt = scaleRewardDebt(stakeRewardDebt(stake), stake.Amount, newAmount)
		return stake, empty, nil
	}

	pending := pendingAgainst(stake.Amount, stakeRewardDebt(stake), accPerShare)
	stake.RewardDebt = math.LegacyNewDecFromInt(newAmount).Mul(accPerShare).TruncateInt()

	settlement := stakeSettlement{Pending: pending, Minted: math.ZeroInt()}
	if !pending.IsPositive() {
		return stake, settlement, nil
	}
	if forfeit {
		settlement.Forfeited = true
		return stake, settlement, nil
	}

	stakerAddr, err := sdk.AccAddressFromBech32(stake.Staker)
	if err != nil {
		return stake, empty, fmt.Errorf("invalid staker address %q: %w", stake.Staker, err)
	}
	if err := k.MintDREAM(ctx, stakerAddr, pending); err != nil {
		return stake, empty, fmt.Errorf("failed to mint staking reward for stake %d: %w", stake.Id, err)
	}
	settlement.Minted = pending

	return stake, settlement, nil
}

// scaleRewardDebt proportionally rescales a reward debt when a stake's
// principal changes without the debt being rebased against a live accumulator.
func scaleRewardDebt(debt, oldAmount, newAmount math.Int) math.Int {
	if newAmount.IsZero() || oldAmount.IsZero() || !debt.IsPositive() {
		return math.ZeroInt()
	}
	if newAmount.GTE(oldAmount) {
		return debt
	}
	return debt.Mul(newAmount).Quo(oldAmount)
}

// initialRewardDebt returns the join-time reward-debt baseline for a brand new
// stake of `amount` on the given target. Setting this at creation is what makes
// a new staker's pending balance start at zero rather than at the pool's entire
// accumulated history.
func (k Keeper) initialRewardDebt(ctx context.Context, stake types.Stake, amount math.Int) (math.Int, error) {
	accPerShare, rewardBearing, err := k.stakeAccumulator(ctx, stake)
	if err != nil {
		return math.ZeroInt(), err
	}
	if !rewardBearing {
		return math.ZeroInt(), nil
	}
	return math.LegacyNewDecFromInt(amount).Mul(accPerShare).TruncateInt(), nil
}

// stakeMeetsMinDuration reports whether the stake has been held for at least
// params.MinStakeDurationSeconds. Rewards are only collectable past this point;
// withdrawing earlier forfeits them.
func (k Keeper) stakeMeetsMinDuration(ctx context.Context, stake types.Stake) (bool, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false, err
	}
	if params.MinStakeDurationSeconds <= 0 {
		return true, nil
	}
	held := sdk.UnwrapSDKContext(ctx).BlockTime().Unix() - stake.CreatedAt
	return held >= params.MinStakeDurationSeconds, nil
}
