#!/bin/bash

echo "--- TESTING: PROJECT LIFECYCLE (PROPOSAL CAPS + TTL EXPIRY) ---"
echo ""
echo "Covers:"
echo "  • Proposal-time hard caps on requested_budget / requested_spark"
echo "    (defaults: 1M DREAM / 100K SPARK). Distinct from"
echo "    large_project_budget_threshold (an approval-time routing rule)."
echo "  • TTL on PROPOSED projects: EndBlocker transitions stale proposals"
echo "    to PROJECT_STATUS_EXPIRED after proposed_project_expiry_blocks."
echo ""

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
echo "Alice: $ALICE_ADDR"

# Tracking
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

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

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    local BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

check_tx_success() { [ "$(echo "$1" | jq -r '.code')" = "0" ]; }
check_tx_failure() { [ "$(echo "$1" | jq -r '.code')" != "0" ]; }

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" = "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}

# Look up Commons Ops Committee policy (needed for the TTL test's op-params
# proposal). Operational params are committee-gated, not gov-gated.
COMMITTEE_NAME="Commons Operations Committee"
COMMITTEE_INFO=$($BINARY query commons get-group "$COMMITTEE_NAME" --output json 2>/dev/null)
COMMITTEE_POLICY=$(echo "$COMMITTEE_INFO" | jq -r '.group.policy_address // empty')
if [ -z "$COMMITTEE_POLICY" ] || [ "$COMMITTEE_POLICY" == "null" ]; then
    echo "SETUP ERROR: '$COMMITTEE_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi
echo "Commons Ops Committee policy: $COMMITTEE_POLICY"

# Read initial caps and expiry from chain so the tests stay correct if
# governance moves them.
PARAMS_JSON=$($BINARY query rep params --output json)
BUDGET_CAP=$(echo "$PARAMS_JSON" | jq -r '.params.max_project_requested_budget')
SPARK_CAP=$(echo "$PARAMS_JSON" | jq -r '.params.max_project_requested_spark')
INITIAL_EXPIRY=$(echo "$PARAMS_JSON" | jq -r '.params.proposed_project_expiry_blocks')
# The chain rejects zero/empty expiry at param validation time, so if the
# query came back nil we'd be unable to restore. Fall back to the production
# default so a malformed query doesn't leave the chain in a broken state.
if [ -z "$INITIAL_EXPIRY" ] || [ "$INITIAL_EXPIRY" == "null" ] || [ "$INITIAL_EXPIRY" -le 0 ] 2>/dev/null; then
    echo "WARNING: proposed_project_expiry_blocks query returned '$INITIAL_EXPIRY'; will restore to 200000"
    INITIAL_EXPIRY=200000
fi
echo "Caps: budget=$BUDGET_CAP udream, spark=$SPARK_CAP uspark"
echo "Initial proposed_project_expiry_blocks: $INITIAL_EXPIRY"
echo ""

# ========================================================================
# Op-params proposal helpers (used only by TEST 4 — TTL expiry)
# ========================================================================

# Resolve a tx hash to the commons proposal_id it created.
get_group_proposal_id() {
    local tx_hash=$1
    local retries=0
    local max_retries=10
    while [ $retries -lt $max_retries ]; do
        sleep 1
        TX_RES=$($BINARY query tx $tx_hash --output json 2>/dev/null)
        if [ $? -eq 0 ]; then
            local code=$(echo $TX_RES | jq -r '.code')
            if [ "$code" != "0" ]; then return 1; fi
            local prop_id=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
            if [ -n "$prop_id" ] && [ "$prop_id" != "null" ]; then
                echo "$prop_id"
                return 0
            fi
        fi
        ((retries++))
    done
    return 1
}

# Vote YES + execute. The Commons Ops Committee uses threshold=1, so a single
# member's YES vote is sufficient to pass.
vote_and_execute() {
    local prop_id=$1
    $BINARY tx commons vote-proposal $prop_id yes \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000${BOND_DENOM} --output json > /dev/null 2>&1
    sleep 6
    $BINARY tx commons execute-proposal $prop_id \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --gas 2000000 --fees 5000000${BOND_DENOM} --output json > /dev/null 2>&1
    sleep 6
    local status=$($BINARY query commons get-proposal $prop_id --output json 2>/dev/null | jq -r '.proposal.status // empty')
    [ "$status" == "PROPOSAL_STATUS_EXECUTED" ]
}

