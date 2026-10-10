#!/bin/bash
# x/artifact: class creation gates, minting, supply ratchets, freezes,
# minters and the two-step ownership handover.

echo "--- TESTING ARTIFACT: CLASS LIFECYCLE ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

echo ""
echo "=== Creation gates ==="
expect_fail "non-member cannot create a class" "trust|member" \
    create-class "Outsider Works" "$LICENSE" --from artifact_outsider
expect_fail "license must be CC0-1.0" "license" \
    create-class "Phoenix" "MIT" --from alice
expect_fail "issuer-burnable classes must be soulbound" "non-transferable|flags" \
    create-class "Phoenix" "$LICENSE" --flags '{"transferable":true,"burn_authorization":"BURN_AUTHORIZATION_HOLDER_OR_ISSUER"}' --from alice
expect_fail "royalty weights must sum to 10000" "royalty split" \
    create-class "Phoenix" "$LICENSE" --flags '{"transferable":true}' --royalty-bps 500 \
    --royalty-recipients "{\"address\":\"$CAROL_ADDR\",\"weight_bps\":5000}" --from alice
expect_fail "data URIs are rejected" "data URI" \
    create-class "Phoenix" "$LICENSE" --description "see data:image/png;base64,AAAA" --from alice
expect_fail "http URIs are rejected" "scheme|not allowed" \
    create-class "Phoenix" "$LICENSE" --uri "http://phoenix.example/cover.png" --from alice

echo ""
echo "=== Create a class ==="
ALICE_BEFORE=$(balance "$ALICE_ADDR")
expect_ok "alice creates a transferable, mutable class" \
    create-class "Phoenix Editions" "$LICENSE" --symbol PHX --uri "ipfs://bafyphoenixcover" \
    --flags '{"transferable":true,"token_metadata_mutable":true}' --royalty-bps 500 \
    --royalty-recipients "{\"address\":\"$CAROL_ADDR\",\"weight_bps\":10000}" --from alice
CLASS_ID=$(tx_event class_id artifact_class_created)
check "class id assigned" "$([ -n "$CLASS_ID" ] && echo yes)" "yes"
CLASS=$(q class "$CLASS_ID")
check "owner is alice" "$(echo "$CLASS" | jq -r '.class.class.owner')" "$ALICE_ADDR"
check "royalty recipient is carol" "$(echo "$CLASS" | jq -r '.class.class.royalty_recipients | map(.address) | join(",")')" "$CAROL_ADDR"
check "media flags EXTERNAL_URI" "$(echo "$CLASS" | jq -r '.class.class.media_flags // 0')" "8"

echo ""
echo "=== Mint ==="
DEPOSIT=$(q params | jq -r '.params.token_deposit')
expect_ok "alice mints two tokens to herself" \
    mint "$CLASS_ID" "$LICENSE" \
    --entries '{"metadata":{"name":"aurora-1","uri":"ipfs://bafyaurora1"}}' \
    --entries '{"metadata":{"name":"aurora-2","uri":"ipfs://bafyaurora2","attributes":[{"key":"edition","value":"2"}]}}' \
    --from alice
check "token 1 owned by alice" "$(q owner "$CLASS_ID" 1 | jq -r '.owner')" "$ALICE_ADDR"
check "token 2 attribute stored" "$(q token "$CLASS_ID" 2 | jq -r '.token.token.metadata.attributes[0].value')" "2"
check "supply is 2" "$(q supply "$CLASS_ID" | jq -r '.supply // "0"')" "2"
expect_ok "alice mints to bob (member sender delivers directly)" \
    mint "$CLASS_ID" "$LICENSE" --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"aurora-3\"}}" --from alice
check "token 3 owned by bob" "$(q owner "$CLASS_ID" 3 | jq -r '.owner')" "$BOB_ADDR"
expect_fail "bob is not a minter" "minter" \
    mint "$CLASS_ID" "$LICENSE" --entries '{"metadata":{"name":"x"}}' --from bob
expect_fail "mint requires the license affirmation" "license" \
    mint "$CLASS_ID" "MIT" --entries '{"metadata":{"name":"x"}}' --from alice

