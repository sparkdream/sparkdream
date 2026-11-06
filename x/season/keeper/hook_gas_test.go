package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"

	storetypes "cosmossdk.io/store/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/types"
)

// TestVoteHookGas reports the gas the vote hook consumes for a seeded
// genesis-style member (counters already high, a few achievements), first
// vote and tenth vote. Printed with -v; asserted against a ceiling so a
// regression that walks every definition again fails here instead of in
// the e2e suite where a 200000-gas vote tx goes out of gas.
func TestVoteHookGas(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	voter := TestAddrMember1
	voterStr := GetAddrString(t, f.addressCodec, voter)
	require.NoError(t, k.MemberProfile.Set(ctx, voterStr, types.MemberProfile{
		Address: voterStr, SeasonXp: 5000, LifetimeXp: 5000, SeasonLevel: 8,
		VotesCast: 15, ForumHelpfulCount: 50, InvitationsSuccessful: 5, ChallengesWon: 2,
		Achievements:   []string{"first_step", "voice_heard", "contributor"},
		UnlockedTitles: []string{"newcomer", "veteran", "rising_star"},
	}))
	measure := func(proposal uint64) uint64 {
		gctx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
		require.NoError(t, k.OnVoteCast(gctx, voter, proposal))
		return gctx.GasMeter().GasConsumed()
	}
	first := measure(1)
	tenth := uint64(0)
	for p := uint64(2); p <= 10; p++ {
		tenth = measure(p)
	}
	t.Logf("vote hook gas: first=%d tenth=%d", first, tenth)
	require.Less(t, first, uint64(60000), "first vote hook gas")
	require.Less(t, tenth, uint64(40000), "steady-state vote hook gas")
}

// TestVoteHookGasWithOpenQuests repeats the measurement with the voter at the
// default quest cap (max_active_quests_per_member open quests, each with a
// VOTES_CAST objective), the worst case of the quest-progress step that the
// plain case does not seed (SEA-103).
func TestVoteHookGasWithOpenQuests(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	voter := TestAddrMember1
	voterStr := GetAddrString(t, f.addressCodec, voter)
	require.NoError(t, k.MemberProfile.Set(ctx, voterStr, types.MemberProfile{
		Address: voterStr, SeasonXp: 5000, LifetimeXp: 5000, SeasonLevel: 8,
		VotesCast: 15, ForumHelpfulCount: 50, InvitationsSuccessful: 5, ChallengesWon: 2,
		Achievements:   []string{"first_step", "voice_heard", "contributor"},
		UnlockedTitles: []string{"newcomer", "veteran", "rising_star"},
	}))
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	for i := uint32(0); i < params.MaxActiveQuestsPerMember; i++ {
		id := fmt.Sprintf("vote_quest_%d", i)
		require.NoError(t, k.Quest.Set(ctx, id, types.Quest{
			QuestId: id, Name: id, XpReward: 10, Active: true,
			Objectives: []*types.QuestObjective{
				{Type: types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_VOTES_CAST, Target: 1000},
				{Type: types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_FORUM_HELPFUL, Target: 10},
			},
		}))
		key := voterStr + ":" + id
		require.NoError(t, k.MemberQuestProgress.Set(ctx, key, types.MemberQuestProgress{
			MemberQuest: key, ObjectiveProgress: []uint64{0, 0},
		}))
	}
	measure := func(proposal uint64) uint64 {
		gctx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
		require.NoError(t, k.OnVoteCast(gctx, voter, proposal))
		return gctx.GasMeter().GasConsumed()
	}
	first := measure(1)
	tenth := uint64(0)
	for p := uint64(2); p <= 10; p++ {
		tenth = measure(p)
	}
	t.Logf("vote hook gas with %d open vote quests: first=%d tenth=%d", params.MaxActiveQuestsPerMember, first, tenth)
	require.Less(t, first, uint64(60000), "first vote hook gas")
	require.Less(t, tenth, uint64(45000), "steady-state vote hook gas")

	// The ten votes reach every open quest at EndBlock.
	k.ApplyPendingVoteQuestProgress(ctx)
	for i := uint32(0); i < params.MaxActiveQuestsPerMember; i++ {
		p, err := k.MemberQuestProgress.Get(ctx, fmt.Sprintf("%s:vote_quest_%d", voterStr, i))
		require.NoError(t, err)
		require.Equal(t, []uint64{10, 0}, p.ObjectiveProgress)
	}
	pending := 0
	require.NoError(t, k.PendingVoteQuestProgress.Walk(ctx, nil, func(collections.Pair[string, uint64]) (bool, error) {
		pending++
		return false, nil
	}))
	require.Zero(t, pending, "the EndBlocker clears the pending votes")
}
