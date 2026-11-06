package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// engineFixture is initFixture plus a current season, the bootstrap
// achievements/titles and a shorter epoch so epoch arithmetic is visible.
func engineFixture(t *testing.T) (*fixture, sdk.Context) {
	t.Helper()
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(1000)
	require.NoError(t, f.keeper.Season.Set(ctx, types.Season{Number: 3, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	f.keeper.BootstrapTitlesAndAchievements(ctx)
	params, err := f.keeper.Params.Get(ctx)
	require.NoError(t, err)
	params.EpochBlocks = 100
	// The bootstrap achievements pay hundreds of XP each; lift the epoch cap
	// so tests can reason about exact totals (the cap has its own test).
	params.MaxXpPerEpoch = 1_000_000
	require.NoError(t, f.keeper.Params.Set(ctx, params))
	// SEASONS_ACTIVE achievements fire for any member on first touch; drop
	// them so each test's XP arithmetic names only the achievements it
	// expects.
	var seasonsActive []string
	require.NoError(t, f.keeper.Achievement.Walk(ctx, nil, func(id string, a types.Achievement) (bool, error) {
		if a.RequirementType == types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE {
			seasonsActive = append(seasonsActive, id)
		}
		return false, nil
	}))
	for _, id := range seasonsActive {
		require.NoError(t, f.keeper.Achievement.Remove(ctx, id))
	}
	return f, ctx
}

func eventCount(ctx sdk.Context, typ string) int {
	n := 0
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

func TestGrantXPCapsPerEpochAndLevelsUp(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	member := TestAddrMember1
	memberStr := GetAddrString(t, f.addressCodec, member)
	params, _ := k.Params.Get(ctx)
	params.MaxXpPerEpoch = 200
	require.NoError(t, k.Params.Set(ctx, params))

	// No profile yet: the first grant creates one.
	require.NoError(t, k.GrantXP(ctx, member, 150, types.XPSourceVoteCast, "proposal:1"))
	p, err := k.MemberProfile.Get(ctx, memberStr)
	require.NoError(t, err)
	require.Equal(t, uint64(150), p.SeasonXp)
	require.Equal(t, uint64(150), p.LifetimeXp)
	require.Equal(t, k.CalculateLevel(ctx, 150), p.SeasonLevel)
	require.Equal(t, 1, eventCount(ctx, "level_up"), "150 XP crosses the level-2 threshold")

	// The rest of the epoch allowance is clamped, then refused.
	require.NoError(t, k.GrantXP(ctx, member, 1000, types.XPSourceQuest, "q"))
	p, _ = k.MemberProfile.Get(ctx, memberStr)
	require.Equal(t, params.MaxXpPerEpoch, p.SeasonXp, "clamped to the epoch cap")
	require.ErrorIs(t, k.GrantXP(ctx, member, 1, types.XPSourceQuest, "q"), types.ErrXPCapReached)

	// Tracker buckets by source.
	tracker, err := k.EpochXpTracker.Get(ctx, fmt.Sprintf("%s:%d", memberStr, k.GetCurrentEpoch(ctx)))
	require.NoError(t, err)
	require.Equal(t, uint64(150), tracker.VoteXpEarned)
	require.Equal(t, params.MaxXpPerEpoch-150, tracker.QuestXpEarned)

	// A new epoch opens a fresh allowance.
	next := ctx.WithBlockHeight(ctx.BlockHeight() + params.EpochBlocks)
	require.NoError(t, k.GrantXP(next, member, 10, types.XPSourceQuest, "q"))
}

func TestOnVoteCastDedupesAndCountsAchievements(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	voter := TestAddrMember1
	voterStr := GetAddrString(t, f.addressCodec, voter)
	params, _ := k.Params.Get(ctx)

	require.NoError(t, k.OnVoteCast(ctx, voter, 7))
	require.NoError(t, k.OnVoteCast(ctx, voter, 7), "second vote on the same proposal is a no-op")
	p, err := k.MemberProfile.Get(ctx, voterStr)
	require.NoError(t, err)
	require.Equal(t, uint64(1), p.VotesCast)
	// The bootstrap "voice_heard" achievement (1 vote) pays 10 XP on top of
	// the vote XP itself.
	require.Contains(t, p.Achievements, "voice_heard")
	require.Equal(t, params.XpVoteCast+10, p.SeasonXp)

	// Per-epoch vote cap: max_vote_xp_per_epoch distinct proposals.
	for id := uint64(100); id < 100+uint64(params.MaxVoteXpPerEpoch)+5; id++ {
		require.NoError(t, k.OnVoteCast(ctx, voter, id))
	}
	tracker, _ := k.EpochXpTracker.Get(ctx, fmt.Sprintf("%s:%d", voterStr, k.GetCurrentEpoch(ctx)))
	require.Equal(t, params.XpVoteCast*uint64(params.MaxVoteXpPerEpoch), tracker.VoteXpEarned)
	p, _ = k.MemberProfile.Get(ctx, voterStr)
	require.Equal(t, uint64(params.MaxVoteXpPerEpoch), p.VotesCast, "votes past the per-epoch vote cap neither pay nor count (spec OnVoteCast)")
}

func TestForumXPAntiGaming(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	author := GetAddrString(t, f.addressCodec, TestAddrMember1)
	actor := GetAddrString(t, f.addressCodec, TestAddrMember2)
	params, _ := k.Params.Get(ctx)

	// Actor without a registration is too new: nothing granted.
	require.NoError(t, k.OnForumReplyReceived(ctx, author, actor, 1))
	require.False(t, k.HasMemberProfile(ctx, author))

	// Register the actor old enough.
	require.NoError(t, k.OnMemberCreated(ctx, TestAddrMember2))
	old := ctx.WithBlockHeight(ctx.BlockHeight() + int64(params.ForumXpMinAccountAgeEpochs+1)*params.EpochBlocks)

	require.NoError(t, k.OnForumReplyReceived(old, author, actor, 1))
	p, err := k.MemberProfile.Get(old, author)
	require.NoError(t, err)
	require.Equal(t, params.XpForumReplyReceived, p.SeasonXp)

	// Same actor again inside the reciprocal cooldown: nothing.
	require.NoError(t, k.OnForumReplyReceived(old, author, actor, 2))
	p, _ = k.MemberProfile.Get(old, author)
	require.Equal(t, params.XpForumReplyReceived, p.SeasonXp)

	// Self-reply never grants.
	require.NoError(t, k.OnForumReplyReceived(old, actor, actor, 3))
	ap, _ := k.MemberProfile.Get(old, actor)
	require.Equal(t, uint64(0), ap.SeasonXp)

	// After the cooldown a helpful mark grants, counts and advances quests.
	later := old.WithBlockHeight(old.BlockHeight() + int64(params.ForumXpReciprocalCooldownEpochs)*params.EpochBlocks)
	require.NoError(t, k.OnForumPostMarkedHelpful(later, author, actor, 9, true))
	p, _ = k.MemberProfile.Get(later, author)
	require.Equal(t, uint64(1), p.ForumHelpfulCount)
	require.Equal(t, params.XpForumReplyReceived+params.XpForumMarkedHelpful, p.SeasonXp)
}

func TestQuestObjectivesProgressAndClaim(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	member := TestAddrMember1
	memberStr := GetAddrString(t, f.addressCodec, member)
	require.NoError(t, k.OnMemberCreated(ctx, member))

	// Typed objectives are required.
	require.ErrorIs(t, k.ValidateQuestObjectives(ctx, []*types.QuestObjective{{Target: 1}}), types.ErrInvalidQuestObjective)
	require.ErrorIs(t, k.ValidateQuestObjectives(ctx, []*types.QuestObjective{{Type: types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_VOTES_CAST}}), types.ErrInvalidQuestObjective)

	SetupQuestWithObjectives(t, k, ctx, "civic", 40, []*types.QuestObjective{
		{Description: "vote twice", Type: types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_VOTES_CAST, Target: 2},
		{Description: "ship one", Type: types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_INITIATIVES_COMPLETED, Target: 1},
	})
	_, err := ms.StartQuest(ctx, &types.MsgStartQuest{Creator: memberStr, QuestId: "civic"})
	require.NoError(t, err)

	// Progress comes from the hooks, clamped at the target.
	require.NoError(t, k.OnVoteCast(ctx, member, 1))
	_, err = ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{Creator: memberStr, QuestId: "civic"})
	require.ErrorIs(t, err, types.ErrQuestNotComplete)
	require.NoError(t, k.OnVoteCast(ctx, member, 2))
	require.NoError(t, k.OnVoteCast(ctx, member, 3))
	require.NoError(t, k.OnInitiativeCompleted(ctx, member, 55))
	// Vote objectives land at EndBlock (SEA-103).
	k.ApplyPendingVoteQuestProgress(ctx)

	progress, err := k.MemberQuestProgress.Get(ctx, memberStr+":civic")
	require.NoError(t, err)
	require.Equal(t, []uint64{2, 1}, progress.ObjectiveProgress)
	require.Equal(t, 1, eventCount(ctx, "quest_objectives_complete"))

	before, _ := k.MemberProfile.Get(ctx, memberStr)
	_, err = ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{Creator: memberStr, QuestId: "civic"})
	require.NoError(t, err)
	after, _ := k.MemberProfile.Get(ctx, memberStr)
	require.Equal(t, before.SeasonXp+40, after.SeasonXp)
	tracker, _ := k.EpochXpTracker.Get(ctx, fmt.Sprintf("%s:%d", memberStr, k.GetCurrentEpoch(ctx)))
	require.Equal(t, uint64(40), tracker.QuestXpEarned, "quest XP lands in the quest bucket")

	// Zeroing drops in-progress quests and resets counters, keeps lifetime XP.
	SetupQuestWithObjectives(t, k, ctx, "again", 5, []*types.QuestObjective{
		{Description: "vote", Type: types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_VOTES_CAST, Target: 1},
	})
	_, err = ms.StartQuest(ctx, &types.MsgStartQuest{Creator: memberStr, QuestId: "again"})
	require.NoError(t, err)
	require.NoError(t, k.OnMemberZeroed(ctx, member))
	_, err = k.MemberQuestProgress.Get(ctx, memberStr+":again")
	require.Error(t, err)
	zeroed, _ := k.MemberProfile.Get(ctx, memberStr)
	require.Equal(t, uint64(0), zeroed.SeasonXp)
	require.Equal(t, uint64(0), zeroed.VotesCast)
	require.Equal(t, after.LifetimeXp, zeroed.LifetimeXp)
	require.Equal(t, after.Achievements, zeroed.Achievements)
}

func TestInviterEarnsMentorshipXP(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	inviter, invitee := TestAddrMember1, TestAddrMember2
	inviterStr := GetAddrString(t, f.addressCodec, inviter)
	params, _ := k.Params.Get(ctx)

	mock := newMockRepKeeper()
	mock.Members[inviter.String()] = true
	mock.Members[invitee.String()] = true
	mock.InvitedBy = map[string]string{invitee.String(): inviterStr}
	mock.CompletedInitiatives = map[string]uint64{GetAddrString(t, f.addressCodec, invitee): 1}
	k.SetRepKeeper(mock)

	require.NoError(t, k.OnInitiativeCompleted(ctx, invitee, 1))
	p, err := k.MemberProfile.Get(ctx, inviterStr)
	require.NoError(t, err)
	require.Equal(t, params.XpInviteeFirstInitiative, p.SeasonXp)

	require.NoError(t, k.OnMemberEstablished(ctx, invitee))
	p, _ = k.MemberProfile.Get(ctx, inviterStr)
	require.Equal(t, uint64(1), p.InvitationsSuccessful)
	// "first_step" (1 successful invitation) pays 10 XP.
	require.Contains(t, p.Achievements, "first_step")
	require.Equal(t, params.XpInviteeFirstInitiative+params.XpInviteeEstablished+10, p.SeasonXp)
}

func TestSeasonalTitlesGrantedOnTransition(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	top := GetAddrString(t, f.addressCodec, TestAddrMember1)
	low := GetAddrString(t, f.addressCodec, TestAddrMember2)
	require.NoError(t, k.MemberProfile.Set(ctx, top, types.MemberProfile{Address: top, SeasonXp: 5000, SeasonLevel: 8, ChallengesWon: 3}))
	require.NoError(t, k.MemberProfile.Set(ctx, low, types.MemberProfile{Address: low, SeasonXp: 10, SeasonLevel: 1}))

	require.NoError(t, k.ComputeSeasonTitleEligibility(ctx, 3))
	elig, err := k.SeasonTitleEligibility.Get(ctx, 3)
	require.NoError(t, err)
	grants := map[string][]string{}
	for _, g := range elig.Grants {
		grants[g.TitleId] = g.EligibleMembers
	}
	require.ElementsMatch(t, []string{top, low}, grants["champion"], "top-10 XP includes both")
	// Threshold titles are recorded on each member's season snapshot.
	topSnap, err := k.MemberSeasonSnapshot.Get(ctx, "3/"+top)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"rising_star", "guardian"}, topSnap.EligibleTitleIds, "level 5+, 3 challenges won; level 10 not reached")
	lowSnap, err := k.MemberSeasonSnapshot.Get(ctx, "3/"+low)
	require.NoError(t, err)
	require.Empty(t, lowSnap.EligibleTitleIds)

	// Drive the TITLES phase over every profile.
	state := types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_TITLES}
	done, err := k.ProcessTitlesPhaseForTest(ctx, &state, 100)
	require.NoError(t, err)
	require.True(t, done)

	tp, _ := k.MemberProfile.Get(ctx, top)
	require.Contains(t, tp.UnlockedTitles, "s3_champion")
	require.Contains(t, tp.UnlockedTitles, "s3_rising_star")
	require.Contains(t, tp.UnlockedTitles, "s3_guardian")
	lp, _ := k.MemberProfile.Get(ctx, low)
	require.Equal(t, []string{"s3_champion"}, lp.UnlockedTitles)
	title, err := k.Title.Get(ctx, "s3_champion")
	require.NoError(t, err)
	require.True(t, title.Seasonal)
	require.Equal(t, uint64(3), title.RequirementSeason)

	// Running the phase again is idempotent.
	state = types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_TITLES}
	_, err = k.ProcessTitlesPhaseForTest(ctx, &state, 100)
	require.NoError(t, err)
	tp, _ = k.MemberProfile.Get(ctx, top)
	require.Len(t, tp.UnlockedTitles, 3)
}

