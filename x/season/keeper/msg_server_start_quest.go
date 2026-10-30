package keeper

import (
	"context"
	"cosmossdk.io/collections"
	"errors"

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

	// Quest window, season, level, achievement, prerequisite, repeat and
	// active-cap checks (shared with the AvailableQuests query, SEA-35).
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	if err := k.canStartQuest(ctx, msg.Creator, profile, quest, params.MaxActiveQuestsPerMember); err != nil {
		return nil, err
	}
	key := questProgressKey(msg.Creator, msg.QuestId)
	currentBlock := sdkCtx.BlockHeight()
	currentEpoch := k.GetCurrentEpoch(ctx)

	// Create progress record
	objectiveProgress := make([]uint64, len(quest.Objectives))
	newProgress := types.MemberQuestProgress{
		MemberQuest:       key, // Composite key: member:quest_id
		ObjectiveProgress: objectiveProgress,
		Completed:         false,
		CompletedBlock:    0,
		LastAttemptBlock:  currentBlock,
		ObjectivesVersion: quest.ObjectivesVersion,
	}

	if err := k.MemberQuestProgress.Set(ctx, key, newProgress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save quest progress")
	}

	// Update profile last active
	profile.LastActiveEpoch = currentEpoch
	if err := k.MemberProfile.Set(ctx, msg.Creator, profile); err != nil {
		return nil, err
	}

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
