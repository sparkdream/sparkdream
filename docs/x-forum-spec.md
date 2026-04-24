# Technical Specification: `x/forum`

> **Scope**
>
> `x/forum` owns content storage, moderation, bounties, appeals, and thread operations. Tag registry/moderation/budgets, bonded-role accountability (sentinel bond/status/slash), and member-level accountability (reports, warnings, gov-action appeals) live in `x/rep` — consult [`docs/x-rep-spec.md`](x-rep-spec.md) and [`docs/bonded-role-generalization.md`](bonded-role-generalization.md) for those primitives. Forum's `SentinelActivity` holds only per-action counters (hides/locks/moves/pins/proposals, per-epoch tallies, local cooldowns); sentinel auth/bond mechanics go through the rep keeper's role-typed API (`IsBondedRole(ROLE_TYPE_FORUM_SENTINEL, …)`, `GetBondedRole`, `GetAvailableBond`, `ReserveBond`, `ReleaseBond`, `SlashBond`, `RecordActivity`, `SetBondStatus`).
>
> Sections that diverge from the current implementation are annotated with `> **Implementation status:**` or `> **Implementation note:**` callouts.
>
> Drift notes:
>
> | Area | Spec | Implementation |
> |------|------|----------------|
> | **Parameters** | ~136 fields across economics, sentinel, anti-gaming | ~30 fields in `Params` + `ForumOperationalParams` (Section 4.6.1) — many design params hardcoded in keeper |
> | **Genesis** | Includes reward_pool, epoch tracking | Ignite CRUD pattern (Section 4.7.1) |
> | **EndBlocker** | Multi-phase: GC + reward distribution + accuracy decay | Ephemeral post pruning only (Section 7.2.1); sentinel reward distribution is implemented in the x/rep EndBlocker — see x/rep spec "Sentinel Rewards" |
> | **Queries** | Custom query names | Ignite CRUD `Get/List` pairs; tag/budget/member-report/appeal queries are served by x/rep |
> | **Tags/ModerationReason** | In `forum/v1/` | `ModerationReason` + `FlagRecord` in `sparkdream.common.v1.*`; `Tag` and `ReservedTag` live in `sparkdream.rep.v1.*` |
> | **Anonymous features** | Full ZK-SNARK anonymous posting (Section 16) | **REMOVED** — Per-module anonymous messages (`MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, `MsgAnonymousReact`) deleted. Anonymous operations now routed through `x/shield`'s unified `MsgShieldedExec`. Forum implements `ShieldAware` interface (see `x/forum/keeper/shield_aware.go`). |
> | **Post proto** | Design uses `id` (field 1), `tags` (field 7), `archive_count` (field 26) | Implementation uses `post_id` (field 1), `tags` (field 30), no `archive_count`. Added: `content_type` (field 31), `initiative_id` (field 32), `conviction_sustained` (field 33) |
> | **Category proto** | Design includes `allow_anonymous` (field 6) | Implementation does NOT have `allow_anonymous` — per-category anonymous toggle is design-only |
> | **Error codes** | Sequential 1-166 | Organized by category 1100-2499 (see Section 10) |
> | **Messages** | ~30 messages in original design | Forum owns content/moderation/bounty/appeal/thread-op messages only; tag, tag-budget, sentinel-bond, and member-accountability messages live in x/rep. |
> | **Conviction renewal** | Not in original design | Added: `conviction_renewal_threshold`, `conviction_renewal_period` params; `conviction_sustained` field on Post |
> | **ForumHooks** | Interface for x/season XP integration | **Not yet implemented** |
> | **Module invariants** | Balance, bond, state invariants | **Not yet implemented** |
> | **New in implementation** | — | `MsgUpdateOperationalParams`, `ForumOperationalParams`, `cost_per_byte`, `cost_per_byte_exempt`, `author_bond` field on `MsgCreatePost`, `ContentType` enum from `sparkdream.common.v1` |

## 1. Abstract

The `x/forum` module implements a decentralized, censorship-resistant discussion platform on the Cosmos SDK. It utilizes a **Hierarchical Access Model** to manage state bloat, a **Dual-Token Sentinel System** (DREAM/SPARK) for decentralized moderation, and strict **Lazy Evaluation** patterns to minimize computational overhead.

The module outsources dispute resolution to `x/rep` and membership status to `x/commons`, focusing purely on content storage, organization, and optimistic moderation. Forum threads and replies are eligible for retroactive public goods nominations via `x/season` — community members can nominate outstanding forum contributions for DREAM rewards during the seasonal nomination window.

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/commons` | Source of Truth for Membership status, council authorization, and "HR Committee" (Authority) address |
| `x/rep` | Source of Truth for User Reputation Tiers, DREAM token operations (mint/burn/lock/transfer), member management, appeal initiatives, author bonds, content conviction staking, cross-module conviction propagation, **tag registry + tag moderation + tag budgets + sentinel bond/unbond + member accountability** |
| `x/bank` | Manages `SPARK` token transfers for taxes, fees, bounties, and flag fees |
| `x/common` | Shared proto types: `ModerationReason`, `FlagRecord`, `ContentType`; tag-validation helpers (`ValidateTagFormat`, `ValidateTagLength`) — `Tag`/`ReservedTag` moved to `sparkdream.rep.v1.*` |
| `x/shield` | **Indirect** — Anonymous operations (posting, replying, reacting) are routed through `x/shield`'s unified `MsgShieldedExec` entry point. Forum implements the `ShieldAware` interface so x/shield can dispatch shielded operations to it. Forum does NOT depend on x/shield directly; x/shield calls into forum. See [Section 16](#16-anonymous-features-via-xshield). |
| `x/season` | **Optional** — `SeasonKeeper.GetEpochDuration()` for epoch-based scoping. Falls back to `DefaultEpochDuration` (7 days) if nil |

> **Note:** Per-module anonymous messages (`MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, `MsgAnonymousReact`) have been **removed**. All anonymous operations are now routed through `x/shield`'s unified `MsgShieldedExec`. The forum keeper implements the `ShieldAware` interface (`x/forum/keeper/shield_aware.go`) to declare which messages are shield-compatible. ZK proof verification, nullifier management, and TLE infrastructure are all owned by x/shield. The `x/commons` module (not `x/group`) provides council authorization and proposal execution.

---

## 3. Core Concepts

### 3.1. Content Lifecycle & Storage

To prevent state explosion, the module treats content differently based on the author's status:

| Type | Author | Storage | Behavior |
|------|--------|---------|----------|
| **Ephemeral** | Non-Member | TTL-based | Pruned if not replied to by a Member within 24h |
| **Permanent** | Member | Indefinite | Stored permanently in IAVL tree |
| **Archived** | Any | Compressed | Threads inactive >30 days compressed to Gzip blob |
| **Tag** | Member (Tier 2+) | TTL-based | **Fee + Expiry**: Tags expire if unused for 30d; creation requires fee |

### 3.2. Dual-Token Economy

| Token | Type | Purpose |
|-------|------|---------|
| `DREAM` | Work Token (Illiquid) | **Bonding** - Users stake DREAM to become Sentinels (Moderators) |
| `SPARK` | Gas Token (Liquid) | **Payments** - Users pay Gas (execution) + Protocol Fees (Spam Tax, Tag Fee); Sentinels earn rewards |

### 3.3. Sentinel Bond Model

Sentinels stake DREAM as collateral for their moderation authority. The bond model uses three states:

| State | Bond Range | Behavior |
|-------|------------|----------|
| **NORMAL** | ≥ 1000 DREAM | Full sentinel privileges, rewards paid out directly |
| **RECOVERY** | 500-999 DREAM | Can still moderate, but rewards are auto-bonded until restored to 1000 |
| **DEMOTED** | < 500 DREAM | Loses sentinel privileges, must re-bond to minimum to restore |

**Slashing Mechanics:**
- **Fixed Amount:** 100 DREAM per overturned appeal (not percentage-based)
- **Consecutive Overturns:** Tracked separately; any upheld verdict resets the counter
- **Escalating Cooldown:** 24h base cooldown, doubles per consecutive overturn (max 7 days)
- **Demotion:** After 5 consecutive slashes (bond drops below 500), sentinel is demoted

**Dual Rewards:**
- **SPARK:** Paid from reward pool (accumulated spam taxes, tag fees) - variable based on accuracy score
- **DREAM:** Fixed amount minted per epoch for eligible sentinels (e.g., 10 DREAM) - independent of SPARK

**Recovery Process:**
- Sentinel remains active in RECOVERY mode (can still moderate)
- SPARK rewards paid out normally (sentinel needs gas money)
- DREAM rewards auto-bonded until bond reaches 1000 DREAM
- Once bond restored, status returns to NORMAL
- Sentinel can voluntarily unbond at any time (exits at a loss)

This model is simpler than debt-based systems because DREAM pools don't overlap - sentinel bonds are separate from conviction voting stakes and invitation bonds in x/rep.

### 3.4. Taxonomy

- **Categories:** High-level containers (e.g., "Governance"). Controlled rigidly by the HR Committee via Governance.
- **Tags:** Fluid descriptors (e.g., `#bug`). Created dynamically by Members (Tier 2+) for a **Fixed Fee**. Subject to expiry if unused ("Use it or lose it").

---

## 4. State Objects (Protobuf)

### 4.1. Post

> **Implementation note:** The actual proto (`proto/sparkdream/forum/v1/post.proto`) uses `post_id` instead of `id`, and does not have an `archive_count` field. Field numbering differs from the design spec below. The `tags` field is at field 30 (not 7), `content_type` is at field 31, `initiative_id` at field 32, and `conviction_sustained` at field 33.

```protobuf
// proto/sparkdream/forum/v1/post.proto (actual implementation)
syntax = "proto3";
package sparkdream.forum.v1;

import "sparkdream/common/v1/content_type.proto";
import "sparkdream/forum/v1/types.proto";

message Post {
  uint64 post_id = 1;
  uint64 category_id = 2;
  uint64 root_id = 3;                                    // ID of the thread starter (0 if this is root)
  uint64 parent_id = 4;                                  // Direct parent (0 if root)
  string author = 5;
  string content = 6;                                    // Post text content

  // Lifecycle
  int64 created_at = 7;
  int64 expiration_time = 8;                             // 0 = Permanent, >0 = Ephemeral

  // Moderation Metadata
  PostStatus status = 9;
  string hidden_by = 10;                                 // Sentinel (if HIDDEN)
  int64 hidden_at = 11;                                  // Timestamp when hidden (0 if not hidden)

  // Pin/Lock State (root posts only - these fields are ignored on replies)
  bool   pinned = 12;                                    // Pinned to top of category (root posts only)
  string pinned_by = 13;                                 // Who pinned it
  int64  pinned_at = 14;                                 // When pinned (0 if not pinned)
  uint64 pin_priority = 15;                              // Sort order among pinned posts (lower = higher)
  bool   locked = 16;                                    // Thread locked - no new replies to ANY post in thread
  string locked_by = 17;                                 // Who locked it
  int64  locked_at = 18;                                 // When locked (0 if not locked)
  string lock_reason = 19;                               // Optional reason for locking (displayed to users)

  // Reactions (aggregate counters — maintained alongside individual ReactionRecords)
  uint64 upvote_count = 20;                              // Total upvotes on this post (public + private)
  uint64 downvote_count = 21;                            // Total downvotes on this post (public + private)

  // Reply depth tracking (for max_reply_depth enforcement)
  uint64 depth = 22;                                     // 0 for root posts, parent.depth + 1 for replies

  // Edit tracking (for client display - no version history stored)
  bool   edited = 23;                                    // True if post has been edited
  int64  edited_at = 24;                                 // Timestamp of last edit (0 if never edited)

  // Tags and content type
  repeated string tags = 30;
  sparkdream.common.v1.ContentType content_type = 31;    // Content type enum from shared module

  // Cross-Module Conviction Propagation (optional initiative reference)
  uint64 initiative_id = 32;                              // x/rep initiative referenced by this post (0 = none, immutable after creation)
  bool conviction_sustained = 33;                         // True if content has entered conviction-sustained state (TTL extended by community conviction)
}

// PostStatus is defined in types.proto
enum PostStatus {
  POST_STATUS_UNSPECIFIED = 0;
  POST_STATUS_ACTIVE      = 1;
  POST_STATUS_HIDDEN      = 2; // Hidden by Sentinel, pending Appeal or Expiry
  POST_STATUS_DELETED     = 3; // Soft deleted (Tombstone)
  POST_STATUS_ARCHIVED    = 4; // Compressed (Root posts only)
}
```

### 4.2. Category

> **Implementation note:** The actual proto (`proto/sparkdream/forum/v1/category.proto`) uses `category_id` instead of `id`, and does **not** have an `allow_anonymous` field. The `allow_anonymous` per-category toggle described in Section 16.3 is a design-only feature not yet implemented.

```protobuf
// proto/sparkdream/forum/v1/category.proto (actual implementation)
syntax = "proto3";
package sparkdream.forum.v1;

message Category {
  uint64 category_id = 1;
  string title = 2;
  string description = 3;
  bool   members_only_write = 4;                         // Restrict posting to Members
  bool   admin_only_write = 5;                           // Restrict posting to HR Committee
}
```

### 4.3. Tag

> **Implementation note:** `Tag` and `ReservedTag` live in `sparkdream.rep.v1.*`. Forum posts reference tags by name only. Tag registry CRUD, `MsgCreateTag` (permissionless, trust-gated, fee-burned), tag expiry GC, `TagReport`, `MsgReportTag`, `MsgResolveTagReport`, and all five tag-budget messages live in x/rep. See [`docs/x-rep-spec.md`](x-rep-spec.md#tag-registry). `ModerationReason` and `FlagRecord` live in `sparkdream.common.v1.*`.

Schema (for reference — actual home is in the rep package):

```protobuf
// proto/sparkdream/rep/v1/tag.proto
message Tag {
    string name = 1;
    uint64 usage_count = 2;
    int64  created_at = 3;
    int64  last_used_at = 4;
    int64  expiration_index = 5;
}

// proto/sparkdream/rep/v1/reserved_tag.proto
message ReservedTag {
    string name = 1;
    string authority = 2;
    bool   members_can_use = 3;
}
```

Forum exposes a narrow `ForumKeeper` interface back to rep for tag moderation and tag-budget awards:

```go
type ForumKeeper interface {
    PruneTagReferences(ctx context.Context, tagName string) error
    GetPostAuthor(ctx context.Context, postID uint64) (string, error)
    GetPostTags(ctx context.Context, postID uint64) ([]string, error)
}
```

### 4.4. Rate Limiting & Sentinel Activity

```protobuf
// proto/forum/v1/limit.proto
syntax = "proto3";
package forum.v1;

import "cosmos_proto/cosmos.proto";

// Epoch-based rate limit - fixed memory, no unbounded growth
// Uses sliding window approximation: previous_epoch_count * overlap_ratio + current_epoch_count
message UserRateLimit {
  string user_address = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 current_epoch_count = 2;                        // Posts in current epoch
  uint64 previous_epoch_count = 3;                       // Posts in previous epoch (for overlap calculation)
  int64  current_epoch_start = 4;                        // Timestamp when current epoch started
  int64  last_post_time = 5;                             // Last post timestamp (for activity tracking)
}

// Unified reaction rate limit - tracks ALL reactions (upvotes + downvotes, public + private) per 24h rolling window
message UserReactionLimit {
  string user_address = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 current_day_count = 2;                          // Reactions in current 24h window
  uint64 previous_day_count = 3;                         // Reactions in previous 24h window (for overlap)
  int64  current_day_start = 4;                          // Timestamp when current window started
}

// Individual reaction record — enforces one reaction per user per post
// Stored for public reactions only; private reactions routed via x/shield (nullifiers managed by x/shield)
enum ReactionType {
  REACTION_TYPE_UNSPECIFIED = 0;
  REACTION_TYPE_UPVOTE     = 1;
  REACTION_TYPE_DOWNVOTE   = 2;
}

message ReactionRecord {
  uint64 post_id = 1;                                    // Post this reaction applies to
  string voter = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  ReactionType reaction_type = 3;                        // UPVOTE or DOWNVOTE
  int64  created_at = 4;                                 // Timestamp of reaction
}

// REMOVED — AnonymousReactionMetadata deleted. Private reactions now routed through
// x/shield's MsgShieldedExec. Nullifier tracking managed by x/shield's centralized store.

// Sentinel bond status for recovery mode tracking
// > **Implementation note:** `SentinelBondStatus` is defined in
// > `sparkdream.rep.v1.SentinelBondStatus`. The enum value set below is
// > retained for reference; the authoritative definition lives in the rep
// > package.
enum SentinelBondStatus {
  SENTINEL_BOND_STATUS_UNSPECIFIED = 0;                  // Proto3 convention: zero value must be UNSPECIFIED
  SENTINEL_BOND_STATUS_NORMAL = 1;                       // Bond >= min_sentinel_bond (1000 DREAM)
  SENTINEL_BOND_STATUS_RECOVERY = 2;                     // Bond < min_sentinel_bond but >= demotion_threshold
  SENTINEL_BOND_STATUS_DEMOTED = 3;                      // Bond < demotion_threshold (loses sentinel privileges)
}

// > **Implementation note (Phase 1–4 bonded-role generalization):** sentinel
// > state is split between x/rep and x/forum:
// >
// > - `sparkdream.rep.v1.BondedRole` (keyed by `(role_type, address)`) — generic
// >   accountability: `address`, `role_type = ROLE_TYPE_FORUM_SENTINEL`,
// >   `bond_status`, `current_bond`, `total_committed_bond`, `registered_at`,
// >   `last_active_epoch`, `consecutive_inactive_epochs`,
// >   `demotion_cooldown_until`, `cumulative_rewards`, `last_reward_epoch`.
// >
// > - `sparkdream.forum.v1.SentinelActivity` — forum-specific action counters:
// >   hides/locks/moves/pins/proposals totals, per-epoch tallies, upheld/overturned
// >   counts, local cooldowns (fields 1..29).
// >
// > Bonding flows through x/rep's generic `MsgBondRole` / `MsgUnbondRole`.
// > Forum content-action handlers authenticate and reserve bond via the rep
// > keeper using the role-typed API:
// > `IsBondedRole(ROLE_TYPE_FORUM_SENTINEL, addr)`,
// > `GetBondedRole(ROLE_TYPE_FORUM_SENTINEL, addr)`,
// > `GetAvailableBond(ROLE_TYPE_FORUM_SENTINEL, addr)`,
// > `ReserveBond(ROLE_TYPE_FORUM_SENTINEL, addr, amount)`,
// > `ReleaseBond(ROLE_TYPE_FORUM_SENTINEL, addr, amount)`,
// > `SlashBond(ROLE_TYPE_FORUM_SENTINEL, addr, amount, reason)`,
// > `RecordActivity(ROLE_TYPE_FORUM_SENTINEL, addr)`,
// > `SetBondStatus(ROLE_TYPE_FORUM_SENTINEL, addr, status, cooldown_until)`.
// >
// > The legacy unified `SentinelActivity` message below (with inline
// > `bond_status`, `current_bond`, etc.) is kept for reference; at runtime the
// > bond fields live on `BondedRole` and the action counters live on forum's
// > per-module `SentinelActivity`.

// Accuracy-based sentinel metrics - rewards based on moderation quality, not volume
message SentinelActivity {
  string address = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 total_hides = 2;                                // Total posts hidden by this sentinel
  uint64 upheld_hides = 3;                               // Hides not overturned by appeal
  uint64 overturned_hides = 4;                           // Hides overturned (sentinel was wrong)
  uint64 unchallenged_hides = 5;                         // Hides with no appeal filed (DO NOT count toward accuracy - prevents gaming)
  uint64 epoch_hides = 6;                                // Hides in current reward epoch (reset each epoch)
  uint64 epoch_appeals_resolved = 7;                     // Appeals resolved in current epoch
  int64  last_reward_epoch = 8;                          // Last epoch sentinel received rewards
  string cumulative_rewards = 9;                         // Total rewards earned (for cap tracking)
  int64  overturn_cooldown_until = 10;                   // Timestamp until which sentinel cannot hide (after losing appeal)
  uint64 consecutive_overturns = 11;                     // Consecutive overturned hides (resets after N consecutive upheld)
  SentinelBondStatus bond_status = 12;                   // Current bond status (NORMAL, RECOVERY, DEMOTED)
  string current_bond = 13;                              // Current DREAM bond amount (tracked separately from x/rep staking)
  string total_committed_bond = 14;                      // Total DREAM committed across all pending hides (available = current_bond - total_committed)
  uint64 pending_hide_count = 15;                        // Number of pending hides (invariant: each hide commits slash_amount)
  uint64 consecutive_upheld = 16;                        // Consecutive upheld hides (counter for reset threshold)
  int64  demotion_cooldown_until = 36;                   // Timestamp until re-bonding allowed after demotion (prevents reset attacks)
  uint64 epoch_appeals_filed = 17;                       // Appeals filed against this sentinel in current epoch (for appeal rate calc)

  // Thread lock tracking (for high-trust sentinels)
  uint64 total_locks = 18;                               // Total threads locked by this sentinel
  uint64 upheld_locks = 19;                              // Locks upheld on appeal (sentinel was right)
  uint64 overturned_locks = 20;                          // Locks overturned on appeal (sentinel was wrong)
  uint64 epoch_locks = 21;                               // Locks in current reward epoch (reset each epoch)

  // Thread move tracking
  uint64 total_moves = 24;                               // Total threads moved by this sentinel
  uint64 upheld_moves = 25;                              // Moves upheld on appeal (sentinel was right)
  uint64 overturned_moves = 26;                          // Moves overturned on appeal (sentinel was wrong)
  uint64 epoch_moves = 27;                               // Moves in current reward epoch (reset each epoch)

  // Curation tracking (pins and accept proposals)
  uint64 total_pins = 28;                                // Total replies pinned by this sentinel
  uint64 upheld_pins = 29;                               // Pins upheld on dispute (sentinel was right)
  uint64 overturned_pins = 30;                           // Pins overturned on dispute (author won)
  uint64 epoch_pins = 31;                                // Pins in current reward epoch
  uint64 total_proposals = 32;                           // Total accept proposals made
  uint64 confirmed_proposals = 33;                       // Proposals confirmed by author or auto-confirmed
  uint64 rejected_proposals = 34;                        // Proposals rejected by author
  uint64 epoch_curations = 35;                           // Total curation actions this epoch (pins + proposals)

  // Inactivity tracking (for accuracy decay)
  int64  last_active_epoch = 22;                         // Last epoch where sentinel had any moderation activity
  uint64 consecutive_inactive_epochs = 23;              // Epochs without activity (triggers decay)
}

// Snapshot of sentinel state at hide time - prevents unbonding to avoid slash
message HideRecord {
  uint64 post_id = 1;                                    // Hidden post ID
  string sentinel = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  int64 hidden_at = 3;                                   // Timestamp of hide action
  string sentinel_bond_snapshot = 4;                     // DREAM bonded at hide time
  string sentinel_backing_snapshot = 5;                  // DREAM delegated at hide time
  string committed_amount = 6;                           // DREAM committed for this specific hide (= slash_amount, locked from available bond)
  ModerationReason reason_code = 7;                      // Unified reason code (same as flag reasons)
  string reason_text = 8;                                // Custom reason text (required if reason_code=OTHER)
}

// Snapshot of sentinel state at thread lock time - for appeal/slashing
// Only created for sentinel-initiated locks (not HR Committee locks)
message ThreadLockRecord {
  uint64 root_id = 1;                                    // Locked thread root ID
  string sentinel = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  int64 locked_at = 3;                                   // Timestamp of lock action
  string sentinel_bond_snapshot = 4;                     // DREAM bonded at lock time
  string sentinel_backing_snapshot = 5;                  // DREAM delegated at lock time
  string lock_reason = 6;                                // Reason provided by sentinel
  bool   appeal_pending = 7;                             // Whether an appeal is currently active
  uint64 initiative_id = 8;                              // x/rep initiative ID if appeal filed (0 if no appeal)
}

// Snapshot of sentinel state at thread move time - for appeal/slashing
// Only created for sentinel-initiated moves (not HR Committee moves)
message ThreadMoveRecord {
  uint64 root_id = 1;                                    // Moved thread root ID
  string sentinel = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 original_category_id = 3;                       // Category before move
  uint64 new_category_id = 4;                            // Category after move
  int64  moved_at = 5;                                   // Timestamp of move action
  string sentinel_bond_snapshot = 6;                     // DREAM bonded at move time
  string sentinel_backing_snapshot = 7;                  // DREAM delegated at move time
  string move_reason = 8;                                // Reason provided by sentinel
  bool   appeal_pending = 9;                             // Whether an appeal is currently active
  uint64 initiative_id = 10;                             // x/rep initiative ID if appeal filed (0 if no appeal)
}

// ============================================
// MODERATION ACTION TYPES (for events)
// ============================================
// NOTE: On-chain moderation history storage is not implemented.
// These enums are used in events for off-chain indexing.

// Types of moderation actions (used in events)
enum ModerationActionType {
  MODERATION_ACTION_UNSPECIFIED = 0;
  MODERATION_ACTION_HIDE = 1;                            // Post hidden by sentinel
  MODERATION_ACTION_UNHIDE = 2;                          // Post restored (appeal won or HR action)
  MODERATION_ACTION_LOCK = 3;                            // Thread locked
  MODERATION_ACTION_UNLOCK = 4;                          // Thread unlocked
  MODERATION_ACTION_MOVE = 5;                            // Thread moved to different category
  MODERATION_ACTION_PIN = 6;                             // Post/thread pinned
  MODERATION_ACTION_UNPIN = 7;                           // Post/thread unpinned
  MODERATION_ACTION_DELETE = 8;                          // Post soft-deleted (appeal lost or expired)
  MODERATION_ACTION_FLAG_DISMISSED = 9;                  // Flags dismissed by sentinel
  MODERATION_ACTION_APPEAL_FILED = 10;                   // Appeal filed against action
  MODERATION_ACTION_APPEAL_UPHELD = 11;                  // Appeal upheld (moderator was wrong)
  MODERATION_ACTION_APPEAL_REJECTED = 12;                // Appeal rejected (moderator was right)
  MODERATION_ACTION_APPEAL_TIMEOUT = 13;                 // Appeal timed out
  MODERATION_ACTION_ARCHIVE = 14;                        // Thread archived
  MODERATION_ACTION_UNARCHIVE = 15;                      // Thread unarchived
}

// Source of moderation action (used in events)
enum ModerationSource {
  MODERATION_SOURCE_UNSPECIFIED = 0;
  MODERATION_SOURCE_SENTINEL = 1;                        // Action by sentinel (accountable)
  MODERATION_SOURCE_GOV_COMMITTEE = 2;                    // Action by HR Committee (authoritative)
  MODERATION_SOURCE_SYSTEM = 3;                          // Automated action (expiration, GC)
  MODERATION_SOURCE_APPEAL_JURY = 4;                     // Result of jury verdict
  MODERATION_SOURCE_AUTHOR = 5;                          // Action by content author (self-delete, etc.)
}

// Unified moderation reason - used for BOTH flags AND sentinel hide actions
// Shared vocabulary ensures consistency between community reports and moderation decisions
// NOTE: Moved to proto/sparkdream/common/v1/moderation_reason.proto (shared across modules)
enum ModerationReason {
  MODERATION_REASON_UNSPECIFIED = 0;
  MODERATION_REASON_SPAM = 1;                            // Unsolicited commercial content, repetitive posts
  MODERATION_REASON_HARASSMENT = 2;                      // Personal attacks, bullying, threats
  MODERATION_REASON_MISINFORMATION = 3;                  // False claims, misleading content
  MODERATION_REASON_OFF_TOPIC = 4;                       // Content unrelated to category/thread
  MODERATION_REASON_LOW_QUALITY = 5;                     // Low-effort posts, unclear content
  MODERATION_REASON_INAPPROPRIATE = 6;                   // Adult content, graphic material
  MODERATION_REASON_IMPERSONATION = 7;                   // Pretending to be someone else
  MODERATION_REASON_POLICY_VIOLATION = 8;                // Violates community guidelines
  MODERATION_REASON_DUPLICATE = 9;                       // Duplicate/cross-posted content
  MODERATION_REASON_SCAM = 10;                           // Fraudulent content, phishing attempts
  MODERATION_REASON_OTHER = 11;                          // Requires custom reason field
}

// Individual flag record - tracks each flagger's reason
// NOTE: Moved to proto/sparkdream/common/v1/flag_record.proto (shared across modules)
message FlagRecord {
  string flagger = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  ModerationReason reason_code = 2;                      // Structured reason (unified with sentinel hide reasons)
  string reason_text = 3;                                // Optional custom reason (required if reason_code=OTHER)
  int64  flagged_at = 4;                                 // When this flag was submitted
  uint64 weight = 5;                                     // Weight of this flag (member vs non-member)
}

// Post flag for sentinel review queue
// Lightweight mechanism for users to report posts without full member report process
message PostFlag {
  uint64 post_id = 1;                                    // Flagged post ID
  repeated string flaggers = 2;                          // Addresses that flagged (capped at max_post_flaggers)
  string total_weight = 3;                               // Total flag weight (members count more)
  int64  first_flag_at = 4;                              // When first flag was submitted
  int64  last_flag_at = 5;                               // Most recent flag timestamp
  bool   in_review_queue = 6;                            // True when weight threshold reached
  repeated FlagRecord flag_records = 7;                  // Detailed record of each flag (reason + text)
  map<int32, uint64> reason_counts = 8;                  // Count of flags per ModerationReason (for analytics)
}

// ============================================
// BOUNTY SYSTEM
// ============================================

// One-time bounty attached to a thread by author
message Bounty {
  uint64 id = 1;
  string creator = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 thread_id = 3;                                  // Thread this bounty is attached to
  string amount = 4;                                     // SPARK amount in escrow
  int64  created_at = 5;
  int64  expires_at = 6;
  BountyStatus status = 7;
  repeated BountyAward awards = 8;                       // Winners (can be multiple)

  // Moderation timer state (for proper pause/resume)
  int64  moderation_suspended_at = 9;                    // When bounty was paused due to moderation (0 = not suspended)
  int64  time_remaining_at_suspension = 10;              // Seconds remaining until expires_at when suspended
}

enum BountyStatus {
  BOUNTY_STATUS_UNSPECIFIED = 0;
  BOUNTY_STATUS_ACTIVE = 1;                              // Open for submissions
  BOUNTY_STATUS_AWARDED = 2;                             // Creator awarded winner(s)
  BOUNTY_STATUS_EXPIRED = 3;                             // Expired without full award
  BOUNTY_STATUS_CANCELLED = 4;                           // Creator cancelled (before any awards)
  BOUNTY_STATUS_MODERATION_PENDING = 5;                  // Thread moderated, waiting for appeal resolution
}

message BountyAward {
  uint64 post_id = 1;                                    // Winning reply
  string recipient = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string amount = 3;                                     // SPARK amount awarded
  string reason = 4;                                     // Public justification (required)
  int64  awarded_at = 5;
  uint32 rank = 6;                                       // 1 = first place, 2 = second, etc. (ties allowed)
}

// Tag budget for groups - simple pool for rewarding quality posts in their tag
// Simpler alternative to complex recurring bounty with periods/managers
message TagBudget {
  uint64 id = 1;
  string group_account = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"]; // Council/committee/guild account
  string tag = 3;                                        // Reserved tag this budget applies to
  string pool_balance = 4;                               // Remaining SPARK in budget pool
  bool   members_only = 5;                               // Only group members can receive awards
  int64  created_at = 6;
  bool   active = 7;                                     // False = paused (group can toggle)
}

// Record of tag budget awards (for history/analytics)
message TagBudgetAward {
  uint64 budget_id = 1;
  uint64 post_id = 2;                                    // Awarded post
  string recipient = 3 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string amount = 4;                                     // SPARK amount awarded
  string reason = 5;                                     // Public justification
  int64  awarded_at = 6;
  string awarded_by = 7 [(cosmos_proto.scalar) = "cosmos.AddressString"]; // Group member who awarded
}

// ============================================
// THREAD METADATA (Pinned/Accepted Replies)
// ============================================

// Metadata for thread-level features (pinned replies, accepted answer)
// Stored separately from Post to avoid bloating the Post message
message ThreadMetadata {
  uint64 thread_id = 1;                                  // Thread root post ID

  // Accepted reply (confirmed by author or auto-confirmed)
  uint64 accepted_reply_id = 2;                          // Reply marked as accepted answer (0 = none)
  string accepted_by = 3 [(cosmos_proto.scalar) = "cosmos.AddressString"]; // Who marked it accepted
  int64  accepted_at = 4;                                // When marked accepted

  // Proposed accepted reply (sentinel proposal awaiting author confirmation)
  uint64 proposed_reply_id = 7;                          // Sentinel's proposed accepted reply (0 = none)
  string proposed_by = 8 [(cosmos_proto.scalar) = "cosmos.AddressString"]; // Sentinel who proposed
  int64  proposed_at = 9;                                // When proposed (for auto-confirm timer)

  // Pinned replies
  repeated uint64 pinned_reply_ids = 5;                  // Replies pinned to top (max 3)
  repeated PinnedReplyRecord pinned_records = 6;         // Details of who pinned each reply
}

// Record of who pinned a reply and when
message PinnedReplyRecord {
  uint64 post_id = 1;                                    // Pinned reply ID
  string pinned_by = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  int64  pinned_at = 3;
  bool   is_sentinel_pin = 4;                            // True if pinned by sentinel (disputeable)
  bool   disputed = 5;                                   // True if author has filed dispute
  uint64 initiative_id = 6;                              // x/rep initiative ID if disputed (0 if not)
}

// Record for tracking sentinel pin disputes (for appeal resolution)
message SentinelPinRecord {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string sentinel = 3 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  int64  pinned_at = 4;
  string sentinel_bond_snapshot = 5;                     // DREAM bonded at pin time
  bool   appeal_pending = 6;
  uint64 initiative_id = 7;                              // x/rep initiative ID if appealed
}

// ============================================
// THREAD FOLLOWING (for off-chain notifications)
// ============================================

// User's follow status for a thread (enables off-chain notification systems)
message ThreadFollow {
  uint64 thread_id = 1;
  string follower = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  int64  followed_at = 3;
}

// Aggregate follow count stored on thread (avoids counting query)
message ThreadFollowCount {
  uint64 thread_id = 1;
  uint64 follower_count = 2;
}
```

### 4.5. Archived Thread

```protobuf
// proto/forum/v1/archive.proto
syntax = "proto3";
package forum.v1;

message ArchivedThread {
  uint64 root_id = 1;                                    // Original thread root ID
  bytes  compressed_data = 2;                            // Gzip-compressed thread data
  int64  archived_at = 3;
  uint64 post_count = 4;                                 // Number of posts in archive
  int64  last_unarchived_at = 5;                         // Last unarchive timestamp (for cooldown)
  // NOTE: archive_count moved to separate ArchiveMetadata storage (see below)
}

// Archive metadata stored SEPARATELY from compressed blob
// Prevents manipulation via corrupted archives or blob tampering
// This is the authoritative source for archive_count (not Post.archive_count)
message ArchiveMetadata {
  uint64 root_id = 1;                                    // Thread root ID (primary key)
  uint64 archive_count = 2;                              // Times archived - AUTHORITATIVE count (prevents griefing)
  int64  first_archived_at = 3;                          // When first archived (for historical tracking)
  int64  last_archived_at = 4;                           // Most recent archive timestamp
  bool   hr_override_required = 5;                       // True if archive_count >= max_archive_cycles
}

// Tag report for problematic tags
// NOTE: reporters list is capped at max_tag_reporters (default 50) to prevent state bloat
message TagReport {
  string tag_name = 1;
  repeated string reporters = 2;                         // Addresses that reported this tag (capped, see params)
  string total_bond = 3;                                 // Total SPARK bonded by reporters
  int64  first_report_at = 4;
  bool   under_review = 5;                               // Flagged for HR Committee review
}

// Member salvation eligibility and rate tracking
message MemberSalvationStatus {
  string address = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  int64  member_since = 2;                               // Timestamp when became member
  bool   can_salvage = 3;                                // Precomputed: member_since + min_membership_for_salvation <= now
  uint64 epoch_salvations = 4;                           // TOTAL POSTS salvaged in current epoch (not operations - prevents bypass)
  int64  epoch_start = 5;                                // Start of current salvation epoch (24h rolling)
}

// Jury participation tracking for timeout abuse prevention
// Jurors with low participation rates are excluded from future selections
message JuryParticipation {
  string juror = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 total_assigned = 2;                             // Total appeals assigned to this juror
  uint64 total_voted = 3;                                // Appeals where juror actually voted
  uint64 total_timeouts = 4;                             // Appeals that timed out where juror was assigned
  int64  last_assigned_at = 5;                           // For tracking recent activity
  bool   excluded = 6;                                   // True if participation rate too low (auto-excluded)
}

// Sentinel report against a member for pattern-based misconduct
// NOTE: reporters list is capped at max_member_reporters (default 20) to prevent state bloat
message MemberReport {
  string member = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  repeated string reporters = 2;                         // Sentinels who filed/co-signed (capped, see params)
  repeated uint64 evidence_post_ids = 3;                 // Posts demonstrating pattern
  string reason = 4;                                     // Explanation from reporters
  uint32 recommended_action = 5;                         // 0=warning, 1=demotion, 2=zeroing
  string total_bond = 6;                                 // Total SPARK bonded by reporters
  int64  created_at = 7;
  uint32 status = 8;                                     // 0=PENDING, 1=ESCALATED, 2=RESOLVED, 3=META_APPEALED
  string defense = 9;                                    // Member's response (if submitted)
  repeated uint64 defense_post_ids = 10;                 // Context posts from member
  int64  defense_submitted_at = 11;
}

// Formal warning record (persists after report resolution)
message MemberWarning {
  string member = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string reason = 2;
  repeated uint64 evidence_post_ids = 3;
  int64  issued_at = 4;
  string issued_by = 5;                                  // HR Committee address
  uint64 warning_number = 6;                             // 1st, 2nd, 3rd warning etc.
}

// HR Committee action appeal (meta-appeal to x/rep jury)
// Allows members to challenge HR Committee decisions (warnings, demotions, zeroing, thread moderation)
enum GovActionType {
  GOV_ACTION_TYPE_UNSPECIFIED = 0;
  GOV_ACTION_TYPE_WARNING = 1;
  GOV_ACTION_TYPE_DEMOTION = 2;
  GOV_ACTION_TYPE_ZEROING = 3;
  GOV_ACTION_TYPE_TAG_REMOVAL = 4;
  GOV_ACTION_TYPE_FORUM_PAUSE = 5;                        // Extended pause (>24h) can be appealed
  GOV_ACTION_TYPE_THREAD_LOCK = 6;                        // HR-initiated thread lock can be appealed by thread author
  GOV_ACTION_TYPE_THREAD_MOVE = 7;                        // HR-initiated thread move can be appealed by thread author
}

message GovActionAppeal {
  uint64 id = 1;                                         // Unique appeal ID
  string appellant = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"]; // Member appealing the action
  GovActionType action_type = 3;                          // Type of HR action being appealed
  string action_target = 4;                              // Target of original action (member address, tag name, or thread root ID)
  string original_reason = 5;                            // HR Committee's original justification
  string appeal_reason = 6;                              // Appellant's argument
  string appeal_bond = 7;                                // SPARK bonded for appeal
  int64  created_at = 8;
  int64  deadline = 9;                                   // Jury verdict deadline
  uint64 initiative_id = 10;                             // x/rep initiative ID for jury resolution
  uint32 status = 11;                                    // 0=PENDING, 1=UPHELD (HR wins), 2=OVERTURNED (appellant wins), 3=TIMEOUT
  uint64 original_category_id = 12;                      // For THREAD_MOVE appeals: category to restore on overturn (0 for other types)
}
```

### 4.6. Parameters

> **Implementation note:** The implemented `Params` message is significantly simpler than the full design specification below. Many sentinel economics, anti-gaming, and reward parameters from the design spec are **not yet implemented as on-chain params**. The keeper logic uses hardcoded values or direct x/rep integration for these features. The design section is preserved below for reference, followed by the actual implementation.

#### 4.6.1. Implemented Parameters (proto/sparkdream/forum/v1/params.proto)

```protobuf
message Params {
  // Emergency Controls (governance-only — cannot be changed via UpdateOperationalParams)
  bool forum_paused = 1;                                 // Emergency pause: stops all new posts
  bool moderation_paused = 2;                            // Pause all moderation actions
  bool appeals_paused = 5;                               // Pause new appeals

  // Feature Toggles
  bool bounties_enabled = 3;                             // Allow bounty creation
  bool reactions_enabled = 4;                            // Allow upvotes/downvotes
  bool editing_enabled = 6;                              // Allow post editing

  // Fees (SPARK)
  cosmos.base.v1beta1.Coin spam_tax = 7;                 // Charged to non-members for posting
  cosmos.base.v1beta1.Coin reaction_spam_tax = 8;        // Charged to non-members for reactions
  cosmos.base.v1beta1.Coin flag_spam_tax = 9;            // Charged to non-members for flagging
  cosmos.base.v1beta1.Coin downvote_deposit = 10;        // Burned when downvoting
  cosmos.base.v1beta1.Coin appeal_fee = 11;              // Charged for appeals
  cosmos.base.v1beta1.Coin lock_appeal_fee = 12;         // Charged for thread lock appeals
  cosmos.base.v1beta1.Coin move_appeal_fee = 13;         // Charged for thread move appeals
  cosmos.base.v1beta1.Coin edit_fee = 14;                // Charged for edits past grace period
  cosmos.base.v1beta1.Coin cost_per_byte = 28;           // Charged for on-chain content storage (burned)
  bool cost_per_byte_exempt = 29;                        // When true, disables cost_per_byte fee

  // Content Limits
  uint64 max_content_size = 16;                          // Max bytes for post content
  uint64 daily_post_limit = 17;                          // Max posts per user per day
  uint32 max_reply_depth = 18;                           // Max nesting depth for replies
  uint64 max_follows_per_day = 21;                       // Max follows per user per 24h

  // Bounty Limits
  uint64 bounty_cancellation_fee_percent = 15;           // % of bounty taken on cancellation (0-100)

  // Edit Windows
  int64 edit_grace_period = 19;                          // Seconds for free edits after creation
  int64 edit_max_window = 20;                            // Seconds after which editing is no longer allowed

  // Archival
  int64 archive_threshold = 22;                          // Seconds of inactivity before archiving allowed
  int64 unarchive_cooldown = 23;                         // Seconds after archive before unarchiving
  int64 archive_cooldown = 24;                           // Seconds after unarchive before re-archiving

  // Appeal Cooldowns
  int64 hide_appeal_cooldown = 25;                       // Seconds after hide before appeal
  int64 lock_appeal_cooldown = 26;                       // Seconds after lock before appeal
  int64 move_appeal_cooldown = 27;                       // Seconds after move before appeal

  // Ephemeral Content
  int64 ephemeral_ttl = 30;                              // TTL for non-member posts (default 86400 = 24h)

  // Cross-Module Conviction Propagation
  string conviction_renewal_threshold = 31 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];  // Min conviction score to renew content at TTL expiry (default: 100.0; 0 = disabled)
  int64 conviction_renewal_period = 32;                  // Duration in seconds to extend TTL by when conviction-renewed (default: 604800 = 7 days)
}
```

The module also defines `ForumOperationalParams` — a subset of `Params` that can be updated by the Commons Council Operations Committee via `MsgUpdateOperationalParams` without a full governance proposal. This excludes the three governance-only emergency controls (`forum_paused`, `moderation_paused`, `appeals_paused`).

#### 4.6.2. Design Parameters (Not Yet Implemented as On-Chain Params)

The following parameters from the original design are **not yet on-chain params** but are either hardcoded in keeper logic or planned for future implementation:

```protobuf
// NOTE: These are NOT in the current params.proto. They are preserved here
// as the design target for future implementation phases.

message Params {

  // Jury Selection
  uint64 min_jury_quorum = 87;                           // Min jurors required to form jury (e.g., 3) - below this triggers insufficiency fallback
  string jury_insufficiency_refund_ratio = 88 [
    (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec",
    (gogoproto.nullable) = false
  ];                                                     // Refund ratio on jury insufficiency (e.g., "0.90" = 90%)

  // Reactions (one reaction per user per post — upvote OR downvote, public or private)
  cosmos.base.v1beta1.Coin reaction_spam_tax = 129;      // Cost for non-members to react (e.g., 10 SPARK)
  uint64 max_reactions_per_day = 130;                    // Max reactions (upvotes + downvotes, public + private) per user per 24h (e.g., 100)
  bool   reactions_enabled = 131;                        // Toggle to enable/disable all reactions (default true)
  cosmos.base.v1beta1.Coin downvote_deposit = 132;       // SPARK burned to downvote (e.g., 50 SPARK) — no refund, applies to both public and private
  bool   private_reactions_enabled = 133;                // Toggle to enable/disable private reactions via x/shield (default true; requires reactions_enabled)

  // Post Editing (time-limited, fee-based after grace period)
  int64  edit_grace_period = 89;                         // Seconds after creation for free edits (e.g., 300 = 5 minutes)
  int64  edit_max_window = 90;                           // Seconds after creation when editing is allowed (e.g., 86400 = 24 hours)
  cosmos.base.v1beta1.Coin edit_fee = 91;                // SPARK fee for edits after grace period (e.g., 25 SPARK)
  bool   editing_enabled = 92;                           // Toggle to enable/disable post editing (default true)

  // Thread Moving (sentinels can move non-reserved tag threads, appealable)
  cosmos.base.v1beta1.Coin move_appeal_fee = 93;         // Fee to appeal a thread move (e.g., 500 SPARK)
  int64  move_appeal_deadline = 94;                      // Seconds for jury to decide move appeal (e.g., 1209600 = 14d)
  int64  move_appeal_cooldown = 95;                      // Min seconds between move and appeal (e.g., 3600 = 1h)
  uint64 max_sentinel_moves_per_epoch = 96;              // Max thread moves per sentinel per epoch (e.g., 10)

  // Post Flagging (lightweight reporting - free for members, fee for non-members)
  cosmos.base.v1beta1.Coin flag_spam_tax = 97;           // Cost for non-members to flag (e.g., 10 SPARK)
  uint64 max_flags_per_day = 98;                         // Max flags per user per 24h (e.g., 20)
  uint64 flag_review_threshold = 99;                     // Total flag weight to enter review queue (e.g., 5)
  uint64 member_flag_weight = 100;                       // Flag weight for members (e.g., 2)
  uint64 nonmember_flag_weight = 101;                    // Flag weight for non-members (e.g., 1)
  uint64 max_post_flaggers = 102;                        // Max flaggers tracked per post (e.g., 50)
  int64  flag_expiration = 103;                          // Seconds before unresolved flags expire (e.g., 604800 = 7d)

  // Thread Following (off-chain notification support)
  uint64 max_follows_per_day = 119;                       // Max follows per user per 24h (e.g., 50)

  // Bounties (author bounties for thread answers)
  cosmos.base.v1beta1.Coin min_bounty_amount = 104;      // Minimum bounty amount (e.g., 50 SPARK)
  int64  bounty_duration = 105;                          // Default bounty duration (e.g., 1209600 = 14 days)
  int64  max_bounty_duration = 106;                      // Maximum bounty duration (e.g., 2592000 = 30 days)
  string bounty_expiry_burn_ratio = 107 [
    (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec",
    (gogoproto.nullable) = false
  ];                                                     // % of expired bounty burned (e.g., "0.50" = 50%, rest to top reply)
  uint64 max_bounty_winners = 108;                       // Max winners per bounty (e.g., 5)
  bool   bounties_enabled = 109;                         // Toggle to enable/disable bounties (default true)
  string bounty_moderation_fee_ratio = 118 [
    (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec",
    (gogoproto.nullable) = false
  ];                                                     // % burned when bounty cancelled due to moderation (e.g., "0.025" = 2.5%)

  // Tag Budgets (simpler group bounties for quality content in their tag)
  cosmos.base.v1beta1.Coin min_tag_budget_amount = 110;  // Min initial pool (e.g., 100 SPARK)
  cosmos.base.v1beta1.Coin min_tag_budget_award = 111;   // Min award per post (e.g., 10 SPARK)
  uint64 max_tag_budget_awards_history = 112;            // Max award history to keep per budget (e.g., 100)

  // Pinned/Accepted Replies
  uint64 max_pinned_replies_per_thread = 120;            // Max pinned replies per thread (e.g., 3)

  // Sentinel Curation (pins and accept proposals)
  int64  accept_proposal_timeout = 121;                  // Seconds before sentinel proposal auto-confirms (e.g., 172800 = 48h)
  int64  inactivity_extension_threshold = 138;           // Seconds since last activity to trigger timeout extension (e.g., 259200 = 3d)
  string curation_dream_reward = 122;                    // DREAM reward per successful curation action (e.g., "5")
  string curation_slash_amount = 123;                    // DREAM slashed if curation overturned on appeal (e.g., "50")
  cosmos.base.v1beta1.Coin pin_dispute_fee = 124;        // Fee to dispute a sentinel pin (e.g., 100 SPARK)

  // NOTE: XP parameters are handled by x/season module via ForumHooks interface
  // x/forum emits engagement events; x/season decides XP rewards
}
```
<!-- END of design parameters section -->

### 4.7. Genesis State

> **Implementation note:** The implemented genesis state uses Ignite-standard `_map` suffixed repeated fields with auto-incrementing counters, and imports shared types from `sparkdream.common.v1.*`.

#### 4.7.1. Implemented Genesis (proto/sparkdream/forum/v1/genesis.proto)

```protobuf
message GenesisState {
  Params params = 1;

  // Core content (map-keyed collections)
  repeated Post post_map = 2;
  repeated Category category_map = 3;
  repeated sparkdream.common.v1.Tag tag_map = 4;
  repeated sparkdream.common.v1.ReservedTag reserved_tag_map = 5;

  // Rate limiting
  repeated UserRateLimit user_rate_limit_map = 6;
  repeated UserReactionLimit user_reaction_limit_map = 7;

  // Sentinel system
  repeated SentinelActivity sentinel_activity_map = 8;
  repeated HideRecord hide_record_map = 9;
  repeated ThreadLockRecord thread_lock_record_map = 10;
  repeated ThreadMoveRecord thread_move_record_map = 11;

  // Flagging
  repeated PostFlag post_flag_map = 12;

  // Bounties (counted list)
  repeated Bounty bounty_list = 13;
  uint64 bounty_count = 14;

  // Tag Budgets (counted list)
  repeated TagBudget tag_budget_list = 15;
  uint64 tag_budget_count = 16;
  repeated TagBudgetAward tag_budget_award_list = 17;
  uint64 tag_budget_award_count = 18;

  // Thread metadata & following
  repeated ThreadMetadata thread_metadata_map = 19;
  repeated ThreadFollow thread_follow_map = 20;
  repeated ThreadFollowCount thread_follow_count_map = 21;

  // Archival
  repeated ArchiveMetadata archive_metadata_map = 23;

  // Tag reporting
  repeated TagReport tag_report_map = 24;

  // Member system
  repeated MemberSalvationStatus member_salvation_status_map = 25;
  repeated JuryParticipation jury_participation_map = 26;
  repeated MemberReport member_report_map = 27;
  repeated MemberWarning member_warning_list = 28;
  uint64 member_warning_count = 29;

  // Gov Action Appeals (counted list)
  repeated GovActionAppeal gov_action_appeal_list = 30;
  uint64 gov_action_appeal_count = 31;
}
```

#### 4.7.2. Design Genesis Fields (Not Yet Implemented)

The following genesis fields from the original design are **not in the current implementation**:

| Field | Reason |
|-------|--------|
| `reward_pool` | Sentinel reward pool accumulation not yet implemented |
| `jury_pool` | Removed — jurors compensated via x/rep DREAM minting; appeal fee remainder burned |
| `total_tags` | Tracked implicitly via tag collection |
| `current_reward_epoch` | Reward epoch system not yet implemented |
| `last_gc_block` | GC uses simpler expiration queue approach |
| `epoch_dream_minted` | DREAM minting cap enforcement not yet implemented |
| `reaction_records` | Public reaction records not in genesis (tracked on-chain but not exported) |
| `anon_reactions` | Anonymous reactions not yet implemented |

---

## 5. State Keys

```
posts/{id}                    -> Post
categories/{id}               -> Category
rate_limits/{address}         -> UserRateLimit
sentinel_activity/{address}   -> SentinelActivity
archived_threads/{root_id}    -> ArchivedThread
archive_metadata/{root_id}    -> ArchiveMetadata (authoritative archive count, separate from blob)
hide_records/{post_id}        -> HideRecord (sentinel eligibility snapshot, includes committed_amount)
expiration_queue/{time}/{id}  -> PostID (index for ephemeral post pruning)
hidden_queue/{time}/{id}      -> PostID (index for hidden post expiration)
tags/{tag_name}               -> Tag (metadata + expiration info)
tag_expiration_queue/{time}/{tag_name} -> String (index for tag pruning)
reward_pool                   -> sdk.Coins
# jury_pool removed — jurors compensated via x/rep DREAM minting
total_tags                    -> uint64
next_post_id                  -> uint64
next_category_id              -> uint64

# New keys for anti-gaming protections
tag_reports/{tag_name}        -> TagReport (active reports against tag)
member_status/{address}       -> MemberSalvationStatus (salvation eligibility)
archive_cooldown/{root_id}    -> int64 (timestamp when cooldown expires)
appeal_cooldown/{post_id}     -> int64 (earliest appeal timestamp)
current_reward_epoch          -> int64
last_gc_block                 -> int64
sentinel_cooldown/{sentinel}  -> int64 (timestamp until sentinel can hide again)
sentinel_demotion_cooldown/{sentinel} -> int64 (timestamp until re-bonding allowed after demotion)

# Member reporting system
member_reports/{member}       -> MemberReport (active report, one per member)
member_warnings/{member}/{n}  -> MemberWarning (historical warnings, indexed by number)
member_warning_count/{member} -> uint64 (total warnings issued to member)
report_filed_at/{member}      -> int64 (timestamp when report was filed, for min_report_duration)
defense_submitted_at/{member} -> int64 (timestamp when defense was submitted, for min_defense_wait)

# Gov Action Appeals (meta-governance)
gov_action_appeals/{id}        -> GovActionAppeal (active Gov action appeals)
next_gov_appeal_id             -> uint64

# DREAM minting tracking
epoch_dream_minted            -> sdk.Int (total DREAM minted by x/forum in current epoch)
epoch_eligible_sentinels      -> []string (snapshotted at epoch start for fair DREAM scaling)

# Module authority (immutable after genesis)
params_authority              -> string (address that can update params, default: x/gov)

# Pinned posts index (for efficient pinned post queries)
pinned_posts/{category_id}/{priority}/{post_id} -> PostID (index for pinned posts by category)
locked_threads/{root_id}      -> int64 (timestamp when locked, for queries)

# Thread lock records (sentinel locks only - for appeal/slashing)
thread_lock_records/{root_id} -> ThreadLockRecord (sentinel lock snapshot)
lock_appeal_cooldown/{root_id} -> int64 (earliest appeal timestamp)

# Reactions (one per user per post — public tracked via ReactionRecord, private via nullifier)
# Aggregate counters (upvote_count, downvote_count) are maintained on Post for display
# Individual records enable one-per-target enforcement and are cleaned up on archival
reaction_records/{post_id}/{voter_address}          -> ReactionRecord (public reaction — one per user per post)
user_reaction_count/{address}                       -> UserReactionLimit (unified daily budget: all reaction types, public + private)

# Private (anonymous) reactions — REMOVED from x/forum state
# Nullifier tracking and anonymous reaction metadata are now managed by x/shield
# (nullifiers scoped per-domain in x/shield's centralized store)

# Thread move records (sentinel moves only - for appeal/slashing)
thread_move_records/{root_id} -> ThreadMoveRecord (sentinel move snapshot)
move_appeal_cooldown/{root_id} -> int64 (earliest appeal timestamp)

# Post flagging (lightweight reporting queue)
post_flags/{post_id}          -> PostFlag (flag state for post)
user_flag_count/{address}     -> UserReactionLimit (rate limit tracking for flags)
flag_review_queue/{post_id}   -> bool (index for posts needing sentinel review)

# Bounties (author bounties for helpful replies)
bounties/{id}                 -> Bounty (bounty state)
bounty_by_thread/{thread_id}  -> uint64 (bounty ID for thread, if exists)
next_bounty_id                -> uint64
bounty_expiration_queue/{time}/{id} -> uint64 (index for bounty expiration processing)

# Tag Budgets (group bounties for quality content - simpler than recurring bounties)
tag_budgets/{id}              -> TagBudget (budget state)
tag_budget_by_tag/{tag}       -> uint64 (budget ID for tag, if exists)
next_tag_budget_id            -> uint64
tag_budget_awards/{budget_id}/{award_id} -> TagBudgetAward (award history, capped per budget)

# Thread Metadata (pinned/accepted replies)
thread_metadata/{thread_id}   -> ThreadMetadata (pinned replies, accepted answer)

# Thread Following (for off-chain notification systems)
thread_followers/{thread_id}/{follower} -> ThreadFollow (follow record)
thread_follow_count/{thread_id} -> ThreadFollowCount (aggregate follower count)
user_followed_threads/{follower}/{thread_id} -> bool (reverse index for user's followed threads)

# Sentinel Curation (pins and accept proposals)
sentinel_pin_records/{thread_id}/{reply_id} -> SentinelPinRecord (for appeal tracking)
proposal_auto_confirm_queue/{time}/{thread_id} -> uint64 (index for auto-confirm processing)

# Moderation History
# NOTE: On-chain moderation history storage is not implemented.
# Moderation actions emit events that can be indexed off-chain for history/analytics.

# NOTE: XP state is managed by x/season module via ForumHooks interface
# x/forum emits engagement events; x/season tracks XP and anti-gaming state

# Jury participation tracking (anti-timeout-abuse)
jury_assigned/{juror}/{initiative_id}    -> bool (juror was assigned to this appeal)
jury_voted/{juror}/{initiative_id}       -> bool (juror voted on this appeal)
jury_participation_rate/{juror}          -> JuryParticipation (participation stats for exclusion logic)
```

---

## 6. Messages & State Transitions

### 6.1. Content Management

#### `MsgCreateCategory`

Creates a new permanent Category.

| Field | Type | Proto # | Description |
|-------|------|---------|-------------|
| `creator` | `string` | 1 | Must match `x/commons` HR Committee Address (signer) |
| `title` | `string` | 2 | Category title |
| `description` | `string` | 3 | Category description |
| `members_only_write` | `bool` | 4 | Restrict posting to members |
| `admin_only_write` | `bool` | 5 | Restrict posting to HR Committee |

> **Note:** The design-only `allow_anonymous` field (Section 16.3) is not in the current proto.

**Authorization:** Signer must be authorized via `CommonsKeeper.IsCouncilAuthorized`.
**Fees:** Standard Gas Fee.

---

#### `MsgCreatePost`

Creates a new post or reply.

| Field | Type | Proto # | Description |
|-------|------|---------|-------------|
| `creator` | `string` | 1 | Post author (signer) |
| `category_id` | `uint64` | 2 | Target category |
| `parent_id` | `uint64` | 3 | Parent post ID (0 if root) |
| `content` | `string` | 4 | Post text content |
| `tags` | `[]string` | 5 | Associated tags |
| `content_type` | `ContentType` | 6 | Content type enum from `sparkdream.common.v1` |
| `author_bond` | `math.Int` | 7 | Optional DREAM amount to lock as author bond (via x/rep `CreateAuthorBond`) |
| `initiative_id` | `uint64` | 8 | Optional x/rep initiative ID for conviction propagation (0 = none, immutable after creation) |

**Fees:**
- **Gas Fee:** Paid by all users (Standard Cosmos SDK Execution Cost).
- **Spam Tax:** Paid by Non-Members (Protocol Fixed Fee).
- **Cost Per Byte:** Charged for on-chain content storage (burned), unless `cost_per_byte_exempt` is true.

**State Transition Logic:**

0. **Emergency Pause Check**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Fail with `ErrNewPostsPaused` if `params.new_posts_paused == true`

0.5. **Thread Lock Check & Depth Check** (for replies only)
   - If `parent_id != 0` (this is a reply):
     - Load parent post by `parent_id`
     - Fail with `ErrPostNotFound` if parent does not exist
     - Determine `root_id`: if `parent.parent_id == 0`, then `root_id = parent_id`; otherwise `root_id = parent.root_id`
     - Load root post by `root_id`
     - Fail with `ErrThreadLocked` if `root_post.locked == true`
     - **Depth Check:**
       - Calculate `new_depth = parent.depth + 1`
       - Fail with `ErrMaxReplyDepthExceeded` if `new_depth > params.max_reply_depth`

1. **Content Validation**
   - Fail with `ErrContentTooLarge` if `len(content) > max_content_size`

2. **Epoch-Based Rate Limit Check** (Fixed Memory - No Unbounded Growth)
   - Load `UserRateLimit` for author (or create new)
   - **Epoch Rotation:** If `now - current_epoch_start >= 86400` (24h):
     - Set `previous_epoch_count = current_epoch_count`
     - Set `current_epoch_count = 0`
     - Set `current_epoch_start = now`
   - **Calculate Effective Count** (sliding window approximation):
     - `overlap_ratio = max(0, 1 - (now - current_epoch_start) / 86400)`
     - `effective_count = current_epoch_count + (previous_epoch_count * overlap_ratio)`
   - Fail with `ErrRateLimitExceeded` if `effective_count >= daily_post_limit`
   - Increment `current_epoch_count`
   - Set `last_post_time = now`

3. **Tag Validation & Fee Charging**
   - Fail with `ErrTagLimitExceeded` if `len(tags) > max_tags_per_post`
   - For each tag:
     - Fail with `ErrInvalidTag` if `len(tag) > max_tag_length`
     - Fail with `ErrInvalidTag` if tag contains non-alphanumeric characters (except hyphen)
     - **Check Existence:**
       - If tag exists (in `tags/{tag_name}`):
         - **Lazy Expiration Refresh** (reduces queue churn):
           - Only update if `now - tag.last_used_at > 86400` (1 day since last use)
           - If updating:
             - Set `tag.last_used_at = now`
             - Calculate new expiration: `new_expiry = now + tag_expiration`
             - Set `tag.expiration_index = new_expiry` (authoritative queue entry)
             - Add to `tag_expiration_queue` at `{new_expiry}/{tag_name}`
             - *Note: Old queue entry becomes stale, will be skipped by GC via expiration_index check*
             - **Same-Block Race Handling:** If multiple txs in same block update the same tag,
               last-writer-wins for the Tag object. All other queue entries become stale
               (expiration_index won't match) and are safely skipped by GC.
           - If NOT updating: Skip queue operation (tag is "fresh enough")
       - If tag is **new**:
         - Fail with `ErrTagLimitExceeded` if `total_tags >= max_total_tags`
         - Fail with `ErrUnauthorized` if tag in `reserved_tags` and author not authorized (see Reserved Tag Authorization below)
         - Fail with `ErrInsufficientReputation` if author's Rep Tier < `min_rep_tier_tags`
         - **Charge Fee:** Deduct `tag_creation_fee` from author (50% Burn, 50% Reward Pool)
         - Calculate expiration: `expiry = now + tag_expiration`
         - Store `Tag` object with `created_at = now`, `last_used_at = now`, `expiration_index = expiry`
         - Add to `tag_expiration_queue` at `{expiry}/{tag_name}`
         - Increment `total_tags`

4. **Membership Check & Spam Tax** (Query `x/commons`)
   - **Member:** `status = ACTIVE`, `expiration_time = 0`
   - **Non-Member:** `status = ACTIVE`, `expiration_time = now + ephemeral_ttl`
     - Charge `spam_tax` (50% Burn, 50% Reward Pool)
     - *Note: This is additive to any tag creation fees and gas.*

4.5. **Initiative Reference Validation** (Cross-Module Conviction Propagation)
   - If `initiative_id > 0` and `repKeeper` is not nil:
     - Call `repKeeper.ValidateInitiativeReference(ctx, initiative_id)`
     - Fail with `ErrInvalidInitiativeRef` if initiative does not exist or is in a terminal status (COMPLETED, FAILED, CANCELLED)
     - *Note: `initiative_id` is immutable after creation — cannot be changed via `MsgEditPost`*
   - If `initiative_id > 0` and `repKeeper` is nil:
     - Fail with `ErrInvalidInitiativeRef` (x/rep not available)

5. **Depth-Limited Recursive Salvation (Anti-Spam Protected + Rate Limited)**
   - If `parent_id != 0`, load parent post
   - If parent was Ephemeral (Member replying to Non-Member):
     - **Check salvation eligibility:**
       - Load `MemberSalvationStatus` for author
       - If `member_since + min_membership_for_salvation > now`:
         - **Skip salvation** (new member cannot save posts)
         - Emit `EventSalvationDenied{Reason: "membership_too_new"}`
         - Continue with post creation (reply still created, parent stays ephemeral)
     - **Check salvation rate limit (counts TOTAL POSTS, not operations):**
       - If `now - epoch_start >= 86400`: reset `epoch_salvations = 0`, `epoch_start = now`
       - If `epoch_salvations >= max_salvations_per_day`:
         - **Skip salvation** (rate limit exceeded)
         - Emit `EventSalvationDenied{Reason: "rate_limit_exceeded"}`
         - Continue with post creation (reply still created, parent stays ephemeral)
       - **Calculate remaining budget:** `remaining_salvations = max_salvations_per_day - epoch_salvations`
       - **Effective depth limit:** `effective_depth = min(max_salvation_depth, remaining_salvations)`
     - Initialize `depth = 0`, `salvaged_count = 0`
     - While parent exists and `depth < effective_depth`:
       - **Check thread lock status:** Find root post of parent's thread
         - If thread is locked (`locked_threads/{root_id}` exists):
           - **Skip salvation for this ancestor** (don't make permanent in locked thread)
           - Emit `EventSalvationDenied{PostID: parent.id, Reason: "thread_locked"}`
           - Break loop (stop salvaging ancestors beyond locked point)
       - Remove parent from Expiration Queue
       - Set parent to Permanent (`expiration_time = 0`)
       - Increment `salvaged_count`
       - Load parent's parent, increment `depth`
     - Update `member_status.epoch_salvations += salvaged_count`
     - *Note: If `effective_depth < max_salvation_depth`, some ancestors may remain ephemeral due to rate limit, not depth limit*
     - If `depth == max_salvation_depth` and more ancestors exist:
       - Emit `EventPartialSalvation` (ancestors beyond depth remain ephemeral)

6. **Lazy Garbage Collection** (Supplementary - see also EndBlocker GC)
   - **Ephemeral Posts:** Iterate `expiration_queue` (max `lazy_prune_limit` items), delete expired.
   - **Hidden Posts:** Iterate `hidden_queue` (max `lazy_prune_limit` items), delete expired, update sentinel metrics.
   - **Expired Tags:** Iterate `tag_expiration_queue` (max `lazy_prune_limit` items).
     - For each entry in queue at `{time}/{tag_name}`:
       - Load tag from `tags/{tag_name}`
       - **Race Condition Check:** Compare `tag.expiration_index` with queue entry timestamp
         - If `tag.expiration_index != queue_entry_time`: skip (tag was refreshed, stale queue entry)
         - If `tag.expiration_index == queue_entry_time`: tag is truly expired
       - Delete `tags/{tag_name}`
       - Decrement `total_tags`
       - Emit `EventTagExpired`
       - *Note: The `expiration_index` field in Tag tracks which queue entry is authoritative, preventing race conditions when tags are refreshed between queue entry creation and GC execution.*

7. **Initiative Link Registration** (Cross-Module Conviction Propagation)
   - If `initiative_id > 0` and `repKeeper` is not nil:
     - Call `repKeeper.RegisterContentInitiativeLink(ctx, initiative_id, STAKE_TARGET_FORUM_CONTENT, post_id)`
     - This registers the post as linked content for the initiative. When the post accumulates community conviction stakes, a fraction (`conviction_propagation_ratio`, default 10%) of the content's conviction score propagates back to boost the initiative's conviction score.
     - *Note: Link removal occurs automatically when the post is tombstoned (EndBlocker TTL expiry) or soft-deleted.*

**Note:** Lazy GC runs opportunistically during `MsgCreatePost`. To prevent state bloat during low-activity periods, the same GC logic also runs in `EndBlocker` (see Section 7.2).

**Reserved Tag Authorization:**

When a post uses a tag that exists in `reserved_tags`, the following authorization check applies:

```go
func IsAuthorizedForReservedTag(ctx sdk.Context, author string, reservedTag ReservedTag) bool {
    // HR Committee can always use any reserved tag
    if k.commonsKeeper.IsHRCommittee(ctx, author) {
        return true
    }

    // If no specific authority set, only HR Committee can use it
    if reservedTag.Authority == "" {
        return false
    }

    // Check if author IS the authority (e.g., group account posting via proposal)
    if author == reservedTag.Authority {
        return true
    }

    // If members_can_use is enabled, check group membership via x/commons
    if reservedTag.MembersCanUse {
        if k.commonsKeeper.IsGroupMember(ctx, reservedTag.Authority, author) {
            return true
        }
    }

    return false
}
```

| Tag Configuration | Who Can Use |
|-------------------|-------------|
| `authority=""`, `members_can_use=false` | HR Committee only |
| `authority="council_addr"`, `members_can_use=false` | Council group account + HR Committee |
| `authority="council_addr"`, `members_can_use=true` | Council members + council group + HR Committee |

---

#### `MsgFreezeThread`

Archives an inactive thread to reduce state size.

| Field | Type | Description |
|-------|------|-------------|
| `sender` | `string` | Any user (permissionless) |
| `root_id` | `uint64` | Thread root post ID |

**Fees:** Standard Gas Fee.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load root post by `root_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrThreadNotInactive` if `root_post.last_reply >= now - archive_threshold`
   - **Check cooldown:** If `archive_cooldown/{root_id}` exists and `value > now`:
     - Fail with `ErrArchiveCooldown` (thread was recently unarchived)
   - **Check archive cycle limit (from separate ArchiveMetadata, not Post):**
     - Load `archive_metadata/{root_id}` if exists (or default to archive_count=0)
     - If `metadata.archive_count >= max_archive_cycles`:
       - Fail with `ErrArchiveCycleLimit` (requires HR Committee approval to re-archive)
   - **Check for pending appeals (prevent archiving disputed threads):**
     - If `ThreadLockRecord` exists for `root_id` AND `record.appeal_pending == true`:
       - Fail with `ErrCannotArchiveThreadWithPendingAppeal` (lock appeal must resolve first)
     - If `ThreadMoveRecord` exists for `root_id` AND `record.appeal_pending == true`:
       - Fail with `ErrCannotArchiveThreadWithPendingAppeal` (move appeal must resolve first)
   - Count total posts in thread: `total_post_count = 1 (root) + descendant_count`
   - Fail with `ErrThreadTooLarge` if `total_post_count > max_archive_post_count`

2. **Action:**
   - **Update ArchiveMetadata (separate from compressed blob):**
     - Load or create `archive_metadata/{root_id}`
     - `metadata.archive_count += 1`
     - `metadata.last_archived_at = now`
     - If `metadata.first_archived_at == 0`: `metadata.first_archived_at = now`
     - If `metadata.archive_count >= max_archive_cycles`: `metadata.hr_override_required = true`
     - Store `archive_metadata/{root_id}`
   - Serialize root post and all descendant posts
   - Gzip compress
   - Fail with `ErrThreadTooLarge` if `len(compressed) > max_archive_size_bytes`
   - Store as `ArchivedThread` with `archived_at = now`
   - Delete individual `Post` keys (including root)
   - **Delete cooldown key** (consumed)
   - Emit `EventThreadArchived`

**Security Note:** The `archive_count` is stored in separate `ArchiveMetadata` storage, NOT in the Post object or compressed blob. This prevents manipulation via:
- Corrupted archive blobs that could reset the count
- Tampering with serialized data before compression
- Desync between Post.archive_count and ArchivedThread.archive_count
The ArchiveMetadata is the authoritative source and persists independently of the archive/unarchive cycle.

---

#### `MsgUnarchiveThread`

Restores an archived thread to active state. Gas cost proportional to thread size.

| Field | Type | Description |
|-------|------|-------------|
| `sender` | `string` | Any user (permissionless, pays gas) |
| `root_id` | `uint64` | Archived thread root ID |

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load `ArchivedThread` by `root_id`
   - Fail with `ErrArchivedThreadNotFound` if not found
   - **Check unarchive cooldown:** If `archived_at + unarchive_cooldown > now`:
     - Fail with `ErrUnarchiveCooldown` (must wait before unarchiving)

2. **Action:**
   - Decompress `compressed_data`
   - Deserialize all posts
   - Store each post individually in `posts/{id}`
   - **Set archive cooldown:** Store `archive_cooldown/{root_id} = now + archive_cooldown`
   - Update `ArchivedThread.last_unarchived_at = now` (for metrics)
   - Delete `ArchivedThread` record
   - Emit `EventThreadUnarchived`

**Note:** Gas metering ensures large thread decompression has proportional cost, preventing decompression bomb attacks.

**Anti-Griefing:** The `archive_cooldown` prevents immediate re-archiving after unarchive. The `unarchive_cooldown` prevents rapid archive/unarchive cycles. Combined, these create a minimum 31-day cycle time (1d unarchive wait + 30d inactivity + cooldown), making griefing economically infeasible.

---

#### `MsgPinPost`

Pins a thread to the top of its category. Only HR Committee can pin posts.

| Field | Type | Description |
|-------|------|-------------|
| `authority` | `string` | Must match `x/commons` HR Committee Address |
| `post_id` | `uint64` | Root post ID to pin |
| `priority` | `uint32` | Sort order among pinned posts (lower = higher priority) |

**Authorization:** Signer must match `x/commons` HR Committee Address.
**Fees:** Standard Gas Fee.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `post_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrNotRootPost` if `post.parent_id != 0` (only root posts can be pinned)
   - Fail with `ErrPostAlreadyPinned` if `post.pinned == true`
   - Fail with `ErrMaxPinnedPosts` if category already has `max_pinned_per_category` pinned posts

2. **Action:**
   - Set `post.pinned = true`
   - Set `post.pinned_by = authority`
   - Set `post.pinned_at = now`
   - Set `post.pin_priority = priority`
   - Add to `pinned_posts/{category_id}/{priority}/{post_id}` index
   - Emit `EventPostPinned`

---

#### `MsgUnpinPost`

Removes a post from pinned status.

| Field | Type | Description |
|-------|------|-------------|
| `authority` | `string` | Must match `x/commons` HR Committee Address |
| `post_id` | `uint64` | Root post ID to unpin |

**Authorization:** Signer must match `x/commons` HR Committee Address.
**Fees:** Standard Gas Fee.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `post_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrPostNotPinned` if `post.pinned == false`

2. **Action:**
   - Remove from `pinned_posts/{category_id}/{post.pin_priority}/{post_id}` index
   - Set `post.pinned = false`
   - Set `post.pinned_by = ""`
   - Set `post.pinned_at = 0`
   - Set `post.pin_priority = 0`
   - Emit `EventPostUnpinned`

---

#### `MsgLockThread`

Locks a thread, preventing any new replies to any post within the thread. Can be executed by HR Committee (always) or high-trust Sentinels (with restrictions).

| Field | Type | Description |
|-------|------|-------------|
| `sender` | `string` | HR Committee Address OR high-trust Sentinel |
| `root_id` | `uint64` | Root post ID of thread to lock |
| `reason` | `string` | Reason for locking (required for Sentinels, optional for HR Committee) |

**Authorization:**
- **HR Committee:** Always authorized (no additional checks)
- **Sentinel:** Must satisfy ALL of the following:
  - `x/rep` Tier >= `min_rep_tier_thread_lock` (Tier 4)
  - Self-bonded DREAM >= `min_sentinel_lock_bond` (2x normal sentinel bond)
  - Delegated backing from others >= `min_sentinel_lock_backing` (2x normal backing)
  - `sentinel.epoch_locks < max_sentinel_locks_per_epoch`
  - Not in overturn cooldown (`sentinel.overturn_cooldown_until <= now`)
  - `reason` field must be non-empty

**Fees:** Standard Gas Fee.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Fail with `ErrModerationPaused` if `params.moderation_paused == true` (Sentinel only)
   - Load post by `root_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrNotRootPost` if `post.parent_id != 0` (only threads can be locked)
   - Fail with `ErrThreadAlreadyLocked` if `post.locked == true`
   - **If Sentinel (not HR Committee):**
     - Fail with `ErrInsufficientReputation` if tier < `min_rep_tier_thread_lock`
     - Fail with `ErrInsufficientLockBond` if bond < `min_sentinel_lock_bond`
     - Fail with `ErrInsufficientLockBacking` if backing < `min_sentinel_lock_backing`
     - Fail with `ErrLockLimitExceeded` if `sentinel.epoch_locks >= max_sentinel_locks_per_epoch`
     - Fail with `ErrSentinelCooldown` if `sentinel.overturn_cooldown_until > now`
     - Fail with `ErrLockReasonRequired` if `reason` is empty

2. **Action:**
   - Set `post.locked = true`
   - Set `post.locked_by = sender`
   - Set `post.locked_at = now`
   - Set `post.lock_reason = reason`
   - Add to `locked_threads/{root_id}` index with timestamp
   - **If Sentinel:** Create `ThreadLockRecord` (for appeal/slashing)
   - **If Sentinel:** Increment `sentinel.epoch_locks`
   - Emit `EventThreadLocked`

**Note:** When a thread is locked, `MsgCreatePost` must check if the target thread (via `root_id`) is locked and fail with `ErrThreadLocked` if so.

**Sentinel Lock Records:** Similar to `HideRecord`, a `ThreadLockRecord` snapshots the sentinel's bond at lock time to ensure slashing can occur if the lock is overturned on appeal.

---

#### `MsgUnlockThread`

Unlocks a previously locked thread, allowing new replies. HR Committee can unlock any thread. Sentinels can only unlock threads they locked (before appeal period expires).

| Field | Type | Description |
|-------|------|-------------|
| `sender` | `string` | HR Committee Address OR the Sentinel who locked the thread |
| `root_id` | `uint64` | Root post ID of thread to unlock |

**Authorization:**
- **HR Committee:** Can unlock any thread
- **Sentinel:** Can only unlock threads they locked, and only if:
  - No appeal is pending
  - Lock appeal period has not expired (`locked_at + lock_appeal_deadline > now`)

**Fees:** Standard Gas Fee.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `root_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrThreadNotLocked` if `post.locked == false`
   - **If Sentinel (not HR Committee):**
     - Fail with `ErrUnauthorized` if `post.locked_by != sender`
     - Fail with `ErrAppealPending` if appeal exists for this lock
     - Fail with `ErrLockAppealExpired` if `post.locked_at + lock_appeal_deadline <= now`

2. **Action (ATOMIC - prevents race with MsgAppealThreadLock):**
   - **FIRST:** Delete `ThreadLockRecord` if Sentinel lock (acts as lock acquisition)
   - *If another tx filed appeal between validation and this point, the appeal would have created state that prevents unlock. The ThreadLockRecord deletion serves as atomic coordination point.*
   - Set `post.locked = false`
   - Set `post.locked_by = ""`
   - Set `post.locked_at = 0`
   - Set `post.lock_reason = ""`
   - Remove from `locked_threads/{root_id}` index
   - Emit `EventThreadUnlocked`

**Atomicity Note:** In same-block scenarios, transaction ordering by the Cosmos SDK ensures either the appeal is processed first (blocking unlock) or the unlock is processed first (no ThreadLockRecord for appeal to reference). Both are valid outcomes.

---

#### `MsgAppealThreadLock`

Thread author appeals a sentinel-initiated lock to jury. Only applies to locks made by Sentinels (HR Committee locks must be appealed via `MsgAppealGovAction` with `GOV_ACTION_TYPE_THREAD_LOCK`).

| Field | Type | Description |
|-------|------|-------------|
| `appellant` | `string` | Thread author (signer) |
| `root_id` | `uint64` | Locked thread root ID |

**Cost:** Deposit `lock_appeal_fee` (SPARK) + Standard Gas Fee.

**Authorization:** Appellant must be the original thread author (`post.author`).

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Fail with `ErrAppealsPaused` if `params.appeals_paused == true`
   - Load post by `root_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrThreadNotLocked` if `post.locked == false`
   - Fail with `ErrNotThreadAuthor` if `appellant != post.author`
   - Load `ThreadLockRecord` for `root_id`
   - Fail with `ErrHRLockNotAppealable` if no `ThreadLockRecord` exists (HR Committee lock - use `MsgAppealGovAction` instead)
   - Fail with `ErrLockAppealAlreadyFiled` if appeal already exists for this lock
   - **Check appeal cooldown:** If `lock_appeal_cooldown/{root_id}` exists and `value > now`:
     - Fail with `ErrAppealCooldown` (must wait before appealing)

2. **Escrow Appeal Fee:**
   - Transfer `lock_appeal_fee` from appellant to module account

3. **Create Initiative in x/rep:**
   - Type: `THREAD_LOCK_APPEAL` (Fast-Track Jury)
   - Payload: `{ root_id, sentinel_addr, appellant_addr, lock_reason }`
   - Deadline: `now + lock_appeal_deadline`
   - If `x/rep.CreateInitiative` fails, transaction reverts (escrow returned automatically)

4. **Emit Event:**
   - Emit `EventThreadLockAppealFiled`

**Verdict Handling (via x/rep hook):**

- **VERDICT_APPROVED (Appellant wins - lock was wrong):**
  - Unlock thread (set `post.locked = false`, clear lock fields)
  - Refund 80% of appeal fee to appellant, 20% burned
  - Slash sentinel's DREAM bond (same as hide overturn: `sentinel_slash_amount`)
  - Increment `sentinel.overturned_locks`
  - Apply escalating cooldown (same formula as hide overturns)
  - Delete `ThreadLockRecord`
  - Emit `EventThreadLockOverturned`

- **VERDICT_REJECTED (Sentinel wins - lock was correct):**
  - Thread remains locked
  - Appellant forfeits appeal fee (sentinel gets 50%, 50% burned)
  - Increment `sentinel.upheld_locks`
  - Delete `ThreadLockRecord` (appeal resolved)
  - Emit `EventThreadLockUpheld`

- **VERDICT_TIMEOUT (Jury didn't reach verdict):**
  - Unlock thread (benefit of doubt to author)
  - Refund 50% of appeal fee to appellant, 50% burned
  - No sentinel penalty (not their fault jury timed out)
  - Delete `ThreadLockRecord`
  - Emit `EventThreadLockAppealTimedOut`

---

#### `MsgMoveThread`

Moves a thread to a different category. HR Committee can move any thread. Sentinels can move threads without reserved tags (appealable).

| Field | Type | Description |
|-------|------|-------------|
| `sender` | `string` | HR Committee Address OR Sentinel |
| `root_id` | `uint64` | Root post ID of thread to move |
| `new_category_id` | `uint64` | Destination category |
| `reason` | `string` | Reason for moving (required for Sentinels) |

**Authorization:**
- **HR Committee:** Can move any thread to any category
- **Sentinel:** Must satisfy ALL of the following:
  - Standard sentinel requirements (tier, bond, backing)
  - Thread must NOT have any reserved tags
  - `sentinel.epoch_moves < max_sentinel_moves_per_epoch`
  - Not in overturn cooldown
  - `reason` field must be non-empty

**Fees:** Standard Gas Fee.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `root_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrNotRootPost` if `post.parent_id != 0`
   - Fail with `ErrCategoryNotFound` if `new_category_id` is invalid
   - Fail with `ErrSameCategory` if `post.category_id == new_category_id`
   - Fail with `ErrCannotMovePinnedThread` if `post.pinned == true` (must unpin first)
   - Fail with `ErrCannotMoveLockedThread` if `post.locked == true` and move appeal pending
   - **If Sentinel (not HR Committee):**
     - Check standard sentinel authorization (tier, bond, backing, cooldown)
     - For each tag in `post.tags`:
       - Fail with `ErrCannotMoveReservedTagThread` if tag exists in `reserved_tags`
     - Fail with `ErrMoveLimitExceeded` if `sentinel.epoch_moves >= max_sentinel_moves_per_epoch`
     - Fail with `ErrMoveReasonRequired` if `reason` is empty

2. **Action:**
   - Store `original_category_id = post.category_id`
   - Set `post.category_id = new_category_id`
   - **If Sentinel:** Create `ThreadMoveRecord` (for appeal/slashing)
   - **If Sentinel:** Increment sentinel's epoch move count
   - **Set move appeal cooldown:** Store `move_appeal_cooldown/{root_id} = now + move_appeal_cooldown`
   - Emit `EventThreadMoved`

**Note:** Reserved tag check ensures sentinels cannot move official announcements or committee posts - only HR Committee can relocate those.

---

#### `MsgAppealThreadMove`

Thread author appeals a sentinel-initiated move to jury. Only applies to moves made by Sentinels (HR Committee moves must be appealed via `MsgAppealGovAction` with `GOV_ACTION_TYPE_THREAD_MOVE`).

| Field | Type | Description |
|-------|------|-------------|
| `appellant` | `string` | Thread author (signer) |
| `root_id` | `uint64` | Moved thread root ID |

**Cost:** Deposit `move_appeal_fee` (SPARK) + Standard Gas Fee.

**Authorization:** Appellant must be the original thread author.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Fail with `ErrAppealsPaused` if `params.appeals_paused == true`
   - Load post by `root_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrNotThreadAuthor` if `appellant != post.author`
   - Load `ThreadMoveRecord` for `root_id`
   - Fail with `ErrHRMoveNotAppealable` if no `ThreadMoveRecord` exists (HR Committee move - use `MsgAppealGovAction` instead)
   - Fail with `ErrMoveAppealAlreadyFiled` if appeal already exists
   - **Check appeal cooldown:** Fail with `ErrAppealCooldown` if cooldown not expired

2. **Escrow Appeal Fee:**
   - Transfer `move_appeal_fee` from appellant to module account

3. **Create Initiative in x/rep:**
   - Type: `THREAD_MOVE_APPEAL` (Fast-Track Jury)
   - Payload: `{ root_id, sentinel_addr, appellant_addr, original_category_id, new_category_id, move_reason }`
   - Deadline: `now + move_appeal_deadline`

4. **Update State:**
   - Set `ThreadMoveRecord.appeal_pending = true`
   - Set `ThreadMoveRecord.initiative_id = initiative_id`
   - Delete `move_appeal_cooldown/{root_id}`
   - Emit `EventThreadMoveAppealFiled`

**Verdict Handling (via x/rep hook):**

- **VERDICT_APPROVED (Appellant wins - move was wrong):**
  - Restore thread to original category (`ThreadMoveRecord.original_category_id`)
  - Refund 80% of appeal fee to appellant, 20% burned
  - Slash sentinel's DREAM bond (`sentinel_slash_amount`)
  - Increment `sentinel.overturned_moves` (counts toward accuracy)
  - Apply escalating cooldown
  - Delete `ThreadMoveRecord`
  - Emit `EventThreadMoveOverturned`

- **VERDICT_REJECTED (Sentinel wins - move was correct):**
  - Thread stays in new category
  - Appellant forfeits appeal fee (sentinel 50%, 50% burned)
  - Increment `sentinel.upheld_moves` (counts toward accuracy)
  - Delete `ThreadMoveRecord`
  - Emit `EventThreadMoveUpheld`

- **VERDICT_TIMEOUT (Jury didn't reach verdict):**
  - **Thread restored to original category** (favors appellant - consistent with lock appeal timeout)
  - *Rationale: On jury failure, revert to original state. Appellant shouldn't suffer for jury non-participation.*
  - Refund 50% of appeal fee to appellant, 50% burned
  - No sentinel penalty (jury failure, not sentinel fault)
  - Delete `ThreadMoveRecord`
  - Emit `EventThreadMoveAppealTimedOut{Outcome: "restored_timeout"}`

---

#### `MsgFollowThread`

Follow a thread to receive notifications (via off-chain indexers).

| Field | Type | Description |
|-------|------|-------------|
| `follower` | `string` | User address (signer) |
| `thread_id` | `uint64` | Thread root post ID to follow |

**Authorization:** Any user (members and non-members).

**Fees:** Standard Gas Fee only.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load thread root by `thread_id`
   - Fail with `ErrThreadNotFound` if thread does not exist
   - Fail with `ErrAlreadyFollowing` if `thread_followers/{thread_id}/{follower}` exists

2. **Rate Limit Check:**
   - Load user's follow count for current epoch
   - Fail with `ErrFollowLimitExceeded` if `follow_count >= max_follows_per_day` (default 50)

3. **Create Follow:**
   - Store `ThreadFollow` at `thread_followers/{thread_id}/{follower}`
   - Store reverse index at `user_followed_threads/{follower}/{thread_id} = true`
   - Increment `thread_follow_count/{thread_id}.follower_count`
   - Increment user's epoch follow count
   - Emit `EventThreadFollowed`

**Note:** Following is purely for off-chain use. Indexers can query follow state and send notifications when threads receive new replies.

---

#### `MsgUnfollowThread`

Stop following a thread.

| Field | Type | Description |
|-------|------|-------------|
| `follower` | `string` | User address (signer) |
| `thread_id` | `uint64` | Thread root post ID to unfollow |

**Authorization:** Original follower only.

**Fees:** Standard Gas Fee only.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Fail with `ErrNotFollowing` if `thread_followers/{thread_id}/{follower}` does not exist

2. **Remove Follow:**
   - Delete `thread_followers/{thread_id}/{follower}`
   - Delete `user_followed_threads/{follower}/{thread_id}`
   - Decrement `thread_follow_count/{thread_id}.follower_count`
   - Emit `EventThreadUnfollowed`

**Note:** Unfollowing does not restore rate limit quota (follows are rate-limited to prevent spam).

---

#### `MsgFlagPost`

Flag a post for sentinel review. Free for members, SPARK fee for non-members.

| Field | Type | Description |
|-------|------|-------------|
| `flagger` | `string` | User address (signer) |
| `post_id` | `uint64` | Post to flag |
| `category` | `FlagCategory` | Structured flag category (see enum) |
| `reason` | `string` | Custom reason (required if category=OTHER, optional otherwise, max 256 chars) |

**Flag Categories:**
| Category | Use Case |
|----------|----------|
| `SPAM` | Unsolicited commercial content, repetitive posts, bot activity |
| `HARASSMENT` | Personal attacks, bullying, threats, targeted abuse |
| `MISINFORMATION` | False claims presented as fact, misleading content |
| `OFF_TOPIC` | Content unrelated to category or thread discussion |
| `LOW_QUALITY` | Low-effort posts, unclear or unintelligible content |
| `INAPPROPRIATE` | Adult content, graphic material, NSFW |
| `IMPERSONATION` | Pretending to be another member or public figure |
| `POLICY_VIOLATION` | Violates community guidelines not covered above |
| `OTHER` | Requires custom reason field for explanation |

**Authorization:** Any user. Non-members pay `flag_spam_tax`.

**Fees:**
- **Gas Fee:** Paid by all users
- **Flag Tax:** Paid by non-members only (`flag_spam_tax`)

**State Transitions:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `post_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrCannotFlagHiddenPost` if `post.status == HIDDEN`
   - Fail with `ErrCannotFlagOwnPost` if `flagger == post.author`
   - Fail with `ErrInvalidFlagCategory` if `category == UNSPECIFIED`
   - Fail with `ErrFlagReasonRequired` if `category == OTHER` and `reason` is empty
   - Fail with `ErrFlagReasonTooLong` if `len(reason) > 256`
   - Load `PostFlag` for post (or create new)
   - Fail with `ErrAlreadyFlagged` if flagger already in `flaggers` list
   - Fail with `ErrMaxFlaggers` if `len(flaggers) >= max_post_flaggers`

2. **Check Membership & Charge Tax:**
   - Query `x/commons` for membership status of `flagger`
   - If NOT a member: Transfer `flag_spam_tax` from flagger to module account (burned)
   - Determine flag weight: `member_flag_weight` if member, `nonmember_flag_weight` if not

3. **Rate Limit Check:**
   - Load or create `UserReactionLimit` for flagger (flag tracking)
   - Calculate effective flag count using sliding window
   - Fail with `ErrFlagLimitExceeded` if `effective_count >= max_flags_per_day`

4. **Record Flag:**
   - Add flagger to `PostFlag.flaggers` list
   - Add weight to `PostFlag.total_weight`
   - Create `FlagRecord` with `{ flagger, category, reason, flagged_at: now, weight }`
   - Append to `PostFlag.flag_records`
   - Increment `PostFlag.category_counts[category]`
   - Update timestamps (`first_flag_at` if new, `last_flag_at` always)
   - Increment user's flag count
   - **Check review threshold:**
     - If `total_weight >= flag_review_threshold` and NOT already `in_review_queue`:
       - Set `PostFlag.in_review_queue = true`
       - Add to `flag_review_queue/{post_id}` index
       - Emit `EventPostEnteredReviewQueue`
   - Emit `EventPostFlagged`

---

#### `MsgDismissFlags`

Sentinel dismisses flags on a post after review (post is fine, flags were incorrect).

| Field | Type | Description |
|-------|------|-------------|
| `sentinel` | `string` | Sentinel address (signer) |
| `post_id` | `uint64` | Flagged post ID |
| `reason` | `string` | Why flags were dismissed (optional) |

**Authorization:** Standard sentinel requirements.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Fail with `ErrModerationPaused` if `params.moderation_paused == true`
   - Check standard sentinel authorization
   - Load `PostFlag` for `post_id`
   - Fail with `ErrNoFlagsToReview` if no flags exist

2. **Action:**
   - Delete `PostFlag` for post
   - Remove from `flag_review_queue/{post_id}` if present
   - Emit `EventFlagsDismissed`

**Note:** If sentinel agrees with flags, they use `MsgHidePost` instead, which also clears the flag state.

---

#### `MsgCreateBounty`

Attach a SPARK bounty to a thread to reward helpful replies.

| Field | Type | Description |
|-------|------|-------------|
| `creator` | `string` | Thread author (signer) |
| `thread_id` | `uint64` | Root post ID to attach bounty to |
| `amount` | `Coin` | SPARK amount for bounty |
| `duration` | `int64` | Optional custom duration in seconds (0 = default) |

**Authorization:** Must be the thread author (root post creator). Must be a member.

**Fees:** Standard Gas Fee + bounty amount escrowed.

**Logic:**
1. **Validation:**
   - Fail with `ErrBountiesDisabled` if `params.bounties_enabled == false`
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `thread_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrNotRootPost` if `post.parent_id != 0`
   - Fail with `ErrNotPostAuthor` if `creator != post.author`
   - Fail with `ErrBountyAlreadyExists` if `bounty_by_thread/{thread_id}` exists
   - Fail with `ErrBountyTooSmall` if `amount < min_bounty_amount`
   - Fail with `ErrNotMember` if creator is not a member

2. **Calculate Duration:**
   - If `duration == 0`: use `params.bounty_duration`
   - Else: use provided duration, capped at `params.max_bounty_duration`

3. **Escrow Bounty:**
   - Transfer `amount` from creator to module account

4. **Create Bounty:**
   - Assign new bounty ID
   - Store `Bounty` with status `ACTIVE`
   - Store `bounty_by_thread/{thread_id} = bounty_id`
   - Add to `bounty_expiration_queue/{expires_at}/{bounty_id}`
   - Emit `EventBountyCreated`

---

#### `MsgAwardBounty`

Award bounty to one or more winning replies.

| Field | Type | Description |
|-------|------|-------------|
| `creator` | `string` | Bounty creator (signer) |
| `bounty_id` | `uint64` | Bounty to award |
| `awards` | `[]AwardEntry` | Winners with amounts and reasons |

```protobuf
message AwardEntry {
  uint64 post_id = 1;    // Winning reply
  string amount = 2;     // SPARK amount (must sum to bounty total)
  string reason = 3;     // Public justification (required)
  uint32 rank = 4;       // 1 = first place, 2 = second, etc. (ties allowed, e.g., two 2nd places)
}
```

**Authorization:** Original bounty creator only.

**Logic:**
1. **Validation:**
   - Fail with `ErrBountiesDisabled` if `params.bounties_enabled == false`
   - Load bounty by `bounty_id`
   - Fail with `ErrBountyNotFound` if not found
   - Fail with `ErrNotBountyCreator` if `creator != bounty.creator`
   - Fail with `ErrBountyNotActive` if `bounty.status != ACTIVE`
   - Fail with `ErrTooManyWinners` if `len(awards) > max_bounty_winners`
   - For each award:
     - Fail with `ErrInvalidRank` if `rank == 0` (rank is required)
     - Fail with `ErrInvalidRank` if `rank > len(awards)` (rank can't exceed winner count)
     - Fail with `ErrPostNotFound` if reply does not exist
     - Fail with `ErrNotReplyToThread` if reply is not in bounty's thread
     - Fail with `ErrCannotAwardSelf` if `reply.author == creator`
     - Fail with `ErrAwardReasonRequired` if `reason` is empty
   - **Fail with `ErrInvalidRank` if no award has `rank == 1`** (first place required)
   - Fail with `ErrAwardAmountMismatch` if sum of award amounts ≠ bounty amount
   - *Note: Ties are allowed (e.g., two entries with rank=2), but at least one rank=1 is required*

2. **Distribute Awards:**
   - For each award (sorted by rank for deterministic ordering):
     - Transfer `award.amount` from module account to `reply.author`
     - Create `BountyAward` record with rank

3. **Finalize Bounty:**
   - Set `bounty.status = AWARDED`
   - Store awards in `bounty.awards`
   - Remove from expiration queue
   - Delete `bounty_by_thread/{thread_id}`
   - Emit `EventBountyAwarded`

---

#### `MsgIncreaseBounty`

Increase an existing bounty amount.

| Field | Type | Description |
|-------|------|-------------|
| `creator` | `string` | Bounty creator (signer) |
| `bounty_id` | `uint64` | Bounty to increase |
| `additional_amount` | `Coin` | Additional SPARK to add |

**Authorization:** Original bounty creator only.

**Logic:**
1. **Validation:**
   - Load bounty, verify creator, verify status is `ACTIVE`
   - Fail with `ErrBountyTooSmall` if `additional_amount < min_bounty_amount`

2. **Increase:**
   - Transfer `additional_amount` from creator to module account
   - Add to `bounty.amount`
   - Emit `EventBountyIncreased`

---

#### `MsgCancelBounty`

Cancel a bounty before any awards (full refund).

| Field | Type | Description |
|-------|------|-------------|
| `creator` | `string` | Bounty creator (signer) |
| `bounty_id` | `uint64` | Bounty to cancel |

**Authorization:** Original bounty creator only. Only if no awards have been made.

**Logic:**
1. **Validation:**
   - Load bounty, verify creator
   - Fail with `ErrBountyNotActive` if `bounty.status != ACTIVE`
   - Fail with `ErrBountyHasAwards` if `len(bounty.awards) > 0`

2. **Refund:**
   - Transfer full bounty amount from module account to creator
   - Set `bounty.status = CANCELLED`
   - Remove from expiration queue
   - Delete `bounty_by_thread/{thread_id}`
   - Emit `EventBountyCancelled`

---

#### `MsgCreateTagBudget`

Create a tag budget for a group's reserved tag. Simpler alternative to recurring bounties.

| Field | Type | Description |
|-------|------|-------------|
| `group_account` | `string` | Group account (signer via proposal) |
| `tag` | `string` | Reserved tag this budget applies to |
| `initial_pool` | `Coin` | Initial SPARK to fund the pool |
| `members_only` | `bool` | Only group members can receive awards |

**Authorization:** Must be executed by group account (via x/commons proposal).

**Logic:**
1. **Validation:**
   - Fail with `ErrBountiesDisabled` if `params.bounties_enabled == false`
   - Fail with `ErrTagNotReserved` if tag is not in `reserved_tags`
   - Fail with `ErrTagNotOwnedByGroup` if tag's authority is not `group_account`
   - Fail with `ErrTagBudgetExists` if `tag_budget_by_tag/{tag}` exists
   - Fail with `ErrBudgetTooSmall` if `initial_pool < min_tag_budget_amount`

2. **Escrow Pool:**
   - Transfer `initial_pool` from group account to module account

3. **Create Tag Budget:**
   - Assign new ID
   - Store `TagBudget` with `active = true`
   - Store `tag_budget_by_tag/{tag} = id`
   - Emit `EventTagBudgetCreated`

---

#### `MsgAwardFromTagBudget`

Award SPARK from tag budget to a post author. Any group member can award.

| Field | Type | Description |
|-------|------|-------------|
| `awarder` | `string` | Group member awarding (signer) |
| `budget_id` | `uint64` | Tag budget ID |
| `post_id` | `uint64` | Post to award |
| `amount` | `Coin` | SPARK amount to award |
| `reason` | `string` | Public justification (required) |

**Authorization:** Must be a member of the group that owns the budget.

**Logic:**
1. **Validation:**
   - Load tag budget by `budget_id`
   - Fail with `ErrTagBudgetNotFound` if not found
   - Fail with `ErrTagBudgetNotActive` if `active == false`
   - Fail with `ErrNotGroupMember` if awarder is not a member of the budget's group
   - Fail with `ErrInsufficientPool` if `pool_balance < amount`
   - Load post by `post_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrPostMissingTag` if post does not use the budget's tag
   - If `members_only`: fail with `ErrNotGroupMember` if post author is not a group member
   - Fail with `ErrCannotAwardSelf` if `awarder == post.author`
   - Fail with `ErrAwardReasonRequired` if `reason` is empty
   - Fail with `ErrAwardTooSmall` if `amount < min_tag_budget_award`

2. **Transfer Award:**
   - Transfer `amount` from module account to `post.author`
   - Deduct `amount` from `pool_balance`

3. **Record Award:**
   - Create `TagBudgetAward` record
   - Store in `tag_budget_awards/{budget_id}/{award_id}`
   - Emit `EventTagBudgetAwarded`

---

#### `MsgTopUpTagBudget`

Add funds to a tag budget pool.

| Field | Type | Description |
|-------|------|-------------|
| `sender` | `string` | Group account (signer via proposal) |
| `budget_id` | `uint64` | Tag budget ID |
| `amount` | `Coin` | SPARK to add to pool |

**Authorization:** Must be the group account that owns the budget.

**Logic:**
1. **Validation:**
   - Load budget, verify sender is group account

2. **Top Up:**
   - Transfer amount to module account
   - Add to `pool_balance`
   - Emit `EventTagBudgetToppedUp`

---

#### `MsgToggleTagBudget`

Pause or unpause a tag budget.

| Field | Type | Description |
|-------|------|-------------|
| `group_account` | `string` | Group account (signer via proposal) |
| `budget_id` | `uint64` | Tag budget ID |
| `active` | `bool` | True to activate, false to pause |

**Authorization:** Must be the group account.

**Logic:**
1. **Validation:**
   - Load budget, verify group account

2. **Toggle:**
   - Set `budget.active = active`
   - Emit `EventTagBudgetToggled`

---

#### `MsgWithdrawTagBudget`

Withdraw remaining funds from tag budget. Returns pool to group.

| Field | Type | Description |
|-------|------|-------------|
| `group_account` | `string` | Group account (signer via proposal) |
| `budget_id` | `uint64` | Tag budget ID |

**Authorization:** Must be the group account.

**Logic:**
1. **Validation:**
   - Load budget, verify group account

2. **Withdraw:**
   - Transfer `pool_balance` to group account
   - Set `pool_balance = 0`
   - Set `active = false`
   - Delete `tag_budget_by_tag/{tag}`
   - Emit `EventTagBudgetWithdrawn`

---

#### Bounty Behavior on Thread Moderation

When a thread with an active bounty is hidden, locked, or archived, the bounty is automatically cancelled with a small fee.

**Trigger Events:**
- `MsgHidePost` on thread root (sentinel or HR)
- `MsgLockThread` on thread root
- `MsgArchiveThread` on thread

**Cancellation Logic:**
1. **Immediate Effect (Timer Pause):**
   - Bounty status set to `MODERATION_PENDING`
   - **Capture remaining time:** `bounty.time_remaining_at_suspension = bounty.expires_at - now`
   - **Record suspension timestamp:** `bounty.moderation_suspended_at = now`
   - Remove from `bounty_expiration_queue` (timer is paused)

2. **After Appeal Period/Verdict:**
   - If thread **restored** (appeal won, unhidden, unlocked, unarchived):
     - **Resume timer with captured time:** `bounty.expires_at = now + bounty.time_remaining_at_suspension`
     - Clear suspension fields: `moderation_suspended_at = 0`, `time_remaining_at_suspension = 0`
     - Re-add to `bounty_expiration_queue` at new `expires_at`
     - Status returns to `ACTIVE`
     - No fee charged
   - If thread **remains moderated** (appeal lost/timeout, or no appeal filed within `appeal_deadline`):
     - Calculate fee: `bounty.amount × bounty_moderation_fee_ratio` (2.5%)
     - Burn fee
     - Refund remainder to bounty creator
     - Set status = `CANCELLED`
     - Emit `EventBountyModerationCancelled`

**Rationale:**
- Small fee (2.5%) thanks creator for participation while discouraging creating bounties on problematic content
- Waiting for appeal period ensures fair treatment if moderation is overturned
- Automatic handling prevents bounties from being stuck in limbo

---

### 6.6. Pinned & Accepted Replies

Thread authors and sentinels can highlight valuable replies through pinning (visual prominence) and accepting (marks as official answer). Sentinel curation is accountable: authors can dispute pins, and accept proposals require author confirmation.

**Incentives:**
- Sentinels earn `curation_dream_reward` DREAM for undisputed pins and confirmed proposals
- Sentinels are slashed `curation_slash_amount` DREAM if their pin is overturned on dispute
- Curation actions count toward `min_epoch_activity_for_reward`

#### `MsgPinReply`

Pin a reply to the top of a thread.

| Field | Type | Description |
|-------|------|-------------|
| `pinner` | `string` | User pinning (signer) |
| `thread_id` | `uint64` | Thread root post ID |
| `reply_id` | `uint64` | Reply to pin |

**Authorization:**
- **Thread author:** Can pin any reply in their thread (not disputeable)
- **Sentinel:** Can pin replies in threads using non-restricted tags (no bounty on thread); disputeable by author

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load thread root by `thread_id`
   - Fail with `ErrThreadNotFound` if thread does not exist
   - Load reply by `reply_id`
   - Fail with `ErrPostNotFound` if reply does not exist
   - Fail with `ErrNotReplyToThread` if `reply.root_id != thread_id`
   - Fail with `ErrCannotPinHiddenPost` if `reply.status == HIDDEN`

2. **Authorization Check:**
   - If `pinner == thread.author`: Allowed (author pin, not disputeable)
   - Otherwise (sentinel pin):
     - Fail with `ErrNotSentinel` if pinner is not an active sentinel
     - Fail with `ErrCannotPinBountyThread` if `bounty_by_thread/{thread_id}` exists
     - Fail with `ErrCannotPinRestrictedTag` if any thread tag is in `reserved_tags` with `members_can_use == false`

3. **Pin Limit Check:**
   - Load or create `ThreadMetadata` for `thread_id`
   - Fail with `ErrTooManyPinnedReplies` if `len(pinned_reply_ids) >= max_pinned_replies_per_thread`
   - Fail with `ErrAlreadyPinned` if `reply_id` already in `pinned_reply_ids`

4. **Pin Reply:**
   - Add `reply_id` to `pinned_reply_ids`
   - Create `PinnedReplyRecord` with:
     - `is_sentinel_pin = (pinner != thread.author)`
     - `disputed = false`
   - If sentinel pin:
     - Create `SentinelPinRecord` for appeal tracking
     - Increment `sentinel_activity.total_pins` and `epoch_pins`
   - Store updated `ThreadMetadata`
   - Emit `EventReplyPinned`

---

#### `MsgUnpinReply`

Remove a pinned reply from the thread.

| Field | Type | Description |
|-------|------|-------------|
| `unpinner` | `string` | User unpinning (signer) |
| `thread_id` | `uint64` | Thread root post ID |
| `reply_id` | `uint64` | Reply to unpin |

**Authorization:**
- **Thread author:** Can unpin any reply in their thread
- **Original pinner:** Can unpin their own pins (sentinel forfeits reward)
- **Sentinel:** Can unpin any reply (for moderation purposes)

**Logic:**
1. **Validation:**
   - Load thread root, verify exists
   - Load `ThreadMetadata` for `thread_id`
   - Fail with `ErrReplyNotPinned` if `reply_id` not in `pinned_reply_ids`
   - Fail with `ErrPinDisputePending` if `pinned_record.disputed == true`

2. **Authorization Check:**
   - If `unpinner == thread.author`: Allowed
   - Else if `unpinner` is the original pinner: Allowed (self-unpin forfeits curation reward)
   - Else if `unpinner` is active sentinel: Allowed
   - Else: Fail with `ErrUnauthorized`

3. **Unpin Reply:**
   - Remove `reply_id` from `pinned_reply_ids`
   - Remove corresponding `PinnedReplyRecord`
   - If was sentinel pin: delete `SentinelPinRecord`
   - Store updated `ThreadMetadata`
   - Emit `EventReplyUnpinned`

---

#### `MsgDisputePin`

Thread author disputes a sentinel's pin. Creates x/rep initiative for jury resolution.

| Field | Type | Description |
|-------|------|-------------|
| `author` | `string` | Thread author (signer) |
| `thread_id` | `uint64` | Thread root post ID |
| `reply_id` | `uint64` | Pinned reply being disputed |
| `reason` | `string` | Why author disputes this pin |

**Authorization:** Thread author only. Only sentinel pins can be disputed.

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load thread root, verify `author == thread.author`
   - Load `ThreadMetadata`, verify `reply_id` is in `pinned_reply_ids`
   - Load `PinnedReplyRecord` for `reply_id`
   - Fail with `ErrCannotDisputeAuthorPin` if `is_sentinel_pin == false`
   - Fail with `ErrPinAlreadyDisputed` if `disputed == true`
   - Fail with `ErrDisputeReasonRequired` if `reason` is empty

2. **Charge Fee:**
   - Transfer `pin_dispute_fee` from author to module account

3. **Create Dispute:**
   - Set `pinned_record.disputed = true`
   - Load `SentinelPinRecord`
   - Create x/rep initiative for jury resolution
   - Store `initiative_id` in both records
   - Increment `sentinel_activity.epoch_appeals_filed` for the sentinel
   - Emit `EventPinDisputed`

**Resolution (via x/rep jury callback):**
- **Dispute upheld (author wins):**
  - Unpin the reply
  - Slash sentinel `curation_slash_amount` DREAM
  - Refund `pin_dispute_fee` to author
  - Increment `sentinel_activity.overturned_pins`
  - Emit `EventPinDisputeUpheld`
- **Dispute rejected (sentinel wins):**
  - Pin remains
  - Burn `pin_dispute_fee` (or award portion to sentinel)
  - Increment `sentinel_activity.upheld_pins`
  - Award `curation_dream_reward` to sentinel
  - Emit `EventPinDisputeRejected`

---

#### `MsgMarkAcceptedReply`

Mark a reply as the accepted answer. For thread authors, this is immediate. For sentinels, this creates a proposal awaiting author confirmation.

| Field | Type | Description |
|-------|------|-------------|
| `marker` | `string` | User marking (signer) |
| `thread_id` | `uint64` | Thread root post ID |
| `reply_id` | `uint64` | Reply to mark as accepted (0 to clear) |

**Authorization:**
- **Thread author:** Immediate accept/unaccept
- **Sentinel:** Creates proposal (requires author confirmation or auto-confirms after timeout)

**Logic:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load thread root by `thread_id`
   - Fail with `ErrThreadNotFound` if thread does not exist
   - If `reply_id != 0`:
     - Load reply by `reply_id`
     - Fail with `ErrPostNotFound` if reply does not exist
     - Fail with `ErrNotReplyToThread` if `reply.root_id != thread_id`
     - Fail with `ErrCannotAcceptHiddenPost` if `reply.status == HIDDEN`

2. **Authorization & Action:**
   - Load or create `ThreadMetadata` for `thread_id`

   **If `marker == thread.author` (immediate action):**
   - If `reply_id == 0` (clearing):
     - Fail with `ErrNoAcceptedReply` if `accepted_reply_id == 0`
     - Clear any pending proposal as well
     - Set `accepted_reply_id = 0`, clear `accepted_by` and `accepted_at`
     - Emit `EventAcceptedReplyCleared`
   - Else (marking):
     - Clear any pending proposal (author's choice supersedes)
     - If `accepted_reply_id != 0`: Emit `EventAcceptedReplyChanged` (replacing previous)
     - Set `accepted_reply_id = reply_id`
     - Set `accepted_by = marker`
     - Set `accepted_at = now`
     - Emit `EventAcceptedReplyMarked`
   - Store updated `ThreadMetadata`

   **If `marker != thread.author` (sentinel proposal):**
   - Fail with `ErrNotSentinel` if marker is not an active sentinel
   - Fail with `ErrCannotMarkBountyThread` if `bounty_by_thread/{thread_id}` exists
   - Fail with `ErrCannotMarkRestrictedTag` if any thread tag is in `reserved_tags` with `members_can_use == false`
   - Fail with `ErrProposalAlreadyPending` if `proposed_reply_id != 0`
   - Fail with `ErrAlreadyAccepted` if `accepted_reply_id == reply_id` (already accepted)
   - If `reply_id == 0`: Fail with `ErrSentinelCannotClearAccepted` (only author can clear)
   - Set `proposed_reply_id = reply_id`
   - Set `proposed_by = marker`
   - Set `proposed_at = now`
   - Add to `proposal_auto_confirm_queue/{now + accept_proposal_timeout}/{thread_id}`
   - Increment `sentinel_activity.total_proposals` and `epoch_curations`
   - Emit `EventAcceptProposalCreated`
   - Store updated `ThreadMetadata`

---

#### `MsgConfirmProposedReply`

Thread author confirms a sentinel's accept proposal.

| Field | Type | Description |
|-------|------|-------------|
| `author` | `string` | Thread author (signer) |
| `thread_id` | `uint64` | Thread root post ID |

**Authorization:** Thread author only.

**Logic:**
1. **Validation:**
   - Load thread root, verify `author == thread.author`
   - Load `ThreadMetadata`
   - Fail with `ErrNoProposalPending` if `proposed_reply_id == 0`

2. **Confirm Proposal:**
   - Set `accepted_reply_id = proposed_reply_id`
   - Set `accepted_by = proposed_by` (credit to sentinel who proposed)
   - Set `accepted_at = now`
   - Clear `proposed_reply_id`, `proposed_by`, `proposed_at`
   - Remove from `proposal_auto_confirm_queue`
   - Award `curation_dream_reward` to `proposed_by` sentinel
   - Increment `sentinel_activity.confirmed_proposals` for that sentinel
   - Store updated `ThreadMetadata`
   - Emit `EventAcceptProposalConfirmed`

---

#### `MsgRejectProposedReply`

Thread author rejects a sentinel's accept proposal.

| Field | Type | Description |
|-------|------|-------------|
| `author` | `string` | Thread author (signer) |
| `thread_id` | `uint64` | Thread root post ID |
| `reason` | `string` | Optional: why rejecting |

**Authorization:** Thread author only.

**Logic:**
1. **Validation:**
   - Load thread root, verify `author == thread.author`
   - Load `ThreadMetadata`
   - Fail with `ErrNoProposalPending` if `proposed_reply_id == 0`

2. **Reject Proposal:**
   - Record `proposed_by` for tracking
   - Clear `proposed_reply_id`, `proposed_by`, `proposed_at`
   - Remove from `proposal_auto_confirm_queue`
   - Increment `sentinel_activity.rejected_proposals` for that sentinel
   - *No slash* - rejection is feedback, not punishment
   - Store updated `ThreadMetadata`
   - Emit `EventAcceptProposalRejected`

---

#### Accept Proposal Auto-Confirmation (EndBlocker)

Proposals auto-confirm if author doesn't respond within `accept_proposal_timeout` (default 48h).

**EndBlocker Logic:**
1. Query `proposal_auto_confirm_queue` for entries where `time <= now`
2. For each expired entry:
   - Load `ThreadMetadata` for `thread_id`
   - If `proposed_reply_id != 0` (still pending):
     - **Author Activity Check (prevents auto-confirming for inactive authors):**
       - Load author's last activity timestamp from x/rep: `author_last_active`
       - If `author_last_active > proposal_submitted_at`:
         - Author was active but chose not to respond - auto-confirm proceeds
       - Else if `author_last_active < proposal_submitted_at - inactivity_extension_threshold`:
         - Author appears inactive - extend timeout by `accept_proposal_timeout`
         - Re-add to queue at `now + accept_proposal_timeout`
         - Emit `EventAcceptProposalExtended{Reason: "author_inactive"}`
         - Continue to next entry (don't auto-confirm yet)
         - *This extension can only happen once per proposal (track via metadata)*
     - Set `accepted_reply_id = proposed_reply_id`
     - Set `accepted_by = proposed_by`
     - Set `accepted_at = now`
     - Clear proposal fields
     - Award `curation_dream_reward` to sentinel
     - Increment `sentinel_activity.confirmed_proposals`
     - Emit `EventAcceptProposalAutoConfirmed`
   - Remove from queue

---

#### `MsgAssignBountyToReply`

Assign bounty reward to a specific reply (shorthand for `MsgAwardBounty` with single winner).

| Field | Type | Description |
|-------|------|-------------|
| `creator` | `string` | Bounty creator (signer) |
| `thread_id` | `uint64` | Thread with bounty |
| `reply_id` | `uint64` | Reply to award bounty to |
| `reason` | `string` | Public justification |

**Authorization:** Original bounty creator only.

**Logic:**
1. **Validation:**
   - Load `bounty_by_thread/{thread_id}` to get `bounty_id`
   - Fail with `ErrNoBountyOnThread` if no bounty exists
   - Delegate to `MsgAwardBounty` logic with single award entry:
     - `awards = [{post_id: reply_id, amount: bounty.amount, reason: reason}]`

2. **Optional Auto-Accept:**
   - After awarding, automatically mark reply as accepted:
     - Load or create `ThreadMetadata`
     - Set `accepted_reply_id = reply_id`, `accepted_by = creator`, `accepted_at = now`
     - Emit `EventAcceptedReplyMarked`
   - This provides a convenient single action for "this reply solved my problem"

---

#### `MsgUpvotePost`

Upvote a post (public reaction). Enforces one reaction per user per post — if the user already has a reaction on this post, the message fails. Members upvote for free; non-members pay spam tax.

| Field | Type | Description |
|-------|------|-------------|
| `voter` | `string` | Voter address (signer) |
| `post_id` | `uint64` | Post to upvote |

**Authorization:** Any user. Non-members pay `reaction_spam_tax`.

**Design Note — Public vs Private Reactions:**
- Public reactions store a `ReactionRecord` keyed by `{post_id}/{voter}`, enabling one-per-target enforcement and voter identity visibility
- Private reactions use ZK nullifiers for the same one-per-target guarantee without revealing identity (routed via `x/shield`'s `MsgShieldedExec`)
- Both modes share the unified `max_reactions_per_day` budget
- Reactions are non-removable in both modes (parity — private reactions cannot be undone via nullifier, so public reactions are also permanent)

**State Transitions:**
1. **Validation:**
   - Fail with `ErrReactionsDisabled` if `params.reactions_enabled == false`
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `post_id`; fail with `ErrPostNotFound` if missing
   - Fail with `ErrCannotReactToHidden` if `post.status == HIDDEN`
   - Fail with `ErrAlreadyReacted` if `reaction_records/{post_id}/{voter}` exists

2. **Check Membership & Charge Tax:**
   - Query `x/rep` for membership status of `voter`
   - If NOT a member: Burn `reaction_spam_tax` from voter

3. **Rate Limit Check (unified budget):**
   - Load or create `UserReactionLimit` for voter
   - Calculate effective count using sliding window (same formula as before)
   - Fail with `ErrReactionLimitExceeded` if `effective_count >= max_reactions_per_day`

4. **Store Reaction:**
   - Store `ReactionRecord { post_id, voter, REACTION_TYPE_UPVOTE, now }` at `reaction_records/{post_id}/{voter}`
   - Increment `post.upvote_count`
   - Increment `UserReactionLimit.current_day_count`
   - Store updated post

5. **Emit Event:**
   - Emit `EventPostUpvoted`

---

#### `MsgDownvotePost`

Downvote a post (public reaction). Requires SPARK deposit which is burned immediately (no refund). Enforces one reaction per user per post — if the user already has any reaction (upvote or downvote) on this post, the message fails.

| Field | Type | Description |
|-------|------|-------------|
| `voter` | `string` | Voter address (signer) |
| `post_id` | `uint64` | Post to downvote |

**Authorization:** Any user with sufficient SPARK for deposit.

**State Transitions:**
1. **Validation:**
   - Fail with `ErrReactionsDisabled` if `params.reactions_enabled == false`
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `post_id`; fail with `ErrPostNotFound` if missing
   - Fail with `ErrCannotReactToHidden` if `post.status == HIDDEN`
   - Fail with `ErrCannotDownvoteOwnPost` if `voter == post.author`
   - Fail with `ErrAlreadyReacted` if `reaction_records/{post_id}/{voter}` exists

2. **Burn Deposit:**
   - Burn `downvote_deposit` from voter (no refund, creates conviction signal)

3. **Rate Limit Check (unified budget):**
   - Load or create `UserReactionLimit` for voter
   - Calculate effective count using sliding window
   - Fail with `ErrReactionLimitExceeded` if `effective_count >= max_reactions_per_day`

4. **Store Reaction:**
   - Store `ReactionRecord { post_id, voter, REACTION_TYPE_DOWNVOTE, now }` at `reaction_records/{post_id}/{voter}`
   - Increment `post.downvote_count`
   - Increment `UserReactionLimit.current_day_count`
   - Store updated post

5. **Emit Event:**
   - Emit `EventPostDownvoted`

---

#### `MsgEditPost`

Edits post content within time window. Free during grace period, SPARK fee required after.

| Field | Type | Proto # | Description |
|-------|------|---------|-------------|
| `creator` | `string` | 1 | Post author (signer) |
| `post_id` | `uint64` | 2 | Post to edit |
| `new_content` | `string` | 3 | Updated content |
| `tags` | `[]string` | 4 | Updated tags |
| `content_type` | `ContentType` | 5 | Updated content type enum from `sparkdream.common.v1` |

**Authorization:** Original post author only.

**Fees:**
- **Gas Fee:** Paid by all users (Standard Cosmos SDK Execution Cost)
- **Edit Fee:** Paid if edit occurs after `edit_grace_period` (e.g., 25 SPARK)

**State Transitions:**
1. **Validation:**
   - Fail with `ErrEditingDisabled` if `params.editing_enabled == false`
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `post_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrNotPostAuthor` if `author != post.author`
   - Fail with `ErrCannotEditHiddenPost` if `post.status == HIDDEN`
   - Fail with `ErrCannotEditDeletedPost` if `post.status == DELETED`
   - Fail with `ErrCannotEditArchivedPost` if `post.status == ARCHIVED`

2. **Check Edit Window:**
   - Calculate `post_age = now - post.created_at`
   - Fail with `ErrEditWindowExpired` if `post_age > params.edit_max_window`

3. **Content Validation:**
   - Fail with `ErrContentTooLarge` if `len(new_content) > params.max_content_size`
   - Fail with `ErrNoContentChange` if `new_content == post.content` (no-op protection)

4. **Charge Edit Fee (if outside grace period):**
   - If `post_age > params.edit_grace_period`:
     - Transfer `edit_fee` from author to module account
     - Fee distribution: 50% burned, 50% to reward pool (same as spam tax)

5. **Update Post:**
   - Set `post.content = new_content`
   - Set `post.edited = true`
   - Set `post.edited_at = now`
   - Store updated post

6. **Emit Event:**
   - Emit `EventPostEdited`

**Design Rationale:**
- **5-minute grace period**: Allows typo fixes immediately after posting without penalty
- **24-hour maximum window**: Preserves discussion integrity while allowing reasonable time to refine posts
- **No version history**: Reduces state bloat; clients can detect edits via `edited` flag
- **SPARK fee after grace**: Discourages frivolous edits, funds reward pool
- **Locked/hidden posts excluded**: Prevents editing during moderation disputes

---

#### `MsgDeletePost`

Allows post author to soft-delete their own post. Content is replaced with "[deleted by author]" placeholder.

| Field | Type | Description |
|-------|------|-------------|
| `author` | `string` | Post author (signer) |
| `post_id` | `uint64` | Post to delete |

**Authorization:** Original post author only.

**Fees:**
- **Gas Fee:** Paid by all users (Standard Cosmos SDK Execution Cost)

**State Transitions:**
1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Load post by `post_id`
   - Fail with `ErrPostNotFound` if post does not exist
   - Fail with `ErrNotPostAuthor` if `author != post.author`
   - Fail with `ErrCannotDeleteHiddenPost` if `post.status == HIDDEN` (under moderation review)
   - Fail with `ErrPostAlreadyDeleted` if `post.status == DELETED`
   - Fail with `ErrCannotDeleteArchivedPost` if `post.status == ARCHIVED`

2. **Check Reply Count:**
   - If `post.reply_count > 0`:
     - Content remains but is replaced with "[deleted by author]"
     - Thread structure preserved for context
   - If `post.reply_count == 0`:
     - Same behavior (soft-delete, content replaced)
     - Note: Full deletion would break thread integrity if replies added later

3. **Update Post:**
   - Set `post.status = DELETED`
   - Set `post.content = "[deleted by author]"`
   - Set `post.deleted_at = now`
   - Clear `post.tags` (no longer associated with tags)
   - Store updated post

4. **Update Rate Limits:**
   - Decrement author's post count in `UserRateLimit`

5. **Cancel Active Bounty (if thread author):**
   - If this post is a thread root (`post.parent_id == 0`) with an active bounty:
     - Refund bounty to author (minus moderation fee ratio for cleanup)
     - Set bounty status to CANCELLED

6. **Remove Initiative Link (if applicable):**
   - If `post.initiative_id > 0` and `repKeeper` is not nil:
     - Call `repKeeper.RemoveContentInitiativeLink(ctx, post.initiative_id, STAKE_TARGET_FORUM_CONTENT, post_id)`
     - Emit `EventInitiativeLinkRemoved` with reason "deleted"

7. **Emit Event:**
   - Emit `EventPostDeleted` with `post_id`, `author`, `had_replies`

**Design Rationale:**
- **Soft-delete only**: Preserves thread structure and context for existing replies
- **No time limit**: Unlike editing, authors can delete at any time (their content, their choice)
- **Hidden posts excluded**: Prevents deleting evidence during active moderation dispute
- **Tags cleared**: Deleted content no longer associated with tags
- **Bounty handling**: Fair refund since content is no longer available for answers

---

### 6.2. Moderation (The Sentinel Flow)

#### `MsgHidePost`

Sentinel hides a post for moderation.

| Field | Type | Description |
|-------|------|-------------|
| `sentinel` | `string` | Sentinel address (signer) |
| `post_id` | `uint64` | Target post ID |
| `reason_code` | `ModerationReason` | Structured reason (unified with flag categories) |
| `reason_text` | `string` | Custom reason text (required if reason_code=OTHER) |

**Validation:**
- Fail with `ErrInvalidModerationReason` if `reason_code == UNSPECIFIED`
- Fail with `ErrReasonTextRequired` if `reason_code == OTHER` and `reason_text` is empty

**Authorization:**
- Fail with `ErrForumPaused` if `params.forum_paused == true`
- Fail with `ErrModerationPaused` if `params.moderation_paused == true`
- Sender must satisfy `x/rep` Tier >= `min_rep_tier_sentinel`
- Load `SentinelActivity` for sender
- **Check bond status:** Fail with `ErrSentinelDemoted` if `sentinel.bond_status == DEMOTED`
- **Check delegated backing (must be from OTHER established members):**
  - Query `x/rep.GetQualifiedDelegatedDREAM(sentinel, min_backer_membership_duration)`
    - Excludes self-delegation
    - Only counts backing from members who joined >= `min_backer_membership_duration` ago
    - Prevents Sybil backing via newly-created accounts
  - Fail with `ErrInsufficientBacking` if qualified_backing < `min_sentinel_backing`
- **Check overturn cooldown:** Fail with `ErrSentinelCooldown` if `sentinel.overturn_cooldown_until > now`
- **Check epoch hide limit:** Fail with `ErrHideLimitExceeded` if `sentinel.epoch_hides >= max_hides_per_epoch`
- **Check bond commitment capacity (aggregate tracking on SentinelActivity):**
  - `available_bond = current_bond - total_committed_bond`
  - Fail with `ErrInsufficientBond` if `available_bond < sentinel_slash_amount`

**Action:**
1. Set `post.status = HIDDEN`
2. Set `post.hidden_by = sentinel`
3. Set `post.hidden_at = now`
4. **Commit bond (aggregate counter on SentinelActivity):**
   - `commit_amount = sentinel_slash_amount`
   - `sentinel.total_committed_bond += commit_amount`
   - `sentinel.pending_hide_count += 1`
5. Create `HideRecord`:
   - `post_id = post.id`
   - `sentinel = sentinel`
   - `hidden_at = now`
   - `sentinel_bond_snapshot = sentinel.current_bond`
   - `sentinel_backing_snapshot = x/rep.GetQualifiedDelegatedDREAM(sentinel, min_backer_membership_duration)`
   - `committed_amount = commit_amount` (stored on HideRecord for accurate release)
   - `reason_code = provided_reason_code`
   - `reason_text = provided_reason_text`
6. Add post to `hidden_queue/{now + hidden_expiration}/{post_id}`
7. **Set appeal cooldown:** Store `appeal_cooldown/{post_id} = now + hide_appeal_cooldown`
8. Increment `sentinel.total_hides` and `sentinel.epoch_hides`
9. Emit `EventPostHidden`

---

#### `MsgAppealPost`

Author appeals a hidden post to jury.

| Field | Type | Description |
|-------|------|-------------|
| `appellant` | `string` | Original post author (signer) |
| `post_id` | `uint64` | Hidden post ID |

**Cost:** Deposit `appeal_fee` (SPARK) + Standard Gas Fee.

**State Transition Logic:**

All state changes within `MsgAppealPost` are atomic. If any step fails, the entire transaction reverts including any escrowed funds.

1. **Validation:**
   - Fail with `ErrForumPaused` if `params.forum_paused == true`
   - Fail with `ErrAppealsPaused` if `params.appeals_paused == true`
   - Fail with `ErrNotPostAuthor` if `appellant != post.author`
   - Fail with `ErrPostNotHidden` if `post.status != HIDDEN`
   - Fail with `ErrAppealAlreadyFiled` if appeal already exists for this post
   - **Check appeal cooldown:** If `appeal_cooldown/{post_id}` exists and `value > now`:
     - Fail with `ErrAppealCooldown` (must wait before appealing, prevents instant collusion)

2. **Escrow Appeal Fee:**
   - Transfer `appeal_fee` from appellant to module account

3. **Create Initiative in x/rep:**
   - Type: `MODERATION_APPEAL` (Fast-Track Jury)
   - Payload: `{ post_id, sentinel_addr, appellant_addr }`
   - Deadline: `now + appeal_deadline`
   - If `x/rep.CreateInitiative` fails, transaction reverts (escrow returned automatically)

4. **Remove from Hidden Queue:** (Critical for timing correctness)
   - Remove post from `hidden_queue` (appeal supersedes auto-expiration)
   - **Delete appeal cooldown key** (consumed)
   - **Note:** This removal is essential. Without it, `hidden_expiration` (7d) would delete the post before `appeal_deadline` (14d) expires. Once appealed, the post's fate is determined solely by the jury verdict, not the hidden queue timer.

5. **Track Appeal for Sentinel Metrics:**
   - Load `HideRecord` to get sentinel address
   - Load `SentinelActivity` for sentinel
   - Increment `sentinel.epoch_appeals_filed` (used for appeal rate calculation)

6. **Emit Event:**
   - Emit `EventAppealFiled`

**Note:** `x/forum` does not handle voting. `x/rep` handles jury selection, tallying, and timeout detection.

**Jury Selection Requirements (implemented in x/rep):**
- **Reputation-Weighted Selection:** Jury selection should weight candidates by reputation score from x/rep. Higher reputation = higher probability of selection. This makes Sybil attacks expensive (need to build reputation across many accounts).
- **Minimum Jury Reputation:** Candidates must have minimum reputation tier (e.g., Tier 2+) to be eligible for jury duty.
- **Conflict Exclusions:** Exclude appellant, sentinel, and any addresses that have transacted with them within N epochs.
- **Participation Tracking:** Track participation rate per juror. Exclude non-voters from future selections after <50% participation over 10+ assignments.
- **Pool Size Requirement:** Select from pool of at least 3x the required quorum to ensure diversity.

**Jury Insufficiency Fallback:** If `x/rep` cannot form a jury quorum (due to exclusions, conflicts, or small community size), the appeal resolves in favor of the appellant:
- `x/rep` calls `OnJuryInsufficientForAppeal(ctx, initiativeID)` callback
- Post is restored to ACTIVE status
- 90% of appeal fee refunded to appellant (10% burned for gas/spam protection)
- Sentinel receives no penalty (not their fault)
- Committed bond released (per-hide map entry deleted)
- Emit `EventAppealResolvedJuryInsufficient{PostID, Appellant, RefundAmount}`

*Rationale: System failures shouldn't penalize the person who paid to appeal.*

**Anti-Collusion:** The `hide_appeal_cooldown` (e.g., 1 hour) prevents instant sentinel-appellant coordination where a sentinel hides and friend immediately appeals. This delay gives time for the community to observe the hide action.

---

## 7. Integration Interfaces

> **Implementation note:** The `x/forum/types/expected_keepers.go` file defines the keeper interfaces. The `RepKeeper` interface includes the following methods that extend beyond the spec's original design:
> - **Author bonds:** `CreateAuthorBond`, `SlashAuthorBond`, `GetAuthorBond` — allows post authors to optionally lock DREAM as collateral on their content
> - **Content conviction:** `GetContentConviction`, `GetContentStakes` — allows querying conviction staking data for forum content
> - **Conviction propagation:** `ValidateInitiativeReference`, `RegisterContentInitiativeLink`, `RemoveContentInitiativeLink` — enables cross-module conviction propagation from forum content to x/rep initiatives (see §16.16)
> - **CommonsKeeper** uses `IsCouncilAuthorized` for operational param updates (not just group policy membership)

### 7.1. Initiative Hooks (Callback from `x/rep`)

> **Implementation status:** The initiative hooks callback system is **partially implemented**. The `RepKeeper` interface in `x/forum/types/expected_keepers.go` defines the `CreateAppealInitiative` method for filing appeals, and appeal messages (`MsgAppealPost`, `MsgAppealThreadLock`, `MsgAppealThreadMove`) call this. However, the `OnInitiativeFinalized` callback handler described below is **not yet implemented** in keeper code.

`x/forum` implements the `InitiativeHooks` interface from `x/rep` to receive jury verdicts.

```go
// x/forum/keeper/hooks.go

func (k Keeper) OnInitiativeFinalized(ctx sdk.Context, verdict Result, payload []byte) {
    data := DecodeAppealPayload(payload)
    post, found := k.GetPost(ctx, data.PostID)
    if !found {
        return // Post already deleted
    }

    params := k.GetParams(ctx)
    hideRecord, found := k.GetHideRecord(ctx, data.PostID)
    if !found {
        return // HideRecord already cleaned up
    }

    // ============================================
    // BALANCE SAFETY CHECK
    // ============================================
    moduleBalance := k.bankKeeper.GetBalance(ctx, ModuleAccount, params.AppealFee.Denom)
    if moduleBalance.Amount.LT(params.AppealFee.Amount) {
        // Module account has insufficient funds - this should never happen
        // Log error and restore post to prevent permanent limbo
        k.Logger(ctx).Error("module account has insufficient balance for appeal resolution",
            "required", params.AppealFee.Amount.String(),
            "available", moduleBalance.Amount.String())
        post.Status = PostStatusActive
        post.HiddenBy = ""
        post.HiddenAt = 0
        k.SetPost(ctx, post)
        k.releaseCommittedBond(ctx, hideRecord)
        k.DeleteHideRecord(ctx, data.PostID)
        k.EmitEvent(ctx, EventAppealFailedInsufficientBalance{PostID: data.PostID})
        return
    }

    // Calculate fee distribution (jury always gets their share)
    juryShare := params.AppealFee.Amount.ToLegacyDec().Mul(params.JuryShareRatio).TruncateInt()
    remainingFee := params.AppealFee.Amount.Sub(juryShare)

    // ============================================
    // PAY JURORS PER-VERDICT (percentage-based to prevent starvation)
    // ============================================
    juryPool := k.GetJuryPool(ctx)
    if juryPool.Amount.IsPositive() {
        // Calculate payout as percentage of pool with cap
        // This prevents pool starvation under high load
        maxPayoutPercent := params.JuryPayoutMaxPercent    // e.g., 5% of pool per verdict
        maxPayoutFixed := sdk.NewIntFromString(params.JuryPayoutMaxFixed)  // e.g., 100 SPARK cap

        percentPayout := juryPool.Amount.Mul(sdk.NewInt(int64(maxPayoutPercent))).Quo(sdk.NewInt(100))
        juryPayout := sdk.MinInt(percentPayout, maxPayoutFixed)

        if juryPayout.IsPositive() {
            // Pay jurors who participated in this verdict (via x/rep callback)
            k.repKeeper.DistributeJuryReward(ctx, data.InitiativeID, juryPayout)
            k.SetJuryPool(ctx, sdk.NewCoin(juryPool.Denom, juryPool.Amount.Sub(juryPayout)))
        }
    }

    switch verdict {
    case VERDICT_APPROVED:
        // Appellant wins: Sentinel was WRONG
        post.Status = PostStatusActive
        post.HiddenBy = ""
        post.HiddenAt = 0
        k.SetPost(ctx, post)

        // Distribute appeal fee: 80% to appellant, 20% burned
        k.bankKeeper.SendCoins(ctx, ModuleAccount, data.Appellant, sdk.NewCoin(params.AppealFee.Denom, remainingFee))
        burnShare := params.AppealFee.Amount.Sub(remainingFee)
        k.bankKeeper.BurnCoins(ctx, ModuleAccount, sdk.NewCoins(sdk.NewCoin(params.AppealFee.Denom, burnShare)))

        // Slash sentinel's DREAM bond with fixed amount
        sentinel := k.GetSentinelActivity(ctx, data.Sentinel)
        slashAmount := sdk.NewIntFromString(params.SentinelSlashAmount)
        currentBond := sdk.NewIntFromString(sentinel.CurrentBond)

        // Apply slash (clamp to available bond)
        actualSlash := sdk.MinInt(slashAmount, currentBond)
        newBond := currentBond.Sub(actualSlash)
        sentinel.CurrentBond = newBond.String()

        // Burn the slashed DREAM via x/rep
        if actualSlash.IsPositive() {
            k.repKeeper.BurnDREAM(ctx, data.Sentinel, actualSlash)
        }

        // Update bond status based on thresholds
        minBond := sdk.NewIntFromString(params.MinSentinelBond)
        demotionThreshold := sdk.NewIntFromString(params.SentinelDemotionThreshold)

        if newBond.LT(demotionThreshold) {
            sentinel.BondStatus = SENTINEL_BOND_STATUS_DEMOTED
            k.EmitEvent(ctx, EventSentinelDemoted{
                Sentinel:  data.Sentinel,
                FinalBond: newBond.String(),
                Reason:    "bond_below_demotion_threshold",
            })
        } else if newBond.LT(minBond) {
            sentinel.BondStatus = SENTINEL_BOND_STATUS_RECOVERY
            k.EmitEvent(ctx, EventSentinelRecoveryMode{
                Sentinel:    data.Sentinel,
                CurrentBond: newBond.String(),
                TargetBond:  minBond.String(),
            })
        }

        // Update accuracy metrics
        sentinel.OverturnedHides++
        sentinel.EpochAppealsResolved++
        sentinel.ConsecutiveOverturns++
        sentinel.ConsecutiveUpheld = 0 // Reset upheld counter on overturn

        // Apply escalating cooldown based on consecutive overturns
        baseCooldown := params.SentinelOverturnCooldown
        multiplier := int64(1) << (sentinel.ConsecutiveOverturns - 1)
        cooldownDuration := baseCooldown * multiplier
        maxCooldown := int64(604800) // 7 days cap
        if cooldownDuration > maxCooldown {
            cooldownDuration = maxCooldown
        }
        sentinel.OverturnCooldownUntil = ctx.BlockTime().Unix() + cooldownDuration

        // Release committed bond for this hide (uses HideRecord.committed_amount)
        k.releaseCommittedBond(ctx, hideRecord)

        // Note: sentinel already updated by releaseCommittedBond, but we still need to save our local changes
        sentinel = k.GetSentinelActivity(ctx, data.Sentinel) // Refresh after release
        k.SetSentinelActivity(ctx, sentinel)

        k.EmitEvent(ctx, EventPostRestored{PostID: data.PostID, Appellant: data.Appellant})
        k.EmitEvent(ctx, EventSentinelSlashed{
            Sentinel:    data.Sentinel,
            SlashAmount: actualSlash.String(),
            NewBond:     newBond.String(),
            BondStatus:  sentinel.BondStatus,
        })

        // Note: Downvote deposits were burned at downvote time, no action needed here

    case VERDICT_REJECTED:
        // Sentinel wins: Sentinel was RIGHT
        post.Status = PostStatusDeleted
        // NOTE: post.Content is NOT cleared - content remains in state for historical record
        // and censorship resistance. Frontends may hide DELETED content, but it remains
        // queryable on-chain for transparency and audit purposes.
        k.SetPost(ctx, post)

        // Distribute appeal fee: 50% to sentinel, 50% burned
        sentinelShare := params.AppealFee.Amount.Mul(sdk.NewInt(50)).Quo(sdk.NewInt(100))
        burnShare := params.AppealFee.Amount.Sub(sentinelShare)
        k.bankKeeper.SendCoins(ctx, ModuleAccount, data.Sentinel, sdk.NewCoin(params.AppealFee.Denom, sentinelShare))
        k.bankKeeper.BurnCoins(ctx, ModuleAccount, sdk.NewCoins(sdk.NewCoin(params.AppealFee.Denom, burnShare)))

        // Update sentinel accuracy metrics
        sentinel := k.GetSentinelActivity(ctx, data.Sentinel)
        sentinel.UpheldHides++
        sentinel.EpochAppealsResolved++
        sentinel.ConsecutiveUpheld++

        // Only reset consecutive overturns after N consecutive upheld hides
        // This prevents "reset attacks" where sentinel hides obvious violations to reset counter
        if sentinel.ConsecutiveUpheld >= params.UpheldHidesToReset {
            sentinel.ConsecutiveOverturns = 0
            sentinel.ConsecutiveUpheld = 0 // Reset the upheld counter too
            k.EmitEvent(ctx, EventSentinelOverturnCounterReset{
                Sentinel:       data.Sentinel,
                UpheldRequired: params.UpheldHidesToReset,
            })
        }

        // Release committed bond for this hide (uses HideRecord.committed_amount)
        k.releaseCommittedBond(ctx, hideRecord)

        // Note: Downvote deposits were burned at downvote time, no refund needed

    case VERDICT_TIMEOUT:
        // Jury failed to reach verdict within appeal_deadline
        // BALANCED TIMEOUT: Neither party penalized, post restored, fee split
        post.Status = PostStatusActive
        post.HiddenBy = ""
        post.HiddenAt = 0
        k.SetPost(ctx, post)

        // Split fee: 50% refunded to appellant, 50% burned
        appellantRefund := params.AppealFee.Amount.Mul(sdk.NewInt(50)).Quo(sdk.NewInt(100))
        burnAmount := params.AppealFee.Amount.Sub(appellantRefund)

        k.bankKeeper.SendCoins(ctx, ModuleAccount, data.Appellant, sdk.NewCoin(params.AppealFee.Denom, appellantRefund))
        k.bankKeeper.BurnCoins(ctx, ModuleAccount, sdk.NewCoins(sdk.NewCoin(params.AppealFee.Denom, burnAmount)))

        // Release committed bond (no penalty on timeout, uses HideRecord.committed_amount)
        k.releaseCommittedBond(ctx, hideRecord)

        // ============================================
        // JURY PARTICIPATION TRACKING (timeout abuse prevention)
        // ============================================
        // Track non-voting jurors for future exclusion
        assignedJurors := k.repKeeper.GetAssignedJurors(ctx, data.InitiativeID)
        for _, juror := range assignedJurors {
            participation := k.GetJuryParticipation(ctx, juror)
            participation.TotalAssigned++
            if k.repKeeper.JurorVoted(ctx, data.InitiativeID, juror) {
                participation.TotalVoted++
            } else {
                participation.TotalTimeouts++
            }
            // Auto-exclude jurors with <50% participation rate after 10+ assignments
            if participation.TotalAssigned >= 10 {
                rate := float64(participation.TotalVoted) / float64(participation.TotalAssigned)
                if rate < 0.50 {
                    participation.Excluded = true
                    k.EmitEvent(ctx, EventJurorExcluded{Juror: juror, ParticipationRate: rate})
                }
            }
            k.SetJuryParticipation(ctx, participation)
        }

        k.EmitEvent(ctx, EventAppealTimedOut{
            PostID:          data.PostID,
            Outcome:         "restored_timeout",
            AppellantRefund: appellantRefund.String(),
            JuryIncentive:   juryIncentive.String(),
        })

        // Note: Downvote deposits were burned at downvote time, no action needed here
    }

    // Clean up HideRecord
    k.DeleteHideRecord(ctx, data.PostID)
}

// releaseCommittedBond releases the committed bond for a hide (uses HideRecord's committed_amount)
// Called on all resolution paths: appeal upheld/rejected/timeout, jury insufficiency, hidden post expiration
func (k Keeper) releaseCommittedBond(ctx sdk.Context, hideRecord HideRecord) {
    sentinel := k.GetSentinelActivity(ctx, hideRecord.Sentinel)
    committedAmount := sdk.NewIntFromString(hideRecord.CommittedAmount)
    totalCommitted := sdk.NewIntFromString(sentinel.TotalCommittedBond)

    // Decrement aggregate counter (clamp to zero for safety)
    newCommitted := totalCommitted.Sub(committedAmount)
    if newCommitted.IsNegative() {
        newCommitted = sdk.ZeroInt() // Should never happen, but clamp for safety
        k.Logger(ctx).Error("committed bond underflow", "sentinel", hideRecord.Sentinel)
    }
    sentinel.TotalCommittedBond = newCommitted.String()

    // Decrement pending count
    if sentinel.PendingHideCount > 0 {
        sentinel.PendingHideCount--
    }

    k.SetSentinelActivity(ctx, sentinel)
}

// OnJuryInsufficientForAppeal handles appeals that can't proceed due to jury unavailability
// Called by x/rep when quorum cannot be formed (exclusions, conflicts, small community)
// Favors appellant: post restored, 90% refund (system failure shouldn't penalize appellant)
func (k Keeper) OnJuryInsufficientForAppeal(ctx sdk.Context, payload []byte) {
    data := DecodeAppealPayload(payload)
    post, found := k.GetPost(ctx, data.PostID)
    if !found {
        return // Post already deleted
    }

    params := k.GetParams(ctx)
    hideRecord, found := k.GetHideRecord(ctx, data.PostID)
    if !found {
        return // HideRecord already cleaned up
    }

    // Restore post to active (favor appellant)
    post.Status = PostStatusActive
    post.HiddenBy = ""
    post.HiddenAt = 0
    k.SetPost(ctx, post)

    // Refund 90% of appeal fee to appellant (10% burned for gas/spam)
    refundAmount := params.AppealFee.Amount.Mul(sdk.NewInt(90)).Quo(sdk.NewInt(100))
    burnAmount := params.AppealFee.Amount.Sub(refundAmount)
    k.bankKeeper.SendCoins(ctx, ModuleAccount, data.Appellant, sdk.NewCoin(params.AppealFee.Denom, refundAmount))
    k.bankKeeper.BurnCoins(ctx, ModuleAccount, sdk.NewCoins(sdk.NewCoin(params.AppealFee.Denom, burnAmount)))

    // Release sentinel's committed bond (no penalty - not their fault)
    k.releaseCommittedBond(ctx, hideRecord)

    // Clean up
    k.DeleteHideRecord(ctx, data.PostID)

    k.EmitEvent(ctx, EventAppealResolvedJuryInsufficient{
        PostID:       data.PostID,
        Appellant:    data.Appellant,
        RefundAmount: refundAmount.String(),
        Reason:       "jury_quorum_unavailable",
    })
}
```

### 7.2. EndBlocker (GC + Reward Distribution)

> **Implementation status:** The current EndBlocker implementation (`x/forum/keeper/abci.go`) only implements **ephemeral post pruning** — walking the `ExpirationQueue` and hard-deleting posts whose TTL has passed (up to 100 per block). The complex multi-phase design below (reward pool management, epoch-based sentinel rewards, DREAM minting, accuracy decay) is **not yet implemented**. The design is preserved here as the target specification.

#### 7.2.1. Current Implementation

```go
// x/forum/keeper/abci.go (actual implementation)
const maxPrunePerBlock = 100

func (k Keeper) EndBlocker(ctx context.Context) error {
    now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
    return k.PruneExpiredPosts(ctx, now)
}

// PruneExpiredPosts walks ExpirationQueue up to `now` and hard-deletes
// up to maxPrunePerBlock posts. Also cleans up associated PostFlag and
// HideRecord entries. Skips salvaged posts (ExpirationTime == 0).
//
// **Conviction Renewal Check (before tombstoning):**
// For each post at TTL expiry, if the post has an initiative_id > 0
// and repKeeper is available:
//   1. Query repKeeper.GetContentConviction(ctx, STAKE_TARGET_FORUM_CONTENT, postID)
//   2. If conviction >= params.conviction_renewal_threshold:
//      a. First time (conviction_sustained == false):
//         - Set post.conviction_sustained = true
//         - Set post.expiration_time = block_time + params.conviction_renewal_period
//         - Re-insert into ExpirationQueue at new expiry
//         - Emit "forum.post.conviction_sustained" event
//         - Skip tombstoning
//      b. Already sustained (conviction_sustained == true):
//         - Set post.expiration_time = block_time + params.conviction_renewal_period
//         - Re-insert into ExpirationQueue at new expiry
//         - Emit "forum.post.renewed" event
//         - Skip tombstoning
//   3. If conviction < threshold and conviction_sustained == true:
//      - Set post.conviction_sustained = false (conviction dropped, post expires normally)
//   4. On tombstone: call repKeeper.RemoveContentInitiativeLink(ctx, initiative_id,
//      STAKE_TARGET_FORUM_CONTENT, postID) to clean up the propagation link
```

#### 7.2.2. Design Target (Not Yet Implemented)

```go
// x/forum/keeper/abci.go (design target)

func (k Keeper) EndBlock(ctx sdk.Context) {
    params := k.GetParams(ctx)

    // ============================================
    // PHASE 1: Garbage Collection (Every Block)
    // ============================================
    // Prevents state bloat even when no posts are created

    lastGC := k.GetLastGCBlock(ctx)
    gcInterval := int64(100) // Run GC every 100 blocks (~10 minutes)

    if ctx.BlockHeight()-lastGC >= gcInterval {
        // Ephemeral Posts
        k.PruneExpiredPosts(ctx, params.LazyPruneLimit * 5) // Higher limit for EndBlocker

        // Hidden Posts (update sentinel metrics for expired hides, release committed bond)
        k.PruneExpiredHiddenPosts(ctx, params.LazyPruneLimit * 5)
        // NOTE: PruneExpiredHiddenPosts also releases committed_bond for each expired hide
        // and increments unchallenged_hides for the sentinel

        // Expired Tags
        k.PruneExpiredTags(ctx, params.LazyPruneLimit * 5)

        // Clean up old rate limit entries for inactive users
        // With epoch-based system, check last_post_time instead of timestamp list
        k.PruneStaleRateLimits(ctx, 86400 * 7) // Remove entries for users with last_post_time >7 days ago

        // Clean up expired archive cooldowns (prevents dead state accumulation)
        k.PruneExpiredArchiveCooldowns(ctx, params.LazyPruneLimit * 5)

        // Clean up expired appeal cooldowns
        k.PruneExpiredAppealCooldowns(ctx, params.LazyPruneLimit * 5)

        // Clean up expired member report timestamps (for resolved reports)
        k.PruneOrphanedReportTimestamps(ctx)

        k.SetLastGCBlock(ctx, ctx.BlockHeight())
    }

    // ============================================
    // PHASE 2: Reward Pool Overflow Management
    // ============================================

    pool := k.GetRewardPool(ctx)
    if pool.Amount.GT(params.MaxRewardPool.Amount) {
        overflow := pool.Amount.Sub(params.MaxRewardPool.Amount)
        burnAmount := overflow.ToLegacyDec().Mul(params.OverflowBurnRatio).TruncateInt()
        k.bankKeeper.BurnCoins(ctx, ModuleAccount, sdk.NewCoins(sdk.NewCoin(pool.Denom, burnAmount)))
        pool = sdk.NewCoin(pool.Denom, params.MaxRewardPool.Amount.Add(overflow.Sub(burnAmount)))
        k.SetRewardPool(ctx, pool)
        k.EmitEvent(ctx, EventRewardPoolOverflow{BurnedAmount: burnAmount.String()})
    }

    // ============================================
    // PHASE 3: Epoch-Based Reward Distribution
    // ============================================

    if !k.IsRewardEpoch(ctx) {
        return
    }

    currentEpoch := k.GetCurrentRewardEpoch(ctx)

    if pool.IsZero() {
        k.ResetEpochMetrics(ctx, currentEpoch)
        return
    }

    // Calculate accuracy-weighted scores for eligible sentinels
    // IMPORTANT: Accuracy is based ONLY on resolved appeals, NOT unchallenged hides/locks
    // Both hiding posts and locking threads contribute to the unified accuracy score
    type eligibleSentinel struct {
        Address       string
        AccuracyScore sdk.Dec
    }
    var eligibleSentinels []eligibleSentinel
    totalScore := sdk.ZeroDec()

    for _, sentinel := range k.GetAllSentinels(ctx) {
        // ANTI-GAMING: Require minimum resolved appeals before accuracy counts
        // Combines both hide appeals and lock appeals
        totalHideDecisions := sentinel.UpheldHides + sentinel.OverturnedHides
        totalLockDecisions := sentinel.UpheldLocks + sentinel.OverturnedLocks
        totalDecided := totalHideDecisions + totalLockDecisions
        if totalDecided < params.MinAppealsForAccuracy {
            continue // Not enough data to determine accuracy
        }

        // ANTI-GAMING: Require minimum activity in THIS epoch
        // Either hiding or locking counts toward activity requirement
        epochActivity := sentinel.EpochHides + sentinel.EpochLocks
        if epochActivity < params.MinEpochActivityForReward {
            continue // Inactive this epoch, no reward
        }

        // ANTI-GAMING: Require minimum appeal rate (prevents targeting non-appellants)
        // Only hides that got appealed count toward meaningful accuracy
        // Note: Lock appeal rate is not checked separately since locks are rate-limited (max 5/epoch)
        if sentinel.EpochHides > 0 {
            appealRate := sdk.NewDec(int64(sentinel.EpochAppealsFiled)).Quo(sdk.NewDec(int64(sentinel.EpochHides)))
            if appealRate.LT(params.MinAppealRate) {
                continue // Not enough of this sentinel's hides are being contested
            }
        }

        // Calculate unified accuracy rate (ONLY from resolved appeals)
        // Combines upheld hides AND upheld locks in numerator
        totalUpheld := sentinel.UpheldHides + sentinel.UpheldLocks
        accuracyRate := sdk.NewDec(int64(totalUpheld)).Quo(sdk.NewDec(int64(totalDecided)))

        // Must meet minimum accuracy threshold (e.g., 70%)
        if accuracyRate.LT(params.MinSentinelAccuracy) {
            continue // Below threshold, no reward
        }

        // Score = accuracy_rate * sqrt(epoch_appeals_resolved)
        // Using epoch appeals (not total hides) to prevent gaming by targeting non-appellants
        sqrtAppeals := sdk.NewDec(int64(sentinel.EpochAppealsResolved)).ApproxSqrt()
        score := accuracyRate.Mul(sqrtAppeals)

        // Add small bonus for participation (capped)
        // Includes both hides and locks (locks weighted higher due to stricter requirements)
        hideBonus := sdk.NewDec(int64(sentinel.EpochHides)).Mul(sdk.NewDecWithPrec(1, 2))   // 0.01 per hide
        lockBonus := sdk.NewDec(int64(sentinel.EpochLocks)).Mul(sdk.NewDecWithPrec(5, 2))   // 0.05 per lock (5x weight)
        participationBonus := sdk.MinDec(
            hideBonus.Add(lockBonus),
            sdk.NewDecWithPrec(5, 1), // Max 0.5 bonus
        )
        score = score.Add(participationBonus)

        eligibleSentinels = append(eligibleSentinels, eligibleSentinel{
            Address:       sentinel.Address,
            AccuracyScore: score,
        })
        totalScore = totalScore.Add(score)
    }

    if totalScore.IsZero() {
        k.ResetEpochMetrics(ctx, currentEpoch)
        return // No eligible sentinels
    }

    // Distribute rewards with PER-SENTINEL CAP
    distributed := sdk.ZeroInt()
    maxPerSentinel := pool.Amount.ToLegacyDec().Mul(params.MaxSentinelRewardShare).TruncateInt()

    // ============================================
    // PRE-CALCULATE DREAM SCALING FACTOR (for fair distribution)
    // ============================================
    // If total DREAM rewards would exceed cap, scale ALL rewards equally
    // This prevents unfairness by address ordering
    //
    // IMPORTANT: Use SNAPSHOTTED eligibility count from epoch start, not current count
    // This ensures the scaling factor was predictable at the start of the epoch
    // and prevents mid-epoch eligibility gaming
    baseDreamReward := sdk.NewIntFromString(params.SentinelDreamReward)
    maxMintPerEpoch := sdk.NewIntFromString(params.MaxDreamMintPerEpoch)
    snapshotEligibleCount := len(k.GetEpochEligibleSentinels(ctx))
    totalEligibleDream := baseDreamReward.Mul(sdk.NewInt(int64(snapshotEligibleCount)))

    var dreamScaleFactor sdk.Dec
    if totalEligibleDream.GT(maxMintPerEpoch) {
        // Scale down: each sentinel gets (baseDreamReward * cap / total)
        dreamScaleFactor = maxMintPerEpoch.ToLegacyDec().Quo(totalEligibleDream.ToLegacyDec())
        k.EmitEvent(ctx, EventDreamMintCapReached{
            EpochMinted:       totalEligibleDream.String(),
            EpochCap:          maxMintPerEpoch.String(),
            SentinelsAffected: uint64(len(eligibleSentinels)),
            ScaleFactor:       dreamScaleFactor.String(),
        })
    } else {
        dreamScaleFactor = sdk.OneDec() // No scaling needed
    }

    for _, s := range eligibleSentinels {
        // Calculate proportional share
        share := pool.Amount.ToLegacyDec().Mul(s.AccuracyScore).Quo(totalScore).TruncateInt()

        // ANTI-MONOPOLY: Cap individual reward
        if share.GT(maxPerSentinel) {
            share = maxPerSentinel
        }

        if share.IsPositive() {
            sentinel := k.GetSentinelActivity(ctx, s.Address)
            minBond := sdk.NewIntFromString(params.MinSentinelBond)
            currentBond := sdk.NewIntFromString(sentinel.CurrentBond)

            // ============================================
            // SPARK REWARD: Always paid out directly
            // ============================================
            k.bankKeeper.SendCoins(ctx, ModuleAccount, sdk.AccAddress(s.Address), sdk.NewCoins(sdk.NewCoin(pool.Denom, share)))
            distributed = distributed.Add(share)

            // ============================================
            // DREAM REWARD: Minted for moderation work (with epoch cap, PRO-RATA SCALED)
            // ============================================
            // Fixed DREAM reward per epoch for eligible sentinels (e.g., 10 DREAM)
            baseDreamReward := sdk.NewIntFromString(params.SentinelDreamReward)

            // PRO-RATA SCALING: If total eligible rewards exceed cap, scale all rewards equally
            // This prevents unfairness where early-processed sentinels get full rewards
            // and late-processed sentinels get nothing.
            //
            // Scaling is pre-calculated before this loop:
            //   totalEligibleDream = baseDreamReward * len(eligibleSentinels)
            //   if totalEligibleDream > maxMintPerEpoch:
            //       dreamScaleFactor = maxMintPerEpoch / totalEligibleDream (< 1.0)
            //   else:
            //       dreamScaleFactor = 1.0
            //
            // Each sentinel gets: baseDreamReward * dreamScaleFactor
            dreamReward := baseDreamReward.ToLegacyDec().Mul(dreamScaleFactor).TruncateInt()

            if dreamReward.IsPositive() {
                // Track minted amount
                epochMinted := k.GetEpochDreamMinted(ctx)
                k.SetEpochDreamMinted(ctx, epochMinted.Add(dreamReward))

                if sentinel.BondStatus == SENTINEL_BOND_STATUS_RECOVERY {
                    // RECOVERY MODE: Auto-bond DREAM until restored
                    deficit := minBond.Sub(currentBond)
                    autoBond := sdk.MinInt(dreamReward, deficit)
                    payout := dreamReward.Sub(autoBond)

                    // Add auto-bonded amount to sentinel's bond
                    newBond := currentBond.Add(autoBond)
                    sentinel.CurrentBond = newBond.String()

                    // Mint the auto-bonded DREAM directly to module (locked as bond)
                    if autoBond.IsPositive() {
                        k.repKeeper.MintDREAM(ctx, ModuleAccount, autoBond)
                    }

                    // Check if bond is now restored
                    if newBond.GTE(minBond) {
                        sentinel.BondStatus = SENTINEL_BOND_STATUS_NORMAL
                        k.EmitEvent(ctx, EventSentinelBondRestored{
                            Sentinel: s.Address,
                            NewBond:  newBond.String(),
                        })
                    }

                    // Mint and pay out remaining DREAM (if any)
                    if payout.IsPositive() {
                        k.repKeeper.MintDREAM(ctx, s.Address, payout)
                    }

                    k.EmitEvent(ctx, EventDreamRewardAutoBonded{
                        Sentinel:   s.Address,
                        AutoBonded: autoBond.String(),
                        PaidOut:    payout.String(),
                        NewBond:    newBond.String(),
                    })
                } else {
                    // NORMAL MODE: Mint DREAM directly to sentinel
                    k.repKeeper.MintDREAM(ctx, s.Address, dreamReward)
                }
            }

            // Update cumulative rewards for tracking (SPARK only)
            sentinel.CumulativeRewards = sdk.NewIntFromString(sentinel.CumulativeRewards).Add(share).String()
            sentinel.LastRewardEpoch = currentEpoch
            k.SetSentinelActivity(ctx, sentinel)
        }
    }

    // Clear distributed amount from pool (keep dust + undistributed cap overflow for next epoch)
    k.SetRewardPool(ctx, sdk.NewCoin(pool.Denom, pool.Amount.Sub(distributed)))

    // Reset epoch metrics for all sentinels
    k.ResetEpochMetrics(ctx, currentEpoch)

    k.EmitEvent(ctx, EventRewardsDistributed{
        TotalDistributed:     distributed.String(),
        SentinelCount:        uint64(len(eligibleSentinels)),
        Epoch:                currentEpoch,
        UndistributedCarried: pool.Amount.Sub(distributed).String(),
    })
}

// ResetEpochMetrics resets per-epoch counters for all sentinels and applies accuracy decay
func (k Keeper) ResetEpochMetrics(ctx sdk.Context, newEpoch int64) {
    params := k.GetParams(ctx)

    for _, sentinel := range k.GetAllSentinels(ctx) {
        // Track activity for this epoch
        epochActivity := sentinel.EpochHides + sentinel.EpochLocks

        if epochActivity > 0 {
            // Active this epoch - reset inactivity counter
            sentinel.LastActiveEpoch = newEpoch
            sentinel.ConsecutiveInactiveEpochs = 0
        } else {
            // Inactive this epoch - increment counter
            sentinel.ConsecutiveInactiveEpochs++

            // Apply accuracy decay if enabled and past grace period
            if params.AccuracyDecayEnabled &&
               sentinel.ConsecutiveInactiveEpochs > params.AccuracyDecayGraceEpochs {

                inactiveEpochs := sentinel.ConsecutiveInactiveEpochs - params.AccuracyDecayGraceEpochs

                if inactiveEpochs >= params.AccuracyDecayMaxEpochs {
                    // Full reset after max decay epochs
                    k.EmitEvent(ctx, EventSentinelAccuracyReset{
                        Sentinel:         sentinel.Address,
                        InactiveEpochs:   sentinel.ConsecutiveInactiveEpochs,
                        PreviousUpheld:   sentinel.UpheldHides + sentinel.UpheldLocks,
                        PreviousOverturned: sentinel.OverturnedHides + sentinel.OverturnedLocks,
                    })
                    sentinel.UpheldHides = 0
                    sentinel.OverturnedHides = 0
                    sentinel.UpheldLocks = 0
                    sentinel.OverturnedLocks = 0
                } else {
                    // SYMMETRIC DECAY: Both upheld and overturned decay at same rate
                    // This preserves accuracy ratio while reducing total history
                    // Rationale: prevents gaming where sentinel could exploit asymmetric decay
                    // to manipulate their ratio through timed inactivity
                    decayRate := params.AccuracyDecayRate           // e.g., 10%
                    retainRate := sdk.OneDec().Sub(decayRate)

                    oldUpheldHides := sentinel.UpheldHides
                    oldUpheldLocks := sentinel.UpheldLocks
                    oldOverturnedHides := sentinel.OverturnedHides
                    oldOverturnedLocks := sentinel.OverturnedLocks

                    // Decay all counts at same rate (preserves ratio)
                    sentinel.UpheldHides = uint64(sdk.NewDec(int64(sentinel.UpheldHides)).Mul(retainRate).TruncateInt64())
                    sentinel.UpheldLocks = uint64(sdk.NewDec(int64(sentinel.UpheldLocks)).Mul(retainRate).TruncateInt64())
                    sentinel.OverturnedHides = uint64(sdk.NewDec(int64(sentinel.OverturnedHides)).Mul(retainRate).TruncateInt64())
                    sentinel.OverturnedLocks = uint64(sdk.NewDec(int64(sentinel.OverturnedLocks)).Mul(retainRate).TruncateInt64())

                    // Accuracy ratio preserved during decay:
                    // Example: 80 upheld / 20 overturned = 80% accuracy
                    // After 1 epoch (10% decay): 72 upheld / 18 overturned = 80% accuracy
                    // After 5 epochs: 47 upheld / 12 overturned = 80% accuracy (roughly)
                    // Total history shrinks but ratio stable - sentinel must stay active to maintain stats

                    if oldUpheldHides != sentinel.UpheldHides || oldUpheldLocks != sentinel.UpheldLocks ||
                       oldOverturnedHides != sentinel.OverturnedHides || oldOverturnedLocks != sentinel.OverturnedLocks {
                        k.EmitEvent(ctx, EventSentinelAccuracyDecay{
                            Sentinel:       sentinel.Address,
                            InactiveEpochs: sentinel.ConsecutiveInactiveEpochs,
                            DecayRate:      decayRate.String(),
                            UpheldHidesDecayed: oldUpheldHides - sentinel.UpheldHides,
                            UpheldLocksDecayed: oldUpheldLocks - sentinel.UpheldLocks,
                            OverturnedHidesDecayed: oldOverturnedHides - sentinel.OverturnedHides,
                            OverturnedLocksDecayed: oldOverturnedLocks - sentinel.OverturnedLocks,
                        })
                    }
                }
            }
        }

        // Reset epoch counters
        sentinel.EpochHides = 0
        sentinel.EpochAppealsResolved = 0
        sentinel.EpochAppealsFiled = 0
        sentinel.EpochLocks = 0
        k.SetSentinelActivity(ctx, sentinel)
    }
    k.SetCurrentRewardEpoch(ctx, newEpoch+1)
    k.SetEpochDreamMinted(ctx, sdk.ZeroInt()) // Reset DREAM mint counter for new epoch

    // ============================================
    // PHASE 5: Snapshot Eligibility for Next Epoch (DREAM Scaling Fairness)
    // ============================================
    // Snapshot eligible sentinels at epoch START so DREAM scaling factor is predictable
    // This prevents mid-epoch eligibility changes from affecting reward calculations
    var eligibleAddresses []string
    for _, sentinel := range k.GetAllSentinels(ctx) {
        if k.CheckSentinelEligibilityForNextEpoch(ctx, sentinel, params) {
            eligibleAddresses = append(eligibleAddresses, sentinel.Address)
        }
    }
    k.SetEpochEligibleSentinels(ctx, eligibleAddresses)
}
```

---

### 7.3. Governance & Emergency Messages

#### `MsgUpdateParams`

Updates all module parameters. Governance (x/gov) authority only.

| Field | Type | Description |
|-------|------|-------------|
| `authority` | `string` | Must match x/gov module account |
| `params` | `Params` | Full parameter set (all fields required) |

**Authorization:** x/gov module account (governance proposal).

---

#### `MsgUpdateOperationalParams`

Updates operational parameters only (excludes emergency pauses). Authorized for Commons Council Operations Committee.

| Field | Type | Description |
|-------|------|-------------|
| `authority` | `string` | Governance authority, Commons Council policy, or Operations Committee member |
| `operational_params` | `ForumOperationalParams` | Operational parameter subset (all fields required) |

**Authorization:** Via `CommonsKeeper.IsCouncilAuthorized(ctx, addr, "commons", "operations")`.
**Excluded fields:** `forum_paused`, `moderation_paused`, `appeals_paused` (governance-only).

---

#### `MsgSetForumPaused`

Emergency pause/unpause the forum. HR Committee only.

| Field | Type | Description |
|-------|------|-------------|
| `authority` | `string` | Must match HR Committee Address |
| `paused` | `bool` | True to pause, false to unpause |

**Authorization:** HR Committee only.
**Effect:** When paused, all content creation and moderation is blocked. Queries still work.

---

#### `MsgSetModerationPaused`

Pause/unpause sentinel actions only (posts still allowed).

| Field | Type | Description |
|-------|------|-------------|
| `authority` | `string` | Must match HR Committee Address |
| `paused` | `bool` | True to pause moderation, false to unpause |

**Authorization:** HR Committee only.
**Effect:** Sentinels cannot hide posts. Appeals and existing queues continue processing.

---

#### `MsgReportTag`

Report a problematic tag for HR Committee review.

| Field | Type | Description |
|-------|------|-------------|
| `reporter` | `string` | Reporter address (signer) |
| `tag_name` | `string` | Tag to report |
| `reason` | `string` | Reason for report |

**Cost:** Deposit `tag_report_bond` (SPARK) + Standard Gas Fee.

**Logic:**
1. Fail if tag doesn't exist
2. Fail if reporter already reported this tag
3. Create/update `TagReport`:
   - Add reporter to `reporters` list
   - Add bond to `total_bond`
4. If `len(reporters) >= tag_report_threshold`:
   - Set `under_review = true`
   - Emit `EventTagFlaggedForReview`
5. Emit `EventTagReported`

**Bond Return:** If HR Committee dismisses report, bonds returned. If tag removed, bonds returned + bonus from tag creator fee pool.

---

#### `MsgResolveTagReport`

HR Committee resolves a tag report.

| Field | Type | Description |
|-------|------|-------------|
| `authority` | `string` | Must match HR Committee Address |
| `tag_name` | `string` | Reported tag |
| `action` | `uint32` | 0 = dismiss (tag stays), 1 = remove tag, 2 = add to reserved_tags |
| `reserve_authority` | `string` | (Optional, action=2 only) Address that can use the reserved tag (empty = HR Committee only) |
| `reserve_members_can_use` | `bool` | (Optional, action=2 only) If true, members of reserve_authority group can also use the tag |

**Authorization:** HR Committee only.

**Logic:**
- **Dismiss (0):**
  - **False Report Penalty:** Calculate `burn_amount = total_bond * dismissed_report_burn_ratio`
  - Burn `burn_amount` from escrowed bonds
  - Return remaining bonds (`total_bond - burn_amount`) to reporters, split proportionally
  - Clear `TagReport`
  - Emit `EventTagReportResolved{Action: 0, BurnedAmount: burn_amount}`
- **Remove (1):** Delete tag, return all bonds (no penalty), distribute bonus to reporters from Reward Pool (replacing the burned creation fee).
- **Reserve (2):** Add to `reserved_tags`, remove tag from active use, return all bonds (no penalty).
  - Create `ReservedTag{Name: tag_name, Authority: reserve_authority, MembersCanUse: reserve_members_can_use}`
  - Append to `params.reserved_tags`
  - **Auto-flag grandfathered posts for sentinel review:**
    - Query `posts_by_tag/{tag_name}` to get all posts using this tag
    - For each post where `post.created_at < now` (pre-existing):
      - Add to `reserved_tag_review_queue/{tag_name}/{post_id}`
      - Emit `EventPostFlaggedForReservedTagReview{PostID: post_id, Tag: tag_name}`
    - *This ensures sentinels have visibility into potentially misleading posts without automatic hiding*
  - **Tag squatting guidance:** Posts using a now-reserved tag that predate the reservation may be misleading (e.g., "[official]" on non-committee posts). Sentinels SHOULD review flagged posts and MAY hide them as misinformation. Authors can appeal if the post content is legitimate despite the tag.
  - Emit `EventTagReserved{TagName: tag_name, Authority: reserve_authority, MembersCanUse: reserve_members_can_use, FlaggedPostCount: len(posts)}`

---

#### `MsgBondRole` (role_type = `ROLE_TYPE_FORUM_SENTINEL`)

> **Phase 1–4 bonded-role generalization:** the former forum-local `MsgBondSentinel` has been subsumed by x/rep's generic `MsgBondRole`. Forum no longer owns a bonding message; bonding flows through rep's role-typed endpoint.

Sentinel bonds DREAM to become a forum moderator (or adds to existing bond). Invoked as a standard `tx rep bond-role ROLE_TYPE_FORUM_SENTINEL <amount>`.

| Field | Type | Description |
|-------|------|-------------|
| `creator` | `string` | Sentinel address (signer) |
| `role_type` | `RoleType` | `ROLE_TYPE_FORUM_SENTINEL` |
| `amount` | `string` | DREAM amount to bond |

**Logic** (implemented in rep, enforced against the forum-owned `BondedRoleConfig(ROLE_TYPE_FORUM_SENTINEL)`):
1. Verify caller meets reputation tier requirement (`min_rep_tier` on the role config, seeded from forum's `min_sentinel_rep_tier` via write-through).
2. **Check demotion cooldown (prevents accuracy reset attack):**
   - Load existing `BondedRole(ROLE_TYPE_FORUM_SENTINEL, addr)` if present.
   - If `demotion_cooldown_until > now`: Fail with `ErrDemotionCooldown`.
   - *This prevents: get slashed to DEMOTED → unbond all → immediately re-bond with fresh stats.*
3. Lock DREAM via rep's `LockDREAM` (author-bond pattern: moves from available balance to staked).
4. Load or create `BondedRole(ROLE_TYPE_FORUM_SENTINEL, addr)` record.
5. Add amount to `current_bond`.
6. Update `bond_status` based on thresholds (computed from the role's `BondedRoleConfig`):
   - If `current_bond >= min_bond`: `BONDED_ROLE_STATUS_NORMAL`.
   - If `current_bond >= demotion_threshold`: `BONDED_ROLE_STATUS_RECOVERY`.
   - Otherwise: `BONDED_ROLE_STATUS_DEMOTED` (should not happen on first bond since rep rejects first bonds below `min_bond`).
7. Emit `bonded_role_bonded` event.

**Note:** Initial bond must be ≥ the forum-seeded `min_sentinel_bond` (default 1000 DREAM) to become sentinel. If sentinel was previously demoted, must wait `sentinel_demotion_cooldown` (default 7d) before re-bonding.

---

#### `MsgUnbondRole` (role_type = `ROLE_TYPE_FORUM_SENTINEL`)

> **Phase 1–4 bonded-role generalization:** the former forum-local `MsgUnbondSentinel` has been subsumed by x/rep's generic `MsgUnbondRole`.

Sentinel withdraws bonded DREAM (exits the sentinel role or reduces bond at loss). Invoked as `tx rep unbond-role ROLE_TYPE_FORUM_SENTINEL <amount>`.

| Field | Type | Description |
|-------|------|-------------|
| `creator` | `string` | Sentinel address (signer) |
| `role_type` | `RoleType` | `ROLE_TYPE_FORUM_SENTINEL` |
| `amount` | `string` | DREAM amount to unbond |

**Logic** (implemented in rep, with forum-side active-appeal checks enforced via the `total_committed_bond` reservation model):
1. Load `BondedRole(ROLE_TYPE_FORUM_SENTINEL, addr)`.
2. Fail with `ErrBondedRoleNotFound` if not a sentinel.
3. **Extended Appeal Window Check (prevents bounty sniping):**
   - rep blocks unbond amounts that exceed `current_bond - total_committed_bond` (i.e. any bond currently reserved against a pending hide/lock/move or an active appeal window).
   - forum's `MsgHideContent` / `MsgLockThread` / `MsgMoveThread` call `ReserveBond`; the reservation is only released when the action ages out unchallenged or the appeal resolves. Sentinel cannot escape accountability mid-appeal because the committed portion of the bond is non-withdrawable.
   - *Without this: hide post → unbond immediately → appeal filed later → sentinel escapes with bounty damage done.*
4. `UnlockDREAM` the amount back to the sentinel's available balance.
5. Update `current_bond` and `bond_status`:
   - If remaining bond < `demotion_threshold`:
     - Set `BONDED_ROLE_STATUS_DEMOTED`.
     - **Set demotion cooldown:** `demotion_cooldown_until = now + demotion_cooldown`.
     - *This prevents immediate re-bonding to reset accuracy stats.*
   - If remaining bond < `min_bond`: set `BONDED_ROLE_STATUS_RECOVERY`.
   - If remaining bond == 0: the `BondedRole` record persists (it's how we track `demotion_cooldown_until` for cooldown enforcement).
6. Emit `bonded_role_unbonded` event.

**Note:** Unbonding while in RECOVERY mode means voluntarily taking a loss. This is allowed but the sentinel loses privileges until they re-bond to minimum.

**Security Note:** When a sentinel is demoted (whether via slashing or voluntary unbonding below threshold), a cooldown period is enforced before re-bonding. This prevents the "accuracy reset attack" where a bad actor: builds accuracy → gets slashed → unbonds → re-bonds with clean accuracy.

**Security Note:** The extended appeal window check ensures sentinels cannot escape accountability by unbonding before appeals are filed. The `appeal_window` (default: appeal_deadline - hide_appeal_cooldown) represents the maximum time an appellant has to file.

---

#### `MsgReportMember`

Sentinel reports a member for pattern-based misconduct (escalation to HR Committee).

| Field | Type | Description |
|-------|------|-------------|
| `sentinel` | `string` | Reporting sentinel address (signer) |
| `member` | `string` | Member being reported |
| `evidence_post_ids` | `[]uint64` | Post IDs demonstrating pattern (min 3) |
| `reason` | `string` | Detailed explanation of pattern |
| `recommended_action` | `uint32` | 0 = warning, 1 = demotion, 2 = zeroing |

**Cost:** Deposit `member_report_bond` (SPARK) + Standard Gas Fee.

**Authorization:**
- Sentinel must meet standard sentinel requirements (tier, bond, backing)
- Sentinel cannot have outstanding slashing debt
- Sentinel cannot report themselves

**Validation:**
1. Fail with `ErrInsufficientEvidence` if `len(evidence_post_ids) < min_evidence_posts` (default 3)
2. For each evidence post:
   - Fail with `ErrPostNotFound` if post doesn't exist
   - Fail with `ErrInvalidEvidence` if post author != reported member
   - Fail with `ErrInvalidEvidence` if post was restored on appeal (sentinel was wrong)
3. Fail with `ErrMemberReportExists` if active report already exists for this member

**Logic:**
1. Create `MemberReport`:
   - `member`: reported member address
   - `reporters`: [sentinel] (list, allows co-signing)
   - `evidence_post_ids`: provided post IDs
   - `reason`: provided reason
   - `recommended_action`: provided recommendation
   - `total_bond`: deposited bond
   - `created_at`: now
   - `status`: PENDING
2. Store `report_filed_at/{member} = now` (for min_report_duration enforcement)
3. Emit `EventMemberReported`

**Co-Signing:** Other sentinels can add their support via `MsgCoSignMemberReport`, adding their bond and strengthening the case.

---

#### `MsgCoSignMemberReport`

Additional sentinel adds support to an existing member report.

| Field | Type | Description |
|-------|------|-------------|
| `sentinel` | `string` | Co-signing sentinel address (signer) |
| `member` | `string` | Member being reported |
| `additional_evidence` | `[]uint64` | Optional additional evidence post IDs |

**Cost:** Deposit `member_report_bond` (SPARK) + Standard Gas Fee.

**Logic:**
1. Load existing `MemberReport` for member
2. Fail with `ErrNoMemberReport` if none exists
3. Fail with `ErrAlreadyCoSigned` if sentinel already in reporters list
4. Add sentinel to `reporters` list
5. Add bond to `total_bond`
6. Append any new evidence posts (validated same as original)
7. If `len(reporters) >= member_report_cosign_threshold`:
   - Set `status = ESCALATED`
   - Emit `EventMemberReportEscalated`
8. Emit `EventMemberReportCoSigned`

---

#### `MsgResolveMemberReport`

HR Committee resolves a member report.

| Field | Type | Description |
|-------|------|-------------|
| `authority` | `string` | Must match HR Committee Address |
| `member` | `string` | Reported member |
| `action` | `uint32` | 0 = dismiss, 1 = warning, 2 = demotion, 3 = zeroing |
| `reason` | `string` | Explanation for decision |
| `signers` | `[]string` | (Required for action=3) Co-signers for quorum validation |

**Authorization:** HR Committee only.

**Validation:**
- Load `report_filed_at/{member}` timestamp
- Fail with `ErrReportTooRecent` if `now - report_filed_at < min_report_duration`
  - This ensures the member has adequate time to submit a defense
- **Check defense wait period:** If `defense_submitted_at/{member}` exists:
  - Fail with `ErrDefenseWaitRequired` if `now - defense_submitted_at < min_defense_wait`
  - This ensures HR Committee considers the defense before resolution
- **Destructive action safeguards (action == 3 zeroing):**
  - **Quorum requirement:** Fail with `ErrInsufficientQuorum` if `len(signers) < hr_destructive_action_quorum` (default 2)
    - Each signer must be verified as HR Committee member
    - Prevents single compromised member from zeroing others
  - **Time-lock check:** Fail with `ErrTimeLockNotExpired` if `report_filed_at + hr_destructive_action_delay > now`
    - Default delay: 48 hours after report filed (on top of min_report_duration)
    - Gives community time to organize appeals before irreversible action

**Logic:**
- **Dismiss (0):**
  - **False Report Penalty:** Calculate `burn_amount = total_bond * dismissed_report_burn_ratio`
  - Burn `burn_amount` from escrowed bonds
  - Return remaining bonds (`total_bond - burn_amount`) to reporters, split proportionally
  - Clear `MemberReport` and `report_filed_at/{member}`
  - Emit `EventMemberReportDismissed{BurnedAmount: burn_amount}`

- **Warning (1):**
  - Create formal warning record for member (visible in queries)
  - Return all bonds to reporters (no penalty - report was valid)
  - Clear `report_filed_at/{member}`
  - Emit `EventMemberWarned`

- **Demotion (2):**
  - Call `x/rep.DemoteMember(member)` to reduce trust tier
  - Return bonds to reporters + bonus from slashed member reputation
  - Clear `report_filed_at/{member}`
  - Emit `EventMemberDemoted`

- **Zeroing (3):**
  - Call `x/rep.ZeroMember(member)` to reset all reputation and DREAM
  - Return bonds to reporters + larger bonus
  - Member can restart with new invitation
  - Clear `report_filed_at/{member}`
  - Emit `EventMemberZeroed`

**Member Defense:** Before resolution, member can submit `MsgDefendMemberReport` with counter-evidence and explanation. HR Committee sees both sides. The `min_report_duration` (default 48h) ensures members have time to respond.

---

#### `MsgDefendMemberReport`

Member responds to a report against them.

| Field | Type | Description |
|-------|------|-------------|
| `member` | `string` | Member being reported (signer) |
| `defense` | `string` | Explanation/defense |
| `context_post_ids` | `[]uint64` | Posts providing context (e.g., provocation) |

**Cost:** Standard Gas Fee only (no bond - right to defend).

**Logic:**
1. Load `MemberReport` for member
2. Fail with `ErrNoMemberReport` if none exists
3. Fail with `ErrDefenseAlreadySubmitted` if member already defended
4. Store defense in `MemberReport.defense`
5. Store `defense_submitted_at/{member} = now`
6. Emit `EventMemberDefenseSubmitted`

---

### 7.4. Gov Action Appeals (Meta-Governance)

These messages allow members to challenge HR Committee decisions by escalating to a jury.

#### `MsgAppealGovAction`

Appeal an HR Committee decision to a jury.

| Field | Type | Description |
|-------|------|-------------|
| `appellant` | `string` | Address appealing the action (must be affected party) |
| `action_type` | `uint32` | Type of action being appealed (see GovActionType enum) |
| `action_target` | `string` | Target of original action (self for warnings/demotions, tag name, or thread root ID) |
| `appeal_reason` | `string` | Argument for why action was unjust |

**Cost:** Deposit `gov_action_appeal_fee` (SPARK) + Standard Gas Fee.

**Authorization:**
- For WARNING/DEMOTION/ZEROING: `appellant` must be the affected member
- For TAG_REMOVAL: `appellant` must be the original tag creator (Tier 2+)
- For FORUM_PAUSE: Any member can appeal if pause duration > `extended_pause_threshold`
- For THREAD_LOCK: `appellant` must be the thread author (original root post creator)
- For THREAD_MOVE: `appellant` must be the thread author (original root post creator)

**Validation:**
1. Fail with `ErrActionNotAppealable` if action type is not valid
2. Fail with `ErrNotAffectedParty` if appellant is not authorized to appeal
3. Fail with `ErrAppealWindowExpired` if action was taken more than 14 days ago
4. Fail with `ErrAppealAlreadyFiled` if active appeal exists for this action

**Logic:**
1. **Escrow Appeal Fee:** Transfer `gov_action_appeal_fee` from appellant to module account
2. **Create Initiative in x/rep:**
   - Type: `HR_ACTION_APPEAL` (Extended Jury)
   - Payload: `{ action_type, action_target, original_reason, appeal_reason, hr_committee_addr }`
   - Deadline: `now + gov_action_appeal_deadline`
3. **Create GovActionAppeal record:**
   - Store with unique ID
   - Set status to PENDING
4. Emit `EventGovActionAppealed`

---

#### `MsgResolveGovActionAppeal` (Callback from x/rep)

Called by x/rep when jury reaches verdict on Gov action appeal.

**Logic:**
- **UPHELD (HR wins):**
  - Original action remains in effect
  - Appellant forfeits appeal fee (50% burned, 50% to HR Committee)
  - No penalty to HR Committee
  - Emit `EventGovActionAppealUpheld`

- **OVERTURNED (Appellant wins):**
  - **Reverse the action:**
    - WARNING: Remove warning from member's record
    - DEMOTION: Restore member to previous tier via `x/rep.RestoreMemberTier`
    - ZEROING: Cannot be fully reversed (DREAM burned), but restore reputation and tier
    - TAG_REMOVAL: Restore tag with original creator
    - FORUM_PAUSE: Unpause forum immediately
    - THREAD_LOCK: Unlock thread (set `post.locked = false`, clear lock fields)
    - THREAD_MOVE: Restore thread to original category (requires storing original_category_id in appeal record)
  - Appeal fee refunded to appellant
  - **HR Committee consequences:**
    - **Reputation Penalty:** Each HR Committee member who signed the overturned action
      receives a reputation deduction via `x/rep.DeductHRActionPenalty(member, penalty_amount)`
      - Default: 50 reputation points per overturned action
      - This affects their standing in confidence votes and future jury eligibility
    - Emit `EventGovActionOverturned` (public record of overturned decision)
    - Emit `EventHRMemberPenalized{Member, PenaltyAmount}` for each affected HR member
    - If HR Committee has multiple overturned actions, x/commons confidence vote may trigger
  - Emit `EventGovActionAppealOverturned`

- **TIMEOUT:**
  - Default to upholding original action (HR Committee presumption)
  - Refund 50% of appeal fee to appellant
  - Emit `EventGovActionAppealTimedOut`

**Important:** Overturning an HR Committee decision is a significant event. The event log creates transparency and may trigger governance review of the HR Committee.

---

### 7.5. XP Integration via Hooks

> **Implementation status:** The ForumHooks interface is **not yet implemented**. The design below is the target specification for integration with x/season. Currently, x/forum emits standard Cosmos SDK events that can be indexed off-chain.

x/forum provides a hooks interface for external modules (like x/season) to respond to forum events. This decouples x/forum from XP logic - the forum module simply emits engagement signals, and x/season decides how to award XP.

#### ForumHooks Interface

```go
// x/forum/types/hooks.go

// ForumHooks defines the interface for modules to receive forum engagement events
type ForumHooks interface {
    // Called when a reply is created on a post
    // Receiver: x/season can award XP to post author for receiving engagement
    OnReplyReceived(ctx sdk.Context, postAuthor string, replier string, postID uint64)

    // Called when a post is marked as "helpful"
    // Receiver: x/season can award XP to post author for quality content
    OnPostMarkedHelpful(ctx sdk.Context, postAuthor string, marker string, postID uint64, isReply bool)

    // Called when a post is created
    // Receiver: modules can track activity metrics
    OnPostCreated(ctx sdk.Context, author string, postID uint64, isReply bool)

    // Called when a post is hidden by a sentinel
    // Receiver: modules can adjust author reputation
    OnPostHidden(ctx sdk.Context, author string, postID uint64, sentinel string)

    // Called when a hidden post is restored via appeal
    // Receiver: modules can restore author reputation
    OnPostRestored(ctx sdk.Context, author string, postID uint64)
}
```

#### Hooks Registration

```go
// x/forum/keeper/keeper.go

type Keeper struct {
    // ... other fields
    hooks []ForumHooks
}

func (k *Keeper) SetHooks(hooks ...ForumHooks) {
    k.hooks = hooks
}

// Helper to call all registered hooks
func (k Keeper) callHooks(ctx sdk.Context, fn func(ForumHooks)) {
    for _, hook := range k.hooks {
        fn(hook)
    }
}

// Example usage in keeper methods:
func (k Keeper) afterReplyCreated(ctx sdk.Context, post Post, replier string) {
    k.callHooks(ctx, func(h ForumHooks) {
        h.OnReplyReceived(ctx, post.Author, replier, post.ID)
    })
}
```

#### Design Rationale

| Principle | Explanation |
|-----------|-------------|
| **Loose coupling** | x/forum has no dependency on x/season. Forum works without XP. |
| **Single responsibility** | x/forum manages content; x/season manages gamification. |
| **Testability** | Forum can be tested with mock hooks or no hooks at all. |
| **Flexibility** | Multiple modules can hook into forum events. |

**Note:** All XP-related parameters (amounts, caps, anti-gaming rules) should be defined in x/season, not x/forum. The forum module simply signals "engagement happened" without prescribing rewards.

#### Events for Off-Chain Consumers

For off-chain systems that want to track engagement without implementing hooks:

```protobuf
// Emitted alongside hook calls for indexer/notification use
message EventEngagementSignal {
  string signal_type = 1;                                // "reply_received", "post_helpful", etc.
  string beneficiary = 2;                                // Address receiving the engagement
  string actor = 3;                                      // Address who performed the action
  uint64 post_id = 4;                                    // Related post ID
}
```

The legacy XP implementation code (with anti-gaming logic, state keys, parameters, and events) has been moved to x/season. This spec file previously contained the full implementation, but the hooks-based design above is the recommended approach for cleaner module separation.

---

## 8. Queries

### 8.1. Query Service

> **Implementation note:** The implemented query service follows Ignite's CRUD pattern with `Get{Entity}/List{Entity}` pairs for all 26 state objects, plus composite queries for common access patterns.

```protobuf
// proto/sparkdream/forum/v1/query.proto (implemented)
service Query {
  // ==========================================
  // CRUD Get/List Pairs (26 entities)
  // ==========================================
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);

  rpc GetPost / ListPost                                 // Post by ID / paginated list
  rpc GetCategory / ListCategory                         // Category by ID / paginated list
  rpc GetTag / ListTag                                   // Tag by name / paginated list (sparkdream.common.v1.Tag)
  rpc GetReservedTag / ListReservedTag                   // Reserved tag by name / paginated list
  rpc GetUserRateLimit / ListUserRateLimit               // Rate limit by address / paginated list
  rpc GetUserReactionLimit / ListUserReactionLimit       // Reaction limit by address / paginated list
  rpc GetSentinelActivity / ListSentinelActivity         // Sentinel by address / paginated list
  rpc GetHideRecord / ListHideRecord                     // Hide record by post_id / paginated list
  rpc GetThreadLockRecord / ListThreadLockRecord         // Lock record by root_id / paginated list
  rpc GetThreadMoveRecord / ListThreadMoveRecord         // Move record by root_id / paginated list
  rpc GetPostFlag / ListPostFlag                         // Post flag by post_id / paginated list
  rpc GetBounty / ListBounty                             // Bounty by ID / paginated list
  rpc GetTagBudget / ListTagBudget                       // Tag budget by ID / paginated list
  rpc GetTagBudgetAward / ListTagBudgetAward             // Award by ID / paginated list
  rpc GetThreadMetadata / ListThreadMetadata             // Thread metadata by thread_id / paginated list
  rpc GetThreadFollow / ListThreadFollow                 // Follow record by follower / paginated list
  rpc GetThreadFollowCount / ListThreadFollowCount       // Follower count by thread_id / paginated list
  rpc GetArchiveMetadata / ListArchiveMetadata           // Archive metadata by root_id / paginated list
  rpc GetTagReport / ListTagReport                       // Tag report by tag_name / paginated list
  rpc GetMemberSalvationStatus / ListMemberSalvationStatus  // Salvation status by address / paginated list
  rpc GetJuryParticipation / ListJuryParticipation       // Jury participation by juror / paginated list
  rpc GetMemberReport / ListMemberReport                 // Member report by member / paginated list
  rpc GetMemberWarning / ListMemberWarning               // Warning by ID / paginated list
  rpc GetGovActionAppeal / ListGovActionAppeal           // Gov appeal by ID / paginated list

  // ==========================================
  // Composite Queries (convenience/filtering)
  // ==========================================
  rpc Posts(QueryPostsRequest) returns (QueryPostsResponse);                    // Filter by category + status
  rpc Thread(QueryThreadRequest) returns (QueryThreadResponse);                 // Full thread tree from root
  rpc Categories(QueryCategoriesRequest) returns (QueryCategoriesResponse);      // All categories
  rpc UserPosts(QueryUserPostsRequest) returns (QueryUserPostsResponse);        // Posts by author
  rpc SentinelStatus(QuerySentinelStatusRequest) returns (QuerySentinelStatusResponse);  // Flat sentinel summary
  rpc SentinelBondCommitment(QuerySentinelBondCommitmentRequest) returns (QuerySentinelBondCommitmentResponse);
  rpc ArchiveCooldown(QueryArchiveCooldownRequest) returns (QueryArchiveCooldownResponse);
  rpc TagExists(QueryTagExistsRequest) returns (QueryTagExistsResponse);
  rpc TagReports(QueryTagReportsRequest) returns (QueryTagReportsResponse);     // Tags under review
  rpc ForumStatus(QueryForumStatusRequest) returns (QueryForumStatusResponse);  // Pause status + epoch
  rpc AppealCooldown(QueryAppealCooldownRequest) returns (QueryAppealCooldownResponse);
  rpc MemberReports(QueryMemberReportsRequest) returns (QueryMemberReportsResponse);
  rpc MemberWarnings(QueryMemberWarningsRequest) returns (QueryMemberWarningsResponse);  // Warnings by member
  rpc MemberStanding(QueryMemberStandingRequest) returns (QueryMemberStandingResponse);  // Aggregate standing
  rpc PinnedPosts(QueryPinnedPostsRequest) returns (QueryPinnedPostsResponse);
  rpc LockedThreads(QueryLockedThreadsRequest) returns (QueryLockedThreadsResponse);
  rpc ThreadLockStatus(QueryThreadLockStatusRequest) returns (QueryThreadLockStatusResponse);
  rpc TopPosts(QueryTopPostsRequest) returns (QueryTopPostsResponse);           // Sort by upvotes
  rpc ThreadFollowers(QueryThreadFollowersRequest) returns (QueryThreadFollowersResponse);
  rpc UserFollowedThreads(QueryUserFollowedThreadsRequest) returns (QueryUserFollowedThreadsResponse);
  rpc IsFollowingThread(QueryIsFollowingThreadRequest) returns (QueryIsFollowingThreadResponse);
  rpc BountyByThread(QueryBountyByThreadRequest) returns (QueryBountyByThreadResponse);
  rpc ActiveBounties(QueryActiveBountiesRequest) returns (QueryActiveBountiesResponse);
  rpc UserBounties(QueryUserBountiesRequest) returns (QueryUserBountiesResponse);
  rpc BountyExpiringSoon(QueryBountyExpiringSoonRequest) returns (QueryBountyExpiringSoonResponse);
  rpc TagBudgetByTag(QueryTagBudgetByTagRequest) returns (QueryTagBudgetByTagResponse);
  rpc TagBudgets(QueryTagBudgetsRequest) returns (QueryTagBudgetsResponse);
  rpc TagBudgetAwards(QueryTagBudgetAwardsRequest) returns (QueryTagBudgetAwardsResponse);
  rpc PostFlags(QueryPostFlagsRequest) returns (QueryPostFlagsResponse);
  rpc FlagReviewQueue(QueryFlagReviewQueueRequest) returns (QueryFlagReviewQueueResponse);
  rpc GovActionAppeals(QueryGovActionAppealsRequest) returns (QueryGovActionAppealsResponse);
}
```

| Query | Description |
|-------|-------------|
| **CRUD Pairs** | Standard `Get{Entity}` / `List{Entity}` for all 26 state objects (see above) |
| `Params` | Get module parameters |
| `Posts` | List posts with pagination (filter by category, status) |
| `Thread` | Get full thread tree from root ID |
| `Categories` | List all categories |
| `UserPosts` | List posts by author address |
| `SentinelStatus` | Get sentinel summary (flat response: address, bond_status, current_bond, accuracy_rate) |
| `SentinelBondCommitment` | Get sentinel's committed vs available bond (flat: current_bond, total_committed_bond, available_bond) |
| `TagExists` | Check if a tag exists (includes expiration_time) |
| `TagReports` | List all tags under review with pagination |
| `ForumStatus` | Get forum pause status and current epoch |
| `ArchiveCooldown` | Check if thread is in archive cooldown period |
| `AppealCooldown` | Check if hidden post is in appeal cooldown period |
| `MemberReports` | List all pending/escalated member reports |
| `MemberWarnings` | Get all warnings issued to a member |
| `MemberStanding` | Get member's full standing (warning_count, active_report, trust_tier) |
| `PinnedPosts` | Get all pinned posts for a category, sorted by priority |
| `LockedThreads` | List all currently locked threads (flat: root_id, locked_by, locked_at) |
| `ThreadLockStatus` | Check if thread is locked and get lock details |
| `TopPosts` | Get posts sorted by upvote count (filter by category, time range) |
| `ThreadFollowers` | Get addresses following a thread (paginated) |
| `UserFollowedThreads` | Get threads followed by a user (paginated) |
| `IsFollowingThread` | Check if a user is following a specific thread |
| `BountyByThread` | Get bounty for a specific thread |
| `ActiveBounties` | List all active bounties (paginated) |
| `UserBounties` | Get bounties created by a specific user |
| `BountyExpiringSoon` | List bounties expiring within a time window |
| `TagBudgetByTag` | Get tag budget for a specific tag |
| `TagBudgets` | List all tag budgets (paginated) |
| `TagBudgetAwards` | Get award history for a tag budget |
| `PostFlags` | Get flag status for a specific post (total_weight, in_review_queue, flagger_count) |
| `FlagReviewQueue` | Get posts in sentinel review queue (paginated) |
| `GovActionAppeals` | List all pending gov action appeals |

**Queries from spec not yet implemented:**

| Query | Status |
|-------|--------|
| `UserReaction` | Not implemented (no individual ReactionRecord tracking) |
| `PostReactions` | Not implemented |
| `UserReactions` | Not implemented |
| `HRActionHistory` | Not implemented (events only) |
| `SentinelMoveHistory` | Not implemented (use ListThreadMoveRecord) |
| `UserFlags` | Not implemented |
| `HasFlagged` | Not implemented |
| `ArchivedThreadMeta` | Replaced by `GetArchiveMetadata` |
| `ArchivedThreads` | Replaced by `ListArchiveMetadata` |

#### Content Visibility Policy (Censorship Resistance)

**IMPORTANT:** All queries return full post content regardless of status. This is a fundamental censorship resistance guarantee:

- **HIDDEN posts:** Content is fully visible via `Post` and `Thread` queries. The `status = HIDDEN` field indicates the post is under moderation review, but content remains accessible for transparency and appeals.

- **DELETED posts:** Content is replaced with "[deleted by author]" for author-initiated deletions. For appeal-rejected posts (jury ruled content violated policy), the original content is **preserved** in state with `status = DELETED`. This allows historical verification while clearly marking the post as policy-violating.

- **ARCHIVED posts:** Full content accessible via `MsgUnarchiveThread` or by decompressing the `ArchivedThread.compressed_data` blob directly.

**Rationale:** Frontends may choose to hide or de-emphasize content based on status, but the on-chain data layer never censors. Anyone running a node or querying the chain can access all content. This ensures:
1. Appeal evidence is always available
2. Moderation decisions can be audited
3. No content is permanently suppressed from the chain
4. Historical record remains intact

---

### 8.2. Thread Following Query Details

#### `QueryThreadFollowersRequest`

```protobuf
message QueryThreadFollowersRequest {
  uint64 thread_id = 1;                                  // Root post ID of thread
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QueryThreadFollowersResponse {
  repeated ThreadFollow followers = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
  uint64 total_count = 3;                                // Total follower count (cached)
}
```

#### `QueryUserFollowedThreadsRequest`

```protobuf
message QueryUserFollowedThreadsRequest {
  string user = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QueryUserFollowedThreadsResponse {
  repeated ThreadFollow followed_threads = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

#### `QueryIsFollowingThreadRequest`

```protobuf
message QueryIsFollowingThreadRequest {
  uint64 thread_id = 1;                                  // Root post ID of thread
  string user = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
}

message QueryIsFollowingThreadResponse {
  bool is_following = 1;
  int64 followed_at = 2;                                 // Timestamp when followed (0 if not following)
}
```

**Use Case:** Allows off-chain notification services to query who follows a thread, what threads a user follows, and check individual follow status. The `ThreadFollowCount` cache enables efficient follower count display without pagination.

---

### 8.3. Bounty Query Details

#### `QueryBountyExpiringSoonRequest`

```protobuf
message QueryBountyExpiringSoonRequest {
  int64  within_seconds = 1;                             // Time window - bounties expiring within this many seconds (e.g., 86400 = 24h)
  uint64 category_id = 2;                                // Filter by category (0 = all categories)
  string min_amount = 3;                                 // Min bounty amount filter (empty = no minimum)
  cosmos.base.query.v1beta1.PageRequest pagination = 4;
}

message QueryBountyExpiringSoonResponse {
  repeated Bounty bounties = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

**Use Case:** Allows frontends to display bounties that are about to expire, helping users discover opportunities before they close. Useful for "last chance" notifications and bounty hunters looking for quick opportunities.

---

## 9. Events

```protobuf
// Events emitted by x/forum

// Emitted when a new post is created
message EventPostCreated {
  uint64 post_id = 1;
  string author = 2;
  uint64 category_id = 3;
  uint64 parent_id = 4;
  bool   is_ephemeral = 5;
}

// Emitted when a post is hidden by a sentinel
message EventPostHidden {
  uint64 post_id = 1;
  string sentinel = 2;
  string reason = 3;
}

// Emitted when a post is restored after successful appeal
message EventPostRestored {
  uint64 post_id = 1;
  string appellant = 2;
}

// Emitted when a post is edited
message EventPostEdited {
  uint64 post_id = 1;
  string author = 2;
  int64  post_age_seconds = 3;                           // Seconds since post creation
  bool   within_grace_period = 4;                        // True if edit was free (within grace period)
  string fee_paid = 5;                                   // SPARK fee paid (0 if within grace period)
}

// Emitted when a post is deleted by its author
message EventPostDeleted {
  uint64 post_id = 1;
  string author = 2;
  bool   had_replies = 3;                                // True if post had replies (thread preserved)
  bool   bounty_cancelled = 4;                           // True if an active bounty was cancelled
}

// Emitted when an appeal is filed
message EventAppealFiled {
  uint64 post_id = 1;
  string appellant = 2;
  string sentinel = 3;
  uint64 initiative_id = 4;
}

// Emitted when a thread is archived
message EventThreadArchived {
  uint64 root_id = 1;
  uint64 post_count = 2;
  string archiver = 3;
}

// Emitted when an archived thread is restored
message EventThreadUnarchived {
  uint64 root_id = 1;
  uint64 post_count = 2;
  string unarchiver = 3;
}

// Emitted when ephemeral posts are pruned
message EventPostsPruned {
  repeated uint64 post_ids = 1;
}

// Emitted when sentinel rewards are distributed
message EventRewardsDistributed {
  string total_distributed = 1;
  uint64 sentinel_count = 2;
}

// Emitted when recursive salvation stops at depth limit
message EventPartialSalvation {
  uint64 post_id = 1;                                    // The reply that triggered salvation
  uint64 saved_count = 2;                                // Number of ancestors saved
  uint64 remaining_ephemeral = 3;                        // Number of ancestors still ephemeral
}

// Emitted when a hidden post expires without appeal
message EventHiddenPostExpired {
  uint64 post_id = 1;
  string sentinel = 2;                                   // Sentinel who hid it
  int64  hidden_duration = 3;                            // Seconds it was hidden
}

// Emitted when an appeal times out without jury verdict
message EventAppealTimedOut {
  uint64 post_id = 1;
  string outcome = 2;                                    // "restored_timeout"
  string appellant_refund = 3;                           // Amount refunded to appellant
  string burn_amount = 4;                                 // Amount burned
}

// Emitted when reward pool exceeds max and overflow is burned
message EventRewardPoolOverflow {
  string burned_amount = 1;
  string remaining_pool = 2;
}

// Emitted when a new tag is created
message EventTagCreated {
  string tag = 1;
  string creator = 2;
}

// Emitted when a tag is pruned due to expiration
message EventTagExpired {
  string tag = 1;
}

// ============================================
// PINNED AND LOCKED THREAD EVENTS
// ============================================

// Emitted when a post is pinned to a category
message EventPostPinned {
  uint64 post_id = 1;
  uint64 category_id = 2;
  string pinned_by = 3;                                  // HR Committee address
  uint32 priority = 4;                                   // Sort priority
}

// Emitted when a post is unpinned
message EventPostUnpinned {
  uint64 post_id = 1;
  uint64 category_id = 2;
  string unpinned_by = 3;                                // HR Committee address
}

// Emitted when a thread is locked
message EventThreadLocked {
  uint64 root_id = 1;
  string locked_by = 2;                                  // HR Committee address or Sentinel address
  string reason = 3;                                     // Lock reason (if provided)
  bool   is_sentinel_lock = 4;                           // True if locked by Sentinel (appealable)
}

// Emitted when a thread is unlocked
message EventThreadUnlocked {
  uint64 root_id = 1;
  string unlocked_by = 2;                                // HR Committee address or Sentinel address
}

// ============================================
// THREAD LOCK APPEAL EVENTS
// ============================================

// Emitted when a thread lock appeal is filed
message EventThreadLockAppealFiled {
  uint64 root_id = 1;
  string appellant = 2;                                  // Thread author
  string sentinel = 3;                                   // Sentinel who locked the thread
  uint64 initiative_id = 4;                              // x/rep initiative ID for jury resolution
}

// Emitted when a thread lock is overturned on appeal (appellant wins)
message EventThreadLockOverturned {
  uint64 root_id = 1;
  string appellant = 2;
  string sentinel = 3;
  string slash_amount = 4;                               // DREAM slashed from sentinel
}

// Emitted when a thread lock is upheld on appeal (sentinel wins)
message EventThreadLockUpheld {
  uint64 root_id = 1;
  string appellant = 2;
  string sentinel = 3;
  string fee_to_sentinel = 4;                            // SPARK awarded to sentinel
}

// Emitted when a thread lock appeal times out without jury verdict
message EventThreadLockAppealTimedOut {
  uint64 root_id = 1;
  string outcome = 2;                                    // "unlocked_timeout"
  string appellant_refund = 3;                           // Amount refunded to appellant
}

// ============================================
// THREAD MOVE EVENTS
// ============================================

// Emitted when a thread is moved to a different category
message EventThreadMoved {
  uint64 root_id = 1;
  uint64 original_category_id = 2;
  uint64 new_category_id = 3;
  string moved_by = 4;                                   // HR Committee address or Sentinel address
  string reason = 5;                                     // Move reason (if provided)
  bool   is_sentinel_move = 6;                           // True if moved by Sentinel (appealable)
}

// Emitted when a thread move appeal is filed
message EventThreadMoveAppealFiled {
  uint64 root_id = 1;
  string appellant = 2;                                  // Thread author
  string sentinel = 3;                                   // Sentinel who moved the thread
  uint64 original_category_id = 4;
  uint64 new_category_id = 5;
  uint64 initiative_id = 6;                              // x/rep initiative ID for jury resolution
}

// Emitted when a thread move is overturned on appeal (appellant wins)
message EventThreadMoveOverturned {
  uint64 root_id = 1;
  string appellant = 2;
  string sentinel = 3;
  uint64 restored_category_id = 4;                       // Thread returned to this category
  string slash_amount = 5;                               // DREAM slashed from sentinel
}

// Emitted when a thread move is upheld on appeal (sentinel wins)
message EventThreadMoveUpheld {
  uint64 root_id = 1;
  string appellant = 2;
  string sentinel = 3;
  string fee_to_sentinel = 4;                            // SPARK awarded to sentinel
}

// Emitted when a thread move appeal times out without jury verdict
message EventThreadMoveAppealTimedOut {
  uint64 root_id = 1;
  string outcome = 2;                                    // "restored_timeout" (thread restored to original category)
  string appellant_refund = 3;                           // Amount refunded to appellant
}

// ============================================
// POST FLAGGING EVENTS
// ============================================

// Emitted when a post is flagged
message EventPostFlagged {
  uint64 post_id = 1;
  string flagger = 2;
  uint64 flag_weight = 3;                                // Weight of this flag (member vs non-member)
  uint64 total_weight = 4;                               // Total flag weight after this flag
  bool   paid_spam_tax = 5;                              // True if flagger was non-member and paid tax
  uint32 category = 6;                                   // FlagCategory enum value
}

// Emitted when a post enters the sentinel review queue (threshold reached)
message EventPostEnteredReviewQueue {
  uint64 post_id = 1;
  uint64 total_weight = 2;                               // Total flag weight
  uint64 flagger_count = 3;                              // Number of unique flaggers
}

// Emitted when sentinel dismisses flags (post is fine)
message EventFlagsDismissed {
  uint64 post_id = 1;
  string sentinel = 2;
  uint64 flags_dismissed = 3;                            // Number of flags cleared
  string reason = 4;                                     // Sentinel's dismissal reason
}

// Emitted when flags expire without action
message EventFlagsExpired {
  uint64 post_id = 1;
  uint64 flags_expired = 2;                              // Number of flags that expired
}

// ============================================
// BOUNTY EVENTS
// ============================================

// Emitted when a bounty is created
message EventBountyCreated {
  uint64 bounty_id = 1;
  string creator = 2;
  uint64 thread_id = 3;
  string amount = 4;
  int64  expires_at = 5;
}

// Emitted when a bounty is awarded
message EventBountyAwarded {
  uint64 bounty_id = 1;
  string creator = 2;
  uint64 thread_id = 3;
  repeated BountyAward awards = 4;
}

// Emitted when a bounty amount is increased
message EventBountyIncreased {
  uint64 bounty_id = 1;
  string additional_amount = 2;
  string new_total = 3;
}

// Emitted when a bounty is cancelled
message EventBountyCancelled {
  uint64 bounty_id = 1;
  string refunded_amount = 2;
}

// Emitted when a bounty expires
message EventBountyExpired {
  uint64 bounty_id = 1;
  uint64 thread_id = 2;
  string burned_amount = 3;                              // Amount burned (expiry_burn_ratio)
  string top_reply_award = 4;                            // Amount awarded to top-upvoted reply (0 if no replies)
  uint64 top_reply_id = 5;                               // Winning reply ID (0 if no award)
}

// ============================================
// TAG BUDGET EVENTS
// ============================================

// Emitted when a tag budget is created
message EventTagBudgetCreated {
  uint64 budget_id = 1;
  string group_account = 2;
  string tag = 3;
  string initial_pool = 4;
  bool   members_only = 5;
}

// Emitted when an award is made from a tag budget
message EventTagBudgetAwarded {
  uint64 budget_id = 1;
  uint64 post_id = 2;
  string recipient = 3;
  string amount = 4;
  string reason = 5;
  string awarded_by = 6;
  string remaining_pool = 7;
}

// Emitted when a tag budget pool is topped up
message EventTagBudgetToppedUp {
  uint64 budget_id = 1;
  string added_amount = 2;
  string new_pool_balance = 3;
}

// Emitted when a tag budget is toggled active/inactive
message EventTagBudgetToggled {
  uint64 budget_id = 1;
  bool   active = 2;
}

// Emitted when a tag budget is withdrawn (remaining pool returned to group)
message EventTagBudgetWithdrawn {
  uint64 budget_id = 1;
  string withdrawn_amount = 2;
}

// Emitted when a bounty is cancelled due to thread moderation
message EventBountyModerationCancelled {
  uint64 bounty_id = 1;
  uint64 thread_id = 2;
  string moderation_type = 3;                            // "hidden", "locked", or "archived"
  string fee_burned = 4;                                 // Amount burned (moderation_fee_ratio)
  string refunded_amount = 5;                            // Amount refunded to creator
}

// ============================================
// PINNED & ACCEPTED REPLY EVENTS
// ============================================

// Emitted when a reply is pinned to the top of a thread
message EventReplyPinned {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string pinned_by = 3;                                  // Thread author or sentinel
  uint64 total_pinned = 4;                               // Total pinned replies after this action
  bool   is_sentinel_pin = 5;                            // True if sentinel pin (disputeable)
}

// Emitted when a reply is unpinned from a thread
message EventReplyUnpinned {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string unpinned_by = 3;
  uint64 total_pinned = 4;                               // Total pinned replies after this action
}

// Emitted when author disputes a sentinel's pin
message EventPinDisputed {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string author = 3;                                     // Thread author who disputed
  string sentinel = 4;                                   // Sentinel who pinned
  uint64 initiative_id = 5;                              // x/rep initiative for jury resolution
  string reason = 6;                                     // Author's dispute reason
}

// Emitted when pin dispute is upheld (author wins, pin removed)
message EventPinDisputeUpheld {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string author = 3;
  string sentinel = 4;
  string slash_amount = 5;                               // DREAM slashed from sentinel
  string refund_amount = 6;                              // Fee refunded to author
}

// Emitted when pin dispute is rejected (sentinel wins, pin remains)
message EventPinDisputeRejected {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string author = 3;
  string sentinel = 4;
  string reward_amount = 5;                              // DREAM reward to sentinel
}

// Emitted when a reply is marked as accepted answer (by author - immediate)
message EventAcceptedReplyMarked {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string marked_by = 3;                                  // Thread author
  string reply_author = 4;                               // Author of the accepted reply
}

// Emitted when accepted reply is changed to a different reply
message EventAcceptedReplyChanged {
  uint64 thread_id = 1;
  uint64 previous_reply_id = 2;
  uint64 new_reply_id = 3;
  string changed_by = 4;
}

// Emitted when accepted reply marking is cleared
message EventAcceptedReplyCleared {
  uint64 thread_id = 1;
  uint64 previous_reply_id = 2;
  string cleared_by = 3;
}

// Emitted when sentinel proposes an accepted reply (awaiting author confirmation)
message EventAcceptProposalCreated {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string sentinel = 3;                                   // Sentinel who proposed
  int64  auto_confirm_at = 4;                            // When proposal auto-confirms if no response
}

// Emitted when author confirms sentinel's proposal
message EventAcceptProposalConfirmed {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string sentinel = 3;                                   // Sentinel who proposed (gets reward)
  string author = 4;                                     // Author who confirmed
  string reward_amount = 5;                              // DREAM reward to sentinel
}

// Emitted when author rejects sentinel's proposal
message EventAcceptProposalRejected {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string sentinel = 3;                                   // Sentinel who proposed
  string author = 4;                                     // Author who rejected
  string reason = 5;                                     // Optional rejection reason
}

// Emitted when proposal auto-confirms due to author inactivity
message EventAcceptProposalAutoConfirmed {
  uint64 thread_id = 1;
  uint64 reply_id = 2;
  string sentinel = 3;                                   // Sentinel who proposed (gets reward)
  string reward_amount = 4;                              // DREAM reward to sentinel
}

// ============================================
// REACTION EVENTS
// ============================================

// Emitted when a post is upvoted (public reaction — voter identity visible)
message EventPostUpvoted {
  uint64 post_id = 1;
  string voter = 2;
  uint64 new_upvote_count = 3;                           // Total upvotes after this vote
  bool   paid_spam_tax = 4;                              // True if voter was non-member and paid tax
}

// Emitted when a post is downvoted (public reaction — voter identity visible)
message EventPostDownvoted {
  uint64 post_id = 1;
  string voter = 2;
  uint64 new_downvote_count = 3;                         // Total downvotes after this vote
  string deposit_burned = 4;                             // SPARK burned for this downvote
}

// REMOVED — EventAnonymousReaction no longer emitted by x/forum.
// Anonymous reactions are routed through x/shield's MsgShieldedExec, which emits
// its own shield-level events. The forum keeper emits standard EventUpvote/EventDownvote
// events when processing the dispatched message.

// ============================================
// ANTI-GAMING EVENTS
// ============================================

// Emitted when salvation is denied due to membership age
message EventSalvationDenied {
  uint64 post_id = 1;                                    // The reply post
  uint64 parent_id = 2;                                  // The ephemeral parent that was NOT saved
  string member = 3;                                     // Member who tried to salvage
  string reason = 4;                                     // "membership_too_new"
}

// Emitted when sentinel is slashed for losing appeal
message EventSentinelSlashed {
  string sentinel = 1;
  string slash_amount = 2;                               // DREAM amount slashed
  string new_bond = 3;                                   // Remaining bond after slash
  uint32 bond_status = 4;                                // New bond status (0=NORMAL, 1=RECOVERY, 2=DEMOTED)
}

// Emitted when sentinel enters recovery mode
message EventSentinelRecoveryMode {
  string sentinel = 1;
  string current_bond = 2;                               // Current bond amount
  string target_bond = 3;                                // Target bond to restore (min_sentinel_bond)
}

// Emitted when sentinel bond is restored via auto-bonding
message EventSentinelBondRestored {
  string sentinel = 1;
  string new_bond = 2;                                   // Restored bond amount
}

// Emitted when DREAM rewards are auto-bonded in recovery mode
message EventDreamRewardAutoBonded {
  string sentinel = 1;
  string auto_bonded = 2;                                // DREAM added to bond
  string paid_out = 3;                                   // DREAM paid to sentinel (overflow after bond restored)
  string new_bond = 4;                                   // New bond total
}

// Emitted when sentinel bonds DREAM
message EventSentinelBonded {
  string sentinel = 1;
  string amount = 2;                                     // DREAM bonded
  string new_bond = 3;                                   // Total bond after
  uint32 bond_status = 4;                                // New bond status
}

// Emitted when sentinel unbonds DREAM
message EventSentinelUnbonded {
  string sentinel = 1;
  string amount = 2;                                     // DREAM unbonded
  string remaining_bond = 3;                             // Remaining bond
  uint32 bond_status = 4;                                // New bond status
}

// Emitted when sentinel is demoted due to low bond
message EventSentinelDemoted {
  string sentinel = 1;
  string final_bond = 2;                                 // Bond at demotion
  string reason = 3;                                     // "bond_below_demotion_threshold" or "voluntary_unbond"
}

// Emitted when sentinel accuracy stats are decayed due to inactivity
// NOTE: Symmetric decay - both upheld and overturned decay at same rate to preserve ratio
message EventSentinelAccuracyDecay {
  string sentinel = 1;
  uint64 inactive_epochs = 2;                            // Total consecutive inactive epochs
  string decay_rate = 3;                                 // Upheld decay rate applied (e.g., "0.10")
  uint64 upheld_hides_decayed = 4;                       // Number of upheld hides lost to decay
  uint64 upheld_locks_decayed = 5;                       // Number of upheld locks lost to decay
  uint64 overturned_hides_decayed = 6;                   // Number of overturned hides lost to decay (half rate)
  uint64 overturned_locks_decayed = 7;                   // Number of overturned locks lost to decay (half rate)
}

// Emitted when sentinel accuracy stats are fully reset due to extended inactivity
message EventSentinelAccuracyReset {
  string sentinel = 1;
  uint64 inactive_epochs = 2;                            // Total consecutive inactive epochs at reset
  uint64 previous_upheld = 3;                            // Total upheld (hides + locks) before reset
  uint64 previous_overturned = 4;                        // Total overturned (hides + locks) before reset
}

// Emitted when forum is paused/unpaused
message EventForumPaused {
  bool   paused = 1;
  string authority = 2;                                  // HR Committee address
}

// Emitted when moderation is paused/unpaused
message EventModerationPaused {
  bool   paused = 1;
  string authority = 2;
}

// Emitted when a tag is reported
message EventTagReported {
  string tag = 1;
  string reporter = 2;
  uint64 total_reports = 3;
}

// Emitted when a tag reaches report threshold
message EventTagFlaggedForReview {
  string tag = 1;
  uint64 report_count = 2;
  string total_bond = 3;
}

// Emitted when HR Committee resolves a tag report
message EventTagReportResolved {
  string tag = 1;
  uint32 action = 2;                                     // 0=dismiss, 1=remove, 2=reserve
  string authority = 3;
}

// Emitted when a tag is reserved (action=2 in MsgResolveTagReport)
message EventTagReserved {
  string tag_name = 1;
  string tag_authority = 2;                              // Address that can use this tag (empty = HR Committee only)
  bool   members_can_use = 3;                            // Whether group members can also use the tag
}

// Emitted when EndBlocker GC runs
message EventGCCompleted {
  uint64 posts_pruned = 1;
  uint64 hidden_expired = 2;
  uint64 tags_expired = 3;
  uint64 rate_limits_cleaned = 4;
  int64  block_height = 5;
  uint64 archive_cooldowns_cleaned = 6;
  uint64 appeal_cooldowns_cleaned = 7;
}

// Emitted when sentinel is placed on cooldown after losing appeal
message EventSentinelCooldownApplied {
  string sentinel = 1;
  int64  duration = 2;                                   // Cooldown duration in seconds
  uint64 consecutive_overturns = 3;                      // How many overturns in a row
  int64  cooldown_until = 4;                             // Timestamp when cooldown ends
}


// Emitted when archive cooldown is set
message EventArchiveCooldownSet {
  uint64 root_id = 1;
  int64  expires_at = 2;
}

// Emitted when appeal cooldown is set
message EventAppealCooldownSet {
  uint64 post_id = 1;
  int64  expires_at = 2;
}

// ============================================
// MEMBER REPORTING EVENTS
// ============================================

// Emitted when a sentinel reports a member
message EventMemberReported {
  string member = 1;
  string reporter = 2;
  uint64 evidence_count = 3;
  uint32 recommended_action = 4;
}

// Emitted when another sentinel co-signs a member report
message EventMemberReportCoSigned {
  string member = 1;
  string cosigner = 2;
  uint64 total_reporters = 3;
}

// Emitted when a report reaches cosign threshold
message EventMemberReportEscalated {
  string member = 1;
  uint64 reporter_count = 2;
  string total_bond = 3;
}

// Emitted when a member submits their defense
message EventMemberDefenseSubmitted {
  string member = 1;
  uint64 context_posts = 2;
}

// Emitted when HR Committee dismisses a report
message EventMemberReportDismissed {
  string member = 1;
  string authority = 2;
  string reason = 3;
}

// Emitted when HR Committee issues a warning
message EventMemberWarned {
  string member = 1;
  string authority = 2;
  uint64 warning_number = 3;                             // Which warning this is (1st, 2nd, etc)
  string reason = 4;
}

// Emitted when HR Committee demotes a member
message EventMemberDemoted {
  string member = 1;
  string authority = 2;
  uint64 previous_tier = 3;
  uint64 new_tier = 4;
  string reason = 5;
}

// Emitted when HR Committee zeroes a member
message EventMemberZeroed {
  string member = 1;
  string authority = 2;
  string dream_burned = 3;
  string reputation_reset = 4;
  string reason = 5;
}

// Emitted when an unresolved member report expires
message EventMemberReportExpired {
  string member = 1;
  uint64 reporter_count = 2;
  string bonds_returned = 3;
}

// ============================================
// NEW EVENTS FOR SECURITY FIXES
// ============================================

// Emitted when consecutive overturn counter is reset after N upheld hides
message EventSentinelOverturnCounterReset {
  string sentinel = 1;
  uint64 upheld_required = 2;                            // N upheld hides that triggered reset
}

// Emitted when appeal resolution fails due to insufficient module balance
message EventAppealFailedInsufficientBalance {
  uint64 post_id = 1;
}

// Emitted when appeal is resolved due to jury insufficiency (favors appellant)
message EventAppealResolvedJuryInsufficient {
  uint64 post_id = 1;
  string appellant = 2;
  string refund_amount = 3;                              // 90% of appeal fee returned
  string reason = 4;                                     // e.g., "jury_quorum_unavailable"
}

// Emitted when salvation is denied due to rate limit
// (extends EventSalvationDenied with reason = "rate_limit_exceeded")

// Emitted when Gov action appeal is filed
message EventGovActionAppealed {
  uint64 appeal_id = 1;
  string appellant = 2;
  uint32 action_type = 3;                                // GovActionType enum value
  string action_target = 4;
  uint64 initiative_id = 5;                              // x/rep initiative for jury
}

// Emitted when Gov action appeal is upheld (HR Committee wins)
message EventGovActionAppealUpheld {
  uint64 appeal_id = 1;
  string appellant = 2;
  string fee_burned = 3;
  string fee_to_hr = 4;
}

// Emitted when Gov action appeal is overturned (appellant wins)
message EventGovActionAppealOverturned {
  uint64 appeal_id = 1;
  string appellant = 2;
  uint32 action_type = 3;
  string action_target = 4;
  string fee_refunded = 5;
}

// Emitted when HR action is reversed due to successful appeal
message EventGovActionOverturned {
  uint32 action_type = 1;
  string action_target = 2;
  string reason = 3;                                     // Jury reasoning
  uint64 appeal_id = 4;
}

// Emitted when Gov action appeal times out
message EventGovActionAppealTimedOut {
  uint64 appeal_id = 1;
  string appellant = 2;
  string partial_refund = 3;
}

// Emitted when DREAM minting hits epoch cap (pro-rata scaling applied)
message EventDreamMintCapReached {
  string epoch_minted = 1;                               // Total DREAM that would be minted without cap
  string epoch_cap = 2;                                  // Max DREAM mintable per epoch
  uint64 sentinels_affected = 3;                         // All sentinels who got scaled rewards
  string scale_factor = 4;                               // Scaling factor applied (e.g., "0.80" means 80% of base reward)
}

// Emitted when archive is blocked due to cycle limit
message EventArchiveCycleLimitReached {
  uint64 root_id = 1;
  uint64 archive_count = 2;
  uint64 max_cycles = 3;
}

// ============================================
// THREAD FOLLOWING EVENTS
// ============================================

// Emitted when a user follows a thread
message EventThreadFollowed {
  uint64 thread_id = 1;                                  // Root post ID of thread
  string follower = 2;                                   // Address following the thread
  uint64 follower_count = 3;                             // Total followers after this follow
}

// Emitted when a user unfollows a thread
message EventThreadUnfollowed {
  uint64 thread_id = 1;                                  // Root post ID of thread
  string follower = 2;                                   // Address unfollowing the thread
  uint64 follower_count = 3;                             // Total followers after this unfollow
}

// ============================================
// CONVICTION PROPAGATION EVENTS
// ============================================

// Emitted when an ephemeral post first enters conviction-sustained state
// (community conviction score meets threshold at TTL expiry, extending its life)
message EventPostConvictionSustained {
  uint64 post_id = 1;                                    // Post that entered conviction-sustained state
  uint64 initiative_id = 2;                              // Linked x/rep initiative
  string conviction_score = 3;                           // Current conviction score at sustain time
  string threshold = 4;                                  // Threshold that was met
  int64  new_expiration = 5;                             // New expiration timestamp
}

// Emitted when a conviction-sustained post is renewed (conviction still meets threshold at subsequent expiry)
message EventPostConvictionRenewed {
  uint64 post_id = 1;                                    // Post that was renewed
  uint64 initiative_id = 2;                              // Linked x/rep initiative
  string conviction_score = 3;                           // Current conviction score
  int64  new_expiration = 4;                             // New expiration timestamp
}

// Emitted when an initiative link is registered for conviction propagation
message EventInitiativeLinkRegistered {
  uint64 post_id = 1;                                    // Forum post ID
  uint64 initiative_id = 2;                              // Linked x/rep initiative
  string creator = 3;                                    // Post creator (or module account for anonymous)
}

// Emitted when an initiative link is removed (post tombstoned or deleted)
message EventInitiativeLinkRemoved {
  uint64 post_id = 1;                                    // Forum post ID
  uint64 initiative_id = 2;                              // Previously linked x/rep initiative
  string reason = 3;                                     // "tombstoned", "deleted", "hidden_expired"
}
```

---

## 10. Errors

> **Implementation note:** The actual error codes in `x/forum/types/errors.go` use the range 1100-2499, organized by category. The design spec previously used codes 1-166. The table below reflects the **actual implementation**.

| Category | Error | Code | Description |
|----------|-------|------|-------------|
| **Base (1100-1199)** | | | |
| | `ErrInvalidSigner` | 1100 | Expected gov account as only signer for proposal message |
| | `ErrUnauthorized` | 1101 | Unauthorized |
| | `ErrInvalidParam` | 1102 | Invalid parameter |
| **Content (1200-1299)** | | | |
| | `ErrContentTooLarge` | 1200 | Content exceeds maximum size |
| | `ErrInvalidTag` | 1201 | Invalid tag format |
| | `ErrTagLimitExceeded` | 1202 | Tag limit exceeded for post |
| | `ErrTotalTagsExceeded` | 1203 | System-wide tag limit exceeded |
| | `ErrTagNotFound` | 1204 | Tag not found |
| | `ErrTagAlreadyExists` | 1205 | Tag already exists |
| | `ErrReservedTag` | 1206 | Tag is reserved |
| | `ErrTagExpired` | 1207 | Tag has expired |
| | `ErrInvalidContent` | 1208 | Invalid content |
| | `ErrEmptyContent` | 1209 | Content cannot be empty |
| | `ErrInvalidReasonCode` | 1210 | Invalid reason code |
| | `ErrReasonTextRequired` | 1211 | Custom reason text required for OTHER reason code |
| | `ErrMaxTagLength` | 1212 | Tag exceeds maximum length |
| **Post (1300-1399)** | | | |
| | `ErrPostNotFound` | 1300 | Post not found |
| | `ErrNotPostAuthor` | 1301 | Not the post author |
| | `ErrPostAlreadyHidden` | 1302 | Post is already hidden |
| | `ErrPostNotHidden` | 1303 | Post is not hidden |
| | `ErrPostDeleted` | 1304 | Post has been deleted |
| | `ErrPostArchived` | 1305 | Post has been archived |
| | `ErrNotRootPost` | 1306 | Operation only allowed on root posts |
| | `ErrIsRootPost` | 1307 | Operation not allowed on root posts |
| | `ErrPostAlreadyPinned` | 1308 | Post is already pinned |
| | `ErrPostNotPinned` | 1309 | Post is not pinned |
| | `ErrMaxPinnedPosts` | 1310 | Maximum pinned posts reached for category |
| | `ErrCannotDeleteHiddenPost` | 1311 | Cannot delete hidden post |
| | `ErrMaxReplyDepthExceeded` | 1312 | Maximum reply depth exceeded |
| | `ErrParentPostNotFound` | 1313 | Parent post not found |
| | `ErrInvalidPostStatus` | 1314 | Invalid post status for this operation |
| **Thread (1400-1449)** | | | |
| | `ErrThreadLocked` | 1400 | Thread is locked |
| | `ErrThreadNotLocked` | 1401 | Thread is not locked |
| | `ErrThreadAlreadyLocked` | 1402 | Thread is already locked |
| | `ErrNotThreadAuthor` | 1403 | Not the thread author |
| | `ErrLockReasonRequired` | 1404 | Lock reason required for sentinels |
| | `ErrMoveReasonRequired` | 1405 | Move reason required |
| | `ErrCannotMoveReservedTag` | 1406 | Cannot move thread with reserved tag |
| **Category (1450-1499)** | | | |
| | `ErrCategoryNotFound` | 1450 | Category not found |
| | `ErrCategoryAlreadyExists` | 1451 | Category already exists |
| | `ErrMembersOnlyWrite` | 1452 | Category restricted to members only |
| | `ErrAdminOnlyWrite` | 1453 | Category restricted to admins only |
| | `ErrInvalidCategoryId` | 1454 | Invalid category ID |
| **Rate Limit (1500-1549)** | | | |
| | `ErrRateLimitExceeded` | 1500 | Rate limit exceeded |
| | `ErrReactionLimitExceeded` | 1501 | Reaction rate limit exceeded |
| | `ErrFlagLimitExceeded` | 1502 | Flag rate limit exceeded |
| | `ErrFollowLimitExceeded` | 1503 | Follow rate limit exceeded |
| | `ErrSalvationLimitExceeded` | 1504 | Salvation rate limit exceeded |
| | `ErrDownvoteLimitExceeded` | 1505 | Downvote rate limit exceeded |
| **Moderation (1550-1649)** | | | |
| | `ErrSentinelCooldown` | 1550 | Sentinel is in cooldown period |
| | `ErrInsufficientBond` | 1551 | Insufficient sentinel bond |
| | `ErrInsufficientBacking` | 1552 | Insufficient sentinel backing |
| | `ErrInsufficientReputation` | 1553 | Insufficient reputation tier |
| | `ErrSentinelDemoted` | 1554 | Sentinel is demoted |
| | `ErrNotSentinel` | 1555 | Not a registered sentinel |
| | `ErrAlreadySentinel` | 1556 | Already registered as sentinel |
| | `ErrHideLimitExceeded` | 1557 | Sentinel hide limit exceeded for epoch |
| | `ErrLockLimitExceeded` | 1558 | Sentinel lock limit exceeded for epoch |
| | `ErrMoveLimitExceeded` | 1559 | Sentinel move limit exceeded for epoch |
| | `ErrInsufficientLockBond` | 1560 | Insufficient bond for thread locking |
| | `ErrInsufficientLockBacking` | 1561 | Insufficient backing for thread locking |
| | `ErrSentinelNotFound` | 1562 | Sentinel activity record not found |
| | `ErrCannotUnbondPendingHides` | 1563 | Cannot unbond with pending hide appeals |
| | `ErrDemotionCooldown` | 1564 | Sentinel is in demotion cooldown |
| | `ErrBondAmountTooSmall` | 1565 | Bond amount too small |
| | `ErrNotGovAuthority` | 1566 | Not governance authority |
| | `ErrNotAuthorized` | 1567 | Not authorized for this action |
| | `ErrPostStatus` | 1568 | Invalid post status for this operation |
| **Appeal (1650-1699)** | | | |
| | `ErrAppealCooldown` | 1650 | Appeal cooldown not yet passed |
| | `ErrAppealAlreadyFiled` | 1651 | Appeal already filed for this action |
| | `ErrAppealNotFound` | 1652 | Appeal not found |
| | `ErrAppealPending` | 1653 | Appeal is pending |
| | `ErrAppealExpired` | 1654 | Appeal deadline has passed |
| | `ErrGovLockNotAppealable` | 1655 | Governance locks must be appealed via gov action appeal |
| | `ErrLockAppealAlreadyFiled` | 1656 | Lock appeal already filed |
| | `ErrLockAppealExpired` | 1657 | Lock appeal window has expired |
| | `ErrMoveAppealAlreadyFiled` | 1658 | Move appeal already filed |
| | `ErrMoveAppealExpired` | 1659 | Move appeal window has expired |
| | `ErrPinDisputeAlreadyFiled` | 1660 | Pin dispute already filed |
| | `ErrNotSentinelPin` | 1661 | Cannot dispute non-sentinel pins |
| **Archive (1700-1749)** | | | |
| | `ErrThreadNotInactive` | 1700 | Thread is not inactive enough to archive |
| | `ErrArchiveCooldown` | 1701 | Archive cooldown not yet passed |
| | `ErrUnarchiveCooldown` | 1702 | Unarchive cooldown not yet passed |
| | `ErrArchiveCycleLimit` | 1703 | Archive cycle limit reached, requires governance approval |
| | `ErrArchivedThreadNotFound` | 1704 | Archived thread not found |
| | `ErrCannotArchiveThreadWithPendingAppeal` | 1706 | Cannot archive thread with pending appeal |
| **Bounty (1750-1799)** | | | |
| | `ErrBountyNotFound` | 1750 | Bounty not found |
| | `ErrBountyNotActive` | 1751 | Bounty is not active |
| | `ErrBountyExpired` | 1752 | Bounty has expired |
| | `ErrBountyAlreadyExists` | 1753 | Bounty already exists for this thread |
| | `ErrNotBountyCreator` | 1754 | Not the bounty creator |
| | `ErrBountyAmountTooSmall` | 1755 | Bounty amount below minimum |
| | `ErrBountyAlreadyAwarded` | 1756 | Bounty has already been awarded |
| | `ErrMaxBountyWinners` | 1757 | Maximum bounty winners reached |
| | `ErrInvalidBountyDuration` | 1758 | Bounty duration exceeds maximum |
| | `ErrBountyInModeration` | 1759 | Bounty is pending moderation resolution |
| | `ErrNotReplyInThread` | 1760 | Post is not a reply in the bounty thread |
| | `ErrBountyFullyAwarded` | 1761 | Bounty has been fully awarded |
| **Tag Budget (1800-1849)** | | | |
| | `ErrTagBudgetNotFound` | 1800 | Tag budget not found |
| | `ErrTagBudgetNotActive` | 1801 | Tag budget is not active |
| | `ErrTagBudgetInsufficient` | 1802 | Insufficient funds in tag budget |
| | `ErrNotGroupMember` | 1803 | Not a member of the budget group |
| | `ErrNotGroupAccount` | 1804 | Not a valid group account |
| | `ErrTagNotReserved` | 1805 | Tag is not reserved |
| | `ErrTagBudgetAlreadyExists` | 1806 | Tag budget already exists for this tag |
| | `ErrAwardAmountTooSmall` | 1807 | Award amount below minimum |
| | `ErrMembersOnlyAward` | 1808 | Award restricted to group members only |
| | `ErrPostNotInTag` | 1809 | Post does not have the required tag |
| **Flag (1850-1899)** | | | |
| | `ErrAlreadyFlagged` | 1850 | Already flagged this post |
| | `ErrFlagNotFound` | 1851 | Flag record not found |
| | `ErrNotInReviewQueue` | 1852 | Post not in review queue |
| | `ErrFlagExpired` | 1853 | Flags have expired |
| | `ErrMaxFlaggersReached` | 1854 | Maximum flaggers reached for this post |
| **Thread Metadata (1900-1949)** | | | |
| | `ErrNoAcceptedReply` | 1900 | No accepted reply for this thread |
| | `ErrAlreadyAccepted` | 1901 | Thread already has an accepted reply |
| | `ErrNoProposedReply` | 1902 | No proposed reply for this thread |
| | `ErrProposalAlreadyPending` | 1903 | Accept proposal already pending |
| | `ErrMaxPinnedReplies` | 1904 | Maximum pinned replies reached |
| | `ErrAlreadyPinned` | 1905 | Post is already pinned |
| | `ErrNotPinned` | 1906 | Post is not pinned |
| | `ErrCannotPinOwnReply` | 1907 | Cannot pin own reply |
| | `ErrPinDisputed` | 1908 | Pin is being disputed |
| | `ErrCannotDisputeGovPin` | 1909 | Cannot dispute governance pins |
| | `ErrAlreadyDisputed` | 1910 | Pin is already disputed |
| **Follow (1950-1999)** | | | |
| | `ErrAlreadyFollowing` | 1950 | Already following this thread |
| | `ErrNotFollowing` | 1951 | Not following this thread |
| | `ErrCannotVoteOwnPost` | 1952 | Cannot vote on your own post |
| **Report (2000-2049)** | | | |
| | `ErrReportNotFound` | 2000 | Report not found |
| | `ErrReportAlreadyExists` | 2001 | Report already exists |
| | `ErrReportExpired` | 2002 | Report has expired |
| | `ErrInsufficientEvidence` | 2003 | Insufficient evidence posts |
| | `ErrMaxReportersReached` | 2004 | Maximum reporters reached |
| | `ErrAlreadyCosigned` | 2005 | Already co-signed this report |
| | `ErrDefenseAlreadySubmitted` | 2006 | Defense already submitted |
| | `ErrDefenseWaitPeriod` | 2007 | Must wait after defense before resolution |
| | `ErrMinReportDuration` | 2008 | Minimum report duration not yet passed |
| | `ErrReportNotPending` | 2009 | Report is not pending |
| | `ErrCannotReportSelf` | 2010 | Cannot report yourself |
| | `ErrTagReportNotFound` | 2011 | Tag report not found |
| | `ErrTagReportAlreadyExists` | 2012 | Tag report already exists |
| **Gov Action Appeal (2050-2099)** | | | |
| | `ErrGovAppealNotFound` | 2050 | Governance action appeal not found |
| | `ErrGovAppealNotPending` | 2051 | Governance action appeal is not pending |
| | `ErrCannotAppealAction` | 2052 | This action type cannot be appealed |
| | `ErrPauseNotLongEnough` | 2053 | Pause duration not long enough to appeal |
| | `ErrGovAppealAlreadyFiled` | 2054 | Governance action appeal already filed |
| **Emergency Pause (2100-2149)** | | | |
| | `ErrForumPaused` | 2100 | Forum is paused |
| | `ErrModerationPaused` | 2101 | Moderation is paused |
| | `ErrNewPostsPaused` | 2102 | New posts are paused |
| | `ErrAppealsPaused` | 2103 | Appeals are paused |
| | `ErrBountiesDisabled` | 2104 | Bounties are disabled |
| | `ErrReactionsDisabled` | 2105 | Reactions are disabled |
| | `ErrEditingDisabled` | 2106 | Post editing is disabled |
| **Edit (2150-2199)** | | | |
| | `ErrEditWindowExpired` | 2150 | Edit window has expired |
| | `ErrCannotEditHiddenPost` | 2151 | Cannot edit hidden post |
| | `ErrCannotEditDeletedPost` | 2152 | Cannot edit deleted post |
| **Payment (2200-2249)** | | | |
| | `ErrInsufficientFunds` | 2200 | Insufficient funds |
| | `ErrInvalidAmount` | 2201 | Invalid amount |
| | `ErrInvalidDenom` | 2202 | Invalid denomination |
| **Member (2250-2299)** | | | |
| | `ErrNotMember` | 2250 | Not a member |
| | `ErrMembershipTooNew` | 2251 | Membership too recent for this operation |
| **Conviction Propagation (2400-2499)** | | | |
| | `ErrInvalidInitiativeRef` | 2400 | Invalid initiative reference |

---

## 11. Security Considerations

| Threat | Defense |
|--------|---------|
| **Spam Flooding** | **Gas Fee (Base)** + **Spam Tax (Non-Member)** creates layered cost. Ephemeral posts auto-prune. |
| **State Bloat (Tags)** | **Tag Creation Fee** + **Tag Expiration** ("Use it or lose it") prevents registry pollution. |
| **Rich Censor** | Moderation requires DREAM (Merit/Work), not SPARK. Money cannot buy authority. |
| **Lazy Sentinel** | Accuracy-based rewards require minimum 70% accuracy rate. Zero activity = zero rewards. Market forces (undelegation) remove lazy mods. |
| **Over-Moderation** | Rewards based on accuracy rate, not volume. Sentinel incentivized to hide only clear violations. |
| **GC Griefing** | `lazy_prune_limit` caps deletions per tx. Gas cost variance is negligible. |
| **Tag Spam** | Only Tier 2+ Members can create tags. `max_tags_per_post=5`, `max_tag_length=32`, `max_total_tags=10000`. Reserved tags protected. |
| **Tag Squatting** | `reserved_tags` parameter protects system-critical tags. HR Committee can reserve tags and assign authorities. Existing posts with newly-reserved tags may be hidden by sentinels as misinformation if misleading. |
| **Tag Authority Abuse** | Only HR Committee can reserve tags or assign authorities (via governance). Groups cannot self-assign reserved tags. HR Committee retains override on all reserved tags. |
| **Sybil Posts** | Membership check via `x/commons` prevents anonymous spam. Non-member tax creates cost. |
| **Appeal Abuse** | `appeal_fee` deposit discourages frivolous appeals. Lost appeals forfeit deposit. |
| **Recursive Salvation Bomb** | `max_salvation_depth=10` limits ancestor chain conversion. Partial salvation emits event. |
| **Archive Bomb** | `max_archive_post_count=500` and `max_archive_size_bytes=1MB` prevent oversized archives. |
| **Decompression Attack** | `MsgUnarchiveThread` gas cost proportional to archive size. Metered decompression. |
| **Content Bloat** | `max_content_size=10KB` limits on-chain storage. |
| **Censorship Resistance** | **No hard delete mechanism exists.** All deletions are soft deletes (content replaced or status changed). Hidden/deleted content remains queryable on-chain. Every moderation action (sentinel or HR) has an appeal path to jury. Frontends may filter, but on-chain data layer never censors. |
| **Deep Nesting Attack** | `max_reply_depth=10` prevents deeply nested reply chains that complicate thread rendering and traversal. |
| **Sentinel Unbonding Escape** | `HideRecord` snapshots sentinel's DREAM bond at hide time. Slashing uses snapshot, not current balance. |
| **Committed Bond Cleanup** | `total_committed_bond` on SentinelActivity is decremented (using HideRecord.committed_amount) on all resolution paths: appeal upheld, appeal rejected, appeal timeout, jury insufficiency, hidden post expiration. Invariant ensures total_committed_bond ≤ current_bond. |
| **Hidden Post Limbo** | `hidden_expiration=7d` auto-deletes unchallenged hidden posts. Posts don't stay in limbo forever. |
| **Appeal Timeout** | `appeal_deadline=14d` ensures verdicts. **Balanced timeout**: post restored, fee split 50% refund / 50% burn. Neither party penalized for jury failure. |
| **Reward Pool Accumulation** | `max_reward_pool` cap with `overflow_burn_ratio=50%` prevents infinite accumulation. |
| **Jury Non-Participation** | Jurors compensated via x/rep DREAM minting (150 DREAM per juror). Participation tracked via `JuryParticipation` records; non-voters excluded from future selections. |
| **Rate Limit Gaming** | Rolling 24h window (timestamps list) prevents boundary manipulation. **Stale entries cleaned in EndBlocker**. |
| **Cross-Module Failure** | All `MsgAppealPost` state changes atomic. Failed x/rep calls revert escrow automatically. |
| **Sentinel Accuracy Gaming** | **Accuracy based ONLY on resolved appeals** (not unchallenged hides/locks). Requires `min_appeals_for_accuracy` (5) before accuracy counts. `min_epoch_activity_for_reward` (3) prevents passive farming. Locks contribute to unified accuracy score. |
| **Sentinel Accuracy Farming** | **Symmetric accuracy decay** after `accuracy_decay_grace_epochs` (3) epochs of inactivity. Both upheld and overturned counts decay at same rate (`accuracy_decay_rate=10%` per epoch), preserving accuracy ratio but reducing total history. Sentinels must stay active to maintain meaningful stats. Full reset after `accuracy_decay_max_epochs` (10) prevents permanent accuracy lock-in. |
| **Sentinel Cherry-Picking** | Score uses `sqrt(epoch_appeals_resolved)` not total hides. Targeting non-appellants yields no accuracy benefit. |
| **Member-Sentinel Collusion** | `hide_appeal_cooldown` (1h) prevents instant coordination. Delay allows community to observe hide action. |
| **Salvation Spam Laundering** | `min_membership_for_salvation` (7d) prevents new members from saving spam. Must be established member to salvage posts. |
| **Salvation in Locked Threads** | Thread lock status checked during salvation traversal. Cannot make posts permanent in locked threads, preventing state inconsistency where permanent posts exist in locked threads. |
| **GC Starvation** | **EndBlocker GC** runs every 100 blocks regardless of post activity. Prevents state bloat during quiet periods. |
| **Archive Griefing** | `archive_cooldown` + `unarchive_cooldown` create minimum 31-day cycle. Griefing becomes economically infeasible. |
| **Slashing Impact** | Fixed slash amount (100 DREAM) is predictable. 5 consecutive overturns → demotion. Recovery mode auto-bonds rewards until restored. |
| **Recovery Mode Incentive** | Sentinels earn both SPARK (paid out) and DREAM (minted). In recovery, DREAM auto-bonds while SPARK keeps flowing. Good moderation restores bond; bad moderation accelerates demotion. |
| **Emergency Attacks** | `forum_paused` and `moderation_paused` flags allow HR Committee to halt forum during active attacks. |
| **Tag Keepalive Attack** | `MsgReportTag` + `MsgResolveTagReport` allow community reporting. HR Committee can force-remove problematic tags. |
| **Reward Pool Monopoly** | `max_sentinel_reward_share` (25%) caps individual sentinel earnings per epoch. Undistributed funds carry to next epoch. |
| **HR Committee Abuse** | Emergency pause is transparent (emits events). Moderation pause separate from forum pause for granular control. **Destructive actions (zeroing) require quorum (2+ signers) and time-lock (48h delay)** to prevent single compromised member from mass zeroing. Meta-appeals via jury provide additional oversight. |
| **Frivolous Member Reports** | `member_report_bond` (200 SPARK) discourages false reports. Evidence must be valid posts by reported member. Restored-on-appeal posts rejected as evidence. |
| **Sentinel Harassment** | Member can submit defense before resolution. HR Committee sees both sides. Warning count tracked for context. Multiple sentinels required for escalation. |
| **Report Camping** | `member_report_expiration` (30d) prevents indefinite pending reports. Expired reports return bonds. |
| **Coordinated Reporting** | Co-sign threshold (3) requires multiple independent sentinels. Bond requirement per sentinel makes coordination expensive. |
| **Warning Accumulation** | `max_warnings_before_demotion` auto-recommends escalation after pattern established. HR Committee has full discretion. |
| **Unbounded Sentinel Moderation** | `max_hides_per_epoch` (50) limits posts any sentinel can hide per reward epoch. Prevents mass hiding attacks. |
| **Sentinel Repeat Offender** | `sentinel_overturn_cooldown` (24h base) with exponential escalation (2x per consecutive overturn, max 7d). Bad actors progressively restricted. |
| **False Report Farming** | `dismissed_report_burn_ratio` (15%) burns portion of bonds on dismissed tag/member reports. Makes frivolous reporting costly. |
| **Defense Bypass** | `min_report_duration` (48h) ensures members have time to submit defense before HR Committee resolution. |
| **Rate Limit Memory Bloat** | Epoch-based rate limiting uses fixed memory (5 fields) instead of unbounded timestamp list. No growth over time. |
| **Tag Queue Thrashing** | Lazy expiration refresh (24h minimum between updates) reduces queue operations for popular tags by ~95%. |
| **Cooldown State Leakage** | EndBlocker GC prunes expired `archive_cooldown` and `appeal_cooldown` records. No dead state accumulation. |
| **Sentinel Bond Escape** | Sentinels cannot unbond while they have pending appeals (via HideRecord check). Snapshot ensures slashing uses hide-time bond value. |
| **Granular Emergency Response** | Separate `new_posts_paused` and `appeals_paused` flags allow targeted intervention without blocking legitimate appeals during attacks. |
| **Params Authority** | `params_authority` key explicitly documents governance control. Critical params should be protected via same pattern as x/mint. |
| **Bond Snapshot Race Condition** | `HideRecord.committed_amount` captures exact commitment at hide time. `total_committed_bond` aggregate tracks sum of all pending commitments. Prevents slashing from exceeding actual bond across concurrent appeals. |
| **Accuracy Gaming via Non-Appellants** | `min_appeal_rate` (10%) requires meaningful portion of hides to be contested. Sentinels targeting users unlikely to appeal get no accuracy credit. |
| **HR Committee Abuse** | Meta-appeals via `MsgAppealGovAction` allow affected members to escalate HR decisions to jury. Overturned decisions incur reputation penalty (`gov_overturn_reputation_penalty=50`) for signing HR members, create public record, and may trigger confidence vote. |
| **HR Thread Lock Abuse** | HR Committee can lock threads, but thread authors can appeal via `MsgAppealGovAction` with `GOV_ACTION_TYPE_THREAD_LOCK`. Overturned locks incur same reputation penalty as other overturned HR actions. Thread author always has recourse. |
| **HR Thread Move Abuse** | HR Committee can move threads to any category, but thread authors can appeal via `MsgAppealGovAction` with `GOV_ACTION_TYPE_THREAD_MOVE`. Appeal record stores `original_category_id` for restoration on overturn. |
| **Consecutive Overturn Reset Attack** | `upheld_hides_to_reset` (3) requires multiple consecutive upheld hides before overturn counter resets. Prevents "reset attacks" via obvious violations. |
| **Salvation Inflation Attack** | `max_salvations_per_day` (10) limits how many ephemeral posts a member can convert to permanent per 24h. Prevents mass salvation of spam. |
| **Jury Compensation** | Jurors receive fixed DREAM via x/rep minting (not from appeal fees). No pool to deplete — compensation scales with governance-adjustable DREAM reward amount. |
| **Jury Timeout Abuse** | Juror participation tracking excludes non-voters from future jury selections after <50% participation rate over 10+ assignments. Prevents collusion to trigger timeouts for fee extraction. |
| **Jury Sybil Attack** | Reputation-weighted jury selection makes Sybil attacks expensive - attackers must build reputation across many accounts. Combined with minimum reputation tier requirement (Tier 2+) and conflict exclusions. |
| **Jury Insufficiency** | When quorum can't be formed (exclusions, conflicts, small community), appeal resolves in favor of appellant with 90% refund. System failures shouldn't penalize those who paid to appeal. |
| **Conviction Propagation Gaming** | `conviction_propagation_ratio` (default 10%) limits amplification. Initiative must exist and be non-terminal at post creation. Content conviction is time-weighted (`amount * min(1, t / (2 * half_life))`), so flash stakes provide minimal benefit. External conviction requirement (50%) on the initiative ensures propagated conviction alone cannot complete an initiative. |
| **Conviction Renewal Abuse** | `conviction_renewal_threshold` (default 100.0) requires substantial community staking. Renewal extends TTL by `conviction_renewal_period` (7 days), not permanently — conviction must be maintained at each renewal. If conviction drops below threshold, `conviction_sustained` is cleared and the post expires normally at next TTL check. |
| **Initiative Reference Spam** | `ValidateInitiativeReference` rejects terminal initiatives (COMPLETED/FAILED/CANCELLED). `initiative_id` is immutable after post creation — cannot be retroactively linked to a new initiative. Each post can reference at most one initiative. |
| **Conviction Inflation via Forum** | Forum content conviction uses `STAKE_TARGET_FORUM_CONTENT` (target type 5), which is tracked separately from initiative conviction. The propagation ratio (10%) means even 1000 DREAM of content conviction only contributes 100 DREAM-equivalent to the initiative. |
| **Anonymous Conviction Laundering** | Anonymous posts can reference initiatives (propagating conviction), but the anonymous author cannot stake on their own content (identity hidden). Only other community members can conviction-stake on forum content, ensuring propagated conviction represents genuine community interest. |
| **XP Farming Rings** | Handled by x/season via ForumHooks. x/forum emits engagement events; x/season implements anti-gaming (epoch limits, reciprocal cooldowns). |
| **Reserved Tag Grandfathering** | Auto-flagging of posts using newly-reserved tags ensures sentinel visibility for review. Prevents misleading "[official]" posts from predating reservation. |
| **Sentinel Accuracy Reset** | Demotion cooldown (7 days) prevents bad actors from resetting accuracy stats by: get slashed → unbond → immediately re-bond fresh. |
| **DREAM Hyperinflation** | `max_dream_mint_per_epoch` caps total DREAM x/forum can mint. **Pro-rata scaling** ensures all eligible sentinels receive equally-reduced rewards when cap is hit, preventing unfairness by address ordering. |
| **DREAM Scaling Gaming** | Eligibility is snapshotted at epoch start (`epoch_eligible_sentinels`), not calculated at distribution time. This ensures the scaling factor is predictable throughout the epoch and prevents mid-epoch eligibility manipulation. |
| **Sentinel Sybil via Self-Backing** | `min_sentinel_backing` must come from OTHER users (not self-delegation). Additionally, `min_backer_membership_duration` (30d default) ensures backing only counts from established members, preventing Sybil attacks via newly-created accounts. |
| **Archive Count Off-by-One** | Thread post count includes root post (`1 + descendant_count`) preventing off-by-one when comparing to `max_archive_post_count`. |
| **Archive Count Tampering** | `archive_count` stored in separate `ArchiveMetadata` (not in Post or compressed blob). Prevents manipulation via corrupted archives, blob tampering, or desync between storage locations. |
| **Tag Expiration Race** | `expiration_index` field in Tag tracks authoritative queue entry. GC skips stale queue entries when tag is refreshed between queue creation and GC. |
| **Reporter List Bloat** | `max_tag_reporters` and `max_member_reporters` cap reporters arrays. Prevents unbounded state growth from popular reports. |
| **Defense Bypass** | `min_defense_wait` (24h) ensures HR Committee cannot resolve immediately after defense submitted. Member's response gets consideration time. |
| **Archive Griefing Cycle** | `max_archive_cycles` (5) requires HR Committee approval for re-archiving after multiple cycles. Makes griefing attacks infeasible. |
| **Module Account Insolvency** | Balance safety check in `OnInitiativeFinalized` detects insufficient funds and gracefully restores post rather than leaving in limbo. |
| **Pinned Post Spam** | `max_pinned_per_category` (5) limits pinned posts per category. Only HR Committee can pin, preventing abuse. |
| **Pin Priority Collision** | Multiple posts can share same priority; secondary sort by `pinned_at` timestamp ensures deterministic ordering. |
| **Thread Lock Bypass** | Lock check occurs at reply creation time, not just validation. Locked status stored on root post, so single lookup determines thread state. |
| **Lock Reason Abuse** | Lock reason required for Sentinels, displayed to users. HR Committee and jury oversight via appeals. |
| **Sentinel Lock Abuse** | High-trust requirements (Tier 4+, 2x bond/backing) ensure only established Sentinels can lock. Rate limit (`max_sentinel_locks_per_epoch=5`) prevents mass locking. |
| **Lock Appeal Gaming** | `lock_appeal_fee` (500 SPARK) discourages frivolous appeals. `lock_appeal_cooldown` (1h) prevents instant coordination. |
| **Sentinel Lock Escape** | `ThreadLockRecord` snapshots bond at lock time. Sentinel cannot unbond to avoid slash during appeal period. |
| **HR Lock Appeal Path** | HR Committee locks cannot be appealed via `MsgAppealThreadLock` but CAN be appealed via `MsgAppealGovAction` with `GOV_ACTION_TYPE_THREAD_LOCK`. Thread author retains recourse while using proper channel. |
| **Lock Overturn Penalty** | Same escalating cooldown as hide overturns. Repeated bad locks progressively restrict sentinel's moderation ability. |
| **Reaction Spam** | Unified `max_reactions_per_day` (100) limits all reactions per user per 24h. Non-member `reaction_spam_tax` (10 SPARK) creates cost barrier. One reaction per user per post prevents vote-stacking. |
| **Reaction Sybil** | Rate limit + spam tax + one-per-target makes Sybil attacks expensive. Membership requirement for free reactions ties votes to earned status. |
| **Reaction State Bloat** | `ReactionRecord` per public vote (cleaned up on thread archival §16.15, leaving only aggregate counters). Private reaction nullifiers managed by x/shield's centralized store. |
| **Vote Brigading** | One reaction per user per post (enforced by keyed storage for public, x/shield nullifier for private). Unified daily budget caps total reactions. |
| **Upvote Farming** | No direct economic benefit to post authors from upvotes. Reactions are engagement signals only, not rewards. One-per-target prevents vote inflation. |
| **Downvote Harassment** | `downvote_deposit` (50 SPARK) creates significant cost for both public and private downvotes. Deposit burned immediately. |
| **Downvote Brigading** | Unified `max_reactions_per_day` limits all reactions. Each downvote burns deposit. Coordinated attacks require large capital commitment. |
| **Self-Downvote Exploit** | `ErrCannotDownvoteOwnPost` prevents gaming via self-downvotes (public mode). Private downvotes cannot check authorship but trust-level requirement + deposit cost make self-gaming expensive and pointless. |
| **Private Reaction Privacy** | ZK proof reveals nothing about the voter except membership at minimum trust level. Nullifiers are scoped to individual posts — reactions on different posts cannot be correlated. All privacy infrastructure (proof verification, nullifiers) now managed by x/shield. |
| **Private Reaction Relay Trust** | Shielded execution via x/shield's `MsgShieldedExec` handles relay trust. Module-paid gas eliminates the need for submitter balance, reducing relay trust assumptions. |
| **Archival Re-reaction** | After archival, reaction uniqueness data is deleted. Unarchived threads allow re-reactions. Acceptable: unarchival is rare, gas-expensive, and aggregate counts are preserved. |
| **Edit Context Manipulation** | `edit_max_window` (24h) prevents late edits that change discussion context. Readers can trust posts older than 24h won't change. |
| **Edit Abuse** | `edit_fee` (25 SPARK) after 5-minute grace period discourages frivolous edits. Fee funds reward pool. |
| **Edit During Moderation** | Hidden/deleted/archived posts cannot be edited. Prevents evidence tampering during disputes. |
| **Edit Spam** | Edits require gas + optional fee. No rate limit needed since fee creates natural cost barrier. |
| **Edit Detection** | `edited` flag and `edited_at` timestamp allow clients to clearly indicate edited posts to users. |
| **Edit Without Change** | `ErrNoContentChange` rejects no-op edits. Prevents fee-free timestamp refreshes via identical content. |
| **Thread Move Abuse** | Sentinels rate-limited (`max_sentinel_moves_per_epoch=10`). Reserved tag threads protected from sentinel moves. HR Committee retains full authority. |
| **Move Appeal Gaming** | `move_appeal_fee` (500 SPARK) discourages frivolous appeals. `move_appeal_cooldown` (1h) prevents instant coordination. |
| **Sentinel Move Escape** | `ThreadMoveRecord` snapshots bond at move time. Sentinel cannot unbond to avoid slash during appeal period. |
| **HR Move Appeal Path** | HR Committee moves cannot be appealed via `MsgAppealThreadMove` but CAN be appealed via `MsgAppealGovAction` with `GOV_ACTION_TYPE_THREAD_MOVE`. Thread author retains recourse while using proper channel. |
| **Archive With Pending Appeal** | `MsgFreezeThread` validates no pending `ThreadLockRecord` or `ThreadMoveRecord` appeals exist. Prevents archiving disputed threads which would destroy appeal state and allow sentinel/HR to escape accountability. Appeals must resolve before archive. |
| **Move Overturn Penalty** | Same escalating cooldown as hide/lock overturns. Bad moves count toward accuracy score. |
| **Flag Spam** | Non-members pay `flag_spam_tax`. Rate limit `max_flags_per_day` (20) prevents flooding. |
| **Flag Sybil** | Member flags weighted 2x non-member flags. Economic cost + rate limit makes Sybil attacks expensive. |
| **Flag State Bloat** | `max_post_flaggers` (50) caps flaggers list. `flag_expiration` (7d) ensures cleanup of unreviewed flags. |
| **Flag Brigading** | Weight threshold (`flag_review_threshold=5`) requires multiple flaggers. Single user cannot force review queue entry. |
| **False Flagging** | No penalty for dismissed flags (unlike downvotes). Flags are signals to sentinels, not assertions of guilt. Low barrier encourages reporting. |
| **Flag Queue Starvation** | `flag_review_queue` index allows sentinels to efficiently query posts needing attention. EndBlocker can prune expired flags. |
| **Thread Follow Spam** | Rate limit `max_follows_per_day` (50) prevents notification flooding. No economic reward for following. |
| **Bounty Self-Award** | `ErrCannotAwardSelf` prevents creators from awarding bounty to themselves. |
| **Bounty Abandonment** | `bounty_expiration_queue` ensures expired bounties are processed. Partial burn + top-reply award incentivizes awarding before expiry. |
| **Bounty Moderation Timer** | `time_remaining_at_suspension` captures exact time left when thread is moderated. Proper resume on thread restoration prevents timer manipulation or premature expiration. |
| **Bounty Gaming** | Creator chooses winner - no gaming vector. Bounty is their money; if they waste it on sycophants, that's their loss. |
| **Recurring Bounty Capture** | Manager rotation ensures no single person controls awards indefinitely. Group can replace managers via proposal. |
| **Manager Self-Award** | `ErrCannotAwardSelf` prevents current manager from awarding to themselves. |
| **Recurring Skip Abuse** | `recurring_skip_burn_ratio` (25%) burns pool on skips. After 3 consecutive skips, auto-pause. Managers have incentive to award. |
| **Pool Depletion** | Auto-transition to `DEPLETED` status when pool insufficient. Group notified via events. Easy top-up mechanism. |
| **Stale Recurring Bounty** | `consecutive_skips` tracking + auto-pause prevents forgotten bounties from lingering. |
| **Manager List Bloat** | `max_recurring_managers` (5) caps manager list size. |
| **Period History Bloat** | `max_period_history` (12) caps stored history. Older periods pruned. |
| **Non-Member Winners** | `members_only` flag allows groups to restrict awards to group members. |
| **Expired Bounty Fairness** | On expiry, top-upvoted reply gets remaining funds after burn. Encourages helpful replies even without guarantee. |

---

## 11.5. Invariants

> **Implementation status:** Module invariants are **not yet implemented** in the x/forum codebase. The design below describes the target invariants that should be registered.

The following invariants MUST hold at all times. Violations indicate bugs and should halt the chain via `InvariantCheck`.

### Balance Invariants

```go
// ModuleAccountSolvencyInvariant ensures module account can cover all obligations
func ModuleAccountSolvencyInvariant(k Keeper) sdk.Invariant {
    return func(ctx sdk.Context) (string, bool) {
        moduleBalance := k.bankKeeper.GetBalance(ctx, k.moduleAddress, "spark")

        // Sum all escrowed funds
        escrowedBounties := k.SumAllActiveBountyAmounts(ctx)
        escrowedRecurringPools := k.SumAllRecurringBountyPools(ctx)
        escrowedDownvotes := k.SumAllPendingDownvoteDeposits(ctx)
        escrowedAppealFees := k.SumAllPendingAppealFees(ctx)
        rewardPool := k.GetRewardPool(ctx)
        juryPool := k.GetJuryPool(ctx)

        totalObligations := escrowedBounties.Add(escrowedRecurringPools).
            Add(escrowedDownvotes).Add(escrowedAppealFees).
            Add(rewardPool.Amount).Add(juryPool.Amount)

        if moduleBalance.Amount.LT(totalObligations) {
            return sdk.FormatInvariant(types.ModuleName, "module_solvency",
                fmt.Sprintf("module balance %s < obligations %s", moduleBalance, totalObligations)), true
        }
        return "", false
    }
}
```

### Bond Commitment Invariants

```go
// SentinelBondCommitmentInvariant ensures committed bonds don't exceed actual bonds
func SentinelBondCommitmentInvariant(k Keeper) sdk.Invariant {
    return func(ctx sdk.Context) (string, bool) {
        for _, sentinel := range k.GetAllSentinels(ctx) {
            totalCommitted := sdk.NewIntFromString(sentinel.TotalCommittedBond)
            currentBond := sdk.NewIntFromString(sentinel.CurrentBond)
            if totalCommitted.GT(currentBond) {
                return sdk.FormatInvariant(types.ModuleName, "bond_commitment",
                    fmt.Sprintf("sentinel %s committed %s > bond %s",
                        sentinel.Address, totalCommitted, currentBond)), true
            }

            // Cross-check: pending_hide_count * slash_amount should approximate total_committed
            // (May differ slightly due to param changes mid-appeal, but should be close)
            params := k.GetParams(ctx)
            expectedCommitted := params.SentinelSlashAmount.Mul(sdk.NewInt(int64(sentinel.PendingHideCount)))
            if sentinel.PendingHideCount > 0 && totalCommitted.IsZero() {
                return sdk.FormatInvariant(types.ModuleName, "bond_commitment",
                    fmt.Sprintf("sentinel %s has %d pending hides but zero committed bond",
                        sentinel.Address, sentinel.PendingHideCount)), true
            }
        }
        return "", false
    }
}
```

### Tag Count Invariant

```go
// TagCountInvariant ensures total_tags matches actual tag count
func TagCountInvariant(k Keeper) sdk.Invariant {
    return func(ctx sdk.Context) (string, bool) {
        storedCount := k.GetTotalTags(ctx)
        actualCount := uint64(len(k.GetAllTags(ctx)))
        if storedCount != actualCount {
            return sdk.FormatInvariant(types.ModuleName, "tag_count",
                fmt.Sprintf("stored total_tags %d != actual %d", storedCount, actualCount)), true
        }
        return "", false
    }
}
```

### Pinned Post Invariant

```go
// PinnedPostLimitInvariant ensures pinned posts don't exceed category limits
func PinnedPostLimitInvariant(k Keeper) sdk.Invariant {
    return func(ctx sdk.Context) (string, bool) {
        params := k.GetParams(ctx)
        for _, category := range k.GetAllCategories(ctx) {
            pinnedCount := k.CountPinnedPostsInCategory(ctx, category.Id)
            if pinnedCount > params.MaxPinnedPerCategory {
                return sdk.FormatInvariant(types.ModuleName, "pinned_limit",
                    fmt.Sprintf("category %d has %d pinned > max %d",
                        category.Id, pinnedCount, params.MaxPinnedPerCategory)), true
            }
        }
        return "", false
    }
}
```

### Archive Metadata Invariant

```go
// ArchiveMetadataConsistencyInvariant ensures archive_count matches metadata
func ArchiveMetadataConsistencyInvariant(k Keeper) sdk.Invariant {
    return func(ctx sdk.Context) (string, bool) {
        // For archived threads, verify metadata exists and is consistent
        for _, archive := range k.GetAllArchivedThreads(ctx) {
            metadata, found := k.GetArchiveMetadata(ctx, archive.RootId)
            if !found {
                return sdk.FormatInvariant(types.ModuleName, "archive_metadata",
                    fmt.Sprintf("archived thread %d missing ArchiveMetadata", archive.RootId)), true
            }
            // archive_count in metadata is authoritative; Post.archive_count is deprecated
        }
        return "", false
    }
}
```

### Registering Invariants

```go
// RegisterInvariants registers all x/forum invariants
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
    ir.RegisterRoute(types.ModuleName, "module-solvency", ModuleAccountSolvencyInvariant(k))
    ir.RegisterRoute(types.ModuleName, "bond-commitment", SentinelBondCommitmentInvariant(k))
    ir.RegisterRoute(types.ModuleName, "tag-count", TagCountInvariant(k))
    ir.RegisterRoute(types.ModuleName, "pinned-limit", PinnedPostLimitInvariant(k))
    ir.RegisterRoute(types.ModuleName, "archive-metadata", ArchiveMetadataConsistencyInvariant(k))
}
```

---

## 12. Implementation Phases

> **Current status as of March 2026:** All phases through Phase 7 have their **messages and queries scaffolded and implemented**. The module has 50+ Msg RPCs and 80+ Query RPCs. Key areas still in design-only status:
> - EndBlocker: Only ephemeral post pruning implemented (no reward distribution, accuracy decay, or bounty expiration)
> - ForumHooks interface: Not yet implemented
> - OnInitiativeFinalized callback: Not yet implemented
> - Module invariants: Not yet implemented
> - Anonymous features (Phase 8): **Migrated to x/shield** — Per-module messages (`MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, `MsgAnonymousReact`) removed. Anonymous operations now use `x/shield`'s unified `MsgShieldedExec`. Forum implements `ShieldAware` interface (`IsShieldCompatible` returns `true` for `MsgCreatePost`, `MsgUpvotePost`, `MsgDownvotePost`).
> - Per-category `allow_anonymous` toggle: Design-only, not in Category proto
> - Many spec parameters not yet on-chain (hardcoded or via x/rep integration)

The x/forum module should be implemented in phases to manage complexity and allow incremental testing.

### Phase 1: Core Forum (Weeks 1-4) — IMPLEMENTED

**Proto & Types:**
- Post, Category, Tag messages
- UserRateLimit, SentinelActivity
- Basic Params (spam_tax, rate limits, content limits)

**Messages:**
- MsgCreateCategory (HR Committee)
- MsgCreatePost (with spam tax, rate limiting, ephemeral handling)
- MsgDeletePost (author only)

**State Management:**
- Post CRUD with ephemeral post tracking
- Category management
- Basic rate limiting (epoch-based)

**EndBlocker:**
- Ephemeral post pruning (lazy, limited per block)

### Phase 2: Moderation System (Weeks 5-8) — IMPLEMENTED

**Proto & Types:**
- HideRecord, ThreadLockRecord
- Sentinel bond status tracking
- Appeal-related state

**Messages:**
- MsgHidePost, MsgAppealPost (author appeal of sentinel hide)
- MsgAppealHide (post author → x/rep initiative)
- MsgLockThread, MsgUnlockThread (sentinel/HR)
- MsgAppealThreadLock
- Sentinel bonding: `MsgBondRole` / `MsgUnbondRole` (x/rep, with `role_type = ROLE_TYPE_FORUM_SENTINEL`)

**Integration:**
- x/rep: Sentinel bond/backing checks
- x/rep: Initiative creation for appeals

**EndBlocker:**
- Hidden post expiration
- Appeal timeout handling
- Sentinel accuracy calculation

### Phase 3: Tag System & Archival (Weeks 9-12) — IMPLEMENTED

**Proto & Types:**
- Tag with expiration tracking
- ArchivedThread
- TagReport

**Messages:**
- Tag creation (implicit via MsgCreatePost)
- MsgArchiveThread, MsgUnarchiveThread
- MsgReportTag, MsgRemoveTag (HR Committee)

**EndBlocker:**
- Tag expiration queue processing
- Archive eligibility checking

**State Management:**
- Expiration queues (tags, archives)
- Tag usage tracking

### Phase 4: Reactions & Editing (Weeks 13-16) — PARTIALLY IMPLEMENTED

**Proto & Types:**
- ReactionRecord, ReactionType (AnonymousReactionMetadata removed — private reactions via x/shield)
- UserReactionLimit (unified budget)

**Messages:**
- MsgUpvotePost (one-per-target, stores ReactionRecord)
- MsgDownvotePost (one-per-target, stores ReactionRecord)
- ~~MsgAnonymousReact~~ (REMOVED — private reactions now routed via `x/shield`'s `MsgShieldedExec`)
- MsgEditPost

**State Management:**
- Reaction records per post per user (public: ReactionRecord; private: nullifier managed by x/shield)
- Aggregate counters on Post (upvote_count, downvote_count — maintained alongside records)
- Unified daily rate limits (UserReactionLimit)
- Reaction record cleanup on thread archival (§16.15)
- Edit history (minimal - just edited flag)

### Phase 5: Member Reporting & HR Appeals (Weeks 17-20) — IMPLEMENTED

**Proto & Types:**
- MemberReport, MemberWarning
- GovActionAppeal
- MemberSalvationStatus

**Messages:**
- MsgReportMember, MsgCoSignReport, MsgSubmitDefense
- MsgResolveReport, MsgIssueDemotion, MsgIssueZeroing
- MsgAppealGovAction

**Integration:**
- x/rep: Member status checks
- x/rep: Jury initiative for HR appeals

### Phase 6: Bounty System (Weeks 21-24) — IMPLEMENTED

**Proto & Types:**
- Bounty, BountyAward
- RecurringBounty, RecurringBountyPeriod

**Messages:**
- MsgCreateBounty, MsgAwardBounty, MsgCancelBounty
- MsgCreateRecurringBounty, MsgAwardRecurringBounty
- MsgSkipRecurringBountyPeriod, MsgTopUpRecurringBounty
- MsgPauseRecurringBounty, MsgCancelRecurringBounty

**EndBlocker:**
- Bounty expiration processing
- Recurring period rotation

### Phase 7: Thread Organization (Weeks 25-28) — IMPLEMENTED

**Proto & Types:**
- ThreadMoveRecord
- ThreadMetadata (pinned/accepted replies)
- PostFlag

**Messages:**
- MsgMoveThread, MsgAppealThreadMove
- MsgPinReply, MsgUnpinReply, MsgMarkAcceptedReply
- MsgFlagPost, MsgDismissFlags

**Integration:**
- x/rep: Flag weight by membership status

---

## 13. Safe Parameter Ranges

The following ranges are recommended for production deployment. Values outside these ranges may cause economic exploits, state bloat, or poor user experience.

### Economics

| Parameter | Safe Range | Default | Notes |
|-----------|------------|---------|-------|
| `spam_tax` | 10-500 SPARK | 50 SPARK | Too low enables spam; too high excludes legitimate non-members |
| `appeal_fee` | 100-2000 SPARK | 500 SPARK | Must be high enough to deter frivolous appeals |
| `tag_creation_fee` | 50-500 SPARK | 100 SPARK | Balance between accessibility and anti-squatting |
| `sentinel_reward_ratio` | 0.30-0.70 | 0.50 | Portion of taxes to sentinel rewards |
| `jury_share_ratio` | — | — | Removed — jurors compensated via x/rep DREAM minting |
| `min_bounty_amount` | 10-1000 SPARK | 50 SPARK | Too low enables spam bounties |
| `bounty_moderation_fee_ratio` | 0.01-0.10 | 0.025 | Penalty for bounties on moderated content |
| `recurring_manager_fee_ratio` | 0.03-0.10 | 0.05 | Manager incentive |
| `recurring_skip_burn_ratio` | 0.10-0.50 | 0.25 | Penalty for skipping periods |

### Authority & Bonding

| Parameter | Safe Range | Default | Notes |
|-----------|------------|---------|-------|
| `min_sentinel_bond` | 500-5000 DREAM | 1000 DREAM | Self-bonded stake for skin-in-the-game |
| `min_sentinel_backing` | 5000-50000 DREAM | 10000 DREAM | Community trust requirement |
| `min_backer_membership_duration` | 7d-90d | 30d (2592000s) | Sybil protection; too low enables fake backing |
| `sentinel_slash_amount` | 50-500 DREAM | 100 DREAM | Per-overturn penalty |
| `sentinel_demotion_threshold` | 250-750 DREAM | 500 DREAM | Must be < min_sentinel_bond |
| `min_sentinel_accuracy` | 0.50-0.90 | 0.70 | Accuracy for reward eligibility |
| `min_appeal_rate` | 0.05-0.20 | 0.10 | Min appeals for accuracy to count |
| `curation_dream_reward` | 2-20 DREAM | 5 DREAM | Reward per successful pin/proposal |
| `curation_slash_amount` | 25-100 DREAM | 50 DREAM | Slash if pin dispute lost |
| `pin_dispute_fee` | 50-500 SPARK | 100 SPARK | Fee to dispute sentinel pin |

### Rate Limiting

| Parameter | Safe Range | Default | Notes |
|-----------|------------|---------|-------|
| `daily_post_limit` | 10-100 | 50 | Per-user posts per 24h |
| `max_hides_per_epoch` | 20-100 | 50 | Per-sentinel hide limit |
| `max_reactions_per_day` | 50-500 | 100 | Unified per-user limit (all reaction types, public + private) |
| `max_salvations_per_day` | 5-50 | 10 | Anti-collusion for post salvation |
| `max_flags_per_day` | 5-50 | 10 | Per-user flag limit |
| `max_follows_per_day` | 10-100 | 50 | Per-user thread follow limit |

### Conviction Propagation

| Parameter | Safe Range | Default | Notes |
|-----------|------------|---------|-------|
| `conviction_renewal_threshold` | 0-1000.0 | 100.0 | Min conviction to renew; 0 disables renewal |
| `conviction_renewal_period` | 1d-30d | 7d (604800s) | TTL extension on renewal; should match `ephemeral_ttl` |

*Note: `conviction_propagation_ratio` is an x/rep parameter (default 0.10). Safe range: 0.01-0.25. Too high allows content conviction to dominate initiative scoring; too low makes propagation ineffective.*

### Timing

| Parameter | Safe Range | Default | Notes |
|-----------|------------|---------|-------|
| `ephemeral_ttl` | 12h-72h | 24h (86400s) | Ephemeral post lifespan |
| `hidden_expiration` | 3d-14d | 7d (604800s) | Time before hidden post deletes |
| `appeal_deadline` | 7d-30d | 14d (1209600s) | Max time for jury verdict |
| `archive_threshold` | 14d-90d | 30d (2592000s) | Inactivity before archive eligible |
| `tag_expiration` | 14d-90d | 30d (2592000s) | Unused tag TTL |
| `edit_window` | 12h-72h | 24h (86400s) | Post edit window |
| `edit_grace_period` | 2m-15m | 5m (300s) | Free edit window |
| `bounty_duration` | 3d-30d | 7d (604800s) | Default bounty expiration |
| `accept_proposal_timeout` | 24h-7d | 48h (172800s) | Time before sentinel proposal auto-confirms |

### Limits

| Parameter | Safe Range | Default | Notes |
|-----------|------------|---------|-------|
| `max_content_size` | 5KB-50KB | 10KB (10240 bytes) | Post content limit |
| `max_reply_depth` | 5-20 | 10 | Max nesting depth; prevents deep recursion |
| `max_tags_per_post` | 3-10 | 5 | Tags per post |
| `max_total_tags` | 1000-50000 | 10000 | System-wide tag limit |
| `max_pinned_per_category` | 3-10 | 5 | Pinned threads per category |
| `max_pinned_replies_per_thread` | 1-5 | 3 | Pinned replies per thread |
| `max_bounty_winners` | 1-10 | 5 | Winners per bounty |
| `max_recurring_managers` | 2-10 | 5 | Managers per recurring bounty |
| `max_recurring_winners_per_period` | 1-20 | 10 | Winners per period |

### XP Integration

XP-related parameters have moved to x/season. The forum module now uses a hooks interface to signal engagement events without defining reward amounts. See section 7.5 for the ForumHooks interface.

---

## 14. Expected Gas Costs

Approximate gas costs for common operations. Actual costs depend on state size and input complexity.

### Content Operations

| Operation | Gas (approx) | Notes |
|-----------|-------------|-------|
| `MsgCreatePost` (root) | 80,000-120,000 | Higher with new tags |
| `MsgCreatePost` (reply) | 60,000-100,000 | + salvation checks |
| `MsgDeletePost` | 40,000-60,000 | Soft delete |
| `MsgEditPost` | 50,000-80,000 | Content size dependent |

### Moderation Operations

| Operation | Gas (approx) | Notes |
|-----------|-------------|-------|
| `MsgHidePost` | 70,000-100,000 | Creates HideRecord |
| `MsgAppealPost` | 60,000-80,000 | Creates x/rep appeal initiative |
| `MsgAppealHide` | 100,000-150,000 | Creates x/rep initiative |
| `MsgLockThread` | 60,000-80,000 | Creates ThreadLockRecord |
| `MsgUnlockThread` | 50,000-70,000 | Clears lock state |

### Sentinel Operations

| Operation | Gas (approx) | Notes |
|-----------|-------------|-------|
| `MsgBondRole` (FORUM_SENTINEL) | 80,000-120,000 | Initial bond (lives in x/rep) |
| `MsgUnbondRole` (FORUM_SENTINEL) | 60,000-100,000 | + pending-action commit check (lives in x/rep) |
| `MsgDelegateSentinel` | 50,000-80,000 | Backing delegation |

### Reactions (Counter-Only)

| Operation | Gas (approx) | Notes |
|-----------|-------------|-------|
| `MsgUpvotePost` | 40,000-60,000 | + rate limit check, counter increment |
| `MsgDownvotePost` | 50,000-80,000 | + deposit burn, counter increment |

### Bounty Operations

| Operation | Gas (approx) | Notes |
|-----------|-------------|-------|
| `MsgCreateBounty` | 80,000-120,000 | + escrow transfer |
| `MsgAwardBounty` | 100,000-200,000 | Per-winner transfers |
| `MsgCancelBounty` | 60,000-80,000 | + refund |
| `MsgCreateRecurringBounty` | 120,000-180,000 | Pool escrow |
| `MsgAwardRecurringBounty` | 150,000-300,000 | Multi-winner + rotation |

### Thread Organization

| Operation | Gas (approx) | Notes |
|-----------|-------------|-------|
| `MsgMoveThread` | 60,000-100,000 | Category change |
| `MsgPinReply` | 40,000-60,000 | |
| `MsgUnpinReply` | 30,000-50,000 | |
| `MsgMarkAcceptedReply` | 40,000-60,000 | |
| `MsgFlagPost` | 40,000-60,000 | + rate limit check |

### Archive Operations

| Operation | Gas (approx) | Notes |
|-----------|-------------|-------|
| `MsgArchiveThread` | 200,000-500,000 | Compression + storage |
| `MsgUnarchiveThread` | 150,000-400,000 | Decompression |

### EndBlocker Costs (per block)

| Operation | Gas (approx) | Notes |
|-----------|-------------|-------|
| Ephemeral pruning | 20,000-50,000 | Per post (lazy_prune_limit) |
| Tag expiration | 10,000-30,000 | Per tag |
| Bounty expiration | 50,000-100,000 | Per bounty |
| Reward distribution | 100,000-200,000 | Per epoch (accumulated) |

---

## 15. Client Integration: Session Keys

For fluid user interactions without repeated wallet popups, frontends should implement the **session key pattern** using `x/authz` and `x/feegrant`. This allows users to approve a single grant, after which all forum actions (posting, replying, voting, flagging) are auto-signed by an ephemeral session key.

See **[docs/session-keys.md](session-keys.md)** for the full specification, grant scoping recommendations, fee delegation setup, and security considerations.

---

## 16. Anonymous Features via x/shield

> **Implementation status (March 2026):** Per-module anonymous messages (`MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, `MsgAnonymousReact`) have been **REMOVED**. All anonymous operations are now routed through `x/shield`'s unified `MsgShieldedExec` entry point. The forum keeper implements the `ShieldAware` interface (see `x/forum/keeper/shield_aware.go`) to declare which messages are shield-compatible: `MsgCreatePost`, `MsgUpvotePost`, `MsgDownvotePost`. ZK proof verification (PLONK over BN254), nullifier management, TLE infrastructure, and module-paid gas are all owned by x/shield. The anonymous posting subsidy system has also been removed (replaced by x/shield's module-paid gas model). See `docs/x-shield-spec.md` for the unified privacy architecture.

Members can create forum posts, replies, and reactions without revealing their identity via `x/shield`'s `MsgShieldedExec`. The member proves they meet a minimum trust level — without revealing *which* member they are. Nullifiers prevent spam and are managed centrally by x/shield with per-domain scoping. Two execution modes are available: **Immediate** (low latency, content visible on-chain) and **Encrypted Batch** (TLE + batching for maximum privacy).

See **[docs/x-shield-spec.md](x-shield-spec.md)** for the full specification covering ZK proof verification, TLE infrastructure, nullifier management, shielded execution modes, and module-paid gas.

### 16.1. Dependencies

| Module | Purpose |
|--------|---------|
| `x/shield` | Owns all ZK proof verification (PLONK/BN254), nullifier management, TLE infrastructure, and module-paid gas. Dispatches shielded operations to forum via the `ShieldAware` interface. |
| `x/rep` | `RepKeeper.GetMemberTrustTreeRoot()` — Merkle root for trust-level proofs (read by x/shield during proof verification) |

x/forum does NOT depend on x/shield directly. Instead, x/shield calls into forum when processing a `MsgShieldedExec` that wraps a forum message. The forum keeper declares shield-compatible messages via the `ShieldAware` interface (`IsShieldCompatible()`). All ZK proof verification, nullifier deduplication, and trust tree root validation are performed by x/shield before dispatching to the forum keeper.

> **ShieldAware interface** (`x/forum/keeper/shield_aware.go`): The forum keeper implements `IsShieldCompatible(ctx, msg) bool` which returns `true` for `MsgCreatePost`, `MsgUpvotePost`, and `MsgDownvotePost`. These are the three operations that can be executed anonymously via x/shield.

### 16.2. Forum-Specific Nullifier Domains (Managed by x/shield)

> **Note:** Nullifier domains are now registered in x/shield via `MsgRegisterShieldedOp` and managed centrally. The per-module `AnonNullifier/` stores have been removed.

| Domain | Action | Scope | Effect |
|--------|--------|-------|--------|
| `11` | Shielded post (MsgCreatePost) | Current epoch | One anonymous thread per member per epoch |
| `12` | Shielded upvote (MsgUpvotePost) | `post_id` | One anonymous upvote per member per post |
| `13` | Shielded downvote (MsgDownvotePost) | `post_id` | One anonymous downvote per member per post |

All domains use `PROOF_DOMAIN_TRUST_TREE` with `min_trust_level=1` and `batch_mode=EITHER`. Nullifier storage and deduplication are handled by x/shield's centralized nullifier store with per-domain scoping.

### 16.3. Per-Category Anonymous Toggle

> **Implementation status:** This feature is **design-only**. The `allow_anonymous` field does NOT exist in the actual `Category` proto (`proto/sparkdream/forum/v1/category.proto`). The `anonymous_posting_enabled` parameter also does not exist in the `Params` proto. Anonymous operations are currently handled entirely at the x/shield layer without per-category gating. The design below is preserved for future implementation.

Anonymous posting is gated per-category rather than a single global flag. This allows governance to enable anonymity in categories where it makes sense (governance, feedback, whistleblowing) while keeping it off in others (support, introductions).

New field on `Category` (design target):

```protobuf
message Category {
  // ... existing fields ...
  bool allow_anonymous = 6;   // Whether anonymous posts are permitted in this category
}
```

The global `anonymous_posting_enabled` param acts as a master kill-switch. If false, anonymous posting is disabled in all categories regardless of `allow_anonymous`. If true, the per-category flag controls.

`MsgCreateCategory` and `MsgUpdateCategory` gain an `allow_anonymous` field, set by the HR Committee.

### 16.4. Messages

> **REMOVED:** The per-module anonymous messages `MsgCreateAnonymousPost`, `MsgCreateAnonymousReply`, and `MsgAnonymousReact` have been **deleted** from `tx.proto`. Anonymous operations are now executed by wrapping standard forum messages (`MsgCreatePost`, `MsgUpvotePost`, `MsgDownvotePost`) inside `x/shield`'s `MsgShieldedExec`.
>
> **How it works:** A user submits `MsgShieldedExec` containing a ZK proof and the inner message (e.g., `MsgCreatePost`). x/shield verifies the proof, checks the nullifier, and dispatches the inner message to the forum keeper with the `creator` field set to the shield module account address. The forum keeper processes it as a normal message — the anonymous identity is enforced at the shield layer, not the forum layer.
>
> **Registered shielded operations for x/forum:**
>
> | Inner Message | Domain | Scope | Proof Domain | Min Trust Level | Batch Mode |
> |--------------|--------|-------|-------------|----------------|------------|
> | `MsgCreatePost` | 11 | epoch-scoped | `PROOF_DOMAIN_TRUST_TREE` | 1 | EITHER |
> | `MsgUpvotePost` | 12 | `post_id`-scoped | `PROOF_DOMAIN_TRUST_TREE` | 1 | EITHER |
> | `MsgDownvotePost` | 13 | `post_id`-scoped | `PROOF_DOMAIN_TRUST_TREE` | 1 | EITHER |
>
> See `x/shield/keeper/registration.go` and `docs/x-shield-spec.md` for details on `MsgShieldedExec` processing.

### 16.5. State Objects

> **REMOVED:** The `AnonymousPostMetadata` proto and the per-module `AnonMeta/` and `AnonNullifier/` state stores have been **deleted** from x/forum. Nullifier tracking and anonymous action metadata are now managed centrally by x/shield's nullifier store with per-domain scoping.
>
> The `AnonymousReactionMetadata` type has also been removed — reaction anonymity is handled entirely at the x/shield layer.
>
> See `x/shield/keeper/nullifier.go` for centralized nullifier management and `docs/x-shield-spec.md` for the nullifier scoping model.

### 16.6. Parameters

> **Implementation status:** The `anonymous_posting_enabled` parameter does **not** exist in the current `Params` proto. Anonymous feature control is handled entirely at the x/shield layer. The design below is preserved for potential future per-module opt-out.

Forum-level parameters controlling anonymous feature availability (design target):

```protobuf
bool anonymous_posting_enabled = N;       // Master toggle — if false, x/shield rejects shielded forum ops (default: true)
```

ZK proof parameters (min trust level, proof domain, verification keys) are configured in x/shield via `MsgRegisterShieldedOp` and stored in x/shield's state. The per-operation `min_trust_level` is set to `1` for all forum shielded ops (see Section 16.4).

### 16.7. Access Control

| Operation | Who Can Execute |
|-----------|-----------------|
| Shielded MsgCreatePost | Any user via `MsgShieldedExec`; ZK proof verified by x/shield. (Design target: category must have `allow_anonymous = true`, but this field is not yet implemented.) |
| Shielded MsgUpvotePost | Any user via `MsgShieldedExec`; ZK proof verified by x/shield |
| Shielded MsgDownvotePost | Any user via `MsgShieldedExec`; ZK proof verified by x/shield |
| Update anonymous post/reply | **Nobody** — anonymous content is immutable (no edit) |
| Delete anonymous post/reply | **Nobody** — author is shield module account, cannot sign |
| Hide anonymous post/reply | **Sentinels** — standard sentinel flow applies (bond commitment, appeal window) |
| Flag anonymous post/reply | **Members** — standard flag flow applies |

Unlike x/blog where anonymous content is fully unmoderable, x/forum's sentinel system provides moderation for anonymous content. Sentinels can hide anonymous posts through the standard `MsgHidePost` flow — they commit bond, the hide is subject to appeal, and overturned hides result in slashing. This is the key advantage of forum over blog for anonymous content.

### 16.8. Moderation of Anonymous Content

Anonymous posts interact with the sentinel moderation system normally with one exception: **there is no author to notify or penalize**.

- **Hiding:** Sentinels hide anonymous posts the same way as regular posts. The `hidden_by` field records the sentinel.
- **Appeals:** Since the anonymous author cannot appeal their own post (module account can't sign), appeals must come from other members. Any member can appeal a hidden anonymous post via `MsgAppealPost`.
- **Reputation impact:** No reputation deduction on the anonymous author if a hide is upheld (identity unknown). The deterrent is the nullifier (managed by x/shield) — the anonymous author's one post/reply for that scope is gone.
- **Flagging:** Members flag anonymous posts normally. The flag weight system and review queue work identically.

### 16.9. Content Lifecycle

Anonymous posts differ from regular posts in lifecycle:

| Aspect | Regular Post | Anonymous Post |
|--------|-------------|----------------|
| Storage | Ephemeral (non-member) or Permanent (member) | Always Permanent |
| Editable | Yes (by author) | No (immutable) |
| Deletable | Yes (by author) | No (module account can't sign) |
| Hideable by sentinel | Yes | Yes |
| Appealable | By author or any member | By any member |
| Archivable | Yes (after inactivity) | Yes (same rules) |
| TTL pruning | Applies to ephemeral | Does not apply (permanent) |
| Initiative reference | Optional (via `initiative_id`) | Optional (via `initiative_id`) |
| Conviction propagation | Yes (if initiative linked) | Yes (if initiative linked) |

Anonymous posts are always permanent because ephemeral posts require author interaction (member replies to "promote" them), and the anonymous author's identity is unknown. x/shield's nullifier scoping (one per epoch/thread) and per-identity rate limiting prevent abuse of permanent storage.

**Conviction propagation:** Both regular and anonymous posts can reference an x/rep initiative via `initiative_id`. When a post accumulates community conviction stakes (via `STAKE_TARGET_FORUM_CONTENT`), x/rep's `GetPropagatedConviction()` multiplies the content's total conviction by `conviction_propagation_ratio` (default 10%) and adds it as external conviction to the referenced initiative. This creates a virtuous cycle: popular discussion content accelerates the linked initiative's completion.

**Conviction renewal for ephemeral posts:** When an ephemeral (non-member) post with an initiative reference reaches its TTL expiry, the EndBlocker checks if the post's conviction score meets `conviction_renewal_threshold`. If so, the post enters "conviction-sustained" state — its TTL is extended by `conviction_renewal_period` and it survives garbage collection. This allows community-valued ephemeral content to persist as long as it maintains conviction support. See §7.2.1 for details.

### 16.10. Queries

> **REMOVED:** The `AnonymousPostMeta`, `AnonymousReplyMeta`, and `IsNullifierUsed` query endpoints have been **deleted** from x/forum's `query.proto`. Nullifier status can be queried via x/shield's `QueryIsNullifierUsed` endpoint. Anonymous post metadata is no longer stored in x/forum state.

### 16.11. Events

> **REMOVED:** The `forum.anonymous_post.created` and `forum.anonymous_reply.created` events are no longer emitted by x/forum. When a shielded operation is dispatched, x/shield emits shield-level events (including `shield.shielded_exec.dispatched`), and the forum keeper emits standard events for the inner message (e.g., `forum.post.created`, `forum.upvote`, `forum.downvote`). The `creator` field in forum events will contain the shield module account address for anonymous operations.

### 16.12. Security Considerations

- **Anonymity set size:** The anonymity set equals all active members at or above the proven trust level. With `min_trust_level=1`, this includes most active members, providing strong anonymity.
- **Nullifier unlinkability:** Nullifiers from different scopes (different posts, different epochs) cannot be correlated to the same member. Nullifier management is centralized in x/shield.
- **Module-paid gas:** x/shield's module account pays transaction fees for shielded operations, so the submitter needs zero balance. This eliminates balance-based deanonymization attacks.
- **Encrypted Batch mode:** For maximum privacy, users can submit via TLE-encrypted batch mode. Content is encrypted until the epoch decryption key is released, preventing transaction ordering analysis.
- **Spam prevention:** Per-identity rate limiting in x/shield + nullifier scoping (one per epoch/post_id) + forum-level rate limits.
- **No edit/delete as feature:** Immutability prevents behavioral deanonymization (edit timing, deletion patterns).
- **Sentinel moderation preserved:** Unlike x/blog, forum anonymous posts are subject to full sentinel moderation, preventing abuse without sacrificing anonymity.
- **Admin-only categories excluded:** (Design target) Categories with `admin_only_write = true` should not allow anonymous posting, preventing impersonation of governance authority. The `allow_anonymous` field is not yet implemented.

### 16.13. Anonymous Posting Subsidy

> **REMOVED:** The per-module anonymous posting subsidy system (`anon_subsidy_budget_per_epoch`, `anon_subsidy_max_per_post`, `anon_subsidy_relay_addresses`) has been **deleted**. This is replaced by x/shield's **module-paid gas** model: the shield module account pays transaction fees for all `MsgShieldedExec` operations, funded automatically from the community pool via BeginBlocker with a governance-controlled epoch cap (`max_funding_per_epoch`). Per-identity rate limiting in x/shield prevents gas abuse. See `docs/x-shield-spec.md` for the funding model.

### 16.14. Anonymous Reactions (Private Upvote/Downvote)

> **REMOVED:** `MsgAnonymousReact` has been **deleted** from `tx.proto`. Private reactions are now executed by wrapping `MsgUpvotePost` (domain 12) or `MsgDownvotePost` (domain 13) inside `x/shield`'s `MsgShieldedExec`.

Members can upvote or downvote posts without revealing their identity via x/shield's `MsgShieldedExec`. A nullifier scoped to the `post_id` (managed by x/shield) enforces one reaction per member per post — the same constraint as public reactions.

**Why this matters:** On-chain transactions reveal the voter's address. Even though the current system stores individual `ReactionRecord`s for public reactions, users who want voting privacy comparable to X/Twitter's private likes can use the shielded execution mode. x/shield's module-paid gas eliminates any balance-based correlation.

**Parity with public reactions:**

| Property | Public | Private (via x/shield) |
|----------|--------|---------|
| One reaction per user per post | `ReactionRecord` keyed storage | ZK nullifier managed by x/shield (domain 12/13, scope=post_id) |
| Non-removable | No removal message | Nullifier permanent |
| Daily budget | `max_reactions_per_day` | Same (shared budget) |
| Downvote cost | `downvote_deposit` burned | Same (charged to shield module account) |
| Voter identity | Stored in `ReactionRecord` | Hidden by ZK proof |

### 16.15. Reaction Aggregation on Thread Archival

When a thread is archived via `MsgFreezeThread`, individual reaction records are deleted and only the aggregate counters on each Post are preserved in the compressed archive. This saves significant storage on long-lived threads.

**Added to MsgFreezeThread logic (step 2, before compression):**

1. For each post in the thread:
   - Delete all `reaction_records/{post_id}/*` (public reaction records)
   - The `upvote_count` and `downvote_count` on the Post object are already correct aggregate counters and are preserved in the compressed archive
   - Note: Private reaction nullifiers are stored in x/shield's centralized store and are pruned by x/shield's own lifecycle management

2. **Effect:** After archival, per-user reaction uniqueness data is lost. If the thread is later unarchived, users could theoretically re-react to posts. This is acceptable because:
   - Archival is for inactive threads (no recent activity)
   - Unarchival is rare and gas-expensive
   - The aggregate counts are the data that matters for display
   - Re-reactions would still be subject to the daily rate limit

**Storage savings:** For a thread with 200 posts averaging 15 reactions each, this deletes ~3,000 ReactionRecord entries on archival.

### 16.16. Cross-Module Conviction Propagation

Forum posts (both regular and anonymous) can optionally reference an x/rep initiative by setting `initiative_id` at creation time. When a post accumulates community conviction stakes, a fraction of that conviction propagates back to the referenced initiative, boosting its completion score. This creates a virtuous feedback loop: popular discussion content about an initiative accelerates the initiative's progress.

#### How It Works

1. **Post creation:** When a post is created with `initiative_id > 0`, the keeper validates the initiative exists (via `repKeeper.ValidateInitiativeReference()`) and registers a content→initiative link (via `repKeeper.RegisterContentInitiativeLink(ctx, initiative_id, STAKE_TARGET_FORUM_CONTENT, post_id)`).

2. **Conviction staking:** Community members stake DREAM on forum content via x/rep's `Stake` message with `target_type = STAKE_TARGET_FORUM_CONTENT` and `target_id = post_id`. Conviction is time-weighted: `conviction = amount * min(1, t / (2 * half_life))` where `half_life = 14 epochs`.

3. **Propagation:** When x/rep evaluates an initiative's completion, it calls `GetPropagatedConviction(initiativeID)` which:
   - Iterates all content items linked to the initiative (across blog and forum modules)
   - For each linked item, queries `GetContentConviction()` to get its current conviction score
   - Sums all content conviction and multiplies by `conviction_propagation_ratio` (default 10%, configurable via x/rep params)
   - The propagated conviction counts as **external conviction** on the initiative (satisfying the 50% external requirement)

4. **Link cleanup:** When a post is tombstoned (TTL expiry), soft-deleted, or hidden-expired, the keeper calls `repKeeper.RemoveContentInitiativeLink()` to clean up the propagation link.

#### Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `conviction_renewal_threshold` | 100.0 | Minimum conviction score to renew ephemeral content at TTL expiry (0 = disabled) |
| `conviction_renewal_period` | 604,800 (7 days) | Duration in seconds to extend TTL by when conviction-renewed |

*Note: `conviction_propagation_ratio` is an x/rep parameter (default 0.10 = 10%), not an x/forum parameter. It applies uniformly across all content modules (blog, forum, collect).*

#### Conviction Renewal (Ephemeral Posts Only)

Ephemeral posts (created by non-members) normally expire after `ephemeral_ttl` (24h). If an ephemeral post references an initiative and has accumulated sufficient community conviction by its TTL expiry, the EndBlocker extends its TTL instead of tombstoning it:

- **First time:** Post enters "conviction-sustained" state (`conviction_sustained = true`), TTL extended by `conviction_renewal_period`. Emits `forum.post.conviction_sustained` event.
- **Subsequent renewals:** If conviction still meets threshold at next expiry, TTL extended again. Emits `forum.post.renewed` event.
- **Conviction drop:** If conviction falls below threshold at expiry, `conviction_sustained` is cleared and the post is tombstoned normally.

This allows community-valued ephemeral content to persist as long as it maintains conviction support, without requiring the anonymous author to interact.

*Note: Permanent posts (member-created or anonymous) do not need conviction renewal — they already persist indefinitely. However, they still benefit from conviction propagation to linked initiatives.*

#### RepKeeper Interface Additions

The following methods must be added to `RepKeeper` in `x/forum/types/expected_keepers.go`:

```go
// Conviction propagation (initiative ↔ content linking)
ValidateInitiativeReference(ctx context.Context, initiativeID uint64) error
RegisterContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType reptypes.StakeTargetType, targetID uint64) error
RemoveContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType reptypes.StakeTargetType, targetID uint64) error
```

#### Example Flow

```
1. Alice creates a thread discussing Initiative #42 (improve documentation):
   MsgCreatePost{creator: alice, initiative_id: 42, content: "I think we should..."}

2. Bob stakes 500 DREAM on Alice's post (believes it's valuable content):
   x/rep MsgStake{staker: bob, target_type: STAKE_TARGET_FORUM_CONTENT, target_id: 1001, amount: 500}

3. After 14 epochs, Bob's stake reaches full conviction:
   content_conviction = 500 DREAM

4. When Initiative #42 is evaluated for completion:
   propagated_conviction = 500 * 0.10 = 50 DREAM (counts as external conviction on Initiative #42)

5. Combined with direct initiative stakes, this helps Initiative #42 reach completion threshold.
```

#### Interaction with Other Features

| Feature | Interaction |
|---------|-------------|
| **Author bonds** | Author bonds (`CreateAuthorBond`) are separate from content conviction stakes. An author can bond DREAM and the community can also conviction-stake — these are independent mechanisms. |
| **Anonymous posts** | Anonymous posts can reference initiatives. The anonymous author cannot stake on their own content (identity hidden), so all conviction comes from other community members. |
| **Thread archival** | When a thread is archived via `MsgFreezeThread`, initiative links for all posts in the thread are removed (conviction no longer propagates). If unarchived, links must be re-established. |
| **Post hiding** | Hiding a post does NOT remove the initiative link — the post may be restored on appeal. Link is only removed on tombstone (permanent deletion). |
| **Post deletion** | Soft-deleting a post removes the initiative link immediately (author chose to remove their content). |
| **Sentinel moderation** | If a sentinel hides a post that references an initiative, the link is preserved during the appeal window. If the hide expires (post tombstoned), the link is cleaned up. |

---

## 17. Future Considerations

- **Nested Categories:** Allow sub-categories for better organization
- **Delegation Proxy:** Allow Sentinels to delegate moderation to trusted accounts
- **Cross-Chain Moderation:** Allow sentinel activity across IBC-connected forums
- **Appeal Escalation:** Multi-tier appeal system for high-stakes decisions
- **Sentinel Insurance:** Pool to compensate wrongly-slashed sentinels on successful meta-appeal