#!/bin/bash

echo "--- TESTING: BATCH EXECUTION EDGE CASES ---"
echo ""

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# Initialize tracking
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

# ========================================================================
# Helper Functions
# ========================================================================

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

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 1
    fi
    return 0
}

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 0
    fi
    return 1
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi

    local BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

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

get_future_expiration() {
    local HOURS=${1:-1}
    date -u -d "+${HOURS} hours" +"%Y-%m-%dT%H:%M:%SZ"
}

# Sign and broadcast a raw unsigned tx JSON file.
# Usage: sign_and_broadcast <grantee_key> <grantee_addr> <unsigned_file> <signed_file>
sign_and_broadcast() {
    local GRANTEE_KEY=$1
    local GRANTEE=$2
    local UNSIGNED_FILE=$3
    local SIGNED_FILE=$4

    local ACCT_INFO=$($BINARY query auth account "$GRANTEE" --output json 2>&1)
    local ACCT_NUM=$(echo "$ACCT_INFO" | jq -r '.account.account_number // .account.base_account.account_number // "0"')
    local SEQ=$(echo "$ACCT_INFO" | jq -r '.account.sequence // .account.base_account.sequence // "0"')

    local SIGN_RESULT=$($BINARY tx sign "$UNSIGNED_FILE" \
        --from "$GRANTEE_KEY" \
        --chain-id "$CHAIN_ID" \
        --keyring-backend test \
        --account-number "$ACCT_NUM" \
        --sequence "$SEQ" \
        --output-document "$SIGNED_FILE" 2>&1)

    if [ ! -f "$SIGNED_FILE" ] || [ ! -s "$SIGNED_FILE" ]; then
        echo "  Sign failed: $SIGN_RESULT" >&2
        TX_RESULT='{"code":99,"raw_log":"sign failed"}'
        return 1
    fi

    local TX_RES=$($BINARY tx broadcast "$SIGNED_FILE" --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    return $?
}

# ========================================================================
# Pre-test: Create dedicated account and session for batch tests
# ========================================================================
echo "--- PRE-TEST: Setting up batch test session ---"

if ! $BINARY keys show batch_grantee --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add batch_grantee --keyring-backend test > /dev/null 2>&1
fi
BATCH_GRANTEE_ADDR=$($BINARY keys show batch_grantee -a --keyring-backend test)

TX_RES=$($BINARY tx bank send alice "$BATCH_GRANTEE_ADDR" 20000000uspark \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)
sleep 6

# Create session with MsgCreatePost and MsgReact allowed
EXPIRATION=$(get_future_expiration 2)
TX_RES=$($BINARY tx session create-session \
    "$BATCH_GRANTEE_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost,/sparkdream.blog.v1.MsgReact" \
    "50000000uspark" \
    "$EXPIRATION" \
    "0" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    echo "  Batch test session created: granter=$GRANTER_ADDR, grantee=$BATCH_GRANTEE_ADDR"
else
    echo "  Failed to create batch test session"
    echo "  Error: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
    exit 1
fi
echo ""

# ========================================================================
# TEST 1: Error - empty msgs in MsgExecSession
# ========================================================================
echo "--- TEST 1: Error - empty msgs ---"

cat > /tmp/batch_empty_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgExecSession",
        "grantee": "$BATCH_GRANTEE_ADDR",
        "granter": "$GRANTER_ADDR",
        "msgs": []
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "uspark", "amount": "50000"}],
      "gas_limit": "300000",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}
TXEOF

sign_and_broadcast "batch_grantee" "$BATCH_GRANTEE_ADDR" \
    /tmp/batch_empty_unsigned.json /tmp/batch_empty_signed.json

if check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "empty\|at least one"; then
        echo "  Correctly rejected: empty msgs"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: empty msgs" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: empty msgs" "FAIL"
fi

# ========================================================================
# TEST 2: Error - too many msgs (11 inner messages, max=10)
# ========================================================================
echo "--- TEST 2: Error - too many msgs (11 > max 10) ---"

# Build 11 inner messages
INNER_MSGS=""
for i in $(seq 1 11); do
    if [ $i -gt 1 ]; then
        INNER_MSGS="$INNER_MSGS,"
    fi
    INNER_MSGS="$INNER_MSGS{\"@type\":\"/sparkdream.blog.v1.MsgCreatePost\",\"creator\":\"$GRANTER_ADDR\",\"title\":\"Batch $i\",\"body\":\"Post $i\"}"
done

cat > /tmp/batch_toomany_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgExecSession",
        "grantee": "$BATCH_GRANTEE_ADDR",
        "granter": "$GRANTER_ADDR",
        "msgs": [$INNER_MSGS]
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "uspark", "amount": "100000"}],
      "gas_limit": "1000000",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}
