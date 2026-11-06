package keeper_test

import (
	"testing"

	"sparkdream/x/split/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	addr := func(name string) string {
		b := make([]byte, 20)
		copy(b, name)
		return sdk.AccAddress(b).String()
	}
	genesisState := types.GenesisState{
		Params:   types.DefaultParams(),
		ShareMap: []types.Share{{Address: addr("phoenix"), Weight: 1}, {Address: addr("aurora"), Weight: 2}}}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)

	// InitGenesis re-validates: a share the chain could never have written
	// is refused instead of stored.
	bad := genesisState
	bad.ShareMap = []types.Share{{Address: addr("zenith"), Weight: 0}}
	require.Error(t, f.keeper.InitGenesis(f.ctx, bad))
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	// Export walks the store in key order, not insertion order.
	require.ElementsMatch(t, genesisState.ShareMap, got.ShareMap)

}
