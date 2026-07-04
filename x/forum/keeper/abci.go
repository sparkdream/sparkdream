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

// maxAutoConfirmPerBlock limits sentinel accepted-reply proposals auto-confirmed
// (or extended) per block to bound gas.
const maxAutoConfirmPerBlock = 50

// EndBlocker runs at the end of each block.
// Phase 0: Drain the membership-driven promotion queue (eager ephemeral →
//
//	permanent for posts owned by recently-admitted members). Capped per
//	block by params.max_promotions_per_block so it can't blow block gas.
//
// Phase 1: Process expired ephemeral posts (upgrade-if-now-member, then
//
//	conviction-renew if anonymous, else hard-delete).
//
// Phase 2: Hidden post expiration
// Phase 3: Bounty expiration
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Phase 0: Drain the membership-driven promotion queue. Skipped if
	// the per-block budget is zero (e.g. emergency throttle via gov).
	params, err := k.Params.Get(ctx)
	if err == nil && params.MaxPromotionsPerBlock > 0 {
		k.drainPromotionQueue(ctx, int(params.MaxPromotionsPerBlock))
	}

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

	// Phase 3b: Auto-confirm sentinel accepted-reply proposals whose timeout
	// has elapsed (or grant the one-time inactivity extension).
	if err := k.ProcessProposalAutoConfirm(ctx, now); err != nil {
		sdkCtx.Logger().Error("error auto-confirming proposals", "error", err)
	}

	// Phase 4: Stream forum reputation to post authors from active
	// PostConvictionStakes. Per-stake errors log but never abort the loop.
	if err := k.AccruePostConvictions(ctx); err != nil {
		sdkCtx.Logger().Error("error accruing post conviction stakes", "error", err)
	}

	// Phase 5: GC released-and-retained PostConvictionStakes whose retention
	// window has passed (lock_window + hide_appeal_cooldown + 1d). Bounded
	// per block by maxStakeGcPerBlock so a large backlog never dominates
	// gas.
	if err := k.GcReleasedPostConvictionStakes(ctx); err != nil {
		sdkCtx.Logger().Error("error gc'ing released post conviction stakes", "error", err)
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

		// Upgrade path: if the author is now an active member (e.g. admitted
		// after they posted), flip the post to permanent rather than pruning
		// it. The eager flow runs in the promotion-queue drain (Phase 0); this
		// is the lazy fallback that catches posts whose authors joined after
		// the queue was already drained — or that were created by an existing
		// member before the EphemeralByAuthor index existed. Skip the upgrade
		// when the post is no longer visible (DELETED/HIDDEN) — let those go
		// down the normal hard-delete path.
		if post.Status == types.PostStatus_POST_STATUS_ACTIVE &&
			post.Author != "" && post.Author != k.forumModuleAddrString() &&
			k.IsMember(ctx, post.Author) {
			oldExpiresAt := post.ExpirationTime
			post.ExpirationTime = 0
			if post.ConvictionSustained {
				post.ConvictionSustained = false
			}
			if setErr := k.Post.Set(ctx, postID, post); setErr == nil {
				_ = k.ExpirationQueue.Remove(ctx, collections.Join(oldExpiresAt, postID))
				k.RemoveEphemeralAuthorIndex(ctx, post.Author, postID)
				sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
					"forum.post.upgraded",
					sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
					sdk.NewAttribute("creator", post.Author),
					sdk.NewAttribute("via", "ttl_member_upgrade"),
				))
				pruned++
				return false, nil
			}
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

		// Drop the EphemeralByAuthor entry now that the post is being
		// hard-deleted; without this, idle index entries would point at
		// non-existent posts and the next promotion-queue drain would have
		// to scrub them.
		k.RemoveEphemeralAuthorIndex(ctx, post.Author, postID)

		// Drop rep-registry UsageCount for every tag the post carried. The post
		// is being hard-deleted by TTL; without this, ephemeral-post churn
		// would inflate UsageCount monotonically and ExpireTags would lose
		// its grip on idle tags.
		k.decrementTagUsages(ctx, postID, post.Tags)

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
		k.clearProposalCounts(ctx, postID)

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

		// Accountability hooks fire here — BEFORE the post's tags are cleared
		// and the post is soft-deleted, because both hooks read the
		// pre-tombstone state. Failures log but never halt: a hide-finalize
		// must always tombstone the post or the moderation flow stalls.

		// Tier 0: author per-tag rep slash. The author put the post in these
		// tags; an unappealed hide says the community rejected it, so claw back
		// rep in each tag. DeductReputation floors at zero — no harm if the
		// author has no rep in the tag (e.g. they never earned any).
		if k.repKeeper != nil && len(post.Tags) > 0 {
			paramsForSlash, paramsErr := k.Params.Get(ctx)
			if paramsErr == nil && !paramsForSlash.AuthorRepSlash.IsNil() && paramsForSlash.AuthorRepSlash.IsPositive() {
				authorBytes, addrErr := k.addressCodec.StringToBytes(post.Author)
				if addrErr == nil {
					for _, tag := range post.Tags {
						if err := k.repKeeper.DeductReputation(ctx, sdk.AccAddress(authorBytes), tag, paramsForSlash.AuthorRepSlash); err != nil {
							sdkCtx.Logger().Warn("failed to slash author rep on hide expiry",
								"post_id", postID, "author", post.Author, "tag", tag, "error", err)
						}
					}
				}
			}
		}

		// Tier 0b: post conviction-stake slash. Every active (or
		// recently-released-but-not-GC'd) PostConvictionStake on this post
		// gets:
		//   - exact author forum-rep clawback per tag (uses each stake's
		//     accrued_rep_per_tag, so we deduct precisely what was credited
		//     — no over-slashing the author beyond what stakes produced)
		//   - staker_slash_bps burn on the staker's locked DREAM
		//   - remaining locked DREAM unlocked + stake closed
		// Best-effort: SlashStakesForPost logs per-stake errors but never
		// halts the tombstone path.
		if err := k.SlashStakesForPost(ctx, postID); err != nil {
			sdkCtx.Logger().Warn("post conviction slash failed on hide expiry",
				"post_id", postID, "error", err)
		}

		// Tier 1: promoter MemberWarning. The member who called
		// MsgMakePostPermanent on this post vouched for content the community
		// rejected. PromotedBy is only set when the promoter is different from
		// the author (self-promote is not a vouching act).
		if k.repKeeper != nil && post.PromotedBy != "" && post.PromotedBy != post.Author {
			if err := k.repKeeper.IssueWarning(
				ctx,
				post.PromotedBy,
				k.GetModuleAddress(),
				"promoted_hidden_content",
				[]uint64{postID},
			); err != nil {
				sdkCtx.Logger().Warn("failed to issue promoter warning on hide expiry",
					"post_id", postID, "promoter", post.PromotedBy, "error", err)
			}
		}

		// Drop rep-registry UsageCount for every tag the post carried — this is
		// the second half of the "hidden post times out" tombstone path. Same
		// rationale as the ephemeral-prune and explicit-delete paths.
		k.decrementTagUsages(ctx, postID, post.Tags)

		// Soft-delete the post
		post.Status = types.PostStatus_POST_STATUS_DELETED
		post.Content = "" // clear content to reclaim space
		post.Tags = nil   // sever the (now-decremented) tag references
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
				if err := k.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, hr.Sentinel, committed); err != nil {
					sdkCtx.Logger().Warn("failed to release sentinel bond on hide expiry",
						"post_id", postID, "sentinel", hr.Sentinel, "error", err)
				}
			}
		}

		// Finalize sentinel counters for an UNAPPEALED hide that has now expired:
		// the hide stood by default, so count it as unchallenged and clear it from
		// the pending tally. An appealed hide was already tallied as
		// upheld/overturned (and its pending slot decremented) at appeal-resolve,
		// so skip those to avoid double-counting. Gov hides (Sentinel == "")
		// carry no sentinel counters.
		if hr.Sentinel != "" && !hr.Appealed {
			local, lerr := k.SentinelActivity.Get(ctx, hr.Sentinel)
			if lerr == nil {
				local.UnchallengedHides++
				if local.PendingHideCount > 0 {
					local.PendingHideCount--
				}
				if err := k.SentinelActivity.Set(ctx, hr.Sentinel, local); err != nil {
					sdkCtx.Logger().Warn("failed to record unchallenged hide on expiry",
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

		// Refund escrowed amount to creator (best effort). Amount is a bare
		// integer string in the bond denom, as written by CreateBounty.
		if bounty.Amount != "" && bounty.Creator != "" {
			creatorAddr, addrErr := sdk.AccAddressFromBech32(bounty.Creator)
			if addrErr == nil {
				refundAmount, ok := sdkmath.NewIntFromString(bounty.Amount)
				if ok && refundAmount.IsPositive() {
					refundCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), refundAmount))
					if sendErr := k.bankKeeper.SendCoinsFromModuleToAccount(
						ctx, types.ModuleName, creatorAddr, refundCoins,
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
