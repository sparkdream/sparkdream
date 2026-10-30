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

**Encrypted Batch mode**: The inner message and proof are sealed to the TLE master public key (identity-based encryption keyed by the shield epoch; `tools/tle`) and queued. Once the epoch closes and its batch is ready (`min_batch_size`, or `max_pending_epochs` of waiting), the EndBlocker records a *decryption demand*; validators then release pairing-verified decryption shares for that epoch in their vote extensions, the PreBlocker reconstructs the epoch key at threshold, and the EndBlocker decrypts, verifies, shuffles and executes the batch in that block. Best for voting and actions where both identity AND content must be hidden.

### ZK Proof Verification

All proofs use a unified `ShieldCircuit` (Groth16 over BN254) with public inputs:

- `MerkleRoot` — trust tree root (current or previous accepted)
- `Nullifier` — action-specific replay prevention
- `RateLimitNullifier` — per-identity epoch-scoped rate limiting
- `MinTrustLevel` — minimum trust level being proven
- `Scope` — domain-separated scope element `crypto.ScopeElement(domain, raw_scope) = MiMC(be(domain), be(raw_scope))`. The circuit computes `Nullifier = MiMC(secret_key, Scope)`, so hashing the domain in keeps one member's nullifiers from matching across operations that share a raw scope (blog post 5 / forum post 5 / commons proposal 5, every GLOBAL op). This needs no circuit change; Scope was already a field element
- `RateLimitEpoch` — current shield epoch
- `MessageHash` - `crypto.ShieldMessageHash(type_url, value)` of the inner `Any` exactly as submitted (domain-separated sha256, top 3 bits cleared). Binds the proof to one inner message, so a proof seen in the mempool can't be resubmitted with different content. The circuit squares it, since a public input in no constraint would get a zero verifying-key term and verify for any value. Ownership-mode ops use `crypto.ShieldOwnedMessageHash(type_url, value, sequence)` instead (separate domain string, plus the owner's be64 sequence)

Verification keys are stored on-chain by circuit ID. They are set at genesis and can only be changed by a chain upgrade (there is no message that sets one), so governance cannot substitute a malicious key.

### Nullifier Domains

Nullifiers are scoped by domain (integer per message type) and scope type to allow different replay-prevention semantics:

| Scope Type | Scope Value | Meaning |
|------------|-------------|---------|
| `NULLIFIER_SCOPE_GLOBAL` | 0 | One action ever (e.g., anonymous challenges) |
| `NULLIFIER_SCOPE_EPOCH` | `epoch / epoch_window` (window 0 or 1 = every epoch) | One action per window (e.g., one anonymous blog post per 12 epochs) |
| `NULLIFIER_SCOPE_MESSAGE_FIELD` | field value | One action per unique field (e.g., one reaction per post) |

Each registered operation specifies its `nullifier_domain` (a small integer per operation) and `nullifier_scope_type`, plus an optional `scope_field_path` for `MESSAGE_FIELD` scopes (e.g., `"post_id"`; `"target_type,target_id"` packs a qualifier as `(target_type << 56) | target_id`; `"reply_id|post_id"` uses `(1 << 56) | reply_id` when `reply_id` is non-zero, else `post_id`). A `MESSAGE_FIELD` scope whose field can't be read rejects the exec with `ErrInvalidInnerMessage` rather than falling back to the epoch scope (which would allow one action per epoch on something meant to be once per target). Used nullifiers are stored under `(domain, raw_scope, hex)`; the proof's scope element is derived from the same pair. All ops sharing a domain must scope it the same way (mode, scope type, field path, epoch window); `MsgRegisterShieldedOp` and genesis validation refuse a conflict (`ErrNullifierDomainConflict`), since pruning and uniqueness are per domain.

### Nullifier Modes

`ShieldedOpRegistration.nullifier_mode` decides what the nullifier proves:

