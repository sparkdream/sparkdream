package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestVoterTreeSnapshotQuery_Happy(t *testing.T) {
	f := initTestFixture(t)

	// Pre-populate a snapshot for proposal 42.
	snap := types.VoterTreeSnapshot{
		ProposalId:    42,
		MerkleRoot:    []byte("fakeroot"),
		SnapshotBlock: 100,
		VoterCount:    5,
	}
	require.NoError(t, f.keeper.VoterTreeSnapshot.Set(f.ctx, snap.ProposalId, snap))

	resp, err := f.queryServer.VoterTreeSnapshotQuery(f.ctx, &types.QueryVoterTreeSnapshotQueryRequest{
		ProposalId: 42,
	})
	require.NoError(t, err)
	require.Equal(t, snap.ProposalId, resp.Snapshot.ProposalId)
	require.Equal(t, snap.MerkleRoot, resp.Snapshot.MerkleRoot)
	require.Equal(t, snap.SnapshotBlock, resp.Snapshot.SnapshotBlock)
	require.Equal(t, snap.VoterCount, resp.Snapshot.VoterCount)
}

func TestVoterTreeSnapshotQuery_NotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.VoterTreeSnapshotQuery(f.ctx, &types.QueryVoterTreeSnapshotQueryRequest{
		ProposalId: 999,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestVoterTreeSnapshotQuery_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.VoterTreeSnapshotQuery(f.ctx, nil)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
