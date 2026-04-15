#!/bin/bash

echo "--- TESTING: REP OPERATIONAL PARAMS UPDATE (COMMITTEE-GATED) ---"

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
            # Check if tx failed
            local code=$(echo $TX_RES | jq -r '.code')
            if [ "$code" != "0" ]; then
                echo "TX failed with code $code: $(echo $TX_RES | jq -r '.raw_log' | head -c 200)" >&2
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

# --- Helper: Convert sdk.Dec fields from raw integer to decimal string ---
# The CLI query outputs LegacyDec as raw 18-precision integers (e.g. "100000000000000000" for 0.1).
# But proto JSON unmarshaling via group proposals expects decimal strings (e.g. "0.1").
# This helper converts the raw format to decimal for use in proposal JSON.
convert_op_params_for_proposal() {
    local params_json="$1"
    python3 -c "
import json, sys

# Fields that use cosmossdk.io/math.LegacyDec (18 decimal precision)
DEC_FIELDS = [
    'unstaked_decay_rate', 'transfer_tax_rate',
    'min_reputation_multiplier', 'referral_reward_rate',
    'invitation_cost_multiplier',
    'challenger_reward_rate', 'jury_super_majority',
    'min_juror_reputation', 'solo_expert_bonus_rate',
    'project_completion_bonus_rate',
    'member_stake_revenue_share', 'tag_stake_revenue_share',
    'content_challenge_reward_share', 'conviction_propagation_ratio',
    'reputation_decay_rate', 'max_conviction_share_per_member',
    'invitation_stake_burn_rate', 'max_reputation_gain_per_epoch',
    'staked_decay_rate'
]

PRECISION = 18

params = json.loads(sys.argv[1])
for field in DEC_FIELDS:
    if field in params and params[field]:
        raw = str(params[field])
        # If already in decimal format (contains '.'), pass through as-is
        # (newer Cosmos SDK versions return LegacyDec as '0.500000000000000000')
        if '.' in raw:
            # Strip trailing zeros but keep at least one decimal digit
            dec_str = raw.rstrip('0').rstrip('.')
            params[field] = dec_str
        else:
            # Raw 18-precision integer format (e.g. '100000000000000000' for 0.1)
            padded = raw.zfill(PRECISION + 1)
            int_part = padded[:len(padded) - PRECISION]
            dec_part = padded[len(padded) - PRECISION:]
            dec_str = (int_part + '.' + dec_part).rstrip('0').rstrip('.')
            params[field] = dec_str

print(json.dumps(params))
" "$params_json"
}

# --- 1. QUERY INITIAL PARAMETERS ---
echo "--- TEST 1: QUERY INITIAL REP PARAMETERS ---"

PARAMS_JSON=$($BINARY query rep params --output json)

# Operational fields we'll test
INITIAL_MAX_TIPS=$(echo $PARAMS_JSON | jq -r '.params.max_tips_per_epoch')
INITIAL_JURY_SIZE=$(echo $PARAMS_JSON | jq -r '.params.jury_size')
INITIAL_EPOCH_BLOCKS=$(echo $PARAMS_JSON | jq -r '.params.epoch_blocks')

# Governance-only fields (should NOT change)
INITIAL_COMPLETER_SHARE=$(echo $PARAMS_JSON | jq -r '.params.completer_share')
INITIAL_TREASURY_SHARE=$(echo $PARAMS_JSON | jq -r '.params.treasury_share')
INITIAL_MINOR_SLASH=$(echo $PARAMS_JSON | jq -r '.params.minor_slash_penalty')

echo "Operational params (subset):"
echo "  max_tips_per_epoch: $INITIAL_MAX_TIPS"
echo "  jury_size:          $INITIAL_JURY_SIZE"
echo "  epoch_blocks:       $INITIAL_EPOCH_BLOCKS"
echo "Governance-only params:"
echo "  completer_share:    $INITIAL_COMPLETER_SHARE"
echo "  treasury_share:     $INITIAL_TREASURY_SHARE"
echo "  minor_slash_penalty:$INITIAL_MINOR_SLASH"

if [ -z "$INITIAL_MAX_TIPS" ] || [ "$INITIAL_MAX_TIPS" == "null" ]; then
    echo "  FAIL: Could not query initial parameters"
else
    QUERY_PARAMS_RESULT="PASS"
    echo "  PASS: Initial parameters queried successfully"
fi
echo ""

# --- 2. BUILD AND SUBMIT OPERATIONAL PARAMS UPDATE ---
echo "--- TEST 2: UPDATE OPERATIONAL PARAMS VIA COUNCIL PROPOSAL ---"

