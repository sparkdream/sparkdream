# `x/forum`

The `x/forum` module implements a decentralized, censorship-resistant discussion platform with hierarchical content organization, dual-token sentinel moderation, bounties, anonymous posting via `x/shield`, and conviction-based content persistence.

## Overview

This module provides:

- **Hierarchical content** — governance-controlled categories and member-created dynamic tags
- **Dual-token moderation** — sentinels stake DREAM as collateral for moderation authority; earn rewards for accurate decisions
- **Content lifecycle** — ephemeral TTL for non-member posts, permanent storage for members, archival for inactive threads
- **Bounties and tag budgets** — economic incentives for quality content
- **Anonymous posting** — anonymous posts, replies, and reactions via `x/shield`'s `MsgShieldedExec`
- **Conviction renewal** — posts linked to `x/rep` initiatives can extend their TTL based on community conviction staking
- **Author bonds** — optional DREAM bonds on content creation, slashable via challenges
- **Appeals system** — jury-based appeals via `x/rep` for overturning moderation actions
- **Thread following** — members can follow threads and track activity
- **Member reports** — community-driven accountability with cosigning, defense, and resolution

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

### Shield-Aware Messages

The following messages support anonymous execution via `x/shield`'s `MsgShieldedExec`:

- `MsgCreatePost` — anonymous posts
- `MsgUpvotePost` — anonymous upvotes
- `MsgDownvotePost` — anonymous downvotes

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
| `ThreadMetadata` | `threadmeta/value/{root_id}` | Thread-level metadata (reply count, last activity) |
| `ThreadFollow` | `threadfollow/value/{root_id}/{address}` | Thread follow subscription |
| `ThreadFollowCount` | `threadfollowcount/value/{root_id}` | Follower count per thread |
| `ThreadMoveRecord` | `threadmove/value/{root_id}` | Thread move history |
| `ArchiveMetadata` | `archivemeta/value/{root_id}` | Archival state and cycle tracking |
| `Bounty` | `bounty/value/{id}` | Escrowed SPARK bounty on a thread |
| `TagBudget` | `tagbudget/value/{id}` | Reward pool for quality posts with specific tag |
| `TagBudgetAward` | `tagbudgetaward/value/{id}` | Award record from a tag budget |
| `TagReport` | `tagreport/value/{name}` | Tag report with evidence |
| `PostFlag` | `postflag/value/{post_id}/{flagger}` | Flag record on a post |
| `UserRateLimit` | `userratelimit/value/{address}` | Per-user daily post tracking |
| `UserReactionLimit` | `userreactionlimit/value/{address}` | Per-user daily reaction tracking |
| `MemberReport` | `memberreport/value/{id}` | Report against a member with cosigning |
| `MemberWarning` | `memberwarning/value/{id}` | Warning issued to a member |
| `MemberSalvationStatus` | `membersalvation/value/{address}` | Member salvation/rehabilitation tracking |
| `JuryParticipation` | `jurypart/value/{address}` | Jury service participation record |
| `GovActionAppeal` | `govactionappeal/value/{id}` | Appeal against a governance action |

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
| `MsgCreateCategory` | Create a governance-controlled category | Governance |

Anonymous posts, replies, and reactions are submitted via `x/shield`'s `MsgShieldedExec` wrapping standard forum messages. See [x/shield](../shield/README.md) for details.

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
| `MsgDisputePin` | Dispute a pin decision | Any member |
| `MsgMarkAcceptedReply` | Mark reply as "accepted answer" | Thread author |
| `MsgConfirmProposedReply` / `MsgRejectProposedReply` | Accept or reject a proposed reply | Thread author |
| `MsgFollowThread` / `MsgUnfollowThread` | Follow or unfollow a thread | Any member |

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
| `MsgAssignBountyToReply` | Assign bounty to a specific reply | Bounty creator |

### Tag Budgets

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateTagBudget` | Create reward pool for posts with a tag | Any member |
| `MsgAwardFromTagBudget` | Award SPARK from budget for quality post | Budget creator |
| `MsgTopUpTagBudget` | Add more SPARK | Budget creator |
| `MsgWithdrawTagBudget` | Withdraw unused funds | Budget creator |
| `MsgToggleTagBudget` | Enable or disable a tag budget | Budget creator |

### Tag Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgReportTag` | Report tag as problematic | Any member |
| `MsgResolveTagReport` | Resolve tag report (reserve/ban/restore) | Governance |

### Member Reports

| Message | Description | Access |
|---------|-------------|--------|
| `MsgReportMember` | Report a member with evidence | Any member |
| `MsgCosignMemberReport` | Cosign an existing member report | Any member |
| `MsgResolveMemberReport` | Resolve a member report (warn/demote/zero) | Governance or sentinel |
| `MsgDefendMemberReport` | Submit defense against a report | Reported member |

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

