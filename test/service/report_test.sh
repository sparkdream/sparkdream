#!/bin/bash

echo "--- TESTING: x/service REPORT (file + reject paths + queries) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

echo "Target operator:      operator1 ($OPERATOR1_ADDR)"
echo "Reporter:             reporter1 ($REPORTER1_ADDR)"
echo "Non-member reporter:  non_member ($NON_MEMBER_ADDR)"
echo "Alice (council mem):  $ALICE_ADDR"
echo "Service type:         $TEST_SERVICE_TYPE"
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

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 0
    fi
    return 1
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3
    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

submit_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
}

# MsgRegisterOperator.metadata is `bytes` in proto, so CLI requires
# base64. MsgReportOperator.reason is a plain `string`, no encoding.
b64() {
    printf '%s' "$1" | base64 -w0
}

# ========================================================================
# Precondition: operator1 ACTIVE (left in that state by register_test.sh)
# ========================================================================
echo "--- PRECONDITION: operator1 must be ACTIVE ---"

OP1_INFO=$($BINARY query service operator "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" --output json 2>&1)
OP1_STATUS=$(echo "$OP1_INFO" | jq -r '.operator.status // empty')

if [ -z "$OP1_STATUS" ]; then
    echo "operator1 not registered yet; registering now..."
    MIN_BOND_AMT=$($BINARY query service service-type "$TEST_SERVICE_TYPE" --output json | jq -r '.config.min_bond.amount')
    TX_RES=$($BINARY tx service register-operator \
        "$TEST_SERVICE_TYPE" "$COMMONS_POLICY" "${MIN_BOND_AMT}uspark" "$(b64 'operator1-report-init')" \
        --from operator1 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark -y --output json 2>&1)
    submit_and_wait "$TX_RES"
    if ! check_tx_success "$TX_RESULT"; then
        echo "FAIL: precondition register-operator failed"
        exit 1
    fi
    OP1_STATUS=$($BINARY query service operator "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" --output json | jq -r '.operator.status')
fi

if [ "$OP1_STATUS" != "OPERATOR_STATUS_ACTIVE" ]; then
    echo "FAIL: operator1 must be ACTIVE for report tests, got $OP1_STATUS"
    exit 1
fi
echo "OK: operator1 is ACTIVE"
echo ""

# ========================================================================
# PART 1: REJECTION — non-member reporter (trust-level gate)
# ========================================================================
echo "--- PART 1: REJECTION — non-member reporter ---"

