# `x/forum`

The `x/forum` module implements a decentralized, censorship-resistant discussion platform with hierarchical content organization, dual-token sentinel moderation, bounties, anonymous posting via ZK proofs, and conviction-based content persistence.

## Overview

This module provides:

- **Hierarchical content** — governance-controlled categories and member-created dynamic tags
- **Dual-token moderation** — sentinels stake DREAM as collateral for moderation authority; earn rewards for accurate decisions
- **Content lifecycle** — ephemeral TTL for non-member posts, permanent storage for members, archival for inactive threads
- **Bounties and tag budgets** — economic incentives for quality content
- **Anonymous posting** — ZK-proof-based anonymous posts, replies, and reactions
- **Conviction renewal** — posts linked to `x/rep` initiatives can extend their TTL based on community conviction staking
- **Author bonds** — optional DREAM bonds on content creation, slashable via challenges
- **Appeals system** — jury-based appeals via `x/rep` for overturning moderation actions

## Concepts

### Content Lifecycle

| Type | Author | Storage | Behavior |
|------|--------|---------|----------|
| **Ephemeral** | Non-member | TTL-based (24h default) | Pruned if not replied to within TTL |
| **Permanent** | Member | Indefinite | Stored permanently in IAVL tree |
| **Archived** | Any | Compressed | Threads inactive >30 days compressed |

### Post Status

```
ACTIVE ◄─── MsgUnhidePost ─── HIDDEN ◄── MsgHidePost (sentinel)
  │                                           │
  ├── MsgDeletePost ──► DELETED               ├── Appeal filed → Jury review
  │                                           │
  └── TTL expiry ──► DELETED                  └── Unappealed → Content deleted
```

### Sentinel System

Sentinels are reputation-bearing members who stake DREAM bonds to moderate content:

- **NORMAL** (>= 1000 DREAM): full moderation privileges
- **RECOVERY** (500-999 DREAM): can moderate; rewards auto-bonded until restored
- **DEMOTED** (< 500 DREAM): loses privileges, must re-bond

**Slashing**: 100 DREAM per overturned appeal (fixed amount). Consecutive overturns escalate cooldown (24h to 7 days). 5+ consecutive slashes trigger demotion.

### Conviction-Based TTL Renewal

Posts linked to `x/rep` initiatives (via `initiative_id`) can renew their ephemeral TTL if conviction exceeds the `conviction_renewal_threshold`. The EndBlocker checks conviction before pruning and extends TTL instead if sufficient.

### Tag System

Tags are owned by the `x/common` `TagKeeper` interface (implemented by this module):

- Created dynamically by members (Tier 2+) for a fee
- Expire if unused for 30 days (reserved tags exempt)
- Maximum 10,000 system-wide tags (configurable)
- Reserved tags controlled by governance

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Post` | `post/value/{id}` | Post with content, metadata, moderation state, reactions |
| `Category` | `category/value/{id}` | Governance-controlled discussion container |
| `Tag` | `tag/value/{name}` | Member-created content descriptor |
| `ReservedTag` | `reserved_tag/value/{name}` | Governance-reserved tags |
| `SentinelActivity` | `sentinel/value/{address}` | Sentinel bond and activity tracking |
| `HideRecord` | `hide_record/value/{post_id}` | Sentinel hide action record |
| `ThreadLockRecord` | `thread_lock/value/{root_id}` | Thread lock record |
| `Bounty` | `bounty/value/{id}` | Escrowed SPARK bounty on a thread |
| `TagBudget` | `tag_budget/value/{id}` | Reward pool for quality posts with specific tag |
| `AnonymousPostMetadata` | `anon_meta/post/{id}` | ZK proof metadata for anonymous posts |
| `AnonNullifierEntry` | `anon_nullifier/{domain}/{scope}/{nullifier}` | Nullifier replay prevention |

### Post Fields

| Field | Type | Description |
|-------|------|-------------|
| `post_id` | uint64 | Auto-incrementing ID |
| `category_id` | uint64 | Parent category |
| `root_id` | uint64 | Thread root (self for root posts) |
| `parent_id` | uint64 | Direct parent (for nested replies) |
| `author` | string | Creator address |
| `content` | string | Post body (max 10KB default) |
| `tags` | []string | Up to 5 tags |
| `status` | enum | ACTIVE, HIDDEN, DELETED, ARCHIVED |
| `depth` | uint32 | Reply nesting level (max 10) |
| `initiative_id` | uint64 | Optional `x/rep` initiative link for conviction |
| `upvote_count` / `downvote_count` | uint64 | Denormalized reaction counts |
| `expiration_time` | int64 | TTL expiry (0 = permanent) |
| `conviction_sustained` | bool | True if TTL renewed by conviction |

## Messages

### Post Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreatePost` | Create post or reply; pays spam tax + storage cost | Any address (member or non-member) |
| `MsgEditPost` | Edit within grace period (free) or after (edit fee) | Post author only |
| `MsgDeletePost` | Soft-delete (tombstone) | Post author only |
| `MsgCreateAnonymousPost` | Create anonymous post with ZK proof | Proven trust level via ZK |
| `MsgCreateAnonymousReply` | Create anonymous reply with ZK proof | Proven trust level via ZK |
| `MsgAnonymousReact` | React anonymously with ZK proof | Proven trust level via ZK |

