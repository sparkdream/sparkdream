package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnlockThread(ctx context.Context, msg *types.MsgUnlockThread) (*types.MsgUnlockThreadResponse, error) {
	if err := k.requireForumNotPaused(ctx); err != nil {
		return nil, err
	}
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Load post
	post, err := k.Post.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.RootId))
	}

	// Check thread is locked
	if !post.Locked {
		return nil, types.ErrThreadNotLocked
	}

	// Check if sender is operations committee
	isGovAuthority := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	if isGovAuthority {
		// A council unlock supersedes a sentinel lock. Settle its record so
		// the reservation does not leak (the author can no longer appeal an
		// unlocked thread), unless the lock is under appeal: the jury owns
		// it, and the council resolves it via MsgResolveGovActionAppeal.
		if lockRecord, err := k.ThreadLockRecord.Get(ctx, msg.RootId); err == nil {
			if lockRecord.AppealPending {
				return nil, errorsmod.Wrap(types.ErrAppealPending, "the lock is under appeal; resolve the appeal instead")
			}
			k.releaseSentinelCommit(ctx, lockRecord.Sentinel, lockRecord.CommittedAmount, "council unlock", msg.RootId)
			if err := k.LockRecordExpiry.Remove(ctx, collections.Join(lockExpiryAt(lockRecord), msg.RootId)); err != nil {
				return nil, err
			}
			if err := k.ThreadLockRecord.Remove(ctx, msg.RootId); err != nil {
				return nil, errorsmod.Wrap(err, "failed to remove lock record")
			}
		}
	} else {
		// Sentinel can only unlock threads they locked
		if !k.canActAs(ctx, msg.Creator, post.LockedBy) {
			return nil, errorsmod.Wrap(types.ErrUnauthorized, "sentinels can only unlock threads they locked")
		}

		// Check lock record exists and is a sentinel lock. An authority
		// record (no sentinel) belongs to a committee or council lock, which
		// only the council or an appeal can undo, even if the member who
		// signed it has since left the committee.
		lockRecord, err := k.ThreadLockRecord.Get(ctx, msg.RootId)
		if err != nil || lockRecord.Sentinel == "" {
			return nil, errorsmod.Wrap(types.ErrUnauthorized, "not a sentinel lock: this was a governance authority lock")
		}

		// Check no appeal is pending
		if lockRecord.AppealPending {
			return nil, types.ErrAppealPending
		}

		// Check lock appeal window hasn't expired
		lockAppealDeadline := lockRecord.LockedAt + types.DefaultLockAppealDeadline
		if now >= lockAppealDeadline {
			return nil, errorsmod.Wrapf(types.ErrLockAppealExpired,
				"lock appeal window expired at %d", lockAppealDeadline)
		}

		// Release the sentinel's reserved lock bond. A self-unlock within the
		// appeal window (no appeal pending) means the lock will not be appealed,
		// so there is nothing to slash and the reservation must be freed —
		// mirroring the MsgUnhidePost / MsgUnpinReply self-correct paths.
		// Best-effort: a release failure is logged but does not block the unlock.
		if lockRecord.CommittedAmount != "" && k.repKeeper != nil {
			if committed, ok := math.NewIntFromString(lockRecord.CommittedAmount); ok && committed.IsPositive() {
				if err := k.repKeeper.ReleaseBond(ctx, reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, lockRecord.Sentinel, committed); err != nil {
					sdkCtx.Logger().Warn("unlock thread: failed to release sentinel lock bond",
						"sentinel", lockRecord.Sentinel, "root_id", msg.RootId, "error", err)
				}
			}
		}

		// Remove lock record (atomic coordination point)
		if err := k.LockRecordExpiry.Remove(ctx, collections.Join(lockExpiryAt(lockRecord), msg.RootId)); err != nil {
			return nil, err
		}
		if err := k.ThreadLockRecord.Remove(ctx, msg.RootId); err != nil {
			return nil, errorsmod.Wrap(err, "failed to remove lock record")
		}
	}

	// Update post
	post.Locked = false
	post.LockedBy = ""
	post.LockedAt = 0
	post.LockReason = ""

	if err := k.Post.Set(ctx, msg.RootId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}
	if err := k.LockedRoots.Remove(ctx, msg.RootId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to unindex locked thread")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_unlocked",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("unlocked_by", msg.Creator),
			sdk.NewAttribute("is_gov_authority", fmt.Sprintf("%t", isGovAuthority)),
		),
	)

	// Keep the thread's bounty timer in step with its moderation state.
	if err := k.reconcileThreadBounty(ctx, msg.RootId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to resume thread bounty")
	}

	return &types.MsgUnlockThreadResponse{}, nil
}
