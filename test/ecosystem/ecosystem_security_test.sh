#!/bin/bash

echo "--- TESTING SECURITY: UNAUTHORIZED SPEND ATTEMPT ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
DENOM="uspark"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
echo "Attacker (Carol): $CAROL_ADDR"

# Discover the Ecosystem Module Address
ECO_MODULE_ADDR=$($BINARY query auth module-account ecosystem --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Ecosystem Pool:   $ECO_MODULE_ADDR"

# --- 1. CHECK INITIAL STATES ---
echo "--- SNAPSHOTTING BALANCES ---"

# Get Ecosystem Balance
ECO_START=$($BINARY query bank balances $ECO_MODULE_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$ECO_START" ]; then ECO_START=0; fi

# Get Carol's Balance
CAROL_START=$($BINARY query bank balances $CAROL_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$CAROL_START" ]; then CAROL_START=0; fi

echo "Ecosystem Start: $ECO_START $DENOM"
echo "Carol Start:     $CAROL_START $DENOM"

if [ "$ECO_START" == "0" ]; then
    echo "⚠️  WARNING: Ecosystem pool is empty. Test might not be meaningful."
fi

# --- 2. THE ATTACK ---
echo "--- PHASE 1: CAROL ATTEMPTS DIRECT THEFT ---"
echo "Carol is attempting to call 'tx ecosystem spend' directly, bypassing Governance..."

# We use '|| true' to prevent the script from exiting if the binary returns an error code.
ATTACK_OUTPUT=$($BINARY tx ecosystem spend $CAROL_ADDR 1000000$DENOM --from carol -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>&1 || true)

# --- 3. VERIFY FAILURE ---
echo "Analyzing Attack Result..."

IS_FAILED=false

if echo "$ATTACK_OUTPUT" | grep -q "invalid authority"; then
    echo "✅ Attack blocked during simulation (Invalid Authority)."
    IS_FAILED=true
elif echo "$ATTACK_OUTPUT" | grep -q "unauthorized"; then
    echo "✅ Attack blocked during simulation (Unauthorized)."
    IS_FAILED=true
elif echo "$ATTACK_OUTPUT" | grep -q "failed to execute message"; then
    echo "✅ Attack blocked during simulation (Execution Failure)."
    IS_FAILED=true
else
    # If it returned a TxHash, we must check if it failed on-chain
    TX_HASH=$(echo "$ATTACK_OUTPUT" | jq -r '.txhash // empty')
    
    if [ ! -z "$TX_HASH" ]; then
        echo "Tx Broadcasted ($TX_HASH). Checking on-chain status..."
        sleep 6
        
        TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
        TX_CODE=$(echo "$TX_RES" | jq -r '.code')
        
        if [ "$TX_CODE" != "0" ] && [ "$TX_CODE" != "null" ]; then
            echo "✅ Attack transaction failed on-chain with code $TX_CODE."
            IS_FAILED=true
        elif [ "$TX_CODE" == "0" ]; then
            echo "❌ CRITICAL FAILURE: Attack transaction succeeded! Carol stole funds."
            IS_FAILED=false
        else
            echo "❌ ERROR: Could not determine Tx status."
            echo "$TX_RES"
        fi
    else
        echo "❓ Unknown output from CLI:"
        echo "$ATTACK_OUTPUT"
    fi
fi

# --- 4. VERIFY BALANCES (DOUBLE CHECK) ---
echo "--- VERIFYING FUNDS ARE SAFE ---"

# Get Ecosystem Balance
ECO_END=$($BINARY query bank balances $ECO_MODULE_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$ECO_END" ]; then ECO_END=0; fi

# Get Carol's Balance
CAROL_END=$($BINARY query bank balances $CAROL_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$CAROL_END" ]; then CAROL_END=0; fi

echo "Ecosystem End: $ECO_END $DENOM"
echo "Carol End:     $CAROL_END $DENOM"

# FIX: Check for DECREASE (Theft) rather than exact match. 
# An increase is expected due to block rewards/inflation accumulation.
if [ "$ECO_END" -lt "$ECO_START" ]; then
    echo "❌ ALARM: Ecosystem balance DECREASED! Theft occurred."
    echo "Difference: $((ECO_START - ECO_END)) $DENOM lost."
    exit 1
elif [ "$ECO_END" -gt "$ECO_START" ]; then
    echo "ℹ️  Note: Ecosystem balance increased (+$((ECO_END - ECO_START)) $DENOM). This is normal (block rewards)."
fi

if [ "$CAROL_START" != "$CAROL_END" ]; then
    # Carol spends gas fees even if tx fails, so slight decrease is expected if tx was broadcast.
    if [ "$CAROL_END" -gt "$CAROL_START" ]; then
        echo "❌ ALARM: Carol's balance increased! Theft confirmed."
        exit 1
    fi
fi

if [ "$IS_FAILED" = true ]; then
    echo "🎉 SECURITY CHECK PASSED: The ecosystem module is secure against direct access."
else
    echo "❌ SECURITY CHECK FAILED."
    exit 1
fi