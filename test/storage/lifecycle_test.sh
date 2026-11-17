#!/bin/bash

echo "--- TESTING: x/storage LIFECYCLE (request, sources, claim, completion, dispute, void, settlement, expiry, lapse) ---"
# docs/x-storage-spec.md §5.1. Needs the testparams build: the windows are a
# few blocks each (request ttl 120, claim 15, settle 10, dispute 30).

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_lib.sh"
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

# ========================================================================
# PART 1: backend, params and a quote
# ========================================================================
echo "--- PART 1: backend, params, quote ---"
B=$($BINARY query storage backend "$BACKEND" --output json 2>&1)
if [ "$(echo "$B" | jq -r '.backend.service_type // empty')" == "storage-arweave" ] &&
   [ "$(echo "$B" | jq -r '.backend.enabled // false')" == "true" ] &&
   [ "$(echo "$B" | jq -r '[.backend.types[] | select(.scanned==true)] | length')" -ge 2 ]; then
    pass "arweave backend seeded with scanned image types"
else
    fail "backend: $(echo "$B" | head -c 300)"
    exit 1
fi
PARAMS=$($BINARY query storage params --output json)
REQUEST_FEE=$(echo "$PARAMS" | jq -r '.params.request_fee')
SETTLE_WINDOW=$(echo "$PARAMS" | jq -r '.params.settle_window_blocks')
DISPUTE_WINDOW=$(echo "$PARAMS" | jq -r '.params.dispute_window_blocks')
CLAIM_WINDOW=$(echo "$PARAMS" | jq -r '.params.claim_window_blocks')
QUOTE=$($BINARY query storage quote-fee "$BACKEND" image/png 2048 --output json 2>&1)
COPY_FEE=$(echo "$QUOTE" | jq -r '.copy_fee // empty')
if [ -n "$COPY_FEE" ] && [ "$(echo "$QUOTE" | jq -r '.total')" == "$((COPY_FEE + REQUEST_FEE))" ]; then
    pass "quote: copy fee $COPY_FEE + request fee $REQUEST_FEE"
else
    fail "quote-fee: $(echo "$QUOTE" | head -c 300)"
    exit 1
fi
N_OPS=$($BINARY query storage approved-operators "$BACKEND" --output json | jq -r '[.operators[] | select(.stale != true)] | length')
N_SCAN=$($BINARY query storage approved-scanners --output json | jq -r '[.scanners[] | select(.stale != true)] | length')
if [ "$N_OPS" -ge 2 ] && [ "$N_SCAN" -ge 1 ]; then
    pass "$N_OPS approved operators, $N_SCAN approved scanner(s)"
else
    fail "approvals: operators=$N_OPS scanners=$N_SCAN"
    exit 1
fi

# ========================================================================
# PART 2: who may request, and what
# ========================================================================
echo "--- PART 2: request validation ---"
new_file reject
storage_tx storscan1 request-permanent-copy "$BACKEND" "$FILE_HASH" 2048 image/png "$FILE_URL" 100000000
expect_err "a non-member cannot request" "not an active member"
storage_tx bob request-permanent-copy "$BACKEND" "$FILE_HASH" 2048 image/gif "$FILE_URL" 100000000
expect_err "an unlisted content type is refused" "content type is not accepted"
storage_tx bob request-permanent-copy "$BACKEND" "$FILE_HASH" 2048 image/png "http://files.example/a.png" 100000000
expect_err "a plain http address is refused" "invalid source address"
storage_tx bob request-permanent-copy "$BACKEND" "$FILE_HASH" 2048 image/png "$FILE_URL" 1
expect_err "max_fee below the total is refused" "fee exceeds max_fee"

# ========================================================================
# PART 3: request, escrow, sources
# ========================================================================
echo "--- PART 3: request and sources ---"
# A request left alone until the end of the script, to watch it expire.
new_file expiring
request_copy bob
EXPIRING_ID=$REQ_ID
EXPIRING_DEADLINE=$(req_field "$EXPIRING_ID" deadline 0)

