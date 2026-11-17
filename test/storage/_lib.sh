#!/bin/bash
# Shared helpers for the x/storage e2e scripts. Sourced, never run.
#
# Every wait here polls block height or delivered tx codes: the module's
# windows are counted in blocks, and block time drifts over a long run.

BINARY="${BINARY:-sparkdreamd}"
CHAIN_ID="${CHAIN_ID:-sparkdream}"
STORAGE_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$STORAGE_DIR/../lib/denoms.sh"
source "$STORAGE_DIR/../lib/ops_committee.sh"

BACKEND="arweave"
TXFLAGS="--chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --gas 600000 -y --output json"

FAIL_COUNT=0
pass() { echo "  [ OK ] $1"; }
fail() { echo "  [FAIL] $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }

wait_for_tx() {
    local attempt=0 result
    while [ $attempt -lt 30 ]; do
        result=$($BINARY q tx "$1" --output json 2>&1)
        if echo "$result" | jq -e '.code' > /dev/null 2>&1; then
            echo "$result"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    return 1
}

# submit_and_wait <broadcast json>: sets TX_RESULT to the delivered tx (or to
# the broadcast response when the tx never reached a block).
submit_and_wait() {
    local tx_res="$1" hash
    hash=$(echo "$tx_res" | jq -r '.txhash' 2>/dev/null)
    if [ -z "$hash" ] || [ "$hash" == "null" ]; then
        TX_RESULT="$tx_res"
        return 1
    fi
    if [ "$(echo "$tx_res" | jq -r '.code // "0"')" != "0" ]; then
        TX_RESULT="$tx_res"
        return 0
    fi
    TX_RESULT=$(wait_for_tx "$hash")
}
tx_code() { echo "$1" | jq -r '.code // "1"' 2>/dev/null; }
raw_log() { echo "$1" | jq -r '.raw_log // empty' 2>/dev/null; }
event_attr() { # <tx json> <event type> <key>
    echo "$1" | jq -r --arg t "$2" --arg k "$3" \
        '.events[] | select(.type==$t) | .attributes[] | select(.key==$k) | .value' | tr -d '"' | head -1
}

# storage_tx <from> <subcommand> [args...]: broadcast and wait.
storage_tx() {
    local from=$1; shift
    submit_and_wait "$($BINARY tx storage "$@" --from "$from" $TXFLAGS 2>&1)"
}

# expect_ok <label> / expect_err <label> <text>: judge TX_RESULT.
expect_ok() {
    if [ "$(tx_code "$TX_RESULT")" == "0" ]; then
        pass "$1"
        return 0
    fi
    fail "$1: code=$(tx_code "$TX_RESULT") $(raw_log "$TX_RESULT" | head -c 300)"
    return 1
}
expect_err() {
    if [ "$(tx_code "$TX_RESULT")" != "0" ] && { raw_log "$TX_RESULT"; echo "$TX_RESULT"; } | grep -q "$2"; then
        pass "$1"
        return 0
    fi
    fail "$1: expected '$2', got code=$(tx_code "$TX_RESULT") $(raw_log "$TX_RESULT" | head -c 300)"
    return 1
}

height() { $BINARY status 2>/dev/null | jq -r '.sync_info.latest_block_height'; }

# wait_height <h>: block until the chain has passed height h (so the
# EndBlocker of h has run).
wait_height() {
    local target=$1 i
    for i in $(seq 1 600); do
        [ "$(height)" -gt "$target" ] && return 0
        sleep 1
    done
    echo "  timed out waiting for height $target" >&2
    return 1
}

request_json() { $BINARY query storage request "$1" --output json 2>/dev/null; }
# req_field <id> <jq path under .request>; proto3 omits zero values, so a
# missing field reads as the given default.
req_field() { request_json "$1" | jq -r ".request.$2 // \"${3:-}\""; }
req_status() { req_field "$1" status REQUEST_STATUS_UNSPECIFIED; }

# wait_status <id> <status> [max seconds]
wait_status() {
    local id=$1 want=$2 max=${3:-180} i
    for i in $(seq 1 "$max"); do
        [ "$(req_status "$id")" == "$want" ] && return 0
        sleep 1
    done
    return 1
}

balance() { $BINARY query bank balances "$1" --output json 2>/dev/null | jq -r --arg d "$BOND_DENOM" '[.balances[] | select(.denom==$d) | .amount][0] // "0"'; }

# A unique fake file per call: its hash, and a source address for it.
new_file() {
    FILE_TAG="$1-$(date +%s%N)"
    FILE_HASH=$(printf '%s' "$FILE_TAG" | sha256sum | cut -d' ' -f1)
    FILE_URL="https://files.example/$FILE_TAG.png"
}
# locator_for <text>: a well-formed Arweave id (43 base64url characters).
locator_for() { printf '%s' "$1" | sha256sum | cut -c1-43; }
hash_of() { printf '%s' "$1" | sha256sum | cut -d' ' -f1; }

# request_copy <from> [size]: files a request for FILE_HASH; sets REQ_ID.
request_copy() {
    local from=$1 size=${2:-2048}
    storage_tx "$from" request-permanent-copy "$BACKEND" "$FILE_HASH" "$size" image/png "$FILE_URL" 100000000
    REQ_ID=$(event_attr "$TX_RESULT" storage.request_created request_id)
}

# complete <operator key> <id>: a scanned completion with one attestation.
complete() {
    storage_tx "$1" complete-request "$2" "$(locator_for "copy-$2-$FILE_TAG")" \
        --attestations "$(hash_of "verdict-$2-$FILE_TAG")" \
        --verdicts-locator "$(locator_for "verdicts-$2-$FILE_TAG")"
}
