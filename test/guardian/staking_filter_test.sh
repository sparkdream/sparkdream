#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E: staking.MsgUpdateParams FILTER
# ============================================================================
# Covers filterStakingUpdateParams:
#
#   negative: bond_denom change is rejected (the only immutable field).
#   positive: max_validators bump succeeds (tunable).
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo "=================================================="
echo "TEST: guardian staking filter"
echo "=================================================="

ORIG=$(staking_params)
UNBOND=$(echo "$ORIG"   | jq -r '.unbonding_time')
MAXVAL=$(echo "$ORIG"   | jq -r '.max_validators')
MAXENT=$(echo "$ORIG"   | jq -r '.max_entries')
HISTENT=$(echo "$ORIG"  | jq -r '.historical_entries')
BD=$(echo "$ORIG"       | jq -r '.bond_denom')
MINCOMM=$(echo "$ORIG"  | jq -r '.min_commission_rate')
# Note: `key_rotation_fee` was removed from x/staking Params in cosmos-sdk
# v0.53; we no longer round-trip it. If a future SDK reintroduces a similar
# fee, gate it through filterStakingUpdateParams the same way.

NEW_MAXVAL=$((MAXVAL + 1))

echo "current: bond_denom=$BD max_validators=$MAXVAL unbonding_time=$UNBOND"

# Inner builder: produces a full MsgUpdateParams payload for the staking
# module. All current fields are passed through; only the one named in
# $override_field is replaced with $override_value.
staking_inner() {
    local override_field="$1"
    local override_value="$2"
    jq -n \
        --arg field "$override_field" \
        --arg value "$override_value" \
        --arg ut    "$UNBOND" \
        --argjson mv "$MAXVAL" \
        --argjson me "$MAXENT" \
        --argjson hi "$HISTENT" \
        --arg bd    "$BD" \
        --arg mc    "$MINCOMM" '
        {
          unbonding_time:      $ut,
          max_validators:      $mv,
          max_entries:         $me,
          historical_entries:  $hi,
          bond_denom:          $bd,
          min_commission_rate: $mc
        }
        | if $field == "max_validators" then .max_validators = ($value | tonumber)
          else .[$field] = $value
          end
        | {
            "@type":     "/cosmos.staking.v1beta1.MsgUpdateParams",
            "authority": "",
            "params":    .
          }'
}

build_and_submit() {
    local title="$1" field="$2" value="$3" slug="$4"
    local inner="$PROPOSAL_DIR/staking_${slug}_inner.json"
    local outer="$PROPOSAL_DIR/staking_${slug}_prop.json"
    staking_inner "$field" "$value" > "$inner"
    guardian_exec_proposal "$title" "$inner" "$outer"
    submit_proposal "$outer"
}

echo ""
echo "[neg] bond_denom mutation proposal..."
PROP_BD=$(build_and_submit "STAKING: swap bond_denom" \
    "bond_denom" "uattacker.phoenix" bd)
echo "  bond_denom reject prop: $PROP_BD"

echo "[pos] max_validators passthrough proposal..."
PROP_MV=$(build_and_submit "STAKING: bump max_validators" \
    "max_validators" "$NEW_MAXVAL" mv)
echo "  max_validators passthrough prop: $PROP_MV"

vote_yes "$PROP_BD"
vote_yes "$PROP_MV"
wait_voting

echo ""
check_status "$PROP_BD" "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_MV" "PROPOSAL_STATUS_PASSED" || exit 1

FINAL=$(staking_params)
F_BD=$(echo "$FINAL"     | jq -r '.bond_denom')
F_MV=$(echo "$FINAL"     | jq -r '.max_validators')

if [ "$F_BD" != "$BD" ]; then
    echo "FAIL: bond_denom changed: $BD -> $F_BD"
    exit 1
fi
echo "  bond_denom unchanged: $F_BD"

if [ "$F_MV" != "$NEW_MAXVAL" ]; then
    echo "FAIL: max_validators did not update: got=$F_MV expected=$NEW_MAXVAL"
    exit 1
fi
echo "  max_validators UPDATED: $MAXVAL -> $F_MV"

echo ""
echo "TEST PASSED: guardian staking filter"
