#!/bin/bash
# x/artifact waves 3 and 4: crafting recipes (append-locked after the first
# craft) and rentals (offer, paid acceptance, RENTED lock, early end, expiry).

echo "--- TESTING ARTIFACT: CRAFTING AND RENTALS ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

FEES=60000
PARAMS=$(q params | jq '.params')
DEPOSIT=$(echo "$PARAMS" | jq -r '.token_deposit')
FEE_BPS=$(echo "$PARAMS" | jq -r '.sale_fee_bps')

lock_of() { q token "$1" "$2" | jq -r '.token.token.lock // "TOKEN_LOCK_NONE"'; }

echo "=== Crafting ==="
expect_ok "alice creates a shard class" create-class "Aurora Shards" "$LICENSE" --flags '{"transferable":true}' --from alice
SHARDS=$(tx_event class_id artifact_class_created)
expect_ok "alice creates a herb edition class" create-class "Aurora Herbs" "$LICENSE" \
    --flags '{"transferable":true}' --token-model editions --from alice
HERBS=$(tx_event class_id artifact_class_created)
expect_ok "alice creates the herb edition" create-edition "$HERBS" "$LICENSE" --metadata '{"name":"moonleaf"}' --from alice
HERB=$(tx_event edition_id artifact_edition_created)
expect_ok "alice creates a relic class" create-class "Aurora Relics" "$LICENSE" --flags '{"transferable":true}' --from alice
RELICS=$(tx_event class_id artifact_class_created)
expect_fail "only the class owner adds recipes" "class owner" add-craft-recipe "$RELICS" "$LICENSE" \
    --inputs "{\"class_id\":\"$SHARDS\",\"quantity\":\"2\"}" --output-metadata '{"name":"relic"}' --from bob
expect_ok "alice adds: 2 shards + 3 moonleaf -> relic" add-craft-recipe "$RELICS" "$LICENSE" \
    --inputs "{\"class_id\":\"$SHARDS\",\"quantity\":\"2\"}" \
    --inputs "{\"class_id\":\"$HERBS\",\"edition_id\":\"$HERB\",\"quantity\":\"3\"}" \
    --output-metadata '{"name":"aurora relic"}' --from alice
RECIPE=$(tx_event recipe_id artifact_craft_recipe_added)
check "the recipe is listed" "$(q craft-recipes "$RELICS" | jq -r '.recipes | length')" "1"
expect_ok "alice mints 3 shards to bob" mint "$SHARDS" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"shard a\"}}" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"shard b\"}}" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"shard c\"}}" --from alice
expect_ok "alice gives bob 5 moonleaf" mint-units "$HERBS" "$HERB" --entries "{\"recipient\":\"$BOB_ADDR\",\"amount\":\"5\"}" --from alice
expect_fail "the inputs must match the recipe" "match" craft "$RELICS" "$RECIPE" \
    --input-tokens "{\"class_id\":\"$SHARDS\",\"token_id\":\"1\"}" --from bob
BOB_B=$(balance "$BOB_ADDR")
expect_ok "bob crafts a relic" craft "$RELICS" "$RECIPE" \
    --input-tokens "{\"class_id\":\"$SHARDS\",\"token_id\":\"1\"}" --input-tokens "{\"class_id\":\"$SHARDS\",\"token_id\":\"2\"}" --from bob
