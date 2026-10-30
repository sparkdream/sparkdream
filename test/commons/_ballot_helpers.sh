#!/bin/bash
# Shared helpers for secret_ballot_test.sh and poll_test.sh.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

wait_for_tx() {
    local TXHASH=$1
    local ATTEMPT=0
    while [ $ATTEMPT -lt 20 ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done
    echo "ERROR: Transaction $TXHASH not found" >&2
    return 1
}

check_tx_success() {
    [ "$(echo "$1" | jq -r '.code')" == "0" ]
}

# submit_tx_and_wait sets TX_RESULT to the delivered tx (or the broadcast
# response when CheckTx refused it).
submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash' 2>/dev/null)
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    if [ "$(echo "$TX_RES" | jq -r '.code // "0"')" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
}

raw_log() {
    echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null
}

extract_event_value() {
    echo "$1" | jq -r ".events[] | select(.type==\"$2\") | .attributes[] | select(.key==\"$3\") | .value" | tr -d '"' | head -n 1
}

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    TEST_NAMES+=("$1")
    RESULTS+=("$2")
    if [ "$2" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $2"
    echo ""
}

# expect_refusal NAME PATTERN: the last tx failed with PATTERN in its log.
expect_refusal() {
    local LOG=$(raw_log)
    if check_tx_success "$TX_RESULT"; then
        echo "  ERROR: accepted"
        record_result "$1" "FAIL"
    elif echo "$LOG" | grep -q "$2"; then
        echo "  Correctly refused: $2"
        record_result "$1" "PASS"
    else
        echo "  ERROR: refused for another reason: ${LOG:0:200}"
        record_result "$1" "FAIL"
    fi
}

print_summary() {
    echo "=================================================="
    echo "  $1 SUMMARY"
    echo "=================================================="
    for i in "${!TEST_NAMES[@]}"; do
        echo "  ${RESULTS[$i]}  ${TEST_NAMES[$i]}"
    done
    echo ""
    echo "  Passed: $PASS_COUNT  Failed: $FAIL_COUNT"
    if [ $FAIL_COUNT -gt 0 ]; then
        echo ">>> SOME TESTS FAILED <<<"
        exit 1
    fi
    echo ">>> ALL TESTS PASSED <<<"
}

# tx runs a sparkdreamd tx from an account and waits for it. A commons
# submit-proposal pays the proposal fee (the ante handler refuses less).
tx() {
    local FROM=$1
    shift
    local FEES=500000
    if [ "$1" == "commons" ] && [ "$2" == "submit-proposal" ]; then
        FEES=$($BINARY query commons params --output json 2>/dev/null | jq -r '.params.proposal_fee // empty' | grep -oE '^[0-9]+')
        FEES=$(( ${FEES:-5000000} + 500000 ))
    fi
    local RES=$($BINARY tx "$@" --from "$FROM" --chain-id $CHAIN_ID --keyring-backend test \
        --fees ${FEES}${BOND_DENOM} --gas 2000000 -y --output json 2>&1)
    submit_tx_and_wait "$RES"
}
