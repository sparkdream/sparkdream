# x/season Module Specification

## Overview

The `x/season` module manages:
- Seasonal time periods (~5 months each)
- Reputation archival and reset
- Gamification: XP, levels, achievements, titles
- Guilds (social units)
- Quests (directed engagement)
- Leaderboards
- Retroactive public goods funding (seasonal nomination window + conviction-weighted DREAM rewards)

## Design Principles

### Separation of Reward Types

The system uses three distinct reward mechanisms with **no overlap**:

| Metric | Measures | Earned From | Used For |
|--------|----------|-------------|----------|
| **DREAM** | Economic output | Initiative completion, staking, compensation | Governance power, staking, transfers |
| **Reputation** | Skill/quality per domain | Initiative completion, jury accuracy | Initiative eligibility, jury selection |
| **XP** | Engagement/participation | Governance, forum engagement, mentorship outcomes | Levels, achievements, titles |

**Critical:** XP is NOT earned from activities that grant DREAM. This prevents double-counting and clarifies incentive signals.

### XP from Outcomes, Not Actions

XP rewards **engagement received** and **observable outcomes**, not self-reported actions:

| Gameable (avoid) | Harder to game (prefer) |
|------------------|------------------------|
| "I logged in" | - |
| "I posted" | "Others replied to my post" |
| "I helped someone" | "Someone I helped reached Established" |

### Social Focus

The system emphasizes cooperation over competition:
- Guilds as social units, not competitive teams
- Leaderboards as celebration, not ranking
- Recognition through achievements, not zero-sum contests

### Cosmetics via NFTs

The chain tracks accomplishments (levels, achievements, titles). Visual cosmetics:
- **Frames/badges** are handled by the UI layer based on on-chain level/achievements
- No cosmetic inventory tracked in this module

## Module Dependencies

The `x/season` module depends on:

| Module | Usage |
|--------|-------|
| `x/rep` | Member validation, DREAM burning for guild creation, DREAM minting for retroactive rewards, challenge/jury hooks |
| `x/name` | Display name and guild name uniqueness (see below) |
| `x/gov` / `x/commons` | Vote/proposal hooks for XP |
| `x/forum` | Forum calls `seasonKeeper.GrantXP()` directly for forum-sourced XP; content validation for nominations |
| `x/blog` | Content validation for nominations (post existence, status, creator) |
| `x/collect` | Content validation for nominations (collection existence, status, owner) |
| `x/commons` | Authority checks for governance-gated messages |

### x/name Integration

The `x/name` module (see [x-name-spec.md](x-name-spec.md)) provides name registration and uniqueness enforcement. `x/season` uses it for:

1. **Usernames**: Reserved via `ReserveName(ctx, name, NameTypeUsername, owner)` - unique @handles
2. **Guild names**: Reserved via `ReserveName(ctx, name, NameTypeGuild, founder)` - unique guild identifiers

**Note:** Display names are NOT reserved via x/name - they are non-unique cosmetic names.

**Owned Names:** If a member already owns a name in x/name (e.g., registered their identity), they can use it as their username without additional reservation or cost via `IsNameOwner()` check.

Both reserved names are released when no longer needed (username change, guild dissolution, member zeroed).

### x/forum Integration

The `x/season` module implements the `ForumHooks` interface from `x/forum` (see [x-forum-spec.md section 7.5](x-forum-spec.md)). When forum events occur (replies, helpful marks), x/forum calls these hooks, and x/season handles XP granting with anti-gaming logic.

**Anti-Gaming Logic (handled by x/season):**
- **Account age check:** Actor must be registered for `forum_xp_min_account_age_epochs` (default 7) to grant XP
- **Reciprocal cooldown:** Same actor cannot grant XP to same beneficiary more than once per `forum_xp_reciprocal_cooldown_epochs` (default 1)
- **Per-epoch cap:** Total forum XP per epoch capped at `max_forum_xp_per_epoch` (default 50)
- **Self-reply prevention:** Cannot earn XP from replies to your own posts

```go
// x/season implements ForumHooks from x/forum
type SeasonForumHooks struct {
    keeper Keeper
}

func (h SeasonForumHooks) OnReplyReceived(ctx sdk.Context, postAuthor, replier string, postID uint64) {
    h.keeper.handleForumReplyReceived(ctx, postAuthor, replier, postID)
}

func (h SeasonForumHooks) OnPostMarkedHelpful(ctx sdk.Context, postAuthor, marker string, postID uint64, isReply bool) {
    h.keeper.handleForumPostMarkedHelpful(ctx, postAuthor, marker, postID, isReply)
}

// ... other ForumHooks methods
```

### Authority Requirements

The module validates authority for admin messages via `x/commons`:

| Authority | Messages | Check Method |
|-----------|----------|--------------|
| Commons Operations Committee | Quest CRUD | `commonsKeeper.IsCommitteeMember(ctx, sender, "commons", "operations")` |
| Commons Council | Season extension, naming | `commonsKeeper.IsCouncilMember(ctx, sender, "commons")` or via commons proposal |

## State

### Season

```protobuf
message Season {
  uint32 number = 1;
  string name = 2;
  string theme = 3;
  int64 start_block = 4;
  int64 end_block = 5;
  SeasonStatus status = 6;

  // Extension tracking
  uint32 extensions_count = 7;
  uint64 total_extension_epochs = 8;
  int64 original_end_block = 9;

  // Note: Season stats (total_initiatives_completed, total_members_joined, etc.)
  // are NOT stored on-chain. Indexers compute these from events:
  // - EventMemberProfileCreated -> total_members_joined
  // - EventInitiativeCompleted (from x/rep) -> total_initiatives_completed
}

enum SeasonStatus {
  SEASON_STATUS_ACTIVE = 0;
  SEASON_STATUS_NOMINATION = 1;      // Nomination window open (last nomination_window_epochs of season)
  SEASON_STATUS_ENDING = 2;          // Transition in progress
  SEASON_STATUS_MAINTENANCE = 3;     // Blocking user actions during critical transition phases
  SEASON_STATUS_COMPLETED = 4;
}
```

### SeasonSnapshot

Snapshots are indexed in the KVStore, not stored as a single object.

```protobuf
message SeasonSnapshot {
  uint32 season = 1;
  int64 snapshot_block = 2;
  // Note: total_participants is computed by indexers from MemberSeasonSnapshot count
}

// StoreKey: 0x03 | SeasonID | MemberAddress -> MemberSeasonSnapshot
message MemberSeasonSnapshot {
  string address = 1;
  string final_dream_balance = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  map<string, string> final_reputation = 3;
  uint32 initiatives_completed = 4;
  uint64 xp_earned = 5;
  uint32 season_level = 6;
  repeated string achievements_earned = 7;
}
```

### MemberProfile

```protobuf
message MemberProfile {
  string address = 1;

  // Identity
  string display_name = 2;      // Non-unique, cosmetic (e.g., "Crypto Enthusiast 🚀")
  string username = 17;         // Unique, reserved via x/name (e.g., "alice")

  string display_title = 3;
  // Active titles available for display selection (limited to max_displayable_titles)
  // When limit exceeded, oldest seasonal titles are archived automatically
  repeated string unlocked_titles = 4;
  // Archived seasonal titles (still earned, just not in quick-select)
  // Oldest archived titles are pruned when max_archived_titles is exceeded
  repeated string archived_titles = 19;
  repeated string achievements = 5;
  uint64 season_xp = 6;
  uint32 season_level = 7;
  uint64 lifetime_xp = 8;
  uint64 guild_id = 9;

  // Change tracking
  int64 last_display_name_change_epoch = 10;
  int64 last_username_change_epoch = 18;

  // Achievement progress tracking (lifetime counts)
  uint64 challenges_won = 11;
  uint64 jury_duties_completed = 12;
  uint64 votes_cast = 13;
  uint64 forum_helpful_count = 14;
  uint64 invitations_successful = 15;

  // Activity tracking (for streaks and pruning)
  int64 last_active_epoch = 16;
}
```

### Achievement

```protobuf
message Achievement {
  string id = 1;
  string name = 2;
  string description = 3;
  Rarity rarity = 4;
  uint32 xp_reward = 5;
  AchievementRequirement requirement = 6;
}

message AchievementRequirement {
  RequirementType type = 1;
  uint64 threshold = 2;
  repeated string tags = 3;
}

enum RequirementType {
  REQUIREMENT_TYPE_INITIATIVES_COMPLETED = 0;
  REQUIREMENT_TYPE_REPUTATION_EARNED = 1;
  REQUIREMENT_TYPE_INVITATIONS_SUCCESSFUL = 2;
  REQUIREMENT_TYPE_CHALLENGES_WON = 3;
  REQUIREMENT_TYPE_JURY_DUTY = 4;
  REQUIREMENT_TYPE_SEASONS_ACTIVE = 5;
  REQUIREMENT_TYPE_VOTES_CAST = 6;
  REQUIREMENT_TYPE_FORUM_HELPFUL = 7;
}

enum Rarity {
  RARITY_COMMON = 0;
  RARITY_UNCOMMON = 1;
  RARITY_RARE = 2;
  RARITY_EPIC = 3;
  RARITY_LEGENDARY = 4;
  RARITY_UNIQUE = 5;
}
```

### Title

```protobuf
message Title {
  string id = 1;              // e.g., "champion", "sage", "veteran"
  string name = 2;            // Display name (may be prefixed with season for seasonal titles)
  string description = 3;
  Rarity rarity = 4;
  TitleRequirement requirement = 5;
  bool seasonal = 6;          // If true, title is granted per-season with "S{N} " prefix
}

// Seasonal Title Behavior:
// - Seasonal titles are earned each season and prefixed with season number
// - Example: "Champion" title in Season 3 becomes "S3 Champion"
// - Once earned, seasonal titles are PERMANENT (not revoked at season end)
// - Members can display any earned seasonal title (e.g., "S1 Champion" in Season 5)
// - The title_id stored in unlocked_titles includes the season: "s3_champion"

message TitleRequirement {
  RequirementType type = 1;
  uint64 threshold = 2;
  uint32 season = 3;  // 0 for non-seasonal titles
}
```

### Guild

```protobuf
message Guild {
  uint64 id = 1;
  string name = 2;
  string description = 3;
  string founder = 4;
  repeated string officers = 5;
  int64 created_block = 6;
  bool invite_only = 7;
  repeated string pending_invites = 8;
  GuildStatus status = 9;

  // Note: Guild XP (season_xp) is NOT stored on-chain.
  // Indexers compute it by summing member XP from EventXPGranted events
  // where the member was in the guild at the time of the grant.
}

enum GuildStatus {
  GUILD_STATUS_ACTIVE = 0;
  GUILD_STATUS_FROZEN = 1;   // No founder, any member can claim founder via MsgClaimGuildFounder
  GUILD_STATUS_DISSOLVED = 2;
}

message GuildMembership {
  string member = 1;
  uint64 guild_id = 2;
  int64 joined_epoch = 3;
  int64 left_epoch = 4;
  uint32 guilds_joined_this_season = 5;
}

message GuildInvite {
  uint64 guild_id = 1;
  string invitee = 2;
  string inviter = 3;
  int64 created_epoch = 4;
  int64 expires_epoch = 5;  // 0 = never expires (if guild_invite_ttl_epochs param is 0)
}
```

### Quest

**Design: Governance-Updatable Quests**

Quests can be updated after creation via `MsgUpdateQuest` (governance-gated). This allows the Operations Committee to adjust quest parameters without needing to deactivate and recreate. Members with in-progress quests on a deactivated quest can still complete them. Quests can also be deactivated via `MsgDeactivateQuest`.

```protobuf
message Quest {
  string id = 1;
  string name = 2;
  string description = 3;
  repeated QuestObjective objectives = 4;
  uint64 xp_reward = 5;
  bool repeatable = 6;
  uint32 cooldown_epochs = 7;
  uint32 season = 8;            // 0 = permanent
  int64 start_block = 9;
  int64 end_block = 10;
  bool active = 11;

  // Prerequisites
  uint32 min_level = 12;
  string required_achievement = 13;  // Empty = no requirement
  string prerequisite_quest = 14;    // Must complete this quest first (empty = no requirement)
  string chain_id = 15;              // Groups related quests for UI display (empty = standalone)
}

message QuestObjective {
  string description = 1;
  QuestObjectiveType type = 2;
  uint64 target = 3;
}

enum QuestObjectiveType {
  QUEST_OBJECTIVE_VOTES_CAST = 0;
  QUEST_OBJECTIVE_FORUM_HELPFUL = 1;
  QUEST_OBJECTIVE_INVITEE_MILESTONE = 2;
  QUEST_OBJECTIVE_INITIATIVES_COMPLETED = 3;
}

message MemberQuestProgress {
  string member = 1;
  string quest_id = 2;
  repeated uint64 objective_progress = 3;
  bool completed = 4;
  int64 completed_block = 5;
  int64 last_attempt_block = 6;
}
```

### Leaderboards (Off-Chain)

Leaderboards are **NOT stored on-chain**. They are computed by indexers from:
- `MemberProfile.season_xp` → XP leaderboard
- `EventInitiativeCompleted` events → Initiatives leaderboard
- `EventXPGranted` events with guild membership → Guild XP leaderboard

This saves significant state storage and avoids expensive on-chain sorting.

### XP Tracking

```protobuf
message EpochXPTracker {
  string member = 1;
  int64 epoch = 2;
  uint64 vote_xp_earned = 3;      // XP from voting/proposals
  uint64 forum_xp_earned = 4;     // XP from forum engagement
  uint64 quest_xp_earned = 5;     // XP from quest completion
  uint64 other_xp_earned = 6;     // XP from achievements, mentorship outcomes, etc.
  // Total = vote + forum + quest + other, capped at max_xp_per_epoch
}

message VoteXPRecord {
  uint32 season = 1;
  string member = 2;
  uint64 proposal_id = 3;
  int64 granted_block = 4;
}

// Forum XP anti-gaming: tracks when member A last received XP from member B
// Prevents XP farming rings (reciprocal XP boosting)
message ForumXPCooldown {
  string beneficiary = 1;        // Member receiving XP
  string actor = 2;              // Member granting XP (via reply/helpful mark)
  int64 last_granted_epoch = 3;  // Last epoch when actor granted XP to beneficiary
}

// Forum XP: tracks member's registration epoch for account age requirements
// Stored once when member is created via OnMemberCreated hook
message MemberRegistration {
  string member = 1;
  int64 registered_epoch = 2;
}
```

### Retroactive Public Goods Nominations

At the end of each season, a nomination window opens where members can nominate completed contributions — blog posts, forum answers, curated collections, initiative work, jury service — for retroactive DREAM rewards. The community stakes conviction on nominations. Top-conviction nominations receive bonus DREAM minted from the protocol.

This rewards the unexpected contributions that were never pre-planned as initiatives: the member who wrote an incredibly helpful forum answer, the collection curator who surfaced a hidden gem, the blog post that clarified a confusing governance process. These contributions fall through the cracks of the initiative system but are exactly the public goods that make the community valuable.

**Why this chain unlocks it:** The seasonal cycle creates a natural "retrospective" moment. Content conviction already provides the quality signal. The conviction math already exists. This adds a seasonal ceremony that channels conviction into retroactive rewards.

```protobuf
message Nomination {
  uint64 id = 1;
  string nominator = 2;                    // Address of the nominator
  string content_ref = 3;                  // e.g. "blog/post/42", "forum/post/7", "collect/collection/3", "rep/initiative/5", "rep/jury/addr"
  string rationale = 4;
  int64 created_at_block = 5;
  uint64 season = 6;

  // Conviction tracking (uses same formula as content conviction staking)
  string total_staked = 7 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", (gogoproto.nullable) = false];
  string conviction = 8 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", (gogoproto.nullable) = false];

  // Reward (set during PhaseRetroRewards)
  string reward_amount = 9 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", (gogoproto.nullable) = false];  // DREAM minted (0 if not rewarded)
  bool rewarded = 10;
}

// NominationStake represents a DREAM stake on a nomination.
message NominationStake {
  uint64 nomination_id = 1;
  string staker = 2;
  string amount = 3 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", (gogoproto.nullable) = false];
  int64 staked_at_block = 4;
}

// RetroRewardRecord tracks the history of retroactive rewards distributed per season.
message RetroRewardRecord {
  uint64 season = 1;
  uint64 nomination_id = 2;
  string recipient = 3;                    // nominator address
  string content_ref = 4;
  string conviction = 5 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", (gogoproto.nullable) = false];
  string reward_amount = 6 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", (gogoproto.nullable) = false];
  int64 distributed_at_block = 7;
}
```

**Content eligibility:** Nominated content must have been created during the current season and must be in an active (non-hidden, non-deleted) state at nomination time. The content creator must be an active member. Self-nomination is allowed (someone might have written a great answer that nobody else thought to nominate), but self-staking on own nominations is not allowed (same anti-gaming rule as content conviction).

**Nomination limit:** Each member may submit up to `max_nominations_per_member` nominations per season (default 5). This prevents nomination spam while allowing members to champion multiple contributions they found valuable.

**Conviction staking on nominations:** During the nomination window, members stake DREAM on nominations using `MsgStakeNomination`. This uses the same conviction formula as content conviction staking (`conviction(t) = stake_amount * (1 - 2^(-t / half_life))`), with the `nomination_conviction_half_life_epochs` parameter. Since the nomination window is shorter than a full season, a shorter half-life (default 3 epochs) ensures conviction can build meaningfully within the window. Stakes are returned after the season transition completes.

**Reward distribution:** During the season transition, the top nominations by conviction score (up to `retro_reward_max_recipients`) receive freshly minted DREAM. The total mint budget is `retro_reward_budget_per_season`. Distribution is conviction-weighted: each rewarded nomination receives a share proportional to its conviction score relative to the total conviction across all rewarded nominations. A minimum conviction threshold (`retro_reward_min_conviction`) prevents low-effort nominations from receiving rewards.

**Duplicate prevention:** Only one active nomination per content reference per season. If content has already been nominated, additional nominators can stake conviction on the existing nomination rather than creating a duplicate.

## Params

```protobuf
message Params {
  // Epoch configuration (immutable after genesis in practice)
  int64 epoch_blocks = 1;             // Blocks per epoch (default 17280 = ~1 day at 5s blocks)

  // Season timing
  int64 season_duration_epochs = 2;
  int64 season_transition_epochs = 3;

  // XP rewards
  uint64 xp_vote_cast = 4;
  uint64 xp_proposal_created = 5;
  uint64 xp_forum_reply_received = 6;
  uint64 xp_forum_marked_helpful = 7;
  uint64 xp_invitee_first_initiative = 8;
  uint64 xp_invitee_established = 9;

  // XP caps
  uint32 max_vote_xp_per_epoch = 10;
  uint64 max_forum_xp_per_epoch = 11;
  uint64 max_xp_per_epoch = 12;

  // Levels
  repeated uint64 level_thresholds = 13;

  // Reputation reset
  string baseline_reputation = 14 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];

  // Guilds
  uint32 min_guild_members = 15;
  uint32 max_guild_members = 16;
  uint32 max_guild_officers = 17;                // Max officers per guild (default 5)
  string guild_creation_cost = 18 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  uint64 guild_hop_cooldown_epochs = 19;
  uint32 max_guilds_per_season = 20;
  uint64 min_guild_age_epochs = 21;
  uint32 max_pending_invites = 22;

  // Display names (non-unique, cosmetic)
  uint32 display_name_min_length = 23;   // Default 1
  uint32 display_name_max_length = 24;   // Default 50
  uint64 display_name_change_cooldown_epochs = 25;  // Default 1

  // Usernames (unique, reserved via x/name)
  uint32 username_min_length = 42;       // Default 3
  uint32 username_max_length = 43;       // Default 20
  uint64 username_change_cooldown_epochs = 44;  // Default 30
  string username_cost_dream = 45 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // DREAM cost (default 10)

  // Display name moderation
  string display_name_report_stake_dream = 48 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // Stake for reports (default 50, burned if frivolous)
  string display_name_appeal_stake_dream = 53 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // Stake for appeals (default 100, burned if appeal fails)
  uint64 display_name_appeal_period_blocks = 54;  // Blocks after moderation during which appeal is allowed (default 100800 = ~7 days)

  // Title management
  uint32 max_displayable_titles = 49;  // Max titles in unlocked_titles before archiving (default 50)
  uint32 max_archived_titles = 55;     // Max titles in archived_titles before oldest are pruned (default 200)

  // Season transitions
  uint64 max_transition_epochs = 26;
  uint32 transition_batch_size = 27;

  // Season extensions
  uint32 max_season_extensions = 28;
  uint64 max_extension_epochs = 29;

  // Guild content limits
  uint32 guild_description_max_length = 30;  // Default 500 chars
  uint64 guild_invite_ttl_epochs = 31;       // Default 30 epochs, 0 = never expire

  // Guild invite cleanup (BeginBlocker)
  uint32 invite_cleanup_interval_blocks = 50;  // Run cleanup every N blocks (default 100)
  uint32 invite_cleanup_batch_size = 51;       // Max invites to check per cleanup (default 50)

  // Quests
  uint32 max_quest_objectives = 32;
  uint64 max_quest_xp_reward = 41;  // Max XP a single quest can reward (default 100)
  uint32 max_active_quests_per_member = 47;  // Max concurrent in-progress quests (default 10)
  uint32 max_objective_description_length = 52;  // Max chars for objective description (default 200)

  // Historical data management and cleanup
  uint32 snapshot_retention_seasons = 33;    // How many past seasons to keep snapshots (default 10, 0 = keep all)
  uint32 epoch_tracker_retention_epochs = 34;  // How many epochs of XP trackers to keep (default 30)
  uint32 vote_xp_record_retention_seasons = 35;  // How many seasons of vote XP records to keep (default 2)
  uint32 forum_cooldown_retention_epochs = 36;  // How many epochs of forum cooldowns to keep (default 30)

  // Forum XP anti-gaming (x/season implements ForumHooks from x/forum)
  uint64 forum_xp_min_account_age_epochs = 37;  // Min epochs since registration to grant XP (default 7)
  uint64 forum_xp_reciprocal_cooldown_epochs = 38;  // Cooldown between same actor->beneficiary XP grants (default 1)
  uint64 forum_xp_self_reply_cooldown_epochs = 39;  // Cooldown before replying to own thread grants XP to others (default 3)

  // Transition recovery
  uint32 transition_grace_period = 40;   // Blocks to add when aborting (default: 50400 = ~1 week)
  uint32 transition_max_retries = 46;    // Max automatic retries before entering recovery mode (default: 3)

  // Retroactive Public Goods Funding
  uint64 nomination_window_epochs = 56;                  // Epochs before season end when nominations open (default: 14 = ~2 weeks)
  uint32 max_nominations_per_member = 57;                // Max nominations per member per season (default: 5)
  uint32 retro_reward_max_recipients = 58;               // Max nominations that receive rewards (default: 20)
  string retro_reward_budget_per_season = 59 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // Total DREAM minted for retro rewards (default: 50,000 DREAM = 50_000_000_000 micro-DREAM)
  string retro_reward_min_conviction = 60 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];  // Min conviction to qualify for reward (default: 50.0)
  uint64 nomination_conviction_half_life_epochs = 61;    // Half-life for nomination conviction (default: 3 epochs, shorter than content conviction)
  uint32 nomination_rationale_max_length = 62;           // Max chars for nomination rationale (default: 500)
  uint32 nomination_min_trust_level = 63;                // Min trust level to nominate (default: 1 = PROVISIONAL)
  uint32 nomination_stake_min_trust_level = 64;          // Min trust level to stake on nominations (default: 0 = NEW)
  string nomination_min_stake = 65 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // Min DREAM to stake on a nomination (default: 10 DREAM = 10_000_000 micro-DREAM)
}
```

