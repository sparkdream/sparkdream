package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) DeletePost(ctx context.Context, msg *types.MsgDeletePost) (*types.MsgDeletePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Verify author ownership
	if post.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotPostAuthor, "only the author can delete their post")
	}

	// Check post status - cannot delete hidden or already deleted posts
	switch post.Status {
	case types.PostStatus_POST_STATUS_HIDDEN:
		return nil, types.ErrCannotDeleteHiddenPost
	case types.PostStatus_POST_STATUS_DELETED:
		return nil, types.ErrPostDeleted
	case types.PostStatus_POST_STATUS_ARCHIVED:
		return nil, types.ErrPostArchived
	}

	// Soft delete: update status and clear content
	post.Status = types.PostStatus_POST_STATUS_DELETED
	post.Content = "[deleted]"

	// Store updated post
	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_deleted",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("author", msg.Creator),
		),
	)

	return &types.MsgDeletePostResponse{}, nil
}
