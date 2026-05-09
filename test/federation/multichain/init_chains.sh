#!/bin/bash
# ------------------------------------------------------------------
# Initialize two independent sparkdreamd chains for the multi-chain
# federation E2E suite.
#
# Strategy: ignite chain init produces a complete, IBC-ready genesis
# (founders, Commons Council, x/rep, x/name, etc. all bootstrapped via
# the genesis_bootstrap modules). We run it once, then clone the
# resulting ~/.sparkdream into two distinct homes — chain-a and
# chain-b — patching chain_id, ports, and node_key per chain.
#
# Why not the post-setup snapshot? The snapshot's CometBFT data dir
# carries the original chain_id ("sparkdream") in cached state and in
# block headers. Rewriting genesis.json after-the-fact has no effect on
# a chain that already has state, so any blocks chain-b produces still
# claim to belong to the original chain — which the IBC light client
# rejects. By starting from a fresh `ignite chain init` per chain, the
# new chain_id is baked in from height 1.
#
# What we lose: the snapshot's federation-specific accounts (linker1,
# operator1, verifier1). The multichain tests don't need these — they
# create their own per-chain keys via setup_chain_keys.sh (mc-linker-a,
# mc-owner-b). What we keep: alice/bob/carol founders, Commons Council,
# x/rep base infrastructure — all needed for council governance and
# x/rep trust-level checks.
#
# Prerequisites: check_prereqs.sh must pass.
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$( cd "$SCRIPT_DIR/../../.." && pwd )"

# Shared safe-rmtree helper (CLAUDE.md forbids `rm -rf`).
# shellcheck source=../../_safe_rm.sh
source "$SCRIPT_DIR/../../_safe_rm.sh"

BINARY="${BINARY:-sparkdreamd}"
CHAIN_A_HOME="$SCRIPT_DIR/data/chain-a"
CHAIN_B_HOME="$SCRIPT_DIR/data/chain-b"
CHAIN_A_ID="fedtest-a"
CHAIN_B_ID="fedtest-b"

# Bootstrap template: where ignite chain init drops its scaffolded chain home.
# We put it under our own data/ dir (not $HOME/.sparkdream) so a single
# multichain run never touches the user's main local chain. ignite hardcodes
# its output path to $HOME/.sparkdream, so we redirect by temporarily moving
# any pre-existing $HOME/.sparkdream out of the way and back into place.
TEMPLATE_HOME="$SCRIPT_DIR/data/template"

echo "=================================================="
echo "  MULTI-CHAIN INITIALIZATION"
echo "=================================================="
echo ""

# ------------------------------------------------------------------
# Step 0: Run `ignite chain init` once and capture the result as our
# bootstrap template. This bootstraps founders, Commons Council, x/rep
# base infrastructure via the project's genesis_bootstrap.
#
# IMPORTANT: ignite hardcodes its output to $HOME/.sparkdream. To avoid
# wiping the user's primary local chain home, we:
#   1. Move any pre-existing $HOME/.sparkdream aside as a temp backup.
#   2. Run ignite chain init (which now writes into a clean $HOME/.sparkdream).
#   3. Move the result into our own data/template/ dir.
#   4. Restore the user's original $HOME/.sparkdream.
#
# Cleanup of stale chain processes is scoped to this script's chains
# (template/, data/chain-[ab]) — we do NOT touch parallel suite chains
# (test/run_parallel.sh's sparkdreamd lives under e2e/parallel-PID/suite-N/
# home/, never matches data/chain-[ab] or template/).
# ------------------------------------------------------------------
echo "=== Step 0: Bootstrapping template chain via ignite chain init ==="

# Stop any prior multichain chains. Surgical patterns only.
pkill -f "sparkdreamd.*$SCRIPT_DIR/data/template" 2>/dev/null || true
pkill -f "sparkdreamd.*data/chain-[ab]"           2>/dev/null || true
sleep 2

# Clean any leftovers from a previous multichain run.
safe_rmtree "$TEMPLATE_HOME"

