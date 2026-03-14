# Technical Specification: `x/collect`

## 1. Abstract

The `x/collect` module provides a generalized on-chain collection management system. Users create **Collections** — curated, ordered groupings of **Items** that reference things both on-chain and off-chain.

The initial motivation is tracking NFT collections across chains, but the module is designed to handle any type of curated reference:
- **NFTs**: Cross-chain NFT portfolio tracking (ERC-721, CW-721, etc.)
- **Links**: Bookmarks, resource lists, reading lists
- **On-chain references**: Pointers to blog posts, forum threads, reveal contributions, or any module entity
- **Custom items**: Arbitrary typed references with user-defined metadata

Key principles:
- **Referential, not custodial**: Collections store metadata and pointers, not the assets themselves
- **Nomination-eligible**: Collections created during a season can be nominated for retroactive DREAM rewards via the `x/season` retroactive public goods funding system
- **Type-flexible**: A single Item model supports NFTs, URIs, on-chain references, and custom types via a `reference_type` discriminator
- **Economically sustainable**: Storage deposits, per-item fees, endorsement fees, and per-item spam taxes ensure that on-chain data is never a net cost to validators
- **True privacy**: Private collections use client-side encryption — all content is packed into a single opaque blob, revealing nothing about internal structure
- **Collaborative**: Members can share write access to collections via an on-chain collaborator list (requires `x/rep` membership)
- **Ephemeral by default for non-members**: Non-members can only create TTL collections — their state is self-cleaning. Permanence requires `x/rep` membership or sponsorship by a trusted member
- **Endorsement gateway for non-members**: Non-member collections start in PENDING state and require member endorsement before becoming publicly visible. This keeps discovery opt-in for adventurous members and prevents moderator burden from unknown content
- **Sponsorship pathway**: Trusted members can sponsor non-member collections for permanence. The non-member pays the full permanent deposit; the sponsor pays a small fee and vouches for quality. Two-party commitment creates a strong quality signal
- **Ephemeral or permanent for members**: Members can freely choose TTL or permanent collections; TTL deposits are refunded at expiry, permanent deposits are burned
- **Tiered capacity**: Collection limits scale with `x/rep` trust level — higher trust earns more capacity
- **Ordered**: Items have explicit positions for deliberate curation
- **Quality-rated**: Bonded curators rate public collections (up/down + descriptive tags), with challenge/appeals via x/rep jury
- **Community reactions**: Members can upvote (free) or downvote (25 SPARK burned) public collections and items, providing lightweight sentiment signals separate from expert curation. Owners can opt out via `community_feedback_enabled`
- **Sentinel-moderated**: x/forum sentinels can flag and hide inappropriate public collections and items, with the same bond-commitment and appeal system used in x/forum. Sentinel moderation applies to all public collections regardless of owner preferences

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/gov` | Authority for full parameter updates |
| `x/auth` | Address codec for bech32 conversion |
| `x/bank` | Storage deposit collection, per-item fees, endorsement fee escrow, per-item spam tax burns, deposit refunds, downvote cost burns |
| `x/commons` | Council/committee authorization for operational parameter updates; anonymous proposal/voting (replaces x/group) |
| `x/name` | Optional: resolve owner names for display |
| `x/rep` | Membership verification, trust level checks, per-item spam tax exemption, curator bonding, jury resolution, endorser DREAM staking, content conviction staking, author bonds, trust tree Merkle roots for ZK proof validation |
| `x/forum` | Sentinel bond system: sentinel status checks, bond commitment/release/slash for content moderation |
| `x/shield` | Unified privacy layer: all anonymous collection operations (creation, reactions) go through `MsgShieldedExec`. Owns ZK proof verification, nullifier management, TLE infrastructure, and module-paid gas. x/collect implements the `ShieldAware` interface to register compatible operations. See `docs/x-shield-spec.md` |

---

## 3. State Objects

### 3.1. Collection

A collection is a named, ordered container of items owned by a single address.

```protobuf
message Collection {
  uint64 id = 1;
  string owner = 2;

  // --- Public collection content fields (used when encrypted = false) ---
  string name = 3;
  string description = 4;
  string cover_uri = 5;
  repeated string tags = 6;

  // --- Private collection content (used when encrypted = true) ---
  bytes encrypted_data = 7;     // Single opaque blob (see Encryption Model)

  CollectionType type = 8;      // Plaintext even on private collections
  Visibility visibility = 9;    // PUBLIC or PRIVATE (immutable after creation)
  bool encrypted = 10;          // Immutable after creation; true = blob mode

  uint64 item_count = 11;
  uint32 collaborator_count = 12;

  int64 created_at = 13;
  int64 updated_at = 14;
  int64 expires_at = 15;        // 0 = permanent

  // --- Deposit tracking ---
  string deposit_amount = 16 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string item_deposit_total = 17 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // Sum of per-item deposits held (TTL only)
  bool deposit_burned = 18;     // True if deposits were burned (permanent collection)

  // --- Sponsorship ---
  string sponsored_by = 19;     // Address of member who sponsored permanence (empty if not sponsored)

  // --- Community feedback ---
  bool community_feedback_enabled = 20;  // Owner opt-in for reactions + curation (default true)

  // --- Moderation ---
  CollectionStatus status = 21;          // ACTIVE, HIDDEN (default ACTIVE)

  // --- Reactions (counter-only) ---
  uint64 upvote_count = 22;
  uint64 downvote_count = 23;

  // --- Non-member endorsement ---
  string endorsed_by = 24;              // Address of member who endorsed (empty if not endorsed)
  bool seeking_endorsement = 25;        // Non-member signals readiness for endorsement
  bool immutable = 26;                  // True after endorsement; unlocked when owner becomes member

  // --- Conviction-sustained state (anonymous collections only) ---
  bool conviction_sustained = 27;       // True if anonymous collection has entered conviction-sustained state

  // --- Cross-module conviction propagation ---
  uint64 initiative_id = 28;            // x/rep initiative referenced by this collection (0 = none, immutable)
}
```

### 3.1.1. Encryption Model (Blob)

Private collections use a **single encrypted blob** approach. All decryption happens offchain.

**Public collections** (`encrypted = false`): Use the structured fields (`name`, `description`, `cover_uri`, `tags`). The `encrypted_data` field is empty. The chain validates field lengths and tag counts.

**Private collections** (`encrypted = true`): The client packs all content (name, description, cover URI, tags, and any other metadata) into a single payload (JSON or protobuf), encrypts it with a symmetric key, and stores the ciphertext in `encrypted_data`. Structured content fields are left empty. The chain:
- Cannot interpret the content — it is a single opaque blob
- Cannot validate individual field lengths (only total blob size via `max_encrypted_data_size`)
- Reveals nothing about internal structure — only that a collection exists, its `type`, `item_count`, and approximate total size
- Does not store, manage, or verify encryption keys

For collaborative private collections, the owner shares the symmetric encryption key with collaborators through an off-chain side channel. Key management is entirely the frontend's responsibility.

**Constraint**: `encrypted = true` requires `visibility = PRIVATE`. The combination `encrypted = true` + `visibility = PUBLIC` is rejected (would place encrypted gibberish in public listings).

**Visibility and encryption are both immutable after creation.** Changing visibility from PRIVATE to PUBLIC would expose ciphertext. Changing from PUBLIC to PRIVATE would require retroactively encrypting all existing plaintext items. Changing `encrypted` would corrupt existing item data.

### 3.1.2. Fee Model (Storage Deposit + Per-Item Fees + Endorsement Fee)

All fees are denominated in **SPARK** because on-chain storage has a real hardware cost borne by validators. The fee model ensures stored data is never a net cost to the chain.

**Collection deposit** (`base_collection_deposit`): Paid at creation by all users.

**Per-item fee** (`per_item_deposit`): Paid each time an item is added. Scales linearly with collection size, ensuring large collections pay proportionally.

**Endorsement creation fee** (`endorsement_creation_fee`): Non-member collection creation fee (10 SPARK). Escrowed in the module account. On endorsement, 80% is sent to the endorser and 20% is burned. On deletion or auto-prune, refunded (minus `endorsement_deletion_burn_fraction`). Replaces the old per-collection spam tax with better incentive alignment.

**Per-item spam tax** (`per_item_spam_tax`): Additional fee for non-members on each item addition. Always burned. Ensures the non-refundable cost scales with the number of items stored.

The deposit fate depends on the collection type and membership:
- **TTL collections** (`expires_at > 0`): Collection deposit and per-item deposits are held in the module account and **refunded** on expiry, manual deletion, or item removal.
- **Permanent collections** (`expires_at = 0`): All deposits are **burned immediately**. No refund — the cost reflects the permanent state burden. **Only `x/rep` members can create permanent collections directly.**
- **Non-member permanent**: Not allowed at creation. Non-members must create TTL collections. Permanence is achieved through the **sponsorship mechanism** (see §3.16).

| Scenario | Collection Deposit | Per-Item Deposit | Non-Member Surcharge | On Expiry / Delete |
|----------|-------------------|-----------------|---------------------|-------------------|
| Member + TTL | Held | Held per item | None | All refunded |
| Member + permanent | Burned | Burned per item | None | No refund |
| Non-member + TTL | Held | Held per item + `per_item_spam_tax` burned per item | `endorsement_creation_fee` escrowed (split on endorsement) | Deposits refunded (taxes are not; endorsement fee refunded minus burn fraction) |
| Non-member + permanent | **Not allowed** — must use sponsorship (§3.16) | — | — | — |
| Sponsored (non-member → permanent) | Original TTL deposit refunded; permanent deposit burned | Original per-item deposits refunded; permanent per-item deposits burned | Original per-item spam taxes already burned | No refund (permanent) |

**Example costs** (at defaults): A non-member creating a TTL collection and adding 100 items pays: 1 SPARK (collection deposit, held) + 10 SPARK (100 × 0.1 SPARK item deposits, held) + 10 SPARK (endorsement creation fee, escrowed) + 50 SPARK (100 × 0.5 SPARK per-item spam tax, burned) = **71 SPARK total** (11 refundable, 10 escrowed, 50 burned). On endorsement, the 10 SPARK endorsement fee is split: 8 SPARK to endorser, 2 SPARK burned. A member creating a permanent 100-item collection pays: 1 SPARK + 10 SPARK = **11 SPARK burned**. A non-member requesting sponsorship for that same TTL collection additionally escrows: 1 SPARK + 10 SPARK = **11 SPARK** (burned on sponsorship approval, refunded on cancel/expiry). The sponsor pays a `sponsor_fee` of **1 SPARK** (burned). After sponsorship, the collection is permanent — any new items added cost `per_item_deposit` each (burned, since the collection is now permanent). The non-member owner still pays `per_item_spam_tax` on future item additions (spam tax is based on the adder's membership status, not the collection's sponsorship status).

### 3.1.3. Tiered Collection Limits

The maximum number of collections an address can own scales with `x/rep` trust level:

```
max_collections = max_collections_base + (trust_level_index × max_collections_per_trust_level)
```

Where `trust_level_index` is:
- Non-member: 0
- PROVISIONAL: 1
- ESTABLISHED: 2
- TRUSTED: 3
- COUNCIL: 4

| Trust Level | Default Max Collections |
|------------|------------------------|
| Non-member | 5 |
| PROVISIONAL | 20 |
| ESTABLISHED | 35 |
| TRUSTED | 50 |
| COUNCIL | 65 |

Both parameters are governance-adjustable.

**Non-member TTL cap**: Non-members are additionally constrained by `max_non_member_ttl_blocks`, which limits the maximum lifetime of their collections. This is typically shorter than `max_ttl_blocks` (e.g., ~30 days vs unlimited for members).

### 3.2. CollectionType

Hint for client applications about the primary content type. Does not restrict what items can be added.

```protobuf
enum CollectionType {
  COLLECTION_TYPE_UNSPECIFIED = 0;
  COLLECTION_TYPE_NFT = 1;
  COLLECTION_TYPE_LINK = 2;
  COLLECTION_TYPE_ONCHAIN = 3;
  COLLECTION_TYPE_MIXED = 4;
}
```

### 3.3. Visibility

```protobuf
enum Visibility {
  VISIBILITY_UNSPECIFIED = 0;
  VISIBILITY_PUBLIC = 1;
  VISIBILITY_PRIVATE = 2;
}
```

### 3.4. Item

An individual entry within a collection.

```protobuf
message Item {
  uint64 id = 1;
  uint64 collection_id = 2;
  string added_by = 3;

  // --- Public item content (used when parent collection encrypted = false) ---
  string title = 4;
  string description = 5;
  string image_uri = 6;
  ReferenceType reference_type = 7;
  oneof reference {
    NftReference nft = 8;
    LinkReference link = 9;
    OnChainReference on_chain = 10;
    CustomReference custom = 11;
  }
  map<string, string> attributes = 12;

  // --- Private item content (used when parent collection encrypted = true) ---
  bytes encrypted_data = 13;

  uint64 position = 14;
  int64 added_at = 15;

  // --- Moderation ---
  ItemStatus status = 16;              // ACTIVE, HIDDEN (default ACTIVE)

  // --- Reactions (counter-only) ---
  uint64 upvote_count = 17;
  uint64 downvote_count = 18;
}
```

**Public items** (parent `encrypted = false`): Use structured fields. Chain validates field lengths, attribute counts, reference type consistency, and reference field lengths.

**Private items** (parent `encrypted = true`): Client packs all content into a single encrypted blob in `encrypted_data`. Structured fields left empty. Chain enforces only `max_encrypted_data_size`.

### 3.5. ReferenceType

```protobuf
enum ReferenceType {
  REFERENCE_TYPE_UNSPECIFIED = 0;
  REFERENCE_TYPE_NFT = 1;
  REFERENCE_TYPE_LINK = 2;
  REFERENCE_TYPE_ON_CHAIN = 3;
  REFERENCE_TYPE_CUSTOM = 4;
}
```

### 3.6. NftReference

```protobuf
message NftReference {
  string chain_id = 1;            // All string fields ≤ max_reference_field_length
  string contract_address = 2;
  string token_id = 3;
  string token_standard = 4;
  string token_uri = 5;
}
```

### 3.7. LinkReference

```protobuf
message LinkReference {
  string uri = 1;                 // ≤ max_reference_field_length
  string content_hash = 2;        // ≤ max_reference_field_length
  string content_type = 3;        // ≤ max_reference_field_length
}
```

### 3.8. OnChainReference

```protobuf
message OnChainReference {
  string module = 1;              // ≤ max_reference_field_length
  string entity_type = 2;         // ≤ max_reference_field_length
  string entity_id = 3;           // ≤ max_reference_field_length
}
```

### 3.9. CustomReference

```protobuf
message CustomReference {
  string type_label = 1;          // ≤ max_reference_field_length
  string value = 2;               // ≤ max_reference_field_length
  map<string, string> extra = 3;  // Count ≤ max_attributes_per_item; key/value lengths ≤ attribute limits
}
```

**Note**: `CustomReference.extra` shares the same limits as `Item.attributes` (`max_attributes_per_item`, `max_attribute_key_length`, `max_attribute_value_length`). The combined count of `attributes` + `extra` must not exceed `max_attributes_per_item`.

### 3.10. Collaborator

```protobuf
message Collaborator {
  uint64 collection_id = 1;
  string address = 2;             // Must be active x/rep member
  CollaboratorRole role = 3;
  int64 added_at = 4;
}
```

### 3.11. CollaboratorRole

```protobuf
enum CollaboratorRole {
  COLLABORATOR_ROLE_UNSPECIFIED = 0;
  COLLABORATOR_ROLE_EDITOR = 1;   // Add, update, remove, reorder items
  COLLABORATOR_ROLE_ADMIN = 2;    // Editor + manage collaborators (not delete collection)
}
```

The collection **owner** has full control. Owner is not represented as a Collaborator.

### 3.12. Curator

A curator is an `x/rep` member who stakes DREAM to rate public collection quality. Same bond-and-work pattern as x/forum sentinels but with quality assessment rather than moderation.

```protobuf
message Curator {
  string address = 1;
  string bond_amount = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 registered_at = 3;
  uint64 total_reviews = 4;
  uint64 challenged_reviews = 5;   // Successfully challenged (overturned)
  bool active = 6;                 // False if bond < min_curator_bond or unregistered
  uint32 pending_challenges = 7;   // Count of unresolved challenges against this curator's reviews
}
```

### 3.13. CurationReview

```protobuf
message CurationReview {
  uint64 id = 1;
  uint64 collection_id = 2;
  string curator = 3;
  CurationVerdict verdict = 4;
  repeated string tags = 5;       // Descriptive quality tags
  string comment = 6;
  int64 created_at = 7;
  bool challenged = 8;
  bool overturned = 9;
  string challenger = 10;           // Address that filed the challenge (empty if unchallenged)
}
```

### 3.14. CurationVerdict

```protobuf
enum CurationVerdict {
  CURATION_VERDICT_UNSPECIFIED = 0;
  CURATION_VERDICT_UP = 1;
  CURATION_VERDICT_DOWN = 2;
}
```

### 3.15. CurationSummary

Denormalized aggregate quality signal, updated on each review or overturn.

```protobuf
message CurationSummary {
  uint64 collection_id = 1;
  uint32 up_count = 2;
  uint32 down_count = 3;
  repeated TagCount top_tags = 4;
  int64 last_reviewed_at = 5;
}

message TagCount {
  string tag = 1;
  uint32 count = 2;
}
```

### 3.16. SponsorshipRequest

A pending request from a non-member collection owner to have their TTL collection sponsored for permanence.

```protobuf
message SponsorshipRequest {
  uint64 collection_id = 1;
  string requester = 2;             // Non-member collection owner
  string collection_deposit = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // Escrowed permanent collection deposit
  string item_deposit_total = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // Escrowed permanent per-item deposits
  int64 requested_at = 5;           // Block height of request
  int64 expires_at = 6;             // Block height when request expires (escrowed deposits refunded)
}
```

**Lifecycle:**
1. Non-member calls `MsgRequestSponsorship` — pays `base_collection_deposit + (item_count × per_item_deposit)` into escrow in the module account. `SponsorshipRequest` created.
2. **Item count is locked** while the request is pending. `MsgAddItem`, `MsgAddItems`, `MsgRemoveItem`, and `MsgRemoveItems` are rejected on this collection. This ensures the escrowed deposit always matches the actual item count. Updates and reorders remain allowed (they don't affect item count or deposits).
3. A trusted member calls `MsgSponsorCollection` — pays `sponsor_fee` (burned). Escrowed permanent deposits are burned. Original TTL deposits (collection + per-item) are refunded to the owner. Collection converted to permanent. `sponsored_by` set.
4. If the request expires (EndBlocker) or the owner cancels (`MsgCancelSponsorshipRequest`), escrowed deposits are refunded and the item lock is released.
5. Only one active sponsorship request per collection at a time.

### 3.17. CollectionStatus

```protobuf
enum CollectionStatus {
  COLLECTION_STATUS_UNSPECIFIED = 0;
  COLLECTION_STATUS_ACTIVE = 1;       // Normal, visible
  COLLECTION_STATUS_PENDING = 2;      // Non-member collection awaiting endorsement (not publicly listed)
  COLLECTION_STATUS_HIDDEN = 3;       // Hidden by sentinel, pending appeal or expiry
}
```

**PENDING** collections are only visible to: (1) the owner via `CollectionsByOwner`, (2) members browsing the endorsement discovery feed via `PendingCollections`. They do not appear in `PublicCollections` or `PublicCollectionsByType`.

**HIDDEN** collections are excluded from all public listing queries but remain accessible via direct `Collection` query by ID (with status = HIDDEN visible). Items within a hidden collection are also excluded from public queries.

### 3.18. ItemStatus

```protobuf
enum ItemStatus {
  ITEM_STATUS_UNSPECIFIED = 0;
  ITEM_STATUS_ACTIVE = 1;
  ITEM_STATUS_HIDDEN = 2;            // Hidden by sentinel
}
```

### 3.19. CollectionFlag

Lightweight reporting mechanism. Members flag public collections or items for inappropriate content. When total flag weight reaches the review threshold, the content enters the sentinel review queue.

```protobuf
message CollectionFlag {
  uint64 target_id = 1;             // Collection ID or Item ID
  FlagTargetType target_type = 2;   // COLLECTION or ITEM
  repeated FlagRecord flag_records = 3;
  string total_weight = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 first_flag_at = 5;
  int64 last_flag_at = 6;
  bool in_review_queue = 7;
}

message FlagRecord {
  string flagger = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  ModerationReason reason = 2;
  string reason_text = 3;           // Custom text for REASON_OTHER
  int64 flagged_at = 4;
  string weight = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // Member = 2, non-member not allowed
}

