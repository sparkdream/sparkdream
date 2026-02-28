package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestEpochDecryptionKeyQuery_Available(t *testing.T) {
	f := initTestFixture(t)

	// Store an epoch decryption key for epoch 5.
	require.NoError(t, f.keeper.EpochDecryptionKey.Set(f.ctx, 5, types.EpochDecryptionKey{
		Epoch:         5,
		DecryptionKey: []byte("decryption-key-5"),
		AvailableAt:   100,
	}))

	resp, err := f.queryServer.EpochDecryptionKeyQuery(f.ctx, &types.QueryEpochDecryptionKeyQueryRequest{
		Epoch: 5,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(5), resp.Epoch)
	require.True(t, resp.Available)
	require.Equal(t, []byte("decryption-key-5"), resp.DecryptionKey)
}

func TestEpochDecryptionKeyQuery_NotAvailable(t *testing.T) {
	f := initTestFixture(t)

	// Query for an epoch with no stored key.
	resp, err := f.queryServer.EpochDecryptionKeyQuery(f.ctx, &types.QueryEpochDecryptionKeyQueryRequest{
		Epoch: 99,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(99), resp.Epoch)
	require.False(t, resp.Available)
	require.Nil(t, resp.DecryptionKey)
}

func TestEpochDecryptionKeyQuery_ZeroValidators(t *testing.T) {
	f := initTestFixture(t)

	// No TLE validator shares registered, so sharesNeeded should be 0.
	resp, err := f.queryServer.EpochDecryptionKeyQuery(f.ctx, &types.QueryEpochDecryptionKeyQueryRequest{
		Epoch: 1,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp.Epoch)
	require.False(t, resp.Available)
	require.Equal(t, uint64(0), resp.SharesNeeded)
	require.Equal(t, uint64(0), resp.SharesReceived)
}

func TestEpochDecryptionKeyQuery_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.EpochDecryptionKeyQuery(f.ctx, nil)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
