#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/vote Tests"
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

ACCOUNTS=("voter1" "voter2" "voter3" "proposer1" "proposer2")

for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $ACCOUNT"
    else
        echo "  Key exists: $ACCOUNT"
    fi
done

# Get addresses
VOTER1_ADDR=$($BINARY keys show voter1 -a --keyring-backend test)
VOTER2_ADDR=$($BINARY keys show voter2 -a --keyring-backend test)
VOTER3_ADDR=$($BINARY keys show voter3 -a --keyring-backend test)
PROPOSER1_ADDR=$($BINARY keys show proposer1 -a --keyring-backend test)
PROPOSER2_ADDR=$($BINARY keys show proposer2 -a --keyring-backend test)

echo ""

# ========================================================================
# 2. Fund Test Accounts with SPARK (for gas fees)
# ========================================================================
echo "Step 2: Funding test accounts with SPARK for gas fees..."

for ADDR in $VOTER1_ADDR $VOTER2_ADDR $VOTER3_ADDR $PROPOSER1_ADDR $PROPOSER2_ADDR; do
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
# 3. Invite Test Accounts to x/rep (Required for voter registration)
# ========================================================================
echo "Step 3: Inviting test accounts to become x/rep members..."

INVITATION_IDS=()

for i in "${!ACCOUNTS[@]}"; do
    ACCOUNT="${ACCOUNTS[$i]}"

    # Get address based on account name
    case "$ACCOUNT" in
        "voter1") ADDR=$VOTER1_ADDR ;;
        "voter2") ADDR=$VOTER2_ADDR ;;
        "voter3") ADDR=$VOTER3_ADDR ;;
        "proposer1") ADDR=$PROPOSER1_ADDR ;;
        "proposer2") ADDR=$PROPOSER2_ADDR ;;
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
# 5. Transfer DREAM to Test Accounts (for deposits)
# ========================================================================
echo "Step 5: Transferring DREAM to test accounts..."

for ACCOUNT in "${ACCOUNTS[@]}"; do
    # Get address based on account name
    case "$ACCOUNT" in
        "voter1") ADDR=$VOTER1_ADDR ;;
        "voter2") ADDR=$VOTER2_ADDR ;;
        "voter3") ADDR=$VOTER3_ADDR ;;
        "proposer1") ADDR=$PROPOSER1_ADDR ;;
        "proposer2") ADDR=$PROPOSER2_ADDR ;;
        *) continue ;;
    esac

    # Proposers need more DREAM for deposits
    if [ "$ACCOUNT" == "proposer1" ] || [ "$ACCOUNT" == "proposer2" ]; then
        DREAM_AMOUNT="500000000"  # 500 DREAM
        echo "  Sending 500 DREAM to $ACCOUNT (extra for proposal deposits)..."
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
# 6. Register Voters (voter1, voter2, voter3 + alice)
# ========================================================================
echo "Step 6: Registering voters for anonymous voting..."

# Generate deterministic test ZK public keys (32-byte hex values).
# In production these would be hash(secretKey); for e2e we use fixed values.
# Each voter gets a unique key to avoid ErrDuplicatePublicKey.
VOTER1_ZK_KEY="0101010101010101010101010101010101010101010101010101010101010101"
VOTER2_ZK_KEY="0202020202020202020202020202020202020202020202020202020202020202"
VOTER3_ZK_KEY="0303030303030303030303030303030303030303030303030303030303030303"
ALICE_ZK_KEY="0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a"

# Encryption public keys (Babyjubjub points, 32 bytes).
VOTER1_ENC_KEY="1111111111111111111111111111111111111111111111111111111111111111"
VOTER2_ENC_KEY="2222222222222222222222222222222222222222222222222222222222222222"
VOTER3_ENC_KEY="3333333333333333333333333333333333333333333333333333333333333333"
ALICE_ENC_KEY="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

