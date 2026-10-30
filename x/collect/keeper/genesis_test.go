package keeper_test

import (
	"testing"

	"sparkdream/x/collect/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
}

// The promotion queue is hook-driven, so genesis carries it verbatim.
func TestGenesisKeepsPromotionQueue(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.PromotionQueue.Set(f.ctx, "phoenix", 60))
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.PromotionQueue, 1)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	at, err := g.keeper.PromotionQueue.Get(g.ctx, "phoenix")
	require.NoError(t, err)
	require.Equal(t, int64(60), at)
}
