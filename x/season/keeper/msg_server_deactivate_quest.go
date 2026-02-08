package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DeactivateQuest deactivates a quest so it can no longer be started.
// Members with in-progress quests can still complete them.
// Authorized: Commons Council policy address or Commons Operations Committee members.
func (k msgServer) DeactivateQuest(ctx context.Context, msg *types.MsgDeactivateQuest) (*types.MsgDeactivateQuestResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority (Commons Council or Operations Committee)
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for gamification management")
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
