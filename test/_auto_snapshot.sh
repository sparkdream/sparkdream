# Auto-snapshot helper, sourced by per-module run_all_tests.sh.
#
# When a module runner is invoked with no explicit `--save-setup`,
# `--restore-setup`, or `--no-auto-snapshot` flag, this helper:
#
#   - Restores from snapshots/post-setup when present and fresh
#     (i.e. the SHA-256 of setup_test_accounts.sh matches the snapshot's
#     recorded hash). Skips setup_test_accounts.sh in that case.
#   - Otherwise marks AUTO_SAVE_AFTER_SETUP=true so that the runner saves a
#     fresh snapshot once setup completes, then restarts the chain.
#
# It also auto-initializes and starts the chain on demand: if no chain is
# running when the runner is invoked in auto-mode, the helper re-inits and
# starts it so a single `bash run_all_tests.sh` invocation "just works"
# without requiring the user to start the chain first. The full-suite runner
# (test/run_all_tests.sh) keeps managing the chain itself; in that path the
# chain is already running so the auto-start is a no-op.
#
# The setup-script hash gate prevents stale snapshots from masking
# setup-script changes. To force a refresh manually, delete the snapshot dir
# or pass `--save-setup`.
#
# Required caller-supplied variables (set before sourcing):
#   SCRIPT_DIR  — the module's directory (where run_all_tests.sh lives)
#   BINARY      — the sparkdreamd binary name (e.g., "sparkdreamd")
#
# Optional caller-supplied variables:
#   SAVE_SETUP, RESTORE_SETUP, AUTO_SNAPSHOT, AUTO_SAVE_AFTER_SETUP, RUN_SETUP

if [ -z "${SCRIPT_DIR:-}" ]; then
    echo "ERROR: _auto_snapshot.sh requires SCRIPT_DIR to be set before sourcing" >&2
    return 1 2>/dev/null || exit 1
fi

# Shared safe-rmtree helper (CLAUDE.md forbids `rm -rf`).
# SCRIPT_DIR is the module dir (test/<module>/); the helper lives one up.
# shellcheck source=./_safe_rm.sh
source "$SCRIPT_DIR/../_safe_rm.sh"

# These remain settable by the caller — initialize only if unset.
: "${BINARY:=sparkdreamd}"
: "${AUTO_SNAPSHOT:=true}"
: "${AUTO_SAVE_AFTER_SETUP:=false}"
: "${SAVE_SETUP:=false}"
: "${RESTORE_SETUP:=false}"

