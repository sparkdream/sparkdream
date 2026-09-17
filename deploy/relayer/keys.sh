#!/bin/bash
# ------------------------------------------------------------------
# Generate and import the two relayer keys Hermes signs with.
#
# Generates a fresh mnemonic per chain rather than reusing an existing account:
# the relayer signs continuously and unattended, so it should not share a key
# with a council member or a funded treasury account.
#
# This script does NOT fund the keys. On a live chain that is a real transfer
# from a real account, so it stays a deliberate manual step -- run this, then
# send to the printed addresses, then run fund_check.sh.
#
# Idempotent: existing mnemonics are reused, so re-running re-imports the same
# keys rather than orphaning funded ones.
#
# Usage: ./keys.sh
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
HERMES="${HERMES:-hermes}"
HERMES_CONFIG="${HERMES_CONFIG:-$SCRIPT_DIR/hermes_config.toml}"
BIN_DEV="${BIN_DEV:-$HOME/.local/bin/sparkdreamd-devnet}"

MNEMONICS_DIR="$SCRIPT_DIR/.hermes_mnemonics"

command -v "$HERMES" >/dev/null 2>&1 || {
    echo "ERROR: hermes not on PATH. Set HERMES=/path/to/hermes." >&2
    exit 1
}

# Restrictive umask BEFORE writing any mnemonic, so the file is created 600
# atomically -- a chmod afterwards leaves a window where the secret is
# world-readable.
mkdir -p "$MNEMONICS_DIR"
chmod 700 "$MNEMONICS_DIR"
umask 077

# generate <outfile>
# `sparkdreamd keys add --output json` puts the mnemonic in the JSON payload on
# stdout. Generate into a throwaway keyring home so the key itself never lands
# in anyone's real keyring; only the mnemonic file matters here.
generate() {
    local OUT=$1
    local TMP_HOME
    TMP_HOME=$(mktemp -d)
    local JSON
    JSON=$("$BIN_DEV" keys add throwaway --keyring-backend test --home "$TMP_HOME" --output json 2>/dev/null)
    find "$TMP_HOME" -mindepth 1 -delete 2>/dev/null || true
    rmdir "$TMP_HOME" 2>/dev/null || true
    if [ -z "$JSON" ]; then
        echo "ERROR: key generation produced no output" >&2
        return 1
    fi
    printf '%s' "$JSON" | python3 -c 'import json,sys; sys.stdout.write(json.load(sys.stdin)["mnemonic"])' > "$OUT"
    [ -s "$OUT" ] || { echo "ERROR: no mnemonic extracted" >&2; return 1; }
}

# key_name | chain_id
KEYS="relayer-dev:sparkdream-dev-1 relayer-test:sparkdream-test-1"

echo "=================================================="
echo "  RELAYER KEYS"
echo "=================================================="
echo ""

for pair in $KEYS; do
    NAME="${pair%%:*}"
    CHAIN="${pair##*:}"
    FILE="$MNEMONICS_DIR/$NAME.mnemonic"

    if [ -s "$FILE" ]; then
        echo "  $NAME: reusing existing mnemonic"
    else
        generate "$FILE"
        echo "  $NAME: generated"
    fi

    # --overwrite so a re-run is idempotent rather than erroring on a key that
    # is already imported.
    "$HERMES" --config "$HERMES_CONFIG" keys add \
        --chain "$CHAIN" \
        --mnemonic-file "$FILE" \
        --key-name "$NAME" \
        --overwrite >/dev/null 2>&1 \
        || { echo "  ERROR: hermes keys add failed for $NAME" >&2; exit 1; }
done

echo ""
echo "=== Addresses to fund ==="
for pair in $KEYS; do
    NAME="${pair%%:*}"
    CHAIN="${pair##*:}"
    ADDR=$("$HERMES" --config "$HERMES_CONFIG" keys list --chain "$CHAIN" 2>/dev/null \
        | grep -oE 'sprkdrm1[a-z0-9]+' | head -1)
    printf "  %-14s %-16s %s\n" "$NAME" "$CHAIN" "${ADDR:-<not found>}"
done

echo ""
echo "Mnemonics: $MNEMONICS_DIR (gitignored, mode 600)"
echo ""
echo "Next: fund both addresses in their own denom, then ./fund_check.sh"
