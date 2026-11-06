package keeper_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// Regression tests for the Kimi fold-in findings SEA-20 .. SEA-36.

func addrStr(t *testing.T, f *beginBlockFixture, name string) string {
	t.Helper()
	s, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte(name)))
	require.NoError(t, err)
	return s
}

func activeSeason(t *testing.T, f *beginBlockFixture, number uint64, status types.SeasonStatus) {
	t.Helper()
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{
		Number: number, StartBlock: 1, EndBlock: 10_000_000, Status: status,
	}))
}

// --- SEA-21: retro payouts ---------------------------------------------------

// failingPayRep fails PayRetroPgfReward while fail is set, after writing a
// marker into the season store through the context it was given: a stand-in
// for the treasury draw that x/rep commits before a failing mint.
type failingPayRep struct {
	*mockRepKeeper
	k     *keeper.Keeper
	fail  bool
	calls int
}

func (r *failingPayRep) PayRetroPgfReward(ctx context.Context, recipient sdk.AccAddress, amount math.Int) (math.Int, math.Int, error) {
	r.calls++
	if err := r.k.PruneCursor.Set(ctx, "partial-credit", recipient.String()); err != nil {
		return math.ZeroInt(), math.ZeroInt(), err
	}
	if r.fail {
		return math.ZeroInt(), math.ZeroInt(), errors.New("max dream mint per epoch reached")
	}
	return r.mockRepKeeper.PayRetroPgfReward(ctx, recipient, amount)
}

func setupPayoutNomination(t *testing.T, f *beginBlockFixture) string {
	t.Helper()
	lowerRetroMinConviction(t, f)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_ENDING}))
	nominator := addrStr(t, f, "phoenix_nominatr")
	f.repKeeper.SetMember(nominator, 0, nil, 0)
	require.NoError(t, f.keeper.SetNomination(f.ctx, types.Nomination{
		Id: 1, Nominator: nominator, ContentRef: "blog/post/1", Season: 1,
		TotalStaked: math.LegacyNewDec(100), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec(),
	}))
	require.NoError(t, f.keeper.NominationStake.Set(f.ctx, "1/"+nominator, types.NominationStake{
		NominationId: 1, Staker: nominator, Amount: math.LegacyNewDec(100), StakedAtBlock: 0,
	}))
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS}))
	return nominator
}

func TestSEA21_FailedRetroPayoutIsRetriedWithoutPartialCredit(t *testing.T) {
	f := initBeginBlockFixture(t)
	rep := &failingPayRep{mockRepKeeper: f.repKeeper, k: &f.keeper, fail: true}
	f.keeper.SetRepKeeper(rep)
	setupPayoutNomination(t, f)
	f.ctx = f.ctx.WithBlockHeight(200_001) // not an epoch boundary

	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	nom, err := f.keeper.Nomination.Get(f.ctx, 1)
	require.NoError(t, err)
	require.False(t, nom.Rewarded)
	require.True(t, nom.RewardAmount.TruncateInt().IsPositive(), "the award is kept for the retry")
	require.Equal(t, uint32(1), nom.PayoutAttempts)
	queued, err := f.keeper.RetroPayoutRetry.Has(f.ctx, collections.Join(uint64(1), uint64(1)))
	require.NoError(t, err)
	require.True(t, queued, "a failed payout is queued")
	has, err := f.keeper.PruneCursor.Has(f.ctx, "partial-credit")
	require.NoError(t, err)
	require.False(t, has, "the failed payout's writes are discarded (no partial credit)")
	has, err = f.keeper.RetroRewardRecord.Has(f.ctx, "1/1")
	require.NoError(t, err)
	require.False(t, has)

	// The transition moves on; the retry happens once the transition is
	// over, on an epoch boundary.
	require.NoError(t, f.keeper.SeasonTransitionState.Remove(f.ctx))
	params, _ := f.keeper.Params.Get(f.ctx)
	rep.fail = false
	f.ctx = f.ctx.WithBlockHeight(params.EpochBlocks * 1000)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))

	nom, err = f.keeper.Nomination.Get(f.ctx, 1)
	require.NoError(t, err)
	require.True(t, nom.Rewarded, "the retry pays")
	queued, _ = f.keeper.RetroPayoutRetry.Has(f.ctx, collections.Join(uint64(1), uint64(1)))
	require.False(t, queued)
	has, err = f.keeper.RetroRewardRecord.Has(f.ctx, "1/1")
	require.NoError(t, err)
	require.True(t, has, "a paid retry is recorded")
}

func TestSEA21_RetroPayoutAbandonedAfterMaxAttempts(t *testing.T) {
	f := initBeginBlockFixture(t)
	rep := &failingPayRep{mockRepKeeper: f.repKeeper, k: &f.keeper, fail: true}
	f.keeper.SetRepKeeper(rep)
	setupPayoutNomination(t, f)
	f.ctx = f.ctx.WithBlockHeight(200_001)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	require.NoError(t, f.keeper.SeasonTransitionState.Remove(f.ctx))

	params, _ := f.keeper.Params.Get(f.ctx)
	for i := int64(1); i < keeper.MaxRetroPayoutAttempts; i++ {
		f.ctx = f.ctx.WithBlockHeight(params.EpochBlocks * (1000 + i))
		require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	}
	nom, err := f.keeper.Nomination.Get(f.ctx, 1)
	require.NoError(t, err)
	require.True(t, nom.PayoutAbandoned)
	require.Equal(t, uint32(keeper.MaxRetroPayoutAttempts), nom.PayoutAttempts)
	queued, _ := f.keeper.RetroPayoutRetry.Has(f.ctx, collections.Join(uint64(1), uint64(1)))
	require.False(t, queued, "an abandoned payout leaves the queue")
}

