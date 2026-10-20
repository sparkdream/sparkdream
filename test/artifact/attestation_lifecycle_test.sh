#!/bin/bash
# x/artifact wave 1: attendance batch issuance, mutual attestations,
# expiring (MARK and BURN) and redeemable tokens, locked royalty splits.

echo "--- TESTING ARTIFACT: ATTESTATIONS, EXPIRY, REDEMPTION, ROYALTY LOCK ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

FEES=60000
PARAMS=$(q params | jq '.params')
DEPOSIT=$(echo "$PARAMS" | jq -r '.token_deposit')
INBOX_FEE=$(echo "$PARAMS" | jq -r '.inbox_fee')
FEE_BPS=$(echo "$PARAMS" | jq -r '.sale_fee_bps')

echo "=== Attendance batch issuance ==="
expect_ok "alice creates a soulbound attendance badge class" create-class "Aurora Summit 2026" "$LICENSE" \
    --flags '{"burn_authorization":"BURN_AUTHORIZATION_HOLDER_OR_ISSUER"}' --from alice
BADGE_ID=$(tx_event class_id artifact_class_created)
expect_ok "outsider opts into the inbox" set-receive-policy inbox --from artifact_outsider
ALICE_B=$(balance "$ALICE_ADDR")
expect_ok "alice issues one badge to bob, carol and the outsider" issue "$BADGE_ID" "$LICENSE" \
    --recipients "$BOB_ADDR" --recipients "$CAROL_ADDR" --recipients "$OUTSIDER_ADDR" \
    --metadata '{"name":"attended aurora summit"}' --from alice
check "three badges issued" "$(tx_event count artifact_issued)" "3"
check "two delivered live" "$(tx_event live artifact_issued)" "2"
check "bob owns badge 1" "$(q owner "$BADGE_ID" 1 | jq -r '.owner')" "$BOB_ADDR"
check "badge carries the shared metadata" "$(q token "$BADGE_ID" 1 | jq -r '.token.token.metadata.name')" "attended aurora summit"
check "outsider's badge waits in the inbox" \
    "$(q inbox "$OUTSIDER_ADDR" | jq -r --arg c "$BADGE_ID" '[.items[]? | select(.class_id == $c)] | length')" "1"
check "alice paid three deposits, one inbox fee and gas" "$((ALICE_B - $(balance "$ALICE_ADDR")))" "$((3 * DEPOSIT + INBOX_FEE + FEES))"
expect_fail "duplicate recipients are refused" "duplicate" issue "$BADGE_ID" "$LICENSE" \
    --recipients "$BOB_ADDR" --recipients "$BOB_ADDR" --metadata '{"name":"x"}' --from alice
expect_fail "bob is not a minter" "not a minter" issue "$BADGE_ID" "$LICENSE" \
    --recipients "$CAROL_ADDR" --metadata '{"name":"x"}' --from bob
expect_ok "outsider accepts the badge" accept-incoming --entries "{\"class_id\":\"$BADGE_ID\",\"token_id\":\"3\"}" --from artifact_outsider
expect_ok "outsider restores the default policy" set-receive-policy unspecified --from artifact_outsider

echo ""
echo "=== Mutual attestations ==="
expect_fail "mutual classes need HOLDER_OR_ISSUER burns" "flags|mutual" create-class "Vouch" "$LICENSE" \
    --flags '{"mutual":true}' --from alice
expect_ok "alice creates a mutual vouching class" create-class "Vouch" "$LICENSE" \
    --flags '{"mutual":true,"burn_authorization":"BURN_AUTHORIZATION_HOLDER_OR_ISSUER"}' --from alice
VOUCH_ID=$(tx_event class_id artifact_class_created)
expect_fail "no self-attestation" "recipient" mint "$VOUCH_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$ALICE_ADDR\",\"metadata\":{\"name\":\"me\"}}" --from alice
expect_ok "alice vouches for bob" mint "$VOUCH_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"trusted collaborator\"}}" --from alice
check "even a member recipient must sign: the mint is pending" \
    "$(q inbox "$BOB_ADDR" | jq -r --arg c "$VOUCH_ID" '[.items[]? | select(.class_id == $c)] | length')" "1"