register_voter() {
    local ACCOUNT=$1
    local ZK_KEY_HEX=$2
    local ENC_KEY_HEX=$3

    # Convert hex to base64 for protobuf bytes fields
    local ZK_KEY_B64=$(echo "$ZK_KEY_HEX" | xxd -r -p | base64)
    local ENC_KEY_B64=$(echo "$ENC_KEY_HEX" | xxd -r -p | base64)

    echo "  Registering $ACCOUNT as voter..."

    TX_RES=$($BINARY tx vote register-voter \
        --zk-public-key "$ZK_KEY_B64" \
        --encryption-public-key "$ENC_KEY_B64" \
        --from $ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to register $ACCOUNT: no txhash"
        echo "  Response: $TX_RES"
        return 1
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Registered $ACCOUNT as voter"
        return 0
    else
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')
        if echo "$RAW_LOG" | grep -qi "already.*regist"; then
            echo "  $ACCOUNT is already registered as voter"
            return 0
        fi
        echo "  Failed to register $ACCOUNT: $RAW_LOG"
        return 1
    fi
}

register_voter "voter1" "$VOTER1_ZK_KEY" "$VOTER1_ENC_KEY"
register_voter "voter2" "$VOTER2_ZK_KEY" "$VOTER2_ENC_KEY"
register_voter "voter3" "$VOTER3_ZK_KEY" "$VOTER3_ENC_KEY"
register_voter "alice" "$ALICE_ZK_KEY" "$ALICE_ENC_KEY"

echo ""

# ========================================================================
# 7. Verify All Members and Voter Registrations
# ========================================================================
echo "Step 7: Verifying all test accounts are members..."

ALL_SUCCESS=true

for ACCOUNT in "${ACCOUNTS[@]}"; do
    # Get address based on account name
    case "$ACCOUNT" in
        "voter1") ADDR=$VOTER1_ADDR ;;
        "voter2") ADDR=$VOTER2_ADDR ;;
        "voter3") ADDR=$VOTER3_ADDR ;;
        "proposer1") ADDR=$PROPOSER1_ADDR ;;
        "proposer2") ADDR=$PROPOSER2_ADDR ;;
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

echo "Verifying voter registrations..."

for ACCOUNT in "voter1" "voter2" "voter3" "alice"; do
    case "$ACCOUNT" in
        "voter1") ADDR=$VOTER1_ADDR ;;
        "voter2") ADDR=$VOTER2_ADDR ;;
        "voter3") ADDR=$VOTER3_ADDR ;;
        "alice") ADDR=$ALICE_ADDR ;;
    esac

    REG_INFO=$($BINARY query vote get-voter-registration $ADDR --output json 2>&1)

    if echo "$REG_INFO" | grep -qi "not found\|error"; then
        echo "  $ACCOUNT: NOT registered as voter"
        ALL_SUCCESS=false
    else
        ACTIVE=$(echo "$REG_INFO" | jq -r '.voter_registration.active // "unknown"')
        echo "  $ACCOUNT: registered (active=$ACTIVE)"
    fi
done

echo ""

# ========================================================================
# 8. Check Vote Module Params
# ========================================================================
echo "Step 8: Checking vote module params..."

VOTE_PARAMS=$($BINARY query vote params --output json 2>&1)
if echo "$VOTE_PARAMS" | grep -q "error"; then
    echo "  Warning: Could not query vote params"
    echo "  $VOTE_PARAMS"
else
    OPEN_REG=$(echo "$VOTE_PARAMS" | jq -r '.params.open_registration // "unknown"')
    TLE_ENABLED=$(echo "$VOTE_PARAMS" | jq -r '.params.tle_enabled // "unknown"')
    echo "  Open Registration: $OPEN_REG"
    echo "  TLE Enabled: $TLE_ENABLED"
fi

echo ""

# ========================================================================
# Export Environment Variables
# ========================================================================
cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables for x/vote tests
export VOTER1_ADDR=$VOTER1_ADDR
export VOTER2_ADDR=$VOTER2_ADDR
export VOTER3_ADDR=$VOTER3_ADDR
export PROPOSER1_ADDR=$PROPOSER1_ADDR
export PROPOSER2_ADDR=$PROPOSER2_ADDR
export ALICE_ADDR=$ALICE_ADDR

# ZK public keys (hex)
export VOTER1_ZK_KEY=$VOTER1_ZK_KEY
export VOTER2_ZK_KEY=$VOTER2_ZK_KEY
export VOTER3_ZK_KEY=$VOTER3_ZK_KEY
export ALICE_ZK_KEY=$ALICE_ZK_KEY

# Encryption public keys (hex)
export VOTER1_ENC_KEY=$VOTER1_ENC_KEY
export VOTER2_ENC_KEY=$VOTER2_ENC_KEY
export VOTER3_ENC_KEY=$VOTER3_ENC_KEY
export ALICE_ENC_KEY=$ALICE_ENC_KEY
EOF

echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "Test environment ready:"
echo "  5 test accounts created and funded"
echo "  All accounts are x/rep members with DREAM"
echo "  4 accounts registered as voters (voter1, voter2, voter3, alice)"
echo ""
echo "Environment variables saved to: $SCRIPT_DIR/.test_env"
echo "Source this file in your tests: source .test_env"
echo ""

if [ "$ALL_SUCCESS" = false ]; then
    echo "Some accounts may not be properly initialized"
    echo "Review the output above for errors"
    exit 1
fi
