#!/bin/bash
# x/artifact market: escrowed token and collection offers, English auctions
# with outbid refunds, anti-sniping and EndBlocker settlement, and unit
# listing updates.

echo "--- TESTING ARTIFACT: OFFERS, AUCTIONS, UNIT LISTING UPDATES ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

FEES=60000
PARAMS=$(q params | jq '.params')
FEE_BPS=$(echo "$PARAMS" | jq -r '.sale_fee_bps')
EXT=$(echo "$PARAMS" | jq -r '.auction_extension')

echo "=== Token and collection offers ==="
expect_ok "alice creates an art class (no royalty)" create-class "Aurora Prints" "$LICENSE" --flags '{"transferable":true}' --from alice
ART=$(tx_event class_id artifact_class_created)
expect_ok "alice mints two prints to bob" mint "$ART" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"print 1\"}}" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"print 2\"}}" --from alice
CAROL_B=$(balance "$CAROL_ADDR")
expect_ok "carol offers 3 SPARK for print 1" make-offer "$ART" 1 "3000000${BOND_DENOM}" 600 --from carol
OFFER=$(tx_event offer_id artifact_offer_made)
check "the offer is escrowed" "$((CAROL_B - $(balance "$CAROL_ADDR")))" "$((3000000 + FEES))"
expect_fail "bob cannot bid on his own token" "already hold" make-offer "$ART" 1 "1000000${BOND_DENOM}" 600 --from bob
expect_fail "DREAM never buys a token" "DREAM" make-offer "$ART" 1 "5${DREAM_DENOM}" 600 --from carol
check "the token lists the offer" "$(q offers "$ART" 1 | jq -r '.offers | length')" "1"
expect_ok "bob lists print 1 meanwhile" list "$ART" 1 "9000000${BOND_DENOM}" 600 --from bob
expect_fail "a changed price is refused" "changed" accept-offer "$OFFER" 1 "2000000${BOND_DENOM}" --from bob
BOB_B=$(balance "$BOB_ADDR")
expect_ok "bob accepts carol's offer" accept-offer "$OFFER" 1 "3000000${BOND_DENOM}" --from bob
FEE=$((3000000 * FEE_BPS / 10000))
check "bob received the price minus the fee" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "$((3000000 - FEE - FEES))"
check "carol owns print 1" "$(q owner "$ART" 1 | jq -r '.owner')" "$CAROL_ADDR"
check "the listing went with the sale" "$(q listing "$ART" 1 > /dev/null 2>&1 && echo present || echo gone)" "gone"

expect_ok "the outsider makes a collection offer of 1 SPARK" make-offer "$ART" 0 "1000000${BOND_DENOM}" 600 --from artifact_outsider
COFFER=$(tx_event offer_id artifact_offer_made)
check "collection offers sit under token 0" "$(q offers "$ART" 0 | jq -r '.offers | length')" "1"
expect_ok "bob sells print 2 into the collection offer" accept-offer "$COFFER" 2 "1000000${BOND_DENOM}" --from bob
check "the outsider owns print 2" "$(q owner "$ART" 2 | jq -r '.owner')" "$OUTSIDER_ADDR"

expect_ok "carol offers on print 2" make-offer "$ART" 2 "2000000${BOND_DENOM}" 600 --from carol
OFFER2=$(tx_event offer_id artifact_offer_made)
CAROL_B=$(balance "$CAROL_ADDR")
expect_ok "carol withdraws it" cancel-offer "$OFFER2" --from carol
check "the escrow came back" "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((2000000 - FEES))"
check "carol has no open offers" "$(q offers-by-bidder "$CAROL_ADDR" | jq -r '.offers | length')" "0"

echo ""
echo "=== English auction ==="
expect_ok "carol auctions print 1: reserve 2 SPARK, +10% steps, 30 seconds" start-auction "$ART" 1 "2000000${BOND_DENOM}" 30 \
    --min-increment-bps 1000 --from carol
AUC=$(tx_event auction_id artifact_auction_started)
check "the token is AUCTION-locked" "$(q token "$ART" 1 | jq -r '.token.token.lock')" "TOKEN_LOCK_AUCTION"
expect_fail "a locked token cannot move" "locked" transfer \
    --entries "{\"class_id\":\"$ART\",\"token_id\":\"1\",\"recipient\":\"$ALICE_ADDR\"}" --from carol
