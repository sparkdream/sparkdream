#!/bin/bash
# x/artifact wave 2 minting: linear and Dutch price curves, blind mints with
# Merkle reveal (escrow, refund after the deadline) and generative seed
# reveal. Commitments come from cmd/artifactcommit.

echo "--- TESTING ARTIFACT: PRICE CURVES, BLIND AND GENERATIVE MINTS ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/../.." && pwd )"

PARAMS=$(q params | jq '.params')
FEES=60000
DEPOSIT=$(echo "$PARAMS" | jq -r '.token_deposit')
FEE_BPS=$(echo "$PARAMS" | jq -r '.sale_fee_bps')
TOOL_DIR=$(mktemp -d)
(cd "$PROJECT_ROOT" && go build -o "$TOOL_DIR/artifactcommit" ./cmd/artifactcommit) || { echo "  Failed to build artifactcommit"; exit 1; }
COMMIT="$TOOL_DIR/artifactcommit"

echo "=== Linear supply curve ==="
expect_ok "alice opens a curve: 1 SPARK plus 0.5 SPARK per sale, paid to carol" create-class "Curve" "$LICENSE" \
    --flags '{"transferable":true}' --payout-address "$CAROL_ADDR" \
    --mint-policy "{\"public_mint_enabled\":true,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"1000000\"},\"pricing\":\"MINT_PRICING_LINEAR_SUPPLY\",\"price_step\":\"500000\"}" --from alice
CURVE_ID=$(tx_event class_id artifact_class_created)
QUOTE=$(q public-mint-quote "$CURVE_ID" "$OUTSIDER_ADDR" 2)
check "quote totals the first two steps" "$(echo "$QUOTE" | jq -r '.total_price.amount')" "2500000"
check "quote names the dearest unit" "$(echo "$QUOTE" | jq -r '.max_unit_price.amount')" "1500000"
expect_fail "max price must cover the dearest token" "exceeds" public-mint "$CURVE_ID" 2 "1000000${BOND_DENOM}" --from artifact_outsider
CAROL_B=$(balance "$CAROL_ADDR")
expect_ok "outsider buys two along the curve" public-mint "$CURVE_ID" 2 "1500000${BOND_DENOM}" --from artifact_outsider
check "payout received 2.5 SPARK minus the fee" "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((2500000 - 2500000 * FEE_BPS / 10000))"
check "public_minted counts sales" "$(q class "$CURVE_ID" | jq -r '.class.class.public_minted')" "2"
check "the third token costs 2 SPARK" "$(q public-mint-quote "$CURVE_ID" "$BOB_ADDR" 1 | jq -r '.unit_price.amount')" "2000000"
expect_fail "the curve is fixed once minting opened" "fixed|locked" set-mint-policy "$CURVE_ID" \
    --mint-policy "{\"public_mint_enabled\":true,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"1000000\"},\"pricing\":\"MINT_PRICING_LINEAR_SUPPLY\",\"price_step\":\"1000000\"}" --from alice
expect_ok "the owner may still cap per address" set-mint-policy "$CURVE_ID" \
    --mint-policy "{\"public_mint_enabled\":true,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"1000000\"},\"pricing\":\"MINT_PRICING_LINEAR_SUPPLY\",\"price_step\":\"500000\",\"per_address_limit\":5}" --from alice

echo ""
echo "=== Dutch auction ==="
NOW=$(block_time_unix)
expect_ok "alice opens a Dutch auction from 10 to 2 SPARK over 120 seconds" create-class "Dutch" "$LICENSE" \
    --flags '{"transferable":true}' \
    --mint-policy "{\"public_mint_enabled\":true,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"10000000\"},\"pricing\":\"MINT_PRICING_DUTCH\",\"floor_price\":\"2000000\",\"start_time\":\"$NOW\",\"end_time\":\"$((NOW + 120))\"}" --from alice
