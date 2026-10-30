# `x/commons`

The `x/commons` module is the orchestrator of Spark Dream's "Three Pillars" governance system. It implements a **native governance layer** with councils, committees, weighted proposals, anonymous voting (via `x/shield`), permission control, and elastic tenure via prediction markets.

## Overview

This module provides:

- **Native governance** — proposal submission, weighted voting, threshold-based acceptance, and execution
- **Hierarchical governance** — parent-child group relationships with permission inheritance
- **Three Pillars structure** — Commons, Technical, and Ecosystem councils with operational and governance committees
- **Anonymous governance** — anonymous proposals and votes via `x/shield` ZK proofs
- **Cross-council veto** — parent councils can invalidate child proposals via policy versioning
- **Treasury control** — per-group spending limits with rate-limited disbursement
- **Permission system** — restricted message allowlisting with forbidden/restricted message enforcement
- **Elastic tenure** — integration with `x/futarchy` for confidence-vote-based term extension (+20%) or reduction (-50%)
- **Electoral delegation** — child committees control parent council membership
- **Recurring spends** — wire-compatible wrappers (`Schedule`/`Cancel`/`Claim`/`Decline` `RecurringSpend`) over x/session's unified grant registry (`RECURRING_PULL` payload variant); council policies are granters
- **Categories** — typed councils for proposal classification
- **Treasury accounting** — `MintToTreasury` / `PayDREAMFromTreasuryFirst` / `CreditDREAM` / `AddToTreasury` / `SpendFromTreasury` primitives with per-epoch caps, inflow/outflow counters, and referral-reward consolidation

## Concepts

### The Three Pillars Hierarchy

```
GOVERNANCE (x/gov)
├── Commons Council (Culture, Arts, Events) — 47.5% funding
│   ├── Commons Operations Committee — 2.5% funding
│   └── Commons Governance Committee
├── Technical Council (Chain Upgrades & Security) — 28.5% funding
│   ├── Technical Operations Committee — 1.5% funding
│   └── Technical Governance Committee
├── Ecosystem Council (Treasury & Growth) — 19% funding
│   ├── Ecosystem Operations Committee — 1% funding
│   └── Ecosystem Governance Committee
└── Commons Supervisory Board (oversees HR decisions)

Each pillar's combined share (Council + Operations Committee) sums to its
macro allocation — 50% Commons, 30% Technical, 20% Ecosystem. 5% of each
pillar is routed continuously to its Operations Committee so day-to-day
spending has its own budget without requiring a council
`MsgSpendFromCommons` proposal. Governance committees and the Supervisory
Board keep `FundingWeight=0` because their `MaxSpendPerEpoch=0`.
```

### Groups

Each group (council or committee) carries metadata beyond basic membership:

- **Parent/child relationships** — hierarchical governance with delegation
- **Electoral delegation** — child committees manage parent council membership
- **Funding weight** — treasury allocation share
- **Spending limits** — per-epoch rate limiting on total spending
- **Term duration** — configurable mandate length with renewal
- **Member constraints** — min/max member counts enforced at creation and update
- **Activation time** — shell groups accumulate funds before activation
- **Futarchy toggle** — per-group elastic tenure via prediction markets
- **Veto policy** — separate policy address for parent council vetoes

### Native Proposal System

The module owns its proposal lifecycle end-to-end:

1. **Submit** — a council member submits a proposal containing one or more messages
2. **Vote** — council members cast weighted votes (YES, NO, ABSTAIN, NO_WITH_VETO)
3. **Finalize** — proposals are accepted or rejected based on decision policy thresholds; early acceptance triggers when threshold is met before the deadline; EndBlocker auto-finalizes expired proposals
4. **Execute** — anyone can execute an accepted proposal after `min_execution_period` elapses
5. **Veto** — parent councils can bump the policy version, which invalidates any pending proposals created under the old version

Decision policies support two modes:
- **Percentage** — YES votes as a fraction of total weight must meet or exceed threshold (e.g., 51%)
- **Threshold** — absolute YES vote weight must meet or exceed a fixed value

### Anonymous Governance

