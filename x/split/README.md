# `x/split`

The `x/split` module implements automated revenue distribution that runs in BeginBlock. It splits coins from the `x/distribution` Community Pool to registered treasury recipients based on dynamic weight allocations.

## Overview

This module provides:

- **Automatic distribution** — runs every block in BeginBlock without explicit calls
- **Weight-based allocation** — each recipient has a proportional weight; distribution is `share = (balance * weight) / total_weight`
- **Dust optimization** — skips distributions where total balance < number of recipients to prevent wasteful gas
- **Denom-agnostic** — handles any coin type in the Community Pool
- **Graceful degradation** — logs errors on failed transfers but continues processing

## Concepts

### Revenue Flow

```
SPARK Inflation + Tx Fees
    │
    ├── 85% → Validators / Delegators
    └── 15% → Community Pool (x/distribution)
                │
                └── x/split BeginBlock distributes to:
                    ├── Commons Council  — 50% weight
                    ├── Technical Council — 30% weight
                    └── Ecosystem Council — 20% weight
```

### Cross-Module Wiring

`x/commons` calls `SetShareByAddress()` during council setup to register treasury recipients and their weights. The split module has no awareness of governance structure — it simply distributes to addresses by weight.

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Params` | `p_split` | Module parameters (currently empty) |
| `Share` | `share/value/{address}` | Recipient address and weight |

## Messages

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update module parameters (placeholder) | `x/gov` authority only |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters (currently empty) |
| `GetShare` | Retrieve recipient weight by address |
| `ListShare` | All recipients (paginated) |

## Parameters

None defined. Placeholder for future governance-controlled distribution rules.

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Module address resolution |
| `x/bank` | Yes | Balance queries and coin transfers |
| `x/distribution` | Yes | Community pool queries (`GetCommunityPool`) and fund distribution (`DistributeFromFeePool`) |

### Cyclic Dependency Breaking

`DistrKeeper` is wired manually in `app.go` via `SetDistrKeeper()` after `depinject.Inject()` (using a `DistrKeeperAdapter` that wraps the SDK's distribution keeper). Held in a shared `lateKeepers` struct so value-copies of Keeper see updates.

## BeginBlocker

Runs at the start of every block:

1. Fetch all registered shares and compute total weight (skip if zero)
2. Query the community pool balance via `DistrKeeper.GetCommunityPool()` and truncate to integer coins
3. For each coin denomination in the pool:
   - Skip if amount <= number of recipients (dust protection)
   - For each recipient, calculate `share_amount = (amount * weight) / total_weight`
   - Call `DistrKeeper.DistributeFromFeePool()` to transfer coins to recipient
   - If pool is exhausted mid-distribution, log and continue to next denomination
4. All errors are non-fatal — the module logs but never panics

## Client

### CLI

```bash
sparkdreamd q split params
sparkdreamd q split get-share [address]    # alias: show-share
sparkdreamd q split list-share
```

**Note:** `MsgUpdateParams` is authority-gated (`x/gov` only) and not exposed via CLI.

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/split/v1/query.proto` for the full API surface.
