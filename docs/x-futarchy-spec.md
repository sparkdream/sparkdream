# Technical Specification: `x/futarchy`

## 1. Abstract

The `x/futarchy` module implements an on-chain prediction market system using the Logarithmic Market Scoring Rule (LMSR) automated market maker. It is the accountability engine of the Spark Dream chain: prediction markets are used as confidence-vote instruments that elastically extend or shorten council tenure based on market sentiment, with no oracle dependency.

Key principles:
- **LMSR pricing**: a constant-function automated market maker with the cost function `C(qY, qN) = b * ln(e^(qY/b) + e^(qN/b))`, where `b` is a liquidity parameter derived from the creator's seed liquidity.
- **Trust-gated creation**: only ESTABLISHED+ members (verified via `x/rep`) can open markets, so prediction markets are not a vector for spam or grief by drive-by accounts.
- **Two-tier authorization**: governance-only parameters (`min_liquidity`, `default_min_tick`, `max_lmsr_exponent`) require a full `MsgUpdateParams` proposal, while operational parameters (`trading_fee_bps`, `max_duration`, `max_redemption_delay`) can be tuned by the Commons Council Operations Committee.
- **No oracle dependency**: market resolution is deterministic, derived from the share pools at `end_block`. No external data feed is needed and no human resolver is consulted.
- **Gas-metered math**: every iteration of the `Exp` and `Ln` series approximations consumes gas, so unbounded numerical work cannot starve consensus.
- **Hooks for elastic tenure**: when integrated with `x/commons`, market resolution drives council term extension (+20%), term slashing (-50%), or no change (invalid).
- **Fair settlement on cancellation**: cancelled markets and tied (`RESOLVED_INVALID`) markets snapshot an LMSR-implied price so existing share holders can redeem at a proportional share of the remaining collateral instead of having their funds trapped.

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/auth` | Address codec, module account resolution |
| `x/bank` | Liquidity transfers, conditional token mint/burn, fee payouts to `fee_collector` |
| `x/rep` | Trust-level gating (`GetTrustLevel`) for `MsgCreateMarket`. Wired post-depinject via `SetRepKeeper` |
| `x/commons` | *(optional)* Council authorization (`IsCouncilAuthorized`) for operational parameter updates. Wired post-depinject via `SetCommonsKeeper`. Falls back to gov-authority-only when not wired. Programmatic market creation (`CreateMarketInternal`) is initiated by the Commons keeper for confidence-vote markets, and the Commons keeper registers as a `FutarchyHooks` consumer to apply elastic tenure on resolution |

---

## 3. State Objects

### 3.1. Market

The single state object describing a prediction market and its full LMSR state.

```protobuf
message Market {
  // Metadata
  uint64 index = 1;
  string creator = 2;
  string symbol = 3;
  string question = 4;

  // Configuration
  string denom = 5;
  string min_tick = 6;        // cosmossdk.io/math.Int

  // Temporal properties
  int64 end_block = 7;
  int64 redemption_blocks = 8;
  int64 resolution_height = 9;
  string status = 10;

  // LMSR state
  string b_value = 11;        // cosmossdk.io/math.LegacyDec
  string pool_yes = 12;       // cosmossdk.io/math.Int
  string pool_no = 13;        // cosmossdk.io/math.Int

  // Liquidity tracking
  string initial_liquidity = 14;     // cosmossdk.io/math.Int
  string liquidity_withdrawn = 15;   // cosmossdk.io/math.Int

  // Settlement price for CANCELLED and RESOLVED_INVALID markets that have
  // outstanding shares. Empty for ACTIVE or RESOLVED_YES/NO markets.
  string settlement_price_yes = 16;  // cosmossdk.io/math.LegacyDec
}
```

Status values are stored as plain strings (no proto enum):

| Status | Meaning |
|--------|---------|
| `ACTIVE` | Market is open. Trades can be submitted. |
| `RESOLVED_YES` | EndBlocker found `pool_yes > pool_no`. Yes shares pay 1:1. |
| `RESOLVED_NO` | EndBlocker found `pool_no > pool_yes`. No shares pay 1:1. |
| `RESOLVED_INVALID` | Both pools are zero, or the pools tied. Outstanding shares (if any) redeem at the snapshotted LMSR-implied price. |
| `CANCELLED` | Governance terminated the market via `MsgCancelMarket`. Outstanding shares redeem at the LMSR-implied price snapshot. |

### 3.2. Params

Full parameter set, controlled by governance (`x/gov` authority).

```protobuf
message Params {
  string min_liquidity = 1;        // cosmossdk.io/math.Int
  int64 max_duration = 2;          // blocks
  string default_min_tick = 3;     // cosmossdk.io/math.Int
  int64 max_redemption_delay = 4;  // blocks
  uint64 trading_fee_bps = 5;      // basis points
  string max_lmsr_exponent = 6;    // numerical-stability ceiling for exp/ln
}
```

### 3.3. FutarchyOperationalParams

The subset of `Params` that the Commons Council Operations Committee can update via `MsgUpdateOperationalParams` (no full governance proposal required). Governance-only fields (`min_liquidity`, `default_min_tick`, `max_lmsr_exponent`) are excluded.

```protobuf
message FutarchyOperationalParams {
  uint64 trading_fee_bps = 1;
  int64 max_duration = 2;
  int64 max_redemption_delay = 3;
}
```

### 3.4. Conditional Tokens (Bank Denom)

Trades mint outcome shares as bank-native tokens with the denom format:

```
f/{market_id}/{outcome}    // outcome ∈ {"yes", "no"}
```

Example: market 1 yes shares are denominated `f/1/yes`. Holders custody and transfer these like any other bank coin and burn them on redemption.

---

## 4. Storage Schema

Using the Cosmos SDK collections framework:

| Collection | Key | Value | Purpose |
|------------|-----|-------|---------|
| `Params` | (singleton) | `Params` | Module parameters |
| `Market` | `uint64` (market id) | `Market` | Primary market record |
| `MarketSeq` | (singleton sequence) | `uint64` | Auto-incrementing market id |
| `ActiveMarkets` | `(int64 end_block, uint64 market_id)` pair | (key-only set) | Index of unresolved markets, ordered by `end_block` so the EndBlocker can walk only the prefix that has expired |

---

## 5. Messages

### 5.1. CreateMarket

Open a new prediction market. The creator deposits SPARK as initial liquidity; this seeds the LMSR `b` parameter.

```protobuf
message MsgCreateMarket {
  string creator = 1;
  string symbol = 2;
  string initial_liquidity = 3;  // cosmossdk.io/math.Int
  string question = 4;
  int64 end_block = 5;
}
```

**Validation:**
- `creator` must be a member with trust level `TRUST_LEVEL_ESTABLISHED` or higher (`x/rep.GetTrustLevel`). Fails closed if the rep keeper is not wired.
- `initial_liquidity` is non-nil, non-negative, and at least `min_liquidity`.
- `end_block - current_block` is positive and does not exceed `max_duration`.
- The resulting `sdk.Coin` (denom `uspark`, amount = `initial_liquidity`) is valid.

**Logic:**
1. Verify trust level via `x/rep`.
2. Compute `duration = end_block - current_block`; reject non-positive values with `ErrInvalidDuration`.
3. Pull `initial_liquidity` SPARK from the creator into the `futarchy` module account.
4. Compute `b_value = initial_liquidity / ln(2)` so the maximum subsidy loss equals `initial_liquidity`.
5. Allocate a fresh market id from `MarketSeq`.
6. Initialize `Market` with `pool_yes = pool_no = 0`, `min_tick = default_min_tick`, `liquidity_withdrawn = 0`, `status = ACTIVE`.
7. Persist the `Market` and add `(end_block, market_id)` to `ActiveMarkets`.
8. Emit `market_created` event.

`MsgCreateMarket` always uses zero `redemption_blocks` (Liquid Markets pattern). Programmatic creation via `CreateMarketInternal` (used by `x/commons`) supports a non-zero redemption delay.

### 5.2. Trade

Buy yes or no shares against an active market.

```protobuf
message MsgTrade {
  string creator = 1;
  uint64 market_id = 2;
  bool is_yes = 3;
  string amount_in = 4;  // cosmossdk.io/math.Int (collateral spent)
}
```

**Validation:**
- Market exists and `status == ACTIVE`.
- `amount_in` is non-nil, non-negative, denomination matches `market.denom`.
- `amount_in >= market.min_tick` (spam protection).

**Logic:**
1. Compute `fee = amount_in * trading_fee_bps / 10000` (truncated). Reject if the post-fee remainder is zero or negative.
2. Compute `currentCost = C(pool_yes, pool_no)` and `newCost = currentCost + (amount_in - fee)`.
3. Solve for the new winning pool using a numerically-stable rearrangement of the LMSR:
   ```
   newPool_target = newCost + b * ln(1 - exp((pool_loser - newCost) / b))
   shares_out = newPool_target - pool_target
   ```
   Reject the trade with "would deplete market liquidity" if the inner term `1 - exp(...)` is non-positive.
4. Send `amount_in` SPARK from the trader to the futarchy module account. Forward `fee` to the standard `fee_collector` module.
5. Mint `shares_out` of `f/{market_id}/{outcome}` and transfer to the trader.
6. Persist the updated `Market`.
7. Return `MsgTradeResponse{ shares_out }`.

Truncation note: `shares_out` is truncated to `Int`; trades that round to zero shares are rejected.

### 5.3. Redeem

Claim collateral after a market resolves.

```protobuf
message MsgRedeem {
  string creator = 1;
  uint64 market_id = 2;
}
```

**Validation:**
- Market is in a redeemable status (`RESOLVED_YES`, `RESOLVED_NO`, `CANCELLED`, `RESOLVED_INVALID`).
- If `redemption_blocks > 0`, then `current_block >= resolution_height + redemption_blocks`.
- The redeemer holds at least one outstanding share for this market.

**Logic:**

Two payout regimes:

- **Winning path** (`RESOLVED_YES` or `RESOLVED_NO`): the holder's full balance of the winning denom (`f/{id}/{winner}`) is burned and they receive an equal amount of `market.denom` 1:1. Losing shares are not redeemable but are also not actively burned.
- **Settled path** (`CANCELLED` or `RESOLVED_INVALID` with non-zero pools): both yes and no balances are burned and the holder receives `yes_balance * settlement_price_yes + no_balance * (1 - settlement_price_yes)`. The settlement price was snapshotted at resolution / cancellation time. Truncation losses are absorbed silently (dust loss is accepted).

If both pools were zero at resolution (`RESOLVED_INVALID` with no trades), no shares were ever minted and there is nothing to redeem; the creator's entire subsidy returns via `WithdrawLiquidity`.

### 5.4. WithdrawLiquidity

Allow the market creator to recover the LMSR residual after the market resolves.

```protobuf
message MsgWithdrawLiquidity {
  string creator = 1;
  uint64 market_id = 2;
}
```

**Validation:**
- Sender is the original `market.creator`.
- Market is in `RESOLVED_YES`, `RESOLVED_NO`, or `RESOLVED_INVALID` (cancellation refunds inside `MsgCancelMarket` itself).
- Computed residual exceeds `liquidity_withdrawn` (otherwise nothing is owed).

**Logic:**
1. Compute the LMSR creator residual via `computeCreatorResidual` (see Section 7.3).
2. Cap the residual at `initial_liquidity` defensively to absorb rounding above the theoretical bound.
3. Pay `residual - liquidity_withdrawn` SPARK to the creator and bump `liquidity_withdrawn`.
4. Emit `liquidity_withdrawn` event.

### 5.5. CancelMarket

Emergency governance termination of an active market.

```protobuf
message MsgCancelMarket {
  string authority = 1;   // x/gov authority
  uint64 market_id = 2;
  string reason = 3;
}
```

**Authorization:** must equal the `x/gov` authority address.

**Logic:**
1. Verify authority and that the market is `ACTIVE`.
2. Set `status = CANCELLED`, `resolution_height = current_block`, and remove the market from `ActiveMarkets`.
3. If any trades happened (`pool_yes > 0` or `pool_no > 0`), snapshot the LMSR-implied YES price into `settlement_price_yes` so holders can redeem at a fair price.
4. Compute and refund the creator's residual minus `liquidity_withdrawn`, capped at `initial_liquidity`.
5. Persist the market and emit `market_cancelled`.

This path is the only one that immediately settles the creator at cancellation time (other resolution paths require a separate `MsgWithdrawLiquidity` call).

### 5.6. UpdateParams

Full governance parameter update.

```protobuf
message MsgUpdateParams {
  string authority = 1;  // x/gov authority
  Params params = 2;
}
```

**Authorization:** must equal the `x/gov` authority. Returns `ErrInvalidSigner` otherwise.

### 5.7. UpdateOperationalParams

Operations Committee tuning of the operational subset.

```protobuf
message MsgUpdateOperationalParams {
  string authority = 1;
  FutarchyOperationalParams operational_params = 2;
}
```

**Authorization:** governance authority, Commons Council policy address, or Commons Operations Committee member, verified via `x/commons.IsCouncilAuthorized(addr, "commons", "operations")`. Falls back to gov-authority-only if `x/commons` is not wired.

**Logic:**
1. Verify authorization.
2. Validate `operational_params` standalone.
3. Merge with current params (`ApplyOperationalParams` overwrites `trading_fee_bps`, `max_duration`, `max_redemption_delay` and preserves the governance-only fields).
4. Re-validate the merged params and persist.

---

## 6. Queries

| Query | Input | Output | Description |
|-------|-------|--------|-------------|
| `Params` | (none) | `Params` | Current module parameters |
| `GetMarket` | `index` (market id) | `Market` | Single market by id |
| `ListMarket` | pagination | `[]Market` | All markets (paginated) |
| `GetMarketPrice` | `market_id`, `is_yes`, `amount` | `price`, `shares_out` | Quote for a hypothetical trade without mutating state |

---

## 7. Business Logic

### 7.1. LMSR Pricing

The cost function and price are:

```
C(qY, qN) = b * ln(e^(qY/b) + e^(qN/b))
p_yes(qY, qN) = exp(qY/b) / (exp(qY/b) + exp(qN/b))
```

The implementation in `types/lmsr.go` is numerically stabilized:
- `Exp` and `Ln` are Maclaurin / Mercator series with a 100-iteration cap and an `Epsilon = 1e-8` early-exit.
- `Exp` and `Ln` consume `LmsrIterationGasCost = 200` gas per iteration via the SDK gas meter (`lmsr_exp_iteration` / `lmsr_ln_iteration`). This bounds worst-case work and lets the standard gas mechanism pay for arithmetic.
- All `(q/b)` exponents are clamped to the range `[-max_lmsr_exponent, max_lmsr_exponent]` (`ClampExponent`) before being passed to `Exp`.
- `C` is computed as `max + ln(e^(x-max) + e^(y-max))` to avoid overflow when one pool is much larger than the other.

`Ln(x)` is undefined for `x <= 0` and returns an error rather than panicking; trade computation surfaces this as `ErrInvalidRequest` with "trade too large: would deplete market liquidity".

### 7.2. Settlement Price Snapshot

When a market is cancelled with non-zero pools, or auto-resolves to `RESOLVED_INVALID` with non-zero pools, the EndBlocker / cancel handler computes:

```
SettlementPriceYes(b, qY, qN) = exp(qY/b) / (exp(qY/b) + exp(qN/b))
```

(or `0.5` when both pools are zero) and stores it in `Market.SettlementPriceYes`. This snapshot is the price at which `Redeem` pays the holders of yes and no shares, ensuring no funds are trapped after a market is terminated without a clean winner.

### 7.3. Creator Residual

The creator's correct withdrawal amount depends on the resolution regime (`x/futarchy/keeper/settlement.go::computeCreatorResidual`):

| Regime | Formula | Notes |
|--------|---------|-------|
| `RESOLVED_YES` / `RESOLVED_NO` | `b * ln(1 + exp((q_loser - q_winner) / b))` | Winners are paid 1:1 from the module account; the residual is the leftover collateral. |
| `CANCELLED` / `RESOLVED_INVALID` (non-zero pools) | `b * H(p_yes)` where `H(p) = -p ln(p) - (1-p) ln(1-p)` | Equal to entropy of the implied distribution scaled by `b`. Equivalent to `C(qY, qN) - qY * p_yes - qN * p_no`. |
| `CANCELLED` / `RESOLVED_INVALID` (zero pools) | `initial_liquidity` | No trades, full subsidy refund. |
| Other statuses | error | Withdrawal not allowed. |

The residual is always capped at `initial_liquidity` defensively, even though the math should never exceed that bound.

### 7.4. EndBlocker (Resolution Loop)

Each block, `EndBlocker` walks `ActiveMarkets` over the prefix `[*, current_height]`:

1. Load the `Market`. If missing or non-`ACTIVE`, drop the index entry and continue (orphan cleanup).
2. Resolve based on pool comparison:
   - `pool_yes > pool_no` -> `RESOLVED_YES`
   - `pool_no > pool_yes` -> `RESOLVED_NO`
   - tied (including both zero) -> `RESOLVED_INVALID`
3. Set `resolution_height = current_height`.
4. For `RESOLVED_INVALID` with non-zero pools, snapshot `settlement_price_yes`.
5. Persist the market and remove from `ActiveMarkets`.
6. Call `Hooks.AfterMarketResolved(market_id, "yes" | "no")` for `RESOLVED_YES` / `RESOLVED_NO`. `RESOLVED_INVALID` skips the hook (no winner to announce). Hook errors are logged but do not halt the chain.
7. Emit `market_resolved` event.

### 7.5. Hooks

The module exposes a `FutarchyHooks` interface so other modules can react to market resolution:

```go
type FutarchyHooks interface {
    AfterMarketResolved(ctx context.Context, marketId uint64, winner string) error
}
```

`MultiFutarchyHooks` composes multiple hook subscribers. The hook is registered via the keeper's `SetHooks` method (single-set; subsequent calls panic).

### 7.6. Confidence-Vote Integration with `x/commons`

When integrated with `x/commons`, the futarchy module is the engine of elastic council tenure (`x/commons/keeper/governance_logic.go` and `hooks.go`):

- At 50% of a council's term, `x/commons.TriggerGovernanceMarket` programmatically calls `CreateMarketInternal` (bypassing the public `MsgCreateMarket` and its trust-level gate) with:
  - `symbol = "CONF-{group}-{height}"`, `question = "Confidence Vote: {group}"`
  - `duration = 72000` blocks (~5 days at 6s blocks)
  - `redemption_blocks = 302400` (~21 days)
  - `subsidy = 1000 SPARK` paid by the commons module account
- The created `market_id` is recorded in `MarketToGroup` so the resolution hook can find the linked council.
- On `AfterMarketResolved`:
  - `winner = "yes"` (high confidence) -> council's `current_term_expiration` extended by `term_duration / 5` (+20%), capped at two terms from `current_time`.
  - `winner = "no"` (low confidence) -> `current_term_expiration` reduced by `term_duration / 2` (-50%), floored at `current_time` so it never expires in the past.
  - `RESOLVED_INVALID` -> no tenure change; emits `market_invalid_no_quorum`.

### 7.7. Genesis

`InitGenesis` re-imports every market exactly as exported, then rebuilds the `ActiveMarkets` index for any market still in `ACTIVE` so the EndBlocker can pick it up after import. `MarketSeq` is seeded to `max(imported_index) + 1` so freshly-created markets cannot collide with imported ids. `ExportGenesis` walks the full `Market` collection and emits the params snapshot.

---

## 8. Default Parameters

| Parameter | Default | Rationale |
|-----------|---------|-----------|
| `min_liquidity` | 100,000 (`uspark`) | Minimum subsidy a creator must commit, prevents zero-`b` markets |
| `max_duration` | 5,256,000 blocks (~1 year at 6s) | Upper bound on how long collateral can sit in a market |
| `default_min_tick` | 1,000 (`uspark`) | Per-trade minimum, spam protection |
| `max_redemption_delay` | 5,256,000 blocks (~1 year) | Upper bound on the post-resolution lock |
| `trading_fee_bps` | 30 (0.3%) | Sent to `fee_collector`; the standard validator fee path |
| `max_lmsr_exponent` | "20" | Numerical-stability ceiling for `Exp` / `Ln` arguments |

Confidence-vote markets created by `x/commons` use a 1000 SPARK subsidy, ~5-day duration, and ~21-day redemption delay (constants in `x/commons/keeper/governance_logic.go`).

---

## 9. Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrInvalidSigner` | 1100 | Authority mismatch on `MsgUpdateParams` / `MsgUpdateOperationalParams` |
| `ErrInvalidDuration` | 1101 | `end_block <= current_block` on market creation |