### Reactions and Flags

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpvotePost` | Upvote a post (free for members) | Any member |
| `MsgDownvotePost` | Downvote (costs SPARK deposit, burned) | Any member |
| `MsgFlagPost` | Flag post for review | Any member (non-members pay spam tax) |
| `MsgDismissFlags` | Dismiss flags after review | Governance |

### Moderation

| Message | Description | Access |
|---------|-------------|--------|
| `MsgHidePost` | Hide post (requires reason) | Active sentinel |
| `MsgBondSentinel` | Stake DREAM to become/restore sentinel | Members meeting reputation tier |
| `MsgUnbondSentinel` | Unbond DREAM (exit sentinel) | Sentinel only |

### Thread Control

| Message | Description | Access |
|---------|-------------|--------|
| `MsgLockThread` | Lock thread (prevent new replies) | Root author or sentinel |
| `MsgUnlockThread` | Unlock thread | Locker or governance |
| `MsgMoveThread` | Move thread to different category | Sentinel or governance |
| `MsgPinPost` | Pin post (up to 5 per category) | Governance or author |
| `MsgUnpinPost` | Unpin post | Pin creator or governance |
| `MsgPinReply` / `MsgUnpinReply` | Pin/unpin reply (3 max per thread) | Thread author |
| `MsgMarkAcceptedReply` | Mark reply as "accepted answer" | Thread author |

### Appeals

| Message | Description | Access |
|---------|-------------|--------|
| `MsgAppealPost` | Appeal hidden post (triggers jury in `x/rep`) | Post author |
| `MsgAppealThreadLock` | Appeal thread lock | Thread author |
| `MsgAppealThreadMove` | Appeal thread move | Thread author |
| `MsgAppealGovAction` | Appeal governance pause/lock/move | Affected author |

### Bounties

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateBounty` | Create bounty on thread (escrows SPARK) | Any member |
| `MsgAwardBounty` | Award bounty to reply | Bounty creator |
| `MsgIncreaseBounty` | Add more SPARK to active bounty | Bounty creator |
| `MsgCancelBounty` | Cancel (refund minus 10% fee) | Bounty creator |

### Tag Budgets

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateTagBudget` | Create reward pool for posts with a tag | Any member |
| `MsgAwardFromTagBudget` | Award SPARK from budget for quality post | Budget creator |
| `MsgTopUpTagBudget` | Add more SPARK | Budget creator |
| `MsgWithdrawTagBudget` | Withdraw unused funds | Budget creator |

### Tag Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgReportTag` | Report tag as problematic | Any member |
| `MsgResolveTagReport` | Resolve tag report (reserve/ban/restore) | Governance |

### Archival

| Message | Description | Access |
|---------|-------------|--------|
| `MsgFreezeThread` | Begin archival for inactive thread (>30 days) | Any member |
| `MsgUnarchiveThread` | Restore archived thread | Governance |

### Emergency Controls

| Message | Description | Access |
|---------|-------------|--------|
| `MsgSetForumPaused` | Stop all new posts | Governance |
| `MsgSetModerationPaused` | Stop moderation actions | Governance |
| `MsgUpdateParams` | Update full parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Commons Operations Committee |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `Posts` | Filter by category and status |
| `Thread` | Full thread (root + all replies) |
| `Categories` | List all categories |
| `UserPosts` | Posts by author |
| `SentinelStatus` | Sentinel bond and activity |
| `PinnedPosts` | Category's pinned posts |
| `LockedThreads` | All locked threads |
| `TopPosts` | Posts by score within time range |
| `BountyByThread` / `ActiveBounties` / `UserBounties` | Bounty queries |
| `TagBudgetByTag` / `TagBudgets` | Budget queries |
| `PostFlags` / `FlagReviewQueue` | Flag review state |
| `ForumStatus` | Paused/enabled flags |
| `MemberReports` / `MemberWarnings` / `MemberStanding` | Member accountability |
| `TagExists` / `TagReports` | Tag queries |
| `AnonymousPostMeta` / `AnonymousReplyMeta` | Anonymous metadata |
| `IsNullifierUsed` | Nullifier replay check |