DUTCH_ID=$(tx_event class_id artifact_class_created)
P1=$(q public-mint-quote "$DUTCH_ID" "$BOB_ADDR" 1 | jq -r '.unit_price.amount')
wait_blocks 3
P2=$(q public-mint-quote "$DUTCH_ID" "$BOB_ADDR" 1 | jq -r '.unit_price.amount')
check "the price falls over time" "$(( P2 < P1 && P2 >= 2000000 ))" "1"
expect_fail "max price below the current price" "exceeds" public-mint "$DUTCH_ID" 1 "1000000${BOND_DENOM}" --from bob
expect_ok "bob buys at the falling price" public-mint "$DUTCH_ID" 1 "${P2}${BOND_DENOM}" --from bob
expect_fail "the auction curve cannot change mid-flight" "fixed|locked" set-mint-policy "$DUTCH_ID" \
    --mint-policy "{\"public_mint_enabled\":true,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"10000000\"},\"pricing\":\"MINT_PRICING_DUTCH\",\"floor_price\":\"1000000\",\"start_time\":\"$NOW\",\"end_time\":\"$((NOW + 120))\"}" --from alice

echo ""
echo "=== Blind mint with Merkle reveal ==="
NEXT_ID=$(( $(q classes --page-limit 1000 | jq '[.classes[].class.id | tonumber] | max') + 1 ))
ENTRIES="$TOOL_DIR/entries.json"
cat > "$ENTRIES" <<EOF
[{"token_id":1,"salt":"0a0b0c0d","metadata":{"name":"Blind #1","attributes":[{"key":"rarity","value":"legendary"}]}},
 {"token_id":2,"salt":"0e0f1011","metadata":{"name":"Blind #2","attributes":[{"key":"rarity","value":"common"}]}}]
EOF
TREE=$("$COMMIT" tree "$NEXT_ID" "$ENTRIES")
ROOT=$(echo "$TREE" | jq -r '.root')
LEAF1=$(echo "$TREE" | jq -c '.leaves[] | select(.token_id == 1)')
SALT1=$(echo "$LEAF1" | jq -r '.salt')
PROOF_FLAGS=""
for p in $(echo "$LEAF1" | jq -r '.proof[]'); do PROOF_FLAGS="$PROOF_FLAGS --proof $p"; done
META1='{"name":"Blind #1","attributes":[{"key":"rarity","value":"legendary"}]}'
DEADLINE=$(( $(block_time_unix) + 40 ))
expect_ok "alice creates a 2-token blind edition at 2 SPARK, paid to carol" create-class "Blind Box" "$LICENSE" \
    --flags '{"transferable":true}' --max-supply 2 --payout-address "$CAROL_ADDR" \
    --mint-policy "{\"public_mint_enabled\":true,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"2000000\"}}" \
    --reveal "{\"kind\":\"REVEAL_KIND_BLIND_METADATA\",\"commitment\":\"$ROOT\",\"deadline\":\"$DEADLINE\"}" --from alice
BLIND_ID=$(tx_event class_id artifact_class_created)
check "committed to the predicted class id" "$BLIND_ID" "$NEXT_ID"
expect_fail "blind classes mint no metadata up front" "reveal" mint "$BLIND_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"peek\"}}" --from alice
CAROL_B=$(balance "$CAROL_ADDR")
expect_ok "bob buys token 1 blind" public-mint "$BLIND_ID" 1 "2000000${BOND_DENOM}" --from bob
check "price escrowed, payout untouched" "$(balance "$CAROL_ADDR")" "$CAROL_B"
TOK=$(q token "$BLIND_ID" 1)
check "token unrevealed" "$(echo "$TOK" | jq -r '.token.token.unrevealed // false')" "true"
check "escrowed price recorded" "$(echo "$TOK" | jq -r '.token.token.paid')" "2000000"
check "display source unrevealed" "$(echo "$TOK" | jq -r '.token.display_source')" "unrevealed"
expect_fail "a wrong salt does not prove into the commitment" "reveal|commitment" reveal "$BLIND_ID" 1 "deadbeef" \
    --metadata "$META1" $PROOF_FLAGS --from alice
