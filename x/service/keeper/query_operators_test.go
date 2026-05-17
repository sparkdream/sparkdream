package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/service/types"
)

func TestQueryOperators(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))
	f.seedActiveOperator(t, testOperator2, testController, math.NewInt(2_000_000))

	resp, err := f.queryServer.Operators(f.ctx, &types.QueryOperatorsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Operators, 2)
}

func TestQueryOperators_StatusFilter(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000_000))
	f.seedActiveOperator(t, testOperator2, testController, math.NewInt(2_000_000))

	// Flip op1 to UNBONDING.
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	op.UnbondCompleteAt = f.sdkCtx().BlockHeight() + 10
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))

	respA, err := f.queryServer.Operators(f.ctx, &types.QueryOperatorsRequest{StatusFilter: "active"})
	require.NoError(t, err)
	require.Len(t, respA.Operators, 1)
	require.Equal(t, testOperator2, respA.Operators[0].Address)

	respU, err := f.queryServer.Operators(f.ctx, &types.QueryOperatorsRequest{StatusFilter: "OPERATOR_STATUS_UNBONDING"})
	require.NoError(t, err)
	require.Len(t, respU.Operators, 1)
	require.Equal(t, testOperator1, respU.Operators[0].Address)
}

func TestQueryOperators_BadFilterRejected(t *testing.T) {
	f := initFixture(t)

	_, err := f.queryServer.Operators(f.ctx, &types.QueryOperatorsRequest{StatusFilter: "not-a-real-status"})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryOperators_NilRequest(t *testing.T) {
	f := initFixture(t)
	_, err := f.queryServer.Operators(f.ctx, nil)
	require.Error(t, err)
}
