#!/bin/bash
# x/artifact: council hides, query withholding, appeals routed through the
# x/rep moderation-appeal machinery (resolved here by the Commons Operations
# Committee via `rep resolve-gov-action-appeal`), and class hides that cancel
# listings.
# alice is the genesis Operations Committee member (council path).

echo "--- TESTING ARTIFACT: MODERATION ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"

expect_ok "bob creates a class" create-class "Phoenix Gallery" "$LICENSE" \
    --flags '{"transferable":true}' --uri "ipfs://bafygallery" --from bob
CLASS_ID=$(tx_event class_id artifact_class_created)
expect_ok "bob mints three tokens" mint "$CLASS_ID" "$LICENSE" \
    --entries '{"metadata":{"name":"g1","uri":"ipfs://bafyg1"}}' \
    --entries '{"metadata":{"name":"g2","uri":"ipfs://bafyg2"}}' \
    --entries '{"metadata":{"name":"g3","uri":"ipfs://bafyg3"}}' --from bob

echo ""
echo "=== Token hide ==="
expect_fail "a non-sentinel, non-council account cannot hide" "not authorized|sentinel" \
    hide-content token "$CLASS_ID" 1 spam --from artifact_outsider
expect_ok "alice (Ops Committee) hides token 1" \
    hide-content token "$CLASS_ID" 1 spam --from alice
HIDE_ID=$(tx_event hide_id artifact_hidden)
TOKEN=$(q token "$CLASS_ID" 1)
check "token withheld in queries" "$(echo "$TOKEN" | jq -r '.token.withheld')" "true"
check "metadata blanked" "$(echo "$TOKEN" | jq -r '.token.token.metadata.uri // ""')" ""
check "owner still visible" "$(echo "$TOKEN" | jq -r '.token.token.owner')" "$BOB_ADDR"
expect_fail "hidden tokens cannot be listed" "hidden" list "$CLASS_ID" 1 "1000000${BOND_DENOM}" 60 --from bob
expect_fail "only the holder appeals" "may not appeal" appeal-hide "$HIDE_ID" --from carol
BOB_BEFORE_APPEAL=$(balance "$BOB_ADDR")
expect_ok "bob appeals (opens an x/rep ARTIFACT_HIDE appeal)" appeal-hide "$HIDE_ID" --reason "not spam" --from bob
APPEAL_ID=$(tx_event appeal_id gov_action_appealed)
APPEAL_ID=${APPEAL_ID:-0}
check "record appealed" "$(q hide-record "$HIDE_ID" | jq -r '.record.appealed')" "true"
check "record keeps the x/rep appeal id" "$(q hide-record "$HIDE_ID" | jq -r '.record.appeal_id // "0"')" "$APPEAL_ID"
check "x/rep appeal is ARTIFACT_HIDE" "$($BINARY q rep get-gov-action-appeal "$APPEAL_ID" --output json | jq -r '.gov_action_appeal.action_type')" "GOV_ACTION_TYPE_ARTIFACT_HIDE"
check "x/rep charged its 10 SPARK appeal bond (plus gas)" "$((BOB_BEFORE_APPEAL - $(balance "$BOB_ADDR")))" "$((10000000 + 60000))"
# Resolving goes through x/rep; an outsider is not the Ops authority.
RES=$($BINARY tx rep resolve-gov-action-appeal "$APPEAL_ID" overturned "x" --from artifact_outsider \
    --chain-id "$CHAIN_ID" --keyring-backend test --fees 60000"$BOND_DENOM" -y --output json 2>&1)
RES_HASH=$(echo "$RES" | jq -r '.txhash // empty' 2>/dev/null)
if [ -n "$RES_HASH" ] && [ "$(wait_for_tx "$RES_HASH" | jq -r '.code')" = "0" ]; then
    fail "outsider cannot resolve via x/rep (unexpectedly succeeded)"
else
    pass "outsider cannot resolve via x/rep"
fi
RES=$($BINARY tx rep resolve-gov-action-appeal "$APPEAL_ID" overturned "token was fine" --from alice \
    --chain-id "$CHAIN_ID" --keyring-backend test --fees 60000"$BOND_DENOM" -y --output json 2>&1)
check "alice overturns via x/rep" "$(wait_for_tx "$(echo "$RES" | jq -r '.txhash')" | jq -r '.code')" "0"
check "outcome OVERTURNED" "$(q hide-record "$HIDE_ID" | jq -r '.record.outcome')" "HIDE_OUTCOME_OVERTURNED"
check "token visible again" "$(q token "$CLASS_ID" 1 | jq -r '.token.withheld // false')" "false"

echo ""
echo "=== Upheld appeal scrubs metadata ==="
expect_ok "alice hides token 2" hide-content token "$CLASS_ID" 2 spam --from alice
HIDE2=$(tx_event hide_id artifact_hidden)
expect_ok "bob appeals" appeal-hide "$HIDE2" --from bob
APPEAL2=$(tx_event appeal_id gov_action_appealed)
APPEAL2=${APPEAL2:-0}
RES=$($BINARY tx rep resolve-gov-action-appeal "$APPEAL2" upheld "spam confirmed" --from alice \
    --chain-id "$CHAIN_ID" --keyring-backend test --fees 60000"$BOND_DENOM" -y --output json 2>&1)
check "alice upholds via x/rep" "$(wait_for_tx "$(echo "$RES" | jq -r '.txhash')" | jq -r '.code')" "0"
check "outcome UPHELD" "$(q hide-record "$HIDE2" | jq -r '.record.outcome')" "HIDE_OUTCOME_UPHELD"
check "bob still owns token 2" "$(q owner "$CLASS_ID" 2 | jq -r '.owner')" "$BOB_ADDR"
expect_ok "bob can still burn the scrubbed token" burn --refs "{\"class_id\":\"$CLASS_ID\",\"token_id\":\"2\"}" --from bob

echo ""
echo "=== Class hide cancels listings, unhide restores ==="
expect_ok "bob lists token 3" list "$CLASS_ID" 3 "1000000${BOND_DENOM}" 3600 --from bob
expect_ok "alice hides the class" hide-content class "$CLASS_ID" 0 spam --from alice
HIDE3=$(tx_event hide_id artifact_hidden)
check "class withheld" "$(q class "$CLASS_ID" | jq -r '.class.withheld')" "true"
expect_fail "cannot buy in a hidden class" "hidden|not found" buy "$CLASS_ID" 3 "1000000${BOND_DENOM}" 1 --from carol
expect_fail "cannot mint in a hidden class" "hidden" mint "$CLASS_ID" "$LICENSE" --entries '{"metadata":{"name":"x"}}' --from bob
wait_blocks 2
check "listing drained" "$(q listings --class-id "$CLASS_ID" | jq -r '.listings // [] | length')" "0"
check "token 3 unlocked" "$(q token "$CLASS_ID" 3 | jq -r '.token.token.lock // "TOKEN_LOCK_NONE"')" "TOKEN_LOCK_NONE"
expect_ok "alice unhides the class" unhide-content "$HIDE3" --from alice
check "class visible" "$(q class "$CLASS_ID" | jq -r '.class.withheld // false')" "false"
check "hide history for class" "$(q hide-records-by-target "$CLASS_ID" 0 | jq -r '.records | length')" "1"

finish
