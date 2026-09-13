#!/bin/bash
# ============================================================================
# REST GATEWAY E2E: EVERY MODULE'S LCD ROUTES ARE SERVED
# ============================================================================
# The REST gateway (LCD, app.toml [api]) is rebuilt at boot by
# app/gateway_fix.go, which registers routes by iterating the module manager
# and then swaps the resulting mux into the SDK's API server.
#
# Two distinct silent failures live here, and this script separates them:
#
#   501 Not Implemented -- the route never reached the mux. That is what
#       happened when the registration list was hand-maintained: federation,
#       guardian, identity and service were missing from it, and ~66
#       endpoints answered 501 with no build or boot error.
#
#   500 Internal Error  -- the route IS registered, but the handler panicked
#       and gateway_fix.go's safety net swallowed it into a generic body.
#       This is how a proto v2 reflection panic on a gogoproto custom type
#       (math.Int, math.LegacyDec) presents -- the exact failure the whole
#       file exists to prevent. Nothing else reports it: the node logs one
#       line to stderr and the HTTP response looks like an ordinary error.
#
# An earlier revision of this script treated any non-501 status as success,
# which passed a run in which 17 endpoints were panicking. Endpoints are now
# asserted to return 200, and the node log is checked for panics raised by
# this script's own requests.
#
# app/gateway_routes_test.go covers the routing table in-process. This
# script covers what it cannot: the live serving path end to end.
#
# Checks:
#   1. One declared path per module returns 200.
#   2. No gateway handler panic was logged while doing so.
#   3. Block endpoints (pre-intercepted ahead of the mux, because the
#      gogogateway marshaler cannot render their *time.Time fields) return
#      a parseable header time.
#   4. Responses use snake_case field names and keep zero-valued fields,
#      which is what the mux's marshaler options are for.
#
# Prerequisites: a running chain with [api] enable = true. Skips with a
# warning if REST is disabled.
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

FAIL_COUNT=0

echo "=================================================="
echo "TEST: REST gateway routes"
echo "=================================================="

# ----------------------------------------------------------------------------
# Resolve the API address from the node's own app.toml rather than assuming
# 1317 -- run_parallel.sh gives each suite its own offset port.
# ----------------------------------------------------------------------------
NODE_HOME="${CHAIN_HOME:-$HOME/.sparkdream}"
APP_TOML="$NODE_HOME/config/app.toml"

if [ ! -f "$APP_TOML" ]; then
    echo "[WARN] no app.toml at $APP_TOML; cannot locate the REST endpoint"
    echo "[WARN] SKIPPED"
    exit 0
fi

API_SECTION=$(sed -n '/^\[api\]/,/^\[[a-z]/p' "$APP_TOML")
API_ENABLED=$(echo "$API_SECTION" | grep -E '^[[:space:]]*enable[[:space:]]*=' | head -1 | sed 's/.*=[[:space:]]*//' | tr -d '"')
API_ADDR=$(echo "$API_SECTION" | grep -E '^[[:space:]]*address[[:space:]]*=' | head -1 | sed 's|.*tcp://||' | tr -d '"')

if [ "$API_ENABLED" != "true" ]; then
    echo "[WARN] REST API disabled in $APP_TOML; nothing to test"
    echo "[WARN] SKIPPED"
    exit 0
fi

# 0.0.0.0 is a bind address, not a dial address.
LCD="http://${API_ADDR/0.0.0.0/127.0.0.1}"
echo "REST endpoint: $LCD"

# ----------------------------------------------------------------------------
# Locate the node log so handler panics can be attributed to our requests.
# The three runners each put it somewhere different; stale files from a
# previous run are harmless because only the DELTA across our probes counts.
# ----------------------------------------------------------------------------
NODE_LOGS=()
for candidate in "$NODE_HOME/chain.log" /tmp/sparkdreamd-e2e.log /tmp/sparkdream-start.log; do
    [ -f "$candidate" ] && NODE_LOGS+=("$candidate")
done

PANIC_MARKER="gateway_fix: handler panic"

count_panics() {
    local total=0 n
    for lg in "${NODE_LOGS[@]}"; do
        # grep -c prints 0 AND exits 1 when there are no matches, so the
        # count must come from stdout and the exit status be discarded
        # separately -- `|| echo 0` would append a second line.
        n=$(grep -c "$PANIC_MARKER" "$lg" 2>/dev/null) || n=0
        [ -n "$n" ] || n=0
        total=$((total + n))
    done
    echo "$total"
}

if [ "${#NODE_LOGS[@]}" -eq 0 ]; then
    echo "[WARN] no node log found; handler-panic detection disabled for this run"
else
    echo "node log(s): ${NODE_LOGS[*]}"
fi

PANICS_BEFORE=$(count_panics)

