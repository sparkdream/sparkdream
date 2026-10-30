package keeper

// Moderation-record lifecycle helpers: every sentinel reservation (hide, lock,
// move, pin) has exactly one terminal path that releases or slashes it, and
// every EndBlocker pass walks a time-ordered index instead of a whole store.
//
//   - Hide: released on self-correct / council unhide (MsgUnhidePost, refused
//     while appealed), at hidden_expiration when unappealed
//     (ExpireHiddenPosts), when an unappealed hidden ephemeral post reaches
//     its TTL (PruneExpiredPosts), or by the appeal verdict (OnAppealOutcome).
//   - Lock / move: released on self-unlock (lock), at the appeal deadline when
//     no appeal was filed (ExpireSentinelActionRecords), or by the verdict.
//   - Pin: released on self-unpin or by the verdict; the verdict zeroes the
//     record's committed amount so a later unpin cannot release it again.

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

// maxSentinelRecordExpiry bounds the lock/move record expiry pass per block.
const maxSentinelRecordExpiry = 50

// hideExpiryAt is the time an unappealed hide finalizes. hidden_expiration is
// a compiled (build-tagged) constant, so the key derived here is stable for
// the life of the binary.
func hideExpiryAt(hr types.HideRecord) int64 {
	return hr.HiddenAt + int64(types.DefaultHiddenExpiration)
}

func lockExpiryAt(rec types.ThreadLockRecord) int64 {
	return rec.LockedAt + types.DefaultLockAppealDeadline
}

func moveExpiryAt(rec types.ThreadMoveRecord) int64 {
	return rec.MovedAt + types.DefaultMoveAppealDeadline
}

// indexHideExpiry adds an unappealed hide to the expiry index.
func (k Keeper) indexHideExpiry(ctx context.Context, postID uint64, hr types.HideRecord) error {
	return k.HideRecordExpiry.Set(ctx, collections.Join(hideExpiryAt(hr), postID))
}

// unindexHideExpiry drops a hide from the expiry index (best effort).
func (k Keeper) unindexHideExpiry(ctx context.Context, postID uint64, hr types.HideRecord) {
	if err := k.HideRecordExpiry.Remove(ctx, collections.Join(hideExpiryAt(hr), postID)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("unindexHideExpiry: HideRecordExpiry.Remove failed", "err", err)
	}
}

// releaseSentinelCommit frees a sentinel reservation recorded as a decimal
// string. Empty / zero / unparsable amounts and gov actions (sentinel == "")
// are a no-op. Best effort: a failure is logged, never returned.
func (k Keeper) releaseSentinelCommit(ctx context.Context, sentinel, committed, cause string, target uint64) {
	if k.repKeeper == nil || sentinel == "" || committed == "" {
		return
	}
	amt, ok := sdkmath.NewIntFromString(committed)
	if !ok || !amt.IsPositive() {
		return
	}
	if err := k.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, sentinel, amt); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Warn("failed to release sentinel reservation",
			"cause", cause, "target", target, "sentinel", sentinel, "error", err)
	}
}

// decrementPendingHide clears one slot of the sentinel's forum-local
// pending-hide tally (and optionally counts the hide as unchallenged).
func (k Keeper) decrementPendingHide(ctx context.Context, sentinel string, unchallenged bool) {
	if sentinel == "" {
		return
	}
	local, err := k.SentinelActivity.Get(ctx, sentinel)
	if err != nil {
		return
	}
	if unchallenged {
		local.UnchallengedHides++
	}
	if local.PendingHideCount > 0 {
		local.PendingHideCount--
	}
	if err := k.SentinelActivity.Set(ctx, sentinel, local); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Warn("failed to update sentinel activity",
			"sentinel", sentinel, "error", err)
	}
}

// settleUnappealedHide closes an unappealed hide whose content is being
// finalized (expiry) or destroyed (TTL prune): the sentinel's reservation is
// released, the pending-hide slot cleared (counted as unchallenged: nobody
// appealed), and the record plus its expiry index entry are dropped. When
// tombstone is true and the post is still HIDDEN, the accountability
// consequences of a hide that stood are applied first (tombstoneHiddenPost).
func (k Keeper) settleUnappealedHide(ctx context.Context, postID uint64, hr types.HideRecord, tombstone bool, cause string) error {
	if tombstone {
		if post, err := k.Post.Get(ctx, postID); err == nil && post.Status == types.PostStatus_POST_STATUS_HIDDEN {
			if err := k.tombstoneHiddenPost(ctx, postID, post, cause); err != nil {
				return err
			}
		}
	}
	k.releaseSentinelCommit(ctx, hr.Sentinel, hr.CommittedAmount, cause, postID)
	k.decrementPendingHide(ctx, hr.Sentinel, true)
	k.unindexHideExpiry(ctx, postID, hr)
	if err := k.HideRecord.Remove(ctx, postID); err != nil {
		return err
	}
	return k.reconcileThreadBounty(ctx, postID)
}