## Timing Units: Epochs

All duration parameters use **epochs** (not blocks). An epoch is a fixed number of blocks defined at genesis and stored in params for flexibility (e.g., shorter epochs on testnets).

```go
// EpochBlocks is stored in params, not as a constant
// Default: 17280 blocks (~1 day assuming 5-second blocks)
// Testnet: 720 blocks (~1 hour for faster testing)

func (k Keeper) GetCurrentEpoch(ctx sdk.Context) int64 {
    params := k.GetParams(ctx)
    return ctx.BlockHeight() / params.EpochBlocks
}

func (k Keeper) BlockToEpoch(ctx sdk.Context, block int64) int64 {
    params := k.GetParams(ctx)
    return block / params.EpochBlocks
}

func (k Keeper) EpochToBlock(ctx sdk.Context, epoch int64) int64 {
    params := k.GetParams(ctx)
    return epoch * params.EpochBlocks
}
```

## Messages

### Profile Management

```protobuf
// Display name: non-unique, cosmetic
message MsgSetDisplayName {
  string creator = 1;
  string name = 2;    // e.g., "Crypto Enthusiast", "John Smith"
}

message MsgSetDisplayNameResponse {}

// Username: unique, reserved via x/name
message MsgSetUsername {
  string creator = 1;
  string username = 2;  // e.g., "alice", "crypto_whale"
}

message MsgSetUsernameResponse {}

message MsgSetDisplayTitle {
  string creator = 1;
  string title_id = 2;
}

message MsgSetDisplayTitleResponse {}
```

### Guild Management

All guild messages use `creator` as the signer field name, following standard Cosmos SDK conventions.

```protobuf
message MsgCreateGuild {
  string creator = 1;
  string name = 2;
  string description = 3;
  bool invite_only = 4;
}

message MsgCreateGuildResponse {}

message MsgJoinGuild {
  string creator = 1;
  uint64 guild_id = 2;
}

message MsgJoinGuildResponse {}

message MsgLeaveGuild {
  string creator = 1;
}

message MsgLeaveGuildResponse {}

message MsgTransferGuildFounder {
  string creator = 1;
  uint64 guild_id = 2;
  string new_founder = 3;
}

message MsgTransferGuildFounderResponse {}

message MsgDissolveGuild {
  string creator = 1;
  uint64 guild_id = 2;
}

message MsgDissolveGuildResponse {}

// Officer management (founder only)
message MsgPromoteToOfficer {
  string creator = 1;
  uint64 guild_id = 2;
  string member = 3;
}

message MsgPromoteToOfficerResponse {}

message MsgDemoteOfficer {
  string creator = 1;
  uint64 guild_id = 2;
  string officer = 3;
}

message MsgDemoteOfficerResponse {}

// Invitation management (founder or officers)
message MsgInviteToGuild {
  string creator = 1;
  uint64 guild_id = 2;
  string invitee = 3;
}

message MsgInviteToGuildResponse {}

message MsgAcceptGuildInvite {
  string creator = 1;
  uint64 guild_id = 2;
}

message MsgAcceptGuildInviteResponse {}

message MsgRevokeGuildInvite {
  string creator = 1;
  uint64 guild_id = 2;
  string invitee = 3;
}

message MsgRevokeGuildInviteResponse {}

message MsgSetGuildInviteOnly {
  string creator = 1;
  uint64 guild_id = 2;
  bool invite_only = 3;
}

message MsgSetGuildInviteOnlyResponse {}

message MsgUpdateGuildDescription {
  string creator = 1;
  uint64 guild_id = 2;
  string description = 3;
}

message MsgUpdateGuildDescriptionResponse {}

// Kick a member from guild (founder or officers)
message MsgKickFromGuild {
  string creator = 1;         // Founder or officer
  uint64 guild_id = 2;
  string member = 3;
  string reason = 4;          // Required reason for transparency
}

message MsgKickFromGuildResponse {}

// Claim founder status of a frozen guild (any member can claim, first-come-first-serve)
message MsgClaimGuildFounder {
  string creator = 1;         // Must be a member of the frozen guild
  uint64 guild_id = 2;
}

message MsgClaimGuildFounderResponse {}
```

### Quest Management

```protobuf
message MsgStartQuest {
  string creator = 1;
  string quest_id = 2;
}

message MsgStartQuestResponse {}

message MsgClaimQuestReward {
  string creator = 1;
  string quest_id = 2;
}

message MsgClaimQuestRewardResponse {}

// Abandon an in-progress quest (resets progress, applies cooldown for repeatable quests)
message MsgAbandonQuest {
  string creator = 1;
  string quest_id = 2;
}

message MsgAbandonQuestResponse {}
```

### Admin (Governance-Gated)

Authority is split between the Commons Operations Committee (routine tasks) and Commons Council (significant decisions):

| Message | Authority | Rationale |
|---------|-----------|-----------|
| `MsgCreateQuest` | Operations Committee | Routine content management |
| `MsgUpdateQuest` | Operations Committee | Routine content management |
| `MsgDeactivateQuest` | Operations Committee | Routine content management |
| `MsgCreateAchievement` | Operations Committee | Achievement management |
| `MsgUpdateAchievement` | Operations Committee | Achievement management |
| `MsgDeleteAchievement` | Operations Committee | Achievement management |
| `MsgCreateTitle` | Operations Committee | Title management |
| `MsgUpdateTitle` | Operations Committee | Title management |
| `MsgDeleteTitle` | Operations Committee | Title management |
| `MsgExtendSeason` | Commons Council | Significant scheduling decision |
| `MsgSetNextSeasonInfo` | Commons Council | Community-wide visibility |
| `MsgAbortSeasonTransition` | Commons Council | Emergency transition recovery |
| `MsgRetrySeasonTransition` | Commons Council | Emergency transition recovery |
| `MsgSkipTransitionPhase` | Commons Council | Emergency transition recovery |
| `MsgResolveDisplayNameAppeal` | Commons Council / Operations Committee | Moderation resolution |
| `MsgResolveUnappealedModeration` | Commons Council / Operations Committee | Moderation resolution |

**Note:** Frozen guilds are handled by `MsgClaimGuildFounder` (any member can claim), not governance. This avoids delays and governance overhead for a common guild lifecycle event.

```protobuf
// Quest management (Operations Committee)
message MsgCreateQuest {
  string authority = 1;  // Commons Operations Committee
  string quest_id = 2;
  string name = 3;
  string description = 4;
  uint64 xp_reward = 5;
  bool repeatable = 6;
  uint64 cooldown_epochs = 7;
  uint64 season = 8;
  int64 start_block = 9;
  int64 end_block = 10;
  uint64 min_level = 11;
  string required_achievement = 12;
  string prerequisite_quest = 13;
  string quest_chain = 14;
}

message MsgCreateQuestResponse {}

message MsgUpdateQuest {
  string authority = 1;  // Commons Operations Committee
  string quest_id = 2;
  string name = 3;
  string description = 4;
  uint64 xp_reward = 5;
  bool repeatable = 6;
  uint64 cooldown_epochs = 7;
  uint64 season = 8;
  int64 start_block = 9;
  int64 end_block = 10;
  uint64 min_level = 11;
  string required_achievement = 12;
  string prerequisite_quest = 13;
  string quest_chain = 14;
  bool active = 15;
}

message MsgUpdateQuestResponse {}

message MsgDeactivateQuest {
  string authority = 1;  // Commons Operations Committee
  string quest_id = 2;
}

message MsgDeactivateQuestResponse {}

// Season management (Commons Council)
message MsgExtendSeason {
  string authority = 1;  // Commons Council
  uint64 extension_epochs = 2;
  string reason = 3;
}

message MsgExtendSeasonResponse {}

// Season naming (submitted before season ends, takes effect at next season start)
message MsgSetNextSeasonInfo {
  string authority = 1;  // Commons Council
  string name = 2;
  string theme = 3;
}

message MsgSetNextSeasonInfoResponse {}

// Achievement management (Operations Committee)
message MsgCreateAchievement {
  string authority = 1;
  string achievement_id = 2;
  string name = 3;
  string description = 4;
  uint32 rarity = 5;
  uint64 xp_reward = 6;
  uint32 requirement_type = 7;
  uint64 requirement_threshold = 8;
}

message MsgCreateAchievementResponse {}

message MsgUpdateAchievement {
  string authority = 1;
  string achievement_id = 2;
  string name = 3;
  string description = 4;
  uint32 rarity = 5;
  uint64 xp_reward = 6;
  uint32 requirement_type = 7;
  uint64 requirement_threshold = 8;
}

message MsgUpdateAchievementResponse {}

message MsgDeleteAchievement {
  string authority = 1;
  string achievement_id = 2;
}

message MsgDeleteAchievementResponse {}

// Title management (Operations Committee)
message MsgCreateTitle {
  string authority = 1;
  string title_id = 2;
  string name = 3;
  string description = 4;
  uint32 rarity = 5;
  uint32 requirement_type = 6;
  uint64 requirement_threshold = 7;
  uint64 requirement_season = 8;
  bool seasonal = 9;
}

message MsgCreateTitleResponse {}

message MsgUpdateTitle {
  string authority = 1;
  string title_id = 2;
  string name = 3;
  string description = 4;
  uint32 rarity = 5;
  uint32 requirement_type = 6;
  uint64 requirement_threshold = 7;
  uint64 requirement_season = 8;
  bool seasonal = 9;
}

message MsgUpdateTitleResponse {}

message MsgDeleteTitle {
  string authority = 1;
  string title_id = 2;
}

message MsgDeleteTitleResponse {}
```

### Retroactive Public Goods Nominations

```protobuf
// Nominate a contribution for retroactive DREAM rewards
// Only available during SEASON_STATUS_NOMINATION window
message MsgNominate {
  string creator = 1;                     // Must be active member at nomination_min_trust_level+
  string content_ref = 2;                 // Content reference ("blog/post/42", "forum/post/7", etc.)
  string rationale = 3;                   // Why this deserves retroactive reward (max nomination_rationale_max_length)
}

message MsgNominateResponse {
  uint64 nomination_id = 1;
}

// Stake DREAM on a nomination to signal conviction
// Only available during SEASON_STATUS_NOMINATION window
message MsgStakeNomination {
  string creator = 1;                     // Must be active member at nomination_stake_min_trust_level+
  uint64 nomination_id = 2;
  string amount = 3;                      // DREAM amount as decimal string. Min: nomination_min_stake
}

message MsgStakeNominationResponse {}

// Unstake DREAM from a nomination (conviction is recalculated)
// Available during nomination window; stakes auto-returned after season transition
message MsgUnstakeNomination {
  string creator = 1;
  uint64 nomination_id = 2;
}

message MsgUnstakeNominationResponse {}
```

**Validation rules:**
- `MsgNominate`: Season must be in `NOMINATION` status. Nominator must be active member with trust level ≥ `nomination_min_trust_level`. Content ref must point to content created during the current season. Content must be active (not hidden/deleted). Content creator must be an active member. No duplicate nomination for the same content ref in the same season. Nominator must not exceed `max_nominations_per_member`
- `MsgStakeNomination`: Season must be in `NOMINATION` status. Staker must be active member with trust level ≥ `nomination_stake_min_trust_level`. Amount ≥ `nomination_min_stake`. Staker cannot stake on nominations for their own content (same anti-gaming rule as content conviction). Staker must have sufficient DREAM balance
- `MsgUnstakeNomination`: Season must be in `NOMINATION` status. Staker must have an active stake on the nomination

## Queries

The module exposes 68 queries. These are organized into CRUD-style queries (Get/List for each state type) and higher-level application queries.

