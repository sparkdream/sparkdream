package keeper

import (
	"context"
	"fmt"
	"math"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// maxPrunePerBlock limits the number of expired posts pruned per EndBlock
// to bound gas consumption.
const maxPrunePerBlock = 100

// maxBountyExpirations limits bounty expirations per block.
const maxBountyExpirations = 50

// maxHiddenExpiry limits hidden post expiry processing per block.
const maxHiddenExpiry = 50

// EndBlocker runs at the end of each block.
// Phase 1: Ephemeral post pruning
// Phase 2: Hidden post expiration
// Phase 3: Bounty expiration
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Phase 1: Prune expired ephemeral posts
	if err := k.PruneExpiredPosts(ctx, now); err != nil {
		sdkCtx.Logger().Error("error pruning expired posts", "error", err)
	}

	// Phase 2: Expire hidden posts (hidden_at + hidden_expiration exceeded)
	if err := k.ExpireHiddenPosts(ctx, now); err != nil {
		sdkCtx.Logger().Error("error expiring hidden posts", "error", err)
	}

	// Phase 3: Expire bounties past their deadline
	if err := k.ExpireBounties(ctx, now); err != nil {
		sdkCtx.Logger().Error("error expiring bounties", "error", err)
	}

	return nil
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

		// Conviction renewal check: if post has initiative_id and conviction meets threshold, renew TTL
		if post.InitiativeId > 0 && k.repKeeper != nil {
			params, pErr := k.Params.Get(ctx)
			if pErr == nil && params.ConvictionRenewalThreshold.IsPositive() {
				conviction, cErr := k.repKeeper.GetContentConviction(ctx, reptypes.StakeTargetType_STAKE_TARGET_FORUM_CONTENT, postID)
				if cErr == nil && conviction.GTE(params.ConvictionRenewalThreshold) {
					blockTime := sdkCtx.BlockTime().Unix()
					newExpiresAt := blockTime + params.ConvictionRenewalPeriod
					// Remove old queue entry
					if removeErr := k.ExpirationQueue.Remove(ctx, key); removeErr != nil {
						sdkCtx.Logger().Error("failed to remove old queue entry for conviction renewal", "post_id", postID, "error", removeErr)
					}
					if !post.ConvictionSustained {
						post.ConvictionSustained = true
						post.ExpirationTime = newExpiresAt
						_ = k.Post.Set(ctx, postID, post)
						_ = k.ExpirationQueue.Set(ctx, collections.Join(newExpiresAt, postID))
						sdkCtx.EventManager().EmitEvent(sdk.NewEvent("forum.post.conviction_sustained",
							sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
							sdk.NewAttribute("conviction_score", conviction.String()),
							sdk.NewAttribute("new_expires_at", fmt.Sprintf("%d", newExpiresAt)),
						))
					} else {
						post.ExpirationTime = newExpiresAt
						_ = k.Post.Set(ctx, postID, post)
						_ = k.ExpirationQueue.Set(ctx, collections.Join(newExpiresAt, postID))
						sdkCtx.EventManager().EmitEvent(sdk.NewEvent("forum.post.renewed",
							sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
							sdk.NewAttribute("conviction_score", conviction.String()),
							sdk.NewAttribute("new_expires_at", fmt.Sprintf("%d", newExpiresAt)),
						))
					}
					pruned++
					return false, nil // continue walking
				}
				// Conviction below threshold — clear flag, proceed to hard-delete
				if post.ConvictionSustained {
					post.ConvictionSustained = false
					_ = k.Post.Set(ctx, postID, post)
				}
			}
		}

		// Remove initiative link on hard-delete (best effort)
		if post.InitiativeId > 0 && k.repKeeper != nil {
			if linkErr := k.repKeeper.RemoveContentInitiativeLink(ctx, post.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_FORUM_CONTENT), postID); linkErr != nil {
				sdkCtx.Logger().Error("failed to remove initiative link on prune", "post_id", postID, "error", linkErr)
			}
		}

		// FORUM-S2-8: drop secondary index entries for the now-deleted post.
		if post.ParentId == 0 {
			_ = k.PostsByUpvotes.Remove(ctx, collections.Join(post.UpvoteCount, postID))
			if post.Pinned {
				_ = k.PostsByPinned.Remove(ctx, collections.Join(post.CategoryId, postID))
			}
		}

		// Hard-delete the post
		if removeErr := k.Post.Remove(ctx, postID); removeErr != nil {
			sdkCtx.Logger().Error("failed to remove expired post", "post_id", postID, "error", removeErr)
			// Remove queue entry anyway to avoid infinite retry
		}

		// Clean up associated records (best effort)
		_ = k.PostFlag.Remove(ctx, postID)
		_ = k.HideRecord.Remove(ctx, postID)
		_ = k.ThreadLockRecord.Remove(ctx, postID)
		_ = k.ThreadMoveRecord.Remove(ctx, postID)
		_ = k.ThreadMetadata.Remove(ctx, postID)
		_ = k.ThreadFollowCount.Remove(ctx, postID)

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