The module also returns standard `cosmos-sdk/types/errors` for input-shape problems (`ErrInvalidRequest`, `ErrUnauthorized`, `ErrInvalidAddress`, `ErrInvalidCoins`, `ErrNotFound`, `ErrInsufficientFunds`).

---

## 10. Events

| Event | Attributes | Trigger |
|-------|------------|---------|
| `market_created` | `market_id`, `creator`, `symbol`, `liquidity` | `CreateMarketInternal` (called by `MsgCreateMarket` and by `x/commons`) |
| `market_resolved` | `market_id`, `end_block`, `outcome` | EndBlocker resolution |
| `market_cancelled` | `market_id`, `reason`, `refunded` | `MsgCancelMarket` |
| `liquidity_withdrawn` | `market_id`, `creator`, `amount` | `MsgWithdrawLiquidity` |
| `elastic_tenure` | `group`, `action` (extended/shortened), `seconds` | Resolution hook in `x/commons` |
| `market_invalid_no_quorum` | `group`, `action` | Resolution hook on `RESOLVED_INVALID` |

---

## 11. Integration Points

### 11.1. Programmatic Market Creation

`x/commons` (and any future internal caller) opens markets without a user transaction by calling the keeper directly:

```go
marketId, err := futarchyKeeper.CreateMarketInternal(
    ctx,
    creator,            // module account (e.g. x/commons module address)
    symbol,             // "CONF-Commons-1234"
    question,           // "Confidence Vote: Commons"
    durationBlocks,     // 72000
    redemptionBlocks,   // 302400
    subsidyCoin,        // 1000 SPARK from commons treasury
)
```