// --- SEA-23: display-name moderation ------------------------------------------

func TestSEA23_AppealTimesOutAndReturnsBothStakes(t *testing.T) {
	f := initBeginBlockFixture(t)
	activeSeason(t, f, 1, types.SeasonStatus_SEASON_STATUS_ACTIVE)
	target := addrStr(t, f, "target__________")
	reporter := addrStr(t, f, "reporter________")
	require.NoError(t, f.keeper.MemberProfile.Set(f.ctx, target, types.MemberProfile{Address: target, DisplayName: "Zenith", SeasonLevel: 1}))
	require.NoError(t, f.keeper.MemberProfile.Set(f.ctx, reporter, types.MemberProfile{Address: reporter, SeasonLevel: 1}))
	ms := keeper.NewMsgServerImpl(f.keeper)

	f.ctx = f.ctx.WithBlockHeight(100)
	_, err := ms.ReportDisplayName(f.ctx, &types.MsgReportDisplayName{Creator: reporter, Target: target, Reason: "x"})
	require.NoError(t, err)
	f.ctx = f.ctx.WithBlockHeight(110)
	_, err = ms.AppealDisplayNameModeration(f.ctx, &types.MsgAppealDisplayNameModeration{Creator: target, AppealReason: "fine"})
	require.NoError(t, err)

	params, _ := f.keeper.Params.Get(f.ctx)
	window := keeper.AppealResolutionPeriodMultiplier * int64(params.DisplayNameAppealPeriodBlocks)
	// Before the appeal deadline nothing happens (the report deadline has
	// passed, but appealed records never auto-uphold).
	f.ctx = f.ctx.WithBlockHeight(110 + window)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	mod, _ := f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.True(t, mod.Active)

	f.ctx = f.ctx.WithBlockHeight(111 + window)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	mod, _ = f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.False(t, mod.Active, "an unresolved appeal times out")
	require.Empty(t, mod.BlockedNames, "a timed-out appeal blocks nothing")
	profile, _ := f.keeper.MemberProfile.Get(f.ctx, target)
	require.Equal(t, "Zenith", profile.DisplayName, "the name is restored")
	stakes := 0
	require.NoError(t, f.keeper.DisplayNameReportStake.Walk(f.ctx, nil, func(string, types.DisplayNameReportStake) (bool, error) { stakes++; return false, nil }))
	require.NoError(t, f.keeper.DisplayNameAppealStake.Walk(f.ctx, nil, func(string, types.DisplayNameAppealStake) (bool, error) { stakes++; return false, nil }))
	require.Zero(t, stakes, "both stakes are settled (returned)")
}

func TestSEA23_UpheldNameStaysBlockedAfterClosure(t *testing.T) {
	f := initBeginBlockFixture(t)
	activeSeason(t, f, 1, types.SeasonStatus_SEASON_STATUS_ACTIVE)
	target := addrStr(t, f, "target__________")
	reporter := addrStr(t, f, "reporter________")
	require.NoError(t, f.keeper.MemberProfile.Set(f.ctx, target, types.MemberProfile{Address: target, DisplayName: "Rudename", SeasonLevel: 1}))
	ms := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = f.ctx.WithBlockHeight(100)
	_, err := ms.ReportDisplayName(f.ctx, &types.MsgReportDisplayName{Creator: reporter, Target: target, Reason: "x"})
	require.NoError(t, err)

	params, _ := f.keeper.Params.Get(f.ctx)
	f.ctx = f.ctx.WithBlockHeight(101 + int64(params.DisplayNameAppealPeriodBlocks))
	require.NoError(t, f.keeper.BeginBlocker(f.ctx)) // unappealed: report upheld
	mod, _ := f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.False(t, mod.Active)
	require.Equal(t, []string{"rudename"}, mod.BlockedNames)

	_, err = ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{Creator: target, Name: "  RUDENAME "})
	require.ErrorIs(t, err, types.ErrDisplayNameModerated, "the rejected name stays refused after the case closes")
	_, err = ms.SetDisplayName(f.ctx, &types.MsgSetDisplayName{Creator: target, Name: "Aurora"})
	require.NoError(t, err)

	// A second report keeps the earlier blocked names.
	f.ctx = f.ctx.WithBlockHeight(f.ctx.BlockHeight() + 10)
	_, err = ms.ReportDisplayName(f.ctx, &types.MsgReportDisplayName{Creator: reporter, Target: target, Reason: "y"})
	require.NoError(t, err)
	mod, _ = f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.Equal(t, []string{"rudename"}, mod.BlockedNames)
}

