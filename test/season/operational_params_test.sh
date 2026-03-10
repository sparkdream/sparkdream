#!/bin/bash

echo "--- TESTING: SEASON OPERATIONAL PARAMS UPDATE (COUNCIL-GATED) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Season uses Commons Council for authorization
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

# Helper: Fix LegacyDec fields in operational params JSON.
# The query output shows LegacyDec values as their internal integer representation
# (e.g., "1000000000000000000" for 1.0). When sent back in a message, the handler
# parses these as decimal values, causing double-encoding.
# This function converts the 3 LegacyDec operational params fields back to proper
# decimal format by dividing by 10^18.
fix_legacy_dec_fields() {
    local json_input="$1"
    echo "$json_input" | python3 -c "
import json, sys
d = json.load(sys.stdin)
DEC_FIELDS = ['retro_reward_budget_per_season', 'retro_reward_min_conviction', 'nomination_min_stake']
for f in DEC_FIELDS:
    if f in d and d[f] is not None:
        s = str(d[f])
        # Remove any existing decimal point for uniform handling
        s = s.replace('.', '')
        # Pad to at least 19 chars (1 integer digit + 18 decimal)
        if len(s) <= 18:
            s = s.zfill(19)
        # Insert decimal point 18 positions from the right
        int_part = s[:-18]
        dec_part = s[-18:]
        # Strip leading zeros from integer part (but keep at least '0')
        int_part = int_part.lstrip('0') or '0'
        d[f] = int_part + '.' + dec_part
json.dump(d, sys.stdout)
"
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
            # Check if the tx itself failed (non-zero code)
            local tx_code=$(echo $TX_RES | jq -r '.code // 0')
            if [ "$tx_code" != "0" ]; then
                echo "TX failed with code $tx_code: $(echo $TX_RES | jq -r '.raw_log' | head -c 200)" >&2
                return 1
            fi
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
if echo "$CURRENT_MSGS" | jq -e 'index("/sparkdream.season.v1.MsgUpdateOperationalParams")' > /dev/null 2>&1; then
    echo "  Permission already exists, skipping governance setup"
    GOV_SETUP_RESULT="PASS"
else
    # Build the new allowed messages list (current + new)
    NEW_MSGS=$(echo "$CURRENT_MSGS" | jq '. + ["/sparkdream.season.v1.MsgUpdateOperationalParams"]')

    GOV_PROPOSAL_FILE="$PROPOSAL_DIR/add_season_op_params_perm.json"
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
      title: "Add season operational params permission to Commons Council",
      summary: "Enable Commons Council to update season operational params",
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

    if echo "$PERMS" | jq -e '.policy_permissions.allowed_messages | index("/sparkdream.season.v1.MsgUpdateOperationalParams")' > /dev/null 2>&1; then
        GOV_SETUP_RESULT="PASS"
        echo "  PASS: Permission added successfully"
    else
        echo "  FATAL: Permission not found after governance proposal passed"
        exit 1
    fi
fi
echo ""

# --- 1. QUERY INITIAL PARAMETERS ---
echo "--- TEST 1: QUERY INITIAL SEASON PARAMETERS ---"

PARAMS_JSON=$($BINARY query season params --output json)

# Operational fields we'll test
INITIAL_XP_VOTE=$(echo $PARAMS_JSON | jq -r '.params.xp_vote_cast')
INITIAL_MIN_GUILD=$(echo $PARAMS_JSON | jq -r '.params.min_guild_members')
INITIAL_MAX_QUEST_OBJ=$(echo $PARAMS_JSON | jq -r '.params.max_quest_objectives')

# Governance-only fields (should NOT change)
INITIAL_MAX_GUILD_MEMBERS=$(echo $PARAMS_JSON | jq -r '.params.max_guild_members')
INITIAL_BASELINE_REP=$(echo $PARAMS_JSON | jq -r '.params.baseline_reputation')
INITIAL_SNAPSHOT_RETENTION=$(echo $PARAMS_JSON | jq -r '.params.snapshot_retention_seasons')

echo "Operational params (subset):"
echo "  xp_vote_cast:       $INITIAL_XP_VOTE"
echo "  min_guild_members:  $INITIAL_MIN_GUILD"
echo "  max_quest_objectives: $INITIAL_MAX_QUEST_OBJ"
echo "Governance-only params:"
echo "  max_guild_members:        $INITIAL_MAX_GUILD_MEMBERS"
echo "  baseline_reputation:      $INITIAL_BASELINE_REP"
echo "  snapshot_retention_seasons: $INITIAL_SNAPSHOT_RETENTION"

if [ -z "$INITIAL_XP_VOTE" ] || [ "$INITIAL_XP_VOTE" == "null" ]; then
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
      epoch_blocks,
      season_duration_epochs,
      season_transition_epochs,
      xp_vote_cast,
      xp_proposal_created,
      xp_forum_reply_received,
      xp_forum_marked_helpful,
      xp_invitee_first_initiative,
      xp_invitee_established,
      max_vote_xp_per_epoch,
      max_forum_xp_per_epoch,
      max_xp_per_epoch,
      min_guild_members,
      max_guild_officers,
      guild_creation_cost,
      guild_hop_cooldown_epochs,
      max_guilds_per_season,
      min_guild_age_epochs,
      max_pending_invites,
      display_name_min_length,
      display_name_max_length,
      display_name_change_cooldown_epochs,
      transition_batch_size,
      max_season_extensions,
      max_extension_epochs,
      guild_description_max_length,
      guild_invite_ttl_epochs,
      max_quest_objectives,
      forum_xp_min_account_age_epochs,
      forum_xp_reciprocal_cooldown_epochs,
      forum_xp_self_reply_cooldown_epochs,
      transition_grace_period,
      max_quest_xp_reward,
      username_min_length,
      username_max_length,
      username_change_cooldown_epochs,
      username_cost_dream,
      max_active_quests_per_member,
      display_name_report_stake_dream,
      max_displayable_titles,
      invite_cleanup_interval_blocks,
      invite_cleanup_batch_size,
      max_objective_description_length,
      display_name_appeal_stake_dream,
      display_name_appeal_period_blocks,
      max_archived_titles,
      nomination_window_epochs,
      max_nominations_per_member,
      retro_reward_max_recipients,
      retro_reward_budget_per_season,
      retro_reward_min_conviction,
      nomination_conviction_half_life_epochs,
      nomination_rationale_max_length,
      nomination_min_trust_level,
      nomination_stake_min_trust_level,
      nomination_min_stake
    }')

    # Fix LegacyDec fields: query output uses internal integer format (e.g., 10^18 for 1.0)
    # which would be double-encoded if sent back as-is in a message
    OP_PARAMS=$(fix_legacy_dec_fields "$OP_PARAMS")

    # Modify test fields
    NEW_XP_VOTE="10"
    NEW_MIN_GUILD="5"
    NEW_MAX_QUEST_OBJ="10"

    OP_PARAMS=$(echo "$OP_PARAMS" | jq '
      .xp_vote_cast = "'$NEW_XP_VOTE'" |
      .min_guild_members = '$NEW_MIN_GUILD' |
      .max_quest_objectives = '$NEW_MAX_QUEST_OBJ'
    ')

    # Build the proposal JSON
    jq -n \
      --arg policy "$COUNCIL_POLICY" \
      --arg alice "$ALICE_ADDR" \
      --argjson op_params "$OP_PARAMS" \
    '{
      policy_address: $policy,
      metadata: "Adjust XP rewards and guild limits via Operations Committee",
      messages: [{
        "@type": "/sparkdream.season.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }]
    }' > "$PROPOSAL_DIR/update_season_op_params.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_season_op_params.json" \
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
    UPDATED_PARAMS=$($BINARY query season params --output json)
    UPDATED_XP_VOTE=$(echo $UPDATED_PARAMS | jq -r '.params.xp_vote_cast')
    UPDATED_MIN_GUILD=$(echo $UPDATED_PARAMS | jq -r '.params.min_guild_members')
    UPDATED_MAX_QUEST_OBJ=$(echo $UPDATED_PARAMS | jq -r '.params.max_quest_objectives')

    echo "  xp_vote_cast:         $UPDATED_XP_VOTE (expected: $NEW_XP_VOTE)"
    echo "  min_guild_members:    $UPDATED_MIN_GUILD (expected: $NEW_MIN_GUILD)"
    echo "  max_quest_objectives: $UPDATED_MAX_QUEST_OBJ (expected: $NEW_MAX_QUEST_OBJ)"

    VERIFY_OP_OK=true
    if [ "$UPDATED_XP_VOTE" != "$NEW_XP_VOTE" ]; then
        echo "  xp_vote_cast mismatch (got $UPDATED_XP_VOTE)"
        VERIFY_OP_OK=false
    fi
    if [ "$UPDATED_MIN_GUILD" != "$NEW_MIN_GUILD" ]; then
        echo "  min_guild_members mismatch (got $UPDATED_MIN_GUILD)"
        VERIFY_OP_OK=false
    fi
    if [ "$UPDATED_MAX_QUEST_OBJ" != "$NEW_MAX_QUEST_OBJ" ]; then
        echo "  max_quest_objectives mismatch (got $UPDATED_MAX_QUEST_OBJ)"
        VERIFY_OP_OK=false
    fi

    if [ "$VERIFY_OP_OK" == true ]; then
        VERIFY_OPERATIONAL_RESULT="PASS"
        echo "  PASS: Operational params updated correctly"
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
    CURRENT_MAX_GUILD_MEMBERS=$(echo $UPDATED_PARAMS | jq -r '.params.max_guild_members')
    CURRENT_BASELINE_REP=$(echo $UPDATED_PARAMS | jq -r '.params.baseline_reputation')
    CURRENT_SNAPSHOT_RETENTION=$(echo $UPDATED_PARAMS | jq -r '.params.snapshot_retention_seasons')

    echo "  max_guild_members:         $CURRENT_MAX_GUILD_MEMBERS (expected: $INITIAL_MAX_GUILD_MEMBERS)"
    echo "  baseline_reputation:       $CURRENT_BASELINE_REP (expected: $INITIAL_BASELINE_REP)"
    echo "  snapshot_retention_seasons: $CURRENT_SNAPSHOT_RETENTION (expected: $INITIAL_SNAPSHOT_RETENTION)"

    VERIFY_GOV_OK=true
    if [ "$CURRENT_MAX_GUILD_MEMBERS" != "$INITIAL_MAX_GUILD_MEMBERS" ]; then
        echo "  max_guild_members was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_BASELINE_REP" != "$INITIAL_BASELINE_REP" ]; then
        echo "  baseline_reputation was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_SNAPSHOT_RETENTION" != "$INITIAL_SNAPSHOT_RETENTION" ]; then
        echo "  snapshot_retention_seasons was modified by operational update!"
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
      epoch_blocks,
      season_duration_epochs,
      season_transition_epochs,
      xp_vote_cast,
      xp_proposal_created,
      xp_forum_reply_received,
      xp_forum_marked_helpful,
      xp_invitee_first_initiative,
      xp_invitee_established,
      max_vote_xp_per_epoch,
      max_forum_xp_per_epoch,
      max_xp_per_epoch,
      min_guild_members,
      max_guild_officers,
      guild_creation_cost,
      guild_hop_cooldown_epochs,
      max_guilds_per_season,
      min_guild_age_epochs,
      max_pending_invites,
      display_name_min_length,
      display_name_max_length,
      display_name_change_cooldown_epochs,
      transition_batch_size,
      max_season_extensions,
      max_extension_epochs,
      guild_description_max_length,
      guild_invite_ttl_epochs,
      max_quest_objectives,
      forum_xp_min_account_age_epochs,
      forum_xp_reciprocal_cooldown_epochs,
      forum_xp_self_reply_cooldown_epochs,
      transition_grace_period,
      max_quest_xp_reward,
      username_min_length,
      username_max_length,
      username_change_cooldown_epochs,
      username_cost_dream,
      max_active_quests_per_member,
      display_name_report_stake_dream,
      max_displayable_titles,
      invite_cleanup_interval_blocks,
      invite_cleanup_batch_size,
      max_objective_description_length,
      display_name_appeal_stake_dream,
      display_name_appeal_period_blocks,
      max_archived_titles,
      nomination_window_epochs,
      max_nominations_per_member,
      retro_reward_max_recipients,
      retro_reward_budget_per_season,
      retro_reward_min_conviction,
      nomination_conviction_half_life_epochs,
      nomination_rationale_max_length,
      nomination_min_trust_level,
      nomination_stake_min_trust_level,
      nomination_min_stake
    }')

    # Fix LegacyDec fields before sending back
    RESET_OP_PARAMS=$(fix_legacy_dec_fields "$RESET_OP_PARAMS")

    jq -n \
      --arg policy "$COUNCIL_POLICY" \
      --arg alice "$ALICE_ADDR" \
      --argjson op_params "$RESET_OP_PARAMS" \
    '{
      policy_address: $policy,
      metadata: "Restoring original values after test",
      messages: [{
        "@type": "/sparkdream.season.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }]
    }' > "$PROPOSAL_DIR/reset_season_op_params.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/reset_season_op_params.json" \
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
            RESET_PARAMS=$($BINARY query season params --output json)
            RESET_XP_VOTE=$(echo $RESET_PARAMS | jq -r '.params.xp_vote_cast')

            if [ "$RESET_XP_VOTE" == "$INITIAL_XP_VOTE" ]; then
                RESET_PARAMS_RESULT="PASS"
                echo "  PASS: Params reset to original values"
            else
                echo "  FAIL: Params did not reset correctly (got $RESET_XP_VOTE, expected $INITIAL_XP_VOTE)"
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
echo "  SEASON OPERATIONAL PARAMS TEST RESULTS"
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
