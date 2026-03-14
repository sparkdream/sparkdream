package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestQueryParamsNilRequest(t *testing.T) {
	_, qs := initQueryServer(t)

	_, err := qs.Params(nil, nil)
	require.Error(t, err)
}

func TestQueryParamsAfterUpdate(t *testing.T) {
	f, qs := initQueryServer(t)

	// Update params
	newParams := types.DefaultParams()
	newParams.MaxGasPerExec = 777_777
	newParams.MaxFundingPerDay = math.NewInt(999)
	require.NoError(t, f.keeper.Params.Set(f.ctx, newParams))

	resp, err := qs.Params(f.ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(777_777), resp.Params.MaxGasPerExec)
	require.Equal(t, math.NewInt(999), resp.Params.MaxFundingPerDay)
}

func TestQueryParamsDefault(t *testing.T) {
	f, qs := initQueryServer(t)

	resp, err := qs.Params(f.ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.True(t, resp.Params.Enabled)
	require.Equal(t, types.DefaultMaxGasPerExec, resp.Params.MaxGasPerExec)
	require.Equal(t, types.DefaultMaxExecsPerIdentity, resp.Params.MaxExecsPerIdentityPerEpoch)
}
