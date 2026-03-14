# `x/shield`

The `x/shield` module is the unified privacy layer for the Spark Dream chain. It provides a single entry point (`MsgShieldedExec`) for all anonymous operations across all modules, owns ZK proof verification and TLE (Timelock Encryption) infrastructure, and manages module-paid gas so submitters need zero balance.

## Overview

This module provides:

- **Single entry point** — `MsgShieldedExec` wraps any registered inner message for anonymous execution
- **Module-paid gas** — shield module account pays tx fees; submitters need zero balance (auto-funded from community pool)
- **ZK proof verification** — Groth16 over BN254 with on-chain verification keys
- **Two execution modes** — Immediate (low latency, inner message visible on-chain) and Encrypted Batch (TLE + batching for maximum privacy)
- **TLE infrastructure** — DKG ceremony, master public key, Shamir secret sharing, epoch-based decryption
- **Centralized nullifier management** — per-domain scoping with global, epoch, and field-value nullifier domains
- **Per-identity rate limiting** — ZK-derived rate-limit nullifiers prevent gas abuse without revealing identity
- **Auto-funding** — BeginBlocker draws from community pool when gas reserves are low
- **DKG state machine** — automatic validator key generation with drift detection and re-keying
- **ShieldAware module protocol** — target modules implement `ShieldAware` interface for integration

## Concepts

### Execution Modes

**Immediate mode**: Inner message and ZK proof are submitted in cleartext. The operation executes in the same block. Best for latency-sensitive actions (posts, reactions) where content visibility is acceptable.

**Encrypted Batch mode**: The inner message and proof are encrypted with the TLE master public key. The encrypted payload is queued. At epoch boundaries, validators produce decryption shares; once threshold is reached, the batch is decrypted, shuffled deterministically, and executed. Best for voting and actions where both identity AND content must be hidden.

### ZK Proof Verification

All proofs use a unified `ShieldCircuit` (Groth16 over BN254) with public inputs:

- `MerkleRoot` — trust tree root (current or previous accepted)
- `Nullifier` — action-specific replay prevention
- `RateLimitNullifier` — per-identity epoch-scoped rate limiting
- `MinTrustLevel` — minimum trust level being proven
- `Scope` — nullifier scope value
- `RateLimitEpoch` — current shield epoch

Verification keys are stored on-chain by circuit ID and updated via governance.

### Nullifier Domains

Nullifiers are scoped by domain (integer per message type) and scope type to allow different replay-prevention semantics:

| Scope Type | Scope Value | Meaning |
|------------|-------------|---------|
| `NULLIFIER_SCOPE_GLOBAL` | 0 | One action ever (e.g., anonymous challenges) |
| `NULLIFIER_SCOPE_EPOCH` | epoch number | One action per epoch (e.g., anonymous posts) |
| `NULLIFIER_SCOPE_MESSAGE_FIELD` | hash of field value | One action per unique field (e.g., one reaction per post) |

Each registered operation specifies its `nullifier_domain` (integer 1-42) and `nullifier_scope_type`, plus an optional `scope_field_path` for `MESSAGE_FIELD` scopes (e.g., `"post_id"`).

### DKG State Machine

The Distributed Key Generation ceremony creates the TLE master key:

```
INACTIVE → REGISTERING → CONTRIBUTING → ACTIVE
    │            │             │            │
    │     deadline passes   deadline     drift detected
    │            │          passes          │
    └────────────┴──── INACTIVE ◄───────────┘
```

- **INACTIVE**: No DKG in progress
- **REGISTERING**: Validators register public keys with proof of possession
- **CONTRIBUTING**: Validators submit Feldman VSS commitments and encrypted evaluations
- **ACTIVE**: Master key assembled, TLE operational

Auto-trigger: BeginBlocker starts DKG when bonded validators >= `min_tle_validators` and no TLE key set exists. Drift detection resets DKG if validator set diverges beyond `max_validator_set_drift` threshold.

### TLE Epoch Lifecycle

```
Epoch N starts → Validators submit decryption shares for epoch N-1
                → Threshold reached → Reconstruct decryption key
                → Decrypt + shuffle + execute pending ops from epoch N-1
                → Prune stale state
                → Check TLE liveness (increment miss counters, jail violators)
```

### Module-Paid Gas

The shield module account holds gas reserves (uspark). A custom ante decorator (`ShieldGasDecorator`) detects `MsgShieldedExec` transactions and deducts fees from the module account instead of the submitter. A companion `SkipIfFeePaidDecorator` wraps the standard `DeductFeeDecorator` to prevent double-deduction when fees have already been paid by the shield module. BeginBlocker auto-refills from the community pool when balance drops below `min_gas_reserve`, capped at `max_funding_per_day` (tracked per day, where day = `block_height / 14400`).

