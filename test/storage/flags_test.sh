#!/bin/bash

echo "--- TESTING: x/storage FLAGS AND DECLINES (flag, hold, lift, reopen, burn, withholding, declines, prices) ---"
# docs/x-storage-spec.md §3.6, §3.7. Needs the testparams build
# (flag hold 30 blocks, settle 10).

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_lib.sh"
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

COPY_FEE=$($BINARY query storage quote-fee "$BACKEND" image/png 2048 --output json | jq -r '.copy_fee')
flag() { storage_tx storscan1 flag-removed "$1" "$(hash_of "removed-$1-$RANDOM")"; }
unflag() { ops_committee_exec "$(jq -nc --arg h "$1" '{"@type":"/sparkdream.storage.v1.MsgUnflagRemoved",hashes:[$h]}')"; }

# ========================================================================
# PART 1: who may flag; a flag before a claim refunds
# ========================================================================
echo "--- PART 1: flag on a pending request ---"
new_file flag-pending
storage_tx storop1 flag-removed "$FILE_HASH" "$(hash_of x)"
expect_err "an address that is not an approved scanner cannot flag" "scanner is not approved"

BOB_BEFORE=$(balance "$BOB_ADDR")
request_copy bob
A=$REQ_ID
expect_ok "bob requests (request $A)"
flag "$FILE_HASH"
expect_ok "the scanner flags the hash"
if [ "$(req_status "$A")" == "REQUEST_STATUS_REJECTED" ] && [ "$(req_field "$A" closed_reason)" == "CLOSED_REASON_FLAGGED" ] &&
   [ "$(req_field "$A" copy_fee 0)" == "0" ] && [ "$(req_field "$A" forfeit_at 0)" == "0" ]; then
    pass "the pending request was rejected and refunded"
else
    fail "flag-pending state: $(request_json "$A" | jq -c '.request | {status, closed_reason, copy_fee, forfeit_at}')"
fi
flag "$FILE_HASH"
expect_err "a scanner flags a hash once" "already flagged"
storage_tx bob request-permanent-copy "$BACKEND" "$FILE_HASH" 2048 image/png "$FILE_URL" 100000000
expect_err "a flagged hash cannot be requested" "flagged as removed"
N_FLAGS=$($BINARY query storage removed-hash "$FILE_HASH" --output json 2>&1 | jq -r '.removed_hash.flags | length')
if [ "$N_FLAGS" == "1" ]; then
    pass "removed-hash shows the flag"
else
    fail "removed-hash flags: $N_FLAGS"
fi

if unflag "$FILE_HASH"; then
    pass "the scanner's controller lifts the flag"
else
    fail "unflag: $OPS_COMMITTEE_ERROR"
fi
request_copy bob
expect_ok "the file can be requested again"
storage_tx bob cancel-request "$REQ_ID"

# ========================================================================
# PART 2: flag after completion: held, lifted, reopened, settled
# ========================================================================
echo "--- PART 2: flag after completion, then lift ---"
new_file flag-lift
B_HASH=$FILE_HASH
request_copy bob
B=$REQ_ID
storage_tx storop1 claim-request "$B"
complete storop1 "$B"
expect_ok "request $B completed"
storage_tx bob dispute-completion "$B" 100000000
expect_ok "bob disputes it"
flag "$B_HASH"
expect_ok "the scanner flags it"
FORFEIT_AT=$(req_field "$B" forfeit_at 0)
if [ "$(req_status "$B")" == "REQUEST_STATUS_REJECTED" ] && [ "$FORFEIT_AT" -gt 0 ] && [ "$(req_field "$B" copy_fee 0)" == "$COPY_FEE" ]; then
    pass "the fee is held until $FORFEIT_AT, neither refunded nor paid"
else
    fail "flag-completed state: $(request_json "$B" | jq -c '.request | {status, forfeit_at, copy_fee}')"
fi
if unflag "$B_HASH"; then
    pass "the flag is lifted inside the hold"
else
    fail "unflag: $OPS_COMMITTEE_ERROR"
fi
if [ "$(req_status "$B")" == "REQUEST_STATUS_COMPLETED" ] && [ "$(req_field "$B" disputed false)" == "false" ] &&
   [ "$(req_field "$B" forfeit_at 0)" == "0" ]; then
    pass "the completion is reopened and may be disputed again"
else
    fail "reopen state: $(request_json "$B" | jq -c '.request | {status, disputed, forfeit_at}')"