## Parameters

### Fees (SPARK)

| Parameter | Default | Description |
|-----------|---------|-------------|
| `spam_tax` | 1.0 SPARK | Non-member post tax |
| `downvote_deposit` | 0.05 SPARK | Burned on downvote |
| `appeal_fee` | 5.0 SPARK | Appeal submission fee |
| `edit_fee` | 0.01 SPARK | Edit after grace period |
| `cost_per_byte` | 100 uspark/byte | Storage cost |

### Content Limits

| Parameter | Default | Description |
|-----------|---------|-------------|
| `max_content_size` | 10,240 bytes | Post content limit |
| `max_reply_depth` | 10 | Max reply nesting |
| `max_tags_per_post` | 5 | Tags per post |
| `max_total_tags` | 10,000 | System-wide tag limit |

### Rate Limits (per 24h)

| Parameter | Default | Description |
|-----------|---------|-------------|
| `daily_post_limit` | 50 | Posts per user per day |
| `max_reactions_per_day` | 100 | Reactions per user per day |
| `max_downvotes_per_day` | 20 | Downvotes per user per day |
| `max_flags_per_day` | 20 | Flags per user per day |

### Time Windows

| Parameter | Default | Description |
|-----------|---------|-------------|
| `ephemeral_ttl` | 86,400 (24h) | Non-member post TTL |
| `archive_threshold` | 2,592,000 (30d) | Inactivity before archival |
| `edit_grace_period` | 300 (5m) | Free edit window |
| `appeal_deadline` | 1,209,600 (14d) | Appeal submission deadline |

### Sentinel Requirements

| Parameter | Default | Description |
|-----------|---------|-------------|
| `min_sentinel_bond` | 500 DREAM | Minimum to become sentinel |
| `sentinel_slash_amount` | 100 DREAM | Per overturned appeal |
| `max_hides_per_epoch` | 50 | Hide limit per epoch |

### Anonymous Posting

| Parameter | Default | Description |
|-----------|---------|-------------|
| `anonymous_posting_enabled` | true | Enable ZK anonymous posts |
| `anonymous_min_trust_level` | 2 | Min trust for anon posting |
| `conviction_renewal_threshold` | 100 | Min conviction to renew TTL |
| `conviction_renewal_period` | 604,800 (7d) | TTL extension on renewal |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec |
| `x/bank` | Yes | Fee collection, bounty escrow, burning |
| `x/rep` | Yes | Membership, trust levels, DREAM operations, jury appeals, author bonds, conviction |
| `x/commons` | No | Council authorization for categories and operational params |
| `x/vote` | No | ZK proof verification for anonymous posting |
| `x/season` | No | Epoch duration for nullifier scoping |

## EndBlocker

Four phases executed per block (with per-phase caps for gas efficiency):

1. **Ephemeral Post Pruning** (max 100/block) — remove expired TTL posts; check conviction renewal before pruning (extend TTL if conviction sufficient)
2. **Hidden Post Expiration** (max 50/block) — soft-delete posts hidden >7 days
3. **Bounty Expiration** (max 50/block) — mark expired bounties, refund escrowed amount
4. **Tag Expiration** (max 50/block) — remove unused tags past 30-day expiration (reserved tags exempt)

## Events

All state-changing operations emit typed events for indexing and client notification.

## Client

### CLI

```bash
# Posts
sparkdreamd tx forum create-post --category-id 1 --content "Hello World" --tags "general" --from alice
sparkdreamd tx forum edit-post 1 --content "Updated" --from alice
sparkdreamd tx forum delete-post 1 --from alice

# Moderation
sparkdreamd tx forum bond-sentinel --amount 1000 --from bob
sparkdreamd tx forum hide-post 1 --reason-code SPAM --from sentinel

# Bounties
sparkdreamd tx forum create-bounty 1 --amount 100spark --from alice
sparkdreamd tx forum award-bounty 1 --reply-id 5 --from alice

# Queries
sparkdreamd q forum posts --category-id 1
sparkdreamd q forum thread 1
sparkdreamd q forum sentinel-status [address]
sparkdreamd q forum params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/forum/v1/query.proto` for the full API surface.
