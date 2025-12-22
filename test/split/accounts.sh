#!/bin/bash

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
DENOM="uspark"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

echo "--- 1. DISCOVERING ADDRESSES ---"

# A. Module Accounts (Infrastructure)
DISTR_ADDR=$($BINARY query auth module-account distribution --output json | jq -r '.account.base_account.address // .account.value.address')
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
ECO_MODULE_ADDR=$($BINARY query auth module-account ecosystem --output json | jq -r '.account.base_account.address // .account.value.address')

# B. Council Groups (Governance Bodies)
# Helper function to get policy address by Group Name
get_policy_addr() {
    local name="$1"
    # Suppress error if group not found, return empty
    $BINARY query commons get-extended-group "$name" --output json 2>/dev/null | jq -r '.extended_group.policy_address // empty'
}

COMMONS_POLICY=$(get_policy_addr "Commons Council")
TECH_POLICY=$(get_policy_addr "Technical Council")
ECO_COUNCIL_POLICY=$(get_policy_addr "Ecosystem Council")

# --- 2. REPORTING BALANCES ---

print_balance() {
    local name="$1"
    local addr="$2"
    
    if [ -z "$addr" ] || [ "$addr" == "null" ]; then
        echo "Example: $name -> [NOT FOUND / NOT CREATED]"
    else
        # Fetch balance line (e.g., "1000000uspark")
        BAL=$($BINARY query bank balances $addr --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount + .denom' 2>/dev/null)
        if [ -z "$BAL" ]; then BAL="0$DENOM"; fi
        
        printf "%-25s %-45s %s\n" "$name:" "$addr" "$BAL"
    fi
}

echo ""
echo "=========================================================================================="
echo "                                  MODULE ACCOUNTS"
echo "=========================================================================================="
print_balance "Distribution Module" "$DISTR_ADDR"
print_balance "Gov Module"          "$GOV_ADDR"
print_balance "Ecosystem Module"    "$ECO_MODULE_ADDR"

echo ""
echo "=========================================================================================="
echo "                                  COUNCIL TREASURIES"
echo "=========================================================================================="
print_balance "Commons Council"     "$COMMONS_POLICY"
print_balance "Technical Council"   "$TECH_POLICY"
print_balance "Ecosystem Council"   "$ECO_COUNCIL_POLICY"
echo ""