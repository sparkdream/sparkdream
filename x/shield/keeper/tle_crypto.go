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

// tleG2Suite is the BN256 G2 suite for G2 point operations.
var tleG2Suite = bn256.NewSuiteG2()

// tlePairingSuite is the full BN256 pairing suite for bilinear pairing checks.
var tlePairingSuite = bn256.NewSuite()

// verifyDecryptionShare validates a decryption share using a pairing-based check
// when a G2 public share is available, falling back to well-formedness checks only
// for legacy key sets without G2 shares.
//
// Pairing check (SHIELD-5): e(share_i, G2_gen) == e(epoch_tag, pubShare_i_on_G2)
// This proves share_i = secret_i * epoch_tag without revealing secret_i.
func verifyDecryptionShare(shareBytes []byte, pubShareBytes []byte, pubShareG2Bytes []byte, epochTag []byte, shareIndex int, expectedIndex int) error {
	// Validate share index matches the expected validator index
	if shareIndex != expectedIndex {
		return fmt.Errorf("share index mismatch: submitted %d, expected %d for this validator", shareIndex, expectedIndex)
	}

	// Validate the decryption share is a valid G1 point
	epochShare := tleSuite.G1().Point()
	if err := epochShare.UnmarshalBinary(shareBytes); err != nil {
		return fmt.Errorf("invalid decryption share: not a valid G1 point: %w", err)
	}

	// Reject the identity element (zero point) — a trivial forgery
	if epochShare.Equal(tleSuite.G1().Point().Null()) {
		return fmt.Errorf("decryption share is the identity element")
	}

	// Validate the epoch tag is a valid G1 point
	tag := tleSuite.G1().Point()
	if err := tag.UnmarshalBinary(epochTag); err != nil {
		return fmt.Errorf("invalid epoch tag: %w", err)
	}

	// Require G2 public share for pairing-based verification.
	if len(pubShareG2Bytes) == 0 {
		return fmt.Errorf("G2 public key share is required for decryption share verification")
	}

	pubShareG2 := tleG2Suite.G2().Point()
	if err := pubShareG2.UnmarshalBinary(pubShareG2Bytes); err != nil {
		return fmt.Errorf("invalid G2 public key share: %w", err)
	}
	if pubShareG2.Equal(tleG2Suite.G2().Point().Null()) {
		return fmt.Errorf("G2 public key share is the identity element")
	}

	// Pairing check: e(share_i, G2_gen) == e(epoch_tag, pubShare_i_G2)
	// share_i = secret_i * epoch_tag, pubShare_i_G2 = secret_i * G2_gen
	// So: e(secret_i * tag, G2_gen) = e(tag, secret_i * G2_gen)
	g2Gen := tleG2Suite.G2().Point().Base()
	lhs := tlePairingSuite.Pair(epochShare, g2Gen)
	rhs := tlePairingSuite.Pair(tag, pubShareG2)
	if !lhs.Equal(rhs) {
		return fmt.Errorf("pairing check failed: decryption share is not valid for this validator's key")
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
// epoch decryption key.
//
// KEY DERIVATION SCHEME (SHIELD-4):
//
// The reconstructed epoch decryption key is a BN256 G1 point:
//
//	epoch_key = master_secret * H_to_G1("shield_epoch_<N>")
//
// ECIES requires a scalar private key, not a G1 point. We derive the ECIES
// scalar deterministically from the reconstructed point using the suite's XOF
// (eXtendable Output Function):
//
//	ecies_scalar = XOF(marshal(epoch_key)).Pick()
//
// This is a standard point-to-scalar key derivation: the serialized point is
// hashed via a cryptographic XOF (SHAKE-256 in kyber's BN256 suite), and the
// output stream is used to sample a uniform scalar in the BN256 scalar field.
//
// IMPORTANT: Client-side encryption in tools/tle/ MUST use the identical
// derivation. The client computes the epoch public key as:
//
//	epoch_pub = master_pub * H_to_G1("shield_epoch_<N>")  (pairing shortcut)
//
// ... but for ECIES encryption, the client must derive the ECIES public key
// from the ECIES scalar:
//
//	ecies_scalar = XOF(marshal(epoch_key)).Pick()  -- same derivation
//	ecies_pub    = ecies_scalar * G1_generator
//
// The client encrypts with ecies_pub, and this function decrypts with
// ecies_scalar. Both sides MUST use tleSuite.XOF() on the raw marshaled
// G1 point bytes to ensure consistency.
func decryptPayload(encryptedPayload []byte, decryptionKey []byte) ([]byte, error) {
	// Parse the reconstructed epoch decryption key (a G1 point).
	keyPoint := tleSuite.Point()
	if err := keyPoint.UnmarshalBinary(decryptionKey); err != nil {
		return nil, fmt.Errorf("invalid decryption key: %w", err)
	}

	// Derive the ECIES scalar from the G1 point via XOF key derivation.
	// This hashes the serialized point through SHAKE-256 and samples a
	// uniform scalar from the output stream. The client-side tools/tle/
	// code MUST use the identical derivation: tleSuite.Scalar().Pick(tleSuite.XOF(pointBytes)).
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
