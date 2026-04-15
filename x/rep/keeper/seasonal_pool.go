package keeper

import (
	"context"
	"errors"
	"fmt"

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

// UpdateSeasonalPoolTotalStaked adds delta to the total staked amount.
// delta may be negative (e.g. when a user unstakes).
func (k Keeper) UpdateSeasonalPoolTotalStaked(ctx context.Context, delta math.Int) error {
	current, err := k.getSeasonalPoolTotalStaked(ctx)
	if err != nil {
		return err
	}
	updated := current.Add(delta)
	if updated.IsNegative() {
		return fmt.Errorf("seasonal pool total staked cannot go negative: current %s, delta %s", current, delta)
	}
	return k.setSeasonalPoolTotalStaked(ctx, updated)
}

// InitSeasonalPool initialises the seasonal staking reward pool for a new
// season. It sets the remaining budget from params.MaxStakingRewardsPerSeason,
// resets the accumulator to zero, and records the season number. It also
// resets SeasonMinted and SeasonBurned counters.
func (k Keeper) InitSeasonalPool(ctx context.Context, season uint64) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	if err := k.setSeasonalPoolRemaining(ctx, params.MaxStakingRewardsPerSeason); err != nil {
		return fmt.Errorf("failed to set seasonal pool remaining: %w", err)
	}
	if err := k.setSeasonalPoolAccPerShare(ctx, math.LegacyZeroDec()); err != nil {
		return fmt.Errorf("failed to reset seasonal pool acc_per_share: %w", err)
	}
	if err := k.SeasonalPoolSeason.Set(ctx, season); err != nil {
		return fmt.Errorf("failed to set seasonal pool season: %w", err)
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

	// SeasonDurationEpochs is the total number of epochs in a season.
	// remainingEpochs is at least 1 to avoid division by zero; if we are
	// past the expected season end, dump whatever is left this epoch.
	remainingEpochs := params.SeasonDurationEpochs - currentEpoch%params.SeasonDurationEpochs
	if remainingEpochs <= 0 {
		remainingEpochs = 1
	}

	// epochSlice = remaining / remainingEpochs  (integer division)
	epochSlice := remaining.Quo(math.NewInt(remainingEpochs))
	if epochSlice.IsZero() {
		// Pool nearly exhausted — distribute whatever dust remains.
		epochSlice = remaining
	}

	// If there is staked DREAM, increment the accumulator.
	if totalStaked.IsPositive() {
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
	}
	// Note: if totalStaked is zero the epoch slice is effectively lost.
	// This is intentional — rewards only go to stakers.

	// Decrement remaining pool.
	remaining = remaining.Sub(epochSlice)
	if err := k.setSeasonalPoolRemaining(ctx, remaining); err != nil {
		return fmt.Errorf("failed to update seasonal pool remaining: %w", err)
	}

	return nil
}
