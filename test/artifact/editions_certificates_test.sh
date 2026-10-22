#!/bin/bash
# x/artifact wave 3: semi-fungible editions (unit mint, transfer, inbox,
# partial-quantity listings, burns), impact certificates with retroactive
# SPARK funding, and the NEITHER burn mode.

echo "--- TESTING ARTIFACT: EDITIONS, IMPACT CERTIFICATES, NEITHER ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

FEES=60000
PARAMS=$(q params | jq '.params')
DEPOSIT=$(echo "$PARAMS" | jq -r '.token_deposit')
INBOX_FEE=$(echo "$PARAMS" | jq -r '.inbox_fee')
FEE_BPS=$(echo "$PARAMS" | jq -r '.sale_fee_bps')

units() { q balance "$1" "$2" "$3" | jq -r '.balance.balance.amount // "0"'; }
locked() { q balance "$1" "$2" "$3" | jq -r '.balance.balance.locked // "0"'; }

echo "=== Editions ==="
expect_ok "alice creates an editions class" create-class "Aurora Potions" "$LICENSE" \
    --flags '{"transferable":true}' --token-model editions --royalty-bps 500 --from alice
POT=$(tx_event class_id artifact_class_created)
expect_fail "unique mints are refused on an editions class" "token model" mint "$POT" "$LICENSE" \
    --entries '{"metadata":{"name":"x"}}' --from alice
ALICE_B=$(balance "$ALICE_ADDR")
expect_ok "alice creates a capped edition" create-edition "$POT" "$LICENSE" \
    --metadata '{"name":"healing potion"}' --max-supply 100 --from alice
ED=$(tx_event edition_id artifact_edition_created)
check "the edition burns one deposit as its fee" "$((ALICE_B - $(balance "$ALICE_ADDR")))" "$((DEPOSIT + FEES))"
check "class supply counts editions" "$(q supply "$POT" | jq -r '.supply // "0"')" "1"

ALICE_B=$(balance "$ALICE_ADDR")
expect_ok "alice mints 10 units to bob" mint-units "$POT" "$ED" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"amount\":\"10\"}" --from alice
check "bob holds 10" "$(units "$POT" "$ED" "$BOB_ADDR")" "10"
check "one record deposit for ten units" "$((ALICE_B - $(balance "$ALICE_ADDR")))" "$((DEPOSIT + FEES))"
expect_fail "the edition cap counts units ever issued" "supply" mint-units "$POT" "$ED" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"amount\":\"91\"}" --from alice
expect_ok "alice mints 5 units to the outsider" mint-units "$POT" "$ED" \
    --entries "{\"recipient\":\"$OUTSIDER_ADDR\",\"amount\":\"5\"}" --from alice

echo ""
echo "=== Unit inbox ==="
OUT_B=$(balance "$OUTSIDER_ADDR")
expect_ok "the untrusted outsider sends 2 units to bob" transfer-units \
    --entries "{\"class_id\":\"$POT\",\"edition_id\":\"$ED\",\"recipient\":\"$BOB_ADDR\",\"amount\":\"2\"}" --from artifact_outsider
PID=$(tx_event pending_id artifact_units_pending)
check "the transfer waits in bob's inbox" \
    "$(q inbox "$BOB_ADDR" | jq -r --arg p "$PID" '[.items[]? | select((.class_id // "0") == "0" and .token_id == $p)] | .[0].amount // ""')" "2"
check "the units are locked with the sender" "$(locked "$POT" "$ED" "$OUTSIDER_ADDR")" "2"
check "the outsider paid the inbox fee, an escrowed deposit and gas" "$((OUT_B - $(balance "$OUTSIDER_ADDR")))" "$((INBOX_FEE + DEPOSIT + FEES))"
expect_ok "bob accepts the units with ref (0, pending_id)" accept-incoming \
    --entries "{\"class_id\":\"0\",\"token_id\":\"$PID\"}" --from bob
check "bob holds 12" "$(units "$POT" "$ED" "$BOB_ADDR")" "12"
check "the escrow went back to the sender (bob had a record)" "$((OUT_B - $(balance "$OUTSIDER_ADDR")))" "$((INBOX_FEE + FEES))"
check "the outsider keeps 3" "$(units "$POT" "$ED" "$OUTSIDER_ADDR")" "3"

echo ""
echo "=== Unit market ==="
expect_ok "bob lists 6 units at 2 SPARK each" list-units "$POT" "$ED" 6 "2000000${BOND_DENOM}" 600 --from bob
LID=$(tx_event listing_id artifact_units_listed)
check "listed units are locked" "$(locked "$POT" "$ED" "$BOB_ADDR")" "6"
expect_fail "a stale unit price is refused" "changed" buy-units "$LID" 2 "1000000${BOND_DENOM}" --from carol
BOB_B=$(balance "$BOB_ADDR")
expect_ok "carol buys 2 of the 6" buy-units "$LID" 2 "2000000${BOND_DENOM}" --from carol
PRICE=4000000
FEE=$((PRICE * FEE_BPS / 10000)); ROYALTY=$((PRICE * 500 / 10000))
check "bob received price minus fee and royalty" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "$((PRICE - FEE - ROYALTY))"
check "carol holds 2" "$(units "$POT" "$ED" "$CAROL_ADDR")" "2"
check "4 units remain listed" "$(q unit-listing "$LID" | jq -r '.listing.quantity')" "4"
expect_fail "cannot buy more than is listed" "insufficient units" buy-units "$LID" 5 "2000000${BOND_DENOM}" --from carol
expect_ok "bob delists the rest" delist-units "$LID" --from bob
check "bob's units are unlocked" "$(locked "$POT" "$ED" "$BOB_ADDR")" "0"

