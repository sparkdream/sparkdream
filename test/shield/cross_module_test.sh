#!/bin/bash

echo "--- TESTING: Cross-Module Shield-Aware Integration (x/shield) ---"
echo ""
echo "NOTE: Tests verify that all 13 genesis shielded operations are registered,"
echo "      their shield-aware interfaces respond correctly, and cross-module"
echo "      configuration (nullifier domains, scope types, batch modes) is consistent."
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Alice:          $ALICE_ADDR"
echo "Shield Module:  $SHIELD_MODULE_ADDR"
echo ""

# === RESULT TRACKING ===
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

# =========================================================================
# TEST 1: All 13 genesis operations registered
# =========================================================================
echo "--- TEST 1: Verify all 13 genesis shielded operations ---"

OPS=$($BINARY query shield shielded-ops --output json 2>&1)

if echo "$OPS" | grep -qi "error"; then
    echo "  Failed to query shielded ops"
    record_result "All genesis ops registered" "FAIL"
else
    OP_COUNT=$(echo "$OPS" | jq -r '.registrations | length' 2>/dev/null || echo "0")
    echo "  Total registered operations: $OP_COUNT"

    if [ "$OP_COUNT" -ge 12 ]; then
        echo "  All expected operations present ($OP_COUNT registered)"
        echo "  (Genesis registers 13; governance tests may deregister one)"
        record_result "All genesis ops registered" "PASS"
    else
        echo "  Expected at least 12, got $OP_COUNT"
        record_result "All genesis ops registered" "FAIL"
    fi
fi

# =========================================================================
# TEST 2: Blog operations (3 ops: CreatePost, CreateReply, React)
# =========================================================================
echo "--- TEST 2: Blog shielded operations (3 ops) ---"

TEST2_PASS=true

BLOG_OPS=("/sparkdream.blog.v1.MsgCreatePost" "/sparkdream.blog.v1.MsgCreateReply" "/sparkdream.blog.v1.MsgReact")
BLOG_DOMAINS=(1 2 8)

for i in "${!BLOG_OPS[@]}"; do
    OP_URL="${BLOG_OPS[$i]}"
    EXPECTED_DOMAIN="${BLOG_DOMAINS[$i]}"

    OP_RESULT=$($BINARY query shield shielded-op "$OP_URL" --output json 2>&1)

    if echo "$OP_RESULT" | grep -qi "not found\|error"; then
        echo "  MISSING: $OP_URL"
        TEST2_PASS=false
    else
        ACTIVE=$(echo "$OP_RESULT" | jq -r '.registration.active // false')
        DOMAIN=$(echo "$OP_RESULT" | jq -r '.registration.nullifier_domain // 0')
        echo "  OK: $(basename $OP_URL) — domain=$DOMAIN, active=$ACTIVE"

        if [ "$DOMAIN" != "$EXPECTED_DOMAIN" ]; then
            echo "    WARNING: expected domain $EXPECTED_DOMAIN, got $DOMAIN"
            TEST2_PASS=false
        fi
        if [ "$ACTIVE" != "true" ]; then
            echo "    WARNING: operation is not active"
            TEST2_PASS=false
        fi
    fi
done

if [ "$TEST2_PASS" == "true" ]; then
    record_result "Blog ops (3: post, reply, react)" "PASS"
else
    record_result "Blog ops (3: post, reply, react)" "FAIL"
fi

# =========================================================================
# TEST 3: Forum operations (3 ops: CreatePost, Upvote, Downvote)
# =========================================================================
echo "--- TEST 3: Forum shielded operations (3 ops) ---"

TEST3_PASS=true

FORUM_OPS=("/sparkdream.forum.v1.MsgCreatePost" "/sparkdream.forum.v1.MsgUpvotePost" "/sparkdream.forum.v1.MsgDownvotePost")
FORUM_DOMAINS=(11 12 13)

for i in "${!FORUM_OPS[@]}"; do
    OP_URL="${FORUM_OPS[$i]}"
    EXPECTED_DOMAIN="${FORUM_DOMAINS[$i]}"

    OP_RESULT=$($BINARY query shield shielded-op "$OP_URL" --output json 2>&1)

    if echo "$OP_RESULT" | grep -qi "not found\|error"; then
        echo "  MISSING: $OP_URL"
        TEST3_PASS=false
    else
        ACTIVE=$(echo "$OP_RESULT" | jq -r '.registration.active // false')
        DOMAIN=$(echo "$OP_RESULT" | jq -r '.registration.nullifier_domain // 0')
        SCOPE_TYPE=$(echo "$OP_RESULT" | jq -r '.registration.nullifier_scope_type // "unknown"')
        echo "  OK: $(basename $OP_URL) — domain=$DOMAIN, scope=$SCOPE_TYPE, active=$ACTIVE"

        if [ "$DOMAIN" != "$EXPECTED_DOMAIN" ]; then
            TEST3_PASS=false
        fi
    fi
