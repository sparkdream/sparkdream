package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// CreateCategory registers a new shared content category. Only governance,
// the Commons Council policy, or the Commons Operations Committee may create
// categories.
func (k msgServer) CreateCategory(ctx context.Context, msg *types.MsgCreateCategory) (*types.MsgCreateCategoryResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	if !k.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "only governance, the Commons Council, or the Operations Committee can create categories")
	}

	if msg.Title == "" {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "title cannot be empty")
	}
	if len(msg.Title) > 256 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "title exceeds 256 characters")
	}
	if len(msg.Description) > 2048 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "description exceeds 2048 characters")
	}

	categoryID, err := k.CategorySeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate category ID")
	}

	category := types.Category{
		CategoryId:       categoryID,
		Title:            msg.Title,
		Description:      msg.Description,
		MembersOnlyWrite: msg.MembersOnlyWrite,
		AdminOnlyWrite:   msg.AdminOnlyWrite,
	}
	if err := k.Category.Set(ctx, categoryID, category); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store category")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"category_created",
			sdk.NewAttribute("category_id", fmt.Sprintf("%d", categoryID)),
			sdk.NewAttribute("title", msg.Title),
			sdk.NewAttribute("creator", msg.Creator),
		),
	)

	return &types.MsgCreateCategoryResponse{CategoryId: categoryID}, nil
}
