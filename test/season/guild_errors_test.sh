#!/bin/bash

echo "--- TESTING: GUILD ERROR PATHS ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Source test env if available
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    source "$SCRIPT_DIR/.test_env"
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

echo "Alice: $ALICE_ADDR"
echo "Bob:   $BOB_ADDR"
echo "Carol: $CAROL_ADDR"
echo ""

# ========================================================================
# Result Tracking & Helpers
# ========================================================================
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}

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
    return 1
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    local BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    [ "$CODE" == "0" ]
}

expect_tx_failure() {
    local TX_RES="$1"
    local EXPECTED_ERR="$2"
    local TEST_NAME="$3"

    if ! submit_tx_and_wait "$TX_RES"; then
        if echo "$TX_RES" | grep -qi "$EXPECTED_ERR"; then
            record_result "$TEST_NAME" "PASS"
        else
            echo "  Broadcast rejection did not contain expected error: $EXPECTED_ERR"
            echo "  Response: $(echo "$TX_RES" | head -c 300)"
            record_result "$TEST_NAME" "FAIL"
        fi
        return
    fi

    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        local RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "$EXPECTED_ERR"; then
            echo "  Failed as expected (code: $CODE)"
            record_result "$TEST_NAME" "PASS"
        else
            echo "  Failed but unexpected error: $RAW_LOG"
            echo "  Expected: $EXPECTED_ERR"
            record_result "$TEST_NAME" "FAIL"
        fi
    else
        echo "  ERROR: Transaction succeeded when it should have failed!"
        record_result "$TEST_NAME" "FAIL"
    fi
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

# ========================================================================
# TEST 1: Create guild with empty name (ErrGuildNameTooShort)
# ========================================================================
echo "--- TEST 1: Create guild with empty name ---"

TX_RES=$($BINARY tx season create-guild \
    "" "A guild with no name" "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "too short\|empty\|invalid\|required" "Create guild with empty name"

# ========================================================================
# TEST 2: Create guild with 1-char name (ErrGuildNameTooShort)
# ========================================================================
echo "--- TEST 2: Create guild with 1-char name ---"

TX_RES=$($BINARY tx season create-guild \
    "X" "A guild with a too-short name" "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "too short\|invalid\|minimum" "Create guild with 1-char name"

# ========================================================================
# TEST 3: Create guild with very long name (ErrGuildNameTooLong)
# ========================================================================
echo "--- TEST 3: Create guild with very long name ---"

LONG_NAME=$(python3 -c "print('X' * 300)" 2>/dev/null || printf '%0.sX' $(seq 1 300))

TX_RES=$($BINARY tx season create-guild \
    "$LONG_NAME" "A guild with a too-long name" "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "too long\|exceeds\|invalid\|max" "Create guild with very long name"

# ========================================================================
# TEST 4: Join non-existent guild (ErrGuildNotFound)
# ========================================================================
echo "--- TEST 4: Join non-existent guild ---"

TX_RES=$($BINARY tx season join-guild \
    99999 \
    --from bob \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "not found\|does not exist\|invalid" "Join non-existent guild"

# ========================================================================
# FIXTURE: Get or create an ACTIVE guild for subsequent error tests.
# Previous tests may have frozen Alice's guild, so we always verify the
# guild status and create a fresh one with a free account if needed.
# ========================================================================
echo "--- FIXTURE: Get or create guild for error tests ---"

ERR_GUILD_ID=""
FIXTURE_GUILD_NAME=""
# Track which keyring key is the founder / non-founder / outsider for this guild
FOUNDER_KEY="alice"
FOUNDER_ADDR="$ALICE_ADDR"
NON_FOUNDER_KEY="bob"
NON_FOUNDER_ADDR="$BOB_ADDR"
OUTSIDER_KEY="carol"
OUTSIDER_ADDR="$CAROL_ADDR"

# Helper: create a guild with a given key and return the guild ID
create_fixture_guild() {
    local FROM_KEY=$1
    FIXTURE_GUILD_NAME="errtestguild-$(date +%s)"

    TX_RES=$($BINARY tx season create-guild \
        "$FIXTURE_GUILD_NAME" "Guild for error path testing" "false" \
        --from "$FROM_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            ERR_GUILD_ID=$(extract_event_value "$TX_RESULT" "guild_created" "guild_id")
            if [ -z "$ERR_GUILD_ID" ] || [ "$ERR_GUILD_ID" == "null" ]; then
                GUILDS=$($BINARY query season guilds-list --output json 2>&1)
                ERR_GUILD_ID=$(echo "$GUILDS" | jq -r '.guilds[-1].id // empty')
            fi
            echo "  Fixture guild created: ID=$ERR_GUILD_ID, Name=$FIXTURE_GUILD_NAME (founder=$FROM_KEY)"
            return 0
        else
            echo "  Failed to create fixture guild: $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
            return 1
        fi
    else
        echo "  Failed to submit fixture guild creation"
        return 1
    fi
}

# Helper: check if account has a member profile
has_member_profile() {
    local ADDR=$1
    local PROFILE=$($BINARY query season get-member-profile "$ADDR" --output json 2>&1)
    ! echo "$PROFILE" | grep -q "not found"
}

# Helper: create a member profile via set-display-name if missing
ensure_member_profile() {
    local KEY=$1
    local ADDR=$($BINARY keys show "$KEY" -a --keyring-backend test 2>/dev/null)
    if has_member_profile "$ADDR"; then return 0; fi
    echo "    Creating member profile for $KEY..."
    # Use short, deterministic display name (avoid cooldown clash with long unique names)
    local DNAME="ErrTest$(printf '%05d' $RANDOM)"
    TX_RES=$($BINARY tx season set-display-name "$DNAME" \
        --from "$KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")
        if ! check_tx_success "$TX_RESULT"; then
            echo "    set-display-name failed: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"')"
        fi
    else
        echo "    set-display-name broadcast failed: $(echo "$TX_RES" | head -c 200)"
    fi
}

# First check if Alice already has an ACTIVE guild we can reuse
ALICE_PROFILE=$($BINARY query season get-member-profile "$ALICE_ADDR" --output json 2>/dev/null)
EXISTING_GUILD=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.guild_id // "0"')

if [ -n "$EXISTING_GUILD" ] && [ "$EXISTING_GUILD" != "0" ] && [ "$EXISTING_GUILD" != "null" ]; then
    GUILD_INFO=$($BINARY query season get-guild "$EXISTING_GUILD" --output json 2>/dev/null)
    GUILD_STATUS=$(echo "$GUILD_INFO" | jq -r '.guild.status // "0"')
    FIXTURE_GUILD_NAME=$(echo "$GUILD_INFO" | jq -r '.guild.name // "existing"')

    # status 1 = ACTIVE; anything else (2=frozen, 3=dissolved) means unusable
    if [ "$GUILD_STATUS" == "1" ] || [ "$GUILD_STATUS" == "GUILD_STATUS_ACTIVE" ]; then
        ERR_GUILD_ID="$EXISTING_GUILD"
        echo "  Using Alice's existing active guild: ID=$ERR_GUILD_ID, Name=$FIXTURE_GUILD_NAME"
    fi
fi

# If no active guild found, try existing setup accounts that are members with DREAM but not in any guild
if [ -z "$ERR_GUILD_ID" ]; then
    echo "  Alice's guild is unusable, searching for existing setup account not in a guild..."
    CANDIDATE_KEYS="display_user quest_user guild_member2 guild_member1 guild_officer guild_founder"
    PICKED_KEY=""
    PICKED_ADDR=""
    for CANDIDATE in $CANDIDATE_KEYS; do
        CAND_ADDR=$($BINARY keys show "$CANDIDATE" -a --keyring-backend test 2>/dev/null)
        [ -z "$CAND_ADDR" ] && continue
        CAND_PROFILE=$($BINARY query season get-member-profile "$CAND_ADDR" --output json 2>/dev/null)
        CAND_GUILD=$(echo "$CAND_PROFILE" | jq -r '.member_profile.guild_id // "0"')
        if [ "$CAND_GUILD" = "0" ] || [ -z "$CAND_GUILD" ] || [ "$CAND_GUILD" = "null" ]; then
            PICKED_KEY="$CANDIDATE"
            PICKED_ADDR="$CAND_ADDR"
            echo "  Using existing account $CANDIDATE ($CAND_ADDR) — not in a guild"
            break
        fi
    done
    if [ -z "$PICKED_KEY" ]; then
        echo "  All setup accounts are in guilds — skipping dependent tests"
    fi
    if [ -n "$PICKED_KEY" ] && create_fixture_guild "$PICKED_KEY"; then
        FOUNDER_KEY="$PICKED_KEY"
        FOUNDER_ADDR="$PICKED_ADDR"

        # Also pick a non-founder and outsider from remaining free accounts
        NF_CAND=""
        OS_CAND=""
        for CANDIDATE in $CANDIDATE_KEYS; do
            [ "$CANDIDATE" = "$PICKED_KEY" ] && continue
            CAND_ADDR=$($BINARY keys show "$CANDIDATE" -a --keyring-backend test 2>/dev/null)
            [ -z "$CAND_ADDR" ] && continue
            CAND_PROFILE=$($BINARY query season get-member-profile "$CAND_ADDR" --output json 2>/dev/null)
            CAND_GUILD=$(echo "$CAND_PROFILE" | jq -r '.member_profile.guild_id // "0"')
            if [ "$CAND_GUILD" = "0" ] || [ -z "$CAND_GUILD" ] || [ "$CAND_GUILD" = "null" ]; then
                if [ -z "$NF_CAND" ]; then
                    NF_CAND="$CANDIDATE"
                    NON_FOUNDER_KEY="$CANDIDATE"
                    NON_FOUNDER_ADDR="$CAND_ADDR"
                elif [ -z "$OS_CAND" ]; then
                    OS_CAND="$CANDIDATE"
                    OUTSIDER_KEY="$CANDIDATE"
                    OUTSIDER_ADDR="$CAND_ADDR"
                    break
                fi
            fi
        done

    fi
fi

echo "  Roles: founder=$FOUNDER_KEY, non_founder=$NON_FOUNDER_KEY, outsider=$OUTSIDER_KEY"
echo ""

# ========================================================================
# TEST 5: Already in guild - founder tries to create another guild
# (ErrAlreadyInGuild)
# ========================================================================
echo "--- TEST 5: Already in guild (create second guild) ---"

if [ -n "$ERR_GUILD_ID" ]; then
    TX_RES=$($BINARY tx season create-guild \
        "SecondGuild_$(date +%s)" "Founder already has a guild" "false" \
        --from "$FOUNDER_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "already in\|already a member\|already has\|guild" "Already in guild (create second guild)"
else
    echo "  SKIP: No fixture guild created"
    record_result "Already in guild (create second guild)" "FAIL"
fi

# ========================================================================
# FIXTURE: Ensure non-founder is in the guild (for later tests)
# ========================================================================
if [ -n "$ERR_GUILD_ID" ]; then
    echo "--- FIXTURE: $NON_FOUNDER_KEY joins guild ---"

    NF_PROFILE=$($BINARY query season get-member-profile "$NON_FOUNDER_ADDR" --output json 2>/dev/null)
    NF_GUILD=$(echo "$NF_PROFILE" | jq -r '.member_profile.guild_id // "0"')

    if [ "$NF_GUILD" == "$ERR_GUILD_ID" ]; then
        echo "  $NON_FOUNDER_KEY is already in guild $ERR_GUILD_ID"
    else
        TX_RES=$($BINARY tx season join-guild \
            $ERR_GUILD_ID \
            --from "$NON_FOUNDER_KEY" \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)
            if check_tx_success "$TX_RESULT"; then
                echo "  $NON_FOUNDER_KEY joined guild $ERR_GUILD_ID"
            else
                echo "  FAIL: $NON_FOUNDER_KEY failed to join guild"
                echo "  $(echo "$TX_RESULT" | jq -r '.raw_log')"
                FAILURES=$((FAILURES + 1))
            fi
        fi
    fi
    echo ""
fi

# ========================================================================
# TEST 6: Non-founder tries to promote (ErrNotGuildFounder)
# ========================================================================
echo "--- TEST 6: Non-founder tries to promote ---"

if [ -n "$ERR_GUILD_ID" ]; then
    TX_RES=$($BINARY tx season promote-to-officer \
        $ERR_GUILD_ID \
        $OUTSIDER_ADDR \
        --from "$NON_FOUNDER_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "not.*founder\|not authorized\|permission\|only.*founder" "Non-founder tries to promote"
else
    echo "  SKIP: No fixture guild created"
    record_result "Non-founder tries to promote" "FAIL"
fi

# ========================================================================
# TEST 7: Founder tries to leave guild (ErrCannotLeaveAsFounder)
# ========================================================================
echo "--- TEST 7: Founder tries to leave guild ---"

if [ -n "$ERR_GUILD_ID" ]; then
    TX_RES=$($BINARY tx season leave-guild \
        --from "$FOUNDER_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "cannot leave\|founder\|dissolve" "Founder tries to leave guild"
else
    echo "  SKIP: No fixture guild created"
    record_result "Founder tries to leave guild" "FAIL"
fi

# ========================================================================
# TEST 8: Kick the founder (ErrCannotKickFounder)
# ========================================================================
echo "--- TEST 8: Kick the guild founder ---"

if [ -n "$ERR_GUILD_ID" ]; then
    TX_RES=$($BINARY tx season kick-from-guild \
        $ERR_GUILD_ID \
        $FOUNDER_ADDR \
        "testing kick founder" \
        --from "$FOUNDER_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "cannot kick\|founder\|kick founder" "Kick the guild founder"
else
    echo "  SKIP: No fixture guild created"
    record_result "Kick the guild founder" "FAIL"
fi

# ========================================================================
# TEST 9: Demote non-officer (ErrNotOfficer)
# ========================================================================
echo "--- TEST 9: Demote non-officer ---"

if [ -n "$ERR_GUILD_ID" ]; then
    # Non-founder is a regular member, not an officer
    TX_RES=$($BINARY tx season demote-officer \
        $ERR_GUILD_ID \
        $NON_FOUNDER_ADDR \
        --from "$FOUNDER_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "not.*officer\|not an officer\|officer\|not.*member" "Demote non-officer"
else
    echo "  SKIP: No fixture guild created"
    record_result "Demote non-officer" "FAIL"
fi

# ========================================================================
# TEST 10: Join invite-only guild without invite (ErrGuildInviteOnly)
# ========================================================================
echo "--- TEST 10: Join invite-only guild without invite ---"

if [ -n "$ERR_GUILD_ID" ]; then
    # First set the guild to invite-only
    echo "  Setting guild to invite-only..."
    TX_RES=$($BINARY tx season set-guild-invite-only \
        $ERR_GUILD_ID \
        "true" \
        --from "$FOUNDER_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "  Guild set to invite-only"
        else
            echo "  Warning: Failed to set invite-only"
        fi
    fi

    # Create a fresh account with zero guild history (existing accounts may have cooldowns)
    FRESH_KEY="guild_err_fresh_$(date +%s)"
    echo "  Creating fresh account: $FRESH_KEY"
    $BINARY keys add "$FRESH_KEY" --keyring-backend test > /dev/null 2>&1
    FRESH_ADDR=$($BINARY keys show "$FRESH_KEY" -a --keyring-backend test 2>/dev/null)

    # Fund the fresh account (use alice who has genesis funds)
    TX_RES=$($BINARY tx bank send alice "$FRESH_ADDR" 1000000uspark \
        --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        wait_for_tx "$TXHASH" > /dev/null 2>&1
    fi

    # Create member profile for fresh account
    ensure_member_profile "$FRESH_KEY"

    # Fresh account tries to join without invite
    echo "  $FRESH_KEY trying to join invite-only guild without invite..."
    TX_RES=$($BINARY tx season join-guild \
        $ERR_GUILD_ID \
        --from "$FRESH_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "invite.only\|invite\|no.*invite\|not invited" "Join invite-only guild without invite"
else
    echo "  SKIP: No fixture guild created"
    record_result "Join invite-only guild without invite" "FAIL"
fi

# ========================================================================
# TEST 11: Accept non-existent invite (ErrNoGuildInvite)
# ========================================================================
echo "--- TEST 11: Accept non-existent guild invite ---"

TX_RES=$($BINARY tx season accept-guild-invite \
    99999 \
    --from carol \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "no.*invite\|invite.*not found\|not found\|does not exist" "Accept non-existent guild invite"

# ========================================================================
# TEST 12: Duplicate guild name (ErrGuildNameTaken)
# ========================================================================
echo "--- TEST 12: Duplicate guild name ---"

if [ -n "$ERR_GUILD_ID" ] && [ -n "$FIXTURE_GUILD_NAME" ]; then
    # Use outsider (not in the error guild) to attempt creating a guild with the same name
    TX_RES=$($BINARY tx season create-guild \
        "$FIXTURE_GUILD_NAME" "Duplicate name test" "false" \
        --from "$OUTSIDER_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "taken\|already exists\|duplicate\|name.*used" "Duplicate guild name"
else
    echo "  SKIP: No fixture guild created"
    record_result "Duplicate guild name" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================================================"
echo "  GUILD ERROR PATHS TEST SUMMARY"
echo "============================================================================"
echo ""
echo "  Tests Run:    $((PASS_COUNT + FAIL_COUNT))"
echo "  Tests Passed: $PASS_COUNT"
echo "  Tests Failed: $FAIL_COUNT"
echo ""

for i in "${!TEST_NAMES[@]}"; do
    echo "  ${RESULTS[$i]}: ${TEST_NAMES[$i]}"
done

echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
