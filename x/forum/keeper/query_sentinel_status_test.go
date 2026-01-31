package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQuerySentinelStatus(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.SentinelStatus(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty address", func(t *testing.T) {
		_, err := qs.SentinelStatus(f.ctx, &types.QuerySentinelStatusRequest{Address: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("sentinel not found", func(t *testing.T) {
		_, err := qs.SentinelStatus(f.ctx, &types.QuerySentinelStatusRequest{Address: testCreator})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("successful query", func(t *testing.T) {
		sentinel := f.createTestSentinel(t, testSentinel, "1000000")

		resp, err := qs.SentinelStatus(f.ctx, &types.QuerySentinelStatusRequest{Address: testSentinel})
		require.NoError(t, err)
		require.Equal(t, testSentinel, resp.Address)
		require.Equal(t, sentinel.CurrentBond, resp.CurrentBond)
		require.Equal(t, uint64(types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL), resp.BondStatus)
	})
}
