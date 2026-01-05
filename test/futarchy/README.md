# Futarchy Module Integration Tests

This directory contains comprehensive integration tests for the `x/futarchy` prediction market module.

## Test Overview

### 1. `market_lifecycle_test.sh`
**Purpose**: Tests the complete lifecycle of a prediction market from creation to resolution.

**Scenarios Covered**:
- Market creation with proper parameters
- Querying market details and status
- Price discovery queries (GetMarketPrice)
- Trading YES and NO shares
- Conditional token distribution (f/{marketId}/yes and f/{marketId}/no)
- Price updates after trades
- Market listing and pagination
- Automatic market resolution at end block
- Share redemption for winners

**Key Validations**:
- Market creation events emitted correctly
- LMSR pricing mechanism works
- YES price increases after YES purchases
- Conditional tokens minted to traders
- Market resolves to correct outcome
- Winners can redeem shares for payouts

---

### 2. `governance_integration_test.sh`
**Purpose**: Demonstrates futarchy's integration with governance for decision-making.

**Scenarios Covered**:
- Creating prediction markets for governance proposal outcomes
- Multiple participants trading based on their beliefs
- Market sentiment analysis via price signals
- Parallel governance proposal submission
- Comparing market predictions vs. actual governance outcomes
- Rewarding accurate predictors

**Key Validations**:
- Markets correctly predict governance outcomes
- Price signals reflect community consensus
- Conditional tokens track participant positions
- Winners receive payouts when predictions are correct
- Integration with Commons Council futarchy settings

**Governance Use Cases**:
- Elastic tenure: Market outcomes could adjust council term durations
- Decision validation: Markets provide confidence signals for proposals
- Information aggregation: Collective intelligence informs governance

---

### 3. `params_update_test.sh`
**Purpose**: Tests governance-controlled parameter updates for the futarchy module.

**Parameters Tested**:
- `min_liquidity`: Minimum required liquidity for market creation
- `max_duration`: Maximum market duration in blocks
- `trading_fee_bps`: Trading fee in basis points (1 bps = 0.01%)
- `default_min_tick`: Minimum trade size
- `max_redemption_delay`: Maximum redemption delay after resolution
- `max_lmsr_exponent`: Numerical stability limit for LMSR calculations

**Scenarios Covered**:
- Querying initial parameters
- Creating markets with current minimum liquidity
- Submitting governance proposal to update parameters
- Voting and passing parameter update
- Verifying parameters updated on-chain
- Market creation failing below new minimum
- Market creation succeeding with new parameters
- Trading with new fee structure

**Key Validations**:
- Only governance can update parameters (authority check)
- Parameter validation enforced (e.g., positive values)
- New parameters apply to future markets immediately
- Existing markets unaffected by parameter changes

---

### 4. `liquidity_withdrawal_test.sh`
**Purpose**: Tests the liquidity withdrawal mechanism for market creators.

**Scenarios Covered**:
- Market creator provides initial liquidity
- Trading consumes some liquidity (mints conditional tokens)
- Attempting withdrawal before market resolution (fails)
- Attempting withdrawal by non-creator (fails)
- Creator successfully withdraws remaining liquidity after resolution
- Verifying liquidity_withdrawn field updates
- Creator balance increases with recovered funds
- Attempting second withdrawal with no liquidity available (fails)

**Key Validations**:
- Only resolved markets allow withdrawal
- Only market creator can withdraw
- Liquidity accounting accurate (initial - minted shares - withdrawn)
- Multiple withdrawals prevented once liquidity exhausted
- Market state correctly tracks withdrawals

**Economic Model**:
- Creator provides liquidity = provides market depth
- Traders consume liquidity = mint conditional shares
- Remaining liquidity = initial - shares_minted
- Creator can recover unused liquidity after resolution

---

### 5. `emergency_cancel_test.sh`
**Purpose**: Tests emergency market cancellation via governance authority.

**Scenarios Covered**:
- Creating a market with trading activity
- Verifying market is active with traders holding shares
- Attempting unauthorized cancellation (fails)
- Submitting governance proposal to cancel market
- Voting on and passing cancellation proposal
- Verifying market status changes to CANCELLED
- Verifying resolution height is set
- Verifying liquidity refunded to creator
- Trading disabled on cancelled markets

**Key Validations**:
- Only governance authority can cancel markets
- Cancellation sets market status to CANCELLED
- Resolution height recorded for cancelled markets
- Creator receives remaining liquidity refund
- All trading ceases after cancellation
- Conditional tokens become worthless (market invalid)

**Use Cases**:
- Market data compromised or manipulated
- Oracle failure (external data unavailable)
- Emergency situations requiring immediate market halt
- Bug discovered in market parameters or pricing

---

## Running the Tests

### Run All Futarchy Tests
```bash
cd test/futarchy
./market_lifecycle_test.sh
./governance_integration_test.sh
./params_update_test.sh
./liquidity_withdrawal_test.sh
./emergency_cancel_test.sh
```

### Run All Integration Tests (Including Futarchy)
```bash
cd test
./run_all_tests.sh
```

## Test Dependencies

All tests assume:
- `sparkdreamd` binary is in PATH
- Chain is running with test keyring backend
- Test accounts (alice, bob, carol) are pre-configured
- Chain ID is `sparkdream`
- Genesis includes governance module and futarchy module

## Test Patterns

All integration tests follow the same pattern used in other modules:

1. **Setup**: Initialize addresses, directories, get module addresses
2. **Step-by-step execution**: Each test has numbered steps with clear descriptions
3. **Validation**: Every step validates expected outcomes with ✅/❌ indicators
4. **Error handling**: Expected failures are tested (unauthorized actions, etc.)
5. **Cleanup**: Tests are idempotent where possible

## Event Types Tested

- `sparkdream.futarchy.v1.EventMarketCreated`
- `sparkdream.futarchy.v1.EventTrade`
- `sparkdream.futarchy.v1.EventMarketResolved`
- `sparkdream.futarchy.v1.EventRedemption`
- `sparkdream.futarchy.v1.EventLiquidityWithdrawn`
- `sparkdream.futarchy.v1.EventMarketCancelled`

## Query Types Tested

- `get-market`: Get market details by ID
- `list-market`: List all markets with pagination
- `get-market-price`: Query hypothetical price for trade amount
- `params`: Query module parameters

## Transaction Types Tested

- `create-market`: Create new prediction market
- `trade`: Buy YES or NO shares
- `redeem`: Redeem winning shares after resolution
- `withdraw-liquidity`: Withdraw unused liquidity (creator only)
- `cancel-market`: Emergency market cancellation (governance only)
- `update-params`: Update module parameters (governance only)

## Future Enhancements

Potential additional integration tests:

1. **Hook Integration**: Test `AfterMarketResolved` hook with Commons Council
2. **Elastic Tenure**: Test term adjustment based on market outcomes
3. **Multi-market**: Test multiple markets resolving simultaneously
4. **Edge Cases**: Test markets with extreme parameter values
5. **LMSR Stress**: Test numerical stability with large trades
6. **Redemption Delays**: Test time-locked redemptions
7. **Fee Distribution**: Test trading fee collection and distribution

## Notes

- Tests may take 1-3 minutes each due to block time waits
- Some tests submit governance proposals requiring 60+ second voting periods
- Tests create real transactions and consume gas fees
- Each test is independent and can run in isolation