enum FlagTargetType {
  FLAG_TARGET_TYPE_UNSPECIFIED = 0;
  FLAG_TARGET_TYPE_COLLECTION = 1;
  FLAG_TARGET_TYPE_ITEM = 2;
}
```

### 3.20. ModerationReason

Shared enum for both flags and sentinel hide actions. Mirrors x/forum's moderation reasons.

```protobuf
enum ModerationReason {
  MODERATION_REASON_UNSPECIFIED = 0;
  MODERATION_REASON_SPAM = 1;
  MODERATION_REASON_HARASSMENT = 2;
  MODERATION_REASON_MISINFORMATION = 3;
  MODERATION_REASON_INAPPROPRIATE = 4;
  MODERATION_REASON_IMPERSONATION = 5;
  MODERATION_REASON_POLICY_VIOLATION = 6;
  MODERATION_REASON_COPYRIGHT = 7;
  MODERATION_REASON_SCAM = 8;
  MODERATION_REASON_OTHER = 9;       // Requires reason_text
}
```

### 3.21. HideRecord

Created when a sentinel hides a collection or item. Tracks the sentinel's bond commitment for accountability.

```protobuf
message HideRecord {
  uint64 id = 1;
  uint64 target_id = 2;             // Collection ID or Item ID
  FlagTargetType target_type = 3;   // COLLECTION or ITEM
  string sentinel = 4 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  int64 hidden_at = 5;
  string committed_amount = 6 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // DREAM committed for this hide
  ModerationReason reason_code = 7;
  string reason_text = 8;
  int64 appeal_deadline = 9;        // Block height after which auto-deleted if no appeal
  bool appealed = 10;
  bool resolved = 11;               // True after appeal verdict or auto-deletion
}
```

**Lifecycle:**
1. Sentinel calls `MsgHideContent` — target set to HIDDEN, `committed_amount` (100 DREAM) locked from sentinel's x/forum sentinel bond (shared sentinel identity across modules), HideRecord created.
2. **Auto-deletion**: If no appeal within `hide_expiry_blocks` (~7 days), the EndBlocker deletes the target (collection or item) with deposit refunds and marks the HideRecord as resolved. For collections, this triggers full cleanup (items, collaborators, curation, sponsorship).
3. **Appeal**: Owner calls `MsgAppealHide` within the window. Routed to x/rep jury. HideRecord marked `appealed = true`. Auto-deletion paused.
4. **Resolution**: Jury verdict via callback — upheld (content restored, sentinel slashed) or rejected (content deleted, sentinel vindicated).

### 3.22. Endorsement

Created when a member endorses a non-member's PENDING collection.

```protobuf
message Endorsement {
  uint64 collection_id = 1;
  string endorser = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string dream_stake = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // DREAM staked by endorser
  int64 endorsed_at = 4;
  int64 stake_release_at = 5;       // Block height when stake is released (endorsed_at + endorsement_stake_duration)
  bool stake_released = 6;          // True after clean period or after slash
}
```

**Lifecycle:**
1. Non-member creates collection → status = PENDING
2. Non-member adds/edits items freely, toggles `seeking_endorsement = true` when ready
3. Member calls `MsgEndorseCollection` — pays nothing in SPARK (the non-member already paid the creation fee), stakes `endorsement_dream_stake` (100 DREAM)
4. Collection status → ACTIVE, `endorsed_by` set, `immutable = true`
5. **Creation fee split**: 80% of the non-member's original creation fee (`endorsement_creation_fee`, 10 SPARK) is sent from the module account to the endorser. 20% is burned.
6. After `endorsement_stake_duration` (~30 days) with no sentinel action: endorser's DREAM stake is released
7. If content is hidden by sentinel during the stake period: endorser's DREAM stake is slashed (burned), endorser receives a small reputation penalty via x/rep
8. If the non-member later becomes a member: `immutable` is set to `false`, unlocking full edit capabilities

### 3.23. Params

```protobuf
message Params {
  // --- Collection limits (tiered) — GOVERNANCE ONLY ---
  uint32 max_collections_base = 1;              // Base limit for non-members
  uint32 max_collections_per_trust_level = 2;   // Additional per trust level index

  // --- Size limits — GOVERNANCE ONLY ---
  uint32 max_items_per_collection = 3;
  uint32 max_title_length = 4;
  uint32 max_name_length = 5;
  uint32 max_description_length = 6;
  uint32 max_tag_length = 7;                    // Per-tag character limit
  uint32 max_tags_per_collection = 8;
  uint32 max_attributes_per_item = 9;           // Shared with CustomReference.extra
  uint32 max_attribute_key_length = 10;
  uint32 max_attribute_value_length = 11;
  uint32 max_reference_field_length = 12;       // Per-field limit for all reference types
  uint32 max_encrypted_data_size = 13;          // Max bytes per encrypted blob
  uint32 max_collaborators_per_collection = 14;
  uint32 max_batch_size = 15;
  int64 max_ttl_blocks = 16;                    // 0 = no upper limit
  int64 max_non_member_ttl_blocks = 17;         // Max TTL for non-member collections (shorter than max_ttl_blocks)

  // --- EndBlocker — GOVERNANCE ONLY ---
  uint32 max_prune_per_block = 35;              // Cap on entries pruned per block to prevent DoS

  // --- Fee parameters (SPARK) — OPERATIONAL ---
  string base_collection_deposit = 18 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string per_item_deposit = 19 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string per_item_spam_tax = 20 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // Non-member surcharge per item added; always burned

  // --- Sponsorship parameters — OPERATIONAL ---
  string sponsor_fee = 22 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];          // SPARK fee paid by sponsor (burned)
  string min_sponsor_trust_level = 23;           // Min trust level to sponsor (default: ESTABLISHED)
  int64 sponsorship_request_ttl_blocks = 24;     // How long a request stays open before expiry

  // --- Curation parameters — OPERATIONAL ---
  string min_curator_bond = 25 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string min_curator_trust_level = 26;
  int64 min_curator_age_blocks = 27;            // Blocks after registration before first review allowed
  uint32 max_tags_per_review = 28;
  uint32 max_review_comment_length = 29;
  uint32 max_reviews_per_collection = 30;
  string curator_slash_fraction = 31 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string challenge_reward_fraction = 36 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];  // Fraction of slashed bond sent to challenger; remainder burned
  int64 challenge_window_blocks = 32;
  string challenge_deposit = 33 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  uint32 max_challenge_reason_length = 34;

  // --- Reaction parameters — OPERATIONAL ---
  string downvote_cost = 37 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // SPARK burned per downvote (default 25 SPARK)
  uint32 max_upvotes_per_day = 38;    // Rate limit for upvotes per member per rolling 24h
  uint32 max_downvotes_per_day = 39;  // Rate limit for downvotes per member per rolling 24h

  // --- Flagging parameters — OPERATIONAL ---
  uint32 flag_review_threshold = 40;  // Total flag weight to enter sentinel review queue (default 5)
  uint32 max_flags_per_day = 41;      // Max flags per member per rolling 24h (default 20)
  uint32 max_flaggers_per_target = 42; // Max flag records tracked per target (default 50)
  int64 flag_expiration_blocks = 43;  // Flags expire after this many blocks (default ~7 days)
  uint32 max_flag_reason_length = 44; // Custom reason text limit for OTHER

  // --- Sentinel moderation parameters — OPERATIONAL ---
  string sentinel_commit_amount = 45 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // DREAM committed per hide (default 100)
  int64 hide_expiry_blocks = 46;      // Blocks before auto-deletion if no appeal (default ~7 days)
  string appeal_fee = 47 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // SPARK fee to appeal a hide (default 5 SPARK)
  int64 appeal_cooldown_blocks = 48;  // Blocks after hide before appeal allowed (default ~1 hour)
  int64 appeal_deadline_blocks = 49;  // Max blocks for jury to resolve appeal (default ~14 days)

  // --- Endorsement parameters — OPERATIONAL ---
  string endorsement_creation_fee = 50 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // SPARK fee for non-member collection creation (default 10 SPARK)
  string endorsement_dream_stake = 51 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];   // DREAM staked by endorser (default 100)
  int64 endorsement_stake_duration = 52;  // Blocks before endorser stake is released (default ~30 days)
  int64 endorsement_expiry_blocks = 53;   // Blocks before unendorsed collection auto-prunes (default ~30 days)
  string endorsement_fee_endorser_share = 54 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];  // Fraction of creation fee to endorser (default 0.80)
  string endorsement_deletion_burn_fraction = 55 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];  // Fraction of endorsement fee burned on PENDING deletion (default 0.10)

  // --- Conviction renewal parameters — OPERATIONAL ---
  string conviction_renewal_threshold = 56 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];  // Min conviction score to renew anonymous collections at TTL expiry (default: 0 = disabled)
  int64 conviction_renewal_period = 57;                          // Blocks to extend TTL by when conviction-renewed (default: same as collection's original TTL)

  // --- Pinning parameters — OPERATIONAL ---
  uint32 pin_min_trust_level = 61;    // Min trust level to pin collections (default: 2 = ESTABLISHED)
  uint32 max_pins_per_day = 62;       // Max pins per address per day (default: 10)
}
```

### 3.24. CollectOperationalParams

Subset of `Params` tunable by the Commons Operations Committee without a full governance proposal. Covers fees, sponsorship thresholds, and curation economics — parameters that may need frequent adjustment based on real-world usage.

```protobuf
message CollectOperationalParams {
  // --- Fee parameters (SPARK) ---
  string base_collection_deposit = 1 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string per_item_deposit = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string per_item_spam_tax = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // --- Sponsorship parameters ---
  string sponsor_fee = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string min_sponsor_trust_level = 6;
  int64 sponsorship_request_ttl_blocks = 7;

  // --- Curation parameters ---
  string min_curator_bond = 8 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string min_curator_trust_level = 9;
  int64 min_curator_age_blocks = 10;
  uint32 max_tags_per_review = 11;
  uint32 max_review_comment_length = 12;
  uint32 max_reviews_per_collection = 13;
  string curator_slash_fraction = 14 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string challenge_reward_fraction = 18 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  int64 challenge_window_blocks = 15;
  string challenge_deposit = 16 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  uint32 max_challenge_reason_length = 17;

  // --- Reaction parameters ---
  string downvote_cost = 19 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  uint32 max_upvotes_per_day = 20;
  uint32 max_downvotes_per_day = 21;

  // --- Flagging parameters ---
  uint32 flag_review_threshold = 22;
  uint32 max_flags_per_day = 23;
  uint32 max_flaggers_per_target = 24;
  int64 flag_expiration_blocks = 25;
  uint32 max_flag_reason_length = 26;

  // --- Sentinel moderation parameters ---
  string sentinel_commit_amount = 27 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 hide_expiry_blocks = 28;
  string appeal_fee = 29 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 appeal_cooldown_blocks = 30;
  int64 appeal_deadline_blocks = 31;

  // --- Endorsement parameters ---
  string endorsement_creation_fee = 32 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string endorsement_dream_stake = 33 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 endorsement_stake_duration = 34;
  int64 endorsement_expiry_blocks = 35;
  string endorsement_fee_endorser_share = 36 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string endorsement_deletion_burn_fraction = 37 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];

  // --- Conviction renewal parameters ---
  string conviction_renewal_threshold = 38 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  int64 conviction_renewal_period = 39;

  // --- Pinning parameters ---
  uint32 pin_min_trust_level = 43;
  uint32 max_pins_per_day = 44;
}
```

**Governance-only fields** (require full `x/gov` proposal via `MsgUpdateParams`): collection limits (`max_collections_base`, `max_collections_per_trust_level`), all size limits (`max_items_per_collection`, `max_title_length`, `max_name_length`, `max_description_length`, `max_tag_length`, `max_tags_per_collection`, `max_attributes_per_item`, `max_attribute_key_length`, `max_attribute_value_length`, `max_reference_field_length`, `max_encrypted_data_size`, `max_collaborators_per_collection`, `max_batch_size`), TTL policies (`max_ttl_blocks`, `max_non_member_ttl_blocks`), and EndBlocker config (`max_prune_per_block`). These are structural constraints that affect validation rules, capacity planning, and consensus behavior.

---

## 4. Storage Schema

| Key Prefix | Value | Description |
|------------|-------|-------------|
| `Collection/value/{id}` | `Collection` | Collection by ID |
| `Collection/count/` | `uint64` | Next collection ID counter |
| `Collection/owner/{owner}/{id}` | `[]byte{}` | Index: collections by owner |
| `Collection/expiry/{expires_at}/{id}` | `[]byte{}` | Index: by expiry block |
| `Item/value/{id}` | `Item` | Item by ID |
| `Item/count/` | `uint64` | Next item ID counter |
| `Item/collection/{collection_id}/{position}` | `uint64` | Index: items by collection, ordered |
| `Item/owner/{collection_owner}/{id}` | `[]byte{}` | Index: items by collection owner (not `added_by`) |
| `Collaborator/{collection_id}/{address}` | `Collaborator` | Collaborator record |
| `Collaborator/address/{address}/{collection_id}` | `[]byte{}` | Reverse index |
| `Curator/{address}` | `Curator` | Curator record |
| `CurationReview/value/{id}` | `CurationReview` | Review by ID |
| `CurationReview/count/` | `uint64` | Next review ID counter |
| `CurationReview/collection/{collection_id}/{id}` | `[]byte{}` | Index: reviews by collection |
| `CurationReview/curator/{curator}/{id}` | `[]byte{}` | Index: reviews by curator |
| `CurationSummary/{collection_id}` | `CurationSummary` | Aggregate quality signal |
| `SponsorshipRequest/{collection_id}` | `SponsorshipRequest` | Pending sponsorship request |
| `SponsorshipRequest/expiry/{expires_at}/{collection_id}` | `[]byte{}` | Index: by request expiry block |
| `Flag/{target_type}/{target_id}` | `CollectionFlag` | Flag state for a collection or item |
| `Flag/review/{target_type}/{target_id}` | `[]byte{}` | Index: flagged content in review queue |
| `Flag/expiry/{expires_at}/{target_type}/{target_id}` | `[]byte{}` | Index: flag expiry |
| `HideRecord/value/{id}` | `HideRecord` | Hide record by ID |
| `HideRecord/count/` | `uint64` | Next hide record ID counter |
| `HideRecord/target/{target_type}/{target_id}/{id}` | `[]byte{}` | Index: hide records by target |
| `HideRecord/expiry/{appeal_deadline}/{id}` | `[]byte{}` | Index: hide deadline (used for both unappealed auto-delete and appeal timeout) |
| `Endorsement/{collection_id}` | `Endorsement` | Endorsement record by collection ID |
| `Endorsement/expiry/{stake_release_at}/{collection_id}` | `[]byte{}` | Index: endorsement stake release |
| `Endorsement/pending/{endorsement_expiry}/{collection_id}` | `[]byte{}` | Index: unendorsed collection auto-prune |
| `ReactionLimit/{address}/{day}` | `ReactionCount` | Daily reaction counter (upvotes, downvotes, flags) |
| `ReactionDedup/{address}/{target_type}/{target_id}` | `uint8` | Dedup: 1 = upvoted, 2 = downvoted. Prevents same member from voting same target twice |
| `Item/by_onchain_ref/{module}:{entity_type}:{entity_id}/{item_id}` | `[]byte{}` | Reverse index for OnChainReference lookups |
| `Collection/by_status/{status}/{id}` | `[]byte{}` | Index: collections by status (for PendingCollections query) |

**Module account** (`x/collect`): Holds **SPARK** (TTL collection deposits, TTL per-item deposits, sponsorship escrow deposits, appeal fee escrow, endorsement creation fee escrow) and **DREAM** (curator bonds, challenge deposits). Endorser DREAM stakes are held via x/rep keeper (not in the module account directly). Permanent deposits, sponsor fees, downvote costs, and all spam taxes are sent directly to the burn address.

---

## 5. Messages

### 5.1. MsgCreateCollection

```protobuf
message MsgCreateCollection {
  string creator = 1;
  CollectionType type = 2;
  Visibility visibility = 3;
  bool encrypted = 4;
  int64 expires_at = 5;

  // Public content (encrypted = false)
  string name = 6;
  string description = 7;
  string cover_uri = 8;
  repeated string tags = 9;

  // Private content (encrypted = true)
  bytes encrypted_data = 10;

  // Optional DREAM amount to lock as author bond
  string author_bond = 11 [(gogoproto.customtype) = "cosmossdk.io/math.Int", (gogoproto.nullable) = true];

  // Optional x/rep initiative to link for conviction propagation (0 = none, immutable)
  uint64 initiative_id = 12;
}

message MsgCreateCollectionResponse {
  uint64 id = 1;
}
```

**Fee logic:**
1. **Members**: Charge `base_collection_deposit` only. No surcharge.
2. **Non-members**: Charge `endorsement_creation_fee` (10 SPARK) — escrowed in module account (paid out on endorsement or refunded on pruning/deletion). Plus `base_collection_deposit` (held for TTL). `per_item_spam_tax` still applies when non-members add items.
3. TTL (`expires_at > 0`): transfer `base_collection_deposit` to module account
4. Permanent (`expires_at = 0`): burn `base_collection_deposit` (**members only** — non-members rejected)

**Status assignment:**
- **Members**: `status = ACTIVE`, `community_feedback_enabled = true` (default)
- **Non-members**: `status = PENDING`, `seeking_endorsement = true`, `immutable = false`, `community_feedback_enabled = true` (default). Non-member collections start seeking endorsement immediately to appear in the endorsement discovery feed. Non-members can toggle this off via `MsgSetSeekingEndorsement` if they want to curate items before seeking endorsement

**Validation:**
- Owner must not exceed tiered `max_collections` for their trust level
- **Non-members must set `expires_at > 0`** — permanent collections require `x/rep` membership (or sponsorship after creation)
- Non-member TTL must be ≤ `max_non_member_ttl_blocks`; member TTL must be ≤ `max_ttl_blocks` (when `max_ttl_blocks > 0`)
- If `encrypted = false`: `name` 1–`max_name_length`; `description` ≤ `max_description_length`; `cover_uri` ≤ `max_reference_field_length`; each tag ≤ `max_tag_length`; tag count ≤ `max_tags_per_collection`; `encrypted_data` must be empty
- If `encrypted = true`: `encrypted_data` ≤ `max_encrypted_data_size`; structured fields must be empty; `visibility` must be `PRIVATE`
- If `visibility = PRIVATE`, `encrypted` must be `true`
- `encrypted = true` + `visibility = PUBLIC` is rejected
- If `expires_at > 0`, must be in the future

### 5.2. MsgUpdateCollection

```protobuf
message MsgUpdateCollection {
  string creator = 1;
  uint64 id = 2;
  CollectionType type = 3;
  int64 expires_at = 4;

  // Public content (collection encrypted = false)
  string name = 5;
  string description = 6;
  string cover_uri = 7;
  repeated string tags = 8;

  // Private content (collection encrypted = true)
  bytes encrypted_data = 9;

  // Community feedback toggle
  bool community_feedback_enabled = 10;
  bool update_community_feedback = 11;  // true to apply community_feedback_enabled value
}

message MsgUpdateCollectionResponse {}
```

**Validation:**
- `creator` must be collection owner
- **Collection must not be `immutable`** (endorsed non-member collections cannot be edited until owner becomes member)
- Collection must have `status = ACTIVE` or `status = PENDING` (hidden collections cannot be edited)
- Same size constraints as create
- `visibility` and `encrypted` are immutable (not present in message)
- `community_feedback_enabled`: if `update_community_feedback = true`, applies the `community_feedback_enabled` value. Disabling freezes existing reaction/curation data (no new reactions or reviews accepted). Re-enabling allows new reactions and reviews. No effect on sentinel moderation — flagging and hiding always apply
- **Non-members cannot set `expires_at = 0`** — TTL→permanent conversion requires membership (or sponsorship via §5.17–5.19)
- **Permanent collections cannot set `expires_at > 0`** — permanent→TTL conversion is not allowed (deposits already burned; use MsgDeleteCollection instead)
- If `expires_at > 0`, must be in the future
- Member TTL update must remain ≤ `max_ttl_blocks` (when `max_ttl_blocks > 0`)
- Non-member TTL extension must remain ≤ `max_non_member_ttl_blocks` from original creation block

**TTL conversion (members only):** Setting `expires_at = 0` on a TTL collection converts it to permanent: the held collection deposit and all held item deposits are burned from the module account. `deposit_burned` set to `true`. Collection removed from expiry index. If a pending `SponsorshipRequest` exists, the escrowed deposits are refunded to the owner, the request is deleted, and its expiry index entry is removed (the direct conversion supersedes sponsorship).

### 5.3. MsgDeleteCollection

Deletes a collection and all associated state: items, collaborators, curation reviews, curation summary, sponsorship request, flags, hide records, endorsement record, and reaction dedup records for the collection and its items.

```protobuf
message MsgDeleteCollection {
  string creator = 1;
  uint64 id = 2;
}

