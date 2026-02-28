# Technical Specification: `x/blog`

## 1. Abstract

The `x/blog` module is an on-chain content management system for blog posts with threaded replies and reactions. It provides CRUD operations for posts, nested replies (member-only), and a fixed-set reaction system, all with creator-based access control, configurable content length constraints, per-byte storage fees that are burned on use, and per-address daily rate limits.

Deleted posts and replies are tombstoned (content cleared, structural metadata preserved) so that thread structure and child content remain intact. Hidden posts and replies are excluded from list query results for effective moderation — content is preserved in state for restoration via unhide.

This module serves as:
- A simple on-chain publishing mechanism for any address
- A member-gated discussion layer (replies and reactions require x/rep membership)
- Author-moderated comment sections with hide/delete controls
- A foundation for more complex content systems (e.g., x/forum)
- A content source for retroactive public goods nominations (x/season) — blog posts and replies created during a season can be nominated for retroactive DREAM rewards

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/gov` | Authority for full parameter updates (`MsgUpdateParams`) |
| `x/auth` | Address codec for bech32 conversion |
| `x/bank` | Storage fee collection and burning |
| `x/rep` | Membership and trust-level checks for replies, reactions, member trust tree for anonymous posting, content conviction staking queries, and author bond creation |
| `x/vote` | *(optional)* ZK proof verification for anonymous posting (`VerifyAnonymousActionProof`) |
| `x/commons` | *(optional)* Council-gated operational parameter updates; treasury draws for anonymous posting subsidy (`SpendFromTreasury`) |
| `x/season` | *(optional)* Epoch duration for anonymous posting nullifier scoping and subsidy draw timing (`GetEpochDuration`); falls back to `DefaultEpochDuration` constant if not wired |

---

## 3. State Objects

### 3.1. Post

Primary content storage object.

```protobuf
message Post {
  string title = 1;                                    // Post title
  string body = 2;                                     // Post content
  string creator = 3;                                  // Creator's bech32 address
  uint64 id = 4;                                       // Auto-incremented unique identifier
  sparkdream.common.v1.ContentType content_type = 5;   // Content encoding hint
  bool replies_enabled = 6;                            // Whether replies are accepted (default: true)
  uint64 reply_count = 7;                              // Denormalized reply count (active replies only)
  int32 min_reply_trust_level = 8;                     // Minimum trust level to reply (default: 0)
  int64 created_at = 9;                                // Unix timestamp of creation
  int64 updated_at = 10;                               // Unix timestamp of last update (0 if never)
  PostStatus status = 11;                              // ACTIVE, HIDDEN, or DELETED
  string hidden_by = 12;                               // Address that hid the post (empty if not hidden)
  int64 hidden_at = 13;                                // Unix timestamp when hidden (0 if not hidden)
  int64 expires_at = 14;                               // Unix timestamp when post auto-tombstones (0 = permanent)
  string pinned_by = 15;                               // Address of member who pinned (empty if not pinned)
  int64 pinned_at = 16;                                // Unix timestamp when pinned (0 if not pinned)
  uint64 fee_bytes_high_water = 17;                    // Highest byte count for which storage fees have been paid
  bool edited = 18;                                    // Whether post content has been edited (via UpdatePost)
  int64 edited_at = 19;                                // Unix timestamp of last content edit (0 if never)
  uint64 initiative_id = 20;                            // x/rep initiative reference (0 = none, set at creation, immutable)
  bool conviction_sustained = 21;                       // True if anonymous content has entered conviction-sustained state (TTL extended by community conviction)
}
```

The `initiative_id` field links a blog post to an x/rep initiative for conviction propagation. It is set once at creation via `MsgCreatePost` and cannot be changed. When a post is hidden, the initiative link is removed; when unhidden, it is re-registered. When a post is deleted, the link is permanently removed.

The `content_type` field is an enum from `sparkdream.common.v1` indicating how to interpret the post content (e.g., `CONTENT_TYPE_TEXT`, `CONTENT_TYPE_MARKDOWN`, `CONTENT_TYPE_GZIP`, `CONTENT_TYPE_IPFS`).

**`min_reply_trust_level`** controls who can reply to this post. The post author sets this value when creating or updating the post. Values map to `sparkdream.rep.v1.TrustLevel` with one additional sentinel:

| Value | Meaning | Who can reply |
|-------|---------|---------------|
| `-1` | Open | Any valid address (no membership required) |
| `0` | `TRUST_LEVEL_NEW` | Any active member (default) |
| `1` | `TRUST_LEVEL_PROVISIONAL` | Provisional members and above |
| `2` | `TRUST_LEVEL_ESTABLISHED` | Established members and above |
| `3` | `TRUST_LEVEL_TRUSTED` | Trusted members and above |
| `4` | `TRUST_LEVEL_CORE` | Core members only |

Default is `0` (any active member), consistent with replies being a membership perk. Authors who want fully open discussion can set `-1`.

### 3.2. PostStatus

```protobuf
enum PostStatus {
  POST_STATUS_UNSPECIFIED = 0;
  POST_STATUS_ACTIVE = 1;      // Visible and functional
  POST_STATUS_DELETED = 2;     // Tombstoned — title/body cleared, replies/reactions preserved
  POST_STATUS_HIDDEN = 3;      // Soft-deleted — title/body preserved in state, excluded from list queries
}
```

### 3.3. Reply

Threaded reply to a blog post. Reply access is governed by the parent post's `min_reply_trust_level`: default `0` requires active membership (via x/rep), but authors may set `-1` to allow non-member replies.

```protobuf
message Reply {
  uint64 id = 1;                                       // Global auto-incremented ID
  uint64 post_id = 2;                                  // Parent blog post
  uint64 parent_reply_id = 3;                          // 0 = top-level reply, >0 = nested under another reply
  string creator = 4;                                  // Reply author (subject to min_reply_trust_level)
  string body = 5;                                     // Reply content
  sparkdream.common.v1.ContentType content_type = 6;   // Content encoding hint
  int64 created_at = 7;                                // Unix timestamp of creation
  bool edited = 8;                                     // Whether reply has been edited
  int64 edited_at = 9;                                 // Unix timestamp of last edit (0 if never)
  uint32 depth = 10;                                   // Nesting level (0 = top-level)
  ReplyStatus status = 11;                             // ACTIVE, HIDDEN, or DELETED
  string hidden_by = 12;                               // Address of post author who hid it
  int64 hidden_at = 13;                                // Unix timestamp when hidden
  int64 expires_at = 14;                               // Unix timestamp when reply auto-tombstones (0 = permanent)
  string pinned_by = 15;                               // Address of member who pinned (empty if not pinned)
  int64 pinned_at = 16;                                // Unix timestamp when pinned (0 if not pinned)
  uint64 fee_bytes_high_water = 17;                    // Highest byte count for which storage fees have been paid
  bool conviction_sustained = 18;                      // True if anonymous reply has entered conviction-sustained state
}
```

### 3.4. ReplyStatus

```protobuf
enum ReplyStatus {
  REPLY_STATUS_UNSPECIFIED = 0;
  REPLY_STATUS_ACTIVE = 1;      // Visible to all
  REPLY_STATUS_DELETED = 2;     // Tombstoned — body cleared, structural metadata preserved
  REPLY_STATUS_HIDDEN = 3;      // Soft-deleted by post author; excluded from all query results
}
```

### 3.5. ReactionType

Fixed set of reactions. Extensible via governance parameter update in the future.

```protobuf
enum ReactionType {
  REACTION_TYPE_UNSPECIFIED = 0;
  REACTION_TYPE_LIKE = 1;          // General positive sentiment
  REACTION_TYPE_INSIGHTFUL = 2;    // Adds intellectual value
  REACTION_TYPE_DISAGREE = 3;      // Respectful disagreement
  REACTION_TYPE_FUNNY = 4;         // Humor appreciation
}
```

### 3.6. Reaction

Individual reaction record. One reaction per user per target (post or reply). Users can change their reaction type or remove it entirely.

```protobuf
message Reaction {
  string creator = 1;              // Reactor address (must be active member)
  ReactionType reaction_type = 2;  // Selected reaction
  uint64 post_id = 3;             // Target post ID
  uint64 reply_id = 4;            // Target reply ID (0 = reacting to the post itself)
}
```

### 3.7. ReactionCounts

Denormalized aggregate counts per target, maintained atomically when reactions are added, changed, or removed.

```protobuf
message ReactionCounts {
  uint64 like_count = 1;
  uint64 insightful_count = 2;
  uint64 disagree_count = 3;
  uint64 funny_count = 4;
}
```

### 3.8. Params

Configurable module parameters.

```protobuf
message Params {
  uint64 max_title_length = 1;                    // Maximum title length in bytes (default: 200)
  uint64 max_body_length = 2;                     // Maximum body length in bytes (default: 10,000)
  cosmos.base.v1beta1.Coin cost_per_byte = 3;     // Storage fee per byte (default: 100uspark)
  bool cost_per_byte_exempt = 4;                  // Disable storage fees (default: false)
  uint64 max_reply_length = 5;                    // Maximum reply body length in bytes (default: 2,000)
  uint32 max_reply_depth = 6;                     // Maximum nesting depth for replies (default: 5)
  cosmos.base.v1beta1.Coin reaction_fee = 7;      // Flat fee per reaction (default: 50uspark)
  bool reaction_fee_exempt = 8;                   // Disable reaction fees (default: false)
  uint32 max_posts_per_day = 9;                   // Max posts per address per day (default: 10)
  uint32 max_replies_per_day = 10;                // Max replies per address per day (default: 50)
  uint32 max_reactions_per_day = 11;              // Max reactions per address per day (default: 100)
  bool anonymous_posting_enabled = 12;            // Master toggle for anonymous posting (default: true)
  uint32 anonymous_min_trust_level = 13;          // Minimum trust level for anonymous posting (default: 2 = ESTABLISHED)
  cosmos.base.v1beta1.Coin anon_subsidy_budget_per_epoch = 14;  // Auto-transferred from Commons Council treasury each epoch (default: 100spark)
  cosmos.base.v1beta1.Coin anon_subsidy_max_per_post = 15;      // Max subsidy per anonymous post/reply (default: 2spark)
  repeated string anon_subsidy_relay_addresses = 16;             // Approved relay addresses eligible for subsidy (default: [])
  int64 ephemeral_content_ttl = 17;                                 // TTL in seconds for ephemeral content: anonymous and non-member posts/replies (default: 604800 = 7 days; 0 = no expiry)
  uint32 pin_min_trust_level = 18;                               // Minimum trust level to pin ephemeral content (default: 2 = ESTABLISHED)
  uint32 max_pins_per_day = 19;                                  // Max pins per address per day (default: 20)
  int64 min_ephemeral_content_ttl = 20;                          // Governance-only floor for ephemeral_content_ttl (default: 86400 = 1 day)
  cosmos.base.v1beta1.Coin max_cost_per_byte = 21;              // Governance-only ceiling for cost_per_byte (default: 1000uspark)
  cosmos.base.v1beta1.Coin max_reaction_fee = 22;               // Governance-only ceiling for reaction_fee (default: 500uspark)
  string conviction_renewal_threshold = 23 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];  // Min conviction score to renew anonymous content at TTL expiry (default: 100.0; 0 = disabled)
  int64 conviction_renewal_period = 24;                          // Duration in seconds to extend TTL by when conviction-renewed (default: 604800 = 7 days, same as ephemeral_content_ttl)
}
```

**Length limits apply to the stored byte representation**, not to decompressed or resolved content. When `content_type` is `CONTENT_TYPE_GZIP` or an off-chain reference like `CONTENT_TYPE_IPFS`, the limit governs the size of the compressed data or the CID string stored on-chain. Interpretation of referenced or compressed content is entirely the client's responsibility.

### 3.9. BlogOperationalParams

Subset of `Params` that can be updated by the Operations Committee without a full governance proposal.

```protobuf
message BlogOperationalParams {
  cosmos.base.v1beta1.Coin cost_per_byte = 1;     // Storage fee per byte
  bool cost_per_byte_exempt = 2;                   // Disable storage fees
  cosmos.base.v1beta1.Coin reaction_fee = 3;       // Flat fee per reaction
  bool reaction_fee_exempt = 4;                    // Disable reaction fees
  uint32 max_posts_per_day = 5;                    // Max posts per address per day
  uint32 max_replies_per_day = 6;                  // Max replies per address per day
  uint32 max_reactions_per_day = 7;                // Max reactions per address per day
  uint32 anonymous_min_trust_level = 8;            // Operations Committee can adjust anonymous trust threshold
  cosmos.base.v1beta1.Coin anon_subsidy_budget_per_epoch = 9;   // Epoch subsidy draw amount
  cosmos.base.v1beta1.Coin anon_subsidy_max_per_post = 10;      // Per-post subsidy cap
  repeated string anon_subsidy_relay_addresses = 11;             // Approved relay addresses
  int64 ephemeral_content_ttl = 12;                                 // TTL for ephemeral content (anonymous + non-member)
  uint32 max_pins_per_day = 13;                                  // Max pins per address per day
  string conviction_renewal_threshold = 14 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];  // Operations Committee can adjust conviction renewal threshold
  int64 conviction_renewal_period = 15;                          // Operations Committee can adjust conviction renewal period
}
```

### 3.10. RateLimitEntry

Internal storage type for per-address daily action counters. Not exposed in any API response — used only as the serialized value for `RateLimit/` KV store keys.

```protobuf
message RateLimitEntry {
  uint32 count = 1;   // Number of actions taken today
  uint64 day = 2;     // Day identifier: block_time / 86400
}
```

### 3.11. AnonNullifierEntry

Internal storage type for tracking used anonymous posting nullifiers. Defined in `x/common/types` and shared across all content modules (x/blog, x/forum, x/collect). Storage and lookup are handled by shared helpers in `x/common/keeper/anon.go` — see [anonymous-posting.md](anonymous-posting.md) § Nullifier Storage. Each module passes its own store to the helpers, so nullifiers are physically stored in the module's KV store.

```protobuf
message AnonNullifierEntry {
  int64 used_at = 1;    // Block time when the nullifier was recorded
  uint64 domain = 2;    // Nullifier domain (1 = anonymous post, 2 = anonymous reply)
  uint64 scope = 3;     // Nullifier scope (epoch for domain 1, post_id for domain 2)
}
```

### 3.12. AnonymousPostMetadata

Metadata linking an anonymous post or reply to its ZK proof parameters. Stored separately from the Post/Reply record so that standard content queries don't carry proof data.

```protobuf
message AnonymousPostMetadata {
  uint64 content_id = 1;           // Post ID (for anonymous posts) or Reply ID (for anonymous replies)
  bytes nullifier = 2;             // 32-byte nullifier used
  bytes merkle_root = 3;           // Trust tree root at proof time
  uint32 proven_trust_level = 4;   // Minimum trust level proven by the ZK proof
}
```

---

## 4. Storage Schema

| Key | Prefix | Value | Purpose |
|-----|--------|-------|---------|
| Params | `p_blog` | Params | Module parameters |
| Post | `Post/value/{id}` | Post | Individual post storage |
| PostCount | `Post/count/` | uint64 | Auto-increment counter for post IDs |
| Reply | `Reply/value/{id}` | Reply | Individual reply storage |
| ReplyCount | `Reply/count/` | uint64 | Auto-increment counter for reply IDs |
| ReplyPostIndex | `Reply/post/{post_id}/{reply_id}` | []byte | Index: all replies for a post |
| Reaction | `Reaction/value/{post_id}/{reply_id}/{creator}` | Reaction | Individual reaction (one per user per target) |
| ReactionCounts | `Reaction/counts/{post_id}/{reply_id}` | ReactionCounts | Aggregate counts per target |
| RateLimit | `RateLimit/{action_type}/{address}` | RateLimitEntry | Per-address daily action counter |
| AnonPostMeta | `AnonMeta/post/{post_id}` | AnonymousPostMetadata | Metadata for anonymous posts |
| AnonReplyMeta | `AnonMeta/reply/{reply_id}` | AnonymousPostMetadata | Metadata for anonymous replies |
| AnonNullifier | `AnonNullifier/{domain}/{scope}/{nullifier_hex}` | AnonNullifierEntry | Tracks used anonymous nullifiers (keyed by domain/scope for efficient pruning) |
| AnonSubsidyLastEpoch | `AnonSubsidy/last_epoch` | uint64 | Last epoch for which subsidy was drawn from treasury |
| ExpiryIndex | `Expiry/{expires_at}/{type}/{id}` | []byte | Index: content by expiry time (type = `post` or `reply`) |
| CreatorPostIndex | `Post/creator/{creator}/{post_id}` | []byte | Index: all posts by a specific creator |
| ReactorIndex | `Reaction/creator/{creator}/{post_id}/{reply_id}` | []byte | Index: all reactions by a specific creator |

**ID Assignment:**
- Post and reply IDs are each auto-incrementing starting from 0 (separate counters)
- Tombstoned posts/replies retain their IDs; IDs are never reused
- Counters persist across deletions

**Reaction Keys:**
- `reply_id = 0` in the key means the reaction targets the post itself
- `reply_id > 0` means the reaction targets that specific reply

**Rate Limit Entries:**
- Each `RateLimitEntry` stores `{count: uint32, day: uint64}` where `day = block_time / 86400`
- `action_type` is one of: `post`, `reply`, `react`, `pin`
- On read, if `entry.day != current_day`, the count resets to 0 (lazy cleanup — no EndBlocker needed)
- Each address has at most one entry per action type (current day's counter)

---

## 5. Messages

### 5.1. CreatePost

Create a new blog post.

```protobuf
message MsgCreatePost {
  string creator = 1;                                  // Post author address
  string title = 2;                                    // Post title
  string body = 3;                                     // Post content
  sparkdream.common.v1.ContentType content_type = 4;   // Content encoding hint
  int32 min_reply_trust_level = 5;                     // Minimum trust level to reply (-1 to 4, default: 0)
  string author_bond = 6;                              // Optional DREAM amount to lock as author bond (cosmossdk.io/math.Int, nullable)
  uint64 initiative_id = 7;                            // Optional: reference an x/rep initiative (0 = none, immutable after creation)
}

