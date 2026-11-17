#!/bin/bash

echo "--- TESTING: x/storage REFERENCE WORKERS (cmd/storageworker and cmd/contentscan against the chain) ---"
# docs/x-storage-spec.md §10. Runs the worker's own code against this chain:
# real queries, real messages signed with a session key of storop1, a signed
# scanner verdict, and a stand-in for the Arweave gateway (no AR is spent).
# Part 3 runs the scanner worker's flag path the same way.
# Runs last: it replaces storscan1's verdict key.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/../.." && pwd )"
source "$SCRIPT_DIR/_lib.sh"
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

WORK=$(mktemp -d)
SHADE=$(( $(date +%s) % 200 + 20 ))
worker_go() { (cd "$PROJECT_ROOT" && go test -count=1 -v -run "$1" ./cmd/storageworker/ 2>&1); }

# The REST endpoint of this suite's node (run_parallel.sh offsets the port).
APP_TOML="${CHAIN_HOME:-$HOME/.sparkdream}/config/app.toml"
API_ADDR=$(sed -n '/^\[api\]/,/^\[[a-z]/p' "$APP_TOML" | grep -E '^[[:space:]]*address[[:space:]]*=' | head -1 | sed 's|.*tcp://||' | tr -d '"')
LCD="http://${API_ADDR/0.0.0.0/127.0.0.1}"
echo "  REST endpoint: $LCD"

# ========================================================================
# PART 1: the file, the scanner's verdict key, the operator's session key
# ========================================================================
echo "--- PART 1: setup ---"
OUT=$(STW_LIVE_FILE_OUT="$WORK/file.png" STW_LIVE_SHADE=$SHADE worker_go 'TestLiveFile$')
if [ ! -s "$WORK/file.png" ] || [ ! -s "$WORK/file.png.verdictkey" ]; then
    fail "building the test file: $(echo "$OUT" | tail -5)"
    echo "--- x/storage REFERENCE WORKER: $FAIL_COUNT FAILED ---"
    exit 1
fi
FILE_HASH=$(sha256sum "$WORK/file.png" | cut -d' ' -f1)
FILE_SIZE=$(stat -c %s "$WORK/file.png")
FILE_URL="https://files.example/worker-$SHADE-$(date +%s).png"

META=$(printf '{"v":1,"verdict_key":"%s","feed_url":"https://feeds.example/aurora","ruleset_version":"test"}' "$(cat "$WORK/file.png.verdictkey")")
submit_and_wait "$($BINARY tx service update-metadata content-scanner "$(printf '%s' "$META" | base64 -w0)" --from storscan1 $TXFLAGS 2>&1)"
expect_ok "the scanner publishes its verdict key"

KEY_NAME="storworker-$(date +%s)"
$BINARY keys add "$KEY_NAME" --keyring-backend test --output json 2>/dev/null | jq -r '.mnemonic' > "$WORK/session.key"
KEY_ADDR=$($BINARY keys show "$KEY_NAME" -a --keyring-backend test)
submit_and_wait "$($BINARY tx bank send alice "$KEY_ADDR" 1000000${BOND_DENOM} $TXFLAGS 2>&1)"
EXP=$(date -u -d "+2 hours" +"%Y-%m-%dT%H:%M:%SZ")
TYPES="/sparkdream.storage.v1.MsgClaimRequest,/sparkdream.storage.v1.MsgExtendClaim,/sparkdream.storage.v1.MsgCompleteRequest,/sparkdream.storage.v1.MsgDeclineRequest"
submit_and_wait "$($BINARY tx session create-session "$KEY_ADDR" "$TYPES" "50000000${BOND_DENOM}" "$EXP" 100 --from storop1 $TXFLAGS 2>&1)"
expect_ok "storop1 gives the worker a session key for its four messages"

storage_tx bob request-permanent-copy "$BACKEND" "$FILE_HASH" "$FILE_SIZE" image/png "$FILE_URL" 100000000
REQ_ID=$(event_attr "$TX_RESULT" storage.request_created request_id)
expect_ok "bob requests a copy of a real PNG (request $REQ_ID)"

# ========================================================================
# PART 2: the worker claims, stores and completes it
# ========================================================================
echo "--- PART 2: the worker stores the request ---"
OUT=$(STW_LIVE_LCD="$LCD" STW_LIVE_CHAIN_ID="$CHAIN_ID" STW_LIVE_DENOM="$BOND_DENOM" \
      STW_LIVE_SESSION_KEY_FILE="$WORK/session.key" STW_LIVE_GRANTER="$STOROP1_ADDR" \
      STW_LIVE_REQUEST_ID="$REQ_ID" STW_LIVE_SHADE=$SHADE worker_go 'TestLiveChain$')
if echo "$OUT" | grep -q '^ok'; then
    pass "the worker ran against the chain"
else
    fail "worker run: $(echo "$OUT" | tail -40)"
fi