// User decision: appeal resolution and gamification definitions need the
// Operations Committee policy address, never one member; quest XP has a
// compiled ceiling.
func TestSEA23_GamificationAuthorityNeedsPolicyAddress(t *testing.T) {
	member := sdk.AccAddress([]byte("committee_member")).String()
	council := sdk.AccAddress([]byte("council_policy__")).String()
	f := initFixtureWithCommons(t, newMockCommonsKeeperWithCouncil(council, member))
	f.keeper.SetRepKeeper(newMockRepKeeper())
	ctx := sdk.UnwrapSDKContext(f.ctx)
	ms := keeper.NewMsgServerImpl(f.keeper)

	quest := func(authority string, xp uint64) *types.MsgCreateQuest {
		return &types.MsgCreateQuest{Authority: authority, QuestId: fmt.Sprintf("q_%s_%d", authority[len(authority)-4:], xp), Name: "Quest", XpReward: xp}
	}
	_, err := ms.CreateQuest(ctx, quest(member, 10))
	require.ErrorIs(t, err, types.ErrNotAuthorized, "a single committee member is refused")
	_, err = ms.CreateQuest(ctx, quest(council, 10))
	require.NoError(t, err, "the policy address is accepted")

	_, err = ms.CreateTitle(ctx, &types.MsgCreateTitle{Authority: member, TitleId: "t1", Name: "Title"})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = ms.CreateAchievement(ctx, &types.MsgCreateAchievement{Authority: member, AchievementId: "a1", Name: "Ach"})
	require.ErrorIs(t, err, types.ErrNotAuthorized)

	target := sdk.AccAddress([]byte("target__________")).String()
	require.NoError(t, f.keeper.SetDisplayNameModeration(ctx, target, types.DisplayNameModeration{
		Member: target, RejectedName: "x", ModeratedAt: 1, Active: true, AppealChallengeId: "dn_appeal:x:2", AppealedAt: 2,
	}))
	_, err = ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{Authority: member, Member: target, AppealSucceeded: true})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = ms.ResolveDisplayNameAppeal(ctx, &types.MsgResolveDisplayNameAppeal{Authority: council, Member: target, AppealSucceeded: true})
	require.NoError(t, err)

	// Hard ceiling, even when the operational param is raised to it.
	params, _ := f.keeper.Params.Get(ctx)
	params.MaxQuestXpReward = types.HardMaxQuestXpReward
	require.NoError(t, f.keeper.Params.Set(ctx, params))
	_, err = ms.CreateQuest(ctx, quest(council, types.HardMaxQuestXpReward+1))
	require.ErrorIs(t, err, types.ErrQuestXpRewardTooHigh)
	params.MaxQuestXpReward = types.HardMaxQuestXpReward + 1
	require.Error(t, params.Validate(), "max_quest_xp_reward cannot exceed the hard ceiling")
	gs := types.DefaultGenesis()
	gs.QuestMap = []types.Quest{{QuestId: "big", XpReward: types.HardMaxQuestXpReward + 1}}
	require.Error(t, gs.Validate(), "genesis quests are held to the ceiling")
}

// --- SEA-24 / SEA-27 / SEA-28: guilds ------------------------------------------

func TestSEA24_GuildCounterTracksEveryMembershipChange(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	require.NoError(t, k.Season.Set(ctx, types.Season{Number: 1, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	ms := keeper.NewMsgServerImpl(k)
	founder, _ := f.addressCodec.BytesToString(TestAddrFounder)
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	m2, _ := f.addressCodec.BytesToString(TestAddrMember2)
	for _, a := range []sdk.AccAddress{TestAddrFounder, TestAddrMember1, TestAddrMember2} {
		SetupBasicMemberProfile(t, k, ctx, a)
	}
	params, _ := k.Params.Get(ctx)
	params.MinGuildAgeEpochs = 0
	require.NoError(t, k.Params.Set(ctx, params))

	_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{Creator: founder, Name: "phoenix-guild", Description: "d"})
	require.NoError(t, err)
	gid := uint64(1)
	require.Equal(t, uint64(1), k.GetGuildMemberCount(ctx, gid), "the founder counts")

	_, err = ms.JoinGuild(ctx, &types.MsgJoinGuild{Creator: m2, GuildId: gid})
	require.NoError(t, err)
	require.Equal(t, uint64(2), k.GetGuildMemberCount(ctx, gid))

	_, err = ms.InviteToGuild(ctx, &types.MsgInviteToGuild{Creator: founder, GuildId: gid, Invitee: m1})
	require.NoError(t, err)
	_, err = ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{Creator: m1, GuildId: gid})
	require.NoError(t, err)
	require.Equal(t, uint64(3), k.GetGuildMemberCount(ctx, gid), "an accepted invite counts")

	_, err = ms.KickFromGuild(ctx, &types.MsgKickFromGuild{Creator: founder, Member: m2, GuildId: gid})
	require.NoError(t, err)
	require.Equal(t, uint64(2), k.GetGuildMemberCount(ctx, gid), "a kick uncounts")

	_, err = ms.DissolveGuild(ctx, &types.MsgDissolveGuild{Creator: founder, GuildId: gid})
	require.NoError(t, err)
	require.Equal(t, uint64(0), k.GetGuildMemberCount(ctx, gid), "dissolve zeroes the counter")
	p, _ := k.MemberProfile.Get(ctx, m1)
	require.Zero(t, p.GuildId)

	// The invariant agrees with the state.
	msg, broken := keeper.GuildMembershipInvariant(k)(ctx)
	require.False(t, broken, msg)

	// SEA-28: a dissolved guild's name is free again.
	require.NoError(t, k.ValidateGuildName(ctx, "phoenix-guild"))
}

func TestSEA24_AcceptedInvitesRespectCapacity(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	founder, _ := f.addressCodec.BytesToString(TestAddrFounder)
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	SetupBasicMemberProfile(t, k, ctx, TestAddrFounder)
	SetupBasicMemberProfile(t, k, ctx, TestAddrMember1)
	params, _ := k.Params.Get(ctx)
	params.MaxGuildMembers = 1
	params.MinGuildMembers = 1
	require.NoError(t, k.Params.Set(ctx, params))
	_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{Creator: founder, Name: "aurora-guild", Description: "d", InviteOnly: true})
	require.NoError(t, err)
	_, err = ms.InviteToGuild(ctx, &types.MsgInviteToGuild{Creator: founder, GuildId: 1, Invitee: m1})
	require.NoError(t, err)
	_, err = ms.AcceptGuildInvite(ctx, &types.MsgAcceptGuildInvite{Creator: m1, GuildId: 1})
	require.ErrorIs(t, err, types.ErrGuildFull, "the founder already fills a one-member guild")
}

