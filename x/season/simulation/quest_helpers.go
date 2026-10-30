package simulation

import (
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// Quest definitions are created by MsgCreateQuest, whose signer is a
// council/committee policy: the quest ops deliver it through an executed
// x/commons proposal (council.go). simQuestTarget is how many active quests
// the deactivation ops leave in place, simQuestMax how many definitions
// MsgCreateQuest creates at most.
const (
	simQuestTarget = 3
	simQuestMax    = 8
)

// allQuests returns every quest definition by id.
func allQuests(ctx sdk.Context, k keeper.Keeper) (map[string]types.Quest, error) {
	out := map[string]types.Quest{}
	err := k.Quest.Walk(ctx, nil, func(id string, q types.Quest) (bool, error) {
		out[id] = q
		return false, nil
	})
	return out, err
}

// activeQuestCount counts the active quests.
func activeQuestCount(quests map[string]types.Quest) int {
	n := 0
	for _, q := range quests {
		if q.Active {
			n++
		}
	}
	return n
}

// sortedKeys returns m's keys in order (map order is random, and a
// simulation must replay from its seed).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// randomQuestXP draws a quest XP reward within max_quest_xp_reward and the
// compiled HardMaxQuestXpReward.
func randomQuestXP(e *simEnv, params types.Params) uint64 {
	maxXP := params.MaxQuestXpReward
	if maxXP > types.HardMaxQuestXpReward {
		maxXP = types.HardMaxQuestXpReward
	}
	return commontypes.Saturate[uint64](e.r.Int63n(commontypes.Saturate[int64](maxXP) + 1))
}

// pickName returns a fictional name for definitions and season info.
func pickName(e *simEnv) string {
	names := []string{"Phoenix", "Aurora", "Zenith", "Nebula", "Solstice", "Ember", "Tidewater", "Meridian"}
	return names[e.r.Intn(len(names))]
}

// questOpenNow mirrors the keeper's check that a quest can be started or
// claimed at the current block: active, inside its block window and, for a
// seasonal quest, in its season.
func questOpenNow(ctx sdk.Context, k keeper.Keeper, q types.Quest) bool {
	if !q.Active {
		return false
	}
	h := ctx.BlockHeight()
	if q.StartBlock > 0 && h < q.StartBlock {
		return false
	}
	if q.EndBlock > 0 && h > q.EndBlock {
		return false
	}
	if q.Season > 0 {
		if season, err := k.Season.Get(ctx); err == nil && season.Number != q.Season {
			return false
		}
	}
	return true
}

// canStartQuest mirrors the keeper's MsgStartQuest predicate.
func canStartQuest(ctx sdk.Context, k keeper.Keeper, member string, profile types.MemberProfile, q types.Quest, maxActive uint32) bool {
	if !questOpenNow(ctx, k, q) {
		return false
	}
	if q.MinLevel > 0 && profile.SeasonLevel < q.MinLevel {
		return false
	}
	if q.RequiredAchievement != "" {
		has := false
		for _, a := range profile.Achievements {
			if a == q.RequiredAchievement {
				has = true
				break
			}
		}
		if !has {
			return false
		}
	}
	if q.PrerequisiteQuest != "" && !k.HasQuestPrerequisite(ctx, member, q.PrerequisiteQuest) {
		return false
	}
	if progress, err := k.MemberQuestProgress.Get(ctx, questProgressKey(member, q.QuestId)); err == nil {
		if !progress.Completed || !q.Repeatable {
			return false
		}
		since := k.GetCurrentEpoch(ctx) - k.BlockToEpoch(ctx, progress.CompletedBlock)
		if q.CooldownEpochs > 0 && commontypes.Saturate[uint64](since) < q.CooldownEpochs {
			return false
		}
	}
	return k.GetMemberActiveQuestCount(ctx, member) < maxActive
}

// questObjectivesComplete mirrors the keeper's claim check: progress on the
// quest's current objective version with every target reached.
func questObjectivesComplete(q types.Quest, p types.MemberQuestProgress) bool {
	if p.ObjectivesVersion != q.ObjectivesVersion {
		return false
	}
	for i, o := range q.Objectives {
		if o == nil {
			continue
		}
		if i >= len(p.ObjectiveProgress) || p.ObjectiveProgress[i] < o.Target {
			return false
		}
	}
	return true
}

// questProgressKey is the MemberQuestProgress key, "member:quest_id".
func questProgressKey(member, questID string) string {
	return member + ":" + questID
}

// openQuestProgress returns the not-yet-completed quest progress records of
// sim accounts, with the signer and quest id of each.
func openQuestProgress(ctx sdk.Context, k keeper.Keeper, accs map[string]bool) ([]questRef, error) {
	var out []questRef
	err := k.MemberQuestProgress.Walk(ctx, nil, func(key string, p types.MemberQuestProgress) (bool, error) {
		if p.Completed {
			return false, nil
		}
		for i := 0; i < len(key); i++ {
			if key[i] == ':' {
				if member := key[:i]; accs[member] {
					out = append(out, questRef{member: member, questID: key[i+1:], progress: p})
				}
				break
			}
		}
		return false, nil
	})
	return out, err
}

// questRef is a member's progress on one quest.
type questRef struct {
	member   string
	questID  string
	progress types.MemberQuestProgress
}
