package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) DeleteReply(ctx context.Context, msg *types.MsgDeleteReply) (*types.MsgDeleteReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get reply
	reply, found := k.GetReply(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(types.ErrReplyNotFound, fmt.Sprintf("reply %d doesn't exist", msg.Id))
	}

	// Reply must not already be deleted
	if reply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrReplyDeleted, "reply is already deleted")
	}

	// Sender must be reply author OR post author
	post, postFound := k.GetPost(ctx, reply.PostId)
	if !postFound {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("parent post %d doesn't exist", reply.PostId))
	}
	if msg.Creator != reply.Creator && msg.Creator != post.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only reply author or post author can delete a reply")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Remove from expiry index if ephemeral
	if reply.ExpiresAt > 0 {
		k.RemoveFromExpiryIndex(ctx, reply.ExpiresAt, "reply", reply.Id)
	}

	// Decrement post reply count if reply was active (hidden replies already decremented)
	if reply.Status == types.ReplyStatus_REPLY_STATUS_ACTIVE {
		if post.ReplyCount > 0 {
			post.ReplyCount--
		}
		k.SetPost(ctx, post)
	}

	// Tombstone the reply
	reply.Body = ""
	reply.Status = types.ReplyStatus_REPLY_STATUS_DELETED
	reply.HiddenBy = ""
	reply.HiddenAt = 0
	reply.ExpiresAt = 0
	k.SetReply(ctx, reply)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reply.deleted",
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
		sdk.NewAttribute("creator", msg.Creator),
	))

	return &types.MsgDeleteReplyResponse{}, nil
}