func TestSEA27_GuildJoinCapResetsEachSeason(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	founder, _ := f.addressCodec.BytesToString(TestAddrFounder)
	SetupBasicMemberProfile(t, k, ctx, TestAddrFounder)
	SetupBasicMemberProfile(t, k, ctx, TestAddrMember1)
	require.NoError(t, k.Season.Set(ctx, types.Season{Number: 2, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	params, _ := k.Params.Get(ctx)
	params.GuildHopCooldownEpochs = 0
	require.NoError(t, k.Params.Set(ctx, params))
	_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{Creator: founder, Name: "zenith-guild", Description: "d"})
	require.NoError(t, err)

	// The member used up the cap in season 1.
	require.NoError(t, k.GuildMembership.Set(ctx, m1, types.GuildMembership{
		Member: m1, LeftEpoch: 1, GuildsJoinedThisSeason: uint64(params.MaxGuildsPerSeason), GuildsJoinedSeason: 1,
	}))
	_, err = ms.JoinGuild(ctx, &types.MsgJoinGuild{Creator: m1, GuildId: 1})
	require.NoError(t, err, "a new season starts a new count")
	m, _ := k.GuildMembership.Get(ctx, m1)
	require.Equal(t, uint64(1), m.GuildsJoinedThisSeason)
	require.Equal(t, uint64(2), m.GuildsJoinedSeason)
}

func TestSEA28_UsernameAndGuildNameIndexes(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	m2, _ := f.addressCodec.BytesToString(TestAddrMember2)
	SetupBasicMemberProfile(t, k, ctx, TestAddrMember1)
	SetupBasicMemberProfile(t, k, ctx, TestAddrMember2)

	_, err := ms.SetUsername(ctx, &types.MsgSetUsername{Creator: m1, Username: "phoenix"})
	require.NoError(t, err)
	owner, err := k.UsernameOwner.Get(ctx, "phoenix")
	require.NoError(t, err)
	require.Equal(t, m1, owner)
	_, err = ms.SetUsername(ctx, &types.MsgSetUsername{Creator: m2, Username: "PHOENIX"})
	require.ErrorIs(t, err, types.ErrUsernameAlreadyTaken)

	// Active-quest count reads only the member's Open index entries.
	require.NoError(t, k.MemberQuestProgress.Set(ctx, m1+":a", types.MemberQuestProgress{MemberQuest: m1 + ":a"}))
	require.NoError(t, k.MemberQuestProgress.Set(ctx, m1+":b", types.MemberQuestProgress{MemberQuest: m1 + ":b", Completed: true}))
	require.NoError(t, k.MemberQuestProgress.Set(ctx, m2+":a", types.MemberQuestProgress{MemberQuest: m2 + ":a"}))
	require.Equal(t, uint32(1), k.GetMemberActiveQuestCount(ctx, m1))
}

// --- SEA-25: genesis round trip --------------------------------------------------

func TestSEA25_GuildInvitesAndAbortsRoundTripGenesis(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	for _, id := range []uint64{1, 2} {
		require.NoError(t, k.Guild.Set(ctx, id, types.Guild{Id: id, Name: fmt.Sprintf("guild%d", id), Status: types.GuildStatus_GUILD_STATUS_ACTIVE, PendingInvites: []string{m1}}))
		require.NoError(t, k.GuildInvite.Set(ctx, fmt.Sprintf("%d:%s", id, m1), types.GuildInvite{GuildInvitee: m1, GuildId: id}))
	}
	require.NoError(t, k.GuildSeq.Set(ctx, 2))
	require.NoError(t, k.TransitionAborts.Set(ctx, 3, 2))

	gs, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, gs.Validate(), "two guilds may invite the same member")

	f2 := initFixture(t)
	ctx2 := sdk.UnwrapSDKContext(f2.ctx)
	require.NoError(t, f2.keeper.InitGenesis(ctx2, *gs))
	require.True(t, f2.keeper.HasPendingGuildInvite(ctx2, 1, m1))
	require.True(t, f2.keeper.HasPendingGuildInvite(ctx2, 2, m1))
	aborts, err := f2.keeper.TransitionAborts.Get(ctx2, 3)
	require.NoError(t, err)
	require.Equal(t, uint64(2), aborts, "the abort cap survives export/import")
	msg, broken := keeper.GuildInviteInvariant(f2.keeper)(ctx2)
	require.False(t, broken, msg)
}

// --- SEA-26: quest retention ------------------------------------------------------

func TestSEA26_CleanupKeepsCompletedQuestsAndExpiresSeasonal(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	SetupBasicMemberProfile(t, k, ctx, TestAddrMember1)
	require.NoError(t, k.Quest.Set(ctx, "q1", types.Quest{QuestId: "q1", Name: "q", XpReward: 10, Active: true}))
	require.NoError(t, k.Quest.Set(ctx, "s1", types.Quest{QuestId: "s1", Name: "s", XpReward: 10, Active: true, Season: 1,
		Objectives: []*types.QuestObjective{{Type: types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_VOTES_CAST, Target: 5}}}))
	require.NoError(t, k.Season.Set(ctx, types.Season{Number: 1, StartBlock: 1, EndBlock: 100000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))

	_, err := ms.StartQuest(ctx, &types.MsgStartQuest{Creator: m1, QuestId: "q1"})
	require.NoError(t, err)
	_, err = ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{Creator: m1, QuestId: "q1"})
	require.NoError(t, err)
	_, err = ms.StartQuest(ctx, &types.MsgStartQuest{Creator: m1, QuestId: "s1"})
	require.NoError(t, err)

	require.NoError(t, k.SeasonTransitionState.Set(ctx, types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_CLEANUP, TransitionStart: 99}))
	require.NoError(t, k.BeginBlocker(ctx))
	require.NoError(t, k.SeasonTransitionState.Remove(ctx))

	_, err = ms.StartQuest(ctx, &types.MsgStartQuest{Creator: m1, QuestId: "q1"})
	require.ErrorIs(t, err, types.ErrQuestAlreadyClaimed, "a non-repeatable quest is not re-farmable after CLEANUP")
	has, _ := k.MemberQuestProgress.Has(ctx, m1+":s1")
	require.False(t, has, "open progress on the ending season's quest is dropped")
	s1, _ := k.Quest.Get(ctx, "s1")
	require.False(t, s1.Active, "the ending season's quest is deactivated")

	// A seasonal quest cannot be claimed in another season.
	require.NoError(t, k.Quest.Set(ctx, "s2", types.Quest{QuestId: "s2", Name: "s", XpReward: 10, Active: true, Season: 1}))
	require.NoError(t, k.MemberQuestProgress.Set(ctx, m1+":s2", types.MemberQuestProgress{MemberQuest: m1 + ":s2"}))
	require.NoError(t, k.Season.Set(ctx, types.Season{Number: 2, StartBlock: 1, EndBlock: 100000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	_, err = ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{Creator: m1, QuestId: "s2"})
	require.ErrorIs(t, err, types.ErrQuestSeasonMismatch)
}

// --- SEA-29: migration ------------------------------------------------------------

func TestSEA29_MigrationRefusedWhileModerationOpen(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	from, to := TestAddrTarget, TestAddrMember3
	fromStr, _ := f.addressCodec.BytesToString(from)
	toStr, _ := f.addressCodec.BytesToString(to)
	hooks := keeper.NewMigrationHooks(&k)
	key := reptypes.MigrationPath_MIGRATION_PATH_KEY
	require.NoError(t, k.SetDisplayNameModeration(ctx, fromStr, types.DisplayNameModeration{Member: fromStr, RejectedName: "Bad", ModeratedAt: 1, Active: true}))
	require.Error(t, hooks.BeforeMemberMigration(ctx, from, to, key))

	// Closed: allowed, and the blocked names follow the member.
	require.NoError(t, k.SetDisplayNameModeration(ctx, fromStr, types.DisplayNameModeration{Member: fromStr, RejectedName: "Bad", ModeratedAt: 1, BlockedNames: []string{"bad"}}))
	require.NoError(t, hooks.BeforeMemberMigration(ctx, from, to, key))
	require.NoError(t, hooks.AfterMemberMigration(ctx, from, to, key))
	m, err := k.DisplayNameModeration.Get(ctx, toStr)
	require.NoError(t, err)
	require.Equal(t, []string{"bad"}, m.BlockedNames)
}

// REP-R-101: anyone can file a display-name report, so a veto on RECOVERY or
// COUNCIL let a thief (or any griefer) fail every recovery reveal and burn the
// victim's commitments one by one. Those paths are never blocked; the open
// case, its queue entry and the report stake move to the new address and still
// resolve there.
func TestREPR101_RecoveryAndCouncilIgnoreAnOpenModeration(t *testing.T) {
	for _, path := range []reptypes.MigrationPath{
		reptypes.MigrationPath_MIGRATION_PATH_RECOVERY,
		reptypes.MigrationPath_MIGRATION_PATH_COUNCIL,
	} {
		t.Run(path.String(), func(t *testing.T) {
			f := initFixture(t)
			ctx := sdk.UnwrapSDKContext(f.ctx)
			k := f.keeper
			from, to := TestAddrTarget, TestAddrMember3
			fromStr, _ := f.addressCodec.BytesToString(from)
			toStr, _ := f.addressCodec.BytesToString(to)
			reporter, _ := f.addressCodec.BytesToString(TestAddrMember1)
			hooks := keeper.NewMigrationHooks(&k)

			moderatedAt := ctx.BlockHeight()
			require.NoError(t, k.SetDisplayNameModeration(ctx, fromStr, types.DisplayNameModeration{
				Member: fromStr, RejectedName: "Bad", ModeratedAt: moderatedAt, Active: true,
			}))
			oldStake := fmt.Sprintf("dn:%s:%d", fromStr, moderatedAt)
			require.NoError(t, k.DisplayNameReportStake.Set(ctx, oldStake, types.DisplayNameReportStake{
				ChallengeId: oldStake, Reporter: reporter, Amount: math.NewInt(5),
			}))

			require.NoError(t, hooks.BeforeMemberMigration(ctx, from, to, path))
			require.NoError(t, hooks.AfterMemberMigration(ctx, from, to, path))

			_, err := k.DisplayNameModeration.Get(ctx, fromStr)
			require.Error(t, err, "the record left the old address")
			m, err := k.DisplayNameModeration.Get(ctx, toStr)
			require.NoError(t, err)
			require.True(t, m.Active)
			require.Equal(t, toStr, m.Member)
			has, err := k.ModerationQueue.Has(ctx, collections.Join(moderatedAt, toStr))
			require.NoError(t, err)
			require.True(t, has, "the auto-resolution queue follows the record")
			has, err = k.ModerationQueue.Has(ctx, collections.Join(moderatedAt, fromStr))
			require.NoError(t, err)
			require.False(t, has)
			_, err = k.DisplayNameReportStake.Get(ctx, oldStake)
			require.Error(t, err)
			stake, err := k.DisplayNameReportStake.Get(ctx, fmt.Sprintf("dn:%s:%d", toStr, moderatedAt))
			require.NoError(t, err)
			require.Equal(t, reporter, stake.Reporter)
		})
	}
}

// --- SEA-30 / SEA-31 / SEA-20: retro eligibility and stakes ---------------------

type stubCert struct {
	creator   string
	createdAt int64
}

func (s stubCert) ImpactCertificateInfo(_ context.Context, _, _ uint64) (string, int64, bool) {
	return s.creator, s.createdAt, true
}

func TestSEA30_RetroEligibility(t *testing.T) {
	f := initBeginBlockFixture(t)
	juror := addrStr(t, f, "juror___________")
	f.repKeeper.SetMember(juror, 0, nil, 0)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 2, StartBlock: 500, StartTime: 1_000_000, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_NOMINATION}))

	// An initiative completed before this season started is refused.
	f.repKeeper.Initiatives = map[uint64]reptypes.Initiative{
		1: {Id: 1, Status: reptypes.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, Assignee: juror, CompletedAt: 999_999},
		2: {Id: 2, Status: reptypes.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, Assignee: juror, CompletedAt: 1_000_001},
	}
	require.ErrorIs(t, f.keeper.ValidateContentRef(f.ctx, "rep/initiative/1"), types.ErrInvalidContentRef)
	require.NoError(t, f.keeper.ValidateContentRef(f.ctx, "rep/initiative/2"))

	// A member who served no jury this season is not a jury ref.
	require.ErrorIs(t, f.keeper.ValidateContentRef(f.ctx, "rep/jury/"+juror), types.ErrInvalidContentRef)
	require.NoError(t, f.keeper.MemberProfile.Set(f.ctx, juror, types.MemberProfile{Address: juror, JuryDutiesCompleted: 3, LastJuryDutySeason: 1}))
	require.ErrorIs(t, f.keeper.ValidateContentRef(f.ctx, "rep/jury/"+juror), types.ErrInvalidContentRef, "a jury duty last season does not count")
	require.NoError(t, f.keeper.MemberProfile.Set(f.ctx, juror, types.MemberProfile{Address: juror, JuryDutiesCompleted: 4, LastJuryDutySeason: 2}))
	require.NoError(t, f.keeper.ValidateContentRef(f.ctx, "rep/jury/"+juror))

	// A certificate from an earlier season is refused; a current one names
	// its creator as the author.
	f.keeper.SetArtifactKeeper(stubCert{creator: juror, createdAt: 10})
	require.ErrorIs(t, f.keeper.ValidateContentRef(f.ctx, "artifact/certificate/1/2"), types.ErrInvalidContentRef)
	f.keeper.SetArtifactKeeper(stubCert{creator: juror, createdAt: 1_000_005})
	require.NoError(t, f.keeper.ValidateContentRef(f.ctx, "artifact/certificate/1/2"))
}

