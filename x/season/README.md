# `x/season`

The `x/season` module is the gamification and seasonal state management engine for Spark Dream. It manages seasonal time periods, reputation archival, XP/leveling, achievements, titles, guilds, quests, and retroactive public goods funding via conviction-weighted nominations.

## Overview

This module provides:

- **Seasonal cycles** — ~5-month seasons with automated state transitions and multi-phase resets
- **Reputation archival** — seasonal scores reset; lifetime reputation archived
- **XP and leveling** — experience points earned from governance participation, with seasonal and lifetime levels
- **Achievements and titles** — governance-created milestones and cosmetic titles with rarity tiers
- **Guilds** — member-created groups with roles, invitations, and seasonal membership
- **Quests** — governance-defined objectives with XP rewards and quest chains
- **Retroactive public goods funding** — nomination window where members stake DREAM conviction on valuable contributions, with top nominees receiving DREAM rewards
- **Display name management** — cosmetic names with moderation and DREAM-staked appeals

## Concepts

### Season Lifecycle

```
ACTIVE ──────────────────────────► NOMINATION (last ~5 epochs)
                                        │
                                        ▼
                                    ENDING
                                        │
     ┌──────────────────────────────────┘
     ▼
 TRANSITION PHASES (9 phases, ~1 week):
 1. RETRO_REWARDS           — distribute DREAM to top nominees
 2. RETURN_NOMINATION_STAKES — unlock nomination stakes
 3. SNAPSHOT                 — create member snapshots
 4. ARCHIVE_REPUTATION       — move seasonal rep to lifetime
 5. RESET_REPUTATION         — reset seasonal scores to baseline
 6. RESET_XP                 — reset season XP (preserve lifetime)
 7. TITLES                   — archive seasonal titles
 8. CLEANUP                  — clean expired quests, trackers
 9. COMPLETE                 — finalize transition
     │
     ▼
 New season activates
```

Transitions process in batches (100 members/block) to prevent block time spikes. Recovery mode with retry/skip available for governance if a phase fails.

### Retroactive Public Goods Funding

During the nomination window (last ~5 epochs of a season):

1. **Members nominate** content (blog posts, forum threads, collections, initiatives) — max 3 nominations per member per season
2. **Members stake DREAM** on nominations to signal conviction — min 10 DREAM, locked until transition
3. **Conviction grows over time**: `conviction = stake * min(1.0, elapsed / (2 * halfLife))` with 3-epoch half-life
4. **At transition**: top nominations (by conviction) meeting min threshold receive proportional DREAM rewards from the season budget (50,000 DREAM, max 20 recipients)
5. **Stakes returned** to nominators regardless of outcome (conviction staking is not payment)

### Content Validation

Nominations reference content via format strings validated against actual module state:
- `blog/post/{id}` — validated via `x/blog`
- `forum/post/{id}` — validated via `x/forum`
- `collect/collection/{id}` — validated via `x/collect`
- `rep/initiative/{id}` — validated via `x/rep`

### XP System

XP is earned from governance participation (not from DREAM-earning activities, to avoid double-counting):

| Action | XP | Cap |
|--------|-----|-----|
| Vote cast | 5 | 10/epoch |
| Proposal created | 10 | — |
| Forum reply received | 2 | 50/epoch from forum |
| Forum marked helpful | 5 | 50/epoch from forum |
| Invitee's first initiative | 20 | — |
| Invitee reaches ESTABLISHED | 50 | — |

Overall cap: 200 XP per epoch. Anti-gaming: account age requirement (7 epochs), reciprocal cooldown (1 epoch), self-reply cooldown (3 epochs), no self-reply XP.

### Levels

Thresholds: [0, 100, 300, 600, 1,000, 1,500, 2,100, 2,800, 3,600, 4,500] XP

- **Season level**: resets each season
- **Lifetime level**: persists across seasons (based on total XP)

### Achievements

