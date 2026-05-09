#!/bin/bash
# ------------------------------------------------------------------
# Library for multi-chain CLI operations in x/federation E2E tests.
#
# Source this file from any multichain script:
#   source "$SCRIPT_DIR/lib_multichain.sh"
#
# Provides:
#   cli_a / cli_b              tx wrappers (auto --node, --chain-id, --home, --keyring-backend)
#   qcli_a / qcli_b            query wrappers (auto --node, --output json)
#   submit_and_wait_a/_b       submit a tx response, poll for inclusion
#   wait_for_ibc_delivery      poll the destination chain until a jq predicate matches;
#                              nudges hermes mid-timeout to recover from WS subscription drops
#   submit_ops_proposal_a/_b   submit + vote + execute via Commons Council
#   sha256_base64              content hash helper
#   record_result / print_summary  test counters
# ------------------------------------------------------------------

BINARY="${BINARY:-sparkdreamd}"
HERMES="${HERMES:-hermes}"

# Resolve script-relative paths regardless of caller's cwd
LIB_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

CHAIN_A_HOME="$LIB_DIR/data/chain-a"
CHAIN_B_HOME="$LIB_DIR/data/chain-b"

CHAIN_A_ID="fedtest-a"
CHAIN_B_ID="fedtest-b"

CHAIN_A_RPC="tcp://localhost:26657"
CHAIN_B_RPC="tcp://localhost:36657"

HERMES_CONFIG="$LIB_DIR/hermes_config.toml"

# Load IBC channel IDs if present (set by setup_ibc.sh)
IBC_ENV="$LIB_DIR/.ibc_channels"
CHANNEL_A=""
CHANNEL_B=""
[ -f "$IBC_ENV" ] && source "$IBC_ENV"

# Load distinct-chain keys if present (set by setup_chain_keys.sh)
KEYS_ENV="$LIB_DIR/.chain_keys"
LINKER_A_ADDR=""
LINKER_A2_ADDR=""
OWNER_B_ADDR=""
[ -f "$KEYS_ENV" ] && source "$KEYS_ENV"

# -------------------------------------------------------------------
# CLI wrappers — each call targets the right chain
# -------------------------------------------------------------------

cli_a() {
    $BINARY "$@" --node "$CHAIN_A_RPC" --chain-id "$CHAIN_A_ID" \
        --keyring-backend test --home "$CHAIN_A_HOME"
}

cli_b() {
    $BINARY "$@" --node "$CHAIN_B_RPC" --chain-id "$CHAIN_B_ID" \
        --keyring-backend test --home "$CHAIN_B_HOME"
}

qcli_a() {
    $BINARY query "$@" --node "$CHAIN_A_RPC" --output json 2>/dev/null
}

qcli_b() {
    $BINARY query "$@" --node "$CHAIN_B_RPC" --output json 2>/dev/null
}

# Tx-by-hash query wrappers. NOTE: do NOT call cli_a/cli_b for `query tx`,
# because the cli_a wrapper injects `--keyring-backend test --chain-id <id> --home <path>`,
# and Cosmos SDK's `query tx` rejects `--keyring-backend` / `--chain-id` —
# the binary then prints CLI usage to stdout and exits non-zero, which (a) trips
# `set -e` on bare assignments and (b) makes downstream jq filters silently see
# zero events. This was the root cause of the multichain TEST 5 (content) silent
# abort and TEST 1 (reputation) false-FAIL on missing send_packet events.
# These helpers pass *only* the flags `query tx` actually accepts.
qtx_a() {
    $BINARY query tx "$@" --node "$CHAIN_A_RPC" --output json
}

qtx_b() {
    $BINARY query tx "$@" --node "$CHAIN_B_RPC" --output json
}

# Keyring-only operations (no --node, no --chain-id; these flags are
# rejected by `keys` subcommands).
keys_a() {
    $BINARY keys "$@" --keyring-backend test --home "$CHAIN_A_HOME"
}

keys_b() {
    $BINARY keys "$@" --keyring-backend test --home "$CHAIN_B_HOME"
}

# -------------------------------------------------------------------
# Transaction helpers
#
# submit_and_wait <tx_response_json> <label> <node>
#   - returns 0 and sets TX_OK=true / TX_RESULT=<query response> on success
#   - returns 1 and sets TX_RESULT=<original tx response> on failure
# -------------------------------------------------------------------

