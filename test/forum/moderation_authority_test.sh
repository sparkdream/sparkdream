#!/bin/bash

echo "--- TESTING: MODERATION AUTHORITY DISAMBIGUATION (SENTINEL vs COUNCIL) ---"

# This test exercises the shared ModerationAuthority field on the three
# sentinel/council moderation messages — MsgHidePost, MsgLockThread,
# MsgMoveThread — which disambiguates the case where one account is BOTH a
# bonded forum sentinel AND a Commons Operations Committee member. The account
# under test is ALICE: she is the council authority in the forum test harness,
# and we additionally bond her as a forum sentinel so the two roles overlap on a
# single key.
#
# Contract under test (docs/HANDOFF_HIDE_AUTHORITY_DISAMBIGUATION.md):
#   - AUTO (default) by a sentinel-and-council account that is ELIGIBLE for the
#     action -> SENTINEL path (writes the sentinel record; is_gov_authority=false).
#   - explicit --authority council by the same account -> GOV path (no/empty
#     sentinel record; is_gov_authority=true).
#   - explicit --authority sentinel by an INELIGIBLE account -> hard error.
#   - AUTO by a council account NOT eligible for the action -> falls through to
#     the GOV path (e.g. lock below the 2x bond floor).
#
# Eligibility differs per action: hide = any NORMAL/RECOVERY bond; lock = that
# plus rep-tier 4 + 2000 DREAM bond + 20000 DREAM backing; move = that plus no
# reserved tag. Alice's 500 DREAM bond is hide/move-eligible but BELOW the lock
# floor, so the lock section uses TWO fixtures: SENTINEL1 (a fully lock-eligible
# sentinel, not council — provisioned in PART 0 with 6 EPIC interims + a 2500
# DREAM bond) carries the POSITIVE sentinel-lock case, while alice (council, not
# lock-eligible) carries the AUTO-fall-through and explicit-sentinel hard-error.
# sentinel1 is funded 25000 DREAM by setup precisely so it clears the 20000
# backing floor (the council member alice cannot, after funding the other test
# accounts — hence the split).

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# Sanity check: the CLI command + the new --authority flag must be registered by
# autocli — otherwise tx calls return YAML help text and every assertion below
# misreads it. (Lesson learned from the category_delete_test.sh rollout.)
if ! $BINARY tx forum --help 2>&1 | grep -q '^[[:space:]]*hide-post'; then
    echo "FATAL: 'sparkdreamd tx forum hide-post' CLI command not found." >&2
    echo "       Rebuild the binary with: ignite chain build -y --build.tags testparams" >&2
    exit 1
fi
for CMD in hide-post lock-thread move-thread; do
    if ! $BINARY tx forum "$CMD" --help 2>&1 | grep -q -- '--authority'; then
        echo "FATAL: 'forum $CMD' is missing the --authority flag." >&2
        echo "       Rebuild after regenerating proto: ignite generate proto-go -y" >&2
        exit 1
    fi
done

echo "Poster 1:   $POSTER1_ADDR"
echo "Sentinel 1: $SENTINEL1_ADDR (bonded, NOT council)"
echo "Sentinel 2: $SENTINEL2_ADDR"
echo "Alice:      $ALICE_ADDR (council authority + bonded sentinel under test)"
echo "Category:   $TEST_CATEGORY_ID"
echo ""

# ========================================================================
# Helper Functions (mirror unhide_post_test.sh / appeals_test.sh)
# ========================================================================

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

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
        return 1
    fi
    return 0
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3
    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT=""
        return 1
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

expect_tx_failure() {
    local TX_RES="$1"
    local RESULT_VAR="$2"
    local DESC="$3"

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        eval "$RESULT_VAR=PASS"
        return 0
    fi
    echo "  Transaction submitted: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        eval "$RESULT_VAR=PASS"
        return 0
    fi
    echo "  ERROR: Transaction succeeded — $DESC"
    eval "$RESULT_VAR=FAIL"
    return 1
}

