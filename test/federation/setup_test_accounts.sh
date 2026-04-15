#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/federation Tests"
echo "=================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Get alice and bob addresses (genesis members / council members)
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test 2>/dev/null || echo "")

echo "Genesis member (Alice): $ALICE_ADDR"
echo "Genesis member (Bob):   $BOB_ADDR"
echo "Genesis member (Carol): $CAROL_ADDR"
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

# operator1/2 = bridge operators, verifier1/2 = content verifiers,
# linker1/2 = identity linkers, challenger1 = challenge verifier
ACCOUNTS=("operator1" "operator2" "verifier1" "verifier2" "linker1" "linker2" "challenger1")

for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $ACCOUNT"
    else
        echo "  Key exists: $ACCOUNT"
    fi
done

# Get addresses
OPERATOR1_ADDR=$($BINARY keys show operator1 -a --keyring-backend test)
OPERATOR2_ADDR=$($BINARY keys show operator2 -a --keyring-backend test)
VERIFIER1_ADDR=$($BINARY keys show verifier1 -a --keyring-backend test)
VERIFIER2_ADDR=$($BINARY keys show verifier2 -a --keyring-backend test)
LINKER1_ADDR=$($BINARY keys show linker1 -a --keyring-backend test)
LINKER2_ADDR=$($BINARY keys show linker2 -a --keyring-backend test)
CHALLENGER1_ADDR=$($BINARY keys show challenger1 -a --keyring-backend test)

echo ""

# ========================================================================
# 2. Fund Test Accounts with SPARK (for gas fees + bridge stakes)
# ========================================================================
echo "Step 2: Funding test accounts with SPARK..."

# Operators need extra SPARK for bridge stakes (1000 SPARK = 1000000000uspark per stake)
# Give operators 5000 SPARK, others 100 SPARK
for ADDR in $OPERATOR1_ADDR $OPERATOR2_ADDR; do
    echo "  Sending 5000 SPARK to operator $ADDR..."
    TX_RES=$($BINARY tx bank send \
        alice $ADDR \
        5000000000uspark \
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

for ADDR in $VERIFIER1_ADDR $VERIFIER2_ADDR $LINKER1_ADDR $LINKER2_ADDR $CHALLENGER1_ADDR; do
    echo "  Sending 100 SPARK to $ADDR..."
    TX_RES=$($BINARY tx bank send \
        alice $ADDR \
        100000000uspark \
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
# 3. Invite Test Accounts to x/rep (Required for trust levels)
# ========================================================================
echo "Step 3: Inviting test accounts to become x/rep members..."

INVITATION_IDS=()

for i in "${!ACCOUNTS[@]}"; do
    ACCOUNT="${ACCOUNTS[$i]}"

    case "$ACCOUNT" in
        "operator1")   ADDR=$OPERATOR1_ADDR ;;
        "operator2")   ADDR=$OPERATOR2_ADDR ;;
        "verifier1")   ADDR=$VERIFIER1_ADDR ;;
        "verifier2")   ADDR=$VERIFIER2_ADDR ;;
        "linker1")     ADDR=$LINKER1_ADDR ;;
        "linker2")     ADDR=$LINKER2_ADDR ;;
        "challenger1") ADDR=$CHALLENGER1_ADDR ;;
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
            INVITATION_ID=$((i + 1))
        fi
        echo "  Invitation ID: $INVITATION_ID"
        INVITATION_IDS+=($INVITATION_ID)
    else
        echo "  Failed to invite $ACCOUNT"
        INVITATION_IDS+=("")
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
        echo "  Skipping $ACCOUNT (no invitation or already member)"
        continue
    fi

    echo "  $ACCOUNT accepting invitation $INVITATION_ID..."
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
        echo "  Failed to accept: no txhash"
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        echo "  $ACCOUNT is now a member"
    else
        echo "  $ACCOUNT failed to accept invitation"
    fi
done

echo ""

# ========================================================================
# 5. Get Council/Committee Policy Addresses
# ========================================================================
echo "Step 5: Looking up council and committee policy addresses..."

# Commons Council policy (for peer lifecycle)
COMMONS_INFO=$($BINARY query commons get-group "Commons Council" --output json 2>&1)
COMMONS_POLICY=$(echo "$COMMONS_INFO" | jq -r '.group.policy_address')
if [ -z "$COMMONS_POLICY" ] || [ "$COMMONS_POLICY" == "null" ]; then
    echo "  WARNING: Commons Council not found"
    COMMONS_POLICY=""
else
    echo "  Commons Council Policy: $COMMONS_POLICY"
fi

# Operations Committee policy (for bridge/policy mgmt)
OPS_INFO=$($BINARY query commons get-group "Commons Operations Committee" --output json 2>&1)
OPS_POLICY=$(echo "$OPS_INFO" | jq -r '.group.policy_address')
if [ -z "$OPS_POLICY" ] || [ "$OPS_POLICY" == "null" ]; then
    echo "  WARNING: Operations Committee not found"
    OPS_POLICY=""
else
    echo "  Operations Committee Policy: $OPS_POLICY"
fi

echo ""

# ========================================================================
# 6. Export Test Environment
# ========================================================================
echo "Step 6: Exporting test environment..."

cat > "$SCRIPT_DIR/.test_env" <<EOF
export ALICE_ADDR=$ALICE_ADDR
export BOB_ADDR=$BOB_ADDR
export CAROL_ADDR=$CAROL_ADDR
export OPERATOR1_ADDR=$OPERATOR1_ADDR
export OPERATOR2_ADDR=$OPERATOR2_ADDR
export VERIFIER1_ADDR=$VERIFIER1_ADDR
export VERIFIER2_ADDR=$VERIFIER2_ADDR
export LINKER1_ADDR=$LINKER1_ADDR
export LINKER2_ADDR=$LINKER2_ADDR
export CHALLENGER1_ADDR=$CHALLENGER1_ADDR
export COMMONS_POLICY=$COMMONS_POLICY
export OPS_POLICY=$OPS_POLICY
EOF

echo "  Wrote .test_env"
echo ""

# ========================================================================
# Summary
# ========================================================================
echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "  Accounts:      ${#ACCOUNTS[@]} created/verified"
echo "  Council:       Commons Council  = $COMMONS_POLICY"
echo "  Committee:     Operations Cmte  = $OPS_POLICY"
echo ""
echo "  operator1:     $OPERATOR1_ADDR"
echo "  operator2:     $OPERATOR2_ADDR"
echo "  verifier1:     $VERIFIER1_ADDR"
echo "  verifier2:     $VERIFIER2_ADDR"
echo "  linker1:       $LINKER1_ADDR"
echo "  linker2:       $LINKER2_ADDR"
echo "  challenger1:   $CHALLENGER1_ADDR"
echo ""
