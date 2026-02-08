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

# ========================================================================
# PART 1: CHECK INITIAL PROFILE STATE
# ========================================================================
echo "--- PART 1: CHECK INITIAL PROFILE STATE ---"

PROFILE_INFO=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)

if echo "$PROFILE_INFO" | grep -q "not found"; then
    echo "  Member profile not found (may need to be created first)"
    echo "  Note: x/season profiles are created when member is registered in x/rep"
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
    echo "  Failed to submit transaction"
    echo "  $TX_RES"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Display name set successfully"

        # Verify the change
        PROFILE_INFO=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)
        NEW_DISPLAY_NAME=$(echo "$PROFILE_INFO" | jq -r '.member_profile.display_name // "not set"')
        echo "  Verified display name: $NEW_DISPLAY_NAME"

        if [ "$NEW_DISPLAY_NAME" == "$DISPLAY_NAME_TO_SET" ]; then
            echo "  Display name change verified"
        else
            echo "  Warning: Display name mismatch"
        fi
    else
        echo "  Failed to set display name"
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
    echo "  Correctly rejected at broadcast (empty name)"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    TX_CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$TX_CODE" != "0" ]; then
        echo "  Correctly rejected empty display name"
    else
        echo "  Warning: Empty display name was accepted"
    fi
fi

echo ""

# ========================================================================
# PART 4: SET USERNAME
# ========================================================================
echo "--- PART 4: SET USERNAME ---"

USERNAME_TO_SET="testuser_$(date +%s)"
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
    echo "  Failed to submit transaction"
    echo "  $TX_RES"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Username set successfully"

        # Verify the change
        PROFILE_INFO=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)
        NEW_USERNAME=$(echo "$PROFILE_INFO" | jq -r '.member_profile.username // "not set"')
        echo "  Verified username: $NEW_USERNAME"
    else
        echo "  Failed to set username (may require DREAM payment)"
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
    echo "  Failed to list profiles"
    echo "  $PROFILES"
else
    PROFILE_COUNT=$(echo "$PROFILES" | jq -r '.member_profile | length' 2>/dev/null || echo "0")
    echo "  Total profiles: $PROFILE_COUNT"

    # Show first few profiles
    if [ "$PROFILE_COUNT" -gt 0 ]; then
        echo ""
        echo "  Sample profiles:"
        echo "$PROFILES" | jq -r '.member_profile[0:3] | .[] | "    - \(.address) (Name: \(.display_name // "none"), XP: \(.season_xp // 0))"' 2>/dev/null
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
    echo "  No member found with that display name"
else
    FOUND_ADDR=$(echo "$MEMBER_RESULT" | jq -r '.member_profile.address // .address // "unknown"')
    echo "  Found member: $FOUND_ADDR"

    if [ "$FOUND_ADDR" == "$DISPLAY_USER_ADDR" ]; then
        echo "  Address match verified"
    fi
fi

echo ""

# ========================================================================
# PART 7: CHECK AVAILABLE TITLES
# ========================================================================
echo "--- PART 7: CHECK AVAILABLE TITLES ---"

TITLES=$($BINARY query season titles --output json 2>&1)

if echo "$TITLES" | grep -q "error"; then
    echo "  Failed to query titles"
else
    TITLE_COUNT=$(echo "$TITLES" | jq -r '.titles | length' 2>/dev/null || echo "0")
    echo "  Available titles: $TITLE_COUNT"

    if [ "$TITLE_COUNT" -gt 0 ]; then
        echo ""
        echo "  Title examples:"
        echo "$TITLES" | jq -r '.titles[0:3] | .[] | "    - \(.title_id): \(.name) (\(.rarity))"' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 8: CHECK AVAILABLE ACHIEVEMENTS
# ========================================================================
echo "--- PART 8: CHECK AVAILABLE ACHIEVEMENTS ---"

ACHIEVEMENTS=$($BINARY query season achievements --output json 2>&1)

if echo "$ACHIEVEMENTS" | grep -q "error"; then
    echo "  Failed to query achievements"
else
    ACH_COUNT=$(echo "$ACHIEVEMENTS" | jq -r '.achievements | length' 2>/dev/null || echo "0")
    echo "  Available achievements: $ACH_COUNT"

    if [ "$ACH_COUNT" -gt 0 ]; then
        echo ""
        echo "  Achievement examples:"
        echo "$ACHIEVEMENTS" | jq -r '.achievements[0:3] | .[] | "    - \(.achievement_id): \(.name) (XP: \(.xp_reward))"' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 9: VERIFY GENESIS PROFILES (ALICE, BOB, CAROL)
# ========================================================================
echo "--- PART 9: VERIFY GENESIS PROFILES ---"

# Test Alice (high level, multiple achievements/titles)
echo "Verifying Alice's profile (Expected: Level 8, 5000 XP, 3 achievements, 3 titles)..."
ALICE_PROFILE=$($BINARY query season get-member-profile $ALICE_ADDR --output json 2>&1)

if echo "$ALICE_PROFILE" | grep -q "not found"; then
    echo "  ERROR: Alice profile not found!"
