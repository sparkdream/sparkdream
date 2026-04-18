package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQuerySentinelActivity_GetAndList(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentA")).String()
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, addr, types.SentinelActivity{
		Address:     addr,
		CurrentBond: "500",
	}))

	resp, err := qs.GetSentinelActivity(f.ctx, &types.QueryGetSentinelActivityRequest{Address: addr})
	require.NoError(t, err)
	require.Equal(t, addr, resp.SentinelActivity.Address)
	require.Equal(t, "500", resp.SentinelActivity.CurrentBond)

	listed, err := qs.ListSentinelActivity(f.ctx, &types.QueryAllSentinelActivityRequest{})
	require.NoError(t, err)
	require.Len(t, listed.SentinelActivity, 1)
}

func TestQuerySentinelActivity_NotFound(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetSentinelActivity(f.ctx, &types.QueryGetSentinelActivityRequest{Address: "ghost"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestQuerySentinelActivity_NilRequests(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetSentinelActivity(f.ctx, nil)
	require.Error(t, err)
	_, err = qs.ListSentinelActivity(f.ctx, nil)
	require.Error(t, err)
}