RELIC=$(tx_event token_id artifact_crafted)
check "bob owns the relic" "$(q owner "$RELICS" "$RELIC" | jq -r '.owner')" "$BOB_ADDR"
check "the relic carries the recipe's metadata" "$(q token "$RELICS" "$RELIC" | jq -r '.token.token.metadata.name')" "aurora relic"
check "two shard deposits back, one relic deposit out" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "$((DEPOSIT - FEES))"
check "three moonleaf were burned" "$(q balance "$HERBS" "$HERB" "$BOB_ADDR" | jq -r '.balance.balance.amount')" "2"
check "the shards are burned" "$(q supply "$SHARDS" | jq -r '"\(.supply // "0")/\(.burned // "0")"')" "1/2"
check "the recipe list is now locked" "$(q craft-recipes "$RELICS" | jq -r '.locked // false')" "true"
expect_fail "no recipe can be added after the first craft" "after the first craft" add-craft-recipe "$RELICS" "$LICENSE" \
    --inputs "{\"class_id\":\"$SHARDS\",\"quantity\":\"1\"}" --output-metadata '{"name":"cheap relic"}' --from alice
expect_ok "alice may still remove the recipe" remove-craft-recipe "$RELICS" "$RECIPE" --from alice
expect_fail "a removed recipe cannot be crafted" "recipe not found" craft "$RELICS" "$RECIPE" \
    --input-tokens "{\"class_id\":\"$SHARDS\",\"token_id\":\"3\"}" --from bob

echo ""
echo "=== Rentals ==="
expect_ok "alice creates a game item class (5% royalty)" create-class "Aurora Mounts" "$LICENSE" \
    --flags '{"transferable":true}' --royalty-bps 500 --from alice
MOUNTS=$(tx_event class_id artifact_class_created)
expect_ok "alice mints a mount to bob" mint "$MOUNTS" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"star griffin\"}}" --from alice
expect_ok "bob offers a 20-second rental for 1 SPARK" offer-rental "$MOUNTS" 1 "1000000${BOND_DENOM}" 20 600 --from bob
NONCE=$(q rental-offer "$MOUNTS" 1 | jq -r '.offer.nonce')
expect_fail "a stale nonce is refused" "changed" accept-rental "$MOUNTS" 1 "1000000${BOND_DENOM}" "$((NONCE + 1))" --from carol
BOB_B=$(balance "$BOB_ADDR")
expect_ok "carol rents the mount" accept-rental "$MOUNTS" 1 "1000000${BOND_DENOM}" "$NONCE" --from carol
FEE=$((1000000 * FEE_BPS / 10000)); ROYALTY=$((1000000 * 500 / 10000))
check "bob received the rent minus fee and royalty" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "$((1000000 - FEE - ROYALTY))"
check "the mount is rented" "$(lock_of "$MOUNTS" 1)" "TOKEN_LOCK_RENTED"
check "carol is the user" "$(q rental "$MOUNTS" 1 | jq -r '.rental.user')" "$CAROL_ADDR"
check "carol's rentals list it" "$(q rentals-by-user "$CAROL_ADDR" | jq -r '.rentals | length')" "1"
check "ownership did not move" "$(q owner "$MOUNTS" 1 | jq -r '.owner')" "$BOB_ADDR"
expect_fail "the owner cannot transfer a rented token" "locked" transfer \
    --entries "{\"class_id\":\"$MOUNTS\",\"token_id\":\"1\",\"recipient\":\"$ALICE_ADDR\"}" --from bob
expect_fail "the owner cannot list a rented token" "locked" list "$MOUNTS" 1 "5000000${BOND_DENOM}" 60 --from bob
expect_fail "the owner cannot end it early" "cannot be ended" end-rental "$MOUNTS" 1 --from bob
EXP=$(q rental "$MOUNTS" 1 | jq -r '.rental.expires_at')
wait_until_time "$EXP"
check "the EndBlocker ended the rental" "$(lock_of "$MOUNTS" 1)" "TOKEN_LOCK_NONE"
check "no rental remains" "$(q rental "$MOUNTS" 1 > /dev/null 2>&1 && echo present || echo gone)" "gone"

expect_ok "bob offers an early-end rental to carol only" offer-rental "$MOUNTS" 1 "0${BOND_DENOM}" 600 600 \
    --user "$CAROL_ADDR" --owner-may-end --from bob
NONCE=$(q rental-offer "$MOUNTS" 1 | jq -r '.offer.nonce')
expect_fail "the offer is not open to the outsider" "not open" accept-rental "$MOUNTS" 1 "0${BOND_DENOM}" "$NONCE" --from artifact_outsider
expect_ok "carol rents it for free" accept-rental "$MOUNTS" 1 "0${BOND_DENOM}" "$NONCE" --from carol
expect_ok "bob ends it early, as the offer allowed" end-rental "$MOUNTS" 1 --from bob
check "the mount is free again" "$(lock_of "$MOUNTS" 1)" "TOKEN_LOCK_NONE"
expect_ok "bob offers it again" offer-rental "$MOUNTS" 1 "1000000${BOND_DENOM}" 600 600 --from bob
expect_ok "bob transfers the mount" transfer --entries "{\"class_id\":\"$MOUNTS\",\"token_id\":\"1\",\"recipient\":\"$ALICE_ADDR\"}" --from bob
check "a transfer drops the offer" "$(q rental-offer "$MOUNTS" 1 > /dev/null 2>&1 && echo present || echo gone)" "gone"

finish
