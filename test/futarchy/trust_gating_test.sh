#!/bin/bash

echo "--- TESTING: FUTARCHY TRUST-LEVEL GATING (FUTARCHY-6) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
KEYRING="test"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend $KEYRING)
echo "Alice Address: $ALICE_ADDR"

# Create a fresh key with no x/rep member profile (trust level = none).
OUTSIDER_KEY="futarchy_outsider"
if ! $BINARY keys show "$OUTSIDER_KEY" --keyring-backend $KEYRING &>/dev/null; then
    echo "Creating fresh non-member key: $OUTSIDER_KEY"
    $BINARY keys add "$OUTSIDER_KEY" --keyring-backend $KEYRING >/dev/null 2>&1
fi
OUTSIDER_ADDR=$($BINARY keys show "$OUTSIDER_KEY" -a --keyring-backend $KEYRING)
echo "Outsider Address: $OUTSIDER_ADDR"

# Confirm this account is NOT a rep member (no trust level).
MEMBER_CHECK=$($BINARY query rep get-member "$OUTSIDER_ADDR" --output json 2>/dev/null || echo '{}')
IS_MEMBER=$(echo "$MEMBER_CHECK" | jq -r '.member.address // empty' 2>/dev/null)
if [ -n "$IS_MEMBER" ]; then
    echo "❌ SETUP ERROR: $OUTSIDER_KEY is unexpectedly an x/rep member."
    echo "   This test requires a non-member account. Remove the key and re-run."
    exit 1
fi

# Fund the outsider with enough SPARK to cover liquidity + fees.
MIN_LIQ=$($BINARY query futarchy params --output json | jq -r '.params.min_liquidity')
if [ -z "$MIN_LIQ" ] || [ "$MIN_LIQ" == "null" ]; then
    MIN_LIQ="100000"
fi
FUND_AMOUNT=$((MIN_LIQ * 2 + 1000000))

echo "Funding $OUTSIDER_KEY with ${FUND_AMOUNT}uspark from alice..."
FUND_RES=$($BINARY tx bank send alice "$OUTSIDER_ADDR" "${FUND_AMOUNT}uspark" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend $KEYRING \
    --fees 5000uspark \
    -y \
    --output json)
sleep 3

FUND_CODE=$(echo "$FUND_RES" | jq -r '.code // "1"')
if [ "$FUND_CODE" != "0" ]; then
    echo "❌ SETUP ERROR: Failed to fund outsider: $(echo "$FUND_RES" | jq -r '.raw_log // .')"
    exit 1
fi

OUTSIDER_BAL=$($BINARY query bank balance "$OUTSIDER_ADDR" uspark --output json | jq -r '.balance.amount // "0"')
echo "✅ Outsider funded. Balance: ${OUTSIDER_BAL}uspark"

# --- 1. ATTEMPT MARKET CREATION AS NON-MEMBER ---
echo ""
echo "--- STEP 1: ATTEMPT create-market WITHOUT ESTABLISHED+ TRUST LEVEL ---"

CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
END_BLOCK=$((CURRENT_HEIGHT + 100))

CREATE_RES=$($BINARY tx futarchy create-market \
    "TRUST-GATE" \
    "$MIN_LIQ" \
    "Should a non-member be allowed to create a market?" \
    $END_BLOCK \
    --from "$OUTSIDER_KEY" \
    --chain-id $CHAIN_ID \
    --keyring-backend $KEYRING \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TX_HASH=$(echo "$CREATE_RES" | jq -r '.txhash // empty')
BROADCAST_CODE=$(echo "$CREATE_RES" | jq -r '.code // "0"')

# Failures can surface at broadcast time (CheckTx) or at inclusion time (DeliverTx).
if [ "$BROADCAST_CODE" != "0" ] && [ -n "$BROADCAST_CODE" ]; then
    # CheckTx rejection — check the broadcast response directly.
    RAW_LOG=$(echo "$CREATE_RES" | jq -r '.raw_log // .')
    FINAL_CODE="$BROADCAST_CODE"
elif [ -n "$TX_HASH" ] && [ "$TX_HASH" != "null" ]; then
    sleep 3
    TX_RESULT=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
    FINAL_CODE=$(echo "$TX_RESULT" | jq -r '.code // "0"')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // .')
else
    echo "❌ FAILURE: Could not parse broadcast response:"
    echo "$CREATE_RES" | head -c 500
    exit 1
fi

if [ "$FINAL_CODE" == "0" ]; then
    echo "❌ FAILURE: Non-member was allowed to create a market (tx succeeded)."
    echo "   Expected rejection with ESTABLISHED+ trust requirement."
    exit 1
fi

# Match either the trust-level gate message or the not-a-member fallback.
if echo "$RAW_LOG" | grep -qiE "ESTABLISHED|active member|unauthorized"; then
    echo "✅ create-market correctly rejected for non-member (code: $FINAL_CODE)"
    echo "   raw_log: $(echo "$RAW_LOG" | head -c 200)"
else
    echo "❌ FAILURE: Rejected but error message is unexpected."
    echo "   raw_log: $RAW_LOG"
    exit 1
fi

# --- 2. CONTROL CHECK: ALICE (CORE) STILL SUCCEEDS ---
echo ""
echo "--- STEP 2: CONTROL — create-market FROM ALICE (CORE) SHOULD SUCCEED ---"

CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
END_BLOCK=$((CURRENT_HEIGHT + 100))

CONTROL_RES=$($BINARY tx futarchy create-market \
    "TRUST-GATE-OK" \
    "$MIN_LIQ" \
    "Alice should still be able to create markets" \
    $END_BLOCK \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend $KEYRING \
    --fees 5000uspark \
    -y \
    --output json)

sleep 3
CONTROL_TX=$(echo "$CONTROL_RES" | jq -r '.txhash')
CONTROL_TX_RESULT=$($BINARY query tx "$CONTROL_TX" --output json 2>/dev/null)
CONTROL_CODE=$(echo "$CONTROL_TX_RESULT" | jq -r '.code // "1"')

if [ "$CONTROL_CODE" != "0" ]; then
    echo "❌ FAILURE: Alice's control create-market unexpectedly failed (code: $CONTROL_CODE)"
    echo "   raw_log: $(echo "$CONTROL_TX_RESULT" | jq -r '.raw_log')"
    exit 1
fi
echo "✅ Alice successfully created control market (trust gate allows ESTABLISHED+)"

echo ""
echo "============================================================================"
echo "  FUTARCHY TRUST-LEVEL GATING TEST PASSED"
echo "============================================================================"
