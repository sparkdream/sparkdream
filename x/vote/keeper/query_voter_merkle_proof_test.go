package keeper_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestVoterMerkleProof_Happy(t *testing.T) {
	f := initTestFixture(t)

	// Register two voters.
	pk1 := genZkPubKey(1)
	pk2 := genZkPubKey(2)
	f.registerVoter(t, f.member, pk1)
	f.registerVoter(t, f.member2, pk2)

	// Create a public proposal, which stores a voter tree snapshot.
	proposalID := f.createPublicProposal(t, f.member)

	// Query merkle proof for voter 1.
	resp, err := f.queryServer.VoterMerkleProof(f.ctx, &types.QueryVoterMerkleProofRequest{
		ProposalId: proposalID,
		PublicKey:  hex.EncodeToString(pk1),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.MerkleRoot)
	require.NotEmpty(t, resp.Leaf)
	require.GreaterOrEqual(t, resp.LeafIndex, int32(0))
	require.NotEmpty(t, resp.PathElements)
	require.NotEmpty(t, resp.PathIndices)
	require.Equal(t, len(resp.PathElements), len(resp.PathIndices))
}

func TestVoterMerkleProof_NoSnapshot(t *testing.T) {
	f := initTestFixture(t)

	// Query with a proposal ID that has no snapshot.
	_, err := f.queryServer.VoterMerkleProof(f.ctx, &types.QueryVoterMerkleProofRequest{
		ProposalId: 999,
		PublicKey:  hex.EncodeToString(genZkPubKey(1)),
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestVoterMerkleProof_BadHexPublicKey(t *testing.T) {
	f := initTestFixture(t)

	// Register a voter and create a proposal to have a snapshot.
	f.registerVoter(t, f.member, genZkPubKey(1))
	proposalID := f.createPublicProposal(t, f.member)

	_, err := f.queryServer.VoterMerkleProof(f.ctx, &types.QueryVoterMerkleProofRequest{
		ProposalId: proposalID,
		PublicKey:  "not-valid-hex!@#$",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestVoterMerkleProof_NoActiveRegistrations(t *testing.T) {
	f := initTestFixture(t)

	// Manually store a snapshot but ensure no active voter registrations exist.
	snap := types.VoterTreeSnapshot{
		ProposalId:    1,
		MerkleRoot:    []byte("fakeroot"),
		SnapshotBlock: 50,
		VoterCount:    2,
	}
	require.NoError(t, f.keeper.VoterTreeSnapshot.Set(f.ctx, snap.ProposalId, snap))

	_, err := f.queryServer.VoterMerkleProof(f.ctx, &types.QueryVoterMerkleProofRequest{
		ProposalId: 1,
		PublicKey:  hex.EncodeToString(genZkPubKey(1)),
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestVoterMerkleProof_RootMismatch(t *testing.T) {
	f := initTestFixture(t)

	// Register voters and create a proposal (stores a snapshot).
	pk1 := genZkPubKey(1)
	pk2 := genZkPubKey(2)
	f.registerVoter(t, f.member, pk1)
	f.registerVoter(t, f.member2, pk2)
	proposalID := f.createPublicProposal(t, f.member)

	// Deactivate one voter to change the active voter set (root mismatch).
	reg, err := f.keeper.VoterRegistration.Get(f.ctx, f.member)
	require.NoError(t, err)
	reg.Active = false
	require.NoError(t, f.keeper.VoterRegistration.Set(f.ctx, f.member, reg))

	// The rebuilt tree now has only 1 voter while the snapshot has 2.
	// This triggers a FailedPrecondition error (root mismatch).
	_, err = f.queryServer.VoterMerkleProof(f.ctx, &types.QueryVoterMerkleProofRequest{
		ProposalId: proposalID,
		PublicKey:  hex.EncodeToString(pk2),
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestVoterMerkleProof_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.VoterMerkleProof(f.ctx, nil)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
