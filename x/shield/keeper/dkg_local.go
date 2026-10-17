package keeper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/sign/schnorr"

	"sparkdream/tools/tle"
)

// DKGLocalKeyStore manages the validator's local DKG key material.
// These secrets never go on-chain — they're used locally by the
// ExtendVote handler to generate registrations and contributions.
//
// File location: <home>/config/shield_dkg/round_<N>.json
type DKGLocalKeyStore struct {
	mu      sync.RWMutex
	homeDir string

	// shareCache holds the aggregated Shamir share per round, keyed by a
	// digest of the evaluations it was built from, so a validator decrypts
	// its n-1 incoming evaluations once per key set rather than once per vote.
	shareCache map[uint64]cachedShare
}

type cachedShare struct {
	inputs [32]byte
	share  kyber.Scalar
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

// IncomingEvaluation is one dealer's evaluation for this validator, as the
// ABCI handler assembles it from on-chain DKG state: the dealer's Feldman
// commitments (to verify against), and either the ECIES ciphertext the
// dealer published or, when this validator complained and the dealer
// answered, the evaluation in clear. A dealer that published no evaluation
// for this validator appears with neither.
type IncomingEvaluation struct {
	Dealer      string
	DealerIndex uint32
	Commitments [][]byte
	Ciphertext  []byte
	Plaintext   []byte
}

// NewDKGLocalKeyStore creates a key store rooted at the given home directory.
func NewDKGLocalKeyStore(homeDir string) *DKGLocalKeyStore {
	return &DKGLocalKeyStore{homeDir: homeDir, shareCache: map[uint64]cachedShare{}}
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
// Returns (polynomial scalars, commitments as G1 points, error).
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
	return s.evaluatePolynomialFromData(data, j)
}

// Justify is the dealer's answer to a complaint from validator j: p(j) in
// clear, which anyone verifies against the published commitments.
func (s *DKGLocalKeyStore) Justify(round uint64, j uint32) ([]byte, error) {
	eval, err := s.EvaluatePolynomial(round, j)
	if err != nil {
		return nil, err
	}
	return eval.MarshalBinary()
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

// SignPoP creates a Schnorr proof of possession over PoPMessage(validator
// address, round) using the private key (the constant term of the
// polynomial) for the given round.
func (s *DKGLocalKeyStore) SignPoP(round uint64, validatorAddr string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.loadRound(round)
	if err != nil {
		return nil, fmt.Errorf("no key for round %d: %w", round, err)
	}

	privKey, err := parsePrivateKey(data)
	if err != nil {
		return nil, err
	}

	return schnorr.Sign(tleSuite, privKey, PoPMessage(validatorAddr, round))
}

// eciesMinLen is the shortest ECIES ciphertext kyber can parse: an ephemeral
// G1 point plus the AEAD tag. kyber's Decrypt slices without checking and
// panics on anything shorter, which a dealer could use to crash every
// validator's ExtendVote; so the length is checked and the call recovered.
const eciesMinLen = tle.G1PointLen + 16

func decryptEvaluation(privKey kyber.Scalar, ciphertext []byte) (plaintext []byte, err error) {
	if len(ciphertext) < eciesMinLen {
		return nil, fmt.Errorf("ciphertext too short (%d bytes)", len(ciphertext))
	}
	defer func() {
		if r := recover(); r != nil {
			plaintext, err = nil, fmt.Errorf("decrypt panicked: %v", r)
		}
	}()
	return ecies.Decrypt(tleSuite, privKey, ciphertext, nil)
}

// openEvaluation returns dealer's evaluation for selfIdx: the plaintext a
// justification published, or the decrypted ciphertext. It fails when the
// dealer published nothing for this validator, the ciphertext does not open,
// or the value does not match the dealer's commitments.
func openEvaluation(privKey kyber.Scalar, selfIdx uint32, ev IncomingEvaluation) (kyber.Scalar, error) {
	var evalBytes []byte
	switch {
	case len(ev.Plaintext) > 0:
		evalBytes = ev.Plaintext
	case len(ev.Ciphertext) > 0:
		plaintext, err := decryptEvaluation(privKey, ev.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt: %w", err)
		}
		evalBytes = plaintext
	default:
		return nil, fmt.Errorf("no evaluation published for index %d", selfIdx)
	}
	if err := VerifyEvaluation(ev.Commitments, selfIdx, evalBytes); err != nil {
		return nil, err
	}
	eval := tleSuite.Scalar()
	_ = eval.UnmarshalBinary(evalBytes) // verified above
	return eval, nil
}

// FindComplaints returns the dealer indices this validator should complain
// about in the COMPLAINING phase: dealers whose evaluation for selfIdx is
// missing, does not decrypt, or does not match their commitments.
func (s *DKGLocalKeyStore) FindComplaints(round uint64, selfIdx uint32, incoming []IncomingEvaluation) ([]uint32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.loadRound(round)
	if err != nil {
		return nil, fmt.Errorf("no key for round %d: %w", round, err)
	}
	privKey, err := parsePrivateKey(data)
	if err != nil {
		return nil, err
	}
	var complaints []uint32
	for _, ev := range incoming {
		if ev.DealerIndex == selfIdx {
			continue
		}
		if _, err := openEvaluation(privKey, selfIdx, ev); err != nil {
			complaints = append(complaints, ev.DealerIndex)
		}
	}
	return complaints, nil
}

// ComputeDecryptionShare computes this validator's epoch decryption share:
//
//	share_j = s_j * H1(epoch)
//
// where s_j is the validator's AGGREGATED Shamir share — the sum of every
// qualified dealer i's polynomial evaluated at j:
//
//	s_j = Σ_i p_i(j) = p_self(j) + Σ_{i ≠ self} open(eval_i_to_j)
//
// Lagrange reconstruction at x=0 over {(j, share_j)} then yields
//
//	Σ_j L_j(0) * share_j = (Σ_j L_j(0) * s_j) * tag = (Σ_i a_{i,0}) * tag = master * tag
//
// which is the epoch decryption key. Every incoming evaluation is verified
// against its dealer's commitments before it is added: an evaluation that
// fails would make this validator's share inconsistent with its public share
// (the caller should have complained about it during the ceremony), so the
// computation fails with the dealer named instead of producing a share the
// chain would reject. The aggregated share is cached per round and input set.
func (s *DKGLocalKeyStore) ComputeDecryptionShare(
	round uint64,
	epoch uint64,
	selfIdx uint32,
	incoming []IncomingEvaluation,
) ([]byte, error) {
	if selfIdx == 0 {
		return nil, fmt.Errorf("selfIdx must be 1-based, got 0")
	}
	aggregated, err := s.aggregatedShare(round, selfIdx, incoming)
	if err != nil {
		return nil, err
	}
	sharePoint := tleSuite.Point().Mul(aggregated, tle.EpochTag(epoch))
	return sharePoint.MarshalBinary()
}

func (s *DKGLocalKeyStore) aggregatedShare(round uint64, selfIdx uint32, incoming []IncomingEvaluation) (kyber.Scalar, error) {
	digest := evaluationDigest(selfIdx, incoming)

	s.mu.RLock()
	if c, ok := s.shareCache[round]; ok && c.inputs == digest {
		s.mu.RUnlock()
		return c.share, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.shareCache[round]; ok && c.inputs == digest {
		return c.share, nil
	}

	data, err := s.loadRound(round)
	if err != nil {
		return nil, fmt.Errorf("no key for round %d: %w", round, err)
	}
	privKey, err := parsePrivateKey(data)
	if err != nil {
		return nil, err
	}

	// Start with our own polynomial evaluated at our own index.
	selfEval, err := s.evaluatePolynomialFromData(data, selfIdx)
	if err != nil {
		return nil, fmt.Errorf("self polynomial eval failed: %w", err)
	}
	aggregated := tleSuite.Scalar().Set(selfEval)

	for _, ev := range incoming {
		if ev.DealerIndex == selfIdx {
			continue
		}
		eval, err := openEvaluation(privKey, selfIdx, ev)
		if err != nil {
			return nil, fmt.Errorf("evaluation from dealer %s (index %d): %w", ev.Dealer, ev.DealerIndex, err)
		}
		aggregated = aggregated.Add(aggregated, eval)
	}

	s.shareCache[round] = cachedShare{inputs: digest, share: aggregated}
	return aggregated, nil
}

func evaluationDigest(selfIdx uint32, incoming []IncomingEvaluation) [32]byte {
	h := sha256.New()
	fmt.Fprintf(h, "%d|", selfIdx)
	for _, ev := range incoming {
		fmt.Fprintf(h, "%s|%d|", ev.Dealer, ev.DealerIndex)
		for _, c := range ev.Commitments {
			h.Write(c)
		}
		h.Write([]byte{0})
		h.Write(ev.Ciphertext)
		h.Write([]byte{0})
		h.Write(ev.Plaintext)
		h.Write([]byte{0})
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// evaluatePolynomialFromData evaluates the polynomial loaded from data at j.
// Caller must hold s.mu (read or write).
func (s *DKGLocalKeyStore) evaluatePolynomialFromData(data *dkgLocalKeyData, j uint32) (kyber.Scalar, error) {
	if len(data.PolynomialHex) == 0 {
		return nil, fmt.Errorf("no polynomial stored for round %d", data.Round)
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

// Cleanup removes key files for rounds older than keepRound. A retired
// round's share can still open every payload sealed to its key set that sits
// in block history, so validators drop it as soon as the next key set is
// active.
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
			delete(s.shareCache, r)
		}
	}
}

// --- internal helpers ---

func parsePrivateKey(data *dkgLocalKeyData) (kyber.Scalar, error) {
	privBytes, err := hex.DecodeString(data.PrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key hex: %w", err)
	}
	privKey := tleSuite.Scalar()
	if err := privKey.UnmarshalBinary(privBytes); err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	return privKey, nil
}

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
	privKey, err := parsePrivateKey(data)
	if err != nil {
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
