package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) UpdateReply(ctx context.Context, msg *types.MsgUpdateReply) (*types.MsgUpdateReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get reply
	reply, found := k.GetReply(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(types.ErrReplyNotFound, fmt.Sprintf("reply %d doesn't exist", msg.Id))
	}

	// Reply must be active to update
	if reply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrReplyDeleted, "reply has been deleted")
	}
	if reply.Status == types.ReplyStatus_REPLY_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrReplyHidden, "reply is hidden")
	}

	// Sender must be reply author
	if msg.Creator != reply.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only reply author can update a reply")
	}

	// Get params for validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate body
	if len(msg.Body) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "body cannot be empty")
	}
	if uint64(len(msg.Body)) > params.MaxReplyLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"body exceeds maximum length of %d characters", params.MaxReplyLength)
	}

	// High-water mark fee: only charge for bytes above the previous high water mark
	newBytes := uint64(len(msg.Body))
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() && newBytes > reply.FeeBytesHighWater {
		delta := int64(newBytes - reply.FeeBytesHighWater)
		deltaFee := sdk.NewCoin(params.CostPerByte.Denom,
			params.CostPerByte.Amount.MulRaw(delta))
		if deltaFee.IsPositive() {
			creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
			if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(deltaFee)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to charge storage delta fee")
			}
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(deltaFee)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to burn storage delta fee")
			}
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Update reply fields
	reply.Body = msg.Body
	reply.ContentType = msg.ContentType
	reply.Edited = true
	reply.EditedAt = sdkCtx.BlockTime().Unix()
	if newBytes > reply.FeeBytesHighWater {
		reply.FeeBytesHighWater = newBytes
	}

	k.SetReply(ctx, reply)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reply.updated",
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("creator", msg.Creator),
	))

	return &types.MsgUpdateReplyResponse{}, nil
}
