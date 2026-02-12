#!/bin/bash

echo "--- TESTING: CATEGORIES (CREATE, QUERY, LIST, ACCESS CONTROL) ---"

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

echo "Alice:    $ALICE_ADDR"
echo "Poster1:  $POSTER1_ADDR"
echo "Poster2:  $POSTER2_ADDR"
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

# Submit a tx and wait for result. Sets TX_RESULT and returns 0 on submission success.
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

# Expect the tx to be rejected (either at submission or with non-zero code).
# Sets the given result variable to PASS if rejected, FAIL if it succeeded.
# Usage: expect_tx_failure "$TX_RES" "RESULT_VAR_NAME" "description"
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

# ========================================================================
# PART 1: LIST EXISTING CATEGORIES
# ========================================================================
echo "--- PART 1: LIST EXISTING CATEGORIES ---"

CATEGORIES=$($BINARY query forum list-category --output json 2>&1)

if echo "$CATEGORIES" | grep -q "error"; then
    echo "  Failed to query categories"
    INITIAL_CATEGORY_COUNT=0
    LIST_CATEGORIES_RESULT="FAIL"
else
    INITIAL_CATEGORY_COUNT=$(echo "$CATEGORIES" | jq -r '.category | length' 2>/dev/null || echo "0")
    echo "  Existing categories: $INITIAL_CATEGORY_COUNT"
    LIST_CATEGORIES_RESULT="PASS"

    if [ "$INITIAL_CATEGORY_COUNT" -gt 0 ]; then
        echo ""
        echo "  Categories:"
        echo "$CATEGORIES" | jq -r '.category[] | "    - ID \(.category_id): \(.title)"' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 2: CREATE A NEW CATEGORY (Requires Authority)
# ========================================================================
echo "--- PART 2: CREATE A NEW CATEGORY (Authority Required) ---"

CATEGORY_TITLE="Tech Discussion $(date +%s)"
CATEGORY_DESC="A category for technical discussions"

echo "Attempting to create category: $CATEGORY_TITLE"