// ExpireHiddenPosts checks posts in HIDDEN status and soft-deletes them
// if they have been hidden longer than the configured hidden_expiration period.
func (k Keeper) ExpireHiddenPosts(ctx context.Context, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	hiddenExpiry := int64(types.DefaultHiddenExpiration)
	if hiddenExpiry <= 0 {
		return nil // disabled
	}

	expired := 0

	// Walk hide records to find expired hidden posts
	err := k.HideRecord.Walk(ctx, nil, func(postID uint64, hr types.HideRecord) (bool, error) {
		if expired >= maxHiddenExpiry {
			return true, nil // stop
		}

		// Check if hidden long enough
		if hr.HiddenAt+hiddenExpiry > now {
			return false, nil // not yet expired
		}

		// Load post and verify it's still hidden (not restored via appeal)
		post, err := k.Post.Get(ctx, postID)
		if err != nil {
			// Post gone, clean up stale hide record
			_ = k.HideRecord.Remove(ctx, postID)
			expired++
			return false, nil
		}

		if post.Status != types.PostStatus_POST_STATUS_HIDDEN {
			// Post was restored, clean up stale hide record
			_ = k.HideRecord.Remove(ctx, postID)
			return false, nil
		}

		// Soft-delete the post
		post.Status = types.PostStatus_POST_STATUS_DELETED
		post.Content = "" // clear content to reclaim space
		if setErr := k.Post.Set(ctx, postID, post); setErr != nil {
			sdkCtx.Logger().Error("failed to delete expired hidden post", "post_id", postID, "error", setErr)
			return false, nil
		}
		// FORUM-S2-8: drop from secondary indexes; the post is no longer
		// eligible for TopPosts/PinnedPosts.
		if post.ParentId == 0 {
			_ = k.PostsByUpvotes.Remove(ctx, collections.Join(post.UpvoteCount, post.PostId))
			if post.Pinned {
				_ = k.PostsByPinned.Remove(ctx, collections.Join(post.CategoryId, post.PostId))
			}
		}

		// Release the sentinel's reserved bond before dropping the hide record.
		// The hide passed the appeal window without being appealed, so the
		// reservation must be freed so the sentinel's available bond recovers.
		if k.repKeeper != nil && hr.Sentinel != "" && hr.CommittedAmount != "" {
			if committed, ok := sdkmath.NewIntFromString(hr.CommittedAmount); ok && committed.IsPositive() {
				if err := k.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, hr.Sentinel, committed); err != nil {
					sdkCtx.Logger().Warn("failed to release sentinel bond on hide expiry",
						"post_id", postID, "sentinel", hr.Sentinel, "error", err)
				}
			}
		}

		// Clean up hide record
		_ = k.HideRecord.Remove(ctx, postID)

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hidden_post_expired",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
			sdk.NewAttribute("hidden_at", fmt.Sprintf("%d", hr.HiddenAt)),
		))

		expired++
		return false, nil
	})

	if err != nil {
		return nil // don't halt chain
	}

	if expired > 0 {
		sdkCtx.Logger().Info("expired hidden posts", "count", expired)
	}

	return nil
}

// ExpireBounties checks active bounties and marks expired ones.
// Expired bounties have their escrowed funds returned to the creator.
func (k Keeper) ExpireBounties(ctx context.Context, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	expired := 0

	err := k.Bounty.Walk(ctx, nil, func(id uint64, bounty types.Bounty) (bool, error) {
		if expired >= maxBountyExpirations {
			return true, nil
		}

		// Only expire active bounties past their deadline
		if bounty.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE {
			return false, nil
		}
		if bounty.ExpiresAt <= 0 || bounty.ExpiresAt > now {
			return false, nil
		}

		// Mark bounty as expired
		bounty.Status = types.BountyStatus_BOUNTY_STATUS_EXPIRED
		if setErr := k.Bounty.Set(ctx, id, bounty); setErr != nil {
			sdkCtx.Logger().Error("failed to expire bounty", "bounty_id", id, "error", setErr)
			return false, nil
		}
		// FORUM-S2-8: drop the entry from BountiesByExpiry now that the
		// bounty is no longer ACTIVE.
		_ = k.BountiesByExpiry.Remove(ctx, collections.Join(bounty.ExpiresAt, id))

		// Refund escrowed amount to creator (best effort)
		if bounty.Amount != "" && bounty.Creator != "" {
			creatorAddr, addrErr := sdk.AccAddressFromBech32(bounty.Creator)
			if addrErr == nil {
				refundCoin, coinErr := sdk.ParseCoinNormalized(bounty.Amount)
				if coinErr == nil && refundCoin.IsPositive() {
					if sendErr := k.bankKeeper.SendCoinsFromModuleToAccount(
						ctx, types.ModuleName, creatorAddr, sdk.NewCoins(refundCoin),
					); sendErr != nil {
						sdkCtx.Logger().Error("failed to refund expired bounty",
							"bounty_id", id, "amount", bounty.Amount, "error", sendErr)
					}
				}
			}
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("bounty_expired",
			sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("root_id", fmt.Sprintf("%d", bounty.ThreadId)),
			sdk.NewAttribute("amount", bounty.Amount),
		))

		expired++
		return false, nil
	})

	if err != nil {
		return nil // don't halt chain
	}

	if expired > 0 {
		sdkCtx.Logger().Info("expired bounties", "count", expired)
	}

	return nil
}

