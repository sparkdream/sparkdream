# Anonymous Posting via ZK Proofs

## Problem

Members may need to post content without revealing their identity — for whistleblowing, controversial opinions, honest feedback on initiatives, or anonymous peer review. Standard on-chain transactions link content to a specific address, making anonymity impossible without protocol-level support.

## Solution

Reuse the ZK-SNARK infrastructure from `x/vote` (Groth16 on BN254, MiMC hashing, Merkle trees) to let members prove they are an active member meeting a minimum trust level — without revealing *which* member they are. A nullifier system prevents spam: one anonymous action per scope per identity.

This is a **cross-module pattern**. Any content module (`x/blog`, `x/forum`, `x/collect`, etc.) can integrate anonymous posting by adding the message types described below and calling into the shared verification infrastructure.

---

## Dependencies

| Module | Purpose |
|--------|---------|
| `x/vote` | `VoteKeeper.VerifyAnonymousActionProof()` — Groth16 proof verification and verifying key storage |
| `x/rep` | Maintains the **member trust tree** (Merkle tree with trust-level-encoded leaves) |

Both are required. If either keeper is nil, all anonymous posting messages should return an "unavailable" error.

---

## Member Trust Tree (maintained by x/rep)

A separate Merkle tree from x/vote's voter tree, maintained by x/rep and available to any module needing trust-level-aware ZK proofs.

```
Leaf = MiMC_hash(zk_public_key, trust_level)
Tree depth: 20 (~1,048,576 members)
Hash function: MiMC (SNARK-friendly, matches x/vote circuit)
```

**Lifecycle:**
1. Tree is marked dirty when a member's trust level changes, a new voter registers, or a member is deactivated
2. x/rep's EndBlocker rebuilds the tree when dirty (same pattern as x/vote's voter tree)
3. The current root is stored in x/rep state as `MemberTrustTreeRoot`
4. Each anonymous post/reply records the root used at proof time for auditability

**Why a separate tree from x/vote's voter tree:**
- Voter tree leaves encode `hash(pubkey, voting_power=1)` — no trust level information
- Trust tree leaves encode `hash(pubkey, trust_level)` — enables range proofs on trust level
- Both trees use the same ZK public keys from voter registration (no new key management)

**RepKeeper interface addition:**
```go
// GetMemberTrustTreeRoot returns the current Merkle root of the member trust tree.
// Returns error if tree has not been built yet.
func (k Keeper) GetMemberTrustTreeRoot(ctx context.Context) ([]byte, error)

// GetPreviousMemberTrustTreeRoot returns the root from the last rebuild cycle.
// Used by consuming modules to provide a one-cycle grace period for proof validation.
// Returns nil (not error) if no previous root exists (first rebuild).
func (k Keeper) GetPreviousMemberTrustTreeRoot(ctx context.Context) []byte
```

**x/rep query additions for client support:**

```protobuf
// Returns the current member trust tree root and voter count
rpc GetMemberTrustTree(QueryGetMemberTrustTreeRequest) returns (QueryGetMemberTrustTreeResponse);

// Returns the Merkle proof for a specific voter's leaf in the trust tree
rpc GetMemberTrustProof(QueryGetMemberTrustProofRequest) returns (QueryGetMemberTrustProofResponse);
```

---

## Anonymous Action Circuit

A new Groth16 circuit (BN254) proving membership and minimum trust level without revealing identity. Stored alongside the vote circuit in `zkprivatevoting/`.

### Public Inputs (revealed on-chain)

| Field | Size | Description |
|-------|------|-------------|
| `merkle_root` | 32 bytes | x/rep member trust tree root |
| `nullifier` | 32 bytes | Spam prevention token |
| `min_trust_level` | uint32 | Minimum trust level being proven |
| `domain` | uint64 | Action domain (see Nullifier Scoping below) |
| `scope` | uint64 | Scoping value (epoch, post_id, thread_id, etc.) |

