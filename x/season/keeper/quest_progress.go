package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/types"
)

// ValidateQuestObjectives checks the objectives attached to a quest: a known
// type, a positive target and at most max_quest_objectives of them.
func (k Keeper) ValidateQuestObjectives(ctx context.Context, objectives []*types.QuestObjective) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if params.MaxQuestObjectives > 0 && commontypes.Saturate[uint32](len(objectives)) > params.MaxQuestObjectives {
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

// ValidateQuestPrerequisites checks that a quest's prerequisite quest and
// required achievement exist and that the prerequisite chain has no cycle
// back to questID, so a typo cannot make a quest permanently unstartable
// (SEA-19).
func (k Keeper) ValidateQuestPrerequisites(ctx context.Context, questID, prerequisite, requiredAchievement string) error {
	if requiredAchievement != "" {
		if has, err := k.Achievement.Has(ctx, requiredAchievement); err != nil || !has {
			return errorsmod.Wrapf(types.ErrInvalidQuestObjective, "required achievement %q does not exist", requiredAchievement)
		}
	}
	seen := map[string]bool{questID: true}
	for cur := prerequisite; cur != ""; {
		if seen[cur] {
			return errorsmod.Wrapf(types.ErrInvalidQuestObjective, "prerequisite chain of %q loops through %q", questID, cur)
		}
		seen[cur] = true
		q, err := k.Quest.Get(ctx, cur)
		if err != nil {
			return errorsmod.Wrapf(types.ErrInvalidQuestObjective, "prerequisite quest %q does not exist", cur)
		}
		cur = q.PrerequisiteQuest
	}
	return nil
}

// memberInProgressQuests returns the member's started, unclaimed quest
// progress records. It reads the Open index, so a member's completed quests
// cost nothing here (this runs inside vote and forum transactions, where the
// voter pays the gas; SEA-15).
func (k Keeper) memberInProgressQuests(ctx context.Context, memberStr string) ([]types.MemberQuestProgress, error) {
	var keys []string
	if err := k.MemberQuestProgress.Indexes.Open.Walk(ctx,
		collections.NewPrefixedPairRange[string, string](memberStr+questOpenSuffix),
		func(_ string, pk string) (bool, error) {
			keys = append(keys, pk)
			return false, nil
		}); err != nil {
		return nil, err
	}
	out := make([]types.MemberQuestProgress, 0, len(keys))
	for _, key := range keys {
		p, err := k.MemberQuestProgress.Get(ctx, key)
		if err != nil || p.Completed {
			continue
		}
		out = append(out, p)
	}
	return out, nil
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
		// A closed quest earns no progress (SEA-26). The season check is
		// left to CLEANUP, which drops ended seasonal quests' open
		// progress, so this hook reads no season record (vote gas).
		if !quest.Active || (quest.EndBlock > 0 && sdkCtx.BlockHeight() > quest.EndBlock) {
			continue
		}
		updated := false
		if !progressCurrent(quest, progress) {
			// The objectives changed since this progress started: restart
			// it against the current list (SEA-32).
			progress.ObjectiveProgress = make([]uint64, len(quest.Objectives))
			progress.ObjectivesVersion = quest.ObjectivesVersion
			updated = true
		}
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

// sameObjectives reports whether two objective lists are identical in type
// and target, position by position (descriptions do not matter).
func sameObjectives(a, b []*types.QuestObjective) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if (a[i] == nil) != (b[i] == nil) {
			return false
		}
		if a[i] != nil && (a[i].Type != b[i].Type || a[i].Target != b[i].Target) {
			return false
		}
	}
	return true
}

// progressCurrent reports whether progress was recorded against the
// quest's current objectives (SEA-32).
func progressCurrent(quest types.Quest, progress types.MemberQuestProgress) bool {
	return progress.ObjectivesVersion == quest.ObjectivesVersion
}

// questObjectivesComplete reports whether every objective has reached its
// target. Progress recorded against an older objective version is never
// complete.
func questObjectivesComplete(quest types.Quest, progress types.MemberQuestProgress) bool {
	if !progressCurrent(quest, progress) {
		return false
	}
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
