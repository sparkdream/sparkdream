#!/bin/bash

echo "--- TESTING: FEDERATION PARAMS ---"

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
}

# ========================================================================
# TEST 1: Query federation params
# ========================================================================
echo ""
echo "--- TEST 1: Query federation params ---"

PARAMS=$($BINARY query federation params --output json 2>&1)

if echo "$PARAMS" | jq -e '.params' > /dev/null 2>&1; then
    echo "  Params returned successfully"
    record_result "Query federation params" "PASS"
else
    echo "  Failed to query params"
    echo "  $PARAMS"
    record_result "Query federation params" "FAIL"
fi

# ========================================================================
# TEST 2: Verify bridge params
# ========================================================================
echo ""
echo "--- TEST 2: Verify bridge parameters ---"

MIN_BRIDGE_STAKE=$(echo "$PARAMS" | jq -r '.params.min_bridge_stake.amount // "0"')
MAX_BRIDGES=$(echo "$PARAMS" | jq -r '.params.max_bridges_per_peer // "0"')
UNBONDING=$(echo "$PARAMS" | jq -r '.params.bridge_unbonding_period // "0s"')

echo "  min_bridge_stake:    $MIN_BRIDGE_STAKE"
echo "  max_bridges_per_peer: $MAX_BRIDGES"
echo "  bridge_unbonding:    $UNBONDING"

if [ "$MAX_BRIDGES" -gt 0 ] 2>/dev/null; then
    record_result "Bridge params present" "PASS"
else
    record_result "Bridge params present" "FAIL"
fi

# ========================================================================
# TEST 3: Verify content params
# ========================================================================
echo ""
echo "--- TEST 3: Verify content parameters ---"

MAX_INBOUND=$(echo "$PARAMS" | jq -r '.params.max_inbound_per_block // "0"')
MAX_OUTBOUND=$(echo "$PARAMS" | jq -r '.params.max_outbound_per_block // "0"')
CONTENT_TTL=$(echo "$PARAMS" | jq -r '.params.content_ttl // "0s"')
MAX_BODY=$(echo "$PARAMS" | jq -r '.params.max_content_body_size // "0"')

echo "  max_inbound_per_block:  $MAX_INBOUND"
echo "  max_outbound_per_block: $MAX_OUTBOUND"
echo "  content_ttl:            $CONTENT_TTL"
echo "  max_content_body_size:  $MAX_BODY"

if [ "$MAX_INBOUND" -gt 0 ] 2>/dev/null && [ "$MAX_OUTBOUND" -gt 0 ] 2>/dev/null; then
    record_result "Content params present" "PASS"
else
    record_result "Content params present" "FAIL"
fi

# ========================================================================
# TEST 4: Verify known_content_types
# ========================================================================
echo ""
echo "--- TEST 4: Verify known_content_types ---"

KNOWN_TYPES=$(echo "$PARAMS" | jq -r '.params.known_content_types // []')
TYPE_COUNT=$(echo "$KNOWN_TYPES" | jq 'length')

echo "  known_content_types count: $TYPE_COUNT"
echo "  types: $KNOWN_TYPES"

if [ "$TYPE_COUNT" -gt 0 ] 2>/dev/null; then
    # Check for expected default types
    HAS_BLOG=$(echo "$KNOWN_TYPES" | jq 'map(select(. == "blog_post")) | length')
    HAS_FORUM=$(echo "$KNOWN_TYPES" | jq 'map(select(. == "forum_thread")) | length')
    if [ "$HAS_BLOG" -gt 0 ] && [ "$HAS_FORUM" -gt 0 ]; then
        echo "  Contains blog_post and forum_thread"
        record_result "Known content types" "PASS"
    else
        echo "  Missing expected content types"
        record_result "Known content types" "FAIL"
    fi
else
    record_result "Known content types" "FAIL"
fi

# ========================================================================
# TEST 5: Verify identity params
# ========================================================================
echo ""
echo "--- TEST 5: Verify identity parameters ---"

MAX_LINKS=$(echo "$PARAMS" | jq -r '.params.max_identity_links_per_user // "0"')
UNVERIFIED_TTL=$(echo "$PARAMS" | jq -r '.params.unverified_link_ttl // "0s"')
CHALLENGE_TTL=$(echo "$PARAMS" | jq -r '.params.challenge_ttl // "0s"')

echo "  max_identity_links_per_user: $MAX_LINKS"
echo "  unverified_link_ttl:         $UNVERIFIED_TTL"
echo "  challenge_ttl:               $CHALLENGE_TTL"

if [ "$MAX_LINKS" -gt 0 ] 2>/dev/null; then
    record_result "Identity params present" "PASS"
else
    record_result "Identity params present" "FAIL"
fi

# ========================================================================
# TEST 6: Verify verifier params
# ========================================================================
echo ""
echo "--- TEST 6: Verify verifier parameters ---"

