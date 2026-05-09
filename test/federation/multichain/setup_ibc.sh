#!/bin/bash
# ------------------------------------------------------------------
# Establish IBC connection and federation channel between
# chain-a (fedtest-a) and chain-b (fedtest-b).
#
# Steps:
#   1. Generate fresh BIP-39 mnemonics for the hermes relayer (hermes-a, hermes-b)
#   2. Import each mnemonic into both sparkdreamd (to derive an address) and hermes
#   3. Fund both relayer addresses from alice
#   4. Create IBC clients on both chains
#   5. Create IBC connection
#   6. Open federation channel (port=federation, version=federation-1)
#   7. Save channel IDs to .ibc_channels for downstream scripts
#
# This script does NOT start the hermes daemon — channels alone don't
# relay packets. Run start_relayer.sh after this completes.
#
# Prerequisites:
#   - Both chains running (start_chains.sh)
#   - hermes installed and on PATH
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/lib_multichain.sh"
# Shared safe-rmtree helper (CLAUDE.md forbids `rm -rf`).
# shellcheck source=../../_safe_rm.sh
source "$SCRIPT_DIR/../../_safe_rm.sh"

HERMES="${HERMES:-hermes}"
HERMES_CONFIG="$SCRIPT_DIR/hermes_config.toml"

if ! command -v "$HERMES" >/dev/null 2>&1; then
    echo "ERROR: hermes not on PATH. Set HERMES=/path/to/hermes."
    exit 1
fi

echo "=================================================="
echo "  IBC SETUP: HERMES RELAYER + FEDERATION CHANNEL"
echo "=================================================="
echo ""
echo "  hermes: $($HERMES version 2>&1 | head -1)"
echo ""

# ------------------------------------------------------------------
# 1. Generate (or reuse) BIP-39 mnemonics for the relayer
# ------------------------------------------------------------------
MNEMONICS_DIR="$SCRIPT_DIR/.hermes_mnemonics"
mkdir -p "$MNEMONICS_DIR"
chmod 700 "$MNEMONICS_DIR"
# Apply a restrictive umask BEFORE writing mnemonic files so they're created
# with mode 600 atomically — chmod-after leaves a brief window where the
# secret is world-readable on shared dev machines.
umask 077

generate_mnemonic_via_sparkdreamd() {
    # `sparkdreamd keys add` writes the mnemonic to stderr on key creation.
    # We capture, extract the 24 words, and immediately delete the throwaway key.
    # With --output json, sparkdreamd writes the mnemonic to stdout inside
    # the JSON payload (.mnemonic field), NOT to stderr. Parse with jq.
    local OUT_FILE=$1
    local TMP_HOME
    TMP_HOME=$(mktemp -d)
    local KEY_JSON
    KEY_JSON=$("$BINARY" keys add throwaway --keyring-backend test --home "$TMP_HOME" --output json 2>/dev/null)
    safe_rmtree "$TMP_HOME" || true
    if [ -z "$KEY_JSON" ]; then
        echo "ERROR: sparkdreamd keys add produced no output" >&2
        return 1
    fi
    local MNEMONIC
    MNEMONIC=$(echo "$KEY_JSON" | jq -r '.mnemonic // empty')
    if [ -z "$MNEMONIC" ] || [ "$MNEMONIC" = "null" ]; then
        echo "ERROR: keys add JSON missing .mnemonic field" >&2
        echo "  raw output (first 200 chars): $(echo "$KEY_JSON" | head -c 200)" >&2
        return 1
    fi
    echo "$MNEMONIC" > "$OUT_FILE"
    if [ ! -s "$OUT_FILE" ]; then
        echo "ERROR: failed to write mnemonic to $OUT_FILE" >&2
        return 1
    fi
}

if [ ! -s "$MNEMONICS_DIR/hermes-a.mnemonic" ]; then
    generate_mnemonic_via_sparkdreamd "$MNEMONICS_DIR/hermes-a.mnemonic"
fi
if [ ! -s "$MNEMONICS_DIR/hermes-b.mnemonic" ]; then
    generate_mnemonic_via_sparkdreamd "$MNEMONICS_DIR/hermes-b.mnemonic"
