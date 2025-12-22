#!/bin/bash

echo "--- TESTING SECURITY: UNAUTHORIZED SPEND ATTEMPTS (LOGIC & SPOOFING) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
DENOM="uspark"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

# We need a wealthy account to fund the pool (Alice) and an attacker (Carol)
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
echo "Funder (Alice):   $ALICE_ADDR"
echo "Attacker (Carol): $CAROL_ADDR"

# Discover Addresses
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
ECO_MODULE_ADDR=$($BINARY query auth module-account ecosystem --output json | jq -r '.account.base_account.address // .account.value.address')

echo "Governance Auth:  $GOV_ADDR"
echo "Ecosystem Pool:   $ECO_MODULE_ADDR"

# --- 1. BOOTSTRAP: FUND THE TARGET ---
echo "--- STEP 1: FUNDING ECOSYSTEM POOL ---"
# We send funds to the ecosystem module so there is actually something to steal.
# If the pool is empty, the attack might fail for the wrong reason (insufficient funds).
$BINARY tx bank send alice $ECO_MODULE_ADDR 10000000${DENOM} --chain-id $CHAIN_ID -y --fees 5000${DENOM} --keyring-backend test > /dev/null
sleep 6

# --- 2. CHECK INITIAL STATES ---
echo "--- SNAPSHOTTING BALANCES ---"

