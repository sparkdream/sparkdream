#!/bin/bash
# Shared peer fixture registration for federation tests.
# Sources after .test_env so $COMMONS_POLICY, $CHAIN_ID, $BINARY are set.
#
# Each test using this helper runs from a freshly restored snapshot
# (see test/federation/snapshots/post-setup), so peers from prior tests
# don't carry over. Tests that need pre-existing peers source this and
# call register_test_peer to seed them via Commons Council proposal.
#
# Idempotent: registering an existing peer becomes a no-op (proposal will
# fail at execute time but we tolerate that).

PROPOSAL_DIR="${PROPOSAL_DIR:-$SCRIPT_DIR/proposals}"
mkdir -p "$PROPOSAL_DIR"

# register_test_peer <peer_id> <type> <display_name> [ibc_channel_id]
# type: PEER_TYPE_ACTIVITYPUB | PEER_TYPE_ATPROTO | PEER_TYPE_SPARK_DREAM
register_test_peer() {
    local PEER_ID=$1
    local PEER_TYPE=$2
    local DISPLAY_NAME=$3
    local IBC_CHAN=${4:-""}

    # If peer already exists, skip
    local EXIST=$($BINARY query federation get-peer "$PEER_ID" --output json 2>&1 | jq -r '.peer.id // empty' 2>/dev/null)
    if [ "$EXIST" == "$PEER_ID" ]; then
        echo "  Fixture peer $PEER_ID already registered"
        return 0
    fi

    local PROP_FILE="$PROPOSAL_DIR/fixture_register_${PEER_ID//./_}.json"
    cat > "$PROP_FILE" <<EOF
{
  "policy_address": "$COMMONS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterPeer",
      "authority": "$COMMONS_POLICY",
      "peer_id": "$PEER_ID",
      "display_name": "$DISPLAY_NAME",
      "type": "$PEER_TYPE",
      "ibc_channel_id": "$IBC_CHAN",
      "metadata": "fixture peer for federation tests"
    }
  ],
  "metadata": "Fixture: register $PEER_ID"
}
EOF

    local TX_RES=$($BINARY tx commons submit-proposal "$PROP_FILE" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json 2>&1)
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Fixture peer $PEER_ID submission failed (likely already exists)"
        return 1
    fi
    sleep 6

    local PROP_ID=$($BINARY query tx "$TXHASH" --output json 2>/dev/null | \
        jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
    if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
        echo "  Fixture peer $PEER_ID — could not extract proposal id"
        return 1
    fi

    # Vote YES from each Commons Council member
    for VOTER in alice bob carol; do
        $BINARY tx commons vote-proposal "$PROP_ID" yes \
            --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000000uspark --output json > /dev/null 2>&1
        sleep 2
    done

    # Execute
    $BINARY tx commons execute-proposal "$PROP_ID" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --gas 2000000 --output json > /dev/null 2>&1
    sleep 6

    # Verify
    EXIST=$($BINARY query federation get-peer "$PEER_ID" --output json 2>&1 | jq -r '.peer.id // empty' 2>/dev/null)
    if [ "$EXIST" == "$PEER_ID" ]; then
        echo "  Fixture peer $PEER_ID registered (proposal $PROP_ID)"
        return 0
    else
        echo "  Fixture peer $PEER_ID registration FAILED"
        return 1
    fi
}

