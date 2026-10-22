#!/bin/bash
# x/artifact wave 2 presentation: metadata schedules, holder-accepted media
# versions and a query-time renderer evaluated by x/gnovm.

echo "--- TESTING ARTIFACT: SCHEDULES, MEDIA VERSIONS, RENDERERS ---"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/_common.sh"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/../.." && pwd )"

TOOL_DIR=$(mktemp -d)
(cd "$PROJECT_ROOT" && go build -o "$TOOL_DIR/artifactcommit" ./cmd/artifactcommit) || { echo "  Failed to build artifactcommit"; exit 1; }
COMMIT="$TOOL_DIR/artifactcommit"

echo "=== Metadata schedules ==="
expect_ok "alice creates an evolving class" create-class "Evolving" "$LICENSE" --flags '{"transferable":true}' --from alice
EVO_ID=$(tx_event class_id artifact_class_created)
NOW=$(block_time_unix)
expect_fail "schedule stages must lie in the future" "schedule" mint "$EVO_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"egg\"},\"schedule\":[{\"from_time\":\"$((NOW - 100))\",\"metadata\":{\"name\":\"phoenix\"}}]}" --from alice
STAGE=$((NOW + 20))
expect_ok "alice mints an egg that becomes a phoenix" mint "$EVO_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"egg\"},\"schedule\":[{\"from_time\":\"$STAGE\",\"metadata\":{\"name\":\"phoenix\",\"uri\":\"ipfs://bafyphoenix\"}}]}" --from alice
TOK=$(q token "$EVO_ID" 1)
check "shows the egg now" "$(echo "$TOK" | jq -r '.token.displayed_metadata.name')" "egg"
check "display source stored" "$(echo "$TOK" | jq -r '.token.display_source')" "stored"
check "future media already labelled" "$(echo "$TOK" | jq -r '.token.token.media_flags')" "8"
wait_until_time "$STAGE"
TOK=$(q token "$EVO_ID" 1)
check "shows the phoenix after the stage time" "$(echo "$TOK" | jq -r '.token.displayed_metadata.name')" "phoenix"
check "display source schedule" "$(echo "$TOK" | jq -r '.token.display_source')" "schedule"
check "stored metadata never changed" "$(echo "$TOK" | jq -r '.token.token.metadata.name')" "egg"
check "list queries follow the schedule" "$(q tokens "$EVO_ID" | jq -r '.tokens[0].displayed_metadata.name')" "phoenix"

echo ""
echo "=== Media versions ==="
expect_fail "only the class owner proposes media" "class owner" propose-media "$EVO_ID" 1 --metadata '{"name":"remaster"}' --from bob
expect_ok "alice proposes a remaster" propose-media "$EVO_ID" 1 --metadata '{"name":"remaster","uri":"ipfs://bafyremaster"}' --from alice
PID=$(tx_event proposal_id artifact_media_proposed)
check "proposal listed" "$(q media-proposals "$EVO_ID" 1 | jq -r '.proposals | length')" "1"
expect_fail "only the holder accepts" "token owner" accept-media "$EVO_ID" 1 "$PID" --display --from alice
expect_ok "bob accepts and displays the remaster" accept-media "$EVO_ID" 1 "$PID" --display --from bob
TOK=$(q token "$EVO_ID" 1)
check "holder's choice shown" "$(echo "$TOK" | jq -r '.token.displayed_metadata.name')" "remaster"
check "display source media_version" "$(echo "$TOK" | jq -r '.token.display_source')" "media_version"
check "proposal consumed" "$(q media-proposals "$EVO_ID" 1 | jq -r '.proposals | length')" "0"
expect_fail "no such version" "media version" set-displayed-media "$EVO_ID" 1 2 --from bob
expect_ok "bob switches back to the schedule" set-displayed-media "$EVO_ID" 1 0 --from bob
check "schedule shown again" "$(q token "$EVO_ID" 1 | jq -r '.token.display_source')" "schedule"
expect_ok "alice proposes another version" propose-media "$EVO_ID" 1 --metadata '{"name":"animated"}' --from alice
PID2=$(tx_event proposal_id artifact_media_proposed)
expect_ok "bob rejects it" reject-media "$EVO_ID" 1 "$PID2" --from bob
check "rejected proposal gone" "$(q media-proposals "$EVO_ID" 1 | jq -r '.proposals | length')" "0"

echo ""
echo "=== Renderer (x/gnovm) ==="
PKG_DIR="$SCRIPT_DIR/contracts/aurora"
PKG_PATH=$(grep '^module' "$PKG_DIR/gnomod.toml" | sed 's/.*"\(.*\)".*/\1/')
SRC_HASH=$("$COMMIT" source-hash "$PKG_DIR")
echo "  Deploying $PKG_PATH (source hash $SRC_HASH)..."
RES=$($BINARY tx gnovm add-package "$PKG_DIR" --send 1000${BOND_DENOM} --max-deposit 10000${BOND_DENOM} \
    --from alice --gas 5000000 --fees 50000${BOND_DENOM} -y --keyring-backend test --chain-id "$CHAIN_ID" --output json 2>&1)
HASH=$(echo "$RES" | jq -r '.txhash // empty' 2>/dev/null)
if [ -n "$HASH" ] && [ "$(wait_for_tx "$HASH" | jq -r '.code')" = "0" ]; then
    pass "renderer package deployed"
