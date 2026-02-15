#!/bin/bash

echo "--- TESTING: LIQUIDITY WITHDRAWAL ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo "Alice Address: $ALICE_ADDR"
echo "Bob Address:   $BOB_ADDR"

# Track test results
PASSED=0
FAILED=0
pass() { echo "  PASS: $1"; PASSED=$((PASSED + 1)); }
fail() { echo "  FAIL: $1"; FAILED=$((FAILED + 1)); }

# Helper: wait for tx and check result
wait_for_tx() {
    local TX_HASH="$1"
    local LABEL="$2"
    sleep 3
    local TX_RESULT=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
    local TX_CODE=$(echo "$TX_RESULT" | jq -r '.code' 2>/dev/null)
    if [ "$TX_CODE" == "0" ]; then
        echo "$TX_RESULT"
        return 0
    else
        local TX_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)
        echo "  Transaction failed (code: $TX_CODE): $TX_LOG" >&2
        echo "$TX_RESULT"
        return 1
    fi
}

# Helper: submit tx and expect failure
expect_tx_failure() {
    local TX_RES="$1"
    local EXPECTED_MSG="$2"
    local LABEL="$3"
    local TX_HASH=$(echo "$TX_RES" | jq -r '.txhash' 2>/dev/null)
    if [ -z "$TX_HASH" ] || [ "$TX_HASH" == "null" ]; then
        # TX was rejected at CLI level - check stderr
        if echo "$TX_RES" | grep -q "$EXPECTED_MSG"; then
            pass "$LABEL"
        else
            fail "$LABEL (no txhash, unexpected error: $TX_RES)"
        fi
        return
    fi
    sleep 3
    local TX_RESULT=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
    local TX_CODE=$(echo "$TX_RESULT" | jq -r '.code' 2>/dev/null)
    local TX_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)
    if [ "$TX_CODE" != "0" ] && echo "$TX_LOG" | grep -q "$EXPECTED_MSG"; then
        pass "$LABEL"
    elif [ "$TX_CODE" == "0" ]; then
        fail "$LABEL (tx succeeded but should have failed)"
    else
        fail "$LABEL (code=$TX_CODE, log=$TX_LOG, expected: $EXPECTED_MSG)"
    fi
}

# --- PART 1: CREATE MARKET ---
echo ""
echo "--- PART 1: CREATE MARKET WITH LIQUIDITY ---"

CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
END_BLOCK=$((CURRENT_HEIGHT + 50))

# Get current min_liquidity (in case params were updated)
MIN_LIQ=$($BINARY query futarchy params -o json | jq -r '.params.min_liquidity')
if [ -z "$MIN_LIQ" ] || [ "$MIN_LIQ" == "null" ]; then
    MIN_LIQ="200000"
fi
# Use at least 200000 for this test
if [ "$MIN_LIQ" -lt "200000" ]; then
    MIN_LIQ="200000"
fi

ALICE_INITIAL_BALANCE=$($BINARY query bank balance $ALICE_ADDR uspark --output json | jq -r '.balance.amount')
echo "  Alice initial balance: $ALICE_INITIAL_BALANCE uspark"

CREATE_RES=$($BINARY tx futarchy create-market \
  "WITHDRAW-TEST" \
  "$MIN_LIQ" \
  "Market for testing liquidity withdrawal" \
  $END_BLOCK \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json)

TX_HASH=$(echo $CREATE_RES | jq -r '.txhash')
sleep 3

MARKET_ID=$($BINARY query tx $TX_HASH --output json | \
  jq -r '.events[] | select(.type=="market_created") | .attributes[] | select(.key=="market_id") | .value' | \
  tr -d '"')

if [ -z "$MARKET_ID" ] || [ "$MARKET_ID" == "null" ]; then
    fail "Create market"
    echo "  Cannot continue without a market. Exiting."
    exit 1
fi

pass "Create market (ID: $MARKET_ID, liquidity: $MIN_LIQ, end_block: $END_BLOCK)"

# --- PART 2: BOB TRADES ON MARKET ---
echo ""
echo "--- PART 2: BOB TRADES ON THE MARKET ---"