### ShieldAware Protocol

Target modules implement the `ShieldAware` interface to participate in shielded execution:

```go
type ShieldAware interface {
    IsShieldCompatible(ctx context.Context, msg sdk.Msg) bool
}
```

This provides a double gate:
- **Gate 1**: Governance whitelist (`ShieldedOpRegistration`) — controls which message types are allowed
- **Gate 2**: Module interface (`ShieldAware`) — module must explicitly opt in and confirm the message type is designed for anonymous execution via the shield module account

Both gates must pass for a shielded operation to execute.

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Params` | `p_shield` | Module parameters |
| `ShieldedOpRegistration` | `shield/ops/{type_url}` | Registered operation with batch mode, trust level, nullifier domain |
| `UsedNullifier` | `shield/nullifiers/{domain}/{scope}/{hex}` | Used nullifier record with height |
| `PendingNullifier` | `shield/pending_nullifiers/{hex}` | Dedup for encrypted batch queue |
| `DayFunding` | `shield/day_fundings/{day}` | Daily community pool draw amount |
| `IdentityRateLimit` | `shield/rate_limits/{epoch}/{hex}` | Per-identity execution count |
| `VerificationKey` | `shield/vk/{circuit_id}` | On-chain ZK verification key |
| `TLEKeySet` | `shield/tle_keyset` | Master public key + validator shares (singleton) |
| `TLEMissCounter` | `shield/tle_miss/{validator}` | Validator decryption share miss count |
| `PendingShieldedOp` | `shield/pending_ops/{id}` | Queued encrypted operation |
| `ShieldEpochState` | `shield/epoch_state` | Current epoch and start height (singleton) |
| `ShieldEpochDecryptionKey` | `shield/dec_keys/{epoch}` | Reconstructed epoch decryption key |
| `ShieldDecryptionShare` | `shield/dec_shares/{epoch}/{validator}` | Individual validator share |
| `DKGState` | `shield/dkg_state` | DKG ceremony state (singleton) |
| `DKGContribution` | `shield/dkg_contrib/{validator}` | Feldman VSS contribution |
| `DKGRegistration` | `shield/dkg_reg/{validator}` | BN256 G1 public key + Schnorr PoP |
| `NextPendingOpId` | `shield/pending_ops_seq` | Auto-increment sequence for pending ops |

## Messages

| Message | Description | Access |
|---------|-------------|--------|
| `MsgShieldedExec` | Execute any registered operation anonymously (immediate or encrypted batch) | Any address |
| `MsgUpdateParams` | Update module parameters | `x/gov` authority |
| `MsgRegisterShieldedOp` | Register or update a shielded operation whitelist entry | `x/gov` authority |
| `MsgDeregisterShieldedOp` | Remove a shielded operation from the whitelist | `x/gov` authority |
| `MsgTriggerDkg` | Manually trigger a new DKG ceremony | `x/gov` authority |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `ShieldedOp` | Registration for a specific message type |
| `ShieldedOps` | All registered shielded operations (paginated) |
| `ModuleBalance` | Shield module account balance |
| `NullifierUsed` | Check if nullifier used in domain+scope |
| `DayFunding` | Community pool draw for a given day |
| `ShieldEpoch` | Current epoch state |
| `PendingOps` | Pending encrypted operations (optional epoch filter) |
| `PendingOpCount` | Count of pending operations |
| `TLEMasterPublicKey` | Master public key for client-side encryption |
| `TLEKeySet` | Full TLE key set (master key + validator shares) |
| `VerificationKey` | ZK verification key by circuit ID |
| `TLEMissCount` | Validator's TLE miss count |
| `DecryptionShares` | Decryption shares for an epoch |
| `IdentityRateLimit` | Rate limit status for a rate-limit nullifier |
| `DKGState` | Current DKG ceremony state |
| `DKGContributions` | All DKG contributions for current round |

## Parameters

### Core

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enabled` | true | Master toggle for shielded execution |
| `max_funding_per_day` | 200 SPARK | Daily community pool draw cap |
| `min_gas_reserve` | 100 SPARK | Auto-fund trigger threshold |
| `max_gas_per_exec` | 500,000 | Gas limit per shielded execution |
| `max_execs_per_identity_per_epoch` | 50 | Per-identity rate limit |

### Encrypted Batch

