package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/sign/schnorr"

	"sparkdream/x/shield/types"
)

func TestComputeThreshold(t *testing.T) {
	t.Run("2/3 of 3 = 2", func(t *testing.T) {
		require.Equal(t, uint64(2), computeThreshold(2, 3, 3))
	})

	t.Run("2/3 of 4 = 3 (ceiling)", func(t *testing.T) {
		require.Equal(t, uint64(3), computeThreshold(2, 3, 4))
	})

	t.Run("1/2 of 4 = 2", func(t *testing.T) {
		require.Equal(t, uint64(2), computeThreshold(1, 2, 4))
	})

	t.Run("1/1 of 5 = 5", func(t *testing.T) {
		require.Equal(t, uint64(5), computeThreshold(1, 1, 5))
	})

	t.Run("zero denominator returns total", func(t *testing.T) {
		require.Equal(t, uint64(7), computeThreshold(2, 0, 7))
	})

	t.Run("1/3 of 10 = 4 (ceiling)", func(t *testing.T) {
		require.Equal(t, uint64(4), computeThreshold(1, 3, 10))
	})

	t.Run("3/5 of 5 = 3", func(t *testing.T) {
		require.Equal(t, uint64(3), computeThreshold(3, 5, 5))
	})
}

func TestComputeEpochTag(t *testing.T) {
	t.Run("produces valid G1 point", func(t *testing.T) {
		tag, err := computeEpochTag(1)
		require.NoError(t, err)
		require.NotEmpty(t, tag)

		// Verify it's a valid G1 point
		point := tleSuite.G1().Point()
		require.NoError(t, point.UnmarshalBinary(tag))
	})

	t.Run("deterministic", func(t *testing.T) {
		tag1, err := computeEpochTag(42)
		require.NoError(t, err)
		tag2, err := computeEpochTag(42)
		require.NoError(t, err)
		require.Equal(t, tag1, tag2)
	})

	t.Run("different epochs produce different tags", func(t *testing.T) {
		tag1, _ := computeEpochTag(1)
		tag2, _ := computeEpochTag(2)
		require.NotEqual(t, tag1, tag2)
	})
}

