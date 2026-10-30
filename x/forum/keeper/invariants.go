package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

// RegisterInvariants registers all forum module invariants.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "post-counter", PostCounterInvariant(k))
	ir.RegisterRoute(types.ModuleName, "bounty-post-reference", BountyPostReferenceInvariant(k))
	ir.RegisterRoute(types.ModuleName, "thread-lock-consistency", ThreadLockConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "hide-record-consistency", HideRecordConsistencyInvariant(k))
}

// PostCounterInvariant checks that the PostSeq counter is greater than every
// stored post ID (no post ID >= the counter value).
func PostCounterInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		postSeq, err := k.PostSeq.Peek(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "post-counter",
				fmt.Sprintf("failed to read post sequence: %v", err)), true
		}

		var broken int
		var msg string

		err = k.Post.Walk(ctx, nil, func(id uint64, post types.Post) (bool, error) {
			if post.PostId >= postSeq {
				broken++
				msg += fmt.Sprintf("  post ID %d >= PostSeq %d\n", post.PostId, postSeq)
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "post-counter",
				fmt.Sprintf("error walking posts: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "post-counter",
			fmt.Sprintf("found %d post counter violations\n%s", broken, msg)), broken > 0
	}
}

// BountyPostReferenceInvariant checks that every open bounty (ACTIVE or
// suspended) references an existing root post and is the one its thread
// index points to (FRM-25: a suspended bounty without its index entry is
// unreachable by moderation reconciliation).
func BountyPostReferenceInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.Bounty.Walk(ctx, nil, func(id uint64, bounty types.Bounty) (bool, error) {
			if !bounty.IsOpen() {
				return false, nil
			}

			_, err := k.Post.Get(ctx, bounty.ThreadId)
			if err != nil {
				broken++
				msg += fmt.Sprintf("  bounty %d references non-existent root post %d\n",
					id, bounty.ThreadId)
			}
			if indexed, err := k.ActiveBountyByThread.Get(ctx, bounty.ThreadId); err != nil || indexed != id {
				broken++
				msg += fmt.Sprintf("  open bounty %d is not the thread index entry of thread %d\n",
					id, bounty.ThreadId)
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "bounty-post-reference",
				fmt.Sprintf("error walking bounties: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "bounty-post-reference",
			fmt.Sprintf("found %d bounty reference violations\n%s", broken, msg)), broken > 0
	}
}

// ThreadLockConsistencyInvariant checks that every ThreadLockRecord
// references an existing root post that is locked by the record's sentinel
// (an authority record from a committee or council lock has no sentinel).
func ThreadLockConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.ThreadLockRecord.Walk(ctx, nil, func(rootID uint64, tlr types.ThreadLockRecord) (bool, error) {
			post, err := k.Post.Get(ctx, rootID)
			if err != nil {
				broken++
				msg += fmt.Sprintf("  lock record for non-existent root post %d\n", rootID)
				return false, nil
			}
			if !post.Locked {
				broken++
				msg += fmt.Sprintf("  lock record exists for root post %d but post.Locked=false\n", rootID)
			} else if tlr.Sentinel != "" && tlr.Sentinel != post.LockedBy {
				broken++
				msg += fmt.Sprintf("  lock record for root post %d names sentinel %s but post.LockedBy=%s\n",
					rootID, tlr.Sentinel, post.LockedBy)
			}
			if post.ParentId != 0 {
				broken++
				msg += fmt.Sprintf("  lock record references non-root post %d (parent_id=%d)\n",
					rootID, post.ParentId)
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "thread-lock-consistency",
				fmt.Sprintf("error walking thread lock records: %v", err)), true
		}

		// No reverse check: a locked root without a record is legitimate. A
		// sentinel or authority lock whose appeal window closed (or whose
		// appeal was UPHELD) drops its record while the thread stays locked.

		return sdk.FormatInvariant(types.ModuleName, "thread-lock-consistency",
			fmt.Sprintf("found %d thread lock consistency violations\n%s", broken, msg)), broken > 0
	}
}

// HideRecordConsistencyInvariant checks that every HideRecord references
// an existing post that is in HIDDEN status.
func HideRecordConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.HideRecord.Walk(ctx, nil, func(postID uint64, hr types.HideRecord) (bool, error) {
			post, err := k.Post.Get(ctx, postID)
			if err != nil {
				broken++
				msg += fmt.Sprintf("  hide record for non-existent post %d\n", postID)
				return false, nil
			}
			// HideRecord should only exist for HIDDEN posts (or posts pending appeal)
			if post.Status != types.PostStatus_POST_STATUS_HIDDEN {
				broken++
				msg += fmt.Sprintf("  hide record exists for post %d but status is %s\n",
					postID, post.Status.String())
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "hide-record-consistency",
				fmt.Sprintf("error walking hide records: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "hide-record-consistency",
			fmt.Sprintf("found %d hide record consistency violations\n%s", broken, msg)), broken > 0
	}
}
