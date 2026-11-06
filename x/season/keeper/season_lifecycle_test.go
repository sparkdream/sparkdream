package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// seedLifecycleSeason seeds an ACTIVE season 1 ending at block 100 with
// three profiled rep members, two nominations and one open quest progress,
// and a batch size that walks every phase in one block.
func seedLifecycleSeason(t *testing.T, f *beginBlockFixture) {
	t.Helper()
	params := types.DefaultParams()
	params.TransitionBatchSize = 100
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{
		Number: 1, Name: "Phoenix", Theme: "Rebirth", StartBlock: 1, EndBlock: 100,
		OriginalEndBlock: 90, ExtensionsCount: 1, TotalExtensionEpochs: 1,
		Status: types.SeasonStatus_SEASON_STATUS_ACTIVE,
	}))
	for i, addr := range []string{"cosmos1aurora", "cosmos1zenith", "cosmos1nova"} {
		require.NoError(t, f.keeper.MemberProfile.Set(f.ctx, addr, types.MemberProfile{
			Address: addr, SeasonXp: uint64(100 * (i + 1)), SeasonLevel: 1, LifetimeXp: 100,
		}))
		f.repKeeper.SetMember(addr, 1000, map[string]string{"coding": "10.0"}, 0)
	}
	for id := uint64(1); id <= 2; id++ {
		require.NoError(t, f.keeper.SetNomination(f.ctx, types.Nomination{
			Id: id, Season: 1, Nominator: "cosmos1aurora",
			ContentRef: fmt.Sprintf("blog/post/%d", id), TotalStaked: math.LegacyZeroDec(),
			Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec(),
		}))
	}
	require.NoError(t, f.keeper.MemberQuestProgress.Set(f.ctx, "cosmos1aurora:q1", types.MemberQuestProgress{
		MemberQuest: "cosmos1aurora:q1",
	}))
}

// TestSeasonStatusFollowsTransitionPhases drives a full transition and
// checks the season status is ENDING in the nomination phases, MAINTENANCE
// from SNAPSHOT through RESET_XP, ENDING again from TITLES, and that each
// phase starts with total_count equal to the number of items it walks.
func TestSeasonStatusFollowsTransitionPhases(t *testing.T) {
	f := initBeginBlockFixture(t)
	seedLifecycleSeason(t, f)

	f.ctx = f.ctx.WithBlockHeight(100)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS, state.Phase)
	require.Equal(t, uint64(2), state.TotalCount, "retro rewards walks the season's nominations")
	require.False(t, state.MaintenanceMode)
	season, err := f.keeper.Season.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ENDING, season.Status)

	steps := []struct {
		phase       types.TransitionPhase
		total       uint64
		status      types.SeasonStatus
		maintenance bool
	}{
		{types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES, 2, types.SeasonStatus_SEASON_STATUS_ENDING, false},
		{types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT, 3, types.SeasonStatus_SEASON_STATUS_MAINTENANCE, true},
		{types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION, 3, types.SeasonStatus_SEASON_STATUS_MAINTENANCE, true},
		{types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION, 3, types.SeasonStatus_SEASON_STATUS_MAINTENANCE, true},
		{types.TransitionPhase_TRANSITION_PHASE_RESET_XP, 3, types.SeasonStatus_SEASON_STATUS_MAINTENANCE, true},
		{types.TransitionPhase_TRANSITION_PHASE_TITLES, 3, types.SeasonStatus_SEASON_STATUS_ENDING, false},
		{types.TransitionPhase_TRANSITION_PHASE_CLEANUP, 1, types.SeasonStatus_SEASON_STATUS_ENDING, false},
		{types.TransitionPhase_TRANSITION_PHASE_COMPLETE, 0, types.SeasonStatus_SEASON_STATUS_ENDING, false},
	}
	for i, step := range steps {
		f.ctx = f.ctx.WithBlockHeight(int64(101 + i))
		require.NoError(t, f.keeper.BeginBlocker(f.ctx))
		state, err = f.keeper.SeasonTransitionState.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, step.phase, state.Phase, "step %d", i)
		require.Equal(t, step.total, state.TotalCount, "total_count at %s", step.phase)
		require.Equal(t, uint64(0), state.ProcessedCount, "cursor reset at %s", step.phase)
		require.Equal(t, step.maintenance, state.MaintenanceMode, "maintenance flag at %s", step.phase)
		require.Equal(t, step.maintenance, f.keeper.IsInMaintenanceMode(f.ctx))
		season, err = f.keeper.Season.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, step.status, season.Status, "status at %s", step.phase)
	}

	// Finalisation archives season 1 as COMPLETED and starts season 2.
	f.ctx = f.ctx.WithBlockHeight(110)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	live, err := f.keeper.Season.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), live.Number)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ACTIVE, live.Status)

	past, err := f.keeper.SeasonHistory.Get(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_COMPLETED, past.Status)
	require.Equal(t, int64(100), past.EndBlock)
	require.Equal(t, int64(90), past.OriginalEndBlock)
	require.Equal(t, uint64(1), past.ExtensionsCount)
	require.Equal(t, "Phoenix", past.Name)
	require.Equal(t, "Rebirth", past.Theme)
	_, err = f.keeper.SeasonHistory.Get(f.ctx, 2)
	require.Error(t, err, "the live season is not in the history")
}

