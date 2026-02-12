package keeper

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
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

	// Check thread is inactive (use CreatedAt as proxy for last activity)
	// In production, would track last reply time
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
			if !k.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
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

	// Collect all posts in thread
	var threadPosts []types.Post
	threadPosts = append(threadPosts, rootPost)

	// Iterate through all posts to find descendants
	// This is a simplified approach - in production would use proper indexing
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
			threadPosts = append(threadPosts, post)
		}
	}

	// Check post count limit
	if uint64(len(threadPosts)) > types.DefaultMaxArchivePostCount {
		return nil, errorsmod.Wrapf(types.ErrThreadTooLarge,
			"thread has %d posts, max is %d", len(threadPosts), types.DefaultMaxArchivePostCount)
	}

	// Serialize posts to JSON
	postsData, err := json.Marshal(threadPosts)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to serialize posts")
	}

	// Compress with gzip
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	_, err = gzWriter.Write(postsData)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to compress posts")
	}
	gzWriter.Close()

	compressedData := buf.Bytes()

	// Check compressed size limit
	if uint64(len(compressedData)) > types.DefaultMaxArchiveSizeBytes {
		return nil, errorsmod.Wrapf(types.ErrThreadTooLarge,
			"compressed size %d bytes exceeds max %d bytes", len(compressedData), types.DefaultMaxArchiveSizeBytes)
	}

	// Update or create archive metadata
	if err != nil {
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

	// Create archived thread record
	archivedThread := types.ArchivedThread{
		RootId:         msg.RootId,
		CompressedData: compressedData,
		ArchivedAt:     now,
		PostCount:      uint64(len(threadPosts)),
	}

	if err := k.ArchivedThread.Set(ctx, msg.RootId, archivedThread); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store archived thread")
	}

	// Delete individual posts
	for _, post := range threadPosts {
		if err := k.Post.Remove(ctx, post.PostId); err != nil {
			// Log error but continue - partial deletion is handled by having the archive
			sdkCtx.Logger().Error("failed to remove post during archive", "post_id", post.PostId, "error", err)
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_archived",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("archived_by", msg.Creator),
			sdk.NewAttribute("post_count", fmt.Sprintf("%d", len(threadPosts))),
			sdk.NewAttribute("compressed_size", fmt.Sprintf("%d", len(compressedData))),
			sdk.NewAttribute("archive_count", fmt.Sprintf("%d", archiveMetadata.ArchiveCount)),
		),
	)

	return &types.MsgFreezeThreadResponse{}, nil
}
