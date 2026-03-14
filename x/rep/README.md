# `x/rep`

The `x/rep` module is the core coordination engine of Spark Dream, implementing a reputation-based task system with DREAM token economics, conviction voting, human-verified accountability, and progressive trust levels.

## Overview

This module provides:

- **Member lifecycle** — invitation-based onboarding with accountability, five trust levels, "zeroing" instead of permanent bans
- **DREAM token** — internal earned token with minting, burning, limited transfers, lazy unstaked decay
- **Reputation system** — per-tag scores with seasonal resets, lifetime archive, and anti-gaming caps
- **Invitation system** — staked, accountable invitations with referral rewards
- **Projects and initiatives** — council-approved budgets with tiered, conviction-based work completion
- **Conviction staking** — time-weighted stakes on initiatives, projects, members, tags, and content
- **Challenge system** — challenges with jury resolution (anonymous challenges via x/shield)
- **Content challenges** — cross-module quality assurance via author bonds
- **Interim work** — fixed-rate delegated duties (jury duty, moderation, expert review)
- **MasterChef staking rewards** — epoch-based reward pools for member, tag, and project staking
- **ZK trust tree** — persistent sparse Merkle tree for `x/shield` ZK proof validation

## Concepts

### Members and Trust Levels

Members progress through five trust levels by earning reputation and completing interim work:

| Level | Min Reputation | Min Interims | Invitation Credits |
|-------|---------------|-------------|-------------------|
| NEW | 0 | 0 | 0 |
| PROVISIONAL | 50 | 3 | 1 |
| ESTABLISHED | 200 | 10 | 3 |
| TRUSTED | 500 | 1 season | 5 |
| CORE | 1,000 | 2 seasons | 10 |

Member statuses: ACTIVE, INACTIVE, ZEROED. Zeroing burns all DREAM, zeroes reputation, and resets trust level — but the person can restart with a new address and new invitation ("punish position, not person").

### DREAM Token

DREAM is the internal earned token:

- **Minting**: initiative completion (primary), staking rewards, interim compensation, retroactive public goods
- **Burning**: slashing, failed challenges, failed invitations, unstaked decay (1%/epoch), transfer tax (3%)
- **Transfers**: tips (max 100 DREAM, 10/epoch), gifts (max 500 DREAM, invitees only, cooldown per recipient), bounties (escrowed)
- **No external trading**, no IBC transfer

**Lazy decay**: unstaked DREAM decays at 1%/epoch, applied lazily via `GetMember()` for O(1) scaling.

### Reputation System

- **Per-tag scores**: members earn reputation in specific domain tags (e.g., "smart-contracts", "governance")
- **Seasonal reset**: reputation resets at start of each season (~5 months); lifetime archive preserved
- **Decay**: 0.5% per epoch during season (applied lazily)
- **Anti-gaming cap**: max 50 reputation per tag per epoch

### Invitation System

Inviters stake DREAM to create accountable invitations:

- Stake locked from inviter, returned (minus 10% burn) when invitee accepts
- If invitee is zeroed during accountability period (~5 months), inviter is slashed
- Inviter receives 5% of invitee's earnings during referral period
- Invitation credits reset per season based on trust level
- Cost multiplier: 1.1x per additional invitation

### Projects and Initiatives

**Projects**: council-approved budgets (DREAM + SPARK) with categories and tags.

**Initiatives**: self-selected work under projects with four tiers:

| Tier | Max Budget | Min Rep | Rep Cap | Reward Multiplier |
|------|-----------|---------|---------|-------------------|
| Apprentice | 100 DREAM | 0 | 25 | 0.5x |
| Standard | 500 DREAM | 25 | 100 | 1.0x |
| Expert | 2,000 DREAM | 100 | 500 | 1.5x |
| Epic | 10,000 DREAM | 250 | 1,000 | 2.0x |

### Conviction-Based Completion

Initiatives complete when:

- Total conviction >= threshold (`0.2 * sqrt(budget)`)
- External conviction >= 50% (non-affiliated stakers)
- No active challenges
- Challenge period passed

**Conviction formula**: `sqrt(total_stakes * time * reputation)` with 7-epoch half-life. Older stakes decay exponentially to prevent "set and forget" dominance.

### Stakes

Stakes lock DREAM on various targets to signal conviction:

