#!/bin/bash
# x/artifact: public mints (price protection, per-address limit, payout) and
# soulbound badge classes and burn authorization.

echo "--- TESTING ARTIFACT: PUBLIC MINT & SOULBOUND ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

FEES=60000
echo "=== Public mint ==="
expect_ok "alice opens a 2 SPARK public edition paid to carol" create-class "Aurora Open Edition" "$LICENSE" \
    --flags '{"transferable":true}' --token-uri-base "ipfs://bafyopenedition/" --payout-address "$CAROL_ADDR" \
    --mint-policy "{\"public_mint_enabled\":true,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"2000000\"},\"per_address_limit\":2}" \
    --from alice
CLASS_ID=$(tx_event class_id artifact_class_created)
QUOTE=$(q public-mint-quote "$CLASS_ID" "$OUTSIDER_ADDR" 1)
check "quote available" "$(echo "$QUOTE" | jq -r '.available')" "true"
expect_fail "max price below the price" "exceeds" public-mint "$CLASS_ID" 1 "1000000${BOND_DENOM}" --from artifact_outsider
BUYER_B=$(balance "$OUTSIDER_ADDR"); CAROL_B=$(balance "$CAROL_ADDR")
expect_ok "outsider buys 2" public-mint "$CLASS_ID" 2 "2000000${BOND_DENOM}" --from artifact_outsider
FEE_BPS=$(q params | jq -r '.params.sale_fee_bps')
DEPOSIT=$(q params | jq -r '.params.token_deposit')
TOTAL=4000000; FEE=$((TOTAL * FEE_BPS / 10000))
check "buyer paid price + deposits + gas" "$((BUYER_B - $(balance "$OUTSIDER_ADDR")))" "$((TOTAL + 2 * DEPOSIT + FEES))"
check "payout got price minus fee" "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((TOTAL - FEE))"
check "outsider owns token 2" "$(q owner "$CLASS_ID" 2 | jq -r '.owner')" "$OUTSIDER_ADDR"
expect_fail "per-address limit" "limit" public-mint "$CLASS_ID" 1 "2000000${BOND_DENOM}" --from artifact_outsider
expect_ok "owner disables public mint" set-mint-policy "$CLASS_ID" \
    --mint-policy "{\"public_mint_enabled\":false,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"2000000\"}}" --from alice
expect_fail "public mint closed" "not open|disabled" public-mint "$CLASS_ID" 1 "2000000${BOND_DENOM}" --from bob

echo ""
echo "=== Soulbound badge the issuer can revoke ==="
expect_fail "soulbound classes carry no royalty" "royalty|flags" create-class "Badge" "$LICENSE" \
    --flags '{"burn_authorization":"BURN_AUTHORIZATION_HOLDER_OR_ISSUER"}' --royalty-bps 100 --from alice
expect_ok "alice creates a revocable badge class" create-class "Initiative Contributor" "$LICENSE" \
    --flags '{"burn_authorization":"BURN_AUTHORIZATION_HOLDER_OR_ISSUER"}' --from alice
BADGE_ID=$(tx_event class_id artifact_class_created)
expect_ok "alice issues badges to bob and carol" mint "$BADGE_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"season-3\"}}" \
    --entries "{\"recipient\":\"$CAROL_ADDR\",\"metadata\":{\"name\":\"season-3\"}}" --from alice
expect_fail "badges cannot be transferred" "not transferable" \
    transfer --entries "{\"class_id\":\"$BADGE_ID\",\"token_id\":\"1\",\"recipient\":\"$CAROL_ADDR\"}" --from bob
expect_fail "badges cannot be listed" "not transferable" list "$BADGE_ID" 1 "1000000${BOND_DENOM}" 60 --from bob
BOB_B=$(balance "$BOB_ADDR"); ALICE_B=$(balance "$ALICE_ADDR")
expect_ok "alice revokes bob's badge" revoke "$BADGE_ID" --token-ids 1 --reason "term ended" --from alice
check "deposit refunded to the holder, not the issuer" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "$DEPOSIT"
check "issuer paid only gas" "$((ALICE_B - $(balance "$ALICE_ADDR")))" "$FEES"
expect_ok "carol can burn her badge" burn --refs "{\"class_id\":\"$BADGE_ID\",\"token_id\":\"2\"}" --from carol
expect_fail "a holder-only class cannot revoke" "issuer burn" revoke "$CLASS_ID" --token-ids 1 --from alice

echo ""
echo "=== Issuer-only burn ==="
expect_ok "alice creates an issuer-only credential class" create-class "Aurora Credential" "$LICENSE" \
    --flags '{"burn_authorization":"BURN_AUTHORIZATION_ISSUER"}' --from alice
CRED_ID=$(tx_event class_id artifact_class_created)
expect_ok "alice issues a credential to bob" mint "$CRED_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"aurora-1\"}}" --from alice
check "an unburnable token waits in bob's inbox" \
    "$(q inbox "$BOB_ADDR" | jq -r --arg c "$CRED_ID" '[.items[]? | select(.class_id == $c)] | length')" "1"
expect_ok "bob accepts the credential" \
    accept-incoming --entries "{\"class_id\":\"$CRED_ID\",\"token_id\":\"1\"}" --from bob
expect_fail "bob cannot burn an issuer-only credential" "holder burn" \
    burn --refs "{\"class_id\":\"$CRED_ID\",\"token_id\":\"1\"}" --from bob
BOB_B=$(balance "$BOB_ADDR")
expect_ok "alice revokes the credential" revoke "$CRED_ID" --token-ids 1 --reason "expired" --from alice
check "deposit refunded to the holder" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "$DEPOSIT"

finish
