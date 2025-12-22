#!/bin/bash

echo "--- TESTING NAME MODULE: PRIMARY ALIAS & REVERSE RESOLUTION ---"

# --- 0. SETUP & CONFIG ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo "Alice: $ALICE_ADDR"
echo "Bob:   $BOB_ADDR"

# --- 1. SETUP: REGISTER NAMES ---
echo "--- STEP 1: Registration Setup ---"

# Alice registers 'alice-alpha'
echo "Registering 'alice-alpha'..."
$BINARY tx name register-name "alice-alpha" "meta-alpha" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json > /dev/null
sleep 4

# Alice registers 'alice-beta'
echo "Registering 'alice-beta'..."
$BINARY tx name register-name "alice-beta" "meta-beta" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json > /dev/null
sleep 4

# Bob registers 'bob-main'
echo "Registering 'bob-main'..."
$BINARY tx name register-name "bob-main" "meta-bob" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json > /dev/null
sleep 4

# --- 2. SECURITY TEST: SET UNAUTHORIZED PRIMARY ---
echo "--- STEP 2: Security Checks (Unauthorized Set) ---"

# Case A: Alice tries to set a name she doesn't own (Bob's name)
echo "Alice attempting to set 'bob-main' (Bob's name) as primary..."
RES=$($BINARY tx name set-primary "bob-main" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
CODE=$(echo $RES | jq -r '.code')

if [ "$CODE" != "0" ]; then
     echo "✅ SUCCESS: Alice blocked from setting 'bob-main' (Ante/CheckTx)."
else
     # Check On-Chain result
     TX_HASH=$(echo $RES | jq -r '.txhash')
     sleep 4
     QUERY_RES=$($BINARY query tx $TX_HASH --output json)
     FINAL_CODE=$(echo $QUERY_RES | jq -r '.code')
     RAW_LOG=$(echo $QUERY_RES | jq -r '.raw_log')

     if [ "$FINAL_CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -q "not owner"; then
            echo "✅ SUCCESS: Alice blocked on-chain (Not Owner)."
        else
            echo "✅ SUCCESS: Alice blocked on-chain (Code $FINAL_CODE)."
        fi
     else
        echo "❌ FAILURE: Alice successfully set Bob's name as her primary!"
        exit 1
     fi
fi

# Case B: Alice tries to set a non-existent name
echo "Alice attempting to set 'ghost-name' (Does not exist)..."
RES=$($BINARY tx name set-primary "ghost-name" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 4
QUERY_RES=$($BINARY query tx $TX_HASH --output json)

if [ "$(echo $QUERY_RES | jq -r '.code')" != "0" ]; then
    echo "✅ SUCCESS: Setting non-existent name failed."
else
    echo "❌ FAILURE: Setting non-existent name succeeded."
    exit 1
fi

# --- 3. FUNCTIONALITY: SET PRIMARY ---
echo "--- STEP 3: Alice sets 'alice-alpha' as Primary ---"

RES=$($BINARY tx name set-primary "alice-alpha" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 4

QUERY_RES=$($BINARY query tx $TX_HASH --output json)
CODE=$(echo $QUERY_RES | jq -r '.code')

if [ "$CODE" != "0" ]; then
    echo "❌ FAILURE: Failed to set primary."
    echo "Log: $(echo $QUERY_RES | jq -r '.raw_log')"
    exit 1
fi

# Verify Reverse Resolve
RESOLVED=$($BINARY query name reverse-resolve $ALICE_ADDR --output json | jq -r '.name')
echo "Resolved: $RESOLVED"

if [ "$RESOLVED" == "alice-alpha" ]; then
    echo "✅ SUCCESS: Reverse resolve returns 'alice-alpha'."
else
    echo "❌ FAILURE: Expected 'alice-alpha', got '$RESOLVED'."
    exit 1
fi

# --- 4. FUNCTIONALITY: UPDATE PRIMARY ---
echo "--- STEP 4: Alice updates Primary to 'alice-beta' ---"

# Alice decides to change her main alias
RES=$($BINARY tx name set-primary "alice-beta" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 4

# Check Resolve
RESOLVED=$($BINARY query name reverse-resolve $ALICE_ADDR --output json | jq -r '.name')
echo "Resolved: $RESOLVED"

if [ "$RESOLVED" == "alice-beta" ]; then
    echo "✅ SUCCESS: Primary updated to 'alice-beta'."
else
    echo "❌ FAILURE: Expected 'alice-beta', got '$RESOLVED'."
    exit 1
fi

# --- 5. QUERY LIST VERIFICATION ---
echo "--- STEP 5: Verify Name List ---"
NAMES_COUNT=$($BINARY query name names $ALICE_ADDR --output json | jq '.names | length')

echo "Alice owns $NAMES_COUNT names."
if [ "$NAMES_COUNT" -ge 2 ]; then
    echo "✅ SUCCESS: Found multiple names for Alice."
else
    echo "❌ FAILURE: Name count incorrect (Expected >= 2)."
fi