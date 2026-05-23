#!/bin/bash

echo "--- TESTING: FEDERATION MsgUpdateParams via x/gov ---"
#
# MsgUpdateParams is gov-authority only (the full Params blob, NOT the
# subset MsgUpdateOperationalParams accepts). This file:
#   - round-trips the current Params blob (with LegacyDec fields fixed
#     so they don't get double-encoded — see params_test.sh / season's
#     operational_params_test.sh for the same pattern)
#   - mutates ONE field (max_bridges_per_peer) via expedited gov
#   - verifies the change applied
#   - restores the original value
#
# A non-gov authority attempt is also exercised (Operations Committee
# trying to send MsgUpdateParams) to confirm the auth gate rejects it.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "ERROR: .test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then PASS_COUNT=$((PASS_COUNT + 1)); else FAIL_COUNT=$((FAIL_COUNT + 1)); fi
    echo "  => $RESULT"
}

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx "$TXHASH" --output json)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    return 1
}

submit_and_wait() {
    local TX_RES=$1
    local LABEL=${2:-"transaction"}
    TX_OK=false
    local TXHASH
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then echo "  FAIL: $LABEL - no txhash"; TX_RESULT="$TX_RES"; return 1; fi
    local BCODE
    BCODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BCODE" != "0" ] && [ "$BCODE" != "null" ]; then
        echo "  FAIL: $LABEL - broadcast rejected (code=$BCODE)"
        TX_RESULT="$TX_RES"
        return 1
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    if [ $? -ne 0 ]; then return 1; fi
    local CODE
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  FAIL: $LABEL (code=$CODE) raw=$(echo "$TX_RESULT" | jq -r '.raw_log' | head -c 200)"
        return 1
    fi
    TX_OK=true
    return 0
}

# Convert federation Params LegacyDec fields from their internal
# integer representation (e.g., 500000000000000000 = 0.5) back to a
# normal decimal string before round-tripping into a message body.
# The three LegacyDec fields on federation Params are:
#   trust_discount_rate, min_verifier_accuracy, operator_reward_share
fix_legacy_dec_fields() {
    local json_input="$1"
    echo "$json_input" | python3 -c "
import json, sys
d = json.load(sys.stdin)
DEC_FIELDS = ['trust_discount_rate', 'min_verifier_accuracy', 'operator_reward_share']
for f in DEC_FIELDS:
    if f in d and d[f] is not None:
        s = str(d[f])
        s = s.replace('.', '')
        if len(s) <= 18:
            s = s.zfill(19)
        int_part = s[:-18]
        dec_part = s[-18:]
        int_part = int_part.lstrip('0') or '0'
        d[f] = int_part + '.' + dec_part
json.dump(d, sys.stdout)
"
}

GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov module address: $GOV_ADDR"
echo "Operations Committee: $OPS_POLICY"
echo ""

# ========================================================================
# Read the current params and capture the original max_bridges_per_peer
# for restoration.
# ========================================================================
PARAMS=$($BINARY query federation params --output json 2>/dev/null)
ORIG_MAX=$(echo "$PARAMS" | jq -r '.params.max_bridges_per_peer')
echo "Original max_bridges_per_peer: $ORIG_MAX"

NEW_MAX=$((ORIG_MAX - 1))
echo "Target max_bridges_per_peer:   $NEW_MAX"
echo ""

# ========================================================================
# TEST 1: gov-authority MsgUpdateParams happy path
# Build a full Params blob (round-tripping current values, with
# LegacyDec fixup), bump max_bridges_per_peer, submit via expedited gov.
# ========================================================================
echo "============================================================"
echo "TEST 1: gov MsgUpdateParams (expedited)"
echo "============================================================"

# Fix LegacyDec internal-rep → decimal string
PARAMS_FIXED=$(fix_legacy_dec_fields "$(echo "$PARAMS" | jq '.params')")

# Apply the single mutation
PARAMS_NEW=$(echo "$PARAMS_FIXED" | jq --arg v "$NEW_MAX" '.max_bridges_per_peer = $v')

# Build the gov proposal
jq -n \
    --arg auth "$GOV_ADDR" \
    --argjson p "$PARAMS_NEW" '
{
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdateParams",
      "authority": $auth,
      "params": $p
    }
  ],
  "deposit": "100000000'"$BOND_DENOM"'",
  "title": "Update federation max_bridges_per_peer",
  "summary": "Test: gov MsgUpdateParams round-trip, bump max_bridges_per_peer by -1",
  "expedited": true
}
' > "$PROPOSAL_DIR/gov_update_params.json"

TX_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_update_params.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if ! submit_and_wait "$TX_RES" "gov submit-proposal"; then
    record_result "Gov MsgUpdateParams" "FAIL (submit)"
else
    PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"' | head -n 1)
    if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
        record_result "Gov MsgUpdateParams" "FAIL (no proposal_id)"
    else
        echo "  Proposal: $PROP_ID"
        for VOTER in alice bob; do
            $BINARY tx gov vote "$PROP_ID" yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json > /dev/null 2>&1
            sleep 3
        done

        echo "  Waiting 45s for expedited voting period..."
        sleep 45

        PSTATUS=$($BINARY query gov proposal "$PROP_ID" --output json 2>/dev/null | jq -r '.proposal.status')
        echo "  Status: $PSTATUS"

        if [ "$PSTATUS" == "PROPOSAL_STATUS_PASSED" ]; then
            sleep 5
            UPDATED=$($BINARY query federation params --output json | jq -r '.params.max_bridges_per_peer')
            if [ "$UPDATED" == "$NEW_MAX" ]; then
                echo "  max_bridges_per_peer is now $UPDATED"
                record_result "Gov MsgUpdateParams happy path" "PASS"
            else
                echo "  Expected $NEW_MAX, got $UPDATED"
                record_result "Gov MsgUpdateParams happy path" "FAIL"
            fi
        else
            record_result "Gov MsgUpdateParams happy path" "FAIL (proposal did not pass)"
        fi
    fi
