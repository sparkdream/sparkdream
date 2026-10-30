package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/identity/types"
)

func TestInitGenesisAndExport(t *testing.T) {
	f := initFixture(t)
	gs := types.GenesisState{Identity: newValidIdentity(), AllowChainIdMismatch: true}

	require.NoError(t, f.keeper.InitGenesis(f.ctx, gs))

	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, got.Identity.EqualCanonical(gs.Identity))
	// Export never re-enables the mismatch bypass.
	require.False(t, got.AllowChainIdMismatch)
}

func TestInitGenesisRejectsInvalidIdentity(t *testing.T) {
	f := initFixture(t)
	bad := newValidIdentity()
	bad.BondDenom = "uspark"
	require.Error(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Identity: bad, AllowChainIdMismatch: true}))
}

func TestExportGenesisBeforeInitErrors(t *testing.T) {
	f := initFixture(t)
	_, err := f.keeper.ExportGenesis(f.ctx)
	require.Error(t, err)
}

// IDN-03: an empty identity is refused at InitGenesis with a message naming
// the genesis as the cause, instead of writing nothing and leaving every
// later BondDenom read to panic.
func TestInitGenesisRejectsEmptyIdentity(t *testing.T) {
	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, types.GenesisState{AllowChainIdMismatch: true})
	require.ErrorIs(t, err, types.ErrIdentityNotInitialized)
	_, err = f.keeper.GetSealedIdentity(f.ctx)
	require.Error(t, err, "nothing sealed")
}
