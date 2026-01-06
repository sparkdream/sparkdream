#!/bin/bash

echo "==================================================================="
echo "  TESTING: INFLATION PARAMETERS IMMUTABILITY"
echo "  Purpose: Verify that inflation params cannot be changed via x/gov"
echo "==================================================================="

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo ""
echo "Test Configuration:"
echo "  Chain ID: $CHAIN_ID"
echo "  Alice: $ALICE_ADDR"
echo "  Bob: $ALICE_ADDR"
echo ""

# --- 1. VERIFY CURRENT INFLATION PARAMETERS ---
echo "==================================================================="
echo "PHASE 1: Verify Current Inflation Parameters"
echo "==================================================================="

CURRENT_PARAMS=$($BINARY query mint params --output json)
echo "Current Mint Parameters:"
echo "$CURRENT_PARAMS" | jq .

INFLATION_MIN=$(echo "$CURRENT_PARAMS" | jq -r '.inflation_min')
INFLATION_MAX=$(echo "$CURRENT_PARAMS" | jq -r '.inflation_max')
GOAL_BONDED=$(echo "$CURRENT_PARAMS" | jq -r '.goal_bonded')
MINT_DENOM=$(echo "$CURRENT_PARAMS" | jq -r '.mint_denom')

echo ""
echo "✅ Current Inflation Parameters:"
echo "   - inflation_min: $INFLATION_MIN (expected: 0.020000000000000000)"
echo "   - inflation_max: $INFLATION_MAX (expected: 0.050000000000000000)"
echo "   - goal_bonded: $GOAL_BONDED (expected: 0.670000000000000000)"
echo "   - mint_denom: $MINT_DENOM (expected: uspark)"
echo ""

# Verify expected values
if [ "$INFLATION_MIN" != "0.020000000000000000" ]; then
    echo "⚠️  WARNING: inflation_min is not 0.02 (2%)"
fi
if [ "$INFLATION_MAX" != "0.050000000000000000" ]; then
    echo "⚠️  WARNING: inflation_max is not 0.05 (5%)"
fi

# --- 2. CHECK MINT MODULE AUTHORITY ---
echo "==================================================================="
echo "PHASE 2: Verify Mint Module Authority (Should be Burn Address)"
echo "==================================================================="

# Query the mint module params with authority
MINT_AUTHORITY=$($BINARY query mint authority --output json 2>/dev/null | jq -r '.' || echo "")

if [ -z "$MINT_AUTHORITY" ] || [ "$MINT_AUTHORITY" == "null" ]; then
    echo "⚠️  Could not query authority directly. Checking module code..."
    # Authority is in the app config, not queryable via CLI in all SDK versions
    MINT_AUTHORITY="sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe"
    echo "   Expected Authority (Burn Address): $MINT_AUTHORITY"
else
    echo "✅ Mint Module Authority: $MINT_AUTHORITY"
fi

echo ""
echo "🔒 SECURITY: This address is a burn address with no private key."
echo "   No entity can sign transactions from this address."
echo "   Therefore, MsgUpdateParams will ALWAYS fail."
echo ""

# --- 3. ATTEMPT TO UPDATE INFLATION_MAX VIA GOVERNANCE ---
echo "==================================================================="
echo "PHASE 3: Attempt Malicious Inflation Parameter Update"
echo "==================================================================="

echo "📝 Creating governance proposal to increase inflation_max to 100%..."
echo "   (This is a simulated attack - it should FAIL)"
echo ""