fi
# A flag and a lift must not erase the dispute: bob can hold it again.
storage_tx bob dispute-completion "$B" 100000000
expect_ok "bob disputes the reopened completion"
SETTLE_AT=$(req_field "$B" settle_at 0)
wait_height "$SETTLE_AT"
if wait_status "$B" REQUEST_STATUS_SETTLED 60; then
    pass "the unanswered dispute settled at the end of its window"
else
    fail "status after the dispute window: $(req_status "$B")"
fi

# ========================================================================
# PART 3: a settled copy whose hash is flagged is withheld, not deleted
# ========================================================================
echo "--- PART 3: flag on a settled copy ---"
flag "$B_HASH"
expect_ok "the scanner flags the settled copy's hash"
R=$(request_json "$B")
if [ "$(echo "$R" | jq -r '.request.status')" == "REQUEST_STATUS_SETTLED" ] && [ "$(echo "$R" | jq -r '.withheld // false')" == "true" ] &&
   [ "$(echo "$R" | jq -r '.request.locator // ""')" == "" ] && [ "$(echo "$R" | jq -r '.request.sources[0].url // ""')" == "" ]; then
    pass "queries withhold the locator and the source address"
else
    fail "withheld view: $(echo "$R" | jq -c '{withheld, status: .request.status, locator: .request.locator, sources: .request.sources}')"
fi
LOC=$($BINARY query storage request-location "$B" --output json 2>&1)
if [ -n "$(echo "$LOC" | jq -r '.locator // empty')" ] && [ "$(echo "$LOC" | jq -r '.flagged // false')" == "true" ]; then
    pass "request-location still returns it for reviewers"
else
    fail "request-location: $(echo "$LOC" | head -c 300)"
fi
if ops_committee_exec "$(jq -nc --arg id "$B" '{"@type":"/sparkdream.storage.v1.MsgVoidCompletion",request_id:$id,reason:"x"}')"; then
    fail "a flagged settled copy was voided"
elif echo "$OPS_COMMITTEE_ERROR" | grep -q "flagged as removed"; then
    pass "a flagged hash is never voided back to life"
else
    fail "void of a flagged copy failed for another reason: $OPS_COMMITTEE_ERROR"
fi

# ========================================================================
# PART 4: a flag left standing burns the held fee
# ========================================================================
echo "--- PART 4: flag after a claim, left standing ---"
new_file flag-burn
request_copy carol
C=$REQ_ID
storage_tx storop2 claim-request "$C"
expect_ok "storop2 claims request $C"
flag "$FILE_HASH"
expect_ok "the scanner flags it while claimed"
FORFEIT_AT=$(req_field "$C" forfeit_at 0)
if [ "$FORFEIT_AT" -gt 0 ] && [ "$(req_field "$C" copy_fee 0)" == "$COPY_FEE" ]; then
    pass "the fee of a claimed request is held too (until $FORFEIT_AT)"
else
    fail "flag-claimed state: $(request_json "$C" | jq -c '.request | {status, forfeit_at, copy_fee}')"
fi
wait_height "$FORFEIT_AT"
for i in $(seq 1 30); do
    [ "$(req_field "$C" copy_fee 0)" == "0" ] && break
    sleep 1
done
if [ "$(req_field "$C" copy_fee 0)" == "0" ] && [ "$(req_field "$C" forfeit_at 0)" == "0" ] &&
   [ "$(req_status "$C")" == "REQUEST_STATUS_REJECTED" ]; then
    pass "the held fee was burned at the end of the hold"
else
    fail "burn state: $(request_json "$C" | jq -c '.request | {status, forfeit_at, copy_fee}')"
fi

# ========================================================================
# PART 5: declines
# ========================================================================
echo "--- PART 5: declines ---"
new_file decline
request_copy bob
D=$REQ_ID
storage_tx storop1 decline-request "$D" unreachable
expect_err "an unreachable decline needs the retry period first" "too early"
BOB_BEFORE=$(balance "$BOB_ADDR")
storage_tx storop1 decline-request "$D" mismatch
expect_ok "storop1 declines as a mismatch"
BOB_AFTER=$(balance "$BOB_ADDR")
if [ "$(req_status "$D")" == "REQUEST_STATUS_REJECTED" ] && [ "$(req_field "$D" closed_reason)" == "CLOSED_REASON_DECLINED" ] &&
   [ "$((BOB_AFTER - BOB_BEFORE))" == "$COPY_FEE" ]; then
    pass "one decline closes the request (declines_to_close = 1) and refunds it in full"