submit_and_wait() {
    local TX_RES=$1
    local LABEL=${2:-"tx"}
    local NODE=$3

    TX_OK=false
    TX_RESULT="$TX_RES"

    local TXHASH
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$TXHASH" ]; then
        echo "  FAIL: $LABEL - no txhash in response" >&2
        return 1
    fi

    local BCODE
    BCODE=$(echo "$TX_RES" | jq -r '.code // "0"' 2>/dev/null)
    if [ "$BCODE" != "0" ] && [ "$BCODE" != "null" ]; then
        # Broadcast itself rejected (e.g., signature, sequence, fee)
        local RAW
        RAW=$(echo "$TX_RES" | jq -r '.raw_log // empty' 2>/dev/null | head -c 300)
        echo "  FAIL: $LABEL broadcast rejected (code=$BCODE)${RAW:+: $RAW}" >&2
        return 1
    fi

    sleep 2
    local attempt=0
    while [ $attempt -lt 20 ]; do
        local Q
        # `q tx` exits non-zero while the tx is not yet in a block. With set -e
        # in caller scripts, an unguarded `Q=$(... )` capture would abort the
        # script and hide the FAIL message below. `|| true` keeps us alive so
        # we can poll until the tx lands or the timeout fires.
        Q=$($BINARY q tx "$TXHASH" --node "$NODE" --output json 2>&1) || true
        if echo "$Q" | jq -e '.code' >/dev/null 2>&1; then
            local CODE
            CODE=$(echo "$Q" | jq -r '.code')
            TX_RESULT="$Q"
            if [ "$CODE" != "0" ]; then
                local RAW
                RAW=$(echo "$Q" | jq -r '.raw_log // empty' 2>/dev/null | head -c 200)
                echo "  FAIL: $LABEL (code=$CODE)${RAW:+: $RAW}" >&2
                return 1
            fi
            TX_OK=true
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    echo "  FAIL: $LABEL - tx $TXHASH not found after 20 attempts" >&2
    return 1
}

submit_and_wait_a() { submit_and_wait "$1" "$2" "$CHAIN_A_RPC"; }
submit_and_wait_b() { submit_and_wait "$1" "$2" "$CHAIN_B_RPC"; }

# -------------------------------------------------------------------
# IBC delivery polling
#
# wait_for_ibc_delivery <query> <jq_condition> [timeout=30] [nudge_chain] [nudge_channel]
#   <query>         shell command (eval'd) that emits JSON on stdout
#   <jq_condition>  jq -e expression that returns true when delivered
#   <timeout>       seconds to wait (default 30)
#   <nudge_chain>   optional: hermes 'chain' to clear packets on at half-timeout
#   <nudge_channel> optional: channel ID to clear (e.g. channel-0)
#
# Returns 0 on delivery, 1 on timeout. Always restores normal flow control.
# -------------------------------------------------------------------

wait_for_ibc_delivery() {
    local QUERY="$1"
    local JQ_COND="$2"
    local TIMEOUT=${3:-30}
    local NUDGE_CHAIN=${4:-}
    local NUDGE_CHANNEL=${5:-}

    local HALF=$(( TIMEOUT / 2 ))
    local nudged=0
    local i=0

    while [ "$i" -lt "$TIMEOUT" ]; do
        local RAW
        RAW=$(eval "$QUERY" 2>/dev/null || echo "{}")
        if echo "$RAW" | jq -e "$JQ_COND" >/dev/null 2>&1; then
            return 0
        fi

        if [ "$nudged" -eq 0 ] && [ "$i" -ge "$HALF" ] \
                && [ -n "$NUDGE_CHAIN" ] && [ -n "$NUDGE_CHANNEL" ] \
                && command -v "$HERMES" >/dev/null 2>&1; then
            "$HERMES" --config "$HERMES_CONFIG" \
                clear packets --chain "$NUDGE_CHAIN" --port federation --channel "$NUDGE_CHANNEL" \
                >/dev/null 2>&1 || true
            nudged=1
        fi

        i=$((i + 1))
        sleep 1
    done
    echo "  TIMEOUT: IBC delivery did not complete within ${TIMEOUT}s" >&2
    return 1
}

# -------------------------------------------------------------------
# Content hash helper
# -------------------------------------------------------------------

sha256_base64() {
    echo -n "$1" | sha256sum | awk '{print $1}' | xxd -r -p | base64 -w0
}

# -------------------------------------------------------------------
# Governance helpers (Commons Council)
#
# submit_ops_proposal_a <file> <label>
#   - submits a proposal as alice
#   - votes yes from alice + bob
#   - executes
# Sets PROPOSAL_ID. Errors are logged but non-fatal so the orchestrator
# can continue and the test logic can decide if the missing state is fatal.
# -------------------------------------------------------------------