_SNAPSHOT_PATH="$SCRIPT_DIR/snapshots/post-setup"
_SETUP_SCRIPT="$SCRIPT_DIR/setup_test_accounts.sh"
_SNAPSHOT_DATADIR="$SCRIPT_DIR/../snapshot_datadir.sh"
# Project root holds config.yml that ignite uses for chain init. Module dirs
# live at <project>/test/<module>/, so go up two levels.
_PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Re-initializes the chain home from config.yml and starts the chain in the
# background. Used when the runner is invoked in auto-mode but no chain is
# running. Returns 0 once `BINARY status` succeeds, 1 on failure.
#
# Honors $CHAIN_HOME if set (used by parallel runners); otherwise defaults to
# ~/.sparkdream. Note: ignite chain init writes to ~/.sparkdream regardless,
# so when $CHAIN_HOME != ~/.sparkdream, we move the result post-init. Any
# pre-existing ~/.sparkdream is preserved during the swap.
_auto_init_and_start_chain() {
    local _ch="${CHAIN_HOME:-$HOME/.sparkdream}"
    echo "  Stopping any leftover sparkdreamd processes for $_ch..."
    # Scope kills to processes running against this chain home, so concurrent
    # parallel suites (running under e2e/parallel-PID/suite-N/home/) are
    # not collateral damage. Any combination of "--home X" arg ordering is
    # covered by the two patterns; we no longer fall back to a blanket
    # `pkill sparkdreamd` because it would also kill parallel-suite chains.
    pkill -f "sparkdreamd .*--home $_ch" 2>/dev/null || true
    pkill -f "sparkdreamd.* start.*--home $_ch" 2>/dev/null || true
    if [ "$_ch" = "$HOME/.sparkdream" ]; then
        # Default-home only: also kill any orphaned sparkdreamd that
        # implicitly defaults to ~/.sparkdream (lacks --home in argv). We
        # match the explicit "start" subcommand instead of bare
        # "sparkdreamd" so we don't sweep up unrelated CLI invocations
        # (like `sparkdreamd query …`) on a busy box.
        pkill -f "sparkdreamd start" 2>/dev/null || true
    fi
    sleep 2

    # Clean the data dir so we start from genesis. The full-suite runner does
    # the same between modules, so we match its semantics for standalone runs.
    # safe_rmtree refuses to delete /, $HOME, empty, or non-absolute paths
    # — protects against a misconfigured / unset CHAIN_HOME.
    if [ -d "$_ch" ]; then
        echo "  Removing existing chain data at $_ch..."
        safe_rmtree "$_ch" || {
            echo "  ERROR: failed to remove $_ch — aborting auto-init"
            return 1
        }
    fi

    echo "  Initializing chain (ignite chain init -y --build.tags testparams)..."
    # ignite always writes to ~/.sparkdream — back up an unrelated existing one
    # if we'll be moving the result to a different $_ch.
    local _backup=""
    if [ "$_ch" != "$HOME/.sparkdream" ] && [ -e "$HOME/.sparkdream" ]; then
        _backup="$HOME/.sparkdream.auto-snap-bak.$$"
        mv "$HOME/.sparkdream" "$_backup"
    fi
    # Per-chain-home log paths so two concurrent auto-snapshot runs don't
    # clobber each other's output (we'd never get to read both).
    local _ch_slug
    _ch_slug=$(echo "$_ch" | tr '/' '_' | tr -dc 'A-Za-z0-9_.-' | head -c 60)
    local _init_log="/tmp/chain_auto_init.${_ch_slug}.log"
    local _start_log="/tmp/chain_auto_start.${_ch_slug}.log"
    if ! (cd "$_PROJECT_DIR" && ignite chain init -y --build.tags testparams > "$_init_log" 2>&1); then
        echo "  ERROR: ignite chain init failed — see $_init_log"
        tail -20 "$_init_log" >&2 2>/dev/null || true
        [ -n "$_backup" ] && mv "$_backup" "$HOME/.sparkdream"
        return 1
    fi
    if [ "$_ch" != "$HOME/.sparkdream" ]; then
        mkdir -p "$(dirname "$_ch")"
        mv "$HOME/.sparkdream" "$_ch"
        [ -n "$_backup" ] && mv "$_backup" "$HOME/.sparkdream"
    fi

    echo "  Starting chain in background (home=$_ch)..."
    "$BINARY" start --home "$_ch" > "$_start_log" 2>&1 &

    local max=60
    local attempt=0
    while [ $attempt -lt $max ]; do
        # Always pass --home so we hit the right chain when CHAIN_HOME is
        # set. Without --home, a stale client.toml or default home would
        # make us check the wrong RPC endpoint.
        if "$BINARY" status --home "$_ch" &> /dev/null; then
            local height
            height=$("$BINARY" status --home "$_ch" 2>&1 | jq -r '.sync_info.latest_block_height' 2>/dev/null)
            echo "  Chain is up (block height: ${height:-?})"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done

    echo "  ERROR: chain did not start within ${max}s — see $_start_log"
    tail -20 "$_start_log" >&2 2>/dev/null || true
    return 1
}

# Returns 0 if a snapshot exists and its setup_hash matches the current
# setup_test_accounts.sh content. Returns 1 otherwise.
_snapshot_is_fresh() {
    [ -f "$_SNAPSHOT_PATH/restore.sh" ] || return 1
    [ -f "$_SETUP_SCRIPT" ] || return 0  # no setup script → freshness vacuously OK
    [ -f "$_SNAPSHOT_PATH/setup_hash" ] || return 1

    local current_hash stored_hash
    current_hash=$(sha256sum "$_SETUP_SCRIPT" 2>/dev/null | cut -d' ' -f1)
    stored_hash=$(cat "$_SNAPSHOT_PATH/setup_hash" 2>/dev/null)
    [ -n "$current_hash" ] || return 1
    [ "$current_hash" = "$stored_hash" ]
}

