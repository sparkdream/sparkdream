#!/bin/bash

echo "--- TESTING: FORUM OPERATIONAL PARAMS UPDATE (COUNCIL-GATED) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Forum uses Commons Council for authorization
COUNCIL_NAME="Commons Council"
echo "Looking up '$COUNCIL_NAME'..."
COUNCIL_INFO=$($BINARY query commons get-group "$COUNCIL_NAME" --output json)
COUNCIL_POLICY=$(echo $COUNCIL_INFO | jq -r '.group.policy_address')

if [ -z "$COUNCIL_POLICY" ] || [ "$COUNCIL_POLICY" == "null" ]; then
    echo "SETUP ERROR: '$COUNCIL_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

# Governance module address (for MsgUpdatePolicyPermissions)
GOV_MODULE_ADDR=$($BINARY query auth module-account gov --output json 2>/dev/null | jq -r '.account.base_account.address // empty')
if [ -z "$GOV_MODULE_ADDR" ]; then
    GOV_MODULE_ADDR="sprkdrm10d07y265gmmuvt4z0w9aw880jnsr700j865qcw"
fi

echo "Alice Address:    $ALICE_ADDR"
echo "Bob Address:      $BOB_ADDR"
echo "Council Policy:   $COUNCIL_POLICY"
echo "Gov Module:       $GOV_MODULE_ADDR"
echo ""

# --- Result Tracking ---
GOV_SETUP_RESULT="FAIL"
QUERY_PARAMS_RESULT="FAIL"
UPDATE_PARAMS_RESULT="FAIL"
VERIFY_OPERATIONAL_RESULT="FAIL"
VERIFY_GOVERNANCE_RESULT="FAIL"
RESET_PARAMS_RESULT="FAIL"

# Helper: wait for tx to be indexed
wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0

    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
    return 1
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
        return 1
    fi
    return 0
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