message MsgCreatePostResponse {
  uint64 id = 1;  // Assigned post ID
}
```

**Validation:**
- Creator address must be valid bech32
- Title must be non-empty
- Body must be non-empty
- `len(title)` ≤ `params.max_title_length`
- `len(body)` ≤ `params.max_body_length`
- `min_reply_trust_level` must be between -1 and 4 inclusive
- Creator must not exceed `params.max_posts_per_day`
- If `author_bond` is non-nil and positive: validated by `RepKeeper.CreateAuthorBond` (checks cap, existing bond, DREAM balance)
- If `initiative_id` is non-zero: validated by `RepKeeper.ValidateInitiativeReference` (checks initiative exists and is active)

**Storage Fee:**
1. `contentBytes = len(title) + len(body)`
2. `fee = cost_per_byte.amount * contentBytes`
3. Fee sent from creator to the `blog` module account, then immediately burned
4. Skipped if `cost_per_byte_exempt` is true or `cost_per_byte` is zero/nil

**Logic:**
1. Validate creator address
2. Retrieve params and validate content lengths
3. Check rate limit for posts (`params.max_posts_per_day`)
4. Charge and burn storage fee
5. Determine TTL: if `RepKeeper.IsActiveMember(ctx, creator)` is true, `expires_at = 0` (permanent); otherwise `expires_at = block_time + params.ephemeral_content_ttl` (ephemeral, 0 if `ephemeral_content_ttl == 0`)
6. Create Post object with creator, content, content_type, `created_at = block_time`, `status = POST_STATUS_ACTIVE`, `replies_enabled = true`, `fee_bytes_high_water = len(title) + len(body)`, `expires_at` from step 5
7. Call `AppendPost` to assign ID, store post, increment counter, and add to `CreatorPostIndex`
8. If `initiative_id` is non-zero and `RepKeeper` is available: call `RepKeeper.ValidateInitiativeReference(ctx, initiativeId)`. Fail the tx if validation fails. Then call `RepKeeper.RegisterContentInitiativeLink(ctx, initiativeId, targetType, postID)` to link the content for conviction propagation.
9. If `author_bond` is non-nil and positive and `RepKeeper` is available: call `RepKeeper.CreateAuthorBond(ctx, creator, STAKE_TARGET_BLOG_AUTHOR_BOND, postID, amount)`. Fail the tx if bond creation fails.
10. If `expires_at > 0`: add to `ExpiryIndex`
11. Increment rate limit counter
12. Emit `blog.post.created` event
13. Return assigned ID

### 5.2. UpdatePost

Update an existing post. This is a **full-replacement** operation — all fields are applied from the message. Callers must fetch the current post state and re-specify all fields, modifying only the ones they wish to change.

```protobuf
message MsgUpdatePost {
  string creator = 1;                                  // Must be original creator
  string title = 2;                                    // New title
  string body = 3;                                     // New body
  uint64 id = 4;                                       // Post ID to update
  sparkdream.common.v1.ContentType content_type = 5;   // Content encoding hint
  bool replies_enabled = 6;                            // Whether replies are accepted
  int32 min_reply_trust_level = 7;                     // Minimum trust level to reply (-1 to 4)
}

message MsgUpdatePostResponse {}
```

**Validation:**
- Creator address must be valid bech32
- Post must exist and have `status = POST_STATUS_ACTIVE` (`ErrPostDeleted` if tombstoned, `ErrPostHidden` if hidden)
- Sender must be original creator (`ErrUnauthorized` otherwise)
- New content must satisfy length constraints
- `min_reply_trust_level` must be between -1 and 4 inclusive

**Storage Fee (high-water mark):**
1. `newBytes = len(new_title) + len(new_body)`
2. `highWater = post.fee_bytes_high_water`
3. If `newBytes > highWater`: charge `cost_per_byte.amount * (newBytes - highWater)`, then burn; update `post.fee_bytes_high_water = newBytes`
4. If `newBytes ≤ highWater`: no fee charged; `fee_bytes_high_water` unchanged

This prevents the shrink-then-expand exploit: a user cannot shrink a post to 1 byte and then expand to max size for near-zero cost, because the high-water mark remembers the peak size already paid for.

**Logic:**
1. Validate creator address
2. Retrieve existing post, verify it is active and verify ownership
3. Validate new content lengths and `min_reply_trust_level`
4. Charge and burn storage fee (if new size exceeds high-water mark)
5. Update post fields: title, body, content_type, replies_enabled, min_reply_trust_level
6. Set `updated_at = block_time`, `edited = true`, `edited_at = block_time`; update `fee_bytes_high_water` if applicable
7. **`expires_at` is not modified** — updates do not reset or extend the TTL. Ephemeral content cannot be kept alive by periodic edits
8. Store updated post
9. Emit `blog.post.updated` event

### 5.3. DeletePost

Tombstone a post. The post's title and body are cleared and its status is set to `POST_STATUS_DELETED`. All replies, reactions, and reaction counts are preserved intact.

```protobuf
message MsgDeletePost {
  string creator = 1;  // Must be original creator
  uint64 id = 2;       // Post ID to delete
}

message MsgDeletePostResponse {}
```

**Validation:**
- Post must exist and have `status = POST_STATUS_ACTIVE` or `status = POST_STATUS_HIDDEN` (`ErrPostDeleted` otherwise)
- Sender must be original creator

**Logic:**
1. Validate creator address
2. Retrieve existing post and verify it is not tombstoned
3. Verify ownership
4. Set `title = ""`, `body = ""`, `status = POST_STATUS_DELETED`, `updated_at = block_time`, clear `hidden_by` and `hidden_at`
5. If `expires_at > 0`: remove from `ExpiryIndex` (prevents stale entry iteration in EndBlocker)
6. If `initiative_id > 0`: remove content initiative link via `RepKeeper.RemoveContentInitiativeLink`
7. Store tombstoned post (retains `CreatorPostIndex` entry — `ListPostsByCreator` includes tombstoned posts)
8. Emit `blog.post.deleted` event

### 5.4. HidePost

Hide a post (author self-moderation). Hidden posts are excluded from `ListPost` query results. The post title and body are preserved in state so that `UnhidePost` can restore it. Replies, reactions, and reaction counts are preserved intact.

```protobuf
message MsgHidePost {
  string creator = 1;  // Must be post author
  uint64 id = 2;       // Post ID to hide
}

message MsgHidePostResponse {}
```

**Validation:**
- Post must exist and have `status = POST_STATUS_ACTIVE` (`ErrPostDeleted` if tombstoned, `ErrPostHidden` if already hidden)
- Creator must be the post author

**Logic:**
1. Validate creator address
2. Retrieve existing post and verify it is active
3. Verify creator is post author
4. If `initiative_id > 0`: remove content initiative link via `RepKeeper.RemoveContentInitiativeLink` (link is re-registered on unhide)
5. Set `status = POST_STATUS_HIDDEN`, `hidden_by = creator`, `hidden_at = block_time`
6. **`expires_at` is not modified** — hiding does not pause or extend the TTL. If the post is ephemeral, it will still be tombstoned by the EndBlocker when the TTL elapses, making the hide irreversible
7. Store hidden post
8. Emit `blog.post.hidden` event

### 5.5. UnhidePost

Restore a previously hidden post.

```protobuf
message MsgUnhidePost {
  string creator = 1;  // Must be post author
  uint64 id = 2;       // Post ID to unhide
}

message MsgUnhidePostResponse {}
```

**Validation:**
- Post must currently have `status = POST_STATUS_HIDDEN` (`ErrPostNotHidden` otherwise)
- Creator must be the post author

**Logic:**
1. Validate creator address
2. Retrieve existing post and verify it is hidden
3. Verify creator is post author
4. Set `status = POST_STATUS_ACTIVE`, clear `hidden_by` and `hidden_at`
5. Store restored post
6. If `initiative_id > 0`: re-register content initiative link via `RepKeeper.RegisterContentInitiativeLink`
7. Emit `blog.post.unhidden` event

### 5.6. UpdateParams

Governance parameter update (all fields).

```protobuf
message MsgUpdateParams {
  string authority = 1;  // Must be x/gov module account
  Params params = 2;     // New parameters
}

message MsgUpdateParamsResponse {}
```

**Validation:**
- Sender must be governance authority
- New params must pass `Validate()`

### 5.7. UpdateOperationalParams

Council-gated update for operational parameters only. This allows the Operations Committee to adjust fees and rate limits without a full governance proposal.

```protobuf
message MsgUpdateOperationalParams {
  string authority = 1;                            // Must be council-authorized
  BlogOperationalParams operational_params = 2;    // New operational parameters
}

message MsgUpdateOperationalParamsResponse {}
```

**Authorization:**
- Checked via `CommonsKeeper.IsCouncilAuthorized(ctx, addr, "commons", "operations")`
- Falls back to governance authority check if `CommonsKeeper` is not wired

**Validation:**
- Operational params must pass `BlogOperationalParams.Validate()`
- Merged params (current params with operational fields replaced) must pass full `Params.Validate()`

**Logic:**
1. Verify authority is council-authorized
2. Validate operational params
3. Get current params
4. Apply operational params: `currentParams.ApplyOperationalParams(op)`
5. Validate merged result
6. Store merged params
7. Emit `blog.operational_params.updated` event

### 5.8. CreateReply

Create a threaded reply to a blog post. Access is controlled by the post's `min_reply_trust_level` setting.

```protobuf
message MsgCreateReply {
  string creator = 1;                                  // Reply author
  uint64 post_id = 2;                                 // Target blog post
  uint64 parent_reply_id = 3;                         // 0 = top-level, >0 = nested under reply
  string body = 4;                                     // Reply content
  sparkdream.common.v1.ContentType content_type = 5;   // Content encoding hint
  string author_bond = 6;                              // Optional DREAM amount to lock as author bond (cosmossdk.io/math.Int, nullable)
}

message MsgCreateReplyResponse {
  uint64 id = 1;  // Assigned reply ID
}
```

**Validation:**
- Creator must be valid bech32
- Post must exist and have `status = POST_STATUS_ACTIVE` (`ErrPostDeleted` if tombstoned, `ErrPostHidden` if hidden)
- `post.replies_enabled` must be true
- Creator must meet the post's `min_reply_trust_level` requirement:
  - If `-1` (open): any valid address (no membership check)
  - If `0` (NEW): creator must be an active member (`RepKeeper.IsActiveMember`)
  - If `1-4`: creator must be an active member with `RepKeeper.GetTrustLevel(ctx, addr) >= min_reply_trust_level`
- Body must be non-empty
- `len(body)` ≤ `params.max_reply_length`
- If `parent_reply_id > 0`: parent reply must exist, belong to the same post, have `status = REPLY_STATUS_ACTIVE` (`ErrReplyDeleted` if tombstoned, `ErrReplyHidden` if hidden), and `parent.depth + 1` ≤ `params.max_reply_depth`
- Creator must not exceed `params.max_replies_per_day`
- If `author_bond` is non-nil and positive: validated by `RepKeeper.CreateAuthorBond` (checks cap, existing bond, DREAM balance)

**Storage Fee:**
- `fee = cost_per_byte.amount * len(body)` (same model as posts)

**Logic:**
1. Validate creator address
2. Retrieve params
3. Validate post exists, is active, and accepts replies
4. Check creator meets the post's `min_reply_trust_level`
5. If nested: validate parent reply exists and is active (not tombstoned or hidden), compute depth
6. Validate body length (`params.max_reply_length`)
7. Check rate limit for replies (`params.max_replies_per_day`)
8. Charge and burn storage fee
9. Determine TTL: if `RepKeeper.IsActiveMember(ctx, creator)` is true, `expires_at = 0` (permanent); otherwise `expires_at = block_time + params.ephemeral_content_ttl` (ephemeral, 0 if `ephemeral_content_ttl == 0`). Note: when `min_reply_trust_level >= 0`, creator is already verified as an active member in step 4, so TTL is always 0; this check only produces non-zero TTL for the `-1` (open) case
10. Create Reply with `depth`, `created_at = block_time`, `status = REPLY_STATUS_ACTIVE`, `fee_bytes_high_water = len(body)`, `expires_at` from step 9
11. Store reply via `AppendReply`
12. If `author_bond` is non-nil and positive and `RepKeeper` is available: call `RepKeeper.CreateAuthorBond(ctx, creator, STAKE_TARGET_BLOG_AUTHOR_BOND, replyID, amount)`. Fail the tx if bond creation fails.
13. Increment `post.reply_count`
14. If `expires_at > 0`: add to `ExpiryIndex`
15. Increment rate limit counter
16. Emit `blog.reply.created` event
17. Return assigned ID

### 5.9. UpdateReply

Update an existing reply. Only the reply author can update.

```protobuf
message MsgUpdateReply {
  string creator = 1;                                  // Must be reply author
  uint64 id = 2;                                       // Reply ID to update
  string body = 3;                                     // New body content
  sparkdream.common.v1.ContentType content_type = 4;   // Content encoding hint
}

message MsgUpdateReplyResponse {}
```

**Validation:**
- Creator must be reply author
- Reply must exist and have `status = REPLY_STATUS_ACTIVE` (`ErrReplyDeleted` or `ErrReplyHidden` otherwise)
- New body must be non-empty and ≤ `params.max_reply_length`

**Storage Fee (high-water mark):**
- Same model as UpdatePost: charged only when new size exceeds `fee_bytes_high_water`

**Logic:**
1. Validate creator and ownership
2. Validate new body length
3. Charge fee if `len(new_body) > reply.fee_bytes_high_water`; update high-water mark if applicable
4. Update reply: set new body, content_type, `edited = true`, `edited_at = block_time`
5. **`expires_at` is not modified** — same as UpdatePost, updates do not extend the TTL
6. Store updated reply
7. Emit `blog.reply.updated` event

### 5.10. DeleteReply

Tombstone a reply. The reply's body is cleared and its status is set to `REPLY_STATUS_DELETED`. Child replies and reactions are preserved intact — no reparenting is needed since the parent tombstone remains in the tree.

```protobuf
message MsgDeleteReply {
  string creator = 1;  // Reply author OR post author
  uint64 id = 2;       // Reply ID to delete
}

