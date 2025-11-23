#!bin/bash

# NOTE: distrtypes.ModuleName must be temporarily removed from blockAccAddrs in app_config.go to
# allow sending a test amount to the community pool that will be distributed by the split module.

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
DISTR_ADDR=$($BINARY query auth module-account distribution --output json | jq -r '.account.value.address')
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.value.address')
ECOSYSTEM_ADDR=$($BINARY query auth module-account ecosystem --output json | jq -r '.account.value.address')
COMMONS_COUNCIL_ADDR=$($BINARY query commons params --output json | jq -r '.params.commons_council_address')

echo "--- TESTING AUTODIVERT: FUNDING COMMONS POOL ---"
# Send 1000 SPARK to the Commons Pool (DISTR_ADDR)
$BINARY tx bank send $ALICE_ADDR $DISTR_ADDR 1000000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test

# Wait for 2 blocks for the BeginBlocker to run
sleep 5

echo "--- CHECKING COMMUNITY POOL (DISTR_ADDR) BALANCE (Expected 0 SPARK remaining) ---"
$BINARY query bank balances $DISTR_ADDR

echo "--- CHECKING COMMONS POOL (COMMONS_COUNCIL_ADDR) BALANCE (Expected 500 SPARK diverted) ---"
$BINARY query bank balances $COMMONS_COUNCIL_ADDR

echo "--- CHECKING TECHNICAL POOL (GOV_ADDR) BALANCE (Expected 300 SPARK diverted) ---"
$BINARY query bank balances $GOV_ADDR

echo "--- CHECKING ECOSYSTEM POOL (ECOSYSTEM_ADDR) BALANCE (Expected 200 SPARK diverted) ---"
$BINARY query bank balances $ECOSYSTEM_ADDR