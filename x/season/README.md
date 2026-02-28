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
 TRANSITION PHASES (7 phases, ~1 week):
 1. RETRO_REWARDS      — distribute DREAM to top nominees
 2. RETURN_STAKES      — unlock nomination stakes
 3. SNAPSHOT           — create member snapshots
 4. ARCHIVE_REPUTATION — move seasonal rep to lifetime
 5. RESET_REPUTATION   — reset seasonal scores to baseline
 6. RESET_XP           — reset season XP (preserve lifetime)
 7. TITLES/CLEANUP     — archive seasonal titles, clean quests
     │
     ▼
 COMPLETED → New season activates
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

Overall cap: 200 XP per epoch. Anti-gaming: account age requirement, reciprocal cooldown, no self-reply XP.

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
- **Membership**: one guild per season, max 3 guild hops per season
- **Lifecycle**: ACTIVE → FROZEN → DISSOLVED
- **Invitations**: TTL of ~1 month, max 20 pending per guild

### Display Names

- Cosmetic names (1-50 chars) with 1-epoch change cooldown
- Unique @usernames (3-20 chars, alphanumeric) costing 10 DREAM with 30-epoch cooldown
- DREAM-staked moderation: report (50 DREAM) → appeal (100 DREAM) → governance resolution

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Season` | singleton | Current season number, name, theme, start/end blocks, status |
| `SeasonTransitionState` | singleton | Phase, progress, maintenance mode |
| `MemberProfile` | `profile/{address}` | XP, level, titles, achievements, guild, display name |
| `MemberSeasonSnapshot` | `snapshot/{season}/{address}` | End-of-season member state |
| `Guild` | `guild/{id}` | Guild metadata, roster, status |
| `Achievement` | `achievement/{id}` | Governance-created milestones |
| `Title` | `title/{id}` | Governance-created cosmetic titles |
| `Quest` | `quest/{id}` | Governance-defined objectives |
| `MemberQuestProgress` | `quest_progress/{member}/{quest_id}` | Per-member quest state |
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
| `MsgLeaveGuild` | Leave guild | Guild member |
| `MsgInviteToGuild` | Invite to invite-only guild | Founder/officer |
| `MsgAcceptGuildInvite` | Accept invite | Invitee |
| `MsgKickFromGuild` | Remove member | Founder/officer |
| `MsgPromoteToOfficer` / `MsgDemoteOfficer` | Manage officers | Founder |
| `MsgTransferGuildFounder` | Transfer ownership | Founder |
| `MsgDissolveGuild` | Dissolve guild | Founder |

### Quest Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgStartQuest` | Begin a quest | Any member meeting prerequisites |
| `MsgClaimQuestReward` | Claim XP on completion | Quest completer |
| `MsgAbandonQuest` | Abandon in-progress quest | Quest participant |
| `MsgCreateQuest` / `MsgUpdateQuest` / `MsgDeactivateQuest` | Quest CRUD | Governance |

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

### Parameter Updates

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update governance-controlled parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Operations Committee |

## Queries

| Query | Description |
|-------|-------------|
| `CurrentSeason` | Active season details |
| `SeasonByNumber` | Historical season lookup |
| `GetMemberProfile` | Member profile with XP, titles, achievements, guild |
| `MemberSeasonHistory` | Season snapshots and achievements |
| `MemberXpHistory` | XP tracking over time |
| `Achievements` / `MemberAchievements` | Achievement queries |
| `Titles` / `MemberTitles` | Title queries |
| `GuildsList` / `GuildById` / `GuildMembers` | Guild queries |
| `MemberGuild` / `MemberGuildInvites` | Member guild state |
| `QuestsList` / `AvailableQuests` / `MemberQuestStatus` | Quest queries |
| `GetNomination` / `ListNominations` | Nomination queries |
| `ListNominationStakes` | Stakes on a nomination |
| `ListRetroRewardHistory` | Rewards distributed in a season |
| `GetDisplayNameModeration` | Moderation status |

## Parameters

### Season Timing

| Parameter | Default | Description |
|-----------|---------|-------------|
| `epoch_blocks` | 17,280 | Blocks per epoch (~1 day) |
| `season_duration_epochs` | 150 | ~5 months |
| `season_transition_epochs` | 7 | ~1 week for transition |
| `nomination_window_epochs` | 5 | ~1 week before season end |

### XP Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| `xp_vote_cast` | 5 | XP per vote |
| `xp_proposal_created` | 10 | XP per proposal |
| `max_xp_per_epoch` | 200 | Overall XP cap |
| `level_thresholds` | [0..4500] | XP required per level |

### Guilds

| Parameter | Default | Description |
|-----------|---------|-------------|
| `guild_creation_cost` | 100 DREAM | Burned on creation |
| `min_guild_members` / `max_guild_members` | 3 / 100 | Size constraints |
| `max_guild_officers` | 5 | Officer cap |
| `guild_hop_cooldown_epochs` | 30 | ~1 month between guild changes |

### Nominations

| Parameter | Default | Description |
|-----------|---------|-------------|
| `max_nominations_per_member` | 3 | Per season |
| `nomination_min_trust_level` | 2 | ESTABLISHED |
| `nomination_min_stake` | 10 DREAM | Minimum per stake |
| `retro_reward_budget_per_season` | 50,000 DREAM | Total reward pool |
| `retro_reward_max_recipients` | 20 | Top nominations funded |
| `retro_reward_min_conviction` | 50 | Minimum to qualify |
| `nomination_conviction_half_life_epochs` | 3 | Conviction growth rate |

### Transition

| Parameter | Default | Description |
|-----------|---------|-------------|
| `transition_batch_size` | 100 | Members processed per block |
| `max_season_extensions` | 3 | Extension limit |
| `max_extension_epochs` | 14 | ~2 weeks max per extension |
| `snapshot_retention_seasons` | 10 | Historical data retention |

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

## BeginBlocker

1. **Auto-resolve expired display name moderations** — uphold report if appeal period expired
2. **Check for nomination phase entry** — transition to NOMINATION status when window opens
3. **Season transition processing** — continue current phase in batches (100 members/block)
4. **Season end check** — if current block >= end_block, start transition

Critical phases (ARCHIVE_REP, RESET_REP, RESET_XP) enable maintenance mode, blocking nomination/staking operations for data consistency.

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
sparkdreamd tx season create-guild --name "Builders" --from alice
sparkdreamd tx season join-guild 1 --from bob

# Quests
sparkdreamd tx season start-quest 1 --from alice
sparkdreamd tx season claim-quest-reward 1 --from alice

# Nominations
sparkdreamd tx season nominate "blog/post/42" --rationale "..." --from alice
sparkdreamd tx season stake-nomination 1 --amount 100 --from bob

# Queries
sparkdreamd q season current-season
sparkdreamd q season get-member-profile [address]
sparkdreamd q season guilds-list
sparkdreamd q season list-nominations
sparkdreamd q season params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/season/v1/query.proto` for the full API surface.
