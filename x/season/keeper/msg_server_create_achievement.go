package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateAchievement creates a new achievement.
// Authorized: Commons Council policy address or Commons Operations Committee members.
func (k msgServer) CreateAchievement(ctx context.Context, msg *types.MsgCreateAchievement) (*types.MsgCreateAchievementResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authorization (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for gamification management")
	}

	// Validate achievement ID
	if msg.AchievementId == "" {
		return nil, errorsmod.Wrap(types.ErrInvalidAchievementId, "achievement ID cannot be empty")
	}

	// Check if achievement already exists
	_, err := k.Achievement.Get(ctx, msg.AchievementId)
	if err == nil {
		return nil, errorsmod.Wrapf(types.ErrAchievementExists, "achievement %s already exists", msg.AchievementId)
	}

	// Validate name
	if msg.Name == "" {
		return nil, errorsmod.Wrap(types.ErrInvalidAchievementId, "achievement name cannot be empty")
	}

	// Create the achievement
	achievement := types.Achievement{
		AchievementId:        msg.AchievementId,
		Name:                 msg.Name,
		Description:          msg.Description,
		Rarity:               types.Rarity(msg.Rarity),
		XpReward:             msg.XpReward,
		RequirementType:      types.RequirementType(msg.RequirementType),
		RequirementThreshold: msg.RequirementThreshold,
	}

	// Save the achievement
	if err := k.Achievement.Set(ctx, achievement.AchievementId, achievement); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save achievement")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"achievement_created",
			sdk.NewAttribute("achievement_id", achievement.AchievementId),
			sdk.NewAttribute("name", achievement.Name),
			sdk.NewAttribute("created_by", msg.Authority),
			sdk.NewAttribute("xp_reward", fmt.Sprintf("%d", achievement.XpReward)),
		),
	)

	return &types.MsgCreateAchievementResponse{}, nil
}