R=$(request_json "$REQ_ID")
if [ "$(echo "$R" | jq -r '.request.status')" == "REQUEST_STATUS_COMPLETED" ] &&
   [ "$(echo "$R" | jq -r '.request.operator')" == "$STOROP1_ADDR" ]; then
    pass "request $REQ_ID is completed by storop1"
else
    fail "request state: $(echo "$R" | jq -c '.request | {status, operator}')"
fi
LOCATOR=$(echo "$R" | jq -r '.request.locator // empty')
VLOC=$(echo "$R" | jq -r '.request.verdicts_locator // empty')
NATT=$(echo "$R" | jq -r '.request.attestations | length')
if [ ${#LOCATOR} -eq 43 ] && [ ${#VLOC} -eq 43 ] && [ "$LOCATOR" != "$VLOC" ] && [ "$NATT" == "1" ]; then
    pass "the completion carries a locator, a verdicts locator and one attestation hash"
else
    fail "completion fields: $(echo "$R" | jq -c '.request | {locator, verdicts_locator, attestations}')"
fi

if wait_status "$REQ_ID" REQUEST_STATUS_SETTLED 240; then
    pass "the worker's completion settles"
else
    fail "request $REQ_ID did not settle: $(req_status "$REQ_ID")"
fi

# ========================================================================
# PART 3: the scanner worker's flag (cmd/contentscan against the chain)
# ========================================================================
echo "--- PART 3: the scanner worker flags a removed hash ---"
# The worker keeps a flag as owed until it reads it back from the chain: an
# accepted broadcast is not a recorded flag. This runs that path with real
# queries and a real MsgFlagRemoved signed with a session key of storscan1.
SCAN_KEY="storscanworker-$(date +%s)"
$BINARY keys add "$SCAN_KEY" --keyring-backend test --output json 2>/dev/null | jq -r '.mnemonic' > "$WORK/scan-session.key"
SCAN_KEY_ADDR=$($BINARY keys show "$SCAN_KEY" -a --keyring-backend test)
submit_and_wait "$($BINARY tx bank send alice "$SCAN_KEY_ADDR" 1000000${BOND_DENOM} $TXFLAGS 2>&1)"
submit_and_wait "$($BINARY tx session create-session "$SCAN_KEY_ADDR" "/sparkdream.storage.v1.MsgFlagRemoved" "50000000${BOND_DENOM}" "$EXP" 100 --from storscan1 $TXFLAGS 2>&1)"
expect_ok "storscan1 gives its worker a session key for MsgFlagRemoved"

new_file scanner-flag
request_copy bob
FLAG_REQ=$REQ_ID
expect_ok "bob requests a copy (request $FLAG_REQ)"
BOB_BEFORE=$(balance "$BOB_ADDR")
OUT=$(cd "$PROJECT_ROOT" && CS_LIVE_LCD="$LCD" CS_LIVE_CHAIN_ID="$CHAIN_ID" CS_LIVE_DENOM="$BOND_DENOM" \
      CS_LIVE_SESSION_KEY_FILE="$WORK/scan-session.key" CS_LIVE_GRANTER="$STORSCAN1_ADDR" \
      CS_LIVE_REQUEST_ID="$FLAG_REQ" CS_LIVE_SHA256="$FILE_HASH" \
      go test -count=1 -v -run 'TestLiveFlag$' ./cmd/contentscan/ 2>&1)
if echo "$OUT" | grep -q '^ok'; then
    pass "the scanner worker sent one flag and read it back from the chain"
else
    fail "scanner worker run: $(echo "$OUT" | tail -40)"
fi
FLAGGER=$($BINARY query storage removed-hash "$FILE_HASH" --output json 2>/dev/null | jq -r '.removed_hash.flags[0].scanner // empty')
if [ "$FLAGGER" == "$STORSCAN1_ADDR" ]; then
    pass "the hash carries storscan1's flag"
else
    fail "flag on $FILE_HASH: scanner='$FLAGGER'"
fi
BOB_AFTER=$(balance "$BOB_ADDR")
if [ "$(req_status "$FLAG_REQ")" == "REQUEST_STATUS_REJECTED" ] &&
   [ "$(req_field "$FLAG_REQ" closed_reason)" == "CLOSED_REASON_FLAGGED" ] && [ "$BOB_AFTER" -gt "$BOB_BEFORE" ]; then
    pass "the pending request is closed as flagged and refunded"
else
    fail "flagged request: status=$(req_status "$FLAG_REQ") reason=$(req_field "$FLAG_REQ" closed_reason) bob delta=$((BOB_AFTER - BOB_BEFORE))"
fi

echo ""
if [ "$FAIL_COUNT" -eq 0 ]; then
    echo "--- x/storage REFERENCE WORKER: ALL PASSED ---"
    exit 0
fi
echo "--- x/storage REFERENCE WORKER: $FAIL_COUNT FAILED ---"
exit 1