Through `x/shield`, members can:
- **Submit anonymous proposals** — proposer is the shield module account; ZK proof verifies membership. Signaling proposals (no messages) need non-empty metadata, as for `MsgSubmitProposal`
- **Vote by secret ballot** — a group's `ballot_mode` (OPEN default, SECRET, PROPOSER_CHOICE) decides whether a proposal is a secret ballot. A secret ballot snapshots the members' ZK keys into a voter tree (equal member weights and at least 5 keyed members required); members vote with `MsgAnonymousVoteProposal` through `x/shield`, proven against that tree (`PROOF_DOMAIN_GROUP_TREE`), once each. Open votes are refused on a secret ballot and anonymous votes on an open one, so nobody votes twice

Anonymous tallies are reported as `anon_tally` and count the snapshot's member weight per vote in threshold checks.

### Polls

Any member can put a non-binding, single-choice question (2-10 options) to their group for 1 hour to 30 days (`MsgOpenPoll`). Members vote once, finally (`MsgVotePoll`), or by secret ballot through `x/shield` when the creator chose one (`MsgAnonymousVotePoll`). The creator can cancel a poll nobody voted on. The EndBlocker closes polls at their deadline and prunes them 30 days later. Caps: 20 open polls per group, 3 per creator.

### Permission Model

**Forbidden messages** (globally banned from any group):
- `MsgExec`, `MsgGrant` (authz recursion/delegation exploits)
- `MsgCreateGroup`, `MsgUpdateGroupAdmin` (unauthorized group creation/takeover)
- `MsgUnjail`, `MsgSetWithdrawAddress` (validator self-dealing)

**Restricted messages** (only `x/gov` can grant):
- `MsgEmergencyCancelGovProposal` (the "veto gun")
- `MsgUpdateParams`, `MsgForceUpgrade`

**Ratchet-down logic**: groups can only remove permissions, never add them (unless signer is `x/gov`). This prevents privilege escalation through self-administration.

### Elastic Tenure via Futarchy

When a group has `futarchy_enabled=true`:
1. A confidence vote market is created at 50% of the group's term duration
2. Market resolves to "yes" (high confidence) or "no" (low confidence)
3. **Yes outcome**: term extends by +20%
4. **No outcome**: term slashed by -50% (forces re-election)
5. **No quorum**: neutral (no tenure change)

## State

### Collections

| Collection | Key | Value | Description |
|------------|-----|-------|-------------|
| `Groups` | `{council_name}` | `Group` | Council/committee with hierarchy, permissions, lifecycle |
| `Members` | `{council_name, address}` | `Member` | O(1) member lookup per council |
| `DecisionPolicies` | `{policy_address}` | `DecisionPolicy` | Voting rules for a policy |
| `Proposals` | `{proposal_id}` | `Proposal` | All council proposals |
| `Votes` | `{proposal_id, voter}` | `Vote` | Individual votes on proposals |
| `AnonVoteTallies` | `{proposal_id}` | `AnonVoteTally` | Aggregated anonymous vote counts |
| `PolicyPermissions` | `{policy_address}` | `PolicyPermissions` | Allowed messages for a policy |
| `PolicyToName` | `{policy_address}` | `string` | Reverse lookup: policy → council name |
| `PolicyVersion` | `{policy_address}` | `uint64` | For veto invalidation |
| `VetoPolicies` | `{council_name}` | `string` | Veto policy address per council |
| `ProposalsByCouncil` | `{council_name, proposal_id}` | `—` | Index for listing proposals by council |
| `MarketToGroup` | `{market_id}` | `string` | Futarchy market → council mapping |
| `MarketTriggerQueue` | `{trigger_time, name}` | `—` | Scheduled confidence markets |
| `ProposalSeq` | — | `uint64` | Auto-increment proposal ID sequence |
| `CouncilSeq` | — | `uint64` | Auto-increment council ID sequence |
| `ProposalVoterTrees` | `{proposal_id}` | `VoterTree` | Voter snapshot of an open secret ballot |
| `Polls` | `{poll_id}` | `Poll` | Polls with their tallies |
| `PollVotes` | `{poll_id, voter}` | `PollVote` | Open votes on polls |
| `PollsByCouncil` / `OpenPollsByCouncil` | `{council_name, poll_id}` | `—` | Poll listing index / open-poll caps |
| `PollQueue` | `{due_time, poll_id}` | `—` | Poll closing and pruning schedule |
| `PollVoterTrees` | `{poll_id}` | `VoterTree` | Voter snapshot of an open secret poll |
| `PollSeq` | — | `uint64` | Auto-increment poll ID sequence (from 1) |

