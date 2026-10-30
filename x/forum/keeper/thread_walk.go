package keeper

import (
	"context"

	"cosmossdk.io/collections"
)

// threadReplyIDs returns the ids of every reply in a thread from the
// PostsByRoot index, stopping once more than limit ids were collected (the
// caller then refuses the operation). limit == 0 means no limit.
func (k Keeper) threadReplyIDs(ctx context.Context, rootID uint64, limit uint64) ([]uint64, error) {
	var ids []uint64
	err := k.PostsByRoot.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](rootID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			ids = append(ids, key.K2())
			return limit > 0 && uint64(len(ids)) > limit, nil
		})
	return ids, err
}
