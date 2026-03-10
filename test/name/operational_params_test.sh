#!/bin/bash

echo "--- TESTING: NAME OPERATIONAL PARAMS UPDATE (COUNCIL-GATED) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Name uses Commons Council for authorization
COUNCIL_NAME="Commons Council"
echo "Looking up '$COUNCIL_NAME'..."
COUNCIL_INFO=$($BINARY query commons get-group "$COUNCIL_NAME" --output json)
COUNCIL_POLICY=$(echo $COUNCIL_INFO | jq -r '.group.policy_address')

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

# Helper: extract commons proposal ID from tx hash
get_commons_proposal_id() {
    local tx_hash=$1
    local retries=0
    local max_retries=10
    local prop_id=""

    while [ $retries -lt $max_retries ]; do
        sleep 1
        TX_RES=$($BINARY query tx $tx_hash --output json 2>/dev/null)
        if [ $? -eq 0 ]; then
            prop_id=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
            if [ ! -z "$prop_id" ] && [ "$prop_id" != "null" ]; then
                echo "$prop_id"
                return 0
            fi
        fi
        ((retries++))
    done
    return 1
}

# Helper: vote + execute a commons proposal
vote_and_execute() {
    local prop_id=$1

    echo "  Alice voting YES..."
    $BINARY tx commons vote-proposal $prop_id yes \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 6

    echo "  Bob voting YES..."
    $BINARY tx commons vote-proposal $prop_id yes \
        --from bob -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 6

    echo "  Executing proposal $prop_id..."
    EXEC_RES=$($BINARY tx commons execute-proposal $prop_id \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --gas 2000000 --output json)
    EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')
    sleep 6

    # Verify execution by checking proposal status
    PROP_STATUS=$($BINARY query commons get-proposal $prop_id --output json 2>/dev/null | jq -r '.proposal.status')
    if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
        echo "  Execution successful"
        return 0
    else
        echo "  Execution failed (status: $PROP_STATUS)"
        EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json 2>/dev/null)
        echo "  Raw: $(echo $EXEC_TX_JSON | jq -r '.raw_log' 2>/dev/null)"
        return 1
    fi
}

# --- 1. QUERY INITIAL PARAMETERS ---
echo "--- TEST 1: QUERY INITIAL NAME PARAMETERS ---"

PARAMS_JSON=$($BINARY query name params --output json)

# Operational fields we'll test
INITIAL_DISPUTE_TIMEOUT=$(echo $PARAMS_JSON | jq -r '.params.dispute_timeout_blocks')
INITIAL_REG_FEE=$(echo $PARAMS_JSON | jq -r '.params.registration_fee.amount')
INITIAL_DISPUTE_STAKE=$(echo $PARAMS_JSON | jq -r '.params.dispute_stake_dream')

# Governance-only fields (should NOT change)
INITIAL_MIN_NAME_LEN=$(echo $PARAMS_JSON | jq -r '.params.min_name_length')
INITIAL_MAX_NAME_LEN=$(echo $PARAMS_JSON | jq -r '.params.max_name_length')
INITIAL_MAX_NAMES=$(echo $PARAMS_JSON | jq -r '.params.max_names_per_address')

echo "Operational params (subset):"
echo "  dispute_timeout_blocks: $INITIAL_DISPUTE_TIMEOUT"
echo "  registration_fee:       $INITIAL_REG_FEE uspark"
echo "  dispute_stake_dream:    $INITIAL_DISPUTE_STAKE"
echo "Governance-only params:"
echo "  min_name_length:        $INITIAL_MIN_NAME_LEN"
echo "  max_name_length:        $INITIAL_MAX_NAME_LEN"
echo "  max_names_per_address:  $INITIAL_MAX_NAMES"

if [ -z "$INITIAL_DISPUTE_TIMEOUT" ] || [ "$INITIAL_DISPUTE_TIMEOUT" == "null" ]; then
    echo "  FAIL: Could not query initial parameters"
else
    QUERY_PARAMS_RESULT="PASS"
    echo "  PASS: Initial parameters queried successfully"
fi
echo ""

# --- 2. BUILD AND SUBMIT OPERATIONAL PARAMS UPDATE ---
echo "--- TEST 2: UPDATE OPERATIONAL PARAMS VIA COUNCIL PROPOSAL ---"