Governance-created milestones with rarity tiers (COMMON, UNCOMMON, RARE, EPIC, LEGENDARY, UNIQUE). Requirement types include: initiatives completed, reputation earned, invitations successful, challenges won, jury duty, seasons active, votes cast, and more.

### Titles

Cosmetic labels with seasonal and permanent variants:

- **Seasonal titles**: archived at season end (e.g., "Season 5: Elite Contributor")
- **Permanent titles**: persist across seasons
- One active display title per member; up to 50 displayable + 200 archived

### Guilds

Member-created groups:

- **Creation cost**: 100 DREAM (burned)
- **Roles**: Founder (1), Officers (up to 5), Members (3-100)
- **Membership**: one guild per season, max 3 guild hops per season, cooldown of 30 epochs
- **Lifecycle**: ACTIVE → FROZEN → DISSOLVED
- **Invitations**: TTL of ~1 month (30 epochs), max 20 pending per guild
- **Minimum age**: 7 epochs before dissolution allowed
- **Description**: up to 500 characters
- **Founder transfer**: ownership can be transferred or claimed if founder leaves

### Display Names

- Cosmetic names (1-50 chars) with 1-epoch change cooldown
- Unique @usernames (3-20 chars, alphanumeric) costing 10 DREAM with 30-epoch cooldown
- DREAM-staked moderation: report (50 DREAM) → appeal (100 DREAM, ~7 day window) → governance resolution
- Unappealed moderations can be resolved after appeal period expires

## State

### Singleton Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Season` | singleton | Current season number, name, theme, start/end blocks, status |
| `SeasonTransitionState` | singleton | Phase, progress, maintenance mode |
| `TransitionRecoveryState` | singleton | Recovery mode tracking for failed transitions |
| `NextSeasonInfo` | singleton | Pre-set name/theme for next season |

### Per-Season / Per-Member Objects

| Object | Key | Description |
|--------|-----|-------------|
| `SeasonSnapshot` | `season_snapshot/{season}` | End-of-season aggregate statistics |
| `MemberProfile` | `profile/{address}` | XP, level, titles, achievements, guild, display name |
| `MemberSeasonSnapshot` | `snapshot/{season_address}` | End-of-season member state |
| `MemberRegistration` | `registration/{member}` | Member registration metadata |

### Achievements, Titles, and XP Tracking

| Object | Key | Description |
|--------|-----|-------------|
| `Achievement` | `achievement/{id}` | Governance-created milestones |
| `Title` | `title/{id}` | Governance-created cosmetic titles |
| `SeasonTitleEligibility` | `title_eligibility/{title_season}` | Per-season title eligibility criteria |
| `EpochXpTracker` | `epoch_xp/{member_epoch}` | Per-member per-epoch XP tracking |
| `VoteXpRecord` | `vote_xp/{season_member_proposal}` | Prevents duplicate vote XP |
| `ForumXpCooldown` | `forum_cooldown/{beneficiary_actor}` | Anti-gaming cooldowns for forum XP |

### Guilds

| Object | Key | Description |
|--------|-----|-------------|
| `Guild` | `guild/{id}` | Guild metadata, roster, status |
| `GuildMembership` | `guild_membership/{member}` | Member's current guild association |
| `GuildInvite` | `guild_invite/{guild_invitee}` | Pending guild invitations |

### Quests

| Object | Key | Description |
|--------|-----|-------------|
| `Quest` | `quest/{id}` | Governance-defined objectives |
| `MemberQuestProgress` | `quest_progress/{member_quest}` | Per-member quest state |

### Display Name Moderation

| Object | Key | Description |
|--------|-----|-------------|
| `DisplayNameModeration` | `moderation/{member}` | Active moderation state |
| `DisplayNameReportStake` | `report_stake/{challenge_id}` | Reporter's DREAM stake |
| `DisplayNameAppealStake` | `appeal_stake/{challenge_id}` | Appellant's DREAM stake |

### Retroactive Funding

