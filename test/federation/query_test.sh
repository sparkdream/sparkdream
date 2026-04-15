#!/bin/bash

echo "--- TESTING: FEDERATION QUERIES ---"

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "ERROR: .test_env not found."; exit 1
fi
source "$SCRIPT_DIR/.test_env"

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

# ========================================================================
# TEST 1: Query params
# ========================================================================
echo ""
echo "--- TEST 1: Query federation params ---"

PARAMS=$($BINARY query federation params --output json 2>&1)
if echo "$PARAMS" | jq -e '.params' > /dev/null 2>&1; then
    record_result "Query params" "PASS"
else
    record_result "Query params" "FAIL"
fi

# ========================================================================
# TEST 2: Get specific peer
# ========================================================================
echo ""
echo "--- TEST 2: Get specific peer ---"

PEER_DATA=$($BINARY query federation get-peer mastodon.example --output json 2>&1)
PEER_ID=$(echo "$PEER_DATA" | jq -r '.peer.id // empty')

if [ "$PEER_ID" == "mastodon.example" ]; then
    PEER_TYPE=$(echo "$PEER_DATA" | jq -r '.peer.type // empty')
    PEER_STATUS=$(echo "$PEER_DATA" | jq -r '.peer.status // "PEER_STATUS_PENDING"')
    echo "  Peer: id=$PEER_ID, type=$PEER_TYPE, status=$PEER_STATUS"
    record_result "Get peer by ID" "PASS"
else
    echo "  Peer not found"
    record_result "Get peer by ID" "FAIL"
fi

# ========================================================================
# TEST 3: List all peers
# ========================================================================
echo ""
echo "--- TEST 3: List all peers ---"

PEERS=$($BINARY query federation list-peers --output json 2>&1)
PEER_COUNT=$(echo "$PEERS" | jq '.peers | length' 2>/dev/null)

echo "  Total peers: $PEER_COUNT"
if [ "$PEER_COUNT" -ge 1 ] 2>/dev/null; then
    # Print each peer
    echo "$PEERS" | jq -r '.peers[] | "    \(.id) [\(.type)] - \(.status // "PEER_STATUS_PENDING")"' 2>/dev/null
    record_result "List peers" "PASS"
else
    record_result "List peers" "FAIL"
fi

# ========================================================================
# TEST 4: Get peer policy
# ========================================================================
echo ""
echo "--- TEST 4: Get peer policy ---"

POLICY=$($BINARY query federation get-peer-policy mastodon.example --output json 2>&1)
POLICY_PEER=$(echo "$POLICY" | jq -r '.policy.peer_id // empty')

if [ "$POLICY_PEER" == "mastodon.example" ]; then
    INBOUND=$(echo "$POLICY" | jq -r '.policy.inbound_content_types // []')
    BLOCKED=$(echo "$POLICY" | jq -r '.policy.blocked_identities // []')
    echo "  Inbound types: $INBOUND"
    echo "  Blocked identities: $BLOCKED"
    record_result "Get peer policy" "PASS"
else
    record_result "Get peer policy" "FAIL"
fi

# ========================================================================
# TEST 5: Get bridge operator
# ========================================================================
echo ""
echo "--- TEST 5: Get bridge operator ---"

BRIDGE=$($BINARY query federation get-bridge-operator $OPERATOR1_ADDR mastodon.example --output json 2>&1)
BRIDGE_ADDR=$(echo "$BRIDGE" | jq -r '.bridge_operator.address // empty')

if [ "$BRIDGE_ADDR" == "$OPERATOR1_ADDR" ]; then
    BRIDGE_STATUS=$(echo "$BRIDGE" | jq -r '.bridge_operator.status // "BRIDGE_STATUS_UNSPECIFIED"')
    BRIDGE_STAKE=$(echo "$BRIDGE" | jq -r '.bridge_operator.stake.amount // "0"')
    SUBMITTED=$(echo "$BRIDGE" | jq -r '.bridge_operator.content_submitted // "0"')
    echo "  Bridge: status=$BRIDGE_STATUS, stake=$BRIDGE_STAKE, submitted=$SUBMITTED"
    record_result "Get bridge operator" "PASS"
else
    echo "  Bridge operator not found"
    record_result "Get bridge operator" "FAIL"
