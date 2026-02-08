package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	module "sparkdream/x/season/module"
	"sparkdream/x/season/types"
)

// beginBlockFixture is a test fixture specifically for BeginBlock tests
type beginBlockFixture struct {
	ctx          sdk.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	repKeeper    *mockRepKeeper
}

// initBeginBlockFixture creates a fixture with a mock rep keeper for BeginBlock tests
func initBeginBlockFixture(t *testing.T) *beginBlockFixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	mockRep := newMockRepKeeper()

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		nil,     // bankKeeper
		mockRep, // repKeeper
		nil,     // nameKeeper
		nil,     // commonsKeeper
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &beginBlockFixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		repKeeper:    mockRep,
	}
}

// setupSeasonTransition sets up a season that's ready to transition
func setupSeasonTransition(t *testing.T, f *beginBlockFixture) {
	t.Helper()

	// Create a season that has ended
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100, // End at block 100
		Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
	}
	err := f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Advance to a block past the end
	f.ctx = f.ctx.WithBlockHeight(101)
}

// setupMemberProfile creates a member profile for testing
func setupMemberProfile(t *testing.T, f *beginBlockFixture, addr string, xp uint64, level uint64) {
	t.Helper()

	profile := types.MemberProfile{
		Address:     addr,
		DisplayName: "Test User",
		SeasonXp:    xp,
		SeasonLevel: level,
		LifetimeXp:  xp,
	}
	err := f.keeper.MemberProfile.Set(f.ctx, addr, profile)
	require.NoError(t, err)
}

func TestBeginBlocker_NoSeason(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Call BeginBlocker without a season - should not error
	err := f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)
}

func TestBeginBlocker_SeasonNotEnded(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Create an active season that hasn't ended yet
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   1000, // Far in the future
		Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
	}
	err := f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	f.ctx = f.ctx.WithBlockHeight(100)

	// Call BeginBlocker - should not start transition
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify no transition state
	_, err = f.keeper.SeasonTransitionState.Get(f.ctx)
	require.Error(t, err, "transition state should not exist")
}

func TestBeginBlocker_StartsTransition(t *testing.T) {
	f := initBeginBlockFixture(t)
	setupSeasonTransition(t, f)

	// Call BeginBlocker - should start transition
	err := f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify transition started
	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT, state.Phase)

	// Verify season status changed to ENDING
	season, err := f.keeper.Season.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ENDING, season.Status)
}

func TestProcessSnapshotPhase_WithRepKeeper(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Setup season
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
	}
	err := f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Use proper SDK addresses that can be encoded/decoded by the address codec
	testAddr1 := sdk.AccAddress([]byte("member1_________"))
	testAddr2 := sdk.AccAddress([]byte("member2_________"))
	addr1, err := f.addressCodec.BytesToString(testAddr1)
	require.NoError(t, err)
	addr2, err := f.addressCodec.BytesToString(testAddr2)
	require.NoError(t, err)

	// Setup members in mock rep keeper using the bech32 encoded addresses
	f.repKeeper.SetMember(addr1, 1000, map[string]string{
		"backend":  "100.5",
		"frontend": "50.0",
	}, 5)
	f.repKeeper.SetMember(addr2, 500, map[string]string{
		"design": "75.25",
	}, 2)

	// Setup member profiles
	setupMemberProfile(t, f, addr1, 1000, 5)
	setupMemberProfile(t, f, addr2, 500, 3)

	// Setup transition state
	transitionState := types.SeasonTransitionState{
		Phase:          types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
		ProcessedCount: 0,
	}
	err = f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
	require.NoError(t, err)

	// Process the snapshot phase by calling BeginBlocker
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify snapshots were created with reputation data
	snapshot1, err := f.keeper.MemberSeasonSnapshot.Get(f.ctx, fmt.Sprintf("%d/%s", season.Number, addr1))
	require.NoError(t, err)
	require.Equal(t, int64(1000), snapshot1.FinalDreamBalance.Int64())
	require.Equal(t, "100.5", snapshot1.FinalReputation["backend"])
	require.Equal(t, "50.0", snapshot1.FinalReputation["frontend"])
	require.Equal(t, uint64(5), snapshot1.InitiativesCompleted)

	snapshot2, err := f.keeper.MemberSeasonSnapshot.Get(f.ctx, fmt.Sprintf("%d/%s", season.Number, addr2))
	require.NoError(t, err)
	require.Equal(t, int64(500), snapshot2.FinalDreamBalance.Int64())
	require.Equal(t, "75.25", snapshot2.FinalReputation["design"])
	require.Equal(t, uint64(2), snapshot2.InitiativesCompleted)
}

