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

// GetPendingStakingRewards calculates pending rewards for any stake type (O(1))
func (k Keeper) GetPendingStakingRewards(ctx context.Context, stake types.Stake) (math.Int, error) {
	switch stake.TargetType {
	case types.StakeTargetType_STAKE_TARGET_INITIATIVE:
		return k.CalculateStakingReward(ctx, stake)
	case types.StakeTargetType_STAKE_TARGET_PROJECT:
		return k.getPendingProjectRewards(ctx, stake)
	case types.StakeTargetType_STAKE_TARGET_MEMBER:
		return k.getPendingMemberRewards(ctx, stake)
	case types.StakeTargetType_STAKE_TARGET_TAG:
		return k.getPendingTagRewards(ctx, stake)
	case types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND:
		// Content conviction and author bond stakes earn no DREAM rewards
		return math.ZeroInt(), nil
	}
	return math.ZeroInt(), fmt.Errorf("unknown stake target type: %v", stake.TargetType)
}

// getPendingProjectRewards calculates rewards from the seasonal pool for project stakes.
// Uses the same MasterChef accumulator as initiative stakes (shared seasonal pool).
func (k Keeper) getPendingProjectRewards(ctx context.Context, stake types.Stake) (math.Int, error) {
	// Get the project to check if it's still active
	project, err := k.GetProject(ctx, stake.TargetId)
	if err != nil {
		return math.ZeroInt(), err
	}

	// Only earn while project is ACTIVE
	if project.Status != types.ProjectStatus_PROJECT_STATUS_ACTIVE {
		return math.ZeroInt(), nil
	}

	// Same MasterChef formula as initiative stakes
	accPerShare, err := k.getSeasonalPoolAccPerShare(ctx)
	if err != nil {
		return math.ZeroInt(), nil
	}

	rewardDebt := stake.RewardDebt
	if rewardDebt.IsNil() {
		rewardDebt = math.ZeroInt()
	}
	gross := math.LegacyNewDecFromInt(stake.Amount).Mul(accPerShare).TruncateInt()
	pending := gross.Sub(rewardDebt)
	if pending.IsNegative() {
		return math.ZeroInt(), nil
	}
	return pending, nil
}

// getPendingMemberRewards calculates pending rewards from member stake pool
func (k Keeper) getPendingMemberRewards(ctx context.Context, stake types.Stake) (math.Int, error) {
	pool, err := k.GetMemberStakePool(ctx, sdk.MustAccAddressFromBech32(stake.TargetIdentifier))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	if pool.TotalStaked.IsZero() {
		return math.ZeroInt(), nil
	}

	// MasterChef formula: (stake.Amount * pool.AccRewardPerShare) - stake.RewardDebt
	pending := stake.Amount.ToLegacyDec().
		Mul(pool.AccRewardPerShare).
		TruncateInt().
		Sub(stake.RewardDebt)

	if pending.IsNegative() {
		return math.ZeroInt(), nil
	}

	return pending, nil
}

// getPendingTagRewards calculates pending rewards from tag stake pool
func (k Keeper) getPendingTagRewards(ctx context.Context, stake types.Stake) (math.Int, error) {
	pool, err := k.GetTagStakePool(ctx, stake.TargetIdentifier)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	if pool.TotalStaked.IsZero() {
		return math.ZeroInt(), nil
	}

	// MasterChef formula: (stake.Amount * pool.AccRewardPerShare) - stake.RewardDebt
	pending := stake.Amount.ToLegacyDec().
		Mul(pool.AccRewardPerShare).
		TruncateInt().
		Sub(stake.RewardDebt)

	if pending.IsNegative() {
		return math.ZeroInt(), nil
	}

	return pending, nil
}