- **`NULLIFIER_MODE_CONSUME`** (default): a one-time action token, checked unused and recorded under `(domain, raw_scope)`. Both exec paths run the inner message with `types.WithExecNullifier(ctx, ExecNullifier{Domain, Scope, Nullifier})`, so a module can keep the creating exec's nullifier as an owner tag for anonymously created content.
- **`NULLIFIER_MODE_OWNERSHIP`**: proves the submitter is the anonymous creator of the target content; the nullifier is reused, never recorded. The target module implements the optional `ShieldOwnershipResolver` (`ResolveOwnership(ctx, msg) (OwnershipClaim, error)`, `AdvanceOwnership(ctx, msg) error`; `OwnershipClaim{Domain, Scope, Tag, Sequence}`, all stored with the content). The exec's nullifier must equal `claim.Tag` (`ErrOwnershipMismatch`), and the proof is verified with `Scope = ScopeElement(claim.Domain, claim.Scope)` and `MessageHash = ShieldOwnedMessageHash(type_url, value, claim.Sequence)`; `nullifier_scope_type`/`scope_field_path` are ignored. After validation, `AdvanceOwnership` bumps the sequence (retiring the proof), the rate limit is counted, and the inner message runs with `types.WithOwnershipTag(ctx, tag)`; the target module compares that tag with its stored one before letting the shield address manage the content. In CheckTx (not simulate) the ante precheck advances the sequence in the check state so rewrapped copies leave the mempool. A module without the resolver fails with `ErrNotOwnable`.

Ownership ops must be `IMMEDIATE_ONLY` - the sequence is bound at execution, which a batch submitter can't predict. `ShieldedOpRegistration.Validate()` (run by genesis validation and `MsgRegisterShieldedOp`) rejects anything else with `ErrOwnershipBatchMode` (it also rejects `epoch_window > 1` outside EPOCH-scoped consume ops; genesis validation additionally rejects a duplicate `message_type_url`), and the batch executor drops ownership ops. A re-randomized (mauled) Groth16 proof has the same public inputs, so it can only run the owner's own pending op once; after that the sequence has moved on. Management ops on one piece of content are linkable to each other and to its creation (same tag) but not to the member's other activity.

### DKG State Machine

The Distributed Key Generation ceremony creates the TLE master key:

```
INACTIVE → REGISTERING → CONTRIBUTING → COMPLAINING → JUSTIFYING → ACTIVE
    ▲           │              │             │             │          │
    │     too few keys   below threshold     │     below threshold    │ drift detected
    │           │              │             │     after disqualif.   │ (or MsgTriggerDKG)
    └───────────┴──────────────┴─────────────┴─────────────┴──────────┘
```

The `dkg_window_blocks` window is split into four equal phases.

- **INACTIVE**: No DKG in progress
- **REGISTERING**: Validators register BN256 keys with a proof of possession over (operator, round). At the deadline the participant set is narrowed to the registrants (a validator without a key could never receive its evaluations); fewer than `min_tle_validators` fails the round
- **CONTRIBUTING**: Validators deal Feldman VSS commitments (G1 + G2) and ECIES-encrypted evaluations to every other participant; the threshold of contributions must be met
- **COMPLAINING**: Each validator checks the evaluations dealt to it against the dealers' commitments and names the dealers whose evaluation is missing, undecryptable or inconsistent
- **JUSTIFYING**: Accused dealers publish the disputed evaluations in clear, verified on chain against their commitments. At the deadline a dealer with an unanswered complaint is disqualified; the qualified contributions (still at least the threshold) are aggregated into the key set
- **ACTIVE**: Master key assembled, TLE operational. Batch mode turns on by itself only when `enable_batch_after_dkg` is set; otherwise governance enables it

Injected ceremony extensions carry their validator's CometBFT vote-extension signature; ProcessProposal and the PreBlocker verify it against the validator's consensus key (previous height, last commit round), so a proposer can't attribute registrations, contributions or complaints to other validators. Decryption shares are self-authenticating (pairing-verified against the public share).

Auto-trigger: BeginBlocker starts DKG when bonded validators >= `min_tle_validators` and no TLE key set exists. Drift detection (validator set diverging beyond `max_validator_set_drift`) and `MsgTriggerDkg` retire the key set and everything sealed to it (shares, epoch keys, demands, pending ops, the liveness record) and reopen the ceremony; every per-round record (registrations, contributions, complaints) is cleared at each open.

