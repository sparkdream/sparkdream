package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestQueryOperatorsByController(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))
	// Different controller for operator2.
	f.seedActiveOperator(t, testOperator2, testCouncil, math.NewInt(1_000_000))

	resp, err := f.queryServer.OperatorsByController(f.ctx, &types.QueryOperatorsByControllerRequest{
		Controller: testController,
	})
	require.NoError(t, err)
	require.Len(t, resp.Operators, 1)
	require.Equal(t, testOperator1, resp.Operators[0].Address)
}

func TestQueryOperatorsByController_InvalidAddress(t *testing.T) {
	f := initFixture(t)
	_, err := f.queryServer.OperatorsByController(f.ctx, &types.QueryOperatorsByControllerRequest{
		Controller: "not-bech32",
	})
	require.Error(t, err)
}

func TestQueryOperatorsByController_StatusFilter(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	op.UnbondCompleteAt = f.sdkCtx().BlockHeight() + 10
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	resp, err := f.queryServer.OperatorsByController(f.ctx, &types.QueryOperatorsByControllerRequest{
		Controller:   testController,
		StatusFilter: "active",
	})
	require.NoError(t, err)
	require.Empty(t, resp.Operators)
}