func TestOnMemberZeroedLeavesGuildAndFreezesForFounder(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	founder, officer := TestAddrMember1, TestAddrMember2
	founderStr := GetAddrString(t, f.addressCodec, founder)
	officerStr := GetAddrString(t, f.addressCodec, officer)

	guildID := SetupGuild(t, k, ctx, founder, "Aurora", "test guild")
	SetupMemberProfileWithGuild(t, k, ctx, officer, guildID)
	guild, err := k.Guild.Get(ctx, guildID)
	require.NoError(t, err)
	guild.Officers = append(guild.Officers, officerStr)
	require.NoError(t, k.Guild.Set(ctx, guildID, guild))

	// An officer being zeroed leaves the guild and drops the officer rank.
	require.NoError(t, k.OnMemberZeroed(ctx, officer))
	op, _ := k.MemberProfile.Get(ctx, officerStr)
	require.Equal(t, uint64(0), op.GuildId)
	guild, _ = k.Guild.Get(ctx, guildID)
	require.NotContains(t, guild.Officers, officerStr)

	// The founder being zeroed freezes the guild so a member can claim it.
	require.NoError(t, k.OnMemberZeroed(ctx, founder))
	fp, _ := k.MemberProfile.Get(ctx, founderStr)
	require.Equal(t, uint64(0), fp.GuildId)
	guild, _ = k.Guild.Get(ctx, guildID)
	require.Equal(t, types.GuildStatus_GUILD_STATUS_FROZEN, guild.Status)
}