expect_fail "wrong metadata does not prove either" "reveal|commitment" reveal "$BLIND_ID" 1 "$SALT1" \
    --metadata '{"name":"Blind #2"}' $PROOF_FLAGS --from alice
expect_ok "alice reveals token 1" reveal "$BLIND_ID" 1 "$SALT1" --metadata "$META1" $PROOF_FLAGS --from alice
check "metadata set" "$(q token "$BLIND_ID" 1 | jq -r '.token.token.metadata.name')" "Blind #1"
check "payout released minus the fee" "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((2000000 - 2000000 * FEE_BPS / 10000))"
expect_fail "cannot reveal twice" "already revealed" reveal "$BLIND_ID" 1 "$SALT1" --metadata "$META1" $PROOF_FLAGS --from alice
expect_ok "outsider buys token 2 blind" public-mint "$BLIND_ID" 1 "2000000${BOND_DENOM}" --from artifact_outsider
wait_until_time "$DEADLINE"
OUT_B=$(balance "$OUTSIDER_ADDR")
expect_ok "past the deadline the holder burns the unrevealed token" burn --refs "{\"class_id\":\"$BLIND_ID\",\"token_id\":\"2\"}" --from artifact_outsider
check "deposit and price refunded" "$(( $(balance "$OUTSIDER_ADDR") - OUT_B + FEES ))" "$((DEPOSIT + 2000000))"

echo ""
echo "=== Generative seed reveal ==="
SEED=$(openssl rand -hex 16)
SEED_COMMIT=$("$COMMIT" seed "$SEED")
DEADLINE=$(( $(block_time_unix) + 60 ))
expect_ok "alice creates a generative class at 1 SPARK, paid to carol" create-class "Generative" "$LICENSE" \
    --flags '{"transferable":true}' --payout-address "$CAROL_ADDR" \
    --mint-policy "{\"public_mint_enabled\":true,\"price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"1000000\"}}" \
    --reveal "{\"kind\":\"REVEAL_KIND_GENERATIVE_SEED\",\"commitment\":\"$SEED_COMMIT\",\"deadline\":\"$DEADLINE\"}" --from alice
GEN_ID=$(tx_event class_id artifact_class_created)
CAROL_B=$(balance "$CAROL_ADDR")
expect_ok "bob buys two generative tokens" public-mint "$GEN_ID" 2 "1000000${BOND_DENOM}" --from bob
check "no seed before the reveal" "$(q token "$GEN_ID" 1 | jq -r '.token.seed // ""')" ""
check "payments escrowed" "$(balance "$CAROL_ADDR")" "$CAROL_B"
expect_fail "a wrong seed is refused" "commitment" reveal-seed "$GEN_ID" "$(openssl rand -hex 16)" --from alice
expect_ok "alice reveals the seed" reveal-seed "$GEN_ID" "$SEED" --from alice
expect_fail "the seed is revealed once" "already revealed" reveal-seed "$GEN_ID" "$SEED" --from alice
wait_blocks 2
SEED1=$(q token "$GEN_ID" 1 | jq -r '.token.seed // ""')
check "token seeds derive from the class seed" "${#SEED1}" "64"
SEED2=$(q token "$GEN_ID" 2 | jq -r '.token.seed // ""')
check "token seeds differ per token" "$([ "$SEED2" != "$SEED1" ] && [ -n "$SEED2" ] && echo yes)" "yes"
check "escrow released by the EndBlocker queue" "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((2 * (1000000 - 1000000 * FEE_BPS / 10000)))"
check "no payment left on the tokens" "$(q token "$GEN_ID" 2 | jq -r '.token.token.paid')" "0"

finish
