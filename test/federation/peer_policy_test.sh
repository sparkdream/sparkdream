#!/bin/bash

echo "--- TESTING: FEDERATION PEER POLICY (Operations Committee-Gated) ---"

# ========================================================================
# Setup
# ========================================================================
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
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
}

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done
    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
    return 1
}

submit_and_wait() {
    local TX_RES=$1
    local LABEL=${2:-"transaction"}
    TX_OK=false

    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then
        echo "  FAIL: $LABEL - no txhash"
        return 1
    fi

    local BCODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BCODE" != "0" ] && [ "$BCODE" != "null" ]; then
        echo "  FAIL: $LABEL - rejected at broadcast (code=$BCODE)"
        TX_RESULT="$TX_RES"
        return 1
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    if [ $? -ne 0 ]; then return 1; fi

    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  FAIL: $LABEL - tx failed (code=$CODE)"
        return 1
    fi

    TX_OK=true
    return 0
}

get_commons_proposal_id() {
    local TX_RESULT=$1
    echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"'
}

vote_and_execute_ops() {
    local PROP_ID=$1
    # Operations Committee has alice and bob as members
    for VOTER in "alice" "bob"; do
        local PROP_STATUS=$($BINARY query commons get-proposal $PROP_ID --output json 2>/dev/null | jq -r '.proposal.status')
        if [ "$PROP_STATUS" == "PROPOSAL_STATUS_ACCEPTED" ] || [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
            continue
        fi
        TX_RES=$($BINARY tx commons vote-proposal $PROP_ID yes \
            --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000000uspark --output json 2>&1)
        submit_and_wait "$TX_RES" "$VOTER vote" || echo "  Warning: $VOTER vote may have failed"
    done

    TX_RES=$($BINARY tx commons execute-proposal $PROP_ID \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --gas 2000000 --output json 2>&1)
    submit_and_wait "$TX_RES" "proposal exec"
    local EXEC_RC=$?
    sleep 5
    return $EXEC_RC
}

submit_ops_proposal() {
    local PROPOSAL_FILE=$1
    local LABEL=${2:-"proposal"}

    echo "  Submitting $LABEL..."
    TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_FILE" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json 2>&1)

    if ! submit_and_wait "$TX_RES" "$LABEL submission"; then return 1; fi

    PROPOSAL_ID=$(get_commons_proposal_id "$TX_RESULT")
    if [ -z "$PROPOSAL_ID" ]; then
        echo "  ERROR: Could not extract proposal ID"
        return 1
    fi
    echo "  Proposal ID: $PROPOSAL_ID"

    vote_and_execute_ops $PROPOSAL_ID
    return $?
}

# ========================================================================
# Verify policy addresses
# ========================================================================
if [ -z "$OPS_POLICY" ] || [ "$OPS_POLICY" == "null" ]; then
    echo "ERROR: OPS_POLICY not set. Run setup_test_accounts.sh first."
    exit 1
fi
echo "Operations Committee Policy: $OPS_POLICY"
echo ""

# ========================================================================
# TEST 1: Query default peer policy
# ========================================================================
echo "--- TEST 1: Query default peer policy ---"

POLICY_DATA=$($BINARY query federation get-peer-policy mastodon.example --output json 2>&1)
POLICY_PEER=$(echo "$POLICY_DATA" | jq -r '.policy.peer_id // empty')

if [ "$POLICY_PEER" == "mastodon.example" ]; then
    # Default policy should have empty content type lists
    OUTBOUND_COUNT=$(echo "$POLICY_DATA" | jq '.policy.outbound_content_types | length')
    INBOUND_COUNT=$(echo "$POLICY_DATA" | jq '.policy.inbound_content_types | length')
    echo "  Default policy: outbound=$OUTBOUND_COUNT, inbound=$INBOUND_COUNT"
    record_result "Query default peer policy" "PASS"
else
    echo "  Failed to query policy"
    record_result "Query default peer policy" "FAIL"
fi

# ========================================================================
# TEST 2: Update peer policy with content types
# ========================================================================
echo ""
echo "--- TEST 2: Update peer policy with content types ---"

cat > "$PROPOSAL_DIR/update_policy_content.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
      "authority": "$OPS_POLICY",
      "peer_id": "mastodon.example",
      "policy": {
        "peer_id": "mastodon.example",
        "outbound_content_types": [],
        "inbound_content_types": ["blog_post", "blog_reply"],
        "min_outbound_trust_level": 0,
        "inbound_rate_limit_per_epoch": 100,
        "outbound_rate_limit_per_epoch": 0,
        "allow_reputation_queries": false,
        "accept_reputation_attestations": false,
        "max_trust_credit": 0,
        "require_review": false,
        "blocked_identities": []
      }
    }
  ],
  "metadata": "Update ActivityPub peer policy with inbound content types"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/update_policy_content.json" "update policy"; then
    POLICY_DATA=$($BINARY query federation get-peer-policy mastodon.example --output json 2>&1)
    INBOUND=$(echo "$POLICY_DATA" | jq -r '.policy.inbound_content_types // []')
    INBOUND_COUNT=$(echo "$INBOUND" | jq 'length')

    if [ "$INBOUND_COUNT" -eq 2 ] 2>/dev/null; then
        echo "  Policy updated with $INBOUND_COUNT inbound types"
        record_result "Update policy content types" "PASS"
    else
        echo "  Expected 2 inbound types, got $INBOUND_COUNT"
        record_result "Update policy content types" "FAIL"
    fi
