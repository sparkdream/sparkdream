# `x/name`

The `x/name` module is the identity registry for the Spark Dream chain, implementing human-readable name registration with governance-controlled access, reverse resolution, inactivity-based scavenging, and DREAM-staked dispute resolution.

## Overview

This module provides:

- **Council-gated registration** — only Commons Council members can register names, preventing squatting
- **Forward and reverse resolution** — name-to-address and address-to-primary-name lookups
- **Inactivity scavenging** — names become available after 1 year of owner inactivity (default)
- **Dispute resolution** — DREAM-staked disputes with jury arbitration via `x/rep`
- **Blocked names** — 100+ reserved names prevent system/brand impersonation
- **Per-address limits** — max 5 names per address (default) prevents hoarding

## Concepts

### Name Registration

Names follow strict formatting: lowercase alphanumeric with optional hyphens (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`), 3-30 characters. Registration requires Commons Council membership and a 10 SPARK fee (default). The first registered name automatically becomes the owner's primary name for reverse resolution.

### Name Scavenging

Names expire based on owner inactivity (1 year default). Activity triggers (registration, updates, primary name changes) reset the inactivity timer. When registering an existing name, if the owner is expired, the name transfers to the new registrant automatically.

### Dispute Resolution

Any address can file a dispute by staking 50 DREAM. The current owner can contest by staking 100 DREAM, which escalates to a jury review in `x/rep`. The Commons Council resolves disputes; uncontested disputes auto-resolve after ~7 days. Losing party's DREAM stake is burned.

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `NameRecord` | `name/value/{name}` | Name, owner address, and metadata |
| `OwnerInfo` | `owner/value/{address}` | Primary name, last active time |
| `Dispute` | `dispute/value/{name}` | Active dispute with claimant, stakes, jury reference |

### Additional Collections

| Object | Key | Description |
|--------|-----|-------------|
| `DisputeStake` | `dispute_stakes/{challenge_id}` | Claimant DREAM stake record |
| `ContestStake` | `contest_stakes/{challenge_id}` | Owner DREAM contest stake record |

### Indexes

| Index | Purpose |
|-------|---------|
| `OwnerNames` (address, name) | List names owned by address |

## Messages

| Message | Description | Access |
|---------|-------------|--------|
| `MsgRegisterName` | Register a new name or scavenge an expired one | Commons Council members |
| `MsgUpdateName` | Update metadata for an owned name | Name owner only |
| `MsgSetPrimary` | Set primary name for reverse resolution | Name owner only |
| `MsgFileDispute` | Challenge name ownership (stakes 50 DREAM) | Any address |
| `MsgContestDispute` | Contest a dispute (stakes 100 DREAM, triggers jury) | Current name owner |
| `MsgResolveDispute` | Resolve a dispute (transfer or dismiss) | Commons Council or `x/gov` |
| `MsgUpdateParams` | Update governance-controlled parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Commons Operations Committee |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `Resolve` | Look up name → address + data |
| `ReverseResolve` | Look up address → primary name |
| `Names` | List all names owned by address (paginated) |
| `GetDispute` | Active dispute for a specific name |
| `ListDispute` | All active disputes (paginated) |

## Parameters

### Governance-Controlled

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `blocked_names` | []string | 100+ names | Reserved names (admin, founder, etc.) |
| `min_name_length` | uint64 | 3 | Minimum characters |
| `max_name_length` | uint64 | 30 | Maximum characters |
| `max_names_per_address` | uint64 | 5 | Per-address name limit |

### Operationally-Controlled

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `expiration_duration` | Duration | 1 year | Inactivity timeout for scavenging |
| `registration_fee` | Coin | 10 SPARK | Fee to register a name |
| `dispute_stake_dream` | Int | 50 DREAM | Claimant's stake to file a dispute |
| `dispute_timeout_blocks` | uint64 | 100,800 | Auto-resolve timeout (~7 days) |
| `contest_stake_dream` | Int | 100 DREAM | Owner's stake to contest a dispute |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec |
| `x/bank` | Yes | Registration fee collection |
| `x/commons` | Yes | Council membership checks, authorization, group/policy management |
| `x/rep` | Yes | DREAM lock/unlock/burn for disputes; jury integration |

## BeginBlocker

Processes expired disputes each block:

- Iterates all active uncontested disputes
- Auto-upholds disputes where `current_height > filed_at + dispute_timeout_blocks`
- Uncontested expired disputes: name transfers to claimant, claimant's DREAM stake unlocked
- Contested disputes are skipped (await jury/council resolution via `MsgResolveDispute`)

## Client

### CLI

```bash
# Registration
sparkdreamd tx name register-name alice --data "ipfs://..." --from council_member
sparkdreamd tx name update-name alice --data "new metadata" --from alice
sparkdreamd tx name set-primary alice --from alice

# Disputes
sparkdreamd tx name file-dispute alice --reason "..." --from claimant
sparkdreamd tx name contest-dispute alice --reason "..." --from owner
sparkdreamd tx name resolve-dispute alice [new_owner] true --from authority

# Queries
sparkdreamd q name resolve alice
sparkdreamd q name reverse-resolve [address]
sparkdreamd q name names [address]
sparkdreamd q name get-dispute alice
sparkdreamd q name params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/name/v1/query.proto` for the full API surface.