### TLE Epoch Lifecycle

```
Epoch N closes (boundary block B)
  → EndBlocker: is epoch N's batch ready?  (>= min_batch_size alone or together
    with other closed epochs, or waited max_pending_epochs)
      yes → record DecryptionDemand(N, B)
  → Validators (ExtendVote): for the oldest demanded epoch they have not
    served (until its liveness is judged, even once a key exists), embed
    share_i = s_i * H1(N)   (H1 = hash-to-curve, tools/tle.EpochTag)
  → VerifyVoteExtension / ProcessProposal / PreBlocker: pairing check
    e(share_i, G2) == e(H1(N), pubShare_i_G2); bad shares are dropped and
    emit shield_tle_invalid_share (they count as misses)
  → PreBlocker: threshold reached → reconstruct epoch key N
  → EndBlocker (same block): decrypt + verify proofs + shuffle + execute N's ops
Height B + interval (every block, before expiry)
  → judge TLE liveness for epoch N: bonded key-set validators without a
    stored share missed; misses live in a ring of the last tle_miss_window
    judged epochs, reset whenever the key set changes
```

A key is never reconstructed for an epoch whose batch is not ready, so a small batch waits *encrypted* rather than decryptable on chain. Idle epochs demand nothing and count against nobody.

### Module-Paid Gas

The shield module account holds gas reserves in the chain's bond denom (from x/identity). A custom ante decorator (`ShieldGasDecorator`) detects `MsgShieldedExec` transactions and deducts fees from the module account instead of the submitter. A companion `SkipIfFeePaidDecorator` wraps the standard `DeductFeeDecorator` to prevent double-deduction when fees have already been paid by the shield module. BeginBlocker auto-refills from the community pool when balance drops below `min_gas_reserve`, capped at `max_funding_per_day` (tracked per UTC day of the block time).

Because the module pays, it pays only for execs that will run: both modes have a proof, a nullifier and the identity rate limit checked in the ante handler (`PrecheckExec`) before any fee moves — the operation proof for an immediate exec, the cleartext *submission proof* for an encrypted-batch exec (bound to the ciphertext; the operation proof and nullifier stay sealed) — and the fee itself must be in the bond denom and at most `max_fee_per_exec` (otherwise `ErrFeeTooHigh`). The identity rate limit, now proven in both modes, bounds spend per member, so there is no per-submitter-address cap. Batch execs are rejected in the ante handler (`ErrEncryptedBatchDisabled`) while `encrypted_batch_enabled` is false. For an immediate exec the precheck **spends** the exec in the ante state in every mode but simulate - records the consume-mode nullifier (or advances the ownership-mode owner sequence) and counts the identity rate limit - and the msg server, told so through `types.WithExecConsumed`, only runs the inner message. In DeliverTx that state commits with the fee even when the inner message fails, so a failing exec costs its nullifier and slot like a successful one and can't be replayed at the reserve's expense; in CheckTx it is discarded at commit and only keeps rewrapped copies of a pending exec out of the mempool. A batch submission's nullifier is marked pending in CheckTx. Further ante rules: public inputs (nullifier, rate-limit nullifier, merkle root) must be canonical BN254 field elements (`ErrNonCanonicalPublicInput`: the verifier reduces mod r, so `n + r` would alias `n`); in CheckTx the fee must meet the node's minimum gas prices; the fees the module pays per shield epoch are capped chain-wide by `max_fees_per_epoch` (`ErrEpochFeeBudgetExhausted`); and txs signed by the public submitter must be unordered (`ErrOrderedPublicSubmitterTx`). The msg server skips re-verifying a proof the ante handler already verified for the same message. The inner handler runs under a `max_gas_per_exec` meter and what it uses is charged to the outer tx.

Verifying in CheckTx costs every node a Groth16 pairing check (~1.5-3 ms) per admitted exec, and public-submitter txs are free to submit, so the ante handler guards it with two node-local, non-consensus defences (review finding M4 - mitigated; `x/shield/ante/proof_guard.go`):