new_file main
MAIN_HASH=$FILE_HASH
MAIN_TAG=$FILE_TAG
BOB_BEFORE=$(balance "$BOB_ADDR")
request_copy bob
ID=$REQ_ID
if expect_ok "bob requests a permanent copy (request $ID)"; then
    BOB_AFTER=$(balance "$BOB_ADDR")
    SPENT=$((BOB_BEFORE - BOB_AFTER))
    if [ "$(req_status "$ID")" == "REQUEST_STATUS_PENDING" ] && [ "$(req_field "$ID" copy_fee 0)" == "$COPY_FEE" ] &&
       [ "$SPENT" -ge "$((COPY_FEE + REQUEST_FEE))" ]; then
        pass "request is PENDING with $COPY_FEE in escrow (bob paid $SPENT with gas)"
    else
        fail "request state: status=$(req_status "$ID") copy_fee=$(req_field "$ID" copy_fee 0) spent=$SPENT"
    fi
else
    exit 1
fi

storage_tx carol request-permanent-copy "$BACKEND" "$MAIN_HASH" 2048 image/png "https://mirror.example/x.png" 100000000
expect_err "one request per file" "already has an open request"
storage_tx carol add-source "$ID" "https://mirror.example/$MAIN_TAG.png" 100000000
expect_ok "carol adds a source"
storage_tx carol add-source "$ID" "https://third.example/$MAIN_TAG.png" 100000000
expect_err "one source per member" "source already listed"
if [ "$(request_json "$ID" | jq -r '.request.sources | length')" == "2" ]; then
    pass "request lists two sources"
else
    fail "sources: $(request_json "$ID" | jq -c '.request.sources')"
fi

# ========================================================================
# PART 4: claim, extension, completion
# ========================================================================
echo "--- PART 4: claim and completion ---"
storage_tx storscan1 claim-request "$ID"
expect_err "an address that is not an approved operator cannot claim" "not approved"
storage_tx storop1 claim-request "$ID"
expect_ok "storop1 claims"
if [ "$(req_status "$ID")" == "REQUEST_STATUS_CLAIMED" ] && [ "$(req_field "$ID" controller)" == "$OPS_POLICY" ] &&
   [ "$(req_field "$ID" required_attestations 0)" == "1" ]; then
    pass "claim fixed the controller and a quorum of 1"
else
    fail "claim state: $(request_json "$ID" | jq -c '.request | {status, controller, required_attestations}')"
fi
storage_tx bob cancel-request "$ID"
expect_err "a claimed request cannot be cancelled" "not in a status"
storage_tx storop1 extend-claim "$ID"
expect_ok "storop1 extends the claim once"
storage_tx storop1 extend-claim "$ID"
expect_err "a second extension is refused" "already extended"
storage_tx storop1 complete-request "$ID" "$(locator_for "copy-$ID")"
expect_err "a scanned completion needs attestations" "invalid attestations"
FILE_TAG=$MAIN_TAG
complete storop1 "$ID"
expect_ok "storop1 completes"
SETTLE_AT=$(req_field "$ID" settle_at 0)
if [ "$(req_status "$ID")" == "REQUEST_STATUS_COMPLETED" ] && [ "$SETTLE_AT" -gt 0 ]; then
    pass "request is COMPLETED, settles at $SETTLE_AT"
else
    fail "completion state: status=$(req_status "$ID") settle_at=$SETTLE_AT"
fi

# ========================================================================
# PART 5: dispute by another member, void by the controller
# ========================================================================
echo "--- PART 5: dispute and void ---"
storage_tx carol dispute-completion "$ID" 100000000
expect_ok "carol disputes (for the request fee)"
NEW_SETTLE_AT=$(req_field "$ID" settle_at 0)
if [ "$(req_field "$ID" disputed false)" == "true" ] && [ "$NEW_SETTLE_AT" -gt "$SETTLE_AT" ]; then
    pass "settlement held until $NEW_SETTLE_AT"
else
    fail "dispute state: disputed=$(req_field "$ID" disputed false) settle_at=$NEW_SETTLE_AT"
fi
storage_tx bob dispute-completion "$ID" 100000000
expect_err "a completion is disputed once" "already disputed"

# The first settle time passes without a payout.
wait_height "$SETTLE_AT"
if [ "$(req_status "$ID")" == "REQUEST_STATUS_COMPLETED" ]; then
    pass "the disputed completion did not settle at its first settle time"
else
    fail "status after the first settle time: $(req_status "$ID")"
fi

if ops_committee_exec "$(jq -nc --arg id "$ID" '{"@type":"/sparkdream.storage.v1.MsgVoidCompletion",request_id:$id,reason:"locator serves nothing"}')"; then
    pass "the controller voids the completion"
else
    fail "void: $OPS_COMMITTEE_ERROR"
