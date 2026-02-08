package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DeleteTitle deletes a title.
// Authorized: Commons Council policy address or Commons Operations Committee members.
// Note: Deleting a title does not remove it from members who have already unlocked it.
func (k msgServer) DeleteTitle(ctx context.Context, msg *types.MsgDeleteTitle) (*types.MsgDeleteTitleResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authorization (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for gamification management")
	}

	// Check if title exists
	_, err := k.Title.Get(ctx, msg.TitleId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrTitleNotFound, "title %s not found", msg.TitleId)
	}

	// Delete the title
	if err := k.Title.Remove(ctx, msg.TitleId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to delete title")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"title_deleted",
			sdk.NewAttribute("title_id", msg.TitleId),
			sdk.NewAttribute("deleted_by", msg.Authority),
		),
	)

	return &types.MsgDeleteTitleResponse{}, nil
}
