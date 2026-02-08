package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateTitle creates a new title.
// Authorized: Commons Council policy address or Commons Operations Committee members.
func (k msgServer) CreateTitle(ctx context.Context, msg *types.MsgCreateTitle) (*types.MsgCreateTitleResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authorization (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for gamification management")
	}

	// Validate title ID
	if msg.TitleId == "" {
		return nil, errorsmod.Wrap(types.ErrInvalidTitleId, "title ID cannot be empty")
	}

	// Check if title already exists
	_, err := k.Title.Get(ctx, msg.TitleId)
	if err == nil {
		return nil, errorsmod.Wrapf(types.ErrTitleExists, "title %s already exists", msg.TitleId)
	}

	// Validate name
	if msg.Name == "" {
		return nil, errorsmod.Wrap(types.ErrInvalidTitleId, "title name cannot be empty")
	}

	// Create the title
	title := types.Title{
		TitleId:              msg.TitleId,
		Name:                 msg.Name,
		Description:          msg.Description,
		Rarity:               types.Rarity(msg.Rarity),
		RequirementType:      types.RequirementType(msg.RequirementType),
		RequirementThreshold: msg.RequirementThreshold,
		RequirementSeason:    msg.RequirementSeason,
		Seasonal:             msg.Seasonal,
	}

	// Save the title
	if err := k.Title.Set(ctx, title.TitleId, title); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save title")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"title_created",
			sdk.NewAttribute("title_id", title.TitleId),
			sdk.NewAttribute("name", title.Name),
			sdk.NewAttribute("created_by", msg.Authority),
			sdk.NewAttribute("seasonal", boolToString(title.Seasonal)),
		),
	)

	return &types.MsgCreateTitleResponse{}, nil
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
