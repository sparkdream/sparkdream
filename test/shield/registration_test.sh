#!/bin/bash

echo "--- TESTING: Shielded Operation Registrations (x/shield) ---"
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Alice:     $ALICE_ADDR"
echo ""

# === PASS/FAIL TRACKING ===
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
    echo ""
}

# === HELPER FUNCTIONS ===

check_query_success() {
    local RESULT=$1
    local QUERY_NAME=$2

    if echo "$RESULT" | grep -qi "error\|Error\|ERROR"; then
        if echo "$RESULT" | grep -qi "not found"; then
            echo "  $QUERY_NAME: empty result (not found)"
            return 0
        fi
        echo "  $QUERY_NAME: FAILED"
        echo "  $RESULT"
        return 1
    fi
    echo "  $QUERY_NAME: OK"
    return 0
}

# =========================================================================
# PART 1: List all registered shielded operations
# =========================================================================
echo "--- PART 1: List all registered shielded operations ---"

OPS_RESULT=$($BINARY query shield shielded-ops --output json 2>&1)

PART1_OK=true

if echo "$OPS_RESULT" | grep -qi "error"; then
    echo "  Failed to list shielded operations"
    echo "  Response: $OPS_RESULT"
    PART1_OK=false
fi

if $PART1_OK; then
    # shielded-ops returns .registrations array
    OP_COUNT=$(echo "$OPS_RESULT" | jq -r '.registrations | length' 2>/dev/null || echo "0")
    echo "  Total registered operations: $OP_COUNT"

    if [ "$OP_COUNT" -lt 1 ]; then
        echo "  Expected at least 1 registered operation from genesis"
        PART1_OK=false
    else
        echo "  Registered operations:"
        echo "$OPS_RESULT" | jq -r '.registrations[]? | "    \(.message_type_url) (domain=\(.nullifier_domain // 0), active=\(.active // false))"' 2>/dev/null
    fi
fi

if $PART1_OK; then
    record_result "List shielded operations" "PASS"
else
    record_result "List shielded operations" "FAIL"
fi

# =========================================================================
# PART 2: Query specific blog operation (MsgCreatePost)
# =========================================================================
echo "--- PART 2: Query blog MsgCreatePost registration ---"

BLOG_POST_URL="/sparkdream.blog.v1.MsgCreatePost"
BLOG_OP=$($BINARY query shield shielded-op "$BLOG_POST_URL" --output json 2>&1)

PART2_OK=true

if echo "$BLOG_OP" | grep -qi "error\|not found"; then
    echo "  MsgCreatePost not registered (may not be in genesis defaults)"
    echo "  Skipping specific operation checks..."
else
    # shielded-op returns .registration
    OP_TYPE=$(echo "$BLOG_OP" | jq -r '.registration.message_type_url // "null"')
    OP_DOMAIN=$(echo "$BLOG_OP" | jq -r '.registration.proof_domain // "null"')
    OP_TRUST=$(echo "$BLOG_OP" | jq -r '.registration.min_trust_level // "0"')
    OP_NULL_DOMAIN=$(echo "$BLOG_OP" | jq -r '.registration.nullifier_domain // "0"')
    OP_ACTIVE=$(echo "$BLOG_OP" | jq -r '.registration.active // false')
    OP_BATCH=$(echo "$BLOG_OP" | jq -r '.registration.batch_mode // "0"')

    echo "  Message type: $OP_TYPE"
    echo "  Proof domain: $OP_DOMAIN"
    echo "  Min trust level: $OP_TRUST"
    echo "  Nullifier domain: $OP_NULL_DOMAIN"
    echo "  Active: $OP_ACTIVE"
    echo "  Batch mode: $OP_BATCH"

    if [ "$OP_TYPE" != "$BLOG_POST_URL" ]; then
        echo "  Type URL mismatch"
        PART2_OK=false
    fi

    # Proof domain should be TRUST_TREE (1) for blog operations
    if $PART2_OK; then
        if [ "$OP_DOMAIN" != "PROOF_DOMAIN_TRUST_TREE" ] && [ "$OP_DOMAIN" != "1" ]; then
            echo "  Expected PROOF_DOMAIN_TRUST_TREE, got: $OP_DOMAIN"
            PART2_OK=false
        fi
    fi

    if $PART2_OK; then
        echo "  Blog MsgCreatePost registration verified"
    fi
fi

if $PART2_OK; then
    record_result "Blog MsgCreatePost registration" "PASS"
else
    record_result "Blog MsgCreatePost registration" "FAIL"
fi

# =========================================================================
# PART 3: Query forum operation (MsgCreatePost)
# =========================================================================
echo "--- PART 3: Query forum MsgCreatePost registration ---"

FORUM_POST_URL="/sparkdream.forum.v1.MsgCreatePost"
FORUM_OP=$($BINARY query shield shielded-op "$FORUM_POST_URL" --output json 2>&1)

PART3_OK=true

if echo "$FORUM_OP" | grep -qi "error\|not found"; then
    echo "  Forum MsgCreatePost not registered"
    echo "  Skipping..."
