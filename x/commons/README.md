# `x/commons`

The `x/commons` module is the orchestrator of Spark Dream's "Three Pillars" governance system. It implements a **native governance layer** with councils, committees, weighted proposals, anonymous voting (via `x/shield`), permission control, and elastic tenure via prediction markets.

## Overview

This module provides:

- **Native governance** — proposal submission, weighted voting, threshold-based acceptance, and execution (replaces `x/group`)
- **Hierarchical governance** — parent-child group relationships with permission inheritance
- **Three Pillars structure** — Commons, Technical, and Ecosystem councils with operational and governance committees
- **Anonymous governance** — anonymous proposals and votes via `x/shield` ZK proofs
- **Cross-council veto** — parent councils can invalidate child proposals via policy versioning
- **Treasury control** — per-group spending limits with rate-limited disbursement
- **Permission system** — restricted message allowlisting with forbidden/restricted message enforcement
- **Elastic tenure** — integration with `x/futarchy` for confidence-vote-based term extension (+20%) or reduction (-50%)
- **Electoral delegation** — child committees control parent council membership

## Concepts

### The Three Pillars Hierarchy

```
GOVERNANCE (x/gov)
├── Commons Council (Culture, Arts, Events) — 50% funding
│   ├── Commons Operations Committee
│   └── Commons Governance Committee
├── Technical Council (Chain Upgrades & Security) — 30% funding
│   ├── Technical Operations Committee
│   └── Technical Governance Committee
├── Ecosystem Council (Treasury & Growth) — 20% funding
│   ├── Ecosystem Operations Committee
│   └── Ecosystem Governance Committee
└── Commons Supervisory Board (oversees HR decisions)
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

The module implements its own proposal lifecycle without depending on `x/group`:

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
- **Submit anonymous proposals** — proposer is the shield module account; ZK proof verifies membership
- **Cast anonymous votes** — uniform weight of 1; nullifier scoped by proposal ID prevents double-voting

Anonymous vote tallies are stored separately and combined with regular votes during threshold checks.

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
| `MsgAnonymousVoteProposal` | Cast an anonymous vote via `x/shield` | Via `x/shield` (voter = shield module) |

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
| `GetProposal` | Single proposal by ID (includes votes and tally) |
| `ListProposals` | Paginated proposals with optional council filter |
| `GetProposalVotes` | Votes for a proposal with tally |

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

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Account and module address access |
| `x/bank` | Yes | Coin transfers for spending and fees |
| `x/gov` | Yes | Governance authority and ultimate fallback (late-wired) |
| `x/futarchy` | No | Prediction market creation and resolution hooks |
| `x/split` | No | Treasury fund distribution by funding weight |
| `x/upgrade` | No | Chain upgrade scheduling |

### Late Wiring (app.go)

- `SetGovKeeper()` — wires `x/gov`'s GovKeeper after depinject (breaks cycle)
- `SetRouter()` — wires baseapp's MsgServiceRouter after app build (needed for proposal execution)

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

- `MsgSubmitAnonymousProposal` — shield-compatible
- `MsgAnonymousVoteProposal` — shield-compatible

These messages are routed through `x/shield`'s `MsgShieldedExec` for ZK proof verification and nullifier management.

## Security

- **Cycle detection** — prevents circular parent-child relationships via ancestry walk
- **Golden shares** — Technical and Ecosystem councils have Commons Council as guardian veto
- **Electoral separation** — governance committees are separate from operational committees
- **Permission ratcheting** — groups can reduce but not expand their own permissions
- **Rate limiting** — `SpendFromCommons` enforces per-epoch spending caps
- **Policy versioning** — veto bumps version, invalidating all pending proposals under the old version
- **Anonymous vote weight cap** — anonymous votes use uniform weight=1 to prevent weight manipulation

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