### Private Inputs (known only to prover)

| Field | Size | Description |
|-------|------|-------------|
| `secret_key` | 32 bytes | Voter's secret key (same as x/vote) |
| `trust_level` | uint32 | Member's actual trust level |
| `path_elements` | [20]×32 bytes | Merkle proof sibling hashes |
| `path_indices` | [20]×1 bit | Left/right position at each level |

### Circuit Constraints

1. **Public key derivation:** `publicKey = MiMC_hash(secretKey)`
2. **Leaf computation:** `leaf = MiMC_hash(publicKey, trustLevel)`
3. **Merkle proof:** Computed root from leaf + path must equal `merkle_root`
4. **Trust level range:** `trustLevel >= minTrustLevel` (range check)
5. **Nullifier:** `nullifier = MiMC_hash(domain, secretKey, scope)`
6. **Path index binary:** All `pathIndices[i] ∈ {0, 1}`

**Estimated constraints:** ~14,000 (comparable to vote circuit)

**Verifying key:** Stored in x/vote params as `anon_action_verifying_key` (separate from `vote_verifying_key`). Derived from the same SRS ceremony.

---

## Nullifier Scoping

Nullifiers are deterministic: the same member performing the same action in the same scope always produces the same nullifier. This is what prevents double-posting.

```
nullifier = MiMC_hash(domain, secretKey, scope)
```

### Domain Registry

Each module registers its own domain values to prevent cross-module nullifier collisions:

| Domain | Module | Action | Scope | Effect |
|--------|--------|--------|-------|--------|
| `1` | x/blog | Anonymous post | Current epoch | One anonymous post per member per epoch |
| `2` | x/blog | Anonymous reply | `post_id` | One anonymous reply per member per post |
| `3` | x/forum | Anonymous thread | Current epoch | One anonymous thread per member per epoch |
| `4` | x/forum | Anonymous reply | `thread_id` | One anonymous reply per member per thread |
| `5` | x/forum | Anonymous reaction | `post_id` | One anonymous reaction (upvote or downvote) per member per post |
| `6` | x/collect | Anonymous collection creation | Current epoch | One anonymous collection per member per epoch |
| `7` | x/collect | Anonymous item addition | `collection_id * epoch_multiplier + epoch` | One anonymous item per member per collection per epoch |
| `8` | x/blog | Anonymous post reaction | `post_id` | One anonymous reaction per member per post |
| `9` | x/blog | Anonymous reply reaction | `reply_id` | One anonymous reaction per member per reply |
| `10` | x/collect | Anonymous collection reaction | `collection_id` | One anonymous reaction per member per collection |
| `11` | x/collect | Anonymous item reaction | `item_id` | One anonymous reaction per member per item |

Additional domains can be added by any module. The domain value is a public circuit input and part of the nullifier computation, so nullifiers from different domains never collide even with the same secret key and scope.

### Nullifier Storage (x/common)

Nullifier storage and lookup is provided by **`x/common/keeper/anon.go`** — a shared helper layer that all content modules (x/blog, x/forum, x/collect) import. Each module passes its own `StoreService` to the helpers, so nullifiers are still physically stored in the consuming module's KV store (not in a separate x/common store). This avoids cross-module store access while eliminating code duplication.

**Canonical key structure:**

```
AnonNullifier/{domain}/{scope}/{nullifier_hex} → AnonNullifierEntry { used_at: int64, domain: uint64, scope: uint64 }
```

**Shared helper interface (`x/common/keeper/anon.go`):**

