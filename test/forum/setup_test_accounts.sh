#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/forum Tests"
echo "=================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Get alice address (genesis member)
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)

echo "Genesis member (Alice): $ALICE_ADDR"
echo ""

# Delete stale .test_env so it is regenerated from the current keyring
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Removing stale .test_env (will be regenerated at end of setup)..."
    rm -f "$SCRIPT_DIR/.test_env"
fi

# ========================================================================
# Helper Functions
# ========================================================================

# Wait for transaction and extract result
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

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
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
# 1. Create Test Account Keys (if not exist)
# ========================================================================
echo "Step 1: Creating test account keys..."

ACCOUNTS=("poster1" "poster2" "sentinel1" "sentinel2" "bounty_creator" "moderator")

for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $ACCOUNT"
    else
        echo "  Key exists: $ACCOUNT"
    fi
done

# Get addresses
POSTER1_ADDR=$($BINARY keys show poster1 -a --keyring-backend test)
POSTER2_ADDR=$($BINARY keys show poster2 -a --keyring-backend test)
SENTINEL1_ADDR=$($BINARY keys show sentinel1 -a --keyring-backend test)
SENTINEL2_ADDR=$($BINARY keys show sentinel2 -a --keyring-backend test)
BOUNTY_CREATOR_ADDR=$($BINARY keys show bounty_creator -a --keyring-backend test)
MODERATOR_ADDR=$($BINARY keys show moderator -a --keyring-backend test)

echo ""

# ========================================================================
# 2. Fund Test Accounts with SPARK (for gas fees)
# ========================================================================
echo "Step 2: Funding test accounts with SPARK for gas fees..."

