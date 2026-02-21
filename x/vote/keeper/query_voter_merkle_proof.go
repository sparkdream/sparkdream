package keeper

import (
	"bytes"
	"context"
	"encoding/hex"

	"sparkdream/x/vote/types"
	zkcrypto "sparkdream/zkprivatevoting/crypto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) VoterMerkleProof(ctx context.Context, req *types.QueryVoterMerkleProofRequest) (*types.QueryVoterMerkleProofResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	snapshot, err := q.k.VoterTreeSnapshot.Get(ctx, req.ProposalId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "voter tree snapshot not found")
	}

	// Decode the public key from hex.
	pubKeyBytes, err := hex.DecodeString(req.PublicKey)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid public key hex")
	}

	// Rebuild the full Merkle tree from current active voter registrations.
	// This produces the same tree as was computed at snapshot time, provided
	// the voter set hasn't changed. We verify the root matches below.
	var zkPubKeys [][]byte
	err = q.k.VoterRegistration.Walk(ctx, nil, func(_ string, reg types.VoterRegistration) (bool, error) {
		if reg.Active {
			zkPubKeys = append(zkPubKeys, reg.ZkPublicKey)
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to walk voter registrations")
	}

	if len(zkPubKeys) == 0 {
		return nil, status.Error(codes.NotFound, "no active voter registrations")
	}

	tree := buildMerkleTreeFull(zkPubKeys)

	// Verify the rebuilt tree root matches the stored snapshot root.
	if !bytes.Equal(tree.Root(), snapshot.MerkleRoot) {
		return nil, status.Error(codes.FailedPrecondition,
			"voter set has changed since snapshot; rebuilt root does not match")
	}

	// Find the leaf for the requested public key.
	// Leaf = MiMC(publicKey, votingPower=1), matching the circuit.
	targetLeaf := zkcrypto.ComputeLeaf(pubKeyBytes, 1)
	leafIndex := tree.FindLeafIndex(targetLeaf)
	if leafIndex < 0 {
		return nil, status.Error(codes.NotFound, "public key not found in voter tree")
	}

	// Generate the Merkle proof.
	proof, err := tree.GetProof(leafIndex)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate merkle proof")
	}

	return &types.QueryVoterMerkleProofResponse{
		MerkleRoot:   snapshot.MerkleRoot,
		Leaf:         targetLeaf,
		LeafIndex:    int32(leafIndex),
		PathElements: proof.PathElements,
		PathIndices:  proof.PathIndices,
	}, nil
}
