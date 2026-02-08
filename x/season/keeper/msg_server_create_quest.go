package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateQuest creates a new quest.
// Authorized: Commons Council policy address or Commons Operations Committee members.
func (k msgServer) CreateQuest(ctx context.Context, msg *types.MsgCreateQuest) (*types.MsgCreateQuestResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for gamification management")
	}

	// Validate quest ID is not empty
	if msg.QuestId == "" {
		return nil, errorsmod.Wrap(types.ErrInvalidQuestObjective, "quest ID cannot be empty")
	}

	// Validate quest ID doesn't already exist
	_, err := k.Quest.Get(ctx, msg.QuestId)
	if err == nil {
		return nil, errorsmod.Wrapf(types.ErrQuestIdAlreadyExists, "quest %s already exists", msg.QuestId)
	}

	// Get params for validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate XP reward
	if msg.XpReward > params.MaxQuestXpReward {
		return nil, errorsmod.Wrapf(types.ErrQuestXpRewardTooHigh,
			"XP reward %d exceeds max %d", msg.XpReward, params.MaxQuestXpReward)
	}

	// Build the Quest from individual message fields
	quest := types.Quest{
		QuestId:             msg.QuestId,
		Name:                msg.Name,
		Description:         msg.Description,
		XpReward:            msg.XpReward,
		Repeatable:          msg.Repeatable,
		CooldownEpochs:      msg.CooldownEpochs,
		Season:              msg.Season,
		StartBlock:          msg.StartBlock,
		EndBlock:            msg.EndBlock,
		MinLevel:            msg.MinLevel,
		RequiredAchievement: msg.RequiredAchievement,
		PrerequisiteQuest:   msg.PrerequisiteQuest,
		QuestChain:          msg.QuestChain,
		Active:              true,
		Objectives:          []*types.QuestObjective{}, // Start with empty objectives
	}

	// Save the quest
	if err := k.Quest.Set(ctx, quest.QuestId, quest); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save quest")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"quest_created",
			sdk.NewAttribute("quest_id", quest.QuestId),
			sdk.NewAttribute("name", quest.Name),
			sdk.NewAttribute("created_by", msg.Authority),
			sdk.NewAttribute("xp_reward", fmt.Sprintf("%d", quest.XpReward)),
		),
	)

	return &types.MsgCreateQuestResponse{}, nil
}
