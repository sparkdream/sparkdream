package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// decrementTagUsages drops UsageCount on every tag the post used to carry.
// Used by delete and TTL-tombstone paths (MsgDeletePost, ExpireEphemeralPosts,
// ExpireHiddenPosts) so the rep registry's UsageCount stays in sync with the
// actual live-reference count — without this, dropping a post leaves its
// tags' counts inflated and ExpireTags loses its ability to reclaim slots.
//
// ErrNotFound on an individual tag is non-fatal (the tag may have been GC'd
// between create and delete) — log and continue so the user's transaction
// or the EndBlocker run still completes.
//
// Mirrors x/blog/keeper.Keeper.decrementTagUsages and
// x/collect/keeper.Keeper.decrementTagUsages.
func (k Keeper) decrementTagUsages(ctx context.Context, postID uint64, tags []string) {
	if k.repKeeper == nil || len(tags) == 0 {
		return
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, tag := range tags {
		if err := k.repKeeper.DecrementTagUsage(ctx, tag); err != nil {
			sdkCtx.Logger().Error("failed to decrement tag usage on post tombstone",
				"post_id", postID, "tag", tag, "error", err)
		}
	}
}
