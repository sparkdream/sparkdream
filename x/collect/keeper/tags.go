package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/collect/types"
)

// validateTags validates a list of tags against module params and the shared
// x/rep tag registry, then bumps IncrementTagUsage for each tag. Used on
// create-style flows (new content attaches all of these tags).
//
// maxTags is params.MaxTagsPerCollection or params.MaxTagsPerReview; maxLen is
// params.MaxTagLength.
func (k Keeper) validateTags(ctx context.Context, tags []string, maxTags, maxLen uint32, now int64) error {
	if err := k.validateTagsNoIncrement(ctx, tags, maxTags, maxLen); err != nil {
		return err
	}
	return k.incrementTagUsages(ctx, tags, now)
}

// validateTagsNoIncrement runs format, length, duplicate, existence, and
// reserved-tag checks without touching IncrementTagUsage. Used on update flows
// where we only want to increment the delta.
func (k Keeper) validateTagsNoIncrement(ctx context.Context, tags []string, maxTags, maxLen uint32) error {
	if uint32(len(tags)) > maxTags {
		return errorsmod.Wrapf(types.ErrMaxTags, "%d tags, max %d", len(tags), maxTags)
	}
	if len(tags) == 0 {
		return nil
	}
	if k.repKeeper == nil {
		return errorsmod.Wrap(types.ErrTagNotFound, "tag registry not available")
	}

	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if seen[tag] {
			return errorsmod.Wrapf(types.ErrDuplicateTag, "tag %q repeated", tag)
		}
		seen[tag] = true

		if uint32(len(tag)) > maxLen {
			return errorsmod.Wrapf(types.ErrTagTooLong, "tag %q exceeds max length %d", tag, maxLen)
		}
		if !commontypes.ValidateTagFormat(tag) {
			return errorsmod.Wrapf(types.ErrInvalidTag, "tag %q does not match required format", tag)
		}

		exists, err := k.repKeeper.TagExists(ctx, tag)
		if err != nil {
			return errorsmod.Wrapf(err, "failed to check tag %q", tag)
		}
		if !exists {
			return errorsmod.Wrapf(types.ErrTagNotFound, "tag %q not found", tag)
		}
		reserved, err := k.repKeeper.IsReservedTag(ctx, tag)
		if err != nil {
			return errorsmod.Wrapf(err, "failed to check reserved tag %q", tag)
		}
		if reserved {
			return errorsmod.Wrapf(types.ErrReservedTag, "tag %q is reserved", tag)
		}
	}
	return nil
}

// incrementTagUsages bumps IncrementTagUsage for each tag. Caller is expected
// to have already validated the tags (or to be incrementing only a known-valid
// subset like "tags newly added on update").
func (k Keeper) incrementTagUsages(ctx context.Context, tags []string, now int64) error {
	if k.repKeeper == nil {
		if len(tags) == 0 {
			return nil
		}
		return errorsmod.Wrap(types.ErrTagNotFound, "tag registry not available")
	}
	for _, tag := range tags {
		if err := k.repKeeper.IncrementTagUsage(ctx, tag, now); err != nil {
			return errorsmod.Wrap(err, "failed to update tag metadata")
		}
	}
	return nil
}

// addCollectionTagIndex writes (tag, collectionID) entries for each tag.
func (k Keeper) addCollectionTagIndex(ctx context.Context, collID uint64, tags []string) error {
	for _, tag := range tags {
		if err := k.CollectionsByTag.Set(ctx, collections.Join(tag, collID)); err != nil {
			return errorsmod.Wrap(err, "failed to set tag index")
		}
	}
	return nil
}

// removeCollectionTagIndex deletes (tag, collectionID) entries for each tag.
func (k Keeper) removeCollectionTagIndex(ctx context.Context, collID uint64, tags []string) error {
	for _, tag := range tags {
		if err := k.CollectionsByTag.Remove(ctx, collections.Join(tag, collID)); err != nil {
			return errorsmod.Wrap(err, "failed to remove tag index")
		}
	}
	return nil
}

// diffCollectionTags returns the tags newly added and tags removed between
// oldTags and newTags. Order is preserved based on newTags / oldTags.
func diffCollectionTags(oldTags, newTags []string) (added, removed []string) {
	oldSet := make(map[string]struct{}, len(oldTags))
	for _, t := range oldTags {
		oldSet[t] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(newTags))
	for _, t := range newTags {
		newSet[t] = struct{}{}
	}
	for _, t := range newTags {
		if _, had := oldSet[t]; !had {
			added = append(added, t)
		}
	}
	for _, t := range oldTags {
		if _, still := newSet[t]; !still {
			removed = append(removed, t)
		}
	}
	return added, removed
}
