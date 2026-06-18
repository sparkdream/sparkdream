package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// Compile-time assertion: Keeper satisfies the rep ForumKeeper contract.
var _ reptypes.ForumKeeper = Keeper{}

// PruneTagReferences scans every post and removes the given tag name from
// any post that references it. Invoked by x/rep's ResolveTagReport (action=1).
// Best-effort: iteration or write errors are surfaced but individual post
// update failures do not abort the scan.
func (k Keeper) PruneTagReferences(ctx context.Context, tagName string) error {
	iter, err := k.Post.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		post, err := iter.Value()
		if err != nil {
			continue
		}
		changed := false
		for i := 0; i < len(post.Tags); i++ {
			if post.Tags[i] == tagName {
				post.Tags = append(post.Tags[:i], post.Tags[i+1:]...)
				changed = true
				break
			}
		}
		if changed {
			_ = k.Post.Set(ctx, post.PostId, post)
		}
	}
	return nil
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

// GetActionSentinel resolves the sentinel address that executed the given
// gov action from forum's own moderation records. Returns the empty string
// with no error if the record is missing (already garbage-collected) —
// callers should treat this as a soft skip.
func (k Keeper) GetActionSentinel(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) (string, error) {
	id, err := strconv.ParseUint(actionTarget, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid action target %q: %w", actionTarget, err)
	}

	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		rec, err := k.ThreadLockRecord.Get(ctx, id)
		if err != nil {
			return "", nil // missing — GC'd or never recorded
		}
		return rec.Sentinel, nil
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		rec, err := k.ThreadMoveRecord.Get(ctx, id)
		if err != nil {
			return "", nil
		}
		return rec.Sentinel, nil
	case reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN:
		// actionTarget is the reply id; the owning thread is derived from it.
		rec, _, _, ok := k.findPinnedRecord(ctx, id)
		if !ok {
			return "", nil
		}
		return rec.PinnedBy, nil
	default:
		// Post-level hide (GOV_ACTION_TYPE_POST_HIDE; default kept as a safe
		// fallback for legacy/unspecified targets).
		rec, err := k.HideRecord.Get(ctx, id)
		if err != nil {
			return "", nil
		}
		return rec.Sentinel, nil
	}
}

// committedOrDefault parses a record's committed_amount, falling back to the
// flat DefaultSentinelSlashAmount for legacy lock/move records written before
// committed_amount was snapshotted (so they still release/slash a sane amount).
func committedOrDefault(committed string) math.Int {
	if committed != "" {
		if v, ok := math.NewIntFromString(committed); ok {
			return v
		}
	}
	return math.NewInt(types.DefaultSentinelSlashAmount)
}

// GetActionCommittedAmount returns the bond amount reserved by the sentinel for
// the given gov action. Hide / lock / move / pin actions each snapshot
// committed_amount on their record at action time (committedOrDefault applies
// the flat DefaultSentinelSlashAmount fallback for legacy lock/move records that
// predate the snapshot). Returns zero (no error) when the record is missing —
// caller should treat as a soft skip.
func (k Keeper) GetActionCommittedAmount(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) (math.Int, error) {
	id, err := strconv.ParseUint(actionTarget, 10, 64)
	if err != nil {
		return math.ZeroInt(), fmt.Errorf("invalid action target %q: %w", actionTarget, err)
	}
	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		rec, err := k.ThreadLockRecord.Get(ctx, id)
		if err != nil {
			return math.ZeroInt(), nil
		}
		return committedOrDefault(rec.CommittedAmount), nil
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		rec, err := k.ThreadMoveRecord.Get(ctx, id)
		if err != nil {
			return math.ZeroInt(), nil
		}
		return committedOrDefault(rec.CommittedAmount), nil
	case reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN:
		rec, _, _, ok := k.findPinnedRecord(ctx, id)
		if !ok || rec.CommittedAmount == "" {
			return math.ZeroInt(), nil
		}
		v, ok := math.NewIntFromString(rec.CommittedAmount)
		if !ok {
			return math.ZeroInt(), nil
		}
		return v, nil
	default:
		rec, err := k.HideRecord.Get(ctx, id)
		if err != nil {
			return math.ZeroInt(), nil
		}
		if rec.CommittedAmount == "" {
			return math.ZeroInt(), nil
		}
		v, ok := math.NewIntFromString(rec.CommittedAmount)
		if !ok {
			return math.ZeroInt(), nil
		}
		return v, nil
	}
}