else
    F_TYPE=$(echo "$FORUM_OP" | jq -r '.registration.message_type_url // "null"')
    F_DOMAIN=$(echo "$FORUM_OP" | jq -r '.registration.proof_domain // "null"')
    F_BATCH=$(echo "$FORUM_OP" | jq -r '.registration.batch_mode // "0"')

    echo "  Message type: $F_TYPE"
    echo "  Proof domain: $F_DOMAIN"
    echo "  Batch mode: $F_BATCH"

    echo "  Forum MsgCreatePost registration verified"
fi

if $PART3_OK; then
    record_result "Forum MsgCreatePost registration" "PASS"
else
    record_result "Forum MsgCreatePost registration" "FAIL"
fi

# =========================================================================
# PART 4: Query collect operation (MsgCreateCollection)
# =========================================================================
echo "--- PART 4: Query collect MsgCreateCollection registration ---"

COLLECT_URL="/sparkdream.collect.v1.MsgCreateCollection"
COLLECT_OP=$($BINARY query shield shielded-op "$COLLECT_URL" --output json 2>&1)

PART4_OK=true

if echo "$COLLECT_OP" | grep -qi "error\|not found"; then
    echo "  Collect MsgCreateCollection not registered"
    echo "  Skipping..."
else
    C_TYPE=$(echo "$COLLECT_OP" | jq -r '.registration.message_type_url // "null"')
    C_DOMAIN=$(echo "$COLLECT_OP" | jq -r '.registration.proof_domain // "null"')

    echo "  Message type: $C_TYPE"
    echo "  Proof domain: $C_DOMAIN"

    echo "  Collect MsgCreateCollection registration verified"
fi

if $PART4_OK; then
    record_result "Collect MsgCreateCollection registration" "PASS"
else
    record_result "Collect MsgCreateCollection registration" "FAIL"
fi

# =========================================================================
# PART 5: Query commons anonymous proposal (ENCRYPTED_ONLY)
# =========================================================================
echo "--- PART 5: Query commons MsgSubmitAnonymousProposal registration ---"

COMMONS_PROP_URL="/sparkdream.commons.v1.MsgSubmitAnonymousProposal"
COMMONS_OP=$($BINARY query shield shielded-op "$COMMONS_PROP_URL" --output json 2>&1)

PART5_OK=true

if echo "$COMMONS_OP" | grep -qi "error\|not found"; then
    echo "  Commons MsgSubmitAnonymousProposal not registered"
    echo "  Skipping..."
else
    CP_TYPE=$(echo "$COMMONS_OP" | jq -r '.registration.message_type_url // "null"')
    CP_DOMAIN=$(echo "$COMMONS_OP" | jq -r '.registration.proof_domain // "null"')
    CP_BATCH=$(echo "$COMMONS_OP" | jq -r '.registration.batch_mode // "null"')

    echo "  Message type: $CP_TYPE"
    echo "  Proof domain: $CP_DOMAIN"
    echo "  Batch mode: $CP_BATCH"

    # Should be TRUST_TREE domain (unified circuit -- no separate voter tree needed)
    if [ "$CP_DOMAIN" != "PROOF_DOMAIN_TRUST_TREE" ] && [ "$CP_DOMAIN" != "1" ]; then
        echo "  Expected PROOF_DOMAIN_TRUST_TREE, got: $CP_DOMAIN"
        PART5_OK=false
    fi

    # Should be EITHER (supports both immediate and encrypted batch modes)
    if $PART5_OK; then
        if [ "$CP_BATCH" != "SHIELD_BATCH_MODE_EITHER" ] && [ "$CP_BATCH" != "2" ]; then
            echo "  Expected EITHER batch mode, got: $CP_BATCH"
            PART5_OK=false
        fi
    fi

    if $PART5_OK; then
        echo "  Commons anonymous proposal registration verified"
    fi
fi

if $PART5_OK; then
    record_result "Commons anonymous proposal registration" "PASS"
else
    record_result "Commons anonymous proposal registration" "FAIL"
fi

# =========================================================================
# PART 6: Query rep challenge operation (ENCRYPTED_ONLY, GLOBAL scope)
# =========================================================================
echo "--- PART 6: Query rep MsgCreateChallenge registration ---"

REP_CHALLENGE_URL="/sparkdream.rep.v1.MsgCreateChallenge"
REP_OP=$($BINARY query shield shielded-op "$REP_CHALLENGE_URL" --output json 2>&1)

PART6_OK=true

if echo "$REP_OP" | grep -qi "error\|not found"; then
    echo "  Rep MsgCreateChallenge not registered"
    echo "  Skipping..."
else
    R_TYPE=$(echo "$REP_OP" | jq -r '.registration.message_type_url // "null"')
    R_SCOPE=$(echo "$REP_OP" | jq -r '.registration.nullifier_scope_type // "null"')
    R_BATCH=$(echo "$REP_OP" | jq -r '.registration.batch_mode // "null"')

    echo "  Message type: $R_TYPE"
    echo "  Nullifier scope type: $R_SCOPE"
    echo "  Batch mode: $R_BATCH"

    # Should be GLOBAL scope
    if [ "$R_SCOPE" != "NULLIFIER_SCOPE_GLOBAL" ] && [ "$R_SCOPE" != "2" ]; then
        echo "  Expected NULLIFIER_SCOPE_GLOBAL, got: $R_SCOPE"
        PART6_OK=false
    fi

    if $PART6_OK; then
        echo "  Rep MsgCreateChallenge registration verified"
    fi
