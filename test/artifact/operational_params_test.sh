#!/bin/bash
# x/artifact: operational params are updatable by the Commons Operations
# Committee and clamped by compiled hard bounds.

echo "--- TESTING ARTIFACT: OPERATIONAL PARAMS ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

PARAMS=$(q params | jq '.params')
# Every operational field must be supplied (full replacement).
OP=$(echo "$PARAMS" | jq -c '{class_creation_fee, token_deposit, inbox_fee, pending_ttl, max_listing_duration,
    max_pending_per_pair, max_inbox_per_recipient, max_expirations_per_block, market_enabled, public_mint_enabled,
    max_hides_per_sentinel_per_day, sentinel_unhide_window_blocks, hide_expiry_blocks,
    sentinel_hide_commit_dream}')
ORIG_FEE=$(echo "$OP" | jq -r '.inbox_fee')

NEW=$(echo "$OP" | jq -c '.inbox_fee = "30000"')
expect_fail "outsider cannot update" "not authorized|may not" \
    update-operational-params --operational-params "$NEW" --from artifact_outsider
expect_ok "alice (Ops Committee) updates inbox_fee" \
    update-operational-params --operational-params "$NEW" --from alice
check "inbox_fee updated" "$(q params | jq -r '.params.inbox_fee')" "30000"

ZERO=$(echo "$OP" | jq -c '.token_deposit = "0"')
expect_fail "hard bound: deposit cannot be zeroed" "token_deposit" \
    update-operational-params --operational-params "$ZERO" --from alice

RESTORE=$(echo "$OP" | jq -c --arg f "$ORIG_FEE" '.inbox_fee = $f')
expect_ok "restore inbox_fee" update-operational-params --operational-params "$RESTORE" --from alice
check "inbox_fee restored" "$(q params | jq -r '.params.inbox_fee')" "$ORIG_FEE"

finish
