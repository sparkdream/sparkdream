package keeper

import (
	"context"
	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// IsEpochEnd checks if the current block is the end of an epoch
func (k Keeper) IsEpochEnd(ctx context.Context) (bool, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return false, err
	}
	if params.EpochBlocks == 0 {
		return false, nil
	}
	return sdk.UnwrapSDKContext(ctx).BlockHeight()%params.EpochBlocks == 0, nil
}

// UpdateInitiativeConviction updates the conviction for a specific initiative
func (k Keeper) UpdateInitiativeConviction(ctx context.Context, initiativeID uint64) error {
	return k.UpdateInitiativeConvictionLazy(ctx, initiativeID)
}

// TransitionToChallengePeriod moves an initiative from SUBMITTED to IN_REVIEW
func (k Keeper) TransitionToChallengePeriod(ctx context.Context, initiativeID uint64) error {
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW

	// Set review period end (current time + default review period)
	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	reviewDuration := params.DefaultReviewPeriodEpochs * params.EpochBlocks
	initiative.ReviewPeriodEnd = currentHeight + reviewDuration

	// Also set challenge period end which follows review
	challengeDuration := params.DefaultChallengePeriodEpochs * params.EpochBlocks
	initiative.ChallengePeriodEnd = initiative.ReviewPeriodEnd + challengeDuration

	return k.UpdateInitiative(ctx, initiative)
}

// ApplyDecay applies decay to unstaked DREAM balances of all members
func (k Keeper) ApplyDecay(ctx context.Context) error {
	// Iterate all members and apply pending decay
	// This ensures everyone is up to date at epoch end
	return k.Member.Walk(ctx, nil, func(key string, member types.Member) (stop bool, err error) {
		// Apply pending decay
		if err := k.ApplyPendingDecay(ctx, &member); err != nil {
			return true, err
		}
		// Save updated member back to store
		if err := k.Member.Set(ctx, key, member); err != nil {
			return true, err
		}
		return false, nil
	})
}

// DistributeEpochStakingRewards is called every epoch but intentionally does not distribute rewards.
// This is the OPTIMAL design for gas efficiency and scalability.
//
// Reward distribution uses event-driven and lazy calculation patterns:
// - Initiative/Project stakes: Calculated lazily on claim (CalculateStakingReward, getPendingProjectRewards)
//   Gas cost: O(1) per claim instead of O(all_stakes) per epoch
// - Member stakes: Updated when member earns DREAM (AccumulateMemberStakeRevenue in CompleteInitiative)
//   Gas cost: O(stakers_on_member) per revenue event instead of O(all_stakes) per epoch
// - Tag stakes: Updated when initiative completes (AccumulateTagStakeRevenue in CompleteInitiative)
//   Gas cost: O(stakers_on_tags) per completion instead of O(all_stakes) per epoch
//
// Periodic distribution would be LESS efficient and provide no benefit to stakers.
func (k Keeper) DistributeEpochStakingRewards(ctx context.Context) error {
	// Intentionally empty - rewards are distributed via event-driven and lazy patterns
	// Do not add periodic distribution here as it would significantly increase gas costs
	return nil
}

// UpdateAllTrustLevels updates trust levels for all members
func (k Keeper) UpdateAllTrustLevels(ctx context.Context) error {
	return k.Member.Walk(ctx, nil, func(key string, member types.Member) (stop bool, err error) {
		memberAddr, err := sdk.AccAddressFromBech32(member.Address)
		if err == nil {
			_ = k.UpdateTrustLevel(ctx, memberAddr)
		}
		return false, nil
	})
}

// ProcessExpiredAccountability checks for expired invitations
func (k Keeper) ProcessExpiredAccountability(ctx context.Context) error {
	// No-op for now as accountability expiry is passive
	return nil
}