MIN_TRUST=$(echo "$PARAMS" | jq -r '.params.min_verifier_trust_level // "0"')
MIN_BOND=$(echo "$PARAMS" | jq -r '.params.min_verifier_bond // "0"')
VERIFY_WINDOW=$(echo "$PARAMS" | jq -r '.params.verification_window // "0s"')
CHALLENGE_WINDOW=$(echo "$PARAMS" | jq -r '.params.challenge_window // "0s"')
CHALLENGE_FEE=$(echo "$PARAMS" | jq -r '.params.challenge_fee.amount // "0"')

echo "  min_verifier_trust_level: $MIN_TRUST"
echo "  min_verifier_bond:        $MIN_BOND"
echo "  verification_window:      $VERIFY_WINDOW"
echo "  challenge_window:         $CHALLENGE_WINDOW"
echo "  challenge_fee:            $CHALLENGE_FEE"

if [ "$MIN_TRUST" -gt 0 ] 2>/dev/null && [ "$MIN_BOND" != "0" ]; then
    record_result "Verifier params present" "PASS"
else
    record_result "Verifier params present" "FAIL"
fi

# ========================================================================
# TEST 7: Verify arbiter params
# ========================================================================
echo ""
echo "--- TEST 7: Verify arbiter parameters ---"

ARBITER_QUORUM=$(echo "$PARAMS" | jq -r '.params.arbiter_quorum // "0"')
ARBITER_WINDOW=$(echo "$PARAMS" | jq -r '.params.arbiter_resolution_window // "0s"')
ESCALATION_FEE=$(echo "$PARAMS" | jq -r '.params.escalation_fee.amount // "0"')

echo "  arbiter_quorum:           $ARBITER_QUORUM"
echo "  arbiter_resolution_window: $ARBITER_WINDOW"
echo "  escalation_fee:           $ESCALATION_FEE"

if [ "$ARBITER_QUORUM" -gt 0 ] 2>/dev/null; then
    record_result "Arbiter params present" "PASS"
else
    record_result "Arbiter params present" "FAIL"
fi

# ========================================================================
# TEST 8: Verify IBC params
# ========================================================================
echo ""
echo "--- TEST 8: Verify IBC parameters ---"

IBC_PORT=$(echo "$PARAMS" | jq -r '.params.ibc_port // ""')
IBC_VERSION=$(echo "$PARAMS" | jq -r '.params.ibc_channel_version // ""')

echo "  ibc_port:            $IBC_PORT"
echo "  ibc_channel_version: $IBC_VERSION"

if [ -n "$IBC_PORT" ] && [ "$IBC_PORT" != "null" ]; then
    record_result "IBC params present" "PASS"
else
    record_result "IBC params present" "FAIL"
fi

# ========================================================================
# TEST 9: Update operational params via Operations Committee
# MsgUpdateOperationalParams allows Ops Committee to change a subset
# of params (content sizes, rate limits, TTLs) without full governance.
# ========================================================================
echo ""
echo "--- TEST 9: Update operational params via Ops Committee ---"

# Load test env for committee policy address
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    source "$SCRIPT_DIR/.test_env"
fi

PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

