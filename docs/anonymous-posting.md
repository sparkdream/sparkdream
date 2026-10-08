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
        (proven trust level passed in the context)
```

### Two Execution Modes

**Immediate mode**: Inner message and ZK proof are submitted in cleartext. The operation executes in the same block. Best for latency-sensitive actions (posts, reactions) where content visibility is acceptable — the submitter address is visible, but every anonymous client signs with the same shared public submitter (see Client Workflow), so it has no link to the anonymous author.

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

**x/rep query for client support:**
```protobuf
// GET /sparkdream/rep/v1/trust_tree; CLI: sparkdreamd q rep trust-tree (paginated)
rpc TrustTree(QueryTrustTreeRequest) returns (QueryTrustTreeResponse);
```

`TrustTree` returns the root, depth, `leaf_count` and every non-empty leaf (`index`, `hash`) in index order; empty leaves (zero hash) are not stored or returned. There is deliberately no per-member proof query: a client downloads the whole tree, finds its own leaf and computes its Merkle path locally, so the request reveals nothing about who is asking.

Each page reads the root at its own height, so a multi-page fetch can straddle a tree change (a member deactivated between pages zeroes a leaf the client already read) and rebuild a root that matches no chain state. Fetch every page at one pinned height (gRPC/REST `x-cosmos-block-height` header, CLI `--height`) and check the rebuilt root equals that response's `root`; if it doesn't, refetch. A proof built on a torn root fails closed (the chain rejects an unknown Merkle root), but only a pinned fetch avoids the wasted proof.

`MsgRegisterZkPublicKey` requires the key to be a canonical BN254 scalar (32 bytes, big-endian value below the field modulus); anything else is rejected with `ErrInvalidRequest`, since it would hash to a leaf no proof can open.

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
| `Scope` | field element | Domain-separated scope element `ScopeElement(domain, rawScope) = MiMC(be(domain), be(rawScope))`, where `rawScope` is the operation's scope value (epoch, post_id, etc.) |
| `RateLimitEpoch` | uint64 | Current shield epoch |
| `MessageHash` | field element | `ShieldMessageHash` of the exact inner message (or `ShieldOwnedMessageHash`, which also binds the owner sequence, for ownership-mode ops) |

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
5. **Nullifier:** `nullifier = MiMC_hash(secretKey, Scope)` where `Scope = MiMC_hash(domain, rawScope)` is computed outside the circuit (by the prover and, independently, by the chain)
6. **Rate limit nullifier:** `rateLimitNullifier = MiMC_hash(secretKey, MaxUint64, rateLimitEpoch)`
7. **Path index binary:** All `pathIndices[i] in {0, 1}`

**Verification key:** Stored on-chain in x/shield state by circuit ID (`shield_v1`). Set at genesis; changed only by a chain upgrade (no message sets it).

---

## Nullifier Scoping

Nullifiers are deterministic: the same member performing the same action in the same scope always produces the same nullifier. This prevents double-posting.

```
scopeElement = MiMC_hash(domain, rawScope)        // crypto.ScopeElement
nullifier    = MiMC_hash(secretKey, scopeElement) // crypto.ComputeScopedNullifier
```

The circuit itself only hashes `(secretKey, Scope)`. The chain never accepts a raw scope as the `Scope` public input: it always derives the scope element from the registered operation's `nullifier_domain` and the resolved raw scope. Without the domain, one member's nullifiers would match across every operation sharing a raw scope (blog post 5, forum post 5, commons proposal 5; every GLOBAL op), linking that member's actions across modules and epochs. With it, each (domain, raw scope) pair gives an unrelated nullifier.

All nullifiers are stored centrally in x/shield (not per-module), keyed by `(domain, rawScope, nullifier)`. Each registered operation specifies its nullifier domain, scope type, and optional scope field path.

### Scope Types

| Scope Type | Scope Value | Meaning |
|------------|-------------|---------|
| `NULLIFIER_SCOPE_GLOBAL` | 0 | One action ever (e.g., anonymous challenges) |
| `NULLIFIER_SCOPE_EPOCH` | `epoch / epoch_window` (window 0 or 1 = every epoch) | One action per window of `epoch_window` shield epochs (e.g., anonymous posts) |
| `NULLIFIER_SCOPE_MESSAGE_FIELD` | field value (two-field paths packed as `(qualifier << 56) \| id`; fallback paths `a\|b` use `(1 << 56) \| a` when `a` is non-zero, else `b`) | One action per unique field (e.g., one reaction per post) |

### Domain Registry (Genesis Defaults)

| Domain | Module | Action | Scope | Effect |
|--------|--------|--------|-------|--------|
| `1` | x/blog | Anonymous post | EPOCH (window 12) | One anonymous post per member per 12 epochs (~1 hour) |
| `2` | x/blog | Anonymous reply | MESSAGE_FIELD (`post_id`) | One anonymous reply per member per post |
| `8` | x/blog | Anonymous reaction | MESSAGE_FIELD (`reply_id\|post_id`: `(1 << 56) \| reply_id` on a reply, else `post_id`) | One anonymous reaction per member per post, and per reply |
| `11` | x/forum | Anonymous post or reply | EPOCH (window 3) | One anonymous post or reply per member per 3 epochs (~15 minutes) |
| `12` | x/forum | Anonymous upvote or downvote | MESSAGE_FIELD (`post_id`) | One anonymous vote (up or down) per member per post |
| `21` | x/collect | Anonymous collection | EPOCH (window 288) | One anonymous collection per member per 288 epochs (~1 day) |
| `22` | x/collect | Anonymous upvote or downvote | MESSAGE_FIELD (`target_type`, `target_id`, packed as `(target_type << 56) \| target_id`) | One anonymous vote (up or down) per member per collection or item |
| `23` | x/collect | Manage own anonymous collection (8 messages) | Ownership mode: the collection's owner claim | Only the creator; nothing recorded under 23 (see below) |
| `31` | x/commons | Anonymous proposal | EPOCH | One anonymous proposal per member per epoch |
| `32` | x/commons | Anonymous vote | MESSAGE_FIELD (`proposal_id`) | One anonymous vote per member per proposal |
| `41` | x/rep | Anonymous challenge | GLOBAL | One anonymous challenge per member ever |
| `51` | x/federation | Anonymous arbiter hash | MESSAGE_FIELD (`content_id`) | One arbiter vote per member per federated content |

Additional domains can be registered via governance (`MsgRegisterShieldedOp`).

### Ownership Mode (Managing Anonymous Content)

Every operation above except domain 23 is `NULLIFIER_MODE_CONSUME`: the nullifier is spent. An operation registered as `NULLIFIER_MODE_OWNERSHIP` instead proves the submitter created some anonymous content, by reproducing the content's creation nullifier (its owner tag). The content module stores the creating exec's `ExecNullifier{Domain, Scope, Nullifier}` (handed to it in the context) as the owner claim, implements `ShieldOwnershipResolver` to return that claim plus an owner sequence, and checks `GetOwnershipTag(ctx)` against the stored tag before letting the shield address manage the content. The proof binds the sequence in its message hash and the sequence advances after every op, so a proof can't be replayed. Ownership ops are immediate-only. x/collect is the only user today (anonymous collection management; see [x-collect-spec.md](x-collect-spec.md) section 18.3).

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

When a shielded operation executes, the inner message's `creator` field is set to the **shield module account address** (`authtypes.NewModuleAddress("shield")`). The content module creates the content normally — the creator being the shield module address is what marks it as anonymous. Every anonymous member shares this address, which has consequences a content module must handle:

- **Trust gates:** compare against the level the ZK proof established, read with `shieldtypes.ProvenTrustLevel(ctx)`. If no proven level is in the context, the message did not come through a shield exec and nothing is known about the signer: refuse (or treat it as the lowest level). Never treat the shield address as automatically trusted.
- **Per-address bookkeeping does not apply:** one-vote-per-voter records, own-content checks, and per-address daily rate limits would treat all anonymous members as one account. Skip them for the shield address; x/shield's nullifier (one action per member per target) and per-identity rate limit replace them. This is why upvotes and downvotes on the same target share one nullifier domain.
- **No author identity:** an anonymous action on anonymous content is not "by the author" just because both carry the shield address; exemptions keyed on `creator == author` must exclude it.
- **No per-action SPARK charges:** the shield module account's balance is the communal gas reserve, not the member's money, so content modules charge anonymous actions nothing beyond gas — no storage fees or edit delta fees (x/blog, x/forum), no spam taxes or edit fee (x/forum), no reaction fee (x/blog), no downvote costs (x/forum `downvote_deposit`, x/collect `downvote_cost`), and no collection/item deposits (x/collect). Check for the shield address before every `SendCoinsFromAccountToModule`/burn on the signer. Anonymous volume is bounded instead by x/shield's per-identity exec limit and per-op rate-limit windows.

### 5. Access Control Rules

- **No edit/delete by author** — anonymous blog and forum content is immutable (no author identity to verify). Anonymous collections are the exception: their creator manages them through ownership mode (see Ownership Mode above)
- **Post author moderation** — post/thread authors can hide anonymous replies (same as regular)
- **Operations Committee** — can delete anonymous content for policy violations
- **Reactions** — regular identified members can react to anonymous content normally
- **Lifetime**: x/blog anonymous posts and replies always stay ephemeral (the shield address passes the membership gate, but the TTL is kept) and rely on conviction renewal; they are skipped by the author-keyed ephemeral index and promotion queue

---

## Client Workflow

**Proof generation (client-side, ~2-3 seconds on modern hardware):**

1. **Register ZK public key** (one-time): Call `MsgRegisterZkPublicKey` in x/rep to store the public key on-chain and add to the trust tree.

2. **Fetch trust tree data** from x/rep:
   - Download the whole tree with `sparkdreamd q rep trust-tree` (all pages): root, depth, and non-empty leaves
   - Rebuild the tree locally (missing indices are zero leaves), find the member's leaf `MiMC(zk_public_key, trust_level)`, and compute the Merkle path (path elements + indices) from it
   - Member's current trust level (the one encoded in the leaf)

3. **Compute nullifiers** (the prover does this from `Domain` and the raw `Scope`):
   ```
   nullifier = ComputeScopedNullifier(secretKey, domain, rawScope)  // MiMC(secretKey, MiMC(domain, rawScope))
   rateLimitNullifier = ComputeRateLimitNullifier(secretKey, currentEpoch)
   ```
   `domain` is the operation's registered `nullifier_domain`; `rawScope` is what the chain resolves for it (current shield epoch, the message field, or 0). A proof made with the wrong domain or scope fails verification.

4. **Generate Groth16 proof** with the ShieldCircuit (`prover.ShieldProofInput{Domain, Scope, ...}`, or the browser wasm `prove` with `"domain"` and `"scope"`), bound to `ShieldMessageHash` of the exact inner `Any`

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
     --from <public-submitter>
   ```