```protobuf
service Query {
  // Params
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);

  // Season state (CRUD)
  rpc GetSeason(QueryGetSeasonRequest) returns (QueryGetSeasonResponse);
  rpc GetSeasonTransitionState(QueryGetSeasonTransitionStateRequest) returns (QueryGetSeasonTransitionStateResponse);
  rpc GetTransitionRecoveryState(QueryGetTransitionRecoveryStateRequest) returns (QueryGetTransitionRecoveryStateResponse);
  rpc GetNextSeasonInfo(QueryGetNextSeasonInfoRequest) returns (QueryGetNextSeasonInfoResponse);

  // Season snapshots (CRUD)
  rpc GetSeasonSnapshot(QueryGetSeasonSnapshotRequest) returns (QueryGetSeasonSnapshotResponse);
  rpc ListSeasonSnapshot(QueryAllSeasonSnapshotRequest) returns (QueryAllSeasonSnapshotResponse);
  rpc GetMemberSeasonSnapshot(QueryGetMemberSeasonSnapshotRequest) returns (QueryGetMemberSeasonSnapshotResponse);
  rpc ListMemberSeasonSnapshot(QueryAllMemberSeasonSnapshotRequest) returns (QueryAllMemberSeasonSnapshotResponse);

  // Member profiles (CRUD)
  rpc GetMemberProfile(QueryGetMemberProfileRequest) returns (QueryGetMemberProfileResponse);
  rpc ListMemberProfile(QueryAllMemberProfileRequest) returns (QueryAllMemberProfileResponse);
  rpc GetMemberRegistration(QueryGetMemberRegistrationRequest) returns (QueryGetMemberRegistrationResponse);
  rpc ListMemberRegistration(QueryAllMemberRegistrationRequest) returns (QueryAllMemberRegistrationResponse);

  // Achievements (CRUD)
  rpc GetAchievement(QueryGetAchievementRequest) returns (QueryGetAchievementResponse);
  rpc ListAchievement(QueryAllAchievementRequest) returns (QueryAllAchievementResponse);

  // Titles (CRUD)
  rpc GetTitle(QueryGetTitleRequest) returns (QueryGetTitleResponse);
  rpc ListTitle(QueryAllTitleRequest) returns (QueryAllTitleResponse);
  rpc GetSeasonTitleEligibility(QueryGetSeasonTitleEligibilityRequest) returns (QueryGetSeasonTitleEligibilityResponse);
  rpc ListSeasonTitleEligibility(QueryAllSeasonTitleEligibilityRequest) returns (QueryAllSeasonTitleEligibilityResponse);

  // Guilds (CRUD)
  rpc GetGuild(QueryGetGuildRequest) returns (QueryGetGuildResponse);
  rpc ListGuild(QueryAllGuildRequest) returns (QueryAllGuildResponse);
  rpc GetGuildMembership(QueryGetGuildMembershipRequest) returns (QueryGetGuildMembershipResponse);
  rpc ListGuildMembership(QueryAllGuildMembershipRequest) returns (QueryAllGuildMembershipResponse);
  rpc GetGuildInvite(QueryGetGuildInviteRequest) returns (QueryGetGuildInviteResponse);
  rpc ListGuildInvite(QueryAllGuildInviteRequest) returns (QueryAllGuildInviteResponse);

  // Quests (CRUD)
  rpc GetQuest(QueryGetQuestRequest) returns (QueryGetQuestResponse);
  rpc ListQuest(QueryAllQuestRequest) returns (QueryAllQuestResponse);
  rpc GetMemberQuestProgress(QueryGetMemberQuestProgressRequest) returns (QueryGetMemberQuestProgressResponse);
  rpc ListMemberQuestProgress(QueryAllMemberQuestProgressRequest) returns (QueryAllMemberQuestProgressResponse);

  // XP tracking (CRUD)
  rpc GetEpochXpTracker(QueryGetEpochXpTrackerRequest) returns (QueryGetEpochXpTrackerResponse);
  rpc ListEpochXpTracker(QueryAllEpochXpTrackerRequest) returns (QueryAllEpochXpTrackerResponse);
  rpc GetVoteXpRecord(QueryGetVoteXpRecordRequest) returns (QueryGetVoteXpRecordResponse);
  rpc ListVoteXpRecord(QueryAllVoteXpRecordRequest) returns (QueryAllVoteXpRecordResponse);
  rpc GetForumXpCooldown(QueryGetForumXpCooldownRequest) returns (QueryGetForumXpCooldownResponse);
  rpc ListForumXpCooldown(QueryAllForumXpCooldownRequest) returns (QueryAllForumXpCooldownResponse);

  // Display name moderation (CRUD)
  rpc GetDisplayNameModeration(QueryGetDisplayNameModerationRequest) returns (QueryGetDisplayNameModerationResponse);
  rpc ListDisplayNameModeration(QueryAllDisplayNameModerationRequest) returns (QueryAllDisplayNameModerationResponse);
  rpc GetDisplayNameReportStake(QueryGetDisplayNameReportStakeRequest) returns (QueryGetDisplayNameReportStakeResponse);
  rpc ListDisplayNameReportStake(QueryAllDisplayNameReportStakeRequest) returns (QueryAllDisplayNameReportStakeResponse);
  rpc GetDisplayNameAppealStake(QueryGetDisplayNameAppealStakeRequest) returns (QueryGetDisplayNameAppealStakeResponse);
  rpc ListDisplayNameAppealStake(QueryAllDisplayNameAppealStakeRequest) returns (QueryAllDisplayNameAppealStakeResponse);

  // Retroactive public goods nominations (CRUD)
  rpc GetNomination(QueryGetNominationRequest) returns (QueryGetNominationResponse);
  rpc ListNominations(QueryListNominationsRequest) returns (QueryListNominationsResponse);
  rpc ListNominationsByCreator(QueryListNominationsByCreatorRequest) returns (QueryListNominationsByCreatorResponse);
  rpc ListNominationStakes(QueryListNominationStakesRequest) returns (QueryListNominationStakesResponse);
  rpc ListRetroRewardHistory(QueryListRetroRewardHistoryRequest) returns (QueryListRetroRewardHistoryResponse);

  // ===== Application-level queries =====

  // Season
  rpc CurrentSeason(QueryCurrentSeasonRequest) returns (QueryCurrentSeasonResponse);
  rpc SeasonByNumber(QuerySeasonByNumberRequest) returns (QuerySeasonByNumberResponse);
  rpc SeasonStats(QuerySeasonStatsRequest) returns (QuerySeasonStatsResponse);

  // Member
  rpc MemberByDisplayName(QueryMemberByDisplayNameRequest) returns (QueryMemberByDisplayNameResponse);
  rpc MemberSeasonHistory(QueryMemberSeasonHistoryRequest) returns (QueryMemberSeasonHistoryResponse);
  rpc MemberXpHistory(QueryMemberXpHistoryRequest) returns (QueryMemberXpHistoryResponse);

  // Achievements & Titles
  rpc Achievements(QueryAchievementsRequest) returns (QueryAchievementsResponse);
  rpc MemberAchievements(QueryMemberAchievementsRequest) returns (QueryMemberAchievementsResponse);
  rpc Titles(QueryTitlesRequest) returns (QueryTitlesResponse);
  rpc MemberTitles(QueryMemberTitlesRequest) returns (QueryMemberTitlesResponse);

  // Guilds
  rpc GuildById(QueryGuildByIdRequest) returns (QueryGuildByIdResponse);
  rpc GuildsList(QueryGuildsListRequest) returns (QueryGuildsListResponse);
  rpc GuildsByFounder(QueryGuildsByFounderRequest) returns (QueryGuildsByFounderResponse);
  rpc GuildMembers(QueryGuildMembersRequest) returns (QueryGuildMembersResponse);
  rpc MemberGuild(QueryMemberGuildRequest) returns (QueryMemberGuildResponse);
  rpc GuildInvites(QueryGuildInvitesRequest) returns (QueryGuildInvitesResponse);
  rpc MemberGuildInvites(QueryMemberGuildInvitesRequest) returns (QueryMemberGuildInvitesResponse);

  // Quests
  rpc QuestsList(QueryQuestsListRequest) returns (QueryQuestsListResponse);
  rpc QuestById(QueryQuestByIdRequest) returns (QueryQuestByIdResponse);
  rpc QuestChain(QueryQuestChainRequest) returns (QueryQuestChainResponse);
  rpc MemberQuestStatus(QueryMemberQuestStatusRequest) returns (QueryMemberQuestStatusResponse);
  rpc AvailableQuests(QueryAvailableQuestsRequest) returns (QueryAvailableQuestsResponse);

  // Note: Leaderboard queries are handled by indexers, not on-chain
}

// Season stats
message QuerySeasonStatsRequest {
  uint32 season = 1;  // 0 = current
}

message QuerySeasonStatsResponse {
  uint32 season_number = 1;
  uint64 total_xp_earned = 2;
  uint64 active_members = 3;
  uint64 initiatives_completed = 4;
  uint64 total_reputation_earned = 5;
  uint64 guilds_active = 6;
  uint64 quests_completed = 7;
  int64 blocks_remaining = 8;
}

// Next season info
message QueryNextSeasonInfoRequest {}

message QueryNextSeasonInfoResponse {
  string name = 1;
  string theme = 2;
  bool is_set = 3;
}

// Member guild
message QueryMemberGuildRequest {
  string member = 1;
}

message QueryMemberGuildResponse {
  uint64 guild_id = 1;
  Guild guild = 2;
  GuildMembership membership = 3;
}

// Guild invites (pending invites for a guild)
message QueryGuildInvitesRequest {
  uint64 guild_id = 1;
}

message QueryGuildInvitesResponse {
  repeated string invitees = 1;
}

// Member's pending guild invites
message QueryMemberGuildInvitesRequest {
  string member = 1;
}

message QueryMemberGuildInvitesResponse {
  repeated Guild guilds = 1;
}

// Available quests for a member (filtered by prerequisites)
message QueryAvailableQuestsRequest {
  string member = 1;
}

message QueryAvailableQuestsResponse {
  repeated Quest quests = 1;
}

// Guilds by founder (historical, includes dissolved)
message QueryGuildsByFounderRequest {
  string founder = 1;
  bool include_dissolved = 2;
}

message QueryGuildsByFounderResponse {
  repeated Guild guilds = 1;
}

// Member XP history (for graphing progression)
message QueryMemberXPHistoryRequest {
  string member = 1;
  uint32 season = 2;           // 0 = current season
  uint32 epochs_back = 3;      // How many epochs of history (default 30)
}

message QueryMemberXPHistoryResponse {
  repeated EpochXPEntry entries = 1;
}

message EpochXPEntry {
  int64 epoch = 1;
  uint64 xp_earned = 2;
  uint64 cumulative_xp = 3;
}

// Quest chain (all quests in a chain, ordered by prerequisites)
message QueryQuestChainRequest {
  string chain_id = 1;
}

message QueryQuestChainResponse {
  repeated Quest quests = 1;  // Ordered by prerequisite dependency
}

// Lookup member by display name
message QueryMemberByDisplayNameRequest {
  string display_name = 1;
}

message QueryMemberByDisplayNameResponse {
  MemberProfile profile = 1;
}

// Member's historical season snapshots
message QueryMemberSeasonHistoryRequest {
  string member = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QueryMemberSeasonHistoryResponse {
  repeated MemberSeasonSnapshot snapshots = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}

// Note: Leaderboard queries removed - computed by indexers from:
// - MemberProfile.season_xp (XP leaderboard)
// - EventInitiativeCompleted (initiatives leaderboard)
// - Guild membership + EventXPGranted (guild XP leaderboard)

// Retroactive Public Goods Nominations
message QueryGetNominationRequest {
  uint64 id = 1;
}

message QueryGetNominationResponse {
  Nomination nomination = 1;
}

message QueryListNominationsRequest {
  cosmos.base.query.v1beta1.PageRequest pagination = 1;
}

message QueryListNominationsResponse {
  repeated Nomination nominations = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}

message QueryListNominationsByCreatorRequest {
  string creator = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QueryListNominationsByCreatorResponse {
  repeated Nomination nominations = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}

message QueryListNominationStakesRequest {
  uint64 nomination_id = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QueryListNominationStakesResponse {
  repeated NominationStake stakes = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}

// Historical retro rewards for past seasons
message QueryListRetroRewardHistoryRequest {
  uint64 season = 1;           // Required — which season to query
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QueryListRetroRewardHistoryResponse {
  repeated RetroRewardRecord records = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

## Keeper Interface

```go
type Keeper interface {
    // Season management
    GetCurrentSeason(ctx) Season
    StartNewSeason(ctx, name, theme string) error
    EndSeason(ctx) error
    GetSeason(ctx, number uint32) (Season, error)
    ExtendSeason(ctx, extensionEpochs uint64) error
    SetNextSeasonInfo(ctx, name, theme string) error
    GetNextSeasonInfo(ctx) (name, theme string, isSet bool)
    // Note: GetSeasonStats removed - stats computed by indexers

    // Season transitions
    ProcessSeasonTransitionBatch(ctx, batchSize int) (done bool, err error)
    GetTransitionState(ctx) SeasonTransitionState
    SetTransitionState(ctx, state SeasonTransitionState)
    DeleteTransitionState(ctx)
    InitializeTransitionState(ctx)

    // Transition recovery
    AbortTransition(ctx) error
    RetryTransitionPhase(ctx) error
    SkipTransitionPhase(ctx) error
    HandleTransitionError(ctx, err error)
    GetTransitionRecoveryState(ctx) TransitionRecoveryState
    SetTransitionRecoveryState(ctx, state TransitionRecoveryState)
    DeleteTransitionRecoveryState(ctx)

    // XP management
    GrantXP(ctx, member sdk.AccAddress, amount uint64, source XPSource, reference string) error
    GetMemberXP(ctx, member sdk.AccAddress) uint64
    CalculateLevel(ctx, xp uint64) uint32
    GetEpochXPEarned(ctx, member sdk.AccAddress, epoch int64) uint64

    // Profile management
    GetMemberProfile(ctx, member sdk.AccAddress) MemberProfile
    SetMemberProfile(ctx, profile MemberProfile)
    HasMemberProfile(ctx, member sdk.AccAddress) bool
    SetDisplayName(ctx, member sdk.AccAddress, name string) error
    SetUsername(ctx, member sdk.AccAddress, username string) error

    // Achievement management
    CheckAndGrantAchievements(ctx, member sdk.AccAddress) ([]string, error)
    GrantAchievement(ctx, member sdk.AccAddress, achievementID string) error
    GetMemberAchievements(ctx, member sdk.AccAddress) []string
    HasAchievement(ctx, member sdk.AccAddress, achievementID string) bool

    // Achievement progress tracking (for hooks)
    IncrementChallengesWon(ctx, member sdk.AccAddress)
    GetChallengesWon(ctx, member sdk.AccAddress) uint64
    IncrementJuryDutyCount(ctx, member sdk.AccAddress)
    GetJuryDutyCount(ctx, member sdk.AccAddress) uint64
    IncrementVotesCast(ctx, member sdk.AccAddress)
    GetVotesCast(ctx, member sdk.AccAddress) uint64
    IncrementForumHelpfulCount(ctx, member sdk.AccAddress)
    GetForumHelpfulCount(ctx, member sdk.AccAddress) uint64
    IncrementInvitationsSuccessful(ctx, member sdk.AccAddress)
    GetInvitationsSuccessful(ctx, member sdk.AccAddress) uint64

    // Title management
    GetTitle(ctx, titleID string) (Title, error)
    UnlockTitle(ctx, member sdk.AccAddress, titleID string) error
    SetDisplayTitle(ctx, member sdk.AccAddress, titleID string) error

    // Guild management
    CreateGuild(ctx, founder sdk.AccAddress, name, description string, inviteOnly bool) (uint64, error)
    JoinGuild(ctx, member sdk.AccAddress, guildID uint64) error
    LeaveGuild(ctx, member sdk.AccAddress) error
    TransferGuildFounder(ctx, guildID uint64, newFounder sdk.AccAddress) error
    DissolveGuild(ctx, founder sdk.AccAddress, guildID uint64) error
    GetGuild(ctx, guildID uint64) (Guild, error)
    GetMemberGuild(ctx, member sdk.AccAddress) uint64

    // Guild officer management
    PromoteToOfficer(ctx, guildID uint64, member sdk.AccAddress) error
    DemoteOfficer(ctx, guildID uint64, officer sdk.AccAddress) error
    IsGuildOfficer(ctx, guildID uint64, member sdk.AccAddress) bool

    // Guild invitation management
    InviteToGuild(ctx, guildID uint64, invitee sdk.AccAddress) error
    AcceptGuildInvite(ctx, member sdk.AccAddress, guildID uint64) error
    RevokeGuildInvite(ctx, guildID uint64, invitee sdk.AccAddress) error
    SetGuildInviteOnly(ctx, guildID uint64, inviteOnly bool) error
    GetGuildInvites(ctx, guildID uint64) []string
    GetMemberGuildInvites(ctx, member sdk.AccAddress) []uint64

    // Guild administration
    UpdateGuildDescription(ctx, guildID uint64, description string) error
    KickFromGuild(ctx, guildID uint64, member sdk.AccAddress, reason string) error
    ClaimGuildFounder(ctx, member sdk.AccAddress, guildID uint64) error
    HandleFounderDeparture(ctx, guildID uint64) error

    // Guild internal helpers
    GetGuildMemberCount(ctx, guildID uint64) uint64
    GetGuildMembership(ctx, member sdk.AccAddress) GuildMembership
    SetGuildMembership(ctx, membership GuildMembership)
    GetGuildMembers(ctx, guildID uint64) []string
    IterateGuilds(ctx, fn func(guild Guild) bool)
    ResetGuildXPForSeason(ctx)

    // Quest management
    CreateQuest(ctx, quest Quest) (string, error)
    UpdateQuest(ctx, quest Quest) error
    DeactivateQuest(ctx, questID string) error
    StartQuest(ctx, member sdk.AccAddress, questID string) error
    // SECURITY: UpdateQuestProgress is NOT exposed via MsgServer.
    // Only authorized module hooks may call this method:
    // - x/rep: OnInitiativeCompleted, OnChallengeResolved
    // - x/forum: OnPostMarkedHelpful (via ForumHooks)
    // - x/gov: OnVoteCast, OnProposalCreated
    // Direct user calls to update quest progress are NOT permitted.
    UpdateQuestProgress(ctx, member sdk.AccAddress, questID string, objectiveIndex int, progress uint64) error
    ClaimQuestReward(ctx, member sdk.AccAddress, questID string) (uint64, error)
    AbandonQuest(ctx, member sdk.AccAddress, questID string) error
    GetActiveQuests(ctx) []Quest
    GetAvailableQuests(ctx, member sdk.AccAddress) []Quest
    GetMemberQuestProgress(ctx, member sdk.AccAddress, questID string) (MemberQuestProgress, error)
    GetMemberQuestProgressIfExists(ctx, member sdk.AccAddress, questID string) (MemberQuestProgress, bool)
    CanStartQuest(ctx, member sdk.AccAddress, quest Quest) error
    HandleSeasonalQuestExpiration(ctx, season uint32) error
    ClearMemberQuestProgress(ctx, member sdk.AccAddress) error

    // Quest internal helpers
    GetQuest(ctx, questID string) (Quest, error)
    SetQuest(ctx, quest Quest)
    GetQuestsBySeason(ctx, season uint32) []Quest
    GetMemberInProgressQuests(ctx, member sdk.AccAddress) []MemberQuestProgress
    ClearIncompleteQuestProgress(ctx, questID string)
    SetMemberQuestProgress(ctx, progress MemberQuestProgress)
    DeleteMemberQuestProgress(ctx, member sdk.AccAddress, questID string)

    // Seasonal titles (leaderboards removed - computed by indexers)
    GrantSeasonalTitles(ctx)

    // Epoch helpers
    GetCurrentEpoch(ctx) int64
    BlockToEpoch(ctx, block int64) int64
    EpochToBlock(ctx, epoch int64) int64

    // Historical data management
    PruneOldSnapshots(ctx) error
    HasSeasonSnapshots(ctx, seasonNumber uint32) bool
    DeleteSeasonSnapshots(ctx, seasonNumber uint32)

    // State cleanup (prevent unbounded growth)
    PruneOldEpochTrackers(ctx)
    PruneOldForumXPCooldowns(ctx)
    PruneOldVoteXPRecords(ctx)

    // Guild invite management (with expiration)
    GetGuildInvitesRaw(ctx, guildID uint64) []GuildInvite
    SetGuildInvite(ctx, invite GuildInvite)
    DeleteGuildInvite(ctx, guildID uint64, invitee sdk.AccAddress)
    CleanupExpiredGuildInvites(ctx, guildID uint64)
    CleanupExpiredGuildInvitesBatch(ctx, batchSize int)
    IterateGuildInvitesFrom(ctx, cursor GuildInvite, cb func(GuildInvite) bool)
    GetInviteCleanupCursor(ctx) GuildInvite
    SetInviteCleanupCursor(ctx, cursor GuildInvite)

    // Forum XP management (implements ForumHooks from x/forum)
    GetEpochXPTracker(ctx, member sdk.AccAddress, epoch int64) EpochXPTracker
    SetEpochXPTracker(ctx, tracker EpochXPTracker)
    GetMemberRegistration(ctx, member sdk.AccAddress) MemberRegistration
    SetMemberRegistration(ctx, reg MemberRegistration)
    GetForumXPCooldown(ctx, beneficiary, actor sdk.AccAddress) ForumXPCooldown
    SetForumXPCooldown(ctx, cooldown ForumXPCooldown)

    // Display name moderation management
    GetDisplayNameModeration(ctx, member sdk.AccAddress) DisplayNameModeration
    SetDisplayNameModeration(ctx, moderation DisplayNameModeration)
    CheckDisplayNameModeration(ctx, member sdk.AccAddress, name string) error
    SetDisplayNameReportStake(ctx, challengeID string, reporter sdk.AccAddress, amount math.Int)
    GetDisplayNameReportStake(ctx, challengeID string) DisplayNameReportStake
    DeleteDisplayNameReportStake(ctx, challengeID string)
    SetDisplayNameAppealStake(ctx, challengeID string, appellant sdk.AccAddress, amount math.Int)
    GetDisplayNameAppealStake(ctx, challengeID string) DisplayNameAppealStake
    DeleteDisplayNameAppealStake(ctx, challengeID string)

    // Retroactive Public Goods Nominations
    Nominate(ctx, nominator sdk.AccAddress, contentRef, rationale string) (uint64, error)
    StakeNomination(ctx, staker sdk.AccAddress, nominationID uint64, amount math.Int) error
    UnstakeNomination(ctx, staker sdk.AccAddress, nominationID uint64) error
    GetNomination(ctx, nominationID uint64) (Nomination, error)
    GetNominationsByContentRef(ctx, contentRef string, season uint32) (Nomination, bool)
    ListNominations(ctx, season uint32) []Nomination
    ListNominationsByCreator(ctx, creator string, season uint32) []Nomination
    ProcessRetroRewards(ctx) error
    ReturnNominationStakes(ctx) error
    IsNominationWindowOpen(ctx) bool
    OpenNominationWindow(ctx) error
}
```

## Hooks Interface

The season module exposes hooks for other modules to trigger XP grants and achievement tracking.

**Note:** `x/forum` uses a different hooks interface (`ForumHooks`, see section "ForumHooks Implementation" below). The `SeasonHooks` interface below is for modules like `x/gov` and `x/rep` that rely on x/season for anti-gaming logic.

```go
type SeasonHooks interface {
    // From x/gov or x/commons
    OnVoteCast(ctx sdk.Context, voter sdk.AccAddress, proposalID uint64) error
    OnProposalCreated(ctx sdk.Context, proposer sdk.AccAddress, proposalID uint64) error

    // From x/rep - member lifecycle
    OnMemberCreated(ctx sdk.Context, member sdk.AccAddress) error
    OnMemberEstablished(ctx sdk.Context, member sdk.AccAddress) error
    OnInitiativeCompleted(ctx sdk.Context, member sdk.AccAddress, initiativeID string) error

    // From x/rep - accountability (for achievements)
    OnChallengeResolved(ctx sdk.Context, challenger sdk.AccAddress, challengee sdk.AccAddress, challengerWon bool) error
    OnJuryDutyCompleted(ctx sdk.Context, juror sdk.AccAddress, juryReviewID uint64) error

    // From x/rep - zeroing (accountability cleanup)
    OnMemberZeroed(ctx sdk.Context, member sdk.AccAddress) error
}

// QuestProgressHooks are called by other modules to update quest objective progress.
// These are separate from SeasonHooks because they may be called frequently.
type QuestProgressHooks interface {
    // OnQuestObjectiveProgress updates progress for quests with matching objective types.
    // Called by x/gov (votes), x/forum (helpful marks), x/rep (initiatives), etc.
    // The hook finds all in-progress quests for the member with matching objective types
    // and increments their progress.
    OnQuestObjectiveProgress(ctx sdk.Context, member sdk.AccAddress, objectiveType QuestObjectiveType, increment uint64) error
}
```

### Hook Implementations

```go
// OnVoteCast grants XP for governance participation
func (k Keeper) OnVoteCast(ctx sdk.Context, voter sdk.AccAddress, proposalID uint64) error {
    params := k.GetParams(ctx)
    season := k.GetCurrentSeason(ctx)

    // Check if already earned XP for this proposal
    if k.HasVoteXPRecord(ctx, season.Number, voter, proposalID) {
        return nil
    }

    // Check epoch cap
    epoch := k.GetCurrentEpoch(ctx)
    voteXPCount := k.GetVoteXPCountThisEpoch(ctx, voter, epoch)
    if voteXPCount >= params.MaxVoteXPPerEpoch {
        return nil
    }

    // Grant XP
    k.GrantXP(ctx, voter, params.XpVoteCast, XP_SOURCE_VOTE_CAST, fmt.Sprintf("proposal:%d", proposalID))
    k.SetVoteXPRecord(ctx, season.Number, voter, proposalID)
    k.IncrementVoteXPCount(ctx, voter, epoch)

    // Track for "voter" achievement
    k.IncrementVotesCast(ctx, voter)
    k.CheckAndGrantAchievements(ctx, voter)

    return nil
}

// NOTE: x/forum does NOT use hooks - it calls GrantXP() directly with its own
// anti-gaming logic. See x-forum-spec.md section 7.5 for details.
// x/forum should also call IncrementForumHelpfulCount() when a post is marked helpful
// to track progress toward the "helpful" and "sage" achievements.

// OnMemberCreated initializes a MemberProfile when a new member joins via x/rep
func (k Keeper) OnMemberCreated(ctx sdk.Context, member sdk.AccAddress) error {
    // Check if profile already exists (shouldn't happen, but defensive)
    if k.HasMemberProfile(ctx, member) {
        return nil
    }

    currentEpoch := k.GetCurrentEpoch(ctx)

    // Create initial profile
    profile := MemberProfile{
        Address:          member.String(),
        SeasonXP:         0,
        SeasonLevel:      1,
        LifetimeXP:       0,
        LastActiveEpoch:  currentEpoch,
        // All other fields default to zero/empty
    }
    k.SetMemberProfile(ctx, profile)

    // Record registration for forum XP account age checks
    registration := MemberRegistration{
        Member:          member.String(),
        RegisteredEpoch: currentEpoch,
    }
    k.SetMemberRegistration(ctx, registration)

    // Update season stats
    season := k.GetCurrentSeason(ctx)
    season.TotalMembersJoined++
    k.SetSeason(ctx, season)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("member_profile_created",
            sdk.NewAttribute("member", member.String()),
            sdk.NewAttribute("season", fmt.Sprintf("%d", season.Number)),
        ),
    )

    return nil
}

// OnMemberEstablished grants XP to the inviter (mentorship outcome)
func (k Keeper) OnMemberEstablished(ctx sdk.Context, member sdk.AccAddress) error {
    params := k.GetParams(ctx)

    // Find inviter
    memberData := k.repKeeper.GetMember(ctx, member)
    if memberData == nil || memberData.InvitedBy == "" {
        return nil
    }

    inviter, err := sdk.AccAddressFromBech32(memberData.InvitedBy)
    if err != nil {
        return nil
    }

    // Grant XP to inviter for successful mentorship
    k.GrantXP(ctx, inviter, params.XpInviteeEstablished, XP_SOURCE_INVITEE_ESTABLISHED, member.String())

    // Track for "talent_scout" achievement
    k.IncrementInvitationsSuccessful(ctx, inviter)
    k.CheckAndGrantAchievements(ctx, inviter)

    return nil
}

// OnInitiativeCompleted checks if this triggers inviter XP (first initiative)
func (k Keeper) OnInitiativeCompleted(ctx sdk.Context, member sdk.AccAddress, initiativeID string) error {
    params := k.GetParams(ctx)

    memberData := k.repKeeper.GetMember(ctx, member)
    if memberData == nil {
        return nil
    }

    // Check if this is their first initiative
    if memberData.InitiativesCompleted != 1 {
        return nil
    }

    // Find inviter
    if memberData.InvitedBy == "" {
        return nil
    }

    inviter, err := sdk.AccAddressFromBech32(memberData.InvitedBy)
    if err != nil {
        return nil
    }

    // Grant XP to inviter
    k.GrantXP(ctx, inviter, params.XpInviteeFirstInitiative, XP_SOURCE_INVITEE_FIRST_INITIATIVE, member.String())

    return nil
}

// OnChallengeResolved tracks successful challenges for the "Watchdog" achievement
func (k Keeper) OnChallengeResolved(ctx sdk.Context, challenger sdk.AccAddress, challengee sdk.AccAddress, challengerWon bool) error {
    if !challengerWon {
        return nil
    }

    // Track successful challenge for achievements
    profile := k.GetMemberProfile(ctx, challenger)
    k.IncrementChallengesWon(ctx, challenger)

    // Check for "watchdog" achievement
    k.CheckAndGrantAchievements(ctx, challenger)

    return nil
}

// OnJuryDutyCompleted tracks jury service for the "Twelve Angry Members" achievement
func (k Keeper) OnJuryDutyCompleted(ctx sdk.Context, juror sdk.AccAddress, juryReviewID uint64) error {
    // Track jury duty completion for achievements
    k.IncrementJuryDutyCount(ctx, juror)

    // Check for "juror" achievement
    k.CheckAndGrantAchievements(ctx, juror)

    return nil
}

// OnMemberZeroed handles cleanup when a member is zeroed in x/rep
// This ensures consistency when a member loses all DREAM and reputation
//
// Design Philosophy:
// - Earned achievements and titles are PRESERVED (you earned them, they're forever)
// - Achievement PROGRESS counters are RESET (you're starting fresh toward future achievements)
// - This aligns with "no permanent exclusion" - keep what you earned, start fresh on progress
func (k Keeper) OnMemberZeroed(ctx sdk.Context, member sdk.AccAddress) error {
    // 1. Remove from guild (triggers succession if founder)
    guildID := k.GetMemberGuild(ctx, member)
    if guildID != 0 {
        guild, err := k.GetGuild(ctx, guildID)
        if err == nil {
            if guild.Founder == member.String() {
                // Founder being zeroed - trigger succession
                k.HandleFounderDeparture(ctx, guildID)
            } else {
                // Remove from officers if applicable
                if k.IsGuildOfficer(ctx, guildID, member) {
                    k.removeOfficer(ctx, guild, member)
                }
            }
        }

        // Clear membership
        membership := k.GetGuildMembership(ctx, member)
        membership.GuildID = 0
        membership.LeftEpoch = k.GetCurrentEpoch(ctx)
        k.SetGuildMembership(ctx, membership)

        // Check if guild dropped below minimum members - auto-freeze if so
        k.checkAndFreezeUnderMinimum(ctx, guildID)
    }

    // 2. Clear all in-progress quest progress
    k.ClearMemberQuestProgress(ctx, member)

    // 3. Release username via x/name (allows re-registration by others)
    // Note: Display name is NOT released (it's non-unique and not reserved)
    profile := k.GetMemberProfile(ctx, member)
    releasedUsername := ""
    if profile.Username != "" {
        // Only release if it was reserved as NameTypeUsername (not if owned via x/name directly)
        if !k.nameKeeper.IsNameOwner(ctx, profile.Username, member) {
            releasedUsername = profile.Username
            k.nameKeeper.ReleaseName(ctx, profile.Username)
        }
        profile.Username = ""
    }

    // 4. Reset season XP and achievement progress counters
    profile.SeasonXP = 0
    profile.SeasonLevel = 1

    // Reset achievement progress counters (but keep earned achievements/titles)
    // This means if someone had 4/5 challenges won for "Watchdog" achievement,
    // they start over at 0/5 but KEEP any achievements already unlocked
    profile.ChallengesWon = 0
    profile.JuryDutiesCompleted = 0
    profile.VotesCast = 0
    profile.ForumHelpfulCount = 0
    profile.InvitationsSuccessful = 0

    // Preserved:
    // - profile.Achievements (earned, never revoked)
    // - profile.UnlockedTitles (earned, never revoked)
    // - profile.LifetimeXP (historical record)
    k.SetMemberProfile(ctx, profile)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("member_zeroed_cleanup",
            sdk.NewAttribute("member", member.String()),
            sdk.NewAttribute("guild_removed", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("display_name_released", releasedName),
            sdk.NewAttribute("achievement_progress_reset", "true"),
        ),
    )

    return nil
}

// OnQuestObjectiveProgress updates quest progress when relevant actions occur.
// This is called by other modules (x/gov, x/forum, x/rep) when actions happen
// that could advance quest objectives.
func (k Keeper) OnQuestObjectiveProgress(ctx sdk.Context, member sdk.AccAddress, objectiveType QuestObjectiveType, increment uint64) error {
    // Get all in-progress quests for this member
    inProgressQuests := k.GetMemberInProgressQuests(ctx, member)

    for _, progress := range inProgressQuests {
        quest, err := k.GetQuest(ctx, progress.QuestId)
        if err != nil {
            continue
        }

        // Check each objective
        updated := false
        for i, objective := range quest.Objectives {
            if objective.Type == objectiveType {
                // Increment progress for this objective
                if uint64(len(progress.ObjectiveProgress)) > uint64(i) {
                    oldProgress := progress.ObjectiveProgress[i]
                    newProgress := oldProgress + increment
                    if newProgress > objective.Target {
                        newProgress = objective.Target
                    }
                    if newProgress != oldProgress {
                        progress.ObjectiveProgress[i] = newProgress
                        updated = true
                    }
                }
            }
        }

        if updated {
            k.SetMemberQuestProgress(ctx, progress)

            // Check if quest is now complete
            if k.isQuestComplete(ctx, quest, progress) {
                ctx.EventManager().EmitEvent(
                    sdk.NewEvent("quest_objectives_complete",
                        sdk.NewAttribute("member", member.String()),
                        sdk.NewAttribute("quest_id", quest.Id),
                    ),
                )
            }
        }
    }

    // Update last active epoch for activity tracking
    profile := k.GetMemberProfile(ctx, member)
    profile.LastActiveEpoch = k.GetCurrentEpoch(ctx)
    k.SetMemberProfile(ctx, profile)

    return nil
}

