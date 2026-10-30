package keeper_test

import (
	"testing"

	"sparkdream/x/blog/types"

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

// The promotion queue (authors admitted as members with ephemeral content
// still pending) is hook-driven, so genesis carries it verbatim.
func TestGenesisKeepsPromotionQueue(t *testing.T) {
	f := initFixture(t)
	f.keeper.EnqueueAuthorForPromotion(f.ctx, "phoenix")
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.PromotionQueue, 1)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	require.True(t, g.keeper.IsAuthorQueuedForPromotion(g.ctx, "phoenix"))
	again, err := g.keeper.ExportGenesis(g.ctx)
	require.NoError(t, err)
	require.Equal(t, exported.PromotionQueue, again.PromotionQueue)
}
