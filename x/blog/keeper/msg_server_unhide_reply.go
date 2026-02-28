package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnhideReply(ctx context.Context, msg *types.MsgUnhideReply) (*types.MsgUnhideReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get reply
	reply, found := k.GetReply(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(types.ErrReplyNotFound, fmt.Sprintf("reply %d doesn't exist", msg.Id))
	}

	// Reply must be hidden to unhide
	if reply.Status != types.ReplyStatus_REPLY_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrReplyNotHidden, "reply is not hidden")
	}

	// Sender must be POST author
	post, postFound := k.GetPost(ctx, reply.PostId)
	if !postFound {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("parent post %d doesn't exist", reply.PostId))
	}
	if msg.Creator != post.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only post author can unhide a reply")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	reply.Status = types.ReplyStatus_REPLY_STATUS_ACTIVE
	reply.HiddenBy = ""
	reply.HiddenAt = 0

	// Increment post reply count (unhidden replies count again)
	post.ReplyCount++
	k.SetPost(ctx, post)
	k.SetReply(ctx, reply)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reply.unhidden",
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
		sdk.NewAttribute("creator", msg.Creator),
	))

	return &types.MsgUnhideReplyResponse{}, nil
}