// Helper: check if all objectives are complete
func (k Keeper) isQuestComplete(ctx sdk.Context, quest Quest, progress MemberQuestProgress) bool {
    for i, objective := range quest.Objectives {
        if uint64(len(progress.ObjectiveProgress)) <= uint64(i) {
            return false
        }
        if progress.ObjectiveProgress[i] < objective.Target {
            return false
        }
    }
    return true
}
```

### ForumHooks Implementation

x/season implements the `ForumHooks` interface from x/forum to handle forum XP with anti-gaming logic.

```go
// SeasonForumHooks implements forum.ForumHooks
type SeasonForumHooks struct {
    keeper Keeper
}

func NewSeasonForumHooks(k Keeper) SeasonForumHooks {
    return SeasonForumHooks{keeper: k}
}

// OnReplyReceived grants XP to post author when someone replies to their post
func (h SeasonForumHooks) OnReplyReceived(ctx sdk.Context, postAuthor, replier string, postID uint64) {
    h.keeper.handleForumReplyReceived(ctx, postAuthor, replier, postID)
}

// OnPostMarkedHelpful grants XP to post author when their post is marked helpful
func (h SeasonForumHooks) OnPostMarkedHelpful(ctx sdk.Context, postAuthor, marker string, postID uint64, isReply bool) {
    h.keeper.handleForumPostMarkedHelpful(ctx, postAuthor, marker, postID)
}

// OnPostCreated - no XP granted for creating posts (prevents spam incentive)
func (h SeasonForumHooks) OnPostCreated(ctx sdk.Context, author string, postID uint64, isReply bool) {
    // No XP for posting - XP is earned from engagement received, not actions taken
    // This prevents "post farming" where users spam low-quality content
}

// OnPostHidden - no direct XP impact (reputation impact is in x/rep if needed)
func (h SeasonForumHooks) OnPostHidden(ctx sdk.Context, author string, postID uint64, sentinel string) {
    // Hidden posts don't affect XP directly
    // Reputation impact, if any, would be handled by x/rep
}

// OnPostRestored - no direct XP impact
func (h SeasonForumHooks) OnPostRestored(ctx sdk.Context, author string, postID uint64) {
    // Restored posts don't grant/restore XP
}

// handleForumReplyReceived implements the anti-gaming logic for reply XP
func (k Keeper) handleForumReplyReceived(ctx sdk.Context, postAuthor, replier string, postID uint64) {
    params := k.GetParams(ctx)

    // Self-reply check: cannot earn XP from replies to your own posts
    if postAuthor == replier {
        return
    }

    author, err := sdk.AccAddressFromBech32(postAuthor)
    if err != nil {
        return
    }
    actor, err := sdk.AccAddressFromBech32(replier)
    if err != nil {
        return
    }

    // Check actor account age (replier must be old enough to grant XP)
    if !k.isAccountOldEnough(ctx, actor, params.ForumXpMinAccountAgeEpochs) {
        return
    }

    // Check reciprocal cooldown (same replier can't grant XP to same author too frequently)
    if !k.checkForumXPCooldown(ctx, author, actor, params.ForumXpReciprocalCooldownEpochs) {
        return
    }

    // Check per-epoch forum XP cap
    epoch := k.GetCurrentEpoch(ctx)
    tracker := k.GetEpochXPTracker(ctx, author, epoch)
    if tracker.ForumXpEarned >= params.MaxForumXpPerEpoch {
        return
    }

    // Grant XP
    xpAmount := params.XpForumReplyReceived
    remaining := params.MaxForumXpPerEpoch - tracker.ForumXpEarned
    if xpAmount > remaining {
        xpAmount = remaining
    }

    // Note: GrantXP handles the EpochXPTracker update internally
    k.GrantXP(ctx, author, xpAmount, XP_SOURCE_FORUM_REPLY, fmt.Sprintf("post:%d:replier:%s", postID, replier))

    // Set cooldown
    k.setForumXPCooldown(ctx, author, actor)

    // Update quest progress for QUEST_OBJECTIVE_FORUM_HELPFUL (replies count toward engagement)
    // Note: Separate from helpful marks, but both indicate content quality
}

// handleForumPostMarkedHelpful implements the anti-gaming logic for helpful XP
func (k Keeper) handleForumPostMarkedHelpful(ctx sdk.Context, postAuthor, marker string, postID uint64) {
    params := k.GetParams(ctx)

    // Self-mark check: cannot mark your own post as helpful
    if postAuthor == marker {
        return
    }

    author, err := sdk.AccAddressFromBech32(postAuthor)
    if err != nil {
        return
    }
    actor, err := sdk.AccAddressFromBech32(marker)
    if err != nil {
        return
    }

    // Check actor account age
    if !k.isAccountOldEnough(ctx, actor, params.ForumXpMinAccountAgeEpochs) {
        return
    }

    // Check reciprocal cooldown
    if !k.checkForumXPCooldown(ctx, author, actor, params.ForumXpReciprocalCooldownEpochs) {
        return
    }

    // Check per-epoch forum XP cap
    epoch := k.GetCurrentEpoch(ctx)
    tracker := k.GetEpochXPTracker(ctx, author, epoch)
    if tracker.ForumXpEarned >= params.MaxForumXpPerEpoch {
        return
    }

    // Grant XP (helpful marks are worth more than replies)
    xpAmount := params.XpForumMarkedHelpful
    remaining := params.MaxForumXpPerEpoch - tracker.ForumXpEarned
    if xpAmount > remaining {
        xpAmount = remaining
    }

    // Note: GrantXP handles the EpochXPTracker update internally
    k.GrantXP(ctx, author, xpAmount, XP_SOURCE_FORUM_HELPFUL, fmt.Sprintf("post:%d:marker:%s", postID, marker))

    // Set cooldown
    k.setForumXPCooldown(ctx, author, actor)

    // Track for "helpful" and "sage" achievements
    k.IncrementForumHelpfulCount(ctx, author)
    k.CheckAndGrantAchievements(ctx, author)

    // Update quest progress
    k.OnQuestObjectiveProgress(ctx, author, QUEST_OBJECTIVE_FORUM_HELPFUL, 1)
}

// isAccountOldEnough checks if member registered at least minEpochs ago
func (k Keeper) isAccountOldEnough(ctx sdk.Context, member sdk.AccAddress, minEpochs uint64) bool {
    registration := k.GetMemberRegistration(ctx, member)
    if registration.RegisteredEpoch == 0 {
        return false // Not found or not registered
    }
    currentEpoch := k.GetCurrentEpoch(ctx)
    return uint64(currentEpoch-registration.RegisteredEpoch) >= minEpochs
}

// checkForumXPCooldown returns true if cooldown has passed (XP can be granted)
func (k Keeper) checkForumXPCooldown(ctx sdk.Context, beneficiary, actor sdk.AccAddress, cooldownEpochs uint64) bool {
    cooldown := k.GetForumXPCooldown(ctx, beneficiary, actor)
    if cooldown.LastGrantedEpoch == 0 {
        return true // No previous grant
    }
    currentEpoch := k.GetCurrentEpoch(ctx)
    return uint64(currentEpoch-cooldown.LastGrantedEpoch) >= cooldownEpochs
}

// setForumXPCooldown records when actor granted XP to beneficiary
func (k Keeper) setForumXPCooldown(ctx sdk.Context, beneficiary, actor sdk.AccAddress) {
    cooldown := ForumXPCooldown{
        Beneficiary:      beneficiary.String(),
        Actor:            actor.String(),
        LastGrantedEpoch: k.GetCurrentEpoch(ctx),
    }
    k.SetForumXPCooldown(ctx, cooldown)
}
```

## XP Sources

```go
type XPSource int

const (
    // Governance participation
    XP_SOURCE_VOTE_CAST XPSource = iota
    XP_SOURCE_PROPOSAL_CREATED

    // Forum engagement (received from others)
    XP_SOURCE_FORUM_REPLY
    XP_SOURCE_FORUM_HELPFUL

    // Mentorship outcomes
    XP_SOURCE_INVITEE_FIRST_INITIATIVE
    XP_SOURCE_INVITEE_ESTABLISHED

    // Progression
    XP_SOURCE_ACHIEVEMENT
    XP_SOURCE_QUEST
)
```

## Retroactive Public Goods Funding

### Overview

At the end of each season, a **nomination window** opens where members can nominate completed contributions for retroactive DREAM rewards. This creates a seasonal "retrospective ceremony" that rewards the unexpected public goods that were never pre-planned as initiatives.

### Lifecycle

```
Season ACTIVE ──────────────────────────────────────────────┐
                                                            │
  [nomination_window_epochs before end_block]               │
                                                            ▼
Season NOMINATION ──── Members nominate + stake conviction ─┐
                                                            │
  [end_block reached]                                       │
                                                            ▼
Season ENDING ────── PhaseRetroRewards ─────────────────────┐
                     (calculate conviction, mint rewards)    │
                                                            ▼
                     PhaseReturnNominationStakes ────────────┐
                     (return staked DREAM to stakers)        │
                                                            ▼
                     PhaseSnapshot → ... → PhaseComplete
```

### Nomination Rules

1. **Who can nominate:** Any active member with trust level ≥ `nomination_min_trust_level` (default: PROVISIONAL)
2. **What can be nominated:** Content created during the current season that is active (not hidden/deleted), where the creator is an active member. Content types: blog posts, blog replies, forum threads, forum replies, collections
3. **Limit:** `max_nominations_per_member` per season (default: 5)
4. **Deduplication:** One nomination per content reference per season. If already nominated, stake on the existing nomination instead
5. **Self-nomination:** Allowed — an author may nominate their own work if nobody else has. However, self-staking on own-content nominations is prohibited (same rule as content conviction)

### Conviction Staking on Nominations

During the nomination window, members stake DREAM on nominations to signal quality conviction:

- Uses the same conviction formula: `conviction(t) = stake_amount * (1 - 2^(-t / half_life))`
- Shorter half-life (`nomination_conviction_half_life_epochs`, default 3 epochs) ensures conviction builds meaningfully within the ~2 week window
- Min stake: `nomination_min_stake` (default: 10 DREAM)
- No self-staking: members cannot stake on nominations for content they created
- Stakes are locked during the nomination window and returned after `PhaseReturnNominationStakes`

### Reward Distribution (PhaseRetroRewards)

During the season transition:

1. **Rank** all nominations by conviction score (descending)
2. **Filter** nominations below `retro_reward_min_conviction` (default: 50.0)
3. **Select** top `retro_reward_max_recipients` (default: 20) nominations
4. **Distribute** the `retro_reward_budget_per_season` (default: 50,000 DREAM) proportionally by conviction:
   ```
   reward_i = budget * (conviction_i / sum(conviction_all_rewarded))
   ```
5. **Mint** DREAM to each content creator via `repKeeper.MintDream(ctx, creator, amount)`
6. **Emit** `EventRetroRewardGranted` for each rewarded nomination
7. **Emit** `EventRetroRewardsProcessed` with aggregate totals

### Anti-Gaming

- **No self-staking:** Authors cannot stake conviction on their own nominations (enforced at message validation)
- **Time-weighted conviction:** Flash-staking at the last moment yields near-zero conviction
- **Per-member nomination limit:** Prevents nomination spam
- **Minimum conviction threshold:** Filters out low-quality nominations
- **DREAM cost:** Staking locks real DREAM, creating economic cost for manipulation
- **Seasonal scope:** Only content created during the current season is eligible, preventing re-nomination of old content

### Content Validation

The `x/season` module needs to validate that nominated content exists, is active, and was created during the current season. This requires keeper interfaces from content modules:

```go
// Expected keeper interfaces for retroactive public goods funding

// RepKeeper additions (added to existing RepKeeper interface in x/season)
type RepKeeper interface {
    // ... existing methods (IsMember, GetMember, BurnDREAM, etc.) ...

    // DREAM minting for retroactive rewards
    MintDream(ctx context.Context, recipient sdk.AccAddress, amount math.Int) error

    // Member and trust level checks
    IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool
    GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)
}

type BlogKeeper interface {
    GetPost(ctx context.Context, postID uint64) (blogtypes.Post, error)
    GetReply(ctx context.Context, replyID uint64) (blogtypes.Reply, error)
}

type ForumKeeper interface {
    GetPost(ctx context.Context, postID uint64) (forumtypes.Post, error)
}

type CollectKeeper interface {
    GetCollection(ctx context.Context, collectionID uint64) (collecttypes.Collection, error)
}
```

Content ref parsing maps `"blog/post/42"` → `BlogKeeper.GetPost(ctx, 42)`, etc. The keeper validates:
1. Content exists
2. Content status is active (not hidden/deleted)
3. Content was created during the current season (created_at ≥ season.StartBlock)
4. Content creator is an active x/rep member

## Season Transition Logic

Season transitions use batched processing to avoid exceeding block gas limits.

### Maintenance Mode

During critical transition phases (reputation archival/reset), the system enters **maintenance mode** to prevent state inconsistencies. In maintenance mode:

**Blocked Actions:**
- `MsgStake` / `MsgUnstake` (x/rep) - Reputation calculations in flux
- `MsgSubmitInitiative` / `MsgCompleteInitiative` (x/rep) - Reputation grants blocked
- `MsgChallenge` (x/rep) - New challenges blocked during reset
- `MsgJoinGuild` / `MsgLeaveGuild` (x/season) - Guild XP calculations in flux
- `MsgStartQuest` / `MsgClaimQuestReward` (x/season) - XP grants blocked

**Allowed Actions:**
- All read queries
- `MsgSetDisplayName` / `MsgSetDisplayTitle` - Profile cosmetics
- Governance voting (x/gov, x/commons)
- DREAM transfers (tips/gifts)

**Duration:** Maintenance mode is only active during `PhaseArchiveReputation`, `PhaseResetReputation`, and `PhaseResetXP` phases. Other transition phases run without blocking.

### Transition State Machine

```go
type SeasonTransitionState struct {
    Phase           TransitionPhase
    ProcessedCount  uint64
    TotalCount      uint64
    LastProcessed   string
    TransitionStart int64
    MaintenanceMode bool              // True during blocking phases
}

type TransitionPhase int

const (
    PhaseRetroRewards TransitionPhase = iota  // Process retroactive public goods rewards
    PhaseReturnNominationStakes               // Return nomination stakes to stakers
    PhaseSnapshot                              // Snapshot member state
    PhaseArchiveReputation                     // Maintenance mode ON
    PhaseResetReputation                       // Maintenance mode ON
    PhaseResetXP                               // Maintenance mode ON
    PhaseTitles                                // Grant seasonal titles
    PhaseCleanup                               // Prune old data (trackers, cooldowns, snapshots, nominations)
    PhaseComplete
)
```

### Processing

```go
func (k Keeper) ProcessSeasonTransitionBatch(ctx sdk.Context, batchSize int) (done bool, err error) {
    state := k.GetTransitionState(ctx)
    season := k.GetCurrentSeason(ctx)

    switch state.Phase {
    case PhaseRetroRewards:
        // Calculate conviction scores and mint DREAM for top nominations
        err := k.ProcessRetroRewards(ctx)
        if err != nil {
            return false, err
        }
        state.Phase = PhaseReturnNominationStakes
        state.LastProcessed = ""

    case PhaseReturnNominationStakes:
        // Return staked DREAM to all nomination stakers
        err := k.ReturnNominationStakes(ctx)
        if err != nil {
            return false, err
        }
        state.Phase = PhaseSnapshot
        state.LastProcessed = ""

    case PhaseSnapshot:
        done, err := k.snapshotMembersBatch(ctx, state, batchSize)
        if done {
            // Pre-compute title eligibility BEFORE any state changes
            // This captures accurate rankings at snapshot time
            k.ComputeSeasonTitleEligibility(ctx)

            state.Phase = PhaseArchiveReputation
            state.LastProcessed = ""
            // Enter maintenance mode for critical phases
            state.MaintenanceMode = true
            season.Status = SeasonStatus_MAINTENANCE
            k.SetSeason(ctx, season)
            ctx.EventManager().EmitEvent(
                sdk.NewEvent("maintenance_mode_started",
                    sdk.NewAttribute("season_number", fmt.Sprintf("%d", season.Number)),
                ),
            )
        }

    case PhaseArchiveReputation:
        done, err := k.archiveReputationBatch(ctx, state, batchSize)
        if done {
            state.Phase = PhaseResetReputation
            state.LastProcessed = ""
        }

    case PhaseResetReputation:
        done, err := k.resetReputationBatch(ctx, state, batchSize)
        if done {
            state.Phase = PhaseResetXP
            state.LastProcessed = ""
        }

    case PhaseResetXP:
        done, err := k.resetXPBatch(ctx, state, batchSize)
        if done {
            state.Phase = PhaseTitles
            // Exit maintenance mode
            state.MaintenanceMode = false
            season.Status = SeasonStatus_ENDING
            k.SetSeason(ctx, season)
            ctx.EventManager().EmitEvent(
                sdk.NewEvent("maintenance_mode_ended",
                    sdk.NewAttribute("season_number", fmt.Sprintf("%d", season.Number)),
                ),
            )
        }

    case PhaseTitles:
        // Grant seasonal titles with season prefix (e.g., "S3 Champion")
        // Titles are permanent once earned - not revoked at season end
        k.GrantSeasonalTitles(ctx)
        state.Phase = PhaseCleanup

    case PhaseCleanup:
        // Prune old data to prevent unbounded state growth
        k.PruneOldSnapshots(ctx)
        k.PruneOldEpochTrackers(ctx)
        k.PruneOldForumXPCooldowns(ctx)
        k.PruneOldVoteXPRecords(ctx)
        state.Phase = PhaseComplete

    case PhaseComplete:
        season.Status = SeasonStatus_COMPLETED
        k.SetSeason(ctx, season)
        k.DeleteTransitionState(ctx)
        k.StartNewSeason(ctx, getNextSeasonName(), getNextSeasonTheme())
        return true, nil
    }

    k.SetTransitionState(ctx, state)
    return false, nil
}

// IsInMaintenanceMode checks if the system is blocking certain actions
func (k Keeper) IsInMaintenanceMode(ctx sdk.Context) bool {
    state := k.GetTransitionState(ctx)
    return state.MaintenanceMode
}
```

## BeginBlocker

The BeginBlocker handles automatic season transitions:

```go
func (k Keeper) BeginBlocker(ctx sdk.Context) {
    season := k.GetCurrentSeason(ctx)
    params := k.GetParams(ctx)

    switch season.Status {
    case SeasonStatus_ACTIVE:
        // Periodic cleanup of expired guild invites (batched to avoid gas spikes)
        // Only run every invite_cleanup_interval blocks
        if ctx.BlockHeight() % int64(params.InviteCleanupIntervalBlocks) == 0 {
            k.CleanupExpiredGuildInvitesBatch(ctx, int(params.InviteCleanupBatchSize))
        }

        // Check if nomination window should open
        nominationStartBlock := season.EndBlock - k.EpochToBlock(ctx, int64(params.NominationWindowEpochs))
        if ctx.BlockHeight() >= nominationStartBlock {
            season.Status = SeasonStatus_NOMINATION
            k.SetSeason(ctx, season)
            k.OpenNominationWindow(ctx)

            ctx.EventManager().EmitEvent(
                sdk.NewEvent("nomination_window_opened",
                    sdk.NewAttribute("season_number", fmt.Sprintf("%d", season.Number)),
                    sdk.NewAttribute("closes_at_block", fmt.Sprintf("%d", season.EndBlock)),
                ),
            )
        }

    case SeasonStatus_NOMINATION:
        // Nomination window is open — members can nominate and stake
        // Check if season should end (nomination window closes, transition begins)
        if ctx.BlockHeight() >= season.EndBlock {
            season.Status = SeasonStatus_ENDING
            k.SetSeason(ctx, season)
            k.InitializeTransitionState(ctx)

            ctx.EventManager().EmitEvent(
                sdk.NewEvent("season_transition_started",
                    sdk.NewAttribute("season_number", fmt.Sprintf("%d", season.Number)),
                ),
            )
        }

    case SeasonStatus_ENDING:
        // Check if in recovery mode (requires governance intervention)
        recovery := k.GetTransitionRecoveryState(ctx)
        if recovery.RecoveryMode {
            // Stop processing until governance resolves the issue
            return
        }

        // Process transition batch
        done, err := k.ProcessSeasonTransitionBatch(ctx, int(params.TransitionBatchSize))
        if err != nil {
            k.Logger(ctx).Error("season transition error", "error", err)
            k.HandleTransitionError(ctx, err)
            return
        }

        if done {
            // Transition complete - start new season
            name, theme, isSet := k.GetNextSeasonInfo(ctx)
            if !isSet {
                name = fmt.Sprintf("Season %d", season.Number+1)
                theme = "New Beginnings"
            }

            if err := k.StartNewSeason(ctx, name, theme); err != nil {
                k.Logger(ctx).Error("failed to start new season", "error", err)
            }

            // Clear next season info
            k.DeleteNextSeasonInfo(ctx)
        }
    }
}
```

## Transition Recovery

The season transition can fail mid-process due to various reasons (unexpected state, bugs, resource exhaustion). The following mechanisms handle stuck transitions:

### TransitionRecoveryState

```go
type TransitionRecoveryState struct {
    LastAttemptBlock   int64
    FailedPhase        TransitionPhase
    FailureCount       uint32
    LastError          string
    RecoveryMode       bool
}
```

### Recovery Functions

```go
// AbortTransition allows governance to cancel a stuck transition
// This rolls back to the previous stable state (season remains ACTIVE)
func (k Keeper) AbortTransition(ctx sdk.Context) error {
    state := k.GetTransitionState(ctx)
    if state.Phase == PhaseComplete {
        return ErrNoActiveTransition
    }

    season := k.GetCurrentSeason(ctx)

    // Cannot abort after reputation has been archived (data loss)
    if state.Phase > PhaseSnapshot {
        return ErrTransitionTooFarToAbort
    }

    // Reset season to active
    season.Status = SeasonStatus_ACTIVE
    // Extend season to allow governance to address the issue
    season.EndBlock = ctx.BlockHeight() + int64(k.GetParams(ctx).TransitionGracePeriod)
    k.SetSeason(ctx, season)

    // Clear transition state
    k.DeleteTransitionState(ctx)
    k.DeleteTransitionRecoveryState(ctx)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("season_transition_aborted",
            sdk.NewAttribute("season_number", fmt.Sprintf("%d", season.Number)),
            sdk.NewAttribute("aborted_at_phase", fmt.Sprintf("%d", state.Phase)),
        ),
    )

    return nil
}