message MsgDeleteCollectionResponse {}
```

**Validation:**
- `creator` must be collection owner

**Cleanup:** If the collection has a TTL (`expires_at > 0`), its expiry index entry is removed. If a pending `SponsorshipRequest` exists, the escrowed permanent deposits are also refunded to the owner, the request deleted, and its expiry index entry removed.

**Deposit refund:** If `deposit_burned = false` (TTL): `deposit_amount` + `item_deposit_total` refunded from module account to owner. If `deposit_burned = true`: no refund.

**Pending challenge cleanup:** For each curation review on this collection where `challenged = true` and `overturned = false` (unresolved challenge): refund `challenge_deposit` from module account to the challenger, and decrement `pending_challenges` on the reviewed curator. This prevents permanent fund lockup and ensures curators can eventually unregister.

**PENDING collection deletion:** If `status = PENDING` (non-member collection), the escrowed `endorsement_creation_fee` is partially refunded: `endorsement_deletion_burn_fraction` (default 10%) is burned to cover network costs, the remainder is refunded to the owner.

**Endorsed collection deletion:** If the collection was endorsed (`endorsed_by` is not empty) and the endorser's DREAM stake has not yet been released (`stake_released = false` on the `Endorsement` record), the endorser's stake is released immediately via x/rep keeper. The endorser already received their fee share at endorsement time and should not be penalized for the owner's deletion.

**Active appeal cleanup:** If there is an active hide appeal on this collection or any of its items (i.e., a `HideRecord` with `appealed = true` and `resolved = false`), the escrowed `appeal_fee` is burned (the owner chose to delete rather than await resolution). The sentinel's bond commitment is released (no slash since there is no verdict). The HideRecord is marked `resolved = true` and removed from the `HideRecord/expiry/` index to prevent §10.3a from processing a stale record.

### 5.4. MsgAddItem

```protobuf
message MsgAddItem {
  string creator = 1;
  uint64 collection_id = 2;
  uint64 position = 3;

  // Public item content
  string title = 4;
  string description = 5;
  string image_uri = 6;
  ReferenceType reference_type = 7;
  NftReference nft = 8;
  LinkReference link = 9;
  OnChainReference on_chain = 10;
  CustomReference custom = 11;
  map<string, string> attributes = 12;

  // Private item content
  bytes encrypted_data = 13;
}

message MsgAddItemResponse {
  uint64 id = 1;
}
```

**Fee logic:**
- `per_item_deposit` charged to the **message signer** (`creator`) in SPARK — the adder always pays, even when adding to another owner's collection as a collaborator. This prevents collaborators from draining the owner's funds by adding items on their behalf. Deposit refunds on item removal or collection expiry go to the collection **owner** (not the original adder).
- Permanent collection (including sponsored): `per_item_deposit` burned immediately
- TTL collection: `per_item_deposit` held in module account; `item_deposit_total` incremented on parent
- Non-member: additionally charged `per_item_spam_tax` per item (burned)

**Validation:**
- `creator` must be owner or EDITOR/ADMIN collaborator
- If `creator` is a collaborator (not owner), they must still be an active `x/rep` member (see §14.8)
- **Collection must not be `immutable`** (endorsed non-member collections cannot be modified)
- Collection must not have a pending `SponsorshipRequest` (item count is locked during sponsorship — see §3.16)
- Collection must not exceed `max_items_per_collection`
- `position`: insertion index in range [0, current `item_count`]. Items at `position` and above are shifted right by one. Use `position = item_count` (or omit / set to max uint64) to append at the end.
- If public: `title` ≤ `max_title_length`; `description` ≤ `max_description_length`; `image_uri` ≤ `max_reference_field_length`; exactly one reference matching `reference_type`; all reference string fields ≤ `max_reference_field_length`; `attributes` + `CustomReference.extra` combined count ≤ `max_attributes_per_item`; attribute key/value within length limits; `encrypted_data` must be empty
- If encrypted: `encrypted_data` ≤ `max_encrypted_data_size`; structured fields must be empty

### 5.5. MsgAddItems

```protobuf
message AddItemEntry {
  string title = 1;
  string description = 2;
  string image_uri = 3;
  ReferenceType reference_type = 4;
  NftReference nft = 5;
  LinkReference link = 6;
  OnChainReference on_chain = 7;
  CustomReference custom = 8;
  map<string, string> attributes = 9;
  bytes encrypted_data = 10;
}

message MsgAddItems {
  string creator = 1;
  uint64 collection_id = 2;
  repeated AddItemEntry items = 3;
}

message MsgAddItemsResponse {
  repeated uint64 ids = 1;
}
```

**Fee logic:** `items.length × per_item_deposit` charged to the **message signer** (`creator`) as a single transfer. Same burn/hold logic per collection type. Non-members additionally pay `items.length × per_item_spam_tax` (burned). Deposit refunds go to the collection owner (see §5.4).

**Validation:**
- Same per-item validation as MsgAddItem (including immutability and sponsorship lock checks)
- `items` count 1–`max_batch_size`
- Total item count after addition ≤ `max_items_per_collection`

**Position assignment:** Batch-added items are appended sequentially at the end of the collection in the order they appear in the `items` array. The first item gets position `current_item_count`, the second `current_item_count + 1`, and so on.

### 5.6. MsgUpdateItem

```protobuf
message MsgUpdateItem {
  string creator = 1;
  uint64 id = 2;
  string title = 3;
  string description = 4;
  string image_uri = 5;
  ReferenceType reference_type = 6;
  NftReference nft = 7;
  LinkReference link = 8;
  OnChainReference on_chain = 9;
  CustomReference custom = 10;
  map<string, string> attributes = 11;
  bytes encrypted_data = 12;
}

message MsgUpdateItemResponse {}
```

**Validation:**
- Item must exist
- `creator` must be owner or EDITOR/ADMIN collaborator
- If `creator` is a collaborator (not owner), they must still be an active `x/rep` member (see §14.8)
- **Parent collection must not be `immutable`**
- Same size constraints as MsgAddItem (title, description, image_uri, reference fields, attributes, encrypted_data)
- No additional deposit charged for updates
- No per-item spam tax charged for updates
- Updates are allowed during pending sponsorship (item count does not change)

### 5.7. MsgRemoveItem

Removes a single item. Positions compacted.

```protobuf
message MsgRemoveItem {
  string creator = 1;
  uint64 id = 2;
}

message MsgRemoveItemResponse {}
```

**Validation:**
- `creator` must be owner or EDITOR/ADMIN collaborator
- If `creator` is a collaborator (not owner), they must still be an active `x/rep` member (see §14.8)
- **Parent collection must not be `immutable`**
- Item must exist
- Parent collection must not have a pending `SponsorshipRequest` (item count is locked during sponsorship)

**Deposit refund:** If parent collection `deposit_burned = false`, the `per_item_deposit` for this item is refunded from module account to collection owner. `item_deposit_total` decremented.

### 5.8. MsgRemoveItems

Batch remove. All items must belong to the same collection. Positions compacted once after all removals (not per-item).

```protobuf
message MsgRemoveItems {
  string creator = 1;
  repeated uint64 ids = 2;
}

message MsgRemoveItemsResponse {}
```

**Validation:**
- All items must exist
- No duplicate IDs
- All items must belong to the same collection
- `creator` must be owner or EDITOR/ADMIN collaborator on that collection
- If `creator` is a collaborator (not owner), they must still be an active `x/rep` member (see §14.8)
- **Parent collection must not be `immutable`**
- Parent collection must not have a pending `SponsorshipRequest` (item count is locked during sponsorship)
- `ids` count 1–`max_batch_size`

**Position compaction:** Items are removed, then remaining positions are compacted in a single pass to avoid O(N²) reshuffling.

### 5.9. MsgReorderItem

```protobuf
message MsgReorderItem {
  string creator = 1;
  uint64 id = 2;
  uint64 new_position = 3;
}

message MsgReorderItemResponse {}
```

**Validation:**
- `creator` must be owner or EDITOR/ADMIN collaborator
- If `creator` is a collaborator (not owner), they must still be an active `x/rep` member (see §14.8)
- **Parent collection must not be `immutable`** (endorsed non-member collections cannot be modified until owner becomes member)
- Item must exist and belong to a collection where `creator` has write access
- `new_position` must be in range [0, `item_count - 1`]

### 5.10. MsgAddCollaborator

```protobuf
message MsgAddCollaborator {
  string creator = 1;
  uint64 collection_id = 2;
  string address = 3;
  CollaboratorRole role = 4;
}

message MsgAddCollaboratorResponse {}
```

**Validation:**
- `creator` must be owner or ADMIN
- **Collection must not be `immutable`** (endorsed non-member collections cannot add collaborators until owner becomes member)
- `address` must be active `x/rep` member
- `address` not already a collaborator or the owner
- Collection ≤ `max_collaborators_per_collection`

### 5.11. MsgRemoveCollaborator

```protobuf
message MsgRemoveCollaborator {
  string creator = 1;
  uint64 collection_id = 2;
  string address = 3;
}

message MsgRemoveCollaboratorResponse {}
```

**Validation:**
- `creator` must be owner or ADMIN (ADMIN cannot remove other ADMINs)
- **Collection must not be `immutable`** (endorsed non-member collections cannot modify collaborators until owner becomes member), unless self-removal
- Self-removal always allowed

### 5.12. MsgUpdateCollaboratorRole

```protobuf
message MsgUpdateCollaboratorRole {
  string creator = 1;
  uint64 collection_id = 2;
  string address = 3;
  CollaboratorRole role = 4;
}

