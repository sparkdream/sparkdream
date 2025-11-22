#!/bin/bash

echo "--- TESTING NAME MODULE: PRIMARY ALIAS ---"

# --- 0. SETUP & CONFIG ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)

# --- 1. SETUP: Ensure Alice has a name ---
echo "Registering backup name 'alice_v2' to ensure ownership..."

# Submit Tx
RES=$($BINARY tx name register-name "alice_v2" "meta" --from alice -y --chain-id $CHAIN_ID --keyring-backend test -o json)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 3

# Verify Execution
QUERY_RES=$($BINARY query tx $TX_HASH -o json)
CODE=$(echo $QUERY_RES | jq -r '.code')

if [ "$CODE" != "0" ]; then
    # It might fail if already exists, which is fine for setup, but we warn
    echo "⚠️  Registration msg code: $CODE (Might already exist)"
else
    echo "✅ Name Registered."
fi

# --- 2. TEST: SET PRIMARY ---
echo "--- CASE 1: Alice sets 'alice_v2' as Primary ---"

# Submit Tx
RES=$($BINARY tx name set-primary "alice_v2" --from alice -y --chain-id $CHAIN_ID --keyring-backend test -o json)
TX_HASH=$(echo $RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
sleep 3

# Verify Execution
QUERY_RES=$($BINARY query tx $TX_HASH -o json)
CODE=$(echo $QUERY_RES | jq -r '.code')

if [ "$CODE" != "0" ]; then
    echo "❌ FAILURE: Failed to set primary name."
    echo "Raw Log: $(echo $QUERY_RES | jq -r '.raw_log')"
    exit 1
fi

echo "✅ Set Primary Tx Executed."

# --- 3. TEST: REVERSE RESOLVE ---
echo "--- CASE 2: Reverse Resolve (Address -> Name) ---"

PRIMARY_NAME=$($BINARY query name reverse-resolve $ALICE_ADDR -o json | jq -r '.name')

echo "Resolved Name: $PRIMARY_NAME"

if [ "$PRIMARY_NAME" == "alice_v2" ]; then
    echo "✅ SUCCESS: Reverse resolution worked."
else
    echo "❌ FAILURE: Expected 'alice_v2', got '$PRIMARY_NAME'"
    exit 1
fi

# --- 4. TEST: QUERY BY ADDRESS ---
echo "--- CASE 3: Query All Names for Alice ---"
NAMES_COUNT=$($BINARY query name names $ALICE_ADDR -o json | jq '.names | length')

echo "Alice owns $NAMES_COUNT names."
if [ "$NAMES_COUNT" -ge 1 ]; then
    echo "✅ SUCCESS: Found names list."
else
    echo "❌ FAILURE: No names found for Alice."
fi