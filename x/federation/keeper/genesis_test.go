package keeper_test

import (
	"testing"

	"sparkdream/x/federation/types"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, genesisState.PortId, got.PortId)
	require.EqualExportedValues(t, genesisState.Params, got.Params)
}

func TestGenesisRoundTripsOperatorRewardDayFunding(t *testing.T) {
	// The per-UTC-day draw ledger bounds how much the operator reward pool can
	// pull from the community pool in a day. It lives only in module state, so
	// leaving it out of genesis let a mid-day export/import hand the chain a
	// fresh allowance and take the same day's draw twice. x/rep ledgers its
	// role-reward equivalent the same way.
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
		OperatorRewardDayFundingList: []types.OperatorRewardDayFunding{
			{Day: 20120, AmountFunded: math.NewInt(1_500_000)},
			{Day: 20121, AmountFunded: math.NewInt(2_750_000)},
		},
	}
	require.NoError(t, genesisState.Validate())

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, genesisState))

	// The imported ledger is what the daily cap reads, not a zeroed one.
	require.Equal(t, math.NewInt(1_500_000), f.keeper.GetOperatorRewardDayFunding(f.ctx, 20120))
	require.Equal(t, math.NewInt(2_750_000), f.keeper.GetOperatorRewardDayFunding(f.ctx, 20121))

	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, genesisState.OperatorRewardDayFundingList, got.OperatorRewardDayFundingList)
}

func TestGenesisRejectsDuplicateOperatorRewardDay(t *testing.T) {
	// A duplicate day collapses silently on import, under-reporting the day's
	// draw and handing back part of an allowance already spent.
	gs := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
		OperatorRewardDayFundingList: []types.OperatorRewardDayFunding{
			{Day: 20120, AmountFunded: math.NewInt(1_000_000)},
			{Day: 20120, AmountFunded: math.NewInt(2_000_000)},
		},
	}
	require.ErrorContains(t, gs.Validate(), "duplicated operator reward day funding")

	gs.OperatorRewardDayFundingList = []types.OperatorRewardDayFunding{
		{Day: 20120, AmountFunded: math.NewInt(-1)},
	}
	require.ErrorContains(t, gs.Validate(), "must be non-negative")
}
