#!/bin/bash

echo "--- TESTING: FUTARCHY OPERATIONAL PARAMS UPDATE (COUNCIL-GATED) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Futarchy uses Technical Council for authorization
COUNCIL_NAME="Technical Council"
echo "Looking up '$COUNCIL_NAME'..."
COUNCIL_INFO=$($BINARY query commons get-extended-group "$COUNCIL_NAME" --output json)
COUNCIL_POLICY=$(echo $COUNCIL_INFO | jq -r '.extended_group.policy_address')

if [ -z "$COUNCIL_POLICY" ] || [ "$COUNCIL_POLICY" == "null" ]; then
    echo "SETUP ERROR: '$COUNCIL_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

echo "Alice Address:    $ALICE_ADDR"
echo "Bob Address:      $BOB_ADDR"
echo "Council Policy:   $COUNCIL_POLICY"
echo ""

# --- Result Tracking ---
QUERY_PARAMS_RESULT="FAIL"
UPDATE_PARAMS_RESULT="FAIL"
VERIFY_OPERATIONAL_RESULT="FAIL"
VERIFY_GOVERNANCE_RESULT="FAIL"
RESET_PARAMS_RESULT="FAIL"

# Helper: extract group proposal ID from tx hash
get_group_proposal_id() {
    local tx_hash=$1
    local retries=0
    local max_retries=10
    local prop_id=""

    while [ $retries -lt $max_retries ]; do
        sleep 1
        TX_RES=$($BINARY query tx $tx_hash --output json 2>/dev/null)
        if [ $? -eq 0 ]; then
            prop_id=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
            if [ ! -z "$prop_id" ] && [ "$prop_id" != "null" ]; then
                echo "$prop_id"
                return 0
            fi
        fi
        ((retries++))
    done
    return 1
}

# Helper: vote + execute a group proposal
vote_and_execute() {
    local prop_id=$1

    echo "  Alice voting YES..."
    $BINARY tx group vote $prop_id $ALICE_ADDR VOTE_OPTION_YES "Approve" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --output json > /dev/null 2>&1
    sleep 3

    echo "  Bob voting YES..."
    $BINARY tx group vote $prop_id $BOB_ADDR VOTE_OPTION_YES "Approve" \
        --from bob -y --chain-id $CHAIN_ID --keyring-backend test \
        --output json > /dev/null 2>&1
    sleep 3

    echo "  Executing proposal $prop_id..."
    EXEC_RES=$($BINARY tx group exec $prop_id \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --output json)
    EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')
    sleep 3

    EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json 2>/dev/null)
    if echo "$EXEC_TX_JSON" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
        echo "  Execution successful"
        return 0
    else
        echo "  Execution failed"
        echo "  Raw: $(echo $EXEC_TX_JSON | jq -r '.raw_log' 2>/dev/null)"
        return 1
    fi
}

# --- 1. QUERY INITIAL PARAMETERS ---
echo "--- TEST 1: QUERY INITIAL FUTARCHY PARAMETERS ---"

PARAMS_JSON=$($BINARY query futarchy params --output json)

INITIAL_TRADING_FEE=$(echo $PARAMS_JSON | jq -r '.params.trading_fee_bps')
INITIAL_MAX_DURATION=$(echo $PARAMS_JSON | jq -r '.params.max_duration')
INITIAL_MAX_REDEMPTION=$(echo $PARAMS_JSON | jq -r '.params.max_redemption_delay')

# Governance-only fields (should NOT change)
INITIAL_MIN_LIQ=$(echo $PARAMS_JSON | jq -r '.params.min_liquidity')
INITIAL_DEFAULT_TICK=$(echo $PARAMS_JSON | jq -r '.params.default_min_tick')
INITIAL_MAX_LMSR=$(echo $PARAMS_JSON | jq -r '.params.max_lmsr_exponent')

echo "Operational params:"
echo "  trading_fee_bps:      $INITIAL_TRADING_FEE"
echo "  max_duration:         $INITIAL_MAX_DURATION"
echo "  max_redemption_delay: $INITIAL_MAX_REDEMPTION"
echo "Governance-only params:"
echo "  min_liquidity:        $INITIAL_MIN_LIQ"
echo "  default_min_tick:     $INITIAL_DEFAULT_TICK"
echo "  max_lmsr_exponent:    $INITIAL_MAX_LMSR"

if [ -z "$INITIAL_TRADING_FEE" ] || [ "$INITIAL_TRADING_FEE" == "null" ]; then
    echo "  FAIL: Could not query initial parameters"
else
    QUERY_PARAMS_RESULT="PASS"
    echo "  PASS: Initial parameters queried successfully"
