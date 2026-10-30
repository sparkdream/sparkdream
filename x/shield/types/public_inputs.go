package types

import (
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"

	zkcrypto "sparkdream/tools/crypto"
)

// ValidateCanonicalPublicInputs refuses any of the cleartext public inputs of
// a shielded exec (nullifier, rate-limit nullifier, merkle root) that is not a
// canonical BN254 scalar-field element, i.e. whose big-endian value is not
// below the field modulus r.
//
// The Groth16 verifier reduces every public input mod r, so a nullifier n and
// n+r (both fit in 32 bytes for most n) verify against the same proof. The
// chain keys nullifiers and rate limits by the raw bytes, so without this
// check every proof would have up to ~5 distinct encodings: one member could
// act several times in a scope, exceed the identity rate limit, or anyone
// could replay a landed exec under a new encoding.
func ValidateCanonicalPublicInputs(nullifier, rateLimitNullifier, merkleRoot []byte) error {
	if !zkcrypto.IsCanonicalFieldElement(nullifier) {
		return errorsmod.Wrap(ErrNonCanonicalPublicInput, "nullifier")
	}
	if !zkcrypto.IsCanonicalFieldElement(rateLimitNullifier) {
		return errorsmod.Wrap(ErrNonCanonicalPublicInput, "rate_limit_nullifier")
	}
	if !zkcrypto.IsCanonicalFieldElement(merkleRoot) {
		return errorsmod.Wrap(ErrNonCanonicalPublicInput, "merkle_root")
	}
	return nil
}

// IsCanonicalNullifierHex reports whether a stored nullifier hex string decodes
// to a canonical field element (used by the invariants).
func IsCanonicalNullifierHex(nullifierHex string) bool {
	bz, err := hex.DecodeString(nullifierHex)
	return err == nil && zkcrypto.IsCanonicalFieldElement(bz)
}
