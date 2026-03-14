#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/shield Tests"
echo "=================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Get alice address (genesis member / validator)
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

ACCOUNTS=("member1" "member2" "member3" "submitter1" "submitter2")

for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $ACCOUNT"
    else
        echo "  Key exists: $ACCOUNT"
    fi
done

# Get addresses
MEMBER1_ADDR=$($BINARY keys show member1 -a --keyring-backend test)
MEMBER2_ADDR=$($BINARY keys show member2 -a --keyring-backend test)
MEMBER3_ADDR=$($BINARY keys show member3 -a --keyring-backend test)
SUBMITTER1_ADDR=$($BINARY keys show submitter1 -a --keyring-backend test)
SUBMITTER2_ADDR=$($BINARY keys show submitter2 -a --keyring-backend test)

echo ""

# ========================================================================
# 2. Fund Test Accounts with SPARK (for gas fees)
# ========================================================================
echo "Step 2: Funding test accounts with SPARK for gas fees..."

for ADDR in $MEMBER1_ADDR $MEMBER2_ADDR $MEMBER3_ADDR $SUBMITTER1_ADDR $SUBMITTER2_ADDR; do
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
# 3. Invite Test Accounts to x/rep (Required for shielded execution)
# ========================================================================
echo "Step 3: Inviting test accounts to become x/rep members..."

INVITATION_IDS=()

# Only invite member accounts — submitter accounts don't need membership
# (shield pays gas, submitters can be anonymous)
MEMBER_ACCOUNTS=("member1" "member2" "member3")

for i in "${!MEMBER_ACCOUNTS[@]}"; do
    ACCOUNT="${MEMBER_ACCOUNTS[$i]}"

    case "$ACCOUNT" in
        "member1") ADDR=$MEMBER1_ADDR ;;
        "member2") ADDR=$MEMBER2_ADDR ;;
        "member3") ADDR=$MEMBER3_ADDR ;;
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

for i in "${!MEMBER_ACCOUNTS[@]}"; do
    ACCOUNT="${MEMBER_ACCOUNTS[$i]}"
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
# 5. Transfer DREAM to Test Accounts
# ========================================================================
echo "Step 5: Transferring DREAM to member accounts..."

for ACCOUNT in "${MEMBER_ACCOUNTS[@]}"; do
    case "$ACCOUNT" in
        "member1") ADDR=$MEMBER1_ADDR ;;
        "member2") ADDR=$MEMBER2_ADDR ;;
        "member3") ADDR=$MEMBER3_ADDR ;;
        *) continue ;;
    esac

    DREAM_AMOUNT="250000000"  # 250 DREAM
    echo "  Sending 250 DREAM to $ACCOUNT..."

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
# 6. Register ZK Public Keys in x/rep (Required for trust tree)
# ========================================================================
echo "Step 6: Registering ZK public keys in x/rep..."

# Deterministic test ZK public keys (32-byte hex values).
# In production these come from key generation; for e2e we use fixed values.
# Each member gets a unique key.
MEMBER1_ZK_KEY="0101010101010101010101010101010101010101010101010101010101010101"
MEMBER2_ZK_KEY="0202020202020202020202020202020202020202020202020202020202020202"
MEMBER3_ZK_KEY="0303030303030303030303030303030303030303030303030303030303030303"
ALICE_ZK_KEY="0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a"

