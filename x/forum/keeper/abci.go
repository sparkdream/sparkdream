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

// hiddenPruneDeferral is how far an ephemeral post whose hide is under appeal
// is pushed back each time its TTL comes up, until the verdict lands.
const hiddenPruneDeferral = int64(86400)

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

	// Phase 2b: Release lock / move reservations whose appeal window closed
	// unappealed.
	if err := k.ExpireSentinelActionRecords(ctx, now); err != nil {
		sdkCtx.Logger().Error("error expiring sentinel action records", "error", err)
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

	// Phase 4b: Finish slashing the conviction stakes of posts whose hide
	// stood, a bounded batch per block. Runs before GC so a queued post's
	// released stakes are settled, never collected.
	if err := k.ProcessStakeSlashQueue(ctx); err != nil {
		sdkCtx.Logger().Error("error slashing queued post conviction stakes", "error", err)
	}

	// Phase 5: GC released-and-retained PostConvictionStakes whose retention
	// window has passed (lock_window + hide_appeal_cooldown + 1d). Bounded
	// per block by maxStakeGcPerBlock so a large backlog never dominates
	// gas.
	if err := k.GcReleasedPostConvictionStakes(ctx); err != nil {
		sdkCtx.Logger().Error("error gc'ing released post conviction stakes", "error", err)
	}

	// Phase 6: Strip removed tags from posts, a capped batch per block.
	if err := k.ProcessTagPruneQueue(ctx); err != nil {
		sdkCtx.Logger().Error("error pruning removed tag references", "error", err)
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

		// A hide on the post must be settled before the post can go: its
		// reservation would otherwise leak, and an appeal in flight would
		// find nothing to restore or slash.
		if hr, herr := k.HideRecord.Get(ctx, postID); herr == nil {
			if hr.Appealed {
				// The verdict owns the post. Defer the prune; UPHELD tombstones
				// it and OVERTURNED restores it, and either is pruned on a later
				// pass.
				if err := k.ExpirationQueue.Remove(ctx, key); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ExpirationQueue.Remove failed", "err", err)
				}
				post.ExpirationTime = now + hiddenPruneDeferral
				if setErr := k.Post.Set(ctx, postID, post); setErr == nil {
					if err := k.ExpirationQueue.Set(ctx, collections.Join(post.ExpirationTime, postID)); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ExpirationQueue.Set failed", "err", err)
					}
				}
				pruned++
				return false, nil
			}
			// Unappealed but still inside its appeal window: an ephemeral
			// post must not lose the window a permanent one gets (the appeal
			// opens hide_appeal_cooldown after the hide, which can be after
			// the TTL). Defer the prune to the hide's own expiry, where
			// ExpireHiddenPosts settles it; the post stays HIDDEN meanwhile.
			// An anonymous post has no appellant, so there is nothing to wait
			// for (FRM-102).
			if until := hideExpiryAt(hr); now < until && !k.isAnonymous(post.Author) {
				if err := k.ExpirationQueue.Remove(ctx, key); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ExpirationQueue.Remove failed", "err", err)
				}
				post.ExpirationTime = until + 1
				if setErr := k.Post.Set(ctx, postID, post); setErr == nil {
					if err := k.ExpirationQueue.Set(ctx, collections.Join(post.ExpirationTime, postID)); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ExpirationQueue.Set failed", "err", err)
					}
				}
				pruned++
				return false, nil
			}
			// Unappealed past its window: the hide stood until the content's
			// own TTL ran out. Finalize it exactly as hidden_expiration would
			// (author slash, conviction-stake slash, reservation released),
			// then prune.
			if err := k.settleUnappealedHide(ctx, postID, hr, true, "ephemeral prune"); err != nil {
				sdkCtx.Logger().Error("failed to settle hide before prune", "post_id", postID, "error", err)
			}
			if reloaded, rerr := k.Post.Get(ctx, postID); rerr == nil {
				post = reloaded
			}
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
				if err := k.ExpirationQueue.Remove(ctx, collections.Join(oldExpiresAt, postID)); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ExpirationQueue.Remove failed", "err", err)
				}
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
						if err := k.Post.Set(ctx, postID, post); err != nil {
							sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: Post.Set failed", "err", err)
						}
						if err := k.ExpirationQueue.Set(ctx, collections.Join(newExpiresAt, postID)); err != nil {
							sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ExpirationQueue.Set failed", "err", err)
						}
						sdkCtx.EventManager().EmitEvent(sdk.NewEvent("forum.post.conviction_sustained",
							sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
							sdk.NewAttribute("conviction_score", conviction.String()),
							sdk.NewAttribute("new_expires_at", fmt.Sprintf("%d", newExpiresAt)),
						))
					} else {
						post.ExpirationTime = newExpiresAt
						if err := k.Post.Set(ctx, postID, post); err != nil {
							sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: Post.Set failed", "err", err)
						}
						if err := k.ExpirationQueue.Set(ctx, collections.Join(newExpiresAt, postID)); err != nil {
							sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ExpirationQueue.Set failed", "err", err)
						}
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
					if err := k.Post.Set(ctx, postID, post); err != nil {
						sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: Post.Set failed", "err", err)
					}
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
			if err := k.PostsByUpvotes.Remove(ctx, collections.Join(post.UpvoteCount, postID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: PostsByUpvotes.Remove failed", "err", err)
			}
			if post.Pinned {
				if err := k.PostsByPinned.Remove(ctx, collections.Join(post.CategoryId, postID)); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: PostsByPinned.Remove failed", "err", err)
				}
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
		if post.ParentId == 0 {
			if err := k.RootsByCategory.Remove(ctx, collections.Join(post.CategoryId, postID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: RootsByCategory.Remove failed", "err", err)
			}
			if err := k.LockedRoots.Remove(ctx, postID); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: LockedRoots.Remove failed", "err", err)
			}
		}
		if removeErr := k.Post.Remove(ctx, postID); removeErr != nil {
			sdkCtx.Logger().Error("failed to remove expired post", "post_id", postID, "error", removeErr)
			// Remove queue entry anyway to avoid infinite retry
		}

		// Release any sentinel reservation still attached to the post (thread
		// lock / move records and reply pins) before the records go, then
		// clean up associated records (best effort).
		k.releaseModerationRecordsOnDelete(ctx, post)
		if err := k.PostFlag.Remove(ctx, postID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: PostFlag.Remove failed", "err", err)
		}
		if err := k.HideRecord.Remove(ctx, postID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: HideRecord.Remove failed", "err", err)
		}
		if err := k.ThreadLockRecord.Remove(ctx, postID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ThreadLockRecord.Remove failed", "err", err)
		}
		if err := k.ThreadMoveRecord.Remove(ctx, postID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ThreadMoveRecord.Remove failed", "err", err)
		}
		if err := k.ThreadMetadata.Remove(ctx, postID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ThreadMetadata.Remove failed", "err", err)
		}
		if post.ParentId != 0 {
			if err := k.PostsByRoot.Remove(ctx, collections.Join(post.RootId, postID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: PostsByRoot.Remove failed", "err", err)
			}
		}
		if err := k.ThreadFollowCount.Remove(ctx, postID); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("PruneExpiredPosts: ThreadFollowCount.Remove failed", "err", err)
		}
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

// ExpireHiddenPosts finalizes unappealed hides whose hidden_expiration has
// passed: the post is tombstoned with the accountability consequences of a
// hide that stood, the sentinel's reservation is released, and the record is
// dropped. It walks the HideRecordExpiry index up to now (appealed hides are
// not indexed: the verdict finalizes them via OnAppealOutcome), so the pass
// costs O(expired) rather than O(all hide records).
func (k Keeper) ExpireHiddenPosts(ctx context.Context, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	hiddenExpiry := int64(types.DefaultHiddenExpiration)
	if hiddenExpiry <= 0 {
		return nil // disabled
	}

	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(now, uint64(math.MaxUint64)))

	var keys []collections.Pair[int64, uint64]
	if err := k.HideRecordExpiry.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if len(keys) >= maxHiddenExpiry {
			return true, nil
		}
		keys = append(keys, key)
		return false, nil
	}); err != nil {
		return nil // don't halt chain
	}

	expired := 0
	for _, key := range keys {
		if err := k.HideRecordExpiry.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("ExpireHiddenPosts: HideRecordExpiry.Remove failed", "err", err)
		}
		postID := key.K2()

		hr, err := k.HideRecord.Get(ctx, postID)
		// Stale entry: record gone, appealed (the verdict owns it), or a
		// newer hide replaced it.
		if err != nil || hr.Appealed || hideExpiryAt(hr) != key.K1() {
			continue
		}

		// The post is tombstoned only if still HIDDEN. Any other status means
		// the hide was reversed or the post removed without dropping the
		// record; the reservation is released either way so it never leaks.
		if err := k.settleUnappealedHide(ctx, postID, hr, true, "hide expiry"); err != nil {
			sdkCtx.Logger().Error("failed to finalize expired hidden post", "post_id", postID, "error", err)
			continue
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hidden_post_expired",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
			sdk.NewAttribute("hidden_at", fmt.Sprintf("%d", hr.HiddenAt)),
		))
		expired++
	}

	if expired > 0 {
		sdkCtx.Logger().Info("expired hidden posts", "count", expired)
	}

	return nil
}