// RetryTransitionPhase retries the current phase after a failure
func (k Keeper) RetryTransitionPhase(ctx sdk.Context) error {
    state := k.GetTransitionState(ctx)
    if state.Phase == PhaseComplete {
        return ErrNoActiveTransition
    }

    recovery := k.GetTransitionRecoveryState(ctx)
    if !recovery.RecoveryMode {
        return ErrNotInRecoveryMode
    }

    // Reset processed count for current phase
    state.ProcessedCount = 0
    state.LastProcessed = ""
    k.SetTransitionState(ctx, state)

    // Clear recovery mode
    recovery.RecoveryMode = false
    k.SetTransitionRecoveryState(ctx, recovery)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("season_transition_retry",
            sdk.NewAttribute("phase", fmt.Sprintf("%d", state.Phase)),
        ),
    )

    return nil
}

// SkipTransitionPhase skips the current phase (governance emergency action)
// Should only be used when a phase is fundamentally broken and cannot proceed
func (k Keeper) SkipTransitionPhase(ctx sdk.Context) error {
    state := k.GetTransitionState(ctx)
    if state.Phase == PhaseComplete {
        return ErrNoActiveTransition
    }

    // Cannot skip critical phases
    if state.Phase == PhaseArchiveReputation || state.Phase == PhaseResetReputation {
        return ErrCannotSkipCriticalPhase
    }

    skippedPhase := state.Phase
    state.Phase++
    state.ProcessedCount = 0
    state.LastProcessed = ""
    k.SetTransitionState(ctx, state)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("season_transition_phase_skipped",
            sdk.NewAttribute("skipped_phase", fmt.Sprintf("%d", skippedPhase)),
            sdk.NewAttribute("new_phase", fmt.Sprintf("%d", state.Phase)),
        ),
    )

    return nil
}

// HandleTransitionError is called when a transition batch fails
func (k Keeper) HandleTransitionError(ctx sdk.Context, err error) {
    state := k.GetTransitionState(ctx)
    recovery := k.GetTransitionRecoveryState(ctx)

    recovery.LastAttemptBlock = ctx.BlockHeight()
    recovery.FailedPhase = state.Phase
    recovery.FailureCount++
    recovery.LastError = err.Error()

    params := k.GetParams(ctx)

    // Enter recovery mode after max retries exceeded
    if recovery.FailureCount >= params.TransitionMaxRetries {
        recovery.RecoveryMode = true
        state.MaintenanceMode = true // Keep maintenance mode on
        k.SetTransitionState(ctx, state)

        ctx.EventManager().EmitEvent(
            sdk.NewEvent("season_transition_stuck",
                sdk.NewAttribute("phase", fmt.Sprintf("%d", state.Phase)),
                sdk.NewAttribute("failure_count", fmt.Sprintf("%d", recovery.FailureCount)),
                sdk.NewAttribute("error", recovery.LastError),
            ),
        )
    }

    k.SetTransitionRecoveryState(ctx, recovery)
}
```

### Parameters for Recovery

```protobuf
// Added to Params
uint32 transition_grace_period = 37;   // Blocks to add when aborting (default: 50400 = ~1 week)
uint32 transition_max_retries = 38;    // Max automatic retries before entering recovery mode (default: 3)
```

### Governance Messages for Recovery

```protobuf
message MsgAbortSeasonTransition {
    option (cosmos.msg.v1.signer) = "authority";
    string authority = 1;
}

message MsgRetrySeasonTransition {
    option (cosmos.msg.v1.signer) = "authority";
    string authority = 1;
}

message MsgSkipTransitionPhase {
    option (cosmos.msg.v1.signer) = "authority";
    string authority = 1;
}
```

## Error Types

```go
var (
    // Member errors
    ErrNotAMember          = errorsmod.Register(ModuleName, 2, "address is not a member")
    ErrMemberNotFound      = errorsmod.Register(ModuleName, 3, "member not found")

    // Guild errors
    ErrAlreadyInGuild      = errorsmod.Register(ModuleName, 10, "already in a guild")
    ErrNotInGuild          = errorsmod.Register(ModuleName, 11, "not in a guild")
    ErrGuildNotFound       = errorsmod.Register(ModuleName, 12, "guild not found")
    ErrNotGuildFounder     = errorsmod.Register(ModuleName, 13, "not the guild founder")
    ErrNotGuildOfficer     = errorsmod.Register(ModuleName, 14, "not a guild officer")
    ErrGuildTooYoung       = errorsmod.Register(ModuleName, 15, "guild too young to dissolve")
    ErrGuildNameTaken      = errorsmod.Register(ModuleName, 16, "guild name already taken")
    ErrGuildFull           = errorsmod.Register(ModuleName, 17, "guild is at max capacity")
    ErrGuildInviteOnly     = errorsmod.Register(ModuleName, 21, "guild is invite-only")
    ErrNotInvitedToGuild   = errorsmod.Register(ModuleName, 22, "not invited to this guild")
    ErrAlreadyInvited      = errorsmod.Register(ModuleName, 23, "already invited to this guild")
    ErrTooManyInvites      = errorsmod.Register(ModuleName, 24, "guild has too many pending invites")
    ErrGuildHopCooldown    = errorsmod.Register(ModuleName, 25, "must wait before joining another guild")
    ErrMaxGuildsPerSeason  = errorsmod.Register(ModuleName, 26, "joined max guilds this season")
    ErrAlreadyOfficer      = errorsmod.Register(ModuleName, 27, "already an officer")
    ErrNotAnOfficer        = errorsmod.Register(ModuleName, 28, "not an officer")
    ErrCannotDemoteFounder = errorsmod.Register(ModuleName, 29, "cannot demote the founder")
    ErrCannotKickFounder   = errorsmod.Register(ModuleName, 30, "cannot kick the founder")
    ErrCannotKickOfficer   = errorsmod.Register(ModuleName, 31, "officers cannot kick other officers")
    ErrTooManyOfficers     = errorsmod.Register(ModuleName, 32, "guild has max officers")
    ErrGuildNotFrozen      = errorsmod.Register(ModuleName, 33, "guild is not frozen")
    ErrGuildDescriptionTooLong = errorsmod.Register(ModuleName, 34, "guild description exceeds max length")
    ErrGuildInviteExpired  = errorsmod.Register(ModuleName, 35, "guild invite has expired")

    // Display name errors (non-unique, cosmetic)
    ErrDisplayNameTooShort    = errorsmod.Register(ModuleName, 40, "display name too short")
    ErrDisplayNameTooLong     = errorsmod.Register(ModuleName, 41, "display name too long")
    ErrDisplayNameInvalidChars = errorsmod.Register(ModuleName, 42, "display name contains invalid characters")
    ErrDisplayNameCooldown    = errorsmod.Register(ModuleName, 44, "must wait before changing display name")
    ErrDisplayNameRejected    = errorsmod.Register(ModuleName, 53, "display name was rejected by moderation")
    ErrNoDisplayNameToReport  = errorsmod.Register(ModuleName, 54, "target has no display name to report")
    ErrNoActiveModeration     = errorsmod.Register(ModuleName, 55, "no active moderation to appeal")
    ErrAppealPeriodExpired    = errorsmod.Register(ModuleName, 56, "appeal period has expired")
    ErrAppealAlreadyPending   = errorsmod.Register(ModuleName, 57, "appeal is already pending")

    // Username errors (unique, reserved via x/name)
    ErrUsernameTooShort       = errorsmod.Register(ModuleName, 48, "username too short")
    ErrUsernameTooLong        = errorsmod.Register(ModuleName, 49, "username too long")
    ErrUsernameInvalidChars   = errorsmod.Register(ModuleName, 50, "username contains invalid characters")
    ErrUsernameTaken          = errorsmod.Register(ModuleName, 51, "username already taken")
    ErrUsernameCooldown       = errorsmod.Register(ModuleName, 52, "must wait before changing username")
    ErrInsufficientDREAM      = errorsmod.Register(ModuleName, 47, "insufficient DREAM balance")

    // Title errors
    ErrTitleNotUnlocked    = errorsmod.Register(ModuleName, 45, "title not unlocked")
    ErrTitleNotFound       = errorsmod.Register(ModuleName, 46, "title not found")

    // Achievement errors
    ErrAchievementNotFound = errorsmod.Register(ModuleName, 58, "achievement not found")
    ErrAchievementAlreadyEarned = errorsmod.Register(ModuleName, 59, "achievement already earned")

    // Quest errors
    ErrQuestNotFound       = errorsmod.Register(ModuleName, 60, "quest not found")
    ErrQuestNotActive      = errorsmod.Register(ModuleName, 61, "quest is not active")
    ErrQuestAlreadyStarted = errorsmod.Register(ModuleName, 62, "quest already started")
    ErrQuestNotStarted     = errorsmod.Register(ModuleName, 63, "quest not started")
    ErrQuestNotCompleted   = errorsmod.Register(ModuleName, 64, "quest objectives not completed")
    ErrQuestOnCooldown     = errorsmod.Register(ModuleName, 65, "quest is on cooldown")
    ErrQuestLevelTooLow    = errorsmod.Register(ModuleName, 66, "level too low for this quest")
    ErrQuestMissingAchievement = errorsmod.Register(ModuleName, 67, "required achievement not earned")
    ErrQuestAlreadyCompleted = errorsmod.Register(ModuleName, 68, "quest already completed")
    ErrQuestPrerequisiteNotMet = errorsmod.Register(ModuleName, 69, "prerequisite quest not completed")
    ErrQuestCycleDetected  = errorsmod.Register(ModuleName, 70, "quest prerequisite chain contains a cycle")
    ErrQuestPrerequisiteNotFound = errorsmod.Register(ModuleName, 71, "prerequisite quest does not exist")
    ErrTooManyObjectives   = errorsmod.Register(ModuleName, 72, "quest has too many objectives")
    ErrQuestRewardTooHigh  = errorsmod.Register(ModuleName, 73, "quest XP reward exceeds maximum")
    ErrTooManyActiveQuests = errorsmod.Register(ModuleName, 74, "member has too many active quests")
    ErrInvalidObjectiveTarget = errorsmod.Register(ModuleName, 77, "objective target value out of valid range")
    ErrUnknownObjectiveType   = errorsmod.Register(ModuleName, 78, "unknown quest objective type")
    ErrInvalidObjectiveDescription = errorsmod.Register(ModuleName, 79, "objective description invalid or too long")

    // XP errors
    ErrXPCapReached        = errorsmod.Register(ModuleName, 75, "XP cap reached for this epoch")
    ErrXPOverflow          = errorsmod.Register(ModuleName, 76, "XP would overflow maximum value")

    // Season errors
    ErrSeasonNotActive     = errorsmod.Register(ModuleName, 80, "season is not active")
    ErrSeasonTransitioning = errorsmod.Register(ModuleName, 81, "season is transitioning")
    ErrMaxExtensions       = errorsmod.Register(ModuleName, 82, "max season extensions reached")
    ErrExtensionTooLong    = errorsmod.Register(ModuleName, 83, "extension exceeds max allowed")

    // Transition recovery errors
    ErrNoActiveTransition      = errorsmod.Register(ModuleName, 90, "no active season transition")
    ErrTransitionTooFarToAbort = errorsmod.Register(ModuleName, 91, "transition has passed point of safe abort")
    ErrNotInRecoveryMode       = errorsmod.Register(ModuleName, 92, "transition is not in recovery mode")
    ErrCannotSkipCriticalPhase = errorsmod.Register(ModuleName, 93, "cannot skip critical transition phase")

    // Nomination errors (retroactive public goods funding)
    ErrNominationWindowClosed     = errorsmod.Register(ModuleName, 100, "nomination window is not open")
    ErrNominationNotFound         = errorsmod.Register(ModuleName, 101, "nomination not found")
    ErrContentAlreadyNominated    = errorsmod.Register(ModuleName, 102, "content already nominated this season")
    ErrMaxNominationsReached      = errorsmod.Register(ModuleName, 103, "max nominations per member reached")
    ErrInvalidContentRef          = errorsmod.Register(ModuleName, 104, "invalid content reference format")
    ErrContentNotFound            = errorsmod.Register(ModuleName, 105, "nominated content not found")
    ErrContentNotActive           = errorsmod.Register(ModuleName, 106, "nominated content is not active")
    ErrContentNotFromThisSeason   = errorsmod.Register(ModuleName, 107, "content was not created during this season")
    ErrContentCreatorNotMember    = errorsmod.Register(ModuleName, 108, "content creator is not an active member")
    ErrCannotStakeOwnNomination   = errorsmod.Register(ModuleName, 109, "cannot stake on nomination for own content")
    ErrNominationStakeNotFound    = errorsmod.Register(ModuleName, 110, "no active stake on this nomination")
    ErrNominationStakeTooLow      = errorsmod.Register(ModuleName, 111, "stake amount below minimum")
    ErrInsufficientTrustLevel     = errorsmod.Register(ModuleName, 112, "trust level too low for this action")
    ErrRationaleTooLong           = errorsmod.Register(ModuleName, 113, "nomination rationale exceeds max length")
)
```

## Quest Validation

Quests with `prerequisite_quest` fields must be validated to prevent cycles (A→B→C→A) that would make quests impossible to complete.

```go
// ValidateQuestChain checks that a quest's prerequisite chain is acyclic
// Called when creating a new quest via MsgCreateQuest
func (k Keeper) ValidateQuestChain(ctx sdk.Context, quest Quest) error {
    if quest.PrerequisiteQuest == "" {
        return nil // No prerequisites, no cycle possible
    }

    // Track visited quests to detect cycles
    visited := make(map[string]bool)
    visited[quest.Id] = true // Include the new quest itself

    current := quest.PrerequisiteQuest
    for current != "" {
        // Check for cycle
        if visited[current] {
            return ErrQuestCycleDetected
        }
        visited[current] = true

        // Get the prerequisite quest
        prereq, err := k.GetQuest(ctx, current)
        if err != nil {
            return ErrQuestPrerequisiteNotFound
        }

        // Move to next prerequisite in chain
        current = prereq.PrerequisiteQuest
    }

    return nil
}

// CreateQuest validates and creates a new quest
func (k Keeper) CreateQuest(ctx sdk.Context, quest Quest) (string, error) {
    // Validate prerequisite chain for cycles
    if err := k.ValidateQuestChain(ctx, quest); err != nil {
        return "", err
    }

    // Validate objectives and rewards
    params := k.GetParams(ctx)
    if uint32(len(quest.Objectives)) > params.MaxQuestObjectives {
        return "", ErrTooManyObjectives
    }
    if quest.XpReward > params.MaxQuestXpReward {
        return "", ErrQuestRewardTooHigh
    }

    // Validate each objective type and target
    for i, obj := range quest.Objectives {
        if err := k.ValidateQuestObjective(ctx, obj); err != nil {
            return "", sdkerrors.Wrapf(err, "objective %d", i)
        }
    }

    // Generate quest ID if not provided
    if quest.Id == "" {
        quest.Id = k.GenerateQuestID(ctx)
    }

    // Validate chain_id doesn't create issues
    if quest.ChainId != "" {
        // Verify all quests in chain exist (except this one)
        // This is a soft check - chains are for UI grouping only
    }

    k.SetQuest(ctx, quest)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("quest_created",
            sdk.NewAttribute("quest_id", quest.Id),
            sdk.NewAttribute("name", quest.Name),
        ),
    )

    return quest.Id, nil
}

// ValidateQuestObjective ensures objective type is valid and target is reasonable
func (k Keeper) ValidateQuestObjective(ctx sdk.Context, obj QuestObjective) error {
    params := k.GetParams(ctx)

    // Validate objective type is known
    switch obj.Type {
    case QUEST_OBJECTIVE_VOTES_CAST:
        // Target: number of votes (reasonable range: 1-1000)
        if obj.Target == 0 || obj.Target > 1000 {
            return ErrInvalidObjectiveTarget
        }

    case QUEST_OBJECTIVE_FORUM_HELPFUL:
        // Target: number of helpful marks (reasonable range: 1-500)
        if obj.Target == 0 || obj.Target > 500 {
            return ErrInvalidObjectiveTarget
        }

    case QUEST_OBJECTIVE_INVITEE_MILESTONE:
        // Target: number of invitees reaching milestone (reasonable range: 1-100)
        if obj.Target == 0 || obj.Target > 100 {
            return ErrInvalidObjectiveTarget
        }

    case QUEST_OBJECTIVE_INITIATIVES_COMPLETED:
        // Target: number of initiatives (reasonable range: 1-100)
        if obj.Target == 0 || obj.Target > 100 {
            return ErrInvalidObjectiveTarget
        }

    default:
        // Unknown objective type - reject to prevent future incompatibility
        return ErrUnknownObjectiveType
    }

    // Validate description length
    if len(obj.Description) == 0 || len(obj.Description) > int(params.MaxObjectiveDescriptionLength) {
        return ErrInvalidObjectiveDescription
    }

    return nil
}
```

## Quest Prerequisites

```go
// CanStartQuest checks if a member meets quest prerequisites
func (k Keeper) CanStartQuest(ctx sdk.Context, member sdk.AccAddress, quest Quest) error {
    params := k.GetParams(ctx)

    // Check max active quests limit
    inProgressQuests := k.GetMemberInProgressQuests(ctx, member)
    if uint32(len(inProgressQuests)) >= params.MaxActiveQuestsPerMember {
        return ErrTooManyActiveQuests
    }

    // Check quest is active
    if !quest.Active {
        return ErrQuestNotActive
    }

    // Check time bounds
    if quest.StartBlock > 0 && ctx.BlockHeight() < quest.StartBlock {
        return ErrQuestNotActive
    }
    if quest.EndBlock > 0 && ctx.BlockHeight() > quest.EndBlock {
        return ErrQuestNotActive
    }

    // Check level prerequisite
    if quest.MinLevel > 0 {
        profile := k.GetMemberProfile(ctx, member)
        if profile.SeasonLevel < quest.MinLevel {
            return ErrQuestLevelTooLow
        }
    }

    // Check achievement prerequisite
    if quest.RequiredAchievement != "" {
        if !k.HasAchievement(ctx, member, quest.RequiredAchievement) {
            return ErrQuestMissingAchievement
        }
    }

    // Check prerequisite quest
    if quest.PrerequisiteQuest != "" {
        prereqProgress, found := k.GetMemberQuestProgressIfExists(ctx, member, quest.PrerequisiteQuest)
        if !found || !prereqProgress.Completed {
            return ErrQuestPrerequisiteNotMet
        }
    }

    // Check if already started (non-repeatable)
    progress, found := k.GetMemberQuestProgressIfExists(ctx, member, quest.Id)
    if found {
        if !quest.Repeatable {
            // Non-repeatable quest: can only start once (unless progress was deleted)
            return ErrQuestAlreadyStarted
        }

        // Check cooldown for repeatable quests
        if quest.CooldownEpochs > 0 {
            currentEpoch := k.GetCurrentEpoch(ctx)
            var cooldownStartEpoch int64

            if progress.Completed {
                // Completed quest: cooldown from completion
                cooldownStartEpoch = k.BlockToEpoch(ctx, progress.CompletedBlock)
            } else if progress.LastAttemptBlock > 0 {
                // Abandoned quest: cooldown from abandonment
                cooldownStartEpoch = k.BlockToEpoch(ctx, progress.LastAttemptBlock)
            } else {
                // Quest in progress - can't start again
                return ErrQuestAlreadyStarted
            }

            if currentEpoch-cooldownStartEpoch < int64(quest.CooldownEpochs) {
                return ErrQuestOnCooldown
            }
        } else if !progress.Completed {
            // No cooldown, but quest is in progress
            return ErrQuestAlreadyStarted
        }
    }

    return nil
}

// GetAvailableQuests returns quests the member can start
func (k Keeper) GetAvailableQuests(ctx sdk.Context, member sdk.AccAddress) []Quest {
    allQuests := k.GetActiveQuests(ctx)
    available := make([]Quest, 0)

    for _, quest := range allQuests {
        if err := k.CanStartQuest(ctx, member, quest); err == nil {
            available = append(available, quest)
        }
    }

    return available
}

// AbandonQuest allows a member to abandon an in-progress quest
func (k Keeper) AbandonQuest(ctx sdk.Context, member sdk.AccAddress, questID string) error {
    progress, err := k.GetMemberQuestProgress(ctx, member, questID)
    if err != nil {
        return ErrQuestNotStarted
    }

    if progress.Completed {
        return ErrQuestAlreadyCompleted
    }

    quest, err := k.GetQuest(ctx, questID)
    if err != nil {
        return err
    }

    // For repeatable quests, abandonment triggers cooldown
    if quest.Repeatable && quest.CooldownEpochs > 0 {
        progress.Completed = false
        progress.LastAttemptBlock = ctx.BlockHeight()
        k.SetMemberQuestProgress(ctx, progress)
    } else {
        // For non-repeatable quests, just delete progress to allow restart
        k.DeleteMemberQuestProgress(ctx, member, questID)
    }

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("quest_abandoned",
            sdk.NewAttribute("member", member.String()),
            sdk.NewAttribute("quest_id", questID),
        ),
    )

    return nil
}

// HandleSeasonalQuestExpiration handles quests when their season ends
// Called during season transition for seasonal quests
func (k Keeper) HandleSeasonalQuestExpiration(ctx sdk.Context, season uint32) error {
    // Get all quests for this season
    quests := k.GetQuestsBySeason(ctx, season)

    for _, quest := range quests {
        // Deactivate the quest
        quest.Active = false
        k.SetQuest(ctx, quest)

        // Clear all in-progress quest progress for this quest
        // Completed progress is kept for historical records
        k.ClearIncompleteQuestProgress(ctx, quest.Id)

        ctx.EventManager().EmitEvent(
            sdk.NewEvent("seasonal_quest_expired",
                sdk.NewAttribute("quest_id", quest.Id),
                sdk.NewAttribute("season", fmt.Sprintf("%d", season)),
            ),
        )
    }

    return nil
}
```

## Default Achievements

```go
var DefaultAchievements = []Achievement{
    // Contribution
    {ID: "first_initiative", Name: "First Steps", Description: "Complete your first initiative", Rarity: Rarity_Common, XPReward: 10},
    {ID: "ten_initiatives", Name: "Getting Started", Description: "Complete 10 initiatives", Rarity: Rarity_Common, XPReward: 25},
    {ID: "centurion", Name: "Centurion", Description: "Complete 100 initiatives", Rarity: Rarity_Rare, XPReward: 100},
    {ID: "polymath", Name: "Polymath", Description: "Earn reputation in 5 different domains", Rarity: Rarity_Epic, XPReward: 150},

    // Social
    {ID: "welcomer", Name: "Welcome Committee", Description: "Have an invitee complete their first initiative", Rarity: Rarity_Common, XPReward: 25},
    {ID: "talent_scout", Name: "Talent Scout", Description: "Have 5 invitees reach Established", Rarity: Rarity_Rare, XPReward: 100},
    {ID: "dynasty", Name: "Dynasty Builder", Description: "Have 3 generations of invitees", Rarity: Rarity_Epic, XPReward: 200},

    // Governance
    {ID: "voter", Name: "Voice of the People", Description: "Cast 50 votes", Rarity: Rarity_Uncommon, XPReward: 50},
    {ID: "proposer", Name: "Agenda Setter", Description: "Create 10 proposals that reach deposit", Rarity: Rarity_Rare, XPReward: 75},

    // Accountability
    {ID: "watchdog", Name: "Watchdog", Description: "Submit a successful challenge", Rarity: Rarity_Rare, XPReward: 75},
    {ID: "juror", Name: "Twelve Angry Members", Description: "Serve on 12 juries", Rarity: Rarity_Rare, XPReward: 100},

    // Forum
    {ID: "helpful", Name: "Helpful", Description: "Have 10 posts marked helpful", Rarity: Rarity_Common, XPReward: 30},
    {ID: "sage", Name: "Sage", Description: "Have 100 posts marked helpful", Rarity: Rarity_Rare, XPReward: 100},

    // Comeback
    {ID: "phoenix", Name: "Phoenix", Description: "Return from zeroing and reach Established", Rarity: Rarity_Legendary, XPReward: 300},
}
```

## Default Titles

```go
var DefaultTitles = []Title{
    // Progression (level-based)
    {ID: "newcomer", Name: "Newcomer", Rarity: Rarity_Common},
    {ID: "contributor", Name: "Contributor", Rarity: Rarity_Common},
    {ID: "artisan", Name: "Artisan", Rarity: Rarity_Uncommon},
    {ID: "master_builder", Name: "Master Builder", Rarity: Rarity_Rare},
    {ID: "architect", Name: "Architect", Rarity: Rarity_Epic},

    // Achievement-based
    {ID: "talent_scout", Name: "Talent Scout", Rarity: Rarity_Rare},
    {ID: "sentinel", Name: "Sentinel", Rarity: Rarity_Rare},
    {ID: "sage", Name: "Sage", Rarity: Rarity_Rare},

    // Seasonal (awarded at season end, never return)
    {ID: "champion", Name: "Champion", Rarity: Rarity_Legendary, Seasonal: true},
    {ID: "rising_star", Name: "Rising Star", Rarity: Rarity_Epic, Seasonal: true},
}
```

## XP and Level Configuration

```go
var DefaultLevelThresholds = []uint64{
    0,        // Level 1
    100,      // Level 2
    250,      // Level 3
    500,      // Level 4
    1000,     // Level 5
    2000,     // Level 6
    3500,     // Level 7
    5500,     // Level 8
    8000,     // Level 9
    11000,    // Level 10
    15000,    // Level 11
    20000,    // Level 12
    26000,    // Level 13
    33000,    // Level 14
    41000,    // Level 15
    50000,    // Level 16 (max)
}

