package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"

	"sparkdream/x/storage/types"
)

// counter returns an address's count of kind in the current window: zero
// when the stored window has rolled.
func (k Keeper) counter(ctx context.Context, addr string, kind types.CounterKind, now int64, p types.Params) (types.Counter, error) {
	c, err := k.Counters.Get(ctx, collections.Join(addr, int32(kind)))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Counter{WindowStart: now}, nil
		}
		return types.Counter{}, err
	}
	if now >= c.WindowStart+p.CounterWindowBlocks {
		return types.Counter{WindowStart: now}, nil
	}
	return c, nil
}

// bump adds one to an address's count of kind and returns the new counter.
func (k Keeper) bump(ctx context.Context, addr string, kind types.CounterKind, now int64, p types.Params) (types.Counter, error) {
	c, err := k.counter(ctx, addr, kind, now, p)
	if err != nil {
		return types.Counter{}, err
	}
	c.Count++
	return c, k.Counters.Set(ctx, collections.Join(addr, int32(kind)), c)
}

// bumpCapped adds one unless the address already reached max in this window.
func (k Keeper) bumpCapped(ctx context.Context, addr string, kind types.CounterKind, max uint32, now int64, p types.Params) error {
	c, err := k.counter(ctx, addr, kind, now, p)
	if err != nil {
		return err
	}
	if c.Count >= uint64(max) {
		return types.ErrRateLimited.Wrapf("%s: %d per window", kind, max)
	}
	c.Count++
	return k.Counters.Set(ctx, collections.Join(addr, int32(kind)), c)
}
