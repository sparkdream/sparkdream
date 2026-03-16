#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/session Tests"
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

# session_granter: main account that creates/owns sessions (needs rep membership for blog tests)
# session_grantee1, session_grantee2: session key accounts
ACCOUNTS=("session_granter" "session_grantee1" "session_grantee2")

for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $ACCOUNT"
    else
        echo "  Key exists: $ACCOUNT"
    fi
done

# Get addresses
GRANTER_ADDR=$($BINARY keys show session_granter -a --keyring-backend test)
GRANTEE1_ADDR=$($BINARY keys show session_grantee1 -a --keyring-backend test)
GRANTEE2_ADDR=$($BINARY keys show session_grantee2 -a --keyring-backend test)

echo "  session_granter: $GRANTER_ADDR"
echo "  session_grantee1: $GRANTEE1_ADDR"
echo "  session_grantee2: $GRANTEE2_ADDR"
echo ""

# ========================================================================
# 2. Fund Test Accounts with SPARK
# ========================================================================
echo "Step 2: Funding test accounts with SPARK..."

# Granter needs more SPARK (pays session fees + gas)
echo "  Sending 100 SPARK to session_granter..."
TX_RES=$($BINARY tx bank send \
    alice $GRANTER_ADDR \
    100000000uspark \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)
sleep 6

# Grantees need just enough SPARK for account existence + signing
for ADDR in $GRANTEE1_ADDR $GRANTEE2_ADDR; do
    echo "  Sending 10 SPARK to $ADDR..."
    TX_RES=$($BINARY tx bank send \
        alice $ADDR \
        10000000uspark \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    sleep 6
done

echo "  All accounts funded"
echo ""

# ========================================================================
# 3. Invite session_granter to x/rep (Required for blog post creation via exec-session)
# ========================================================================
echo "Step 3: Inviting session_granter to become x/rep member..."

MEMBER_INFO=$($BINARY query rep get-member $GRANTER_ADDR --output json 2>&1)
if echo "$MEMBER_INFO" | grep -q "not found"; then
    echo "  Inviting session_granter ($GRANTER_ADDR)..."

    TX_RES=$($BINARY tx rep invite-member \
        $GRANTER_ADDR \
        "100" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to invite session_granter: no txhash"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            INVITATION_ID=$(extract_event_value "$TX_RESULT" "create_invitation" "invitation_id")
            if [ -z "$INVITATION_ID" ]; then
                INVITATION_ID="1"
            fi
            echo "  Invited session_granter (invitation #$INVITATION_ID)"

            # Accept invitation
            echo "  session_granter accepting invitation #$INVITATION_ID..."
            TX_RES=$($BINARY tx rep accept-invitation \
                $INVITATION_ID \
                --from session_granter \
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
                    echo "  session_granter is now a member"
                else
                    echo "  Failed to accept invitation"
                fi
            fi
        else
            echo "  Failed to invite session_granter"
        fi
    fi
else
    echo "  session_granter is already a member, skipping"
fi

echo ""

# ========================================================================
# 4. Verify Setup
# ========================================================================
echo "Step 4: Verifying setup..."

ALL_SUCCESS=true

# Check granter is a member
MEMBER_INFO=$($BINARY query rep get-member $GRANTER_ADDR --output json 2>&1)
if echo "$MEMBER_INFO" | grep -q "not found"; then
    echo "  session_granter is NOT a member"
    ALL_SUCCESS=false
else
    echo "  session_granter: member OK"
fi

# Check all accounts have balance
for ACCOUNT in "session_granter" "session_grantee1" "session_grantee2"; do
    ADDR=$($BINARY keys show $ACCOUNT -a --keyring-backend test)
    BALANCE=$($BINARY query bank balances $ADDR --output json 2>/dev/null | jq -r '.balances[] | select(.denom=="uspark") | .amount // "0"' || echo "0")
    echo "  $ACCOUNT: $BALANCE uspark"
    if [ "$BALANCE" = "0" ] || [ -z "$BALANCE" ]; then
        ALL_SUCCESS=false
    fi
done

echo ""

# ========================================================================
# Export Environment Variables
# ========================================================================
cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables for x/session tests
export GRANTER_ADDR=$GRANTER_ADDR
export GRANTEE1_ADDR=$GRANTEE1_ADDR
export GRANTEE2_ADDR=$GRANTEE2_ADDR
export ALICE_ADDR=$ALICE_ADDR
EOF

echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "Test environment ready:"
echo "  session_granter: $GRANTER_ADDR (funded + rep member)"
echo "  session_grantee1: $GRANTEE1_ADDR (funded)"
echo "  session_grantee2: $GRANTEE2_ADDR (funded)"
echo ""
echo "Environment variables saved to: $SCRIPT_DIR/.test_env"
echo ""

if [ "$ALL_SUCCESS" = false ]; then
    echo "Some accounts may not be properly initialized"
    echo "Review the output above for errors"
    exit 1
fi