```go
// StoreNullifier records a nullifier as used. Returns ErrNullifierUsed if already present.
func StoreNullifier(ctx context.Context, store storetypes.KVStore, domain uint64, scope uint64, nullifierHex string, blockTime int64) error

// IsNullifierUsed checks whether a nullifier has already been recorded.
func IsNullifierUsed(ctx context.Context, store storetypes.KVStore, domain uint64, scope uint64, nullifierHex string) bool

// PruneEpochNullifiers deletes epoch-scoped nullifiers where scope < currentEpoch - 1.
// Retains current and previous epoch (grace period for in-flight transactions).
func PruneEpochNullifiers(ctx context.Context, store storetypes.KVStore, domain uint64, currentEpoch uint64) uint64

// VerifyAndStoreAnonymousAction performs the full validation sequence shared across all
// anonymous message handlers: param check, trust level check, root verification,
// nullifier dedup, ZK proof verification, and nullifier recording.
func VerifyAndStoreAnonymousAction(
    ctx context.Context,
    store storetypes.KVStore,
    voteKeeper VoteKeeper,
    repKeeper RepKeeper,
    params AnonymousParams,
    proof []byte,
    merkleRoot []byte,
    nullifier []byte,
    minTrustLevel uint32,
    domain uint64,
    scope uint64,
    blockTime int64,
) error
```

Each module calls these helpers from its own message handlers. The module-specific logic (content creation, metadata storage, event emission) remains in the module.

See each module's spec for pruning strategy and EndBlocker integration.

---

## VoteKeeper Interface Extension

x/vote exposes a new verification method for the anonymous action circuit:

```go
type VoteKeeper interface {
    // Existing
    VerifyMembershipProof(ctx context.Context, proof []byte, nullifier []byte) error

    // New — verifies the anonymous action circuit proof
    VerifyAnonymousActionProof(
        ctx context.Context,
        proof []byte,
        merkleRoot []byte,
        nullifier []byte,
        minTrustLevel uint32,
        domain uint64,
        scope uint64,
    ) error
}
```

---

## Relay Pattern

All anonymous posting messages include a `submitter` field that decouples the transaction signer from the anonymous author:

1. Author generates the ZK proof client-side (using their secret key)
2. Author sends the proof + content to a relay (friend, service, or themselves from a different address)
3. Relay signs and broadcasts the transaction, paying gas and storage fees
4. On-chain, the submitter is visible but has no provable link to the anonymous content

**Why this matters:** On-chain transaction analysis can correlate addresses with behavior patterns. Using a relay breaks this correlation.

**Without a relay:** Members can still submit directly from their own address. The ZK proof guarantees the post could have come from *any* active member at the proven trust level — the submitter address doesn't prove authorship.

---

## Integration Guide for Content Modules

To add anonymous posting to a content module:

### 1. Add Dependencies

```go
// In expected_keepers.go
type VoteKeeper interface {
    VerifyAnonymousActionProof(ctx context.Context, proof []byte, merkleRoot []byte, nullifier []byte, minTrustLevel uint32, domain uint64, scope uint64) error
}

type RepKeeper interface {
    // ... existing methods ...
    GetMemberTrustTreeRoot(ctx context.Context) ([]byte, error)
}
```

Wire `VoteKeeper` as an **optional** dependency via depinject. If nil, anonymous messages return `ErrAnonymousPostingUnavailable`.

### 2. Add Parameters

```protobuf
bool anonymous_posting_enabled = N;       // Master toggle (default: true)
uint32 anonymous_min_trust_level = N+1;   // Minimum trust level (default: 2 = ESTABLISHED)
```

Include both in the module's `OperationalParams` so the Operations Committee can adjust them without governance.

### 3. Add Message Types

Each anonymous message should include:

```protobuf
message MsgCreateAnonymous<Action> {
  string submitter = 1;         // Tx signer (pays gas; NOT the author)
  // ... content fields ...
  bytes proof = N;              // ~500-byte Groth16 proof
  bytes nullifier = N+1;        // 32-byte nullifier
  bytes merkle_root = N+2;      // Trust tree root used for proof
  uint32 min_trust_level = N+3; // Trust level proven
}
```

### 4. Add Validation Logic

In the message handler, use the shared `x/common` helper for the ZK verification sequence, then add module-specific logic:

