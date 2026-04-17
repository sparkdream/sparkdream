package keeper

import (
	"context"

	"sparkdream/x/rep/types"
)

// TagExists reports whether a tag is registered.
func (k Keeper) TagExists(ctx context.Context, name string) (bool, error) {
	return k.Tag.Has(ctx, name)
}

// IsReservedTag reports whether a tag is in the reserved registry.
func (k Keeper) IsReservedTag(ctx context.Context, name string) (bool, error) {
	return k.ReservedTag.Has(ctx, name)
}

// GetTag returns the tag metadata for the given name. Returns an error
// wrapping collections.ErrNotFound when the tag does not exist.
func (k Keeper) GetTag(ctx context.Context, name string) (types.Tag, error) {
	return k.Tag.Get(ctx, name)
}

// GetReservedTag returns the reserved-tag entry for the given name.
func (k Keeper) GetReservedTag(ctx context.Context, name string) (types.ReservedTag, error) {
	return k.ReservedTag.Get(ctx, name)
}

// IncrementTagUsage increments the usage count and updates last_used_at
// on the named tag. Used by content modules when a tag is referenced.
func (k Keeper) IncrementTagUsage(ctx context.Context, name string, timestamp int64) error {
	tag, err := k.Tag.Get(ctx, name)
	if err != nil {
		return err
	}
	tag.UsageCount++
	tag.LastUsedAt = timestamp
	return k.Tag.Set(ctx, name, tag)
}

// SetTag writes a tag entry. Used by genesis and internal callers.
func (k Keeper) SetTag(ctx context.Context, tag types.Tag) error {
	return k.Tag.Set(ctx, tag.Name, tag)
}

// RemoveTag deletes a tag entry. Used by expiry GC and moderation flows
// (e.g., forum's ResolveTagReport when action = remove).
func (k Keeper) RemoveTag(ctx context.Context, name string) error {
	return k.Tag.Remove(ctx, name)
}

// SetReservedTag writes a reserved-tag entry. Used by genesis and by
// moderation flows (e.g., forum's ResolveTagReport when action = reserve).
func (k Keeper) SetReservedTag(ctx context.Context, rt types.ReservedTag) error {
	return k.ReservedTag.Set(ctx, rt.Name, rt)
}

// RemoveReservedTag deletes a reserved-tag entry.
func (k Keeper) RemoveReservedTag(ctx context.Context, name string) error {
	return k.ReservedTag.Remove(ctx, name)
}
