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
| x/commons | `MsgSubmitAnonymousProposal`, `MsgAnonymousVoteProposal` | Submitter needs gas funds |
| x/rep | `MsgCreateChallenge` (with `is_anonymous` flag) | Submitter needs SPARK for gas |
| x/forum | `MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, `MsgAnonymousReact` | Submitter needs gas funds |
| x/collect | `MsgCreateAnonymousCollection`, `MsgAnonymousReact` | Submitter needs gas funds |

Every anonymous operation requires the submitter to have funds for gas. This creates a metadata trail: an observer can correlate funding transactions to anonymous submissions. Relayer subsidy (x/blog) partially addresses this but introduces a trusted intermediary who learns timing metadata.

x/shield eliminates all of this. The submitter address is meaningless — it can be a fresh account with zero balance. The module pays gas. No funding trail, no relayer, no correlation.

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
├── The nullifier (replay prevention — unlinkable to identity by design)
├── The rate-limit nullifier (same for all ops by same member in same epoch — for rate limiting)
├── The Merkle root (same for ALL members in a tree snapshot)
├── The proof domain (trust tree)
├── The minimum trust level being proven
└── The message type URL (in immediate mode) or target epoch (in encrypted batch mode)
```

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
| Timing correlation (submission → operation) | **Vulnerable** | Defeated |
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
   submitter = any account

4. Broadcast ──────────────────►  5. ShieldGasDecorator (ante handler)
                                     - Detects MsgShieldedExec
                                     - Deducts gas from shield module account
                                     - Skips normal fee deduction for submitter

                                  6. msg_server.ShieldedExec()
                                     - Validates inner message type is registered
                                     - Looks up proof requirements for that message type
                                     - Verifies ZK proof (trust tree membership)
                                     - Checks nullifier not used (domain + scope)
                                     - Records nullifier
                                     - Dispatches inner message via router

                                  7. Target module executes normally
                                     - Sees shield module account as sender
                                     - No knowledge that this was shielded
```

### Encrypted Batch Mode Flow

```
User (offline)                    Chain
─────────────                     ─────
1. Construct inner message
   + generate ZK proof

2. Read TLE master public key
   from chain state

3. Encrypt (inner_message + proof)
   with TLE master key via IBE
   → encrypted_payload

4. Wrap in MsgShieldedExec
   exec_mode = ENCRYPTED_BATCH
   nullifier = cleartext (for spam prevention)
   merkle_root = cleartext (for pre-validation)
   encrypted_payload = ciphertext
   target_epoch = current shield epoch

