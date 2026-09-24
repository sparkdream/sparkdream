#!/bin/bash

# ============================================================================
# Minimal fixture bootstrap for running verifier_test.sh (or
# no_quorum_settlement_test.sh) on its own.
# ============================================================================
#
# Neither of those suites creates its own peer, policy or bridge -- they
# inherit them from peer_lifecycle_test.sh / peer_policy_test.sh /
# bridge_operator_test.sh, which together cost ~15 minutes. When you only
# want to re-check the verifier path, that is almost all wasted time.
#
# This creates just the three things they actually depend on, using the
# idempotent helpers in peer_fixtures.sh:
#
#   1. mastodon.example registered as a bridge peer and ACTIVE
#   2. its policy allowing blog_post / blog_reply inbound
#   3. operator2 registered as an ACTIVE activitypub bridge on it
#
# Usage:
#   ./setup_test_accounts.sh            # once, writes .test_env
#   ./bootstrap_verifier_fixtures.sh    # ~2-3 min
#   ./verifier_test.sh
#
# Everything here is idempotent, so running it against a chain that already
# has the fixtures is a fast no-op rather than an error.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="${PROPOSAL_DIR:-$SCRIPT_DIR/proposals}"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "ERROR: .test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi
source "$SCRIPT_DIR/.test_env"
# shellcheck disable=SC1091
source "$SCRIPT_DIR/peer_fixtures.sh"

PEER="mastodon.example"

echo "=========================================================="
echo "Bootstrapping verifier fixtures on $PEER"
echo "=========================================================="

echo ""
echo "--- 1/3: register + activate peer ---"
if ! register_test_peer "$PEER" "PEER_TYPE_ACTIVITYPUB" "Example Mastodon instance" "" "yes"; then
    echo "ERROR: could not register/activate $PEER"
    exit 1
fi

echo ""
echo "--- 2/3: inbound content policy ---"
# Bare comma-separated list, NOT JSON: set_peer_policy wraps it into a
# JSON array itself (peer_fixtures.sh), so passing '["a","b"]' here
# double-wraps and the proposal fails to parse.
if ! set_peer_policy "$PEER" "blog_post,blog_reply" "" "" "false" "false"; then
    echo "ERROR: could not set inbound policy on $PEER"
    exit 1
fi

echo ""
echo "--- 3/3: operator2 bridge ---"
if ! register_test_bridge "operator2" "$OPERATOR2_ADDR" "$PEER" "activitypub" \
        "https://bridge.mastodon.example"; then
    echo "ERROR: could not register operator2's bridge on $PEER"
    exit 1
fi

echo ""
echo "=========================================================="
echo "Verification"
echo "=========================================================="
PEER_STATUS=$($BINARY query federation get-peer "$PEER" --output json 2>/dev/null | jq -r '.peer.status // "MISSING"')
INBOUND=$($BINARY query federation get-peer-policy "$PEER" --output json 2>/dev/null | jq -c '.policy.inbound_content_types // []')
SVC=$($BINARY query service operator "$OPERATOR2_ADDR" federation-bridge-activitypub --output json 2>/dev/null | jq -r '.operator.status // "MISSING"')

echo "  peer $PEER:            $PEER_STATUS"
echo "  inbound_content_types: $INBOUND"
echo "  operator2 bridge:      $SVC"

if [ "$PEER_STATUS" == "PEER_STATUS_ACTIVE" ] && \
   [ "$SVC" == "OPERATOR_STATUS_ACTIVE" ] && \
   echo "$INBOUND" | grep -q "blog_post"; then
    echo ""
    echo ">>> Fixtures ready — verifier_test.sh / no_quorum_settlement_test.sh can run <<<"
    exit 0
fi

echo ""
echo ">>> Fixtures INCOMPLETE <<<"
exit 1