This path skips `MsgCreateMarket`'s trust-level check (the caller is a module account, not a member). Validation for `min_liquidity`, `max_duration`, and `max_redemption_delay` still applies.

### 11.2. Hook Subscription

`x/commons` registers itself as the hook consumer at app wiring time:

```go
futarchyKeeper.SetHooks(commonsKeeper)  // Keeper implements FutarchyHooks
```

`MultiFutarchyHooks` is available for composing multiple consumers if more modules need to react in the future.

### 11.3. Late-Wired Keepers

`x/futarchy` depends on `x/rep` (for trust gating) and `x/commons` (for council authorization). Both are late-wired in `app.go` via `SetRepKeeper` and `SetCommonsKeeper`, sharing a `lateKeepers` pointer so all value copies of the keeper see the update. This breaks the cycle that would otherwise form: `commons -> futarchy -> rep -> commons`.

---

## 12. Security Considerations

### 12.1. Trust-Level Gating

`MsgCreateMarket` requires `TRUST_LEVEL_ESTABLISHED` and fails closed when the rep keeper is unwired (rather than allowing creation by default). This limits market creation to accounts that have established themselves in the system, preventing a flood of low-quality or grief markets.

### 12.2. Numerical Stability

Without clamps, `exp(q/b)` overflows once one pool grows much larger than the other. The `max_lmsr_exponent` bound combined with the `(x - max)` rebasing in `CalculateLMSRCost` and `SettlementPriceYes` keeps every series argument in a safe range. Trades that would push the inner term into a negative or zero region (i.e. trades that try to buy more shares than the LMSR can serve) are rejected with "would deplete market liquidity" rather than panicking.