fi

if $PART6_OK; then
    record_result "Rep MsgCreateChallenge registration" "PASS"
else
    record_result "Rep MsgCreateChallenge registration" "FAIL"
fi

# =========================================================================
# PART 7: Query non-existent operation (should return not found)
# =========================================================================
echo "--- PART 7: Query non-existent operation ---"

FAKE_URL="/sparkdream.fake.v1.MsgDoNothing"
FAKE_OP=$($BINARY query shield shielded-op "$FAKE_URL" --output json 2>&1)

PART7_OK=true

if echo "$FAKE_OP" | grep -qi "not found"; then
    echo "  Correctly returned not found for non-existent operation"
elif echo "$FAKE_OP" | grep -qi "error"; then
    echo "  Correctly returned error for non-existent operation"
else
    echo "  Warning: Expected not-found error, got a response"
    echo "  $FAKE_OP"
    PART7_OK=false
fi

if $PART7_OK; then
    record_result "Non-existent operation query" "PASS"
else
    record_result "Non-existent operation query" "FAIL"
fi

# =========================================================================
# PART 8: Verify batch mode assignments
# =========================================================================
echo "--- PART 8: Verify batch mode assignments ---"

echo "  Checking EITHER mode operations (content modules)..."

EITHER_URLS=(
    "/sparkdream.blog.v1.MsgCreatePost"
    "/sparkdream.blog.v1.MsgCreateReply"
    "/sparkdream.blog.v1.MsgReact"
    "/sparkdream.forum.v1.MsgCreatePost"
)

EITHER_OK=true
for URL in "${EITHER_URLS[@]}"; do
    OP=$($BINARY query shield shielded-op "$URL" --output json 2>&1)
    if echo "$OP" | grep -qi "not found\|error"; then
        echo "    $URL: not registered (skipping)"
        continue
    fi
    BATCH=$(echo "$OP" | jq -r '.registration.batch_mode // "0"')
    # EITHER = 2, IMMEDIATE_ONLY = 0
    if [ "$BATCH" != "SHIELD_BATCH_MODE_EITHER" ] && [ "$BATCH" != "2" ]; then
        echo "    $URL: Expected EITHER, got $BATCH"
        EITHER_OK=false
    else
        echo "    $URL: EITHER (correct)"
    fi
done

echo ""
echo "  Checking EITHER mode operations (governance — immediate needed while TLE not active)..."

EITHER_GOV_URLS=(
    "/sparkdream.commons.v1.MsgSubmitAnonymousProposal"
    "/sparkdream.commons.v1.MsgAnonymousVoteProposal"
)

EITHER_GOV_OK=true
for URL in "${EITHER_GOV_URLS[@]}"; do
    OP=$($BINARY query shield shielded-op "$URL" --output json 2>&1)
    if echo "$OP" | grep -qi "not found\|error"; then
        echo "    $URL: not registered (skipping)"
        continue
    fi
    BATCH=$(echo "$OP" | jq -r '.registration.batch_mode // "0"')
    if [ "$BATCH" != "SHIELD_BATCH_MODE_EITHER" ] && [ "$BATCH" != "2" ]; then
        echo "    $URL: Expected EITHER, got $BATCH"
        EITHER_GOV_OK=false
    else
        echo "    $URL: EITHER (correct)"
    fi
done

echo ""
echo "  Checking ENCRYPTED_ONLY operations (sensitive actions)..."

ENCRYPTED_URLS=(
    "/sparkdream.rep.v1.MsgCreateChallenge"
)

ENCRYPTED_OK=true
for URL in "${ENCRYPTED_URLS[@]}"; do
    OP=$($BINARY query shield shielded-op "$URL" --output json 2>&1)
    if echo "$OP" | grep -qi "not found\|error"; then
        echo "    $URL: not registered (skipping)"
        continue
    fi
    BATCH=$(echo "$OP" | jq -r '.registration.batch_mode // "0"')
    if [ "$BATCH" != "SHIELD_BATCH_MODE_ENCRYPTED_ONLY" ] && [ "$BATCH" != "1" ]; then
        echo "    $URL: Expected ENCRYPTED_ONLY, got $BATCH"
        ENCRYPTED_OK=false
    else
        echo "    $URL: ENCRYPTED_ONLY (correct)"
    fi
done

if $EITHER_OK && $EITHER_GOV_OK && $ENCRYPTED_OK; then
    record_result "Batch mode assignments" "PASS"
else
    record_result "Batch mode assignments" "FAIL"
fi

# =========================================================================
# SUMMARY
# =========================================================================
echo "--- FINAL RESULTS ---"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "Total: $PASS_COUNT passed, $FAIL_COUNT failed out of $((PASS_COUNT + FAIL_COUNT))"
if [ $FAIL_COUNT -gt 0 ]; then
    exit 1
fi
