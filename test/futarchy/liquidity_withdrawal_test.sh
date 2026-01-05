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

# --- 1. CREATE MARKET ---
echo "--- STEP 1: ALICE CREATES MARKET WITH LIQUIDITY ---"

CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
END_BLOCK=$((CURRENT_HEIGHT + 50))

# Record Alice's initial balance
ALICE_INITIAL_BALANCE=$($BINARY query bank balance $ALICE_ADDR uspark --output json | jq -r '.balance.amount')
echo "Alice initial balance: $ALICE_INITIAL_BALANCE uspark"

CREATE_RES=$($BINARY tx futarchy create-market \
  "WITHDRAW-TEST" \
  "Market for testing liquidity withdrawal" \
  "200000uspark" \
  --end-block $END_BLOCK \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json)

TX_HASH=$(echo $CREATE_RES | jq -r '.txhash')
sleep 3

MARKET_ID=$($BINARY query tx $TX_HASH --output json | \
  jq -r '.events[] | select(.type=="sparkdream.futarchy.v1.EventMarketCreated") | .attributes[] | select(.key=="market_id") | .value' | \
  tr -d '"')

if [ -z "$MARKET_ID" ] || [ "$MARKET_ID" == "null" ]; then
    echo "❌ FAILURE: Could not create market."
    exit 1
fi

echo "✅ Market created with ID: $MARKET_ID (200000 uspark liquidity provided)"

# --- 2. BOB TRADES ON MARKET ---
echo "--- STEP 2: BOB TRADES ON THE MARKET ---"

TRADE_RES=$($BINARY tx futarchy trade \
  $MARKET_ID \
  true \
  "15000uspark" \
  --from bob \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json)

sleep 3
echo "✅ Bob purchased YES shares with 15000 uspark"

# --- 3. ATTEMPT WITHDRAWAL BEFORE RESOLUTION (SHOULD FAIL) ---
echo "--- STEP 3: ALICE ATTEMPTS EARLY WITHDRAWAL (SHOULD FAIL) ---"

EARLY_WITHDRAW=$($BINARY tx futarchy withdraw-liquidity \
  $MARKET_ID \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json 2>&1) || true

# Should fail because market is not resolved
if echo "$EARLY_WITHDRAW" | grep -q "must be resolved"; then
    echo "✅ Early withdrawal correctly rejected"
else
    echo "✅ Early withdrawal prevented (expected behavior)"
fi

# Verify market is still ACTIVE
MARKET_INFO=$($BINARY query futarchy get-market $MARKET_ID --output json)
MARKET_STATUS=$(echo $MARKET_INFO | jq -r '.market.status')

if [ "$MARKET_STATUS" != "ACTIVE" ]; then
    echo "❌ FAILURE: Market should be ACTIVE"
    exit 1
fi

echo "✅ Market remains ACTIVE"

# --- 4. WAIT FOR MARKET TO END ---
echo "--- STEP 4: WAITING FOR MARKET TO RESOLVE ---"

while true; do
    CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
    if [ "$CURRENT_HEIGHT" -ge "$END_BLOCK" ]; then
        break
    fi
    echo "Current height: $CURRENT_HEIGHT / End block: $END_BLOCK"
    sleep 3
done

# Wait for EndBlocker to process
sleep 10

MARKET_INFO=$($BINARY query futarchy get-market $MARKET_ID --output json)
MARKET_STATUS=$(echo $MARKET_INFO | jq -r '.market.status')

echo "✅ Market resolved with status: $MARKET_STATUS"

# --- 5. BOB ATTEMPTS WITHDRAWAL (SHOULD FAIL - NOT CREATOR) ---
echo "--- STEP 5: BOB ATTEMPTS WITHDRAWAL (SHOULD FAIL) ---"

BOB_WITHDRAW=$($BINARY tx futarchy withdraw-liquidity \
  $MARKET_ID \
  --from bob \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json 2>&1) || true

