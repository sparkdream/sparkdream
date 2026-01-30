package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DeactivateQuest deactivates a quest so it can no longer be started.
// Members with in-progress quests can still complete them.
// Only Operations Committee can deactivate quests.
func (k msgServer) DeactivateQuest(ctx context.Context, msg *types.MsgDeactivateQuest) (*types.MsgDeactivateQuestResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority (Operations Committee or governance)
	if !k.IsOperationsCommittee(ctx, msg.Authority) {
		return nil, types.ErrNotOperationsCommittee
	}

	// Get the quest
	quest, err := k.Quest.Get(ctx, msg.QuestId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrQuestNotFound, "quest %s not found", msg.QuestId)
	}

	// Check if already inactive
	if !quest.Active {
		return nil, errorsmod.Wrap(types.ErrQuestNotActive, "quest is already inactive")
	}

	// Deactivate the quest
	quest.Active = false

	if err := k.Quest.Set(ctx, msg.QuestId, quest); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update quest")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"quest_deactivated",
			sdk.NewAttribute("quest_id", msg.QuestId),
			sdk.NewAttribute("deactivated_by", msg.Authority),
		),
	)

	return &types.MsgDeactivateQuestResponse{}, nil
}