- **Rejected-proof cache** - a bounded LRU (4096 entries) of execs whose proof failed verification; an identical resubmission gets `ErrInvalidProof` without a second pairing. The key hashes the full `MsgShieldedExec` bytes, the stored VK bytes (a VK change invalidates everything), the shield epoch and the op registration, so a cached rejection can never become valid. Only `ErrInvalidProof` is cached (in this path it comes only from proof verification); state-dependent failures (stale root, used nullifier, rate limit, disabled shield, inactive op) are never cached, nor are ownership-mode execs (their public inputs depend on the owner record).
- **Verification throttle** - a token bucket of 150 verifications/s sustained, burst 300, for new CheckTx and simulate. When empty the exec is refused with `ErrProofVerificationThrottled` (code 51; retryable). Cache hits and execs that fail a cheap state check first don't use tokens. ReCheckTx is never throttled (it would evict txs that already passed).

Both apply only in CheckTx / ReCheckTx / simulate - DeliverTx always verifies - and only when a VK is stored. All cheap state checks (registration, nullifier, identity rate limit, merkle root) run before the pairing check. Operators can also tune CometBFT's per-peer `send_rate` / `recv_rate` in `config.toml`.

Anonymous actions are signed by the shield module account, so target modules don't charge them any per-action SPARK costs (storage fees, spam taxes, edit fees, the blog reaction fee, downvote costs, collection/item deposits) — that would spend the gas reserve, not the member's SPARK. Anonymous volume is bounded instead by x/shield's per-identity exec limit and per-op rate-limit windows.

Target modules apply their own trust gates to anonymous actions using the level the proof established (`types.ProvenTrustLevel`, set via `types.WithProvenTrustLevel(ctx, min_trust_level)` by both exec paths). A message from the shield address with no proven level in its context didn't come through a shield exec, so the gates refuse it (forum treats it as the lowest trust level).

### Public Anonymous Submitter

`MsgShieldedExec` still needs an outer signer. `types.PublicSubmitterAddress()` is a shared secp256k1 account derived from `sha256("sparkdream/shield/public-submitter/v1")`; its private key is public on purpose. Anonymous clients all sign with it (as unordered txs, so they never race on its sequence), so the outer signer reveals nothing - the ZK proof, not the signature, authorizes an exec. `ShieldGasDecorator` refuses any non-shield tx signed by it, and `InitGenesis` creates its account if missing (clients sign with its account number). Any other account still works as submitter.

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