### 12.3. Gas-Metered Math

Each `Exp` / `Ln` iteration consumes `LmsrIterationGasCost = 200` gas, capped at `MaxIterations = 100`. This ensures the worst-case cost of a trade is bounded and paid for by the caller, even though the series loops are not constant-time.

### 12.4. Trapped-Funds Prevention (FUTARCHY-S2-1)

Earlier versions refunded the creator the full `initial_liquidity` on cancellation and never paid out outstanding shares. That behavior trapped trader funds in the module account whenever a market was cancelled or resolved invalid with non-zero pools. The current implementation:

- Snapshots `settlement_price_yes` at cancellation / invalid-resolution time.
- Pays the creator only the LMSR residual (`b * H(p_yes)`), bounded by entropy of the implied distribution.
- Pays each share holder their LMSR-implied value via `MsgRedeem`.

This guarantees the module account is solvent: creator residual + holder payouts = collateral on hand.

### 12.5. Pool Pointer Aliasing

`CreateMarketInternal` deliberately allocates separate `math.Int` variables for `pool_yes` and `pool_no`. Sharing a pointer would alias the two pools and cause mutations to one to mutate both, silently corrupting LMSR state.

### 12.6. Active Index Cleanup

The EndBlocker is defensive about the `ActiveMarkets` index. Orphan entries (market missing) and stale entries (market not in `ACTIVE`) are removed and skipped instead of failing the block.