# Move the user's $HOME/.sparkdream aside if it exists, so ignite chain
# init starts from a clean slate without us having to delete it.
USER_HOME_BAK=""
if [ -e "$HOME/.sparkdream" ]; then
    USER_HOME_BAK="$HOME/.sparkdream.multichain-bak.$$"
    mv "$HOME/.sparkdream" "$USER_HOME_BAK"
fi

# Run ignite. Use a trap so an interrupt during init still restores the user's
# $HOME/.sparkdream. set -e is on; we capture explicit rc via `||`.
restore_user_home() {
    if [ -n "$USER_HOME_BAK" ] && [ -e "$USER_HOME_BAK" ]; then
        # Drop whatever ignite produced (we may or may not have moved it
        # successfully) before restoring the user's original home.
        if [ -e "$HOME/.sparkdream" ]; then
            safe_rmtree "$HOME/.sparkdream"
        fi
        mv "$USER_HOME_BAK" "$HOME/.sparkdream"
    fi
}
trap 'rc=$?; restore_user_home; exit $rc' INT TERM

INIT_RC=0
(cd "$PROJECT_DIR" && ignite chain init -y --build.tags testparams) || INIT_RC=$?
if [ "$INIT_RC" -ne 0 ]; then
    echo "ERROR: ignite chain init failed (rc=$INIT_RC)"
    restore_user_home
    trap - INT TERM
    exit 1
fi
if [ ! -d "$HOME/.sparkdream" ]; then
    echo "ERROR: ignite did not produce ~/.sparkdream"
    restore_user_home
    trap - INT TERM
    exit 1
fi

# Move ignite's output into our data/template/ slot.
mkdir -p "$(dirname "$TEMPLATE_HOME")"
mv "$HOME/.sparkdream" "$TEMPLATE_HOME"

# Restore the user's original $HOME/.sparkdream.
restore_user_home
trap - INT TERM

echo "  template ready at $TEMPLATE_HOME"

