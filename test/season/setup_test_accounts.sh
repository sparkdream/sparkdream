#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/season Tests"
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

ACCOUNTS=("guild_founder" "guild_officer" "guild_member1" "guild_member2" "quest_user" "display_user")

for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $ACCOUNT"
    else
        echo "  Key exists: $ACCOUNT"
    fi
done

# Get addresses
GUILD_FOUNDER_ADDR=$($BINARY keys show guild_founder -a --keyring-backend test)
GUILD_OFFICER_ADDR=$($BINARY keys show guild_officer -a --keyring-backend test)
GUILD_MEMBER1_ADDR=$($BINARY keys show guild_member1 -a --keyring-backend test)
GUILD_MEMBER2_ADDR=$($BINARY keys show guild_member2 -a --keyring-backend test)
QUEST_USER_ADDR=$($BINARY keys show quest_user -a --keyring-backend test)
DISPLAY_USER_ADDR=$($BINARY keys show display_user -a --keyring-backend test)

echo ""

# ========================================================================
# 2. Fund Test Accounts with SPARK (for gas fees)
# ========================================================================
echo "Step 2: Funding test accounts with SPARK for gas fees..."

for ADDR in $GUILD_FOUNDER_ADDR $GUILD_OFFICER_ADDR $GUILD_MEMBER1_ADDR $GUILD_MEMBER2_ADDR $QUEST_USER_ADDR $DISPLAY_USER_ADDR; do
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
# 3. Invite Test Accounts to x/rep (Required for x/season membership)
# ========================================================================
echo "Step 3: Inviting test accounts to become x/rep members..."

INVITATION_IDS=()

for i in "${!ACCOUNTS[@]}"; do
    ACCOUNT="${ACCOUNTS[$i]}"

    # Get address based on account name
    case "$ACCOUNT" in
        "guild_founder") ADDR=$GUILD_FOUNDER_ADDR ;;
        "guild_officer") ADDR=$GUILD_OFFICER_ADDR ;;
        "guild_member1") ADDR=$GUILD_MEMBER1_ADDR ;;
        "guild_member2") ADDR=$GUILD_MEMBER2_ADDR ;;
        "quest_user") ADDR=$QUEST_USER_ADDR ;;
        "display_user") ADDR=$DISPLAY_USER_ADDR ;;
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

    # Stake 100 DREAM (100000000 micro-DREAM) on the invitation
    TX_RES=$($BINARY tx rep invite-member \
        $ADDR \
        "100000000" \
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
# 5. Transfer DREAM to Test Accounts (needed for guild creation costs)
# ========================================================================
echo "Step 5: Transferring DREAM to test accounts..."

for ACCOUNT in "${ACCOUNTS[@]}"; do
    # Get address based on account name
    case "$ACCOUNT" in
        "guild_founder") ADDR=$GUILD_FOUNDER_ADDR ;;
        "guild_officer") ADDR=$GUILD_OFFICER_ADDR ;;
        "guild_member1") ADDR=$GUILD_MEMBER1_ADDR ;;
        "guild_member2") ADDR=$GUILD_MEMBER2_ADDR ;;
        "quest_user") ADDR=$QUEST_USER_ADDR ;;
        "display_user") ADDR=$DISPLAY_USER_ADDR ;;
        *) continue ;;
    esac

    # Guild founder needs more DREAM for guild creation
    if [ "$ACCOUNT" == "guild_founder" ]; then
        DREAM_AMOUNT="500000000"  # 500 DREAM
        echo "  Sending 500 DREAM to $ACCOUNT (extra for guild creation)..."
    else
        DREAM_AMOUNT="250000000"  # 250 DREAM
        echo "  Sending 250 DREAM to $ACCOUNT..."
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
        "guild_founder") ADDR=$GUILD_FOUNDER_ADDR ;;
        "guild_officer") ADDR=$GUILD_OFFICER_ADDR ;;
        "guild_member1") ADDR=$GUILD_MEMBER1_ADDR ;;
        "guild_member2") ADDR=$GUILD_MEMBER2_ADDR ;;
        "quest_user") ADDR=$QUEST_USER_ADDR ;;
        "display_user") ADDR=$DISPLAY_USER_ADDR ;;
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
# 7. Verify Season State
# ========================================================================
echo "Step 7: Checking current season state..."

