package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
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

// MaybeApplyBulkDecay applies decay to every member exactly once per epoch.
// It runs at the top of EndBlocker so all subsequent reads within the epoch
// (whether via lazy ApplyPendingDecay on write paths or via gRPC queries on
// a cache-wrapped context) observe the same post-decay balances — fixing the
// lazy-decay view inconsistency (x-rep-security.md Point 6). Cost: O(members)
// Power() calls once per epoch; for the 1000-member target this is cheap and
// amortizes to ~0.07 Power() calls per block at 14400 blocks/epoch.
func (k Keeper) MaybeApplyBulkDecay(ctx context.Context) error {
	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	if currentEpoch <= 0 {
		return nil
	}

	last, err := k.DecayLastProcessedEpoch.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	if err == nil && last >= uint64(currentEpoch) {
		return nil
	}

	if err := k.ApplyDecay(ctx); err != nil {
		return err
	}
	return k.DecayLastProcessedEpoch.Set(ctx, uint64(currentEpoch))
}

// DistributeEpochStakingRewards updates the seasonal reward pool accumulator.
// Called each epoch in EndBlocker. The accumulator enables O(1) lazy reward
// calculation per stake claim (MasterChef pattern).
//
// Each epoch: accPerShare += (poolRemaining / remainingEpochs) / totalStaked
// Individual rewards computed lazily: pending = stake * accPerShare - rewardDebt
//
// Member and tag stake rewards remain event-driven (unchanged):
//   - Member stakes: AccumulateMemberStakeRevenue on initiative completion
//   - Tag stakes: AccumulateTagStakeRevenue on initiative completion
func (k Keeper) DistributeEpochStakingRewards(ctx context.Context) error {
	return k.DistributeEpochStakingRewardsFromPool(ctx)
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