# ----------------------------------------------------------------------------
# Wait for the API server to answer. Poll rather than sleep -- the API server
# starts after CometBFT RPC, and block rate drifts over a long suite run.
# ----------------------------------------------------------------------------
READY=0
for i in $(seq 1 60); do
    # curl prints its %{http_code} (000) on failure and exits non-zero, so the
    # status must come from stdout and the fallback from the exit code -- an
    # `|| echo 000` here appends a second line and prints as "000000".
    CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 \
        "$LCD/cosmos/base/tendermint/v1beta1/node_info" 2>/dev/null) || CODE=000
    if [ "$CODE" = "200" ]; then
        READY=1
        echo "[ OK ] REST API responding after ${i}s"
        break
    fi
    sleep 1
done

if [ "$READY" != "1" ]; then
    echo "[FAIL] REST API at $LCD never answered (last status: $CODE)"
    echo ""
    echo "       app.toml says [api] enable = true, so the server should be up."
    echo "       A common cause is the node's gRPC port being taken: the API"
    echo "       server and the gRPC server share an errgroup, so a gRPC bind"
    echo "       failure tears the API server down while consensus keeps"
    echo "       producing blocks -- the node looks healthy and REST is dead."
    GRPC_ADDR=$(sed -n '/^\[grpc\]/,/^\[[a-z]/p' "$APP_TOML" \
        | grep -E '^[[:space:]]*address[[:space:]]*=' | head -1 \
        | sed 's/.*=[[:space:]]*//' | tr -d '"')
    echo "       Check the node log for a gRPC bind error on ${GRPC_ADDR:-the gRPC address}."
    echo "       On WSL2 a Windows-side process can hold the port invisibly:"
    echo "       'ss -ltn' shows nothing while the bind still fails. Test with"
    echo "       python3 -c \"import socket;socket.socket().bind(('127.0.0.1',PORT))\""
    exit 1
fi

# ----------------------------------------------------------------------------
# 1. One declared path per module. Each must return 200.
#
# Kept in step with app/gateway_routes_test.go, whose
# TestEveryModuleRegistersGatewayRoutes fails in CI if a newly wired module
# is missing from the gateway entirely.
#
# If an endpoint here ever has a legitimate reason to answer non-200 on a
# healthy chain, move it to a documented exception with the reason -- do not
# relax the assertion for everything.
# ----------------------------------------------------------------------------
echo ""
echo "[1/4] module routes serve 200"

PATHS=(
    "/sparkdream/blog/v1/params"
    "/sparkdream/collect/v1/params"
    "/sparkdream/commons/v1/params"
    "/sparkdream/ecosystem/v1/params"
    "/sparkdream/federation/v1/params"
    "/sparkdream/forum/v1/params"
    "/sparkdream/futarchy/v1/params"
    "/sparkdream/guardian/v1/allowed_msgs"
    "/sparkdream/identity/v1/chain-identity"
    "/sparkdream/name/v1/params"
    "/sparkdream/rep/v1/params"
    "/sparkdream/reveal/v1/params"
    "/sparkdream/season/v1/params"
    "/sparkdream/service/v1/params"
    "/sparkdream/session/v1/params"
    "/sparkdream/shield/v1/params"
    "/sparkdream/split/v1/params"
    "/sparkdream/sparkdream/v1/params"
    "/cosmos/auth/v1beta1/params"
    "/cosmos/bank/v1beta1/params"
    "/cosmos/distribution/v1beta1/params"
    "/cosmos/gov/v1/params/deposit"
    "/cosmos/mint/v1beta1/params"
    "/cosmos/slashing/v1beta1/params"
    "/cosmos/staking/v1beta1/params"
    "/cosmos/upgrade/v1beta1/current_plan"
    "/ibc/apps/transfer/v1/params"
    "/ibc/core/channel/v1/channels"
    "/ibc/core/connection/v1/connections"
    "/ibc/core/client/v1/params"
    "/cosmos/tx/v1beta1/txs?query=tx.height%3D1"
    "/cosmos/base/tendermint/v1beta1/node_info"
    "/cosmos/base/node/v1beta1/config"
)

NOT_REGISTERED=0
HANDLER_FAILED=0

for p in "${PATHS[@]}"; do
    BODY=$(curl -s --max-time 10 -w $'\n%{http_code}' "$LCD$p" 2>/dev/null || printf '\n000')
    CODE=$(echo "$BODY" | tail -1)
    PAYLOAD=$(echo "$BODY" | sed '$d')

    case "$CODE" in
        200)
            echo "  [ OK ] $p"
            ;;
        501)
            echo "  [FAIL] $p -> 501 (route never registered on the gateway mux)"
            NOT_REGISTERED=$((NOT_REGISTERED + 1))
            FAIL_COUNT=$((FAIL_COUNT + 1))
            ;;
        500)
            echo "  [FAIL] $p -> 500 (route registered; handler failed)"
            echo "         body: $(echo "$PAYLOAD" | head -c 200)"
            HANDLER_FAILED=$((HANDLER_FAILED + 1))
            FAIL_COUNT=$((FAIL_COUNT + 1))
            ;;
        000)
            echo "  [FAIL] $p -> no response (API server died?)"
            FAIL_COUNT=$((FAIL_COUNT + 1))
            ;;
        *)
            echo "  [FAIL] $p -> $CODE (expected 200)"
            echo "         body: $(echo "$PAYLOAD" | head -c 200)"
            FAIL_COUNT=$((FAIL_COUNT + 1))
            ;;
    esac
