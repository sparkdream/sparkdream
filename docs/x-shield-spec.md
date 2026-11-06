# x/shield Module Specification

## Overview

The `x/shield` module provides **unified shielded execution** for anonymous on-chain operations. Instead of each module implementing its own anonymous message types, ZK verification, and nullifier management, x/shield provides a single entry point: `MsgShieldedExec`.

**Core guarantees:**

1. **Gasless submission**: The shield module account pays transaction fees — submitters need zero balance
2. **Universal anonymity**: Any registered shielded operation across any module goes through one message type
3. **Proof-gated access**: Every shielded execution requires a valid ZK proof of trust tree membership
4. **Replay prevention**: Centralized nullifier management with per-domain scoping
5. **Rate-limited funding**: Community pool auto-funding with governance-controlled caps

### Why x/shield?

The current architecture has each module independently implementing anonymous operations:

| Module | Anonymous Messages | Problems |
|--------|-------------------|----------|
| x/blog | `MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, `MsgAnonymousReact` | Submitter needs gas funds (funding trail) |
| x/commons | `MsgSubmitAnonymousProposal`; secret-ballot votes `MsgAnonymousVoteProposal` and `MsgAnonymousVotePoll`, proven against the ballot's voter snapshot (`PROOF_DOMAIN_GROUP_TREE`, see [x-commons-spec.md](x-commons-spec.md) §11.2) | Submitter needs gas funds |
| x/rep | `MsgCreateChallenge` (with `is_anonymous` flag) | Submitter needs SPARK for gas |
| x/forum | `MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, `MsgAnonymousReact` | Submitter needs gas funds |
| x/collect | `MsgCreateAnonymousCollection`, `MsgAnonymousReact` | Submitter needs gas funds |

Every anonymous operation requires the submitter to have funds for gas. This creates a metadata trail: an observer can correlate funding transactions to anonymous submissions. Relayer subsidy (x/blog) partially addresses this but introduces a trusted intermediary who learns timing metadata.

