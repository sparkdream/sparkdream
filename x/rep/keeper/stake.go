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
		// Per-member cap: prevents reward pool extraction via disproportionate stakes
		memberTotal, tranches, err := k.stakerTotalsOnTarget(ctx, staker, targetType, targetID)
		if err != nil {
			return 0, err
		}
		if tranches >= types.MaxStakeTranchesPerTarget {
			return 0, types.ErrTooManyStakeTranches
		}
		if memberTotal.Add(amount).GT(params.MaxInitiativeStakePerMember) {
			return 0, types.ErrInitiativeStakeCap
		}
	case types.StakeTargetType_STAKE_TARGET_PROJECT:
		_, err := k.GetProject(ctx, targetID)
		if err != nil {
			return 0, fmt.Errorf("project not found: %w", err)
		}
		// Same per-member cap for projects (shared seasonal reward pool)
		memberTotal, tranches, err := k.stakerTotalsOnTarget(ctx, staker, targetType, targetID)
		if err != nil {
			return 0, err
		}
		if tranches >= types.MaxStakeTranchesPerTarget {
			return 0, types.ErrTooManyStakeTranches
		}
		if memberTotal.Add(amount).GT(params.MaxInitiativeStakePerMember) {
			return 0, types.ErrInitiativeStakeCap
		}
	case types.StakeTargetType_STAKE_TARGET_MEMBER:
		if targetIdentifier == "" {
			return 0, fmt.Errorf("member address required for member staking")
		}
		_, err := sdk.AccAddressFromBech32(targetIdentifier)
		if err != nil {
			return 0, fmt.Errorf("invalid member address: %w", err)
		}
		// Circular staking prevention: check if target already has an active
		// member stake on the staker. This prevents A→B + B→A mutual inflation.
		hasReverse, err := k.HasMemberStakeOn(ctx, targetIdentifier, staker.String())
		if err != nil {
			return 0, fmt.Errorf("failed to check circular stake: %w", err)
		}
		if hasReverse {
			return 0, types.ErrCircularMemberStake
		}
	case types.StakeTargetType_STAKE_TARGET_TAG:
		if targetIdentifier == "" {
			return 0, fmt.Errorf("tag name required for tag staking")
		}
	case types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT:
		if targetID == 0 {
			return 0, fmt.Errorf("content ID must be positive")
		}
		// Self-stake prevention: resolve the true author/owner from the owning
		// module's keeper. The user-supplied targetIdentifier cannot be trusted
		// for security checks (an empty or forged value would bypass).
		if author := k.resolveContentAuthor(ctx, targetType, targetID); author != "" && author == staker.String() {
			return 0, types.ErrSelfContentStake
		}
		// Per-member cap: sum existing stakes by this member on this target
		memberTotal, tranches, err := k.stakerTotalsOnTarget(ctx, staker, targetType, targetID)
		if err != nil {
			return 0, err
		}
		if tranches >= types.MaxStakeTranchesPerTarget {
			return 0, types.ErrTooManyStakeTranches
		}
		if memberTotal.Add(amount).GT(params.MaxContentStakePerMember) {
			return 0, types.ErrContentStakeCap
		}
	case types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_BLOG_REPLY_AUTHOR_BOND:
		// Author bonds must be created via keeper methods, not MsgStake
		return 0, types.ErrAuthorBondViaMsg
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

	// Snapshot the pool accumulator as this stake's reward-debt baseline. This
	// is what makes the new staker's pending balance start at zero instead of
	// at the pool's entire accumulated history, and it is why a stake placed
	// just before a distribution earns nothing from it. It must be taken for
	// every reward-bearing target type — initiative and project stakes were
	// previously left at a permanent zero debt, which turned `pending` into a
	// pure function of stake size, repayable on every claim.
	//
	// Read before the denominator moves below: the accumulator is only advanced
	// by revenue, not by total_staked, but reading first keeps the ordering
	// obvious.
	rewardDebt, err := k.initialRewardDebt(ctx, stake, amount)
	if err != nil {
		return 0, fmt.Errorf("failed to compute initial reward debt: %w", err)
	}
	stake.RewardDebt = rewardDebt

	// Grow every denominator this stake will draw rewards from. Content
	// conviction and author bond types have no pool accounting and are no-ops.
	if err := k.updateStakePoolTotals(ctx, stake, amount); err != nil {
		return 0, fmt.Errorf("failed to update stake pool totals: %w", err)
	}

	// Store stake
	if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
		return 0, fmt.Errorf("failed to store stake: %w", err)
	}

	// Add to target index for efficient lookups
	if err := k.AddStakeToTargetIndex(ctx, stake); err != nil {
		return 0, fmt.Errorf("failed to add stake to target index: %w", err)
	}

	// Refresh conviction for whatever this stake feeds into. The recompute is
	// synchronous here — it is gas-metered and the staker should see the effect
	// immediately — and the initiative is then re-armed on the queue so its
	// time-driven drift keeps being tracked.
	if err := k.refreshConvictionForStake(ctx, stake); err != nil {
		return 0, err
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

	// Content conviction and author bond stakes earn no DREAM rewards
	if types.IsContentOrBondType(stake.TargetType) {
		// Prevent unstaking author bonds that are locked by an active content challenge
		if types.IsAuthorBondType(stake.TargetType) {
			if hasChallenge, _ := k.HasActiveContentChallenge(ctx, stake.TargetType, stake.TargetId); hasChallenge {
				return types.ErrBondLockedByChallenge
			}
		}

		// Simply unlock and return DREAM — no reward minting
		if err := k.UnlockDREAM(ctx, stakerAddr, amount); err != nil {
			return fmt.Errorf("failed to unlock DREAM: %w", err)
		}
		remainingAmount := currentStakeAmount.Sub(amount)
		if remainingAmount.IsZero() {
			if err := k.RemoveStakeFromTargetIndex(ctx, stake); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Debug("failed to remove stake from target index", "error", err)
			}
			if err := k.Stake.Remove(ctx, stakeID); err != nil {
				return fmt.Errorf("failed to remove stake: %w", err)
			}
		} else {
			stake.Amount = remainingAmount
			if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
				return fmt.Errorf("failed to update stake: %w", err)
			}
		}
		// Withdrawing content conviction lowers what propagates into any
		// initiative linking that content, so those need recomputing too.
		if err := k.refreshConvictionForStake(ctx, stake); err != nil {
			return err
		}

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
				sdk.NewAttribute("reward", "0"),
			),
		)
		return nil
	}

	remainingAmount := currentStakeAmount.Sub(amount)

	// Settle the whole position against the accumulator that actually owns it.
	// The previous code ran CalculateStakingReward — the *seasonal* accumulator
	// — for every non-content type, so a member or tag staker who unstaked
	// without claiming first was paid from the wrong pool (in practice, zero)
	// and then lost the record that was their only claim ticket. settleStake
	// dispatches on target type, so member and tag stakes settle against their
	// own pool and initiative and project stakes against the seasonal one.
	//
	// Rewards are collectable only after MinStakeDurationSeconds. Leaving
	// earlier forfeits them: the debt is still rebased, so the unclaimed DREAM
	// is never minted and simply stays in the pool for the remaining stakers.
	eligible, err := k.stakeMeetsMinDuration(ctx, stake)
	if err != nil {
		return err
	}
	stake, settlement, err := k.settleStake(ctx, stake, remainingAmount, !eligible)
	if err != nil {
		return err
	}
	stakingReward := settlement.Minted

	// Unlock the removed principal DREAM
	if err := k.UnlockDREAM(ctx, stakerAddr, amount); err != nil {
		return fmt.Errorf("failed to unlock DREAM: %w", err)
	}

	// Shrink every denominator this stake was diluting. Without this the pool
	// keeps dividing incoming revenue by DREAM that has already left, silently
	// and permanently under-paying everyone who stayed.
	if err := k.updateStakePoolTotals(ctx, stake, amount.Neg()); err != nil {
		return fmt.Errorf("failed to update stake pool totals: %w", err)
	}

	// Update or Delete Stake
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
		// Partial removal - index key doesn't change, just the amount.
		// settleStake has already rebased reward_debt to remainingAmount, so
		// the shrunken stake no longer carries a debt sized for its original
		// principal (which would have clamped its future rewards to zero).
		stake.Amount = remainingAmount
		if err := k.Stake.Set(ctx, stakeID, stake); err != nil {
			return fmt.Errorf("failed to update stake: %w", err)
		}
	}

	// Trigger conviction update after store change
	if err := k.refreshConvictionForStake(ctx, stake); err != nil {
		return err
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
			sdk.NewAttribute("reward_forfeited", settlement.Pending.Sub(stakingReward).String()),
		),
	)

	return nil
}

