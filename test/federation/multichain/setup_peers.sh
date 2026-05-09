#!/bin/bash
# ------------------------------------------------------------------
# Register and activate IBC peers on both chains.
#
# This script:
#   1. Registers fedtest-b as a SPARK_DREAM peer on chain-a
#   2. Registers fedtest-a as a SPARK_DREAM peer on chain-b
#   3. Activates both peers (they start as PENDING)
#
# Uses governance proposals via Commons Council.
#
# Prerequisites:
#   - Both chains running (start_chains.sh)
#   - IBC channel established (setup_ibc.sh)
#   - .ibc_channels file exists
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/lib_multichain.sh"

if [ ! -f "$IBC_ENV" ]; then
    echo "ERROR: .ibc_channels not found. Run setup_ibc.sh first."
    exit 1
fi
source "$IBC_ENV"

PROP_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROP_DIR"

echo "=================================================="
echo "  PEER REGISTRATION: CROSS-CHAIN IBC PEERS"
echo "=================================================="
echo ""

# ------------------------------------------------------------------
# 1. Get council/committee policy addresses
# ------------------------------------------------------------------
echo "=== Looking up governance policies ==="

CC_POLICY_A=$(qcli_a commons get-group "Commons Council" | jq -r '.group.policy_address // empty')
CC_POLICY_B=$(qcli_b commons get-group "Commons Council" | jq -r '.group.policy_address // empty')

if [ -z "$CC_POLICY_A" ]; then
    echo "ERROR: Commons Council not found on chain-a"
    qcli_a commons get-group "Commons Council" | head -5
    exit 1
fi
if [ -z "$CC_POLICY_B" ]; then
    echo "ERROR: Commons Council not found on chain-b"
    exit 1
fi

echo "  Commons Council A: $CC_POLICY_A"
echo "  Commons Council B: $CC_POLICY_B"
echo "  Channel A: $CHANNEL_A"
echo "  Channel B: $CHANNEL_B"
echo ""

# ------------------------------------------------------------------
# 2. Register fedtest-b as peer on chain-a
# ------------------------------------------------------------------
echo "=== Registering peer 'fedtest-b' on chain-a ==="

cat > "$PROP_DIR/register_peer_b.json" <<EOF
{
  "policy_address": "$CC_POLICY_A",
  "messages": [{
    "@type": "/sparkdream.federation.v1.MsgRegisterPeer",
    "authority": "$CC_POLICY_A",
    "peer_id": "fedtest-b",
    "type": "PEER_TYPE_SPARK_DREAM",
    "display_name": "",
    "ibc_channel_id": "$CHANNEL_A"
  }],
  "metadata": "Register chain-b as IBC peer"
}
EOF

submit_ops_proposal_a "$PROP_DIR/register_peer_b.json" "register peer fedtest-b"

# ------------------------------------------------------------------
# 3. Activate peer on chain-a (PENDING -> ACTIVE)
# ------------------------------------------------------------------
echo ""
echo "=== Activating peer 'fedtest-b' on chain-a ==="

cat > "$PROP_DIR/activate_peer_b.json" <<EOF
{
  "policy_address": "$CC_POLICY_A",
  "messages": [{
    "@type": "/sparkdream.federation.v1.MsgResumePeer",
    "authority": "$CC_POLICY_A",
    "peer_id": "fedtest-b"
  }],
  "metadata": "Activate chain-b peer"
}
EOF

submit_ops_proposal_a "$PROP_DIR/activate_peer_b.json" "activate peer fedtest-b"

# Verify
PEER_A_STATUS=$(qcli_a federation get-peer fedtest-b | jq -r '.peer.status // "PEER_STATUS_PENDING"')
echo "  chain-a peer 'fedtest-b' status: $PEER_A_STATUS"

# ------------------------------------------------------------------
# 4. Register fedtest-a as peer on chain-b
# ------------------------------------------------------------------
echo ""
echo "=== Registering peer 'fedtest-a' on chain-b ==="

cat > "$PROP_DIR/register_peer_a.json" <<EOF
{
  "policy_address": "$CC_POLICY_B",
  "messages": [{
    "@type": "/sparkdream.federation.v1.MsgRegisterPeer",
    "authority": "$CC_POLICY_B",
    "peer_id": "fedtest-a",
    "type": "PEER_TYPE_SPARK_DREAM",
    "display_name": "",
    "ibc_channel_id": "$CHANNEL_B"
  }],
  "metadata": "Register chain-a as IBC peer"
}
EOF

submit_ops_proposal_b "$PROP_DIR/register_peer_a.json" "register peer fedtest-a"

# ------------------------------------------------------------------
# 5. Activate peer on chain-b (PENDING -> ACTIVE)
# ------------------------------------------------------------------
echo ""
echo "=== Activating peer 'fedtest-a' on chain-b ==="

cat > "$PROP_DIR/activate_peer_a.json" <<EOF
{
  "policy_address": "$CC_POLICY_B",
  "messages": [{
    "@type": "/sparkdream.federation.v1.MsgResumePeer",
    "authority": "$CC_POLICY_B",
    "peer_id": "fedtest-a"
  }],
  "metadata": "Activate chain-a peer"
}
EOF

submit_ops_proposal_b "$PROP_DIR/activate_peer_a.json" "activate peer fedtest-a"

# Verify
PEER_B_STATUS=$(qcli_b federation get-peer fedtest-a | jq -r '.peer.status // "PEER_STATUS_PENDING"')
echo "  chain-b peer 'fedtest-a' status: $PEER_B_STATUS"

echo ""
echo "=================================================="
echo "  PEER SETUP COMPLETE"
echo "=================================================="
echo ""
echo "  chain-a → fedtest-b: $PEER_A_STATUS"
echo "  chain-b → fedtest-a: $PEER_B_STATUS"
echo ""