# activate_test_peer <peer_id>
# Moves a PENDING peer to ACTIVE via Commons Council ResumePeer proposal.
# Identity-link, content-federation, etc. require ACTIVE peers.
activate_test_peer() {
    local PEER_ID=$1

    local STATUS=$($BINARY query federation get-peer "$PEER_ID" --output json 2>&1 | jq -r '.peer.status // "PEER_STATUS_PENDING"' 2>/dev/null)
    if [ "$STATUS" == "PEER_STATUS_ACTIVE" ]; then
        echo "  Fixture peer $PEER_ID already ACTIVE"
        return 0
    fi

    local PROP_FILE="$PROPOSAL_DIR/fixture_activate_${PEER_ID//./_}.json"
    cat > "$PROP_FILE" <<EOF
{
  "policy_address": "$COMMONS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgResumePeer",
      "authority": "$COMMONS_POLICY",
      "peer_id": "$PEER_ID"
    }
  ],
  "metadata": "Fixture: activate $PEER_ID"
}
EOF

    local TX_RES=$($BINARY tx commons submit-proposal "$PROP_FILE" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json 2>&1)
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Fixture activate $PEER_ID — submission failed"
        return 1
    fi
    sleep 6

    local PROP_ID=$($BINARY query tx "$TXHASH" --output json 2>/dev/null | \
        jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
    if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
        echo "  Fixture activate $PEER_ID — could not extract proposal id"
        return 1
    fi

    for VOTER in alice bob carol; do
        $BINARY tx commons vote-proposal "$PROP_ID" yes \
            --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000000uspark --output json > /dev/null 2>&1
        sleep 2
    done

    $BINARY tx commons execute-proposal "$PROP_ID" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --gas 2000000 --output json > /dev/null 2>&1
    sleep 6

    STATUS=$($BINARY query federation get-peer "$PEER_ID" --output json 2>&1 | jq -r '.peer.status // "PEER_STATUS_PENDING"' 2>/dev/null)
    if [ "$STATUS" == "PEER_STATUS_ACTIVE" ]; then
        echo "  Fixture peer $PEER_ID activated"
        return 0
    else
        echo "  Fixture peer $PEER_ID activation FAILED (status: $STATUS)"
        return 1
    fi
}

# set_peer_policy <peer_id> <inbound_types> <outbound_types> [accept_rep_attestations] [allow_rep_queries]
# Updates the peer's content-type policy via Operations Committee proposal.
# Required so submit-content/federate-content tests aren't rejected with 2310.
set_peer_policy() {
    local PEER_ID=$1
    local INBOUND=$2
    local OUTBOUND=${3:-""}
    local BLOCKED=${4:-""}
    local ACCEPT_REP=${5:-false}
    local ALLOW_REP=${6:-false}

    local INBOUND_JSON="[]"
    if [ -n "$INBOUND" ]; then
        INBOUND_JSON=$(echo "[\"$(echo "$INBOUND" | sed 's/,/", "/g')\"]")
    fi
    local OUTBOUND_JSON="[]"
    if [ -n "$OUTBOUND" ]; then
        OUTBOUND_JSON=$(echo "[\"$(echo "$OUTBOUND" | sed 's/,/", "/g')\"]")
    fi
    local BLOCKED_JSON="[]"
    if [ -n "$BLOCKED" ]; then
        BLOCKED_JSON=$(echo "[\"$(echo "$BLOCKED" | sed 's/,/", "/g')\"]")
    fi

    local PROP_FILE="$PROPOSAL_DIR/fixture_policy_${PEER_ID//./_}.json"
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
        "outbound_content_types": $OUTBOUND_JSON,
        "inbound_content_types": $INBOUND_JSON,
        "min_outbound_trust_level": 0,
        "inbound_rate_limit_per_epoch": 100,
        "outbound_rate_limit_per_epoch": 100,
        "allow_reputation_queries": $ALLOW_REP,
        "accept_reputation_attestations": $ACCEPT_REP,
        "max_trust_credit": 0,
        "require_review": false,
        "blocked_identities": $BLOCKED_JSON
      }
    }
  ],
  "metadata": "Fixture: set inbound policy for $PEER_ID"
}
EOF

    local TX_RES=$($BINARY tx commons submit-proposal "$PROP_FILE" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json 2>&1)
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Fixture policy $PEER_ID — submission failed"
        return 1
    fi
    sleep 6

    local PROP_ID=$($BINARY query tx "$TXHASH" --output json 2>/dev/null | \
        jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
    if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
        echo "  Fixture policy $PEER_ID — could not extract proposal id"
        return 1
    fi

    # Operations Committee = alice + bob
    for VOTER in alice bob; do
        $BINARY tx commons vote-proposal "$PROP_ID" yes \
            --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000000uspark --output json > /dev/null 2>&1
        sleep 2
    done

    EXEC_RES=$($BINARY tx commons execute-proposal "$PROP_ID" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --gas 2000000 --output json 2>&1)
    sleep 6

    # Verify policy was actually applied
    local POLICY_QUERY=$($BINARY query federation get-peer-policy "$PEER_ID" --output json 2>&1)
    local IN_TYPES=$(echo "$POLICY_QUERY" | jq -r '(.policy.inbound_content_types // []) | join(",")' 2>/dev/null)
    local OUT_TYPES=$(echo "$POLICY_QUERY" | jq -r '(.policy.outbound_content_types // []) | join(",")' 2>/dev/null)
    local APPLIED_OK=false
    if [ -n "$INBOUND" ] && [ -n "$IN_TYPES" ]; then
        APPLIED_OK=true
    fi
    if [ -n "$OUTBOUND" ] && [ -n "$OUT_TYPES" ]; then
        APPLIED_OK=true
    fi
    if [ -z "$INBOUND" ] && [ -z "$OUTBOUND" ]; then
        APPLIED_OK=true
    fi
    if [ "$APPLIED_OK" = "true" ]; then
        echo "  Fixture peer $PEER_ID policy: inbound=[$IN_TYPES] outbound=[$OUT_TYPES]"
        return 0
    else
        echo "  Fixture peer $PEER_ID policy NOT applied (proposal $PROP_ID may have failed)"
        echo "  Exec result: $(echo "$EXEC_RES" | jq -r '.raw_log // .txhash // "no info"' 2>/dev/null | head -c 200)"
        return 1
    fi
}

