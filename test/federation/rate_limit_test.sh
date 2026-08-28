#!/bin/bash

# ============================================================================
# FEDERATION RATE-LIMIT E2E TEST
# ============================================================================
# Verifies per-peer sliding-window inbound rate-limit enforcement
# (spec §10.2) on MsgSubmitFederatedContent.
#
# Coverage:
#   1. Below the limit: submissions succeed.
#   2. At the limit + 1: submission is rejected with the
#      federation rate-limit error (code 2311).
#   3. Limit disabled (set to 0): submissions resume.
#
# Uses a dedicated test peer ("rl-test.example") so the counter starts
# fresh — running this after content_federation_test would otherwise
# inherit a partly-full window on mastodon.example.
#
# The global per-block cap (max_inbound_per_block) is covered by the
# Go unit tests in x/federation/keeper/rate_limit_test.go; reliably
# racing multiple txs into one block from a shell is too flaky to be
# worth wiring up here.
# ============================================================================

echo "--- TESTING: FEDERATION RATE LIMIT ENFORCEMENT ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "ERROR: .test_env not found."; exit 1
fi
source "$SCRIPT_DIR/.test_env"
source "$SCRIPT_DIR/peer_fixtures.sh"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1; local RESULT=$2
    TEST_NAMES+=("$NAME"); RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then PASS_COUNT=$((PASS_COUNT + 1)); else FAIL_COUNT=$((FAIL_COUNT + 1)); fi
    echo "  => $RESULT"
}

wait_for_tx() {
    local TXHASH=$1; local MAX=20; local A=0
    while [ $A -lt $MAX ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>/dev/null)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        A=$((A + 1)); sleep 1
    done
    return 1
}

# submit_and_wait_capture: like submit_and_wait but does NOT short-circuit
# on a non-zero tx code — the caller wants to inspect the failure.
submit_and_wait_capture() {
    local TX_RES=$1
    TX_RESULT=""
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then TX_RESULT="$TX_RES"; return 1; fi
    local BCODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BCODE" != "0" ] && [ "$BCODE" != "null" ]; then
        # check_tx rejection — return the broadcast envelope, caller
        # inspects .code / .raw_log.
        TX_RESULT="$TX_RES"
        return 0
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    if [ $? -ne 0 ]; then return 1; fi
    return 0
}

get_commons_proposal_id() {
    echo "$1" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"'
}

vote_and_execute_ops() {
    local PROP_ID=$1
    for VOTER in "alice" "bob"; do
        local S=$($BINARY query commons get-proposal $PROP_ID --output json 2>/dev/null | jq -r '.proposal.status')
        if [ "$S" == "PROPOSAL_STATUS_ACCEPTED" ] || [ "$S" == "PROPOSAL_STATUS_EXECUTED" ]; then continue; fi
        $BINARY tx commons vote-proposal $PROP_ID yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json > /dev/null 2>&1
        sleep 2
    done
    $BINARY tx commons execute-proposal $PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --gas 2000000 --output json > /dev/null 2>&1
    sleep 6
}

submit_ops_proposal() {
    local FILE=$1; local LABEL=${2:-"proposal"}
    echo "  Submitting $LABEL..."
    local TX_RES=$($BINARY tx commons submit-proposal "$FILE" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json)
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then echo "  No txhash from broadcast"; return 1; fi
    sleep 6
    local CHECK=$(wait_for_tx "$TXHASH")
    local CODE=$(echo "$CHECK" | jq -r '.code')
    if [ "$CODE" != "0" ]; then echo "  Proposal tx failed (code=$CODE)"; return 1; fi
    local PROP_ID=$(get_commons_proposal_id "$CHECK")
    if [ -z "$PROP_ID" ]; then echo "  No proposal ID"; return 1; fi
    echo "  Proposal ID: $PROP_ID"
    vote_and_execute_ops "$PROP_ID"
}

sha256_base64() {
    echo -n "$1" | sha256sum | awk '{print $1}' | xxd -r -p | base64 -w0
}

