# `x/blog`

The `x/blog` module is an on-chain content management system for blog posts with threaded replies, reactions, anonymous posting via ZK proofs, and ephemeral content lifecycle management.

## Overview

This module provides:

- **On-chain publishing** — any address can create blog posts
- **Member-gated discussions** — replies and reactions require `x/rep` membership
- **Trust-level gating** — post authors control minimum trust level required to reply
- **Anonymous posting** — ZK proof-based anonymous posts, replies, and reactions
- **Author moderation** — hide/unhide controls for posts and replies
- **Reactions** — fixed-set reaction system (Like, Insightful, Disagree, Funny)
- **Ephemeral content** — non-member and anonymous content auto-expires with TTL
- **Conviction renewal** — community conviction staking can extend ephemeral content indefinitely
- **Pinning** — trusted members can convert ephemeral content to permanent
- **Rate limiting** — per-address daily limits on all content actions
- **Storage fees** — per-byte fees burned on creation/expansion

## Concepts

### Two-Tier Content System

Active `x/rep` members get permanent posts (no expiry). Non-members and anonymous posters get ephemeral content with a configurable TTL (default: 7 days). Ephemeral content is automatically tombstoned by the EndBlocker when it expires.

### Non-Destructive Deletion

Posts and replies are **tombstoned** rather than fully removed — content is cleared but structural metadata (IDs, timestamps, parent references) is preserved. This keeps thread structure and child content intact.

### Anonymous Posting

Members with sufficient trust level can post anonymously using ZK proofs that demonstrate membership and minimum trust level without revealing identity. Anonymous actions use **nullifiers** (scoped per epoch for posts, per post for replies) to prevent double-posting while preserving privacy.

Approved relay addresses can receive subsidies from the Commons treasury to cover storage fees for anonymous content.

### Conviction-Sustained Content

Anonymous ephemeral content can be **conviction-sustained**: if community conviction staking on the content meets a configurable threshold, the TTL is automatically extended. This allows valuable anonymous contributions to persist based on community support.

### Author Bonds

Posts and replies can optionally include a DREAM author bond created via `x/rep`. This bond can be slashed if content is later flagged or challenged, providing economic accountability.

### Initiative Links

Posts can reference an `x/rep` initiative (set at creation, immutable). This enables conviction propagation between the post and the initiative. Links are managed automatically on hide/unhide/delete.

## State

### Objects

| Object | Description | Key |
|--------|-------------|-----|
| `Post` | Blog post with title, body, metadata, and lifecycle state | `Post/value/{id}` |
| `Reply` | Threaded reply with nesting up to configurable depth | `Reply/value/{id}` |
| `Reaction` | Per-user reaction on a post or reply (one per target) | `Reaction/value/{post_id}/{reply_id}/{creator}` |
| `ReactionCounts` | Denormalized aggregate counts per target | `Reaction/counts/{post_id}/{reply_id}` |
| `AnonymousPostMetadata` | ZK proof metadata for anonymous posts | `AnonMeta/post/{id}` |
| `AnonymousReplyMetadata` | ZK proof metadata for anonymous replies | `AnonMeta/reply/{id}` |
| `AnonNullifierEntry` | Used nullifier tracking | `AnonNullifier/{domain}/{scope}/{nullifier}` |
| `RateLimitEntry` | Per-address daily action counter | `RateLimit/{action_type}/{address}` |

### Indexes

| Index | Purpose |
|-------|---------|
| `Post/creator/{creator}/{id}` | List posts by creator |
| `Reply/post/{post_id}/{id}` | List replies by post |
| `Reaction/creator/{creator}/{post_id}/{reply_id}` | List reactions by creator |
| `Expiry/{expires_at}/{type}/{id}` | Efficient TTL expiry processing |

### Post Status Lifecycle