fi
echo ""

# --- 2. UPDATE OPERATIONAL PARAMS VIA COUNCIL PROPOSAL ---
echo "--- TEST 2: UPDATE OPERATIONAL PARAMS VIA COUNCIL PROPOSAL ---"

if [ "$QUERY_PARAMS_RESULT" == "PASS" ]; then
    # Double the trading fee and max duration
    NEW_TRADING_FEE="100"
    NEW_MAX_DURATION="2419200"
    NEW_MAX_REDEMPTION="7200"

    echo '{
      "group_policy_address": "'$COUNCIL_POLICY'",
      "proposers": ["'$ALICE_ADDR'"],
      "title": "Update Futarchy Operational Params",
      "summary": "Adjust trading fee and duration limits via Operations Committee",
      "messages": [
        {
          "@type": "/sparkdream.futarchy.v1.MsgUpdateOperationalParams",
          "authority": "'$COUNCIL_POLICY'",
          "operational_params": {
            "trading_fee_bps": "'$NEW_TRADING_FEE'",
            "max_duration": "'$NEW_MAX_DURATION'",
            "max_redemption_delay": "'$NEW_MAX_REDEMPTION'"
          }
        }
      ]
    }' > "$PROPOSAL_DIR/update_op_params.json"

    SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/update_op_params.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

    echo "Submitted tx: $TX_HASH"
    PROPOSAL_ID=$(get_group_proposal_id $TX_HASH)

    if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
        echo "  FAIL: Could not submit operational params proposal"
    else
        echo "Proposal ID: $PROPOSAL_ID"
        vote_and_execute $PROPOSAL_ID
        if [ $? -eq 0 ]; then
            UPDATE_PARAMS_RESULT="PASS"
            echo "  PASS: Operational params update proposal executed"
        else
            echo "  FAIL: Operational params update proposal failed to execute"
        fi
    fi
else
    echo "  SKIP: Query params failed, cannot submit update"
fi
echo ""

# --- 3. VERIFY OPERATIONAL PARAMS UPDATED ---
echo "--- TEST 3: VERIFY OPERATIONAL PARAMS UPDATED ---"

if [ "$UPDATE_PARAMS_RESULT" == "PASS" ]; then
    UPDATED_PARAMS=$($BINARY query futarchy params --output json)
    UPDATED_TRADING_FEE=$(echo $UPDATED_PARAMS | jq -r '.params.trading_fee_bps')
    UPDATED_MAX_DURATION=$(echo $UPDATED_PARAMS | jq -r '.params.max_duration')
    UPDATED_MAX_REDEMPTION=$(echo $UPDATED_PARAMS | jq -r '.params.max_redemption_delay')

    echo "  trading_fee_bps:      $UPDATED_TRADING_FEE (expected: $NEW_TRADING_FEE)"
    echo "  max_duration:         $UPDATED_MAX_DURATION (expected: $NEW_MAX_DURATION)"
    echo "  max_redemption_delay: $UPDATED_MAX_REDEMPTION (expected: $NEW_MAX_REDEMPTION)"

    VERIFY_OP_OK=true
    if [ "$UPDATED_TRADING_FEE" != "$NEW_TRADING_FEE" ]; then
        echo "  trading_fee_bps mismatch (got $UPDATED_TRADING_FEE)"
        VERIFY_OP_OK=false
    fi
    if [ "$UPDATED_MAX_DURATION" != "$NEW_MAX_DURATION" ]; then
        echo "  max_duration mismatch (got $UPDATED_MAX_DURATION)"
        VERIFY_OP_OK=false
    fi
    if [ "$UPDATED_MAX_REDEMPTION" != "$NEW_MAX_REDEMPTION" ]; then
        echo "  max_redemption_delay mismatch (got $UPDATED_MAX_REDEMPTION)"
        VERIFY_OP_OK=false
    fi

    if [ "$VERIFY_OP_OK" == true ]; then
        VERIFY_OPERATIONAL_RESULT="PASS"
        echo "  PASS: All operational params updated correctly"
    else
        echo "  FAIL: Some operational params did not update"
    fi
else
    echo "  SKIP: Update failed, cannot verify"
fi
echo ""

# --- 4. VERIFY GOVERNANCE-ONLY FIELDS UNCHANGED ---
echo "--- TEST 4: VERIFY GOVERNANCE-ONLY FIELDS UNCHANGED ---"

