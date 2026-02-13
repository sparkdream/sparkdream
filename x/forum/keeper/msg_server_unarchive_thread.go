package keeper

import (
	"context"
	"fmt"

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

	// Load root post
	rootPost, err := k.Post.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.RootId))
	}

	// Check this is a root post and it's archived
	if rootPost.ParentId != 0 {
		return nil, types.ErrNotRootPost
	}
	if rootPost.Status != types.PostStatus_POST_STATUS_ARCHIVED {
		return nil, errorsmod.Wrap(types.ErrArchivedThreadNotFound, fmt.Sprintf("thread %d is not archived", msg.RootId))
	}

	// Check unarchive cooldown
	archiveMetadata, err := k.ArchiveMetadata.Get(ctx, msg.RootId)
	if err == nil {
		unarchiveCooldown := params.UnarchiveCooldown
		if unarchiveCooldown == 0 {
			unarchiveCooldown = types.DefaultUnarchiveCooldown
		}
		if now-archiveMetadata.LastArchivedAt < unarchiveCooldown {
			return nil, errorsmod.Wrapf(types.ErrUnarchiveCooldown,
				"must wait %d seconds after archive before unarchiving", unarchiveCooldown)
		}
	}

	// Restore root post status
	rootPost.Status = types.PostStatus_POST_STATUS_ACTIVE
	if err := k.Post.Set(ctx, msg.RootId, rootPost); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update root post status")
	}

	// Restore all thread posts
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
		if post.RootId == msg.RootId && post.PostId != msg.RootId && post.Status == types.PostStatus_POST_STATUS_ARCHIVED {
			post.Status = types.PostStatus_POST_STATUS_ACTIVE
			if err := k.Post.Set(ctx, post.PostId, post); err != nil {
				sdkCtx.Logger().Error("failed to unarchive post", "post_id", post.PostId, "error", err)
			}
			postCount++
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_unarchived",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("unarchived_by", msg.Creator),
			sdk.NewAttribute("post_count", fmt.Sprintf("%d", postCount)),
		),
	)

	return &types.MsgUnarchiveThreadResponse{}, nil
}
