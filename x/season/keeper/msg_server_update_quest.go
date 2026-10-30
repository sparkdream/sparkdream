package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UpdateQuest updates an existing quest.
// Authorized: Commons Council policy address or Commons Operations Committee members.
func (k msgServer) UpdateQuest(ctx context.Context, msg *types.MsgUpdateQuest) (*types.MsgUpdateQuestResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authorization (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for gamification management")
	}

	// Get existing quest
	quest, err := k.Quest.Get(ctx, msg.QuestId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrQuestNotFound, "quest %s not found", msg.QuestId)
	}

	// Get params for validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate XP reward if changed
	if msg.XpReward > types.HardMaxQuestXpReward {
		return nil, errorsmod.Wrapf(types.ErrQuestXpRewardTooHigh,
			"XP reward %d exceeds the hard ceiling %d", msg.XpReward, types.HardMaxQuestXpReward)
	}
	if msg.XpReward > params.MaxQuestXpReward {
		return nil, errorsmod.Wrapf(types.ErrQuestXpRewardTooHigh,
			"XP reward %d exceeds max %d", msg.XpReward, params.MaxQuestXpReward)
	}

	if err := k.ValidateQuestPrerequisites(ctx, msg.QuestId, msg.PrerequisiteQuest, msg.RequiredAchievement); err != nil {
		return nil, err
	}

	// Update fields
	if msg.Name != "" {
		quest.Name = msg.Name
	}
	if msg.Description != "" {
		quest.Description = msg.Description
	}
	// These fields can be 0/false, so always update
	quest.XpReward = msg.XpReward
	quest.Repeatable = msg.Repeatable
	quest.CooldownEpochs = msg.CooldownEpochs
	quest.Season = msg.Season
	quest.StartBlock = msg.StartBlock
	quest.EndBlock = msg.EndBlock
	quest.MinLevel = msg.MinLevel
	quest.RequiredAchievement = msg.RequiredAchievement
	quest.PrerequisiteQuest = msg.PrerequisiteQuest
	quest.QuestChain = msg.QuestChain
	quest.Active = msg.Active
	// Objectives are replaced only when supplied; an update that omits them
	// keeps the existing list so in-flight progress stays aligned.
	if len(msg.Objectives) > 0 {
		if err := k.ValidateQuestObjectives(ctx, msg.Objectives); err != nil {
			return nil, err
		}
		// SEA-32: a changed objective list gets a new version; progress
		// recorded against the old one restarts from zero instead of being
		// re-read against different objective types or targets.
		if !sameObjectives(quest.Objectives, msg.Objectives) {
			quest.ObjectivesVersion++
		}
		quest.Objectives = msg.Objectives
	}

	// Save the updated quest
	if err := k.Quest.Set(ctx, msg.QuestId, quest); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update quest")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"quest_updated",
			sdk.NewAttribute("quest_id", msg.QuestId),
			sdk.NewAttribute("name", quest.Name),
			sdk.NewAttribute("updated_by", msg.Authority),
			sdk.NewAttribute("xp_reward", fmt.Sprintf("%d", quest.XpReward)),
			sdk.NewAttribute("active", fmt.Sprintf("%t", quest.Active)),
		),
	)

	return &types.MsgUpdateQuestResponse{}, nil
}
