# Anonymous Posting via x/shield

## Problem

Members may need to post content without revealing their identity — for whistleblowing, controversial opinions, honest feedback on initiatives, or anonymous peer review. Standard on-chain transactions link content to a specific address, making anonymity impossible without protocol-level support.

## Solution

The `x/shield` module provides a **unified privacy layer** that any content module can use for anonymous operations. Members prove they are an active member meeting a minimum trust level — without revealing *which* member they are — via a single `MsgShieldedExec` entry point. A nullifier system prevents spam: one anonymous action per scope per identity.

Content modules (`x/blog`, `x/forum`, `x/collect`, etc.) integrate anonymous posting by implementing the `ShieldAware` interface. They do **not** need their own anonymous message types, nullifier storage, or proof verification logic.

---

## Architecture

```
Client generates ZK proof (Groth16/BN254)
    │
    ▼
MsgShieldedExec { inner_message, proof, nullifier, ... }
    │
    ▼
x/shield:
    ├── Verify ZK proof against trust tree root
    ├── Check nullifier not used (centralized store)
    ├── Check per-identity rate limit
    ├── Pay gas from shield module account
    └── Dispatch inner message to target module
            │
            ▼
Target module (e.g., x/blog):
    ├── ShieldAware.IsShieldCompatible() → true
    └── Execute message with creator = shield module account
```

### Two Execution Modes

**Immediate mode**: Inner message and ZK proof are submitted in cleartext. The operation executes in the same block. Best for latency-sensitive actions (posts, reactions) where content visibility is acceptable — the submitter address is visible but has no provable link to the anonymous author.

**Encrypted Batch mode**: The inner message and proof are encrypted with the TLE master public key. The encrypted payload is queued. At epoch boundaries, validators produce decryption shares; once threshold is reached, the batch is decrypted, shuffled deterministically, and executed. Best for voting and actions where both identity AND content must be hidden until decryption.

---

## Dependencies

| Module | Purpose |
|--------|---------|
| `x/shield` | ZK proof verification, nullifier management, module-paid gas, shielded execution dispatch |
| `x/rep` | Maintains the **member trust tree** (Merkle tree with trust-level-encoded leaves) |

Content modules depend only on implementing the `ShieldAware` interface. They do **not** need a direct keeper dependency on x/shield.

---

## Member Trust Tree (maintained by x/rep)

A persistent KV-based sparse Merkle tree maintained by x/rep, providing trust-level-aware ZK proofs for x/shield.

```
Leaf = MiMC_hash(zk_public_key, trust_level)
Tree depth: 20 (~1,048,576 members)
Hash function: MiMC (SNARK-friendly)
```

**Lifecycle:**
1. Tree is marked dirty when a member's trust level changes, a ZK public key is registered/updated, or a member is deactivated
2. x/rep's EndBlocker incrementally rebuilds the tree when dirty (O(depth) updates via dirty member tracking)
3. The current root is stored in x/rep state as `MemberTrustTreeRoot`; the previous root is retained for a one-cycle grace period
4. x/shield accepts proofs against either the current or previous root

**RepKeeper interface (used by x/shield):**
```go
func (k Keeper) GetTrustTreeRoot(ctx context.Context) ([]byte, error)
func (k Keeper) GetPreviousTrustTreeRoot(ctx context.Context) []byte
```

**x/rep queries for client support:**
```protobuf
rpc GetMemberTrustTree(QueryGetMemberTrustTreeRequest) returns (QueryGetMemberTrustTreeResponse);
rpc GetMemberTrustProof(QueryGetMemberTrustProofRequest) returns (QueryGetMemberTrustProofResponse);
```

---

## ShieldCircuit (Unified ZK Circuit)

A single Groth16 circuit (BN254) proving membership and minimum trust level without revealing identity. Located in `tools/zk/circuit/shield_circuit.go`.

### Public Inputs (revealed on-chain)

