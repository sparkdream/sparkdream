package keeper

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/sign/schnorr"
)

// DKGLocalKeyStore manages the validator's local DKG key material.
// These secrets never go on-chain — they're used locally by the
// ExtendVote handler to generate registrations and contributions.
//
// File location: <home>/config/shield_dkg/round_<N>.json
type DKGLocalKeyStore struct {
	mu      sync.RWMutex
	homeDir string
}

// dkgLocalKeyData is the on-disk format for DKG secrets.
type dkgLocalKeyData struct {
	Round uint64 `json:"round"`
	// BN256 G1 private key (scalar) — hex-encoded
	PrivateKeyHex string `json:"private_key_hex"`
	// BN256 G1 public key — hex-encoded
	PublicKeyHex string `json:"public_key_hex"`
	// Polynomial coefficients (scalars) for CONTRIBUTING phase — hex-encoded
	// Index 0 is the constant term (== PrivateKey). Length == threshold.
	PolynomialHex []string `json:"polynomial_hex,omitempty"`
}

// NewDKGLocalKeyStore creates a key store rooted at the given home directory.
func NewDKGLocalKeyStore(homeDir string) *DKGLocalKeyStore {
	return &DKGLocalKeyStore{homeDir: homeDir}
}

func (s *DKGLocalKeyStore) keyDir() string {
	return filepath.Join(s.homeDir, "config", "shield_dkg")
}

func (s *DKGLocalKeyStore) keyPath(round uint64) string {
	return filepath.Join(s.keyDir(), fmt.Sprintf("round_%d.json", round))
}

// EnsureRegistrationKey generates or loads the BN256 keypair for the given DKG round.
// Returns (privateKey, publicKey, error).
func (s *DKGLocalKeyStore) EnsureRegistrationKey(round uint64) (kyber.Scalar, kyber.Point, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Try to load existing key for this round
	data, err := s.loadRound(round)
	if err == nil && data.PrivateKeyHex != "" {
		privKey, pubKey, parseErr := s.parseKeyPair(data)
		if parseErr == nil {
			return privKey, pubKey, nil
		}
	}

	// Generate new keypair
	privKey := tleSuite.Scalar().Pick(tleSuite.RandomStream())
	pubKey := tleSuite.Point().Mul(privKey, nil)

	privBytes, _ := privKey.MarshalBinary()
	pubBytes, _ := pubKey.MarshalBinary()

	newData := &dkgLocalKeyData{
		Round:         round,
		PrivateKeyHex: hex.EncodeToString(privBytes),
		PublicKeyHex:  hex.EncodeToString(pubBytes),
	}

	if err := s.saveRound(newData); err != nil {
		return nil, nil, fmt.Errorf("failed to save DKG key: %w", err)
	}

	return privKey, pubKey, nil
}

// GeneratePolynomial creates random polynomial coefficients for the CONTRIBUTING phase.
// The constant term (index 0) is the validator's private key from the REGISTERING phase.
// Returns (commitments as G1 points, polynomial scalars, error).
func (s *DKGLocalKeyStore) GeneratePolynomial(round uint64, threshold int) ([]kyber.Scalar, []kyber.Point, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadRound(round)
	if err != nil {
		return nil, nil, fmt.Errorf("no key for round %d: %w", round, err)
	}

	privKey := tleSuite.Scalar()
	privBytes, err := hex.DecodeString(data.PrivateKeyHex)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid private key hex: %w", err)
	}
	if err := privKey.UnmarshalBinary(privBytes); err != nil {
		return nil, nil, fmt.Errorf("invalid private key: %w", err)
	}

	// If polynomial already generated for this round, reload it
	if len(data.PolynomialHex) == threshold {
		poly, commitments, parseErr := s.parsePolynomial(data)
		if parseErr == nil {
			return poly, commitments, nil
		}
	}

	// Generate polynomial: a_0 = privKey, a_1..a_{t-1} = random
	poly := make([]kyber.Scalar, threshold)
	poly[0] = privKey
	for i := 1; i < threshold; i++ {
		poly[i] = tleSuite.Scalar().Pick(tleSuite.RandomStream())
	}

	// Compute Feldman commitments: C_k = a_k * G
	commitments := make([]kyber.Point, threshold)
	polyHex := make([]string, threshold)
	for i, coeff := range poly {
		commitments[i] = tleSuite.Point().Mul(coeff, nil)
		coeffBytes, _ := coeff.MarshalBinary()
		polyHex[i] = hex.EncodeToString(coeffBytes)
	}

	// Save polynomial back to disk
	data.PolynomialHex = polyHex
	if err := s.saveRound(data); err != nil {
		return nil, nil, fmt.Errorf("failed to save polynomial: %w", err)
	}

	return poly, commitments, nil
}

// EvaluatePolynomial evaluates the stored polynomial at a given 1-based index.
// Returns p(j) as a scalar.
func (s *DKGLocalKeyStore) EvaluatePolynomial(round uint64, j uint32) (kyber.Scalar, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.loadRound(round)
	if err != nil {
		return nil, fmt.Errorf("no key for round %d: %w", round, err)
	}

	if len(data.PolynomialHex) == 0 {
		return nil, fmt.Errorf("no polynomial stored for round %d", round)
	}

	poly := make([]kyber.Scalar, len(data.PolynomialHex))
	for i, h := range data.PolynomialHex {
		b, err := hex.DecodeString(h)
		if err != nil {
			return nil, fmt.Errorf("invalid polynomial coeff %d: %w", i, err)
		}
		poly[i] = tleSuite.Scalar()
		if err := poly[i].UnmarshalBinary(b); err != nil {
			return nil, fmt.Errorf("invalid polynomial coeff %d: %w", i, err)
		}
	}

	// Horner's method: p(j) = a_0 + j*(a_1 + j*(a_2 + ...))
	jScalar := tleSuite.Scalar().SetInt64(int64(j))
	result := tleSuite.Scalar().Zero()
	for i := len(poly) - 1; i >= 0; i-- {
		result = result.Mul(result, jScalar)
		result = result.Add(result, poly[i])
	}

	return result, nil
}