register_zk_key() {
    local ACCOUNT=$1
    local ZK_KEY_HEX=$2

    # Convert hex to base64 for protobuf bytes fields
    local ZK_KEY_B64=$(echo "$ZK_KEY_HEX" | xxd -r -p | base64)

    echo "  Registering ZK public key for $ACCOUNT..."

    TX_RES=$($BINARY tx rep register-zk-public-key \
        "$ZK_KEY_B64" \
        --from $ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to register ZK key for $ACCOUNT: no txhash"
        echo "  Response: $TX_RES"
        return 1
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Registered ZK public key for $ACCOUNT"
        return 0
    else
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')
        if echo "$RAW_LOG" | grep -qi "already.*regist\|key.*exists"; then
            echo "  $ACCOUNT already has a ZK public key registered"
            return 0
        fi
        echo "  Failed to register ZK key for $ACCOUNT: $RAW_LOG"
        return 1
    fi
}

register_zk_key "member1" "$MEMBER1_ZK_KEY"
register_zk_key "member2" "$MEMBER2_ZK_KEY"
register_zk_key "member3" "$MEMBER3_ZK_KEY"
register_zk_key "alice" "$ALICE_ZK_KEY"

echo ""

# ========================================================================
# 7. Verify All Members
# ========================================================================
echo "Step 7: Verifying all test accounts are members..."

ALL_SUCCESS=true

for ACCOUNT in "${MEMBER_ACCOUNTS[@]}"; do
    case "$ACCOUNT" in
        "member1") ADDR=$MEMBER1_ADDR ;;
        "member2") ADDR=$MEMBER2_ADDR ;;
        "member3") ADDR=$MEMBER3_ADDR ;;
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
# 8. Check Shield Module Params
# ========================================================================
echo "Step 8: Checking shield module params..."

SHIELD_PARAMS=$($BINARY query shield params --output json 2>&1)
if echo "$SHIELD_PARAMS" | grep -q "error"; then
    echo "  Warning: Could not query shield params"
    echo "  $SHIELD_PARAMS"
else
    ENABLED=$(echo "$SHIELD_PARAMS" | jq -r '.params.enabled // "unknown"')
    BATCH_ENABLED=$(echo "$SHIELD_PARAMS" | jq -r '.params.encrypted_batch_enabled // false')
    echo "  Enabled: $ENABLED"
    echo "  Encrypted Batch Enabled: $BATCH_ENABLED"
fi

echo ""

# ========================================================================
# 9. Get Shield Module Address
# ========================================================================
echo "Step 9: Getting shield module address..."

# The shield module account address (derived from module name)
SHIELD_MODULE_ADDR=$($BINARY query auth module-account shield --output json 2>&1 | jq -r '.account.base_account.address // .account.value.address // ""')
if [ -z "$SHIELD_MODULE_ADDR" ] || [ "$SHIELD_MODULE_ADDR" == "null" ]; then
    echo "  Warning: Could not get shield module address"
    SHIELD_MODULE_ADDR=""
else
    echo "  Shield module address: $SHIELD_MODULE_ADDR"
fi

echo ""

# ========================================================================
# Export Environment Variables
# ========================================================================
cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables for x/shield tests
export MEMBER1_ADDR=$MEMBER1_ADDR
export MEMBER2_ADDR=$MEMBER2_ADDR
export MEMBER3_ADDR=$MEMBER3_ADDR
export SUBMITTER1_ADDR=$SUBMITTER1_ADDR
export SUBMITTER2_ADDR=$SUBMITTER2_ADDR
export ALICE_ADDR=$ALICE_ADDR
export SHIELD_MODULE_ADDR=$SHIELD_MODULE_ADDR

# ZK public keys (hex)
export MEMBER1_ZK_KEY=$MEMBER1_ZK_KEY
export MEMBER2_ZK_KEY=$MEMBER2_ZK_KEY
export MEMBER3_ZK_KEY=$MEMBER3_ZK_KEY
export ALICE_ZK_KEY=$ALICE_ZK_KEY
EOF

echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "Test environment ready:"
echo "  3 member accounts created, funded, and registered with ZK keys"
echo "  2 submitter accounts created and funded (for gasless submission tests)"
echo "  Alice registered with ZK key"
echo ""
echo "Environment variables saved to: $SCRIPT_DIR/.test_env"
echo "Source this file in your tests: source .test_env"
echo ""

if [ "$ALL_SUCCESS" = false ]; then
    echo "Some accounts may not be properly initialized"
    echo "Review the output above for errors"
    exit 1
fi