| Field | Size | Description |
|-------|------|-------------|
| `MerkleRoot` | 32 bytes | x/rep member trust tree root (current or previous) |
| `Nullifier` | 32 bytes | Action-specific replay prevention |
| `RateLimitNullifier` | 32 bytes | Per-identity epoch-scoped rate limiting |
| `MinTrustLevel` | uint32 | Minimum trust level being proven |
| `Scope` | uint64 | Nullifier scope value (epoch, post_id, etc.) |
| `RateLimitEpoch` | uint64 | Current shield epoch |

### Private Inputs (known only to prover)

| Field | Size | Description |
|-------|------|-------------|
| `secret_key` | 32 bytes | Member's ZK secret key |
| `trust_level` | uint32 | Member's actual trust level |
| `path_elements` | [20]x32 bytes | Merkle proof sibling hashes |
| `path_indices` | [20]x1 bit | Left/right position at each level |

### Circuit Constraints

1. **Public key derivation:** `publicKey = MiMC_hash(secretKey)`
2. **Leaf computation:** `leaf = MiMC_hash(publicKey, trustLevel)`
3. **Merkle proof:** Computed root from leaf + path must equal `MerkleRoot`
4. **Trust level range:** `trustLevel >= MinTrustLevel` (range check)
5. **Nullifier:** `nullifier = MiMC_hash(domain, secretKey, scope)`
6. **Rate limit nullifier:** `rateLimitNullifier = MiMC_hash(secretKey, rateLimitEpoch)`
7. **Path index binary:** All `pathIndices[i] in {0, 1}`

**Verification key:** Stored on-chain in x/shield state by circuit ID (`shield_v1`). Updated via governance.

---

## Nullifier Scoping

Nullifiers are deterministic: the same member performing the same action in the same scope always produces the same nullifier. This prevents double-posting.

```
nullifier = MiMC_hash(domain, secretKey, scope)
```

All nullifiers are stored centrally in x/shield (not per-module). Each registered operation specifies its nullifier domain, scope type, and optional scope field path.

### Scope Types

| Scope Type | Scope Value | Meaning |
|------------|-------------|---------|
| `NULLIFIER_SCOPE_GLOBAL` | 0 | One action ever (e.g., anonymous challenges) |
| `NULLIFIER_SCOPE_EPOCH` | epoch number | One action per epoch (e.g., anonymous posts) |
| `NULLIFIER_SCOPE_MESSAGE_FIELD` | hash of field value | One action per unique field (e.g., one reaction per post) |

### Domain Registry (Genesis Defaults)

| Domain | Module | Action | Scope | Effect |
|--------|--------|--------|-------|--------|
| `1` | x/blog | Anonymous post | EPOCH | One anonymous post per member per epoch |
| `2` | x/blog | Anonymous reply | MESSAGE_FIELD (`post_id`) | One anonymous reply per member per post |
| `8` | x/blog | Anonymous reaction | MESSAGE_FIELD (`post_id`) | One anonymous reaction per member per post |
| `11` | x/forum | Anonymous post | EPOCH | One anonymous post per member per epoch |
| `12` | x/forum | Anonymous upvote | MESSAGE_FIELD (`post_id`) | One anonymous upvote per member per post |
| `13` | x/forum | Anonymous downvote | MESSAGE_FIELD (`post_id`) | One anonymous downvote per member per post |
| `21` | x/collect | Anonymous collection | EPOCH | One anonymous collection per member per epoch |
| `22` | x/collect | Anonymous upvote | MESSAGE_FIELD (`target_id`) | One anonymous upvote per member per item |
| `23` | x/collect | Anonymous downvote | MESSAGE_FIELD (`target_id`) | One anonymous downvote per member per item |
| `31` | x/commons | Anonymous proposal | EPOCH | One anonymous proposal per member per epoch |
| `32` | x/commons | Anonymous vote | MESSAGE_FIELD (`proposal_id`) | One anonymous vote per member per proposal |
| `41` | x/rep | Anonymous challenge | GLOBAL | One anonymous challenge per member ever |

Additional domains can be registered via governance (`MsgRegisterShieldedOp`).

