package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/sign/schnorr"

	"sparkdream/tools/tle"
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
	g2Suite := bn256.NewSuiteG2()
	pubKeyG2Bytes, _ := g2Suite.G2().Point().Mul(privKey, nil).MarshalBinary()

	const epoch = uint64(1)
	shareBytes := tle.DecryptionShare(privKey, epoch)

	t.Run("valid share passes pairing check", func(t *testing.T) {
		require.NoError(t, verifyDecryptionShare(shareBytes, pubKeyG2Bytes, epoch))
	})

	t.Run("nil G2 share rejected", func(t *testing.T) {
		err := verifyDecryptionShare(shareBytes, nil, epoch)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no G2 public share")
	})

	t.Run("pairing check rejects wrong share", func(t *testing.T) {
		wrongKey := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		err := verifyDecryptionShare(tle.DecryptionShare(wrongKey, epoch), pubKeyG2Bytes, epoch)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not match")
	})

	t.Run("pairing check rejects mismatched G2 key", func(t *testing.T) {
		wrongPriv := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		wrongG2Bytes, _ := g2Suite.G2().Point().Mul(wrongPriv, nil).MarshalBinary()
		err := verifyDecryptionShare(shareBytes, wrongG2Bytes, epoch)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not match")
	})

	t.Run("share for another epoch rejected", func(t *testing.T) {
		err := verifyDecryptionShare(shareBytes, pubKeyG2Bytes, epoch+1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not match")
	})

	t.Run("invalid share bytes rejected", func(t *testing.T) {
		err := verifyDecryptionShare([]byte("not a point"), pubKeyG2Bytes, epoch)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a G1 point")
	})

	t.Run("identity element share rejected", func(t *testing.T) {
		identityBytes, _ := tleSuite.G1().Point().Null().MarshalBinary()
		err := verifyDecryptionShare(identityBytes, pubKeyG2Bytes, epoch)
		require.Error(t, err)
		require.Contains(t, err.Error(), "identity")
	})

	t.Run("identity G2 share rejected", func(t *testing.T) {
		identityG2Bytes, _ := g2Suite.G2().Point().Null().MarshalBinary()
		err := verifyDecryptionShare(shareBytes, identityG2Bytes, epoch)
		require.Error(t, err)
		require.Contains(t, err.Error(), "identity")
	})
}

func TestVerifyProofOfPossession(t *testing.T) {
	privKey := tleSuite.Scalar().Pick(tleSuite.RandomStream())
	pubKey := tleSuite.Point().Mul(privKey, nil)
	pubKeyBytes, _ := pubKey.MarshalBinary()
	validatorAddr := "sprkdrmvaloper1testval"
	const round = uint64(3)
	pop, err := schnorr.Sign(tleSuite, privKey, PoPMessage(validatorAddr, round))
	require.NoError(t, err)

	t.Run("valid PoP passes", func(t *testing.T) {
		err := VerifyProofOfPossession(pubKeyBytes, pop, validatorAddr, round)
		require.NoError(t, err)
	})

	t.Run("empty PoP rejected", func(t *testing.T) {
		err := VerifyProofOfPossession(pubKeyBytes, nil, validatorAddr, round)
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty")
	})

	t.Run("wrong address fails", func(t *testing.T) {
		err := VerifyProofOfPossession(pubKeyBytes, pop, "sprkdrmvaloper1other", round)
		require.Error(t, err)
	})

	t.Run("another round fails (no replay across rounds)", func(t *testing.T) {
		err := VerifyProofOfPossession(pubKeyBytes, pop, validatorAddr, round+1)
		require.Error(t, err)
	})

	t.Run("wrong key fails", func(t *testing.T) {
		otherPriv := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		otherPub := tleSuite.Point().Mul(otherPriv, nil)
		otherPubBytes, _ := otherPub.MarshalBinary()
		err := VerifyProofOfPossession(otherPubBytes, pop, validatorAddr, round)
		require.Error(t, err)
	})

	t.Run("identity key rejected", func(t *testing.T) {
		id, _ := tleSuite.G1().Point().Null().MarshalBinary()
		err := VerifyProofOfPossession(id, pop, validatorAddr, round)
		require.Error(t, err)
	})

	t.Run("invalid pub share bytes rejected", func(t *testing.T) {
		err := VerifyProofOfPossession([]byte("bad"), pop, validatorAddr, round)
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

func TestDecryptPayload(t *testing.T) {
	master := tleSuite.Scalar().Pick(tleSuite.RandomStream())
	mpkG2 := tle.MasterPublicKeyG2(master)

	t.Run("invalid decryption key rejected", func(t *testing.T) {
		ct, err := tle.Encrypt(mpkG2, 3, []byte("x"))
		require.NoError(t, err)
		_, err = decryptPayload(ct, []byte("not a G1 point"), 3)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a G1 point")
	})

	t.Run("decryption fails with wrong key", func(t *testing.T) {
		ct, err := tle.Encrypt(mpkG2, 3, []byte("test"))
		require.NoError(t, err)
		other := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		_, err = decryptPayload(ct, tle.EpochDecryptionKey(other, 3), 3)
		require.ErrorIs(t, err, tle.ErrDecrypt)
		// The right master secret, another epoch's key.
		_, err = decryptPayload(ct, tle.EpochDecryptionKey(master, 4), 3)
		require.ErrorIs(t, err, tle.ErrDecrypt)
	})

	t.Run("round trip: client encrypts to the G2 master key, chain decrypts with the epoch key", func(t *testing.T) {
		plaintext := []byte("hello shield world")
		ct, err := tle.Encrypt(mpkG2, 3, plaintext)
		require.NoError(t, err)
		recovered, err := decryptPayload(ct, tle.EpochDecryptionKey(master, 3), 3)
		require.NoError(t, err)
		require.Equal(t, plaintext, recovered)
	})
}