### Content

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `Post` | Single post by ID |
| `Posts` | Filter by category and status |
| `Thread` | Full thread (root + all replies) |
| `Categories` | List all categories |
| `UserPosts` | Posts by author |
| `TopPosts` | Posts by score within time range |
| `PinnedPosts` | Category's pinned posts |
| `ForumStatus` | Paused/enabled flags |

### Thread Management

| Query | Description |
|-------|-------------|
| `ThreadMetadata` | Thread-level metadata (reply count, last activity) |
| `ThreadLockRecord` | Lock record for a thread |
| `ThreadLockStatus` | Whether a thread is locked |
| `LockedThreads` | All locked threads |
| `ThreadMoveRecord` | Move history for a thread |
| `ThreadFollow` | Whether a user follows a thread |
| `ThreadFollowers` | List of followers for a thread |
| `ThreadFollowCount` | Number of followers for a thread |
| `UserFollowedThreads` | Threads followed by a user |

### Moderation

| Query | Description |
|-------|-------------|
| `SentinelStatus` | Sentinel bond and activity |
| `SentinelActivity` | Sentinel moderation actions |
| `SentinelBondCommitment` | Sentinel bond commitment details |
| `HideRecord` | Hide action record for a post |
| `PostFlag` | Single flag record |
| `PostFlags` | All flags on a post |
| `FlagReviewQueue` | Posts pending flag review |

### Bounties

| Query | Description |
|-------|-------------|
| `Bounty` | Single bounty by ID |
| `BountyByThread` | Bounty on a specific thread |
| `ActiveBounties` | All active bounties |
| `UserBounties` | Bounties created by a user |
| `BountyExpiringSoon` | Bounties near expiration |

### Tag Budgets

| Query | Description |
|-------|-------------|
| `TagBudget` | Single tag budget by ID |
| `TagBudgetByTag` | Budget for a specific tag |
| `TagBudgets` | All tag budgets |
| `TagBudgetAward` | Single award record |
| `TagBudgetAwards` | All awards for a budget |

### Tags

| Query | Description |
|-------|-------------|
| `TagExists` | Whether a tag exists |
| `TagReport` | Report on a specific tag |
| `TagReports` | All tag reports |

### Archives

| Query | Description |
|-------|-------------|
| `ArchiveMetadata` | Archival state for a thread |
| `AppealCooldown` | Remaining appeal cooldown |
| `ArchiveCooldown` | Remaining archive cooldown |

### Member Accountability

| Query | Description |
|-------|-------------|
| `MemberReport` | Single member report |
| `MemberReports` | All member reports |
| `MemberWarning` | Single member warning |
| `MemberWarnings` | All warnings for a member |
| `MemberStanding` | Overall member standing |
| `MemberSalvationStatus` | Member salvation/rehabilitation status |
| `JuryParticipation` | Jury service participation for a member |

### Appeals

| Query | Description |
|-------|-------------|
| `GovActionAppeal` | Single governance action appeal |
| `GovActionAppeals` | All governance action appeals |

### Rate Limits

| Query | Description |
|-------|-------------|
| `UserRateLimit` | User's daily post usage |
| `UserReactionLimit` | User's daily reaction usage |

## Parameters

### Governance-Only (via `MsgUpdateParams`)

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `forum_paused` | bool | false | Stop all new posts |
| `moderation_paused` | bool | false | Stop moderation actions |
| `appeals_paused` | bool | false | Stop appeal submissions |

### Operational (via `MsgUpdateOperationalParams`)

#### Feature Toggles

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `bounties_enabled` | bool | true | Enable bounty system |
| `reactions_enabled` | bool | true | Enable reactions |
| `editing_enabled` | bool | true | Enable post editing |
| `cost_per_byte_exempt` | bool | false | Exempt members from storage cost |

#### Fees (SPARK)

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `spam_tax` | Coin | 1.0 SPARK | Non-member post tax |
| `reaction_spam_tax` | Coin | 0.1 SPARK | Non-member reaction tax |
| `flag_spam_tax` | Coin | 0.1 SPARK | Non-member flag tax |
| `downvote_deposit` | Coin | 0.05 SPARK | Burned on downvote |
| `appeal_fee` | Coin | 5.0 SPARK | Hide appeal submission fee |
| `lock_appeal_fee` | Coin | 5.0 SPARK | Lock appeal fee |
| `move_appeal_fee` | Coin | 5.0 SPARK | Move appeal fee |
| `edit_fee` | Coin | 0.01 SPARK | Edit after grace period |
| `cost_per_byte` | Coin | 100 uspark/byte | Storage cost |

#### Content Limits

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `max_content_size` | uint64 | 10,240 bytes | Post content limit |
| `max_reply_depth` | uint32 | 10 | Max reply nesting |
| `daily_post_limit` | uint64 | 50 | Posts per user per day |
| `max_follows_per_day` | uint64 | 50 | Thread follows per user per day |
| `bounty_cancellation_fee_percent` | uint64 | 10% | Fee on bounty cancellation |