fi
chmod 600 "$MNEMONICS_DIR"/*.mnemonic

# ------------------------------------------------------------------
# 2. Import the mnemonics into sparkdreamd (so we can fund the addresses)
# ------------------------------------------------------------------
import_into_sparkdreamd() {
    local NAME=$1; local CHAIN_HOME=$2; local MNEMONIC_FILE=$3
    if "$BINARY" keys show "$NAME" --keyring-backend test --home "$CHAIN_HOME" >/dev/null 2>&1; then
        return 0
    fi
    "$BINARY" keys add "$NAME" --keyring-backend test --home "$CHAIN_HOME" \
        --recover --output json < "$MNEMONIC_FILE" >/dev/null 2>&1
}

echo "=== Importing relayer keys into sparkdreamd ==="
import_into_sparkdreamd hermes-a "$CHAIN_A_HOME" "$MNEMONICS_DIR/hermes-a.mnemonic"
import_into_sparkdreamd hermes-b "$CHAIN_B_HOME" "$MNEMONICS_DIR/hermes-b.mnemonic"

RELAYER_A=$("$BINARY" keys show hermes-a --keyring-backend test --home "$CHAIN_A_HOME" -a)
RELAYER_B=$("$BINARY" keys show hermes-b --keyring-backend test --home "$CHAIN_B_HOME" -a)
echo "  hermes-a: $RELAYER_A (chain-a)"
echo "  hermes-b: $RELAYER_B (chain-b)"
echo ""

# ------------------------------------------------------------------
# 3. Fund the relayer accounts from alice
# ------------------------------------------------------------------
echo "=== Funding relayer accounts ==="

# Helper: send a bank tx and surface failures clearly. Without `|| true`
# on the assignment, set -e would abort if the CLI returns non-zero, hiding
# the actual error message.
fund_relayer() {
    local CHAIN=$1   # a or b
    local TARGET=$2
    local AMOUNT=${3:-1000000000uspark}
    local TX
    if [ "$CHAIN" = "a" ]; then
        TX=$(cli_a tx bank send alice "$TARGET" "$AMOUNT" \
            --from alice -y --fees 5000uspark --output json 2>&1) || true
    else
        TX=$(cli_b tx bank send alice "$TARGET" "$AMOUNT" \
            --from alice -y --fees 5000uspark --output json 2>&1) || true
    fi
    if ! echo "$TX" | jq -e '.txhash' >/dev/null 2>&1; then
        echo "  ERROR: cli failed to produce a txhash" >&2
        echo "  --- raw output (first 500 chars) ---" >&2
        echo "$TX" | head -c 500 >&2
        echo "" >&2
        echo "  --- end raw output ---" >&2
        return 1
    fi
    if [ "$CHAIN" = "a" ]; then
        submit_and_wait_a "$TX" "fund $TARGET on chain-a"
    else
        submit_and_wait_b "$TX" "fund $TARGET on chain-b"
    fi
}

echo "  Funding $RELAYER_A on chain-a..."
fund_relayer a "$RELAYER_A"

echo "  Funding $RELAYER_B on chain-b..."
fund_relayer b "$RELAYER_B"

# Register the relayer accounts in auth.account: bank.SendCoins in
# Cosmos SDK v0.53 does NOT create an auth.BaseAccount for the recipient;
# the account exists in bank state but `query auth account <addr>` returns
# NotFound. Hermes's gRPC `query_account` path requires the BaseAccount,
# so without this step `hermes create client` aborts with:
#   "account <addr> not found"
# A self-send signed by the relayer key forces auth to materialize the
# account from the tx's auth_info pubkey.
register_account() {
    local CHAIN=$1   # a or b
    local KEY=$2     # hermes-a or hermes-b
    local ADDR=$3
    local TX
    if [ "$CHAIN" = "a" ]; then
        TX=$(cli_a tx bank send "$ADDR" "$ADDR" 1uspark --from "$KEY" -y --fees 5000uspark --output json 2>&1) || true
    else
        TX=$(cli_b tx bank send "$ADDR" "$ADDR" 1uspark --from "$KEY" -y --fees 5000uspark --output json 2>&1) || true
    fi
    if ! echo "$TX" | jq -e '.txhash' >/dev/null 2>&1; then
        echo "  ERROR: register-account self-send (chain-$CHAIN) failed" >&2
        echo "$TX" | head -c 500 >&2
        return 1
    fi
    # `set -e` is on, so an explicit `if ! ...; then` is the only way to
    # capture submit_and_wait_*'s non-zero exit without aborting the whole
    # script before we can react. The previous `_RC=$?` form was dead code:
    # the failing call would `exit` before the assignment ever ran.
    if [ "$CHAIN" = "a" ]; then
        if ! submit_and_wait_a "$TX" "register $KEY on chain-a"; then
            echo "  ERROR: register-account submit_and_wait (chain-a) failed" >&2
            return 1
        fi
    else
        if ! submit_and_wait_b "$TX" "register $KEY on chain-b"; then
            echo "  ERROR: register-account submit_and_wait (chain-b) failed" >&2
            return 1
        fi
    fi
    # Verify the auth.account record actually materialized — bank.SendCoins
    # alone does not create a BaseAccount, and on this chain the AnteHandler
    # is what creates it from the tx's auth_info pubkey on first signature.
    # We log both the bank balance and the auth account so any continuing
    # NotFound error in `hermes create client` can be diagnosed.
    if [ "$CHAIN" = "a" ]; then
        echo "  chain-a balance: $(qcli_a bank balances "$ADDR" 2>/dev/null | jq -c '.balances // []')"
        echo "  chain-a auth.account: $(qcli_a auth account "$ADDR" 2>/dev/null | jq -c '.account // .' | head -c 300)"
    else
        echo "  chain-b balance: $(qcli_b bank balances "$ADDR" 2>/dev/null | jq -c '.balances // []')"
        echo "  chain-b auth.account: $(qcli_b auth account "$ADDR" 2>/dev/null | jq -c '.account // .' | head -c 300)"
    fi
}

echo "  Registering hermes-a in auth.account on chain-a (self-send 1uspark)..."
register_account a hermes-a "$RELAYER_A"
echo "  Registering hermes-b in auth.account on chain-b (self-send 1uspark)..."
register_account b hermes-b "$RELAYER_B"

# Verify balances
BAL_A=$(qcli_a bank balances "$RELAYER_A" 2>/dev/null | jq -r '.balances[]? | select(.denom=="uspark") | .amount' | head -1)
BAL_B=$(qcli_b bank balances "$RELAYER_B" 2>/dev/null | jq -r '.balances[]? | select(.denom=="uspark") | .amount' | head -1)
echo "  Balance hermes-a: ${BAL_A:-0} uspark"
echo "  Balance hermes-b: ${BAL_B:-0} uspark"
echo ""

# ------------------------------------------------------------------
# 4. Import mnemonics into hermes (clean slate first)
# ------------------------------------------------------------------
echo "=== Importing relayer keys into hermes ==="
"$HERMES" --config "$HERMES_CONFIG" keys add \
    --chain fedtest-a \
    --key-name hermes-a \
    --mnemonic-file "$MNEMONICS_DIR/hermes-a.mnemonic" \
    --overwrite 2>&1 | tail -5

"$HERMES" --config "$HERMES_CONFIG" keys add \
    --chain fedtest-b \
    --key-name hermes-b \
    --mnemonic-file "$MNEMONICS_DIR/hermes-b.mnemonic" \
    --overwrite 2>&1 | tail -5
echo ""

# ------------------------------------------------------------------
# 5. Create IBC clients — TRULY idempotent.
#
# Naive `hermes create client` is NOT idempotent: each invocation creates
# a fresh client even if a working one already tracks the counterparty.
# Reusing previous clients matters because the connection step below grabs
# `query clients | tail -1` and would otherwise pair the channel against
# a stale-but-newer client on one side and the matching one on the other,
# producing a broken connection.
#
# Strategy: count existing clients BEFORE create, and only run create if
# the chain has zero clients tracking the counterparty. On first run this
# is identical to the unconditional create; on re-runs (e.g. after a
# transient hermes failure) we skip and reuse what's already there.
# ------------------------------------------------------------------
count_clients_for_chain() {
    # $1: host chain id, $2: reference chain id (counterparty)
    "$HERMES" --config "$HERMES_CONFIG" query clients --host-chain "$1" 2>/dev/null \
        | grep -cE "($2|07-tendermint-)" || true
}

echo "=== Creating IBC clients ==="
if [ "$(count_clients_for_chain fedtest-a fedtest-b)" -eq 0 ]; then
    "$HERMES" --config "$HERMES_CONFIG" create client \
        --host-chain fedtest-a --reference-chain fedtest-b 2>&1 | tail -3
    sleep 3
else
    echo "  client already exists on fedtest-a tracking fedtest-b — skipping create"
fi
if [ "$(count_clients_for_chain fedtest-b fedtest-a)" -eq 0 ]; then
    "$HERMES" --config "$HERMES_CONFIG" create client \
        --host-chain fedtest-b --reference-chain fedtest-a 2>&1 | tail -3
    sleep 3
else
    echo "  client already exists on fedtest-b tracking fedtest-a — skipping create"
fi

# ------------------------------------------------------------------
# 6. Create IBC connection — also idempotent.
#
# `hermes create connection` likewise creates a fresh handshake every
# call. We only run create when both chains have zero connections; on
# re-runs we reuse the existing connection (matched by symmetry on
# both ends so the IDs aren't shuffled by hermes).
# ------------------------------------------------------------------
list_connection_ids() {
    "$HERMES" --config "$HERMES_CONFIG" query connections --chain "$1" 2>&1 \
        | grep -oE 'connection-[0-9]+' || true
}

EXISTING_CONNS_A=$(list_connection_ids fedtest-a | wc -l)
EXISTING_CONNS_B=$(list_connection_ids fedtest-b | wc -l)
echo ""
echo "=== Creating IBC connection ==="
if [ "$EXISTING_CONNS_A" -eq 0 ] && [ "$EXISTING_CONNS_B" -eq 0 ]; then
    "$HERMES" --config "$HERMES_CONFIG" create connection \
        --a-chain fedtest-a --b-chain fedtest-b 2>&1 | tail -10
    sleep 3
else
    echo "  connection already exists (A=$EXISTING_CONNS_A, B=$EXISTING_CONNS_B) — skipping create"
fi

# ------------------------------------------------------------------
# 7. Find the connection IDs for channel creation.
# Use the LAST connection on each side — it's the most recently created
# and is the one paired by `hermes create connection` above.
# ------------------------------------------------------------------
CONN_A=$(list_connection_ids fedtest-a | tail -1)
CONN_B=$(list_connection_ids fedtest-b | tail -1)

if [ -z "$CONN_A" ] || [ -z "$CONN_B" ]; then
    echo "ERROR: failed to discover IBC connections"
    echo "  chain-a connections:"
    "$HERMES" --config "$HERMES_CONFIG" query connections --chain fedtest-a 2>&1 | head -20
    echo "  chain-b connections:"
    "$HERMES" --config "$HERMES_CONFIG" query connections --chain fedtest-b 2>&1 | head -20
    exit 1
fi
echo "  Connection A: $CONN_A"
echo "  Connection B: $CONN_B"

# ------------------------------------------------------------------
# 8. Open the federation channel — idempotent.
# Skip create if hermes already shows a channel on the federation port.
# ------------------------------------------------------------------
existing_federation_channel() {
    "$HERMES" --config "$HERMES_CONFIG" query channels --chain "$1" 2>&1 \
        | grep -B1 'federation' | grep -oE 'channel-[0-9]+' | head -1
}

echo ""
echo "=== Opening federation channel ==="
if [ -z "$(existing_federation_channel fedtest-a)" ] || \
   [ -z "$(existing_federation_channel fedtest-b)" ]; then
    "$HERMES" --config "$HERMES_CONFIG" create channel \
        --a-chain fedtest-a \
        --a-connection "$CONN_A" \
        --a-port federation \
        --b-port federation \
        --channel-version federation-1 \
        --order unordered 2>&1 | tail -10
    sleep 5
else
    echo "  federation channel already exists on both chains — skipping create"
fi

# ------------------------------------------------------------------
# 9. Discover and save channel IDs
# ------------------------------------------------------------------
extract_federation_channel() {
    local CHAIN=$1
    "$HERMES" --config "$HERMES_CONFIG" query channels --chain "$CHAIN" --show-counterparty 2>&1 \
        | grep -B1 'federation' \
        | grep -oE 'channel-[0-9]+' \
        | head -1
}

CHANNEL_A=$(extract_federation_channel fedtest-a)
CHANNEL_B=$(extract_federation_channel fedtest-b)

# Fallback to first channel if filter didn't catch it
if [ -z "$CHANNEL_A" ]; then
    CHANNEL_A=$("$HERMES" --config "$HERMES_CONFIG" query channels --chain fedtest-a 2>&1 \
        | grep -oE 'channel-[0-9]+' | head -1)
fi
if [ -z "$CHANNEL_B" ]; then
    CHANNEL_B=$("$HERMES" --config "$HERMES_CONFIG" query channels --chain fedtest-b 2>&1 \
        | grep -oE 'channel-[0-9]+' | head -1)
fi

if [ -z "$CHANNEL_A" ] || [ -z "$CHANNEL_B" ]; then
    echo "ERROR: could not discover federation channels."
    echo "  chain-a channels:"
    "$HERMES" --config "$HERMES_CONFIG" query channels --chain fedtest-a 2>&1 | head -20
    echo "  chain-b channels:"
    "$HERMES" --config "$HERMES_CONFIG" query channels --chain fedtest-b 2>&1 | head -20
    exit 1
fi

cat > "$SCRIPT_DIR/.ibc_channels" <<EOF
CHANNEL_A=$CHANNEL_A
CHANNEL_B=$CHANNEL_B
CONN_A=$CONN_A
CONN_B=$CONN_B
EOF

echo ""
echo "=================================================="
echo "  IBC SETUP COMPLETE"
echo "=================================================="
echo ""
echo "  Channel A (fedtest-a): $CHANNEL_A"
echo "  Channel B (fedtest-b): $CHANNEL_B"
echo ""
echo "  Saved to: $SCRIPT_DIR/.ibc_channels"
echo ""
echo "  Next: start_relayer.sh"
echo ""
