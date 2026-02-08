package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UpdateAchievement updates an existing achievement.
// Authorized: Commons Council policy address or Commons Operations Committee members.
func (k msgServer) UpdateAchievement(ctx context.Context, msg *types.MsgUpdateAchievement) (*types.MsgUpdateAchievementResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authorization (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for gamification management")
	}

	// Get existing achievement
	achievement, err := k.Achievement.Get(ctx, msg.AchievementId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrAchievementNotFound, "achievement %s not found", msg.AchievementId)
	}

	// Update fields
	if msg.Name != "" {
		achievement.Name = msg.Name
	}
	if msg.Description != "" {
		achievement.Description = msg.Description
	}
	if msg.Rarity != 0 {
		achievement.Rarity = types.Rarity(msg.Rarity)
	}
	// XP reward can be set to 0, so always update
	achievement.XpReward = msg.XpReward
	if msg.RequirementType != 0 {
		achievement.RequirementType = types.RequirementType(msg.RequirementType)
	}
	// Threshold can be 0, so always update
	achievement.RequirementThreshold = msg.RequirementThreshold

	// Save the updated achievement
	if err := k.Achievement.Set(ctx, achievement.AchievementId, achievement); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update achievement")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"achievement_updated",
			sdk.NewAttribute("achievement_id", achievement.AchievementId),
			sdk.NewAttribute("name", achievement.Name),
			sdk.NewAttribute("updated_by", msg.Authority),
			sdk.NewAttribute("xp_reward", fmt.Sprintf("%d", achievement.XpReward)),
		),
	)

	return &types.MsgUpdateAchievementResponse{}, nil
}