// TestSkipPhaseSyncsStatusAndTotalCount: MsgSkipTransitionPhase moves the
// status with the phase, in both directions, and leaves total_count at 0
// (unknown) rather than counting the new phase's items at gas (SEA-201).
func TestSkipPhaseSyncsStatusAndTotalCount(t *testing.T) {
	f := initBeginBlockFixture(t)
	seedLifecycleSeason(t, f)
	ms := keeper.NewMsgServerImpl(f.keeper)
	authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	// RETURN_NOMINATION_STAKES -> SNAPSHOT enters maintenance.
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_ENDING}))
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{
		Phase: types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES, ProcessedCount: 1, LastProcessed: "x", TotalCount: 2,
	}))
	_, err = ms.SkipTransitionPhase(f.ctx, &types.MsgSkipTransitionPhase{Authority: authority})
	require.NoError(t, err)
	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT, state.Phase)
	require.True(t, state.MaintenanceMode)
	require.Equal(t, uint64(0), state.TotalCount, "skip does not count the new phase's items (SEA-201)")
	require.Equal(t, uint64(0), state.ProcessedCount)
	season, err := f.keeper.Season.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_MAINTENANCE, season.Status)

	// RESET_XP -> TITLES leaves maintenance.
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{
		Phase: types.TransitionPhase_TRANSITION_PHASE_RESET_XP, MaintenanceMode: true,
	}))
	_, err = ms.SkipTransitionPhase(f.ctx, &types.MsgSkipTransitionPhase{Authority: authority})
	require.NoError(t, err)
	state, err = f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_TITLES, state.Phase)
	require.False(t, state.MaintenanceMode)
	season, err = f.keeper.Season.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ENDING, season.Status)
}

// TestRetryKeepsTotalCount: MsgRetrySeasonTransition restarts the phase's
// cursor but keeps the total_count the BeginBlocker counted, instead of
// walking the phase's item set at gas inside the tx (SEA-201).
func TestRetryKeepsTotalCount(t *testing.T) {
	f := initBeginBlockFixture(t)
	seedLifecycleSeason(t, f)
	ms := keeper.NewMsgServerImpl(f.keeper)
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{
		Phase: types.TransitionPhase_TRANSITION_PHASE_TITLES, ProcessedCount: 2, LastProcessed: "cosmos1nova", TotalCount: 99,
	}))
	require.NoError(t, f.keeper.TransitionRecoveryState.Set(f.ctx, types.TransitionRecoveryState{RecoveryMode: true, FailureCount: 3}))
	authority, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)
	_, err = ms.RetrySeasonTransition(f.ctx, &types.MsgRetrySeasonTransition{Authority: authority})
	require.NoError(t, err)
	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(99), state.TotalCount, "retry keeps the count (SEA-201)")
	require.Equal(t, uint64(0), state.ProcessedCount)
	require.Equal(t, "", state.LastProcessed)
}

// TestResetReputationTotalCountWalksRepMembers: with x/rep wired the
// RESET_REPUTATION count is rep's member set, which may exceed the season
// profiles.
func TestResetReputationTotalCountWalksRepMembers(t *testing.T) {
	f := initBeginBlockFixture(t)
	seedLifecycleSeason(t, f)
	f.repKeeper.SetMember("cosmos1noprofile", 1, nil, 0)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_MAINTENANCE}))
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{
		Phase: types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION, MaintenanceMode: true,
	}))
	f.ctx = f.ctx.WithBlockHeight(101)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION, state.Phase)
	require.Equal(t, uint64(4), state.TotalCount)
}