else
    record_result "Update policy content types" "FAIL"
fi

# ========================================================================
# TEST 3: Update IBC peer policy with reputation and outbound
# ========================================================================
echo ""
echo "--- TEST 3: Update IBC peer policy with reputation ---"

cat > "$PROPOSAL_DIR/update_ibc_policy.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
      "authority": "$OPS_POLICY",
      "peer_id": "spark.testnet",
      "policy": {
        "peer_id": "spark.testnet",
        "outbound_content_types": ["blog_post", "forum_thread"],
        "inbound_content_types": ["blog_post", "forum_thread", "forum_reply"],
        "min_outbound_trust_level": 2,
        "inbound_rate_limit_per_epoch": 200,
        "outbound_rate_limit_per_epoch": 100,
        "allow_reputation_queries": true,
        "accept_reputation_attestations": true,
        "max_trust_credit": 1,
        "require_review": false,
        "blocked_identities": []
      }
    }
  ],
  "metadata": "Update IBC peer policy with reputation support"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/update_ibc_policy.json" "update ibc policy"; then
    POLICY_DATA=$($BINARY query federation get-peer-policy spark.testnet --output json 2>&1)
    ALLOW_REP=$(echo "$POLICY_DATA" | jq -r 'if .policy.allow_reputation_queries then "true" else "false" end')
    MIN_TRUST=$(echo "$POLICY_DATA" | jq -r '.policy.min_outbound_trust_level // "0"')

    if [ "$ALLOW_REP" == "true" ] && [ "$MIN_TRUST" -eq 2 ] 2>/dev/null; then
        echo "  IBC policy updated: rep=$ALLOW_REP, trust=$MIN_TRUST"
        record_result "Update IBC peer policy" "PASS"
    else
        echo "  Unexpected: rep=$ALLOW_REP, trust=$MIN_TRUST"
        record_result "Update IBC peer policy" "FAIL"
    fi
else
    record_result "Update IBC peer policy" "FAIL"
fi

# ========================================================================
# TEST 4: Reputation on non-IBC peer fails
# ========================================================================
echo ""
echo "--- TEST 4: Reputation policy on non-IBC peer fails ---"

