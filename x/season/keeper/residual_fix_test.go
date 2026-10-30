package keeper_test

import (
	"errors"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SEA-35: SeasonStats reads running counters maintained where the events
// happen, including initiatives_completed, and the counters survive
// export/import.
func TestSEA35_SeasonStatsRunningCounters(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)
	ms := keeper.NewMsgServerImpl(k)

	m1, m2 := TestAddrMember1, TestAddrMember2
	m1Str := GetAddrString(t, f.addressCodec, m1)
	SetupBasicMemberProfile(t, k, ctx, m1)
	SetupBasicMemberProfile(t, k, ctx, m2)

	stats := func() *types.QuerySeasonStatsResponse {
		resp, err := qs.SeasonStats(ctx, &types.QuerySeasonStatsRequest{})
		require.NoError(t, err)
		return resp
	}
	require.Zero(t, stats().ActiveMembers, "profiles without season XP are not active")

	require.NoError(t, k.GrantXP(ctx, m1, 30, types.XPSourceForumReply, "r1"))
	require.NoError(t, k.GrantXP(ctx, m1, 20, types.XPSourceForumReply, "r2"))
	require.NoError(t, k.GrantXP(ctx, m2, 10, types.XPSourceForumReply, "r3"))
	s := stats()
	require.Equal(t, uint64(2), s.ActiveMembers)
	// The counters equal the sum of season XP held by the profiles.
	p1, _ := k.MemberProfile.Get(ctx, m1Str)
	p2, _ := k.MemberProfile.Get(ctx, GetAddrString(t, f.addressCodec, m2))
	require.Equal(t, p1.SeasonXp+p2.SeasonXp, s.TotalXpEarned)

	// Initiative completions are counted (was always 0).
	require.NoError(t, k.OnInitiativeCompleted(ctx, m1, 7))
	require.NoError(t, k.OnInitiativeCompleted(ctx, m2, 8))
	require.Equal(t, uint64(2), stats().InitiativesCompleted)

	// Zeroing removes the member's season XP from the aggregates.
	before := stats()
	require.NoError(t, k.OnMemberZeroed(ctx, m2))
	after := stats()
	require.Equal(t, before.ActiveMembers-1, after.ActiveMembers)
	p2, _ = k.MemberProfile.Get(ctx, GetAddrString(t, f.addressCodec, m2))
	require.Zero(t, p2.SeasonXp)

	// A claimed quest is counted.
	require.NoError(t, k.Quest.Set(ctx, "q1", types.Quest{QuestId: "q1", Name: "q", XpReward: 5, Active: true}))
	_, err := ms.StartQuest(ctx, &types.MsgStartQuest{Creator: m1Str, QuestId: "q1"})
	require.NoError(t, err)
	_, err = ms.ClaimQuestReward(ctx, &types.MsgClaimQuestReward{Creator: m1Str, QuestId: "q1"})
	require.NoError(t, err)
	require.Equal(t, uint64(1), stats().QuestsCompleted)

	// Guilds: the active counter follows status changes.
	gid := SetupGuild(t, k, ctx, TestAddrFounder, "Aurora Guild", TestGuildDesc)
	require.Equal(t, uint64(1), stats().GuildsActive)
	g, err := k.Guild.Get(ctx, gid)
	require.NoError(t, err)
	g.Status = types.GuildStatus_GUILD_STATUS_FROZEN
	require.NoError(t, k.Guild.Set(ctx, gid, g))
	require.Zero(t, stats().GuildsActive)
	g.Status = types.GuildStatus_GUILD_STATUS_ACTIVE
	require.NoError(t, k.Guild.Set(ctx, gid, g))
	require.Equal(t, uint64(1), stats().GuildsActive)

	// Export/import keeps the counters; the guild counter is rebuilt.
	want := stats()
	gs, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, gs.Validate())
	g2, ctx2 := engineFixture(t)
	require.NoError(t, g2.keeper.InitGenesis(ctx2, *gs))
	got, err := keeper.NewQueryServerImpl(g2.keeper).SeasonStats(ctx2, &types.QuerySeasonStatsRequest{})
	require.NoError(t, err)
	require.Equal(t, want.TotalXpEarned, got.TotalXpEarned)
	require.Equal(t, want.ActiveMembers, got.ActiveMembers)
	require.Equal(t, want.InitiativesCompleted, got.InitiativesCompleted)
	require.Equal(t, want.QuestsCompleted, got.QuestsCompleted)
	require.Equal(t, want.GuildsActive, got.GuildsActive)

	// A past season reads its archived counters.
	gs.SeasonStatsCountersList = append(gs.SeasonStatsCountersList, types.SeasonStatsCounters{Season: 2, TotalXpEarned: 99, InitiativesCompleted: 4})
	gs.SeasonSnapshotMap = append(gs.SeasonSnapshotMap, types.SeasonSnapshot{Season: 2})
	g3, ctx3 := engineFixture(t)
	require.NoError(t, g3.keeper.InitGenesis(ctx3, *gs))
	past, err := keeper.NewQueryServerImpl(g3.keeper).SeasonStats(ctx3, &types.QuerySeasonStatsRequest{Season: 2})
	require.NoError(t, err)
	require.Equal(t, uint64(99), past.TotalXpEarned)
	require.Equal(t, uint64(4), past.InitiativesCompleted)
	require.Zero(t, past.BlocksRemaining)

	// Season XP seeded in genesis without counters is counted on import.
	seeded := types.DefaultGenesis()
	seeded.Season = &types.Season{Number: 1, EndBlock: 100000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}
	seeded.MemberProfileMap = []types.MemberProfile{{Address: m1Str, SeasonXp: 5000, SeasonLevel: 1}}
	g4, ctx4 := engineFixture(t)
	require.NoError(t, g4.keeper.InitGenesis(ctx4, *seeded))
	sd, err := keeper.NewQueryServerImpl(g4.keeper).SeasonStats(ctx4, &types.QuerySeasonStatsRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(5000), sd.TotalXpEarned)
	require.Equal(t, uint64(1), sd.ActiveMembers)

	dup := *gs
	dup.SeasonStatsCountersList = append(dup.SeasonStatsCountersList, types.SeasonStatsCounters{Season: 2})
	require.Error(t, dup.Validate())
}

