# `x/split`

The `x/split` module implements automated revenue distribution that runs in BeginBlock. It splits the chain's bond denom (SPARK) from the `x/distribution` Community Pool to registered treasury recipients based on dynamic weight allocations.

## Overview

This module provides:

- **Automatic distribution** — runs every block in BeginBlock without explicit calls
- **Weight-based allocation** — each recipient has a proportional weight; distribution is `share = (balance * weight) / total_weight`
- **Dust optimization** — skips distributions where total balance < number of recipients to prevent wasteful gas
- **Bond denom only** — distributes only the chain's bond denom, resolved through x/identity. Anyone can donate a foreign denom to the Community Pool, so distributing every denom would let the pool's denom count set the per-block work; other denoms stay in the pool, where x/gov can spend them
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
                    ├── Commons pillar    — 50% (Council 47.5%, Commons Operations 2.5%)
                    ├── Technical pillar  — 30% (Council 28.5%, Technical Operations 1.5%)
                    └── Ecosystem pillar  — 20% (Council 19%,   Ecosystem Operations 1%)

Within each pillar, 5% of the pillar's share is routed directly to its
Operations Committee so day-to-day spending has its own continuous budget
without a council `MsgSpendFromCommons` proposal. Governance committees and
the Supervisory Board keep `FundingWeight=0` because their
`MaxSpendPerEpoch` is also 0.
```

### Cross-Module Wiring

`x/commons` calls `SetShareByAddress()` during council setup to register treasury recipients and their weights, and `RemoveShareByAddress()` when a group is deleted. The split module has no awareness of governance structure — it simply distributes to addresses by weight.

`SetShareByAddress` returns an error (propagated by x/commons, so a refused weight is never recorded on the group) for a zero weight, a weight above `MaxShareWeight` (10000), or a weight that would push the sum of all shares above `MaxTotalShareWeight` (10000; the genesis pillars total about 1000). Only x/gov can give a group a non-zero weight: a council-registered group must have `funding_weight` 0, and `MsgUpdateGroupConfig.funding_weight` (which replaces a live group's share) is gov-only. Every change emits `split_share_set` / `split_share_removed`.

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

None defined. Placeholder for future governance-controlled distribution rules. Weights are changed through x/commons (`MsgRegisterGroup` / `MsgUpdateGroupConfig` by x/gov, `MsgDeleteGroup`).

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Module address resolution |
| `x/bank` | Yes | Balance queries and coin transfers |
| `x/distribution` | Yes | Community pool queries (`GetCommunityPool`) and fund distribution (`DistributeFromFeePool`) |
| `x/identity` | Yes | The bond denom to distribute (`BondDenom`) |

### Cyclic Dependency Breaking

`DistrKeeper` and `IdentityKeeper` are wired manually in `app.go` via `SetDistrKeeper()` (using a `DistrKeeperAdapter` that wraps the SDK's distribution keeper) and `SetIdentityKeeper()` after `depinject.Inject()`. Held in a shared `lateKeepers` struct so value-copies of Keeper see updates. If either is unwired, BeginBlock logs and skips distribution.

## BeginBlocker

Runs at the start of every block:

1. Fetch all registered shares and compute total weight (skip if zero)
2. Query the community pool balance via `DistrKeeper.GetCommunityPool()` and truncate the bond denom's amount to whole units (other denoms are ignored)
3. Skip the block if the amount is below `MinDistributionAmount` (1,000,000 base units, i.e. 1 SPARK) or not above the number of recipients (dust protection)
4. For each recipient:
   - Calculate `share_amount = (amount * weight) / total_weight`
   - Call `DistrKeeper.DistributeFromFeePool()` to transfer coins to recipient and emit `split_distribution` (`recipient`, `amount`, `weight`)
   - If the pool cannot pay a recipient, skip it and continue with the next. The starting recipient rotates with the block height, so a shortfall does not always hit the same addresses
5. All errors are non-fatal — store and pool errors are logged and the block moves on (BeginBlock never halts the chain)

## Invariants

`ShareWeightsInvariant` (run by app's `TestInvariants`): every share has a valid address and a weight in (0, 10000], and the weights sum to at most 10000. `GenesisState.Validate` enforces the same per-share and total caps on import.

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