# The CLI query renders LegacyDec fields as raw 18-precision integers, but
# proto-JSON unmarshaling via the proposal expects decimal strings. Mirror the
# converter from operational_params_test.sh; keep the field list in sync.
convert_op_params_for_proposal() {
    local params_json="$1"
    python3 -c "
import json, sys
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
    'staked_decay_rate',
    'sentinel_reward_pool_overflow_burn_ratio',
    'min_sentinel_accuracy', 'min_appeal_rate'
]
PRECISION = 18
params = json.loads(sys.argv[1])
for field in DEC_FIELDS:
    if field in params and params[field]:
        raw = str(params[field])
        if '.' in raw:
            params[field] = raw.rstrip('0').rstrip('.')
        else:
            padded = raw.zfill(PRECISION + 1)
            int_part = padded[:len(padded) - PRECISION]
            dec_part = padded[len(padded) - PRECISION:]
            params[field] = (int_part + '.' + dec_part).rstrip('0').rstrip('.')
print(json.dumps(params))
" "$params_json"
}

# Build the full operational params JSON from current chain state, with one
# field override applied. Returns the converted (proposal-ready) JSON.
build_op_params_with_override() {
    local override_field="$1"
    local override_value="$2"

    local raw_params=$($BINARY query rep params --output json)
    local op_params=$(echo "$raw_params" | jq '.params | {
      epoch_blocks, season_duration_epochs, unstaked_decay_rate,
      transfer_tax_rate, max_tip_amount, max_tips_per_epoch,
      max_gift_amount,
      gift_only_to_invitees: (.gift_only_to_invitees // false),
      min_reputation_multiplier, default_review_period_epochs,
      default_challenge_period_epochs, min_invitation_stake,
      invitation_accountability_epochs, referral_reward_rate,
      invitation_cost_multiplier, min_challenge_stake,
      challenger_reward_rate, jury_size, jury_super_majority,
      min_juror_reputation, simple_complexity_budget,
      standard_complexity_budget, complex_complexity_budget,
      expert_complexity_budget, solo_expert_bonus_rate,
      interim_deadline_epochs, max_active_challenges_per_committee,
      max_new_challenges_per_epoch, challenge_queue_max_size,
      project_completion_bonus_rate, member_stake_revenue_share,
      tag_stake_revenue_share, min_stake_duration_seconds,
      allow_self_member_stake: (.allow_self_member_stake // false),
      challenge_response_deadline_epochs, gift_cooldown_blocks,
      max_gifts_per_sender_epoch, content_conviction_half_life_epochs,
      max_content_stake_per_member, max_author_bond_per_content,
      author_bond_slash_on_moderation: (.author_bond_slash_on_moderation // false),
      content_challenge_reward_share, conviction_propagation_ratio,
      reputation_decay_rate, max_conviction_share_per_member,
      invitation_stake_burn_rate, max_reputation_gain_per_epoch,
      max_tags_per_initiative, max_staking_rewards_per_season,
      staked_decay_rate, new_member_decay_grace_epochs,
      max_treasury_balance,
      treasury_funds_interims: (.treasury_funds_interims // false),
      treasury_funds_retro_pgf: (.treasury_funds_retro_pgf // false),
      max_initiative_stake_per_member, max_initiative_rewards_per_season,
      large_project_budget_threshold, project_creation_fee,
      initiative_creation_fee_apprentice, initiative_creation_fee_standard,
      tag_creation_fee, max_sentinel_reward_pool,
      sentinel_reward_pool_overflow_burn_ratio, sentinel_reward_epoch_blocks,
      min_sentinel_accuracy, min_appeals_for_accuracy,
      min_epoch_activity_for_reward, min_appeal_rate,
      max_active_initiatives_per_member, max_active_interims_per_member,
      max_dream_mint_per_epoch,
      max_project_requested_budget, max_project_requested_spark,
      proposed_project_expiry_blocks
    }')

    # Apply the override. int64 fields are JSON numbers in the query output;
    # string-typed Int fields (e.g. max_project_requested_budget) come back as
    # quoted strings to preserve precision. Preserve whichever type the
    # existing field uses so the proposal JSON round-trips cleanly.
    op_params=$(echo "$op_params" | jq --arg f "$override_field" --arg v "$override_value" '
      .[$f] = (if (.[$f] | type) == "number" then ($v | tonumber) else $v end)
    ')

    convert_op_params_for_proposal "$op_params"
}

# Submit + vote + execute a one-field operational params update.
update_operational_param() {
    local field="$1"
    local value="$2"
    local label="$3"

    local op_params=$(build_op_params_with_override "$field" "$value")
    local proposal_file="$PROPOSAL_DIR/proj_lifecycle_${field}.json"

    jq -n \
      --arg policy "$COMMITTEE_POLICY" \
      --argjson op_params "$op_params" \
    '{
      policy_address: $policy,
      metadata: ($ARGS.positional[0]),
      messages: [{
        "@type": "/sparkdream.rep.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }]
    }' --args "$label" > "$proposal_file"

    local submit_res=$($BINARY tx commons submit-proposal "$proposal_file" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000${BOND_DENOM} --output json)
    local tx_hash=$(echo "$submit_res" | jq -r '.txhash')
    local prop_id=$(get_group_proposal_id "$tx_hash")
    if [ -z "$prop_id" ] || [ "$prop_id" == "null" ]; then
        echo "  ERROR: could not submit op-params proposal ($label)"
        return 1
    fi
    if ! vote_and_execute "$prop_id"; then
        echo "  ERROR: op-params proposal $prop_id failed to execute ($label)"
        return 1
    fi
    return 0
}

# ========================================================================
# TEST 1 — requested_budget over the cap is rejected
# ========================================================================
# Single-shot rejection at message-server time. The error string includes
# both ErrRequestedBudgetExceedsCap's registered text and the requested-vs-cap
# values, so we grep on the stable substring.
echo "--- TEST 1: requested_budget over cap is rejected ---"

OVER_BUDGET=$(echo "$BUDGET_CAP + 1" | bc)
TX_RES=$($BINARY tx rep propose-project \
    "lifecycle-over-budget" "Over the budget cap" "infrastructure" \
    "Technical Council" "$OVER_BUDGET" "0" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} --gas 400000 -y --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW" | grep -qi "requested project budget exceeds"; then
        echo "  Correctly rejected: $RAW"
        record_result "TEST 1: budget over cap rejected" "PASS"
    else
        echo "  Rejected but with unexpected error: $RAW"
        record_result "TEST 1: budget over cap rejected" "FAIL"
    fi
else
    echo "  Expected rejection but tx succeeded"
    record_result "TEST 1: budget over cap rejected" "FAIL"
fi

# ========================================================================
# TEST 2 — requested_spark over the cap is rejected
# ========================================================================
echo "--- TEST 2: requested_spark over cap is rejected ---"

OVER_SPARK=$(echo "$SPARK_CAP + 1" | bc)
TX_RES=$($BINARY tx rep propose-project \
    "lifecycle-over-spark" "Over the SPARK cap" "infrastructure" \
    "Technical Council" "0" "$OVER_SPARK" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} --gas 400000 -y --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW" | grep -qi "requested project SPARK exceeds"; then
        echo "  Correctly rejected: $RAW"
        record_result "TEST 2: spark over cap rejected" "PASS"
    else
        echo "  Rejected but with unexpected error: $RAW"
        record_result "TEST 2: spark over cap rejected" "FAIL"
    fi
else
    echo "  Expected rejection but tx succeeded"
    record_result "TEST 2: spark over cap rejected" "FAIL"
fi

# ========================================================================
# TEST 3 — exactly at the cap is accepted (boundary, GT not GTE)
# ========================================================================
# A request equal to the cap must pass — this is the same convention the
# existing large_project_budget_threshold uses, and our new tests pin it.
# The project is left in PROPOSED state; nothing else relies on it.
echo "--- TEST 3: budget exactly at cap is accepted ---"

TX_RES=$($BINARY tx rep propose-project \
    "lifecycle-at-cap" "Budget exactly at cap" "infrastructure" \
    "Technical Council" "$BUDGET_CAP" "0" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} --gas 400000 -y --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    echo "  Accepted at budget=$BUDGET_CAP (cap)"
    record_result "TEST 3: at-cap accepted" "PASS"
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    echo "  Expected acceptance but tx failed: $RAW"
    record_result "TEST 3: at-cap accepted" "FAIL"
fi

# ========================================================================
# TEST 4 — TTL: stale PROPOSED projects are auto-expired by EndBlocker
# ========================================================================
# The default expiry (200,000 blocks ≈ 11.5 days) is way longer than any e2e
# run, so we lower it via an op-params proposal, then propose a fresh project
# and let the EndBlocker sweep transition it to PROJECT_STATUS_EXPIRED. We
# restore the original value at the end so subsequent tests / reruns are not
# affected.
echo "--- TEST 4: PROPOSED project auto-expires via EndBlocker ---"

# 4a. Lower expiry to 5 blocks (~25-30s at 5s block time, plenty for one
# sweep but still well below any other test's propose→approve window).
echo "  Lowering proposed_project_expiry_blocks to 5..."
if ! update_operational_param "proposed_project_expiry_blocks" "5" "TTL test: lower expiry"; then
    record_result "TEST 4: TTL expiry" "FAIL"
else
    # 4b. Propose a project that no one will approve.
    TX_RES=$($BINARY tx rep propose-project \
        "lifecycle-ttl-target" "Will be expired by EndBlocker" "infrastructure" \
        "Technical Council" "100000000" "0" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} --gas 400000 -y --output json 2>&1)
    if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
        echo "  Failed to propose TTL target project: $(echo "$TX_RESULT" | jq -r '.raw_log // ""')"
        record_result "TEST 4: TTL expiry" "FAIL"
    else
        TTL_PID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="project_proposed") | .attributes[] | select(.key=="project_id") | .value' | tr -d '"')
        echo "  Proposed TTL target project #$TTL_PID"

        # 4c. Sleep through the expiry window. 5 blocks at ~5s/block ≈ 25s;
        # add headroom for block-time jitter and the EndBlocker landing on
        # the right boundary.
        echo "  Sleeping 35s to let the EndBlocker sweep run..."
        sleep 35

        # 4d. Status must now be EXPIRED. Read via get-project; expiry must
        # be cleared (we set it to 0 on transition).
        PROJ_JSON=$($BINARY query rep get-project "$TTL_PID" --output json 2>/dev/null)
        FINAL_STATUS=$(echo "$PROJ_JSON" | jq -r '.project.status // empty')
        FINAL_EXPIRY=$(echo "$PROJ_JSON" | jq -r '.project.expiry_block_height // "0"')

        echo "  Project #$TTL_PID status: $FINAL_STATUS (expiry_block_height=$FINAL_EXPIRY)"
        if [ "$FINAL_STATUS" == "PROJECT_STATUS_EXPIRED" ] && [ "$FINAL_EXPIRY" == "0" ]; then
            record_result "TEST 4: TTL expiry" "PASS"
        else
            echo "  Expected PROJECT_STATUS_EXPIRED and expiry_block_height=0"
            record_result "TEST 4: TTL expiry" "FAIL"
        fi
    fi

    # 4e. Restore the original expiry so subsequent tests / reruns aren't
    # affected. Best-effort: a failure here is logged but not propagated.
    echo "  Restoring proposed_project_expiry_blocks to $INITIAL_EXPIRY..."
    if ! update_operational_param "proposed_project_expiry_blocks" "$INITIAL_EXPIRY" "TTL test: restore expiry"; then
        echo "  WARNING: failed to restore proposed_project_expiry_blocks; subsequent runs may see a short expiry"
    fi
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "PROJECT LIFECYCLE TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
    exit 0
fi
