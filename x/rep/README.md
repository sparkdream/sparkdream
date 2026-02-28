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
- **Challenge system** — named and anonymous challenges with jury resolution
- **Content challenges** — cross-module quality assurance via author bonds
- **Interim work** — fixed-rate delegated duties (jury duty, moderation, expert review)
- **MasterChef staking rewards** — epoch-based reward pools for member, tag, and project staking

## Concepts

### Members and Trust Levels

Members progress through five trust levels by earning reputation and completing interim work:

| Level | Reputation | Interims | Invitation Credits |
|-------|-----------|----------|--------------------|
| NEW | 0 | 0 | 0 |
| PROVISIONAL | 10 | 1 | 2 |
| ESTABLISHED | 50 | 3 | 5 |
| TRUSTED | 100 | 0 seasons | 10 |
| CORE | 200 | 0 seasons | 20 |

Member statuses: ACTIVE, INACTIVE, ZEROED. Zeroing burns all DREAM, zeroes reputation, and resets trust level — but the person can restart with a new address and new invitation ("punish position, not person").

### DREAM Token

DREAM is the internal earned token:

- **Minting**: initiative completion (primary), staking rewards, interim compensation, retroactive public goods
- **Burning**: slashing, failed challenges, failed invitations, unstaked decay (1%/epoch), transfer tax (3%)
- **Transfers**: tips (max 100 DREAM, 10/epoch), gifts (max 500 DREAM, invitees only), bounties (escrowed)
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

### Projects and Initiatives

**Projects**: council-approved budgets (DREAM + SPARK) with categories and tags.

**Initiatives**: self-selected work under projects with four tiers:

| Tier | Max Budget | Reward Multiplier |
|------|-----------|-------------------|
| Apprentice | 100 DREAM | 0.5x |
| Standard | 500 DREAM | 1.0x |
| Expert | 2,000 DREAM | 1.5x |
| Epic | 10,000 DREAM | 2.0x |

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
| Project | 8% | Support active projects |
| Member | 5% | Peer support (circular A↔B blocked) |
| Tag | 2% | Domain expertise signal |
| Content | — | Blog/forum/collection conviction |

- Min stake duration: 24 hours
- Max conviction share per member: 33% (prevents single-member dominance)
- Content stakes capped at 10K DREAM per member per item

### Challenge System

Members can challenge initiative work quality:

- **Named challenges**: min 50 DREAM stake, identity public
- **Anonymous challenges**: ZK proof required (via `x/vote`), 1 SPARK escrowed

**Jury resolution**: 5 jurors (odd, configurable), weighted by reputation in relevant tags, 67% supermajority. Auto-uphold if assignee doesn't respond within 3 epochs. Successful challenger receives 20% of initiative budget.

### Content Challenges

Cross-module quality assurance via author bonds:

- Content creators stake DREAM to bond their reputation to posts/collections (max 1000 DREAM)
- Members challenge bonded content; same jury system as initiatives
- Successful challenger gets 50% of slashed bond (minted)
- 10% of content conviction propagates to linked initiatives

### Interim Work

Fixed-rate delegated duties:

| Complexity | Compensation |
|-----------|-------------|
| SIMPLE | 50 DREAM |
| STANDARD | 150 DREAM |
| COMPLEX | 400 DREAM |
| EXPERT | 1,000 DREAM |

Types: JURY_DUTY, EXPERT_TESTIMONY, DISPUTE_MEDIATION, PROJECT_APPROVAL, BUDGET_REVIEW, MODERATION. Solo expert bonus: +50%. Capped reputation per tag per epoch prevents grinding.

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Member` | `member/value/{address}` | Balance, reputation, trust level, decay tracking |
| `Invitation` | `invitation/value/{id}` | Pending/accepted invitations with accountability |
| `Project` | `project/value/{id}` | Council-approved project budgets |
| `Initiative` | `initiative/value/{id}` | Self-selected work units with conviction tracking |
| `Stake` | `stake/value/{id}` | Conviction/content/author bond stakes |
| `Challenge` | `challenge/value/{id}` | Initiative challenges with jury reference |
| `JuryReview` | `juryreview/value/{id}` | Jury voting on challenges |
| `Interim` | `interim/value/{id}` | Fixed-rate delegated work |
| `ContentChallenge` | `contentchallenge/value/{id}` | Challenges on bonded content |
| `UsedNullifier` | `usednullifier/{nullifier}` | ZK double-challenge prevention |
| `GiftRecord` | `giftrecord/{sender}/{recipient}` | Gift cooldown tracking |

### Indexes

| Index | Purpose |
|-------|---------|
| `InitiativesByStatus` | Filter by OPEN/SUBMITTED/IN_REVIEW/COMPLETED/etc. |
| `InterimsByStatus` | Filter by PENDING/ASSIGNED/SUBMITTED/COMPLETED/etc. |
| `StakesByTarget` | All stakes on a specific target |
| `ChallengesByStatus` | Filter by ACTIVE/IN_JURY_REVIEW/UPHELD/DISMISSED |
| `ContentChallengesByTarget` | Active challenge per content item |
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

### Challenges

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateChallenge` | Challenge initiative work (named or anonymous) | Members |
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
| `MsgCompleteInterim` | Finalize, mint rewards, grant reputation | Authority |

