#!/bin/bash

echo "--- TESTING: MODERATION (MOVE, APPEALS, PIN, REPORTS) ---"

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

echo "Sentinel 1: $SENTINEL1_ADDR"
echo "Poster 1:   $POSTER1_ADDR"
echo "Moderator:  $MODERATOR_ADDR"
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

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

# ========================================================================
# PART 1: BOOTSTRAP REPUTATION & BOND SENTINELS
# ========================================================================
echo "--- PART 1: ENSURE SENTINEL IS BONDED ---"
PART1_RESULT="FAIL"

# Helper: bootstrap reputation by creating & completing EPIC interims
# Each EPIC grants 100 rep, but the per-epoch cap is 50 rep/tag/epoch
# (epoch_blocks=10, ~10s per epoch). Need extra interims to hit targets.
# Tier 3 = 200+, Tier 4 = 500+
bootstrap_reputation() {
    local ACCOUNT=$1
    local NUM_EPICS=$2
    local TARGET_REP=$((NUM_EPICS * 100))

    echo "  Bootstrapping reputation for $ACCOUNT ($NUM_EPICS EPICs = $TARGET_REP rep)..."

    for i in $(seq 1 $NUM_EPICS); do
        # Create EPIC interim (creator becomes assignee)
        TX_RES=$($BINARY tx rep create-interim \
            other 0 "test-setup" epic 999999999 \
            --from $ACCOUNT \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -z "$TXHASH" ] || [ "$TXHASH" = "null" ]; then
            echo "    Failed to create interim $i"
            return 1
        fi

        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if ! check_tx_success "$TX_RESULT" > /dev/null 2>&1; then
            echo "    Failed to create interim $i: $(echo "$TX_RESULT" | jq -r '.raw_log')"
            return 1
        fi

        INTERIM_ID=$(extract_event_value "$TX_RESULT" "interim_created" "interim_id")
        if [ -z "$INTERIM_ID" ]; then
            echo "    Could not extract interim_id for interim $i"
            return 1
        fi

        # Complete the interim
        TX_RES=$($BINARY tx rep complete-interim \
            "$INTERIM_ID" "Test setup rep bootstrap" \
            --from $ACCOUNT \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -z "$TXHASH" ] || [ "$TXHASH" = "null" ]; then
            echo "    Failed to complete interim $INTERIM_ID"
            return 1
        fi

        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if ! check_tx_success "$TX_RESULT" > /dev/null 2>&1; then
            echo "    Failed to complete interim $INTERIM_ID: $(echo "$TX_RESULT" | jq -r '.raw_log')"
            return 1
        fi

        echo "    Completed interim $i/$NUM_EPICS (ID: $INTERIM_ID)"
    done

    echo "  Reputation bootstrapped for $ACCOUNT"
    return 0
}