# Backward-compat alias for older callers
set_peer_inbound_policy() {
    set_peer_policy "$1" "$2" ""
}

# register_test_bridge <operator_key> <operator_addr> <peer_id> <protocol> <endpoint>
# Post-Phase-4: bridges are operator-signed directly into x/federation,
# which calls into x/service to escrow the bond. Stake amount is taken
# from the federation-bridge-<protocol> ServiceTypeConfig's min_bond.
# The fixture is idempotent: if the binding already exists AND the
# matching service.Operator is ACTIVE, it's a no-op.
register_test_bridge() {
    local OPERATOR_KEY=$1
    local OPERATOR=$2
    local PEER_ID=$3
    local PROTOCOL=$4
    local ENDPOINT=$5

    local BINDING_ADDR=$($BINARY query federation get-bridge-binding "$OPERATOR" "$PEER_ID" --output json 2>&1 | jq -r '.bridge_binding.address // empty' 2>/dev/null)
    local SVC_STATUS=$($BINARY query service operator "$OPERATOR" "federation-bridge-${PROTOCOL}" --output json 2>&1 | jq -r '.operator.status // empty' 2>/dev/null)
    if [ -n "$BINDING_ADDR" ] && [ "$SVC_STATUS" == "OPERATOR_STATUS_ACTIVE" ]; then
        echo "  Fixture bridge $OPERATOR_KEY → $PEER_ID already present (service status=ACTIVE)"
        return 0
    fi

    local MIN_BOND_AMT=$($BINARY query service service-type "federation-bridge-${PROTOCOL}" --output json 2>/dev/null | jq -r '.config.min_bond.amount // "1000000000"')

    local TX_RES=$($BINARY tx federation register-bridge \
        "$PEER_ID" "$PROTOCOL" "$ENDPOINT" "${MIN_BOND_AMT}uspark" \
        --from "$OPERATOR_KEY" -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000uspark --output json 2>&1)
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Fixture bridge — submission failed: $(echo "$TX_RES" | head -c 200)"
        return 1
    fi
    sleep 6

    BINDING_ADDR=$($BINARY query federation get-bridge-binding "$OPERATOR" "$PEER_ID" --output json 2>&1 | jq -r '.bridge_binding.address // empty' 2>/dev/null)
    SVC_STATUS=$($BINARY query service operator "$OPERATOR" "federation-bridge-${PROTOCOL}" --output json 2>&1 | jq -r '.operator.status // empty' 2>/dev/null)
    if [ -n "$BINDING_ADDR" ] && [ "$SVC_STATUS" == "OPERATOR_STATUS_ACTIVE" ]; then
        echo "  Fixture bridge $OPERATOR_KEY → $PEER_ID ACTIVE"
        return 0
    else
        echo "  Fixture bridge — registered but binding=${BINDING_ADDR:-missing} service_status=${SVC_STATUS:-missing}"
        return 1
    fi
}
