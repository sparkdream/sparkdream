#!/bin/bash

echo "--- TESTING: x/reveal QUERY ENDPOINTS ---"
echo ""

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    source "$SCRIPT_DIR/.test_env"
else
    echo "ERROR: .test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi

# --- Result Tracking ---
PARAMS_RESULT="FAIL"
CONTRIBUTIONS_RESULT="FAIL"
CONTRIBUTION_BY_ID_RESULT="FAIL"
BY_CONTRIBUTOR_RESULT="FAIL"
BY_STATUS_PROPOSED_RESULT="FAIL"
BY_STATUS_CANCELLED_RESULT="FAIL"
BY_STATUS_IN_PROGRESS_RESULT="FAIL"
TRANCHE_QUERY_RESULT="FAIL"
TRANCHE_TALLY_RESULT="FAIL"
TRANCHE_STAKES_RESULT="FAIL"
STAKES_BY_STAKER_RESULT="FAIL"
VOTES_BY_VOTER_RESULT="FAIL"

# ========================================================================
# TEST 1: Query params
# ========================================================================
echo "--- TEST 1: QUERY PARAMS ---"

RESULT=$($BINARY query reveal params --output json 2>&1)
if echo "$RESULT" | jq -e '.params' > /dev/null 2>&1; then
    PARAMS_RESULT="PASS"
    echo "  PASS: Params query works"
else
    echo "  FAIL: Params query failed"
    echo "  Response: $RESULT"
fi
echo ""

# ========================================================================
# TEST 2: Query all contributions (with pagination)
# ========================================================================
echo "--- TEST 2: QUERY ALL CONTRIBUTIONS ---"

RESULT=$($BINARY query reveal contributions --output json 2>&1)
COUNT=$(echo "$RESULT" | jq -r '.contributions // [] | length')
echo "  Found $COUNT contributions"

# Query works even if empty (it returns valid JSON)
if echo "$RESULT" | jq -e '.' > /dev/null 2>&1; then
    CONTRIBUTIONS_RESULT="PASS"
    echo "  PASS: Contributions query works"

    FIRST_CONTRIB_ID=$(echo "$RESULT" | jq -r '.contributions[0].id // empty' 2>/dev/null)
    if [ -n "$FIRST_CONTRIB_ID" ]; then
        echo "  First contribution ID: $FIRST_CONTRIB_ID"
    fi
else
    echo "  FAIL: Contributions query returned invalid JSON"
fi
echo ""

# ========================================================================
# TEST 3: Query single contribution by ID
# ========================================================================
echo "--- TEST 3: QUERY CONTRIBUTION BY ID ---"

if [ -n "$FIRST_CONTRIB_ID" ]; then
    RESULT=$($BINARY query reveal contribution $FIRST_CONTRIB_ID --output json 2>&1)
    if echo "$RESULT" | jq -e '.contribution' > /dev/null 2>&1; then
        PROJECT_NAME=$(echo "$RESULT" | jq -r '.contribution.project_name')
        STATUS=$(echo "$RESULT" | jq -r '.contribution.status')
        echo "  Project: $PROJECT_NAME"
        echo "  Status:  $STATUS"
        CONTRIBUTION_BY_ID_RESULT="PASS"
        echo "  PASS: Single contribution query works"
    else
        echo "  FAIL: Could not query contribution $FIRST_CONTRIB_ID"
    fi
else
    echo "  SKIP: No contribution ID available"
fi
echo ""

# ========================================================================
# TEST 4: Query contributions by contributor
# ========================================================================
echo "--- TEST 4: QUERY CONTRIBUTIONS BY CONTRIBUTOR ---"

RESULT=$($BINARY query reveal contributions-by-contributor $ALICE_ADDR --output json 2>&1)
COUNT=$(echo "$RESULT" | jq -r '.contributions // [] | length')
echo "  Found $COUNT contributions by Alice"
if echo "$RESULT" | jq -e '.' > /dev/null 2>&1; then
    BY_CONTRIBUTOR_RESULT="PASS"
    echo "  PASS: By contributor query works"
else
    echo "  FAIL: By contributor query failed"
    echo "  Response: $RESULT"
fi
echo ""

# ========================================================================
# TEST 5: Query contributions by status (multiple statuses)
# ========================================================================
echo "--- TEST 5a: QUERY BY STATUS - PROPOSED ---"