var DefaultXPRewards = map[XPSource]uint64{
    XP_SOURCE_VOTE_CAST:               15,
    XP_SOURCE_PROPOSAL_CREATED:        50,
    XP_SOURCE_FORUM_REPLY:             5,
    XP_SOURCE_FORUM_HELPFUL:           15,
    XP_SOURCE_INVITEE_FIRST_INITIATIVE: 30,
    XP_SOURCE_INVITEE_ESTABLISHED:     50,
    // Achievement and quest rewards vary by definition
}

var DefaultXPCaps = struct {
    MaxVoteXPPerEpoch  uint32
    MaxForumXPPerEpoch uint64
    MaxXPPerEpoch      uint64
}{
    MaxVoteXPPerEpoch:  10,   // 10 proposals * 15 = 150 XP max from voting
    MaxForumXPPerEpoch: 50,   // Prevents forum farming
    MaxXPPerEpoch:      500,  // Global cap
}
```

## Display Name and Username

Members have two identity fields:

| Field | Example | Unique? | Reserved via x/name? | Cost |
|-------|---------|---------|---------------------|------|
| **Display Name** | "Crypto Enthusiast 🚀" | No | No | Free |
| **Username** | "alice" | Yes | Yes | DREAM (optional) |

### Display Name (Non-Unique, Cosmetic)

Display names are free-form cosmetic names with basic validation only.

```go
func (k Keeper) SetDisplayName(ctx sdk.Context, member sdk.AccAddress, name string) error {
    params := k.GetParams(ctx)
    profile := k.GetMemberProfile(ctx, member)

    // Check cooldown
    currentEpoch := k.GetCurrentEpoch(ctx)
    if profile.LastDisplayNameChangeEpoch > 0 &&
       currentEpoch-profile.LastDisplayNameChangeEpoch < int64(params.DisplayNameChangeCooldownEpochs) {
        return ErrDisplayNameCooldown
    }

    // Validate length (no uniqueness check - display names are NOT unique)
    if len(name) < int(params.DisplayNameMinLength) {
        return ErrDisplayNameTooShort
    }
    if len(name) > int(params.DisplayNameMaxLength) {
        return ErrDisplayNameTooLong
    }

    // Basic character validation (no control characters, etc.)
    if !isValidDisplayName(name) {
        return ErrDisplayNameInvalidChars
    }

    // Check if this name was previously rejected for this member
    if err := k.CheckDisplayNameModeration(ctx, member, name); err != nil {
        return err
    }

    // Update profile (no x/name reservation needed)
    profile.DisplayName = name
    profile.LastDisplayNameChangeEpoch = currentEpoch
    k.SetMemberProfile(ctx, profile)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("display_name_set",
            sdk.NewAttribute("member", member.String()),
            sdk.NewAttribute("name", name),
        ),
    )

    return nil
}

// isValidDisplayName checks for control characters and other invalid content
func isValidDisplayName(name string) bool {
    for _, r := range name {
        // Allow letters, numbers, spaces, punctuation, emoji
        // Reject control characters (0x00-0x1F, 0x7F)
        if r < 0x20 || r == 0x7F {
            return false
        }
    }
    return true
}

// CheckDisplayNameModeration checks if a display name was previously moderated
func (k Keeper) CheckDisplayNameModeration(ctx sdk.Context, member sdk.AccAddress, name string) error {
    moderation := k.GetDisplayNameModeration(ctx, member)
    if moderation.RejectedName == name && moderation.Active {
        return ErrDisplayNameRejected
    }
    return nil
}
```

### Display Name Community Moderation

Display names are not pre-moderated - offensive content is handled through community reporting and jury review via x/rep's challenge system.

**Flow:**
1. Member sets display name (any printable characters allowed)
2. If offensive, another member reports via `MsgReportDisplayName`
3. Report triggers a jury challenge in x/rep
4. If jury upholds report, display name is forcibly cleared with moderation reason
5. Member can set a new display name but the rejected name is blocked for that member

```protobuf
message MsgReportDisplayName {
    string creator = 1;       // Member reporting the display name
    string target = 2;        // Member with offensive display name
    string reason = 3;        // Why the name is offensive
    // Reporter must stake display_name_report_stake_dream (from params)
    // Stake is burned if report fails (frivolous report penalty)
    // Stake is returned if report is upheld by jury
}

message MsgReportDisplayNameResponse {}

// Appeal a display name moderation decision
message MsgAppealDisplayNameModeration {
    string creator = 1;        // Member appealing the moderation
    string appeal_reason = 2;  // Why the moderation was incorrect
    // Appellant must stake display_name_appeal_stake_dream (from params)
    // Stake is burned if appeal fails, returned if appeal succeeds
}

message MsgAppealDisplayNameModerationResponse {}

// Resolve a display name appeal (governance-gated)
message MsgResolveDisplayNameAppeal {
    string authority = 1;
    string member = 2;
    bool appeal_succeeded = 3;
}

message MsgResolveDisplayNameAppealResponse {}

// Resolve an unappealed display name moderation (governance-gated)
message MsgResolveUnappealedModeration {
    string authority = 1;
    string member = 2;
}

message MsgResolveUnappealedModerationResponse {}
```

```go
// ReportDisplayName creates a challenge for jury review of an offensive display name
// Reporter must stake DREAM which is burned if report fails (anti-spam)
func (k Keeper) ReportDisplayName(ctx sdk.Context, reporter, target sdk.AccAddress, reason string) (string, error) {
    params := k.GetParams(ctx)
    targetProfile := k.GetMemberProfile(ctx, target)

    if targetProfile.DisplayName == "" {
        return "", ErrNoDisplayNameToReport
    }

    // Escrow reporter's stake (returned if report upheld, burned if report fails)
    if params.DisplayNameReportStakeDream.IsPositive() {
        if err := k.repKeeper.EscrowDREAM(ctx, reporter, params.DisplayNameReportStakeDream); err != nil {
            return "", ErrInsufficientDREAM
        }
    }

    // Create a challenge via x/rep for jury review
    // ChallengeType_DISPLAY_NAME is a special challenge type for name moderation
    challengeID, err := k.repKeeper.CreateChallenge(ctx, reporter, target, ChallengeType_DISPLAY_NAME, reason)
    if err != nil {
        // Refund stake on failure
        k.repKeeper.RefundEscrowedDREAM(ctx, reporter, params.DisplayNameReportStakeDream)
        return "", err
    }

    // Store stake info for resolution
    k.SetDisplayNameReportStake(ctx, challengeID, reporter, params.DisplayNameReportStakeDream)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("display_name_reported",
            sdk.NewAttribute("reporter", reporter.String()),
            sdk.NewAttribute("target", target.String()),
            sdk.NewAttribute("display_name", targetProfile.DisplayName),
            sdk.NewAttribute("challenge_id", challengeID),
            sdk.NewAttribute("stake_amount", params.DisplayNameReportStakeDream.String()),
        ),
    )

    return challengeID, nil
}

// OnDisplayNameChallengeResolved is called by x/rep when a display name challenge is resolved
func (k Keeper) OnDisplayNameChallengeResolved(ctx sdk.Context, challengeID string, reporter, target sdk.AccAddress, reporterWon bool, reason string) {
    // Handle reporter stake
    stake := k.GetDisplayNameReportStake(ctx, challengeID)
    if stake.Amount.IsPositive() {
        if reporterWon {
            // Report upheld: return stake to reporter
            k.repKeeper.RefundEscrowedDREAM(ctx, reporter, stake.Amount)
        } else {
            // Report failed: burn stake (frivolous report penalty)
            k.repKeeper.BurnEscrowedDREAM(ctx, reporter, stake.Amount)
        }
        k.DeleteDisplayNameReportStake(ctx, challengeID)
    }

    if !reporterWon {
        return // Challenge failed, display name is fine
    }

    profile := k.GetMemberProfile(ctx, target)
    rejectedName := profile.DisplayName

    // Clear the display name
    profile.DisplayName = ""
    k.SetMemberProfile(ctx, profile)

    // Record moderation so the member can't reuse the same name
    moderation := DisplayNameModeration{
        Member:       target.String(),
        RejectedName: rejectedName,
        Reason:       reason,
        ModeratedAt:  ctx.BlockHeight(),
        Active:       true,
    }
    k.SetDisplayNameModeration(ctx, moderation)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("display_name_moderated",
            sdk.NewAttribute("member", target.String()),
            sdk.NewAttribute("rejected_name", rejectedName),
            sdk.NewAttribute("reason", reason),
            sdk.NewAttribute("challenge_id", challengeID),
        ),
    )
}

// AppealDisplayNameModeration allows a member to appeal a moderation decision
// The appeal triggers a new jury challenge in x/rep
func (k Keeper) AppealDisplayNameModeration(ctx sdk.Context, member sdk.AccAddress, appealReason string) (string, error) {
    params := k.GetParams(ctx)
    moderation := k.GetDisplayNameModeration(ctx, member)

    // Check if there's an active moderation to appeal
    if !moderation.Active {
        return "", ErrNoActiveModeration
    }

    // Check if appeal period has passed
    appealDeadline := moderation.ModeratedAt + int64(params.DisplayNameAppealPeriodBlocks)
    if ctx.BlockHeight() > appealDeadline {
        return "", ErrAppealPeriodExpired
    }

    // Check not already appealing
    if moderation.AppealChallengeId != "" {
        existingChallenge, err := k.repKeeper.GetChallenge(ctx, moderation.AppealChallengeId)
        if err == nil && existingChallenge.Status == ChallengeStatus_PENDING {
            return "", ErrAppealAlreadyPending
        }
    }

    // Escrow appellant's stake
    if params.DisplayNameAppealStakeDream.IsPositive() {
        if err := k.repKeeper.EscrowDREAM(ctx, member, params.DisplayNameAppealStakeDream); err != nil {
            return "", ErrInsufficientDREAM
        }
    }

    // Create appeal challenge via x/rep
    // ChallengeType_DISPLAY_NAME_APPEAL is reviewed by a fresh jury
    challengeID, err := k.repKeeper.CreateAppealChallenge(ctx, member, ChallengeType_DISPLAY_NAME_APPEAL, moderation.RejectedName, appealReason)
    if err != nil {
        k.repKeeper.RefundEscrowedDREAM(ctx, member, params.DisplayNameAppealStakeDream)
        return "", err
    }

    // Update moderation with appeal info
    moderation.AppealChallengeId = challengeID
    moderation.AppealedAt = ctx.BlockHeight()
    k.SetDisplayNameModeration(ctx, moderation)

    // Store stake info for resolution
    k.SetDisplayNameAppealStake(ctx, challengeID, member, params.DisplayNameAppealStakeDream)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("display_name_appeal_created",
            sdk.NewAttribute("member", member.String()),
            sdk.NewAttribute("rejected_name", moderation.RejectedName),
            sdk.NewAttribute("appeal_reason", appealReason),
            sdk.NewAttribute("challenge_id", challengeID),
            sdk.NewAttribute("stake_amount", params.DisplayNameAppealStakeDream.String()),
        ),
    )

    return challengeID, nil
}

// OnDisplayNameAppealResolved is called by x/rep when a display name appeal is resolved
func (k Keeper) OnDisplayNameAppealResolved(ctx sdk.Context, challengeID string, member sdk.AccAddress, appealSucceeded bool) {
    // Handle appellant stake
    stake := k.GetDisplayNameAppealStake(ctx, challengeID)
    if stake.Amount.IsPositive() {
        if appealSucceeded {
            // Appeal succeeded: return stake to appellant
            k.repKeeper.RefundEscrowedDREAM(ctx, member, stake.Amount)
        } else {
            // Appeal failed: burn stake
            k.repKeeper.BurnEscrowedDREAM(ctx, member, stake.Amount)
        }
        k.DeleteDisplayNameAppealStake(ctx, challengeID)
    }

    moderation := k.GetDisplayNameModeration(ctx, member)

    if appealSucceeded {
        // Appeal succeeded: deactivate moderation, member can use the name again
        moderation.Active = false
        moderation.AppealSucceeded = true
        k.SetDisplayNameModeration(ctx, moderation)

        ctx.EventManager().EmitEvent(
            sdk.NewEvent("display_name_appeal_succeeded",
                sdk.NewAttribute("member", member.String()),
                sdk.NewAttribute("rejected_name", moderation.RejectedName),
                sdk.NewAttribute("challenge_id", challengeID),
            ),
        )
    } else {
        // Appeal failed: moderation stands, clear appeal info to allow future appeals (if within period)
        moderation.AppealChallengeId = ""
        k.SetDisplayNameModeration(ctx, moderation)

        ctx.EventManager().EmitEvent(
            sdk.NewEvent("display_name_appeal_failed",
                sdk.NewAttribute("member", member.String()),
                sdk.NewAttribute("rejected_name", moderation.RejectedName),
                sdk.NewAttribute("challenge_id", challengeID),
            ),
        )
    }
}
```

```protobuf
message DisplayNameModeration {
    string member = 1;
    string rejected_name = 2;    // The name that was rejected
    string reason = 3;           // Moderation reason
    int64 moderated_at = 4;      // Block height when moderated
    bool active = 5;             // If false, moderation was overturned by appeal
    string appeal_challenge_id = 6;  // x/rep challenge ID for pending appeal (empty if no appeal)
    int64 appealed_at = 7;       // Block height when appeal was created (0 if no appeal)
    bool appeal_succeeded = 8;   // True if appeal was successful
}

// Stake tracking for display name reports (escrowed during jury review)
message DisplayNameReportStake {
    string challenge_id = 1;
    string reporter = 2;
    string amount = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

// Stake tracking for display name appeals (escrowed during jury review)
message DisplayNameAppealStake {
    string challenge_id = 1;
    string appellant = 2;
    string amount = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

// Pre-computed title eligibility for efficient transition processing
message SeasonTitleEligibility {
    uint32 season = 1;
    repeated TitleGrant grants = 2;
}

message TitleGrant {
    string title_id = 1;
    string title_name = 2;
    repeated string eligible_members = 3;  // Pre-computed list of eligible addresses
}
```

### Username (Unique, Reserved via x/name)

Usernames are unique identifiers reserved through the `x/name` module, used for @mentions and identification.

**x/name Integration:**
- Usernames use `NameTypeUsername` reservation type
- Guild names use `NameTypeGuild` reservation type
- x/name handles: length validation, character validation, uniqueness, reserved words
- **Owned names:** If a member already owns a name in x/name, they can use it as their username without additional reservation
- **Scavenging:** x/name supports scavenging inactive usernames. When scavenging, x/name calls `seasonKeeper.GetMemberProfile()` to check `last_active_epoch`. If the owner has been inactive beyond x/name's scavenge threshold, the username can be claimed by another member.

```go
func (k Keeper) SetUsername(ctx sdk.Context, member sdk.AccAddress, username string) error {
    params := k.GetParams(ctx)
    profile := k.GetMemberProfile(ctx, member)

    // Check cooldown
    currentEpoch := k.GetCurrentEpoch(ctx)
    if profile.LastUsernameChangeEpoch > 0 &&
       currentEpoch-profile.LastUsernameChangeEpoch < int64(params.UsernameChangeCooldownEpochs) {
        return ErrUsernameCooldown
    }

    // Check if member already owns this name in x/name (no reservation needed)
    alreadyOwned := k.nameKeeper.IsNameOwner(ctx, username, member)

    if !alreadyOwned {
        // Charge DREAM cost for new username reservation (anti-squatting)
        if params.UsernameCostDream.IsPositive() {
            if err := k.repKeeper.BurnDREAM(ctx, member, params.UsernameCostDream); err != nil {
                return ErrInsufficientDREAM
            }
        }

        // Release old username if exists (and we don't own it via x/name directly)
        if profile.Username != "" && !k.nameKeeper.IsNameOwner(ctx, profile.Username, member) {
            k.nameKeeper.ReleaseName(ctx, profile.Username)
        }

        // Reserve new username via x/name
        // x/name handles: length validation, character validation, uniqueness, reserved words
        if err := k.nameKeeper.ReserveName(ctx, username, NameTypeUsername, member); err != nil {
            return ErrUsernameTaken
        }
    }

    // Update profile
    profile.Username = username
    profile.LastUsernameChangeEpoch = currentEpoch
    k.SetMemberProfile(ctx, profile)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("username_set",
            sdk.NewAttribute("member", member.String()),
            sdk.NewAttribute("username", username),
            sdk.NewAttribute("already_owned", fmt.Sprintf("%t", alreadyOwned)),
        ),
    )

    return nil
}

func (k Keeper) SetDisplayTitle(ctx sdk.Context, member sdk.AccAddress, titleID string) error {
    // Verify title exists
    title, err := k.GetTitle(ctx, titleID)
    if err != nil {
        return ErrTitleNotFound
    }

    // Verify member has unlocked this title
    profile := k.GetMemberProfile(ctx, member)
    unlocked := false
    for _, t := range profile.UnlockedTitles {
        if t == titleID {
            unlocked = true
            break
        }
    }
    if !unlocked {
        return ErrTitleNotUnlocked
    }

    // Update display title
    profile.DisplayTitle = title.Name
    k.SetMemberProfile(ctx, profile)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("display_title_changed",
            sdk.NewAttribute("member", member.String()),
            sdk.NewAttribute("title_id", titleID),
        ),
    )

    return nil
}

// GrantSeasonalTitles grants titles for the ending season with season prefix
// Titles are permanent once granted - they persist across seasons
// GrantSeasonalTitles grants titles for the ending season.
// Uses pre-computed eligible member lists to avoid expensive iteration during transition.
//
// SECURITY: Title eligibility is computed BEFORE the transition starts (in PhaseSnapshot)
// and stored in SeasonTitleEligibility. This avoids O(n) iteration during PhaseTitles
// and prevents manipulation during the transition window.
func (k Keeper) GrantSeasonalTitles(ctx sdk.Context) {
    season := k.GetCurrentSeason(ctx)

    // Get pre-computed eligibility (computed in PhaseSnapshot before any state changes)
    eligibility := k.GetSeasonTitleEligibility(ctx, season.Number)

    for _, titleGrant := range eligibility.Grants {
        // Create season-specific title ID and name
        seasonTitleID := fmt.Sprintf("s%d_%s", season.Number, titleGrant.TitleId)
        seasonTitleName := fmt.Sprintf("S%d %s", season.Number, titleGrant.TitleName)

        // Create the season-specific title if it doesn't exist
        titleDef, _ := k.GetTitle(ctx, titleGrant.TitleId)
        seasonTitle := Title{
            Id:          seasonTitleID,
            Name:        seasonTitleName,
            Description: fmt.Sprintf("%s - Season %d", titleDef.Description, season.Number),
            Rarity:      titleDef.Rarity,
            Requirement: titleDef.Requirement,
            Seasonal:    true,
        }
        k.SetTitle(ctx, seasonTitle)

        // Grant to each eligible member
        for _, memberAddr := range titleGrant.EligibleMembers {
            member, _ := sdk.AccAddressFromBech32(memberAddr)
            k.unlockTitleForMember(ctx, member, seasonTitleID)

            ctx.EventManager().EmitEvent(
                sdk.NewEvent("seasonal_title_granted",
                    sdk.NewAttribute("member", memberAddr),
                    sdk.NewAttribute("title_id", seasonTitleID),
                    sdk.NewAttribute("title_name", seasonTitleName),
                    sdk.NewAttribute("season", fmt.Sprintf("%d", season.Number)),
                ),
            )
        }
    }

    // Clear eligibility data after grants
    k.DeleteSeasonTitleEligibility(ctx, season.Number)
}

// ComputeSeasonTitleEligibility pre-computes title eligibility during PhaseSnapshot.
// Called once before any transition state changes to capture accurate rankings.
func (k Keeper) ComputeSeasonTitleEligibility(ctx sdk.Context) {
    season := k.GetCurrentSeason(ctx)
    seasonalTitles := k.GetSeasonalTitles(ctx)

    eligibility := SeasonTitleEligibility{
        Season: season.Number,
        Grants: make([]TitleGrant, 0, len(seasonalTitles)),
    }

    for _, titleDef := range seasonalTitles {
        eligibleMembers := k.getEligibleMembersForTitle(ctx, titleDef)
        memberAddrs := make([]string, len(eligibleMembers))
        for i, m := range eligibleMembers {
            memberAddrs[i] = m.String()
        }

        eligibility.Grants = append(eligibility.Grants, TitleGrant{
            TitleId:         titleDef.Id,
            TitleName:       titleDef.Name,
            EligibleMembers: memberAddrs,
        })
    }

    k.SetSeasonTitleEligibility(ctx, eligibility)
}

// getEligibleMembersForTitle determines who qualifies for a seasonal title
// This queries current state (not stored leaderboards) to determine eligibility
func (k Keeper) getEligibleMembersForTitle(ctx sdk.Context, title Title) []sdk.AccAddress {
    var eligible []sdk.AccAddress

    switch title.Requirement.Type {
    case RequirementType_TOP_XP:
        // Top N XP earners this season
        // Iterate all profiles, sort by SeasonXP, take top N
        eligible = k.getTopXPEarners(ctx, int(title.Requirement.Threshold))

    case RequirementType_MIN_LEVEL:
        // All members who reached minimum level
        eligible = k.getMembersAtOrAboveLevel(ctx, uint32(title.Requirement.Threshold))

    case RequirementType_ACHIEVEMENT_COUNT:
        // Members with N or more achievements
        eligible = k.getMembersWithAchievementCount(ctx, int(title.Requirement.Threshold))

    // Add more requirement types as needed
    }

    return eligible
}