// RecordSentinelActionUpheld updates forum's SentinelActivity counters when
// an appeal is resolved in the sentinel's favor. Increments the upheld_*
// counter (hide / lock / move) and consecutive_upheld, and resets
// consecutive_overturns. For hide actions, also decrements pending_hide_count.
// No-op (with log) when the sentinel cannot be resolved.
func (k Keeper) RecordSentinelActionUpheld(ctx context.Context, epoch uint64, actionType reptypes.GovActionType, actionTarget string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sentinel, err := k.GetActionSentinel(ctx, actionType, actionTarget)
	if err != nil {
		return err
	}
	if sentinel == "" {
		sdkCtx.Logger().Warn("record sentinel upheld: action record missing",
			"action_type", actionType.String(), "action_target", actionTarget)
		return nil
	}

	local, err := k.SentinelActivity.Get(ctx, sentinel)
	if err != nil {
		local = types.SentinelActivity{Address: sentinel}
	}

	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		local.UpheldLocks++
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		local.UpheldMoves++
	case reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN:
		local.UpheldPins++
	default:
		local.UpheldHides++
		if local.PendingHideCount > 0 {
			local.PendingHideCount--
		}
	}

	local.ConsecutiveUpheld++
	local.ConsecutiveOverturns = 0
	local.EpochAppealsResolved++
	bumpAccuracyWindow(&local, epoch, true)

	if err := k.SentinelActivity.Set(ctx, sentinel, local); err != nil {
		return fmt.Errorf("persist sentinel activity (upheld): %w", err)
	}
	return nil
}

// RecordSentinelActionOverturned updates forum's SentinelActivity counters
// when an appeal is resolved against the sentinel. Increments overturned_*,
// increments consecutive_overturns, resets consecutive_upheld. If
// consecutive_overturns crosses DefaultMaxConsecutiveOverturnsBeforeDemotion,
// demotes the sentinel via the rep keeper. No-op (with log) when the sentinel
// cannot be resolved.
func (k Keeper) RecordSentinelActionOverturned(ctx context.Context, epoch uint64, actionType reptypes.GovActionType, actionTarget string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	sentinel, err := k.GetActionSentinel(ctx, actionType, actionTarget)
	if err != nil {
		return err
	}
	if sentinel == "" {
		sdkCtx.Logger().Warn("record sentinel overturned: action record missing",
			"action_type", actionType.String(), "action_target", actionTarget)
		return nil
	}

	local, err := k.SentinelActivity.Get(ctx, sentinel)
	if err != nil {
		local = types.SentinelActivity{Address: sentinel}
	}

	switch actionType {
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		local.OverturnedLocks++
	case reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		local.OverturnedMoves++
	case reptypes.GovActionType_GOV_ACTION_TYPE_REPLY_PIN:
		local.OverturnedPins++
	default:
		local.OverturnedHides++
		if local.PendingHideCount > 0 {
			local.PendingHideCount--
		}
	}

	local.ConsecutiveOverturns++
	local.ConsecutiveUpheld = 0
	local.EpochAppealsResolved++
	bumpAccuracyWindow(&local, epoch, false)
	local.OverturnCooldownUntil = sdkCtx.BlockTime().Unix() + types.DefaultSentinelOverturnCooldown

	if err := k.SentinelActivity.Set(ctx, sentinel, local); err != nil {
		return fmt.Errorf("persist sentinel activity (overturned): %w", err)
	}

	if local.ConsecutiveOverturns >= reptypes.DefaultMaxConsecutiveOverturnsBeforeDemotion {
		if k.repKeeper == nil {
			sdkCtx.Logger().Warn("cannot demote sentinel: rep keeper not wired",
				"sentinel", sentinel)
			return nil
		}
		cooldownUntil := sdkCtx.BlockTime().Unix() + reptypes.DefaultSentinelDemotionCooldown
		if err := k.repKeeper.SetBondStatus(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, sentinel,
			reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, cooldownUntil); err != nil {
			sdkCtx.Logger().Error("failed to demote sentinel after overturn streak",
				"sentinel", sentinel, "consecutive_overturns", local.ConsecutiveOverturns,
				"error", err)
		}
	}
	return nil
}

// bumpAccuracyWindow records one resolved appeal in the sentinel's rolling
// accuracy ring at the slot for `epoch` (slot index = epoch % ring size). The
// ring is lazily allocated to its fixed size on first write. A slot whose
// stamp does not match `epoch` is stale (left by an earlier epoch that mapped
// to the same index) and is reset before counting — because the ring size is
// >= the read window, any slot being overwritten is necessarily older than the
// window, so the overwrite never drops in-window data.
func bumpAccuracyWindow(local *types.SentinelActivity, epoch uint64, upheld bool) {
	if len(local.AccuracyWindow) != types.SentinelAccuracyRingSize {
		local.AccuracyWindow = make([]*types.AccuracyEpochBucket, types.SentinelAccuracyRingSize)
		for i := range local.AccuracyWindow {
			local.AccuracyWindow[i] = &types.AccuracyEpochBucket{}
		}
	}
	slot := local.AccuracyWindow[epoch%uint64(types.SentinelAccuracyRingSize)]
	if slot.Epoch != epoch {
		slot.Epoch, slot.Upheld, slot.Overturned = epoch, 0, 0
	}
	if upheld {
		slot.Upheld++
	} else {
		slot.Overturned++
	}
}

