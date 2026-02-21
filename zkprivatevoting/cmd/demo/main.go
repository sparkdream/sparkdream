// Command demo demonstrates the full private voting flow.
//
// This example:
// 1. Sets up a mock election with multiple voters
// 2. Generates proving/verifying keys
// 3. Each voter generates a proof for their vote
// 4. Verifies all proofs
// 5. Tallies the results
//
// Usage:
//
//	go run ./zkprivatevoting/cmd/demo
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

	"sparkdream/zkprivatevoting/circuit"
	"sparkdream/zkprivatevoting/crypto"
	"sparkdream/zkprivatevoting/prover"
)

// Voter represents a mock voter
type Voter struct {
	Name        string
	SecretKey   []byte
	PublicKey   []byte
	VotingPower uint64
	LeafIndex   int
}

// Vote represents a submitted vote
type Vote struct {
	VoterName  string // Only for demo purposes - NOT visible on-chain!
	Output     *prover.VoteProofOutput
	Verified   bool
	VerifyTime time.Duration
}

func main() {
	fmt.Println("Private Voting System - Full Demo")
	fmt.Println()

	// Step 1: Create voters
	fmt.Println("Step 1: Creating voters...")

	voters := createVoters()
	for _, v := range voters {
		fmt.Printf("   %s: %d voting power\n", v.Name, v.VotingPower)
	}
	fmt.Println()

	// Step 2: Build voter Merkle tree
	fmt.Println("Step 2: Building voter eligibility Merkle tree...")

	tree := crypto.NewMerkleTree(circuit.TreeDepth)
	for i, v := range voters {
		leaf := crypto.ComputeLeaf(v.PublicKey, v.VotingPower)
		if err := tree.AddLeaf(leaf); err != nil {
			fatal("Failed to add voter to tree: %v", err)
		}
		voters[i].LeafIndex = i
	}

	if err := tree.Build(); err != nil {
		fatal("Failed to build Merkle tree: %v", err)
	}

	merkleRoot := tree.Root()
	fmt.Printf("   Tree built with %d voters\n", tree.LeafCount())
	fmt.Printf("   Merkle root: %x...\n", merkleRoot[:8])
	fmt.Println()

	// Step 3: Generate ZK keys (trusted setup)
	fmt.Println("Step 3: Running trusted setup...")

	startTime := time.Now()
	var voteCircuit circuit.VoteCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &voteCircuit)
	if err != nil {
		fatal("Circuit compilation failed: %v", err)
	}
	fmt.Printf("   Circuit compiled: %d constraints\n", ccs.GetNbConstraints())

	pk, vk, err := groth16.Setup(ccs)
	if err != nil {
		fatal("Setup failed: %v", err)
	}
	setupTime := time.Since(startTime)
	fmt.Printf("   Keys generated in %v\n", setupTime)
	fmt.Println()

	// Step 4: Create a proposal
	fmt.Println("Step 4: Creating proposal...")

	proposal := struct {
		ID          uint64
		Title       string
		Description string
		MerkleRoot  []byte
	}{
		ID:          1,
		Title:       "Upgrade to Protocol v2",
		Description: "Proposal to upgrade the chain to protocol version 2",
		MerkleRoot:  merkleRoot,
	}

	fmt.Printf("   Proposal #%d: %s\n", proposal.ID, proposal.Title)
	fmt.Println()

	// Step 5: Voters cast their votes
	fmt.Println("Step 5: Voters casting anonymous votes...")

	// Define how each voter votes (for demo purposes only)
	voteChoices := map[string]uint8{
		"Alice":   0, // Yes
		"Bob":     1, // No
		"Charlie": 0, // Yes
		"Diana":   2, // Abstain
		"Eve":     0, // Yes
	}

	voteOptionNames := []string{"Yes", "No", "Abstain"}

	var votes []Vote
	var totalProveTime time.Duration

	for _, voter := range voters {
		choice := voteChoices[voter.Name]

		// Get Merkle proof for this voter
		merkleProof, err := tree.GetProof(voter.LeafIndex)
		if err != nil {
			fatal("Failed to get merkle proof for %s: %v", voter.Name, err)
		}

		// Compute nullifier
		nullifier := crypto.ComputeNullifier(voter.SecretKey, proposal.ID)

		// Build circuit assignment
		assignment := &circuit.VoteCircuit{
			MerkleRoot:  crypto.BytesToFieldElement(merkleRoot),
			Nullifier:   crypto.BytesToFieldElement(nullifier),
			ProposalID:  proposal.ID,
			VoteOption:  uint64(choice),
			VotingPower: voter.VotingPower,
			SecretKey:   crypto.BytesToFieldElement(voter.SecretKey),
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
			fatal("Failed to create witness for %s: %v", voter.Name, err)
		}

		proveStart := time.Now()
		proof, err := groth16.Prove(ccs, pk, witness)
		if err != nil {
			fatal("Proof generation failed for %s: %v", voter.Name, err)
		}
		proveTime := time.Since(proveStart)
		totalProveTime += proveTime

		// Serialize proof
		var proofBuf bytes.Buffer
		proof.WriteTo(&proofBuf)

		output := &prover.VoteProofOutput{
			ProofBytes:  proofBuf.Bytes(),
			Nullifier:   nullifier,
			ProposalID:  proposal.ID,
			VoteOption:  choice,
			VotingPower: voter.VotingPower,
			MerkleRoot:  merkleRoot,
			ProvingTime: proveTime,
		}

		votes = append(votes, Vote{
			VoterName: voter.Name,
			Output:    output,
		})

		fmt.Printf("   %s voted %s (proof: %v)\n",
			voter.Name, voteOptionNames[choice], proveTime.Round(time.Millisecond))
	}

	fmt.Printf("   Total proving time: %v\n", totalProveTime.Round(time.Millisecond))
	fmt.Println()

	// Step 6: Verify all proofs (simulating on-chain verification)
	fmt.Println("Step 6: Verifying proofs (on-chain simulation)...")

	nullifierSet := make(map[string]bool)
	var totalVerifyTime time.Duration

	for i := range votes {
		vote := &votes[i]

		// Check for double voting (nullifier already used)
		nullifierHex := fmt.Sprintf("%x", vote.Output.Nullifier)
		if nullifierSet[nullifierHex] {
			fmt.Printf("   %s: REJECTED - nullifier already used!\n", vote.VoterName)
			continue
		}

		// Deserialize proof
		proof := groth16.NewProof(ecc.BN254)
		if _, err := proof.ReadFrom(bytes.NewReader(vote.Output.ProofBytes)); err != nil {
			fmt.Printf("   %s: REJECTED - invalid proof format\n", vote.VoterName)
			continue
		}

		// Build public witness
		publicAssignment := &circuit.VoteCircuit{
			MerkleRoot:  crypto.BytesToFieldElement(vote.Output.MerkleRoot),
			Nullifier:   crypto.BytesToFieldElement(vote.Output.Nullifier),
			ProposalID:  vote.Output.ProposalID,
			VoteOption:  uint64(vote.Output.VoteOption),
			VotingPower: vote.Output.VotingPower,
		}

		publicWitness, err := frontend.NewWitness(
			publicAssignment,
			ecc.BN254.ScalarField(),
			frontend.PublicOnly(),
		)
		if err != nil {
			fmt.Printf("   %s: REJECTED - witness error\n", vote.VoterName)
			continue
		}

		// Verify proof
		verifyStart := time.Now()
		err = groth16.Verify(proof, vk, publicWitness)
		verifyTime := time.Since(verifyStart)
		totalVerifyTime += verifyTime

		if err != nil {
			fmt.Printf("   %s: REJECTED - proof verification failed\n", vote.VoterName)
			continue
		}

		// Mark as verified and record nullifier
		vote.Verified = true
		vote.VerifyTime = verifyTime
		nullifierSet[nullifierHex] = true

		fmt.Printf("   Vote verified (verify: %v)\n", verifyTime.Round(time.Microsecond))
	}

	fmt.Printf("   Total verification time: %v\n", totalVerifyTime.Round(time.Millisecond))
	fmt.Println()

	// Step 7: Tally the results
	fmt.Println("Step 7: Tallying results...")

	tally := struct {
		Yes     uint64
		No      uint64
		Abstain uint64
	}{}

	for _, vote := range votes {
		if !vote.Verified {
			continue
		}

		switch vote.Output.VoteOption {
		case 0:
			tally.Yes += vote.Output.VotingPower
		case 1:
			tally.No += vote.Output.VotingPower
		case 2:
			tally.Abstain += vote.Output.VotingPower
		}
	}

	totalVoted := tally.Yes + tally.No + tally.Abstain
	totalPower := uint64(0)
	for _, v := range voters {
		totalPower += v.VotingPower
	}

	fmt.Println()
	fmt.Printf("  Proposal: %s\n", proposal.Title)
	fmt.Printf("  Yes:     %6d votes (%5.1f%%)\n", tally.Yes, float64(tally.Yes)/float64(totalVoted)*100)
	fmt.Printf("  No:      %6d votes (%5.1f%%)\n", tally.No, float64(tally.No)/float64(totalVoted)*100)
	fmt.Printf("  Abstain: %6d votes (%5.1f%%)\n", tally.Abstain, float64(tally.Abstain)/float64(totalVoted)*100)
	fmt.Printf("  Total voted: %d / %d (%.1f%% participation)\n",
		totalVoted, totalPower, float64(totalVoted)/float64(totalPower)*100)

	// Determine outcome (simple majority of non-abstain votes)
	if tally.Yes > tally.No {
		fmt.Println("  RESULT: PASSED")
	} else if tally.No > tally.Yes {
		fmt.Println("  RESULT: REJECTED")
	} else {
		fmt.Println("  RESULT: TIE")
	}

	fmt.Println()
	fmt.Println("Privacy Analysis:")
	fmt.Println("   What is PUBLIC (visible on-chain):")
	fmt.Println("   - The vote options and voting power for each vote")
	fmt.Println("   - The nullifiers (but these cannot be linked to identities)")
	fmt.Println("   - The running and final tallies")
	fmt.Println()
	fmt.Println("   What is PRIVATE (never revealed):")
	fmt.Println("   - Which voter cast which vote")
	fmt.Println("   - Voter secret keys")
	fmt.Println("   - The link between nullifiers and voter identities")
	fmt.Println()
	fmt.Println("   Even though we printed voter names in this demo,")
	fmt.Println("   on a real blockchain the votes would be completely unlinkable!")
	fmt.Println()
}

// createVoters generates test voters with random keys
func createVoters() []*Voter {
	voterData := []struct {
		name  string
		power uint64
	}{
		{"Alice", 1000},
		{"Bob", 500},
		{"Charlie", 2000},
		{"Diana", 750},
		{"Eve", 1500},
	}

	voters := make([]*Voter, len(voterData))
	for i, data := range voterData {
		entropy := make([]byte, 32)
		if _, err := rand.Read(entropy); err != nil {
			fatal("Failed to generate entropy: %v", err)
		}

		keys, err := crypto.GenerateVoterKeys(entropy)
		if err != nil {
			fatal("Failed to generate keys: %v", err)
		}

		voters[i] = &Voter{
			Name:        data.name,
			SecretKey:   keys.SecretKey,
			PublicKey:   keys.PublicKey,
			VotingPower: data.power,
		}
	}

	return voters
}

func fatal(format string, args ...interface{}) {
	fmt.Printf("\nError: "+format+"\n", args...)
	panic("demo failed")
}
