#!/bin/bash

echo "--- TESTING: FORUM OPERATIONAL PARAMS UPDATE (COMMITTEE-GATED) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Operational params are gated by the Commons Operations Committee
COMMITTEE_NAME="Commons Operations Committee"
echo "Looking up '$COMMITTEE_NAME'..."
COMMITTEE_INFO=$($BINARY query commons get-group "$COMMITTEE_NAME" --output json)
COMMITTEE_POLICY=$(echo $COMMITTEE_INFO | jq -r '.group.policy_address')

if [ -z "$COMMITTEE_POLICY" ] || [ "$COMMITTEE_POLICY" == "null" ]; then
    echo "SETUP ERROR: '$COMMITTEE_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

echo "Alice Address:      $ALICE_ADDR"
echo "Bob Address:        $BOB_ADDR"
echo "Committee Policy:   $COMMITTEE_POLICY"
echo ""

# --- Result Tracking ---
QUERY_PARAMS_RESULT="FAIL"
UPDATE_PARAMS_RESULT="FAIL"
VERIFY_OPERATIONAL_RESULT="FAIL"
VERIFY_GOVERNANCE_RESULT="FAIL"
RESET_PARAMS_RESULT="FAIL"

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

# Helper: vote + execute a Commons Operations Committee proposal
# Threshold=1, so a single vote from any member suffices.
vote_and_execute() {
    local prop_id=$1

    echo "  Alice voting YES..."
    $BINARY tx commons vote-proposal $prop_id yes \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 6

    echo "  Executing proposal $prop_id..."
    EXEC_RES=$($BINARY tx commons execute-proposal $prop_id \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --gas 2000000 --fees 5000000uspark --output json)
    EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')
    sleep 6

    # Check proposal status
    PROP_STATUS=$($BINARY query commons get-proposal $prop_id --output json 2>/dev/null | jq -r '.proposal.status // empty')
    if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
        echo "  Proposal executed successfully"
        return 0
    else
        EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json 2>/dev/null)
        echo "  Execution failed (status: $PROP_STATUS)"
        echo "  Raw: $(echo $EXEC_TX_JSON | jq -r '.raw_log' 2>/dev/null)"
        return 1
    fi
}

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
      --arg policy "$COMMITTEE_POLICY" \
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
      conviction_renewal_threshold,
      conviction_renewal_period
    }')

    jq -n \
      --arg policy "$COMMITTEE_POLICY" \
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
