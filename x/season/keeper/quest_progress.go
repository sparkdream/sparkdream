package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"
)

// ValidateQuestObjectives checks the objectives attached to a quest: a known
// type, a positive target and at most max_quest_objectives of them.
func (k Keeper) ValidateQuestObjectives(ctx context.Context, objectives []*types.QuestObjective) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if params.MaxQuestObjectives > 0 && uint32(len(objectives)) > params.MaxQuestObjectives {
		return errorsmod.Wrapf(types.ErrQuestTooManyObjectives, "%d objectives, max %d", len(objectives), params.MaxQuestObjectives)
	}
	for i, o := range objectives {
		if o == nil {
			return errorsmod.Wrapf(types.ErrInvalidQuestObjective, "objective %d is empty", i)
		}
		if _, ok := types.QuestObjectiveType_name[int32(o.Type)]; !ok || o.Type == types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_UNSPECIFIED {
			return errorsmod.Wrapf(types.ErrInvalidQuestObjective, "objective %d has no type", i)
		}
		if o.Target == 0 {
			return errorsmod.Wrapf(types.ErrInvalidQuestObjective, "objective %d target must be positive", i)
		}
	}
	return nil
}

// memberInProgressQuests returns the member's started, unclaimed quest
// progress records.
func (k Keeper) memberInProgressQuests(ctx context.Context, memberStr string) ([]types.MemberQuestProgress, error) {
	// Keys are "member:quest", so a prefix range reads only this member's
	// records instead of every member's (this runs inside vote and forum
	// transactions, where a full scan would be paid in gas by the voter).
	var out []types.MemberQuestProgress
	rng := new(collections.Range[string]).Prefix(memberStr + ":")
	err := k.MemberQuestProgress.Walk(ctx, rng, func(_ string, p types.MemberQuestProgress) (bool, error) {
		if !p.Completed {
			out = append(out, p)
		}
		return false, nil
	})
	return out, err
}

// OnQuestObjectiveProgress advances every matching objective on the member's
// in-progress quests by `increment`, clamped at the target, and emits
// quest_objectives_complete when a quest becomes claimable. Called by the
// season hooks when the underlying action happens (votes, helpful marks,
// initiative completions, invitee milestones).
func (k Keeper) OnQuestObjectiveProgress(ctx context.Context, member sdk.AccAddress, objectiveType types.QuestObjectiveType, increment uint64) error {
	if increment == 0 {
		return nil
	}
	memberStr, err := k.addressCodec.BytesToString(member)
	if err != nil {
		return err
	}
	inProgress, err := k.memberInProgressQuests(ctx, memberStr)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, progress := range inProgress {
		questID := extractQuestIdFromKey(progress.MemberQuest)
		quest, err := k.Quest.Get(ctx, questID)
		if err != nil {
			continue
		}
		updated := false
		for i, objective := range quest.Objectives {
			if objective == nil || objective.Type != objectiveType || i >= len(progress.ObjectiveProgress) {
				continue
			}
			next := progress.ObjectiveProgress[i] + increment
			if next > objective.Target {
				next = objective.Target
			}
			if next != progress.ObjectiveProgress[i] {
				progress.ObjectiveProgress[i] = next
				updated = true
			}
		}
		if !updated {
			continue
		}
		if err := k.MemberQuestProgress.Set(ctx, progress.MemberQuest, progress); err != nil {
			return err
		}
		if questObjectivesComplete(quest, progress) {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent("quest_objectives_complete",
				sdk.NewAttribute("member", memberStr),
				sdk.NewAttribute("quest_id", questID),
			))
		}
	}
	return nil
}

// questObjectivesComplete reports whether every objective has reached its target.
func questObjectivesComplete(quest types.Quest, progress types.MemberQuestProgress) bool {
	for i, objective := range quest.Objectives {
		if objective == nil {
			continue
		}
		if i >= len(progress.ObjectiveProgress) || progress.ObjectiveProgress[i] < objective.Target {
			return false
		}
	}
	return true
}

// clearMemberQuestProgress drops every in-progress quest for the member
// (completed records are kept so prerequisites and cooldowns still hold).
func (k Keeper) clearMemberQuestProgress(ctx context.Context, memberStr string) error {
	inProgress, err := k.memberInProgressQuests(ctx, memberStr)
	if err != nil {
		return err
	}
	for _, p := range inProgress {
		if err := k.MemberQuestProgress.Remove(ctx, p.MemberQuest); err != nil {
			return err
		}
	}
	return nil
}

// questProgressKey builds the composite MemberQuestProgress key.
func questProgressKey(memberStr, questID string) string {
	return fmt.Sprintf("%s:%s", memberStr, questID)
}
