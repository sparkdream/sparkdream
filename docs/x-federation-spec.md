# Technical Specification: `x/federation`

## 1. Abstract

The `x/federation` module enables Spark Dream chains to exchange content, verify reputation, and link identities with other Spark Dream chains (via IBC) and with external social protocols (ActivityPub, AT Protocol) via off-chain bridges.

Key principles:
- **Sovereignty first**: Every chain governs itself. Federation is a set of bilateral relationships, never a supergovernment. No external chain can dictate local policy, override local moderation, or force governance decisions. Each chain independently chooses who to federate with, what to share, and what to accept.
- **Bilateral, not multilateral**: Federation between Chain A and Chain B is two independent, unilateral decisions. Chain A sets its own policies toward Chain B, and vice versa. Policies can be asymmetric. Either chain can suspend or remove the other at any time without coordination.
- **Protocol-agnostic core**: The module defines generic primitives (peers, policies, identity links, content attestations). Protocol-specific logic (IBC packets, ActivityPub JSON-LD, AT Protocol records) lives in dedicated layers that plug into the core.
- **No cross-chain tokens**: SPARK and DREAM are strictly per-chain. No IBC transfers, no shared pools, no cross-chain minting. Each chain's token economy is fully independent. Bridge operators stake SPARK only. DREAM is used locally for verifier accountability bonds (not for cross-chain operations, bridge staking, or attestations).
- **Reputation is advisory**: Cross-chain reputation attestations are heavily discounted, time-limited, and at the full discretion of the receiving chain. They provide a signal, not a right.

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/commons` | Council/committee authorization for peer registration, bridge management |
| `x/bank` | Bridge operator staking, slash burning (SPARK only) |
| `x/rep` | Reputation data, trust level checks, DREAM bonding for verifiers, jury system for verification disputes |
| `x/name` | Name resolution for identity linking |
| `x/shield` | Anonymous challenge resolution via `MsgShieldedExec` (ZK proofs, scoped nullifiers, module-paid gas) |
| `ibc-go` | IBC core for Spark Dream ↔ Spark Dream federation channels |

**Depended on by x/split only** (read-only queries for compensation weights). Otherwise a leaf module — content modules (blog, forum, collect) emit standard events; the federation layer consumes them without requiring those modules to know about federation.

---

## 3. Core Concepts

### 3.1. Federation Layers

The module operates across three layers:

```
┌──────────────────────────────────────────────────────────┐
│  Layer 1: On-Chain Primitives (x/federation module)      │
│  Peer registry, policies, identity links, attestations   │
├──────────────────────────────────────────────────────────┤
│  Layer 2: IBC Protocol (Spark Dream ↔ Spark Dream)       │
│  Custom IBC port, reputation queries, content sync       │
│  Trustless — verified by light client proofs             │
├──────────────────────────────────────────────────────────┤
│  Layer 3: Off-Chain Bridges (ActivityPub, AT Protocol)   │
│  Relay daemons that translate chain events ↔ protocols   │
│  Trust-minimized — operators stake and are accountable   │
└──────────────────────────────────────────────────────────┘
```

Layer 1 is the module itself. Layer 2 is the IBC application within the module. Layer 3 is external software that interacts with the module via standard transactions.

### 3.2. Peer Types

Three categories of federation peer:

| Type | Transport | Trust Model | Capabilities |
|------|-----------|-------------|-------------|
| Spark Dream chain | IBC | Trustless (light client) | Full: reputation, content, identity |
| ActivityPub instance | HTTP (bridge) | Trust-minimized (staked bridge) | Content exchange, identity linking |
| AT Protocol PDS | HTTP (bridge) | Trust-minimized (staked bridge) | Content exchange, identity linking |

IBC peers can verify reputation cryptographically. Bridge peers rely on operator honesty, incentivized by staking.

### 3.3. Content Federation Model

**Outbound flow** (publishing local content to peers):
1. Content module (blog, forum, collect) emits a standard creation event
2. An off-chain relayer watches chain events and determines what to federate based on peer policies
3. For IBC peers: the **content creator** (or a relayer acting via x/session delegation) submits `MsgFederateContent` to x/federation, which sends a `ContentPacket` via IBC and records an `OutboundAttestation`. The creator's signature proves authenticity — the receiving chain knows the content was authorized by its author.
4. For bridge peers: the bridge daemon translates the content to the target protocol (ActivityPub/AT Protocol) and submits `MsgAttestOutbound` on-chain as an audit trail

**Why creator-signed?** Since x/federation is a leaf module (it cannot import content module keepers to verify content exists), authenticity comes from the creator's signature on `MsgFederateContent`. This prevents relayers from fabricating content. The relayer's role is to notify creators and facilitate transaction broadcast, not to vouch for content. For automation, creators can delegate `MsgFederateContent` to a relayer via x/session.

**Inbound flow** (receiving content from peers):
1. For IBC peers: `OnRecvPacket` validates and stores as `FederatedContent`
2. For bridge peers: bridge daemon receives content, submits `MsgSubmitFederatedContent`
3. Module validates against peer policy (content type allowed, rate limits, peer active)
4. Content stored with full origin metadata, subject to local moderation

Federated content is stored in x/federation, not in content modules. Content modules remain unaware of federation. Frontends query both native content and federated content and merge for display.

**Content from x/reveal**: Reveal contributions (code proposals, tranches) are NOT eligible for federation. Reveal content involves IP licensing, contributor bonds, and holdback mechanics that are inherently local to the chain. The `outbound_content_types` allowlist in PeerPolicy should never include reveal-related types.

### 3.4. Identity Linking

Users can voluntarily link their local identity to remote identities:
- `alice` on Chain A links to `alice` on Chain B (verified via IBC)
- `alice` links to `@alice@mastodon.social` (verified via bridge attestation)
- `alice` links to `alice.bsky.social` (verified via bridge attestation)

Identity links are self-asserted by the local user and optionally verified:
- **IBC verification**: Two-phase protocol where the remote user must sign a transaction on the remote chain to prove key ownership (see Section 8.3 for details)
- **Bridge verification**: Bridge confirms the remote account posted a verification string
- **Unverified**: User claims the link, marked as unverified

**Constraints:**
- Each `(peer_id, remote_identity)` pair can be linked to at most ONE local address. If another local address attempts to claim the same remote identity on the same peer, the link is rejected with `ErrRemoteIdentityAlreadyClaimed`. This prevents identity collision in the `IdentityLinksByRemote` reverse index.
- Each local address can have at most `max_identity_links_per_user` links across all peers (default: 10). This prevents link explosion attacks.
- Unverified links expire after `unverified_link_ttl` (default: 30 days). EndBlocker prunes expired unverified links automatically. Verified links do not expire (but can be manually unlinked or revoked on peer removal).

### 3.5. Reputation Bridging (Spark Dream ↔ Spark Dream Only)

Cross-chain reputation uses the **attestation model**: Chain B queries Chain A via IBC for a member's trust level and reputation scores. The result is cached locally with heavy discounting.

**Discounting rules** (receiving chain applies):
- Remote TRUSTED → local credit capped at PROVISIONAL
- Remote CORE → local credit capped at ESTABLISHED
- Credit is time-limited (default: 30 days, must be refreshed)
- Each chain sets its own `max_trust_credit` cap per peer
- Attestations are advisory — they may influence onboarding but never bypass local invitation requirements

**No reputation bridging for ActivityPub/AT Protocol peers** — those protocols have no comparable reputation system.

### 3.6. Bridge Operators

Bridge operators are off-chain service providers that translate between the chain and external protocols. They:
- Register on-chain with a SPARK stake (escrowed in the federation module account)
- Submit inbound federated content via `MsgSubmitFederatedContent`
- Submit outbound attestations via `MsgAttestOutbound`
- Are accountable: tracked submissions, rejection rate, slash history
- Can be slashed by Operations Committee for misbehavior (spam, fabricated content, protocol violations)
- Multiple operators can serve the same peer (redundancy, competition)
- Are auto-revoked if slashing drops their stake below `min_bridge_stake`
- Must wait `bridge_revocation_cooldown` (default: 7 days) after revocation before re-registering for the same peer

Bridge operators can use x/session to create a scoped session key for their daemon, avoiding exposure of the operator's main private key.

### 3.7. Bridge Operator Session Keys

Bridge operators SHOULD create scoped session keys for their daemon processes using x/session. Recommended session configuration:

```
MsgCreateSession {
  granter: "<bridge_operator_main_wallet>",
  grantee: "<daemon_ephemeral_key>",
  allowed_msg_types: [
    "/sparkdream.federation.v1.MsgSubmitFederatedContent",
    "/sparkdream.federation.v1.MsgAttestOutbound",
  ],
  spend_limit: "10000000uspark",   // gas budget for daemon operations
  expiration: "now + 30 days",     // rotate periodically
  max_exec_count: 0,               // unlimited (bounded by spend_limit and expiration)
}
```

**Rationale:** The session key limits the daemon to only submitting content and attesting outbound — it cannot register/revoke bridges, update policies, or perform any other operation. If the daemon key is compromised, the attacker can only submit content (which is rate-limited and auditable) and cannot steal the operator's stake. Operators should rotate session keys every 30 days.

### 3.8. Federated Content Moderation

Federated content stored in x/federation can be moderated through two paths:

1. **Direct moderation** via `MsgModerateContent` (Operations Committee): Hide, reject, or restore any federated content item. This is the primary moderation path.

2. **Future sentinel integration** (see Section 17): When implemented, x/forum sentinels will be able to flag federated content via a keeper interface exposed by x/federation. The sentinel workflow would be: sentinel flags content → x/federation marks it HIDDEN → appeals go through the standard x/rep jury system (not x/forum's internal appeal flow, since the content lives in x/federation).

Content lifecycle (TTL, pruning) is always managed by x/federation's EndBlocker, regardless of moderation path.

### 3.9. Federation Verifiers

Federation verifiers are community members who independently verify that bridged content matches its source. The role is one of the DREAM-bonded roles managed by x/rep's generic `BondedRole` primitive (`role_type = ROLE_TYPE_FEDERATION_VERIFIER`). Verifiers bond DREAM, must meet a minimum trust level, are rewarded for accurate verification, and are slashed by jury verdict if proven wrong. See [bonded-role-generalization.md](bonded-role-generalization.md).

**Why a separate role from bridge operators?** Bridge operators have infrastructure at stake (SPARK bond) but also have incentive to inflate their submission volume. Verifiers are independent community members with reputation and DREAM at stake — they have no incentive to rubber-stamp operator submissions, and collusion between operator and verifier is punishable by both SPARK slashing (operator) and DREAM slashing (verifier).

**Why separate primitives?** Bridge operators stake SPARK with a 14-day unbonding period and are slashed by the Operations Committee; verifiers stake DREAM via x/rep's reputation-gated BondedRole primitive. The two use cases have incompatible mechanics, so x/rep's BondedRole is DREAM-only and federation keeps its own `BridgeOperator` proto for the SPARK-staked role.

#### 3.9.1. Verifier Registration

A member bonds DREAM to become a federation verifier via x/rep's generic `MsgBondRole` with `role_type = ROLE_TYPE_FEDERATION_VERIFIER`. CLI: `tx rep bond-role ROLE_TYPE_FEDERATION_VERIFIER <amount>`.

**Requirements (enforced in rep against the federation-seeded `BondedRoleConfig`):**
- Minimum trust level: `min_verifier_trust_level` (default: ESTABLISHED / 2) — must have demonstrated community standing. Federation writes this through to the role's `BondedRoleConfig.MinTrustLevel` field.
- Minimum bond: `min_verifier_bond` (default: 500 DREAM). Federation writes this through to `BondedRoleConfig.MinBond`.
- `verifier_demotion_cooldown` (default 7d) + `verifier_recovery_threshold` (default: 250 DREAM) are written through to `BondedRoleConfig.DemotionCooldown` / `.DemotionThreshold`.

**Bond status model** (shared `BondedRoleStatus` enum; the federation-specific `VerifierBondStatus` enum from the pre-Phase-4 proto is gone):

| Status | Bond Range | Behavior |
|--------|-----------|----------|
| `BONDED_ROLE_STATUS_NORMAL` | ≥ `min_verifier_bond` | Full privileges; rewards paid directly |
| `BONDED_ROLE_STATUS_RECOVERY` | ≥ `verifier_recovery_threshold` and < `min_verifier_bond` | Can still verify, but DREAM rewards auto-bond until restored |
| `BONDED_ROLE_STATUS_DEMOTED` | < `verifier_recovery_threshold` | Loses all privileges; must wait `verifier_demotion_cooldown` before re-bonding |

#### 3.9.2. Verification Flow

```
Bridge operator submits content
         │
         ▼
   PENDING_VERIFICATION ──── verification_window expires ──→ auto-HIDDEN
         │                   (unverified, not shown by default)
         │
    Verifier independently
    fetches from source,
    computes content_hash
         │
         ├─ hash matches ──→ VERIFIED (content visible)
         │
         └─ hash mismatch ──→ DISPUTED
                               │
                           EndBlocker creates
                           x/rep jury initiative
                               │
                      ┌────────┼────────┐
                      ▼        ▼        ▼
                   OPERATOR  VERIFIER  TIMEOUT
                   WRONG     WRONG
                   (content  (content  (content
                   REJECTED, VERIFIED, VERIFIED,
                   operator  verifier  no slash)
                   slashed)  slashed)
