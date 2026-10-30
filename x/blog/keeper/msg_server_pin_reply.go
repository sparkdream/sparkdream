package keeper

import (
	"context"
	"fmt"
	commontypes "sparkdream/x/common/types"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) PinReply(ctx context.Context, msg *types.MsgPinReply) (*types.MsgPinReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get reply, must exist and be active
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

	// Pin is display-only and requires the reply to already be permanent.
	if reply.ExpiresAt > 0 {
		return nil, errorsmod.Wrap(types.ErrCannotPinEphemeral, fmt.Sprintf("reply %d is ephemeral; call MakeReplyPermanent first", msg.Id))
	}

	if reply.PinnedBy != "" {
		return nil, errorsmod.Wrap(types.ErrAlreadyPinned, fmt.Sprintf("reply %d is already pinned", msg.Id))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if !k.meetsReplyTrustLevel(ctx, creatorAddr, commontypes.Saturate[int32](params.PinMinTrustLevel)) {
		return nil, k.trustLevelError(ctx, creatorAddr, commontypes.Saturate[int32](params.PinMinTrustLevel), "pinning replies")
	}

	if err := k.checkRateLimit(ctx, "pin", creatorAddr, params.MaxPinsPerDay); err != nil {
		return nil, err
	}

	reply.PinnedBy = msg.Creator
	reply.PinnedAt = sdkCtx.BlockTime().Unix()
	k.SetReply(ctx, reply)

	// Increment rate limit
	k.incrementRateLimit(ctx, "pin", creatorAddr)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reply.pinned",
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
		sdk.NewAttribute("pinned_by", msg.Creator),
	))

	return &types.MsgPinReplyResponse{}, nil
}
