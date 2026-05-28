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
      min_sentinel_bond: (.min_sentinel_bond // "0"),
      min_sentinel_rep_tier: (.min_sentinel_rep_tier // 0),
      min_sentinel_trust_level: (.min_sentinel_trust_level // ""),
      min_sentinel_age_blocks: (.min_sentinel_age_blocks // "0"),
      sentinel_demotion_cooldown: (.sentinel_demotion_cooldown // "0"),
      sentinel_demotion_threshold: (.sentinel_demotion_threshold // "0"),
      sentinel_unhide_window: (.sentinel_unhide_window // "0"),
      sentinel_unbond_cooldown: (.sentinel_unbond_cooldown // "0"),
      make_permanent_min_trust_level: (.make_permanent_min_trust_level // 0),
      max_promotions_per_block: (
        if (.max_promotions_per_block // 0) > 0
        then .max_promotions_per_block else 50 end
      ),
      ephemeral_ttl: $ttl
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