```go
func (k msgServer) CreateAnonymousPost(ctx context.Context, msg *types.MsgCreateAnonymousPost) (*types.MsgCreateAnonymousPostResponse, error) {
    // Steps 1–5 handled by x/common shared helper:
    err := commonkeeper.VerifyAndStoreAnonymousAction(
        ctx, k.storeService, k.voteKeeper, k.repKeeper,
        anonParams, msg.Proof, msg.MerkleRoot, msg.Nullifier,
        msg.MinTrustLevel, domain, scope, blockTime,
    )
    if err != nil {
        return nil, err
    }

    // Module-specific logic:
    // 6. Charge storage fee from submitter
    // 7. Create content with creator = module_account_address (sentinel)
    // 8. Store anonymous metadata (nullifier, merkle_root, proven_trust_level)
    // 9. Emit event (WITHOUT submitter — exclude from indexer correlation)
}
```

The `VerifyAndStoreAnonymousAction` helper performs: param enabled check, trust level validation, Merkle root verification (current + previous root grace period), nullifier dedup, ZK proof verification via `VoteKeeper.VerifyAnonymousActionProof`, and nullifier recording. See § Nullifier Storage above for the full interface.

### 5. Access Control Rules

- **No edit/delete by author** — anonymous content is immutable (no author identity to verify)
- **Post author moderation** — post/thread authors can hide anonymous replies (same as regular)
- **Operations Committee** — can delete anonymous content for policy violations
- **Reactions** — regular identified members can react to anonymous content normally

### 6. Metadata Storage

Store `AnonymousPostMetadata` linked to the content item:

```protobuf
message AnonymousPostMetadata {
  uint64 content_id = 1;           // References the post/reply/thread
  bytes nullifier = 2;             // 32-byte nullifier
  bytes merkle_root = 3;           // Trust tree root at proof time
  uint32 proven_trust_level = 4;   // Minimum trust level proven
}
```

### 7. Queries

Add queries for anonymous metadata:

```protobuf
rpc AnonymousPostMeta(QueryAnonymousPostMetaRequest) returns (QueryAnonymousPostMetaResponse);
rpc IsNullifierUsed(QueryIsNullifierUsedRequest) returns (QueryIsNullifierUsedResponse);
```

Anonymous content appears in standard list queries with `creator` set to the module account address. Clients detect anonymous content by checking `creator == module_account` and then fetching metadata.

---

## Client Workflow

**Proof generation (client-side, ~2-3 seconds on modern hardware):**

1. **Derive ZK keys** from account signature (same as x/vote voter registration):
   ```
   secretKey = derive_from_signature(account_key)
   publicKey = MiMC_hash(secretKey)
   ```

2. **Fetch trust tree data** from x/rep:
   - Current `MemberTrustTreeRoot`
   - Merkle proof for the voter's leaf (path elements + indices)
   - Voter's current trust level

3. **Compute nullifier:**
   ```
   nullifier = MiMC_hash(domain, secretKey, scope)
   ```

4. **Generate Groth16 proof** with the anonymous action circuit

5. **Submit transaction** (directly or via relay)

---

## Security Considerations

### Anonymity Guarantees

- ZK proof reveals *nothing* about the poster except that they are an active member at or above the proven trust level
- The anonymity set is all active members at that trust level — the larger the set, the stronger the anonymity
- Nullifiers are unlinkable across different scopes (different scopes produce different nullifiers)
- Same-scope nullifiers are deterministic (prevents double-posting) but don't reveal identity

### Anonymity Limitations

- **Transaction timing:** The submitter address and submission timestamp are visible on-chain. Using a relay mitigates this.
- **Writing style:** Stylometric analysis of post content could deanonymize frequent anonymous posters. This is outside the protocol's threat model.
- **Small anonymity sets:** If only 3 members are ESTABLISHED+, anonymity is weak. The `anonymous_min_trust_level` param should be set to a level with sufficient membership.
- **Merkle root freshness:** Consuming modules accept the current root or the immediately previous root (one-rebuild-cycle grace period). If a member's trust level was just upgraded, they must wait for the next EndBlocker tree rebuild. Roots older than one cycle are rejected.

