package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	"cosmossdk.io/collections"
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

	// Remove initiative link if post references an initiative
	if post.InitiativeId > 0 && k.repKeeper != nil {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		if err := k.repKeeper.RemoveContentInitiativeLink(ctx, post.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_FORUM_CONTENT), msg.PostId); err != nil {
			sdkCtx.Logger().Error("failed to remove content initiative link on delete", "post_id", msg.PostId, "error", err)
		}
	}

	// FORUM-S2-8: drop secondary index entries for the now-DELETED post.
	if post.ParentId == 0 {
		_ = k.PostsByUpvotes.Remove(ctx, collections.Join(post.UpvoteCount, post.PostId))
		if post.Pinned {
			_ = k.PostsByPinned.Remove(ctx, collections.Join(post.CategoryId, post.PostId))
		}
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