echo ""
echo "=== Minters ==="
expect_ok "alice adds bob as a minter" set-minters "$CLASS_ID" --add "$BOB_ADDR" --from alice
expect_ok "bob can now mint" mint "$CLASS_ID" "$LICENSE" --entries '{"metadata":{"name":"aurora-4"}}' --from bob
expect_ok "alice removes bob" set-minters "$CLASS_ID" --remove "$BOB_ADDR" --from alice
expect_fail "bob can no longer mint" "minter" \
    mint "$CLASS_ID" "$LICENSE" --entries '{"metadata":{"name":"x"}}' --from bob

echo ""
echo "=== Supply ratchet ==="
expect_ok "cap an unlimited class at 6" set-max-supply "$CLASS_ID" 6 --from alice
expect_fail "cannot raise the cap" "lowered|cap" set-max-supply "$CLASS_ID" 7 --from alice
expect_fail "cannot cap below issued" "issued|exceeded" set-max-supply "$CLASS_ID" 3 --from alice
expect_ok "burn token 1" burn --refs "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"1\"}" --from alice
expect_ok "mint 2 more (issued 6 of 6)" mint "$CLASS_ID" "$LICENSE" \
    --entries '{"metadata":{"name":"a5"}}' --entries '{"metadata":{"name":"a6"}}' --from alice
expect_fail "burned tokens still count toward the cap" "max supply" \
    mint "$CLASS_ID" "$LICENSE" --entries '{"metadata":{"name":"a7"}}' --from alice
SUPPLY=$(q supply "$CLASS_ID")
check "supply 5" "$(echo "$SUPPLY" | jq -r '.supply // "0"')" "5"
check "burned 1" "$(echo "$SUPPLY" | jq -r '.burned // "0"')" "1"

echo ""
echo "=== Metadata edits and freezes ==="
expect_ok "class owner edits token 2" update-token "$CLASS_ID" 2 \
    --metadata '{"name":"aurora-2-v2","uri":"ipfs://bafyaurora2v2"}' --from alice
check "token 2 renamed" "$(q token "$CLASS_ID" 2 | jq -r '.token.token.metadata.name')" "aurora-2-v2"
expect_ok "bob (holder) freezes token 3" freeze-token-metadata --refs "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"3\"}" --from bob
expect_fail "issuer cannot edit a frozen token" "frozen" \
    update-token "$CLASS_ID" 3 --metadata '{"name":"swap"}' --from alice
expect_ok "royalty can be lowered" update-class "$CLASS_ID" --royalty-bps 300 --update-mask royalty_bps --from alice
expect_fail "royalty cannot be raised" "royalty" update-class "$CLASS_ID" --royalty-bps 400 --update-mask royalty_bps --from alice
expect_ok "freeze class metadata" freeze-class-metadata "$CLASS_ID" --from alice
expect_fail "class name frozen" "frozen" update-class "$CLASS_ID" --name "Renamed" --update-mask name --from alice
expect_fail "token metadata frozen with the class" "frozen" \
    update-token "$CLASS_ID" 2 --metadata '{"name":"swap"}' --from alice

echo ""
echo "=== Ownership handover ==="
expect_ok "alice adds carol as minter before handover" set-minters "$CLASS_ID" --add "$CAROL_ADDR" --from alice
expect_ok "alice proposes bob" propose-class-owner "$CLASS_ID" "$BOB_ADDR" --from alice
expect_fail "carol cannot accept" "proposal" accept-class-owner "$CLASS_ID" --from carol
expect_ok "bob accepts" accept-class-owner "$CLASS_ID" --from bob
CLASS=$(q class "$CLASS_ID")
check "owner is bob" "$(echo "$CLASS" | jq -r '.class.class.owner')" "$BOB_ADDR"
check "creator stays alice" "$(echo "$CLASS" | jq -r '.class.class.creator')" "$ALICE_ADDR"
check "minters cleared" "$(echo "$CLASS" | jq -r '.class.class.minters // [] | length')" "0"
check "payout reset to bob" "$(echo "$CLASS" | jq -r '.class.class.payout_address')" "$BOB_ADDR"
check "royalty recipient reset to bob" "$(echo "$CLASS" | jq -r '.class.class.royalty_recipients | map(.address) | join(",")')" "$BOB_ADDR"

echo ""
echo "=== Close minting ==="
expect_ok "bob closes minting" close-minting "$CLASS_ID" --from bob
check "minting closed" "$(q supply "$CLASS_ID" | jq -r '.minting_closed // false')" "true"

finish
