package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestTleStatus_Happy(t *testing.T) {
	f := initTestFixture(t)

	// Default params have TleEnabled=true. The mock season keeper returns epoch 10.
	resp, err := f.queryServer.TleStatus(f.ctx, &types.QueryTleStatusRequest{})
	require.NoError(t, err)
	require.True(t, resp.TleEnabled)
	require.Equal(t, uint64(10), resp.CurrentEpoch)
	require.Equal(t, uint64(0), resp.LatestAvailableEpoch) // no keys stored yet
	require.Nil(t, resp.MasterPublicKey)                    // default params have nil master key
}

func TestTleStatus_LatestAvailableEpoch(t *testing.T) {
	f := initTestFixture(t)

	// Store epoch decryption keys for epochs 3 and 7.
	require.NoError(t, f.keeper.EpochDecryptionKey.Set(f.ctx, 3, types.EpochDecryptionKey{
		Epoch:        3,
		DecryptionKey: []byte("key3"),
		AvailableAt:  50,
	}))
	require.NoError(t, f.keeper.EpochDecryptionKey.Set(f.ctx, 7, types.EpochDecryptionKey{
		Epoch:        7,
		DecryptionKey: []byte("key7"),
		AvailableAt:  100,
	}))

	resp, err := f.queryServer.TleStatus(f.ctx, &types.QueryTleStatusRequest{})
	require.NoError(t, err)
	require.True(t, resp.TleEnabled)
	require.Equal(t, uint64(10), resp.CurrentEpoch)
	require.Equal(t, uint64(7), resp.LatestAvailableEpoch)
}

func TestTleStatus_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.TleStatus(f.ctx, nil)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
