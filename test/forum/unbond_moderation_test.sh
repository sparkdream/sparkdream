#!/bin/bash

# Strict E2E for the sentinel bond-quantity eligibility gate and incremental
# bond modification, proven end-to-end through the real CLI/state:
#
#   1. A sentinel that has queued a PARTIAL unbond (status UNBONDING) keeps its
#      moderation authority as long as the *staying* bond
#      (current_bond - pending_unbond_amount) covers min_sentinel_bond — it can
#      still hide a post. (eligibleSentinel quantity gate.)
#   2. A bond TOP-UP is accepted while an unbond is in flight: current_bond
#      increases immediately, the queued withdrawal keeps maturing on its own
#      schedule (status stays UNBONDING, pending unchanged). Sentinels can
#      bond / unbond / rebond incrementally without waiting out the cooldown.
#
# The OVERTURNED-slash == reserved-committed behavior is covered by Go unit
# tests (msg_server_resolve_gov_action_appeal_test.go) — exercising it here
# would require an appeal+resolve to land inside the 15s testparams
# hidden-expiration window, which is racy. This test stays deterministic:
# hide-succeeds is immediate and there is no appeal/finalization dependency.
#
# Uses the otherwise-unused `moderator` account as a DEDICATED sentinel so it
# never contaminates the shared sentinel1 that other forum tests reuse.
# Depends on the forum account setup (.test_env): `moderator` is invited+funded.

echo "--- TESTING: SENTINEL PARTIAL-UNBOND MODERATION + INCREMENTAL BOND ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing). Run: bash setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"
BOND_DENOM="${BOND_DENOM:-uspark}"

FAILED=0
fail() { echo "  FAIL: $1"; FAILED=1; }
pass() { echo "  PASS: $1"; }

TX_FLAGS="--chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json"