5. Broadcast ──────────────────►  6. ShieldGasDecorator (ante handler)
                                     - Deducts gas from shield module account

                                  7. msg_server.ShieldedExec()
                                     - Validates nullifier not used (cleartext check)
                                     - Validates merkle root is current or previous
                                     - Validates encrypted_payload size within limits
                                     - Checks per-identity rate limit (via nullifier)
                                     - Records nullifier
                                     - Stores PendingShieldedOp in queue
                                     - Does NOT verify ZK proof (it's encrypted)

                              ┌──── Operations accumulate in queue... ────┐

                                  8. Shield epoch boundary reached
                                     Validators submit decryption shares
                                     (from x/shield DKG key shares)

                                  9. EndBlocker: threshold met
                                     - Reconstruct epoch decryption key
                                     - Decrypt all pending ops for this epoch
                                     - Verify ZK proofs (now readable)
                                     - Shuffle execution order (block hash seed)
                                     - Execute valid inner messages
                                     - Drop invalid ops (bad proof, decode error)
                                     - Emit batch execution event
```

### Why Nullifier Stays Cleartext

The nullifier is *designed* to be publicly visible — it's a cryptographic commitment that prevents double-action without revealing identity. Every observer already sees nullifiers in immediate mode. Keeping it cleartext in encrypted batch mode enables critical spam prevention at submission time:

1. **Duplicate rejection**: Prevent the same nullifier from being submitted twice (before paying decryption costs)
2. **Rate limiting**: Track per-identity submission count using the `rate_limit_nullifier` (same for all ops by the same member in the same epoch)
3. **No privacy loss**: Neither nullifier reveals which member created it — that's the fundamental ZK guarantee

## ZK Proof Verification (Owned by x/shield)

x/shield owns all ZK proof verification infrastructure. No other module verifies ZK proofs — they all route through `MsgShieldedExec`. This centralization ensures consistent proof handling, nullifier management, and gas accounting.

### Proof Domain

x/shield uses a single unified proof domain:

#### TRUST_TREE

Proves membership in the x/rep trust tree with a minimum trust level. Used for **all** anonymous operations — content (blog posts, forum posts, reactions, collections) and governance (anonymous proposals, votes, challenges).

**Circuit**: `ShieldCircuit` (Groth16 over BN254)
**Public inputs**: MerkleRoot, Nullifier, RateLimitNullifier, MinTrustLevel, Scope, RateLimitEpoch
**Proves**: (1) member exists in trust tree, (2) trust level >= minimum, (3) valid scoped nullifier, (4) valid rate-limit nullifier for this epoch
**Tree source**: x/rep keeper provides the Merkle tree snapshots (leaves = `MiMC(zk_public_key, trust_level)`)

The **RateLimitNullifier** is derived as `H(member_secret, RateLimitDomainTag, epoch)` where `RateLimitDomainTag = MaxUint64`. It is the same for all operations by the same member in the same epoch, enabling per-identity rate limiting without revealing which member it is. The circuit proves this derivation is correct.

> **Note**: The original design had a separate `PROOF_DOMAIN_VOTER_TREE` for governance operations. This was removed because the unified `ShieldCircuit` handles all proof domains — governance operations use `TRUST_TREE` with `min_trust_level = 0` (any member). The `PROOF_DOMAIN_VOTER_TREE` enum value (2) is reserved but unused.

### Merkle Root Validation

The trust tree domain accepts the **current or previous** tree root. This allows proofs generated slightly before a tree update to remain valid.

**Encrypted batch mode note**: Merkle roots are validated at submission time, not at execution time. If a batch op carries over multiple epochs (due to small batch or TLE failure), the root may be stale by execution time. A member removed from the tree after submission but before execution would still have their proof verify against the stored root. This is "valid when submitted" semantics — consistent with how traditional systems honor transactions valid at submission time. The `max_pending_epochs` parameter bounds how stale a root can be (default: 6 epochs ≈ 30 min).

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

The verification key is set at genesis and can only be updated via chain upgrade (not governance parameter changes) to prevent malicious key substitution. When no verification key is stored (test mode / early startup), all proof verification is skipped — this allows E2E testing without a trusted setup ceremony.

### Proof Verification Flow

```go
func (k Keeper) verifyProof(ctx context.Context, msg *types.MsgShieldedExec, scope uint64) error {
    // 1. Look up verification key — unified circuit for all domains.
    // When no VK is stored (test mode / early startup), skip ALL verification
    // including merkle root validation.
    const circuitID = "shield_v1"
    storedVK, found := k.GetVerificationKeyVal(ctx, circuitID)
    if !found || len(storedVK.VkBytes) == 0 {
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
    // Public inputs: MerkleRoot, Nullifier, RateLimitNullifier, MinTrustLevel, Scope, RateLimitEpoch
    epoch := k.GetCurrentEpoch(ctx)
    return k.verifyShieldProof(vk, proof, msg, scope, epoch)
}
```

The ZK circuit proves that `RateLimitNullifier = H(member_secret, RateLimitDomainTag, epoch)`. Since it's a public input verified by the circuit, it cannot be forged. Different members produce different rate-limit nullifiers, but the same member always produces the same one for a given epoch — enabling true per-identity rate limiting.

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

> **Note**: The original design included `GetVoterTreeRoot` / `GetPreviousVoterTreeRoot` for a separate voter tree. This was removed when the circuit was unified — all operations now use the trust tree.

## State

### Params

```protobuf
message Params {
  // Whether shielded execution is enabled globally
  bool enabled = 1;

  // Maximum uspark to skim from community pool per day (14400 blocks at 6s/block)
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
  // Total DKG window in blocks (split: first half = key registration, second half = contributions)
  uint64 dkg_window_blocks = 17;
  // Validator set drift percentage (0-100) that triggers automatic re-keying.
  // e.g. 33 means re-key if >33% of DKG participants are no longer in the bonded set.
  uint32 max_validator_set_drift = 18;
}
```

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
}

enum ProofDomain {
  PROOF_DOMAIN_UNSPECIFIED = 0;
  PROOF_DOMAIN_TRUST_TREE = 1;
  // 2 was PROOF_DOMAIN_VOTER_TREE — removed (unified circuit makes it redundant)
}

enum NullifierScopeType {
  // Scope = current epoch (one op per epoch)
  NULLIFIER_SCOPE_EPOCH = 0;
  // Scope = extracted from inner message (e.g. post_id for replies)
  NULLIFIER_SCOPE_MESSAGE_FIELD = 1;
  // No scoping (nullifier is globally unique)
  NULLIFIER_SCOPE_GLOBAL = 2;
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

### Funding Ledger

```protobuf
// Tracks funding per day to enforce the daily cap
// Day is calculated as block_height / 14400 (at 6s/block = 1 day)
message DayFunding {
  uint64 day = 1;
  string amount_funded = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}
```

### Pending Shielded Operations (Encrypted Batch Mode)

```protobuf
// Stored in queue until the shield epoch boundary triggers batch execution
message PendingShieldedOp {
  uint64 id = 1;                    // auto-increment ID
  uint64 target_epoch = 2;          // shield epoch when this should execute
  bytes nullifier = 3;              // cleartext — used for pre-checks at submission
  bytes merkle_root = 4;            // cleartext — validated at submission
  ProofDomain proof_domain = 5;
  uint32 min_trust_level = 6;
  bytes encrypted_payload = 7;      // TLE-encrypted: (inner_message + proof)
  // Note: rate_limit_nullifier is NOT stored here — rate limiting is checked at
  // submission time (handleEncryptedBatch step 7) and doesn't need re-checking at execution.
  int64 submitted_at_height = 8;    // for max_pending_epochs tracking
  uint64 submitted_at_epoch = 9;    // shield epoch when submitted
}
```

### Shield Epoch State

```protobuf
// Tracks the current shield epoch and decryption key availability
message ShieldEpochState {
  uint64 current_epoch = 1;
  int64  epoch_start_height = 2;
  // True once validators have produced the decryption key for this epoch
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

## Messages

### MsgShieldedExec

The single entry point for all anonymous operations. Supports both immediate and encrypted batch modes:

```protobuf
message MsgShieldedExec {
  option (cosmos.msg.v1.signer) = "submitter";

  // Any account — does not need funds, identity is irrelevant
  string submitter = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];

  // --- Immediate mode fields (cleartext) ---

  // The operation to execute, encoded as Any
  // The inner message's signer field MUST be set to the shield module account address
  // REQUIRED for IMMEDIATE mode, EMPTY for ENCRYPTED_BATCH mode
  google.protobuf.Any inner_message = 2;

  // ZK proof bytes (Groth16 over BN254)
  // REQUIRED for IMMEDIATE mode, EMPTY for ENCRYPTED_BATCH mode
  bytes proof = 3;

  // --- Always cleartext (both modes) ---

  // 32-byte nullifier (prevents replay — safe to expose, unlinkable to identity)
  bytes nullifier = 4;

  // 32-byte rate-limit nullifier: H(secret, "rate_limit", epoch)
  // Same for ALL operations by the same member in the same epoch.
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

  // TLE-encrypted payload containing (inner_message + proof)
  // IBE-encrypted with the epoch-derived public key (NOT the master key directly)
  // REQUIRED for ENCRYPTED_BATCH mode, EMPTY for IMMEDIATE mode
  bytes encrypted_payload = 10;

  // Target shield epoch for batch execution
  // Must be current epoch (ops execute at next epoch boundary)
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

### DKG via Vote Extensions

Validators participate in the DKG ceremony and submit decryption shares via **CometBFT Vote Extensions** rather than standalone `Msg` transactions. This eliminates separate transaction overhead and leverages the existing consensus participation mechanism.

**How it works:**

1. **ExtendVote / VerifyVoteExtension**: Each validator embeds DKG-related cryptographic material in their consensus vote extensions. The `DKGVoteExtensionHandler` (in `x/shield/abci/vote_extensions.go`) builds the appropriate extension based on the current DKG phase:

   - **REGISTERING phase**: Validators embed their BN256 G1 public key + Schnorr proof of possession (PoP) over their operator address.
   - **CONTRIBUTING phase**: Validators embed Feldman commitments + ECIES-encrypted polynomial evaluations (one per other validator) + PoP.
   - **ACTIVE phase**: When pending encrypted ops exist, validators embed per-epoch decryption shares (`masterShare_i * H_to_G1("shield_epoch" || epoch)`).

2. **PrepareProposalHandler**: The block proposer aggregates all DKG vote extensions from the previous block's commit into a single `InjectedDKGData` pseudo-transaction, placed at position 0 of the block. If no DKG is active or no extensions are present, the block is unmodified.

3. **ProcessProposalHandler**: Validators verify the DKG injection in tx[0] is well-formed (valid round/phase, valid G1 points, correct commitment counts). Invalid injections cause proposal rejection.

4. **PreBlocker**: `ProcessDKGInjection` (in `x/shield/abci/preblocker.go`) extracts the `InjectedDKGData` from tx[0], maps each validator's consensus address to their operator address via the staking keeper, and processes the data:
   - For REGISTERING: stores public keys as DKG registrations, verifies Schnorr PoP
   - For CONTRIBUTING: stores Feldman commitments + encrypted evaluations as DKG contributions, updates contribution count
   - For ACTIVE: stores decryption shares, attempts epoch key reconstruction when threshold is met
   - Strips tx[0] from the transaction list so it does not go through normal tx delivery

**Vote extension data format:**

```protobuf
message DKGVoteExtension {
  uint64 round = 1;
  DKGPhase phase = 2;
  // REGISTERING phase
  bytes registration_pub_key = 3;   // BN256 G1 point
  bytes registration_pop = 4;       // Schnorr PoP over operator address
  // CONTRIBUTING phase
  repeated bytes feldman_commitments = 5;
  repeated EncryptedEvaluation encrypted_evaluations = 6;
  bytes contribution_pop = 7;
  // ACTIVE phase (decryption shares)
  uint64 decryption_epoch = 8;
  bytes decryption_share = 9;       // epoch-derived share: masterShare_i * H_to_G1(epoch)
}
```

**Share verification**: Decryption shares are verified against the validator's public key share stored in `TLEKeySet` using a pairing check:

```
Verification equation:
  epoch_tag = H_to_G1("shield_epoch" || epoch)
  e(epoch_share_i, G2) == e(epoch_tag, pubShare_i)

This proves epoch_share_i = secretShare_i * epoch_tag without revealing secretShare_i.
If the check fails, the share is rejected (ErrInvalidDecryptionShare).
```

Late decryption shares are accepted for up to `max_pending_epochs` past epochs (to allow decryption of carried-over operations).

**DKG lifecycle (vote extension-based):**

1. **INACTIVE**: No DKG ceremony in progress. BeginBlocker monitors the bonded validator set — when the count reaches `min_tle_validators` (param field 16), a DKG ceremony auto-opens.
2. **REGISTERING**: Validators embed BN256 G1 public keys + Schnorr PoP in their vote extensions. PrepareProposalHandler aggregates these into `InjectedDKGData` pseudo-transactions. PreBlocker processes registrations and stores public keys. When all expected validators have registered (or `dkg_window_blocks / 2` blocks have elapsed), the phase advances to CONTRIBUTING.
3. **CONTRIBUTING**: Validators embed Feldman commitments + ECIES-encrypted polynomial evaluations + PoP in vote extensions. PreBlocker processes contributions and tracks the count. When `ceil(threshold_numerator / threshold_denominator * totalValidators)` contributions are received, the master public key is computed from the Feldman commitments (constant terms) and stored in `TLEKeySet`.
4. **ACTIVE**: DKG complete. Encrypted batch mode becomes available. Validators embed per-epoch decryption shares in vote extensions when pending ops exist. PreBlocker stores shares and attempts epoch key reconstruction when threshold is met.

**Validator set drift:**
- BeginBlocker monitors `max_validator_set_drift` (param field 18) — the percentage of original DKG participants no longer in the bonded set. If drift exceeds the threshold (default 33%), a new DKG ceremony is automatically triggered (`EventTypeShieldValidatorDrift`).
- **Validator leaves**: Their shares are no longer available for future epochs. Threshold must still be met with remaining validators. If the active validator set drops below threshold, encrypted batch mode gracefully degrades (ops carry over, eventually expire).

### MsgTriggerDKG (Governance)

Triggers a new DKG ceremony. Used when the validator set changes significantly (>1/3 turnover) or when the existing key set needs rotation. Resets the `TLEKeySet` and opens a new registration window. Encrypted batch mode is disabled until the new DKG completes.

```protobuf
message MsgTriggerDKG {
  option (cosmos.msg.v1.signer) = "authority";
  string authority = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  // Optional: new threshold numerator (0 = keep current)
  uint64 threshold_numerator = 2;
  // Optional: new threshold denominator (0 = keep current)
  uint64 threshold_denominator = 3;
}

message MsgTriggerDKGResponse {}
```

**Effect**: Clears `TLEKeySet.validator_shares`, sets `encrypted_batch_enabled = false` in params until threshold met. Existing pending ops from the old key set are marked for expiry (they cannot be decrypted with the new key). Validators automatically re-register via vote extensions when the new DKG round opens in REGISTERING phase.

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

### MsgDeregisterShieldedOp (Governance)

Fully removes a shielded operation registration from state. Unlike setting `active = false` (which preserves state for potential reactivation), deregistration deletes the entry entirely.

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

  // TLEMasterPublicKey returns the TLE master public key (for client encryption)
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

  // DKGState returns the current DKG ceremony state
  rpc DKGState(QueryDKGStateRequest) returns (QueryDKGStateResponse);

  // DKGContributions returns all DKG contributions for the current round
  rpc DKGContributions(QueryDKGContributionsRequest) returns (QueryDKGContributionsResponse);
}
```

## Ante Handler: ShieldGasDecorator

The critical piece that enables gasless submission. Inserted into the ante handler chain:

```go
func (d ShieldGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
    msgs := tx.GetMsgs()

    // Check if any message is MsgShieldedExec
    hasShieldedExec := false
    for _, msg := range msgs {
        if _, ok := msg.(*shieldtypes.MsgShieldedExec); ok {
            hasShieldedExec = true
            break
        }
    }

    if !hasShieldedExec {
        return next(ctx, tx, simulate)
    }

    // REJECT multi-message transactions containing MsgShieldedExec.
    // Bundling MsgShieldedExec with other messages (e.g. MsgSend) would:
    // 1. Deanonymize the shielded exec (the other message reveals the submitter's identity)
    // 2. Allow piggybacking non-shield messages on module-paid gas
    if len(msgs) != 1 {
        return ctx, shieldtypes.ErrMultiMsgNotAllowed
    }

    // Check shield module is enabled
    params, err := d.shieldKeeper.GetShieldParams(ctx)
    if err != nil {
        return ctx, err
    }
    if !params.Enabled {
        return ctx, shieldtypes.ErrShieldDisabled
    }

    // Calculate fee from gas
    feeTx, ok := tx.(sdk.FeeTx)
    if !ok {
        return ctx, sdkerrors.ErrTxDecode
    }
    fees := feeTx.GetFee()

    if fees.IsZero() {
        // No fees to pay — proceed (gas is still metered)
        ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)
        return next(ctx, tx, simulate)
    }

    // Deduct fees from shield module account → fee collector
    err = d.bankKeeper.SendCoinsFromModuleToModule(
        ctx,
        shieldtypes.ModuleName,       // source: shield module
        authtypes.FeeCollectorName,    // dest: fee collector
        fees,
    )
    if err != nil {
        return ctx, shieldtypes.ErrShieldGasDepleted
    }

    // Set fee-paid flag so the standard DeductFeeDecorator skips this tx
    ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)
    return next(ctx, tx, simulate)
}
```

The decorator is placed **before** the standard `DeductFeeDecorator` in the ante chain. The standard decorator checks the `ContextKeyFeePaid` flag and skips deduction if already handled.

### Single-Message Enforcement

`MsgShieldedExec` transactions **must** contain exactly one message. This prevents a user from bundling a shielded exec (gas-free) with non-shielded messages (piggybacking free gas). The ante handler rejects multi-message transactions containing any `MsgShieldedExec`.

## BeginBlocker: Auto-Funding

```go
func (k Keeper) BeginBlocker(ctx context.Context) error {
    sdkCtx := sdk.UnwrapSDKContext(ctx)
    params := k.GetParams(ctx)

    if !params.Enabled {
        return nil
    }

    // Check current balance
    moduleAddr := k.authKeeper.GetModuleAddress(types.ModuleName)
    balance := k.bankKeeper.GetBalance(sdkCtx, moduleAddr, "uspark")

    // Only fund if below minimum reserve
    if balance.Amount.GTE(params.MinGasReserve) {
        return nil
    }

    // Calculate how much to fund (up to the gap, capped by daily limit)
    gap := params.MinGasReserve.Sub(balance.Amount)

    // Check how much has already been funded today (day = block_height / 14400)
    day := uint64(sdkCtx.BlockHeight()) / 14400
    funded := k.GetDayFunding(ctx, day)
    remaining := params.MaxFundingPerDay.Sub(funded)

    if remaining.IsZero() || !remaining.IsPositive() {
        sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
            types.EventShieldFundingCapReached,
            sdk.NewAttribute(types.AttributeKeyDay, fmt.Sprintf("%d", day)),
            sdk.NewAttribute(types.AttributeKeyTotalFunded, funded.String()),
            sdk.NewAttribute(types.AttributeKeyCap, params.MaxFundingPerDay.String()),
        ))
        return nil // daily cap reached
    }

    // Fund the lesser of gap and remaining cap
    fundAmount := math.MinInt(gap, remaining)

    // Transfer from community pool (distribution module) to shield module
    coins := sdk.NewCoins(sdk.NewCoin("uspark", fundAmount))
    err := k.distrKeeper.DistributeFromFeePool(ctx, coins, moduleAddr)
    if err != nil {
        // Community pool may be empty — not fatal, just log
        sdkCtx.Logger().With("module", "x/shield").Info(
            "Failed to fund shield module from community pool",
            "requested", fundAmount.String(),
            "err", err,
        )
        return nil
    }

    // Record funding for daily cap tracking
    k.SetDayFunding(ctx, day, funded.Add(fundAmount))

    sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
        types.EventTypeShieldFunded,
        sdk.NewAttribute(types.AttributeKeyAmount, fundAmount.String()),
        sdk.NewAttribute(types.AttributeKeyDay, fmt.Sprintf("%d", day)),
        sdk.NewAttribute(types.AttributeKeyNewBalance, balance.Amount.Add(fundAmount).String()),
    ))

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

- **Curve**: BN256 (alt_bn128) G1
- **Encryption**: Boneh-Franklin IBE (Identity-Based Encryption) on BN256 pairing
- **Secret sharing**: Shamir's Secret Sharing over the BN256 scalar field
- **Hash function**: MiMC (for Merkle trees, consistent with ZK circuits)
- **Key reconstruction**: Lagrange interpolation over Shamir shares

### DKG (Distributed Key Generation)

Validators participate in a **Feldman VSS** (Verifiable Secret Sharing) ceremony to produce:
1. A **master public key** (BN256 G1 point) — stored on-chain, used by clients for encryption
2. Per-validator **secret key shares** (Shamir shares) — held privately by each validator
3. Per-validator **public key shares** (BN256 G1 points) — stored on-chain for share verification
4. **Polynomial commitments** — public commitments to the dealer's polynomial coefficients, enabling verification

The DKG ceremony runs at chain genesis and after significant validator set changes. x/shield stores the master public key and public key shares; validators hold their secret shares locally.

**Why Feldman VSS**: Simple PoP (proof of possession) alone is insufficient to prevent rogue key attacks in aggregated key schemes. Feldman VSS provides verifiable shares where each validator can independently verify that their share is consistent with the committed polynomial, without a trusted dealer. The master public key is the polynomial's constant term commitment (`A_0 = s * G`), and each public share satisfies `pubShare_i = Σ_j(A_j * i^j)` where `A_j` are the polynomial commitments.

```protobuf
// Stored in x/shield state
message TLEKeySet {
  bytes master_public_key = 1;                // BN256 G1 point
  uint64 threshold_numerator = 2;             // e.g. 2
  uint64 threshold_denominator = 3;           // e.g. 3 → 2/3 threshold
  repeated TLEValidatorPublicShare validator_shares = 4;
  int64 created_at_height = 5;
}

message TLEValidatorPublicShare {
  string validator_address = 1;
  bytes  public_share = 2;                    // BN256 G1 point
  uint32 share_index = 3;                     // Shamir share index
}
```

### Shield Epoch Lifecycle

```
Shield Epoch N                           Shield Epoch N+1
─────────────────────────────────────    ─────────────────────
│                                   │    │
│  Users submit ENCRYPTED_BATCH ops │    │  Validators submit decryption shares
│  target_epoch = N                 │    │  for epoch N (first few blocks)
│  Ops accumulate in pending queue  │    │
│                                   │    │  Threshold met → reconstruct key
│                                   │    │  → Decrypt all epoch N pending ops
│                                   │    │  → Verify ZK proofs
│                                   │    │  → Shuffle execution order
│                                   │    │  → Execute batch
│                                   │    │
├───────────────────────────────────┤    ├─────────────────────
  shield_epoch_interval blocks              shield_epoch_interval blocks
```

### Validator Overhead

With `shield_epoch_interval = 50` blocks (~5 minutes):

- Validators embed decryption shares in their consensus vote extensions — zero additional transaction overhead
- Shares ride along with existing CometBFT votes (~100 bytes of vote extension data per epoch)
- No sidecar process needed — the `DKGVoteExtensionHandler` is built into the node binary

**Liveness enforcement**: x/shield tracks its own TLE liveness. Validators who miss decryption share submissions are tracked via `tle_miss_window` / `tle_miss_tolerance` parameters. Missing too many windows results in jailing (same severity as missing consensus blocks).

TLE liveness parameters are defined directly in `Params` (fields 13-15: `tle_miss_window`, `tle_miss_tolerance`, `tle_jail_duration`). There is no separate `TLELivenessParams` message — keeping them in `Params` allows standard governance parameter updates via `MsgUpdateParams`.

### Per-Epoch Key Derivation (Forward Secrecy)

**Critical**: x/shield does NOT encrypt directly with the master public key. Doing so would mean every epoch's decryption key is the master secret itself — revealing it once would compromise all past and future encrypted payloads.

Instead, x/shield uses an IBE-style (Identity-Based Encryption) construction where each epoch has a unique derived key:

```
Epoch key derivation:
  epoch_tag       = H_to_G1("shield_epoch" || epoch_number)    // hash-to-curve
  epoch_pub_key   = e(masterPubKey, epoch_tag)                  // BN256 pairing
  epoch_share_i   = masterShare_i * epoch_tag                   // validator computes per-epoch
  epoch_secret    = Lagrange_reconstruct(epoch_shares)          // = masterSK * epoch_tag
```

**Properties:**
- Each epoch has a unique encryption/decryption key pair
- Revealing epoch N's key reveals NOTHING about epoch N±1
- Validators derive epoch shares from their master shares — no new DKG per epoch
- Forward secrecy: past epoch keys cannot decrypt future epochs
- Backward secrecy: future epoch keys cannot decrypt past epochs

### Encryption Format

Clients encrypt the payload using a Boneh-Franklin IBE-style construction with the epoch-derived public key:

```
Encryption (client-side):
  epoch_tag     = H_to_G1("shield_epoch" || target_epoch)
  gID           = e(masterPubKey, epoch_tag)           // GT element, precomputable per-epoch
  r             = random scalar
  U             = r * G1_generator                     // ephemeral G1 point
  symKey        = H_kdf(gID^r)                         // derive symmetric key from GT element
  V             = AES-GCM.Encrypt(symKey, plaintext)   // authenticated encryption

  encrypted_payload = encode(U, V)

Decryption (chain-side, at batch execution):
  epochSecret   = Lagrange_reconstruct(epoch_shares)   // = masterSK * epoch_tag (G1 point)
  gID_r         = e(U, epochSecret)                    // = e(r*G, s*epoch_tag) = gID^r (GT element)
  symKey        = H_kdf(gID_r)                         // same symmetric key
  plaintext     = AES-GCM.Decrypt(symKey, V)

plaintext = inner_message_bytes || proof_bytes || inner_message_type_url_bytes
```

The `encrypted_payload` is self-contained: after decryption, the chain can reconstruct the full inner message and ZK proof for verification and execution.

**Why IBE over ECIES**: Standard ECIES operates on group elements (G1/G2 points) with Diffie-Hellman. Here, the epoch "public key" is a GT pairing result, not a curve point. The IBE construction naturally handles GT elements: the client computes `gID = e(masterPubKey, epoch_tag)` in GT, raises it to a random power, and derives a symmetric key. The chain reconstructs the same GT element using the epoch decryption key and the ephemeral point U.

**Client-side**: Clients query `TLEMasterPublicKey` and compute the epoch-specific `gID` locally. No per-epoch key material needs to be fetched from the chain.

### Decryption Key Reconstruction

Lagrange interpolation over per-epoch validator shares:

```
// Each validator computes: epoch_share_i = masterShare_i * H_to_G1("shield_epoch" || epoch)
// Reconstructed epoch key:
epochDecryptionKey = Lagrange_reconstruct(epoch_shares)  // = masterSK * epoch_tag
```

The threshold is `ceil(threshold_numerator / threshold_denominator * registeredValidators)` (default 2/3).

**Important**: Validators submit epoch-derived shares, NOT their raw master shares. Submitting a raw master share would be a slashable offense (it compromises all epochs).

### Graceful Degradation

If the decryption key is NOT produced for a shield epoch (insufficient validator participation):

1. Pending ops from that epoch carry over to the next epoch
2. Carried-over ops **cannot** be decrypted by a different epoch's key (each epoch's key is unique)
3. When a subsequent epoch's key IS produced, carried-over ops remain encrypted until their original epoch key is eventually reconstructed (late validator share submissions are accepted for up to `max_pending_epochs` epochs)
4. If ops hit `max_pending_epochs` without their epoch's decryption key, they are dropped and an `EventShieldBatchExpired` is emitted
5. Immediate mode operations continue working normally regardless of TLE status

### Late Share Submission

To handle carried-over ops, validators can embed decryption shares for past epochs (up to `max_pending_epochs` old) in their vote extensions. The PreBlocker processes these late shares and attempts epoch key reconstruction even when the threshold was not met at the epoch boundary. Shares for epochs older than `currentEpoch - max_pending_epochs` are ignored.

## EndBlocker: Batch Execution

The EndBlocker checks at each block whether a batch should execute:

```go
func (k Keeper) EndBlocker(ctx context.Context) error {
    sdkCtx := sdk.UnwrapSDKContext(ctx)
    params := k.GetParams(ctx)

    if !params.EncryptedBatchEnabled {
        return nil
    }

    epochState := k.GetShieldEpochState(ctx)
    currentHeight := sdkCtx.BlockHeight()

    // Check if we've crossed an epoch boundary
    if currentHeight < epochState.EpochStartHeight + int64(params.ShieldEpochInterval) {
        return nil // not yet at boundary
    }

    // Advance to next epoch
    newEpoch := epochState.CurrentEpoch + 1
    k.SetShieldEpochState(ctx, types.ShieldEpochState{
        CurrentEpoch:    newEpoch,
        EpochStartHeight: currentHeight,
    })

    // Try to process the PREVIOUS epoch's pending ops
    prevEpoch := epochState.CurrentEpoch
    k.tryProcessBatch(ctx, params, prevEpoch, currentHeight)

    // Also try to process any carried-over ops from older epochs
    k.processCarriedOverBatches(ctx, params, prevEpoch, currentHeight)

    // Prune stale state (rate limits, old nullifiers, day fundings, decryption state)
    k.pruneStaleState(ctx, newEpoch)

    return nil
}

func (k Keeper) tryProcessBatch(ctx context.Context, params types.Params, epoch uint64, currentHeight int64) {
    sdkCtx := sdk.UnwrapSDKContext(ctx)

    // Get decryption key for this epoch
    decKey, found := k.GetShieldEpochDecryptionKey(ctx, epoch)
    if !found {
        return // validators haven't produced the key yet; ops carry over
    }

    // Collect all pending ops for this epoch
    pendingOps := k.GetPendingOpsForEpoch(ctx, epoch)

    if len(pendingOps) == 0 {
        return
    }

    // Check min_batch_size (unless max_pending_epochs forces execution)
    oldestSubmittedEpoch := k.getOldestPendingEpoch(pendingOps)
    epochsWaiting := epoch - oldestSubmittedEpoch
    forceExecute := epochsWaiting >= uint64(params.MaxPendingEpochs)

    if len(pendingOps) < int(params.MinBatchSize) && !forceExecute {
        return // batch too small, carry over (unless forced)
    }

    // Limit ops processed per block to bound EndBlocker gas consumption.
    // Remaining ops stay in the queue and are processed in subsequent blocks.
    if uint32(len(pendingOps)) > params.MaxOpsPerBatch {
        pendingOps = pendingOps[:params.MaxOpsPerBatch]
    }

    // Decrypt all payloads
    var decryptedOps []decryptedOp
    for _, op := range pendingOps {
        // IBE decryption: parse U (G1 point) and V (AES-GCM ciphertext) from payload,
        // compute gID_r = e(U, epochSecret), derive symKey = H_kdf(gID_r),
        // then plaintext = AES-GCM.Decrypt(symKey, V)
        plaintext, err := ibe.Decrypt(decKey.DecryptionKey, op.EncryptedPayload)
        if err != nil {
            // Decryption failed — corrupted or invalid payload, drop it
            k.DeletePendingOp(ctx, op.Id)
            continue
        }
        innerMsg, proof, err := decodePayload(plaintext)
        if err != nil {
            k.DeletePendingOp(ctx, op.Id)
            continue
        }
        decryptedOps = append(decryptedOps, decryptedOp{
            pending: op,
            innerMsg: innerMsg,
            proof: proof,
        })
    }

    // Shuffle execution order using block hash as entropy
    // Deterministic but unpredictable at submission time
    shuffleSeed := sha256(sdkCtx.BlockHeader().LastBlockHash, epoch)
    shuffled := deterministicShuffle(decryptedOps, shuffleSeed)

    // Execute each operation
    executed := 0
    for _, op := range shuffled {
        err := k.executeDecryptedOp(ctx, params, op)
        if err != nil {
            sdkCtx.Logger().With("module", "x/shield").Info(
                "Batch op execution failed",
                "op_id", op.pending.Id,
                "err", err,
            )
        } else {
            executed++
        }
        // Clean up pending op AND its pending nullifier to prevent unbounded state growth
        k.DeletePendingOp(ctx, op.pending.Id)
        k.DeletePendingNullifier(ctx, hex.EncodeToString(op.pending.Nullifier))
    }

    // Emit batch execution event
    sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
        types.EventTypeShieldBatchExecuted,
        sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", epoch)),
        sdk.NewAttribute(types.AttributeKeyBatchSize, fmt.Sprintf("%d", len(pendingOps))),
        sdk.NewAttribute(types.AttributeKeyExecuted, fmt.Sprintf("%d", executed)),
        sdk.NewAttribute(types.AttributeKeyDropped, fmt.Sprintf("%d", len(pendingOps)-executed)),
    ))
}

func (k Keeper) executeDecryptedOp(ctx context.Context, params types.Params, op decryptedOp) error {
    // 1. Look up registered operation
    reg, found := k.GetShieldedOp(ctx, op.innerMsg.TypeUrl)
    if !found || !reg.Active {
        return types.ErrUnregisteredOperation
    }

    // 2. Validate batch mode allows encrypted batch execution.
    //    Operations registered as IMMEDIATE_ONLY must not execute in batch mode.
    //    This check is deferred from submission (inner msg was encrypted).
    if reg.BatchMode == types.SHIELD_BATCH_MODE_IMMEDIATE_ONLY {
        return types.ErrEncryptedBatchNotAllowed
    }

    // 3. Validate minimum trust level meets registration requirement.
    //    The ZK proof only proves "my trust >= op.pending.MinTrustLevel".
    //    If the submitter claimed a lower trust level than the registration requires,
    //    the proof passes but the operation must still be rejected.
    if op.pending.MinTrustLevel < reg.MinTrustLevel {
        return types.ErrInsufficientTrustLevel
    }

    // 4. Verify ZK proof (now decrypted and readable) — x/shield owns verification
    if err := k.verifyProofForBatch(ctx, op); err != nil {
        return types.ErrInvalidProof
    }

    // 5. Verify inner message signer is shield module account
    innerMsg, err := k.decodeInnerMessage(op.innerMsg)
    if err != nil {
        return types.ErrInvalidInnerMessage
    }
    signers, _, _ := k.cdc.GetMsgSigners(innerMsg)
    moduleAddr := k.authKeeper.GetModuleAddress(types.ModuleName)
    if !bytes.Equal(signers[0], moduleAddr) {
        return types.ErrInvalidInnerMessageSigner
    }

    // 6. Verify target module implements ShieldAware and accepts this message type
    shieldAware, ok := k.getShieldAware(op.innerMsg.TypeUrl)
    if !ok || !shieldAware.IsShieldCompatible(ctx, innerMsg) {
        return types.ErrIncompatibleOperation
    }

    // 7. Check and record nullifier in permanent UsedNullifiers store.
    //    PendingNullifier dedup at submission only catches duplicates within the queue —
    //    a nullifier previously used in immediate mode must also be rejected here.
    sdkCtx := sdk.UnwrapSDKContext(ctx)
    nullifierHex := hex.EncodeToString(op.pending.Nullifier)
    scope := k.resolveNullifierScope(ctx, reg, &types.MsgShieldedExec{InnerMessage: op.innerMsg})
    if k.IsNullifierUsed(ctx, reg.NullifierDomain, scope, nullifierHex) {
        return types.ErrNullifierUsed
    }
    k.RecordNullifier(ctx, reg.NullifierDomain, scope, nullifierHex, sdkCtx.BlockHeight())

    // 8. Execute with gas limit
    childCtx := sdkCtx.WithGasMeter(storetypes.NewGasMeter(params.MaxGasPerExec))
    _, err = handler(childCtx, innerMsg)
    return err
}
```

### Batch Execution Order: Deterministic Shuffle

Within a batch, operations are shuffled to prevent:

1. **Validator ordering attacks**: A malicious block proposer cannot control which operation executes first to game state
2. **Submission order leakage**: If ops executed in FIFO order, an observer could correlate early-submitted ops with early-executing results
3. **Front-running**: No operation can reliably execute before another in the same batch

The shuffle uses `SHA256(last_block_hash || epoch)` as the seed for a Fisher-Yates shuffle. The last block hash is not known at submission time, making the ordering unpredictable to submitters.

**Block proposer influence**: The block proposer knows `last_block_hash` before proposing and could weakly influence it by including/excluding transactions. However, the impact is minimal — all operations in a batch execute atomically in the same block, so controlling execution order doesn't enable front-running or state manipulation. The proposer also cannot predict which operations are in the batch (they're encrypted).

### Expired Operations

Operations that hit `max_pending_epochs` without being executed (due to sustained TLE failure or persistent small batch sizes) are dropped:

```go
func (k Keeper) processCarriedOverBatches(ctx context.Context, params types.Params, currentEpoch uint64, currentHeight int64) {
    sdkCtx := sdk.UnwrapSDKContext(ctx)

    // Guard against underflow on early chain startup.
    var cutoffEpoch uint64
    if currentEpoch > uint64(params.MaxPendingEpochs) {
        cutoffEpoch = currentEpoch - uint64(params.MaxPendingEpochs)
    }

    // 1. Try to process older epochs that now have late-arrived decryption keys.
    //    Iterate from cutoffEpoch to prevEpoch-1 (prevEpoch was already handled by tryProcessBatch).
    for epoch := cutoffEpoch; epoch < currentEpoch; epoch++ {
        if _, found := k.GetShieldEpochDecryptionKey(ctx, epoch); found {
            pendingOps := k.GetPendingOpsForEpoch(ctx, epoch)
            if len(pendingOps) > 0 {
                k.tryProcessBatch(ctx, params, epoch, currentHeight)
            }
        }
    }

    // 2. Expire ops from epochs older than max_pending_epochs that still have no key.
    expiredOps := k.GetPendingOpsBeforeEpoch(ctx, cutoffEpoch)
    for _, op := range expiredOps {
        k.DeletePendingOp(ctx, op.Id)
        k.DeletePendingNullifier(ctx, hex.EncodeToString(op.Nullifier))
    }

    if len(expiredOps) > 0 {
        sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
            types.EventTypeShieldBatchExpired,
            sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", len(expiredOps))),
            sdk.NewAttribute(types.AttributeKeyCutoffEpoch, fmt.Sprintf("%d", cutoffEpoch)),
        ))
    }
}
```

## State Pruning

The EndBlocker performs periodic pruning to prevent unbounded state growth. Pruning runs once per epoch boundary (same trigger as batch processing):

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
    if cutoffEpoch > 0 {
        k.PruneEpochScopedNullifiers(ctx, cutoffEpoch)
    }

    // 3. Prune old DayFunding entries (keep current and previous day only).
    currentDay := uint64(sdkCtx.BlockHeight()) / 14400
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
// In EndBlocker, after tryProcessBatch and processCarriedOverBatches:
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
| `ShieldDecryptionKeys` | Every epoch boundary | `max_pending_epochs` |
| `ShieldDecryptionShares` | Every epoch boundary | `max_pending_epochs` |

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

## Execution Flow

### MsgShieldedExec Handler

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

// handleImmediate verifies the ZK proof and executes the inner message immediately.
func (k msgServer) handleImmediate(ctx sdk.Context, params types.Params, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
    // 1. Look up registered operation
    typeURL := msg.InnerMessage.TypeUrl
    reg, found := k.GetShieldedOp(ctx, typeURL)
    if !found {
        return nil, types.ErrUnregisteredOperation
    }
    if !reg.Active {
        return nil, types.ErrOperationInactive
    }

    // 2. Validate batch mode allows immediate
    if reg.BatchMode == types.SHIELD_BATCH_MODE_ENCRYPTED_ONLY {
        return nil, types.ErrImmediateNotAllowed
    }

    // 3. Validate proof domain matches registration
    if msg.ProofDomain != reg.ProofDomain {
        return nil, types.ErrProofDomainMismatch
    }

    // 4. Validate minimum trust level meets requirement
    if msg.MinTrustLevel < reg.MinTrustLevel {
        return nil, types.ErrInsufficientTrustLevel
    }

    // 5. Resolve nullifier scope and verify ZK proof
    scope := k.resolveNullifierScope(ctx, reg, msg)
    if err := k.verifyProof(ctx, msg, scope); err != nil {
        return nil, err
    }

    // 6. Check and record nullifier
    nullifierHex := hex.EncodeToString(msg.Nullifier)
    if k.IsNullifierUsed(ctx, reg.NullifierDomain, scope, nullifierHex) {
        return nil, types.ErrNullifierUsed
    }
    k.RecordNullifier(ctx, reg.NullifierDomain, scope, nullifierHex, ctx.BlockHeight())

    // 7. Check per-identity rate limit (uses rate_limit_nullifier, NOT the operation nullifier)
    // rate_limit_nullifier = H(secret, "rate_limit", epoch) — same for all ops by same member in same epoch
    rateLimitHex := hex.EncodeToString(msg.RateLimitNullifier)
    if !k.CheckAndIncrementRateLimit(ctx, rateLimitHex, params.MaxExecsPerIdentityPerEpoch) {
        return nil, types.ErrRateLimitExceeded
    }

    // 8. Decode, validate signer, check ShieldAware interface, and execute inner message
    resp, err := k.executeInnerMessage(ctx, params, msg.InnerMessage)
    if err != nil {
        return nil, err
    }

    // 9. Emit event (no identity information)
    ctx.EventManager().EmitEvent(sdk.NewEvent(
        types.EventTypeShieldedExec,
        sdk.NewAttribute(types.AttributeKeyMessageType, typeURL),
        sdk.NewAttribute(types.AttributeKeyNullifierDomain, fmt.Sprintf("%d", reg.NullifierDomain)),
        sdk.NewAttribute(types.AttributeKeyNullifierHex, nullifierHex),
        sdk.NewAttribute(types.AttributeKeyExecMode, "immediate"),
    ))

    return &types.MsgShieldedExecResponse{InnerResponse: resp}, nil
}

// handleEncryptedBatch validates cleartext fields and queues the encrypted payload.
func (k msgServer) handleEncryptedBatch(ctx sdk.Context, params types.Params, msg *types.MsgShieldedExec) (*types.MsgShieldedExecResponse, error) {
    if !params.EncryptedBatchEnabled {
        return nil, types.ErrEncryptedBatchDisabled
    }

    // 1. Reject cleartext fields — in encrypted batch mode, inner_message and proof
    //    must be empty. Including them alongside encrypted_payload leaks the operation
    //    in cleartext, defeating the privacy guarantees of batch mode.
    if msg.InnerMessage != nil {
        return nil, types.ErrCleartextFieldInBatchMode
    }
    if len(msg.Proof) > 0 {
        return nil, types.ErrCleartextFieldInBatchMode
    }

    // 2. Validate encrypted payload
    if len(msg.EncryptedPayload) == 0 {
        return nil, types.ErrMissingEncryptedPayload
    }
    if uint32(len(msg.EncryptedPayload)) > params.MaxEncryptedPayloadSize {
        return nil, types.ErrPayloadTooLarge
    }

    // 3. Validate target epoch is current
    epochState := k.GetShieldEpochState(ctx)
    if msg.TargetEpoch != epochState.CurrentEpoch {
        return nil, types.ErrInvalidTargetEpoch
    }

    // 4. Check pending queue capacity
    pendingCount := k.GetPendingOpCount(ctx)
    if pendingCount >= uint64(params.MaxPendingQueueSize) {
        return nil, types.ErrPendingQueueFull
    }

    // 5. Validate merkle root (cleartext — can verify immediately)
    if err := k.validateMerkleRoot(ctx, msg.MerkleRoot, msg.ProofDomain); err != nil {
        return nil, err
    }

    // 6. Check nullifier not already used (cleartext — can verify immediately)
    nullifierHex := hex.EncodeToString(msg.Nullifier)
    // For encrypted batch, use a generic "pending" scope to prevent duplicate submissions
    // The actual domain-scoped nullifier check happens at batch execution after decryption
    if k.IsPendingNullifier(ctx, nullifierHex) {
        return nil, types.ErrNullifierUsed
    }
    k.RecordPendingNullifier(ctx, nullifierHex)

    // 7. Check per-identity rate limit (uses rate_limit_nullifier, same for all ops by same member)
    rateLimitHex := hex.EncodeToString(msg.RateLimitNullifier)
    if !k.CheckAndIncrementRateLimit(ctx, rateLimitHex, params.MaxExecsPerIdentityPerEpoch) {
        return nil, types.ErrRateLimitExceeded
    }

    // 8. Store pending operation
    opID := k.NextPendingOpID(ctx)
    k.SetPendingOp(ctx, types.PendingShieldedOp{
        Id:                opID,
        TargetEpoch:       msg.TargetEpoch,
        Nullifier:         msg.Nullifier,
        MerkleRoot:        msg.MerkleRoot,
        ProofDomain:       msg.ProofDomain,
        MinTrustLevel:     msg.MinTrustLevel,
        EncryptedPayload:  msg.EncryptedPayload,
        SubmittedAtHeight: ctx.BlockHeight(),
        SubmittedAtEpoch:  epochState.CurrentEpoch,
    })

    // 9. Emit queued event
    ctx.EventManager().EmitEvent(sdk.NewEvent(
        types.EventTypeShieldedQueued,
        sdk.NewAttribute(types.AttributeKeyPendingOpId, fmt.Sprintf("%d", opID)),
        sdk.NewAttribute(types.AttributeKeyTargetEpoch, fmt.Sprintf("%d", msg.TargetEpoch)),
        sdk.NewAttribute(types.AttributeKeyNullifierHex, nullifierHex),
        sdk.NewAttribute(types.AttributeKeyExecMode, "encrypted_batch"),
    ))

    return &types.MsgShieldedExecResponse{PendingOpId: opID}, nil
}
```

