package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// GetDayFunding returns the amount funded for a given day.
func (k Keeper) GetDayFunding(ctx context.Context, day uint64) math.Int {
	df, err := k.DayFundings.Get(ctx, day)
	if err != nil {
		return math.ZeroInt()
	}
	return df.AmountFunded
}

// SetDayFunding stores the funding amount for a given day.
func (k Keeper) SetDayFunding(ctx context.Context, day uint64, amount math.Int) error {
	return k.DayFundings.Set(ctx, day, types.DayFunding{
		Day:          day,
		AmountFunded: amount,
	})
}

// PruneDayFundings removes day funding entries before cutoffDay, walking only
// the stale key range.
func (k Keeper) PruneDayFundings(ctx context.Context, cutoffDay uint64) error {
	iter, err := k.DayFundings.Iterate(ctx, new(collections.Range[uint64]).EndExclusive(cutoffDay))
	if err != nil {
		return err
	}
	keys, err := iter.Keys()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := k.DayFundings.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// EpochFeesPaid returns the fees the module has paid in the current shield
// epoch.
func (k Keeper) EpochFeesPaid(ctx context.Context) math.Int {
	ledger, err := k.EpochFeeLedger.Get(ctx)
	if err != nil || ledger.Epoch != k.GetCurrentEpoch(ctx) || ledger.Paid.IsNil() {
		return math.ZeroInt()
	}
	return ledger.Paid
}

// ChargeEpochFee books fee against the chain-wide per-epoch budget, or
// refuses it (ErrEpochFeeBudgetExhausted) when the epoch's paid fees plus fee
// would exceed budget. Called by the ante handler right before the module
// pays the fee, in the same ante state.
func (k Keeper) ChargeEpochFee(ctx sdk.Context, fee math.Int, budget math.Int) error {
	if fee.IsNil() || !fee.IsPositive() {
		return nil
	}
	paid := k.EpochFeesPaid(ctx)
	if budget.IsNil() || paid.Add(fee).GT(budget) {
		return errorsmod.Wrapf(types.ErrEpochFeeBudgetExhausted, "paid %s + fee %s > budget %s", paid, fee, budget)
	}
	return k.EpochFeeLedger.Set(ctx, types.EpochFeeLedger{Epoch: k.GetCurrentEpoch(ctx), Paid: paid.Add(fee)})
}
