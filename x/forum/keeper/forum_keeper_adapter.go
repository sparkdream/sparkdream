package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// Compile-time assertion: Keeper satisfies the rep ForumKeeper contract.
var _ reptypes.ForumKeeper = Keeper{}

// Compile-time assertion: Keeper finalizes post hides on every terminal
// appeal outcome (x/rep type-asserts its forum appeal target for this).
var _ reptypes.ModerationAppealOutcomeHandler = Keeper{}

// maxTagPruneScanPerBlock caps how many posts the tag-prune pass reads per
// block, across every queued tag.
const maxTagPruneScanPerBlock = 200

// PruneTagReferences queues the removal of tagName from every post that
// references it. Invoked by x/rep when a tag leaves the registry (council
// ResolveTagReport action=1, tag expiry). The walk itself runs in the
// EndBlocker in capped batches (ProcessTagPruneQueue), so the caller's tx or
// block never pays for a scan of every post. Re-queuing a tag that is
// already queued keeps its cursor.
func (k Keeper) PruneTagReferences(ctx context.Context, tagName string) error {
	if tagName == "" {
		return nil
	}
	has, err := k.TagPruneQueue.Has(ctx, tagName)
	if err != nil || has {
		return err
	}
	return k.TagPruneQueue.Set(ctx, tagName, 0)
}

// ProcessTagPruneQueue scans up to maxTagPruneScanPerBlock posts, resuming
// each queued tag at its cursor, and strips the tag from posts that carry
// it. A tag that is back in the registry (re-created) is dropped from the
// queue without touching posts: its references are valid again.
func (k Keeper) ProcessTagPruneQueue(ctx context.Context) error {
	type entry struct {
		tag    string
		cursor uint64
	}
	var queued []entry
	if err := k.TagPruneQueue.Walk(ctx, nil, func(tag string, cursor uint64) (bool, error) {
		queued = append(queued, entry{tag, cursor})
		return len(queued) >= maxTagPruneScanPerBlock, nil
	}); err != nil {
		return err
	}

	budget := maxTagPruneScanPerBlock
	for _, e := range queued {
		if budget <= 0 {
			break
		}
		if k.repKeeper != nil {
			if exists, err := k.repKeeper.TagExists(ctx, e.tag); err == nil && exists {
				if err := k.TagPruneQueue.Remove(ctx, e.tag); err != nil {
					return err
				}
				continue
			}
		}
		next, done, err := k.pruneTagBatch(ctx, e.tag, e.cursor, &budget)
		if err != nil {
			return err
		}
		if done {
			if err := k.TagPruneQueue.Remove(ctx, e.tag); err != nil {
				return err
			}
			continue
		}
		if err := k.TagPruneQueue.Set(ctx, e.tag, next); err != nil {
			return err
		}
	}
	return nil
}

// pruneTagBatch walks posts from id `from` while budget lasts. It returns
// the id to resume at and whether the walk reached the end of the store.
func (k Keeper) pruneTagBatch(ctx context.Context, tagName string, from uint64, budget *int) (uint64, bool, error) {
	iter, err := k.Post.Iterate(ctx, new(collections.Range[uint64]).StartInclusive(from))
	if err != nil {
		return from, false, err
	}

	var changed []types.Post
	next := from
	for ; iter.Valid(); iter.Next() {
		if *budget <= 0 {
			break
		}
		*budget--
		key, kErr := iter.Key()
		if kErr == nil {
			next = key + 1
		}
		post, err := iter.Value()
		if err != nil {
			continue
		}
		for i := 0; i < len(post.Tags); i++ {
			if post.Tags[i] == tagName {
				post.Tags = append(post.Tags[:i], post.Tags[i+1:]...)
				changed = append(changed, post)
				break
			}
		}
	}
	done := !iter.Valid()
	iter.Close()
	for _, post := range changed {
		if err := k.Post.Set(ctx, post.PostId, post); err != nil {
			return from, false, err
		}
	}
	return next, done, nil
}

// GetPostAuthor returns the author address for the given post.
func (k Keeper) GetPostAuthor(ctx context.Context, postID uint64) (string, error) {
	post, err := k.Post.Get(ctx, postID)
	if err != nil {
		return "", fmt.Errorf("post %d not found: %w", postID, err)
	}
	return post.Author, nil
}