for ADDR in $POSTER1_ADDR $POSTER2_ADDR $SENTINEL1_ADDR $SENTINEL2_ADDR $BOUNTY_CREATOR_ADDR $MODERATOR_ADDR; do
    echo "  Sending 10 SPARK to $ADDR..."
    TX_RES=$($BINARY tx bank send \
        alice $ADDR \
        10000000uspark \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to send SPARK: no txhash"
        continue
    fi

    sleep 6
done

echo "  All accounts funded with SPARK"
echo ""

# ========================================================================
# 3. Invite Test Accounts to x/rep (Required for forum membership)
# ========================================================================
echo "Step 3: Inviting test accounts to become x/rep members..."

INVITATION_IDS=()

for i in "${!ACCOUNTS[@]}"; do
    ACCOUNT="${ACCOUNTS[$i]}"

    # Get address based on account name
    case "$ACCOUNT" in
        "poster1") ADDR=$POSTER1_ADDR ;;
        "poster2") ADDR=$POSTER2_ADDR ;;
        "sentinel1") ADDR=$SENTINEL1_ADDR ;;
        "sentinel2") ADDR=$SENTINEL2_ADDR ;;
        "bounty_creator") ADDR=$BOUNTY_CREATOR_ADDR ;;
        "moderator") ADDR=$MODERATOR_ADDR ;;
        *) echo "Unknown account: $ACCOUNT"; continue ;;
    esac

    # Check if already a member
    MEMBER_INFO=$($BINARY query rep get-member $ADDR --output json 2>&1)
    if ! echo "$MEMBER_INFO" | grep -q "not found"; then
        echo "  $ACCOUNT is already a member, skipping invitation"
        INVITATION_IDS+=("")
        continue
    fi

    echo "  Inviting $ACCOUNT ($ADDR)..."

    # Stake minimum (100 micro-DREAM) on the invitation
    TX_RES=$($BINARY tx rep invite-member \
        $ADDR \
        "100" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to invite $ACCOUNT: no txhash"
        INVITATION_IDS+=("")
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        INVITATION_ID=$(extract_event_value "$TX_RESULT" "create_invitation" "invitation_id")
        if [ -z "$INVITATION_ID" ]; then
            echo "  Could not extract invitation_id, using index: $((i + 1))"
            INVITATION_ID=$((i + 1))
        fi
        INVITATION_IDS+=($INVITATION_ID)
        echo "  Invited $ACCOUNT (invitation #$INVITATION_ID)"
    else
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "invitation already exists"; then
            echo "  $ACCOUNT already has an invitation"
            INVITATION_IDS+=("")
        else
            echo "  Failed to invite $ACCOUNT: $RAW_LOG"
            INVITATION_IDS+=("")
        fi
    fi
done

echo ""

# ========================================================================
# 4. Accept Invitations
# ========================================================================
echo "Step 4: Accepting invitations..."

for i in "${!ACCOUNTS[@]}"; do
    ACCOUNT="${ACCOUNTS[$i]}"
    INVITATION_ID="${INVITATION_IDS[$i]}"

    if [ -z "$INVITATION_ID" ]; then
        echo "  Skipping $ACCOUNT (no invitation ID or already member)"
        continue
    fi

    echo "  $ACCOUNT accepting invitation #$INVITATION_ID..."

    TX_RES=$($BINARY tx rep accept-invitation \
        $INVITATION_ID \
        --from $ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to accept invitation: no txhash"
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  $ACCOUNT is now a member"
    else
        echo "  Failed: $ACCOUNT could not accept invitation"
    fi
done

echo ""

# ========================================================================
# 5. Transfer DREAM to Test Accounts (needed for sentinel bonding, bounties)
# ========================================================================
echo "Step 5: Transferring DREAM to test accounts..."

for ACCOUNT in "${ACCOUNTS[@]}"; do
    # Get address based on account name
    case "$ACCOUNT" in
        "poster1") ADDR=$POSTER1_ADDR ;;
        "poster2") ADDR=$POSTER2_ADDR ;;
        "sentinel1") ADDR=$SENTINEL1_ADDR ;;
        "sentinel2") ADDR=$SENTINEL2_ADDR ;;
        "bounty_creator") ADDR=$BOUNTY_CREATOR_ADDR ;;
        "moderator") ADDR=$MODERATOR_ADDR ;;
        *) continue ;;
    esac

    # Sentinel bonding requires 100 DREAM (100000000 micro-DREAM).
    # Gift enough to cover the bond plus the 3% transfer tax.
    # Alice (Tier 1 founder) has 50000 DREAM, so these amounts are fine.
    if [ "$ACCOUNT" == "sentinel1" ]; then
        DREAM_AMOUNT="200000000"  # 200 DREAM (covers 100 DREAM bond + tax + extra for sentinel2 unbond test)
        echo "  Sending 200 DREAM to $ACCOUNT (for sentinel bonding)..."
    elif [ "$ACCOUNT" == "sentinel2" ]; then
        DREAM_AMOUNT="150000000"  # 150 DREAM (covers 100 DREAM bond + tax)
        echo "  Sending 150 DREAM to $ACCOUNT (for sentinel bonding)..."
    elif [ "$ACCOUNT" == "bounty_creator" ]; then
        DREAM_AMOUNT="200000"  # 0.2 DREAM
        echo "  Sending 0.2 DREAM to $ACCOUNT (for bounties)..."
    else
        DREAM_AMOUNT="100000"  # 0.1 DREAM
        echo "  Sending 0.1 DREAM to $ACCOUNT..."
    fi

    # Gift DREAM to the new member
    TX_RES=$($BINARY tx rep transfer-dream \
        $ADDR \
        "$DREAM_AMOUNT" \
        "gift" \
        "Test setup funding" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to send DREAM to $ACCOUNT: no txhash"
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Transferred DREAM to $ACCOUNT"
    else
        echo "  Failed to transfer DREAM to $ACCOUNT"
        echo "     $(echo "$TX_RESULT" | jq -r '.raw_log')"
    fi
done

echo ""

# ========================================================================
# 6. Verify All Members
# ========================================================================
echo "Step 6: Verifying all test accounts are members..."

ALL_SUCCESS=true

for ACCOUNT in "${ACCOUNTS[@]}"; do
    # Get address based on account name
    case "$ACCOUNT" in
        "poster1") ADDR=$POSTER1_ADDR ;;
        "poster2") ADDR=$POSTER2_ADDR ;;
        "sentinel1") ADDR=$SENTINEL1_ADDR ;;
        "sentinel2") ADDR=$SENTINEL2_ADDR ;;
        "bounty_creator") ADDR=$BOUNTY_CREATOR_ADDR ;;
        "moderator") ADDR=$MODERATOR_ADDR ;;
        *) continue ;;
    esac

    MEMBER_INFO=$($BINARY query rep get-member $ADDR --output json 2>&1)

    if echo "$MEMBER_INFO" | grep -q "not found"; then
        echo "  $ACCOUNT is NOT a member"
        ALL_SUCCESS=false
    else
        DREAM_BALANCE=$(echo "$MEMBER_INFO" | jq -r '.member.dream_balance')
        echo "  $ACCOUNT: $DREAM_BALANCE micro-DREAM"
    fi