func TestProcessArchiveReputationPhase_WithRepKeeper(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Setup season
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
	}
	err := f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Setup member in mock rep keeper
	addr := "cosmos1testaddr"
	f.repKeeper.SetMember(addr, 1000, map[string]string{
		"backend": "200.0",
	}, 3)

	// Setup member profile
	setupMemberProfile(t, f, addr, 500, 2)

	// Create an existing snapshot (from previous snapshot phase)
	snapshotKey := fmt.Sprintf("%d/%s", season.Number, addr)
	existingSnapshot := types.MemberSeasonSnapshot{
		SeasonAddress:   snapshotKey,
		FinalReputation: make(map[string]string), // Empty initially
	}
	err = f.keeper.MemberSeasonSnapshot.Set(f.ctx, snapshotKey, existingSnapshot)
	require.NoError(t, err)

	// Setup transition state at ARCHIVE_REPUTATION phase
	transitionState := types.SeasonTransitionState{
		Phase:          types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION,
		ProcessedCount: 0,
	}
	err = f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
	require.NoError(t, err)

	// Process the archive phase
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify snapshot was updated with reputation from x/rep
	snapshot, err := f.keeper.MemberSeasonSnapshot.Get(f.ctx, snapshotKey)
	require.NoError(t, err)
	require.Equal(t, "200.0", snapshot.FinalReputation["backend"])
}

func TestProcessResetReputationPhase_CallsArchive(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Setup season
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
	}
	err := f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Setup members in mock rep keeper with reputation
	addr1 := "cosmos1addr1"
	addr2 := "cosmos1addr2"

	f.repKeeper.SetMember(addr1, 1000, map[string]string{
		"backend": "150.0",
	}, 2)
	f.repKeeper.SetMember(addr2, 500, map[string]string{
		"frontend": "100.0",
		"design":   "50.0",
	}, 1)

	// Setup member profiles
	setupMemberProfile(t, f, addr1, 1000, 5)
	setupMemberProfile(t, f, addr2, 500, 3)

	// Setup transition state at RESET_REPUTATION phase
	transitionState := types.SeasonTransitionState{
		Phase:          types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION,
		ProcessedCount: 0,
	}
	err = f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
	require.NoError(t, err)

	// Clear any previous archive calls
	f.repKeeper.ArchiveCallCount = 0
	f.repKeeper.LastArchivedAddresses = []string{}

	// Process the reset phase
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify ArchiveSeasonalReputation was called for each member
	require.Equal(t, 2, f.repKeeper.ArchiveCallCount, "should call archive for each member")
	require.Contains(t, f.repKeeper.LastArchivedAddresses, addr1)
	require.Contains(t, f.repKeeper.LastArchivedAddresses, addr2)

	// Verify seasonal scores were cleared (mock behavior)
	require.Empty(t, f.repKeeper.ReputationScores[addr1])
	require.Empty(t, f.repKeeper.ReputationScores[addr2])

	// Verify lifetime reputation was updated
	require.Equal(t, "150.0", f.repKeeper.LifetimeReputation[addr1]["backend"])
	require.Equal(t, "100.0", f.repKeeper.LifetimeReputation[addr2]["frontend"])
	require.Equal(t, "50.0", f.repKeeper.LifetimeReputation[addr2]["design"])
}

func TestProcessResetReputationPhase_BatchProcessing(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Set small batch size
	params := types.DefaultParams()
	params.TransitionBatchSize = 2
	err := f.keeper.Params.Set(f.ctx, params)
	require.NoError(t, err)

	// Setup season
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
	}
	err = f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Setup 5 members
	for i := 0; i < 5; i++ {
		addr := fmt.Sprintf("cosmos1addr%d", i)
		f.repKeeper.SetMember(addr, 1000, map[string]string{"tag": "100.0"}, 1)
		setupMemberProfile(t, f, addr, 100, 1)
	}

	// Setup transition state at RESET_REPUTATION phase
	transitionState := types.SeasonTransitionState{
		Phase:          types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION,
		ProcessedCount: 0,
	}
	err = f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
	require.NoError(t, err)

	// First batch should process 2
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	// Should still be in RESET_REPUTATION phase (5 members, batch of 2)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION, state.Phase)
	require.Equal(t, uint64(2), state.ProcessedCount)

	// Second batch
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)
	state, err = f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), state.ProcessedCount)

	// Third batch completes the phase
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)
	state, err = f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	// Should have moved to next phase (RESET_XP)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RESET_XP, state.Phase)
}

