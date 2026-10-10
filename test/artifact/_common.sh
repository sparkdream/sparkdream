#!/bin/bash
# ============================================================================
# X/ARTIFACT E2E TEST COMMON HELPERS
# ============================================================================
# Sourced by every artifact/*_test.sh. Provides:
#
#   Globals: BINARY, CHAIN_ID, BOND_DENOM, DREAM_DENOM, ALICE_ADDR, BOB_ADDR,
#            CAROL_ADDR, OUTSIDER_ADDR (from .test_env), LICENSE
#
#   tx <args...>                 -> submit `tx artifact <args>`; echoes the
#                                   delivered tx JSON; returns non-zero on any
#                                   CheckTx or DeliverTx failure
#   expect_ok <desc> <args...>   -> tx must succeed; records PASS/FAIL
#   expect_fail <desc> <pattern> <args...>
#                                -> tx must fail with raw_log matching pattern
#   q <args...>                  -> `query artifact <args> --output json`
#   check <desc> <actual> <expected>
#   balance <addr>               -> bond-denom balance (integer)
#   wait_blocks <n>              -> poll until height advances by n
#   wait_until_time <unix>       -> poll until the latest block time >= unix
#   finish                       -> prints summary, exits 1 on any failure
#
# Notes:
#   - proto3 JSON omits zero-valued uint64/bool fields; use jq `// "0"` /
#     `// false` when reading them.
#   - Every wait polls chain state (block height / block time), never a
#     wall-clock sleep alone, so the suite tolerates slow blocks.
# ============================================================================

BINARY="${BINARY:-sparkdreamd}"
CHAIN_ID="${CHAIN_ID:-sparkdream}"
SCRIPT_DIR="${SCRIPT_DIR:-$( cd "$( dirname "${BASH_SOURCE[1]}" )" && pwd )}"
source "$SCRIPT_DIR/../lib/denoms.sh"

if [ -f "$SCRIPT_DIR/.test_env" ]; then
    source "$SCRIPT_DIR/.test_env"
fi

LICENSE="CC0-1.0"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
# Always resolve the outsider from the keyring that signs its txs, so a
# stale .test_env can never point assertions at a different address.
OUTSIDER_ADDR=$($BINARY keys show artifact_outsider -a --keyring-backend test 2>/dev/null || echo "${OUTSIDER_ADDR:-}")

PASS_COUNT=0
FAIL_COUNT=0
declare -a FAILURES=()

pass() { echo "  [PASS] $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "  [FAIL] $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); FAILURES+=("$1"); }

wait_for_tx() {
    local hash=$1 attempt=0 res
    while [ $attempt -lt 30 ]; do
        res=$($BINARY q tx "$hash" --output json 2>/dev/null)
        if echo "$res" | jq -e '.code' > /dev/null 2>&1; then
            echo "$res"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    echo "{\"code\": -1, \"raw_log\": \"tx $hash not found\"}"
    return 1
}

# tx <subcommand args...> --from <key>
# Echoes the delivered tx JSON (or the CheckTx response on early failure).
tx() {
    local res hash code
    res=$($BINARY tx artifact "$@" --chain-id "$CHAIN_ID" --keyring-backend test \
        --gas 1500000 --fees 60000"$BOND_DENOM" -y --output json 2>&1)
    hash=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$hash" ]; then
        echo "{\"code\": -1, \"raw_log\": $(echo "$res" | jq -Rs .)}"
        return 1
    fi
    code=$(echo "$res" | jq -r '.code // 0')
    if [ "$code" != "0" ]; then
        echo "$res"
        return 1
    fi
    res=$(wait_for_tx "$hash") || { echo "$res"; return 1; }
    echo "$res"
    [ "$(echo "$res" | jq -r '.code')" = "0" ]
}

expect_ok() {
    local desc=$1; shift
    LAST_TX=$(tx "$@")
    if [ $? -eq 0 ]; then
        pass "$desc"
        return 0
    fi
    fail "$desc -- $(echo "$LAST_TX" | jq -r '.raw_log // .' 2>/dev/null | head -c 300)"
    return 1
}

expect_fail() {
    local desc=$1 pattern=$2; shift 2
    LAST_TX=$(tx "$@")
    if [ $? -eq 0 ]; then
        fail "$desc (unexpectedly succeeded)"
        return 1
    fi
    local log
    log=$(echo "$LAST_TX" | jq -r '.raw_log // .' 2>/dev/null)
    if echo "$log" | grep -qiE "$pattern"; then
        pass "$desc"
        return 0
    fi
    fail "$desc -- wrong error: $(echo "$log" | head -c 300)"
    return 1
}

# tx_event <attr> <event-type>: reads an attribute from the last tx.
tx_event() {
    echo "$LAST_TX" | jq -r --arg t "$2" --arg k "$1" \
        '[.events[] | select(.type==$t) | .attributes[] | select(.key==$k) | .value] | first // empty'
}

q() {
    $BINARY query artifact "$@" --output json 2>/dev/null
}

check() {
    local desc=$1 actual=$2 expected=$3
    if [ "$actual" = "$expected" ]; then
        pass "$desc"
    else
        fail "$desc (expected '$expected', got '$actual')"
    fi
}

balance() {
    $BINARY q bank balances "$1" --output json 2>/dev/null | \
        jq -r --arg d "$BOND_DENOM" '[.balances[] | select(.denom==$d) | .amount] | first // "0"'
}

height() {
    $BINARY status 2>/dev/null | jq -r '.sync_info.latest_block_height'
}

block_time_unix() {
    local t
    t=$($BINARY status 2>/dev/null | jq -r '.sync_info.latest_block_time')
    date -d "$t" +%s
}

wait_blocks() {
    local target=$(( $(height) + $1 )) attempt=0
    while [ "$(height)" -lt "$target" ] && [ $attempt -lt 300 ]; do
        sleep 1
        attempt=$((attempt + 1))
    done
}

wait_until_time() {
    local target=$1 attempt=0
    while [ "$(block_time_unix)" -lt "$target" ] && [ $attempt -lt 300 ]; do
        sleep 1
        attempt=$((attempt + 1))
    done
    # One more block so the EndBlocker for that time has run.
    wait_blocks 1
}

finish() {
    echo ""
    echo "  Passed: $PASS_COUNT  Failed: $FAIL_COUNT"
    if [ "$FAIL_COUNT" -gt 0 ]; then
        for f in "${FAILURES[@]}"; do
            echo "    - $f"
        done
        exit 1
    fi
    exit 0
}
