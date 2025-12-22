#!/bin/bash

echo "--- TESTING NAME MODULE: REGISTRATION, VALIDATION & PERMISSIONS ---"

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
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test) # Non-Member (Plebeian)

echo "Alice (Council): $ALICE_ADDR"
echo "Dave (Public):  $DAVE_ADDR"

# Fetch Params
echo "Fetching Name Params..."
PARAMS=$($BINARY query name params --output json)
MAX_LEN=$(echo $PARAMS | jq -r '.params.max_name_length')
MIN_LEN=$(echo $PARAMS | jq -r '.params.min_name_length')
FEE_AMOUNT=$(echo $PARAMS | jq -r '.params.registration_fee.amount')

echo "Constraints: Min $MIN_LEN, Max $MAX_LEN, Fee $FEE_AMOUNT $DENOM"

# --- PRE-FLIGHT CHECK: IS ALICE IN COUNCIL? ---
# We query the Commons Council via the commons module to get the Group ID.
# (The group module's metadata is the description, not the name, so groups-by-name fails)
echo "Locating Commons Council..."
COUNCIL_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json)
COUNCIL_ID=$(echo $COUNCIL_INFO | jq -r '.extended_group.group_id')

if [ -z "$COUNCIL_ID" ] || [ "$COUNCIL_ID" == "null" ]; then
    echo "❌ Error: Could not find 'Commons Council' group ID."
    exit 1
fi
echo "Found Commons Council (Group ID: $COUNCIL_ID)"

MEMBERSHIP=$($BINARY query group group-members $COUNCIL_ID --output json | jq -r --arg ADDR "$ALICE_ADDR" '.members[] | select(.member.address==$ADDR)')

if [ -z "$MEMBERSHIP" ]; then
    echo "⚠️  WARNING: Alice is NOT found in the Commons Council (Group ID $COUNCIL_ID)."
    echo "    Valid registration tests are likely to fail."
else
    echo "✅ Pre-flight: Alice is a verified Council member."
fi

# --- 1. TEST: UNAUTHORIZED REGISTRATION (Dave) ---
echo "--- CASE 1: Dave (Non-Council) tries to register 'dave' ---"

