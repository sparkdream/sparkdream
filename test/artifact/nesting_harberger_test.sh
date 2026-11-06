#!/bin/bash
# x/artifact wave 4: nesting (trees that move as one, contents-hash pinned
# sales) and Harberger tokens (self-assessed prices, forced buys, grace and
# foreclosure, tax paid through an x/session RecurringPull).

echo "--- TESTING ARTIFACT: NESTING AND HARBERGER TOKENS ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

FEES=60000
PARAMS=$(q params | jq '.params')
FEE_BPS=$(echo "$PARAMS" | jq -r '.sale_fee_bps')

echo "=== Nesting ==="
expect_ok "alice creates a character class" create-class "Aurora Heroes" "$LICENSE" --flags '{"transferable":true}' --from alice
HEROES=$(tx_event class_id artifact_class_created)
expect_ok "alice creates a gear class" create-class "Aurora Gear" "$LICENSE" --flags '{"transferable":true}' --from alice
GEAR=$(tx_event class_id artifact_class_created)
expect_ok "alice mints a hero to bob" mint "$HEROES" "$LICENSE" --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"zenith\"}}" --from alice
expect_ok "alice mints a sword and a gem to bob" mint "$GEAR" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"sword\"}}" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"gem\"}}" --from alice
HERO="{\"class_id\":\"$HEROES\",\"token_id\":\"1\"}"
SWORD="{\"class_id\":\"$GEAR\",\"token_id\":\"1\"}"
GEM="{\"class_id\":\"$GEAR\",\"token_id\":\"2\"}"
expect_ok "bob puts the sword in the hero" nest --child "$SWORD" --parent "$HERO" --from bob
expect_ok "bob sets the gem in the sword" nest --child "$GEM" --parent "$SWORD" --from bob
expect_fail "a token cannot hold its own ancestor" "cycle" nest --child "$HERO" --parent "$GEM" --from bob
check "the hero holds two tokens" "$(q children "$HEROES" 1 | jq -r '.descendants // 0')" "2"
check "the sword is NESTED" "$(q token "$GEAR" 1 | jq -r '.token.token.lock')" "TOKEN_LOCK_NESTED"
expect_fail "a nested token cannot move on its own" "locked" transfer \
    --entries "{\"class_id\":\"$GEAR\",\"token_id\":\"1\",\"recipient\":\"$CAROL_ADDR\"}" --from bob
expect_ok "bob lists the hero" list "$HEROES" 1 "10000000${BOND_DENOM}" 600 --from bob
LNONCE=$(q listing "$HEROES" 1 | jq -r '.listing.nonce')
expect_fail "a listed tree is frozen" "locked" detach --child "$SWORD" --from bob
expect_fail "a sale without the contents pin fails closed" "contents" buy "$HEROES" 1 "10000000${BOND_DENOM}" "$LNONCE" --from carol
CHASH=$(q children "$HEROES" 1 | jq -r '.contents_hash')
expect_ok "carol buys the hero with its contents pinned" buy "$HEROES" 1 "10000000${BOND_DENOM}" "$LNONCE" \
    --expected-contents-hash "$CHASH" --from carol
check "the sword moved with the hero" "$(q owner "$GEAR" 1 | jq -r '.owner')" "$CAROL_ADDR"
check "the gem moved with the hero" "$(q owner "$GEAR" 2 | jq -r '.owner')" "$CAROL_ADDR"
expect_ok "carol detaches the sword (with its gem)" detach --child "$SWORD" --from carol
check "the sword is free" "$(q token "$GEAR" 1 | jq -r '.token.token.lock // "TOKEN_LOCK_NONE"')" "TOKEN_LOCK_NONE"
check "the gem stays in the sword" "$(q token "$GEAR" 2 | jq -r '.token.token.parent.token_id')" "1"

echo ""
echo "=== Harberger: forced buys, grace and foreclosure ==="
expect_ok "alice creates a Harberger class sold at 10 SPARK (10 s tax period, 20 s foreclosure)" create-class "Aurora Plots" "$LICENSE" \
    --flags '{"transferable":true}' \
    --harberger '{"tax_bps_per_year":1000,"tax_period":"10","foreclosure_duration":"20"}' \
    --mint-policy "{\"public_mint_enabled\":true,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"10000000\"}}" --from alice
