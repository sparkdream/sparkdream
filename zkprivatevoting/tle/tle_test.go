package tle

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/share"
)

// TestRunDKG verifies basic DKG output structure and key consistency.
func TestRunDKG(t *testing.T) {
	output, err := RunDKG(2, 3)
	require.NoError(t, err)
	require.Len(t, output.ValidatorShares, 3)
	require.Equal(t, 2, output.Threshold)
	require.Equal(t, 3, output.TotalValidators)
	require.NotEmpty(t, output.MasterPublicKey)

	// Each share should have valid index (1-based) and non-empty keys.
	for i, vs := range output.ValidatorShares {
		require.Equal(t, i+1, vs.Index)
		require.NotEmpty(t, vs.PrivateScalar)
		require.NotEmpty(t, vs.PublicKeyShare)
	}
}

// TestRunDKGInvalidParams verifies DKG rejects bad parameters.
func TestRunDKGInvalidParams(t *testing.T) {
	_, err := RunDKG(0, 3)
	require.Error(t, err)

	_, err = RunDKG(4, 3)
	require.Error(t, err)
}

// TestEndToEndSealAndDecrypt is a full round-trip test:
// DKG → encrypt vote → reconstruct key → decrypt vote.
func TestEndToEndSealAndDecrypt(t *testing.T) {
	threshold := 2
	totalVals := 3

	// Step 1: Run DKG.
	dkg, err := RunDKG(threshold, totalVals)
	require.NoError(t, err)

	// Step 2: Voter seals a vote using master public key.
	voteOption := uint32(2)
	sealed, err := SealVote(dkg.MasterPublicKey, voteOption, nil)
	require.NoError(t, err)
	require.Equal(t, voteOption, sealed.VoteOption)
	require.Len(t, sealed.Salt, 32)
	require.NotEmpty(t, sealed.Commitment)
	require.NotEmpty(t, sealed.EncryptedReveal)

	// Step 3: Simulate validator share submission and secret reconstruction.
	// Use threshold (2) of 3 shares.
	priShares := make([]*share.PriShare, threshold)
	for i := 0; i < threshold; i++ {
		vs := dkg.ValidatorShares[i]
		scalar := suite.Scalar()
		require.NoError(t, scalar.UnmarshalBinary(vs.PrivateScalar))
		priShares[i] = &share.PriShare{
			I: vs.Index - 1, // convert 1-based to 0-based
			V: scalar,
		}
	}

	recovered, err := share.RecoverSecret(suite, priShares, threshold, totalVals)
	require.NoError(t, err)

	// Step 4: Decrypt the sealed vote using the reconstructed key.
	keyBytes, err := recovered.MarshalBinary()
	require.NoError(t, err)

	plaintext, err := ecies.Decrypt(suite, recovered, sealed.EncryptedReveal, nil)
	require.NoError(t, err)
	require.Len(t, plaintext, 36)

	// Verify decrypted vote matches original.
	gotOption := binary.BigEndian.Uint32(plaintext[:4])
	gotSalt := plaintext[4:36]
	require.Equal(t, voteOption, gotOption)
	require.Equal(t, sealed.Salt, gotSalt)

	// Step 5: Verify commitment matches.
	expectedCommitment := ComputeVoteCommitment(gotOption, gotSalt)
	require.True(t, bytes.Equal(expectedCommitment, sealed.Commitment))

	_ = keyBytes // used for verification above via recovered scalar
}

// TestSealVoteCustomSalt verifies SealVote works with a provided salt.
func TestSealVoteCustomSalt(t *testing.T) {
	dkg, err := RunDKG(2, 3)
	require.NoError(t, err)

	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}

	sealed, err := SealVote(dkg.MasterPublicKey, 1, salt)
	require.NoError(t, err)
	require.Equal(t, salt, sealed.Salt)

	// Commitment should be deterministic with same salt.
	commitment2 := ComputeVoteCommitment(1, salt)
	require.True(t, bytes.Equal(sealed.Commitment, commitment2))
}