func TestVerifyDecryptionShare(t *testing.T) {
	privKey := tleSuite.Scalar().Pick(tleSuite.RandomStream())
	pubKey := tleSuite.Point().Mul(privKey, nil)
	pubKeyBytes, _ := pubKey.MarshalBinary()

	// Compute G2 public share: pubKey_G2 = privKey * G2_gen
	g2Suite := bn256.NewSuiteG2()
	pubKeyG2 := g2Suite.G2().Point().Mul(privKey, nil)
	pubKeyG2Bytes, _ := pubKeyG2.MarshalBinary()

	epochTagPoint := tleSuite.Point().Pick(tleSuite.XOF([]byte("shield_epoch_1")))
	epochTagBytes, _ := epochTagPoint.MarshalBinary()

	sharePoint := tleSuite.Point().Mul(privKey, epochTagPoint)
	shareBytes, _ := sharePoint.MarshalBinary()

	t.Run("valid share passes pairing check", func(t *testing.T) {
		err := verifyDecryptionShare(shareBytes, pubKeyBytes, pubKeyG2Bytes, epochTagBytes, 1, 1)
		require.NoError(t, err)
	})

	t.Run("nil G2 share rejected", func(t *testing.T) {
		err := verifyDecryptionShare(shareBytes, pubKeyBytes, nil, epochTagBytes, 1, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "G2 public key share is required")
	})

	t.Run("pairing check rejects wrong share", func(t *testing.T) {
		wrongKey := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		wrongSharePoint := tleSuite.Point().Mul(wrongKey, epochTagPoint)
		wrongShareBytes, _ := wrongSharePoint.MarshalBinary()
		err := verifyDecryptionShare(wrongShareBytes, pubKeyBytes, pubKeyG2Bytes, epochTagBytes, 1, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "pairing check failed")
	})

	t.Run("pairing check rejects mismatched G2 key", func(t *testing.T) {
		// G2 key for a DIFFERENT private key
		wrongPriv := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		wrongG2 := g2Suite.G2().Point().Mul(wrongPriv, nil)
		wrongG2Bytes, _ := wrongG2.MarshalBinary()
		err := verifyDecryptionShare(shareBytes, pubKeyBytes, wrongG2Bytes, epochTagBytes, 1, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "pairing check failed")
	})

	t.Run("share index mismatch rejected", func(t *testing.T) {
		err := verifyDecryptionShare(shareBytes, pubKeyBytes, pubKeyG2Bytes, epochTagBytes, 2, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "share index mismatch")
	})

	t.Run("invalid share bytes rejected", func(t *testing.T) {
		err := verifyDecryptionShare([]byte("not a point"), pubKeyBytes, pubKeyG2Bytes, epochTagBytes, 1, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a valid G1 point")
	})

	t.Run("identity element share rejected", func(t *testing.T) {
		identity := tleSuite.G1().Point().Null()
		identityBytes, _ := identity.MarshalBinary()
		err := verifyDecryptionShare(identityBytes, pubKeyBytes, pubKeyG2Bytes, epochTagBytes, 1, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "identity element")
	})

	t.Run("invalid epoch tag rejected", func(t *testing.T) {
		err := verifyDecryptionShare(shareBytes, pubKeyBytes, pubKeyG2Bytes, []byte("bad"), 1, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid epoch tag")
	})

	t.Run("identity G2 share rejected", func(t *testing.T) {
		identityG2 := g2Suite.G2().Point().Null()
		identityG2Bytes, _ := identityG2.MarshalBinary()
		err := verifyDecryptionShare(shareBytes, pubKeyBytes, identityG2Bytes, epochTagBytes, 1, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "G2 public key share is the identity element")
	})
}

func TestVerifyProofOfPossession(t *testing.T) {
	privKey := tleSuite.Scalar().Pick(tleSuite.RandomStream())
	pubKey := tleSuite.Point().Mul(privKey, nil)
	pubKeyBytes, _ := pubKey.MarshalBinary()
	validatorAddr := "sprkdrmvaloper1testval"
	pop, err := schnorr.Sign(tleSuite, privKey, []byte(validatorAddr))
	require.NoError(t, err)

	t.Run("valid PoP passes", func(t *testing.T) {
		err := verifyProofOfPossession(pubKeyBytes, pop, validatorAddr)
		require.NoError(t, err)
	})

	t.Run("empty PoP rejected", func(t *testing.T) {
		err := verifyProofOfPossession(pubKeyBytes, nil, validatorAddr)
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty")
	})

	t.Run("wrong address fails", func(t *testing.T) {
		err := verifyProofOfPossession(pubKeyBytes, pop, "sprkdrmvaloper1other")
		require.Error(t, err)
	})

	t.Run("wrong key fails", func(t *testing.T) {
		otherPriv := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		otherPub := tleSuite.Point().Mul(otherPriv, nil)
		otherPubBytes, _ := otherPub.MarshalBinary()
		err := verifyProofOfPossession(otherPubBytes, pop, validatorAddr)
		require.Error(t, err)
	})

	t.Run("invalid pub share bytes rejected", func(t *testing.T) {
		err := verifyProofOfPossession([]byte("bad"), pop, validatorAddr)
		require.Error(t, err)
	})
}

func TestReconstructEpochDecryptionKey(t *testing.T) {
	t.Run("no shares returns error", func(t *testing.T) {
		ks := types.TLEKeySet{ThresholdNumerator: 2, ThresholdDenominator: 3}
		_, err := ReconstructEpochDecryptionKey(nil, ks)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no shares")
	})

	t.Run("insufficient shares returns error", func(t *testing.T) {
		ks := types.TLEKeySet{
			ThresholdNumerator:   2,
			ThresholdDenominator: 3,
			ValidatorShares: []*types.TLEValidatorPublicShare{
				{ValidatorAddress: "val1", ShareIndex: 1},
				{ValidatorAddress: "val2", ShareIndex: 2},
				{ValidatorAddress: "val3", ShareIndex: 3},
			},
		}
		point := tleSuite.G1().Point().Pick(tleSuite.RandomStream())
		pointBytes, _ := point.MarshalBinary()
		shares := []types.ShieldDecryptionShare{
			{Epoch: 1, Validator: "val1", Share: pointBytes},
		}
		_, err := ReconstructEpochDecryptionKey(shares, ks)
		require.Error(t, err)
		require.Contains(t, err.Error(), "insufficient")
	})

	t.Run("shares from unknown validators are filtered", func(t *testing.T) {
		ks := types.TLEKeySet{
			ThresholdNumerator:   1,
			ThresholdDenominator: 1,
			ValidatorShares: []*types.TLEValidatorPublicShare{
				{ValidatorAddress: "val1", ShareIndex: 1},
			},
		}
		point := tleSuite.G1().Point().Pick(tleSuite.RandomStream())
		pointBytes, _ := point.MarshalBinary()
		shares := []types.ShieldDecryptionShare{
			{Epoch: 1, Validator: "unknown_val", Share: pointBytes},
		}
		_, err := ReconstructEpochDecryptionKey(shares, ks)
		require.Error(t, err)
		require.Contains(t, err.Error(), "insufficient valid shares")
	})

	t.Run("malformed share bytes filtered", func(t *testing.T) {
		ks := types.TLEKeySet{
			ThresholdNumerator:   1,
			ThresholdDenominator: 1,
			ValidatorShares: []*types.TLEValidatorPublicShare{
				{ValidatorAddress: "val1", ShareIndex: 1},
			},
		}
		shares := []types.ShieldDecryptionShare{
			{Epoch: 1, Validator: "val1", Share: []byte("not a valid G1 point")},
		}
		_, err := ReconstructEpochDecryptionKey(shares, ks)
		require.Error(t, err)
	})
}

func TestComputeMasterPublicKey(t *testing.T) {
	t.Run("no shares returns error", func(t *testing.T) {
		_, err := computeMasterPublicKey(nil, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no validator shares")
	})

	t.Run("insufficient valid shares returns error", func(t *testing.T) {
		shares := []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("bad"), ShareIndex: 1},
		}
		_, err := computeMasterPublicKey(shares, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "insufficient valid shares")
	})

	t.Run("valid shares produce master key", func(t *testing.T) {
		p1 := tleSuite.G1().Point().Pick(tleSuite.RandomStream())
		p1Bytes, _ := p1.MarshalBinary()
		p2 := tleSuite.G1().Point().Pick(tleSuite.RandomStream())
		p2Bytes, _ := p2.MarshalBinary()

		shares := []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: p1Bytes, ShareIndex: 1},
			{ValidatorAddress: "val2", PublicShare: p2Bytes, ShareIndex: 2},
		}
		mpk, err := computeMasterPublicKey(shares, 1)
		require.NoError(t, err)
		require.NotEmpty(t, mpk)

		result := tleSuite.G1().Point()
		require.NoError(t, result.UnmarshalBinary(mpk))
	})
}