TRADE_RES=$($BINARY tx futarchy trade \
  $MARKET_ID \
  true \
  "15000" \
  --from bob \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json)

TRADE_TX_HASH=$(echo $TRADE_RES | jq -r '.txhash')
if TRADE_RESULT=$(wait_for_tx "$TRADE_TX_HASH" "Bob trade"); then
    pass "Bob purchased YES shares with 15000 uspark"
else
    fail "Bob trade failed"
fi

# --- PART 3: EARLY WITHDRAWAL (SHOULD FAIL - MARKET NOT RESOLVED) ---
echo ""
echo "--- PART 3: EARLY WITHDRAWAL (SHOULD FAIL) ---"

EARLY_WITHDRAW=$($BINARY tx futarchy withdraw-liquidity \
  --market-id $MARKET_ID \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json 2>&1) || true

expect_tx_failure "$EARLY_WITHDRAW" "must be resolved" "Early withdrawal rejected (market not resolved)"

# Verify market is still ACTIVE
MARKET_INFO=$($BINARY query futarchy get-market $MARKET_ID --output json)
MARKET_STATUS=$(echo $MARKET_INFO | jq -r '.market.status')

if [ "$MARKET_STATUS" == "ACTIVE" ]; then
    pass "Market remains ACTIVE after early withdrawal attempt"
else
    fail "Market status should be ACTIVE, got: $MARKET_STATUS"
fi

# --- PART 4: WAIT FOR MARKET TO RESOLVE ---
echo ""
echo "--- PART 4: WAITING FOR MARKET TO RESOLVE ---"

while true; do
    CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
    if [ "$CURRENT_HEIGHT" -ge "$END_BLOCK" ]; then
        break
    fi
    echo "  Current height: $CURRENT_HEIGHT / End block: $END_BLOCK"
    sleep 3
done

# Wait for EndBlocker to process
sleep 10

MARKET_INFO=$($BINARY query futarchy get-market $MARKET_ID --output json)
MARKET_STATUS=$(echo $MARKET_INFO | jq -r '.market.status')

if echo "$MARKET_STATUS" | grep -q "RESOLVED"; then
    pass "Market resolved with status: $MARKET_STATUS"
else
    fail "Market not resolved (status: $MARKET_STATUS)"
    echo "  Cannot continue without resolved market. Exiting."
    exit 1
fi

# --- PART 5: NON-CREATOR WITHDRAWAL (SHOULD FAIL) ---
echo ""
echo "--- PART 5: BOB ATTEMPTS WITHDRAWAL (SHOULD FAIL) ---"

BOB_WITHDRAW=$($BINARY tx futarchy withdraw-liquidity \
  --market-id $MARKET_ID \
  --from bob \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json 2>&1) || true

expect_tx_failure "$BOB_WITHDRAW" "only market creator can withdraw" "Non-creator withdrawal rejected"

# --- PART 6: ALICE WITHDRAWS LIQUIDITY ---
echo ""
echo "--- PART 6: ALICE (CREATOR) WITHDRAWS LIQUIDITY ---"

ALICE_BALANCE_BEFORE=$($BINARY query bank balance $ALICE_ADDR uspark --output json | jq -r '.balance.amount')
echo "  Alice balance before withdrawal: $ALICE_BALANCE_BEFORE uspark"

WITHDRAW_RES=$($BINARY tx futarchy withdraw-liquidity \
  --market-id $MARKET_ID \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json)

WITHDRAW_TX_HASH=$(echo $WITHDRAW_RES | jq -r '.txhash')
if WITHDRAW_RESULT=$(wait_for_tx "$WITHDRAW_TX_HASH" "Alice withdraw"); then
    # Check for liquidity_withdrawn event
    WITHDRAW_AMOUNT=$(echo "$WITHDRAW_RESULT" | \
      jq -r '.events[] | select(.type=="liquidity_withdrawn") | .attributes[] | select(.key=="amount") | .value')
    if [ -n "$WITHDRAW_AMOUNT" ] && [ "$WITHDRAW_AMOUNT" != "null" ]; then
        pass "Alice withdrew liquidity (amount: $WITHDRAW_AMOUNT)"
    else
        pass "Alice withdrawal tx succeeded"
    fi
