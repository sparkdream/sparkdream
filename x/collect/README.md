# `x/collect`

The `x/collect` module is a decentralized collection management system for curating and referencing both on-chain and off-chain content. Collections are ordered, collaborative groupings of items that can reference NFTs, external links, blockchain entities, or custom data.

## Overview

This module provides:

- **Flexible referencing** — items support NFTs, external links, on-chain references (blog posts, forum threads), and custom typed data
- **Collaborative collections** — multiple members can manage a collection with role-based permissions (EDITOR, ADMIN)
- **Privacy-first design** — client-side encryption for private collections; single opaque blob model
- **Two-tier content system** — members create permanent collections; non-members create TTL (ephemeral) collections with sponsorship pathway to permanence
- **Quality curation** — bonded curators rate public collections with verdicts and tags; challenges via `x/rep` jury system
- **Community reactions** — members upvote (free) or downvote (25 SPARK cost) public collections and items
- **Sentinel moderation** — `x/forum` sentinels can hide inappropriate content with appeal mechanism and bond accountability
- **Tiered collection limits** — capacity scales with `x/rep` trust level
- **Anonymous operations** — anonymous collections and reactions via `x/shield`'s `MsgShieldedExec`
- **Conviction renewal** — anonymous collections can be sustained if community conviction staking meets threshold

## Concepts

### Collection Lifecycle

- **Non-member path**: Create TTL collection → Seek endorsement (PENDING status) → Get member endorsement → Become ACTIVE
- **Member path**: Create permanent or TTL collection → ACTIVE immediately
- **Sponsorship path**: Non-member pays for permanent deposits; trusted member vouches and converts collection to permanent

### Deposit Model

Three types of deposits:

1. **Collection deposit** — base fee per collection (1 SPARK default)
2. **Per-item deposit** — fee per item added (0.1 SPARK default)
3. **Non-member surcharges** — endorsement creation fee (10 SPARK, split on endorsement) + per-item spam tax (0.5 SPARK/item, burned)

For **TTL collections**: deposits held and refunded at expiry. For **permanent collections**: deposits burned immediately (reflects permanent state cost).

### Visibility and Encryption

- **Public**: structured fields (name, description, cover_uri, tags) visible on-chain; items with references visible
- **Private**: single encrypted blob; no structured field visibility; requires client-side decryption
- Both visibility and encryption flags are immutable after creation

### Reaction System

- **Upvotes**: free, limited per day (`max_upvotes_per_day`)
- **Downvotes**: cost 25 SPARK (burned), limited per day (`max_downvotes_per_day`)
- Owners can disable community reactions via `community_feedback_enabled` flag
- Denormalized counters on collections and items for fast queries

### Curation System

- Curators register by staking DREAM bond (minimum `min_curator_bond`, default 500 DREAM)
- Curators rate public collections with verdict (UP/DOWN) and descriptive tags
- One active review per curator per collection
- Reviews can be challenged by jury members; challenge window ~7 days
- Successful challenges slash curator bond (10% default)

### Sponsorship Pathway

Non-member requests sponsorship → escrowed deposit paid → member sponsors (pays `sponsor_fee`) → deposits burned → collection permanent. Item additions are locked during pending request (prevents escrow mismatch). Sponsorship expires after ~7 days if not claimed; deposits refunded.

### Anonymous Collections