func TestSnapshotPhase_NoRepKeeper(t *testing.T) {
	// Create fixture without rep keeper
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		nil, // bankKeeper
		nil, // NO repKeeper
		nil, // nameKeeper
		nil, // commonsKeeper
	)

	err := k.Params.Set(ctx, types.DefaultParams())
	require.NoError(t, err)

	// Setup season and member
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
	}
	err = k.Season.Set(ctx, season)
	require.NoError(t, err)

	addr := "cosmos1testaddr"
	profile := types.MemberProfile{
		Address:     addr,
		DisplayName: "Test User",
		SeasonXp:    500,
		SeasonLevel: 3,
	}
	err = k.MemberProfile.Set(ctx, addr, profile)
	require.NoError(t, err)

	// Setup transition state at SNAPSHOT phase
	transitionState := types.SeasonTransitionState{
		Phase:          types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
		ProcessedCount: 0,
	}
	err = k.SeasonTransitionState.Set(ctx, transitionState)
	require.NoError(t, err)

	// Process should work even without rep keeper
	err = k.BeginBlocker(ctx)
	require.NoError(t, err)

	// Verify snapshot was created with default values
	snapshot, err := k.MemberSeasonSnapshot.Get(ctx, fmt.Sprintf("%d/%s", season.Number, addr))
	require.NoError(t, err)
	require.Equal(t, int64(0), snapshot.FinalDreamBalance.Int64()) // Default when no rep keeper
	require.Empty(t, snapshot.FinalReputation)                     // Empty when no rep keeper
	require.Equal(t, uint64(0), snapshot.InitiativesCompleted)     // Default when no rep keeper
	require.Equal(t, uint64(500), snapshot.XpEarned)               // From profile
	require.Equal(t, uint64(3), snapshot.SeasonLevel)              // From profile
}

func TestProcessResetXPPhase_BatchProcessing(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Set small batch size
	params := types.DefaultParams()
	params.TransitionBatchSize = 2
	err := f.keeper.Params.Set(f.ctx, params)
	require.NoError(t, err)

	// Setup season
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
	}
	err = f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Setup 5 members with XP
	for i := 0; i < 5; i++ {
		addr := fmt.Sprintf("cosmos1addr%d", i)
		setupMemberProfile(t, f, addr, uint64((i+1)*100), uint64(i+1))
	}

	// Setup transition state at RESET_XP phase
	transitionState := types.SeasonTransitionState{
		Phase:          types.TransitionPhase_TRANSITION_PHASE_RESET_XP,
		ProcessedCount: 0,
	}
	err = f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
	require.NoError(t, err)

	// First batch should process 2 members
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RESET_XP, state.Phase)
	require.Equal(t, uint64(2), state.ProcessedCount)
	require.NotEmpty(t, state.LastProcessed, "LastProcessed should be set after first batch")

	// Second batch should process 2 more
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	state, err = f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RESET_XP, state.Phase)
	require.Equal(t, uint64(4), state.ProcessedCount)

	// Third batch should process the last 1 and complete the phase
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	state, err = f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	// Should have moved to next phase (TITLES)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_TITLES, state.Phase)
	require.Equal(t, uint64(0), state.ProcessedCount, "ProcessedCount should reset on phase advance")

	// Verify XP was actually reset for all 5 members
	for i := 0; i < 5; i++ {
		addr := fmt.Sprintf("cosmos1addr%d", i)
		profile, err := f.keeper.MemberProfile.Get(f.ctx, addr)
		require.NoError(t, err)
		require.Equal(t, uint64(0), profile.SeasonXp, "SeasonXp should be reset for %s", addr)
		require.Equal(t, uint64(1), profile.SeasonLevel, "SeasonLevel should be reset to 1 for %s", addr)
		require.Equal(t, uint64((i+1)*100), profile.LifetimeXp, "LifetimeXp should be preserved for %s", addr)
	}
}

