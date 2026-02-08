package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UpdateTitle updates an existing title.
// Authorized: Commons Council policy address or Commons Operations Committee members.
func (k msgServer) UpdateTitle(ctx context.Context, msg *types.MsgUpdateTitle) (*types.MsgUpdateTitleResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authorization (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for gamification management")
	}

	// Get existing title
	title, err := k.Title.Get(ctx, msg.TitleId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrTitleNotFound, "title %s not found", msg.TitleId)
	}

	// Update fields
	if msg.Name != "" {
		title.Name = msg.Name
	}
	if msg.Description != "" {
		title.Description = msg.Description
	}
	if msg.Rarity != 0 {
		title.Rarity = types.Rarity(msg.Rarity)
	}
	if msg.RequirementType != 0 {
		title.RequirementType = types.RequirementType(msg.RequirementType)
	}
	// These can be 0, so always update
	title.RequirementThreshold = msg.RequirementThreshold
	title.RequirementSeason = msg.RequirementSeason
	title.Seasonal = msg.Seasonal

	// Save the updated title
	if err := k.Title.Set(ctx, title.TitleId, title); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update title")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"title_updated",
			sdk.NewAttribute("title_id", title.TitleId),
			sdk.NewAttribute("name", title.Name),
			sdk.NewAttribute("updated_by", msg.Authority),
		),
	)

	return &types.MsgUpdateTitleResponse{}, nil
}
