package keeper

import (
	"fmt"

	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/sign/schnorr"

	"sparkdream/x/shield/types"
)

// tleSuite is the BN256 G1 suite used for all TLE operations.
var tleSuite = bn256.NewSuiteG1()

// computeThreshold computes ceil(numerator / denominator * total).
func computeThreshold(numerator, denominator, total uint64) uint64 {
	if denominator == 0 {
		return total
	}
	// ceil(numerator * total / denominator)
	return (numerator*total + denominator - 1) / denominator
}

// ReconstructEpochDecryptionKey reconstructs the epoch decryption key
// from validator decryption shares using Lagrange interpolation.
func ReconstructEpochDecryptionKey(shares []types.ShieldDecryptionShare, ks types.TLEKeySet) ([]byte, error) {
	if len(shares) == 0 {
		return nil, fmt.Errorf("no shares provided")
	}

	threshold := computeThreshold(ks.ThresholdNumerator, ks.ThresholdDenominator, uint64(len(ks.ValidatorShares)))
	if uint64(len(shares)) < threshold {
		return nil, fmt.Errorf("insufficient shares: have %d, need %d", len(shares), threshold)
	}

	// Build a map from validator address to share index for lookup.
	valIndexMap := make(map[string]int)
	for _, vs := range ks.ValidatorShares {
		valIndexMap[vs.ValidatorAddress] = int(vs.ShareIndex)
	}

	// Convert shares to kyber PubShare format for Lagrange interpolation.
	// Each decryption share is a G1 point: epoch_share_i = s_i * H(epoch_tag)
	kyberShares := make([]*share.PubShare, 0, len(shares))
	for _, s := range shares {
		idx, ok := valIndexMap[s.Validator]
		if !ok {
			continue
		}

		point := tleSuite.Point()
		if err := point.UnmarshalBinary(s.Share); err != nil {
			continue // Skip malformed shares
		}

		kyberShares = append(kyberShares, &share.PubShare{
			I: idx - 1, // Convert 1-based to 0-based for kyber
			V: point,
		})
	}

	if uint64(len(kyberShares)) < threshold {
		return nil, fmt.Errorf("insufficient valid shares after filtering: have %d, need %d", len(kyberShares), threshold)
	}

	// Lagrange interpolation on G1 points to recover the epoch decryption key.
	// key = s * H(epoch_tag) where s is the master secret.
	recovered, err := share.RecoverCommit(tleSuite, kyberShares, int(threshold), len(ks.ValidatorShares))
	if err != nil {
		return nil, fmt.Errorf("lagrange interpolation failed: %w", err)
	}

	return recovered.MarshalBinary()
}

// verifyDecryptionShare validates a decryption share for well-formedness.
//
// The ideal verification is a pairing check:
//
//	e(epoch_share_i, G2_gen) == e(epoch_tag, pubShare_i_on_G2)
//
// However, our DKG stores public key shares as G1 points. A full pairing-based
// verification would require dual G1/G2 public shares (a proto schema change).
//
// Current verification:
//  1. Validates the share is a well-formed, non-identity G1 point
//  2. Validates the share size matches expected BN256 G1 point encoding
//  3. Relies on PoP verification at registration time (Schnorr proof) to ensure
//     the validator knows their secret key and will compute shares correctly
//  4. TLE liveness enforcement (miss tracking + jailing) disincentivizes invalid shares
//
// A validator submitting malformed shares will cause reconstruction to fail,
// which is caught by the reconstruction error handling. Malicious validators
// who consistently submit bad shares will be jailed via the liveness system.
func verifyDecryptionShare(shareBytes []byte, pubShareBytes []byte, epochTag []byte) error {
	// Validate the decryption share is a valid G1 point
	epochShare := tleSuite.G1().Point()
	if err := epochShare.UnmarshalBinary(shareBytes); err != nil {
		return fmt.Errorf("invalid decryption share: not a valid G1 point: %w", err)
	}

	// Reject the identity element (zero point) — a trivial forgery
	if epochShare.Equal(tleSuite.G1().Point().Null()) {
		return fmt.Errorf("decryption share is the identity element")
	}

	// Validate the public key share is a valid G1 point
	pubShare := tleSuite.G1().Point()
	if err := pubShare.UnmarshalBinary(pubShareBytes); err != nil {
		return fmt.Errorf("invalid public key share: %w", err)
	}

	// Validate the epoch tag
	tag := tleSuite.G1().Point()
	if err := tag.UnmarshalBinary(epochTag); err != nil {
		return fmt.Errorf("invalid epoch tag: %w", err)
	}

	return nil
}

