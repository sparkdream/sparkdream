#!/bin/bash
# ============================================================================
# Forum params helpers for e2e tests.
# ============================================================================
# Bumps params.ephemeral_ttl via a Commons Operations Committee proposal.
# The default config.yml sets ephemeral_ttl to 15s so that post_test.sh
# part 19 can exercise the EndBlocker prune within a fast test run, but
# tests that operate on ephemeral fixtures across many `submit_tx_and_wait`
# (sleep 6) cycles need the fixtures to live longer than ~60s — otherwise
# they get pruned before the assertion runs.
#
# Idempotent: if the current ephemeral_ttl is already >= target, this is a
# no-op. Callers are expected to set:
#   BINARY        — e.g. "sparkdreamd"
#   CHAIN_ID      — e.g. "sparkdream"
#   BOND_DENOM    — sourced via test/lib/denoms.sh
#   SCRIPT_DIR    — directory of the calling test (used for proposals/ dir)
#   ALICE_ADDR    — populated by the calling test (vote/exec is Alice).
# ============================================================================

bump_ephemeral_ttl() {
    local target_ttl=$1
    if [ -z "$target_ttl" ] || [ "$target_ttl" -le 0 ] 2>/dev/null; then
        echo "  bump_ephemeral_ttl: invalid target '$target_ttl'" >&2
        return 1
    fi

    local current
    current=$($BINARY query forum params --output json 2>/dev/null \
        | jq -r '.params.ephemeral_ttl // "0"')
    if [ -n "$current" ] && [ "$current" -ge "$target_ttl" ] 2>/dev/null; then
        return 0
    fi

    local committee_info policy
    committee_info=$($BINARY query commons get-group "Commons Operations Committee" --output json 2>/dev/null)
    policy=$(echo "$committee_info" | jq -r '.group.policy_address')
    if [ -z "$policy" ] || [ "$policy" == "null" ]; then
        echo "  bump_ephemeral_ttl: Commons Operations Committee not found" >&2
        return 1
    fi

    local proposal_dir="$SCRIPT_DIR/proposals"
    mkdir -p "$proposal_dir"

    local params_json op_params
    params_json=$($BINARY query forum params --output json 2>/dev/null)
    op_params=$(echo "$params_json" | jq --arg ttl "$target_ttl" '.params | {
      bounties_enabled: (.bounties_enabled // false),
      reactions_enabled: (.reactions_enabled // false),
      editing_enabled: (.editing_enabled // false),
      spam_tax_amount: (.spam_tax_amount // "0"),
      reaction_spam_tax_amount: (.reaction_spam_tax_amount // "0"),
      flag_spam_tax_amount: (.flag_spam_tax_amount // "0"),
      downvote_deposit_amount: (.downvote_deposit_amount // "0"),
      appeal_fee_amount: (.appeal_fee_amount // "0"),
      lock_appeal_fee_amount: (.lock_appeal_fee_amount // "0"),
      move_appeal_fee_amount: (.move_appeal_fee_amount // "0"),
      edit_fee_amount: (.edit_fee_amount // "0"),
      cost_per_byte_amount: (.cost_per_byte_amount // "0"),
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
      conviction_renewal_threshold,
      conviction_renewal_period,
      min_sentinel_bond: (.min_sentinel_bond // "500000000"),
      min_sentinel_rep_tier: (.min_sentinel_rep_tier // 0),
      min_sentinel_trust_level: (.min_sentinel_trust_level // ""),
      min_sentinel_age_blocks: (.min_sentinel_age_blocks // "0"),
      sentinel_demotion_cooldown: (.sentinel_demotion_cooldown // "0"),
      sentinel_demotion_threshold: (.sentinel_demotion_threshold // "0"),
      sentinel_unhide_window: (.sentinel_unhide_window // "0"),
      sentinel_unbond_cooldown: (.sentinel_unbond_cooldown // "0"),
      make_permanent_min_trust_level: (.make_permanent_min_trust_level // 0),
      max_make_permanent_per_day: (.max_make_permanent_per_day // "10"),
      max_hides_per_epoch: (.max_hides_per_epoch // "50"),
      max_sentinel_locks_per_epoch: (.max_sentinel_locks_per_epoch // "5"),
      max_sentinel_moves_per_epoch: (.max_sentinel_moves_per_epoch // "10"),
      sentinel_slash_amount: (.sentinel_slash_amount // "100000000"),
      max_promotions_per_block: (
        if (.max_promotions_per_block // 0) > 0
        then .max_promotions_per_block else 50 end
      ),
      author_rep_slash: (.author_rep_slash // "5000000000000000000"),
      ephemeral_ttl: $ttl,
      min_post_conviction_stake: (.min_post_conviction_stake // "10000000"),
      post_conviction_lock_seconds: (.post_conviction_lock_seconds // "1209600"),
      post_conviction_stream_rate_per_block: (.post_conviction_stream_rate_per_block // "50000000000000000"),
      max_forum_rep_per_tag_per_epoch: (.max_forum_rep_per_tag_per_epoch // "5000000000000000000"),
      post_conviction_staker_slash_bps: (.post_conviction_staker_slash_bps // "2500"),
      accept_proposal_timeout: (.accept_proposal_timeout // "172800"),
      curation_dream_reward: (.curation_dream_reward // "5000000")
    }')

    jq -n \
      --arg policy "$policy" \
      --argjson op_params "$op_params" \
    '{
      policy_address: $policy,
      metadata: "Bump ephemeral_ttl for promotion-style e2e tests",
      messages: [{
        "@type": "/sparkdream.forum.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }]
    }' > "$proposal_dir/bump_ephemeral_ttl.json"

    local sub_res sub_tx prop_id
    sub_res=$($BINARY tx commons submit-proposal "$proposal_dir/bump_ephemeral_ttl.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000${BOND_DENOM} --output json 2>/dev/null)
    sub_tx=$(echo "$sub_res" | jq -r '.txhash')
    if [ -z "$sub_tx" ] || [ "$sub_tx" == "null" ]; then
        echo "  bump_ephemeral_ttl: submit-proposal returned no txhash" >&2
        return 1
    fi
    sleep 6
    prop_id=$($BINARY query tx "$sub_tx" --output json 2>/dev/null \
        | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' \
        | tr -d '"' | head -n1)
    if [ -z "$prop_id" ] || [ "$prop_id" == "null" ]; then
        echo "  bump_ephemeral_ttl: could not extract proposal_id" >&2
        return 1
    fi

    $BINARY tx commons vote-proposal "$prop_id" yes \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000${BOND_DENOM} --output json > /dev/null 2>&1
    sleep 6
    $BINARY tx commons execute-proposal "$prop_id" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --gas 2000000 --fees 5000000${BOND_DENOM} --output json > /dev/null 2>&1
    sleep 6

    local got
    got=$($BINARY query forum params --output json 2>/dev/null \
        | jq -r '.params.ephemeral_ttl // "0"')
    if [ "$got" != "$target_ttl" ]; then
        echo "  bump_ephemeral_ttl: bump didn't take (got=$got want=$target_ttl)" >&2
        return 1
    fi
    echo "  ephemeral_ttl bumped to ${target_ttl}s (was ${current}s)"
    return 0
}

# bump_post_conviction_params tightens the conviction-stake economics for
# fast e2e runs: drops the 14-day lock window to a few seconds and boosts
# the per-DREAM-per-day stream rate so credits are visible across a handful
# of blocks. Args:
#   $1 — target lock_seconds (e.g. 30)
#   $2 — target rate as a plain decimal string (e.g. "100" for 100 rep/DREAM/day)
#   $3 — target max_forum_rep_per_tag_per_epoch as a plain decimal string
#         (e.g. "1000" to disable the cap practically for this test)
#
# Idempotent on lock_seconds: if current is already <= target lock AND the
# other two parameters are already at the requested values, returns 0
# without re-submitting. Same env requirements as bump_ephemeral_ttl.
bump_post_conviction_params() {
    local target_lock=$1 target_rate_plain=$2 target_cap_plain=$3
    if [ -z "$target_lock" ] || [ -z "$target_rate_plain" ] || [ -z "$target_cap_plain" ]; then
        echo "  bump_post_conviction_params: missing args (lock rate cap)" >&2
        return 1
    fi

    # Encode decimal strings to 18-decimal-shifted integers (math.LegacyDec
    # wire format). Uses awk for portable bignum-ish handling on small ints.
    local target_rate_enc target_cap_enc
    target_rate_enc=$(awk -v v="$target_rate_plain" 'BEGIN{printf "%.0f", v * 1e18}')
    target_cap_enc=$(awk -v v="$target_cap_plain" 'BEGIN{printf "%.0f", v * 1e18}')

    local params_json
    params_json=$($BINARY query forum params --output json 2>/dev/null)
    local cur_lock cur_rate cur_cap
    cur_lock=$(echo "$params_json" | jq -r '.params.post_conviction_lock_seconds // "0"')
    cur_rate=$(echo "$params_json" | jq -r '.params.post_conviction_stream_rate_per_block // "0"')
    cur_cap=$(echo "$params_json"  | jq -r '.params.max_forum_rep_per_tag_per_epoch // "0"')
    if [ "$cur_lock" -le "$target_lock" ] 2>/dev/null && \
       [ "$cur_rate" == "$target_rate_enc" ] && \
       [ "$cur_cap" == "$target_cap_enc" ]; then
        return 0
    fi

    local committee_info policy
    committee_info=$($BINARY query commons get-group "Commons Operations Committee" --output json 2>/dev/null)
    policy=$(echo "$committee_info" | jq -r '.group.policy_address')
    if [ -z "$policy" ] || [ "$policy" == "null" ]; then
        echo "  bump_post_conviction_params: Commons Operations Committee not found" >&2
        return 1
    fi

    local proposal_dir="$SCRIPT_DIR/proposals"
    mkdir -p "$proposal_dir"

    local op_params
    op_params=$(echo "$params_json" \
      | jq --arg lock "$target_lock" \
           --arg rate "$target_rate_enc" \
           --arg cap "$target_cap_enc" \
        '.params | {
      bounties_enabled: (.bounties_enabled // false),
      reactions_enabled: (.reactions_enabled // false),
      editing_enabled: (.editing_enabled // false),
      spam_tax_amount: (.spam_tax_amount // "0"),
      reaction_spam_tax_amount: (.reaction_spam_tax_amount // "0"),
      flag_spam_tax_amount: (.flag_spam_tax_amount // "0"),
      downvote_deposit_amount: (.downvote_deposit_amount // "0"),
      appeal_fee_amount: (.appeal_fee_amount // "0"),
      lock_appeal_fee_amount: (.lock_appeal_fee_amount // "0"),
      move_appeal_fee_amount: (.move_appeal_fee_amount // "0"),
      edit_fee_amount: (.edit_fee_amount // "0"),
      cost_per_byte_amount: (.cost_per_byte_amount // "0"),
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
      conviction_renewal_threshold,
      conviction_renewal_period,
      min_sentinel_bond: (.min_sentinel_bond // "500000000"),
      min_sentinel_rep_tier: (.min_sentinel_rep_tier // 0),
      min_sentinel_trust_level: (.min_sentinel_trust_level // ""),
      min_sentinel_age_blocks: (.min_sentinel_age_blocks // "0"),
      sentinel_demotion_cooldown: (.sentinel_demotion_cooldown // "0"),
      sentinel_demotion_threshold: (.sentinel_demotion_threshold // "0"),
      sentinel_unhide_window: (.sentinel_unhide_window // "0"),
      sentinel_unbond_cooldown: (.sentinel_unbond_cooldown // "0"),
      make_permanent_min_trust_level: (.make_permanent_min_trust_level // 0),
      max_make_permanent_per_day: (.max_make_permanent_per_day // "10"),
      max_hides_per_epoch: (.max_hides_per_epoch // "50"),
      max_sentinel_locks_per_epoch: (.max_sentinel_locks_per_epoch // "5"),
      max_sentinel_moves_per_epoch: (.max_sentinel_moves_per_epoch // "10"),
      sentinel_slash_amount: (.sentinel_slash_amount // "100000000"),
      max_promotions_per_block: (
        if (.max_promotions_per_block // 0) > 0
        then .max_promotions_per_block else 50 end
      ),
      author_rep_slash: (.author_rep_slash // "5000000000000000000"),
      ephemeral_ttl: (.ephemeral_ttl // "15"),
      min_post_conviction_stake: (.min_post_conviction_stake // "10000000"),
      post_conviction_lock_seconds: $lock,
      post_conviction_stream_rate_per_block: $rate,
      max_forum_rep_per_tag_per_epoch: $cap,
      post_conviction_staker_slash_bps: (.post_conviction_staker_slash_bps // "2500"),
      accept_proposal_timeout: (.accept_proposal_timeout // "172800"),
      curation_dream_reward: (.curation_dream_reward // "5000000")
    }')

    jq -n --arg policy "$policy" --argjson op_params "$op_params" \
    '{
      policy_address: $policy,
      metadata: "Tighten post_conviction_* params for e2e",
      messages: [{
        "@type": "/sparkdream.forum.v1.MsgUpdateOperationalParams",
        authority: $policy,
        operational_params: $op_params
      }]
    }' > "$proposal_dir/bump_post_conviction_params.json"

    local sub_res sub_tx prop_id
    sub_res=$($BINARY tx commons submit-proposal "$proposal_dir/bump_post_conviction_params.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000${BOND_DENOM} --output json 2>/dev/null)
    sub_tx=$(echo "$sub_res" | jq -r '.txhash')
    if [ -z "$sub_tx" ] || [ "$sub_tx" == "null" ]; then
        echo "  bump_post_conviction_params: submit-proposal returned no txhash" >&2
        return 1
    fi
    sleep 6
    prop_id=$($BINARY query tx "$sub_tx" --output json 2>/dev/null \
        | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' \
        | tr -d '"' | head -n1)
    if [ -z "$prop_id" ] || [ "$prop_id" == "null" ]; then
        echo "  bump_post_conviction_params: could not extract proposal_id" >&2
        return 1
    fi

    $BINARY tx commons vote-proposal "$prop_id" yes \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000${BOND_DENOM} --output json > /dev/null 2>&1
    sleep 6
    $BINARY tx commons execute-proposal "$prop_id" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --gas 2000000 --fees 5000000${BOND_DENOM} --output json > /dev/null 2>&1
    sleep 6

    local got_lock
    got_lock=$($BINARY query forum params --output json 2>/dev/null \
        | jq -r '.params.post_conviction_lock_seconds // "0"')
    if [ "$got_lock" != "$target_lock" ]; then
        echo "  bump_post_conviction_params: bump didn't take (got_lock=$got_lock want=$target_lock)" >&2
        return 1
    fi
    echo "  post_conviction params: lock=${target_lock}s rate=${target_rate_plain} cap=${target_cap_plain}"
    return 0
}
