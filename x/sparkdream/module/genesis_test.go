package sparkdream_test

import (
	"testing"

	keepertest "sparkdream/testutil/keeper"
	"sparkdream/testutil/nullify"
	sparkdream "sparkdream/x/sparkdream/module"
	"sparkdream/x/sparkdream/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		// this line is used by starport scaffolding # genesis/test/state
	}

	k, ctx := keepertest.SparkdreamKeeper(t)
	sparkdream.InitGenesis(ctx, k, genesisState)
	got := sparkdream.ExportGenesis(ctx, k)
	require.NotNil(t, got)

	nullify.Fill(&genesisState)
	nullify.Fill(got)

	// this line is used by starport scaffolding # genesis/test/assert
}