| Object | Key | Description |
|--------|-----|-------------|
| `Nomination` | `nomination/{id}` | Retroactive funding nomination |
| `NominationStake` | `nomination_stake/{nominationId}/{staker}` | DREAM staked on nomination |
| `RetroRewardRecord` | `retro_reward/{season}/{id}` | Historical reward distribution |

## Messages

### Profile Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgSetDisplayName` | Set cosmetic name (1-epoch cooldown) | Any member |
| `MsgSetUsername` | Set unique @handle (costs 10 DREAM, 30-epoch cooldown) | Any member |
| `MsgSetDisplayTitle` | Equip an unlocked title | Any member |

### Guild Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateGuild` | Create guild (burns 100 DREAM) | Any member |
| `MsgJoinGuild` | Join public guild | Any member |
| `MsgLeaveGuild` | Leave guild | Guild member (not founder) |
| `MsgInviteToGuild` | Invite to invite-only guild | Founder/officer |
| `MsgAcceptGuildInvite` | Accept invite | Invitee |
| `MsgRevokeGuildInvite` | Revoke pending invite | Founder/officer |
| `MsgSetGuildInviteOnly` | Toggle invite-only mode | Founder/officer |
| `MsgUpdateGuildDescription` | Update guild description | Founder/officer |
| `MsgKickFromGuild` | Remove member | Founder/officer |
| `MsgPromoteToOfficer` / `MsgDemoteOfficer` | Manage officers | Founder |
| `MsgTransferGuildFounder` | Transfer ownership | Founder |
| `MsgClaimGuildFounder` | Claim founder role (if founder left) | Any guild member |
| `MsgDissolveGuild` | Dissolve guild | Founder |

### Quest Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgStartQuest` | Begin a quest | Any member meeting prerequisites |
| `MsgClaimQuestReward` | Claim XP on completion | Quest completer |
| `MsgAbandonQuest` | Abandon in-progress quest | Quest participant |
| `MsgCreateQuest` | Create quest | Governance |
| `MsgUpdateQuest` | Update quest | Governance |
| `MsgDeactivateQuest` | Deactivate quest | Governance |

### Retroactive Public Goods Funding

| Message | Description | Access |
|---------|-------------|--------|
| `MsgNominate` | Nominate content (max 3/member/season) | ESTABLISHED+ member |
| `MsgStakeNomination` | Stake DREAM on nomination (locks DREAM) | PROVISIONAL+ member |
| `MsgUnstakeNomination` | Remove stake (only during nomination phase) | Staker |

### Season Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgExtendSeason` | Extend season (max 3 extensions, 2 weeks each) | Governance |
| `MsgSetNextSeasonInfo` | Set name/theme for next season | Governance |
| `MsgAbortSeasonTransition` | Abort during transition | Governance |
| `MsgRetrySeasonTransition` | Retry after failure | Governance |
| `MsgSkipTransitionPhase` | Skip failed phase (recovery mode only) | Governance |

### Achievement/Title Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateAchievement` / `MsgUpdateAchievement` / `MsgDeleteAchievement` | Achievement CRUD | Governance |
| `MsgCreateTitle` / `MsgUpdateTitle` / `MsgDeleteTitle` | Title CRUD | Governance |

### Display Name Moderation

| Message | Description | Access |
|---------|-------------|--------|
| `MsgReportDisplayName` | Report inappropriate name (50 DREAM stake) | Any member |
| `MsgAppealDisplayNameModeration` | Appeal moderation (100 DREAM stake) | Affected member |
| `MsgResolveDisplayNameAppeal` | Resolve appeal | Governance |
| `MsgResolveUnappealedModeration` | Resolve expired unappealed moderation | Governance |

### Parameter Updates

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update governance-controlled parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Operations Committee |

## Queries

### Season State

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `CurrentSeason` | Active season details |
| `SeasonByNumber` | Historical season lookup |
| `SeasonStats` | Aggregate season statistics |
| `GetSeason` / `GetSeasonTransitionState` / `GetTransitionRecoveryState` / `GetNextSeasonInfo` | Singleton state |

