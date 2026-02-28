package tle

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ShareFile is the JSON format for a validator's DKG share file.
type ShareFile struct {
	ShareIndex        int    `json:"share_index"`
	PrivateScalarHex  string `json:"private_scalar_hex"`
	PublicKeyShareHex string `json:"public_key_share_hex"`
}

// MasterFile is the JSON format for the DKG master output.
type MasterFile struct {
	MasterPublicKeyHex string `json:"master_public_key_hex"`
	Threshold          int    `json:"threshold"`
	TotalValidators    int    `json:"total_validators"`
}

// SealedVoteFile is the JSON format for a sealed vote's local state.
// Voters save this to reveal their vote later if auto-reveal fails.
type SealedVoteFile struct {
	ProposalID    uint64 `json:"proposal_id"`
	VoteOption    uint32 `json:"vote_option"`
	SaltHex       string `json:"salt_hex"`
	NullifierHex  string `json:"nullifier_hex"`
	CommitmentHex string `json:"commitment_hex"`
	EncryptedHex  string `json:"encrypted_hex"`
}

// SaveDKGOutput writes the DKG output to the given directory as JSON files.
//
// Generated files:
//   - master.json — master public key + threshold info (safe to share)
//   - validator_1.json, validator_2.json, ... — per-validator share files (KEEP PRIVATE)
//
// The output directory is created with 0700 permissions. Share files are
// created with 0600 permissions (owner-only read/write).
func SaveDKGOutput(output *DKGOutput, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save master file.
	masterFile := MasterFile{
		MasterPublicKeyHex: hex.EncodeToString(output.MasterPublicKey),
		Threshold:          output.Threshold,
		TotalValidators:    output.TotalValidators,
	}

	masterJSON, err := json.MarshalIndent(masterFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal master file: %w", err)
	}

	masterPath := filepath.Join(outputDir, "master.json")
	if err := os.WriteFile(masterPath, masterJSON, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", masterPath, err)
	}

	// Save per-validator share files.
	for _, share := range output.ValidatorShares {
		sf := ShareFile{
			ShareIndex:        share.Index,
			PrivateScalarHex:  hex.EncodeToString(share.PrivateScalar),
			PublicKeyShareHex: hex.EncodeToString(share.PublicKeyShare),
		}

		shareJSON, err := json.MarshalIndent(sf, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal share %d: %w", share.Index, err)
		}

		filename := fmt.Sprintf("validator_%d.json", share.Index)
		sharePath := filepath.Join(outputDir, filename)
		// 0600: owner read/write only — this file contains the private scalar.
		if err := os.WriteFile(sharePath, shareJSON, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", sharePath, err)
		}
	}

	return nil
}

// LoadShareFromFile reads a validator's DKG share from a JSON file.
func LoadShareFromFile(path string) (*ValidatorShare, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read share file: %w", err)
	}

	var sf ShareFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("failed to parse share file: %w", err)
	}

	privBytes, err := hex.DecodeString(sf.PrivateScalarHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private scalar hex: %w", err)
	}

	pubBytes, err := hex.DecodeString(sf.PublicKeyShareHex)
	if err != nil {
		return nil, fmt.Errorf("invalid public key share hex: %w", err)
	}

	return &ValidatorShare{
		Index:          sf.ShareIndex,
		PrivateScalar:  privBytes,
		PublicKeyShare: pubBytes,
	}, nil
}

// LoadMasterFile reads the DKG master output from a JSON file.
func LoadMasterFile(path string) (*MasterFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read master file: %w", err)
	}

	var mf MasterFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("failed to parse master file: %w", err)
	}

	return &mf, nil
}

// SaveSealedVote writes sealed vote data to a JSON file for later reveal.
func SaveSealedVote(svd *SealedVoteData, proposalID uint64, nullifier []byte, path string) error {
	svf := SealedVoteFile{
		ProposalID:    proposalID,
		VoteOption:    svd.VoteOption,
		SaltHex:       hex.EncodeToString(svd.Salt),
		NullifierHex:  hex.EncodeToString(nullifier),
		CommitmentHex: hex.EncodeToString(svd.Commitment),
		EncryptedHex:  hex.EncodeToString(svd.EncryptedReveal),
	}

	data, err := json.MarshalIndent(svf, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sealed vote: %w", err)
	}

	// 0600: owner read/write only — contains vote choice.
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

// LoadSealedVote reads sealed vote data from a JSON file.
func LoadSealedVote(path string) (*SealedVoteFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read sealed vote file: %w", err)
	}

	var svf SealedVoteFile
	if err := json.Unmarshal(data, &svf); err != nil {
		return nil, fmt.Errorf("failed to parse sealed vote file: %w", err)
	}

	return &svf, nil
}
