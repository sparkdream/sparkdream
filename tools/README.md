# Spark Dream Client Tools

Client-side cryptographic tooling for the Spark Dream blockchain. These tools are used offline by clients to generate ZK proofs for shielded operations. The on-chain verification, DKG ceremony, and TLE decryption all live in `x/shield` and run automatically via ABCI++ vote extensions.

## Directory Structure

```
tools/
├── crypto/               # Shared cryptographic primitives
│   ├── crypto.go         # MiMC hash, Merkle trees, key management, nullifiers
│   └── crypto_test.go
├── zk/                   # Zero-knowledge proof tooling (Groth16/BN254)
│   ├── circuit/
│   │   ├── shield_circuit.go      # Unified shield circuit (~14k constraints)
│   │   └── shield_circuit_test.go
│   ├── prover/
│   │   └── prover.go              # Client-side proof generation & verification
│   └── cmd/
│       ├── setup/
│           └── main.go            # Trusted setup key generation
│       └── demo/
│           └── main.go            # Full e2e shielded action demo
└── README.md
```

## Quick Start

### Run tests

```bash
# All tests
go test ./tools/...

# ZK circuits
go test -v ./tools/zk/circuit/

# Crypto primitives
go test -v ./tools/crypto/

# Benchmarks
go test -bench=. ./tools/zk/circuit/
go test -bench=. ./tools/crypto/
```

### Run the shielded action demo

```bash
go run ./tools/zk/cmd/demo
```

Creates mock members at different trust levels, builds a trust tree, runs trusted setup, generates and verifies anonymous action proofs, and demonstrates rate limiting.

### Generate ZK proving/verifying keys

```bash
go run ./tools/zk/cmd/setup -output ./keys
```

Creates:
- `keys/proving_key.bin` — Distribute to members (~50-100 MB)
- `keys/verifying_key.bin` — Store on-chain as circuit_id="shield_v1" (~1-2 KB)
- `keys/circuit.r1cs` — Compiled circuit (optional, speeds up proving)

## Shield Circuit

The unified `ShieldCircuit` handles all anonymous operations — voting, posting, reacting, challenges, etc. It proves that a member:

1. Is in the trust tree (Merkle proof)
2. Meets the minimum trust level (without revealing exact level)
3. Has computed a valid scoped nullifier (prevents double actions)
4. Has computed a valid rate-limit nullifier (enables per-identity rate limiting)

**Public inputs** (visible on-chain): MerkleRoot, Nullifier, RateLimitNullifier, MinTrustLevel, Scope, RateLimitEpoch

**Private inputs** (never revealed): SecretKey, TrustLevel, PathElements[20], PathIndices[20]

**Constraints**: ~14,000 | **Proof generation**: ~2-5s | **Verification**: ~2-5ms | **Proof size**: ~200 bytes

### Nullifier Design

- **Action nullifier**: `H(secretKey, scope)` — prevents duplicate actions per scope (epoch, postID, etc.)
- **Rate-limit nullifier**: `H(secretKey, domainTag, epoch)` — same for ALL operations by the same member in the same epoch. Enables per-identity rate limiting without breaking anonymity. The domain tag (`MaxUint64`) prevents collision with action nullifiers.

## Crypto Primitives

- **MiMC hash**: SNARK-friendly hash function (BN254 field)
- **Merkle tree**: Sparse tree with depth 20 (~1M leaves), MiMC-based
- **Key derivation**: `publicKey = MiMC(secretKey)`
- **Nullifier computation**: `nullifier = MiMC(secretKey, scope)`
- **Rate-limit nullifier**: `rateLimitNullifier = MiMC(secretKey, domainTag, epoch)`
- **Leaf format**: `MiMC(publicKey, trustLevel)`

## Usage with x/shield

```go
import (
    "sparkdream/tools/crypto"
    "sparkdream/tools/zk/prover"
)

// Client: generate proof for shielded execution
p, _ := prover.NewShieldProver("proving_key.bin", "circuit.r1cs")
output, _ := p.GenerateProof(&prover.ShieldProofInput{
    SecretKey:      secretKey,
    TrustLevel:     2,      // ESTABLISHED
    MinTrustLevel:  1,      // Require at least PROVISIONAL
    Scope:          epoch,  // Or postID, proposalID, etc.
    RateLimitEpoch: epoch,
    MerkleRoot:     root,
    MerkleProof:    proof,
})

// Chain (x/shield): verify proof
v, _ := prover.NewShieldVerifierFromBytes(params.VerifyingKey)
err := v.Verify(output)
```

## Security Notes

- **Groth16 trusted setup**: Requires MPC ceremony for production. Consider PLONK for universal setup.
- **MiMC**: SNARK-friendly but less cryptanalyzed than SHA-256. Acceptable for most applications.
- **Tree depth 20**: Supports ~1M members. Adjust `TreeDepth` in circuit files if needed.
- **BN256 pairing**: Used by x/shield for TLE. Provides ~128-bit security.

## License

Apache 2.0
