// Command setup generates the proving and verifying keys for the shield circuit.
//
// Usage:
//
//	go run ./tools/zk/cmd/setup
//
// This will create:
//   - keys/proving_key.bin   - Used by members to generate proofs (~50-100 MB)
//   - keys/verifying_key.bin - Used on-chain to verify proofs (~1-2 KB)
//   - keys/circuit.r1cs      - Compiled circuit (optional, speeds up proving)
//
// SECURITY NOTE:
// This performs a local trusted setup. The randomness used ("toxic waste") must
// be destroyed after setup. For production, consider:
//  1. Running an MPC ceremony with multiple participants, OR
//  2. Using PLONK with a universal trusted setup
package main

import (
	"bytes"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	"sparkdream/tools/crypto"
	"sparkdream/tools/zk/circuit"
)

var (
	outputDir = flag.String("output", "keys", "Output directory for keys")
	testProof = flag.Bool("test", true, "Generate and verify a test proof after setup")
	verbose   = flag.Bool("v", true, "Verbose output")
)

func main() {
	flag.Parse()

	fmt.Println("Shield Circuit - Trusted Setup")
	fmt.Println()

	// Create output directory
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		fatal("Failed to create output directory: %v", err)
	}

	// Step 1: Compile the circuit
	step("1", "Compiling shield circuit...")

	startTime := time.Now()
	var shieldCircuit circuit.ShieldCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &shieldCircuit)
	if err != nil {
		fatal("Circuit compilation failed: %v", err)
	}
	compileDuration := time.Since(startTime)

	info("Circuit compiled in %v", compileDuration)
	info("  Constraints: %d", ccs.GetNbConstraints())
	info("  Public inputs: %d", ccs.GetNbPublicVariables())
	info("  Private inputs: %d", ccs.GetNbSecretVariables())
	info("  Tree depth: %d (max %d members)", circuit.TreeDepth, 1<<circuit.TreeDepth)
	fmt.Println()

	// Step 2: Run trusted setup (Groth16)
	step("2", "Running Groth16 trusted setup...")
	info("This may take a minute...")

	startTime = time.Now()
	pk, vk, err := groth16.Setup(ccs)
	if err != nil {
		fatal("Trusted setup failed: %v", err)
	}
	setupDuration := time.Since(startTime)

	info("Setup completed in %v", setupDuration)
	fmt.Println()

	// Step 3: Save the proving key
	step("3", "Saving proving key...")

	pkPath := filepath.Join(*outputDir, "proving_key.bin")
	pkFile, err := os.Create(pkPath)
	if err != nil {
		fatal("Failed to create proving key file: %v", err)
	}

	pkBytes, err := pk.WriteTo(pkFile)
	if err != nil {
		pkFile.Close()
		fatal("Failed to write proving key: %v", err)
	}
	pkFile.Close()

	info("Proving key saved to %s (%s)", pkPath, formatBytes(pkBytes))
	fmt.Println()

	// Step 4: Save the verifying key
	step("4", "Saving verifying key...")

	vkPath := filepath.Join(*outputDir, "verifying_key.bin")
	vkFile, err := os.Create(vkPath)
	if err != nil {
		fatal("Failed to create verifying key file: %v", err)
	}

	vkBytes, err := vk.WriteTo(vkFile)
	if err != nil {
		vkFile.Close()
		fatal("Failed to write verifying key: %v", err)
	}
	vkFile.Close()

	info("Verifying key saved to %s (%s)", vkPath, formatBytes(vkBytes))

	// Also save as bytes for embedding in genesis
	var vkBuf bytes.Buffer
	vk.WriteTo(&vkBuf)
	vkHexPath := filepath.Join(*outputDir, "verifying_key.hex")
	os.WriteFile(vkHexPath, []byte(fmt.Sprintf("%x", vkBuf.Bytes())), 0644)
	info("Verifying key (hex) saved to %s", vkHexPath)
	fmt.Println()

	// Step 5: Save compiled constraint system
	step("5", "Saving compiled constraint system...")

	ccsPath := filepath.Join(*outputDir, "circuit.r1cs")
	ccsFile, err := os.Create(ccsPath)
	if err != nil {
		fatal("Failed to create R1CS file: %v", err)
	}

	ccsBytes, err := ccs.WriteTo(ccsFile)
	if err != nil {
		ccsFile.Close()
		fatal("Failed to write R1CS: %v", err)
	}
	ccsFile.Close()

	info("Constraint system saved to %s (%s)", ccsPath, formatBytes(ccsBytes))
	fmt.Println()

	// Step 6: Test proof generation and verification
	if *testProof {
		step("6", "Testing proof generation and verification...")

		if err := testProofGeneration(ccs, pk, vk); err != nil {
			fatal("Test proof failed: %v", err)
		}

		info("Test proof generated and verified successfully!")
		fmt.Println()
	}

	// Summary
	fmt.Println("Setup Complete!")
	fmt.Println()
	fmt.Println("Generated files:")
	fmt.Printf("  %s/\n", *outputDir)
	fmt.Printf("     proving_key.bin     (%s) - Distribute to members\n", formatBytes(pkBytes))
	fmt.Printf("     verifying_key.bin   (%s) - Embed in chain params\n", formatBytes(vkBytes))
	fmt.Printf("     verifying_key.hex           - Hex-encoded for genesis\n")
	fmt.Printf("     circuit.r1cs        (%s) - Optional, speeds up proving\n", formatBytes(ccsBytes))
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Store verifying_key.bin on-chain as circuit_id=\"shield_v1\"")
	fmt.Println("  2. Distribute proving_key.bin to members via your client app")
	fmt.Println("  3. Keep backup copies of these keys - regenerating creates")
	fmt.Println("     incompatible proofs!")
	fmt.Println()
	fmt.Println("Security reminder:")
	fmt.Println("    This was a local trusted setup. For production use, consider")
	fmt.Println("    running an MPC ceremony or switching to PLONK for universal setup.")
	fmt.Println()
}