#### Time Windows

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `ephemeral_ttl` | int64 | 86,400 (24h) | Non-member post TTL |
| `edit_grace_period` | int64 | 300 (5m) | Free edit window |
| `edit_max_window` | int64 | 86,400 (24h) | Maximum edit window |
| `archive_threshold` | int64 | 2,592,000 (30d) | Inactivity before archival |
| `archive_cooldown` | int64 | 2,592,000 (30d) | Cooldown between archive cycles |
| `unarchive_cooldown` | int64 | 86,400 (1d) | Cooldown after unarchive |
| `hide_appeal_cooldown` | int64 | 3,600 (1h) | Cooldown between hide appeals |
| `lock_appeal_cooldown` | int64 | 3,600 (1h) | Cooldown between lock appeals |
| `move_appeal_cooldown` | int64 | 3,600 (1h) | Cooldown between move appeals |

#### Conviction Renewal

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `conviction_renewal_threshold` | LegacyDec | 100 | Min conviction to renew TTL |
| `conviction_renewal_period` | int64 | 604,800 (7d) | TTL extension on renewal |

### Non-Operational (hardcoded defaults, not in `ForumOperationalParams`)

| Parameter | Default | Description |
|-----------|---------|-------------|
| `max_tags_per_post` | 5 | Tags per post |
| `max_tag_length` | 32 | Max tag name length |
| `max_total_tags` | 10,000 | System-wide tag limit |
| `max_reactions_per_day` | 100 | Reactions per user per day |
| `max_downvotes_per_day` | 20 | Downvotes per user per day |
| `max_flags_per_day` | 20 | Flags per user per day |
| `max_salvations_per_day` | 10 | Salvations per user per day |
| `hidden_expiration` | 604,800 (7d) | Time before hidden post deleted |
| `tag_expiration` | 2,592,000 (30d) | Unused tag TTL |
| `appeal_deadline` | 1,209,600 (14d) | Appeal submission deadline |
| `min_sentinel_bond` | 500 DREAM | Minimum to become sentinel |
| `sentinel_slash_amount` | 100 DREAM | Per overturned appeal |
| `min_rep_tier_sentinel` | 3 | Rep tier required to be sentinel |
| `min_rep_tier_tags` | 2 | Rep tier required to create tags |
| `min_rep_tier_thread_lock` | 4 | Rep tier required to lock threads |
| `max_hides_per_epoch` | 50 | Sentinel hide limit per epoch |
| `max_sentinel_locks_per_epoch` | 5 | Sentinel lock limit per epoch |
| `max_sentinel_moves_per_epoch` | 10 | Sentinel move limit per epoch |
| `max_pinned_per_category` | 5 | Pinned posts per category |
| `max_pinned_replies_per_thread` | 3 | Pinned replies per thread |
| `max_bounty_winners` | 5 | Max winners per bounty |
| `bounty_duration` | 1,209,600 (14d) | Default bounty duration |
| `max_bounty_duration` | 2,592,000 (30d) | Maximum bounty duration |
| `flag_review_threshold` | 5 | Flags needed for review queue |
| `max_archive_cycles` | 5 | Maximum archive/unarchive cycles |
| `max_salvation_depth` | 10 | Maximum salvation chain depth |
| `min_evidence_posts` | 3 | Minimum evidence for member report |
| `member_report_cosign_threshold` | 3 | Cosigns needed for report action |
| `max_warnings_before_demotion` | 3 | Warnings before auto-demotion |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec |
| `x/bank` | Yes | Fee collection, bounty escrow, burning, DREAM transfers |
| `x/rep` | Yes | Membership, trust levels, DREAM operations, jury appeals, author bonds, conviction, initiative linking |
| `x/commons` | No | Council authorization for categories and operational params |
| `x/shield` | No | ZK proof verification and module-paid gas for anonymous posting (via ShieldAware interface) |

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

# Thread following
sparkdreamd tx forum follow-thread 1 --from alice
sparkdreamd tx forum unfollow-thread 1 --from alice

# Moderation
sparkdreamd tx forum bond-sentinel --amount 1000 --from bob
sparkdreamd tx forum hide-post 1 --reason-code SPAM --from sentinel

# Bounties
sparkdreamd tx forum create-bounty 1 --amount 100spark --from alice
sparkdreamd tx forum award-bounty 1 --reply-id 5 --from alice

# Member reports
sparkdreamd tx forum report-member [address] --evidence "post:1,post:2,post:3" --from alice
sparkdreamd tx forum cosign-member-report 1 --from bob

# Queries
sparkdreamd q forum posts --category-id 1
sparkdreamd q forum thread 1
sparkdreamd q forum sentinel-status [address]
sparkdreamd q forum params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/forum/v1/query.proto` for the full API surface.