Anonymous collections are created via `x/shield`'s `MsgShieldedExec` wrapping `MsgCreateCollection`. The shield module verifies ZK proofs demonstrating membership and minimum trust level without revealing identity. Nullifiers prevent double-creation while preserving privacy. The shield module pays gas fees so submitters need zero balance. Pinning converts anonymous ephemeral collections to permanent; the pinner becomes the new owner.

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Collection` | `collection/value/{id}` | Collection metadata, status, visibility, encryption |
| `Item` | `item/value/{id}` | Collection item with references and attributes |
| `Collaborator` | `collaborator/value/{collectionID}/{address}` | Collaborator role entry (EDITOR, ADMIN) |
| `Curator` | `curator/value/{address}` | Curator registration and bond |
| `CurationReview` | `curation_review/value/{id}` | Curator review record |
| `CurationSummary` | `curation_summary/value/{collectionID}` | Aggregated verdict counts and top tags |
| `SponsorshipRequest` | `sponsorship_request/value/{collectionID}` | Pending sponsorship request |
| `CollectionFlag` | `flag/value/{targetType}:{targetID}` | Flag records and metadata |
| `HideRecord` | `hide_record/value/{id}` | Sentinel hide action record |
| `Endorsement` | `endorsement/value/{collectionID}` | Endorsement record (member voucher) |
| `ReactionDedup` | `reaction/dedup/{address}:{targetType}:{targetID}` | User's latest reaction type per target |

### Indexes

| Index | Purpose |
|-------|---------|
| `CollectionsByOwner` | Query collections owned by address |
| `CollectionsByExpiry` | TTL pruning in EndBlocker |
| `CollectionsByStatus` | Query by status (ACTIVE, PENDING, HIDDEN) |
| `ItemsByCollection` | Items ordered within collection |
| `ItemsByOnChainRef` | Collections referencing specific on-chain content |
| `CollaboratorReverse` | Collections where address is collaborator |
| `CurationReviewsByCollection` | Reviews for specific collection |
| `CurationReviewsByCurator` | Reviews by specific curator |
| `SponsorshipRequestsByExpiry` | TTL pruning of expired requests |
| `FlagReviewQueue` | Flagged content in review queue |
| `FlagExpiry` | Flag expiration index |
| `HideRecordByTarget` | Hide records for specific content |
| `HideRecordExpiry` | Hide/appeal timeout index |
| `EndorsementPending` | Unendorsed collections auto-prune |
| `EndorsementStakeExpiry` | Endorser stake release index |
| `ReactionLimit` | Per-address daily reaction counter |

## Messages

### Collection Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateCollection` | Create collection with type, visibility, encryption, TTL, metadata | Any address (members get permanent; non-members get TTL + PENDING) |
| `MsgUpdateCollection` | Update name, description, cover_uri, tags, type, TTL | Owner only |
| `MsgDeleteCollection` | Delete collection and all items; refund deposits | Owner only |

### Item Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgAddItem` | Add single item to collection | Owner or EDITOR/ADMIN collaborator |
| `MsgAddItems` | Batch add items (up to `max_batch_size`) | Owner or EDITOR/ADMIN collaborator |
| `MsgUpdateItem` | Update item title, description, image_uri, references, attributes | Owner or EDITOR/ADMIN collaborator |
| `MsgRemoveItem` | Remove single item; refund per-item deposit (TTL only) | Owner or EDITOR/ADMIN collaborator |
| `MsgRemoveItems` | Batch remove items | Owner or EDITOR/ADMIN collaborator |
| `MsgReorderItem` | Change item position; triggers compaction | Owner or EDITOR/ADMIN collaborator |

### Collaborator Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgAddCollaborator` | Add member as EDITOR or ADMIN | Owner only |
| `MsgRemoveCollaborator` | Remove collaborator | Owner only |
| `MsgUpdateCollaboratorRole` | Change EDITOR/ADMIN; only owner can grant ADMIN | Owner only |

### Curation

| Message | Description | Access |
|---------|-------------|--------|
| `MsgRegisterCurator` | Register as curator, lock DREAM bond | Active member meeting trust level |
| `MsgUnregisterCurator` | Unregister, release bond (if no pending challenges) | Curator only |
| `MsgRateCollection` | Submit review with verdict, tags, comment | Active curator (one per collection) |
| `MsgChallengeReview` | Challenge curator review within window | Jury members via `x/rep` |

### Sponsorship

| Message | Description | Access |
|---------|-------------|--------|
| `MsgRequestSponsorship` | Non-member requests sponsorship, escrows deposits | Collection owner (non-member, TTL only) |
| `MsgCancelSponsorshipRequest` | Cancel pending request, refund escrowed deposits | Collection owner |
| `MsgSponsorCollection` | Member sponsors, burns deposits, converts to permanent | ESTABLISHED+ member |

### Endorsement

| Message | Description | Access |
|---------|-------------|--------|
| `MsgSetSeekingEndorsement` | Signal readiness for endorsement | Collection owner (PENDING status) |
| `MsgEndorseCollection` | Endorse non-member collection; splits endorsement fee | Any member |

