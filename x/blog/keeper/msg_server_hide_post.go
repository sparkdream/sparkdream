package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) HidePost(ctx context.Context, msg *types.MsgHidePost) (*types.MsgHidePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	post, found := k.GetPost(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d doesn't exist", msg.Id))
	}

	// Post must be active to hide
	if post.Status == types.PostStatus_POST_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrPostDeleted, "post has been deleted")
	}
	if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrPostNotHidden, "post is already hidden")
	}

	// Sender must be post author
	if msg.Creator != post.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only post author can hide a post")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Remove initiative link while hidden
	if post.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.RemoveContentInitiativeLink(ctx, post.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_BLOG_CONTENT), post.Id); err != nil {
			sdkCtx.Logger().Error("failed to remove content initiative link on hide", "post_id", post.Id, "error", err)
		}
	}

	post.Status = types.PostStatus_POST_STATUS_HIDDEN
	post.HiddenBy = msg.Creator
	post.HiddenAt = sdkCtx.BlockTime().Unix()
	k.SetPost(ctx, post)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.post.hidden",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("hidden_by", msg.Creator),
	))

	return &types.MsgHidePostResponse{}, nil
}
