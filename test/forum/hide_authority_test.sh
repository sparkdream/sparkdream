#!/bin/bash

echo "--- TESTING: HIDE AUTHORITY DISAMBIGUATION (SENTINEL vs COUNCIL) ---"

# This test exercises MsgHidePost.authority, which disambiguates the case where
# one account is BOTH a bonded forum sentinel AND a Commons Operations Committee
# member. The account under test is ALICE: she is the council authority in the
# forum test harness, and we additionally bond her as a forum sentinel so the
# two roles overlap on a single key.
#
# Contract under test (docs/HANDOFF_HIDE_AUTHORITY_DISAMBIGUATION.md):
#   - AUTO (default) by a sentinel-and-council account -> SENTINEL path
#     (HideRecord.sentinel == caller; is_gov_authority=false).
#   - explicit HIDE_AUTHORITY_COUNCIL by the same account -> GOV path
#     (HideRecord.sentinel == ""; is_gov_authority=true).
#   - explicit HIDE_AUTHORITY_SENTINEL by a non-sentinel -> hard error.
#   - explicit HIDE_AUTHORITY_COUNCIL by a sentinel-only (non-council) -> hard error.

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
if ! $BINARY tx forum hide-post --help 2>&1 | grep -q -- '--authority'; then
    echo "FATAL: 'hide-post' is missing the --authority flag." >&2
    echo "       Rebuild after regenerating proto: ignite generate proto-go -y" >&2
    exit 1
fi

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

# ========================================================================
# PART 0: BOND ALICE AS A FORUM SENTINEL (she is already council)
# ========================================================================
echo "--- PART 0: MAKE ALICE BOTH SENTINEL AND COUNCIL ---"
ALICE_BOND=$($BINARY query rep bonded-role forum-sentinel "$ALICE_ADDR" --output json 2>&1)
if echo "$ALICE_BOND" | grep -q "error\|not found"; then
    bootstrap_reputation alice 3 || { echo "FATAL: failed to bootstrap alice reputation"; exit 1; }
    echo "Bonding alice as forum sentinel..."
    TX_RES=$($BINARY tx rep bond-role forum-sentinel "500000000" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
        echo "FATAL: failed to bond alice as sentinel"; exit 1
    fi
    echo "  Alice bonded as sentinel"
else
    echo "  Alice already bonded as sentinel (skipping)"
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
# sentinel1 is bonded (by sentinel_test.sh / unhide_post_test.sh) but NOT council.
SENTINEL1_BOND=$($BINARY query rep bonded-role forum-sentinel "$SENTINEL1_ADDR" --output json 2>&1)
if echo "$SENTINEL1_BOND" | grep -q "error\|not found"; then
    echo "  sentinel1 not bonded; bonding for this negative case..."
    bootstrap_reputation sentinel1 3 > /dev/null
    TX_RES=$($BINARY tx rep bond-role forum-sentinel "500000000" --from sentinel1 \
        --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
    submit_tx_and_wait "$TX_RES" > /dev/null
fi
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

# ========================================================================
# SUMMARY
# ========================================================================
echo "========================================================================"
echo "  HIDE AUTHORITY TEST SUMMARY"
echo "========================================================================"
echo ""
echo "  AUTO defaults to sentinel path (alice):     $AUTO_SENTINEL_RESULT"
echo "  Explicit COUNCIL opt-in gov path (alice):   $EXPLICIT_COUNCIL_RESULT"
echo "  Force SENTINEL by non-sentinel rejected:    $NEG_FORCE_SENTINEL_RESULT"
echo "  Force COUNCIL by sentinel-only rejected:    $NEG_FORCE_COUNCIL_RESULT"
echo ""

FAIL_COUNT=0
TOTAL_COUNT=0
for RESULT in \
    "$AUTO_SENTINEL_RESULT" "$EXPLICIT_COUNCIL_RESULT" \
    "$NEG_FORCE_SENTINEL_RESULT" "$NEG_FORCE_COUNCIL_RESULT"; do
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
echo "HIDE AUTHORITY TEST COMPLETED"
echo ""
