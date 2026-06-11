package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

func TestIsActiveOperator(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	// Missing operator: false.
	require.False(t, f.keeper.IsActiveOperator(f.ctx, testOperator1Addr, testServiceType))

	// ACTIVE operator: true.
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))
	require.True(t, f.keeper.IsActiveOperator(f.ctx, testOperator1Addr, testServiceType))

	// Flip to UNBONDING: false (only ACTIVE counts).
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	op.UnbondCompleteAt = f.sdkCtx().BlockHeight() + 10
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	require.False(t, f.keeper.IsActiveOperator(f.ctx, testOperator1Addr, testServiceType))
}

func TestGetAvailableBond(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	// Missing operator: zero coin.
	c := f.keeper.GetAvailableBond(f.ctx, testOperator1Addr, testServiceType)
	require.Equal(t, testBondDenom, c.Denom)
	require.True(t, c.Amount.IsZero())

	// Live operator: bond is returned verbatim.
	want := math.NewInt(2_500_000)
	f.seedActiveOperator(t, testOperator1, testController, want)
	c = f.keeper.GetAvailableBond(f.ctx, testOperator1Addr, testServiceType)
	require.True(t, c.Amount.Equal(want))
}

func TestGetServiceTypeConfig(t *testing.T) {
	f := initFixture(t)

	_, ok := f.keeper.GetServiceTypeConfig(f.ctx, testServiceType)
	require.False(t, ok)

	cfg := f.seedServiceType(t)

	got, ok := f.keeper.GetServiceTypeConfig(f.ctx, testServiceType)
	require.True(t, ok)
	require.Equal(t, cfg.ServiceType, got.ServiceType)
	require.True(t, got.Enabled)
}

func TestGetArchivedOperators(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	// Empty initially.
	got, err := f.keeper.GetArchivedOperators(f.ctx, testOperator1Addr, testServiceType)
	require.NoError(t, err)
	require.Empty(t, got)

	// Insert a SLASHED record.
	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		BondAmount:              math.ZeroInt(),
		Status:                  types.OperatorStatus_OPERATOR_STATUS_SLASHED,
		RetiredAt:               f.sdkCtx().BlockHeight(),
		Tier1SlashedInWindow:    math.ZeroInt(),
		TotalLifetimeBondBlocks: math.ZeroInt(),
		Tier1WindowStartBond:    math.ZeroInt(),
	}
	require.NoError(t, f.keeper.ArchiveOperator(f.ctx, op))

	got, err = f.keeper.GetArchivedOperators(f.ctx, testOperator1Addr, testServiceType)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_SLASHED, got[0].Status)
}

func TestListPendingReportsAgainst(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	got, err := f.keeper.ListPendingReportsAgainst(f.ctx, testOperator1Addr, testServiceType)
	require.NoError(t, err)
	require.Empty(t, got)

	// Seed PENDING + RESOLVED reports. Only PENDING (and ESCALATED) come back.
	f.seedPendingReport(t)
	f.seedPendingReport(t)

	got, err = f.keeper.ListPendingReportsAgainst(f.ctx, testOperator1Addr, testServiceType)
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestPublicAPI_RegisterOperator(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	op, err := f.keeper.RegisterOperator(
		f.ctx,
		testOperator1,
		testServiceType,
		testController,
		math.NewInt(2_000_000),
		[]byte("via-api"),
		keeper.SlashSource(0), // normal mode (any non-Migration)
	)
	require.NoError(t, err)
	require.Equal(t, testOperator1, op.Address)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, op.Status)
}

func TestPublicAPI_RegisterOperator_MigrationBypassesGates(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	// Migration bypasses the "controller must be a Group" check. testRandom
	// is not a group policy in the default mock.
	op, err := f.keeper.RegisterOperator(
		f.ctx,
		testOperator1,
		testServiceType,
		testRandom,
		math.NewInt(1), // below min, but bypass
		[]byte("migrate"),
		keeper.SlashSourceMigration,
	)
	require.NoError(t, err)
	require.Equal(t, testRandom, op.Controller)
}

func TestPublicAPI_SlashOperator_InvalidBps(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	_, err := f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 0, 0, keeper.SlashSourceTier2Jury)
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrSlashCapExceeded.Error())

	_, err = f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 10001, 0, keeper.SlashSourceTier2Jury)
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrSlashCapExceeded.Error())
}

func TestPublicAPI_SlashOperator_Tier2RoutesToCommunityPool(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	preBond := math.NewInt(2_000_000)
	slashCoin, err := f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 1000, 0, keeper.SlashSourceTier2Jury)
	require.NoError(t, err)
	require.True(t, slashCoin.Amount.IsPositive())

	// Bond decreased.
	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.True(t, op.BondAmount.LT(preBond))

	// Distribution keeper was called.
	require.NotEmpty(t, f.distributionKeeper.Calls)
}

func TestPublicAPI_TerminateOperator(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	err := f.keeper.TerminateOperator(f.ctx, testOperator1Addr, testServiceType, 0)
	require.NoError(t, err)

	// Live record gone.
	_, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.False(t, ok)

	// SLASHED archive present.
	archived, err := f.keeper.GetArchivedOperators(f.ctx, testOperator1Addr, testServiceType)
	require.NoError(t, err)
	require.Len(t, archived, 1)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_SLASHED, archived[0].Status)

	// Dissolution hook fired.
	require.Len(t, f.hooks.Dissolved, 1)
}

func TestPublicAPI_TerminateOperator_NotFound(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	err := f.keeper.TerminateOperator(f.ctx, testOperator1Addr, testServiceType, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrOperatorNotFound.Error())
}

func TestPublicAPI_SetServiceTypeConfig(t *testing.T) {
	f := initFixture(t)

	cfg := validSimCfg()
	cfg.Description = "set-via-keeper"
	require.NoError(t, f.keeper.SetServiceTypeConfig(f.ctx, cfg))

	got, err := f.keeper.ServiceTypes.Get(f.ctx, cfg.ServiceType)
	require.NoError(t, err)
	require.Equal(t, "set-via-keeper", got.Description)
}

// ---------------------------------------------------------------------------
// SlashOperator: AfterOperatorUnderfunded hook fires when a tier-1 slash
// drops bond below min_bond (federation→service migration Phase 0).
// ---------------------------------------------------------------------------

func TestSlashOperator_FiresAfterOperatorUnderfundedOnTransition(t *testing.T) {
	f := initFixture(t)
	hooks := f.hooks

	cfg := f.seedServiceType(t)
	// Make the threshold "easy to drop below" — set min_bond high relative
	// to seeded bond so a small slash flips status.
	cfg.MinBondAmount = math.NewInt(900_000)
	cfg.UnilateralSlashCapBps = 5000 // permit a big tier-1 slash for the test
	cfg.Tier1AggregateCapBps = 5000
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))

	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))

	// 50% tier-1 slash on a 1_000_000 bond drops to 500_000 < min_bond
	// (900_000) → ACTIVE → UNDERFUNDED transition.
	_, err := f.keeper.SlashOperator(f.ctx, testOperator1Addr, testServiceType, 5000, 0, keeper.SlashSourceTier1)
	require.NoError(t, err)

	require.Len(t, hooks.Underfunded, 1, "underfunded hook must fire")
	require.True(t, hooks.Underfunded[0].Operator.Equals(testOperator1Addr))
	require.Equal(t, testServiceType, hooks.Underfunded[0].ServiceType)
}
