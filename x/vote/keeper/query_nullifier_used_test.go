package keeper_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func TestNullifierUsed_Used(t *testing.T) {
	f := initTestFixture(t)

	nullBytes := genNullifier(1)
	key := keeper.NullifierKeyForTest(1, nullBytes)
	require.NoError(t, f.keeper.UsedNullifier.Set(f.ctx, key, types.UsedNullifier{
		Index:      key,
		ProposalId: 1,
		Nullifier:  nullBytes,
		UsedAt:     100,
	}))

	resp, err := f.queryServer.NullifierUsed(f.ctx, &types.QueryNullifierUsedRequest{
		ProposalId: 1,
		Nullifier:  hex.EncodeToString(nullBytes),
	})
	require.NoError(t, err)
	require.True(t, resp.Used)
	require.Equal(t, int64(100), resp.UsedAt)
}

func TestNullifierUsed_NotUsed(t *testing.T) {
	f := initTestFixture(t)

	// Query for a nullifier that has never been stored.
	resp, err := f.queryServer.NullifierUsed(f.ctx, &types.QueryNullifierUsedRequest{
		ProposalId: 1,
		Nullifier:  hex.EncodeToString(genNullifier(42)),
	})
	require.NoError(t, err)
	require.False(t, resp.Used)
	require.Equal(t, int64(0), resp.UsedAt)
}

func TestNullifierUsed_BadHex(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.NullifierUsed(f.ctx, &types.QueryNullifierUsedRequest{
		ProposalId: 1,
		Nullifier:  "zzzz-not-hex",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestNullifierUsed_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.NullifierUsed(f.ctx, nil)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