```

1. Bridge operator submits `MsgSubmitFederatedContent` — content enters `PENDING_VERIFICATION`
2. Within `verification_window` (default: 24 hours), a verifier calls `MsgVerifyContent` with the `content_hash` they independently computed by fetching the source content
3. **Hash match**: Content status → `VERIFIED`, visible to frontends. A `VerificationRecord` is created linking the verifier to this content (their bond is committed against potential challenge).
4. **Hash mismatch**: Content status → `DISPUTED`. The module initiates the two-phase arbiter/jury resolution (Section 3.9.7) with the **verifier acting as the implicit challenger**. Key differences from explicit challenges (Section 3.9.3):
   - **No challenge fee**: The verifier did their job by checking the content — they shouldn't be penalized for discovering a mismatch. The dispute is initiated automatically, not via `MsgChallengeVerification`.
   - **Verifier's bond is committed** (same as for a match — `verifier_slash_amount` DREAM), so they have skin in the game.
   - **Verdict outcomes**: If the arbiter quorum or jury determines the **operator's hash was wrong** (content doesn't match what the operator claimed): content → REJECTED, operator's `content_rejected` incremented, verifier's committed bond released, verifier's `upheld_verifications` incremented. If the **verifier's hash was wrong** (verifier fetched incorrect content or made an error): verifier slashed `verifier_slash_amount` DREAM (50% to community pool, 50% burned), verifier's `overturned_verifications` incremented, content → VERIFIED with operator's original hash. If **no quorum and no jury consensus**: content → HIDDEN, both parties' bonds/stakes unaffected (ambiguous outcome).
   - **Escalation**: Either the verifier or the bridge operator can escalate within `arbiter_escalation_window` (same as challenge escalation, paying `escalation_fee`).
5. **No verification within window**: Content status → `HIDDEN` (unverified). Stays queryable but excluded from default frontend listings. The bridge operator's `unverified_count` is incremented.

#### 3.9.3. Challenges

Any member meeting `min_verifier_trust_level` can challenge a VERIFIED piece of content via `MsgChallengeVerification` by providing a re-computed `content_hash` and paying a `challenge_fee` (SPARK). This is the accountability backstop — if a verifier rubber-stamped without actually checking, anyone can catch them.

**Challenge flow (two-phase resolution):**
1. Challenger pays `challenge_fee` (default: 250 SPARK), escrowed in federation module
2. Content status → `CHALLENGED`
3. **Phase 1 — Anonymous resolution** (see Section 3.9.7): Anonymous community members independently fetch and hash the content via x/shield. If `arbiter_quorum` matching hashes are reached within `arbiter_resolution_window` (default: 24h), the challenge auto-resolves. Either party can escalate within `arbiter_escalation_window` (default: 48h).
4. **Phase 2 — Human jury** (fallback): If no quorum is reached, or either party escalates, the dispute routes to a standard x/rep jury initiative with `challenge_jury_deadline` (default: 14 days).
5. Verdict outcomes (same for both phases):
   - **CHALLENGE_UPHELD** (verifier was wrong): Verifier slashed `verifier_slash_amount` DREAM — 50% to challenger as bounty, 50% burned. Challenger refunded 100% of fee. Content → REJECTED. Operator's `content_rejected` incremented.
   - **CHALLENGE_REJECTED** (verifier was right): Challenger loses fee (50% to verifier as reward, 50% burned). Content stays VERIFIED.
   - **CHALLENGE_TIMEOUT** (no quorum and no jury consensus): Challenger refunded 50%. Content stays VERIFIED. No slash. 50% of fee burned.

#### 3.9.4. Verifier Compensation

Verifiers receive **dual-token compensation** — SPARK for infrastructure costs and a small DREAM reward that enables organic bond recovery:

**SPARK rewards** (via x/split, proportional to work and accuracy):

Distribution weight per verifier per epoch:
```
weight = verified_count * accuracy_multiplier
```
Where:
- `verified_count` = content items verified this epoch that remain VERIFIED (not overturned by challenge)
- `accuracy_multiplier` = `upheld_verifications / (upheld_verifications + overturned_verifications)` over rolling 30-day window. Must be ≥ `min_verifier_accuracy` (default: 0.8) to receive any compensation.

**DREAM rewards** (minted per epoch via x/rep):

Each eligible verifier receives `verifier_dream_reward` (default: 5 DREAM) per epoch, minted by x/rep. If total eligible rewards exceed `max_verifier_dream_mint_per_epoch`, all verifiers are scaled down pro-rata (same model as forum sentinels). This DREAM reward serves two purposes:
1. **Bond recovery**: Verifiers slashed into RECOVERY can rebuild their bond through continued good work rather than needing to front DREAM from their own balance. At 50 DREAM per slash and 5 DREAM per epoch, recovery takes ~10 epochs — meaningful but not punishing.
2. **Community alignment**: Earning DREAM ties verifiers deeper into the community's internal economy, not just infrastructure compensation.

**Eligibility requirements** (same for both SPARK and DREAM):
- Verifier bond status NORMAL or RECOVERY
- No slashing events within current epoch
- At least `min_epoch_verifications` (default: 3) verified items this epoch
- Accuracy ≥ `min_verifier_accuracy`

**DREAM auto-bonding in RECOVERY:** If verifier is in RECOVERY status, DREAM rewards auto-bond until the minimum bond is restored. SPARK rewards are always paid out directly (even in RECOVERY). Once bond is restored to `≥ min_verifier_bond`, status returns to NORMAL and DREAM rewards are paid out normally.

#### 3.9.5. Challenge Ecosystem

Verification challenges are expected to come from three natural sources:

- **Competing bridge operators**: Operators serving the same peer already run infrastructure that fetches from the same external platform. They have direct economic incentive — every piece of a competitor's content that gets rejected increases their relative share of x/split compensation. They have the capability (monitoring the same feeds) and the motivation (competition for rewards).
- **Established community members**: Any member at `min_verifier_trust_level` (ESTABLISHED+) can challenge content they encounter in the frontend. The `challenge_fee` (default: 250 SPARK) deters frivolous challenges while remaining accessible for members with genuine evidence.
- **Operations Committee**: The committee can conduct periodic spot-checks of verified content as part of their bridge oversight mandate. This provides a baseline of challenges even for single-operator peers where competitive dynamics are absent.

This mirrors the forum sentinel model where post authors are the natural counterparty for appeals. The key difference: forum authors might not know they can appeal, but bridge operators are sophisticated actors who understand the economics. No mandatory challenge rate floor is needed — competitive pressure creates organic accountability.

**Why `max_bridges_per_peer ≥ 2` matters for security:** With a single bridge operator per peer, the challenge ecosystem is weaker — there's no competitor with infrastructure-level visibility. The `max_bridges_per_peer` minimum of 2 (already enforced for verification) creates the competitive dynamics that keep both operators and verifiers honest.

#### 3.9.6. Challenge Evidence Protocol

A key difference from the forum sentinel model: forum jury evidence is fully on-chain (the hidden post is immutable and always available), but federation challenge evidence is **off-chain and mutable**. The external content may change or be deleted between verification and jury deliberation. The challenge protocol is designed around this constraint.

**What the jury sees:**

When a challenge initiative is created, it includes:
1. `content_uri` — URL of the content on the external platform
2. `operator_hash` — SHA-256 hash submitted by the bridge operator
3. `verifier_hash` — SHA-256 hash submitted by the verifier
4. `challenger_hash` — SHA-256 hash submitted by the challenger
5. `evidence` — free-text field from the challenger (screenshots, archive.org links, timestamps, description of what they observed)
6. `remote_content_id` — identifier on the source system
7. `peer_id` — which federation peer the content came from

**How jurors decide:**

Jurors are expected to independently fetch the content from `content_uri` and compute the SHA-256 hash themselves. The judgment is objective — either the hash matches or it doesn't.

Three scenarios:

1. **Content is accessible and hashable.** Juror computes hash, compares to all three submitted hashes. Whichever party's hash matches the live content is correct. This is deterministic — all honest jurors will reach the same conclusion.

2. **Content was modified since verification.** The live hash matches neither party. **Key rule: if the external platform's edit timestamp is after the on-chain `verified_at` timestamp, the verifier was correct at the time of verification and must not be faulted.** The challenge should be rejected — the content changed after verification, not before. Arbiters and jurors check the edit/modified timestamp on the external content (ActivityPub `updated` field, AT Protocol `indexedAt`, etc.) against the `verified_at` block time on the VerificationRecord. Only if the content's edit timestamp predates the verification (or no edit timestamp exists) does a hash mismatch count against the verifier.

3. **Content was deleted or is inaccessible.** If content that was supposedly federated from an external platform no longer exists there, this is evidence in the challenger's favor — it suggests the operator may have fabricated the content or the verifier rubber-stamped without checking. However, legitimate deletion (the author removed their post) is also possible. Jurors weigh the timing: content that disappeared within hours of being challenged is more suspicious than content removed weeks later. The `evidence` field is important here — the challenger should document when they first noticed the content was missing.

**Why this works despite off-chain evidence:**

- The core judgment ("does this hash match?") is objective and independently reproducible by each juror, unlike forum moderation which is subjective
- External content that exists and hasn't changed (the common case) produces a deterministic outcome — all jurors will compute the same hash
- The `challenge_window` (default: 7 days) is short enough that most content won't change during the window
- The challenge fee (250 SPARK) plus tx costs make it uneconomical to challenge content that you know matches, even with a full refund on winning — you risk losing 125 SPARK (50% of fee) if the jury disagrees
- Jurors who can't reach the content can abstain — the jury initiative uses x/rep's standard quorum requirements

**Comparison to forum sentinel evidence model:**

| Aspect | Forum Sentinel | Federation Verifier |
|--------|---------------|-------------------|
| Evidence location | On-chain (immutable post) | Off-chain (mutable URL) |
| Judgment type | Subjective ("was hiding justified?") | Objective ("does hash match?") |
| Evidence availability | Always available | May change or disappear |
| Juror verification | Read the post, apply standards | Fetch URL, compute hash, compare |
| Temporal risk | None | Content may change before verdict |

#### 3.9.7. Challenge Resolution

Federation verification challenges involve an objective, deterministic question — "does the content at this URL hash to this value?" — that doesn't require human judgment. Routing these to a 14-day human jury wastes community time on a question a fetch-and-hash can answer in seconds. The module uses a **two-phase resolution** model: quorum-based auto-resolution first, human jury as fallback.

**Phase 1: Arbiter Quorum** (`arbiter_resolution_window`, default: 24 hours)

Two types of participants can submit arbiter hashes:

**1. Competing bridge operators** (identified, via `MsgSubmitArbiterHash`):

Registered bridge operators who serve the same peer can submit hashes directly. They already run infrastructure that fetches from the same external platform and have the strongest economic incentive — catching a competitor increases their relative x/split share. The submitting operator (whose content is being challenged) is excluded.

```protobuf
message MsgSubmitArbiterHash {
  string creator = 1;              // Bridge operator address (identified path) or shield module address (anonymous path)
  uint64 content_id = 2;
  bytes content_hash = 3;          // SHA-256 hash independently computed from content_uri
}
```

**Constraints for identified submissions:**
- Must be a registered, ACTIVE bridge operator for the same peer as the challenged content
- Cannot be the bridge operator who submitted the challenged content (reject with `ErrSelfArbiter`)
- One submission per operator per content_id

**2. Anonymous community members** (via x/shield `MsgShieldedExec`):

Any ESTABLISHED+ member can submit anonymously:

```
MsgShieldedExec {
  inner_message: MsgSubmitArbiterHash {
    content_id: <challenged content ID>
    content_hash: <SHA-256 hash independently computed from content_uri>
  }
  zk_proof: <proves trust_level >= ESTABLISHED>
  nullifier: <scoped to "federation_arbiter:<content_id>" — one submission per member>
  domain: "FEDERATION_ARBITER"
}
```

The ZK proof (verified by x/shield's PLONK/BN254 verifier) proves the submitter is a member at the required trust level without revealing their identity. The scoped nullifier prevents any member from submitting twice for the same content.

**Quorum and auto-resolution:**

Both identified and anonymous submissions count toward the same quorum. When `arbiter_quorum` (default: 3) matching hashes accumulate:
1. The matching hash is compared against the challenger's hash and the verifier's hash
2. If the quorum hash matches the **challenger's** hash: auto-resolve as CHALLENGE_UPHELD (verifier was wrong)
3. If the quorum hash matches the **verifier's** hash: auto-resolve as CHALLENGE_REJECTED (challenger was wrong)
4. If the quorum hash matches **neither**: auto-resolve as CHALLENGE_UPHELD (both operator and verifier had wrong hashes — content was fabricated or corrupted)
5. Apply the standard slashing, refund, and reward logic (Section 6.25 verdict definitions)
6. Start `arbiter_escalation_window` (default: 48 hours) — either party can escalate to human jury during this period

**Escalation to Phase 2:**

Phase 2 (human jury) activates if:
- No quorum reached within `arbiter_resolution_window` (content inaccessible, insufficient participation)
- Either party calls `MsgEscalateChallenge` within `arbiter_escalation_window` after auto-resolution (pays `escalation_fee`, default: 100 SPARK)
- All submitted hashes are different (no quorum — genuine ambiguity)

If escalated, the human jury sees all evidence including the arbiter hashes as additional signal. The auto-resolution verdict is reversed and replaced by the jury verdict.

**Phase 2: Human Jury** (`challenge_jury_deadline`, default: 14 days)

Standard x/rep jury initiative. Used only when quorum resolution fails or is escalated. The jury sees the full evidence package from Section 3.9.6 plus any arbiter hashes submitted during Phase 1.

**Arbiter incentives:**

Arbiters submit with **zero fee and zero reward**. This is deliberate:

- **No fee needed**: The scoped nullifier (one submission per member per content_id) prevents spam. A random hash can't match the quorum of honest arbiters who all compute the same deterministic SHA-256 from the same URL.
- **No reward needed**: Competing bridge operators are the primary arbiter pool and have direct economic incentive — every piece of a competitor's content that gets rejected increases their x/split share. Community members participate because they use federated content and want it to be authentic. This is crowd-verification, not paid labor.
- **No SPARK movement**: No SPARK fees or rewards flow to/from arbiters, which preserves anonymity for the x/shield path and avoids complexity. The challenger (named) is the one with skin in the game via the challenge fee.

Non-matching arbiters (submitted a hash that didn't match quorum) receive nothing and lose nothing. This encourages honest submission even when uncertain.

**Why this resists gaming:**

- **Operator collusion:** The submitting operator is excluded from arbitrating their own content. Even if they submit anonymously as a member, they're at most 1 of `arbiter_quorum` needed. For accessible content, honest hashes are deterministic — one dishonest hash can't swing the result.
- **Verifier collusion:** Same logic. The verifier is one participant. Even if operator and verifier collude and both submit, they're 2 of 3 needed — one honest arbiter (likely a competing bridge operator) breaks the collusion.
- **Sybil (anonymous path):** ZK proofs are tied to unique member identities via the trust tree. Creating multiple members requires multiple invitations at ESTABLISHED+ trust level — x/rep's invitation and trust system is the Sybil barrier.
- **Sybil (identified path):** Bridge operators are registered with SPARK stake. Creating multiple fake operators requires multiple `min_bridge_stake` deposits and Operations Committee approval.
- **Mixed quorum strength:** A quorum containing both identified operators and anonymous members is harder to corrupt than either type alone — the attacker would need to compromise entities in both categories simultaneously.
- **No penalty for abstaining:** Members and operators who can't fetch the content simply don't submit. Natural self-selection ensures only participants who can actually verify participate.

#### 3.9.8. Verifier Bond Commitment

When a verifier confirms content, their bond is partially committed (like forum sentinel hide records):
- `committed_amount` = `verifier_slash_amount` per verification (default: 50 DREAM)
- `total_committed_bond` tracks sum of all pending commitments (within challenge window)
- Verifier cannot unbond while `total_committed_bond > 0`
- After `challenge_window` (default: 7 days) expires with no challenge, the commitment is released

**Invariant:** `total_committed_bond ≤ current_bond` — verifier cannot verify more content than their bond can cover.

#### 3.9.9. Bridge Operator Compensation

Bridge operators are compensated separately via x/split for infrastructure work. Only **verified** content counts toward compensation:

**Distribution weight per operator per epoch:**
```
weight = verified_submissions
```
Where `verified_submissions` = content items submitted this epoch that reached VERIFIED status.

**Eligibility:**
- Bridge status ACTIVE
- No slashing events within current epoch
- At least 1 verified submission in the epoch
- `unverified_rate` < 50% (content that expired unverified / total submitted, rolling 30-day window)

This eliminates volume gaming: operators can submit as much as they want, but compensation only flows for content independently verified by a community member.

#### 3.9.10. x/split Allocations

The "Federation Operations" x/split allocation (suggested: 5% of Community Pool flow) is split between operators and verifiers:

- **Bridge operators**: 60% of Federation Operations allocation (infrastructure costs are higher)
- **Verifiers**: 40% of Federation Operations allocation

If no eligible operators/verifiers exist in an epoch, their share rolls back to the Community Pool. The split ratio is a governance parameter (`operator_reward_share`, default: 0.6).

---

## 4. State Objects

### 4.1. Peer

```protobuf
message Peer {
  string id = 1;                      // Unique identifier (e.g., "sparkdream-2", "mastodon.social")
  string display_name = 2;            // Human-readable name
  PeerType type = 3;
  PeerStatus status = 4;
  string ibc_channel_id = 5;          // IBC channel (Spark Dream peers only)
  int64 registered_at = 6;            // Block time when registered
  int64 last_activity = 7;            // Last inbound or outbound activity
  string registered_by = 8;           // Council member who proposed registration
  string metadata = 9;                // Optional JSON metadata (chain-id, version, etc.)
  int64 removed_at = 10;              // Block time when removed (0 if not removed)
}

enum PeerType {
  PEER_TYPE_UNSPECIFIED = 0;
  PEER_TYPE_SPARK_DREAM = 1;
  PEER_TYPE_ACTIVITYPUB = 2;
  PEER_TYPE_ATPROTO = 3;
}

enum PeerStatus {
  PEER_STATUS_PENDING = 0;            // Registered but not yet active (awaiting IBC handshake or bridge)
  PEER_STATUS_ACTIVE = 1;
  PEER_STATUS_SUSPENDED = 2;          // Temporarily disconnected
  PEER_STATUS_REMOVED = 3;
}
```

### 4.2. PeerPolicy

Per-peer federation policies. Stored separately from `Peer` so policies can be updated by Operations Committee without a full council proposal.

```protobuf
message PeerPolicy {
  string peer_id = 1;

  // Content federation
  repeated string outbound_content_types = 2;     // e.g., ["blog_post", "forum_thread"]
  repeated string inbound_content_types = 3;
  uint32 min_outbound_trust_level = 4;             // Min trust level to have content federated out
  uint64 inbound_rate_limit_per_epoch = 5;         // Max inbound items per epoch
  uint64 outbound_rate_limit_per_epoch = 11;       // Max outbound items per epoch to this peer

  // Reputation (Spark Dream peers only)
  bool allow_reputation_queries = 6;               // Respond to inbound IBC reputation queries
  bool accept_reputation_attestations = 7;         // Accept reputation attestations from this peer
  uint32 max_trust_credit = 8;                     // Max local trust credit from this peer's attestations

  // Moderation
  bool require_review = 9;                         // If true, inbound content starts hidden until reviewed
  repeated string blocked_identities = 10;         // Blocked remote identities (addresses, actor URIs, DIDs)
}
```

### 4.3. BridgeOperator

```protobuf
message BridgeOperator {
  string address = 1;                              // Operator's bech32 address
  string peer_id = 2;                              // Which peer this bridge serves
  string protocol = 3;                             // "activitypub" or "atproto"
  string endpoint = 4;                             // Bridge endpoint URL (for monitoring/health checks)
  cosmos.base.v1beta1.Coin stake = 5;              // SPARK staked (current balance after any slashing)
  int64 registered_at = 6;
  BridgeStatus status = 7;
  uint64 content_submitted = 8;                    // Total inbound items submitted
  uint64 content_verified = 14;                    // Items that reached VERIFIED status
  uint64 content_unverified = 15;                  // Items that expired without verification
  uint64 content_rejected = 9;                     // Items rejected by policy
  uint64 slash_count = 10;
  int64 revoked_at = 11;                           // Block time when revoked (0 if never revoked)
  int64 last_submission_at = 12;                   // Block time of last content submission (for inactivity checks)
  int64 unbonding_end_time = 13;                   // Block time when stake can be released (0 if not unbonding)
}

enum BridgeStatus {
  BRIDGE_STATUS_UNSPECIFIED = 0;
  BRIDGE_STATUS_ACTIVE = 1;
  BRIDGE_STATUS_SUSPENDED = 2;
  BRIDGE_STATUS_UNBONDING = 3;                     // Revoked, stake locked during unbonding period
  BRIDGE_STATUS_REVOKED = 4;                       // Fully revoked, stake returned
}
```

### 4.4. Verifier state (split across x/rep and x/federation)

> **Phase 1–4 bonded-role generalization:** the former standalone `FederationVerifier` proto is gone. Generic bond/status/activity state now lives in x/rep as `BondedRole(ROLE_TYPE_FEDERATION_VERIFIER, addr)`. Federation owns only per-module counters in `VerifierActivity`.

**Generic bond state (x/rep):**

```protobuf
// See docs/x-rep-spec.md and proto/sparkdream/rep/v1/bonded_role.proto.
message BondedRole {
  string           address                       = 1;
  RoleType         role_type                     = 2; // ROLE_TYPE_FEDERATION_VERIFIER
  BondedRoleStatus bond_status                   = 3;
  string           current_bond                  = 4;
  string           total_committed_bond          = 5;
  int64            registered_at                 = 6;
  int64            last_active_epoch             = 7;
  uint64           consecutive_inactive_epochs   = 8;
  int64            demotion_cooldown_until       = 9;
  string           cumulative_rewards            = 10;
  int64            last_reward_epoch             = 11;
}
```

**Federation-specific counters:**

```protobuf
// proto/sparkdream/federation/v1/verifier_activity.proto
message VerifierActivity {
  string address = 1;                              // Verifier's bech32 address

  // Lifetime metrics
  uint64 total_verifications = 2;                  // Total content items verified
  uint64 upheld_verifications = 3;                 // Challenges rejected (verifier was right)
  uint64 overturned_verifications = 4;             // Challenges upheld (verifier was wrong)
  uint64 unchallenged_verifications = 5;           // Challenge window expired (not counted in accuracy)

  // Epoch metrics (reset each epoch)
  uint64 epoch_verifications = 6;
  uint64 epoch_challenges_resolved = 7;

  // Cooldown / streak tracking
  uint64 consecutive_overturns = 8;                // For escalating cooldown / demotion trigger
  uint64 consecutive_upheld = 9;                   // For overturn counter reset
  int64  overturn_cooldown_until = 10;             // Cannot verify during cooldown
  uint64 slash_count = 11;                         // Total times slashed
}
```

**Bond status:** the shared `BondedRoleStatus` enum (`BONDED_ROLE_STATUS_NORMAL` / `_RECOVERY` / `_DEMOTED`) from x/rep replaces the pre-Phase-4 `VerifierBondStatus` enum.

### 4.5. VerificationRecord

Created when a verifier confirms content. Tracks the bond commitment and enables challenges.

```protobuf
message VerificationRecord {
  uint64 content_id = 1;                           // FederatedContent being verified
  string verifier = 2;                             // Verifier address
  bytes verifier_hash = 3;                         // Hash independently computed by verifier
  int64 verified_at = 4;                           // Block time of verification
  int64 challenge_window_ends = 5;                 // verified_at + challenge_window
  string committed_amount = 6;                     // DREAM committed (= verifier_slash_amount)
  string verifier_bond_snapshot = 7;               // Bond at verification time [(gogoproto.customtype) = "cosmossdk.io/math.Int"]
  VerificationOutcome outcome = 8;
  uint32 prior_rejected_challenges = 9;            // Count of prior challenges rejected on this content (for fee escalation)
  int64 last_challenge_resolved_at = 10;           // Block time of last challenge resolution (for cooldown)
}