if [ "$UPDATE_PARAMS_RESULT" == "PASS" ]; then
    CURRENT_MIN_LIQ=$(echo $UPDATED_PARAMS | jq -r '.params.min_liquidity')
    CURRENT_DEFAULT_TICK=$(echo $UPDATED_PARAMS | jq -r '.params.default_min_tick')
    CURRENT_MAX_LMSR=$(echo $UPDATED_PARAMS | jq -r '.params.max_lmsr_exponent')

    echo "  min_liquidity:    $CURRENT_MIN_LIQ (expected: $INITIAL_MIN_LIQ)"
    echo "  default_min_tick: $CURRENT_DEFAULT_TICK (expected: $INITIAL_DEFAULT_TICK)"
    echo "  max_lmsr_exp:     $CURRENT_MAX_LMSR (expected: $INITIAL_MAX_LMSR)"

    VERIFY_GOV_OK=true
    if [ "$CURRENT_MIN_LIQ" != "$INITIAL_MIN_LIQ" ]; then
        echo "  min_liquidity was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_DEFAULT_TICK" != "$INITIAL_DEFAULT_TICK" ]; then
        echo "  default_min_tick was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_MAX_LMSR" != "$INITIAL_MAX_LMSR" ]; then
        echo "  max_lmsr_exponent was modified by operational update!"
        VERIFY_GOV_OK=false
    fi

    if [ "$VERIFY_GOV_OK" == true ]; then
        VERIFY_GOVERNANCE_RESULT="PASS"
        echo "  PASS: Governance-only fields preserved"
    else
        echo "  FAIL: Governance-only fields were modified"
    fi
else
    echo "  SKIP: Update failed, cannot verify governance fields"
fi
echo ""

# --- 5. RESET PARAMS TO ORIGINAL VALUES ---
echo "--- TEST 5: RESET OPERATIONAL PARAMS TO ORIGINAL ---"

if [ "$UPDATE_PARAMS_RESULT" == "PASS" ]; then
    echo '{
      "group_policy_address": "'$COUNCIL_POLICY'",
      "proposers": ["'$ALICE_ADDR'"],
      "title": "Reset Futarchy Operational Params",
      "summary": "Restoring original values after test",
      "messages": [
        {
          "@type": "/sparkdream.futarchy.v1.MsgUpdateOperationalParams",
          "authority": "'$COUNCIL_POLICY'",
          "operational_params": {
            "trading_fee_bps": "'$INITIAL_TRADING_FEE'",
            "max_duration": "'$INITIAL_MAX_DURATION'",
            "max_redemption_delay": "'$INITIAL_MAX_REDEMPTION'"
          }
        }
      ]
    }' > "$PROPOSAL_DIR/reset_op_params.json"

    SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/reset_op_params.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

    PROPOSAL_ID=$(get_group_proposal_id $TX_HASH)

    if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
        echo "  FAIL: Could not submit reset proposal"
    else
        vote_and_execute $PROPOSAL_ID
        if [ $? -eq 0 ]; then
            # Verify reset
            RESET_PARAMS=$($BINARY query futarchy params --output json)
            RESET_TRADING_FEE=$(echo $RESET_PARAMS | jq -r '.params.trading_fee_bps')

            if [ "$RESET_TRADING_FEE" == "$INITIAL_TRADING_FEE" ]; then
                RESET_PARAMS_RESULT="PASS"
                echo "  PASS: Params reset to original values"
            else
                echo "  FAIL: Params did not reset correctly (got $RESET_TRADING_FEE, expected $INITIAL_TRADING_FEE)"
            fi
        else
            echo "  FAIL: Reset proposal failed to execute"
        fi
    fi
else
    echo "  SKIP: Update failed, nothing to reset"
fi
echo ""

# --- RESULTS SUMMARY ---
echo "============================================================================"
echo "  FUTARCHY OPERATIONAL PARAMS TEST RESULTS"
echo "============================================================================"
echo ""

TOTAL_COUNT=0
PASS_COUNT=0
FAIL_COUNT=0

for RESULT in "$QUERY_PARAMS_RESULT" "$UPDATE_PARAMS_RESULT" "$VERIFY_OPERATIONAL_RESULT" "$VERIFY_GOVERNANCE_RESULT" "$RESET_PARAMS_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

echo "  1. Query Initial Params:          $QUERY_PARAMS_RESULT"
echo "  2. Update Operational Params:      $UPDATE_PARAMS_RESULT"
echo "  3. Verify Operational Updated:     $VERIFY_OPERATIONAL_RESULT"
echo "  4. Verify Governance Unchanged:    $VERIFY_GOVERNANCE_RESULT"
echo "  5. Reset Params to Original:       $RESET_PARAMS_RESULT"
echo ""
echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