### Member Profiles

| Query | Description |
|-------|-------------|
| `GetMemberProfile` / `ListMemberProfile` | Profile with XP, titles, achievements, guild |
| `MemberByDisplayName` | Lookup by display name |
| `MemberSeasonHistory` | Season snapshots and achievements |
| `MemberXpHistory` | XP tracking over time |
| `GetMemberRegistration` / `ListMemberRegistration` | Registration metadata |
| `GetSeasonSnapshot` / `ListSeasonSnapshot` | Season-level snapshots |
| `GetMemberSeasonSnapshot` / `ListMemberSeasonSnapshot` | Per-member season snapshots |

### Achievements and Titles

| Query | Description |
|-------|-------------|
| `Achievements` / `GetAchievement` / `ListAchievement` | Achievement queries |
| `MemberAchievements` | Achievements earned by member |
| `Titles` / `GetTitle` / `ListTitle` | Title queries |
| `MemberTitles` | Titles held by member |
| `GetSeasonTitleEligibility` / `ListSeasonTitleEligibility` | Per-season title eligibility |

### Guilds

| Query | Description |
|-------|-------------|
| `GuildsList` / `GuildById` / `GetGuild` / `ListGuild` | Guild queries |
| `GuildsByFounder` | Filter by founder (with include-dissolved option) |
| `GuildMembers` | Members of a guild |
| `MemberGuild` / `GetGuildMembership` / `ListGuildMembership` | Member's guild state |
| `GuildInvites` / `MemberGuildInvites` / `GetGuildInvite` / `ListGuildInvite` | Invitation queries |

### Quests

| Query | Description |
|-------|-------------|
| `QuestsList` / `QuestById` / `GetQuest` / `ListQuest` | Quest queries |
| `QuestChain` | All quests in a chain |
| `AvailableQuests` | Quests available for a member |
| `MemberQuestStatus` / `GetMemberQuestProgress` / `ListMemberQuestProgress` | Quest progress |

### XP Tracking

| Query | Description |
|-------|-------------|
| `GetEpochXpTracker` / `ListEpochXpTracker` | Per-epoch XP tracking |
| `GetVoteXpRecord` / `ListVoteXpRecord` | Vote XP records |
| `GetForumXpCooldown` / `ListForumXpCooldown` | Forum XP cooldowns |

### Display Name Moderation

| Query | Description |
|-------|-------------|
| `GetDisplayNameModeration` / `ListDisplayNameModeration` | Moderation state |
| `GetDisplayNameReportStake` / `ListDisplayNameReportStake` | Report stakes |
| `GetDisplayNameAppealStake` / `ListDisplayNameAppealStake` | Appeal stakes |

### Retroactive Funding

| Query | Description |
|-------|-------------|
| `GetNomination` / `ListNominations` | Nomination queries |
| `ListNominationsByCreator` | Nominations by creator |
| `ListNominationStakes` | Stakes on a nomination |
| `ListRetroRewardHistory` | Rewards distributed in a season |

## Parameters

### Governance-Only (via `MsgUpdateParams`)

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `level_thresholds` | []uint64 | [0..4500] | XP required per level (10 tiers) |
| `baseline_reputation` | LegacyDec | 0.5 | Starting reputation score |
| `max_guild_members` | uint64 | 100 | Max members per guild |
| `snapshot_retention_seasons` | uint64 | 10 | Historical data retention |
| `epoch_tracker_retention_epochs` | uint64 | 30 | Epoch tracker retention |
| `vote_xp_record_retention_seasons` | uint64 | 2 | Vote XP record retention |
| `forum_cooldown_retention_epochs` | uint64 | 30 | Forum cooldown retention |
| `max_transition_epochs` | uint64 | 7 | Max epochs for transition process |
| `transition_max_retries` | uint64 | 3 | Max retries for failed transitions |