### 12.7. Two-Tier Authority

Critical pricing parameters (`min_liquidity`, `default_min_tick`, `max_lmsr_exponent`) are governance-only so the Operations Committee cannot reduce the LMSR safety bounds without a full proposal. Operational knobs (fee, durations) move through the lighter committee path.

---

## 13. Future Considerations

1. **More-than-binary markets**: extend the LMSR cost function to `n` outcomes.
2. **Market parameter recording**: persist the LMSR `max_lmsr_exponent` snapshot per market so resolution math is reproducible across param changes.
3. **Gas refund on small trades**: optionally rebate truncation-induced dust losses on `Redeem`.
4. **Generalized hooks**: add `BeforeMarketResolved` / `AfterMarketCancelled` if other modules need finer-grained reactions.
5. **Conviction-weighted markets**: integrate with `x/rep` conviction so reputation can be staked alongside SPARK in confidence votes.

---

## 14. File References

- Proto definitions: `proto/sparkdream/futarchy/v1/*.proto`
- Keeper logic: `x/futarchy/keeper/`
  - LMSR market math entry points: `keeper/msg_server_create_market.go`, `keeper/msg_server_trade.go`, `keeper/market_logic.go`
  - Resolution and settlement: `module/abci.go`, `keeper/settlement.go`
  - Cancellation, redemption, withdrawal: `keeper/msg_server_cancel_market.go`, `keeper/msg_server_redeem.go`, `keeper/msg_server_withdraw_liquidity.go`
  - Late keeper wiring: `keeper/keeper.go`, `keeper/keeper_helpers.go`
- LMSR primitives and gas metering: `x/futarchy/types/lmsr.go`
- Types, errors, hooks, expected keepers: `x/futarchy/types/`
- Module setup, EndBlocker, genesis: `x/futarchy/module/`
- Confidence-vote integration: `x/commons/keeper/governance_logic.go`, `x/commons/keeper/hooks.go`
- README summary: `x/futarchy/README.md`