---

## Module-Paid Gas

The x/shield module account holds gas reserves (uspark) and pays transaction fees for all `MsgShieldedExec` transactions. Submitters need zero balance. This replaces the old per-module anonymous posting subsidy.

**Funding:** BeginBlocker auto-refills from the community pool when balance drops below `min_gas_reserve`, capped at `max_funding_per_day`.

**Rate limiting:** Per-identity rate limiting (via `RateLimitNullifier`) prevents gas abuse without revealing identity. Each identity is limited to `max_execs_per_identity_per_epoch` operations per shield epoch.

---

## Integration Guide for Content Modules

To add anonymous posting support to a content module via x/shield:

### 1. Implement ShieldAware Interface

```go
// In keeper/shield_aware.go
func (k msgServer) IsShieldCompatible(ctx context.Context, msg sdk.Msg) bool {
    switch msg.(type) {
    case *types.MsgCreatePost, *types.MsgCreateReply, *types.MsgReact:
        return true
    default:
        return false
    }
}
```

This is the only code change needed in the content module. The module opts in to specific message types being callable via `MsgShieldedExec`.

### 2. Register ShieldAware in app.go

```go
// In app.go, after depinject:
app.ShieldKeeper.RegisterShieldAwareModule("/sparkdream.blog.v1.", &app.BlogKeeper)
```

### 3. Register Operations at Genesis

Operations are registered in x/shield's genesis state (see `x/shield/types/genesis.go`). Each operation specifies the message type URL, proof domain, minimum trust level, nullifier domain, scope type, and batch mode.

### 4. Content Creation

When a shielded operation executes, the inner message's `creator` field is set to the **shield module account address**. The content module creates the content normally — the creator being the shield module address is what marks it as anonymous.

### 5. Access Control Rules

- **No edit/delete by author** — anonymous content is immutable (no author identity to verify)
- **Post author moderation** — post/thread authors can hide anonymous replies (same as regular)
- **Operations Committee** — can delete anonymous content for policy violations
- **Reactions** — regular identified members can react to anonymous content normally

---

## Client Workflow

**Proof generation (client-side, ~2-3 seconds on modern hardware):**

1. **Register ZK public key** (one-time): Call `MsgRegisterZkPublicKey` in x/rep to store the public key on-chain and add to the trust tree.

2. **Fetch trust tree data** from x/rep:
   - Current `MemberTrustTreeRoot`
   - Merkle proof for the member's leaf (path elements + indices)
   - Member's current trust level

3. **Compute nullifiers:**
   ```
   nullifier = MiMC_hash(domain, secretKey, scope)
   rateLimitNullifier = MiMC_hash(secretKey, currentEpoch)
   ```

4. **Generate Groth16 proof** with the ShieldCircuit

5. **Submit transaction:**
   ```bash
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
   ```

