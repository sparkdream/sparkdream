package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"sparkdream/x/name/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		// Fix: Add valid NameRecords
		NameRecords: []types.NameRecord{
			{Name: "alice", Owner: "cosmos1alice", Data: "metadata"},
			{Name: "bob", Owner: "cosmos1bob", Data: "metadata"},
		},

		// Fix: Add valid OwnerInfos
		OwnerInfos: []types.OwnerInfo{
			{Address: "cosmos1alice", PrimaryName: "alice"},
			{Address: "cosmos1bob", PrimaryName: "bob"},
		},

		// Fix: Use valid Dispute fields (Name, Claimant) instead of 'Index'
		DisputeMap: []types.Dispute{
			{Name: "alice", Claimant: "cosmos1bob", StakeAmount: math.ZeroInt()},
			{Name: "bob", Claimant: "cosmos1charlie", StakeAmount: math.ZeroInt()},
		},

		DisputeStakes: []types.DisputeStake{
			{ChallengeId: "name_dispute:alice:10", Staker: "cosmos1bob", Amount: math.NewInt(500)},
		},
		ContestStakes: []types.ContestStake{
			{ChallengeId: "name_contest:alice:12", Owner: "cosmos1alice", Amount: math.NewInt(500)},
		},
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)

	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	// Verify all collections were exported correctly
	require.ElementsMatch(t, genesisState.NameRecords, got.NameRecords)
	require.ElementsMatch(t, genesisState.OwnerInfos, got.OwnerInfos)
	require.ElementsMatch(t, genesisState.DisputeMap, got.DisputeMap)
	require.ElementsMatch(t, genesisState.DisputeStakes, got.DisputeStakes)
	require.ElementsMatch(t, genesisState.ContestStakes, got.ContestStakes)
}

// TestGenesisRebuildsAcceptedTargets checks that the target -> name
// reverse-resolution index is derived from each record's consent flag on
// import, so an accepted target survives an export/import round-trip.
func TestGenesisRebuildsAcceptedTargets(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		NameRecords: []types.NameRecord{
			{Name: "phoenix", Owner: "cosmos1alice", Target: "cosmos1zenith", TargetAccepted: true},
			{Name: "aurora", Owner: "cosmos1alice", Target: "cosmos1zenith", TargetAccepted: false},
		},
	}

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, genesisState))

	has, err := f.keeper.AcceptedTargets.Has(f.ctx, collections.Join("cosmos1zenith", "phoenix"))
	require.NoError(t, err)
	require.True(t, has, "accepted target must be indexed on import")

	has, err = f.keeper.AcceptedTargets.Has(f.ctx, collections.Join("cosmos1zenith", "aurora"))
	require.NoError(t, err)
	require.False(t, has, "unaccepted target must not be indexed")
}

// A dispute the BeginBlocker left to council review stays active but out of
// the auto-resolution queue, and a genesis import keeps it out (the queue is
// rebuilt from the disputes).
func TestGenesisKeepsReviewedDisputeOutOfQueue(t *testing.T) {
	f := initFixture(t)
	d := types.Dispute{Name: "phoenix", Claimant: "aurora", FiledAt: 8, StakeAmount: math.NewInt(1), Active: true, AwaitingReview: true}
	require.NoError(t, f.keeper.SetDispute(f.ctx, d))
	has, err := f.keeper.PendingDisputes.Has(f.ctx, collections.Join(int64(8), "phoenix"))
	require.NoError(t, err)
	require.False(t, has)

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	has, err = g.keeper.PendingDisputes.Has(g.ctx, collections.Join(int64(8), "phoenix"))
	require.NoError(t, err)
	require.False(t, has, "a reviewed dispute is not queued for auto-resolution after import")
}