// ExpireSentinelActionRecords finalizes sentinel lock and move records whose
// appeal deadline passed with no appeal filed: the action stood unchallenged,
// so the reservation is released and the record dropped (the thread stays
// locked / moved; only the council can undo it from here). Records with an
// appeal pending are finalized by the verdict instead and are skipped.
func (k Keeper) ExpireSentinelActionRecords(ctx context.Context, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(now, uint64(math.MaxUint64)))

	processed := 0
	var lockKeys, moveKeys []collections.Pair[int64, uint64]
	if err := k.LockRecordExpiry.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if processed >= maxSentinelRecordExpiry {
			return true, nil
		}
		lockKeys = append(lockKeys, key)
		processed++
		return false, nil
	}); err != nil {
		return err
	}
	if err := k.MoveRecordExpiry.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if processed >= maxSentinelRecordExpiry {
			return true, nil
		}
		moveKeys = append(moveKeys, key)
		processed++
		return false, nil
	}); err != nil {
		return err
	}

	for _, key := range lockKeys {
		if err := k.LockRecordExpiry.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("ExpireSentinelActionRecords: LockRecordExpiry.Remove failed", "err", err)
		}
		rootID := key.K2()
		rec, err := k.ThreadLockRecord.Get(ctx, rootID)
		if err != nil || rec.AppealPending || lockExpiryAt(rec) != key.K1() {
			continue // stale entry (record gone, appealed, or replaced)
		}
		k.releaseSentinelCommit(ctx, rec.Sentinel, rec.CommittedAmount, "lock appeal window closed", rootID)
		if err := k.ThreadLockRecord.Remove(ctx, rootID); err != nil {
			sdkCtx.Logger().Error("failed to drop expired lock record", "root_id", rootID, "error", err)
			continue
		}
		if err := k.reconcileThreadBounty(ctx, rootID); err != nil {
			sdkCtx.Logger().Error("failed to cancel bounty of locked thread", "root_id", rootID, "error", err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("thread_lock_record_expired",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", rootID)),
			sdk.NewAttribute("sentinel", rec.Sentinel),
		))
	}

	for _, key := range moveKeys {
		if err := k.MoveRecordExpiry.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("ExpireSentinelActionRecords: MoveRecordExpiry.Remove failed", "err", err)
		}
		rootID := key.K2()
		rec, err := k.ThreadMoveRecord.Get(ctx, rootID)
		if err != nil || rec.AppealPending || moveExpiryAt(rec) != key.K1() {
			continue
		}
		k.releaseSentinelCommit(ctx, rec.Sentinel, rec.CommittedAmount, "move appeal window closed", rootID)
		if err := k.ThreadMoveRecord.Remove(ctx, rootID); err != nil {
			sdkCtx.Logger().Error("failed to drop expired move record", "root_id", rootID, "error", err)
			continue
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("thread_move_record_expired",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", rootID)),
			sdk.NewAttribute("sentinel", rec.Sentinel),
		))
	}
	return nil
}

// pinDisputePending reports whether a pin carries an unresolved dispute: the
// dispute was filed and the reservation has not been settled by a verdict
// (verdicts zero CommittedAmount).
func pinDisputePending(rec *types.PinnedReplyRecord) bool {
	return rec != nil && rec.Disputed && rec.CommittedAmount != ""
}

// releaseModerationRecordsOnDelete releases every sentinel reservation tied to
// a post that is about to be hard-deleted: the thread lock / move records of a
// root post, every pin recorded on its thread metadata, and (for a reply) its
// own pin record on the root's metadata. Appeals still in flight resolve
// against missing records afterwards (soft skip), so the reservation is freed
// here rather than leaked. The caller removes the records themselves.
func (k Keeper) releaseModerationRecordsOnDelete(ctx context.Context, post types.Post) {
	postID := post.PostId
	if post.ParentId == 0 {
		if rec, err := k.ThreadLockRecord.Get(ctx, postID); err == nil {
			k.releaseSentinelCommit(ctx, rec.Sentinel, rec.CommittedAmount, "thread pruned", postID)
			if err := k.LockRecordExpiry.Remove(ctx, collections.Join(lockExpiryAt(rec), postID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("releaseModerationRecordsOnDelete: LockRecordExpiry.Remove failed", "err", err)
			}
		}
		if rec, err := k.ThreadMoveRecord.Get(ctx, postID); err == nil {
			k.releaseSentinelCommit(ctx, rec.Sentinel, rec.CommittedAmount, "thread pruned", postID)
			if err := k.MoveRecordExpiry.Remove(ctx, collections.Join(moveExpiryAt(rec), postID)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("releaseModerationRecordsOnDelete: MoveRecordExpiry.Remove failed", "err", err)
			}
		}
		if md, err := k.ThreadMetadata.Get(ctx, postID); err == nil {
			for _, rec := range md.PinnedRecords {
				if rec != nil {
					k.releaseSentinelCommit(ctx, rec.PinnedBy, rec.CommittedAmount, "thread pruned", rec.PostId)
				}
			}
		}
		return
	}
	// Reply: drop its own pin from the root's metadata.
	md, err := k.ThreadMetadata.Get(ctx, post.RootId)
	if err != nil {
		return
	}
	changed := false
	for i, rec := range md.PinnedRecords {
		if rec != nil && rec.PostId == postID {
			k.releaseSentinelCommit(ctx, rec.PinnedBy, rec.CommittedAmount, "reply pruned", postID)
			md.PinnedRecords = append(md.PinnedRecords[:i], md.PinnedRecords[i+1:]...)
			changed = true
			break
		}
	}
	for i, id := range md.PinnedReplyIds {
		if id == postID {
			md.PinnedReplyIds = append(md.PinnedReplyIds[:i], md.PinnedReplyIds[i+1:]...)
			changed = true
			break
		}
	}
	if changed {
		if err := k.ThreadMetadata.Set(ctx, post.RootId, md); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("releaseModerationRecordsOnDelete: ThreadMetadata.Set failed", "err", err)
		}
	}
}