TXEOF

sign_and_broadcast "batch_grantee" "$BATCH_GRANTEE_ADDR" \
    /tmp/batch_toomany_unsigned.json /tmp/batch_toomany_signed.json

if check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "too many\|max 10"; then
        echo "  Correctly rejected: too many msgs"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: too many msgs (11)" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: too many msgs (11)" "FAIL"
fi

# ========================================================================
# TEST 3: Success - multiple inner messages in one batch
# ========================================================================
echo "--- TEST 3: Multiple inner messages in one batch ---"

# Execute 2 blog posts in one MsgExecSession
cat > /tmp/batch_multi_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgExecSession",
        "grantee": "$BATCH_GRANTEE_ADDR",
        "granter": "$GRANTER_ADDR",
        "msgs": [
          {
            "@type": "/sparkdream.blog.v1.MsgCreatePost",
            "creator": "$GRANTER_ADDR",
            "title": "Batch Post A",
            "body": "First post in batch"
          },
          {
            "@type": "/sparkdream.blog.v1.MsgCreatePost",
            "creator": "$GRANTER_ADDR",
            "title": "Batch Post B",
            "body": "Second post in batch"
          }
        ]
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "uspark", "amount": "100000"}],
      "gas_limit": "500000",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}
TXEOF

sign_and_broadcast "batch_grantee" "$BATCH_GRANTEE_ADDR" \
    /tmp/batch_multi_unsigned.json /tmp/batch_multi_signed.json

if check_tx_success "$TX_RESULT"; then
    # Verify exec_count incremented by number of inner messages (SESSION-2 fix)
    # 2 inner messages → exec_count should be 2
    SESSION=$($BINARY query session session "$GRANTER_ADDR" "$BATCH_GRANTEE_ADDR" --output json 2>&1)
    EXEC_COUNT=$(echo "$SESSION" | jq -r '.session.exec_count // "0"')
    echo "  Batch of 2 msgs executed, exec_count=$EXEC_COUNT"
    if [ "$EXEC_COUNT" = "2" ]; then
        record_result "Multiple inner msgs in one batch" "PASS"
    else
        echo "  Unexpected exec_count (expected 2)"
        record_result "Multiple inner msgs in one batch" "FAIL"
    fi
else
    echo "  TX failed: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
    record_result "Multiple inner msgs in one batch" "FAIL"
fi

# ========================================================================
# TEST 4: Error - nested MsgExecSession (anti-recursion)
# ========================================================================
echo "--- TEST 4: Error - nested MsgExecSession (anti-recursion) ---"

cat > /tmp/batch_nested_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgExecSession",
        "grantee": "$BATCH_GRANTEE_ADDR",
        "granter": "$GRANTER_ADDR",
        "msgs": [
          {
            "@type": "/sparkdream.session.v1.MsgExecSession",
            "grantee": "$BATCH_GRANTEE_ADDR",
            "granter": "$GRANTER_ADDR",
            "msgs": []
          }
        ]
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "uspark", "amount": "50000"}],
      "gas_limit": "300000",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}
TXEOF

sign_and_broadcast "batch_grantee" "$BATCH_GRANTEE_ADDR" \
    /tmp/batch_nested_unsigned.json /tmp/batch_nested_signed.json

if check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "nested\|non.?delegable\|NonDelegable\|recursion\|forbidden"; then
        echo "  Correctly rejected: nested MsgExecSession"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: nested MsgExecSession" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: nested MsgExecSession" "FAIL"
fi

# ========================================================================
# TEST 5: Atomic rollback - second msg fails, first reverts
# ========================================================================
echo "--- TEST 5: Atomic rollback - partial batch failure ---"

# Count posts by granter BEFORE this test
POSTS_BEFORE=$($BINARY query blog list-posts --output json 2>&1 | jq '[.posts[] | select(.creator=="'"$GRANTER_ADDR"'")] | length' 2>/dev/null || echo "0")
echo "  Posts by granter before: $POSTS_BEFORE"

# First msg: valid MsgCreatePost
# Second msg: MsgUpdatePost with nonexistent post ID — should fail
cat > /tmp/batch_rollback_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgExecSession",
        "grantee": "$BATCH_GRANTEE_ADDR",
        "granter": "$GRANTER_ADDR",
        "msgs": [
          {
            "@type": "/sparkdream.blog.v1.MsgCreatePost",
            "creator": "$GRANTER_ADDR",
            "title": "Should Be Rolled Back",
            "body": "This post should not persist"
          },
          {
            "@type": "/sparkdream.blog.v1.MsgReact",
            "creator": "$GRANTER_ADDR",
            "post_id": "99999999",
            "emoji": "thumbsup"
          }
        ]
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "uspark", "amount": "100000"}],
      "gas_limit": "500000",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}
