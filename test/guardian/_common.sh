#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E TEST COMMON HELPERS
# ============================================================================
# Sourced by every guardian/*_test.sh. Provides:
#
#   Globals: BINARY, CHAIN_ID, ALICE_ADDR, BOB_ADDR, GOV_ADDR, GUARDIAN_ADDR,
#            BOND_DENOM, DREAM_DENOM, SCRIPT_DIR, PROPOSAL_DIR
#
#   submit_proposal <json_path>             -> echoes proposal_id
#   vote_yes <proposal_id>                  -> votes yes from alice+bob
#   wait_voting                             -> sleeps 75s (config voting=60s)
#   check_status <proposal_id> <status>     -> returns 0 if match, 1 otherwise
#   mint_params / staking_params / ...      -> echo current JSON params
#
# The flow expected by every test:
#
#   1. Take a params snapshot for any module the test mutates.
#   2. Build N proposal JSON files (inline heredocs).
#   3. submit_proposal each, capturing proposal_ids.
#   4. vote_yes each id.
#   5. wait_voting (ONCE — voting periods overlap).
#   6. check_status each id with the expected outcome.
#   7. Verify post-state matches expectations.
#
# Batching multiple proposals into a single voting cycle keeps total runtime
# bounded by one voting period (~75s) regardless of how many filter cases
# the test covers.
# ============================================================================

set -euo pipefail

# Path setup. SCRIPT_DIR is set by the calling test (it's local to that script).
SCRIPT_DIR="${SCRIPT_DIR:-$( cd "$( dirname "${BASH_SOURCE[1]}" )" && pwd )}"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="${BINARY:-sparkdreamd}"
CHAIN_ID="${CHAIN_ID:-sparkdream}"

# Test keys. alice + bob are seeded with stake by genesis_vals.go.
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Module addresses. The gov address is the proposal-execution signer; the
# guardian address is what mint/staking/etc. set as their authority.
GOV_ADDR=$($BINARY query auth module-account gov --output json | \
    jq -r '.account.base_account.address // .account.value.address')
GUARDIAN_ADDR=$($BINARY query auth module-account guardian --output json | \
    jq -r '.account.base_account.address // .account.value.address')

# Native denoms. Resolve via x/identity so tests are portable across
# per-chain federation deployments (Phoenix Spark = upspk.phoenix, etc.).
BOND_DENOM=$($BINARY q identity bond-denom --output json | jq -r '.denom // .bond_denom')
DREAM_DENOM=$($BINARY q identity dream-denom --output json | jq -r '.denom // .dream_denom')

if [ -z "$GUARDIAN_ADDR" ] || [ "$GUARDIAN_ADDR" = "null" ]; then
    echo "guardian: GUARDIAN_ADDR not resolvable (is x/guardian wired?)"
    exit 1
fi
if [ -z "$BOND_DENOM" ] || [ "$BOND_DENOM" = "null" ]; then
    echo "guardian: BOND_DENOM not resolvable (is x/identity wired?)"
    exit 1
fi

# ----------------------------------------------------------------------------
# submit_proposal <proposal_json_path>
#
# Submits the proposal JSON as alice (a stakeholder large enough to cover
# initial deposit). Polls for the proposal_id (some chains lag the tx
# query by a block). Echoes the id on stdout; exits non-zero on failure.
# ----------------------------------------------------------------------------
submit_proposal() {
    local prop_path="$1"
    local submit_res
    # No `2>&1`: with `--gas auto`, the CLI writes "gas estimate: N" to
    # stderr which would otherwise prepend itself to the JSON payload and
    # break jq parsing. A fixed `--gas` also avoids the stderr line.
    submit_res=$($BINARY tx gov submit-proposal "$prop_path" \
        --from alice --chain-id "$CHAIN_ID" --keyring-backend test \
        --gas 400000 --yes --output json)
    local tx_hash
    tx_hash=$(echo "$submit_res" | jq -r '.txhash // empty')
    if [ -z "$tx_hash" ]; then
        echo "guardian: submit-proposal returned no txhash:" >&2
        echo "$submit_res" | head -10 >&2
        return 1
    fi
    local retries=0
    local max_retries=15
    while [ $retries -lt $max_retries ]; do
        sleep 1
        local tx_res
        tx_res=$($BINARY query tx "$tx_hash" --output json 2>/dev/null || echo "")
        if [ -n "$tx_res" ]; then
            local prop_id
            prop_id=$(echo "$tx_res" | jq -r '.events[]? | select(.type=="submit_proposal") | .attributes[]? | select(.key=="proposal_id") | .value' | tr -d '"')
            if [ -z "$prop_id" ] || [ "$prop_id" = "null" ]; then
                prop_id=$(echo "$tx_res" | jq -r '.logs[0].events[]? | select(.type=="submit_proposal") | .attributes[]? | select(.key=="proposal_id") | .value' | tr -d '"')
            fi
            if [ -n "$prop_id" ] && [ "$prop_id" != "null" ]; then
                echo "$prop_id"
                return 0
            fi
        fi
        retries=$((retries + 1))
    done
    echo "guardian: timed out waiting for proposal_id (tx $tx_hash)" >&2
    return 1
}