// ClaimStakingRewards claims pending rewards for a stake
func (k Keeper) ClaimStakingRewards(ctx context.Context, stakeID uint64, stakerAddr sdk.AccAddress) (math.Int, error) {
	stake, err := k.GetStake(ctx, stakeID)
	if err != nil {
		return math.ZeroInt(), err
	}

	// Verify staker owns the stake
	if stake.Staker != stakerAddr.String() {
		return math.ZeroInt(), fmt.Errorf("only stake owner can claim rewards")
	}

	// Calculate pending rewards
	rewards, err := k.GetPendingStakingRewards(ctx, stake)
	if err != nil {
		return math.ZeroInt(), err
	}

	if rewards.IsZero() {
		return math.ZeroInt(), nil
	}

	// Mint rewards to staker
	if err := k.MintDREAM(ctx, stakerAddr, rewards); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to mint rewards: %w", err)
	}

	// Update stake's last claimed timestamp and reward debt
	stake.LastClaimedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	// Update reward debt for MasterChef-style targets
	if stake.TargetType == types.StakeTargetType_STAKE_TARGET_MEMBER {
		pool, err := k.GetMemberStakePool(ctx, sdk.MustAccAddressFromBech32(stake.TargetIdentifier))
		if err == nil {
			stake.RewardDebt = stake.Amount.ToLegacyDec().Mul(pool.AccRewardPerShare).TruncateInt()
		}
	} else if stake.TargetType == types.StakeTargetType_STAKE_TARGET_TAG {
		pool, err := k.GetTagStakePool(ctx, stake.TargetIdentifier)
		if err == nil {
			stake.RewardDebt = stake.Amount.ToLegacyDec().Mul(pool.AccRewardPerShare).TruncateInt()
		}
	}

	// Save updated stake
	if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
		return math.ZeroInt(), err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"staking_rewards_claimed",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("staker", stakerAddr.String()),
			sdk.NewAttribute("rewards", rewards.String()),
		),
	)

	return rewards, nil
}

// CompoundStakingRewards compounds pending rewards into stake principal
func (k Keeper) CompoundStakingRewards(ctx context.Context, stakeID uint64, stakerAddr sdk.AccAddress) (math.Int, error) {
	stake, err := k.GetStake(ctx, stakeID)
	if err != nil {
		return math.ZeroInt(), err
	}

	// Verify staker owns the stake
	if stake.Staker != stakerAddr.String() {
		return math.ZeroInt(), fmt.Errorf("only stake owner can compound rewards")
	}

	// Calculate pending rewards
	rewards, err := k.GetPendingStakingRewards(ctx, stake)
	if err != nil {
		return math.ZeroInt(), err
	}

	if rewards.IsZero() {
		return math.ZeroInt(), nil
	}

	// Mint the rewards to the staker's balance first, so LockDREAM has sufficient unlocked balance
	if err := k.MintDREAM(ctx, stakerAddr, rewards); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to mint compounded rewards: %w", err)
	}

	// Add rewards to stake principal
	stake.Amount = stake.Amount.Add(rewards)
	stake.LastClaimedAt = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	// Update reward debt for MasterChef-style targets
	if stake.TargetType == types.StakeTargetType_STAKE_TARGET_MEMBER {
		pool, err := k.GetMemberStakePool(ctx, sdk.MustAccAddressFromBech32(stake.TargetIdentifier))
		if err == nil {
			stake.RewardDebt = stake.Amount.ToLegacyDec().Mul(pool.AccRewardPerShare).TruncateInt()
		}
	} else if stake.TargetType == types.StakeTargetType_STAKE_TARGET_TAG {
		pool, err := k.GetTagStakePool(ctx, stake.TargetIdentifier)
		if err == nil {
			stake.RewardDebt = stake.Amount.ToLegacyDec().Mul(pool.AccRewardPerShare).TruncateInt()
		}
	}

	// Lock additional DREAM for the compounded rewards
	if err := k.LockDREAM(ctx, stakerAddr, rewards); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to lock compounded rewards: %w", err)
	}

	// For member/tag staking, update pool totals
	if stake.TargetType == types.StakeTargetType_STAKE_TARGET_MEMBER {
		if err := k.updateMemberStakePoolOnStake(ctx, stake.TargetIdentifier, rewards); err != nil {
			return math.ZeroInt(), err
		}
	} else if stake.TargetType == types.StakeTargetType_STAKE_TARGET_TAG {
		if err := k.updateTagStakePoolOnStake(ctx, stake.TargetIdentifier, rewards); err != nil {
			return math.ZeroInt(), err
		}
	}

	// Save updated stake
	if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
		return math.ZeroInt(), err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"staking_rewards_compounded",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("staker", stakerAddr.String()),
			sdk.NewAttribute("compounded", rewards.String()),
			sdk.NewAttribute("new_principal", stake.Amount.String()),
		),
	)

	return rewards, nil
}
