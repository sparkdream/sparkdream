package keeper

import (
	"context"
	"errors"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
)

// ---------------------------------------------------------------------------
// Treasury management — DREAM balance tracking and enforcement
// ---------------------------------------------------------------------------

// GetTreasuryBalance returns the current DREAM balance held in the x/rep
// module treasury. Returns zero if the balance has never been set.
func (k Keeper) GetTreasuryBalance(ctx context.Context) (math.Int, error) {
	str, err := k.TreasuryBalance.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	val, ok := math.NewIntFromString(str)
	if !ok {
		return math.Int{}, fmt.Errorf("invalid treasury balance %q", str)
	}
	return val, nil
}

// AddToTreasury adds the given amount of DREAM to the module treasury.
func (k Keeper) AddToTreasury(ctx context.Context, amount math.Int) error {
	bal, err := k.GetTreasuryBalance(ctx)
	if err != nil {
		return err
	}
	bal = bal.Add(amount)
	return k.TreasuryBalance.Set(ctx, bal.String())
}

// SpendFromTreasury spends up to `amount` of DREAM from the module treasury.
// If the treasury holds less than the requested amount, the entire remaining
// balance is spent. Returns the actual amount spent.
func (k Keeper) SpendFromTreasury(ctx context.Context, amount math.Int) (math.Int, error) {
	bal, err := k.GetTreasuryBalance(ctx)
	if err != nil {
		return math.Int{}, err
	}

	spent := amount
	if bal.LT(amount) {
		spent = bal
	}

	bal = bal.Sub(spent)
	if err := k.TreasuryBalance.Set(ctx, bal.String()); err != nil {
		return math.Int{}, err
	}
	return spent, nil
}

// EnforceTreasuryBalance checks whether the treasury balance exceeds the
// MaxTreasuryBalance parameter. If it does, the excess is burned and the
// SeasonBurned counter is incremented accordingly.
func (k Keeper) EnforceTreasuryBalance(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	bal, err := k.GetTreasuryBalance(ctx)
	if err != nil {
		return err
	}

	maxBal := params.MaxTreasuryBalance
	if bal.LTE(maxBal) {
		return nil
	}

	excess := bal.Sub(maxBal)

	// Burn the excess by capping the treasury at the maximum.
	if err := k.TreasuryBalance.Set(ctx, maxBal.String()); err != nil {
		return err
	}

	// Track the burn in the seasonal counter.
	if err := k.TrackBurn(ctx, excess); err != nil {
		return err
	}

	return nil
}

// ---------------------------------------------------------------------------
// Seasonal mint/burn counters
// ---------------------------------------------------------------------------

// TrackMint adds the given amount to the SeasonMinted counter.
func (k Keeper) TrackMint(ctx context.Context, amount math.Int) error {
	minted, err := k.GetSeasonMinted(ctx)
	if err != nil {
		return err
	}
	minted = minted.Add(amount)
	return k.SeasonMinted.Set(ctx, minted.String())
}

// TrackBurn adds the given amount to the SeasonBurned counter.
func (k Keeper) TrackBurn(ctx context.Context, amount math.Int) error {
	burned, err := k.GetSeasonBurned(ctx)
	if err != nil {
		return err
	}
	burned = burned.Add(amount)
	return k.SeasonBurned.Set(ctx, burned.String())
}

// GetSeasonMinted returns the total DREAM minted during the current season.
// Returns zero if the counter has not been set.
func (k Keeper) GetSeasonMinted(ctx context.Context) (math.Int, error) {
	str, err := k.SeasonMinted.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	val, ok := math.NewIntFromString(str)
	if !ok {
		return math.Int{}, fmt.Errorf("invalid season minted %q", str)
	}
	return val, nil
}

// GetSeasonInitiativeRewardsMinted returns the total DREAM minted via initiative
// completion during the current season. Returns zero if the counter has not been set.
func (k Keeper) GetSeasonInitiativeRewardsMinted(ctx context.Context) (math.Int, error) {
	str, err := k.SeasonInitiativeRewardsMinted.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	val, ok := math.NewIntFromString(str)
	if !ok {
		return math.Int{}, fmt.Errorf("invalid season initiative rewards minted %q", str)
	}
	return val, nil
}

// TrackInitiativeRewardMint adds the given amount to the per-season initiative
// rewards counter. Called by CompleteInitiative after minting the completer's reward.
func (k Keeper) TrackInitiativeRewardMint(ctx context.Context, amount math.Int) error {
	minted, err := k.GetSeasonInitiativeRewardsMinted(ctx)
	if err != nil {
		return err
	}
	minted = minted.Add(amount)
	return k.SeasonInitiativeRewardsMinted.Set(ctx, minted.String())
}

// CheckAndTrackEpochMint atomically enforces the per-epoch DREAM mint ceiling
// (params.MaxDreamMintPerEpoch) and advances the counter. The tracked epoch
// rolls over automatically on the first mint of a new epoch, so no separate
// bookkeeping is required in the EndBlocker. Param validation now rejects a
// zero cap, so an unset/zero cap here is a configuration error rather than an
// "unbounded" escape hatch.
func (k Keeper) CheckAndTrackEpochMint(ctx context.Context, amount math.Int) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if params.MaxDreamMintPerEpoch.IsNil() || !params.MaxDreamMintPerEpoch.IsPositive() {
		return fmt.Errorf("max_dream_mint_per_epoch must be a positive value (got %v)", params.MaxDreamMintPerEpoch)
	}

	currentEpoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}

	trackedEpoch, err := k.EpochMintedEpoch.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}

	var minted math.Int
	if err == nil && trackedEpoch == uint64(currentEpoch) {
		amountStr, getErr := k.EpochMintedAmount.Get(ctx)
		if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
			return getErr
		}
		if getErr == nil {
			parsed, ok := math.NewIntFromString(amountStr)
			if !ok {
				return fmt.Errorf("invalid epoch minted amount %q", amountStr)
			}
			minted = parsed
		} else {
			minted = math.ZeroInt()
		}
	} else {
		minted = math.ZeroInt()
	}

	newTotal := minted.Add(amount)
	if newTotal.GT(params.MaxDreamMintPerEpoch) {
		return types.ErrDreamMintCapExceeded
	}

	if err := k.EpochMintedEpoch.Set(ctx, uint64(currentEpoch)); err != nil {
		return err
	}
	return k.EpochMintedAmount.Set(ctx, newTotal.String())
}

// GetSeasonBurned returns the total DREAM burned during the current season.
// Returns zero if the counter has not been set.
func (k Keeper) GetSeasonBurned(ctx context.Context) (math.Int, error) {
	str, err := k.SeasonBurned.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	val, ok := math.NewIntFromString(str)
	if !ok {
		return math.Int{}, fmt.Errorf("invalid season burned %q", str)
	}
	return val, nil
}