### Reactions

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpvoteContent` | Upvote public collection or item | Any member |
| `MsgDownvoteContent` | Downvote (costs 25 SPARK, burned) | Any member |

### Moderation

| Message | Description | Access |
|---------|-------------|--------|
| `MsgFlagContent` | Report inappropriate content (builds review queue) | Any member |
| `MsgHideContent` | Hide flagged content (requires sentinel bond) | Active sentinel (`x/forum`) |
| `MsgAppealHide` | Appeal hide decision; 50% fee refunded on timeout | Content owner |

### Anonymous Operations (via x/shield)

Anonymous collections and reactions are submitted via `x/shield`'s `MsgShieldedExec` wrapping standard collect messages (`MsgCreateCollection`, `MsgUpvoteContent`, `MsgDownvoteContent`). The shield module handles ZK proof verification, nullifier management, and module-paid gas. The collect module implements the `ShieldAware` interface to validate shielded messages.

### Pinning

| Message | Description | Access |
|---------|-------------|--------|
| `MsgPinCollection` | Convert ephemeral collection to permanent (burns deposits) | ESTABLISHED+ trust level |

### Parameter Updates

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update governance-controlled parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Commons Council Operations Committee |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `Collection` | Single collection by ID |
| `CollectionsByOwner` | Paginated collections owned by address |
| `PublicCollections` | All ACTIVE public collections |
| `PublicCollectionsByType` | Filter public collections by type |
| `CollectionsByCollaborator` | Collections where address is collaborator |
| `CollectionsByContent` | Collections referencing specific on-chain entity |
| `CollectionConviction` | Conviction score, stakes, and author bond |
| `PendingCollections` | PENDING endorsement-seeking collections |
| `Item` | Single item by ID |
| `Items` | Paginated items in collection |
| `ItemsByOwner` | Items added by address |
| `Collaborators` | Collaborators for a collection |
| `Curator` | Curator registration and bond info |
| `ActiveCurators` | Paginated list of active curators |
| `CurationSummary` | Aggregated verdict counts and top tags |
| `CurationReviews` | Individual reviews for a collection |
| `CurationReviewsByCurator` | Reviews by a specific curator |
| `SponsorshipRequest` | Current sponsorship request |
| `SponsorshipRequests` | Paginated list of all pending sponsorship requests |
| `Endorsement` | Endorsement record for collection |
| `ContentFlag` | Flag record and metadata for target |
| `FlaggedContent` | Paginated flagged content in review queue |
| `HideRecord` | Single hide record by ID |
| `HideRecordsByTarget` | Hide records for specific content |

## Parameters

### Governance-Controlled

These can only be changed via `x/gov` proposal (`MsgUpdateParams`).

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `max_collections_base` | uint32 | 5 | Non-member base collection limit |
| `max_collections_per_trust_level` | uint32 | 15 | Additional collections per trust level |
| `max_items_per_collection` | uint32 | 500 | Items per collection ceiling |
| `max_title_length` | uint32 | 128 | Item title max length |
| `max_name_length` | uint32 | 128 | Collection name max length |
| `max_description_length` | uint32 | 1,024 | Collection/item description max length |
| `max_tag_length` | uint32 | 32 | Tag max length |
| `max_tags_per_collection` | uint32 | 10 | Tags per collection ceiling |
| `max_attributes_per_item` | uint32 | 20 | Attributes per item ceiling |
| `max_attribute_key_length` | uint32 | 64 | Attribute key max length |
| `max_attribute_value_length` | uint32 | 256 | Attribute value max length |
| `max_reference_field_length` | uint32 | 256 | Reference field max length |
| `max_collaborators_per_collection` | uint32 | 20 | Collaborators per collection ceiling |
| `max_batch_size` | uint32 | 50 | Max items in batch operations |
| `max_encrypted_data_size` | uint32 | 4,096 | Encrypted blob max size |
| `max_ttl_blocks` | int64 | 0 | Member TTL ceiling (0 = unlimited) |
| `max_non_member_ttl_blocks` | int64 | 432,000 | Non-member TTL ceiling (~30 days) |
| `max_prune_per_block` | uint32 | 100 | EndBlocker prune operations per block |

### Operationally-Controlled

These can be updated by the Commons Council Operations Committee via `MsgUpdateOperationalParams`.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `base_collection_deposit` | Int | 1,000,000 | Collection creation deposit (uspark) |
| `per_item_deposit` | Int | 100,000 | Per-item storage deposit (uspark) |
| `per_item_spam_tax` | Int | 500,000 | Non-member per-item surcharge (burned) |
| `sponsor_fee` | Int | 1,000,000 | Member sponsor fee (burned) |
| `min_sponsor_trust_level` | string | ESTABLISHED | Min trust to sponsor |
| `sponsorship_request_ttl_blocks` | int64 | 100,800 | Sponsorship request expiry (~7 days) |
| `min_curator_bond` | Int | 500 | Min DREAM bond for curator registration |
| `min_curator_trust_level` | string | PROVISIONAL | Min trust to register as curator |
| `min_curator_age_blocks` | int64 | 14,400 | Min curator registration age (~1 day) |
| `max_tags_per_review` | uint32 | 5 | Tags per curation review |
| `max_review_comment_length` | uint32 | 512 | Review comment max length |
| `max_reviews_per_collection` | uint32 | 20 | Reviews per collection ceiling |
| `curator_slash_fraction` | LegacyDec | 10% | Curator bond slash on challenge |
| `challenge_reward_fraction` | LegacyDec | 80% | Fraction of slashed bond to challenger |
| `challenge_window_blocks` | int64 | 100,800 | Review challenge window (~7 days) |
| `challenge_deposit` | Int | 250 | DREAM deposit to challenge a review |
| `max_challenge_reason_length` | uint32 | 1,024 | Challenge reason max length |
| `downvote_cost` | Int | 25,000,000 | Downvote fee in uspark (burned) |
| `max_upvotes_per_day` | uint32 | 100 | Per-address daily upvote limit |
| `max_downvotes_per_day` | uint32 | 20 | Per-address daily downvote limit |
| `flag_review_threshold` | uint32 | 5 | Flag count to enter review queue |
| `max_flags_per_day` | uint32 | 20 | Per-address daily flag limit |
| `max_flaggers_per_target` | uint32 | 50 | Max flaggers per target |
| `flag_expiration_blocks` | int64 | 100,800 | Flag expiry (~7 days) |
| `max_flag_reason_length` | uint32 | 512 | Flag reason text max length |
| `sentinel_commit_amount` | Int | 100 | DREAM sentinel commits to hide |
| `hide_expiry_blocks` | int64 | 100,800 | Unappealed hide expiry (~7 days) |
| `appeal_fee` | Int | 5,000,000 | SPARK fee to appeal hide |
| `appeal_cooldown_blocks` | int64 | 600 | Appeal cooldown period (~1 hour) |
| `appeal_deadline_blocks` | int64 | 201,600 | Appeal deadline (~14 days) |
| `endorsement_creation_fee` | Int | 10,000,000 | Escrowed non-member collection fee |
| `endorsement_dream_stake` | Int | 100 | DREAM endorser stakes |
| `endorsement_stake_duration` | int64 | 432,000 | Endorser stake lock period (~30 days) |
| `endorsement_expiry_blocks` | int64 | 432,000 | Unendorsed collection expiry (~30 days) |
| `endorsement_fee_endorser_share` | LegacyDec | 80% | Endorser's share of endorsement fee |
| `endorsement_deletion_burn_fraction` | LegacyDec | 10% | Fraction burned on endorsed collection deletion |
| `conviction_renewal_threshold` | LegacyDec | 0 | Conviction to sustain anonymous TTL (0 = disabled) |
| `conviction_renewal_period` | int64 | 432,000 | TTL extension period (~30 days) |
| `pin_min_trust_level` | uint32 | 2 | Min trust to pin collections |
| `max_pins_per_day` | uint32 | 10 | Per-address daily pin limit |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec |
| `x/bank` | Yes | Deposit/fee collection, burning, refunds |
| `x/rep` | Yes | Membership, trust levels, DREAM bonding, conviction staking, author bonds |
| `x/commons` | No | Council authorization for operational parameter updates |
| `x/blog` | No | Validate on-chain reference items pointing to blog posts/replies |
| `x/forum` | No | Sentinel status checks, bond commitment/release/slash, tag registry, forum post references |
| `x/shield` | No | ZK proof verification and module-paid gas for anonymous operations |

## EndBlocker

The EndBlocker processes up to `max_prune_per_block` state changes across six tasks (shared budget):

1. **TTL Collection Pruning** — expire collections past their `expires_at`:
   - Anonymous collection with conviction >= threshold: renew TTL
   - Otherwise: delete collection, refund TTL deposits

2. **Sponsorship Request Expiry** — refund escrowed deposits for expired requests

3. **Hide Record Expiry** — process unappealed and appealed hides:
   - Unappealed: delete content, release sentinel bond
   - Appealed: restore content, refund 50% appeal fee, release sentinel bond

4. **Flag Expiry** — remove expired flags from review queue

5. **Unendorsed Collection Pruning** — delete PENDING collections past endorsement expiry (minus burn fraction)

6. **Endorsement Stake Release** — unlock DREAM to endorsers after lock period

## Events

All state-changing operations emit typed events for indexing and client notification.

## Client

### CLI

```bash
# Collections
sparkdreamd tx collect create-collection --name "My List" --type GENERAL --from alice
sparkdreamd tx collect update-collection 1 --name "Updated" --from alice
sparkdreamd tx collect delete-collection 1 --from alice

# Items
sparkdreamd tx collect add-item 1 --title "Item" --from alice
sparkdreamd tx collect remove-item 1 --from alice

# Curation
sparkdreamd tx collect register-curator --from bob
sparkdreamd tx collect rate-collection 1 UP --from bob

# Reactions
sparkdreamd tx collect upvote-content collection 1 --from bob
sparkdreamd tx collect downvote-content collection 1 --from bob

# Queries
sparkdreamd q collect collection 1
sparkdreamd q collect public-collections
sparkdreamd q collect items 1
sparkdreamd q collect params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/collect/v1/query.proto` for the full API surface.