message MsgUpdateCollaboratorRoleResponse {}
```

**Validation:**
- **Collection must not be `immutable`** (endorsed non-member collections cannot modify collaborator roles until owner becomes member)
- Only owner can grant/revoke ADMIN

### 5.13. MsgRegisterCurator

```protobuf
message MsgRegisterCurator {
  string creator = 1;
  string bond_amount = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

message MsgRegisterCuratorResponse {}
```

**Validation:**
- `creator` must be active `x/rep` member at or above `min_curator_trust_level`
- `bond_amount` ≥ `min_curator_bond`
- Not already a registered active curator

### 5.14. MsgUnregisterCurator

```protobuf
message MsgUnregisterCurator {
  string creator = 1;
}

message MsgUnregisterCuratorResponse {}
```

**Validation:**
- Must be a registered active curator
- Must have `pending_challenges == 0` (cannot unregister while challenges are in-flight; bond must remain locked until resolution)
- Remaining bond refunded

### 5.15. MsgRateCollection

```protobuf
message MsgRateCollection {
  string creator = 1;
  uint64 collection_id = 2;
  CurationVerdict verdict = 3;
  repeated string tags = 4;
  string comment = 5;
}

message MsgRateCollectionResponse {
  uint64 review_id = 1;
}
```

**Validation:**
- `creator` must be a registered active curator with `bond_amount ≥ min_curator_bond`
- `creator` must still meet `min_curator_trust_level` (trust level re-checked on every rating, not just at registration)
- Curator must have been registered for at least `min_curator_age_blocks` (prevents register-rate-unregister sybil)
- Collection must be `VISIBILITY_PUBLIC` with `status = ACTIVE`
- Collection must have `community_feedback_enabled = true`
- Collection must not be expired (`expires_at = 0` or `expires_at > current_block`)
- `creator` must not be owner or collaborator of the collection
- `creator` must not have an active (non-overturned) review for this collection — curators may re-review after a successful challenge overturns their previous review
- Active (non-overturned) reviews for collection < `max_reviews_per_collection`
- `tags` count ≤ `max_tags_per_review`; each tag 1–`max_tag_length`, lowercase alphanumeric + hyphens
- `comment` ≤ `max_review_comment_length`
- `verdict` must be UP or DOWN
- Curator must not have exceeded `max_reviews_per_collection` reviews today (curator daily rate limit using the shared daily reaction counter)

**Logic:**
1. Create CurationReview record
2. Increment `total_reviews` on the curator
3. Increment daily review counter for the curator
4. Update CurationSummary (increment `up_count` or `down_count`, merge tags)

### 5.16. MsgChallengeReview

```protobuf
message MsgChallengeReview {
  string creator = 1;
  uint64 review_id = 2;
  string reason = 3;
}

message MsgChallengeReviewResponse {}
```

**Validation:**
- `creator` must be active `x/rep` member
- Review must exist, not already challenged or overturned
- Review's parent collection must not be expired (`expires_at = 0` or `expires_at > current_block`)
- Review within `challenge_window_blocks` of creation
- `creator` must not be the review's curator
- `reason` ≤ `max_challenge_reason_length`
- `creator` must post `challenge_deposit` (DREAM, held in module account)

**Logic:**
1. Mark review `challenged = true`, set `challenger = creator`
2. Increment `pending_challenges` on the reviewed curator
3. Transfer `challenge_deposit` from `creator` to module account

**Resolution** (via x/rep jury):
- **Upheld**: Review marked `overturned = true`, CurationSummary recalculated. Curator `challenged_reviews` incremented. Curator bond slashed by `curator_slash_fraction`; of the slashed amount, `challenge_reward_fraction` (default 80%) is sent to the challenger as a reward and the remainder is burned. If remaining bond < `min_curator_bond`, curator deactivated (`active = false`). `pending_challenges` decremented. Challenge deposit refunded.
- **Rejected**: Review stands. `pending_challenges` decremented. Challenge deposit burned.
- **Review missing** (collection deleted or expired during jury deliberation): No-op — the deletion/expiry cleanup already refunded the challenge deposit and decremented `pending_challenges`. `ResolveChallengeResult` returns nil without modifying any state. This prevents double-refunds, unwarranted bond slashing, and `pending_challenges` underflow.

### 5.17. MsgRequestSponsorship

Non-member collection owner requests sponsorship for permanence. Escrows the full permanent deposit.

```protobuf
message MsgRequestSponsorship {
  string creator = 1;
  uint64 collection_id = 2;
}

message MsgRequestSponsorshipResponse {}
```

**Validation:**
- `creator` must be the collection owner
- Collection must be a TTL collection (`expires_at > 0`, `deposit_burned = false`)
- `creator` must **not** be an active `x/rep` member (members can convert to permanent directly via MsgUpdateCollection)
- No existing sponsorship request for this collection
- Collection must not have already been sponsored

**Fee logic:**
1. Calculate permanent deposit: `base_collection_deposit + (item_count × per_item_deposit)`
2. Transfer full amount from `creator` to module account (escrow)
3. Create `SponsorshipRequest` with `expires_at = current_block + sponsorship_request_ttl_blocks`

### 5.18. MsgCancelSponsorshipRequest

Owner cancels a pending sponsorship request. Escrowed deposits refunded.

```protobuf
message MsgCancelSponsorshipRequest {
  string creator = 1;
  uint64 collection_id = 2;
}

message MsgCancelSponsorshipRequestResponse {}
```

**Validation:**
- `creator` must be the collection owner
- Active sponsorship request must exist for this collection

**Logic:**
1. Refund escrowed `collection_deposit + item_deposit_total` from module account to `creator`
2. Delete `SponsorshipRequest` and its expiry index entry

### 5.19. MsgSponsorCollection

Trusted member sponsors a non-member collection for permanence.

```protobuf
message MsgSponsorCollection {
  string creator = 1;
  uint64 collection_id = 2;
}

message MsgSponsorCollectionResponse {}
```

**Validation:**
- `creator` must be an active `x/rep` member at or above `min_sponsor_trust_level`
- `creator` must **not** be the collection owner (no self-sponsorship)
- Active sponsorship request must exist for this collection
- Collection must still be a TTL collection (not expired)

**Logic:**
1. Charge `sponsor_fee` from `creator` — **burned** (skin in the game)
2. Burn the escrowed permanent deposits (`collection_deposit + item_deposit_total`) from module account
3. Refund the original TTL deposits (`deposit_amount + item_deposit_total` on Collection) from module account to collection owner
4. Update collection deposit fields: `deposit_amount` = escrowed `collection_deposit`, `item_deposit_total` = escrowed `item_deposit_total` (reflects the permanent amounts that were burned)
5. Convert collection to permanent: set `expires_at = 0`, `deposit_burned = true`
6. Set `sponsored_by = creator` on Collection
7. Delete `SponsorshipRequest` and its expiry index entry
8. Remove collection from expiry index

### 5.20. MsgUpdateParams

```protobuf
message MsgUpdateParams {
  string authority = 1;  // Must be x/gov module account
  Params params = 2;
}

message MsgUpdateParamsResponse {}
```

**Validation:**
- `authority` must be the `x/gov` module account (strict bytes-equal check)

### 5.21. MsgUpdateOperationalParams

Update operational (non-critical) parameters via Commons Operations Committee.

```protobuf
message MsgUpdateOperationalParams {
  string authority = 1;  // Must be authorized by x/commons Operations Committee
  CollectOperationalParams operational_params = 2;
}

message MsgUpdateOperationalParamsResponse {}
```

**Validation:**
- `authority` must be authorized via `x/commons.IsCouncilAuthorized(ctx, authority, "commons", "operations")` — accepts x/gov authority, Commons Council policy address, or Operations Committee member
- `operational_params` must pass field-level validation (positive fees, valid trust levels, positive timeouts, etc.)

**Logic:**
1. Verify council authorization
2. Validate operational params in isolation
3. Fetch current full `Params`
4. Merge: apply operational fields onto current params (governance-only fields preserved)
5. Validate merged params (cross-field invariants)
6. Store merged params

### 5.22. MsgUpvoteContent

```protobuf
message MsgUpvoteContent {
  string creator = 1;
  uint64 target_id = 2;
  FlagTargetType target_type = 3;   // COLLECTION or ITEM
}

message MsgUpvoteContentResponse {}
```

**Validation:**
- `creator` must be an active `x/rep` member
- Target must exist and have status = ACTIVE
- Target must be a public, active collection (`visibility = PUBLIC`, `status = ACTIVE`) or an item within such a collection
- If `target_type = COLLECTION`: collection must have `community_feedback_enabled = true`
- If `target_type = ITEM`: parent collection must have `community_feedback_enabled = true`
- `creator` must not be the collection owner or a collaborator
- `creator` must not have already voted (upvote or downvote) on this target (`ReactionDedup` key must not exist)
- `creator` must not exceed `max_upvotes_per_day` (rolling 24h window)

**Logic:**
1. Increment `upvote_count` on target
2. Store `ReactionDedup/{creator}/{target_type}/{target_id} = 1` (upvote marker)
3. Emit `content_upvoted` event

### 5.23. MsgDownvoteContent

```protobuf
message MsgDownvoteContent {
  string creator = 1;
  uint64 target_id = 2;
  FlagTargetType target_type = 3;   // COLLECTION or ITEM
}

message MsgDownvoteContentResponse {}
```

**Validation:**
- `creator` must be an active `x/rep` member
- Target must exist and have status = ACTIVE
- Target must be a public, active collection (`visibility = PUBLIC`, `status = ACTIVE`) or an item within such a collection
- If `target_type = COLLECTION`: collection must have `community_feedback_enabled = true`
- If `target_type = ITEM`: parent collection must have `community_feedback_enabled = true`
- `creator` must not be the collection owner or a collaborator
- `creator` must not have already voted (upvote or downvote) on this target (`ReactionDedup` key must not exist)
- `creator` must not exceed `max_downvotes_per_day` (rolling 24h window)

**Fee logic:** `downvote_cost` (25 SPARK) is **burned immediately**. No refund. This ensures downvotes represent serious signals.

**Logic:**
1. Burn `downvote_cost` from `creator`
2. Increment `downvote_count` on target
3. Store `ReactionDedup/{creator}/{target_type}/{target_id} = 2` (downvote marker)
4. Emit `content_downvoted` event

### 5.24. MsgFlagContent

```protobuf
message MsgFlagContent {
  string creator = 1;
  uint64 target_id = 2;
  FlagTargetType target_type = 3;   // COLLECTION or ITEM
  ModerationReason reason = 4;
  string reason_text = 5;           // Required if reason = OTHER
}

message MsgFlagContentResponse {}
```

**Validation:**
- `creator` must be an active `x/rep` member (members-only flagging)
- Target must exist and have status = ACTIVE
- Target must be a public, active collection (`visibility = PUBLIC`, `status = ACTIVE`) or an item within such a collection
- `creator` must not have already flagged this target
- `creator` must not exceed `max_flags_per_day` (rolling 24h window)
- If `reason = OTHER`: `reason_text` must be 1–`max_flag_reason_length`
- If `reason != OTHER`: `reason_text` must be empty

**Logic:**
1. Create or update `CollectionFlag` for the target:
   - Add `FlagRecord` with weight = 2 (member weight)
   - Increment `total_weight`
   - Update `last_flag_at`
   - If `total_weight ≥ flag_review_threshold` and not already `in_review_queue`: set `in_review_queue = true`
2. Cap `flag_records` at `max_flaggers_per_target` (oldest records dropped after cap)
3. Emit `content_flagged` event

### 5.25. MsgHideContent

Sentinel hides a public collection or item for policy violation. Uses the shared x/forum sentinel identity — sentinels bonded for x/forum automatically have moderation authority in x/collect.

```protobuf
message MsgHideContent {
  string creator = 1;
  uint64 target_id = 2;
  FlagTargetType target_type = 3;   // COLLECTION or ITEM
  ModerationReason reason_code = 4;
  string reason_text = 5;
}

message MsgHideContentResponse {
  uint64 hide_record_id = 1;
}
```

**Validation:**
- `creator` must be an active x/forum sentinel (bond status NOT DEMOTED, meets min rep tier, has qualified backing)
- `creator` must not be in overturn cooldown
- Target must exist and have status = ACTIVE
- Target must not already be hidden (prevents duplicate hide records for the same target)
- Target must be a public, active collection (`visibility = PUBLIC`, `status = ACTIVE`) or an item within such a collection
- If `reason_code = OTHER`: `reason_text` must be non-empty and ≤ `max_flag_reason_length`
- If `reason_code != OTHER`: `reason_text` must be empty (structured reasons don't allow free text)
- Sentinel must have available bond ≥ `sentinel_commit_amount` (available = current bond - total committed across x/forum + x/collect)

**Logic:**
1. Set target status to HIDDEN
2. Commit `sentinel_commit_amount` (100 DREAM) from sentinel's bond via x/forum keeper (cross-module bond commitment)
3. Create `HideRecord` with `appeal_deadline = current_block + hide_expiry_blocks`
4. Clear any existing `CollectionFlag` for this target (sentinel action supersedes flags)
5. If hiding a collection: all items within are implicitly hidden (excluded from queries)
6. Emit `content_hidden` event

### 5.26. MsgAppealHide

Collection or item owner appeals a sentinel hide action. Routed to x/rep jury for resolution.

```protobuf
message MsgAppealHide {
  string creator = 1;
  uint64 hide_record_id = 2;
}

message MsgAppealHideResponse {}
```

**Validation:**
- `creator` must be the owner of the hidden collection, or for hidden items, the item adder (`added_by`) or the parent collection owner
- `HideRecord` must exist, not already appealed, not resolved
- Must wait at least `appeal_cooldown_blocks` after hide
- Appeal deadline must not have passed (`current_block < appeal_deadline`)

**Fee logic:** `appeal_fee` (5 SPARK) escrowed in module account.

**Logic:**
1. Escrow `appeal_fee` from `creator` to module account
2. Create x/rep Initiative (MODERATION_APPEAL type) for jury resolution
3. Mark `HideRecord.appealed = true`
4. Update `HideRecord.appeal_deadline = current_block + appeal_deadline_blocks` and re-index in `HideRecord/expiry/` (replaces the original auto-delete deadline with the appeal resolution deadline)
5. Emit `hide_appealed` event

**Resolution** (via x/rep jury callback `ResolveHideAppeal`):
- **Upheld (appellant wins — sentinel was wrong):**
  - Target restored to ACTIVE
  - Appellant receives 80% of `appeal_fee` (4 SPARK)
  - 20% burned (1 SPARK)
  - Sentinel slashed `sentinel_commit_amount` (100 DREAM) via x/forum keeper
  - If endorsed non-member collection: endorser's DREAM stake is NOT slashed (sentinel was wrong)
  - HideRecord marked `resolved = true`
  - Emit `hide_appeal_upheld` event

- **Rejected (sentinel wins — sentinel was right):**
  - Target deleted: for collections, full cleanup (items, collaborators, curation, sponsorship, endorsement). For items, remove and compact positions.
  - Deposit refunds follow standard deletion logic
  - Sentinel receives 50% of `appeal_fee` (2.5 SPARK)
  - 50% burned (2.5 SPARK)
  - If endorsed non-member collection: endorser's DREAM stake is slashed (burned)
  - HideRecord marked `resolved = true`
  - Emit `hide_appeal_rejected` event

- **Timeout (jury failed to resolve within `appeal_deadline_blocks`):**
  - Target restored to ACTIVE (favor appellant)
  - Appellant refunded 50% of `appeal_fee` (2.5 SPARK)
  - 50% burned (2.5 SPARK)
  - Sentinel committed bond released (no penalty)
  - HideRecord marked `resolved = true`
  - Emit `hide_appeal_timeout` event

### 5.27. MsgEndorseCollection

A member endorses a non-member's PENDING collection, making it publicly visible but immutable.

```protobuf
message MsgEndorseCollection {
  string creator = 1;
  uint64 collection_id = 2;
}

message MsgEndorseCollectionResponse {}
```

**Validation:**
- `creator` must be an active `x/rep` member
- Collection must exist and have status = PENDING
- Collection owner must NOT be an `x/rep` member (member collections don't need endorsement)
- `seeking_endorsement` must be `true`
- `creator` must NOT be the collection owner
- Collection must not already be endorsed

**Logic:**
1. Stake `endorsement_dream_stake` (100 DREAM) from `creator` via x/rep keeper
2. Create `Endorsement` record with `stake_release_at = current_block + endorsement_stake_duration`
3. Set collection `status = ACTIVE`, `endorsed_by = creator`, `immutable = true`
4. Split the escrowed creation fee (`endorsement_creation_fee`):
   - 80% (`endorsement_fee_endorser_share`) sent from module account to `creator`
   - 20% burned from module account
5. Emit `collection_endorsed` event

### 5.28. MsgSetSeekingEndorsement

Non-member collection owner signals that their collection is ready for endorsement review.

```protobuf
message MsgSetSeekingEndorsement {
  string creator = 1;
  uint64 collection_id = 2;
  bool seeking = 3;                 // true = ready for endorsement, false = not ready
}

message MsgSetSeekingEndorsementResponse {}
```

**Validation:**
- `creator` must be the collection owner
- Collection must have status = PENDING (not yet endorsed)
- Collection must not already be endorsed

**Logic:**
1. Set `seeking_endorsement = seeking` on the collection
2. Emit `seeking_endorsement_updated` event

### 5.29. MsgPinCollection

Makes an ephemeral collection permanent by burning its deposits. See §18.12 for full details.

```protobuf
message MsgPinCollection {
  string creator = 1;           // Member pinning the collection
  uint64 collection_id = 2;
}

message MsgPinCollectionResponse {}
```

**Validation:**
1. `creator` must be an active x/rep member at or above `pin_min_trust_level`
2. Collection must exist and have `expires_at > 0` (TTL collection)
3. Collection must have `status = ACTIVE`
4. `creator` must not exceed `max_pins_per_day` (rolling 24h window)

**Logic:**
1. Set `expires_at = 0` (permanent)
2. If `conviction_sustained == true`: set `conviction_sustained = false`
3. Burn the held collection deposit + item deposits from module account (`deposit_burned = true`)
4. Remove from expiry index
5. Emit `collection_pinned` event with `collection_id`, `pinned_by`

---

## 6. Queries

| Query | Input | Output | Description |
|-------|-------|--------|-------------|
| `Collection` | `uint64 id` | `Collection` | Get collection by ID (any visibility) |
| `CollectionsByOwner` | `string owner`, pagination | `[]Collection` | All collections for an owner |
| `PublicCollections` | pagination | `[]Collection` | Public collections only |
| `PublicCollectionsByType` | `CollectionType`, pagination | `[]Collection` | Public, filtered by type |
| `CollectionsByCollaborator` | `string address`, pagination | `[]Collection` | Collections where address collaborates |
| `Item` | `uint64 id` | `Item` | Single item |
| `Items` | `uint64 collection_id`, pagination | `[]Item` | Items in collection (by position) |
| `ItemsByOwner` | `string owner`, pagination | `[]Item` | All items across owner's collections |
| `Collaborators` | `uint64 collection_id` | `[]Collaborator` | Collaborators for a collection |
| `Curator` | `string address` | `Curator` | Single curator record |
| `ActiveCurators` | pagination | `[]Curator` | All active curators |
| `CurationSummary` | `uint64 collection_id` | `CurationSummary` | Aggregate quality signal |
| `CurationReviews` | `uint64 collection_id`, pagination | `[]CurationReview` | Reviews for a collection |
| `CurationReviewsByCurator` | `string curator`, pagination | `[]CurationReview` | All reviews by a curator |
| `SponsorshipRequest` | `uint64 collection_id` | `SponsorshipRequest` | Pending sponsorship request for a collection |
| `SponsorshipRequests` | pagination | `[]SponsorshipRequest` | All pending sponsorship requests (for discovery by potential sponsors) |
| `ContentFlag` | `uint64 target_id`, `FlagTargetType` | `CollectionFlag` | Flag state for a collection or item |
| `FlaggedContent` | pagination | `[]CollectionFlag` | All content in the sentinel review queue (`in_review_queue = true`) |
| `HideRecord` | `uint64 id` | `HideRecord` | Single hide record by ID |
| `HideRecordsByTarget` | `uint64 target_id`, `FlagTargetType` | `[]HideRecord` | All hide records for a target |
| `PendingCollections` | pagination | `[]Collection` | Non-member collections with `seeking_endorsement = true` (endorsement discovery feed) |
| `Endorsement` | `uint64 collection_id` | `Endorsement` | Endorsement record for a collection |
| `CollectionConviction` | `uint64 collection_id` | `ConvictionResponse` | Current conviction score, stake count, total staked, and author bond for any collection (delegates to x/rep) |
| ~~`AnonymousCollections`~~ | — | — | **REMOVED** — not implemented. Anonymous collections can be found via `CollectionsByOwner` with the module account address |
| ~~`IsCollectNullifierUsed`~~ | — | — | **REMOVED** — nullifier queries are now centralized in x/shield (`QueryNullifierUsed`) |
| `CollectionsByContent` | `string module`, `string entity_type`, `string entity_id`, pagination | `[]Collection` | Collections containing items with the given `OnChainReference` (uses `Item/by_onchain_ref` index) |
| `Params` | — | `Params` | Current parameters |

```protobuf
message ConvictionResponse {
  string conviction_score = 1 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  uint32 stake_count = 2;
  string total_staked = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string author_bond = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}
```

**Conviction ranking:** `PublicCollections` and `PublicCollectionsByType` support an optional `order_by` field. When `order_by = CONVICTION`, results are ranked by conviction score (highest first). Clients can combine conviction score with `CurationSummary` for composite ranking.

**Privacy note:** `Collection` by ID returns any collection (including HIDDEN, with status visible). Privacy is enforced by encryption, not query restriction. `PublicCollections` filters to `VISIBILITY_PUBLIC` + `status = ACTIVE` only.

**Sponsorship discovery:** `SponsorshipRequests` is the primary way for members to discover collections seeking sponsorship. Clients can combine this with `CurationSummary` to prioritize well-rated collections.

**Endorsement discovery:** `PendingCollections` returns non-member collections where `seeking_endorsement = true`. This is the primary feed for members looking to endorse promising non-member content. Members browse this feed voluntarily — there is no moderator obligation.

**Moderation queries:** `FlaggedContent` returns content that has reached the flag review threshold. This is used by sentinels to find content requiring moderation attention. `HideRecordsByTarget` provides the full moderation history for a target.

---

## 7. Keeper Interface

```go
type Keeper interface {
    // Collection CRUD
    CreateCollection(ctx context.Context, msg *MsgCreateCollection) (uint64, error)
    UpdateCollection(ctx context.Context, msg *MsgUpdateCollection) error
    DeleteCollection(ctx context.Context, owner string, id uint64) error
    GetCollection(ctx context.Context, id uint64) (Collection, error)
    GetCollectionsByOwner(ctx context.Context, owner string, pagination *query.PageRequest) ([]Collection, *query.PageResponse, error)
    GetPublicCollections(ctx context.Context, pagination *query.PageRequest) ([]Collection, *query.PageResponse, error)
    GetPublicCollectionsByType(ctx context.Context, ctype CollectionType, pagination *query.PageRequest) ([]Collection, *query.PageResponse, error)
    GetCollectionsByCollaborator(ctx context.Context, address string, pagination *query.PageRequest) ([]Collection, *query.PageResponse, error)
    GetMaxCollections(ctx context.Context, address string) (uint32, error)  // Resolve tiered limit

    // Item CRUD
    AddItem(ctx context.Context, msg *MsgAddItem) (uint64, error)
    AddItems(ctx context.Context, msg *MsgAddItems) ([]uint64, error)
    UpdateItem(ctx context.Context, msg *MsgUpdateItem) error
    RemoveItem(ctx context.Context, creator string, id uint64) error
    RemoveItems(ctx context.Context, msg *MsgRemoveItems) error
    ReorderItem(ctx context.Context, creator string, id uint64, newPosition uint64) error
    GetItem(ctx context.Context, id uint64) (Item, error)
    GetItems(ctx context.Context, collectionID uint64, pagination *query.PageRequest) ([]Item, *query.PageResponse, error)
    GetItemsByOwner(ctx context.Context, owner string, pagination *query.PageRequest) ([]Item, *query.PageResponse, error)

    // Collaborators
    AddCollaborator(ctx context.Context, msg *MsgAddCollaborator) error
    RemoveCollaborator(ctx context.Context, msg *MsgRemoveCollaborator) error
    UpdateCollaboratorRole(ctx context.Context, msg *MsgUpdateCollaboratorRole) error
    GetCollaborators(ctx context.Context, collectionID uint64) ([]Collaborator, error)
    IsCollaborator(ctx context.Context, collectionID uint64, address string) (bool, CollaboratorRole, error)
    HasWriteAccess(ctx context.Context, collectionID uint64, address string) (bool, error)

    // Curation
    RegisterCurator(ctx context.Context, msg *MsgRegisterCurator) error
    UnregisterCurator(ctx context.Context, creator string) error
    RateCollection(ctx context.Context, msg *MsgRateCollection) (uint64, error)
    ChallengeReview(ctx context.Context, msg *MsgChallengeReview) error
    ResolveChallengeResult(ctx context.Context, reviewID uint64, upheld bool) error
    GetCurator(ctx context.Context, address string) (Curator, error)
    GetActiveCurators(ctx context.Context, pagination *query.PageRequest) ([]Curator, *query.PageResponse, error)
    GetCurationSummary(ctx context.Context, collectionID uint64) (CurationSummary, error)
    GetCurationReviews(ctx context.Context, collectionID uint64, pagination *query.PageRequest) ([]CurationReview, *query.PageResponse, error)
    GetCurationReviewsByCurator(ctx context.Context, curator string, pagination *query.PageRequest) ([]CurationReview, *query.PageResponse, error)
    RecalculateSummary(ctx context.Context, collectionID uint64) error
    CleanupCollectionCuration(ctx context.Context, collectionID uint64) error  // Refund pending challenge deposits, update curator state, then delete all reviews + summary

    // Sponsorship
    RequestSponsorship(ctx context.Context, msg *MsgRequestSponsorship) error
    CancelSponsorshipRequest(ctx context.Context, creator string, collectionID uint64) error
    SponsorCollection(ctx context.Context, msg *MsgSponsorCollection) error
    GetSponsorshipRequest(ctx context.Context, collectionID uint64) (SponsorshipRequest, error)
    GetSponsorshipRequests(ctx context.Context, pagination *query.PageRequest) ([]SponsorshipRequest, *query.PageResponse, error)

    // Reactions
    UpvoteContent(ctx context.Context, msg *MsgUpvoteContent) error
    DownvoteContent(ctx context.Context, msg *MsgDownvoteContent) error

    // Flagging
    FlagContent(ctx context.Context, msg *MsgFlagContent) error
    GetContentFlag(ctx context.Context, targetID uint64, targetType FlagTargetType) (CollectionFlag, error)
    GetFlaggedContent(ctx context.Context, pagination *query.PageRequest) ([]CollectionFlag, *query.PageResponse, error)
    PruneExpiredFlags(ctx context.Context, currentBlock int64) (uint64, error)

    // Sentinel moderation
    HideContent(ctx context.Context, msg *MsgHideContent) error
    ResolveHideAppeal(ctx context.Context, hideRecordID uint64, upheld bool) error   // x/rep jury callback
    GetHideRecord(ctx context.Context, id uint64) (HideRecord, error)
    GetHideRecordsByTarget(ctx context.Context, targetID uint64, targetType FlagTargetType) ([]HideRecord, error)
    PruneUnappealedHides(ctx context.Context, currentBlock int64) (uint64, error)
    ResolveAppealTimeouts(ctx context.Context, currentBlock int64) (uint64, error)

    // Appeals
    AppealHide(ctx context.Context, msg *MsgAppealHide) error

    // Endorsement
    EndorseCollection(ctx context.Context, msg *MsgEndorseCollection) error
    SetSeekingEndorsement(ctx context.Context, msg *MsgSetSeekingEndorsement) error
    GetEndorsement(ctx context.Context, collectionID uint64) (Endorsement, error)
    GetPendingCollections(ctx context.Context, pagination *query.PageRequest) ([]Collection, *query.PageResponse, error)
    ReleaseExpiredEndorsementStakes(ctx context.Context, currentBlock int64) (uint64, error)
    PruneUnendorsedCollections(ctx context.Context, currentBlock int64) (uint64, error)

    // Membership transition (called by x/rep on new member)
    OnMembershipGranted(ctx context.Context, address string) error

    // TTL / expiry
    PruneExpired(ctx context.Context, currentBlock int64) (uint64, error)
    PruneExpiredSponsorshipRequests(ctx context.Context, currentBlock int64) (uint64, error)

    // Deposits
    CollectCollectionDeposit(ctx context.Context, creator string, expiresAt int64) error
    CollectItemDeposit(ctx context.Context, creator string, collection Collection, count uint64) error
    RefundCollectionDeposit(ctx context.Context, collection Collection) error
    RefundItemDeposit(ctx context.Context, collection Collection, count uint64) error

    // Params
    GetParams(ctx context.Context) (Params, error)
    SetParams(ctx context.Context, params Params) error
    UpdateOperationalParams(ctx context.Context, msg *MsgUpdateOperationalParams) error

    // Pinning
    PinCollection(ctx context.Context, msg *MsgPinCollection) error

    // Conviction (delegates to x/rep)
    GetCollectionConviction(ctx context.Context, collectionID uint64) (ConvictionResponse, error)

    // Council authorization (delegates to x/commons)
    IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool

    // ShieldAware interface (see x/collect/keeper/shield_aware.go)
    IsShieldCompatible(ctx context.Context, msg sdk.Msg) bool
}
```

---

## 8. Parameters

**Gov** = governance-only (`MsgUpdateParams`). **Ops** = operational (`MsgUpdateOperationalParams` via Operations Committee).

| Parameter | Default | Gov/Ops | Rationale |
|-----------|---------|---------|-----------|
| `max_collections_base` | `5` | Gov | Non-member collection limit |
| `max_collections_per_trust_level` | `15` | Gov | Additional collections per trust level index |
| `max_items_per_collection` | `500` | Gov | Keeps individual collections manageable |
| `max_title_length` | `128` | Gov | Item title limit |
| `max_name_length` | `128` | Gov | Collection name limit |
| `max_description_length` | `1024` | Gov | Description limit (collections and items) |
| `max_tag_length` | `32` | Gov | Per-tag character limit |
| `max_tags_per_collection` | `10` | Gov | Tag count limit |
| `max_attributes_per_item` | `20` | Gov | Attribute count limit (shared with CustomReference.extra) |
| `max_attribute_key_length` | `64` | Gov | Key length limit |
| `max_attribute_value_length` | `256` | Gov | Value length limit |
| `max_reference_field_length` | `256` | Gov | Per-field limit for all reference type strings |
| `max_encrypted_data_size` | `4096` | Gov | Max bytes per encrypted blob |
| `max_collaborators_per_collection` | `20` | Gov | Collaborator count limit |
| `max_batch_size` | `50` | Gov | Batch operation cap |
| `max_ttl_blocks` | `0` | Gov | Max TTL in blocks (0 = unlimited) |
| `max_non_member_ttl_blocks` | `432000` (~30 days) | Gov | Max TTL for non-member collections |
| `max_prune_per_block` | `100` | Gov | EndBlocker prune cap per block |
| `base_collection_deposit` | `1000000usprkdrm` (1 SPARK) | Ops | Per-collection storage deposit |
| `per_item_deposit` | `100000usprkdrm` (0.1 SPARK) | Ops | Per-item storage deposit; scales with collection size |
| `per_item_spam_tax` | `500000usprkdrm` (0.5 SPARK) | Ops | Non-member per-item addition surcharge; always burned |
| `sponsor_fee` | `1000000usprkdrm` (1 SPARK) | Ops | Fee paid by sponsor (burned) |
| `min_sponsor_trust_level` | `TRUST_LEVEL_ESTABLISHED` | Ops | Min trust level to sponsor a collection |
| `sponsorship_request_ttl_blocks` | `100800` (~7 days) | Ops | How long a request stays open before auto-expiry |
| `min_curator_bond` | `500` DREAM | Ops | Min DREAM bond for curators |
| `min_curator_trust_level` | `TRUST_LEVEL_PROVISIONAL` | Ops | Adjustable by council based on community needs |
| `min_curator_age_blocks` | `14400` (~1 day) | Ops | Cooldown after registration before first review |
| `max_tags_per_review` | `5` | Ops | Review tag limit |
| `max_review_comment_length` | `512` | Ops | Review comment limit |
| `max_reviews_per_collection` | `20` | Ops | Prevents dogpiling |
| `curator_slash_fraction` | `0.10` (10%) | Ops | Bond slashed per successful challenge |
| `challenge_reward_fraction` | `0.80` (80%) | Ops | Fraction of slashed curator bond sent to challenger; remainder burned |
| `challenge_window_blocks` | `100800` (~7 days) | Ops | Challenge submission window |
| `challenge_deposit` | `250` DREAM | Ops | Challenge deposit; refunded if upheld, burned if rejected |
| `max_challenge_reason_length` | `1024` | Ops | Reason field limit |
| `downvote_cost` | `25000000usprkdrm` (25 SPARK) | Ops | SPARK burned per downvote; high cost ensures serious signal |
| `max_upvotes_per_day` | `100` | Ops | Rate limit for upvotes per member per rolling 24h |
| `max_downvotes_per_day` | `20` | Ops | Rate limit for downvotes per member per rolling 24h |
| `flag_review_threshold` | `5` | Ops | Total flag weight to enter sentinel review queue (member flags weight 2 each) |
| `max_flags_per_day` | `20` | Ops | Max flags per member per rolling 24h |
| `max_flaggers_per_target` | `50` | Ops | Max flag records tracked per target |
| `flag_expiration_blocks` | `100800` (~7 days) | Ops | Flags expire after this many blocks |
| `max_flag_reason_length` | `512` | Ops | Custom reason text limit for OTHER |
| `sentinel_commit_amount` | `100` DREAM | Ops | DREAM committed per hide from sentinel bond |
| `hide_expiry_blocks` | `100800` (~7 days) | Ops | Blocks before auto-deletion if no appeal |
| `appeal_fee` | `5000000usprkdrm` (5 SPARK) | Ops | SPARK fee to appeal a hide (escrowed) |
| `appeal_cooldown_blocks` | `600` (~1 hour) | Ops | Blocks after hide before appeal is allowed |
| `appeal_deadline_blocks` | `201600` (~14 days) | Ops | Max blocks for jury to resolve appeal |
| `endorsement_creation_fee` | `10000000usprkdrm` (10 SPARK) | Ops | Non-member collection creation fee (80% to endorser, 20% burned) |
| `endorsement_dream_stake` | `100` DREAM | Ops | DREAM staked by endorser (returned after clean period, slashed if hidden) |
| `endorsement_stake_duration` | `432000` (~30 days) | Ops | Blocks before endorser stake is released |
| `endorsement_expiry_blocks` | `432000` (~30 days) | Ops | Blocks before unendorsed collection auto-prunes |
| `endorsement_fee_endorser_share` | `0.80` (80%) | Ops | Fraction of creation fee sent to endorser |
| `endorsement_deletion_burn_fraction` | `0.10` (10%) | Ops | Fraction of endorsement fee burned when owner deletes PENDING collection (remainder refunded) |
| `conviction_renewal_threshold` | `0` (disabled) | Ops | Min conviction score to renew anonymous collections at TTL expiry (0 = disabled) |
| `conviction_renewal_period` | `432000` (~30 days) | Ops | Blocks to extend TTL by when conviction-renewed |
| `pin_min_trust_level` | `2` (ESTABLISHED) | Ops | Minimum trust level to pin anonymous collections |
| `max_pins_per_day` | `10` | Ops | Max pins per address per rolling 24h |

---

## 9. Genesis State

```protobuf
message GenesisState {
  Params params = 1;
  repeated Collection collections = 2;
  uint64 collection_count = 3;
  repeated Item items = 4;
  uint64 item_count = 5;
  repeated Collaborator collaborators = 6;
  repeated Curator curators = 7;
  repeated CurationReview curation_reviews = 8;
  uint64 curation_review_count = 9;
  repeated CurationSummary curation_summaries = 10;
  repeated SponsorshipRequest sponsorship_requests = 11;
  repeated CollectionFlag flags = 12;
  repeated HideRecord hide_records = 13;
  uint64 hide_record_count = 14;
  repeated Endorsement endorsements = 15;
}
```

---

## 10. Module Lifecycle

| Hook | Behavior |
|------|----------|
| `InitGenesis` | Import params, collections, items, collaborators, curators, reviews, summaries, sponsorship requests, and all counters |
| `ExportGenesis` | Export all state |
| `BeginBlock` | None |
| `EndBlock` | Prune expired collections/sponsorship requests (10.1), unappealed hides (10.3), appeal timeouts (10.3a), expired flags (10.4), unendorsed collections (10.5), release endorsement stakes (10.6) |

### 10.1. EndBlocker: TTL Pruning

Each block, the EndBlocker processes expiry indexes up to `max_prune_per_block` entries total:

```
pruned = 0
for each expired collection expiry index entry (where expires_at ≤ current_block and pruned < max_prune_per_block):
    if Collection no longer exists (already deleted by owner via MsgDeleteCollection):
        delete stale expiry index entry only
    else:
        // --- Conviction check (anonymous collections only) ---
        if collection.owner == module_account_address AND params.conviction_renewal_threshold > 0:
            conviction = repKeeper.GetContentConviction(ctx, "collect/collection/{id}")
            if conviction.Score >= params.conviction_renewal_threshold:
                new_expires_at = current_block + params.conviction_renewal_period
                update collection.expires_at = new_expires_at
                remove old expiry index entry
                add new expiry index entry at new_expires_at
                if NOT collection.conviction_sustained:
                    // First entry into conviction-sustained state
                    collection.conviction_sustained = true
                    emit collection_conviction_sustained event
                else:
                    // Subsequent renewal
                    emit collection_renewed event (conviction_score, new_expires_at)
                pruned += 1
                continue   // skip deletion — collection survives
            else:
                // Conviction below threshold — clear sustained flag
                if collection.conviction_sustained:
                    collection.conviction_sustained = false
                // Fall through to normal deletion below

        if deposit_burned = false:
            refund deposit_amount + item_deposit_total from module account to owner
        if pending SponsorshipRequest exists for this collection:
            refund escrowed deposits from module account to requester
            delete SponsorshipRequest and its expiry index entry
        if status = PENDING (non-member collection):
            refund escrowed endorsement_creation_fee (minus endorsement_deletion_burn_fraction) to owner
            delete Endorsement/pending/ index entry
        if endorsed_by is not empty and endorser stake not yet released:
            release endorser's DREAM stake via x/rep keeper
            delete Endorsement/expiry/ index entry
        if active hide appeal exists (HideRecord with appealed = true, resolved = false):
            burn escrowed appeal_fee
            release sentinel's bond commitment (no slash)
        delete all items in collection
        delete all collaborators for collection
        for each curation review where challenged = true and overturned = false:
            refund challenge_deposit from module account to challenger
            decrement pending_challenges on reviewed curator
        delete all curation reviews and summary for collection
        delete all flags, hide records, and endorsement records for collection
        delete collection
        emit collection_expired event
        pruned += item_count + 1   // each item deletion + the collection itself
        continue
    pruned += 1

for each expired sponsorship request index entry (where expires_at ≤ current_block and pruned < max_prune_per_block):
    if SponsorshipRequest no longer exists (already deleted by collection cleanup, cancellation, or sponsorship):
        delete stale expiry index entry only
    else:
        refund escrowed collection_deposit + item_deposit_total from module account to requester
        delete SponsorshipRequest and expiry index entry
        emit sponsorship_request_expired event
    pruned += 1
```

Remaining expired entries are processed in subsequent blocks. The `max_prune_per_block` cap ensures the EndBlocker never consumes unbounded gas.

### 10.2. Position Compaction

When items are removed (single or batch via MsgRemoveItem/MsgRemoveItems), positions are compacted in a **single pass** over the remaining items for each affected collection. This avoids O(N²) per-item reshuffling:

1. Iterate items in position order for the affected collection
2. Assign new sequential positions starting from 0
3. Update the position index entries

For batch removes (MsgRemoveItems), all removals happen first, then one compaction pass runs.

### 10.3. EndBlocker: Unappealed Hide Expiry

Each block, processes the hide expiry index for hide records where `appeal_deadline ≤ current_block` and `appealed = false` and `resolved = false`:

1. Delete the hidden content (collection with full cleanup, or item with position compaction)
2. Refund deposits per standard deletion logic
3. Release sentinel's committed bond (no penalty — content was not appealed, implying owner accepted the hide)
4. Mark HideRecord `resolved = true`
5. Emit `unappealed_hide_expired` event

Respects `max_prune_per_block` cap shared with TTL pruning.

### 10.3a. EndBlocker: Appeal Timeout

Each block, processes the hide expiry index for hide records where `appeal_deadline ≤ current_block` and `appealed = true` and `resolved = false`:

1. Restore hidden content to ACTIVE (favor appellant — jury failed to resolve in time)
2. Refund 50% of `appeal_fee` to appellant (2.5 SPARK)
3. Burn 50% of `appeal_fee` (2.5 SPARK)
5. Release sentinel's committed bond (no penalty — jury timed out, not a verdict against sentinel)
6. Mark HideRecord `resolved = true`
7. Emit `hide_appeal_timeout` event

Respects `max_prune_per_block` cap shared with other EndBlocker tasks.

### 10.4. EndBlocker: Flag Expiry

Prunes expired flags where `last_flag_at + flag_expiration_blocks ≤ current_block`:

1. Delete `CollectionFlag` record and all index entries
2. Emit `flags_expired` event

### 10.5. EndBlocker: Unendorsed Collection Pruning

Processes the `Endorsement/pending/` index for entries where `endorsement_expiry ≤ current_block`:

1. If the collection no longer exists (already deleted by TTL pruning §10.1 or owner deletion): delete stale index entry only
2. Otherwise:
   - Refund escrowed `endorsement_creation_fee` to the collection owner (minus `endorsement_deletion_burn_fraction` burned)
   - Refund TTL deposits per standard deletion logic
   - Delete collection and all associated state (items, collaborators)
   - Emit `unendorsed_collection_pruned` event

### 10.6. EndBlocker: Endorsement Stake Release

Releases endorser DREAM stakes where `stake_release_at ≤ current_block` and `stake_released = false`:

1. Release `endorsement_dream_stake` to endorser via x/rep keeper
2. Set `stake_released = true` on the Endorsement record
3. Emit `endorsement_stake_released` event

---

## 11. Access Control

| Operation | Who Can Execute |
|-----------|----------------|
| Create collection (member) | x/rep members: TTL or permanent, no endorsement needed |
| Create collection (non-member) | Any account: TTL only, pays `endorsement_creation_fee` (escrowed), status = PENDING until endorsed |
| Create permanent collection | x/rep members only (or via sponsorship) |
| Update collection | Owner only (non-members cannot convert TTL→permanent; immutable collections blocked) |
| Delete collection | Owner only |
| Add item | Owner, EDITOR, ADMIN (blocked during pending sponsorship; blocked if collection immutable) |
| Update item | Owner, EDITOR, ADMIN (blocked if collection immutable) |
| Remove item | Owner, EDITOR, ADMIN (blocked during pending sponsorship; blocked if collection immutable) |
| Reorder item | Owner, EDITOR, ADMIN (blocked if collection immutable) |
| Add collaborator | Owner, ADMIN (blocked if collection immutable) |
| Remove collaborator | Owner, ADMIN (ADMIN cannot remove other ADMINs), or self |
| Update collaborator role | Owner, ADMIN (only owner can grant/revoke ADMIN) |
| Set seeking endorsement | Non-member collection owner (PENDING collections only) |
| Endorse collection | Any x/rep member (not the collection owner) |
| Request sponsorship | Non-member collection owner (TTL collections only) |
| Cancel sponsorship request | Collection owner (with pending request) |
| Sponsor a collection | x/rep member at `min_sponsor_trust_level`+ (not the collection owner) |
| Register as curator | x/rep member at `min_curator_trust_level`+ |
| Unregister as curator | Curator (no pending challenges) |
| Rate a collection | Active curator with age ≥ `min_curator_age_blocks` (not owner/collaborator; requires `community_feedback_enabled`) |
| Challenge a review | Any x/rep member (not the review's curator) |
| Upvote | Any x/rep member (not owner/collaborator; requires `community_feedback_enabled`) |
| Downvote | Any x/rep member (not owner/collaborator; requires `community_feedback_enabled`; burns 25 SPARK) |
| Flag content | Any x/rep member (public content only) |
| Hide content | Active x/forum sentinel (bonded, not demoted, not in cooldown) |
| Appeal hide | Owner of hidden content (pays 5 SPARK) |
| Pin collection | Active x/rep member at `pin_min_trust_level`+ |
| Update params | Governance (`x/gov`) |
| Update operational params | Commons Operations Committee (via `x/commons`), or governance |

---

## 12. Events

| Event | Attributes | Trigger |
|-------|------------|---------|
| `collection_created` | `id`, `owner`, `type`, `visibility`, `encrypted`, `expires_at`, `deposit_amount`, `endorsement_fee_escrowed` | MsgCreateCollection |
| `collection_updated` | `id`, `owner`, `expires_at`, `deposit_burned` | MsgUpdateCollection |
| `collection_deleted` | `id`, `owner`, `item_count`, `deposit_refunded`, `item_deposit_refunded` | MsgDeleteCollection |
| `collection_conviction_sustained` | `id`, `conviction_score`, `new_expires_at` | EndBlocker: first entry into conviction-sustained state |
| `collection_renewed` | `id`, `conviction_score`, `new_expires_at` | EndBlocker: subsequent conviction renewal |
| `collection_expired` | `id`, `owner`, `item_count`, `deposit_refunded`, `item_deposit_refunded` | EndBlocker |
| `deposit_burned` | `collection_id`, `owner`, `collection_deposit`, `item_deposit_total` | Permanent creation or TTL→permanent conversion |
| `item_added` | `id`, `collection_id`, `added_by`, `reference_type`, `position`, `deposit`, `per_item_spam_tax_burned` | MsgAddItem |
| `items_added` | `collection_id`, `added_by`, `count`, `total_deposit`, `per_item_spam_tax_burned` | MsgAddItems |
| `item_updated` | `id`, `collection_id`, `creator` | MsgUpdateItem |
| `item_removed` | `id`, `collection_id`, `creator`, `deposit_refunded` | MsgRemoveItem |
| `items_removed` | `collection_id`, `creator`, `count`, `deposit_refunded` | MsgRemoveItems |
| `item_reordered` | `id`, `collection_id`, `old_position`, `new_position` | MsgReorderItem |
| `collaborator_added` | `collection_id`, `address`, `role`, `added_by` | MsgAddCollaborator |
| `collaborator_removed` | `collection_id`, `address`, `removed_by` | MsgRemoveCollaborator |
| `collaborator_role_updated` | `collection_id`, `address`, `old_role`, `new_role` | MsgUpdateCollaboratorRole |
| `curator_registered` | `address`, `bond_amount` | MsgRegisterCurator |
| `curator_unregistered` | `address`, `bond_refunded` | MsgUnregisterCurator |
| `curator_deactivated` | `address`, `reason` | Bond fell below min_curator_bond after slash |
| `collection_rated` | `review_id`, `collection_id`, `curator`, `verdict`, `tags` | MsgRateCollection |
| `review_challenged` | `review_id`, `challenger` | MsgChallengeReview |
| `challenge_resolved` | `review_id`, `upheld`, `curator`, `slash_amount`, `reward_amount`, `burn_amount`, `curator_deactivated`, `challenger_refunded` | x/rep jury callback |
| `sponsorship_requested` | `collection_id`, `requester`, `escrowed_deposit`, `expires_at` | MsgRequestSponsorship |
| `sponsorship_cancelled` | `collection_id`, `requester`, `deposit_refunded` | MsgCancelSponsorshipRequest |
| `collection_sponsored` | `collection_id`, `owner`, `sponsored_by`, `sponsor_fee_burned`, `permanent_deposit_burned`, `ttl_deposit_refunded` | MsgSponsorCollection |
| `sponsorship_request_expired` | `collection_id`, `requester`, `deposit_refunded` | EndBlocker |
| `operational_params_updated` | `authority` | MsgUpdateOperationalParams |
| `content_upvoted` | `target_id`, `target_type`, `voter` | MsgUpvoteContent |
| `content_downvoted` | `target_id`, `target_type`, `voter`, `cost_burned` | MsgDownvoteContent |
| `content_flagged` | `target_id`, `target_type`, `flagger`, `reason`, `total_weight`, `in_review_queue` | MsgFlagContent |
| `content_hidden` | `target_id`, `target_type`, `sentinel`, `hide_record_id`, `reason_code`, `committed_amount` | MsgHideContent |
| `hide_appealed` | `hide_record_id`, `appellant`, `appeal_fee` | MsgAppealHide |
| `hide_appeal_upheld` | `hide_record_id`, `target_id`, `target_type`, `sentinel_slashed`, `appellant_refund` | x/rep jury callback |
| `hide_appeal_rejected` | `hide_record_id`, `target_id`, `target_type`, `sentinel_reward`, `target_deleted` | x/rep jury callback |
| `hide_appeal_timeout` | `hide_record_id`, `target_id`, `target_type`, `appellant_refund` | EndBlocker |
| `unappealed_hide_expired` | `hide_record_id`, `target_id`, `target_type`, `target_deleted` | EndBlocker |
| `collection_endorsed` | `collection_id`, `endorser`, `dream_staked`, `endorser_reward` | MsgEndorseCollection |
| `seeking_endorsement_updated` | `collection_id`, `seeking` | MsgSetSeekingEndorsement |
| `endorsement_stake_released` | `collection_id`, `endorser`, `amount` | EndBlocker |
| `endorsement_stake_slashed` | `collection_id`, `endorser`, `amount` | Sentinel hide during stake period |
| `unendorsed_collection_pruned` | `collection_id`, `owner`, `deposit_refunded` | EndBlocker |
| `flags_expired` | `target_id`, `target_type` | EndBlocker |
| `collection_pinned` | `collection_id`, `pinned_by` | MsgPinCollection |

---

## 13. Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrCollectionNotFound` | 1100 | Collection ID does not exist |
| `ErrItemNotFound` | 1101 | Item ID does not exist |
| `ErrUnauthorized` | 1102 | Caller lacks permission |
| `ErrMaxCollections` | 1103 | Owner has reached tiered collection limit |
| `ErrMaxItems` | 1104 | Collection has reached `max_items_per_collection` |
| `ErrInvalidName` | 1105 | Name empty or exceeds `max_name_length` |
| `ErrInvalidTitle` | 1106 | Title empty or exceeds `max_title_length` |
| `ErrInvalidDescription` | 1107 | Description exceeds `max_description_length` |
| `ErrInvalidReference` | 1108 | Reference type/data mismatch |
| `ErrReferenceFieldTooLong` | 1109 | Reference string field exceeds `max_reference_field_length` |
| `ErrInvalidPosition` | 1110 | Position out of range |
| `ErrTagTooLong` | 1111 | Tag exceeds `max_tag_length` |
| `ErrMaxTags` | 1112 | Tag count exceeds limit |
| `ErrMaxAttributes` | 1113 | Attribute count exceeds `max_attributes_per_item` |
| `ErrAttributeTooLong` | 1114 | Attribute key/value exceeds length limit |
| `ErrMaxCollaborators` | 1115 | Collection at `max_collaborators_per_collection` |
| `ErrNotMember` | 1116 | Address is not an active x/rep member |
| `ErrAlreadyCollaborator` | 1117 | Already a collaborator |
| `ErrCannotCollaborateSelf` | 1118 | Cannot add owner as collaborator |
| `ErrBatchTooLarge` | 1119 | Batch size exceeds `max_batch_size` |
| `ErrInvalidExpiry` | 1120 | Expiry in past or exceeds `max_ttl_blocks` |
| `ErrCannotChangeEncryption` | 1121 | `encrypted` is immutable |
| `ErrCannotChangeVisibility` | 1122 | `visibility` is immutable |
| `ErrEncryptedRequiresPrivate` | 1123 | `encrypted = true` requires `VISIBILITY_PRIVATE` |
| `ErrPrivateRequiresEncryption` | 1124 | `VISIBILITY_PRIVATE` requires `encrypted = true` |
| `ErrBatchItemsMixedCollections` | 1125 | Batch items span multiple collections |
| `ErrAdminRemoveAdmin` | 1126 | ADMIN cannot remove another ADMIN |
| `ErrAdminOnlyOwner` | 1127 | Only owner can grant/revoke ADMIN |
| `ErrInsufficientFunds` | 1128 | Insufficient SPARK for deposit + fees |
| `ErrEncryptedDataTooLarge` | 1129 | Blob exceeds `max_encrypted_data_size` |
| `ErrEncryptedFieldMismatch` | 1130 | Structured fields on encrypted collection or blob on public |
| `ErrNotCurator` | 1131 | Not a registered active curator |
| `ErrAlreadyCurator` | 1132 | Already registered |
| `ErrInsufficientBond` | 1133 | Bond below `min_curator_bond` |
| `ErrTrustLevelTooLow` | 1134 | Below `min_curator_trust_level` |
| `ErrCuratorTooNew` | 1135 | Curator registered less than `min_curator_age_blocks` ago |
| `ErrCannotRateOwnCollection` | 1136 | Curator is owner/collaborator |
| `ErrAlreadyReviewed` | 1137 | One active (non-overturned) review per curator per collection |
| `ErrMaxReviews` | 1138 | Collection at `max_reviews_per_collection` |
| `ErrCannotRatePrivate` | 1139 | Cannot rate private collections |
| `ErrReviewNotFound` | 1140 | Review ID does not exist |
| `ErrReviewAlreadyChallenged` | 1141 | Review already challenged |
| `ErrChallengeWindowExpired` | 1142 | Past `challenge_window_blocks` |
| `ErrCannotChallengeSelf` | 1143 | Cannot challenge own review |
| `ErrReasonTooLong` | 1144 | Challenge reason exceeds `max_challenge_reason_length` |
| `ErrCuratorHasPendingChallenges` | 1145 | Cannot unregister with pending challenges |
| `ErrCuratorBondInsufficient` | 1146 | Curator bond dropped below `min_curator_bond` (auto-deactivated) |
| `ErrNonMemberPermanent` | 1147 | Non-members cannot create permanent collections (must use TTL + sponsorship) |
| `ErrNonMemberTTLExceeded` | 1148 | Non-member TTL exceeds `max_non_member_ttl_blocks` |
| `ErrSponsorshipRequestExists` | 1149 | Collection already has a pending sponsorship request |
| `ErrSponsorshipRequestNotFound` | 1150 | No pending sponsorship request for this collection |
| `ErrMemberCannotRequestSponsorship` | 1151 | Members can convert to permanent directly (no need for sponsorship) |
| `ErrCannotSponsorOwn` | 1152 | Sponsor cannot be the collection owner |
| `ErrSponsorTrustLevelTooLow` | 1153 | Sponsor below `min_sponsor_trust_level` |
| `ErrAlreadySponsored` | 1154 | Collection has already been sponsored |
| `ErrCollectionExpired` | 1155 | Collection has already expired |
| `ErrCollectionAlreadyPermanent` | 1156 | Collection is already permanent (no sponsorship needed) |
| `ErrItemsLockedForSponsorship` | 1157 | Cannot add/remove items while sponsorship request is pending |
| `ErrDuplicateIDs` | 1158 | Batch contains duplicate item IDs |
| `ErrCannotRevertPermanent` | 1159 | Permanent collection cannot set `expires_at > 0` (deposits already burned) |
| `ErrCollectionImmutable` | 1160 | Collection is immutable (endorsed non-member collection; owner must become member to unlock) |
| `ErrNotSentinel` | 1161 | Caller is not an active x/forum sentinel |
| `ErrNotPublicActive` | 1162 | Target must be a public, active collection or item |
| `ErrSentinelInCooldown` | 1163 | Sentinel is in overturn cooldown |
| `ErrInsufficientBondAvailable` | 1164 | Sentinel bond insufficient for commit |
| `ErrAlreadyHidden` | 1165 | Content is already hidden |
| `ErrContentNotHidden` | 1166 | Target is not hidden (cannot appeal) |
| `ErrHideRecordNotFound` | 1167 | Hide record does not exist |
| `ErrAppealAlreadyFiled` | 1168 | Appeal already filed for this hide record |
| `ErrAppealCooldown` | 1169 | Must wait `appeal_cooldown_blocks` after hide before appealing |
| `ErrAppealDeadlinePassed` | 1170 | Appeal deadline has passed |
| `ErrAlreadyFlagged` | 1171 | Caller has already flagged this target |
| `ErrFlagRateLimitExceeded` | 1172 | Caller has exceeded `max_flags_per_day` |
| `ErrMaxDailyReactions` | 1173 | Daily reaction limit reached |
| `ErrDownvoteRateLimitExceeded` | 1174 | Downvote rate limit exceeded |
| `ErrNotContentOwner` | 1175 | Only content owner can appeal |
| `ErrCannotVoteOwnContent` | 1176 | Cannot vote on own content |
| `ErrCollectionNotPending` | 1177 | Collection is not in PENDING state (cannot endorse) |
| `ErrNotSeekingEndorsement` | 1178 | Collection is not seeking endorsement |
| `ErrAlreadyEndorsed` | 1179 | Collection has already been endorsed |
| `ErrCannotEndorseOwn` | 1180 | Cannot endorse own collection |
| `ErrEndorsementNotFound` | 1181 | No endorsement record for this collection |
| `ErrAlreadyVoted` | 1182 | Member has already upvoted or downvoted this target |
| `ErrCannotModifyPrivateContent` | 1183 | Cannot flag/hide/react to private/encrypted collections |
| `ErrHideRecordResolved` | 1184 | Hide record already resolved |
| `ErrInvalidFlagReason` | 1185 | Invalid or unspecified flag reason code |
| `ErrMaxFlagsPerTarget` | 1186 | Target has reached `max_flaggers_per_target` |
| `ErrFlagReasonTextRequired` | 1187 | `reason_text` required when `reason_code = OTHER` |
| `ErrFlagReasonTextTooLong` | 1188 | `reason_text` exceeds `max_flag_reason_length` |
| `ErrInvalidOnChainRef` | 1189 | Invalid on-chain reference: unknown `entity_type` or malformed `entity_id` |
| `ErrOnChainRefNotFound` | 1190 | On-chain referenced content not found |
| `ErrCannotPinActive` | 1214 | Collection is already permanent |
| `ErrPinTrustLevelTooLow` | 1215 | Below required pin trust level |
| `ErrInvalidInitiativeRef` | 1230 | Invalid initiative reference for conviction propagation |

**Rate-limit error note:** Errors 1172 (`ErrFlagRateLimitExceeded`), 1173 (`ErrMaxDailyReactions`), and 1174 (`ErrDownvoteRateLimitExceeded`) cover distinct rate-limiting aspects: flagging, general daily reactions, and downvotes respectively. Each has an independent daily counter tracked in `ReactionLimit/{address}/{day}`.

---

## 14. Security Considerations

### 14.1. State Bloat
All entities are bounded by governance-controlled parameters. Non-member collections are ephemeral by design (forced TTL). TTL expiry, per-item deposits, tiered collection limits, and sponsorship gating for permanence provide layered defense against state bloat.

### 14.2. Storage Economics
The per-item deposit (`per_item_deposit`) ensures that storage cost scales linearly with actual data stored. A max-size collection (500 items) costs 51 SPARK in deposits (1 collection + 50 item deposits). For permanent collections this is burned, directly compensating the network for perpetual storage. For TTL collections, the deposits are held (opportunity cost to the user) and refunded at expiry when state is reclaimed. Governance can adjust deposit amounts if SPARK price changes make them too cheap or too expensive relative to hardware costs.

### 14.3. No Custodial Claims
Collections are purely informational. Adding an NFT does not prove ownership. Clients should verify externally.

### 14.4. Content Moderation
Public collections and items are moderated through three on-chain layers: (1) member flagging with weight-based sentinel review queue, (2) sentinel hiding with bond commitment and appeal rights, and (3) expert curation by bonded curators. Private (encrypted) collections cannot be inspected or moderated by content — they are exempt from flagging, reactions, and sentinel moderation. Sentinel moderation applies to all public collections regardless of `community_feedback_enabled` — owners cannot opt out of policy enforcement.

### 14.5. Encryption and Privacy
Privacy is enforced by client-side encryption. Private collections store all content in a single `encrypted_data` blob — the chain cannot see individual fields, counts, or structure. Anyone can query by ID but gets useless ciphertext without the key. Key management is the frontend's responsibility.

### 14.6. Encrypted Blob Size
Ciphertext includes overhead (nonce, auth tag, padding). `max_encrypted_data_size` caps the stored size. Clients must account for overhead.

### 14.7. Encrypted Storage Limits
A single address can store at most: `max_collections × max_items_per_collection × max_encrypted_data_size` bytes. At defaults: 5 collections × 500 items × 4096 bytes = ~10MB for a non-member, ~130MB for a COUNCIL member. The per-item deposit ensures this storage is paid for: 500 items × 0.1 SPARK = 50 SPARK per collection. Non-member storage is further constrained: all non-member collections are ephemeral (TTL ≤ ~30 days), so non-member encrypted storage is always temporary and self-cleaning.

### 14.8. Collaborator Membership Lapse
Collaborator records persist after membership lapses, but write operations are rejected at execution time. Owner/ADMIN can remove stale collaborators.

### 14.9. Spam Prevention
Five layers: (1) non-member collections are ephemeral (self-cleaning), (2) storage deposits scale with data size, (3) non-member endorsement creation fee (10 SPARK escrowed) gates collection creation, (4) non-member per-item spam tax is burned on every item addition — ensuring the non-refundable cost scales linearly with items stored, (5) tiered collection limits cap non-members at `max_collections_base`. Together these make spam attacks expensive, bounded, and temporary.

### 14.10. EndBlocker DoS Prevention
`max_prune_per_block` caps the number of expired entries processed per block. If many entries expire simultaneously, cleanup is spread across multiple blocks. This prevents a single block from consuming excessive gas.

### 14.11. Position Compaction Cost
Removing items triggers position compaction. For a 500-item collection, this is O(500) in the worst case. Batch removes compact once (not per-item). The `max_items_per_collection` bounds the worst case. Lazy compaction (compacting only on next read/reorder rather than on every delete) is an implementation optimization that doesn't change the spec.

### 14.12. Curation Gaming
- **Sybil attack**: Each sybil curator costs `min_curator_bond` DREAM + must wait `min_curator_age_blocks` before rating. At defaults: 500 DREAM × 20 curators × ~1 day wait = 10,000 DREAM investment over 20 days to fill a collection's review slots. Governance can raise the bond or age requirement if needed.
- **Self-rating prevented**: Owners and collaborators cannot rate their own collections.
- **Challenge mechanism**: Bad reviews can be overturned via x/rep jury. Curators face bond slashing; auto-deactivation when bond < `min_curator_bond` creates escalating consequences.
- **Review cap**: `max_reviews_per_collection` limits coordinated dogpiling.
- **Challenge economics**: `challenge_deposit` (250 DREAM) is set high enough relative to `curator_slash_fraction` (10% of 500 = 50 DREAM slashed) that frivolous challenges are costly. A successful challenger recovers their 250 DREAM deposit plus earns 80% of the 50 DREAM slash (40 DREAM reward); the remaining 10 DREAM is burned. This 16% return on the deposit creates a meaningful incentive to identify bad curation. Frivolous challenges lose the full 250 DREAM deposit (burned), creating asymmetric risk that deters griefing.

### 14.13. Module Account Mixed Denomination
The module account holds both SPARK (TTL collection/item deposits, sponsorship escrow, endorsement creation fee escrow, appeal fee escrow) and DREAM (curator bonds, challenge deposits). Both denominations are tracked separately through their respective deposit fields. Permanent deposits, sponsor fees, downvote costs, and per-item spam taxes bypass the module account entirely (sent to burn address).

### 14.14. Sponsorship Abuse

- **Self-sponsorship prevented**: The sponsor cannot be the collection owner.
- **Sponsor fee cost**: Even a small sponsor fee (1 SPARK) prevents rubber-stamping. A member who sponsors 100 collections spends 100 SPARK in fees, creating real economic friction.
- **Trust level gating**: Only ESTABLISHED+ members can sponsor, ensuring sponsors have demonstrated commitment to the community.
- **Escrow timeout**: `sponsorship_request_ttl_blocks` (~7 days) prevents deposits from being locked indefinitely. If no sponsor appears, the non-member gets their escrow back automatically.
- **Non-member pays full deposit**: The non-member bears the full storage cost for permanence — the sponsor is vouching, not subsidizing. This prevents a single generous sponsor from creating permanent state on behalf of many spam accounts.
- **Item count locked during request**: Add/remove operations are blocked while a sponsorship request is pending. This guarantees the escrowed deposit always matches the actual item count at approval time — no stale escrow, no under/over-payment.
- **Collection must still exist**: Sponsorship checks that the collection hasn't expired. An expired collection cannot be retroactively sponsored.

### 14.15. Ephemeral Non-Member State
Non-member collections are guaranteed to self-clean via TTL (max ~30 days at default `max_non_member_ttl_blocks`). Even in a spam attack, the chain returns to clean state within 30 days without manual intervention. This is a stronger guarantee than deposit-based deterrence alone.

### 14.16. Non-Member Endorsement Gate
Non-member collections start in PENDING state and are invisible to public queries until endorsed by a member. This design ensures:
- **Zero moderator burden from non-member content**: Sentinels and juries never see unendorsed content. Discovery is opt-in for adventurous members browsing the pending feed.
- **Bait-and-switch prevention**: Endorsed collections become immutable. The non-member cannot add, remove, or modify items after endorsement. If they want full editing, they must become a member.
- **Endorser accountability**: The endorser stakes 100 DREAM for 30 days. If the endorsed content is hidden by a sentinel during this period, the stake is burned and the endorser receives a reputation penalty.
- **Revenue sharing**: 80% of the 10 SPARK creation fee goes to the endorser, incentivizing members to actively scout the pending feed. 20% is burned.
- **Auto-cleanup**: Unendorsed collections auto-prune after ~30 days (`endorsement_expiry_blocks`), preventing indefinite PENDING state bloat.
- **Membership transition**: When a non-member becomes an `x/rep` member, their PENDING collections transition to ACTIVE and immutability is lifted (see §14.20).

### 14.17. Sentinel Cross-Module Bond
x/collect shares the sentinel identity with x/forum. A sentinel bonded in x/forum can moderate x/collect content using the same bond. Bond commitments are tracked across both modules — the sentinel's "available bond" is their current bond minus total commitments in x/forum and x/collect combined. This prevents a sentinel from over-committing by hiding content across both modules simultaneously.

### 14.18. Reaction Economics
Upvotes are free for members (counter-only, no cost) to encourage positive engagement. Downvotes cost 25 SPARK (burned) to ensure they represent serious signals and prevent drive-by negativity. The asymmetric cost reflects the asymmetric social impact: upvotes are constructive, downvotes are consequential. Rate limits (100 upvotes/day, 20 downvotes/day) prevent gaming.

### 14.19. Community Feedback Opt-Out
Collection owners can set `community_feedback_enabled = false` to disable reactions and curation on their collection and its items. This is useful for personal/utility collections where public ratings are unwanted. However, sentinel moderation (flagging, hiding) always applies regardless of this setting — no one can opt out of policy enforcement. If the owner disables feedback on a collection that already has reactions/reviews, existing data is frozen (retained but no new reactions/reviews accepted).

### 14.20. Non-Member → Member Transition

When a non-member who owns collections becomes an `x/rep` member, their existing collections are upgraded:

1. **PENDING → ACTIVE**: All collections with `status = PENDING` are set to `status = ACTIVE`. They become visible in `PublicCollections` and `PublicCollectionsByType` queries.
2. **Immutability lifted**: `immutable = false` on all collections owned by the new member. The owner regains full editing rights.
3. **Seeking endorsement cleared**: `seeking_endorsement = false` on any collections still awaiting endorsement.
4. **Unendorsed collections preserved**: PENDING collections that were not yet endorsed are transitioned directly — no endorsement is needed when the owner becomes a member.
5. **Endorsed collection stakes unaffected**: If an endorser already staked DREAM on a collection, the stake continues its normal lifecycle (released after `endorsement_stake_duration`). The endorser already received their fee share.

**Implementation:** This is triggered by an `x/rep` membership callback. When `x/rep` confirms a new member, it calls `collectKeeper.OnMembershipGranted(ctx, address)` which iterates the owner's collections and applies the above transitions. The callback is idempotent.

---

## 14a. Crisis Module Invariants

The module registers the following invariants with the Cosmos SDK crisis module for state consistency verification:

| Invariant | Check |
|-----------|-------|
| `collection-counter` | Highest collection ID in store ≤ collection sequence counter |
| `item-counter` | Highest item ID in store ≤ item sequence counter |
| `item-collection-reference` | Every item's `collection_id` references an existing collection |
| `hide-record-consistency` | Every hide record references a target that exists in store |
| `status-index-consistency` | Every collection in the status index has the matching status in its record |

These can be triggered via `sparkdreamd query crisis invariants` during development or included in governance proposals for chain health checks.

---

## 15. Integration Points

### 15.1. x/name (Identity)
Client-side join: resolve `owner` → name for display. No keeper dependency.

### 15.2. x/commons (Council Authorization)
- **Operational params**: `IsCouncilAuthorized(ctx, addr, "commons", "operations")` gates `MsgUpdateOperationalParams`.
- **Three-level check**: accepts x/gov authority, Commons Council policy address, or Operations Committee member.
- **Optional dependency**: if `x/commons` is not wired (e.g., during development), falls back to x/gov authority check only.

### 15.3. x/rep (Membership, Reputation, Jury)
- **Permanent collections**: Only members can create permanent collections directly.
- **Sponsorship**: Member trust level verified for `min_sponsor_trust_level` at sponsorship time.
- **Collaborators**: Active membership verified at add time and on every write.
- **Curators**: Membership and trust level verified at registration and on every rating.
- **Reactions**: Membership required for all reactions (upvote, downvote, flag).
- **Fee exemptions**: Members are exempt from `endorsement_creation_fee` (collections start ACTIVE, not PENDING) and `per_item_spam_tax` (no surcharge on item additions).
- **Tiered limits**: Trust level checked to determine collection cap.
- **Curator rewards**: Curators earn DREAM staking rewards on bonded DREAM. Unchallenged reviews contribute to "curation" reputation tag.
- **Reputation impact**: Collection owners with high UP ratings gain "curation" reputation. DOWN ratings carry no direct penalty.
- **Jury resolution**: Challenge verdicts and hide appeal verdicts come from x/rep jury via `ResolveChallengeResult` and `ResolveHideAppeal` callbacks.
- **Endorser DREAM staking**: Endorser stakes are managed via x/rep keeper (`LockDREAM` / `UnlockDREAM` / `BurnDREAM`).
- **Endorser reputation**: If endorsed content is hidden by sentinel, endorser receives a small reputation penalty.
- **Community conviction staking**: Any active member can stake DREAM on a public collection via `MsgStake` (x/rep) with `target_type = STAKE_TARGET_CONTENT` and `target_identifier = "collect/collection/{id}"`. Conviction builds over time using the `content_conviction_half_life_epochs` half-life formula (see x/rep spec §Content Staking). No DREAM rewards — conviction is the only output. Authors cannot stake conviction on their own collections (use author bonds instead). `max_content_stake_per_member` caps individual whale influence. Works for both anonymous and regular collections.
- **Author bonds**: Collection creators can deposit DREAM as an author bond via x/rep's `CreateAuthorBond()`, keyed to `"collect/collection/{id}"`. Bonds signal skin-in-the-game quality commitment; slashable via sentinel moderation. Author bonds do NOT contribute to conviction score — the two signals are independent (see x/rep spec §Author Bonds).

### 15.4. x/blog (OnChainReference Validation)
- **Post/reply existence checks**: `BlogKeeper.HasPost()` and `BlogKeeper.HasReply()` validate OnChainReference entries pointing to blog posts and replies.
- **Optional dependency**: If `BlogKeeper` is nil (e.g., during early development), blog references are accepted without validation.

**BlogKeeper interface** (defined in `x/collect/types/expected_keepers.go`):

```go
type BlogKeeper interface {
    HasPost(ctx context.Context, id uint64) bool
    HasReply(ctx context.Context, id uint64) bool
}
```

### 15.5. x/forum (Sentinel Moderation)
- **Shared sentinel identity**: x/collect uses the same sentinel bond system as x/forum. Sentinels bonded in x/forum automatically have moderation authority in x/collect.
- **Cross-module bond tracking**: Bond commitments in x/collect are tracked alongside x/forum commitments. A sentinel's available bond = current bond - total committed (x/forum + x/collect).
- **Sentinel state reads**: x/collect reads sentinel bond status, backing, cooldown, and metrics via `ForumKeeper` interface.
- **Bond operations**: Hide actions commit DREAM from the sentinel's x/forum bond. Appeal resolutions slash or release via x/forum keeper.
- **Collections shareable in forum threads**: Forum posts can reference collections via `OnChainReference`.

**ForumKeeper interface** (defined in `x/collect/types/expected_keepers.go`):

```go
type ForumKeeper interface {
    // Sentinel status checks
    IsSentinelActive(ctx context.Context, sentinel string) (bool, error)
    GetAvailableBond(ctx context.Context, sentinel string) (math.Int, error)  // current bond - total committed across all modules

    // Bond commitment operations
    CommitBond(ctx context.Context, sentinel string, amount math.Int, module string, referenceID uint64) error
    ReleaseBondCommitment(ctx context.Context, sentinel string, amount math.Int, module string, referenceID uint64) error
    SlashBondCommitment(ctx context.Context, sentinel string, amount math.Int, module string, referenceID uint64) error

    // Tag registry operations (shared tag system)
    TagExists(ctx context.Context, name string) (bool, error)
    IsReservedTag(ctx context.Context, name string) (bool, error)

    // Post existence check for OnChainReference validation
    HasPost(ctx context.Context, id uint64) bool
}
```

### 15.6. x/reveal (Progressive Open-Source)
Track contributions via `OnChainReference` with `module: "reveal"`.

---

## 16. Future Considerations

### 16.1. Forking / Cloning
Fork public collections into the user's own ownership.

### 16.2. Attestation
Optional NFT ownership verification via IBC or signed attestation.

### 16.3. Collection Following
Subscribe to public collection updates.

### 16.4. SPARK Fee Split for Curators
Governance-controlled split of burned SPARK fees into a curation reward pool. Deferred to v1+ after observing real usage.

### 16.5. Encryption Key Rotation
Re-encrypt collection data after collaborator removal. Client-side operation, potentially facilitated by a `MsgReEncryptCollection`.

### 16.6. Ownership Transfer
Transfer collection ownership to another address. Would require deposit tracking updates and collaborator/curation state reconciliation. Currently, if an owner loses their key, the collection is orphaned (TTL collections are eventually cleaned up; permanent collections persist as dead state).

### 16.7. Cross-Module Conviction Propagation

Collections (both regular and anonymous) can optionally reference an x/rep initiative by setting `initiative_id` at creation time. When a collection accumulates community conviction stakes, a fraction of that conviction propagates back to the referenced initiative, boosting its completion score. This creates a virtuous feedback loop: popular curated collections about an initiative accelerate the initiative's progress.

#### How It Works

1. **Collection creation:** When a collection is created with `initiative_id > 0`, the keeper validates the initiative exists (via `repKeeper.ValidateInitiativeReference()`) and registers a content→initiative link (via `repKeeper.RegisterContentInitiativeLink(ctx, initiativeID, STAKE_TARGET_COLLECT_CONTENT, collection_id)`).

2. **Conviction staking:** Community members stake DREAM on collections via x/rep's `Stake` message with `target_type = STAKE_TARGET_COLLECT_CONTENT` and `target_id = collection_id`. Conviction is time-weighted: `conviction = amount * min(1, t / (2 * half_life))` where `half_life = 14 epochs`.

3. **Propagation:** When x/rep evaluates an initiative's completion, it calls `GetPropagatedConviction(initiativeID)` which:
   - Iterates all content items linked to the initiative (across blog, forum, and collect modules)
   - For each linked item, queries `GetContentConviction()` to get its current conviction score
   - Sums all content conviction and multiplies by `conviction_propagation_ratio` (default 10%, configurable via x/rep params)
   - The propagated conviction counts as **external conviction** on the initiative (satisfying the 50% external requirement)

4. **Link cleanup:** When a collection is pruned (TTL expiry with insufficient conviction), deleted by its owner, or hidden-expired, the keeper calls `repKeeper.RemoveContentInitiativeLink()` to clean up the propagation link.

#### Proto Changes

**Collection** (add to §3.1):
```protobuf
message Collection {
  // ... existing fields ...
  uint64 initiative_id = 28;           // x/rep initiative referenced by this collection (0 = none, immutable)
}
```

**MsgCreateCollection** (add to §5.1):
```protobuf
message MsgCreateCollection {
  // ... existing fields ...
  string author_bond = 11;             // Optional DREAM amount to lock as author bond
  uint64 initiative_id = 12;           // Optional: reference an x/rep initiative (0 = none, immutable)
}
```

**Anonymous collection creation via x/shield** (replaces MsgCreateAnonymousCollection §18.7):
When `MsgCreateCollection` is executed via `MsgShieldedExec`, the `initiative_id` field works the same way — it is part of the standard `MsgCreateCollection` message.

*Note: `conviction_sustained` (field 27) and conviction renewal params already exist — see §18.9 and §18.15.*

#### Parameters

No new parameters needed. The existing `conviction_renewal_threshold` and `conviction_renewal_period` (§18.15) control the TTL renewal behavior. The `conviction_propagation_ratio` that controls how much content conviction flows to the initiative is an x/rep parameter (default 0.10 = 10%), not an x/collect parameter — it applies uniformly across all content modules (blog, forum, collect).

#### Conviction Renewal Interaction

Ephemeral collections with `initiative_id > 0` that reach their TTL expiry are candidates for conviction renewal (§18.9, §10.1). This works the same whether or not an initiative is referenced — conviction renewal is based on the collection's own conviction score, not the linked initiative's score. The initiative link is a one-way propagation: collection conviction → initiative, not the reverse.

If a collection has both initiative-propagated conviction AND is eligible for conviction renewal, both mechanisms operate independently:
- The collection's conviction score determines whether its TTL is renewed
- The same conviction score (multiplied by `conviction_propagation_ratio`) propagates to the initiative

#### RepKeeper Interface Additions

The following methods must be added to `RepKeeper` in `x/collect/types/expected_keepers.go`:

```go
// Conviction propagation (initiative ↔ content linking)
ValidateInitiativeReference(ctx context.Context, initiativeID uint64) error
RegisterContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error
RemoveContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error
```

These match the identical methods on x/blog and x/forum RepKeeper interfaces.

#### Keeper Changes

**MsgCreateCollection:** After deposit handling, before event emission:
```
if msg.InitiativeId > 0 and repKeeper is not nil:
    validate initiative reference via repKeeper.ValidateInitiativeReference(msg.InitiativeId)
    set collection.InitiativeId = msg.InitiativeId
    register link via repKeeper.RegisterContentInitiativeLink(msg.InitiativeId, STAKE_TARGET_COLLECT_CONTENT, collection_id)
```

**Anonymous collection creation (via x/shield → MsgCreateCollection):** The same `MsgCreateCollection` handler logic above applies when the message is dispatched via `MsgShieldedExec`. No separate handler needed.

**MsgDeleteCollection:** Before cleanup:
```
if collection.InitiativeId > 0 and repKeeper is not nil:
    remove link via repKeeper.RemoveContentInitiativeLink(collection.InitiativeId, STAKE_TARGET_COLLECT_CONTENT, collection_id)
    (best-effort — log error, don't fail deletion)
```

**EndBlocker TTL Pruning (§10.1):** Before deleting an expired collection:
```
if collection.InitiativeId > 0 and repKeeper is not nil:
    remove link via repKeeper.RemoveContentInitiativeLink(collection.InitiativeId, STAKE_TARGET_COLLECT_CONTENT, collection_id)
    (best-effort — log error, don't halt chain)
```

#### Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrInvalidInitiativeRef` | 1230 | Invalid initiative reference for conviction propagation |

#### Events

| Event | Attributes | Trigger |
|-------|------------|---------|
| `collect.collection.initiative_linked` | `collection_id`, `initiative_id` | Collection created with initiative reference |
| `collect.collection.initiative_unlinked` | `collection_id`, `initiative_id` | Initiative link removed (deletion or TTL expiry) |

*Note: `collection_conviction_sustained` and `collection_renewed` events (§18.16) continue to fire as before for conviction renewal — they are independent of initiative linking.*

#### Example Flow

```
1. Alice creates a curated collection of resources about Initiative #42 (improve documentation):
   MsgCreateCollection{creator: alice, name: "Docs Initiative Resources", initiative_id: 42, ...}

2. Bob stakes 500 DREAM on Alice's collection (believes it's valuable curation):
   x/rep MsgStake{staker: bob, target_type: STAKE_TARGET_COLLECT_CONTENT, target_id: 101, amount: 500}

3. After 14 epochs, Bob's stake reaches full conviction:
   content_conviction = 500 DREAM

4. When Initiative #42 is evaluated for completion:
   propagated_conviction = 500 * 0.10 = 50 DREAM (counts as external conviction on Initiative #42)

5. Combined with direct initiative stakes and conviction from blog/forum content,
   this helps Initiative #42 reach completion threshold.
```

#### Interaction with Other Features

| Feature | Interaction |
|---------|-------------|
| **Author bonds** | Author bonds (`CreateAuthorBond`) are separate from content conviction stakes. An author can bond DREAM and the community can also conviction-stake — these are independent mechanisms. |
| **Anonymous collections** | Anonymous collections can reference initiatives. The anonymous author cannot stake on their own collection (identity hidden), so all conviction comes from other community members. |
| **Sponsorship** | Sponsored collections retain their initiative link after becoming permanent. Conviction continues to propagate. |
| **Pinning** | Pinned anonymous collections retain their initiative link. The `conviction_sustained` flag is cleared (§18.12) but the initiative link persists — pinning makes the collection permanent, and conviction propagation continues independently. |
| **Collection hiding** | Hiding a collection does NOT remove the initiative link — the collection may be restored on appeal. Link is only removed on deletion or TTL expiry. |
| **Collection deletion** | Deleting a collection removes the initiative link immediately (owner chose to remove their content). |
| **Endorsement** | Non-member collections in PENDING status can reference initiatives. The initiative link is created at collection creation time, regardless of endorsement status. If the collection is pruned before endorsement, the link is cleaned up. |
| **Curation reviews** | Curation quality ratings are independent of initiative linking — a collection can be highly rated by curators and also propagate conviction to an initiative. |

---

## 17. CLI Commands

```bash
# Collections
sparkdreamd tx collect create-collection "My NFTs" --description "My NFT portfolio" --type nft --visibility public --tags "art,pfp" --from alice
sparkdreamd tx collect create-collection --type nft --visibility private --encrypted --encrypted-data "base64..." --expires-at 1000000 --from alice
sparkdreamd tx collect update-collection 1 --name "My Art NFTs" --from alice
sparkdreamd tx collect delete-collection 1 --from alice

# Items (single)
sparkdreamd tx collect add-item 1 --title "CryptoPunk #1234" --reference-type nft --chain-id ethereum --contract 0xb47e3cd837dDF8e4c57F05d70Ab865de6e193BBB --token-id 1234 --token-standard ERC-721 --from alice
sparkdreamd tx collect add-item 1 --title "Cosmos SDK Docs" --reference-type link --uri "https://docs.cosmos.network" --from alice
sparkdreamd tx collect add-item 1 --title "Forum Thread" --reference-type on-chain --module forum --entity-type thread --entity-id 42 --from alice
sparkdreamd tx collect update-item 5 --title "Updated Title" --from alice
sparkdreamd tx collect remove-item 5 --from alice
sparkdreamd tx collect reorder-item 5 --position 0 --from alice

# Items (batch)
sparkdreamd tx collect add-items 1 --items-file ./my-nfts.json --from alice
sparkdreamd tx collect remove-items 5 6 7 8 --from alice

# Collaborators
sparkdreamd tx collect add-collaborator 1 --address sprkdrm1xyz... --role editor --from alice
sparkdreamd tx collect remove-collaborator 1 --address sprkdrm1xyz... --from alice
sparkdreamd tx collect update-collaborator-role 1 --address sprkdrm1xyz... --role admin --from alice

# Sponsorship
sparkdreamd tx collect request-sponsorship 1 --from bob                    # Non-member requests sponsorship for collection 1
sparkdreamd tx collect cancel-sponsorship-request 1 --from bob             # Cancel and get escrow back
sparkdreamd tx collect sponsor-collection 1 --from alice                   # Trusted member sponsors collection 1

# Curators
sparkdreamd tx collect register-curator --bond 500dream --from alice
sparkdreamd tx collect unregister-curator --from alice
sparkdreamd tx collect rate-collection 1 --verdict up --tags "well-organized,comprehensive" --comment "Great showcase" --from alice
sparkdreamd tx collect challenge-review 7 --reason "Biased rating without justification" --from bob

# Reactions
sparkdreamd tx collect upvote-content 1 --target-type collection --from alice
sparkdreamd tx collect upvote-content 5 --target-type item --from alice
sparkdreamd tx collect downvote-content 1 --target-type collection --from alice      # Burns 25 SPARK
sparkdreamd tx collect downvote-content 5 --target-type item --from alice

# Flagging
sparkdreamd tx collect flag-content 1 --target-type collection --reason spam --from alice
sparkdreamd tx collect flag-content 5 --target-type item --reason other --reason-text "Plagiarized content" --from alice

# Sentinel moderation
sparkdreamd tx collect hide-content 1 --target-type collection --reason inappropriate --from sentinel1
sparkdreamd tx collect hide-content 5 --target-type item --reason copyright --reason-text "Stolen artwork" --from sentinel1
sparkdreamd tx collect appeal-hide 1 --from alice                                     # Appeals hide record 1, costs 5 SPARK

# Endorsement (non-member collections)
sparkdreamd tx collect set-seeking-endorsement 1 --seeking true --from bob            # Non-member signals readiness
sparkdreamd tx collect endorse-collection 1 --from alice                              # Member endorses, stakes 100 DREAM

# Queries
sparkdreamd query collect collection 1
sparkdreamd query collect collections-by-owner sprkdrm1abc...
sparkdreamd query collect public-collections
sparkdreamd query collect public-collections-by-type nft
sparkdreamd query collect collections-by-collaborator sprkdrm1xyz...
sparkdreamd query collect item 5
sparkdreamd query collect items 1
sparkdreamd query collect items-by-owner sprkdrm1abc...
sparkdreamd query collect collaborators 1
sparkdreamd query collect sponsorship-request 1
sparkdreamd query collect sponsorship-requests
sparkdreamd query collect curator sprkdrm1abc...
sparkdreamd query collect active-curators
sparkdreamd query collect curation-summary 1
sparkdreamd query collect curation-reviews 1
sparkdreamd query collect curation-reviews-by-curator sprkdrm1abc...
sparkdreamd query collect content-flag 1 --target-type collection
sparkdreamd query collect flagged-content
sparkdreamd query collect hide-record 1
sparkdreamd query collect hide-records-by-target 1 --target-type collection
sparkdreamd query collect pending-collections
sparkdreamd query collect collection-conviction 1
sparkdreamd query collect public-collections --order-by conviction
sparkdreamd query collect endorsement 1
sparkdreamd query collect params
```

---

## 18. Anonymous Collections via x/shield

> **Architecture change (x/shield migration):** All per-module anonymous messages (`MsgCreateAnonymousCollection`, `MsgManageAnonymousCollection`, `MsgAnonymousReact`) and their corresponding query endpoints (`AnonymousCollectionMeta`, `IsCollectNullifierUsed`) have been **removed** from x/collect. Anonymous operations now go through x/shield's unified `MsgShieldedExec` entry point, which handles ZK proof verification, nullifier management, and module-paid gas centrally. x/collect implements the **ShieldAware interface** (see `x/collect/keeper/shield_aware.go`) to register which messages support anonymous execution via shield. The proto definitions, keeper code, and simulation files for the removed messages have been deleted.
>
> **What moved to x/shield:**
> - ZK proof verification (PLONK over BN254, verification keys stored on-chain)
> - Nullifier storage and dedup (centralized per-domain scoping replaces per-module stores)
> - TLE infrastructure (DKG ceremony, epoch decryption, encrypted batching)
> - Module-paid gas (shield module account pays tx fees; submitter needs zero balance)
> - Relay/subsidy infrastructure
>
> **What remains in x/collect:**
> - ShieldAware interface implementation (`IsShieldCompatible()`)
> - Regular (non-anonymous) message handlers for all collection operations
> - Conviction staking, curation, endorsement, sentinel moderation (unchanged)
> - Anonymous collection properties (module-account ownership, TTL requirement, public-only) still apply when collections are created via `MsgShieldedExec`

Members can create and manage collections without revealing their identity, using x/shield's unified privacy infrastructure. The submitter sends a `MsgShieldedExec` to x/shield, which verifies the ZK proof, checks nullifiers, and dispatches the inner message (e.g., `MsgCreateCollection`, `MsgUpvoteContent`, `MsgDownvoteContent`) to x/collect. The creator proves they are an active member meeting a minimum trust level without revealing *which* member they are. Community conviction staking (via x/rep's existing content staking system) provides a complementary quality signal alongside expert curation — bonded curators can rate anonymous collections just like any other public collection.