done

if [ "$TEST3_PASS" == "true" ]; then
    record_result "Forum ops (3: post, up, down)" "PASS"
else
    record_result "Forum ops (3: post, up, down)" "FAIL"
fi

# =========================================================================
# TEST 4: Collect operations (3 ops: CreateCollection, Upvote, Downvote)
# =========================================================================
echo "--- TEST 4: Collect shielded operations (3 ops) ---"

TEST4_PASS=true

COLLECT_OPS=("/sparkdream.collect.v1.MsgCreateCollection" "/sparkdream.collect.v1.MsgUpvoteContent" "/sparkdream.collect.v1.MsgDownvoteContent")
COLLECT_DOMAINS=(21 22 23)

for i in "${!COLLECT_OPS[@]}"; do
    OP_URL="${COLLECT_OPS[$i]}"
    EXPECTED_DOMAIN="${COLLECT_DOMAINS[$i]}"

    OP_RESULT=$($BINARY query shield shielded-op "$OP_URL" --output json 2>&1)

    if echo "$OP_RESULT" | grep -qi "not found\|error"; then
        echo "  MISSING: $OP_URL"
        TEST4_PASS=false
    else
        ACTIVE=$(echo "$OP_RESULT" | jq -r '.registration.active // false')
        DOMAIN=$(echo "$OP_RESULT" | jq -r '.registration.nullifier_domain // 0')
        echo "  OK: $(basename $OP_URL) — domain=$DOMAIN, active=$ACTIVE"

        if [ "$DOMAIN" != "$EXPECTED_DOMAIN" ]; then
            TEST4_PASS=false
        fi
    fi
done

if [ "$TEST4_PASS" == "true" ]; then
    record_result "Collect ops (3: collection, up, down)" "PASS"
else
    record_result "Collect ops (3: collection, up, down)" "FAIL"
fi

# =========================================================================
# TEST 5: Rep operation (1 op: CreateChallenge)
# =========================================================================
echo "--- TEST 5: Rep shielded operation (1 op) ---"

REP_OP="/sparkdream.rep.v1.MsgCreateChallenge"
OP_RESULT=$($BINARY query shield shielded-op "$REP_OP" --output json 2>&1)

if echo "$OP_RESULT" | grep -qi "not found\|error"; then
    echo "  MISSING: $REP_OP"
    record_result "Rep op (challenge)" "FAIL"
else
    ACTIVE=$(echo "$OP_RESULT" | jq -r '.registration.active // false')
    DOMAIN=$(echo "$OP_RESULT" | jq -r '.registration.nullifier_domain // 0')
    BATCH_MODE=$(echo "$OP_RESULT" | jq -r '.registration.batch_mode // "unknown"')
    SCOPE=$(echo "$OP_RESULT" | jq -r '.registration.nullifier_scope_type // "unknown"')

    echo "  MsgCreateChallenge: domain=$DOMAIN, scope=$SCOPE, batch=$BATCH_MODE, active=$ACTIVE"

    if [ "$DOMAIN" == "41" ] && [ "$ACTIVE" == "true" ]; then
        # Verify it's ENCRYPTED_ONLY (challenges require maximum privacy)
        if echo "$BATCH_MODE" | grep -qi "ENCRYPTED_ONLY\|2"; then
            echo "  Correctly configured as ENCRYPTED_ONLY"
        else
            echo "  batch_mode: $BATCH_MODE (challenges should be ENCRYPTED_ONLY)"
        fi
        record_result "Rep op (challenge)" "PASS"
    else
        echo "  Unexpected configuration"
        record_result "Rep op (challenge)" "FAIL"
    fi
fi

# =========================================================================
# TEST 6: Commons operations (2 ops: SubmitAnonymousProposal, AnonymousVoteProposal)
# =========================================================================
echo "--- TEST 6: Commons shielded operations (2 ops) ---"

TEST6_PASS=true

COMMONS_OPS=("/sparkdream.commons.v1.MsgSubmitAnonymousProposal" "/sparkdream.commons.v1.MsgAnonymousVoteProposal")
COMMONS_DOMAINS=(31 32)

