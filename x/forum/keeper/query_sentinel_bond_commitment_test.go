package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQuerySentinelBondCommitment(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.SentinelBondCommitment(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty address", func(t *testing.T) {
		_, err := qs.SentinelBondCommitment(f.ctx, &types.QuerySentinelBondCommitmentRequest{Address: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("sentinel not found", func(t *testing.T) {
		_, err := qs.SentinelBondCommitment(f.ctx, &types.QuerySentinelBondCommitmentRequest{Address: testCreator})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("sentinel with bond", func(t *testing.T) {
		f.createTestSentinel(t, testSentinel, "1000000")

		resp, err := qs.SentinelBondCommitment(f.ctx, &types.QuerySentinelBondCommitmentRequest{Address: testSentinel})
		require.NoError(t, err)
		require.Equal(t, "1000000", resp.CurrentBond)
		require.Equal(t, "0", resp.TotalCommittedBond)
		require.Equal(t, "1000000", resp.AvailableBond)
	})

	t.Run("sentinel with committed bond", func(t *testing.T) {
		sentinel := f.createTestSentinel(t, testCreator2, "2000000")
		sentinel.TotalCommittedBond = "500000"
		f.keeper.SentinelActivity.Set(f.ctx, testCreator2, sentinel)

		resp, err := qs.SentinelBondCommitment(f.ctx, &types.QuerySentinelBondCommitmentRequest{Address: testCreator2})
		require.NoError(t, err)
		require.Equal(t, "2000000", resp.CurrentBond)
		require.Equal(t, "500000", resp.TotalCommittedBond)
		require.Equal(t, "1500000", resp.AvailableBond)
	})
}
