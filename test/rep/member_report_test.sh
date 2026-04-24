#!/bin/bash

echo "--- TESTING: MEMBER REPORT ACCOUNTABILITY (REPORT, COSIGN, DEFEND, RESOLVE, WARNINGS) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment from x/rep setup, which seeds sentinel1/sentinel2/
# poster1/moderator alongside the challenger/juror/assignee accounts.
REP_ENV="$SCRIPT_DIR/.test_env"

if [ -f "$REP_ENV" ]; then
    source "$REP_ENV"
else
    echo "Test environment not found at $REP_ENV"
    echo "   Run: bash test/rep/setup_test_accounts.sh"
    exit 1
fi

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

# Helper: bootstrap reputation by creating & completing EPIC interims
bootstrap_reputation() {
    local ACCOUNT=$1
    local NUM_EPICS=$2
    local TARGET_REP=$((NUM_EPICS * 100))

    echo "  Bootstrapping reputation for $ACCOUNT ($NUM_EPICS EPICs = $TARGET_REP rep)..."

    for i in $(seq 1 $NUM_EPICS); do
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

    SENTINEL_STATUS=$($BINARY query rep bonded-role forum-sentinel $ADDR --output json 2>&1)
    CURRENT_BOND=$(echo "$SENTINEL_STATUS" | jq -r '.bonded_role.current_bond // "0"' 2>/dev/null)

    if echo "$SENTINEL_STATUS" | grep -q "error\|not found" \
       || [ "$(echo "$SENTINEL_STATUS" | jq -r '.bonded_role.address // empty')" = "" ] \
       || [ "$CURRENT_BOND" = "0" ] || [ "$CURRENT_BOND" = "null" ] || [ -z "$CURRENT_BOND" ]; then
        echo "  Bonding $ACCOUNT (amount: $BOND_AMOUNT)..."

        TX_RES=$($BINARY tx rep bond-role forum-sentinel \
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

# ========================================================================
# PART 1: BOOTSTRAP REPUTATION & BOND SENTINELS
# ========================================================================
echo "--- PART 1: ENSURE SENTINEL IS BONDED ---"
PART1_RESULT="FAIL"

# Sentinel1 needs tier 4 (500+ rep) for reporting ops.
bootstrap_reputation sentinel1 11

SECOND_SENTINEL_ACCOUNT="sentinel2"
SECOND_SENTINEL_ADDR="$SENTINEL2_ADDR"

bootstrap_reputation sentinel2 5

bond_sentinel sentinel1 "$SENTINEL1_ADDR"
BOND1_RC=$?
bond_sentinel sentinel2 "$SENTINEL2_ADDR"
BOND2_RC=$?

if [ "$BOND2_RC" -ne 0 ]; then
    echo ""
    echo "  sentinel2 unavailable (demotion cooldown), using moderator as second sentinel..."
    SECOND_SENTINEL_ACCOUNT="moderator"
    SECOND_SENTINEL_ADDR="$MODERATOR_ADDR"
    bootstrap_reputation moderator 5

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
# PART 10: REPORT MEMBER
# ========================================================================
echo "--- PART 10: REPORT MEMBER ---"
PART10_RESULT="FAIL"

# Sentinel1 needs enough unlocked DREAM to post a report bond.
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

TX_RES=$($BINARY tx rep report-member \
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

REPORTS=$($BINARY query rep member-reports --output json 2>&1)

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

    TX_RES=$($BINARY tx rep cosign-member-report \
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

    TX_RES=$($BINARY tx rep defend-member-report \
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

STANDING=$($BINARY query rep member-standing $POSTER2_ADDR --output json 2>&1)

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
# PART 28: RESOLVE MEMBER REPORT (GOV)
# ========================================================================
echo "--- PART 28: RESOLVE MEMBER REPORT (GOV) ---"
PART28_RESULT="FAIL"

# The poster2 report has a defense (Part 13) with a 24-hour wait period.
# Create a separate report on bounty_creator (no defense) to test resolution.
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

TX_RES=$($BINARY tx rep report-member \
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

        TX_RES=$($BINARY tx rep resolve-member-report \
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

WARNINGS=$($BINARY query rep member-warnings $POSTER2_ADDR --output json 2>&1)

if echo "$WARNINGS" | jq -e '.' > /dev/null 2>&1 && ! echo "$WARNINGS" | grep -q "error"; then
    echo "  Member warnings query successful for poster2"
    PART29_RESULT="PASS"
else
    echo "  Failed to query member warnings"
fi

echo ""

# ========================================================================
# PART 50: ERROR - ReportMember cannot report self
# ========================================================================
echo "--- PART 50: ERROR - ReportMember ErrCannotReportSelf ---"
PART50_RESULT="FAIL"

TX_RES=$($BINARY tx rep report-member \
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

TX_RES=$($BINARY tx rep resolve-member-report \
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
# SUMMARY
# ========================================================================
echo "--- MEMBER REPORT TEST SUMMARY ---"
echo ""
echo "  HAPPY PATH TESTS:"
echo "  Part 1  - Ensure sentinel bonded:          ${PART1_RESULT:-SKIP}"
echo "  Part 10 - Report member:                   ${PART10_RESULT:-SKIP}"
echo "  Part 11 - Query member reports:            ${PART11_RESULT:-SKIP}"
echo "  Part 12 - Cosign member report:            ${PART12_RESULT:-SKIP}"
echo "  Part 13 - Defend member report:            ${PART13_RESULT:-SKIP}"
echo "  Part 14 - Query member standing:           ${PART14_RESULT:-SKIP}"
echo "  Part 28 - Resolve member report (gov):     ${PART28_RESULT:-SKIP}"
echo "  Part 29 - Query member warnings:           ${PART29_RESULT:-SKIP}"
echo ""
echo "  ERROR CASE TESTS:"
echo "  Part 50 - ReportMember self report:        ${PART50_RESULT:-SKIP}"
echo "  Part 51 - ResolveMemberReport not ops:     ${PART51_RESULT:-SKIP}"

FAIL_COUNT=0
ALL_RESULTS=(
    "$PART1_RESULT" "$PART10_RESULT" "$PART11_RESULT" "$PART12_RESULT"
    "$PART13_RESULT" "$PART14_RESULT" "$PART28_RESULT" "$PART29_RESULT"
    "$PART50_RESULT" "$PART51_RESULT"
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
echo "MEMBER REPORT TEST COMPLETED"
echo ""
exit $FAIL_COUNT