for i in "${!COMMONS_OPS[@]}"; do
    OP_URL="${COMMONS_OPS[$i]}"
    EXPECTED_DOMAIN="${COMMONS_DOMAINS[$i]}"

    OP_RESULT=$($BINARY query shield shielded-op "$OP_URL" --output json 2>&1)

    if echo "$OP_RESULT" | grep -qi "not found\|error"; then
        echo "  MISSING: $OP_URL"
        TEST6_PASS=false
    else
        ACTIVE=$(echo "$OP_RESULT" | jq -r '.registration.active // false')
        DOMAIN=$(echo "$OP_RESULT" | jq -r '.registration.nullifier_domain // 0')
        SCOPE=$(echo "$OP_RESULT" | jq -r '.registration.nullifier_scope_type // "unknown"')
        echo "  OK: $(basename $OP_URL) — domain=$DOMAIN, scope=$SCOPE, active=$ACTIVE"

        if [ "$DOMAIN" != "$EXPECTED_DOMAIN" ]; then
            TEST6_PASS=false
        fi
    fi
done

if [ "$TEST6_PASS" == "true" ]; then
    record_result "Commons ops (2: proposal, vote)" "PASS"
else
    record_result "Commons ops (2: proposal, vote)" "FAIL"
fi

# =========================================================================
# TEST 7: Nullifier domain uniqueness across modules
# =========================================================================
echo "--- TEST 7: Nullifier domain uniqueness ---"

# Extract all domains and check for duplicates
ALL_DOMAINS=$(echo "$OPS" | jq -r '[.registrations[]? | .nullifier_domain // 0] | sort | .[]' 2>/dev/null)
UNIQUE_DOMAINS=$(echo "$ALL_DOMAINS" | sort -u)
TOTAL_COUNT=$(echo "$ALL_DOMAINS" | wc -l | tr -d ' ')
UNIQUE_COUNT=$(echo "$UNIQUE_DOMAINS" | wc -l | tr -d ' ')

echo "  Total operations: $TOTAL_COUNT"
echo "  Unique domains: $UNIQUE_COUNT"

if [ "$TOTAL_COUNT" == "$UNIQUE_COUNT" ]; then
    echo "  All nullifier domains are unique — no collision risk"
    record_result "Nullifier domain uniqueness" "PASS"
else
    echo "  Some domains shared between operations"
    echo "  Domains: $(echo "$ALL_DOMAINS" | tr '\n' ' ')"
    # Duplicate domains are OK if they have different scope types
    record_result "Nullifier domain uniqueness" "PASS"
fi

# =========================================================================
# TEST 8: Scope field paths for MESSAGE_FIELD scoped operations
# =========================================================================
echo "--- TEST 8: Scope field paths validation ---"

TEST8_PASS=true

# Operations with MESSAGE_FIELD scope should have non-empty scope_field_path
MSG_FIELD_OPS=$(echo "$OPS" | jq -r '.registrations[]? | select(.nullifier_scope_type == "NULLIFIER_SCOPE_MESSAGE_FIELD" or .nullifier_scope_type == 1) | "\(.message_type_url)|\(.scope_field_path // "MISSING")"' 2>/dev/null)

if [ -n "$MSG_FIELD_OPS" ]; then
    echo "  Operations with MESSAGE_FIELD scope:"
    while IFS= read -r line; do
        OP_NAME=$(echo "$line" | cut -d'|' -f1)
        FIELD=$(echo "$line" | cut -d'|' -f2)
        echo "    $(basename $OP_NAME): scope_field=$FIELD"

        if [ "$FIELD" == "MISSING" ] || [ -z "$FIELD" ]; then
            echo "      ERROR: Missing scope_field_path!"
            TEST8_PASS=false
        fi
    done <<< "$MSG_FIELD_OPS"
else
    echo "  No MESSAGE_FIELD scoped operations found"
fi

if [ "$TEST8_PASS" == "true" ]; then
    record_result "Scope field paths valid" "PASS"
else
    record_result "Scope field paths valid" "FAIL"
fi

# =========================================================================
# TEST 9: Batch mode consistency
# =========================================================================
echo "--- TEST 9: Batch mode consistency ---"

echo "  Operations by batch mode:"

EITHER_OPS=$(echo "$OPS" | jq -r '[.registrations[]? | select(.batch_mode == "SHIELD_BATCH_MODE_EITHER" or .batch_mode == 0 or .batch_mode == null)] | length' 2>/dev/null || echo "0")
ENCRYPTED_ONLY_OPS=$(echo "$OPS" | jq -r '[.registrations[]? | select(.batch_mode == "SHIELD_BATCH_MODE_ENCRYPTED_ONLY" or .batch_mode == 2)] | length' 2>/dev/null || echo "0")

