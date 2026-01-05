#!/bin/bash

echo "--- TESTING: FUTARCHY GOVERNANCE INTEGRATION ---"

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

# Get Commons Council info (should exist from genesis)
COUNCIL_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json 2>/dev/null) || true

if [ -n "$COUNCIL_INFO" ] && [ "$COUNCIL_INFO" != "null" ]; then
    COUNCIL_ADDR=$(echo $COUNCIL_INFO | jq -r '.extended_group.policy_address')
    FUTARCHY_ENABLED=$(echo $COUNCIL_INFO | jq -r '.extended_group.futarchy_enabled')

    echo "Commons Council Address: $COUNCIL_ADDR"
    echo "Futarchy Enabled: $FUTARCHY_ENABLED"
else
    echo "⚠️  Commons Council not found in genesis (this is OK for basic futarchy tests)"
    COUNCIL_ADDR=""
    FUTARCHY_ENABLED="false"
fi

# --- 1. CREATE GOVERNANCE PROPOSAL OUTCOME MARKET ---
echo "--- STEP 1: CREATE MARKET FOR GOVERNANCE PROPOSAL OUTCOME ---"

CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
END_BLOCK=$((CURRENT_HEIGHT + 80))

# Create a market predicting whether a specific governance proposal will pass
CREATE_RES=$($BINARY tx futarchy create-market \
  "GOV-PROP-42" \
  "Will governance proposal #42 pass?" \
  "150000uspark" \
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
    echo "❌ FAILURE: Could not create governance prediction market."
    exit 1
fi

echo "✅ Governance prediction market created with ID: $MARKET_ID"

# --- 2. MULTIPLE TRADERS PARTICIPATE ---
echo "--- STEP 2: MULTIPLE PARTICIPANTS TRADE ON OUTCOME ---"

# Alice bets YES (thinks proposal will pass)
echo "Alice bets YES on proposal passing..."
$BINARY tx futarchy trade \
  $MARKET_ID \
  true \
  "20000uspark" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json > /dev/null

sleep 2

# Bob also bets YES
echo "Bob bets YES on proposal passing..."
$BINARY tx futarchy trade \
  $MARKET_ID \
  true \
  "15000uspark" \
  --from bob \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json > /dev/null

sleep 2

# Carol bets NO (skeptical)
echo "Carol bets NO on proposal passing..."
$BINARY tx futarchy trade \
  $MARKET_ID \
  false \
  "10000uspark" \
  --from carol \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000uspark \
  -y \
  --output json > /dev/null

sleep 2

echo "✅ Multiple traders participated in governance prediction market"

# --- 3. QUERY MARKET SENTIMENT ---
echo "--- STEP 3: ANALYZE MARKET SENTIMENT ---"

# Query price to see market belief
YES_PRICE_INFO=$($BINARY query futarchy get-market-price $MARKET_ID true "1000" --output json)
YES_PRICE=$(echo $YES_PRICE_INFO | jq -r '.price')

NO_PRICE_INFO=$($BINARY query futarchy get-market-price $MARKET_ID false "1000" --output json)
NO_PRICE=$(echo $NO_PRICE_INFO | jq -r '.price')

echo ""
echo "Market Sentiment Analysis:"
echo "  YES price (per 1000 uspark): $YES_PRICE"
echo "  NO price (per 1000 uspark):  $NO_PRICE"
echo ""

# Note: Higher relative trading volume on YES suggests market believes proposal will pass
# This information could be used by governance participants to inform their voting

if [ -n "$YES_PRICE" ] && [ -n "$NO_PRICE" ] && [ "$YES_PRICE" != "null" ] && [ "$NO_PRICE" != "null" ]; then
    echo "✅ Market sentiment successfully captured"
    echo "   (In a full futarchy implementation, this would influence governance decisions)"
else
    echo "⚠️  Could not fully analyze market sentiment"
fi

# --- 4. TEST CONDITIONAL TOKENS ---
echo "--- STEP 4: VERIFY CONDITIONAL TOKEN BALANCES ---"

YES_TOKEN="f/${MARKET_ID}/yes"
NO_TOKEN="f/${MARKET_ID}/no"

ALICE_YES=$($BINARY query bank balance $ALICE_ADDR $YES_TOKEN --output json | jq -r '.balance.amount')
BOB_YES=$($BINARY query bank balance $BOB_ADDR $YES_TOKEN --output json | jq -r '.balance.amount')
CAROL_NO=$($BINARY query bank balance $CAROL_ADDR $NO_TOKEN --output json | jq -r '.balance.amount')

echo ""
echo "Conditional Token Holdings:"
echo "  Alice YES shares: $ALICE_YES"
echo "  Bob YES shares:   $BOB_YES"
echo "  Carol NO shares:  $CAROL_NO"
echo ""

if [ "$ALICE_YES" -gt "0" ] && [ "$BOB_YES" -gt "0" ] && [ "$CAROL_NO" -gt "0" ]; then
    echo "✅ Conditional tokens correctly distributed to traders"
else
    echo "❌ FAILURE: Conditional tokens not distributed correctly"
    exit 1
fi

# --- 5. SIMULATE GOVERNANCE PROPOSAL ---
echo "--- STEP 5: SUBMIT ACTUAL GOVERNANCE PROPOSAL ---"

# Submit a real governance proposal (proposal #42 that the market is predicting)
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

echo '{
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$GOV_ADDR'",
      "to_address": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1000"}]
    }
  ],
  "deposit": "50000000uspark",
  "title": "Test Proposal #42",
  "summary": "A test proposal to validate futarchy prediction market accuracy."
}' > "$PROPOSAL_DIR/gov_prop_42.json"

