package tle

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"

	"go.dedis.ch/kyber/v4/encrypt/ecies"

	zkcrypto "sparkdream/zkprivatevoting/crypto"
)

// SealedVoteData contains everything needed to submit a MsgSealedVote
// and later reveal the vote (either manually or via TLE auto-reveal).
type SealedVoteData struct {
	VoteOption      uint32 // the vote choice (0-based option index)
	Salt            []byte // 32-byte random salt — SAVE THIS for manual reveal
	Commitment      []byte // MiMC(voteOption, salt) — submitted as vote_commitment
	EncryptedReveal []byte // ECIES ciphertext — submitted as encrypted_reveal
}

// GenerateSalt generates a cryptographically random 32-byte salt for vote sealing.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate random salt: %w", err)
	}
	return salt, nil
}

// SealVote creates all the data needed for a MsgSealedVote submission.
//
// This function:
//  1. Generates a random 32-byte salt (or uses the provided one)
//  2. Computes the vote commitment: MiMC(voteOption_4bytes, salt)
//  3. ECIES-encrypts the payload: voteOption(4B) || salt(32B) using the master public key
//
// The commitment and encrypted reveal are submitted on-chain. The salt must be
// saved locally for manual reveal fallback (in case TLE auto-reveal fails).
//
// Parameters:
//   - masterPublicKey: the TLE master public key bytes (from chain params TleMasterPublicKey)
//   - voteOption: the 0-based vote option index
//   - salt: optional 32-byte salt (pass nil to auto-generate)
func SealVote(masterPublicKey []byte, voteOption uint32, salt []byte) (*SealedVoteData, error) {
	// Generate salt if not provided.
	if salt == nil {
		var err error
		salt, err = GenerateSalt()
		if err != nil {
			return nil, err
		}
	}
	if len(salt) != 32 {
		return nil, fmt.Errorf("salt must be exactly 32 bytes, got %d", len(salt))
	}

	// Compute vote commitment = MiMC(voteOption_4bytes, salt).
	// Must match computeCommitmentHash() in x/vote/keeper/crypto_stubs.go.
	commitment := ComputeVoteCommitment(voteOption, salt)

	// ECIES encrypt: voteOption(4B) || salt(32B) = 36 bytes.
	// Must match decryptTLEPayload() in x/vote/keeper/tle.go.
	encrypted, err := EncryptVotePayload(masterPublicKey, voteOption, salt)
	if err != nil {
		return nil, err
	}

	return &SealedVoteData{
		VoteOption:      voteOption,
		Salt:            salt,
		Commitment:      commitment,
		EncryptedReveal: encrypted,
	}, nil
}

// EncryptVotePayload ECIES-encrypts a vote payload using the TLE master public key.
//
// Plaintext format (36 bytes): voteOption (4 bytes, big-endian uint32) || salt (32 bytes)
// This format must match decryptTLEPayload() in x/vote/keeper/tle.go.
func EncryptVotePayload(masterPublicKey []byte, voteOption uint32, salt []byte) ([]byte, error) {
	if len(salt) != 32 {
		return nil, fmt.Errorf("salt must be 32 bytes, got %d", len(salt))
	}

	// Unmarshal master public key as BN256 G1 point.
	pubPoint := suite.Point()
	if err := pubPoint.UnmarshalBinary(masterPublicKey); err != nil {
		return nil, fmt.Errorf("invalid master public key: %w", err)
	}

	// Build plaintext: voteOption(4B) || salt(32B).
	plaintext := make([]byte, 36)
	binary.BigEndian.PutUint32(plaintext[:4], voteOption)
	copy(plaintext[4:], salt)

	// ECIES encrypt with BN256 G1 suite (SHA256 default hash).
	ciphertext, err := ecies.Encrypt(suite, pubPoint, plaintext, nil)
	if err != nil {
		return nil, fmt.Errorf("ECIES encryption failed: %w", err)
	}

	return ciphertext, nil
}

// ComputeVoteCommitment computes MiMC(voteOption_4bytes, salt).
// Must match computeCommitmentHash() in x/vote/keeper/crypto_stubs.go.
func ComputeVoteCommitment(voteOption uint32, salt []byte) []byte {
	optBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(optBytes, voteOption)
	return zkcrypto.HashToField(optBytes, salt)
}

// ComputeNullifier computes MiMC(secretKey, proposalID) for double-vote prevention.
// Must match ComputeNullifier() in zkprivatevoting/crypto/crypto.go.
func ComputeNullifier(secretKey []byte, proposalID uint64) []byte {
	return zkcrypto.ComputeNullifier(secretKey, proposalID)
}