// testProofGeneration creates and verifies a test proof using the shield circuit.
func testProofGeneration(
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	vk groth16.VerifyingKey,
) error {
	// Create a test member
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return fmt.Errorf("failed to generate entropy: %w", err)
	}

	keys, err := crypto.GenerateVoterKeys(entropy)
	if err != nil {
		return fmt.Errorf("failed to generate keys: %w", err)
	}

	trustLevel := uint64(2)    // ESTABLISHED
	minTrustLevel := uint64(1) // Require at least PROVISIONAL
	scope := uint64(1)         // Test scope (e.g., epoch)
	rateLimitEpoch := uint64(1)

	// Create Merkle tree with this member
	leaf := crypto.ComputeLeaf(keys.PublicKey, trustLevel)
	tree := crypto.NewMerkleTree(circuit.TreeDepth)
	if err := tree.AddLeaf(leaf); err != nil {
		return fmt.Errorf("failed to add leaf: %w", err)
	}
	if err := tree.Build(); err != nil {
		return fmt.Errorf("failed to build tree: %w", err)
	}

	// Get Merkle proof
	merkleProof, err := tree.GetProof(0)
	if err != nil {
		return fmt.Errorf("failed to get merkle proof: %w", err)
	}

	// Compute nullifiers
	nullifier := crypto.ComputeNullifier(keys.SecretKey, scope)
	rateLimitNullifier := crypto.ComputeRateLimitNullifier(keys.SecretKey, rateLimitEpoch)

	// Build circuit assignment
	assignment := &circuit.ShieldCircuit{
		MerkleRoot:         crypto.BytesToFieldElement(merkleProof.Root),
		Nullifier:          crypto.BytesToFieldElement(nullifier),
		RateLimitNullifier: crypto.BytesToFieldElement(rateLimitNullifier),
		MinTrustLevel:      minTrustLevel,
		Scope:              scope,
		RateLimitEpoch:     rateLimitEpoch,
		SecretKey:          crypto.BytesToFieldElement(keys.SecretKey),
		TrustLevel:         trustLevel,
	}

	for i := 0; i < circuit.TreeDepth; i++ {
		if i < len(merkleProof.PathElements) {
			assignment.PathElements[i] = crypto.BytesToFieldElement(merkleProof.PathElements[i])
			assignment.PathIndices[i] = merkleProof.PathIndices[i]
		} else {
			assignment.PathElements[i] = 0
			assignment.PathIndices[i] = 0
		}
	}

	// Create witness
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	if err != nil {
		return fmt.Errorf("failed to create witness: %w", err)
	}

	// Generate proof
	startTime := time.Now()
	proof, err := groth16.Prove(ccs, pk, witness)
	if err != nil {
		return fmt.Errorf("proof generation failed: %w", err)
	}
	proveDuration := time.Since(startTime)
	info("  Proof generated in %v", proveDuration)

	// Get proof size
	var proofBuf bytes.Buffer
	proof.WriteTo(&proofBuf)
	info("  Proof size: %s", formatBytes(int64(proofBuf.Len())))

	// Extract public witness
	publicWitness, err := witness.Public()
	if err != nil {
		return fmt.Errorf("failed to extract public witness: %w", err)
	}

	// Verify proof
	startTime = time.Now()
	err = groth16.Verify(proof, vk, publicWitness)
	if err != nil {
		return fmt.Errorf("proof verification failed: %w", err)
	}
	verifyDuration := time.Since(startTime)
	info("  Proof verified in %v", verifyDuration)

	return nil
}

// Helper functions for formatted output

func step(num, msg string) {
	fmt.Printf("\n[Step %s] %s\n", num, msg)
}

func info(format string, args ...interface{}) {
	if *verbose {
		fmt.Printf("  "+format+"\n", args...)
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Printf("\nError: "+format+"\n", args...)
	os.Exit(1)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
