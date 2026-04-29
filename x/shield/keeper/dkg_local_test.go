package keeper_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
)

func TestDKGLocalKeyStoreEnsureKey(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	// Generate key for round 1
	priv, pub, err := store.EnsureRegistrationKey(1)
	require.NoError(t, err)
	require.NotNil(t, priv)
	require.NotNil(t, pub)

	// Calling again should return the same key
	priv2, pub2, err := store.EnsureRegistrationKey(1)
	require.NoError(t, err)

	privBytes, _ := priv.MarshalBinary()
	priv2Bytes, _ := priv2.MarshalBinary()
	require.Equal(t, privBytes, priv2Bytes)

	pubBytes, _ := pub.MarshalBinary()
	pub2Bytes, _ := pub2.MarshalBinary()
	require.Equal(t, pubBytes, pub2Bytes)
}

func TestDKGLocalKeyStoreDifferentRounds(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	priv1, _, err := store.EnsureRegistrationKey(1)
	require.NoError(t, err)

	priv2, _, err := store.EnsureRegistrationKey(2)
	require.NoError(t, err)

	// Different rounds should produce different keys
	p1Bytes, _ := priv1.MarshalBinary()
	p2Bytes, _ := priv2.MarshalBinary()
	require.NotEqual(t, p1Bytes, p2Bytes)
}

func TestDKGLocalKeyStorePolynomial(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	// Must have a key first
	_, _, err := store.EnsureRegistrationKey(1)
	require.NoError(t, err)

	// Generate polynomial with threshold=2
	poly, commitments, err := store.GeneratePolynomial(1, 2)
	require.NoError(t, err)
	require.Len(t, poly, 2)
	require.Len(t, commitments, 2)

	// Calling again should return the same polynomial (idempotent)
	poly2, commitments2, err := store.GeneratePolynomial(1, 2)
	require.NoError(t, err)

	for i := range poly {
		p1, _ := poly[i].MarshalBinary()
		p2, _ := poly2[i].MarshalBinary()
		require.Equal(t, p1, p2)
	}
	for i := range commitments {
		c1, _ := commitments[i].MarshalBinary()
		c2, _ := commitments2[i].MarshalBinary()
		require.Equal(t, c1, c2)
	}
}

func TestDKGLocalKeyStorePolynomialWithoutKey(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	// Generate polynomial without key should fail
	_, _, err := store.GeneratePolynomial(1, 2)
	require.Error(t, err)
}

func TestDKGLocalKeyStoreEvaluatePolynomial(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	_, _, err := store.EnsureRegistrationKey(1)
	require.NoError(t, err)
	_, _, err = store.GeneratePolynomial(1, 2)
	require.NoError(t, err)

	// Evaluate at different points
	val1, err := store.EvaluatePolynomial(1, 1)
	require.NoError(t, err)
	require.NotNil(t, val1)

	val2, err := store.EvaluatePolynomial(1, 2)
	require.NoError(t, err)
	require.NotNil(t, val2)

	// Different points should give different values
	v1Bytes, _ := val1.MarshalBinary()
	v2Bytes, _ := val2.MarshalBinary()
	require.NotEqual(t, v1Bytes, v2Bytes)
}

func TestDKGLocalKeyStoreEvaluateWithoutPolynomial(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	_, _, err := store.EnsureRegistrationKey(1)
	require.NoError(t, err)

	// Evaluate without generating polynomial should fail
	_, err = store.EvaluatePolynomial(1, 1)
	require.Error(t, err)
}

func TestDKGLocalKeyStoreSignPoP(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	_, _, err := store.EnsureRegistrationKey(1)
	require.NoError(t, err)

	sig, err := store.SignPoP(1, "sprkdrmvaloper1test")
	require.NoError(t, err)
	require.NotEmpty(t, sig)
}

func TestDKGLocalKeyStoreSignPoPWithoutKey(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	_, err := store.SignPoP(1, "sprkdrmvaloper1test")
	require.Error(t, err)
}

func TestDKGLocalKeyStoreComputeDecryptionShare(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	_, _, err := store.EnsureRegistrationKey(1)
	require.NoError(t, err)

	// SHIELD-S2-2: ComputeDecryptionShare aggregates the validator's
	// polynomial evaluation at selfIdx plus any decrypted incoming
	// evaluations. With a single-validator setup (threshold=1, no incoming
	// ciphertexts), s_self = p_self(1) = a_{0,0}.
	_, _, err = store.GeneratePolynomial(1, 1)
	require.NoError(t, err)

	share, err := store.ComputeDecryptionShare(1, 5, 1, nil)
	require.NoError(t, err)
	require.NotEmpty(t, share)

	// Different epochs produce different shares (epoch tag enters the product).
	share2, err := store.ComputeDecryptionShare(1, 6, 1, nil)
	require.NoError(t, err)
	require.NotEqual(t, share, share2)

	// selfIdx == 0 is rejected.
	_, err = store.ComputeDecryptionShare(1, 5, 0, nil)
	require.Error(t, err)
}

func TestDKGLocalKeyStoreCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	// Generate keys for rounds 1, 2, 3
	_, _, err := store.EnsureRegistrationKey(1)
	require.NoError(t, err)
	_, _, err = store.EnsureRegistrationKey(2)
	require.NoError(t, err)
	_, _, err = store.EnsureRegistrationKey(3)
	require.NoError(t, err)

	// Verify files exist
	keyDir := filepath.Join(tmpDir, "config", "shield_dkg")
	entries, err := os.ReadDir(keyDir)
	require.NoError(t, err)
	require.Len(t, entries, 3)

	// Cleanup, keeping only round 3+
	store.Cleanup(3)

	entries, err = os.ReadDir(keyDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Round 3 key should still work
	_, _, err = store.EnsureRegistrationKey(3)
	require.NoError(t, err)
}

func TestDKGLocalKeyStoreCleanupNoDir(t *testing.T) {
	tmpDir := t.TempDir()
	store := keeper.NewDKGLocalKeyStore(tmpDir)

	// Cleanup without any keys should not panic
	require.NotPanics(t, func() {
		store.Cleanup(1)
	})
}
