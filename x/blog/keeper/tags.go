package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/blog/types"
)

// validatePostTags validates a list of tags for use on a post and updates tag
// metadata via the x/rep tag registry. Unknown tags are rejected (no
// auto-creation), reserved tags are rejected, duplicates are rejected. On
// success, IncrementTagUsage is called for every tag in the list.
//
// Mirrors x/forum/keeper.msgServer.validatePostTags.
func (k Keeper) validatePostTags(ctx context.Context, tags []string, now int64) error {
	if err := k.validatePostTagsNoIncrement(ctx, tags); err != nil {
		return err
	}
	for _, tagName := range tags {
		if err := k.repKeeper.IncrementTagUsage(ctx, tagName, now); err != nil {
			return errorsmod.Wrap(err, "failed to update tag metadata")
		}
	}
	return nil
}

// validatePostTagsNoIncrement runs the same validation as validatePostTags
// (format, length, registry existence, reserved check, duplicates, count) but
// does not touch tag usage metadata. Callers can then selectively increment
// only genuinely new tags on update paths.
func (k Keeper) validatePostTagsNoIncrement(ctx context.Context, tags []string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return errorsmod.Wrap(err, "failed to get params")
	}

	if uint32(len(tags)) > params.MaxTagsPerPost {
		return errorsmod.Wrapf(types.ErrTagLimitExceeded, "max %d tags per post", params.MaxTagsPerPost)
	}
	if k.repKeeper == nil {
		return errorsmod.Wrap(types.ErrTagNotFound, "tag registry not available")
	}

	seen := make(map[string]bool, len(tags))
	for _, tagName := range tags {
		if seen[tagName] {
			return errorsmod.Wrapf(types.ErrInvalidTag, "duplicate tag: %s", tagName)
		}
		seen[tagName] = true

		if uint32(len(tagName)) > params.MaxTagLength {
			return errorsmod.Wrapf(types.ErrMaxTagLength, "tag %q exceeds max length %d", tagName, params.MaxTagLength)
		}

		if !commontypes.ValidateTagFormat(tagName) {
			return errorsmod.Wrapf(types.ErrInvalidTag, "tag %q does not match required format", tagName)
		}

		exists, err := k.repKeeper.TagExists(ctx, tagName)
		if err != nil {
			return errorsmod.Wrapf(err, "failed to check tag %q", tagName)
		}
		if !exists {
			return errorsmod.Wrapf(types.ErrTagNotFound, "tag %q not found", tagName)
		}

		reserved, err := k.repKeeper.IsReservedTag(ctx, tagName)
		if err != nil {
			return errorsmod.Wrapf(err, "failed to check reserved tag %q", tagName)
		}
		if reserved {
			return errorsmod.Wrapf(types.ErrReservedTag, "tag %q is reserved", tagName)
		}
	}

	return nil
}

// tagPostIndexKey returns the secondary-index key used to list posts by tag.
// Layout: {tag}/{postID big-endian} — {tag} already has a trailing '/' from
// the caller side when used as a prefix, but when writing the full key we
// join on '/' to avoid collisions between tags sharing a common prefix.
func tagPostIndexKey(tag string, postID uint64) []byte {
	return append([]byte(tag+"/"), GetPostIDBytes(postID)...)
}

// addTagIndexEntries writes tag → postID entries for each tag on the post.
func (k Keeper) addTagIndexEntries(ctx context.Context, postID uint64, tags []string) {
	if len(tags) == 0 {
		return
	}
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	tagStore := prefix.NewStore(storeAdapter, []byte(types.TagPostKey))
	for _, tag := range tags {
		tagStore.Set(tagPostIndexKey(tag, postID), []byte{0x01})
	}
}

// removeTagIndexEntries deletes tag → postID entries for each tag on the post.
func (k Keeper) removeTagIndexEntries(ctx context.Context, postID uint64, tags []string) {
	if len(tags) == 0 {
		return
	}
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	tagStore := prefix.NewStore(storeAdapter, []byte(types.TagPostKey))
	for _, tag := range tags {
		tagStore.Delete(tagPostIndexKey(tag, postID))
	}
}

// updateTagIndexEntries diffs the old vs new tag sets for a post and writes
// only the deltas — new tags get an entry, removed tags get their entry
// deleted. Tags present in both are untouched.
func (k Keeper) updateTagIndexEntries(ctx context.Context, postID uint64, oldTags, newTags []string) {
	oldSet := make(map[string]struct{}, len(oldTags))
	for _, t := range oldTags {
		oldSet[t] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(newTags))
	for _, t := range newTags {
		newSet[t] = struct{}{}
	}

	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	tagStore := prefix.NewStore(storeAdapter, []byte(types.TagPostKey))

	for _, t := range newTags {
		if _, had := oldSet[t]; !had {
			tagStore.Set(tagPostIndexKey(t, postID), []byte{0x01})
		}
	}
	for _, t := range oldTags {
		if _, still := newSet[t]; !still {
			tagStore.Delete(tagPostIndexKey(t, postID))
		}
	}
}
