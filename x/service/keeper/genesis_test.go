package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"

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

// SVC-14: retire, then re-register the same (address, service_type). The
// chain's own export must validate and import: the RETIRED history ended
// before the live record began. A SLASHED history beside a live record, or
// a RETIRED one overlapping it, is still refused.
func TestGenesisRetireThenReregisterRoundTrips(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.withBlockHeight(10)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000_000))

	f.withBlockHeight(20)
	op.Status = types.OperatorStatus_OPERATOR_STATUS_RETIRED
	op.RetiredAt = 20
	op.BondAmount = math.ZeroInt()
	require.NoError(t, f.keeper.ArchiveOperator(f.ctx, op))

	f.withBlockHeight(30)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000_000))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.ArchivedOperators, 1)
	require.Len(t, exported.Operators, 1)
	require.NoError(t, exported.Validate())

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))

	overlap := *exported
	overlap.ArchivedOperators = append([]types.Operator(nil), exported.ArchivedOperators...)
	overlap.ArchivedOperators[0].RetiredAt = 30
	require.Error(t, overlap.Validate(), "history overlapping the live record")

	slashed := *exported
	slashed.ArchivedOperators = append([]types.Operator(nil), exported.ArchivedOperators...)
	slashed.ArchivedOperators[0].Status = types.OperatorStatus_OPERATOR_STATUS_SLASHED
	require.Error(t, slashed.Validate(), "a slashed history never sits beside a live record")
}

// An unused report or escrow sequence exports as 1 (Peek + 1) and must not
// come back as a stored 0: the import leaves it unset, like the live chain.
func TestGenesisUnusedSequencesStayUnset(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Params: types.DefaultParams()}))
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), exported.NextEscrowId)
	require.Equal(t, uint64(1), exported.NextReportId)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	store := sdk.UnwrapSDKContext(g.ctx).KVStore(g.storeKey)
	require.False(t, store.Has(types.NextEscrowIDKey))
	require.False(t, store.Has(types.NextReportIDKey))

	// A used sequence still round-trips.
	exported.NextEscrowId = 7
	h := initFixture(t)
	require.NoError(t, h.keeper.InitGenesis(h.ctx, *exported))
	next, err := h.keeper.NextEscrowID.Peek(h.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(6), next)
}