| Target Type | APY | Description |
|-------------|-----|-------------|
| Initiative | 10% | Signal belief in work quality |
| Project | 8% | Support active projects (5% completion bonus) |
| Member | 5% | Peer support (circular A↔B blocked, no self-stake) |
| Tag | 2% | Domain expertise signal |
| Content | — | Blog/forum/collection conviction |

- Min stake duration: 24 hours
- Max conviction share per member: 33% (prevents single-member dominance)
- Content stakes capped at 10K DREAM per member per item

### Challenge System

Members can challenge initiative work quality:

- **Named challenges**: min 50 DREAM stake, identity public
- **Anonymous challenges**: via `x/shield`'s `MsgShieldedExec` (only `MsgCreateChallenge` is shield-compatible), no DREAM stake, identity hidden, module-paid gas

**Jury resolution**: 5 jurors (odd, configurable), weighted by reputation in relevant tags, 67% supermajority, min 50 reputation to serve. Auto-uphold if assignee doesn't respond within 3 epochs. Successful challenger receives 20% of initiative budget.

### Content Challenges

Cross-module quality assurance via author bonds:

- Content creators stake DREAM to bond their reputation to posts/collections (max 1,000 DREAM)
- Members challenge bonded content; same jury system as initiatives
- Successful challenger gets 50% of slashed bond (minted)
- 10% of content conviction propagates to linked initiatives
- Author bonds slashed on content moderation actions

### Interim Work

Fixed-rate delegated duties:

| Complexity | Compensation |
|-----------|-------------|
| SIMPLE | 50 DREAM |
| STANDARD | 150 DREAM |
| COMPLEX | 400 DREAM |
| EXPERT | 1,000 DREAM |

Types: JURY_DUTY, EXPERT_TESTIMONY, DISPUTE_MEDIATION, PROJECT_APPROVAL, BUDGET_REVIEW, MODERATION. Solo expert bonus: +50%. 7-epoch deadline. Capped reputation per tag per epoch prevents grinding.

### ZK Trust Tree

Persistent KV-based sparse Merkle tree for `x/shield` ZK proof validation:

- Leaves = `MiMC(zk_public_key, trust_level)` for each member with a registered ZK key
- Built incrementally via EndBlocker `MaybeRebuildTrustTree()` (dirty member tracking for O(depth) updates)
- Exposes `GetTrustTreeRoot()` and `GetPreviousTrustTreeRoot()` for stale-proof tolerance

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Member` | `member/value/{address}` | Balance, reputation, trust level, decay tracking, ZK public key |
| `Invitation` | `invitation/value/{id}` | Pending/accepted invitations with accountability |
| `Project` | `project/value/{id}` | Council-approved project budgets |
| `Initiative` | `initiative/value/{id}` | Self-selected work units with conviction tracking |
| `Stake` | `stake/value/{id}` | Conviction/content/author bond stakes |
| `Challenge` | `challenge/value/{id}` | Initiative challenges with jury reference |
| `JuryReview` | `juryreview/value/{id}` | Jury voting on challenges |
| `Interim` | `interim/value/{id}` | Fixed-rate delegated work |
| `InterimTemplate` | `interimtemplate/value/{index}` | Reusable interim work templates |
| `ContentChallenge` | `contentchallenge/value/{id}` | Challenges on bonded content |
| `GiftRecord` | `giftrecord/{sender}/{recipient}` | Gift cooldown tracking |
| `MemberStakePool` | `stake/member_pool/{address}` | Aggregate member stake pool for rewards |
| `TagStakePool` | `stake/tag_pool/{tag}` | Aggregate tag stake pool for rewards |
| `ProjectStakeInfo` | `stake/project_info/{id}` | Project-level stake aggregation |

### Indexes

| Index | Purpose |
|-------|---------|
| `InitiativesByStatus` | Filter by OPEN/SUBMITTED/IN_REVIEW/COMPLETED/etc. |
| `InterimsByStatus` | Filter by PENDING/ASSIGNED/SUBMITTED/COMPLETED/etc. |
| `JuryReviewsByVerdict` | Filter jury reviews by verdict |
| `StakesByTarget` | All stakes on a specific target (type, id) |
| `ChallengesByStatus` | Filter by ACTIVE/IN_JURY_REVIEW/UPHELD/DISMISSED |
| `ContentChallengesByStatus` | Active/resolved content challenges |
| `ContentChallengesByTarget` | Active challenge per content item (type, id) |
| `ContentInitiativeLinks` | Content → initiative conviction propagation |

### Initiative Status Lifecycle

```
OPEN → SUBMITTED → IN_REVIEW → PENDING_COMPLETION → COMPLETED
  │       │           │                │
  │       │           ├── CHALLENGED ──┘
  │       │           │
  └───────┴───────────┴── ABANDONED