enum VerificationOutcome {
  VERIFICATION_OUTCOME_UNSPECIFIED = 0;
  VERIFICATION_OUTCOME_PENDING = 1;                // Within challenge window
  VERIFICATION_OUTCOME_CONFIRMED = 2;              // Challenge window expired, no challenge
  VERIFICATION_OUTCOME_CHALLENGED = 3;             // Challenge filed, awaiting jury
  VERIFICATION_OUTCOME_UPHELD = 4;                 // Challenge rejected (verifier was right)
  VERIFICATION_OUTCOME_OVERTURNED = 5;             // Challenge upheld (verifier was wrong)
}
```

### 4.6. ArbiterHashSubmission

Hash submissions for quorum-based challenge resolution. Supports two submission paths:
- **Identified**: Bridge operators submit `MsgSubmitArbiterHash` directly. `operator` field set to their address.
- **Anonymous**: Members submit via x/shield's `MsgShieldedExec`. `operator` field is empty, `nullifier` is set.

```protobuf
message ArbiterHashSubmission {
  uint64 content_id = 1;                           // Challenged content
  bytes content_hash = 2;                          // SHA-256 hash computed by arbiter
  int64 submitted_at = 3;                          // Block time
  string operator = 4;                             // Bridge operator address (empty for anonymous submissions)
  bytes nullifier = 5;                             // Scoped nullifier (empty for identified submissions)
}
```

**Storage:** Submissions are stored per content_id. Both identified and anonymous submissions count toward the same `arbiter_quorum`. When quorum matching hashes accumulate, auto-resolution triggers. Submissions are deleted after the challenge is fully resolved (including escalation window).

### 4.7. FederatedContent

Inbound content from any federation peer.

```protobuf
message FederatedContent {
  uint64 id = 1;                                   // Auto-incrementing local ID
  string peer_id = 2;                              // Source peer
  string remote_content_id = 3;                    // ID on the source system
  string content_type = 4;                         // "blog_post", "forum_thread", "note", "article"
  string creator_identity = 5;                     // Remote identity (address, actor URI, DID)
  string creator_name = 6;                         // Human-readable name (if resolved)
  string title = 7;
  string body = 8;                                 // Content body (truncated to max_content_body_size)
  string content_uri = 9;                          // Full content URL (truncated to max_content_uri_size)
  bytes protocol_metadata = 10;                    // Protocol-specific metadata (truncated to max_protocol_metadata_size)
  int64 remote_created_at = 11;                    // When created on source
  int64 received_at = 12;                          // When received locally
  string submitted_by = 13;                        // Bridge operator address (empty for IBC)
  FederatedContentStatus status = 14;
  int64 expires_at = 15;                           // received_at + content_ttl (for expiration queue)
  bytes content_hash = 16;                         // SHA-256 hash of (title + body) for integrity verification and deduplication
}

enum FederatedContentStatus {
  FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION = 0; // Awaiting verifier confirmation (bridge content only)
  FEDERATED_CONTENT_STATUS_VERIFIED = 1;           // Independently verified by a federation verifier
  FEDERATED_CONTENT_STATUS_ACTIVE = 2;             // Active without verification requirement (IBC content)
  FEDERATED_CONTENT_STATUS_HIDDEN = 3;             // Locally moderated or unverified (hidden from default queries)
  FEDERATED_CONTENT_STATUS_DISPUTED = 4;           // Hash mismatch between operator and verifier, awaiting jury
  FEDERATED_CONTENT_STATUS_CHALLENGED = 5;         // Verified content challenged, awaiting jury
  FEDERATED_CONTENT_STATUS_REJECTED = 6;           // Rejected by policy, moderation, or jury verdict
}
```

**Note:** IBC content from Spark Dream peers enters as ACTIVE (verified by light client proof — no verifier needed). Bridge content from ActivityPub/AT Protocol peers enters as PENDING_VERIFICATION and must be independently verified.

### 4.8. IdentityLink

```protobuf
message IdentityLink {
  string local_address = 1;
  string peer_id = 2;
  string remote_identity = 3;                      // Address, actor URI, or DID
  IdentityLinkStatus status = 4;
  int64 linked_at = 5;
  int64 verified_at = 6;                           // 0 if unverified
}

enum IdentityLinkStatus {
  IDENTITY_LINK_STATUS_UNVERIFIED = 0;
  IDENTITY_LINK_STATUS_VERIFIED = 1;
  IDENTITY_LINK_STATUS_REVOKED = 2;
}
```

### 4.9. PendingIdentityChallenge

Stored on the **receiving chain** during IBC identity verification. The remote user must sign a `MsgConfirmIdentityLink` transaction to prove key ownership before this challenge expires.

```protobuf
message PendingIdentityChallenge {
  string claimed_address = 1;          // Address on this chain being claimed
  string claimant_chain_peer_id = 2;   // Peer ID of the chain making the claim
  string claimant_address = 3;         // Address on the claiming chain
  bytes challenge = 4;                 // Random 32-byte challenge
  int64 received_at = 5;              // Block time when received
  int64 expires_at = 6;               // received_at + challenge_ttl (default: 7 days)
}
```

**Primary key:** `(claimed_address, claimant_chain_peer_id)` — one pending challenge per (address, peer) pair.

### 4.10. ReputationAttestation

Cached cross-chain reputation data (Spark Dream peers only).

```protobuf
message ReputationAttestation {
  string local_address = 1;                        // Local address that holds this attestation
  string peer_id = 2;                              // Source peer chain
  string remote_address = 3;                       // Address on source chain
  uint32 remote_trust_level = 4;                   // Trust level on source chain
  uint32 local_trust_credit = 5;                   // Discounted trust credit (set by receiving chain)
  repeated TagReputation remote_reputations = 6;   // Per-tag reputation scores
  int64 attested_at = 7;
  int64 expires_at = 8;
}

