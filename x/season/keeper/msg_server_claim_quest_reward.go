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

	// Votes cast earlier in this block reach quest objectives at
	// EndBlock; apply this member's now (SEA-103).
	if err := k.applyPendingVoteQuestProgress(ctx, msg.Creator); err != nil {
		return nil, err
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

	// SEA-26: a quest can only be claimed while it is open: active, inside
	// its block window and in its season. Progress from an ended seasonal
	// quest is not claimable in a later season.
	if err := k.questOpenNow(ctx, quest); err != nil {
		return nil, err
	}

	// Check all objectives are complete
	if !questObjectivesComplete(quest, progress) {
		return nil, types.ErrQuestNotComplete
	}

	// Mark as completed
	progress.Completed = true
	progress.CompletedBlock = sdkCtx.BlockHeight()
	if err := k.MemberQuestProgress.Set(ctx, key, progress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update quest progress")
	}
	if err := k.recordQuestCompletedStats(ctx); err != nil {
		return nil, err
	}

	// Grant the XP reward through the engine so the per-epoch cap and the
	// quest bucket of the EpochXpTracker apply. A claim is still valid when
	// the cap is already spent: the quest completes, the XP is forfeit.
	if !k.HasMemberProfile(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "member profile not found")
	}
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	xpEarned := quest.XpReward
	if err := k.GrantXP(ctx, creatorAddr, xpEarned, types.XPSourceQuest, msg.QuestId); err != nil {
		if !errorsmod.IsOf(err, types.ErrXPCapReached) {
			return nil, errorsmod.Wrap(err, "failed to grant quest xp")
		}
		xpEarned = 0
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"quest_completed",
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("quest_id", msg.QuestId),
			sdk.NewAttribute("xp_earned", fmt.Sprintf("%d", xpEarned)),
		),
	)

	return &types.MsgClaimQuestRewardResponse{}, nil
}
