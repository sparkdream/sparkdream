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
CREATE → ACTIVE → RESOLVED_YES / RESOLVED_NO / RESOLVED_INVALID
  │         │                      │
  │  Buy/sell yes/no shares        │  Wait redemption_blocks
  │  (conditional tokens minted)   │  then MsgRedeem
  │                                │
  └── MsgCancelMarket ──► CANCELLED (refund remaining liquidity)
```

### Conditional Tokens

Trading mints conditional tokens with denom `f/{marketId}/{outcome}` (e.g. `f/1/yes`, `f/1/no`). These are 1:1 redeemable for collateral after resolution if the outcome matches.

### Confidence Voting Integration

When integrated with `x/commons` via `FutarchyHooks`:

- A confidence market is created at 50% of a council's term duration
- **"Yes" outcome** (`RESOLVED_YES`): term extends by +20% (incentivizes good performance)
- **"No" outcome** (`RESOLVED_NO`): term slashed by -50% (forces re-election)
- **Invalid** (`RESOLVED_INVALID`): both pools at zero, no tenure change

### Hooks

The module exposes a `FutarchyHooks` interface called on market resolution:

```go
type FutarchyHooks interface {
    AfterMarketResolved(ctx context.Context, marketId uint64, winner string) error
}
```

Multiple hooks can be combined via `MultiFutarchyHooks`.

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
| `denom` | string | Collateral token denomination |
| `min_tick` | Int | Minimum trade size |
| `b_value` | LegacyDec | Liquidity parameter (`initial_liquidity / ln(2)`) |
| `pool_yes` / `pool_no` | Int | Shares minted per outcome |
| `end_block` | int64 | When voting ends |
| `redemption_blocks` | int64 | Wait period after end |
| `resolution_height` | int64 | Block of resolution |
| `status` | string | ACTIVE, RESOLVED_YES, RESOLVED_NO, RESOLVED_INVALID, CANCELLED |
| `initial_liquidity` | Int | Seed liquidity amount |
| `liquidity_withdrawn` | Int | Amount withdrawn by creator |

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

Walks the `ActiveMarkets` index each block and resolves any markets where `end_block <= current_block`:

1. **Resolution**: `pool_yes > pool_no` → `RESOLVED_YES`; `pool_no > pool_yes` → `RESOLVED_NO`; both zero → `RESOLVED_INVALID`
2. **Cleanup**: removes resolved markets from the active index, sets `resolution_height`
3. **Hooks**: calls `AfterMarketResolved()` on registered `FutarchyHooks` (e.g. confidence voting integration)

## Client

### CLI

```bash
# Trading
sparkdreamd tx futarchy create-market "Q1-TECH" 100000 "Confidence in Technical Council?" 1000000 --from alice
sparkdreamd tx futarchy trade 1 true 1000 --from bob
sparkdreamd tx futarchy redeem 1 --from bob
sparkdreamd tx futarchy withdraw-liquidity 1 --from alice
sparkdreamd tx futarchy cancel-market 1 --reason "emergency" --from authority

# Queries
sparkdreamd q futarchy get-market 1
sparkdreamd q futarchy list-market
sparkdreamd q futarchy params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/futarchy/v1/query.proto` for the full API surface.