# Helper: extract commons proposal ID from tx hash
get_group_proposal_id() {
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

# Helper: vote + execute a commons proposal (council requires 2 of 3 votes)
vote_and_execute() {
    local prop_id=$1

    echo "  Alice voting YES..."
    $BINARY tx commons vote-proposal $prop_id yes \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 5

    echo "  Bob voting YES..."
    $BINARY tx commons vote-proposal $prop_id yes \
        --from bob -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 5

    echo "  Executing proposal $prop_id..."
    EXEC_RES=$($BINARY tx commons execute-proposal $prop_id \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --gas 2000000 --fees 5000000uspark --output json)
    EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')
    sleep 6

    EXEC_TX_JSON=$(wait_for_tx $EXEC_TX_HASH)
    # Check proposal status
    PROP_STATUS=$($BINARY query commons get-proposal $prop_id --output json 2>/dev/null | jq -r '.proposal.status // empty')
    if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
        echo "  Proposal executed successfully"
        return 0
    elif check_tx_success "$EXEC_TX_JSON" 2>/dev/null; then
        echo "  Execution tx succeeded (status: $PROP_STATUS)"
        return 0
    else
        echo "  Execution failed (status: $PROP_STATUS)"
        echo "  Raw: $(echo $EXEC_TX_JSON | jq -r '.raw_log' 2>/dev/null)"
        return 1
    fi
}

# ========================================================================
# PART 0: GOVERNANCE SETUP
# Add MsgUpdateOperationalParams to Commons Council's AllowedMessages
# ========================================================================
echo "--- TEST 0: GOVERNANCE SETUP (ADD PERMISSION) ---"
echo "Adding MsgUpdateOperationalParams permission to Commons Council via governance..."

# Get current allowed messages and append the new one
CURRENT_PERMS=$($BINARY query commons get-policy-permissions "$COUNCIL_POLICY" --output json 2>&1)
CURRENT_MSGS=$(echo "$CURRENT_PERMS" | jq -c '.policy_permissions.allowed_messages // []')

# Check if permission already exists
if echo "$CURRENT_MSGS" | jq -e 'index("/sparkdream.forum.v1.MsgUpdateOperationalParams")' > /dev/null 2>&1; then
    echo "  Permission already exists, skipping governance setup"
    GOV_SETUP_RESULT="PASS"
else
    # Build the new allowed messages list (current + new)
    NEW_MSGS=$(echo "$CURRENT_MSGS" | jq '. + ["/sparkdream.forum.v1.MsgUpdateOperationalParams"]')

    GOV_PROPOSAL_FILE="$PROPOSAL_DIR/add_op_params_perm.json"
    jq -n \
      --arg policy "$COUNCIL_POLICY" \
      --arg gov "$GOV_MODULE_ADDR" \
      --argjson msgs "$NEW_MSGS" \
    '{
      messages: [{
        "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
        authority: $gov,
        policy_address: $policy,
        allowed_messages: $msgs
      }],
      metadata: "",
      deposit: "50000000uspark",
      title: "Add forum operational params permission to Commons Council",
      summary: "Enable Commons Council to update forum operational params",
      expedited: false
    }' > "$GOV_PROPOSAL_FILE"

    echo "  Submitting governance proposal..."
    TX_RES=$($BINARY tx gov submit-proposal \
        "$GOV_PROPOSAL_FILE" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --gas 500000 \
        --fees 10000uspark \
        -y \
        --output json 2>&1)

    GOV_TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$GOV_TXHASH" ] || [ "$GOV_TXHASH" == "null" ]; then
        echo "  FATAL: Failed to submit governance proposal"
        echo "  $TX_RES"
        exit 1
    fi

    sleep 6
    GOV_TX_RESULT=$(wait_for_tx $GOV_TXHASH)

    if ! check_tx_success "$GOV_TX_RESULT"; then
        echo "  FATAL: Governance proposal submission failed"
        exit 1
    fi

    # Extract proposal ID
    GOV_PROPOSAL_ID=$(extract_event_value "$GOV_TX_RESULT" "submit_proposal" "proposal_id")
    if [ -z "$GOV_PROPOSAL_ID" ]; then
        GOV_PROPOSAL_ID=$($BINARY query gov proposals --status voting_period --output json 2>&1 | jq -r '.proposals[-1].id // empty')
    fi
    if [ -z "$GOV_PROPOSAL_ID" ]; then
        GOV_PROPOSAL_ID=$($BINARY query gov proposals --output json 2>&1 | jq -r '.proposals[-1].id // empty')
    fi

    if [ -z "$GOV_PROPOSAL_ID" ]; then
        echo "  FATAL: Could not determine governance proposal ID"
        exit 1
    fi

    echo "  Governance proposal #$GOV_PROPOSAL_ID submitted"

    # Alice votes YES (she controls ~75% of bonded stake)
    echo "  Alice voting YES..."
    TX_RES=$($BINARY tx gov vote \
        "$GOV_PROPOSAL_ID" \
        yes \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --gas 300000 \
        --fees 10000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        wait_for_tx $TXHASH > /dev/null 2>&1
    fi

    # Wait for voting period (60s in genesis config)
    echo "  Waiting for voting period (65s)..."
    sleep 65

    # Verify proposal passed
    PROPOSAL_STATUS=$($BINARY query gov proposal "$GOV_PROPOSAL_ID" --output json 2>&1 | jq -r '.proposal.status // .status // "unknown"')
    echo "  Proposal status: $PROPOSAL_STATUS"

    if [ "$PROPOSAL_STATUS" != "PROPOSAL_STATUS_PASSED" ] && [ "$PROPOSAL_STATUS" != "3" ]; then
        echo "  FATAL: Governance proposal did not pass (status=$PROPOSAL_STATUS)"
        exit 1
    fi

    # Verify updated permissions
    echo "  Verifying permissions..."
    PERMS=$($BINARY query commons get-policy-permissions "$COUNCIL_POLICY" --output json 2>&1)
    echo "  Permissions: $(echo "$PERMS" | jq -c '.policy_permissions.allowed_messages' 2>/dev/null)"

    if echo "$PERMS" | jq -e '.policy_permissions.allowed_messages | index("/sparkdream.forum.v1.MsgUpdateOperationalParams")' > /dev/null 2>&1; then
        GOV_SETUP_RESULT="PASS"
        echo "  PASS: Permission added successfully"
    else
        echo "  FATAL: Permission not found after governance proposal passed"
        exit 1
    fi
fi
echo ""

# --- 1. QUERY INITIAL PARAMETERS ---
echo "--- TEST 1: QUERY INITIAL FORUM PARAMETERS ---"

PARAMS_JSON=$($BINARY query forum params --output json)

# Operational fields we'll test
INITIAL_EPHEMERAL_TTL=$(echo $PARAMS_JSON | jq -r '.params.ephemeral_ttl')
INITIAL_SPAM_TAX=$(echo $PARAMS_JSON | jq -r '.params.spam_tax.amount')
INITIAL_DAILY_POST_LIMIT=$(echo $PARAMS_JSON | jq -r '.params.daily_post_limit')

# Governance-only fields (should NOT change)
INITIAL_FORUM_PAUSED=$(echo $PARAMS_JSON | jq -r '.params.forum_paused')
INITIAL_MODERATION_PAUSED=$(echo $PARAMS_JSON | jq -r '.params.moderation_paused')
INITIAL_APPEALS_PAUSED=$(echo $PARAMS_JSON | jq -r '.params.appeals_paused')

echo "Operational params (subset):"
echo "  ephemeral_ttl:     $INITIAL_EPHEMERAL_TTL"
echo "  spam_tax:          $INITIAL_SPAM_TAX uspark"
echo "  daily_post_limit:  $INITIAL_DAILY_POST_LIMIT"
echo "Governance-only params:"
echo "  forum_paused:      $INITIAL_FORUM_PAUSED"
echo "  moderation_paused: $INITIAL_MODERATION_PAUSED"
echo "  appeals_paused:    $INITIAL_APPEALS_PAUSED"

if [ -z "$INITIAL_EPHEMERAL_TTL" ] || [ "$INITIAL_EPHEMERAL_TTL" == "null" ]; then
    echo "  FAIL: Could not query initial parameters"
else
    QUERY_PARAMS_RESULT="PASS"
    echo "  PASS: Initial parameters queried successfully"
fi
echo ""

# --- 2. BUILD AND SUBMIT OPERATIONAL PARAMS UPDATE ---
echo "--- TEST 2: UPDATE OPERATIONAL PARAMS VIA COUNCIL PROPOSAL ---"

if [ "$QUERY_PARAMS_RESULT" == "PASS" ]; then
    # Extract all operational fields from current params using jq.
    # Proto3 JSON omits default-valued fields (false bools, zero ints), so we
    # must provide explicit defaults to avoid null values in the proposal JSON.
    OP_PARAMS=$(echo "$PARAMS_JSON" | jq '.params | {
      bounties_enabled: (.bounties_enabled // false),
      reactions_enabled: (.reactions_enabled // false),
      editing_enabled: (.editing_enabled // false),
      spam_tax,
      reaction_spam_tax,
      flag_spam_tax,
      downvote_deposit,
      appeal_fee,
      lock_appeal_fee,
      move_appeal_fee,
      edit_fee,
      cost_per_byte,
      cost_per_byte_exempt: (.cost_per_byte_exempt // false),
      max_content_size,
      daily_post_limit,
      max_reply_depth,
      max_follows_per_day,
      bounty_cancellation_fee_percent,
      edit_grace_period,
      edit_max_window,
      archive_threshold,
      unarchive_cooldown,
      archive_cooldown,
      hide_appeal_cooldown,
      lock_appeal_cooldown,
      move_appeal_cooldown,
      ephemeral_ttl,
      anonymous_posting_enabled: (.anonymous_posting_enabled // false),
      anonymous_min_trust_level,
      private_reactions_enabled: (.private_reactions_enabled // false),
      conviction_renewal_threshold,
      conviction_renewal_period
    }')

    # Modify test fields: double the ephemeral TTL and spam tax
    NEW_EPHEMERAL_TTL="172800"
    NEW_SPAM_TAX_AMOUNT="2000000"
    NEW_DAILY_POST_LIMIT="100"

    OP_PARAMS=$(echo "$OP_PARAMS" | jq '
      .ephemeral_ttl = "'$NEW_EPHEMERAL_TTL'" |
      .spam_tax.amount = "'$NEW_SPAM_TAX_AMOUNT'" |
      .daily_post_limit = "'$NEW_DAILY_POST_LIMIT'"
    ')

    # Build the proposal JSON
    jq -n \
      --arg policy "$COUNCIL_POLICY" \
      --arg alice "$ALICE_ADDR" \
      --argjson op_params "$OP_PARAMS" \
    '{
      policy_address: $policy,
      metadata: "Adjust TTL and spam tax via Council",
      messages: [{
        "@type": "/sparkdream.forum.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }]
    }' > "$PROPOSAL_DIR/update_forum_op_params.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_forum_op_params.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

    echo "Submitted tx: $TX_HASH"
    PROPOSAL_ID=$(get_group_proposal_id $TX_HASH)

    if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
        echo "  FAIL: Could not submit operational params proposal"
        # Show raw error for debugging
        sleep 3
        TX_DETAIL=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
        if [ -n "$TX_DETAIL" ]; then
            echo "  TX code: $(echo $TX_DETAIL | jq -r '.code')"
            echo "  TX log:  $(echo $TX_DETAIL | jq -r '.raw_log' | head -c 200)"
        fi
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
    UPDATED_PARAMS=$($BINARY query forum params --output json)
    UPDATED_EPHEMERAL_TTL=$(echo $UPDATED_PARAMS | jq -r '.params.ephemeral_ttl')
    UPDATED_SPAM_TAX=$(echo $UPDATED_PARAMS | jq -r '.params.spam_tax.amount')
    UPDATED_DAILY_POST_LIMIT=$(echo $UPDATED_PARAMS | jq -r '.params.daily_post_limit')

    echo "  ephemeral_ttl:    $UPDATED_EPHEMERAL_TTL (expected: $NEW_EPHEMERAL_TTL)"
    echo "  spam_tax:         $UPDATED_SPAM_TAX (expected: $NEW_SPAM_TAX_AMOUNT)"
    echo "  daily_post_limit: $UPDATED_DAILY_POST_LIMIT (expected: $NEW_DAILY_POST_LIMIT)"

    VERIFY_OP_OK=true
    if [ "$UPDATED_EPHEMERAL_TTL" != "$NEW_EPHEMERAL_TTL" ]; then
        echo "  ephemeral_ttl mismatch (got $UPDATED_EPHEMERAL_TTL)"
        VERIFY_OP_OK=false
    fi
    if [ "$UPDATED_SPAM_TAX" != "$NEW_SPAM_TAX_AMOUNT" ]; then
        echo "  spam_tax mismatch (got $UPDATED_SPAM_TAX)"
        VERIFY_OP_OK=false
    fi
    if [ "$UPDATED_DAILY_POST_LIMIT" != "$NEW_DAILY_POST_LIMIT" ]; then
        echo "  daily_post_limit mismatch (got $UPDATED_DAILY_POST_LIMIT)"
        VERIFY_OP_OK=false
    fi

    if [ "$VERIFY_OP_OK" == true ]; then
        VERIFY_OPERATIONAL_RESULT="PASS"
        echo "  PASS: All tested operational params updated correctly"
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
    CURRENT_FORUM_PAUSED=$(echo $UPDATED_PARAMS | jq -r '.params.forum_paused')
    CURRENT_MOD_PAUSED=$(echo $UPDATED_PARAMS | jq -r '.params.moderation_paused')
    CURRENT_APPEALS_PAUSED=$(echo $UPDATED_PARAMS | jq -r '.params.appeals_paused')

    echo "  forum_paused:      $CURRENT_FORUM_PAUSED (expected: $INITIAL_FORUM_PAUSED)"
    echo "  moderation_paused: $CURRENT_MOD_PAUSED (expected: $INITIAL_MODERATION_PAUSED)"
    echo "  appeals_paused:    $CURRENT_APPEALS_PAUSED (expected: $INITIAL_APPEALS_PAUSED)"

    VERIFY_GOV_OK=true
    if [ "$CURRENT_FORUM_PAUSED" != "$INITIAL_FORUM_PAUSED" ]; then
        echo "  forum_paused was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_MOD_PAUSED" != "$INITIAL_MODERATION_PAUSED" ]; then
        echo "  moderation_paused was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_APPEALS_PAUSED" != "$INITIAL_APPEALS_PAUSED" ]; then
        echo "  appeals_paused was modified by operational update!"
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
    # Re-extract from initial params for reset (same null-safe extraction)
    RESET_OP_PARAMS=$(echo "$PARAMS_JSON" | jq '.params | {
      bounties_enabled: (.bounties_enabled // false),
      reactions_enabled: (.reactions_enabled // false),
      editing_enabled: (.editing_enabled // false),
      spam_tax,
      reaction_spam_tax,
      flag_spam_tax,
      downvote_deposit,
      appeal_fee,
      lock_appeal_fee,
      move_appeal_fee,
      edit_fee,
      cost_per_byte,
      cost_per_byte_exempt: (.cost_per_byte_exempt // false),
      max_content_size,
      daily_post_limit,
      max_reply_depth,
      max_follows_per_day,
      bounty_cancellation_fee_percent,
      edit_grace_period,
      edit_max_window,
      archive_threshold,
      unarchive_cooldown,
      archive_cooldown,
      hide_appeal_cooldown,
      lock_appeal_cooldown,
      move_appeal_cooldown,
      ephemeral_ttl,
      anonymous_posting_enabled: (.anonymous_posting_enabled // false),
      anonymous_min_trust_level,
      private_reactions_enabled: (.private_reactions_enabled // false),
      conviction_renewal_threshold,
      conviction_renewal_period
    }')

    jq -n \
      --arg policy "$COUNCIL_POLICY" \
      --arg alice "$ALICE_ADDR" \
      --argjson op_params "$RESET_OP_PARAMS" \
    '{
      policy_address: $policy,
      metadata: "Restoring original values after test",
      messages: [{
        "@type": "/sparkdream.forum.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }]
    }' > "$PROPOSAL_DIR/reset_forum_op_params.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/reset_forum_op_params.json" \
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
            RESET_PARAMS=$($BINARY query forum params --output json)
            RESET_TTL=$(echo $RESET_PARAMS | jq -r '.params.ephemeral_ttl')

            if [ "$RESET_TTL" == "$INITIAL_EPHEMERAL_TTL" ]; then
                RESET_PARAMS_RESULT="PASS"
                echo "  PASS: Params reset to original values"
            else
                echo "  FAIL: Params did not reset correctly (got $RESET_TTL, expected $INITIAL_EPHEMERAL_TTL)"
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
echo "  FORUM OPERATIONAL PARAMS TEST RESULTS"
echo "============================================================================"
echo ""

TOTAL_COUNT=0
PASS_COUNT=0
FAIL_COUNT=0

for RESULT in "$GOV_SETUP_RESULT" "$QUERY_PARAMS_RESULT" "$UPDATE_PARAMS_RESULT" "$VERIFY_OPERATIONAL_RESULT" "$VERIFY_GOVERNANCE_RESULT" "$RESET_PARAMS_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

echo "  0. Governance Setup (Add Perm):   $GOV_SETUP_RESULT"
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