### Group Fields

| Field | Type | Description |
|-------|------|-------------|
| `index` | string | Group name (e.g., "Technical Council") |
| `group_id` | uint64 | Internal council ID |
| `policy_address` | string | Standard decision policy address |
| `parent_policy_address` | string | Parent group's policy or `x/gov` |
| `electoral_policy_address` | string | Committee managing membership |
| `veto_policy_address` | string | Veto policy for parent council checks |
| `funding_weight` | uint64 | Treasury share percentage |
| `max_spend_per_epoch` | Int | Spending cap per epoch |
| `min_members` / `max_members` | uint64 | Group size constraints |
| `term_duration` | int64 | Seconds until renewal required |
| `current_term_expiration` | int64 | When the group's mandate ends |
| `ballot_mode` | BallotMode | `OPEN` (default), `SECRET` or `PROPOSER_CHOICE`: how proposals are voted |
| `activation_time` | int64 | Shell group activation timestamp |
| `futarchy_enabled` | bool | Confidence markets enabled |

## Messages

### Proposal Lifecycle

| Message | Description | Access |
|---------|-------------|--------|
| `MsgSubmitProposal` | Submit a proposal containing messages to a council | Council members |
| `MsgVoteProposal` | Cast a weighted vote on a proposal | Council members |
| `MsgExecuteProposal` | Execute an accepted proposal after min execution period | Anyone |
| `MsgSubmitAnonymousProposal` | Submit a proposal via `x/shield` ZK proof | Via `x/shield` (proposer = shield module) |
| `MsgAnonymousVoteProposal` | Cast a secret-ballot vote via `x/shield` | Via `x/shield` (voter = shield module) |
| `MsgOpenPoll` | Open a single-choice poll for the group | Group members |
| `MsgVotePoll` | Vote on an open poll | Group members |
| `MsgAnonymousVotePoll` | Cast a secret-ballot poll vote via `x/shield` | Via `x/shield` (voter = shield module) |
| `MsgCancelPoll` | Withdraw a poll with no votes | Poll creator |

### Group Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgRegisterGroup` | Create new group with policy, permissions, parent, constraints | `x/gov` or parent group |
| `MsgRenewGroup` | Update members/weights when term expires | Parent group only |
| `MsgUpdateGroupMembers` | Add/remove members from a council | Council itself via proposal |
| `MsgUpdateGroupConfig` | Modify constraints (spend limit, cooldown, term, voting windows) | Council itself |
| `MsgDeleteGroup` | Tombstone a group (irreversible) | Parent council |
| `MsgVetoGroupProposals` | Bump policy version to invalidate all pending proposals | Veto policy only |

### Treasury

| Message | Description | Access |
|---------|-------------|--------|
| `MsgSpendFromCommons` | Transfer from community pool to recipient | Council policy via proposal |

### Categories

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateCategory` | Create a proposal/spend category | Council policy via proposal |
| `MsgDeleteCategory` | Remove an existing category | Council policy via proposal |

### Recurring Spends (wrappers over x/session)

These four messages are wire-compatible facades. Internally they call x/session's module-bypass entrypoints with the council policy as granter. The legacy `RecurringSpends*` collections and the three duplicated params (`MinRecurringPeriodSeconds`, `MaxRecurringDurationSeconds`, `MaxActiveRecurringSpendsPerGroup`) are removed (proto `reserved 2, 3, 4` in [params.proto](../../proto/sparkdream/commons/v1/params.proto)).

| Message | Maps to | Access |
|---------|---------|--------|
| `MsgScheduleRecurringSpend` | `session.CreateGrantOnBehalfOf` with `RecurringPull` payload | Council policy via proposal |
| `MsgCancelRecurringSpend` | `session.RevokeGrantInternal` | Granter (council policy) or grantee |
| `MsgClaimRecurringSpend` | `session.ClaimRecurringPullForGrantee` | Grantee |
| `MsgDeclineRecurringSpend` | `session.DeclineGrantInternal` | Grantee |

Cancelled and declined schedules return `NotFound` from `QueryGetRecurringSpend(id)` post-migration — audit trail is the `grant_revoked` / `grant_declined` events. Council activation, term-expiry, and per-epoch rate-limit gates apply to every council-policy claim via `SessionClaimHook` (PreCheck + PostCommit). PostCommit is tx-halting on error, closing a double-debit window the single-method design would have left open on bank-send retry.

### Permission Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreatePolicyPermissions` | Define allowed messages for a policy | `x/gov` or policy self |
| `MsgUpdatePolicyPermissions` | Modify policy's message allowlist (ratchet-down only) | `x/gov` or policy self |
| `MsgDeletePolicyPermissions` | Remove all permissions for a policy | `x/gov` or policy self |

