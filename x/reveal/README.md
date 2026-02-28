# `x/reveal`

The `x/reveal` module implements a progressive open-source system enabling trusted community members to gradually reveal closed-source code through tranched conviction staking, council-gated approvals, and stake-weighted verification voting.

## Overview

This module provides:

- **Tranched progressive disclosure** — contributions split into sequential tranches (up to 10), each requiring community conviction before reveal
- **Conviction staking (not payment)** — community members lock DREAM to signal belief; DREAM returned after verification, contributor paid via fresh mint
- **Contributor bonds** — 10% of valuation locked as accountability guarantee, slashable on failure
- **Stake-weighted verification voting** — only stakers vote; vote weight equals stake amount; 60% threshold required
- **Three-way dispute resolution** — ACCEPT, IMPROVE, or REJECT via Commons Council group proposal
- **Payout holdback** — 20% of each tranche payout retained until all tranches complete (prevents abandon-after-profit)
- **Self-dealing prevention** — contributors cannot stake on or vote on their own contributions
- **Community ownership on completion** — revealed code transitions to community-owned `x/rep` Project

## Concepts

### Tranched Progressive Disclosure

Contributions are broken into discrete tranches (e.g., "Foundation", "Features", "Polish"). Tranches process sequentially: Tranche N+1 stays LOCKED until Tranche N is verified. The sum of all tranche `stake_threshold` values equals `total_valuation`.

### Conviction Staking

Community members stake DREAM toward tranches to show conviction. Staked DREAM is locked in the module account and **returned after verification** — it is not payment. The contributor is paid via freshly minted DREAM on tranche verification. Minimum stake: 100 DREAM. Stakes exceeding the threshold are rejected (no overstaking).

### Contributor Bond

Contributors post a bond of `bond_rate * total_valuation` (default 10%):
- **Returned** if all tranches complete, council rejects proposal, or committee cancels
- **Fully slashed** if contributor fails to reveal after tranche is backed
- **50% slashed** if dispute verdict is REJECT

### Verification Voting

Only stakers for a tranche may vote. Contributors cannot vote on their own contributions. Vote weight equals the staker's stake amount.

- **Threshold**: >= 60% stake-weighted YES required
- **Minimum votes**: `max(3, stake_threshold / 5000)` — larger payouts get more scrutiny
- **Quality ratings**: 1-5 scale influences contributor reputation bonus
- If minimum votes not met, one extension of the verification period is allowed

### Dispute Resolution

When verification fails, the tranche enters DISPUTED status. An Operations Committee member initiates `MsgResolveDispute`, routed as a Commons Council group proposal:

| Verdict | Effect |
|---------|--------|
| **ACCEPT** | Tranche proceeds to payout (council overrules staker vote) |
| **IMPROVE** | Return to BACKED; all votes deleted (clean slate); new reveal deadline |
| **REJECT** | Tranche fails; 50% bond slashed; holdback burned; remaining tranches cancelled |

Auto-REJECT on timeout if council doesn't resolve within `dispute_resolution_epochs`.

### Payout Holdback

20% of each tranche payout is retained until all tranches complete. On completion: holdback released + bond returned. On abandonment/failure: holdback burned. This makes early abandonment unprofitable.

### Tranche Lifecycle