// unlockTitleForMember adds a title to a member's unlocked titles
// unlockTitleForMember adds a title to member's collection, archiving old titles if limit exceeded
func (k Keeper) unlockTitleForMember(ctx sdk.Context, member sdk.AccAddress, titleID string) {
    params := k.GetParams(ctx)
    profile := k.GetMemberProfile(ctx, member)

    // Check if already unlocked or archived
    for _, t := range profile.UnlockedTitles {
        if t == titleID {
            return // Already unlocked
        }
    }
    for _, t := range profile.ArchivedTitles {
        if t == titleID {
            return // Already archived
        }
    }

    // Add to unlocked titles
    profile.UnlockedTitles = append(profile.UnlockedTitles, titleID)

    // Archive oldest seasonal titles if over limit
    // Non-seasonal titles (achievements) are never archived
    for uint32(len(profile.UnlockedTitles)) > params.MaxDisplayableTitles {
        // Find oldest seasonal title to archive
        for i, t := range profile.UnlockedTitles {
            if strings.HasPrefix(t, "s") && strings.Contains(t, "_") {
                // This is a seasonal title (e.g., "s1_champion")
                profile.ArchivedTitles = append(profile.ArchivedTitles, t)
                profile.UnlockedTitles = append(profile.UnlockedTitles[:i], profile.UnlockedTitles[i+1:]...)
                break
            }
        }
        // Safety: if no seasonal titles found, stop to prevent infinite loop
        if uint32(len(profile.UnlockedTitles)) > params.MaxDisplayableTitles {
            break
        }
    }

    // Prune oldest archived titles if over limit
    // Archived titles are appended in chronological order, so oldest are at the front
    for uint32(len(profile.ArchivedTitles)) > params.MaxArchivedTitles {
        profile.ArchivedTitles = profile.ArchivedTitles[1:] // Remove oldest (first element)
    }

    k.SetMemberProfile(ctx, profile)
}

func (k Keeper) GetTitle(ctx sdk.Context, titleID string) (Title, error) {
    store := k.storeService.OpenKVStore(ctx)
    bz, err := store.Get(TitleKey(titleID))
    if err != nil || bz == nil {
        return Title{}, ErrTitleNotFound
    }
    var title Title
    k.cdc.MustUnmarshal(bz, &title)
    return title, nil
}
```

## Guild Management

### Creation

```go
func (k Keeper) CreateGuild(ctx sdk.Context, founder sdk.AccAddress, name, description string, inviteOnly bool) (uint64, error) {
    params := k.GetParams(ctx)

    // Validate founder is a member
    if !k.repKeeper.IsMember(ctx, founder) {
        return 0, ErrNotAMember
    }

    // Check founder isn't in a guild
    if k.GetMemberGuild(ctx, founder) != 0 {
        return 0, ErrAlreadyInGuild
    }

    // Validate description length
    if uint32(len(description)) > params.GuildDescriptionMaxLength {
        return 0, ErrGuildDescriptionTooLong
    }

    // Validate and reserve guild name via x/name
    if err := k.nameKeeper.ReserveName(ctx, name, NameTypeGuild, founder); err != nil {
        return 0, ErrGuildNameTaken
    }

    // Deduct creation cost
    if err := k.repKeeper.BurnDREAM(ctx, founder, params.GuildCreationCost); err != nil {
        k.nameKeeper.ReleaseName(ctx, name) // Rollback name reservation
        return 0, err
    }

    // Create guild
    guildID := k.GetNextGuildID(ctx)
    guild := Guild{
        ID:           guildID,
        Name:         name,
        Description:  description,
        Founder:      founder.String(),
        CreatedBlock: ctx.BlockHeight(),
        InviteOnly:   inviteOnly,
        Status:       GuildStatus_ACTIVE,
    }

    k.SetGuild(ctx, guild)
    k.SetGuildMembership(ctx, GuildMembership{
        Member:      founder.String(),
        GuildID:     guildID,
        JoinedEpoch: k.GetCurrentEpoch(ctx),
    })
    k.IncrementNextGuildID(ctx)

    return guildID, nil
}

```

### Join Guild

```go
func (k Keeper) JoinGuild(ctx sdk.Context, member sdk.AccAddress, guildID uint64) error {
    params := k.GetParams(ctx)
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // Validate member
    if !k.repKeeper.IsMember(ctx, member) {
        return ErrNotAMember
    }

    // Check not already in a guild
    if k.GetMemberGuild(ctx, member) != 0 {
        return ErrAlreadyInGuild
    }

    // Check guild capacity
    memberCount := k.GetGuildMemberCount(ctx, guildID)
    if memberCount >= uint64(params.MaxGuildMembers) {
        return ErrGuildFull
    }

    // Check invite-only
    if guild.InviteOnly {
        if !k.HasGuildInvite(ctx, guildID, member) {
            return ErrNotInvitedToGuild
        }
        // Clear the invite
        k.RemoveGuildInvite(ctx, guildID, member)
    }

    // Check hop cooldown
    membership := k.GetGuildMembership(ctx, member)
    if membership.LeftEpoch > 0 {
        epoch := k.GetCurrentEpoch(ctx)
        if epoch-membership.LeftEpoch < int64(params.GuildHopCooldownEpochs) {
            return ErrGuildHopCooldown
        }
    }

    // Check max guilds per season
    if membership.GuildsJoinedThisSeason >= params.MaxGuildsPerSeason {
        return ErrMaxGuildsPerSeason
    }

    // Join guild
    k.SetGuildMembership(ctx, GuildMembership{
        Member:                 member.String(),
        GuildID:                guildID,
        JoinedEpoch:            k.GetCurrentEpoch(ctx),
        GuildsJoinedThisSeason: membership.GuildsJoinedThisSeason + 1,
    })

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("guild_joined",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("member", member.String()),
        ),
    )

    return nil
}
```

### Leave Guild

```go
func (k Keeper) LeaveGuild(ctx sdk.Context, member sdk.AccAddress) error {
    guildID := k.GetMemberGuild(ctx, member)
    if guildID == 0 {
        return ErrNotInGuild
    }

    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // If founder is leaving, trigger succession
    if guild.Founder == member.String() {
        if err := k.HandleFounderDeparture(ctx, guildID); err != nil {
            return err
        }
    } else {
        // Remove from officers if applicable
        if k.IsGuildOfficer(ctx, guildID, member) {
            k.removeOfficer(ctx, guild, member)
        }
    }

    // Update membership (set left_epoch for cooldown tracking)
    membership := k.GetGuildMembership(ctx, member)
    membership.GuildID = 0
    membership.LeftEpoch = k.GetCurrentEpoch(ctx)
    k.SetGuildMembership(ctx, membership)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("guild_left",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("member", member.String()),
        ),
    )

    // Check if guild dropped below minimum members - auto-freeze if so
    k.checkAndFreezeUnderMinimum(ctx, guildID)

    return nil
}

// checkAndFreezeUnderMinimum freezes a guild if membership drops below minimum
func (k Keeper) checkAndFreezeUnderMinimum(ctx sdk.Context, guildID uint64) {
    params := k.GetParams(ctx)
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil || guild.Status != GuildStatus_ACTIVE {
        return
    }

    memberCount := k.GetGuildMemberCount(ctx, guildID)
    if memberCount < uint64(params.MinGuildMembers) {
        guild.Status = GuildStatus_FROZEN
        guild.Founder = ""  // Clear founder - any member can claim
        k.SetGuild(ctx, guild)

        ctx.EventManager().EmitEvent(
            sdk.NewEvent("guild_frozen",
                sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
                sdk.NewAttribute("reason", "below_minimum_members"),
                sdk.NewAttribute("member_count", fmt.Sprintf("%d", memberCount)),
                sdk.NewAttribute("minimum_required", fmt.Sprintf("%d", params.MinGuildMembers)),
            ),
        )
    }
}
```

### Guild Invitations

```go
func (k Keeper) InviteToGuild(ctx sdk.Context, inviter sdk.AccAddress, guildID uint64, invitee sdk.AccAddress) error {
    params := k.GetParams(ctx)
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // Verify inviter is founder or officer
    if guild.Founder != inviter.String() && !k.IsGuildOfficer(ctx, guildID, inviter) {
        return ErrNotGuildOfficer
    }

    // Verify invitee is a member of the system
    if !k.repKeeper.IsMember(ctx, invitee) {
        return ErrNotAMember
    }

    // Check invitee not already in a guild
    if k.GetMemberGuild(ctx, invitee) != 0 {
        return ErrAlreadyInGuild
    }

    // Check not already invited
    if k.HasGuildInvite(ctx, guildID, invitee) {
        return ErrAlreadyInvited
    }

    // Check invite limit
    invites := k.GetGuildInvites(ctx, guildID)
    if uint32(len(invites)) >= params.MaxPendingInvites {
        return ErrTooManyInvites
    }

    // Add invite
    k.AddGuildInvite(ctx, guildID, invitee)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("guild_invite_sent",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("inviter", inviter.String()),
            sdk.NewAttribute("invitee", invitee.String()),
        ),
    )

    return nil
}

func (k Keeper) AcceptGuildInvite(ctx sdk.Context, member sdk.AccAddress, guildID uint64) error {
    // Verify invite exists
    if !k.HasGuildInvite(ctx, guildID, member) {
        return ErrNotInvitedToGuild
    }

    // JoinGuild handles the rest (and clears the invite)
    return k.JoinGuild(ctx, member, guildID)
}
```

### Officer Management

```go
func (k Keeper) PromoteToOfficer(ctx sdk.Context, founder sdk.AccAddress, guildID uint64, member sdk.AccAddress) error {
    params := k.GetParams(ctx)
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // Only founder can promote
    if guild.Founder != founder.String() {
        return ErrNotGuildFounder
    }

    // Verify member is in guild
    if k.GetMemberGuild(ctx, member) != guildID {
        return ErrNotInGuild
    }

    // Check not already officer
    if k.IsGuildOfficer(ctx, guildID, member) {
        return ErrAlreadyOfficer
    }

    // Check max officers limit
    if uint32(len(guild.Officers)) >= params.MaxGuildOfficers {
        return ErrTooManyOfficers
    }

    // Add to officers
    guild.Officers = append(guild.Officers, member.String())
    k.SetGuild(ctx, guild)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("officer_promoted",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("member", member.String()),
        ),
    )

    return nil
}

func (k Keeper) DemoteOfficer(ctx sdk.Context, founder sdk.AccAddress, guildID uint64, officer sdk.AccAddress) error {
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // Only founder can demote
    if guild.Founder != founder.String() {
        return ErrNotGuildFounder
    }

    // Find and remove officer
    officerStr := officer.String()
    found := false
    newOfficers := make([]string, 0, len(guild.Officers))
    for _, o := range guild.Officers {
        if o == officerStr {
            found = true
        } else {
            newOfficers = append(newOfficers, o)
        }
    }

    if !found {
        return ErrNotAnOfficer
    }

    guild.Officers = newOfficers
    k.SetGuild(ctx, guild)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("officer_demoted",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("member", officer.String()),
        ),
    )

    return nil
}
```

### Founder Succession (Simplified)

```go
func (k Keeper) HandleFounderDeparture(ctx sdk.Context, guildID uint64) error {
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // Try to transfer to first officer
    if len(guild.Officers) > 0 {
        newFounder := guild.Officers[0]
        guild.Founder = newFounder
        guild.Officers = guild.Officers[1:]
        k.SetGuild(ctx, guild)

        ctx.EventManager().EmitEvent(
            sdk.NewEvent("guild_founder_transferred",
                sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
                sdk.NewAttribute("new_founder", newFounder),
                sdk.NewAttribute("reason", "founder_departed"),
            ),
        )
        return nil
    }

    // No officers - guild freezes, requires governance intervention
    guild.Status = GuildStatus_FROZEN
    guild.Founder = ""  // Clear founder
    k.SetGuild(ctx, guild)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("guild_frozen",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("reason", "no_successor"),
        ),
    )
    return nil
}
```

### Dissolution

```go
func (k Keeper) DissolveGuild(ctx sdk.Context, founder sdk.AccAddress, guildID uint64) error {
    params := k.GetParams(ctx)
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // Verify founder
    if guild.Founder != founder.String() {
        return ErrNotGuildFounder
    }

    // Check minimum age
    guildAge := k.GetCurrentEpoch(ctx) - k.BlockToEpoch(ctx, guild.CreatedBlock)
    if guildAge < int64(params.MinGuildAgeEpochs) {
        return ErrGuildTooYoung
    }

    // Remove all memberships
    members := k.GetGuildMembers(ctx, guildID)
    for _, member := range members {
        k.DeleteGuildMembership(ctx, sdk.MustAccAddressFromBech32(member))
    }

    // Clear pending invites
    k.ClearGuildInvites(ctx, guildID)

    // Release guild name
    k.nameKeeper.ReleaseName(ctx, guild.Name)

    // Remove guild
    k.DeleteGuild(ctx, guildID)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("guild_dissolved",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("founder", founder.String()),
        ),
    )

    return nil
}
```

### Update Guild Description

```go
func (k Keeper) UpdateGuildDescription(ctx sdk.Context, founder sdk.AccAddress, guildID uint64, description string) error {
    params := k.GetParams(ctx)
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // Only founder can update description
    if guild.Founder != founder.String() {
        return ErrNotGuildFounder
    }

    // Validate description length
    if uint32(len(description)) > params.GuildDescriptionMaxLength {
        return ErrGuildDescriptionTooLong
    }

    guild.Description = description
    k.SetGuild(ctx, guild)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("guild_description_updated",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
        ),
    )

    return nil
}
```

### Kick Member

```go
func (k Keeper) KickFromGuild(ctx sdk.Context, kicker sdk.AccAddress, guildID uint64, member sdk.AccAddress, reason string) error {
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // Verify kicker is founder or officer
    isFounder := guild.Founder == kicker.String()
    isOfficer := k.IsGuildOfficer(ctx, guildID, kicker)
    if !isFounder && !isOfficer {
        return ErrNotGuildOfficer
    }

    // Verify member is in the guild
    if k.GetMemberGuild(ctx, member) != guildID {
        return ErrNotInGuild
    }

    // Cannot kick the founder
    if member.String() == guild.Founder {
        return ErrCannotKickFounder
    }

    // Officers can only kick non-officers
    if !isFounder && k.IsGuildOfficer(ctx, guildID, member) {
        return ErrCannotKickOfficer
    }

    // Remove from officers if applicable
    if k.IsGuildOfficer(ctx, guildID, member) {
        k.removeOfficer(ctx, guild, member)
    }

    // Remove membership (set left_epoch for cooldown tracking)
    membership := k.GetGuildMembership(ctx, member)
    membership.GuildID = 0
    membership.LeftEpoch = k.GetCurrentEpoch(ctx)
    k.SetGuildMembership(ctx, membership)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("guild_member_kicked",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("member", member.String()),
            sdk.NewAttribute("kicker", kicker.String()),
            sdk.NewAttribute("reason", reason),
        ),
    )

    // Check if guild dropped below minimum members - auto-freeze if so
    k.checkAndFreezeUnderMinimum(ctx, guildID)

    return nil
}
```

### Claim Guild Founder (Member Action)

Any member of a frozen guild can claim founder status. First-come-first-serve.

```go
func (k Keeper) ClaimGuildFounder(ctx sdk.Context, member sdk.AccAddress, guildID uint64) error {
    guild, err := k.GetGuild(ctx, guildID)
    if err != nil {
        return err
    }

    // Verify guild is frozen
    if guild.Status != GuildStatus_FROZEN {
        return ErrGuildNotFrozen
    }

    // Verify member is in the guild
    if k.GetMemberGuild(ctx, member) != guildID {
        return ErrNotInGuild
    }

    // Unfreeze and set new founder
    guild.Status = GuildStatus_ACTIVE
    guild.Founder = member.String()

    // Remove from officers if they were one
    newOfficers := make([]string, 0, len(guild.Officers))
    for _, o := range guild.Officers {
        if o != member.String() {
            newOfficers = append(newOfficers, o)
        }
    }
    guild.Officers = newOfficers

    k.SetGuild(ctx, guild)

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("guild_founder_claimed",
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
            sdk.NewAttribute("new_founder", member.String()),
        ),
    )

    return nil
}
```

### Guild XP Accumulation

Guild XP is the sum of XP earned by members while they are in the guild. This powers the `LEADERBOARD_GUILD_XP` category.

**Anti-Gaming:** XP only counts toward guild totals if the member has been in the guild for at least 1 epoch. This prevents rapid guild-hopping to inflate guild XP.

```go
// GrantXP is the public entry point for granting XP. It enforces the global epoch cap.
// All XP sources (votes, forum, quests, achievements) go through this function.
func (k Keeper) GrantXP(ctx sdk.Context, member sdk.AccAddress, amount uint64, source XPSource, reference string) error {
    params := k.GetParams(ctx)
    epoch := k.GetCurrentEpoch(ctx)

    // Check global epoch cap
    tracker := k.GetEpochXPTracker(ctx, member, epoch)
    totalEpochXP := tracker.VoteXpEarned + tracker.ForumXpEarned + tracker.QuestXpEarned + tracker.OtherXpEarned

    if totalEpochXP >= params.MaxXpPerEpoch {
        return ErrXPCapReached
    }

    // Cap amount to remaining allowance
    remaining := params.MaxXpPerEpoch - totalEpochXP
    if amount > remaining {
        amount = remaining
    }

    if amount == 0 {
        return nil
    }

    // Track XP by source type in epoch tracker
    switch source {
    case XP_SOURCE_VOTE_CAST, XP_SOURCE_PROPOSAL_CREATED:
        tracker.VoteXpEarned += amount
    case XP_SOURCE_FORUM_REPLY, XP_SOURCE_FORUM_HELPFUL:
        tracker.ForumXpEarned += amount
    case XP_SOURCE_QUEST:
        tracker.QuestXpEarned += amount
    default:
        tracker.OtherXpEarned += amount
    }
    k.SetEpochXPTracker(ctx, tracker)

    return k.grantXPInternal(ctx, member, amount, source, reference)
}

// grantXPInternal is the internal XP granting function (cap already checked)
func (k Keeper) grantXPInternal(ctx sdk.Context, member sdk.AccAddress, amount uint64, source XPSource, reference string) error {
    // Update member's XP
    profile := k.GetMemberProfile(ctx, member)

    // Overflow protection for uint64 XP values
    if profile.SeasonXP > math.MaxUint64 - amount {
        return ErrXPOverflow
    }
    if profile.LifetimeXP > math.MaxUint64 - amount {
        return ErrXPOverflow
    }

    profile.SeasonXP += amount
    profile.LifetimeXP += amount

    // Check for level up
    oldLevel := profile.SeasonLevel
    newLevel := k.CalculateLevel(ctx, profile.SeasonXP)
    if newLevel > oldLevel {
        profile.SeasonLevel = newLevel
        ctx.EventManager().EmitEvent(
            sdk.NewEvent("level_up",
                sdk.NewAttribute("member", member.String()),
                sdk.NewAttribute("old_level", fmt.Sprintf("%d", oldLevel)),
                sdk.NewAttribute("new_level", fmt.Sprintf("%d", newLevel)),
            ),
        )
    }
    k.SetMemberProfile(ctx, profile)

    // Note: Guild XP is NOT tracked on-chain. Indexers compute guild XP from
    // xp_granted events by aggregating XP for members who were in a guild
    // for at least 1 epoch at the time of grant.

    ctx.EventManager().EmitEvent(
        sdk.NewEvent("xp_granted",
            sdk.NewAttribute("member", member.String()),
            sdk.NewAttribute("amount", fmt.Sprintf("%d", amount)),
            sdk.NewAttribute("source", fmt.Sprintf("%d", source)),
            sdk.NewAttribute("reference", reference),
            sdk.NewAttribute("guild_id", fmt.Sprintf("%d", k.GetMemberGuild(ctx, member))),
        ),
    )

    return nil
}

// ResetGuildXPForSeason is called during season transitions
func (k Keeper) ResetGuildXPForSeason(ctx sdk.Context) {
    k.IterateGuilds(ctx, func(guild Guild) bool {
        guild.SeasonXP = 0
        k.SetGuild(ctx, guild)
        return false
    })
}
```

## Security Considerations

### XP Anti-Gaming

**Forum XP:**
- Replies from accounts < N epochs old don't grant XP
- Can't earn XP from same person more than once per epoch
- Per-epoch cap on forum XP

**Vote XP:**
- First vote per proposal only (de-duplication)
- Per-epoch cap on vote XP grants

**General:**
- Global per-epoch XP cap
- All XP sources are from external triggers (hooks), not self-reported

### Guild Security

- Hop cooldown between guilds (prevents rapid switching for benefits)
- Max guilds per season (prevents guild hopping abuse)
- Min age before dissolution (prevents flash guild attacks)
- Name uniqueness via x/name module
- Invite-only mode prevents unwanted joins
- Max pending invites limit
- Kick requires reason for transparency and appeal basis
- Officers cannot kick other officers (prevents lateral abuse)
- **Guild XP tenure requirement:** XP only counts toward guild totals after 1 epoch of membership (prevents join→earn→leave gaming)

### Hook Authorization

All hooks validate the calling module to prevent unauthorized XP grants.

### Quest Security

- **Progress Authorization:** `UpdateQuestProgress` is NOT exposed via MsgServer. Only authorized module hooks (x/rep, x/forum, x/gov) may update quest progress. Users cannot directly manipulate their quest progress.
- **Objective Type Validation:** Quest creation validates that all objective types are known enum values.
- **Reward Limits:** Quest XP rewards are capped by `max_quest_xp_reward` param.
- **Cycle Detection:** Quest prerequisite chains are validated to prevent infinite loops.
- **Active Quest Limit:** Members are limited to `max_active_quests_per_member` concurrent quests.

### Display Name Moderation Security

- **Stake Requirement:** Reporters must stake DREAM (burned if report fails, returned if upheld). Prevents spam reports.
- **Jury Review:** All reports go through x/rep jury system for fair adjudication.
- **Permanent Block:** Rejected names are blocked for that specific member only.
- **Appeal Mechanism:** Moderated members can appeal through x/rep challenge system.

## KVStore Layout

```
# Module store prefix: 0x01 (season)

# Season data
season/current                           -> Season (current active season)
season/{season_number}                   -> Season (historical seasons)
season/transition_state                  -> SeasonTransitionState
season/transition_recovery               -> TransitionRecoveryState
season/next_info                         -> NextSeasonInfo

# Member profiles and XP tracking
profile/{member_address}                 -> MemberProfile
epoch_xp/{member_address}/{epoch}        -> EpochXPTracker
vote_xp/{season}/{member_address}/{proposal_id} -> VoteXPRecord (de-duplication)
member_registration/{member_address}     -> MemberRegistration (account age tracking)

# Forum XP anti-gaming state
forum_xp_cooldown/{beneficiary}/{actor}  -> ForumXPCooldown (reciprocal XP cooldown)

# Display name moderation
display_name_moderation/{member_address} -> DisplayNameModeration
display_name_report_stake/{challenge_id} -> DisplayNameReportStake
display_name_appeal_stake/{challenge_id} -> DisplayNameAppealStake

# Snapshots (per season, per member)
snapshot/{season_number}                 -> SeasonSnapshot (aggregate stats)
snapshot/{season_number}/{member_address} -> MemberSeasonSnapshot