```
                  ┌─── MsgHidePost ───► HIDDEN ───► MsgUnhidePost ──┐
                  │                                                   │
ACTIVE ◄──────────┴───────────────────────────────────────────────────┘
  │
  ├─── MsgDeletePost ──► DELETED (tombstoned, irreversible)
  │
  └─── TTL expiry ──► DELETED (auto-tombstoned by EndBlocker)
```

### Reaction Types

| Type | Value | Purpose |
|------|-------|---------|
| `LIKE` | 1 | General positive sentiment |
| `INSIGHTFUL` | 2 | Intellectual value |
| `DISAGREE` | 3 | Respectful disagreement |
| `FUNNY` | 4 | Humor appreciation |

Each user can have at most one reaction per target. Reacting again changes the reaction type atomically.

## Messages

### Post Operations

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreatePost` | Create a blog post with optional title, body, content type, trust gate, author bond, and initiative link | Any address |
| `MsgUpdatePost` | Update post content (title, body, content type, replies_enabled, min_reply_trust_level) | Post creator only |
| `MsgDeletePost` | Tombstone a post (clears content, preserves structure) | Post creator only |
| `MsgHidePost` | Soft-hide a post (excluded from lists, content preserved) | Post creator only |
| `MsgUnhidePost` | Restore a hidden post | Post creator only |
| `MsgPinPost` | Convert ephemeral post to permanent | ESTABLISHED+ trust level |

### Reply Operations

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateReply` | Create a threaded reply (supports nesting) | Active members meeting post's trust gate |
| `MsgUpdateReply` | Update reply body and content type | Reply creator only |
| `MsgDeleteReply` | Tombstone a reply | Reply creator only |
| `MsgHideReply` | Soft-hide a reply on your post | Post author only |
| `MsgUnhideReply` | Restore a hidden reply | Post author only |
| `MsgPinReply` | Convert ephemeral reply to permanent | ESTABLISHED+ trust level |

### Reactions

| Message | Description | Access |
|---------|-------------|--------|
| `MsgReact` | Add or change reaction on a post or reply | Active members |
| `MsgRemoveReaction` | Remove your reaction | Reaction creator only |