# Called once after arg parsing, before the existing restore-setup branch.
# Decides whether to auto-restore or auto-save based on snapshot presence,
# and auto-starts the chain when needed for the save path.
auto_snapshot_pre() {
    if [ "$AUTO_SNAPSHOT" != "true" ]; then
        return 0
    fi
    if [ "$SAVE_SETUP" = "true" ] || [ "$RESTORE_SETUP" = "true" ]; then
        return 0
    fi

    if _snapshot_is_fresh; then
        echo "==========================================================================="
        echo "AUTO-SNAPSHOT: Reusing existing snapshot (setup_test_accounts.sh unchanged)"
        echo "==========================================================================="
        echo "  Snapshot: $_SNAPSHOT_PATH"
        echo "  (Pass --no-auto-snapshot or delete the snapshot to force a refresh.)"
        echo ""
        # The runner's existing restore-setup branch will stop the chain,
        # restore the snapshot, and restart the chain — no chain auto-start
        # needed here.
        RESTORE_SETUP=true
        RUN_SETUP=false
    else
        if [ -f "$_SNAPSHOT_PATH/restore.sh" ]; then
            echo "AUTO-SNAPSHOT: setup_test_accounts.sh changed since last snapshot — will refresh."
        else
            echo "AUTO-SNAPSHOT: no snapshot found at $_SNAPSHOT_PATH — will create one after setup."
        fi
        AUTO_SAVE_AFTER_SETUP=true

        # Setup must run against a clean genesis state. If we don't reinit
        # here, an already-running chain may have stale state from a prior
        # run (e.g. Alice's DREAM balance depleted by earlier tests, leaving
        # her unable to fund sentinel1's bond). The full-suite runner re-inits
        # the chain between modules for the same reason; standalone runs need
        # the same isolation. Always re-init when AUTO_SAVE_AFTER_SETUP fires.
        local _ch="${CHAIN_HOME:-$HOME/.sparkdream}"
        if [ "$_ch" = "$HOME/.sparkdream" ] && [ -d "$_ch" ] && [ -f "$_ch/data/priv_validator_state.json" ]; then
            # Standalone run against the user's main chain home: the
            # imminent re-init will destroy any live state. If we have a
            # tty, give the user a 5-second window to Ctrl-C; otherwise
            # honor a non-interactive opt-out via SPARKDREAM_AUTO_SNAPSHOT_FORCE=1.
            if [ -t 0 ] && [ -t 2 ] && [ "${SPARKDREAM_AUTO_SNAPSHOT_FORCE:-}" != "1" ]; then
                echo "AUTO-SNAPSHOT: about to re-initialize $_ch (this will discard"
                echo "AUTO-SNAPSHOT: any chain state currently there). Ctrl-C within 5s to abort,"
                echo "AUTO-SNAPSHOT: or set SPARKDREAM_AUTO_SNAPSHOT_FORCE=1 to skip this prompt."
                local i
                for i in 5 4 3 2 1; do
                    printf "AUTO-SNAPSHOT:   continuing in %ds...\r" "$i" >&2
                    sleep 1
                done
                printf "                                          \r" >&2
            fi
        fi
        echo "AUTO-SNAPSHOT: re-initializing chain to a clean genesis state..."
        _auto_init_and_start_chain || {
            echo "AUTO-SNAPSHOT: failed to auto-start chain; aborting."
            exit 1
        }
    fi
}

# Called after setup_test_accounts.sh has run, but only honors itself when
# AUTO_SAVE_AFTER_SETUP=true (set by auto_snapshot_pre).
# Saves the snapshot, records the setup-script hash, and restarts the chain
# (snapshot_datadir.sh stops the chain to take a consistent copy).
#
# If the setup itself reported failures (TESTS_FAILED > 0 — the runner's
# run_test increments this when setup_test_accounts.sh exits non-zero), the
# save is skipped so we never persist a half-funded post-setup state. The
# next run will see "no fresh snapshot" and re-init the chain from genesis.
auto_snapshot_post() {
    if [ "$AUTO_SAVE_AFTER_SETUP" != "true" ]; then
        return 0
    fi

    if [ "${TESTS_FAILED:-0}" -gt 0 ]; then
        echo ""
        echo "==========================================================================="
        echo "AUTO-SNAPSHOT: setup reported $TESTS_FAILED failure(s) — NOT saving snapshot"
        echo "==========================================================================="
        echo "  Aborting save so a corrupted post-setup state isn't persisted."
        echo "  Inspect the setup output above, fix the root cause, and rerun."
        return 0
    fi

    echo ""
    echo "==========================================================================="
    echo "AUTO-SNAPSHOT: Saving setup state for future runs..."
    echo "==========================================================================="

    if [ ! -f "$_SNAPSHOT_DATADIR" ]; then
        echo "  WARNING: snapshot_datadir.sh not found at $_SNAPSHOT_DATADIR"
        echo "  Continuing without auto-saving snapshot."
        return 0
    fi

    bash "$_SNAPSHOT_DATADIR" post-setup "$SCRIPT_DIR/snapshots"
    local rc=$?
    if [ $rc -ne 0 ]; then
        echo "  WARNING: snapshot save returned exit code $rc — continuing anyway."
    fi

    # Record the setup-script hash so future runs can detect script changes.
    if [ -f "$_SETUP_SCRIPT" ] && [ -d "$_SNAPSHOT_PATH" ]; then
        sha256sum "$_SETUP_SCRIPT" 2>/dev/null | cut -d' ' -f1 > "$_SNAPSHOT_PATH/setup_hash"
    fi

    # snapshot_datadir.sh stops the chain to produce a consistent copy.
    # Restart it so the test run can continue.
    echo ""
    local _ch="${CHAIN_HOME:-$HOME/.sparkdream}"
    local _ch_slug
    _ch_slug=$(echo "$_ch" | tr '/' '_' | tr -dc 'A-Za-z0-9_.-' | head -c 60)
    local _after_log="/tmp/chain_after_snapshot.${_ch_slug}.log"
    echo "Restarting chain after snapshot save (home=$_ch)..."
    "$BINARY" start --home "$_ch" > "$_after_log" 2>&1 &

    local max_attempts=30
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        # --home so we always check the chain we just started, not whatever
        # client.toml's default points at.
        if "$BINARY" status --home "$_ch" &> /dev/null; then
            local height
            height=$("$BINARY" status --home "$_ch" 2>&1 | jq -r '.sync_info.latest_block_height' 2>/dev/null)
            echo "Chain restarted (block height: ${height:-unknown})"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done

    echo "  WARNING: chain did not respond after snapshot save."
    echo "  Check $_after_log for details."
    return 1
}