### Spam Prevention

- One anonymous action per scope per identity (nullifier-enforced)
- Storage fees apply (paid by submitter)
- Rate limits apply to the submitter address (shared with regular actions)
- Trust level minimum (ESTABLISHED by default) raises the Sybil cost

### Proof Soundness

- Groth16 proofs are computationally sound under the knowledge-of-exponent assumption
- Verifying key derived from trusted SRS ceremony (same as x/vote)
- Proof verification is ~2ms on-chain (negligible gas overhead)
- Invalid proofs are rejected deterministically — no false positives

### Moderation

- Anonymous content can still be moderated (hidden/deleted) by content authors and Operations Committee
- Persistent abuse from the same nullifier pattern can be flagged (same nullifier = same member, even if identity unknown)
- In extreme cases (illegal content), the chain's governance can coordinate with law enforcement — the ZK proof guarantees the poster *is* a registered member, narrowing the search space

### Recommended Default: ESTABLISHED (Trust Level 2)

Anonymous posting is a powerful tool that can be abused for harassment or manipulation. Requiring ESTABLISHED trust ensures the poster has a meaningful reputation at stake — even though their identity is hidden per-post, systematic abuse patterns can be investigated and the ZK proof guarantees they *are* a real member who could be identified through other means if warranted.

---

## Conviction-Based Lifetime Extension

Anonymous content is ephemeral by default — it carries a TTL and is automatically tombstoned (x/blog) or pruned (x/collect) when the TTL elapses. This is a deliberate spam control: unvalued content self-cleans. However, useful anonymous content (fraud watchlists, whistleblower evidence, valuable tips) should be able to survive beyond its initial TTL if the community actively signals its value.

