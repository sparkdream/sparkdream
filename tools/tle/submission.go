package tle

import (
	"crypto/sha256"
	"encoding/binary"
)

// Encrypted-batch submissions carry a cleartext membership proof of their own
// (the operation proof travels inside the ciphertext). It is an ordinary
// ShieldCircuit proof whose public inputs are:
//
//	Nullifier          = MiMC(secret, ScopeElement(BatchSubmissionDomain, BatchSubmissionScope(payload)))
//	RateLimitNullifier = H(secret, rate-limit tag, current epoch)
//	Scope              = ScopeElement(BatchSubmissionDomain, BatchSubmissionScope(payload))
//	MessageHash        = ShieldMessageHash(BatchSubmissionTypeURL, payload)
//	MerkleRoot, MinTrustLevel, RateLimitEpoch as for an immediate exec.
//
// It makes the identity rate limit unforgeable at submission (so the module
// pays only for proven members under their quota), stops anyone from
// squatting another member's submission (the nullifier is theirs alone), and
// binds the ciphertext to the proof. Nothing in it is linkable to the
// operation executed later: the operation nullifier is sealed. These live
// here, beside the ciphertext format, so web clients need no SDK types.
const (
	// BatchSubmissionDomain is the nullifier domain of submission proofs. It
	// is outside the range any ShieldedOpRegistration uses.
	BatchSubmissionDomain uint32 = 0xFFFFFFFF

	// BatchSubmissionTypeURL is the pseudo type URL hashed with the encrypted
	// payload into the submission proof's MessageHash. No message of this
	// type exists; it only domain-separates the hash from real inner Anys.
	BatchSubmissionTypeURL = "/sparkdream.shield.v1.EncryptedBatchSubmission"
)

// BatchSubmissionScope is the raw nullifier scope of a submission proof: the
// first eight bytes of sha256(payload) as a big-endian integer, so the
// submission nullifier is unique per (member, payload).
func BatchSubmissionScope(encryptedPayload []byte) uint64 {
	h := sha256.Sum256(encryptedPayload)
	return binary.BigEndian.Uint64(h[:8])
}
