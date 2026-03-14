#!/bin/bash

echo "--- TESTING: DISPLAY NAME MODERATION (REPORTS, APPEALS) ---"

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

# Override addresses from keyring (in case .test_env is stale after snapshot restore)
DISPLAY_USER_ADDR=$($BINARY keys show display_user -a --keyring-backend test 2>/dev/null || echo "$DISPLAY_USER_ADDR")
GUILD_MEMBER1_ADDR=$($BINARY keys show guild_member1 -a --keyring-backend test 2>/dev/null || echo "$GUILD_MEMBER1_ADDR")
GUILD_MEMBER2_ADDR=$($BINARY keys show guild_member2 -a --keyring-backend test 2>/dev/null || echo "$GUILD_MEMBER2_ADDR")
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null || echo "$ALICE_ADDR")

echo "Display User (target):  $DISPLAY_USER_ADDR"
echo "Guild Member 1 (reporter): $GUILD_MEMBER1_ADDR"
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

# Proto3 omits false/0/"" defaults from JSON. This helper normalizes booleans.
jq_bool() {
    local JQ_INPUT=$1
    local JQ_PATH=$2
    echo "$JQ_INPUT" | jq -r "if ${JQ_PATH} then \"true\" else \"false\" end"
}

# ========================================================================
# PART 1: SET UP A DISPLAY NAME TO TEST
# ========================================================================
echo "--- PART 1: SET UP A DISPLAY NAME TO TEST ---"

# First, set a display name for the target user
DISPLAY_NAME="TestName_$(date +%s)"
echo "Setting display name: $DISPLAY_NAME"

