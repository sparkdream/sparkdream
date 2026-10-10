#!/bin/bash
# x/artifact: transfers, receive policies, the inbox (accept / reject /
# cancel / expiry), burns and deposit refunds.

echo "--- TESTING ARTIFACT: TRANSFER & INBOX ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

ref() { echo "{\"class_id\":\"$1\",\"token_id\":\"$2\"}"; }

expect_ok "alice creates a class" create-class "Aurora Inbox" "$LICENSE" \
    --flags '{"transferable":true}' --from alice
CLASS_ID=$(tx_event class_id artifact_class_created)
expect_ok "alice mints 4 tokens to herself" mint "$CLASS_ID" "$LICENSE" \
    --entries '{"metadata":{"name":"z1"}}' --entries '{"metadata":{"name":"z2"}}' \
    --entries '{"metadata":{"name":"z3"}}' --entries '{"metadata":{"name":"z4"}}' --from alice

echo ""
echo "=== Default policy (MEMBERS) ==="
check "outsider default policy" "$(q receive-policy "$OUTSIDER_ADDR" | jq -r '.policy')" "RECEIVE_POLICY_MEMBERS"
expect_ok "alice (member) -> outsider delivers directly" \
    transfer --entries "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"1\",\"recipient\":\"$OUTSIDER_ADDR\"}" --from alice
check "outsider owns token 1" "$(q owner "$CLASS_ID" 1 | jq -r '.owner')" "$OUTSIDER_ADDR"
expect_ok "outsider (untrusted) -> bob goes to bob's inbox" \
    transfer --entries "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"1\",\"recipient\":\"$BOB_ADDR\"}" --from artifact_outsider
check "token 1 still owned by outsider" "$(q owner "$CLASS_ID" 1 | jq -r '.owner')" "$OUTSIDER_ADDR"
check "token 1 locked pending" "$(q token "$CLASS_ID" 1 | jq -r '.token.token.lock')" "TOKEN_LOCK_PENDING_TRANSFER"
check "bob's inbox has 1 item" "$(q inbox "$BOB_ADDR" | jq -r '.inbox_count // 0')" "1"
expect_fail "locked token cannot move again" "locked" \
    transfer --entries "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"1\",\"recipient\":\"$CAROL_ADDR\"}" --from artifact_outsider
HASH=$(q inbox "$BOB_ADDR" | jq -r '.items[0].metadata_hash')
expect_fail "accept with a wrong metadata hash" "hash" \
    accept-incoming --entries "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"1\",\"expected_metadata_hash\":\"00\"}" --from bob
expect_ok "bob accepts with the pinned hash" \
    accept-incoming --entries "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"1\",\"expected_metadata_hash\":\"$HASH\"}" --from bob
check "bob owns token 1" "$(q owner "$CLASS_ID" 1 | jq -r '.owner')" "$BOB_ADDR"

echo ""
echo "=== INBOX policy, reject and cancel ==="
expect_ok "carol sets INBOX policy" set-receive-policy inbox --from carol
expect_ok "alice -> carol (inbox)" \
    transfer --entries "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"2\",\"recipient\":\"$CAROL_ADDR\"}" --from alice
expect_ok "carol rejects" reject-incoming --refs "$(ref "$CLASS_ID" 2)" --from carol
check "token 2 back with alice, unlocked" "$(q token "$CLASS_ID" 2 | jq -r '.token.token.lock // "TOKEN_LOCK_NONE"')" "TOKEN_LOCK_NONE"
expect_ok "alice -> carol again" \
    transfer --entries "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"2\",\"recipient\":\"$CAROL_ADDR\"}" --from alice
expect_ok "alice cancels" cancel-outgoing --refs "$(ref "$CLASS_ID" 2)" --from alice
check "outbox empty" "$(q outbox "$ALICE_ADDR" | jq -r '.items // [] | length')" "0"

echo ""
echo "=== Pending mint deposits ==="
ALICE_BEFORE=$(balance "$ALICE_ADDR")
expect_ok "alice mints to carol's inbox" mint "$CLASS_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$CAROL_ADDR\",\"metadata\":{\"name\":\"gift\"}}" --from alice
check "reserved supply 1" "$(q supply "$CLASS_ID" | jq -r '.reserved_supply // "0"')" "1"
PENDING_ID=$(q inbox "$CAROL_ADDR" | jq -r '.items[0].token_id')
expect_ok "carol rejects the pending mint" reject-incoming --refs "$(ref "$CLASS_ID" "$PENDING_ID")" --from carol
check "reserved supply 0" "$(q supply "$CLASS_ID" | jq -r '.reserved_supply // "0"')" "0"
check "id stays consumed" "$(q class "$CLASS_ID" | jq -r '.class.class.next_token_id')" "$((PENDING_ID + 1))"

echo ""
echo "=== Expiry ==="
TTL=$(q params | jq -r '.params.pending_ttl')
expect_ok "alice -> carol (will expire)" \
    transfer --entries "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"3\",\"recipient\":\"$CAROL_ADDR\"}" --from alice
SENT_AT=$(block_time_unix)
wait_until_time $((SENT_AT + TTL + 2))
check "expired item gone from inbox" "$(q inbox "$CAROL_ADDR" | jq -r '.items // [] | length')" "0"
check "token 3 unlocked after expiry" "$(q token "$CLASS_ID" 3 | jq -r '.token.token.lock // "TOKEN_LOCK_NONE"')" "TOKEN_LOCK_NONE"
check "token 3 still alice's" "$(q owner "$CLASS_ID" 3 | jq -r '.owner')" "$ALICE_ADDR"
expect_ok "carol back to the default policy" set-receive-policy unspecified --from carol

echo ""
echo "=== Burn refunds the holder ==="
DEPOSIT=$(q params | jq -r '.params.token_deposit')
BOB_BEFORE=$(balance "$BOB_ADDR")
expect_ok "bob burns token 1 (minted and paid for by alice)" burn --refs "$(ref "$CLASS_ID" 1)" --from bob
BOB_AFTER=$(balance "$BOB_ADDR")
FEES=60000
check "bob received the deposit (minus fees)" "$((BOB_AFTER - BOB_BEFORE + FEES))" "$DEPOSIT"
expect_fail "token 1 is gone" "not found" burn --refs "$(ref "$CLASS_ID" 1)" --from bob

finish
