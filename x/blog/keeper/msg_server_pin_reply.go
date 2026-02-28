package keeper

import (
	"context"
	"fmt"

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

	// Must be ephemeral
	if reply.ExpiresAt == 0 {
		return nil, errorsmod.Wrap(types.ErrContentNotEphemeral, "reply is not ephemeral")
	}

	// Must not be expired
	if reply.ExpiresAt <= sdkCtx.BlockTime().Unix() {
		return nil, errorsmod.Wrap(types.ErrReplyExpired, fmt.Sprintf("reply %d has expired", msg.Id))
	}

	// Must not already be pinned
	if reply.PinnedBy != "" {
		return nil, errorsmod.Wrap(types.ErrAlreadyPinned, fmt.Sprintf("reply %d is already pinned", msg.Id))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Creator must meet pin trust level
	if !k.meetsReplyTrustLevel(ctx, creatorAddr, int32(params.PinMinTrustLevel)) {
		return nil, errorsmod.Wrap(types.ErrInsufficientTrustLevel, "does not meet pin trust level requirement")
	}

	// Rate limit check (shared "pin" counter)
	if err := k.checkRateLimit(ctx, "pin", creatorAddr, params.MaxPinsPerDay); err != nil {
		return nil, err
	}

	// Remove from expiry index
	k.RemoveFromExpiryIndex(ctx, reply.ExpiresAt, "reply", reply.Id)

	// Update reply
	reply.ExpiresAt = 0
	reply.PinnedBy = msg.Creator
	reply.PinnedAt = sdkCtx.BlockTime().Unix()
	if reply.ConvictionSustained {
		reply.ConvictionSustained = false
	}
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