RESULT=$($BINARY query reveal contributions-by-status proposed --output json 2>&1)
if echo "$RESULT" | jq -e '.' > /dev/null 2>&1; then
    COUNT=$(echo "$RESULT" | jq -r '.contributions // [] | length')
    echo "  Found $COUNT PROPOSED contributions"
    BY_STATUS_PROPOSED_RESULT="PASS"
    echo "  PASS"
else
    echo "  FAIL: Query returned invalid response"
fi
echo ""

echo "--- TEST 5b: QUERY BY STATUS - CANCELLED ---"

RESULT=$($BINARY query reveal contributions-by-status cancelled --output json 2>&1)
if echo "$RESULT" | jq -e '.' > /dev/null 2>&1; then
    COUNT=$(echo "$RESULT" | jq -r '.contributions // [] | length')
    echo "  Found $COUNT CANCELLED contributions"
    BY_STATUS_CANCELLED_RESULT="PASS"
    echo "  PASS"
else
    echo "  FAIL: Query returned invalid response"
fi
echo ""

echo "--- TEST 5c: QUERY BY STATUS - IN_PROGRESS ---"

RESULT=$($BINARY query reveal contributions-by-status in-progress --output json 2>&1)
if echo "$RESULT" | jq -e '.' > /dev/null 2>&1; then
    COUNT=$(echo "$RESULT" | jq -r '.contributions // [] | length')
    echo "  Found $COUNT IN_PROGRESS contributions"
    BY_STATUS_IN_PROGRESS_RESULT="PASS"
    echo "  PASS"
else
    echo "  FAIL: Query returned invalid response"
fi
echo ""

# ========================================================================
# TEST 6: Query tranche detail
# ========================================================================
echo "--- TEST 6: QUERY TRANCHE ---"

# Find an IN_PROGRESS contribution with tranches
IN_PROGRESS_CONTRIB=$($BINARY query reveal contributions-by-status in-progress --output json 2>&1 | \
    jq -r '.contributions[0].id // empty' 2>/dev/null)

if [ -n "$IN_PROGRESS_CONTRIB" ]; then
    RESULT=$($BINARY query reveal tranche $IN_PROGRESS_CONTRIB 0 --output json 2>&1)
    if echo "$RESULT" | jq -e '.tranche' > /dev/null 2>&1; then
        TRANCHE_NAME=$(echo "$RESULT" | jq -r '.tranche.name')
        TRANCHE_STATUS=$(echo "$RESULT" | jq -r '.tranche.status')
        echo "  Tranche name: $TRANCHE_NAME"
        echo "  Tranche status: $TRANCHE_STATUS"
        TRANCHE_QUERY_RESULT="PASS"
        echo "  PASS: Tranche query works"
    else
        echo "  FAIL: Could not query tranche"
        echo "  Response: $RESULT"
    fi
else
    # Try with first contribution
    if [ -n "$FIRST_CONTRIB_ID" ]; then
        RESULT=$($BINARY query reveal tranche $FIRST_CONTRIB_ID 0 --output json 2>&1)
        if echo "$RESULT" | jq -e '.tranche' > /dev/null 2>&1; then
            TRANCHE_QUERY_RESULT="PASS"
            echo "  PASS: Tranche query works"
            IN_PROGRESS_CONTRIB=$FIRST_CONTRIB_ID
        else
            echo "  FAIL: No suitable contribution for tranche query"
        fi
    else
        echo "  SKIP: No contributions available"
    fi
fi
echo ""

# ========================================================================
# TEST 7: Query tranche tally
# ========================================================================
echo "--- TEST 7: QUERY TRANCHE TALLY ---"

if [ -n "$IN_PROGRESS_CONTRIB" ]; then
    RESULT=$($BINARY query reveal tranche-tally $IN_PROGRESS_CONTRIB 0 --output json 2>&1)
    if echo "$RESULT" | jq -e '.yes_weight' > /dev/null 2>&1; then
        YES=$(echo "$RESULT" | jq -r '.yes_weight')
        NO=$(echo "$RESULT" | jq -r '.no_weight')
        VOTES=$(echo "$RESULT" | jq -r '.vote_count')
        echo "  Yes: $YES, No: $NO, Votes: $VOTES"
        TRANCHE_TALLY_RESULT="PASS"
        echo "  PASS: Tally query works"
    else
        echo "  FAIL: Tally query failed"
        echo "  Response: $RESULT"
    fi
