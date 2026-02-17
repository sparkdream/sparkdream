#!/bin/bash

echo "--- TESTING: x/reveal PROPOSE CONTRIBUTIONS ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    source "$SCRIPT_DIR/.test_env"
else
    echo "ERROR: .test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi

# --- Result Tracking ---
PROPOSE_BASIC_RESULT="FAIL"
QUERY_CONTRIBUTION_RESULT="FAIL"
QUERY_BY_CONTRIBUTOR_RESULT="FAIL"
QUERY_BY_STATUS_RESULT="FAIL"
NEG_EMPTY_NAME_RESULT="FAIL"
NEG_VALUATION_MISMATCH_RESULT="FAIL"
NEG_TOO_HIGH_VALUATION_RESULT="FAIL"
NEG_NO_TRANCHES_RESULT="FAIL"
NEG_TOO_MANY_TRANCHES_RESULT="FAIL"

# --- Helper Functions ---
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

expect_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" == "0" ]; then
        return 1  # Expected failure but got success
    fi
    return 0  # Got expected failure
}

# ========================================================================
# TEST 1: Propose a basic contribution (happy path)
# ========================================================================
echo ""
echo "--- TEST 1: PROPOSE BASIC CONTRIBUTION ---"

# Two tranches: 1000 DREAM each = 2000 total (use camelCase JSON, one per --tranches flag)
echo "  Submitting proposal: Project Phoenix (2 tranches, 2000 DREAM total)..."