# update_inbound_limit <peer_id> <limit>
# Updates a peer's inbound_rate_limit_per_epoch via OpsComm. All other
# policy fields are preserved by reading the current policy first.
update_inbound_limit() {
    local PEER_ID=$1
    local LIMIT=$2

    local CURRENT=$($BINARY query federation get-peer-policy "$PEER_ID" --output json)
    local IN_TYPES=$(echo "$CURRENT" | jq -c '.policy.inbound_content_types // []')
    local OUT_TYPES=$(echo "$CURRENT" | jq -c '.policy.outbound_content_types // []')
    local MIN_TRUST=$(echo "$CURRENT" | jq -r '.policy.min_outbound_trust_level // "0"')
    local OUT_LIMIT=$(echo "$CURRENT" | jq -r '.policy.outbound_rate_limit_per_epoch // "0"')
    local ALLOW_REP=$(echo "$CURRENT" | jq -r 'if .policy.allow_reputation_queries then "true" else "false" end')
    local ACCEPT_REP=$(echo "$CURRENT" | jq -r 'if .policy.accept_reputation_attestations then "true" else "false" end')
    local MAX_TRUST=$(echo "$CURRENT" | jq -r '.policy.max_trust_credit // "0"')
    local REVIEW=$(echo "$CURRENT" | jq -r 'if .policy.require_review then "true" else "false" end')
    local BLOCKED=$(echo "$CURRENT" | jq -c '.policy.blocked_identities // []')

    local PROP_FILE="$PROPOSAL_DIR/ratelimit_set_${PEER_ID//./_}_${LIMIT}.json"
    cat > "$PROP_FILE" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
      "authority": "$OPS_POLICY",
      "peer_id": "$PEER_ID",
      "policy": {
        "peer_id": "$PEER_ID",
        "outbound_content_types": $OUT_TYPES,
        "inbound_content_types": $IN_TYPES,
        "min_outbound_trust_level": $MIN_TRUST,
        "inbound_rate_limit_per_epoch": $LIMIT,
        "outbound_rate_limit_per_epoch": $OUT_LIMIT,
        "allow_reputation_queries": $ALLOW_REP,
        "accept_reputation_attestations": $ACCEPT_REP,
        "max_trust_credit": $MAX_TRUST,
        "require_review": $REVIEW,
        "blocked_identities": $BLOCKED
      }
    }
  ],
  "metadata": "Rate-limit test: set inbound limit to $LIMIT"
}
EOF
    submit_ops_proposal "$PROP_FILE" "set inbound limit $LIMIT for $PEER_ID"

    local APPLIED=$($BINARY query federation get-peer-policy "$PEER_ID" --output json | jq -r '.policy.inbound_rate_limit_per_epoch // "0"')
    if [ "$APPLIED" != "$LIMIT" ]; then
        echo "  WARN: policy update — expected $LIMIT, got $APPLIED"
        return 1
    fi
    echo "  Policy applied: inbound_rate_limit_per_epoch=$APPLIED"
    return 0
}

# submit_rl_content <peer_id> <operator_key> <remote_id> <body>
# Returns the broadcast JSON; caller inspects via submit_and_wait_capture.
submit_rl_content() {
    local PEER_ID=$1
    local OP_KEY=$2
    local REMOTE_ID=$3
    local BODY=$4
    local HASH=$(sha256_base64 "$BODY")

    $BINARY tx federation submit-federated-content \
        "$PEER_ID" \
        "$REMOTE_ID" \
        "blog_post" \
        "@u@$PEER_ID" \
        "RL User" \
        "rl-title-$REMOTE_ID" \
        "$BODY" \
        "" \
        "1700000000" \
        --content-hash "$HASH" \
        --from "$OP_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json
}

# is_rate_limit_error: returns 0 if $1 looks like the federation
# rate-limit module error (code 2311 OR raw_log mentions "rate limit").
is_rate_limit_error() {
    local TX_JSON=$1
    local CODE=$(echo "$TX_JSON" | jq -r '.code // "0"')
    local RAW=$(echo "$TX_JSON" | jq -r '.raw_log // ""')
    if [ "$CODE" = "2311" ]; then return 0; fi
    if echo "$RAW" | grep -qi "rate limit"; then return 0; fi
    return 1
}

echo "Operator2: $OPERATOR2_ADDR"
echo ""

# ========================================================================
# SETUP: dedicated peer + bridge so the counter starts at zero
# ========================================================================
echo "=== Setting up isolated rate-limit test peer ==="

PEER_ID="rl-test.example"
register_test_peer "$PEER_ID" "PEER_TYPE_ACTIVITYPUB" "Rate-limit test peer" ""
activate_test_peer "$PEER_ID"
# set_peer_policy seeds inbound_rate_limit_per_epoch=100 by default;
# the tests below tighten it via update_inbound_limit.
set_peer_policy "$PEER_ID" "blog_post" "" "" "false" "false" || true