done

# ----------------------------------------------------------------------------
# 2. Handler panics raised by the requests above. A 500 from gateway_fix.go's
#    safety net carries no detail in the body -- the reason is one stderr
#    line in the node log, and this is the only place it surfaces.
# ----------------------------------------------------------------------------
echo ""
echo "[2/4] no gateway handler panics"

if [ "${#NODE_LOGS[@]}" -eq 0 ]; then
    echo "  [WARN] node log not found; cannot check for handler panics"
else
    PANICS_AFTER=$(count_panics)
    NEW_PANICS=$((PANICS_AFTER - PANICS_BEFORE))
    if [ "$NEW_PANICS" -gt 0 ]; then
        echo "  [FAIL] $NEW_PANICS gateway handler panic(s) during this test"
        for lg in "${NODE_LOGS[@]}"; do
            grep "$PANIC_MARKER" "$lg" 2>/dev/null | tail -n "$NEW_PANICS" | sed 's/^/         /'
        done
        echo ""
        echo "         A panic naming math.Int or math.LegacyDec means proto v2"
        echo "         reflection reached a gogoproto custom type -- the failure"
        echo "         app/gateway_fix.go exists to prevent. See the REST Gateway"
        echo "         Routes section of docs/development-conventions.md."
        FAIL_COUNT=$((FAIL_COUNT + 1))
    else
        echo "  [ OK ] no handler panics logged"
    fi
fi

# ----------------------------------------------------------------------------
# 3. Block endpoints -- answered by preIntercept ahead of the mux because the
#    gogogateway JSONPb marshaler cannot render their *time.Time fields.
# ----------------------------------------------------------------------------
echo ""
echo "[3/4] block endpoints (pre-intercepted)"

LATEST=$(curl -s --max-time 10 "$LCD/cosmos/base/tendermint/v1beta1/blocks/latest" 2>/dev/null || echo "")
LATEST_TIME=$(echo "$LATEST" | jq -r '.block.header.time // ""' 2>/dev/null || echo "")
LATEST_HEIGHT=$(echo "$LATEST" | jq -r '.block.header.height // ""' 2>/dev/null || echo "")

if [ -z "$LATEST_TIME" ] || [ "$LATEST_TIME" = "null" ]; then
    echo "  [FAIL] blocks/latest did not return a parseable header time"
    echo "         response: $(echo "$LATEST" | head -c 200)"
    FAIL_COUNT=$((FAIL_COUNT + 1))
else
    echo "  [ OK ] blocks/latest -> height $LATEST_HEIGHT, time $LATEST_TIME"
fi

BY_HEIGHT=$(curl -s --max-time 10 "$LCD/cosmos/base/tendermint/v1beta1/blocks/1" 2>/dev/null || echo "")
BY_HEIGHT_TIME=$(echo "$BY_HEIGHT" | jq -r '.block.header.time // ""' 2>/dev/null || echo "")
if [ -z "$BY_HEIGHT_TIME" ] || [ "$BY_HEIGHT_TIME" = "null" ]; then
    echo "  [FAIL] blocks/1 did not return a parseable header time"
    echo "         response: $(echo "$BY_HEIGHT" | head -c 200)"
    FAIL_COUNT=$((FAIL_COUNT + 1))
else
    echo "  [ OK ] blocks/1 -> time $BY_HEIGHT_TIME"
fi

# ----------------------------------------------------------------------------
# 4. Response shape: snake_case names, zero values retained. These come from
#    the mux's marshaler options; without them every LCD consumer's field
#    paths break at once (and jq paths in these test scripts along with them).
# ----------------------------------------------------------------------------
echo ""
echo "[4/4] marshaler options (snake_case + zero values retained)"

BANK_PARAMS=$(curl -s --max-time 10 "$LCD/cosmos/bank/v1beta1/params" 2>/dev/null || echo "")

if echo "$BANK_PARAMS" | jq -e 'has("params") and (.params | has("default_send_enabled"))' >/dev/null 2>&1; then
    echo "  [ OK ] bank params kept snake_case default_send_enabled"
else
    echo "  [FAIL] bank params lost snake_case/zero-value field spelling"
    echo "         response: $(echo "$BANK_PARAMS" | head -c 200)"
    FAIL_COUNT=$((FAIL_COUNT + 1))
fi

# ----------------------------------------------------------------------------
echo ""
echo "=================================================="
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "RESULT: $FAIL_COUNT check(s) FAILED"
    if [ "$NOT_REGISTERED" -gt 0 ]; then
        echo "  $NOT_REGISTERED endpoint(s) not registered on the mux (501)"
    fi
    if [ "$HANDLER_FAILED" -gt 0 ]; then
        echo "  $HANDLER_FAILED endpoint(s) registered but failing in the handler (500)"
    fi
    echo "=================================================="
    exit 1
fi
echo "RESULT: all REST gateway checks passed"
echo "=================================================="