message TagReputation {
  string tag = 1;
  string score = 2;                                // cosmos.Int
}
```

### 4.11. OutboundAttestation

Audit trail for content published to federation peers. OutboundAttestations serve two purposes:

1. **Accountability**: Track which bridge operators published what content to which peers. If a peer complains about receiving spam or fabricated content, the Operations Committee can query outbound attestations to identify the responsible bridge operator and slash them.

2. **Provenance**: For IBC peers, outbound attestations are created automatically when a `ContentPacket` is sent. For bridge peers, bridge operators submit `MsgAttestOutbound` after publishing content to the external protocol. This creates an on-chain record linking local content IDs to outbound federation, enabling audits of bridge operator behavior.

```protobuf
message OutboundAttestation {
  uint64 id = 1;
  string peer_id = 2;
  string content_type = 3;
  string local_content_id = 4;                     // ID in the source content module
  string creator = 5;                              // Local creator address
  string submitted_by = 6;                         // Bridge operator (empty for IBC)
  int64 published_at = 7;
}
```

### 4.12. Params

```protobuf
message Params {
  // Bridge operator requirements
  cosmos.base.v1beta1.Coin min_bridge_stake = 1;
  uint64 max_bridges_per_peer = 2;
  google.protobuf.Duration bridge_revocation_cooldown = 3;  // Min time before re-registration after revocation
  google.protobuf.Duration bridge_unbonding_period = 4;     // Time stake remains locked after revocation (slash window)

  // Content types
  repeated string known_content_types = 5;         // Registry of valid content type strings (governance-managed)

  // Content federation
  uint64 max_inbound_per_block = 6;                // Global rate limit across all peers
  uint64 max_outbound_per_block = 22;              // Global outbound rate limit across all peers
  uint64 max_content_body_size = 7;                // Max bytes for FederatedContent.body
  uint64 max_content_uri_size = 8;                 // Max bytes for FederatedContent.content_uri
  uint64 max_protocol_metadata_size = 9;           // Max bytes for FederatedContent.protocol_metadata
  google.protobuf.Duration content_ttl = 10;       // How long to retain federated content

  // Reputation
  google.protobuf.Duration attestation_ttl = 11;   // How long reputation attestations are valid
  uint32 global_max_trust_credit = 12;             // Absolute cap on trust credit from any peer
  string trust_discount_rate = 13;                 // Discount applied (e.g., "0.5" = 50% reduction)
                                                   // [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]

  // Identity
  uint32 max_identity_links_per_user = 14;         // Max identity links per local address across all peers
  google.protobuf.Duration unverified_link_ttl = 15; // How long unverified identity links survive before pruning
  google.protobuf.Duration challenge_ttl = 16;     // How long pending identity challenges survive (default: 7 days)

  // Bridge monitoring
  uint64 bridge_inactivity_threshold = 17;         // Epochs without submissions before warning event

  // IBC
  string ibc_port = 18;                            // IBC port ID (default: "federation")
  string ibc_channel_version = 19;                 // Channel version string
  google.protobuf.Duration ibc_packet_timeout = 20; // Timeout for outbound IBC packets

  // Rate limiting
  google.protobuf.Duration rate_limit_window = 23;   // Sliding window duration for per-peer rate limits (default: 24h)

  // Verification (sentinel-style)
  uint32 min_verifier_trust_level = 24;              // Min trust level to become verifier (default: ESTABLISHED = 2)
  string min_verifier_bond = 25;                     // Min DREAM bond [(gogoproto.customtype) = "cosmossdk.io/math.Int"]
  string verifier_recovery_threshold = 26;           // Bond below this = DEMOTED [(gogoproto.customtype) = "cosmossdk.io/math.Int"]
  string verifier_slash_amount = 27;                 // DREAM slashed per overturned verification [(gogoproto.customtype) = "cosmossdk.io/math.Int"]
  google.protobuf.Duration verification_window = 28; // Time for verifier to check content after submission
  google.protobuf.Duration challenge_window = 29;    // Time to challenge a VERIFIED item
  cosmos.base.v1beta1.Coin challenge_fee = 30;       // SPARK fee to file a challenge
  google.protobuf.Duration challenge_jury_deadline = 31; // Max time for jury to render verdict
  google.protobuf.Duration verifier_demotion_cooldown = 32; // Cooldown before re-bonding after demotion
  google.protobuf.Duration verifier_overturn_base_cooldown = 33; // Base cooldown after overturn (escalates 2x)
  uint32 upheld_to_reset_overturns = 34;             // Consecutive upheld verifications to reset overturn counter
  uint32 min_epoch_verifications = 35;               // Min verifications per epoch for reward eligibility
  string min_verifier_accuracy = 36;                 // Min accuracy for reward eligibility [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]
  string operator_reward_share = 37;                 // Fraction of Federation Operations allocation for operators [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]
  string verifier_dream_reward = 38;                 // DREAM minted per eligible verifier per epoch [(gogoproto.customtype) = "cosmossdk.io/math.Int"]
  string max_verifier_dream_mint_per_epoch = 39;     // Cap on total DREAM minted for verifiers per epoch [(gogoproto.customtype) = "cosmossdk.io/math.Int"]

  // Anonymous challenge resolution
  uint32 arbiter_quorum = 40;                        // Matching hashes needed for auto-resolution
  google.protobuf.Duration arbiter_resolution_window = 41; // Time for anonymous arbiters to submit hashes
  google.protobuf.Duration arbiter_escalation_window = 42; // Time to escalate auto-resolution to jury
  cosmos.base.v1beta1.Coin escalation_fee = 43;      // SPARK fee to escalate auto-resolution to jury
  google.protobuf.Duration challenge_cooldown = 44;  // Min time between challenges on the same content after a rejected challenge

  // Pruning
  uint64 max_prune_per_block = 21;                 // Max items pruned per EndBlocker invocation
}
```

### 4.13. FederationOperationalParams

Subset of `Params` updateable by Operations Committee without governance proposal.

```protobuf
message FederationOperationalParams {
  uint64 max_inbound_per_block = 1;
  uint64 max_outbound_per_block = 11;
  uint64 max_content_body_size = 2;
  uint64 max_content_uri_size = 3;
  uint64 max_protocol_metadata_size = 4;
  google.protobuf.Duration content_ttl = 5;
  google.protobuf.Duration attestation_ttl = 6;
  uint32 global_max_trust_credit = 7;
  string trust_discount_rate = 8;
  uint64 bridge_inactivity_threshold = 9;
  uint64 max_prune_per_block = 10;
}
```

Governance-only fields: `min_bridge_stake`, `max_bridges_per_peer`, `bridge_revocation_cooldown`, `bridge_unbonding_period`, `known_content_types`, `max_identity_links_per_user`, `unverified_link_ttl`, `challenge_ttl`, `rate_limit_window`, `min_verifier_trust_level`, `min_verifier_bond`, `verifier_recovery_threshold`, `verifier_slash_amount`, `verification_window`, `challenge_window`, `challenge_fee`, `challenge_jury_deadline`, `verifier_demotion_cooldown`, `verifier_overturn_base_cooldown`, `upheld_to_reset_overturns`, `operator_reward_share`, `verifier_dream_reward`, `max_verifier_dream_mint_per_epoch`, `arbiter_quorum`, `arbiter_resolution_window`, `arbiter_escalation_window`, `escalation_fee`, `challenge_cooldown`, `ibc_port`, `ibc_channel_version`, `ibc_packet_timeout`.

**Validation ranges** (enforced on both `MsgUpdateParams` and `MsgUpdateOperationalParams`):

| Field | Min | Max | Rationale |
|-------|-----|-----|-----------|
| `max_inbound_per_block` | 1 | 10000 | Prevent disabling rate limits or absurd values |
| `max_outbound_per_block` | 1 | 10000 | Prevent outbound flooding of IBC channels |
| `max_content_body_size` | 256 | 65536 | Meaningful preview without state bloat |
| `max_content_uri_size` | 64 | 2048 | Standard URL length bounds |
| `max_protocol_metadata_size` | 0 | 8192 | Allow disabling (0) up to reasonable JSON |
| `bridge_unbonding_period` | 7 days | 90 days | Must be long enough for investigation |
| `challenge_ttl` | 1 day | 30 days | Window for remote user to confirm identity |
| `content_ttl` | 1 day | 365 days | Prevent indefinite retention or instant pruning |
| `attestation_ttl` | 1 day | 365 days | Same rationale |
| `global_max_trust_credit` | 0 | 4 | Cannot exceed max trust level (CORE = 4) |
| `trust_discount_rate` | 0.0 | 1.0 | 0 = reject all, 1 = no discount |
| `bridge_inactivity_threshold` | 10 | 10000 | Reasonable monitoring window |
| `max_prune_per_block` | 10 | 1000 | Bound EndBlocker gas without stalling cleanup |
| `rate_limit_window` | 1 hour | 7 days | Sliding window size for per-peer rate limits |
| `min_verifier_trust_level` | 1 (PROVISIONAL) | 4 (CORE) | Must have some community standing |
| `min_verifier_bond` | 100 DREAM | 10000 DREAM | Meaningful accountability without being prohibitive |
| `verifier_recovery_threshold` | 50 DREAM | `min_verifier_bond` | Must be less than min bond |
| `verifier_slash_amount` | 10 DREAM | 1000 DREAM | Per-overturn penalty |
| `verification_window` | 1 hour | 7 days | Time for verifier to check submitted content |
| `challenge_window` | 1 day | 30 days | Time to challenge verified content |
| `challenge_fee` | 50 SPARK | 5000 SPARK | Must deter frivolous challenges |
| `challenge_jury_deadline` | 3 days | 30 days | Jury must have reasonable time |
| `verifier_demotion_cooldown` | 1 day | 30 days | Prevents accuracy-reset attacks |
| `verifier_overturn_base_cooldown` | 1 hour | 7 days | Base cooldown, escalates 2x per consecutive overturn |
| `min_verifier_accuracy` | 0.5 | 0.95 | Cannot be impossible to achieve |
| `operator_reward_share` | 0.1 | 0.9 | Neither operators nor verifiers should get zero |
| `verifier_dream_reward` | 1 DREAM | 50 DREAM | Must enable recovery without being excessive |
| `max_verifier_dream_mint_per_epoch` | 10 DREAM | 1000 DREAM | Cap total DREAM inflation from verification |
| `arbiter_quorum` | 2 | 10 | Min 2 for meaningful consensus; higher = harder to game but slower |
| `arbiter_resolution_window` | 1 hour | 7 days | Time for anonymous hash submissions |
| `arbiter_escalation_window` | 1 hour | 7 days | Time to contest auto-resolution |
| `escalation_fee` | 25 SPARK | 1000 SPARK | Deter frivolous escalation without blocking legitimate disputes |
| `challenge_cooldown` | 1 hour | 30 days | Prevents censorship-by-challenge (repeated challenges keeping content hidden) |

### 4.14. GenesisState

```protobuf
message GenesisState {
  Params params = 1 [(gogoproto.nullable) = false];
  repeated Peer peers = 2 [(gogoproto.nullable) = false];
  repeated PeerPolicy peer_policies = 3 [(gogoproto.nullable) = false];
  repeated BridgeOperator bridge_operators = 4 [(gogoproto.nullable) = false];
  repeated FederatedContent federated_content = 5 [(gogoproto.nullable) = false];
  repeated IdentityLink identity_links = 6 [(gogoproto.nullable) = false];
  repeated ReputationAttestation reputation_attestations = 7 [(gogoproto.nullable) = false];
  repeated OutboundAttestation outbound_attestations = 8 [(gogoproto.nullable) = false];
  // Field 11 previously held FederationVerifier records; replaced by x/rep
  // BondedRole(ROLE_TYPE_FEDERATION_VERIFIER). Per-module counters live in
  // verifier_activities.
  reserved 11;
  reserved "verifiers";
  repeated VerifierActivity verifier_activities = 14 [(gogoproto.nullable) = false];
  repeated VerificationRecord verification_records = 12 [(gogoproto.nullable) = false];
  uint64 next_content_id = 9;
  uint64 next_outbound_attestation_id = 10;
}
```

**Default genesis**: Empty peer list, no bridges, no content. Only `params` is populated with defaults from Section 13.

**Genesis validation**:
- All `peer_id` references in policies, bridges, content, links, and attestations must reference an existing peer in the `peers` list
- `next_content_id` must be greater than the highest `id` in `federated_content`
- `next_outbound_attestation_id` must be greater than the highest `id` in `outbound_attestations`
- All bridge operators must have `stake >= min_bridge_stake`
- No duplicate `(peer_id, remote_identity)` pairs in `identity_links`
- All verifiers must have `bond_status` consistent with their `current_bond` relative to `min_verifier_bond` and `verifier_recovery_threshold`
- For each verifier, `total_committed_bond` must equal the sum of `committed_amount` across all `verification_records` where `verifier == address` and `outcome` is PENDING or CHALLENGED. This ensures committed bond accounting is consistent after genesis import.
- `total_committed_bond ≤ current_bond` for all verifiers
- All verification records must reference existing content in `federated_content`

**Excluded from genesis** (runtime-ephemeral state, managed by EndBlocker):
- `PendingIdentityChallenge`: Short-lived IBC challenges with `challenge_ttl` (default 7 days). Pruned by EndBlocker Phase 4. On genesis restart, any pending challenges expire naturally — the remote user can re-initiate identity linking.
- `ArbiterHashSubmission`: Temporary challenge resolution state bound to `arbiter_resolution_window` (default 24 hours). Pruned by EndBlocker Phases 8-9. On genesis restart, any active challenges escalate to human jury automatically.

---

## 5. Storage Schema

| Collection | Key | Value | Purpose |
|------------|-----|-------|---------|
| `Peers` | `peer_id` | `Peer` | Peer registry |
| `PeerPolicies` | `peer_id` | `PeerPolicy` | Per-peer policies |
| `BridgeOperators` | `(address, peer_id)` | `BridgeOperator` | Bridge operator registry |
| `BridgesByPeer` | `(peer_id, address)` | — | Index: bridges serving a peer |
| `FederatedContent` | `id` (auto-increment) | `FederatedContent` | Inbound content |
| `ContentByPeer` | `(peer_id, id)` | — | Index: content from a peer |
| `ContentByType` | `(content_type, id)` | — | Index: content by type |
| `ContentByCreator` | `(creator_identity, id)` | — | Index: content by remote creator (for moderation/search) |
| `ContentByHash` | `content_hash` | `id` | Index: deduplication lookup by content hash |
| `ContentExpirationQueue` | `(expires_at, id)` | — | Sorted index for efficient TTL pruning |
| `PendingIdentityChallenges` | `(claimed_address, peer_id)` | `PendingIdentityChallenge` | IBC identity verification challenges |
| `IdentityLinks` | `(local_address, peer_id)` | `IdentityLink` | Identity mappings |
| `IdentityLinksByRemote` | `(peer_id, remote_identity)` | `local_address` | Reverse lookup (unique constraint) |
| `IdentityLinkCount` | `local_address` | `uint32` | Per-user link count for cap enforcement |
| `UnverifiedLinkExpirationQueue` | `(expires_at, local_address, peer_id)` | — | Sorted index for unverified link pruning |
| `ReputationAttestations` | `(local_address, peer_id)` | `ReputationAttestation` | Cached reputation |
| `AttestationExpirationQueue` | `(expires_at, local_address, peer_id)` | — | Sorted index for attestation pruning |
| `OutboundAttestations` | `id` (auto-increment) | `OutboundAttestation` | Outbound audit trail |
| `VerifierActivity` | `address` | `VerifierActivity` | Federation-specific per-verifier counters. Generic bond state lives in x/rep as `BondedRoles[(ROLE_TYPE_FEDERATION_VERIFIER, address)]`. |
| `VerificationRecords` | `content_id` | `VerificationRecord` | Verification records (one per content item) |
| `VerificationWindowQueue` | `(expires_at, content_id)` | — | Sorted index for verification window expiry |
| `ChallengeWindowQueue` | `(challenge_window_ends, content_id)` | — | Sorted index for challenge window expiry |
| `ArbiterHashSubmissions` | `(content_id, submitter_key)` | `ArbiterHashSubmission` | Arbiter hash submissions (submitter_key = operator address for identified, nullifier for anonymous) |
| `ArbiterHashCounts` | `(content_id, content_hash)` | `uint32` (count) | Count of matching hashes per content_id (for quorum detection) |
| `ArbiterResolutionQueue` | `(arbiter_resolution_deadline, content_id)` | — | Sorted index for arbiter window expiry |
| `ArbiterEscalationQueue` | `(arbiter_escalation_deadline, content_id)` | — | Sorted index for escalation window expiry |
| `BridgeUnbondingQueue` | `(unbonding_end_time, address, peer_id)` | — | Sorted index for bridge stake release |
| `PeerRemovalQueue` | `peer_id` | `PeerRemovalState` | Peers pending data cleanup (with cursor) |
| `RateLimitCounters` | `(peer_id, window_start)` | `uint64` (count) | Sliding window inbound rate limit tracking |
| `OutboundRateLimitCounters` | `(peer_id, window_start)` | `uint64` (count) | Sliding window outbound rate limit tracking |
| `Params` | — | `Params` | Module parameters |
| `NextContentID` | — | `uint64` | Auto-increment counter |
| `NextOutboundAttestationID` | — | `uint64` | Auto-increment counter |

---

## 6. Messages

### 6.1. RegisterPeer (Commons Council)

Register a new federation peer. Submitted as a Commons Council proposal.

```protobuf
message MsgRegisterPeer {
  string authority = 1;            // Council policy address or governance
  string peer_id = 2;
  string display_name = 3;
  PeerType type = 4;
  string ibc_channel_id = 5;      // Required for Spark Dream peers
  string metadata = 6;
}
```

**Logic:**
1. Verify authority is governance or Commons Council policy address
2. Validate peer ID format (lowercase alphanumeric + hyphens + dots, 3-64 chars)
3. Check peer doesn't already exist (or is REMOVED — allow re-registration)
4. If peer is REMOVED, verify it is NOT in `PeerRemovalQueue` — reject with `ErrPeerCleanupInProgress` if cleanup is still running. Re-registration is only allowed after all associated data has been fully cleaned up.
5. Set status to PENDING (activated when IBC channel confirms or first bridge registers)
6. Create default PeerPolicy (empty content types, conservative defaults)
7. Emit `peer_registered` event

### 6.2. RemovePeer (Commons Council)

Permanently remove a federation peer. Triggers cleanup of all associated data.

```protobuf
message MsgRemovePeer {
  string authority = 1;            // Council policy address or governance
  string peer_id = 2;
  string reason = 3;
}
```

**Logic:**
1. Verify authority is governance or Commons Council policy address
2. Verify peer exists and is not already REMOVED
3. Set peer status to REMOVED, set `removed_at` to current block time
4. Add peer to `PeerRemovalQueue` for EndBlocker cleanup
5. Immediately reject all pending inbound content from this peer (any `MsgSubmitFederatedContent` in the same block after removal will fail with `ErrPeerNotActive`)
6. Emit `peer_removed` event

**Cleanup lifecycle** (handled by EndBlocker, see Section 9):
- All FederatedContent for this peer: deleted (pruned from storage and all indexes)
- All IdentityLinks for this peer: verified links marked REVOKED (preserved 90 days for audit), unverified links deleted immediately. Emit `identity_link_revoked` event for each verified link.
- All ReputationAttestations for this peer: deleted
- All OutboundAttestations for this peer: deleted
- All BridgeOperators for this peer: status set to REVOKED, remaining stake returned to operator via x/bank. Emit `bridge_revoked` event for each.
- PeerPolicy for this peer: deleted
- Peer record itself: deleted only after all associated data is cleaned up

### 6.3. SuspendPeer (Commons Council)

Temporarily suspend a peer. Blocks all inbound/outbound federation. Reversible via ResumePeer.

```protobuf
message MsgSuspendPeer {
  string authority = 1;            // Council policy address or governance
  string peer_id = 2;
  string reason = 3;
}
```

### 6.4. ResumePeer (Commons Council)

Resume a suspended peer.

```protobuf
message MsgResumePeer {
  string authority = 1;            // Council policy address or governance
  string peer_id = 2;
}
```

### 6.5. UpdatePeerPolicy (Operations Committee)

Update federation policies for a specific peer. Day-to-day operational tuning.

```protobuf
message MsgUpdatePeerPolicy {
  string authority = 1;            // Operations Committee member
  string peer_id = 2;
  PeerPolicy policy = 3;
}
```

**Authorization:** `isCouncilAuthorized(ctx, addr, "commons", "operations")`

**Validation:**
1. Verify all entries in `outbound_content_types` and `inbound_content_types` are present in `params.known_content_types` — reject with `ErrUnknownContentType` if any are not recognized
2. Reject policies that include `"reveal_proposal"` or `"reveal_tranche"` in either content type list (see Section 15.7)
3. If peer type is ACTIVITYPUB or ATPROTO, reject if `allow_reputation_queries` or `accept_reputation_attestations` is true — reputation bridging is only supported for Spark Dream peers (reject with `ErrPeerTypeMismatch`)

### 6.6. RegisterBridge (Operations Committee)

Register a bridge operator for a bridge-type peer.

```protobuf
message MsgRegisterBridge {
  string authority = 1;            // Operations Committee member
  string operator = 2;             // Bridge operator address
  string peer_id = 3;
  string protocol = 4;             // "activitypub" or "atproto"
  string endpoint = 5;
}
```

**Logic:**
1. Verify authority is Operations Committee
2. Verify peer exists and is type ACTIVITYPUB or ATPROTO
3. Check `max_bridges_per_peer` not exceeded
4. If operator was previously revoked for this peer, verify `bridge_revocation_cooldown` has elapsed since `revoked_at`. Reject with `ErrCooldownNotElapsed` if too soon.
5. Verify operator has at least `min_bridge_stake` available. Escrow from operator's account to federation module account.
6. Create BridgeOperator record (reset `content_submitted`, `content_rejected`, `slash_count` for fresh registration; `revoked_at` preserved for history)
7. If peer was PENDING, transition to ACTIVE
8. Emit `bridge_registered` event

### 6.7. RevokeBridge (Operations Committee)

Revoke a bridge operator. Returns remaining stake minus any slashing.

```protobuf
message MsgRevokeBridge {
  string authority = 1;
  string operator = 2;
  string peer_id = 3;
  string reason = 4;
}
```

**Logic:**
1. Verify authority is Operations Committee
2. Set bridge status to UNBONDING, set `revoked_at` to current block time
3. Set `unbonding_end_time` = block_time + `bridge_unbonding_period`
4. Add to `BridgeUnbondingQueue` for EndBlocker processing
5. All subsequent `MsgSubmitFederatedContent` from this operator for this peer are rejected immediately (UNBONDING bridges are not ACTIVE)
6. Emit `bridge_revoked` event

**Note:** Stake is NOT returned immediately. It remains locked during the unbonding period so the Operations Committee can still slash for misbehavior discovered after revocation. Stake is released automatically by EndBlocker when `unbonding_end_time` is reached (see Section 9, Phase 5).

### 6.8. SlashBridge (Operations Committee)

Slash a misbehaving bridge operator's stake.

```protobuf
message MsgSlashBridge {
  string authority = 1;
  string operator = 2;
  string peer_id = 3;
  string amount = 4;              // SPARK amount to slash [(gogoproto.customtype) = "cosmossdk.io/math.Int"]
  string reason = 5;
}
```

**Logic:**
1. Verify authority is Operations Committee
2. Verify slash amount does not exceed operator's remaining stake
3. Deduct from operator's escrowed stake, **burn** the slashed amount via `x/bank.BurnCoins` from the federation module account
4. Increment `slash_count`
5. **Auto-revocation check**: If remaining stake after slash is below `min_bridge_stake`, automatically set bridge status to UNBONDING, set `revoked_at` and `unbonding_end_time`, add to `BridgeUnbondingQueue`. Emit `bridge_auto_revoked` event with reason "stake below minimum after slash".
6. Emit `bridge_slashed` event

**Note:** Slashing works on both ACTIVE and UNBONDING bridges. An operator who is already in the unbonding period can still be slashed for misbehavior discovered during unbonding.

### 6.9. UpdateBridge (Operations Committee)

Update a bridge operator's endpoint or protocol metadata. Avoids the need to revoke and re-register for routine infrastructure changes.

```protobuf
message MsgUpdateBridge {
  string authority = 1;            // Operations Committee member
  string operator = 2;             // Bridge operator address
  string peer_id = 3;
  string endpoint = 4;             // New endpoint URL (empty string = no change)
}
```

**Logic:**
1. Verify authority is Operations Committee
2. Verify bridge exists and is ACTIVE
3. Update non-empty fields on the BridgeOperator record
4. Emit `bridge_updated` event

### 6.10. UnbondBridge (Bridge Operator — Self)

Voluntarily exit as a bridge operator. Initiates the standard unbonding period. The operator can still be slashed during unbonding for misbehavior discovered after exit.

```protobuf
message MsgUnbondBridge {
  string operator = 1;             // Bridge operator address (signer)
  string peer_id = 2;
}
```

**Logic:**
1. Verify signer is the bridge operator
2. Verify bridge exists and is ACTIVE (cannot unbond if already UNBONDING/REVOKED)
3. Set bridge status to UNBONDING, set `revoked_at` to current block time
4. Set `unbonding_end_time` = block_time + `bridge_unbonding_period`
5. Add to `BridgeUnbondingQueue` for EndBlocker processing
6. Emit `bridge_self_unbonded` event

**Note:** This mirrors the Operations Committee `MsgRevokeBridge` flow, but is initiated by the operator themselves. The unbonding period still applies (stake remains locked and slashable) — there is no instant exit. This prevents an operator from unbonding to escape a pending investigation. After unbonding completes, the operator must wait `bridge_revocation_cooldown` before re-registering for the same peer.

### 6.11. TopUpBridgeStake (Bridge Operator — Self)

Add additional SPARK to an existing bridge operator's escrowed stake. Allows operators to restore stake after partial slashing without needing to revoke and re-register (which would cause service disruption and require cooldown).

```protobuf
message MsgTopUpBridgeStake {
  string operator = 1;             // Bridge operator address (signer)
  string peer_id = 2;
  cosmos.base.v1beta1.Coin amount = 3;  // Additional SPARK to escrow
}
```

**Logic:**
1. Verify signer is the bridge operator
2. Verify bridge exists and is ACTIVE or UNBONDING (allow top-up during unbonding to potentially prevent auto-revocation on future slash checks)
3. Verify `amount.denom` is `uspark`
4. Transfer `amount` from operator's account to federation module account via x/bank
5. Add `amount` to operator's `stake` field
6. Emit `bridge_stake_topped_up` event

### 6.12. LinkIdentity (User)

Link local identity to a remote identity on a federation peer.

```protobuf
message MsgLinkIdentity {
  string creator = 1;             // Local address
  string peer_id = 2;
  string remote_identity = 3;     // Address, actor URI, or DID
}
```

**Logic:**
1. Verify peer exists and is ACTIVE
2. Verify no existing link for `(creator, peer_id)` — reject with `ErrIdentityLinkExists`
3. Verify no existing link for `(peer_id, remote_identity)` by any local address — reject with `ErrRemoteIdentityAlreadyClaimed`
4. Verify creator has not exceeded `max_identity_links_per_user` — reject with `ErrMaxIdentityLinksExceeded`
5. Create IdentityLink with status UNVERIFIED, `linked_at` = current block time
6. Add to `UnverifiedLinkExpirationQueue` with expiry = `linked_at + unverified_link_ttl`
7. Increment `IdentityLinkCount` for creator
8. For IBC peers: initiate verification packet (challenge-response, see Section 8.3)
9. For bridge peers: emit event for bridge to verify
10. Emit `identity_linked` event

### 6.13. UnlinkIdentity (User)

Remove an identity link.

```protobuf
message MsgUnlinkIdentity {
  string creator = 1;
  string peer_id = 2;
}
```

**Logic:**
1. Verify link exists for `(creator, peer_id)`
2. Remove from `IdentityLinks`, `IdentityLinksByRemote`, and any expiration queue entry
3. Decrement `IdentityLinkCount` for creator
4. Emit `identity_unlinked` event

### 6.14. ConfirmIdentityLink (User on Remote Chain)

Confirm a pending identity challenge on this chain. Called by the user whose address was claimed in an `IdentityVerificationPacket` from another chain. The transaction signature proves key ownership. See Section 8.3 for the full protocol flow.

```protobuf
message MsgConfirmIdentityLink {
  string creator = 1;             // Must be the claimed_address (signer proves key ownership)
  string claimant_chain_peer_id = 2; // Peer ID of the chain that sent the challenge
}
```

**Logic:**
1. Look up `PendingIdentityChallenge` for `(creator, claimant_chain_peer_id)` — reject with `ErrNoPendingChallenge` if not found
2. Verify challenge has not expired — reject with `ErrChallengeExpired` if past `expires_at`
3. Send `IdentityVerificationConfirmPacket` via IBC to the claimant chain (see Section 8.3)
4. Delete the `PendingIdentityChallenge`
5. Emit `identity_challenge_confirmed` event

### 6.15. SubmitFederatedContent (Bridge Operator)

Submit inbound content from an external protocol. Only callable by registered bridge operators.

```protobuf
message MsgSubmitFederatedContent {
  string operator = 1;             // Bridge operator address (signer)
  string peer_id = 2;
  string remote_content_id = 3;
  string content_type = 4;
  string creator_identity = 5;
  string creator_name = 6;
  string title = 7;
  string body = 8;
  string content_uri = 9;
  bytes protocol_metadata = 10;
  int64 remote_created_at = 11;
  bytes content_hash = 12;         // SHA-256 hash of original content (for integrity verification)
}
```

**Validation:**
1. Verify operator is a registered, ACTIVE bridge for this peer (status check rejects revoked bridges atomically)
2. Verify peer is ACTIVE
3. Verify `content_type` is in peer policy's `inbound_content_types`
4. Verify `creator_identity` is not in `blocked_identities`
5. Check rate limits (see Section 10.2 for sliding window details)
6. **Content hash** (MUST be provided): The `content_hash` field is **required** — reject with `ErrContentHashRequired` if empty. The hash must be `SHA-256(title + body)` computed from the **full, untruncated** source content. This is critical: verifiers independently fetch the full source content and compute the same hash. If the bridge computed the hash from truncated content, every piece of long content would produce a false DISPUTED status. The bridge operator is responsible for hashing the full content before submission.
7. Truncate `body` to `max_content_body_size`, `content_uri` to `max_content_uri_size`, `protocol_metadata` to `max_protocol_metadata_size`. Note: truncation happens AFTER the hash is stored — the on-chain body is a preview, but the hash covers the full source content.
8. Check `ContentByHash` index for duplicates — if the same hash already exists for this peer, reject with `ErrDuplicateContent`.
8. Set status to `PENDING_VERIFICATION` (bridge content requires independent verification — see Section 3.9). If peer policy `require_review` is true, content will additionally need Operations Committee review after verification.
9. Compute `expires_at` = current block time + `content_ttl`
10. Store FederatedContent with `expires_at`, add to `ContentExpirationQueue`, `ContentByCreator`, and `ContentByHash` indexes
11. Add to `VerificationWindowQueue` with expiry = block_time + `verification_window`
12. Increment bridge's `content_submitted`, update `last_submission_at`
13. Emit `federated_content_received` event

### 6.16. FederateContent (Content Creator)

Authorize outbound federation of local content to an IBC peer. **Must be signed by the content creator** — this prevents relayers from fabricating content attributed to arbitrary users. The relayer's role is to build and broadcast the transaction on behalf of the creator (using x/session delegation or by requesting the creator's signature).

```protobuf
message MsgFederateContent {
  string creator = 1;              // Content creator address (signer — proves authenticity)
  string peer_id = 2;              // Target IBC peer
  string content_type = 3;
  string local_content_id = 4;     // ID in the source content module
  string title = 5;
  string body = 6;
  string content_uri = 7;
  bytes content_hash = 8;          // SHA-256 of (title + body)
}
```

**Logic:**
1. Verify peer exists, is ACTIVE, and is type SPARK_DREAM (IBC only — bridge peers use `MsgAttestOutbound` instead)
2. Verify `content_type` is in peer policy's `outbound_content_types`
3. Verify `creator` meets `min_outbound_trust_level` from peer policy (query x/rep)
4. Check outbound rate limits (see Section 10.3)
5. Send `ContentPacket` via IBC to the peer's channel (with `creator` as the content creator)
6. Store `OutboundAttestation` automatically
7. Emit `content_federated` event

**Authenticity model:** The content creator signs this message, so the receiving chain knows the sending chain's validator set attested that this user authorized the federation. The IBC light client verifies the sending chain's state transition, which includes signature verification. This makes content fabrication impossible without compromising the sending chain's validator set.

**Session key delegation:** Content creators who want automated federation can create an x/session session key for a relayer daemon, scoped to `MsgFederateContent` only. This allows the relayer to federate content on the creator's behalf without holding their main private key. The session key proves the creator authorized the delegation.

**Note:** The `content_creator` field from the previous design was removed — `creator` (the signer) IS the content creator. This eliminates the authenticity gap where a relayer could attribute fabricated content to any address.

### 6.17. AttestOutbound (Bridge Operator)

Record that content was published to an external (non-IBC) peer. Creates an on-chain audit trail so that the Operations Committee can verify bridge operator behavior — e.g., confirm that a bridge is actually publishing content it claims to publish, or identify which operator published problematic content to a peer.

For IBC peers, outbound attestations are created automatically by `MsgFederateContent`. For bridge peers, bridge operators submit this message after publishing content to the external protocol.

```protobuf
message MsgAttestOutbound {
  string operator = 1;
  string peer_id = 2;
  string content_type = 3;
  string local_content_id = 4;
}
```

**Logic:**
1. Verify operator is a registered, ACTIVE bridge for this peer
2. Verify peer is ACTIVE
3. Verify `content_type` is in peer policy's `outbound_content_types`
4. Store OutboundAttestation
5. Emit `outbound_attested` event

### 6.18. ModerateContent (Operations Committee)

Moderate inbound federated content (hide, reject, or restore).

```protobuf
message MsgModerateContent {
  string authority = 1;            // Operations Committee member
  uint64 content_id = 2;
  FederatedContentStatus new_status = 3;
  string reason = 4;
}
```

### 6.19. RequestReputationAttestation (User)

Request a reputation attestation from a Spark Dream peer via IBC.

```protobuf
message MsgRequestReputationAttestation {
  string creator = 1;             // Local address requesting attestation
  string peer_id = 2;             // Spark Dream peer to query
  string remote_address = 3;      // Address on the remote chain to query
}
```

**Logic:**
1. Verify peer is type SPARK_DREAM and ACTIVE
2. Verify peer policy `accept_reputation_attestations` is true
3. Send IBC `ReputationQueryPacket` to peer with timeout = `ibc_packet_timeout`
4. Response handled in `OnAcknowledgementPacket` — stores ReputationAttestation
5. Timeout handled in `OnTimeoutPacket` — emits `reputation_query_timeout` event, user must retry manually

### 6.20. Verifier bonding (moved to x/rep)

> **Phase 1–4 bonded-role generalization:** the former federation-local `MsgBondVerifier` / `MsgUnbondVerifier` have been subsumed by x/rep's generic `MsgBondRole` / `MsgUnbondRole`. Verifier bonding now flows through rep's role-typed endpoint; federation no longer owns a registration message.

Invoked as:

- `tx rep bond-role ROLE_TYPE_FEDERATION_VERIFIER <amount>` — bond DREAM to become a federation verifier.
- `tx rep unbond-role ROLE_TYPE_FEDERATION_VERIFIER <amount>` — withdraw bonded DREAM (subject to committed-bond constraints).

**Bond logic (implemented in rep against the federation-seeded `BondedRoleConfig`):**

1. Verify creator meets `MinTrustLevel` (seeded from federation's `min_verifier_trust_level`).
2. If re-bonding after demotion, verify `demotion_cooldown_until` has passed — reject with `ErrDemotionCooldown`.
3. Lock DREAM via rep's `LockDREAM` (moves from available balance to staked, non-decay).
4. Create or update `BondedRole(ROLE_TYPE_FEDERATION_VERIFIER, addr)`, increment `current_bond`.
5. Compute `bond_status` from config thresholds: `NORMAL` if `≥ min_bond`, `RECOVERY` if `≥ demotion_threshold`, otherwise `DEMOTED`.
6. Emit `bonded_role_bonded` event with `role_type=ROLE_TYPE_FEDERATION_VERIFIER`.

**Unbond logic:**

1. Verify `amount ≤ current_bond - total_committed_bond` — reject with `ErrInsufficientBond` if insufficient available bond (in-flight `MsgVerifyContent` reservations and unresolved challenges lock committed portions).
2. `UnlockDREAM` the amount back to the verifier's available balance.
3. Recompute `bond_status`. If transitioning to `DEMOTED`, set `demotion_cooldown_until = block_time + demotion_cooldown`.
4. The `BondedRole` record persists even at `current_bond == 0` to preserve the demotion cooldown; rep never deletes role records.
5. Emit `bonded_role_unbonded` event.

### 6.22. VerifyContent (Verifier)

Independently verify bridged content by providing a hash computed from the source platform.

```protobuf
message MsgVerifyContent {
  string creator = 1;             // Verifier address (signer)
  uint64 content_id = 2;          // FederatedContent ID to verify
  bytes content_hash = 3;         // SHA-256 hash independently computed by fetching source content
}
```

**Logic:**
1. Verify creator is a bonded verifier with status NORMAL or RECOVERY
2. Verify creator is not in `overturn_cooldown_until`
3. Verify content exists and is in PENDING_VERIFICATION status — **first-verifier-wins**: if another verifier already changed the status (to VERIFIED or DISPUTED) in an earlier transaction in the same block, this check fails with `ErrContentNotPendingVerification`. Only one verifier can verify each piece of content.
4. Verify creator is NOT the bridge operator who submitted this content (`content.submitted_by != creator`) — reject with `ErrSelfVerification`. This prevents an entity from operating as both bridge operator and verifier for the same content, which would bypass the accountability layer entirely. Note: this is enforced per-content (not per-role), because the same person using two different addresses cannot be detected on-chain, but they cannot use the *same* address for both roles on the *same* content.
5. Verify content is still within `verification_window` (not expired)
6. Verify verifier has sufficient uncommitted bond: `current_bond - total_committed_bond ≥ verifier_slash_amount`
7. Compare `content_hash` with the bridge operator's submitted `content_hash` on the FederatedContent record:
   - **Match**: Content status → VERIFIED. Create VerificationRecord with `committed_amount = verifier_slash_amount`. Increment verifier's `total_committed_bond`. Emit `content_verified` event.
   - **Mismatch**: Content status → DISPUTED. Create VerificationRecord with `committed_amount = verifier_slash_amount`. Increment verifier's `total_committed_bond`. Initiate two-phase resolution with verifier as implicit challenger (see Section 3.9.2 step 4 for dispute-specific verdict outcomes — no challenge fee, verifier bond at stake). Add to `ArbiterResolutionQueue`. Emit `content_disputed` event.
8. Increment verifier's `total_verifications`, `epoch_verifications`
9. Update operator's `content_verified` count (on match only)

### 6.23. ChallengeVerification (Member)

Challenge a VERIFIED piece of content by providing evidence that the verifier's hash is incorrect.

```protobuf
message MsgChallengeVerification {
  string creator = 1;             // Challenger address (signer)
  uint64 content_id = 2;          // FederatedContent ID to challenge
  bytes content_hash = 3;         // SHA-256 hash independently computed by challenger
  string evidence = 4;            // Description/URL of evidence supporting the challenge
}
```

**Logic:**
1. Verify creator meets `min_verifier_trust_level` (challengers need community standing)
2. Verify content exists and is VERIFIED
3. Look up VerificationRecord — verify `challenge_window_ends` has not passed (reject with `ErrChallengeWindowExpired`)
4. Verify challenger is not the original verifier or the submitting bridge operator (reject with `ErrSelfChallenge`)
5. **Anti-censorship check**: If this content was previously challenged and the challenge was rejected (verifier was right), verify `challenge_cooldown` has elapsed since the last challenge resolution. Reject with `ErrChallengeCooldownActive` if too soon. The fee also escalates: `effective_fee = challenge_fee × 2^(prior_rejected_challenges)` for this content. This makes sustained censorship-by-challenge exponentially expensive while keeping the first legitimate challenge affordable.
6. Escrow `effective_fee` SPARK from challenger to federation module account
6. Content status → CHALLENGED
7. VerificationRecord outcome → CHALLENGED
8. Start Phase 1 (anonymous resolution): set `arbiter_resolution_deadline` = block_time + `arbiter_resolution_window`. Add to `ArbiterResolutionQueue`.
9. Emit `verification_challenged` event with `content_id`, `challenger`, `verifier`

### 6.24. SubmitArbiterHash (Bridge Operator or Anonymous Member)

Submit an independent hash for quorum-based challenge resolution. Two submission paths: identified (bridge operator signs directly) or anonymous (member submits via x/shield `MsgShieldedExec`).

```protobuf
message MsgSubmitArbiterHash {
  string creator = 1;              // Bridge operator address (identified path) or shield module address (anonymous path)
  uint64 content_id = 2;
  bytes content_hash = 3;          // SHA-256 hash independently computed from content_uri
}
```

**Logic (identified path):**
1. Verify creator is a registered, ACTIVE bridge operator for the same peer as the challenged content
2. Verify creator is not the bridge operator who submitted the challenged content (reject with `ErrSelfArbiter`)
3. Verify content is in CHALLENGED status and within `arbiter_resolution_window`
4. Verify no existing submission from this operator for this content_id
5. Store `ArbiterHashSubmission` with `operator = creator`
6. Increment `ArbiterHashCounts[(content_id, content_hash)]`
7. If count reaches `arbiter_quorum`: trigger auto-resolution (see Section 3.9.7), add to `ArbiterEscalationQueue`
8. Emit `arbiter_hash_submitted` event

**Logic (anonymous path — dispatched by x/shield after ZK proof verification):**
1. Verify content is in CHALLENGED status and within `arbiter_resolution_window`
2. x/shield has already verified: trust_level >= ESTABLISHED, nullifier is unique for this content_id
3. Store `ArbiterHashSubmission` with `nullifier` from x/shield context
4. Increment `ArbiterHashCounts[(content_id, content_hash)]`
5. If count reaches `arbiter_quorum`: trigger auto-resolution, add to `ArbiterEscalationQueue`
6. Emit `arbiter_hash_submitted` event

### 6.25. EscalateChallenge (Challenger or Verifier)

Escalate an auto-resolved challenge to a human jury. Either party can escalate if they disagree with the anonymous resolution verdict.

```protobuf
message MsgEscalateChallenge {
  string creator = 1;             // Challenger or verifier address (signer)
  uint64 content_id = 2;
}
```

**Logic:**
1. Verify content is in CHALLENGED status with an auto-resolution verdict pending escalation
2. Verify `arbiter_escalation_deadline` has not passed
3. Verify creator is the challenger or the verifier for this content
4. Escrow `escalation_fee` SPARK from creator (returned if jury overturns the auto-verdict)
5. Reverse the auto-resolution: restore content to CHALLENGED status, undo any slashing/refunds applied by auto-resolution
6. Create x/rep jury initiative with full evidence package: operator hash, verifier hash, challenger hash, all anonymous arbiter hashes, content URI, challenger evidence
7. Emit `challenge_escalated` event

**Resolution verdicts** (same outcomes for both Phase 1 auto-resolution and Phase 2 jury):

- **CHALLENGE_UPHELD** (verifier was wrong):
  1. Verifier slashed `verifier_slash_amount` DREAM: 50% to challenger as bounty (via x/rep), 50% burned
  2. Update verifier: `overturned_verifications++`, `consecutive_overturns++`, `consecutive_upheld = 0`
  3. Apply escalating cooldown: `overturn_cooldown_until = block_time + base_cooldown * 2^(consecutive_overturns - 1)`, capped at 7 days
  4. Update `bond_status` (may transition to RECOVERY or DEMOTED)
  5. Challenger refunded 100% of `challenge_fee`
  6. Content status → REJECTED. Operator's `content_rejected` incremented.

- **CHALLENGE_REJECTED** (verifier was right):
  1. Verifier's `upheld_verifications++`, `consecutive_upheld++`
  2. If `consecutive_upheld ≥ upheld_to_reset_overturns` (default: 3): reset `consecutive_overturns = 0`
  3. Challenger loses fee: 50% to verifier (SPARK reward), 50% burned
  4. Content stays VERIFIED

- **CHALLENGE_TIMEOUT** (Phase 1: no quorum within `arbiter_resolution_window`):
  1. Automatically escalate to Phase 2 (human jury). No fee for auto-escalation.

- **CHALLENGE_TIMEOUT** (Phase 2: no jury consensus within `challenge_jury_deadline`):
  1. Challenger refunded 50%. 50% burned.
  2. No slash. Content stays VERIFIED.
  3. Verifier not penalized (balanced outcome)

**Content status during resolution:**
- During Phase 1 (arbiter quorum) and Phase 2 (jury): content remains in CHALLENGED status. It is excluded from default frontend listings (same visibility as HIDDEN). This prevents users from consuming content whose authenticity is actively disputed.
- During the escalation window (48h after auto-resolution): content status reflects the provisional verdict (REJECTED if upheld, VERIFIED if rejected). If escalated, status reverts to CHALLENGED and the provisional verdict is unwound.
- `MsgModerateContent` can be applied to CHALLENGED content (Operations Committee can override at any time regardless of dispute status).
- New challenges cannot be filed against CHALLENGED content (already in dispute).

### 6.26. UpdateParams (Governance)

```protobuf
message MsgUpdateParams {
  string authority = 1;            // x/gov module account
  Params params = 2;
}
```

**Logic:**
1. Verify authority is governance module account
2. Validate all params against ranges in Section 4.13
3. **Content type removal check**: If `known_content_types` is being reduced (types removed), scan all `PeerPolicy` records for references to the removed types. Automatically strip removed types from all `inbound_content_types` and `outbound_content_types` lists in affected PeerPolicies. Emit `peer_policy_auto_updated` event for each modified policy. This prevents stale type references from silently blocking content federation.
4. Set params

### 6.27. UpdateOperationalParams (Operations Committee)

```protobuf
message MsgUpdateOperationalParams {
  string authority = 1;
  FederationOperationalParams operational_params = 2;
}
```

**Logic:**
1. Verify authority via `commonsKeeper.IsCouncilAuthorized(ctx, authority, "commons", "operations")`
2. Validate all operational params against ranges in Section 4.13
3. Merge into current Params (only overwrite the operational subset)
4. Emit `operational_params_updated` event with old and new values for each changed field

---

## 7. Queries

| Query | Input | Output | Description |
|-------|-------|--------|-------------|
| `GetPeer` | peer_id | Peer | Peer details |
| `ListPeers` | status filter, pagination | []Peer | List peers |
| `GetPeerPolicy` | peer_id | PeerPolicy | Policy for a peer |
| `GetBridgeOperator` | address, peer_id | BridgeOperator | Bridge operator details |
| `ListBridgeOperators` | peer_id filter, pagination | []BridgeOperator | List bridge operators |
| `GetFederatedContent` | id | FederatedContent | Single content item |
| `ListFederatedContent` | peer_id, content_type, creator_identity, status filters, pagination | []FederatedContent | List federated content |
| `GetIdentityLink` | local_address, peer_id | IdentityLink | Identity link details |
| `ListIdentityLinks` | local_address or peer_id filter, pagination | []IdentityLink | List identity links |
| `ResolveRemoteIdentity` | peer_id, remote_identity | local_address | Reverse lookup: remote → local |
| `GetPendingIdentityChallenge` | claimed_address, peer_id | PendingIdentityChallenge | Pending challenge details |
| `ListPendingIdentityChallenges` | claimed_address filter, pagination | []PendingIdentityChallenge | List pending challenges for an address |
| `GetReputationAttestation` | local_address, peer_id | ReputationAttestation | Cached reputation |
| `ListOutboundAttestations` | peer_id filter, pagination | []OutboundAttestation | Outbound audit trail |
| `VerifierActivity` | address | VerifierActivity | Federation-specific counters. Generic bond state is at `query rep bonded-role ROLE_TYPE_FEDERATION_VERIFIER <addr>`. |
| (use `query rep bonded-roles-by-type ROLE_TYPE_FEDERATION_VERIFIER`) | role_type filter, pagination | []BondedRole | List bonded verifiers (lives in x/rep) |
| `GetVerificationRecord` | content_id | VerificationRecord | Verification record for content |
| `Params` | — | Params | Module parameters |

---

## 8. IBC Protocol

### 8.1. Port and Channel

- **Port ID**: `federation` (configurable via params)
- **Channel version**: `federation-1`
- **Channel ordering**: UNORDERED (content and reputation queries are independent)

### 8.2. Channel Handshake

On `OnChanOpenInit` / `OnChanOpenTry`:
1. Verify port is `federation`
2. Verify counterparty port is `federation`
3. Verify channel version matches or is compatible (see Section 8.5)

On `OnChanOpenAck` / `OnChanOpenConfirm`:
1. Find the Peer record matching this IBC channel
2. Transition peer status from PENDING to ACTIVE
3. Emit `peer_activated` event

### 8.3. Packet Types

```protobuf
message FederationPacketData {
  oneof packet {
    ReputationQueryPacket reputation_query = 1;
    ContentPacket content = 2;
    IdentityVerificationPacket identity_verification = 3;
    IdentityVerificationConfirmPacket identity_confirmation = 4;
  }
}
```

#### ReputationQueryPacket

```protobuf
message ReputationQueryPacket {
  string queried_address = 1;      // Address on the receiving chain
  string requester = 2;            // Address on the sending chain
}
```

**OnRecvPacket** (receiving chain):
1. Check peer policy `allow_reputation_queries`
2. Look up member via x/rep: trust level, active status, per-tag reputations
3. Return acknowledgement with `ReputationResponseData`

```protobuf
message ReputationResponseData {
  string address = 1;
  uint32 trust_level = 2;
  bool is_active = 3;
  int64 member_since = 4;
  repeated TagReputation reputations = 5;
}
```

**OnAcknowledgementPacket** (sending chain):
1. Parse `ReputationResponseData` from ack
2. Apply trust discount: `local_credit = min(discounted_trust_level, peer_max_trust_credit, global_max_trust_credit)`
3. Store ReputationAttestation with `expires_at` = now + `attestation_ttl`, add to `AttestationExpirationQueue`
4. Emit `reputation_attested` event

**OnTimeoutPacket** (sending chain):
1. Emit `reputation_query_timeout` event with `peer_id`, `queried_address`, `requester`
2. No automatic retry — user must submit a new `MsgRequestReputationAttestation`

#### ContentPacket

```protobuf
message ContentPacket {
  string content_type = 1;
  string remote_content_id = 2;
  string creator = 3;
  string creator_name = 4;
  string title = 5;
  string body = 6;
  string content_uri = 7;
  int64 created_at = 8;
  bytes content_hash = 9;          // SHA-256 hash of original content
  bytes protocol_metadata = 10;    // Protocol-specific metadata (e.g., chain-specific tags, categories)
}
```

**OnRecvPacket** (receiving chain):
1. Identify source peer from channel
2. Validate against peer policy (content type allowed, rate limits)
3. Store `content_hash` from the packet directly (hash covers full source content, computed by the sending chain)
4. Truncate `body` to `max_content_body_size`, `content_uri` to `max_content_uri_size`, `protocol_metadata` to `max_protocol_metadata_size` (truncation is for storage only — hash covers full content)
5. Check `ContentByHash` for duplicates from this peer — reject if same hash already exists
6. Store as FederatedContent with computed `expires_at`, add to `ContentExpirationQueue`, `ContentByCreator`, `ContentByHash`
6. Return success/rejection ack

**OnTimeoutPacket** (sending chain):
1. Emit `content_send_timeout` event with `peer_id`, `content_type`, `remote_content_id`
2. No automatic retry — content is considered undelivered. Bridge operators or the module can retry by sending a new packet.

#### IdentityVerificationPacket (Two-Phase Protocol)

Cross-chain identity verification uses a two-phase protocol that proves key ownership. The user must sign a transaction on the remote chain — the signature on that transaction IS the proof.

**Phase 1: Challenge Delivery**

When a user on Chain A calls `MsgLinkIdentity`, Chain A sends a challenge to Chain B:

```protobuf
message IdentityVerificationPacket {
  string claimed_address = 1;      // Address on Chain B being claimed
  string claimant_address = 2;     // Address on Chain A making the claim
  bytes challenge = 3;             // Random 32-byte challenge
}
```

**OnRecvPacket** on Chain B:
1. Check if `claimed_address` exists via x/auth — if not, ack with `exists = false`
2. Store a `PendingIdentityChallenge` keyed by `(claimed_address, sending_chain_peer_id)` with expiry = block_time + `challenge_ttl`
3. Ack with `exists = true` (challenge stored, awaiting user confirmation)
4. Emit `identity_challenge_received` event so frontends can notify the user

```protobuf
message IdentityVerificationAck {
  bool exists = 1;                 // Whether claimed_address exists on Chain B
}
```

**OnAcknowledgementPacket** on Chain A:
1. If `exists = false`, mark link as REVOKED (remote address doesn't exist)
2. If `exists = true`, link remains UNVERIFIED — waiting for Phase 2

**Phase 2: User Confirmation**

The user who controls `claimed_address` on Chain B signs a `MsgConfirmIdentityLink` transaction. The signature on this transaction proves they control the private key for `claimed_address`.

```protobuf
message MsgConfirmIdentityLink {
  string creator = 1;             // Must be the claimed_address (signer proves key ownership)
  string claimant_chain_peer_id = 2; // Peer ID of the chain that sent the challenge
}
```

**Logic** on Chain B:
1. Look up `PendingIdentityChallenge` for `(creator, claimant_chain_peer_id)` — reject with `ErrNoPendingChallenge` if not found
2. Verify challenge has not expired — reject with `ErrChallengeExpired` if past `expires_at`
3. The fact that `creator` signed this transaction proves they own the private key for `claimed_address` — no separate signature verification needed
4. Send `IdentityVerificationConfirmPacket` to Chain A via IBC:

```protobuf
message IdentityVerificationConfirmPacket {
  string claimed_address = 1;      // Address on Chain B (confirmed)
  string claimant_address = 2;     // Address on Chain A
  bytes challenge = 3;             // Echoed challenge (for matching)
  bool confirmed = 4;             // Always true (only sent on successful confirmation)
}
```

5. Delete the `PendingIdentityChallenge`
6. Emit `identity_challenge_confirmed` event

**OnRecvPacket** on Chain A (receiving confirmation):
1. Match the `challenge` and `claimant_address` against the pending IdentityLink
2. Upgrade IdentityLink status to VERIFIED, set `verified_at` = block_time
3. Remove from `UnverifiedLinkExpirationQueue`
4. Emit `identity_verified` event

**Timeout handling:**
- Phase 1 timeout (challenge delivery): Emit `identity_verification_timeout` event. Link remains UNVERIFIED. Automatic retry up to 3 times; after that, emit `identity_verification_failed` and link stays UNVERIFIED until pruned by TTL.
- Phase 2 timeout (confirmation delivery): Emit `identity_confirmation_timeout` event on Chain B. User can re-submit `MsgConfirmIdentityLink` which sends a new confirmation packet.
- Challenge expiry: If the user on Chain B doesn't confirm within `challenge_ttl` (default 7 days), the `PendingIdentityChallenge` is pruned by EndBlocker. The link on Chain A remains UNVERIFIED and will be pruned by `unverified_link_ttl`.

**Why this proves ownership:** The security comes from the Tendermint/CometBFT transaction signing model. `MsgConfirmIdentityLink` is a standard Cosmos SDK transaction signed by `creator`. The chain verifies the signature against `creator`'s public key before executing the message. If the signature doesn't match, the transaction is rejected at the ante handler level. Therefore, successful execution of this message is proof that the signer controls the private key for `claimed_address`.

### 8.4. Packet Timeout Configuration

All outbound IBC packets use the `ibc_packet_timeout` parameter (default: 10 minutes) as the timeout duration. This is set relative to the sending chain's block time when the packet is created.

**Timeout behavior by packet type:**

| Packet Type | On Timeout | Retry? |
|-------------|-----------|--------|
| `ReputationQueryPacket` | Emit event, no state change | Manual (user re-submits `MsgRequestReputationAttestation`) |
| `ContentPacket` | Emit event, content not delivered | Manual (bridge or module re-sends) |
| `IdentityVerificationPacket` | Emit event, link stays UNVERIFIED | Automatic on next block if link still exists; give up after 3 attempts, emit `identity_verification_failed` |

Identity verification is the only packet type with automatic retry, because it is triggered by user action (`MsgLinkIdentity`) and the user cannot easily re-trigger verification without unlinking and re-linking.

### 8.5. Channel Version Compatibility

If `ibc_channel_version` is updated via governance (e.g., from `federation-1` to `federation-2`):

1. **Existing channels remain open** with their original version. They continue to function with the packet format they were opened with.
2. **New `RegisterPeer` calls** create channels with the new version.
3. **`OnChanOpenTry` version negotiation**: If the counterparty proposes a different version, accept if the proposed version is in the set of supported versions (the module maintains a hardcoded list: `["federation-1"]` at launch, extended via chain upgrades).
4. **`OnRecvPacket` compatibility**: The module inspects the channel version to determine packet format. If a new packet type is added in `federation-2`, it is simply ignored by `federation-1` receivers (the `oneof` will have no matching case, and the packet is acked with an error).
5. **Migration**: To upgrade an existing peer to a new channel version, the Operations Committee should: (a) register a new IBC channel with the new version, (b) update the peer's `ibc_channel_id`, (c) close the old channel. This is a manual process — no automatic migration.

---

## 9. EndBlocker

The EndBlocker handles periodic cleanup and monitoring. All pruning phases share a single `pruned` counter capped at `max_prune_per_block` to bound gas consumption. Phases execute in order; if the cap is reached, remaining work is deferred to the next block.

### Phase 1: Prune Expired Federated Content

Walk `ContentExpirationQueue` from earliest entry. For each entry where `expires_at <= block_time`:
1. Delete FederatedContent from primary store and all indexes (`ContentByPeer`, `ContentByType`, `ContentByCreator`, `ContentByHash`)
2. Delete the expiration queue entry
3. Increment `pruned` counter
4. Stop when `pruned >= max_prune_per_block` or queue is exhausted

This is O(expired_count) per block, not O(total_content).

### Phase 2: Prune Expired Reputation Attestations

Walk `AttestationExpirationQueue` from earliest entry. For each entry where `expires_at <= block_time`:
1. Delete ReputationAttestation from primary store
2. Delete the expiration queue entry
3. Emit `reputation_expired` event
4. Increment `pruned` counter

### Phase 3: Prune Expired Unverified Identity Links

Walk `UnverifiedLinkExpirationQueue` from earliest entry. For each entry where the expiry time <= block_time:
1. Look up the IdentityLink — if still UNVERIFIED, delete it from `IdentityLinks`, `IdentityLinksByRemote`, decrement `IdentityLinkCount`
2. If the link was already VERIFIED (verification completed before TTL), just remove the queue entry
3. Emit `identity_link_expired` event for deleted links
4. Increment `pruned` counter

### Phase 4: Prune Expired Identity Challenges

Walk `PendingIdentityChallenges` and delete entries where `expires_at <= block_time`. Increment `pruned` counter. Emit `identity_challenge_expired` event for each.

### Phase 5: Release Unbonded Bridge Stakes

Walk `BridgeUnbondingQueue` from earliest entry. For each entry where `unbonding_end_time <= block_time`:
1. Look up the BridgeOperator
2. Return remaining stake to operator via x/bank
3. Set status to REVOKED (unbonding complete)
4. Remove from `BridgeUnbondingQueue`
5. Emit `bridge_unbonding_complete` event
6. Increment `pruned` counter

### Phase 6: Expire Unverified Content

Walk `VerificationWindowQueue` from earliest entry. For each entry where `expires_at <= block_time`:
1. Look up FederatedContent — if still `PENDING_VERIFICATION`, set status to `HIDDEN`
2. Increment the submitting bridge operator's `content_unverified` counter
3. Delete the queue entry
4. Emit `content_verification_expired` event
5. Increment `pruned` counter

Unverified content stays in storage (queryable) but is excluded from default frontend listings.

### Phase 7: Release Verifier Bond Commitments

Walk `ChallengeWindowQueue` from earliest entry. For each entry where `challenge_window_ends <= block_time`:
1. Look up VerificationRecord — if outcome is still `PENDING` (no challenge filed):
   - Set outcome to `CONFIRMED`
   - Release verifier's committed bond: `total_committed_bond -= committed_amount`
   - Increment verifier's `unchallenged_verifications`
2. Delete the queue entry
3. Increment `pruned` counter

### Phase 8: Expire Arbiter Resolution Windows

Walk `ArbiterResolutionQueue` from earliest entry. For each entry where `arbiter_resolution_deadline <= block_time`:
1. If no quorum was reached (challenge still in CHALLENGED status without auto-resolution):
   - Automatically escalate to Phase 2 (human jury): create x/rep jury initiative with full evidence
   - Emit `arbiter_resolution_expired` event
2. Clean up `ArbiterHashSubmissions` and `ArbiterHashCounts` for this content_id
3. Delete the queue entry
4. Increment `pruned` counter

### Phase 9: Finalize Auto-Resolutions

Walk `ArbiterEscalationQueue` from earliest entry. For each entry where `arbiter_escalation_deadline <= block_time`:
1. If no `MsgEscalateChallenge` was submitted within the escalation window:
   - The auto-resolution verdict becomes final
   - Clean up `ArbiterHashSubmissions` and `ArbiterHashCounts` for this content_id
2. Delete the queue entry
3. Increment `pruned` counter

### Phase 10: Process Peer Removal Queue

Each entry in `PeerRemovalQueue` stores a `PeerRemovalState` that tracks cleanup progress with a cursor:

```protobuf
message PeerRemovalState {
  int64 removed_at = 1;
  uint64 last_pruned_content_id = 2;   // Cursor: resume content deletion from here
  bool content_done = 3;
  bool links_done = 4;
  bool attestations_done = 5;
  bool outbound_done = 6;
  bool bridges_done = 7;
  bool policy_done = 8;
}
```

For each peer in `PeerRemovalQueue` (bounded by remaining prune budget):
1. **Content cleanup (cursor-based):** Walk `ContentByPeer` index starting from `last_pruned_content_id + 1`. For each content item, before deleting:
   - If content is in CHALLENGED or DISPUTED status: cancel the dispute. Refund any escrowed `challenge_fee` to the challenger. Release verifier's `committed_bond`. Delete associated `VerificationRecord`, `ArbiterHashSubmissions`, and queue entries (`ArbiterResolutionQueue`, `ArbiterEscalationQueue`). Emit `challenge_cancelled_peer_removed` event.
   - If content is in PENDING_VERIFICATION: delete `VerificationWindowQueue` entry.
   - If content is VERIFIED with active challenge window: release verifier's committed bond, delete `VerificationRecord` and `ChallengeWindowQueue` entry.
   - Delete the content from primary store and all indexes.
   Update `last_pruned_content_id` to the last deleted ID. Set `content_done = true` when the index walk finds no more items. This guarantees O(N) total operations across all blocks, not O(N^2).
2. **Identity links** (if `content_done` and budget remains): Mark verified links as REVOKED with `verified_at` preserved (kept 90 days for audit trail), delete unverified links immediately. Emit events. Set `links_done = true`.
3. **Reputation attestations** (if `links_done`): Delete all for this peer. Set `attestations_done = true`.
4. **Outbound attestations** (if `attestations_done`): Delete all for this peer. Set `outbound_done = true`.
5. **Bridge operators** (if `outbound_done`): Revoke all for this peer — set status UNBONDING, add to `BridgeUnbondingQueue` for stake return after unbonding period. Emit events. Set `bridges_done = true`.
6. **Peer policy** (if `bridges_done`): Delete. Set `policy_done = true`.
7. If ALL flags are true, delete the Peer record itself and remove from `PeerRemovalQueue`. Emit `peer_cleanup_complete` event.
8. If budget exhausted at any step, save cursor state — remaining work resumes from exactly where it left off next block.

### Phase 11: Verifier Epoch Rewards and Counter Reset

Triggered once per epoch (determined by `IsRewardEpoch(ctx)` from x/season, fallback: 7-day intervals):

**DREAM reward distribution:**
1. Identify eligible verifiers: bond status NORMAL or RECOVERY, no slashing this epoch, `epoch_verifications >= min_epoch_verifications`, accuracy ≥ `min_verifier_accuracy`
2. Calculate total DREAM to mint: `min(eligible_count × verifier_dream_reward, max_verifier_dream_mint_per_epoch)`
3. If cap hit, compute `scale_factor = max_verifier_dream_mint_per_epoch / (eligible_count × verifier_dream_reward)` — all verifiers scaled equally
4. For each eligible verifier:
   - If RECOVERY: auto-bond DREAM until `current_bond >= min_verifier_bond`, pay remainder. Emit `verifier_dream_reward_auto_bonded`. If bond restored, emit `verifier_bond_restored`.
   - If NORMAL: mint DREAM directly to verifier address. Emit `verifier_dream_reward_paid`.
5. Update `last_reward_epoch` for each paid verifier

**Epoch counter reset** (all verifiers):
- `epoch_verifications = 0`
- `epoch_challenges_resolved = 0`
- Update `last_active_epoch` and `consecutive_inactive_epochs` for accuracy decay tracking (mirrors forum sentinel model)

### Phase 12: Bridge Operator Monitoring

This phase checks bridge operator health. To bound gas cost, it processes at most `max_bridges_per_peer × number_of_active_peers` operators (which is bounded by `max_bridges_per_peer` × peer count — a small number). Operators that were checked recently (within the last `bridge_inactivity_threshold / 2` epochs) are skipped, since their status cannot have changed meaningfully. This ensures O(stale_bridges) work per block, not O(total_bridges).

For each active bridge operator checked:
1. If `last_submission_at` is set and `(block_time - last_submission_at) / epoch_duration > bridge_inactivity_threshold`, emit `bridge_inactive_warning` event
2. **Stake monitoring**: If bridge's `stake < min_bridge_stake` (can happen if `min_bridge_stake` was raised by governance), emit `bridge_stake_insufficient` event. This does NOT auto-revoke — the Operations Committee investigates and decides. This preserves the principle that governance param changes don't cause automatic disruptions.

### Phase 13: Clean Stale Rate Limit Counters

Delete `RateLimitCounters` and `OutboundRateLimitCounters` entries where `window_start + 2 * rate_limit_window < block_time`. This is bounded by the number of active peers (small).

---

## 10. Business Logic

### 10.1. Trust Discounting

When a reputation attestation is received, the receiving chain applies discounting:

```go
func (k Keeper) CalculateTrustCredit(ctx context.Context, remoteTrustLevel uint32, peerID string) uint32 {
    params := k.GetParams(ctx)
    policy := k.GetPeerPolicy(ctx, peerID)

    // Apply discount rate using LegacyDec (deterministic fixed-point arithmetic)
    remoteDec := math.LegacyNewDec(int64(remoteTrustLevel))
    discountedDec := remoteDec.Mul(params.TrustDiscountRate) // e.g., 3 * 0.5 = 1.5
    discounted := uint32(discountedDec.TruncateInt64())      // 1.5 → 1

    // Cap at peer-specific limit
    if discounted > policy.MaxTrustCredit {
        discounted = policy.MaxTrustCredit
    }

    // Cap at global limit
    if discounted > params.GlobalMaxTrustCredit {
        discounted = params.GlobalMaxTrustCredit
    }

    return discounted
}
```

With default parameters (`trust_discount_rate = 0.5`, `global_max_trust_credit = 1`):
- Remote NEW (0) → local credit 0
- Remote PROVISIONAL (1) → local credit 0
- Remote ESTABLISHED (2) → local credit 1 (PROVISIONAL equivalent)
- Remote TRUSTED (3) → local credit 1 (capped)
- Remote CORE (4) → local credit 1 (capped)

This means cross-chain reputation can provide at most a PROVISIONAL-equivalent signal. Full trust must be earned locally.

### 10.2. Content Rate Limiting

Rate limits are enforced at two levels using a **sliding window** model:

**Global per-block limit** (`max_inbound_per_block`):
- Tracked in transient store (reset each block automatically)
- Incremented on each `MsgSubmitFederatedContent` and each `OnRecvPacket` for content
- If the counter reaches `max_inbound_per_block`, subsequent content submissions in the same block are rejected with `ErrRateLimitExceeded`

**Per-peer sliding window** (`inbound_rate_limit_per_epoch`):
- Tracked in persistent `RateLimitCounters` store, keyed by `(peer_id, window_start)`
- `window_start` is computed as `block_time - (block_time % rate_limit_window)` where `rate_limit_window` is a governance parameter (default: 86400 seconds / 24 hours)
- On each inbound content item, the counter for the current window AND the previous window are consulted:
  ```
  current_count = RateLimitCounters[(peer_id, current_window_start)]
  prev_count = RateLimitCounters[(peer_id, prev_window_start)]
  elapsed_fraction = (block_time - current_window_start) / rate_limit_window
  effective_count = current_count + prev_count * (1 - elapsed_fraction)
  ```
- If `effective_count >= inbound_rate_limit_per_epoch`, reject with `ErrRateLimitExceeded`
- This sliding window prevents the epoch boundary burst attack: submitting N items at 23:59:59 and N more at 00:00:00 is caught because the previous window's count is still weighted.

**Implementation note (consensus determinism):** The `effective_count` computation involves fractional arithmetic, but `RateLimitCounters` stores `uint64`. To avoid precision loss and ensure all nodes compute identical results, use integer-only arithmetic: `effective_count = current_count + (prev_count * remaining_seconds) / window_seconds` where `remaining_seconds = rate_limit_window - (block_time - current_window_start)`. This avoids floating-point entirely. All division is integer floor division (truncating). This matches the `cosmossdk.io/math.LegacyDec` pattern used elsewhere in the codebase for deterministic fixed-point arithmetic.

**Stale counter cleanup**: EndBlocker Phase 13 removes counters older than 2 windows.

### 10.3. Outbound Rate Limiting

Outbound rate limits prevent a single chain from flooding IBC channels or being used as a spam amplifier.

**Global per-block limit** (`max_outbound_per_block`):
- Tracked in transient store (reset each block automatically)
- Incremented on each `MsgFederateContent` that successfully sends a `ContentPacket`
- If the counter reaches `max_outbound_per_block`, subsequent federation requests in the same block are rejected with `ErrRateLimitExceeded`

**Per-peer sliding window** (`outbound_rate_limit_per_epoch`):
- Uses the same sliding window model as inbound rate limiting (Section 10.2)
- Tracked in persistent `OutboundRateLimitCounters` store, keyed by `(peer_id, window_start)`
- Prevents a burst of federation requests to a single peer at epoch boundaries

Outbound rate limits do NOT apply to `MsgAttestOutbound` — that message records content already published by a bridge and does not generate IBC packets.

### 10.4. Bridge Slashing

Bridge operators can be slashed for:
- Submitting fabricated content (content that doesn't exist on the source)
- Spamming (excessive submissions beyond rate limits)
- Protocol violations (malformed data, incorrect translations)
- Relaying content from blocked identities

Slashing is a manual Operations Committee action (not automated). The committee investigates reports and decides severity. Slash amount is capped at the operator's remaining stake.

**Auto-revocation**: If slashing drops the operator's stake below `min_bridge_stake`, the bridge enters UNBONDING status (see Section 6.8). The remaining stake stays locked for `bridge_unbonding_period` so it can still be slashed for additional misbehavior discovered during the unbonding window. After unbonding completes, the operator must wait `bridge_revocation_cooldown` before re-registering.

---

## 11. Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrPeerNotFound` | 2300 | Peer ID does not exist |
| `ErrPeerAlreadyExists` | 2301 | Peer ID already registered |
| `ErrPeerNotActive` | 2302 | Peer is not in ACTIVE status |
| `ErrPeerTypeMismatch` | 2303 | Operation not valid for this peer type |
| `ErrInvalidPeerID` | 2304 | Peer ID format validation failed |
| `ErrBridgeNotFound` | 2305 | Bridge operator not registered for this peer |
| `ErrBridgeAlreadyExists` | 2306 | Bridge operator already registered for this peer |
| `ErrBridgeNotActive` | 2307 | Bridge is suspended or revoked |
| `ErrInsufficientStake` | 2308 | Below minimum bridge stake requirement |
| `ErrMaxBridgesExceeded` | 2309 | Peer has reached max bridge operators |
| `ErrContentTypeNotAllowed` | 2310 | Content type not in peer policy |
| `ErrRateLimitExceeded` | 2311 | Inbound rate limit exceeded |
| `ErrIdentityBlocked` | 2312 | Remote identity is blocked |
| `ErrIdentityLinkExists` | 2313 | Identity link already exists for this peer |
| `ErrIdentityLinkNotFound` | 2314 | No identity link for this peer |
| `ErrContentNotFound` | 2315 | Federated content ID not found |
| `ErrAttestationNotFound` | 2316 | Reputation attestation not found |
| `ErrReputationNotSupported` | 2317 | Reputation queries not supported for this peer type |
| `ErrNotAuthorized` | 2318 | Sender not authorized for this action |
| `ErrInvalidSigner` | 2319 | Non-governance signer for UpdateParams |
| `ErrSlashExceedsStake` | 2320 | Slash amount exceeds operator's remaining stake |
| `ErrContentTooLarge` | 2321 | Content body exceeds max_content_body_size |
| `ErrRemoteIdentityAlreadyClaimed` | 2322 | Another local address already claims this remote identity |
| `ErrMaxIdentityLinksExceeded` | 2323 | Local address has reached max_identity_links_per_user |
| `ErrCooldownNotElapsed` | 2324 | Bridge revocation cooldown has not elapsed |
| `ErrInvalidParamValue` | 2325 | Operational or governance param outside valid range |
| `ErrContentUriTooLarge` | 2326 | Content URI exceeds max_content_uri_size |
| `ErrMetadataTooLarge` | 2327 | Protocol metadata exceeds max_protocol_metadata_size |
| `ErrDuplicateContent` | 2328 | Content with same hash already exists for this peer |
| `ErrNoPendingChallenge` | 2329 | No pending identity challenge for this address/peer |
| `ErrChallengeExpired` | 2330 | Identity challenge has expired |
| `ErrUnknownContentType` | 2331 | Content type not in known_content_types registry |
| `ErrPeerCleanupInProgress` | 2332 | Peer removal cleanup still in progress, cannot re-register |
| `ErrBridgeNotOwnedBySigner` | 2333 | Signer is not the bridge operator |
| `ErrInvalidStakeDenom` | 2334 | Stake denomination must be uspark |
| `ErrVerifierNotFound` | 2335 | Address is not a registered verifier |
| `ErrVerifierNotActive` | 2336 | Verifier bond status is not NORMAL or RECOVERY |
| `ErrInsufficientVerifierBond` | 2337 | Verifier bond too low for this operation |
| `ErrBondCommitted` | 2338 | Cannot unbond — bond committed against pending challenges |
| `ErrDemotionCooldown` | 2339 | Demotion cooldown has not elapsed |
| `ErrVerifierOverturnCooldown` | 2340 | Verifier in overturn cooldown, cannot verify |
| `ErrContentNotPendingVerification` | 2341 | Content is not in PENDING_VERIFICATION status |
| `ErrVerificationWindowExpired` | 2342 | Verification window has expired |
| `ErrContentNotVerified` | 2343 | Content is not in VERIFIED status (cannot challenge) |
| `ErrChallengeWindowExpired` | 2344 | Challenge window has expired |
| `ErrSelfChallenge` | 2345 | Challenger cannot be the verifier or submitting operator |
| `ErrTrustLevelInsufficient` | 2346 | Sender does not meet minimum trust level |
| `ErrSelfArbiter` | 2347 | Submitting operator cannot arbitrate their own content |
| `ErrNotChallengeParty` | 2348 | Escalation signer is not the challenger or verifier |
| `ErrNoAutoResolutionToEscalate` | 2349 | Content has no pending auto-resolution to escalate |
| `ErrEscalationWindowExpired` | 2350 | Escalation window has passed |
| `ErrContentHashRequired` | 2351 | Content hash is required (must be SHA-256 of full source content) |
| `ErrSelfVerification` | 2352 | Verifier cannot verify content submitted by their own bridge operator address |
| `ErrChallengeCooldownActive` | 2353 | Challenge cooldown has not elapsed since last rejected challenge on this content |