func TestDecryptPayload(t *testing.T) {
	t.Run("invalid decryption key rejected", func(t *testing.T) {
		_, err := decryptPayload([]byte("encrypted"), []byte("not a G1 point"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid decryption key")
	})

	t.Run("decryption fails with wrong key", func(t *testing.T) {
		// Generate two different key points
		keyPoint := tleSuite.G1().Point().Pick(tleSuite.RandomStream())
		keyBytes, _ := keyPoint.MarshalBinary()

		wrongKeyPoint := tleSuite.G1().Point().Pick(tleSuite.RandomStream())
		wrongKeyBytes, _ := wrongKeyPoint.MarshalBinary()

		// Derive the scalar from the correct key and encrypt with it
		derivedScalar := tleSuite.Scalar().Pick(tleSuite.XOF(keyBytes))
		derivedPub := tleSuite.Point().Mul(derivedScalar, nil)
		encrypted, err := ecies.Encrypt(tleSuite, derivedPub, []byte("test"), nil)
		require.NoError(t, err)

		// Try to decrypt with the wrong key — should fail
		_, err = decryptPayload(encrypted, wrongKeyBytes)
		require.Error(t, err)
		require.Contains(t, err.Error(), "ECIES decryption failed")
	})

	t.Run("round trip encrypt then decrypt", func(t *testing.T) {
		// Generate a "decryption key" as a G1 point
		keyPoint := tleSuite.G1().Point().Pick(tleSuite.RandomStream())
		keyBytes, _ := keyPoint.MarshalBinary()

		// Derive the scalar the same way decryptPayload does
		derivedScalar := tleSuite.Scalar().Pick(tleSuite.XOF(keyBytes))
		derivedPub := tleSuite.Point().Mul(derivedScalar, nil)

		// Encrypt with the derived public key
		plaintext := []byte("hello shield world")
		encrypted, err := ecies.Encrypt(tleSuite, derivedPub, plaintext, nil)
		require.NoError(t, err)

		// Decrypt should recover the plaintext
		recovered, err := decryptPayload(encrypted, keyBytes)
		require.NoError(t, err)
		require.Equal(t, plaintext, recovered)
	})
}
