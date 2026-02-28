package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) UnhidePost(ctx context.Context, msg *types.MsgUnhidePost) (*types.MsgUnhidePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	post, found := k.GetPost(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d doesn't exist", msg.Id))
	}

	// Post must be hidden to unhide
	if post.Status != types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrPostNotHidden, "post is not hidden")
	}

	// Sender must be post author
	if msg.Creator != post.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only post author can unhide a post")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	post.Status = types.PostStatus_POST_STATUS_ACTIVE
	post.HiddenBy = ""
	post.HiddenAt = 0
	k.SetPost(ctx, post)

	// Re-register initiative link now that post is active again
	if post.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.RegisterContentInitiativeLink(ctx, post.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_BLOG_CONTENT), post.Id); err != nil {
			sdkCtx.Logger().Error("failed to re-register content initiative link on unhide", "post_id", post.Id, "error", err)
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.post.unhidden",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("creator", msg.Creator),
	))

	return &types.MsgUnhidePostResponse{}, nil
}