```

## Messages

### Membership

| Message | Description | Access |
|---------|-------------|--------|
| `MsgInviteMember` | Create invitation, lock DREAM stake | Members with invitation credits |
| `MsgAcceptInvitation` | Accept invitation, create new member | Invitee |
| `MsgRegisterZkPublicKey` | Register ZK public key for anonymous operations | Any member |

### DREAM Transfers

| Message | Description | Access |
|---------|-------------|--------|
| `MsgTransferDream` | Tip/gift with purpose validation and rate limiting | Members |

### Projects

| Message | Description | Access |
|---------|-------------|--------|
| `MsgProposeProject` | Propose project with budget and tags | Any member |
| `MsgApproveProjectBudget` | Approve and fund project | Committee authority |
| `MsgCancelProject` | Cancel project with reason | Committee authority |

### Initiatives

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateInitiative` | Create initiative under project | Any member |
| `MsgAssignInitiative` | Assign to worker (can't self-assign) | Project authority |
| `MsgSubmitInitiativeWork` | Submit deliverable | Assignee |
| `MsgApproveInitiative` | Confirm completion | Approver |
| `MsgAbandonInitiative` | Abandon work | Assignee |
| `MsgCompleteInitiative` | Finalize after challenge period, mint rewards | Authority |

### Staking

| Message | Description | Access |
|---------|-------------|--------|
| `MsgStake` | Create conviction/content/author bond stake | Members |
| `MsgUnstake` | Partial/full unstake (min 24h duration) | Stake owner |
| `MsgClaimRewards` | Claim accumulated staking rewards | Members |

### Challenges

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateChallenge` | Challenge initiative work (named or anonymous via x/shield) | Members |
| `MsgRespondToChallenge` | Respond to prevent auto-uphold | Assignee |
| `MsgSubmitJurorVote` | Cast jury vote with verdict and confidence | Selected juror |
| `MsgSubmitExpertTestimony` | Provide expert context during review | Domain experts |

### Content Challenges

| Message | Description | Access |
|---------|-------------|--------|
| `MsgChallengeContent` | Challenge bonded content | Members |
| `MsgRespondToContentChallenge` | Author responds to challenge | Content author |

### Interims

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateInterim` | Create delegated work | Committee authority |
| `MsgAssignInterim` | Assign to worker | Authority |
| `MsgSubmitInterimWork` | Submit deliverable | Assignee |
| `MsgApproveInterim` | Approve completion | Authority |
| `MsgAbandonInterim` | Abandon assigned interim | Assignee |
| `MsgCompleteInterim` | Finalize, mint rewards, grant reputation | Authority |

### Parameter Updates

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update all parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Committee authority |

## Queries

### Core Lookups

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `GetMember` / `ListMember` | Member with lazy decay/reputation applied |
| `MembersByTrustLevel` | Filter by trust level |
| `GetInvitation` / `ListInvitation` | Invitation lookup/list |
| `InvitationsByInviter` | Invitations sent by member |

### Projects and Initiatives

| Query | Description |
|-------|-------------|
| `GetProject` / `ListProject` | Project lookup/list |
| `ProjectsByCouncil` | Projects approved by council |
| `GetInitiative` / `ListInitiative` | Initiative lookup/list |
| `InitiativesByProject` | Initiatives under a project |
| `InitiativesByAssignee` | Member's assigned initiatives |
| `AvailableInitiatives` | Open initiatives to claim |
| `InitiativeConviction` | Current conviction score (time-weighted) |

### Staking

| Query | Description |
|-------|-------------|
| `GetStake` / `ListStake` | Stake lookup/list |
| `StakesByStaker` | Stakes placed by member |
| `StakesByTarget` | Stakes on specific target |
| `Reputation` | Member's reputation in a specific tag |

### Challenges

| Query | Description |
|-------|-------------|
| `GetChallenge` / `ListChallenge` | Challenge lookup/list |
| `ChallengesByInitiative` | Challenges on initiative |
| `GetJuryReview` / `ListJuryReview` | Jury review lookup/list |

### Content

| Query | Description |
|-------|-------------|
| `ContentConviction` | Conviction score on content |
| `AuthorBond` | Author bond stake for content |
| `GetContentChallenge` / `ListContentChallenge` | Content challenge lookup/list |
| `ContentChallengesByTarget` | Active challenges on content |
| `ContentByInitiative` | Content linked to initiative |

### Interim Work

| Query | Description |
|-------|-------------|
| `GetInterim` / `ListInterim` | Interim lookup/list |
| `InterimsByAssignee` | Interim work assigned to member |
| `InterimsByType` | Interim work filtered by type |
| `InterimsByReference` | Interim work linked to content |
| `GetInterimTemplate` / `ListInterimTemplate` | Interim template lookup/list |

## Parameters

### Governance-Only (via `MsgUpdateParams`)

These parameters are excluded from `RepOperationalParams` and can only be changed via `x/gov`:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `apprentice_tier` / `standard_tier` / `expert_tier` / `epic_tier` | See table above | Initiative tier definitions (budget, reputation, multiplier) |
| `completer_share` | 90% | Initiative reward to completer |
| `treasury_share` | 10% | Initiative reward to treasury |
| `trust_level_config` | See trust levels table | Trust level thresholds and invitation credits |

### Operational (via `MsgUpdateOperationalParams`)

#### Time Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `epoch_blocks` | uint64 | 14,400 | Blocks per epoch (~1 day) |
| `season_duration_epochs` | uint64 | 150 | Epochs per season (~5 months) |

#### DREAM Economics

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `staking_apy` | LegacyDec | 10% | On staked DREAM |
| `unstaked_decay_rate` | LegacyDec | 1% | Per epoch on unstaked DREAM |
| `transfer_tax_rate` | LegacyDec | 3% | Burned on transfers |
| `max_tip_amount` | Int | 100 DREAM | Per tip |
| `max_tips_per_epoch` | uint64 | 10 | Rate limit |
| `max_gift_amount` | Int | 500 DREAM | Per gift (invitees only) |
| `gift_only_to_invitees` | bool | true | Restrict gifts to invitees |
| `gift_cooldown_blocks` | int64 | 14,400 | Cooldown per recipient (1 day) |
| `max_gifts_per_sender_epoch` | Int | 2,000 DREAM | Total gifts per sender per epoch |

#### Conviction

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `conviction_per_dream` | LegacyDec | 0.2 | Sqrt scaling factor |
| `conviction_half_life_epochs` | uint64 | 7 | Exponential decay rate |
| `external_conviction_ratio` | LegacyDec | 50% | Required from non-affiliated stakers |
| `max_conviction_share_per_member` | LegacyDec | 33% | Prevents single-member dominance |

#### Challenges

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `min_challenge_stake` | Int | 50 DREAM | Minimum to file challenge |
| `challenger_reward_rate` | LegacyDec | 20% | Of initiative budget |
| `jury_size` | uint64 | 5 | Odd number |
| `jury_super_majority` | LegacyDec | 67% | To uphold/reject |
| `min_juror_reputation` | uint64 | 50 | Reputation required to serve |
| `challenge_response_deadline_epochs` | uint64 | 3 | Auto-uphold if no response |
| `max_active_challenges_per_committee` | uint64 | 3 | Rate limit |
| `max_new_challenges_per_epoch` | uint64 | 2 | Rate limit |
| `challenge_queue_max_size` | uint64 | 10 | Queue size limit |

#### Content Conviction

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `content_conviction_half_life_epochs` | uint64 | 14 | Slower than initiatives |
| `max_content_stake_per_member` | Int | 10,000 DREAM | Per content item |
| `max_author_bond_per_content` | Int | 1,000 DREAM | Bond cap |
| `author_bond_slash_on_moderation` | bool | true | Slash bonds on moderation |
| `content_challenge_reward_share` | LegacyDec | 50% | Minted to successful challenger |
| `conviction_propagation_ratio` | LegacyDec | 10% | Content → initiative conviction |

#### Review Periods

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `default_review_period_epochs` | uint64 | 7 | Initiative review window |
| `default_challenge_period_epochs` | uint64 | 7 | Post-review challenge window |

#### Invitations

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `min_invitation_stake` | Int | 100 DREAM | Min stake per invitation |
| `invitation_accountability_epochs` | uint64 | 150 | Accountability period (~1 season) |
| `referral_reward_rate` | LegacyDec | 5% | Inviter receives from invitee earnings |
| `invitation_cost_multiplier` | LegacyDec | 1.1x | Cost increase per additional invitation |
| `invitation_stake_burn_rate` | LegacyDec | 10% | Burned on acceptance |

#### Extended Staking

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `project_staking_apy` | LegacyDec | 8% | While project active |
| `project_completion_bonus_rate` | LegacyDec | 5% | On project completion |
| `member_stake_revenue_share` | LegacyDec | 5% | Revenue share to member stakers |
| `tag_stake_revenue_share` | LegacyDec | 2% | Per tag revenue share |
| `min_stake_duration_seconds` | int64 | 86,400 | 24 hours minimum |
| `allow_self_member_stake` | bool | false | Cannot self-stake |

#### Interim Work

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `simple_complexity_budget` | Int | 50 DREAM | Simple task compensation |
| `standard_complexity_budget` | Int | 150 DREAM | Standard task compensation |
| `complex_complexity_budget` | Int | 400 DREAM | Complex task compensation |
| `expert_complexity_budget` | Int | 1,000 DREAM | Expert task compensation |
| `solo_expert_bonus_rate` | LegacyDec | 50% | Bonus for solo expert work |
| `interim_deadline_epochs` | uint64 | 7 | Deadline in epochs |

#### Slashing

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `minor_slash_penalty` | LegacyDec | 5% | Minor infraction |
| `moderate_slash_penalty` | LegacyDec | 15% | Moderate infraction |
| `severe_slash_penalty` | LegacyDec | 30% | Severe infraction |
| `zeroing_slash_penalty` | LegacyDec | 100% | Complete zeroing |

#### Anti-Gaming

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `reputation_decay_rate` | LegacyDec | 0.5% | Per epoch |
| `max_reputation_gain_per_epoch` | uint64 | 50 | Per tag |
| `max_tags_per_initiative` | uint64 | 3 | Prevents tag stuffing |
| `min_reputation_multiplier` | LegacyDec | 10% | Floor for reputation-based calculations |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec, account lookups |
| `x/bank` | Yes | DREAM token operations, SPARK transfers |
| `x/commons` | Yes | Committee/council authorization checks |
| `x/season` | No | Current season number for reputation resets |
| `x/forum` | No | Tag registry validation via `TagKeeper` interface |

### Shield-Aware Messages

Only `MsgCreateChallenge` is shield-compatible, enabling anonymous challenge creation via `x/shield`'s `MsgShieldedExec`.

### Cyclic Dependency Breaking

Cross-module keepers are wired manually in `app.go` via shared `lateKeepers` struct:
- `SetTagKeeper()` — forum ↔ rep cycle
- `SetSeasonKeeper()` — season ↔ rep cycle

## EndBlocker

1. **Update conviction** for all active initiative stakes (time-weighted decay)
2. **Check completion thresholds** for submitted initiatives
3. **Finalize unchallenged** initiatives after challenge period expires
4. **Process expired challenge responses** (auto-uphold if no response by deadline)
5. **Process expired content challenge responses**
6. **Tally jury review votes** when deadline reached
7. **Process interim deadlines** (expire if deadline passes)
8. **Distribute epoch staking rewards** (MasterChef pools for member/tag/project stakes)
9. **Process invitation accountability** (slash inviters if invitee zeroed)
10. **Rebuild member trust tree** if dirty (for `x/shield` ZK proofs)

Lazy operations (applied on-demand via `GetMember()`):
- DREAM decay, reputation decay, invitation credit resets, trust level updates

## Events

All state-changing operations emit typed events for indexing and client notification.

## Client

### CLI

```bash
# Membership
sparkdreamd tx rep invite-member [invitee] [stake] --from alice
sparkdreamd tx rep accept-invitation [invitation_id] --from bob
sparkdreamd tx rep register-zk-public-key [hex_key] --from alice

# Initiatives
sparkdreamd tx rep create-initiative [project_id] --title "..." --tier STANDARD --from alice
sparkdreamd tx rep submit-initiative-work [initiative_id] --deliverable-uri "..." --from bob
sparkdreamd tx rep stake initiative [initiative_id] [amount] --from carol

# Challenges
sparkdreamd tx rep create-challenge [initiative_id] --reason "..." --stake 100 --from dave

# Staking rewards
sparkdreamd tx rep claim-rewards --from alice

# Queries
sparkdreamd q rep get-member [address]
sparkdreamd q rep initiative-conviction [initiative_id]
sparkdreamd q rep reputation [address] [tag]
sparkdreamd q rep params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/rep/v1/query.proto` for the full API surface.
