package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ClaimQuestReward claims the XP reward for a completed quest.
func (k msgServer) ClaimQuestReward(ctx context.Context, msg *types.MsgClaimQuestReward) (*types.MsgClaimQuestRewardResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check maintenance mode
	if k.IsInMaintenanceMode(ctx) {
		return nil, types.ErrMaintenanceMode
	}

	// Get quest progress
	key := fmt.Sprintf("%s:%s", msg.Creator, msg.QuestId)
	progress, err := k.MemberQuestProgress.Get(ctx, key)
	if err != nil {
		return nil, types.ErrQuestNotStarted
	}

	// Check not already claimed
	if progress.Completed {
		return nil, types.ErrQuestAlreadyClaimed
	}

	// Get the quest
	quest, err := k.Quest.Get(ctx, msg.QuestId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrQuestNotFound, "quest %s not found", msg.QuestId)
	}

	// Check all objectives are complete
	for i, objective := range quest.Objectives {
		if i >= len(progress.ObjectiveProgress) {
			return nil, types.ErrQuestNotComplete
		}
		if progress.ObjectiveProgress[i] < objective.Target {
			return nil, types.ErrQuestNotComplete
		}
	}

	// Mark as completed
	progress.Completed = true
	progress.CompletedBlock = sdkCtx.BlockHeight()
	if err := k.MemberQuestProgress.Set(ctx, key, progress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update quest progress")
	}

	// Grant XP reward
	profile, err := k.MemberProfile.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "member profile not found")
	}

	xpEarned := quest.XpReward
	profile.SeasonXp += xpEarned
	profile.LifetimeXp += xpEarned
	profile.SeasonLevel = k.CalculateLevel(ctx, profile.SeasonXp)
	profile.LastActiveEpoch = k.GetCurrentEpoch(ctx)

	if err := k.MemberProfile.Set(ctx, msg.Creator, profile); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update profile")
	}

	// Emit events
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"quest_completed",
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("quest_id", msg.QuestId),
			sdk.NewAttribute("xp_earned", fmt.Sprintf("%d", xpEarned)),
		),
	)

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"xp_granted",
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("amount", fmt.Sprintf("%d", xpEarned)),
			sdk.NewAttribute("source", "quest"),
			sdk.NewAttribute("reference", msg.QuestId),
		),
	)

	return &types.MsgClaimQuestRewardResponse{}, nil
}