# Register operator2 as a bridge for the new peer. If operator2 already
# has an ACTIVE service.Operator under federation-bridge-activitypub
# (likely, from bridge_operator_test), passing stake=0 reuses the
# existing bond per the multi-binding-per-operator model.
SVC_TYPE="federation-bridge-activitypub"
# Note: 2>/dev/null (not 2>&1) — when operator2 has no service.Operator or
# no binding yet, the CLI prints a "not found" error to stderr. Merging it
# into jq's stdin produces a "jq: parse error". Dropping stderr lets jq see
# empty input so the `// empty` fallback yields "" cleanly.
OP_STATUS=$($BINARY query service operator "$OPERATOR2_ADDR" $SVC_TYPE --output json 2>/dev/null | jq -r '.operator.status // empty')
BINDING_ADDR=$($BINARY query federation get-bridge-binding "$OPERATOR2_ADDR" "$PEER_ID" --output json 2>/dev/null | jq -r '.bridge_binding.address // empty')

if [ -z "$BINDING_ADDR" ]; then
    if [ "$OP_STATUS" == "OPERATOR_STATUS_ACTIVE" ]; then
        STAKE_FOR_REG="0"
    else
        STAKE_FOR_REG=$($BINARY query service service-type $SVC_TYPE --output json 2>/dev/null | jq -r '.config.min_bond_amount // "1000000000"')
    fi
    echo "  Registering operator2 bridge for $PEER_ID (stake=$STAKE_FOR_REG)..."
    TX_RES=$($BINARY tx federation register-bridge \
        "$PEER_ID" activitypub "https://bridge.example/rl" "$STAKE_FOR_REG" \
        --from operator2 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)
    submit_and_wait_capture "$TX_RES" || true
    BINDING_ADDR=$($BINARY query federation get-bridge-binding "$OPERATOR2_ADDR" "$PEER_ID" --output json 2>/dev/null | jq -r '.bridge_binding.address // empty')
fi

if [ -z "$BINDING_ADDR" ]; then
    echo "  Bridge binding for $PEER_ID could not be created — aborting"
    exit 1
fi
echo "  Bridge binding for $PEER_ID: present"
echo ""

# ========================================================================
# TESTS 1 + 2: Tighten limit to 2 and submit 3 messages.
#
# The serial submit_and_wait_capture pattern (sleep 6s + poll per tx)
# takes 20-30s for three submissions, which races the 30s sliding
# window in testparams — if tx 3 lands in a new window the count
# resets to 0 and the smoothed `prevCount * remaining/window` slack
# lets it through. Async / mempool tricks to compress wall-time hit
# CLI account-sequence inference races. The robust fix is to bump
# `rate_limit_window` via gov to 5 minutes for the duration of this
# test, run the serial test inside that window, then restore.
# ========================================================================
echo "--- Pre-test: bump rate_limit_window to 5m via gov ---"

GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
ORIG_PARAMS=$($BINARY query federation params --output json | jq '.params')
ORIG_WINDOW=$(echo "$ORIG_PARAMS" | jq -r '.rate_limit_window')
echo "  Original rate_limit_window: $ORIG_WINDOW"

# Rewrite LegacyDec internal-rep ("500000000000000000") to a parseable
# decimal string ("0.500000000000000000") so MsgUpdateParams round-trips.
# Match the helper in governance_params_test.sh.
fix_legacy_dec_fields() {
    local json_input="$1"
    echo "$json_input" | python3 -c "
import json, sys
d = json.load(sys.stdin)
DEC_FIELDS = ['trust_discount_rate', 'operator_reward_inflation_share',
              'operator_reward_pool_overflow_burn_ratio', 'max_unverified_rate']
for f in DEC_FIELDS:
    if f in d and d[f] is not None:
        s = str(d[f]).replace('.', '')
        if len(s) <= 18:
            s = s.zfill(19)
        int_part = s[:-18]; dec_part = s[-18:]
        int_part = int_part.lstrip('0') or '0'
        d[f] = int_part + '.' + dec_part
json.dump(d, sys.stdout)
"
}

PARAMS_FIXED=$(fix_legacy_dec_fields "$ORIG_PARAMS")
PARAMS_NEW=$(echo "$PARAMS_FIXED" | jq '.rate_limit_window = "300s"')

mkdir -p "$PROPOSAL_DIR"
jq -n --arg auth "$GOV_ADDR" --argjson p "$PARAMS_NEW" '
{
  "messages": [{"@type": "/sparkdream.federation.v1.MsgUpdateParams", "authority": $auth, "params": $p}],
  "deposit": "100000000'"$BOND_DENOM"'",
  "title": "Bump rate_limit_window for rate_limit_test",
  "summary": "Widen window to keep 3 sequential submissions inside it.",
  "expedited": true
}' > "$PROPOSAL_DIR/rl_bump_window.json"