---

## 12. Events

| Event | Attributes | Trigger |
|-------|------------|---------|
| `peer_registered` | peer_id, type, display_name, registered_by | New peer registered |
| `peer_activated` | peer_id | Peer transitioned to ACTIVE |
| `peer_suspended` | peer_id, reason | Peer suspended |
| `peer_resumed` | peer_id | Peer resumed |
| `peer_removed` | peer_id, reason | Peer removed |
| `peer_cleanup_complete` | peer_id | All data for removed peer cleaned up |
| `peer_policy_updated` | peer_id, updated_by | Peer policy changed |
| `bridge_registered` | operator, peer_id, protocol | Bridge operator registered |
| `bridge_revoked` | operator, peer_id, reason | Bridge operator revoked |
| `bridge_auto_revoked` | operator, peer_id, reason | Bridge auto-revoked (stake below minimum) |
| `bridge_slashed` | operator, peer_id, amount, reason | Bridge operator slashed |
| `bridge_updated` | operator, peer_id, updated_fields | Bridge operator metadata updated |
| `bridge_self_unbonded` | operator, peer_id | Bridge operator voluntarily initiated unbonding |
| `bridge_stake_topped_up` | operator, peer_id, amount, new_total | Bridge operator added stake |
| `bridge_unbonding_complete` | operator, peer_id, stake_returned | Bridge unbonding finished, stake returned |
| `bridge_inactive_warning` | operator, peer_id, last_submission_at | Bridge operator inactive |
| `bridge_stake_insufficient` | operator, peer_id, stake, min_required | Bridge stake below current minimum |
| `federated_content_received` | id, peer_id, content_type, creator_identity | Inbound content stored |
| `federated_content_moderated` | id, new_status, reason, moderated_by | Content moderation action |
| `content_federated` | peer_id, content_type, local_content_id, creator | Outbound IBC content sent |
| `outbound_attested` | peer_id, content_type, local_content_id | Outbound bridge attestation |
| `identity_linked` | local_address, peer_id, remote_identity | Identity link created |
| `identity_verified` | local_address, peer_id, remote_identity | Identity link verified |
| `identity_unlinked` | local_address, peer_id | Identity link removed |
| `identity_link_revoked` | local_address, peer_id, remote_identity | Identity link revoked (peer removed) |
| `identity_link_expired` | local_address, peer_id, remote_identity | Unverified identity link pruned by TTL |
| `identity_verification_timeout` | local_address, peer_id | IBC verification packet timed out |
| `identity_verification_failed` | local_address, peer_id | Verification failed after max retries |
| `identity_challenge_received` | claimed_address, claimant_address, claimant_chain_peer_id | Challenge stored, awaiting user confirmation |
| `identity_challenge_confirmed` | claimed_address, claimant_chain_peer_id | User confirmed identity challenge |
| `identity_challenge_expired` | claimed_address, claimant_chain_peer_id | Pending challenge pruned by TTL |
| `identity_confirmation_timeout` | claimed_address, claimant_chain_peer_id | Confirmation packet timed out |
| `reputation_attested` | local_address, peer_id, remote_trust_level, local_trust_credit | Reputation attestation stored |
| `reputation_expired` | local_address, peer_id | Attestation expired and pruned |
| `reputation_query_timeout` | peer_id, queried_address, requester | IBC reputation query timed out |
| `content_send_timeout` | peer_id, content_type, remote_content_id | IBC content packet timed out |
| `operational_params_updated` | updated_by, changed_fields | Operational params changed |
| `peer_policy_auto_updated` | peer_id, removed_content_types | Policy auto-cleaned after known_content_types change |
| `verifier_bonded` | address, amount, bond_status | Verifier bonded DREAM |
| `verifier_unbonded` | address, amount, bond_status | Verifier unbonded DREAM |
| `verifier_demoted` | address, remaining_bond | Verifier bond dropped below recovery threshold |
| `content_verified` | content_id, verifier, peer_id | Content independently verified |
| `content_disputed` | content_id, verifier, peer_id, operator_hash, verifier_hash | Hash mismatch, jury initiated |
| `content_verification_expired` | content_id, peer_id, operator | Verification window expired without verification |
| `verification_challenged` | content_id, challenger, verifier | VERIFIED content challenged |
| `challenge_upheld` | content_id, verifier, challenger, slash_amount | Verifier wrong, slashed |
| `challenge_rejected` | content_id, verifier, challenger | Verifier right, challenger loses fee |
| `challenge_timeout` | content_id, verifier, challenger | No jury consensus |
| `verifier_slashed` | address, amount, remaining_bond, bond_status | Verifier DREAM slashed |
| `verifier_cooldown_applied` | address, cooldown_until, consecutive_overturns | Escalating cooldown after overturn |
| `verifier_dream_reward_paid` | address, amount | DREAM reward paid to verifier (NORMAL status) |
| `verifier_dream_reward_auto_bonded` | address, auto_bonded, payout, new_bond | DREAM reward auto-bonded in RECOVERY |
| `verifier_bond_restored` | address, new_bond | Verifier bond restored to NORMAL via auto-bonding |
| `arbiter_hash_submitted` | content_id, content_hash, is_identified | Arbiter hash received (operator address included if identified, omitted if anonymous) |
| `arbiter_quorum_reached` | content_id, quorum_hash, matching_count | Quorum of matching hashes reached |
| `challenge_auto_resolved` | content_id, verdict, quorum_hash | Challenge auto-resolved by anonymous quorum |
| `challenge_escalated` | content_id, escalated_by | Auto-resolution escalated to human jury |
| `arbiter_resolution_expired` | content_id | No quorum reached, escalating to jury |
| `challenge_cancelled_peer_removed` | content_id, peer_id, challenger_refunded | Active dispute cancelled due to peer removal |