cat > "$PROPOSAL_DIR/update_policy_rep_fail.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
      "authority": "$OPS_POLICY",
      "peer_id": "mastodon.example",
      "policy": {
        "peer_id": "mastodon.example",
        "outbound_content_types": [],
        "inbound_content_types": ["blog_post"],
        "min_outbound_trust_level": 0,
        "inbound_rate_limit_per_epoch": 100,
        "outbound_rate_limit_per_epoch": 0,
        "allow_reputation_queries": true,
        "accept_reputation_attestations": false,
        "max_trust_credit": 0,
        "require_review": false,
        "blocked_identities": []
      }
    }
  ],
  "metadata": "Reputation on ActivityPub peer (should fail)"
}
EOF

TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_policy_rep_fail.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "rep on activitypub proposal"; then
    PROP_ID=$(get_commons_proposal_id "$TX_RESULT")
    if [ -n "$PROP_ID" ]; then
        vote_and_execute_ops $PROP_ID
        # Verify policy was NOT updated with reputation
        POLICY_DATA=$($BINARY query federation get-peer-policy mastodon.example --output json 2>&1)
        ALLOW_REP=$(echo "$POLICY_DATA" | jq -r 'if .policy.allow_reputation_queries then "true" else "false" end')
        if [ "$ALLOW_REP" == "false" ]; then
            echo "  Correctly rejected reputation on non-IBC peer"
            record_result "Reputation on non-IBC fails" "PASS"
        else
            echo "  Reputation should not be enabled on ActivityPub peer"
            record_result "Reputation on non-IBC fails" "FAIL"
        fi
    else
        record_result "Reputation on non-IBC fails" "PASS"
    fi
else
    record_result "Reputation on non-IBC fails" "PASS"
fi

# ========================================================================
# TEST 5: Unknown content type fails
# ========================================================================
echo ""
echo "--- TEST 5: Unknown content type in policy fails ---"

cat > "$PROPOSAL_DIR/update_policy_bad_type.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
      "authority": "$OPS_POLICY",
      "peer_id": "mastodon.example",
      "policy": {
        "peer_id": "mastodon.example",
        "outbound_content_types": [],
        "inbound_content_types": ["nonexistent_type"],
        "min_outbound_trust_level": 0,
        "inbound_rate_limit_per_epoch": 100,
        "outbound_rate_limit_per_epoch": 0,
        "allow_reputation_queries": false,
        "accept_reputation_attestations": false,
        "max_trust_credit": 0,
        "require_review": false,
        "blocked_identities": []
      }
    }
  ],
  "metadata": "Unknown content type (should fail)"
}
EOF

TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_policy_bad_type.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "unknown type proposal"; then
    PROP_ID=$(get_commons_proposal_id "$TX_RESULT")
    if [ -n "$PROP_ID" ]; then
        vote_and_execute_ops $PROP_ID
        # Verify policy still has original inbound types (not overwritten)
        POLICY_DATA=$($BINARY query federation get-peer-policy mastodon.example --output json 2>&1)
        HAS_BAD=$(echo "$POLICY_DATA" | jq '.policy.inbound_content_types // [] | map(select(. == "nonexistent_type")) | length')
        if [ "$HAS_BAD" -eq 0 ] 2>/dev/null; then
            echo "  Unknown content type correctly rejected"
            record_result "Unknown content type fails" "PASS"
        else
            record_result "Unknown content type fails" "FAIL"
        fi
    else
        record_result "Unknown content type fails" "PASS"
    fi
else
    record_result "Unknown content type fails" "PASS"
fi

# ========================================================================
# TEST 6: Update policy with blocked identities
# ========================================================================
echo ""
echo "--- TEST 6: Update policy with blocked identities ---"

