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

// readIntItem returns the math.Int stored as a string in `item`, or zero
// when the item has not been set. Used by the per-season counters in this
// file.
func readIntItem(ctx context.Context, item collections.Item[string], name string) (math.Int, error) {
	str, err := item.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	val, ok := math.NewIntFromString(str)
	if !ok {
		return math.Int{}, fmt.Errorf("invalid %s %q", name, str)
	}
	return val, nil
}

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

// AddToTreasury credits `amount` DREAM to the module treasury ledger and
// updates the per-season inflow counter. Callers responsible for any minting
// (mint-cap enforcement + SeasonMinted tracking) must do that before calling
// AddToTreasury; see MintToTreasury for the combined helper.
func (k Keeper) AddToTreasury(ctx context.Context, amount math.Int) error {
	if amount.IsNil() || !amount.IsPositive() {
		return nil
	}
	bal, err := k.GetTreasuryBalance(ctx)
	if err != nil {
		return err
	}
	bal = bal.Add(amount)
	if err := k.TreasuryBalance.Set(ctx, bal.String()); err != nil {
		return err
	}
	return k.trackTreasuryInflow(ctx, amount)
}

// MintToTreasury mints `amount` fresh DREAM into the module treasury ledger.
// Enforces the per-epoch mint ceiling, increments the season mint counter,
// and records the treasury inflow in a single bookkeeping step. Use this
// for revenue that the protocol "earns" into treasury (e.g. the
// CompleteInitiative TreasuryShare), distinct from member-account mints.
func (k Keeper) MintToTreasury(ctx context.Context, amount math.Int) error {
	if amount.IsNil() || !amount.IsPositive() {
		return nil
	}
	if err := k.CheckAndTrackEpochMint(ctx, amount); err != nil {
		return err
	}
	if err := k.TrackMint(ctx, amount); err != nil {
		return err
	}
	return k.AddToTreasury(ctx, amount)
}

// SpendFromTreasury spends up to `amount` of DREAM from the module treasury.
// If the treasury holds less than the requested amount, the entire remaining
// balance is spent. The per-season outflow counter is incremented by the
// actual amount drawn. Returns the actual amount spent.
func (k Keeper) SpendFromTreasury(ctx context.Context, amount math.Int) (math.Int, error) {
	if amount.IsNil() || !amount.IsPositive() {
		return math.ZeroInt(), nil
	}
	bal, err := k.GetTreasuryBalance(ctx)
	if err != nil {
		return math.Int{}, err
	}

	spent := amount
	if bal.LT(amount) {
		spent = bal
	}

	if !spent.IsPositive() {
		return math.ZeroInt(), nil
	}

	bal = bal.Sub(spent)
	if err := k.TreasuryBalance.Set(ctx, bal.String()); err != nil {
		return math.Int{}, err
	}
	if err := k.trackTreasuryOutflow(ctx, spent); err != nil {
		return math.Int{}, err
	}
	return spent, nil
}