The submitter can be any address (including the member's own address). Since x/shield pays gas, the submitter doesn't need any balance.

---

## Security Considerations

### Anonymity Guarantees

- ZK proof reveals *nothing* about the poster except that they are an active member at or above the proven trust level
- The anonymity set is all active members at that trust level — the larger the set, the stronger the anonymity
- Nullifiers are unlinkable across different scopes (different scopes produce different nullifiers)
- Same-scope nullifiers are deterministic (prevents double-posting) but don't reveal identity
- Rate limit nullifiers are scoped per epoch — they identify "the same person" for rate limiting within an epoch but are unlinkable across epochs

### Anonymity Limitations

- **Transaction timing:** The submitter address and submission timestamp are visible on-chain. In immediate mode, content is visible. Using encrypted batch mode and/or a relay mitigates this.
- **Writing style:** Stylometric analysis of post content could deanonymize frequent anonymous posters. This is outside the protocol's threat model.
- **Small anonymity sets:** If only 3 members are ESTABLISHED+, anonymity is weak. The minimum trust level should be set to a level with sufficient membership.
- **Merkle root freshness:** x/shield accepts the current root or the immediately previous root (one-rebuild-cycle grace period). Roots older than one cycle are rejected.

### Spam Prevention

- One anonymous action per scope per identity (nullifier-enforced)
- Per-identity rate limiting via `RateLimitNullifier` (max operations per epoch)
- Module-paid gas funded from community pool with daily cap
- Trust level minimum raises the Sybil cost
- Governance can deregister abused operations

### Proof Soundness

- Groth16 proofs are computationally sound under the knowledge-of-exponent assumption
- Verification key stored on-chain and updateable only via governance
- Proof verification is ~2ms on-chain (negligible gas overhead)
- Invalid proofs are rejected deterministically — no false positives

### Moderation

- Anonymous content can still be moderated (hidden/deleted) by content authors and Operations Committee
- Persistent abuse from the same nullifier pattern can be flagged (same nullifier = same member, even if identity unknown)
- In extreme cases (illegal content), the chain's governance can coordinate with law enforcement — the ZK proof guarantees the poster *is* a registered member, narrowing the search space

### Recommended Default: PROVISIONAL (Trust Level 1)

Most genesis-registered operations require trust level 1 (PROVISIONAL). Modules can require higher trust levels by registering operations with a higher `min_trust_level`.

---

## Conviction-Based Lifetime Extension

Anonymous content is ephemeral by default — it carries a TTL and is automatically tombstoned (x/blog) or pruned (x/collect) when the TTL elapses. This is a deliberate spam control: unvalued content self-cleans. However, useful anonymous content (fraud watchlists, whistleblower evidence, valuable tips) should be able to survive beyond its initial TTL if the community actively signals its value.

**Conviction staking as a lifetime signal:** If anonymous content has accumulated enough community conviction (DREAM staked via x/rep's content staking system) by the time its initial TTL expires, the content enters a **conviction-sustained** state. Conviction must be maintained continuously above the threshold — every stake and unstake operation on that content is checked in real time, and the EndBlocker verifies conviction at each renewal deadline.

This creates a three-tier lifecycle for anonymous content:
1. **Ephemeral** (default): expires after `ephemeral_content_ttl` / collection TTL
2. **Conviction-sustained**: conviction must stay >= threshold continuously; expires if it drops
3. **Pinned** (permanent): member-initiated `MsgPinPost` / `MsgPinCollection` clears the TTL entirely

### Parameters

Each content module adds these operational params:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `conviction_renewal_threshold` | `sdk.Dec` | `100.0` | Minimum conviction score to enter and maintain conviction-sustained state |
| `conviction_renewal_period` | `int64` | Same as `ephemeral_content_ttl` | Duration of each renewal period; conviction is verified again at each deadline |

Setting `conviction_renewal_threshold = 0` disables conviction renewal (all expired content is tombstoned regardless of conviction). Setting it very high limits renewal to only the most endorsed content.

### Key Rules

- **Anonymous content only**: Non-anonymous ephemeral content uses the membership auto-upgrade path instead (if the creator later joins x/rep, the content becomes permanent). Conviction renewal is specifically for content that has no known author to upgrade
- **Initial TTL must elapse**: Conviction-sustained state is only entered at the first TTL expiry
- **Deposits held through renewals**: For x/collect, deposits remain held through renewals and refunded only when content finally expires, is pinned, or is deleted
- **Free unstaking**: Stakers can unstake at any time. If conviction drops below threshold, the content expires at the next renewal check
- **Pinning overrides renewal**: Pinning makes content permanent, leaving the renewal cycle

### Security Considerations

- **No flash-staking**: Time-weighted conviction (`conviction(t) = stake_amount * (1 - 2^(-t / half_life))`) makes flash-staking ineffective
- **Grief prevention**: The conviction threshold should be high enough that a single member's stake cannot sustain content indefinitely
- **Cost of sustaining spam**: DREAM is illiquid while staked, subject to unstaked decay, and earns no staking rewards
- **Threshold governance**: The Operations Committee can adjust `conviction_renewal_threshold` via operational params