RL_WINDOW_OK=false
TX_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/rl_bump_window.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)
if submit_and_wait_capture "$TX_RES"; then
    PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"' | head -n1)
    if [ -n "$PROP_ID" ] && [ "$PROP_ID" != "null" ]; then
        echo "  Gov proposal $PROP_ID; voting..."
        for VOTER in alice bob; do
            $BINARY tx gov vote "$PROP_ID" yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json > /dev/null 2>&1
            sleep 3
        done
        echo "  Waiting 45s for expedited voting..."
        sleep 45
        PSTATUS=$($BINARY query gov proposal "$PROP_ID" --output json 2>/dev/null | jq -r '.proposal.status')
        NEW_WIN=$($BINARY query federation params --output json | jq -r '.params.rate_limit_window')
        if [ "$PSTATUS" = "PROPOSAL_STATUS_PASSED" ] && [ "$NEW_WIN" = "5m0s" ]; then
            echo "  rate_limit_window now $NEW_WIN"
            RL_WINDOW_OK=true
        else
            echo "  WARN: bump failed (status=$PSTATUS, window=$NEW_WIN) — proceeding anyway"
        fi
    fi
fi
echo ""

echo "--- TEST 1: Submit up to limit ---"

if ! update_inbound_limit "$PEER_ID" 2; then
    record_result "Tighten inbound limit to 2" "FAIL"
    exit 1
fi
record_result "Tighten inbound limit to 2" "PASS"

# Align so all 3 submissions land inside ONE fresh window (count starts
# at 0, ~240s+ of headroom). We must NOT use a wall-clock `sleep` to hit
# a chain-time boundary: under parallel-suite load the chain runs behind
# wall-clock (block production drifts to ~2s/block), so sleeping N
# wall-seconds advances chain-time by less and strands the first
# submission in the *previous* window. That splits the counter (1 prev +
# 1 current) and the smoothed prevCount*remaining/window slack lets sub3
# through (~4% of runs). Instead, POLL the chain's block_time until it's
# safely inside a fresh window before submitting.
WIN_S=300  # we bumped rate_limit_window to 5m above
echo "  Polling chain block_time for a fresh ${WIN_S}s window (no wall-clock sleep)..."
POS=-1
for _ in $(seq 1 120); do
    BT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_time' | sed 's/T/ /; s/\.[0-9]*Z$//')
    BT_EPOCH=$(date -d "$BT UTC" +%s 2>/dev/null || echo 0)
    POS=$(( BT_EPOCH % WIN_S ))
    # Safe zone: at least 5s past a boundary (a fresh window — this is a
    # dedicated peer, so its count is 0) and >=150s of headroom for the
    # three serial submissions (each ~6-10s incl. poll).
    if [ "$POS" -ge 5 ] && [ "$POS" -le 150 ] 2>/dev/null; then break; fi
    sleep 3
done
echo "  Aligned: chain block_time=$BT (pos=${POS}s into ${WIN_S}s window)"

# Diagnostic: print params and block-time at each step so we can see
# whether the rate-limit counter actually persists between submissions.
echo "  Pre-test diagnostic:"
echo "    rate_limit_window: $($BINARY query federation params -o json | jq -r '.params.rate_limit_window')"
echo "    block_time:        $($BINARY status 2>&1 | jq -r '.sync_info.latest_block_time')"
echo "    block_height:      $($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')"

# First submission — must succeed.
TX1=$(submit_rl_content "$PEER_ID" operator2 "rl-1" "first content under limit")
submit_and_wait_capture "$TX1"
CODE1=$(echo "$TX_RESULT" | jq -r '.code // "0"')
TS1=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_time')
echo "  sub1: code=$CODE1 at $TS1"
if [ "$CODE1" = "0" ]; then
    record_result "Submit #1 within limit" "PASS"
else
    echo "  Unexpected failure (code=$CODE1, raw=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' | head -c 200))"
    record_result "Submit #1 within limit" "FAIL"
fi

# Second submission — must succeed (still at-or-below limit).
TX2=$(submit_rl_content "$PEER_ID" operator2 "rl-2" "second content under limit")
submit_and_wait_capture "$TX2"
CODE2=$(echo "$TX_RESULT" | jq -r '.code // "0"')
TS2=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_time')
echo "  sub2: code=$CODE2 at $TS2"
if [ "$CODE2" = "0" ]; then
    record_result "Submit #2 within limit" "PASS"