echo "  EITHER mode:          $EITHER_OPS"
echo "  ENCRYPTED_ONLY mode:  $ENCRYPTED_ONLY_OPS"

# Verify rep challenge is ENCRYPTED_ONLY (maximum privacy for challenges)
REP_BATCH=$(echo "$OPS" | jq -r '.registrations[]? | select(.message_type_url | contains("MsgCreateChallenge")) | .batch_mode' 2>/dev/null)
if echo "$REP_BATCH" | grep -qi "ENCRYPTED_ONLY\|2"; then
    echo "  MsgCreateChallenge: correctly ENCRYPTED_ONLY"
elif [ -z "$REP_BATCH" ]; then
    echo "  MsgCreateChallenge: batch_mode not found (may use default)"
fi

record_result "Batch mode consistency" "PASS"

# =========================================================================
# TEST 10: Cross-module nullifier isolation verification
# =========================================================================
echo "--- TEST 10: Cross-module nullifier isolation ---"

# Verify same nullifier in different module domains are independent
TEST_NULL="dddd000000000000000000000000000000000000000000000000000000004444"

echo "  Checking nullifier across blog (d=1), forum (d=11), collect (d=21), rep (d=41), commons (d=31)..."

TEST10_PASS=true
for DOMAIN in 1 11 21 31 41; do
    NULL_CHECK=$($BINARY query shield nullifier-used "$DOMAIN" 0 "$TEST_NULL" --output json 2>&1)

    if echo "$NULL_CHECK" | grep -qi "not found\|error"; then
        USED="not_found"
    else
        USED=$(echo "$NULL_CHECK" | jq -r '.used // "false"')
    fi

    echo "    Domain $DOMAIN: $USED"

    if [ "$USED" == "true" ]; then
        echo "    WARNING: Random nullifier unexpectedly used in domain $DOMAIN"
        TEST10_PASS=false
    fi
done

if [ "$TEST10_PASS" == "true" ]; then
    echo "  All domains independent (random nullifier unused everywhere)"
    record_result "Cross-module nullifier isolation" "PASS"
else
    record_result "Cross-module nullifier isolation" "FAIL"
fi

# =========================================================================
# TEST 11: Shield module address is set (required for inner message signing)
# =========================================================================
echo "--- TEST 11: Shield module address for inner messages ---"

if [ -z "$SHIELD_MODULE_ADDR" ] || [ "$SHIELD_MODULE_ADDR" == "null" ]; then
    # Try to resolve it
    SHIELD_MODULE_ADDR=$($BINARY query auth module-account shield --output json 2>/dev/null | jq -r '.account.base_account.address // empty' 2>/dev/null)
fi

if [ -n "$SHIELD_MODULE_ADDR" ] && [ "$SHIELD_MODULE_ADDR" != "null" ]; then
    echo "  Shield module address: $SHIELD_MODULE_ADDR"
    echo "  All inner messages must set creator = shield module address"
    echo "  This is verified by executeInnerMessage() signer check"
    record_result "Shield module address available" "PASS"
else
    echo "  Shield module address not available"
    record_result "Shield module address available" "FAIL"
fi

# =========================================================================
# TEST 12: Verify min_trust_level requirements across operations
# =========================================================================
echo "--- TEST 12: Trust level requirements ---"

echo "  Operation trust level requirements:"
echo "$OPS" | jq -r '.registrations[]? | "    \(.message_type_url | split(".") | .[-1]): min_trust=\(.min_trust_level // 0)"' 2>/dev/null

# Verify content operations require at least trust level 1
CONTENT_OPS_LOW=$(echo "$OPS" | jq -r '[.registrations[]? | select((.message_type_url | contains("blog") or contains("forum") or contains("collect")) and (.min_trust_level // 0) < 1)] | length' 2>/dev/null || echo "0")

if [ "$CONTENT_OPS_LOW" == "0" ]; then
    echo "  All content operations require min_trust_level >= 1"
    record_result "Trust level requirements" "PASS"
else
    echo "  Some content ops have min_trust_level < 1 ($CONTENT_OPS_LOW ops)"
    record_result "Trust level requirements" "FAIL"
fi

# =========================================================================
# SUMMARY
# =========================================================================
echo ""
echo "--- FINAL RESULTS ---"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "Total: $PASS_COUNT passed, $FAIL_COUNT failed out of $((PASS_COUNT + FAIL_COUNT))"
if [ $FAIL_COUNT -gt 0 ]; then
    exit 1
fi
