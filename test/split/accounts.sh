#!bin/bash

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
DISTR_ADDR=$($BINARY query auth module-account distribution --output json | jq -r '.account.value.address')
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.value.address')
ECOSYSTEM_ADDR=$($BINARY query auth module-account ecosystem --output json | jq -r '.account.value.address')
COMMONS_COUNCIL_ADDR=$($BINARY query split params --output json | jq -r '.params.commons_council_address')

echo "--- CHECKING COMMUNITY POOL (DISTR_ADDR) BALANCE ---"
$BINARY query bank balances $DISTR_ADDR

echo "--- CHECKING COMMONS POOL (COMMONS_COUNCIL_ADDR) BALANCE ---"
$BINARY query bank balances $COMMONS_COUNCIL_ADDR

echo "--- CHECKING TECHNICAL POOL (GOV_ADDR) BALANCE ---"
$BINARY query bank balances $GOV_ADDR

echo "--- CHECKING ECOSYSTEM POOL (ECOSYSTEM_ADDR) BALANCE ---"
$BINARY query bank balances $ECOSYSTEM_ADDR