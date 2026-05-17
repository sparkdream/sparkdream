package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/service/types"
)

func TestQueryOperator(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.seedActiveOperator(t, testOperator1, testController, math.NewInt(2_000_000))

	resp, err := f.queryServer.Operator(f.ctx, &types.QueryOperatorRequest{
		Address:     testOperator1,
		ServiceType: testServiceType,
	})
	require.NoError(t, err)
	require.Equal(t, testOperator1, resp.Operator.Address)
}

func TestQueryOperator_NotFound(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	_, err := f.queryServer.Operator(f.ctx, &types.QueryOperatorRequest{
		Address:     testOperator1,
		ServiceType: testServiceType,
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestQueryOperator_InvalidAddress(t *testing.T) {
	f := initFixture(t)

	_, err := f.queryServer.Operator(f.ctx, &types.QueryOperatorRequest{
		Address:     "not-bech32",
		ServiceType: testServiceType,
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryOperator_NilRequest(t *testing.T) {
	f := initFixture(t)

	_, err := f.queryServer.Operator(f.ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
