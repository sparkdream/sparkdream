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

func TestQuerySentinelBondCommitment_HappyPath(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sent")).String()
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, addr, types.SentinelActivity{
		Address:            addr,
		CurrentBond:        "1000",
		TotalCommittedBond: "300",
	}))

	resp, err := qs.SentinelBondCommitment(f.ctx, &types.QuerySentinelBondCommitmentRequest{Address: addr})
	require.NoError(t, err)
	require.Equal(t, "1000", resp.CurrentBond)
	require.Equal(t, "300", resp.TotalCommittedBond)
	require.Equal(t, "700", resp.AvailableBond)
}

func TestQuerySentinelBondCommitment_NegativeAvailableClampedToZero(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("over")).String()
	require.NoError(t, f.keeper.SentinelActivity.Set(f.ctx, addr, types.SentinelActivity{
		Address:            addr,
		CurrentBond:        "100",
		TotalCommittedBond: "500", // over-committed past a slash
	}))

	resp, err := qs.SentinelBondCommitment(f.ctx, &types.QuerySentinelBondCommitmentRequest{Address: addr})
	require.NoError(t, err)
	require.Equal(t, "0", resp.AvailableBond, "available must not go negative when bond is over-committed")
}

func TestQuerySentinelBondCommitment_Errors(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.SentinelBondCommitment(f.ctx, nil)
	require.Error(t, err)

	_, err = qs.SentinelBondCommitment(f.ctx, &types.QuerySentinelBondCommitmentRequest{Address: ""})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())

	_, err = qs.SentinelBondCommitment(f.ctx, &types.QuerySentinelBondCommitmentRequest{Address: "ghost"})
	require.Error(t, err)
	st, ok = status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}
