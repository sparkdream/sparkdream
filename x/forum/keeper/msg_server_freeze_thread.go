package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) FreezeThread(ctx context.Context, msg *types.MsgFreezeThread) (*types.MsgFreezeThreadResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check forum_paused param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.ForumPaused {
		return nil, types.ErrForumPaused
	}

	// Load root post
	rootPost, err := k.Post.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.RootId))
	}

	// Check this is a root post
	if rootPost.ParentId != 0 {
		return nil, types.ErrNotRootPost
	}

	// Only a live thread can be archived. A HIDDEN or DELETED root stays in
	// its moderation lifecycle; archiving it would let MsgUnarchiveThread
	// bring it back as ACTIVE.
	switch rootPost.Status {
	case types.PostStatus_POST_STATUS_ARCHIVED:
		return nil, types.ErrPostArchived
	case types.PostStatus_POST_STATUS_ACTIVE:
	default:
		return nil, errorsmod.Wrapf(types.ErrPostStatus, "cannot archive a thread whose root is %s", rootPost.Status)
	}

	// An open bounty (ACTIVE or suspended by moderation) must be awarded,
	// cancelled or expire first: archiving would freeze the replies it pays.
	if has, _ := k.ActiveBountyByThread.Has(ctx, msg.RootId); has {
		return nil, errorsmod.Wrap(types.ErrBountyAlreadyExists, "thread has an open bounty; archive it after the bounty closes")
	}

	// Collect the thread from the PostsByRoot index (O(thread), not O(forum)).
	// Root + replies must fit MaxArchivePostCount so freeze and unarchive stay
	// bounded in gas.
	replyIDs, err := k.threadReplyIDs(ctx, msg.RootId, types.MaxArchivePostCount-1)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to walk thread")
	}
	if uint64(len(replyIDs))+1 > types.MaxArchivePostCount {
		return nil, errorsmod.Wrapf(types.ErrThreadTooLarge, "thread exceeds %d posts", types.MaxArchivePostCount)
	}
	replies := make([]types.Post, 0, len(replyIDs))
	lastActivity := rootPost.CreatedAt
	if rootPost.EditedAt > lastActivity {
		lastActivity = rootPost.EditedAt
	}
	for _, id := range replyIDs {
		reply, err := k.Post.Get(ctx, id)
		if err != nil {
			continue
		}
		// A pending hide anywhere in the thread keeps it out of the archive:
		// the hide's verdict or expiry must be able to act on a live thread.
		if has, _ := k.HideRecord.Has(ctx, id); has {
			return nil, errorsmod.Wrapf(types.ErrCannotArchiveThreadWithPendingAppeal,
				"post %d has an unresolved hide", id)
		}
		if reply.CreatedAt > lastActivity {
			lastActivity = reply.CreatedAt
		}
		if reply.EditedAt > lastActivity {
			lastActivity = reply.EditedAt
		}
		replies = append(replies, reply)
	}

	// Inactivity is measured from the thread's last activity (newest reply or
	// edit), not the root's creation time, so a busy thread cannot be
	// archived just because it is old.
	archiveThreshold := params.ArchiveThreshold
	if archiveThreshold == 0 {
		archiveThreshold = types.DefaultArchiveThreshold
	}
	if now-lastActivity < archiveThreshold {
		return nil, errorsmod.Wrapf(types.ErrThreadNotInactive,
			"thread must be inactive for %d seconds", archiveThreshold)
	}

	// Check archive cooldown (since the last unarchive) and cycle limit.
	archiveMetadata, err := k.ArchiveMetadata.Get(ctx, msg.RootId)
	if err == nil {
		archiveCooldown := params.ArchiveCooldown
		if archiveCooldown == 0 {
			archiveCooldown = types.DefaultArchiveCooldown
		}
		if archiveMetadata.LastUnarchivedAt > 0 && now < archiveMetadata.LastUnarchivedAt+archiveCooldown {
			return nil, errorsmod.Wrapf(types.ErrArchiveCooldown,
				"thread was unarchived at %d; it can be archived again at %d",
				archiveMetadata.LastUnarchivedAt, archiveMetadata.LastUnarchivedAt+archiveCooldown)
		}
		if archiveMetadata.ArchiveCount >= types.DefaultMaxArchiveCycles {
			// Only operations committee can archive after cycle limit
			if !k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
				return nil, types.ErrArchiveCycleLimit
			}
		}
	}

	// Check for pending appeals that prevent archival
	if has, _ := k.HideRecord.Has(ctx, msg.RootId); has {
		return nil, errorsmod.Wrap(types.ErrCannotArchiveThreadWithPendingAppeal, "root post has an unresolved hide")
	}
	lockRecord, err := k.ThreadLockRecord.Get(ctx, msg.RootId)
	if err == nil && lockRecord.AppealPending {
		return nil, types.ErrCannotArchiveThreadWithPendingAppeal
	}

	moveRecord, err := k.ThreadMoveRecord.Get(ctx, msg.RootId)
	if err == nil && moveRecord.AppealPending {
		return nil, types.ErrCannotArchiveThreadWithPendingAppeal
	}

	// Archive the root and every ACTIVE reply. Only ACTIVE posts are marked
	// ARCHIVED, and MsgUnarchiveThread restores only ARCHIVED posts, so a
	// HIDDEN or DELETED reply keeps its status across the cycle.
	rootPost.Status = types.PostStatus_POST_STATUS_ARCHIVED
	if err := k.Post.Set(ctx, msg.RootId, rootPost); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update root post status")
	}
	postCount := uint64(1) // count root post
	for _, post := range replies {
		if post.Status != types.PostStatus_POST_STATUS_ACTIVE {
			continue
		}
		post.Status = types.PostStatus_POST_STATUS_ARCHIVED
		if err := k.Post.Set(ctx, post.PostId, post); err != nil {
			return nil, errorsmod.Wrapf(err, "failed to archive post %d", post.PostId)
		}
		postCount++
	}

	// Update or create archive metadata
	if archiveMetadata.RootId == 0 {
		// New metadata
		archiveMetadata = types.ArchiveMetadata{
			RootId:             msg.RootId,
			ArchiveCount:       0,
			FirstArchivedAt:    now,
			LastArchivedAt:     now,
			HrOverrideRequired: false,
		}
	}
	archiveMetadata.ArchiveCount++
	archiveMetadata.LastArchivedAt = now
	if archiveMetadata.ArchiveCount >= types.DefaultMaxArchiveCycles {
		archiveMetadata.HrOverrideRequired = true
	}

	if err := k.ArchiveMetadata.Set(ctx, msg.RootId, archiveMetadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store archive metadata")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_archived",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("archived_by", msg.Creator),
			sdk.NewAttribute("post_count", fmt.Sprintf("%d", postCount)),
			sdk.NewAttribute("archive_count", fmt.Sprintf("%d", archiveMetadata.ArchiveCount)),
		),
	)

	return &types.MsgFreezeThreadResponse{}, nil
}
