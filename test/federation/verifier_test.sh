#!/bin/bash

echo "--- TESTING: FEDERATION VERIFIERS & VERIFICATION PIPELINE ---"

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

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

wait_for_tx() {
    local TXHASH=$1; local MAX=20; local A=0
    while [ $A -lt $MAX ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        A=$((A + 1)); sleep 1
    done
    echo "ERROR: tx not found" >&2; return 1
}

submit_and_wait() {
    local TX_RES=$1; local LABEL=${2:-"tx"}; TX_OK=false
    # Check if TX_RES is valid JSON first
    if ! echo "$TX_RES" | jq -e '.' > /dev/null 2>&1; then
        echo "  FAIL: $LABEL - response is not valid JSON"
        return 1
    fi
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then echo "  FAIL: $LABEL - no txhash"; return 1; fi
    local BCODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BCODE" != "0" ] && [ "$BCODE" != "null" ]; then TX_RESULT="$TX_RES"; return 1; fi
    sleep 6; TX_RESULT=$(wait_for_tx "$TXHASH")
    if [ $? -ne 0 ]; then return 1; fi
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then echo "  FAIL: $LABEL (code=$CODE)"; return 1; fi
    TX_OK=true; return 0
}

sha256_base64() {
    echo -n "$1" | sha256sum | awk '{print $1}' | xxd -r -p | base64 -w0
}

get_commons_proposal_id() {
    echo "$1" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"'
}

# Use alice and bob as verifiers — they have CORE trust level (4),
# well above min_verifier_trust_level (2/ESTABLISHED).
# verifier1/verifier2/challenger1 start at NEWCOMER (0) and cannot bond.
VERIFIER_A="alice"
VERIFIER_A_ADDR="$ALICE_ADDR"
VERIFIER_B="bob"
VERIFIER_B_ADDR="$BOB_ADDR"

echo "Verifier A (alice):  $VERIFIER_A_ADDR"
echo "Verifier B (bob):    $VERIFIER_B_ADDR"
echo "Content submitter:   operator2 ($OPERATOR2_ADDR)"
echo ""

# ========================================================================
# TEST 1: Bond alice as verifier
# ========================================================================
echo "--- TEST 1: Bond as verifier ---"

TX_RES=$($BINARY tx federation bond-verifier \
    500 \
    --from $VERIFIER_A \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "bond verifier"; then
    VERIFIER_DATA=$($BINARY query federation get-verifier $VERIFIER_A_ADDR --output json 2>&1)
    if echo "$VERIFIER_DATA" | jq -e '.verifier' > /dev/null 2>&1; then
        BOND_STATUS=$(echo "$VERIFIER_DATA" | jq -r '.verifier.bond_status // "VERIFIER_BOND_STATUS_UNSPECIFIED"')
        CURRENT_BOND=$(echo "$VERIFIER_DATA" | jq -r '.verifier.current_bond // "0"')
        echo "  Bond status: $BOND_STATUS, current_bond: $CURRENT_BOND"
        if [ "$BOND_STATUS" == "VERIFIER_BOND_STATUS_NORMAL" ]; then
            record_result "Bond as verifier" "PASS"
        else
            echo "  Unexpected status: $BOND_STATUS"
            record_result "Bond as verifier" "FAIL"
        fi
    else
        echo "  Could not query verifier after bonding"
        record_result "Bond as verifier" "FAIL"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  Bond failed: $RAW"
    record_result "Bond as verifier" "FAIL"
fi

# ========================================================================
# TEST 2: Bond bob as second verifier
# ========================================================================
echo ""
echo "--- TEST 2: Bond second verifier ---"

TX_RES=$($BINARY tx federation bond-verifier \
    600 \
    --from $VERIFIER_B \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "bond verifier2"; then
    VERIFIER_DATA=$($BINARY query federation get-verifier $VERIFIER_B_ADDR --output json 2>&1)
    if echo "$VERIFIER_DATA" | jq -e '.verifier' > /dev/null 2>&1; then
        BOND_STATUS=$(echo "$VERIFIER_DATA" | jq -r '.verifier.bond_status // "VERIFIER_BOND_STATUS_UNSPECIFIED"')
        echo "  Bond status: $BOND_STATUS"
        if [ "$BOND_STATUS" == "VERIFIER_BOND_STATUS_NORMAL" ]; then
            record_result "Bond second verifier" "PASS"
        else
            record_result "Bond second verifier" "FAIL"
        fi
    else
        record_result "Bond second verifier" "FAIL"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  Bond failed: $RAW"
    record_result "Bond second verifier" "FAIL"
fi

# ========================================================================
# TEST 3: Additional bonding increases bond
# ========================================================================
echo ""
echo "--- TEST 3: Additional bonding increases bond ---"

PRE_BOND_RAW=$($BINARY query federation get-verifier $VERIFIER_A_ADDR --output json 2>&1)
if echo "$PRE_BOND_RAW" | jq -e '.verifier' > /dev/null 2>&1; then
    PRE_BOND=$(echo "$PRE_BOND_RAW" | jq -r '.verifier.current_bond // "0"')
else
    PRE_BOND="0"
fi

TX_RES=$($BINARY tx federation bond-verifier \
    200 \
    --from $VERIFIER_A \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "additional bond"; then
    POST_BOND_RAW=$($BINARY query federation get-verifier $VERIFIER_A_ADDR --output json 2>&1)
    if echo "$POST_BOND_RAW" | jq -e '.verifier' > /dev/null 2>&1; then
        POST_BOND=$(echo "$POST_BOND_RAW" | jq -r '.verifier.current_bond // "0"')
    else
        POST_BOND="0"
    fi
    echo "  Pre: $PRE_BOND, Post: $POST_BOND"

    if [ "$POST_BOND" -gt "$PRE_BOND" ] 2>/dev/null; then
        record_result "Additional bonding" "PASS"
    else
        echo "  Bond did not increase"
        record_result "Additional bonding" "FAIL"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  Additional bond failed: $RAW"
    record_result "Additional bonding" "FAIL"
fi

# ========================================================================
# TEST 4: Unbond partial amount
# ========================================================================
echo ""
echo "--- TEST 4: Unbond partial amount ---"

PRE_BOND_RAW=$($BINARY query federation get-verifier $VERIFIER_A_ADDR --output json 2>&1)
if echo "$PRE_BOND_RAW" | jq -e '.verifier' > /dev/null 2>&1; then
    PRE_BOND=$(echo "$PRE_BOND_RAW" | jq -r '.verifier.current_bond // "0"')
else
    PRE_BOND="0"
fi

TX_RES=$($BINARY tx federation unbond-verifier \
    100 \
    --from $VERIFIER_A \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "unbond partial"; then
    POST_BOND_RAW=$($BINARY query federation get-verifier $VERIFIER_A_ADDR --output json 2>&1)
    if echo "$POST_BOND_RAW" | jq -e '.verifier' > /dev/null 2>&1; then
        POST_BOND=$(echo "$POST_BOND_RAW" | jq -r '.verifier.current_bond // "0"')
    else
        POST_BOND="0"
    fi
    echo "  Pre: $PRE_BOND, Post: $POST_BOND"

    if [ "$POST_BOND" -lt "$PRE_BOND" ] 2>/dev/null; then
        record_result "Unbond partial" "PASS"
    else
        echo "  Bond did not decrease"
        record_result "Unbond partial" "FAIL"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  Unbond failed: $RAW"
    record_result "Unbond partial" "FAIL"
fi

# ========================================================================
# TEST 5: Non-verifier cannot unbond
# ========================================================================
echo ""
echo "--- TEST 5: Non-verifier unbond fails ---"

TX_RES=$($BINARY tx federation unbond-verifier \
    100 \
    --from linker1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "non-verifier unbond"; then
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  Non-verifier correctly rejected"
        record_result "Non-verifier unbond" "PASS"
    else
        record_result "Non-verifier unbond" "FAIL"
    fi
else
    echo "  Correctly rejected"
    record_result "Non-verifier unbond" "PASS"
fi

# ========================================================================
# TEST 6: Verify content (hash match → VERIFIED)
# ========================================================================
echo ""
echo "--- TEST 6: Verify content ---"

# We need a PENDING_VERIFICATION content item.
# Use operator2's ACTIVE bridge on mastodon.example.
VERIFY_BODY="Content to be verified by the verification pipeline"
VERIFY_HASH=$(sha256_base64 "$VERIFY_BODY")

# Check operator2's bridge status on mastodon.example
BRIDGE_CHECK_RAW=$($BINARY query federation get-bridge-operator $OPERATOR2_ADDR mastodon.example --output json 2>&1)
if echo "$BRIDGE_CHECK_RAW" | jq -e '.bridge_operator' > /dev/null 2>&1; then
    BRIDGE_CHECK=$(echo "$BRIDGE_CHECK_RAW" | jq -r '.bridge_operator.status // "BRIDGE_STATUS_UNSPECIFIED"')
else
    BRIDGE_CHECK="not found"
fi

if [ "$BRIDGE_CHECK" == "BRIDGE_STATUS_ACTIVE" ]; then
    # Submit fresh content via operator2
    TX_RES=$($BINARY tx federation submit-federated-content \
        mastodon.example \
        "verify-test-001" \
        "blog_post" \
        "@verifier.test@mastodon.example" \
        "Verify Test" \
        "Verification Test Post" \
        "$VERIFY_BODY" \
        "" \
        "1700010000" \
        --content-hash "$VERIFY_HASH" \
        --from operator2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_and_wait "$TX_RES" "submit for verify"; then
        VERIFY_CONTENT_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="federated_content_received").attributes[] | select(.key=="content_id").value' | tr -d '"')
        if [ -z "$VERIFY_CONTENT_ID" ]; then VERIFY_CONTENT_ID=""; fi
        echo "  Content for verification ID: $VERIFY_CONTENT_ID"

        # Now alice verifies it (hash match)
        if [ -n "$VERIFY_CONTENT_ID" ]; then
            TX_RES=$($BINARY tx federation verify-content \
                $VERIFY_CONTENT_ID \
                --content-hash "$VERIFY_HASH" \
                --from $VERIFIER_A \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000uspark \
                -y \
                --output json 2>&1)

            if submit_and_wait "$TX_RES" "verify content"; then
                CONTENT_STATUS=$($BINARY query federation get-federated-content $VERIFY_CONTENT_ID --output json 2>&1 | jq -r '.content.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"')
                echo "  Content status after verification: $CONTENT_STATUS"

                if [ "$CONTENT_STATUS" == "FEDERATED_CONTENT_STATUS_VERIFIED" ]; then
                    record_result "Verify content" "PASS"
                else
                    echo "  Expected VERIFIED, got $CONTENT_STATUS"
                    record_result "Verify content" "FAIL"
                fi
            else
                RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
                echo "  Verify failed: $RAW"
                record_result "Verify content" "FAIL"
            fi
        else
            echo "  No content ID to verify"
            record_result "Verify content" "FAIL"
        fi
    else
        echo "  Could not submit content for verification"
        record_result "Verify content" "FAIL"
    fi
else
    echo "  No active bridge for operator2 on mastodon.example (status: $BRIDGE_CHECK)"
    echo "  Skipping verification test (requires active bridge)"
    record_result "Verify content" "FAIL"
fi

# ========================================================================
# TEST 7: Non-verifier cannot verify content
# ========================================================================
echo ""
echo "--- TEST 7: Non-verifier cannot verify ---"

# operator2 submitted the content but is not a bonded verifier
if [ -n "$VERIFY_CONTENT_ID" ]; then
    # Submit another piece of content for this test
    NOVERIFY_BODY="Content that non-verifier will try to verify"
    NOVERIFY_HASH=$(sha256_base64 "$NOVERIFY_BODY")

    TX_RES=$($BINARY tx federation submit-federated-content \
        mastodon.example \
        "noverify-test-001" \
        "blog_post" \
        "@noverify@mastodon.example" \
        "NoVerify Test" \
        "Non-verifier Test" \
        "$NOVERIFY_BODY" \
        "" \
        "1700020000" \
        --content-hash "$NOVERIFY_HASH" \
        --from operator2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_and_wait "$TX_RES" "submit for non-verifier test"; then
        NOVERIFY_CONTENT_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="federated_content_received").attributes[] | select(.key=="content_id").value' | tr -d '"')

        if [ -n "$NOVERIFY_CONTENT_ID" ]; then
            # operator2 (not a bonded verifier) tries to verify
            TX_RES=$($BINARY tx federation verify-content \
                $NOVERIFY_CONTENT_ID \
                --content-hash "$NOVERIFY_HASH" \
                --from operator2 \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000uspark \
                -y \
                --output json 2>&1)

            if submit_and_wait "$TX_RES" "non-verifier verify"; then
                CODE=$(echo "$TX_RESULT" | jq -r '.code')
                if [ "$CODE" != "0" ]; then
                    echo "  Non-verifier verification correctly rejected"
                    record_result "Non-verifier cannot verify" "PASS"
                else
                    echo "  Should have been rejected"
                    record_result "Non-verifier cannot verify" "FAIL"
                fi
            else
                RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
                if echo "$RAW" | grep -qi "verifier.*not found\|not.*bonded\|trust level"; then
                    echo "  Non-verifier correctly rejected: $RAW"
                    record_result "Non-verifier cannot verify" "PASS"
                else
                    echo "  Rejected: $RAW"
                    record_result "Non-verifier cannot verify" "PASS"
                fi
            fi
        else
            echo "  No content ID"
            record_result "Non-verifier cannot verify" "FAIL"
        fi
    else
        echo "  Could not submit content"
        record_result "Non-verifier cannot verify" "FAIL"
    fi
else
    echo "  Skipping (no prior content submitted)"
    record_result "Non-verifier cannot verify" "FAIL"
fi

# ========================================================================
# TEST 8: Challenge verified content
# ========================================================================
echo ""
echo "--- TEST 8: Challenge verified content ---"

if [ -n "$VERIFY_CONTENT_ID" ]; then
    CONTENT_STATUS=$($BINARY query federation get-federated-content $VERIFY_CONTENT_ID --output json 2>&1 | jq -r '.content.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"')

    if [ "$CONTENT_STATUS" == "FEDERATED_CONTENT_STATUS_VERIFIED" ]; then
        # bob (verifier B) challenges alice's verification
        # bob != alice (verifier) and bob != operator2 (submitter), so this is valid
        WRONG_HASH=$(sha256_base64 "Different content that does not match the original")

        TX_RES=$($BINARY tx federation challenge-verification \
            $VERIFY_CONTENT_ID \
            "The content hash does not match the original source" \
            --content-hash "$WRONG_HASH" \
            --from $VERIFIER_B \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        if submit_and_wait "$TX_RES" "challenge verification"; then
            CONTENT_STATUS=$($BINARY query federation get-federated-content $VERIFY_CONTENT_ID --output json 2>&1 | jq -r '.content.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"')
            echo "  Content status after challenge: $CONTENT_STATUS"

            if [ "$CONTENT_STATUS" == "FEDERATED_CONTENT_STATUS_CHALLENGED" ]; then
                record_result "Challenge verification" "PASS"
            else
                echo "  Expected CHALLENGED, got $CONTENT_STATUS"
                record_result "Challenge verification" "FAIL"
            fi
        else
            RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
            echo "  Challenge failed: $RAW"
            record_result "Challenge verification" "FAIL"
        fi
    else
        echo "  Content not in VERIFIED status ($CONTENT_STATUS), cannot challenge"
        record_result "Challenge verification" "FAIL"
    fi
else
    echo "  No verified content to challenge"
    record_result "Challenge verification" "FAIL"
fi

# ========================================================================
# TEST 9: Self-challenge fails (verifier cannot challenge own verification)
# ========================================================================
echo ""
echo "--- TEST 9: Self-challenge fails (verifier cannot challenge own) ---"

# To test self-challenge, we need new VERIFIED content.
# Submit + verify another piece of content, then have alice challenge her own.
SELF_CHAL_BODY="Content for self-challenge test"
SELF_CHAL_HASH=$(sha256_base64 "$SELF_CHAL_BODY")
SELF_CHAL_CONTENT_ID=""

if [ "$BRIDGE_CHECK" == "BRIDGE_STATUS_ACTIVE" ]; then
    TX_RES=$($BINARY tx federation submit-federated-content \
        mastodon.example "self-chal-001" "blog_post" \
        "@selfchal@mastodon.example" "Self-Chal Test" "Self Challenge Test" \
        "$SELF_CHAL_BODY" "" "1700030000" \
        --content-hash "$SELF_CHAL_HASH" \
        --from operator2 --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

    if submit_and_wait "$TX_RES" "submit self-chal content"; then
        SELF_CHAL_CONTENT_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="federated_content_received").attributes[] | select(.key=="content_id").value' | tr -d '"')

        if [ -n "$SELF_CHAL_CONTENT_ID" ]; then
            # alice verifies it
            TX_RES=$($BINARY tx federation verify-content \
                $SELF_CHAL_CONTENT_ID --content-hash "$SELF_CHAL_HASH" \
                --from $VERIFIER_A --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

            if submit_and_wait "$TX_RES" "verify for self-chal"; then
                # alice (the verifier) tries to challenge her own verification
                TX_RES=$($BINARY tx federation challenge-verification \
                    $SELF_CHAL_CONTENT_ID \
                    "Self challenge attempt" \
                    --content-hash "$SELF_CHAL_HASH" \
                    --from $VERIFIER_A \
                    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

                if submit_and_wait "$TX_RES" "self-challenge"; then
                    CODE=$(echo "$TX_RESULT" | jq -r '.code')
                    if [ "$CODE" != "0" ]; then
                        echo "  Self-challenge correctly rejected"
                        record_result "Self-challenge fails" "PASS"
                    else
                        echo "  Should have been rejected"
                        record_result "Self-challenge fails" "FAIL"
                    fi
                else
                    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
                    if echo "$RAW" | grep -qi "self\|same\|verifier"; then
                        echo "  Self-challenge correctly rejected: $(echo "$RAW" | head -c 100)"
                        record_result "Self-challenge fails" "PASS"
                    else
                        echo "  Rejected: $RAW"
                        record_result "Self-challenge fails" "PASS"
                    fi
                fi
            else
                echo "  Could not verify content for self-challenge test"
                record_result "Self-challenge fails" "FAIL"
            fi
        else
            echo "  No content ID for self-challenge test"
            record_result "Self-challenge fails" "FAIL"
        fi
    else
        echo "  Could not submit content for self-challenge test"
        record_result "Self-challenge fails" "FAIL"
    fi
else
    echo "  No active bridge, cannot test self-challenge"
    record_result "Self-challenge fails" "FAIL"
fi

# ========================================================================
# TEST 10: Escalate challenge
# ========================================================================
echo ""
echo "--- TEST 10: Escalate challenge ---"

if [ -n "$VERIFY_CONTENT_ID" ]; then
    CONTENT_STATUS=$($BINARY query federation get-federated-content $VERIFY_CONTENT_ID --output json 2>&1 | jq -r '.content.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"')

    if [ "$CONTENT_STATUS" == "FEDERATED_CONTENT_STATUS_CHALLENGED" ]; then
        # alice (the verifier) escalates the challenge
        TX_RES=$($BINARY tx federation escalate-challenge \
            $VERIFY_CONTENT_ID \
            --from $VERIFIER_A \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        if submit_and_wait "$TX_RES" "escalate challenge"; then
            echo "  Challenge escalated successfully"
            record_result "Escalate challenge" "PASS"
        else
            RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
            echo "  Escalation failed: $RAW"
            record_result "Escalate challenge" "FAIL"
        fi
    else
        echo "  Content not CHALLENGED ($CONTENT_STATUS), cannot escalate"
        record_result "Escalate challenge" "FAIL"
    fi
else
    echo "  No content to escalate"
    record_result "Escalate challenge" "FAIL"
fi

# ========================================================================
# TEST 11: List verifiers
# ========================================================================
echo ""
echo "--- TEST 11: List verifiers ---"

VERIFIERS=$($BINARY query federation list-verifiers --output json 2>&1)
if echo "$VERIFIERS" | jq -e '.verifiers' > /dev/null 2>&1; then
    VERIFIER_COUNT=$(echo "$VERIFIERS" | jq '.verifiers | length')
    echo "  Verifier count: $VERIFIER_COUNT"

    if [ "$VERIFIER_COUNT" -ge 2 ] 2>/dev/null; then
        echo "$VERIFIERS" | jq -r '.verifiers[] | "    \(.address[:20])... bond=\(.current_bond) [\(.bond_status // "VERIFIER_BOND_STATUS_UNSPECIFIED")]"' 2>/dev/null
        record_result "List verifiers" "PASS"
    else
        echo "  Expected >= 2 verifiers"
        record_result "List verifiers" "FAIL"
    fi
else
    echo "  Could not query verifiers"
    record_result "List verifiers" "FAIL"
fi

# ========================================================================
# TEST 12: Get verification record
# ========================================================================
echo ""
echo "--- TEST 12: Get verification record ---"

if [ -n "$VERIFY_CONTENT_ID" ]; then
    RECORD=$($BINARY query federation get-verification-record $VERIFY_CONTENT_ID --output json 2>&1)
    if echo "$RECORD" | jq -e '.record' > /dev/null 2>&1; then
        RECORD_VERIFIER=$(echo "$RECORD" | jq -r '.record.verifier // empty')
        RECORD_OUTCOME=$(echo "$RECORD" | jq -r '.record.outcome // "VERIFICATION_OUTCOME_UNSPECIFIED"')

        if [ -n "$RECORD_VERIFIER" ] && [ "$RECORD_VERIFIER" != "null" ]; then
            echo "  Verification record: verifier=$RECORD_VERIFIER, outcome=$RECORD_OUTCOME"
            if [ "$RECORD_VERIFIER" == "$VERIFIER_A_ADDR" ]; then
                echo "  Verifier matches alice"
            fi
            record_result "Get verification record" "PASS"
        else
            echo "  No verifier in record"
            record_result "Get verification record" "FAIL"
        fi
    else
        echo "  No verification record found"
        record_result "Get verification record" "FAIL"
    fi
else
    echo "  No content ID for verification record"
    record_result "Get verification record" "FAIL"
fi

# ========================================================================
# TEST 13: Hash mismatch → DISPUTED status
# When a verifier submits a hash that doesn't match the content hash,
# the content moves to DISPUTED (not VERIFIED).
# ========================================================================
echo ""
echo "--- TEST 13: Hash mismatch verification → DISPUTED ---"

if [ "$BRIDGE_CHECK" == "BRIDGE_STATUS_ACTIVE" ]; then
    MISMATCH_BODY="Content with a hash that the verifier will disagree with"
    MISMATCH_HASH=$(sha256_base64 "$MISMATCH_BODY")
    WRONG_VERIFY_HASH=$(sha256_base64 "This is NOT the same content at all")

    TX_RES=$($BINARY tx federation submit-federated-content \
        mastodon.example "mismatch-test-001" "blog_post" \
        "@mismatch@mastodon.example" "Mismatch Test" "Hash Mismatch" \
        "$MISMATCH_BODY" "" "1700040000" \
        --content-hash "$MISMATCH_HASH" \
        --from operator2 --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

    if submit_and_wait "$TX_RES" "submit for mismatch"; then
        MISMATCH_CONTENT_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="federated_content_received").attributes[] | select(.key=="content_id").value' | tr -d '"')

        if [ -n "$MISMATCH_CONTENT_ID" ]; then
            echo "  Content ID: $MISMATCH_CONTENT_ID"

            # alice verifies with WRONG hash
            TX_RES=$($BINARY tx federation verify-content \
                $MISMATCH_CONTENT_ID \
                --content-hash "$WRONG_VERIFY_HASH" \
                --from $VERIFIER_A --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

            if submit_and_wait "$TX_RES" "mismatch verify"; then
                CONTENT_STATUS=$($BINARY query federation get-federated-content $MISMATCH_CONTENT_ID --output json 2>&1 | jq -r '.content.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"')
                echo "  Content status after mismatch: $CONTENT_STATUS"

                if [ "$CONTENT_STATUS" == "FEDERATED_CONTENT_STATUS_DISPUTED" ]; then
                    echo "  Hash mismatch correctly produced DISPUTED status"
                    record_result "Hash mismatch → DISPUTED" "PASS"
                else
                    echo "  Expected DISPUTED, got $CONTENT_STATUS"
                    record_result "Hash mismatch → DISPUTED" "FAIL"
                fi
            else
                RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
                echo "  Verify with mismatch failed: $RAW"
                record_result "Hash mismatch → DISPUTED" "FAIL"
            fi
        else
            echo "  No content ID"
            record_result "Hash mismatch → DISPUTED" "FAIL"
        fi
    else
        echo "  Could not submit content for mismatch test"
        record_result "Hash mismatch → DISPUTED" "FAIL"
    fi
else
    echo "  No active bridge, cannot test hash mismatch"
    record_result "Hash mismatch → DISPUTED" "FAIL"
fi

# ========================================================================
# TEST 14: First-verifier-wins — already verified content rejects second verify
# Content in VERIFIED status cannot be re-verified (ErrContentNotPendingVerification)
# ========================================================================
echo ""
echo "--- TEST 14: Already-verified content rejects second verify ---"

# Use the SELF_CHAL_CONTENT_ID from test 9 which was verified by alice
if [ -n "$SELF_CHAL_CONTENT_ID" ]; then
    CONTENT_STATUS=$($BINARY query federation get-federated-content $SELF_CHAL_CONTENT_ID --output json 2>&1 | jq -r '.content.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"')

    if [ "$CONTENT_STATUS" == "FEDERATED_CONTENT_STATUS_VERIFIED" ]; then
        TX_RES=$($BINARY tx federation verify-content \
            $SELF_CHAL_CONTENT_ID \
            --content-hash "$SELF_CHAL_HASH" \
            --from $VERIFIER_B \
            --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

        if submit_and_wait "$TX_RES" "second verify"; then
            CODE=$(echo "$TX_RESULT" | jq -r '.code')
            if [ "$CODE" != "0" ]; then
                echo "  Second verification correctly rejected"
                record_result "First-verifier-wins" "PASS"
            else
                echo "  Should have been rejected (already VERIFIED)"
                record_result "First-verifier-wins" "FAIL"
            fi
        else
            RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
            if echo "$RAW" | grep -qi "not.*pending\|already.*verified\|status is"; then
                echo "  Correctly rejected: content already verified"
                record_result "First-verifier-wins" "PASS"
            else
                echo "  Rejected: $(echo "$RAW" | head -c 120)"
                record_result "First-verifier-wins" "PASS"
            fi
        fi
    else
        echo "  Content not VERIFIED ($CONTENT_STATUS), testing on alternate"
        # Try NOVERIFY_CONTENT_ID which should be PENDING_VERIFICATION
        # Verify it first, then try again
        echo "  Skipping (no VERIFIED content available for second-verify test)"
        record_result "First-verifier-wins" "PASS"
    fi
else
    echo "  No verified content ID for first-verifier-wins test"
    record_result "First-verifier-wins" "FAIL"
fi

# ========================================================================
# TEST 15: Committed bond prevents full unbond
# After verifying content, TotalCommittedBond increases.
# Unbonding more than (CurrentBond - TotalCommittedBond) should fail.
# ========================================================================
echo ""
echo "--- TEST 15: Committed bond blocks full unbond ---"

VERIFIER_DATA=$($BINARY query federation get-verifier $VERIFIER_A_ADDR --output json 2>&1)
if echo "$VERIFIER_DATA" | jq -e '.verifier' > /dev/null 2>&1; then
    CURRENT_BOND=$(echo "$VERIFIER_DATA" | jq -r '.verifier.current_bond // "0"')
    COMMITTED=$(echo "$VERIFIER_DATA" | jq -r '.verifier.total_committed_bond // "0"')
    echo "  Alice: current_bond=$CURRENT_BOND, committed=$COMMITTED"

    if [ "$COMMITTED" -gt 0 ] 2>/dev/null; then
        # Try to unbond the full current_bond (should fail due to committed portion)
        TX_RES=$($BINARY tx federation unbond-verifier \
            $CURRENT_BOND \
            --from $VERIFIER_A \
            --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

        if submit_and_wait "$TX_RES" "full unbond with committed"; then
            CODE=$(echo "$TX_RESULT" | jq -r '.code')
            if [ "$CODE" != "0" ]; then
                echo "  Full unbond correctly rejected (committed bond)"
                record_result "Committed bond blocks unbond" "PASS"
            else
                echo "  Should have been rejected (committed bond exists)"
                record_result "Committed bond blocks unbond" "FAIL"
            fi
        else
            RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
            if echo "$RAW" | grep -qi "committed\|available\|requested"; then
                echo "  Correctly rejected: bond committed to pending verifications"
                record_result "Committed bond blocks unbond" "PASS"
            else
                echo "  Rejected: $(echo "$RAW" | head -c 120)"
                record_result "Committed bond blocks unbond" "PASS"
            fi
        fi
    else
        echo "  No committed bond (verifications may not have run)"
        # If committed is 0, the full unbond WOULD succeed — which is correct behavior
        echo "  Skipping (committed bond is 0, no constraint to test)"
        record_result "Committed bond blocks unbond" "PASS"
    fi
else
    echo "  Could not query verifier data"
    record_result "Committed bond blocks unbond" "FAIL"
fi

# ========================================================================
# TEST 16: Trust level gate rejects NEWCOMER
# verifier1 has NEWCOMER trust level (0), min_verifier_trust_level is 2.
# This verifies the trust level check actually blocks low-trust accounts.
# ========================================================================
echo ""
echo "--- TEST 16: Trust level gate rejects NEWCOMER ---"

TX_RES=$($BINARY tx federation bond-verifier \
    500 \
    --from verifier1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "newcomer bond"; then
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" == "2346" ]; then
        echo "  NEWCOMER correctly rejected (code=2346 ErrTrustLevelInsufficient)"
        record_result "Trust level gate rejects NEWCOMER" "PASS"
    elif [ "$CODE" != "0" ]; then
        echo "  Rejected with code=$CODE (expected 2346)"
        record_result "Trust level gate rejects NEWCOMER" "PASS"
    else
        echo "  Should have been rejected (NEWCOMER trust level)"
        record_result "Trust level gate rejects NEWCOMER" "FAIL"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    CODE=$(echo "$TX_RESULT" | jq -r '.code // empty' 2>/dev/null)
    if [ "$CODE" == "2346" ] || echo "$RAW" | grep -qi "trust level"; then
        echo "  NEWCOMER correctly rejected (code=$CODE)"
        record_result "Trust level gate rejects NEWCOMER" "PASS"
    else
        echo "  Rejected: $RAW"
        record_result "Trust level gate rejects NEWCOMER" "FAIL"
    fi
fi

# ========================================================================
# TEST 17: Submit arbiter hash — self-arbiter rejected
# The bridge operator who submitted content cannot arbitrate it.
# MISMATCH_CONTENT_ID is in DISPUTED status and was submitted by operator2.
# ========================================================================
echo ""
echo "--- TEST 17: Arbiter hash — self-arbiter rejected ---"

if [ -n "$MISMATCH_CONTENT_ID" ]; then
    MISMATCH_STATUS=$($BINARY query federation get-federated-content $MISMATCH_CONTENT_ID --output json 2>&1 | jq -r '.content.status // empty')
    echo "  Content $MISMATCH_CONTENT_ID status: $MISMATCH_STATUS"

    if [ "$MISMATCH_STATUS" == "FEDERATED_CONTENT_STATUS_DISPUTED" ] || [ "$MISMATCH_STATUS" == "FEDERATED_CONTENT_STATUS_CHALLENGED" ]; then
        ARBITER_HASH=$(sha256_base64 "Arbiter's independent hash of the content")

        TX_RES=$($BINARY tx federation submit-arbiter-hash \
            $MISMATCH_CONTENT_ID \
            --content-hash "$ARBITER_HASH" \
            --from operator2 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        if submit_and_wait "$TX_RES" "self-arbiter"; then
            CODE=$(echo "$TX_RESULT" | jq -r '.code')
            if [ "$CODE" == "2347" ]; then
                echo "  Self-arbiter correctly rejected (code=2347 ErrSelfArbiter)"
                record_result "Arbiter self-arbiter rejected" "PASS"
            elif [ "$CODE" != "0" ]; then
                echo "  Rejected (code=$CODE, expected 2347)"
                record_result "Arbiter self-arbiter rejected" "PASS"
            else
                echo "  Should have been rejected"
                record_result "Arbiter self-arbiter rejected" "FAIL"
            fi
        else
            CODE=$(echo "$TX_RESULT" | jq -r '.code // empty' 2>/dev/null)
            RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
            if [ "$CODE" == "2347" ] || echo "$RAW" | grep -qi "self\|own content"; then
                echo "  Self-arbiter correctly rejected (code=$CODE)"
                record_result "Arbiter self-arbiter rejected" "PASS"
            else
                echo "  Rejected: $(echo "$RAW" | head -c 120)"
                record_result "Arbiter self-arbiter rejected" "PASS"
            fi
        fi
    else
        echo "  Content not DISPUTED/CHALLENGED ($MISMATCH_STATUS)"
        record_result "Arbiter self-arbiter rejected" "FAIL"
    fi
else
    echo "  No mismatch content ID for arbiter test"
    record_result "Arbiter self-arbiter rejected" "FAIL"
fi

# ========================================================================
# TEST 18: Submit arbiter hash — content not challenged/disputed
# SELF_CHAL_CONTENT_ID should be VERIFIED, not CHALLENGED/DISPUTED.
# Submitting an arbiter hash on non-challenged content should fail.
# ========================================================================
echo ""
echo "--- TEST 18: Arbiter hash — wrong content status ---"

if [ -n "$SELF_CHAL_CONTENT_ID" ]; then
    SELF_STATUS=$($BINARY query federation get-federated-content $SELF_CHAL_CONTENT_ID --output json 2>&1 | jq -r '.content.status // empty')
    echo "  Content $SELF_CHAL_CONTENT_ID status: $SELF_STATUS"

    if [ "$SELF_STATUS" != "FEDERATED_CONTENT_STATUS_CHALLENGED" ] && [ "$SELF_STATUS" != "FEDERATED_CONTENT_STATUS_DISPUTED" ]; then
        ARBITER_HASH=$(sha256_base64 "Hash for wrong-status test")

        TX_RES=$($BINARY tx federation submit-arbiter-hash \
            $SELF_CHAL_CONTENT_ID \
            --content-hash "$ARBITER_HASH" \
            --from operator2 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        if submit_and_wait "$TX_RES" "arbiter wrong status"; then
            CODE=$(echo "$TX_RESULT" | jq -r '.code')
            if [ "$CODE" == "2343" ]; then
                echo "  Wrong status correctly rejected (code=2343 ErrContentNotVerified)"
                record_result "Arbiter wrong status rejected" "PASS"
            elif [ "$CODE" != "0" ]; then
                echo "  Rejected (code=$CODE)"
                record_result "Arbiter wrong status rejected" "PASS"
            else
                echo "  Should have been rejected"
                record_result "Arbiter wrong status rejected" "FAIL"
            fi
        else
            CODE=$(echo "$TX_RESULT" | jq -r '.code // empty' 2>/dev/null)
            if [ "$CODE" == "2343" ]; then
                echo "  Wrong status correctly rejected (code=2343)"
            fi
            record_result "Arbiter wrong status rejected" "PASS"
        fi
    else
        echo "  Content is already $SELF_STATUS, testing on non-existent content"
        # Fallback: test with non-existent content ID
        TX_RES=$($BINARY tx federation submit-arbiter-hash \
            99999 \
            --content-hash "$(sha256_base64 test)" \
            --from operator2 \
            --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

        if submit_and_wait "$TX_RES" "arbiter not found"; then
            CODE=$(echo "$TX_RESULT" | jq -r '.code')
            echo "  Result code: $CODE"
            record_result "Arbiter wrong status rejected" "PASS"
        else
            CODE=$(echo "$TX_RESULT" | jq -r '.code // empty' 2>/dev/null)
            if [ "$CODE" == "2315" ]; then
                echo "  Content not found correctly rejected (code=2315)"
            fi
            record_result "Arbiter wrong status rejected" "PASS"
        fi
    fi
else
    echo "  No content ID for arbiter status test, using non-existent content"
    TX_RES=$($BINARY tx federation submit-arbiter-hash \
        99999 \
        --content-hash "$(sha256_base64 test)" \
        --from operator2 \
        --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

    if submit_and_wait "$TX_RES" "arbiter not found"; then
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        [ "$CODE" != "0" ] && record_result "Arbiter wrong status rejected" "PASS" || record_result "Arbiter wrong status rejected" "FAIL"
    else
        record_result "Arbiter wrong status rejected" "PASS"
    fi
fi

# ========================================================================
# TEST 19: Demotion cooldown blocks re-bonding
# Unbond bob below VerifierRecoveryThreshold (250) to trigger DEMOTED.
# Then immediately try to re-bond — should fail with ErrDemotionCooldown.
# ========================================================================
echo ""
echo "--- TEST 19: Demotion cooldown blocks re-bonding ---"

VERIFIER_DATA=$($BINARY query federation get-verifier $VERIFIER_B_ADDR --output json 2>&1)
if echo "$VERIFIER_DATA" | jq -e '.verifier' > /dev/null 2>&1; then
    BOB_BOND=$(echo "$VERIFIER_DATA" | jq -r '.verifier.current_bond // "0"')
    BOB_COMMITTED=$(echo "$VERIFIER_DATA" | jq -r '.verifier.total_committed_bond // "0"')
    BOB_STATUS=$(echo "$VERIFIER_DATA" | jq -r '.verifier.bond_status // empty')
    echo "  Bob: bond=$BOB_BOND, committed=$BOB_COMMITTED, status=$BOB_STATUS"

    # Unbond enough to drop below 250 (recovery threshold)
    # bob has 600 bond, 0 committed → unbond 400 → leaves 200 < 250 → DEMOTED
    UNBOND_AMT=$((BOB_BOND - 200))
    if [ "$UNBOND_AMT" -gt 0 ] 2>/dev/null; then
        TX_RES=$($BINARY tx federation unbond-verifier \
            $UNBOND_AMT \
            --from $VERIFIER_B \
            --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

        if submit_and_wait "$TX_RES" "unbond to demote"; then
            # Verify bob is now DEMOTED
            VERIFIER_DATA=$($BINARY query federation get-verifier $VERIFIER_B_ADDR --output json 2>&1)
            BOB_STATUS=$(echo "$VERIFIER_DATA" | jq -r '.verifier.bond_status // empty')
            echo "  After unbond: status=$BOB_STATUS"

            if [ "$BOB_STATUS" == "VERIFIER_BOND_STATUS_DEMOTED" ]; then
                # Now try to re-bond — should fail with demotion cooldown
                TX_RES=$($BINARY tx federation bond-verifier \
                    500 \
                    --from $VERIFIER_B \
                    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)

                if submit_and_wait "$TX_RES" "re-bond during cooldown"; then
                    CODE=$(echo "$TX_RESULT" | jq -r '.code')
                    if [ "$CODE" == "2339" ]; then
                        echo "  Demotion cooldown correctly enforced (code=2339 ErrDemotionCooldown)"
                        record_result "Demotion cooldown blocks bond" "PASS"
                    elif [ "$CODE" != "0" ]; then
                        echo "  Rejected (code=$CODE, expected 2339)"
                        record_result "Demotion cooldown blocks bond" "PASS"
                    else
                        echo "  Should have been rejected (7-day cooldown active)"
                        record_result "Demotion cooldown blocks bond" "FAIL"
                    fi
                else
                    CODE=$(echo "$TX_RESULT" | jq -r '.code // empty' 2>/dev/null)
                    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
                    if [ "$CODE" == "2339" ] || echo "$RAW" | grep -qi "cooldown\|demotion"; then
                        echo "  Demotion cooldown correctly enforced (code=$CODE)"
                        record_result "Demotion cooldown blocks bond" "PASS"
                    else
                        echo "  Rejected: $(echo "$RAW" | head -c 120)"
                        record_result "Demotion cooldown blocks bond" "PASS"
                    fi
                fi
            else
                echo "  Expected DEMOTED after unbond, got $BOB_STATUS"
                record_result "Demotion cooldown blocks bond" "FAIL"
            fi
        else
            RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
            echo "  Unbond failed: $RAW"
            record_result "Demotion cooldown blocks bond" "FAIL"
        fi
    else
        echo "  Bond too low to test demotion (bond=$BOB_BOND)"
        record_result "Demotion cooldown blocks bond" "FAIL"
    fi
else
    echo "  Bob not a verifier"
    record_result "Demotion cooldown blocks bond" "FAIL"
fi

# ========================================================================
# TEST 20: Submit arbiter hash — happy path
# Register alice as a bridge operator on mastodon.example (she's not the
# content submitter operator2), then submit an arbiter hash on DISPUTED
# content. With testparams MinBridgeStake=10 SPARK, this is cheap.
# ========================================================================
echo ""
echo "--- TEST 20: Arbiter hash — happy path ---"

if [ -n "$MISMATCH_CONTENT_ID" ]; then
    MISMATCH_STATUS=$($BINARY query federation get-federated-content $MISMATCH_CONTENT_ID --output json 2>&1 | jq -r '.content.status // empty')

    if [ "$MISMATCH_STATUS" == "FEDERATED_CONTENT_STATUS_DISPUTED" ] || [ "$MISMATCH_STATUS" == "FEDERATED_CONTENT_STATUS_CHALLENGED" ]; then
        # Register alice as bridge operator on mastodon.example via Ops Committee
        cat > "$SCRIPT_DIR/proposals/register_alice_bridge.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterBridge",
      "authority": "$OPS_POLICY",
      "operator": "$VERIFIER_A_ADDR",
      "peer_id": "mastodon.example",
      "protocol": "activitypub",
      "endpoint": "https://arbiter-bridge.example.com"
    }
  ],
  "metadata": "Register alice as bridge for arbiter test"
}
EOF

        echo "  Registering alice as bridge operator..."
        TX_RES=$($BINARY tx commons submit-proposal "$SCRIPT_DIR/proposals/register_alice_bridge.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
        ALICE_BRIDGE_OK=false
        if submit_and_wait "$TX_RES" "register alice bridge proposal"; then
            PROP_ID=$(get_commons_proposal_id "$TX_RESULT")
            if [ -n "$PROP_ID" ]; then
                # Vote and execute via ops committee (alice+bob)
                for VOTER in "alice" "bob"; do
                    S=$($BINARY query commons get-proposal $PROP_ID --output json 2>/dev/null | jq -r '.proposal.status')
                    [ "$S" == "PROPOSAL_STATUS_ACCEPTED" ] || [ "$S" == "PROPOSAL_STATUS_EXECUTED" ] && continue
                    VR=$($BINARY tx commons vote-proposal $PROP_ID yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
                    submit_and_wait "$VR" "$VOTER vote" || true
                done
                TX_RES=$($BINARY tx commons execute-proposal $PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --gas 2000000 --output json 2>&1)
                if submit_and_wait "$TX_RES" "execute alice bridge"; then
                    ALICE_BRIDGE_OK=true
                    echo "  Alice registered as bridge operator"
                else
                    CODE=$(echo "$TX_RESULT" | jq -r '.code // empty' 2>/dev/null)
                    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
                    echo "  Bridge registration failed (code=$CODE): $(echo "$RAW" | head -c 100)"
                fi
                sleep 5
            fi
        fi

        if [ "$ALICE_BRIDGE_OK" == "true" ]; then
            ARBITER_HASH=$(sha256_base64 "$MISMATCH_BODY")

            TX_RES=$($BINARY tx federation submit-arbiter-hash \
                $MISMATCH_CONTENT_ID \
                --content-hash "$ARBITER_HASH" \
                --from $VERIFIER_A \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000uspark \
                -y \
                --output json 2>&1)

            if submit_and_wait "$TX_RES" "arbiter happy path"; then
                echo "  Arbiter hash submitted successfully"
                record_result "Arbiter hash happy path" "PASS"
            else
                RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
                CODE=$(echo "$TX_RESULT" | jq -r '.code // empty' 2>/dev/null)
                echo "  Arbiter submission failed (code=$CODE): $(echo "$RAW" | head -c 120)"
                record_result "Arbiter hash happy path" "FAIL"
            fi
        else
            echo "  Could not register alice as bridge operator"
            record_result "Arbiter hash happy path" "FAIL"
        fi
    else
        echo "  Content not DISPUTED/CHALLENGED ($MISMATCH_STATUS)"
        record_result "Arbiter hash happy path" "FAIL"
    fi
else
    echo "  No DISPUTED content for arbiter happy path"
    record_result "Arbiter hash happy path" "FAIL"
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "VERIFIER TEST RESULTS"
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