// GetSentinelWindowedAccuracy returns (upheld, overturned) resolved-appeal
// counts summed over the last `window` reward epochs ending at currentEpoch
// (inclusive). Slots stamped outside that range — stale entries or epochs
// older than the window — are ignored. Missing record or window 0 -> (0, 0).
func (k Keeper) GetSentinelWindowedAccuracy(ctx context.Context, addr string, currentEpoch, window uint64) (uint64, uint64, error) {
	if window == 0 {
		return 0, 0, nil
	}
	local, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return 0, 0, nil
	}
	var lo uint64
	if currentEpoch+1 > window {
		lo = currentEpoch - window + 1
	}
	var up, ov uint64
	for _, b := range local.AccuracyWindow {
		if b != nil && b.Epoch >= lo && b.Epoch <= currentEpoch {
			up += b.Upheld
			ov += b.Overturned
		}
	}
	return up, ov, nil
}

// GetSentinelActivityCounters loads forum's per-sentinel counter record and
// returns the subset needed by x/rep's reward distribution logic.
// Missing record -> zero-valued struct with no error.
func (k Keeper) GetSentinelActivityCounters(ctx context.Context, addr string) (reptypes.SentinelActivityCounters, error) {
	local, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return reptypes.SentinelActivityCounters{}, nil
	}
	return reptypes.SentinelActivityCounters{
		UpheldHides:          local.UpheldHides,
		OverturnedHides:      local.OverturnedHides,
		UpheldLocks:          local.UpheldLocks,
		OverturnedLocks:      local.OverturnedLocks,
		UpheldMoves:          local.UpheldMoves,
		OverturnedMoves:      local.OverturnedMoves,
		EpochHides:           local.EpochHides,
		EpochLocks:           local.EpochLocks,
		EpochMoves:           local.EpochMoves,
		EpochPins:            local.EpochPins,
		EpochCurations:       local.EpochCurations,
		EpochAppealsFiled:    local.EpochAppealsFiled,
		EpochAppealsResolved: local.EpochAppealsResolved,
	}, nil
}

// ResetSentinelEpochCounters zeros the per-epoch counters on the forum-side
// SentinelActivity record, preserving cumulative counters. Called by x/rep at
// sentinel-reward epoch boundaries. Missing record -> no-op.
func (k Keeper) ResetSentinelEpochCounters(ctx context.Context, addr string) error {
	local, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return nil
	}
	local.EpochHides = 0
	local.EpochLocks = 0
	local.EpochMoves = 0
	local.EpochPins = 0
	local.EpochCurations = 0
	local.EpochAppealsFiled = 0
	local.EpochAppealsResolved = 0
	return k.SentinelActivity.Set(ctx, addr, local)
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
// record may have been GC'd or never existed (e.g. the action was taken by
// gov authority and bypassed record creation).
func (k Keeper) ReverseSentinelAction(ctx context.Context, actionType reptypes.GovActionType, actionTarget string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

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
			_ = k.ThreadLockRecord.Remove(ctx, id)
			return nil
		}
		post.Locked = false
		post.LockedBy = ""
		post.LockedAt = 0
		post.LockReason = ""
		if err := k.Post.Set(ctx, id, post); err != nil {
			return fmt.Errorf("reverse lock: update post: %w", err)
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
		post.CategoryId = rec.OriginalCategoryId
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

	default:
		// Hide-like (post-level).
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
			_ = k.HideRecord.Remove(ctx, id)
			return nil
		}
		// Dangling-reference guard.
		if k.commonsKeeper == nil || !k.commonsKeeper.HasCategory(ctx, post.CategoryId) {
			sdkCtx.Logger().Warn("reverse sentinel action: parent category gone, skipping unhide",
				"post_id", id, "category_id", post.CategoryId)
			return nil
		}
		post.Status = types.PostStatus_POST_STATUS_ACTIVE
		post.HiddenBy = ""
		post.HiddenAt = 0
		if err := k.Post.Set(ctx, id, post); err != nil {
			return fmt.Errorf("reverse hide: update post: %w", err)
		}

		// Restore the author bond that MsgHidePost slashed (mint + re-lock).
		// Net DREAM supply change across slash→restore is zero. Best-effort:
		// failures are logged but do not abort the appeal-resolution tx.
		if haveHideRecord && k.repKeeper != nil && hideRecord.AuthorBondAmount != "" {
			if amt, ok := math.NewIntFromString(hideRecord.AuthorBondAmount); ok && amt.IsPositive() {
				authorAddr, addrErr := sdk.AccAddressFromBech32(post.Author)
				if addrErr != nil {
					sdkCtx.Logger().Warn("reverse sentinel action: bad author address for bond restore",
						"post_id", id, "author", post.Author, "error", addrErr)
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
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"post_unhidden",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("unhidden_by", "appeal_overturned"),
			sdk.NewAttribute("is_council", "false"),
			sdk.NewAttribute("is_self_correct", "false"),
		))
		return nil
	}
}