fi
if [ "$(req_status "$ID")" == "REQUEST_STATUS_PENDING" ] && [ "$(req_field "$ID" locator)" == "" ] &&
   [ "$(request_json "$ID" | jq -r '.request.barred | join(",")')" == "$STOROP1_ADDR" ] &&
   [ "$(req_field "$ID" copy_fee 0)" == "$COPY_FEE" ]; then
    pass "request is PENDING again, fee still in escrow, storop1 barred"
else
    fail "void state: $(request_json "$ID" | jq -c '.request | {status, locator, barred, copy_fee}')"
fi
storage_tx storop1 claim-request "$ID"
expect_err "the voided operator cannot claim again" "barred"

# ========================================================================
# PART 6: second operator, settlement, payout
# ========================================================================
echo "--- PART 6: settlement ---"
POLICY_BEFORE=$(balance "$OPS_POLICY")
storage_tx storop2 claim-request "$ID"
expect_ok "storop2 claims"
complete storop2 "$ID"
expect_ok "storop2 completes"
SETTLE_AT=$(req_field "$ID" settle_at 0)
wait_height "$SETTLE_AT"
if wait_status "$ID" REQUEST_STATUS_SETTLED 60; then
    pass "request settled"
else
    fail "status after settle_at: $(req_status "$ID")"
fi
POLICY_AFTER=$(balance "$OPS_POLICY")
if [ "$(req_field "$ID" paid_to)" == "$OPS_POLICY" ] && [ "$(req_field "$ID" paid_amount 0)" == "$COPY_FEE" ] &&
   [ "$((POLICY_AFTER - POLICY_BEFORE))" -ge "$COPY_FEE" ]; then
    pass "the copy fee went to the controller council, not the operator"
else
    fail "payout: paid_to=$(req_field "$ID" paid_to) paid_amount=$(req_field "$ID" paid_amount 0) policy delta=$((POLICY_AFTER - POLICY_BEFORE))"
fi
if [ "$(request_json "$ID" | jq -r '.request.sources | length')" == "1" ] &&
   [ "$(request_json "$ID" | jq -r '.request.sources[0].added_by')" == "$BOB_ADDR" ]; then
    pass "added sources were dropped at settlement"
else
    fail "sources after settlement: $(request_json "$ID" | jq -c '.request.sources')"
fi
BY_HASH=$($BINARY query storage request-by-hash "$BACKEND" "$MAIN_HASH" --output json 2>&1 | jq -r '.request.id // empty')
if [ "$BY_HASH" == "$ID" ]; then
    pass "request-by-hash finds the copy"
else
    fail "request-by-hash returned '$BY_HASH'"
fi
storage_tx carol request-permanent-copy "$BACKEND" "$MAIN_HASH" 2048 image/png "https://mirror.example/y.png" 100000000
expect_err "a stored file is not requested again" "already has a permanent copy"

# ========================================================================
# PART 7: the paid council voids its settled copy and returns the fee
# ========================================================================
echo "--- PART 7: void of a settled copy ---"
BOB_BEFORE=$(balance "$BOB_ADDR")
if ops_committee_exec "$(jq -nc --arg id "$ID" '{"@type":"/sparkdream.storage.v1.MsgVoidCompletion",request_id:$id,reason:"copy never confirmed"}')"; then
    pass "the paid council voids its settled copy"
else
    fail "void of settled copy: $OPS_COMMITTEE_ERROR"
fi
if [ "$(req_status "$ID")" == "REQUEST_STATUS_PENDING" ] && [ "$(req_field "$ID" copy_fee 0)" == "$COPY_FEE" ]; then
    pass "the fee is back in escrow"
else
    fail "state after void: status=$(req_status "$ID") copy_fee=$(req_field "$ID" copy_fee 0)"
fi
storage_tx bob cancel-request "$ID"
expect_ok "bob cancels the reopened request"
BOB_AFTER=$(balance "$BOB_ADDR")
if [ "$(req_status "$ID")" == "REQUEST_STATUS_CANCELLED" ] && [ "$((BOB_AFTER - BOB_BEFORE))" -gt "$((COPY_FEE / 2))" ]; then
    pass "the copy fee was refunded to bob"
else
    fail "cancel: status=$(req_status "$ID") bob delta=$((BOB_AFTER - BOB_BEFORE))"
fi

# ========================================================================
# PART 7b: request-file-copy computes the hash and checks the file profile
# ========================================================================
echo "--- PART 7b: request-file-copy ---"
TMP_DIR=$(mktemp -d)
python3 - "$TMP_DIR" <<'PY'
import struct, sys, zlib, os, time
def chunk(t, d):
    return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
