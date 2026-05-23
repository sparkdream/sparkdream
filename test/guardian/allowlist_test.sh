#!/bin/bash
# ============================================================================
# X/GUARDIAN E2E: ALLOWLIST INTROSPECTION + DENY-BY-DEFAULT
# ============================================================================
# Two integration checks for guardian's outer envelope:
#
#   1. `q guardian allowed-msgs` returns the expected 10-entry list. The
#      list is compiled into the binary; an unexpected count signals a
#      spec change that needs review.
#
#   2. A gov proposal whose MsgExec inner is NOT in the allowlist
#      (here: cosmos.bank.v1beta1.MsgSend) is voted through but FAILS
#      at execution with ErrInnerMsgNotAllowed.
#
# The authority gate (alice tries to sign MsgExec directly) is covered by
# the keeper unit tests in x/guardian/keeper/msg_server_test.go
# (TestExecRejectsWrongAuthority) and is not re-tested at the e2e level
# because the only practical e2e path to MsgExec is via a gov proposal,
# whose signer is always the gov module by definition.
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo "=================================================="
echo "TEST: guardian allowlist + deny-by-default"
echo "=================================================="

# ----------------------------------------------------------------------------
# 1. AllowedMsgs query
# ----------------------------------------------------------------------------
echo ""
echo "[1/2] q guardian allowed-msgs"
ALLOWED_JSON=$($BINARY q guardian allowed-msgs --output json)
echo "$ALLOWED_JSON" | jq .

EXPECTED=(
    "/cosmos.auth.v1beta1.MsgUpdateParams"
    "/cosmos.bank.v1beta1.MsgSetSendEnabled"
    "/cosmos.bank.v1beta1.MsgUpdateParams"
    "/cosmos.consensus.v1.MsgUpdateParams"
    "/cosmos.distribution.v1beta1.MsgCommunityPoolSpend"
    "/cosmos.distribution.v1beta1.MsgUpdateParams"
    "/cosmos.gov.v1.MsgUpdateParams"
    "/cosmos.mint.v1beta1.MsgUpdateParams"
    "/cosmos.slashing.v1beta1.MsgUpdateParams"
    "/cosmos.staking.v1beta1.MsgUpdateParams"
)
for url in "${EXPECTED[@]}"; do
    if ! echo "$ALLOWED_JSON" | jq -e --arg u "$url" '.type_urls | index($u)' > /dev/null; then
        echo "FAIL: allowed-msgs missing $url"
        exit 1
    fi
done
COUNT=$(echo "$ALLOWED_JSON" | jq -r '.type_urls | length')
if [ "$COUNT" != "${#EXPECTED[@]}" ]; then
    echo "FAIL: allowed-msgs returned $COUNT entries, expected ${#EXPECTED[@]}"
    echo "      (extra entries indicate a spec change that needs review)"
    exit 1
fi
echo "PASS: allowed-msgs returns all ${#EXPECTED[@]} expected entries"

# ----------------------------------------------------------------------------
# 2. Non-allowlisted inner msg rejected at execution
# ----------------------------------------------------------------------------
echo ""
echo "[2/2] MsgExec with non-allowlisted inner (bank.MsgSend) should FAIL"

cat > "$PROPOSAL_DIR/allowlist_msgsend.json" <<EOF
{
  "messages": [
    {
      "@type": "/sparkdream.guardian.v1.MsgExec",
      "authority": "$GOV_ADDR",
      "inner": {
        "@type": "/cosmos.bank.v1beta1.MsgSend",
        "from_address": "$ALICE_ADDR",
        "to_address": "$BOB_ADDR",
        "amount": [{"denom": "$BOND_DENOM", "amount": "1"}]
      }
    }
  ],
  "deposit": "100000000${BOND_DENOM}",
  "title": "MALICIOUS: route bank.MsgSend through guardian",
  "summary": "MsgSend is not in guardian's allowlist; execution must FAIL."
}
EOF
PROP_NONALLOWED=$(submit_proposal "$PROPOSAL_DIR/allowlist_msgsend.json")
echo "proposal id: $PROP_NONALLOWED"
vote_yes "$PROP_NONALLOWED"

wait_voting
check_status "$PROP_NONALLOWED" "PROPOSAL_STATUS_FAILED" || exit 1

echo ""
echo "TEST PASSED: guardian allowlist + deny-by-default"
