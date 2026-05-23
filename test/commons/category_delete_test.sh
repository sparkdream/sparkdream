#!/bin/bash

echo "--- TESTING: CATEGORIES (DELETE + CREATE-AS-SETUP, EDGE & ERROR PATHS) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment populated by setup_test_accounts.sh:
#   ALICE_ADDR   — gov authority + Commons Operations Committee member
#   POSTER1_ADDR — member, NOT on Operations Committee
#   POSTER2_ADDR — member, NOT on Operations Committee
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# Sanity check: the CLI command we're about to exercise must be registered
# by autocli (in x/commons/module/autocli.go). Without this guard, a missing
# subcommand makes every tx call emit YAML help text — jq then fails to parse
# it, expect_tx_failure interprets that as "rejected at submission", and the
# negative cases all "pass" while the happy paths fail confusingly. Catch it
# up front and exit with a clear message.
if ! $BINARY tx commons --help 2>&1 | grep -q '^[[:space:]]*delete-category'; then
    echo "FATAL: 'sparkdreamd tx commons delete-category' CLI command not found." >&2
    echo "       Rebuild the binary with: ignite chain build -y --build.tags testparams" >&2
    exit 1
fi

echo "Alice:    $ALICE_ADDR"
echo "Poster1:  $POSTER1_ADDR"
echo "Poster2:  $POSTER2_ADDR"
echo ""

# ========================================================================
# Helper Functions (copied verbatim from category_test.sh for consistency)
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
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        eval "$RESULT_VAR=PASS"
        return 0
    else
        echo "  ERROR: Transaction succeeded — $DESC"
        eval "$RESULT_VAR=FAIL"
        return 1
    fi
}