| Parameter | Default | Description |
|-----------|---------|-------------|
| `encrypted_batch_enabled` | false | Requires DKG ceremony to complete |
| `shield_epoch_interval` | 50 blocks | ~5 minutes at 6s blocks |
| `min_batch_size` | 3 | Minimum ops before batch execution |
| `max_pending_epochs` | 6 | Max epochs before force-execute |
| `max_pending_queue_size` | 1,000 | Queue capacity |
| `max_encrypted_payload_size` | 16 KB | Max encrypted payload |
| `max_ops_per_batch` | 100 | Max ops processed per block |

### TLE Liveness

| Parameter | Default | Description |
|-----------|---------|-------------|
| `tle_miss_window` | 100 | Epoch window for miss tracking |
| `tle_miss_tolerance` | 10 | Misses before jailing |
| `tle_jail_duration` | 600s | Jail duration for TLE violations |

### DKG

| Parameter | Default | Description |
|-----------|---------|-------------|
| `min_tle_validators` | 5 | Minimum validators for DKG |
| `dkg_window_blocks` | 200 | Total ceremony duration (~20 min at 6s blocks); split equally between registration and contribution phases |
| `max_validator_set_drift` | 33 | % drift threshold for re-keying |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec, module account |
| `x/bank` | Yes | Balance queries, fee payment from module account |
| `x/rep` | No | Trust tree Merkle root snapshots for ZK proof validation |
| `x/distribution` | No | Community pool draws for auto-funding |
| `x/staking` | No | Bonded validator set for DKG and TLE liveness |
| `x/slashing` | No | Jail TLE-violating validators |

### Cyclic Dependency Breaking

All optional keepers are wired manually in `app.go` via `Set*Keeper()` methods after `depinject.Inject()`:
- `SetRepKeeper()` — trust tree roots
- `SetDistrKeeper()` — community pool funding
- `SetStakingKeeper()` — validator set queries
- `SetSlashingKeeper()` — TLE liveness jailing
- `SetRouter()` — inner message dispatch

Additionally, `RegisterShieldAwareModule(prefix, impl)` is called for each target module (blog, forum, collect, rep, commons) to register their `ShieldAware` implementations.

All held in shared `lateKeepers` struct so value-copies of Keeper (in msgServer, AppModule) see updates.

## BeginBlocker

1. **Auto-fund module account** — if balance < `min_gas_reserve`, draw from community pool (up to day cap)
2. **DKG state machine** — advance DKG phases, auto-trigger on startup, detect validator set drift
3. **DKG auto-trigger** — start DKG when validators >= `min_tle_validators` and no TLE key set exists

## EndBlocker

1. **Epoch advancement** — advance shield epoch when interval reached
2. **Batch execution** — decrypt, shuffle, verify, and execute pending ops from previous epoch
3. **Carry-over processing** — handle ops from older epochs with late-arrived keys
4. **Expired op cleanup** — drop ops past `max_pending_epochs`
5. **TLE liveness** — increment miss counters and jail violating validators
6. **State pruning** — clean up stale decryption keys and shares

## ABCI Extensions

### Vote Extensions (CometBFT)

Validators include TLE decryption shares in their vote extensions during `ExtendVote`. Other validators verify shares during `VerifyVoteExtension`. The `PrepareProposal` handler aggregates shares into the block proposal.

### Ante Handlers

- **`ShieldGasDecorator`** — detects `MsgShieldedExec` transactions (must be single-message), transfers fees from shield module account to fee pool, and sets `ContextKeyFeePaid` flag
- **`SkipIfFeePaidDecorator`** — wraps the standard `DeductFeeDecorator`; if `ContextKeyFeePaid` flag is set, skips inner fee deduction to prevent double-charging

## Events

All state-changing operations emit typed events:

### Execution

| Event | Description |
|-------|-------------|
| `shielded_exec` | Immediate mode execution (success/failure) |
| `shielded_queued` | Operation queued for encrypted batch |

### Batch Processing

| Event | Description |
|-------|-------------|
| `shield_batch_executed` | Batch processed (executed, dropped, decrypt-fail counts) |
| `shield_batch_skipped` | Batch skipped (missing decryption key, etc.) |
| `shield_batch_expired` | Stale ops expired past `max_pending_epochs` |

### Decryption

| Event | Description |
|-------|-------------|
| `shield_decryption_key_available` | Epoch decryption key reconstructed |
| `shield_decryption_key_failed` | Key reconstruction failed |

### Funding

| Event | Description |
|-------|-------------|
| `shield_funded` | Module funded from community pool |
| `shield_funding_cap_reached` | Daily funding cap exhausted |

### Registration

| Event | Description |
|-------|-------------|
| `shielded_op_registered` | Shielded operation whitelisted |
| `shielded_op_deregistered` | Shielded operation removed |

### DKG Lifecycle

