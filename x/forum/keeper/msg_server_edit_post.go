package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) EditPost(ctx context.Context, msg *types.MsgEditPost) (*types.MsgEditPostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check editing_enabled param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if !params.EditingEnabled {
		return nil, types.ErrEditingDisabled
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Verify author ownership
	if post.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotPostAuthor, "only the author can edit their post")
	}

	// Check post status - cannot edit hidden, deleted, or archived posts
	switch post.Status {
	case types.PostStatus_POST_STATUS_HIDDEN:
		return nil, types.ErrCannotEditHiddenPost
	case types.PostStatus_POST_STATUS_DELETED:
		return nil, types.ErrCannotEditDeletedPost
	case types.PostStatus_POST_STATUS_ARCHIVED:
		return nil, types.ErrPostArchived
	}

	// Check edit window
	editAge := now - post.CreatedAt
	if editAge > types.DefaultEditMaxWindow {
		return nil, errorsmod.Wrapf(types.ErrEditWindowExpired, "edit window is %d seconds", types.DefaultEditMaxWindow)
	}

	// Validate new content
	if msg.NewContent == "" {
		return nil, types.ErrEmptyContent
	}
	if uint64(len(msg.NewContent)) > types.DefaultMaxContentSize {
		return nil, errorsmod.Wrapf(types.ErrContentTooLarge, "max size is %d bytes", types.DefaultMaxContentSize)
	}

	// Charge cost_per_byte storage delta fee (applies to all posters, burned)
	if !params.CostPerByteExempt && params.CostPerByte.IsPositive() {
		oldBytes := int64(len(post.Content))
		newBytes := int64(len(msg.NewContent))
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

	// Validate tags (if provided, replace all tags; empty list clears tags)
	if len(msg.Tags) > 0 {
		if err := k.validatePostTags(ctx, msg.Tags, now); err != nil {
			return nil, err
		}
	}

	// Charge edit fee if past grace period; split 50/50 burn / sentinel reward pool
	if editAge > params.EditGracePeriod && params.EditFee.IsPositive() {
		creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
		editFeeCoins := sdk.NewCoins(params.EditFee)
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, editFeeCoins); err != nil {
			return nil, errorsmod.Wrap(err, "failed to charge edit fee")
		}
		if err := k.distributeSpamTax(ctx, editFeeCoins, "edit"); err != nil {
			return nil, err
		}
	}

	// Update post
	post.Content = msg.NewContent
	post.ContentType = msg.ContentType
	post.Tags = msg.Tags
	post.Edited = true
	post.EditedAt = now

	// Store updated post
	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Emit event
	inGracePeriod := editAge <= types.DefaultEditGracePeriod
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_edited",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("author", msg.Creator),
			sdk.NewAttribute("in_grace_period", fmt.Sprintf("%t", inGracePeriod)),
		),
	)

	return &types.MsgEditPostResponse{}, nil
}
