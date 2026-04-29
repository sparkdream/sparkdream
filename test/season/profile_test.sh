#!/bin/bash

echo "--- TESTING: MEMBER PROFILES (DISPLAY NAME, USERNAME, DISPLAY TITLE) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Test Accounts:"
echo "  Display User: $DISPLAY_USER_ADDR"
echo "  Alice:        $ALICE_ADDR"
echo "  Bob:          $BOB_ADDR"
echo "  Carol:        $CAROL_ADDR"
echo ""

# ========================================================================
# Helper Functions
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

FAILURES=0
PASSES=0

pass() {
    echo "  PASS: $1"
    PASSES=$((PASSES + 1))
}

fail() {
    echo "  FAIL: $1"
    FAILURES=$((FAILURES + 1))
}

assert_equals() {
    local LABEL=$1
    local EXPECTED=$2
    local ACTUAL=$3

    if [ "$EXPECTED" == "$ACTUAL" ]; then
        pass "$LABEL (=$ACTUAL)"
    else
        fail "$LABEL (expected=$EXPECTED, actual=$ACTUAL)"
    fi
}

# ========================================================================
# PART 1: CHECK INITIAL PROFILE STATE
# ========================================================================
echo "--- PART 1: CHECK INITIAL PROFILE STATE ---"

PROFILE_INFO=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)

if echo "$PROFILE_INFO" | grep -q "not found"; then
    echo "  Note: x/season profiles are created when member is registered in x/rep"
    echo "  display_user profile not yet created (will be created on first action)"
    pass "Initial profile state checked for display_user"
else
    DISPLAY_NAME=$(echo "$PROFILE_INFO" | jq -r '.member_profile.display_name // "not set"')
    USERNAME=$(echo "$PROFILE_INFO" | jq -r '.member_profile.username // "not set"')
    DISPLAY_TITLE=$(echo "$PROFILE_INFO" | jq -r '.member_profile.display_title // "not set"')
    SEASON_XP=$(echo "$PROFILE_INFO" | jq -r '.member_profile.season_xp // "0"')
    SEASON_LEVEL=$(echo "$PROFILE_INFO" | jq -r '.member_profile.season_level // "0"')

    echo "  Display Name: $DISPLAY_NAME"
    echo "  Username: $USERNAME"
    echo "  Display Title: $DISPLAY_TITLE"
    echo "  Season XP: $SEASON_XP"
    echo "  Season Level: $SEASON_LEVEL"
    pass "Profile query succeeded"
fi

echo ""

# ========================================================================
# PART 2: SET DISPLAY NAME
# ========================================================================
echo "--- PART 2: SET DISPLAY NAME ---"

DISPLAY_NAME_TO_SET="CryptoEnthusiast_$(date +%s)"
echo "Setting display name to: $DISPLAY_NAME_TO_SET"

TX_RES=$($BINARY tx season set-display-name \
    "$DISPLAY_NAME_TO_SET" \
    --from display_user \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
TX_CODE=$(echo "$TX_RES" | jq -r '.code // 0')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    fail "Set display name - no txhash"
    echo "  $TX_RES"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        # Verify the change
        PROFILE_INFO=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)
        NEW_DISPLAY_NAME=$(echo "$PROFILE_INFO" | jq -r '.member_profile.display_name // "not set"')

        assert_equals "Display name set correctly" "$DISPLAY_NAME_TO_SET" "$NEW_DISPLAY_NAME"
    else
        fail "Set display name transaction failed"
    fi
fi

echo ""

# ========================================================================
# PART 3: TEST DISPLAY NAME VALIDATION
# ========================================================================
echo "--- PART 3: TEST DISPLAY NAME VALIDATION ---"

# Test empty display name (should fail)
echo "Testing empty display name (should fail)..."