message MsgDeleteReplyResponse {}
```

**Authorization:**
- Reply author can delete their own reply
- Post author can delete any reply on their post

**Logic:**
1. Validate creator address
2. Retrieve reply and its parent post
3. Verify reply has `status = REPLY_STATUS_ACTIVE` or `status = REPLY_STATUS_HIDDEN` (`ErrReplyDeleted` otherwise)
4. Verify authorization (reply author or post author)
5. Set `body = ""`, `status = REPLY_STATUS_DELETED`
6. If `expires_at > 0`: remove from `ExpiryIndex` (prevents stale entry iteration in EndBlocker)
7. Store tombstoned reply
8. If reply was `REPLY_STATUS_ACTIVE`: decrement `post.reply_count` (skip if reply was already hidden — `reply_count` was decremented by HideReply)
9. Emit `blog.reply.deleted` event

### 5.11. HideReply

Hide a reply (post author moderation). Hidden replies are excluded from all query results. The reply body is preserved in state so that `UnhideReply` can restore it.

```protobuf
message MsgHideReply {
  string creator = 1;  // Must be post author
  uint64 id = 2;       // Reply ID to hide
}

message MsgHideReplyResponse {}
```

**Validation:**
- Creator must be the author of the post that the reply belongs to
- Reply must have `status = REPLY_STATUS_ACTIVE`

**Logic:**
1. Retrieve reply and parent post
2. Verify creator is post author
3. Set `status = REPLY_STATUS_HIDDEN`, `hidden_by = creator`, `hidden_at = block_time`
4. **`expires_at` is not modified** — hiding does not pause or extend the TTL. If the reply is ephemeral, it will still be tombstoned by the EndBlocker when the TTL elapses, making the hide irreversible (same behavior as HidePost — see Section 5.4)
5. Decrement `post.reply_count` (hidden replies are not "active")
6. Store hidden reply
7. Emit `blog.reply.hidden` event

### 5.12. UnhideReply

Restore a previously hidden reply.

```protobuf
message MsgUnhideReply {
  string creator = 1;  // Must be post author
  uint64 id = 2;       // Reply ID to unhide
}

message MsgUnhideReplyResponse {}
```

**Validation:**
- Creator must be the post author
- Reply must currently have `status = REPLY_STATUS_HIDDEN` (`ErrReplyNotHidden` otherwise)

**Logic:**
1. Retrieve reply and parent post
2. Verify creator is post author and reply is hidden
3. Set `status = REPLY_STATUS_ACTIVE`, clear `hidden_by` and `hidden_at`
4. Increment `post.reply_count`
5. Store restored reply
6. Emit `blog.reply.unhidden` event

### 5.13. React

Add or change a reaction on a post or reply. Requires active membership via x/rep. One reaction per user per target — calling again with a different type replaces the previous reaction.

```protobuf
message MsgReact {
  string creator = 1;              // Reactor (must be active member)
  uint64 post_id = 2;             // Target post
  uint64 reply_id = 3;            // Target reply (0 = reacting to the post)
  ReactionType reaction_type = 4;  // Selected reaction
}

message MsgReactResponse {}
```

**Validation:**
- Creator must be valid bech32 and active member
- Post must exist and have `status = POST_STATUS_ACTIVE` (`ErrPostDeleted` if tombstoned, `ErrPostHidden` if hidden)
- If `reply_id > 0`: reply must exist, belong to the post, and have `status = REPLY_STATUS_ACTIVE` (`ErrReplyDeleted` or `ErrReplyHidden` otherwise)
- `reaction_type` must not be `UNSPECIFIED`
- Creator must not exceed `params.max_reactions_per_day` (only counted for new reactions, not changes)

**Reaction Fee:**
- A flat `params.reaction_fee` is charged when adding a **new** reaction (sent to blog module, then burned)
- **Changing** an existing reaction to a different type: no additional fee
- **Removing** a reaction: no fee
- Skipped if `reaction_fee_exempt` is true or `reaction_fee` is zero/nil

**Logic:**
1. Validate creator, membership, and target existence
2. Check for existing reaction by this user on this target
3. If exists with same type: no-op
4. If exists with different type: decrement old type count, increment new type count, update record (no fee)
5. If new: check rate limit, charge and burn reaction fee, create reaction record, add to `ReactorIndex`, increment type count in `ReactionCounts`, increment rate limit counter
6. Emit `blog.reaction.added` or `blog.reaction.changed` event

### 5.14. RemoveReaction

Remove your own reaction from a post or reply.

```protobuf
message MsgRemoveReaction {
  string creator = 1;   // Must be the reactor
  uint64 post_id = 2;   // Target post
  uint64 reply_id = 3;  // Target reply (0 = post reaction)
}

message MsgRemoveReactionResponse {}
```

**Validation:**
- Reaction must exist for this creator on this target
- Active membership is **not** required — deactivated members can still clean up their own reactions (deliberate design choice)

**Logic:**
1. Retrieve existing reaction
2. Decrement the appropriate count in `ReactionCounts`
3. Delete reaction record and remove from `ReactorIndex`
4. Emit `blog.reaction.removed` event

### 5.15. PinPost

Pin an ephemeral post, making it permanent (clearing its TTL). Only posts with a non-zero `expires_at` (anonymous or non-member posts) can be pinned — member posts are already permanent. Requires active membership at `params.pin_min_trust_level` or above.

```protobuf
message MsgPinPost {
  string creator = 1;  // Must be active member at pin_min_trust_level+
  uint64 id = 2;       // Post ID to pin
}

message MsgPinPostResponse {}
```

**Validation:**
- Post must exist and have `status = POST_STATUS_ACTIVE`
- Post must be ephemeral (`expires_at > 0`); posts with `expires_at == 0` are already permanent (`ErrContentNotEphemeral`)
- Post must not already be pinned (`ErrAlreadyPinned`)
- Post must not be expired (`ErrPostExpired` if `block_time >= expires_at`)
- Creator must be an active member with `RepKeeper.GetTrustLevel(ctx, addr) >= params.pin_min_trust_level`
- Creator must not exceed `params.max_pins_per_day`

**Logic:**
1. Validate creator address and membership/trust level
2. Retrieve post, verify it is active, ephemeral, not already pinned, and not expired
3. Check rate limit for pins (`params.max_pins_per_day`)
4. Set `expires_at = 0`, `pinned_by = creator`, `pinned_at = block_time`
5. If `conviction_sustained == true`: set `conviction_sustained = false`
6. Remove from `ExpiryIndex` (post is now permanent)
7. Increment rate limit counter
8. Store updated post
9. Emit `blog.post.pinned` event

### 5.16. PinReply

Pin an ephemeral reply, making it permanent (clearing its TTL). Same rules as PinPost but for replies. If conviction-sustained, clears the flag (same as PinPost step 5).

```protobuf
message MsgPinReply {
  string creator = 1;  // Must be active member at pin_min_trust_level+
  uint64 id = 2;       // Reply ID to pin
}

message MsgPinReplyResponse {}
```

**Validation:**
- Reply must exist and have `status = REPLY_STATUS_ACTIVE`
- Reply must be ephemeral (`expires_at > 0`); replies with `expires_at == 0` are already permanent (`ErrContentNotEphemeral`)
- Reply must not already be pinned (`ErrAlreadyPinned`)
- Reply must not be expired (`ErrReplyExpired` if `block_time >= expires_at`)
- Creator must be an active member with `RepKeeper.GetTrustLevel(ctx, addr) >= params.pin_min_trust_level`
- Creator must not exceed `params.max_pins_per_day` (shared counter with PinPost)

**Logic:**
1. Validate creator address and membership/trust level
2. Retrieve reply and parent post, verify reply is active, ephemeral, not already pinned, and not expired
3. Check rate limit for pins (`params.max_pins_per_day`)
4. Set `expires_at = 0`, `pinned_by = creator`, `pinned_at = block_time`
5. Remove from `ExpiryIndex`
6. Increment rate limit counter
7. Store updated reply
8. Emit `blog.reply.pinned` event

---

## 6. Queries

| Query | Endpoint | Input | Output |
|-------|----------|-------|--------|
| `Params` | `/sparkdream/blog/v1/params` | - | Current Params |
| `ShowPost` | `/sparkdream/blog/v1/show_post/{id}` | id (uint64) | Post |
| `ListPost` | `/sparkdream/blog/v1/list_post` | pagination | []Post |
| `ShowReply` | `/sparkdream/blog/v1/show_reply/{id}` | id (uint64) | Reply |
| `ListReplies` | `/sparkdream/blog/v1/list_replies/{post_id}` | post_id, filters, include_hidden, pagination | []Reply |
| `ReactionCounts` | `/sparkdream/blog/v1/reaction_counts/{post_id}` | post_id, reply_id | ReactionCounts |
| `UserReaction` | `/sparkdream/blog/v1/user_reaction/{creator}/{post_id}` | creator, post_id, reply_id | Reaction |
| `ListReactions` | `/sparkdream/blog/v1/list_reactions/{post_id}` | post_id, reply_id, pagination | []Reaction |
| `ListReactionsByCreator` | `/sparkdream/blog/v1/list_reactions_by_creator/{creator}` | creator, pagination | []Reaction |
| `ListPostsByCreator` | `/sparkdream/blog/v1/list_posts_by_creator/{creator}` | creator, include_hidden, pagination | []Post |
| `AnonymousPostMeta` | `/sparkdream/blog/v1/anonymous_post_meta/{post_id}` | post_id | AnonymousPostMetadata |
| `AnonymousReplyMeta` | `/sparkdream/blog/v1/anonymous_reply_meta/{reply_id}` | reply_id | AnonymousPostMetadata |
| `IsNullifierUsed` | `/sparkdream/blog/v1/is_nullifier_used` | domain, scope, nullifier (hex) | bool |
| `ListExpiringContent` | `/sparkdream/blog/v1/list_expiring_content` | expires_before, content_type, pagination | []Post + []Reply |

### 6.1. Params

Returns current module parameters. Returns default params if none are set.

```protobuf
message QueryParamsRequest {}

message QueryParamsResponse {
  Params params = 1;
}
```

### 6.2. ShowPost

Returns a single post by ID. Tombstoned posts are returned with `status = POST_STATUS_DELETED` and empty title/body. Hidden posts are returned with `status = POST_STATUS_HIDDEN` and full content preserved — this allows post authors to view their hidden posts by ID for unhide decisions. Clients should check the `status` field and display accordingly.

```protobuf
message QueryShowPostRequest {
  uint64 id = 1;
}

message QueryShowPostResponse {
  Post post = 1;
}
```

**Errors:**
- Returns `ErrKeyNotFound` if post doesn't exist (never assigned or invalid ID)

### 6.3. ListPost

Returns paginated list of all posts. Hidden posts are excluded from the default listing. Tombstoned posts are included with `status = POST_STATUS_DELETED` and empty title/body — clients should check the `status` field and display accordingly (e.g., "[deleted]" placeholder).

```protobuf
message QueryListPostRequest {
  cosmos.base.query.v1beta1.PageRequest pagination = 1;
}

