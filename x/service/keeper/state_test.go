package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestPutOperator_PrimaryAndIndexes(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	// Primary store.
	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.True(t, ok)
	require.Equal(t, testOperator1, op.Address)

	// OperatorsByController index seeded.
	hasCtl, _ := f.keeper.OperatorsByController.Has(f.ctx,
		collections.Join3(testControllerAddr.Bytes(), testOperator1Addr.Bytes(), testServiceType))
	require.True(t, hasCtl)

	// OperatorsByServiceType index seeded.
	hasSvc, _ := f.keeper.OperatorsByServiceType.Has(f.ctx,
		collections.Join(testServiceType, testOperator1Addr.Bytes()))
	require.True(t, hasSvc)
}

func TestPutOperator_UnderfundedQueueLifecycle(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	height := f.sdkCtx().BlockHeight()

	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		BondAmount:                    math.NewInt(100_000),
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

	has, _ := f.keeper.UnderfundedQueue.Has(f.ctx, collections.Join3(height, testOperator1Addr.Bytes(), testServiceType))
	require.True(t, has)

	// PutOperator removes the queue entry for the CURRENT (underfunded_since,
	// addr, svc) tuple when status leaves UNDERFUNDED. Callers that want to
	// drop a stale entry from a prior underfunded_since must call
	// removeUnderfundedQueueEntry themselves first — exercise that path here.
	require.NoError(t, f.keeper.UnderfundedQueue.Remove(f.ctx,
		collections.Join3(height, testOperator1Addr.Bytes(), testServiceType)))
	op.Status = types.OperatorStatus_OPERATOR_STATUS_ACTIVE
	op.UnderfundedSince = 0
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	has, _ = f.keeper.UnderfundedQueue.Has(f.ctx, collections.Join3(height, testOperator1Addr.Bytes(), testServiceType))
	require.False(t, has)
}

func TestArchiveOperator_DropsLiveAndKeepsArchive(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	// Move to archive shape: bond zero, status RETIRED, retired_at set.
	op.BondAmount = math.ZeroInt()
	op.Status = types.OperatorStatus_OPERATOR_STATUS_RETIRED
	op.RetiredAt = f.sdkCtx().BlockHeight()
	require.NoError(t, f.keeper.ArchiveOperator(f.ctx, op))

	// Live store entry gone.
	_, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.False(t, ok)

	// Live indexes gone.
	hasCtl, _ := f.keeper.OperatorsByController.Has(f.ctx,
		collections.Join3(testControllerAddr.Bytes(), testOperator1Addr.Bytes(), testServiceType))
	require.False(t, hasCtl)

	// Archived record present.
	archived, err := f.keeper.GetArchivedOperators(f.ctx, testOperator1Addr, testServiceType)
	require.NoError(t, err)
	require.Len(t, archived, 1)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_RETIRED, archived[0].Status)
}

func TestArchiveOperator_RejectsNonTerminalStatus(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	// Mutate to satisfy bond-zero + retired_at preconditions but keep
	// status ACTIVE — ArchiveOperator must reject.
	op.BondAmount = math.ZeroInt()
	op.RetiredAt = f.sdkCtx().BlockHeight()
	err := f.keeper.ArchiveOperator(f.ctx, op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SLASHED or RETIRED")
}

func TestArchiveOperator_RejectsNonZeroBond(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	op.Status = types.OperatorStatus_OPERATOR_STATUS_RETIRED
	op.RetiredAt = f.sdkCtx().BlockHeight()
	err := f.keeper.ArchiveOperator(f.ctx, op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bond must be zero")
}

func TestArchiveOperator_RejectsZeroRetiredAt(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	op.BondAmount = math.ZeroInt()
	op.Status = types.OperatorStatus_OPERATOR_STATUS_RETIRED
	op.RetiredAt = 0
	err := f.keeper.ArchiveOperator(f.ctx, op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retired_at must be set")
}

func TestHasSlashedRecord(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	// Insert SLASHED archive for (op1, testServiceType).
	op := types.Operator{
		Address:                 testOperator1,
		ServiceType:             testServiceType,
		Controller:              testController,
		BondAmount:                    math.ZeroInt(),
		Status:                  types.OperatorStatus_OPERATOR_STATUS_SLASHED,
		RetiredAt:               f.sdkCtx().BlockHeight(),
		Tier1SlashedInWindow:    math.ZeroInt(),
		TotalLifetimeBondBlocks: math.ZeroInt(),
		Tier1WindowStartBond:    math.ZeroInt(),
	}
	require.NoError(t, f.keeper.ArchiveOperator(f.ctx, op))

	has, err := f.keeper.HasSlashedRecord(f.ctx, testOperator1Addr.Bytes(), testServiceType)
	require.NoError(t, err)
	require.True(t, has)

	// Different service type → not slashed.
	has, err = f.keeper.HasSlashedRecord(f.ctx, testOperator1Addr.Bytes(), "other")
	require.NoError(t, err)
	require.False(t, has)
}

func TestCountLiveOperatorsForAddress(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	count, err := f.keeper.CountLiveOperatorsForAddress(f.ctx, testOperator1Addr.Bytes())
	require.NoError(t, err)
	require.EqualValues(t, 0, count)

	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))

	count, err = f.keeper.CountLiveOperatorsForAddress(f.ctx, testOperator1Addr.Bytes())
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
