package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// DeleteCategory removes a shared content category. Same authorization as
// CreateCategory: governance, the Commons Council policy, or the Commons
// Operations Committee (or a COC member). Refuses to delete a category that
// still has forum posts attached — admins must move/archive those first via
// MsgMoveThread.
func (k msgServer) DeleteCategory(ctx context.Context, msg *types.MsgDeleteCategory) (*types.MsgDeleteCategoryResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	if !k.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "only governance, the Commons Council, or the Operations Committee can delete categories")
	}

	if _, err := k.Category.Get(ctx, msg.CategoryId); err != nil {
		if errorsmod.IsOf(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "category %d not found", msg.CategoryId)
		}
		return nil, errorsmod.Wrap(err, "failed to load category")
	}

	// Cross-module check: refuse if any forum post still references this
	// category. Late-wired keeper — guard against nil in case wiring is
	// skipped in a custom app.
	if k.late.forumKeeper == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "forum keeper not wired; cannot verify category is empty")
	}
	hasPosts, err := k.late.forumKeeper.HasPostInCategory(ctx, msg.CategoryId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check forum posts in category")
	}
	if hasPosts {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "category %d still has forum posts; move or archive them before deleting", msg.CategoryId)
	}

	if err := k.Category.Remove(ctx, msg.CategoryId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove category")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"category_deleted",
			sdk.NewAttribute("category_id", fmt.Sprintf("%d", msg.CategoryId)),
			sdk.NewAttribute("creator", msg.Creator),
		),
	)

	return &types.MsgDeleteCategoryResponse{}, nil
}