```
LOCKED → STAKING → BACKED → REVEALED → VERIFIED
                     │          │          │
                     │          │     ┌── DISPUTED ──┐
                     │          │     │              │
                     │          │     ├── ACCEPT ────┤
                     │          │     ├── IMPROVE ───┘ (back to BACKED)
                     │          │     └── REJECT ──► FAILED
                     │          │
                     │          └── reveal deadline missed → FAILED
                     └── stake deadline missed → CANCELLED
```

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Contribution` | `contribution/{id}` | Proposal with tranches, bond tracking, status |
| `RevealStake` | `stake/{id}` | Individual DREAM stake on a tranche |
| `VerificationVote` | `vote/{contributionId}/{trancheId}/{voter}` | Stake-weighted verification vote |

### Indexes

| Index | Purpose |
|-------|---------|
| `ContributionsByStatus` | Filter by PROPOSED/IN_PROGRESS/COMPLETED/CANCELLED |
| `ContributionsByContributor` | Per-contributor queries |
| `StakesByTranche` | All stakes on a specific tranche |
| `StakesByStaker` | Per-staker queries |
| `VotesByTranche` | Per-tranche vote tallying |
| `VotesByVoter` | Per-voter vote history |

### Contribution Fields

| Field | Type | Description |
|-------|------|-------------|
| `contributor` | string | Proposer address |
| `project_name` / `description` | string | Metadata |
| `tranches` | []RevealTranche | Sequential code chunks |
| `current_tranche` | uint32 | Active tranche index |
| `total_valuation` | Int | Total DREAM to be minted |
| `bond_amount` / `bond_remaining` | Int | Bond tracking (remaining decreases on slash) |
| `holdback_amount` | Int | Accumulated payout holdback |
| `status` | enum | PROPOSED, IN_PROGRESS, COMPLETED, CANCELLED |
| `proposal_eligible_at` | int64 | Cooldown after rejection |

## Messages

| Message | Description | Access |
|---------|-------------|--------|
| `MsgPropose` | Propose contribution with tranches and bond | ESTABLISHED+ member |
| `MsgApprove` | Approve contribution (Council proposal) | Commons Council |
| `MsgReject` | Reject contribution (Council proposal), return bond | Commons Council |
| `MsgStake` | Stake DREAM on tranche (not contributor) | PROVISIONAL+ member |
| `MsgWithdraw` | Withdraw stake (blocked during REVEALED/DISPUTED) | Stake owner |
| `MsgReveal` | Submit code_uri, docs_uri, commit_hash | Contributor only |
| `MsgVerify` | Vote on verification (stakers only, not contributor) | Tranche staker |
| `MsgCancel` | Cancel contribution | Contributor (pre-BACKED) or committee |
| `MsgResolveDispute` | ACCEPT/IMPROVE/REJECT verdict (Council proposal) | Commons Council |
| `MsgUpdateParams` | Update module parameters | `x/gov` authority |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `Contribution` | Single contribution by ID |
| `Contributions` | All contributions (paginated) |
| `ContributionsByContributor` | Filter by contributor address |
| `ContributionsByStatus` | Filter by status |
| `Tranche` | Single tranche by contribution and tranche ID |
| `TrancheTally` | Aggregated vote tally (yes_weight, no_weight, vote_count) |
| `TrancheStakes` | All stakes for a tranche |
| `StakeDetail` | Single stake by ID |
| `StakesByStaker` | All stakes by a staker |
| `VotesByVoter` | All verification votes by a voter |

## Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `stake_deadline_epochs` | int64 | 60 | Max time in STAKING (~2 months) |
| `reveal_deadline_epochs` | int64 | 14 | Max time to reveal after BACKED (~2 weeks) |
| `verification_period_epochs` | int64 | 14 | Verification voting window (~2 weeks) |
| `dispute_resolution_epochs` | int64 | 30 | Council resolution deadline (~1 month) |
| `verification_threshold` | Dec | 0.60 | Stake-weighted YES votes required |
| `min_verification_votes` | uint32 | 3 | Base minimum vote count |
| `max_tranches` | uint32 | 10 | Max tranches per contribution |
| `max_tranche_valuation` | Int | 50,000 | Max DREAM per tranche |
| `max_total_valuation` | Int | 50,000 | Max total DREAM per contribution |
| `bond_rate` | Dec | 0.10 | Bond = rate * total_valuation |
| `min_proposer_trust_level` | uint32 | 2 | ESTABLISHED minimum |
| `min_stake_amount` | Int | 100 | Min DREAM per stake (dust prevention) |
| `payout_holdback_rate` | Dec | 0.20 | Retained until completion |
| `proposal_cooldown_epochs` | int64 | 14 | Wait after rejection (~2 weeks) |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec |
| `x/bank` | Yes | DREAM lock/unlock/burn via module account |
| `x/rep` | Yes | Membership, trust levels, DREAM mint, reputation grant/deduct, Project creation |
| `x/commons` | Yes | Committee membership verification, Council proposal routing |

## EndBlocker

Processes all deadline-based state transitions every block:

1. **STAKING + deadline passed** — cancel tranche and contribution, return all stakes, burn holdback
2. **BACKED + reveal deadline passed** — fail tranche, fully slash bond, return stakes, burn holdback, deduct reputation
3. **REVEALED + verification deadline passed** — auto-tally votes:
   - Threshold passed → VERIFIED (payout with holdback, reputation grant, unlock next tranche)
   - Failed + min votes not met → extend deadline (one extension allowed)
   - Failed after extension → DISPUTED
4. **DISPUTED + resolution deadline passed** — auto-REJECT (staker protection)

On all-tranches VERIFIED: release holdback, return bond, create community-owned `x/rep` Project.

## Events

All state-changing operations emit typed events for indexing and client notification.

## Client

### CLI

```bash
# Propose
sparkdreamd tx reveal propose --project-name "Widget Engine" --total-valuation 30000 --from contributor

# Staking
sparkdreamd tx reveal stake [contribution_id] [tranche_id] --amount 500 --from staker

# Reveal
sparkdreamd tx reveal reveal [contribution_id] [tranche_id] --code-uri "ipfs://..." --commit-hash "abc123" --from contributor

# Verify
sparkdreamd tx reveal verify [contribution_id] [tranche_id] --value-confirmed true --quality-rating 4 --from staker

# Queries
sparkdreamd q reveal contribution 1
sparkdreamd q reveal tranche-tally 1 0
sparkdreamd q reveal tranche-stakes 1 0
sparkdreamd q reveal params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/reveal/v1/query.proto` for the full API surface.
