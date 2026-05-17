package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/service/types"
)

func TestQueryServiceType(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)

	resp, err := f.queryServer.ServiceType(f.ctx, &types.QueryServiceTypeRequest{
		ServiceType: testServiceType,
	})
	require.NoError(t, err)
	require.Equal(t, cfg.ServiceType, resp.Config.ServiceType)
	require.True(t, resp.Config.Enabled)
}

func TestQueryServiceType_NotFound(t *testing.T) {
	f := initFixture(t)

	_, err := f.queryServer.ServiceType(f.ctx, &types.QueryServiceTypeRequest{
		ServiceType: "missing",
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestQueryServiceType_NilRequest(t *testing.T) {
	f := initFixture(t)
	_, err := f.queryServer.ServiceType(f.ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