### Parameter Updates

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update all parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Committee authority |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `GetMember` | Member with lazy decay/reputation applied |
| `ListMember` | Paginated member list |
| `MembersByTrustLevel` | Filter by trust level |
| `GetInitiative` / `ListInitiative` | Initiative lookup/list |
| `InitiativesByProject` | Initiatives under a project |
| `InitiativesByAssignee` | Member's assigned initiatives |
| `AvailableInitiatives` | Open initiatives to claim |
| `InitiativeConviction` | Current conviction score (time-weighted) |
| `GetStake` / `StakesByTarget` | Stake lookup |
| `GetChallenge` / `ChallengesByStatus` | Challenge queries |
| `Reputation` | Member's reputation in a specific tag |
| `ContentConviction` | Conviction score on content |
| `AuthorBond` | Author bond stake for content |
| `ContentChallengesByTarget` | Active challenges on content |
| `ContentByInitiative` | Content linked to initiative |

## Parameters

### Time Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| `epoch_blocks` | 14,400 | Blocks per epoch (~1 day) |
| `season_duration_epochs` | 150 | Epochs per season (~5 months) |

### DREAM Economics

| Parameter | Default | Description |
|-----------|---------|-------------|
| `staking_apy` | 10% | On staked DREAM |
| `unstaked_decay_rate` | 1% | Per epoch on unstaked DREAM |
| `transfer_tax_rate` | 3% | Burned on transfers |
| `max_tip_amount` | 100 DREAM | Per tip |
| `max_tips_per_epoch` | 10 | Rate limit |
| `max_gift_amount` | 500 DREAM | Per gift (invitees only) |

### Initiative Rewards

| Parameter | Default | Description |
|-----------|---------|-------------|
| `completer_share` | 90% | To initiative completer |
| `treasury_share` | 10% | To treasury |

### Conviction

| Parameter | Default | Description |
|-----------|---------|-------------|
| `conviction_per_dream` | 0.2 | Sqrt scaling factor |
| `conviction_half_life_epochs` | 7 | Exponential decay rate |
| `external_conviction_ratio` | 50% | Required from non-affiliated stakers |
| `max_conviction_share_per_member` | 33% | Prevents single-member dominance |

### Challenges

| Parameter | Default | Description |
|-----------|---------|-------------|
| `min_challenge_stake` | 50 DREAM | Minimum to file challenge |
| `challenger_reward_rate` | 20% | Of initiative budget |
| `jury_size` | 5 | Odd number |
| `jury_super_majority` | 67% | To uphold/reject |
| `challenge_response_deadline_epochs` | 3 | Auto-uphold if no response |

### Content Conviction

| Parameter | Default | Description |
|-----------|---------|-------------|
| `content_conviction_half_life_epochs` | 14 | Slower than initiatives |
| `max_content_stake_per_member` | 10,000 DREAM | Per content item |
| `max_author_bond_per_content` | 1,000 DREAM | Bond cap |
| `content_challenge_reward_share` | 50% | Minted to successful challenger |
| `conviction_propagation_ratio` | 10% | Content → initiative conviction |

### Anti-Gaming

| Parameter | Default | Description |
|-----------|---------|-------------|
| `reputation_decay_rate` | 0.5% | Per epoch |
| `max_reputation_gain_per_epoch` | 50 | Per tag |
| `max_tags_per_initiative` | 3 | Prevents tag stuffing |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec, account lookups |
| `x/bank` | Yes | SPARK escrow for anonymous challenges |
| `x/commons` | Yes | Committee/council authorization checks |
| `x/vote` | No | ZK membership proof verification (anonymous challenges) |
| `x/season` | No | Current season number, epoch timing |
| `x/forum` | No | Tag registry validation via `TagKeeper` interface |

### Cyclic Dependency Breaking

Cross-module keepers are wired manually in `app.go` via `Set*Keeper()` methods:
- `SetVoteKeeper()` — vote ↔ rep cycle
- `SetTagKeeper()` — forum ↔ rep cycle
- `SetSeasonKeeper()` — season ↔ rep cycle

All held in shared `lateKeepers` struct so mutations are visible across value copies.

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
10. **Rebuild member trust tree** if dirty (for `x/vote` ZK proofs)

Lazy operations (applied on-demand via `GetMember()`):
- DREAM decay, reputation decay, invitation credit resets

## Events

All state-changing operations emit typed events for indexing and client notification.

## Client

### CLI

```bash
# Membership
sparkdreamd tx rep invite-member [invitee] [stake] --from alice
sparkdreamd tx rep accept-invitation [invitation_id] --from bob

# Initiatives
sparkdreamd tx rep create-initiative [project_id] --title "..." --tier STANDARD --from alice
sparkdreamd tx rep submit-initiative-work [initiative_id] --deliverable-uri "..." --from bob
sparkdreamd tx rep stake initiative [initiative_id] [amount] --from carol

# Challenges
sparkdreamd tx rep create-challenge [initiative_id] --reason "..." --stake 100 --from dave

# Queries
sparkdreamd q rep get-member [address]
sparkdreamd q rep initiative-conviction [initiative_id]
sparkdreamd q rep reputation [address] [tag]
sparkdreamd q rep params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/rep/v1/query.proto` for the full API surface.