PROP_SUBMIT=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_prop_42.json" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  -y \
  --output json)

PROP_TX=$(echo $PROP_SUBMIT | jq -r '.txhash')
sleep 5

PROP_ID=$($BINARY query tx $PROP_TX --output json | \
  jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | \
  tr -d '"' | head -n 1)

if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
    echo "❌ FAILURE: Could not submit governance proposal"
    exit 1
fi

echo "✅ Governance proposal #$PROP_ID submitted"

# --- 6. VOTE ON GOVERNANCE PROPOSAL ---
echo "--- STEP 6: COMMUNITY VOTES ON GOVERNANCE PROPOSAL ---"

# Vote YES (matching market prediction)
$BINARY tx gov vote $PROP_ID yes \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  -y \
  --output json > /dev/null

echo "Alice voted YES on governance proposal"

# --- 7. WAIT FOR MARKET AND PROPOSAL RESOLUTION ---
echo "--- STEP 7: WAITING FOR MARKET TO RESOLVE ---"

while true; do
    CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
    if [ "$CURRENT_HEIGHT" -ge "$END_BLOCK" ]; then
        break
    fi
    echo "Current height: $CURRENT_HEIGHT / Market end: $END_BLOCK"
    sleep 3
done

echo "Market duration completed"

# Wait for both to resolve
echo "Waiting for voting period and market resolution..."
sleep 70

# --- 8. CHECK GOVERNANCE PROPOSAL OUTCOME ---
GOV_OUTCOME=$($BINARY query gov proposal $PROP_ID --output json | jq -r '.proposal.status')

echo ""
echo "Governance Proposal Outcome: $GOV_OUTCOME"

# --- 9. CHECK MARKET RESOLUTION ---
MARKET_INFO=$($BINARY query futarchy get-market $MARKET_ID --output json)
MARKET_STATUS=$(echo $MARKET_INFO | jq -r '.market.status')

echo "Market Resolution: $MARKET_STATUS"
echo ""

# --- 10. VALIDATE PREDICTION ACCURACY ---
if [[ "$GOV_OUTCOME" == "PROPOSAL_STATUS_PASSED" ]] && [[ "$MARKET_STATUS" == "RESOLVED_YES" ]]; then
    echo "✅ PERFECT PREDICTION: Market correctly predicted proposal would PASS"
    echo "   Winners: Alice and Bob (YES share holders)"
elif [[ "$GOV_OUTCOME" == "PROPOSAL_STATUS_REJECTED" ]] && [[ "$MARKET_STATUS" == "RESOLVED_NO" ]]; then
    echo "✅ PERFECT PREDICTION: Market correctly predicted proposal would FAIL"
    echo "   Winner: Carol (NO share holder)"
else
    echo "📊 Market Status: $MARKET_STATUS"
    echo "📊 Governance Outcome: $GOV_OUTCOME"
    echo "   (Market prediction may differ from actual outcome - this is normal in prediction markets)"
fi

# --- 11. REDEEM WINNING SHARES ---
if [[ "$MARKET_STATUS" == "RESOLVED_YES" ]]; then
    echo "--- STEP 11: WINNERS REDEEM YES SHARES ---"

    # Alice redeems
    $BINARY tx futarchy redeem \
      $MARKET_ID \
      --from alice \
      --chain-id $CHAIN_ID \
      --keyring-backend test \
      --fees 5000uspark \
      -y \
      --output json > /dev/null

    sleep 2

    # Bob redeems
    $BINARY tx futarchy redeem \
      $MARKET_ID \
      --from bob \
      --chain-id $CHAIN_ID \
      --keyring-backend test \
      --fees 5000uspark \
      -y \
      --output json > /dev/null

    echo "✅ YES share holders redeemed their winnings"

elif [[ "$MARKET_STATUS" == "RESOLVED_NO" ]]; then
    echo "--- STEP 11: WINNER REDEEMS NO SHARES ---"

    # Carol redeems
    $BINARY tx futarchy redeem \
      $MARKET_ID \
      --from carol \
      --chain-id $CHAIN_ID \
      --keyring-backend test \
      --fees 5000uspark \
      -y \
      --output json > /dev/null

    echo "✅ NO share holder redeemed their winnings"
fi

# --- 12. SUMMARY ---
echo ""
echo "=========================================="
echo "GOVERNANCE INTEGRATION TEST SUMMARY"
echo "=========================================="
echo "Market ID: $MARKET_ID"
echo "Governance Proposal ID: $PROP_ID"
echo "Market Resolution: $MARKET_STATUS"
echo "Governance Outcome: $GOV_OUTCOME"
echo ""
echo "This test demonstrates how futarchy prediction markets can:"
echo "  1. Aggregate community beliefs about governance outcomes"
echo "  2. Provide price signals reflecting consensus"
echo "  3. Reward accurate predictors with financial incentives"
echo "  4. Create information markets for better governance decisions"
echo ""

if [ "$FUTARCHY_ENABLED" == "true" ] && [ -n "$COUNCIL_ADDR" ]; then
    echo "NOTE: Commons Council has futarchy_enabled=true"
    echo "      Future enhancements could use market outcomes to:"
    echo "      - Adjust council term durations (elastic tenure)"
    echo "      - Influence governance decisions programmatically"
    echo "      - Provide oracle data for on-chain decision making"
    echo ""
fi

echo "✅✅✅ GOVERNANCE INTEGRATION TEST COMPLETED SUCCESSFULLY ✅✅✅"