# Achievements and titles (definitions)
achievement/{achievement_id}             -> Achievement
title/{title_id}                         -> Title

# Guilds
guild/{guild_id}                         -> Guild
guild/next_id                            -> uint64
guild_membership/{member_address}        -> GuildMembership
guild_members/{guild_id}/{member_address} -> bool (index for member lookup)
guild_invite/{guild_id}/{invitee_address} -> GuildInvite
guild_invite_cleanup_cursor              -> GuildInvite (cursor for batch cleanup)

# Quests
quest/{quest_id}                         -> Quest
quest_progress/{member_address}/{quest_id} -> MemberQuestProgress
quest_by_season/{season}/{quest_id}      -> bool (index for seasonal quests)

# Retroactive Public Goods Nominations
nomination/next_id                       -> uint64
nomination/{nomination_id}               -> Nomination
nomination_by_season/{season}/{nomination_id} -> bool (index)
nomination_by_content/{season}/{content_ref}  -> uint64 (nomination_id, dedup index)
nomination_by_nominator/{season}/{nominator}/{nomination_id} -> bool (index for per-member limit)
nomination_stake/{nomination_id}/{staker} -> NominationStake
retro_reward/{season}/{nomination_id}   -> RetroRewardRecord

# Note: Leaderboards are NOT stored on-chain.
# Indexers compute them from MemberProfile.season_xp and events.
```

### Historical Data Pruning

To prevent unbounded state growth, the module prunes old season snapshots based on the `snapshot_retention_seasons` param.

**Pruning Strategy:**
- Keep the last N seasons of member snapshots (default: 10)
- Keep all season metadata (Season struct) forever for historical reference
- Prune during season transitions in a batched phase
- If `snapshot_retention_seasons = 0`, keep all snapshots (not recommended for production)

```go
// PruneOldSnapshots removes member snapshots older than retention limit
// Called during season transition after PhaseComplete
func (k Keeper) PruneOldSnapshots(ctx sdk.Context) error {
    params := k.GetParams(ctx)
    if params.SnapshotRetentionSeasons == 0 {
        return nil // Keep all snapshots
    }

    currentSeason := k.GetCurrentSeason(ctx)
    if currentSeason.Number <= params.SnapshotRetentionSeasons {
        return nil // Not enough seasons to prune yet
    }

    // Prune seasons older than retention limit
    pruneBeforeSeason := currentSeason.Number - params.SnapshotRetentionSeasons

    for seasonNum := uint32(1); seasonNum < pruneBeforeSeason; seasonNum++ {
        // Check if this season's snapshots still exist
        if k.HasSeasonSnapshots(ctx, seasonNum) {
            k.DeleteSeasonSnapshots(ctx, seasonNum)

            ctx.EventManager().EmitEvent(
                sdk.NewEvent("season_snapshots_pruned",
                    sdk.NewAttribute("season_number", fmt.Sprintf("%d", seasonNum)),
                ),
            )
        }
    }

    return nil
}

// DeleteSeasonSnapshots removes all member snapshots for a given season
// This is batched internally to avoid gas limit issues
func (k Keeper) DeleteSeasonSnapshots(ctx sdk.Context, seasonNumber uint32) {
    store := k.storeService.OpenKVStore(ctx)
    prefix := SnapshotMemberPrefix(seasonNumber)

    iterator := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
    defer iterator.Close()

    keysToDelete := make([][]byte, 0)
    for ; iterator.Valid(); iterator.Next() {
        keysToDelete = append(keysToDelete, iterator.Key())
    }

    for _, key := range keysToDelete {
        store.Delete(key)
    }
}

// PruneOldEpochTrackers removes epoch XP trackers older than retention limit
// Called during season transition PhaseCleanup
func (k Keeper) PruneOldEpochTrackers(ctx sdk.Context) {
    params := k.GetParams(ctx)
    if params.EpochTrackerRetentionEpochs == 0 {
        return // Keep all trackers (not recommended)
    }

    currentEpoch := k.GetCurrentEpoch(ctx)
    cutoffEpoch := currentEpoch - int64(params.EpochTrackerRetentionEpochs)
    if cutoffEpoch <= 0 {
        return
    }

    store := k.storeService.OpenKVStore(ctx)
    prefix := EpochXPTrackerPrefix()

    iterator := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
    defer iterator.Close()

    keysToDelete := make([][]byte, 0)
    for ; iterator.Valid(); iterator.Next() {
        var tracker EpochXPTracker
        k.cdc.MustUnmarshal(iterator.Value(), &tracker)

        if tracker.Epoch < cutoffEpoch {
            keysToDelete = append(keysToDelete, iterator.Key())
        }
    }

    for _, key := range keysToDelete {
        store.Delete(key)
    }

    if len(keysToDelete) > 0 {
        ctx.EventManager().EmitEvent(
            sdk.NewEvent("epoch_trackers_pruned",
                sdk.NewAttribute("count", fmt.Sprintf("%d", len(keysToDelete))),
                sdk.NewAttribute("cutoff_epoch", fmt.Sprintf("%d", cutoffEpoch)),
            ),
        )
    }
}

// PruneOldForumXPCooldowns removes stale forum XP cooldown records
// Called during season transition PhaseCleanup
func (k Keeper) PruneOldForumXPCooldowns(ctx sdk.Context) {
    params := k.GetParams(ctx)
    if params.ForumCooldownRetentionEpochs == 0 {
        return // Keep all cooldowns (not recommended)
    }

    currentEpoch := k.GetCurrentEpoch(ctx)
    cutoffEpoch := currentEpoch - int64(params.ForumCooldownRetentionEpochs)
    if cutoffEpoch <= 0 {
        return
    }

    store := k.storeService.OpenKVStore(ctx)
    prefix := ForumXPCooldownPrefix()

    iterator := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
    defer iterator.Close()

    keysToDelete := make([][]byte, 0)
    for ; iterator.Valid(); iterator.Next() {
        var cooldown ForumXPCooldown
        k.cdc.MustUnmarshal(iterator.Value(), &cooldown)

        if cooldown.LastGrantedEpoch < cutoffEpoch {
            keysToDelete = append(keysToDelete, iterator.Key())
        }
    }

    for _, key := range keysToDelete {
        store.Delete(key)
    }

    if len(keysToDelete) > 0 {
        ctx.EventManager().EmitEvent(
            sdk.NewEvent("forum_cooldowns_pruned",
                sdk.NewAttribute("count", fmt.Sprintf("%d", len(keysToDelete))),
            ),
        )
    }
}

// PruneOldVoteXPRecords removes vote XP records from old seasons
// Called during season transition PhaseCleanup
func (k Keeper) PruneOldVoteXPRecords(ctx sdk.Context) {
    params := k.GetParams(ctx)
    if params.VoteXpRecordRetentionSeasons == 0 {
        return // Keep all records (not recommended)
    }

    currentSeason := k.GetCurrentSeason(ctx)
    if currentSeason.Number <= params.VoteXpRecordRetentionSeasons {
        return // Not enough seasons to prune
    }

    cutoffSeason := currentSeason.Number - params.VoteXpRecordRetentionSeasons

    store := k.storeService.OpenKVStore(ctx)

    // Delete records for seasons older than cutoff
    for seasonNum := uint32(1); seasonNum < cutoffSeason; seasonNum++ {
        prefix := VoteXPRecordSeasonPrefix(seasonNum)

        iterator := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
        keysToDelete := make([][]byte, 0)
        for ; iterator.Valid(); iterator.Next() {
            keysToDelete = append(keysToDelete, iterator.Key())
        }
        iterator.Close()

        for _, key := range keysToDelete {
            store.Delete(key)
        }
    }
}
```

**Migration Consideration:** When enabling pruning on an existing chain, old snapshots are not retroactively pruned. They will be pruned as new seasons complete.

### Guild Invite Expiration

Expired guild invites are cleaned up during BeginBlocker or lazily when accessed.

```go
// CleanupExpiredGuildInvites removes expired invites for a guild
// Called lazily when invites are accessed, or during BeginBlocker
func (k Keeper) CleanupExpiredGuildInvites(ctx sdk.Context, guildID uint64) {
    params := k.GetParams(ctx)
    if params.GuildInviteTtlEpochs == 0 {
        return // Invites never expire
    }

    currentEpoch := k.GetCurrentEpoch(ctx)
    invites := k.GetGuildInvitesRaw(ctx, guildID)

    for _, invite := range invites {
        if invite.ExpiresEpoch > 0 && currentEpoch >= invite.ExpiresEpoch {
            k.DeleteGuildInvite(ctx, guildID, sdk.MustAccAddressFromBech32(invite.Invitee))

            ctx.EventManager().EmitEvent(
                sdk.NewEvent("guild_invite_expired",
                    sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
                    sdk.NewAttribute("invitee", invite.Invitee),
                ),
            )
        }
    }
}

// CleanupExpiredGuildInvitesBatch iterates guilds and cleans up expired invites
// Called periodically from BeginBlocker to prevent unbounded invite accumulation
// Uses cursor-based iteration to spread work across multiple blocks
func (k Keeper) CleanupExpiredGuildInvitesBatch(ctx sdk.Context, batchSize int) {
    params := k.GetParams(ctx)
    if params.GuildInviteTtlEpochs == 0 {
        return // Invites never expire
    }

    currentEpoch := k.GetCurrentEpoch(ctx)
    cursor := k.GetInviteCleanupCursor(ctx)
    processed := 0

    k.IterateGuildInvitesFrom(ctx, cursor, func(invite GuildInvite) bool {
        if processed >= batchSize {
            return true // Stop iteration
        }

        if invite.ExpiresEpoch > 0 && currentEpoch >= invite.ExpiresEpoch {
            k.DeleteGuildInvite(ctx, invite.GuildId, sdk.MustAccAddressFromBech32(invite.Invitee))

            ctx.EventManager().EmitEvent(
                sdk.NewEvent("guild_invite_expired",
                    sdk.NewAttribute("guild_id", fmt.Sprintf("%d", invite.GuildId)),
                    sdk.NewAttribute("invitee", invite.Invitee),
                ),
            )
        }

        processed++
        cursor = invite // Update cursor to last processed invite
        return false
    })

    // Save cursor for next batch (wraps around when iteration completes)
    k.SetInviteCleanupCursor(ctx, cursor)
}

// InviteToGuild now sets expiration based on params
func (k Keeper) InviteToGuild(ctx sdk.Context, inviter sdk.AccAddress, guildID uint64, invitee sdk.AccAddress) error {
    params := k.GetParams(ctx)
    // ... existing validation ...

    currentEpoch := k.GetCurrentEpoch(ctx)
    var expiresEpoch int64
    if params.GuildInviteTtlEpochs > 0 {
        expiresEpoch = currentEpoch + int64(params.GuildInviteTtlEpochs)
    }

    invite := GuildInvite{
        GuildId:      guildID,
        Invitee:      invitee.String(),
        Inviter:      inviter.String(),
        CreatedEpoch: currentEpoch,
        ExpiresEpoch: expiresEpoch,
    }
    k.SetGuildInvite(ctx, invite)

    // ... rest of function ...
}
```

## Genesis State

```protobuf
message GenesisState {
  Params params = 1;
  Season current_season = 2;
  repeated Season past_seasons = 3;
  repeated MemberProfile profiles = 4;
  repeated Achievement achievements = 5;
  repeated Title titles = 6;
  repeated Guild guilds = 7;
  uint64 next_guild_id = 8;
  repeated Quest quests = 9;
  repeated MemberQuestProgress quest_progress = 10;
  repeated GuildMembership guild_memberships = 11;
  repeated GuildInvite guild_invites = 12;
  repeated EpochXPTracker epoch_xp_trackers = 13;
  repeated VoteXPRecord vote_xp_records = 14;
  SeasonTransitionState transition_state = 15;
  NextSeasonInfo next_season_info = 16;
  repeated MemberRegistration member_registrations = 17;  // For forum XP account age checks
  repeated ForumXPCooldown forum_xp_cooldowns = 18;       // For forum XP anti-gaming
  TransitionRecoveryState transition_recovery_state = 19; // Recovery state for stuck transitions
  repeated DisplayNameModeration display_name_moderations = 20; // Rejected display names
  repeated DisplayNameReportStake display_name_report_stakes = 21; // Pending report stakes
  repeated DisplayNameAppealStake display_name_appeal_stakes = 22; // Pending appeal stakes
  repeated Nomination nominations = 23;                            // Retroactive public goods nominations
  uint64 next_nomination_id = 24;
}

message NextSeasonInfo {
  string name = 1;
  string theme = 2;
}
```

### Genesis Initialization

The module bootstraps Season 1 at genesis if no current season is provided:

```go
func (k Keeper) InitGenesis(ctx sdk.Context, data GenesisState) {
    k.SetParams(ctx, data.Params)

    // Import default achievements and titles
    for _, achievement := range DefaultAchievements {
        k.SetAchievement(ctx, achievement)
    }
    for _, title := range DefaultTitles {
        k.SetTitle(ctx, title)
    }

    // Bootstrap Season 1 if no current season provided
    if data.CurrentSeason == nil {
        season := Season{
            Number:    1,
            Name:      "Season 1",
            Theme:     "Genesis",
            StartBlock: ctx.BlockHeight(),
            EndBlock:   ctx.BlockHeight() + (data.Params.EpochBlocks * data.Params.SeasonDurationEpochs),
            Status:     SeasonStatus_ACTIVE,
            OriginalEndBlock: ctx.BlockHeight() + (data.Params.EpochBlocks * data.Params.SeasonDurationEpochs),
        }
        k.SetCurrentSeason(ctx, season)

        ctx.EventManager().EmitEvent(
            sdk.NewEvent("new_season_started",
                sdk.NewAttribute("season_number", "1"),
                sdk.NewAttribute("name", "Season 1"),
                sdk.NewAttribute("theme", "Genesis"),
            ),
        )
    } else {
        k.SetCurrentSeason(ctx, *data.CurrentSeason)
    }

    // Import remaining genesis state...
    for _, season := range data.PastSeasons {
        k.SetSeason(ctx, season)
    }
    // ... (profiles, guilds, quests, etc.)
}
```

## Events

```protobuf
// XP events
message EventXPGranted {
  string member = 1;
  uint64 amount = 2;
  string source = 3;
  string reference = 4;
  uint64 new_total = 5;
}

message EventLevelUp {
  string member = 1;
  uint32 old_level = 2;
  uint32 new_level = 3;
}

// Achievement events
message EventAchievementUnlocked {
  string member = 1;
  string achievement_id = 2;
  uint64 xp_reward = 3;
}

// Guild events
message EventGuildCreated {
  uint64 guild_id = 1;
  string founder = 2;
  string name = 3;
}

message EventGuildJoined {
  uint64 guild_id = 1;
  string member = 2;
}

message EventGuildLeft {
  uint64 guild_id = 1;
  string member = 2;
}

message EventGuildDissolved {
  uint64 guild_id = 1;
}

message EventGuildInviteSent {
  uint64 guild_id = 1;
  string inviter = 2;
  string invitee = 3;
}

message EventGuildInviteAccepted {
  uint64 guild_id = 1;
  string member = 2;
}

message EventGuildInviteRevoked {
  uint64 guild_id = 1;
  string invitee = 2;
}

message EventGuildInviteExpired {
  uint64 guild_id = 1;
  string invitee = 2;
}

message EventOfficerPromoted {
  uint64 guild_id = 1;
  string member = 2;
}

message EventOfficerDemoted {
  uint64 guild_id = 1;
  string member = 2;
}

message EventGuildFounderTransferred {
  uint64 guild_id = 1;
  string old_founder = 2;
  string new_founder = 3;
}

message EventGuildDescriptionUpdated {
  uint64 guild_id = 1;
}

message EventGuildMemberKicked {
  uint64 guild_id = 1;
  string member = 2;
  string kicker = 3;
  string reason = 4;
}

message EventGuildFounderClaimed {
  uint64 guild_id = 1;
  string new_founder = 2;
}

// Quest events
message EventQuestStarted {
  string member = 1;
  string quest_id = 2;
}

message EventQuestCompleted {
  string member = 1;
  string quest_id = 2;
  uint64 xp_reward = 3;
}

message EventQuestAbandoned {
  string member = 1;
  string quest_id = 2;
}

message EventSeasonalQuestExpired {
  string quest_id = 1;
  uint32 season = 2;
}

// Season events
message EventSeasonTransitionStarted {
  uint32 season_number = 1;
}

message EventSeasonTransitionProgress {
  uint32 season_number = 1;
  string phase = 2;
  uint64 processed = 3;
  uint64 total = 4;
}

message EventSeasonEnded {
  uint32 season_number = 1;
}

message EventNewSeasonStarted {
  uint32 season_number = 1;
  string name = 2;
  string theme = 3;
}

message EventSeasonExtended {
  uint32 season_number = 1;
  uint64 extension_epochs = 2;
  int64 new_end_block = 3;
}

// Maintenance mode
message EventMaintenanceModeStarted {
  uint32 season_number = 1;
}

message EventMaintenanceModeEnded {
  uint32 season_number = 1;
}

// Member zeroing cleanup
message EventMemberZeroedCleanup {
  string member = 1;
  uint64 guild_removed = 2;  // 0 if not in guild
  string display_name_released = 3;  // Empty if none
}

// Member profile lifecycle
message EventMemberProfileCreated {
  string member = 1;
  uint32 season = 2;
}

// Historical data management
message EventSeasonSnapshotsPruned {
  uint32 season_number = 1;
}

// Retroactive Public Goods events
message EventNominationWindowOpened {
  uint32 season_number = 1;
  int64 closes_at_block = 2;
}

message EventNominationCreated {
  uint64 nomination_id = 1;
  string nominator = 2;
  string content_ref = 3;
  uint32 season = 4;
}

message EventNominationStaked {
  uint64 nomination_id = 1;
  string staker = 2;
  string amount = 3;
}

message EventNominationUnstaked {
  uint64 nomination_id = 1;
  string staker = 2;
  string amount = 3;
}

message EventRetroRewardGranted {
  uint64 nomination_id = 1;
  string content_ref = 2;
  string recipient = 3;              // Nominator who receives the reward
  string reward_amount = 4;          // DREAM minted
  string conviction_score = 5;
  uint32 rank = 6;                   // Position among rewarded nominations
  uint32 season = 7;
}

message EventRetroRewardsProcessed {
  uint32 season = 1;
  uint32 nominations_rewarded = 2;
  string total_dream_minted = 3;
}

message EventNominationStakesReturned {
  uint32 season = 1;
  uint32 stakes_returned = 2;
  string total_dream_returned = 3;
}
```

## Phased Implementation Plan

**Total Estimated Duration:** 6-8 weeks

### Phase 1: Core Infrastructure (Week 1-2)

**Deliverables:**
1. Module scaffold, store keys, params
2. Season state management and status machine
3. Epoch system
4. Genesis import/export

**Tests:** Epoch calculations, season state transitions

---

### Phase 2: Member Profiles & XP (Week 2-3)

**Deliverables:**
1. MemberProfile state and CRUD
2. XP granting with caps
3. Level calculation
4. Display names (basic validation)

**Tests:** XP caps, level thresholds, name validation

---

### Phase 3: Hooks Integration (Week 3-4)

**Deliverables:**
1. SeasonHooks interface
2. Governance hooks (OnVoteCast, OnProposalCreated)
3. Mentorship hooks (OnMemberEstablished, OnInitiativeCompleted)
4. Accountability hooks (OnChallengeResolved, OnJuryDutyCompleted)
5. Vote XP de-duplication
6. GrantXP() for direct callers (x/forum)

**Tests:** Hook caller validation, XP de-duplication, anti-gaming caps

---

### Phase 4: Achievements & Titles (Week 4-5)

**Deliverables:**
1. Achievement system with requirements
2. Title system
3. Auto-grant on milestones
4. Default achievements and titles

**Tests:** Achievement unlock logic, title requirements

---

### Phase 5: Quests (Week 5)

**Deliverables:**
1. Quest state with versioning
2. Quest CRUD (governance-gated)
3. Quest progress tracking
4. Reward claiming

**Tests:** Quest lifecycle, version handling

---

### Phase 6: Guilds (Week 5-6)

**Deliverables:**
1. Guild CRUD (create, update description, dissolve)
2. Membership management with hop cooldown
3. Officer management (promote, demote, kick)
4. Invitation system (invite, accept, revoke)
5. Founder succession (simplified)
6. Guild XP accumulation for leaderboards

**Tests:** Guild lifecycle, hop prevention, officer permissions, XP accumulation

---

### Phase 7: Season Transitions (Week 6-7)

**Deliverables:**
1. Transition state machine
2. Batched processing for all phases
3. Maintenance mode
4. Season extension

**Tests:** Full transition simulation, batch correctness

---

### Phase 8: Retroactive Public Goods (Week 7-8)

**Deliverables:**
1. Nomination state objects and KVStore layout
2. `MsgNominate`, `MsgStakeNomination`, `MsgUnstakeNomination` message handlers
3. Nomination window trigger in BeginBlocker (`ACTIVE` → `NOMINATION` transition)
4. `PhaseRetroRewards` and `PhaseReturnNominationStakes` transition phases
5. Conviction calculation and reward distribution logic
6. Nomination queries
7. Integration with `x/rep.MintDream` for reward minting
8. Content validation via `x/blog`, `x/forum`, `x/collect` keepers

**Tests:** Nomination lifecycle, conviction calculation, reward distribution, anti-gaming (self-staking prevention, per-member nomination limits), stake return correctness

---

### Phase 9: Leaderboards & Polish (Week 8-9)

**Deliverables:**
1. Leaderboard tracking and finalization
2. Seasonal title grants
3. Security review
4. Full integration tests

**Tests:** Leaderboard accuracy, seasonal awards

---

### Dependencies

```
                  x/name (required for names)
                       │
Phase 1 (Core)         │
    │                  │
    ▼                  │
Phase 2 (Profiles/XP) ◄┘
    │
    ▼
Phase 3 (Hooks) ◄─── x/gov, x/forum, x/rep
    │
    ├──────────┬──────────┐
    ▼          ▼          ▼
Phase 4    Phase 5    Phase 6
(Achieve)  (Quests)  (Guilds) ◄── x/name (guild names)
    │          │          │
    └──────────┴──────────┘
               │
               ▼
         Phase 7 (Transitions)
               │
               ▼
         Phase 8 (Retro PGF) ◄── x/rep (DREAM minting), x/blog, x/forum, x/collect (content validation)
               │
               ▼
         Phase 9 (Polish)
```

**External Dependencies:**
- `x/name` must be implemented before Phase 2 (display names) and Phase 6 (guilds)
- `x/forum` hooks can be stubbed initially if forum module isn't ready
- `x/commons` authority checks required for admin messages
- `x/rep` DREAM minting required for Phase 8 (retroactive rewards)
- `x/blog`, `x/forum`, `x/collect` content queries required for Phase 8 (content validation)

### Risk Assessment

| Phase | Risk | Mitigation |
|-------|------|------------|
| 1-2 | Low | Standard patterns |
| 3 | Medium | Hook authorization critical |
| 4-6 | Low | Straightforward state management |
| 7 | Medium | Batch processing needs testing |
| 8 | Medium | Conviction math reuses x/rep patterns; cross-module content validation needs careful keeper wiring |
| 9 | Low | Polish phase |