check "nothing live yet" "$(q supply "$VOUCH_ID" | jq -r '.supply // "0"')" "0"
expect_ok "bob counter-signs by accepting" accept-incoming --entries "{\"class_id\":\"$VOUCH_ID\",\"token_id\":\"1\"}" --from bob
check "attestation exists" "$(q owner "$VOUCH_ID" 1 | jq -r '.owner')" "$BOB_ADDR"
expect_fail "a stranger cannot revoke it" "not authorized|class owner" revoke "$VOUCH_ID" --token-ids 1 --reason "x" --from artifact_outsider
expect_ok "alice withdraws her attestation" revoke "$VOUCH_ID" --token-ids 1 --reason "withdrawn" --from alice
check "attestation gone" "$(q supply "$VOUCH_ID" | jq -r '.supply // "0"')" "0"

echo ""
echo "=== Expiring passes (MARK) and renewal ==="
NOW=$(block_time_unix)
expect_ok "alice creates a 20-second pass renewable for 1 SPARK, paid to carol" create-class "Aurora Pass" "$LICENSE" \
    --flags '{"transferable":true}' --payout-address "$CAROL_ADDR" \
    --expiry "{\"mode\":\"EXPIRY_MODE_MARK\",\"term\":\"20\",\"renewal_price\":{\"denom\":\"$BOND_DENOM\",\"amount\":\"1000000\"}}" --from alice
PASS_ID=$(tx_event class_id artifact_class_created)
expect_ok "alice mints a pass to bob" mint "$PASS_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"season pass\"}}" --from alice
EXP=$(q token "$PASS_ID" 1 | jq -r '.token.token.expires_at')
check "pass expires in the future" "$(( EXP > NOW ))" "1"
check "pass not expired yet" "$(q token "$PASS_ID" 1 | jq -r '.token.expired // false')" "false"
wait_until_time "$EXP"
check "pass marked expired" "$(q token "$PASS_ID" 1 | jq -r '.token.expired // false')" "true"
expect_fail "an expired pass cannot be listed" "expired" list "$PASS_ID" 1 "1000000${BOND_DENOM}" 60 --from bob
expect_fail "renewal price protection" "exceeds" renew "$PASS_ID" 1 --max-price "500000${BOND_DENOM}" --from bob
CAROL_B=$(balance "$CAROL_ADDR")
expect_ok "bob renews for 1 SPARK" renew "$PASS_ID" 1 --max-price "1000000${BOND_DENOM}" --from bob
RENEW_FEE=$((1000000 * FEE_BPS / 10000))
check "payout address received the renewal minus the fee" "$(( $(balance "$CAROL_ADDR") - CAROL_B ))" "$((1000000 - RENEW_FEE))"
check "pass valid again" "$(q token "$PASS_ID" 1 | jq -r '.token.expired // false')" "false"
EXP2=$(q token "$PASS_ID" 1 | jq -r '.token.token.expires_at')
ALICE_B=$(balance "$ALICE_ADDR")
expect_ok "the issuer renews for free" renew "$PASS_ID" 1 --from alice
check "issuer paid only gas" "$((ALICE_B - $(balance "$ALICE_ADDR")))" "$FEES"
check "issuer renewal stacks a full term" "$(q token "$PASS_ID" 1 | jq -r '.token.token.expires_at')" "$((EXP2 + 20))"
expect_fail "outsider cannot renew someone else's pass" "not authorized" renew "$PASS_ID" 1 --max-price "1000000${BOND_DENOM}" --from artifact_outsider

echo ""
echo "=== Expiring passes (BURN) ==="
expect_ok "alice creates a 15-second burning pass" create-class "Day Pass" "$LICENSE" \
    --flags '{"transferable":true}' --expiry '{"mode":"EXPIRY_MODE_BURN","term":"15"}' --from alice