Both gates must pass for a shielded operation to execute. Ownership-mode operations additionally need the module's `ShieldAware` implementation to satisfy `ShieldOwnershipResolver` (see [Nullifier Modes](#nullifier-modes)); x/collect is the only one today.

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Params` | `p_shield` | Module parameters |
| `ShieldedOpRegistration` | `shield/ops/{type_url}` | Registered operation with batch mode, trust level, nullifier domain, nullifier mode |
| `UsedNullifier` | `shield/nullifiers/{domain}/{scope}/{hex}` | Used nullifier record with height |
| `PendingNullifier` | `shield/pending_nullifiers/{hex}` | Dedup for encrypted batch queue |
| `DayFunding` | `shield/day_fundings/{day}` | Daily community pool draw amount |
| `IdentityRateLimit` | `shield/rate_limits/{epoch}/{hex}` | Per-identity execution count |
| `VerificationKey` | `shield/vk/{circuit_id}` | On-chain ZK verification key |
| `TLEKeySet` | `shield/tle_keyset` | Master public key + validator shares (singleton) |
| `TLEMissCounter` | `shield/tle_miss/{validator}` | Validator decryption share miss count |
| `PendingShieldedOp` | `shield/pending_ops/{target_epoch}/{id}` | Queued encrypted operation (submission nullifier in cleartext; operation nullifier sealed) |
| `PendingOpCount` | `shield/pending_op_count` | Queue size (singleton) |
| `TLEEpochParticipation` | `shield/tle_part/{validator}/{slot}` | Ring of the last `tle_miss_window` judged epochs |
| `TLEJudgedEpochs` | `shield/tle_judged` | Judged-epoch counter under the current key set (singleton) |
| `DKGComplaint` | `shield/dkg_complaint/{dealer}/{complainer}` | Complaint and, once answered, the justified evaluation |
| `ShieldEpochState` | `shield/epoch_state` | Current epoch and start height (singleton) |
| `ShieldEpochDecryptionKey` | `shield/dec_keys/{epoch}` | Reconstructed epoch decryption key |
| `ShieldDecryptionShare` | `shield/dec_shares/{epoch}/{validator}` | Individual validator share (pairing-verified before storage) |
| `DecryptionDemand` | `shield/dec_demand/{epoch}` | Height the EndBlocker asked validators for a closed epoch's shares (negative once liveness judged) |
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
| `MsgTriggerDkg` | Manually trigger a new DKG ceremony; refused while a round is running, with fewer than `min_tle_validators` bonded validators, or unless `numerator <= denominator <= 100` (`ErrInvalidDKGThreshold`) | `x/gov` authority |

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
| `TLEMasterPublicKey` | Master public keys; clients encrypt to `master_public_key_g2` (`tools/tle`) |
| `TLEKeySet` | Full TLE key set (master key + validator shares) |
| `VerificationKey` | ZK verification key by circuit ID |
| `TLEMissCount` | Validator's TLE miss count |
| `DecryptionShares` | Decryption shares for an epoch |
| `IdentityRateLimit` | Rate limit status for a rate-limit nullifier |
| `DKGState` | Current DKG ceremony state |
| `DKGContributions` | All DKG contributions for current round |

## Parameters

Defaults below are the production (`mainnet`/`testnet` build) values; `DefaultParams()` returns the build tag's values (`x/shield/types/genesis_vals_*.go`), and `testparams`/`devnet` use shorter windows and smaller minimums. Mainnet ships `enabled: false` until its ceremony verification key is in genesis: a production build refuses to start a genesis that enables shield without one (see `deploy/README.md`, "Shield verification key").

### Core

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enabled` | true | Master toggle for shielded execution |
| `max_funding_per_day` | 200 SPARK | Daily community pool draw cap |
| `min_gas_reserve` | 100 SPARK | Auto-fund trigger threshold |
| `max_gas_per_exec` | 500,000 | Gas limit per shielded execution |
| `max_execs_per_identity_per_epoch` | 50 | Per-identity rate limit |
| `max_fee_per_exec` | 0.05 SPARK | Most the module pays for one exec; a higher fee, or any other denom, is refused unpaid |

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
| `tle_miss_window` | 100 | Ring of the last N *judged* epochs misses are counted over (reset on every key set change) |
| `tle_miss_tolerance` | 10 | Misses in the ring before jailing |
| `tle_jail_duration` | 600s | Seconds a TLE-jailed validator stays jailed (`JailUntil`) before `MsgUnjail` is accepted |

### DKG

| Parameter | Default | Description |
|-----------|---------|-------------|
| `min_tle_validators` | 5 | Minimum validators for DKG |
| `dkg_window_blocks` | 200 | Total ceremony duration (~20 min at 6s blocks); four equal phases: registration, dealing, complaints, justifications |
| `max_validator_set_drift` | 33 | % drift threshold for re-keying |
| `enable_batch_after_dkg` | false | Whether a completed DKG turns `encrypted_batch_enabled` on by itself (true in `testparams`); a re-key always turns it off |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec, module account |
| `x/bank` | Yes | Balance queries, fee payment from module account |
| `x/rep` | No | Trust tree Merkle root snapshots for ZK proof validation |
| `x/distribution` | No | Community pool draws for auto-funding |
| `x/staking` | No | Bonded validator set for DKG and TLE liveness |
| `x/slashing` | No | Jail TLE-violating validators |
| `x/identity` | Yes (late-wired) | Bond denom; the keeper panics on first denom lookup if unwired |

### Cyclic Dependency Breaking

All optional keepers are wired manually in `app.go` via `Set*Keeper()` methods after `depinject.Inject()`:
- `SetRepKeeper()` — trust tree roots
- `SetDistrKeeper()` — community pool funding
- `SetStakingKeeper()` — validator set queries
- `SetSlashingKeeper()` — TLE liveness jailing
- `SetIdentityKeeper()` — bond denom
- `SetRouter()` — inner message dispatch

