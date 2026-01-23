package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateStake creates a new stake on a target (initiative, project, member, tag)
func (k Keeper) CreateStake(
	ctx context.Context,
	staker sdk.AccAddress,
	targetType types.StakeTargetType,
	targetID uint64,
	targetIdentifier string,
	amount math.Int,
) (uint64, error) {
	// Validate amount
	if amount.IsNegative() || amount.IsZero() {
		return 0, types.ErrInvalidAmount
	}

	// Validate member exists
	_, err := k.GetMember(ctx, staker)
	if err != nil {
		return 0, fmt.Errorf("staker is not a member: %w", err)
	}

	// Get params for validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	// Self-stake prevention for member staking
	if targetType == types.StakeTargetType_STAKE_TARGET_MEMBER {
		if targetIdentifier == staker.String() && !params.AllowSelfMemberStake {
			return 0, fmt.Errorf("cannot stake on yourself")
		}
	}

	// Validate target exists based on type
	switch targetType {
	case types.StakeTargetType_STAKE_TARGET_INITIATIVE:
		_, err := k.GetInitiative(ctx, targetID)
		if err != nil {
			return 0, fmt.Errorf("initiative not found: %w", err)
		}
	case types.StakeTargetType_STAKE_TARGET_PROJECT:
		_, err := k.GetProject(ctx, targetID)
		if err != nil {
			return 0, fmt.Errorf("project not found: %w", err)
		}
	case types.StakeTargetType_STAKE_TARGET_MEMBER:
		if targetIdentifier == "" {
			return 0, fmt.Errorf("member address required for member staking")
		}
		_, err := sdk.AccAddressFromBech32(targetIdentifier)
		if err != nil {
			return 0, fmt.Errorf("invalid member address: %w", err)
		}
	case types.StakeTargetType_STAKE_TARGET_TAG:
		if targetIdentifier == "" {
			return 0, fmt.Errorf("tag name required for tag staking")
		}
	default:
		return 0, types.ErrInvalidTargetType
	}

	// Lock DREAM from staker
	if err := k.LockDREAM(ctx, staker, amount); err != nil {
		return 0, fmt.Errorf("failed to lock DREAM: %w", err)
	}

	// Get next stake ID
	stakeID, err := k.StakeSeq.Next(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get next stake ID: %w", err)
	}

	// Create stake with new fields
	stake := types.Stake{
		Id:               stakeID,
		Staker:           staker.String(),
		TargetType:       targetType,
		TargetId:         targetID,
		TargetIdentifier: targetIdentifier,
		Amount:           amount,
		CreatedAt:        sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
		LastClaimedAt:    0,
		RewardDebt:       math.ZeroInt(),
	}

	// For MasterChef-style pools, initialize reward debt
	if targetType == types.StakeTargetType_STAKE_TARGET_MEMBER {
		pool, err := k.MemberStakePool.Get(ctx, targetIdentifier)
		if err == nil {
			stake.RewardDebt = amount.ToLegacyDec().Mul(pool.AccRewardPerShare).TruncateInt()
		}
		// Update member stake pool
		if err := k.updateMemberStakePoolOnStake(ctx, targetIdentifier, amount); err != nil {
			return 0, fmt.Errorf("failed to update member stake pool: %w", err)
		}
	} else if targetType == types.StakeTargetType_STAKE_TARGET_TAG {
		pool, err := k.TagStakePool.Get(ctx, targetIdentifier)
		if err == nil {
			stake.RewardDebt = amount.ToLegacyDec().Mul(pool.AccRewardPerShare).TruncateInt()
		}
		// Update tag stake pool
		if err := k.updateTagStakePoolOnStake(ctx, targetIdentifier, amount); err != nil {
			return 0, fmt.Errorf("failed to update tag stake pool: %w", err)
		}
	} else if targetType == types.StakeTargetType_STAKE_TARGET_PROJECT {
		// Update project stake info
		if err := k.updateProjectStakeInfoOnStake(ctx, targetID, amount); err != nil {
			return 0, fmt.Errorf("failed to update project stake info: %w", err)
		}
	}

	// Store stake
	if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
		return 0, fmt.Errorf("failed to store stake: %w", err)
	}

	// Add to target index for efficient lookups
	if err := k.AddStakeToTargetIndex(ctx, stake); err != nil {
		return 0, fmt.Errorf("failed to add stake to target index: %w", err)
	}

	// If staking on an initiative, update conviction (lazy update)
	if targetType == types.StakeTargetType_STAKE_TARGET_INITIATIVE {
		if err := k.UpdateInitiativeConvictionLazy(ctx, targetID); err != nil {
			return 0, fmt.Errorf("failed to update conviction: %w", err)
		}
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"stake_created",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("staker", staker.String()),
			sdk.NewAttribute("target_type", targetType.String()),
			sdk.NewAttribute("target_id", fmt.Sprintf("%d", targetID)),
			sdk.NewAttribute("target_identifier", targetIdentifier),
			sdk.NewAttribute("amount", amount.String()),
		),
	)

	return stakeID, nil
}