---

## 13. Default Parameters

| Parameter | Default | Rationale |
|-----------|---------|-----------|
| `min_bridge_stake` | 1000 SPARK | Meaningful stake for accountability without being prohibitive |
| `max_bridges_per_peer` | 5 | Redundancy without fragmentation |
| `bridge_revocation_cooldown` | 7 days | Prevent slash-then-re-register cycling |
| `bridge_unbonding_period` | 14 days | Slash window after revocation — longer than cooldown to ensure misbehavior can be investigated |
| `known_content_types` | `["blog_post", "blog_reply", "forum_thread", "forum_reply", "collection"]` | Exhaustive registry of valid content types; prevents typo-based silent failures |
| `max_inbound_per_block` | 50 | Prevent state bloat from high-volume peers |
| `max_outbound_per_block` | 50 | Prevent outbound IBC channel flooding |
| `max_content_body_size` | 4096 bytes | Enough for a meaningful preview; full content via `content_uri` |
| `max_content_uri_size` | 2048 bytes | Standard maximum URL length |
| `max_protocol_metadata_size` | 8192 bytes | Reasonable JSON metadata cap |
| `content_ttl` | 90 days | Balance between useful history and state management |
| `attestation_ttl` | 30 days | Reputation is dynamic; attestations must be refreshed |
| `global_max_trust_credit` | 1 | At most PROVISIONAL equivalent from any remote chain |
| `trust_discount_rate` | 0.5 | Halve the remote trust level before applying caps |
| `max_identity_links_per_user` | 10 | Generous but bounded |
| `unverified_link_ttl` | 30 days | Unverified links should not persist indefinitely |
| `challenge_ttl` | 7 days | Reasonable window for remote user to confirm identity |
| `bridge_inactivity_threshold` | 100 epochs | ~100 days before warning (assumes 1-day epochs) |
| `ibc_port` | "federation" | Standard port for this module |
| `ibc_channel_version` | "federation-1" | Versioned for upgrade compatibility |
| `ibc_packet_timeout` | 10 minutes | Reasonable timeout for cross-chain packets |
| `max_prune_per_block` | 100 | Bound EndBlocker gas while keeping cleanup responsive |
| `rate_limit_window` | 24 hours | Matches natural daily cycle for rate limit accounting |
| `min_verifier_trust_level` | 2 (ESTABLISHED) | Must have demonstrated community standing |
| `min_verifier_bond` | 500 DREAM | Meaningful accountability, consistent with forum sentinel model |
| `verifier_recovery_threshold` | 250 DREAM | Half of min bond — gives recovery runway |
| `verifier_slash_amount` | 50 DREAM | Per-overturn penalty, allows ~10 mistakes before demotion |
| `verification_window` | 24 hours | Matches rate limit epoch; content visible within a day |
| `challenge_window` | 7 days | Enough time to spot-check verified content |
| `challenge_fee` | 250 SPARK | Deters frivolous challenges without being prohibitive |
| `challenge_jury_deadline` | 14 days | Matches forum appeal deadline |
| `verifier_demotion_cooldown` | 7 days | Matches forum sentinel demotion cooldown |
| `verifier_overturn_base_cooldown` | 24 hours | Escalates 2x per consecutive overturn, capped at 7 days |
| `upheld_to_reset_overturns` | 3 | Consecutive correct verifications to reset overturn counter |
| `min_epoch_verifications` | 3 | Must do real work to earn rewards |
| `min_verifier_accuracy` | 0.8 | Higher than forum sentinel (0.7) because verification is more objective |
| `operator_reward_share` | 0.6 | 60% operators / 40% verifiers — operators have higher infra costs |
| `verifier_dream_reward` | 5 DREAM | Enough for ~10 epoch recovery from one slash (50 DREAM) |
| `max_verifier_dream_mint_per_epoch` | 100 DREAM | Caps inflation: supports up to 20 verifiers at full reward |
| `arbiter_quorum` | 3 | Minimum for meaningful consensus; odd number avoids ties |
| `arbiter_resolution_window` | 24 hours | Matches verification_window; most accessible content resolved same day |
| `arbiter_escalation_window` | 48 hours | Enough time for losing party to review and decide whether to escalate |
| `escalation_fee` | 100 SPARK | Low enough to not block legitimate disputes, high enough to deter spam |
| `challenge_cooldown` | 7 days | Matches challenge_window; prevents re-challenge immediately after resolution |

