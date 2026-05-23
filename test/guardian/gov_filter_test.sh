#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E: gov.MsgUpdateParams FILTER
# ============================================================================
# Covers filterGovUpdateParams:
#
#   negative: voting_period below 6h floor — rejected.
#   negative: quorum below 0.20 floor — rejected.
#   negative: threshold below 0.50 floor — rejected.
#   negative: veto_threshold below 0.20 floor — rejected.
#
# A passthrough positive test is intentionally NOT included here: making
# the gov params *more* permissive (e.g., shorter voting_period) is the
# attack surface guardian protects against, and bumping floors UP would
# permanently slow the e2e suite. The mint test already proves end-to-end
# routing through guardian works; gov is exercised in negative mode only.
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo "=================================================="
echo "TEST: guardian gov filter"
echo "=================================================="

ORIG=$(gov_params)
echo "current gov params:"
echo "$ORIG" | jq -c '{voting_period, expedited_voting_period, quorum, threshold, veto_threshold}'

# Pull current values; mutations override one field at a time. Durations
# round-trip as e.g. "60s" / "172800s".
VP=$(echo "$ORIG"  | jq -r '.voting_period')
EVP=$(echo "$ORIG" | jq -r '.expedited_voting_period')
Q=$(echo "$ORIG"   | jq -r '.quorum')
T=$(echo "$ORIG"   | jq -r '.threshold')
VT=$(echo "$ORIG"  | jq -r '.veto_threshold')
ET=$(echo "$ORIG"  | jq -r '.expedited_threshold')

# Mutate a single field, leaving the rest at their current values. We
# preserve `min_deposit` and other nested fields by inlining a jq pipeline
# that overrides on top of the original params blob.
gov_params_inner() {
    local field="$1" value="$2"
    jq -n \
        --argjson orig "$ORIG" \
        --arg field "$field" \
        --arg value "$value" '
        {
          "@type":     "/cosmos.gov.v1.MsgUpdateParams",
          "authority": "",
          "params":    ($orig | .[$field] = $value)
        }'
}

build_and_submit() {
    local title="$1" field="$2" value="$3" slug="$4"
    local inner="$PROPOSAL_DIR/gov_${slug}_inner.json"
    local outer="$PROPOSAL_DIR/gov_${slug}_prop.json"
    gov_params_inner "$field" "$value" > "$inner"
    guardian_exec_proposal "$title" "$inner" "$outer"
    submit_proposal "$outer"
}

echo ""
echo "[neg] voting_period = 1s (below 6h floor)..."
PROP_VP=$(build_and_submit "GOV: collapse voting_period" "voting_period" "1s" vp)
echo "  $PROP_VP"

echo "[neg] quorum = 0.01 (below 0.20 floor)..."
PROP_Q=$(build_and_submit "GOV: lower quorum" "quorum" "0.010000000000000000" q)
echo "  $PROP_Q"

echo "[neg] threshold = 0.10 (below 0.50 floor)..."
PROP_T=$(build_and_submit "GOV: lower threshold" "threshold" "0.100000000000000000" t)
echo "  $PROP_T"

echo "[neg] veto_threshold = 0.01 (below 0.20 floor)..."
PROP_VT=$(build_and_submit "GOV: lower veto_threshold" "veto_threshold" "0.010000000000000000" vt)
echo "  $PROP_VT"

for id in "$PROP_VP" "$PROP_Q" "$PROP_T" "$PROP_VT"; do
    vote_yes "$id"
done
wait_voting

echo ""
check_status "$PROP_VP" "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_Q"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_T"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_VT" "PROPOSAL_STATUS_FAILED" || exit 1

# All four params must be unchanged.
FINAL=$(gov_params)
F_VP=$(echo "$FINAL" | jq -r '.voting_period')
F_Q=$(echo "$FINAL"  | jq -r '.quorum')
F_T=$(echo "$FINAL"  | jq -r '.threshold')
F_VT=$(echo "$FINAL" | jq -r '.veto_threshold')
[ "$F_VP" = "$VP" ] || { echo "FAIL: voting_period drift $VP -> $F_VP"; exit 1; }
[ "$F_Q"  = "$Q"  ] || { echo "FAIL: quorum drift $Q -> $F_Q"; exit 1; }
[ "$F_T"  = "$T"  ] || { echo "FAIL: threshold drift $T -> $F_T"; exit 1; }
[ "$F_VT" = "$VT" ] || { echo "FAIL: veto_threshold drift $VT -> $F_VT"; exit 1; }

echo "  all four params unchanged"
echo ""
echo "TEST PASSED: guardian gov filter"
