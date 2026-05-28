package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UnpinReply clears the pinned marker on a reply without re-introducing an
// expiry. Mirrors UnpinPost — shares the same trust gate and rate-limit
// counter as Pin/PinReply.
func (k msgServer) UnpinReply(ctx context.Context, msg *types.MsgUnpinReply) (*types.MsgUnpinReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	reply, found := k.GetReply(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(types.ErrReplyNotFound, fmt.Sprintf("reply %d not found", msg.Id))
	}
	if reply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrReplyDeleted, fmt.Sprintf("reply %d has been deleted", msg.Id))
	}
	if reply.Status == types.ReplyStatus_REPLY_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrReplyHidden, fmt.Sprintf("reply %d is hidden", msg.Id))
	}

	if reply.PinnedBy == "" {
		return nil, errorsmod.Wrap(types.ErrNotPinned, fmt.Sprintf("reply %d is not pinned", msg.Id))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if !k.meetsReplyTrustLevel(ctx, creatorAddr, int32(params.PinMinTrustLevel)) {
		return nil, k.trustLevelError(ctx, creatorAddr, int32(params.PinMinTrustLevel), "unpinning replies")
	}

	if err := k.checkRateLimit(ctx, "pin", creatorAddr, params.MaxPinsPerDay); err != nil {
		return nil, err
	}

	prevPinnedBy := reply.PinnedBy
	reply.PinnedBy = ""
	reply.PinnedAt = 0
	k.SetReply(ctx, reply)

	k.incrementRateLimit(ctx, "pin", creatorAddr)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reply.unpinned",
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
		sdk.NewAttribute("unpinned_by", msg.Creator),
		sdk.NewAttribute("was_pinned_by", prevPinnedBy),
	))

	return &types.MsgUnpinReplyResponse{}, nil
}