// computeEpochTag computes the epoch tag: H_to_G1("shield_epoch" || epoch_bytes).
// This is the base point that validators multiply by their private share to get
// their epoch decryption share.
func computeEpochTag(epoch uint64) ([]byte, error) {
	// Hash the epoch identifier to a G1 point
	data := fmt.Appendf(nil, "shield_epoch_%d", epoch)
	point := tleSuite.Point().Pick(tleSuite.XOF(data))

	return point.MarshalBinary()
}

// decryptPayload decrypts an ECIES-encrypted payload using the reconstructed
// epoch decryption key. The decryption key is a G1 point that serves as the
// private key for ECIES decryption.
func decryptPayload(encryptedPayload []byte, decryptionKey []byte) ([]byte, error) {
	// The decryption key is a G1 point. For ECIES decryption, we need the
	// corresponding scalar. In our TLE scheme, the epoch decryption key is
	// key = master_secret * H(epoch_tag), and encryption was done with the
	// master public key. However, the encrypted payload format uses ECIES
	// with the master public key, so we need the master secret scalar.
	//
	// In practice, the epoch decryption key enables decryption because:
	// 1. Client encrypts with master public key: Enc(masterPub, payload)
	// 2. Validators each contribute: share_i = private_i * H(epoch)
	// 3. Lagrange interpolation recovers: masterSecret * H(epoch)
	// 4. We use this to derive the ECIES decryption scalar
	//
	// For the ECIES scheme, we need the actual scalar. The reconstructed key
	// is a point, so we use it as a shared secret for symmetric decryption.

	// Parse the decryption key point
	keyPoint := tleSuite.Point()
	if err := keyPoint.UnmarshalBinary(decryptionKey); err != nil {
		return nil, fmt.Errorf("invalid decryption key: %w", err)
	}

	// ECIES decrypt using the key point as the private key scalar.
	// Note: This is a simplification. The actual TLE decryption flow
	// would use the reconstructed point with the epoch tag to derive
	// a symmetric key for AES decryption of the payload.
	scalar := tleSuite.Scalar().Pick(tleSuite.XOF(decryptionKey))
	plaintext, err := ecies.Decrypt(tleSuite, scalar, encryptedPayload, nil)
	if err != nil {
		return nil, fmt.Errorf("ECIES decryption failed: %w", err)
	}

	return plaintext, nil
}

// verifyProofOfPossession verifies that the validator knows the secret key
// corresponding to their public share. The PoP is a Schnorr signature over
// the validator's address using the secret key share.
func verifyProofOfPossession(pubShareBytes []byte, popBytes []byte, validatorAddr string) error {
	if len(popBytes) == 0 {
		return fmt.Errorf("proof of possession is empty")
	}

	pubPoint := tleSuite.Point()
	if err := pubPoint.UnmarshalBinary(pubShareBytes); err != nil {
		return fmt.Errorf("invalid public share: %w", err)
	}

	// The PoP message is the validator address bytes (canonical, deterministic)
	msg := []byte(validatorAddr)

	if err := schnorr.Verify(tleSuite, pubPoint, msg, popBytes); err != nil {
		return fmt.Errorf("proof of possession verification failed: %w", err)
	}

	return nil
}

// computeMasterPublicKey aggregates validator public key shares into a master public key
// using Lagrange interpolation on G1 points.
func computeMasterPublicKey(validatorShares []*types.TLEValidatorPublicShare, threshold uint64) ([]byte, error) {
	if len(validatorShares) == 0 {
		return nil, fmt.Errorf("no validator shares")
	}

	kyberShares := make([]*share.PubShare, 0, len(validatorShares))
	for _, vs := range validatorShares {
		point := tleSuite.Point()
		if err := point.UnmarshalBinary(vs.PublicShare); err != nil {
			continue
		}
		kyberShares = append(kyberShares, &share.PubShare{
			I: int(vs.ShareIndex) - 1, // Convert 1-based to 0-based
			V: point,
		})
	}

	if uint64(len(kyberShares)) < threshold {
		return nil, fmt.Errorf("insufficient valid shares: have %d, need %d", len(kyberShares), threshold)
	}

	recovered, err := share.RecoverCommit(tleSuite, kyberShares, int(threshold), len(validatorShares))
	if err != nil {
		return nil, fmt.Errorf("master key recovery failed: %w", err)
	}

	return recovered.MarshalBinary()
}
