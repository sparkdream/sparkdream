package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/forum/types"
)

// AddEphemeralAuthorIndex records that `author` has an ephemeral post (root or
// reply, since forum unifies them under one Post record) with id `postID`
// pending eventual promotion. No-op for module-account (anonymous) authors —
// they cannot "become a member" so the membership-driven promotion queue does
// not apply to them.
func (k Keeper) AddEphemeralAuthorIndex(ctx context.Context, author string, postID uint64) error {
	if author == "" || author == k.forumModuleAddrString() {
		return nil
	}
	return k.EphemeralByAuthor.Set(ctx, collections.Join(author, postID))
}

// RemoveEphemeralAuthorIndex idempotently removes an (author, postID) entry.
func (k Keeper) RemoveEphemeralAuthorIndex(ctx context.Context, author string, postID uint64) {
	if author == "" {
		return
	}
	_ = k.EphemeralByAuthor.Remove(ctx, collections.Join(author, postID))
}

// EnqueueAuthorForPromotion adds `author` to the promotion queue with the
// current block height stamped as the enqueue marker. Idempotent (overwrites
// existing entry with the newer block height). No-op for module-account
// authors.
func (k Keeper) EnqueueAuthorForPromotion(ctx context.Context, author string) error {
	if author == "" || author == k.forumModuleAddrString() {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return k.PromotionQueue.Set(ctx, author, sdkCtx.BlockHeight())
}

// IsAuthorQueuedForPromotion reports whether `author` is currently in the
// promotion queue. Exposed for tests.
func (k Keeper) IsAuthorQueuedForPromotion(ctx context.Context, author string) bool {
	has, _ := k.PromotionQueue.Has(ctx, author)
	return has
}

// drainPromotionQueue eagerly promotes up to `budget` ephemeral posts to
// permanent across all queued authors, iterating the queue in bech32-string
// (i.e. iterator) order. Authors whose ephemeral content is fully drained are
// removed from the queue. Returns the count actually promoted.
func (k Keeper) drainPromotionQueue(ctx context.Context, budget int) int {
	if budget <= 0 {
		return 0
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// Snapshot queue entries so we can mutate the map safely during iteration.
	var authors []string
	_ = k.PromotionQueue.Walk(ctx, nil, func(author string, _ int64) (bool, error) {
		authors = append(authors, author)
		return false, nil
	})

	promoted := 0
	for _, author := range authors {
		if promoted >= budget {
			return promoted
		}
		done := k.promoteAuthorEphemerals(ctx, sdkCtx, author, blockTime, &promoted, budget)
		if done {
			_ = k.PromotionQueue.Remove(ctx, author)
		}
	}
	return promoted
}

// promoteAuthorEphemerals walks EphemeralByAuthor for `author` and promotes
// each entry to permanent until either the per-block budget is exhausted or
// the author has no more ephemeral content. Returns true iff fully drained.
func (k Keeper) promoteAuthorEphemerals(
	ctx context.Context,
	sdkCtx sdk.Context,
	author string,
	blockTime int64,
	promoted *int,
	budget int,
) bool {
	// Snapshot (author, postID) entries to avoid mid-iteration mutation.
	var postIDs []uint64
	rng := collections.NewPrefixedPairRange[string, uint64](author)
	_ = k.EphemeralByAuthor.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		postIDs = append(postIDs, key.K2())
		return false, nil
	})

	for _, id := range postIDs {
		if *promoted >= budget {
			return false
		}
		k.promoteEphemeralPost(ctx, sdkCtx, author, id, blockTime, promoted)
	}
	return true
}

// promoteEphemeralPost promotes a single ephemeral post identified by `id` to
// permanent. Handles the cases where the post is gone, already permanent, or
// in a non-promotable state by cleaning up the index entry.
func (k Keeper) promoteEphemeralPost(
	ctx context.Context,
	sdkCtx sdk.Context,
	author string,
	id uint64,
	blockTime int64,
	promoted *int,
) {
	post, err := k.Post.Get(ctx, id)
	if err != nil {
		k.RemoveEphemeralAuthorIndex(ctx, author, id)
		return
	}
	// Author mismatch, already permanent, or tombstoned — drop the stale entry.
	if post.Author != author || post.ExpirationTime == 0 ||
		post.Status == types.PostStatus_POST_STATUS_DELETED {
		k.RemoveEphemeralAuthorIndex(ctx, author, id)
		return
	}

	oldExpiresAt := post.ExpirationTime
	post.ExpirationTime = 0
	if post.ConvictionSustained {
		post.ConvictionSustained = false
	}
	if err := k.Post.Set(ctx, id, post); err != nil {
		return
	}
	_ = k.ExpirationQueue.Remove(ctx, collections.Join(oldExpiresAt, id))
	k.RemoveEphemeralAuthorIndex(ctx, author, id)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"forum.post.upgraded",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("creator", author),
		sdk.NewAttribute("via", "promotion_queue"),
	))
	*promoted++
	_ = blockTime
}

// forumModuleAddrString is the bech32 address of the forum module account,
// used to skip anonymous (shielded) posts from the promotion queue. Their
// "author" is the module account, which cannot become a member.
func (k Keeper) forumModuleAddrString() string {
	return authtypes.NewModuleAddress(types.ModuleName).String()
}
