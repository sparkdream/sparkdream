#!/bin/bash
# ------------------------------------------------------------------
# Configure peer policies on both chains so content and identity
# can flow between them.
#
# Sets:
#   - Inbound content types: blog_post, blog_reply (on both chains)
#   - Outbound content types: blog_post, blog_reply (on both chains)
#   - Accepts reputation attestations
#   - No blocked identities (for now)
#   - Min outbound trust level: 0 (no restriction)
#
# Uses governance proposals via Operations Committee.
#
# Prerequisites:
#   - Both chains running + peers registered (setup_peers.sh)
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/lib_multichain.sh"

PROP_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROP_DIR"

echo "=================================================="
echo "  PEER POLICY SETUP"
echo "=================================================="
echo ""

# ------------------------------------------------------------------
# 1. Get Operations Committee policies
# ------------------------------------------------------------------
OPS_POLICY_A=$(qcli_a commons get-group "Commons Operations Committee" | jq -r '.group.policy_address // empty')
OPS_POLICY_B=$(qcli_b commons get-group "Commons Operations Committee" | jq -r '.group.policy_address // empty')

if [ -z "$OPS_POLICY_A" ]; then
    echo "ERROR: Operations Committee not found on chain-a"
    exit 1
fi
if [ -z "$OPS_POLICY_B" ]; then
    echo "ERROR: Operations Committee not found on chain-b"
    exit 1
fi

echo "  Ops Committee A: $OPS_POLICY_A"
echo "  Ops Committee B: $OPS_POLICY_B"
echo ""

# ------------------------------------------------------------------
# 2. Set policy on chain-a for fedtest-b
# ------------------------------------------------------------------
echo "=== Setting policy on chain-a for peer 'fedtest-b' ==="

cat > "$PROP_DIR/policy_a_for_b.json" <<EOF
{
  "policy_address": "$OPS_POLICY_A",
  "messages": [{
    "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
    "authority": "$OPS_POLICY_A",
    "peer_id": "fedtest-b",
    "policy": {
      "peer_id": "fedtest-b",
      "inbound_content_types": ["blog_post", "blog_reply"],
      "outbound_content_types": ["blog_post", "blog_reply"],
      "min_outbound_trust_level": 0,
      "allow_reputation_queries": true,
      "accept_reputation_attestations": true,
      "blocked_identities": []
    }
  }],
  "metadata": "Set content sharing policy for fedtest-b"
}
EOF

submit_ops_proposal_a "$PROP_DIR/policy_a_for_b.json" "policy a->b"

# Verify
echo "  Verifying chain-a policy for fedtest-b..."
POLICY_A=$(qcli_a federation get-peer-policy fedtest-b 2>/dev/null | jq -r '.policy.inbound_content_types // empty')
echo "    Inbound types: $POLICY_A"

# ------------------------------------------------------------------
# 3. Set policy on chain-b for fedtest-a
# ------------------------------------------------------------------
echo ""
echo "=== Setting policy on chain-b for peer 'fedtest-a' ==="

cat > "$PROP_DIR/policy_b_for_a.json" <<EOF
{
  "policy_address": "$OPS_POLICY_B",
  "messages": [{
    "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
    "authority": "$OPS_POLICY_B",
    "peer_id": "fedtest-a",
    "policy": {
      "peer_id": "fedtest-a",
      "inbound_content_types": ["blog_post", "blog_reply"],
      "outbound_content_types": ["blog_post", "blog_reply"],
      "min_outbound_trust_level": 0,
      "allow_reputation_queries": true,
      "accept_reputation_attestations": true,
      "blocked_identities": []
    }
  }],
  "metadata": "Set content sharing policy for fedtest-a"
}
EOF

submit_ops_proposal_b "$PROP_DIR/policy_b_for_a.json" "policy b->a"

echo "  Verifying chain-b policy for fedtest-a..."
POLICY_B=$(qcli_b federation get-peer-policy fedtest-a 2>/dev/null | jq -r '.policy.inbound_content_types // empty')
echo "    Inbound types: $POLICY_B"

echo ""
echo "=================================================="
echo "  POLICY SETUP COMPLETE"
echo "=================================================="
echo ""