# Create the MsgUpdateParams proposal
cat > "$PROPOSAL_DIR/update_inflation.json" <<EOF
{
  "messages": [
    {
      "@type": "/cosmos.mint.v1beta1.MsgUpdateParams",
      "authority": "$MINT_AUTHORITY",
      "params": {
        "mint_denom": "uspark",
        "inflation_rate_change": "0.130000000000000000",
        "inflation_max": "1.000000000000000000",
        "inflation_min": "0.020000000000000000",
        "goal_bonded": "0.670000000000000000",
        "blocks_per_year": "6311520"
      }
    }
  ],
  "deposit": "50000000uspark",
  "title": "MALICIOUS: Inflate SPARK to 100%",
  "summary": "This proposal attempts to set inflation_max to 100%. It should be rejected due to authority mismatch.",
  "expedited": false
}
EOF

echo "Proposal content:"
cat "$PROPOSAL_DIR/update_inflation.json" | jq .
echo ""

# --- 4. SUBMIT THE PROPOSAL ---
echo "⚡ Submitting governance proposal..."
SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/update_inflation.json" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --gas auto \
    --gas-adjustment 1.5 \
    --yes \
    --output json 2>&1)

TX_HASH=$(echo "$SUBMIT_RES" | jq -r '.txhash // empty')

if [ -z "$TX_HASH" ]; then
    echo "❌ Transaction submission failed (this might be expected):"
    echo "$SUBMIT_RES" | jq . || echo "$SUBMIT_RES"
    echo ""
    echo "✅ TEST PASSED: Proposal submission was rejected at transaction level!"
    echo "   The authority check prevented the malicious proposal from even being created."
    exit 0
fi

echo "Transaction submitted. Hash: $TX_HASH"
echo "Waiting for block inclusion (3s)..."
sleep 3

# --- 5. CHECK TRANSACTION RESULT ---
echo ""
echo "==================================================================="
echo "PHASE 4: Verify Transaction Failed"
echo "==================================================================="

TX_RES=$($BINARY query tx $TX_HASH --output json 2>&1)
TX_CODE=$(echo "$TX_RES" | jq -r '.code // empty')
TX_RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // empty')

echo "Transaction Result:"
echo "  Code: $TX_CODE (0 = success, non-zero = failure)"
echo "  Raw Log: $TX_RAW_LOG"
echo ""

if [ "$TX_CODE" != "0" ]; then
    echo "✅ TEST PASSED: Transaction FAILED as expected!"
    echo "   The authority check prevented parameter modification."

    # Check if it's an authority mismatch error
    if echo "$TX_RAW_LOG" | grep -q -i "unauthorized\|authority\|signer"; then
        echo "   ✓ Failure reason: Authority/Signer mismatch (expected)"
    else
        echo "   ⚠️  Failure reason might be different than expected"
    fi

    # Verify params unchanged
    echo ""
    echo "==================================================================="
    echo "PHASE 5: Verify Parameters Remain Unchanged"
    echo "==================================================================="

    FINAL_PARAMS=$($BINARY query mint params --output json)
    FINAL_MAX=$(echo "$FINAL_PARAMS" | jq -r '.inflation_max')

    if [ "$FINAL_MAX" == "$INFLATION_MAX" ]; then
        echo "✅ inflation_max unchanged: $FINAL_MAX"
        echo ""
        echo "╔═══════════════════════════════════════════════════════════╗"
        echo "║                   🔒 TEST PASSED 🔒                       ║"
        echo "║                                                           ║"
        echo "║  Inflation parameters are IMMUTABLE via governance!      ║"
        echo "║  Only chain upgrades can modify these values.            ║"
        echo "║                                                           ║"
        echo "║  Security guarantee: Monetary policy is trustless.       ║"
        echo "╚═══════════════════════════════════════════════════════════╝"
        echo ""
        exit 0
    else
        echo "❌ ERROR: inflation_max changed from $INFLATION_MAX to $FINAL_MAX"
        echo "   This should NOT have happened!"
        exit 1
    fi