done

echo ""

# ========================================================================
# 7. Check Forum Status
# ========================================================================
echo "Step 7: Checking forum status..."

FORUM_STATUS=$($BINARY query forum forum-status --output json 2>&1)
if echo "$FORUM_STATUS" | grep -q "error"; then
    echo "  Warning: Could not query forum status"
    echo "  $FORUM_STATUS"
else
    FORUM_PAUSED=$(echo "$FORUM_STATUS" | jq -r '.forum_paused // "false"')
    MOD_PAUSED=$(echo "$FORUM_STATUS" | jq -r '.moderation_paused // "false"')
    echo "  Forum Paused: $FORUM_PAUSED"
    echo "  Moderation Paused: $MOD_PAUSED"
fi

echo ""

# ========================================================================
# 8. Create Initial Category (if needed)
# Note: After IsCouncilAuthorized integration, alice can create categories
# as a Commons Operations Committee member, not just as governance authority.
# ========================================================================
echo "Step 8: Creating initial category for tests..."

CATEGORIES=$($BINARY query forum list-category --output json 2>&1)
CATEGORY_COUNT=$(echo "$CATEGORIES" | jq -r '.category | length' 2>/dev/null || echo "0")

if [ "$CATEGORY_COUNT" -gt 0 ]; then
    echo "  Categories already exist ($CATEGORY_COUNT found)"
    FIRST_CATEGORY=$(echo "$CATEGORIES" | jq -r '.category[0].category_id')
    echo "  Using existing category ID: $FIRST_CATEGORY"
else
    echo "  Creating a test category..."

    TX_RES=$($BINARY tx forum create-category \
        "General Discussion" \
        "A category for general forum discussions" \
        "false" \
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
            FIRST_CATEGORY=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
            if [ -z "$FIRST_CATEGORY" ]; then
                FIRST_CATEGORY="1"
            fi
            echo "  Category created: $FIRST_CATEGORY"
        else
            echo "  Failed to create category (may require authority)"
            FIRST_CATEGORY="1"
        fi
    else
        echo "  Failed to submit category creation"
        FIRST_CATEGORY="1"
    fi
fi

echo ""

# ========================================================================
# Export Environment Variables
# ========================================================================
cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables for x/forum tests
export POSTER1_ADDR=$POSTER1_ADDR
export POSTER2_ADDR=$POSTER2_ADDR
export SENTINEL1_ADDR=$SENTINEL1_ADDR
export SENTINEL2_ADDR=$SENTINEL2_ADDR
export BOUNTY_CREATOR_ADDR=$BOUNTY_CREATOR_ADDR
export MODERATOR_ADDR=$MODERATOR_ADDR
export ALICE_ADDR=$ALICE_ADDR
export TEST_CATEGORY_ID=$FIRST_CATEGORY
EOF

echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "Test environment ready:"
echo "  6 test accounts created and funded"
echo "  All accounts are x/rep members with DREAM"
echo "  Initial category ID: $FIRST_CATEGORY"
echo ""
echo "Environment variables saved to: $SCRIPT_DIR/.test_env"
echo "Source this file in your tests: source .test_env"
echo ""

if [ "$ALL_SUCCESS" = false ]; then
    echo "Some accounts may not be properly initialized"
    echo "Review the output above for errors"
    exit 1
fi