**Conviction staking as a lifetime signal:** If anonymous content has accumulated enough community conviction (DREAM staked via x/rep's content staking system) by the time its initial TTL expires, the content enters a **conviction-sustained** state. Conviction must be maintained continuously above the threshold — every stake and unstake operation on that content is checked in real time, and the EndBlocker verifies conviction at each renewal deadline.

This creates a three-tier lifecycle for anonymous content:
1. **Ephemeral** (default): expires after `ephemeral_content_ttl` / collection TTL
2. **Conviction-sustained**: conviction must stay ≥ threshold continuously; expires if it drops
3. **Pinned** (permanent): member-initiated `MsgPinPost` / `MsgPinCollection` clears the TTL entirely

### Parameters

Each content module adds these operational params:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `conviction_renewal_threshold` | `sdk.Dec` | `100.0` | Minimum conviction score to enter and maintain conviction-sustained state |
| `conviction_renewal_period` | `int64` | Same as `ephemeral_content_ttl` | Duration (seconds or blocks) of each renewal period; conviction is verified again at each deadline |

`conviction_renewal_threshold` is an operational param (Operations Committee can adjust). `conviction_renewal_period` is also operational.

Setting `conviction_renewal_threshold = 0` disables conviction renewal (all expired content is tombstoned regardless of conviction). Setting it very high limits renewal to only the most endorsed content.

### State Transition

When anonymous content's initial TTL expires (the first `expires_at` set at creation time):

```
// In EndBlocker, when expires_at <= block_time for anonymous content:
conviction = repKeeper.GetContentConviction(ctx, targetType, targetID)
if conviction.Score >= params.conviction_renewal_threshold:
    content.conviction_sustained = true
    content.expires_at = block_time + params.conviction_renewal_period
    update ExpiryIndex (remove old, add new)
    emit content.conviction_sustained event
else:
    proceed with normal expiry (tombstone or prune)
```

### Renewal Deadline Check

At each renewal deadline (when `expires_at` elapses for conviction-sustained content), the EndBlocker re-verifies conviction:

```
// In EndBlocker, when expires_at <= block_time for conviction-sustained content:
conviction = repKeeper.GetContentConviction(ctx, targetType, targetID)
if conviction.Score >= params.conviction_renewal_threshold:
    content.expires_at = block_time + params.conviction_renewal_period
    update ExpiryIndex
    emit content.renewed event (conviction_score, new_expires_at)
else:
    // Conviction has decayed below threshold
    content.conviction_sustained = false
    proceed with normal expiry (tombstone or prune)
```

**Why this is safe without unstake enforcement:** Time-weighted conviction (`conviction(t) = stake_amount * (1 - 2^(-t / half_life))`) means a stake placed moments before a renewal check contributes near-zero conviction. Flash-staking is ineffective because meaningful conviction requires stakes held for a significant fraction of the half-life. Stakers can freely unstake at any time — if conviction drops below threshold, the content simply expires at the next renewal check. The market speaks.

### Staking Freedom

Stakers can stake and unstake content conviction at any time with no restrictions. If a whale unstakes and conviction drops below threshold, the content dies at the next renewal check. This is the correct outcome — community support has been withdrawn. No lock-in mechanism is needed because time-weighting already ensures that only sustained commitment produces meaningful conviction.

### Key Rules

- **Anonymous content only**: Non-anonymous ephemeral content (non-member posts) is not eligible for conviction renewal — it uses the membership auto-upgrade path instead (if the creator later joins x/rep, the content becomes permanent). Conviction renewal is specifically for content that has no known author to upgrade
- **Initial TTL must elapse**: Conviction-sustained state is only entered at the first TTL expiry. Content cannot skip its initial ephemeral period — this ensures the community has time to evaluate the content before it can enter the sustained state
- **Deposits held through renewals**: For x/collect, the collection deposit and per-item deposits remain held in the module account through renewals. They are refunded only when the content finally expires (conviction dropped), is pinned, or is deleted
- **Free unstaking**: Stakers can unstake at any time. If conviction drops below threshold, the content expires at the next renewal check. No lock-in or floor enforcement is needed — time-weighted conviction already prevents flash-staking
- **Pinning overrides renewal**: If a member pins conviction-sustained content, the TTL is cleared permanently and the content leaves the renewal cycle

### Interaction with Other Mechanisms

| Mechanism | Interaction |
|-----------|-------------|
| **Pinning** | Pinning makes content permanent. A member observing high-conviction content may choose to pin it, making the conviction signal moot (content persists regardless) |
| **Sentinel moderation** | Hidden content is still eligible for conviction renewal. If anonymous content is hidden by a sentinel but maintains high conviction, it survives. The community and the sentinel may disagree — the hide remains visible, but the content is not tombstoned |
| **Author bonds** | Author bonds are independent of conviction renewal. A bonded anonymous collection with low conviction still expires; an unbonded one with high conviction still gets renewed. Bonds signal creator commitment, conviction signals community endorsement — different axes |
| **Staker incentives** | Stakers commit real DREAM as a quality signal. Time-weighted conviction rewards early, sustained stakers (higher conviction per DREAM over time). Stakers can freely exit at any time — if conviction drops, the content dies at the next renewal check |

### Security Considerations

- **No flash-staking**: Time-weighted conviction (`conviction(t) = stake_amount * (1 - 2^(-t / half_life))`) makes flash-staking ineffective. A stake placed moments before a renewal check contributes near-zero conviction. Meaningful conviction requires stakes held for a significant duration relative to the half-life
- **Grief prevention**: The conviction threshold should be high enough that a single member's stake cannot sustain content indefinitely. With `max_content_stake_per_member` caps and the time-weighted conviction formula, sustaining content requires broad community support
- **Free unstaking**: Stakers can unstake at any time with no restrictions. If conviction drops below threshold, the content expires at the next renewal check. No lock-in mechanism is needed
- **Cost of sustaining spam**: Stakers lock DREAM for as long as they choose to stake. Sustaining unwanted content has a real economic cost — the DREAM is illiquid while staked, subject to unstaked decay, and earns no staking rewards (conviction staking has no yield)
- **Threshold governance**: The Operations Committee can raise `conviction_renewal_threshold` if conviction renewal is being abused, or lower it to make it easier for minority-interest content to survive
- **No cross-module state**: Content modules query x/rep for conviction on demand in their EndBlockers. No floors, locks, or other cross-module state needs cleanup — x/rep stores stakes and computes conviction, content modules make keep-or-flush decisions

---

## Anonymous Posting Subsidy

To encourage balanced democratic dialogue, the Commons Council can subsidize the cost of anonymous posting so that members (via approved relays) don't bear gas or spam tax costs.

### Funding Source

The subsidy is funded from the **Commons Council group treasury**, which receives its allocation via x/split (50% of community pool revenue). This keeps the subsidy within the Three Pillars governance hierarchy — the Operations Committee controls the budget and relay list without requiring x/distribution or x/gov involvement.

```
x/distribution community pool
    → x/split (50% to Commons Council)
        → Commons Council group treasury
            → anon subsidy module account (per epoch)
                → covers relay gas + spam tax for anonymous posts
```

### Parameters

Each content module (x/blog, x/forum) adds these operational params:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `anon_subsidy_budget_per_epoch` | `sdk.Coin` | `100spark` | Amount auto-transferred from Commons Council treasury to the module's subsidy account each epoch |
| `anon_subsidy_max_per_post` | `sdk.Coin` | `2spark` | Maximum subsidy per anonymous post/reply (caps per-transaction cost) |
| `anon_subsidy_relay_addresses` | `[]string` | `[]` | Approved relay addresses eligible for subsidy (Operations Committee managed) |

### Mechanism

1. **Epoch funding:** At each epoch boundary (in EndBlocker), the module requests `anon_subsidy_budget_per_epoch` from the Commons Council treasury via `x/commons` spending interface. If the treasury has insufficient funds, the transfer is partial or skipped — no error.
2. **Subsidized submission:** When a transaction signer is in `anon_subsidy_relay_addresses` and submits an anonymous post/reply, the module pays gas refund + spam tax from the subsidy account instead of charging the relay.
3. **Per-post cap:** The subsidy per transaction is capped at `anon_subsidy_max_per_post` to prevent a single expensive operation from draining the budget. Any cost above the cap is charged to the relay.
4. **Budget exhaustion:** When the epoch budget is spent, approved relays pay normally until the next epoch refill.
5. **Rollover:** Unspent budget carries over to the next epoch (accumulates in the module's subsidy account).
6. **Non-approved relays:** Addresses not in `anon_subsidy_relay_addresses` always pay their own costs, regardless of budget.

### Governance Surface

The Operations Committee (under Commons Council) manages all subsidy params:

- **Add/remove relay addresses:** Operational param update, no full governance proposal needed
- **Adjust budget:** Operational param update
- **Emergency disable:** Set `anon_subsidy_budget_per_epoch` to zero

This is intentionally minimal — the only governance action needed to bootstrap the subsidy is voting to approve the initial relay addresses. Budget flows automatically from the existing treasury allocation.

### Integration with x/commons

The module uses the existing `CommonsKeeper.SpendFromTreasury()` interface to draw funds:

```go
// In EndBlocker, once per epoch:
err := k.commonsKeeper.SpendFromTreasury(ctx, "commons", k.moduleAddress, subsidyBudget)
if err != nil {
    // Treasury insufficient — skip, no error
}
```

### Why Not x/feegrant?

`x/feegrant` covers gas costs but not protocol-level fees (spam tax, storage fees) that are charged inside message handlers. The subsidy mechanism operates at the module level, covering the full cost of anonymous posting in a single, self-renewing system without requiring repeated governance proposals for grant renewals.
