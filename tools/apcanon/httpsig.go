package apcanon

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HTTPSigner produces draft-cavage-http-signatures ("HTTP Signatures")
// for GET requests against secure-mode (AUTHORIZED_FETCH) instances. An
// unsigned GET to such an instance returns 401, which is why the bridge
// daemon and — when it faces a secure-mode peer — the verifier runner
// both need this.
//
// NOT RFC 9421. RFC 9421 ("HTTP Message Signatures") is a DIFFERENT and
// wire-incompatible scheme: Signature-Input plus structured-field
// components (@method, @target-uri) rather than the
// `Signature: keyId=...,algorithm=...,headers=...,signature=...` header
// built below. Cavage is what the ActivityPub fediverse actually
// verifies — Mastodon, GoToSocial, Pleroma — so it is what this emits.
// Earlier comments here and in the spec called it RFC 9421, which
// invited a future "upgrade to the real RFC" that would silently break
// every signed fetch. Do not rename it back.
//
// Only rsa-sha256 is emitted: it is the one algorithm every ActivityPub
// server in practice accepts; ed25519/hs2019 support varies by version
// and adds nothing for a first-party bridge key we control.
type HTTPSigner struct {
	KeyID      string
	PrivateKey *rsa.PrivateKey
}

// LoadHTTPSigner builds a signer from a PEM-encoded RSA private key
// (the actor key Mastodon exposes at <actor>#main-key, or any key the
// operator registers with their instance for the bridge actor).
func LoadHTTPSigner(keyID string, pemPEM []byte) (*HTTPSigner, error) {
	key, err := parseRSAPrivateKeyPEM(pemPEM)
	if err != nil {
		return nil, fmt.Errorf("apcanon: parse signing key: %w", err)
	}
	return &HTTPSigner{KeyID: keyID, PrivateKey: key}, nil
}

// Sign mutates req into a Cavage-signed GET: it sets Date and Digest and
// appends the Signature header. Signed headers: (request-target) host
// date digest accept — the set Mastodon's secure mode requires.
//
// The signature is bound to (request-target) and host, so it is valid
// only for the exact hop it was made for; Fetch strips these headers on
// a cross-host redirect rather than presenting them to another server.
func (s *HTTPSigner) Sign(req *http.Request) error {
	if s == nil || s.PrivateKey == nil {
		return fmt.Errorf("apcanon: signer has no key")
	}
	body := []byte{}
	digest := sha256.Sum256(body)
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("Digest", "sha-256="+base64.StdEncoding.EncodeToString(digest[:]))
	req.Header.Set("Accept", AcceptAS2)

	var b strings.Builder
	b.WriteString("(request-target): " + strings.ToLower(req.Method) + " " + req.URL.RequestURI() + "\n")
	b.WriteString("host: " + req.URL.Host + "\n")
	b.WriteString("date: " + req.Header.Get("Date") + "\n")
	b.WriteString("digest: " + req.Header.Get("Digest") + "\n")
	b.WriteString("accept: " + AcceptAS2)
	signingString := b.String()

	sum := sha256.Sum256([]byte(signingString))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.PrivateKey, crypto.SHA256, sum[:])
	if err != nil {
		return fmt.Errorf("apcanon: sign request: %w", err)
	}
	req.Header.Set("Signature", fmt.Sprintf(
		`keyId="%s",algorithm="rsa-sha256",headers="(request-target) host date digest accept",signature="%s"`,
		s.KeyID, base64.StdEncoding.EncodeToString(sig)))
	return nil
}

// parseRSAPrivateKeyPEM accepts PKCS#1 ("RSA PRIVATE KEY") and PKCS#8
// ("PRIVATE KEY") PEM blocks.
func parseRSAPrivateKeyPEM(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is %T, want RSA", key)
	}
	return rsaKey, nil
}