func TestProcessResetXPPhase_CreatesSnapshots(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Setup season
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
	}
	err := f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Setup members with XP
	addr1 := "cosmos1member1"
	addr2 := "cosmos1member2"
	setupMemberProfile(t, f, addr1, 500, 3)
	setupMemberProfile(t, f, addr2, 1000, 5)

	// Setup transition state at RESET_XP phase
	transitionState := types.SeasonTransitionState{
		Phase:          types.TransitionPhase_TRANSITION_PHASE_RESET_XP,
		ProcessedCount: 0,
	}
	err = f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
	require.NoError(t, err)

	// Process phase (default batch size is large enough for 2 members)
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify MemberSeasonSnapshot was created for members with XP > 0
	snapshot1, err := f.keeper.MemberSeasonSnapshot.Get(f.ctx, fmt.Sprintf("1/%s", addr1))
	require.NoError(t, err)
	require.Equal(t, uint64(500), snapshot1.XpEarned)
	require.Equal(t, uint64(3), snapshot1.SeasonLevel)

	snapshot2, err := f.keeper.MemberSeasonSnapshot.Get(f.ctx, fmt.Sprintf("1/%s", addr2))
	require.NoError(t, err)
	require.Equal(t, uint64(1000), snapshot2.XpEarned)
	require.Equal(t, uint64(5), snapshot2.SeasonLevel)
}

func TestBeginBlocker_CompletePhaseFinalizes(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Setup season in ENDING state
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		Theme:      "Testing",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
	}
	err := f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Set next season info
	nextInfo := types.NextSeasonInfo{
		Name:  "Season Two",
		Theme: "New Beginnings",
	}
	err = f.keeper.NextSeasonInfo.Set(f.ctx, nextInfo)
	require.NoError(t, err)

	// Setup transition state at COMPLETE phase
	transitionState := types.SeasonTransitionState{
		Phase:           types.TransitionPhase_TRANSITION_PHASE_COMPLETE,
		TransitionStart: 100,
	}
	err = f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
	require.NoError(t, err)

	// Set block height for the new season start
	f.ctx = f.ctx.WithBlockHeight(150)

	// Call BeginBlocker - should finalize and create Season 2
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify new season was created
	newSeason, err := f.keeper.Season.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), newSeason.Number)
	require.Equal(t, "Season Two", newSeason.Name)
	require.Equal(t, "New Beginnings", newSeason.Theme)
	require.Equal(t, int64(150), newSeason.StartBlock)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ACTIVE, newSeason.Status)

	// Verify transition state was cleared
	_, err = f.keeper.SeasonTransitionState.Get(f.ctx)
	require.Error(t, err, "transition state should be removed after finalization")

	// Verify next season info was cleared
	_, err = f.keeper.NextSeasonInfo.Get(f.ctx)
	require.Error(t, err, "next season info should be cleared after finalization")
}

func TestBeginBlocker_CompletePhaseFinalizes_DefaultName(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Setup season in ENDING state
	season := types.Season{
		Number:     1,
		Name:       "Test Season",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ENDING,
	}
	err := f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Don't set next season info - should get default name

	// Setup transition state at COMPLETE phase
	transitionState := types.SeasonTransitionState{
		Phase:           types.TransitionPhase_TRANSITION_PHASE_COMPLETE,
		TransitionStart: 100,
	}
	err = f.keeper.SeasonTransitionState.Set(f.ctx, transitionState)
	require.NoError(t, err)

	f.ctx = f.ctx.WithBlockHeight(200)

	// Call BeginBlocker - should finalize with default name
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	newSeason, err := f.keeper.Season.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), newSeason.Number)
	require.Equal(t, "Season 2", newSeason.Name, "should use default name when NextSeasonInfo is empty")
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ACTIVE, newSeason.Status)
}