Additionally, `RegisterShieldAwareModule(prefix, impl)` is called for each target module (blog, forum, collect, rep, commons, federation) to register their `ShieldAware` implementations.

All held in shared `lateKeepers` struct so value-copies of Keeper (in msgServer, AppModule) see updates.

## BeginBlocker

Nothing runs while `params.enabled` is false (no funding, no DKG phase advance; the ABCI handlers then also refuse ceremony-phase data).

1. **Auto-fund module account** — if balance < `min_gas_reserve`, draw from community pool (up to day cap)
2. **DKG state machine** — advance DKG phases, auto-trigger on startup, detect validator set drift
3. **DKG auto-trigger** — start DKG when validators >= `min_tle_validators` and no TLE key set exists

## EndBlocker

1. **Epoch advancement** — advance shield epoch when interval reached, whether or not encrypted batch mode is enabled (immediate mode uses it for EPOCH-scoped nullifiers and per-epoch rate limits; a frozen epoch would make those lifetime limits)
2. **TLE liveness** *(batch mode, every block)* — judge each demand exactly one full interval after it was recorded, before expiry can forget it: bonded key-set validators without a stored share for that epoch missed; past `tle_miss_tolerance` misses in the ring the validator is jailed and `JailUntil` is set `tle_jail_duration` seconds ahead
3. **Expired op cleanup** *(every block, batch mode on or off)* — drop ops past `max_pending_epochs` and forget their demands
4. **Decryption demands** *(batch mode, every block)* — mark each closed epoch whose batch is ready (a key-only count per epoch); validators only release shares for demanded epochs
5. **Batch execution** *(batch mode, every block)* — for every demanded closed epoch whose key exists: decrypt, verify the operation proofs (against the submission epoch, the cleartext fields stored on the pending op and the submission's rate-limit nullifier), shuffle, execute, at most `max_ops_per_batch` per block
6. **State pruning** — every epoch: identity rate limits, EPOCH-scoped nullifiers (one range scan per EPOCH-scoped domain), day fundings, stale decryption keys and shares

## ABCI Extensions

### Vote Extensions (CometBFT)

Validators carry the DKG ceremony (registrations, contributions, complaints, justifications) and, once ACTIVE, TLE decryption shares in their vote extensions during `ExtendVote` — one share per vote, for the oldest demanded closed epoch they have not served. `VerifyVoteExtension`, `PrepareProposal`, `ProcessProposal` and the PreBlocker all apply one per-phase acceptance rule (`x/shield/abci/validate.go`; for shares `VerifyValidatorDecryptionShare`: key-set membership, closed and demanded epoch inside the `max_pending_epochs` window, and the pairing check against the validator's G2 public share), so an invalid share never reaches Lagrange reconstruction and is attributable to its submitter, and an honest proposer never injects what `ProcessProposal` would reject. The `PrepareProposal` handler aggregates the extensions into a pseudo-tx at position 0 within half of `MaxTxBytes` (the rest is re-sent in later votes) and delegates the remaining block to the SDK's default proposal handler. Vote extensions must be enabled in consensus params (`abci.vote_extensions_enable_height`, 1 in every network's `config.yml`).

### Ante Handlers

- **`ShieldGasDecorator`** — detects `MsgShieldedExec` transactions (must be single-message), prechecks both modes (nullifiers, identity rate limit, then the operation or submission proof, behind a node-local rejected-proof cache and verification throttle in CheckTx), caps the fee at `max_fee_per_exec`, transfers fees from shield module account to fee pool, and sets `ContextKeyFeePaid` flag. Also refuses any non-shield tx signed by the public anonymous submitter
- **`SkipIfFeePaidDecorator`** — wraps the standard `DeductFeeDecorator`; if `ContextKeyFeePaid` flag is set, skips inner fee deduction to prevent double-charging

## Events

All state-changing operations emit typed events:

### Execution

| Event | Description |
|-------|-------------|
| `shielded_exec` | Successful immediate mode execution; attributes include `nullifier_domain`, `nullifier_hex`, `exec_mode` and `nullifier_mode` |
| `shielded_queued` | Operation queued for encrypted batch |

### Batch Processing

| Event | Description |
|-------|-------------|
| `shield_batch_executed` | Batch processed (`batch_size`, `executed`, `dropped`, `decrypt_failed`, `proof_failed`) |
| `shield_batch_skipped` | Batch skipped: the epoch's stored decryption key is empty |
| `shield_batch_expired` | Stale ops expired past `max_pending_epochs` (`cutoff_epoch`), or dropped by a re-key (`dkg_round`) |

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
| `shield_dkg_opened` | Round opened (auto-trigger or `MsgTriggerDkg`) and every phase transition (`dkg_phase`, `count`; no `count` on the COMPLAINING transition) |
| `shield_dkg_registration` | Validator key registered |
| `shield_dkg_contribution` | Validator contribution recorded |
| `shield_dkg_complaint` | Complaint recorded (`dealer`, `complainer`) |
| `shield_dkg_justification` | Complaint answered with a valid evaluation |
| `shield_dkg_disqualified` | Dealer excluded from the key set (unanswered complaint) |
| `shield_dkg_complete` | DKG ceremony succeeded (`contributions_count` qualified) |
| `shield_dkg_activated` | Master key activated |
| `shield_dkg_failed` | DKG ceremony failed |

### TLE Operations

| Event | Description |
|-------|-------------|
| `shield_tle_miss` | Bonded key-set validator had no share for a judged epoch (`validator`, `epoch`) |
| `shield_tle_jail` | Validator jailed for exceeding miss tolerance (`JailUntil` = now + `tle_jail_duration`) |
| `shield_tle_invalid_share` | A validator's decryption share failed verification and was dropped |
| `shield_validator_set_drift` | Validator set drift triggered re-keying |

## Genesis Registered Operations

The following 21 operations are registered at genesis with their nullifier domains, scope types, and batch modes (all `NULLIFIER_MODE_CONSUME` except the eight collect management ops):

| Module | Message | Domain | Scope Type | Batch Mode |
|--------|---------|--------|------------|------------|
| blog | `MsgCreatePost` | 1 | EPOCH (window 12, ~1 h) | EITHER |
| blog | `MsgCreateReply` | 2 | MESSAGE_FIELD (`post_id`) | EITHER |
| blog | `MsgReact` | 8 | MESSAGE_FIELD (`reply_id\|post_id`) | EITHER |
| forum | `MsgCreatePost` | 11 | EPOCH (window 3, ~15 min) | EITHER |
| forum | `MsgUpvotePost` | 12 | MESSAGE_FIELD (`post_id`) | EITHER |
| forum | `MsgDownvotePost` | 12 | MESSAGE_FIELD (`post_id`) | EITHER |
| collect | `MsgCreateCollection` | 21 | EPOCH (window 288, ~1 day) | EITHER |
| collect | `MsgUpvoteContent` | 22 | MESSAGE_FIELD (`target_type,target_id`) | EITHER |
| collect | `MsgDownvoteContent` | 22 | MESSAGE_FIELD (`target_type,target_id`) | EITHER |
| collect | `MsgUpdateCollection`, `MsgDeleteCollection`, `MsgAddItem`, `MsgAddItems`, `MsgUpdateItem`, `MsgRemoveItem`, `MsgRemoveItems`, `MsgReorderItem` | 23 (label only) | OWNERSHIP mode (owner claim from the collection) | IMMEDIATE_ONLY |
| rep | `MsgCreateChallenge` | 41 | GLOBAL | ENCRYPTED_ONLY |
| commons | `MsgSubmitAnonymousProposal` | 31 | EPOCH | EITHER |
| commons | `MsgAnonymousVoteProposal` | 32 | MESSAGE_FIELD (`proposal_id`) | EITHER |
| federation | `MsgSubmitArbiterHash` | 51 | MESSAGE_FIELD (`content_id`) | EITHER |

Upvotes and downvotes share their module's domain, so a member gets one anonymous vote per target either way. A two-field scope (`target_type,target_id`) is packed as `(target_type << 56) | target_id` — clients pass that value as the prover's raw scope — so a collection and an item with the same id don't share a scope.

The collect management ops let the anonymous creator of a collection manage it: the proof is made over the collection's `anon_owner_domain`/`anon_owner_scope` (the creating `MsgCreateCollection`'s domain 21 and epoch), reproduces `anon_owner_tag`, and binds `anon_owner_sequence`. Nothing is recorded under domain 23.

**Batch Mode Options:**
- `IMMEDIATE_ONLY` — immediate execution only
- `ENCRYPTED_ONLY` — encrypted batch only (e.g., anonymous challenges require maximum privacy)
- `EITHER` — both modes allowed (default for most operations; immediate works without TLE/DKG)

All operations use `PROOF_DOMAIN_TRUST_TREE`. The registered `min_trust_level` is a floor the exec's proven level must meet: 0 for everything except federation arbiter hashes (2). For content ops (blog, forum, collect) the proof then shows membership and nothing else, so every member is in the anonymity set; where the target asks for more (a blog post's `min_reply_trust_level`, collect's curator or sponsor gates), the client proves that level for that action only, and the target compares it via `types.ProvenTrustLevel`.

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
  --min-trust-level 0 \
  --exec-mode 0 \
  --from <submitter>

# Anonymous execution (encrypted batch mode; --proof is the submission proof)
sparkdreamd tx shield shielded-exec \
  --encrypted-payload <hex> \
  --proof <hex> \
  --nullifier <hex> \
  --rate-limit-nullifier <hex> \
  --merkle-root <hex> \
  --proof-domain 1 \
  --min-trust-level 0 \
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

### E2E genesis flavours

`test/shield/run_all_tests.sh` runs against a default genesis (no DKG, batch mode off). Two scripts patch `~/.sparkdream/config/genesis.json` after `ignite chain init` for the TLE paths: `patch_genesis_dkg.sh` (min_tle_validators=1; `dkg_ceremony_test.sh` then watches the live ceremony and pushes a sealed op through the resulting key) and `patch_genesis_tle.sh` (seeded single-validator key set with G1+G2 keys and pre-computed epoch keys; `encrypted_batch_test.sh`). Both need `setup_test_accounts.sh` first; the scripts fund the community pool so the shield gas reserve is non-zero.

### Proving Tools

- **Browser client** - `tools/zk/wasm` (built by `tools/zk/wasm/build.sh`) exposes `sparkdreamShield.version/deriveSecret/publicKey/loadKeys/prove/encrypt`, using the same code the chain verifies against; `encrypt` seals the inner Any with the operation proof and nullifier and returns the submission-proof inputs for a second `prove()`. `prove` input takes the operation's nullifier `"domain"` and raw `"scope"`; for an ownership-mode op pass the content's owner domain and scope plus `"owner_sequence"` (decimal string), and check the returned nullifier equals the content's owner tag.
- **Prover** - `prover.ShieldProofInput` carries `Domain` and the raw `Scope`; it derives the scope element and the nullifier (`crypto.ComputeScopedNullifier`) itself.
- **Merkle path** - clients rebuild the trust tree from x/rep's `TrustTree` query (`/sparkdream/rep/v1/trust_tree`) and compute their own path with `crypto.SparseMerkleProof`, so they never reveal which leaf they want. Fetch all pages at one pinned height (`x-cosmos-block-height` header) and refetch if the rebuilt root differs from the response's `root`: pages read at different heights can straddle a leaf change.
- **Secret key** - `crypto.DeriveSecretKeyFromSignature` derives it from a wallet signature; secret keys are always canonical BN254 field elements.
- **Trusted setup** - `tools/zk/cmd/ceremony` runs the multi-party Groth16 setup producing `proving_key.bin`, `circuit.r1cs` and the on-chain verifying key.

See the Client Tooling section of [docs/x-shield-spec.md](../../docs/x-shield-spec.md) for the full flow.

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/shield/v1/query.proto` for the full API surface.
