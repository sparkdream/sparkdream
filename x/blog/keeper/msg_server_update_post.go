package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) UpdatePost(ctx context.Context, msg *types.MsgUpdatePost) (*types.MsgUpdatePostResponse, error) {
	// Validate creator address
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Check if post exists and verify ownership
	val, found := k.GetPost(ctx, msg.Id)
	if !found {
		return nil, errorsmod.Wrap(sdkerrors.ErrKeyNotFound, fmt.Sprintf("key %d doesn't exist", msg.Id))
	}
	if msg.Creator != val.Creator {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "incorrect owner")
	}

	// Get params for validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate title
	if len(msg.Title) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "title cannot be empty")
	}
	if uint64(len(msg.Title)) > params.MaxTitleLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"title exceeds maximum length of %d characters", params.MaxTitleLength)
	}

	// Validate body
	if len(msg.Body) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "body cannot be empty")
	}
	if uint64(len(msg.Body)) > params.MaxBodyLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"body exceeds maximum length of %d characters", params.MaxBodyLength)
	}

	// Charge cost_per_byte storage delta fee on size increase (burned)
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() {
		oldBytes := int64(len(val.Title)) + int64(len(val.Body))
		newBytes := int64(len(msg.Title)) + int64(len(msg.Body))
		if newBytes > oldBytes {
			deltaFee := sdk.NewCoin(params.CostPerByte.Denom,
				params.CostPerByte.Amount.MulRaw(newBytes-oldBytes))
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
	}

	var post = types.Post{
		Creator:     msg.Creator,
		Id:          msg.Id,
		Title:       msg.Title,
		Body:        msg.Body,
		ContentType: msg.ContentType,
	}

	k.SetPost(ctx, post)
	return &types.MsgUpdatePostResponse{}, nil
}
