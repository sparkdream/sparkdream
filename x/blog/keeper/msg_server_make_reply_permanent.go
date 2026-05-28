package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MakeReplyPermanent mirrors MakePostPermanent for replies.
func (k msgServer) MakeReplyPermanent(ctx context.Context, msg *types.MsgMakeReplyPermanent) (*types.MsgMakeReplyPermanentResponse, error) {
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
	if reply.ExpiresAt > 0 && reply.ExpiresAt <= sdkCtx.BlockTime().Unix() {
		return nil, errorsmod.Wrap(types.ErrReplyExpired, fmt.Sprintf("reply %d has expired", msg.Id))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if !k.meetsReplyTrustLevel(ctx, creatorAddr, int32(params.MakePermanentMinTrustLevel)) {
		return nil, k.trustLevelError(ctx, creatorAddr, int32(params.MakePermanentMinTrustLevel), "making replies permanent")
	}

	if err := k.checkRateLimit(ctx, "pin", creatorAddr, params.MaxPinsPerDay); err != nil {
		return nil, err
	}

	if reply.ExpiresAt == 0 {
		k.incrementRateLimit(ctx, "pin", creatorAddr)
		return &types.MsgMakeReplyPermanentResponse{}, nil
	}

	oldExpiresAt := reply.ExpiresAt
	reply.ExpiresAt = 0
	if reply.ConvictionSustained {
		reply.ConvictionSustained = false
	}
	k.SetReply(ctx, reply)
	k.RemoveFromExpiryIndex(ctx, oldExpiresAt, "reply", reply.Id)
	k.RemoveEphemeralAuthorIndex(ctx, reply.Creator, EphemeralKindReply, reply.Id)

	k.incrementRateLimit(ctx, "pin", creatorAddr)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reply.upgraded",
		sdk.NewAttribute("id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", reply.PostId)),
		sdk.NewAttribute("creator", reply.Creator),
		sdk.NewAttribute("by", msg.Creator),
		sdk.NewAttribute("via", "make_permanent"),
	))

	return &types.MsgMakeReplyPermanentResponse{}, nil
}
