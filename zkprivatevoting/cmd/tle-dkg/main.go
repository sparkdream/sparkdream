// Command tle-dkg runs a dealer-based DKG ceremony for Threshold Timelock Encryption.
//
// Usage:
//
//	go run ./zkprivatevoting/cmd/tle-dkg --threshold 2 --validators 3 --output ./tle-shares
//
// This generates:
//   - master.json — master public key (set as TleMasterPublicKey chain param)
//   - validator_N.json — per-validator share files (distribute securely, one per validator)
//
// After running, each validator registers their share on-chain:
//
//	sparkdreamd tx vote register-tle-share <public_key_share_hex> <share_index> --from <validator_key>
//
// And the governance authority sets the master public key param:
//
//	sparkdreamd tx vote update-params ... --tle-master-public-key <master_public_key_hex>
//
// SECURITY: In production, replace this dealer-based DKG with a proper distributed
// protocol (Pedersen DKG or FROST) so no single party ever knows the master secret.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"sparkdream/zkprivatevoting/tle"
)

func main() {
	threshold := flag.Int("threshold", 0, "Minimum shares needed to reconstruct (required)")
	validators := flag.Int("validators", 0, "Total number of validators (required)")
	outputDir := flag.String("output", "./tle-shares", "Output directory for share files")
	flag.Parse()

	if *threshold <= 0 || *validators <= 0 {
		fmt.Fprintf(os.Stderr, "Usage: tle-dkg --threshold T --validators N [--output DIR]\n\n")
		fmt.Fprintf(os.Stderr, "Example: tle-dkg --threshold 2 --validators 3\n")
		fmt.Fprintf(os.Stderr, "  Generates a 2-of-3 threshold key setup\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Run DKG.
	output, err := tle.RunDKG(*threshold, *validators)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DKG failed: %v\n", err)
		os.Exit(1)
	}

	// Save outputs.
	if err := tle.SaveDKGOutput(output, *outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save DKG output: %v\n", err)
		os.Exit(1)
	}

	// Print summary.
	fmt.Printf("DKG ceremony complete.\n\n")
	fmt.Printf("  Threshold:          %d-of-%d\n", output.Threshold, output.TotalValidators)
	fmt.Printf("  Master public key:  %s\n", hex.EncodeToString(output.MasterPublicKey))
	fmt.Printf("  Output directory:   %s\n\n", *outputDir)

	fmt.Printf("Generated files:\n")
	fmt.Printf("  master.json              Master public key (set as TleMasterPublicKey param)\n")
	for _, vs := range output.ValidatorShares {
		fmt.Printf("  validator_%d.json         Share for validator %d (KEEP PRIVATE)\n", vs.Index, vs.Index)
	}

	fmt.Printf("\n--- Next steps ---\n\n")

	fmt.Printf("1. Each validator registers their TLE share on-chain:\n\n")
	for _, vs := range output.ValidatorShares {
		fmt.Printf("   sparkdreamd tx vote register-tle-share \\\n")
		fmt.Printf("     %s \\\n", hex.EncodeToString(vs.PublicKeyShare))
		fmt.Printf("     %d \\\n", vs.Index)
		fmt.Printf("     --from <validator_%d_key>\n\n", vs.Index)
	}

	fmt.Printf("2. Set the master public key in module params (governance proposal or direct update):\n\n")
	fmt.Printf("   Master public key (hex): %s\n\n", hex.EncodeToString(output.MasterPublicKey))

	fmt.Printf("3. Each epoch, validators submit their decryption share:\n\n")
	fmt.Printf("   sparkdreamd tx vote submit-decryption-share \\\n")
	fmt.Printf("     <epoch> \\\n")
	fmt.Printf("     <private_scalar_hex from validator_N.json> \\\n")
	fmt.Printf("     --from <validator_key>\n\n")

	fmt.Printf("SECURITY: Validator share files contain private keys. Distribute them\n")
	fmt.Printf("securely and delete the originals after distribution.\n")
}