x/shield eliminates all of this. The submitter address is meaningless - by default every anonymous client signs with the same shared public submitter (see [Public Anonymous Submitter](#public-anonymous-submitter)), which holds no balance. The module pays gas. No funding trail, no relayer, no correlation.

## Privacy Model

x/shield provides two execution modes with increasing privacy guarantees:

### Mode 1: Immediate Shielded (Default)

```
Privacy:
├── Identity:       Hidden (ZK proof, module-paid gas, no funding trail)
├── Timing:         Exposed (operation executes in the block it's submitted)
├── Content:        Exposed (inner message visible in mempool and on-chain)
└── Anonymity set:  All members meeting the trust level requirement
```

Content is visible but cannot be swapped: the proof is bound to the exact inner message (`MessageHash`), so a proof seen in the mempool cannot be resubmitted with different content to spend the member's nullifier first.

Operations execute on receipt after ZK proof verification. Best for latency-sensitive operations where content isn't secret (blog posts, reactions, replies). This is the behavior described in the original architecture section below.

### Mode 2: Encrypted Batch (Maximum Privacy)

```
Privacy:
├── Identity:       Hidden (ZK proof, module-paid gas, no funding trail)
├── Timing:         Hidden within batch window (all ops in a batch execute together)
├── Content:        Hidden until batch (TLE-encrypted in mempool and on-chain)
├── Execution order: Shuffled (deterministic but unpredictable within batch)
└── Anonymity set:  All operations in the same batch × all eligible members
```

Operations are threshold-encrypted and queued. They are only decrypted and executed when the batch epoch's decryption key becomes available. Provides maximum privacy at the cost of latency (up to one shield epoch). Best for governance operations (votes, challenges) where privacy outweighs speed.

### What's Always Public (Both Modes)

```
├── That a shielded execution occurred
├── A nullifier: the operation nullifier in immediate mode (replay prevention),
│   the submission nullifier in encrypted batch mode (one per payload; the
│   operation nullifier is sealed) — both unlinkable to identity by design
├── The rate-limit nullifier (same for all trust-tree ops by same member in same epoch — for rate limiting;
│   a group-tree vote's is unique to the member and the ballot, see Rate-limit linkability)
├── The Merkle root (same for ALL members in a tree snapshot)
├── The proof domain (trust tree)
├── The minimum trust level being proven
└── The message type URL (in immediate mode) or target epoch (in encrypted batch mode)
```

Nullifiers are domain-separated (the proof's Scope is `ScopeElement(nullifier_domain, raw_scope)`), so one member's nullifiers never repeat across operations. In ownership mode the public nullifier is the content's owner tag, shared by every management op on that content (see [Nullifier Modes](#nullifier-modes)).

### What's Always Hidden (Both Modes)

```
├── Link between submitter address and anonymous identity
├── Which member performed the operation
├── Member's position in the Merkle tree
├── Member's actual trust level (only lower bound proven)
└── The Merkle proof path
```

### What's Hidden Only in Encrypted Batch Mode

```
├── Inner message content (encrypted until batch execution)
├── ZK proof bytes (encrypted — prevents proof analysis)
├── Timing within the batch epoch (all ops in a batch execute simultaneously)
└── Execution order (shuffled unpredictably within batch)
```

### Privacy Comparison

| Threat | Immediate | Encrypted Batch |
|--------|-----------|-----------------|
| Identity correlation via address | Defeated | Defeated |
| Identity correlation via gas funding | Defeated | Defeated |
| Timing correlation (submission → operation) | **Vulnerable** | Defeated for the operation (the operation nullifier is sealed; the public submission nullifier is unrelated to it); the submission tx itself remains visible |
| Mempool content analysis | **Vulnerable** | Defeated |
| Validator content snooping before inclusion | **Vulnerable** | Defeated |
| Network-level P2P observation (IP → encrypted blob) | **Vulnerable** | Partially defeated (content hidden, IP still visible) |
| Batch size = 1 timing analysis | N/A | **Vulnerable** (mitigated by `min_batch_size`) |
| Colluding validators (≥ 2/3) | N/A | **Vulnerable** (same trust assumption as consensus) |

## Architecture

### Immediate Mode Flow

```
User (offline)                    Chain
─────────────                     ─────
1. Construct inner message
   (e.g. MsgCreatePost with
   creator = shield module addr)

2. Generate ZK proof
   (same circuits as today)

3. Wrap in MsgShieldedExec
   exec_mode = IMMEDIATE
   submitter = public anonymous submitter
   (signed as an unordered tx)

4. Broadcast ──────────────────►  5. ShieldGasDecorator (ante handler)
                                     - Detects MsgShieldedExec
                                     - Prechecks proof, nullifier, identity
                                       rate limit (PrecheckExec)
                                     - Caps fee at max_fee_per_exec
                                     - Deducts gas from shield module account
                                     - Skips normal fee deduction for submitter

                                  6. msg_server.ShieldedExec()
                                     - Validates inner message type is registered
                                     - Looks up proof requirements for that message type
                                     - Verifies ZK proof (trust tree membership,
                                       bound to the inner message); skipped if
                                       the ante handler already verified it
                                     - Checks nullifier not used (domain + scope)
                                     - Records nullifier
                                       (ownership mode instead: nullifier must
                                       equal the target's owner tag; nothing is
                                       recorded and the owner sequence advances)
                                     - Dispatches inner message via router with
                                       the proven trust level in context (plus
                                       the exec nullifier or the owner tag)

                                  7. Target module executes normally
                                     - Sees shield module account as sender
                                     - Applies its own trust gates against
                                       types.ProvenTrustLevel(ctx)
```

### Encrypted Batch Mode Flow

```
User (offline)                    Chain
─────────────                     ─────
1. Construct inner message
   + generate the operation proof
   (nullifier, rate-limit nullifier)

2. Read TLE master public key
   from chain state

3. Encrypt (inner_message + operation
   proof + operation nullifier) with
   the TLE master key via IBE
   → encrypted_payload

4. Generate the submission proof:
   same member, same epoch, bound to
   encrypted_payload (Scope from the
   payload hash in the batch-submission
   domain, MessageHash over the payload)

5. Wrap in MsgShieldedExec
   exec_mode = ENCRYPTED_BATCH
   proof = submission proof
   nullifier = submission nullifier
   rate_limit_nullifier, merkle_root
   encrypted_payload = ciphertext
   target_epoch = current shield epoch

6. Broadcast ──────────────────►  7. ShieldGasDecorator (ante handler)
                                     - PrecheckExec: every msg-server check
                                       plus the submission proof, before
                                       any fee moves
                                     - Deducts gas from shield module account

                                  8. msg_server.ShieldedExec()
                                     - Validates merkle root, target epoch,
                                       payload size, queue headroom
                                     - Submission nullifier not pending
                                     - Per-identity rate limit (the proven
                                       rate-limit nullifier)
                                     - Verifies the submission proof (unless
                                       the ante handler already did)
                                     - Records the submission nullifier
                                     - Stores PendingShieldedOp in queue
                                     - The operation proof stays sealed

                              ┌──── Operations accumulate in queue... ────┐

                                  9. Epoch closes; once its batch is ready
                                     the EndBlocker records a DecryptionDemand
                                     Validators embed decryption shares in
                                     their votes (from x/shield DKG key shares)

                                 10. PreBlocker: threshold met
                                     - Reconstruct epoch decryption key
                                     EndBlocker (same block, any block):
                                     - Decrypt all pending ops for this epoch
                                     - Verify ZK proofs (now readable)
                                     - Shuffle execution order (block hash seed)
                                     - Execute valid inner messages
                                     - Drop invalid ops (bad proof, decode error)
                                     - Emit batch execution event
```

### The Submission Proof

An encrypted-batch submission carries a cleartext membership proof of its own, distinct from the operation proof sealed in the ciphertext. It is an ordinary `ShieldCircuit` proof whose public inputs are (`tools/tle/submission.go`, mirrored by `types.BatchSubmission*`):

```
Scope              = ScopeElement(BatchSubmissionDomain, BatchSubmissionScope(encrypted_payload))
Nullifier          = MiMC(secret, Scope)                        // the submission nullifier
RateLimitNullifier = H(secret, rate-limit tag, current epoch)   // the member's quota key
MessageHash        = ShieldMessageHash(BatchSubmissionTypeURL, encrypted_payload)
MerkleRoot, MinTrustLevel, RateLimitEpoch as for an immediate exec
```

where `BatchSubmissionDomain = 0xFFFFFFFF` (outside every registration's domain), `BatchSubmissionScope` is the first eight bytes of `sha256(payload)`, and `BatchSubmissionTypeURL = "/sparkdream.shield.v1.EncryptedBatchSubmission"` (a pseudo type URL that only domain-separates the hash). What it buys:

1. **The module pays only for proven members under their quota.** The rate-limit nullifier is verified at submission, exactly as in immediate mode, so the identity rate limit cannot be dodged by inventing one or burned by reusing another member's.
2. **No squatting.** The submission nullifier is unique to the prover and the payload: nobody can race a member's submission with the same nullifier, and a copied payload fails the proof.
3. **The ciphertext is bound.** A payload cannot be swapped under a submission proof.
4. **No linkability.** The submission nullifier lives in its own domain and scope; it is unrelated to the operation nullifier sealed in the payload, so nothing public at submission links the submission to the executed operation. The operation nullifier is checked against `UsedNullifiers` only at execution.

The pending-queue dedup key is the submission nullifier; it is released when the op executes or expires. A replay of the exact same submission in a later epoch fails the proof (`RateLimitEpoch`); a resubmission of the same *operation* under a fresh submission queues, and is dropped at execution because its operation nullifier is already used.

## ZK Proof Verification (Owned by x/shield)

x/shield owns all ZK proof verification infrastructure. No other module verifies ZK proofs — they all route through `MsgShieldedExec`. This centralization ensures consistent proof handling, nullifier management, and gas accounting.

### Proof Domain

x/shield uses a single unified proof domain:

#### TRUST_TREE

Proves membership in the x/rep trust tree with a minimum trust level. Used for **all** anonymous operations — content (blog posts, forum posts, reactions, collections) and governance (anonymous proposals, votes, challenges).

**Circuit**: `ShieldCircuit` (Groth16 over BN254)
**Public inputs**: MerkleRoot, Nullifier, RateLimitNullifier, MinTrustLevel, Scope, RateLimitEpoch, MessageHash
**Proves**: (1) member exists in trust tree, (2) trust level >= minimum, (3) valid scoped nullifier, (4) valid rate-limit nullifier for this epoch, (5) the proof is for one specific inner message
**Tree source**: x/rep keeper provides the Merkle tree snapshots (leaves = `MiMC(zk_public_key, trust_level)`)

The **Scope** public input is the domain-separated scope element, never the raw scope: `Scope = crypto.ScopeElement(nullifier_domain, raw_scope) = MiMC(be(nullifier_domain), be(raw_scope))`, where `raw_scope` is what `resolveNullifierScope` returns (epoch, message field value, or 0 for GLOBAL). The circuit computes `Nullifier = MiMC(secret_key, Scope)` and hashes nothing else, so with a raw scope one member's nullifiers would match across every operation sharing it (blog post 5, forum post 5 and commons proposal 5; every GLOBAL op) and link that member across modules and epochs. Hashing the domain into the scope element gives each (domain, raw scope) pair its own nullifier: `crypto.ComputeScopedNullifier(secret_key, domain, raw_scope)`. The domain separation needs no circuit change (Scope was already a field element). Chain storage still keys used nullifiers by `(domain, raw_scope, nullifier_hex)`.

The **RateLimitNullifier** is derived as `H(member_secret, RateLimitDomainTag, epoch)` where `RateLimitDomainTag = MaxUint64`. It is the same for all operations by the same member in the same epoch, enabling per-identity rate limiting without revealing which member it is. The circuit proves this derivation is correct.

The **MessageHash** is `crypto.ShieldMessageHash(type_url, value)` over the inner `Any` exactly as submitted (no re-encoding on either side): `sha256("sparkdream/shield/message/v1" || be32(len(type_url)) || type_url || value)` with the top three bits cleared so it is a canonical BN254 scalar. The verifier computes it from the tx, so a proof only verifies alongside the inner message it was made for - a proof copied out of a pending exec cannot be reused with different content. The circuit squares it (`api.Mul(MessageHash, MessageHash)`): a public input that appears in no constraint gets a zero verifying-key term, so any value would verify.

For a `NULLIFIER_MODE_OWNERSHIP` operation (see [Nullifier Modes](#nullifier-modes)) the MessageHash is `crypto.ShieldOwnedMessageHash(type_url, value, sequence)` instead: `sha256("sparkdream/shield/owned-message/v1" || be32(len(type_url)) || type_url || value || be64(sequence))`, top three bits cleared, where `sequence` is the owner sequence the target module reports for the content. The separate domain string keeps the two hash families disjoint.

**Group trees.** The circuit does not care which tree it proves against. `PROOF_DOMAIN_GROUP_TREE` (2) proves membership of a tree the inner message's module keeps instead of the trust tree, with the same circuit and verifying key: leaves `MiMC(zk_public_key, 0)` and `min_trust_level = 0`. x/commons uses it for secret ballots (a snapshot of a group's members per proposal or poll). Only the root check differs (see [Merkle Root Validation](#merkle-root-validation)).

### Merkle Root Validation

The trust tree domain accepts the **current or previous** tree root. This allows proofs generated slightly before a tree update to remain valid.

The group tree domain accepts exactly the root the inner message's module names: shield decodes the inner message, finds its `ShieldAware` module and asks its `ShieldRootResolver.ShieldMerkleRoot(ctx, msg)`; a module without a resolver, a message without a root (no such ballot, or voting on it is over) or any other root is refused. A group-tree exec must therefore carry its inner message in the clear: a batch submission is checked before its payload is decrypted, so group-tree operations are registered `IMMEDIATE_ONLY` and a group-tree batch submission is refused. Batch execution also refuses an operation whose registered proof domain differs from the domain the submission was proven in.

**Encrypted batch mode note**: Merkle roots are validated at submission time, not at execution time. If a batch op carries over multiple epochs (due to small batch or TLE failure), the root may be stale by execution time. A member removed from the tree after submission but before execution would still have their proof verify against the stored root. This is "valid when submitted" semantics — consistent with how traditional systems honor transactions valid at submission time. The `max_pending_epochs` parameter bounds how stale a root can be (default: 6 epochs ≈ 30 min). The same rule applies to the epoch: the prover binds `RateLimitEpoch` (and any EPOCH-scoped nullifier scope) to the epoch the op is submitted in (`target_epoch`, stored as `submitted_at_epoch`), and the EndBlocker verifies against that stored epoch, never the current one, since the epoch has always advanced by the time a batch runs.

### Verification Keys (SRS)

x/shield stores the verification key for the unified `ShieldCircuit`:

```protobuf
// Stored in x/shield state, set at genesis or via governance upgrade
message VerificationKey {
  string circuit_id = 1;        // "shield_v1" (unified circuit for all domains)
  bytes  vk_bytes = 2;          // serialized Groth16 verification key (BN254)
  string description = 3;
}
```

The verification key is set at genesis and can only be updated via chain upgrade (not governance parameter changes) to prevent malicious key substitution. When no verification key is stored, production builds (mainnet/testnet/devnet) reject every exec with `ErrNoVerificationKey`; only `testparams` builds skip proof verification, which allows E2E testing without a trusted setup ceremony.

Two guards keep that test-mode skip off the real networks:

- **Launch gate.** In production builds `InitGenesis` fails when `params.enabled` is true and no `shield_v1` key is in genesis: no message can add one later, so such a chain could never run a shielded exec. Mainnet genesis therefore ships with `enabled: false` until its ceremony key is pasted in; flip both together. `TestShieldEnabledHasVerificationKey` (`deploy/config/network/crossnetwork`) catches the mismatch before chain start.
- **Network build guard.** `app.New` refuses to start on a canonical chain-id (`sparkdream-1`, `sparkdream-test-1`, `sparkdream-dev-1`) unless the binary carries that network's build tag (`app/network_build.go`). An untagged build would skip verification, and a build for another network would compute different compiled values; both would fork off. Other chain-ids (local chains, sister chains) are unaffected.

**Sister chains and other chain-ids.** The network build guard only covers the canonical chain-ids. A sister chain (or any network on a non-canonical chain-id) must be built with a network tag (`-tags mainnet|testnet|devnet`), never with plain `go build` / `ignite chain build`: an untagged build is the test build, which skips proof verification whenever no key is stored and accepts any merkle root when the rep keeper is not wired. Put this in the release checklist of every sister chain. As a backstop, the daemon refuses to start an untagged build on any chain-id other than the local `sparkdream` chain unless `SPARKDREAM_ALLOW_TEST_BUILD=1` is set (`app.CheckTestBuildStart`). In network builds an unwired rep keeper fails closed (`ErrInvalidMerkleRoot`) instead.

One ceremony serves every network and sister chain: the key depends on the circuit, not the chain. Check each network's key with `ceremony verify -genesis <genesis.json>` against the published transcript.

### Proof Verification Flow

```go
// proofBinding is what the chain binds a proof's Scope and MessageHash
// public inputs to.
type proofBinding struct {
    scope       []byte // crypto.ScopeElement(domain, rawScope)
    messageHash []byte // ShieldMessageHash, or ShieldOwnedMessageHash in ownership mode
}

// Consume mode (immediate and batch): the registration's domain and the
// resolved raw scope.
func consumeBinding(domain uint32, rawScope uint64, msg *types.MsgShieldedExec) proofBinding {
    return proofBinding{scope: zkcrypto.ScopeElement(domain, rawScope), messageHash: messageHash(msg)}
}

// Ownership mode: the owner claim's domain and scope, and a message hash that
// also covers the owner's current sequence.
func ownedBinding(claim types.OwnershipClaim, msg *types.MsgShieldedExec) proofBinding {
    return proofBinding{
        scope:       zkcrypto.ScopeElement(claim.Domain, claim.Scope),
        messageHash: zkcrypto.ShieldOwnedMessageHash(msg.InnerMessage.TypeUrl, msg.InnerMessage.Value, claim.Sequence),
    }
}

func (k Keeper) verifyProof(ctx context.Context, msg *types.MsgShieldedExec, binding proofBinding) error {
    // 1. Look up verification key — unified circuit for all domains.
    // No VK: production builds refuse; testparams builds skip ALL
    // verification, including merkle root validation.
    const circuitID = "shield_v1"
    storedVK, found := k.GetVerificationKeyVal(ctx, circuitID)
    if !found || len(storedVK.VkBytes) == 0 {
        if requireVerificationKey() {
            return types.ErrNoVerificationKey
        }
        return nil
    }

    // 2. Validate merkle root is current or previous
    if err := k.validateMerkleRoot(ctx, msg.MerkleRoot, msg.ProofDomain); err != nil {
        return err
    }

    // 3. Deserialize the Groth16 verification key
    vk := groth16.NewVerifyingKey(ecc.BN254)
    if _, err := vk.ReadFrom(bytes.NewReader(storedVK.VkBytes)); err != nil {
        return types.ErrInvalidProof
    }

    // 4. Deserialize and verify the proof against the unified ShieldCircuit
    // Public inputs: MerkleRoot, Nullifier, RateLimitNullifier, MinTrustLevel,
    // Scope (= binding.scope, the domain-separated scope element), RateLimitEpoch,
    // MessageHash (= binding.messageHash)
    // RateLimitEpoch: the current epoch, or for PROOF_DOMAIN_GROUP_TREE the
    // epoch bound to the scope (rateLimitEpochInput, see below)
    epoch := k.GetCurrentEpoch(ctx)
    return k.verifyShieldProof(vk, proof, msg, binding,
        rateLimitEpochInput(msg.ProofDomain, binding.scope, epoch))
}
```

The ZK circuit proves that `RateLimitNullifier = H(member_secret, RateLimitDomainTag, epoch)`. Since it's a public input verified by the circuit, it cannot be forged where the proof is verified, which in both modes is the ante handler, before anything is paid: the operation proof for an immediate exec, the cleartext [submission proof](#the-submission-proof) for an encrypted-batch exec (whose sealed operation proof must then reproduce the same rate-limit nullifier at execution). Different members produce different rate-limit nullifiers, but the same member always produces the same one for a given epoch — enabling true per-identity rate limiting.

**Rate-limit linkability.** The rate-limit nullifier depends only on the secret key and the `RateLimitEpoch` input, never on the operation, so every trust-tree exec one member makes in one shield epoch carries the same 32-byte value in cleartext. For trust-tree traffic (anonymity set: the whole membership) that is accepted: it links a member's anonymous actions to each other within one epoch (about five minutes), not to the member. Secret ballots (`PROOF_DOMAIN_GROUP_TREE`, x/commons §11.2/§11.4) prove against a council-sized voter tree with the same secret key, so the same value would link a member's votes on two ballots, and a vote to the member's anonymous posts in the same epoch (narrowing the post's author to one council). The verifier therefore supplies a different `RateLimitEpoch` for group-tree execs: `MiMC(ScopeElement(domain, scope), epoch)` (`crypto.GroupTreeRateLimitEpoch`; prover `GroupTree: true`, wasm `"group_tree": true`). The circuit is unchanged (`RateLimitEpoch` was always a verifier-supplied public input), so no new ceremony is needed. A vote's rate-limit nullifier is then unique to the member and the ballot, like its action nullifier, and links to nothing. A proof built with the plain epoch fails verification. Residual, not closed without a circuit change: trust-tree execs in one epoch stay linkable to each other (the rate limit needs one per-member value per epoch); and timing, fees, gas and the submitting account remain side channels a client must manage (use the public submitter, and do not send a vote in the same block as one's other anonymous actions).

### Merkle Tree Snapshots

x/shield queries x/rep for trust tree snapshots but performs all proof verification internally:

```go
type RepKeeper interface {
    // Get the current trust tree Merkle root
    GetTrustTreeRoot(ctx context.Context) ([]byte, error)
    // Get the previous trust tree Merkle root (for tolerance)
    GetPreviousTrustTreeRoot(ctx context.Context) ([]byte, error)
}
```

Group-tree roots come from the target module through the `ShieldRootResolver` interface (implemented by x/commons), not from x/rep.

## State

### Params

```protobuf
message Params {
  // Whether shielded execution is enabled globally
  bool enabled = 1;

  // Maximum bond-denom amount to draw from the community pool per UTC day (day = block time / 86400)
  // Governance-controlled cap to prevent pool depletion
  // Independent of shield epoch interval — changing epoch cadence doesn't change the budget
  string max_funding_per_day = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Target minimum balance for the shield module account
  // BeginBlocker tops up to this level (up to max_funding_per_day)
  string min_gas_reserve = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Maximum gas allowed per shielded execution (prevents abuse via expensive inner messages)
  uint64 max_gas_per_exec = 4;

  // Per-identity rate limit: max shielded executions per epoch per nullifier-identity
  // Enforced via a separate rate-limit nullifier domain
  uint64 max_execs_per_identity_per_epoch = 5;

  // --- Encrypted Batch Mode params ---

  // Whether encrypted batch mode is enabled (requires TLE DKG ceremony completed)
  bool encrypted_batch_enabled = 6;

  // Shield epoch interval in blocks. All encrypted ops submitted during one shield epoch
  // are decrypted and executed together when the next epoch starts.
  // Shorter = less latency but smaller anonymity sets.
  uint64 shield_epoch_interval = 7;

  // Minimum number of operations in a batch before it executes.
  // If fewer ops are queued, they carry over to the next epoch (up to max_pending_epochs).
  // Prevents anonymity set = 1 (single operation trivially deanonymized by timing).
  uint32 min_batch_size = 8;

  // Maximum epochs an operation can remain pending before force-executing
  // (even in a small batch). Prevents indefinite delays.
  // After max_pending_epochs, the batch executes regardless of size.
  uint32 max_pending_epochs = 9;

  // Maximum number of pending operations in the queue.
  // Prevents unbounded state growth. When full, new encrypted submissions are rejected.
  uint32 max_pending_queue_size = 10;

  // Maximum size of encrypted_payload in bytes (prevents oversized spam)
  uint32 max_encrypted_payload_size = 11;

  // Maximum operations to execute in a single EndBlocker batch.
  // Bounds gas consumption per block. Remaining ops carry over to next block.
  uint32 max_ops_per_batch = 12;

  // --- TLE liveness enforcement params ---

  // Rolling window in shield epochs for tracking validator TLE misses
  uint64 tle_miss_window = 13;
  // Max misses within window before validator is jailed
  uint64 tle_miss_tolerance = 14;
  // Jail duration in seconds for TLE liveness violations
  int64  tle_jail_duration = 15;

  // --- DKG automation params ---

  // Minimum bonded validators required to auto-open a DKG ceremony
  uint32 min_tle_validators = 16;
  // Total DKG window in blocks, split into four equal phases: key registration, dealing, complaints, justifications
  uint64 dkg_window_blocks = 17;
  // Validator set drift percentage (0-100) that triggers automatic re-keying.
  // e.g. 33 means re-key once 33% or more of the DKG participants (truncated
  // to a whole percent) are no longer in the bonded set.
  uint32 max_validator_set_drift = 18;

  // Maximum fee (bond denom) the module pays for one MsgShieldedExec. The
  // submitter picks the fee but the module pays it; a higher fee, or any other
  // denom, is refused before anything is paid.
  string max_fee_per_exec = 19 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Whether a completed DKG turns encrypted_batch_enabled on by itself. Off
  // by default: governance enables batch mode explicitly once it is satisfied
  // with the ceremony. A re-key always turns batch mode off.
  bool enable_batch_after_dkg = 20;

  // Chain-wide budget (bond denom) of fees the module pays in one shield
  // epoch, summed over every exec. An exec whose fee would exceed what is
  // left is rejected before anything is paid (ErrEpochFeeBudgetExhausted);
  // the budget resets at the next shield epoch. Must be >= max_fee_per_exec.
  string max_fees_per_epoch = 21 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Whether the randomness beacon is on: once a DKG has completed, the
  // EndBlocker demands the TLE key of every epoch as it closes, batch or
  // no batch (see "Randomness beacon"). Default true.
  bool beacon_enabled = 22;
}
```

`max_fees_per_epoch` bounds what all identities together can spend: one identity may run `max_execs_per_identity_per_epoch` execs at up to `max_fee_per_exec` each (50 x 0.05 SPARK x 288 epochs = 720 SPARK a day at the defaults), far above `max_funding_per_day`. Size the budget so `max_fees_per_epoch x epochs per day <= max_funding_per_day` (0.65 SPARK x 288 = 187 SPARK at 6 s blocks and 50-block epochs; devnet's 25-block epochs use 0.325 SPARK). The reserve then never drains faster than it is funded. Two compiled shares divide the budget (`x/shield/types/params.go`) so neither one member nor trust-tree traffic can lock everyone else out, and secret-ballot governance cannot be starved:

- **Per-identity share** (`IdentityFeeShareDivisor = 10`): the fees paid for one identity's execs in an epoch, keyed by its rate-limit nullifier like the identity rate limit, may not pass `max(max_fees_per_epoch / 10, max_fee_per_exec)` (`ErrIdentityFeeShareExhausted`). Without it, 13 max-fee execs by one member (which need not even succeed: the ante books the fee before the inner message runs) took the whole mainnet budget every epoch, freezing every anonymous op including secret-ballot votes. Exhausting it now takes ten members acting together.
- **Group-tree reserve** (`GroupTreeFeeReserveDivisor = 5`): execs other than `PROOF_DOMAIN_GROUP_TREE` may take the epoch's paid fees only to `max_fees_per_epoch - max_fees_per_epoch / 5` (never below one `max_fee_per_exec`); group-tree execs (secret-ballot votes and polls) may use the whole budget. A group-tree exec's rate-limit nullifier is per ballot (see Rate-limit linkability), so its share is per member and ballot; one vote per ballot is all its nullifier allows anyway. The explicit bound: a member with `k` secret ballots and polls open in one epoch can spend up to `k x max_fee_per_exec` of it, not a tenth of the budget, and holds `k` separate `max_execs_per_identity_per_epoch` quotas. Only governance opens ballots (members cannot), each admits one vote per member, and a budget that is exhausted refuses the next vote for that epoch only, so this is bounded growth, not an attacker's lever. Keying the share by a member-level value instead would need a second public input that is stored but never emitted, which the circuit does not have; the per-ballot bound is accepted and documented instead.

Both are compiled rather than params: they decide how the one budget param is divided. The fees paid so far in the current epoch are the `EpochFeeLedger` singleton (exported in genesis as `epoch_fee_ledger`); the per-identity amounts are `IdentityEpochFees` (`(epoch, rate_limit_nullifier_hex) → paid`, exported as `identity_epoch_fees`, pruned with the rate limits). A refused fee is not booked.

**Refund of unused gas.** The ante charges the declared fee in full (it cannot know the gas yet), and a post handler (`FeeRefundDecorator`, `x/shield/ante/fee_refund.go`, chained after `app/relayrefund`'s in `app.go`) returns what the gas actually used did not cost: `fee - ceil(fee x gas_used / gas_limit)` per denom, at the price the submitter implied by its own fee and gas limit. The charged part rounds up, so the fee collector never gets less than gas used at that price; a tx that used all its gas gets nothing back. The refund moves from the fee collector back to the shield module account and `RefundEpochFee` unbooks it from the `EpochFeeLedger` and the identity's `IdentityEpochFees` entry (both saturating at zero, and only in the epoch the fee was charged in: a refund whose epoch is no longer current unbooks nothing). So the budget and the per-identity share measure the gas anonymous execs **used**, not the gas limits their submitters declared: a member who declares the 200k default limit for an exec that uses 60k is booked for 30% of the fee. The ante records what it charged (fee, gas limit, epoch, rate-limit nullifier) in an in-memory, per-block map keyed by tx hash (`FeeMarks`, the same shape as relayrefund's `Marks`: the ante chain's returned context does not reliably reach the post handler), only in finalize mode, so CheckTx and simulation neither mark nor refund. The refund's own writes run under an infinite gas meter (a tx that used nearly all its gas is not failed by its own refund), and a refund the fee collector cannot pay is logged and skipped: the exec stands and the module is out the unused part. Event `shield_fee_refund` (`amount`, `gas_used`, `gas_limit`).

A **failed exec is not refunded**: baseapp runs the post handler for a failed tx too, but in the message state it then discards, while the ante's fee payment and booking are already committed, so no refund could land. A failing exec therefore costs the reserve its declared fee, as it costs its member the nullifier and the rate-limit slot (see PrecheckExec below), bounded by `max_fee_per_exec` and the identity's share. Residual: a member can still spend its share faster by sending execs that fail, which is the same budget as before the refund and already what the share caps.

### Registered Operations

Each module registers which of its message types can be executed in shielded mode:

```protobuf
message ShieldedOpRegistration {
  // Full protobuf message type URL (e.g. "/sparkdream.blog.v1.MsgCreatePost")
  string message_type_url = 1;

  // Which proof domain is required
  ProofDomain proof_domain = 2;

  // Minimum trust level required (only for TRUST_TREE domain, 0 = any member)
  uint32 min_trust_level = 3;

  // Nullifier domain (scopes nullifiers to prevent cross-operation reuse)
  uint32 nullifier_domain = 4;

  // How the nullifier scope is determined
  NullifierScopeType nullifier_scope_type = 5;

  // Whether this operation is currently active
  bool active = 6;

  // Which execution modes this operation supports
  ShieldBatchMode batch_mode = 7;

  // Proto field name for scope extraction when nullifier_scope_type = MESSAGE_FIELD
  // e.g. "post_id", "proposal_id", "collection_id"
  // Must refer to a uint64 field in the inner message
  string scope_field_path = 8;

  // What the nullifier proves: a consumed one-time action (default) or
  // ownership of anonymously created content. OWNERSHIP ignores
  // nullifier_scope_type and scope_field_path (the target module supplies the
  // owner's scope) and requires batch_mode IMMEDIATE_ONLY.
  NullifierMode nullifier_mode = 9;

  // For NULLIFIER_SCOPE_EPOCH: the scope is current_epoch / epoch_window, so
  // each member gets one action per window of this many shield epochs instead
  // of one per epoch. 0 or 1 = every epoch. Must be 0 or 1 for other scope
  // types and for ownership ops.
  uint64 epoch_window = 10;
}

enum ProofDomain {
  PROOF_DOMAIN_UNSPECIFIED = 0;
  PROOF_DOMAIN_TRUST_TREE = 1;
  PROOF_DOMAIN_GROUP_TREE = 2;   // a module's own tree (x/commons secret ballots); immediate only
}

enum NullifierScopeType {
  // Scope = current epoch (one op per epoch)
  NULLIFIER_SCOPE_EPOCH = 0;
  // Scope = extracted from inner message (e.g. post_id for replies)
  NULLIFIER_SCOPE_MESSAGE_FIELD = 1;
  // No scoping (nullifier is globally unique)
  NULLIFIER_SCOPE_GLOBAL = 2;
}

enum NullifierMode {
  // The nullifier is consumed: each (member, scope) acts once.
  NULLIFIER_MODE_CONSUME = 0;
  // The nullifier proves ownership of anonymously created content and is
  // reused: the target module's ShieldOwnershipResolver supplies the owner's
  // domain, scope and tag (the creation nullifier), the proof must reproduce
  // the tag, and the owner's sequence (bound into the message hash) stops
  // replays. Immediate mode only.
  NULLIFIER_MODE_OWNERSHIP = 1;
}

enum ShieldBatchMode {
  // Only immediate execution allowed
  SHIELD_BATCH_MODE_IMMEDIATE_ONLY = 0;
  // Only encrypted batch execution allowed
  SHIELD_BATCH_MODE_ENCRYPTED_ONLY = 1;
  // User chooses per-submission
  SHIELD_BATCH_MODE_EITHER = 2;
}
```

`ShieldedOpRegistration.Validate()` (`x/shield/types/registration.go`) requires a non-empty `message_type_url` and rejects `NULLIFIER_MODE_OWNERSHIP` with any batch mode other than `IMMEDIATE_ONLY` (`ErrOwnershipBatchMode`), rejects `PROOF_DOMAIN_GROUP_TREE` with any batch mode other than `IMMEDIATE_ONLY` (`ErrInvalidProofDomain`; the root is resolved from the cleartext inner message, so the immediate-only guarantee rests on the registration rule, with the batch-path refusals as a second layer), and rejects `epoch_window > 1` unless `nullifier_scope_type` is EPOCH and the mode is CONSUME (`sdkerrors.ErrInvalidRequest`). Both genesis validation and `MsgRegisterShieldedOp` run it.

### Used Nullifiers

```protobuf
// Key: domain (uint32) + scope (uint64) + nullifier_hex (string)
// Value: block height when used
message UsedNullifier {
  uint32 domain = 1;
  uint64 scope = 2;
  string nullifier_hex = 3;
  int64  used_at_height = 4;
}
```

`scope` here is the raw scope (epoch, message field value, or 0), not the proof's scope element: the domain is already part of the key. Only consume-mode execs record nullifiers; ownership-mode execs reuse the owner tag and record nothing, and x/shield keeps no ownership state (the owner claim and its sequence live with the content in the target module).

**Canonical encodings only.** Nullifiers, rate-limit nullifiers and merkle roots are BN254 scalar-field elements, and the Groth16 verifier reduces every public input mod r. Without a check, `n` and `n + r` (both fit in 32 bytes for most `n`) verify against the same proof while being distinct store keys, so one proof could act about five times in one scope, exceed the identity rate limit five-fold, or be replayed by anyone under a new encoding. Every path that accepts public inputs (the ante handler's format check, `validateImmediate`, `validateBatch`, and the EndBlocker for the sealed operation nullifier) refuses a value that is not below r (`ErrNonCanonicalPublicInput`, `types.ValidateCanonicalPublicInputs`). The `canonical-nullifiers` invariant checks no stored nullifier is non-canonical.

### Funding Ledger

```protobuf
// Tracks funding per day to enforce the daily cap
// Day is the UTC day of the block time (block_time / 86400)
message DayFunding {
  uint64 day = 1;
  string amount_funded = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}
```

### Pending Shielded Operations (Encrypted Batch Mode)

```protobuf
// Stored in queue until its epoch has closed, its batch was demanded and the
// epoch key exists; it then executes in that block.
// Keyed by (target_epoch, id): one epoch's batch is a prefix range and
// per-epoch counts walk keys only (payloads are never decoded for counting).
message PendingShieldedOp {
  uint64 id = 1;                    // auto-increment ID
  uint64 target_epoch = 2;          // shield epoch when this should execute
  bytes submission_nullifier = 3;   // proven at submission; dedups the queue; unrelated to the sealed operation nullifier
  bytes merkle_root = 4;            // cleartext — validated at submission
  ProofDomain proof_domain = 5;
  uint32 min_trust_level = 6;
  bytes encrypted_payload = 7;      // TLE-encrypted MsgShieldedExec fragment: inner_message + operation proof + operation nullifier
  int64 submitted_at_height = 8;    // for max_pending_epochs tracking
  uint64 submitted_at_epoch = 9;    // shield epoch when submitted (both proofs' RateLimitEpoch)
  bytes rate_limit_nullifier = 10;  // proven by the submission proof; the operation proof must reproduce it
}
```

### Shield Epoch State

```protobuf
// Tracks the current shield epoch and decryption key availability
message ShieldEpochState {
  uint64 current_epoch = 1;
  int64  epoch_start_height = 2;
  // Always false: a key never exists for the open epoch by design (keys are
  // reconstructed only for closed, demanded epochs and stored per epoch in
  // ShieldEpochDecryptionKey). Kept for genesis compatibility.
  bool   decryption_key_available = 3;
}

// Shield epoch decryption key (reconstructed from validator shares)
message ShieldEpochDecryptionKey {
  uint64 epoch = 1;
  bytes  decryption_key = 2;        // BN256 G1 point (reconstructed epoch secret)
  int64  reconstructed_at_height = 3;
}

// Validator's decryption share for a shield epoch
message ShieldDecryptionShare {
  uint64 epoch = 1;
  string validator = 2;
  bytes  share = 3;                  // epoch-derived share: masterShare_i * H_to_G1(epoch)
}
```

### Decryption Demands

`DecryptionDemands` (`collections.Map[uint64 epoch, int64]`) records, per closed epoch, the height at which the EndBlocker asked validators for their shares: because the epoch's batch was ready, or because the randomness beacon demands every closing epoch (see "Randomness beacon"). It is the gate between "ops are queued" and "the key may exist": validators produce shares only for demanded epochs, every share-acceptance path refuses shares for undemanded epochs, and TLE liveness judges only demanded epochs, one full `shield_epoch_interval` after the demand (the stored value turns negative, `-(height+1)`, once judged, so it is judged exactly once while late shares stay acceptable until the epoch's ops expire). Exported in genesis as `decryption_demands`.

## Messages

### MsgShieldedExec

The single entry point for all anonymous operations. Supports both immediate and encrypted batch modes:

```protobuf
message MsgShieldedExec {
  option (cosmos.msg.v1.signer) = "submitter";

  // Any account — does not need funds, identity is irrelevant. Clients
  // default to the shared public anonymous submitter (see below).
  string submitter = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];

  // --- Immediate mode fields (cleartext) ---

  // The operation to execute, encoded as Any
  // Every signer of the inner message MUST be the shield module account address
  // (all declared signers are checked, not only the first)
  // REQUIRED for IMMEDIATE mode, EMPTY for ENCRYPTED_BATCH mode
  google.protobuf.Any inner_message = 2;

  // ZK proof bytes (Groth16 over BN254), always required. IMMEDIATE: the
  // operation proof, bound to inner_message. ENCRYPTED_BATCH: the submission
  // proof, bound to encrypted_payload; the operation proof is sealed inside.
  bytes proof = 3;

  // --- Always cleartext (both modes) ---

  // 32-byte nullifier (safe to expose, unlinkable to identity). IMMEDIATE:
  // the operation nullifier. ENCRYPTED_BATCH: the submission nullifier.
  bytes nullifier = 4;

  // 32-byte rate-limit nullifier: MiMC(secret_key, MaxUint64, epoch)
  // Same for ALL trust-tree operations by the same member in the same epoch;
  // a group-tree exec binds the epoch to its scope instead
  // (MiMC(ScopeElement(domain, scope), epoch)), so a secret-ballot vote's
  // value is unique to the member and the ballot.
  // Used for per-identity rate limiting without breaking anonymity.
  // Proven correct by the ZK circuit (cannot be forged).
  bytes rate_limit_nullifier = 5;

  // Merkle root the proof was generated against
  bytes merkle_root = 6;

  // Proof domain (trust tree)
  ProofDomain proof_domain = 7;

  // Minimum trust level being proven (0 = any member)
  uint32 min_trust_level = 8;

  // --- Execution mode selection ---

  ShieldExecMode exec_mode = 9;

  // --- Encrypted batch mode fields ---

  // TLE-encrypted payload containing (inner_message + operation proof +
  // operation nullifier); sealed to the shield epoch (IBE identity)
  // REQUIRED for ENCRYPTED_BATCH mode, EMPTY for IMMEDIATE mode
  bytes encrypted_payload = 10;

  // Target shield epoch for batch execution
  // Must be the current epoch (the op executes once that epoch has closed and its key exists)
  // REQUIRED for ENCRYPTED_BATCH mode
  uint64 target_epoch = 11;
}

enum ShieldExecMode {
  SHIELD_EXEC_IMMEDIATE = 0;
  SHIELD_EXEC_ENCRYPTED_BATCH = 1;
}

message MsgShieldedExecResponse {
  // For IMMEDIATE mode: the response from the inner message execution
  // For ENCRYPTED_BATCH mode: empty (result available after batch execution)
  google.protobuf.Any inner_response = 1;

  // For ENCRYPTED_BATCH mode: the pending operation ID for tracking
  uint64 pending_op_id = 2;
}
```

### Public Anonymous Submitter

`MsgShieldedExec` still needs an outer signer, and a member's own address would deanonymize them. `types.PublicSubmitterAddress()` is a shared secp256k1 account whose private key is **public on purpose**: it is derived from `sha256("sparkdream/shield/public-submitter/v1")` (`types.PublicSubmitterPrivKey()`). Every anonymous client signs its exec with it, so all anonymous actions come from one address and the outer signer reveals nothing. The ZK proof, not the signature, authorizes an exec.

- Clients send **unordered** txs, so concurrent submitters never race on the account's sequence. The ante handler enforces it: a shield tx signed by the public submitter that is not unordered is refused (`ErrOrderedPublicSubmitterTx`), since anyone could otherwise bump the shared sequence and an ordered tx is also a client fingerprint.
- `InitGenesis` creates the account if it is missing - clients sign with its account number, which a just-created account would not have yet.
- Because anyone can sign as it, the `ShieldGasDecorator` refuses any tx signed by it that is not a `MsgShieldedExec` (`sdkerrors.ErrUnauthorized`), so nothing else it signs is spendable by anyone.
- Anyone can also rewrap a pending exec in a new outer tx; the CheckTx nullifier mark in `PrecheckExec` keeps those copies out of the mempool, the DeliverTx spend in the ante handler (see [Ante Handler](#ante-handler-shieldgasdecorator)) makes any copy that reaches a block fail before the module pays, and the message binding (`MessageHash`) stops a copy from carrying different content.

Any other account still works as submitter; the public one is the recommended default.

### DKG via Vote Extensions

Validators participate in the DKG ceremony and submit decryption shares via **CometBFT Vote Extensions** rather than standalone `Msg` transactions. This eliminates separate transaction overhead and leverages the existing consensus participation mechanism.

**How it works:**

1. **ExtendVote / VerifyVoteExtension**: Each validator embeds DKG-related cryptographic material in their consensus vote extensions. The `DKGVoteExtensionHandler` (in `x/shield/abci/vote_extensions.go`) builds the appropriate extension based on the current DKG phase:

   - **REGISTERING phase**: Validators embed their BN256 G1 public key + Schnorr proof of possession (PoP) over `PoPMessage(operator address, round)` (round-bound, so a PoP cannot be replayed into a later round).
   - **CONTRIBUTING phase**: Validators embed Feldman commitments (G1 + G2) + ECIES-encrypted polynomial evaluations (one per other participant) + PoP under `a_0`.
   - **COMPLAINING phase**: Each validator decrypts every evaluation dealt to it and checks `p_i(j)·G1 == Σ_k C_{i,k}·j^k` against the dealer's commitments. For every dealer whose evaluation is missing, does not decrypt, or fails the check, it embeds the dealer's index (`complaints`). Nothing to complain about: no extension.
   - **JUSTIFYING phase**: A dealer with complaints against it embeds, per complainer, the disputed evaluation `p_i(j)` in clear (`justifications`). The chain verifies it against the dealer's commitments; a valid answer marks the complaint justified and the complainer uses the public evaluation in place of the ciphertext it could not open.
   - **ACTIVE phase**: When a closed epoch has a decryption demand (its batch is ready) and this validator has not served it, the validator embeds that epoch's decryption share (`masterShare_i * H1(epoch)`), oldest demanded epoch first, one per vote. Validators keep serving an epoch until its liveness is judged, even once its key exists (see Validator Overhead).

2. **PrepareProposalHandler**: The block proposer aggregates the DKG vote extensions from the previous block's commit into a single `InjectedDKGData` pseudo-transaction at position 0 of the block, then hands the rest of the block to the SDK's default proposal handler (byte and block-gas limits over the mempool). Each extension is checked against the phase's acceptance rule (`x/shield/abci/validate.go`) with the same state `ProcessProposal` will use, so an honest proposer never injects something that would get its block rejected. The injection is capped at half of `MaxTxBytes`; extensions that do not fit are left out and their validators re-send them in the next vote, so a large validator set's ceremony spreads over several blocks instead of never landing. If no DKG is active or no extension qualifies, the block is whatever the default handler returns.

   Each injected ceremony extension carries the validator's CometBFT **vote-extension signature** (`ValidatorDKGExtension.extension_signature`): the proposer copies it from the extended commit and leaves out any extension whose bytes do not re-marshal identically or whose signature does not verify.

3. **ProcessProposalHandler**: Validators check that every injected ceremony extension is signed by the validator it names — its consensus key over the length-delimited `CanonicalVoteExtension{extension, height - 1, last commit round, chain id}`, the same bytes CometBFT signs (`baseapp.ValidateVoteExtensions` uses them too) — and that no validator appears twice. Without this a single proposer could inject registrations, contributions or complaints "from" other validators: choose their keys and polynomials (and so learn the master secret), or complain on their behalf against every honest dealer, whose justifications would then publish enough evaluations to reconstruct the master secret. The proof of possession inside an extension does not authenticate the validator, since it is made under a key the prover chooses. Decryption shares need no signature: each is pairing-verified against the validator's public share, which only the holder of the secret share can satisfy. Validators then re-run the acceptance rule on every injected extension and share (round/phase, participant membership, PoPs, commitment counts and G1/G2 pairing consistency, complaint and justification validity, share pairing checks). An invalid injection rejects the proposal; the remaining txs go to the SDK default handler.

4. **PreBlocker**: `ProcessDKGInjection` (in `x/shield/abci/preblocker.go`) extracts the `InjectedDKGData` from tx[0], maps each validator's consensus address to their operator address via the staking keeper, re-verifies each ceremony extension's signature (against `req.Height - 1` and `DecidedLastCommit.Round`; a node replaying blocks never ran ProcessProposal), re-applies the acceptance rule, and stores what passes:
   - For REGISTERING: stores public keys as DKG registrations (first key per round wins)
   - For CONTRIBUTING: stores Feldman commitments + encrypted evaluations as DKG contributions, updates the contribution count
   - For COMPLAINING: records `(dealer, complainer)` complaints (`shield_dkg_complaint`)
   - For JUSTIFYING: marks complaints answered with a valid evaluation as justified and stores the evaluation (`shield_dkg_justification`)
   - For ACTIVE: re-verifies each decryption share (`VerifyValidatorDecryptionShare`), stores the valid ones, drops the rest with `shield_tle_invalid_share`, and attempts epoch key reconstruction when the threshold is met
   - tx[0] stays in the block (CometBFT needs one result per tx); it fails to decode in normal delivery and is recorded as a failed tx

**Vote extension data format:**

```protobuf
message DKGVoteExtension {
  uint64 round = 1;
  DKGPhase phase = 2;
  // REGISTERING phase
  bytes registration_pub_key = 3;   // BN256 G1 point
  bytes registration_pop = 4;       // Schnorr PoP over PoPMessage(operator address, round)
  // CONTRIBUTING phase
  repeated bytes feldman_commitments = 5;
  repeated EncryptedEvaluation encrypted_evaluations = 6;
  bytes contribution_pop = 7;
  repeated bytes feldman_commitments_g2 = 8;  // same coefficients on G2 (pairing-checked against 5)
  // ACTIVE phase (decryption shares)
  uint64 decryption_epoch = 9;
  bytes decryption_share = 10;      // epoch-derived share: masterShare_i * H1(epoch)
  // COMPLAINING phase
  repeated uint32 complaints = 11;  // 1-based dealer indices
  // JUSTIFYING phase
  repeated DKGJustification justifications = 12;
}

message DKGJustification {
  uint32 complainer_index = 1;  // 1-based
  bytes evaluation = 2;         // p_dealer(complainer_index), verifiable against the dealer's commitments
}

// On-chain complaint record (DKGComplaints, key dealer + complainer; exported
// in genesis as dkg_complaints)
message DKGComplaint {
  string dealer_address = 1;
  string complainer_address = 2;
  bool justified = 3;
  bytes evaluation = 4;         // set once justified
}
```

**Share verification**: every path that admits a share (`VerifyVoteExtension`, `ProcessProposal`, PreBlocker) verifies it against the validator's G2 public key share stored in `TLEKeySet` using a pairing check, and additionally requires the epoch to be closed, inside the `max_pending_epochs` window and demanded by the EndBlocker:

```
Verification equation:
  epoch_tag = H1("sparkdream/shield/tle/epoch-tag/v1" || be64(epoch))   // hash-to-curve
  e(epoch_share_i, G2) == e(epoch_tag, pubShare_i)

This proves epoch_share_i = secretShare_i * epoch_tag without revealing secretShare_i.
If any check fails, the share is rejected (ErrInvalidDecryptionShare): the vote extension or proposal carrying it is refused, and the PreBlocker never stores it, so it can neither poison reconstruction nor count as participation.
```

Late decryption shares are accepted for up to `max_pending_epochs` past epochs while the epoch's demand stands (to allow decryption of carried-over operations). Shares for the current epoch, or for an epoch no batch was demanded for, are refused: a key must never exist before its batch is ready.

**DKG lifecycle (vote extension-based):**

The `dkg_window_blocks` window is split into four equal phases (`newDKGState`): registration, dealing, complaints, justifications.

1. **INACTIVE**: No DKG ceremony in progress. BeginBlocker monitors the bonded validator set — when the count reaches `min_tle_validators` (param field 16) and no TLE key set exists, a DKG ceremony auto-opens.
2. **REGISTERING**: Validators embed BN256 G1 public keys + round-bound Schnorr PoP in their vote extensions. At `registration_deadline` the participant set (`expected_validators`, whose order fixes the 1-based Shamir indices) is **narrowed to the validators that registered a key this round**: nobody can encrypt an evaluation to a validator without one, so keeping it would give it a public share it can never serve. Fewer registrants than `min_tle_validators` fails the round.
3. **CONTRIBUTING**: Validators embed Feldman commitments (G1 + G2, pairing-consistent) + ECIES-encrypted polynomial evaluations for every other participant + PoP under `a_0`. When every participant has dealt, or at `contribution_deadline`, at least `ceil(threshold_numerator / threshold_denominator × participants)` contributions must be in, else the round fails.
4. **COMPLAINING** (until `complaint_deadline`): each validator checks the evaluations dealt to it against the dealers' commitments and reports the dealers whose evaluation is missing, undecryptable or inconsistent. Complaints are not verified on chain (the dealer's answer settles them).
5. **JUSTIFYING** (until `justification_deadline`): accused dealers publish the disputed evaluations in clear; the chain verifies each against the dealer's commitments. At the deadline every dealer with a complaint it did not answer validly is **disqualified** (`disqualified_dealers`, `shield_dkg_disqualified`) and its contribution dropped; the qualified contributions must still meet the threshold, else the round fails. The master public key is the sum of the qualified dealers' constant-term commitments, each participant's public share the aggregated polynomial evaluated at its index, in both groups; the key set is stored and the TLE liveness record reset.
6. **ACTIVE**: DKG complete. Encrypted batch mode turns on by itself only when `enable_batch_after_dkg` is set (false in production: governance enables it explicitly with `MsgUpdateParams` once satisfied with the ceremony; true in `testparams` builds). Validators embed decryption shares in vote extensions for closed epochs the EndBlocker has demanded. PreBlocker verifies and stores shares and attempts epoch key reconstruction when the threshold is met; the same block's EndBlocker then executes the batch.

While `params.enabled` is false the BeginBlocker does not run, so a ceremony phase (REGISTERING through JUSTIFYING) cannot advance; the vote-extension, proposal and PreBlocker paths then neither produce nor accept that phase's data, instead of piling it up against deadlines that never move. ACTIVE-phase decryption shares keep flowing, because the EndBlocker still judges the liveness of demanded epochs.

A validator that misses the complaint window while holding a bad evaluation ends up with a share inconsistent with its public share and is judged like one that is offline — the standard assumption of a complaint-based DKG is that participants are online for the whole ceremony. A dealer's garbage ciphertext cannot crash a validator: ECIES decryption is length-checked and recovered (`decryptEvaluation`).

**Validator set drift:**
- BeginBlocker monitors `max_validator_set_drift` (param field 18) — the percentage of original DKG participants no longer in the bonded set, counted by head (departures only, truncated to a whole percent). When drift reaches the threshold (at or above; default 33%), a new DKG ceremony is automatically triggered (`EventTypeShieldValidatorDrift`).
- **Validator leaves**: Their shares are no longer available for future epochs. Threshold must still be met with remaining validators. If the active validator set drops below threshold, encrypted batch mode gracefully degrades (ops carry over, eventually expire).

### MsgTriggerDKG (Governance)

Triggers a new DKG ceremony (proto name `MsgTriggerDkg`). Used when the validator set changes significantly (>1/3 turnover) or when the existing key set needs rotation. Opens a new registration window; encrypted batch mode is disabled until the new DKG completes. Refused with `ErrDKGInProgress` while a round is in any phase between REGISTERING and ACTIVE (a re-key mid-ceremony would discard its contributions and complaints).

```protobuf
message MsgTriggerDkg {
  option (cosmos.msg.v1.signer) = "authority";
  string authority = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  // Optional threshold numerator (0 = default 2)
  uint64 threshold_numerator = 2;
  // Optional threshold denominator (0 = default 3)
  uint64 threshold_denominator = 3;
}

message MsgTriggerDkgResponse {}
```

**Validation**: refused with `ErrInsufficientValidators` when fewer than `min_tle_validators` validators are bonded (or the staking keeper is not wired). After defaulting, `threshold_numerator <= threshold_denominator <= 100` (`ErrInvalidDKGThreshold`): a threshold above the participant count could never be met.

**Effect**: Opens round N+1 in REGISTERING and sets `encrypted_batch_enabled = false` (governance turns it back on after the new round completes, unless `enable_batch_after_dkg` is set; the key set itself is replaced when the round aggregates). Every per-round record of the previous round is cleared (registrations, contributions, complaints — a stale registration would have dealers encrypt evaluations to a retired key), and everything derived from the old key set is retired at once (`RetireKeySetMaterial`): decryption shares, reconstructed epoch keys, decryption demands and the TLE liveness record are cleared, and every pending op is dropped with a `shield_batch_expired` event (count + `dkg_round`) — they were sealed to the old key and could never be opened under the new one. Validator-set drift re-keying (`dkgCheckDrift`) retires the same material. Validators automatically re-register via vote extensions when the new round opens.

### MsgRegisterShieldedOp (Governance)

Registers or updates a shielded operation. Authority is x/gov module account.

```protobuf
message MsgRegisterShieldedOp {
  option (cosmos.msg.v1.signer) = "authority";
  string authority = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  ShieldedOpRegistration registration = 2;
}

message MsgRegisterShieldedOpResponse {}
```

A nullifier domain keeps one scoping rule: a registration is refused (`ErrNullifierDomainConflict`) when another registered op uses the same `nullifier_domain` with a different `nullifier_mode`, `nullifier_scope_type`, `scope_field_path` or epoch window. Uniqueness and pruning are per domain (`PruneEpochScopedNullifiers` prunes every domain an EPOCH op uses), so an EPOCH op in, say, the anonymous-vote domain would have pruning delete old vote nullifiers and re-open voting, and two windows in one domain would make different ops' nullifiers collide. Ops that share a domain on purpose (upvote and downvote of one target) have identical scoping. `GenesisState.Validate` applies the same rule to the genesis op table. An in-place update of an op that keeps its domain is checked against the registration it replaces as well, so an op alone in its domain cannot change its scope type, field or window in place (its stored nullifiers were written under the old rule; an EPOCH rescope would let pruning delete them and re-admit every action). Changing how an op is scoped needs a new domain: re-registering an op under a new domain resets its uniqueness (the old nullifiers stay under the old domain, which is retired).

### MsgDeregisterShieldedOp (Governance)

Fully removes a shielded operation registration from state. Unlike setting `active = false` (which preserves state for potential reactivation), deregistration deletes the entry entirely.

Its nullifier domain's scoping rule outlives it. When no other registered op uses the domain, the deregistered registration is kept in `RetiredDomains` (keyed by domain): `PruneEpochScopedNullifiers` still expires an EPOCH domain's nullifiers, and `MsgRegisterShieldedOp` refuses an op that would reuse the domain under a different rule (`ErrNullifierDomainConflict`), exactly as for a live op. Registering a compatible op makes the domain live again and clears the entry; an update that moves an op to another domain retires its old domain the same way. A retired EPOCH domain's entry is dropped once pruning has emptied it; a retired MESSAGE_FIELD or GLOBAL domain keeps its nullifiers, and its entry, for good. `RetiredDomains` is exported in genesis.

```protobuf
message MsgDeregisterShieldedOp {
  option (cosmos.msg.v1.signer) = "authority";
  string authority = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string message_type_url = 2;
}

message MsgDeregisterShieldedOpResponse {}
```

### MsgUpdateParams (Governance)

Standard governance parameter update.

```protobuf
message MsgUpdateParams {
  option (cosmos.msg.v1.signer) = "authority";
  string authority = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  Params params = 2 [(gogoproto.nullable) = false];
}

message MsgUpdateParamsResponse {}
```

## Queries

```protobuf
service Query {
  // Params returns module parameters
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);

  // ShieldedOp returns the registration for a specific message type
  rpc ShieldedOp(QueryShieldedOpRequest) returns (QueryShieldedOpResponse);

  // ShieldedOps returns all registered shielded operations
  rpc ShieldedOps(QueryShieldedOpsRequest) returns (QueryShieldedOpsResponse);

  // ModuleBalance returns the shield module account's current balance
  rpc ModuleBalance(QueryModuleBalanceRequest) returns (QueryModuleBalanceResponse);

  // NullifierUsed checks if a nullifier has been used in a given domain+scope
  rpc NullifierUsed(QueryNullifierUsedRequest) returns (QueryNullifierUsedResponse);

  // DayFunding returns the amount funded from community pool today
  rpc DayFunding(QueryDayFundingRequest) returns (QueryDayFundingResponse);

  // --- Encrypted batch mode queries ---

  // ShieldEpoch returns the current shield epoch state
  rpc ShieldEpoch(QueryShieldEpochRequest) returns (QueryShieldEpochResponse);

  // PendingOps returns pending shielded operations (optionally filtered by epoch)
  rpc PendingOps(QueryPendingOpsRequest) returns (QueryPendingOpsResponse);

  // PendingOpCount returns the count of pending operations (for queue capacity checks)
  rpc PendingOpCount(QueryPendingOpCountRequest) returns (QueryPendingOpCountResponse);

  // TLEMasterPublicKey returns the TLE master public keys; clients encrypt to master_public_key_g2
  rpc TLEMasterPublicKey(QueryTLEMasterPublicKeyRequest) returns (QueryTLEMasterPublicKeyResponse);

  // TLEKeySet returns the full TLE key set (master key + validator shares)
  rpc TLEKeySet(QueryTLEKeySetRequest) returns (QueryTLEKeySetResponse);

  // VerificationKey returns a ZK verification key by circuit ID
  rpc VerificationKey(QueryVerificationKeyRequest) returns (QueryVerificationKeyResponse);

  // TLEMissCount returns a validator's current TLE miss count
  rpc TLEMissCount(QueryTLEMissCountRequest) returns (QueryTLEMissCountResponse);

  // DecryptionShares returns the decryption shares submitted for a given epoch
  rpc DecryptionShares(QueryDecryptionSharesRequest) returns (QueryDecryptionSharesResponse);

  // IdentityRateLimit returns the remaining rate limit for a given rate-limit nullifier in the current epoch
  rpc IdentityRateLimit(QueryIdentityRateLimitRequest) returns (QueryIdentityRateLimitResponse);

  // Beacon returns an epoch's randomness beacon value and status (0 = latest closed epoch)
  rpc Beacon(QueryBeaconRequest) returns (QueryBeaconResponse);

  // DKGState returns the current DKG ceremony state
  rpc DKGState(QueryDKGStateRequest) returns (QueryDKGStateResponse);

  // DKGContributions returns all DKG contributions for the current round
  rpc DKGContributions(QueryDKGContributionsRequest) returns (QueryDKGContributionsResponse);
}
```

## Ante Handler: ShieldGasDecorator

The critical piece that enables gasless submission. Inserted into the ante handler chain. Because the module pays, the decorator validates the exec **before** any fee moves (SHIELD-8):

```go
func (d ShieldGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
    shieldMsg, hasShieldedExec := firstShieldedExec(tx.GetMsgs())

    if !hasShieldedExec {
        // The public anonymous submitter's key is public: refuse anything
        // else it signed (ErrUnauthorized).
        if err := rejectPublicSubmitter(tx); err != nil {
            return ctx, err
        }
        return next(ctx, tx, simulate)
    }

    // REJECT multi-message transactions containing MsgShieldedExec
    // (deanonymization + piggybacking on module-paid gas).
    if len(tx.GetMsgs()) != 1 {
        return ctx, shieldtypes.ErrMultiMsgNotAllowed
    }

    params, _ := d.shieldKeeper.GetShieldParams(ctx)
    if !params.Enabled {
        return ctx, shieldtypes.ErrShieldDisabled
    }

    // --- Validation BEFORE paying gas ---
    // 1. Format, both modes: 32-byte nullifier and rate-limit nullifier, and a
    //    proof of at least 128 bytes (the operation proof, or the submission
    //    proof of a batch exec). Batch execs are refused outright while batch
    //    mode is off.
    if len(shieldMsg.Nullifier) != 32 || len(shieldMsg.RateLimitNullifier) != 32 {
        return ctx, shieldtypes.ErrInvalidNullifierLength
    }
    // Canonical BN254 field elements only (see Used Nullifiers).
    if err := shieldtypes.ValidateCanonicalPublicInputs(shieldMsg.Nullifier,
        shieldMsg.RateLimitNullifier, shieldMsg.MerkleRoot); err != nil {
        return ctx, err // ErrNonCanonicalPublicInput
    }
    if len(shieldMsg.Proof) < 128 {
        return ctx, shieldtypes.ErrInvalidProof
    }
    if batch && !params.EncryptedBatchEnabled {
        return ctx, shieldtypes.ErrEncryptedBatchDisabled
    }

    // 2. Full validation (PrecheckExec): every check the msg server would fail
    //    on, then the proof (incl. MessageHash) and the identity rate limit,
    //    which the proof makes unforgeable in both modes. There is no
    //    per-address cap: anonymous clients share one submitter address and
    //    the identity rate limit bounds spend per member. Outside DeliverTx
    //    this runs behind the node-local proof guard (rejected-proof cache +
    //    verification throttle; see below).
    consumed, err := d.precheckExec(ctx, shieldMsg, simulate)
    if err != nil {
        return ctx, err
    }
    // The msg server needn't verify the proof again, and must not spend an
    // exec the ante already spent.
    ctx = shieldtypes.WithProofVerified(ctx, shieldMsg)
    if consumed {
        ctx = shieldtypes.WithExecConsumed(ctx, shieldMsg)
    }

    fees := tx.(sdk.FeeTx).GetFee()

    // 3. The submitter picks the fee but the module pays it: bond denom only,
    //    at most max_fee_per_exec.
    maxFee := sdk.NewCoins(sdk.NewCoin(d.shieldKeeper.BondDenom(ctx), params.MaxFeePerExec))
    if !fees.IsZero() && !fees.IsAllLTE(maxFee) {
        return ctx, shieldtypes.ErrFeeTooHigh
    }

    // 4. CheckTx only: the node's minimum gas prices, as DeductFeeDecorator
    //    (skipped below) would apply them, so a zero or token fee can't buy
    //    a large gas limit.
    if ctx.IsCheckTx() && !simulate {
        if err := checkMinGasPrices(ctx, fees, feeTx.GetGas()); err != nil {
            return ctx, err // ErrInsufficientFee
        }
    }

    if fees.IsZero() {
        // No fees to pay — proceed (gas is still metered)
        ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)
        return next(ctx, tx, simulate)
    }

    // 5. Chain-wide per-epoch budget (max_fees_per_epoch), the identity's
    //    share of it, and the group-tree reserve.
    if err := d.shieldKeeper.ChargeEpochFee(ctx, fees.AmountOf(bondDenom), params, shieldMsg); err != nil {
        return ctx, err // ErrEpochFeeBudgetExhausted / ErrIdentityFeeShareExhausted
    }

    // Deduct fees from shield module account → fee collector
    if err := d.bankKeeper.SendCoinsFromModuleToModule(ctx,
        shieldtypes.ModuleName, authtypes.FeeCollectorName, fees); err != nil {
        return ctx, shieldtypes.ErrShieldGasDepleted
    }

    // 6. DeliverTx only: record the charge (fee, gas limit, epoch, rate-limit
    //    nullifier) for the FeeRefundDecorator post handler, which returns
    //    fee - ceil(fee x gas_used / gas_limit) to the module after the
    //    messages ran and unbooks it (RefundEpochFee).
    if !simulate && ctx.ExecMode() == sdk.ExecModeFinalize {
        d.marks.mark(ctx, FeeCharge{...})
    }

    // Set fee-paid flag so the standard DeductFeeDecorator skips this tx
    ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)
    return next(ctx, tx, simulate)
}
```

`PrecheckExec` runs the same read-only validation as the msg server (`validateImmediate` for an immediate exec, `validateBatch` for a batch exec), then, for an immediate exec in every mode but simulate (CheckTx, ReCheckTx, the proposal-verification runs and DeliverTx), **spends** it in the ante state: a consume-mode nullifier is recorded (an ownership-mode exec calls the target module's `AdvanceOwnership` instead, so the owner sequence moves on) and the exec is counted against the identity rate limit. It reports that it did, and the ante handler tells the msg server through `types.WithExecConsumed`; the msg server then only runs the inner message.

- In **DeliverTx** the ante state commits with the fee payment even when the inner message fails and the msg state is discarded. A failing exec therefore costs its member a rate-limit slot and its nullifier, exactly like a successful one: it can't be replayed block after block at the reserve's expense (before, the nullifier and the rate-limit increment were written in the msg state and reverted with the failed inner message, while the module had already paid the fee), and nobody can re-run it later once its target becomes valid. This matches batch mode, where the operation nullifier stays recorded on failure. Copies of one exec in a block fail in the ante after the first, before anything is paid.
- In **CheckTx** the check state is discarded at every commit; the mark keeps rewrapped copies of a pending exec out of the mempool (anyone can sign as the public submitter).
- A batch exec's submission nullifier is marked pending in CheckTx; in DeliverTx the msg server records it (it cannot fail once `validateBatch` passed against the same state).

### Mempool CPU Guard (CheckTx Proof Verification)

Verifying in CheckTx means a Groth16 pairing check (~1.5-3 ms per proof) for every immediate exec a node admits. Public-submitter txs are unordered and an invalid proof is rejected before any fee moves, so submission is free: without a guard, anyone could make every node pay a pairing check per submission, indefinitely (staged-upgrade review finding **M4** - **mitigated**). The reserve is never at risk either way; this is purely a CPU-DoS defence. `ShieldGasDecorator.precheckExec` (`x/shield/ante/proof_guard.go`) wraps `PrecheckExec` with two **node-local, non-consensus** defences:

1. **Rejected-proof cache.** A bounded, mutex-guarded LRU (`rejectedProofCacheSize` = 4096 keys, < 1 MiB) of execs whose proof failed verification. An identical resubmission is refused with `ErrInvalidProof` ("proof already rejected by this node") without verifying again. The key is `sha256` over: the full `MsgShieldedExec` bytes (proof, merkle root, nullifiers, min trust level, proof domain, inner message - every public input the message carries), the stored `shield_v1` VK bytes (a VK change, which only a chain upgrade can make, invalidates every entry), the current shield epoch (the `RateLimitEpoch` input and the scope of epoch-scoped ops) and the op's registration (nullifier domain and scope derivation; a batch submission proof derives both from the payload, so its key is the message, VK and epoch alone). Verification is then a pure function of the key, so a cached rejection can never become valid. Only `ErrInvalidProof` is cached: in the `PrecheckExec` path it comes solely from proof verification (decoding the VK/proof, building the witness, the pairing check); every state-dependent failure (shield disabled, unregistered/inactive op, stale merkle root, used nullifier, identity rate limit) has its own error code and is re-checked each time. Ownership-mode execs are never cached - their Scope and MessageHash inputs come from the target content's owner record (scope, sequence), which the key cannot cover.
2. **Verification throttle.** A token bucket (`proofVerifyRate` = 150/s sustained, `proofVerifyBurst` = 300) caps how many verifications new CheckTx and simulate run. 150/s bounds an attacker to roughly a third to half of one core, while honest anonymous traffic - capped per member by `max_execs_per_identity_per_epoch` - sits far below it; the burst absorbs a flush of queued txs after a network hiccup. When the bucket is empty the exec is refused with **`ErrProofVerificationThrottled`** (code 51, retryable: honest clients resubmit the same tx a moment later). A cache hit costs no token, and an exec that fails a cheap state check before any verification gets its token back. **ReCheckTx is never throttled** - that would evict txs that already passed.

Both are used only in CheckTx / ReCheckTx / simulate and **never in DeliverTx** (`FinalizeBlock`), which always verifies, so consensus never depends on node-local memory. Neither applies without a stored VK (test-mode builds), where nothing is verified. The constants are package-level vars in `x/shield/ante` (tests override them).

`validateImmediate` also orders every cheap state check **before** the pairing check: registration, proof domain, trust level, ownership/nullifier freshness, the identity rate limit, and (inside `verifyProof`) the merkle root all run first, so garbage with a bogus root or a spent nullifier is rejected without a pairing. Reordering changes only which error a doubly-invalid exec reports; DeliverTx outcomes are identical.

Operators can additionally bound what any one peer can push at them with CometBFT's per-peer p2p limits (`send_rate` / `recv_rate` in `config.toml`, plus the mempool's `size` / `max_txs_bytes`); the chain ships no p2p changes.

The decorator is placed **before** the standard `DeductFeeDecorator` in the ante chain. The standard decorator checks the `ContextKeyFeePaid` flag and skips deduction if already handled.

### Single-Message Enforcement

`MsgShieldedExec` transactions **must** contain exactly one message. This prevents a user from bundling a shielded exec (gas-free) with non-shielded messages (piggybacking free gas). The ante handler rejects multi-message transactions containing any `MsgShieldedExec`.

## BeginBlocker: Auto-Funding

The BeginBlocker returns at once while `params.enabled` is false. Otherwise it runs `autoFundModule` (below), then always `advanceDKGStateMachine`: the early returns in the sketch leave the funding step only, never the DKG step.

```go
func (k Keeper) BeginBlocker(ctx context.Context) error {
    sdkCtx := sdk.UnwrapSDKContext(ctx)
    params := k.GetParams(ctx)

    if !params.Enabled {
        return nil
    }

    // Check current balance (bond denom from the late-wired identity keeper)
    moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleName)
    balance := k.bankKeeper.GetBalance(ctx, moduleAddr, k.BondDenom(ctx))

    // Only fund if below minimum reserve
    if balance.Amount.GTE(params.MinGasReserve) {
        return nil
    }

    // Calculate how much to fund (up to the gap, capped by daily limit)
    gap := params.MinGasReserve.Sub(balance.Amount)

    // Check how much has already been funded today (day = UTC day of block time)
    day := fundingDay(sdkCtx)
    funded := k.GetDayFunding(ctx, day)
    remaining := params.MaxFundingPerDay.Sub(funded)

    if !remaining.IsPositive() {
        sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventShieldFundingCapReached, ...))
        return nil // daily cap reached
    }

    // Fund the lesser of gap and remaining cap
    fundAmount := math.MinInt(gap, remaining)

    // The pool must hold the whole amount: no partial top-up, no event, just
    // try again next block.
    pool, _ := k.late.distrKeeper.GetCommunityPool(ctx)
    if pool.AmountOf(k.BondDenom(ctx)).TruncateInt().LT(fundAmount) {
        return nil
    }

    // Transfer from community pool (distribution module) to shield module
    coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), fundAmount))
    if err := k.late.distrKeeper.DistributeFromFeePool(ctx, coins, moduleAddr); err != nil {
        return nil // not fatal, logged
    }

    // Record funding for daily cap tracking
    k.SetDayFunding(ctx, day, funded.Add(fundAmount))

    sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeShieldFunded, ...)) // amount, day, new_balance

    return nil
}
```

### Daily Cap Rationale

The `max_funding_per_day` parameter is the key governance control:

- **Prevents pool depletion**: Even if shielded operations spike, the community pool can only be drawn down by a bounded amount per day
- **Governance-adjustable**: If anonymous usage grows, governance can increase the cap; if the pool is stressed, they can decrease it
- **Predictable budgeting**: Council treasuries (via x/split) can plan around a known maximum drain rate
- **Decoupled from epoch interval**: Changing `shield_epoch_interval` for privacy tuning doesn't accidentally change the budget
- **Graceful degradation**: When the cap is exhausted, shielded execution returns `ErrShieldGasDepleted` — operations fail gracefully rather than silently. Users can retry tomorrow or submit with their own gas (the inner message types still work as normal non-shielded transactions)

### Suggested Defaults

```
max_funding_per_day:                 200_000_000 uspark  (200 SPARK/day)
min_gas_reserve:                     100_000_000 uspark  (100 SPARK)
max_gas_per_exec:                        500_000 gas
max_execs_per_identity_per_epoch:             50
```

At ~200,000 gas per shielded exec and a gas price of 0.025 uspark/gas, each exec costs ~5,000 uspark (0.005 SPARK). The 200 SPARK daily cap supports ~40,000 shielded operations per day — far more than realistic early usage.

**Annual budget at max**: 200 SPARK/day × 365 = 73,000 SPARK/year. With community pool inflow of ~300-750K SPARK/year (15% of 2-5% inflation on 100M), shield draw at max ≈ 10-24% of inflow. Governance can adjust as usage patterns become clear.

## Threshold Timelock Encryption (Owned by x/shield)

x/shield owns all TLE infrastructure. There is no separate x/tle or x/vote module — all threshold encryption lives here because both TLE and ZK proofs are privacy primitives serving the same purpose.

### Cryptographic Primitives

- **Curve**: BN256 (alt_bn128), type-3 pairing. G1 carries epoch tags, validator shares and epoch keys; G2 carries the master public key clients encrypt to and the ciphertext's ephemeral point
- **Encryption**: Boneh-Franklin identity-based encryption with the shield epoch as the identity, implemented once in `tools/tle` and shared by the chain (decrypt), validators (shares) and clients (encrypt)
- **Secret sharing**: Shamir's Secret Sharing over the BN256 scalar field (Feldman VSS in the DKG)
- **Key reconstruction**: Lagrange interpolation over pairing-verified epoch shares
- **Symmetric layer**: HKDF-SHA256 over the pairing output, AES-256-GCM

### DKG (Distributed Key Generation)

Validators participate in a **Feldman VSS** ceremony to produce:
1. The **master public key** in both groups: `master_public_key = s·G1` (share bookkeeping) and `master_public_key_g2 = s·G2` (what clients encrypt to). Both are sums of the contributions' constant-term commitments; every contribution carries G1 and G2 commitments to the same polynomial, and the `e(C_k^G1, G2) == e(G1, C_k^G2)` consistency check rejects a contribution whose two halves disagree
2. Per-validator **secret key shares** (Shamir shares) — held privately by each validator
3. Per-validator **public key shares** in both groups — stored on-chain; the G2 one is what decryption shares are verified against

The DKG ceremony runs at chain genesis and after significant validator set changes. x/shield stores the key set; validators hold their secret shares locally.

**Why Feldman VSS**: Simple PoP (proof of possession) alone is insufficient to prevent rogue key attacks in aggregated key schemes. Feldman VSS provides verifiable shares where each validator can independently verify that their share is consistent with the committed polynomial, without a trusted dealer. The master public key is the polynomial's constant term commitment (`A_0 = s * G`), and each public share satisfies `pubShare_i = Σ_j(A_j * i^j)` where `A_j` are the polynomial commitments.

```protobuf
// Stored in x/shield state
message TLEKeySet {
  bytes master_public_key = 1;                // BN256 G1 point, s*G1
  uint64 threshold_numerator = 2;             // e.g. 2
  uint64 threshold_denominator = 3;           // e.g. 3 → 2/3 threshold
  repeated TLEValidatorPublicShare validator_shares = 4;
  int64 created_at_height = 5;
  bytes master_public_key_g2 = 6;             // BN256 G2 point, s*G2: clients encrypt to this
}

message TLEValidatorPublicShare {
  string validator_address = 1;
  bytes  public_share = 2;                    // BN256 G1 point, s_i*G1
  uint32 share_index = 3;                     // Shamir share index (1-based)
  bytes  public_share_g2 = 4;                 // BN256 G2 point, s_i*G2: decryption shares verify against this
}
```

A seeded key set (genesis) must carry both master keys and a G2 public share per validator (`TLEKeySet.Validate`); `test/shield/patch_genesis_tle.sh` + `tools/zk/cmd/seed-tle keygen` produce one for single-validator tests.

### Shield Epoch Lifecycle

```
Shield Epoch N                           Shield Epoch N+1
─────────────────────────────────────    ──────────────────────────────────────
│                                   │    │ boundary block B:
│  Users submit ENCRYPTED_BATCH ops │    │   EndBlocker: N's batch ready?
│  target_epoch = N, sealed to      │    │   (min_batch_size alone or across all
│  epoch N's identity               │    │    closed epochs, or max_pending_epochs)
│  Ops accumulate in pending queue  │    │     → DecryptionDemand(N, B)
│                                   │    │ following blocks:
│  No key for N can exist yet:      │    │   validators embed share_i(N) in votes
│  validators release shares only   │    │   each share pairing-checked, stored
│  for closed, demanded epochs      │    │   threshold → epoch key N (PreBlocker)
│                                   │    │   → same block's EndBlocker decrypts,
│                                   │    │     verifies, shuffles, executes N
├───────────────────────────────────┤    │ boundary B + interval:
  shield_epoch_interval blocks           │   TLE liveness judged for N
```

Readiness is decided before any share is released, so a batch that is too small waits *encrypted*; the old behaviour (shares for the current epoch as soon as one op was pending, key reconstructed mid-epoch) would have left every later submission to that epoch readable on arrival. Execution happens in the block the key appears, not at the next boundary.

### Validator Overhead

With `shield_epoch_interval = 50` blocks (~5 minutes):

- Validators embed decryption shares in their consensus vote extensions — zero additional transaction overhead; one share (one closed epoch) per vote, oldest demanded epoch first
- Shares ride along with existing CometBFT votes (~100 bytes of vote extension data per demanded epoch)
- No sidecar process needed — the `DKGVoteExtensionHandler` is built into the node binary
- Vote extensions must be enabled in consensus params (`abci.vote_extensions_enable_height = 1` in every network's `config.yml`; the genesis generator copies `genesis.consensus` from there)

**Liveness enforcement**: x/shield tracks its own TLE liveness over demanded epochs only. A demand is judged once, exactly one full epoch interval after it was recorded (the judge runs every block, and before expiry, so a force-demanded lone op's epoch is judged even though it falls out of the window at that same boundary): each **bonded** validator in the key set without a stored (verified) share for that epoch gets a miss. Validators are asked for their share until the demand is judged — including after the key exists, so a validator whose vote was not in the reconstructing commit is never judged for a share the chain stopped requesting. Misses live in a ring of the last `tle_miss_window` *judged* epochs (slot = judged-epoch counter mod window, so sparse demands do not keep a miss alive indefinitely); more than `tle_miss_tolerance` misses jails the validator (`x/slashing.Jail`) and sets `JailUntil` to now + `tle_jail_duration` seconds, after which `MsgUnjail` is accepted again. The ring, the miss counters and the counter are reset whenever the key set changes. Idle epochs, and epochs whose batch never became ready, count against no one. An invalid share is not stored, so it is a miss too. Validators that are no longer bonded are not judged.

TLE liveness parameters are defined directly in `Params` (fields 13-15: `tle_miss_window`, `tle_miss_tolerance`, `tle_jail_duration`). Setting `tle_miss_window` or `tle_miss_tolerance` to 0 disables liveness judging entirely (`Params.Validate` allows it), and `max_validator_set_drift = 0` disables drift re-keying. There is no separate `TLELivenessParams` message — keeping them in `Params` allows standard governance parameter updates via `MsgUpdateParams`.

### Per-Epoch Key Derivation (Forward Secrecy)

**Critical**: x/shield does NOT encrypt directly with the master public key. Doing so would mean every epoch's decryption key is the master secret itself — revealing it once would compromise all past and future encrypted payloads.

Instead the shield epoch is the IBE *identity*:

```
epoch_tag    Q  = H1("sparkdream/shield/tle/epoch-tag/v1" || be64(N))   hash-to-G1 (tools/tle.EpochTag)
epoch key    d  = s·Q                             G1; what the chain reconstructs
share_i      d_i = s_i·Q                          G1; what validator i releases
master key   P2 = s·G2                            G2; what clients use
```

**Properties:**
- Each epoch has a unique decryption key; revealing epoch N's key reveals NOTHING about epoch N±1
- Validators derive epoch shares from their master shares — no new DKG per epoch
- Nothing a client needs is secret: `P2` and the epoch number

**The tag must be a genuine hash-to-curve** (kyber's `pointG1.Hash`, try-and-increment over SHA-256; BN254 G1 has cofactor 1). `Point().Pick(XOF(label))` is *not* one: it draws a scalar `h` from the stream and multiplies the generator, so `Q = h·G1` with a public `h`, and anyone holding the on-chain G1 master public key computes `d = h·MPK` and every validator's share `h·pubShare_i` — the whole scheme collapses (review finding C1; `TestEpochTagHasNoKnownDiscreteLog` pins the fix). Both derivation sites (`tools/tle.EpochTag`, used by the chain, the validators' key store and the clients) go through this one function.

### Encryption Format

`tools/tle.Encrypt(master_public_key_g2, epoch, plaintext)`:

```
r      = random scalar
U      = r·G2                                     ephemeral point (128 bytes)
g      = e(Q, P2)^r                               GT element
k      = HKDF-SHA256(ikm = g, salt = U, info = "sparkdream/shield/tle/v1" || be64(epoch))
nonce  = 12 random bytes
ct     = AES-256-GCM(k, nonce, plaintext, aad = header)

encrypted_payload = 0x01 || U || nonce || ct     (header = the first 141 bytes)
```

`tools/tle.Decrypt(d, epoch, encrypted_payload)` recomputes `g = e(d, U) = e(s·Q, r·G2) = e(Q, s·G2)^r` and opens the AEAD; the epoch is in the KDF, so a payload cannot be opened under another epoch's label even with the right key. The plaintext is the `MsgShieldedExec` *fragment*: a proto `MsgShieldedExec` with only `inner_message` (field 2), `proof` (field 3, the operation proof) and `nullifier` (field 4, the operation nullifier) set (`tle.EncodeExecFragment`, byte-identical to the SDK codec, so browsers need not link it). Every other field is restored from the `PendingShieldedOp` at execution.

**Why IBE and not ECIES**: an ECIES public key for the epoch would have to be derived from the epoch *decryption* key, which only the validators' threshold can produce — a client could never compute it. With IBE the client pairs the public epoch tag with the public G2 master key; the chain pairs the reconstructed epoch key with the ciphertext's ephemeral point. (An earlier implementation shipped exactly that ECIES construction; it worked only for the test tool that held the master secret.)

**Client-side**: query `TLEMasterPublicKey` (`master_public_key_g2`) and `ShieldEpoch` (`current_epoch`); prove the operation with `RateLimitEpoch = current_epoch`; encrypt the fragment (inner message, operation proof, operation nullifier); prove the *submission* (domain `BatchSubmissionDomain`, scope `BatchSubmissionScope(payload)`, message `BatchSubmissionTypeURL`/payload, same epoch); submit with `target_epoch = current_epoch`, the submission proof and its nullifiers in cleartext. `tools/zk/wasm` exposes this as `encrypt(inputJSON)` (which returns the submission-proof inputs for a second `prove()`); `tools/zk/cmd/seed-tle payload --mpk-g2=…` builds the payload from the CLI and prints them.

### Decryption Share Verification and Key Reconstruction

Every path that admits a share — `VerifyVoteExtension`, `ProcessProposal` and the PreBlocker — applies one keeper rule, `VerifyValidatorDecryptionShare(validator, epoch, share)`:

1. the validator holds a share in the active key set;
2. `epoch < current_epoch` (closed) and `epoch >= current_epoch - max_pending_epochs`;
3. a `DecryptionDemand` exists for `epoch`;
4. `e(share, G2) == e(H1(epoch), public_share_g2_i)`.

Rule 4 proves `share = s_i·H1(epoch)` without revealing `s_i`; a random point, another validator's share, a share for another epoch and a raw master share (`s_i·G1`, which would leak every epoch) all fail it. Rules 2 and 3 keep a key from ever being assembled early, and keep future-epoch garbage out of state. A share that fails is dropped by the PreBlocker with `shield_tle_invalid_share` (validator, epoch, error) and is therefore a liveness miss; a vote extension carrying one is rejected outright, and a proposal that injects one is rejected.

Lagrange interpolation over the verified shares (`threshold = ceil(numerator/denominator × validators)`) yields `d = s·H1(epoch)`, stored as `ShieldEpochDecryptionKey` with `shield_decryption_key_available`. Because only verified shares are interpolated, one Byzantine validator cannot poison the key for everyone.

**Important**: Validators submit epoch-derived shares, NOT their raw master shares. A raw master share fails the pairing check and is dropped.

### Graceful Degradation

If the decryption key is NOT produced for a demanded epoch (insufficient validator participation):

1. Its ops stay queued and encrypted; the demand stays recorded
2. Carried-over ops **cannot** be decrypted by a different epoch's key (each epoch's key is unique)
3. Validators keep offering their share for that epoch in every vote until its liveness is judged (and, with no key yet, until the ops expire; late shares are accepted for up to `max_pending_epochs` epochs)
4. If ops hit `max_pending_epochs` without their epoch's decryption key, they are dropped with `EventShieldBatchExpired` and the demand is forgotten
5. Immediate mode operations continue working normally regardless of TLE status

## Randomness beacon

The TLE epoch key `d = s·H1(epoch)` is a threshold BLS signature by the DKG master secret over the epoch number. For a given key set and epoch there is exactly one valid value; nobody holding fewer than `threshold` shares can compute it; and once `threshold` validators have released shares anyone can, and the pairing check in every acceptance path rejects any other value. That makes it a **randomness beacon**: a public value per shield epoch that no member, block proposer, or sub-threshold validator coalition can predict before the epoch closes or bias afterwards.

**Trust assumption.** The value is deterministic in the epoch number, so a coalition holding `threshold` shares can compute it for any future epoch at any time; it cannot choose a different value (the pairing check rejects every other one), only know it early. This is inherent to a threshold-BLS beacon (drand has the same property). Every consumer therefore trusts that fewer than `threshold` key-set validators collude; a jury draw seeded from the beacon (x/rep) is unpredictable to members and proposers but not to such a coalition, which could time a review to a seed it knows. The DKG threshold is the security parameter for both batch secrecy and beacon unpredictability. Batch mode only ever reconstructed keys for epochs with a ready batch; the beacon reconstructs one for every epoch.

**Mechanics.** With `beacon_enabled` (default true) and a completed DKG (a `TLEKeySet` with a master public key), the EndBlocker records a `DecryptionDemand` for the epoch that just closed at every epoch boundary (`recordBeaconDemand`), whether or not anything was submitted to it. From there the existing share pipeline carries it unchanged: validators embed their share for the oldest demanded epoch they have not served (`NextEpochNeedingShare`; `buildDecryptionShareExtension` no longer requires `encrypted_batch_enabled`, the demand is the gate), `VerifyVoteExtension` / `ProcessProposal` / the PreBlocker apply `VerifyValidatorDecryptionShare`, `TryReconstructEpochKey` stores the value as the epoch's `ShieldEpochDecryptionKey` (event `shield_decryption_key_available` with `source=beacon`; `source=batch` when the epoch holds pending ops), and TLE liveness judges the demand one interval later like any other, so a validator that stops serving the beacon is jailed like one that stops serving batches. The value is normally available one or two blocks after the boundary.

**Validator contract.** With the beacon on, every closed epoch is a demand once a DKG has completed, batch mode or not: a key-set validator must serve decryption shares continuously from its first DKG onward, and one that stops is jailed by the liveness ring (`tle_miss_window` / `tle_miss_tolerance` / `tle_jail_duration`) even on a network that never enables encrypted batches. Before the beacon, misses were possible only in epochs that held a ready batch. Validators outside the key set (joined after the DKG, not yet re-keyed in) are not judged. `deploy/README.md` carries the operator-facing note.

**Why releasing a key for an epoch with no batch leaks nothing.** The key only opens ciphertexts sealed to its own epoch: the epoch tag is a hash-to-curve of the epoch number (`tools/tle.EpochTag`), the keys of different epochs are independent, and the master secret is not derivable from any number of epoch keys. The epoch is closed before its demand is recorded, so no later submission can target it (the ante handler refuses a batch submission whose target epoch is not the current one), and nothing else in the module or its clients uses the epoch key.

**Batch mode interaction.** With `encrypted_batch_enabled`, a closing epoch that holds pending ops is left to the batch readiness rule (`recordDecryptionDemands`), so a batch under `min_batch_size` keeps waiting encrypted for company rather than sitting decryptable; its beacon value arrives when the batch is demanded, at the latest after `max_pending_epochs`. An idle epoch is demanded at once. Batch behaviour is unchanged.

**Read API** (`x/shield/keeper/beacon.go`), for other modules through a late-wired interface and for clients through the `Beacon` query (`beacon [epoch]`, `/sparkdream/shield/v1/beacon/{epoch}`, epoch 0 = latest closed epoch; returns `status`, `key_hex`, `reconstructed_at_height`, `current_epoch`):

- `BeaconActive(ctx)`: the param is on and a DKG has completed.
- `CurrentEpoch(ctx)`, `BeaconEpochInterval(ctx)`.
- `BeaconKey(ctx, epoch) ([]byte, bool)`: the canonical G1 encoding of the epoch key.
- `BeaconStatus(ctx, epoch)`: `AVAILABLE` (stored, whatever the param says now: it is already public); `PENDING` (the epoch is open or in the future, or closed and demanded without a key yet, or holds a small batch still waiting); `UNAVAILABLE` (beacon off or no DKG; the epoch is older than `max_pending_epochs`, so pruned; closed but never demanded, e.g. before the beacon was enabled; or its demand was judged for liveness without reaching the threshold, so a consumer should not wait for a late share). A re-key (`MsgTriggerDKG`, drift) retires every key and demand, so epochs sealed to the old key set become `UNAVAILABLE`.

**Liveness and fallback contract.** The beacon is best effort: a consumer must tolerate `PENDING` for a bounded time and `UNAVAILABLE` at any time, and keep a seed of its own as the floor. Values live `max_pending_epochs` epochs (`ShieldDecryptionKeys` pruning, the same bound as batch keys), so a consumer reads the value within that window or not at all; today's only consumer reads it in the same block it is reconstructed.

**Consumers.** x/rep jury draws (`docs/x-rep-spec.md` "Jury Selection"): a review queued for a draw records `seed_epoch = CurrentEpoch + 1` when the beacon is active and the EndBlocker seats it from SHA-256 over a domain tag, `BeaconKey(seed_epoch)` and the review id once `AVAILABLE`; it falls back to the block hash after 3 epoch intervals of `PENDING` or on `UNAVAILABLE`. Wired in `app.go` as `RepKeeper.SetBeaconSource(ShieldKeeper)` (x/shield already depends on x/rep).

## EndBlocker: Batch Execution

The EndBlocker advances the shield epoch at each boundary and, when encrypted batch mode is enabled, runs the batch pipeline **every block**: a closed epoch's key arrives whenever enough shares have landed, and its batch runs in that block rather than at the next boundary. The epoch advances **whether or not** `encrypted_batch_enabled` is set: immediate mode reads it for EPOCH-scoped nullifiers and per-epoch identity rate limits, so a frozen epoch would turn those into lifetime limits. Pending ops expire whether or not batch mode is enabled, so disabling it (governance, a re-key) drains the queue instead of freezing it.

```go
func (k Keeper) EndBlocker(ctx context.Context) error {
    params := k.GetParams(ctx)
    epochState := k.GetShieldEpochState(ctx)
    currentHeight := sdkCtx.BlockHeight()
    currentEpoch := epochState.CurrentEpoch

    atBoundary := currentHeight >= epochState.EpochStartHeight + int64(params.ShieldEpochInterval)
    if atBoundary {
        currentEpoch++
        k.SetShieldEpochState(ctx, types.ShieldEpochState{CurrentEpoch: currentEpoch, EpochStartHeight: currentHeight})
    }

    if params.EncryptedBatchEnabled {
        k.judgeTLELiveness(ctx, params, currentHeight)                         // demands one interval old, before expiry forgets them
    }
    k.expirePendingOps(ctx, params, currentEpoch)                              // past max_pending_epochs; forget demands (batch on or off)
    if params.EncryptedBatchEnabled {
        k.recordDecryptionDemands(ctx, params, currentEpoch, currentHeight)    // which closed epochs are ready
        k.processReadyBatches(ctx, params, currentEpoch)                       // demanded + key present → execute
    }
    if atBoundary {
        k.pruneStaleState(ctx, currentEpoch)
    }
    return nil
}
```

**Readiness (`recordDecryptionDemands`).** Pending ops are counted per closed epoch (`target_epoch < currentEpoch`; a key-only walk of the `(target_epoch, id)` index, payloads are not decoded). An epoch without a demand gets one when it alone holds `>= min_batch_size` ops, when all closed epochs together do (they will execute in the same block and form one anonymity set), or when it has waited `max_pending_epochs`. The current epoch is never demanded: its ops are still arriving.

**Execution (`processReadyBatches` → `executeBatch`).** Each closed epoch with pending ops, a demand and a reconstructed (or genesis-seeded) key is executed, oldest first, at most `max_ops_per_batch` ops per block in total, shared across every ready epoch (the remainder run next block). Per op (`processEncryptedOp`):

1. `tle.Decrypt(epoch key, target_epoch, payload)`; failure → dropped (`decrypt_failed`)
2. Decode the fragment (`inner_message`, `proof`, the 32-byte operation `nullifier`); restore `merkle_root`, `proof_domain`, `min_trust_level` and `rate_limit_nullifier` (the one the submission proof established) from the pending op
3. Registration must exist, be active, allow batch execution (not `IMMEDIATE_ONLY`), not be ownership-mode, and `min_trust_level >= reg.min_trust_level`
4. Resolve the scope and verify the operation proof against `submitted_at_epoch` (the prover's `RateLimitEpoch`; the root was validated at submission and is not re-checked against the live tree). The proof must reproduce the submission's rate-limit nullifier, so the operation was made by the member who paid for the submission, in that epoch (`proof_failed` otherwise)
5. The operation nullifier must be unused in the permanent store (nothing about it was visible at submission); record it
6. Execute the inner message with `ProvenTrustLevel` and the exec nullifier in context; its events are re-emitted. The inner handler runs under a `max_gas_per_exec` meter in both modes, and in immediate mode what it uses is charged to the outer tx's meter afterwards. In batch mode it runs on a cache context written back only on success, so a handler that fails part-way leaves no partial state (the operation nullifier from step 5 stays recorded)

The EndBlocker has no `runTx` recovery, so `processEncryptedOp` recovers every panic (an inner handler exhausting its `max_gas_per_exec` meter, a downstream bug) into a dropped op; a payload can never halt the chain, including ops queued before governance lowered `max_gas_per_exec`.

Whatever the outcome, the pending op and its submission nullifier are deleted, and one `shield_batch_executed` event reports `batch_size`, `executed`, `dropped`, `decrypt_failed`, `proof_failed`.

### Batch Execution Order: Deterministic Shuffle

Within a batch, operations are shuffled to prevent:

1. **Validator ordering attacks**: A malicious block proposer cannot control which operation executes first to game state
2. **Submission order leakage**: If ops executed in FIFO order, an observer could correlate early-submitted ops with early-executing results
3. **Front-running**: No operation can reliably execute before another in the same batch

The shuffle uses `SHA256(last_block_hash || epoch)` as the seed for a Fisher-Yates shuffle. The last block hash is not known at submission time, making the ordering unpredictable to submitters.

**Block proposer influence**: The block proposer knows `last_block_hash` before proposing and could weakly influence it by including/excluding transactions. However, the impact is minimal — all operations in a batch execute atomically in the same block, so controlling execution order doesn't enable front-running or state manipulation. The proposer also cannot predict which operations are in the batch (they're encrypted).

### Expired Operations

Operations that hit `max_pending_epochs` without being executed (sustained TLE failure, or a key that never came) are dropped every block by `expirePendingOps`, together with their pending nullifiers and their epoch's decryption demand; one `EventShieldBatchExpired` (`count`, `cutoff_epoch`) reports them. A persistently small batch is no longer a cause: readiness forces a demand once an epoch has waited `max_pending_epochs`, so a lone op executes (with an anonymity set of one, which is the documented trade-off) rather than expiring.

## Invariants

x/crisis is not wired; `app.TestInvariants` runs every module's invariants against the e2e snapshots. x/shield registers:

- `pending-op-count`: the maintained `PendingOpCount` equals the number of stored pending ops (the queue-capacity check reads the counter);
- `pending-nullifiers`: every pending submission nullifier belongs to a pending op and every pending op's nullifier is in the pending set;
- `canonical-nullifiers`: no used or pending nullifier is a non-canonical field element.

## State Pruning

The EndBlocker performs periodic pruning to prevent unbounded state growth. Pruning runs once per epoch boundary, whether or not encrypted batch mode is enabled (the snippet is abridged; day buckets are the UTC day of the block time, the same bucket the funding ledger uses):

```go
func (k Keeper) pruneStaleState(ctx context.Context, currentEpoch uint64) {
    sdkCtx := sdk.UnwrapSDKContext(ctx)
    params := k.GetParams(ctx)

    // Guard against underflow on early chain startup.
    var cutoffEpoch uint64
    if currentEpoch > uint64(params.MaxPendingEpochs) {
        cutoffEpoch = currentEpoch - uint64(params.MaxPendingEpochs)
    }

    // 1. Prune identity rate limits from old epochs.
    //    Rate limits are keyed by (epoch, rate_limit_nullifier_hex).
    //    Entries older than currentEpoch-1 are no longer relevant.
    if currentEpoch > 1 {
        k.PruneIdentityRateLimits(ctx, currentEpoch-1)
    }

    // 2. Prune epoch-scoped UsedNullifiers.
    //    Nullifiers with NULLIFIER_SCOPE_EPOCH only need to persist for max_pending_epochs
    //    (to prevent replay of carried-over batch ops). GLOBAL nullifiers are never pruned.
    //    Keys are (domain, scope, hex), so each EPOCH-scoped domain is pruned by one
    //    range scan [(domain, 0), (domain, cutoffEpoch)) - the ever-growing
    //    MESSAGE_FIELD nullifiers are never visited.
    if cutoffEpoch > 0 {
        k.PruneEpochScopedNullifiers(ctx, cutoffEpoch)
    }

    // 3. Prune old DayFunding entries (keep current and previous day only).
    currentDay := uint64(sdkCtx.BlockTime().Unix()) / 86400
    if currentDay > 1 {
        k.PruneDayFundings(ctx, currentDay-1)
    }

    // 4. Prune old ShieldDecryptionKeys and ShieldDecryptionShares.
    //    Keys/shares older than max_pending_epochs are no longer useful
    //    (no pending ops from those epochs can remain).
    if cutoffEpoch > 0 {
        k.PruneDecryptionState(ctx, cutoffEpoch)
    }
}
```

This is called from `EndBlocker` after batch processing:

```go
// In EndBlocker, after the (batch-mode-only) batch processing:
k.pruneStaleState(ctx, newEpoch)
```

**Pruning schedule summary:**

| Collection | Pruned when | Retention |
|-----------|------------|-----------|
| `IdentityRateLimits` | Every epoch boundary | Current epoch only |
| `UsedNullifiers` (EPOCH scope) | Every epoch boundary | `max_pending_epochs` |
| `UsedNullifiers` (GLOBAL scope) | Never | Forever |
| `UsedNullifiers` (MESSAGE_FIELD scope) | Never | Forever (scoped to entity) |
| `DayFundings` | Every epoch boundary | Current + previous day |
| `ShieldDecryptionKeys` (batch keys and beacon values alike) | Every epoch boundary | `max_pending_epochs` |
| `ShieldDecryptionShares` | Every epoch boundary | `max_pending_epochs` |
| `DecryptionDemands` | Every block (`expirePendingOps`) | `max_pending_epochs` |
| `PendingOps` / `PendingNullifiers` | Every block (`expirePendingOps`), batch mode on or off | `max_pending_epochs` |
| `TLEEpochParticipation` / `TLEMissCounters` / `TLEJudgedEpochs` | On every key set change (`ResetTLELiveness`); the ring itself is bounded by `tle_miss_window` | Current key set |
| `DKGRegistrations` / `DKGContributions` / `DKGComplaints` | At every round open, failure or `MsgTriggerDKG` | Current round |
| `EpochFeeLedger` | Overwritten when a fee is paid in a new epoch (singleton) | Current epoch |

`IdentityRateLimits` and `DayFundings` are keyed by epoch / day first, so their prune passes iterate only the stale key range, like the EPOCH-scoped nullifier pass.

## Shield-Aware Module Interface

Target modules must explicitly opt in to shielded execution by implementing the `ShieldAware` interface. This creates a **double gate** — both the governance whitelist AND the module opt-in must pass:

```go
// ShieldAware is implemented by modules that accept shielded operations.
// x/shield checks this interface at execution time before dispatching the inner message.
// If the target module's msg server does not implement ShieldAware, the operation is rejected.
type ShieldAware interface {
    // IsShieldCompatible returns true if this message type is designed to accept
    // the shield module account as sender for anonymous execution.
    IsShieldCompatible(ctx context.Context, msg sdk.Msg) bool
}
```

**Why both gates are needed:**

| Gate | Prevents | Who controls |
|------|----------|-------------|
| Governance whitelist (`ShieldedOpRegistration`) | Unauthorized message types getting free gas | Governance (can be attacked via malicious proposals) |
| Module interface (`ShieldAware`) | Registered-but-incompatible operations executing | Module code (cannot be changed without chain upgrade) |

**Example**: If governance is tricked into registering `x/bank.MsgSend`, the whitelist gate passes but the interface gate rejects — the bank module doesn't implement `ShieldAware`. No funds are drained.

In immediate mode the interface gate is also consulted before the ante spends the nullifier and the reserve pays the fee: an active registration whose module is registered and rejects the message is refused with `ErrIncompatibleOperation` at no cost. (Execution checks it again; a message that does not decode, or whose module registered no gate, is refused there.) Encrypted batch ops are checked only at execution, since their inner message is sealed at submission.

**Implementation per module:**

```go
// In x/blog's msg_server.go
func (k msgServer) IsShieldCompatible(ctx context.Context, msg sdk.Msg) bool {
    switch msg.(type) {
    case *types.MsgCreatePost, *types.MsgCreateReply, *types.MsgReact:
        return true
    default:
        return false
    }
}
```

Each module lists exactly which of its message types are safe for anonymous execution. This is a compile-time declaration that cannot be changed by governance.

### Ownership Resolver (Optional)

A `ShieldAware` module whose anonymously created content can later be managed by its anonymous creator also implements `ShieldOwnershipResolver` (`x/shield/types/shield_aware.go`). x/shield type-asserts the module's `ShieldAware` implementation to it for every `NULLIFIER_MODE_OWNERSHIP` operation; a module that does not implement it fails those operations with `ErrNotOwnable`.

```go
// OwnershipClaim identifies the anonymous owner of some content: the domain and
// raw scope of the proof that created it, the resulting nullifier (the owner
// tag), and the owner's current sequence.
type OwnershipClaim struct {
    Domain   uint32
    Scope    uint64
    Tag      []byte
    Sequence uint64
}

type ShieldOwnershipResolver interface {
    // ResolveOwnership returns the ownership claim msg acts under, or an error
    // if its target is missing or not anonymously owned.
    ResolveOwnership(ctx context.Context, msg sdk.Msg) (OwnershipClaim, error)

    // AdvanceOwnership increments the claim's sequence, so the proof just
    // accepted (bound to the old sequence) can never be replayed.
    AdvanceOwnership(ctx context.Context, msg sdk.Msg) error
}
```

The claim, including its sequence, is stored by the target module alongside the content (x/collect: `Collection.anon_owner_domain/scope/tag/sequence`), so it is pruned with the content and x/shield keeps no ownership state of its own. A resolver error, or a claim with an empty tag, is wrapped as `ErrNotOwnable`. x/collect is the only module implementing it today.

### Exec Context Values

x/shield passes what the proof established to the inner message's handler through the context (`x/shield/types/exec_context.go`, `proven_trust.go`):

| Accessor | Set for | Value |
|----------|---------|-------|
| `ProvenTrustLevel(ctx)` | Every exec (immediate and batch) | The proven `min_trust_level` |
| `GetExecNullifier(ctx)` | Consume-mode execs (immediate and batch) | `ExecNullifier{Domain, Scope, Nullifier}`: the registration's domain, the resolved raw scope, and the consumed nullifier |
| `GetOwnershipTag(ctx)` | Ownership-mode execs | The owner tag the proof reproduced |

A module that lets the anonymous creator manage content later stores the `ExecNullifier` of the creating exec as the content's owner claim; before letting the shield address manage that content it compares `GetOwnershipTag(ctx)` with the stored tag. Without a matching tag in context the shield address owns nothing, so one anonymous member cannot manage another's content even though both act as the same shield account.

## Nullifier Modes

`ShieldedOpRegistration.nullifier_mode` decides what an operation's nullifier proves.

**`NULLIFIER_MODE_CONSUME` (default)** - the nullifier is a one-time action token. It must be unused under `(nullifier_domain, raw_scope)`, is recorded on execution, and the proof's Scope is `ScopeElement(nullifier_domain, raw_scope)` with the raw scope from `nullifier_scope_type`. Every operation registered before ownership mode existed is a consume op.

**`NULLIFIER_MODE_OWNERSHIP`** - the nullifier proves the submitter is the anonymous creator of the target content and is deliberately reused. For an ownership op, `validateImmediate`:

1. Decodes the inner message and resolves the module's `ShieldOwnershipResolver` (`ErrIncompatibleOperation` if no `ShieldAware` module is registered for the type, `ErrNotOwnable` if it does not resolve ownership).
2. Calls `ResolveOwnership` to get the content's `OwnershipClaim`.
3. Requires `msg.nullifier == claim.Tag` (`ErrOwnershipMismatch`). Only the creator's secret key yields the tag over the claim's domain and scope.
4. Verifies the proof with `Scope = ScopeElement(claim.Domain, claim.Scope)` and `MessageHash = ShieldOwnedMessageHash(type_url, value, claim.Sequence)`. `nullifier_scope_type` and `scope_field_path` are ignored.
5. Checks the identity rate limit as usual. The nullifier is NOT checked against or recorded in `UsedNullifiers`.

`handleImmediate` then calls `AdvanceOwnership` (sequence + 1), counts the rate limit, and runs the inner message with `WithOwnershipTag(ctx, tag)` and the proven trust level. Advancing the sequence retires the proof: the next ownership proof for the same content must bind the new sequence. In CheckTx (not simulate) `PrecheckExec` calls `AdvanceOwnership` in the check state, the ownership counterpart of the consume-mode nullifier mark, so rewrapped copies of a pending ownership exec leave the mempool.

`nullifier_domain` on an ownership registration only labels the operation (it appears in the `shielded_exec` event); nothing is recorded under it. The domain proven against is the claim's, i.e. the creation op's.

**Immediate only.** An ownership proof binds the owner sequence at execution time, which an encrypted batch submitter cannot predict. `ShieldedOpRegistration.Validate()` rejects an ownership registration whose batch mode is not `IMMEDIATE_ONLY` (`ErrOwnershipBatchMode`), and the batch executor drops any decrypted ownership op.

**Groth16 malleability.** A Groth16 proof can be re-randomized by anyone into a different valid proof for the same public inputs. Because the public inputs bind the exact inner message and the owner sequence, a mauled copy of a pending ownership exec can only run that same operation once; after it lands the sequence has advanced and every copy fails verification.

**Linkability.** Management ops on one piece of content all carry its owner tag, so they are linkable to each other and to the creation exec (whose nullifier is the tag). They are not linkable to the member or to the member's other content: the tag is a domain-separated nullifier, distinct for every (domain, scope) pair.

## Execution Flow

### MsgShieldedExec Handler

The sketch omits the format checks the handler makes first: the submitter address must decode, `nullifier` and `merkle_root` must be empty or 32 bytes and `proof` at most 1024 bytes (each `ErrInvalidProof`), and immediate mode refuses a nil `inner_message` (`ErrInvalidInnerMessage`).

```go
func (k msgServer) ShieldedExec(goCtx context.Context, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
    ctx := sdk.UnwrapSDKContext(goCtx)
    params := k.GetParams(ctx)

    if !params.Enabled {
        return nil, types.ErrShieldDisabled
    }

    // Route based on execution mode
    switch msg.ExecMode {
    case types.SHIELD_EXEC_IMMEDIATE:
        return k.handleImmediate(ctx, params, msg)
    case types.SHIELD_EXEC_ENCRYPTED_BATCH:
        return k.handleEncryptedBatch(ctx, params, msg)
    default:
        return nil, types.ErrInvalidExecMode
    }
}

// validateImmediate runs every read-only check of an immediate exec. The ante
// handler runs it too (PrecheckExec) before the module pays the fee.
func (k Keeper) validateImmediate(ctx context.Context, params types.Params, msg *types.MsgShieldedExec) (immediateExec, error) {
    // 1. Look up registered operation (must exist and be active)
    reg, found := k.GetShieldedOp(ctx, msg.InnerMessage.TypeUrl)
    if !found {
        return immediateExec{}, types.ErrUnregisteredOperation
    }
    if !reg.Active {
        return immediateExec{}, types.ErrOperationInactive
    }

    // 2. Validate batch mode allows immediate
    if reg.BatchMode == types.SHIELD_BATCH_MODE_ENCRYPTED_ONLY {
        return immediateExec{}, types.ErrImmediateNotAllowed
    }

    // 3. Validate proof domain matches registration
    if msg.ProofDomain != reg.ProofDomain {
        return immediateExec{}, types.ErrProofDomainMismatch
    }

    // 4. Validate minimum trust level meets requirement
    if msg.MinTrustLevel < reg.MinTrustLevel {
        return immediateExec{}, types.ErrInsufficientTrustLevel
    }

    // 5. Resolve what the proof is bound to, check the nullifier, verify the
    //    proof. Verification is skipped when the ante handler already verified
    //    this message in the same tx (types.WithProofVerified).
    v := immediateExec{reg: reg}
    var binding proofBinding
    if reg.NullifierMode == types.NullifierMode_NULLIFIER_MODE_OWNERSHIP {
        // Ownership: the target module supplies the owner claim; the proof
        // must reproduce its tag over the claim's domain and scope.
        owner, claim, err := k.resolveOwnership(ctx, msg) // ErrIncompatibleOperation / ErrNotOwnable
        if err != nil {
            return immediateExec{}, err
        }
        if !bytes.Equal(msg.Nullifier, claim.Tag) {
            return immediateExec{}, types.ErrOwnershipMismatch
        }
        v.owner = owner
        binding = ownedBinding(claim, msg)
    } else {
        scope, err := k.resolveNullifierScope(ctx, reg, msg)
        if err != nil {
            return immediateExec{}, err
        }
        if k.IsNullifierUsed(ctx, reg.NullifierDomain, scope, hex.EncodeToString(msg.Nullifier)) {
            return immediateExec{}, types.ErrNullifierUsed
        }
        v.scope = scope
        binding = consumeBinding(reg.NullifierDomain, scope, msg)
    }

    // 6. Identity under its rate limit - before the proof, like every other
    //    state check, so CheckTx rejects it without a pairing check.
    if k.GetIdentityRateLimitCount(ctx, hex.EncodeToString(msg.RateLimitNullifier)) >= params.MaxExecsPerIdentityPerEpoch {
        return immediateExec{}, types.ErrRateLimitExceeded
    }

    // Last, the ZK proof (verifyProof checks the merkle root first).
    if !types.ProofVerified(ctx, msg) {
        if err := k.verifyProof(ctx, msg, binding); err != nil {
            return immediateExec{}, err
        }
    }
    return v, nil
}

// handleImmediate validates the exec and executes the inner message immediately.
func (k msgServer) handleImmediate(ctx sdk.Context, params types.Params, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
    v, err := k.validateImmediate(ctx, params, msg)
    if err != nil {
        return nil, err
    }
    nullifierHex := hex.EncodeToString(msg.Nullifier)
    execCtx := types.WithProvenTrustLevel(ctx, msg.MinTrustLevel)

    if v.owner != nil {
        // 7. Ownership: the nullifier is the owner tag and is reused, so it is
        //    not recorded; advancing the owner's sequence retires this proof.
        if err := v.owner.resolver.AdvanceOwnership(ctx, v.owner.innerMsg); err != nil {
            return nil, err
        }
        execCtx = types.WithOwnershipTag(execCtx, v.owner.tag)
    } else {
        // 7. Record nullifier, and expose it to the target module (which may
        //    keep it as the owner tag of content it creates)
        k.RecordNullifier(ctx, v.reg.NullifierDomain, v.scope, nullifierHex, ctx.BlockHeight())
        execCtx = types.WithExecNullifier(execCtx, types.ExecNullifier{
            Domain: v.reg.NullifierDomain, Scope: v.scope, Nullifier: msg.Nullifier,
        })
    }

    // 8. Count against the per-identity rate limit (uses rate_limit_nullifier,
    //    NOT the operation nullifier - same for all ops by same member in same epoch)
    rateLimitHex := hex.EncodeToString(msg.RateLimitNullifier)
    if !k.CheckAndIncrementRateLimit(ctx, rateLimitHex, params.MaxExecsPerIdentityPerEpoch) {
        return nil, types.ErrRateLimitExceeded
    }

    // 9. Decode, validate signer, check ShieldAware interface, and execute inner
    //    message with the proven trust level (and exec nullifier or owner tag)
    //    in context
    resp, err := k.executeInnerMessage(execCtx, params, msg.InnerMessage)
    if err != nil {
        return nil, err
    }

    // 10. Emit event (no identity information)
    ctx.EventManager().EmitEvent(sdk.NewEvent(
        types.EventTypeShieldedExec,
        sdk.NewAttribute(types.AttributeKeyMessageType, msg.InnerMessage.TypeUrl),
        sdk.NewAttribute(types.AttributeKeyNullifierDomain, fmt.Sprintf("%d", v.reg.NullifierDomain)),
        sdk.NewAttribute(types.AttributeKeyNullifierHex, nullifierHex),
        sdk.NewAttribute(types.AttributeKeyExecMode, "immediate"),
        sdk.NewAttribute(types.AttributeKeyNullifierMode, v.reg.NullifierMode.String()),
    ))

    return &types.MsgShieldedExecResponse{InnerResponse: resp}, nil
}

// validateBatch runs every check of an encrypted-batch submission that does
// not write state; the ante handler runs it too (PrecheckExec) before the
// module pays the fee.
func (k Keeper) validateBatch(ctx context.Context, params types.Params, msg *types.MsgShieldedExec) error {
    if !params.EncryptedBatchEnabled {
        return types.ErrEncryptedBatchDisabled
    }
    // 1. A cleartext inner message would leak the operation
    if msg.InnerMessage != nil {
        return types.ErrCleartextFieldInBatchMode
    }
    // 2. Payload present and within size; 32-byte nullifiers
    if len(msg.EncryptedPayload) == 0 { return types.ErrMissingEncryptedPayload }
    if uint32(len(msg.EncryptedPayload)) > params.MaxEncryptedPayloadSize { return types.ErrPayloadTooLarge }
    if len(msg.Nullifier) != 32 || len(msg.RateLimitNullifier) != 32 { return types.ErrInvalidNullifierLength }
    // 3. Target epoch is the current one
    epochState, _ := k.GetShieldEpochStateVal(ctx)
    if msg.TargetEpoch != epochState.CurrentEpoch { return types.ErrInvalidTargetEpoch }
    // 4. Queue headroom
    if k.GetPendingOpCountVal(ctx) >= uint64(params.MaxPendingQueueSize) { return types.ErrPendingQueueFull }
    // 5. Submission nullifier not already pending
    if k.IsPendingNullifier(ctx, hex.EncodeToString(msg.Nullifier)) { return types.ErrInvalidSubmissionNullifier }
    // 6. Identity under its rate limit (read-only here; counted on success)
    if k.GetIdentityRateLimitCount(ctx, hex.EncodeToString(msg.RateLimitNullifier)) >= params.MaxExecsPerIdentityPerEpoch {
        return types.ErrRateLimitExceeded
    }
    // 7. The submission proof (merkle root checked inside), unless the ante
    //    handler already verified this message
    if !types.ProofVerified(ctx, msg) {
        return k.verifyProof(ctx, msg, batchBinding(msg))
    }
    return nil
}

// handleEncryptedBatch validates the submission and queues the encrypted payload.
func (k msgServer) handleEncryptedBatch(ctx sdk.Context, params types.Params, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
    if err := k.validateBatch(ctx, params, msg); err != nil {
        return nil, err
    }
    epochState, _ := k.GetShieldEpochStateVal(ctx)

    // Record the submission nullifier (released when the op executes or expires)
    nullifierHex := hex.EncodeToString(msg.Nullifier)
    k.RecordPendingNullifier(ctx, nullifierHex)

    // Count against the per-identity rate limit: the rate-limit nullifier was
    // just proven for the current epoch
    if !k.CheckAndIncrementRateLimit(ctx, hex.EncodeToString(msg.RateLimitNullifier), params.MaxExecsPerIdentityPerEpoch) {
        return nil, types.ErrRateLimitExceeded
    }

    // Store pending operation
    opID := k.GetNextPendingOpID(ctx)
    k.SetPendingOp(ctx, types.PendingShieldedOp{
        Id:                  opID,
        TargetEpoch:         msg.TargetEpoch,
        SubmissionNullifier: msg.Nullifier,
        MerkleRoot:          msg.MerkleRoot,
        ProofDomain:         msg.ProofDomain,
        MinTrustLevel:       msg.MinTrustLevel,
        EncryptedPayload:    msg.EncryptedPayload,
        SubmittedAtHeight:   ctx.BlockHeight(),
        SubmittedAtEpoch:    epochState.CurrentEpoch,
        RateLimitNullifier:  msg.RateLimitNullifier,
    })

    // Emit queued event (pending_op_id, target_epoch, nullifier_hex = submission nullifier, exec_mode)
    ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeShieldedQueued, ...))

    return &types.MsgShieldedExecResponse{PendingOpId: opID}, nil
}
```

### Nullifier Scope Resolution

The scope determines the granularity of nullifier uniqueness:

```go
func (k Keeper) resolveNullifierScope(ctx context.Context, reg types.ShieldedOpRegistration, msg *types.MsgShieldedExec) (uint64, error) {
    switch reg.NullifierScopeType {
    case types.NULLIFIER_SCOPE_EPOCH:
        // current_epoch / epoch_window: one action per member per window
        return reg.EpochScope(k.GetCurrentEpoch(ctx)), nil
    case types.NULLIFIER_SCOPE_MESSAGE_FIELD:
        // Extract scope from inner message using the registered field path
        if msg.InnerMessage != nil && reg.ScopeFieldPath != "" {
            if val, ok := k.extractScope(msg.InnerMessage, reg.ScopeFieldPath); ok {
                return val, nil
            }
        }
        // No fallback to the epoch scope: that would let one identity act
        // once per epoch on something meant to be once per target.
        return 0, errorsmod.Wrapf(types.ErrInvalidInnerMessage, "cannot read nullifier scope field %q", reg.ScopeFieldPath)
    case types.NULLIFIER_SCOPE_GLOBAL:
        return 0, nil
    default:
        return k.GetCurrentEpoch(ctx), nil
    }
}
```

**Epoch windows**: an EPOCH-scoped op's raw scope is `current_epoch / epoch_window` (`ShieldedOpRegistration.EpochScope`; a window of 0 or 1 means every epoch), so a member gets one action per window of `epoch_window` shield epochs. This is the rate limit for anonymous creation, which carries no SPARK charge (the shield reserve, not the member, would pay it). Clients compute the same window index for the proof's scope. Defaults (at the default 50-block shield epoch): blog posts 12 (~1 hour), forum posts and replies 3 (~15 minutes), collections 288 (~1 day); other EPOCH ops 1. `epoch_window > 1` is rejected for MESSAGE_FIELD/GLOBAL scopes and ownership ops. EPOCH ops sharing a nullifier domain should share a window; pruning cuts each domain at `cutoff_epoch / largest_window`, so a mismatch never prunes a live window.

**Scope field extraction**: When `nullifier_scope_type = MESSAGE_FIELD`, the `ShieldedOpRegistration` includes a `scope_field_path` (e.g., `"post_id"`, `"proposal_id"`, `"collection_id"`). The keeper uses reflection to extract the named integer field from the decoded inner message. A two-field path joined by `types.ScopeFieldSeparator` (`"target_type,target_id"`) is packed with `types.PackNullifierScope` as `(qualifier << 56) | id`. A fallback path joined by `types.ScopeFieldFallback` (`"reply_id|post_id"`) uses the first field when it is non-zero, packed as `(1 << 56) | first`, and the bare second field otherwise - so a blog reaction on a reply has its own scope and doesn't spend the post's. If a field is missing, not an integer, or does not fit the packing, the operation is rejected with `ErrInvalidInnerMessage` - there is no fallback to the epoch scope.

`resolveNullifierScope` returns the **raw** scope: it keys the `UsedNullifiers` store and is never passed to the verifier directly. The proof's Scope public input is always `crypto.ScopeElement(reg.NullifierDomain, rawScope)` (`consumeBinding`). Ownership-mode ops skip this function; their scope comes from the target module's `OwnershipClaim`.

For encrypted batch mode, the scope cannot be extracted at submission time (inner message is encrypted). The scope check is deferred to batch execution time in `processEncryptedOp`, where the decrypted inner message is available.

## Keeper Interface

### Required Keeper Dependencies

x/shield uses a "late keepers" pattern to break depinject cycles. The `accountKeeper` and `bankKeeper` are injected via depinject. All other keepers (`repKeeper`, `distrKeeper`, `slashingKeeper`, `stakingKeeper`, `identityKeeper`, `router`) are wired post-depinject via `Set*Keeper()` methods. This is tracked via a shared `lateKeepers` pointer so value-copies of Keeper (in AppModule, msgServer) see updates.

```go
type lateKeepers struct {
    repKeeper      types.RepKeeper      // Trust tree Merkle root snapshots
    distrKeeper    types.DistrKeeper    // Community pool funding
    slashingKeeper types.SlashingKeeper // TLE liveness jailing
    stakingKeeper  types.StakingKeeper  // Validator set iteration
    identityKeeper types.IdentityKeeper // Bond denom (shield panics on first use if unwired)
    router         baseapp.MessageRouter
    shieldAwareModules map[string]types.ShieldAware  // module opt-in registry
}

type Keeper struct {
    storeService  corestore.KVStoreService
    cdc           codec.Codec
    addressCodec  address.Codec
    authority     []byte
    accountKeeper types.AccountKeeper
    bankKeeper    types.BankKeeper
    late          *lateKeepers  // shared pointer — late-wired dependencies

    // Collections — General
    Params              collections.Item[types.Params]
    ShieldedOps         collections.Map[string, types.ShieldedOpRegistration]  // key: type_url
    UsedNullifiers      collections.Map[collections.Triple[uint32, uint64, string], types.UsedNullifier]
    PendingNullifiers   collections.Map[string, bool]                          // key: nullifier_hex
    DayFundings         collections.Map[uint64, types.DayFunding]
    IdentityRateLimits  collections.Map[collections.Pair[uint64, string], uint64]  // key: (epoch, nullifier_hex) → count

    // Collections — ZK proof verification
    VerificationKeys    collections.Map[string, types.VerificationKey]  // key: circuit_id

    // Collections — TLE (owned by x/shield)
    TLEKeySet             collections.Item[types.TLEKeySet]
    TLEMissCounters       collections.Map[string, uint64]  // key: validator_address → misses in the ring (queryable)
    TLEEpochParticipation collections.Map[collections.Pair[string, uint64], bool]  // key: (validator, slot) ring of judged epochs
    TLEJudgedEpochs       collections.Item[uint64]         // judged-epoch counter under the current key set (slot = count mod window)

    // Collections — Encrypted batch mode
    PendingOps           collections.Map[collections.Pair[uint64, uint64], types.PendingShieldedOp]  // key: (target_epoch, op_id)
    PendingOpCount       collections.Item[uint64]          // maintained by Set/DeletePendingOp
    NextPendingOpId      collections.Sequence
    ShieldEpochState     collections.Item[types.ShieldEpochState]
    ShieldDecryptionKeys collections.Map[uint64, types.ShieldEpochDecryptionKey]  // key: epoch
    ShieldDecryptionShares collections.Map[collections.Pair[uint64, string], types.ShieldDecryptionShare]  // key: (epoch, validator)
    DecryptionDemands    collections.Map[uint64, int64]  // key: epoch → demand height (negative once liveness judged)

    // Collections — DKG ceremony
    DKGState             collections.Item[types.DKGState]
    DKGContributions     collections.Map[string, types.DKGContribution]   // key: validator_address
    DKGRegistrations     collections.Map[string, types.DKGContribution]   // key: validator_address
    DKGComplaints        collections.Map[collections.Pair[string, string], types.DKGComplaint]  // key: (dealer, complainer)
}
```

### Expected Keeper Interfaces

```go
type AccountKeeper interface {
    GetModuleAddress(moduleName string) sdk.AccAddress
    GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
    GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI // used by simulation
    NewAccountWithAddress(ctx context.Context, addr sdk.AccAddress) sdk.AccountI // public submitter at genesis
    SetAccount(ctx context.Context, acc sdk.AccountI)
}

type BankKeeper interface {
    GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
    SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
    SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

type DistrKeeper interface {
    DistributeFromFeePool(ctx context.Context, amount sdk.Coins, receiveAddr sdk.AccAddress) error
    GetCommunityPool(ctx context.Context) (sdk.DecCoins, error) // funding is skipped when the pool holds less than the top-up
}

type IdentityKeeper interface {
    IsIdentityKeeper()
    BondDenom(ctx context.Context) string
}

type RepKeeper interface {
    // Trust tree Merkle root snapshots for ZK proof root validation
    GetTrustTreeRoot(ctx context.Context) ([]byte, error)
    GetPreviousTrustTreeRoot(ctx context.Context) ([]byte, error)
}

type SlashingKeeper interface {
    Jail(ctx context.Context, consAddr sdk.ConsAddress) error
    JailUntil(ctx context.Context, consAddr sdk.ConsAddress, jailTime time.Time) error
}

type StakingKeeper interface {
    GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
    GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error)
    GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error)
}
```

**Note**: x/shield is a leaf dependency — nothing depends on x/shield. RepKeeper provides trust tree Merkle roots. SlashingKeeper and StakingKeeper are needed for TLE liveness jailing. DistrKeeper provides community pool funding. All late-wired keepers use `Set*Keeper()` methods on the Keeper (see app.go wiring).

## Registration: Shielded Operations

The list of permitted shielded operations is a **governance-controlled whitelist** via `MsgRegisterShieldedOp`. Only explicitly registered message types can be executed through `MsgShieldedExec`. Unregistered message types (e.g. `x/bank.MsgSend`, `x/staking.MsgDelegate`) are rejected — this prevents abuse of gas-free execution for non-privacy operations.

### Default Operations (Genesis)

The genesis set covers all existing anonymous functionality on the chain: 21 operations (13 consume-mode, 8 ownership-mode collect management ops). `GenesisState.Validate()` runs `ShieldedOpRegistration.Validate()` on every entry. Every row below is `NULLIFIER_MODE_CONSUME` unless its table says otherwise.

Content ops (blog, forum, collect) register `min_trust_level = 0`: the proof shows membership and nothing else, so every member is in the anonymity set and holders of rarer trust levels are not singled out. Where the target itself asks for more (a post's `min_reply_trust_level`, collect's curator or sponsor gates), the client proves that level for that action only.

#### x/blog

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgCreatePost` | TRUST_TREE | 0 | 1 | EPOCH (window 12) | EITHER |
| `MsgCreateReply` | TRUST_TREE | 0 | 2 | MESSAGE_FIELD (post_id) | EITHER |
| `MsgReact` | TRUST_TREE | 0 | 8 | MESSAGE_FIELD (reply_id\|post_id) | EITHER |

#### x/forum

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgCreatePost` | TRUST_TREE | 0 | 11 | EPOCH (window 3) | EITHER |
| `MsgUpvotePost` | TRUST_TREE | 0 | 12 | MESSAGE_FIELD (post_id) | EITHER |
| `MsgDownvotePost` | TRUST_TREE | 0 | 12 | MESSAGE_FIELD (post_id) | EITHER |

#### x/collect

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgCreateCollection` | TRUST_TREE | 0 | 21 | EPOCH (window 288) | EITHER |
| `MsgUpvoteContent` | TRUST_TREE | 0 | 22 | MESSAGE_FIELD (target_type,target_id) | EITHER |
| `MsgDownvoteContent` | TRUST_TREE | 0 | 22 | MESSAGE_FIELD (target_type,target_id) | EITHER |

Upvotes and downvotes share a nullifier domain (forum 12, collect 22): one anonymous vote per member per target, either direction, matching the modules' own per-voter record. A two-field scope path packs the first field into the top byte: raw scope = `(target_type << 56) | target_id` (`types.PackNullifierScope`; ids must fit in 56 bits). Clients pass that value as the prover's raw `Scope` (the prover hashes it with the domain into the scope element). Without the qualifier, a vote on collection 5 would block a vote on item 5.

#### x/collect (Anonymous Collection Management - Ownership Mode)

| Message Type | Proof Domain | Min Trust | Null. Domain | Nullifier Mode | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgUpdateCollection` | TRUST_TREE | 0 | 23 (label) | OWNERSHIP | IMMEDIATE_ONLY |
| `MsgDeleteCollection` | TRUST_TREE | 0 | 23 (label) | OWNERSHIP | IMMEDIATE_ONLY |
| `MsgAddItem` | TRUST_TREE | 0 | 23 (label) | OWNERSHIP | IMMEDIATE_ONLY |
| `MsgAddItems` | TRUST_TREE | 0 | 23 (label) | OWNERSHIP | IMMEDIATE_ONLY |
| `MsgUpdateItem` | TRUST_TREE | 0 | 23 (label) | OWNERSHIP | IMMEDIATE_ONLY |
| `MsgRemoveItem` | TRUST_TREE | 0 | 23 (label) | OWNERSHIP | IMMEDIATE_ONLY |
| `MsgRemoveItems` | TRUST_TREE | 0 | 23 (label) | OWNERSHIP | IMMEDIATE_ONLY |
| `MsgReorderItem` | TRUST_TREE | 0 | 23 (label) | OWNERSHIP | IMMEDIATE_ONLY |

These let the anonymous creator of a collection manage it (see [Nullifier Modes](#nullifier-modes)). The proof is made over the collection's owner claim - `anon_owner_domain` (21, the `MsgCreateCollection` domain) and `anon_owner_scope` (the creation epoch) - so its nullifier reproduces `anon_owner_tag`, and it binds `anon_owner_sequence`. Domain 23 only labels the ops; nothing is recorded under it, and they have no scope type or scope field. x/collect's `ShieldOwnershipResolver` resolves the collection from the message id (item ops resolve it from the item; all `MsgRemoveItems` ids must be in one collection).

#### x/commons (Anonymous Governance)

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgSubmitAnonymousProposal` | TRUST_TREE | 0 | 31 | EPOCH | EITHER |
| `MsgAnonymousVoteProposal` | GROUP_TREE | 0 | 32 | MESSAGE_FIELD (proposal_id) | IMMEDIATE_ONLY |
| `MsgAnonymousVotePoll` | GROUP_TREE | 0 | 33 | MESSAGE_FIELD (poll_id) | IMMEDIATE_ONLY |

The two votes are secret-ballot votes: the proof shows membership of the proposal's or poll's voter snapshot, whose root x/commons names (a trust-tree proof is refused with a domain mismatch), and the nullifier lets each member vote once per proposal or poll. x/commons refuses an anonymous vote on an openly voted proposal or poll and an open vote on a secret one, so no member can vote both ways. They are immediate-only because their root is resolved from the inner message (see [Merkle Root Validation](#merkle-root-validation)).

> **Note**: `MsgSubmitAnonymousProposal`'s batch mode is EITHER so it works in both immediate and encrypted batch modes. Immediate mode is needed while TLE/DKG is not yet active (`encrypted_batch_enabled=false`). Once TLE is production-ready, governance can tighten these to ENCRYPTED_ONLY for maximum sender unlinkability.

#### x/rep

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgCreateChallenge` | TRUST_TREE | 0 | 41 | GLOBAL | ENCRYPTED_ONLY (registered **inactive**) |

The rep challenge op is registered but inactive. x/rep refuses a challenge whose creator is the shield module until it can derive the challenger from the member's identity (`TestShieldedChallengePathFailsClosed`), so an active registration would only let members pay for batch submissions that are always dropped. Governance activates it (`MsgRegisterShieldedOp` with `active = true`) once x/rep supports the flow.

#### x/federation (Anonymous Arbiter Quorum)

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgSubmitArbiterHash` | TRUST_TREE | 2 | 51 | MESSAGE_FIELD (content_id) | EITHER |

Scoped per `content_id` so one identity cannot cast several arbiter votes for the same federated content.

### Governance Control

- **Add operations**: Register new message types via `MsgRegisterShieldedOp` governance proposal
- **Deactivate operations**: Set `active = false` to disable without removing state
- **Remove operations**: Fully delete via `MsgDeregisterShieldedOp` governance proposal
- **Modify parameters**: Change proof domain, trust level, batch mode, nullifier scoping or nullifier mode per operation (an OWNERSHIP registration must stay IMMEDIATE_ONLY)
- **Whitelist only**: Any message type NOT in the registry is rejected — no gas-free execution of token transfers, staking, IBC, or other non-privacy operations

### Module Account Constraint

**Governance can register any message type, but only shield-aware operations will actually work.** This is because every inner message has its signer set to the shield module account address. The target module sees the shield module account as the sender — not the anonymous user.

An operation works through x/shield only if:

1. **No asset ownership required** — the operation creates content or casts a vote, it doesn't move tokens from the sender's balance
2. **The target module accepts the shield module account as sender** — the handler has explicit logic for anonymous/module-account senders
3. **Identity comes from the ZK proof, not the sender address** — eligibility, trust level, and voting power are proven by the circuit
4. **The operation is semantically meaningful without a known actor** — an anonymous post makes sense; an anonymous token transfer does not (the shield module account has no user funds)

**Examples of operations that would NOT work even if registered:**

| Operation | Why it fails |
|-----------|-------------|
| `x/bank.MsgSend` | Sends from shield module's gas reserve, not the user's funds. Drains gas. |
| `x/staking.MsgDelegate` | Delegates the module's gas reserve. User's SPARK is untouched. |
| `x/gov.MsgVote` | Module accounts have no staking weight. Vote counts for nothing. |
| `x/commons.MsgVoteProposal` | Shield module account is not a council member. Rejected. |
| `x/authz.MsgExec` | No authz grants exist for the shield module address. Fails. |
| `ibc.MsgTransfer` | Would drain shield module funds off-chain via IBC. |

**To add new shield-compatible operations**, the target module must:
1. Implement the `ShieldAware` interface on its msg server (see [Shield-Aware Module Interface](#shield-aware-module-interface))
2. Return `true` from `IsShieldCompatible` for the specific message type
3. Handle the shield module account as a valid sender in the message handler
4. Derive authorization from the ZK proof context, not the sender address
5. Not require the sender to own or transfer assets

**Proven trust level.** Both exec paths (immediate and encrypted batch) run the inner message with `types.WithProvenTrustLevel(ctx, min_trust_level)` - the level the ZK proof established. When the signer is the shield module account, target modules (blog, collect, forum) compare their own trust gates against `types.ProvenTrustLevel(ctx)` instead of looking up a member. A message from the shield address with no proven level in its context did not come through a shield exec, so nothing is known about the signer: blog and collect refuse it, and forum treats it as the lowest trust level.

Even if governance registers a message type, x/shield rejects it at execution time unless the target module explicitly opts in via `ShieldAware`. This is defense in depth — a malicious governance proposal cannot compromise non-privacy modules.

**Rationale for batch mode assignments:**
- **Content operations (blog, forum, collect)**: `EITHER` — users choose latency vs privacy. Most content doesn't need batch delay, but whistleblower content benefits from maximum privacy.
- **Governance operations (commons proposals/votes)**: `EITHER` — immediate mode is required while TLE/DKG is not yet active. Once TLE is production-ready, governance can tighten to `ENCRYPTED_ONLY` for maximum sender unlinkability.
- **Challenges (rep)**: `ENCRYPTED_ONLY` — challenges are inherently privacy-critical. The batch delay (one shield epoch, ~5 min) is acceptable for challenge submissions.
- **Anonymous collection management (collect ownership ops)**: `IMMEDIATE_ONLY` - required for ownership mode: the proof binds the owner sequence at execution time, which a batch submitter cannot predict.

## Events

```protobuf
// --- Immediate mode events ---

// Emitted on every immediate shielded execution
EventShieldedExec {
  message_type:     string  // inner message type URL
  nullifier_domain: uint32
  nullifier_hex:    string  // for public nullifier tracking (ownership mode: the owner tag)
  exec_mode:        string  // "immediate"
  nullifier_mode:   string  // "NULLIFIER_MODE_CONSUME" or "NULLIFIER_MODE_OWNERSHIP"
  // NOTE: no submitter address, no identity information
}

// --- Encrypted batch mode events ---

// Emitted when an encrypted op is queued
EventShieldedQueued {
  pending_op_id: uint64
  target_epoch:  uint64
  nullifier_hex: string
  exec_mode:     string  // "encrypted_batch"
}

// Emitted when a batch is decrypted and executed
EventShieldBatchExecuted {
  epoch:          uint64
  batch_size:     uint32   // total ops in batch
  executed:       uint32   // successfully executed
  dropped:        uint32   // decrypt_failed + proof_failed + other (decode, unregistered, used nullifier, inner error)
  decrypt_failed: uint32
  proof_failed:   uint32
}

// Emitted when batch execution is skipped (empty decryption key)
EventShieldBatchSkipped {
  epoch: uint64
  error: string
}

// Emitted when pending ops expire without execution (cutoff_epoch), or are
// dropped because their key set was retired by MsgTriggerDKG / drift
// re-keying (dkg_round)
EventShieldBatchExpired {
  count:        uint32  // number of dropped ops
  cutoff_epoch: uint64  // expiry path
  dkg_round:    uint64  // re-key path
}

// Emitted when shield epoch decryption key is reconstructed
EventShieldDecryptionKeyAvailable {
  epoch:                    uint64
  shares_submitted:         uint32
  threshold_required:       uint32
  source:                   string   // "batch" (the epoch holds pending ops) or "beacon"
}

// Emitted when decryption key reconstruction fails
EventShieldDecryptionKeyFailed {
  epoch: uint64
  error: string
}

// --- Registration events ---

// Emitted when a shielded operation is registered via governance
EventShieldedOpRegistered {
  message_type: string
}

// Emitted when a shielded operation is deregistered via governance
EventShieldedOpDeregistered {
  message_type: string
}

// --- Funding events ---

// Emitted when module is funded from community pool
EventShieldFunded {
  amount:      string  // uspark amount funded
  day:         uint64
  new_balance: string  // module account balance after funding
}

// Emitted when daily funding cap is reached
EventShieldFundingCapReached {
  day:          uint64
  total_funded: string
  cap:          string
}

// --- DKG lifecycle events ---

// Emitted when a round opens (auto-trigger or MsgTriggerDKG, count =
// participants) and on every phase transition (dkg_phase = the new phase;
// CONTRIBUTING carries the narrowed participant count, JUSTIFYING the
// number of complaints; count is absent on the COMPLAINING transition)
EventShieldDKGOpened {
  dkg_round: uint64
  dkg_phase: string
  count:     uint64
}

// Emitted when a validator's complaint against a dealer is recorded
EventShieldDKGComplaint {
  dealer:     string
  complainer: string
  dkg_round:  uint64
}

// Emitted when a dealer answers a complaint with a valid evaluation
EventShieldDKGJustification {
  dealer:     string
  complainer: string
  dkg_round:  uint64
}

// Emitted at the justification deadline for each dealer excluded from the key set
EventShieldDKGDisqualified {
  dealer:    string
  dkg_round: uint64
}

// Emitted when a validator's registration is processed from vote extensions
EventShieldDKGRegistration {
  validator: string
  dkg_round: uint64
}

// Emitted when a validator's DKG contribution is processed from vote extensions
EventShieldDKGContribution {
  validator:          string
  dkg_round:         uint64
  contributions_count: uint64
}

// Emitted when the DKG ceremony completes (master public key computed)
EventShieldDKGComplete {
  dkg_round:           uint64
  contributions_count: uint64  // qualified contributions aggregated
}

// Emitted when the DKG transitions to ACTIVE phase
EventShieldDKGActivated {
  dkg_round: uint64
}

// Emitted when DKG fails (insufficient contributions, timeout)
EventShieldDKGFailed {
  dkg_round: uint64
  error:     string
}

// Emitted when validator set drift exceeds max_validator_set_drift threshold
EventShieldValidatorDrift {
  drift_percent: uint32
  dkg_round:     uint64
}

// --- TLE liveness events ---

// Emitted when a bonded key-set validator has no stored share for a judged epoch
EventTLEMiss {
  validator: string
  epoch:     uint64
}

// Emitted when a validator is jailed for exceeding TLE miss tolerance
EventTLEJail {
  validator:     string
  jail_duration: string
}

// Emitted by the PreBlocker when a validator's decryption share fails
// verification (unknown validator, epoch not closed/demanded/in window, or
// the pairing check). The share is dropped and counts as a miss.
EventTLEInvalidShare {
  validator: string
  epoch:     uint64
  error:     string
}
```

## Errors

```go
var (
    // General
    ErrInvalidSigner             = errorsmod.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
    ErrShieldDisabled            = errorsmod.Register(ModuleName, 2, "shielded execution is disabled")
    ErrShieldGasDepleted         = errorsmod.Register(ModuleName, 3, "shield module gas reserve depleted")
    ErrUnregisteredOperation     = errorsmod.Register(ModuleName, 4, "inner message type not registered for shielded execution")
    ErrOperationInactive         = errorsmod.Register(ModuleName, 5, "shielded operation is currently inactive")
    ErrProofDomainMismatch       = errorsmod.Register(ModuleName, 6, "proof domain does not match registered requirement")
    ErrInsufficientTrustLevel    = errorsmod.Register(ModuleName, 7, "proven trust level below required minimum")
    ErrInvalidProof              = errorsmod.Register(ModuleName, 8, "ZK proof verification failed")
    ErrInvalidProofDomain        = errorsmod.Register(ModuleName, 9, "unknown proof domain")
    ErrNullifierUsed             = errorsmod.Register(ModuleName, 10, "nullifier already used in this domain and scope")
    ErrRateLimitExceeded         = errorsmod.Register(ModuleName, 11, "per-identity rate limit exceeded for this epoch")
    ErrInvalidInnerMessage       = errorsmod.Register(ModuleName, 12, "inner message is invalid or cannot be decoded")
    ErrInvalidInnerMessageSigner = errorsmod.Register(ModuleName, 13, "inner message signer must be shield module account")
    ErrMultiMsgNotAllowed        = errorsmod.Register(ModuleName, 14, "MsgShieldedExec must be the only message in the transaction")

    // Execution mode
    ErrInvalidExecMode          = errorsmod.Register(ModuleName, 15, "invalid execution mode")
    ErrImmediateNotAllowed      = errorsmod.Register(ModuleName, 16, "this operation requires encrypted batch mode")
    ErrEncryptedBatchDisabled   = errorsmod.Register(ModuleName, 17, "encrypted batch mode is disabled")

    // Encrypted batch
    ErrMissingEncryptedPayload = errorsmod.Register(ModuleName, 19, "encrypted_payload is required for encrypted batch mode")
    ErrPayloadTooLarge         = errorsmod.Register(ModuleName, 20, "encrypted_payload exceeds max_encrypted_payload_size")
    ErrInvalidTargetEpoch      = errorsmod.Register(ModuleName, 21, "target_epoch must be the current shield epoch")
    ErrPendingQueueFull        = errorsmod.Register(ModuleName, 22, "pending operation queue is at capacity")
    ErrInvalidMerkleRoot       = errorsmod.Register(ModuleName, 24, "merkle root is not current or previous")

    // Validator TLE / DKG
    ErrInvalidDecryptionShare    = errorsmod.Register(ModuleName, 25, "decryption share verification failed")
    ErrIncompatibleOperation     = errorsmod.Register(ModuleName, 32, "target module does not implement ShieldAware or rejects this message type")
    ErrDKGInProgress             = errorsmod.Register(ModuleName, 33, "DKG ceremony already in progress")
    ErrCleartextFieldInBatchMode = errorsmod.Register(ModuleName, 34, "inner_message must be empty in encrypted batch mode")
    ErrInsufficientValidators    = errorsmod.Register(ModuleName, 41, "not enough bonded validators for DKG")
    ErrNoVerificationKey         = errorsmod.Register(ModuleName, 43, "no verification key registered; shielded execution requires a VK to be stored before use")

    // Input validation
    ErrInvalidNullifierLength = errorsmod.Register(ModuleName, 44, "nullifier must be exactly 32 bytes")
    ErrFeeTooHigh             = errorsmod.Register(ModuleName, 47, "shielded exec fee exceeds max_fee_per_exec")

    // Ownership mode
    ErrNotOwnable         = errorsmod.Register(ModuleName, 48, "target module cannot resolve ownership for this operation")
    ErrOwnershipMismatch  = errorsmod.Register(ModuleName, 49, "proof nullifier does not match the content's owner tag")
    ErrOwnershipBatchMode = errorsmod.Register(ModuleName, 50, "ownership operations are immediate-only")

    // Mempool guard (node-local, CheckTx only)
    ErrProofVerificationThrottled = errorsmod.Register(ModuleName, 51, "node is at its proof verification rate limit; retry shortly")

    // Encrypted batch submission proof
    ErrInvalidSubmissionNullifier = errorsmod.Register(ModuleName, 52, "submission nullifier already pending for this epoch")

    // DKG trigger
    ErrInvalidDKGThreshold = errorsmod.Register(ModuleName, 53, "DKG threshold must satisfy 0 < numerator <= denominator <= 100")

    // Public inputs, epoch fee budget, registrations, public submitter
    ErrNonCanonicalPublicInput  = errorsmod.Register(ModuleName, 54, "public input is not a canonical BN254 scalar field element")
    ErrEpochFeeBudgetExhausted  = errorsmod.Register(ModuleName, 55, "shield epoch fee budget (max_fees_per_epoch) exhausted; retry next shield epoch")
    ErrNullifierDomainConflict  = errorsmod.Register(ModuleName, 56, "nullifier domain is already used by an operation with a different scope type, scope field or window")
    ErrOrderedPublicSubmitterTx = errorsmod.Register(ModuleName, 57, "txs signed by the public anonymous submitter must be unordered")
    ErrIdentityFeeShareExhausted = errorsmod.Register(ModuleName, 58, "this identity's share of the shield epoch fee budget is used up; retry next shield epoch")
)
```

Codes are never reused; the gaps (18, 23, 26-31, 35-40, 42, 45, 46) are codes retired because nothing returned them. Vote-extension and proposal rejections carry plain errors (they are not tx results); a decryption share that fails any rule is reported as `ErrInvalidDecryptionShare`.

## Migration Plan

### Phase 1: Immediate Mode + ZK Verification

Implement x/shield with immediate mode and native ZK proof verification:
- Ante handler (ShieldGasDecorator)
- `MsgShieldedExec` handler (immediate path)
- Native ZK proof verification (verification keys stored in x/shield)
- Nullifier store, registration store
- Auto-funding BeginBlocker with epoch cap
- Per-identity rate limiting
- `MsgRegisterShieldedOp` for governance
- Register all existing anonymous operations

### Phase 2: TLE + Encrypted Batch Mode (COMPLETE 2026-10-16; disabled by default — governance enables it after a network's DKG has run, or sets `enable_batch_after_dkg`)

Add TLE infrastructure and encrypted batch execution:
- DKG ceremony for validator key generation via CometBFT vote extensions (stored in x/shield)
- TLE master public key + validator public shares on-chain
- Shield epoch state machine
- Vote extension handlers: `ExtendVote` / `VerifyVoteExtension` for DKG registration, contribution, and decryption shares
- `PrepareProposalHandler` aggregates vote extensions into `InjectedDKGData` pseudo-transactions
- `PreBlocker` processes aggregated DKG data (registrations, contributions, decryption shares)
- Decryption key reconstruction (native Lagrange interpolation)
- TLE liveness enforcement (miss window/tolerance/jailing)
- Pending operation queue
- EndBlocker batch processing (decrypt, verify, shuffle, execute)
- `min_batch_size` / `max_pending_epochs` logic
- Register governance operations (initially as `EITHER`, tighten to `ENCRYPTED_ONLY` when TLE is stable)

### Phase 3: x/commons Anonymous Governance

Add anonymous proposal and voting to x/commons (absorbing former x/vote functionality):
- `MsgSubmitAnonymousProposal` — shielded proposal creation, routed through `MsgShieldedExec` (EITHER batch mode — works in immediate mode while TLE is inactive)
- `MsgAnonymousVoteProposal` / `MsgAnonymousVotePoll` — secret-ballot voting against the ballot's voter snapshot (`PROOF_DOMAIN_GROUP_TREE`, see x-commons-spec.md §11.2)
- Standard (non-anonymous) proposals/votes continue working as before

### Phase 4: Dual Support

- Both old per-module anonymous messages and `MsgShieldedExec` work simultaneously
- Client tooling updated to prefer `MsgShieldedExec`
- Old anonymous messages log deprecation warnings

### Phase 5: Remove Legacy Anonymous Messages (COMPLETE)

Legacy per-module anonymous messages have been removed:
- Removed `MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, `MsgAnonymousReact` from x/blog
- Removed `MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, `MsgAnonymousReact` from x/forum
- Removed `MsgCreateAnonymousCollection`, `MsgManageAnonymousCollection`, `MsgAnonymousReact` from x/collect
- Removed per-module nullifier stores and anon keeper files (centralized in x/shield)
- Removed relayer subsidy logic from x/blog
- x/vote module fully eliminated (ZK + TLE in x/shield, voting in x/commons)
- Each module now has a `shield_aware.go` implementing the `ShieldAware` interface

## Security Considerations

### Gas Abuse Prevention

Multiple layers prevent free gas exploitation:

1. **ZK proof required**: Every shielded exec needs a valid membership proof — no proof, no free gas. In both modes the ante handler checks a proof, the nullifier and the identity rate limit (`PrecheckExec`) before any fee moves — the operation proof for an immediate exec, the submission proof for a batch exec — and the msg server skips re-verifying a proof the ante handler already verified for the same message. In CheckTx the precheck also marks the exec spent in the check state (discarded at commit) - it records a consume-mode nullifier, advances an ownership-mode owner sequence, or records a pending submission nullifier - so rewrapped copies of a pending exec — anyone can sign as the public submitter — never enter the mempool. In DeliverTx the ante spends an immediate exec (nullifier and rate-limit slot) in the state that commits with the fee, so a failing inner message still costs the exec and cannot be replayed at the reserve's expense
2. **Per-identity rate limit**: `max_execs_per_identity_per_epoch` caps how many free operations each anonymous identity gets per epoch, enforced via the ZK-proven `rate_limit_nullifier` (same for all ops by the same member in the same epoch). Unforgeable in both modes: an immediate exec proves it with the operation proof, a batch exec with the cleartext submission proof, and both are verified before payment
3. **Single-message enforcement**: Cannot bundle non-shielded messages with a shielded exec to piggyback free gas. Multi-message txs containing `MsgShieldedExec` are explicitly rejected by the ante handler.
4. **Max gas per exec**: `max_gas_per_exec` prevents expensive inner messages from draining the module quickly
5. **Daily funding cap**: `max_funding_per_day` bounds community pool drain regardless of usage (independent of shield epoch interval)
6. **Registered operations only**: Only governance-approved message types can be executed via shield (whitelist model)
7. **Fee cap**: the submitter picks the fee but the module pays it, so `max_fee_per_exec` caps it (bond denom only); without it one exec could hand the whole reserve to the fee collector. In CheckTx the fee must also meet the node's minimum gas prices for the tx's gas limit, as for any other tx
7b. **Epoch fee budget**: `max_fees_per_epoch` caps the fees the module pays per shield epoch across all identities, so in-quota successful execs can't drain the reserve faster than it is funded; one identity may use at most a tenth of it, and a fifth is reserved for group-tree (secret-ballot) execs; the unused part of a successful exec's fee (gas used against gas declared) is refunded to the module and unbooked by the post handler, so both budgets measure gas used
7c. **Canonical public inputs**: non-canonical nullifier, rate-limit nullifier or root encodings are refused, so one proof has exactly one nullifier
8. **No per-action charges on anonymous actions**: inner messages are signed by the shield module account, so target modules skip every per-action SPARK cost for it (storage fees, spam taxes, edit fees, blog reaction fee, downvote costs, collection/item deposits) — charging them would spend the gas reserve, not the member's SPARK. Anonymous volume is bounded instead by the per-identity exec limit and per-op rate-limit windows

### Privacy Properties

See the [Privacy Model](#privacy-model) section for a detailed comparison of immediate vs encrypted batch mode.

**Cross-operation unlinkability:** the proof's Scope is `ScopeElement(nullifier_domain, raw_scope)`, so the same member's nullifiers never repeat across operations or domains (a blog post 5, a forum post 5 and a commons proposal 5 give three unrelated nullifiers, as do two GLOBAL ops in different domains). Before the domain was hashed in, any two ops sharing a raw scope produced the same public nullifier for one member and linked them across modules and epochs.

**Ownership-mode linkability:** every management op on one piece of anonymous content carries the same owner tag, which is the creation exec's nullifier, so those ops are linkable to each other and to the creation (inherent: they must prove the same owner). They stay unlinkable to the member and to the member's other content.

**Timing analysis mitigation (immediate mode):**
- Submitter address is meaningless: by default every client signs as the shared [public anonymous submitter](#public-anonymous-submitter), so all anonymous execs come from one address (any other account still works, fresh or reused, but stands out)
- No funding transaction needed (shield pays gas)
- Multiple users share a submitter address without privacy loss; immediate execs have no per-address cap (the identity rate limit bounds them), so the shared address never throttles honest users
- Client tooling should add random delays before submission

**Timing analysis mitigation (encrypted batch mode):**
- All of the above, plus:
- Operations execute together in a batch — no per-operation timing
- Execution order is shuffled — no correlation between submission order and execution order
- `min_batch_size` prevents single-operation batches (anonymity set = 1)
- Content is hidden until batch execution — mempool analysis reveals nothing

### Encrypted Batch: What the Submission Proof Closes

The operation proof is sealed in the ciphertext and verified only at execution, so without a cleartext proof a batch submission used to be paid for on trust: the rate-limit nullifier could be invented or borrowed (dodging or burning quotas), anyone watching the mempool could race a member's cleartext nullifier with garbage and censor the op epoch after epoch for free, and the reserve could be drained at `max_pending_queue_size × max_fee_per_exec` per epoch by garbage from throwaway accounts (the per-address cap was no bound: accounts are free). The [submission proof](#the-submission-proof) closes all three: the module pays only for a proven member under its quota, a submission nullifier belongs to its prover and payload, and the payload is bound to the proof. Nothing public at submission links to the executed operation (the operation nullifier is sealed), so the earlier objection to a cleartext proof — that it would link execution back to the submission — no longer applies; it was in any case already true of the old cleartext operation nullifier.

What remains deferred, by construction: the inner message type and registration are checked only at execution (`processEncryptedOp`), so a member can spend its own quota on a payload that drops (unregistered, `IMMEDIATE_ONLY`, ownership-mode, bad operation proof, used operation nullifier). That costs the attacker its own identity's `max_execs_per_identity_per_epoch` slots and one fee each, the same budget an immediate exec has; queue capacity and payload size bound the state it can hold.

### Encrypted Batch: Small Batch Deanonymization

A single operation in a batch is trivially correlated by timing. Mitigations:

- `min_batch_size` (default 3; params only require `>= 1`, and devnet runs 1) is enforced *before any key exists*: the EndBlocker demands shares for a closed epoch only once it holds `min_batch_size` ops alone or together with the other closed epochs, so a small batch waits encrypted instead of decryptable
- `max_pending_epochs` caps the wait — a lone op is eventually demanded and executed with an anonymity set of one
- During quiet periods, this means maximum privacy is weaker. This is an inherent tradeoff: anonymity sets require co-submitters

### Encrypted Batch: Decryption Share Integrity

Lagrange interpolation returns *a* point for any set of shares; one wrong share yields a wrong key with no indication of whose it was. Every admitted share therefore passes the pairing check against its validator's G2 public share (`VerifyValidatorDecryptionShare`) in `VerifyVoteExtension`, `ProcessProposal` and the PreBlocker, and only stored (verified) shares are interpolated. A validator that submits bad shares is identified (`shield_tle_invalid_share`) and accrues liveness misses like one that submits none. Shares are also refused for epochs that are not closed, not demanded, or outside the `max_pending_epochs` window, so no validator can pre-publish shares for future epochs or get a key reconstructed before the batch is ready.

### DKG: Misbehaving Dealers

A Feldman DKG without a complaint round lets one dealer make every other validator's share inconsistent with its on-chain public share (a wrong or missing evaluation passes every on-chain check, since the ciphertext is opaque), which would stall TLE and get honest validators jailed by the liveness judge. The ceremony therefore has a COMPLAINING and a JUSTIFYING phase (GJKR-style): a validator that cannot open or verify an evaluation names the dealer; the dealer must publish the evaluation in clear, verified against its commitments; an unanswered complaint disqualifies the dealer. A false complaint only costs the complainer the secrecy of its own evaluation from that dealer. Garbage ciphertexts are length-checked and recovered so they cannot crash a validator's `ExtendVote`. Validators must be online for the whole window (`dkg_window_blocks`); one that misses the complaint window while holding a bad evaluation is judged like an offline validator until the next re-key.

### Colluding Validators

If ≥ 2/3 of validators collude, they can:
- Reconstruct the decryption key early (decrypt pending ops before batch time)
- Read encrypted payloads in the mempool
- This is the **same trust assumption as consensus** — if 2/3 of validators are malicious, the chain itself is compromised. TLE privacy is the least of the chain's problems at that point.

### Community Pool Impact

At maximum usage (daily cap reached every day):
- 200 SPARK/day × 365 days = 73,000 SPARK/year
- With 100M total supply and 2-5% inflation, annual new SPARK ≈ 2-5M
- Community pool gets 15% ≈ 300-750K SPARK/year
- Shield draw at max ≈ 73K SPARK/year ≈ 10-24% of community pool inflow
- In practice, early usage will be far below the cap

Governance can adjust `max_funding_per_day` as usage patterns become clear.

## Genesis State

```protobuf
message GenesisState {
  Params params = 1 [(gogoproto.nullable) = false];
  repeated ShieldedOpRegistration registered_ops = 2 [(gogoproto.nullable) = false];
  repeated UsedNullifier used_nullifiers = 3 [(gogoproto.nullable) = false];
  repeated DayFunding day_fundings = 4 [(gogoproto.nullable) = false];
  // ZK proof verification keys (set at genesis, updated only via chain upgrade)
  repeated VerificationKey verification_keys = 5 [(gogoproto.nullable) = false];
  // TLE key set (from DKG ceremony — populated at genesis or first DKG)
  TLEKeySet tle_key_set = 6 [(gogoproto.nullable) = false];
  // Encrypted batch mode state (empty at genesis, populated during operation)
  repeated PendingShieldedOp pending_ops = 7 [(gogoproto.nullable) = false];
  ShieldEpochState shield_epoch_state = 8 [(gogoproto.nullable) = false];
  repeated ShieldEpochDecryptionKey decryption_keys = 9 [(gogoproto.nullable) = false];
  uint64 next_pending_op_id = 10;
  // Operational state (empty at genesis, required for export/import consistency)
  repeated IdentityRateLimitEntry identity_rate_limits = 11 [(gogoproto.nullable) = false];
  repeated string pending_nullifiers = 12;
  repeated ShieldDecryptionShare decryption_shares = 13 [(gogoproto.nullable) = false];
  repeated TLEMissCounterEntry tle_miss_counters = 14 [(gogoproto.nullable) = false];
  // DKG ceremony state
  DKGState dkg_state = 15 [(gogoproto.nullable) = false];
  // DKG contributions for the current round
  repeated DKGContributionEntry dkg_contributions = 16 [(gogoproto.nullable) = false];
  // DKG registrations (pub keys from REGISTERING phase)
  repeated DKGContributionEntry dkg_registrations = 17 [(gogoproto.nullable) = false];
  // Closed epochs whose decryption shares were demanded (height; negative once judged)
  repeated DecryptionDemandEntry decryption_demands = 18 [(gogoproto.nullable) = false];
  // DKG complaints for the current round
  repeated DKGComplaint dkg_complaints = 19 [(gogoproto.nullable) = false];
  // TLE liveness ring buffer (validator, slot) -> participated
  repeated TLEParticipationEntry tle_participation = 20 [(gogoproto.nullable) = false];
  // Demanded epochs judged under the current key set (slot = count % window)
  uint64 tle_judged_epochs = 21;
  // Fees paid in the current shield epoch (params.max_fees_per_epoch)
  EpochFeeLedger epoch_fee_ledger = 22 [(gogoproto.nullable) = false];

  // Nullifier domains no live op uses, with the last op's registration.
  repeated ShieldedOpRegistration retired_domains = 23 [(gogoproto.nullable) = false];
  // Fees paid per identity (rate-limit nullifier) in the current shield epoch
  repeated IdentityEpochFeeEntry identity_epoch_fees = 24 [(gogoproto.nullable) = false];
}

// Helper messages for genesis export/import
message IdentityRateLimitEntry {
  uint64 epoch = 1;
  string rate_limit_nullifier_hex = 2;
  uint64 count = 3;
}

message TLEMissCounterEntry {
  string validator_address = 1;
  uint64 miss_count = 2;
}
```

`GenesisState.Validate()` runs `ShieldedOpRegistration.Validate()` on every registered op and rejects a duplicate `message_type_url` or two ops that share a nullifier domain with different scoping (`ErrNullifierDomainConflict`), and a retired domain listed twice or still used by a registered op, then validates the TLE key set and the params. `InitGenesis` pairing-verifies every imported decryption share against its validator's public share in the imported key set (like every ABCI path) and refuses the genesis on a bad one; it restores the pending-op sequence with one write. Export/import keeps the TLE liveness ring and judged-epoch counter, so a restart forgives no accumulated misses. `ExportGenesis` exports exactly the stored registrations (not the `DefaultGenesis` seed), so an op governance deregistered stays gone across an export/import. In production builds `InitGenesis` also applies the verification-key launch gate (see Verification Keys).

## Default Parameters

Production (`mainnet`/`testnet` build) values. Mainnet ships `enabled: false` until its ceremony key is in genesis (see Verification Keys). `DefaultParams()` returns the build tag's values (`genesis_vals_*.go`): `testparams` and `devnet` builds use shorter windows and smaller minimums (`min_gas_reserve`, `shield_epoch_interval`, `min_batch_size`, `max_pending_queue_size`, `tle_miss_window`, `tle_miss_tolerance`, `tle_jail_duration`, `min_tle_validators`, `dkg_window_blocks`), and `testparams` sets `enable_batch_after_dkg: true`.

```yaml
params:
  # General
  enabled: true
  max_funding_per_day: "200000000"            # 200 SPARK/day
  min_gas_reserve: "100000000"               # 100 SPARK
  max_gas_per_exec: 500000                   # 500k gas
  max_execs_per_identity_per_epoch: 50

  # Encrypted batch mode
  encrypted_batch_enabled: false             # requires DKG ceremony completed first
  shield_epoch_interval: 50                  # ~5 minutes at 6s blocks
  min_batch_size: 3                          # minimum anonymity set
  max_pending_epochs: 6                      # ~30 min max wait before force-execute
  max_pending_queue_size: 1000               # max pending ops in queue
  max_encrypted_payload_size: 16384          # 16 KB max per encrypted payload
  max_ops_per_batch: 100                     # max ops executed per EndBlocker (bounds gas)

  # TLE liveness enforcement
  tle_miss_window: 100                        # rolling window in shield epochs
  tle_miss_tolerance: 10                      # max misses before jail
  tle_jail_duration: 600                      # 10 minutes jail

  # DKG automation
  min_tle_validators: 5                       # min bonded validators to auto-open DKG
  dkg_window_blocks: 200                      # ~20 min DKG window: four equal phases (register, deal, complain, justify)
  max_validator_set_drift: 33                 # 33% drift triggers automatic re-keying
  enable_batch_after_dkg: false               # governance enables batch mode after the DKG (testparams: true)

  # Fees
  max_fee_per_exec: "50000"                   # 0.05 SPARK: ~700k gas at 0.025 uspark/gas, ~3x headroom
  max_fees_per_epoch: "650000"                # 0.65 SPARK/epoch x 288 epochs/day = 187 SPARK/day (devnet 325000, testparams 10 SPARK)
```

## CLI Commands

### Transaction Commands

```bash
# Submit an immediate shielded execution (every byte field is hex)
sparkdreamd tx shield shielded-exec \
  --inner-message '<inner msg JSON with @type>' \
  --proof <hex> \
  --nullifier <hex> --rate-limit-nullifier <hex> \
  --merkle-root <hex> \
  --proof-domain 1 --min-trust-level 1 \
  --from <any-account>

# Submit an encrypted-batch execution (exec-mode 1; proof = the submission proof)
sparkdreamd tx shield shielded-exec \
  --exec-mode 1 --target-epoch <current shield epoch> \
  --encrypted-payload <hex> \
  --proof <hex> \
  --nullifier <hex> --rate-limit-nullifier <hex> \
  --merkle-root <hex> \
  --proof-domain 1 --min-trust-level 0 \
  --from <any-account>

# MsgRegisterShieldedOp, MsgDeregisterShieldedOp, MsgUpdateParams and
# MsgTriggerDkg are governance-only and have no CLI command (autocli skips
# them); they are submitted as x/gov proposal messages.
```

### Query Commands

```bash
# Query parameters
sparkdreamd q shield params

# Query a registered shielded operation
sparkdreamd q shield shielded-op "/sparkdream.blog.v1.MsgCreatePost"

# List all registered shielded operations
sparkdreamd q shield shielded-ops

# Query module balance
sparkdreamd q shield module-balance

# Check if a nullifier has been used
sparkdreamd q shield nullifier-used <domain> <scope> <nullifier-hex>

# Query a day's funding amount (day = UTC day of the block time)
sparkdreamd q shield day-funding <day>

# Query current shield epoch state
sparkdreamd q shield shield-epoch

# Query pending operations (optionally one epoch)
sparkdreamd q shield pending-ops [--epoch N]

# Query pending operation count
sparkdreamd q shield pending-op-count

# Query TLE master public keys (clients encrypt to master_public_key_g2)
sparkdreamd q shield tle-master-public-key

# Query the full TLE key set (master keys + validator public shares)
sparkdreamd q shield tle-key-set

# Query the ZK verification key
sparkdreamd q shield verification-key shield_v1

# Query a validator's TLE miss count (operator address)
sparkdreamd q shield tle-miss-count <valoper-address>

# Query decryption shares for an epoch
sparkdreamd q shield decryption-shares <epoch>

# Query remaining rate limit for a rate-limit nullifier
sparkdreamd q shield identity-rate-limit <rate-limit-nullifier-hex>

# Query DKG ceremony state
sparkdreamd q shield dkg-state

# Query DKG contributions for the current round
sparkdreamd q shield dkg-contributions
```

## Cross-Module Integration

### x/shield → x/rep (Merkle Tree Roots)

```go
// Get trust tree roots for proof validation
k.late.repKeeper.GetTrustTreeRoot(ctx)
k.late.repKeeper.GetPreviousTrustTreeRoot(ctx)
```

### x/shield → x/distribution (Auto-Funding)

```go
// Draw from community pool to fund gas reserve
k.distrKeeper.DistributeFromFeePool(ctx, coins, shieldModuleAddr)
```

### x/shield: Self-Contained (ZK + TLE)

x/shield performs all ZK proof verification and TLE operations internally — no external keeper calls needed for these:

```go
// ZK proof verification — native to x/shield
k.verifyProof(ctx, msg, binding)  // uses stored Groth16 verification key (shield_v1); binding = consumeBinding / ownedBinding

// TLE — native to x/shield
k.GetTLEKeySetVal(ctx)                 // master keys + public shares
k.TryReconstructEpochKey(ctx, epoch)   // from stored (verified) shares, at threshold
```

### App-Level Wiring (app.go)

```go
// x/shield is created via depinject with accountKeeper and bankKeeper.
// Late-wired keepers are set after depinject.Inject() to avoid cycles:
app.ShieldKeeper.SetRepKeeper(app.RepKeeper)
app.ShieldKeeper.SetDistrKeeper(NewDistrKeeperAdapter(app.DistrKeeper)) // community-pool portion only
app.ShieldKeeper.SetSlashingKeeper(app.SlashingKeeper)
app.ShieldKeeper.SetStakingKeeper(app.StakingKeeper)
app.ShieldKeeper.SetIdentityKeeper(app.IdentityKeeper) // bond denom
app.ShieldKeeper.SetRouter(app.MsgServiceRouter())

// Register ShieldAware modules (double-gate for inner message dispatch)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.blog.v1.", app.BlogKeeper)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.forum.v1.", app.ForumKeeper)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.collect.v1.", app.CollectKeeper)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.rep.v1.", app.RepKeeper)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.commons.v1.", app.CommonsKeeper)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.federation.v1.", app.FederationKeeper)

// Ante chain: ShieldGasDecorator before the (wrapped) DeductFeeDecorator
shieldante.NewShieldGasDecorator(app.ShieldKeeper, app.BankKeeper),
shieldante.NewSkipIfFeePaidDecorator(cosmos_ante.NewDeductFeeDecorator(...)),

// ABCI++: DKG vote extensions, proposal handlers (delegating to the SDK
// default handler) and the PreBlocker injection
dkgHandler := shieldabci.NewDKGVoteExtensionHandler(app.ShieldKeeper, homeDir)
app.SetExtendVoteHandler(dkgHandler.ExtendVoteHandler())
app.SetVerifyVoteExtensionHandler(dkgHandler.VerifyVoteExtensionHandler())
defaultProposals := baseapp.NewDefaultProposalHandler(app.Mempool(), app.BaseApp)
app.SetPrepareProposal(shieldabci.PrepareProposalHandler(app.ShieldKeeper, defaultProposals.PrepareProposalHandler()))
app.SetProcessProposal(shieldabci.ProcessProposalHandler(app.ShieldKeeper, defaultProposals.ProcessProposalHandler()))
```

## File References

### Client-Side Tools (Reused by x/shield)

- `tools/zk/circuit/shield_circuit.go` — Unified ShieldCircuit (Groth16 over BN254, trust tree membership + trust level + nullifiers + rate limiting)
- `tools/zk/circuit/shield_circuit_test.go` — Circuit tests
- `tools/crypto/crypto.go` — Merkle tree, MiMC hashing, key management, nullifiers (`ScopeElement`, `ComputeScopedNullifier`)
- `tools/zk/prover/prover.go` — Client-side proof generation
- `tools/zk/cmd/seed-tle/main.go` — TLE seed data generation CLI
- `tools/crypto/sparse.go` - `SparseMerkleProof`, `DeriveSecretKeyFromSignature`, `ShieldMessageHash`, `ShieldOwnedMessageHash`
- `tools/zk/cmd/ceremony/main.go` - Multi-party Groth16 trusted setup
- `tools/zk/wasm/main.go`, `tools/zk/wasm/build.sh` - Browser shield client (WASM)

### Client Tooling

How a client produces an exec, using only public chain data:

1. **Secret key.** `crypto.DeriveSecretKeyFromSignature(sig)` turns a wallet signature over a fixed message into the shield secret key, so it can be recovered on any device holding the wallet. Secret keys (this and `prover.GenerateSecretKey`) are always canonical BN254 scalars (`crypto.IsCanonicalFieldElement`): MiMC rejects larger values and `HashToField` discards that error, so a non-canonical key would derive a public key the circuit can never prove against. The member registers the derived public key with x/rep (`MsgRegisterZkPublicKey`). A native multisig account has no single key to sign the fixed message with, so multisig members derive the secret from a dedicated 256-bit shield seed (`prover.DeriveSecretKeyFromSeed`) and co-signers verify the key's fingerprint (`tools/crypto.ZkKeyFingerprint`, `sparkdreamd zk-key-fingerprint`) before signing the registration; anonymous actions remain 1-of-1 ([docs/multisig-accounts.md](multisig-accounts.md)). `MsgClearZkPublicKey` zeroes a leaked key at once with no cooldown.
2. **Merkle path.** x/rep's `TrustTree` query (`/sparkdream/rep/v1/trust_tree`, paginated) exports the root, depth, leaf count and every non-empty leaf. The client rebuilds the tree locally and computes its own path with `crypto.SparseMerkleProof` (which hashes only occupied branches and matches x/rep's root), so it never reveals which leaf it wants by asking for a specific proof.
3. **Proof.** The prover (`prover.ShieldProofInput`) takes the secret key, path, `min_trust_level`, the operation's nullifier `Domain`, the resolved raw nullifier `Scope` (two-field scopes packed as `(qualifier << 56) | id`), the current shield epoch and `crypto.ShieldMessageHash` of the exact inner `Any` the exec will carry. It computes the circuit's Scope input as `crypto.ScopeElement(Domain, Scope)` and the nullifier as `crypto.ComputeScopedNullifier(sk, Domain, Scope)`; a proof made with the wrong domain fails verification. For an ownership-mode op the domain and scope are the content's owner claim (not the op's registration), and the message hash is `crypto.ShieldOwnedMessageHash(type_url, value, owner_sequence)`; the resulting nullifier must equal the content's owner tag. A client finds the content it owns from public chain data alone by checking `ComputeScopedNullifier(sk, owner_domain, owner_scope) == owner_tag`, so a wallet-derived key restores management on a new device.
4. **Submit.** Wrap in `MsgShieldedExec`, sign as the public anonymous submitter (unordered tx), fee in the bond denom at most `max_fee_per_exec`.
5. **Encrypted batch (optional).** For an op whose content should stay hidden until execution: read `current_epoch` (`ShieldEpoch`) and `master_public_key_g2` (`TLEMasterPublicKey`); prove the operation with `RateLimitEpoch = current_epoch`; seal `tle.EncodeExecFragment(type_url, value, proof, nullifier)` with `tle.Encrypt(master_public_key_g2, current_epoch, fragment)`; prove the *submission* with the same key and epoch, `Domain = tle.BatchSubmissionDomain`, `Scope = tle.BatchSubmissionScope(payload)` and `MessageHash = crypto.ShieldMessageHash(tle.BatchSubmissionTypeURL, payload)`; submit with `exec_mode = ENCRYPTED_BATCH`, `target_epoch = current_epoch`, `encrypted_payload`, and the submission proof's `proof`, `nullifier`, `rate_limit_nullifier` and `merkle_root` in cleartext (no `inner_message`). Nothing secret is needed to encrypt; `tools/tle` is the one implementation the chain decrypts with.

**TLE library (`tools/tle`).** Boneh-Franklin IBE on BN256 with the shield epoch as identity: `Encrypt`/`Decrypt`, `EpochTag`, `DecryptionShare`/`VerifyDecryptionShare`/`ReconstructEpochKey`, and the fragment encoder. Compiles to wasm. `tools/zk/cmd/seed-tle` wraps it for tests and operators: `keygen` (single-validator key set + pre-computed epoch keys for genesis), `encrypt`/`payload` (to a key file's or the chain's `--mpk-g2`), `decrypt`.

**Trusted setup (`tools/zk/cmd/ceremony`).** Replaces the single-party `tools/zk/cmd/setup` for any key that goes on chain. Runs gnark's `mpcsetup` in five steps on a transcript directory handed from contributor to contributor: `contribute-phase1`, `seal-phase1 -beacon <hex>`, `contribute-phase2`, `seal-phase2 -beacon <hex>`, `verify [-genesis genesis.json]`. Sound as long as one contributor discarded their randomness; nothing secret is written to disk. Beacons should be public randomness fixed in advance (e.g. a future drand round). `seal-phase2` writes `proving_key.bin`, `circuit.r1cs`, `verifying_key.bin`, `verifying_key.hex` and `proving_key.raw.bin` (uncompressed, for web clients), and prints the genesis `verification_keys` entry (circuit id `shield_v1`, `vk_bytes` base64 - not the `.hex` file's encoding). The mainnet launch steps are in `deploy/README.md` ("Shield verification key"). `verify` replays the transcript and checks the published VK - and optionally the one in a genesis file - matches. A circuit change (such as adding `MessageHash`) needs Phase 2 redone and a new VK.

**Browser client (`tools/zk/wasm`).** Built by `tools/zk/wasm/build.sh [OUT] [KEYS]` into `shield.wasm` plus Go's `wasm_exec.js` (given the ceremony directory as `KEYS`, it also copies `proving_key.raw.bin` and `circuit.r1cs`), so web clients hash, build Merkle paths and prove with exactly the code the chain verifies against. The wasm is stamped with the chain release it was built from (`VERSION`/`COMMIT`, resolved like the Makefile's), and `manifest.json` records that version and every file's sha256 for clients to pin. A prover from another release can derive the proof's public inputs (scope element, message hash) differently from the chain, and its proofs then fail only at broadcast with `ErrInvalidProof`, so `deploy/scripts/prepare-release.sh` builds the prover for every release into `build/zk/<version>/` (after checking the ceremony's verifying key against each network's genesis), and clients serve each release's prover from its own `<version>/` directory and refuse to prove when `version()` differs from the node's `application_version`. It registers a global `sparkdreamShield` with `version()`, `deriveSecret(signatureB64)`, `publicKey(secretKeyHex)`, `loadKeys(provingKey, r1cs)`, `prove(inputJSON)` and `encrypt(inputJSON)`; every call returns a JSON string (`{"error": ...}` on failure). `encrypt` takes `master_public_key_g2`, the `epoch`, the inner Any and the proof from `prove` and returns the `encrypted_payload` for a batch submission. `prove` takes the trust-tree data from the `TrustTree` query, the operation's nullifier `"domain"` and raw `"scope"`, and the inner Any's type URL and bytes. For an ownership-mode op it also takes `"owner_sequence"` (decimal string; the content's current owner sequence, e.g. a collection's `anon_owner_sequence`) and `"domain"`/`"scope"` are the content's owner claim; when `owner_sequence` is present the message hash is `ShieldOwnedMessageHash`. The result includes the nullifier, which for an ownership op must equal the content's owner tag. `loadKeys` uses `prover.NewShieldProverFromTrustedBytes`, which skips the proving key's curve-point checks (tens of seconds in a browser) - only for a key whose integrity is checked another way, such as a pinned hash; paired with `proving_key.raw.bin` it also skips point decompression.

### On-Chain Implementation

- `x/shield/keeper/proof.go` — ZK proof verification (Groth16 over BN254)
- `x/shield/keeper/keeper.go` — Keeper struct, late-keeper wiring, ShieldAware registry
- `x/shield/keeper/msg_server_shielded_exec.go` — MsgShieldedExec handler (immediate + encrypted batch)
- `x/shield/keeper/dkg.go` — DKG round helpers, complaints storage, qualified contributions
- `x/shield/keeper/begin_block.go` — Auto-funding and the DKG state machine (five phases, drift re-keying)
- `x/shield/keeper/dkg_crypto.go` — DKG cryptographic operations (Feldman checks, `VerifyEvaluation`, aggregation)
- `x/shield/keeper/dkg_local.go` — Local DKG key management (per-validator secret state, complaints, justifications, cached shares)
- `x/shield/keeper/tle_shares.go` — Share acceptance rule, decryption demands, key reconstruction, key-set retirement
- `x/shield/keeper/tle_liveness.go` — TLE liveness judge (ring of judged epochs, bonded validators only)
- `x/shield/abci/validate.go` — The per-phase acceptance rule shared by VerifyVoteExtension, Prepare/ProcessProposal and the PreBlocker
- `x/shield/abci/vote_extensions.go` — DKGVoteExtensionHandler (ExtendVote/VerifyVoteExtension)
- `x/shield/abci/proposal_handler.go` — PrepareProposalHandler / ProcessProposalHandler (size-budgeted injection, delegating to the SDK default handler)
- `x/shield/abci/preblocker.go` — ProcessDKGInjection (PreBlocker)
- `x/shield/ante/shield_gas.go` — ShieldGasDecorator (module-paid gas)
- `x/shield/ante/proof_guard.go` — Node-local rejected-proof cache and verification throttle
- `tools/tle/` — TLE encryption, epoch tags, share verification, exec fragment, submission-proof parameters (shared with clients)
- `x/shield/ante/skip_fee_decorator.go` — SkipIfFeePaidDecorator
- `x/shield/types/genesis.go` — Default genesis shielded operations
- `x/shield/types/shield_aware.go` — ShieldAware interface definition, `ShieldOwnershipResolver`, `OwnershipClaim`
- `x/shield/types/exec_context.go` - `WithExecNullifier`/`GetExecNullifier`, `WithOwnershipTag`/`GetOwnershipTag`
- `x/shield/types/registration.go` - `ShieldedOpRegistration.Validate()` (ownership ops must be IMMEDIATE_ONLY)

### Legacy Anonymous Implementations (Replaced by x/shield)

Per-module anonymous messages have been removed. Each module now has a `shield_aware.go` that implements the `ShieldAware` interface, gating which messages can be executed via `MsgShieldedExec`:

- `x/blog/keeper/shield_aware.go`
- `x/forum/keeper/shield_aware.go`
- `x/collect/keeper/shield_aware.go`
- `x/commons/keeper/shield_aware.go`
- `x/rep/keeper/shield_aware.go`
- `x/federation/keeper/shield_aware.go`

### File Structure

```
x/shield/
├── keeper/
│   ├── keeper.go                           # Keeper constructor, late-keeper wiring, ShieldAware registry
│   ├── keeper_test.go                      # Keeper unit tests
│   ├── msg_server.go                       # MsgServer constructor
│   ├── msg_server_shielded_exec.go         # MsgShieldedExec handler (immediate + encrypted batch)
│   ├── msg_server_register_shielded_op.go  # MsgRegisterShieldedOp handler (governance)
│   ├── msg_server_deregister_shielded_op.go # MsgDeregisterShieldedOp handler (governance)
│   ├── dkg_local.go                        # Local DKG key management (per-validator secret state)
│   ├── msg_server_trigger_dkg.go           # MsgTriggerDKG handler (governance)
│   ├── msg_server_test.go                  # Message server tests
│   ├── msg_update_params.go                # MsgUpdateParams handler
│   ├── begin_block.go                      # Auto-funding from community pool, DKG state machine
│   ├── end_block.go                        # Epoch advancement, demands, batch execution (every block)
│   ├── nullifier.go                        # Nullifier storage and checking
│   ├── registration.go                     # Shielded op registration CRUD
│   ├── rate_limit.go                       # Per-identity rate limiting
│   ├── funding.go                          # Community pool funding logic
│   ├── pending.go                          # Pending operation queue management
│   ├── epoch.go                            # Shield epoch state management
│   ├── proof.go                            # ZK proof verification (Groth16 over BN254)
│   ├── tle.go                              # TLE key management
│   ├── tle_crypto.go                       # TLE cryptographic operations, PoP message
│   ├── tle_shares.go                       # Share acceptance rule, demands, reconstruction, key-set retirement
│   ├── tle_liveness.go                     # TLE liveness tracking and jailing
│   ├── dkg.go                              # DKG round helpers, complaints
│   ├── dkg_crypto.go                       # DKG cryptographic operations
│   ├── proof_vk_production.go / proof_vk_testparams.go  # requireVerificationKey per build tag
│   ├── shuffle.go                          # Deterministic batch shuffle (Fisher-Yates)
│   ├── prune.go                            # State pruning
│   ├── query.go                            # Query server constructor
│   ├── query_params.go                     # Params query handler
│   ├── query_shield.go                     # Shield-specific query handlers
│   ├── query_test.go                       # Query tests
│   └── genesis.go                          # Genesis import/export
├── module/
│   ├── module.go                           # AppModule implementation
│   ├── depinject.go                        # Depinject wiring
│   ├── autocli.go                          # AutoCLI configuration
│   └── simulation.go                       # Simulation module
├── simulation/
│   ├── register_shielded_op.go             # Register shielded op simulation
│   ├── deregister_shielded_op.go           # Deregister shielded op simulation
│   ├── shielded_exec.go                    # Shielded execution simulation
│   └── trigger_dkg.go                      # DKG trigger simulation
├── types/
│   ├── keys.go                             # Store keys
│   ├── errors.go                           # Typed errors
│   ├── expected_keepers.go                 # Keeper interfaces (RepKeeper, SlashingKeeper, StakingKeeper)
│   ├── shield_aware.go                     # ShieldAware interface (implemented by target modules)
│   ├── public_submitter.go                 # Shared public anonymous submitter key/address
│   ├── proven_trust.go                     # WithProvenTrustLevel / ProvenTrustLevel context helpers
│   ├── proof_verified.go                   # Ante-to-msg-server "proof already verified" marker
│   ├── exec_context.go                     # Exec nullifier / ownership tag context helpers
│   ├── scope.go                            # Two-field nullifier scope packing
│   ├── batch_submission.go                 # Submission-proof parameters (re-exported from tools/tle)
│   ├── threshold.go                        # ComputeThreshold
│   ├── events.go                           # Event types
│   ├── genesis.go                          # Default genesis state, shielded operations, TLEKeySet.Validate
│   ├── genesis_vals_{testparams,devnet,testnet,mainnet}.go  # Genesis parameter values per build tag
│   ├── params.go                           # Parameter defaults and validation
│   ├── types.go                            # Type helpers
│   ├── types_test.go                       # Type tests
│   └── codec.go                            # Codec registration
├── abci/
│   ├── validate.go                         # Per-phase acceptance rule (shared by every ABCI path)
│   ├── vote_extensions.go                  # DKGVoteExtensionHandler (ExtendVote/VerifyVoteExtension)
│   ├── proposal_handler.go                 # PrepareProposalHandler / ProcessProposalHandler
│   ├── preblocker.go                       # ProcessDKGInjection (PreBlocker)
│   └── types.go                            # InjectedDKGData encoding/decoding, magic prefix
├── ante/
│   ├── shield_gas.go                       # ShieldGasDecorator (module-paid gas)
│   ├── proof_guard.go                      # Rejected-proof cache + verification throttle (CheckTx only)
│   ├── skip_fee_decorator.go               # SkipIfFeePaidDecorator
│   └── ante_test.go                        # Ante handler tests
├── client/cli/
│   ├── tx.go                               # CLI transaction commands
│   └── tx_shielded_exec.go                 # MsgShieldedExec CLI handler
└── proto/sparkdream/shield/v1/             # Proto definitions
    ├── tx.proto
    ├── query.proto
    ├── params.proto
    ├── types.proto
    └── genesis.proto
```

### Specification References

- `docs/x-rep-spec.md` — Trust tree, member management, ZK public keys
- `docs/x-blog-spec.md` — Blog content management (anonymous ops via x/shield)
- `docs/x-forum-spec.md` — Forum discussion platform (anonymous ops via x/shield)
- `tools/` — Client-side cryptographic tooling (ZK circuits, TLE, crypto primitives)