### Governance

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update module parameters | `x/gov` only |
| `MsgEmergencyCancelGovProposal` | Cancel an `x/gov` proposal | Veto policy only |
| `MsgForceUpgrade` | Schedule chain upgrade | `x/gov` only |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `GetGroup` | Single group by name |
| `ListGroup` | Paginated list of all groups |
| `GetCouncilMembers` | List members of a council |
| `GetPolicyPermissions` | Allowed messages for a policy |
| `ListPolicyPermissions` | Paginated list of all permission sets |
| `GetProposal` | Single proposal by ID (includes votes, tally, and `anon_tally` of anonymous votes) |
| `ListProposals` | Paginated proposals with optional council filter |
| `GetProposalVotes` | Votes for a proposal with tally and `anon_tally` |
| `GetProposalVoterTree` | A secret ballot's voter snapshot (for building the shield proof) |
| `GetPoll` / `ListPolls` | Polls with their tallies (filter by group and status) |
| `GetPollVotes` | Open votes on a poll |
| `GetPollVoterTree` | A secret poll's voter snapshot |

## Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `proposal_fee` | string (coins) | 5,000,000 uspark | Anti-spam fee for proposal submission (waived for `x/gov`) |

## Events

| Event | Attributes | Description |
|-------|-----------|-------------|
| `submit_proposal` | proposal_id, council_name, proposer | Proposal created |
| `vote_proposal` | proposal_id, voter, option | Vote cast |
| `execute_proposal` | proposal_id, executor, status | Proposal executed |
| `submit_anonymous_proposal` | proposal_id, council_name | Anonymous proposal via shield |
| `anonymous_vote_proposal` | proposal_id, option | Anonymous vote via shield |
| `proposal_finalized` | proposal_id, status | EndBlocker auto-finalization |

## Treasury Accounting

`x/commons` mints DREAM into a per-pillar treasury account and pays from it before mint when configured to do so. Five keeper primitives back this:

- `MintToTreasury` — enforces per-epoch cap, advances `SeasonMinted` and the new `SeasonTreasuryInflow` counter, called from `CompleteInitiative` to credit the 10% treasury share. Per-season initiative-reward cap now accounts for the full (completer + treasury) mint.
- `PayDREAMFromTreasuryFirst` — drains treasury first when the `TreasuryFundsInterims` / `TreasuryFundsRetroPgf` flags are on, mints only the shortfall. Critically, fires `CalculateReferralReward` ONCE on the combined (treasuryPaid + minted) total — MintDREAM's per-mint referral is suppressed via the existing `referralMintingKey` guard for the shortfall portion. Closes a silent regression where flipping `TreasuryFundsInterims` on would have under-paid inviters by the treasury-covered fraction. Wired into both interim payment paths and into x/season's retro-PGF distribution via `PayRetroPgfReward` on the `season.RepKeeper` interface.
- `CreditDREAM` — mint-cap-free, referral-silent primitive used for the treasury-paid portion of payouts; emits `credit_dream{source=treasury}`. Recipients may see either or both `mint_dream` and `credit_dream` events.
- `AddToTreasury` / `SpendFromTreasury` — advance the new inflow/outflow counters (both reset in `InitSeasonalPool`). `TreasuryStatus` reads the counters and reports both `SeasonTreasuryInflow` and `SeasonTreasuryOutflow`.

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Account and module address access |
| `x/bank` | Yes | Coin transfers for spending and fees |
| `x/gov` | Yes | Governance authority and ultimate fallback (late-wired) |
| `x/futarchy` | No | Prediction market creation and resolution hooks |
| `x/split` | No | Treasury fund distribution by funding weight |
| `x/upgrade` | No | Chain upgrade scheduling |
| `x/session` | Yes | Host for recurring-spend grants (genesis-seeds commons module address in `authorized_grant_creators`) |
| `x/service` | No | Subscribes to `AfterOperatorDissolved` hook to auto-cancel matching `RecurringSpend` schedules |