### Anonymous Operations

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateAnonymousPost` | Create post with ZK proof of membership | Proven trust level via ZK |
| `MsgCreateAnonymousReply` | Create reply with ZK proof | Proven trust level via ZK |
| `MsgAnonymousReact` | React anonymously with ZK proof | Proven trust level via ZK |

### Parameter Updates

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update governance-controlled parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Commons Council Operations Committee |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `ShowPost` | Single post by ID |
| `ListPost` | Paginated list of all active posts |
| `ListPostsByCreator` | Posts by a specific creator (optional: include hidden) |
| `ShowReply` | Single reply by ID |
| `ListReplies` | Replies for a post (optional: filter by parent, include hidden) |
| `ReactionCounts` | Aggregate reaction counts for a post or reply |
| `UserReaction` | Check a user's reaction on a target |
| `ListReactions` | Individual reaction records for a target |
| `ListReactionsByCreator` | All reactions by a specific user |
| `AnonymousPostMeta` | ZK metadata for an anonymous post |
| `AnonymousReplyMeta` | ZK metadata for an anonymous reply |
| `IsNullifierUsed` | Check if a nullifier has been used (by domain and scope) |
| `ListExpiringContent` | Find ephemeral content expiring before a given timestamp |

## Parameters

### Governance-Controlled

These can only be changed via `x/gov` proposal (`MsgUpdateParams`).

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `max_title_length` | uint64 | 200 | Maximum post title length (chars) |
| `max_body_length` | uint64 | 10,000 | Maximum post body length (chars) |
| `max_reply_length` | uint64 | 2,000 | Maximum reply body length (bytes) |
| `max_reply_depth` | uint32 | 5 | Maximum reply nesting depth |
| `anonymous_posting_enabled` | bool | true | Master toggle for anonymous operations |
| `pin_min_trust_level` | int32 | 2 | Minimum trust level to pin content |
| `min_ephemeral_content_ttl` | int64 | 86,400 | Floor for ephemeral TTL (seconds) |
| `max_cost_per_byte` | uint64 | 1,000 | Ceiling for storage fee (uspark/byte) |
| `max_reaction_fee` | uint64 | 500 | Ceiling for reaction fee (uspark) |

### Operationally-Controlled

These can be updated by the Commons Council Operations Committee via `MsgUpdateOperationalParams`.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `cost_per_byte` | uint64 | 100 | Storage fee per byte (uspark), burned |
| `cost_per_byte_exempt` | bool | false | Disable storage fees entirely |
| `reaction_fee` | uint64 | 50 | Flat fee per reaction (uspark), burned |
| `reaction_fee_exempt` | bool | false | Disable reaction fees entirely |
| `max_posts_per_day` | uint32 | 10 | Per-address daily post limit |
| `max_replies_per_day` | uint32 | 50 | Per-address daily reply limit |
| `max_reactions_per_day` | uint32 | 100 | Per-address daily reaction limit |
| `max_pins_per_day` | uint32 | 20 | Per-address daily pin limit |
| `anonymous_min_trust_level` | int32 | 2 | Minimum proven trust for anonymous posting |
| `anon_subsidy_budget_per_epoch` | uint64 | 100,000,000 | Subsidy budget per epoch (uspark) |
| `anon_subsidy_max_per_post` | uint64 | 2,000,000 | Max subsidy per anonymous post (uspark) |
| `anon_subsidy_relay_addresses` | []string | [] | Approved relay addresses for subsidy |
| `ephemeral_content_ttl` | int64 | 604,800 | TTL for ephemeral content (seconds, default 7 days) |
| `conviction_renewal_threshold` | string | "100.0" | Minimum conviction score to extend TTL |
| `conviction_renewal_period` | int64 | 604,800 | Duration to extend TTL by (seconds) |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec, account access |
| `x/bank` | Yes | Storage fee collection and burning |
| `x/rep` | Yes | Membership, trust levels, conviction staking, author bonds |
| `x/vote` | No | ZK proof verification for anonymous posting |
| `x/commons` | No | Council-gated operational params, treasury for anon subsidy |
| `x/season` | No | Epoch duration for nullifier scoping and subsidy timing |

## EndBlocker

The EndBlocker runs each block and handles:

1. **TTL Expiry** — processes content past its `expires_at` timestamp:
   - If the non-anonymous creator is now a member: upgrade to permanent
   - If anonymous with sufficient conviction: renew TTL for another period
   - Otherwise: tombstone (clear content)
2. **Subsidy Draw** — draws from Commons treasury for anonymous posting subsidy budget
3. **Nullifier Pruning** — garbage collection of expired nullifier entries

## Events

All state-changing operations emit typed events for indexing and client notification.

## Client

### CLI

CLI commands follow the module's naming conventions. Examples:

```bash
# Posts
sparkdreamd tx blog create-post --title "Hello" --body "World" --from alice
sparkdreamd tx blog update-post 1 --title "Updated" --from alice
sparkdreamd tx blog delete-post 1 --from alice
sparkdreamd tx blog hide-post 1 --from alice
sparkdreamd tx blog unhide-post 1 --from alice
sparkdreamd tx blog pin-post 1 --from bob

# Replies
sparkdreamd tx blog create-reply 1 --body "Great post!" --from bob
sparkdreamd tx blog delete-reply 1 --from bob
sparkdreamd tx blog hide-reply 1 --from alice  # post author hides reply

# Reactions
sparkdreamd tx blog react 1 0 LIKE --from bob
sparkdreamd tx blog remove-reaction 1 0 --from bob

# Queries
sparkdreamd q blog show-post 1
sparkdreamd q blog list-post
sparkdreamd q blog list-replies 1
sparkdreamd q blog reaction-counts 1 0
sparkdreamd q blog params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/blog/v1/query.proto` for the full API surface.