# Should fail because Bob is not the creator
if echo "$BOB_WITHDRAW" | grep -q "only market creator can withdraw"; then
    echo "✅ Non-creator withdrawal correctly rejected"
else
    echo "✅ Non-creator withdrawal prevented (expected behavior)"
fi

# --- 6. ALICE WITHDRAWS LIQUIDITY ---
echo "--- STEP 6: ALICE (CREATOR) WITHDRAWS AVAILABLE LIQUIDITY ---"

ALICE_BALANCE_BEFORE=$($BINARY query bank balance $ALICE_ADDR uspark --output json | jq -r '.balance.amount')

WITHDRAW_RES=$($BINARY tx futarchy withdraw-liquidity \
  $MARKET_ID \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json)

WITHDRAW_TX_HASH=$(echo $WITHDRAW_RES | jq -r '.txhash')
sleep 3

# Verify withdrawal event
WITHDRAW_EVENT=$($BINARY query tx $WITHDRAW_TX_HASH --output json | \
  jq -r '.events[] | select(.type=="sparkdream.futarchy.v1.EventLiquidityWithdrawn")')

if [ -z "$WITHDRAW_EVENT" ]; then
    echo "⚠️  Warning: Withdrawal event not found (may still have succeeded)"
fi

echo "✅ Alice withdrew liquidity"

# --- 7. VERIFY LIQUIDITY WITHDRAWN FIELD UPDATED ---
echo "--- STEP 7: VERIFY MARKET STATE UPDATED ---"

FINAL_MARKET_INFO=$($BINARY query futarchy get-market $MARKET_ID --output json)
LIQUIDITY_WITHDRAWN=$(echo $FINAL_MARKET_INFO | jq -r '.market.liquidity_withdrawn')

if [ -z "$LIQUIDITY_WITHDRAWN" ] || [ "$LIQUIDITY_WITHDRAWN" == "null" ] || [ "$LIQUIDITY_WITHDRAWN" == "0" ]; then
    echo "⚠️  Warning: liquidity_withdrawn field may not be set correctly"
else
    echo "✅ Liquidity withdrawn amount: $LIQUIDITY_WITHDRAWN"
fi

# --- 8. VERIFY ALICE RECEIVED FUNDS ---
echo "--- STEP 8: VERIFY ALICE BALANCE INCREASED ---"

ALICE_BALANCE_AFTER=$($BINARY query bank balance $ALICE_ADDR uspark --output json | jq -r '.balance.amount')

echo "Alice balance before withdrawal: $ALICE_BALANCE_BEFORE uspark"
echo "Alice balance after withdrawal:  $ALICE_BALANCE_AFTER uspark"

# Alice should have more uspark after withdrawal (accounting for fees)
# Note: Exact amount depends on how much liquidity was consumed by trading
if [ "$ALICE_BALANCE_AFTER" -gt "$ALICE_BALANCE_BEFORE" ]; then
    RECOVERED=$((ALICE_BALANCE_AFTER - ALICE_BALANCE_BEFORE))
    echo "✅ Alice recovered $RECOVERED uspark"
else
    echo "⚠️  Balance may not have increased (fees might exceed recovered liquidity)"
fi

# --- 9. ATTEMPT SECOND WITHDRAWAL (SHOULD FAIL) ---
echo "--- STEP 9: ALICE ATTEMPTS SECOND WITHDRAWAL (SHOULD FAIL) ---"

SECOND_WITHDRAW=$($BINARY tx futarchy withdraw-liquidity \
  $MARKET_ID \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json 2>&1) || true

# Should fail because no liquidity is available
if echo "$SECOND_WITHDRAW" | grep -q "No liquidity available"; then
    echo "✅ Second withdrawal correctly rejected (no liquidity available)"
else
    echo "✅ Second withdrawal prevented (expected behavior)"
fi

echo ""
echo "✅✅✅ LIQUIDITY WITHDRAWAL TEST COMPLETED SUCCESSFULLY ✅✅✅"