---

## 14. Security Considerations

### 14.1. Sovereignty Guarantees

The module is designed with hard guarantees against governance capture from external chains:

- **No shared governance**: There is no mechanism for peers to vote on, propose, or influence local governance decisions. Federation is purely for content and reputation exchange.
- **No token bridging**: SPARK and DREAM cannot cross chain boundaries. No IBC transfer, no wrapped tokens, no shared pools.
- **No binding reputation**: Cross-chain reputation is advisory. A CORE member on Chain B has no automatic rights on Chain A. Local invitation requirements are never bypassed.
- **Unilateral control**: Any chain can suspend or remove any peer instantly, without coordination or approval from the peer.
- **Policy independence**: Each chain sets its own content types, rate limits, trust caps, and moderation rules per peer. No "standard federation agreement" is imposed.

### 14.2. Bridge Trust Model

Bridges introduce a trust assumption (operator honesty) that IBC avoids. Mitigations:

- **Staking**: Operators stake SPARK, creating economic accountability
- **Slashing**: Operations Committee can slash for misbehavior after investigation. Slashed SPARK is burned (not sent to community pool or validators), eliminating perverse incentives for the committee that decides to slash.
- **Auto-revocation**: Slashing below `min_bridge_stake` immediately revokes the bridge, preventing undercapitalized operators from continuing
- **Unbonding period**: After revocation, stake remains locked for `bridge_unbonding_period` (14 days). The Operations Committee can still slash during this window if misbehavior is discovered post-revocation. This prevents the escape-before-slash attack where an operator revokes their own bridge to retrieve their stake before a pending investigation concludes.
- **Cooldown**: `bridge_revocation_cooldown` prevents revoked operators from immediately re-registering (stops slash-and-re-register cycling)
- **Competition**: Multiple bridges per peer means no single operator is a bottleneck
- **Auditability**: All bridge submissions are on-chain with full provenance. OutboundAttestations create a verifiable record of what was published.
- **Separation**: Bridge operators cannot modify policies, register peers, or perform governance actions
- **Session keys**: Operators can use x/session to limit daemon key exposure (see Section 3.7)
- **Stake monitoring**: EndBlocker emits warning events if governance raises `min_bridge_stake` above an existing operator's stake, giving the Operations Committee visibility without causing automatic disruption