### Nullifier Scope Resolution

The scope determines the granularity of nullifier uniqueness:

```go
func (k Keeper) resolveNullifierScope(ctx sdk.Context, reg types.ShieldedOpRegistration, msg *types.MsgShieldedExec) uint64 {
    switch reg.NullifierScopeType {
    case types.NULLIFIER_SCOPE_EPOCH:
        return k.GetCurrentEpoch(ctx)
    case types.NULLIFIER_SCOPE_MESSAGE_FIELD:
        // Extract scope from inner message using the registered field path
        return k.extractScopeFromMessage(msg.InnerMessage, reg.ScopeFieldPath)
    case types.NULLIFIER_SCOPE_GLOBAL:
        return 0
    default:
        return k.GetCurrentEpoch(ctx)
    }
}
```

**Scope field extraction**: When `nullifier_scope_type = MESSAGE_FIELD`, the `ShieldedOpRegistration` includes a `scope_field_path` (e.g., `"post_id"`, `"proposal_id"`, `"collection_id"`). The keeper uses proto reflection to extract the named uint64 field from the decoded inner message. If the field is missing or not a uint64, the operation is rejected with `ErrInvalidInnerMessage`.

For encrypted batch mode, the scope cannot be extracted at submission time (inner message is encrypted). The scope check is deferred to batch execution time in `executeDecryptedOp`, where the decrypted inner message is available.

