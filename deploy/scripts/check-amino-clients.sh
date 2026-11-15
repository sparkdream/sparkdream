#!/bin/bash
# ============================================================================
# CHECK-AMINO-CLIENTS — are the clients' amino files current with this chain?
# ============================================================================
# Keplr+Ledger users sign in amino-JSON mode, and a client signs correctly
# only if it renders every message exactly as this chain does. The clients
# hold two files generated from this repo by cmd/aminogolden:
#
#   schema   sparkdreamjs builds its amino converters from it
#   golden   sparkdreamjs and sparkdream-ui test their converters against it
#
# A proto change that ships without regenerating them leaves the clients
# signing documents the chain no longer rebuilds: "signature verification
# failed" for every Ledger user of the changed message. This script
# regenerates both from the current tree and compares them with the copies
# in the client checkouts beside this repo:
#
#   ../sparkdreamjs/packages/sparkdreamjs/src-overrides/amino-schema.ts
#   ../sparkdreamjs/packages/sparkdreamjs/__tests__/amino-golden.jsonl
#   ../sparkdream-ui/src/lib/__fixtures__/amino-golden.jsonl
#
# It also checks that sparkdream-ui depends on the sparkdreamjs version in
# the checkout, and, when sparkdream-ui has its node_modules, runs its signing
# test and verifies every transaction it signs the way the ante handler does
# (`aminogolden verify`).
#
# A missing checkout is skipped with a note (it cannot be checked from here);
# pass --require-clients to make that a failure. Exit 1 on any stale file or
# failed signature, 0 otherwise. Nothing is written outside a temp directory.
#
# Usage:
#   deploy/scripts/check-amino-clients.sh
#   deploy/scripts/check-amino-clients.sh --require-clients
#   deploy/scripts/check-amino-clients.sh --no-sign     # skip the UI signing test
#
# Environment:
#   SPARKDREAMJS_DIR   sparkdreamjs checkout   (default: ../sparkdreamjs)
#   SPARKDREAM_UI_DIR  sparkdream-ui checkout  (default: ../sparkdream-ui)
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$( cd "$SCRIPT_DIR/../.." && pwd )"
cd "$REPO_ROOT"

if [ -t 1 ]; then
    RED='\033[31m'; GRN='\033[32m'; YLW='\033[33m'; CYN='\033[36m'; RST='\033[0m'
else
    RED=''; GRN=''; YLW=''; CYN=''; RST=''
fi
ok()   { printf "${GRN}[OK]${RST} %s\n" "$*"; }
warn() { printf "${YLW}[WARN]${RST} %s\n" "$*"; }
fail() { printf "${RED}[FAIL]${RST} %s\n" "$*" >&2; }
note() { printf "${CYN}[NOTE]${RST} %s\n" "$*"; }

REQUIRE=false
DO_SIGN=true
for arg in "$@"; do
    case "$arg" in
        --require-clients) REQUIRE=true ;;
        --no-sign) DO_SIGN=false ;;
        -h|--help) awk '/^#!/ {next} /^[^#]/ {exit} /^#/ {print}' "$0" | sed 's/^#\s\?//'; exit 0 ;;
        *) fail "unknown argument: $arg"; exit 2 ;;
    esac
done

JS_DIR="${SPARKDREAMJS_DIR:-$REPO_ROOT/../sparkdreamjs}"
UI_DIR="${SPARKDREAM_UI_DIR:-$REPO_ROOT/../sparkdream-ui}"
JS_PKG="$JS_DIR/packages/sparkdreamjs"

for cmd in go jq cmp; do
    command -v "$cmd" >/dev/null 2>&1 || { fail "missing required tool: $cmd"; exit 2; }
done

TMP="$(mktemp -d)"
cleanup() {
    rm -f "$TMP/schema.json" "$TMP/golden.jsonl" "$TMP/have-schema.json" "$TMP/want-schema.json" \
        "$TMP/signed.jsonl" "$TMP/npm-test.log" "$TMP/verify.log" "$TMP/aminogolden"
    rmdir "$TMP" 2>/dev/null || true
}
trap cleanup EXIT

go build -o "$TMP/aminogolden" ./cmd/aminogolden
"$TMP/aminogolden" schema > "$TMP/schema.json"
"$TMP/aminogolden" golden > "$TMP/golden.jsonl"
jq -cS . "$TMP/schema.json" > "$TMP/want-schema.json"
ok "chain renders $(wc -l < "$TMP/golden.jsonl") golden records for $(jq '.msgs | length' "$TMP/schema.json") Msgs"

