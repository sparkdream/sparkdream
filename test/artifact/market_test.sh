#!/bin/bash
# x/artifact: fixed-price listings, nonce / price protection, settlement
# split (fee, royalty split across recipients, proceeds), expiry and delisting.

echo "--- TESTING ARTIFACT: MARKET ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

FEES=60000
expect_ok "alice creates a class with a 5% royalty split 70/30 carol/alice" create-class "Zenith Market" "$LICENSE" \
    --flags '{"transferable":true}' --royalty-bps 500 \
    --royalty-recipients "{\"address\":\"$CAROL_ADDR\",\"weight_bps\":7000}" \
    --royalty-recipients "{\"address\":\"$ALICE_ADDR\",\"weight_bps\":3000}" --from alice
CLASS_ID=$(tx_event class_id artifact_class_created)
expect_ok "alice mints to bob" mint "$CLASS_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"m1\"}}" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"m2\"}}" --from alice

expect_fail "DREAM is not a price" "DREAM" list "$CLASS_ID" 1 "5${DREAM_DENOM}" 3600 --from bob
expect_fail "only the owner lists" "owner" list "$CLASS_ID" 1 "5000000${BOND_DENOM}" 3600 --from alice
expect_ok "bob lists token 1 at 10 SPARK" list "$CLASS_ID" 1 "10000000${BOND_DENOM}" 3600 --from bob
check "nonce 1" "$(q listing "$CLASS_ID" 1 | jq -r '.listing.nonce')" "1"
expect_fail "listed token cannot be transferred" "locked" \
    transfer --entries "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"1\",\"recipient\":\"$CAROL_ADDR\"}" --from bob

expect_ok "bob raises the price to 20 SPARK" update-listing "$CLASS_ID" 1 "20000000${BOND_DENOM}" 3600 --from bob
expect_fail "a buy against the old price fails" "changed" buy "$CLASS_ID" 1 "10000000${BOND_DENOM}" 1 --from artifact_outsider
expect_fail "a buy against the old nonce fails" "changed" buy "$CLASS_ID" 1 "20000000${BOND_DENOM}" 1 --from artifact_outsider
expect_fail "seller cannot buy" "own listing" buy "$CLASS_ID" 1 "20000000${BOND_DENOM}" 2 --from bob

BUYER_B=$(balance "$OUTSIDER_ADDR"); BOB_B=$(balance "$BOB_ADDR"); CAROL_B=$(balance "$CAROL_ADDR"); ALICE_B=$(balance "$ALICE_ADDR")
expect_ok "outsider buys at 20 SPARK, nonce 2" buy "$CLASS_ID" 1 "20000000${BOND_DENOM}" 2 --from artifact_outsider
FEE_BPS=$(q params | jq -r '.params.sale_fee_bps')
PRICE=20000000
FEE=$((PRICE * FEE_BPS / 10000)); ROYALTY=$((PRICE * 500 / 10000))
check "buyer paid price + gas" "$((BUYER_B - $(balance "$OUTSIDER_ADDR")))" "$((PRICE + FEES))"
check "seller got proceeds" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "$((PRICE - FEE - ROYALTY))"
check "carol got 70% of the royalty" "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((ROYALTY * 7000 / 10000))"
check "alice got 30% of the royalty" "$(( $(balance "$ALICE_ADDR") - ALICE_B ))" "$((ROYALTY - ROYALTY * 7000 / 10000))"
check "outsider owns token 1" "$(q owner "$CLASS_ID" 1 | jq -r '.owner')" "$OUTSIDER_ADDR"
check "sold event royalty" "$(tx_event royalty artifact_sold)" "$ROYALTY"

echo ""
echo "=== Relist nonce grows ==="
expect_ok "outsider relists" list "$CLASS_ID" 1 "20000000${BOND_DENOM}" 3600 --from artifact_outsider
check "nonce 3 (monotonic per token)" "$(q listing "$CLASS_ID" 1 | jq -r '.listing.nonce')" "3"
expect_ok "outsider delists" delist "$CLASS_ID" 1 --from artifact_outsider

echo ""
echo "=== Listing expiry ==="
expect_ok "bob lists token 2 for 5 seconds" list "$CLASS_ID" 2 "1000000${BOND_DENOM}" 5 --from bob
LISTED_AT=$(block_time_unix)
wait_until_time $((LISTED_AT + 6))
expect_fail "expired listing cannot be bought" "not found|expired" buy "$CLASS_ID" 2 "1000000${BOND_DENOM}" 1 --from carol
check "token 2 unlocked" "$(q token "$CLASS_ID" 2 | jq -r '.token.token.lock // "TOKEN_LOCK_NONE"')" "TOKEN_LOCK_NONE"
check "no active listings in class" "$(q listings --class-id "$CLASS_ID" | jq -r '.listings // [] | length')" "0"

finish
