# `x/futarchy`

The `x/futarchy` module implements Logarithmic Market Scoring Rule (LMSR) prediction markets for elastic tenure confidence voting. Council members' tenure can be extended (+20%) or slashed (-50%) based on market sentiment.

## Overview

This module provides:

- **LMSR automated market maker** — constant function market with `C = b * ln(e^(y/b) + e^(n/b))` pricing
- **Confidence voting** — prediction markets for council tenure adjustments
- **Gas-metered math** — exp/ln calculations consume gas per iteration for numerical stability
- **Two-tier authorization** — governance-only and Operations Committee parameter tiers
- **No oracle dependency** — resolution via governance/committee, not external data feeds

## Concepts

### LMSR Pricing Model

The market uses a Logarithmic Market Scoring Rule:

- `b` = liquidity parameter (derived from initial liquidity)
- `y` = yes shares, `n` = no shares
- Price: `p = 1 / (1 + e^((n-y)/b))`
- Cost to buy: marginal cost integrated over share quantity
- Exponents clamped at `max_lmsr_exponent` (default 20) to prevent overflow

### Market Lifecycle

```
CREATE → TRADING → CLOSED → RESOLVED → REDEEMED
  │                   │         │
  │    Buy/sell       │  Wait   │  Claim
  │    yes/no shares  │  redemp │  winnings
  │                   │  blocks │
  └───────────────────┴─────────┘
```

### Confidence Voting Integration

When integrated with `x/commons`:

- A confidence market is created at 50% of a council's term duration
- **"Yes" outcome**: term extends by +20% (incentivizes good performance)
- **"No" outcome**: term slashed by -50% (forces re-election)
- **No quorum**: neutral (no tenure change)

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Market` | `market/value/{id}` | Market metadata, LMSR state, lifecycle status |
| `MarketSeq` | `market/seq/` | Auto-incrementing market ID |
| `ActiveMarkets` | `active_markets/{end_block}/{id}` | Markets pending resolution |

### Market Fields

| Field | Type | Description |
|-------|------|-------------|
| `index` | uint64 | Market ID |
| `creator` | string | Market creator address |
| `symbol` | string | Market identifier |
| `question` | string | What's being voted on |
| `b_value` | Dec | Liquidity parameter |
| `pool_yes` / `pool_no` | Int | Share pools |
| `end_block` | int64 | When voting ends |
| `redemption_blocks` | int64 | Wait period after end |
| `resolution_height` | int64 | Block of resolution |
| `status` | enum | ACTIVE, CLOSED, RESOLVED, CANCELLED |
| `initial_liquidity` | Int | Seed liquidity amount |

## Messages

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateMarket` | Create market with initial liquidity and question | Any address |
| `MsgTrade` | Buy yes/no shares | Any address |
| `MsgRedeem` | Claim winnings after resolution | Share holder |
| `MsgWithdrawLiquidity` | Withdraw remaining liquidity after redemption | Market creator |
| `MsgCancelMarket` | Cancel active market with reason | `x/gov` authority |
| `MsgUpdateParams` | Update all parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Commons Operations Committee |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `GetMarket` | Single market by ID |
| `ListMarket` | All markets (paginated) |
| `GetMarketPrice` | Price calculation for hypothetical trade (market_id, is_yes, amount) |

## Parameters

### Governance-Controlled

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `min_liquidity` | Int | 100,000 | Minimum initial liquidity |
| `default_min_tick` | Int | 1,000 | Minimum trade size |
| `max_lmsr_exponent` | string | "20" | Max exp/ln exponent for stability |

### Operationally-Controlled

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `trading_fee_bps` | uint64 | 30 | Fee in basis points (0.3%) |
| `max_duration` | int64 | 5,256,000 | Max market lifetime in blocks (~1 year) |
| `max_redemption_delay` | int64 | 5,256,000 | Max wait after end_block |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address validation, module accounts |
| `x/bank` | Yes | Mint/burn coins, liquidity transfers |
| `x/commons` | No | Council authorization for operational param updates |

## EndBlocker

- Resolves markets when `end_block` is reached
- Transitions markets to redemption window
- Cleans up expired markets
- Triggers confidence vote markets on schedule (via `x/commons` hook)

## Client

### CLI

```bash
# Trading
sparkdreamd tx futarchy create-market --symbol "Q1-TECH" --question "Confidence in Technical Council?" --initial-liquidity 100000 --end-block 1000000 --from alice
sparkdreamd tx futarchy trade 1 --is-yes true --amount-in 1000 --from bob
sparkdreamd tx futarchy redeem 1 --from bob

# Queries
sparkdreamd q futarchy get-market 1
sparkdreamd q futarchy list-market
sparkdreamd q futarchy get-market-price 1 true 1000
sparkdreamd q futarchy params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/futarchy/v1/query.proto` for the full API surface.