See **[docs/x-shield-spec.md](x-shield-spec.md)** for the full specification covering ZK proof verification, nullifier management, shielded execution modes, and the ShieldAware interface.

### 18.1. Design Rationale

Anonymous collections serve use cases where the curator's identity must be protected:

- **Fraud watchlists**: Tracking suspected fraudulent art or scams without exposing the reporter to retaliation
- **Whistleblower evidence**: Curated evidence collections linked to anonymous blog posts and forum threads
- **Controversial curation**: Assembling politically sensitive or culturally contentious resource lists

Anonymous collections benefit from two quality signals working together: **expert curation** (bonded curators rate the collection as they would any public collection, providing attributed expert judgment) and **community conviction** (members stake DREAM to signal belief in the collection's value, providing broad economic consensus). The absence of a known author means neither signal alone is sufficient — curators provide credibility through their bonded reputation, while conviction staking surfaces community-wide endorsement.

### 18.2. Collect-Specific Nullifier Domains

> **Note:** Nullifier storage and dedup are now managed centrally by x/shield. The per-module `AnonNullifier/` store prefix and `IsCollectNullifierUsed` query have been removed from x/collect. x/shield uses the same domain/scope scheme below but stores nullifiers in its own centralized store with per-domain scoping.

The following shielded operations are registered via x/collect's ShieldAware interface (see `x/collect/keeper/shield_aware.go`):

| Domain | Inner Message | Scope | Effect | Proof Domain |
|--------|--------------|-------|--------|-------------|
| `21` | `MsgCreateCollection` | Current epoch (epoch-scoped) | One anonymous collection per member per epoch | `PROOF_DOMAIN_TRUST_TREE` |
| `22` | `MsgUpvoteContent` | `target_id`-scoped | One anonymous upvote per member per target | `PROOF_DOMAIN_TRUST_TREE` |
| `23` | `MsgDownvoteContent` | `target_id`-scoped | One anonymous downvote per member per target | `PROOF_DOMAIN_TRUST_TREE` |

All operations require `min_trust_level=1` and support `batch_mode=EITHER` (both immediate and encrypted batch execution).

**Removed domains (legacy):** Domains 6, 7, 10, 11 were previously used by the per-module anonymous messages (`MsgCreateAnonymousCollection`, `MsgManageAnonymousCollection`, `MsgAnonymousReact`). These have been replaced by the shield-managed domains above.

### 18.3. Management Key

> **Status: REMOVED.** The `AnonymousCollectionMeta` proto and management key infrastructure have been removed from x/collect. Anonymous collection management now goes through x/shield's `MsgShieldedExec`, which provides its own session management and replay protection. The management key pattern described below is historical context only.

~~Anonymous collections used a **pseudonymous management key** for ongoing curation. This avoided requiring a ZK proof for every item add/remove/reorder operation.~~

With x/shield, anonymous management operations (adding/removing/reordering items, updating metadata) are submitted as `MsgShieldedExec` wrapping the appropriate inner message. x/shield handles proof verification, nullifier checks, and replay protection centrally. The two execution modes (Immediate and Encrypted Batch) provide different privacy/latency tradeoffs — see `docs/x-shield-spec.md`.

### 18.4. State Objects

> **Status: REMOVED.** The `AnonymousCollectionMeta`, `AnonNullifierEntry`, and `AnonCollectionNonce` state objects have been removed from x/collect. Nullifier state is now managed centrally by x/shield. Anonymous collection metadata (proof context, session tracking) is tracked by x/shield's shielded operation registration system.

### 18.5. Storage Schema Additions

> **Status: REMOVED.** The `AnonCollectionMeta/` and `AnonNullifier/` key prefixes have been removed from x/collect's store. Nullifier storage is now centralized in x/shield. No anonymous-specific storage remains in x/collect.

### 18.6. Collection Properties

Anonymous collections have specific constraints that differ from regular collections:

- **Owner**: Set to the collect module account address (same pattern as x/blog anonymous posts)
- **Visibility**: Must be `PUBLIC` — anonymous private collections are rejected (encrypted content from an unknown author has no discoverability or accountability path)
- **Encrypted**: Must be `false` (follows from `PUBLIC` visibility)
- **TTL**: Must have `expires_at > 0` — anonymous collections are always ephemeral. Permanence requires community action (see §18.12 Pinning)
- **Collaborators**: Not supported — anonymous collections are single-curator only
- **Endorsement/Sponsorship**: Not applicable — anonymous collections are not created by non-members (they require ZK membership proof), so the non-member endorsement and sponsorship pathways do not apply
- **Community feedback**: Always enabled (`community_feedback_enabled = true`, immutable on anonymous collections)
- **Author bond**: Optional — the anonymous creator can deposit DREAM as an author bond at creation time via x/rep. The bond is keyed to the management public key (not an address), slashable via sentinel moderation

### 18.7. Messages

> **Status: REMOVED.** `MsgCreateAnonymousCollection`, `MsgManageAnonymousCollection`, and `MsgAnonymousReact` (see §18.10) have been **deleted** from x/collect. Their proto definitions, keeper handlers, simulation files, and CLI commands have been removed.
>
> **Replacement:** Anonymous collection operations now use x/shield's `MsgShieldedExec`, which wraps a standard inner message:
>
> | Old Message (REMOVED) | Replacement via x/shield |
> |----------------------|--------------------------|
> | `MsgCreateAnonymousCollection` | `MsgShieldedExec` wrapping `MsgCreateCollection` (domain 21, epoch-scoped) |
> | `MsgManageAnonymousCollection` (ADD_ITEM) | `MsgShieldedExec` wrapping `MsgAddItem` or `MsgAddItems` |
> | `MsgManageAnonymousCollection` (REMOVE_ITEM) | `MsgShieldedExec` wrapping `MsgRemoveItem` or `MsgRemoveItems` |
> | `MsgManageAnonymousCollection` (UPDATE_ITEM) | `MsgShieldedExec` wrapping `MsgUpdateItem` |
> | `MsgManageAnonymousCollection` (REORDER_ITEM) | `MsgShieldedExec` wrapping `MsgReorderItem` |
> | `MsgManageAnonymousCollection` (UPDATE_METADATA) | `MsgShieldedExec` wrapping `MsgUpdateCollection` |
> | `MsgAnonymousReact` (upvote) | `MsgShieldedExec` wrapping `MsgUpvoteContent` (domain 22, target_id-scoped) |
> | `MsgAnonymousReact` (downvote) | `MsgShieldedExec` wrapping `MsgDownvoteContent` (domain 23, target_id-scoped) |
>
> x/shield sets the inner message's `creator` field to the shield module account address before dispatching to x/collect, preserving the anonymous-owner pattern (collection `owner = module_account_address`). x/shield handles ZK proof verification, nullifier dedup, module-paid gas, and rate limiting. x/collect's standard message handlers execute the inner message as if it came from a regular sender.
>
> See `docs/x-shield-spec.md` for `MsgShieldedExec` details.

### 18.8. Nonce Management

> **Status: REMOVED.** The `AnonCollectionNonce/` store prefix has been removed from x/collect. Replay protection for anonymous operations is now handled by x/shield's nullifier system — each `MsgShieldedExec` includes a unique nullifier that is checked and stored centrally by x/shield.

### 18.9. Conviction, Curation, and Lifetime Extension

Anonymous collections participate in the same conviction staking and expert curation systems as regular collections (see §6 `CollectionConviction` query, §15.3 x/rep integration). The only anonymous-specific constraint:

- Authors of anonymous collections cannot create community conviction stakes on their own collection. This is enforced via x/rep's author exclusion rule — the author bond is keyed to the anonymous identity, and x/rep prevents the same identity from holding both an author bond and a community conviction stake on the same content.

For anonymous collections where the curator's identity is unknown, conviction score and curation reviews together provide the quality signal that curator identity would normally reinforce for regular collections.

**Conviction-based lifetime extension:** Anonymous collections are ephemeral by default (TTL required, §18.6). When a collection's TTL expires, the EndBlocker (§10.1) checks its community conviction score. If the score meets or exceeds `params.conviction_renewal_threshold`, the TTL is extended by `params.conviction_renewal_period` instead of pruning — the collection survives as long as the community actively supports it with staked DREAM. Deposits remain held through renewals and are only refunded when the collection is finally pruned (conviction dropped below threshold) or pinned (§18.12). See [x-shield-spec.md](x-shield-spec.md) for the unified privacy architecture.

This creates a three-tier lifecycle for anonymous collections:

| Stage | Trigger | Effect |
|-------|---------|--------|
| **Ephemeral** (default) | Creation with `expires_at > 0` | Pruned by EndBlocker at TTL expiry |
| **Conviction-sustained** | Conviction score ≥ threshold at expiry | TTL extended; rolling renewal as long as community supports |
| **Pinned** (permanent) | ESTABLISHED+ member calls `MsgPinCollection` | TTL cleared; deposits burned; permanently stored |

### 18.10. Anonymous Reactions (Upvote/Downvote)

> **Status: `MsgAnonymousReact` REMOVED.** Anonymous reactions now use x/shield's `MsgShieldedExec` wrapping the standard `MsgUpvoteContent` (domain 22) or `MsgDownvoteContent` (domain 23). x/shield handles ZK proof verification, nullifier dedup (one anonymous reaction per member per target via target_id-scoped nullifiers), and module-paid gas.

Members can upvote or downvote any public collection or item without revealing their identity. This applies to **all** public collections — both anonymous and regular. The mechanism uses x/shield's unified privacy infrastructure.

**How it works:** The anonymous member submits a `MsgShieldedExec` to x/shield with the inner message set to `MsgUpvoteContent` or `MsgDownvoteContent`. x/shield verifies the ZK proof (PLONK/BN254, `PROOF_DOMAIN_TRUST_TREE`, `min_trust_level=1`), checks the nullifier (domain 22 or 23, scoped to `target_id`), and dispatches the inner message to x/collect with the `creator` field set to the shield module account. x/collect's standard `MsgUpvoteContent` / `MsgDownvoteContent` handlers execute normally.

**Parity with public reactions:**

| Property | Public | Anonymous (via x/shield) |
|----------|--------|--------------------------|
| One vote per user per target | `ReactionDedup` keyed storage | x/shield nullifier (domain=22/23, scope=target_id) |
| Removable | No (public votes are also permanent) | No — nullifier is permanent |
| Rate limit | `max_upvotes_per_day` / `max_downvotes_per_day` | x/shield per-identity rate limiting |
| Downvote cost | 25 SPARK burned | Module-paid gas via x/shield; downvote SPARK cost charged to shield module account |
| Voter identity | Stored in `ReactionDedup` | Hidden by ZK proof |
| Self-vote prevention | Owner/collaborator check | Not enforced (ZK proof hides identity; economic cost + nullifier uniqueness provide sufficient spam deterrence) |

### 18.11. Queries

> **Status: `AnonymousCollectionMeta` and `IsCollectNullifierUsed` queries REMOVED.** These have been replaced by x/shield queries:
> - Nullifier status: use x/shield's `QueryNullifierUsed` (centralized nullifier store)
> - Shielded operation registration: use x/shield's `QueryShieldedOps` to discover registered operations for x/collect

The `AnonymousCollections` query has been removed. Anonymous collections (where `owner = module_account_address`) can be found via `CollectionsByOwner` with the module account address. The `CollectionConviction` query remains (works for all collections).

### 18.12. Pinning (TTL Override)

Anonymous collections are ephemeral by default (TTL required). Any active x/rep member at or above `pin_min_trust_level` (default ESTABLISHED) can **pin** an anonymous collection, clearing its TTL and making it permanent:

```protobuf
message MsgPinCollection {
  string creator = 1;           // Member pinning the collection
  uint64 collection_id = 2;
}

message MsgPinCollectionResponse {}
```

**Validation:**
1. `creator` must be an active x/rep member at or above `pin_min_trust_level`
2. Collection must exist and have `expires_at > 0` (TTL collection)
3. Collection must have `status = ACTIVE`
4. `creator` must not exceed `max_pins_per_day` (rolling 24h window)

**Logic:**
1. Set `expires_at = 0` (permanent)
2. If `conviction_sustained == true`: set `conviction_sustained = false`
3. Burn the held collection deposit + item deposits from module account (`deposit_burned = true`)
4. Remove from expiry index
5. Emit `collection_pinned` event with `collection_id`, `pinned_by`

**Rationale:** This mirrors x/blog's pinning mechanism for anonymous posts. The pinner signals that the collection is valuable enough to persist. The deposit burn reflects the permanent state burden. Combined with conviction staking and conviction-based lifetime extension (§18.9), this creates a three-tier quality filter: low-quality anonymous collections expire at TTL, community-supported ones survive through conviction renewal, and the most valuable ones get pinned for permanence. Pinning a conviction-sustained collection removes it from the renewal cycle entirely.

### 18.13. Deposit Refund Behavior

Deposit refunds for anonymous collections work slightly differently from regular collections because the owner is the module account:

- **TTL expiry refund**: Collection deposit + item deposits are refunded to the **module account** (which already holds them) — effectively a no-op. The deposits are simply released from the module account's balance when the collection and its bookkeeping are deleted.
- **Item removal refund** (via `MsgShieldedExec` wrapping `MsgRemoveItem`): The `per_item_deposit` for removed items stays in the module account (held for future expiry cleanup). `item_deposit_total` is decremented on the collection.
- **Pinning**: When an anonymous collection is pinned (§18.12), held deposits are burned — same as when a member converts TTL → permanent.
- **Sentinel hide/deletion**: Standard deposit handling applies — deposits are released or burned per the hide/appeal resolution rules (§5.25, §5.26).

### 18.14. Cross-Module Linking

Anonymous collections can reference content across modules via the existing `OnChainReference` item type:

```
OnChainReference { module: "blog",  entity_type: "post",   entity_id: "42" }
OnChainReference { module: "forum", entity_type: "thread",  entity_id: "17" }
OnChainReference { module: "collect", entity_type: "collection", entity_id: "5" }
```

**Use case flow** (fraud watchlist example):
1. Anonymous member creates a blog post with detailed evidence (x/blog anonymous post)
2. Anonymous member creates a collection with structured items (each item referencing a suspected fraudulent artwork)
3. Each item links to the blog post and/or a forum thread for discussion
4. Community members stake conviction on the collection (x/rep content staking)
5. High-conviction collections surface in ranked queries
6. If validated, an ESTABLISHED+ member pins the collection for permanence

The anonymous author can also add items over time as new evidence emerges, using the management key.

### 18.15. Parameters

> **Status: Partially removed.** Parameters related to per-module anonymous infrastructure have been removed or moved:
> - **Removed from x/collect**: `anonymous_posting_enabled`, `anonymous_min_trust_level`, `max_anonymous_collections_per_key`, `anon_subsidy_budget_per_epoch`, `anon_subsidy_max_per_action`, `anon_subsidy_relay_addresses` — these are now controlled by x/shield's params (shielded operation registration, rate limiting, funding)
> - **Retained in x/collect**: `pin_min_trust_level`, `conviction_renewal_threshold`, `conviction_renewal_period` — these control x/collect-specific behavior (pinning and conviction renewal)

Remaining parameters in `Params` and `CollectOperationalParams`:

| Parameter | Default | Gov/Ops | Description |
|-----------|---------|---------|-------------|
| `pin_min_trust_level` | `2` (ESTABLISHED) | Ops | Minimum trust level to pin anonymous collections |
| `max_pins_per_day` | `10` | Ops | Max pins per address per rolling 24h |
| `conviction_renewal_threshold` | `0` (disabled) | Ops | Min conviction score to renew anonymous collections at TTL expiry (0 = disabled). See §18.9 and §10.1 |
| `conviction_renewal_period` | `432000` (~30 days) | Ops | Blocks to extend TTL by when conviction-renewed |

### 18.16. Events

> **Status: Partially removed.** Events for deleted messages (`anonymous_collection_created`, `anonymous_item_*`, `anonymous_collection_updated`, `anonymous_content_upvoted`, `anonymous_content_downvoted`) are no longer emitted by x/collect. x/shield emits its own `shielded_exec_dispatched` events when executing anonymous operations. The standard x/collect events (`collection_created`, `item_added`, `content_upvoted`, etc.) are still emitted by the inner message handlers when dispatched via x/shield.

Remaining anonymous-specific events:

| Event | Attributes | Trigger |
|-------|------------|---------|
| `collection_pinned` | `collection_id`, `pinned_by` | MsgPinCollection |
| `collection_conviction_sustained` | `id`, `conviction_score`, `new_expires_at` | EndBlocker: first entry into conviction-sustained state |
| `collection_renewed` | `id`, `conviction_score`, `new_expires_at` | EndBlocker: subsequent conviction renewal |

### 18.17. Error Codes

> **Status: Partially removed.** Error codes related to per-module ZK verification, nullifiers, management keys, and nonces (1200-1212) have been removed from x/collect. These error conditions are now handled by x/shield. Remaining errors relate to pinning and collection-level constraints.

| Error | Code | Status | Description |
|-------|------|--------|-------------|
| `ErrAnonymousPostingUnavailable` | 1200 | REMOVED | Now handled by x/shield |
| `ErrAnonymousPostingDisabled` | 1201 | REMOVED | Now handled by x/shield params |
| `ErrInsufficientAnonTrustLevel` | 1202 | REMOVED | Now handled by x/shield shielded op registration (`min_trust_level`) |
| `ErrStaleMerkleRoot` | 1203 | REMOVED | Now handled by x/shield |
| `ErrNullifierUsed` | 1204 | REMOVED | Now handled by x/shield centralized nullifier store |
| `ErrInvalidZKProof` | 1205 | REMOVED | Now handled by x/shield |
| `ErrAnonymousRequiresTTL` | 1206 | REMOVED | Enforced by x/shield shielded op constraints |
| `ErrInvalidManagementKey` | 1207 | REMOVED | Management key pattern eliminated |
| `ErrInvalidManagementSignature` | 1208 | REMOVED | Management key pattern eliminated |
| `ErrInvalidNonce` | 1209 | REMOVED | Replay protection handled by x/shield nullifiers |
| `ErrNotAnonymousCollection` | 1210 | REMOVED | No per-module anonymous metadata |
| `ErrMaxAnonymousCollections` | 1211 | REMOVED | Rate limiting handled by x/shield |
| `ErrAnonymousCannotBePrivate` | 1212 | REMOVED | Enforced by x/shield shielded op registration |
| `ErrCannotPinActive` | 1214 | Retained | Collection is already permanent (`expires_at = 0`) |
| `ErrPinTrustLevelTooLow` | 1215 | Retained | Caller below `pin_min_trust_level` |

### 18.18. Access Control

| Operation | Who Can Execute |
|-----------|-----------------|
| Create anonymous collection | Via `MsgShieldedExec` wrapping `MsgCreateCollection`; x/shield verifies ZK proof (domain 21, `min_trust_level=1`) |
| Manage anonymous collection (add/remove/update/reorder items, update metadata) | Via `MsgShieldedExec` wrapping the appropriate inner message; x/shield handles proof/nullifier verification |
| Pin anonymous collection | Active x/rep member at `pin_min_trust_level`+ |
| Flag anonymous collection | Any x/rep member (same as regular collections) |
| Hide anonymous collection | Active x/forum sentinel (same as regular collections) |
| Appeal hide on anonymous collection | **Nobody** — owner is module account (cannot sign). Anonymous collections rely on TTL expiry or sentinel discretion |
| Upvote/downvote anonymous collection | Any x/rep member (regular), or via `MsgShieldedExec` wrapping `MsgUpvoteContent`/`MsgDownvoteContent` (anonymous, domains 22/23) |
| Rate anonymous collection (curator) | Active curator (same as regular collections) |
| Stake conviction on anonymous collection | Any active x/rep member except the author (via x/rep content staking) |
| Delete anonymous collection | **Nobody** directly — expires via TTL, or deleted by sentinel moderation |

**Appeal limitation:** Since the collection owner is the module account (set by x/shield during shielded execution), no one can sign an `MsgAppealHide` on behalf of an anonymous collection. If a sentinel hides an anonymous collection unfairly, the community's recourse is: (1) flag the sentinel for abuse via x/forum sentinel accountability mechanisms, (2) re-create the collection anonymously after the hide expires, or (3) raise the issue through governance. This tradeoff is inherent to anonymity — accountability and anonymity are in tension, and sentinel moderation provides the safety net.

### 18.19. Security Considerations

> **Note:** Many security concerns previously handled per-module (ZK proof verification, nullifier uniqueness, management key security, relay correlation) are now handled centrally by x/shield. The considerations below reflect the current architecture.

#### 18.19.1. Anonymity Set

The anonymity set for anonymous collection creation is all active members at or above the proven trust level. With x/shield's registered shielded ops, x/collect uses `min_trust_level=1` (PROVISIONAL). x/shield governance can adjust the minimum trust level per registered operation if the anonymity set is too small or quality guarantees need tightening.

#### 18.19.2. Execution Mode Privacy

x/shield offers two execution modes: **Immediate** (lower latency, inner message content visible on-chain) and **Encrypted Batch** (maximum privacy via TLE + batching, inner message content hidden until epoch decryption). For sensitive anonymous collections (e.g., fraud watchlists), encrypted batch mode prevents observers from correlating submission timing with content. See `docs/x-shield-spec.md` for details.

#### 18.18.4. Conviction Gaming

Anonymous collection authors cannot stake conviction on their own content (x/rep author exclusion enforced via the management key → author bond link). Sybil conviction staking costs `min_stake_duration_seconds` of locked DREAM per fake identity, making it expensive.

#### 18.18.5. Fraud Accusation Abuse

Anonymous fraud watchlists can themselves be weaponized (false accusations to damage rivals). Mitigations:
- Conviction staking requires economic commitment — false accusations don't attract genuine community conviction
- Sentinel moderation can hide defamatory collections
- Ephemeral TTL ensures false watchlists expire naturally without community support (pinning)
- ESTABLISHED+ trust level requirement raises the Sybil cost for creating malicious watchlists

### 18.20. Module-Paid Gas (via x/shield)

> **Status: Per-module subsidy REMOVED.** The per-module anonymous posting subsidy (`anon_subsidy_budget_per_epoch`, `anon_subsidy_max_per_action`, `anon_subsidy_relay_addresses`) has been removed from x/collect. Gas subsidies for anonymous operations are now handled centrally by x/shield's module-paid gas system.

x/shield's module account pays transaction fees for all `MsgShieldedExec` operations, funded automatically from the community pool via BeginBlocker with a governance-controlled epoch cap (`max_funding_per_epoch`). The submitter of a shielded execution needs zero balance. Per-identity rate limiting in x/shield prevents gas abuse. See `docs/x-shield-spec.md` for details.

### 18.21. CLI Commands

> **Status: Partially removed.** CLI commands for `create-anonymous-collection`, `manage-anonymous-collection`, `anonymous-react`, and queries `anonymous-collection-meta` and `is-collect-nullifier-used` have been removed. Anonymous operations are now submitted via x/shield CLI.

```bash
# Anonymous collection creation (via x/shield)
sparkdreamd tx shield shielded-exec \
  --inner-msg '{"@type":"/sparkdream.collect.v1.MsgCreateCollection","creator":"","type":1,"visibility":1,"name":"Fraud Watchlist","description":"Suspected fraudulent art","tags":["fraud","watchlist"],"expires_at":432000}' \
  --proof <base64-proof> --nullifier <hex> --merkle-root <hex> \
  --from relay1

# Pin anonymous collection (member action — remains in x/collect)
sparkdreamd tx collect pin-collection 42 --from alice

# Queries (remaining in x/collect)
sparkdreamd query collect collection-conviction 42

# Nullifier queries (now via x/shield)
sparkdreamd query shield nullifier-used --domain 21 --scope 12345 --nullifier <hex>
```

### 18.22. Genesis State Additions

> **Status: REMOVED.** The `anonymous_collection_metas`, `anon_nullifiers`, and `anon_collection_nonces` genesis fields have been removed from x/collect's `GenesisState`. Nullifier state is now part of x/shield's genesis. No anonymous-specific genesis fields remain in x/collect.

### 18.23. Keeper Interface Additions

> **Status: Partially removed.** The `CreateAnonymousCollection`, `ManageAnonymousCollection`, `GetAnonymousCollectionMeta`, `IsCollectNullifierUsed`, and `SetCollectNullifierUsed` methods have been removed from x/collect's keeper. Anonymous operations are dispatched by x/shield to x/collect's standard message handlers.

Remaining keeper additions:

```go
// Pinning
PinCollection(ctx context.Context, msg *MsgPinCollection) error

// ShieldAware interface (see x/collect/keeper/shield_aware.go)
IsShieldCompatible(ctx context.Context, msg sdk.Msg) bool
```

The `IsShieldCompatible` method implements x/shield's `ShieldAware` interface, returning `true` for `MsgCreateCollection`, `MsgUpvoteContent`, and `MsgDownvoteContent` — the messages that support anonymous execution via `MsgShieldedExec`.

### 18.24. Dependencies

> **Status: Updated.** x/vote has been eliminated. x/common anonymous helpers are no longer used by x/collect (nullifiers are centralized in x/shield).

New dependencies for anonymous functionality (in addition to existing §2):

| Module | Purpose |
|--------|---------|
| `x/shield` | Unified privacy layer: handles `MsgShieldedExec` dispatch, ZK proof verification, nullifier management, module-paid gas. x/collect implements x/shield's `ShieldAware` interface |
| `x/rep` | `GetMemberTrustTreeRoot()`, `GetPreviousMemberTrustTreeRoot()` for trust tree roots (used by x/shield for proof validation); `CreateAuthorBond()` for optional author bonds; content conviction staking queries |

x/shield is a **leaf dependency** (depends on x/rep, x/distribution, bank, auth; nothing depends on x/shield). x/collect does not directly import x/shield — the integration is via the `ShieldAware` interface that x/shield queries at runtime.

---

## 19. File References

- Proto definitions: `proto/sparkdream/collect/v1/`
- Module code: `x/collect/`
- Keeper: `x/collect/keeper/`
- Types: `x/collect/types/`
- Integration tests: `test/collect/`