RES=$($BINARY tx name register-name "dave" "meta" --from dave -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
CODE=$(echo $RES | jq -r '.code')

if [ "$CODE" != "0" ]; then
    echo "✅ SUCCESS: Dave was blocked immediately (AnteHandler/CheckTx)."
else
    # If it passed CheckTx, check DeliverTx
    TX_HASH=$(echo $RES | jq -r '.txhash')
    sleep 4
    QUERY_RES=$($BINARY query tx $TX_HASH --output json)
    FINAL_CODE=$(echo $QUERY_RES | jq -r '.code')
    RAW_LOG=$(echo $QUERY_RES | jq -r '.raw_log')

    if [ "$FINAL_CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -q "unauthorized" || echo "$RAW_LOG" | grep -q "not in council"; then
            echo "✅ SUCCESS: Dave blocked (Unauthorized)."
        else
            echo "✅ SUCCESS: Dave blocked (Code $FINAL_CODE)."
        fi
    else
        echo "❌ FAILURE: Dave successfully registered a name!"
        exit 1
    fi
fi

# --- 2. TEST: VALIDATION CHECKS (Invalid Names) ---
echo "--- CASE 2: Invalid Name Formats ---"

# A. Too Short (Assuming Min > 1)
SHORT_NAME="a"
echo "Attempting Short Name: '$SHORT_NAME'..."

# 1. Broadcast
RES=$($BINARY tx name register-name "$SHORT_NAME" "meta" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
TX_HASH=$(echo $RES | jq -r '.txhash')

# 2. Wait for block
sleep 4

# 3. Query Result
QUERY_RES=$($BINARY query tx $TX_HASH --output json 2>&1)
RAW_LOG=$(echo $QUERY_RES | jq -r '.raw_log')

# 4. Check Log
if echo "$RAW_LOG" | grep -q "too short"; then
    echo "✅ SUCCESS: Short name rejected."
elif [ "$(echo $QUERY_RES | jq -r '.code')" != "0" ]; then
    echo "✅ SUCCESS: Short name rejected (Code != 0)."
else
    echo "❌ FAILURE: Short name accepted."
    echo "DEBUG LOG: $RAW_LOG"
fi

# B. Invalid Characters (Regex)
BAD_NAME="bad name!"
echo "Attempting Invalid Chars: '$BAD_NAME'..."

# 1. Broadcast
RES=$($BINARY tx name register-name "$BAD_NAME" "meta" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
TX_HASH=$(echo $RES | jq -r '.txhash')

# 2. Wait for block
sleep 4

# 3. Query Result
QUERY_RES=$($BINARY query tx $TX_HASH --output json 2>&1)
RAW_LOG=$(echo $QUERY_RES | jq -r '.raw_log')

if echo "$RAW_LOG" | grep -q "invalid character"; then
    echo "✅ SUCCESS: Invalid characters rejected."
elif [ "$(echo $QUERY_RES | jq -r '.code')" != "0" ]; then
    echo "✅ SUCCESS: Invalid characters rejected (Code != 0)."
else
    echo "❌ FAILURE: Invalid characters accepted."
    echo "DEBUG LOG: $RAW_LOG"
fi

# C. Blocked/Reserved Word
echo "Attempting Reserved Word: 'admin'..."
RES=$($BINARY tx name register-name "admin" "meta" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 4
QUERY_RES=$($BINARY query tx $TX_HASH --output json)

if echo "$QUERY_RES" | jq -r '.raw_log' | grep -q "reserved"; then
    echo "✅ SUCCESS: 'admin' is reserved."
else 
    echo "❌ FAILURE: 'admin' check failed or passed unexpectedly."
fi

# --- 3. TEST: VALID REGISTRATION (Alice) ---
echo "--- CASE 3: Alice registers 'alice-test' (Valid) ---"

# Snapshot Balance
BAL_START=$($BINARY query bank balances $ALICE_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$BAL_START" ]; then BAL_START=0; fi

RES=$($BINARY tx name register-name "alice-test" "My Personal Metadata" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 4

# Verify
QUERY_RES=$($BINARY query tx $TX_HASH --output json)
CODE=$(echo $QUERY_RES | jq -r '.code')

if [ "$CODE" != "0" ]; then
    echo "❌ FAILURE: Valid registration failed!"
    echo "Raw Log: $(echo $QUERY_RES | jq -r '.raw_log')"
    exit 1
fi

# Verify Ownership
OWNER=$($BINARY query name resolve "alice-test" --output json | jq -r '.name_record.owner')
if [ "$OWNER" == "$ALICE_ADDR" ]; then
    echo "✅ SUCCESS: Alice owns 'alice'."
else
    echo "❌ FAILURE: Owner is $OWNER"
    exit 1
fi

# Verify Fee Deduction
BAL_END=$($BINARY query bank balances $ALICE_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$BAL_END" ]; then BAL_END=0; fi

DIFF=$((BAL_START - BAL_END))
echo "Spent: $DIFF $DENOM (Fee: $FEE_AMOUNT)"

if [ "$DIFF" -ge "$FEE_AMOUNT" ]; then
    echo "✅ SUCCESS: Fee deducted."
else
    echo "❌ FAILURE: Fee not deducted correctly."
    exit 1
fi

# --- 4. TEST: UNIQUENESS (Duplicate) ---
echo "--- CASE 4: Alice tries to register 'alice-test' AGAIN ---"

RES=$($BINARY tx name register-name "alice-test" "Duplicate attempt" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 4

QUERY_RES=$($BINARY query tx $TX_HASH --output json)
CODE=$(echo $QUERY_RES | jq -r '.code')
RAW_LOG=$(echo $QUERY_RES | jq -r '.raw_log')

if [ "$CODE" != "0" ]; then
    if echo "$RAW_LOG" | grep -q "already taken"; then
        echo "✅ SUCCESS: Duplicate registration blocked."
    else
        echo "✅ SUCCESS: Blocked (Code $CODE)."
    fi
else
    echo "❌ FAILURE: Duplicate name registered!"
    exit 1
fi

# --- 5. TEST: UPDATE METADATA ---
echo "--- CASE 5: Alice updates metadata for 'alice' ---"

NEW_META="IPFS://NewHash123"

RES=$($BINARY tx name update-name "alice-test" "$NEW_META" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 4

QUERY_RES=$($BINARY query tx $TX_HASH --output json)
CODE=$(echo $QUERY_RES | jq -r '.code')

if [ "$CODE" == "0" ]; then
    # Verify State
    STORED_META=$($BINARY query name resolve "alice-test" --output json | jq -r '.name_record.data')
    if [ "$STORED_META" == "$NEW_META" ]; then
        echo "✅ SUCCESS: Metadata updated."
    else
        echo "❌ FAILURE: Metadata mismatch. Expected $NEW_META, got $STORED_META"
    fi
else
    echo "❌ FAILURE: Update Tx failed."
    echo "Raw Log: $(echo $QUERY_RES | jq -r '.raw_log')"
    exit 1
fi