TX_RES=$($BINARY tx season set-display-name \
    "$DISPLAY_NAME" \
    --from display_user \
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
        echo "  Display name set"
    else
        echo "  Failed to set display name"
    fi
else
    echo "  Failed to submit transaction"
fi

echo ""

# ========================================================================
# PART 2: QUERY DISPLAY NAME MODERATIONS
# ========================================================================
echo "--- PART 2: QUERY DISPLAY NAME MODERATIONS ---"

MODERATIONS=$($BINARY query season list-display-name-moderation --output json 2>&1)

if echo "$MODERATIONS" | grep -q "error"; then
    echo "  Failed to query display name moderations"
else
    MOD_COUNT=$(echo "$MODERATIONS" | jq -r '.display_name_moderation | length' 2>/dev/null || echo "0")
    echo "  Active moderation records: $MOD_COUNT"

    if [ "$MOD_COUNT" -gt 0 ]; then
        echo "$MODERATIONS" | jq -r '.display_name_moderation[] | "    - \(.member | .[0:20])...: active=\(.active)"' 2>/dev/null | head -5
    fi
fi

echo ""

# ========================================================================
# PART 3: REPORT DISPLAY NAME
# ========================================================================
echo "--- PART 3: REPORT DISPLAY NAME ---"

echo "Reporting display name of display_user..."
echo "Note: This requires staking DREAM (default 50) which is burned if frivolous"

TX_RES=$($BINARY tx season report-display-name \
    "$DISPLAY_USER_ADDR" \
    "Testing the display name reporting system" \
    --from guild_member1 \
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
        echo "  Display name reported successfully"

        # Query the moderation record
        MOD_RECORD=$($BINARY query season get-display-name-moderation $DISPLAY_USER_ADDR --output json 2>&1)

        if ! echo "$MOD_RECORD" | grep -q "error\|not found"; then
            echo ""
            echo "  Moderation Record:"
            echo "    Active: $(echo "$MOD_RECORD" | jq -r '.display_name_moderation.active // "unknown"')"
            echo "    Rejected Name: $(echo "$MOD_RECORD" | jq -r '.display_name_moderation.rejected_name // "unknown"')"
            echo "    Reason: $(echo "$MOD_RECORD" | jq -r '.display_name_moderation.reason // "none"')"
        fi
    else
        echo "  Failed to report display name (may need more DREAM or already reported)"
    fi
fi

echo ""

# ========================================================================
# PART 4: QUERY REPORT STAKES
# ========================================================================
echo "--- PART 4: QUERY REPORT STAKES ---"

STAKES=$($BINARY query season list-display-name-report-stake --output json 2>&1)

if echo "$STAKES" | grep -q "error"; then
    echo "  Failed to query report stakes"
else
    STAKE_COUNT=$(echo "$STAKES" | jq -r '.display_name_report_stake | length' 2>/dev/null || echo "0")
    echo "  Active report stakes: $STAKE_COUNT"

    if [ "$STAKE_COUNT" -gt 0 ]; then
        echo "$STAKES" | jq -r '.display_name_report_stake[] | "    - Challenge \(.challenge_id): \(.amount) DREAM"' 2>/dev/null | head -5
    fi
fi

echo ""

# ========================================================================
# PART 5: APPEAL DISPLAY NAME MODERATION
# ========================================================================
echo "--- PART 5: APPEAL DISPLAY NAME MODERATION ---"

# Check if the user has a moderation record to appeal
MOD_RECORD=$($BINARY query season get-display-name-moderation $DISPLAY_USER_ADDR --output json 2>&1)

if echo "$MOD_RECORD" | grep -q "error\|not found"; then
    echo "  No moderation record to appeal"
else
    MOD_STATUS=$(echo "$MOD_RECORD" | jq -r '.display_name_moderation.active // "unknown"')
    echo "  Current moderation active: $MOD_STATUS"

    echo "  Attempting to appeal moderation..."
    echo "  Note: This requires staking DREAM (default 100) which is burned if appeal fails"

    TX_RES=$($BINARY tx season appeal-display-name-moderation \
        "My display name is legitimate and does not violate any rules" \
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
            echo "  Appeal submitted successfully"

            # Query updated moderation record
            MOD_RECORD=$($BINARY query season get-display-name-moderation $DISPLAY_USER_ADDR --output json 2>&1)

            if ! echo "$MOD_RECORD" | grep -q "error\|not found"; then
                echo ""
                echo "  Updated Moderation Record:"
                echo "    Active: $(echo "$MOD_RECORD" | jq -r '.display_name_moderation.active // "unknown"')"
                echo "    Appeal Challenge ID: $(echo "$MOD_RECORD" | jq -r '.display_name_moderation.appeal_challenge_id // "none"')"
            fi
        else
            echo "  Failed to appeal (may not be in appealable state or need more DREAM)"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 6: QUERY APPEAL STAKES
# ========================================================================
echo "--- PART 6: QUERY APPEAL STAKES ---"

APPEAL_STAKES=$($BINARY query season list-display-name-appeal-stake --output json 2>&1)

if echo "$APPEAL_STAKES" | grep -q "error"; then
    echo "  Failed to query appeal stakes"
else
    APPEAL_STAKE_COUNT=$(echo "$APPEAL_STAKES" | jq -r '.display_name_appeal_stake | length' 2>/dev/null || echo "0")
    echo "  Active appeal stakes: $APPEAL_STAKE_COUNT"

    if [ "$APPEAL_STAKE_COUNT" -gt 0 ]; then
        echo "$APPEAL_STAKES" | jq -r '.display_name_appeal_stake[] | "    - Challenge \(.challenge_id): \(.amount) DREAM"' 2>/dev/null | head -5
    fi
fi

echo ""

# ========================================================================
# PART 7: CHECK MODERATION PARAMETERS
# ========================================================================
echo "--- PART 7: CHECK MODERATION PARAMETERS ---"

PARAMS=$($BINARY query season params --output json 2>&1)

if echo "$PARAMS" | grep -q "error"; then
    echo "  Failed to query params"
else
    echo "  Display Name Moderation Parameters:"
    echo "    Report Stake (DREAM): $(echo "$PARAMS" | jq -r '.params.display_name_report_stake_dream // "unknown"')"
    echo "    Appeal Stake (DREAM): $(echo "$PARAMS" | jq -r '.params.display_name_appeal_stake_dream // "unknown"')"
    echo "    Appeal Period (blocks): $(echo "$PARAMS" | jq -r '.params.display_name_appeal_period_blocks // "unknown"')"
    echo "    Display Name Min Length: $(echo "$PARAMS" | jq -r '.params.display_name_min_length // "unknown"')"
    echo "    Display Name Max Length: $(echo "$PARAMS" | jq -r '.params.display_name_max_length // "unknown"')"
    echo "    Change Cooldown (epochs): $(echo "$PARAMS" | jq -r '.params.display_name_change_cooldown_epochs // "unknown"')"
fi

echo ""

# ========================================================================
# PART 8: RESOLVE APPEAL — APPEAL SUCCEEDS (name restored)
# ========================================================================
echo "--- PART 8: RESOLVE APPEAL — APPEAL SUCCEEDS ---"

MOD_RECORD=$($BINARY query season get-display-name-moderation $DISPLAY_USER_ADDR --output json 2>&1)

if echo "$MOD_RECORD" | grep -q "error\|not found"; then
    echo "  SKIP: No moderation record to resolve"
else
    MOD_ACTIVE=$(echo "$MOD_RECORD" | jq -r '.display_name_moderation.active // "unknown"')
    APPEAL_ID=$(echo "$MOD_RECORD" | jq -r '.display_name_moderation.appeal_challenge_id // ""')
    REJECTED_NAME=$(echo "$MOD_RECORD" | jq -r '.display_name_moderation.rejected_name // ""')

    echo "  Pre-resolution state:"
    echo "    Active: $MOD_ACTIVE"
    echo "    Appeal Challenge ID: $APPEAL_ID"
    echo "    Rejected Name: $REJECTED_NAME"

    if [ "$MOD_ACTIVE" == "true" ] && [ -n "$APPEAL_ID" ] && [ "$APPEAL_ID" != "" ]; then
        echo ""

        # Snapshot stake counts before resolve
        PRE_REPORT_STAKES=$($BINARY query season list-display-name-report-stake --output json 2>&1)
        PRE_REPORT_COUNT=$(echo "$PRE_REPORT_STAKES" | jq -r '.display_name_report_stake | length' 2>/dev/null || echo "0")
        PRE_APPEAL_STAKES=$($BINARY query season list-display-name-appeal-stake --output json 2>&1)
        PRE_APPEAL_COUNT=$(echo "$PRE_APPEAL_STAKES" | jq -r '.display_name_appeal_stake | length' 2>/dev/null || echo "0")

        echo "  Resolving appeal with appeal_succeeded=true..."

        TX_RES=$($BINARY tx season resolve-display-name-appeal \
            "$DISPLAY_USER_ADDR" true \
            --from alice \
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
                echo "  Transaction succeeded"
                echo ""

                # Verify moderation record
                MOD_RECORD=$($BINARY query season get-display-name-moderation $DISPLAY_USER_ADDR --output json 2>&1)
                RESULT_ACTIVE=$(jq_bool "$MOD_RECORD" ".display_name_moderation.active")
                RESULT_APPEAL_SUCCEEDED=$(jq_bool "$MOD_RECORD" ".display_name_moderation.appeal_succeeded")

                assert_equals "moderation.active" "false" "$RESULT_ACTIVE"
                assert_equals "moderation.appeal_succeeded" "true" "$RESULT_APPEAL_SUCCEEDED"

                # Verify display name was restored
                PROFILE=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)
                RESTORED_NAME=$(echo "$PROFILE" | jq -r '.member_profile.display_name // ""')

                assert_equals "display name restored" "$REJECTED_NAME" "$RESTORED_NAME"

                # Verify stakes decreased (report stake for this case should be cleaned up)
                REPORT_STAKES=$($BINARY query season list-display-name-report-stake --output json 2>&1)
                POST_REPORT_COUNT=$(echo "$REPORT_STAKES" | jq -r '.display_name_report_stake | length' 2>/dev/null || echo "0")
                APPEAL_STAKES=$($BINARY query season list-display-name-appeal-stake --output json 2>&1)
                POST_APPEAL_COUNT=$(echo "$APPEAL_STAKES" | jq -r '.display_name_appeal_stake | length' 2>/dev/null || echo "0")

                REPORT_DECREASED=$(( PRE_REPORT_COUNT - POST_REPORT_COUNT ))
                APPEAL_DECREASED=$(( PRE_APPEAL_COUNT - POST_APPEAL_COUNT ))

                assert_equals "report stake cleaned up (decreased by 1)" "1" "$REPORT_DECREASED"
                assert_equals "appeal stake cleaned up (decreased by 1)" "1" "$APPEAL_DECREASED"
            else
                fail "Resolve appeal transaction failed"
            fi
        fi
    else
        echo "  SKIP: No active appeal to resolve (active=$MOD_ACTIVE, appeal_id=$APPEAL_ID)"
    fi
fi

echo ""

# ========================================================================
# PART 9: FULL CYCLE — REPORT → APPEAL → RESOLVE (appeal fails, name stays cleared)
# ========================================================================
echo "--- PART 9: FULL CYCLE — APPEAL FAILS ---"

# After PART 8, the display name was restored. We can report it again directly
# (no need to set a new name, which would be blocked by cooldown).
PART9_READY=false

# 9a. Verify the display name is currently set (restored by PART 8)
PROFILE=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)
CURRENT_NAME=$(echo "$PROFILE" | jq -r '.member_profile.display_name // ""')

if [ -n "$CURRENT_NAME" ] && [ "$CURRENT_NAME" != "" ]; then
    echo "  Current display name (restored by PART 8): $CURRENT_NAME"
    PART9_READY=true
else
    echo "  SKIP: No display name to report (PART 8 may not have restored it)"
fi

if [ "$PART9_READY" == "true" ]; then
    echo ""

    # 9b. Report the restored display name
    echo "  Reporting display name..."
    TX_RES=$($BINARY tx season report-display-name \
        "$DISPLAY_USER_ADDR" \
        "Testing appeal failure path" \
        --from guild_member1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit report"
        PART9_READY=false
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Report submitted"

            # Verify display name was cleared by report
            PROFILE=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)
            CLEARED_NAME=$(echo "$PROFILE" | jq -r '.member_profile.display_name // ""')
            assert_equals "display name cleared by report" "" "$CLEARED_NAME"
        else
            echo "  Failed to report"
            PART9_READY=false
        fi
    fi
fi

if [ "$PART9_READY" == "true" ]; then
    echo ""

    # 9c. Appeal the moderation
    echo "  Appealing moderation..."
    TX_RES=$($BINARY tx season appeal-display-name-moderation \
        "I believe my name is appropriate" \
        --from display_user \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit appeal"
        PART9_READY=false
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Appeal submitted"

            # Verify pre-resolution state
            MOD_RECORD=$($BINARY query season get-display-name-moderation $DISPLAY_USER_ADDR --output json 2>&1)
            PRE_ACTIVE=$(jq_bool "$MOD_RECORD" ".display_name_moderation.active")
            PRE_APPEAL_ID=$(echo "$MOD_RECORD" | jq -r '.display_name_moderation.appeal_challenge_id // ""')

            assert_equals "pre-resolve moderation.active" "true" "$PRE_ACTIVE"
            echo "  Pre-resolve appeal_challenge_id: $PRE_APPEAL_ID"

            if [ -z "$PRE_APPEAL_ID" ] || [ "$PRE_APPEAL_ID" == "" ]; then
                fail "No appeal challenge ID set"
                PART9_READY=false
            fi
        else
            echo "  Failed to appeal"
            PART9_READY=false
        fi
    fi
fi

if [ "$PART9_READY" == "true" ]; then
    echo ""

    # Snapshot stake counts before resolve
    PRE9_REPORT_STAKES=$($BINARY query season list-display-name-report-stake --output json 2>&1)
    PRE9_REPORT_COUNT=$(echo "$PRE9_REPORT_STAKES" | jq -r '.display_name_report_stake | length' 2>/dev/null || echo "0")
    PRE9_APPEAL_STAKES=$($BINARY query season list-display-name-appeal-stake --output json 2>&1)
    PRE9_APPEAL_COUNT=$(echo "$PRE9_APPEAL_STAKES" | jq -r '.display_name_appeal_stake | length' 2>/dev/null || echo "0")

    # 9d. Resolve with appeal_succeeded=false (appeal fails, name stays cleared)
    echo "  Resolving appeal with appeal_succeeded=false..."
    TX_RES=$($BINARY tx season resolve-display-name-appeal \
        "$DISPLAY_USER_ADDR" false \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit resolve transaction"
        echo "  $TX_RES"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Transaction succeeded"
            echo ""

            # Verify moderation record
            MOD_RECORD=$($BINARY query season get-display-name-moderation $DISPLAY_USER_ADDR --output json 2>&1)
            RESULT_ACTIVE=$(jq_bool "$MOD_RECORD" ".display_name_moderation.active")
            RESULT_APPEAL_SUCCEEDED=$(jq_bool "$MOD_RECORD" ".display_name_moderation.appeal_succeeded")

            assert_equals "moderation.active" "false" "$RESULT_ACTIVE"
            assert_equals "moderation.appeal_succeeded" "false" "$RESULT_APPEAL_SUCCEEDED"

            # Verify display name stays cleared (appeal failed, so name is not restored)
            PROFILE=$($BINARY query season get-member-profile $DISPLAY_USER_ADDR --output json 2>&1)
            FINAL_NAME=$(echo "$PROFILE" | jq -r '.member_profile.display_name // ""')

            assert_equals "display name stays cleared" "" "$FINAL_NAME"

            # Verify stakes decreased
            REPORT_STAKES=$($BINARY query season list-display-name-report-stake --output json 2>&1)
            POST9_REPORT_COUNT=$(echo "$REPORT_STAKES" | jq -r '.display_name_report_stake | length' 2>/dev/null || echo "0")
            APPEAL_STAKES=$($BINARY query season list-display-name-appeal-stake --output json 2>&1)
            POST9_APPEAL_COUNT=$(echo "$APPEAL_STAKES" | jq -r '.display_name_appeal_stake | length' 2>/dev/null || echo "0")

            REPORT9_DECREASED=$(( PRE9_REPORT_COUNT - POST9_REPORT_COUNT ))
            APPEAL9_DECREASED=$(( PRE9_APPEAL_COUNT - POST9_APPEAL_COUNT ))

            assert_equals "report stake cleaned up (decreased by 1)" "1" "$REPORT9_DECREASED"
            assert_equals "appeal stake cleaned up (decreased by 1)" "1" "$APPEAL9_DECREASED"
        else
            fail "Resolve appeal (fails) transaction failed"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 10: UNAPPEALED MODERATION — REPORT → NO APPEAL → RESOLVE
# ========================================================================
echo "--- PART 10: UNAPPEALED MODERATION (report → no appeal → resolve) ---"

# Use guild_member2 as the target for this test (fresh user, not used above)
PART10_TARGET_ADDR=$GUILD_MEMBER2_ADDR
PART10_READY=false

# Pre-check: if guild_member2 has an active moderation from a previous run, resolve it first
PRE_MOD=$($BINARY query season get-display-name-moderation $PART10_TARGET_ADDR --output json 2>&1)
if ! echo "$PRE_MOD" | grep -q "error\|not found"; then
    PRE_MOD_ACTIVE=$(jq_bool "$PRE_MOD" ".display_name_moderation.active")
    if [ "$PRE_MOD_ACTIVE" == "true" ]; then
        echo "  Pre-existing active moderation on guild_member2 — resolving first..."
        PRE_APPEAL_ID=$(echo "$PRE_MOD" | jq -r '.display_name_moderation.appeal_challenge_id // ""')
        if [ -n "$PRE_APPEAL_ID" ] && [ "$PRE_APPEAL_ID" != "" ]; then
            # Has an appeal — resolve it
            TX_RES=$($BINARY tx season resolve-display-name-appeal \
                "$PART10_TARGET_ADDR" false \
                --from alice --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)
        else
            # No appeal — resolve as unappealed (may fail if appeal period not expired)
            TX_RES=$($BINARY tx season resolve-unappealed-moderation \
                "$PART10_TARGET_ADDR" \
                --from alice --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)
        fi
        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
            sleep 6
            wait_for_tx $TXHASH > /dev/null 2>&1
        fi
        echo "  Pre-existing moderation resolved (or attempted)"
    fi
fi

# 10a. Set a display name for guild_member2
PART10_NAME="UnappealedTest_$(date +%s)"
echo "  Setting display name for guild_member2: $PART10_NAME"

TX_RES=$($BINARY tx season set-display-name \
    "$PART10_NAME" \
    --from guild_member2 \
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
        echo "  Display name set for guild_member2"
        PART10_READY=true
    else
        echo "  Failed to set display name for guild_member2"
    fi
else
    echo "  Failed to submit transaction"
fi

# Snapshot global report stake count before PART 10's report
PRE10_REPORT_STAKES=$($BINARY query season list-display-name-report-stake --output json 2>&1)
PRE10_REPORT_COUNT=$(echo "$PRE10_REPORT_STAKES" | jq -r '.display_name_report_stake | length' 2>/dev/null || echo "0")

# 10b. Report the display name (from guild_member1)
if [ "$PART10_READY" == "true" ]; then
    echo ""
    echo "  Reporting guild_member2's display name..."
    TX_RES=$($BINARY tx season report-display-name \
        "$PART10_TARGET_ADDR" \
        "Testing unappealed moderation resolution" \
        --from guild_member1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit report"
        PART10_READY=false
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Report submitted"

            # Verify moderation is active
            MOD_RECORD=$($BINARY query season get-display-name-moderation $PART10_TARGET_ADDR --output json 2>&1)
            MOD_ACTIVE=$(jq_bool "$MOD_RECORD" ".display_name_moderation.active")
            assert_equals "moderation is active after report" "true" "$MOD_ACTIVE"

            # Verify no appeal challenge ID (unappealed)
            APPEAL_ID=$(echo "$MOD_RECORD" | jq -r '.display_name_moderation.appeal_challenge_id // ""')
            assert_equals "no appeal filed" "" "$APPEAL_ID"
        else
            echo "  Failed to report"
            PART10_READY=false
        fi
    fi
fi

# 10c. Try to resolve-unappealed-moderation (should FAIL — appeal period not expired)
if [ "$PART10_READY" == "true" ]; then
    echo ""
    echo "  Attempting resolve-unappealed-moderation (expect failure: period not expired)..."
    TX_RES=$($BINARY tx season resolve-unappealed-moderation \
        "$PART10_TARGET_ADDR" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        fail "Could not submit resolve-unappealed-moderation tx"
        echo "  $TX_RES"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        TX_CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$TX_CODE" != "0" ]; then
            # Expected failure — appeal period not expired
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "appeal period\|not.*expired\|not yet expired"; then
                pass "Correctly rejected (appeal period not expired)"
            else
                pass "Transaction failed as expected (code=$TX_CODE)"
                echo "    Log: $RAW_LOG"
            fi
        else
            echo "  UNEXPECTED: Transaction succeeded (appeal period should not have expired)"
            echo "  This may indicate the appeal period is very short in this config"
        fi
    fi
fi

# 10d. Verify moderation is still active (not resolved by the failed attempt)
if [ "$PART10_READY" == "true" ]; then
    echo ""
    echo "  Verifying moderation still active after failed resolve..."
    MOD_RECORD=$($BINARY query season get-display-name-moderation $PART10_TARGET_ADDR --output json 2>&1)
    MOD_ACTIVE=$(jq_bool "$MOD_RECORD" ".display_name_moderation.active")
    assert_equals "moderation still active" "true" "$MOD_ACTIVE"

    # Verify reporter's stake is still locked
    REPORT_STAKES=$($BINARY query season list-display-name-report-stake --output json 2>&1)
    REPORT_STAKE_COUNT=$(echo "$REPORT_STAKES" | jq -r '.display_name_report_stake | length' 2>/dev/null || echo "0")
    echo "  Report stakes still locked: $REPORT_STAKE_COUNT"

    if [ "$REPORT_STAKE_COUNT" -gt 0 ]; then
        pass "Reporter's stake remains locked while awaiting resolution"
    else
        fail "No report stakes found (expected stake to be locked)"
    fi
fi

# 10e. Wait for the appeal period to expire
if [ "$PART10_READY" == "true" ]; then
    echo ""

    # Get appeal period from params
    APPEAL_PERIOD=$($BINARY query season params --output json 2>&1 | jq -r '.params.display_name_appeal_period_blocks // "20"')
    echo "  Appeal period: $APPEAL_PERIOD blocks"

    # Get the block height when moderation was created
    MOD_RECORD=$($BINARY query season get-display-name-moderation $PART10_TARGET_ADDR --output json 2>&1)
    MODERATED_AT=$(echo "$MOD_RECORD" | jq -r '.display_name_moderation.moderated_at // "0"')
    EXPIRY_BLOCK=$((MODERATED_AT + APPEAL_PERIOD + 1))

    echo "  Moderated at block: $MODERATED_AT"
    echo "  Waiting for block $EXPIRY_BLOCK (appeal period expiry)..."

    # Poll until the chain passes the expiry block
    for i in $(seq 1 60); do
        CURRENT_BLOCK=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
        if [ "$CURRENT_BLOCK" -ge "$EXPIRY_BLOCK" ] 2>/dev/null; then
            echo "  Current block $CURRENT_BLOCK >= $EXPIRY_BLOCK — appeal period expired"
            break
        fi
        if [ $i -eq 60 ]; then
            fail "Timed out waiting for appeal period to expire (block $CURRENT_BLOCK < $EXPIRY_BLOCK)"
            PART10_READY=false
        fi
        sleep 1
    done
fi

# 10f. Verify unappealed moderation was resolved (by BeginBlock auto-resolve or manual tx)
if [ "$PART10_READY" == "true" ]; then
    echo ""

    # Check if BeginBlock already auto-resolved the expired moderation
    MOD_RECORD=$($BINARY query season get-display-name-moderation $PART10_TARGET_ADDR --output json 2>&1)
    ALREADY_RESOLVED=$(jq_bool "$MOD_RECORD" ".display_name_moderation.active")

    if [ "$ALREADY_RESOLVED" == "false" ]; then
        echo "  Moderation auto-resolved by BeginBlock (appeal period expired)"
    else
        # Not yet auto-resolved, try manual resolve
        echo "  Resolving unappealed moderation manually (appeal period expired)..."
        TX_RES=$($BINARY tx season resolve-unappealed-moderation \
            "$PART10_TARGET_ADDR" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            fail "Could not submit resolve-unappealed-moderation tx"
            echo "  $TX_RES"
            PART10_READY=false
        else
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)

            if ! check_tx_success "$TX_RESULT"; then
                fail "Resolve unappealed moderation transaction failed"
                PART10_READY=false
            fi
        fi

        # Re-query after manual resolve
        MOD_RECORD=$($BINARY query season get-display-name-moderation $PART10_TARGET_ADDR --output json 2>&1)
    fi
fi

# 10g. Verify final state
if [ "$PART10_READY" == "true" ]; then
    echo ""

    RESULT_ACTIVE=$(jq_bool "$MOD_RECORD" ".display_name_moderation.active")
    RESULT_APPEAL_SUCCEEDED=$(jq_bool "$MOD_RECORD" ".display_name_moderation.appeal_succeeded")

    assert_equals "unappealed moderation.active" "false" "$RESULT_ACTIVE"
    assert_equals "unappealed moderation.appeal_succeeded" "false" "$RESULT_APPEAL_SUCCEEDED"

    # Verify display name stays cleared (report upheld)
    PROFILE=$($BINARY query season get-member-profile $PART10_TARGET_ADDR --output json 2>&1)
    FINAL_NAME=$(echo "$PROFILE" | jq -r '.member_profile.display_name // ""')
    assert_equals "unappealed display name stays cleared" "" "$FINAL_NAME"

    # Verify reporter's stake was returned (count back to pre-report level)
    REPORT_STAKES=$($BINARY query season list-display-name-report-stake --output json 2>&1)
    POST10_REPORT_COUNT=$(echo "$REPORT_STAKES" | jq -r '.display_name_report_stake | length' 2>/dev/null || echo "0")
    assert_equals "unappealed report stake cleaned up (back to pre-report count)" "$PRE10_REPORT_COUNT" "$POST10_REPORT_COUNT"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- DISPLAY NAME MODERATION TEST SUMMARY ---"
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
echo "DISPLAY NAME MODERATION TEST COMPLETED"
echo ""
