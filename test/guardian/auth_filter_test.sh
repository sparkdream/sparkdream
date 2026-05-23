#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E: auth.MsgUpdateParams FILTER
# ============================================================================
# Covers filterAuthUpdateParams:
#
#   negative: tx_size_cost_per_byte = 0 — rejected.
#   negative: sig_verify_cost_ed25519 = 0 — rejected.
#   negative: sig_verify_cost_secp256k1 = 0 — rejected.
#   positive: bump max_memo_characters by 1 (tunable, no floor) — passes.
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo "=================================================="
echo "TEST: guardian auth filter"
echo "=================================================="

ORIG=$(auth_params)
echo "$ORIG" | jq .

MMC=$(echo "$ORIG"   | jq -r '.max_memo_characters')
TSL=$(echo "$ORIG"   | jq -r '.tx_sig_limit')
TSCB=$(echo "$ORIG"  | jq -r '.tx_size_cost_per_byte')
SVCE=$(echo "$ORIG"  | jq -r '.sig_verify_cost_ed25519')
SVCS=$(echo "$ORIG"  | jq -r '.sig_verify_cost_secp256k1')

NEW_MMC=$((MMC + 1))

auth_inner() {
    local field="$1" value="$2"
    jq -n \
        --arg field "$field" --arg value "$value" \
        --arg mmc "$MMC" --arg tsl "$TSL" --arg tscb "$TSCB" \
        --arg svce "$SVCE" --arg svcs "$SVCS" '
        {
          max_memo_characters:       $mmc,
          tx_sig_limit:              $tsl,
          tx_size_cost_per_byte:     $tscb,
          sig_verify_cost_ed25519:   $svce,
          sig_verify_cost_secp256k1: $svcs
        }
        | .[$field] = $value
        | {
            "@type":     "/cosmos.auth.v1beta1.MsgUpdateParams",
            "authority": "",
            "params":    .
          }'
}

build_and_submit() {
    local title="$1" field="$2" value="$3" slug="$4"
    local inner="$PROPOSAL_DIR/auth_${slug}_inner.json"
    local outer="$PROPOSAL_DIR/auth_${slug}_prop.json"
    auth_inner "$field" "$value" > "$inner"
    guardian_exec_proposal "$title" "$inner" "$outer"
    submit_proposal "$outer"
}

echo ""
echo "[neg] tx_size_cost_per_byte = 0..."
PROP_TSCB=$(build_and_submit "AUTH: zero tx_size_cost_per_byte" \
    "tx_size_cost_per_byte" "0" tscb)
echo "  $PROP_TSCB"

echo "[neg] sig_verify_cost_ed25519 = 0..."
PROP_ED=$(build_and_submit "AUTH: zero sig_verify_cost_ed25519" \
    "sig_verify_cost_ed25519" "0" svce)
echo "  $PROP_ED"

echo "[neg] sig_verify_cost_secp256k1 = 0..."
PROP_SE=$(build_and_submit "AUTH: zero sig_verify_cost_secp256k1" \
    "sig_verify_cost_secp256k1" "0" svcs)
echo "  $PROP_SE"

echo "[pos] max_memo_characters +1 (passthrough)..."
PROP_OK=$(build_and_submit "AUTH: bump max_memo_characters" \
    "max_memo_characters" "$NEW_MMC" mmc)
echo "  $PROP_OK"

for id in "$PROP_TSCB" "$PROP_ED" "$PROP_SE" "$PROP_OK"; do
    vote_yes "$id"
done
wait_voting

echo ""
check_status "$PROP_TSCB" "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_ED"   "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_SE"   "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_OK"   "PROPOSAL_STATUS_PASSED" || exit 1

FINAL=$(auth_params)
F_TSCB=$(echo "$FINAL" | jq -r '.tx_size_cost_per_byte')
F_ED=$(echo "$FINAL"   | jq -r '.sig_verify_cost_ed25519')
F_SE=$(echo "$FINAL"   | jq -r '.sig_verify_cost_secp256k1')
F_MMC=$(echo "$FINAL"  | jq -r '.max_memo_characters')

[ "$F_TSCB" = "$TSCB" ] || { echo "FAIL: tx_size_cost_per_byte drift"; exit 1; }
[ "$F_ED"   = "$SVCE" ] || { echo "FAIL: sig_verify_cost_ed25519 drift"; exit 1; }
[ "$F_SE"   = "$SVCS" ] || { echo "FAIL: sig_verify_cost_secp256k1 drift"; exit 1; }
echo "  floor-protected fields unchanged"

if [ "$F_MMC" != "$NEW_MMC" ]; then
    echo "FAIL: max_memo_characters did not update: got=$F_MMC expected=$NEW_MMC"
    exit 1
fi
echo "  max_memo_characters UPDATED: $MMC -> $F_MMC"

echo ""
echo "TEST PASSED: guardian auth filter"