### 14.3. Reputation Gaming

An attacker could create a permissive Spark Dream chain with inflated reputation to gain trust credit on a target chain. Mitigations:

- **Heavy discounting**: Default 50% discount + cap at PROVISIONAL equivalent
- **Per-peer caps**: Each chain sets max trust credit per peer independently
- **Manual peer registration**: Commons Council must explicitly register each peer — no automatic discovery
- **Time-limited**: Attestations expire and must be refreshed
- **Advisory only**: Trust credit doesn't bypass local requirements (invitations, staking)

### 14.4. Content Spam via Federation

Mitigations:
- **Sliding window rate limits**: Per-peer and global limits using sliding window (Section 10.2) prevent epoch-boundary burst attacks
- Bridge operator accountability (submissions tracked, slashable)
- Content type allowlists (not denylists)
- `require_review` flag for untrusted peers
- Operations Committee can block specific remote identities
- **Field size limits**: `max_content_body_size`, `max_content_uri_size`, `max_protocol_metadata_size` prevent oversized content from consuming disproportionate storage. Applied to both bridge submissions and IBC packets.
- **Efficient pruning**: `ContentExpirationQueue` ensures O(expired_count) cleanup, not O(total_content)

### 14.5. Identity Spoofing

An attacker claims a false identity link. Mitigations:
- IBC links use a two-phase verification protocol where the remote user must sign a `MsgConfirmIdentityLink` transaction on the remote chain, cryptographically proving key ownership (see Section 8.3)
- Bridge-verified links rely on operator honesty (staked)
- Unverified links are clearly marked — consumers should treat them differently
- Users can only link their own local address
- **Unique remote identity constraint**: Each `(peer_id, remote_identity)` can only be claimed by one local address, preventing identity collision in reverse lookups
- **Per-user link cap**: `max_identity_links_per_user` prevents link explosion attacks
- **Unverified link TTL**: Stale unverified claims are automatically pruned

### 14.6. Verification and Challenge Gaming

The verification system is a multi-layered accountability stack. Potential attacks and mitigations:

**Verifier rubber-stamping (verifying without actually checking):**
- Challenged by any ESTABLISHED+ member or competing bridge operator
- Verifier DREAM bond at risk (50 DREAM per overturn)
- Escalating cooldowns prevent repeat offenders from continuing (24h → 48h → 96h → 7d cap)
- Demotion below `verifier_recovery_threshold` removes all verification privileges
- Accuracy below `min_verifier_accuracy` (80%) disqualifies from x/split rewards

**Challenger spoofing content to frame a verifier:**
- Challengers must be ESTABLISHED+ members (reputation at stake beyond the challenge fee)
- Edit timestamp rule (Section 3.9.6): if external content was modified after `verified_at`, verifier is not faulted
- Losing a challenge costs 50% of `challenge_fee` (125 SPARK) — meaningful deterrent
- Arbiter quorum independently fetches the content, so a challenger who submits a false hash is outvoted by honest arbiters

**Operator-verifier collusion (operator submits fabricated content, friendly verifier rubber-stamps):**
- Challenges from competing bridge operators are the primary accountability mechanism — they already run infrastructure monitoring the same external platform
- Arbiter quorum (default 3) requires majority consensus — even if operator and verifier are both members and submit as anonymous arbiters, they can only contribute 2 of 3 needed votes
- Collusion requires both SPARK (operator) and DREAM (verifier) at risk simultaneously

**Arbiter quorum manipulation:**
- Anonymous path: ZK proof ties to unique member identity via trust tree; scoped nullifier prevents double-submission; Sybil requires multiple ESTABLISHED+ invitations
- Identified path: bridge operators registered with SPARK stake and Operations Committee approval; creating fake operators requires multiple `min_bridge_stake` deposits
- Mixed quorums (identified + anonymous) require compromising both categories simultaneously
- Deterministic hash comparison means all honest arbiters produce the same result — dishonest submissions are mathematically outvoted

**Escalation abuse (losing party always escalates to delay resolution):**
- `escalation_fee` (100 SPARK) makes routine escalation expensive
- Escalation fee returned only if jury overturns the auto-verdict — frivolous escalation loses the fee
- Most challenges resolve in Phase 1 (24h) — escalation is the exception, not the norm

**Verification window boundary (implementation note):**
Cosmos SDK processes all `DeliverTx` (message execution) before running `EndBlocker`. A verifier submitting `MsgVerifyContent` in the same block where the verification window expires will succeed — the message executes before Phase 6 sets the content to HIDDEN. This is deterministic behavior, not a race condition. No special handling needed.

**IBC content trust model:**
IBC content enters as ACTIVE without verifier confirmation. This is an intentional trust model decision, not a gap. The trust chain is: Commons Council registers the peer → IBC light client proves the sending chain committed the content packet → the sending chain's validator set attested to the packet's validity. A malicious Spark Dream chain could send fabricated content, but: (a) Commons Council must have explicitly registered that chain as a peer, (b) the Operations Committee can suspend/remove any peer at any time, (c) `MsgModerateContent` can hide/reject any individual piece of IBC content, (d) the `require_review` flag on PeerPolicy can force all inbound IBC content to start HIDDEN. The verification system is for bridge peers specifically because bridges introduce a weaker trust model (operator honesty) that IBC's light client proofs avoid.

### 14.7. Operational Parameter Abuse

An Operations Committee member could attempt to weaken security by adjusting operational params (e.g., setting `global_max_trust_credit = 4` or `max_inbound_per_block = 10000`).

Mitigations:
- **Validation ranges**: All operational params are validated against strict bounds (see Section 4.13 table). Values outside the allowed range are rejected.
- **Audit events**: Every `MsgUpdateOperationalParams` emits an event listing old and new values for each changed field, creating a transparent audit trail.
- **Governance-only fields**: Critical security parameters (`min_bridge_stake`, `bridge_revocation_cooldown`, `max_identity_links_per_user`) can only be changed via governance proposal, not by the Operations Committee.

### 14.8. Outbound Attestation Integrity

`MsgAttestOutbound` (Section 6.17) creates an audit trail of content published to bridge peers, but attestations are **self-reported by the bridge operator**. The module cannot verify that the operator actually published the content, or that the published content matches what was attested. Mitigations:

- **Audit, not proof**: Outbound attestations serve as a verifiable claim, not a proof. The Operations Committee can compare attestations against the actual state of the external platform (e.g., checking if a claimed ActivityPub post exists at the expected URI).
- **Reputation consequence**: An operator caught making false attestations (claiming publications that don't exist) can be slashed via `MsgSlashBridge`.
- **No content guarantee**: Bridge peers should independently verify content they receive via their own protocol mechanisms (e.g., ActivityPub HTTP signatures). The attestation is for the *sending chain's* audit trail, not the receiving peer's trust model.

### 14.9. Bridge Stake Economics

Bridge operator stakes are escrowed in the federation module account, **not delegated via x/staking**. This means:

- **No staking rewards**: Escrowed SPARK does not earn inflation rewards or fee distributions. This is an intentional design trade-off — staking rewards would complicate slashing mechanics and create misaligned incentives (operators would be rewarded simply for holding a bridge position, regardless of service quality).
- **Opportunity cost**: Operators forgo staking yield on their escrowed SPARK. The `min_bridge_stake` should be set considering this cost — too high and no one operates bridges, too low and there's insufficient accountability.
- **Operator compensation**: Bridge operators receive automatic SPARK compensation via x/split, proportional to their **verified** submissions per epoch (see Section 3.9). Only content independently confirmed by a federation verifier counts. This covers infrastructure costs and offsets staking opportunity cost. The compensation rate is governance-controlled via the x/split Federation Operations allocation.

### 14.10. IBC Gas Cost Model

IBC packet processing (both sending and receiving) follows the standard Cosmos IBC gas model:

- **Sending chain**: The signer of the message that triggers packet creation (`MsgFederateContent`, `MsgRequestReputationAttestation`, `MsgLinkIdentity`) pays gas for packet commitment and event emission.
- **Receiving chain**: The IBC relayer pays gas for `MsgRecvPacket`, which covers `OnRecvPacket` execution including state writes (storing `FederatedContent`, `ReputationAttestation`, etc.). Relayers recoup costs through their own incentive model.
- **Acknowledgement**: The relayer pays gas for `MsgAcknowledgement` on the sending chain, which covers `OnAcknowledgementPacket` execution.
- **Content storage cost**: Inbound `ContentPacket` processing involves multiple store writes (primary record + up to 5 indexes). The `max_content_body_size` and field size limits bound the per-packet gas cost.

---

## 15. Integration Points

### 15.1. x/commons

```go
// Check council authorization for peer registration
k.commonsKeeper.HasMember(ctx, "Commons Council", addr)

// Check Operations Committee for policy/bridge management
k.commonsKeeper.IsCouncilAuthorized(ctx, addr, "commons", "operations")

// Get council policy address for authorization checks
group, _ := k.commonsKeeper.GetGroup(ctx, "Commons Council")
```

### 15.2. x/rep

```go
// Look up member reputation for outbound attestation responses
member, _ := k.repKeeper.GetMember(ctx, address)
trustLevel := member.TrustLevel
reputations := k.repKeeper.GetReputationsByMember(ctx, address)

// Verifier DREAM bonding (bond held in federation module, tracked via x/rep)
k.repKeeper.TransferDream(ctx, verifierAddr, federationModuleAddr, bondAmount)
k.repKeeper.BurnDream(ctx, federationModuleAddr, slashAmount)

// Jury system for verification disputes and challenges
k.repKeeper.CreateAppealInitiative(ctx, initiativeParams)  // Creates jury voting initiative
// OnInitiativeFinalized callback delivers verdict to x/federation
```

### 15.3. x/name

```go
// Resolve local name for outbound content creator attribution
name, _ := k.nameKeeper.ReverseResolve(ctx, creatorAddress)
```

### 15.4. x/bank

```go
// Escrow bridge operator stake
k.bankKeeper.SendCoinsFromAccountToModule(ctx, operator, "federation", stake)

// Return stake on revocation
k.bankKeeper.SendCoinsFromModuleToAccount(ctx, "federation", operator, stake)

// Slash: burn the slashed amount (reduces SPARK supply, benefits all holders equally)
k.bankKeeper.BurnCoins(ctx, "federation", slashAmount)
```

### 15.5. x/split (reverse dependency — x/split depends on x/federation, not vice versa)

```go
// x/split imports x/federation's keeper interface (read-only) to compute distribution weights.
// x/federation does NOT import or call x/split — it only exposes these query methods:
k.federationKeeper.GetActiveOperatorWeights(ctx)  // Returns []OperatorWeight{address, peer_id, weight}
k.federationKeeper.GetActiveVerifierWeights(ctx)   // Returns []VerifierWeight{address, weight}
```

x/split calls these each epoch from its own BeginBlocker to distribute the Federation Operations allocation. Operators receive `operator_reward_share` (default 60%) based on verified submissions. Verifiers receive the remainder (40%) based on verification count and accuracy (see Section 3.9). x/split handles the actual SPARK transfer from Community Pool. This is a **read-only, one-directional** call — no circular dependency.

### 15.6. Content Modules (Blog, Forum, Collect)

No direct keeper dependency. Federation consumes events emitted by content modules:
- `EventCreatePost` (blog)
- `EventCreateThread` (forum)
- `EventCreateCollection` (collect)

Bridges watch these events and decide what to federate based on peer policies.

### 15.7. x/reveal (NOT Federated)

Reveal contributions are explicitly excluded from federation. Reveal content involves contributor bonds, holdback mechanics, tranche-based payouts, and IP licensing (the `initial_license`/`final_license` fields) that are inherently local to the chain. The PeerPolicy `outbound_content_types` allowlist should never include reveal-related types. This is enforced by validation: `MsgUpdatePeerPolicy` rejects policies that include `"reveal_proposal"` or `"reveal_tranche"` in `outbound_content_types` or `inbound_content_types`.

### 15.8. x/shield

```go
// Anonymous challenge resolution: x/federation registers as a ShieldAware module
// so that MsgShieldedExec can dispatch MsgSubmitArbiterHash to x/federation
k.shieldKeeper.RegisterShieldAwareModule("/sparkdream.federation.v1.", app.FederationKeeper)

// x/shield handles:
// - ZK proof verification (trust_level >= ESTABLISHED)
// - Nullifier checking (scoped to "federation_arbiter:<content_id>")
// - Module-paid gas (arbiter pays nothing, preserving anonymity)
// - Inner message dispatch to x/federation's MsgSubmitArbiterHash handler
```

x/federation implements the `ShieldAwareModule` interface so x/shield can route `MsgSubmitArbiterHash` after verifying the ZK proof and nullifier. The federation module only sees the hash and nullifier — never the submitter's identity.

---

## 16. CLI

The module uses **autocli** for all transaction and query commands. No custom CLI handlers are needed — autocli generates commands from the proto service definitions.

**Transaction commands** (generated from `Msg` service):
```
sparkdreamd tx federation register-peer --authority ... --peer-id ... --display-name ... --type ...
sparkdreamd tx federation remove-peer --authority ... --peer-id ... --reason ...
sparkdreamd tx federation suspend-peer --authority ... --peer-id ... --reason ...
sparkdreamd tx federation resume-peer --authority ... --peer-id ...
sparkdreamd tx federation update-peer-policy --authority ... --peer-id ... --policy ...
sparkdreamd tx federation register-bridge --authority ... --operator ... --peer-id ... --protocol ...
sparkdreamd tx federation revoke-bridge --authority ... --operator ... --peer-id ... --reason ...
sparkdreamd tx federation slash-bridge --authority ... --operator ... --peer-id ... --amount ... --reason ...
sparkdreamd tx federation update-bridge --authority ... --operator ... --peer-id ... --endpoint ...
sparkdreamd tx federation unbond-bridge --peer-id ...
sparkdreamd tx federation top-up-bridge-stake --peer-id ... --amount ...
sparkdreamd tx federation link-identity --peer-id ... --remote-identity ...
sparkdreamd tx federation confirm-identity-link --claimant-chain-peer-id ...
sparkdreamd tx federation unlink-identity --peer-id ...
sparkdreamd tx federation submit-federated-content --peer-id ... --content-type ... --body ...
sparkdreamd tx federation federate-content --peer-id ... --content-type ... --local-content-id ... --title ... --body ...
sparkdreamd tx federation attest-outbound --peer-id ... --content-type ... --local-content-id ...
sparkdreamd tx federation moderate-content --authority ... --content-id ... --new-status ... --reason ...
sparkdreamd tx federation request-reputation-attestation --peer-id ... --remote-address ...
sparkdreamd tx federation update-params --authority ... --params ...
sparkdreamd tx federation bond-verifier --amount ...
sparkdreamd tx federation unbond-verifier --amount ...
sparkdreamd tx federation verify-content --content-id ... --content-hash ...
sparkdreamd tx federation challenge-verification --content-id ... --content-hash ... --evidence ...
sparkdreamd tx federation submit-arbiter-hash --content-id ... --content-hash ...
sparkdreamd tx federation escalate-challenge --content-id ...
sparkdreamd tx federation update-operational-params --authority ... --operational-params ...
```

**Query commands** (generated from `Query` service):
```
sparkdreamd query federation get-peer [peer-id]
sparkdreamd query federation list-peers
sparkdreamd query federation get-peer-policy [peer-id]
sparkdreamd query federation get-bridge-operator [address] [peer-id]
sparkdreamd query federation list-bridge-operators
sparkdreamd query federation get-federated-content [id]
sparkdreamd query federation list-federated-content
sparkdreamd query federation get-identity-link [local-address] [peer-id]
sparkdreamd query federation list-identity-links
sparkdreamd query federation resolve-remote-identity [peer-id] [remote-identity]
sparkdreamd query federation get-pending-identity-challenge [claimed-address] [peer-id]
sparkdreamd query federation list-pending-identity-challenges [claimed-address]
sparkdreamd query federation get-reputation-attestation [local-address] [peer-id]
sparkdreamd query federation list-outbound-attestations
sparkdreamd query federation get-verifier [address]
sparkdreamd query federation list-verifiers
sparkdreamd query federation get-verification-record [content-id]
sparkdreamd query federation params
```

---

## 17. Future Considerations

1. **ZK Reputation Proofs**: Allow members to prove trust level on a remote chain without revealing identity, using x/shield's existing ZK infrastructure. The flow would be: user generates a ZK proof using x/shield's `TRUST_TREE` domain proving `min_trust_level >= PROVISIONAL` without revealing identity → proof is sent via IBC → receiving chain verifies using x/shield's PLONK verifier → result stored as a `ZkReputationProof` (distinct from `ReputationAttestation`). This uses x/shield's existing circuit and verification infrastructure — no new circuit is needed. Trust tree roots can be shared via IBC light client state proofs.
2. **Cross-Chain Initiatives**: Shared projects across Spark Dream chains with independent DREAM budgets per chain, coordinated via IBC messaging
3. **ActivityPub Extensions**: Custom ActivityPub extensions for Spark Dream-specific features (reputation badges, conviction signals, trust level indicators)
4. **AT Protocol Lexicons**: Custom lexicon schemas for Spark Dream content types
5. **Federation Discovery**: Optional peer discovery protocol (still requires manual council approval to activate)
6. **Content Threading**: Federated replies and reactions (not just top-level content)
7. **Bridge SDK**: Reference implementation and SDK for bridge operators
8. **Sentinel Integration**: Allow x/forum sentinels to flag federated content for moderation via a keeper interface. Sentinel would call `federationKeeper.FlagContent(ctx, contentID, sentinelAddr, reason)` which sets status to HIDDEN. Appeals go through x/rep jury system.
9. **Multi-Hop Federation**: Chain A federates content from Chain B to Chain C (with full provenance chain)

---

## 18. File References

- Proto definitions: `proto/sparkdream/federation/v1/types.proto`, `tx.proto`, `query.proto`, `genesis.proto`
- Module config: `proto/sparkdream/federation/module/v1/module.proto`
- Keeper logic: `x/federation/keeper/`
- Types and errors: `x/federation/types/`
- Module setup: `x/federation/module/`
- IBC handler: `x/federation/keeper/ibc.go`
- EndBlocker: `x/federation/keeper/endblock.go`
- Integration tests: `test/federation/`