_submit_ops_proposal() {
    local FILE=$1; local LABEL=$2; local CHAIN=$3
    local cli_x submit_x
    if [ "$CHAIN" = "a" ]; then cli_x="cli_a"; submit_x="submit_and_wait_a"
    else                          cli_x="cli_b"; submit_x="submit_and_wait_b"
    fi

    echo "  Submitting $LABEL on chain-$CHAIN..."
    local TX_RES
    # `|| true` keeps set -e callers alive when the CLI fails so we can
    # surface an actionable error below instead of dying silently.
    # Commons Council enforces a flat 5,000,000 uspark minimum proposal fee
    # (anti-spam). Use 6M to give some headroom over any future bumps.
    TX_RES=$($cli_x tx commons submit-proposal "$FILE" \
        --from alice -y --fees 6000000uspark --gas 500000 --output json 2>&1) || true
    if ! echo "$TX_RES" | jq -e '.txhash' >/dev/null 2>&1; then
        echo "  ERROR: $LABEL submit-proposal CLI failed (no txhash)" >&2
        echo "  --- raw output (first 800 chars) ---" >&2
        echo "$TX_RES" | head -c 800 >&2
        echo "" >&2
        echo "  --- end raw output ---" >&2
        return 1
    fi
    if ! $submit_x "$TX_RES" "$LABEL"; then
        return 1
    fi

    PROPOSAL_ID=$(echo "$TX_RESULT" | jq -r \
        '.events[]
         | select(.type=="submit_proposal")
         | .attributes[]
         | select(.key=="proposal_id")
         | .value' 2>/dev/null | tr -d '"' | head -1)
    if [ -z "$PROPOSAL_ID" ]; then
        # Cosmos SDK 0.50+ may emit different event names
        PROPOSAL_ID=$(echo "$TX_RESULT" | jq -r \
            '.events[]
             | select((.type|test("submit_proposal|EventSubmitProposal")) // false)
             | .attributes[]
             | select((.key|test("proposal_id|^id$")) // false)
             | .value' 2>/dev/null | tr -d '"' | head -1)
    fi
    if [ -z "$PROPOSAL_ID" ]; then
        echo "  WARN: could not extract proposal_id; continuing" >&2
        return 1
    fi
    echo "  Proposal ID: $PROPOSAL_ID"

    # Vote yes from alice + bob (separate txs, sleep between).
    # `|| true` on the assignment keeps set -e callers alive even if the
    # vote broadcast itself errors (e.g., already voted). x/commons does
    # early acceptance the moment the threshold is met, so on a 2-member
    # council with a 50% threshold alice's single YES already flips the
    # proposal to ACCEPTED — bob's later YES then errors with "proposal
    # not open for voting". Skip bob's vote in that case to keep the log
    # clean.
    local NODE_ARG HOME_ARG
    if [ "$CHAIN" = "a" ]; then NODE_ARG="$CHAIN_A_RPC"; HOME_ARG="$CHAIN_A_HOME"
    else                          NODE_ARG="$CHAIN_B_RPC"; HOME_ARG="$CHAIN_B_HOME"
    fi
    for VOTER in alice bob; do
        local PROP_STATUS
        PROP_STATUS=$("$BINARY" query commons get-proposal "$PROPOSAL_ID" \
            --node "$NODE_ARG" --home "$HOME_ARG" --output json 2>/dev/null \
            | jq -r '.proposal.status // empty')
        if [ "$PROP_STATUS" = "PROPOSAL_STATUS_ACCEPTED" ] || \
           [ "$PROP_STATUS" = "PROPOSAL_STATUS_REJECTED" ] || \
           [ "$PROP_STATUS" = "PROPOSAL_STATUS_EXECUTED" ]; then
            echo "  skipping $VOTER vote (proposal $PROPOSAL_ID already $PROP_STATUS)"
            continue
        fi
        local V_RES
        V_RES=$($cli_x tx commons vote-proposal "$PROPOSAL_ID" yes \
            --from "$VOTER" -y --fees 5000uspark --output json 2>&1) || true
        $submit_x "$V_RES" "$VOTER vote on $PROPOSAL_ID" || true
        sleep 2
    done

    # Execute
    local E_RES
    E_RES=$($cli_x tx commons execute-proposal "$PROPOSAL_ID" \
        --from alice -y --fees 5000uspark --gas 2000000 --output json 2>&1) || true
    $submit_x "$E_RES" "execute $PROPOSAL_ID" || true
    sleep 2
}

submit_ops_proposal_a() { _submit_ops_proposal "$1" "$2" a; }
submit_ops_proposal_b() { _submit_ops_proposal "$1" "$2" b; }

# -------------------------------------------------------------------
# Test result tracking
# -------------------------------------------------------------------

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1; local RESULT=$2
    TEST_NAMES+=("$NAME"); RESULTS+=("$RESULT")
    case "$RESULT" in
        PASS) PASS_COUNT=$((PASS_COUNT + 1)) ;;
        SKIP) SKIP_COUNT=$((SKIP_COUNT + 1)) ;;
        *)    FAIL_COUNT=$((FAIL_COUNT + 1)) ;;
    esac
    echo "  => $RESULT"
}

print_summary() {
    local TITLE=${1:-"TEST RESULTS"}
    echo ""
    echo "============================================"
    echo "$TITLE"
    echo "============================================"
    for i in "${!TEST_NAMES[@]}"; do
        printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
    done
    echo ""
    local total=$((PASS_COUNT + FAIL_COUNT + SKIP_COUNT))
    if [ "$SKIP_COUNT" -gt 0 ]; then
        echo "  Passed: $PASS_COUNT / $total  (skipped: $SKIP_COUNT)"
    else
        echo "  Passed: $PASS_COUNT / $total"
    fi
    if [ "$FAIL_COUNT" -gt 0 ]; then
        echo ">>> SOME TESTS FAILED <<<"
        return 1
    fi
    echo ">>> ALL TESTS PASSED <<<"
    return 0
}