cat > "$PROPOSAL_DIR/update_policy_blocked.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
      "authority": "$OPS_POLICY",
      "peer_id": "mastodon.example",
      "policy": {
        "peer_id": "mastodon.example",
        "outbound_content_types": [],
        "inbound_content_types": ["blog_post", "blog_reply"],
        "min_outbound_trust_level": 0,
        "inbound_rate_limit_per_epoch": 100,
        "outbound_rate_limit_per_epoch": 0,
        "allow_reputation_queries": false,
        "accept_reputation_attestations": false,
        "max_trust_credit": 0,
        "require_review": true,
        "blocked_identities": ["@spammer@evil.instance", "@troll@bad.server"]
      }
    }
  ],
  "metadata": "Update policy with blocked identities"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/update_policy_blocked.json" "update blocked identities"; then
    POLICY_DATA=$($BINARY query federation get-peer-policy mastodon.example --output json 2>&1)
    BLOCKED_COUNT=$(echo "$POLICY_DATA" | jq '.policy.blocked_identities | length')
    REQUIRE_REVIEW=$(echo "$POLICY_DATA" | jq -r 'if .policy.require_review then "true" else "false" end')

    if [ "$BLOCKED_COUNT" -eq 2 ] 2>/dev/null && [ "$REQUIRE_REVIEW" == "true" ]; then
        echo "  Policy updated: blocked=$BLOCKED_COUNT, require_review=$REQUIRE_REVIEW"
        record_result "Blocked identities" "PASS"
    else
        echo "  Unexpected: blocked=$BLOCKED_COUNT, require_review=$REQUIRE_REVIEW"
        record_result "Blocked identities" "FAIL"
    fi
else
    record_result "Blocked identities" "FAIL"
fi

# ========================================================================
# TEST 7: Unauthorized user cannot update policy directly
# ========================================================================
echo ""
echo "--- TEST 7: Unauthorized policy update fails ---"

TX_RES=$($BINARY tx federation update-peer-policy mastodon.example \
    --from operator1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "unauthorized policy update"; then
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  Correctly rejected (code=$CODE)"
        record_result "Unauthorized policy update" "PASS"
    else
        record_result "Unauthorized policy update" "FAIL"
    fi
else
    echo "  Correctly rejected"
    record_result "Unauthorized policy update" "PASS"
fi

# ========================================================================
# TEST 8: Reveal content types are always rejected
# reveal_proposal and reveal_tranche can never be federated.
# ========================================================================
echo ""
echo "--- TEST 8: Reveal content types rejected ---"

cat > "$PROPOSAL_DIR/update_policy_reveal.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
      "authority": "$OPS_POLICY",
      "peer_id": "mastodon.example",
      "policy": {
        "peer_id": "mastodon.example",
        "outbound_content_types": [],
        "inbound_content_types": ["blog_post", "reveal_proposal"],
        "min_outbound_trust_level": 0,
        "inbound_rate_limit_per_epoch": 100,
        "outbound_rate_limit_per_epoch": 0,
        "allow_reputation_queries": false,
        "accept_reputation_attestations": false,
        "max_trust_credit": 0,
        "require_review": false,
        "blocked_identities": []
      }
    }
  ],
  "metadata": "Reveal content type in policy (should fail)"
}
EOF

TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_policy_reveal.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "reveal type proposal"; then
    PROP_ID=$(get_commons_proposal_id "$TX_RESULT")
    if [ -n "$PROP_ID" ]; then
        vote_and_execute_ops $PROP_ID
        # Verify reveal_proposal was NOT added to policy
        POLICY_DATA=$($BINARY query federation get-peer-policy mastodon.example --output json 2>&1)
        HAS_REVEAL=$(echo "$POLICY_DATA" | jq '[.policy.inbound_content_types[] | select(. == "reveal_proposal")] | length')
        if [ "$HAS_REVEAL" == "0" ] 2>/dev/null; then
            echo "  Reveal content type correctly rejected"
            record_result "Reveal types rejected" "PASS"
        else
            echo "  Reveal content type should not have been accepted"
            record_result "Reveal types rejected" "FAIL"
        fi
    else
        record_result "Reveal types rejected" "PASS"
    fi
else
    echo "  Correctly failed"
    record_result "Reveal types rejected" "PASS"