// GetPostTags returns the tag list for the given post.
func (k Keeper) GetPostTags(ctx context.Context, postID uint64) ([]string, error) {
	post, err := k.Post.Get(ctx, postID)
	if err != nil {
		return nil, fmt.Errorf("post %d not found: %w", postID, err)
	}
	return post.Tags, nil
}

// findPinnedRecord locates the PinnedReplyRecord for a reply by deriving its
// owning thread from the reply post's RootId, then scanning that thread's
// metadata. Returns the record, the thread id, its slice index, and ok=false
// when the reply, thread metadata, or record is missing (soft skip for
// callers — the pin may have been removed/GC'd).
func (k Keeper) findPinnedRecord(ctx context.Context, replyID uint64) (*types.PinnedReplyRecord, uint64, int, bool) {
	reply, err := k.Post.Get(ctx, replyID)
	if err != nil {
		return nil, 0, 0, false
	}
	threadID := reply.RootId
	if threadID == 0 {
		threadID = reply.PostId
	}
	metadata, err := k.ThreadMetadata.Get(ctx, threadID)
	if err != nil {
		return nil, 0, 0, false
	}
	for i, rec := range metadata.PinnedRecords {
		if rec.PostId == replyID {
			return rec, threadID, i, true
		}
	}
	return nil, 0, 0, false
}

// errNotForumAppeal is returned by every appeal-target method for an action
// type the forum does not own. x/rep routes every unregistered type here, so
// without this the WARNING / DEMOTION / ... types would fall through to the
// post-hide branch and act on a forum hide.
func errNotForumAppeal(actionType reptypes.GovActionType) error {
	return fmt.Errorf("x/forum does not own %s appeals", actionType.String())
}

// forumAppealRecord resolves the moderation record behind a forum appeal and
// reports whether the forum honors it. Only appeals filed through the forum's
// own handlers count (MsgAppealPost, MsgAppealThreadLock, MsgAppealThreadMove,
// MsgDisputePin): each marks its record before opening the rep appeal, after
// the author / cooldown / one-appeal checks. An appeal filed for the same
// target straight through x/rep MsgAppealGovAction finds an unmarked record
// and is a no-op for the forum (no release, no slash, no reversal), so it can
// neither bypass those checks nor release a reservation twice.
//
// honored == false with err == nil means "record missing or not under a forum
// appeal" (soft skip).
func (k Keeper) forumAppealRecord(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) (sentinel string, committed math.Int, honored bool, err error) {
	committed = math.ZeroInt()
	id, perr := strconv.ParseUint(actionTarget, 10, 64)
	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE,
		reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK,
		reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE,
		reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN:
	default:
		return "", committed, false, errNotForumAppeal(actionType)
	}
	if perr != nil {
		return "", committed, false, fmt.Errorf("invalid action target %q: %w", actionTarget, perr)
	}

	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		rec, gerr := k.ThreadLockRecord.Get(ctx, id)
		if gerr != nil || !rec.AppealPending {
			return "", committed, false, nil
		}
		return rec.Sentinel, parseIntOrZero(rec.CommittedAmount), true, nil
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		rec, gerr := k.ThreadMoveRecord.Get(ctx, id)
		if gerr != nil || !rec.AppealPending {
			return "", committed, false, nil
		}
		return rec.Sentinel, parseIntOrZero(rec.CommittedAmount), true, nil
	case reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN:
		// actionTarget is the reply id; the owning thread is derived from it.
		rec, _, _, ok := k.findPinnedRecord(ctx, id)
		if !ok || !pinDisputePending(rec) {
			return "", committed, false, nil
		}
		return rec.PinnedBy, parseIntOrZero(rec.CommittedAmount), true, nil
	default: // POST_HIDE
		rec, gerr := k.HideRecord.Get(ctx, id)
		if gerr != nil || !rec.Appealed {
			return "", committed, false, nil
		}
		return rec.Sentinel, parseIntOrZero(rec.CommittedAmount), true, nil
	}
}

// GetActionSentinel resolves the sentinel address that executed the given
// action from forum's own moderation records. Returns "" with no error when
// the record is missing (already settled or GC'd) or not under a forum-filed
// appeal (see forumAppealRecord); callers treat that as a soft skip. Returns
// an error for action types the forum does not own.
func (k Keeper) GetActionSentinel(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) (string, error) {
	sentinel, _, honored, err := k.forumAppealRecord(ctx, actionType, actionTarget)
	if err != nil || !honored {
		return "", err
	}
	return sentinel, nil
}

