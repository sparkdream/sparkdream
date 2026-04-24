#!/bin/bash

echo "--- TESTING: BONDED ROLE (MsgBondRole / MsgUnbondRole + queries) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load the shared rep test env.
REP_ENV="$SCRIPT_DIR/.test_env"
if [ -f "$REP_ENV" ]; then
    source "$REP_ENV"
else
    echo "Test environment not found at $REP_ENV"
    echo "   Run: bash test/rep/setup_test_accounts.sh"
    exit 1
fi

echo "Alice:       $ALICE_ADDR"
echo "Sentinel 1:  $SENTINEL1_ADDR"
echo "Sentinel 2:  $SENTINEL2_ADDR"
echo "Poster 1:    $POSTER1_ADDR  (not a bonded role — used for negative tests)"
echo ""

# ========================================================================
# Helper Functions (shared shape with other rep scripts)
# ========================================================================

wait_for_tx() {
    local TXHASH=$1
    local MAX=20
    local i=0
    while [ $i -lt $MAX ]; do
        local r=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$r" | jq -e '.code' > /dev/null 2>&1; then
            echo "$r"
            return 0
        fi
        i=$((i + 1))
        sleep 1
    done
    echo "ERROR: tx $TXHASH not found" >&2
    return 1
}

# Returns 0 when tx code=0, 1 otherwise. Never prints on success.
check_tx_success() {
    local r=$1
    local code=$(echo "$r" | jq -r '.code')
    if [ "$code" != "0" ]; then
        echo "tx failed code=$code: $(echo "$r" | jq -r '.raw_log' | head -c 240)" >&2
        return 1
    fi
    return 0
}

# Submit a tx, wait, return "ok" or "err:<code>:<first-line-of-raw-log>".
send_tx() {
    local out
    out=$("$@" --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)
    local hash=$(echo "$out" | jq -r '.txhash' 2>/dev/null)
    if [ -z "$hash" ] || [ "$hash" == "null" ]; then
        # Tx rejected before broadcast (e.g. offline validation) — surface the raw CLI error.
        echo "err:broadcast:$(echo "$out" | head -c 240)"
        return 0
    fi
    local r=$(wait_for_tx $hash)
    local code=$(echo "$r" | jq -r '.code')
    if [ "$code" == "0" ]; then
        echo "ok"
    else
        local raw=$(echo "$r" | jq -r '.raw_log' | head -c 240)
        echo "err:$code:$raw"
    fi
}

# Read BondedRole.current_bond (string math.Int).
bonded_current_bond() {
    local role=$1
    local addr=$2
    $BINARY q rep bonded-role "$role" "$addr" --output json 2>/dev/null | \
        jq -r '.bonded_role.current_bond // "0"'
}

# Read BondedRole.bond_status (enum name string).
bonded_status() {
    local role=$1
    local addr=$2
    $BINARY q rep bonded-role "$role" "$addr" --output json 2>/dev/null | \
        jq -r '.bonded_role.bond_status // "MISSING"'
}

# --- Result tracking ---
T1_BOND_HAPPY="FAIL"
T2_UNBOND_PARTIAL="FAIL"
T3_BOND_BELOW_MIN="FAIL"
T4_BOND_INVALID_ROLE="FAIL"
T5_UNBOND_OVER_AVAILABLE="FAIL"
T6_QUERY_CONFIG_ALL_ROLES="FAIL"
T7_QUERY_LIST_BY_TYPE="FAIL"
T8_UNBOND_NOT_BONDED="FAIL"
T9_BOND_NONEXISTENT_ROLE_TYPE="FAIL"
T10_QUERY_UNSPECIFIED_FAILS="FAIL"

# ========================================================================
# Part 1: HAPPY PATH — bond a sentinel (sentinel1 may already be bonded from
# setup; pick the amount delta so we're definitely above zero here).
# ========================================================================
echo "--- PART 1: BOND SENTINEL (happy path) ---"
PRE_BOND=$(bonded_current_bond forum-sentinel $SENTINEL1_ADDR)
echo "  sentinel1 pre-bond current_bond: $PRE_BOND"

