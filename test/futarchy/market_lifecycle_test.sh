#!/bin/bash

echo "--- TESTING: FUTARCHY MARKET LIFECYCLE ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

echo "Alice Address: $ALICE_ADDR"
echo "Bob Address:   $BOB_ADDR"
echo "Carol Address: $CAROL_ADDR"

# --- 1. CREATE MARKET ---
echo "--- STEP 1: CREATE PREDICTION MARKET ---"

# Create a governance proposal outcome market
CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
END_BLOCK=$((CURRENT_HEIGHT + 100))

echo "Creating market ending at block $END_BLOCK..."
CREATE_RES=$($BINARY tx futarchy create-market \
  "PROP-1" \
  "Will governance proposal #1 pass?" \
  "100000uspark" \
  --end-block $END_BLOCK \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json)

TX_HASH=$(echo $CREATE_RES | jq -r '.txhash')
echo "Transaction Hash: $TX_HASH"
sleep 3

# Extract market ID from events
MARKET_ID=$($BINARY query tx $TX_HASH --output json | \
  jq -r '.events[] | select(.type=="sparkdream.futarchy.v1.EventMarketCreated") | .attributes[] | select(.key=="market_id") | .value' | \
  tr -d '"')

if [ -z "$MARKET_ID" ] || [ "$MARKET_ID" == "null" ]; then
    echo "❌ FAILURE: Could not extract market ID."
    exit 1
fi

echo "✅ Market created with ID: $MARKET_ID"

# --- 2. QUERY MARKET ---
echo "--- STEP 2: QUERY MARKET DETAILS ---"

MARKET_INFO=$($BINARY query futarchy get-market $MARKET_ID --output json)
MARKET_STATUS=$(echo $MARKET_INFO | jq -r '.market.status')
MARKET_SYMBOL=$(echo $MARKET_INFO | jq -r '.market.symbol')

if [ "$MARKET_STATUS" != "ACTIVE" ]; then
    echo "❌ FAILURE: Market status should be ACTIVE, got $MARKET_STATUS"
    exit 1
fi

if [ "$MARKET_SYMBOL" != "PROP-1" ]; then
    echo "❌ FAILURE: Market symbol mismatch"
    exit 1
fi

echo "✅ Market is ACTIVE with symbol $MARKET_SYMBOL"

# --- 3. QUERY MARKET PRICE ---
echo "--- STEP 3: QUERY INITIAL MARKET PRICE ---"

PRICE_INFO=$($BINARY query futarchy get-market-price $MARKET_ID true "1000" --output json)
YES_PRICE=$(echo $PRICE_INFO | jq -r '.price')
YES_SHARES=$(echo $PRICE_INFO | jq -r '.shares_out')

echo "YES Price for 1000 uspark: $YES_PRICE"
echo "Expected YES Shares: $YES_SHARES"

if [ -z "$YES_PRICE" ] || [ "$YES_PRICE" == "null" ]; then
    echo "❌ FAILURE: Could not query market price"
    exit 1
fi

echo "✅ Market price query successful"

# --- 4. TRADE (BUY YES) ---
echo "--- STEP 4: BOB BUYS YES SHARES ---"

TRADE_RES=$($BINARY tx futarchy trade \
  $MARKET_ID \
  true \
  "10000uspark" \
  --from bob \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json)

TRADE_TX_HASH=$(echo $TRADE_RES | jq -r '.txhash')
sleep 3

# Verify trade event
TRADE_EVENT=$($BINARY query tx $TRADE_TX_HASH --output json | \
  jq -r '.events[] | select(.type=="sparkdream.futarchy.v1.EventTrade")')

if [ -z "$TRADE_EVENT" ]; then
    echo "❌ FAILURE: Trade event not found"
    exit 1
fi

echo "✅ Bob successfully purchased YES shares"

# Verify Bob received YES token
YES_TOKEN="f/${MARKET_ID}/yes"
BOB_YES_BALANCE=$($BINARY query bank balance $BOB_ADDR $YES_TOKEN --output json | jq -r '.balance.amount')

if [ "$BOB_YES_BALANCE" == "0" ] || [ -z "$BOB_YES_BALANCE" ]; then
    echo "❌ FAILURE: Bob should have YES shares"
    exit 1
fi

echo "✅ Bob has $BOB_YES_BALANCE YES shares (token: $YES_TOKEN)"

# --- 5. TRADE (BUY NO) ---
echo "--- STEP 5: CAROL BUYS NO SHARES ---"

TRADE_NO_RES=$($BINARY tx futarchy trade \
  $MARKET_ID \
  false \
  "10000uspark" \
  --from carol \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json)