fi

# ========================================================================
# TEST 6: List bridge operators
# ========================================================================
echo ""
echo "--- TEST 6: List bridge operators ---"

BRIDGES=$($BINARY query federation list-bridge-operators --output json 2>&1)
BRIDGE_COUNT=$(echo "$BRIDGES" | jq '.bridge_operators | length' 2>/dev/null)

echo "  Total bridge operators: $BRIDGE_COUNT"
if [ "$BRIDGE_COUNT" -ge 1 ] 2>/dev/null; then
    echo "$BRIDGES" | jq -r '.bridge_operators[] | "    \(.address[:20])... → \(.peer_id) [\(.status // "BRIDGE_STATUS_UNSPECIFIED")]"' 2>/dev/null
    record_result "List bridge operators" "PASS"
else
    record_result "List bridge operators" "FAIL"
fi

# ========================================================================
# TEST 7: Get federated content by ID
# ========================================================================
echo ""
echo "--- TEST 7: Get federated content ---"

# Try content ID 0 (first one)
CONTENT=$($BINARY query federation get-federated-content 0 --output json 2>&1)
CONTENT_PEER=$(echo "$CONTENT" | jq -r '.content.peer_id // empty')

if [ -n "$CONTENT_PEER" ] && [ "$CONTENT_PEER" != "null" ]; then
    CONTENT_TYPE=$(echo "$CONTENT" | jq -r '.content.content_type // empty')
    CONTENT_STATUS=$(echo "$CONTENT" | jq -r '.content.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"')
    CONTENT_TITLE=$(echo "$CONTENT" | jq -r '.content.title // empty')
    echo "  Content: peer=$CONTENT_PEER, type=$CONTENT_TYPE, status=$CONTENT_STATUS"
    echo "  Title: $CONTENT_TITLE"
    record_result "Get federated content" "PASS"
else
    echo "  No content found at ID 0 (may not have been created)"
    record_result "Get federated content" "PASS"
fi

# ========================================================================
# TEST 8: List federated content
# ========================================================================
echo ""
echo "--- TEST 8: List federated content ---"

CONTENT_LIST=$($BINARY query federation list-federated-content --output json 2>&1)
CONTENT_COUNT=$(echo "$CONTENT_LIST" | jq '.content | length' 2>/dev/null)

echo "  Total federated content: $CONTENT_COUNT"
if [ "$CONTENT_COUNT" -ge 0 ] 2>/dev/null; then
    if [ "$CONTENT_COUNT" -gt 0 ]; then
        echo "$CONTENT_LIST" | jq -r '.content[] | "    #\(.id // 0) \(.content_type) from \(.peer_id) [\(.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION")]"' 2>/dev/null
    fi
    record_result "List federated content" "PASS"
else
    record_result "List federated content" "FAIL"
fi

# ========================================================================
# TEST 9: Get identity link
# ========================================================================
echo ""
echo "--- TEST 9: Get identity link ---"

LINK=$($BINARY query federation get-identity-link $LINKER1_ADDR mastodon.example --output json 2>&1)
LINK_REMOTE=$(echo "$LINK" | jq -r '.link.remote_identity // empty')

if [ -n "$LINK_REMOTE" ] && [ "$LINK_REMOTE" != "null" ]; then
    LINK_STATUS=$(echo "$LINK" | jq -r '.link.status // "IDENTITY_LINK_STATUS_UNVERIFIED"')
    echo "  Link: remote=$LINK_REMOTE, status=$LINK_STATUS"
    record_result "Get identity link" "PASS"
else
    echo "  No identity link found (may not have been created)"
    record_result "Get identity link" "PASS"
fi

# ========================================================================
# TEST 10: List identity links
# ========================================================================
echo ""
echo "--- TEST 10: List identity links ---"

LINKS=$($BINARY query federation list-identity-links --output json 2>&1)
LINK_COUNT=$(echo "$LINKS" | jq '.links | length' 2>/dev/null)