// GetActionCommittedAmount returns the bond amount reserved by the sentinel
// for the given action, snapshotted on its record at action time. Zero (no
// error) when the record is missing, already settled, or not under a
// forum-filed appeal; an error for action types the forum does not own.
func (k Keeper) GetActionCommittedAmount(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) (math.Int, error) {
	_, committed, honored, err := k.forumAppealRecord(ctx, actionType, actionTarget)
	if err != nil {
		return math.ZeroInt(), err
	}
	if !honored {
		return math.ZeroInt(), nil
	}
	return committed, nil
}

// ReverseSentinelAction reverses the content-state effects of a sentinel
// moderation action when an appeal is resolved against the sentinel
// (OVERTURNED) or when the council otherwise overrides the action through
// the rep module. Branches on actionType:
//
//   - GOV_ACTION_TYPE_THREAD_LOCK: clears post.Locked / LockedBy / LockedAt /
//     LockReason on the root post and removes the ThreadLockRecord. Bypasses
//     the LockAppealDeadline that MsgUnlockThread enforces — the appeal has
//     just resolved, so the deadline is irrelevant.
//   - GOV_ACTION_TYPE_THREAD_MOVE: restores post.CategoryId to the
//     ThreadMoveRecord.OriginalCategoryId and removes the move record.
//   - GOV_ACTION_TYPE_REPLY_PIN: removes the reply from the owning thread's
//     PinnedReplyIds / PinnedRecords (actionTarget is the reply id; the thread
//     is derived from the reply post's RootId).
//   - default (post-level hide): flips post.Status HIDDEN → ACTIVE, clears
//     HiddenBy / HiddenAt, and removes the HideRecord.
//
// All branches apply the same dangling-reference guard used by
// MsgUnhidePost / MsgUnarchiveThread: the post's destination category must
// still exist in x/commons. If it doesn't, the reverse is refused — the
// caller (x/rep's appeal resolver) should log and continue rather than
// abort the appeal-resolution transaction. The invariant is documented on
// Keeper.HasPostInCategory in keeper.go.
//
// This method does NOT enforce any caller-side authorization — it is
// privileged and only callable from x/rep via the ForumKeeper interface.
// User-driven reversals go through the MsgUnhidePost / MsgUnlockThread /
// MsgMoveThread handlers, which carry their own auth.
//
// Missing action records are a soft skip (logs warning, returns nil): the
// record may have been GC'd. Committee and council locks and moves write an
// authority record (no sentinel), so an overturned appeal reverses them the
// same way.
func (k Keeper) ReverseSentinelAction(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Only reverse actions under a forum-filed appeal (see forumAppealRecord).
	if _, _, honored, err := k.forumAppealRecord(ctx, actionType, actionTarget); err != nil || !honored {
		return err
	}
	id, err := strconv.ParseUint(actionTarget, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid action target %q: %w", actionTarget, err)
	}

	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		post, err := k.Post.Get(ctx, id)
		if err != nil {
			sdkCtx.Logger().Warn("reverse sentinel action: post missing for unlock",
				"root_id", id, "error", err)
			return nil
		}
		if !post.Locked {
			// Idempotent: already unlocked (e.g. self-unlocked then appeal resolved).
			if err := k.ThreadLockRecord.Remove(ctx, id); err != nil {
				return err
			}
			return nil
		}
		post.Locked = false
		post.LockedBy = ""
		post.LockedAt = 0
		post.LockReason = ""
		if err := k.Post.Set(ctx, id, post); err != nil {
			return fmt.Errorf("reverse lock: update post: %w", err)
		}
		if err := k.LockedRoots.Remove(ctx, id); err != nil {
			return fmt.Errorf("reverse lock: unindex: %w", err)
		}
		if err := k.ThreadLockRecord.Remove(ctx, id); err != nil {
			sdkCtx.Logger().Warn("reverse sentinel action: remove lock record failed",
				"root_id", id, "error", err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"thread_unlocked",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("unlocked_by", "appeal_overturned"),
			sdk.NewAttribute("is_gov_authority", "true"),
		))
		return nil

	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		rec, err := k.ThreadMoveRecord.Get(ctx, id)
		if err != nil {
			sdkCtx.Logger().Warn("reverse sentinel action: move record missing",
				"root_id", id, "error", err)
			return nil
		}
		post, err := k.Post.Get(ctx, id)
		if err != nil {
			sdkCtx.Logger().Warn("reverse sentinel action: post missing for un-move",
				"root_id", id, "error", err)
			return nil
		}
		// Dangling-reference guard: the original category may have been
		// deleted while the thread was sitting in the wrong category. Refuse
		// rather than restore into a non-existent category.
		if k.commonsKeeper == nil || !k.commonsKeeper.HasCategory(ctx, rec.OriginalCategoryId) {
			sdkCtx.Logger().Warn("reverse sentinel action: original category gone, skipping un-move",
				"root_id", id, "original_category_id", rec.OriginalCategoryId)
			return nil
		}
		if err := k.setRootCategory(ctx, &post, rec.OriginalCategoryId); err != nil {
			return fmt.Errorf("reverse move: %w", err)
		}
		if err := k.Post.Set(ctx, id, post); err != nil {
			return fmt.Errorf("reverse move: update post: %w", err)
		}
		if err := k.ThreadMoveRecord.Remove(ctx, id); err != nil {
			sdkCtx.Logger().Warn("reverse sentinel action: remove move record failed",
				"root_id", id, "error", err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"thread_moved",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("from_category", fmt.Sprintf("%d", rec.NewCategoryId)),
			sdk.NewAttribute("to_category", fmt.Sprintf("%d", rec.OriginalCategoryId)),
			sdk.NewAttribute("moved_by", "appeal_overturned"),
		))
		return nil

	case reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN:
		// Overturned pin dispute: unpin the reply. actionTarget is the reply id;
		// the owning thread is derived from the reply post's RootId.
		_, threadID, _, ok := k.findPinnedRecord(ctx, id)
		if !ok {
			sdkCtx.Logger().Warn("reverse sentinel action: pin record missing for unpin",
				"reply_id", id)
			return nil
		}
		metadata, err := k.ThreadMetadata.Get(ctx, threadID)
		if err != nil {
			sdkCtx.Logger().Warn("reverse sentinel action: thread metadata missing for unpin",
				"thread_id", threadID, "reply_id", id, "error", err)
			return nil
		}
		// Remove from both the id list and the record list.
		for i, pid := range metadata.PinnedReplyIds {
			if pid == id {
				metadata.PinnedReplyIds = append(metadata.PinnedReplyIds[:i], metadata.PinnedReplyIds[i+1:]...)
				break
			}
		}
		for i, r := range metadata.PinnedRecords {
			if r.PostId == id {
				metadata.PinnedRecords = append(metadata.PinnedRecords[:i], metadata.PinnedRecords[i+1:]...)
				break
			}
		}
		if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
			return fmt.Errorf("reverse pin: update thread metadata: %w", err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"reply_unpinned",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", threadID)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("unpinned_by", "appeal_overturned"),
		))
		return nil

	case reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE:
		post, err := k.Post.Get(ctx, id)
		if err != nil {
			sdkCtx.Logger().Warn("reverse sentinel action: post missing for unhide",
				"post_id", id, "error", err)
			return nil
		}
		// Read HideRecord BEFORE mutating anything — we need the snapshotted
		// AuthorBondAmount to restore the slashed author bond.
		hideRecord, hideRecordErr := k.HideRecord.Get(ctx, id)
		haveHideRecord := hideRecordErr == nil

		if post.Status != types.PostStatus_POST_STATUS_HIDDEN {
			// Idempotent: already unhidden (e.g. sentinel self-corrected then appeal landed).
			if err := k.HideRecord.Remove(ctx, id); err != nil {
				return err
			}
			return nil
		}
		// Dangling-reference guard: the post cannot come back to a deleted
		// category. The author still won, so the bond is restored below, and
		// the post is soft-deleted rather than left HIDDEN with no record
		// (nothing would ever settle it).
		categoryGone := k.commonsKeeper == nil || !k.commonsKeeper.HasCategory(ctx, post.CategoryId)
		if categoryGone {
			sdkCtx.Logger().Warn("reverse sentinel action: parent category gone, soft-deleting instead of unhiding",
				"post_id", id, "category_id", post.CategoryId)
			if err := k.softDeleteHiddenPost(ctx, id, post); err != nil {
				return fmt.Errorf("reverse hide: soft-delete post: %w", err)
			}
		} else {
			post.Status = types.PostStatus_POST_STATUS_ACTIVE
			post.HiddenBy = ""
			post.HiddenAt = 0
			if err := k.Post.Set(ctx, id, post); err != nil {
				return fmt.Errorf("reverse hide: update post: %w", err)
			}
		}

		// Restore the author bond that MsgHidePost slashed (mint + re-lock).
		// Net DREAM supply change across slash→restore is zero. Best-effort:
		// failures are logged but do not abort the appeal-resolution tx.
		// A soft-deleted post gets the DREAM back unlocked: a bond re-locked
		// on a post that is gone would strand it.
		if haveHideRecord && k.repKeeper != nil && hideRecord.AuthorBondAmount != "" {
			if amt, ok := math.NewIntFromString(hideRecord.AuthorBondAmount); ok && amt.IsPositive() {
				authorAddr, addrErr := sdk.AccAddressFromBech32(post.Author)
				if addrErr != nil {
					sdkCtx.Logger().Warn("reverse sentinel action: bad author address for bond restore",
						"post_id", id, "author", post.Author, "error", addrErr)
				} else if categoryGone {
					if err := k.repKeeper.MintDREAM(ctx, authorAddr, amt); err != nil {
						sdkCtx.Logger().Warn("reverse sentinel action: return author bond failed",
							"post_id", id, "author", post.Author, "error", err)
					}
				} else if err := k.repKeeper.RestoreAuthorBond(ctx, authorAddr, reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, id, amt); err != nil {
					sdkCtx.Logger().Warn("reverse sentinel action: restore author bond failed",
						"post_id", id, "author", post.Author, "error", err)
				}
			}
		}

		if err := k.HideRecord.Remove(ctx, id); err != nil {
			sdkCtx.Logger().Warn("reverse sentinel action: remove hide record failed",
				"post_id", id, "error", err)
		}
		if categoryGone {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"hidden_post_removed_category_gone",
				sdk.NewAttribute("post_id", fmt.Sprintf("%d", id)),
				sdk.NewAttribute("category_id", fmt.Sprintf("%d", post.CategoryId)),
			))
			return nil
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"post_unhidden",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("unhidden_by", "appeal_overturned"),
			sdk.NewAttribute("is_council", "false"),
			sdk.NewAttribute("is_self_correct", "false"),
		))
		return nil
	}
	return errNotForumAppeal(actionType)
}