else
    echo "  SKIP: No contribution available"
fi
echo ""

# ========================================================================
# TEST 8: Query tranche stakes
# ========================================================================
echo "--- TEST 8: QUERY TRANCHE STAKES ---"

if [ -n "$IN_PROGRESS_CONTRIB" ]; then
    RESULT=$($BINARY query reveal tranche-stakes $IN_PROGRESS_CONTRIB 0 --output json 2>&1)
    if echo "$RESULT" | jq -e '.' > /dev/null 2>&1; then
        COUNT=$(echo "$RESULT" | jq -r '.stakes // [] | length')
        echo "  Found $COUNT stakes for tranche 0"
        TRANCHE_STAKES_RESULT="PASS"
        echo "  PASS: Tranche stakes query works"
    else
        echo "  FAIL: Tranche stakes query failed"
        echo "  Response: $RESULT"
    fi
else
    echo "  SKIP: No contribution available"
fi
echo ""

# ========================================================================
# TEST 9: Query stakes by staker
# ========================================================================
echo "--- TEST 9: QUERY STAKES BY STAKER ---"

RESULT=$($BINARY query reveal stakes-by-staker $STAKER1_ADDR --output json 2>&1)
if echo "$RESULT" | jq -e '.' > /dev/null 2>&1; then
    COUNT=$(echo "$RESULT" | jq -r '.stakes // [] | length')
    echo "  Found $COUNT stakes by staker1"
    STAKES_BY_STAKER_RESULT="PASS"
    echo "  PASS: Stakes by staker query works"
else
    echo "  FAIL: Stakes by staker query failed"
    echo "  Response: $RESULT"
fi
echo ""

# ========================================================================
# TEST 10: Query votes by voter
# ========================================================================
echo "--- TEST 10: QUERY VOTES BY VOTER ---"

RESULT=$($BINARY query reveal votes-by-voter $STAKER1_ADDR --output json 2>&1)
if echo "$RESULT" | jq -e '.' > /dev/null 2>&1; then
    COUNT=$(echo "$RESULT" | jq -r '.votes // [] | length')
    echo "  Found $COUNT votes by staker1"
    VOTES_BY_VOTER_RESULT="PASS"
    echo "  PASS: Votes by voter query works"
else
    echo "  FAIL: Votes by voter query failed"
    echo "  Response: $RESULT"
fi
echo ""

# --- RESULTS SUMMARY ---
echo "============================================================================"
echo "  REVEAL QUERY TEST RESULTS"
echo "============================================================================"
echo ""

TOTAL_COUNT=0
PASS_COUNT=0
FAIL_COUNT=0

for RESULT in "$PARAMS_RESULT" "$CONTRIBUTIONS_RESULT" "$CONTRIBUTION_BY_ID_RESULT" "$BY_CONTRIBUTOR_RESULT" "$BY_STATUS_PROPOSED_RESULT" "$BY_STATUS_CANCELLED_RESULT" "$BY_STATUS_IN_PROGRESS_RESULT" "$TRANCHE_QUERY_RESULT" "$TRANCHE_TALLY_RESULT" "$TRANCHE_STAKES_RESULT" "$STAKES_BY_STAKER_RESULT" "$VOTES_BY_VOTER_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

echo "  1.  Params:                     $PARAMS_RESULT"
echo "  2.  All Contributions:           $CONTRIBUTIONS_RESULT"
echo "  3.  Contribution by ID:          $CONTRIBUTION_BY_ID_RESULT"
echo "  4.  By Contributor:              $BY_CONTRIBUTOR_RESULT"
echo "  5a. By Status (PROPOSED):        $BY_STATUS_PROPOSED_RESULT"
echo "  5b. By Status (CANCELLED):       $BY_STATUS_CANCELLED_RESULT"
echo "  5c. By Status (IN_PROGRESS):     $BY_STATUS_IN_PROGRESS_RESULT"
echo "  6.  Tranche Detail:              $TRANCHE_QUERY_RESULT"
echo "  7.  Tranche Tally:               $TRANCHE_TALLY_RESULT"
echo "  8.  Tranche Stakes:              $TRANCHE_STAKES_RESULT"
echo "  9.  Stakes by Staker:            $STAKES_BY_STAKER_RESULT"
echo "  10. Votes by Voter:              $VOTES_BY_VOTER_RESULT"
echo ""
echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
