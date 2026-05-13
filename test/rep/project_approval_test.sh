#!/bin/bash

echo "--- TESTING: PROJECT APPROVAL (TIER + COUNCIL-LOCK) ---"
echo ""
echo "Covers the tightened ApproveProject authorization:"
echo "  • Locked to the picked council (no cross-council ops-committee fallback)"
echo "  • Tier-by-budget gate: individual committee member only up to"
echo "    params.large_project_budget_threshold; above that requires a passed"
echo "    council / committee proposal."
echo ""

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo "Alice: $ALICE_ADDR   (Technical Operations Committee member — founder)"
echo "Bob:   $BOB_ADDR   (founding member, NOT on any operations committee)"
echo ""

# Tracking
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

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

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    local BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

check_tx_success() {
    [ "$(echo "$1" | jq -r '.code')" = "0" ]
}

check_tx_failure() {
    [ "$(echo "$1" | jq -r '.code')" != "0" ]
}

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" = "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}

# Helper: propose a budget-backed project pointed at the given council and
# requesting the given budget. Echoes the project_id on success.
propose_project() {
    local NAME="$1"
    local COUNCIL="$2"
    local REQUESTED_DREAM="$3" # in micro-DREAM
    local FROM="$4"

    local TX_RES
    TX_RES=$($BINARY tx rep propose-project \
        "$NAME" \
        "Project for x/rep approval e2e" \
        "infrastructure" \
        "$COUNCIL" \
        "$REQUESTED_DREAM" \
        "0" \
        --from "$FROM" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        --gas 400000 \
        -y \
        --output json 2>&1)

    if ! submit_tx_and_wait "$TX_RES"; then
        echo "ERROR: propose_project broadcast failed" >&2
        return 1
    fi
    if ! check_tx_success "$TX_RESULT"; then
        echo "ERROR: propose_project tx failed: $(echo "$TX_RESULT" | jq -r '.raw_log // ""')" >&2
        return 1
    fi
    echo "$TX_RESULT" | jq -r '.events[] | select(.type=="project_proposed") | .attributes[] | select(.key=="project_id") | .value' | tr -d '"'
}

# Read the live threshold so the tests stay correct if governance moves it.
PARAMS=$($BINARY query rep params --output json 2>&1)
THRESHOLD_UDREAM=$(echo "$PARAMS" | jq -r '.params.large_project_budget_threshold // "10000000000"')
THRESHOLD_DREAM=$((THRESHOLD_UDREAM / 1000000))
echo "Large project budget threshold: $THRESHOLD_UDREAM udream ($THRESHOLD_DREAM DREAM)"
echo ""

# ========================================================================
# TEST 1 — Small-budget approval by Alice (committee member, picked council)
# ========================================================================
# Below the threshold, an individual member of the picked council's
# operations committee is sufficient. This is the bread-and-butter path
# the rest of the suite already relies on; we re-assert it explicitly so
# regressions in the tier logic don't silently pass.
echo "--- TEST 1: small budget approved by committee member (alice / Technical) ---"

SMALL_BUDGET=100000000 # 100 DREAM
PID=$(propose_project "approval-test-small" "Technical Council" "$SMALL_BUDGET" alice)
if [ -z "$PID" ]; then
    echo "  Failed to create project"
    record_result "TEST 1: small budget approved" "FAIL"
