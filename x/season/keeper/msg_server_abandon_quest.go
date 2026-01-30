package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AbandonQuest abandons an in-progress quest.
// For repeatable quests, this applies a cooldown before it can be started again.
func (k msgServer) AbandonQuest(ctx context.Context, msg *types.MsgAbandonQuest) (*types.MsgAbandonQuestResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get quest progress
	key := fmt.Sprintf("%s:%s", msg.Creator, msg.QuestId)
	progress, err := k.MemberQuestProgress.Get(ctx, key)
	if err != nil {
		return nil, types.ErrQuestNotStarted
	}

	// Cannot abandon completed quest
	if progress.Completed {
		return nil, errorsmod.Wrap(types.ErrQuestAlreadyClaimed, "quest already completed")
	}

	// Get the quest
	quest, err := k.Quest.Get(ctx, msg.QuestId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrQuestNotFound, "quest %s not found", msg.QuestId)
	}

	// For repeatable quests with cooldown, mark as "completed" to start cooldown
	// but set completed_block to current block so cooldown starts now
	if quest.Repeatable && quest.CooldownEpochs > 0 {
		progress.Completed = true
		progress.CompletedBlock = sdkCtx.BlockHeight()
		progress.ObjectiveProgress = make([]uint64, len(quest.Objectives)) // Reset progress
		if err := k.MemberQuestProgress.Set(ctx, key, progress); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update quest progress")
		}
	} else {
		// Non-repeatable or no cooldown - just delete the progress
		if err := k.MemberQuestProgress.Remove(ctx, key); err != nil {
			return nil, errorsmod.Wrap(err, "failed to remove quest progress")
		}
	}

	// Update profile last active
	profile, _ := k.MemberProfile.Get(ctx, msg.Creator)
	profile.LastActiveEpoch = k.GetCurrentEpoch(ctx)
	_ = k.MemberProfile.Set(ctx, msg.Creator, profile)

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"quest_abandoned",
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("quest_id", msg.QuestId),
		),
	)

	return &types.MsgAbandonQuestResponse{}, nil
}
