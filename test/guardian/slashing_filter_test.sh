#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E: slashing.MsgUpdateParams FILTER
# ============================================================================
# Covers filterSlashingUpdateParams:
#
#   negative: slash_fraction_double_sign = 0 — rejected.
#   negative: slash_fraction_downtime = 0 — rejected.
#   negative: signed_blocks_window above ceiling (2_000_000) — rejected.
#   positive: increase downtime_jail_duration by 1s (tunable) — passes.
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo "=================================================="
echo "TEST: guardian slashing filter"
echo "=================================================="

ORIG=$(slashing_params)
echo "$ORIG" | jq .

SBW=$(echo "$ORIG"  | jq -r '.signed_blocks_window')
MSPW=$(echo "$ORIG" | jq -r '.min_signed_per_window')
DJD=$(echo "$ORIG"  | jq -r '.downtime_jail_duration')
SFDS=$(echo "$ORIG" | jq -r '.slash_fraction_double_sign')
SFD=$(echo "$ORIG"  | jq -r '.slash_fraction_downtime')

# Bump downtime_jail_duration by 1s for the passthrough test. Format must
# match the CLI's duration shape (e.g. "600s", "10m0s"). Easiest: parse
# the trailing 's' and bump.
NEW_DJD_SECS=$(echo "$DJD" | sed 's/s$//' | awk '{print $1 + 1}')
NEW_DJD="${NEW_DJD_SECS}s"

slashing_inner() {
    local field="$1" value="$2"
    jq -n \
        --arg field "$field" --arg value "$value" \
        --arg sbw "$SBW" --arg mspw "$MSPW" --arg djd "$DJD" \
        --arg sfds "$SFDS" --arg sfd "$SFD" '
        {
          signed_blocks_window:       $sbw,
          min_signed_per_window:      $mspw,
          downtime_jail_duration:     $djd,
          slash_fraction_double_sign: $sfds,
          slash_fraction_downtime:    $sfd
        }
        | .[$field] = $value
        | {
            "@type":     "/cosmos.slashing.v1beta1.MsgUpdateParams",
            "authority": "",
            "params":    .
          }'
}

build_and_submit() {
    local title="$1" field="$2" value="$3" slug="$4"
    local inner="$PROPOSAL_DIR/slashing_${slug}_inner.json"
    local outer="$PROPOSAL_DIR/slashing_${slug}_prop.json"
    slashing_inner "$field" "$value" > "$inner"
    guardian_exec_proposal "$title" "$inner" "$outer"
    submit_proposal "$outer"
}

echo ""
echo "[neg] slash_fraction_double_sign = 0..."
PROP_DS=$(build_and_submit "SLASH: zero double-sign fraction" \
    "slash_fraction_double_sign" "0.000000000000000000" sfds)
echo "  $PROP_DS"

echo "[neg] slash_fraction_downtime = 0..."
PROP_DT=$(build_and_submit "SLASH: zero downtime fraction" \
    "slash_fraction_downtime" "0.000000000000000000" sfd)
echo "  $PROP_DT"

echo "[neg] signed_blocks_window = 2000000 (above ceiling)..."
PROP_SBW=$(build_and_submit "SLASH: oversized signed_blocks_window" \
    "signed_blocks_window" "2000000" sbw)
echo "  $PROP_SBW"

echo "[pos] downtime_jail_duration bump (+1s)..."
PROP_OK=$(build_and_submit "SLASH: bump downtime_jail_duration" \
    "downtime_jail_duration" "$NEW_DJD" djd)
echo "  $PROP_OK"

for id in "$PROP_DS" "$PROP_DT" "$PROP_SBW" "$PROP_OK"; do
    vote_yes "$id"
done
wait_voting

echo ""
check_status "$PROP_DS"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_DT"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_SBW" "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_OK"  "PROPOSAL_STATUS_PASSED" || exit 1

FINAL=$(slashing_params)
F_SFDS=$(echo "$FINAL" | jq -r '.slash_fraction_double_sign')
F_SFD=$(echo "$FINAL"  | jq -r '.slash_fraction_downtime')
F_SBW=$(echo "$FINAL"  | jq -r '.signed_blocks_window')
F_DJD=$(echo "$FINAL"  | jq -r '.downtime_jail_duration')

[ "$F_SFDS" = "$SFDS" ] || { echo "FAIL: slash_fraction_double_sign drift"; exit 1; }
[ "$F_SFD"  = "$SFD"  ] || { echo "FAIL: slash_fraction_downtime drift"; exit 1; }
[ "$F_SBW"  = "$SBW"  ] || { echo "FAIL: signed_blocks_window drift"; exit 1; }
echo "  immutable-direction fields unchanged"

if [ "$F_DJD" != "$NEW_DJD" ]; then
    echo "FAIL: downtime_jail_duration did not update: got=$F_DJD expected=$NEW_DJD"
    exit 1
fi
echo "  downtime_jail_duration UPDATED: $DJD -> $F_DJD"

echo ""
echo "TEST PASSED: guardian slashing filter"