if [ -n "$OPS_POLICY" ] && [ "$OPS_POLICY" != "null" ]; then
    # Save original values for comparison
    ORIG_MAX_INBOUND=$MAX_INBOUND
    ORIG_MAX_BODY=$MAX_BODY

    NEW_MAX_INBOUND=75
    NEW_MAX_BODY=8192

    # Get current values for fields we're NOT changing (to preserve them)
    CURRENT_MAX_OUTBOUND=$(echo "$PARAMS" | jq -r '.params.max_outbound_per_block // "50"')
    CURRENT_MAX_URI=$(echo "$PARAMS" | jq -r '.params.max_content_uri_size // "512"')
    CURRENT_MAX_META=$(echo "$PARAMS" | jq -r '.params.max_protocol_metadata_size // "1024"')
    CURRENT_CONTENT_TTL=$(echo "$PARAMS" | jq -r '.params.content_ttl // "2160h0m0s"')
    CURRENT_ATTEST_TTL=$(echo "$PARAMS" | jq -r '.params.attestation_ttl // "720h0m0s"')
    CURRENT_MAX_TRUST=$(echo "$PARAMS" | jq -r '.params.global_max_trust_credit // "5"')
    CURRENT_DISCOUNT=$(echo "$PARAMS" | jq -r '.params.trust_discount_rate // "500000000000000000"')
    CURRENT_INACTIVITY=$(echo "$PARAMS" | jq -r '.params.bridge_inactivity_threshold // "100"')
    CURRENT_MAX_PRUNE=$(echo "$PARAMS" | jq -r '.params.max_prune_per_block // "10"')

    cat > "$PROPOSAL_DIR/update_ops_params.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdateOperationalParams",
      "authority": "$OPS_POLICY",
      "operational_params": {
        "max_inbound_per_block": "$NEW_MAX_INBOUND",
        "max_outbound_per_block": "$CURRENT_MAX_OUTBOUND",
        "max_content_body_size": "$NEW_MAX_BODY",
        "max_content_uri_size": "$CURRENT_MAX_URI",
        "max_protocol_metadata_size": "$CURRENT_MAX_META",
        "content_ttl": "$CURRENT_CONTENT_TTL",
        "attestation_ttl": "$CURRENT_ATTEST_TTL",
        "global_max_trust_credit": $CURRENT_MAX_TRUST,
        "trust_discount_rate": "$CURRENT_DISCOUNT",
        "bridge_inactivity_threshold": "$CURRENT_INACTIVITY",
        "max_prune_per_block": "$CURRENT_MAX_PRUNE"
      }
    }
  ],
  "metadata": "Update operational params via Ops Committee"
}
EOF

    wait_for_tx_param() {
        local TXHASH=$1; local MAX=20; local A=0
        while [ $A -lt $MAX ]; do
            RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
            if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
            A=$((A + 1)); sleep 1
        done
        return 1
    }

    # Submit proposal
    TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_ops_params.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -n "$TXHASH" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx_param "$TXHASH")
        CODE=$(echo "$TX_RESULT" | jq -r '.code // "1"')

        if [ "$CODE" == "0" ]; then
            PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

            if [ -n "$PROP_ID" ]; then
                # Vote YES
                for VOTER in "alice" "bob"; do
                    S=$($BINARY query commons get-proposal $PROP_ID --output json 2>/dev/null | jq -r '.proposal.status')
                    [ "$S" == "PROPOSAL_STATUS_ACCEPTED" ] || [ "$S" == "PROPOSAL_STATUS_EXECUTED" ] && continue
                    VR=$($BINARY tx commons vote-proposal $PROP_ID yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
                    VH=$(echo "$VR" | jq -r '.txhash // empty')
                    [ -n "$VH" ] && sleep 6
                done

                # Execute
                ER=$($BINARY tx commons execute-proposal $PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --gas 2000000 --output json 2>&1)
                EH=$(echo "$ER" | jq -r '.txhash // empty')
                [ -n "$EH" ] && sleep 6

                # Verify params changed
                NEW_PARAMS=$($BINARY query federation params --output json 2>&1)
                UPDATED_INBOUND=$(echo "$NEW_PARAMS" | jq -r '.params.max_inbound_per_block // "0"')
                UPDATED_BODY=$(echo "$NEW_PARAMS" | jq -r '.params.max_content_body_size // "0"')

                echo "  max_inbound: $ORIG_MAX_INBOUND → $UPDATED_INBOUND (expected $NEW_MAX_INBOUND)"
                echo "  max_body:    $ORIG_MAX_BODY → $UPDATED_BODY (expected $NEW_MAX_BODY)"

                if [ "$UPDATED_INBOUND" == "$NEW_MAX_INBOUND" ] && [ "$UPDATED_BODY" == "$NEW_MAX_BODY" ]; then
                    record_result "Update operational params" "PASS"

                    # Restore original values
                    cat > "$PROPOSAL_DIR/restore_ops_params.json" <<REOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdateOperationalParams",
      "authority": "$OPS_POLICY",
      "operational_params": {
        "max_inbound_per_block": "$ORIG_MAX_INBOUND",
        "max_outbound_per_block": "$CURRENT_MAX_OUTBOUND",
        "max_content_body_size": "$ORIG_MAX_BODY",
        "max_content_uri_size": "$CURRENT_MAX_URI",
        "max_protocol_metadata_size": "$CURRENT_MAX_META",
        "content_ttl": "$CURRENT_CONTENT_TTL",
        "attestation_ttl": "$CURRENT_ATTEST_TTL",
        "global_max_trust_credit": $CURRENT_MAX_TRUST,
        "trust_discount_rate": "$CURRENT_DISCOUNT",
        "bridge_inactivity_threshold": "$CURRENT_INACTIVITY",
        "max_prune_per_block": "$CURRENT_MAX_PRUNE"
      }
    }
  ],
  "metadata": "Restore original operational params"
}
REOF
                    RR=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/restore_ops_params.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
                    RH=$(echo "$RR" | jq -r '.txhash // empty')
                    if [ -n "$RH" ]; then
                        sleep 6
                        RT=$(wait_for_tx_param "$RH")
                        RP=$(echo "$RT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
                        if [ -n "$RP" ]; then
                            for V in "alice" "bob"; do
                                S=$($BINARY query commons get-proposal $RP --output json 2>/dev/null | jq -r '.proposal.status')
                                [ "$S" == "PROPOSAL_STATUS_ACCEPTED" ] && continue
                                $BINARY tx commons vote-proposal $RP yes --from $V -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json > /dev/null 2>&1
                                sleep 6
                            done
                            $BINARY tx commons execute-proposal $RP --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --gas 2000000 --output json > /dev/null 2>&1
                            sleep 5
                            echo "  Original params restored"
                        fi
                    fi
                else
                    echo "  Params did not change"
                    record_result "Update operational params" "FAIL"
                fi
            else
                echo "  No proposal ID"
                record_result "Update operational params" "FAIL"
            fi
        else
            echo "  Proposal submission failed (code=$CODE)"
            record_result "Update operational params" "FAIL"
        fi
    else
        echo "  No txhash from proposal submission"
        record_result "Update operational params" "FAIL"
    fi
else
    echo "  OPS_POLICY not set (run setup_test_accounts.sh first)"
    record_result "Update operational params" "FAIL"
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "PARAMS TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-40s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
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