# Add 50 DREAM (50_000_000 micro-DREAM) to whatever's already there.
DELTA_AMOUNT="50000000"
RES=$(send_tx $BINARY tx rep bond-role forum-sentinel $DELTA_AMOUNT --from sentinel1)
if [ "$RES" == "ok" ]; then
    POST_BOND=$(bonded_current_bond forum-sentinel $SENTINEL1_ADDR)
    echo "  sentinel1 post-bond current_bond: $POST_BOND"
    EXPECTED=$((${PRE_BOND:-0} + DELTA_AMOUNT))
    if [ "$POST_BOND" == "$EXPECTED" ]; then
        T1_BOND_HAPPY="PASS"
    else
        echo "  expected $EXPECTED, got $POST_BOND"
    fi
else
    echo "  bond failed: $RES"
fi
echo ""

# ========================================================================
# Part 2: UNBOND PARTIAL — take back half of what we just added.
# ========================================================================
echo "--- PART 2: UNBOND PARTIAL (happy path) ---"
BEFORE=$(bonded_current_bond forum-sentinel $SENTINEL1_ADDR)
UNBOND_AMOUNT="25000000"  # half of the 50 DREAM we just added
RES=$(send_tx $BINARY tx rep unbond-role forum-sentinel $UNBOND_AMOUNT --from sentinel1)
if [ "$RES" == "ok" ]; then
    AFTER=$(bonded_current_bond forum-sentinel $SENTINEL1_ADDR)
    EXPECTED=$((${BEFORE:-0} - UNBOND_AMOUNT))
    if [ "$AFTER" == "$EXPECTED" ]; then
        T2_UNBOND_PARTIAL="PASS"
        echo "  $BEFORE → $AFTER (delta -$UNBOND_AMOUNT) ✓"
    else
        echo "  expected $EXPECTED, got $AFTER"
    fi
else
    echo "  unbond failed: $RES"
fi
echo ""

# ========================================================================
# Part 3: BOND BELOW MIN — first-time bond below the seeded min_bond (1000
# DREAM for FORUM_SENTINEL) is rejected with ErrBondAmountTooSmall.
#
# poster1 is not a sentinel and has no prior bond, so this is a first-bond
# attempt. Also seed some reputation via a direct tier check — if the account
# doesn't meet the rep-tier gate the error surfaces ErrInsufficientReputation
# first, which is also an acceptable reject path for this test.
# ========================================================================
echo "--- PART 3: REJECT BELOW-MIN FIRST BOND ---"
# Verify this is a fresh address (no existing role record).
EXISTING=$(bonded_current_bond forum-sentinel $POSTER1_ADDR)
if [ "$EXISTING" == "0" ] || [ "$EXISTING" == "null" ]; then
    # Intentionally below the 1000-DREAM (1_000_000 uDREAM) default min_bond.
    RES=$(send_tx $BINARY tx rep bond-role forum-sentinel 500000 --from poster1)
    if [[ "$RES" == err:* ]]; then
        # Either "bond amount below minimum" or "insufficient reputation" —
        # both are valid rejections for a brand-new low-bond attempt.
        if echo "$RES" | grep -qE "bond amount below minimum|insufficient reputation|insufficient reputation tier"; then
            T3_BOND_BELOW_MIN="PASS"
            echo "  rejected as expected: $(echo "$RES" | head -c 120)"
        else
            echo "  wrong error: $RES"
        fi
    else
        echo "  expected rejection, got: $RES"
    fi
else
    # poster1 is already bonded (test rerun case) — skip gracefully.
    echo "  poster1 already has a bonded role; skipping below-min first-bond test"
    T3_BOND_BELOW_MIN="SKIP"
fi
echo ""