# A 4x4 8-bit greyscale PNG, different on every run.
rows = b"".join(b"\x00" + os.urandom(4) for _ in range(4))
png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 4, 4, 8, 0, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")
open(os.path.join(sys.argv[1], "good.png"), "wb").write(png)
open(os.path.join(sys.argv[1], "trailing.png"), "wb").write(png + b"hidden")
PY
OUT=$($BINARY tx storage request-file-copy "$BACKEND" "$TMP_DIR/trailing.png" image/png "https://files.example/trailing.png" 100000000 --from bob $TXFLAGS 2>&1)
if echo "$OUT" | grep -q "would be declined"; then
    pass "a file with bytes after the image is refused before anything is paid"
else
    fail "request-file-copy on a bad file: $(echo "$OUT" | head -c 300)"
fi
GOOD_HASH=$(sha256sum "$TMP_DIR/good.png" | cut -d' ' -f1)
submit_and_wait "$($BINARY tx storage request-file-copy "$BACKEND" "$TMP_DIR/good.png" image/png "https://files.example/$GOOD_HASH.png" 100000000 --from bob $TXFLAGS 2>&1)"
FILE_ID=$(event_attr "$TX_RESULT" storage.request_created request_id)
if [ "$(tx_code "$TX_RESULT")" == "0" ] && [ "$(req_field "$FILE_ID" sha256)" == "$GOOD_HASH" ] &&
   [ "$(req_field "$FILE_ID" size_bytes 0)" == "$(stat -c %s "$TMP_DIR/good.png")" ]; then
    pass "request-file-copy filed request $FILE_ID with the file's hash and size"
else
    fail "request-file-copy: code=$(tx_code "$TX_RESULT") $(raw_log "$TX_RESULT" | head -c 300)"
fi
storage_tx bob cancel-request "$FILE_ID"
rm -f "$TMP_DIR/good.png" "$TMP_DIR/trailing.png"; rmdir "$TMP_DIR"

# ========================================================================
# PART 8: a claim that lapses
# ========================================================================
echo "--- PART 8: lapse ---"
new_file lapse
request_copy carol
LAPSE_ID=$REQ_ID
storage_tx storop1 claim-request "$LAPSE_ID"
expect_ok "storop1 claims and then does nothing"
CLAIM_END=$(req_field "$LAPSE_ID" claim_expires_at 0)
wait_height "$CLAIM_END"
if wait_status "$LAPSE_ID" REQUEST_STATUS_PENDING 60 &&
   [ "$(request_json "$LAPSE_ID" | jq -r '.request.barred | join(",")')" == "$STOROP1_ADDR" ]; then
    pass "the claim lapsed: request is PENDING, storop1 barred"
else
    fail "lapse state: $(request_json "$LAPSE_ID" | jq -c '.request | {status, barred}')"
fi
STANDING=$($BINARY query storage operator-standing "$BACKEND" "$STOROP1_ADDR" --output json 2>&1)
if [ "$(echo "$STANDING" | jq -r '.lapses.count // "0"')" -ge 1 ] && [ "$(echo "$STANDING" | jq -r '.approved // false')" == "true" ]; then
    pass "operator-standing counts the lapse"
else
    fail "operator-standing: $(echo "$STANDING" | head -c 300)"
fi
storage_tx carol cancel-request "$LAPSE_ID"
expect_ok "carol cancels"

# ========================================================================
# PART 9: expiry of the request nobody took
# ========================================================================
echo "--- PART 9: expiry ---"
wait_height "$EXPIRING_DEADLINE"
if wait_status "$EXPIRING_ID" REQUEST_STATUS_EXPIRED 60 && [ "$(req_field "$EXPIRING_ID" copy_fee 0)" == "0" ]; then
    pass "the untaken request expired and was refunded"
else
    fail "expiry: status=$(req_status "$EXPIRING_ID") copy_fee=$(req_field "$EXPIRING_ID" copy_fee 0)"
fi
storage_tx storop1 claim-request "$EXPIRING_ID"
expect_err "an expired request cannot be claimed" "not in a status"

echo ""
if [ "$FAIL_COUNT" -eq 0 ]; then
    echo "--- x/storage LIFECYCLE: ALL PASSED ---"
    exit 0
fi
echo "--- x/storage LIFECYCLE: $FAIL_COUNT FAILED ---"
exit 1