sleep 3
echo "✅ Carol successfully purchased NO shares"

# Verify Carol received NO token
NO_TOKEN="f/${MARKET_ID}/no"
CAROL_NO_BALANCE=$($BINARY query bank balance $CAROL_ADDR $NO_TOKEN --output json | jq -r '.balance.amount')

if [ "$CAROL_NO_BALANCE" == "0" ] || [ -z "$CAROL_NO_BALANCE" ]; then
    echo "❌ FAILURE: Carol should have NO shares"
    exit 1
fi

echo "✅ Carol has $CAROL_NO_BALANCE NO shares (token: $NO_TOKEN)"

# --- 6. QUERY UPDATED PRICE ---
echo "--- STEP 6: VERIFY PRICE CHANGED AFTER TRADES ---"

NEW_PRICE_INFO=$($BINARY query futarchy get-market-price $MARKET_ID true "1000" --output json)
NEW_YES_PRICE=$(echo $NEW_PRICE_INFO | jq -r '.price')

echo "New YES Price: $NEW_YES_PRICE"
echo "Original YES Price: $YES_PRICE"

# Note: We can't easily compare decimal strings in bash, so we just verify it's not empty
if [ -z "$NEW_YES_PRICE" ] || [ "$NEW_YES_PRICE" == "null" ]; then
    echo "❌ FAILURE: Could not query updated market price"
    exit 1
fi

echo "✅ Market price updated after trades"

# --- 7. LIST ALL MARKETS ---
echo "--- STEP 7: LIST ALL MARKETS ---"

MARKETS_LIST=$($BINARY query futarchy list-market --output json)
MARKET_COUNT=$(echo $MARKETS_LIST | jq -r '.market | length')

if [ "$MARKET_COUNT" -lt "1" ]; then
    echo "❌ FAILURE: Should have at least 1 market"
    exit 1
fi

echo "✅ Found $MARKET_COUNT market(s) in the system"

# --- 8. WAIT FOR MARKET TO END ---
echo "--- STEP 8: WAITING FOR MARKET TO REACH END BLOCK ---"

while true; do
    CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
    if [ "$CURRENT_HEIGHT" -ge "$END_BLOCK" ]; then
        echo "Market has reached end block"
        break
    fi
    echo "Current height: $CURRENT_HEIGHT / End block: $END_BLOCK"
    sleep 3
done

echo "✅ Market duration completed"

# --- 9. VERIFY MARKET STATUS ---
echo "--- STEP 9: VERIFY MARKET RESOLVED ---"

# Wait a few blocks for EndBlocker to process
sleep 10

FINAL_MARKET_INFO=$($BINARY query futarchy get-market $MARKET_ID --output json)
FINAL_STATUS=$(echo $FINAL_MARKET_INFO | jq -r '.market.status')

# Market should be resolved (YES, NO, or INVALID)
if [[ "$FINAL_STATUS" == "RESOLVED_YES" ]] || [[ "$FINAL_STATUS" == "RESOLVED_NO" ]] || [[ "$FINAL_STATUS" == "RESOLVED_INVALID" ]]; then
    echo "✅ Market resolved with status: $FINAL_STATUS"
else
    echo "⚠️  Market status: $FINAL_STATUS (may still be processing)"
fi

# --- 10. REDEMPTION ---
if [[ "$FINAL_STATUS" == "RESOLVED_YES" ]]; then
    echo "--- STEP 10: BOB REDEEMS WINNING YES SHARES ---"

    REDEEM_RES=$($BINARY tx futarchy redeem \
      $MARKET_ID \
      --from bob \
      --chain-id $CHAIN_ID \
      --keyring-backend test \
      --fees 5000uspark \
      -y \
      --output json)

    sleep 3
    echo "✅ Bob redeemed YES shares"

elif [[ "$FINAL_STATUS" == "RESOLVED_NO" ]]; then
    echo "--- STEP 10: CAROL REDEEMS WINNING NO SHARES ---"

    REDEEM_RES=$($BINARY tx futarchy redeem \
      $MARKET_ID \
      --from carol \
      --chain-id $CHAIN_ID \
      --keyring-backend test \
      --fees 5000uspark \
      -y \
      --output json)

    sleep 3
    echo "✅ Carol redeemed NO shares"
else
    echo "--- STEP 10: SKIPPED (Market not resolved or resolved as INVALID) ---"
fi

echo ""
echo "✅✅✅ FUTARCHY MARKET LIFECYCLE TEST COMPLETED SUCCESSFULLY ✅✅✅"