SEASON_INFO=$($BINARY query season current-season --output json 2>&1)
if echo "$SEASON_INFO" | grep -q "error"; then
    echo "  Warning: Could not query current season"
    echo "  $SEASON_INFO"
else
    SEASON_NUM=$(echo "$SEASON_INFO" | jq -r '.season.number // "unknown"')
    SEASON_STATUS=$(echo "$SEASON_INFO" | jq -r '.season.status // "unknown"')
    echo "  Current Season: #$SEASON_NUM"
    echo "  Status: $SEASON_STATUS"
fi

echo ""

# ========================================================================
# Step 8: Verify Genesis Profiles (Alice, Bob, Carol)
# ========================================================================
echo "Step 8: Verifying genesis member profiles..."

for MEMBER in "alice" "bob" "carol"; do
    ADDR=$($BINARY keys show $MEMBER -a --keyring-backend test 2>/dev/null)
    if [ -n "$ADDR" ]; then
        PROFILE=$($BINARY query season get-member-profile $ADDR --output json 2>&1)
        if echo "$PROFILE" | grep -q "not found"; then
            echo "  $MEMBER: No profile found"
        else
            DISPLAY_NAME=$(echo "$PROFILE" | jq -r '.member_profile.display_name // "none"')
            SEASON_XP=$(echo "$PROFILE" | jq -r '.member_profile.season_xp // "0"')
            SEASON_LEVEL=$(echo "$PROFILE" | jq -r '.member_profile.season_level // "0"')
            ACH_COUNT=$(echo "$PROFILE" | jq -r '.member_profile.achievements | length' 2>/dev/null || echo "0")
            TITLE_COUNT=$(echo "$PROFILE" | jq -r '.member_profile.unlocked_titles | length' 2>/dev/null || echo "0")
            echo "  $MEMBER ($DISPLAY_NAME): Level $SEASON_LEVEL, $SEASON_XP XP, $ACH_COUNT achievements, $TITLE_COUNT titles"
        fi
    fi
done

echo ""

# ========================================================================
# Export Environment Variables
# ========================================================================
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test 2>/dev/null || echo "")
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test 2>/dev/null || echo "")

cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables for x/season tests
export GUILD_FOUNDER_ADDR=$GUILD_FOUNDER_ADDR
export GUILD_OFFICER_ADDR=$GUILD_OFFICER_ADDR
export GUILD_MEMBER1_ADDR=$GUILD_MEMBER1_ADDR
export GUILD_MEMBER2_ADDR=$GUILD_MEMBER2_ADDR
export QUEST_USER_ADDR=$QUEST_USER_ADDR
export DISPLAY_USER_ADDR=$DISPLAY_USER_ADDR
export ALICE_ADDR=$ALICE_ADDR
export BOB_ADDR=$BOB_ADDR
export CAROL_ADDR=$CAROL_ADDR
EOF

echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "Test environment ready:"
echo "  6 test accounts created and funded"
echo "  All accounts are x/rep members with DREAM"
echo "  Genesis members (alice, bob, carol) have pre-configured XP/achievements/titles"
echo ""
echo "Genesis Profile Summary:"
echo "  - Alice: Level 8, 5000 XP, 5 achievements, 3 titles (veteran, rising_star)"
echo "  - Bob:   Level 4, 1500 XP, 2 achievements, 1 title (newcomer)"
echo "  - Carol: Level 2,  300 XP, 1 achievement,  1 title (newcomer)"
echo ""
echo "Environment variables saved to: $SCRIPT_DIR/.test_env"
echo "Source this file in your tests: source .test_env"
echo ""

if [ "$ALL_SUCCESS" = false ]; then
    echo "Some accounts may not be properly initialized"
    echo "Review the output above for errors"
    exit 1
fi