// GetStake retrieves a stake by ID
func (k Keeper) GetStake(ctx context.Context, stakeID uint64) (types.Stake, error) {
	stake, err := k.Stake.Get(ctx, stakeID)
	if err != nil {
		if err == collections.ErrNotFound {
			return types.Stake{}, fmt.Errorf("stake %d not found", stakeID)
		}
		return types.Stake{}, err
	}
	return stake, nil
}

// RemoveStake removes a stake (partially or fully) and returns DREAM to staker with time-based APY rewards
func (k Keeper) RemoveStake(ctx context.Context, stakeID uint64, stakerAddr sdk.AccAddress, amount math.Int) error {
	// Get stake
	stake, err := k.GetStake(ctx, stakeID)
	if err != nil {
		return err
	}

	// Validate staker
	if stake.Staker != stakerAddr.String() {
		return fmt.Errorf("only staker can remove stake")
	}

	currentStakeAmount := stake.Amount

	// Validate amount
	if amount.IsNegative() || amount.IsZero() {
		return types.ErrInvalidAmount
	}
	if amount.GT(currentStakeAmount) {
		return types.ErrInsufficientBalance
	}

	// Create a temporary stake representing only the portion being removed
	// This ensures rewards are calculated only for the removed amount
	removedPortionStake := stake
	removedPortionStake.Amount = amount

	// Calculate staking rewards based on time and APY for the removed portion
	stakingReward, err := k.CalculateStakingReward(ctx, removedPortionStake)
	if err != nil {
		return fmt.Errorf("failed to calculate staking reward: %w", err)
	}

	// Mint staking rewards to staker
	if stakingReward.GT(math.ZeroInt()) {
		if err := k.MintDREAM(ctx, stakerAddr, stakingReward); err != nil {
			return fmt.Errorf("failed to mint staking reward: %w", err)
		}
	}

	// Unlock the removed principal DREAM
	if err := k.UnlockDREAM(ctx, stakerAddr, amount); err != nil {
		return fmt.Errorf("failed to unlock DREAM: %w", err)
	}

	// Update or Delete Stake
	remainingAmount := currentStakeAmount.Sub(amount)

	if remainingAmount.IsZero() {
		// Full removal - also remove from target index
		if err := k.RemoveStakeFromTargetIndex(ctx, stake); err != nil {
			// Log but don't fail - index might not exist for old stakes
			sdk.UnwrapSDKContext(ctx).Logger().Debug("failed to remove stake from target index", "error", err)
		}
		if err := k.Stake.Remove(ctx, stakeID); err != nil {
			return fmt.Errorf("failed to remove stake: %w", err)
		}
	} else {
		// Partial removal - index key doesn't change, just the amount
		stake.Amount = remainingAmount
		if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
			return fmt.Errorf("failed to update stake: %w", err)
		}
	}

	// Trigger conviction update after store change
	if stake.TargetType == types.StakeTargetType_STAKE_TARGET_INITIATIVE {
		if err := k.UpdateInitiativeConvictionLazy(ctx, stake.TargetId); err != nil {
			return fmt.Errorf("failed to update conviction: %w", err)
		}
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	eventType := "stake_removed"
	if !remainingAmount.IsZero() {
		eventType = "stake_reduced"
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			eventType,
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("staker", stakerAddr.String()),
			sdk.NewAttribute("amount_removed", amount.String()),
			sdk.NewAttribute("amount_remaining", remainingAmount.String()),
			sdk.NewAttribute("reward", stakingReward.String()),
		),
	)

	return nil
}

// GetInitiativeStakes returns all stakes for an initiative.
// Uses the StakesByTarget index for O(stakes_on_initiative) instead of O(all_stakes) complexity.
func (k Keeper) GetInitiativeStakes(ctx context.Context, initiativeID uint64) ([]types.Stake, error) {
	return k.GetStakesByTarget(ctx, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initiativeID)
}

// GetProjectStakes returns all stakes for a project.
// Uses the StakesByTarget index for O(stakes_on_project) instead of O(all_stakes) complexity.
func (k Keeper) GetProjectStakes(ctx context.Context, projectID uint64) ([]types.Stake, error) {
	return k.GetStakesByTarget(ctx, types.StakeTargetType_STAKE_TARGET_PROJECT, projectID)
}
