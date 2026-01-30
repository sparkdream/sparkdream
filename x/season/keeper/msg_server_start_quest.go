package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// StartQuest starts tracking progress for a quest.
func (k msgServer) StartQuest(ctx context.Context, msg *types.MsgStartQuest) (*types.MsgStartQuestResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check maintenance mode
	if k.IsInMaintenanceMode(ctx) {
		return nil, types.ErrMaintenanceMode
	}

	// Get member profile
	profile, err := k.MemberProfile.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "member profile not found")
	}

	// Get the quest
	quest, err := k.Quest.Get(ctx, msg.QuestId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrQuestNotFound, "quest %s not found", msg.QuestId)
	}

	// Check quest is active
	if !quest.Active {
		return nil, types.ErrQuestNotActive
	}

	// Check block window
	currentBlock := sdkCtx.BlockHeight()
	if quest.StartBlock > 0 && currentBlock < quest.StartBlock {
		return nil, errorsmod.Wrap(types.ErrQuestNotActive, "quest has not started yet")
	}
	if quest.EndBlock > 0 && currentBlock > quest.EndBlock {
		return nil, errorsmod.Wrap(types.ErrQuestNotActive, "quest has ended")
	}

	// Check season
	if quest.Season > 0 {
		season, err := k.GetCurrentSeason(ctx)
		if err == nil && uint64(season.Number) != quest.Season {
			return nil, types.ErrQuestSeasonMismatch
		}
	}

	// Check level requirement
	if quest.MinLevel > 0 && profile.SeasonLevel < quest.MinLevel {
		return nil, errorsmod.Wrapf(types.ErrQuestLevelTooLow, "requires level %d", quest.MinLevel)
	}

	// Check achievement requirement
	if quest.RequiredAchievement != "" {
		hasAchievement := false
		for _, ach := range profile.Achievements {
			if ach == quest.RequiredAchievement {
				hasAchievement = true
				break
			}
		}
		if !hasAchievement {
			return nil, errorsmod.Wrapf(types.ErrQuestPrerequisiteNotMet, "requires achievement %s", quest.RequiredAchievement)
		}
	}

	// Check prerequisite quest
	if quest.PrerequisiteQuest != "" {
		if !k.HasQuestPrerequisite(ctx, msg.Creator, quest.PrerequisiteQuest) {
			return nil, errorsmod.Wrapf(types.ErrQuestPrerequisiteNotMet, "requires quest %s", quest.PrerequisiteQuest)
		}
	}

	// Check if already started (or completed but on cooldown)
	key := fmt.Sprintf("%s:%s", msg.Creator, msg.QuestId)
	progress, err := k.MemberQuestProgress.Get(ctx, key)
	currentEpoch := k.GetCurrentEpoch(ctx)
	if err == nil {
		if !progress.Completed {
			return nil, types.ErrQuestAlreadyStarted
		}
		// Quest was completed before - check if repeatable
		if !quest.Repeatable {
			return nil, types.ErrQuestAlreadyClaimed
		}
		// Check cooldown
		completedEpoch := k.BlockToEpoch(ctx, progress.CompletedBlock)
		epochsSinceComplete := currentEpoch - completedEpoch
		if quest.CooldownEpochs > 0 && uint64(epochsSinceComplete) < quest.CooldownEpochs {
			return nil, errorsmod.Wrapf(types.ErrQuestOnCooldown,
				"must wait %d more epochs", quest.CooldownEpochs-uint64(epochsSinceComplete))
		}
	}

	// Check max active quests
	params, _ := k.Params.Get(ctx)
	activeCount := k.GetMemberActiveQuestCount(ctx, msg.Creator)
	if activeCount >= params.MaxActiveQuestsPerMember {
		return nil, types.ErrMaxActiveQuests
	}

	// Create progress record
	objectiveProgress := make([]uint64, len(quest.Objectives))
	newProgress := types.MemberQuestProgress{
		MemberQuest:       key, // Composite key: member:quest_id
		ObjectiveProgress: objectiveProgress,
		Completed:         false,
		CompletedBlock:    0,
		LastAttemptBlock:  currentBlock,
	}

	if err := k.MemberQuestProgress.Set(ctx, key, newProgress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save quest progress")
	}

	// Update profile last active
	profile.LastActiveEpoch = currentEpoch
	_ = k.MemberProfile.Set(ctx, msg.Creator, profile)

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"quest_started",
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("quest_id", msg.QuestId),
		),
	)

	return &types.MsgStartQuestResponse{}, nil
}
