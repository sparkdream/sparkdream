#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E: consensus.MsgUpdateParams FILTER
# ============================================================================
# Covers filterConsensusUpdateParams. Consensus params live in cometbft
# state rather than the SDK keeper, so the filter enforces absolute floors
# on the proposed values (no "compare to current" check).
#
#   negative: block.max_bytes below 200_000 — rejected.
#   negative: block.max_gas == 1000 (not -1, below floor) — rejected.
#   negative: evidence.max_age_num_blocks below 1000 — rejected.
#   positive: block.max_gas = -1 (unlimited, always allowed) — passes.
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo "=================================================="
echo "TEST: guardian consensus filter"
echo "=================================================="

ORIG=$($BINARY query consensus params --output json | jq -c '.params // .')
echo "$ORIG" | jq .

# Snapshot pieces; defensive defaults pick up cometbft factory values if
# any sub-struct is absent on this network (some testnets ship without
# explicit evidence params).
ORIG_MAX_BYTES=$(echo "$ORIG" | jq -r '.block.max_bytes // "22020096"')
ORIG_MAX_GAS=$(echo "$ORIG"   | jq -r '.block.max_gas   // "-1"')
ORIG_EV_BLOCKS=$(echo "$ORIG" | jq -r '.evidence.max_age_num_blocks   // "100000"')
ORIG_EV_DUR=$(echo "$ORIG"    | jq -r '.evidence.max_age_duration     // "172800s"')
ORIG_EV_BYTES=$(echo "$ORIG"  | jq -r '.evidence.max_bytes             // "1048576"')
ORIG_PK_TYPES=$(echo "$ORIG"  | jq    '.validator.pub_key_types        // ["ed25519"]')

# Builder: produces a consensus.MsgUpdateParams Any with one named override.
# All four sub-structs (block, evidence, validator, abci) are required by
# the proto contract.
consensus_inner() {
    local override_field="$1"
    local override_value="$2"
    jq -n \
        --arg field "$override_field" \
        --arg value "$override_value" \
        --arg mb    "$ORIG_MAX_BYTES" \
        --arg mg    "$ORIG_MAX_GAS" \
        --arg eb    "$ORIG_EV_BLOCKS" \
        --arg ed    "$ORIG_EV_DUR" \
        --arg ebt   "$ORIG_EV_BYTES" \
        --argjson pk "$ORIG_PK_TYPES" '
        {
          block:    { max_bytes: $mb, max_gas: $mg },
          evidence: { max_age_num_blocks: $eb, max_age_duration: $ed, max_bytes: $ebt },
          validator: { pub_key_types: $pk }
        }
        | if   $field == "block.max_bytes"            then .block.max_bytes = $value
          elif $field == "block.max_gas"              then .block.max_gas   = $value
          elif $field == "evidence.max_age_num_blocks" then .evidence.max_age_num_blocks = $value
          else . end
        | {
            "@type":     "/cosmos.consensus.v1.MsgUpdateParams",
            "authority": "",
            "block":     .block,
            "evidence":  .evidence,
            "validator": .validator
          }'
}

build_and_submit() {
    local title="$1" field="$2" value="$3" slug="$4"
    local inner="$PROPOSAL_DIR/consensus_${slug}_inner.json"
    local outer="$PROPOSAL_DIR/consensus_${slug}_prop.json"
    consensus_inner "$field" "$value" > "$inner"
    guardian_exec_proposal "$title" "$inner" "$outer"
    submit_proposal "$outer"
}

echo ""
echo "[neg] block.max_bytes = 1000 (below floor)..."
PROP_MB=$(build_and_submit "CONS: tiny block.max_bytes" \
    "block.max_bytes" "1000" mb)
echo "  $PROP_MB"

echo "[neg] block.max_gas = 1000 (not -1, below floor)..."
PROP_MG=$(build_and_submit "CONS: tiny block.max_gas" \
    "block.max_gas" "1000" mg)
echo "  $PROP_MG"

echo "[neg] evidence.max_age_num_blocks = 10..."
PROP_EV=$(build_and_submit "CONS: short evidence.max_age_num_blocks" \
    "evidence.max_age_num_blocks" "10" ev)
echo "  $PROP_EV"

echo "[pos] block.max_gas = -1 (unlimited, always allowed)..."
PROP_OK=$(build_and_submit "CONS: max_gas unlimited" \
    "block.max_gas" "-1" mgu)
echo "  $PROP_OK"

for id in "$PROP_MB" "$PROP_MG" "$PROP_EV" "$PROP_OK"; do
    vote_yes "$id"
done
wait_voting

echo ""
check_status "$PROP_MB" "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_MG" "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_EV" "PROPOSAL_STATUS_FAILED" || exit 1
check_status "$PROP_OK" "PROPOSAL_STATUS_PASSED" || exit 1

# Best-effort post-state check. cometbft normalizes max_gas across formats
# ("-1" / "0" / "" depending on chain version), so we only assert that
# the passthrough proposal didn't break params (params query still works
# and returns a coherent block sub-struct).
FINAL=$($BINARY query consensus params --output json | jq -c '.params // .')
echo "$FINAL" | jq -e '.block.max_bytes' > /dev/null
echo "  consensus params query still healthy"

echo ""
echo "TEST PASSED: guardian consensus filter"