else
    # A re-run against saved state finds the package already deployed.
    if $BINARY query gnovm info "$PKG_PATH" > /dev/null 2>&1; then
        pass "renderer package already deployed"
    else
        fail "renderer package deploy -- $(echo "$RES" | head -c 300)"
    fi
fi
ZERO_HASH=$(printf '0%.0s' $(seq 1 64))
expect_fail "the source hash must match the stored package" "renderer" create-class "Aurora Render" "$LICENSE" \
    --flags '{"transferable":true}' --renderer "{\"pkg_path\":\"$PKG_PATH\",\"source_hash\":\"$ZERO_HASH\"}" --from alice
expect_fail "an undeployed package is refused" "renderer" create-class "Aurora Render" "$LICENSE" \
    --flags '{"transferable":true}' --renderer "{\"pkg_path\":\"gno.land/p/demo/nothere\",\"source_hash\":\"$SRC_HASH\"}" --from alice
expect_ok "alice creates a rendered class" create-class "Aurora Render" "$LICENSE" \
    --flags '{"transferable":true}' --renderer "{\"pkg_path\":\"$PKG_PATH\",\"source_hash\":\"$SRC_HASH\"}" --from alice
RENDER_ID=$(tx_event class_id artifact_class_created)
expect_ok "alice mints a rendered token to bob" mint "$RENDER_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"stored fallback\"}}" --from alice
TOK=$(q token "$RENDER_ID" 1)
check "render error empty" "$(echo "$TOK" | jq -r '.token.render_error // ""')" ""
check "display source renderer" "$(echo "$TOK" | jq -r '.token.display_source')" "renderer"
check "renderer named the token" "$(echo "$TOK" | jq -r '.token.displayed_metadata.name' | cut -d' ' -f1-2)" "Aurora #1"
check "renderer read the block height" "$(echo "$TOK" | jq -r '.token.displayed_metadata.description' | cut -d' ' -f1-3)" "rendered at height"
check "stored metadata untouched" "$(echo "$TOK" | jq -r '.token.token.metadata.name')" "stored fallback"
check "list queries show stored metadata" "$(q tokens "$RENDER_ID" | jq -r '.tokens[0].display_source')" "stored"
check "class carries the renderer pin" "$(q class "$RENDER_ID" | jq -r '.class.class.renderer.source_hash')" "$SRC_HASH"

echo ""
echo "=== State renderer (state_version 2: season and nested contents) ==="
PKG_DIR2="$SCRIPT_DIR/contracts/zenith"
PKG_PATH2=$(grep '^module' "$PKG_DIR2/gnomod.toml" | sed 's/.*"\(.*\)".*/\1/')
SRC_HASH2=$("$COMMIT" source-hash "$PKG_DIR2")
RES=$($BINARY tx gnovm add-package "$PKG_DIR2" --send 1000${BOND_DENOM} --max-deposit 10000${BOND_DENOM} \
    --from alice --gas 5000000 --fees 50000${BOND_DENOM} -y --keyring-backend test --chain-id "$CHAIN_ID" --output json 2>&1)
HASH=$(echo "$RES" | jq -r '.txhash // empty' 2>/dev/null)
if [ -n "$HASH" ] && [ "$(wait_for_tx "$HASH" | jq -r '.code')" = "0" ]; then
    pass "state renderer package deployed"
elif $BINARY query gnovm info "$PKG_PATH2" > /dev/null 2>&1; then
    pass "state renderer package already deployed"
else
    fail "state renderer package deploy -- $(echo "$RES" | head -c 300)"
fi
expect_fail "a RenderState package needs a state version" "renderer" create-class "Zenith" "$LICENSE" \
    --flags '{"transferable":true}' --renderer "{\"pkg_path\":\"$PKG_PATH2\",\"source_hash\":\"$SRC_HASH2\"}" --from alice
expect_ok "alice creates a state-rendered class" create-class "Zenith" "$LICENSE" \
    --flags '{"transferable":true}' --renderer "{\"pkg_path\":\"$PKG_PATH2\",\"source_hash\":\"$SRC_HASH2\",\"state_version\":2}" --from alice
ZENITH_ID=$(tx_event class_id artifact_class_created)
expect_ok "alice mints a zenith and a gem to bob" mint "$ZENITH_ID" "$LICENSE" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"zenith\"}}" \
    --entries "{\"recipient\":\"$BOB_ADDR\",\"metadata\":{\"name\":\"gem\"}}" --from alice
expect_ok "bob sets the gem in the zenith" nest --child "{\"class_id\":\"$ZENITH_ID\",\"token_id\":\"2\"}" \
    --parent "{\"class_id\":\"$ZENITH_ID\",\"token_id\":\"1\"}" --from bob
TOK=$(q token "$ZENITH_ID" 1)
SEASON=$($BINARY query season current-season --output json 2>/dev/null | jq -r '.season.number // .number // empty')
check "the state renderer ran" "$(echo "$TOK" | jq -r '.token.display_source')" "renderer"
check "it read state version 2" "$(echo "$TOK" | jq -r '.token.displayed_metadata.attributes[0].value')" "2"
check "it read the nested contents" "$(echo "$TOK" | jq -r '.token.displayed_metadata.description' | sed 's/.*holding //')" "1"
if [ -n "$SEASON" ]; then
    check "it read the current season" "$(echo "$TOK" | jq -r '.token.displayed_metadata.description' | sed 's/season \([0-9]*\),.*/\1/')" "$SEASON"
fi

finish