wait_for_tx() {
    local TXHASH=$1 MAX=20 N=0 R
    while [ $N -lt $MAX ]; do
        R=$($BINARY q tx "$TXHASH" --output json 2>&1)
        if echo "$R" | jq -e '.code' >/dev/null 2>&1; then echo "$R"; return 0; fi
        N=$((N + 1)); sleep 1
    done
    echo '{"code":1,"raw_log":"tx not found"}'; return 1
}
check_tx_success() { [ "$(echo "$1" | jq -r '.code // 1')" = "0" ]; }
extract_event_value() {
    echo "$1" | jq -r ".events[]? | select(.type==\"$2\") | .attributes[]? | select(.key==\"$3\") | .value" | tr -d '"'
}
run_tx() {
    local FROM=$1; shift
    local RES TXHASH
    RES=$($BINARY tx "$@" --from "$FROM" $TX_FLAGS 2>&1)
    TXHASH=$(echo "$RES" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$TXHASH" ] || [ "$TXHASH" = "null" ]; then echo "$RES"; return 1; fi
    sleep 3
    wait_for_tx "$TXHASH"
}

# role_field: query a BondedRole field, defaulting to $3. A missing record makes
# the CLI emit a non-JSON error, which jq can't parse — so coerce empty output to
# the fallback (otherwise numeric `-lt` comparisons on an empty string error out
# and silently skip the bond setup).
role_field() {
    local out
    out=$($BINARY query rep bonded-role content-sentinel "$1" --output json 2>/dev/null | jq -r ".bonded_role.$2 // \"$3\"" 2>/dev/null)
    [ -z "$out" ] && out="$3"
    echo "$out"
}
current_bond()   { role_field "$1" current_bond 0; }
bond_status()    { role_field "$1" bond_status unknown; }
pending_unbond() { role_field "$1" pending_unbond_amount 0; }
dream_balance()  {
    local out
    out=$($BINARY query rep get-member "$1" --output json 2>/dev/null | jq -r '.member.dream_balance // "0"' 2>/dev/null)
    [ -z "$out" ] && out="0"
    echo "$out"
}

# bootstrap_reputation <account> <count> — build rep + DREAM via EPIC interims.
bootstrap_reputation() {
    local ACCOUNT=$1 COUNT=$2 i RES IID
    echo "  Bootstrapping $COUNT EPIC interims for $ACCOUNT..."
    for i in $(seq 1 $COUNT); do
        RES=$(run_tx "$ACCOUNT" rep create-interim other 0 "unbondmod-$i-$RANDOM" epic 999999999)
        check_tx_success "$RES" || { echo "    create-interim $i failed"; return 1; }
        IID=$(extract_event_value "$RES" "interim_created" "interim_id")
        RES=$(run_tx "$ACCOUNT" rep complete-interim "$IID" "done")
        check_tx_success "$RES" || { echo "    complete-interim $i failed"; return 1; }
    done
}

BOND_AMT=700000000        # 700 DREAM bonded
UNBOND_AMT=100000000      # withdraw 100 -> 600 staying (>= 500 floor)
TOPUP_AMT=300000000       # top-up 300 while UNBONDING -> 1000 current
DREAM_FUND=2000000000     # 2000 DREAM from alice (~1940 net after 3% tax) — covers
                          # the 700 bond + 300 top-up + headroom. Interim completion
                          # mints negligible DREAM and setup only gives moderator
                          # ~0.1 DREAM, so the bond stake must be funded explicitly
                          # (mirrors how setup funds sentinel1/sentinel2 from alice).

# ========================================================================
# PART 0: BOOTSTRAP + BOND THE DEDICATED SENTINEL
# ========================================================================
echo "--- PART 0: BOOTSTRAP + BOND moderator AS A DEDICATED SENTINEL ---"

CUR=$(current_bond "$MODERATOR_ADDR")
if [ "$CUR" -lt "$BOND_AMT" ] 2>/dev/null; then
    # Fund the bond stake in DREAM from alice (uncapped "bounty" purpose, like
    # setup does for the sentinels). Idempotent enough for re-runs: extra DREAM
    # is harmless.
    echo "  Funding moderator with DREAM from alice..."
    run_tx alice rep transfer-dream "$MODERATOR_ADDR" "$DREAM_FUND" bounty "unbond-mod test funding" >/dev/null
    echo "  moderator dream_balance: $(dream_balance "$MODERATOR_ADDR")"

    # Build rep/trust to clear the bond eligibility gate, then bond. Retry with
    # more rep if the tier gate isn't met yet (sentinel bonding needs ~tier 3).
    GUARD=0
    while [ "$(current_bond "$MODERATOR_ADDR")" -lt "$BOND_AMT" ] 2>/dev/null && [ $GUARD -lt 5 ]; do
        bootstrap_reputation moderator 3 || break
        RES=$(run_tx moderator rep bond-role content-sentinel "$BOND_AMT")
        if check_tx_success "$RES"; then
            pass "moderator bonded $BOND_AMT"
            break
        fi
        echo "  bond attempt failed (likely tier gate), bootstrapping more: $(echo "$RES" | jq -r '.raw_log // .' | head -c 100)"
        GUARD=$((GUARD + 1))
    done
fi
CUR=$(current_bond "$MODERATOR_ADDR")
echo "  moderator current_bond=$CUR status=$(bond_status "$MODERATOR_ADDR")"
[ "$CUR" -ge "$BOND_AMT" ] 2>/dev/null && pass "moderator bonded with headroom" || fail "moderator bond too low ($CUR)"

# ========================================================================
# PART 1: QUEUE A PARTIAL UNBOND -> UNBONDING, STAYING BOND ABOVE FLOOR
# ========================================================================
echo "--- PART 1: PARTIAL UNBOND (staying bond stays above the floor) ---"

if [ "$(bond_status "$MODERATOR_ADDR")" != "BONDED_ROLE_STATUS_UNBONDING" ]; then
    RES=$(run_tx moderator rep unbond-role content-sentinel "$UNBOND_AMT")
    check_tx_success "$RES" && pass "queued partial unbond" || { echo "  $(echo "$RES" | jq -r '.raw_log // .')"; fail "unbond-role"; }
fi
ST=$(bond_status "$MODERATOR_ADDR"); PEND=$(pending_unbond "$MODERATOR_ADDR")
echo "  moderator status=$ST pending=$PEND"
[ "$ST" = "BONDED_ROLE_STATUS_UNBONDING" ] && pass "status flipped to UNBONDING" || fail "expected UNBONDING (got $ST)"
[ "$PEND" = "$UNBOND_AMT" ] && pass "pending_unbond_amount recorded" || fail "pending mismatch (got $PEND)"

# ========================================================================
# PART 2: UNBONDING SENTINEL ABOVE FLOOR CAN STILL HIDE (quantity gate)
# ========================================================================
echo "--- PART 2: PARTIAL-UNBONDING SENTINEL CAN STILL MODERATE ---"

RES=$(run_tx poster1 forum create-post "${TEST_CATEGORY_ID:-1}" "0" "unbond-mod-target-$(date +%s)")
PID=$(extract_event_value "$RES" "post_created" "post_id")
[ -z "$PID" ] && PID=$($BINARY query forum list-post --output json 2>/dev/null | jq -r '.post[-1].id // empty')

if [ -n "$PID" ]; then
    RES=$(run_tx moderator forum hide-post "$PID" "1" "spam (hidden mid-unbond)")
    if check_tx_success "$RES"; then
        REC_SENT=$($BINARY query forum get-hide-record "$PID" --output json 2>/dev/null | jq -r '.hide_record.sentinel // ""')
        [ "$REC_SENT" = "$MODERATOR_ADDR" ] \
            && pass "partial-unbonding sentinel hid the post (quantity gate works)" \
            || fail "hide record sentinel mismatch (got '$REC_SENT')"
    else
        echo "  $(echo "$RES" | jq -r '.raw_log // .')"
        fail "hide rejected for a partial-unbonding sentinel above the floor"
    fi
else
    fail "could not create target post"
fi

# ========================================================================
# PART 3: BOND TOP-UP WHILE UNBONDING (incremental rebond, no 14d wait)
# ========================================================================
echo "--- PART 3: BOND TOP-UP WHILE UNBONDING ---"

# moderator was funded with enough DREAM in PART 0 to cover this top-up.
BOND_BEFORE=$(current_bond "$MODERATOR_ADDR")
RES=$(run_tx moderator rep bond-role content-sentinel "$TOPUP_AMT")
if check_tx_success "$RES"; then
    pass "bond top-up accepted while UNBONDING"
    BOND_AFTER=$(current_bond "$MODERATOR_ADDR")
    ST=$(bond_status "$MODERATOR_ADDR"); PEND=$(pending_unbond "$MODERATOR_ADDR")
    EXPECTED=$((BOND_BEFORE + TOPUP_AMT))
    echo "  current_bond: before=$BOND_BEFORE after=$BOND_AFTER (expected $EXPECTED); status=$ST pending=$PEND"
    [ "$BOND_AFTER" = "$EXPECTED" ] && pass "current_bond increased by the top-up" || fail "current_bond delta wrong (got $BOND_AFTER, want $EXPECTED)"
    [ "$ST" = "BONDED_ROLE_STATUS_UNBONDING" ] && pass "status still UNBONDING (queued withdrawal preserved)" || fail "status changed unexpectedly (got $ST)"
    [ "$PEND" = "$UNBOND_AMT" ] && pass "pending_unbond_amount unchanged by the top-up" || fail "pending changed (got $PEND)"
else
    echo "  $(echo "$RES" | jq -r '.raw_log // .')"
    fail "bond top-up rejected while UNBONDING (the limitation this test guards against)"
fi

# ========================================================================
# PART 4: INCREMENTAL UNBOND WHILE ALREADY UNBONDING (no 14d wait)
# ========================================================================
# Correcting/growing a withdrawal must not require waiting out the first
# unbond's cooldown — a second unbond accumulates into pending_unbond_amount.
echo "--- PART 4: INCREMENTAL UNBOND (accumulates pending) ---"

INCR_AMT=50000000   # add 50 more to the withdrawal
PEND_BEFORE=$(pending_unbond "$MODERATOR_ADDR")
RES=$(run_tx moderator rep unbond-role content-sentinel "$INCR_AMT")
if check_tx_success "$RES"; then
    pass "second unbond accepted while already UNBONDING"
    PEND_AFTER=$(pending_unbond "$MODERATOR_ADDR")
    EXPECTED=$((PEND_BEFORE + INCR_AMT))
    echo "  pending_unbond_amount: before=$PEND_BEFORE after=$PEND_AFTER (expected $EXPECTED)"
    [ "$PEND_AFTER" = "$EXPECTED" ] && pass "pending accumulated by the increment" || fail "pending delta wrong (got $PEND_AFTER, want $EXPECTED)"
    [ "$(bond_status "$MODERATOR_ADDR")" = "BONDED_ROLE_STATUS_UNBONDING" ] && pass "still UNBONDING after increment" || fail "status changed unexpectedly"
else
    echo "  $(echo "$RES" | jq -r '.raw_log // .')"
    fail "incremental unbond rejected (the limitation this test guards against)"
fi

# ========================================================================
# PART 5: CANCEL AN IN-FLIGHT UNBOND (partial, then full -> active again)
# ========================================================================
# A mistyped/over-large withdrawal can be walked back without waiting out the
# cooldown: cancelling reduces pending; cancelling all of it returns the role
# to active status. No DREAM moves (pending was only an earmark).
echo "--- PART 5: CANCEL UNBOND (partial then full) ---"

PEND=$(pending_unbond "$MODERATOR_ADDR")
if [ "$PEND" -gt 0 ] 2>/dev/null; then
    # Partial cancel: give back half (rounded down), still UNBONDING.
    HALF=$((PEND / 2))
    RES=$(run_tx moderator rep cancel-unbond-role content-sentinel "$HALF")
    if check_tx_success "$RES"; then
        pass "partial cancel accepted"
        AFTER=$(pending_unbond "$MODERATOR_ADDR")
        EXP=$((PEND - HALF))
        [ "$AFTER" = "$EXP" ] && pass "pending reduced by the cancelled amount ($PEND -> $AFTER)" || fail "pending after partial cancel wrong (got $AFTER, want $EXP)"
        [ "$(bond_status "$MODERATOR_ADDR")" = "BONDED_ROLE_STATUS_UNBONDING" ] && pass "still UNBONDING after partial cancel" || fail "status changed after partial cancel"

        # Full cancel of the remainder: role returns to active (NORMAL).
        REM=$(pending_unbond "$MODERATOR_ADDR")
        RES=$(run_tx moderator rep cancel-unbond-role content-sentinel "$REM")
        if check_tx_success "$RES"; then
            pass "full cancel of remainder accepted"
            ST=$(bond_status "$MODERATOR_ADDR"); P=$(pending_unbond "$MODERATOR_ADDR")
            echo "  after full cancel: status=$ST pending=$P"
            [ "$P" = "0" ] && pass "pending cleared" || fail "pending not cleared (got $P)"
            [ "$ST" = "BONDED_ROLE_STATUS_NORMAL" ] && pass "role returned to NORMAL (no cooldown wait)" || fail "expected NORMAL after full cancel (got $ST)"
        else
            echo "  $(echo "$RES" | jq -r '.raw_log // .')"
            fail "full cancel rejected"
        fi
    else
        echo "  $(echo "$RES" | jq -r '.raw_log // .')"
        fail "partial cancel rejected (the capability this part guards)"
    fi
else
    fail "no pending unbond to cancel (PART 4 should have left one)"
fi

echo ""
if [ "$FAILED" = "0" ]; then
    echo "--- UNBOND MODERATION TEST: ALL CHECKS PASSED ---"
    exit 0
else
    echo "--- UNBOND MODERATION TEST: FAILURES PRESENT ---"
    exit 1
fi