TX_RES=$($BINARY tx reveal propose \
    "Project Phoenix" \
    "A progressive reveal test project" \
    "2000" \
    "MIT" \
    "Apache-2.0" \
    --tranches '{"name":"Core Module","description":"Core functionality","components":["module.go","handler.go"],"stakeThreshold":"1000","previewUri":"https://example.com/preview-core"}' \
    --tranches '{"name":"API Layer","description":"REST API endpoints","components":["api.go","routes.go"],"stakeThreshold":"1000","previewUri":"https://example.com/preview-api"}' \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  FAIL: No txhash returned"
    echo "  Raw: $TX_RES"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        CONTRIBUTION_ID=$(extract_event_value "$TX_RESULT" "contribution_proposed" "contribution_id")
        if [ -z "$CONTRIBUTION_ID" ]; then
            echo "  Could not extract contribution_id from events"
            # Try to find it from contributions query
            CONTRIBUTION_ID=$($BINARY query reveal contributions --output json 2>&1 | jq -r '.contributions[-1].id // empty')
        fi

        if [ -n "$CONTRIBUTION_ID" ]; then
            PROPOSE_BASIC_RESULT="PASS"
            echo "  PASS: Contribution proposed (ID: $CONTRIBUTION_ID)"
            # Save for later tests
            echo "export TEST_CONTRIBUTION_ID=$CONTRIBUTION_ID" >> "$SCRIPT_DIR/.test_env"
        else
            echo "  FAIL: Contribution proposed but could not determine ID"
        fi
    else
        echo "  FAIL: Transaction failed"
    fi
fi
echo ""

# ========================================================================
# TEST 2: Query contribution by ID
# ========================================================================
echo "--- TEST 2: QUERY CONTRIBUTION BY ID ---"

if [ -n "$CONTRIBUTION_ID" ]; then
    CONTRIB_JSON=$($BINARY query reveal contribution $CONTRIBUTION_ID --output json 2>&1)

    if echo "$CONTRIB_JSON" | jq -e '.contribution' > /dev/null 2>&1; then
        STATUS=$(echo "$CONTRIB_JSON" | jq -r '.contribution.status')
        PROJECT_NAME=$(echo "$CONTRIB_JSON" | jq -r '.contribution.project_name')
        CONTRIBUTOR=$(echo "$CONTRIB_JSON" | jq -r '.contribution.contributor')
        TRANCHE_COUNT=$(echo "$CONTRIB_JSON" | jq -r '.contribution.tranches | length')
        TOTAL_VAL=$(echo "$CONTRIB_JSON" | jq -r '.contribution.total_valuation')
        BOND_AMT=$(echo "$CONTRIB_JSON" | jq -r '.contribution.bond_amount')

        echo "  Status:       $STATUS"
        echo "  Project:      $PROJECT_NAME"
        echo "  Contributor:  $CONTRIBUTOR"
        echo "  Tranches:     $TRANCHE_COUNT"
        echo "  Valuation:    $TOTAL_VAL"
        echo "  Bond:         $BOND_AMT"

        # Note: proto3 omits zero-value enums, so PROPOSED (0) appears as null
        if ([ "$STATUS" == "CONTRIBUTION_STATUS_PROPOSED" ] || [ "$STATUS" == "null" ]) && [ "$PROJECT_NAME" == "Project Phoenix" ] && [ "$TRANCHE_COUNT" == "2" ]; then
            QUERY_CONTRIBUTION_RESULT="PASS"
            echo "  PASS: Contribution query matches expected values"
        else
            echo "  FAIL: Unexpected values in contribution query"
        fi
    else
        echo "  FAIL: Could not query contribution $CONTRIBUTION_ID"
        echo "  Response: $CONTRIB_JSON"
    fi
else
    echo "  SKIP: No contribution ID available"
fi
echo ""

# ========================================================================
# TEST 3: Query contributions by contributor
# ========================================================================
echo "--- TEST 3: QUERY CONTRIBUTIONS BY CONTRIBUTOR ---"

BY_CONTRIBUTOR=$($BINARY query reveal contributions-by-contributor $ALICE_ADDR --output json 2>&1)
COUNT=$(echo "$BY_CONTRIBUTOR" | jq -r '.contributions // [] | length')
echo "  Found $COUNT contributions by Alice"

if [ "$COUNT" -gt 0 ]; then
    QUERY_BY_CONTRIBUTOR_RESULT="PASS"
    echo "  PASS: Contributions by contributor query works"
else
    echo "  FAIL: Expected at least 1 contribution"
    echo "  Response: $BY_CONTRIBUTOR"
fi
echo ""

# ========================================================================
# TEST 4: Query contributions by status (PROPOSED)
# ========================================================================
echo "--- TEST 4: QUERY CONTRIBUTIONS BY STATUS ---"

BY_STATUS=$($BINARY query reveal contributions-by-status proposed --output json 2>&1)
COUNT=$(echo "$BY_STATUS" | jq -r '.contributions // [] | length')
echo "  Found $COUNT PROPOSED contributions"

if [ "$COUNT" -gt 0 ]; then
    QUERY_BY_STATUS_RESULT="PASS"
    echo "  PASS: Contributions by status query works"
else
    echo "  FAIL: Expected at least 1 PROPOSED contribution"
    echo "  Response: $BY_STATUS"
fi
echo ""

# ========================================================================
# TEST 5: Negative - Empty project name
# ========================================================================
echo "--- TEST 5: NEGATIVE - EMPTY PROJECT NAME ---"

TX_RES=$($BINARY tx reveal propose \
    "" \
    "Bad project with no name" \
    "1000" \
    "MIT" \
    "MIT" \
    --tranches '{"name":"T1","description":"d","components":["a"],"stakeThreshold":"1000","previewUri":""}' \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    # TX rejected at simulation / ante-handler level
    NEG_EMPTY_NAME_RESULT="PASS"
    echo "  PASS: Empty name rejected at submission"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if expect_tx_failure "$TX_RESULT"; then
        NEG_EMPTY_NAME_RESULT="PASS"
        echo "  PASS: Empty name rejected on-chain"
    else
        echo "  FAIL: Empty name was accepted (should have been rejected)"
    fi
fi
echo ""

# ========================================================================
# TEST 6: Negative - Valuation mismatch (sum != total)
# ========================================================================
echo "--- TEST 6: NEGATIVE - VALUATION MISMATCH ---"

# Total 1000 but tranches sum to 800
TX_RES=$($BINARY tx reveal propose \
    "Bad Valuation Project" \
    "Mismatched valuation" \
    "1000" \
    "MIT" \
    "MIT" \
    --tranches '{"name":"T1","description":"d","components":["a"],"stakeThreshold":"500","previewUri":""}' \
    --tranches '{"name":"T2","description":"d","components":["b"],"stakeThreshold":"300","previewUri":""}' \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    NEG_VALUATION_MISMATCH_RESULT="PASS"
    echo "  PASS: Valuation mismatch rejected at submission"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if expect_tx_failure "$TX_RESULT"; then
        NEG_VALUATION_MISMATCH_RESULT="PASS"
        echo "  PASS: Valuation mismatch rejected on-chain"
    else
        echo "  FAIL: Valuation mismatch was accepted (should have been rejected)"
    fi
fi
echo ""

# ========================================================================
# TEST 7: Negative - Total valuation exceeds max (default 50000)
# ========================================================================
echo "--- TEST 7: NEGATIVE - VALUATION TOO HIGH ---"

TX_RES=$($BINARY tx reveal propose \
    "Overvalued Project" \
    "Way too expensive" \
    "100000" \
    "MIT" \
    "MIT" \
    --tranches '{"name":"T1","description":"d","components":["a"],"stakeThreshold":"100000","previewUri":""}' \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    NEG_TOO_HIGH_VALUATION_RESULT="PASS"
    echo "  PASS: High valuation rejected at submission"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if expect_tx_failure "$TX_RESULT"; then
        NEG_TOO_HIGH_VALUATION_RESULT="PASS"
        echo "  PASS: High valuation rejected on-chain"
    else
        echo "  FAIL: High valuation was accepted (should have been rejected)"
    fi
fi
echo ""

# ========================================================================
# TEST 8: Negative - No tranches provided
# ========================================================================
echo "--- TEST 8: NEGATIVE - NO TRANCHES ---"

TX_RES=$($BINARY tx reveal propose \
    "No Tranche Project" \
    "Has zero tranches" \
    "1000" \
    "MIT" \
    "MIT" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    NEG_NO_TRANCHES_RESULT="PASS"
    echo "  PASS: No-tranches rejected at submission"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if expect_tx_failure "$TX_RESULT"; then
        NEG_NO_TRANCHES_RESULT="PASS"
        echo "  PASS: No-tranches rejected on-chain"
    else
        echo "  FAIL: No-tranches was accepted (should have been rejected)"
    fi
fi
echo ""

# ========================================================================
# TEST 9: Negative - Too many tranches (>10)
# ========================================================================
echo "--- TEST 9: NEGATIVE - TOO MANY TRANCHES ---"

# Build 11 tranches (max is 10) each worth 100 DREAM = 1100 total
TRANCHE_ARGS=""
for i in $(seq 1 11); do
    TRANCHE_ARGS="$TRANCHE_ARGS --tranches {\"name\":\"T$i\",\"description\":\"d\",\"components\":[\"a\"],\"stakeThreshold\":\"100\",\"previewUri\":\"\"}"
done

TX_RES=$(eval $BINARY tx reveal propose \
    '"Too Many Tranches"' \
    '"11 tranches exceed max"' \
    '"1100"' \
    '"MIT"' \
    '"MIT"' \
    $TRANCHE_ARGS \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    NEG_TOO_MANY_TRANCHES_RESULT="PASS"
    echo "  PASS: Too many tranches rejected at submission"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if expect_tx_failure "$TX_RESULT"; then
        NEG_TOO_MANY_TRANCHES_RESULT="PASS"
        echo "  PASS: Too many tranches rejected on-chain"
    else
        echo "  FAIL: Too many tranches was accepted (should have been rejected)"
    fi
fi
echo ""

# --- RESULTS SUMMARY ---
echo "============================================================================"
echo "  REVEAL PROPOSE TEST RESULTS"
echo "============================================================================"
echo ""

TOTAL_COUNT=0
PASS_COUNT=0
FAIL_COUNT=0

for RESULT in "$PROPOSE_BASIC_RESULT" "$QUERY_CONTRIBUTION_RESULT" "$QUERY_BY_CONTRIBUTOR_RESULT" "$QUERY_BY_STATUS_RESULT" "$NEG_EMPTY_NAME_RESULT" "$NEG_VALUATION_MISMATCH_RESULT" "$NEG_TOO_HIGH_VALUATION_RESULT" "$NEG_NO_TRANCHES_RESULT" "$NEG_TOO_MANY_TRANCHES_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

echo "  1. Propose Basic Contribution:      $PROPOSE_BASIC_RESULT"
echo "  2. Query Contribution by ID:        $QUERY_CONTRIBUTION_RESULT"
echo "  3. Query by Contributor:            $QUERY_BY_CONTRIBUTOR_RESULT"
echo "  4. Query by Status:                 $QUERY_BY_STATUS_RESULT"
echo "  5. Neg: Empty Project Name:         $NEG_EMPTY_NAME_RESULT"
echo "  6. Neg: Valuation Mismatch:         $NEG_VALUATION_MISMATCH_RESULT"
echo "  7. Neg: Valuation Too High:         $NEG_TOO_HIGH_VALUATION_RESULT"
echo "  8. Neg: No Tranches:                $NEG_NO_TRANCHES_RESULT"
echo "  9. Neg: Too Many Tranches:          $NEG_TOO_MANY_TRANCHES_RESULT"
echo ""
echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