func TestSEA30_StakeAuthorBanFailsClosed(t *testing.T) {
	f := initBeginBlockFixture(t)
	activeSeason(t, f, 1, types.SeasonStatus_SEASON_STATUS_NOMINATION)
	nominator := addrStr(t, f, "nominator_______")
	author := addrStr(t, f, "author__________")
	staker := addrStr(t, f, "staker__________")
	for _, a := range []string{nominator, author, staker} {
		f.repKeeper.SetMember(a, 1_000_000_000, nil, 0)
	}
	ms := keeper.NewMsgServerImpl(f.keeper)

	// Author stored at Nominate: the author cannot stake.
	require.NoError(t, f.keeper.SetNomination(f.ctx, types.Nomination{Id: 1, Nominator: nominator, ContentRef: "rep/initiative/9", Season: 1, Author: author,
		TotalStaked: math.LegacyZeroDec(), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec()}))
	_, err := ms.StakeNomination(f.ctx, &types.MsgStakeNomination{Creator: author, NominationId: 1, Amount: "100000000"})
	require.ErrorIs(t, err, types.ErrNominationStakeExists)

	// No stored author and the content no longer resolves: refused.
	require.NoError(t, f.keeper.SetNomination(f.ctx, types.Nomination{Id: 2, Nominator: nominator, ContentRef: "rep/initiative/404", Season: 1,
		TotalStaked: math.LegacyZeroDec(), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec()}))
	_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{Creator: staker, NominationId: 2, Amount: "100000000"})
	require.ErrorIs(t, err, types.ErrContentNotFound, "an unverifiable author fails closed")

	// SEA-31: fractional micro-DREAM is refused.
	_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{Creator: staker, NominationId: 1, Amount: "100000000.5"})
	require.ErrorIs(t, err, types.ErrStakeAmountFractional)

	// SEA-20: a stake stores no conviction; the query computes it live.
	_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{Creator: staker, NominationId: 1, Amount: "100000000"})
	require.NoError(t, err)
	nom, _ := f.keeper.Nomination.Get(f.ctx, 1)
	require.True(t, nom.Conviction.IsZero(), "StakeNomination does not walk the stakes")
	f.ctx = f.ctx.WithBlockHeight(f.ctx.BlockHeight() + 1_000_000)
	resp, err := keeper.NewQueryServerImpl(f.keeper).GetNomination(f.ctx, &types.QueryGetNominationRequest{Id: 1})
	require.NoError(t, err)
	require.True(t, resp.Nomination.Conviction.IsPositive(), "the query shows live conviction")
}