fi

# ========================================================================
# TEST 2: Restore original max_bridges_per_peer via a second gov proposal
# (don't pollute later tests with a tightened kill-switch).
# ========================================================================
echo ""
echo "============================================================"
echo "TEST 2: Restore original max_bridges_per_peer"
echo "============================================================"

PARAMS=$($BINARY query federation params --output json 2>/dev/null)
PARAMS_FIXED=$(fix_legacy_dec_fields "$(echo "$PARAMS" | jq '.params')")
PARAMS_RESTORE=$(echo "$PARAMS_FIXED" | jq --arg v "$ORIG_MAX" '.max_bridges_per_peer = $v')

jq -n \
    --arg auth "$GOV_ADDR" \
    --argjson p "$PARAMS_RESTORE" '
{
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdateParams",
      "authority": $auth,
      "params": $p
    }
  ],
  "deposit": "100000000'"$BOND_DENOM"'",
  "title": "Restore federation max_bridges_per_peer",
  "summary": "Test cleanup: restore max_bridges_per_peer to original value",
  "expedited": true
}
' > "$PROPOSAL_DIR/gov_restore_params.json"

TX_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_restore_params.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "gov restore"; then
    PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"' | head -n 1)
    for VOTER in alice bob; do
        $BINARY tx gov vote "$PROP_ID" yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json > /dev/null 2>&1
        sleep 3
    done
    echo "  Waiting 45s for expedited voting period..."
    sleep 45
    sleep 5
    RESTORED=$($BINARY query federation params --output json | jq -r '.params.max_bridges_per_peer')
    if [ "$RESTORED" == "$ORIG_MAX" ]; then
        echo "  max_bridges_per_peer restored to $RESTORED"
        record_result "Restore params" "PASS"
    else
        echo "  Expected $ORIG_MAX, got $RESTORED"
        record_result "Restore params" "FAIL"
    fi
else
    record_result "Restore params" "FAIL"
fi

# ========================================================================
# TEST 3: Operations Committee MsgUpdateParams rejected (auth gate).
# OpsComm can submit MsgUpdateOperationalParams but NOT MsgUpdateParams.
# We construct a proposal containing MsgUpdateParams with OpsComm
# authority and verify the inner execution fails (the gov-authority
# check rejects).
# ========================================================================
echo ""
echo "============================================================"
echo "TEST 3: OpsComm MsgUpdateParams rejected"
echo "============================================================"

PARAMS=$($BINARY query federation params --output json 2>/dev/null)
PARAMS_FIXED=$(fix_legacy_dec_fields "$(echo "$PARAMS" | jq '.params')")
BAD_TARGET=$((ORIG_MAX + 100))
PARAMS_BAD=$(echo "$PARAMS_FIXED" | jq --arg v "$BAD_TARGET" '.max_bridges_per_peer = $v')

jq -n \
    --arg policy "$OPS_POLICY" \
    --argjson p "$PARAMS_BAD" '
{
  "policy_address": $policy,
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdateParams",
      "authority": $policy,
      "params": $p
    }
  ],
  "metadata": "OpsComm trying to update federation Params — should be rejected"
}
' > "$PROPOSAL_DIR/ops_update_params.json"

TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/ops_update_params.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json)

# Either of two failure paths is acceptable: x/commons rejects the
# proposal at SUBMIT time via its AllowedMessages check (unauthorized,
# this is the post-Phase-1 behavior), OR the proposal is submitted but
# its inner MsgUpdateParams rejects at EXECUTE time because authority
# != x/gov. Both prove the auth gate held. The only failure mode is
# `max_bridges_per_peer` actually changing to BAD_TARGET.
SUBMIT_RC=0
submit_and_wait "$TX_RES" "ops attempt submit" || SUBMIT_RC=$?

if [ $SUBMIT_RC -eq 0 ]; then
    PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
    if [ -n "$PROP_ID" ]; then
        for VOTER in alice bob; do
            $BINARY tx commons vote-proposal "$PROP_ID" yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json > /dev/null 2>&1
            sleep 2
        done
        $BINARY tx commons execute-proposal "$PROP_ID" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --gas 2000000 --output json > /dev/null 2>&1
        sleep 6
    fi
else
    echo "  Submit-time AllowedMessages check rejected (commons enforces auth gate up-front)"
fi

# Confirm BAD_TARGET never took effect either way.
AFTER=$($BINARY query federation params --output json | jq -r '.params.max_bridges_per_peer')
if [ "$AFTER" == "$ORIG_MAX" ]; then
    echo "  max_bridges_per_peer unchanged ($AFTER); OpsComm MsgUpdateParams rejected"
    record_result "OpsComm MsgUpdateParams rejected" "PASS"
else
    echo "  ERROR: max_bridges_per_peer is now $AFTER (expected $ORIG_MAX)"
    record_result "OpsComm MsgUpdateParams rejected" "FAIL"
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "GOVERNANCE PARAMS TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-55s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
    exit 0
fi