// TestSeasonHistoryPrunedByRetention: the CLEANUP phase drops completed
// seasons older than snapshot_retention_seasons, so that after the ending
// season is archived the history holds at most that many records.
func TestSeasonHistoryPrunedByRetention(t *testing.T) {
	f := initBeginBlockFixture(t)
	params := types.DefaultParams()
	params.TransitionBatchSize = 100
	params.SnapshotRetentionSeasons = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	for n := uint64(2); n <= 4; n++ {
		require.NoError(t, f.keeper.SeasonHistory.Set(f.ctx, n, types.Season{
			Number: n, Status: types.SeasonStatus_SEASON_STATUS_COMPLETED,
		}))
	}
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 5, EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_ENDING}))
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{
		Phase: types.TransitionPhase_TRANSITION_PHASE_CLEANUP,
	}))
	f.ctx = f.ctx.WithBlockHeight(101)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx)) // CLEANUP
	var kept []uint64
	require.NoError(t, f.keeper.SeasonHistory.Walk(f.ctx, nil, func(n uint64, _ types.Season) (bool, error) {
		kept = append(kept, n)
		return false, nil
	}))
	require.Equal(t, []uint64{4}, kept)

	f.ctx = f.ctx.WithBlockHeight(102)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx)) // COMPLETE
	kept = nil
	require.NoError(t, f.keeper.SeasonHistory.Walk(f.ctx, nil, func(n uint64, _ types.Season) (bool, error) {
		kept = append(kept, n)
		return false, nil
	}))
	require.Equal(t, []uint64{4, 5}, kept)
	live, err := f.keeper.Season.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(6), live.Number)
}

// TestQuerySeasonByNumberReadsHistory: a completed season is answered from
// its stored record; a season older than the history falls back to the
// snapshot-derived shape; an unknown one is NotFound.
func TestQuerySeasonByNumberReadsHistory(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	qs := keeper.NewQueryServerImpl(k)

	require.NoError(t, k.Season.Set(ctx, types.Season{Number: 4, Name: "Live", Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	require.NoError(t, k.SeasonHistory.Set(ctx, 3, types.Season{
		Number: 3, Name: "Aurora", Theme: "Dawn", StartBlock: 500, EndBlock: 900,
		Status: types.SeasonStatus_SEASON_STATUS_COMPLETED,
	}))
	require.NoError(t, k.SeasonSnapshot.Set(ctx, 1, types.SeasonSnapshot{Season: 1, SnapshotBlock: 120}))

	resp, err := qs.SeasonByNumber(ctx, &types.QuerySeasonByNumberRequest{Number: 3})
	require.NoError(t, err)
	require.Equal(t, "Aurora", resp.Name)
	require.Equal(t, "Dawn", resp.Theme)
	require.Equal(t, int64(500), resp.StartBlock)
	require.Equal(t, int64(900), resp.EndBlock)
	require.Equal(t, uint64(types.SeasonStatus_SEASON_STATUS_COMPLETED), resp.Status)

	resp, err = qs.SeasonByNumber(ctx, &types.QuerySeasonByNumberRequest{Number: 1})
	require.NoError(t, err)
	require.Equal(t, "", resp.Name)
	require.Equal(t, int64(120), resp.EndBlock)
	require.Equal(t, uint64(types.SeasonStatus_SEASON_STATUS_COMPLETED), resp.Status)

	_, err = qs.SeasonByNumber(ctx, &types.QuerySeasonByNumberRequest{Number: 2})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

// TestGenesisSeasonHistoryRoundTrip exports and re-imports the history.
func TestGenesisSeasonHistoryRoundTrip(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	gs := types.DefaultGenesis()
	gs.Season = &types.Season{Number: 3, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}
	gs.SeasonHistoryList = []types.Season{
		{Number: 1, Name: "Phoenix", EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_COMPLETED},
		{Number: 2, Name: "Aurora", EndBlock: 200, ExtensionsCount: 2, Status: types.SeasonStatus_SEASON_STATUS_COMPLETED},
	}
	require.NoError(t, gs.Validate())
	require.NoError(t, f.keeper.InitGenesis(ctx, *gs))

	got, err := f.keeper.SeasonHistory.Get(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, "Aurora", got.Name)
	require.Equal(t, uint64(2), got.ExtensionsCount)

	exported, err := f.keeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, gs.SeasonHistoryList, exported.SeasonHistoryList)
	require.NoError(t, exported.Validate())
}