// OnSentinelActionResolved applies forum-LOCAL bookkeeping when a jury
// verdict lands on a sentinel action. The SHARED accountability
// consequences (streaks, accuracy ring, overturn cooldown, streak
// demotion) are applied by x/rep itself via RecordRoleOutcome — the only
// forum-local effect today is the pending-hide count decrement for hide
// verdicts. Missing or non-forum-filed records are a soft no-op; foreign
// action types are an error.
func (k Keeper) OnSentinelActionResolved(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) error {
	sentinel, _, honored, err := k.forumAppealRecord(ctx, actionType, actionTarget)
	if err != nil || !honored || sentinel == "" {
		return err
	}
	if actionType != reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE {
		return nil // only hides track a pending count
	}
	k.decrementPendingHide(ctx, sentinel, false)
	return nil
}

// OnAppealOutcome finalizes a forum-filed appeal once x/rep has applied its
// own bond and accountability effects. It is the only path that ends an
// appealed record (the expiry passes skip appealed hides, locks and moves):
//
//   - UPHELD: the action stands. x/rep released the reservation. A hide is
//     tombstoned with the consequences an unappealed expiry applies (author
//     per-tag rep slash, conviction-stake slash, promoter warning); a lock or
//     move record is dropped (the thread stays locked / moved; the sentinel
//     can no longer self-unlock and the author cannot appeal again); a pin
//     keeps its record but its reservation is zeroed so no later unpin can
//     release it a second time.
//   - OVERTURNED: x/rep slashed the reservation and ReverseSentinelAction
//     restored the content. Any record it had to leave behind (e.g. the
//     original category was deleted) is dropped here.
//   - TIMEOUT: no verdict; x/rep neither moved the sentinel's bond nor called
//     OnSentinelActionResolved. Neither party is blamed: the reservation is
//     released and the action reversed (timeout favors the appellant, as in
//     x/collect), then the record is dropped.
//     A committee or council lock / move (authority record, no sentinel) is
//     the exception: it stands on TIMEOUT.
//
// Missing or non-forum-filed records are a no-op; foreign action types are an
// error.
func (k Keeper) OnAppealOutcome(ctx context.Context, actionType reptypes.GovActionType, actionTarget string, outcome reptypes.GovAppealStatus) error {
	sentinel, committed, honored, err := k.forumAppealRecord(ctx, actionType, actionTarget)
	if err != nil || !honored {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	id, err := strconv.ParseUint(actionTarget, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid action target %q: %w", actionTarget, err)
	}
	// Once the record is settled below, a bounty on the thread resumes
	// (action reversed) or is cancelled (action stood).
	if actionType == reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE ||
		actionType == reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK {
		defer func() {
			if rErr := k.reconcileThreadBounty(ctx, id); rErr != nil {
				sdkCtx.Logger().Error("failed to reconcile thread bounty after appeal outcome",
					"thread_id", id, "error", rErr)
			}
		}()
	}

	// A committee or council lock / move (authority record, no sentinel)
	// stands on TIMEOUT: only a jury verdict reverses a council decision, so
	// a jury that could not be seated or did not decide leaves it in place.
	// The record is dropped below like any settled appeal.
	if outcome == reptypes.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT && sentinel == "" {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("forum_appeal_timeout",
			sdk.NewAttribute("action_type", actionType.String()),
			sdk.NewAttribute("target", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("sentinel", ""),
			sdk.NewAttribute("outcome", "authority_action_stands"),
		))
	} else if outcome == reptypes.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT {
		if committed.IsPositive() {
			k.releaseSentinelCommit(ctx, sentinel, committed.String(), "appeal timeout", id)
		}
		if err := k.OnSentinelActionResolved(ctx, actionType, actionTarget); err != nil {
			sdkCtx.Logger().Warn("failed to clear pending hide on appeal timeout",
				"target", id, "sentinel", sentinel, "error", err)
		}
		if err := k.ReverseSentinelAction(ctx, actionType, actionTarget); err != nil {
			return fmt.Errorf("reverse action on appeal timeout: %w", err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("forum_appeal_timeout",
			sdk.NewAttribute("action_type", actionType.String()),
			sdk.NewAttribute("target", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("sentinel", sentinel),
			sdk.NewAttribute("outcome", "restored_timeout"),
		))
	}

	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_POST_HIDE:
		hr, err := k.HideRecord.Get(ctx, id)
		if err != nil {
			return nil
		}
		if outcome == reptypes.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD {
			post, err := k.Post.Get(ctx, id)
			if err == nil && post.Status == types.PostStatus_POST_STATUS_HIDDEN {
				if err := k.tombstoneHiddenPost(ctx, id, post, "appeal upheld"); err != nil {
					return fmt.Errorf("tombstone upheld hide: %w", err)
				}
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hidden_post_appeal_upheld",
				sdk.NewAttribute("post_id", fmt.Sprintf("%d", id)),
				sdk.NewAttribute("sentinel", hr.Sentinel),
				sdk.NewAttribute("hidden_at", fmt.Sprintf("%d", hr.HiddenAt)),
			))
		}
		k.unindexHideExpiry(ctx, id, hr)
		if err := k.HideRecord.Remove(ctx, id); err != nil {
			return fmt.Errorf("remove hide record: %w", err)
		}

	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		if rec, err := k.ThreadLockRecord.Get(ctx, id); err == nil {
			if err := k.LockRecordExpiry.Remove(ctx, collections.Join(lockExpiryAt(rec), id)); err != nil {
				return err
			}
			if err := k.ThreadLockRecord.Remove(ctx, id); err != nil {
				return fmt.Errorf("remove lock record: %w", err)
			}
		}

	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		if rec, err := k.ThreadMoveRecord.Get(ctx, id); err == nil {
			if err := k.MoveRecordExpiry.Remove(ctx, collections.Join(moveExpiryAt(rec), id)); err != nil {
				return err
			}
			if err := k.ThreadMoveRecord.Remove(ctx, id); err != nil {
				return fmt.Errorf("remove move record: %w", err)
			}
		}

	case reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN:
		// OVERTURNED / TIMEOUT removed the pin with its record. UPHELD keeps
		// the pin: settle its reservation so it cannot be released again.
		rec, threadID, idx, ok := k.findPinnedRecord(ctx, id)
		if !ok {
			return nil
		}
		md, err := k.ThreadMetadata.Get(ctx, threadID)
		if err != nil || idx >= len(md.PinnedRecords) {
			return nil
		}
		rec.CommittedAmount = ""
		md.PinnedRecords[idx] = rec
		if err := k.ThreadMetadata.Set(ctx, threadID, md); err != nil {
			return fmt.Errorf("settle pin record: %w", err)
		}
	}
	return nil
}