| Event | Description |
|-------|-------------|
| `shield_dkg_triggered` | DKG ceremony manually triggered |
| `shield_dkg_opened` | DKG ceremony opened (auto-trigger) |
| `shield_dkg_registration` | Registration phase details |
| `shield_dkg_contribution` | Validator contribution submitted |
| `shield_dkg_complete` | DKG ceremony succeeded |
| `shield_dkg_activated` | Master key activated |
| `shield_dkg_failed` | DKG ceremony failed |

### TLE Operations

| Event | Description |
|-------|-------------|
| `shield_tle_registered` | Validator registered for TLE |
| `shield_tle_miss` | Validator missed decryption share submission |
| `shield_tle_jail` | Validator jailed for exceeding miss tolerance |
| `shield_validator_set_drift` | Validator set drift triggered re-keying |

## Genesis Registered Operations

The following operations are registered at genesis with their nullifier domains, scope types, and batch modes:

| Module | Message | Domain | Scope Type | Batch Mode |
|--------|---------|--------|------------|------------|
| blog | `MsgCreatePost` | 1 | EPOCH | EITHER |
| blog | `MsgCreateReply` | 2 | MESSAGE_FIELD (`post_id`) | EITHER |
| blog | `MsgReact` | 8 | MESSAGE_FIELD (`post_id`) | EITHER |
| forum | `MsgCreatePost` | 11 | EPOCH | EITHER |
| forum | `MsgUpvotePost` | 12 | MESSAGE_FIELD (`post_id`) | EITHER |
| forum | `MsgDownvotePost` | 13 | MESSAGE_FIELD (`post_id`) | EITHER |
| collect | `MsgCreateCollection` | 21 | EPOCH | EITHER |
| collect | `MsgUpvoteContent` | 22 | MESSAGE_FIELD (`target_id`) | EITHER |
| collect | `MsgDownvoteContent` | 23 | MESSAGE_FIELD (`target_id`) | EITHER |
| rep | `MsgCreateChallenge` | 41 | GLOBAL | ENCRYPTED_ONLY |
| commons | `MsgSubmitAnonymousProposal` | 31 | EPOCH | EITHER |
| commons | `MsgAnonymousVoteProposal` | 32 | MESSAGE_FIELD (`proposal_id`) | EITHER |

**Batch Mode Options:**
- `IMMEDIATE_ONLY` — immediate execution only
- `ENCRYPTED_ONLY` — encrypted batch only (e.g., anonymous challenges require maximum privacy)
- `EITHER` — both modes allowed (default for most operations; immediate works without TLE/DKG)

All operations use `PROOF_DOMAIN_TRUST_TREE` and require minimum trust level of 1 (PROVISIONAL), except rep challenges and commons governance (trust level 0).

## Client

### CLI

```bash
# Anonymous execution (immediate mode)
sparkdreamd tx shield shielded-exec \
  --inner-message '{"@type":"/sparkdream.blog.v1.MsgCreatePost","creator":"<shield-module-addr>","title":"Anon","body":"Hello"}' \
  --proof <hex> \
  --nullifier <hex> \
  --rate-limit-nullifier <hex> \
  --merkle-root <hex> \
  --proof-domain 1 \
  --min-trust-level 1 \
  --exec-mode 0 \
  --from <submitter>

# Anonymous execution (encrypted batch mode)
sparkdreamd tx shield shielded-exec \
  --encrypted-payload <hex> \
  --nullifier <hex> \
  --rate-limit-nullifier <hex> \
  --merkle-root <hex> \
  --proof-domain 1 \
  --min-trust-level 1 \
  --exec-mode 1 \
  --target-epoch <epoch> \
  --from <submitter>

# Queries
sparkdreamd q shield params
sparkdreamd q shield shielded-op /sparkdream.blog.v1.MsgCreatePost
sparkdreamd q shield shielded-ops
sparkdreamd q shield module-balance
sparkdreamd q shield nullifier-used 1 0 <hex>
sparkdreamd q shield day-funding 0
sparkdreamd q shield shield-epoch
sparkdreamd q shield pending-ops
sparkdreamd q shield pending-op-count
sparkdreamd q shield tle-master-public-key
sparkdreamd q shield tle-key-set
sparkdreamd q shield verification-key shield_v1
sparkdreamd q shield tle-miss-count <validator>
sparkdreamd q shield decryption-shares 0
sparkdreamd q shield identity-rate-limit <hex>
sparkdreamd q shield dkg-state
sparkdreamd q shield dkg-contributions
```

**Note:** `MsgRegisterShieldedOp`, `MsgDeregisterShieldedOp`, `MsgTriggerDkg`, and `MsgUpdateParams` are authority-gated (require `x/gov` proposals) and are not exposed via CLI.

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/shield/v1/query.proto` for the full API surface.
