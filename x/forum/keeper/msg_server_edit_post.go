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
	if !params.CostPerByteExempt && params.CostPerByteAmount.IsPositive() {
		oldBytes := int64(len(post.Content))
		newBytes := int64(len(msg.NewContent))
		if newBytes > oldBytes {
			deltaFee := sdk.NewCoin(k.BondDenom(ctx),
				params.CostPerByteAmount.MulRaw(newBytes-oldBytes))
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

	// Validate the full new tag set without touching usage metadata, then bump
	// usage for tags newly added on this edit and drop usage for tags removed.
	// Both directions must run regardless of whether msg.Tags is empty, since
	// clearing all tags on an edit needs to decrement usage on every oldTag.
	// Without the diff, repeated edits within the edit window inflate
	// UsageCount on tags the post already carried.
	if len(msg.Tags) > 0 {
		if err := k.validatePostTagsNoIncrement(ctx, msg.Tags); err != nil {
			return nil, err
		}
	}
	oldSet := make(map[string]struct{}, len(post.Tags))
	for _, t := range post.Tags {
		oldSet[t] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(msg.Tags))
	for _, t := range msg.Tags {
		newSet[t] = struct{}{}
	}
	for _, t := range msg.Tags {
		if _, had := oldSet[t]; !had {
			if err := k.repKeeper.IncrementTagUsage(ctx, t, now); err != nil {
				return nil, errorsmod.Wrap(err, "failed to update tag metadata")
			}
		}
	}
	for _, t := range post.Tags {
		if _, still := newSet[t]; still {
			continue
		}
		if err := k.repKeeper.DecrementTagUsage(ctx, t); err != nil {
			// Tag may have been GC'd between create and edit; non-fatal, log
			// and continue so the user can still finish their edit.
			sdkCtx.Logger().Error("failed to decrement dropped tag usage",
				"post_id", msg.PostId, "tag", t, "error", err)
		}
	}

	// Charge edit fee if past grace period; split 50/50 burn / sentinel reward pool
	if editAge > params.EditGracePeriod && params.EditFeeAmount.IsPositive() {
		creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
		editFeeCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), params.EditFeeAmount))
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
