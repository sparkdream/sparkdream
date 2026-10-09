package contentscan

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// OperatorMetadata is what a content-scanner worker stores in its x/service
// operator `metadata` (§8.1). Trust follows operator status, so there is no
// separate key registry: a client reads ACTIVE content-scanner operators and
// the verdict key each one published here.
type OperatorMetadata struct {
	V              int               `json:"v"`
	VerdictKey     string            `json:"verdict_key"` // base64 ed25519 public key
	FeedURL        string            `json:"feed_url"`    // base URL of the feed (manifest.json lives under it)
	RulesetVersion string            `json:"ruleset_version"`
	Models         map[string]string `json:"models,omitempty"`
}

// ParseOperatorMetadata decodes and checks operator metadata bytes.
func ParseOperatorMetadata(raw []byte) (OperatorMetadata, error) {
	var m OperatorMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("contentscan: operator metadata: %w", err)
	}
	if _, err := m.PublicKey(); err != nil {
		return m, err
	}
	if m.FeedURL == "" {
		return m, errors.New("contentscan: operator metadata has no feed_url")
	}
	return m, nil
}

// PublicKey decodes the verdict key.
func (m OperatorMetadata) PublicKey() (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(m.VerdictKey)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("contentscan: verdict_key must be a base64 ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}

// Marshal encodes the metadata for MsgRegisterOperator / MsgUpdateMetadata.
func (m OperatorMetadata) Marshal() ([]byte, error) {
	if m.V == 0 {
		m.V = 1
	}
	return json.Marshal(m)
}