if [ "$QUERY_PARAMS_RESULT" == "PASS" ]; then
    # Extract all operational fields from current params.
    # Proto3 JSON omits default-valued fields (false bools, zero ints), so we
    # must provide explicit defaults to avoid null values in the proposal JSON.
    OP_PARAMS=$(echo "$PARAMS_JSON" | jq '.params | {
      epoch_blocks,
      season_duration_epochs,
      unstaked_decay_rate,
      transfer_tax_rate,
      max_tip_amount,
      max_tips_per_epoch,
      max_gift_amount,
      gift_only_to_invitees: (.gift_only_to_invitees // false),
      min_reputation_multiplier,
      default_review_period_epochs,
      default_challenge_period_epochs,
      min_invitation_stake,
      invitation_accountability_epochs,
      referral_reward_rate,
      invitation_cost_multiplier,
      min_challenge_stake,
      challenger_reward_rate,
      jury_size,
      jury_super_majority,
      min_juror_reputation,
      simple_complexity_budget,
      standard_complexity_budget,
      complex_complexity_budget,
      expert_complexity_budget,
      solo_expert_bonus_rate,
      interim_deadline_epochs,
      max_active_challenges_per_committee,
      max_new_challenges_per_epoch,
      challenge_queue_max_size,
      project_completion_bonus_rate,
      member_stake_revenue_share,
      tag_stake_revenue_share,
      min_stake_duration_seconds,
      allow_self_member_stake: (.allow_self_member_stake // false),
      challenge_response_deadline_epochs,
      gift_cooldown_blocks,
      max_gifts_per_sender_epoch,

      content_conviction_half_life_epochs,
      max_content_stake_per_member,
      max_author_bond_per_content,
      author_bond_slash_on_moderation: (.author_bond_slash_on_moderation // false),
      content_challenge_reward_share,
      conviction_propagation_ratio,
      reputation_decay_rate,
      max_conviction_share_per_member,
      invitation_stake_burn_rate,
      max_reputation_gain_per_epoch,
      max_tags_per_initiative,
      max_staking_rewards_per_season,
      staked_decay_rate,
      new_member_decay_grace_epochs,
      max_treasury_balance,
      treasury_funds_interims: (.treasury_funds_interims // false),
      treasury_funds_retro_pgf: (.treasury_funds_retro_pgf // false),
      max_initiative_stake_per_member,
      max_initiative_rewards_per_season,
      large_project_budget_threshold
    }')

    # Modify test fields
    NEW_MAX_TIPS="20"
    NEW_JURY_SIZE="7"

    OP_PARAMS=$(echo "$OP_PARAMS" | jq '
      .max_tips_per_epoch = '$NEW_MAX_TIPS' |
      .jury_size = '$NEW_JURY_SIZE'
    ')

    # Convert LegacyDec fields from raw 18-precision integers to decimal strings
    # (query returns "100000000000000000" for 0.1, but proposal JSON needs "0.1")
    OP_PARAMS=$(convert_op_params_for_proposal "$OP_PARAMS")

    echo "  Converted operational params for proposal (sample):"
    echo "    unstaked_decay_rate: $(echo $OP_PARAMS | jq -r '.unstaked_decay_rate')"
    echo "    max_tips_per_epoch: $(echo $OP_PARAMS | jq -r '.max_tips_per_epoch')"

    # Build the proposal JSON
    jq -n \
      --arg policy "$COMMITTEE_POLICY" \
      --arg alice "$ALICE_ADDR" \
      --argjson op_params "$OP_PARAMS" \
    '{
      policy_address: $policy,
      metadata: "Adjust tip limits and jury size via Operations Committee",
      messages: [{
        "@type": "/sparkdream.rep.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }]
    }' > "$PROPOSAL_DIR/update_rep_op_params.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_rep_op_params.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

    echo "  Submitted tx: $TX_HASH"

    # Show raw error for debugging
    sleep 3
    TX_DETAIL=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
    if [ -n "$TX_DETAIL" ]; then
        echo "  TX code: $(echo $TX_DETAIL | jq -r '.code')"
        echo "  TX log:  $(echo $TX_DETAIL | jq -r '.raw_log' | head -c 200)"
    fi

    PROPOSAL_ID=$(get_group_proposal_id $TX_HASH)

    if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
        echo "  FAIL: Could not submit operational params proposal"
    else
        echo "  Proposal ID: $PROPOSAL_ID"
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
    UPDATED_PARAMS=$($BINARY query rep params --output json)
    UPDATED_MAX_TIPS=$(echo $UPDATED_PARAMS | jq -r '.params.max_tips_per_epoch')
    UPDATED_JURY_SIZE=$(echo $UPDATED_PARAMS | jq -r '.params.jury_size')

    echo "  max_tips_per_epoch: $UPDATED_MAX_TIPS (expected: $NEW_MAX_TIPS)"
    echo "  jury_size:          $UPDATED_JURY_SIZE (expected: $NEW_JURY_SIZE)"

    VERIFY_OP_OK=true
    if [ "$UPDATED_MAX_TIPS" != "$NEW_MAX_TIPS" ]; then
        echo "  max_tips_per_epoch mismatch (got $UPDATED_MAX_TIPS)"
        VERIFY_OP_OK=false
    fi
    if [ "$UPDATED_JURY_SIZE" != "$NEW_JURY_SIZE" ]; then
        echo "  jury_size mismatch (got $UPDATED_JURY_SIZE)"
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
    CURRENT_COMPLETER=$(echo $UPDATED_PARAMS | jq -r '.params.completer_share')
    CURRENT_TREASURY=$(echo $UPDATED_PARAMS | jq -r '.params.treasury_share')
    CURRENT_MINOR_SLASH=$(echo $UPDATED_PARAMS | jq -r '.params.minor_slash_penalty')

    echo "  completer_share:     $CURRENT_COMPLETER (expected: $INITIAL_COMPLETER_SHARE)"
    echo "  treasury_share:      $CURRENT_TREASURY (expected: $INITIAL_TREASURY_SHARE)"
    echo "  minor_slash_penalty: $CURRENT_MINOR_SLASH (expected: $INITIAL_MINOR_SLASH)"

    VERIFY_GOV_OK=true
    if [ "$CURRENT_COMPLETER" != "$INITIAL_COMPLETER_SHARE" ]; then
        echo "  completer_share was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_TREASURY" != "$INITIAL_TREASURY_SHARE" ]; then
        echo "  treasury_share was modified by operational update!"
        VERIFY_GOV_OK=false
    fi
    if [ "$CURRENT_MINOR_SLASH" != "$INITIAL_MINOR_SLASH" ]; then
        echo "  minor_slash_penalty was modified by operational update!"
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
      unstaked_decay_rate,
      transfer_tax_rate,
      max_tip_amount,
      max_tips_per_epoch,
      max_gift_amount,
      gift_only_to_invitees: (.gift_only_to_invitees // false),
      min_reputation_multiplier,
      default_review_period_epochs,
      default_challenge_period_epochs,
      min_invitation_stake,
      invitation_accountability_epochs,
      referral_reward_rate,
      invitation_cost_multiplier,
      min_challenge_stake,
      challenger_reward_rate,
      jury_size,
      jury_super_majority,
      min_juror_reputation,
      simple_complexity_budget,
      standard_complexity_budget,
      complex_complexity_budget,
      expert_complexity_budget,
      solo_expert_bonus_rate,
      interim_deadline_epochs,
      max_active_challenges_per_committee,
      max_new_challenges_per_epoch,
      challenge_queue_max_size,
      project_completion_bonus_rate,
      member_stake_revenue_share,
      tag_stake_revenue_share,
      min_stake_duration_seconds,
      allow_self_member_stake: (.allow_self_member_stake // false),
      challenge_response_deadline_epochs,
      gift_cooldown_blocks,
      max_gifts_per_sender_epoch,

      content_conviction_half_life_epochs,
      max_content_stake_per_member,
      max_author_bond_per_content,
      author_bond_slash_on_moderation: (.author_bond_slash_on_moderation // false),
      content_challenge_reward_share,
      conviction_propagation_ratio,
      reputation_decay_rate,
      max_conviction_share_per_member,
      invitation_stake_burn_rate,
      max_reputation_gain_per_epoch,
      max_tags_per_initiative,
      max_staking_rewards_per_season,
      staked_decay_rate,
      new_member_decay_grace_epochs,
      max_treasury_balance,
      treasury_funds_interims: (.treasury_funds_interims // false),
      treasury_funds_retro_pgf: (.treasury_funds_retro_pgf // false),
      max_initiative_stake_per_member,
      max_initiative_rewards_per_season,
      large_project_budget_threshold
    }')

    # Convert LegacyDec fields from raw format to decimal format
    RESET_OP_PARAMS=$(convert_op_params_for_proposal "$RESET_OP_PARAMS")

    jq -n \
      --arg policy "$COMMITTEE_POLICY" \
      --arg alice "$ALICE_ADDR" \
      --argjson op_params "$RESET_OP_PARAMS" \
    '{
      policy_address: $policy,
      metadata: "Restoring original values after test",
      messages: [{
        "@type": "/sparkdream.rep.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }]
    }' > "$PROPOSAL_DIR/reset_rep_op_params.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/reset_rep_op_params.json" \
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
            RESET_PARAMS=$($BINARY query rep params --output json)
            RESET_TIPS=$(echo $RESET_PARAMS | jq -r '.params.max_tips_per_epoch')

            if [ "$RESET_TIPS" == "$INITIAL_MAX_TIPS" ]; then
                RESET_PARAMS_RESULT="PASS"
                echo "  PASS: Params reset to original values"
            else
                echo "  FAIL: Params did not reset correctly (got $RESET_TIPS, expected $INITIAL_MAX_TIPS)"
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
echo "  REP OPERATIONAL PARAMS TEST RESULTS"
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
