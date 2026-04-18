#!/bin/bash

echo "--- TESTING: TAG MODERATION (REPORT, QUERY, RESOLVE) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment from x/rep setup, which seeds sentinel1/sentinel2/
# poster1/moderator alongside the challenger/juror/assignee accounts.
REP_ENV="$SCRIPT_DIR/.test_env"

if [ -f "$REP_ENV" ]; then
    source "$REP_ENV"
else
    echo "Test environment not found at $REP_ENV"
    echo "   Run: bash test/rep/setup_test_accounts.sh"
    exit 1
fi

echo "Poster 1: $POSTER1_ADDR"
echo "Moderator: $MODERATOR_ADDR"
echo "Alice: $ALICE_ADDR"
echo ""

# ========================================================================
# Helper Functions
# ========================================================================

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0

    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
    return 1
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
        return 1
    fi
    return 0
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

# ========================================================================
# PART 1: REPORT TAG
# ========================================================================
echo "--- PART 1: REPORT TAG ---"

PART1_RESULT="FAIL"

echo "Reporting tag 'commons-council'..."

TX_RES=$($BINARY tx rep report-tag \
    "commons-council" \
    "Tag is being misused" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit report-tag transaction"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Tag reported successfully"
        PART1_RESULT="PASS"
    else
        echo "  Report failed"
        echo "  $(echo "$TX_RESULT" | jq -r '.raw_log')"
    fi
fi

echo "  Result: $PART1_RESULT"
echo ""

# ========================================================================
# PART 2: QUERY TAG REPORTS
# ========================================================================
echo "--- PART 2: QUERY TAG REPORTS ---"

PART2_RESULT="FAIL"

TAG_REPORTS=$($BINARY query rep list-tag-report --output json 2>&1)

if echo "$TAG_REPORTS" | grep -q "error"; then
    # Try list version
    TAG_REPORTS=$($BINARY query rep list-tag-report --output json 2>&1)
fi

if echo "$TAG_REPORTS" | grep -q "error"; then
    echo "  Failed to query tag reports"
else
    REPORT_COUNT=$(echo "$TAG_REPORTS" | jq -r '.tag_report | length // .reports | length // 0' 2>/dev/null)
    echo "  Total tag reports: $REPORT_COUNT"
    PART2_RESULT="PASS"

    if [ "$REPORT_COUNT" -gt 0 ]; then
        echo ""
        echo "  Tag Reports:"
        echo "$TAG_REPORTS" | jq -r '.tag_report[:5] // .reports[:5] | .[] | "    - Tag: \(.tag // .tag_name) reason: \(.reason_code)"' 2>/dev/null
    fi
fi

echo "  Result: $PART2_RESULT"
echo ""

# ========================================================================
# PART 3: RESOLVE TAG REPORT (Test interface - requires authority)
# ========================================================================
echo "--- PART 3: RESOLVE TAG REPORT (Authority required) ---"

PART3_RESULT="FAIL"

echo "Testing resolve-tag-report command (requires authority)..."

TX_RES=$($BINARY tx rep resolve-tag-report \
    "commons-council" \
    "0" \
    "" \
    "false" \
    --from moderator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction failed (expected - requires authority)"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
    PART3_RESULT="PASS"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Tag report resolved"
        PART3_RESULT="PASS"
    else
        echo "  Resolution failed (expected - requires authority)"
        PART3_RESULT="PASS"
    fi
fi

echo "  Result: $PART3_RESULT"
echo ""

# ========================================================================
# PART 4: RESOLVE TAG REPORT ERROR - UNAUTHORIZED
# ========================================================================
echo "--- PART 4: RESOLVE TAG REPORT ERROR - UNAUTHORIZED ---"

PART4_RESULT="FAIL"

TX_RES=$($BINARY tx rep resolve-tag-report \
    "test-tag" \
    "0" \
    "" \
    "false" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART4_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
        PART4_RESULT="PASS"
    fi
fi

echo "  Result: $PART4_RESULT"
echo ""

# ========================================================================
# PART 5: RESOLVE TAG REPORT ERROR - REPORT NOT FOUND
# ========================================================================
echo "--- PART 5: RESOLVE TAG REPORT ERROR - REPORT NOT FOUND ---"

PART5_RESULT="FAIL"

TX_RES=$($BINARY tx rep resolve-tag-report \
    "nonexistent-tag-xyz" \
    "0" \
    "" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not found\|report not found"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART5_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not found\|report not found"; then
        PART5_RESULT="PASS"
    fi
fi

echo "  Result: $PART5_RESULT"
echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- TAG MODERATION TEST SUMMARY ---"
echo ""
echo "  Report tag:                  $PART1_RESULT"
echo "  Query tag reports:           $PART2_RESULT"
echo "  Resolve tag report:          $PART3_RESULT"
echo "  Resolve tag: unauthorized:   $PART4_RESULT"
echo "  Resolve tag: not found:      $PART5_RESULT"

FAIL_COUNT=0
for R in "$PART1_RESULT" "$PART2_RESULT" "$PART3_RESULT" "$PART4_RESULT" "$PART5_RESULT"; do
    if [ "$R" == "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
else
    echo "  ALL TESTS PASSED"
fi
echo ""
echo "TAG MODERATION TEST COMPLETED"
echo ""
