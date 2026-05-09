#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/commons Tests"
echo "=================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test 2>/dev/null || echo "")
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test 2>/dev/null || echo "")

echo "Genesis member (Alice): $ALICE_ADDR"
echo ""

# Delete stale .test_env so it is regenerated from the current keyring
rm -f "$SCRIPT_DIR/.test_env"

# ========================================================================
# Helper Functions
# ========================================================================

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        local r
        r=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$r" | jq -e '.code' > /dev/null 2>&1; then
            echo "$r"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done
    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
    return 1
}

check_tx_success() {
    local r=$1
    local code
    code=$(echo "$r" | jq -r '.code')
    if [ "$code" != "0" ]; then
        echo "Transaction failed with code $code:" >&2
        echo "$r" | jq -r '.raw_log' >&2
        return 1
    fi
    return 0
}

extract_event_value() {
    local r=$1
    local type=$2
    local key=$3
    echo "$r" | jq -r ".events[] | select(.type==\"$type\") | .attributes[] | select(.key==\"$key\") | .value" | tr -d '"'
}

# Create a key, fund with SPARK, invite as x/rep member, accept invite, gift DREAM.
# category_test.sh expects poster1 and poster2 to be active x/rep members.
provision_member() {
    local NAME=$1

    if ! $BINARY keys show $NAME --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $NAME --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $NAME"
    else
        echo "  Key exists: $NAME"
    fi
    local ADDR
    ADDR=$($BINARY keys show $NAME -a --keyring-backend test)

    # Fund with SPARK for gas (10 SPARK is enough for the half-dozen post/category txs).
    local TX
    TX=$($BINARY tx bank send alice "$ADDR" 10000000uspark \
        --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y \
        --output json 2>&1)
    local H
    H=$(echo "$TX" | jq -r '.txhash')
    if [ -n "$H" ] && [ "$H" != "null" ]; then
        sleep 6; wait_for_tx "$H" >/dev/null
    fi

    # Already a member? skip invite.
    local M
    M=$($BINARY query rep get-member "$ADDR" --output json 2>&1)
    if ! echo "$M" | grep -q "not found"; then
        echo "  $NAME is already an x/rep member"
        return 0
    fi

    local STAKE
    STAKE=$($BINARY query rep required-invitation-stake "$ALICE_ADDR" --output json 2>/dev/null \
        | jq -r '.required_stake // "100000000"')
    TX=$($BINARY tx rep invite-member "$ADDR" "$STAKE" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y \
        --output json 2>&1)
    H=$(echo "$TX" | jq -r '.txhash')
    if [ -z "$H" ] || [ "$H" == "null" ]; then
        echo "  Failed to invite $NAME: no txhash"
        return 1
    fi
    sleep 6
    local R
    R=$(wait_for_tx "$H")
    if ! check_tx_success "$R"; then return 1; fi
    local INV
    INV=$(extract_event_value "$R" "create_invitation" "invitation_id")
    [ -z "$INV" ] && INV="$(extract_event_value "$R" "invitation_created" "invitation_id")"
    if [ -z "$INV" ]; then
        echo "  Could not extract invitation_id for $NAME"
        return 1
    fi

    TX=$($BINARY tx rep accept-invitation "$INV" \
        --from $NAME --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y \
        --output json 2>&1)
    H=$(echo "$TX" | jq -r '.txhash')
    sleep 6
    R=$(wait_for_tx "$H")
    if ! check_tx_success "$R"; then return 1; fi
    echo "  $NAME is now an x/rep member"
}

echo "Step 1: Provisioning poster1 / poster2 (category_test.sh requires them)..."
provision_member poster1
provision_member poster2
echo ""

POSTER1_ADDR=$($BINARY keys show poster1 -a --keyring-backend test 2>/dev/null || echo "")
POSTER2_ADDR=$($BINARY keys show poster2 -a --keyring-backend test 2>/dev/null || echo "")

cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables for x/commons tests
export ALICE_ADDR=$ALICE_ADDR
export BOB_ADDR=$BOB_ADDR
export CAROL_ADDR=$CAROL_ADDR
export POSTER1_ADDR=$POSTER1_ADDR
export POSTER2_ADDR=$POSTER2_ADDR
EOF

echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "Environment saved to: $SCRIPT_DIR/.test_env"
echo ""
