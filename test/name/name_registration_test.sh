#!/bin/bash

echo "--- TESTING NAME MODULE: REGISTRATION, VALIDATION & PERMISSIONS ---"

# --- 0. SETUP & CONFIG ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
DENOM="uspark"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "[FAIL] Error: jq is not installed."
    exit 1
fi

# Actors
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test) # Active x/rep member (founder)
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)   # Non-rep-member (uninvited)

echo "Alice (rep member):  $ALICE_ADDR"
echo "Dave (non-rep-member): $DAVE_ADDR"

# Fetch Params
echo "Fetching Name Params..."
PARAMS=$($BINARY query name params --output json)
MAX_LEN=$(echo $PARAMS | jq -r '.params.max_name_length')
MIN_LEN=$(echo $PARAMS | jq -r '.params.min_name_length')
FEE_AMOUNT=$(echo $PARAMS | jq -r '.params.registration_fee.amount')

echo "Constraints: Min $MIN_LEN, Max $MAX_LEN, Fee $FEE_AMOUNT $DENOM"

# --- PRE-FLIGHT CHECK: IS ALICE AN ACTIVE x/rep MEMBER? ---
# Per commit 44c71ca, name registration is gated on x/rep membership
# (`IsActiveRepMember`), NOT on Commons Council membership. The keeper at
# x/name/keeper/msg_server_register_name.go:52-66 returns
# "only active x/rep members can register names" when the gate fails.
# This pre-flight verifies the right invariant and gives a useful warning
# when Alice's rep member record is missing or non-ACTIVE.
echo "Verifying Alice is an active x/rep member..."
REP_MEMBER=$($BINARY query rep get-member "$ALICE_ADDR" --output json 2>/dev/null)
REP_STATUS=$(echo "$REP_MEMBER" | jq -r '.member.status // empty')

if [ -z "$REP_STATUS" ] || [ "$REP_STATUS" = "null" ]; then
    echo "[WARN]  WARNING: Alice has no x/rep Member record."
    echo "    Valid registration tests are likely to fail."
elif [ "$REP_STATUS" != "MEMBER_STATUS_ACTIVE" ]; then
    echo "[WARN]  WARNING: Alice's rep status is $REP_STATUS (expected MEMBER_STATUS_ACTIVE)."
    echo "    Valid registration tests are likely to fail."
else
    echo "[ OK ] Pre-flight: Alice is an ACTIVE x/rep member."
fi

# --- 1. TEST: UNAUTHORIZED REGISTRATION (Dave) ---
echo "--- CASE 1: Dave (Non-Council) tries to register 'dave' ---"