func TestSEA20_NominateDuplicateAndCapByIndex(t *testing.T) {
	f := initBeginBlockFixture(t)
	activeSeason(t, f, 1, types.SeasonStatus_SEASON_STATUS_NOMINATION)
	nominator := addrStr(t, f, "nominator_______")
	f.repKeeper.SetMember(nominator, 0, nil, 0)
	params, _ := f.keeper.Params.Get(f.ctx)
	f.repKeeper.Initiatives = map[uint64]reptypes.Initiative{}
	for i := uint64(1); i <= params.MaxNominationsPerMember+1; i++ {
		f.repKeeper.Initiatives[i] = reptypes.Initiative{Id: i, Status: reptypes.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, Assignee: nominator, CompletedAt: f.ctx.BlockTime().Unix()}
	}
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err := ms.Nominate(f.ctx, &types.MsgNominate{Creator: nominator, ContentRef: "rep/initiative/1"})
	require.NoError(t, err)
	_, err = ms.Nominate(f.ctx, &types.MsgNominate{Creator: nominator, ContentRef: "rep/initiative/1"})
	require.ErrorIs(t, err, types.ErrAlreadyNominated)
	for i := uint64(2); i <= params.MaxNominationsPerMember; i++ {
		_, err = ms.Nominate(f.ctx, &types.MsgNominate{Creator: nominator, ContentRef: fmt.Sprintf("rep/initiative/%d", i)})
		require.NoError(t, err)
	}
	_, err = ms.Nominate(f.ctx, &types.MsgNominate{Creator: nominator, ContentRef: fmt.Sprintf("rep/initiative/%d", params.MaxNominationsPerMember+1)})
	require.ErrorIs(t, err, types.ErrMaxNominationsReached)
	nom, _ := f.keeper.Nomination.Get(f.ctx, 1)
	require.Equal(t, nominator, nom.Author, "the author is stored at Nominate")
}

