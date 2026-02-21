package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestTleValidatorShares_Empty(t *testing.T) {
	f := initTestFixture(t)

	resp, err := f.queryServer.TleValidatorShares(f.ctx, &types.QueryTleValidatorSharesRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Shares)
	require.Equal(t, uint64(0), resp.TotalValidators)
	require.Equal(t, uint64(0), resp.RegisteredValidators)
	require.Equal(t, uint64(0), resp.ThresholdNeeded)
}

func TestTleValidatorShares_WithShares(t *testing.T) {
	f := initTestFixture(t)

	// Register 3 validator shares.
	for i, name := range []string{"val1", "val2", "val3"} {
		require.NoError(t, f.keeper.TleValidatorShare.Set(f.ctx, name, types.TleValidatorShare{
			Validator:      name,
			ShareIndex:     uint64(i + 1),
			PublicKeyShare: []byte("pk" + name),
			RegisteredAt:   int64(i * 10),
		}))
	}

	resp, err := f.queryServer.TleValidatorShares(f.ctx, &types.QueryTleValidatorSharesRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Shares, 3)
	require.Equal(t, uint64(3), resp.TotalValidators)
	require.Equal(t, uint64(3), resp.RegisteredValidators)
	// Default threshold: numerator=2, denominator=3.
	// threshold = ceil(3*2/3) = ceil(6/3) = 2
	require.Equal(t, uint64(2), resp.ThresholdNeeded)
}

func TestTleValidatorShares_SingleShare(t *testing.T) {
	f := initTestFixture(t)

	require.NoError(t, f.keeper.TleValidatorShare.Set(f.ctx, "solo", types.TleValidatorShare{
		Validator:      "solo",
		ShareIndex:     1,
		PublicKeyShare: []byte("pksolo"),
		RegisteredAt:   42,
	}))

	resp, err := f.queryServer.TleValidatorShares(f.ctx, &types.QueryTleValidatorSharesRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Shares, 1)
	require.Equal(t, uint64(1), resp.TotalValidators)
	require.Equal(t, uint64(1), resp.RegisteredValidators)
	// threshold = ceil(1*2/3) = ceil(2/3) = 1
	require.Equal(t, uint64(1), resp.ThresholdNeeded)
}

func TestTleValidatorShares_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.TleValidatorShares(f.ctx, nil)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