## Keeper Interface

### Required Keeper Dependencies

x/shield uses a "late keepers" pattern to break depinject cycles. The `accountKeeper` and `bankKeeper` are injected via depinject. All other keepers (`repKeeper`, `distrKeeper`, `slashingKeeper`, `stakingKeeper`, `router`) are wired post-depinject via `Set*Keeper()` methods. This is tracked via a shared `lateKeepers` pointer so value-copies of Keeper (in AppModule, msgServer) see updates.

```go
type lateKeepers struct {
    repKeeper      types.RepKeeper      // Trust tree Merkle root snapshots
    distrKeeper    types.DistrKeeper    // Community pool funding
    slashingKeeper types.SlashingKeeper // TLE liveness jailing
    stakingKeeper  types.StakingKeeper  // Validator set iteration
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
    TLEKeySet            collections.Item[types.TLEKeySet]
    TLEMissCounters      collections.Map[string, uint64]  // key: validator_address → miss count

    // Collections — Encrypted batch mode
    PendingOps           collections.Map[uint64, types.PendingShieldedOp]  // key: op_id
    NextPendingOpId      collections.Sequence
    ShieldEpochState     collections.Item[types.ShieldEpochState]
    ShieldDecryptionKeys collections.Map[uint64, types.ShieldEpochDecryptionKey]  // key: epoch
    ShieldDecryptionShares collections.Map[collections.Pair[uint64, string], types.ShieldDecryptionShare]  // key: (epoch, validator)

    // Collections — DKG ceremony
    DKGState             collections.Item[types.DKGState]
    DKGContributions     collections.Map[string, types.DKGContribution]   // key: validator_address
    DKGRegistrations     collections.Map[string, types.DKGContribution]   // key: validator_address
}
```

