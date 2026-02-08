#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/name Tests"
echo "=================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

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
# 1. Create Test Account Key
# ========================================================================
echo "Step 1: Creating test account key..."

ACCOUNT="name_claimant"

if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
    echo "  Created key: $ACCOUNT"
else
    echo "  Key exists: $ACCOUNT"
fi

CLAIMANT_ADDR=$($BINARY keys show $ACCOUNT -a --keyring-backend test)
echo "  Address: $CLAIMANT_ADDR"
echo ""

# ========================================================================
# 2. Fund with SPARK (for gas fees)
# ========================================================================
echo "Step 2: Funding $ACCOUNT with SPARK for gas fees..."

TX_RES=$($BINARY tx bank send \
    alice $CLAIMANT_ADDR \
    10000000uspark \
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
        echo "  Sent 10 SPARK to $ACCOUNT"
    else
        echo "  Failed to send SPARK"
    fi
else
    echo "  Failed to send SPARK: no txhash"
fi

echo ""

# ========================================================================
# 3. Invite to x/rep
# ========================================================================
echo "Step 3: Inviting $ACCOUNT to become x/rep member..."

MEMBER_INFO=$($BINARY query rep get-member $CLAIMANT_ADDR --output json 2>&1)
if ! echo "$MEMBER_INFO" | grep -q "not found"; then
    echo "  $ACCOUNT is already a member, skipping invitation"
    INVITATION_ID=""
else
    TX_RES=$($BINARY tx rep invite-member \
        $CLAIMANT_ADDR \
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
        exit 1
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        INVITATION_ID=$(extract_event_value "$TX_RESULT" "create_invitation" "invitation_id")
        if [ -z "$INVITATION_ID" ]; then
            echo "  Could not extract invitation_id, using fallback: 1"
            INVITATION_ID="1"
        fi
        echo "  Invited $ACCOUNT (invitation #$INVITATION_ID)"
    else
        echo "  Failed to invite $ACCOUNT"
        exit 1
    fi
fi

echo ""

# ========================================================================
# 4. Accept Invitation
# ========================================================================
echo "Step 4: Accepting invitation..."

if [ -n "$INVITATION_ID" ]; then
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
        exit 1
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  $ACCOUNT is now a member"
    else
        echo "  Failed to accept invitation"
        exit 1
    fi
else
    echo "  Skipped (already a member)"
fi

echo ""

# ========================================================================
# 5. Transfer DREAM
# ========================================================================
echo "Step 5: Transferring DREAM to $ACCOUNT..."

TX_RES=$($BINARY tx rep transfer-dream \
    $CLAIMANT_ADDR \
    "250000000" \
    "gift" \
    "Name test setup funding" \
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
        echo "  Transferred 250 DREAM to $ACCOUNT"
    else
        echo "  Failed to transfer DREAM"
    fi
else
    echo "  Failed to transfer DREAM: no txhash"
fi

echo ""

# ========================================================================
# 6. Verify
# ========================================================================
echo "Step 6: Verifying $ACCOUNT is an x/rep member with DREAM..."

MEMBER_INFO=$($BINARY query rep get-member $CLAIMANT_ADDR --output json 2>&1)

if echo "$MEMBER_INFO" | grep -q "not found"; then
    echo "  FAIL: $ACCOUNT is NOT a member"
    exit 1
else
    DREAM_BALANCE=$(echo "$MEMBER_INFO" | jq -r '.member.dream_balance')
    echo "  $ACCOUNT: $DREAM_BALANCE micro-DREAM"
fi

echo ""

# ========================================================================
# Export Environment Variables
# ========================================================================
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test 2>/dev/null || echo "")
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test 2>/dev/null || echo "")

cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables for x/name tests
export ALICE_ADDR=$ALICE_ADDR
export BOB_ADDR=$BOB_ADDR
export CAROL_ADDR=$CAROL_ADDR
export NAME_CLAIMANT_ADDR=$CLAIMANT_ADDR
EOF

echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "Test accounts ready:"
echo "  alice, bob, carol  — genesis Council members"
echo "  name_claimant      — x/rep member with DREAM (dispute claimant)"
echo ""
echo "Environment saved to: $SCRIPT_DIR/.test_env"
echo ""