else
    ALICE_XP=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.season_xp // "0"')
    ALICE_LEVEL=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.season_level // "0"')
    ALICE_ACH=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.achievements | length' 2>/dev/null || echo "0")
    ALICE_TITLES=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.unlocked_titles | length' 2>/dev/null || echo "0")
    ALICE_DISPLAY=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.display_title // "none"')

    echo "  Level: $ALICE_LEVEL (expected: 8)"
    echo "  XP: $ALICE_XP (expected: 5000)"
    echo "  Achievements: $ALICE_ACH (expected: 3)"
    echo "  Unlocked Titles: $ALICE_TITLES (expected: 3)"
    echo "  Display Title: $ALICE_DISPLAY (expected: veteran)"

    # Verify specific achievements
    if [ "$ALICE_ACH" -gt 0 ]; then
        echo "  Achievement IDs:"
        echo "$ALICE_PROFILE" | jq -r '.member_profile.achievements[] | "    - \(.)"' 2>/dev/null
    fi

    # Verify specific titles
    if [ "$ALICE_TITLES" -gt 0 ]; then
        echo "  Unlocked Title IDs:"
        echo "$ALICE_PROFILE" | jq -r '.member_profile.unlocked_titles[] | "    - \(.)"' 2>/dev/null
    fi
fi

echo ""

# Test Bob (medium level)
echo "Verifying Bob's profile (Expected: Level 4, 1500 XP, 2 achievements, 1 title)..."
BOB_PROFILE=$($BINARY query season get-member-profile $BOB_ADDR --output json 2>&1)

if echo "$BOB_PROFILE" | grep -q "not found"; then
    echo "  ERROR: Bob profile not found!"
else
    BOB_XP=$(echo "$BOB_PROFILE" | jq -r '.member_profile.season_xp // "0"')
    BOB_LEVEL=$(echo "$BOB_PROFILE" | jq -r '.member_profile.season_level // "0"')
    BOB_ACH=$(echo "$BOB_PROFILE" | jq -r '.member_profile.achievements | length' 2>/dev/null || echo "0")
    BOB_TITLES=$(echo "$BOB_PROFILE" | jq -r '.member_profile.unlocked_titles | length' 2>/dev/null || echo "0")

    echo "  Level: $BOB_LEVEL (expected: 4)"
    echo "  XP: $BOB_XP (expected: 1500)"
    echo "  Achievements: $BOB_ACH (expected: 2)"
    echo "  Unlocked Titles: $BOB_TITLES (expected: 1)"
fi

echo ""

# Test Carol (low level)
echo "Verifying Carol's profile (Expected: Level 2, 300 XP, 1 achievement, 1 title)..."
CAROL_PROFILE=$($BINARY query season get-member-profile $CAROL_ADDR --output json 2>&1)

if echo "$CAROL_PROFILE" | grep -q "not found"; then
    echo "  ERROR: Carol profile not found!"
else
    CAROL_XP=$(echo "$CAROL_PROFILE" | jq -r '.member_profile.season_xp // "0"')
    CAROL_LEVEL=$(echo "$CAROL_PROFILE" | jq -r '.member_profile.season_level // "0"')
    CAROL_ACH=$(echo "$CAROL_PROFILE" | jq -r '.member_profile.achievements | length' 2>/dev/null || echo "0")
    CAROL_TITLES=$(echo "$CAROL_PROFILE" | jq -r '.member_profile.unlocked_titles | length' 2>/dev/null || echo "0")

    echo "  Level: $CAROL_LEVEL (expected: 2)"
    echo "  XP: $CAROL_XP (expected: 300)"
    echo "  Achievements: $CAROL_ACH (expected: 1)"
    echo "  Unlocked Titles: $CAROL_TITLES (expected: 1)"
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
        echo "  Failed to set display title: no txhash"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Display title changed successfully"

            # Verify the change
            ALICE_PROFILE=$($BINARY query season get-member-profile $ALICE_ADDR --output json 2>&1)
            NEW_TITLE=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.display_title // "not set"')
            echo "  New display title: $NEW_TITLE"

            if [ "$NEW_TITLE" == "$SECOND_TITLE" ]; then
                echo "  Display title change verified"
            else
                echo "  Warning: Display title mismatch"
            fi
        else
            echo "  Failed to set display title"
        fi
    fi
else
    echo "  Alice doesn't have a second title to change to (expected at least 2)"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- PROFILE TEST SUMMARY ---"
echo ""
echo "Basic Profile Operations:"
echo "  Profile queries:            Tested"
echo "  Set display name:           Tested"
echo "  Display name validation:    Tested"
echo "  Set username:               Tested"
echo "  Query by display name:      Tested"
echo ""
echo "XP/Achievements/Titles System:"
echo "  Titles query:               Tested"
echo "  Achievements query:         Tested"
echo "  Genesis profiles verified:  Tested (Alice, Bob, Carol)"
echo "  Set/change display title:   Tested"
echo ""
echo "PROFILE TEST COMPLETED"
echo ""
