#!/bin/bash

echo "--- TESTING: x/service CHECKPOINTS (content-scanner seed, MsgSubmitCheckpoint, queries) ---"
# docs/content-scanning.md §8.2 / x-service-spec §3.8. Liveness reports need
# checkpoint_max_lag_blocks (~1 day) to elapse, so they are covered by unit
# tests (x/service/keeper/checkpoints_test.go), not here.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

SERVICE_TYPE="content-scanner"

wait_for_tx() {
    local ATTEMPT=0
    while [ $ATTEMPT -lt 20 ]; do
        RESULT=$($BINARY q tx $1 --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done
    return 1
}

submit_and_wait() {
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
    TX_RESULT=$(wait_for_tx "$TXHASH")
}
tx_code() { echo "$1" | jq -r '.code // "1"' 2>/dev/null; }
raw_log() { echo "$1" | jq -r '.raw_log // empty' 2>/dev/null; }

FAIL_COUNT=0
pass() { echo "  [ OK ] $1"; }
fail() { echo "  [FAIL] $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }

height() { $BINARY status 2>/dev/null | jq -r '.sync_info.latest_block_height'; }
checkpoint() { # height root_b64
    $BINARY tx service submit-checkpoint "$SERVICE_TYPE" "$1" "$2" \
        --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1
}
ROOT_B64=$(head -c 32 /dev/zero | tr '\0' '\253' | base64 -w0)
SHORT_B64=$(printf 'x' | base64 -w0)

# ========================================================================
# PART 1: the content-scanner type is seeded at genesis
# ========================================================================
echo "--- PART 1: content-scanner service type ---"
CFG=$($BINARY query service service-type "$SERVICE_TYPE" --output json 2>&1)
QUORUM=$(echo "$CFG" | jq -r '.config.attestation_quorum // "0"')
LAG=$(echo "$CFG" | jq -r '.config.checkpoint_max_lag_blocks // "0"')
ENABLED=$(echo "$CFG" | jq -r '.config.enabled // false')
MIN_BOND=$(echo "$CFG" | jq -r '.config.min_bond_amount // empty')
if [ "$ENABLED" == "true" ] && [ "$QUORUM" -ge 1 ] && [ "$LAG" -gt 0 ] && [ -n "$MIN_BOND" ]; then
    pass "content-scanner enabled, attestation_quorum=$QUORUM, checkpoint_max_lag_blocks=$LAG, min_bond=$MIN_BOND"
else
    fail "content-scanner config: $(echo "$CFG" | head -c 400)"
    exit 1
fi

# ========================================================================
# PART 2: register operator2 as a scanner (controller: commons policy)
# ========================================================================
echo "--- PART 2: register a content-scanner operator ---"
EXISTING=$($BINARY query service operator "$OPERATOR2_ADDR" "$SERVICE_TYPE" --output json 2>/dev/null | jq -r '.operator.status // empty')
if [ -n "$EXISTING" ]; then
    pass "operator2 already registered ($EXISTING)"
else
    META=$(printf '{"v":1,"verdict_key":"%s","feed_url":"https://feeds.example/zenith","ruleset_version":"test"}' \
        "$(head -c 32 /dev/zero | base64 -w0)" | base64 -w0)
    TX_RES=$($BINARY tx service register-operator "$SERVICE_TYPE" "$COMMONS_POLICY" "$MIN_BOND" "$META" \
        --from operator2 --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
    submit_and_wait "$TX_RES"
    if [ "$(tx_code "$TX_RESULT")" == "0" ]; then
        pass "operator2 registered under $SERVICE_TYPE"
    else
        fail "register-operator: $(raw_log "$TX_RESULT")"
        exit 1
    fi
fi

# ========================================================================
# PART 3: submit checkpoints
# ========================================================================
echo "--- PART 3: submit-checkpoint ---"
H=$(height)
CP_HEIGHT=$((H - 1))

submit_and_wait "$(checkpoint "$CP_HEIGHT" "$SHORT_B64")"
if [ "$(tx_code "$TX_RESULT")" != "0" ] && raw_log "$TX_RESULT" | grep -q "invalid checkpoint"; then
    pass "short root rejected"
else
    fail "short root: code=$(tx_code "$TX_RESULT") $(raw_log "$TX_RESULT")"
fi

submit_and_wait "$(checkpoint "$((H + 100000))" "$ROOT_B64")"
if [ "$(tx_code "$TX_RESULT")" != "0" ] && raw_log "$TX_RESULT" | grep -q "invalid checkpoint"; then
    pass "future height rejected"
else
    fail "future height: code=$(tx_code "$TX_RESULT") $(raw_log "$TX_RESULT")"
fi

submit_and_wait "$(checkpoint "$CP_HEIGHT" "$ROOT_B64")"
if [ "$(tx_code "$TX_RESULT")" == "0" ]; then
    pass "checkpoint at height $CP_HEIGHT accepted"
else
    fail "checkpoint: $(raw_log "$TX_RESULT")"
fi

submit_and_wait "$(checkpoint "$CP_HEIGHT" "$ROOT_B64")"
if [ "$(tx_code "$TX_RESULT")" != "0" ] && raw_log "$TX_RESULT" | grep -q "must exceed previous checkpoint"; then
    pass "non-advancing height rejected"
else
    fail "repeat height: code=$(tx_code "$TX_RESULT") $(raw_log "$TX_RESULT")"
fi

# ========================================================================
# PART 4: queries
# ========================================================================
echo "--- PART 4: checkpoint queries ---"
CP=$($BINARY query service checkpoint "$OPERATOR2_ADDR" "$SERVICE_TYPE" --output json 2>&1)
if [ "$(echo "$CP" | jq -r '.checkpoint.height // "0"')" == "$CP_HEIGHT" ] && [ "$(echo "$CP" | jq -r '.checkpoint.root // ""')" == "$ROOT_B64" ]; then
    pass "checkpoint query returns height and root"
else
    fail "checkpoint query: $(echo "$CP" | head -c 300)"
fi

LIST=$($BINARY query service checkpoints-by-service-type "$SERVICE_TYPE" --output json 2>&1)
if echo "$LIST" | jq -e --arg op "$OPERATOR2_ADDR" '.checkpoints[] | select(.operator == $op)' > /dev/null 2>&1; then
    pass "checkpoints-by-service-type lists operator2"
else
    fail "checkpoints-by-service-type: $(echo "$LIST" | head -c 300)"
fi

if $BINARY query service checkpoint "$OPERATOR1_ADDR" "$SERVICE_TYPE" --output json > /dev/null 2>&1; then
    fail "checkpoint query returned a record for an operator that never checkpointed"
else
    pass "checkpoint query 404s for an operator without checkpoints"
fi

echo ""
if [ "$FAIL_COUNT" -eq 0 ]; then
    echo "Checkpoint tests: all passed"
    exit 0
fi
echo "Checkpoint tests: $FAIL_COUNT failed"
exit 1
