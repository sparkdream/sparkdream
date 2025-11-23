#!/bin/bash

echo "--- TESTING NAME MODULE: REGISTRATION & PERMISSIONS ---"

# Run commons/group_member_update_test.sh first!

# --- 0. SETUP & CONFIG ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
DENOM="uspark"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

# Actors
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test) # Council Member
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)     # Council Member
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test) # Non-Member (Plebeian)

echo "Alice (Council): $ALICE_ADDR"
echo "Bob (Council):   $BOB_ADDR"
echo "Carol (Public):  $CAROL_ADDR"

# Fetch Params
echo "Fetching Name Params..."
MAX_LEN=$($BINARY query name params -o json | jq -r '.params.max_name_length')
MIN_LEN=$($BINARY query name params -o json | jq -r '.params.min_name_length')
FEE_AMOUNT=$($BINARY query name params -o json | jq -r '.params.registration_fee.amount')

echo "Constraints: Min $MIN_LEN, Max $MAX_LEN, Fee $FEE_AMOUNT $DENOM"

# --- 1. TEST: UNAUTHORIZED REGISTRATION (Carol) ---
echo "--- CASE 1: Carol (Non-Council) tries to register 'carol' ---"

# 1. Submit Tx (Async)
RES=$($BINARY tx name register-name "carol" "meta" --from carol -y --chain-id $CHAIN_ID --keyring-backend test -o json)
TX_HASH=$(echo $RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion (3s)..."
sleep 3

# 2. Query Tx Result (DeliverTx)
QUERY_RES=$($BINARY query tx $TX_HASH -o json)
CODE=$(echo $QUERY_RES | jq -r '.code')
RAW_LOG=$(echo $QUERY_RES | jq -r '.raw_log')

echo "Execution Code: $CODE"

# 3. Validate Failure
if [ "$CODE" != "0" ]; then
    if echo "$RAW_LOG" | grep -q "unauthorized"; then
        echo "✅ SUCCESS: Carol was blocked (Unauthorized)."
    elif echo "$RAW_LOG" | grep -q "only council members"; then
        echo "✅ SUCCESS: Carol was blocked (Council check caught it)."
    else
        echo "✅ SUCCESS: Carol was blocked (Code $CODE)."
        echo "Log: $RAW_LOG"
    fi
else
    echo "❌ FAILURE: Carol successfully registered a name!"
    echo "Raw Log: $RAW_LOG"
    exit 1
fi

# --- 2. TEST: BLOCKED NAME (Alice) ---
echo "--- CASE 2: Alice tries to register 'admin' (Blocked) ---"

RES=$($BINARY tx name register-name "admin" "meta" --from alice -y --chain-id $CHAIN_ID --keyring-backend test -o json)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 3

QUERY_RES=$($BINARY query tx $TX_HASH -o json)
CODE=$(echo $QUERY_RES | jq -r '.code')
RAW_LOG=$(echo $QUERY_RES | jq -r '.raw_log')

if [ "$CODE" != "0" ]; then
    if echo "$RAW_LOG" | grep -q "name is reserved"; then
        echo "✅ SUCCESS: 'admin' is blocked."
    else
        echo "✅ SUCCESS: Blocked execution (Code $CODE)."
        echo "Log: $RAW_LOG"
    fi
else
    echo "❌ FAILURE: Alice registered 'admin'!"
    echo "Raw Log: $RAW_LOG"
    exit 1
fi

# --- 3. TEST: VALID REGISTRATION (Alice) ---
echo "--- CASE 3: Alice registers 'alice' (Valid) ---"

# Capture Alice's balance before
BAL_START=$($BINARY query bank balances $ALICE_ADDR -o json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')

# Submit
RES=$($BINARY tx name register-name "alice" "My Metadata" --from alice -y --chain-id $CHAIN_ID --keyring-backend test -o json)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 3

# Query Result
QUERY_RES=$($BINARY query tx $TX_HASH -o json)
CODE=$(echo $QUERY_RES | jq -r '.code')

if [ "$CODE" != "0" ]; then
    echo "❌ FAILURE: Valid registration failed!"
    echo "Raw Log: $(echo $QUERY_RES | jq -r '.raw_log')"
    exit 1
fi

echo "✅ Valid Tx Executed."

# Verify Ownership
OWNER=$($BINARY query name resolve "alice" -o json | jq -r '.name_record.owner')

if [ "$OWNER" == "$ALICE_ADDR" ]; then
    echo "✅ SUCCESS: Alice owns 'alice'."
else
    echo "❌ FAILURE: Owner is $OWNER"
    exit 1
fi

# Verify Fee Deduction
BAL_END=$($BINARY query bank balances $ALICE_ADDR -o json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
DIFF=$((BAL_START - BAL_END))

echo "Total Cost: $DIFF $DENOM (Fee: $FEE_AMOUNT + Gas)"

# Since we don't set explicit gas fees, we just check if *at least* the registration fee was taken.
if [ "$DIFF" -ge "$FEE_AMOUNT" ]; then
    echo "✅ SUCCESS: Registration fee deducted."
else
    echo "❌ FAILURE: Fee not deducted correctly."
fi