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
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d doesn't exist", msg.Id))
	}
	if msg.Creator != val.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "incorrect owner")
	}

	// Post must be active to update
	if val.Status == types.PostStatus_POST_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrPostDeleted, "post has been deleted")
	}
	if val.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrPostHidden, "post is hidden")
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

	// Validate min_reply_trust_level
	if msg.MinReplyTrustLevel < -1 || msg.MinReplyTrustLevel > 4 {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"min_reply_trust_level must be between -1 and 4, got %d", msg.MinReplyTrustLevel)
	}

	// High-water mark fee: only charge for bytes above the previous high water mark
	newBytes := uint64(len(msg.Title) + len(msg.Body))
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() && newBytes > val.FeeBytesHighWater {
		delta := int64(newBytes - val.FeeBytesHighWater)
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

	oldTags := val.Tags

	// Validate the full new tag set without touching usage metadata, then bump
	// usage only for tags genuinely new on this edit (diff vs oldTags).
	if len(msg.Tags) > 0 {
		if err := k.validatePostTagsNoIncrement(ctx, msg.Tags); err != nil {
			return nil, err
		}
		oldSet := make(map[string]struct{}, len(oldTags))
		for _, t := range oldTags {
			oldSet[t] = struct{}{}
		}
		now := sdkCtx.BlockTime().Unix()
		for _, t := range msg.Tags {
			if _, had := oldSet[t]; !had {
				if err := k.repKeeper.IncrementTagUsage(ctx, t, now); err != nil {
					return nil, errorsmod.Wrap(err, "failed to update tag metadata")
				}
			}
		}
	}

	// Update post fields, preserving existing fields not in update message
	val.Title = msg.Title
	val.Body = msg.Body
	val.ContentType = msg.ContentType
	val.RepliesEnabled = msg.RepliesEnabled
	val.MinReplyTrustLevel = msg.MinReplyTrustLevel
	val.UpdatedAt = sdkCtx.BlockTime().Unix()
	val.Edited = true
	val.EditedAt = sdkCtx.BlockTime().Unix()
	val.Tags = msg.Tags
	if newBytes > val.FeeBytesHighWater {
		val.FeeBytesHighWater = newBytes
	}

	k.SetPost(ctx, val)

	// Diff old vs new tag set; write added entries and remove dropped ones.
	k.updateTagIndexEntries(ctx, val.Id, oldTags, msg.Tags)

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.post.updated",
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.Id)),
		sdk.NewAttribute("creator", msg.Creator),
	))

	return &types.MsgUpdatePostResponse{}, nil
}