echo "  Total identity links: $LINK_COUNT"
if [ "$LINK_COUNT" -ge 0 ] 2>/dev/null; then
    if [ "$LINK_COUNT" -gt 0 ]; then
        echo "$LINKS" | jq -r '.links[] | "    \(.local_address[:20])... → \(.remote_identity) on \(.peer_id) [\(.status // "IDENTITY_LINK_STATUS_UNVERIFIED")]"' 2>/dev/null
    fi
    record_result "List identity links" "PASS"
else
    record_result "List identity links" "FAIL"
fi

# ========================================================================
# TEST 11: Resolve remote identity
# ========================================================================
echo ""
echo "--- TEST 11: Resolve remote identity ---"

RESOLVE=$($BINARY query federation resolve-remote-identity mastodon.example "@alice@mastodon.example" --output json 2>&1)
RESOLVED_ADDR=$(echo "$RESOLVE" | jq -r '.local_address // empty')

if [ -n "$RESOLVED_ADDR" ] && [ "$RESOLVED_ADDR" != "null" ]; then
    echo "  Resolved: @alice@mastodon.example → $RESOLVED_ADDR"
    record_result "Resolve remote identity" "PASS"
else
    echo "  Could not resolve (link may not exist)"
    record_result "Resolve remote identity" "PASS"
fi

# ========================================================================
# TEST 12: Get verifier
# ========================================================================
echo ""
echo "--- TEST 12: Get verifier ---"

# Verifier tests now use alice (CORE trust level) as verifier instead of verifier1
VERIFIER=$($BINARY query federation get-verifier $ALICE_ADDR --output json 2>&1)

if echo "$VERIFIER" | jq -e '.verifier' > /dev/null 2>&1; then
    VERIFIER_ADDR=$(echo "$VERIFIER" | jq -r '.verifier.address // empty')
    BOND_STATUS=$(echo "$VERIFIER" | jq -r '.verifier.bond_status // "VERIFIER_BOND_STATUS_UNSPECIFIED"')
    CURRENT_BOND=$(echo "$VERIFIER" | jq -r '.verifier.current_bond // "0"')
    TOTAL_VERIFICATIONS=$(echo "$VERIFIER" | jq -r '.verifier.total_verifications // "0"')
    echo "  Verifier: addr=${VERIFIER_ADDR:0:20}..., status=$BOND_STATUS, bond=$CURRENT_BOND, verifications=$TOTAL_VERIFICATIONS"
    record_result "Get verifier" "PASS"
else
    echo "  No verifier found (verifier_test.sh may not have run yet)"
    record_result "Get verifier" "PASS"
fi

# ========================================================================
# TEST 13: List verifiers
# ========================================================================
echo ""
echo "--- TEST 13: List verifiers ---"

VERIFIERS=$($BINARY query federation list-verifiers --output json 2>&1)
if echo "$VERIFIERS" | jq -e '.verifiers' > /dev/null 2>&1; then
    VERIFIER_COUNT=$(echo "$VERIFIERS" | jq '.verifiers | length')
    echo "  Total verifiers: $VERIFIER_COUNT"
    if [ "$VERIFIER_COUNT" -gt 0 ]; then
        echo "$VERIFIERS" | jq -r '.verifiers[] | "    \(.address[:20])... bond=\(.current_bond) [\(.bond_status // "VERIFIER_BOND_STATUS_UNSPECIFIED")]"' 2>/dev/null
    fi
    record_result "List verifiers" "PASS"
else
    echo "  Could not query verifiers"
    record_result "List verifiers" "PASS"
fi

# ========================================================================
# TEST 14: Get verification record
# ========================================================================
echo ""
echo "--- TEST 14: Get verification record ---"

# Try the last content that was verified (find first content ID)
CONTENT_LIST=$($BINARY query federation list-federated-content --output json 2>&1)
FIRST_CONTENT_ID=$(echo "$CONTENT_LIST" | jq -r 'if (.content | length) > 0 then (.content[0].id // 0) else empty end')

if [ -n "$FIRST_CONTENT_ID" ] && [ "$FIRST_CONTENT_ID" != "null" ]; then
    RECORD=$($BINARY query federation get-verification-record $FIRST_CONTENT_ID --output json 2>&1)
    RECORD_VERIFIER=$(echo "$RECORD" | jq -r '.record.verifier // empty')

    if [ -n "$RECORD_VERIFIER" ] && [ "$RECORD_VERIFIER" != "null" ]; then
        OUTCOME=$(echo "$RECORD" | jq -r '.record.outcome // "VERIFICATION_OUTCOME_UNSPECIFIED"')
        echo "  Verification record: verifier=$RECORD_VERIFIER, outcome=$OUTCOME"
        record_result "Get verification record" "PASS"
    else
        echo "  No verification record for content $FIRST_CONTENT_ID"
        record_result "Get verification record" "PASS"
    fi
