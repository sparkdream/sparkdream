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

// IncrementTagUsage bumps usage_count and refreshes last_used_at on the
// named tag. Used by content modules when a tag is referenced.
//
// LastUsedAt drives the GC schedule: ExpireTags reclaims tags where
// `last_used_at + DefaultTagExpiration <= now`. Every reference therefore
// rolls the deadline forward; tags that fall idle hit their natural expiry
// and get reclaimed.
func (k Keeper) IncrementTagUsage(ctx context.Context, name string, timestamp int64) error {
	tag, err := k.Tag.Get(ctx, name)
	if err != nil {
		return err
	}
	tag.UsageCount++
	tag.LastUsedAt = timestamp
	return k.Tag.Set(ctx, name, tag)
}

// DecrementTagUsage decrements the usage count on the named tag without
// touching LastUsedAt (a tag being released from content isn't a "use",
// and dropping shouldn't pull the GC schedule forward — the tag still
// expires `last_used_at + DefaultTagExpiration` after its last real
// reference, whether or not someone has since released theirs). Used by
// content modules when a tag is removed from a piece of content — e.g.
// blog/forum edit dropping a tag, collect update dropping a tag, content
// deletion.
//
// UsageCount is clamped at 0: an underflow attempt means an upstream caller
// double-decremented, which is the kind of drift this helper exists to fix
// in the first place — better to floor silently than to abort the user's
// transaction. Returns ErrNotFound if the tag has been GC'd; callers
// typically log and continue.
func (k Keeper) DecrementTagUsage(ctx context.Context, name string) error {
	tag, err := k.Tag.Get(ctx, name)
	if err != nil {
		return err
	}
	if tag.UsageCount > 0 {
		tag.UsageCount--
	}
	return k.Tag.Set(ctx, name, tag)
}

// SetTag writes a tag entry. Used by genesis and internal callers. A new
// entry advances the registry counter (REP-E-09).
func (k Keeper) SetTag(ctx context.Context, tag types.Tag) error {
	existed, err := k.Tag.Has(ctx, tag.Name)
	if err != nil {
		return err
	}
	if err := k.Tag.Set(ctx, tag.Name, tag); err != nil {
		return err
	}
	if existed {
		return nil
	}
	return k.TagCount.Set(ctx, k.GetTagCount(ctx)+1)
}

// RemoveTag deletes a tag entry and moves the registry counter back. Used by
// expiry GC and moderation flows (e.g., ResolveTagReport when action =
// remove).
func (k Keeper) RemoveTag(ctx context.Context, name string) error {
	existed, err := k.Tag.Has(ctx, name)
	if err != nil {
		return err
	}
	if !existed {
		return nil
	}
	if err := k.Tag.Remove(ctx, name); err != nil {
		return err
	}
	if n := k.GetTagCount(ctx); n > 0 {
		return k.TagCount.Set(ctx, n-1)
	}
	return nil
}

// GetTagCount returns the number of registered tags (0 when unset).
func (k Keeper) GetTagCount(ctx context.Context) uint64 {
	n, err := k.TagCount.Get(ctx)
	if err != nil {
		return 0
	}
	return n
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
