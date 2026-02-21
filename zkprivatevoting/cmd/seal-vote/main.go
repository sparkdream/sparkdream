// Command seal-vote creates a sealed vote for submission to x/vote.
//
// Usage:
//
//	go run ./zkprivatevoting/cmd/seal-vote \
//	  --master-pubkey <hex> \
//	  --vote-option 1 \
//	  --secret-key <hex> \
//	  --proposal-id 42 \
//	  --save ./my_sealed_vote.json
//
// This computes:
//   - vote_commitment: MiMC(voteOption, salt) — submitted on-chain
//   - encrypted_reveal: ECIES(masterPubKey, voteOption || salt) — submitted on-chain
//   - nullifier: MiMC(secretKey, proposalID) — submitted on-chain for double-vote prevention
//   - salt: random 32 bytes — saved locally for manual reveal fallback
//
// The --save flag writes a JSON file with all data needed for later reveal.
// Without it, data is printed to stdout.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"sparkdream/zkprivatevoting/tle"
)

func main() {
	masterPubKeyHex := flag.String("master-pubkey", "", "TLE master public key (hex, from master.json or chain params)")
	voteOption := flag.Uint("vote-option", 0, "Vote option index (0-based)")
	secretKeyHex := flag.String("secret-key", "", "Voter's ZK secret key (hex, 32 bytes) — for nullifier computation")
	proposalID := flag.Uint64("proposal-id", 0, "Proposal ID (for nullifier computation)")
	saltHex := flag.String("salt", "", "Salt (hex, 32 bytes) — auto-generated if empty")
	savePath := flag.String("save", "", "Save sealed vote data to this JSON file (for later reveal)")
	flag.Parse()

	if *masterPubKeyHex == "" {
		fmt.Fprintf(os.Stderr, "Usage: seal-vote --master-pubkey <hex> --vote-option <N> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Required:\n")
		fmt.Fprintf(os.Stderr, "  --master-pubkey  TLE master public key (hex)\n")
		fmt.Fprintf(os.Stderr, "  --vote-option    Vote option index (0-based)\n\n")
		fmt.Fprintf(os.Stderr, "Optional:\n")
		fmt.Fprintf(os.Stderr, "  --secret-key     Voter ZK secret key (hex) for nullifier\n")
		fmt.Fprintf(os.Stderr, "  --proposal-id    Proposal ID for nullifier\n")
		fmt.Fprintf(os.Stderr, "  --salt           Custom salt (hex, 32 bytes)\n")
		fmt.Fprintf(os.Stderr, "  --save           Save data to JSON file for later reveal\n")
		os.Exit(1)
	}

	masterPubKey, err := hex.DecodeString(*masterPubKeyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid master public key hex: %v\n", err)
		os.Exit(1)
	}

	// Parse optional salt.
	var salt []byte
	if *saltHex != "" {
		salt, err = hex.DecodeString(*saltHex)
		if err != nil || len(salt) != 32 {
			fmt.Fprintf(os.Stderr, "Salt must be exactly 32 bytes hex-encoded\n")
			os.Exit(1)
		}
	}

	voteOpt := uint32(*voteOption)

	// Seal the vote.
	sealed, err := tle.SealVote(masterPubKey, voteOpt, salt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to seal vote: %v\n", err)
		os.Exit(1)
	}

	// Compute nullifier if secret key is provided.
	var nullifier []byte
	if *secretKeyHex != "" {
		secretKey, err := hex.DecodeString(*secretKeyHex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid secret key hex: %v\n", err)
			os.Exit(1)
		}
		nullifier = tle.ComputeNullifier(secretKey, *proposalID)
	}

	// Print results.
	fmt.Printf("Sealed Vote Data\n")
	fmt.Printf("================\n\n")
	fmt.Printf("  Vote option:        %d\n", sealed.VoteOption)
	fmt.Printf("  Salt (hex):         %s\n", hex.EncodeToString(sealed.Salt))
	fmt.Printf("  Commitment (hex):   %s\n", hex.EncodeToString(sealed.Commitment))
	fmt.Printf("  Encrypted (hex):    %s\n", hex.EncodeToString(sealed.EncryptedReveal))
	fmt.Printf("  Encrypted size:     %d bytes\n", len(sealed.EncryptedReveal))
	if nullifier != nil {
		fmt.Printf("  Nullifier (hex):    %s\n", hex.EncodeToString(nullifier))
	}

	// Save to file if requested.
	if *savePath != "" {
		if err := tle.SaveSealedVote(sealed, *proposalID, nullifier, *savePath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n  Saved to: %s\n", *savePath)
	}

	fmt.Printf("\n--- Submit sealed vote ---\n\n")

	commitHex := hex.EncodeToString(sealed.Commitment)
	encryptedHex := hex.EncodeToString(sealed.EncryptedReveal)
	fmt.Printf("sparkdreamd tx vote sealed-vote \\\n")
	fmt.Printf("  --proposal-id %d \\\n", *proposalID)
	if nullifier != nil {
		fmt.Printf("  --nullifier %s \\\n", hex.EncodeToString(nullifier))
	} else {
		fmt.Printf("  --nullifier <nullifier_hex> \\\n")
	}
	fmt.Printf("  --vote-commitment %s \\\n", commitHex)
	fmt.Printf("  --proof <zk_proof_hex> \\\n")
	fmt.Printf("  --encrypted-reveal %s \\\n", encryptedHex)
	fmt.Printf("  --from <voter_key>\n")

	fmt.Printf("\n--- Manual reveal fallback ---\n\n")
	fmt.Printf("If TLE auto-reveal fails, reveal manually after epoch key is available:\n\n")
	fmt.Printf("sparkdreamd tx vote reveal-vote \\\n")
	fmt.Printf("  --proposal-id %d \\\n", *proposalID)
	if nullifier != nil {
		fmt.Printf("  --nullifier %s \\\n", hex.EncodeToString(nullifier))
	} else {
		fmt.Printf("  --nullifier <nullifier_hex> \\\n")
	}
	fmt.Printf("  --vote-option %d \\\n", sealed.VoteOption)
	fmt.Printf("  --reveal-salt %s \\\n", hex.EncodeToString(sealed.Salt))
	fmt.Printf("  --from <voter_key>\n\n")

	fmt.Printf("IMPORTANT: Save the salt (%s) for manual reveal.\n", hex.EncodeToString(sealed.Salt))
	fmt.Printf("Without it, your sealed vote cannot be revealed if auto-reveal fails.\n")
}
