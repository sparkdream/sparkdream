package keeper_test

import (
	"context"
	"testing"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/identity/types"
)

// mockBank tracks SetDenomMetaData calls for tests that need to assert
// genesis-time bank seeding without pulling in the full bank keeper.
type mockBank struct {
	metas map[string]banktypes.Metadata
}

func newMockBank() *mockBank {
	return &mockBank{metas: make(map[string]banktypes.Metadata)}
}

func (m *mockBank) SetDenomMetaData(_ context.Context, md banktypes.Metadata) {
	m.metas[md.Base] = md
}

func (m *mockBank) GetDenomMetaData(_ context.Context, denom string) (banktypes.Metadata, bool) {
	md, ok := m.metas[denom]
	return md, ok
}

func TestInitGenesisSeedsBankMetadata(t *testing.T) {
	f := initFixture(t)
	id := newValidIdentity()
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Identity: id, AllowChainIdMismatch: true}))

	spark, ok := f.bank.GetDenomMetaData(f.ctx, id.BondDenom)
	require.True(t, ok)
	require.Equal(t, id.BondDisplaySymbol, spark.Symbol)
	require.Equal(t, "pspk", spark.Display)
	require.Equal(t, id.BondDisplayName, spark.Name)
	require.Contains(t, spark.Description, id.BondDisplayName)

	dream, ok := f.bank.GetDenomMetaData(f.ctx, id.DreamDenom)
	require.True(t, ok)
	require.Equal(t, id.DreamDisplaySymbol, dream.Symbol)
	require.Equal(t, "pdrm", dream.Display)
	require.Contains(t, dream.Description, "non-transferable")
}

func TestInitGenesisSealedMatchesMetadataWrite(t *testing.T) {
	f := initFixture(t)
	id := newValidIdentity()
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Identity: id, AllowChainIdMismatch: true}))

	sealed, err := f.keeper.GetSealedIdentity(f.ctx)
	require.NoError(t, err)

	spark, _ := f.bank.GetDenomMetaData(f.ctx, sealed.BondDenom)
	require.Equal(t, sealed.BondDisplaySymbol, spark.Symbol)
	require.Equal(t, uint32(spark.DenomUnits[1].Exponent), sealed.BondDisplayDecimals)
	dream, _ := f.bank.GetDenomMetaData(f.ctx, sealed.DreamDenom)
	require.Equal(t, sealed.DreamDisplaySymbol, dream.Symbol)
}
