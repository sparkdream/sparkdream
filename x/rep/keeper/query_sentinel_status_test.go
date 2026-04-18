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

func TestQuerySentinelStatus_HappyPath(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentS")).String()
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, addr, types.SentinelActivity{
		Address:     addr,
		CurrentBond: "750",
		BondStatus:  types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY,
	}))

	resp, err := qs.SentinelStatus(f.ctx, &types.QuerySentinelStatusRequest{Address: addr})
	require.NoError(t, err)
	require.Equal(t, addr, resp.Address)
	require.Equal(t, "750", resp.CurrentBond)
	require.Equal(t, uint64(types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY), resp.BondStatus)
}

func TestQuerySentinelStatus_Errors(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.SentinelStatus(f.ctx, nil)
	require.Error(t, err)

	_, err = qs.SentinelStatus(f.ctx, &types.QuerySentinelStatusRequest{Address: ""})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())

	_, err = qs.SentinelStatus(f.ctx, &types.QuerySentinelStatusRequest{Address: "ghost"})
	require.Error(t, err)
	st, ok = status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}