echo ""
echo "=== Burning units ==="
CAROL_B=$(balance "$CAROL_ADDR")
expect_ok "carol burns her 2 units" burn-units "$POT" "$ED" 2 --from carol
check "emptying the balance refunds its deposit" "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((DEPOSIT - FEES))"
check "burned units are counted" "$(q edition "$POT" "$ED" | jq -r '.edition.edition.burned // "0"')" "2"
check "the record is gone" "$(q edition-holders "$POT" "$ED" | jq -r --arg a "$CAROL_ADDR" '[.balances[]? | select(.balance.owner == $a)] | length')" "0"

echo ""
echo "=== Impact certificates ==="
expect_ok "alice issues an impact certificate" create-edition "$POT" "$LICENSE" \
    --metadata '{"name":"aurora docs rewrite"}' \
    --claim "{\"scope\":\"rewrote the aurora handbook\",\"contributors\":[\"$ALICE_ADDR\",\"$BOB_ADDR\"],\"work_start\":\"1700000000\",\"work_end\":\"1700100000\"}" \
    --from alice
CERT=$(tx_event edition_id artifact_edition_created)
check "the certificate records its claim" "$(q edition "$POT" "$CERT" | jq -r '.edition.edition.claim.scope')" "rewrote the aurora handbook"
expect_ok "alice issues 60 shares to bob and 40 to carol" mint-units "$POT" "$CERT" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"amount\":\"60\"}" --entries "{\"recipient\":\"$CAROL_ADDR\",\"amount\":\"40\"}" --from alice
expect_fail "DREAM never funds a certificate" "DREAM" fund-edition "$POT" "$CERT" "5${DREAM_DENOM}" --from artifact_outsider
expect_ok "a retro funder pays 100 SPARK to the holders" fund-edition "$POT" "$CERT" "100000000${BOND_DENOM}" --from artifact_outsider
check "bob can claim 60 SPARK" "$(q balance "$POT" "$CERT" "$BOB_ADDR" | jq -r '.balance.claimable')" "60000000"
BOB_B=$(balance "$BOB_ADDR")
expect_ok "bob claims" claim-edition-rewards --refs "{\"class_id\":\"$POT\",\"edition_id\":\"$CERT\"}" --from bob
check "bob received 60 SPARK" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "$((60000000 - FEES))"
CAROL_B=$(balance "$CAROL_ADDR")
expect_ok "carol moves all her shares to the outsider after the funding" transfer-units \
    --entries "{\"class_id\":\"$POT\",\"edition_id\":\"$CERT\",\"recipient\":\"$OUTSIDER_ADDR\",\"amount\":\"40\"}" --from carol
check "emptying her balance paid carol her 40 SPARK (her deposit funded the new record)" \
    "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((40000000 - FEES))"
check "the outsider inherits no past rewards" "$(q balance "$POT" "$CERT" "$OUTSIDER_ADDR" | jq -r '.balance.claimable')" "0"
check "the funding total is recorded" "$(q edition "$POT" "$CERT" | jq -r '.edition.edition.rewards_funded')" "100000000"
expect_ok "alice creates an edition with no units yet" create-edition "$POT" "$LICENSE" --metadata '{"name":"empty"}' --from alice
EMPTY=$(tx_event edition_id artifact_edition_created)
expect_fail "an edition with no units cannot be funded" "no units" fund-edition "$POT" "$EMPTY" "1000000${BOND_DENOM}" --from carol

echo ""
echo "=== NEITHER burn mode ==="
expect_ok "alice creates a permanent class" create-class "Aurora Founders Wall" "$LICENSE" \
    --flags '{"transferable":true,"burn_authorization":"BURN_AUTHORIZATION_NEITHER"}' --from alice
WALL=$(tx_event class_id artifact_class_created)
expect_ok "alice mints herself a permanent token" mint "$WALL" "$LICENSE" \
    --entries "{\"recipient\":\"$ALICE_ADDR\",\"metadata\":{\"name\":\"brick 1\"}}" --from alice
check "it carries no refundable deposit" "$(q token "$WALL" 1 | jq -r '.token.token.deposit')" "0"
expect_ok "alice mints a permanent token for bob" mint "$WALL" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"brick 2\"}}" --from alice
check "even a member must accept a permanent token" \
    "$(q inbox "$BOB_ADDR" | jq -r --arg c "$WALL" '[.items[]? | select(.class_id == $c)] | length')" "1"
expect_ok "bob accepts" accept-incoming --entries "{\"class_id\":\"$WALL\",\"token_id\":\"2\"}" --from bob
expect_fail "the holder cannot burn it" "holder" burn --refs "{\"class_id\":\"$WALL\",\"token_id\":\"2\"}" --from bob
expect_fail "permanent classes cannot burn on expiry" "flags|expiry" create-class "x" "$LICENSE" \
    --flags '{"transferable":true,"burn_authorization":"BURN_AUTHORIZATION_NEITHER"}' \
    --expiry '{"mode":"EXPIRY_MODE_BURN","term":"60"}' --from alice

finish