# Build COUNT EPIC interims for $ACCOUNT (key name) — each grants 100 reputation,
# enough EPICs to clear the tier-3 (200+) floor required to bond as a sentinel.
bootstrap_reputation() {
    local ACCOUNT=$1
    local COUNT=$2
    echo "  Bootstrapping $COUNT EPIC interims for $ACCOUNT..."
    for i in $(seq 1 $COUNT); do
        TX_RES=$($BINARY tx rep create-interim other 0 "hide-authority-$i" epic 999999999 \
            --from $ACCOUNT --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000${BOND_DENOM} -y --output json 2>&1)
        if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
            echo "    Failed to create interim $i"; return 1
        fi
        INTERIM_ID=$(extract_event_value "$TX_RESULT" "interim_created" "interim_id")
        TX_RES=$($BINARY tx rep complete-interim $INTERIM_ID "hide-authority setup" \
            --from $ACCOUNT --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000${BOND_DENOM} -y --output json 2>&1)
        if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
            echo "    Failed to complete interim $i"; return 1
        fi
        echo "    Completed interim $i/$COUNT (ID: $INTERIM_ID)"
    done
}

create_post_as_poster1() {
    local BODY="$1"
    TX_RES=$($BINARY tx forum create-post "$TEST_CATEGORY_ID" "0" "$BODY" \
        --from poster1 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
        POST_ID=""; return 1
    fi
    POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    if [ -z "$POST_ID" ] || [ "$POST_ID" == "null" ]; then
        POSTS=$($BINARY query forum list-post --output json 2>&1)
        POST_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
    fi
    return 0
}

# Returns the HideRecord.sentinel (empty string == gov-hide marker) via jq. Uses
# `// ""` because proto3 omits the empty sentinel field from JSON entirely.
hide_record_sentinel() {
    local POST_ID="$1"
    $BINARY query forum get-hide-record "$POST_ID" --output json 2>&1 \
        | jq -r '.hide_record.sentinel // ""'
}

# Returns the ThreadLockRecord.sentinel, or "" when no record exists (a gov lock
# writes no record at all — the council path is recordless).
lock_record_sentinel() {
    local ROOT_ID="$1"
    local OUT
    OUT=$($BINARY query forum get-thread-lock-record "$ROOT_ID" --output json 2>&1)
    if echo "$OUT" | grep -qiE "not found|does not exist|key not found"; then
        echo ""
    else
        echo "$OUT" | jq -r '.thread_lock_record.sentinel // ""'
    fi
}

# Returns the ThreadMoveRecord.sentinel, or "" when no record exists (gov move).
move_record_sentinel() {
    local ROOT_ID="$1"
    local OUT
    OUT=$($BINARY query forum get-thread-move-record "$ROOT_ID" --output json 2>&1)
    if echo "$OUT" | grep -qiE "not found|does not exist|key not found"; then
        echo ""
    else
        echo "$OUT" | jq -r '.thread_move_record.sentinel // ""'
    fi
}

# Picks a destination category id different from the thread's current one
# (category 1 is the test default; fall back to 2). Threads are created in
# $TEST_CATEGORY_ID, so move to the first category id that differs.
MOVE_DEST_CATEGORY=$([ "$TEST_CATEGORY_ID" = "2" ] && echo "1" || echo "2")

# Returns the bond_status of a forum-sentinel bonded role, or "" if unbonded.
sentinel_bond_status() {
    local ADDR="$1"
    local OUT
    OUT=$($BINARY query rep bonded-role forum-sentinel "$ADDR" --output json 2>&1)
    if echo "$OUT" | grep -q "error\|not found"; then
        echo ""
    else
        echo "$OUT" | jq -r '.bonded_role.current_bond // "0"'
    fi
}

# ========================================================================
# PART 0: PROVISION THE TWO ROLE-OVERLAP FIXTURES
#
#   ALICE     — council authority + a 500 DREAM sentinel bond. That bond is
#               enough for hide and move (no extra floor) but BELOW the
#               2000 DREAM lock floor, so alice is NOT lock-eligible. This is
#               deliberate: it lets the lock section exercise the dual-role
#               AUTO fall-through and the explicit-sentinel hard error.
#   SENTINEL1 — a fully lock-eligible sentinel (tier 4 via 6 EPIC interims +
#               2500 DREAM bond + its 25000 DREAM funding clears the 20000
#               backing floor) but NOT council. It carries the POSITIVE
#               sentinel-lock case: AUTO resolves to the sentinel path because
#               it is eligible. setup funds sentinel1 specifically for this.
# ========================================================================
echo "--- PART 0: PROVISION ALICE (council) AND SENTINEL1 (lock-eligible) ---"

# Alice: council + a small sentinel bond (hide/move eligible, lock-ineligible).
if [ -z "$(sentinel_bond_status "$ALICE_ADDR")" ] || [ "$(sentinel_bond_status "$ALICE_ADDR")" = "0" ]; then
    bootstrap_reputation alice 3 || { echo "FATAL: failed to bootstrap alice reputation"; exit 1; }
    echo "Bonding alice as forum sentinel (500 DREAM, sub-lock-floor)..."
    TX_RES=$($BINARY tx rep bond-role forum-sentinel "500000000" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
        echo "FATAL: failed to bond alice as sentinel"; exit 1
    fi
    echo "  Alice bonded (500 DREAM)"
else
    echo "  Alice already bonded as sentinel (skipping)"
fi

# Sentinel1: lock-eligible. Needs tier 4 (6 EPIC interims = 600 rep) AND a
# >= 2000 DREAM bond. The 25000 DREAM setup funding clears the 20000 backing
# floor (bonding locks DREAM but does NOT reduce the backing balance). In the
# full suite sentinel_test already makes sentinel1 lock-eligible, so this block
# is skipped; standalone it runs.
S1_BOND=$(sentinel_bond_status "$SENTINEL1_ADDR")
S1_BOND_OK=0
if [ -n "$S1_BOND" ] && [ "$S1_BOND" != "0" ] && [ "$S1_BOND" -ge 2000000000 ] 2>/dev/null; then
    S1_BOND_OK=1
fi
if [ "$S1_BOND_OK" = "0" ]; then
    echo "Provisioning sentinel1 to lock-eligibility (tier 4 + 2500 DREAM bond)..."
    bootstrap_reputation sentinel1 6 || { echo "FATAL: failed to bootstrap sentinel1 reputation"; exit 1; }
    TX_RES=$($BINARY tx rep bond-role forum-sentinel "2500000000" \
        --from sentinel1 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
        echo "FATAL: failed to bond sentinel1 for lock eligibility"; exit 1
    fi
    echo "  Sentinel1 provisioned (2500 DREAM bond)"
else
    echo "  Sentinel1 already lock-eligible (bond=$S1_BOND, skipping)"
fi
echo ""

# ========================================================================
# PART 1: AUTO HIDE BY ALICE -> SENTINEL PATH (accountable default)
# ========================================================================
echo "--- PART 1: AUTO HIDE BY SENTINEL-AND-COUNCIL ALICE -> SENTINEL PATH ---"
create_post_as_poster1 "Auto-hide default path $(date +%s)"
if [ -z "$POST_ID" ]; then
    echo "  Setup failed: could not create post"; AUTO_SENTINEL_RESULT="FAIL"
else
    # No --authority flag => AUTO. Must resolve to the sentinel path.
    TX_RES=$($BINARY tx forum hide-post "$POST_ID" "1" "auto default" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        IS_GOV=$(extract_event_value "$TX_RESULT" "post_hidden" "is_gov_authority")
        SENTINEL=$(hide_record_sentinel "$POST_ID")
        echo "  is_gov_authority=$IS_GOV record.sentinel=$SENTINEL (expect alice)"
        if [ "$IS_GOV" = "false" ] && [ "$SENTINEL" = "$ALICE_ADDR" ]; then
            AUTO_SENTINEL_RESULT="PASS"
        else
            echo "  ERROR: AUTO did not default to the accountable sentinel path"
            AUTO_SENTINEL_RESULT="FAIL"
        fi
        # Cleanup: unhide so the post slot is reusable.
        TX_RES=$($BINARY tx forum unhide-post "$POST_ID" --from alice \
            --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
        submit_tx_and_wait "$TX_RES" > /dev/null
    else
        echo "  ERROR: AUTO hide tx failed"; AUTO_SENTINEL_RESULT="FAIL"
    fi
fi
echo ""

# ========================================================================
# PART 2: EXPLICIT COUNCIL HIDE BY ALICE -> GOV PATH (deliberate opt-in)
# ========================================================================
echo "--- PART 2: EXPLICIT COUNCIL HIDE BY ALICE -> GOV PATH ---"
create_post_as_poster1 "Council opt-in path $(date +%s)"
if [ -z "$POST_ID" ]; then
    echo "  Setup failed: could not create post"; EXPLICIT_COUNCIL_RESULT="FAIL"
else
    TX_RES=$($BINARY tx forum hide-post "$POST_ID" "1" "act as committee" \
        --authority council \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        IS_GOV=$(extract_event_value "$TX_RESULT" "post_hidden" "is_gov_authority")
        SENTINEL=$(hide_record_sentinel "$POST_ID")
        echo "  is_gov_authority=$IS_GOV record.sentinel='$SENTINEL' (expect empty gov marker)"
        if [ "$IS_GOV" = "true" ] && [ -z "$SENTINEL" ]; then
            EXPLICIT_COUNCIL_RESULT="PASS"
        else
            echo "  ERROR: explicit COUNCIL did not take the gov path"
            EXPLICIT_COUNCIL_RESULT="FAIL"
        fi
        TX_RES=$($BINARY tx forum unhide-post "$POST_ID" --from alice \
            --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
        submit_tx_and_wait "$TX_RES" > /dev/null
    else
        echo "  ERROR: explicit COUNCIL hide tx failed"; EXPLICIT_COUNCIL_RESULT="FAIL"
    fi
fi
echo ""

# ========================================================================
# PART 3 (NEGATIVE): EXPLICIT SENTINEL BY A NON-SENTINEL -> HARD ERROR
# ========================================================================
echo "--- PART 3 (NEG): EXPLICIT SENTINEL BY NON-SENTINEL poster2 ---"
create_post_as_poster1 "Force-sentinel by non-sentinel $(date +%s)"
if [ -z "$POST_ID" ]; then
    NEG_FORCE_SENTINEL_RESULT="FAIL"
else
    TX_RES=$($BINARY tx forum hide-post "$POST_ID" "1" "should fail" \
        --authority sentinel \
        --from poster2 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    expect_tx_failure "$TX_RES" "NEG_FORCE_SENTINEL_RESULT" "non-sentinel forced a sentinel hide!"
fi
echo ""

# ========================================================================
# PART 4 (NEGATIVE): EXPLICIT COUNCIL BY A SENTINEL-ONLY ACCOUNT -> HARD ERROR
# ========================================================================
echo "--- PART 4 (NEG): EXPLICIT COUNCIL BY SENTINEL-ONLY sentinel1 ---"
# sentinel1 is bonded (PART 0) but NOT council.
create_post_as_poster1 "Force-council by sentinel-only $(date +%s)"
if [ -z "$POST_ID" ]; then
    NEG_FORCE_COUNCIL_RESULT="FAIL"
else
    TX_RES=$($BINARY tx forum hide-post "$POST_ID" "1" "should fail" \
        --authority council \
        --from sentinel1 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    expect_tx_failure "$TX_RES" "NEG_FORCE_COUNCIL_RESULT" "sentinel-only account forced a council hide!"
fi
echo ""

# ########################################################################
#
#   MOVE-THREAD AUTHORITY (alice is move-eligible: any NORMAL bond, no
#   reserved tag — her 500 DREAM bond suffices since move has no bond floor)
#
# ########################################################################

# ========================================================================
# PART 5: AUTO MOVE BY ALICE -> SENTINEL PATH (writes a move record)
# ========================================================================
echo "--- PART 5: AUTO MOVE BY SENTINEL-AND-COUNCIL ALICE -> SENTINEL PATH ---"
create_post_as_poster1 "Auto-move default path $(date +%s)"
if [ -z "$POST_ID" ]; then
    echo "  Setup failed: could not create thread"; AUTO_MOVE_RESULT="FAIL"
else
    TX_RES=$($BINARY tx forum move-thread "$POST_ID" "$MOVE_DEST_CATEGORY" "auto move" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        IS_GOV=$(extract_event_value "$TX_RESULT" "thread_moved" "is_gov_authority")
        SENTINEL=$(move_record_sentinel "$POST_ID")
        echo "  is_gov_authority=$IS_GOV record.sentinel=$SENTINEL (expect alice)"
        if [ "$IS_GOV" = "false" ] && [ "$SENTINEL" = "$ALICE_ADDR" ]; then
            AUTO_MOVE_RESULT="PASS"
        else
            echo "  ERROR: AUTO move did not default to the sentinel path"
            AUTO_MOVE_RESULT="FAIL"
        fi
    else
        echo "  ERROR: AUTO move tx failed"; AUTO_MOVE_RESULT="FAIL"
    fi
fi
echo ""

# ========================================================================
# PART 6: EXPLICIT COUNCIL MOVE BY ALICE -> GOV PATH (no move record)
# ========================================================================
echo "--- PART 6: EXPLICIT COUNCIL MOVE BY ALICE -> GOV PATH ---"
create_post_as_poster1 "Council move opt-in $(date +%s)"
if [ -z "$POST_ID" ]; then
    echo "  Setup failed: could not create thread"; EXPLICIT_COUNCIL_MOVE_RESULT="FAIL"
else
    TX_RES=$($BINARY tx forum move-thread "$POST_ID" "$MOVE_DEST_CATEGORY" "act as committee" \
        --authority council \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        IS_GOV=$(extract_event_value "$TX_RESULT" "thread_moved" "is_gov_authority")
        SENTINEL=$(move_record_sentinel "$POST_ID")
        echo "  is_gov_authority=$IS_GOV record.sentinel='$SENTINEL' (expect empty / no record)"
        if [ "$IS_GOV" = "true" ] && [ -z "$SENTINEL" ]; then
            EXPLICIT_COUNCIL_MOVE_RESULT="PASS"
        else
            echo "  ERROR: explicit COUNCIL move did not take the gov path"
            EXPLICIT_COUNCIL_MOVE_RESULT="FAIL"
        fi
    else
        echo "  ERROR: explicit COUNCIL move tx failed"; EXPLICIT_COUNCIL_MOVE_RESULT="FAIL"
    fi
fi
echo ""

# ########################################################################
#
#   LOCK-THREAD AUTHORITY. Locking needs rep tier 4 AND a 2000 DREAM bond
#   AND 20000 DREAM backing. SENTINEL1 (provisioned in PART 0) meets all
#   three and carries the POSITIVE sentinel-lock case. ALICE is council but
#   holds only a 500 DREAM bond, so she is NOT lock-eligible — she carries
#   the dual-role AUTO fall-through and explicit-sentinel hard error.
#
# ########################################################################

# ========================================================================
# PART 7: AUTO LOCK BY LOCK-ELIGIBLE SENTINEL1 -> SENTINEL PATH
# ========================================================================
echo "--- PART 7: AUTO LOCK BY LOCK-ELIGIBLE SENTINEL1 -> SENTINEL PATH ---"
create_post_as_poster1 "Auto-lock sentinel path $(date +%s)"
if [ -z "$POST_ID" ]; then
    echo "  Setup failed: could not create thread"; AUTO_LOCK_SENTINEL_RESULT="FAIL"
else
    # No --authority => AUTO. sentinel1 is lock-eligible, so AUTO must resolve to
    # the sentinel path: a ThreadLockRecord with sentinel == sentinel1.
    TX_RES=$($BINARY tx forum lock-thread "$POST_ID" "auto sentinel lock" \
        --from sentinel1 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        IS_GOV=$(extract_event_value "$TX_RESULT" "thread_locked" "is_gov_authority")
        SENTINEL=$(lock_record_sentinel "$POST_ID")
        echo "  is_gov_authority=$IS_GOV record.sentinel=$SENTINEL (expect sentinel1)"
        if [ "$IS_GOV" = "false" ] && [ "$SENTINEL" = "$SENTINEL1_ADDR" ]; then
            AUTO_LOCK_SENTINEL_RESULT="PASS"
        else
            echo "  ERROR: AUTO lock did not take the sentinel path"
            AUTO_LOCK_SENTINEL_RESULT="FAIL"
        fi
    else
        echo "  ERROR: AUTO sentinel lock tx failed"; AUTO_LOCK_SENTINEL_RESULT="FAIL"
    fi
fi
echo ""

# ========================================================================
# PART 8: AUTO LOCK BY ALICE (council, not lock-eligible) -> GOV FALL-THROUGH
# ========================================================================
echo "--- PART 8: AUTO LOCK BY ALICE (not lock-eligible) -> GOV FALL-THROUGH ---"
create_post_as_poster1 "Auto-lock fall-through $(date +%s)"
if [ -z "$POST_ID" ]; then
    echo "  Setup failed: could not create thread"; AUTO_LOCK_FALLTHROUGH_RESULT="FAIL"
else
    TX_RES=$($BINARY tx forum lock-thread "$POST_ID" "auto lock" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        IS_GOV=$(extract_event_value "$TX_RESULT" "thread_locked" "is_gov_authority")
        SENTINEL=$(lock_record_sentinel "$POST_ID")
        echo "  is_gov_authority=$IS_GOV record.sentinel='$SENTINEL' (expect gov, no record)"
        # Alice's 500 DREAM bond is below the 2000 DREAM lock floor, so AUTO must
        # fall through to the council path: gov lock, no sentinel lock record.
        if [ "$IS_GOV" = "true" ] && [ -z "$SENTINEL" ]; then
            AUTO_LOCK_FALLTHROUGH_RESULT="PASS"
        else
            echo "  ERROR: AUTO lock did not fall through to the gov path"
            AUTO_LOCK_FALLTHROUGH_RESULT="FAIL"
        fi
    else
        echo "  ERROR: AUTO lock tx failed"; AUTO_LOCK_FALLTHROUGH_RESULT="FAIL"
    fi
fi
echo ""

# ========================================================================
# PART 9 (NEG): EXPLICIT SENTINEL LOCK WHEN NOT ELIGIBLE -> HARD ERROR
# ========================================================================
echo "--- PART 9 (NEG): EXPLICIT SENTINEL LOCK BY ALICE (not lock-eligible) ---"
create_post_as_poster1 "Force-sentinel lock not eligible $(date +%s)"
if [ -z "$POST_ID" ]; then
    NEG_LOCK_FORCE_SENTINEL_RESULT="FAIL"
else
    # Even though alice is council, forcing SENTINEL must hard-error on a lock
    # eligibility gate (rep-tier / bond floor) rather than silently downgrading
    # to a gov lock.
    TX_RES=$($BINARY tx forum lock-thread "$POST_ID" "should fail" \
        --authority sentinel \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    expect_tx_failure "$TX_RES" "NEG_LOCK_FORCE_SENTINEL_RESULT" "lock-ineligible sentinel forced a lock!"
fi
echo ""

# ========================================================================
# PART 10 (NEG): EXPLICIT COUNCIL LOCK BY A SENTINEL-ONLY ACCOUNT -> HARD ERROR
# ========================================================================
echo "--- PART 10 (NEG): EXPLICIT COUNCIL LOCK BY SENTINEL-ONLY sentinel1 ---"
create_post_as_poster1 "Force-council lock by sentinel-only $(date +%s)"
if [ -z "$POST_ID" ]; then
    NEG_LOCK_FORCE_COUNCIL_RESULT="FAIL"
else
    TX_RES=$($BINARY tx forum lock-thread "$POST_ID" "should fail" \
        --authority council \
        --from sentinel1 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    expect_tx_failure "$TX_RES" "NEG_LOCK_FORCE_COUNCIL_RESULT" "sentinel-only account forced a council lock!"
fi
echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "========================================================================"
echo "  MODERATION AUTHORITY TEST SUMMARY"
echo "========================================================================"
echo ""
echo "  --- Hide ---"
echo "  AUTO defaults to sentinel path (alice):     $AUTO_SENTINEL_RESULT"
echo "  Explicit COUNCIL opt-in gov path (alice):   $EXPLICIT_COUNCIL_RESULT"
echo "  Force SENTINEL by non-sentinel rejected:    $NEG_FORCE_SENTINEL_RESULT"
echo "  Force COUNCIL by sentinel-only rejected:    $NEG_FORCE_COUNCIL_RESULT"
echo ""
echo "  --- Move ---"
echo "  AUTO defaults to sentinel path (alice):     $AUTO_MOVE_RESULT"
echo "  Explicit COUNCIL opt-in gov path (alice):   $EXPLICIT_COUNCIL_MOVE_RESULT"
echo ""
echo "  --- Lock ---"
echo "  AUTO -> sentinel path (lock-eligible s1):    $AUTO_LOCK_SENTINEL_RESULT"
echo "  AUTO falls through to council (ineligible):  $AUTO_LOCK_FALLTHROUGH_RESULT"
echo "  Force SENTINEL when ineligible rejected:     $NEG_LOCK_FORCE_SENTINEL_RESULT"
echo "  Force COUNCIL by sentinel-only rejected:     $NEG_LOCK_FORCE_COUNCIL_RESULT"
echo ""

FAIL_COUNT=0
TOTAL_COUNT=0
for RESULT in \
    "$AUTO_SENTINEL_RESULT" "$EXPLICIT_COUNCIL_RESULT" \
    "$NEG_FORCE_SENTINEL_RESULT" "$NEG_FORCE_COUNCIL_RESULT" \
    "$AUTO_MOVE_RESULT" "$EXPLICIT_COUNCIL_MOVE_RESULT" \
    "$AUTO_LOCK_SENTINEL_RESULT" "$AUTO_LOCK_FALLTHROUGH_RESULT" \
    "$NEG_LOCK_FORCE_SENTINEL_RESULT" "$NEG_LOCK_FORCE_COUNCIL_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" = "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done
PASS_COUNT=$((TOTAL_COUNT - FAIL_COUNT))

echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
    exit 1
fi

echo "  ALL TESTS PASSED"
echo ""
echo "MODERATION AUTHORITY TEST COMPLETED"
echo ""