# non_member has zero x/rep membership; MeetsTrustLevel returns false
# for any minimum (including TRUST_LEVEL_NEW, since "not a member" is
# distinct from "has trust level NEW").
TX_RES=$($BINARY tx service report-operator \
    "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" "non-member trying to file" \
    --from non_member --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_failure "$TX_RESULT"; then
    echo "FAIL: non-member reporter should have been rejected"
    exit 1
fi
echo "OK: non-member reporter rejected"
echo ""

# ========================================================================
# PART 2: REJECTION — reporter is controller-group member (alice is on
# the Commons Council, which is operator1's controller)
# ========================================================================
echo "--- PART 2: REJECTION — controller-group member as reporter (alice) ---"

TX_RES=$($BINARY tx service report-operator \
    "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" "alice cannot report on Council-controlled op" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_failure "$TX_RESULT"; then
    echo "FAIL: alice (controller-group member) should have been rejected"
    exit 1
fi
echo "OK: controller-group-member reporter rejected"
echo ""

# ========================================================================
# PART 3: HAPPY PATH — reporter1 files a report
# ========================================================================
echo "--- PART 3: HAPPY PATH — reporter1 files ---"

REPORTER_BAL_BEFORE=$($BINARY query bank balances "$REPORTER1_ADDR" --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
echo "reporter1 balance before: $REPORTER_BAL_BEFORE uspark"

TX_RES=$($BINARY tx service report-operator \
    "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" "reporter1 alleges operator1 misbehaved on test deployment" \
    --from reporter1 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: reporter1 report-operator failed"
    exit 1
fi

REPORT_ID=$(extract_event_value "$TX_RESULT" "service.report_filed" "report_id")
if [ -z "$REPORT_ID" ]; then
    echo "FAIL: could not extract report_id from event"
    echo "$TX_RESULT" | jq '.events'
    exit 1
fi
echo "OK: filed report #$REPORT_ID"

# Verify the report record is PENDING with the right shape.
REPORT_INFO=$($BINARY query service report "$REPORT_ID" --output json 2>&1)
R_STATUS=$(echo "$REPORT_INFO" | jq -r '.report.status')
R_OP=$(echo "$REPORT_INFO" | jq -r '.report.operator_address')
R_RPT=$(echo "$REPORT_INFO" | jq -r '.report.reporter')
R_SVC=$(echo "$REPORT_INFO" | jq -r '.report.service_type')
R_DEP=$(echo "$REPORT_INFO" | jq -r '.report.deposit.amount')

if [ "$R_STATUS" != "REPORT_STATUS_PENDING" ]; then
    echo "FAIL: expected REPORT_STATUS_PENDING, got $R_STATUS"
    echo "$REPORT_INFO"
    exit 1
fi
if [ "$R_OP" != "$OPERATOR1_ADDR" ]; then
    echo "FAIL: report.operator_address mismatch: expected $OPERATOR1_ADDR, got $R_OP"
    exit 1
fi
if [ "$R_RPT" != "$REPORTER1_ADDR" ]; then
    echo "FAIL: report.reporter mismatch: expected $REPORTER1_ADDR, got $R_RPT"
    exit 1
fi
if [ "$R_SVC" != "$TEST_SERVICE_TYPE" ]; then
    echo "FAIL: report.service_type mismatch: expected $TEST_SERVICE_TYPE, got $R_SVC"
    exit 1
fi
echo "OK: Report record verified (status=$R_STATUS deposit=$R_DEP)"

# Verify deposit was escrowed.
REPORTER_BAL_AFTER=$($BINARY query bank balances "$REPORTER1_ADDR" --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
echo "reporter1 balance after:  $REPORTER_BAL_AFTER uspark (lost $((REPORTER_BAL_BEFORE - REPORTER_BAL_AFTER)) to deposit + gas)"
echo ""

# ========================================================================
# PART 4: ReportsByOperator query returns the new report
# ========================================================================
echo "--- PART 4: Query ReportsByOperator returns the new report ---"

BY_OP=$($BINARY query service reports-by-operator "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" --output json 2>&1)
# proto JSON renders uint64 fields as strings, so compare as strings.
FOUND=$(echo "$BY_OP" | jq -r --arg id "$REPORT_ID" '.reports | map(select(.report_id == $id)) | length')
if [ "$FOUND" -lt 1 ]; then
    echo "FAIL: reports-by-operator did not contain report_id $REPORT_ID"
    echo "$BY_OP"
    exit 1
fi
echo "OK: ReportsByOperator returns report #$REPORT_ID"
echo ""

# ========================================================================
# PART 5: REJECTION — reporter rate-limit cap (3 reports per window)
# ========================================================================
echo "--- PART 5: REJECTION — reporter rate-limit cap ---"

# Default cap = 3 per (reporter, op, service_type) in window. We've
# already filed 1 in PART 3. File 2 more (total 3), then the 4th must
# fail with ErrReporterRateLimitExceeded.

for i in 2 3; do
    TX_RES=$($BINARY tx service report-operator \
        "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" "follow-up report #$i" \
        --from reporter1 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark -y --output json 2>&1)
    submit_and_wait "$TX_RES"
    if ! check_tx_success "$TX_RESULT"; then
        echo "FAIL: follow-up report #$i should have succeeded (cap is 3)"
        echo "$TX_RESULT" | jq -r '.raw_log'
        exit 1
    fi
    NEW_ID=$(extract_event_value "$TX_RESULT" "service.report_filed" "report_id")
    echo "  filed report #$NEW_ID (count = $i)"
done

# The 4th filing must now hit the rate limit.
TX_RES=$($BINARY tx service report-operator \
    "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" "over the cap" \
    --from reporter1 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_failure "$TX_RESULT"; then
    echo "FAIL: 4th report from reporter1 should hit the rate-limit cap"
    exit 1
fi
RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
echo "OK: 4th report rejected: $(echo "$RAW_LOG" | head -c 200)"
echo ""

# ========================================================================
# Summary
# ========================================================================
TOTAL_REPORTS=$($BINARY query service reports-by-operator "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" --output json | jq -r '.reports | length')
echo "Final ReportsByOperator count for operator1: $TOTAL_REPORTS"
echo ""

echo "=================================================="
echo ">>> REPORT TESTS: PASSED <<<"
echo "=================================================="
exit 0
