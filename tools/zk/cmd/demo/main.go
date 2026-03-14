// Command demo demonstrates the full anonymous shielded action flow.
//
// This example:
// 1. Sets up a mock community with members at different trust levels
// 2. Generates proving/verifying keys (trusted setup)
// 3. Each member generates a proof for an anonymous action
// 4. Verifies all proofs
// 5. Demonstrates rate limiting and nullifier scoping
//
// Usage:
//
//	go run ./tools/zk/cmd/demo
package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	"sparkdream/tools/crypto"
	"sparkdream/tools/zk/circuit"
)

// Member represents a mock community member.
type Member struct {
	Name       string
	SecretKey  []byte
	PublicKey  []byte
	TrustLevel uint64
	LeafIndex  int
}

// Action represents an anonymous shielded action.
type Action struct {
	MemberName  string // Only for demo purposes — NOT visible on-chain!
	Nullifier   []byte
	RLNullifier []byte
	ProofBytes  []byte
	Scope       uint64
	Verified    bool
	VerifyTime  time.Duration
}

func main() {
	fmt.Println("Shielded Execution System - Full Demo")
	fmt.Println()

	// Step 1: Create members with different trust levels
	fmt.Println("Step 1: Creating community members...")

	members := createMembers()
	trustLevelNames := []string{"NEW", "PROVISIONAL", "ESTABLISHED", "CORE"}
	for _, m := range members {
		name := "UNKNOWN"
		if int(m.TrustLevel) < len(trustLevelNames) {
			name = trustLevelNames[m.TrustLevel]
		}
		fmt.Printf("   %s: trust level %d (%s)\n", m.Name, m.TrustLevel, name)
	}
	fmt.Println()

	// Step 2: Build trust Merkle tree
	fmt.Println("Step 2: Building trust tree...")

	tree := crypto.NewMerkleTree(circuit.TreeDepth)
	for i, m := range members {
		leaf := crypto.ComputeLeaf(m.PublicKey, m.TrustLevel)
		if err := tree.AddLeaf(leaf); err != nil {
			fatal("Failed to add member to tree: %v", err)
		}
		members[i].LeafIndex = i
	}

	if err := tree.Build(); err != nil {
		fatal("Failed to build Merkle tree: %v", err)
	}

	merkleRoot := tree.Root()
	fmt.Printf("   Tree built with %d members\n", tree.LeafCount())
	fmt.Printf("   Merkle root: %x...\n", merkleRoot[:8])
	fmt.Println()

	// Step 3: Generate ZK keys (trusted setup)
	fmt.Println("Step 3: Running trusted setup for ShieldCircuit...")

	startTime := time.Now()
	var shieldCircuit circuit.ShieldCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &shieldCircuit)
	if err != nil {
		fatal("Circuit compilation failed: %v", err)
	}
	fmt.Printf("   Circuit compiled: %d constraints\n", ccs.GetNbConstraints())
	fmt.Printf("   Public inputs: %d | Private inputs: %d\n",
		ccs.GetNbPublicVariables(), ccs.GetNbSecretVariables())

	pk, vk, err := groth16.Setup(ccs)
	if err != nil {
		fatal("Setup failed: %v", err)
	}
	setupTime := time.Since(startTime)
	fmt.Printf("   Keys generated in %v\n", setupTime)
	fmt.Println()

	// Step 4: Simulate anonymous actions
	fmt.Println("Step 4: Members performing anonymous actions...")
	fmt.Println("   (Minimum trust level required: 1 = PROVISIONAL)")

	epoch := uint64(42)
	minTrustLevel := uint64(1) // Require at least PROVISIONAL

	var actions []Action
	var totalProveTime time.Duration

	for _, member := range members {
		// Each member gets a unique scope (e.g., post ID they're reacting to)
		scope := uint64(100 + member.LeafIndex)

		// Get Merkle proof
		merkleProof, err := tree.GetProof(member.LeafIndex)
		if err != nil {
			fatal("Failed to get merkle proof for %s: %v", member.Name, err)
		}

		// Compute nullifiers
		nullifier := crypto.ComputeNullifier(member.SecretKey, scope)
		rlNullifier := crypto.ComputeRateLimitNullifier(member.SecretKey, epoch)

		// Build circuit assignment
		assignment := &circuit.ShieldCircuit{
			MerkleRoot:         crypto.BytesToFieldElement(merkleRoot),
			Nullifier:          crypto.BytesToFieldElement(nullifier),
			RateLimitNullifier: crypto.BytesToFieldElement(rlNullifier),
			MinTrustLevel:      minTrustLevel,
			Scope:              scope,
			RateLimitEpoch:     epoch,
			SecretKey:          crypto.BytesToFieldElement(member.SecretKey),
			TrustLevel:         member.TrustLevel,
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

		// Generate proof
		witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
		if err != nil {
			// Trust level too low — proof will fail at witness creation
			fmt.Printf("   %s: SKIPPED (trust level %d < required %d)\n",
				member.Name, member.TrustLevel, minTrustLevel)
			continue
		}

		proveStart := time.Now()
		proof, proveErr := groth16.Prove(ccs, pk, witness)
		proveTime := time.Since(proveStart)
		totalProveTime += proveTime

		if proveErr != nil {
			fmt.Printf("   %s: PROOF FAILED — trust level %d < required %d (proof: %v)\n",
				member.Name, member.TrustLevel, minTrustLevel, proveTime.Round(time.Millisecond))
			continue
		}

		// Serialize proof
		var proofBuf bytes.Buffer
		proof.WriteTo(&proofBuf)

		actions = append(actions, Action{
			MemberName:  member.Name,
			Nullifier:   nullifier,
			RLNullifier: rlNullifier,
			ProofBytes:  proofBuf.Bytes(),
			Scope:       scope,
		})

		fmt.Printf("   %s acted anonymously (scope=%d, proof: %v)\n",
			member.Name, scope, proveTime.Round(time.Millisecond))
	}

	fmt.Printf("   Total proving time: %v\n", totalProveTime.Round(time.Millisecond))
	fmt.Println()

	// Step 5: Verify all proofs (simulating on-chain verification)
	fmt.Println("Step 5: Verifying proofs (on-chain simulation)...")

	nullifierSet := make(map[string]bool)
	rlCounts := make(map[string]int)
	maxPerEpoch := 5 // Rate limit
	var totalVerifyTime time.Duration

	for i := range actions {
		action := &actions[i]

		// Check action nullifier (dedup per scope)
		nullifierHex := fmt.Sprintf("%x", action.Nullifier)
		if nullifierSet[nullifierHex] {
			fmt.Printf("   REJECTED - duplicate nullifier (same action, same scope)\n")
			continue
		}

		// Check rate limit nullifier
		rlHex := fmt.Sprintf("%x", action.RLNullifier)
		if rlCounts[rlHex] >= maxPerEpoch {
			fmt.Printf("   REJECTED - rate limit exceeded (%d/%d per epoch)\n",
				rlCounts[rlHex], maxPerEpoch)
			continue
		}

		// Deserialize and verify proof
		proof := groth16.NewProof(ecc.BN254)
		if _, err := proof.ReadFrom(bytes.NewReader(action.ProofBytes)); err != nil {
			fmt.Printf("   REJECTED - invalid proof format\n")
			continue
		}

		publicAssignment := &circuit.ShieldCircuit{
			MerkleRoot:         crypto.BytesToFieldElement(merkleRoot),
			Nullifier:          crypto.BytesToFieldElement(action.Nullifier),
			RateLimitNullifier: crypto.BytesToFieldElement(action.RLNullifier),
			MinTrustLevel:      minTrustLevel,
			Scope:              action.Scope,
			RateLimitEpoch:     epoch,
		}

		publicWitness, err := frontend.NewWitness(
			publicAssignment,
			ecc.BN254.ScalarField(),
			frontend.PublicOnly(),
		)
		if err != nil {
			fmt.Printf("   REJECTED - witness error\n")
			continue
		}

		verifyStart := time.Now()
		err = groth16.Verify(proof, vk, publicWitness)
		verifyTime := time.Since(verifyStart)
		totalVerifyTime += verifyTime

		if err != nil {
			fmt.Printf("   REJECTED - proof verification failed\n")
			continue
		}

		action.Verified = true
		action.VerifyTime = verifyTime
		nullifierSet[nullifierHex] = true
		rlCounts[rlHex]++

		fmt.Printf("   Action verified (verify: %v)\n", verifyTime.Round(time.Microsecond))
	}

	fmt.Printf("   Total verification time: %v\n", totalVerifyTime.Round(time.Millisecond))
	fmt.Println()

	// Step 6: Summary
	fmt.Println("Step 6: Summary")
	fmt.Println()

	verified := 0
	for _, a := range actions {
		if a.Verified {
			verified++
		}
	}

	fmt.Printf("  Members: %d total, %d actions generated, %d verified\n",
		len(members), len(actions), verified)
	fmt.Println()
	fmt.Println("  Privacy guarantees:")
	fmt.Println("    - Member identity is NEVER revealed")
	fmt.Println("    - Exact trust level is hidden (only proves >= minimum)")
	fmt.Println("    - Actions across different scopes are unlinkable")
	fmt.Println("    - Rate limiting works without breaking anonymity")
	fmt.Println("      (same member = same rate-limit nullifier per epoch)")
	fmt.Println()
	fmt.Println("  Rate-limit nullifier analysis:")

	rlSeen := make(map[string]int)
	for _, a := range actions {
		if a.Verified {
			rlHex := fmt.Sprintf("%x", a.RLNullifier[:4])
			rlSeen[rlHex]++
		}
	}
	fmt.Printf("    %d unique rate-limit nullifiers across %d verified actions\n",
		len(rlSeen), verified)
	fmt.Println("    (Each member has exactly one RL nullifier per epoch)")
	fmt.Println()
}

// createMembers generates test members with random keys and varied trust levels.
func createMembers() []*Member {
	memberData := []struct {
		name  string
		trust uint64
	}{
		{"Phoenix", 3}, // CORE
		{"Aurora", 2},  // ESTABLISHED
		{"Zenith", 2},  // ESTABLISHED
		{"Drift", 1},   // PROVISIONAL
		{"Nova", 0},    // NEW — will fail minimum trust check
	}

	members := make([]*Member, len(memberData))
	for i, data := range memberData {
		entropy := make([]byte, 32)
		if _, err := rand.Read(entropy); err != nil {
			fatal("Failed to generate entropy: %v", err)
		}

		keys, err := crypto.GenerateVoterKeys(entropy)
		if err != nil {
			fatal("Failed to generate keys: %v", err)
		}

		members[i] = &Member{
			Name:       data.name,
			SecretKey:  keys.SecretKey,
			PublicKey:  keys.PublicKey,
			TrustLevel: data.trust,
		}
	}

	return members
}

func fatal(format string, args ...interface{}) {
	fmt.Printf("\nError: "+format+"\n", args...)
	panic("demo failed")
}