// --- SEA-22: transition side effects ------------------------------------------------

type failingPoolRep struct {
	*mockRepKeeper
	fail bool
}

func (r *failingPoolRep) InitSeasonalPool(ctx context.Context, season uint64) error {
	if r.fail {
		return errors.New("store error")
	}
	return r.mockRepKeeper.InitSeasonalPool(ctx, season)
}

type failingArchiveRep struct{ *mockRepKeeper }

func (r *failingArchiveRep) ArchiveSeasonalReputation(context.Context, string, math.LegacyDec) (map[string]string, error) {
	return nil, errors.New("store error")
}

func TestSEA22_SeasonalPoolInitRetriedAndArchiveFailureFailsBatch(t *testing.T) {
	f := initBeginBlockFixture(t)
	rep := &failingPoolRep{mockRepKeeper: f.repKeeper, fail: true}
	f.keeper.SetRepKeeper(rep)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_ENDING}))
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_COMPLETE}))
	f.ctx = f.ctx.WithBlockHeight(200)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	season, _ := f.keeper.Season.Get(f.ctx)
	require.Equal(t, uint64(2), season.Number)
	pending, err := f.keeper.PendingSeasonalPoolInit.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), pending)

	rep.fail = false
	f.ctx = f.ctx.WithBlockHeight(201)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	has, _ := f.keeper.PendingSeasonalPoolInit.Has(f.ctx)
	require.False(t, has)
	require.Equal(t, uint64(2), f.repKeeper.SeasonalPoolInitialisedFor)

	// An archive failure fails the batch instead of skipping the member.
	g := initBeginBlockFixture(t)
	g.repKeeper.SetMember(addrStr(t, g, "phoenix_member__"), 0, nil, 0)
	g.keeper.SetRepKeeper(&failingArchiveRep{g.repKeeper})
	require.NoError(t, g.keeper.Season.Set(g.ctx, types.Season{Number: 1, EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_ENDING}))
	require.NoError(t, g.keeper.SeasonTransitionState.Set(g.ctx, types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION}))
	require.NoError(t, g.keeper.BeginBlocker(g.ctx))
	st, _ := g.keeper.SeasonTransitionState.Get(g.ctx)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION, st.Phase)
	require.Empty(t, st.LastProcessed, "the cursor does not advance past the failed member")
	rec, err := g.keeper.TransitionRecoveryState.Get(g.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), rec.FailureCount)
}

// --- SEA-32: objective changes ----------------------------------------------------

func TestSEA32_ObjectiveChangeRestartsProgress(t *testing.T) {
	member := sdk.AccAddress([]byte("committee_member")).String()
	council := sdk.AccAddress([]byte("council_policy__")).String()
	f := initFixtureWithCommons(t, newMockCommonsKeeperWithCouncil(council, member))
	f.keeper.SetRepKeeper(newMockRepKeeper())
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	ms := keeper.NewMsgServerImpl(k)
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	SetupBasicMemberProfile(t, k, ctx, TestAddrMember1)
	vote := types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_VOTES_CAST
	forum := types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_FORUM_HELPFUL
	_, err := ms.CreateQuest(ctx, &types.MsgCreateQuest{Authority: council, QuestId: "q", Name: "Q", XpReward: 10,
		Objectives: []*types.QuestObjective{{Type: vote, Target: 10}, {Type: forum, Target: 1}}})
	require.NoError(t, err)
	_, err = ms.StartQuest(ctx, &types.MsgStartQuest{Creator: m1, QuestId: "q"})
	require.NoError(t, err)
	require.NoError(t, k.OnQuestObjectiveProgress(ctx, TestAddrMember1, vote, 10))

	_, err = ms.UpdateQuest(ctx, &types.MsgUpdateQuest{Authority: council, QuestId: "q", XpReward: 10, Active: true,
		Objectives: []*types.QuestObjective{{Type: forum, Target: 10}, {Type: vote, Target: 1}}})
	require.NoError(t, err)
	_, err = ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{Creator: m1, QuestId: "q"})
	require.ErrorIs(t, err, types.ErrQuestNotComplete, "old VOTE progress does not satisfy the new FORUM objective")

	require.NoError(t, k.OnQuestObjectiveProgress(ctx, TestAddrMember1, vote, 1))
	p, _ := k.MemberQuestProgress.Get(ctx, m1+":q")
	require.Equal(t, []uint64{0, 1}, p.ObjectiveProgress, "progress restarted against the new objectives")
}

// --- SEA-33: vote cap ---------------------------------------------------------------