# ------------------------------------------------------------------
# Helper: clone the template into a chain home, patch chain_id +
# ports, regenerate node_key (p2p identity) so chains are distinct.
# Args: target_home chain_id port_offset
# ------------------------------------------------------------------
clone_template() {
    local CHAIN_HOME=$1
    local CHAIN_ID=$2
    local PORT_OFFSET=$3   # 0 for chain-a, 10000 for chain-b

    # Refuse to operate on anything outside our own data/ tree — defensive
    # guard in case CHAIN_HOME is ever accidentally pointed at the user's
    # filesystem.
    case "$CHAIN_HOME" in
        "$SCRIPT_DIR"/data/*) ;;
        *) echo "ERROR: clone_template refusing CHAIN_HOME outside $SCRIPT_DIR/data: $CHAIN_HOME" >&2; return 1 ;;
    esac

    safe_rmtree "$CHAIN_HOME" || return 1
    mkdir -p "$(dirname "$CHAIN_HOME")"
    cp -r "$TEMPLATE_HOME" "$CHAIN_HOME"

    # Rewrite chain_id in genesis.json + client.toml. Because the data dir
    # we just copied has not been started yet, the new chain_id is honored
    # on first launch (no cached state).
    local GEN="$CHAIN_HOME/config/genesis.json"
    local tmp; tmp=$(mktemp)
    # Strip gentxs at the same time — their signatures are bound to ignite's
    # default chain_id ("sparkdream") and will fail signature verification
    # at chain init under our new chain_id. We re-run gentx below.
    jq --arg id "$CHAIN_ID" '
        .chain_id = $id
        | .app_state.genutil.gen_txs = []
        | .app_state.staking.validators = []
        | .app_state.staking.delegations = []
        | .app_state.staking.last_total_power = "0"
        | .app_state.staking.last_validator_powers = []
    ' "$GEN" > "$tmp" && mv "$tmp" "$GEN"
    sed -i "s|^chain-id *=.*|chain-id = \"$CHAIN_ID\"|" "$CHAIN_HOME/config/client.toml" 2>/dev/null || true

    # Wipe data dir so the chain genuinely starts at height 1 with the new
    # chain_id (no inherited state from the template's first-launch).
    safe_rmtree "$CHAIN_HOME/data" || return 1
    mkdir -p "$CHAIN_HOME/data"
    cat > "$CHAIN_HOME/data/priv_validator_state.json" <<'PVSTATE'
{
  "height": "0",
  "round": 0,
  "step": 0
}
PVSTATE

    # Regenerate BOTH node_key (p2p identity) AND priv_validator_key
    # (consensus pubkey). Sharing priv_validator_key across two chains was
    # historically "harmless because they're independent networks" — but
    # any future test that runs a third chain as a counterparty of both
    # could see double-sign-class outcomes from the same key. Cheap to
    # regenerate; safer by default.
    rm -f "$CHAIN_HOME/config/node_key.json" "$CHAIN_HOME/config/priv_validator_key.json"
    local TMP_INIT
    TMP_INIT=$(mktemp -d)
    "$BINARY" init nk-tmp --chain-id "$CHAIN_ID" --home "$TMP_INIT" >/dev/null 2>&1
    cp "$TMP_INIT/config/node_key.json"           "$CHAIN_HOME/config/node_key.json"
    cp "$TMP_INIT/config/priv_validator_key.json" "$CHAIN_HOME/config/priv_validator_key.json"
    safe_rmtree "$TMP_INIT" || true

    # Port offsets. Anchor each replacement so we only rewrite the *.laddr /
    # *.address / address-style lines and never an embedded comment, log
    # URL, or future telemetry endpoint that happens to contain ":9090".
    # The primary forms produced by ignite chain init are:
    #   tcp://0.0.0.0:<port>     (listen addresses)
    #   tcp://127.0.0.1:<port>   (RPC, client.toml node)
    #   tcp://localhost:<port>   (some templates' client.toml)
    #   0.0.0.0:<port>           (gRPC, gRPC-Web in app.toml)
    #   127.0.0.1:<port>         (telemetry / pprof)
    #   localhost:<port>         (api/grpc client side)
    # We anchor on these prefixes so a stray ":9090" in a comment is left
    # alone.
    if [ "$PORT_OFFSET" -ne 0 ]; then
        local A=$PORT_OFFSET
        local cfg="$CHAIN_HOME/config/config.toml"
        local app="$CHAIN_HOME/config/app.toml"
        local cli="$CHAIN_HOME/config/client.toml"

        local rewrite_port
        rewrite_port() {
            local file="$1" old="$2" new="$3"
            [ -f "$file" ] || return 0
            sed -i \
                -e "s|tcp://0.0.0.0:${old}|tcp://0.0.0.0:${new}|g" \
                -e "s|tcp://127.0.0.1:${old}|tcp://127.0.0.1:${new}|g" \
                -e "s|tcp://localhost:${old}|tcp://localhost:${new}|g" \
                -e "s|^address = \"0.0.0.0:${old}\"|address = \"0.0.0.0:${new}\"|" \
                -e "s|^address = \"127.0.0.1:${old}\"|address = \"127.0.0.1:${new}\"|" \
                -e "s|^address = \"localhost:${old}\"|address = \"localhost:${new}\"|" \
                -e "s|0.0.0.0:${old}|0.0.0.0:${new}|g" \
                -e "s|127.0.0.1:${old}|127.0.0.1:${new}|g" \
                -e "s|localhost:${old}|localhost:${new}|g" \
                "$file"
        }

        # CometBFT ports
        rewrite_port "$cfg" 26657 "$((26657 + A))"
        rewrite_port "$cfg" 26656 "$((26656 + A))"
        rewrite_port "$cfg" 26658 "$((26658 + A))"
        rewrite_port "$cfg" 26660 "$((26660 + A))"
        rewrite_port "$cfg" 6060  "$((6060 + A))"
        # gRPC, gRPC-Web, REST live in app.toml
        rewrite_port "$app" 9090 "$((9090 + (A / 100)))"
        rewrite_port "$app" 9091 "$((9091 + (A / 100)))"
        rewrite_port "$app" 1317 "$((1317 + (A / 100)))"
        # client.toml node URL
        rewrite_port "$cli" 26657 "$((26657 + A))"

        # Sanity check: confirm at least one substitution landed in each
        # rewritten file. If config.toml still contains the old RPC port,
        # fail loudly — silent sed drift would leave both chains on the
        # same port at runtime.
        if grep -q ":26657" "$cfg" 2>/dev/null; then
            echo "ERROR: port rewrite did not take effect on $cfg" >&2
            return 1
        fi
    fi

    # Pin minimum-gas-prices to match Hermes' gas_price (0.0025uspark, see
    # hermes_config.toml). Previously this was 0.001uspark, which under deep
    # IBC flushes (max_msg_num=30) could let Hermes compute a per-tx fee
    # below the chain's per-tx floor and trigger "insufficient fee" rejections.
    # 0.0025 matches the relayer side exactly.
    sed -i 's|^minimum-gas-prices *=.*|minimum-gas-prices = "0.0025uspark"|' "$CHAIN_HOME/config/app.toml"

    # Enable API + gRPC explicitly (Hermes needs gRPC).
    sed -i '/^\[api\]/,/^\[/{s|^enable *=.*|enable = true|}'  "$CHAIN_HOME/config/app.toml" 2>/dev/null || true
    sed -i '/^\[grpc\]/,/^\[/{s|^enable *=.*|enable = true|}' "$CHAIN_HOME/config/app.toml" 2>/dev/null || true
    sed -i '/^\[api\]/,/^\[/{s|^enabled-unsafe-cors *=.*|enabled-unsafe-cors = true|}' "$CHAIN_HOME/config/app.toml" 2>/dev/null || true

    # 1s block time for fast IBC tests.
    sed -i 's|^timeout_commit *=.*|timeout_commit = "1s"|' "$CHAIN_HOME/config/config.toml"
    sed -i 's|^timeout_propose *=.*|timeout_propose = "500ms"|' "$CHAIN_HOME/config/config.toml"

    # Re-run gentx so the validator self-delegation is signed with the new
    # chain_id. (The original gentx from the ignite template was signed under
    # chain-id="sparkdream" and would fail signature verification at init.)
    safe_rmtree "$CHAIN_HOME/config/gentx" || return 1
    mkdir -p "$CHAIN_HOME/config/gentx"
    "$BINARY" genesis gentx alice 1000000000uspark \
        --chain-id "$CHAIN_ID" \
        --home "$CHAIN_HOME" \
        --keyring-backend test \
        --moniker "$CHAIN_ID-validator" >/dev/null 2>&1
    "$BINARY" genesis collect-gentxs --home "$CHAIN_HOME" >/dev/null 2>&1
    "$BINARY" genesis validate-genesis --home "$CHAIN_HOME" >/dev/null
}

# ------------------------------------------------------------------
# Step 1: chain-a (default ports)
# ------------------------------------------------------------------
echo ""
echo "=== Step 1: Bootstrapping chain-a ($CHAIN_A_ID) ==="
clone_template "$CHAIN_A_HOME" "$CHAIN_A_ID" 0
echo "  chain-a ready at $CHAIN_A_HOME"

# ------------------------------------------------------------------
# Step 2: chain-b (offset ports)
# ------------------------------------------------------------------
echo ""
echo "=== Step 2: Bootstrapping chain-b ($CHAIN_B_ID) ==="
clone_template "$CHAIN_B_HOME" "$CHAIN_B_ID" 10000
echo "  chain-b ready at $CHAIN_B_HOME"

echo ""
echo "=================================================="
echo "  INITIALIZATION COMPLETE"
echo "=================================================="
echo ""
echo "  chain-a ($CHAIN_A_ID): $CHAIN_A_HOME"
echo "    RPC=26657  P2P=26656  gRPC=9090  LCD=1317"
echo "  chain-b ($CHAIN_B_ID): $CHAIN_B_HOME"
echo "    RPC=36657  P2P=36656  gRPC=9190  LCD=1417"
echo ""
echo "  Next: start_chains.sh"
echo ""
