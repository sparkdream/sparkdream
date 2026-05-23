#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E: bank.MsgSetSendEnabled FILTER + MsgUpdateParams PASSTHROUGH
# ============================================================================
# Covers filterBankSetSendEnabled:
#
#   negative: SetSendEnabled disabling the native bond denom — rejected.
#   negative: SetSendEnabled disabling the native dream denom — rejected.
#   negative: UseDefaultFor pointing at native bond denom — rejected.
#   positive: SetSendEnabled on a foreign denom (ibc/... pseudo-trace) —
#             succeeds. Bank treats unknown denoms as legitimate targets,
#             and guardian's filter is scoped to native denoms only.
#   positive: bank.MsgUpdateParams with default_send_enabled=true — passes
#             through (no field filter on bank params).
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo "=================================================="
echo "TEST: guardian bank filter"
echo "=================================================="
echo "native bond denom:  $BOND_DENOM"
echo "native dream denom: $DREAM_DENOM"

# A clearly foreign denom (IBC trace shape, but with a sentinel hash).
FOREIGN_DENOM="ibc/0000000000000000000000000000000000000000000000000000000000000001"

ssec_proposal() {
    local title="$1" denom="$2" enabled="$3" use_default="$4" slug="$5"
    local inner="$PROPOSAL_DIR/bank_${slug}_inner.json"
    local outer="$PROPOSAL_DIR/bank_${slug}_prop.json"
    if [ "$use_default" = "true" ]; then
        jq -n --arg d "$denom" '{
          "@type":     "/cosmos.bank.v1beta1.MsgSetSendEnabled",
          "authority": "",
          "send_enabled":    [],
          "use_default_for": [ $d ]
        }' > "$inner"
    else
        jq -n --arg d "$denom" --argjson e "$enabled" '{
          "@type":     "/cosmos.bank.v1beta1.MsgSetSendEnabled",
          "authority": "",
          "send_enabled":    [ { denom: $d, enabled: $e } ],
          "use_default_for": []
        }' > "$inner"
    fi
    guardian_exec_proposal "$title" "$inner" "$outer"
    submit_proposal "$outer"
}

echo ""
echo "[neg] SetSendEnabled disable native BOND_DENOM..."
PROP_DISABLE_BOND=$(ssec_proposal \
    "BANK: disable native bond_denom" "$BOND_DENOM" false false disable_bond)
echo "  $PROP_DISABLE_BOND"

echo "[neg] SetSendEnabled disable native DREAM_DENOM..."
PROP_DISABLE_DREAM=$(ssec_proposal \
    "BANK: disable native dream_denom" "$DREAM_DENOM" false false disable_dream)
echo "  $PROP_DISABLE_DREAM"

echo "[neg] UseDefaultFor with native BOND_DENOM..."
PROP_USE_DEFAULT=$(ssec_proposal \
    "BANK: use_default_for native bond_denom" "$BOND_DENOM" false true use_default_native)
echo "  $PROP_USE_DEFAULT"

echo "[pos] SetSendEnabled enable foreign denom (passthrough)..."
PROP_FOREIGN=$(ssec_proposal \
    "BANK: enable foreign denom" "$FOREIGN_DENOM" true false foreign_enable)
echo "  $PROP_FOREIGN"

# bank.MsgUpdateParams passthrough — flip default_send_enabled to itself.
# Reading the current bank params and writing them back unchanged is a
# valid no-op that proves the dispatch reaches bank cleanly.
echo "[pos] bank.MsgUpdateParams no-op (passthrough)..."
BANK_PARAMS=$($BINARY query bank params --output json | jq -c '.params // .')
BANK_DEFAULT_ENABLED=$(echo "$BANK_PARAMS" | jq -r '.default_send_enabled')
jq -n --argjson d "$BANK_DEFAULT_ENABLED" '{
  "@type":     "/cosmos.bank.v1beta1.MsgUpdateParams",
  "authority": "",
  "params": {
    "default_send_enabled": $d,
    "send_enabled":         []
  }
}' > "$PROPOSAL_DIR/bank_updateparams_inner.json"
guardian_exec_proposal "BANK: MsgUpdateParams no-op" \
    "$PROPOSAL_DIR/bank_updateparams_inner.json" \
    "$PROPOSAL_DIR/bank_updateparams_prop.json"
PROP_UPDATEPARAMS=$(submit_proposal "$PROPOSAL_DIR/bank_updateparams_prop.json")
echo "  $PROP_UPDATEPARAMS"

echo ""
echo "voting yes on all 5..."
for id in "$PROP_DISABLE_BOND" "$PROP_DISABLE_DREAM" "$PROP_USE_DEFAULT" \
          "$PROP_FOREIGN" "$PROP_UPDATEPARAMS"; do
    vote_yes "$id"
done

wait_voting

echo ""
check_status "$PROP_DISABLE_BOND"  "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_DISABLE_DREAM" "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_USE_DEFAULT"   "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_FOREIGN"       "PROPOSAL_STATUS_PASSED" || exit 1
check_status "$PROP_UPDATEPARAMS"  "PROPOSAL_STATUS_PASSED" || exit 1

# Native denoms must still be send-enabled.
SEND_ENABLED_BOND=$($BINARY q bank send-enabled "$BOND_DENOM" --output json 2>/dev/null | \
    jq -r '.send_enabled[0].enabled // true')
if [ "$SEND_ENABLED_BOND" = "false" ]; then
    echo "FAIL: $BOND_DENOM send-enabled is false after run"
    exit 1
fi
echo "  $BOND_DENOM still send-enabled"

echo ""
echo "TEST PASSED: guardian bank filter"