else
    echo "  Unexpected failure (code=$CODE2, raw=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' | head -c 200))"
    record_result "Submit #2 within limit" "FAIL"
fi

# ========================================================================
# TEST 2: One past the limit — must be rejected
# ========================================================================
echo ""
echo "--- TEST 2: Submit past limit (rejection expected) ---"

TX3=$(submit_rl_content "$PEER_ID" operator2 "rl-3" "third content over limit")
submit_and_wait_capture "$TX3"
TS3=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_time')
CODE3=$(echo "$TX_RESULT" | jq -r '.code // "0"')
RAW3=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
echo "  sub3: code=$CODE3 at $TS3 raw=$(echo "$RAW3" | head -c 120)"

if is_rate_limit_error "$TX_RESULT"; then
    echo "  Got expected rate-limit rejection (code=$CODE3)"
    record_result "Reject submission past limit" "PASS"
else
    echo "  Expected rate-limit rejection; got code=$CODE3 raw=$(echo "$RAW3" | head -c 200)"
    # Diagnostic dump: blockchain state at time of test 3
    echo "  DIAG: peer policy:"
    $BINARY query federation get-peer-policy "$PEER_ID" -o json | jq '.policy | {inbound_rate_limit_per_epoch, outbound_rate_limit_per_epoch}' | sed 's/^/    /'
    echo "  DIAG: federation params:"
    $BINARY query federation params -o json | jq '.params | {rate_limit_window, max_inbound_per_block}' | sed 's/^/    /'
    record_result "Reject submission past limit" "FAIL"
fi

# ========================================================================
# TEST 3: Disable limit (=0) and verify submissions resume
# ========================================================================
echo ""
echo "--- TEST 3: Disable limit and submit again ---"

if ! update_inbound_limit "$PEER_ID" 0; then
    record_result "Disable inbound limit" "FAIL"
else
    record_result "Disable inbound limit" "PASS"

    TX4=$(submit_rl_content "$PEER_ID" operator2 "rl-4" "fourth content with limit disabled")
    submit_and_wait_capture "$TX4"
    CODE4=$(echo "$TX_RESULT" | jq -r '.code // "0"')
    if [ "$CODE4" = "0" ]; then
        record_result "Submit succeeds when limit disabled" "PASS"
    else
        echo "  Unexpected failure (code=$CODE4, raw=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' | head -c 200))"
        record_result "Submit succeeds when limit disabled" "FAIL"
    fi
fi

# ========================================================================
# Cleanup: restore a sane limit on the test peer (defensive — the peer
# is dedicated to this test, but leaving it open looks intentional).
# ========================================================================
echo ""
echo "=== Restoring inbound limit to 100 on $PEER_ID ==="
update_inbound_limit "$PEER_ID" 100 || true

# Restore rate_limit_window via gov (only if we bumped it).
if [ "$RL_WINDOW_OK" = "true" ]; then
    echo ""
    echo "=== Restoring rate_limit_window to $ORIG_WINDOW via gov ==="
    PARAMS_NOW=$($BINARY query federation params --output json | jq '.params')
    PARAMS_FIX2=$(fix_legacy_dec_fields "$PARAMS_NOW")
    PARAMS_RESTORED=$(echo "$PARAMS_FIX2" | jq --arg v "$ORIG_WINDOW" '.rate_limit_window = $v')
    jq -n --arg auth "$GOV_ADDR" --argjson p "$PARAMS_RESTORED" '
{
  "messages": [{"@type": "/sparkdream.federation.v1.MsgUpdateParams", "authority": $auth, "params": $p}],
  "deposit": "100000000'"$BOND_DENOM"'",
  "title": "Restore rate_limit_window",
  "summary": "restore",
  "expedited": true
}' > "$PROPOSAL_DIR/rl_restore_window.json"
    TX_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/rl_restore_window.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)
    if submit_and_wait_capture "$TX_RES"; then
        PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"' | head -n1)
        if [ -n "$PROP_ID" ] && [ "$PROP_ID" != "null" ]; then
            for VOTER in alice bob; do
                $BINARY tx gov vote "$PROP_ID" yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json > /dev/null 2>&1
                sleep 3
            done
            echo "  Waiting 45s for expedited voting..."
            sleep 45
        fi
    fi
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "=================================================="
echo "RATE LIMIT TEST RESULTS"
echo "=================================================="
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo "--------------------------------------------------"
echo "  Passed: $PASS_COUNT"
echo "  Failed: $FAIL_COUNT"
echo "=================================================="

if [ $FAIL_COUNT -gt 0 ]; then exit 1; fi
exit 0