# ========================================================================
# Part 4: INVALID ROLE TYPE — rep rejects unspecified at msg time.
# ========================================================================
echo "--- PART 4: REJECT unspecified ---"
RES=$(send_tx $BINARY tx rep bond-role unspecified 2000000 --from sentinel1)
if [[ "$RES" == err:* ]] && echo "$RES" | grep -qE "invalid role type|role_type must be specified"; then
    T4_BOND_INVALID_ROLE="PASS"
    echo "  rejected as expected: $(echo "$RES" | head -c 120)"
else
    echo "  expected invalid-role-type rejection, got: $RES"
fi
echo ""

# ========================================================================
# Part 5: UNBOND OVER AVAILABLE — try to withdraw more than current_bond.
# ========================================================================
echo "--- PART 5: REJECT UNBOND OVER AVAILABLE ---"
CUR=$(bonded_current_bond forum-sentinel $SENTINEL1_ADDR)
OVER=$((${CUR:-0} + 1000000000))  # current + 1000 DREAM = guaranteed too much
RES=$(send_tx $BINARY tx rep unbond-role forum-sentinel $OVER --from sentinel1)
if [[ "$RES" == err:* ]] && echo "$RES" | grep -qE "insufficient bond|cannot unbond"; then
    T5_UNBOND_OVER_AVAILABLE="PASS"
    echo "  rejected as expected: $(echo "$RES" | head -c 120)"
else
    echo "  expected insufficient-bond rejection, got: $RES"
fi
echo ""

# ========================================================================
# Part 6: CONFIG QUERY — all three role types have a seeded config.
# ========================================================================
echo "--- PART 6: QUERY bonded-role-config FOR EACH ROLE TYPE ---"
PASS=0
TOTAL=0
for role in forum-sentinel collect-curator federation-verifier; do
    TOTAL=$((TOTAL + 1))
    OUT=$($BINARY q rep bonded-role-config $role --output json 2>/dev/null)
    MINBOND=$(echo "$OUT" | jq -r '.bonded_role_config.min_bond // "MISSING"')
    if [ "$MINBOND" != "MISSING" ] && [ "$MINBOND" != "null" ] && [ "$MINBOND" != "" ]; then
        echo "  $role: min_bond=$MINBOND"
        PASS=$((PASS + 1))
    else
        echo "  $role: config MISSING or empty → $OUT"
    fi
done
if [ "$PASS" == "$TOTAL" ]; then
    T6_QUERY_CONFIG_ALL_ROLES="PASS"
fi
echo ""

# ========================================================================
# Part 7: LIST-BY-TYPE — sentinel1 must appear in the FORUM_SENTINEL list.
# ========================================================================
echo "--- PART 7: QUERY bonded-roles-by-type forum-sentinel ---"
LIST=$($BINARY q rep bonded-roles-by-type forum-sentinel --output json 2>/dev/null)
COUNT=$(echo "$LIST" | jq -r '.bonded_roles | length')
FOUND=$(echo "$LIST" | jq -r --arg a "$SENTINEL1_ADDR" '.bonded_roles[] | select(.address==$a) | .address' | head -1)
echo "  list returned $COUNT entries; sentinel1 present: $FOUND"
if [ -n "$FOUND" ] && [ "$COUNT" -ge "1" ]; then
    T7_QUERY_LIST_BY_TYPE="PASS"
fi
echo ""

# ========================================================================
# Part 8: UNBOND WITHOUT BOND — any rando address should get
# ErrBondedRoleNotFound on unbond.
# ========================================================================
echo "--- PART 8: REJECT UNBOND WITH NO BONDED ROLE ---"
# bounty_creator has no forum-sentinel bond.
EXISTING=$(bonded_current_bond forum-sentinel $BOUNTY_CREATOR_ADDR)
if [ "$EXISTING" == "0" ] || [ "$EXISTING" == "null" ]; then
    RES=$(send_tx $BINARY tx rep unbond-role forum-sentinel 100000 --from bounty_creator)
    if [[ "$RES" == err:* ]] && echo "$RES" | grep -qE "bonded role not found|sentinel not found"; then
        T8_UNBOND_NOT_BONDED="PASS"
        echo "  rejected as expected: $(echo "$RES" | head -c 120)"
    else
        echo "  expected not-found rejection, got: $RES"
    fi