TX_RES=$($BINARY tx forum create-category \
    "$CATEGORY_TITLE" \
    "$CATEGORY_DESC" \
    "true" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    NEW_CATEGORY_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")

    if [ -z "$NEW_CATEGORY_ID" ] || [ "$NEW_CATEGORY_ID" == "null" ]; then
        # Fallback: query latest category
        CATEGORIES=$($BINARY query forum list-category --output json 2>&1)
        NEW_CATEGORY_ID=$(echo "$CATEGORIES" | jq -r '.category[-1].category_id // empty')
    fi

    echo "  Category created successfully (ID: $NEW_CATEGORY_ID)"
    CREATE_CATEGORY_RESULT="PASS"
else
    echo "  Failed to create category"
    NEW_CATEGORY_ID=""
    CREATE_CATEGORY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 3: QUERY CATEGORY DETAILS
# ========================================================================
echo "--- PART 3: QUERY CATEGORY DETAILS ---"

# Use existing category if we couldn't create one
QUERY_CATEGORY_ID="${NEW_CATEGORY_ID:-$TEST_CATEGORY_ID}"

if [ -n "$QUERY_CATEGORY_ID" ]; then
    CATEGORY_INFO=$($BINARY query forum get-category $QUERY_CATEGORY_ID --output json 2>&1)

    if echo "$CATEGORY_INFO" | grep -q "error\|not found"; then
        echo "  Category $QUERY_CATEGORY_ID not found"
        QUERY_CATEGORY_RESULT="FAIL"
    else
        QUERIED_TITLE=$(echo "$CATEGORY_INFO" | jq -r '.category.title')
        echo "  Category Details:"
        echo "    ID: $(echo "$CATEGORY_INFO" | jq -r '.category.category_id')"
        echo "    Title: $QUERIED_TITLE"
        echo "    Description: $(echo "$CATEGORY_INFO" | jq -r '.category.description')"
        echo "    Members Only Write: $(echo "$CATEGORY_INFO" | jq -r '.category.members_only_write // false')"
        echo "    Admin Only Write: $(echo "$CATEGORY_INFO" | jq -r '.category.admin_only_write // false')"

        if [ -n "$QUERIED_TITLE" ] && [ "$QUERIED_TITLE" != "null" ]; then
            echo "  Query returned valid category (correct)"
            QUERY_CATEGORY_RESULT="PASS"
        else
            echo "  ERROR: Query returned category with no title"
            QUERY_CATEGORY_RESULT="FAIL"
        fi
    fi
else
    echo "  No category ID available"
    QUERY_CATEGORY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 4: QUERY CATEGORIES LIST (with details)
# ========================================================================
echo "--- PART 4: QUERY CATEGORIES LIST (with details) ---"

CATEGORIES=$($BINARY query forum list-category --output json 2>&1)

if echo "$CATEGORIES" | grep -q "error"; then
    echo "  Failed to query categories"
    LIST_DETAILS_RESULT="FAIL"
else
    CAT_COUNT=$(echo "$CATEGORIES" | jq -r '.category | length' 2>/dev/null || echo "0")
    echo "  All categories ($CAT_COUNT):"
    echo "$CATEGORIES" | jq -r '.category[] | "    - \(.category_id): \(.title) (members_only=\(.members_only_write // false), admin_only=\(.admin_only_write // false))"' 2>/dev/null

    if [ "$CAT_COUNT" -gt 0 ]; then
        LIST_DETAILS_RESULT="PASS"
    else
        echo "  ERROR: No categories found"
        LIST_DETAILS_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 5: CREATE MEMBERS-ONLY CATEGORY
# ========================================================================
echo "--- PART 5: CREATE MEMBERS-ONLY CATEGORY ---"

MEMBERS_CATEGORY_TITLE="Members Only $(date +%s)"
MEMBERS_CATEGORY_DESC="Only members can post here"

echo "Creating members-only category: $MEMBERS_CATEGORY_TITLE"

TX_RES=$($BINARY tx forum create-category \
    "$MEMBERS_CATEGORY_TITLE" \
    "$MEMBERS_CATEGORY_DESC" \
    "true" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    MEMBERS_CATEGORY_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
    echo "  Members-only category created (ID: $MEMBERS_CATEGORY_ID)"

    # Verify the flag was set
    MEMBERS_INFO=$($BINARY query forum get-category $MEMBERS_CATEGORY_ID --output json 2>&1)
    MEMBERS_FLAG=$(echo "$MEMBERS_INFO" | jq -r '.category.members_only_write // false')

    if [ "$MEMBERS_FLAG" == "true" ]; then
        echo "  members_only_write=true (correct)"
        CREATE_MEMBERS_RESULT="PASS"
    else
        echo "  ERROR: members_only_write flag is not true"
        CREATE_MEMBERS_RESULT="FAIL"
    fi
else
    echo "  Failed to create members-only category"
    MEMBERS_CATEGORY_ID=""
    CREATE_MEMBERS_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 6: CREATE ADMIN-ONLY CATEGORY
# ========================================================================
echo "--- PART 6: CREATE ADMIN-ONLY CATEGORY ---"

ADMIN_CATEGORY_TITLE="Announcements $(date +%s)"
ADMIN_CATEGORY_DESC="Only admins can post here"

echo "Creating admin-only category: $ADMIN_CATEGORY_TITLE"

TX_RES=$($BINARY tx forum create-category \
    "$ADMIN_CATEGORY_TITLE" \
    "$ADMIN_CATEGORY_DESC" \
    "false" \
    "true" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    ADMIN_CATEGORY_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
    echo "  Admin-only category created (ID: $ADMIN_CATEGORY_ID)"

    # Verify the flag was set
    ADMIN_INFO=$($BINARY query forum get-category $ADMIN_CATEGORY_ID --output json 2>&1)
    ADMIN_FLAG=$(echo "$ADMIN_INFO" | jq -r '.category.admin_only_write // false')

    if [ "$ADMIN_FLAG" == "true" ]; then
        echo "  admin_only_write=true (correct)"
        CREATE_ADMIN_RESULT="PASS"
    else
        echo "  ERROR: admin_only_write flag is not true"
        CREATE_ADMIN_RESULT="FAIL"
    fi
else
    echo "  Failed to create admin-only category"
    ADMIN_CATEGORY_ID=""
    CREATE_ADMIN_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 7: VERIFY CATEGORY COUNT INCREASED
# ========================================================================
echo "--- PART 7: VERIFY CATEGORY COUNT INCREASED ---"

CATEGORIES=$($BINARY query forum list-category --output json 2>&1)

if echo "$CATEGORIES" | grep -q "error"; then
    echo "  Failed to query categories"
    COUNT_VERIFY_RESULT="FAIL"
else
    MID_CATEGORY_COUNT=$(echo "$CATEGORIES" | jq -r '.category | length' 2>/dev/null || echo "0")
    CREATED=$((MID_CATEGORY_COUNT - INITIAL_CATEGORY_COUNT))
    echo "  Total categories: $MID_CATEGORY_COUNT"
    echo "  Categories created in test so far: $CREATED"

    if [ "$CREATED" -ge 3 ] 2>/dev/null; then
        echo "  Count increased by at least 3 (correct)"
        COUNT_VERIFY_RESULT="PASS"
    else
        echo "  ERROR: Expected at least 3 new categories, got $CREATED"
        COUNT_VERIFY_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 8: CATEGORIES SIMPLIFIED QUERY
# ========================================================================
echo "--- PART 8: CATEGORIES SIMPLIFIED QUERY ---"

CATEGORIES_SIMPLE=$($BINARY query forum categories --output json 2>&1)

if echo "$CATEGORIES_SIMPLE" | grep -q "error"; then
    echo "  Failed to query categories (simplified)"
    CATEGORIES_SIMPLE_RESULT="FAIL"
else
    SIMPLE_ID=$(echo "$CATEGORIES_SIMPLE" | jq -r '.category_id // empty')
    SIMPLE_TITLE=$(echo "$CATEGORIES_SIMPLE" | jq -r '.title // empty')

    if [ -n "$SIMPLE_ID" ] && [ "$SIMPLE_ID" != "0" ]; then
        echo "  Simplified query returned:"
        echo "    Category ID: $SIMPLE_ID"
        echo "    Title: $SIMPLE_TITLE"
        CATEGORIES_SIMPLE_RESULT="PASS"
    else
        echo "  Simplified query returned empty/zero (no categories?)"
        CATEGORIES_SIMPLE_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 9: CREATE OPEN CATEGORY (both flags false) AND VERIFY
# ========================================================================
echo "--- PART 9: CREATE OPEN CATEGORY (both flags false) ---"

OPEN_CATEGORY_TITLE="Open Forum $(date +%s)"
OPEN_CATEGORY_DESC="Anyone can post here"

echo "Creating open category: $OPEN_CATEGORY_TITLE"

TX_RES=$($BINARY tx forum create-category \
    "$OPEN_CATEGORY_TITLE" \
    "$OPEN_CATEGORY_DESC" \
    "false" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    OPEN_CATEGORY_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
    echo "  Open category created (ID: $OPEN_CATEGORY_ID)"

    # Verify both flags are false
    OPEN_INFO=$($BINARY query forum get-category $OPEN_CATEGORY_ID --output json 2>&1)
    OPEN_MEMBERS=$(echo "$OPEN_INFO" | jq -r '.category.members_only_write // false')
    OPEN_ADMIN=$(echo "$OPEN_INFO" | jq -r '.category.admin_only_write // false')

    echo "  members_only_write: $OPEN_MEMBERS"
    echo "  admin_only_write: $OPEN_ADMIN"

    if [ "$OPEN_MEMBERS" == "false" ] && [ "$OPEN_ADMIN" == "false" ]; then
        echo "  Both flags correctly set to false"
        OPEN_CATEGORY_RESULT="PASS"
    else
        echo "  ERROR: Flags not set correctly"
        OPEN_CATEGORY_RESULT="FAIL"
    fi
else
    echo "  Failed to create open category"
    OPEN_CATEGORY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 10: PAGINATION FOR LIST-CATEGORY
# ========================================================================
echo "--- PART 10: PAGINATION FOR LIST-CATEGORY ---"

echo "Querying categories with --page-limit 2..."

PAGE1=$($BINARY query forum list-category --page-limit 2 --output json 2>&1)

if echo "$PAGE1" | grep -q "error"; then
    echo "  Failed to query with pagination"
    PAGINATION_RESULT="FAIL"
else
    PAGE1_COUNT=$(echo "$PAGE1" | jq -r '.category | length' 2>/dev/null || echo "0")
    NEXT_KEY=$(echo "$PAGE1" | jq -r '.pagination.next_key // empty')

    echo "  Page 1: $PAGE1_COUNT categories"

    if [ "$PAGE1_COUNT" -le 2 ]; then
        echo "  Limit respected (got $PAGE1_COUNT, limit was 2)"
    else
        echo "  ERROR: Limit not respected (got $PAGE1_COUNT, limit was 2)"
    fi

    if [ -n "$NEXT_KEY" ] && [ "$NEXT_KEY" != "null" ]; then
        echo "  Next key present: $NEXT_KEY"
        echo "  Querying page 2..."

        PAGE2=$($BINARY query forum list-category --page-key "$NEXT_KEY" --output json 2>&1)

        if echo "$PAGE2" | grep -q "error"; then
            echo "  Failed to query page 2"
            PAGINATION_RESULT="FAIL"
        else
            PAGE2_COUNT=$(echo "$PAGE2" | jq -r '.category | length' 2>/dev/null || echo "0")
            echo "  Page 2: $PAGE2_COUNT categories"
            PAGINATION_RESULT="PASS"
        fi
    else
        echo "  No next page (all categories fit in one page of 2)"
        # Still a pass if limit was respected
        if [ "$PAGE1_COUNT" -le 2 ]; then
            PAGINATION_RESULT="PASS"
        else
            PAGINATION_RESULT="FAIL"
        fi
    fi
fi

echo ""

# ========================================================================
# PART 11: CREATE CATEGORY WITH EMPTY DESCRIPTION (valid)
# ========================================================================
echo "--- PART 11: CREATE CATEGORY WITH EMPTY DESCRIPTION (valid) ---"

echo "Creating category with empty description..."

TX_RES=$($BINARY tx forum create-category \
    "No Description $(date +%s)" \
    "" \
    "false" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    EMPTY_DESC_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
    echo "  Category with empty description created (ID: $EMPTY_DESC_ID)"
    EMPTY_DESC_CATEGORY_RESULT="PASS"
else
    echo "  Failed to create category with empty description"
    EMPTY_DESC_CATEGORY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 12: CREATE CATEGORY WITH BOTH FLAGS TRUE
# ========================================================================
echo "--- PART 12: CREATE CATEGORY WITH BOTH FLAGS TRUE ---"

echo "Creating category with members_only=true AND admin_only=true..."

TX_RES=$($BINARY tx forum create-category \
    "Both Flags $(date +%s)" \
    "Both members_only and admin_only set to true" \
    "true" \
    "true" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    BOTH_FLAGS_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
    echo "  Category created (ID: $BOTH_FLAGS_ID)"

    BOTH_INFO=$($BINARY query forum get-category $BOTH_FLAGS_ID --output json 2>&1)
    BOTH_MEMBERS=$(echo "$BOTH_INFO" | jq -r '.category.members_only_write // false')
    BOTH_ADMIN=$(echo "$BOTH_INFO" | jq -r '.category.admin_only_write // false')

    echo "  members_only_write: $BOTH_MEMBERS"
    echo "  admin_only_write: $BOTH_ADMIN"

    if [ "$BOTH_MEMBERS" == "true" ] && [ "$BOTH_ADMIN" == "true" ]; then
        echo "  Both flags correctly set to true"
        BOTH_FLAGS_RESULT="PASS"
    else
        echo "  ERROR: Flags not set correctly"
        BOTH_FLAGS_RESULT="FAIL"
    fi
else
    echo "  Failed to create category with both flags"
    BOTH_FLAGS_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 13: DUPLICATE TITLE (should succeed - no uniqueness constraint)
# ========================================================================
echo "--- PART 13: DUPLICATE TITLE CREATION ---"

DUP_TITLE="Duplicate Title Test $(date +%s)"

echo "Creating two categories with the same title: $DUP_TITLE"

TX_RES=$($BINARY tx forum create-category \
    "$DUP_TITLE" \
    "First category with this title" \
    "false" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DUP_ID1=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
    echo "  First category created (ID: $DUP_ID1)"

    TX_RES=$($BINARY tx forum create-category \
        "$DUP_TITLE" \
        "Second category with the same title" \
        "false" \
        "false" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        DUP_ID2=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
        echo "  Second category created (ID: $DUP_ID2)"

        if [ "$DUP_ID1" != "$DUP_ID2" ]; then
            echo "  Different IDs assigned (correct - no uniqueness constraint)"
            DUPLICATE_TITLE_RESULT="PASS"
        else
            echo "  ERROR: Both categories got the same ID"
            DUPLICATE_TITLE_RESULT="FAIL"
        fi
    else
        echo "  Second duplicate creation failed unexpectedly"
        DUPLICATE_TITLE_RESULT="FAIL"
    fi
else
    echo "  First creation failed, cannot test duplicates"
    DUPLICATE_TITLE_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 14: TITLE AT BOUNDARY (256 chars - should succeed)
# ========================================================================
echo "--- PART 14: TITLE AT BOUNDARY (256 chars) ---"

BOUNDARY_TITLE=$(python3 -c "print('T' * 256)" 2>/dev/null || printf 'T%.0s' $(seq 1 256))
echo "Creating category with ${#BOUNDARY_TITLE}-char title (limit is 256)..."

TX_RES=$($BINARY tx forum create-category \
    "$BOUNDARY_TITLE" \
    "Boundary test description" \
    "false" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    BOUNDARY_TITLE_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
    echo "  Category created at boundary (ID: $BOUNDARY_TITLE_ID)"
    TITLE_BOUNDARY_RESULT="PASS"
else
    echo "  Failed - 256-char title should be accepted"
    TITLE_BOUNDARY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 15: DESCRIPTION AT BOUNDARY (2048 chars - should succeed)
# ========================================================================
echo "--- PART 15: DESCRIPTION AT BOUNDARY (2048 chars) ---"

BOUNDARY_DESC=$(python3 -c "print('D' * 2048)" 2>/dev/null || printf 'D%.0s' $(seq 1 2048))
echo "Creating category with ${#BOUNDARY_DESC}-char description (limit is 2048)..."

TX_RES=$($BINARY tx forum create-category \
    "Desc Boundary $(date +%s)" \
    "$BOUNDARY_DESC" \
    "false" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    BOUNDARY_DESC_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
    echo "  Category created at boundary (ID: $BOUNDARY_DESC_ID)"
    DESC_BOUNDARY_RESULT="PASS"
else
    echo "  Failed - 2048-char description should be accepted"
    DESC_BOUNDARY_RESULT="FAIL"
fi

echo ""

# ########################################################################
#
#   NEGATIVE PATH TESTS
#
# ########################################################################

echo "========================================================================"
echo "  NEGATIVE PATH TESTS"
echo "========================================================================"
echo ""

# ========================================================================
# NEG 1: UNAUTHORIZED CATEGORY CREATION
# ========================================================================
echo "--- NEG 1: UNAUTHORIZED CATEGORY CREATION ---"

echo "Attempting to create category as poster1 (non-authority)..."

TX_RES=$($BINARY tx forum create-category \
    "Unauthorized Category $(date +%s)" \
    "This should fail" \
    "false" \
    "false" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "NEG_UNAUTHORIZED_RESULT" "unauthorized user created a category!"

echo ""

# ========================================================================
# NEG 2: EMPTY TITLE
# ========================================================================
echo "--- NEG 2: EMPTY TITLE ---"

echo "Attempting to create category with empty title..."

TX_RES=$($BINARY tx forum create-category \
    "" \
    "Description without a title" \
    "false" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "NEG_EMPTY_TITLE_RESULT" "empty title was accepted!"

echo ""

# ========================================================================
# NEG 3: TITLE TOO LONG (>256 chars)
# ========================================================================
echo "--- NEG 3: TITLE TOO LONG ---"

LONG_TITLE=$(python3 -c "print('A' * 257)" 2>/dev/null || printf 'A%.0s' $(seq 1 257))
echo "Attempting to create category with ${#LONG_TITLE}-char title..."

TX_RES=$($BINARY tx forum create-category \
    "$LONG_TITLE" \
    "Description for long title test" \
    "false" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "NEG_LONG_TITLE_RESULT" "title >256 chars was accepted!"

echo ""

# ========================================================================
# NEG 4: DESCRIPTION TOO LONG (>2048 chars)
# ========================================================================
echo "--- NEG 4: DESCRIPTION TOO LONG ---"

LONG_DESC=$(python3 -c "print('B' * 2049)" 2>/dev/null || printf 'B%.0s' $(seq 1 2049))
echo "Attempting to create category with ${#LONG_DESC}-char description..."

TX_RES=$($BINARY tx forum create-category \
    "Valid Title $(date +%s)" \
    "$LONG_DESC" \
    "false" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "NEG_LONG_DESC_RESULT" "description >2048 chars was accepted!"

echo ""

# ========================================================================
# NEG 5: QUERY NON-EXISTENT CATEGORY
# ========================================================================
echo "--- NEG 5: QUERY NON-EXISTENT CATEGORY ---"

echo "Querying category ID 999999 (should not exist)..."

NONEXISTENT=$($BINARY query forum get-category 999999 --output json 2>&1)

if echo "$NONEXISTENT" | grep -qi "not found\|does not exist\|error"; then
    echo "  Correctly returned error for non-existent category"
    echo "  Response: $(echo "$NONEXISTENT" | head -1)"
    NEG_NONEXISTENT_RESULT="PASS"
else
    echo "  ERROR: Query returned a result for non-existent category!"
    echo "  $NONEXISTENT"
    NEG_NONEXISTENT_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 6: POST IN ADMIN-ONLY CATEGORY BY NON-ADMIN
# ========================================================================
echo "--- NEG 6: POST IN ADMIN-ONLY CATEGORY BY NON-ADMIN ---"

if [ -n "$ADMIN_CATEGORY_ID" ]; then
    echo "Attempting to post in admin-only category $ADMIN_CATEGORY_ID as poster1..."

    TX_RES=$($BINARY tx forum create-post \
        "$ADMIN_CATEGORY_ID" \
        "0" \
        "This should fail - admin only category" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "NEG_POST_ADMIN_ONLY_RESULT" "non-admin posted in admin-only category!"
else
    echo "  No admin-only category available, skipping"
    NEG_POST_ADMIN_ONLY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 7: POST IN MEMBERS-ONLY CATEGORY BY NON-MEMBER
# ========================================================================
echo "--- NEG 7: POST IN MEMBERS-ONLY CATEGORY BY NON-MEMBER ---"

if [ -n "$MEMBERS_CATEGORY_ID" ]; then
    # Create a non-member account for this test
    NON_MEMBER_EXISTS=$($BINARY keys show nonmember --keyring-backend test 2>/dev/null)

    if [ -z "$NON_MEMBER_EXISTS" ]; then
        $BINARY keys add nonmember --keyring-backend test --output json > /dev/null 2>&1
    fi

    NON_MEMBER_ADDR=$($BINARY keys show nonmember -a --keyring-backend test)

    # Fund the non-member so they can pay gas
    TX_RES=$($BINARY tx bank send \
        alice $NON_MEMBER_ADDR \
        1000000uspark \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    submit_tx_and_wait "$TX_RES" > /dev/null 2>&1

    echo "Attempting to post in members-only category $MEMBERS_CATEGORY_ID as non-member..."

    TX_RES=$($BINARY tx forum create-post \
        "$MEMBERS_CATEGORY_ID" \
        "0" \
        "This should fail - members only category" \
        --from nonmember \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "NEG_POST_MEMBERS_ONLY_RESULT" "non-member posted in members-only category!"
else
    echo "  No members-only category available, skipping"
    NEG_POST_MEMBERS_ONLY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 8: POST IN ADMIN-ONLY CATEGORY BY MEMBER (non-admin)
# ========================================================================
echo "--- NEG 8: POST IN ADMIN-ONLY CATEGORY BY MEMBER (non-admin) ---"

if [ -n "$ADMIN_CATEGORY_ID" ]; then
    echo "Attempting to post in admin-only category $ADMIN_CATEGORY_ID as poster2 (member, not admin)..."

    TX_RES=$($BINARY tx forum create-post \
        "$ADMIN_CATEGORY_ID" \
        "0" \
        "This should fail - admin only even for members" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "NEG_POST_ADMIN_MEMBER_RESULT" "member posted in admin-only category!"
else
    echo "  No admin-only category available, skipping"
    NEG_POST_ADMIN_MEMBER_RESULT="FAIL"
fi

echo ""

# ========================================================================
# VERIFICATION: POST IN MEMBERS-ONLY CATEGORY BY MEMBER (should succeed)
# ========================================================================
echo "--- VERIFY: POST IN MEMBERS-ONLY CATEGORY BY MEMBER ---"

if [ -n "$MEMBERS_CATEGORY_ID" ]; then
    echo "Posting in members-only category $MEMBERS_CATEGORY_ID as poster1 (member)..."

    TX_RES=$($BINARY tx forum create-post \
        "$MEMBERS_CATEGORY_ID" \
        "0" \
        "Members can post here - this should succeed" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Member posted in members-only category successfully (correct)"
        VERIFY_MEMBER_POST_RESULT="PASS"
    else
        echo "  Failed - members should be able to post in members-only category"
        VERIFY_MEMBER_POST_RESULT="FAIL"
    fi
else
    echo "  No members-only category available, skipping"
    VERIFY_MEMBER_POST_RESULT="FAIL"
fi

echo ""

# ========================================================================
# VERIFICATION: POST IN ADMIN-ONLY CATEGORY BY AUTHORITY (should succeed)
# ========================================================================
echo "--- VERIFY: POST IN ADMIN-ONLY CATEGORY BY AUTHORITY ---"

if [ -n "$ADMIN_CATEGORY_ID" ]; then
    echo "Posting in admin-only category $ADMIN_CATEGORY_ID as alice (authority)..."

    TX_RES=$($BINARY tx forum create-post \
        "$ADMIN_CATEGORY_ID" \
        "0" \
        "Authority can post here - this should succeed" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Authority posted in admin-only category successfully (correct)"
        VERIFY_ADMIN_POST_RESULT="PASS"
    else
        echo "  Failed - authority should be able to post in admin-only category"
        VERIFY_ADMIN_POST_RESULT="FAIL"
    fi
else
    echo "  No admin-only category available, skipping"
    VERIFY_ADMIN_POST_RESULT="FAIL"
fi

echo ""

# ========================================================================
# FINAL CATEGORY COUNT VERIFICATION
# ========================================================================
echo "--- FINAL: CATEGORY COUNT VERIFICATION ---"

CATEGORIES=$($BINARY query forum list-category --output json 2>&1)

if echo "$CATEGORIES" | grep -q "error"; then
    echo "  Failed to query categories"
    FINAL_COUNT_RESULT="FAIL"
else
    FINAL_CATEGORY_COUNT=$(echo "$CATEGORIES" | jq -r '.category | length' 2>/dev/null || echo "0")
    TOTAL_CREATED=$((FINAL_CATEGORY_COUNT - INITIAL_CATEGORY_COUNT))
    echo "  Total categories: $FINAL_CATEGORY_COUNT"
    echo "  Categories created in test: $TOTAL_CREATED"

    echo ""
    echo "  All categories:"
    echo "$CATEGORIES" | jq -r '.category[] | "    - \(.category_id): \(.title) (members_only=\(.members_only_write // false), admin_only=\(.admin_only_write // false))"' 2>/dev/null

    # We created: tech, members-only, admin-only, open, empty-desc, both-flags,
    # dup1, dup2, boundary-title, boundary-desc = 10 categories
    # Negative tests should NOT have created any
    if [ "$TOTAL_CREATED" -ge 8 ] 2>/dev/null; then
        echo "  Sufficient categories created (correct)"
        FINAL_COUNT_RESULT="PASS"
    else
        echo "  ERROR: Expected at least 8 new categories, got $TOTAL_CREATED"
        FINAL_COUNT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "========================================================================"
echo "  CATEGORY TEST SUMMARY"
echo "========================================================================"
echo ""
echo "  --- Happy Path ---"
echo "  List categories:             $LIST_CATEGORIES_RESULT"
echo "  Create category:             $CREATE_CATEGORY_RESULT"
echo "  Query category details:      $QUERY_CATEGORY_RESULT"
echo "  List with details:           $LIST_DETAILS_RESULT"
echo "  Create members-only:         $CREATE_MEMBERS_RESULT"
echo "  Create admin-only:           $CREATE_ADMIN_RESULT"
echo "  Verify count increased:      $COUNT_VERIFY_RESULT"
echo "  Categories simplified:       $CATEGORIES_SIMPLE_RESULT"
echo "  Open category (f/f):         $OPEN_CATEGORY_RESULT"
echo "  Empty description (valid):   $EMPTY_DESC_CATEGORY_RESULT"
echo "  Both flags true (t/t):       $BOTH_FLAGS_RESULT"
echo "  Duplicate title:             $DUPLICATE_TITLE_RESULT"
echo "  Title at boundary (256):     $TITLE_BOUNDARY_RESULT"
echo "  Desc at boundary (2048):     $DESC_BOUNDARY_RESULT"
echo "  Pagination:                  $PAGINATION_RESULT"
echo "  Member posts members-only:   $VERIFY_MEMBER_POST_RESULT"
echo "  Authority posts admin-only:  $VERIFY_ADMIN_POST_RESULT"
echo "  Final count:                 $FINAL_COUNT_RESULT"
echo ""
echo "  --- Negative Path ---"
echo "  Unauthorized creation:       $NEG_UNAUTHORIZED_RESULT"
echo "  Empty title:                 $NEG_EMPTY_TITLE_RESULT"
echo "  Title too long (>256):       $NEG_LONG_TITLE_RESULT"
echo "  Desc too long (>2048):       $NEG_LONG_DESC_RESULT"
echo "  Query non-existent:          $NEG_NONEXISTENT_RESULT"
echo "  Non-admin posts admin-only:  $NEG_POST_ADMIN_ONLY_RESULT"
echo "  Non-member posts members:    $NEG_POST_MEMBERS_ONLY_RESULT"
echo "  Member posts admin-only:     $NEG_POST_ADMIN_MEMBER_RESULT"
echo ""

# Count failures
FAIL_COUNT=0
TOTAL_COUNT=0

for RESULT in \
    "$LIST_CATEGORIES_RESULT" "$CREATE_CATEGORY_RESULT" "$QUERY_CATEGORY_RESULT" \
    "$LIST_DETAILS_RESULT" "$CREATE_MEMBERS_RESULT" "$CREATE_ADMIN_RESULT" \
    "$COUNT_VERIFY_RESULT" "$CATEGORIES_SIMPLE_RESULT" "$OPEN_CATEGORY_RESULT" \
    "$EMPTY_DESC_CATEGORY_RESULT" "$BOTH_FLAGS_RESULT" "$DUPLICATE_TITLE_RESULT" \
    "$TITLE_BOUNDARY_RESULT" "$DESC_BOUNDARY_RESULT" "$PAGINATION_RESULT" \
    "$VERIFY_MEMBER_POST_RESULT" "$VERIFY_ADMIN_POST_RESULT" "$FINAL_COUNT_RESULT" \
    "$NEG_UNAUTHORIZED_RESULT" "$NEG_EMPTY_TITLE_RESULT" \
    "$NEG_LONG_TITLE_RESULT" "$NEG_LONG_DESC_RESULT" \
    "$NEG_NONEXISTENT_RESULT" "$NEG_POST_ADMIN_ONLY_RESULT" \
    "$NEG_POST_MEMBERS_ONLY_RESULT" "$NEG_POST_ADMIN_MEMBER_RESULT"; do
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
else
    echo "  ALL TESTS PASSED"
fi

echo ""
echo "CATEGORY TEST COMPLETED"
echo ""