### Expected Keeper Interfaces

```go
type AccountKeeper interface {
    GetModuleAddress(moduleName string) sdk.AccAddress
    GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
    GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI // used by simulation
}

type BankKeeper interface {
    GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
    SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
    SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

type DistrKeeper interface {
    DistributeFromFeePool(ctx context.Context, amount sdk.Coins, receiveAddr sdk.AccAddress) error
}

type RepKeeper interface {
    // Trust tree Merkle root snapshots for ZK proof root validation
    GetTrustTreeRoot(ctx context.Context) ([]byte, error)
    GetPreviousTrustTreeRoot(ctx context.Context) ([]byte, error)
}

type SlashingKeeper interface {
    Jail(ctx context.Context, consAddr sdk.ConsAddress) error
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

The genesis set covers all existing anonymous functionality on the chain:

#### x/blog

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgCreatePost` | TRUST_TREE | `anon_min_trust` | 1 | EPOCH | EITHER |
| `MsgCreateReply` | TRUST_TREE | `anon_min_trust` | 2 | MESSAGE_FIELD (post_id) | EITHER |
| `MsgReact` | TRUST_TREE | `anon_min_trust` | 8 | MESSAGE_FIELD (post_id) | EITHER |

#### x/forum

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgCreatePost` | TRUST_TREE | `anon_min_trust` | 11 | EPOCH | EITHER |
| `MsgUpvotePost` | TRUST_TREE | `anon_min_trust` | 12 | MESSAGE_FIELD (post_id) | EITHER |
| `MsgDownvotePost` | TRUST_TREE | `anon_min_trust` | 13 | MESSAGE_FIELD (post_id) | EITHER |

#### x/collect

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgCreateCollection` | TRUST_TREE | `anon_min_trust` | 21 | EPOCH | EITHER |
| `MsgUpvoteContent` | TRUST_TREE | `anon_min_trust` | 22 | MESSAGE_FIELD (target_id) | EITHER |
| `MsgDownvoteContent` | TRUST_TREE | `anon_min_trust` | 23 | MESSAGE_FIELD (target_id) | EITHER |

