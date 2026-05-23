#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E: distribution FILTERS
# ============================================================================
# Covers two distribution-related arms of guardian's switch:
#
#   negative: distribution.MsgCommunityPoolSpend — hard reject regardless
#             of recipient or amount. x/split is the canonical path for
#             community-pool spending; gov must not be able to drain it
#             directly. (Listed in the allowlist so the rejection emits
#             an explicit "must flow through x/split" error rather than
#             the generic deny-by-default message.)
#
#   negative: distribution.MsgUpdateParams with community_tax below the
#             floor (0.05) — rejected by filterDistrUpdateParams.
#   negative: distribution.MsgUpdateParams with community_tax above the
#             ceiling (0.25) — rejected.
#   positive: distribution.MsgUpdateParams with community_tax inside the
#             band — passes through. Toggles community_tax up by 1bp and
#             verifies the value changed.
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo "=================================================="
echo "TEST: guardian distribution filter"
echo "=================================================="

ORIG=$(distr_params)
CTAX=$(echo "$ORIG" | jq -r '.community_tax')
BPROP=$(echo "$ORIG" | jq -r '.base_proposer_reward // "0.000000000000000000"')
BBONUS=$(echo "$ORIG" | jq -r '.bonus_proposer_reward // "0.000000000000000000"')
WADE=$(echo "$ORIG" | jq -r '.withdraw_addr_enabled')

# Build a new tax 1bp above current, still inside the [0.05, 0.25] band.
# We compute by reading the current decimal and adjusting; this needs
# python-free pure-bash math, so we just compose strings around a fixed
# delta knowing genesis ships 0.15.
#
# Genesis sets community_tax = 0.150000000000000000.
# Floor: 0.05; ceiling: 0.25. We pick 0.160000000000000000 as the
# in-band passthrough target (independent of current value).
NEW_TAX_PASSTHROUGH="0.160000000000000000"
TAX_BELOW_FLOOR="0.010000000000000000"
TAX_ABOVE_CEIL="0.500000000000000000"

distr_params_inner() {
    local tax="$1"
    jq -n \
        --arg tax    "$tax" \
        --arg bprop  "$BPROP" \
        --arg bbonus "$BBONUS" \
        --argjson w  "$WADE" '
        {
          "@type":     "/cosmos.distribution.v1beta1.MsgUpdateParams",
          "authority": "",
          "params": {
            community_tax:          $tax,
            base_proposer_reward:   $bprop,
            bonus_proposer_reward:  $bbonus,
            withdraw_addr_enabled:  $w
          }
        }'
}

build_params_proposal() {
    local title="$1" tax="$2" slug="$3"
    local inner="$PROPOSAL_DIR/distr_${slug}_inner.json"
    local outer="$PROPOSAL_DIR/distr_${slug}_prop.json"
    distr_params_inner "$tax" > "$inner"
    guardian_exec_proposal "$title" "$inner" "$outer"
    submit_proposal "$outer"
}

# MsgCommunityPoolSpend — hard reject regardless of inner content.
echo ""
echo "[neg] CommunityPoolSpend (any amount) - hard reject..."
cat > "$PROPOSAL_DIR/distr_cps_inner.json" <<EOF
{
  "@type":     "/cosmos.distribution.v1beta1.MsgCommunityPoolSpend",
  "authority": "",
  "recipient": "$ALICE_ADDR",
  "amount":    [ { "denom": "$BOND_DENOM", "amount": "1" } ]
}
EOF
guardian_exec_proposal "DIST: CommunityPoolSpend drain attempt" \
    "$PROPOSAL_DIR/distr_cps_inner.json" "$PROPOSAL_DIR/distr_cps_prop.json"
PROP_CPS=$(submit_proposal "$PROPOSAL_DIR/distr_cps_prop.json")
echo "  $PROP_CPS"

echo "[neg] community_tax = $TAX_BELOW_FLOOR (below floor)..."
PROP_LOW=$(build_params_proposal "DIST: community_tax below floor" "$TAX_BELOW_FLOOR" tax_low)
echo "  $PROP_LOW"

echo "[neg] community_tax = $TAX_ABOVE_CEIL (above ceiling)..."
PROP_HIGH=$(build_params_proposal "DIST: community_tax above ceiling" "$TAX_ABOVE_CEIL" tax_high)
echo "  $PROP_HIGH"

echo "[pos] community_tax = $NEW_TAX_PASSTHROUGH (in band)..."
PROP_OK=$(build_params_proposal "DIST: community_tax passthrough" "$NEW_TAX_PASSTHROUGH" tax_ok)
echo "  $PROP_OK"

for id in "$PROP_CPS" "$PROP_LOW" "$PROP_HIGH" "$PROP_OK"; do
    vote_yes "$id"
done
wait_voting

echo ""
check_status "$PROP_CPS"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_LOW"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_HIGH" "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_OK"   "PROPOSAL_STATUS_PASSED" || exit 1

FINAL_TAX=$(distr_params | jq -r '.community_tax')
if [ "$FINAL_TAX" != "$NEW_TAX_PASSTHROUGH" ]; then
    echo "FAIL: community_tax did not update via passthrough: got=$FINAL_TAX expected=$NEW_TAX_PASSTHROUGH"
    exit 1
fi
echo "  community_tax UPDATED: $CTAX -> $FINAL_TAX"

echo ""
echo "TEST PASSED: guardian distribution filter"
