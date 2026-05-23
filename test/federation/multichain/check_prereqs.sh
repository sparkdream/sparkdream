#!/bin/bash
# ------------------------------------------------------------------
# Verify host prerequisites for the multi-chain federation suite.
#
# Fails fast with actionable messages when:
#   - sparkdreamd is missing or not built with testparams
#   - hermes is missing or too old
#   - jq, sha256sum, xxd are missing
#   - the post-setup snapshot is missing
#   - the federation IBC port is not bound in app.go
#
# Exit 0 on success, non-zero with a diagnostic message otherwise.
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$( cd "$SCRIPT_DIR/../../.." && pwd )"

BINARY="${BINARY:-sparkdreamd}"
HERMES="${HERMES:-hermes}"
SNAPSHOT="${SNAPSHOT:-$SCRIPT_DIR/../snapshots/post-setup/sparkdream_data}"

ERR=0

red()   { printf '\033[0;31m%s\033[0m\n' "$*"; }
green() { printf '\033[0;32m%s\033[0m\n' "$*"; }
yellow(){ printf '\033[0;33m%s\033[0m\n' "$*"; }

echo "=================================================="
echo "  PREREQUISITE CHECKS"
echo "=================================================="
echo ""

# ------------------------------------------------------------------
# 1. sparkdreamd binary
# ------------------------------------------------------------------
if ! command -v "$BINARY" >/dev/null 2>&1; then
    red "  [FAIL] $BINARY not on PATH"
    echo "         Install: cd $PROJECT_DIR && ignite chain build --build.tags testparams"
    ERR=1
else
    BIN_PATH=$(command -v "$BINARY")
    GOPATH_BIN="$(go env GOPATH 2>/dev/null)/bin/$BINARY"
    green "  [ OK ] $BINARY -> $BIN_PATH"

    if [ "$BIN_PATH" != "$GOPATH_BIN" ] && [ -x "$GOPATH_BIN" ]; then
        yellow "  [WARN] PATH binary differs from GOPATH binary ($GOPATH_BIN)"
        yellow "         Project convention: only one sparkdreamd binary should exist (see docs/development-conventions.md)."
    fi

    # Quick check that this is a testparams build (testparams reduces voting periods etc.)
    if [ -x "$PROJECT_DIR/test/check_testparams.sh" ]; then
        if ! bash "$PROJECT_DIR/test/check_testparams.sh" >/dev/null 2>&1; then
            yellow "  [WARN] sparkdreamd may not be built with -tags testparams"
            yellow "         Multi-chain tests assume short voting periods. Run:"
            yellow "         (cd $PROJECT_DIR && ignite chain build --build.tags testparams)"
        fi
    fi
fi

# ------------------------------------------------------------------
# 2. Hermes IBC relayer
# ------------------------------------------------------------------
if ! command -v "$HERMES" >/dev/null 2>&1; then
    red "  [FAIL] hermes not on PATH (set HERMES=/path/to/hermes if installed elsewhere)"
    echo "         Install (recommended: pre-built binary):"
    echo "           https://github.com/informalsystems/hermes/releases (use v1.13.3 or newer)"
    echo "         Or via cargo:"
    echo "           cargo install --version 1.13.3 ibc-relayer-cli --bin hermes --locked"
    echo "         Note: this project uses ibc-go/v10. Hermes < 1.13 predates ibc-go/v10"
    echo "         and may fail at the channel handshake."
    ERR=1
else
    HERMES_VER=$("$HERMES" version 2>&1 | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
    if [ -z "$HERMES_VER" ]; then
        yellow "  [WARN] hermes found but version could not be parsed"
    else
        green "  [ OK ] hermes v$HERMES_VER"
        # Floor: 1.13.0 — first line with ibc-go/v10 support, which this project uses.
        MAJOR=$(echo "$HERMES_VER" | cut -d. -f1)
        MINOR=$(echo "$HERMES_VER" | cut -d. -f2)
        if [ "$MAJOR" -lt 1 ] || { [ "$MAJOR" -eq 1 ] && [ "$MINOR" -lt 13 ]; }; then
            yellow "  [WARN] hermes < 1.13 predates ibc-go/v10 (this project's IBC version)."
            yellow "         Channel handshake or packet relay may fail. Upgrade to 1.13.3+:"
            yellow "         https://github.com/informalsystems/hermes/releases"
        fi
    fi
fi

# ------------------------------------------------------------------
# 3. jq, xxd, sha256sum
# ------------------------------------------------------------------
for tool in jq xxd sha256sum; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        red "  [FAIL] $tool not on PATH"
        ERR=1
    else
        green "  [ OK ] $tool"
    fi
done

# ------------------------------------------------------------------
# 4. Post-setup snapshot (warn-only — orchestrator auto-creates)
# ------------------------------------------------------------------
if [ ! -d "$SNAPSHOT" ]; then
    yellow "  [WARN] post-setup snapshot missing: $SNAPSHOT"
    yellow "         The orchestrator will auto-create it via:"
    yellow "           $PROJECT_DIR/test/federation/run_all_tests.sh --save-setup"
    yellow "         Pass --no-auto-snapshot to fail-fast instead."
else
    if [ ! -f "$SNAPSHOT/config/genesis.json" ]; then
        red "  [FAIL] snapshot exists but missing config/genesis.json (corrupt — delete and rebuild)"
        ERR=1
    else
        green "  [ OK ] post-setup snapshot present"
    fi
fi

# ------------------------------------------------------------------
# 5. Federation IBC port bound in app.go
# ------------------------------------------------------------------
APP_IBC="$PROJECT_DIR/app/ibc.go"
if [ ! -f "$APP_IBC" ]; then
    yellow "  [WARN] $APP_IBC not found; skipping federation port binding check"
else
    if grep -q 'AddRoute(federationmoduletypes' "$APP_IBC" 2>/dev/null; then
        green "  [ OK ] federation port bound in app/ibc.go"
    else
        red "  [FAIL] federation port not registered in $APP_IBC"
        echo "         The IBC channel handshake will hang silently without this binding."
        ERR=1
    fi
fi

# ------------------------------------------------------------------
# 6. Port availability (best-effort)
# ------------------------------------------------------------------
for port in 26657 26656 9090 1317 36657 36656 9190 1417; do
    if command -v ss >/dev/null 2>&1; then
        if ss -tln 2>/dev/null | awk '{print $4}' | grep -qE ":$port\$"; then
            yellow "  [WARN] port $port is already in use (will conflict on chain start)"
        fi
    fi
done

echo ""
if [ "$ERR" -ne 0 ]; then
    red "PREREQUISITES FAILED — fix the above before running multichain tests."
    exit 1
fi
green "All prerequisites OK."