expect_fail "below the reserve" "too low" bid "$AUC" "1000000${BOND_DENOM}" --from bob
expect_ok "bob bids the reserve" bid "$AUC" "2000000${BOND_DENOM}" --from bob
expect_fail "the next bid must beat 10%" "too low" bid "$AUC" "2100000${BOND_DENOM}" --from artifact_outsider
check "the auction quotes the next minimum" "$(q auction "$AUC" | jq -r '.min_next_bid')" "2200000"
BOB_B=$(balance "$BOB_ADDR")
expect_ok "the outsider outbids bob" bid "$AUC" "2200000${BOND_DENOM}" --from artifact_outsider
check "bob's bid came back at once" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "2000000"
expect_fail "a bid auction cannot be cancelled" "running|bids" cancel-auction "$AUC" --from carol
ENDS=$(q auction "$AUC" | jq -r '.auction.ends_at')
CAROL_B=$(balance "$CAROL_ADDR")
wait_until_time "$((ENDS + 1))"
check "the EndBlocker settled the auction" "$(q owner "$ART" 1 | jq -r '.owner')" "$OUTSIDER_ADDR"
FEE=$((2200000 * FEE_BPS / 10000))
check "carol received the high bid minus the fee" "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((2200000 - FEE))"
check "the token is unlocked" "$(q token "$ART" 1 | jq -r '.token.token.lock // "TOKEN_LOCK_NONE"')" "TOKEN_LOCK_NONE"

expect_ok "the outsider auctions print 2 with nobody watching" start-auction "$ART" 2 "5000000${BOND_DENOM}" 600 --from artifact_outsider
AUC2=$(tx_event auction_id artifact_auction_started)
expect_ok "a bidless auction can be cancelled" cancel-auction "$AUC2" --from artifact_outsider
check "no auctions remain" "$(q auctions --class-id "$ART" | jq -r '.auctions | length')" "0"

echo ""
echo "=== Unit listing updates ==="
expect_ok "alice creates an editions class" create-class "Zenith Seeds" "$LICENSE" --flags '{"transferable":true}' --token-model editions --from alice
SEEDS=$(tx_event class_id artifact_class_created)
expect_ok "alice creates an edition" create-edition "$SEEDS" "$LICENSE" --metadata '{"name":"moon seed"}' --from alice
SEED=$(tx_event edition_id artifact_edition_created)
expect_ok "alice mints 10 units to bob" mint-units "$SEEDS" "$SEED" --entries "{\"recipient\":\"$BOB_ADDR\",\"amount\":\"10\"}" --from alice
expect_ok "bob lists 4 at 1 SPARK" list-units "$SEEDS" "$SEED" 4 "1000000${BOND_DENOM}" 600 --from bob
LID=$(tx_event listing_id artifact_units_listed)
expect_ok "bob raises the quantity to 7 at 2 SPARK" update-unit-listing "$LID" 7 "2000000${BOND_DENOM}" 600 --from bob
check "7 units are locked" "$(q balance "$SEEDS" "$SEED" "$BOB_ADDR" | jq -r '.balance.balance.locked')" "7"
expect_fail "the old price no longer buys" "changed" buy-units "$LID" 1 "1000000${BOND_DENOM}" --from carol
expect_fail "cannot list more than held" "insufficient units" update-unit-listing "$LID" 11 "2000000${BOND_DENOM}" 600 --from bob
expect_ok "bob trims it to 2" update-unit-listing "$LID" 2 "2000000${BOND_DENOM}" 600 --from bob
check "2 units are locked" "$(q balance "$SEEDS" "$SEED" "$BOB_ADDR" | jq -r '.balance.balance.locked')" "2"
expect_ok "carol buys both at the new price" buy-units "$LID" 2 "2000000${BOND_DENOM}" --from carol
check "carol holds 2" "$(q balance "$SEEDS" "$SEED" "$CAROL_ADDR" | jq -r '.balance.balance.amount')" "2"

finish
