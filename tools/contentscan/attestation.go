// Package contentscan is the reference implementation of the content-scanner
// protocol in docs/content-scanning.md: the signed verdict format (§4.1),
// the client combination rule (§4.4), the static feed layout (§5.4), the
// operator metadata a worker registers under x/service (§8.1) and the
// detection pipeline a worker runs (§5.2). cmd/contentscan is the worker
// built on it; frontends port Verify and Combine.
package contentscan

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// AttestationVersion is the "v" every attestation carries.
const AttestationVersion = 1

// Verdict states (§4.2).
const (
	VerdictClean   = "clean"
	VerdictHeld    = "held"
	VerdictRemoved = "removed"
	// VerdictUnchecked is a combination result, never a signed verdict.
	VerdictUnchecked = "unchecked"
)

// Categories (§4.3).
const (
	CategoryCSAM     = "csam"
	CategoryIllegal  = "illegal"
	CategorySexual   = "sexual"
	CategoryViolence = "violence"
	CategorySpam     = "spam"
	CategoryTest     = "test"
	CategoryNone     = "none"
)

// Subject identifies what a verdict is about. SHA256 is the hash of the exact
// bytes checked: the stored body for on-chain records (the chain's
// body_hash), or the bytes fetched for off-chain references and URIs.
type Subject struct {
	ChainID string `json:"chain_id,omitempty"`
	Record  string `json:"record,omitempty"` // module/type/id, e.g. "blog/post/123"
	SHA256  string `json:"sha256"`           // lowercase hex
	CID     string `json:"cid,omitempty"`
	URI     string `json:"uri,omitempty"`
	PDQ     string `json:"pdq,omitempty"`
}

// Ruleset pins what produced a verdict, so two workers scoring the same
// bytes with the same ruleset agree (the quorum rule depends on it).
type Ruleset struct {
	Version    string             `json:"version"`
	Models     map[string]string  `json:"models,omitempty"`
	Thresholds map[string]float64 `json:"thresholds,omitempty"`
}

// Attestation is one signed verdict (§4.1).
type Attestation struct {
	V             int       `json:"v"`
	Worker        string    `json:"worker"` // x/service operator address
	Subject       Subject   `json:"subject"`
	Verdict       string    `json:"verdict"`
	Category      string    `json:"category"`
	Ruleset       Ruleset   `json:"ruleset"`
	ScannedHeight int64     `json:"scanned_height"`
	IssuedAt      time.Time `json:"issued_at"`
	ExpiresAt     time.Time `json:"expires_at,omitzero"`
	Sig           string    `json:"sig,omitempty"` // base64 ed25519 over SigningBytes
}

// Validate checks the shape of an attestation (not its signature).
func (a Attestation) Validate() error {
	if a.V != AttestationVersion {
		return fmt.Errorf("contentscan: unsupported attestation version %d", a.V)
	}
	if a.Worker == "" {
		return errors.New("contentscan: attestation has no worker")
	}
	if len(a.Subject.SHA256) != 64 {
		return errors.New("contentscan: subject.sha256 must be 64 hex characters")
	}
	switch a.Verdict {
	case VerdictClean, VerdictHeld, VerdictRemoved:
	default:
		return fmt.Errorf("contentscan: invalid verdict %q", a.Verdict)
	}
	switch a.Category {
	case CategoryCSAM, CategoryIllegal, CategorySexual, CategoryViolence, CategorySpam, CategoryTest, CategoryNone:
	default:
		return fmt.Errorf("contentscan: invalid category %q", a.Category)
	}
	if a.Subject.URI != "" && a.Subject.Record == "" && a.ExpiresAt.IsZero() {
		// Plain-URI content can change under the same URL.
		return errors.New("contentscan: plain-URI subjects need expires_at")
	}
	return nil
}

// SigningBytes is the canonical JSON of the attestation without its
// signature: object keys sorted, no insignificant whitespace, no HTML
// escaping, numbers as written. Every implementation must produce the same
// bytes; the test vectors in attestation_test.go pin them.
func (a Attestation) SigningBytes() ([]byte, error) {
	a.Sig = ""
	return CanonicalJSON(a)
}

// CanonicalJSON marshals v and re-encodes it with sorted keys.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil { // map keys are sorted by encoding/json
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Sign signs the attestation with the worker's dedicated verdict key (never
// its wallet key, §4.1) and returns it with Sig set.
func Sign(a Attestation, key ed25519.PrivateKey) (Attestation, error) {
	if err := a.Validate(); err != nil {
		return a, err
	}
	msg, err := a.SigningBytes()
	if err != nil {
		return a, err
	}
	a.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(key, msg))
	return a, nil
}

// Verify checks the attestation's shape and its signature under pub.
func Verify(a Attestation, pub ed25519.PublicKey) error {
	if err := a.Validate(); err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(a.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("contentscan: malformed signature")
	}
	msg, err := a.SigningBytes()
	if err != nil {
		return err
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, msg, sig) {
		return errors.New("contentscan: bad signature")
	}
	return nil
}
