# ZK Private Voting - Phase 1: Core Circuit Implementation

This package implements the core ZK-SNARK circuit for anonymous voting using gnark.

## Overview

The voting circuit proves that a voter:
1. **Is eligible** - Their public key exists in the voter Merkle tree
2. **Has correct voting power** - The claimed power matches the tree
3. **Hasn't double-voted** - The nullifier is correctly computed
4. **Cast a valid vote** - The vote option is 0, 1, or 2

All of this is proven **without revealing the voter's identity**.

## Project Structure

```
zkprivatevoting/
├── circuit/
│   ├── vote_circuit.go      # ZK circuit definition
│   └── vote_circuit_test.go # Circuit tests
├── crypto/
│   ├── crypto.go            # Merkle tree, hashing, key management
│   └── crypto_test.go       # Crypto tests
├── prover/
│   └── prover.go            # Client-side proof generation
├── cmd/
│   ├── setup/
│   │   └── main.go          # Trusted setup tool
│   └── demo/
│       └── main.go          # Full demo application
└── go.mod
```

## Quick Start

### 1. Install dependencies

```bash
go mod tidy
```

### 2. Run tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run benchmarks
go test -bench=. ./circuit
```

### 3. Run the demo

```bash
go run ./cmd/demo
```

This will:
- Create 5 mock voters with different voting power
- Build a voter eligibility Merkle tree
- Run the trusted setup
- Have each voter generate a proof for their vote
- Verify all proofs
- Tally the results

### 4. Generate production keys

```bash
go run ./cmd/setup
```

This creates:
- `keys/proving_key.bin` - Distribute to voters (~50-100 MB)
- `keys/verifying_key.bin` - Embed in your chain (~1-2 KB)
- `keys/circuit.r1cs` - Compiled circuit (optional)

## Circuit Details

### Public Inputs (visible on-chain)

| Input | Type | Description |
|-------|------|-------------|
| `MerkleRoot` | field | Root of the voter eligibility tree |
| `Nullifier` | field | Prevents double-voting |
| `ProposalID` | uint64 | Which proposal is being voted on |
| `VoteOption` | uint64 | 0=yes, 1=no, 2=abstain |
| `VotingPower` | uint64 | How much voting power this vote carries |

### Private Inputs (never revealed)

| Input | Type | Description |
|-------|------|-------------|
| `SecretKey` | field | Voter's secret key |
| `PathElements` | [20]field | Merkle proof siblings |
| `PathIndices` | [20]uint64 | Merkle proof positions |

### Constraints

The circuit enforces:

1. **Public key derivation**: `publicKey = hash(secretKey)`
2. **Leaf computation**: `leaf = hash(publicKey, votingPower)`
3. **Merkle proof**: Computed root equals `MerkleRoot`
4. **Nullifier**: `nullifier = hash(secretKey, proposalID)`
5. **Valid vote**: `voteOption ∈ {0, 1, 2}`
6. **Binary indices**: Each `pathIndex ∈ {0, 1}`

### Performance

| Metric | Value |
|--------|-------|
| Constraints | ~14,000 |
| Proof generation | ~2-5 seconds |
| Proof verification | ~2-5 milliseconds |
| Proof size | ~200 bytes |

## Usage in Your Chain

### 1. Generate keys once

```bash
go run ./cmd/setup -output ./keys
```

### 2. Embed verifying key in genesis

```go
// In your genesis.json or module params
{
  "privatevoting": {
    "verifying_key": "<hex from keys/verifying_key.hex>"
  }
}
```

### 3. Client generates proof

```go
import "zkprivatevoting/prover"

// Load proving key
p, _ := prover.NewVoteProver("proving_key.bin", "circuit.r1cs")

// Generate proof
output, _ := p.GenerateProof(&prover.VoteProofInput{
    SecretKey:   voterSecretKey,
    VotingPower: 1000,
    ProposalID:  1,
    VoteOption:  0, // Yes
    MerkleRoot:  proposalMerkleRoot,
    MerkleProof: merkleProof,
})

// Submit to chain
tx := MsgVote{
    ProposalID:  output.ProposalID,
    VoteOption:  output.VoteOption,
    VotingPower: output.VotingPower,
    Nullifier:   output.Nullifier,
    Proof:       output.ProofBytes,
}
```

### 4. Chain verifies proof

```go
import "zkprivatevoting/prover"

// Load verifying key (from params/genesis)
v, _ := prover.NewVoteVerifierFromBytes(params.VerifyingKey)

// Verify proof
err := v.Verify(output)
if err != nil {
    return ErrInvalidProof
}

// Check nullifier not already used
if hasNullifier(proposalID, output.Nullifier) {
    return ErrDoubleVote
}

// Record nullifier and update tally
setNullifier(proposalID, output.Nullifier)
updateTally(proposalID, output.VoteOption, output.VotingPower)
```

## Security Considerations

### Trusted Setup

This implementation uses Groth16, which requires a trusted setup. The setup generates "toxic waste" that must be destroyed - anyone with this data could forge proofs.

**For production:**
1. Run an MPC ceremony with multiple participants
2. Or switch to PLONK (universal setup, no per-circuit ceremony)

### Hash Function

We use MiMC, a SNARK-friendly hash function. While efficient, it has less cryptanalysis than SHA-256. For most voting applications, this is acceptable.

### Tree Depth

The default tree depth of 20 supports ~1 million voters. Adjust `TreeDepth` in `circuit/vote_circuit.go` if you need more or fewer.

## Testing

```bash
# Unit tests
go test ./crypto -v
go test ./circuit -v

# Test with coverage
go test ./... -cover

# Benchmarks
go test -bench=. -benchtime=10s ./circuit
go test -bench=. -benchtime=10s ./crypto
```

## Next Steps (Phase 2)

After Phase 1 is complete:

1. **Cosmos SDK Module**: Implement keeper, msg_server, queries
2. **CLI**: Add commands for registration, voting, queries
3. **Genesis**: Handle verifying key in genesis/params
4. **Testing**: Integration tests with the full module

## License

Apache 2.0 (same as gnark)
