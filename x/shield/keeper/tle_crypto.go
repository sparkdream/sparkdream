package keeper

import (
	"encoding/binary"
	"fmt"

	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/sign/schnorr"

	"sparkdream/tools/tle"
	"sparkdream/x/shield/types"
)

// tleSuite is the BN256 G1 suite used for DKG point arithmetic (registration
// keys, Feldman commitments, Shamir shares). The encryption itself lives in
// tools/tle so the chain, validators and clients share one implementation.
var tleSuite = bn256.NewSuiteG1()

// computeThreshold computes ceil(numerator / denominator * total).
// Thin wrapper around types.ComputeThreshold so the keeper and ABCI share
// one implementation.
func computeThreshold(numerator, denominator, total uint64) uint64 {
	return types.ComputeThreshold(numerator, denominator, total)
}

// keySetThreshold is the number of decryption shares a key set needs.
func keySetThreshold(ks types.TLEKeySet) uint64 {
	return computeThreshold(ks.ThresholdNumerator, ks.ThresholdDenominator, uint64(len(ks.ValidatorShares)))
}

// ReconstructEpochDecryptionKey interpolates validator decryption shares into
// the epoch key d = s·H1(epoch). Shares are expected to have passed
// VerifyDecryptionShare on the way into state; one unverified share would
// silently corrupt the interpolation for everyone.
func ReconstructEpochDecryptionKey(shares []types.ShieldDecryptionShare, ks types.TLEKeySet) ([]byte, error) {
	if len(shares) == 0 {
		return nil, fmt.Errorf("no shares provided")
	}
	threshold := keySetThreshold(ks)
	if uint64(len(shares)) < threshold {
		return nil, fmt.Errorf("insufficient shares: have %d, need %d", len(shares), threshold)
	}

	valIndexMap := make(map[string]int, len(ks.ValidatorShares))
	for _, vs := range ks.ValidatorShares {
		valIndexMap[vs.ValidatorAddress] = int(vs.ShareIndex)
	}
	indexed := make(map[int][]byte, len(shares))
	for _, s := range shares {
		idx, ok := valIndexMap[s.Validator]
		if !ok || idx == 0 {
			continue
		}
		indexed[idx] = s.Share
	}
	if uint64(len(indexed)) < threshold {
		return nil, fmt.Errorf("insufficient valid shares after filtering: have %d, need %d", len(indexed), threshold)
	}
	key, err := tle.ReconstructEpochKey(indexed, int(threshold), len(ks.ValidatorShares))
	if err != nil {
		return nil, fmt.Errorf("lagrange interpolation failed: %w", err)
	}
	return key, nil
}

// verifyDecryptionShare validates a validator's decryption share for an
// epoch: the pairing e(share, G2) == e(H1(epoch), pubShareG2) must hold,
// which proves share = s_i·H1(epoch) without revealing s_i.
func verifyDecryptionShare(shareBytes []byte, pubShareG2Bytes []byte, epoch uint64) error {
	return tle.VerifyDecryptionShare(shareBytes, pubShareG2Bytes, epoch)
}

// computeEpochTag is H1("shield_epoch_<N>"), the G1 point validators multiply
// by their secret share. Delegates to tools/tle.
func computeEpochTag(epoch uint64) ([]byte, error) {
	return tle.EpochTag(epoch).MarshalBinary()
}

// decryptPayload opens an encrypted-batch payload with the reconstructed
// epoch key. Format and KDF are tools/tle's; the epoch is bound into the key
// derivation so a payload cannot be replayed under another epoch's label.
func decryptPayload(encryptedPayload []byte, decryptionKey []byte, epoch uint64) ([]byte, error) {
	return tle.Decrypt(decryptionKey, epoch, encryptedPayload)
}

// PoPMessage is what a DKG proof of possession signs: the operator address
// and the round, so a PoP from one round cannot be replayed into another.
func PoPMessage(validatorAddr string, round uint64) []byte {
	msg := append([]byte("sparkdream/shield/dkg/pop/v1|"), validatorAddr...)
	var be [8]byte
	binary.BigEndian.PutUint64(be[:], round)
	return append(msg, be[:]...)
}

// VerifyProofOfPossession verifies that the validator knows the secret key
// behind pubShareBytes: a Schnorr signature over PoPMessage(addr, round).
func VerifyProofOfPossession(pubShareBytes []byte, popBytes []byte, validatorAddr string, round uint64) error {
	if len(popBytes) == 0 {
		return fmt.Errorf("proof of possession is empty")
	}

	pubPoint := tleSuite.Point()
	if err := pubPoint.UnmarshalBinary(pubShareBytes); err != nil {
		return fmt.Errorf("invalid public share: %w", err)
	}
	if pubPoint.Equal(tleSuite.G1().Point().Null()) {
		return fmt.Errorf("public share is the identity")
	}

	if err := schnorr.Verify(tleSuite, pubPoint, PoPMessage(validatorAddr, round), popBytes); err != nil {
		return fmt.Errorf("proof of possession verification failed: %w", err)
	}

	return nil
}