else
    TX_RES=$($BINARY tx rep approve-project-budget \
        "$PID" "$SMALL_BUDGET" "0" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark --gas 300000 -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Project #$PID approved (budget $SMALL_BUDGET udream)"
        record_result "TEST 1: small budget approved" "PASS"
    else
        echo "  Approval rejected: $(echo "$TX_RESULT" | jq -r '.raw_log // ""')"
        record_result "TEST 1: small budget approved" "FAIL"
    fi
fi

# ========================================================================
# TEST 2 — Large-budget tier rejection
# ========================================================================
# Budget > threshold: even Alice (Tech Ops committee member) cannot approve
# without a passed council/committee proposal. The expected error is
# types.ErrLargeProjectNeedsCouncil ("project budget exceeds threshold;
# requires council proposal approval").
echo "--- TEST 2: large budget rejected for plain committee member ---"

LARGE_BUDGET=$((THRESHOLD_UDREAM + 1000000)) # threshold + 1 DREAM to keep
                                              # the request well above the cap
                                              # but still a sane sim value.
PID=$(propose_project "approval-test-large" "Technical Council" "$LARGE_BUDGET" alice)
if [ -z "$PID" ]; then
    echo "  Failed to create project (the proposal itself doesn't trip the cap)"
    record_result "TEST 2: large budget rejected" "FAIL"
else
    TX_RES=$($BINARY tx rep approve-project-budget \
        "$PID" "$LARGE_BUDGET" "0" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark --gas 300000 -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        if echo "$RAW" | grep -qi "exceeds threshold\|council proposal"; then
            echo "  Correctly rejected (large budget needs council proposal): $RAW"
            record_result "TEST 2: large budget rejected" "PASS"
        else
            echo "  Rejected but with unexpected error: $RAW"
            record_result "TEST 2: large budget rejected" "FAIL"
        fi
    else
        echo "  Expected rejection but tx succeeded"
        record_result "TEST 2: large budget rejected" "FAIL"
    fi
fi

# ========================================================================
# TEST 3 — Threshold boundary
# ========================================================================
# Exactly at the threshold (GT is strict, so equal-to is still "small"):
# Alice should still be able to approve unilaterally. Guards against an
# off-by-one in the chain's tier check.
echo "--- TEST 3: budget exactly at threshold still approved by committee member ---"

PID=$(propose_project "approval-test-boundary" "Technical Council" "$THRESHOLD_UDREAM" alice)
if [ -z "$PID" ]; then
    echo "  Failed to create project"
    record_result "TEST 3: at-threshold approved" "FAIL"
else
    TX_RES=$($BINARY tx rep approve-project-budget \
        "$PID" "$THRESHOLD_UDREAM" "0" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark --gas 300000 -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Project #$PID approved at exactly threshold ($THRESHOLD_UDREAM udream)"
        record_result "TEST 3: at-threshold approved" "PASS"
    else
        echo "  Approval rejected: $(echo "$TX_RESULT" | jq -r '.raw_log // ""')"
        record_result "TEST 3: at-threshold approved" "FAIL"
    fi
fi

# ========================================================================
# TEST 4 — Non-ops-committee member rejected
# ========================================================================
# Bob is a founding member (genesis name registration, DREAM grant) but is
# NOT seated on any operations committee. Even for a small budget, his
# approval call should be rejected with ErrUnauthorized — the rule is no
# longer "any global Ops Committee member can approve", it's "must be on
# the picked council's ops committee".
echo "--- TEST 4: non-ops-committee member cannot approve ---"

PID=$(propose_project "approval-test-bob" "Technical Council" "$SMALL_BUDGET" alice)
if [ -z "$PID" ]; then
    echo "  Failed to create project"
    record_result "TEST 4: non-committee member rejected" "FAIL"
else
    TX_RES=$($BINARY tx rep approve-project-budget \
        "$PID" "$SMALL_BUDGET" "0" \
        --from bob --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark --gas 300000 -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        if echo "$RAW" | grep -qi "unauthorized\|operations committee\|member of"; then
            echo "  Correctly rejected (bob is not on Tech Ops): $RAW"
            record_result "TEST 4: non-committee member rejected" "PASS"
        else
            echo "  Rejected but with unexpected error: $RAW"
            record_result "TEST 4: non-committee member rejected" "FAIL"
        fi
    else
        echo "  Expected rejection but tx succeeded — bob should not be able to approve"
        record_result "TEST 4: non-committee member rejected" "FAIL"
    fi
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "PROJECT APPROVAL TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
    exit 0
fi
