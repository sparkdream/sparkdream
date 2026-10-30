package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/types"
)

// questOpenNow reports whether a quest can be started or claimed at the
// current block: active, inside its block window and, for a seasonal quest,
// in its season (SEA-26).
func (k Keeper) questOpenNow(ctx context.Context, quest types.Quest) error {
	if !quest.Active {
		return types.ErrQuestNotActive
	}
	currentBlock := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if quest.StartBlock > 0 && currentBlock < quest.StartBlock {
		return errorsmod.Wrap(types.ErrQuestNotActive, "quest has not started yet")
	}
	if quest.EndBlock > 0 && currentBlock > quest.EndBlock {
		return errorsmod.Wrap(types.ErrQuestNotActive, "quest has ended")
	}
	if quest.Season > 0 {
		season, err := k.getSeason(ctx)
		if err == nil && season.Number != quest.Season {
			return types.ErrQuestSeasonMismatch
		}
	}
	return nil
}

// canStartQuest is the single predicate behind MsgStartQuest and the
// AvailableQuests query (SEA-35): quest open, level, required achievement,
// prerequisite, completed/cooldown state and the active-quest cap.
func (k Keeper) canStartQuest(ctx context.Context, member string, profile types.MemberProfile, quest types.Quest, maxActive uint32) error {
	if err := k.questOpenNow(ctx, quest); err != nil {
		return err
	}
	if quest.MinLevel > 0 && profile.SeasonLevel < quest.MinLevel {
		return errorsmod.Wrapf(types.ErrQuestLevelTooLow, "requires level %d", quest.MinLevel)
	}
	if quest.RequiredAchievement != "" {
		has := false
		for _, ach := range profile.Achievements {
			if ach == quest.RequiredAchievement {
				has = true
				break
			}
		}
		if !has {
			return errorsmod.Wrapf(types.ErrQuestPrerequisiteNotMet, "requires achievement %s", quest.RequiredAchievement)
		}
	}
	if quest.PrerequisiteQuest != "" && !k.HasQuestPrerequisite(ctx, member, quest.PrerequisiteQuest) {
		return errorsmod.Wrapf(types.ErrQuestPrerequisiteNotMet, "requires quest %s", quest.PrerequisiteQuest)
	}
	progress, err := k.MemberQuestProgress.Get(ctx, questProgressKey(member, quest.QuestId))
	if err == nil {
		if !progress.Completed {
			return types.ErrQuestAlreadyStarted
		}
		if !quest.Repeatable {
			return types.ErrQuestAlreadyClaimed
		}
		epochsSinceComplete := k.GetCurrentEpoch(ctx) - k.BlockToEpoch(ctx, progress.CompletedBlock)
		if quest.CooldownEpochs > 0 && commontypes.Saturate[uint64](epochsSinceComplete) < quest.CooldownEpochs {
			return errorsmod.Wrapf(types.ErrQuestOnCooldown,
				"must wait %d more epochs", quest.CooldownEpochs-commontypes.Saturate[uint64](epochsSinceComplete))
		}
	}
	if k.GetMemberActiveQuestCount(ctx, member) >= maxActive {
		return types.ErrMaxActiveQuests
	}
	return nil
}
