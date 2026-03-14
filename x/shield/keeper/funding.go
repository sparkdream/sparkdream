package keeper

import (
	"context"

	"cosmossdk.io/math"

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

// PruneDayFundings removes day funding entries before cutoffDay.
func (k Keeper) PruneDayFundings(ctx context.Context, cutoffDay uint64) error {
	iter, err := k.DayFundings.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	var toDelete []uint64
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		if key < cutoffDay {
			toDelete = append(toDelete, key)
		}
	}

	for _, key := range toDelete {
		if err := k.DayFundings.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