#### x/commons (Anonymous Governance)

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgSubmitAnonymousProposal` | TRUST_TREE | 0 | 31 | EPOCH | EITHER |
| `MsgAnonymousVoteProposal` | TRUST_TREE | 0 | 32 | MESSAGE_FIELD (proposal_id) | EITHER |

> **Note**: Batch mode is EITHER so these work in both immediate and encrypted batch modes. Immediate mode is needed while TLE/DKG is not yet active (`encrypted_batch_enabled=false`). Once TLE is production-ready, governance can tighten these to ENCRYPTED_ONLY for maximum sender unlinkability.

#### x/rep

| Message Type | Proof Domain | Min Trust | Null. Domain | Scope | Batch Mode |
|-------------|-------------|-----------|-------------|-------|------------|
| `MsgCreateChallenge` | TRUST_TREE | 0 | 41 | GLOBAL | ENCRYPTED_ONLY |

### Governance Control

- **Add operations**: Register new message types via `MsgRegisterShieldedOp` governance proposal
- **Deactivate operations**: Set `active = false` to disable without removing state
- **Remove operations**: Fully delete via `MsgDeregisterShieldedOp` governance proposal
- **Modify parameters**: Change proof domain, trust level, batch mode, or nullifier scoping per operation
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

Even if governance registers a message type, x/shield rejects it at execution time unless the target module explicitly opts in via `ShieldAware`. This is defense in depth — a malicious governance proposal cannot compromise non-privacy modules.

**Rationale for batch mode assignments:**
- **Content operations (blog, forum, collect)**: `EITHER` — users choose latency vs privacy. Most content doesn't need batch delay, but whistleblower content benefits from maximum privacy.
- **Governance operations (commons proposals/votes)**: `EITHER` — immediate mode is required while TLE/DKG is not yet active. Once TLE is production-ready, governance can tighten to `ENCRYPTED_ONLY` for maximum sender unlinkability.
- **Challenges (rep)**: `ENCRYPTED_ONLY` — challenges are inherently privacy-critical. The batch delay (one shield epoch, ~5 min) is acceptable for challenge submissions.

## Events

```protobuf
// --- Immediate mode events ---