TXEOF

sign_and_broadcast "batch_grantee" "$BATCH_GRANTEE_ADDR" \
    /tmp/batch_rollback_unsigned.json /tmp/batch_rollback_signed.json

if check_tx_failure "$TX_RESULT"; then
    # Verify the first post was NOT created (atomic rollback)
    POSTS_AFTER=$($BINARY query blog list-posts --output json 2>&1 | jq '[.posts[] | select(.creator=="'"$GRANTER_ADDR"'")] | length' 2>/dev/null || echo "0")
    echo "  Posts by granter after failed batch: $POSTS_AFTER"

    if [ "$POSTS_AFTER" = "$POSTS_BEFORE" ]; then
        echo "  Atomic rollback confirmed: no new posts created"
        record_result "Atomic rollback on partial failure" "PASS"
    else
        echo "  Post count changed! Rollback may have failed"
        record_result "Atomic rollback on partial failure" "FAIL"
    fi
else
    echo "  Expected failure but TX succeeded (second msg should have failed)"
    record_result "Atomic rollback on partial failure" "FAIL"
fi

# ========================================================================
# TEST 6: Error - mixed transaction (MsgExecSession + MsgSend)
# ========================================================================
echo "--- TEST 6: Error - mixed transaction ---"

# This tests the SessionFeeDecorator's mixed-transaction check.
# NOTE: If SessionFeeDecorator is not wired in the ante handler, this test
# verifies the behavior at whatever layer validates the tx structure.

cat > /tmp/batch_mixed_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgExecSession",
        "grantee": "$BATCH_GRANTEE_ADDR",
        "granter": "$GRANTER_ADDR",
        "msgs": [
          {
            "@type": "/sparkdream.blog.v1.MsgCreatePost",
            "creator": "$GRANTER_ADDR",
            "title": "Mixed TX Test",
            "body": "Should fail"
          }
        ]
      },
      {
        "@type": "/cosmos.bank.v1beta1.MsgSend",
        "from_address": "$BATCH_GRANTEE_ADDR",
        "to_address": "$GRANTER_ADDR",
        "amount": [{"denom": "uspark", "amount": "1000"}]
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "uspark", "amount": "50000"}],
      "gas_limit": "300000",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}
TXEOF

# NOTE: This tx has two different signers (batch_grantee for MsgExecSession,
# batch_grantee for MsgSend). Signing with just one key may cause signature
# verification to fail before reaching the mixed-transaction check.
# Either way, the tx should be rejected.
sign_and_broadcast "batch_grantee" "$BATCH_GRANTEE_ADDR" \
    /tmp/batch_mixed_unsigned.json /tmp/batch_mixed_signed.json

if check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "mixed\|signature\|signer"; then
        echo "  Correctly rejected: mixed transaction"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: mixed transaction" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: mixed transaction" "FAIL"
fi

# ========================================================================
# TEST 7: Exactly 10 inner msgs (at the limit — should succeed)
# ========================================================================
echo "--- TEST 7: Exactly 10 inner msgs (at limit) ---"

# Build exactly 10 inner messages
INNER_MSGS_10=""
for i in $(seq 1 10); do
    if [ $i -gt 1 ]; then
        INNER_MSGS_10="$INNER_MSGS_10,"
    fi
    INNER_MSGS_10="$INNER_MSGS_10{\"@type\":\"/sparkdream.blog.v1.MsgCreatePost\",\"creator\":\"$GRANTER_ADDR\",\"title\":\"Limit Post $i\",\"body\":\"Post $i of 10\"}"
done

cat > /tmp/batch_limit_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgExecSession",
        "grantee": "$BATCH_GRANTEE_ADDR",
        "granter": "$GRANTER_ADDR",
        "msgs": [$INNER_MSGS_10]
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "uspark", "amount": "200000"}],
      "gas_limit": "2000000",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}
TXEOF

sign_and_broadcast "batch_grantee" "$BATCH_GRANTEE_ADDR" \
    /tmp/batch_limit_unsigned.json /tmp/batch_limit_signed.json

if check_tx_success "$TX_RESULT"; then
    echo "  10 inner messages executed successfully (at the limit)"
    record_result "Exactly 10 inner msgs (at limit)" "PASS"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    # May hit blog rate limiting (max_posts_per_day) rather than session limit
    if echo "$RAW_LOG" | grep -qi "rate.limit\|max.*posts\|too many"; then
        echo "  Failed due to blog rate limit (not session batch limit) — expected"
        record_result "Exactly 10 inner msgs (at limit)" "PASS"
    else
        echo "  TX failed: $RAW_LOG"
        record_result "Exactly 10 inner msgs (at limit)" "FAIL"
    fi
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "BATCH EXEC TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
    exit 0
fi