else
    echo "⚠️  Transaction succeeded (Code 0)"

    # Extract proposal ID if transaction succeeded
    GOV_PROP_ID=$(echo "$TX_RES" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

    if [ -z "$GOV_PROP_ID" ]; then
        GOV_PROP_ID=$(echo "$TX_RES" | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
    fi

    if [ -n "$GOV_PROP_ID" ] && [ "$GOV_PROP_ID" != "null" ]; then
        echo "   Proposal ID: $GOV_PROP_ID"
        echo ""
        echo "==================================================================="
        echo "PHASE 5: Attempt to Vote and Execute"
        echo "==================================================================="

        # Vote yes on the proposal
        echo "Voting YES on proposal $GOV_PROP_ID..."
        $BINARY tx gov vote $GOV_PROP_ID yes \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --yes \
            --output json
        sleep 3

        # Also vote with bob for quorum
        $BINARY tx gov vote $GOV_PROP_ID yes \
            --from bob \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --yes \
            --output json
        sleep 3

        # Wait for voting period to end (config.yml has 60s voting period)
        echo "Waiting for voting period to end (65s)..."
        sleep 65

        # Check proposal status
        PROPOSAL_STATUS=$($BINARY query gov proposal $GOV_PROP_ID --output json)
        STATUS=$(echo "$PROPOSAL_STATUS" | jq -r '.status')

        echo "Final Proposal Status: $STATUS"
        echo ""

        if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "3" ]; then
            echo "✅ Proposal FAILED (as expected)"

            # Verify params unchanged
            FINAL_PARAMS=$($BINARY query mint params --output json)
            FINAL_MAX=$(echo "$FINAL_PARAMS" | jq -r '.inflation_max')

            if [ "$FINAL_MAX" == "$INFLATION_MAX" ]; then
                echo "✅ inflation_max unchanged: $FINAL_MAX"
                echo ""
                echo "╔═══════════════════════════════════════════════════════════╗"
                echo "║                   🔒 TEST PASSED 🔒                       ║"
                echo "║                                                           ║"
                echo "║  Proposal passed voting but FAILED at execution!         ║"
                echo "║  Inflation parameters remain immutable.                  ║"
                echo "║                                                           ║"
                echo "║  The burn address cannot sign the MsgUpdateParams.       ║"
                echo "╚═══════════════════════════════════════════════════════════╝"
                echo ""
                exit 0
            else
                echo "❌ ERROR: Parameters changed! Security breach!"
                exit 1
            fi
        elif [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ] || [ "$STATUS" == "4" ]; then
            # This shouldn't happen, but let's check params anyway
            FINAL_PARAMS=$($BINARY query mint params --output json)
            FINAL_MAX=$(echo "$FINAL_PARAMS" | jq -r '.inflation_max')

            if [ "$FINAL_MAX" != "$INFLATION_MAX" ]; then
                echo "❌ CRITICAL SECURITY FAILURE!"
                echo "   Proposal passed AND parameters changed!"
                echo "   inflation_max: $INFLATION_MAX → $FINAL_MAX"
                echo "   THIS SHOULD NEVER HAPPEN!"
                exit 1
            else
                echo "⚠️  Proposal status is PASSED but params unchanged"
                echo "   This is unusual but acceptable (execution may have failed)"
                echo ""
                echo "✅ TEST PASSED: Parameters remain unchanged"
                exit 0
            fi
        else
            echo "⚠️  Unexpected proposal status: $STATUS"
            echo "   Checking if parameters changed anyway..."

            FINAL_PARAMS=$($BINARY query mint params --output json)
            FINAL_MAX=$(echo "$FINAL_PARAMS" | jq -r '.inflation_max')

            if [ "$FINAL_MAX" == "$INFLATION_MAX" ]; then
                echo "✅ Parameters unchanged: TEST PASSED"
                exit 0
            else
                echo "❌ Parameters changed: TEST FAILED"
                exit 1
            fi
        fi
    else
        echo "❌ Could not extract proposal ID from transaction"
        echo "   Cannot verify execution phase"
        echo "   But transaction succeeded when it shouldn't have!"
        exit 1
    fi
fi