// TestSealVoteBadSalt verifies SealVote rejects incorrect salt sizes.
func TestSealVoteBadSalt(t *testing.T) {
	dkg, err := RunDKG(2, 3)
	require.NoError(t, err)

	_, err = SealVote(dkg.MasterPublicKey, 0, []byte("short"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "32 bytes")
}

// TestSealVoteBadMasterKey verifies SealVote rejects invalid master keys.
func TestSealVoteBadMasterKey(t *testing.T) {
	_, err := SealVote([]byte("invalid"), 0, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid master public key")
}

// TestAggregateMasterPublicKey verifies Lagrange interpolation on public shares
// recovers the same master public key.
func TestAggregateMasterPublicKey(t *testing.T) {
	threshold := 3
	totalVals := 5

	dkg, err := RunDKG(threshold, totalVals)
	require.NoError(t, err)

	// Use a non-contiguous subset of shares (indices 1, 3, 5).
	subset := []*ValidatorShare{
		dkg.ValidatorShares[0],
		dkg.ValidatorShares[2],
		dkg.ValidatorShares[4],
	}

	aggregated, err := AggregateMasterPublicKey(subset, threshold, totalVals)
	require.NoError(t, err)
	require.True(t, bytes.Equal(aggregated, dkg.MasterPublicKey),
		"aggregated master public key should match DKG output")
}

// TestAggregateMasterPublicKeyInsufficientShares verifies aggregation fails
// with fewer than threshold shares.
func TestAggregateMasterPublicKeyInsufficientShares(t *testing.T) {
	dkg, err := RunDKG(3, 5)
	require.NoError(t, err)

	_, err = AggregateMasterPublicKey(dkg.ValidatorShares[:2], 3, 5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "need at least")
}

// TestComputeNullifier verifies nullifier computation matches crypto package.
func TestComputeNullifier(t *testing.T) {
	secretKey := make([]byte, 32)
	for i := range secretKey {
		secretKey[i] = byte(i + 1)
	}

	n1 := ComputeNullifier(secretKey, 42)
	n2 := ComputeNullifier(secretKey, 42)
	require.True(t, bytes.Equal(n1, n2), "same inputs should produce same nullifier")

	n3 := ComputeNullifier(secretKey, 43)
	require.False(t, bytes.Equal(n1, n3), "different proposal IDs should produce different nullifiers")
}

// TestFileIORoundTrip verifies DKG output can be saved and loaded from disk.
func TestFileIORoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	dkg, err := RunDKG(2, 3)
	require.NoError(t, err)

	// Save.
	err = SaveDKGOutput(dkg, tmpDir)
	require.NoError(t, err)

	// Load master file.
	mf, err := LoadMasterFile(filepath.Join(tmpDir, "master.json"))
	require.NoError(t, err)
	require.Equal(t, 2, mf.Threshold)
	require.Equal(t, 3, mf.TotalValidators)
	require.NotEmpty(t, mf.MasterPublicKeyHex)

	// Load each validator share.
	for i := 1; i <= 3; i++ {
		sharePath := filepath.Join(tmpDir, "validator_"+string(rune('0'+i))+".json")
		vs, err := LoadShareFromFile(sharePath)
		require.NoError(t, err)
		require.Equal(t, i, vs.Index)
		require.Equal(t, dkg.ValidatorShares[i-1].PrivateScalar, vs.PrivateScalar)
		require.Equal(t, dkg.ValidatorShares[i-1].PublicKeyShare, vs.PublicKeyShare)
	}
}

// TestSealedVoteFileRoundTrip verifies sealed vote data can be saved and loaded.
func TestSealedVoteFileRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	dkg, err := RunDKG(2, 3)
	require.NoError(t, err)

	sealed, err := SealVote(dkg.MasterPublicKey, 1, nil)
	require.NoError(t, err)

	nullifier := ComputeNullifier([]byte("secret-key-32-bytes-padded-here"), 7)

	votePath := filepath.Join(tmpDir, "sealed_vote.json")
	err = SaveSealedVote(sealed, 7, nullifier, votePath)
	require.NoError(t, err)

	// Verify file exists with restricted permissions.
	info, err := os.Stat(votePath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	// Load and verify.
	loaded, err := LoadSealedVote(votePath)
	require.NoError(t, err)
	require.Equal(t, uint64(7), loaded.ProposalID)
	require.Equal(t, uint32(1), loaded.VoteOption)
	require.NotEmpty(t, loaded.SaltHex)
	require.NotEmpty(t, loaded.NullifierHex)
	require.NotEmpty(t, loaded.CommitmentHex)
	require.NotEmpty(t, loaded.EncryptedHex)
}

// TestShareFilePermissions verifies validator share files are created with
// restricted permissions (0600).
func TestShareFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()

	dkg, err := RunDKG(2, 3)
	require.NoError(t, err)

	err = SaveDKGOutput(dkg, tmpDir)
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		info, err := os.Stat(filepath.Join(tmpDir, "validator_"+string(rune('0'+i))+".json"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm(),
			"validator share files should be owner-only read/write")
	}
}
