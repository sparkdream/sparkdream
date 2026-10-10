#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/artifact Tests"
echo "=================================================="
echo ""

# ============================================================================
# Configuration
# ============================================================================
# Actors:
#   alice  - genesis founder: x/rep member and Commons Operations Committee
#            member (council path for hides, appeal resolution, op params)
#   bob    - genesis x/rep member
#   carol  - genesis x/rep member (royalty recipient / payout address)
#   artifact_outsider - fresh key, NOT a member: untrusted sender, inbox
#            recipient, public-mint buyer
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"

if [ -f "$SCRIPT_DIR/.test_env" ]; then
    rm -f "$SCRIPT_DIR/.test_env"
fi

wait_for_tx() {
    local TXHASH=$1 ATTEMPT=0 RESULT
    while [ $ATTEMPT -lt 30 ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>/dev/null)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done
    echo "ERROR: Transaction $TXHASH not found" >&2
    return 1
}

# ============================================================================
# 1. Outsider key
# ============================================================================
echo "Step 1: Creating the non-member test key..."
if ! $BINARY keys show artifact_outsider --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add artifact_outsider --keyring-backend test --output json > /dev/null 2>&1
    echo "  Created key: artifact_outsider"
else
    echo "  Key exists: artifact_outsider"
fi
OUTSIDER_ADDR=$($BINARY keys show artifact_outsider -a --keyring-backend test)
echo "  Address: $OUTSIDER_ADDR"
echo ""

# ============================================================================
# 2. Fund the outsider (fees, deposits, public mints, purchases)
# ============================================================================
echo "Step 2: Funding artifact_outsider with 500 SPARK..."
TX_RES=$($BINARY tx bank send alice $OUTSIDER_ADDR 500000000${BOND_DENOM} \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" = "null" ]; then
    echo "  ERROR: funding tx not submitted: $TX_RES"
    exit 1
fi
RESULT=$(wait_for_tx $TXHASH) || exit 1
if [ "$(echo "$RESULT" | jq -r '.code')" != "0" ]; then
    echo "  ERROR: funding failed: $(echo "$RESULT" | jq -r '.raw_log')"
    exit 1
fi
echo "  Funded."
echo ""

# ============================================================================
# 3. Sanity: alice/bob/carol are members, outsider is not
# ============================================================================
echo "Step 3: Verifying membership..."
for KEY in alice bob carol; do
    ADDR=$($BINARY keys show $KEY -a --keyring-backend test)
    STATUS=$($BINARY query rep get-member "$ADDR" --output json 2>/dev/null | jq -r '.member.status // empty')
    echo "  $KEY: ${STATUS:-not a member}"
done
if $BINARY query rep get-member "$OUTSIDER_ADDR" --output json 2>/dev/null | jq -e '.member.status' > /dev/null; then
    echo "  ERROR: artifact_outsider unexpectedly a member"
    exit 1
fi
echo "  artifact_outsider: not a member (as expected)"
echo ""

cat > "$SCRIPT_DIR/.test_env" <<ENVEOF
export OUTSIDER_ADDR=$OUTSIDER_ADDR
ENVEOF
echo "Environment variables saved to: $SCRIPT_DIR/.test_env"
echo "Setup complete."
