#!/bin/bash

echo "--- TESTING: GUILDS (CREATE, JOIN, LEAVE, OFFICERS, INVITES) ---"

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

# Use accounts that have member profiles in genesis (Alice, Bob, Carol)
GUILD_FOUNDER_ADDR=$ALICE_ADDR
GUILD_FOUNDER_KEY="alice"
GUILD_OFFICER_ADDR=$BOB_ADDR
GUILD_OFFICER_KEY="bob"
GUILD_MEMBER1_ADDR=$CAROL_ADDR
GUILD_MEMBER1_KEY="carol"

PASS_COUNT=0
FAIL_COUNT=0
TOTAL_COUNT=0

pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    echo "  PASS: $1"
}

fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    echo "  FAIL: $1"
}

echo "Guild Founder (Alice):  $GUILD_FOUNDER_ADDR"
echo "Guild Officer (Bob):    $GUILD_OFFICER_ADDR"
echo "Guild Member 1 (Carol): $GUILD_MEMBER1_ADDR"
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
# PART 1: LIST EXISTING GUILDS
# ========================================================================
echo "--- PART 1: LIST EXISTING GUILDS ---"

GUILDS=$($BINARY query season guilds-list --output json 2>&1)

if echo "$GUILDS" | grep -q "error"; then
    echo "  Failed to query guilds"
else
    # Response has id, name, founder at root level (singular result with pagination)
    GUILD_ID=$(echo "$GUILDS" | jq -r '.id // "0"')
    if [ "$GUILD_ID" != "0" ] && [ "$GUILD_ID" != "null" ]; then
        GUILD_NAME=$(echo "$GUILDS" | jq -r '.name // "unknown"')
        GUILD_FOUNDER=$(echo "$GUILDS" | jq -r '.founder // "unknown"')
        echo "  Found guild: ID=$GUILD_ID, Name=$GUILD_NAME, Founder=${GUILD_FOUNDER:0:20}..."
    else
        echo "  No guilds exist yet"
    fi
fi

echo ""

# ========================================================================
# PART 2: CREATE A GUILD
# ========================================================================
echo "--- PART 2: CREATE A GUILD ---"

GUILD_NAME="TestGuild_$(date +%s)"
GUILD_DESC="A guild for testing x/season functionality"

echo "Creating guild: $GUILD_NAME"
echo "Description: $GUILD_DESC"
echo "Invite-only: false"