// SEA-09: a moderation whose automatic resolution fails stays queued with
// backoff and resolves on a later block once the cause is gone.
func TestSEA09_FailedAutoResolveIsRetried(t *testing.T) {
	f := initBeginBlockFixture(t)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	target := sdk.AccAddress([]byte("phoenix_target__")).String()
	reporter := sdk.AccAddress([]byte("aurora_reporter_")).String()
	require.NoError(t, f.keeper.SetDisplayNameModeration(f.ctx, target, types.DisplayNameModeration{
		Member: target, RejectedName: "Bad", ModeratedAt: 100, Active: true,
	}))
	stakeID := fmt.Sprintf("dn:%s:%d", target, 100)
	require.NoError(t, f.keeper.DisplayNameReportStake.Set(f.ctx, stakeID, types.DisplayNameReportStake{
		ChallengeId: stakeID, Reporter: reporter, Amount: math.NewInt(50),
	}))
	params, _ := f.keeper.Params.Get(f.ctx)
	h := 100 + int64(params.DisplayNameAppealPeriodBlocks) + 1
	f.repKeeper.UnlockErr = errors.New("rep unavailable")

	require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(h)))
	mod, err := f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.NoError(t, err)
	require.True(t, mod.Active, "failed attempt commits nothing")
	require.Equal(t, uint32(1), mod.AutoResolveAttempts)
	require.Equal(t, h+keeper.ModerationRetryBaseBlocks, mod.AutoResolveRetryAt)
	has, err := f.keeper.ModerationRetryQueue.Has(f.ctx, collections.Join(mod.AutoResolveRetryAt, target))
	require.NoError(t, err)
	require.True(t, has, "kept queued for a retry, not dropped")
	has, err = f.keeper.ModerationQueue.Has(f.ctx, collections.Join(int64(100), target))
	require.NoError(t, err)
	require.False(t, has)
	_, err = f.keeper.DisplayNameReportStake.Get(f.ctx, stakeID)
	require.NoError(t, err, "stake row kept")

	// Not retried before it is due.
	f.repKeeper.UnlockErr = nil
	require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(h+1)))
	mod, _ = f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.True(t, mod.Active)

	// Due: the retry resolves it.
	require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(mod.AutoResolveRetryAt)))
	mod, _ = f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.False(t, mod.Active)
	require.Zero(t, mod.AutoResolveRetryAt)
	_, err = f.keeper.DisplayNameReportStake.Get(f.ctx, stakeID)
	require.Error(t, err, "stake released")
	has, err = f.keeper.ModerationRetryQueue.Has(f.ctx, collections.Join(h+keeper.ModerationRetryBaseBlocks, target))
	require.NoError(t, err)
	require.False(t, has)
}