fi

# ========================================================================
# TEST 9: Outbound content types on IBC peer
# Set outbound types on spark.testnet (IBC peer) and verify.
# ========================================================================
echo ""
echo "--- TEST 9: Outbound content types on IBC peer ---"

cat > "$PROPOSAL_DIR/update_policy_outbound.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
      "authority": "$OPS_POLICY",
      "peer_id": "spark.testnet",
      "policy": {
        "peer_id": "spark.testnet",
        "outbound_content_types": ["blog_post", "forum_thread"],
        "inbound_content_types": ["blog_post"],
        "min_outbound_trust_level": 2,
        "inbound_rate_limit_per_epoch": 100,
        "outbound_rate_limit_per_epoch": 50,
        "allow_reputation_queries": true,
        "accept_reputation_attestations": true,
        "max_trust_credit": 3,
        "require_review": false,
        "blocked_identities": []
      }
    }
  ],
  "metadata": "Set outbound content types on IBC peer"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/update_policy_outbound.json" "outbound types"; then
    POLICY_DATA=$($BINARY query federation get-peer-policy spark.testnet --output json 2>&1)
    OUTBOUND_COUNT=$(echo "$POLICY_DATA" | jq '.policy.outbound_content_types | length' 2>/dev/null)
    MIN_TRUST=$(echo "$POLICY_DATA" | jq -r '.policy.min_outbound_trust_level // 0')

    if [ "$OUTBOUND_COUNT" -eq 2 ] 2>/dev/null; then
        echo "  Outbound types: $OUTBOUND_COUNT, min_outbound_trust_level: $MIN_TRUST"
        record_result "Outbound content types" "PASS"
    else
        echo "  Expected 2 outbound types, got $OUTBOUND_COUNT"
        record_result "Outbound content types" "FAIL"
    fi
else
    record_result "Outbound content types" "FAIL"
fi

# ========================================================================
# TEST 10: Reveal tranche in outbound types rejected
# ========================================================================
echo ""
echo "--- TEST 10: Reveal tranche in outbound rejected ---"

cat > "$PROPOSAL_DIR/update_policy_reveal_outbound.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
      "authority": "$OPS_POLICY",
      "peer_id": "spark.testnet",
      "policy": {
        "peer_id": "spark.testnet",
        "outbound_content_types": ["blog_post", "reveal_tranche"],
        "inbound_content_types": ["blog_post"],
        "min_outbound_trust_level": 2,
        "inbound_rate_limit_per_epoch": 100,
        "outbound_rate_limit_per_epoch": 50,
        "allow_reputation_queries": true,
        "accept_reputation_attestations": true,
        "max_trust_credit": 3,
        "require_review": false,
        "blocked_identities": []
      }
    }
  ],
  "metadata": "Reveal tranche in outbound (should fail)"
}
EOF

TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_policy_reveal_outbound.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "reveal outbound proposal"; then
    PROP_ID=$(get_commons_proposal_id "$TX_RESULT")
    if [ -n "$PROP_ID" ]; then
        vote_and_execute_ops $PROP_ID
        # Verify reveal_tranche was NOT added
        POLICY_DATA=$($BINARY query federation get-peer-policy spark.testnet --output json 2>&1)
        HAS_REVEAL=$(echo "$POLICY_DATA" | jq '[.policy.outbound_content_types[] | select(. == "reveal_tranche")] | length')
        if [ "$HAS_REVEAL" == "0" ] 2>/dev/null; then
            echo "  Reveal tranche correctly rejected"
            record_result "Reveal tranche outbound rejected" "PASS"
        else
            echo "  Reveal tranche should not be in outbound types"
            record_result "Reveal tranche outbound rejected" "FAIL"
        fi
    else
        record_result "Reveal tranche outbound rejected" "PASS"
    fi
else
    echo "  Correctly failed"
    record_result "Reveal tranche outbound rejected" "PASS"
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "PEER POLICY TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
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