STALE=0
missing() {
    if [ "$REQUIRE" = true ]; then
        fail "$1"
        STALE=1
    else
        note "$1 - skipped"
    fi
}

# --- sparkdreamjs -----------------------------------------------------------
if [ -d "$JS_PKG" ]; then
    SCHEMA_TS="$JS_PKG/src-overrides/amino-schema.ts"
    # The file is two comment lines and `export const aminoSchema = <json>;`.
    if [ -f "$SCHEMA_TS" ] && sed -n 's/^export const aminoSchema = \(.*\);$/\1/p' "$SCHEMA_TS" | jq -cS . > "$TMP/have-schema.json" 2>/dev/null \
        && cmp -s "$TMP/have-schema.json" "$TMP/want-schema.json"; then
        ok "sparkdreamjs amino schema is current"
    else
        fail "sparkdreamjs amino schema is stale: $SCHEMA_TS"
        echo "    (cd $REPO_ROOT && go run ./cmd/aminogolden schema) | (cd $JS_PKG && node scripts/amino-schema.js)" >&2
        STALE=1
    fi
    if cmp -s "$JS_PKG/__tests__/amino-golden.jsonl" "$TMP/golden.jsonl"; then
        ok "sparkdreamjs golden records are current"
    else
        fail "sparkdreamjs golden records are stale: $JS_PKG/__tests__/amino-golden.jsonl"
        echo "    (cd $REPO_ROOT && go run ./cmd/aminogolden golden) > $JS_PKG/__tests__/amino-golden.jsonl" >&2
        STALE=1
    fi
    if [ "$STALE" != 0 ]; then
        echo "    then run the sparkdreamjs sync-protos steps (codegen, jest) and publish a new version" >&2
    fi
else
    missing "no sparkdreamjs checkout at $JS_DIR"
fi

# --- sparkdream-ui ----------------------------------------------------------
if [ -d "$UI_DIR/src/lib" ]; then
    FIXTURE="$UI_DIR/src/lib/__fixtures__/amino-golden.jsonl"
    if cmp -s "$FIXTURE" "$TMP/golden.jsonl"; then
        ok "sparkdream-ui golden records are current"
    else
        fail "sparkdream-ui golden records are stale: $FIXTURE"
        echo "    (cd $REPO_ROOT && go run ./cmd/aminogolden golden) > $FIXTURE" >&2
        STALE=1
    fi
    if [ -d "$JS_PKG" ]; then
        JS_VERSION="$(jq -r .version "$JS_PKG/package.json")"
        UI_WANTS="$(jq -r '.dependencies["@sparkdreamnft/sparkdreamjs"] // ""' "$UI_DIR/package.json")"
        if [ "${UI_WANTS#^}" = "$JS_VERSION" ]; then
            ok "sparkdream-ui depends on sparkdreamjs $JS_VERSION"
        else
            fail "sparkdream-ui depends on sparkdreamjs $UI_WANTS, the checkout is at $JS_VERSION"
            echo "    (cd $UI_DIR && npm install @sparkdreamnft/sparkdreamjs@$JS_VERSION)   # after publishing it" >&2
            STALE=1
        fi
    fi
    if [ "$DO_SIGN" = false ]; then
        note "--no-sign: sparkdream-ui signing test not run"
    elif [ ! -d "$UI_DIR/node_modules" ] || ! command -v npm >/dev/null 2>&1; then
        note "sparkdream-ui has no node_modules (or npm is missing): signing test not run"
    else
        if (cd "$UI_DIR" && AMINO_SIGNED_OUT="$TMP/signed.jsonl" npm test > "$TMP/npm-test.log" 2>&1); then
            ok "sparkdream-ui tests pass"
        else
            fail "sparkdream-ui tests fail:"
            tail -n 30 "$TMP/npm-test.log" >&2
            STALE=1
        fi
        if [ -s "$TMP/signed.jsonl" ]; then
            if "$TMP/aminogolden" verify < "$TMP/signed.jsonl" > "$TMP/verify.log" 2>&1; then
                ok "the chain accepts all $(wc -l < "$TMP/signed.jsonl") transactions sparkdream-ui amino-signs in its tests"
            else
                fail "the chain rejects amino signatures from sparkdream-ui:"
                grep '^FAIL' "$TMP/verify.log" | cut -c1-400 >&2
                STALE=1
            fi
        fi
    fi
else
    missing "no sparkdream-ui checkout at $UI_DIR"
fi

if [ "$STALE" != 0 ]; then
    fail "amino clients are not current with this chain: Ledger signing would break. See docs/development-conventions.md (Amino Names)."
    exit 1
fi
ok "amino clients are current with this chain"