else
    fail "decline state: status=$(req_status "$D") reason=$(req_field "$D" closed_reason) bob delta=$((BOB_AFTER - BOB_BEFORE))"
fi

# ========================================================================
# PART 6: prices, inside the governance bounds only
# ========================================================================
echo "--- PART 6: operational params ---"
B_JSON=$($BINARY query storage backend "$BACKEND" --output json)
PRICE=$(echo "$B_JSON" | jq -r '.backend.price_per_byte')
MIN_FEE=$(echo "$B_JSON" | jq -r '.backend.min_fee')
if ops_committee_exec "$(jq -nc --arg p "$PRICE" '{"@type":"/sparkdream.storage.v1.MsgUpdateOperationalParams",backend:"arweave",price_per_byte:$p,min_fee:"1"}')"; then
    fail "a min_fee below the governance floor was accepted"
elif echo "$OPS_COMMITTEE_ERROR" | grep -q "outside the governance bounds"; then
    pass "a price below the governance floor is refused"
else
    fail "price below the floor failed for another reason: $OPS_COMMITTEE_ERROR"
fi
NEW_MIN_FEE=$((MIN_FEE * 2))
if ops_committee_exec "$(jq -nc --arg p "$PRICE" --arg m "$NEW_MIN_FEE" '{"@type":"/sparkdream.storage.v1.MsgUpdateOperationalParams",backend:"arweave",price_per_byte:$p,min_fee:$m}')" &&
   [ "$($BINARY query storage quote-fee "$BACKEND" image/png 2048 --output json | jq -r '.copy_fee')" == "$NEW_MIN_FEE" ]; then
    pass "the Operations Committee raised min_fee to $NEW_MIN_FEE"
else
    fail "price update: $OPS_COMMITTEE_ERROR"
fi
# Put it back for whatever runs next.
ops_committee_exec "$(jq -nc --arg p "$PRICE" --arg m "$MIN_FEE" '{"@type":"/sparkdream.storage.v1.MsgUpdateOperationalParams",backend:"arweave",price_per_byte:$p,min_fee:$m}')" > /dev/null

# ========================================================================
# PART 7: a lift does not restore the completion of an unbonding operator
# ========================================================================
echo "--- PART 7: lift after the operator began to unbond ---"
# Flags can hold a completion for any length of time, so an operator that
# starts to unbond meanwhile could have its bond back before the restored
# completion can be voided. The request goes back to PENDING instead.
# This part leaves storop2 UNBONDING: it runs last in this file, and the
# file needs a fresh setup snapshot to run again.
new_file flag-unbond
request_copy carol
U=$REQ_ID
storage_tx storop2 claim-request "$U"
complete storop2 "$U"
expect_ok "storop2 completes request $U"
flag "$FILE_HASH"
expect_ok "the scanner flags it"
submit_and_wait "$($BINARY tx service unbond-operator storage-arweave --from storop2 $TXFLAGS 2>&1)"
expect_ok "storop2 starts to unbond"
OP_STATUS=$($BINARY query service operator "$STOROP2_ADDR" storage-arweave --output json 2>/dev/null | jq -r '.operator.status // empty')
if [ "$OP_STATUS" == "OPERATOR_STATUS_UNBONDING" ]; then
    pass "storop2 is UNBONDING"
else
    fail "storop2 status: '$OP_STATUS'"
fi
if unflag "$FILE_HASH"; then
    pass "the flag is lifted inside the hold"
else
    fail "unflag: $OPS_COMMITTEE_ERROR"
fi
if [ "$(req_status "$U")" == "REQUEST_STATUS_PENDING" ] && [ -z "$(req_field "$U" operator)" ] &&
   [ -z "$(req_field "$U" locator)" ] && [ "$(req_field "$U" forfeit_at 0)" == "0" ] &&
   [ "$(req_field "$U" copy_fee 0)" == "$COPY_FEE" ]; then
    pass "the request is pending again, fee still in escrow, completion dropped"
else
    fail "lift state: $(request_json "$U" | jq -c '.request | {status, operator, locator, forfeit_at, copy_fee}')"
fi
echo ""
if [ "$FAIL_COUNT" -eq 0 ]; then
    echo "--- x/storage FLAGS AND DECLINES: ALL PASSED ---"
    exit 0
fi
echo "--- x/storage FLAGS AND DECLINES: $FAIL_COUNT FAILED ---"
exit 1