TX_RES=$($BINARY tx season set-display-name \
    "" \
    --from display_user \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    pass "Empty display name rejected at broadcast"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    TX_CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$TX_CODE" != "0" ]; then
        pass "Empty display name rejected on-chain"
    else
        fail "Empty display name was accepted (should have been rejected)"
    fi
fi

echo ""

# ========================================================================
# PART 4: SET USERNAME
# ========================================================================
echo "--- PART 4: SET USERNAME ---"

USERNAME_TO_SET="testuser$(date +%s)"
echo "Setting username to: $USERNAME_TO_SET"

TX_RES=$($BINARY tx season set-username \
    "$USERNAME_TO_SET" \
    --from display_user \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    fail "Set username - no txhash"
    echo "  $TX_RES"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        # Verify the change
        PROFILE_INFO=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)
        NEW_USERNAME=$(echo "$PROFILE_INFO" | jq -r '.member_profile.username // "not set"')

        assert_equals "Username set correctly" "$USERNAME_TO_SET" "$NEW_USERNAME"
    else
        fail "Set username transaction failed"
    fi
fi

echo ""

# ========================================================================
# PART 5: QUERY PROFILES
# ========================================================================
echo "--- PART 5: QUERY PROFILES ---"

echo "Listing all member profiles..."

PROFILES=$($BINARY query season list-member-profile --output json 2>&1)

if echo "$PROFILES" | grep -q "error"; then
    fail "List profiles query failed"
    echo "  $PROFILES"
else
    PROFILE_COUNT=$(echo "$PROFILES" | jq -r '.member_profile | length' 2>/dev/null || echo "0")
    echo "  Total profiles: $PROFILE_COUNT"

    if [ "$PROFILE_COUNT" -gt 0 ]; then
        pass "List profiles returned $PROFILE_COUNT profiles"
        echo ""
        echo "  Sample profiles:"
        echo "$PROFILES" | jq -r '.member_profile[0:3] | .[] | "    - \(.address) (Name: \(.display_name // "none"), XP: \(.season_xp // 0))"' 2>/dev/null
    else
        fail "List profiles returned 0 profiles"
    fi
fi

echo ""

# ========================================================================
# PART 6: QUERY BY DISPLAY NAME
# ========================================================================
echo "--- PART 6: QUERY BY DISPLAY NAME ---"

echo "Querying member by display name: $DISPLAY_NAME_TO_SET"

MEMBER_RESULT=$($BINARY query season member-by-display-name "$DISPLAY_NAME_TO_SET" --output json 2>&1)

if echo "$MEMBER_RESULT" | grep -q "error\|not found"; then
    fail "Query by display name returned no result"
else
    FOUND_ADDR=$(echo "$MEMBER_RESULT" | jq -r '.member_profile.address // .address // "unknown"')

    assert_equals "Query by display name found correct member" "$DISPLAY_USER_ADDR" "$FOUND_ADDR"
fi

echo ""

# ========================================================================
# PART 7: CHECK AVAILABLE TITLES
# ========================================================================
echo "--- PART 7: CHECK AVAILABLE TITLES ---"

TITLES=$($BINARY query season titles --output json 2>&1)

if echo "$TITLES" | grep -q "error"; then
    fail "Titles query failed"
else
    TITLE_COUNT=$(echo "$TITLES" | jq -r '.titles | length' 2>/dev/null || echo "0")
    echo "  Available titles: $TITLE_COUNT"

    if [ "$TITLE_COUNT" -gt 0 ]; then
        pass "Titles query returned $TITLE_COUNT titles"
        echo ""
        echo "  Title examples:"
        echo "$TITLES" | jq -r '.titles[0:3] | .[] | "    - \(.title_id): \(.name) (\(.rarity))"' 2>/dev/null
    else
        fail "Titles query returned 0 titles"
    fi
fi

echo ""

# ========================================================================
# PART 8: CHECK AVAILABLE ACHIEVEMENTS
# ========================================================================
echo "--- PART 8: CHECK AVAILABLE ACHIEVEMENTS ---"

ACHIEVEMENTS=$($BINARY query season achievements --output json 2>&1)

if echo "$ACHIEVEMENTS" | grep -q "error"; then
    fail "Achievements query failed"
else
    ACH_COUNT=$(echo "$ACHIEVEMENTS" | jq -r '.achievements | length' 2>/dev/null || echo "0")
    echo "  Available achievements: $ACH_COUNT"

    if [ "$ACH_COUNT" -gt 0 ]; then
        pass "Achievements query returned $ACH_COUNT achievements"
        echo ""
        echo "  Achievement examples:"
        echo "$ACHIEVEMENTS" | jq -r '.achievements[0:3] | .[] | "    - \(.achievement_id): \(.name) (XP: \(.xp_reward))"' 2>/dev/null
    else
        fail "Achievements query returned 0 achievements"
    fi
fi

echo ""

# ========================================================================
# PART 9: VERIFY GENESIS PROFILES (ALICE, BOB, CAROL)
# ========================================================================
echo "--- PART 9: VERIFY GENESIS PROFILES ---"

# Test Alice (high level, multiple achievements/titles)
echo "Verifying Alice's profile..."
ALICE_PROFILE=$($BINARY query season get-member-profile $ALICE_ADDR --output json 2>&1)

if echo "$ALICE_PROFILE" | grep -q "not found"; then
    fail "Alice profile not found"
else
    ALICE_XP=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.season_xp // "0"')
    ALICE_LEVEL=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.season_level // "0"')
    ALICE_ACH=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.achievements | length' 2>/dev/null || echo "0")
    ALICE_TITLES=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.unlocked_titles | length' 2>/dev/null || echo "0")
    ALICE_DISPLAY=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.display_title // "none"')

    assert_equals "Alice season XP" "5000" "$ALICE_XP"
    assert_equals "Alice season level" "8" "$ALICE_LEVEL"
    assert_equals "Alice achievements count" "3" "$ALICE_ACH"
    assert_equals "Alice unlocked titles count" "3" "$ALICE_TITLES"
    assert_equals "Alice display title" "veteran" "$ALICE_DISPLAY"
fi

echo ""

# Test Bob (medium level)
echo "Verifying Bob's profile..."
BOB_PROFILE=$($BINARY query season get-member-profile $BOB_ADDR --output json 2>&1)

if echo "$BOB_PROFILE" | grep -q "not found"; then
    fail "Bob profile not found"
else
    BOB_XP=$(echo "$BOB_PROFILE" | jq -r '.member_profile.season_xp // "0"')
    BOB_LEVEL=$(echo "$BOB_PROFILE" | jq -r '.member_profile.season_level // "0"')
    BOB_ACH=$(echo "$BOB_PROFILE" | jq -r '.member_profile.achievements | length' 2>/dev/null || echo "0")
    BOB_TITLES=$(echo "$BOB_PROFILE" | jq -r '.member_profile.unlocked_titles | length' 2>/dev/null || echo "0")

    assert_equals "Bob season XP" "1500" "$BOB_XP"
    assert_equals "Bob season level" "6" "$BOB_LEVEL"
    assert_equals "Bob achievements count" "2" "$BOB_ACH"
    assert_equals "Bob unlocked titles count" "1" "$BOB_TITLES"
fi

echo ""

# Test Carol (low level)
echo "Verifying Carol's profile..."
CAROL_PROFILE=$($BINARY query season get-member-profile $CAROL_ADDR --output json 2>&1)

if echo "$CAROL_PROFILE" | grep -q "not found"; then
    fail "Carol profile not found"
else
    CAROL_XP=$(echo "$CAROL_PROFILE" | jq -r '.member_profile.season_xp // "0"')
    CAROL_LEVEL=$(echo "$CAROL_PROFILE" | jq -r '.member_profile.season_level // "0"')
    CAROL_ACH=$(echo "$CAROL_PROFILE" | jq -r '.member_profile.achievements | length' 2>/dev/null || echo "0")
    CAROL_TITLES=$(echo "$CAROL_PROFILE" | jq -r '.member_profile.unlocked_titles | length' 2>/dev/null || echo "0")

    assert_equals "Carol season XP" "300" "$CAROL_XP"
    assert_equals "Carol season level" "2" "$CAROL_LEVEL"
    assert_equals "Carol achievements count" "1" "$CAROL_ACH"
    assert_equals "Carol unlocked titles count" "1" "$CAROL_TITLES"
fi

echo ""

# ========================================================================
# PART 10: TEST CHANGING ALICE'S DISPLAY TITLE
# ========================================================================
echo "--- PART 10: TEST CHANGING DISPLAY TITLE (ALICE) ---"

# Alice has multiple titles unlocked, test changing between them
CURRENT_TITLE=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.display_title // "none"')

# Get a title different from the current one
SECOND_TITLE=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.unlocked_titles[0] // "none"')
if [ "$SECOND_TITLE" == "$CURRENT_TITLE" ]; then
    SECOND_TITLE=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.unlocked_titles[1] // "none"')
fi

if [ "$SECOND_TITLE" != "none" ] && [ "$SECOND_TITLE" != "null" ] && [ -n "$SECOND_TITLE" ]; then
    echo "Alice's current title: $CURRENT_TITLE"
    echo "Changing Alice's title to: $SECOND_TITLE"

    TX_RES=$($BINARY tx season set-display-title \
        "$SECOND_TITLE" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        fail "Set display title - no txhash"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            # Verify the change
            ALICE_PROFILE=$($BINARY query season get-member-profile $ALICE_ADDR --output json 2>&1)
            NEW_TITLE=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.display_title // "not set"')

            assert_equals "Alice display title changed" "$SECOND_TITLE" "$NEW_TITLE"
        else
            fail "Set display title transaction failed"
        fi
    fi
else
    fail "Alice doesn't have a second title to change to"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- PROFILE TEST SUMMARY ---"
echo ""
echo "  Passed: $PASSES"
echo "  Failed: $FAILURES"
echo ""

if [ "$FAILURES" -gt 0 ]; then
    echo "RESULT: $FAILURES ASSERTION(S) FAILED"
    exit 1
else
    echo "RESULT: ALL $PASSES TESTS PASSED"
fi

echo ""
echo "PROFILE TEST COMPLETED"
echo ""
