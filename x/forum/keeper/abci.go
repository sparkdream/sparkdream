package keeper

import (
	"context"
	"fmt"
	"math"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// maxPrunePerBlock limits the number of expired posts pruned per EndBlock
// to bound gas consumption.
const maxPrunePerBlock = 100

// EndBlocker runs at the end of each block and prunes expired ephemeral posts.
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	return k.PruneExpiredPosts(ctx, now)
}

// PruneExpiredPosts removes ephemeral posts whose expiration time has passed.
// It walks the ExpirationQueue up to `now` and hard-deletes up to maxPrunePerBlock posts.
func (k Keeper) PruneExpiredPosts(ctx context.Context, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(now, uint64(math.MaxUint64)))

	pruned := 0

	err := k.ExpirationQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrunePerBlock {
			return true, nil // stop walking
		}

		postID := key.K2()

		// Load the post
		post, err := k.Post.Get(ctx, postID)
		if err != nil {
			// Post no longer exists — just remove the stale queue entry
			if removeErr := k.ExpirationQueue.Remove(ctx, key); removeErr != nil {
				sdkCtx.Logger().Error("failed to remove stale expiration queue entry", "post_id", postID, "error", removeErr)
			}
			pruned++
			return false, nil
		}

		// Skip if post was salvaged (expiration cleared but queue entry stale)
		if post.ExpirationTime == 0 {
			if removeErr := k.ExpirationQueue.Remove(ctx, key); removeErr != nil {
				sdkCtx.Logger().Error("failed to remove salvaged expiration queue entry", "post_id", postID, "error", removeErr)
			}
			pruned++
			return false, nil
		}

		// Hard-delete the post
		if removeErr := k.Post.Remove(ctx, postID); removeErr != nil {
			sdkCtx.Logger().Error("failed to remove expired post", "post_id", postID, "error", removeErr)
			// Remove queue entry anyway to avoid infinite retry
		}

		// Clean up associated records (best effort)
		_ = k.PostFlag.Remove(ctx, postID)
		_ = k.HideRecord.Remove(ctx, postID)

		// Remove from queue
		if removeErr := k.ExpirationQueue.Remove(ctx, key); removeErr != nil {
			sdkCtx.Logger().Error("failed to remove expiration queue entry", "post_id", postID, "error", removeErr)
		}

		// Emit event
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"ephemeral_post_pruned",
				sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
				sdk.NewAttribute("expiration_time", fmt.Sprintf("%d", key.K1())),
			),
		)

		pruned++
		return false, nil
	})

	if err != nil {
		sdkCtx.Logger().Error("error walking expiration queue", "error", err)
		return nil // don't halt chain
	}

	if pruned > 0 {
		sdkCtx.Logger().Info("pruned expired ephemeral posts", "count", pruned)
	}

	return nil
}
