package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) HideReply(ctx context.Context, msg *types.MsgHideReply) (*types.MsgHideReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get reply
	reply, found := k.GetReply(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(types.ErrReplyNotFound, fmt.Sprintf("reply %d doesn't exist", msg.Id))
	}

	// Reply must be active to hide
	if reply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrReplyDeleted, "reply has been deleted")
	}
	if reply.Status == types.ReplyStatus_REPLY_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrReplyNotHidden, "reply is already hidden")
	}

	// Sender must be POST author (not reply author)
	post, postFound := k.GetPost(ctx, reply.PostId)
	if !postFound {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("parent post %d doesn't exist", reply.PostId))
	}
	if msg.Creator != post.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only post author can hide a reply")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	reply.Status = types.ReplyStatus_REPLY_STATUS_HIDDEN
	reply.HiddenBy = msg.Creator
	reply.HiddenAt = sdkCtx.BlockTime().Unix()

	// Decrement post reply count (hidden replies don't count)
	if post.ReplyCount > 0 {
		post.ReplyCount--
	}
	k.SetPost(ctx, post)
	k.SetReply(ctx, reply)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reply.hidden",
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
		sdk.NewAttribute("hidden_by", msg.Creator),
	))

	return &types.MsgHideReplyResponse{}, nil
}