// tombstoneHiddenPost finalizes a hide that stood — an unappealed hide past
// hidden_expiration (ExpireHiddenPosts) or an appealed hide whose appeal was
// UPHELD (OnAppealOutcome). It applies the accountability consequences (author
// per-tag rep slash, post-conviction stake slash, promoter warning) against the
// pre-tombstone state, then soft-deletes the post. It does NOT touch the
// sentinel bond, the sentinel counters or the HideRecord: those differ per
// caller (x/rep already released the bond on UPHELD). cause labels the logs.
func (k Keeper) tombstoneHiddenPost(ctx context.Context, postID uint64, post types.Post, cause string) error {
	// Accountability hooks fire here — BEFORE the post's tags are cleared
	// and the post is soft-deleted, because both hooks read the
	// pre-tombstone state. Failures log but never halt: a hide-finalize
	// must always tombstone the post or the moderation flow stalls.
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Tier 0: author per-tag rep slash. The author put the post in these
	// tags; a hide that stood (unappealed expiry or UPHELD appeal) says the
	// community rejected it, so claw back rep in each tag. DeductReputation
	// floors at zero — no harm if the author has no rep in the tag.
	if k.repKeeper != nil && len(post.Tags) > 0 {
		paramsForSlash, paramsErr := k.Params.Get(ctx)
		if paramsErr == nil && !paramsForSlash.AuthorRepSlash.IsNil() && paramsForSlash.AuthorRepSlash.IsPositive() {
			authorBytes, addrErr := k.addressCodec.StringToBytes(post.Author)
			if addrErr == nil {
				for _, tag := range post.Tags {
					if err := k.repKeeper.DeductReputation(ctx, sdk.AccAddress(authorBytes), tag, paramsForSlash.AuthorRepSlash); err != nil {
						sdkCtx.Logger().Warn("failed to slash author rep on hidden-post finalize", "cause", cause,
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
		sdkCtx.Logger().Warn("post conviction slash failed on hidden-post finalize", "cause", cause,
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
			sdkCtx.Logger().Warn("failed to issue promoter warning on hidden-post finalize", "cause", cause,
				"post_id", postID, "promoter", post.PromotedBy, "error", err)
		}
	}

	return k.softDeleteHiddenPost(ctx, postID, post)
}

// softDeleteHiddenPost is the state half of the hidden-post tombstone: tag
// usages released, the post soft-deleted with its content cleared, and its
// secondary indexes dropped. tombstoneHiddenPost runs it after the "hide
// stood" consequences; an overturned hide whose category is gone runs it on
// its own (the author won, but the post has nowhere to come back to).
func (k Keeper) softDeleteHiddenPost(ctx context.Context, postID uint64, post types.Post) error {
	// Drop rep-registry UsageCount for every tag the post carried — this is
	// the second half of the hidden-post tombstone path. Same
	// rationale as the ephemeral-prune and explicit-delete paths.
	k.decrementTagUsages(ctx, postID, post.Tags)

	// Soft-delete the post
	post.Status = types.PostStatus_POST_STATUS_DELETED
	post.Content = "" // clear content to reclaim space
	post.Tags = nil   // sever the (now-decremented) tag references
	applyPostMediaLabels(&post)
	if err := k.Post.Set(ctx, postID, post); err != nil {
		return err
	}
	// FORUM-S2-8: drop from secondary indexes; the post is no longer
	// eligible for TopPosts/PinnedPosts. DELETED is terminal, so the root
	// also leaves RootsByCategory (HasPostInCategory never walks it again).
	if post.ParentId == 0 {
		if err := k.RootsByCategory.Remove(ctx, collections.Join(post.CategoryId, post.PostId)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("softDeleteHiddenPost: RootsByCategory.Remove failed", "err", err)
		}
		if err := k.PostsByUpvotes.Remove(ctx, collections.Join(post.UpvoteCount, post.PostId)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("tombstoneHiddenPost: PostsByUpvotes.Remove failed", "err", err)
		}
		if post.Pinned {
			if err := k.PostsByPinned.Remove(ctx, collections.Join(post.CategoryId, post.PostId)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("tombstoneHiddenPost: PostsByPinned.Remove failed", "err", err)
			}
		}
	}
	return nil
}

// ExpireBounties settles ACTIVE bounties whose deadline has passed. It walks
// the BountiesByExpiry index up to now (O(expired), not O(all bounties ever
// created)). A bounty with assigned awards pays them out exactly as
// MsgAwardBounty would: assigning winners and then letting the bounty lapse
// must not return the escrow to the creator. With no eligible award,
// settleUnawardedBounty burns BountyExpiryBurnBps and pays the rest to the
// top reply, or (no eligible reply) refunds minus the cancellation fee.
// Suspended (MODERATION_PENDING) bounties are not in the index.
func (k Keeper) ExpireBounties(ctx context.Context, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(now, uint64(math.MaxUint64)))
	var keys []collections.Pair[int64, uint64]
	if err := k.BountiesByExpiry.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if len(keys) >= maxBountyExpirations {
			return true, nil
		}
		keys = append(keys, key)
		return false, nil
	}); err != nil {
		return nil // don't halt chain
	}

	expired := 0
	for _, key := range keys {
		if err := k.BountiesByExpiry.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("ExpireBounties: BountiesByExpiry.Remove failed", "err", err)
		}
		id := key.K2()
		bounty, err := k.Bounty.Get(ctx, id)
		if err != nil || bounty.Status != types.BountyStatus_BOUNTY_STATUS_ACTIVE ||
			bounty.ExpiresAt != key.K1() || bounty.ExpiresAt <= 0 {
			continue // stale entry
		}

		// Settle in a cache context so a failed payout leaves the bounty
		// untouched rather than half-paid.
		cacheCtx, write := sdkCtx.CacheContext()
		paid := 0
		if len(bounty.Awards) > 0 {
			paid, err = k.payBountyAwards(cacheCtx, &bounty)
			if err != nil {
				sdkCtx.Logger().Error("failed to pay expired bounty awards", "bounty_id", id, "error", err)
				paid = 0
				cacheCtx, write = sdkCtx.CacheContext()
			}
		}
		outcome := "awarded"
		if paid > 0 {
			bounty.Status = types.BountyStatus_BOUNTY_STATUS_AWARDED
		} else {
			bounty.Status = types.BountyStatus_BOUNTY_STATUS_EXPIRED
			assigned := bounty.Awards
			outcome, err = k.settleUnawardedBounty(cacheCtx, &bounty)
			if err != nil {
				// Fall back to a plain refund so the escrow never strands.
				sdkCtx.Logger().Error("failed to settle expired bounty", "bounty_id", id, "error", err)
				cacheCtx, write = sdkCtx.CacheContext()
				bounty.Awards = assigned
				outcome = "refunded"
				if amt, ok := sdkmath.NewIntFromString(bounty.Amount); ok && amt.IsPositive() {
					if rErr := k.burnAndRefundBounty(cacheCtx, &bounty, sdkmath.ZeroInt(), amt); rErr != nil {
						sdkCtx.Logger().Error("failed to refund expired bounty", "bounty_id", id, "error", rErr)
					}
				}
			}
		}
		if setErr := k.Bounty.Set(cacheCtx, id, bounty); setErr != nil {
			sdkCtx.Logger().Error("failed to expire bounty", "bounty_id", id, "error", setErr)
			continue
		}
		k.clearActiveBountyIndex(cacheCtx, bounty)
		write()

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("bounty_expired",
			sdk.NewAttribute("bounty_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("root_id", fmt.Sprintf("%d", bounty.ThreadId)),
			sdk.NewAttribute("amount", bounty.Amount),
			sdk.NewAttribute("outcome", outcome),
		))
		expired++
	}

	if expired > 0 {
		sdkCtx.Logger().Info("expired bounties", "count", expired)
	}

	return nil
}