// PayDREAMFromTreasuryFirst pays `amount` DREAM to the recipient, draining
// the module treasury first and minting the shortfall to the recipient. The
// `enabled` switch corresponds to the TreasuryFundsInterims /
// TreasuryFundsRetroPgf operational params: when false this is equivalent
// to a straight MintDREAM. Returns the (treasury_paid, minted) split.
//
// Referral rewards fire ONCE on the total (treasury_paid + minted) so an
// invitee's inviter is compensated identically regardless of whether the
// payment was fresh-minted or drained from treasury. MintDREAM's built-in
// per-mint referral is suppressed via the reentrancy guard so the
// shortfall mint doesn't double-pay the inviter on the same payment.
//
// Note: the treasury-paid portion does NOT route through MintDREAM (no new
// DREAM is created for it) — it is transferred from the treasury ledger
// directly to the recipient's member balance, bypassing the per-epoch mint
// cap. The minted shortfall is subject to that cap as usual.
func (k Keeper) PayDREAMFromTreasuryFirst(
	ctx context.Context,
	recipient sdk.AccAddress,
	amount math.Int,
	enabled bool,
) (treasuryPaid, minted math.Int, err error) {
	treasuryPaid = math.ZeroInt()
	minted = math.ZeroInt()

	if amount.IsNil() || !amount.IsPositive() {
		return treasuryPaid, minted, nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// If we're already inside a referral cascade, do not fire another level
	// of referral — mirrors the guard MintDREAM uses for the same reason.
	alreadyInCascade := sdkCtx.Value(referralMintingKey) != nil

	remaining := amount
	if enabled {
		drawn, drawErr := k.SpendFromTreasury(ctx, amount)
		if drawErr != nil {
			return treasuryPaid, minted, drawErr
		}
		if drawn.IsPositive() {
			if creditErr := k.CreditDREAM(ctx, recipient, drawn); creditErr != nil {
				return treasuryPaid, minted, creditErr
			}
			treasuryPaid = drawn
			remaining = amount.Sub(drawn)
		}
	}

	if remaining.IsPositive() {
		// Mint with the referral guard set so MintDREAM does NOT fire
		// CalculateReferralReward on the shortfall alone — we fire it on
		// the total below to keep inviter economics flat across treasury
		// states.
		mintCtx := ctx
		if !alreadyInCascade {
			mintCtx = sdkCtx.WithValue(referralMintingKey, true)
		}
		if mintErr := k.MintDREAM(mintCtx, recipient, remaining); mintErr != nil {
			return treasuryPaid, minted, mintErr
		}
		minted = remaining
	}

	total := treasuryPaid.Add(minted)
	if total.IsPositive() && !alreadyInCascade {
		guardedCtx := sdkCtx.WithValue(referralMintingKey, true)
		if refErr := k.CalculateReferralReward(guardedCtx, recipient, total); refErr != nil {
			sdkCtx.Logger().Error("failed to calculate referral reward for treasury-funded payment",
				"recipient", recipient.String(),
				"total", total.String(),
				"treasury_paid", treasuryPaid.String(),
				"minted", minted.String(),
				"error", refErr)
		}
	}

	return treasuryPaid, minted, nil
}

// PayRetroPgfReward pays a retroactive public-goods reward to `recipient`.
// Internally reads the TreasuryFundsRetroPgf flag and routes through
// PayDREAMFromTreasuryFirst so the treasury is drained first when the flag
// is on. Returns the (treasuryPaid, minted) split.
func (k Keeper) PayRetroPgfReward(ctx context.Context, recipient sdk.AccAddress, amount math.Int) (treasuryPaid, minted math.Int, err error) {
	params, perr := k.Params.Get(ctx)
	if perr != nil {
		return math.ZeroInt(), math.ZeroInt(), perr
	}
	return k.PayDREAMFromTreasuryFirst(ctx, recipient, amount, params.TreasuryFundsRetroPgf)
}

// trackTreasuryInflow advances the per-season treasury inflow counter.
func (k Keeper) trackTreasuryInflow(ctx context.Context, amount math.Int) error {
	if amount.IsNil() || !amount.IsPositive() {
		return nil
	}
	inflow, err := k.GetSeasonTreasuryInflow(ctx)
	if err != nil {
		return err
	}
	inflow = inflow.Add(amount)
	return k.SeasonTreasuryInflow.Set(ctx, inflow.String())
}

// trackTreasuryOutflow advances the per-season treasury outflow counter.
func (k Keeper) trackTreasuryOutflow(ctx context.Context, amount math.Int) error {
	if amount.IsNil() || !amount.IsPositive() {
		return nil
	}
	outflow, err := k.GetSeasonTreasuryOutflow(ctx)
	if err != nil {
		return err
	}
	outflow = outflow.Add(amount)
	return k.SeasonTreasuryOutflow.Set(ctx, outflow.String())
}

// GetSeasonTreasuryInflow returns the total DREAM credited to the module
// treasury during the current season. Returns zero if the counter has not
// been set.
func (k Keeper) GetSeasonTreasuryInflow(ctx context.Context) (math.Int, error) {
	return readIntItem(ctx, k.SeasonTreasuryInflow, "season treasury inflow")
}

// GetSeasonTreasuryOutflow returns the total DREAM spent from the module
// treasury during the current season. Returns zero if the counter has not
// been set.
func (k Keeper) GetSeasonTreasuryOutflow(ctx context.Context) (math.Int, error) {
	return readIntItem(ctx, k.SeasonTreasuryOutflow, "season treasury outflow")
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

// GetSeasonInterimRewardsMinted returns the DREAM minted to pay interim work
// during the current season. Returns zero if the counter has not been set.
func (k Keeper) GetSeasonInterimRewardsMinted(ctx context.Context) (math.Int, error) {
	str, err := k.SeasonInterimRewardsMinted.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	val, ok := math.NewIntFromString(str)
	if !ok {
		return math.Int{}, fmt.Errorf("invalid season interim rewards %q", str)
	}
	return val, nil
}

// TrackInterimRewardMint adds the given amount to the SeasonInterimRewardsMinted
// counter, which max_interim_rewards_per_season is checked against.
func (k Keeper) TrackInterimRewardMint(ctx context.Context, amount math.Int) error {
	minted, err := k.GetSeasonInterimRewardsMinted(ctx)
	if err != nil {
		return err
	}
	minted = minted.Add(amount)
	return k.SeasonInterimRewardsMinted.Set(ctx, minted.String())
}

// GetSeasonStakingRewardsMinted returns the DREAM minted as staking rewards
// during the current season. Returns zero if the counter has not been set.
func (k Keeper) GetSeasonStakingRewardsMinted(ctx context.Context) (math.Int, error) {
	str, err := k.SeasonStakingRewardsMinted.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return math.ZeroInt(), nil
		}
		return math.Int{}, err
	}
	val, ok := math.NewIntFromString(str)
	if !ok {
		return math.Int{}, fmt.Errorf("invalid season staking rewards minted %q", str)
	}
	return val, nil
}

// TrackStakingRewardMint adds the given amount to the per-season staking rewards
// counter. Called by settleStake, the only path that mints from a stake pool
// accumulator.
//
// This counter exists to be subtracted, not reported: InitSeasonalPool sizes the
// incoming season's budget from the mints the chain produced, and staking
// rewards are not production. Leaving them in the base would let each season's
// pool fund the next one's, compounding against nothing but the schedule cap.
func (k Keeper) TrackStakingRewardMint(ctx context.Context, amount math.Int) error {
	minted, err := k.GetSeasonStakingRewardsMinted(ctx)
	if err != nil {
		return err
	}
	minted = minted.Add(amount)
	return k.SeasonStakingRewardsMinted.Set(ctx, minted.String())
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