// stakerTotalsOnTarget returns how much `staker` already has staked on a target
// and across how many separate stake records. Both feed CreateStake's limits:
// the total against the per-member DREAM cap, the count against
// MaxStakeTranchesPerTarget.
func (k Keeper) stakerTotalsOnTarget(
	ctx context.Context,
	staker sdk.AccAddress,
	targetType types.StakeTargetType,
	targetID uint64,
) (math.Int, int, error) {
	existingStakes, err := k.GetStakesByTarget(ctx, targetType, targetID)
	if err != nil {
		return math.ZeroInt(), 0, fmt.Errorf("failed to check existing stakes: %w", err)
	}

	stakerStr := staker.String()
	total := math.ZeroInt()
	tranches := 0
	for _, s := range existingStakes {
		if s.Staker != stakerStr {
			continue
		}
		total = total.Add(s.Amount)
		tranches++
	}
	return total, tranches, nil
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

// HasMemberStakeOn checks if `stakerAddr` has an active MEMBER-type stake targeting `targetAddr`.
// Used for circular staking prevention: if B already stakes on A, A cannot stake on B.
func (k Keeper) HasMemberStakeOn(ctx context.Context, stakerAddr, targetAddr string) (bool, error) {
	// Walk all MEMBER-type stakes (targetID=0 for member stakes) and check
	// if any have staker==stakerAddr and targetIdentifier==targetAddr.
	memberType := int32(types.StakeTargetType_STAKE_TARGET_MEMBER)
	rng := collections.NewSuperPrefixedTripleRange[int32, uint64, uint64](memberType, 0)
	found := false
	err := k.StakesByTarget.Walk(ctx, rng, func(key collections.Triple[int32, uint64, uint64]) (stop bool, err error) {
		stakeID := key.K3()
		stake, err := k.Stake.Get(ctx, stakeID)
		if err != nil {
			return false, nil // Skip stale index entries
		}
		if stake.Staker == stakerAddr && stake.TargetIdentifier == targetAddr {
			found = true
			return true, nil // Stop walking
		}
		return false, nil
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

// resolveContentAuthor returns the on-chain author/owner address for the given
// content stake target by querying the owning module. Returns "" when the
// owning keeper is not wired or the lookup fails — callers must treat an
// empty result as "unable to verify" (fails closed: stake will not be blocked
// by self-stake check, but the lookup is not the only safeguard).
func (k Keeper) resolveContentAuthor(ctx context.Context, targetType types.StakeTargetType, targetID uint64) string {
	switch targetType {
	case types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT:
		if k.late.blogKeeper == nil {
			return ""
		}
		author, err := k.late.blogKeeper.GetPostAuthor(ctx, targetID)
		if err != nil {
			return ""
		}
		return author
	case types.StakeTargetType_STAKE_TARGET_FORUM_CONTENT:
		if k.late.forumKeeper == nil {
			return ""
		}
		author, err := k.late.forumKeeper.GetPostAuthor(ctx, targetID)
		if err != nil {
			return ""
		}
		return author
	case types.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT:
		if k.late.collectKeeper == nil {
			return ""
		}
		owner, err := k.late.collectKeeper.GetCollectionOwner(ctx, targetID)
		if err != nil {
			return ""
		}
		return owner
	}
	return ""
}
