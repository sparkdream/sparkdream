package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CreateCategory(ctx context.Context, msg *types.MsgCreateCategory) (*types.MsgCreateCategoryResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Only governance, council, or operations committee can create categories
	if !k.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only governance, council, or operations committee can create categories")
	}

	// Validate title and description
	if msg.Title == "" {
		return nil, errorsmod.Wrap(types.ErrInvalidContent, "title cannot be empty")
	}
	if len(msg.Title) > 256 {
		return nil, errorsmod.Wrap(types.ErrContentTooLarge, "title exceeds 256 characters")
	}
	if len(msg.Description) > 2048 {
		return nil, errorsmod.Wrap(types.ErrContentTooLarge, "description exceeds 2048 characters")
	}

	// Generate category ID
	categoryID, err := k.CategorySeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate category ID")
	}

	// Create category
	category := types.Category{
		CategoryId:       categoryID,
		Title:            msg.Title,
		Description:      msg.Description,
		MembersOnlyWrite: msg.MembersOnlyWrite,
		AdminOnlyWrite:   msg.AdminOnlyWrite,
	}

	// Store category
	if err := k.Category.Set(ctx, categoryID, category); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store category")
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"category_created",
			sdk.NewAttribute("category_id", fmt.Sprintf("%d", categoryID)),
			sdk.NewAttribute("title", msg.Title),
			sdk.NewAttribute("creator", msg.Creator),
		),
	)

	return &types.MsgCreateCategoryResponse{}, nil
}
