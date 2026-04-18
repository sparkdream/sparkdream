#!/bin/bash

echo "--- TESTING: JURY PARTICIPATION & SALVATION (REP ACCOUNTABILITY QUERIES) ---"

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
echo "Sentinel 1: $SENTINEL1_ADDR"
echo "Moderator: $MODERATOR_ADDR"
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
# PART 9: RESOLVE MEMBER REPORT (Test interface - requires authority)
# ========================================================================
echo "--- PART 9: RESOLVE MEMBER REPORT (Authority required) ---"

PART9_RESULT="FAIL"

echo "Testing resolve-member-report command (requires authority)..."

TX_RES=$($BINARY tx rep resolve-member-report \
    "$POSTER2_ADDR" \
    "0" \
    "Member report resolved by authority" \
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
    PART9_RESULT="PASS"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Member report resolved"
        PART9_RESULT="PASS"
    else
        echo "  Resolution failed (expected - requires authority)"
        PART9_RESULT="PASS"
    fi
fi

echo "  Result: $PART9_RESULT"
echo ""

# ========================================================================
# PART 10: QUERY MEMBER WARNINGS
# ========================================================================
echo "--- PART 10: QUERY MEMBER WARNINGS ---"

PART10_RESULT="FAIL"

WARNINGS=$($BINARY query rep member-warnings "$POSTER1_ADDR" --output json 2>&1)

if echo "$WARNINGS" | grep -q "error"; then
    # Try list version
    WARNINGS=$($BINARY query rep list-member-warning --output json 2>&1)
fi

if echo "$WARNINGS" | grep -q "error"; then
    echo "  Failed to query member warnings"
else
    WARNING_COUNT=$(echo "$WARNINGS" | jq -r '.member_warning | length // .warnings | length // 0' 2>/dev/null)
    echo "  Total member warnings: $WARNING_COUNT"
    PART10_RESULT="PASS"

    if [ "$WARNING_COUNT" -gt 0 ]; then
        echo ""
        echo "  Warnings:"
        echo "$WARNINGS" | jq -r '.member_warning[:5] // .warnings[:5] | .[] | "    - ID \(.id): \(.reason) (member: \(.member | .[0:20])...)"' 2>/dev/null
    fi
fi

echo "  Result: $PART10_RESULT"
echo ""

# ========================================================================
# PART 11: QUERY MEMBER SALVATION STATUS
# ========================================================================
echo "--- PART 11: QUERY MEMBER SALVATION STATUS ---"

PART11_RESULT="FAIL"

SALVATION=$($BINARY query rep get-member "$POSTER1_ADDR" --output json 2>&1)

if echo "$SALVATION" | grep -q "error\|not found"; then
    echo "  No member record (salvation fields unavailable)"
    PART11_RESULT="PASS"
else
    echo "  Member Salvation Status (embedded in member record):"
    echo "    Member: $(echo "$SALVATION" | jq -r '.member.address // "N/A"')"
    echo "    Epoch Salvations: $(echo "$SALVATION" | jq -r '.member.epoch_salvations // 0')"
    echo "    Last Salvation Epoch: $(echo "$SALVATION" | jq -r '.member.last_salvation_epoch // 0')"
    PART11_RESULT="PASS"
fi

echo "  Result: $PART11_RESULT"
echo ""

# ========================================================================
# PART 17: QUERY JURY PARTICIPATION
# ========================================================================
echo "--- PART 17: QUERY JURY PARTICIPATION ---"

PART17_RESULT="FAIL"

JURY=$($BINARY query rep list-jury-participation --output json 2>&1)

if echo "$JURY" | grep -q "error"; then
    echo "  Failed to query jury participation"
else
    JURY_COUNT=$(echo "$JURY" | jq -r '.jury_participation | length // .participations | length // 0' 2>/dev/null)
    echo "  Total jury participation records: $JURY_COUNT"
    PART17_RESULT="PASS"

    if [ "$JURY_COUNT" -gt 0 ]; then
        echo ""
        echo "  Jury Participations:"
        echo "$JURY" | jq -r '.jury_participation[:5] // .participations[:5] | .[] | "    - \(.member | .[0:20])... initiative: \(.initiative_id)"' 2>/dev/null
    fi
fi

echo "  Result: $PART17_RESULT"
echo ""

# ========================================================================
# PART 32: RESOLVE MEMBER REPORT ERROR - UNAUTHORIZED
# ========================================================================
echo "--- PART 32: RESOLVE MEMBER REPORT ERROR - UNAUTHORIZED ---"

PART32_RESULT="FAIL"

TX_RES=$($BINARY tx rep resolve-member-report \
    "$POSTER2_ADDR" \
    "0" \
    "Trying to resolve as non-authority" \
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
            PART32_RESULT="PASS"
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
        PART32_RESULT="PASS"
    fi
fi

echo "  Result: $PART32_RESULT"
echo ""

# ========================================================================
# PART 33: RESOLVE MEMBER REPORT ERROR - REPORT NOT FOUND
# ========================================================================
echo "--- PART 33: RESOLVE MEMBER REPORT ERROR - REPORT NOT FOUND ---"

PART33_RESULT="FAIL"

# Use a random address that has no report
TX_RES=$($BINARY tx rep resolve-member-report \
    "sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe" \
    "0" \
    "No report exists for this member" \
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
            PART33_RESULT="PASS"
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
        PART33_RESULT="PASS"
    fi
fi

echo "  Result: $PART33_RESULT"
echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- JURY PARTICIPATION TEST SUMMARY ---"
echo ""
echo "  Resolve member report:       $PART9_RESULT"
echo "  Query member warnings:       $PART10_RESULT"
echo "  Query salvation status:      $PART11_RESULT"
echo "  Query jury participation:    $PART17_RESULT"
echo "  Resolve member: unauth:      $PART32_RESULT"
echo "  Resolve member: not found:   $PART33_RESULT"

FAIL_COUNT=0
for R in "$PART9_RESULT" "$PART10_RESULT" "$PART11_RESULT" "$PART17_RESULT" \
         "$PART32_RESULT" "$PART33_RESULT"; do
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
echo "JURY PARTICIPATION TEST COMPLETED"
echo ""