TX_RES=$($BINARY tx season create-guild \
    "$GUILD_NAME" \
    "$GUILD_DESC" \
    "false" \
    --from $GUILD_FOUNDER_KEY \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit transaction"
    echo "  $TX_RES"
    GUILD_ID=""
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        # Try to extract guild ID from events
        GUILD_ID=$(extract_event_value "$TX_RESULT" "guild_created" "guild_id")

        if [ -z "$GUILD_ID" ] || [ "$GUILD_ID" == "null" ]; then
            # Fallback: query the latest guild
            GUILDS=$($BINARY query season guilds-list --output json 2>&1)
            GUILD_ID=$(echo "$GUILDS" | jq -r '.guilds[-1].id // empty')
        fi

        echo "  Guild created successfully"
        echo "  Guild ID: $GUILD_ID"

        if [ -n "$GUILD_ID" ] && [ "$GUILD_ID" != "null" ]; then
            pass "create-guild returned non-empty guild ID"
        else
            fail "create-guild returned empty or null guild ID"
        fi
    else
        echo "  Failed to create guild (may need DREAM balance)"
        fail "create-guild transaction failed"
        GUILD_ID=""
    fi
fi

echo ""

# Export GUILD_ID for use in other tests
if [ -n "$GUILD_ID" ]; then
    echo "export TEST_GUILD_ID=$GUILD_ID" >> "$SCRIPT_DIR/.test_env"
fi

# ========================================================================
# PART 3: QUERY GUILD DETAILS
# ========================================================================
echo "--- PART 3: QUERY GUILD DETAILS ---"

if [ -n "$GUILD_ID" ]; then
    GUILD_INFO=$($BINARY query season guild-by-id $GUILD_ID --output json 2>&1)

    if echo "$GUILD_INFO" | grep -q "error"; then
        echo "  Failed to query guild $GUILD_ID"
        fail "query guild-by-id returned error"
    else
        QUERIED_NAME=$(echo "$GUILD_INFO" | jq -r '.name')
        echo "  Guild Details:"
        echo "    ID: $GUILD_ID"
        echo "    Name: $QUERIED_NAME"
        echo "    Founder: $(echo "$GUILD_INFO" | jq -r '.founder')"
        echo "    Status: $(echo "$GUILD_INFO" | jq -r '.status')"
        echo "    Invite Only: $(echo "$GUILD_INFO" | jq -r '.invite_only // false')"

        if [ "$QUERIED_NAME" = "$GUILD_NAME" ]; then
            pass "guild name matches what was created ($GUILD_NAME)"
        else
            fail "guild name mismatch: expected=$GUILD_NAME got=$QUERIED_NAME"
        fi
    fi
else
    echo "  No guild ID available, skipping query"
fi

echo ""

# ========================================================================
# PART 4: JOIN GUILD (Public Guild)
# ========================================================================
echo "--- PART 4: JOIN GUILD (Public Guild) ---"

if [ -n "$GUILD_ID" ]; then
    echo "Carol joining guild $GUILD_ID..."

    TX_RES=$($BINARY tx season join-guild \
        $GUILD_ID \
        --from $GUILD_MEMBER1_KEY \
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
            echo "  Carol joined the guild"

            # Verify membership
            MEMBERSHIP=$($BINARY query season get-guild-membership $GUILD_MEMBER1_ADDR --output json 2>&1)
            MEMBER_GUILD=$(echo "$MEMBERSHIP" | jq -r '.guild_membership.guild_id // "none"')
            echo "  Verified guild membership: $MEMBER_GUILD"

            if [ "$MEMBER_GUILD" = "$GUILD_ID" ]; then
                pass "join-guild: Carol membership shows guild ID $GUILD_ID"
            else
                fail "join-guild: expected guild_id=$GUILD_ID got=$MEMBER_GUILD"
            fi
        else
            echo "  Failed to join guild"
            fail "join-guild transaction failed"
        fi
    fi
else
    echo "  No guild ID available, skipping join test"
fi

echo ""

# ========================================================================
# PART 5: SET GUILD TO INVITE-ONLY
# ========================================================================
echo "--- PART 5: SET GUILD TO INVITE-ONLY ---"

if [ -n "$GUILD_ID" ]; then
    echo "Setting guild $GUILD_ID to invite-only..."

    TX_RES=$($BINARY tx season set-guild-invite-only \
        $GUILD_ID \
        "true" \
        --from $GUILD_FOUNDER_KEY \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Guild set to invite-only"

            # Verify change
            GUILD_INFO=$($BINARY query season guild-by-id $GUILD_ID --output json 2>&1)
            INVITE_ONLY=$(echo "$GUILD_INFO" | jq -r '.invite_only')
            echo "  Verified invite_only: $INVITE_ONLY"

            if [ "$INVITE_ONLY" = "true" ]; then
                pass "set-invite-only: invite_only is true"
            else
                fail "set-invite-only: expected invite_only=true got=$INVITE_ONLY"
            fi
        else
            echo "  Failed to set invite-only"
            fail "set-invite-only transaction failed"
        fi
    fi
else
    echo "  No guild ID available, skipping"
fi

echo ""

# ========================================================================
# PART 6: INVITE TO GUILD
# ========================================================================
echo "--- PART 6: INVITE TO GUILD ---"

if [ -n "$GUILD_ID" ]; then
    echo "Inviting Bob to guild $GUILD_ID..."

    TX_RES=$($BINARY tx season invite-to-guild \
        $GUILD_ID \
        $GUILD_OFFICER_ADDR \
        --from $GUILD_FOUNDER_KEY \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Invitation sent to Bob"
            pass "invite-to-guild transaction succeeded"

            # Check pending invites
            INVITES=$($BINARY query season member-guild-invites $GUILD_OFFICER_ADDR --output json 2>&1)
            INVITE_GUILD=$(echo "$INVITES" | jq -r '.guild_id // "none"')
            echo "  Pending invite for Bob from guild: $INVITE_GUILD"
        else
            echo "  Failed to send invitation"
            fail "invite-to-guild transaction failed"
        fi
    fi
else
    echo "  No guild ID available, skipping"
fi

echo ""

# ========================================================================
# PART 7: ACCEPT GUILD INVITE
# ========================================================================
echo "--- PART 7: ACCEPT GUILD INVITE ---"

if [ -n "$GUILD_ID" ]; then
    echo "Bob accepting invite to guild $GUILD_ID..."

    TX_RES=$($BINARY tx season accept-guild-invite \
        $GUILD_ID \
        --from $GUILD_OFFICER_KEY \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Bob joined the guild"
            pass "accept-guild-invite transaction succeeded"
        else
            echo "  Failed to accept invite"
            fail "accept-guild-invite transaction failed"
        fi
    fi
else
    echo "  No guild ID available, skipping"
fi

echo ""

# ========================================================================
# PART 8: PROMOTE TO OFFICER
# ========================================================================
echo "--- PART 8: PROMOTE TO OFFICER ---"

if [ -n "$GUILD_ID" ]; then
    echo "Promoting Bob to officer role..."

    TX_RES=$($BINARY tx season promote-to-officer \
        $GUILD_ID \
        $GUILD_OFFICER_ADDR \
        --from $GUILD_FOUNDER_KEY \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Bob promoted to officer"
            pass "promote-to-officer transaction succeeded"
        else
            echo "  Failed to promote"
            fail "promote-to-officer transaction failed"
        fi
    fi
else
    echo "  No guild ID available, skipping"
fi

echo ""

# ========================================================================
# PART 9: QUERY GUILD MEMBERS
# ========================================================================
echo "--- PART 9: QUERY GUILD MEMBERS ---"

if [ -n "$GUILD_ID" ]; then
    MEMBERS=$($BINARY query season guild-members $GUILD_ID --output json 2>&1)

    if echo "$MEMBERS" | grep -q "error"; then
        echo "  Failed to query guild members"
    else
        # Response has 'member' (singular) field
        MEMBER=$(echo "$MEMBERS" | jq -r '.member // "none"')
        if [ "$MEMBER" != "none" ] && [ "$MEMBER" != "null" ] && [ -n "$MEMBER" ]; then
            echo "  Guild member found: ${MEMBER:0:20}..."
        else
            echo "  No members found in response"
        fi
    fi
else
    echo "  No guild ID available, skipping"
fi

echo ""

# ========================================================================
# PART 10: UPDATE GUILD DESCRIPTION
# ========================================================================
echo "--- PART 10: UPDATE GUILD DESCRIPTION ---"

if [ -n "$GUILD_ID" ]; then
    NEW_DESC="Updated description for testing - $(date +%s)"
    echo "Updating guild description..."

    TX_RES=$($BINARY tx season update-guild-description \
        $GUILD_ID \
        "$NEW_DESC" \
        --from $GUILD_FOUNDER_KEY \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Description updated"
            pass "update-guild-description transaction succeeded"
        else
            echo "  Failed to update description"
            fail "update-guild-description transaction failed"
        fi
    fi
else
    echo "  No guild ID available, skipping"
fi

echo ""

# ========================================================================
# PART 11: DEMOTE OFFICER
# ========================================================================
echo "--- PART 11: DEMOTE OFFICER ---"

if [ -n "$GUILD_ID" ]; then
    echo "Demoting Bob from officer role..."

    TX_RES=$($BINARY tx season demote-officer \
        $GUILD_ID \
        $GUILD_OFFICER_ADDR \
        --from $GUILD_FOUNDER_KEY \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Bob demoted"
            pass "demote-officer transaction succeeded"
        else
            echo "  Failed to demote"
            fail "demote-officer transaction failed"
        fi
    fi
else
    echo "  No guild ID available, skipping"
fi

echo ""

# ========================================================================
# PART 12: LEAVE GUILD
# ========================================================================
echo "--- PART 12: LEAVE GUILD ---"

if [ -n "$GUILD_ID" ]; then
    echo "Carol leaving guild..."

    TX_RES=$($BINARY tx season leave-guild \
        --from $GUILD_MEMBER1_KEY \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Carol left the guild"
            pass "leave-guild transaction succeeded"
        else
            echo "  Failed to leave guild"
            fail "leave-guild transaction failed"
        fi
    fi
else
    echo "  No guild ID available, skipping"
fi

echo ""

# ========================================================================
# PART 13: QUERY GUILDS BY FOUNDER
# ========================================================================
echo "--- PART 13: QUERY GUILDS BY FOUNDER ---"

FOUNDER_GUILDS=$($BINARY query season guilds-by-founder $GUILD_FOUNDER_ADDR false --output json 2>&1)

if echo "$FOUNDER_GUILDS" | grep -q "error"; then
    echo "  Failed to query guilds by founder"
else
    # Response has id, name, status at root level (singular result)
    GUILD_ID_FOUND=$(echo "$FOUNDER_GUILDS" | jq -r '.id // "none"')
    GUILD_NAME_FOUND=$(echo "$FOUNDER_GUILDS" | jq -r '.name // "none"')
    if [ "$GUILD_ID_FOUND" != "none" ] && [ "$GUILD_ID_FOUND" != "0" ]; then
        echo "  Found guild: ID=$GUILD_ID_FOUND, Name=$GUILD_NAME_FOUND"
        pass "guilds-by-founder returned guild for Alice"
    else
        echo "  No guilds found for Alice"
        fail "guilds-by-founder returned no guild for Alice"
    fi
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- GUILD TEST SUMMARY ---"
echo ""
echo "  Total:  $TOTAL_COUNT"
echo "  Passed: $PASS_COUNT"
echo "  Failed: $FAIL_COUNT"
echo ""
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "GUILD TEST FAILED ($FAIL_COUNT failures)"
    exit 1
else
    echo "GUILD TEST PASSED (all $PASS_COUNT assertions passed)"
fi
echo ""