// Emitted on every immediate shielded execution
EventShieldedExec {
  message_type:     string  // inner message type URL
  nullifier_domain: uint32
  nullifier_hex:    string  // for public nullifier tracking
  exec_mode:        string  // "immediate"
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
  epoch:      uint64
  batch_size: uint32   // total ops in batch
  executed:   uint32   // successfully executed
  dropped:    uint32   // failed proof verification or decode
}

// Emitted when batch execution is skipped (e.g. no pending ops, no decryption key)
EventShieldBatchSkipped {
  epoch:      uint64
}

// Emitted when pending ops expire without execution
EventShieldBatchExpired {
  count:        uint32  // number of expired ops
  cutoff_epoch: uint64
}

// Emitted when shield epoch decryption key is reconstructed
EventShieldDecryptionKeyAvailable {
  epoch:                    uint64
  shares_submitted:         uint32
  threshold_required:       uint32
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

// Emitted when a DKG ceremony is triggered (via governance or auto-trigger)
EventShieldDKGTriggered {
  dkg_round: uint64
}

// Emitted when a TLE key is registered (legacy, kept for backward compat)
EventShieldTLERegistered {
  validator: string
}

// Emitted when BeginBlocker auto-opens a DKG ceremony
EventShieldDKGOpened {
  dkg_round: uint64
  dkg_phase: string
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
  dkg_round: uint64
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

// Emitted when a validator misses a TLE share submission
EventTLEMiss {
  validator:  string
  miss_count: uint64
}

// Emitted when a validator is jailed for exceeding TLE miss tolerance
EventTLEJail {
  validator:    string
  jail_duration: string
}
```

## Errors

```go
var (
    // General
    ErrShieldDisabled           = errorsmod.Register(ModuleName, 2, "shielded execution is disabled")
    ErrShieldGasDepleted        = errorsmod.Register(ModuleName, 3, "shield module gas reserve depleted")
    ErrUnregisteredOperation    = errorsmod.Register(ModuleName, 4, "inner message type not registered for shielded execution")
    ErrOperationInactive        = errorsmod.Register(ModuleName, 5, "shielded operation is currently inactive")
    ErrProofDomainMismatch      = errorsmod.Register(ModuleName, 6, "proof domain does not match registered requirement")
    ErrInsufficientTrustLevel   = errorsmod.Register(ModuleName, 7, "proven trust level below required minimum")
    ErrInvalidProof             = errorsmod.Register(ModuleName, 8, "ZK proof verification failed")
    ErrInvalidProofDomain       = errorsmod.Register(ModuleName, 9, "unknown proof domain")
    ErrNullifierUsed            = errorsmod.Register(ModuleName, 10, "nullifier already used in this domain and scope")
    ErrRateLimitExceeded        = errorsmod.Register(ModuleName, 11, "per-identity rate limit exceeded for this epoch")
    ErrInvalidInnerMessage      = errorsmod.Register(ModuleName, 12, "inner message is invalid or cannot be decoded")
    ErrInvalidInnerMessageSigner = errorsmod.Register(ModuleName, 13, "inner message signer must be shield module account")
    ErrMultiMsgNotAllowed       = errorsmod.Register(ModuleName, 14, "MsgShieldedExec must be the only message in the transaction")

    // Execution mode
    ErrInvalidExecMode          = errorsmod.Register(ModuleName, 15, "invalid execution mode")
    ErrImmediateNotAllowed      = errorsmod.Register(ModuleName, 16, "this operation requires encrypted batch mode")
    ErrEncryptedBatchDisabled   = errorsmod.Register(ModuleName, 17, "encrypted batch mode is disabled")
    ErrEncryptedBatchNotAllowed = errorsmod.Register(ModuleName, 18, "this operation does not support encrypted batch mode")

    // Encrypted batch
    ErrMissingEncryptedPayload  = errorsmod.Register(ModuleName, 19, "encrypted_payload is required for encrypted batch mode")
    ErrPayloadTooLarge          = errorsmod.Register(ModuleName, 20, "encrypted_payload exceeds max_encrypted_payload_size")
    ErrInvalidTargetEpoch       = errorsmod.Register(ModuleName, 21, "target_epoch must be the current shield epoch")
    ErrPendingQueueFull         = errorsmod.Register(ModuleName, 22, "pending operation queue is at capacity")
    ErrDecryptionFailed         = errorsmod.Register(ModuleName, 23, "failed to decrypt encrypted payload")
    ErrInvalidMerkleRoot        = errorsmod.Register(ModuleName, 24, "merkle root is not current or previous")

    // Validator TLE
    ErrInvalidDecryptionShare   = errorsmod.Register(ModuleName, 25, "decryption share verification failed")
    ErrDuplicateShare           = errorsmod.Register(ModuleName, 26, "decryption share already submitted for this epoch")
    ErrNotTLEValidator          = errorsmod.Register(ModuleName, 27, "validator has no registered TLE public key share")
    ErrEpochTooOld              = errorsmod.Register(ModuleName, 28, "epoch is too old for late share submission")
    ErrDKGNotComplete           = errorsmod.Register(ModuleName, 29, "DKG ceremony not yet complete — encrypted batch mode unavailable")
    ErrInvalidProofOfPossession = errorsmod.Register(ModuleName, 30, "proof of possession for TLE key share is invalid")
    ErrRawMasterShareSubmitted  = errorsmod.Register(ModuleName, 31, "raw master share submitted instead of epoch-derived share — slashable")
    ErrIncompatibleOperation    = errorsmod.Register(ModuleName, 32, "target module does not implement ShieldAware or rejects this message type")
    ErrDKGInProgress            = errorsmod.Register(ModuleName, 33, "DKG ceremony already in progress")
    ErrCleartextFieldInBatchMode = errorsmod.Register(ModuleName, 34, "inner_message and proof must be empty in encrypted batch mode")

    // DKG ceremony
    ErrDKGNotOpen             = errorsmod.Register(ModuleName, 35, "DKG ceremony not in accepting phase")
    ErrNotBondedValidator     = errorsmod.Register(ModuleName, 36, "only bonded validators can participate in DKG")
    ErrDuplicateContribution  = errorsmod.Register(ModuleName, 37, "validator already contributed to this DKG round")
    ErrDKGRoundMismatch       = errorsmod.Register(ModuleName, 38, "DKG round does not match current round")
    ErrInvalidCommitments     = errorsmod.Register(ModuleName, 39, "feldman commitments are invalid")
    ErrInvalidEvaluations     = errorsmod.Register(ModuleName, 40, "encrypted evaluations are invalid or incomplete")
    ErrInsufficientValidators = errorsmod.Register(ModuleName, 41, "not enough bonded validators for DKG")
    ErrValidatorNotInDKG      = errorsmod.Register(ModuleName, 42, "validator is not in the expected DKG participant set")
)
```

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

### Phase 2: TLE + Encrypted Batch Mode

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
- `MsgSubmitAnonymousProposal` — shielded proposal creation
- `MsgAnonymousVoteProposal` — shielded voting
- Both routed through `MsgShieldedExec` (EITHER batch mode — works in immediate mode while TLE is inactive)
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

1. **ZK proof required**: Every shielded exec needs a valid membership proof — no proof, no free gas
2. **Per-identity rate limit**: `max_execs_per_identity_per_epoch` caps how many free operations each anonymous identity gets per epoch, enforced via the ZK-proven `rate_limit_nullifier` (same for all ops by the same member in the same epoch — unforgeable)
3. **Single-message enforcement**: Cannot bundle non-shielded messages with a shielded exec to piggyback free gas. Multi-message txs containing `MsgShieldedExec` are explicitly rejected by the ante handler.
4. **Max gas per exec**: `max_gas_per_exec` prevents expensive inner messages from draining the module quickly
5. **Daily funding cap**: `max_funding_per_day` bounds community pool drain regardless of usage (independent of shield epoch interval)
6. **Registered operations only**: Only governance-approved message types can be executed via shield (whitelist model)

### Privacy Properties

See the [Privacy Model](#privacy-model) section for a detailed comparison of immediate vs encrypted batch mode.

**Timing analysis mitigation (immediate mode):**
- Submitter address is meaningless (can be any account, fresh or reused)
- No funding transaction needed (shield pays gas)
- Multiple users can share a submitter address without privacy loss
- Client tooling should add random delays before submission

**Timing analysis mitigation (encrypted batch mode):**
- All of the above, plus:
- Operations execute together in a batch — no per-operation timing
- Execution order is shuffled — no correlation between submission order and execution order
- `min_batch_size` prevents single-operation batches (anonymity set = 1)
- Content is hidden until batch execution — mempool analysis reveals nothing

### Encrypted Batch: Deferred Verification Risk

In encrypted batch mode, ZK proof verification happens AFTER decryption (at batch time), not at submission time. This means invalid proofs consume module gas at execution time:

- **Impact**: An attacker could submit encrypted garbage payloads, wasting shield module gas when the batch processes
- **Mitigations**:
  1. Per-identity rate limit (cleartext `rate_limit_nullifier` enables this) — each anonymous identity can only submit N ops per epoch
  2. Payload size limit (`max_encrypted_payload_size`) — bounds the storage and processing cost per invalid op
  3. Queue capacity limit (`max_pending_queue_size`) — bounds total pending state
  4. The attacker must hold a valid ZK-provable identity (member or voter) to generate valid nullifiers
  5. Submitting garbage burns the attacker's nullifier for that scope — they can't resubmit with the same identity
- **Conclusion**: The cost of attack (burning nullifiers + rate limit) makes sustained spam uneconomical

### Encrypted Batch: No Operation Type Validation at Submission

In encrypted batch mode, the inner message type URL is encrypted. The handler cannot validate at submission time that:
- The message type is registered
- The message type supports ENCRYPTED_BATCH mode
- The batch mode matches the registration

These checks only happen at execution time (in `executeDecryptedOp`). An attacker could submit encrypted payloads targeting unregistered or IMMEDIATE_ONLY operations — the nullifier burns, queue space is consumed, but the op drops at execution.

**Mitigations**: The attacker must hold a valid identity (to produce valid nullifiers), burns their nullifier per scope, and is rate-limited. The cost of wasting queue slots is bounded by `max_pending_queue_size` and `max_execs_per_identity_per_epoch`. This is an inherent tradeoff of content encryption: you can't validate what you can't see.

### Encrypted Batch: Small Batch Deanonymization

If `min_batch_size = 1` (or disabled), a single operation in a batch epoch is trivially correlated by timing. Mitigations:

- Set `min_batch_size >= 3` (default) to ensure a minimum anonymity set
- If fewer ops arrive, they carry over to the next epoch (increasing the batch)
- `max_pending_epochs` caps how long ops wait — eventual forced execution even with small batches
- During quiet periods, this means maximum privacy is weaker. This is an inherent tradeoff: anonymity sets require co-submitters

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

## Default Parameters

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
  dkg_window_blocks: 200                      # ~20 min DKG window (half reg, half contrib)
  max_validator_set_drift: 33                 # 33% drift triggers automatic re-keying
```

## CLI Commands

### Transaction Commands

```bash
# Submit a shielded execution
sparkdreamd tx shield shielded-exec \
  --inner-message <path-to-inner-msg.json> \
  --proof <path-to-proof.bin> \
  --nullifier <hex-string> \
  --merkle-root <hex-string> \
  --proof-domain trust-tree \
  --min-trust-level 1 \
  --from <any-account>

# Register a shielded operation (governance)
sparkdreamd tx shield register-shielded-op \
  --message-type-url "/sparkdream.blog.v1.MsgCreatePost" \
  --proof-domain trust-tree \
  --min-trust-level 1 \
  --nullifier-domain 1 \
  --nullifier-scope-type epoch \
  --from <gov-authority>
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
sparkdreamd q shield nullifier-used --domain 1 --scope 42 --nullifier <hex>

# Query current day's funding amount
sparkdreamd q shield day-funding --day 42

# Query current shield epoch state
sparkdreamd q shield shield-epoch

# Query pending operations (optionally by epoch)
sparkdreamd q shield pending-ops --epoch 42

# Query pending operation count
sparkdreamd q shield pending-op-count

# Query TLE master public key (for client-side encryption)
sparkdreamd q shield tle-master-public-key

# Query a validator's TLE miss count
sparkdreamd q shield tle-miss-count <validator-address>

# Query decryption shares for an epoch
sparkdreamd q shield decryption-shares --epoch 42

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
k.verifyProof(ctx, msg, scope)  // uses stored Groth16 verification key (shield_v1)

// TLE — native to x/shield
k.GetTLEMasterPublicKey(ctx)           // stored in TLEKeySet
k.ReconstructDecryptionKey(ctx, epoch) // from submitted shares
```

### App-Level Wiring (app.go)

```go
// x/shield is created via depinject with accountKeeper and bankKeeper.
// Late-wired keepers are set after depinject.Inject() to avoid cycles:
app.ShieldKeeper.SetRepKeeper(app.RepKeeper)
app.ShieldKeeper.SetDistrKeeper(app.DistrKeeper)
app.ShieldKeeper.SetSlashingKeeper(app.SlashingKeeper)
app.ShieldKeeper.SetStakingKeeper(app.StakingKeeper)
app.ShieldKeeper.SetRouter(app.MsgServiceRouter())

// Register ShieldAware modules (double-gate for inner message dispatch)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.blog.v1.", app.BlogKeeper)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.forum.v1.", app.ForumKeeper)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.collect.v1.", app.CollectKeeper)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.commons.v1.", app.CommonsKeeper)
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.rep.v1.", app.RepKeeper)

// Register ante handler — ShieldGasDecorator goes before DeductFeeDecorator
anteHandler := ante.NewAnteHandler(ante.HandlerOptions{
    // ... existing options ...
    ShieldKeeper: app.ShieldKeeper,
})
```

## File References

### Client-Side Tools (Reused by x/shield)

- `tools/zk/circuit/shield_circuit.go` — Unified ShieldCircuit (Groth16 over BN254, trust tree membership + trust level + nullifiers + rate limiting)
- `tools/zk/circuit/shield_circuit_test.go` — Circuit tests
- `tools/crypto/crypto.go` — Merkle tree, MiMC hashing, key management, nullifiers
- `tools/zk/prover/prover.go` — Client-side proof generation
- `tools/zk/cmd/seed-tle/main.go` — TLE seed data generation CLI

### On-Chain Implementation

- `x/shield/keeper/proof.go` — ZK proof verification (Groth16 over BN254)
- `x/shield/keeper/keeper.go` — Keeper struct, late-keeper wiring, ShieldAware registry
- `x/shield/keeper/msg_server_shielded_exec.go` — MsgShieldedExec handler (immediate + encrypted batch)
- `x/shield/keeper/dkg.go` — DKG ceremony state machine
- `x/shield/keeper/dkg_crypto.go` — DKG cryptographic operations
- `x/shield/keeper/dkg_local.go` — Local DKG key management (per-validator secret state)
- `x/shield/abci/vote_extensions.go` — DKGVoteExtensionHandler (ExtendVote/VerifyVoteExtension)
- `x/shield/abci/proposal_handler.go` — PrepareProposalHandler / ProcessProposalHandler
- `x/shield/abci/preblocker.go` — ProcessDKGInjection (PreBlocker)
- `x/shield/ante/shield_gas.go` — ShieldGasDecorator (module-paid gas)
- `x/shield/ante/skip_fee_decorator.go` — SkipIfFeePaidDecorator
- `x/shield/types/genesis.go` — Default genesis shielded operations
- `x/shield/types/shield_aware.go` — ShieldAware interface definition

### Legacy Anonymous Implementations (Replaced by x/shield)

Per-module anonymous messages have been removed. Each module now has a `shield_aware.go` that implements the `ShieldAware` interface, gating which messages can be executed via `MsgShieldedExec`:

- `x/blog/keeper/shield_aware.go`
- `x/forum/keeper/shield_aware.go`
- `x/collect/keeper/shield_aware.go`
- `x/commons/keeper/shield_aware.go`
- `x/rep/keeper/shield_aware.go`

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
│   ├── begin_block.go                      # Auto-funding from community pool
│   ├── end_block.go                        # Batch execution (epoch boundary processing)
│   ├── nullifier.go                        # Nullifier storage and checking
│   ├── registration.go                     # Shielded op registration CRUD
│   ├── rate_limit.go                       # Per-identity rate limiting
│   ├── funding.go                          # Community pool funding logic
│   ├── pending.go                          # Pending operation queue management
│   ├── epoch.go                            # Shield epoch state management
│   ├── proof.go                            # ZK proof verification (Groth16 over BN254)
│   ├── tle.go                              # TLE key management
│   ├── tle_crypto.go                       # TLE cryptographic operations
│   ├── tle_liveness.go                     # TLE liveness tracking and jailing
│   ├── dkg.go                              # DKG ceremony state machine
│   ├── dkg_crypto.go                       # DKG cryptographic operations
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
│   ├── events.go                           # Event types
│   ├── genesis.go                          # Default genesis state and shielded operations
│   ├── genesis_vals.go                     # Genesis parameter values (test vs production)
│   ├── params.go                           # Parameter defaults and validation
│   ├── types.go                            # Type helpers
│   ├── types_test.go                       # Type tests
│   └── codec.go                            # Codec registration
├── abci/
│   ├── vote_extensions.go                  # DKGVoteExtensionHandler (ExtendVote/VerifyVoteExtension)
│   ├── proposal_handler.go                 # PrepareProposalHandler / ProcessProposalHandler
│   ├── preblocker.go                       # ProcessDKGInjection (PreBlocker)
│   ├── types.go                            # InjectedDKGData encoding/decoding, magic prefix
│   └── crypto.go                           # Schnorr verification helpers
├── ante/
│   ├── shield_gas.go                       # ShieldGasDecorator (module-paid gas)
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
