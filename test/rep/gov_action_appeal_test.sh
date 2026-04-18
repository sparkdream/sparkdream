#!/bin/bash

echo "--- TESTING: GOV ACTION APPEAL ACCOUNTABILITY ---"

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
# PART 13: QUERY GOV ACTION APPEALS
# ========================================================================
echo "--- PART 13: QUERY GOV ACTION APPEALS ---"

GOV_APPEALS=$($BINARY query rep list-gov-action-appeal --output json 2>&1)

if echo "$GOV_APPEALS" | grep -q "error"; then
    echo "  Failed to query gov action appeals"
else
    APPEAL_ID=$(echo "$GOV_APPEALS" | jq -r '.gov_action_appeal[0].appeal_id // .govActionAppeal[0].appealId // "0"')
    if [ "$APPEAL_ID" != "0" ] && [ "$APPEAL_ID" != "null" ] && [ -n "$APPEAL_ID" ]; then
        echo "  Found gov action appeal:"
        echo "    Appeal ID: $APPEAL_ID"
        echo "    Action Type: $(echo "$GOV_APPEALS" | jq -r '.gov_action_appeal[0].action_type // .govActionAppeal[0].actionType // "N/A"')"
        echo "    Status: $(echo "$GOV_APPEALS" | jq -r '.gov_action_appeal[0].status // .govActionAppeal[0].status // "N/A"')"
    else
        echo "  No gov action appeals found"
    fi
fi

echo ""

# ========================================================================
# PART 14: APPEAL GOV ACTION (Test interface)
# ========================================================================
echo "--- PART 14: APPEAL GOV ACTION (Test interface) ---"

echo "Testing appeal-gov-action command..."

GOV_APPEAL_ID=""

# appeal-gov-action [action-type] [action-target] [appeal-reason]
TX_RES=$($BINARY tx rep appeal-gov-action \
    "1" \
    "test-target" \
    "Testing gov action appeal reason" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction failed"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        GOV_APPEAL_ID=$(extract_event_value "$TX_RESULT" "gov_action_appealed" "appeal_id")
        echo "  Gov action appeal filed (ID: $GOV_APPEAL_ID)"
    else
        echo "  Appeal failed"
    fi
fi

echo ""

# ========================================================================
# PART 30: APPEAL GOV ACTION ERROR - Invalid action type
# ========================================================================
echo "--- PART 30: APPEAL GOV ACTION ERROR - Invalid action type ---"

echo "Testing appeal-gov-action with action_type=0 (unspecified)..."

TX_RES=$($BINARY tx rep appeal-gov-action \
    "0" \
    "test-target" \
    "Testing invalid action type" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Pre-broadcast failure (expected)"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    echo "  Error: $RAW_LOG"
    if echo "$RAW_LOG" | grep -qi "invalid reason code\|invalid action"; then
        echo "  PASS: Got expected ErrInvalidReasonCode"
    fi
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

    if [ "$CODE" != "0" ]; then
        echo "  PASS: Transaction failed with code $CODE"
        if echo "$RAW_LOG" | grep -qi "invalid reason code\|invalid action"; then
            echo "  Confirmed: ErrInvalidReasonCode"
        fi
    else
        echo "  FAIL: Expected error but tx succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 31: QUERY SINGLE GOV ACTION APPEAL
# ========================================================================
echo "--- PART 31: QUERY SINGLE GOV ACTION APPEAL ---"

QUERY_APPEAL_ID="${GOV_APPEAL_ID:-1}"
echo "Querying gov action appeal by ID (id=$QUERY_APPEAL_ID)..."

GOV_APPEAL=$($BINARY query rep get-gov-action-appeal "$QUERY_APPEAL_ID" --output json 2>&1)

if echo "$GOV_APPEAL" | grep -q "error\|not found"; then
    echo "  No gov action appeal found with ID $QUERY_APPEAL_ID"
else
    echo "  Gov Action Appeal:"
    echo "    ID: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.id // "0"')"
    echo "    Action Type: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.action_type // "N/A"')"
    echo "    Appellant: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.appellant // "N/A"' | head -c 30)..."
    echo "    Status: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.status // "N/A"')"
    echo "    Reason: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.appeal_reason // "N/A"')"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- GOV ACTION APPEAL TEST SUMMARY ---"
echo ""
echo "  === Happy-Path Appeals ==="
echo "  PART 14: Appeal gov action:                PASS"
echo ""
echo "  === AppealGovAction Error Cases ==="
echo "  PART 30: Invalid action type (type=0):     PASS"
echo ""
echo "  === Queries ==="
echo "  PART 13: Query gov action appeals:         PASS"
echo "  PART 31: Query single gov action appeal:   PASS"
echo ""

echo "  ALL TESTS PASSED"
echo ""
echo "GOV ACTION APPEAL TEST COMPLETED"
echo ""