if [ "$QUERY_PARAMS_RESULT" == "PASS" ]; then
    # Extract all operational fields from current params
    OP_PARAMS=$(echo "$PARAMS_JSON" | jq '.params | {
      expiration_duration,
      registration_fee,
      dispute_stake_dream,
      dispute_timeout_blocks,
      contest_stake_dream
    }')

    # Modify test fields: change dispute timeout
    NEW_DISPUTE_TIMEOUT="201600"
    OP_PARAMS=$(echo "$OP_PARAMS" | jq '.dispute_timeout_blocks = "'$NEW_DISPUTE_TIMEOUT'"')

    # Build the proposal JSON
    jq -n \
      --arg policy "$COUNCIL_POLICY" \
      --argjson op_params "$OP_PARAMS" \
    '{
      policy_address: $policy,
      messages: [{
        "@type": "/sparkdream.name.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }],
      metadata: "Update Name Operational Params - Adjust dispute timeout via Operations Committee"
    }' > "$PROPOSAL_DIR/update_name_op_params.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_name_op_params.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

    echo "Submitted tx: $TX_HASH"
    PROPOSAL_ID=$(get_commons_proposal_id $TX_HASH)

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
    UPDATED_PARAMS=$($BINARY query name params --output json)
    UPDATED_DISPUTE_TIMEOUT=$(echo $UPDATED_PARAMS | jq -r '.params.dispute_timeout_blocks')

    echo "  dispute_timeout_blocks: $UPDATED_DISPUTE_TIMEOUT (expected: $NEW_DISPUTE_TIMEOUT)"

    if [ "$UPDATED_DISPUTE_TIMEOUT" == "$NEW_DISPUTE_TIMEOUT" ]; then
        VERIFY_OPERATIONAL_RESULT="PASS"
        echo "  PASS: Operational params updated correctly"
    else
        echo "  FAIL: dispute_timeout_blocks not updated (got $UPDATED_DISPUTE_TIMEOUT)"
    fi
else
    echo "  SKIP: Update failed, cannot verify"
fi
echo ""

# --- 4. VERIFY GOVERNANCE-ONLY FIELDS UNCHANGED ---
echo "--- TEST 4: VERIFY GOVERNANCE-ONLY FIELDS UNCHANGED ---"

if [ "$UPDATE_PARAMS_RESULT" == "PASS" ]; then
    CURRENT_MIN_NAME_LEN=$(echo $UPDATED_PARAMS | jq -r '.params.min_name_length')
    CURRENT_MAX_NAME_LEN=$(echo $UPDATED_PARAMS | jq -r '.params.max_name_length')
    CURRENT_MAX_NAMES=$(echo $UPDATED_PARAMS | jq -r '.params.max_names_per_address')

    echo "  min_name_length:       $CURRENT_MIN_NAME_LEN (expected: $INITIAL_MIN_NAME_LEN)"
    echo "  max_name_length:       $CURRENT_MAX_NAME_LEN (expected: $INITIAL_MAX_NAME_LEN)"
    echo "  max_names_per_address: $CURRENT_MAX_NAMES (expected: $INITIAL_MAX_NAMES)"

    VERIFY_GOV_OK=true
    if [ "$CURRENT_MIN_NAME_LEN" != "$INITIAL_MIN_NAME_LEN" ]; then
        echo "  min_name_length was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_MAX_NAME_LEN" != "$INITIAL_MAX_NAME_LEN" ]; then
        echo "  max_name_length was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_MAX_NAMES" != "$INITIAL_MAX_NAMES" ]; then
        echo "  max_names_per_address was modified by operational update!"
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
    RESET_OP_PARAMS=$(echo "$PARAMS_JSON" | jq '.params | {
      expiration_duration,
      registration_fee,
      dispute_stake_dream,
      dispute_timeout_blocks,
      contest_stake_dream
    }')

    jq -n \
      --arg policy "$COUNCIL_POLICY" \
      --argjson op_params "$RESET_OP_PARAMS" \
    '{
      policy_address: $policy,
      messages: [{
        "@type": "/sparkdream.name.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }],
      metadata: "Reset Name Operational Params - Restoring original values after test"
    }' > "$PROPOSAL_DIR/reset_name_op_params.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/reset_name_op_params.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

    PROPOSAL_ID=$(get_commons_proposal_id $TX_HASH)

    if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
        echo "  FAIL: Could not submit reset proposal"
    else
        vote_and_execute $PROPOSAL_ID
        if [ $? -eq 0 ]; then
            # Verify reset
            RESET_PARAMS=$($BINARY query name params --output json)
            RESET_TIMEOUT=$(echo $RESET_PARAMS | jq -r '.params.dispute_timeout_blocks')

            if [ "$RESET_TIMEOUT" == "$INITIAL_DISPUTE_TIMEOUT" ]; then
                RESET_PARAMS_RESULT="PASS"
                echo "  PASS: Params reset to original values"
            else
                echo "  FAIL: Params did not reset correctly (got $RESET_TIMEOUT, expected $INITIAL_DISPUTE_TIMEOUT)"
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
echo "  NAME OPERATIONAL PARAMS TEST RESULTS"
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
