package keeper

import (
	"context"

	commontypes "sparkdream/x/common/types"
)

// Compile-time interface check.
var _ commontypes.TagKeeper = Keeper{}

func (k Keeper) TagExists(ctx context.Context, name string) (bool, error) {
	return k.Tag.Has(ctx, name)
}

func (k Keeper) IsReservedTag(ctx context.Context, name string) (bool, error) {
	return k.ReservedTag.Has(ctx, name)
}

func (k Keeper) GetTag(ctx context.Context, name string) (commontypes.Tag, error) {
	return k.Tag.Get(ctx, name)
}

func (k Keeper) IncrementTagUsage(ctx context.Context, name string, timestamp int64) error {
	tag, err := k.Tag.Get(ctx, name)
	if err != nil {
		return err
	}
	tag.UsageCount++
	tag.LastUsedAt = timestamp
	return k.Tag.Set(ctx, name, tag)
}