ECO_START=$($BINARY query bank balances $ECO_MODULE_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$ECO_START" ]; then ECO_START=0; fi

CAROL_START=$($BINARY query bank balances $CAROL_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$CAROL_START" ]; then CAROL_START=0; fi

echo "Ecosystem Start: $ECO_START $DENOM"

# --- 3. ATTACK VECTOR A: "I AM THE AUTHORITY" ---
echo "--- ATTACK A: INVALID AUTHORITY (LOGIC CHECK) ---"
echo "Carol calls MsgSpend claiming SHE is the authority..."

# Attempt 1: Standard CLI. Usually sets Authority = Signer (Carol).
# This should fail in the Keeper logic: "Expected Gov Address, got Carol".

ATTACK_A_OUT=$($BINARY tx ecosystem spend $CAROL_ADDR 1000000$DENOM --from carol -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>&1 || true)

if echo "$ATTACK_A_OUT" | grep -q "invalid authority" || echo "$ATTACK_A_OUT" | grep -q "unauthorized"; then
    echo "✅ Attack A blocked: Module logic correctly rejected Carol as authority."
elif echo "$ATTACK_A_OUT" | grep -q "failed to execute message"; then
    echo "✅ Attack A blocked: Execution failed."
else
    # Check tx code if broadcasted
    TX_HASH=$(echo "$ATTACK_A_OUT" | jq -r '.txhash // empty')
    if [ ! -z "$TX_HASH" ]; then
        sleep 6
        TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
        if [ "$(echo "$TX_RES" | jq -r '.code')" != "0" ]; then
             echo "✅ Attack A blocked on-chain (Code != 0)."
        else
             echo "❌ CRITICAL FAILURE: Attack A succeeded."
             exit 1
        fi
    else
         echo "❌ FAILURE: Unexpected output for Attack A."
         echo "$ATTACK_A_OUT"
    fi
fi

# --- 4. ATTACK VECTOR B: "GOV IS AUTHORITY (BUT I SIGN)" ---
echo "--- ATTACK B: SIGNATURE SPOOFING (ANTE HANDLER CHECK) ---"
echo "Carol signs a valid tx, then swaps the authority to Gov before broadcast..."

# 1. Generate a VALID transaction (Authority = Carol)
# We let the CLI generate it normally so authority defaults to the signer (Carol).
$BINARY tx ecosystem spend $CAROL_ADDR 1000000${DENOM} --from carol --chain-id $CHAIN_ID --generate-only > "$PROPOSAL_DIR/valid_tx.json"

# 2. Sign the VALID transaction
# This works because Signer (Carol) matches Authority (Carol).
$BINARY tx sign "$PROPOSAL_DIR/valid_tx.json" --from carol --chain-id $CHAIN_ID --keyring-backend test --output-document "$PROPOSAL_DIR/signed_temp.json"

# 3. MALICIOUS MODIFICATION (The Spoof)
# We now edit the SIGNED file to change Authority from Carol -> Gov.
# This invalidates the signature relative to the message content, which is exactly what we want to test.
jq --arg GOV "$GOV_ADDR" '.tx.body.messages[0].authority = $GOV' "$PROPOSAL_DIR/signed_temp.json" > "$PROPOSAL_DIR/attack_b_signed.json"

# NOTE: If the structure of your JSON is different (some versions use .body directly), try this fallback:
if [ ! -s "$PROPOSAL_DIR/attack_b_signed.json" ] || grep -q "null" "$PROPOSAL_DIR/attack_b_signed.json"; then
    jq --arg GOV "$GOV_ADDR" '.body.messages[0].authority = $GOV' "$PROPOSAL_DIR/signed_temp.json" > "$PROPOSAL_DIR/attack_b_signed.json"
fi

echo "Spoofed Transaction Created. Broadcasting..."

# 4. Broadcast
# The node receives a Tx where Body says "Authority: Gov" but Signature says "Signed by Carol (for Authority: Carol)".
# This MUST fail verification.
ATTACK_B_OUT=$($BINARY tx broadcast "$PROPOSAL_DIR/attack_b_signed.json" --output json 2>&1 || true)
echo "Broadcast Result: $ATTACK_B_OUT"

# Analyze B
IS_B_SAFE=false
if echo "$ATTACK_B_OUT" | grep -q "pubKey does not match signer address" || echo "$ATTACK_B_OUT" | grep -q "signature verification failed" || echo "$ATTACK_B_OUT" | grep -q "invalid pubkey"; then
    echo "✅ Attack B blocked: Signature verification failed (AnteHandler)."
    IS_B_SAFE=true
elif echo "$ATTACK_B_OUT" | grep -q "unauthorized"; then
    echo "✅ Attack B blocked: Unauthorized."
    IS_B_SAFE=true
else
     # If broadcasted, check code
     TX_HASH=$(echo "$ATTACK_B_OUT" | jq -r '.txhash // empty')
     if [ ! -z "$TX_HASH" ]; then
        sleep 6
        TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
        TX_CODE=$(echo "$TX_RES" | jq -r '.code')
        
        # Code 32 is "incorrect signature"
        if [ "$TX_CODE" != "0" ] && [ "$TX_CODE" != "null" ]; then
             echo "✅ Attack B blocked on-chain (Code $TX_CODE)."
             IS_B_SAFE=true
        elif [ "$TX_CODE" == "0" ]; then
             echo "❌ CRITICAL FAILURE: Attack B succeeded! Signature check bypassed."
             exit 1
        fi
     else
        # If we are here, it might have failed cleanly at CLI level
        echo "✅ Attack B blocked (CLI/Validation error)."
        IS_B_SAFE=true
     fi
fi

# --- 5. FINAL ACCOUNTING ---
echo "--- VERIFYING FUNDS ARE SAFE ---"

ECO_END=$($BINARY query bank balances $ECO_MODULE_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$ECO_END" ]; then ECO_END=0; fi

CAROL_END=$($BINARY query bank balances $CAROL_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$CAROL_END" ]; then CAROL_END=0; fi

echo "Ecosystem End: $ECO_END $DENOM"
echo "Carol End:     $CAROL_END $DENOM"

# Check for THEFT (Decrease in Ecosystem)
if [ "$ECO_END" -lt "$ECO_START" ]; then
    echo "❌ ALARM: Ecosystem balance DECREASED! Theft occurred."
    echo "Lost: $((ECO_START - ECO_END)) $DENOM"
    exit 1
fi

# Check for GAIN (Increase in Attacker, ignoring gas spend)
# If Carol succeeded, she would have +1,000,000. 
# If she failed, she has Start - Fees.
if [ "$CAROL_END" -gt "$((CAROL_START + 5000))" ]; then
    echo "❌ ALARM: Carol's balance increased significantly!"
    exit 1
fi

echo "🎉 SECURITY AUDIT PASSED: All attack vectors failed. Funds are safe."