BURN_ID=$(tx_event class_id artifact_class_created)
expect_ok "alice mints a day pass to bob" mint "$BURN_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"day pass\"}}" --from alice
BOB_B=$(balance "$BOB_ADDR")
EXP=$(q token "$BURN_ID" 1 | jq -r '.token.token.expires_at')
wait_until_time "$EXP"
GONE=$(q token "$BURN_ID" 1 > /dev/null 2>&1 && echo present || echo gone)
check "the EndBlocker burned the expired pass" "$GONE" "gone"
check "supply back to zero, burned counted" "$(q supply "$BURN_ID" | jq -r '"\(.supply // "0")/\(.burned // "0")"')" "0/1"
check "deposit refunded to the holder" "$(( $(balance "$BOB_ADDR") - BOB_B ))" "$DEPOSIT"

echo ""
echo "=== Redeemable tickets ==="
expect_ok "alice creates a ticket class with carol as the venue" create-class "Aurora Live" "$LICENSE" \
    --flags '{"transferable":true,"redeemable":true}' --minters "$CAROL_ADDR" --from alice
TICKET_ID=$(tx_event class_id artifact_class_created)
expect_ok "alice mints a ticket to bob" mint "$TICKET_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"row A seat 1\"}}" --from alice
expect_ok "bob lists the ticket" list "$TICKET_ID" 1 "3000000${BOND_DENOM}" 600 --from bob
expect_fail "the holder cannot redeem" "not a minter" redeem "$TICKET_ID" --token-ids 1 --from bob
expect_ok "the venue scans the ticket" redeem "$TICKET_ID" --token-ids 1 --from carol
check "listing cancelled on redemption" "$(q listings --class-id "$TICKET_ID" | jq -r '.listings | length')" "0"
check "redemption recorded" "$(( $(q token "$TICKET_ID" 1 | jq -r '.token.token.redeemed_at // "0"') > 0 ))" "1"
check "ownership unchanged" "$(q owner "$TICKET_ID" 1 | jq -r '.owner')" "$BOB_ADDR"
expect_fail "a spent ticket cannot move" "redeemed" transfer \
    --entries "{\"class_id\":\"$TICKET_ID\",\"token_id\":\"1\",\"recipient\":\"$CAROL_ADDR\"}" --from bob
expect_fail "cannot redeem twice" "already redeemed" redeem "$TICKET_ID" --token-ids 1 --from carol
expect_ok "bob burns the spent ticket for its deposit" burn --refs "{\"class_id\":\"$TICKET_ID\",\"token_id\":\"1\"}" --from bob
expect_fail "plain classes are not redeemable" "not redeemable" redeem "$PASS_ID" --token-ids 1 --from alice

echo ""
echo "=== Locked royalty splits ==="
expect_ok "alice creates a class with a 70/30 split with carol" create-class "Duet" "$LICENSE" \
    --flags '{"transferable":true}' --royalty-bps 500 \
    --royalty-recipients "{\"address\":\"$ALICE_ADDR\",\"weight_bps\":7000}" \
    --royalty-recipients "{\"address\":\"$CAROL_ADDR\",\"weight_bps\":3000}" --from alice
DUET_ID=$(tx_event class_id artifact_class_created)
expect_fail "only the owner locks" "class owner" lock-royalty "$DUET_ID" --from bob
expect_ok "alice locks the split" lock-royalty "$DUET_ID" --from alice
check "royalty_locked" "$(q class "$DUET_ID" | jq -r '.class.class.royalty_locked')" "true"
expect_fail "the split cannot be re-cut" "locked" update-class "$DUET_ID" --update-mask royalty_recipients \
    --royalty-recipients "{\"address\":\"$ALICE_ADDR\",\"weight_bps\":10000}" --from alice
expect_fail "the royalty cannot be lowered either" "locked" update-class "$DUET_ID" --update-mask royalty_bps --royalty-bps 100 --from alice
expect_ok "content stays editable" update-class "$DUET_ID" --update-mask description --description "split locked in" --from alice
expect_fail "the lock is one-way" "locked" lock-royalty "$DUET_ID" --from alice

finish
