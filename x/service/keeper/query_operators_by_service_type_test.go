package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestQueryOperatorsByServiceType(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))
	f.seedActiveOperator(t, testOperator2, testController, math.NewInt(1_000_000))

	resp, err := f.queryServer.OperatorsByServiceType(f.ctx, &types.QueryOperatorsByServiceTypeRequest{
		ServiceType: testServiceType,
	})
	require.NoError(t, err)
	require.Len(t, resp.Operators, 2)
}

func TestQueryOperatorsByServiceType_EmptyForUnknown(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))

	resp, err := f.queryServer.OperatorsByServiceType(f.ctx, &types.QueryOperatorsByServiceTypeRequest{
		ServiceType: "unknown",
	})
	require.NoError(t, err)
	require.Empty(t, resp.Operators)
}

func TestQueryOperatorsByServiceType_StatusFilter(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	op.UnbondCompleteAt = f.sdkCtx().BlockHeight() + 10
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	resp, err := f.queryServer.OperatorsByServiceType(f.ctx, &types.QueryOperatorsByServiceTypeRequest{
		ServiceType:  testServiceType,
		StatusFilter: "unbonding",
	})
	require.NoError(t, err)
	require.Len(t, resp.Operators, 1)
}