else
    echo "  bounty_creator unexpectedly has a sentinel bond; skipping"
    T8_UNBOND_NOT_BONDED="SKIP"
fi
echo ""

# ========================================================================
# Part 9: UNKNOWN ROLE_TYPE ENUM VALUE — a numeric role_type that isn't in
# the enum should be rejected (unparseable at autocli level is itself a
# form of rejection).
# ========================================================================
echo "--- PART 9: REJECT UNKNOWN ROLE_TYPE ENUM VALUE ---"
RES=$(send_tx $BINARY tx rep bond-role role-does-not-exist 1000000 --from sentinel1)
if [[ "$RES" == err:* ]]; then
    T9_BOND_NONEXISTENT_ROLE_TYPE="PASS"
    echo "  rejected as expected: $(echo "$RES" | head -c 120)"
else
    echo "  expected rejection, got: $RES"
fi
echo ""

# ========================================================================
# Part 10: QUERY VALIDATION — UNSPECIFIED role_type must return an error.
# ========================================================================
echo "--- PART 10: QUERY bonded-role WITH unspecified ---"
Q=$($BINARY q rep bonded-role unspecified $SENTINEL1_ADDR --output json 2>&1)
if echo "$Q" | grep -qE "role_type required|InvalidArgument|role_type must be specified"; then
    T10_QUERY_UNSPECIFIED_FAILS="PASS"
    echo "  rejected as expected: $(echo "$Q" | head -c 120)"
else
    echo "  expected rejection, got: $(echo "$Q" | head -c 240)"
fi
echo ""

# ========================================================================
# Summary
# ========================================================================
echo "========================================================================="
echo "  BONDED ROLE TEST RESULTS"
echo "========================================================================="
printf "  %-48s %s\n" "Part 1: bond happy path"                "$T1_BOND_HAPPY"
printf "  %-48s %s\n" "Part 2: unbond partial happy path"      "$T2_UNBOND_PARTIAL"
printf "  %-48s %s\n" "Part 3: reject below-min first bond"    "$T3_BOND_BELOW_MIN"
printf "  %-48s %s\n" "Part 4: reject unspecified"   "$T4_BOND_INVALID_ROLE"
printf "  %-48s %s\n" "Part 5: reject unbond over available"   "$T5_UNBOND_OVER_AVAILABLE"
printf "  %-48s %s\n" "Part 6: config query for all roles"     "$T6_QUERY_CONFIG_ALL_ROLES"
printf "  %-48s %s\n" "Part 7: list-by-type includes bonded"   "$T7_QUERY_LIST_BY_TYPE"
printf "  %-48s %s\n" "Part 8: reject unbond when not bonded"  "$T8_UNBOND_NOT_BONDED"
printf "  %-48s %s\n" "Part 9: reject unknown role_type enum"  "$T9_BOND_NONEXISTENT_ROLE_TYPE"
printf "  %-48s %s\n" "Part 10: reject query w/ UNSPECIFIED"   "$T10_QUERY_UNSPECIFIED_FAILS"
echo ""

FAIL=0
for r in "$T1_BOND_HAPPY" "$T2_UNBOND_PARTIAL" "$T3_BOND_BELOW_MIN" "$T4_BOND_INVALID_ROLE" \
         "$T5_UNBOND_OVER_AVAILABLE" "$T6_QUERY_CONFIG_ALL_ROLES" "$T7_QUERY_LIST_BY_TYPE" \
         "$T8_UNBOND_NOT_BONDED" "$T9_BOND_NONEXISTENT_ROLE_TYPE" "$T10_QUERY_UNSPECIFIED_FAILS"; do
    [ "$r" == "FAIL" ] && FAIL=$((FAIL + 1))
done

if [ $FAIL -eq 0 ]; then
    echo "  ALL BONDED ROLE TESTS PASSED"
    exit 0
else
    echo "  $FAIL FAILURE(S)"
    exit 1
fi