**Managing anonymous content (ownership mode):**

1. Read the content's owner claim from its query (for a collection: `anon_owner_domain`, `anon_owner_scope`, `anon_owner_tag`, `anon_owner_sequence`).
2. Check locally that `ComputeScopedNullifier(secretKey, ownerDomain, ownerScope) == ownerTag`. Doing this over public chain data finds the member's own anonymous content, so a wallet-derived secret key restores management on a new device.
3. Build the inner message with `creator` = the shield module address.
4. Prove with the claim's domain and scope (not the management op's own domain) and the owner sequence (wasm `prove` input `"owner_sequence"`), so the message hash is `ShieldOwnedMessageHash(typeURL, value, sequence)`. The returned nullifier must equal the owner tag.
5. Submit `MsgShieldedExec` in immediate mode from the public submitter. If another op on the same content lands first the sequence has advanced; re-read and re-prove.

**Shared public submitter.** Anonymous clients sign `MsgShieldedExec` with one shared account whose secp256k1 private key is deterministic and public on purpose (`x/shield/types/public_submitter.go`: `PublicSubmitterPrivKey()` / `PublicSubmitterAddress()`). Every anonymous action therefore comes from the same outer signer, which reveals nothing about the member; the ZK proof, not the signature, authorizes the exec. Clients send unordered transactions so they never race on the account's sequence. x/shield's genesis creates the account so it has an account number before its first exec. Because anyone can sign with it, the ante handler refuses any transaction it signs that is not a `MsgShieldedExec` (`ErrUnauthorized`). x/shield pays gas, so the submitter needs no balance. Signing with the member's own address still works but links the action to that address.