RES=$($BINARY tx name register-name "dave" "meta" --from dave -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
CODE=$(echo $RES | jq -r '.code')

if [ "$CODE" != "0" ]; then
    echo "[ OK ] SUCCESS: Dave was blocked immediately (AnteHandler/CheckTx)."
else
    # If it passed CheckTx, check DeliverTx
    TX_HASH=$(echo $RES | jq -r '.txhash')
    sleep 4
    QUERY_RES=$($BINARY query tx $TX_HASH --output json)
    FINAL_CODE=$(echo $QUERY_RES | jq -r '.code')
    RAW_LOG=$(echo $QUERY_RES | jq -r '.raw_log')

    if [ "$FINAL_CODE" != "0" ]; then
        # The keeper rejects with "only active x/rep members can register
        # names" (msg_server_register_name.go:62). Match either the canonical
        # phrase or the bare "x/rep" / "unauthorized" tokens so the message
        # check is robust against future error-string tweaks.
        if echo "$RAW_LOG" | grep -qiE "x/rep members|x/rep|unauthorized|active rep member"; then
            echo "[ OK ] SUCCESS: Dave blocked (rep-membership gate)."
        else
            echo "[ OK ] SUCCESS: Dave blocked (Code $FINAL_CODE) — but error doesn't reference the rep gate"
            echo "    raw_log: $(echo "$RAW_LOG" | head -c 200)"
        fi
    else
        echo "[FAIL] FAILURE: Dave successfully registered a name!"
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
    echo "[ OK ] SUCCESS: Short name rejected."
elif [ "$(echo $QUERY_RES | jq -r '.code')" != "0" ]; then
    echo "[ OK ] SUCCESS: Short name rejected (Code != 0)."
else
    echo "[FAIL] FAILURE: Short name accepted."
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
    echo "[ OK ] SUCCESS: Invalid characters rejected."
elif [ "$(echo $QUERY_RES | jq -r '.code')" != "0" ]; then
    echo "[ OK ] SUCCESS: Invalid characters rejected (Code != 0)."
else
    echo "[FAIL] FAILURE: Invalid characters accepted."
    echo "DEBUG LOG: $RAW_LOG"
fi

# C. Blocked/Reserved Word
echo "Attempting Reserved Word: 'admin'..."
RES=$($BINARY tx name register-name "admin" "meta" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 4
QUERY_RES=$($BINARY query tx $TX_HASH --output json)

if echo "$QUERY_RES" | jq -r '.raw_log' | grep -q "reserved"; then
    echo "[ OK ] SUCCESS: 'admin' is reserved."
else
    echo "[FAIL] FAILURE: 'admin' check failed or passed unexpectedly."
fi

# D. Boundary characters — explicit regression guards from commit 44c71ca.
# The validNameRegex `^[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?$`:
#   - REJECTS leading hyphen (`-alice-test`) and trailing hyphen (`alice-test-`)
#   - ACCEPTS leading underscore and trailing underscore (`_alice_under_`)
# These three cases are easy to break in a "let me tighten the regex" PR
# without realising it; the boundary checks below catch any drift.
expect_rejected() {
    local NAME="$1" LABEL="$2"
    local res txh res_q raw code
    res=$($BINARY tx name register-name "$NAME" "meta" --from alice -y \
        --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
    txh=$(echo "$res" | jq -r '.txhash')
    sleep 4
    res_q=$($BINARY query tx "$txh" --output json 2>&1)
    raw=$(echo "$res_q" | jq -r '.raw_log')
    code=$(echo "$res_q" | jq -r '.code')
    if [ "$code" != "0" ] && echo "$raw" | grep -qiE "invalid character|invalid name|cannot start|cannot end"; then
        echo "[ OK ] SUCCESS: $LABEL '$NAME' rejected by regex."
    elif [ "$code" != "0" ]; then
        echo "[ OK ] SUCCESS: $LABEL '$NAME' rejected (Code $code) — but error doesn't mention regex"
    else
        echo "[FAIL] FAILURE: $LABEL '$NAME' was accepted!"
        exit 1
    fi
}

expect_accepted_clean() {
    local NAME="$1" LABEL="$2"
    local res txh res_q code raw
    res=$($BINARY tx name register-name "$NAME" "meta" --from alice -y \
        --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
    txh=$(echo "$res" | jq -r '.txhash')
    sleep 4
    res_q=$($BINARY query tx "$txh" --output json 2>&1)
    code=$(echo "$res_q" | jq -r '.code')
    raw=$(echo "$res_q" | jq -r '.raw_log')
    if [ "$code" = "0" ]; then
        echo "[ OK ] SUCCESS: $LABEL '$NAME' accepted by regex."
    elif echo "$raw" | grep -qi "invalid character\|invalid name"; then
        echo "[FAIL] FAILURE: $LABEL '$NAME' rejected by regex (regression)."
        echo "    raw_log: $(echo "$raw" | head -c 200)"
        exit 1
    else
        # Some other error (e.g. fee, name already taken) — informational only.
        echo "[INFO] $LABEL '$NAME' returned Code $code (non-regex error): $(echo "$raw" | head -c 120)"
    fi
}

echo "Attempting Leading Hyphen: '-alice-test'..."
expect_rejected "-alice-test" "leading hyphen"

echo "Attempting Trailing Hyphen: 'alice-test-'..."
expect_rejected "alice-test-" "trailing hyphen"

echo "Attempting Leading + Trailing Underscore: '_alice_under_'..."
expect_accepted_clean "_alice_under_" "underscore boundary"

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
    echo "[FAIL] FAILURE: Valid registration failed!"
    echo "Raw Log: $(echo $QUERY_RES | jq -r '.raw_log')"
    exit 1
fi

# Verify Ownership
OWNER=$($BINARY query name resolve "alice-test" --output json | jq -r '.name_record.owner')
if [ "$OWNER" == "$ALICE_ADDR" ]; then
    echo "[ OK ] SUCCESS: Alice owns 'alice'."
else
    echo "[FAIL] FAILURE: Owner is $OWNER"
    exit 1
fi

# Verify Fee Deduction
BAL_END=$($BINARY query bank balances $ALICE_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$BAL_END" ]; then BAL_END=0; fi

DIFF=$((BAL_START - BAL_END))
echo "Spent: $DIFF $DENOM (Fee: $FEE_AMOUNT)"

if [ "$DIFF" -ge "$FEE_AMOUNT" ]; then
    echo "[ OK ] SUCCESS: Fee deducted."
else
    echo "[FAIL] FAILURE: Fee not deducted correctly."
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
        echo "[ OK ] SUCCESS: Duplicate registration blocked."
    else
        echo "[ OK ] SUCCESS: Blocked (Code $CODE)."
    fi
else
    echo "[FAIL] FAILURE: Duplicate name registered!"
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
        echo "[ OK ] SUCCESS: Metadata updated."
    else
        echo "[FAIL] FAILURE: Metadata mismatch. Expected $NEW_META, got $STORED_META"
    fi
else
    echo "[FAIL] FAILURE: Update Tx failed."
    echo "Raw Log: $(echo $QUERY_RES | jq -r '.raw_log')"
    exit 1
fi