### Late Wiring (app.go)

- `SetGovKeeper()` — wires `x/gov`'s GovKeeper after depinject (breaks cycle)
- `SetRouter()` — wires baseapp's MsgServiceRouter after app build (needed for proposal execution)
- `session.SetClaimHooks(SessionClaimHook)` — commons supplies the implementation of x/session's `GrantClaimHook` interface so council-policy grants flow through `CheckSpendGates` (PreCheck) + `recordEpochSpend` (PostCommit)

## Genesis

The module bootstraps the entire Three Pillars governance structure at genesis via `BootstrapGovernance()`:

1. Creates **Commons Council** with all founding members (1-year term, 51% threshold)
2. Creates **Technical Council** with founder + Commons Council as guardian veto (66% threshold), plus Operations and Governance committees
3. Creates **Ecosystem Council** with founder + Commons Council as guardian veto (66% threshold), plus Operations and Governance committees
4. Creates **Commons Supervisory Board** (Tech + Eco councils, 2-day veto window)
5. Wires electoral delegations (child committees control parent council membership)
6. Assigns funding weights and registers shares via `x/split`
7. Sets up veto policies for cross-council checks

## Shield-Aware Interface

The module implements `ShieldAware` for `x/shield` integration:

- `MsgSubmitAnonymousProposal` — shield-compatible, proven against the x/rep trust tree
- `MsgAnonymousVoteProposal`, `MsgAnonymousVotePoll` — shield-compatible, proven against the ballot's voter snapshot; the keeper implements `ShieldRootResolver` to name its root

These messages are routed through `x/shield`'s `MsgShieldedExec` for ZK proof verification and nullifier management.

## Security

- **Cycle detection** — prevents circular parent-child relationships via ancestry walk
- **Golden shares** — on the Technical and Ecosystem councils the Commons Council seat weighs as much as all other members together, so it holds half the votes however many join; only x/gov may remove it
- **One member, one vote** — the genesis governance bodies take weight-1 members unless x/gov sets otherwise (the founder's genesis weight-3 seat is kept); other groups choose any positive weight
- **Electoral separation** — governance committees are separate from operational committees
- **Permission ratcheting** — groups can reduce but not expand their own permissions
- **Rate limiting** — `SpendFromCommons` enforces per-epoch spending caps
- **Policy versioning** — veto bumps version, invalidating all pending proposals under the old version
- **One ballot per proposal** — a proposal is voted openly or by secret ballot, never both, and a secret-ballot vote proves membership of that proposal's voter snapshot, not just of the trust tree

## Client

### CLI

```bash
# Proposals
sparkdreamd tx commons submit-proposal [proposal-json] --from member
sparkdreamd tx commons vote-proposal [proposal-id] [yes|no|abstain|no-with-veto] --from member
sparkdreamd tx commons execute-proposal [proposal-id] --from anyone

# Group management (via governance proposals)
sparkdreamd tx commons register-group --from authority
sparkdreamd tx commons spend-from-commons [recipient] [amount] --from policy

# Queries
sparkdreamd q commons get-group "Technical Council"
sparkdreamd q commons list-group
sparkdreamd q commons get-council-members "Technical Council"
sparkdreamd q commons get-proposal [proposal-id]
sparkdreamd q commons list-proposals
sparkdreamd q commons get-proposal-votes [proposal-id]
sparkdreamd q commons get-policy-permissions [policy_address]
sparkdreamd q commons params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/commons/v1/query.proto` for the full API surface.