# Helper: create a category as alice, set NEW_CATEGORY_ID. Aborts caller on
# failure by setting NEW_CATEGORY_ID empty.
create_category_as_alice() {
    local TITLE="$1"
    local DESC="$2"
    local MEMBERS_ONLY="${3:-false}"
    local ADMIN_ONLY="${4:-false}"

    TX_RES=$($BINARY tx commons create-category \
        "$TITLE" \
        "$DESC" \
        "$MEMBERS_ONLY" \
        "$ADMIN_ONLY" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        NEW_CATEGORY_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
        if [ -z "$NEW_CATEGORY_ID" ] || [ "$NEW_CATEGORY_ID" == "null" ]; then
            CATEGORIES=$($BINARY query commons list-category --output json 2>&1)
            NEW_CATEGORY_ID=$(echo "$CATEGORIES" | jq -r '.category[-1].category_id // empty')
        fi
    else
        NEW_CATEGORY_ID=""
    fi
}

# Initial state — track count so we can verify cleanup at the end.
INITIAL_COUNT=$($BINARY query commons list-category --output json 2>&1 | jq -r '.category | length' 2>/dev/null || echo "0")
echo "Categories at start of delete suite: $INITIAL_COUNT"
echo ""

# ========================================================================
# PART 1: CREATE + DELETE HAPPY PATH (open category, no posts)
# ========================================================================
echo "--- PART 1: CREATE + DELETE HAPPY PATH ---"

TITLE1="Delete Me Open $(date +%s)"
create_category_as_alice "$TITLE1" "Disposable category" "false" "false"

if [ -n "$NEW_CATEGORY_ID" ]; then
    CAT1_ID=$NEW_CATEGORY_ID
    echo "  Setup: created category $CAT1_ID ($TITLE1)"

    # Confirm visible before deletion
    PRE=$($BINARY query commons get-category $CAT1_ID --output json 2>&1)
    PRE_TITLE=$(echo "$PRE" | jq -r '.category.title // empty')
    if [ "$PRE_TITLE" != "$TITLE1" ]; then
        echo "  ERROR: created category not visible to query"
        DELETE_HAPPY_RESULT="FAIL"
    else
        echo "  Pre-delete query OK ($PRE_TITLE)"

        TX_RES=$($BINARY tx commons delete-category \
            "$CAT1_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)

        if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
            EVT_ID=$(extract_event_value "$TX_RESULT" "category_deleted" "category_id")
            echo "  category_deleted event emitted for ID: ${EVT_ID:-<missing>}"

            POST=$($BINARY query commons get-category $CAT1_ID --output json 2>&1)
            if echo "$POST" | grep -qi "not found\|does not exist\|error"; then
                echo "  Post-delete query correctly returns not-found"
                if [ "$EVT_ID" == "$CAT1_ID" ]; then
                    DELETE_HAPPY_RESULT="PASS"
                else
                    echo "  ERROR: event category_id ($EVT_ID) != deleted ID ($CAT1_ID)"
                    DELETE_HAPPY_RESULT="FAIL"
                fi
            else
                echo "  ERROR: category still queryable after delete"
                DELETE_HAPPY_RESULT="FAIL"
            fi
        else
            echo "  ERROR: delete tx failed"
            DELETE_HAPPY_RESULT="FAIL"
        fi
    fi
else
    echo "  Setup failed — cannot run happy path"
    DELETE_HAPPY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 2: DELETE A MEMBERS-ONLY CATEGORY (flag does not block deletion)
# ========================================================================
echo "--- PART 2: DELETE MEMBERS-ONLY CATEGORY ---"

create_category_as_alice "Delete Me Members $(date +%s)" "Members-only, no posts" "true" "false"

if [ -n "$NEW_CATEGORY_ID" ]; then
    CAT2_ID=$NEW_CATEGORY_ID
    echo "  Setup: created members-only category $CAT2_ID"

    TX_RES=$($BINARY tx commons delete-category \
        "$CAT2_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        POST=$($BINARY query commons get-category $CAT2_ID --output json 2>&1)
        if echo "$POST" | grep -qi "not found\|does not exist\|error"; then
            echo "  Members-only category deleted successfully"
            DELETE_MEMBERS_ONLY_RESULT="PASS"
        else
            echo "  ERROR: members-only category still exists"
            DELETE_MEMBERS_ONLY_RESULT="FAIL"
        fi
    else
        echo "  ERROR: delete tx failed"
        DELETE_MEMBERS_ONLY_RESULT="FAIL"
    fi
else
    echo "  Setup failed"
    DELETE_MEMBERS_ONLY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 3: DELETE AN ADMIN-ONLY CATEGORY (flag does not block deletion)
# ========================================================================
echo "--- PART 3: DELETE ADMIN-ONLY CATEGORY ---"

create_category_as_alice "Delete Me Admin $(date +%s)" "Admin-only, no posts" "false" "true"

if [ -n "$NEW_CATEGORY_ID" ]; then
    CAT3_ID=$NEW_CATEGORY_ID
    echo "  Setup: created admin-only category $CAT3_ID"

    TX_RES=$($BINARY tx commons delete-category \
        "$CAT3_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        POST=$($BINARY query commons get-category $CAT3_ID --output json 2>&1)
        if echo "$POST" | grep -qi "not found\|does not exist\|error"; then
            echo "  Admin-only category deleted successfully"
            DELETE_ADMIN_ONLY_RESULT="PASS"
        else
            echo "  ERROR: admin-only category still exists"
            DELETE_ADMIN_ONLY_RESULT="FAIL"
        fi
    else
        echo "  ERROR: delete tx failed"
        DELETE_ADMIN_ONLY_RESULT="FAIL"
    fi
else
    echo "  Setup failed"
    DELETE_ADMIN_ONLY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 4: VERIFY LIST-CATEGORY COUNT REFLECTS DELETIONS
# ========================================================================
echo "--- PART 4: LIST-CATEGORY REFLECTS DELETIONS ---"

# We created 3 categories and deleted all 3 — net delta should be 0 vs.
# INITIAL_COUNT (modulo other tests creating in parallel, but run_all_tests
# is sequential).
AFTER=$($BINARY query commons list-category --output json 2>&1)
AFTER_COUNT=$(echo "$AFTER" | jq -r '.category | length' 2>/dev/null || echo "-1")

# Belt-and-braces: ensure none of the three IDs appear in the list.
LEAKED=""
for ID in "$CAT1_ID" "$CAT2_ID" "$CAT3_ID"; do
    if [ -n "$ID" ] && echo "$AFTER" | jq -e --argjson id "$ID" '.category[] | select(.category_id == ($id | tostring | tonumber))' > /dev/null 2>&1; then
        LEAKED="$LEAKED $ID"
    fi
done

echo "  Count: started=$INITIAL_COUNT, now=$AFTER_COUNT"
if [ -n "$LEAKED" ]; then
    echo "  ERROR: deleted IDs still in list:$LEAKED"
    LIST_REFLECTS_RESULT="FAIL"
else
    echo "  None of the deleted IDs ($CAT1_ID, $CAT2_ID, $CAT3_ID) appear in list (correct)"
    LIST_REFLECTS_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 5: RE-DELETE THE SAME CATEGORY (idempotency / not-found)
# ========================================================================
echo "--- PART 5: RE-DELETE ALREADY-DELETED CATEGORY ---"

# Reuse CAT1_ID which was successfully deleted in PART 1.
if [ -n "$CAT1_ID" ]; then
    echo "Attempting to delete category $CAT1_ID a second time..."

    TX_RES=$($BINARY tx commons delete-category \
        "$CAT1_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "REDELETE_RESULT" "re-deleted an already-deleted category!"
else
    echo "  CAT1_ID not set, skipping"
    REDELETE_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 6: DELETE REFUSED WHEN CATEGORY HAS FORUM POSTS
# ========================================================================
echo "--- PART 6: DELETE REFUSED IF FORUM POST EXISTS ---"

create_category_as_alice "Has Posts $(date +%s)" "Will get a forum post" "false" "false"

if [ -n "$NEW_CATEGORY_ID" ]; then
    CAT_WITH_POST_ID=$NEW_CATEGORY_ID
    echo "  Setup: created category $CAT_WITH_POST_ID"

    # Attach a forum post (reply-to-id=0 means top-level).
    POST_TX=$($BINARY tx forum create-post \
        "$CAT_WITH_POST_ID" \
        "0" \
        "Post that should block category deletion" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$POST_TX" && check_tx_success "$TX_RESULT"; then
        # Capture the post ID so we can delete it in the cleanup step below.
        BLOCKING_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Setup: attached forum post (id=${BLOCKING_POST_ID:-<unknown>})"

        # Now attempt to delete the category — should be rejected.
        TX_RES=$($BINARY tx commons delete-category \
            "$CAT_WITH_POST_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)

        expect_tx_failure "$TX_RES" "DELETE_WITH_POSTS_RESULT" "category with forum posts was deleted!"

        # Verify the category still exists.
        STILL=$($BINARY query commons get-category $CAT_WITH_POST_ID --output json 2>&1)
        if echo "$STILL" | grep -qi "not found\|does not exist"; then
            echo "  ERROR: category disappeared even though delete should have been rejected"
            DELETE_WITH_POSTS_RESULT="FAIL"
        else
            echo "  Category $CAT_WITH_POST_ID still present (correct)"
        fi

        # ----- Cleanup (idempotency across reruns) ---------------------------
        # Without this, the blocked category + its post would leak each run,
        # making the suite drift over time. As a bonus, this exercises the
        # "delete succeeds once the blocking post is gone" path, which the
        # keeper-level tests in msg_server_delete_category_test.go also cover.
        if [ -n "$BLOCKING_POST_ID" ] && [ "$BLOCKING_POST_ID" != "null" ]; then
            echo "  Cleanup: deleting blocking forum post $BLOCKING_POST_ID..."
            CLEAN_POST_TX=$($BINARY tx forum delete-post \
                "$BLOCKING_POST_ID" \
                --from poster1 \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000${BOND_DENOM} \
                -y \
                --output json 2>&1)
            if submit_tx_and_wait "$CLEAN_POST_TX" && check_tx_success "$TX_RESULT"; then
                echo "  Cleanup: post removed; deleting now-empty category $CAT_WITH_POST_ID..."
                CLEAN_CAT_TX=$($BINARY tx commons delete-category \
                    "$CAT_WITH_POST_ID" \
                    --from alice \
                    --chain-id $CHAIN_ID \
                    --keyring-backend test \
                    --fees 5000${BOND_DENOM} \
                    -y \
                    --output json 2>&1)
                if submit_tx_and_wait "$CLEAN_CAT_TX" && check_tx_success "$TX_RESULT"; then
                    POST_CLEAN=$($BINARY query commons get-category $CAT_WITH_POST_ID --output json 2>&1)
                    if echo "$POST_CLEAN" | grep -qi "not found\|does not exist"; then
                        echo "  Cleanup: category gone (delete succeeds once posts are removed — bonus assertion)"
                        DELETE_AFTER_POST_REMOVED_RESULT="PASS"
                    else
                        echo "  ERROR: category $CAT_WITH_POST_ID still present after post-removal cleanup"
                        DELETE_AFTER_POST_REMOVED_RESULT="FAIL"
                    fi
                else
                    echo "  WARN: cleanup category-delete failed; will leak category $CAT_WITH_POST_ID"
                    DELETE_AFTER_POST_REMOVED_RESULT="FAIL"
                fi
            else
                echo "  WARN: cleanup post-delete failed; will leak category $CAT_WITH_POST_ID + post $BLOCKING_POST_ID"
                DELETE_AFTER_POST_REMOVED_RESULT="FAIL"
            fi
        else
            echo "  WARN: could not capture blocking post id; cleanup skipped (category will leak)"
            DELETE_AFTER_POST_REMOVED_RESULT="FAIL"
        fi
    else
        echo "  Setup: failed to attach forum post — cannot exercise this case"
        DELETE_WITH_POSTS_RESULT="FAIL"
        DELETE_AFTER_POST_REMOVED_RESULT="FAIL"
    fi
else
    echo "  Setup failed"
    DELETE_WITH_POSTS_RESULT="FAIL"
    DELETE_AFTER_POST_REMOVED_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 7: AFTER A SUCCESSFUL DELETE, A FRESH CREATE STILL WORKS
# ========================================================================
echo "--- PART 7: CREATE STILL WORKS AFTER DELETE ---"

create_category_as_alice "Post Delete Create $(date +%s)" "Sanity-check after deletes" "false" "false"

if [ -n "$NEW_CATEGORY_ID" ]; then
    SANITY_ID=$NEW_CATEGORY_ID
    echo "  Created sanity-check category (ID: $SANITY_ID)"

    # Clean it up so the suite leaves no junk behind.
    TX_RES=$($BINARY tx commons delete-category \
        "$SANITY_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    submit_tx_and_wait "$TX_RES" > /dev/null

    CREATE_AFTER_DELETE_RESULT="PASS"
else
    echo "  ERROR: create failed after deletes — module may be in a bad state"
    CREATE_AFTER_DELETE_RESULT="FAIL"
fi

echo ""

# ########################################################################
#
#   NEGATIVE PATH TESTS
#
# ########################################################################

echo "========================================================================"
echo "  NEGATIVE PATH TESTS (DELETE)"
echo "========================================================================"
echo ""

# Prepare a long-lived category that the negative tests will attempt — and
# fail — to delete. It must survive all negative cases so the final cleanup
# can remove it.
create_category_as_alice "Negative Target $(date +%s)" "Survives all neg cases" "false" "false"
NEG_TARGET_ID=$NEW_CATEGORY_ID

if [ -z "$NEG_TARGET_ID" ]; then
    echo "FATAL: could not provision negative-test target category."
    exit 1
fi
echo "Negative-path target category: $NEG_TARGET_ID"
echo ""

# ========================================================================
# NEG 1: DELETE BY MEMBER WHO IS NOT ON OPERATIONS COMMITTEE
# ========================================================================
echo "--- NEG 1: DELETE BY NON-COC MEMBER (poster1) ---"

TX_RES=$($BINARY tx commons delete-category \
    "$NEG_TARGET_ID" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "NEG_MEMBER_NOT_COC_RESULT" "non-COC member deleted a category!"

echo ""

# ========================================================================
# NEG 2: DELETE BY UNFUNDED RANDOM NEW ACCOUNT (non-member)
# ========================================================================
echo "--- NEG 2: DELETE BY NON-MEMBER ---"

# Provision a fresh key + fund it just enough to pay gas; this account is
# neither a member nor on COC, so the authorization check must reject it.
if ! $BINARY keys show nondeleter --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add nondeleter --keyring-backend test --output json > /dev/null 2>&1
fi
NONDELETER_ADDR=$($BINARY keys show nondeleter -a --keyring-backend test)

FUND_TX=$($BINARY tx bank send \
    alice "$NONDELETER_ADDR" \
    1000000${BOND_DENOM} \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
submit_tx_and_wait "$FUND_TX" > /dev/null

TX_RES=$($BINARY tx commons delete-category \
    "$NEG_TARGET_ID" \
    --from nondeleter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "NEG_NON_MEMBER_RESULT" "non-member deleted a category!"

echo ""

# ========================================================================
# NEG 3: DELETE NON-EXISTENT CATEGORY ID
# ========================================================================
echo "--- NEG 3: DELETE NON-EXISTENT ID (999999999) ---"

TX_RES=$($BINARY tx commons delete-category \
    "999999999" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "NEG_NONEXISTENT_ID_RESULT" "delete of non-existent ID succeeded!"

echo ""

# ========================================================================
# NEG 4: DELETE CATEGORY ID 0 (sequences start at 1, so 0 never exists)
# ========================================================================
echo "--- NEG 4: DELETE CATEGORY ID 0 ---"

TX_RES=$($BINARY tx commons delete-category \
    "0" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "NEG_ID_ZERO_RESULT" "delete of category ID 0 succeeded!"

echo ""

# ========================================================================
# NEG 5: VERIFY NEG_TARGET_ID SURVIVED ALL NEGATIVE ATTEMPTS
# ========================================================================
echo "--- NEG 5: NEGATIVE-TARGET CATEGORY STILL ALIVE ---"

CHECK=$($BINARY query commons get-category $NEG_TARGET_ID --output json 2>&1)
if echo "$CHECK" | grep -qi "not found\|does not exist"; then
    echo "  ERROR: negative-target category $NEG_TARGET_ID was deleted unexpectedly!"
    NEG_TARGET_SURVIVED_RESULT="FAIL"
else
    echo "  Category $NEG_TARGET_ID still present after all failed deletes"
    NEG_TARGET_SURVIVED_RESULT="PASS"
fi

echo ""

# Now actually delete it as alice to clean up.
TX_RES=$($BINARY tx commons delete-category \
    "$NEG_TARGET_ID" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
submit_tx_and_wait "$TX_RES" > /dev/null

# ========================================================================
# SUMMARY
# ========================================================================
echo "========================================================================"
echo "  CATEGORY DELETE TEST SUMMARY"
echo "========================================================================"
echo ""
echo "  --- Happy / Edge Path ---"
echo "  Delete open category:                 $DELETE_HAPPY_RESULT"
echo "  Delete members-only:                  $DELETE_MEMBERS_ONLY_RESULT"
echo "  Delete admin-only:                    $DELETE_ADMIN_ONLY_RESULT"
echo "  List reflects deletions:              $LIST_REFLECTS_RESULT"
echo "  Re-delete (idempotency / not-found):  $REDELETE_RESULT"
echo "  Refuse delete when posts exist:       $DELETE_WITH_POSTS_RESULT"
echo "  Delete after blocking post removed:   $DELETE_AFTER_POST_REMOVED_RESULT"
echo "  Create works after deletes:           $CREATE_AFTER_DELETE_RESULT"
echo ""
echo "  --- Negative Path ---"
echo "  Non-COC member can't delete:          $NEG_MEMBER_NOT_COC_RESULT"
echo "  Non-member can't delete:              $NEG_NON_MEMBER_RESULT"
echo "  Non-existent ID rejected:             $NEG_NONEXISTENT_ID_RESULT"
echo "  Category ID 0 rejected:               $NEG_ID_ZERO_RESULT"
echo "  Negative-target survived:             $NEG_TARGET_SURVIVED_RESULT"
echo ""

FAIL_COUNT=0
TOTAL_COUNT=0

for RESULT in \
    "$DELETE_HAPPY_RESULT" "$DELETE_MEMBERS_ONLY_RESULT" "$DELETE_ADMIN_ONLY_RESULT" \
    "$LIST_REFLECTS_RESULT" "$REDELETE_RESULT" "$DELETE_WITH_POSTS_RESULT" \
    "$DELETE_AFTER_POST_REMOVED_RESULT" "$CREATE_AFTER_DELETE_RESULT" \
    "$NEG_MEMBER_NOT_COC_RESULT" "$NEG_NON_MEMBER_RESULT" \
    "$NEG_NONEXISTENT_ID_RESULT" "$NEG_ID_ZERO_RESULT" \
    "$NEG_TARGET_SURVIVED_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

PASS_COUNT=$((TOTAL_COUNT - FAIL_COUNT))

echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
    exit 1
else
    echo "  ALL TESTS PASSED"
fi

echo ""
echo "CATEGORY DELETE TEST COMPLETED"
echo ""