func TestFullTransitionFlow(t *testing.T) {
	f := initBeginBlockFixture(t)

	// Set batch size to process all members in one go
	params := types.DefaultParams()
	params.TransitionBatchSize = 100
	err := f.keeper.Params.Set(f.ctx, params)
	require.NoError(t, err)

	// Setup season that ends at block 100
	season := types.Season{
		Number:     1,
		Name:       "Season One",
		StartBlock: 0,
		EndBlock:   100,
		Status:     types.SeasonStatus_SEASON_STATUS_ACTIVE,
	}
	err = f.keeper.Season.Set(f.ctx, season)
	require.NoError(t, err)

	// Setup next season info
	nextInfo := types.NextSeasonInfo{
		Name:  "Season Two",
		Theme: "Adventure",
	}
	err = f.keeper.NextSeasonInfo.Set(f.ctx, nextInfo)
	require.NoError(t, err)

	// Setup 3 members with XP and titles
	members := []struct {
		addr  string
		xp    uint64
		level uint64
	}{
		{"cosmos1alice", 500, 3},
		{"cosmos1bob", 200, 2},
		{"cosmos1carol", 100, 1},
	}
	for _, m := range members {
		profile := types.MemberProfile{
			Address:        m.addr,
			DisplayName:    m.addr,
			SeasonXp:       m.xp,
			SeasonLevel:    m.level,
			LifetimeXp:     m.xp,
			UnlockedTitles: []string{"newcomer"},
		}
		err = f.keeper.MemberProfile.Set(f.ctx, m.addr, profile)
		require.NoError(t, err)

		f.repKeeper.SetMember(m.addr, int64(m.xp)*1000, map[string]string{"coding": "50.0"}, 1)
	}

	// Block 100: trigger transition
	f.ctx = f.ctx.WithBlockHeight(100)
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify transition started
	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT, state.Phase)

	// Verify season is ENDING
	s, _ := f.keeper.Season.Get(f.ctx)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ENDING, s.Status)

	// Process remaining phases one block at a time
	// Each BeginBlocker call processes one phase (batch_size=100 handles all 3 members at once)
	expectedPhases := []types.TransitionPhase{
		types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION, // After SNAPSHOT completes
		types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION,   // After ARCHIVE completes
		types.TransitionPhase_TRANSITION_PHASE_RESET_XP,           // After RESET_REP completes
		types.TransitionPhase_TRANSITION_PHASE_TITLES,             // After RESET_XP completes
		types.TransitionPhase_TRANSITION_PHASE_CLEANUP,            // After TITLES completes
		types.TransitionPhase_TRANSITION_PHASE_COMPLETE,           // After CLEANUP completes
	}

	for i, expectedPhase := range expectedPhases {
		f.ctx = f.ctx.WithBlockHeight(int64(101 + i))
		err = f.keeper.BeginBlocker(f.ctx)
		require.NoError(t, err, "BeginBlocker failed at phase step %d", i)

		state, err = f.keeper.SeasonTransitionState.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, expectedPhase, state.Phase,
			"Expected phase %s at step %d, got %s", expectedPhase, i, state.Phase)
	}

	// One more call at COMPLETE phase should finalize
	f.ctx = f.ctx.WithBlockHeight(107)
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	// Verify Season 2 is now active
	newSeason, err := f.keeper.Season.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), newSeason.Number)
	require.Equal(t, "Season Two", newSeason.Name)
	require.Equal(t, "Adventure", newSeason.Theme)
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ACTIVE, newSeason.Status)
	require.Equal(t, int64(107), newSeason.StartBlock)

	// Verify transition state is cleaned up
	_, err = f.keeper.SeasonTransitionState.Get(f.ctx)
	require.Error(t, err, "transition state should be removed")

	// Verify XP was reset for all members
	for _, m := range members {
		profile, err := f.keeper.MemberProfile.Get(f.ctx, m.addr)
		require.NoError(t, err)
		require.Equal(t, uint64(0), profile.SeasonXp, "SeasonXp should be 0 for %s", m.addr)
		require.Equal(t, uint64(1), profile.SeasonLevel, "SeasonLevel should be 1 for %s", m.addr)
		require.Equal(t, m.xp, profile.LifetimeXp, "LifetimeXp should be preserved for %s", m.addr)
	}

	// Verify member season snapshots exist
	for _, m := range members {
		snapshotKey := fmt.Sprintf("1/%s", m.addr)
		snapshot, err := f.keeper.MemberSeasonSnapshot.Get(f.ctx, snapshotKey)
		require.NoError(t, err, "snapshot should exist for %s", m.addr)
		require.Equal(t, m.xp, snapshot.XpEarned)
		require.Equal(t, m.level, snapshot.SeasonLevel)
	}

	// Verify next BeginBlocker doesn't restart transition (season is ACTIVE, hasn't ended)
	f.ctx = f.ctx.WithBlockHeight(108)
	err = f.keeper.BeginBlocker(f.ctx)
	require.NoError(t, err)

	currentSeason, _ := f.keeper.Season.Get(f.ctx)
	require.Equal(t, uint64(2), currentSeason.Number, "should still be Season 2")
	require.Equal(t, types.SeasonStatus_SEASON_STATUS_ACTIVE, currentSeason.Status)
}
