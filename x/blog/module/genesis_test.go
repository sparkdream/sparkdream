package blog_test

import (
	"testing"

	keepertest "sparkdream/testutil/keeper"
	"sparkdream/testutil/nullify"
	blog "sparkdream/x/blog/module"
	"sparkdream/x/blog/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		// this line is used by starport scaffolding # genesis/test/state
	}

	k, ctx := keepertest.BlogKeeper(t)
	blog.InitGenesis(ctx, k, genesisState)
	got := blog.ExportGenesis(ctx, k)
	require.NotNil(t, got)

	nullify.Fill(&genesisState)
	nullify.Fill(got)

	// this line is used by starport scaffolding # genesis/test/assert
}
