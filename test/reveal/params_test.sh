#!/bin/bash

echo "--- TESTING: x/reveal MODULE PARAMS ---"

BINARY="sparkdreamd"

# --- Result Tracking ---
QUERY_PARAMS_RESULT="FAIL"
VERIFY_DEFAULTS_RESULT="FAIL"

# ========================================================================
# TEST 1: Query Reveal Params
# ========================================================================
echo ""
echo "--- TEST 1: QUERY REVEAL PARAMS ---"

PARAMS_JSON=$($BINARY query reveal params --output json 2>&1)

if echo "$PARAMS_JSON" | jq -e '.params' > /dev/null 2>&1; then
    echo "  Params retrieved successfully"
    echo "$PARAMS_JSON" | jq '.params'
    QUERY_PARAMS_RESULT="PASS"
else
    echo "  FAIL: Could not query reveal params"
    echo "  Response: $PARAMS_JSON"
fi
echo ""

# ========================================================================
# TEST 2: Verify Default Parameter Values
# ========================================================================
echo "--- TEST 2: VERIFY DEFAULT PARAMETER VALUES ---"

if [ "$QUERY_PARAMS_RESULT" == "PASS" ]; then
    VERIFY_OK=true

    STAKE_DEADLINE=$(echo "$PARAMS_JSON" | jq -r '.params.stake_deadline_epochs')
    REVEAL_DEADLINE=$(echo "$PARAMS_JSON" | jq -r '.params.reveal_deadline_epochs')
    VERIFICATION_PERIOD=$(echo "$PARAMS_JSON" | jq -r '.params.verification_period_epochs')
    DISPUTE_RESOLUTION=$(echo "$PARAMS_JSON" | jq -r '.params.dispute_resolution_epochs')
    MAX_TRANCHES=$(echo "$PARAMS_JSON" | jq -r '.params.max_tranches')
    MIN_VERIFICATION_VOTES=$(echo "$PARAMS_JSON" | jq -r '.params.min_verification_votes')
    MIN_PROPOSER_TRUST=$(echo "$PARAMS_JSON" | jq -r '.params.min_proposer_trust_level')

    echo "  stake_deadline_epochs:      $STAKE_DEADLINE (expected: 60)"
    echo "  reveal_deadline_epochs:     $REVEAL_DEADLINE (expected: 14)"
    echo "  verification_period_epochs: $VERIFICATION_PERIOD (expected: 14)"
    echo "  dispute_resolution_epochs:  $DISPUTE_RESOLUTION (expected: 30)"
    echo "  max_tranches:               $MAX_TRANCHES (expected: 10)"
    echo "  min_verification_votes:     $MIN_VERIFICATION_VOTES (expected: 3)"
    echo "  min_proposer_trust_level:   $MIN_PROPOSER_TRUST (expected: 2)"

    # Verify key defaults
    if [ "$STAKE_DEADLINE" != "60" ]; then
        echo "  stake_deadline_epochs mismatch"
        VERIFY_OK=false
    fi
    if [ "$REVEAL_DEADLINE" != "14" ]; then
        echo "  reveal_deadline_epochs mismatch"
        VERIFY_OK=false
    fi
    if [ "$VERIFICATION_PERIOD" != "14" ]; then
        echo "  verification_period_epochs mismatch"
        VERIFY_OK=false
    fi
    if [ "$DISPUTE_RESOLUTION" != "30" ]; then
        echo "  dispute_resolution_epochs mismatch"
        VERIFY_OK=false
    fi
    if [ "$MAX_TRANCHES" != "10" ]; then
        echo "  max_tranches mismatch"
        VERIFY_OK=false
    fi
    if [ "$MIN_VERIFICATION_VOTES" != "3" ]; then
        echo "  min_verification_votes mismatch"
        VERIFY_OK=false
    fi
    if [ "$MIN_PROPOSER_TRUST" != "2" ]; then
        echo "  min_proposer_trust_level mismatch"
        VERIFY_OK=false
    fi

    if [ "$VERIFY_OK" == true ]; then
        VERIFY_DEFAULTS_RESULT="PASS"
        echo "  PASS: All default parameter values match"
    else
        echo "  FAIL: Some parameter values do not match defaults"
    fi
else
    echo "  SKIP: Query params failed"
fi
echo ""

# --- RESULTS SUMMARY ---
echo "============================================================================"
echo "  REVEAL PARAMS TEST RESULTS"
echo "============================================================================"
echo ""

TOTAL_COUNT=0
PASS_COUNT=0
FAIL_COUNT=0

for RESULT in "$QUERY_PARAMS_RESULT" "$VERIFY_DEFAULTS_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

echo "  1. Query Params:           $QUERY_PARAMS_RESULT"
echo "  2. Verify Default Values:  $VERIFY_DEFAULTS_RESULT"
echo ""
echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