// GetPolynomialCoefficient returns the k-th polynomial coefficient for the given DKG round.
// Used to compute G2 Feldman commitments (a_k * G2_gen) alongside the G1 commitments.
func (s *DKGLocalKeyStore) GetPolynomialCoefficient(round uint64, k int) (kyber.Scalar, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.loadRound(round)
	if err != nil {
		return nil, fmt.Errorf("no key for round %d: %w", round, err)
	}

	if k < 0 || k >= len(data.PolynomialHex) {
		return nil, fmt.Errorf("coefficient index %d out of range [0, %d)", k, len(data.PolynomialHex))
	}

	b, err := hex.DecodeString(data.PolynomialHex[k])
	if err != nil {
		return nil, fmt.Errorf("invalid polynomial coeff %d hex: %w", k, err)
	}
	scalar := tleSuite.Scalar()
	if err := scalar.UnmarshalBinary(b); err != nil {
		return nil, fmt.Errorf("invalid polynomial coeff %d: %w", k, err)
	}
	return scalar, nil
}

// SignPoP creates a Schnorr proof of possession over the validator address
// using the private key (or constant term polynomial coefficient) for the given round.
func (s *DKGLocalKeyStore) SignPoP(round uint64, validatorAddr string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.loadRound(round)
	if err != nil {
		return nil, fmt.Errorf("no key for round %d: %w", round, err)
	}

	privBytes, err := hex.DecodeString(data.PrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key hex: %w", err)
	}
	privKey := tleSuite.Scalar()
	if err := privKey.UnmarshalBinary(privBytes); err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	return schnorr.Sign(tleSuite, privKey, []byte(validatorAddr))
}

// ComputeDecryptionShare computes this validator's epoch decryption share:
// share_i = private_key_i * H_to_G1(epoch_tag)
// This is used during ACTIVE phase to produce the share that, when combined
// with other validators' shares via Lagrange interpolation, reconstructs
// the epoch decryption key.
func (s *DKGLocalKeyStore) ComputeDecryptionShare(round uint64, epoch uint64) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.loadRound(round)
	if err != nil {
		return nil, fmt.Errorf("no key for round %d: %w", round, err)
	}

	privBytes, err := hex.DecodeString(data.PrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key hex: %w", err)
	}
	privKey := tleSuite.Scalar()
	if err := privKey.UnmarshalBinary(privBytes); err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	// Compute the epoch tag: H_to_G1("shield_epoch_<N>")
	epochData := fmt.Appendf(nil, "shield_epoch_%d", epoch)
	epochTag := tleSuite.Point().Pick(tleSuite.XOF(epochData))

	// share_i = private_key_i * epochTag
	sharePoint := tleSuite.Point().Mul(privKey, epochTag)
	return sharePoint.MarshalBinary()
}

// Cleanup removes key files for rounds older than keepRound.
func (s *DKGLocalKeyStore) Cleanup(keepRound uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.keyDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		var r uint64
		if _, err := fmt.Sscanf(e.Name(), "round_%d.json", &r); err == nil && r < keepRound {
			_ = os.Remove(filepath.Join(s.keyDir(), e.Name()))
		}
	}
}

// --- internal helpers ---

func (s *DKGLocalKeyStore) loadRound(round uint64) (*dkgLocalKeyData, error) {
	path := s.keyPath(round)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data dkgLocalKeyData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (s *DKGLocalKeyStore) saveRound(data *dkgLocalKeyData) error {
	dir := s.keyDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.keyPath(data.Round), b, 0600)
}

func (s *DKGLocalKeyStore) parseKeyPair(data *dkgLocalKeyData) (kyber.Scalar, kyber.Point, error) {
	privBytes, err := hex.DecodeString(data.PrivateKeyHex)
	if err != nil {
		return nil, nil, err
	}
	privKey := tleSuite.Scalar()
	if err := privKey.UnmarshalBinary(privBytes); err != nil {
		return nil, nil, err
	}

	pubBytes, err := hex.DecodeString(data.PublicKeyHex)
	if err != nil {
		return nil, nil, err
	}
	pubKey := tleSuite.Point()
	if err := pubKey.UnmarshalBinary(pubBytes); err != nil {
		return nil, nil, err
	}

	return privKey, pubKey, nil
}

func (s *DKGLocalKeyStore) parsePolynomial(data *dkgLocalKeyData) ([]kyber.Scalar, []kyber.Point, error) {
	poly := make([]kyber.Scalar, len(data.PolynomialHex))
	commitments := make([]kyber.Point, len(data.PolynomialHex))
	for i, h := range data.PolynomialHex {
		b, err := hex.DecodeString(h)
		if err != nil {
			return nil, nil, err
		}
		poly[i] = tleSuite.Scalar()
		if err := poly[i].UnmarshalBinary(b); err != nil {
			return nil, nil, err
		}
		commitments[i] = tleSuite.Point().Mul(poly[i], nil)
	}
	return poly, commitments, nil
}