### Operational (via `MsgUpdateOperationalParams`)

#### Season Timing

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `epoch_blocks` | uint64 | 17,280 | Blocks per epoch (~1 day) |
| `season_duration_epochs` | uint64 | 150 | ~5 months |
| `season_transition_epochs` | uint64 | 7 | ~1 week for transition |
| `nomination_window_epochs` | uint64 | 5 | ~1 week before season end |
| `max_season_extensions` | uint64 | 3 | Extension limit |
| `max_extension_epochs` | uint64 | 14 | ~2 weeks max per extension |
| `transition_batch_size` | uint64 | 100 | Members processed per block |
| `transition_grace_period` | int64 | 50,400 | Grace period in blocks (~1 week) |

#### XP Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `xp_vote_cast` | uint64 | 5 | XP per vote |
| `xp_proposal_created` | uint64 | 10 | XP per proposal |
| `xp_forum_reply_received` | uint64 | 2 | XP per forum reply received |
| `xp_forum_marked_helpful` | uint64 | 5 | XP per marked-helpful reply |
| `xp_invitee_first_initiative` | uint64 | 20 | XP when invitee completes first initiative |
| `xp_invitee_established` | uint64 | 50 | XP when invitee reaches ESTABLISHED |
| `max_vote_xp_per_epoch` | uint64 | 10 | Vote XP cap per epoch |
| `max_forum_xp_per_epoch` | uint64 | 50 | Forum XP cap per epoch |
| `max_xp_per_epoch` | uint64 | 200 | Overall XP cap |
| `forum_xp_min_account_age_epochs` | uint64 | 7 | Min account age for forum XP |
| `forum_xp_reciprocal_cooldown` | uint64 | 1 | Reciprocal forum XP cooldown (epochs) |
| `forum_xp_self_reply_cooldown` | uint64 | 3 | Self-reply forum XP cooldown (epochs) |

#### Guilds

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `guild_creation_cost` | Int | 100 DREAM | Burned on creation |
| `min_guild_members` | uint64 | 3 | Minimum members |
| `max_guild_officers` | uint64 | 5 | Officer cap |
| `guild_hop_cooldown_epochs` | uint64 | 30 | ~1 month between guild changes |
| `max_guilds_per_season` | uint64 | 3 | Max guild hops per season |
| `min_guild_age_epochs` | uint64 | 7 | Min age before dissolution |
| `max_pending_invites` | uint64 | 20 | Max pending invites per guild |
| `guild_description_max_length` | uint64 | 500 | Max description length |
| `guild_invite_ttl_epochs` | uint64 | 30 | Invite expiration (~1 month) |

#### Display Names

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `display_name_min_length` | uint64 | 1 | Min display name length |
| `display_name_max_length` | uint64 | 50 | Max display name length |
| `display_name_change_cooldown` | uint64 | 1 | Epochs between changes |
| `display_name_report_stake` | Int | 50 DREAM | Reporter's stake |
| `display_name_appeal_stake` | Int | 100 DREAM | Appellant's stake |
| `display_name_appeal_period` | int64 | 100,800 | Appeal window in blocks (~7 days) |
| `username_min_length` | uint64 | 3 | Min username length |
| `username_max_length` | uint64 | 20 | Max username length |
| `username_change_cooldown` | uint64 | 30 | Epochs between changes |
| `username_cost_dream` | Int | 10 DREAM | Cost to set username |

#### Quests

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `max_quest_objectives` | uint64 | 5 | Max objectives per quest |
| `max_quest_xp_reward` | uint64 | 100 | Max XP reward per quest |
| `max_active_quests_per_member` | uint64 | 10 | Active quest cap |
| `max_objective_desc_length` | uint64 | 200 | Max objective description |

#### Titles

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `max_displayable_titles` | uint64 | 50 | Max titles per member |
| `max_archived_titles` | uint64 | 200 | Max archived titles |

