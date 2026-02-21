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

func TestProposalNullifierUsed_Used(t *testing.T) {
	f := initTestFixture(t)

	nullBytes := genNullifier(7)
	key := keeper.ProposalNullifierKeyForTest(5, nullBytes)
	require.NoError(t, f.keeper.UsedProposalNullifier.Set(f.ctx, key, types.UsedProposalNullifier{
		Index:     key,
		Epoch:     5,
		Nullifier: nullBytes,
		UsedAt:    200,
	}))

	resp, err := f.queryServer.ProposalNullifierUsed(f.ctx, &types.QueryProposalNullifierUsedRequest{
		Epoch:     5,
		Nullifier: hex.EncodeToString(nullBytes),
	})
	require.NoError(t, err)
	require.True(t, resp.Used)
	require.Equal(t, int64(200), resp.UsedAt)
}

func TestProposalNullifierUsed_NotUsed(t *testing.T) {
	f := initTestFixture(t)

	resp, err := f.queryServer.ProposalNullifierUsed(f.ctx, &types.QueryProposalNullifierUsedRequest{
		Epoch:     5,
		Nullifier: hex.EncodeToString(genNullifier(99)),
	})
	require.NoError(t, err)
	require.False(t, resp.Used)
	require.Equal(t, int64(0), resp.UsedAt)
}

func TestProposalNullifierUsed_BadHex(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.ProposalNullifierUsed(f.ctx, &types.QueryProposalNullifierUsedRequest{
		Epoch:     5,
		Nullifier: "not$$hex!!",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestProposalNullifierUsed_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.ProposalNullifierUsed(f.ctx, nil)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
