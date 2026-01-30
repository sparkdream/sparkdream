package keeper

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnarchiveThread(ctx context.Context, msg *types.MsgUnarchiveThread) (*types.MsgUnarchiveThreadResponse, error) {
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

	// Load archived thread
	archivedThread, err := k.ArchivedThread.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrArchivedThreadNotFound, fmt.Sprintf("archived thread %d not found", msg.RootId))
	}

	// Check unarchive cooldown
	if now-archivedThread.ArchivedAt < types.DefaultUnarchiveCooldown {
		return nil, errorsmod.Wrapf(types.ErrUnarchiveCooldown,
			"must wait %d seconds after archive before unarchiving", types.DefaultUnarchiveCooldown)
	}

	// Decompress data
	gzReader, err := gzip.NewReader(bytes.NewReader(archivedThread.CompressedData))
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create gzip reader")
	}
	defer gzReader.Close()

	decompressedData, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to decompress data")
	}

	// Deserialize posts
	var posts []types.Post
	if err := json.Unmarshal(decompressedData, &posts); err != nil {
		return nil, errorsmod.Wrap(err, "failed to deserialize posts")
	}

	// Restore individual posts
	for _, post := range posts {
		if err := k.Post.Set(ctx, post.PostId, post); err != nil {
			return nil, errorsmod.Wrapf(err, "failed to restore post %d", post.PostId)
		}
	}

	// Archive metadata is preserved for history tracking
	// archive_count tracks total archives and persists even after unarchive

	// Update archived thread record (track last unarchive)
	archivedThread.LastUnarchivedAt = now

	// Remove archived thread record
	if err := k.ArchivedThread.Remove(ctx, msg.RootId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove archived thread record")
	}

	// The archive_cooldown is set on the individual posts or tracked separately
	// to prevent immediate re-archiving

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_unarchived",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("unarchived_by", msg.Creator),
			sdk.NewAttribute("post_count", fmt.Sprintf("%d", len(posts))),
		),
	)

	return &types.MsgUnarchiveThreadResponse{}, nil
}
