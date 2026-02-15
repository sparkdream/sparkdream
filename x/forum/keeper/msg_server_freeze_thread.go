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

	// Check thread is not already archived
	if rootPost.Status == types.PostStatus_POST_STATUS_ARCHIVED {
		return nil, types.ErrPostArchived
	}

	// Check thread is inactive (use CreatedAt as proxy for last activity)
	archiveThreshold := params.ArchiveThreshold
	if archiveThreshold == 0 {
		archiveThreshold = types.DefaultArchiveThreshold
	}
	if now-rootPost.CreatedAt < archiveThreshold {
		return nil, errorsmod.Wrapf(types.ErrThreadNotInactive,
			"thread must be inactive for %d seconds", archiveThreshold)
	}

	// Check archive cooldown (from previous unarchive)
	archiveMetadata, err := k.ArchiveMetadata.Get(ctx, msg.RootId)
	if err == nil {
		// Check archive cycle limit
		if archiveMetadata.ArchiveCount >= types.DefaultMaxArchiveCycles {
			// Only operations committee can archive after cycle limit
			if !k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
				return nil, types.ErrArchiveCycleLimit
			}
		}
	}

	// Check for pending appeals that prevent archival
	lockRecord, err := k.ThreadLockRecord.Get(ctx, msg.RootId)
	if err == nil && lockRecord.AppealPending {
		return nil, types.ErrCannotArchiveThreadWithPendingAppeal
	}

	moveRecord, err := k.ThreadMoveRecord.Get(ctx, msg.RootId)
	if err == nil && moveRecord.AppealPending {
		return nil, types.ErrCannotArchiveThreadWithPendingAppeal
	}

	// Set archived status on root post
	rootPost.Status = types.PostStatus_POST_STATUS_ARCHIVED
	if err := k.Post.Set(ctx, msg.RootId, rootPost); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update root post status")
	}

	// Set archived status on all thread posts
	postCount := uint64(1) // count root post
	iter, err := k.Post.Iterate(ctx, nil)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to iterate posts")
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		post, err := iter.Value()
		if err != nil {
			continue
		}
		if post.RootId == msg.RootId && post.PostId != msg.RootId {
			post.Status = types.PostStatus_POST_STATUS_ARCHIVED
			if err := k.Post.Set(ctx, post.PostId, post); err != nil {
				sdkCtx.Logger().Error("failed to archive post", "post_id", post.PostId, "error", err)
			}
			postCount++
		}
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
