package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DeleteAchievement deletes an achievement.
// Authorized: Commons Council policy address or Commons Operations Committee members.
// Note: Deleting an achievement does not remove it from members who have already earned it.
func (k msgServer) DeleteAchievement(ctx context.Context, msg *types.MsgDeleteAchievement) (*types.MsgDeleteAchievementResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authorization (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for gamification management")
	}

	// Check if achievement exists
	_, err := k.Achievement.Get(ctx, msg.AchievementId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrAchievementNotFound, "achievement %s not found", msg.AchievementId)
	}

	// Delete the achievement
	if err := k.Achievement.Remove(ctx, msg.AchievementId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to delete achievement")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"achievement_deleted",
			sdk.NewAttribute("achievement_id", msg.AchievementId),
			sdk.NewAttribute("deleted_by", msg.Authority),
		),
	)

	return &types.MsgDeleteAchievementResponse{}, nil
}
