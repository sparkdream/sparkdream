# `x/commons`

The `x/commons` module is the orchestrator of Spark Dream's "Three Pillars" governance system. It wraps the Cosmos SDK's `x/group` module with an `ExtendedGroup` abstraction layer, implementing hierarchical governance with councils, committees, policy permissions, and elastic tenure via prediction markets.

## Overview

This module provides:

- **Hierarchical governance** — parent-child group relationships with permission inheritance
- **Three Pillars structure** — Commons, Technical, and Ecosystem councils with operational and membership committees
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

### Extended Groups

Beyond `x/group`'s basic Group and GroupPolicy, this module adds ExtendedGroup metadata:

- **Parent/child relationships** — hierarchical governance with delegation
- **Electoral delegation** — child committees manage parent council membership
- **Funding weight** — treasury allocation share
- **Spending limits** — per-epoch rate limiting on total spending
- **Term duration** — configurable mandate length with renewal
- **Member constraints** — min/max member counts enforced at creation and update
- **Activation time** — shell groups accumulate funds before activation
- **Futarchy toggle** — per-group elastic tenure via prediction markets

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

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `ExtendedGroup` | `extendedGroup/value/{index}` | Group with hierarchy, permissions, and lifecycle |
| `PolicyPermissions` | `policyPermissions/value/{policy_address}` | Allowed messages for a group policy |
| `MarketToGroup` | `market_to_group/value/{market_id}` | Futarchy market → group name mapping |
| `PolicyToName` | `policyToName/value/{policy_address}` | Reverse lookup: policy → group name |
| `MarketTriggerQueue` | `market_trigger_queue/value/{trigger_time,name}` | Scheduled confidence markets |

### ExtendedGroup Fields

| Field | Type | Description |
|-------|------|-------------|
| `index` | string | Group name (e.g., "Technical Council") |
| `group_id` | uint64 | `x/group` reference |
| `policy_address` | string | Active decision policy address |
| `parent_policy_address` | string | Parent group's policy or `x/gov` |
| `electoral_policy_address` | string | Committee managing membership |
| `funding_weight` | uint64 | Treasury share percentage |
| `max_spend_per_epoch` | Int | Spending cap per epoch |
| `min_members` / `max_members` | uint64 | Group size constraints |
| `term_duration` | int64 | Seconds until renewal required |
| `current_term_expiration` | int64 | When the group's mandate ends |
| `activation_time` | int64 | Shell group activation timestamp |
| `futarchy_enabled` | bool | Confidence markets enabled |

## Messages

### Group Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgRegisterGroup` | Create new group with policy, permissions, parent, constraints | `x/gov` or parent group |
| `MsgRenewGroup` | Update members/weights when term expires | Parent group only |
| `MsgUpdateGroupMembers` | Add/remove members from a policy | Group itself via proposal |
| `MsgUpdateGroupConfig` | Modify constraints (spend limit, cooldown, term, voting windows) | Group itself |
| `MsgDeleteGroup` | Tombstone a group (irreversible) | Parent council |
| `MsgForceUpgrade` | Schedule chain upgrade | `x/gov` only |
| `MsgVetoGroupProposals` | Kill pending group proposals | Veto policy only |

### Treasury

| Message | Description | Access |
|---------|-------------|--------|
| `MsgSpendFromCommons` | Transfer from group policy to recipient | Group policy via proposal |

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

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `GetExtendedGroup` | Single group by name |
| `ListExtendedGroup` | Paginated list of all groups |
| `GetPolicyPermissions` | Allowed messages for a policy |
| `ListPolicyPermissions` | Paginated list of all permission sets |

## Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `proposal_fee` | string (coins) | 5,000,000 uspark | Anti-spam fee for group registration (waived for `x/gov`) |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Account and module address access |
| `x/bank` | Yes | Coin transfers for spending and fees |
| `x/group` | Yes | Underlying group and voting mechanics |
| `x/gov` | Yes | Governance authority and ultimate fallback |
| `x/futarchy` | No | Prediction market creation and resolution hooks |
| `x/split` | No | Treasury fund distribution by funding weight |
| `x/upgrade` | No | Chain upgrade scheduling |

## Genesis

The module bootstraps the entire Three Pillars governance structure at genesis via `BootstrapGovernance()`:

1. Creates **Commons Council** with all founding members (1-year term)
2. Creates **Technical Council** with founder + Commons as guardian veto, plus Ops and Governance committees
3. Creates **Ecosystem Council** with founder + Commons as guardian veto, plus Ops and Governance committees
4. Creates **Commons Supervisory Board** (Tech + Eco councils)
5. Wires electoral delegations (child committees control parent council membership)
6. Assigns funding weights and registers shares via `x/split`

## Security

- **Cycle detection** — prevents circular parent-child relationships via ancestry walk
- **Golden shares** — Technical and Ecosystem councils have Commons Council as guardian veto
- **Electoral separation** — HR committees are separate from operational committees
- **Permission ratcheting** — groups can reduce but not expand their own permissions
- **Rate limiting** — `SpendFromCommons` enforces per-epoch spending caps

## Client

### CLI

```bash
# Group management (via governance proposals)
sparkdreamd tx commons register-group --from authority
sparkdreamd tx commons spend-from-commons [recipient] [amount] --from policy

# Queries
sparkdreamd q commons get-extended-group "Technical Council"
sparkdreamd q commons list-extended-group
sparkdreamd q commons get-policy-permissions [policy_address]
sparkdreamd q commons params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/commons/v1/query.proto` for the full API surface.