---

## Security Considerations

### Anonymity Guarantees

- ZK proof reveals *nothing* about the poster except that they are an active member at or above the proven trust level
- The anonymity set is all active members at that trust level — the larger the set, the stronger the anonymity
- Nullifiers are unlinkable across different scopes and across operations: the domain is hashed into the scope element, so even two operations that share a raw scope (or two GLOBAL operations) produce different nullifiers
- Same-scope nullifiers are deterministic (prevents double-posting) but don't reveal identity
- Rate limit nullifiers are scoped per epoch — they identify "the same person" for rate limiting within an epoch but are unlinkable across epochs

### Anonymity Limitations

- **Transaction timing:** The submission timestamp is visible on-chain (the submitter is the shared public submitter, so the address itself carries nothing). In immediate mode, content is visible. Using encrypted batch mode and/or a relay mitigates this.
- **Writing style:** Stylometric analysis of post content could deanonymize frequent anonymous posters. This is outside the protocol's threat model.
- **Small anonymity sets:** If only 3 members are ESTABLISHED+, anonymity is weak. The minimum trust level should be set to a level with sufficient membership.
- **Ownership linkability:** management ops on one piece of anonymous content all carry its owner tag (its creation nullifier), so they are linkable to each other and to the creation, though not to the member's other activity. Rotating the ZK key or losing membership/trust makes the tag unprovable and ends management.
- **Self-conviction:** content modules record the shield address as the author of anonymous content, so x/rep's author exclusion cannot stop an anonymous author from staking conviction on their own content from an identified account.
- **Merkle root freshness:** x/shield accepts the current root or the immediately previous root (one-rebuild-cycle grace period). Roots older than one cycle are rejected.

### Spam Prevention

- One anonymous action per scope per identity (nullifier-enforced)
- Per-identity rate limiting via `RateLimitNullifier` (max operations per epoch); content modules' own per-address rate limits exempt the shared shield address, so this is the per-member bound
- Module-paid gas funded from community pool with daily cap
- Trust level minimum raises the Sybil cost; content-level trust gates compare the proven trust level
- Governance can deregister abused operations

### Proof Soundness

- Groth16 proofs are computationally sound under the knowledge-of-exponent assumption
- Verification key stored on-chain and changeable only by a chain upgrade
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
