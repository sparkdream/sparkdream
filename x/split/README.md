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

## BeginBlocker

Runs at the start of every block:

1. Fetch all registered shares and compute total weight
2. For each coin denomination in the distribution module's balance:
   - Skip if balance <= number of recipients (dust protection)
   - Calculate each recipient's proportional share
   - Transfer coins from distribution module to recipient
3. Log and continue on any transfer failures

## Client

### CLI

```bash
sparkdreamd q split get-share [address]
sparkdreamd q split list-share
sparkdreamd q split params
```