message QueryListPostResponse {
  repeated Post post = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

### 6.4. ShowReply

Returns a single reply by ID. Tombstoned replies are returned with `status = REPLY_STATUS_DELETED` and empty body. Hidden replies are returned with `status = REPLY_STATUS_HIDDEN` and full content preserved — this allows post authors to view hidden replies by ID for unhide decisions. Clients should check the `status` field and display accordingly.

```protobuf
message QueryShowReplyRequest {
  uint64 id = 1;
}

message QueryShowReplyResponse {
  Reply reply = 1;
}
```

**Errors:**
- Returns `ErrKeyNotFound` if reply doesn't exist

### 6.5. ListReplies

Returns paginated replies for a post with optional parent filtering. Hidden replies are excluded by default; set `include_hidden = true` to include them (useful for post authors reviewing their moderation actions). Tombstoned replies are always included (with empty body) to preserve thread structure.

```protobuf
message QueryListRepliesRequest {
  uint64 post_id = 1;                                        // Required: which post
  bool filter_by_parent = 2;                                 // Whether to filter by parent_reply_id
  uint64 parent_reply_id = 3;                                // Only used when filter_by_parent = true (0 = top-level only)
  cosmos.base.query.v1beta1.PageRequest pagination = 4;
  bool include_hidden = 5;                                   // Include hidden replies in results (default: false)
}

message QueryListRepliesResponse {
  repeated Reply replies = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

**Filtering behavior:**
- `filter_by_parent = false`: returns ALL non-hidden replies for the post (any depth)
- `filter_by_parent = true, parent_reply_id = 0`: returns only top-level replies
- `filter_by_parent = true, parent_reply_id = N`: returns only direct children of reply N
- `include_hidden = true`: includes hidden replies (with `status = REPLY_STATUS_HIDDEN`) in the results alongside active and tombstoned replies. Since queries are unauthenticated, this is available to anyone — the privacy model is that hidden content is excluded from *default* listings, not that it is secret

### 6.6. ListPostsByCreator

Returns paginated posts by a specific creator address. Hidden posts are excluded by default; set `include_hidden = true` to include them (useful for authors reviewing their own hidden posts for unhide decisions). Tombstoned posts are always included. Uses the `CreatorPostIndex` for efficient lookup.

```protobuf
message QueryListPostsByCreatorRequest {
  string creator = 1;                                          // Creator address
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
  bool include_hidden = 3;                                     // Include hidden posts in results (default: false)
}

message QueryListPostsByCreatorResponse {
  repeated Post posts = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

Since queries are unauthenticated, `include_hidden` is available to anyone — the privacy model is that hidden content is excluded from *default* listings, not that it is secret. Clients can use the `status` field to filter or display hidden posts differently.

### 6.7. ReactionCounts

Returns aggregate reaction counts for a post or a specific reply.

```protobuf
message QueryReactionCountsRequest {
  uint64 post_id = 1;    // Target post
  uint64 reply_id = 2;   // Target reply (0 = post counts)
}

message QueryReactionCountsResponse {
  ReactionCounts counts = 1;
}
```

### 6.8. UserReaction

Check what reaction (if any) a specific user has on a target.

```protobuf
message QueryUserReactionRequest {
  string creator = 1;     // User address
  uint64 post_id = 2;     // Target post
  uint64 reply_id = 3;    // Target reply (0 = post reaction)
}

message QueryUserReactionResponse {
  Reaction reaction = 1;  // nil if user has not reacted
}
```

### 6.9. AnonymousPostMeta

Returns the anonymous posting metadata for a post, if the post was created anonymously.

```protobuf
message QueryAnonymousPostMetaRequest {
  uint64 post_id = 1;
}

message QueryAnonymousPostMetaResponse {
  AnonymousPostMetadata metadata = 1;  // nil if post is not anonymous
}
```

**Errors:**
- Returns `ErrKeyNotFound` if post doesn't exist
- Returns nil `metadata` (not an error) if the post exists but was not created anonymously

**Tombstoned posts:** Anonymous metadata is intentionally retained after a post is tombstoned (whether by manual delete, TTL expiry, or EndBlocker). The metadata serves as an audit trail — it records the nullifier (for spam pattern detection) and proven trust level. Queries return metadata for tombstoned posts normally.

### 6.10. AnonymousReplyMeta

Returns the anonymous posting metadata for a reply, if the reply was created anonymously.

```protobuf
message QueryAnonymousReplyMetaRequest {
  uint64 reply_id = 1;
}

message QueryAnonymousReplyMetaResponse {
  AnonymousPostMetadata metadata = 1;  // nil if reply is not anonymous
}
```

**Errors:**
- Returns `ErrKeyNotFound` if reply doesn't exist
- Returns nil `metadata` (not an error) if the reply exists but was not created anonymously

### 6.11. ListExpiringContent

Returns paginated list of ephemeral content expiring before a given timestamp. Useful for curators who want to review and pin valuable content before it expires. Uses the `ExpiryIndex` for efficient prefix iteration.

```protobuf
message QueryListExpiringContentRequest {
  int64 expires_before = 1;                                  // Return content expiring before this Unix timestamp (required; 0 = use current block_time + 86400)
  string content_type = 2;                                   // Filter by type: "post", "reply", or "" (both)
  cosmos.base.query.v1beta1.PageRequest pagination = 3;
}

message QueryListExpiringContentResponse {
  repeated Post posts = 1;
  repeated Reply replies = 2;
  cosmos.base.query.v1beta1.PageResponse pagination = 3;
}
```

**Notes:**
- Hidden content is included (curators may want to review and pin it before expiry)
- Tombstoned content is excluded (already expired or manually deleted)
- Results are ordered by `expires_at` ascending (soonest to expire first)
- **Pagination** operates on the `ExpiryIndex` (timestamp-ordered, interleaving posts and replies). The `content_type` filter is applied as a post-processing step after index iteration — when filtering by type, some pages may contain fewer results than the requested page size. The `pagination.next_key` token always points into the `ExpiryIndex`, so subsequent pages resume correctly regardless of filtering

### 6.12. IsNullifierUsed

Check if a nullifier has already been used. Clients can call this before submitting an anonymous post/reply to avoid wasting gas on a duplicate. The client must provide the domain and scope matching the intended action (these are known at proof generation time).

```protobuf
message QueryIsNullifierUsedRequest {
  string nullifier_hex = 1;   // Hex-encoded 32-byte nullifier
  uint64 domain = 2;          // Nullifier domain (1 = anonymous post, 2 = anonymous reply)
  uint64 scope = 3;           // Nullifier scope (epoch for domain 1, post_id for domain 2)
}

message QueryIsNullifierUsedResponse {
  bool used = 1;
}
```

### 6.13. ListReactions

Returns paginated individual reaction records for a post or reply. Useful for clients that want to display reactor identities alongside aggregate counts (e.g., "Alice, Bob, and 3 others liked this").

```protobuf
message QueryListReactionsRequest {
  uint64 post_id = 1;                                        // Target post
  uint64 reply_id = 2;                                       // Target reply (0 = post reactions)
  cosmos.base.query.v1beta1.PageRequest pagination = 3;
}

message QueryListReactionsResponse {
  repeated Reaction reactions = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

**Notes:**
- Uses prefix iteration on `Reaction/value/{post_id}/{reply_id}/`
- Returns all reaction types; clients can filter by `reaction_type` as needed
- Post must exist; returns `ErrKeyNotFound` if post doesn't exist
- If `reply_id > 0`: reply must exist and belong to the post
- Reactions on tombstoned content are returned (reactions are preserved across tombstoning)
- Reactions on hidden content are returned (reactions are not affected by hide/unhide)

### 6.14. ListReactionsByCreator

Returns paginated reactions by a specific creator address. Useful for "my reactions" views where a user wants to see everything they've reacted to.

```protobuf
message QueryListReactionsByCreatorRequest {
  string creator = 1;                                          // Reactor address
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QueryListReactionsByCreatorResponse {
  repeated Reaction reactions = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

**Notes:**
- Uses prefix iteration on `ReactorIndex` (`Reaction/creator/{creator}/`), then fetches each `Reaction` record
- Returns reactions on all targets (posts and replies) regardless of target status — reactions on tombstoned or hidden content are included
- Creator address must be valid bech32

---

## 7. Keeper Methods

### 7.1. Post Operations

```go
// AppendPost creates a new post with auto-incremented ID
func (k Keeper) AppendPost(ctx context.Context, post types.Post) uint64

// SetPost stores/updates a post at its current ID
func (k Keeper) SetPost(ctx context.Context, post types.Post)

// GetPost retrieves a post by ID
func (k Keeper) GetPost(ctx context.Context, id uint64) (val types.Post, found bool)

// RemovePost deletes a post record from the store. Internal method — not called by any message handler; normal deletion uses tombstoning via MsgDeletePost.
func (k Keeper) RemovePost(ctx context.Context, id uint64)
```

### 7.2. Counter Operations

```go
// GetPostCount returns the current post counter (0 if unset)
func (k Keeper) GetPostCount(ctx context.Context) uint64

// SetPostCount updates the post counter
func (k Keeper) SetPostCount(ctx context.Context, count uint64)
```

### 7.3. Helper Methods

```go
// IsGovAuthority returns true if addr matches the module authority
func (k Keeper) IsGovAuthority(addr string) bool

// isCouncilAuthorized delegates to CommonsKeeper.IsCouncilAuthorized,
// falling back to IsGovAuthority if CommonsKeeper is nil
func (k Keeper) isCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
```

### 7.4. Reply Operations

```go
// AppendReply creates a new reply with auto-incremented ID
func (k Keeper) AppendReply(ctx context.Context, reply types.Reply) uint64

// SetReply stores/updates a reply at its current ID
func (k Keeper) SetReply(ctx context.Context, reply types.Reply)

// GetReply retrieves a reply by ID
func (k Keeper) GetReply(ctx context.Context, id uint64) (val types.Reply, found bool)

// RemoveReply deletes a reply record from the store. Internal method — not called by any message handler; normal deletion uses tombstoning via MsgDeleteReply.
func (k Keeper) RemoveReply(ctx context.Context, id uint64)

// GetReplyCount returns the current reply counter (0 if unset)
func (k Keeper) GetReplyCount(ctx context.Context) uint64

// SetReplyCount updates the reply counter
func (k Keeper) SetReplyCount(ctx context.Context, count uint64)
```

### 7.5. Reaction Operations

```go
// SetReaction stores a reaction record (one per user per target)
func (k Keeper) SetReaction(ctx context.Context, reaction types.Reaction)

// GetReaction retrieves a user's reaction on a target
func (k Keeper) GetReaction(ctx context.Context, postId uint64, replyId uint64, creator string) (val types.Reaction, found bool)

// RemoveReaction deletes a reaction record
func (k Keeper) RemoveReaction(ctx context.Context, postId uint64, replyId uint64, creator string)

// GetReactionCounts retrieves aggregate counts for a target
func (k Keeper) GetReactionCounts(ctx context.Context, postId uint64, replyId uint64) types.ReactionCounts

// SetReactionCounts stores aggregate counts for a target
func (k Keeper) SetReactionCounts(ctx context.Context, postId uint64, replyId uint64, counts types.ReactionCounts)
```

### 7.6. Trust Level Check

```go
// meetsReplyTrustLevel checks if addr meets the post's min_reply_trust_level.
// If min_reply_trust_level == -1: always returns true (open to all).
// If min_reply_trust_level == 0: checks IsActiveMember.
// If min_reply_trust_level >= 1: checks IsActiveMember AND GetTrustLevel >= required.
// PANICS if RepKeeper is nil (RepKeeper is a required dependency).
func (k Keeper) meetsReplyTrustLevel(ctx context.Context, addr sdk.AccAddress, minLevel int32) bool

// isActiveMember checks if addr is an active member via RepKeeper.
// PANICS if RepKeeper is nil (RepKeeper is a required dependency).
func (k Keeper) isActiveMember(ctx context.Context, addr sdk.AccAddress) bool
```

### 7.6a. Author Bond (via RepKeeper)

Author bond operations are delegated to `RepKeeper` — x/blog does not implement these methods directly. The following RepKeeper methods are called from message handlers:

```go
// CreateAuthorBond locks DREAM as an author bond on content.
// Called from MsgCreatePost and MsgCreateReply when author_bond is non-nil and positive.
// targetType: STAKE_TARGET_BLOG_AUTHOR_BOND for both posts and replies.
RepKeeper.CreateAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) (uint64, error)

// SlashAuthorBond burns the author bond on moderation action.
// NOT used by x/blog — blog hide is author-only self-moderation, not sentinel moderation.
// Authors can voluntarily unstake their bonds via x/rep's MsgUnstake.
RepKeeper.SlashAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) error

// GetAuthorBond retrieves the author bond stake for a content target.
RepKeeper.GetAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (reptypes.Stake, error)

// GetContentConviction returns the total conviction score for a content target.
// Clients query this via x/rep's ContentConviction RPC — x/blog does not embed conviction in its responses.
RepKeeper.GetContentConviction(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (math.LegacyDec, error)

// GetContentStakes returns all conviction stakes on a content target.
RepKeeper.GetContentStakes(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) ([]reptypes.Stake, error)
```

**Design note:** x/blog has no external moderation (no sentinel system), so `SlashAuthorBond` is never called from x/blog handlers. Author bonds are purely voluntary skin-in-the-game. If cross-module council moderation is added later, slash wiring can be added at that point.

### 7.7. Rate Limit Check

```go
// checkRateLimit reads the current day's counter for the given action type and address.
// Returns ErrRateLimitExceeded if the counter >= the configured limit.
// If the stored day differs from the current day, resets the counter (lazy cleanup).
func (k Keeper) checkRateLimit(ctx context.Context, actionType string, addr sdk.AccAddress, limit uint32) error

// incrementRateLimit increments the current day's counter for the given action type and address.
func (k Keeper) incrementRateLimit(ctx context.Context, actionType string, addr sdk.AccAddress)
```

### 7.8. Expiry Operations

```go
// AddToExpiryIndex adds a post or reply to the expiry index at the given timestamp.
// Called when creating anonymous content with a non-zero expires_at.
func (k Keeper) AddToExpiryIndex(ctx context.Context, expiresAt int64, contentType string, id uint64)

// RemoveFromExpiryIndex removes a post or reply from the expiry index.
// Called when content is pinned (cleared TTL) or manually tombstoned before expiry.
func (k Keeper) RemoveFromExpiryIndex(ctx context.Context, expiresAt int64, contentType string, id uint64)

// TombstoneExpiredContent iterates the expiry index up to block_time and tombstones
// all expired anonymous posts and replies. Returns the count of tombstoned items.
// Called by EndBlocker.
func (k Keeper) TombstoneExpiredContent(ctx context.Context) uint64
```

### 7.9. Parameter Helpers (types)

```go
// ApplyOperationalParams copies cost_per_byte, cost_per_byte_exempt, reaction_fee,
// reaction_fee_exempt, max_posts_per_day, max_replies_per_day, max_reactions_per_day,
// anonymous_min_trust_level, anon_subsidy_budget_per_epoch, anon_subsidy_max_per_post,
// anon_subsidy_relay_addresses, ephemeral_content_ttl, and max_pins_per_day from op,
// preserving all other fields (max_title_length, max_body_length, max_reply_length,
// max_reply_depth, anonymous_posting_enabled, pin_min_trust_level,
// min_ephemeral_content_ttl, max_cost_per_byte, max_reaction_fee)
func (p Params) ApplyOperationalParams(op BlogOperationalParams) Params

// ExtractOperationalParams extracts cost_per_byte, cost_per_byte_exempt, reaction_fee,
// reaction_fee_exempt, max_posts_per_day, max_replies_per_day, max_reactions_per_day,
// anonymous_min_trust_level, anon_subsidy_budget_per_epoch, anon_subsidy_max_per_post,
// anon_subsidy_relay_addresses, ephemeral_content_ttl, and max_pins_per_day
// from the full params
func (p Params) ExtractOperationalParams() BlogOperationalParams
```

---

## 8. Default Parameters

| Parameter | Default | Rationale |
|-----------|---------|-----------|
| `max_title_length` | 200 | Standard headline length |
| `max_body_length` | 10,000 | Reasonable blog post size (~2,000 words) |
| `cost_per_byte` | 100uspark | Discourages state bloat; fees are burned |
| `cost_per_byte_exempt` | false | Storage fees active by default |
| `max_reply_length` | 2,000 | Shorter than posts; encourages concise replies |
| `max_reply_depth` | 5 | Prevents excessive nesting; balances readability with threading |
| `reaction_fee` | 50uspark | Small flat fee to prevent reaction spam; burned |
| `reaction_fee_exempt` | false | Reaction fees active by default |
| `max_posts_per_day` | 10 | Prevents post flooding; reasonable for active authors |
| `max_replies_per_day` | 50 | Prevents reply spam; allows active discussion participation |
| `max_reactions_per_day` | 100 | Prevents mass reaction manipulation; generous for normal use |
| `anonymous_posting_enabled` | true | **Governance-only.** Anonymous posting enabled by default (requires VoteKeeper to be wired). Only disableable via `MsgUpdateParams` (x/gov) — prevents the Operations Committee from silencing anonymous speech, which is the primary whistleblowing mechanism |
| `anonymous_min_trust_level` | 2 | ESTABLISHED — ensures sufficient anonymity set and reputation at stake |
| `anon_subsidy_budget_per_epoch` | 100spark | Epoch draw from Commons Council treasury; zero disables subsidy |
| `anon_subsidy_max_per_post` | 2spark | Caps per-transaction subsidy to prevent budget drain |
| `anon_subsidy_relay_addresses` | [] | No approved relays by default; Operations Committee adds them |
| `ephemeral_content_ttl` | 604,800 (7 days) | Ephemeral content (anonymous + non-member) expires unless pinned; balances free speech with no obligation to preserve bad content |
| `pin_min_trust_level` | 2 (ESTABLISHED) | Ensures only members with meaningful reputation can preserve ephemeral content |
| `max_pins_per_day` | 20 | Prevents mass-pinning of ephemeral content; generous for legitimate curation |
| `min_ephemeral_content_ttl` | 86,400 (1 day) | **Governance-only.** Floor for `ephemeral_content_ttl` — prevents Operations Committee from setting TTL near-zero to suppress ephemeral content |
| `max_cost_per_byte` | 1,000uspark | **Governance-only.** Ceiling for `cost_per_byte` (10x default) — prevents economic censorship via extreme fee increases |
| `max_reaction_fee` | 500uspark | **Governance-only.** Ceiling for `reaction_fee` (10x default) — prevents economic censorship via extreme reaction fees |
| `conviction_renewal_threshold` | 100.0 | Minimum conviction score required to renew anonymous content at TTL expiry; 0 disables conviction renewal (all anonymous content expires normally) |
| `conviction_renewal_period` | 604,800 (7 days) | Duration in seconds to extend TTL by when conviction-renewed; defaults to same as `ephemeral_content_ttl` |

**`Params.Validate()` Rules:**
- `max_title_length` must be > 0
- `max_body_length` must be > 0
- `cost_per_byte` amount must be ≥ 0 (or nil)
- `max_reply_length` must be > 0
- `max_reply_depth` must be > 0 and ≤ 20 (hard ceiling)
- `reaction_fee` amount must be ≥ 0 (or nil)
- `max_posts_per_day` must be > 0
- `max_replies_per_day` must be > 0
- `max_reactions_per_day` must be > 0
- `anonymous_min_trust_level` must be between 0 and 4 inclusive
- `anon_subsidy_budget_per_epoch` amount must be ≥ 0 (or nil); zero disables subsidy
- `anon_subsidy_max_per_post` amount must be ≥ 0 (or nil)
- `anon_subsidy_relay_addresses` must all be valid bech32 addresses (no duplicates); list length ≤ `MaxRelayAddresses` (50)
- `ephemeral_content_ttl` must be ≥ 0; zero disables TTL (all content is permanent regardless of authorship)
- `pin_min_trust_level` must be between 0 and 4 inclusive
- `max_pins_per_day` must be > 0
- `min_ephemeral_content_ttl` must be > 0
- `max_cost_per_byte` amount must be > 0
- `max_reaction_fee` amount must be > 0
- `conviction_renewal_threshold` must be ≥ 0 (zero disables conviction renewal)
- `conviction_renewal_period` must be ≥ 0; if `conviction_renewal_threshold > 0`, must be > 0
- **Cross-field:** if `ephemeral_content_ttl > 0`: must be ≥ `min_ephemeral_content_ttl` (zero is always valid — it disables TTL entirely, making all content permanent)
- **Cross-field:** if `cost_per_byte` is non-nil and non-zero: `cost_per_byte.amount` ≤ `max_cost_per_byte.amount`
- **Cross-field:** if `reaction_fee` is non-nil and non-zero: `reaction_fee.amount` ≤ `max_reaction_fee.amount`

**`BlogOperationalParams.Validate()` Rules:**
- `cost_per_byte` amount must be ≥ 0 (or nil)
- `reaction_fee` amount must be ≥ 0 (or nil)
- `max_posts_per_day` must be > 0
- `max_replies_per_day` must be > 0
- `max_reactions_per_day` must be > 0
- `anonymous_min_trust_level` must be between 0 and 4 inclusive
- `anon_subsidy_budget_per_epoch` amount must be ≥ 0 (or nil)
- `anon_subsidy_max_per_post` amount must be ≥ 0 (or nil)
- `anon_subsidy_relay_addresses` must all be valid bech32 addresses (no duplicates); list length ≤ `MaxRelayAddresses` (50)
- `ephemeral_content_ttl` must be ≥ 0
- `max_pins_per_day` must be > 0
- `conviction_renewal_threshold` must be ≥ 0
- `conviction_renewal_period` must be ≥ 0

Note: `BlogOperationalParams.Validate()` is a subset of `Params.Validate()` covering only the operational fields. In `MsgUpdateOperationalParams`, both `BlogOperationalParams.Validate()` and the merged `Params.Validate()` are called (Section 5.7), so structural constraints (e.g., all lengths > 0, reply depth ≤ 20) and **cross-field constraints** (e.g., `ephemeral_content_ttl >= min_ephemeral_content_ttl`, `cost_per_byte <= max_cost_per_byte`, `reaction_fee <= max_reaction_fee`) are enforced on the final merged result. The governance-only params (`anonymous_posting_enabled`, `min_ephemeral_content_ttl`, `max_cost_per_byte`, `max_reaction_fee`, `pin_min_trust_level`) cannot be changed via `MsgUpdateOperationalParams`, so the Operations Committee cannot bypass these guardrails.

---

## 9. Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrInvalidSigner` | 1100 | Non-governance/non-council signer for parameter updates |
| `ErrNotMember` | 1200 | Creator is not an active member (required for reactions, and replies when trust level ≥ 0) |
| `ErrInsufficientTrustLevel` | 1201 | Creator's trust level is below the post's `min_reply_trust_level` or anonymous `min_trust_level` |
| `ErrPostNotFound` | 1202 | Post ID doesn't exist |
| `ErrPostDeleted` | 1203 | Post is tombstoned and cannot be updated, replied to, or reacted to |
| `ErrPostHidden` | 1204 | Post is hidden and cannot be updated, replied to, or reacted to (note: queries return hidden posts with HIDDEN status — see Sections 6.2, 6.6) |
| `ErrPostNotHidden` | 1205 | Cannot unhide a post that is not hidden |
| `ErrReplyNotFound` | 1206 | Reply ID doesn't exist |
| `ErrReplyDeleted` | 1207 | Reply is tombstoned and cannot be updated, replied to, or reacted to |
| `ErrReplyHidden` | 1208 | Reply is hidden and cannot be updated, replied to, or reacted to (note: queries return hidden replies with HIDDEN status — see Sections 6.4, 6.5) |
| `ErrReplyNotHidden` | 1209 | Cannot unhide a reply that is not hidden |
| `ErrRepliesDisabled` | 1210 | Post has replies disabled |
| `ErrMaxReplyDepth` | 1211 | Reply nesting exceeds `max_reply_depth` |
| `ErrUnauthorized` | 1212 | Non-creator attempting update/delete, non-post-author attempting hide |
| `ErrRateLimitExceeded` | 1213 | Address has exceeded the daily rate limit for this action |
| `ErrInvalidReactionType` | 1214 | Invalid reaction type value |
| `ErrReactionNotFound` | 1215 | No reaction to remove for this user on this target |
| `ErrContentNotEphemeral` | 1216 | Content is already permanent (`expires_at == 0`) and cannot be pinned |
| `ErrAlreadyPinned` | 1217 | Content is already pinned |
| `ErrAnonPostingDisabled` | 1218 | Anonymous posting is disabled via params or VoteKeeper not wired |
| `ErrInvalidProof` | 1219 | ZK proof verification failed or stale/invalid merkle root |
| `ErrNullifierUsed` | 1220 | Nullifier already used (duplicate anonymous post/reply/reaction) |
| `ErrInvalidNullifier` | 1221 | Invalid nullifier format |
| `ErrPostExpired` | 1222 | Post has expired (TTL elapsed, not yet tombstoned by EndBlocker) |
| `ErrReplyExpired` | 1223 | Reply has expired |
| `ErrInvalidInitiativeRef` | 1224 | Invalid initiative reference for conviction propagation |

Standard SDK errors used inline:
- `sdkerrors.ErrInvalidRequest` — empty or over-length content, `min_reply_trust_level` out of range (-1 to 4)

---

## 10. Access Control

### 10.1. Post Operations

| Operation | Who Can Execute |
|-----------|-----------------|
| CreatePost | Any valid address |
| UpdatePost | Original creator only (includes reply settings) |
| DeletePost | Original creator only |
| HidePost | Post author only (self-moderation) |
| UnhidePost | Post author only |
| CreateAnonymousPost | Any address as submitter; ZK proof must prove membership at `anonymous_min_trust_level` |
| PinPost | Active member at `pin_min_trust_level` or above (ephemeral posts only — anonymous or non-member) |
| UpdateParams | Governance authority only |
| UpdateOperationalParams | Operations Committee member (or governance authority as fallback) |

### 10.2. Reply Operations

| Operation | Who Can Execute |
|-----------|-----------------|
| CreateReply | Depends on post's `min_reply_trust_level` (-1 = anyone, 0 = any member, 1-4 = member at that trust level+) |
| CreateAnonymousReply | Any address as submitter; ZK proof must prove membership at max(anonymous_min_trust_level, post.min_reply_trust_level) |
| UpdateReply | Reply author only |
| DeleteReply | Reply author OR post author |
| HideReply | Post author only |
| UnhideReply | Post author only |
| PinReply | Active member at `pin_min_trust_level` or above (ephemeral replies only — anonymous or non-member) |

### 10.3. Reaction Operations

| Operation | Who Can Execute |
|-----------|-----------------|
| React | Any active member (x/rep) |
| RemoveReaction | Reactor only (own reaction) |

### 10.4. Query Operations

All queries are public and require no authentication. Hidden posts and hidden replies are excluded from list query results by default; callers can set `include_hidden = true` on `ListPostsByCreator` and `ListReplies` to include them. `ShowPost` and `ShowReply` return hidden content with the `HIDDEN` status field set (clients should check `status` and display accordingly). Tombstoned posts and replies are returned with empty content and their `DELETED` status field set.

---

## 11. CLI Commands

### 11.1. Post Transactions

```bash
# Create a new post
sparkdreamd tx blog create-post "Title" "Body content" --from alice

# Create a post with content type
sparkdreamd tx blog create-post "Title" "Body content" --content-type CONTENT_TYPE_MARKDOWN --from alice

# Create a post open to non-member replies
sparkdreamd tx blog create-post "Title" "Body" --min-reply-trust-level -1 --from alice

# Create a post requiring ESTABLISHED+ trust to reply
sparkdreamd tx blog create-post "Title" "Body" --min-reply-trust-level 2 --from alice

# Create a post with an author bond (locks 500 DREAM as skin-in-the-game)
sparkdreamd tx blog create-post "Title" "Body" --author-bond 500 --from alice

# Update an existing post (full replacement — fetches current state, overrides specified flags)
sparkdreamd tx blog update-post "New Title" "New Body" 1 --from alice

# Update post and disable replies
sparkdreamd tx blog update-post "Title" "Body" 1 --replies-enabled=false --from alice

# Update post and set minimum trust level to ESTABLISHED+
sparkdreamd tx blog update-post "Title" "Body" 1 --min-reply-trust-level 2 --from alice

# Delete a post (tombstones — clears content, preserves replies)
sparkdreamd tx blog delete-post 1 --from alice

# Hide a post (soft-delete — excluded from list queries, content preserved)
sparkdreamd tx blog hide-post 1 --from alice

# Unhide a post
sparkdreamd tx blog unhide-post 1 --from alice
```

### 11.2. Reply Transactions

```bash
# Create a top-level reply (member-only)
sparkdreamd tx blog create-reply 1 "Great post!" --from bob

# Create a reply with an author bond
sparkdreamd tx blog create-reply 1 "In-depth analysis..." --author-bond 200 --from bob

# Create a nested reply (under reply ID 5)
sparkdreamd tx blog create-reply 1 "I agree" --parent-reply-id 5 --from carol

# Update a reply
sparkdreamd tx blog update-reply 3 "Updated reply text" --from bob

# Delete a reply (tombstones — clears body, preserves thread structure)
sparkdreamd tx blog delete-reply 3 --from bob

# Hide a reply (post author moderation — excluded from all queries)
sparkdreamd tx blog hide-reply 3 --from alice

# Unhide a reply
sparkdreamd tx blog unhide-reply 3 --from alice
```

### 11.3. Reaction Transactions

```bash
# React to a post (member-only)
sparkdreamd tx blog react 1 REACTION_TYPE_LIKE --from bob

# React to a reply
sparkdreamd tx blog react 1 REACTION_TYPE_INSIGHTFUL --reply-id 5 --from carol

# Remove your reaction
sparkdreamd tx blog remove-reaction 1 --from bob
sparkdreamd tx blog remove-reaction 1 --reply-id 5 --from carol
```

Note: `UpdateParams` and `UpdateOperationalParams` are skipped from AutoCLI (authority/council-gated).

### 11.4. Pin Transactions

```bash
# Pin an anonymous post (requires ESTABLISHED+ trust)
sparkdreamd tx blog pin-post 1 --from alice

# Pin an anonymous reply (requires ESTABLISHED+ trust)
sparkdreamd tx blog pin-reply 3 --from alice
```

### 11.5. Anonymous Posting Transactions

Anonymous posting requires a client-side ZK proof. The CLI commands accept hex-encoded proof data — in practice, a frontend or dedicated tool generates the proof and calls these commands programmatically.

```bash
# Create an anonymous post (proof, nullifier, merkle_root are hex-encoded)
sparkdreamd tx blog create-anonymous-post "Title" "Body" \
  --proof <hex> --nullifier <hex> --merkle-root <hex> --min-trust-level 2 \
  --from relay_address

# Create an anonymous reply (always top-level)
sparkdreamd tx blog create-anonymous-reply 1 "Reply body" \
  --proof <hex> --nullifier <hex> --merkle-root <hex> --min-trust-level 2 \
  --from relay_address
```

### 11.6. Subsidy Queries

```bash
# Check current subsidy account balance
sparkdreamd query bank balances $(sparkdreamd query blog module-address --output json | jq -r '.address')

# View subsidy params (part of module params)
sparkdreamd query blog params
```

Subsidy parameters (budget, max per post, relay addresses) are managed via `MsgUpdateOperationalParams` — no separate subsidy commands are needed. The subsidy account balance is the blog module account's bank balance, queryable via `x/bank`.

### 11.7. Queries

```bash
# Get module parameters
sparkdreamd query blog params

# Get a specific post
sparkdreamd query blog show-post 1

# List all posts with pagination
sparkdreamd query blog list-post --limit 10 --offset 0

# List posts by a specific creator
sparkdreamd query blog list-posts-by-creator sprkdrm1abc...

# List posts by creator including hidden posts
sparkdreamd query blog list-posts-by-creator sprkdrm1abc... --include-hidden

# Get a specific reply
sparkdreamd query blog show-reply 3

# List all replies on a post
sparkdreamd query blog list-replies 1

# List all replies including hidden ones (for post author moderation review)
sparkdreamd query blog list-replies 1 --include-hidden

# List top-level replies only
sparkdreamd query blog list-replies 1 --filter-by-parent --parent-reply-id 0

# List children of a specific reply
sparkdreamd query blog list-replies 1 --filter-by-parent --parent-reply-id 5

# Get reaction counts for a post
sparkdreamd query blog reaction-counts 1

# Get reaction counts for a reply
sparkdreamd query blog reaction-counts 1 --reply-id 5

# Check your reaction on a post
sparkdreamd query blog user-reaction 1 sprkdrm1abc...

# Check your reaction on a reply
sparkdreamd query blog user-reaction 1 sprkdrm1abc... --reply-id 5

# List all reactions on a post
sparkdreamd query blog list-reactions 1

# List all reactions on a reply
sparkdreamd query blog list-reactions 1 --reply-id 5

# List all your reactions across all posts/replies
sparkdreamd query blog list-reactions-by-creator sprkdrm1abc...

# Get anonymous post metadata
sparkdreamd query blog anonymous-post-meta 1

# Get anonymous reply metadata
sparkdreamd query blog anonymous-reply-meta 3

# Check if an epoch-scoped nullifier has been used (domain 1 = anonymous post)
sparkdreamd query blog is-nullifier-used <nullifier_hex> --domain 1 --scope <epoch>

# Check if a post-scoped nullifier has been used (domain 2 = anonymous reply)
sparkdreamd query blog is-nullifier-used <nullifier_hex> --domain 2 --scope <post_id>

# List ephemeral content expiring in the next 24 hours
sparkdreamd query blog list-expiring-content --expires-before $(date -d '+1 day' +%s)

# List only expiring posts (exclude replies)
sparkdreamd query blog list-expiring-content --expires-before $(date -d '+1 day' +%s) --content-type post
```

---

## 12. Storage Fee Economics

Posts and replies incur a per-byte storage fee that is burned, creating deflationary pressure proportional to on-chain content usage.

| Operation | Fee Charged |
|-----------|-------------|
| CreatePost | `cost_per_byte * (len(title) + len(body))` |
| UpdatePost | `cost_per_byte * max(0, new_size - fee_bytes_high_water)` (high-water mark) |
| DeletePost | No fee |
| HidePost | No fee |
| UnhidePost | No fee |
| CreateReply | `cost_per_byte * len(body)` |
| UpdateReply | `cost_per_byte * max(0, new_size - fee_bytes_high_water)` (high-water mark) |
| DeleteReply | No fee |
| React (new) | `reaction_fee` flat fee (burned) |
| React (change type) | No fee |
| RemoveReaction | No fee |
| CreateAnonymousPost | `cost_per_byte * (len(title) + len(body))` (charged to submitter, or subsidized — see below) |
| CreateAnonymousReply | `cost_per_byte * len(body)` (charged to submitter, or subsidized — see below) |
| PinPost | No fee |
| PinReply | No fee |

- Fees are sent from the creator to the `blog` module account and immediately burned
- **Anonymous posting subsidy:** If the submitter is in `params.anon_subsidy_relay_addresses` and the module's subsidy balance is sufficient, storage fees are deducted from the module account (up to `anon_subsidy_max_per_post`) instead of the submitter. Any excess beyond the cap is charged to the submitter. See Section 21.6 for the full mechanism
- Setting `cost_per_byte_exempt = true` disables storage fees (posts and replies)
- Setting `reaction_fee_exempt = true` disables reaction fees (independent of storage fees)
- The Operations Committee can adjust fees and rate limits via `MsgUpdateOperationalParams` without a governance vote

---

## 13. Genesis

```protobuf
message GenesisState {
  Params params = 1;
  repeated Post posts = 2;
  uint64 post_count = 3;
  repeated Reply replies = 4;
  uint64 reply_count = 5;
  repeated Reaction reactions = 6;
  repeated GenesisReactionCounts reaction_counts = 7;
  repeated AnonymousPostMetadata anonymous_post_meta = 8;    // Metadata for anonymous posts
  repeated AnonymousPostMetadata anonymous_reply_meta = 9;   // Metadata for anonymous replies
  repeated GenesisNullifierEntry nullifiers = 10;            // Used anonymous nullifiers
  uint64 anon_subsidy_last_epoch = 11;                       // Last epoch for which subsidy was drawn
}

// Wrapper to store keyed ReactionCounts in genesis
message GenesisReactionCounts {
  uint64 post_id = 1;
  uint64 reply_id = 2;
  ReactionCounts counts = 3;
}

// Wrapper to store keyed nullifier entries in genesis
message GenesisNullifierEntry {
  string nullifier_hex = 1;   // Hex-encoded 32-byte nullifier
  int64 used_at = 2;          // Block time when recorded
  uint64 domain = 3;          // Nullifier domain (1 = anonymous post, 2 = anonymous reply)
  uint64 scope = 4;           // Nullifier scope (epoch for domain 1, post_id for domain 2)
}
```

| Hook | Implementation |
|------|----------------|
| `InitGenesis` | Sets params, restores all posts, replies, reactions, reaction counts, counters, anonymous metadata, nullifiers, subsidy epoch tracker; rebuilds derived indexes |
| `ExportGenesis` | Exports params and all state (posts, replies, reactions, reaction counts, counters, anonymous metadata, nullifiers, subsidy epoch tracker) |

All state is preserved across genesis import/export. This includes tombstoned posts, hidden posts, hidden replies, tombstoned replies, anonymous post metadata, used nullifiers, subsidy epoch tracker, and pin/expiry fields on posts and replies — the full state is exported and restored faithfully. The subsidy account balance is preserved by the bank module's own genesis (it is the blog module account balance).

**Derived indexes are not exported.** `ReplyPostIndex`, `CreatorPostIndex`, `ExpiryIndex`, and `ReactorIndex` are rebuilt during `InitGenesis` by iterating imported posts, replies, and reactions.

**Rate limit entries are not exported.** `RateLimitEntry` records are ephemeral (day-scoped with lazy cleanup) and reset to zero on chain restart. This is intentional — rate limits serve as real-time spam prevention, not persistent state.

**Genesis Validation (`InitGenesis`):**
- All posts must have `status` of `POST_STATUS_ACTIVE`, `POST_STATUS_HIDDEN`, or `POST_STATUS_DELETED` (not `UNSPECIFIED`)
- All replies must have `status` of `REPLY_STATUS_ACTIVE`, `REPLY_STATUS_HIDDEN`, or `REPLY_STATUS_DELETED` (not `UNSPECIFIED`)
- All post IDs must be < `post_count`; all reply IDs must be < `reply_count`
- All `reply.post_id` must reference an existing post
- All `reply.parent_reply_id` must reference an existing reply (or 0 for top-level)
- All `reply.depth` must be consistent with the parent chain (top-level = 0, each nesting +1)
- No duplicate reactions (same `post_id`/`reply_id`/`creator`)
- All reaction targets must reference existing posts/replies
- All `reaction_counts` entries must match recomputed counts from individual reactions
- `post.reply_count` must match the count of `REPLY_STATUS_ACTIVE` replies for each post
- All `anonymous_post_meta` entries must reference an existing post with `creator == module_account_address`
- All `anonymous_reply_meta` entries must reference an existing reply with `creator == module_account_address`
- No duplicate nullifiers in `nullifiers` list
- All posts with `pinned_by != ""` must have `expires_at == 0` (pinned content has cleared TTL)
- All replies with `pinned_by != ""` must have `expires_at == 0`
- No active member-authored post or reply should have `expires_at > 0` (member content is permanent at creation; note: this is a soft invariant — membership may have changed since creation, so genesis validation only warns, does not reject)
- All posts and replies with `edited = true` must have `edited_at > 0` and `edited_at >= created_at`; conversely `edited = false` must have `edited_at == 0` (soft check — warns, does not reject, since these are informational fields)
- Params must pass `Validate()`

**Consensus Version:** 1

---

## 14. Module Lifecycle

| Hook | Implementation |
|------|----------------|
| `BeginBlock` | No-op |
| `EndBlock` | (1) Process expired ephemeral content (auto-upgrade or tombstone); (2) Anonymous posting subsidy draw; (3) Prune stale epoch-scoped nullifiers |

**EndBlock — TTL Expiry:**

Iterates the `ExpiryIndex` for all entries where `expires_at <= block_time`. For each expired entry:

1. Retrieve the post or reply
2. If already tombstoned (e.g., manually deleted before TTL): remove from expiry index, skip
3. If already hidden: proceed to step 4 (hidden ephemeral content is not shielded from expiry — see note below)
4. **Membership auto-upgrade check (non-anonymous only):** If the creator is a real address (not module account, i.e. non-anonymous) and `RepKeeper.IsActiveMember(ctx, creator)` is true: the creator has since joined x/rep. Clear `expires_at = 0`, remove from `ExpiryIndex`, and emit `blog.post.upgraded` or `blog.reply.upgraded` event. **No additional fee is charged** — the creator already paid full storage fees at creation, and membership itself is the qualifying event. Skip tombstoning.
5. **Conviction check (anonymous only):** If the creator is the module account (anonymous content) and `params.conviction_renewal_threshold > 0`: query `RepKeeper.GetContentConviction(ctx, targetType, targetID)` where `targetType` = `STAKE_TARGET_CONTENT` and `targetID` encodes `"blog/post/{id}"` or `"blog/reply/{id}"`.
   - **Entering conviction-sustained state (first expiry):** If `conviction_sustained == false` and conviction score ≥ threshold: set `conviction_sustained = true`, set `expires_at = block_time + params.conviction_renewal_period`, update `ExpiryIndex`, and emit `blog.post.conviction_sustained` or `blog.reply.conviction_sustained` event. Skip tombstoning.
   - **Renewal (already conviction-sustained):** If `conviction_sustained == true` and conviction score ≥ threshold: set `expires_at = block_time + params.conviction_renewal_period`, update `ExpiryIndex`, emit `blog.post.renewed` or `blog.reply.renewed` event. Skip tombstoning.
   - **Expiry (conviction dropped):** If conviction score < threshold: set `conviction_sustained = false`. Proceed to tombstone (step 6).
6. Tombstone the content: clear title/body (post) or body (reply), set `status = DELETED`
7. If tombstoning a reply with `status = REPLY_STATUS_ACTIVE`: decrement `post.reply_count`. If `status = REPLY_STATUS_HIDDEN`: do NOT decrement (already decremented by HideReply)
8. Remove from `ExpiryIndex`
9. Emit `blog.post.expired` or `blog.reply.expired` event

**Why no unstake enforcement is needed:** Time-weighted conviction (`conviction(t) = stake_amount * (1 - 2^(-t / half_life))`) means flash-staking is ineffective — a stake placed moments before a renewal check contributes near-zero conviction. Stakers can freely unstake at any time; if conviction drops below threshold, the content expires at the next renewal check. See [anonymous-posting.md](anonymous-posting.md) § "Conviction-Based Lifetime Extension" for the full cross-module pattern.

**Hidden ephemeral content still expires (or enters conviction-sustained).** If a non-member hides their own post (self-moderation) and the TTL elapses, the EndBlocker tombstones it. For anonymous hidden content, the conviction check still runs — if the community supports hidden content via conviction staking, it enters conviction-sustained state (the hide remains visible but the content is not tombstoned).

The expiry scan is bounded by the number of entries with `expires_at <= block_time`. With the default 7-day TTL and moderate ephemeral content volume (anonymous + non-member posts), this is a small number per block. The `ExpiryIndex` key is ordered by timestamp, enabling efficient prefix iteration up to the current block time. The membership check (step 4) adds one keeper call per non-anonymous expiring item. The conviction check (step 5) adds one keeper call per anonymous expiring item. Non-expired content incurs zero overhead.

**EndBlock — Subsidy Draw:**

If `CommonsKeeper` is wired and `anon_subsidy_budget_per_epoch` is non-zero:

1. Compute `current_epoch = block_time / epoch_duration`, where `epoch_duration` is queried via `SeasonKeeper.GetEpochDuration(ctx)` if wired, or falls back to the module constant `DefaultEpochDuration = 13_140_000` seconds (152.08 days, ~5 months)
2. If `current_epoch > last_drawn_epoch` (stored as `AnonSubsidyLastEpoch`):
   - Call `CommonsKeeper.SpendFromTreasury(ctx, "commons", moduleAddress, anon_subsidy_budget_per_epoch)`
   - If treasury has insufficient funds, the transfer is partial or skipped — no error
   - Update `AnonSubsidyLastEpoch = current_epoch`
   - Emit `blog.subsidy.drawn` event
3. Otherwise: no-op

If `CommonsKeeper` is nil or `anon_subsidy_budget_per_epoch` is zero/nil: no-op.

**EndBlock — Nullifier Pruning:**

Epoch-scoped nullifiers (domain 1) from past epochs serve no purpose — a new epoch produces different nullifiers for the same member. To prevent unbounded state growth, the EndBlocker prunes stale epoch-scoped nullifiers using the shared `x/common` helper:

1. Compute `current_epoch = block_time / epoch_duration` (same calculation as subsidy draw)
2. Call `commonkeeper.PruneEpochNullifiers(ctx, store, domain=1, currentEpoch)` — iterates prefix `AnonNullifier/1/`, deletes entries where `scope < currentEpoch - 1`, returns count of pruned entries

This retains nullifiers from the current and previous epoch (grace period for in-flight transactions) and deletes everything older. The pruning cost is proportional to the number of stale entries — after the first prune at each epoch boundary, subsequent blocks in the same epoch find nothing to delete.

**Post-scoped nullifiers (domain 2)** are not pruned by the EndBlocker. They remain valid as long as the referenced post exists and accepts anonymous replies. State growth is bounded by the number of unique anonymous reply actions (one per member per post). If nullifier accumulation becomes a concern, a governance-initiated migration can prune nullifiers referencing tombstoned posts.

**Reaction nullifiers (domains 8–9)** are scope-keyed to `post_id` / `reply_id` and follow the same retention policy as domain 2 — not pruned by the EndBlocker.

---

## 15. Dependency Injection

The module is wired via `depinject` with the following inputs:

| Input | Required | Purpose |
|-------|----------|---------|
| `Config` (*Module) | yes | Proto module config (optional custom authority) |
| `StoreService` | yes | KV store access |
| `Cdc` (codec.Codec) | yes | Protobuf serialization |
| `AddressCodec` | yes | Bech32 address encoding |
| `AuthKeeper` | yes | Address codec, account lookups (simulation) |
| `BankKeeper` | yes | Storage fee collection and burning |
| `RepKeeper` | **yes** | Membership and trust-level checks for replies and reactions; author bond creation on post/reply; content conviction staking queries |
| `VoteKeeper` | **no** | ZK proof verification for anonymous posting (if nil, anonymous posting is unavailable) |
| `CommonsKeeper` | **no** | Council authorization for operational params; treasury draws for subsidy (`SpendFromTreasury`) |
| `SeasonKeeper` | **no** | Epoch duration for nullifier scoping and subsidy draw timing (`GetEpochDuration`); falls back to `DefaultEpochDuration = 13_140_000s` if nil |

If `Config.Authority` is not set, defaults to the `x/gov` module account.

**`RepKeeper` is a required dependency.** The module panics on startup if `RepKeeper` is nil. All membership and trust-level checks depend on it. There is no graceful degradation — this prevents a misconfiguration from silently disabling all access control in production.

---

## 16. Simulation

| Operation | Weight | Description |
|-----------|--------|-------------|
| `SimulateMsgCreatePost` | 100 | Random 20-char title, 200-char body |
| `SimulateMsgUpdatePost` | 100 | Random 25-char title, 250-char body on existing post |
| `SimulateMsgDeletePost` | 100 | Random existing post tombstoning |
| `SimulateMsgHidePost` | 30 | Post author hides random active post |
| `SimulateMsgUnhidePost` | 20 | Post author unhides previously hidden post |
| `SimulateMsgCreateReply` | 80 | Random 100-char reply on existing post |
| `SimulateMsgUpdateReply` | 50 | Random 120-char update on existing reply |
| `SimulateMsgDeleteReply` | 50 | Random existing reply tombstoning |
| `SimulateMsgHideReply` | 30 | Post author hides random reply |
| `SimulateMsgUnhideReply` | 20 | Post author unhides previously hidden reply |
| `SimulateMsgReact` | 80 | Random reaction on existing post/reply |
| `SimulateMsgRemoveReaction` | 30 | Remove existing reaction |
| `SimulateMsgCreateAnonymousPost` | 20 | Anonymous post with mock ZK proof (simulation mode only) |
| `SimulateMsgCreateAnonymousReply` | 20 | Anonymous reply with mock ZK proof (simulation mode only) |
| `SimulateMsgAnonymousReact` | 20 | Anonymous reaction with mock ZK proof (simulation mode only) |
| `SimulateMsgPinPost` | 15 | ESTABLISHED+ member pins random unpinned ephemeral post |
| `SimulateMsgPinReply` | 15 | ESTABLISHED+ member pins random unpinned ephemeral reply |

Each operation has a unique configuration key (`op_weight_msg_blog_<operation>`) so weights can be individually tuned via simulation app params.

The EndBlocker subsidy draw is exercised automatically during simulation when `CommonsKeeper` is wired and `anon_subsidy_budget_per_epoch` is non-zero. No separate simulation operation is needed — the draw triggers at epoch boundaries during the simulated block progression.

---

## 17. Events

Each state-changing message handler emits an event for off-chain indexers and frontends.

| Event Type | Attributes | Emitted By |
|------------|------------|------------|
| `blog.post.created` | `post_id`, `creator`, `expires_at` | CreatePost |
| `blog.post.updated` | `post_id`, `creator` | UpdatePost |
| `blog.post.deleted` | `post_id`, `creator` | DeletePost |
| `blog.post.hidden` | `post_id`, `hidden_by` | HidePost |
| `blog.post.unhidden` | `post_id`, `creator` | UnhidePost |
| `blog.reply.created` | `reply_id`, `post_id`, `creator`, `parent_reply_id`, `expires_at` | CreateReply |
| `blog.reply.updated` | `reply_id`, `creator` | UpdateReply |
| `blog.reply.deleted` | `reply_id`, `post_id`, `creator` | DeleteReply |
| `blog.reply.hidden` | `reply_id`, `post_id`, `hidden_by` | HideReply |
| `blog.reply.unhidden` | `reply_id`, `post_id`, `creator` | UnhideReply |
| `blog.reaction.added` | `creator`, `post_id`, `reply_id`, `reaction_type` | React (new) |
| `blog.reaction.changed` | `creator`, `post_id`, `reply_id`, `old_type`, `new_type` | React (change) |
| `blog.reaction.removed` | `creator`, `post_id`, `reply_id`, `reaction_type` | RemoveReaction |
| `blog.anonymous_post.created` | `post_id`, `proven_trust_level`, `nullifier_hex`, `expires_at` | CreateAnonymousPost |
| `blog.anonymous_reply.created` | `reply_id`, `post_id`, `proven_trust_level`, `nullifier_hex`, `expires_at` | CreateAnonymousReply |
| `blog.anonymous_reaction.added` | `post_id`, `reply_id`, `reaction_type`, `proven_trust_level` | AnonymousReact |
| `blog.post.pinned` | `post_id`, `pinned_by` | PinPost |
| `blog.reply.pinned` | `reply_id`, `post_id`, `pinned_by` | PinReply |
| `blog.post.upgraded` | `post_id`, `creator` | EndBlock membership auto-upgrade (non-member → member) |
| `blog.reply.upgraded` | `reply_id`, `post_id`, `creator` | EndBlock membership auto-upgrade (non-member → member) |
| `blog.post.conviction_sustained` | `post_id`, `conviction_score`, `new_expires_at` | EndBlock: first entry into conviction-sustained state |
| `blog.reply.conviction_sustained` | `reply_id`, `post_id`, `conviction_score`, `new_expires_at` | EndBlock: first entry into conviction-sustained state |
| `blog.post.renewed` | `post_id`, `conviction_score`, `new_expires_at` | EndBlock: subsequent conviction renewal |
| `blog.reply.renewed` | `reply_id`, `post_id`, `conviction_score`, `new_expires_at` | EndBlock: subsequent conviction renewal |
| `blog.post.expired` | `post_id` | EndBlock TTL expiry |
| `blog.reply.expired` | `reply_id`, `post_id` | EndBlock TTL expiry |
| `blog.params.updated` | `authority` | UpdateParams |
| `blog.subsidy.drawn` | `epoch`, `amount`, `source_treasury` | EndBlock subsidy draw |
| `blog.nullifiers.pruned` | `domain`, `pruned_before_scope`, `count` | EndBlock nullifier pruning (only emitted when count > 0) |
| `blog.operational_params.updated` | `authority` | UpdateOperationalParams |

---

## 18. Invariants

The module registers the following invariants for detection via `crisis` module:

### 18.1. ReactionCounts Consistency

For every target (post_id, reply_id), the stored `ReactionCounts` must equal the counts derived by iterating all individual `Reaction` records for that target. If drift is detected, the invariant fails.

This invariant can be checked on-demand via `sparkdreamd query crisis invariants` or triggered automatically if the crisis module is configured.

**Recovery:** If counts drift due to a bug, they can be reconciled via a chain upgrade migration that recomputes `ReactionCounts` from individual `Reaction` records.

### 18.2. ReplyCount Consistency

For every post, `post.reply_count` must equal the count of replies with `status = REPLY_STATUS_ACTIVE` that reference that `post_id`. Tombstoned and hidden replies are excluded from this count. Additionally, the invariant checks for uint64 wrap-around: if `post.reply_count > total_replies_for_post` (including all statuses), a decrement bug has caused underflow.

### 18.3. Counter Consistency

`PostCount` must be greater than the ID of every stored post (`post.id < PostCount` for all posts). `ReplyCount` must be greater than the ID of every stored reply (`reply.id < ReplyCount` for all replies). This ensures the auto-increment counters have never been corrupted or rolled back.

### 18.4. Expiry Index Consistency

Every entry in the `ExpiryIndex` must reference a post or reply that (a) exists and (b) has `expires_at > 0` matching the index key. Conversely, every active post/reply with `expires_at > 0` must have a corresponding `ExpiryIndex` entry. Pinned content (`pinned_by != ""`) must have `expires_at == 0` and no expiry index entry.

### 18.5. High-Water Mark Consistency

For every non-tombstoned post, `fee_bytes_high_water >= len(title) + len(body)` (the high-water mark must be at least as large as the current content). For every non-tombstoned reply, `fee_bytes_high_water >= len(body)`. Tombstoned content is skipped — content is cleared to empty on tombstone, making the check trivially true and uninformative. This ensures the high-water mark was never corrupted to allow fee-free expansion on active content.

---

## 19. Security Considerations

### 19.1. Content Storage

- All content is stored on-chain (no IPFS or external storage by default)
- The `content_type` field supports hints for off-chain storage (e.g., `CONTENT_TYPE_IPFS`)
- `max_body_length` and `max_reply_length` limit the stored byte size — for compressed types (e.g., `CONTENT_TYPE_GZIP`), this governs the compressed payload, not the decompressed content; for off-chain references (e.g., `CONTENT_TYPE_IPFS`), this governs the CID string length
- `max_reply_depth` caps nesting to prevent deep tree traversals
- `content_type` is a client hint only — the module does **not** validate that the body matches the declared type (e.g., a post claiming `CONTENT_TYPE_GZIP` may contain plain text). Interpretation and validation of content encoding is entirely the client's responsibility

### 19.2. Access Control

- Creator address is immutable after post creation
- No transfer of ownership mechanism
- Tombstoned posts and replies retain structural metadata; content is irrecoverably cleared
- Hidden posts retain all content in state; only restorable via `UnhidePost`
- Reply tombstoning preserves thread structure — child replies remain attached to the tombstone node, avoiding orphaned subtrees
- `RepKeeper` is a required dependency — the module panics on startup if not wired, preventing silent access control bypass in production

### 19.3. Spam Prevention

- Per-byte storage fees burned on creation/expansion discourage state bloat; high-water mark tracking prevents the shrink-then-expand exploit (where a user shrinks content to 1 byte then re-expands for near-zero cost)
- Flat reaction fee burned on new reactions discourages mass reaction spam
- Per-address daily rate limits cap posts, replies, and reactions independently
- Gas costs for transactions provide additional spam deterrence
- Fees and rate limits can be adjusted by the Operations Committee without governance delay
- Per-post trust-level gating lets authors protect their comment sections from low-trust accounts
- Default membership requirement (trust level 0) prevents anonymous Sybil spam
- **Rate limit day-boundary note:** Rate limits reset at UTC midnight (`day = block_time / 86400`). A user can theoretically double their throughput across a 2-minute window straddling midnight. This is an accepted trade-off for the simplicity of day-based counters — storage fees and gas costs remain effective economic deterrents in this edge case

### 19.4. Reaction Gaming

- One reaction per user per target prevents vote stuffing
- Membership requirement raises the cost of Sybil attacks (each fake reactor needs a real invitation)
- Reaction fee adds economic cost beyond gas, making mass reaction manipulation expensive
- Daily reaction rate limit caps the number of new reactions per address
- Aggregate counts are maintained atomically — no race conditions between individual records and counts
- Module invariants detect count drift; chain upgrade migration can reconcile if needed

### 19.5. Author Moderation

- Post authors can hide/unhide their own posts (self-moderation) and moderate replies on their own posts
- Only the post author can delete, hide, or unhide their posts — no Operations Committee override at the blog level (content moderation for the broader platform belongs in x/forum)
- Hide is reversible (unhide); tombstone (delete) is permanent
- Hidden posts are excluded from `ListPost` results by default; `ShowPost` returns them with `POST_STATUS_HIDDEN` status, and `ListPostsByCreator` includes them when `include_hidden = true` — title/body are preserved in state for unhide
- Hidden replies are excluded from list query results by default; `ShowReply` returns them with `REPLY_STATUS_HIDDEN` status, and `ListReplies` includes them when `include_hidden = true` — the body is preserved in state for unhide
- Authors cannot hide/delete reactions — only the reactor can remove their own reaction

### 19.6. Ephemeral Content Lifecycle

Two categories of content are **ephemeral** (subject to TTL expiry):

1. **Anonymous posts/replies** (creator = module account) — no author identity for moderation
2. **Non-member posts/replies** — creator is a valid address but not an active x/rep member

Both carry a TTL (`params.ephemeral_content_ttl`, default 7 days) set at creation time and are automatically tombstoned by the EndBlocker when the TTL elapses. Member posts and replies are always permanent (`expires_at = 0`).

This design upholds **free speech** — anyone can post to x/blog, and anonymous members can contribute without revealing identity — while ensuring the **community is not obligated to preserve bad content**. Unendorsed ephemeral content naturally expires; valuable content is preserved by community action.

An ESTABLISHED+ member can **pin** ephemeral content via `MsgPinPost` / `MsgPinReply`, which clears the TTL and makes the content permanent. Pinning is a one-way operation — once pinned, content cannot be unpinned (though the post author's standard hide/delete capabilities still apply for identified parent posts with ephemeral replies).

If `ephemeral_content_ttl` is set to 0, all content is permanent regardless of authorship (pinning becomes a no-op).

**Membership is checked at creation time and again at expiry.** If a member loses their membership after creating a post, the post remains permanent — the TTL was set to 0 at creation and is not retroactively applied. Conversely, if a non-member creates a post (gets TTL) and then joins x/rep before the TTL expires, the EndBlocker automatically upgrades the post to permanent at expiry time — no additional fee is charged because the creator already paid full storage fees at creation. This prevents the frustrating case of a new member's onboarding-era posts expiring shortly after they join.

### 19.7. Censorship Resistance Guardrails

The Operations Committee can adjust fees, rate limits, TTL, and anonymous posting parameters without governance. This operational agility is valuable for responding to spam waves, but creates a censorship surface: a hostile committee could combine parameter changes to suppress speech classes without deleting individual posts.

Five governance-only parameters bound the committee's power:

| Guardrail | Protects Against | Mechanism |
|-----------|------------------|-----------|
| `anonymous_posting_enabled` (default: true) | Silencing whistleblowers — disabling anonymous posting removes the primary mechanism for members to critique leadership without retaliation | Not in `BlogOperationalParams`; only changeable via `MsgUpdateParams` (x/gov). The committee retains `anonymous_min_trust_level` for proportional abuse response |
| `min_ephemeral_content_ttl` (default: 1 day) | TTL suppression — setting TTL near-zero to make ephemeral content expire before anyone can read or pin it | `ephemeral_content_ttl` must be 0 (disabled) or ≥ `min_ephemeral_content_ttl` |
| `max_cost_per_byte` (default: 1,000uspark) | Economic censorship — setting storage fees prohibitively high to price out posting | `cost_per_byte.amount` ≤ `max_cost_per_byte.amount` |
| `max_reaction_fee` (default: 500uspark) | Economic censorship — setting reaction fees prohibitively high to suppress engagement | `reaction_fee.amount` ≤ `max_reaction_fee.amount` |
| `pin_min_trust_level` (governance-only) | Pin suppression — raising the trust level required to preserve ephemeral content, concentrating preservation power in a small group aligned with the committee | Not in `BlogOperationalParams`; only changeable via `MsgUpdateParams` (x/gov) |

**What the committee can still do:**
- Raise `anonymous_min_trust_level` to shrink the anonymity set (e.g., to TRUSTED or CORE during abuse)
- Set `max_posts_per_day = 1` to throttle output
- Remove subsidy relay addresses (anonymous posters pay their own gas)
- Adjust storage fees and reaction fees (within governance ceilings)

**What the committee cannot do:**
- Disable anonymous posting (`anonymous_posting_enabled` is governance-only)
- Set `ephemeral_content_ttl` below the governance floor (unless they set it to 0, which makes all content permanent — the opposite of censorship)
- Set `cost_per_byte` or `reaction_fee` above the governance ceiling
- Change who can pin ephemeral content (`pin_min_trust_level` is governance-only)
- Delete, hide, or modify any post or reply

The remaining committee levers (`anonymous_min_trust_level`, rate limits, subsidy controls) are intentionally left as operational parameters. Raising the trust level is a proportional response to abuse scenarios (e.g., sustained anonymous harassment) — it restricts anonymous posting to more trusted members rather than eliminating it entirely. The Three Pillars hierarchy (Section 19.2 of the architecture spec) provides accountability: the Commons Council can override committee actions, and governance can override the council. The x/futarchy confidence vote mechanism provides ongoing accountability pressure on all councils.

### 19.8. Anonymous Post Mempool Visibility

Anonymous post and reply transactions are submitted to the mempool in cleartext. The ZK proof protects the **author's identity** but not the **content**: a validator or mempool observer can read the title, body, and content_type before the transaction is confirmed. This creates two risks:

1. **Content front-running:** An observer could extract the content and publish it non-anonymously (under their own name) in a transaction with higher gas, landing it on-chain first. The anonymous post would still land (different nullifier), but the observer claims "first post."
2. **Targeted censorship:** A validator could selectively exclude anonymous transactions from their blocks based on content inspection.

**Mitigations (outside this module's scope):**
- **Private relay submission:** Relays can submit transactions directly to a validator via private channels (e.g., gRPC), bypassing the public mempool
- **Encrypted mempools:** Protocol-level transaction encryption (e.g., threshold encryption) prevents content inspection before block inclusion — a future Cosmos SDK or CometBFT feature
- **Content hashing:** The anonymous poster could publish a content hash first, then reveal the content in a follow-up transaction (commit-reveal). This adds latency and complexity but prevents front-running

These are inherent limitations of transparent blockchains, not specific to x/blog. The ZK proof's core guarantee — that the poster's identity is hidden from everyone, including validators — remains intact regardless of mempool visibility.

---

## 20. Client Integration: Session Keys

For fluid user interactions without repeated wallet popups, frontends should implement the **session key pattern** using `x/authz` and `x/feegrant`. This allows users to approve a single grant, after which all blog actions (posting, replying, reacting) are auto-signed by an ephemeral session key.

See **[docs/session-keys.md](session-keys.md)** for the full specification, grant scoping recommendations, fee delegation setup, and security considerations.

---

## 21. Anonymous Posting via ZK Proofs

Members can create blog posts and replies without revealing their identity, using the ZK-SNARK infrastructure from x/vote. The poster proves they are an active member meeting a minimum trust level — without revealing *which* member they are. A nullifier system prevents spam: one anonymous reply per post per identity, one anonymous top-level post per epoch per identity.

See **[docs/anonymous-posting.md](anonymous-posting.md)** for the full specification covering the member trust tree, anonymous action circuit, nullifier system, relay pattern, VoteKeeper interface, client workflow, and security considerations.

### 21.1. Blog-Specific Nullifier Domains

| Domain | Action | Scope | Effect |
|--------|--------|-------|--------|
| `1` | Anonymous post | Current epoch | One anonymous post per member per epoch |
| `2` | Anonymous reply | `post_id` | One anonymous reply per member per post |
| `8` | Anonymous post reaction | `post_id` | One anonymous reaction per member per post |
| `9` | Anonymous reply reaction | `reply_id` | One anonymous reaction per member per reply |

### 21.2. Messages

#### MsgCreateAnonymousPost

```protobuf
message MsgCreateAnonymousPost {
  string submitter = 1;                                // Transaction signer (pays gas + storage fee; identity NOT linked to content)
  string title = 2;                                    // Post title
  string body = 3;                                     // Post content
  sparkdream.common.v1.ContentType content_type = 4;   // Content encoding hint
  bytes proof = 5;                                     // ~500-byte Groth16 proof
  bytes nullifier = 6;                                 // 32-byte nullifier
  bytes merkle_root = 7;                               // Trust tree root used for proof
  uint32 min_trust_level = 8;                          // Trust level proven (must meet anonymous_min_trust_level param)
}

message MsgCreateAnonymousPostResponse {
  uint64 id = 1;  // Assigned post ID
}
```

**Validation:**
1. `submitter` is valid bech32 (pays gas; not recorded as author)
2. Title non-empty, `len(title)` ≤ `params.max_title_length`
3. Body non-empty, `len(body)` ≤ `params.max_body_length`
4. `VoteKeeper` must not be nil (`ErrAnonymousPostingUnavailable` otherwise)
5. `params.anonymous_posting_enabled` must be true (`ErrAnonymousPostingDisabled` otherwise)
6. `min_trust_level` ≥ `params.anonymous_min_trust_level` (`ErrInsufficientAnonTrustLevel` otherwise)
7. `merkle_root` matches the current or previous `MemberTrustTreeRoot` from x/rep (`ErrStaleMerkleRoot` otherwise). Accepting the previous root provides a one-rebuild-cycle grace period so that proofs generated between tree rebuilds remain valid
8. Nullifier not already used (`ErrNullifierUsed` otherwise)
9. `scope` = current epoch, where `epoch = block_time / epoch_duration` and `epoch_duration` is queried via `SeasonKeeper.GetEpochDuration(ctx)` (falls back to `DefaultEpochDuration = 13_140_000` seconds / ~5 months)
10. ZK proof verification via `VoteKeeper.VerifyAnonymousActionProof(proof, merkle_root, nullifier, min_trust_level, domain=1, scope=epoch)` (`ErrInvalidZKProof` otherwise)
11. `submitter` must not exceed `params.max_posts_per_day` (shared counter with regular posts)

**Logic:**
1. Validate content inputs (title, body, content_type)
2. Call `commonkeeper.VerifyAndStoreAnonymousAction(ctx, store, voteKeeper, repKeeper, anonParams, proof, merkleRoot, nullifier, minTrustLevel, domain=1, scope=epoch, blockTime)` — handles steps 4–10 of validation above (param checks, root verification, nullifier dedup, ZK proof verification, nullifier recording)
3. Check submitter rate limit (`params.max_posts_per_day` — shared with regular posts)
4. Compute storage fee: `cost_per_byte * (len(title) + len(body))`
5. If `submitter` is in `params.anon_subsidy_relay_addresses` and module subsidy balance ≥ min(fee, `anon_subsidy_max_per_post`): deduct min(fee, `anon_subsidy_max_per_post`) from module account and burn; charge any excess to submitter. Otherwise: charge full fee to submitter and burn
6. Create Post with `creator = module_account_address`, `replies_enabled = true`, `min_reply_trust_level = 0` (hardcoded to "any active member" — not `-1` (open) because anonymous posts lack author-moderation, so allowing non-member replies would create unmoderable content on unmoderable posts; not configurable by the anonymous author because they cannot later moderate replies, so restrictive settings would create unmoderable locked-down threads), `fee_bytes_high_water = len(title) + len(body)`, `expires_at = block_time + params.ephemeral_content_ttl` (0 if `ephemeral_content_ttl == 0`)
7. If `expires_at > 0`: add to `ExpiryIndex`
8. Store `AnonymousPostMetadata` linked to post ID
9. Record nullifier as used
10. Emit `blog.anonymous_post.created` event (includes `post_id`, `proven_trust_level`, `expires_at`; does NOT include submitter)

The `submitter` pays gas and storage fees but is **not** recorded as the post author. The `creator` field on the stored Post is set to the blog module account address. See the relay pattern in [anonymous-posting.md](anonymous-posting.md) for details.

#### MsgCreateAnonymousReply

```protobuf
message MsgCreateAnonymousReply {
  string submitter = 1;                                // Transaction signer (pays gas; not the author)
  uint64 post_id = 2;                                 // Target blog post
  string body = 3;                                     // Reply content
  sparkdream.common.v1.ContentType content_type = 4;   // Content encoding hint
  bytes proof = 5;                                     // ~500-byte Groth16 proof
  bytes nullifier = 6;                                 // 32-byte nullifier
  bytes merkle_root = 7;                               // Trust tree root used for proof
  uint32 min_trust_level = 8;                          // Trust level proven
}

message MsgCreateAnonymousReplyResponse {
  uint64 id = 1;  // Assigned reply ID
}
```

**Validation:**
1. `submitter` is valid bech32
2. Post must exist and have `status = POST_STATUS_ACTIVE` (`ErrPostDeleted` if tombstoned, `ErrPostHidden` if hidden)
3. `post.replies_enabled` must be true (`ErrRepliesDisabled` otherwise)
4. Body non-empty, `len(body)` ≤ `params.max_reply_length`
5. `VoteKeeper` must not be nil (`ErrAnonymousPostingUnavailable` otherwise)
6. `params.anonymous_posting_enabled` must be true (`ErrAnonymousPostingDisabled` otherwise)
7. `min_trust_level` ≥ max(`params.anonymous_min_trust_level`, `post.min_reply_trust_level`) (`ErrInsufficientAnonTrustLevel` otherwise)
8. `merkle_root` matches the current or previous `MemberTrustTreeRoot` (`ErrStaleMerkleRoot` otherwise) — same one-rebuild-cycle grace period as anonymous posts
9. Nullifier not already used (`ErrNullifierUsed` otherwise)
10. `scope` = `post_id` (verified against nullifier)
11. ZK proof verification via `VoteKeeper.VerifyAnonymousActionProof(proof, merkle_root, nullifier, min_trust_level, domain=2, scope=post_id)` (`ErrInvalidZKProof` otherwise)
12. `submitter` must not exceed `params.max_replies_per_day` (shared counter with regular replies)

**Logic:**
1. Validate content inputs (post exists, replies enabled, body length)
2. Call `commonkeeper.VerifyAndStoreAnonymousAction(ctx, store, voteKeeper, repKeeper, anonParams, proof, merkleRoot, nullifier, minTrustLevel, domain=2, scope=postId, blockTime)` — handles steps 5–11 of validation above
3. Check submitter rate limit (`params.max_replies_per_day`)
4. Compute storage fee: `cost_per_byte * len(body)`
5. If `submitter` is in `params.anon_subsidy_relay_addresses` and module subsidy balance ≥ min(fee, `anon_subsidy_max_per_post`): deduct min(fee, `anon_subsidy_max_per_post`) from module account and burn; charge any excess to submitter. Otherwise: charge full fee to submitter and burn
6. Create Reply with `creator = module_account_address`, `depth = 0` (anonymous replies are always top-level), `parent_reply_id = 0`, `fee_bytes_high_water = len(body)`, `expires_at = block_time + params.ephemeral_content_ttl` (0 if `ephemeral_content_ttl == 0`)
7. If `expires_at > 0`: add to `ExpiryIndex`
8. Store `AnonymousPostMetadata` linked to reply ID
9. Record nullifier, increment `post.reply_count`
10. Emit `blog.anonymous_reply.created` event

**Anonymous replies are always top-level.** Nesting is not supported because threading creates correlation patterns (if only one member replied to a specific sub-thread, they're identifiable). Flat anonymous replies maximize the anonymity set.

### 21.3. State Objects and Storage

Anonymous posting state objects are defined in Section 3: `AnonymousPostMetadata` (3.12), `AnonNullifierEntry` (3.11). Storage keys are listed in the main storage schema (Section 4).

### 21.4. Parameters

Anonymous posting parameters are included in the canonical `Params` (Section 3.8, fields 12-13, 17-19) and `BlogOperationalParams` (Section 3.9, fields 8, 12). Governance-only guardrails affecting anonymous content are in `Params` fields 12 (`anonymous_posting_enabled`), 18 (`pin_min_trust_level`), and 20 (`min_ephemeral_content_ttl`). Defaults and validation rules are in Section 8.

### 21.5. Access Control

| Operation | Who Can Execute |
|-----------|-----------------|
| CreateAnonymousPost | Any address as submitter; ZK proof must prove membership at `anonymous_min_trust_level` |
| CreateAnonymousReply | Any address as submitter; ZK proof must prove membership at max(anonymous_min_trust_level, post.min_reply_trust_level) |
| Update anonymous post/reply | **Nobody** — anonymous content is immutable |
| Delete/hide anonymous post | **Nobody** directly — creator is module account (cannot sign). Content is naturally tombstoned by TTL expiry (see below) |
| Pin anonymous post | Active member at `pin_min_trust_level`+ (clears TTL, makes permanent) |
| Pin anonymous reply | Active member at `pin_min_trust_level`+ (clears TTL, makes permanent) |
| Hide anonymous reply | Post author (same as regular replies — only available when post has an identified author) |
| Delete anonymous reply | Post author only (reply author path is module account, so only post author path applies) |

Anonymous posts and replies cannot be updated, deleted, or hidden by their author (there is no known author). Like all ephemeral content (see Section 19.6), anonymous posts carry a TTL (`params.ephemeral_content_ttl`, default 7 days). At TTL expiry, the EndBlocker checks the content's community conviction score (via x/rep's content staking system). If conviction ≥ `params.conviction_renewal_threshold`, the TTL is extended by `params.conviction_renewal_period` instead of tombstoning — the content survives as long as the community actively supports it. If conviction is below the threshold, the content is tombstoned normally. Additionally, an ESTABLISHED+ member can **pin** the content at any time via `MsgPinPost`/`MsgPinReply`, clearing the TTL entirely and making it permanent. See [anonymous-posting.md](anonymous-posting.md) § "Conviction-Based Lifetime Extension" for the full cross-module pattern.

Anonymous posts hardcode `replies_enabled = true` and `min_reply_trust_level = 0`, so the membership requirement (active members only), rate limits, and TTL provide the primary spam and abuse protection.

**Moderation on anonymous posts:** Since the post author is the module account (which cannot sign transactions), the "post author" moderation path is unavailable for replies on anonymous posts. This means:
- **Regular replies on anonymous posts** can only be self-deleted by the reply author — post-author hide/delete is inaccessible
- **Anonymous replies on anonymous posts** cannot be hidden/deleted by anyone — they expire via TTL unless pinned
- **Non-member replies on anonymous posts** (when `min_reply_trust_level = -1` on a non-anonymous post that allows open replies) also expire via TTL unless pinned

For use cases needing stronger moderation of anonymous or non-member content, x/forum provides platform-level tools (sentinel bonds, appeals, council oversight).

### 21.6. Anonymous Posting Subsidy

The Commons Council can subsidize anonymous posting costs to encourage democratic dialogue. The subsidy is funded from the Commons Council group treasury (which receives its allocation via x/split) and covers **storage fees** for approved relay addresses. Gas subsidization is handled separately via `x/feegrant` grants to approved relays (outside this module's scope).

See **[docs/anonymous-posting.md](anonymous-posting.md)** § "Anonymous Posting Subsidy" for the full specification covering the funding flow, governance surface, and comparison with x/feegrant.

Subsidy parameters are included in the canonical `Params` (Section 3.8, fields 14-16) and `BlogOperationalParams` (Section 3.9, fields 10-12). Defaults and validation rules are in Section 8. The EndBlocker draw mechanism is described in Section 14.

**Mechanism summary:**
1. Each epoch, the EndBlocker draws `anon_subsidy_budget_per_epoch` from Commons Council treasury via `CommonsKeeper.SpendFromTreasury()` (Section 14)
2. When an approved relay (listed in `anon_subsidy_relay_addresses`) submits an anonymous post/reply, storage fees are paid from the subsidy account instead of the relay (Section 21.2, Logic steps 4-5)
3. Per-post cost capped at `anon_subsidy_max_per_post`; excess charged to relay
4. When budget is exhausted, relays pay normally until next epoch
5. Unspent budget rolls over (accumulates in the blog module account)
6. If `CommonsKeeper` is nil or `anon_subsidy_budget_per_epoch` is zero, subsidy is disabled — relays always pay their own costs

**Within-block ordering:** When multiple anonymous posts from approved relays land in the same block, subsidy is applied first-come-first-served in transaction execution order. If the subsidy balance is exhausted mid-block, later transactions in that block charge the relay directly. Relay operators should account for this when estimating costs.

### 21.7. Anonymous Reactions

Members can react to posts and replies without revealing their identity, using the same ZK-SNARK infrastructure as anonymous posting. A nullifier scoped to the target enforces one reaction per member per target — the same constraint as public reactions.

#### Blog-Specific Nullifier Domains

| Domain | Action | Scope | Effect |
|--------|--------|-------|--------|
| `8` | Anonymous post reaction | `post_id` | One anonymous reaction per member per post |
| `9` | Anonymous reply reaction | `reply_id` | One anonymous reaction per member per reply |

#### MsgAnonymousReact

```protobuf
message MsgAnonymousReact {
  string submitter = 1;                                // Transaction signer (pays gas + fees; NOT the reactor)
  uint64 post_id = 2;                                 // Target post
  uint64 reply_id = 3;                                // Target reply (0 = reacting to the post)
  ReactionType reaction_type = 4;                      // Selected reaction (LIKE, INSIGHTFUL, DISAGREE, FUNNY)
  bytes proof = 5;                                     // ~500-byte Groth16 proof
  bytes nullifier = 6;                                 // 32-byte nullifier
  bytes merkle_root = 7;                               // Trust tree root used for proof
  uint32 min_trust_level = 8;                          // Trust level proven
}

message MsgAnonymousReactResponse {}
```

**Validation:**
1. `VoteKeeper` must not be nil (`ErrAnonymousPostingUnavailable`)
2. `params.anonymous_posting_enabled` must be true (`ErrAnonymousPostingDisabled`)
3. Post must exist and have `status = POST_STATUS_ACTIVE`
4. If `reply_id > 0`: reply must exist, belong to the post, and have `status = REPLY_STATUS_ACTIVE`
5. `reaction_type` must not be `UNSPECIFIED`
6. `min_trust_level` >= `params.anonymous_min_trust_level`
7. `merkle_root` matches current or previous `MemberTrustTreeRoot` from x/rep
8. Nullifier domain and scope: if `reply_id == 0`, domain=8, scope=`post_id`; if `reply_id > 0`, domain=9, scope=`reply_id`
9. Nullifier not already used (`ErrNullifierUsed`)
10. ZK proof verified via `VoteKeeper.VerifyAnonymousActionProof(proof, merkle_root, nullifier, min_trust_level, domain, scope)`
11. `submitter` must not exceed `params.max_reactions_per_day` (shared counter with regular reactions)

**Fee logic:**
- `params.reaction_fee` charged to `submitter` and burned (same as regular new reaction)
- Anonymous posting subsidy applies if `submitter` is an approved relay

**Logic:**
1. Validate content inputs (post/reply exists, reaction_type valid)
2. Determine domain and scope (domain=8 + scope=post_id, or domain=9 + scope=reply_id)
3. Call `commonkeeper.VerifyAndStoreAnonymousAction(ctx, store, voteKeeper, repKeeper, anonParams, proof, merkleRoot, nullifier, minTrustLevel, domain, scope, blockTime)` — handles steps 1–2, 6–10 of validation above
4. Check submitter rate limit
5. Charge and burn reaction fee from submitter
6. Increment the appropriate count in `ReactionCounts` for the target (like_count, insightful_count, disagree_count, or funny_count)
7. Emit `blog.anonymous_reaction.added` event (includes `post_id`, `reply_id`, `reaction_type`, `proven_trust_level`; does NOT include `submitter`)

**Parity with public reactions:**

| Property | Public | Anonymous |
|----------|--------|-----------|
| One reaction per user per target | `Reaction` keyed storage | ZK nullifier (domain=8/9, scope=target_id) |
| Reaction type | Any of 4 types | Same 4 types |
| Changeable | Yes (replace with different type) | No — nullifier is permanent |
| Removable | Yes (`MsgRemoveReaction`) | No — nullifier is permanent |
| Rate limit | `max_reactions_per_day` | Same (shared budget via submitter) |
| Fee | `reaction_fee` | Same (charged to submitter) |
| Reactor identity | Stored in `Reaction` record | Hidden by ZK proof |

**Note:** Anonymous reactions are strictly additive — they increment counters but cannot be changed or removed. This is simpler than public reactions (which support type changes and removal) because there is no author identity to verify for subsequent operations.

---

## 22. Future Considerations

1. **Content Hashing**: Store content hash for verification
2. **Categories/Tags**: Add taxonomy support
3. **Version History**: Track edit history for posts and replies
4. **Integration with x/name**: Display author names instead of addresses
5. **Reaction-Weighted Ranking**: Use reaction counts to surface popular posts/replies
6. **Reply Notifications**: Notify post authors when new replies are posted (off-chain indexer)
7. **Reaction Trust Gating**: Per-post minimum trust level for reactions (currently reactions require any active member)
8. **Custom Reaction Sets**: Governance-configurable reaction types beyond the default four
9. ~~**Reply Pinning**: Post author can pin important replies to the top~~ — **Implemented** (see Section 5.15, 5.16)
10. **External Moderation & Author Bond Slashing**: x/blog currently uses author-only self-moderation. If a sentinel or council-driven moderation system is added, `SlashAuthorBond` can be wired into the hide flow (as x/forum and x/collect already do)

---

## 23. File References

- Proto definitions: `proto/sparkdream/blog/v1/*.proto`
- Keeper logic: `x/blog/keeper/`
- Types and errors: `x/blog/types/`
- Module setup: `x/blog/module/`
- Session key pattern: `docs/session-keys.md`
- Anonymous posting: `docs/anonymous-posting.md`
