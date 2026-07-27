package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetPendingStakingRewards calculates pending rewards for any stake type (O(1)).
//
// This is the read-only twin of settleStake: both resolve the accumulator
// through stakeAccumulator and apply the same MasterChef formula, so a query
// can never report a figure the settlement path would not pay.
func (k Keeper) GetPendingStakingRewards(ctx context.Context, stake types.Stake) (math.Int, error) {
	if !types.IsRewardBearingType(stake.TargetType) {
		if types.IsContentOrBondType(stake.TargetType) {
			// Content conviction and author bond stakes earn no DREAM rewards
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), fmt.Errorf("unknown stake target type: %v", stake.TargetType)
	}

	accPerShare, rewardBearing, err := k.stakeAccumulator(ctx, stake)
	if err != nil {
		return math.ZeroInt(), err
	}
	if !rewardBearing {
		return math.ZeroInt(), nil
	}

	// Frozen targets (a project past ACTIVE) stop accruing.
	accruing, err := k.stakeAccruing(ctx, stake)
	if err != nil {
		return math.ZeroInt(), err
	}
	if !accruing {
		return math.ZeroInt(), nil
	}

	return pendingAgainst(stake.Amount, stakeRewardDebt(stake), accPerShare), nil
}

// ClaimStakingRewards claims pending rewards for a stake.
func (k Keeper) ClaimStakingRewards(ctx context.Context, stakeID uint64, stakerAddr sdk.AccAddress) (math.Int, error) {
	stake, err := k.GetStake(ctx, stakeID)
	if err != nil {
		return math.ZeroInt(), err
	}

	// Verify staker owns the stake
	if stake.Staker != stakerAddr.String() {
		return math.ZeroInt(), fmt.Errorf("only stake owner can claim rewards")
	}

	if !types.IsRewardBearingType(stake.TargetType) {
		return math.ZeroInt(), nil
	}

	// Rewards only become collectable once the stake has been held for the
	// minimum duration. Nothing is forfeited here — the debt is left alone so
	// the rewards keep accruing until the staker is eligible.
	eligible, err := k.stakeMeetsMinDuration(ctx, stake)
	if err != nil {
		return math.ZeroInt(), err
	}
	if !eligible {
		return math.ZeroInt(), types.ErrMinStakeDuration
	}

	// Harvest and rebase the debt against the unchanged principal.
	stake, settlement, err := k.settleStake(ctx, stake, stake.Amount, false)
	if err != nil {
		return math.ZeroInt(), err
	}
	if settlement.Minted.IsZero() {
		return math.ZeroInt(), nil
	}

	stake.LastClaimedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
		return math.ZeroInt(), err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"staking_rewards_claimed",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("staker", stakerAddr.String()),
			sdk.NewAttribute("rewards", settlement.Minted.String()),
		),
	)

	return settlement.Minted, nil
}

// CompoundStakingRewards compounds pending rewards into stake principal.
//
// Only MEMBER and TAG stakes may compound. Initiative and project stakes are
// rejected: their principal carries a conviction maturity clock keyed on
// created_at, so growing the amount in place would hand the new DREAM full
// maturity instantly — exactly the exploit that CreateStake's separate-tranche
// design exists to prevent. Those stakers claim and re-stake instead, which
// routes the new DREAM through CreateStake's per-member cap and gives it a
// fresh maturity clock.
func (k Keeper) CompoundStakingRewards(ctx context.Context, stakeID uint64, stakerAddr sdk.AccAddress) (math.Int, error) {
	stake, err := k.GetStake(ctx, stakeID)
	if err != nil {
		return math.ZeroInt(), err
	}

	// Verify staker owns the stake
	if stake.Staker != stakerAddr.String() {
		return math.ZeroInt(), fmt.Errorf("only stake owner can compound rewards")
	}

	if !types.IsRewardBearingType(stake.TargetType) {
		return math.ZeroInt(), nil
	}
	if types.IsSeasonalPoolType(stake.TargetType) {
		return math.ZeroInt(), types.ErrCompoundNotSupported
	}

	eligible, err := k.stakeMeetsMinDuration(ctx, stake)
	if err != nil {
		return math.ZeroInt(), err
	}
	if !eligible {
		return math.ZeroInt(), types.ErrMinStakeDuration
	}

	// Peek at what is owed so the new principal can be passed to settleStake as
	// the rebase target in a single pass.
	pending, err := k.GetPendingStakingRewards(ctx, stake)
	if err != nil {
		return math.ZeroInt(), err
	}
	if !pending.IsPositive() {
		return math.ZeroInt(), nil
	}

	newAmount := stake.Amount.Add(pending)

	// settleStake mints the pending amount to the staker's balance, which is
	// what gives LockDREAM below the unlocked balance it needs.
	stake, settlement, err := k.settleStake(ctx, stake, newAmount, false)
	if err != nil {
		return math.ZeroInt(), err
	}
	if settlement.Minted.IsZero() {
		return math.ZeroInt(), nil
	}

	stake.Amount = newAmount
	stake.LastClaimedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	if err := k.LockDREAM(ctx, stakerAddr, settlement.Minted); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to lock compounded rewards: %w", err)
	}

	// The compounded principal must dilute the pool it earns from, or it would
	// draw rewards without appearing in the denominator.
	if err := k.updateStakePoolTotals(ctx, stake, settlement.Minted); err != nil {
		return math.ZeroInt(), err
	}

	if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
		return math.ZeroInt(), err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"staking_rewards_compounded",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("staker", stakerAddr.String()),
			sdk.NewAttribute("compounded", settlement.Minted.String()),
			sdk.NewAttribute("new_principal", stake.Amount.String()),
		),
	)

	return settlement.Minted, nil
}
