package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/commons/types"
)

// MaxRecurringSpendNoteLen caps the human-readable purpose attached to a
// schedule. Kept tight to bound on-chain string bloat — UIs that need more
// can store off-chain and reference by id.
const MaxRecurringSpendNoteLen = 256

// GetRecurringSpend returns the schedule with the given id.
func (k Keeper) GetRecurringSpend(ctx context.Context, id uint64) (types.RecurringSpend, error) {
	rs, err := k.RecurringSpends.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.RecurringSpend{}, errorsmod.Wrapf(types.ErrRecurringSpendNotFound, "id=%d", id)
		}
		return types.RecurringSpend{}, err
	}
	return rs, nil
}

// setRecurringSpend writes a schedule and keeps the secondary indexes in
// sync. The caller is responsible for incrementing/decrementing
// ActiveRecurringSpendCount when status crosses the ACTIVE boundary.
func (k Keeper) setRecurringSpend(ctx context.Context, rs types.RecurringSpend) error {
	if err := k.RecurringSpends.Set(ctx, rs.Id, rs); err != nil {
		return err
	}
	if err := k.RecurringSpendsByAuthority.Set(ctx, collections.Join(rs.Authority, rs.Id)); err != nil {
		return err
	}
	if err := k.RecurringSpendsByRecipient.Set(ctx, collections.Join(rs.Recipient, rs.Id)); err != nil {
		return err
	}
	return nil
}

// incActiveCount bumps the per-authority active counter and returns the new
// value. Used by Schedule to enforce max_active_recurring_spends_per_group.
func (k Keeper) incActiveCount(ctx context.Context, authority string) (uint32, error) {
	cur, err := k.ActiveRecurringSpendCount.Get(ctx, authority)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return 0, err
	}
	cur++
	return cur, k.ActiveRecurringSpendCount.Set(ctx, authority, cur)
}

// decActiveCount drops the per-authority active counter. Saturates at 0 so
// a double-cancel (or genesis import of a terminal schedule) cannot drive
// the counter negative.
func (k Keeper) decActiveCount(ctx context.Context, authority string) error {
	cur, err := k.ActiveRecurringSpendCount.Get(ctx, authority)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil
		}
		return err
	}
	if cur == 0 {
		return nil
	}
	cur--
	if cur == 0 {
		return k.ActiveRecurringSpendCount.Remove(ctx, authority)
	}
	return k.ActiveRecurringSpendCount.Set(ctx, authority, cur)
}

// ListRecurringSpendsByAuthority walks every schedule owned by `authority`.
func (k Keeper) ListRecurringSpendsByAuthority(ctx context.Context, authority string) ([]types.RecurringSpend, error) {
	var out []types.RecurringSpend
	rng := collections.NewPrefixedPairRange[string, uint64](authority)
	err := k.RecurringSpendsByAuthority.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		rs, err := k.RecurringSpends.Get(ctx, key.K2())
		if err != nil {
			return true, err
		}
		out = append(out, rs)
		return false, nil
	})
	return out, err
}

// ListRecurringSpendsByRecipient walks every schedule payable to `recipient`.
func (k Keeper) ListRecurringSpendsByRecipient(ctx context.Context, recipient string) ([]types.RecurringSpend, error) {
	var out []types.RecurringSpend
	rng := collections.NewPrefixedPairRange[string, uint64](recipient)
	err := k.RecurringSpendsByRecipient.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		rs, err := k.RecurringSpends.Get(ctx, key.K2())
		if err != nil {
			return true, err
		}
		out = append(out, rs)
		return false, nil
	})
	return out, err
}