PLOTS=$(tx_event class_id artifact_class_created)
expect_ok "the outsider public-mints a plot" public-mint "$PLOTS" 1 "10000000${BOND_DENOM}" --from artifact_outsider
check "the plot is assessed at its mint price" "$(q harberger-quote "$PLOTS" 1 | jq -r '.current_price')" "10000000"
expect_fail "Harberger tokens are never listed" "always for sale" list "$PLOTS" 1 "5000000${BOND_DENOM}" 60 --from artifact_outsider
expect_fail "max_price protects the buyer" "exceeds" force-buy "$PLOTS" 1 "9000000${BOND_DENOM}" --from carol
OUT_B=$(balance "$OUTSIDER_ADDR")
expect_ok "carol force-buys the plot at its assessed price" force-buy "$PLOTS" 1 "10000000${BOND_DENOM}" --from carol
FEE=$((10000000 * FEE_BPS / 10000))
check "the holder received the price minus the fee" "$(( $(balance "$OUTSIDER_ADDR") - OUT_B ))" "$((10000000 - FEE))"
check "carol owns the plot" "$(q owner "$PLOTS" 1 | jq -r '.owner')" "$CAROL_ADDR"
GRACE=$(q harberger-quote "$PLOTS" 1 | jq -r '.state.grace_until')
check "carol is not foreclosed inside her grace period" "$(q harberger-quote "$PLOTS" 1 | jq -r '.foreclosed // false')" "false"
wait_until_time "$((GRACE + 10))"
Q=$(q harberger-quote "$PLOTS" 1)
CUR=$(echo "$Q" | jq -r '.current_price')
check "without a tax grant carol is foreclosed" "$(echo "$Q" | jq -r '.foreclosed // false')" "true"
check "the price is falling" "$(( CUR < 10000000 && CUR > 0 ))" "1"
expect_ok "bob force-buys at the falling price" force-buy "$PLOTS" 1 "${CUR}${BOND_DENOM}" --from bob
check "bob owns the plot" "$(q owner "$PLOTS" 1 | jq -r '.owner')" "$BOB_ADDR"
check "bob starts a fresh grace period" "$(q harberger-quote "$PLOTS" 1 | jq -r '.foreclosed // false')" "false"

echo ""
echo "=== Harberger: tax through an x/session RecurringPull ==="
expect_ok "alice creates a daily-tax Harberger class" create-class "Aurora Estates" "$LICENSE" \
    --flags '{"transferable":true}' --harberger '{"tax_bps_per_year":3650,"tax_period":"86400","foreclosure_duration":"86400"}' --from alice
ESTATES=$(tx_event class_id artifact_class_created)
expect_ok "alice mints an estate to bob" mint "$ESTATES" "$LICENSE" --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"north hill\"}}" --from alice
expect_fail "an unpriced estate is not for sale in its grace period" "not for sale" force-buy "$ESTATES" 1 "1000000${BOND_DENOM}" --from carol
expect_fail "a positive price needs a tax grant" "grant" set-harberger-price "$ESTATES" 1 100000000 --from bob

# 36.5%/yr of 100 SPARK for one day = 0.1 SPARK. The grant's per-day cap
# must leave room for two pulls (a pull delayed into the next one's day).
MODULE_ADDR=$($BINARY q auth module-account artifact --output json | jq -r '.account.value.address // .account.base_account.address // .account.address')
create_tax_grant() {
    local amount=$1 res hash
    res=$($BINARY tx session create-recurring-pull "$MODULE_ADDR" "${amount}${BOND_DENOM}" 86400 +720h \
        --max-per-epoch "$((amount * 2))" \
        --grant-note "harberger tax" --from bob --chain-id "$CHAIN_ID" --keyring-backend test \
        --gas 500000 --fees ${FEES}${BOND_DENOM} -y --output json 2>&1)
    hash=$(echo "$res" | jq -r '.txhash // empty')
    [ -n "$hash" ] || { echo ""; return 1; }
    wait_for_tx "$hash" | jq -r '[.events[] | select(.type=="grant_created") | .attributes[] | select(.key=="id") | .value] | first // empty'
}
SMALL=$(create_tax_grant 50000)
check "bob created a too-small tax grant" "$([ -n "$SMALL" ] && echo yes || echo no)" "yes"
expect_fail "the grant must cover the tax" "at least" set-harberger-price "$ESTATES" 1 100000000 --grant-id "$SMALL" --from bob
GRANT=$(create_tax_grant 100000)
check "bob created a tax grant" "$([ -n "$GRANT" ] && echo yes || echo no)" "yes"
expect_ok "bob assesses his estate at 100 SPARK" set-harberger-price "$ESTATES" 1 100000000 --grant-id "$GRANT" --from bob
Q=$(q harberger-quote "$ESTATES" 1)
check "the quote shows the daily tax" "$(echo "$Q" | jq -r '.tax_per_period')" "100000"
# The time until the grant's first pull is charged at the assessed price up
# front (ART-201), rounded up. The grant was created a few blocks before the
# price, so that gap is a few seconds short of a full period: expect the
# per-period tax minus at most two minutes' worth.
PAID_UP_FRONT=$(echo "$Q" | jq -r '.state.tax_paid')
check "the gap to the first pull was paid up front" \
    "$([ "$PAID_UP_FRONT" -le 100000 ] && [ "$PAID_UP_FRONT" -ge 99800 ] && echo "in range" || echo "$PAID_UP_FRONT")" "in range"
check "the grant is recorded" "$(echo "$Q" | jq -r '.state.grant_id')" "$GRANT"
check "the first pull is scheduled" "$(( $(echo "$Q" | jq -r '.state.next_due') > $(block_time_unix) ))" "1"
check "the estate is for sale at its assessment" "$(echo "$Q" | jq -r '.for_sale // false')" "true"

finish
