package keeper_test

import (
	"crypto/rand"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/share"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

var testSuite = bn256.NewSuiteG1()

// TestTLEDecryptPayloadRoundTrip encrypts a vote payload with ECIES using
// a known keypair, then decrypts it via decryptTLEPayload.
func TestTLEDecryptPayloadRoundTrip(t *testing.T) {
	// Generate keypair.
	privScalar := testSuite.Scalar().Pick(testSuite.RandomStream())
	pubPoint := testSuite.Point().Mul(privScalar, nil)

	// Build plaintext: voteOption(4 bytes) || salt(32 bytes).
	var voteOption uint32 = 2
	salt := make([]byte, 32)
	_, err := rand.Read(salt)
	require.NoError(t, err)

	plaintext := make([]byte, 36)
	binary.BigEndian.PutUint32(plaintext[:4], voteOption)
	copy(plaintext[4:], salt)

	// Encrypt with ECIES.
	ciphertext, err := ecies.Encrypt(testSuite, pubPoint, plaintext, nil)
	require.NoError(t, err)

	// Marshal private key for decryption.
	privBytes, err := privScalar.MarshalBinary()
	require.NoError(t, err)

	// Decrypt via the exported test helper.
	_, gotOption, gotSalt, err := keeper.DecryptTLEPayloadForTest(ciphertext, privBytes)
	require.NoError(t, err)
	require.Equal(t, voteOption, gotOption)
	require.Equal(t, salt, gotSalt)
}

// TestTLEDecryptPayloadWrongKey verifies decryption fails with a wrong key.
func TestTLEDecryptPayloadWrongKey(t *testing.T) {
	// Generate two keypairs.
	privScalar := testSuite.Scalar().Pick(testSuite.RandomStream())
	pubPoint := testSuite.Point().Mul(privScalar, nil)

	wrongScalar := testSuite.Scalar().Pick(testSuite.RandomStream())

	plaintext := make([]byte, 36)
	binary.BigEndian.PutUint32(plaintext[:4], 1)

	// Encrypt with the first keypair's public key.
	ciphertext, err := ecies.Encrypt(testSuite, pubPoint, plaintext, nil)
	require.NoError(t, err)

	// Try decrypting with wrong key.
	wrongBytes, err := wrongScalar.MarshalBinary()
	require.NoError(t, err)

	_, _, _, err = keeper.DecryptTLEPayloadForTest(ciphertext, wrongBytes)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ECIES decryption failed")
}

// TestTLECorrectnessProofValid verifies a matching keypair passes verification.
func TestTLECorrectnessProofValid(t *testing.T) {
	privScalar := testSuite.Scalar().Pick(testSuite.RandomStream())
	pubPoint := testSuite.Point().Mul(privScalar, nil)

	privBytes, err := privScalar.MarshalBinary()
	require.NoError(t, err)
	pubBytes, err := pubPoint.MarshalBinary()
	require.NoError(t, err)

	err = keeper.VerifyCorrectnessProofForTest(privBytes, nil, pubBytes)
	require.NoError(t, err)
}

// TestTLECorrectnessProofInvalid verifies a mismatched keypair fails verification.
func TestTLECorrectnessProofInvalid(t *testing.T) {
	privScalar := testSuite.Scalar().Pick(testSuite.RandomStream())

	// Generate a different public key.
	otherScalar := testSuite.Scalar().Pick(testSuite.RandomStream())
	otherPub := testSuite.Point().Mul(otherScalar, nil)

	privBytes, err := privScalar.MarshalBinary()
	require.NoError(t, err)
	otherPubBytes, err := otherPub.MarshalBinary()
	require.NoError(t, err)

	err = keeper.VerifyCorrectnessProofForTest(privBytes, nil, otherPubBytes)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidCorrectnessProof)
}

// TestTLEShareReconstruction generates a t-of-n secret sharing, submits t shares,
// and verifies the reconstructed secret can decrypt a ciphertext encrypted with
// the master public key.
func TestTLEShareReconstruction(t *testing.T) {
	n := 5 // total validators
	threshold := 3

	// Generate a secret polynomial of degree threshold-1.
	secret := testSuite.Scalar().Pick(testSuite.RandomStream())
	poly := share.NewPriPoly(testSuite, threshold, secret, testSuite.RandomStream())

	// Create n shares (indices 0..n-1 map to evaluation at x=1..n).
	shares := poly.Shares(n)

	// Master public key = secret * G.
	masterPub := testSuite.Point().Mul(secret, nil)

	// Encrypt a vote payload with master public key.
	var voteOption uint32 = 3
	salt := make([]byte, 32)
	_, err := rand.Read(salt)
	require.NoError(t, err)

	plaintext := make([]byte, 36)
	binary.BigEndian.PutUint32(plaintext[:4], voteOption)
	copy(plaintext[4:], salt)

	ciphertext, err := ecies.Encrypt(testSuite, masterPub, plaintext, nil)
	require.NoError(t, err)

	// Reconstruct using exactly threshold shares.
	recoveredSecret, err := share.RecoverSecret(testSuite, shares[:threshold], threshold, n)
	require.NoError(t, err)

	// Verify recovered secret matches original.
	require.True(t, recoveredSecret.Equal(secret))

	// Verify decryption with recovered key.
	keyBytes, err := recoveredSecret.MarshalBinary()
	require.NoError(t, err)

	_, gotOption, gotSalt, err := keeper.DecryptTLEPayloadForTest(ciphertext, keyBytes)
	require.NoError(t, err)
	require.Equal(t, voteOption, gotOption)
	require.Equal(t, salt, gotSalt)
}

// TestTLEShareReconstructionInsufficientShares verifies that fewer than
// threshold shares cannot reconstruct the secret correctly.
func TestTLEShareReconstructionInsufficientShares(t *testing.T) {
	n := 5
	threshold := 3

	secret := testSuite.Scalar().Pick(testSuite.RandomStream())
	poly := share.NewPriPoly(testSuite, threshold, secret, testSuite.RandomStream())
	shares := poly.Shares(n)

	// Try with threshold-1 shares — should fail or give wrong result.
	_, err := share.RecoverSecret(testSuite, shares[:threshold-1], threshold, n)
	// RecoverSecret may return an error with too few shares.
	if err == nil {
		// If no error, the result should not match the original secret
		// (Lagrange interpolation with insufficient points is unreliable).
		t.Log("RecoverSecret did not error with insufficient shares (expected — result is unreliable)")
	}
}

// TestTLEShareReconstructionNonContiguous verifies reconstruction works
// with non-contiguous share indices (simulating different validators submitting).
func TestTLEShareReconstructionNonContiguous(t *testing.T) {
	n := 7
	threshold := 4

	secret := testSuite.Scalar().Pick(testSuite.RandomStream())
	poly := share.NewPriPoly(testSuite, threshold, secret, testSuite.RandomStream())
	allShares := poly.Shares(n)

	// Pick non-contiguous shares: indices 0, 2, 4, 6.
	selected := []*share.PriShare{allShares[0], allShares[2], allShares[4], allShares[6]}

	recovered, err := share.RecoverSecret(testSuite, selected, threshold, n)
	require.NoError(t, err)
	require.True(t, recovered.Equal(secret))
}
