package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
)

// GetSalvationCounters returns the current salvation counters for a member.
// Returns zeros if the member record is not found.
func (k Keeper) GetSalvationCounters(ctx context.Context, addr string) (uint32, int64, error) {
	member, err := k.Member.Get(ctx, addr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	return member.EpochSalvations, member.LastSalvationEpoch, nil
}

// UpdateSalvationCounters updates the salvation counters on a member record.
// No-op if the member record is not found.
func (k Keeper) UpdateSalvationCounters(ctx context.Context, addr string, epochSalvations uint32, lastEpoch int64) error {
	member, err := k.Member.Get(ctx, addr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil
		}
		return err
	}
	member.EpochSalvations = epochSalvations
	member.LastSalvationEpoch = lastEpoch
	return k.Member.Set(ctx, addr, member)
}