// SEA-09: after the last attempt the record leaves the queues, still active
// with its stake, and the manual message resolves it.
func TestSEA09_AutoResolveAttemptsAreBounded(t *testing.T) {
	f := initBeginBlockFixture(t)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	target := sdk.AccAddress([]byte("zenith_target___")).String()
	require.NoError(t, f.keeper.SetDisplayNameModeration(f.ctx, target, types.DisplayNameModeration{
		Member: target, RejectedName: "Bad", ModeratedAt: 100, Active: true,
	}))
	stakeID := fmt.Sprintf("dn:%s:%d", target, 100)
	require.NoError(t, f.keeper.DisplayNameReportStake.Set(f.ctx, stakeID, types.DisplayNameReportStake{
		ChallengeId: stakeID, Reporter: sdk.AccAddress([]byte("aurora_reporter_")).String(), Amount: math.NewInt(50),
	}))
	params, _ := f.keeper.Params.Get(f.ctx)
	h := 100 + int64(params.DisplayNameAppealPeriodBlocks) + 1
	f.repKeeper.UnlockErr = errors.New("rep unavailable")

	for i := 0; i < keeper.MaxModerationAutoResolveAttempts; i++ {
		require.NoError(t, f.keeper.BeginBlocker(f.ctx.WithBlockHeight(h)))
		mod, _ := f.keeper.DisplayNameModeration.Get(f.ctx, target)
		if mod.AutoResolveRetryAt > 0 {
			h = mod.AutoResolveRetryAt
		}
	}
	mod, _ := f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.True(t, mod.Active)
	require.Equal(t, uint32(keeper.MaxModerationAutoResolveAttempts), mod.AutoResolveAttempts)
	require.Zero(t, mod.AutoResolveRetryAt, "out of the retry queue")
	n := 0
	require.NoError(t, f.keeper.ModerationRetryQueue.Walk(f.ctx, nil, func(collections.Pair[int64, string]) (bool, error) { n++; return false, nil }))
	require.NoError(t, f.keeper.ModerationQueue.Walk(f.ctx, nil, func(collections.Pair[int64, string]) (bool, error) { n++; return false, nil }))
	require.Zero(t, n)
	_, err := f.keeper.DisplayNameReportStake.Get(f.ctx, stakeID)
	require.NoError(t, err, "stake still accounted for")

	// The manual path resolves it once the cause is gone.
	f.repKeeper.UnlockErr = nil
	require.NoError(t, f.keeper.ResolveUnappealedModerationInternal(f.ctx.WithBlockHeight(h+1), target))
	mod, _ = f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.False(t, mod.Active)
}