// SEA-02: the per-season copies (`s<N>_<id>`, RequirementSeason = N) are
// never evaluated as seasonal bases, so the number of seasonal definitions
// grows by the base count per season instead of doubling.
func TestSeasonalTitleDefinitionsDoNotDouble(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	top := GetAddrString(t, f.addressCodec, TestAddrMember1)
	countSeasonal := func() (bases, copies int) {
		require.NoError(t, k.Title.Walk(ctx, nil, func(_ string, ti types.Title) (bool, error) {
			if ti.Seasonal {
				if ti.RequirementSeason == 0 {
					bases++
				} else {
					copies++
				}
			}
			return false, nil
		}))
		return
	}
	baseCount, _ := countSeasonal()
	require.Positive(t, baseCount)

	for season := uint64(1); season <= 4; season++ {
		cur, err := k.Season.Get(ctx)
		require.NoError(t, err)
		cur.Number = season
		require.NoError(t, k.Season.Set(ctx, cur))
		// The member qualifies for every seasonal base each season.
		require.NoError(t, k.MemberProfile.Set(ctx, top, types.MemberProfile{
			Address: top, SeasonXp: 50000, SeasonLevel: 20, ChallengesWon: 50, VotesCast: 500,
		}))
		require.NoError(t, k.ComputeSeasonTitleEligibility(ctx, season))
		state := types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_TITLES}
		_, err = k.ProcessTitlesPhaseForTest(ctx, &state, 100)
		require.NoError(t, err)

		bases, copies := countSeasonal()
		t.Logf("season %d bases=%d copies=%d", season, bases, copies)
		require.Equal(t, baseCount, bases, "season %d: bases must not multiply", season)
		require.LessOrEqual(t, copies, baseCount*int(season), "season %d: at most one copy per base per season", season)
	}
}
