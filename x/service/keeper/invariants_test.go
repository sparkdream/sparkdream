package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

func TestBondPoolAccountingInvariant_Clean(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount)

	// Make the bank balance match the bond pool exactly.
	f.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, _ string) sdk.Coin {
		return op.Bond
	}

	msg, broken := keeper.BondPoolAccountingInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken, "invariant should pass: %s", msg)
}

func TestBondPoolAccountingInvariant_Broken(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))

	// Bank reports zero balance but operator's bond is 1_000_000.
	f.bankKeeper.GetBalanceFn = func(_ context.Context, _ sdk.AccAddress, _ string) sdk.Coin {
		return sdk.NewCoin(types.BondDenom, math.ZeroInt())
	}

	_, broken := keeper.BondPoolAccountingInvariant(f.keeper)(f.sdkCtx())
	require.True(t, broken, "invariant should flag a broken bond pool")
}

func TestLiveArchiveDisjointInvariant_Clean(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))

	_, broken := keeper.LiveArchiveDisjointInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken)
}

func TestLiveArchiveDisjointInvariant_BrokenWhenBothExist(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))

	// Directly insert an archived record for the same (addr, service_type).
	archived := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		Bond:                    sdk.NewCoin(types.BondDenom, math.ZeroInt()),
		Status:                  types.OperatorStatus_OPERATOR_STATUS_SLASHED,
		RetiredAt:               f.sdkCtx().BlockHeight(),
		Tier1SlashedInWindow:    math.ZeroInt(),
		TotalLifetimeBondBlocks: math.ZeroInt(),
		Tier1WindowStartBond:    math.ZeroInt(),
	}
	require.NoError(t, f.keeper.ArchiveOperator(f.ctx, archived))
	// Re-add a live record after archival (this shouldn't happen via the
	// msg-server, but we're testing the invariant).
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))

	_, broken := keeper.LiveArchiveDisjointInvariant(f.keeper)(f.sdkCtx())
	require.True(t, broken)
}

func TestControllerIndexConsistencyInvariant_Clean(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))

	_, broken := keeper.ControllerIndexConsistencyInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken)
}

func TestServiceTypeIndexConsistencyInvariant_Clean(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))

	_, broken := keeper.ServiceTypeIndexConsistencyInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken)
}

func TestUnderfundedQueueConsistencyInvariant_Clean(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	height := f.sdkCtx().BlockHeight()
	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		Bond:                    sdk.NewCoin(types.BondDenom, math.NewInt(100_000)),
		Status:                  types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
		UnderfundedSince:        height,
		Tier1SlashedInWindow:    math.ZeroInt(),
		Tier1WindowStart:        height,
		Tier1WindowStartBond:    math.NewInt(100_000),
		RegisteredAt:            height,
		TotalLifetimeBondBlocks: math.ZeroInt(),
		LastBondBlockUpdateAt:   height,
	}
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	_, broken := keeper.UnderfundedQueueConsistencyInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken)
}

func TestPendingReportQueueConsistencyInvariant_Clean(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))
	f.seedPendingReport(t)

	_, broken := keeper.PendingReportQueueConsistencyInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken)
}

func TestEscalatedReportQueueConsistencyInvariant_Clean(t *testing.T) {
	f := initFixture(t)
	f.seedEscalatedReport(t, 200)

	_, broken := keeper.EscalatedReportQueueConsistencyInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken)
}

func TestTier1EscrowQueueConsistencyInvariant_Clean(t *testing.T) {
	f := initFixture(t)
	f.seedResolvedT1ReportAndSlash(t, 200)

	_, broken := keeper.Tier1EscrowQueueConsistencyInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken)
}

func TestReportStateMachineSanityInvariant_Clean(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))
	f.seedPendingReport(t)

	_, broken := keeper.ReportStateMachineSanityInvariant(f.keeper)(f.sdkCtx())
	require.False(t, broken)
}

// TestRegisterInvariants_Smoke confirms RegisterInvariants doesn't panic
// and all routes are added to a stub registry.
func TestRegisterInvariants_Smoke(t *testing.T) {
	f := initFixture(t)
	stub := &stubInvariantRegistry{}
	keeper.RegisterInvariants(stub, f.keeper)
	require.Len(t, stub.routes, 9, "expected 9 invariants registered")
}

type stubInvariantRegistry struct {
	routes []string
}

func (s *stubInvariantRegistry) RegisterRoute(moduleName, route string, _ sdk.Invariant) {
	s.routes = append(s.routes, moduleName+"/"+route)
}