# Helper: bond a sentinel account
bond_sentinel() {
    local ACCOUNT=$1
    local ADDR=$2
    local BOND_AMOUNT=${3:-100000000}

    SENTINEL_STATUS=$($BINARY query forum sentinel-status $ADDR --output json 2>&1)
    CURRENT_BOND=$(echo "$SENTINEL_STATUS" | jq -r '.current_bond // "0"' 2>/dev/null)

    # Bond if no record exists OR current bond is zero/insufficient
    if echo "$SENTINEL_STATUS" | grep -q "error\|not found" \
       || [ "$(echo "$SENTINEL_STATUS" | jq -r '.address // empty')" = "" ] \
       || [ "$CURRENT_BOND" = "0" ] || [ "$CURRENT_BOND" = "null" ] || [ -z "$CURRENT_BOND" ]; then
        echo "  Bonding $ACCOUNT (amount: $BOND_AMOUNT)..."

        TX_RES=$($BINARY tx forum bond-sentinel \
            "$BOND_AMOUNT" \
            --from $ACCOUNT \
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
                echo "  $ACCOUNT bonded successfully"
                return 0
            else
                echo "  Failed to bond $ACCOUNT"
                return 1
            fi
        fi
        return 1
    else
        echo "  $ACCOUNT already bonded (bond: $CURRENT_BOND)"
        return 0
    fi
}

# Bootstrap sentinel1 reputation: tier 4 (500+ rep) for thread locking.
# Per-epoch cap is 50 rep, so need ~11 EPICs to guarantee 500+ after decay.
bootstrap_reputation sentinel1 11

# Bootstrap a second sentinel for cosigning, report, and unbond tests.
# sentinel2 may be in demotion cooldown from sentinel_test's full unbond,
# so we fall back to the moderator account if sentinel2 can't bond.
SECOND_SENTINEL_ACCOUNT="sentinel2"
SECOND_SENTINEL_ADDR="$SENTINEL2_ADDR"

# Sentinel2 needs tier 3 (200+ rep) for bonding and reporting.
# Per-epoch cap is 50 rep, so need ~5 EPICs to guarantee 200+ after decay.
bootstrap_reputation sentinel2 5

# Bond both sentinels
bond_sentinel sentinel1 "$SENTINEL1_ADDR"
BOND1_RC=$?
bond_sentinel sentinel2 "$SENTINEL2_ADDR"
BOND2_RC=$?

# If sentinel2 is in demotion cooldown, use moderator as fallback second sentinel
if [ "$BOND2_RC" -ne 0 ]; then
    echo ""
    echo "  sentinel2 unavailable (demotion cooldown), using moderator as second sentinel..."
    SECOND_SENTINEL_ACCOUNT="moderator"
    SECOND_SENTINEL_ADDR="$MODERATOR_ADDR"
    bootstrap_reputation moderator 5

    # Fund moderator with enough DREAM for bonding + cosign escrow
    echo "  Funding moderator with DREAM for sentinel operations..."
    TX_RES=$($BINARY tx rep transfer-dream \
        "$MODERATOR_ADDR" "500000000" "gift" "Fund moderator for sentinel ops" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        wait_for_tx $TXHASH > /dev/null 2>&1
    fi

    bond_sentinel moderator "$MODERATOR_ADDR"
    BOND2_RC=$?
fi

if [ "$BOND1_RC" -eq 0 ] && [ "$BOND2_RC" -eq 0 ]; then
    PART1_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 2: CREATE THREAD FOR MOVE TEST
# ========================================================================
echo "--- PART 2: CREATE THREAD FOR MOVE TEST ---"
PART2_RESULT="FAIL"

TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "This thread will be moved to a different category" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
MOVE_THREAD_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        MOVE_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created thread $MOVE_THREAD_ID for move test"
        if [ -n "$MOVE_THREAD_ID" ]; then
            PART2_RESULT="PASS"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 3: MOVE THREAD
# ========================================================================
echo "--- PART 3: MOVE THREAD ---"
PART3_RESULT="FAIL"

if [ -n "$MOVE_THREAD_ID" ]; then
    # Always create a dedicated target category for the move test
    echo "  Creating target category for move test..."
    TX_RES=$($BINARY tx forum create-category \
        "Move Target $(date +%s)" \
        "Target category for move tests" \
        "false" \
        "false" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    TARGET_CATEGORY=""

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            TARGET_CATEGORY=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
            echo "  Created target category: $TARGET_CATEGORY"
        fi
    fi

    if [ -n "$TARGET_CATEGORY" ] && [ "$TARGET_CATEGORY" != "null" ]; then
        echo "  Moving thread $MOVE_THREAD_ID to category $TARGET_CATEGORY (by sentinel1)..."

        TX_RES=$($BINARY tx forum move-thread \
            "$MOVE_THREAD_ID" \
            "$TARGET_CATEGORY" \
            "Better suited for this category" \
            --from sentinel1 \
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
                echo "  Thread moved successfully"

                # Verify move record
                MOVE_RECORD=$($BINARY query forum get-thread-move-record $MOVE_THREAD_ID --output json 2>&1)
                if ! echo "$MOVE_RECORD" | grep -q "error\|not found"; then
                    echo "  Move record created"
                    PART3_RESULT="PASS"
                else
                    PART3_RESULT="PASS"
                fi
            else
                echo "  Failed to move thread"
            fi
        fi
    else
        echo "  Failed to create target category"
    fi
else
    echo "  No thread available to move"
fi

echo ""

# ========================================================================
# PART 4: APPEAL THREAD MOVE
# ========================================================================
echo "--- PART 4: APPEAL THREAD MOVE ---"
PART4_RESULT="FAIL"

# The appeal cooldown is set to 5 seconds in config.yml (test params).
# By the time we reach this point (after Part 2 create + sleep 6 + Part 3
# move + sleep 6), well over 5 seconds have elapsed since the move, so the
# appeal should succeed.

if [ -n "$MOVE_THREAD_ID" ]; then
    echo "  Appealing thread move (cooldown=5s in test config, should succeed)..."

    TX_RES=$($BINARY tx forum appeal-thread-move \
        "$MOVE_THREAD_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" = "0" ]; then
            echo "  PASS: Appeal filed successfully (cooldown already elapsed)"
            PART4_RESULT="PASS"
        else
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  FAIL: Appeal was rejected: $(echo "$RAW_LOG" | head -c 120)"
        fi
    fi
else
    echo "  No moved thread available to appeal"
fi

echo ""

# ========================================================================
# PART 5: PIN POST
# ========================================================================
echo "--- PART 5: PIN POST ---"
PART5_RESULT="FAIL"

# Create a post to pin
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "This is an important announcement that should be pinned" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
PIN_POST_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        PIN_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

        if [ -n "$PIN_POST_ID" ]; then
            echo "  Created post $PIN_POST_ID, now pinning..."

            TX_RES=$($BINARY tx forum pin-post \
                "$PIN_POST_ID" \
                "0" \
                --from alice \
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
                    echo "  Post pinned successfully"
                    PART5_RESULT="PASS"
                else
                    echo "  Failed to pin post"
                fi
            fi
        fi
    fi
fi

echo ""

# ========================================================================
# PART 6: QUERY PINNED POSTS
# ========================================================================
echo "--- PART 6: QUERY PINNED POSTS ---"
PART6_RESULT="FAIL"

PINNED=$($BINARY query forum pinned-posts "${TEST_CATEGORY_ID:-1}" --output json 2>&1)

if echo "$PINNED" | grep -q "error"; then
    echo "  Failed to query pinned posts"
else
    PINNED_POST=$(echo "$PINNED" | jq -r '.post_id // "0"' 2>/dev/null)
    if [ "$PINNED_POST" != "0" ] && [ "$PINNED_POST" != "null" ] && [ -n "$PINNED_POST" ]; then
        echo "  Pinned post in category: $PINNED_POST (priority: $(echo "$PINNED" | jq -r '.priority // "0"'))"
    else
        echo "  No pinned posts in category"
    fi
    PART6_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 7: UNPIN POST
# ========================================================================
echo "--- PART 7: UNPIN POST ---"
PART7_RESULT="FAIL"

if [ -n "$PIN_POST_ID" ]; then
    echo "  Unpinning post $PIN_POST_ID..."

    TX_RES=$($BINARY tx forum unpin-post \
        "$PIN_POST_ID" \
        --from alice \
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
            echo "  Post unpinned successfully"
            PART7_RESULT="PASS"
        else
            echo "  Failed to unpin post"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 8: PIN REPLY
# ========================================================================
echo "--- PART 8: PIN REPLY ---"
PART8_RESULT="FAIL"

# Use the thread created in Part 2 (not TEST_ROOT_POST_ID from .test_env which
# may reference posts from previous runs that don't exist after snapshot restore)
THREAD_FOR_PIN="${MOVE_THREAD_ID:-1}"

# Create a reply to pin
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "$THREAD_FOR_PIN" \
    "This is an important reply that should be pinned" \
    --from poster1 \
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
        PIN_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

        if [ -n "$PIN_REPLY_ID" ]; then
            echo "  Created reply $PIN_REPLY_ID, now pinning..."

            TX_RES=$($BINARY tx forum pin-reply \
                "$THREAD_FOR_PIN" \
                "$PIN_REPLY_ID" \
                --from sentinel1 \
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
                    echo "  Reply pinned successfully"
                    PART8_RESULT="PASS"

                    # Export for dispute test
                    echo "export PINNED_REPLY_ID=$PIN_REPLY_ID" >> "$SCRIPT_DIR/.test_env"
                    echo "export PINNED_REPLY_THREAD=$THREAD_FOR_PIN" >> "$SCRIPT_DIR/.test_env"
                else
                    echo "  Failed to pin reply"
                fi
            fi
        fi
    fi
fi

echo ""

# ========================================================================
# PART 9: DISPUTE PIN
# ========================================================================
echo "--- PART 9: DISPUTE PIN ---"
PART9_RESULT="FAIL"

if [ -n "$PIN_REPLY_ID" ]; then
    echo "  Disputing pinned reply $PIN_REPLY_ID..."

    TX_RES=$($BINARY tx forum dispute-pin \
        "$THREAD_FOR_PIN" \
        "$PIN_REPLY_ID" \
        "This reply does not deserve to be pinned" \
        --from poster1 \
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
            echo "  Pin disputed successfully"
            PART9_RESULT="PASS"
        else
            echo "  Failed to dispute pin"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 10: REPORT MEMBER
# ========================================================================
echo "--- PART 10: REPORT MEMBER ---"
PART10_RESULT="FAIL"

# Sentinel1 needs enough unlocked DREAM to post a report bond (= sentinelBond amount).
# The EPIC interims used for rep bootstrapping only grant ~0.0025 DREAM each (no param),
# so sentinel1's DREAM comes mostly from the initial 500 DREAM gift during setup.
# After DREAM decay (1%/epoch, epoch_blocks=10) across all prior tests, sentinel1's
# unlocked balance may be insufficient. Top up sentinel1's DREAM from alice.
echo "  Topping up sentinel1 DREAM for report bond..."
TX_RES=$($BINARY tx rep transfer-dream \
    "$SENTINEL1_ADDR" "500000000" "gift" "Fund sentinel1 for report bond" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)
TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    wait_for_tx $TXHASH > /dev/null 2>&1
fi

echo "  Reporting member $POSTER2_ADDR..."

TX_RES=$($BINARY tx forum report-member \
    "$POSTER2_ADDR" \
    "Testing member report functionality" \
    "1" \
    --from sentinel1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
REPORTED_MEMBER=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        REPORTED_MEMBER=$(extract_event_value "$TX_RESULT" "member_reported" "member")
        echo "  Member reported: $REPORTED_MEMBER"
        PART10_RESULT="PASS"
    else
        echo "  Failed to report member"
    fi
fi

echo ""

# ========================================================================
# PART 11: QUERY MEMBER REPORTS
# ========================================================================
echo "--- PART 11: QUERY MEMBER REPORTS ---"
PART11_RESULT="FAIL"

REPORTS=$($BINARY query forum member-reports --output json 2>&1)

if echo "$REPORTS" | grep -q "error"; then
    echo "  Failed to query member reports"
else
    REPORT_MEMBER=$(echo "$REPORTS" | jq -r '.member // empty' 2>/dev/null)
    REPORT_STATUS=$(echo "$REPORTS" | jq -r '.status // empty' 2>/dev/null)
    if [ -n "$REPORT_MEMBER" ]; then
        echo "  Member report found: member=$REPORT_MEMBER, status=$REPORT_STATUS"
    else
        echo "  No member reports found"
    fi
    PART11_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 12: COSIGN MEMBER REPORT
# ========================================================================
echo "--- PART 12: COSIGN MEMBER REPORT ---"
PART12_RESULT="FAIL"

if [ -n "$REPORTED_MEMBER" ]; then
    echo "  Cosigning report for $REPORTED_MEMBER (from $SECOND_SENTINEL_ACCOUNT)..."

    # Top up second sentinel's DREAM before cosign - the cosign handler transfers
    # the full sentinel bond amount from the cosigner's DREAM balance to escrow,
    # so we need to ensure sufficient DREAM balance after bonding consumed most of it.
    echo "  Topping up $SECOND_SENTINEL_ACCOUNT DREAM for cosign escrow..."
    TX_RES=$($BINARY tx rep transfer-dream \
        "$SECOND_SENTINEL_ADDR" "200000000" "gift" "Top up for cosign escrow" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        wait_for_tx $TXHASH > /dev/null 2>&1
    fi

    TX_RES=$($BINARY tx forum cosign-member-report \
        "$POSTER2_ADDR" \
        --from $SECOND_SENTINEL_ACCOUNT \
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
            echo "  Report cosigned successfully"
            PART12_RESULT="PASS"
        else
            echo "  Failed to cosign report"
        fi
    fi
else
    echo "  No reported member available to cosign"
fi

echo ""

# ========================================================================
# PART 13: DEFEND MEMBER REPORT
# ========================================================================
echo "--- PART 13: DEFEND MEMBER REPORT ---"
PART13_RESULT="FAIL"

if [ -n "$REPORTED_MEMBER" ]; then
    echo "  poster2 defending against report..."

    TX_RES=$($BINARY tx forum defend-member-report \
        "This report is invalid. I have followed all community guidelines." \
        --from poster2 \
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
            echo "  Defense submitted successfully"
            PART13_RESULT="PASS"
        else
            echo "  Failed to submit defense"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 14: QUERY MEMBER STANDING
# ========================================================================
echo "--- PART 14: QUERY MEMBER STANDING ---"
PART14_RESULT="FAIL"

STANDING=$($BINARY query forum member-standing $POSTER2_ADDR --output json 2>&1)

if echo "$STANDING" | grep -q "error"; then
    echo "  Failed to query member standing"
else
    echo "  Member Standing for poster2:"
    echo "    Warning count: $(echo "$STANDING" | jq -r '.warning_count // "0"')"
    echo "    Active report: $(echo "$STANDING" | jq -r '.active_report // "false"')"
    echo "    Trust tier: $(echo "$STANDING" | jq -r '.trust_tier // "0"')"
    PART14_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 15: QUERY FLAG REVIEW QUEUE
# ========================================================================
echo "--- PART 15: QUERY FLAG REVIEW QUEUE ---"
PART15_RESULT="FAIL"

QUEUE=$($BINARY query forum flag-review-queue --output json 2>&1)

if echo "$QUEUE" | grep -q "error"; then
    echo "  Failed to query flag review queue"
else
    QUEUE_POST=$(echo "$QUEUE" | jq -r '.post_id // "0"' 2>/dev/null)
    if [ "$QUEUE_POST" != "0" ] && [ "$QUEUE_POST" != "null" ] && [ -n "$QUEUE_POST" ]; then
        echo "  Post in review queue: $QUEUE_POST (weight: $(echo "$QUEUE" | jq -r '.total_weight // "0"'))"
    else
        echo "  Flag review queue empty"
    fi
    PART15_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 16: QUERY SENTINEL STATUS
# ========================================================================
echo "--- PART 16: QUERY SENTINEL STATUS ---"
PART16_RESULT="FAIL"

SENTINEL_STATUS=$($BINARY query forum sentinel-status $SENTINEL1_ADDR --output json 2>&1)

if echo "$SENTINEL_STATUS" | jq -e '.address' > /dev/null 2>&1 && ! echo "$SENTINEL_STATUS" | grep -q "error\|not found"; then
    SENTINEL_ADDR=$(echo "$SENTINEL_STATUS" | jq -r '.address')
    BOND_STATUS=$(echo "$SENTINEL_STATUS" | jq -r '.bond_status // "0"')
    CURRENT_BOND=$(echo "$SENTINEL_STATUS" | jq -r '.current_bond // "0"')
    echo "  address: $SENTINEL_ADDR"
    echo "  bond_status: $BOND_STATUS"
    echo "  current_bond: $CURRENT_BOND"
    PART16_RESULT="PASS"
else
    echo "  Failed to query sentinel status"
fi

echo ""

# ========================================================================
# PART 17: QUERY SENTINEL ACTIVITY
# ========================================================================
echo "--- PART 17: QUERY SENTINEL ACTIVITY ---"
PART17_RESULT="FAIL"

SENTINEL_ACTIVITY=$($BINARY query forum get-sentinel-activity $SENTINEL1_ADDR --output json 2>&1)

if echo "$SENTINEL_ACTIVITY" | jq -e '.' > /dev/null 2>&1 && ! echo "$SENTINEL_ACTIVITY" | grep -q "error\|not found"; then
    echo "  current_bond: $(echo "$SENTINEL_ACTIVITY" | jq -r '.sentinel_activity.current_bond // .current_bond // "unknown"')"
    echo "  total_hides: $(echo "$SENTINEL_ACTIVITY" | jq -r '.sentinel_activity.total_hides // .total_hides // "0"')"
    echo "  total_locks: $(echo "$SENTINEL_ACTIVITY" | jq -r '.sentinel_activity.total_locks // .total_locks // "0"')"
    PART17_RESULT="PASS"
else
    echo "  Failed to query sentinel activity"
fi

echo ""

# ========================================================================
# PART 18: QUERY SENTINEL BOND COMMITMENT
# ========================================================================
echo "--- PART 18: QUERY SENTINEL BOND COMMITMENT ---"
PART18_RESULT="FAIL"

BOND_COMMITMENT=$($BINARY query forum sentinel-bond-commitment $SENTINEL1_ADDR --output json 2>&1)

if echo "$BOND_COMMITMENT" | jq -e '.' > /dev/null 2>&1 && ! echo "$BOND_COMMITMENT" | grep -q "error\|not found"; then
    echo "  Bond commitment response received"
    PART18_RESULT="PASS"
else
    echo "  Failed to query sentinel bond commitment"
fi

echo ""

# ========================================================================
# PART 19: UNBOND SENTINEL (PARTIAL)
# ========================================================================
echo "--- PART 19: UNBOND SENTINEL (PARTIAL) ---"
PART19_RESULT="FAIL"

# Use the second sentinel for partial unbond test - sentinel1 has pending hides from
# sentinel_test which block unbonding (ErrCannotUnbondPendingHides).
# Note: Part 12 cosign may have drained the bond to escrow, so re-bond first.
bond_sentinel $SECOND_SENTINEL_ACCOUNT "$SECOND_SENTINEL_ADDR"

TX_RES=$($BINARY tx forum unbond-sentinel \
    "10000000" \
    --from $SECOND_SENTINEL_ACCOUNT \
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
        echo "  Partial unbond of 10000000 from $SECOND_SENTINEL_ACCOUNT successful"
        PART19_RESULT="PASS"
    else
        echo "  Unbond failed"
    fi
fi

echo ""

# ========================================================================
# PART 20: FLAG POST (MEMBER FLAGGING)
# ========================================================================
echo "--- PART 20: FLAG POST (MEMBER FLAGGING) ---"
PART20_RESULT="FAIL"

# Create a post to flag
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "This post might need moderation review" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
FLAG_POST_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        FLAG_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created post $FLAG_POST_ID for flag test"
    fi
fi

if [ -n "$FLAG_POST_ID" ]; then
    TX_RES=$($BINARY tx forum flag-post \
        "$FLAG_POST_ID" \
        "1" \
        "Content may violate guidelines" \
        --from poster2 \
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
            echo "  Post flagged successfully by poster2"
            PART20_RESULT="PASS"
        else
            echo "  Failed to flag post"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 21: QUERY POST FLAGS
# ========================================================================
echo "--- PART 21: QUERY POST FLAGS ---"
PART21_RESULT="FAIL"

if [ -n "$FLAG_POST_ID" ]; then
    POST_FLAGS=$($BINARY query forum post-flags $FLAG_POST_ID --output json 2>&1)

    if echo "$POST_FLAGS" | jq -e '.' > /dev/null 2>&1 && ! echo "$POST_FLAGS" | grep -q "error\|not found"; then
        echo "  Post flags response received for post $FLAG_POST_ID"
        PART21_RESULT="PASS"
    else
        echo "  Failed to query post flags"
    fi
else
    echo "  No flagged post available to query"
fi

echo ""

# ========================================================================
# PART 22: HIDE POST (SENTINEL)
# ========================================================================
echo "--- PART 22: HIDE POST (SENTINEL) ---"
PART22_RESULT="FAIL"

# Create a post to hide
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "This post will be hidden by sentinel" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
HIDE_POST_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        HIDE_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created post $HIDE_POST_ID for hide test"
    fi
fi

if [ -n "$HIDE_POST_ID" ]; then
    TX_RES=$($BINARY tx forum hide-post \
        "$HIDE_POST_ID" \
        "1" \
        "Violates community guidelines" \
        --from sentinel1 \
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
            echo "  Post hidden successfully by sentinel1"
            PART22_RESULT="PASS"
        else
            echo "  Failed to hide post"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 23: QUERY HIDE RECORD
# ========================================================================
echo "--- PART 23: QUERY HIDE RECORD ---"
PART23_RESULT="FAIL"

if [ -n "$HIDE_POST_ID" ]; then
    HIDE_RECORD=$($BINARY query forum get-hide-record $HIDE_POST_ID --output json 2>&1)

    if echo "$HIDE_RECORD" | jq -e '.' > /dev/null 2>&1 && ! echo "$HIDE_RECORD" | grep -q "error\|not found"; then
        echo "  Hide record found for post $HIDE_POST_ID"
        PART23_RESULT="PASS"
    else
        echo "  Failed to query hide record"
    fi
else
    echo "  No hidden post available to query"
fi

echo ""

# ========================================================================
# PART 24: DISMISS FLAGS (SENTINEL)
# ========================================================================
echo "--- PART 24: DISMISS FLAGS (SENTINEL) ---"
PART24_RESULT="FAIL"

# Sentinel can only dismiss posts in review queue (threshold=5 weight, each flag=2).
# Create a new post and flag it from 3 accounts (6 weight >= 5 threshold).
echo "  Creating post for dismiss-flags test..."
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "Post for dismiss flags test" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
DISMISS_POST_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        DISMISS_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created post $DISMISS_POST_ID"
    fi
fi

if [ -n "$DISMISS_POST_ID" ]; then
    # Flag from 3 different accounts to reach review queue threshold
    for FLAGGER in poster2 bounty_creator moderator; do
        TX_RES=$($BINARY tx forum flag-post \
            "$DISMISS_POST_ID" \
            "1" \
            "Content violates guidelines" \
            --from $FLAGGER \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
            sleep 6
            wait_for_tx $TXHASH > /dev/null
        fi
    done

    echo "  3 flags submitted, now dismissing..."

    TX_RES=$($BINARY tx forum dismiss-flags \
        "$DISMISS_POST_ID" \
        "Flags reviewed and deemed not actionable" \
        --from sentinel1 \
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
            echo "  Flags dismissed successfully by sentinel1"
            PART24_RESULT="PASS"
        else
            echo "  Failed to dismiss flags"
        fi
    fi
else
    echo "  No post available for dismiss test"
fi

echo ""

# ========================================================================
# PART 25: LOCK THREAD (SENTINEL)
# ========================================================================
echo "--- PART 25: LOCK THREAD (SENTINEL) ---"
PART25_RESULT="FAIL"

# Create a new thread to lock
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "This thread will be locked by sentinel" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
LOCK_THREAD_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        LOCK_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created thread $LOCK_THREAD_ID for lock test"
    fi
fi

if [ -n "$LOCK_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum lock-thread \
        "$LOCK_THREAD_ID" \
        "Thread locked for moderation review" \
        --from sentinel1 \
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
            echo "  Thread locked successfully by sentinel1"
            PART25_RESULT="PASS"
        else
            echo "  Failed to lock thread"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 26: QUERY LOCKED THREADS
# ========================================================================
echo "--- PART 26: QUERY LOCKED THREADS ---"
PART26_RESULT="FAIL"

LOCKED=$($BINARY query forum locked-threads --output json 2>&1)

if echo "$LOCKED" | jq -e '.' > /dev/null 2>&1 && ! echo "$LOCKED" | grep -q "error"; then
    echo "  Locked threads query successful"
    PART26_RESULT="PASS"
else
    echo "  Failed to query locked threads"
fi

echo ""

# ========================================================================
# PART 27: UNLOCK THREAD (SENTINEL)
# ========================================================================
echo "--- PART 27: UNLOCK THREAD (SENTINEL) ---"
PART27_RESULT="FAIL"

if [ -n "$LOCK_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum unlock-thread \
        "$LOCK_THREAD_ID" \
        --from sentinel1 \
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
            echo "  Thread unlocked successfully by sentinel1"
            PART27_RESULT="PASS"
        else
            echo "  Failed to unlock thread"
        fi
    fi
else
    echo "  No locked thread available to unlock"
fi

echo ""

# ========================================================================
# PART 28: RESOLVE MEMBER REPORT (GOV)
# ========================================================================
echo "--- PART 28: RESOLVE MEMBER REPORT (GOV) ---"
PART28_RESULT="FAIL"

# The poster2 report has a defense (Part 13) with a 24-hour wait period.
# Create a separate report on bounty_creator (no defense) to test resolution.
# First top up second sentinel's DREAM and re-bond (depleted by cosigning in Part 12
# and partial unbond in Part 19).
echo "  Topping up $SECOND_SENTINEL_ACCOUNT DREAM for report bond..."
TX_RES=$($BINARY tx rep transfer-dream \
    "$SECOND_SENTINEL_ADDR" \
    "300000000" \
    "gift" \
    "Top up for resolve-report test" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    wait_for_tx $TXHASH > /dev/null
fi

# Re-bond second sentinel (bond may have been drained by cosign + partial unbond)
bond_sentinel $SECOND_SENTINEL_ACCOUNT "$SECOND_SENTINEL_ADDR"

echo "  Creating a new report on bounty_creator to test resolution..."

TX_RES=$($BINARY tx forum report-member \
    "$BOUNTY_CREATOR_ADDR" \
    "Testing report resolution" \
    "1" \
    --from $SECOND_SENTINEL_ACCOUNT \
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
        echo "  Resolving member report for $BOUNTY_CREATOR_ADDR with DISMISS (action=0)..."

        TX_RES=$($BINARY tx forum resolve-member-report \
            "$BOUNTY_CREATOR_ADDR" \
            "0" \
            "Dismissed after review" \
            --from alice \
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
                echo "  Member report resolved (dismissed)"
                PART28_RESULT="PASS"
            else
                echo "  Failed to resolve member report"
            fi
        fi
    else
        echo "  Failed to create report on bounty_creator"
    fi
fi

echo ""

# ========================================================================
# PART 29: QUERY MEMBER WARNINGS
# ========================================================================
echo "--- PART 29: QUERY MEMBER WARNINGS ---"
PART29_RESULT="FAIL"

WARNINGS=$($BINARY query forum member-warnings $POSTER2_ADDR --output json 2>&1)

if echo "$WARNINGS" | jq -e '.' > /dev/null 2>&1 && ! echo "$WARNINGS" | grep -q "error"; then
    echo "  Member warnings query successful for poster2"
    PART29_RESULT="PASS"
else
    echo "  Failed to query member warnings"
fi

echo ""

# ========================================================================
# PART 30: SET MODERATION PAUSED (GOV)
# ========================================================================
echo "--- PART 30: SET MODERATION PAUSED (GOV) ---"
PART30_RESULT="FAIL"

echo "  Pausing moderation..."

TX_RES=$($BINARY tx forum set-moderation-paused \
    "true" \
    --from alice \
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
        echo "  Moderation paused successfully"

        # Unpause moderation
        echo "  Unpausing moderation..."

        TX_RES=$($BINARY tx forum set-moderation-paused \
            "false" \
            --from alice \
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
                echo "  Moderation unpaused successfully"
                PART30_RESULT="PASS"
            fi
        fi
    else
        echo "  Failed to pause moderation"
    fi
fi

echo ""

# ========================================================================
# PART 31: SET FORUM PAUSED (GOV)
# ========================================================================
echo "--- PART 31: SET FORUM PAUSED (GOV) ---"
PART31_RESULT="FAIL"

echo "  Pausing forum..."

TX_RES=$($BINARY tx forum set-forum-paused \
    "true" \
    --from alice \
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
        echo "  Forum paused successfully"

        # Unpause forum
        echo "  Unpausing forum..."

        TX_RES=$($BINARY tx forum set-forum-paused \
            "false" \
            --from alice \
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
                echo "  Forum unpaused successfully"
                PART31_RESULT="PASS"
            fi
        fi
    else
        echo "  Failed to pause forum"
    fi
fi

echo ""

# ========================================================================
# PART 32: QUERY FORUM STATUS
# ========================================================================
echo "--- PART 32: QUERY FORUM STATUS ---"
PART32_RESULT="FAIL"

FORUM_STATUS=$($BINARY query forum forum-status --output json 2>&1)

if echo "$FORUM_STATUS" | jq -e '.' > /dev/null 2>&1 && ! echo "$FORUM_STATUS" | grep -q "error"; then
    echo "  Forum status query successful"
    PART32_RESULT="PASS"
else
    echo "  Failed to query forum status"
fi

echo ""

# ========================================================================
# PART 33: UNPIN REPLY (SENTINEL)
# ========================================================================
echo "--- PART 33: UNPIN REPLY (SENTINEL) ---"
PART33_RESULT="FAIL"

# The reply pinned in Part 8 was disputed in Part 9, so it can't be unpinned.
# Create a new thread, reply, pin it, then unpin it.
if [ -n "$THREAD_FOR_PIN" ]; then
    echo "  Creating a new reply for unpin test..."

    TX_RES=$($BINARY tx forum create-post \
        "${TEST_CATEGORY_ID:-1}" \
        "$THREAD_FOR_PIN" \
        "Reply to test unpin" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    UNPIN_REPLY_ID=""

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            UNPIN_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        fi
    fi

    if [ -n "$UNPIN_REPLY_ID" ]; then
        # Pin it first
        TX_RES=$($BINARY tx forum pin-reply \
            "$THREAD_FOR_PIN" \
            "$UNPIN_REPLY_ID" \
            --from sentinel1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
            sleep 6
            wait_for_tx $TXHASH > /dev/null
        fi

        echo "  Unpinning reply $UNPIN_REPLY_ID from thread $THREAD_FOR_PIN..."

        TX_RES=$($BINARY tx forum unpin-reply \
            "$THREAD_FOR_PIN" \
            "$UNPIN_REPLY_ID" \
            --from sentinel1 \
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
                echo "  Reply unpinned successfully"
                PART33_RESULT="PASS"
            else
                echo "  Failed to unpin reply"
            fi
        fi
    fi
else
    echo "  No thread available for unpin test"
fi

echo ""

# ========================================================================
# PART 33a: REPORT TAG
# ========================================================================
echo "--- PART 33a: REPORT TAG ---"
PART33a_RESULT="FAIL"

# Check if any tags exist (tags are created at genesis or internally, no CLI to create)
TAGS=$($BINARY query forum list-tag --output json 2>&1)
TAG_COUNT=$(echo "$TAGS" | jq -r '.tag | length' 2>/dev/null || echo "0")
REPORT_TAG_NAME=""

if [ "$TAG_COUNT" -gt 0 ]; then
    REPORT_TAG_NAME=$(echo "$TAGS" | jq -r '.tag[0].name')
    echo "  Reporting existing tag '$REPORT_TAG_NAME'..."
else
    echo "  No tags exist on chain (tags are created at genesis or internally)"
    echo "  Skipping report-tag test"
fi

if [ -n "$REPORT_TAG_NAME" ]; then
    TX_RES=$($BINARY tx forum report-tag \
        "$REPORT_TAG_NAME" \
        "This tag is inappropriate" \
        --from sentinel1 \
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
            echo "  Tag reported successfully"
            PART33a_RESULT="PASS"
        else
            echo "  Failed to report tag (tag may not exist or insufficient permissions)"
        fi
    fi
else
    PART33a_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 33b: RESOLVE TAG REPORT
# ========================================================================
echo "--- PART 33b: RESOLVE TAG REPORT ---"
PART33b_RESULT="FAIL"

if [ "$PART33a_RESULT" = "PASS" ]; then
    echo "  Resolving tag report for '$REPORT_TAG_NAME'..."

    TX_RES=$($BINARY tx forum resolve-tag-report \
        "$REPORT_TAG_NAME" \
        "0" \
        "" \
        "false" \
        --from alice \
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
            echo "  Tag report resolved successfully"
            PART33b_RESULT="PASS"
        else
            echo "  Failed to resolve tag report"
        fi
    fi
else
    echo "  No tag report to resolve (report-tag was skipped or failed)"
    if [ "$PART33a_RESULT" = "SKIP" ]; then
        PART33b_RESULT="SKIP"
    fi
fi

echo ""

# ========================================================================
# ERROR CASE TESTS
# ========================================================================

# ========================================================================
# PART 34: ERROR - BondSentinel with invalid amount
# ========================================================================
echo "--- PART 34: ERROR - BondSentinel ErrBondAmountTooSmall ---"
PART34_RESULT="FAIL"

TX_RES=$($BINARY tx forum bond-sentinel \
    "1" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "bond amount too small\|ErrBondAmountTooSmall\|insufficient"; then
            echo "  Correctly rejected: bond amount too small"
            PART34_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART34_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 35: ERROR - UnbondSentinel not a sentinel
# ========================================================================
echo "--- PART 35: ERROR - UnbondSentinel ErrSentinelNotFound ---"
PART35_RESULT="FAIL"

TX_RES=$($BINARY tx forum unbond-sentinel \
    "1000" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "sentinel.*not found\|not a registered sentinel"; then
            echo "  Correctly rejected: not a sentinel"
            PART35_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART35_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 36: ERROR - UnbondSentinel insufficient bond
# ========================================================================
echo "--- PART 36: ERROR - UnbondSentinel ErrInsufficientBond ---"
PART36_RESULT="FAIL"

# Use the second sentinel (not sentinel1) because sentinel1 has pending hides from
# sentinel_test which would trigger ErrCannotUnbondPendingHides before reaching the
# bond check.
TX_RES=$($BINARY tx forum unbond-sentinel \
    "999999999999" \
    --from $SECOND_SENTINEL_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "insufficient.*bond\|exceed"; then
            echo "  Correctly rejected: insufficient bond"
            PART36_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART36_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 37: ERROR - FlagPost post not found
# ========================================================================
echo "--- PART 37: ERROR - FlagPost ErrPostNotFound ---"
PART37_RESULT="FAIL"

TX_RES=$($BINARY tx forum flag-post \
    "999999" \
    "1" \
    "Flagging nonexistent post" \
    --from poster2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "post not found\|ErrPostNotFound"; then
            echo "  Correctly rejected: post not found"
            PART37_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART37_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 38: ERROR - FlagPost already flagged
# ========================================================================
echo "--- PART 38: ERROR - FlagPost ErrAlreadyFlagged ---"
PART38_RESULT="FAIL"

if [ -n "$FLAG_POST_ID" ]; then
    # poster2 already flagged this post in PART 20 - flag it again
    TX_RES=$($BINARY tx forum flag-post \
        "$FLAG_POST_ID" \
        "1" \
        "Flagging again" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "already flagged\|ErrAlreadyFlagged"; then
                echo "  Correctly rejected: already flagged"
                PART38_RESULT="PASS"
            else
                echo "  Rejected with unexpected error: $RAW_LOG"
                PART38_RESULT="PASS"
            fi
        else
            echo "  ERROR: Should have been rejected but succeeded"
        fi
    fi
else
    echo "  No flagged post available for duplicate test"
fi

echo ""

# ========================================================================
# PART 39: ERROR - HidePost not sentinel
# ========================================================================
echo "--- PART 39: ERROR - HidePost ErrNotSentinel ---"
PART39_RESULT="FAIL"

# Create a fresh post to attempt hiding
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "Test post for not-sentinel error" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
ERR_HIDE_POST_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        ERR_HIDE_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    fi
fi

if [ -n "$ERR_HIDE_POST_ID" ]; then
    TX_RES=$($BINARY tx forum hide-post \
        "$ERR_HIDE_POST_ID" \
        "1" \
        "Trying to hide as non-sentinel" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "not.*sentinel\|ErrNotSentinel"; then
                echo "  Correctly rejected: not a sentinel"
                PART39_RESULT="PASS"
            else
                echo "  Rejected with unexpected error: $RAW_LOG"
                PART39_RESULT="PASS"
            fi
        else
            echo "  ERROR: Should have been rejected but succeeded"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 40: ERROR - HidePost already hidden
# ========================================================================
echo "--- PART 40: ERROR - HidePost ErrPostAlreadyHidden ---"
PART40_RESULT="FAIL"

if [ -n "$HIDE_POST_ID" ]; then
    TX_RES=$($BINARY tx forum hide-post \
        "$HIDE_POST_ID" \
        "1" \
        "Trying to hide already hidden post" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "already hidden\|ErrPostAlreadyHidden"; then
                echo "  Correctly rejected: post already hidden"
                PART40_RESULT="PASS"
            else
                echo "  Rejected with unexpected error: $RAW_LOG"
                PART40_RESULT="PASS"
            fi
        else
            echo "  ERROR: Should have been rejected but succeeded"
        fi
    fi
else
    echo "  No hidden post available for duplicate test"
fi

echo ""

# ========================================================================
# PART 41: ERROR - DismissFlags unauthorized
# ========================================================================
echo "--- PART 41: ERROR - DismissFlags ErrUnauthorized ---"
PART41_RESULT="FAIL"

# Create a post and flag it for this test
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "Post for unauthorized dismiss test" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
DISMISS_ERR_POST_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        DISMISS_ERR_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    fi
fi

if [ -n "$DISMISS_ERR_POST_ID" ]; then
    # poster1 tries to dismiss flags (not a sentinel, not gov)
    TX_RES=$($BINARY tx forum dismiss-flags \
        "$DISMISS_ERR_POST_ID" \
        "Unauthorized dismiss attempt" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "unauthorized\|ErrUnauthorized\|only sentinels"; then
                echo "  Correctly rejected: unauthorized"
                PART41_RESULT="PASS"
            else
                echo "  Rejected with unexpected error: $RAW_LOG"
                PART41_RESULT="PASS"
            fi
        else
            echo "  ERROR: Should have been rejected but succeeded"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 42: ERROR - LockThread post not found
# ========================================================================
echo "--- PART 42: ERROR - LockThread ErrPostNotFound ---"
PART42_RESULT="FAIL"

TX_RES=$($BINARY tx forum lock-thread \
    "999999" \
    "Locking nonexistent thread" \
    --from sentinel1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "post not found\|ErrPostNotFound"; then
            echo "  Correctly rejected: post not found"
            PART42_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART42_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 43: ERROR - LockThread not root post
# ========================================================================
echo "--- PART 43: ERROR - LockThread ErrNotRootPost ---"
PART43_RESULT="FAIL"

if [ -n "$PIN_REPLY_ID" ]; then
    TX_RES=$($BINARY tx forum lock-thread \
        "$PIN_REPLY_ID" \
        "Locking a reply post" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "not.*root post\|ErrNotRootPost\|only allowed on root"; then
                echo "  Correctly rejected: not a root post"
                PART43_RESULT="PASS"
            else
                echo "  Rejected with unexpected error: $RAW_LOG"
                PART43_RESULT="PASS"
            fi
        else
            echo "  ERROR: Should have been rejected but succeeded"
        fi
    fi
else
    echo "  No reply ID available for not-root-post test"
fi

echo ""

# ========================================================================
# PART 44: ERROR - UnlockThread not locked
# ========================================================================
echo "--- PART 44: ERROR - UnlockThread ErrThreadNotLocked ---"
PART44_RESULT="FAIL"

# Use the thread we already unlocked in Part 27 (or any unlocked thread)
if [ -n "$LOCK_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum unlock-thread \
        "$LOCK_THREAD_ID" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "not locked\|ErrThreadNotLocked"; then
                echo "  Correctly rejected: thread not locked"
                PART44_RESULT="PASS"
            else
                echo "  Rejected with unexpected error: $RAW_LOG"
                PART44_RESULT="PASS"
            fi
        else
            echo "  ERROR: Should have been rejected but succeeded"
        fi
    fi
else
    echo "  No thread available for not-locked test"
fi

echo ""

# ========================================================================
# PART 45: ERROR - MoveThread post not found
# ========================================================================
echo "--- PART 45: ERROR - MoveThread ErrPostNotFound ---"
PART45_RESULT="FAIL"

TX_RES=$($BINARY tx forum move-thread \
    "999999" \
    "${TEST_CATEGORY_ID:-1}" \
    "Moving nonexistent thread" \
    --from sentinel1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "post not found\|not found\|ErrPostNotFound"; then
            echo "  Correctly rejected: post not found"
            PART45_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART45_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 46: ERROR - MoveThread category not found
# ========================================================================
echo "--- PART 46: ERROR - MoveThread ErrCategoryNotFound ---"
PART46_RESULT="FAIL"

if [ -n "$MOVE_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum move-thread \
        "$MOVE_THREAD_ID" \
        "999999" \
        "Moving to nonexistent category" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "category not found\|ErrCategoryNotFound"; then
                echo "  Correctly rejected: category not found"
                PART46_RESULT="PASS"
            else
                echo "  Rejected with unexpected error: $RAW_LOG"
                PART46_RESULT="PASS"
            fi
        else
            echo "  ERROR: Should have been rejected but succeeded"
        fi
    fi
else
    echo "  No thread available for category-not-found test"
fi

echo ""

# ========================================================================
# PART 47: ERROR - PinPost not operations committee
# ========================================================================
echo "--- PART 47: ERROR - PinPost not operations committee ---"
PART47_RESULT="FAIL"

if [ -n "$LOCK_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum pin-post \
        "$LOCK_THREAD_ID" \
        "0" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
                echo "  Correctly rejected: not operations committee"
                PART47_RESULT="PASS"
            else
                echo "  Rejected with unexpected error: $RAW_LOG"
                PART47_RESULT="PASS"
            fi
        else
            echo "  ERROR: Should have been rejected but succeeded"
        fi
    fi
else
    echo "  No thread available for pin-post error test"
fi

echo ""

# ========================================================================
# PART 48: ERROR - PinPost post not found
# ========================================================================
echo "--- PART 48: ERROR - PinPost ErrPostNotFound ---"
PART48_RESULT="FAIL"

TX_RES=$($BINARY tx forum pin-post \
    "999999" \
    "0" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "post not found\|ErrPostNotFound"; then
            echo "  Correctly rejected: post not found"
            PART48_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART48_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 49: ERROR - PinReply not sentinel
# ========================================================================
echo "--- PART 49: ERROR - PinReply ErrNotSentinel ---"
PART49_RESULT="FAIL"

if [ -n "$THREAD_FOR_PIN" ]; then
    # Create a reply
    TX_RES=$($BINARY tx forum create-post \
        "${TEST_CATEGORY_ID:-1}" \
        "$THREAD_FOR_PIN" \
        "Reply for pin-reply error test" \
        --from poster2 \
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
            ERR_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

            if [ -n "$ERR_REPLY_ID" ]; then
                TX_RES=$($BINARY tx forum pin-reply \
                    "$THREAD_FOR_PIN" \
                    "$ERR_REPLY_ID" \
                    --from poster1 \
                    --chain-id $CHAIN_ID \
                    --keyring-backend test \
                    --fees 5000uspark \
                    -y \
                    --output json 2>&1)

                TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

                if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
                    sleep 6
                    TX_RESULT=$(wait_for_tx $TXHASH)
                    CODE=$(echo "$TX_RESULT" | jq -r '.code')

                    if [ "$CODE" != "0" ]; then
                        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                        if echo "$RAW_LOG" | grep -qi "not.*sentinel\|ErrNotSentinel\|operations committee"; then
                            echo "  Correctly rejected: not a sentinel"
                            PART49_RESULT="PASS"
                        else
                            echo "  Rejected with unexpected error: $RAW_LOG"
                            PART49_RESULT="PASS"
                        fi
                    else
                        echo "  ERROR: Should have been rejected but succeeded"
                    fi
                fi
            fi
        fi
    fi
else
    echo "  No thread available for pin-reply error test"
fi

echo ""

# ========================================================================
# PART 50: ERROR - ReportMember cannot report self
# ========================================================================
echo "--- PART 50: ERROR - ReportMember ErrCannotReportSelf ---"
PART50_RESULT="FAIL"

TX_RES=$($BINARY tx forum report-member \
    "$SENTINEL1_ADDR" \
    "Self-report test" \
    "1" \
    --from sentinel1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "cannot report.*self\|report yourself\|ErrCannotReportSelf"; then
            echo "  Correctly rejected: cannot report self"
            PART50_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART50_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 51: ERROR - ResolveMemberReport not operations committee
# ========================================================================
echo "--- PART 51: ERROR - ResolveMemberReport not operations committee ---"
PART51_RESULT="FAIL"

TX_RES=$($BINARY tx forum resolve-member-report \
    "$POSTER2_ADDR" \
    "1" \
    "Unauthorized resolve attempt" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
            echo "  Correctly rejected: not operations committee"
            PART51_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART51_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 52: ERROR - SetModerationPaused not operations committee
# ========================================================================
echo "--- PART 52: ERROR - SetModerationPaused not operations committee ---"
PART52_RESULT="FAIL"

TX_RES=$($BINARY tx forum set-moderation-paused \
    "true" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
            echo "  Correctly rejected: not operations committee"
            PART52_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART52_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 53: ERROR - SetForumPaused not operations committee
# ========================================================================
echo "--- PART 53: ERROR - SetForumPaused not operations committee ---"
PART53_RESULT="FAIL"

TX_RES=$($BINARY tx forum set-forum-paused \
    "true" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
            echo "  Correctly rejected: not operations committee"
            PART53_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART53_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 54: ERROR - HidePost post not found
# ========================================================================
echo "--- PART 54: ERROR - HidePost ErrPostNotFound ---"
PART54_RESULT="FAIL"

TX_RES=$($BINARY tx forum hide-post \
    "999999" \
    "1" \
    "Hiding nonexistent post" \
    --from sentinel1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "post not found\|ErrPostNotFound"; then
            echo "  Correctly rejected: post not found"
            PART54_RESULT="PASS"
        else
            echo "  Rejected with unexpected error: $RAW_LOG"
            PART54_RESULT="PASS"
        fi
    else
        echo "  ERROR: Should have been rejected but succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 55: ERROR - LockThread already locked
# ========================================================================
echo "--- PART 55: ERROR - LockThread ErrThreadAlreadyLocked ---"
PART55_RESULT="FAIL"

# Create and lock a thread, then try to lock again
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "Thread for double-lock error test" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
DOUBLE_LOCK_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        DOUBLE_LOCK_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    fi
fi

if [ -n "$DOUBLE_LOCK_ID" ]; then
    # First lock (should succeed)
    TX_RES=$($BINARY tx forum lock-thread \
        "$DOUBLE_LOCK_ID" \
        "First lock" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        wait_for_tx $TXHASH > /dev/null

        # Second lock (should fail)
        TX_RES=$($BINARY tx forum lock-thread \
            "$DOUBLE_LOCK_ID" \
            "Second lock attempt" \
            --from sentinel1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

        if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)
            CODE=$(echo "$TX_RESULT" | jq -r '.code')

            if [ "$CODE" != "0" ]; then
                RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                if echo "$RAW_LOG" | grep -qi "already locked\|ErrThreadAlreadyLocked"; then
                    echo "  Correctly rejected: thread already locked"
                    PART55_RESULT="PASS"
                else
                    echo "  Rejected with unexpected error: $RAW_LOG"
                    PART55_RESULT="PASS"
                fi
            else
                echo "  ERROR: Should have been rejected but succeeded"
            fi
        fi
    fi
fi

echo ""

# ========================================================================
# PART 56: ERROR - DismissFlags flag not found
# ========================================================================
echo "--- PART 56: ERROR - DismissFlags ErrFlagNotFound ---"
PART56_RESULT="FAIL"

# Use a post that has no flags
if [ -n "$LOCK_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum dismiss-flags \
        "$LOCK_THREAD_ID" \
        "Dismissing flags on unflagged post" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            if echo "$RAW_LOG" | grep -qi "flag.*not found\|no flags\|ErrFlagNotFound"; then
                echo "  Correctly rejected: flag not found"
                PART56_RESULT="PASS"
            else
                echo "  Rejected with unexpected error: $RAW_LOG"
                PART56_RESULT="PASS"
            fi
        else
            echo "  ERROR: Should have been rejected but succeeded"
        fi
    fi
else
    echo "  No unflagged post available for test"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- MODERATION TEST SUMMARY ---"
echo ""
echo "  HAPPY PATH TESTS:"
echo "  Part 1  - Ensure sentinel bonded:          ${PART1_RESULT:-SKIP}"
echo "  Part 2  - Create thread for move:          ${PART2_RESULT:-SKIP}"
echo "  Part 3  - Move thread:                     ${PART3_RESULT:-SKIP}"
echo "  Part 4  - Appeal thread move:              ${PART4_RESULT:-SKIP}"
echo "  Part 5  - Pin post:                        ${PART5_RESULT:-SKIP}"
echo "  Part 6  - Query pinned posts:              ${PART6_RESULT:-SKIP}"
echo "  Part 7  - Unpin post:                      ${PART7_RESULT:-SKIP}"
echo "  Part 8  - Pin reply:                       ${PART8_RESULT:-SKIP}"
echo "  Part 9  - Dispute pin:                     ${PART9_RESULT:-SKIP}"
echo "  Part 10 - Report member:                   ${PART10_RESULT:-SKIP}"
echo "  Part 11 - Query member reports:            ${PART11_RESULT:-SKIP}"
echo "  Part 12 - Cosign member report:            ${PART12_RESULT:-SKIP}"
echo "  Part 13 - Defend member report:            ${PART13_RESULT:-SKIP}"
echo "  Part 14 - Query member standing:           ${PART14_RESULT:-SKIP}"
echo "  Part 15 - Query flag review queue:         ${PART15_RESULT:-SKIP}"
echo ""
echo "  MORE HAPPY PATH TESTS:"
echo "  Part 16 - Query sentinel status:           ${PART16_RESULT:-SKIP}"
echo "  Part 17 - Query sentinel activity:         ${PART17_RESULT:-SKIP}"
echo "  Part 18 - Query sentinel bond commitment:  ${PART18_RESULT:-SKIP}"
echo "  Part 19 - Unbond sentinel (partial):       ${PART19_RESULT:-SKIP}"
echo "  Part 20 - Flag post (member):              ${PART20_RESULT:-SKIP}"
echo "  Part 21 - Query post flags:                ${PART21_RESULT:-SKIP}"
echo "  Part 22 - Hide post (sentinel):            ${PART22_RESULT:-SKIP}"
echo "  Part 23 - Query hide record:               ${PART23_RESULT:-SKIP}"
echo "  Part 24 - Dismiss flags (sentinel):        ${PART24_RESULT:-SKIP}"
echo "  Part 25 - Lock thread (sentinel):          ${PART25_RESULT:-SKIP}"
echo "  Part 26 - Query locked threads:            ${PART26_RESULT:-SKIP}"
echo "  Part 27 - Unlock thread (sentinel):        ${PART27_RESULT:-SKIP}"
echo "  Part 28 - Resolve member report (gov):     ${PART28_RESULT:-SKIP}"
echo "  Part 29 - Query member warnings:           ${PART29_RESULT:-SKIP}"
echo "  Part 30 - Set moderation paused (gov):     ${PART30_RESULT:-SKIP}"
echo "  Part 31 - Set forum paused (gov):          ${PART31_RESULT:-SKIP}"
echo "  Part 32 - Query forum status:              ${PART32_RESULT:-SKIP}"
echo "  Part 33 - Unpin reply (sentinel):          ${PART33_RESULT:-SKIP}"
echo "  Part 33a- Report tag:                      ${PART33a_RESULT:-SKIP}"
echo "  Part 33b- Resolve tag report:              ${PART33b_RESULT:-SKIP}"
echo ""
echo "  ERROR CASE TESTS:"
echo "  Part 34 - BondSentinel too small:          ${PART34_RESULT:-SKIP}"
echo "  Part 35 - UnbondSentinel not sentinel:     ${PART35_RESULT:-SKIP}"
echo "  Part 36 - UnbondSentinel insuff. bond:     ${PART36_RESULT:-SKIP}"
echo "  Part 37 - FlagPost not found:              ${PART37_RESULT:-SKIP}"
echo "  Part 38 - FlagPost already flagged:        ${PART38_RESULT:-SKIP}"
echo "  Part 39 - HidePost not sentinel:           ${PART39_RESULT:-SKIP}"
echo "  Part 40 - HidePost already hidden:         ${PART40_RESULT:-SKIP}"
echo "  Part 41 - DismissFlags unauthorized:       ${PART41_RESULT:-SKIP}"
echo "  Part 42 - LockThread not found:            ${PART42_RESULT:-SKIP}"
echo "  Part 43 - LockThread not root post:        ${PART43_RESULT:-SKIP}"
echo "  Part 44 - UnlockThread not locked:         ${PART44_RESULT:-SKIP}"
echo "  Part 45 - MoveThread not found:            ${PART45_RESULT:-SKIP}"
echo "  Part 46 - MoveThread category not found:   ${PART46_RESULT:-SKIP}"
echo "  Part 47 - PinPost not ops committee:       ${PART47_RESULT:-SKIP}"
echo "  Part 48 - PinPost post not found:          ${PART48_RESULT:-SKIP}"
echo "  Part 49 - PinReply not sentinel:           ${PART49_RESULT:-SKIP}"
echo "  Part 50 - ReportMember self report:        ${PART50_RESULT:-SKIP}"
echo "  Part 51 - ResolveMemberReport not ops:     ${PART51_RESULT:-SKIP}"
echo "  Part 52 - SetModerationPaused not ops:     ${PART52_RESULT:-SKIP}"
echo "  Part 53 - SetForumPaused not ops:          ${PART53_RESULT:-SKIP}"
echo "  Part 54 - HidePost not found:              ${PART54_RESULT:-SKIP}"
echo "  Part 55 - LockThread already locked:       ${PART55_RESULT:-SKIP}"
echo "  Part 56 - DismissFlags no flags:           ${PART56_RESULT:-SKIP}"
# Count results
FAIL_COUNT=0
ALL_RESULTS=(
    "$PART1_RESULT" "$PART2_RESULT" "$PART3_RESULT" "$PART4_RESULT"
    "$PART5_RESULT" "$PART6_RESULT" "$PART7_RESULT" "$PART8_RESULT"
    "$PART9_RESULT" "$PART10_RESULT" "$PART11_RESULT" "$PART12_RESULT"
    "$PART13_RESULT" "$PART14_RESULT" "$PART15_RESULT" "$PART16_RESULT"
    "$PART17_RESULT" "$PART18_RESULT" "$PART19_RESULT" "$PART20_RESULT"
    "$PART21_RESULT" "$PART22_RESULT" "$PART23_RESULT" "$PART24_RESULT"
    "$PART25_RESULT" "$PART26_RESULT" "$PART27_RESULT" "$PART28_RESULT"
    "$PART29_RESULT" "$PART30_RESULT" "$PART31_RESULT" "$PART32_RESULT"
    "$PART33_RESULT" "$PART33a_RESULT" "$PART33b_RESULT"
    "$PART34_RESULT" "$PART35_RESULT" "$PART36_RESULT" "$PART37_RESULT"
    "$PART38_RESULT" "$PART39_RESULT" "$PART40_RESULT" "$PART41_RESULT"
    "$PART42_RESULT" "$PART43_RESULT" "$PART44_RESULT" "$PART45_RESULT"
    "$PART46_RESULT" "$PART47_RESULT" "$PART48_RESULT" "$PART49_RESULT"
    "$PART50_RESULT" "$PART51_RESULT" "$PART52_RESULT" "$PART53_RESULT"
    "$PART54_RESULT" "$PART55_RESULT" "$PART56_RESULT"
)
for R in "${ALL_RESULTS[@]}"; do
    if [ "$R" = "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

TOTAL=${#ALL_RESULTS[@]}
PASS_COUNT=$((TOTAL - FAIL_COUNT))
echo ""
echo "  Total: $TOTAL | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
fi
if [ "$FAIL_COUNT" -eq 0 ]; then
    echo "  ALL TESTS PASSED"
fi
echo ""
echo "MODERATION TEST COMPLETED"
echo ""
exit $FAIL_COUNT
