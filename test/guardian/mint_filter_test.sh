#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E: mint.MsgUpdateParams FILTER
# ============================================================================
# Covers filterMintUpdateParams in one voting cycle:
#
#   negative: 5 proposals, each mutating exactly one immutable field
#             (inflation_min, inflation_max, inflation_rate_change,
#              goal_bonded, mint_denom). All must end PROPOSAL_STATUS_FAILED
#             with mint params unchanged.
#
#   positive: 1 proposal bumping blocks_per_year (tunable). Must end
#             PROPOSAL_STATUS_PASSED with the new value reflected in
#             `q mint params`. Proves the route works through guardian
#             when nothing on the immutable list is touched.
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo "=================================================="
echo "TEST: guardian mint filter"
echo "=================================================="

# Snapshot current mint params; every immutable assertion compares back.
ORIG=$(mint_params)
IMIN=$(echo "$ORIG"  | jq -r '.inflation_min')
IMAX=$(echo "$ORIG"  | jq -r '.inflation_max')
IRC=$(echo "$ORIG"   | jq -r '.inflation_rate_change')
GBND=$(echo "$ORIG"  | jq -r '.goal_bonded')
MDEN=$(echo "$ORIG"  | jq -r '.mint_denom')
BPY=$(echo "$ORIG"   | jq -r '.blocks_per_year')
NEW_BPY=$((BPY + 1)) # tunable bump, smallest possible diff for verification

echo "current mint params:"
echo "  inflation_min=$IMIN  inflation_max=$IMAX  rate_change=$IRC"
echo "  goal_bonded=$GBND  mint_denom=$MDEN  blocks_per_year=$BPY"

# ---- inner-msg builder -----------------------------------------------------
# emit a mint.MsgUpdateParams Any with all fields = current except the named
# override. The override arg is a "field=value" pair so callers can target
# one field at a time.
mint_inner() {
    local override_field="$1"
    local override_value="$2"
    jq -n \
        --arg field   "$override_field" \
        --arg value   "$override_value" \
        --arg imin    "$IMIN" \
        --arg imax    "$IMAX" \
        --arg irc     "$IRC" \
        --arg gbnd    "$GBND" \
        --arg mden    "$MDEN" \
        --arg bpy     "$BPY" '
        {
          inflation_min:         $imin,
          inflation_max:         $imax,
          inflation_rate_change: $irc,
          goal_bonded:           $gbnd,
          mint_denom:            $mden,
          blocks_per_year:       $bpy
        }
        | .[$field] = $value
        | {
            "@type":     "/cosmos.mint.v1beta1.MsgUpdateParams",
            "authority": "",
            "params":    .
          }'
}

# Build + submit a proposal that wraps a mint inner with one field overridden.
build_and_submit() {
    local title="$1"
    local field="$2"
    local value="$3"
    local slug="$4"
    local inner="$PROPOSAL_DIR/mint_${slug}_inner.json"
    local outer="$PROPOSAL_DIR/mint_${slug}_prop.json"
    mint_inner "$field" "$value" > "$inner"
    guardian_exec_proposal "$title" "$inner" "$outer"
    submit_proposal "$outer"
}

# --- negative proposals ----------------------------------------------------
echo ""
echo "[neg] submitting 5 immutable-field violation proposals..."

# Pick deltas that obviously differ from current. LegacyDec serializes as
# the 18-decimal string form, so we add small fixed shifts in the same
# format (the values just need to be != current).
PROP_IMIN=$(build_and_submit "MINT: bump inflation_min" \
    "inflation_min" "0.030000000000000000" imin)
echo "  imin reject prop: $PROP_IMIN"

PROP_IMAX=$(build_and_submit "MINT: bump inflation_max" \
    "inflation_max" "0.100000000000000000" imax)
echo "  imax reject prop: $PROP_IMAX"

PROP_IRC=$(build_and_submit "MINT: bump inflation_rate_change" \
    "inflation_rate_change" "0.200000000000000000" irc)
echo "  irc  reject prop: $PROP_IRC"

PROP_GBND=$(build_and_submit "MINT: change goal_bonded" \
    "goal_bonded" "0.800000000000000000" gbnd)
echo "  gbnd reject prop: $PROP_GBND"

PROP_MDEN=$(build_and_submit "MINT: swap mint_denom" \
    "mint_denom" "uattacker.phoenix" mden)
echo "  mden reject prop: $PROP_MDEN"

# --- positive proposal -----------------------------------------------------
echo ""
echo "[pos] submitting blocks_per_year passthrough proposal..."
PROP_BPY=$(build_and_submit "MINT: tunable blocks_per_year bump" \
    "blocks_per_year" "$NEW_BPY" bpy)
echo "  bpy passthrough prop: $PROP_BPY"

# --- vote on all 6 ---------------------------------------------------------
echo ""
echo "voting yes on all 6 proposals..."
for id in "$PROP_IMIN" "$PROP_IMAX" "$PROP_IRC" "$PROP_GBND" "$PROP_MDEN" "$PROP_BPY"; do
    vote_yes "$id"
done

wait_voting

echo ""
echo "checking final statuses..."
check_status "$PROP_IMIN"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_IMAX"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_IRC"   "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_GBND"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_MDEN"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_BPY"   "PROPOSAL_STATUS_PASSED" || exit 1

# --- final state asserts ---------------------------------------------------
FINAL=$(mint_params)
F_IMIN=$(echo "$FINAL" | jq -r '.inflation_min')
F_IMAX=$(echo "$FINAL" | jq -r '.inflation_max')
F_IRC=$(echo "$FINAL"  | jq -r '.inflation_rate_change')
F_GBND=$(echo "$FINAL" | jq -r '.goal_bonded')
F_MDEN=$(echo "$FINAL" | jq -r '.mint_denom')
F_BPY=$(echo "$FINAL"  | jq -r '.blocks_per_year')

assert_eq() {
    local label="$1" got="$2" expected="$3"
    if [ "$got" = "$expected" ]; then
        echo "  $label unchanged: $got"
    else
        echo "  $label DRIFT: got=$got expected=$expected" >&2
        exit 1
    fi
}
assert_eq inflation_min         "$F_IMIN" "$IMIN"
assert_eq inflation_max         "$F_IMAX" "$IMAX"
assert_eq inflation_rate_change "$F_IRC"  "$IRC"
assert_eq goal_bonded           "$F_GBND" "$GBND"
assert_eq mint_denom            "$F_MDEN" "$MDEN"

if [ "$F_BPY" = "$NEW_BPY" ]; then
    echo "  blocks_per_year UPDATED via passthrough: $BPY -> $F_BPY"
else
    echo "  blocks_per_year DID NOT update: got=$F_BPY expected=$NEW_BPY" >&2
    exit 1
fi

echo ""
echo "TEST PASSED: guardian mint filter"