func TestSEA33_ProposalXPDoesNotConsumeVoteAllowance(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	require.NoError(t, k.Season.Set(ctx, types.Season{Number: 1, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	params, _ := k.Params.Get(ctx)
	params.MaxVoteXpPerEpoch = 1
	require.NoError(t, k.Params.Set(ctx, params))
	SetupBasicMemberProfile(t, k, ctx, TestAddrMember1)
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)

	require.NoError(t, k.GrantXP(ctx, TestAddrMember1, params.XpVoteCast*3, types.XPSourceProposalCreated, "proposal:1"))
	before, _ := k.MemberProfile.Get(ctx, m1)
	require.NoError(t, k.OnVoteCast(ctx, TestAddrMember1, 7))
	after, _ := k.MemberProfile.Get(ctx, m1)
	require.Equal(t, before.SeasonXp+params.XpVoteCast, after.SeasonXp, "the first vote still earns XP")
	tracker, _ := k.EpochXpTracker.Get(ctx, fmt.Sprintf("%s:%d", m1, k.GetCurrentEpoch(ctx)))
	require.Equal(t, uint64(1), tracker.VotesCounted)
}

// --- SEA-34: display-name characters ---------------------------------------------------

func TestSEA34_DisplayNameRejectsControlAndFormatChars(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	for _, bad := range []string{"Pho\nenix", "Aur‮ora", "Zen‍ith", "Zen\x00ith", "Bad\xffName"} {
		require.ErrorIs(t, f.keeper.ValidateDisplayName(ctx, bad), types.ErrDisplayNameInvalidChars, "%q", bad)
	}
	require.NoError(t, f.keeper.ValidateDisplayName(ctx, "Zénith Aurora"))
}

// --- SEA-35: queries ---------------------------------------------------------------------

func TestSEA35_AvailableQuestsAndRetroHistory(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	SetupBasicMemberProfile(t, k, ctx, TestAddrMember1)
	require.NoError(t, k.Season.Set(ctx, types.Season{Number: 2, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	require.NoError(t, k.Quest.Set(ctx, "a", types.Quest{QuestId: "a", Active: true}))
	require.NoError(t, k.Quest.Set(ctx, "b", types.Quest{QuestId: "b", Active: true}))
	require.NoError(t, k.Quest.Set(ctx, "old", types.Quest{QuestId: "old", Active: true, Season: 1}))
	require.NoError(t, k.Quest.Set(ctx, "ach", types.Quest{QuestId: "ach", Active: true, RequiredAchievement: "none"}))
	qs := keeper.NewQueryServerImpl(k)
	resp, err := qs.AvailableQuests(ctx, &types.QueryAvailableQuestsRequest{Member: m1})
	require.NoError(t, err)
	var ids []string
	for _, q := range resp.Quests {
		ids = append(ids, q.QuestId)
	}
	require.Equal(t, []string{"a", "b"}, ids, "every startable quest, judged like MsgStartQuest")

	for i := uint64(1); i <= 3; i++ {
		require.NoError(t, k.RetroRewardRecord.Set(ctx, fmt.Sprintf("1/%d", i), types.RetroRewardRecord{Season: 1, NominationId: i}))
	}
	require.NoError(t, k.RetroRewardRecord.Set(ctx, "10/1", types.RetroRewardRecord{Season: 10, NominationId: 1}))
	h, err := qs.ListRetroRewardHistory(ctx, &types.QueryListRetroRewardHistoryRequest{Season: 1, Pagination: &query.PageRequest{Limit: 2}})
	require.NoError(t, err)
	require.Len(t, h.Records, 2)
	require.NotEmpty(t, h.Pagination.NextKey)
	h, err = qs.ListRetroRewardHistory(ctx, &types.QueryListRetroRewardHistoryRequest{Season: 1, Pagination: &query.PageRequest{Key: h.Pagination.NextKey, Limit: 2}})
	require.NoError(t, err)
	require.Len(t, h.Records, 1, "season 10 is not part of season 1's range")
	require.Empty(t, h.Pagination.NextKey)
}

// --- SEA-36: invariants ---------------------------------------------------------------------

func TestSEA36_InvariantsCatchDrift(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	require.NoError(t, k.Guild.Set(ctx, 1, types.Guild{Id: 1, Name: "g", Status: types.GuildStatus_GUILD_STATUS_ACTIVE}))
	require.NoError(t, k.GuildMembership.Set(ctx, m1, types.GuildMembership{Member: m1, GuildId: 1}))
	require.NoError(t, k.GuildMemberCount.Set(ctx, 1, 5))
	_, broken := keeper.GuildMembershipInvariant(k)(ctx)
	require.True(t, broken, "a drifted member counter is caught")

	require.NoError(t, k.SetNomination(ctx, types.Nomination{Id: 1, Season: 1, TotalStaked: math.LegacyNewDec(5), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec()}))
	require.NoError(t, k.NominationStake.Set(ctx, "1/"+m1, types.NominationStake{NominationId: 1, Staker: m1, Amount: math.LegacyNewDec(7)}))
	_, broken = keeper.NominationStakeInvariant(k)(ctx)
	require.True(t, broken, "total_staked out of step with the stakes is caught")

	require.NoError(t, k.DisplayNameReportStake.Set(ctx, "dn:x:1", types.DisplayNameReportStake{ChallengeId: "dn:x:1", Amount: math.NewInt(1)}))
	_, broken = keeper.DisplayNameModerationInvariant(k)(ctx)
	require.True(t, broken, "an orphan report stake is caught")

	gs := types.DefaultGenesis()
	gs.GuildMembershipMap = []types.GuildMembership{{Member: m1, GuildId: 9}}
	require.Error(t, gs.Validate(), "genesis membership in a missing guild is refused")
}