# ----------------------------------------------------------------------------
# vote_yes <proposal_id>
#
# Casts yes from alice AND bob (config genesis seeds both with stake so
# their combined vote clears quorum + threshold reliably).
# ----------------------------------------------------------------------------
vote_yes() {
    local id="$1"
    $BINARY tx gov vote "$id" yes --from alice --chain-id "$CHAIN_ID" \
        --keyring-backend test --yes --output json > /dev/null
    sleep 2
    $BINARY tx gov vote "$id" yes --from bob --chain-id "$CHAIN_ID" \
        --keyring-backend test --yes --output json > /dev/null
    sleep 2
}

# ----------------------------------------------------------------------------
# wait_voting
#
# Sleeps the configured voting period plus a safety buffer. config.yml
# ships voting_period=60s for test builds; we wait 75s to cover clock skew
# and the next-block latency between voting-end and the FAILED status
# update.
# ----------------------------------------------------------------------------
wait_voting() {
    echo "guardian: waiting voting_period (75s)..."
    sleep 75
}

# ----------------------------------------------------------------------------
# check_status <proposal_id> <expected_status>
#
# Returns 0 if the proposal's final status equals expected, 1 otherwise.
# Prints a diagnostic line either way so test logs are self-explaining.
# Valid expected values:
#   PROPOSAL_STATUS_PASSED   — voted through AND executed cleanly
#   PROPOSAL_STATUS_FAILED   — voted through but execution errored
#   PROPOSAL_STATUS_REJECTED — voting failed quorum/threshold
# ----------------------------------------------------------------------------
check_status() {
    local id="$1"
    local expected="$2"
    local got
    got=$($BINARY query gov proposal "$id" --output json | \
        jq -r '.status // .proposal.status')
    if [ "$got" = "$expected" ]; then
        echo "  prop $id: status=$got (PASS)"
        return 0
    fi
    echo "  prop $id: status=$got, expected=$expected (FAIL)" >&2
    return 1
}

# ----------------------------------------------------------------------------
# Convenience accessors for current params, normalizing the .params /
# flat-root JSON shape that varies by SDK version.
# ----------------------------------------------------------------------------
mint_params()     { $BINARY query mint params     --output json | jq -c '.params // .'; }
staking_params()  { $BINARY query staking params  --output json | jq -c '.params // .'; }
distr_params()    { $BINARY query distribution params --output json | jq -c '.params // .'; }
gov_params()      { $BINARY query gov params      --output json | jq -c '.params // .'; }
slashing_params() { $BINARY query slashing params --output json | jq -c '.params // .'; }
auth_params()     { $BINARY query auth params     --output json | jq -c '.params // .'; }

# ----------------------------------------------------------------------------
# guardian_exec_proposal <title> <inner_json_path> <output_path>
#
# Wraps an inner msg JSON file in the standard guardian.MsgExec +
# gov.MsgSubmitProposal envelope, writing the full proposal JSON to
# output_path. Centralizes the boilerplate so tests focus on the inner
# msg's field-level mutations.
# ----------------------------------------------------------------------------
guardian_exec_proposal() {
    local title="$1"
    local inner_path="$2"
    local output_path="$3"
    local inner_json
    inner_json=$(cat "$inner_path")
    cat > "$output_path" <<EOF
{
  "messages": [
    {
      "@type": "/sparkdream.guardian.v1.MsgExec",
      "authority": "$GOV_ADDR",
      "inner": $inner_json
    }
  ],
  "deposit": "100000000${BOND_DENOM}",
  "title": "$title",
  "summary": "Guardian e2e: $title"
}
EOF
}