else
    fail "Alice withdrawal failed"
fi

# --- PART 7: VERIFY MARKET STATE UPDATED ---
echo ""
echo "--- PART 7: VERIFY MARKET STATE UPDATED ---"

FINAL_MARKET_INFO=$($BINARY query futarchy get-market $MARKET_ID --output json)
LIQUIDITY_WITHDRAWN=$(echo $FINAL_MARKET_INFO | jq -r '.market.liquidity_withdrawn')

if [ -n "$LIQUIDITY_WITHDRAWN" ] && [ "$LIQUIDITY_WITHDRAWN" != "null" ] && [ "$LIQUIDITY_WITHDRAWN" != "0" ]; then
    pass "liquidity_withdrawn updated: $LIQUIDITY_WITHDRAWN"
else
    fail "liquidity_withdrawn not updated (got: $LIQUIDITY_WITHDRAWN)"
fi

# --- PART 8: VERIFY ALICE BALANCE INCREASED ---
echo ""
echo "--- PART 8: VERIFY ALICE BALANCE INCREASED ---"

ALICE_BALANCE_AFTER=$($BINARY query bank balance $ALICE_ADDR uspark --output json | jq -r '.balance.amount')

echo "  Alice balance before: $ALICE_BALANCE_BEFORE uspark"
echo "  Alice balance after:  $ALICE_BALANCE_AFTER uspark"

if [ "$ALICE_BALANCE_AFTER" -gt "$ALICE_BALANCE_BEFORE" ]; then
    RECOVERED=$((ALICE_BALANCE_AFTER - ALICE_BALANCE_BEFORE))
    pass "Alice recovered $RECOVERED uspark"
else
    fail "Alice balance did not increase (before=$ALICE_BALANCE_BEFORE, after=$ALICE_BALANCE_AFTER)"
fi

# --- PART 9: SECOND WITHDRAWAL (SHOULD FAIL - NO LIQUIDITY) ---
echo ""
echo "--- PART 9: SECOND WITHDRAWAL (SHOULD FAIL) ---"

SECOND_WITHDRAW=$($BINARY tx futarchy withdraw-liquidity \
  --market-id $MARKET_ID \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json 2>&1) || true

expect_tx_failure "$SECOND_WITHDRAW" "No liquidity available" "Second withdrawal rejected (no liquidity)"

# --- SUMMARY ---
echo ""
echo "--- LIQUIDITY WITHDRAWAL TEST SUMMARY ---"
echo ""
echo "  Create market:                    $([ $PASSED -ge 1 ] && echo 'PASS' || echo 'FAIL')"
echo "  Bob trades:                       $([ $PASSED -ge 2 ] && echo 'PASS' || echo 'FAIL')"
echo "  Early withdrawal rejected:        $([ $PASSED -ge 3 ] && echo 'PASS' || echo 'FAIL')"
echo "  Market remains ACTIVE:            $([ $PASSED -ge 4 ] && echo 'PASS' || echo 'FAIL')"
echo "  Market resolved:                  $([ $PASSED -ge 5 ] && echo 'PASS' || echo 'FAIL')"
echo "  Non-creator rejected:             $([ $PASSED -ge 6 ] && echo 'PASS' || echo 'FAIL')"
echo "  Creator withdrawal:               $([ $PASSED -ge 7 ] && echo 'PASS' || echo 'FAIL')"
echo "  Market state updated:             $([ $PASSED -ge 8 ] && echo 'PASS' || echo 'FAIL')"
echo "  Balance increased:                $([ $PASSED -ge 9 ] && echo 'PASS' || echo 'FAIL')"
echo "  Second withdrawal rejected:       $([ $PASSED -ge 10 ] && echo 'PASS' || echo 'FAIL')"
echo ""
echo "  Total: $((PASSED + FAILED)) | Passed: $PASSED | Failed: $FAILED"
echo ""

if [ $FAILED -eq 0 ]; then
    echo "  ALL TESTS PASSED"
else
    echo "  SOME TESTS FAILED"
    exit 1
fi

echo ""
echo "LIQUIDITY WITHDRAWAL TEST COMPLETED"
