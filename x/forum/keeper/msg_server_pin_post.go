package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PinPost pins a root post (thread) to the top of its category.
// Only governance authority can pin root posts.
func (k msgServer) PinPost(ctx context.Context, msg *types.MsgPinPost) (*types.MsgPinPostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Only governance, council, or operations committee can pin root posts
	if !k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance, council, or operations committee can pin posts")
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Verify this is a root post (thread)
	if post.ParentId != 0 {
		return nil, errorsmod.Wrap(types.ErrNotRootPost, "can only pin root posts (threads)")
	}

	// Check post is not deleted or hidden
	if post.Status == types.PostStatus_POST_STATUS_DELETED || post.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrapf(types.ErrPostStatus, "cannot pin post with status %s", post.Status.String())
	}

	// Check if already pinned
	if post.Pinned {
		return nil, errorsmod.Wrap(types.ErrAlreadyPinned, "post is already pinned")
	}

	// Pin the post
	post.Pinned = true
	post.PinnedBy = msg.Creator
	post.PinnedAt = now
	post.PinPriority = msg.Priority

	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_pinned",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("pinned_by", msg.Creator),
			sdk.NewAttribute("priority", fmt.Sprintf("%d", msg.Priority)),
		),
	)

	return &types.MsgPinPostResponse{}, nil
}
