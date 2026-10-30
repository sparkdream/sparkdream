package simulation

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// Achievement and title definitions are managed by the Commons Operations
// Committee through executed x/commons proposals (council.go). The sim
// creates its own definitions under simDefPrefix, at most simDefMax of each
// kind, and deletes only those: the genesis catalog stays in place, and a
// sim deletion never leaves another module's reference behind (see the
// report on dangling references).
const (
	simDefPrefix = "sim_"
	simDefMax    = 6
)

// randomRarity draws a defined, non-zero rarity.
func randomRarity(e *simEnv) uint32 {
	return commontypes.Saturate[uint32](1 + e.r.Intn(int(types.Rarity_RARITY_UNIQUE)))
}

// randomRequirementType draws a defined requirement type other than
// UNSPECIFIED and GENESIS (genesis-only founders' badges).
func randomRequirementType(e *simEnv) uint32 {
	return commontypes.Saturate[uint32](1 + e.r.Intn(int(types.RequirementType_REQUIREMENT_TYPE_ACHIEVEMENT_COUNT)))
}

// randomThreshold draws a requirement threshold.
func randomThreshold(e *simEnv) uint64 {
	return commontypes.Saturate[uint64](1 + e.r.Intn(50))
}

// randomAchievementXP draws an achievement XP reward (granted through the
// capped XP engine, so it stays within an epoch's XP).
func randomAchievementXP(e *simEnv) uint64 {
	return commontypes.Saturate[uint64](e.r.Intn(101))
}

// allAchievements returns the achievement ids in order.
func allAchievements(ctx sdk.Context, k keeper.Keeper) ([]string, error) {
	var out []string
	err := k.Achievement.Walk(ctx, nil, func(id string, _ types.Achievement) (bool, error) {
		out = append(out, id)
		return false, nil
	})
	return out, err
}

// allTitles returns the title ids in order.
func allTitles(ctx sdk.Context, k keeper.Keeper) ([]string, error) {
	var out []string
	err := k.Title.Walk(ctx, nil, func(id string, _ types.Title) (bool, error) {
		out = append(out, id)
		return false, nil
	})
	return out, err
}

// simOwned keeps the ids the sim created.
func simOwned(ids []string) []string {
	var out []string
	for _, id := range ids {
		if strings.HasPrefix(id, simDefPrefix) {
			out = append(out, id)
		}
	}
	return out
}

// without drops the ids in skip.
func without(ids []string, skip map[string]bool) []string {
	var out []string
	for _, id := range ids {
		if !skip[id] {
			out = append(out, id)
		}
	}
	return out
}

// newDefinitionID returns a fresh sim definition id not in existing or
// pending.
func newDefinitionID(e *simEnv, kind string, existing []string, pending map[string]bool) (string, bool) {
	id := fmt.Sprintf("%s%s_%d", simDefPrefix, kind, e.r.Intn(100000))
	for _, x := range existing {
		if x == id {
			return "", false
		}
	}
	return id, !pending[id]
}

// achievementIDOf, titleIDOf read the definition id of an open proposal's
// message.
func achievementIDOf(m sdk.Msg) string {
	switch msg := m.(type) {
	case *types.MsgCreateAchievement:
		return msg.AchievementId
	case *types.MsgUpdateAchievement:
		return msg.AchievementId
	case *types.MsgDeleteAchievement:
		return msg.AchievementId
	}
	return ""
}

func titleIDOf(m sdk.Msg) string {
	switch msg := m.(type) {
	case *types.MsgCreateTitle:
		return msg.TitleId
	case *types.MsgUpdateTitle:
		return msg.TitleId
	case *types.MsgDeleteTitle:
		return msg.TitleId
	}
	return ""
}

// pendingIDs returns the ids the open proposals' messages target.
func pendingIDs(taken []sdk.Msg, idOf func(sdk.Msg) string) map[string]bool {
	out := map[string]bool{}
	for _, m := range taken {
		out[idOf(m)] = true
	}
	return out
}
