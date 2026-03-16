package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func TestQueryAllowedMsgTypes(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.AllowedMsgTypes(f.ctx, &types.QueryAllowedMsgTypesRequest{})
	require.NoError(t, err)

	defaultParams := types.DefaultParams()
	require.Equal(t, defaultParams.MaxAllowedMsgTypes, resp.MaxAllowedMsgTypes)
	require.Equal(t, defaultParams.AllowedMsgTypes, resp.AllowedMsgTypes)
}

func TestQueryAllowedMsgTypesAfterUpdate(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Reduce active allowlist
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)

	originalCeiling := params.MaxAllowedMsgTypes
	reducedActive := params.AllowedMsgTypes[:3]
	params.AllowedMsgTypes = reducedActive
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	resp, err := qs.AllowedMsgTypes(f.ctx, &types.QueryAllowedMsgTypesRequest{})
	require.NoError(t, err)

	// Ceiling unchanged, active list reduced
	require.Equal(t, originalCeiling, resp.MaxAllowedMsgTypes)
	require.Equal(t, reducedActive, resp.AllowedMsgTypes)
}

func TestQueryAllowedMsgTypesNilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.AllowedMsgTypes(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}