#### Nominations

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `max_nominations_per_member` | uint64 | 3 | Per season |
| `nomination_min_trust_level` | uint64 | 2 | ESTABLISHED for nominating |
| `nomination_stake_min_trust_level` | uint64 | 1 | PROVISIONAL for staking |
| `nomination_min_stake` | LegacyDec | 10 DREAM | Minimum per stake |
| `nomination_rationale_max_length` | uint64 | 500 | Max rationale length |
| `retro_reward_budget_per_season` | LegacyDec | 50,000 DREAM | Total reward pool |
| `retro_reward_max_recipients` | uint64 | 20 | Top nominations funded |
| `retro_reward_min_conviction` | LegacyDec | 50 | Minimum to qualify |
| `nomination_conviction_half_life_epochs` | uint64 | 3 | Conviction growth rate |

#### Cleanup

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `invite_cleanup_interval` | int64 | 100 | Blocks between invite cleanup runs |
| `invite_cleanup_batch_size` | uint64 | 50 | Invites processed per cleanup |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec |
| `x/bank` | Yes | Fee collection |
| `x/rep` | Yes | Member validation, DREAM operations, reputation archival, trust levels |
| `x/name` | No | Username and guild name uniqueness |
| `x/commons` | No | Authority checks for governance-gated messages |
| `x/blog` | No | Content validation for nominations |
| `x/forum` | No | Content validation for nominations |
| `x/collect` | No | Content validation for nominations |

### Depinject vs Late Wiring

- **Depinject**: Auth, Bank, Rep (`optional`), Commons (`optional`), Name (`optional`)
- **Late wiring via `app.go`**: Blog, Forum, Collect — excluded from depinject to break cycles (`season → blog/forum/collect → rep → season`)

## BeginBlocker

1. **Auto-resolve expired display name moderations** — uphold report if appeal period expired
2. **Check for nomination phase entry** — transition to NOMINATION status when window opens
3. **Season end check** — if current block >= end_block, start transition
4. **Season transition processing** — continue current phase in batches (100 members/block):
   - RETRO_REWARDS → RETURN_NOMINATION_STAKES → SNAPSHOT → ARCHIVE_REPUTATION → RESET_REPUTATION → RESET_XP → TITLES → CLEANUP → COMPLETE
5. **Finalize transition** — create new season, activate it

Critical phases (ARCHIVE_REP, RESET_REP, RESET_XP) enable maintenance mode, blocking nomination/staking operations for data consistency. Failed phases trigger recovery mode with retry/skip governance actions.

## Events

All state-changing operations emit typed events for indexing and client notification.

## Client

### CLI

```bash
# Profile
sparkdreamd tx season set-display-name "Phoenix" --from alice
sparkdreamd tx season set-username "phoenix42" --from alice
sparkdreamd tx season set-display-title "Elite Contributor" --from alice

# Guilds
sparkdreamd tx season create-guild "Builders" "A guild for builders" false --from alice
sparkdreamd tx season join-guild 1 --from bob
sparkdreamd tx season invite-to-guild 1 [invitee] --from alice
sparkdreamd tx season accept-guild-invite 1 --from invitee
sparkdreamd tx season kick-from-guild 1 [member] "reason" --from alice

# Quests
sparkdreamd tx season start-quest 1 --from alice
sparkdreamd tx season claim-quest-reward 1 --from alice

# Nominations
sparkdreamd tx season nominate "blog/post/42" "Outstanding contribution to governance docs" --from alice
sparkdreamd tx season stake-nomination 1 100 --from bob
sparkdreamd tx season unstake-nomination 1 --from bob

# Queries
sparkdreamd q season current-season
sparkdreamd q season get-member-profile [address]
sparkdreamd q season guilds-list
sparkdreamd q season quests-list
sparkdreamd q season list-nominations
sparkdreamd q season list-retro-reward-history 1
sparkdreamd q season params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/season/v1/query.proto` for the full API surface.
