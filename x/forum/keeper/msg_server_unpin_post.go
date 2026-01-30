package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UnpinPost unpins a root post (thread) from its category.
// Only governance authority can unpin root posts.
func (k msgServer) UnpinPost(ctx context.Context, msg *types.MsgUnpinPost) (*types.MsgUnpinPostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Only governance authority can unpin root posts
	if !k.IsGovAuthority(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance authority can unpin posts")
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Check if pinned
	if !post.Pinned {
		return nil, errorsmod.Wrap(types.ErrNotPinned, "post is not pinned")
	}

	// Unpin the post
	post.Pinned = false
	post.PinnedBy = ""
	post.PinnedAt = 0
	post.PinPriority = 0

	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_unpinned",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("unpinned_by", msg.Creator),
		),
	)

	return &types.MsgUnpinPostResponse{}, nil
}