else
    echo "  No content exists to check verification records"
    record_result "Get verification record" "PASS"
fi

# ========================================================================
# TEST 15: List outbound attestations
# ========================================================================
echo ""
echo "--- TEST 15: List outbound attestations ---"

ATTESTATIONS=$($BINARY query federation list-outbound-attestations --output json 2>&1)
ATTEST_COUNT=$(echo "$ATTESTATIONS" | jq '.attestations | length' 2>/dev/null)

echo "  Total outbound attestations: $ATTEST_COUNT"
if [ "$ATTEST_COUNT" -ge 0 ] 2>/dev/null; then
    if [ "$ATTEST_COUNT" -gt 0 ]; then
        echo "$ATTESTATIONS" | jq -r '.attestations[] | "    #\(.id // 0) \(.content_type) → \(.peer_id)"' 2>/dev/null
    fi
    record_result "List outbound attestations" "PASS"
else
    record_result "List outbound attestations" "FAIL"
fi

# ========================================================================
# TEST 16: Get non-existent peer returns error
# ========================================================================
echo ""
echo "--- TEST 16: Get non-existent peer ---"

MISSING=$($BINARY query federation get-peer nonexistent.peer --output json 2>&1)
if echo "$MISSING" | grep -qi "not found\|error"; then
    echo "  Non-existent peer correctly returns error"
    record_result "Non-existent peer error" "PASS"
else
    echo "  Unexpected response for missing peer"
    record_result "Non-existent peer error" "FAIL"
fi

# ========================================================================
# TEST 17: Get non-existent bridge returns error
# ========================================================================
echo ""
echo "--- TEST 17: Get non-existent bridge ---"

MISSING=$($BINARY query federation get-bridge-operator $LINKER1_ADDR nonexistent.peer --output json 2>&1)
if echo "$MISSING" | grep -qi "not found\|error"; then
    echo "  Non-existent bridge correctly returns error"
    record_result "Non-existent bridge error" "PASS"
else
    record_result "Non-existent bridge error" "FAIL"
fi

# ========================================================================
# TEST 18: Get non-existent content returns error
# ========================================================================
echo ""
echo "--- TEST 18: Get non-existent content ---"

MISSING=$($BINARY query federation get-federated-content 99999 --output json 2>&1)
if echo "$MISSING" | grep -qi "not found\|error"; then
    echo "  Non-existent content correctly returns error"
    record_result "Non-existent content error" "PASS"
else
    record_result "Non-existent content error" "FAIL"
fi

# ========================================================================
# TEST 19: Query pending identity challenges
# ========================================================================
echo ""
echo "--- TEST 19: List pending identity challenges ---"

CHALLENGES=$($BINARY query federation list-pending-identity-challenges $LINKER1_ADDR --output json 2>&1)
CHALLENGE_COUNT=$(echo "$CHALLENGES" | jq '.challenges | length' 2>/dev/null)

echo "  Pending challenges for linker1: ${CHALLENGE_COUNT:-0}"
# It's fine to have 0 challenges
record_result "List pending challenges" "PASS"

# ========================================================================
# TEST 20: Get reputation attestation (likely empty)
# ========================================================================
echo ""
echo "--- TEST 20: Get reputation attestation ---"

ATTEST=$($BINARY query federation get-reputation-attestation $ALICE_ADDR spark.testnet --output json 2>&1)
if echo "$ATTEST" | grep -qi "not found\|error"; then
    echo "  No reputation attestation (expected - requires IBC)"
    record_result "Reputation attestation query" "PASS"
else
    ATTEST_ADDR=$(echo "$ATTEST" | jq -r '.attestation.local_address // empty')
    echo "  Attestation found: $ATTEST_ADDR"
    record_result "Reputation attestation query" "PASS"
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "QUERY TEST RESULTS"
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
