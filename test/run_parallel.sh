#!/bin/bash
# ============================================================================
# PARALLEL E2E TEST RUNNER
# ============================================================================
# Runs N module test suites concurrently with isolated chain homes and per-
# suite port offsets. Bypasses the per-module run_all_tests.sh entirely (which
# hardcodes ~/.sparkdream and uses global pkill) and instead invokes
# setup_test_accounts.sh + each *_test.sh directly inside an isolated suite.
#
# Usage:
#   bash test/run_parallel.sh [--concurrency K | --full-parallel] [<module> <module> ...]
#   bash test/run_parallel.sh --stop                # halt any active parallel run
#   bash test/run_parallel.sh --status [PATH|PID]   # report completion + PASS/FAIL counts
#   bash test/run_parallel.sh --clean-only          # wipe inactive parallel-* and exit
#   bash test/run_parallel.sh --clean-stale [DAYS]  # prune inactive runs >DAYS old (default 7)
#
# With no modules, auto-discovers from MODULE_ORDER in test/run_all_tests.sh
# (or by directory scan if that variable can't be parsed). Two pseudo-modules
# are appended automatically:
#   legacy     — runs the ecosystem/split/gov scripts that test/run_all_tests.sh
#                runs in its "legacy" section (no test/<name>/ dir).
#   multichain — delegates to test/federation/multichain/run_all_multichain_tests.sh,
#                which provisions its own chain-a/chain-b + hermes relayer.
#                Auto-included only when `hermes` is on PATH. ALWAYS runs in
#                its own batch (its `ignite chain init` walks the project tree
#                and trips over LevelDB temp files in concurrent suites' homes).
#
# Concurrency defaults to 6 (safe for typical dev machines). Pass
# --concurrency K to override the cap, or --full-parallel to disable it
# entirely and launch every suite at once.
#
# Disk management: each run leaves ~4 GB of artifacts in e2e/parallel-<PID>/
# (12 chain LevelDBs + per-module logs). The runner does NOT auto-clear
# these — they're the post-mortem evidence for failed runs. Three opt-in
# knobs control cleanup:
#   --clean              wipe inactive parallel-* dirs BEFORE this run
#   --keep N             after this run, retain only the N newest run dirs
#   --clean-stale [DAYS] standalone: prune inactive dirs older than DAYS
# All three skip dirs whose embedded PID is still alive — concurrent runs
# against the same E2E_ROOT can't be clobbered.
#
# Within each suite, tests run in the order declared by the module's
# run_all_tests.sh (not alphabetical) — required for modules with order
# dependencies like x/reveal where cancel_reject leaves state that breaks
# later propose-heavy tests.
#
# Examples:
#   bash test/run_parallel.sh                          # all discovered, 6 at a time
#   bash test/run_parallel.sh --full-parallel          # all, no cap
#   bash test/run_parallel.sh --concurrency 2          # all, 2 at a time
#   bash test/run_parallel.sh blog forum               # just two
#   bash test/run_parallel.sh blog forum collect
#   bash test/run_parallel.sh --rebuild blog forum collect rep
#   bash test/run_parallel.sh --clean                  # wipe old runs, then run all
#   bash test/run_parallel.sh --keep 3                 # run, then keep only 3 newest
#   bash test/run_parallel.sh --clean --keep 3         # wipe + rolling window of 3
#   bash test/run_parallel.sh --stop                   # reap any active run
#   bash test/run_parallel.sh --clean-only             # wipe inactive runs, exit
#   bash test/run_parallel.sh --clean-stale 14         # prune runs >14 days old, exit
#
# Port allocation:
#   Each suite gets a +200 offset on RPC/P2P/gRPC/gRPC-Web/API. Suite 1
#   uses (26857, 26856, 9290, 9291, 1517); suite 2 uses (27057, 27056,
#   9490, 9491, 1717); etc. The 200-port stride is small enough that 14
#   suites occupy 26857..29457 — comfortably below the multichain
#   pseudo-module's chain-b (36657/36656/9190/1417), so a multichain
#   suite can run concurrently with normal suites without port collision.
#   Theoretical max ~190 suites before TCP ports wrap; RAM (~350 MB per
#   chain) and CPU contention cap usable parallelism at ~8-10 on a 32 GB
#   machine.
#
# Prerequisites:
#   - sparkdreamd built with testparams (auto-built by default; see --no-build)
#   - jq, curl on PATH
#   - No other sparkdreamd already binding the suite ports
#
# Output:
#   <project>/e2e/parallel-<PID>/         — per-run isolated workdir
#     ├── suite-1-<module>/home/          — chain home for suite 1 (LevelDB, keyring, configs)
#     ├── suite-1-<module>/bin/sparkdreamd — PATH-shadowed wrapper that injects --home
#     ├── suite-2-<module>/...
#     ├── ... (one per suite)
#     └── <module>.log                    — full test output (live + final), one per module
#   <project>/e2e/latest                  — symlink → most recent parallel-<PID>
#
# Override the workdir root with $SPARKDREAM_E2E_DIR (defaults to <project>/e2e).
# The /e2e/ directory is gitignored.
#
# Snapshot reuse:
#   For each suite, if test/<module>/snapshots/post-setup/ exists AND its
#   recorded setup_hash matches the current SHA-256 of setup_test_accounts.sh,
#   the chain home is initialized by `cp -r`'ing the snapshot data instead of
#   running ignite chain init + setup_test_accounts.sh. This typically saves
#   30-90s per suite. The snapshot is created by the sequential workflow
#   (e.g. `bash test/blog/run_all_tests.sh --save-setup`); the parallel runner
#   only consumes snapshots, never produces them.
#
# Caveats:
#   - Tests run in ALPHABETICAL order, not the order in run_all_tests.sh.
#     If a suite has order dependencies, run individual *_test.sh manually.
#   - The suites use chain-id "sparkdream" (same as the default). They never
#     peer with each other because they're isolated home dirs with separate
#     genesis files and no seeds configured.
#   - Snapshot restoration uses cp -r directly rather than the snapshot's
#     bundled restore.sh — the bundled script does its own pkill/backup
#     dance which is irrelevant when restoring into a fresh suite home.
# ============================================================================

set -u
set -o pipefail
# We intentionally do NOT enable `set -e`: this script orchestrates many
# best-effort cleanup steps (kill_tree, pgrep, sed -i on optional configs,
# tail in subshells). A blanket errexit would cascade-abort on benign
# non-zero returns. Failure handling is explicit: each suite returns its
# own rc, and the EXIT trap reaps everything regardless of what failed.

# ----------------------------------------------------------------------------
# Resolve repo paths early (needed by discover_modules + arg parsing)
# ----------------------------------------------------------------------------
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$PROJECT_DIR/test"

# Shared timing helpers (timing_now_epoch, timing_format_duration, ...).
source "$TEST_DIR/_timing.sh"
# Shared safe-rmtree helper (CLAUDE.md forbids `rm -rf`).
source "$TEST_DIR/_safe_rm.sh"

# Workdir root and PID file used by the cleanup trap and --stop flag.
# (Set early so --stop can find the running run_parallel.sh before any other
# initialization runs.)
E2E_ROOT="${SPARKDREAM_E2E_DIR:-$PROJECT_DIR/e2e}"
PID_FILE="$E2E_ROOT/run_parallel.pid"

# ----------------------------------------------------------------------------
# kill_tree: SIGTERM/KILL a process AND all its descendants, post-order.
#
# Solves the orphan-subshell problem: when run_parallel.sh's parent dies, its
# backgrounded run_suite subshells get reparented to init and keep iterating
# their `for test_script in *_test.sh; do bash "$t"; done` loop, spawning
# more sparkdreamd children faster than naive cleanup can keep up. Killing
# the entire process tree atomically (leaves first, parent last) avoids the
# race because no parent can spawn another child after we've collected its
# child list and killed the children.
# ----------------------------------------------------------------------------
kill_tree() {
    local pid="$1"
    local sig="${2:-TERM}"
    [ -z "$pid" ] && return 0
    # Collect children BEFORE killing parent — once parent dies, children
    # reparent to init and we'd lose track of them.
    local children
    children=$(pgrep -P "$pid" 2>/dev/null)
    local child
    for child in $children; do
        kill_tree "$child" "$sig"
    done
    kill -"$sig" "$pid" 2>/dev/null || true
}

# Defensive sweep: kill any sparkdreamd whose --home points at e2e/parallel-*.
# Used by --stop to mop up orphans from older buggy runs even when the
# PID file is missing or stale.
kill_orphan_parallel_chains() {
    local count=0
    local pid cmd
    while read -r pid; do
        [ -z "$pid" ] && continue
        cmd=$(tr '\0' ' ' < /proc/"$pid"/cmdline 2>/dev/null)
        if echo "$cmd" | grep -q '/e2e/parallel-'; then
            kill -KILL "$pid" 2>/dev/null && count=$((count + 1))
        fi
    done < <(pgrep -x sparkdreamd 2>/dev/null)
    echo "$count"
}

# ----------------------------------------------------------------------------
# Disk-management helpers for the e2e/ workdir.
#
# Per-run dirs (e2e/parallel-<PID>/) hold full chain LevelDBs (~350 MB each)
# plus per-module logs. A 12-suite run leaves ~4 GB of artifacts. After
# several runs disk usage grows quickly. These helpers let the user prune
# explicitly without auto-clearing on every start (which would destroy the
# post-mortem evidence the dir is FOR).
#
# The four helpers below all share three safety properties:
#   - Operate ONLY on $E2E_ROOT/parallel-* (path is checked, not just globbed)
#   - Skip dirs whose embedded PID is still alive (defends against clobbering
#     a concurrent run launched against the same E2E_ROOT)
#   - Use `find ... -delete` rather than `rm -rf` (per CLAUDE.md convention)
# ----------------------------------------------------------------------------

# Remove a single parallel-<PID> directory tree. Refuses to operate outside
# $E2E_ROOT/parallel-* and on dirs whose PID is still alive.
e2e_remove_run_dir() {
    local d="$1"
    [ -d "$d" ] || return 0
    case "$d" in
        "$E2E_ROOT"/parallel-*) ;;
        *) echo "  refusing to delete: $d (not under $E2E_ROOT/parallel-*)" >&2; return 1 ;;
    esac
    local pid="${d##*-}"
    if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
        echo "  refusing to delete $d (PID $pid is still running)" >&2
        return 1
    fi
    # Depth-first delete (files before parent dirs). -depth makes find walk
    # children before the parent, so each rmdir succeeds.
    find "$d" -depth -delete 2>/dev/null
    return 0
}

# Print one-line disk-usage summary for $E2E_ROOT, plus a prompt about the
# cleanup flags. Silent if the dir doesn't exist or has no parallel-* subdirs.
e2e_disk_summary() {
    [ -d "$E2E_ROOT" ] || return 0
    local runs total
    runs=$(find "$E2E_ROOT" -maxdepth 1 -name 'parallel-*' -type d 2>/dev/null | wc -l)
    [ "${runs:-0}" -gt 0 ] || return 0
    total=$(du -sh "$E2E_ROOT" 2>/dev/null | awk '{print $1}')
    printf "e2e/ currently uses %s across %d run(s) at %s.\n" \
        "${total:-?}" "$runs" "$E2E_ROOT"
    printf "  (--clean wipes inactive runs before this one; --keep N retains only the N newest at end)\n"
}

# Remove ALL inactive parallel-* dirs. Refuses if any has a live PID
# (use --stop first to halt active runs).
e2e_clean_all() {
    [ -d "$E2E_ROOT" ] || { echo "  e2e/ doesn't exist — nothing to clean."; return 0; }
    local d pid live=0 stale=0
    for d in "$E2E_ROOT"/parallel-*; do
        [ -d "$d" ] || continue
        pid="${d##*-}"
        if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
            live=$((live + 1))
            echo "  skipping $d (PID $pid is still running)"
        else
            stale=$((stale + 1))
        fi
    done
    if [ "$live" -gt 0 ]; then
        echo "ERROR: $live active run(s) detected. Stop them first with: bash $0 --stop" >&2
        return 1
    fi
    if [ "$stale" -eq 0 ]; then
        echo "  e2e/ already clean (no parallel-* dirs)."
        return 0
    fi
    echo "  removing $stale inactive parallel-* dir(s)..."
    local removed=0
    for d in "$E2E_ROOT"/parallel-*; do
        [ -d "$d" ] || continue
        e2e_remove_run_dir "$d" && removed=$((removed + 1))
    done
    # Stale latest symlink and PID file are also fair game now.
    [ -L "$E2E_ROOT/latest" ] && rm -f "$E2E_ROOT/latest"
    [ -f "$E2E_ROOT/run_parallel.pid" ] && rm -f "$E2E_ROOT/run_parallel.pid"
    echo "  pruned $removed run(s)."
    return 0
}

# Remove parallel-* dirs that are BOTH inactive AND older than $1 days.
# Default: 7 days. Live-PID dirs are always preserved.
e2e_clean_stale() {
    local days="${1:-7}"
    if ! [[ "$days" =~ ^[0-9]+$ ]]; then
        echo "ERROR: --clean-stale expects a non-negative integer (got '$days')" >&2
        return 1
    fi
    [ -d "$E2E_ROOT" ] || { echo "  e2e/ doesn't exist — nothing to clean."; return 0; }
    local now cutoff d pid mtime removed=0 kept=0
    now=$(date +%s)
    cutoff=$((now - days * 86400))
    for d in "$E2E_ROOT"/parallel-*; do
        [ -d "$d" ] || continue
        pid="${d##*-}"
        if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
            kept=$((kept + 1))
            continue
        fi
        mtime=$(stat -c %Y "$d" 2>/dev/null || echo 0)
        if [ "$mtime" -lt "$cutoff" ]; then
            e2e_remove_run_dir "$d" && removed=$((removed + 1))
        else
            kept=$((kept + 1))
        fi
    done
    echo "  pruned $removed inactive run(s) older than $days day(s); kept $kept."
}

# Keep only the N most recent parallel-* dirs (by mtime). Always preserves
# $2 ($PARALLEL_DIR for the current run) regardless of mtime ranking, and
# never touches dirs with live PIDs.
e2e_keep_n() {
    local n="$1"
    local self="${2:-}"
    if ! [[ "$n" =~ ^[0-9]+$ ]] || [ "$n" -lt 1 ]; then
        echo "ERROR: --keep expects a positive integer (got '$n')" >&2
        return 1
    fi
    [ -d "$E2E_ROOT" ] || return 0

    # List parallel-* dirs newest first by mtime.
    local -a all=()
    while IFS= read -r line; do
        [ -n "$line" ] && all+=("$line")
    done < <(find "$E2E_ROOT" -maxdepth 1 -name 'parallel-*' -type d -printf '%T@\t%p\n' 2>/dev/null \
             | sort -rn | cut -f2)

    local total=${#all[@]}
    if [ "$total" -le "$n" ]; then
        echo "  --keep $n: only $total run(s) present, nothing to prune."
        return 0
    fi

    # Walk newest -> oldest; keep first N (always counting current run as
    # one of the kept), skip live-PID dirs, prune the rest.
    local kept=0 removed=0 skipped=0 pid d
    for d in "${all[@]}"; do
        if [ "$d" = "$self" ]; then
            kept=$((kept + 1))
            continue
        fi
        pid="${d##*-}"
        if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
            skipped=$((skipped + 1))
            continue
        fi
        if [ "$kept" -lt "$n" ]; then
            kept=$((kept + 1))
            continue
        fi
        e2e_remove_run_dir "$d" && removed=$((removed + 1))
    done
    if [ "$skipped" -gt 0 ]; then
        printf "  --keep %d: kept %d run(s), pruned %d, skipped %d active.\n" \
            "$n" "$kept" "$removed" "$skipped"
    else
        printf "  --keep %d: kept %d run(s), pruned %d.\n" "$n" "$kept" "$removed"
    fi
}

# ----------------------------------------------------------------------------
# e2e_status_report: snapshot of an in-progress or completed parallel run —
# overall completion + per-suite PASS/FAIL counts. Used by --status.
#
# Resolution order for which run to report on:
#   1. Explicit arg (run dir path or bare PID under $E2E_ROOT/parallel-<PID>/)
#   2. Active run referenced by $PID_FILE (if PID is alive)
#   3. The `latest` symlink under $E2E_ROOT/
#   4. Newest parallel-* dir by mtime (last resort)
#
# Per-suite state is derived from the per-module log:
#   DONE     - log contains "[<mod>] === SUITE DONE: P passed, F failed ==="
#   RUNNING  - log non-empty, no SUITE DONE line, chain process alive
#   ABORTED  - log non-empty, no SUITE DONE line, chain not alive (run killed
#              mid-suite — failure logs preserved for post-mortem)
#   PENDING  - log empty (suite hasn't been launched yet; only seen during
#              multi-batch runs while batch 2+ is still queued)
#
# PASS/FAIL counts come from "[<mod>] PASS: <test>" / "[<mod>] FAIL: <test>"
# lines emitted inside run_suite. Exit code: 0 if a run was found, 1 if not.
# ----------------------------------------------------------------------------
e2e_status_report() {
    local target="${1:-}"
    local run_dir=""

    if [ -n "$target" ]; then
        # Allow the user to pass either a full path or a bare PID.
        if [ -d "$target" ]; then
            run_dir="$target"
        elif [[ "$target" =~ ^[0-9]+$ ]] && [ -d "$E2E_ROOT/parallel-$target" ]; then
            run_dir="$E2E_ROOT/parallel-$target"
        else
            echo "ERROR: --status: '$target' is neither a run dir nor a known PID under $E2E_ROOT/" >&2
            return 1
        fi
    else
        # Prefer an actively-tracked run if there is one.
        if [ -f "$PID_FILE" ]; then
            local active
            active=$(cat "$PID_FILE" 2>/dev/null)
            if [ -n "$active" ] && kill -0 "$active" 2>/dev/null \
               && [ -d "$E2E_ROOT/parallel-$active" ]; then
                run_dir="$E2E_ROOT/parallel-$active"
            fi
        fi
        # Fall back to the `latest` symlink.
        if [ -z "$run_dir" ] && [ -L "$E2E_ROOT/latest" ]; then
            local resolved
            resolved=$(readlink -f "$E2E_ROOT/latest" 2>/dev/null)
            [ -d "$resolved" ] && run_dir="$resolved"
        fi
        # Last resort: newest parallel-* by mtime.
        if [ -z "$run_dir" ] && [ -d "$E2E_ROOT" ]; then
            run_dir=$(find "$E2E_ROOT" -maxdepth 1 -name 'parallel-*' -type d -printf '%T@\t%p\n' 2>/dev/null \
                      | sort -rn | head -1 | cut -f2)
        fi
    fi

    if [ -z "$run_dir" ] || [ ! -d "$run_dir" ]; then
        echo "No parallel run found under $E2E_ROOT/."
        echo "  (looked at active PID file, latest/ symlink, and newest parallel-*)"
        return 1
    fi

    local run_pid="${run_dir##*-}"
    local run_alive="no"
    if [[ "$run_pid" =~ ^[0-9]+$ ]] && kill -0 "$run_pid" 2>/dev/null; then
        run_alive="yes"
    fi

    # Snapshot the live sparkdreamd command-lines once so per-suite checks are
    # cheap (one ps walk total instead of one per suite).
    local ps_snapshot
    ps_snapshot=$(ps -eo pid,args 2>/dev/null | awk '/sparkdreamd/ && !/awk|grep/')

    # Walk suite-* subdirs in numeric order (suite-1-foo before suite-10-bar).
    # -printf '%f\n' yields just the basename so the sort key is unambiguous
    # even if $E2E_ROOT itself contains hyphens.
    local -a suite_dirs=()
    while IFS= read -r line; do
        [ -n "$line" ] && suite_dirs+=("$run_dir/$line")
    done < <(find "$run_dir" -maxdepth 1 -mindepth 1 -name 'suite-*' -type d -printf '%f\n' 2>/dev/null \
             | sort -t- -k2,2n)

    if [ ${#suite_dirs[@]} -eq 0 ]; then
        echo "Run dir $run_dir has no suite-* subdirectories yet."
        return 1
    fi

    # Aggregates and per-suite rows.
    local total_pass=0 total_fail=0
    local n_done=0 n_running=0 n_pending=0 n_aborted=0
    local -a rows=()

    local d base idx mod log
    for d in "${suite_dirs[@]}"; do
        base=$(basename "$d")
        # Format: suite-<idx>-<module>; module may contain hyphens, so use
        # cut with a trailing range.
        idx=$(echo "$base" | cut -d- -f2)
        mod=$(echo "$base" | cut -d- -f3-)
        log="$run_dir/$mod.log"

        local pass=0 fail=0 state="PENDING" current="" done_line=""
        if [ -s "$log" ]; then
            done_line=$(grep -E "^\[$mod\] === SUITE DONE:" "$log" 2>/dev/null | tail -1)
            if [ -n "$done_line" ]; then
                # Authoritative count comes from the SUITE DONE summary —
                # the multichain branch (and a few error paths) emit only
                # the summary without per-test PASS:/FAIL: lines, so
                # counting those alone would under-report failures.
                pass=$(echo "$done_line" | sed -E 's/.*SUITE DONE: ([0-9]+) passed.*/\1/')
                fail=$(echo "$done_line" | sed -E 's/.*([0-9]+) passed, ([0-9]+) failed.*/\2/')
                [[ "$pass" =~ ^[0-9]+$ ]] || pass=0
                [[ "$fail" =~ ^[0-9]+$ ]] || fail=0
                state="DONE"
                n_done=$((n_done + 1))
            else
                # Suite still in flight — count PASS/FAIL lines as they appear.
                # grep -c emits "0\n" with rc=1 when no match; the count is
                # correct, the rc is what we ignore.
                pass=$(grep -c "^\[$mod\] PASS:" "$log" 2>/dev/null) || pass=0
                fail=$(grep -c "^\[$mod\] FAIL:" "$log" 2>/dev/null) || fail=0
                if echo "$ps_snapshot" | grep -qF "$d/home"; then
                    state="RUNNING"
                    n_running=$((n_running + 1))
                else
                    state="ABORTED"
                    n_aborted=$((n_aborted + 1))
                fi
                # Last "[<mod>] --- <test_name> ---" line shows what's
                # currently in flight (or the last test before abort).
                current=$(grep -E "^\[$mod\] --- " "$log" 2>/dev/null | tail -1 \
                          | sed -E "s/^\[$mod\] --- (.*) ---$/\1/")
            fi
        else
            n_pending=$((n_pending + 1))
        fi

        total_pass=$((total_pass + pass))
        total_fail=$((total_fail + fail))

        local extra=""
        if [ -n "$current" ]; then
            extra="  (in: $current)"
        fi
        rows+=("$(printf '  Suite %-2s [%-7s] pass=%-3d fail=%-3d  %-12s%s' \
            "$idx" "$state" "$pass" "$fail" "$mod" "$extra")")
    done

    local n_total=${#suite_dirs[@]}
    local overall_state
    if [ "$run_alive" = "yes" ]; then
        overall_state="IN PROGRESS"
    elif [ "$n_done" -eq "$n_total" ]; then
        if [ "$total_fail" -gt 0 ]; then
            overall_state="COMPLETED (with failures)"
        else
            overall_state="COMPLETED (all green)"
        fi
    elif [ "$n_aborted" -gt 0 ] || [ "$n_pending" -gt 0 ] || [ "$n_running" -gt 0 ]; then
        overall_state="ENDED (incomplete — process killed or crashed)"
    else
        overall_state="UNKNOWN"
    fi

    echo "================================================================"
    echo "PARALLEL E2E RUN STATUS"
    echo "================================================================"
    printf "Run dir : %s\n" "$run_dir"
    printf "Run PID : %s (alive=%s)\n" "$run_pid" "$run_alive"
    printf "State   : %s\n" "$overall_state"
    printf "Suites  : %d total — %d done, %d running, %d pending, %d aborted\n" \
        "$n_total" "$n_done" "$n_running" "$n_pending" "$n_aborted"
    printf "Tests   : %d passed, %d failed (across all suites so far)\n" \
        "$total_pass" "$total_fail"
    echo "----------------------------------------------------------------"
    local r
    for r in "${rows[@]}"; do
        echo "$r"
    done
    echo "================================================================"
    return 0
}

# ----------------------------------------------------------------------------
# Auto-discover module test suites.
#
# Primary source: the MODULE_ORDER variable in test/run_all_tests.sh — that's
# the master runner's canonical list, so reading it here keeps the two
# runners in sync automatically (e.g. when a new module is added).
#
# Fallback (when MODULE_ORDER can't be parsed): directory scan that picks
# every test/<name>/ with both run_all_tests.sh and at least one *_test.sh.
#
# Output preserves MODULE_ORDER as written (deduped, blank-stripped) so that
# bin-packing — putting the longest suites at the head of the list so they
# land in batch 1 while the short ones fill batch 2 — is controlled by the
# author of test/run_all_tests.sh. Falling back to a directory scan still
# sorts alphabetically since we have no duration signal there.
# ----------------------------------------------------------------------------
discover_modules() {
    local master="$TEST_DIR/run_all_tests.sh"
    local canonical=""
    if [ -f "$master" ]; then
        # Match lines like:  MODULE_ORDER="commons name futarchy ..."
        canonical=$(grep -E '^MODULE_ORDER=' "$master" 2>/dev/null \
                    | head -1 \
                    | sed -E 's/^MODULE_ORDER=//; s/^"//; s/".*$//' \
                    | tr ' ' '\n' \
                    | grep -vE '^$')
    fi

    # Pseudo-modules — special-cased in run_suite. Listed AFTER the canonical
    # module list so they land in batch 2 (smaller surface area, less likely
    # to be the wall-clock bottleneck). `multichain` is only included if
    # hermes is on PATH; otherwise its tests can't run and we'd just waste
    # a suite slot reporting that. See is_pseudo_module() / module_kind().
    local extras="legacy"
    if command -v hermes >/dev/null 2>&1; then
        extras+=$'\nmultichain'
    fi

    if [ -n "$canonical" ]; then
        # awk dedup that preserves first-seen order (vs `sort -u` which
        # would alphabetize away the bin-packing intent).
        printf '%s\n%s\n' "$canonical" "$extras" | awk 'NF && !seen[$0]++'
        return 0
    fi

    # Fallback: directory scan (no duration signal, alphabetical is fine)
    local d name
    {
        for d in "$TEST_DIR"/*/; do
            name=$(basename "${d%/}")
            [ -f "$TEST_DIR/$name/run_all_tests.sh" ] || continue
            compgen -G "$TEST_DIR/$name/*_test.sh" > /dev/null || continue
            echo "$name"
        done | sort -u
        printf '%s\n' "$extras"
    } | awk 'NF && !seen[$0]++'
}

# ----------------------------------------------------------------------------
# Pseudo-module classifier. Returns "normal" / "legacy" / "multichain" via
# stdout. Pseudo-modules don't have a test/<name>/ directory; they hook into
# run_suite via dedicated branches:
#
#   legacy:     standard chain, but runs the ecosystem/split/gov scripts that
#               test/run_all_tests.sh's legacy section runs (no setup script,
#               no per-module run_all_tests.sh to parse).
#   multichain: skips standard chain start entirely and delegates to
#               test/federation/multichain/run_all_multichain_tests.sh, which
#               provisions its own chain-a (default ports) + chain-b (offset
#               +10000) + hermes relayer. The hardcoded ports don't collide
#               with normal suites because suite N uses RPC=27657+N*1000.
# ----------------------------------------------------------------------------
module_kind() {
    case "$1" in
        legacy)     echo "legacy" ;;
        multichain) echo "multichain" ;;
        *)          echo "normal" ;;
    esac
}

# ----------------------------------------------------------------------------
# Determine the per-module test-script order.
#
# Many modules' tests have ordering dependencies (e.g. x/reveal: cancel_reject
# puts the contributor on a proposal cooldown that breaks subsequent propose-
# heavy tests). Alphabetical iteration breaks these. Instead, we parse each
# module's run_all_tests.sh for `run_test "Display Name" "filename.sh"` lines
# and execute in that order.
#
# Falls back to alphabetical *_test.sh glob if the runner can't be parsed
# (or has no run_test calls).
#
# setup_test_accounts.sh is filtered out — run_suite handles it separately
# (and skips it entirely when restoring from a snapshot).
# ----------------------------------------------------------------------------
get_module_test_order() {
    local module="$1"
    local runner="$TEST_DIR/$module/run_all_tests.sh"

    if [ -f "$runner" ]; then
        # Preferred: a single-line `# TEST_ORDER: foo.sh bar.sh baz.sh`
        # comment. Comment-based manifest is more robust than `run_test`
        # call parsing because it can't accidentally drift from a single
        # source of truth — there's nowhere else for it to drift to.
        local from_comment
        from_comment=$(grep -E '^[[:space:]]*#[[:space:]]*TEST_ORDER:' "$runner" 2>/dev/null \
                        | head -1 \
                        | sed -E 's/^[[:space:]]*#[[:space:]]*TEST_ORDER:[[:space:]]*//' \
                        | tr ' ' '\n' \
                        | grep -vE '^$' \
                        | grep -vE '^setup_test_accounts\.sh$')
        if [ -n "$from_comment" ]; then
            echo "$from_comment"
            return 0
        fi

        # Fallback: parse `run_test "Display Name" "filename.sh"` calls.
        # Kept for backward compatibility with modules that haven't migrated
        # to the comment manifest.
        local ordered
        ordered=$(grep -E '^[[:space:]]*run_test[[:space:]]+"[^"]+"[[:space:]]+"[^"]+\.sh"' "$runner" 2>/dev/null \
                  | sed -E 's/.*run_test[[:space:]]+"[^"]+"[[:space:]]+"([^"]+\.sh)".*/\1/' \
                  | grep -vE '^setup_test_accounts\.sh$')
        if [ -n "$ordered" ]; then
            echo "$ordered"
            return 0
        fi
    fi

    # Last fallback: alphabetical glob of *_test.sh.
    local f
    for f in "$TEST_DIR/$module"/*_test.sh; do
        [ -f "$f" ] || continue
        basename "$f"
    done
}

# ----------------------------------------------------------------------------
# Args
# ----------------------------------------------------------------------------
usage() {
    cat <<EOF
Usage: $0 [--rebuild|--no-build] [--concurrency K | --full-parallel]
          [--clean] [--keep N] [<module> <module> ...]
       $0 --stop
       $0 --status [PATH|PID]
       $0 --clean-only
       $0 --clean-stale [DAYS]

Runs N module E2E test suites with isolated chain homes and +200 port
offsets per suite (suite 1 RPC=26857, suite 2 RPC=27057, ...). The
small stride keeps all suites well below the multichain pseudo-module's
chain-b ports (36xxx) so they can run concurrently without collision.

If no modules are given, auto-discovers from MODULE_ORDER in
test/run_all_tests.sh (the master runner's canonical list). Falls back
to scanning test/<dir>/run_all_tests.sh if MODULE_ORDER can't be parsed.
The pseudo-modules \`legacy\` (ecosystem/split/gov scripts) and
\`multichain\` (test/federation/multichain/run_all_multichain_tests.sh,
auto-included only if hermes is on PATH) are appended automatically.
Both can also be passed explicitly. At least 2 modules required (after
discovery). Duplicate names rejected.

Build options:
  (default)         Build sparkdreamd only if the binary is missing.
  --rebuild         Force a fresh \`ignite chain build -y --build.tags testparams\`.
  --no-build        Skip the build entirely; fail if the binary is missing.

Concurrency options:
  (default)         Run at most 7 suites at a time, in sequential batches.
                    Tuned so the 14-suite default lineup (12 normal modules
                    + legacy + multichain) splits into exactly 2 batches.
                    With the duration-descending MODULE_ORDER (longest
                    first) the slow suites land in batch 1 and the fast
                    ones (plus pseudo-modules) in batch 2, keeping batch-2
                    wall time well under batch-1's bottleneck. Holds
                    ~2.5 GB RSS / 7 chain processes.
  --concurrency K   Run at most K suites at a time (positive integer).
  --full-parallel   Disable the concurrency cap; launch every suite at once.
                    Equivalent to --concurrency=N where N = number of suites.

Disk-management options (never destructive without explicit opt-in):
  --clean           Before this run, remove all INACTIVE parallel-* dirs in
                    e2e/. Refuses if any has a live PID (use --stop first).
  --keep N          After this run, prune older run dirs and retain only the
                    N newest. The current run is always preserved regardless
                    of pass/fail. Combine with --clean for a rolling window.
  --clean-only      Standalone: clean inactive runs and exit (no run).
  --clean-stale [DAYS]
                    Standalone: clean inactive runs older than DAYS (default
                    7) and exit. Live-PID dirs are skipped.
  Each run also prints e2e/'s current disk usage and run count at startup
  so you have visibility without auto-clearing.

Control options:
  --stop            Stop any active parallel run (the script writes its PID
                    to e2e/run_parallel.pid at startup; --stop reads it and
                    SIGTERM/KILLs the entire process tree, then sweeps any
                    orphaned sparkdreamd processes whose --home points at
                    e2e/parallel-*).
  --status [PATH|PID]
                    Standalone: report on an in-progress or completed run.
                    Shows overall completion state and per-suite PASS/FAIL
                    counts plus the test currently in flight for live suites.
                    With no arg, resolves the run via (1) the active PID file,
                    (2) the e2e/latest symlink, or (3) the newest parallel-*
                    dir. Pass an explicit run dir path or a bare PID to
                    target a specific run. Read-only; safe to invoke at
                    any time, including while the run is still going.

Examples:
  $0                                       # all discovered modules, 7 at a time
  $0 forum                                 # just one module (full isolation, 1 batch of 1)
  $0 blog forum                            # just two — full parallel since 2 <= 7
  $0 --concurrency 2                       # all modules, 2 at a time
  $0 --concurrency 8 blog forum collect rep
  $0 --full-parallel                       # all modules, no cap (max throughput)
  $0 --rebuild                             # rebuild + run everything (default cap)
  $0 --no-build blog forum                 # tightest fast-iteration loop
  $0 --clean                               # wipe old runs first, then run all
  $0 --keep 3                              # run, then retain only 3 newest dirs
  $0 --clean --keep 5                      # wipe old + rolling window of 5
  $0 --stop                                # halt any active run; reap orphans
  $0 --status                              # snapshot of the latest/active run
  $0 --status 4036625                      # status of a specific run by PID
  $0 --clean-only                          # wipe inactive runs, no run
  $0 --clean-stale 14                      # prune runs >14 days old, no run
  $0 legacy multichain                     # only the pseudo-modules
  $0 blog forum legacy                     # mix normal modules with pseudo

Test ordering:
  Within each suite, tests run in the order declared by that module's
  run_all_tests.sh (parsed from \`run_test "Display Name" "filename.sh"\`
  lines). Modules with ordering dependencies (e.g. x/reveal — cancel_reject
  puts the contributor on a proposal cooldown that breaks subsequent
  propose-heavy tests) rely on this canonical order. If run_all_tests.sh
  can't be parsed for a module, falls back to alphabetical *_test.sh glob.

Resource note: each suite holds an in-memory chain (~350 MB RSS). The default
cap of 7 holds total chain RSS to ~2.5 GB; raise it via --concurrency for
faster wall-clock when you have headroom. --full-parallel maximizes throughput
but on 14 suites will spike CPU and produce very noisy live output. Full
per-module logs are always preserved at <project>/e2e/latest/<module>.log.
Each run takes ~4 GB on disk; use --keep N or --clean-stale to manage growth.
EOF
}

BUILD_MODE="auto"   # auto | force | skip
# Default concurrency cap protects against thrashing on machines with limited
# RAM/CPU when running many suites. Override with --concurrency K, or use
# --full-parallel to disable the cap entirely (sentinel: CONCURRENCY=0).
#
# Tuning rationale: the bottleneck is the longest single suite (forum at
# ~39 min on this codebase). The default lineup is 14 suites (12 normal
# modules + legacy + multichain pseudo-modules). Concurrency=4 produced 3
# batches with wall time ~88 min; concurrency=7 produces exactly 2 batches
# and — combined with the duration-descending MODULE_ORDER (slowest first)
# — yields wall time ~50 min on a machine with 8+ cores and 8+ GB RAM.
# Seven chains hold ~2.5 GB RSS, which is comfortable on a typical dev box.
CONCURRENCY=7
# Disk-management flags (default off; never destructive without explicit opt-in).
CLEAN_BEFORE_RUN=false
KEEP_N=""           # empty = no post-run pruning
POSITIONAL=()
while [ $# -gt 0 ]; do
    case "$1" in
        --stop)
            # Reap any active parallel run and exit. Tries the PID file
            # first, then sweeps for orphaned sparkdreamd processes.
            echo "Stopping any active parallel run..."
            stopped_tree=0
            if [ -f "$PID_FILE" ]; then
                target=$(cat "$PID_FILE" 2>/dev/null)
                if [ -n "$target" ] && kill -0 "$target" 2>/dev/null; then
                    echo "  Found active run: PID $target"
                    echo "  Killing process tree (TERM, then KILL)..."
                    kill_tree "$target" TERM
                    sleep 2
                    kill_tree "$target" KILL
                    stopped_tree=1
                else
                    echo "  PID file at $PID_FILE references stale PID '$target' — removing"
                fi
                rm -f "$PID_FILE"
            else
                echo "  No PID file at $PID_FILE (no actively-tracked run)"
            fi
            orphan_count=$(kill_orphan_parallel_chains)
            if [ "$orphan_count" -gt 0 ]; then
                echo "  Reaped $orphan_count orphaned sparkdreamd process(es)"
            fi
            if [ "$stopped_tree" -eq 0 ] && [ "$orphan_count" -eq 0 ]; then
                echo "  Nothing to stop."
            else
                echo "Done."
            fi
            exit 0
            ;;
        --status)
            # Standalone: report status of an in-progress or completed run.
            # Optional next positional may be a run dir path or a bare PID.
            shift
            status_target=""
            if [ $# -gt 0 ] && [ "${1:0:1}" != "-" ]; then
                status_target="$1"
                shift
            fi
            e2e_status_report "$status_target"
            exit $?
            ;;
        --rebuild|--build) BUILD_MODE="force"; shift ;;
        --no-build)        BUILD_MODE="skip";  shift ;;
        --clean)
            # Wipe inactive parallel-* dirs BEFORE this run starts.
            CLEAN_BEFORE_RUN=true
            shift
            ;;
        --clean-only)
            # Standalone: wipe inactive runs and exit.
            echo "Cleaning all inactive parallel-* dirs in $E2E_ROOT/..."
            e2e_clean_all || exit 1
            exit 0
            ;;
        --clean-stale)
            # Standalone: wipe inactive runs older than N days (default 7) and exit.
            shift
            stale_days=7
            if [ $# -gt 0 ] && [[ "$1" =~ ^[0-9]+$ ]]; then
                stale_days="$1"
                shift
            fi
            echo "Cleaning inactive parallel-* dirs older than $stale_days day(s)..."
            e2e_clean_stale "$stale_days" || exit 1
            exit 0
            ;;
        --keep)
            shift
            [ $# -gt 0 ] || { echo "ERROR: --keep requires a positive integer" >&2; exit 1; }
            KEEP_N="$1"
            if ! [[ "$KEEP_N" =~ ^[0-9]+$ ]] || [ "$KEEP_N" -lt 1 ]; then
                echo "ERROR: --keep must be a positive integer (got '$KEEP_N')" >&2
                exit 1
            fi
            shift
            ;;
        --keep=*)
            KEEP_N="${1#--keep=}"
            if ! [[ "$KEEP_N" =~ ^[0-9]+$ ]] || [ "$KEEP_N" -lt 1 ]; then
                echo "ERROR: --keep must be a positive integer (got '$KEEP_N')" >&2
                exit 1
            fi
            shift
            ;;
        --full-parallel|--unlimited)
            CONCURRENCY=0   # sentinel: "no limit; run all suites simultaneously"
            shift
            ;;
        --concurrency)
            shift
            [ $# -gt 0 ] || { echo "ERROR: --concurrency requires a positive integer" >&2; exit 1; }
            CONCURRENCY="$1"
            if ! [[ "$CONCURRENCY" =~ ^[0-9]+$ ]] || [ "$CONCURRENCY" -lt 1 ]; then
                echo "ERROR: --concurrency must be a positive integer (got '$CONCURRENCY')" >&2
                exit 1
            fi
            shift
            ;;
        --concurrency=*)
            CONCURRENCY="${1#--concurrency=}"
            if ! [[ "$CONCURRENCY" =~ ^[0-9]+$ ]] || [ "$CONCURRENCY" -lt 1 ]; then
                echo "ERROR: --concurrency must be a positive integer (got '$CONCURRENCY')" >&2
                exit 1
            fi
            shift
            ;;
        -h|--help)         usage; exit 0 ;;
        --)                shift; while [ $# -gt 0 ]; do POSITIONAL+=("$1"); shift; done ;;
        -*)                echo "ERROR: unknown flag: $1" >&2; usage >&2; exit 1 ;;
        *)                 POSITIONAL+=("$1"); shift ;;
    esac
done

# If no modules were given, auto-discover all module test suites.
if [ "${#POSITIONAL[@]}" -eq 0 ]; then
    echo "No modules specified — auto-discovering test/<module>/ suites..."
    mapfile -t POSITIONAL < <(discover_modules)
    if [ "${#POSITIONAL[@]}" -eq 0 ]; then
        echo "ERROR: no module test suites found under $TEST_DIR/" >&2
        echo "  (looking for dirs with run_all_tests.sh + at least one *_test.sh)" >&2
        exit 1
    fi
    echo "Discovered ${#POSITIONAL[@]} module(s): ${POSITIONAL[*]}"
    echo ""
fi

if [ "${#POSITIONAL[@]}" -lt 1 ]; then
    echo "ERROR: need at least 1 module (got ${#POSITIONAL[@]})" >&2
    usage >&2
    exit 1
fi
# Note: a single-module run still gets the full parallel-runner machinery
# (isolated chain home, +200 port offset, snapshot-aware prepare, suite
# log under e2e/parallel-PID/<module>.log). It's slightly heavier than
# `bash test/<module>/run_all_tests.sh` but useful when you want the same
# isolation guarantees and post-mortem layout as a multi-module run.

# MODULES is the canonical ordered list of module names. Suite indices
# are 1-based throughout (suite 1 = MODULES[0], suite 2 = MODULES[1], ...).
MODULES=("${POSITIONAL[@]}")
NUM_SUITES=${#MODULES[@]}

# Reject duplicates — they'd race on a shared snapshot dir and produce
# overlapping log filenames, which is rarely what the user wants.
declare -A _seen_modules=()
for m in "${MODULES[@]}"; do
    if [ -n "${_seen_modules[$m]:-}" ]; then
        echo "ERROR: duplicate module '$m' — each suite must be distinct" >&2
        exit 1
    fi
    _seen_modules[$m]=1
done
unset _seen_modules

# ----------------------------------------------------------------------------
# Pre-flight (PROJECT_DIR / TEST_DIR resolved earlier above)
# ----------------------------------------------------------------------------
REAL_SPARKDREAMD="$(go env GOPATH)/bin/sparkdreamd"

for m in "${MODULES[@]}"; do
    # Pseudo-modules (legacy, multichain) don't have a test/<name>/ dir;
    # they're handled via dedicated branches in prepare_suite_chain / run_suite.
    [ "$(module_kind "$m")" != "normal" ] && continue
    [ -d "$TEST_DIR/$m" ] || { echo "ERROR: module dir not found: $TEST_DIR/$m" >&2; exit 1; }
done

for cmd in jq curl ignite; do
    command -v "$cmd" >/dev/null || { echo "ERROR: $cmd not on PATH" >&2; exit 1; }
done

# ----------------------------------------------------------------------------
# Auto-build sparkdreamd as needed.
# Mirrors test/run_all_tests.sh: cleans stale binaries (per CLAUDE.md "stale
# binary problem") then runs `ignite chain build -y --build.tags testparams`.
# ----------------------------------------------------------------------------
build_sparkdreamd() {
    echo "================================================================"
    echo "Building sparkdreamd (ignite chain build --build.tags testparams)"
    echo "================================================================"
    # Clean stale local copies that ignite may otherwise pick up. The GOPATH
    # binary is rewritten by `ignite chain build`, so don't delete that one.
    rm -f "$PROJECT_DIR/sparkdreamd" "$PROJECT_DIR/build/sparkdreamd" /tmp/sparkdreamd
    rm -f "$HOME/.ignite/local-chains/sparkdream/exported_genesis.json"

    if ! (cd "$PROJECT_DIR" && ignite chain build -y --build.tags testparams); then
        echo "ERROR: ignite chain build failed" >&2
        return 1
    fi
    if [ ! -x "$REAL_SPARKDREAMD" ]; then
        echo "ERROR: build succeeded but $REAL_SPARKDREAMD is missing" >&2
        return 1
    fi
    echo "Build complete: $REAL_SPARKDREAMD"
    echo ""
    return 0
}

case "$BUILD_MODE" in
    force)
        build_sparkdreamd || exit 1
        ;;
    auto)
        if [ ! -x "$REAL_SPARKDREAMD" ]; then
            echo "sparkdreamd not found at $REAL_SPARKDREAMD — auto-building..."
            echo "(use --no-build to skip; --rebuild to force a rebuild)"
            echo ""
            build_sparkdreamd || exit 1
        fi
        ;;
    skip)
        if [ ! -x "$REAL_SPARKDREAMD" ]; then
            echo "ERROR: --no-build given but $REAL_SPARKDREAMD is missing" >&2
            echo "  Run without --no-build, or build manually:" >&2
            echo "    ignite chain build -y --build.tags testparams" >&2
            exit 1
        fi
        ;;
esac

# ----------------------------------------------------------------------------
# Parallel working dir + cleanup
# ----------------------------------------------------------------------------
# Lives inside the project (gitignored: see /e2e/ in .gitignore) so test logs
# are easy to open from the IDE. Override with $SPARKDREAM_E2E_DIR if needed.
# (E2E_ROOT was set early near the top of the script so --stop can use it.)

# Print a one-line disk-usage summary so the user has visibility into how
# many old runs are accumulating. This is informational only — never
# destructive without an explicit --clean / --keep / --clean-stale flag.
e2e_disk_summary

# Pre-run cleanup, opt-in via --clean. Refuses if any run is active.
if [ "$CLEAN_BEFORE_RUN" = "true" ]; then
    echo "--clean: removing inactive parallel-* dirs before this run..."
    e2e_clean_all || exit 1
fi

PARALLEL_DIR="$E2E_ROOT/parallel-$$"
mkdir -p "$E2E_ROOT"

# Acquire the PID file BEFORE creating $PARALLEL_DIR or the latest symlink,
# under flock-on-the-PID-file when flock is available. Two concurrent
# run_parallel.sh invocations would otherwise both pass the staleness
# check and clobber each other's `latest` symlink. flock makes the
# read-then-write check-and-claim atomic.
PID_LOCK="$E2E_ROOT/run_parallel.lock"
acquire_pid_lock() {
    if command -v flock >/dev/null 2>&1; then
        # Open fd 9 on the lock file; flock holds the lock for the lifetime
        # of fd 9 (process). `-n` = non-blocking, fail fast if held.
        exec 9>"$PID_LOCK"
        if ! flock -n 9; then
            echo "ERROR: another run_parallel.sh holds the lock at $PID_LOCK" >&2
            return 1
        fi
    fi
    if [ -f "$PID_FILE" ]; then
        existing=$(cat "$PID_FILE" 2>/dev/null)
        if [ -n "$existing" ] && kill -0 "$existing" 2>/dev/null; then
            echo "ERROR: another run_parallel.sh is already active (PID $existing)" >&2
            echo "       see $PID_FILE — stop it first with: bash $0 --stop" >&2
            return 1
        fi
    fi
    echo "$$" > "$PID_FILE"
    return 0
}
if ! acquire_pid_lock; then
    exit 1
fi

mkdir -p "$PARALLEL_DIR"

# Refresh the `latest` symlink so the user can always do
#   tail -f e2e/latest/<module>.log
# without remembering the PID.
LATEST_LINK="$E2E_ROOT/latest"
ln -sfn "parallel-$$" "$LATEST_LINK"

CHILD_PIDS=()

cleanup() {
    local rc=$?
    # Disarm the trap immediately so calling `exit $rc` below cannot
    # re-fire EXIT and recurse on bash builds where EXIT runs again
    # after the explicit exit. INT/TERM are also disarmed so a
    # second Ctrl-C during cleanup goes straight to the kernel.
    trap - EXIT INT TERM
    echo ""
    echo "Cleaning up child processes (entire tree under each tracked PID)..."
    # SIGTERM the whole tree first — gives chains a chance to flush LevelDB.
    for pid in "${CHILD_PIDS[@]:-}"; do
        [ -n "$pid" ] && kill_tree "$pid" TERM
    done
    sleep 1
    # Escalate to SIGKILL for any survivors. Also belt-and-braces sweep
    # for orphaned parallel-* sparkdreamd in case anything slipped through.
    for pid in "${CHILD_PIDS[@]:-}"; do
        [ -n "$pid" ] && kill_tree "$pid" KILL
    done
    kill_orphan_parallel_chains > /dev/null
    rm -f "$PID_FILE" 2>/dev/null
    rm -f "$PID_LOCK" 2>/dev/null
    echo "Logs preserved in: $PARALLEL_DIR"
    exit $rc
}
trap cleanup EXIT INT TERM

# ----------------------------------------------------------------------------
# Suite allocation: home dir, ports, and a PATH-wrapper that injects --home.
#
# Side-effects: creates $PARALLEL_DIR/suite-<idx>-<module>/{home,bin}/sparkdreamd
# and appends to the per-suite parallel arrays declared below. Suites are
# 1-indexed in the directory naming for human readability; the parallel
# arrays themselves are 0-indexed (bash convention).
# ----------------------------------------------------------------------------
SUITE_HOMES=()
SUITE_BINS=()
SUITE_RPCS=()
SUITE_P2PS=()
SUITE_GRPCS=()
SUITE_GRPC_WEBS=()
SUITE_APIS=()

allocate_suite() {
    local idx="$1"       # 1-based suite index
    local module="$2"
    # Stride chosen so the multichain pseudo-module's chain-b ports (36xxx,
    # 9190, 1417) stay outside the parallel suite range even at full
    # parallelism. With idx*200, suite 14 tops out at 29457/11890/4117 —
    # well below 36xxx. The previous idx*1000 multiplier put suite 10 on
    # 36657 which collided with chain-b. See header "Port allocation"
    # block for the full layout.
    local offset=$((idx * 200))

    local suite_root="$PARALLEL_DIR/suite-${idx}-${module}"
    local home="$suite_root/home"
    local bin="$suite_root/bin"

    mkdir -p "$bin"

    # Wrapper: any `sparkdreamd …` call from a script with this dir on PATH
    # becomes `<real_binary> --home <suite_home> …`. `--home` is a persistent
    # root flag in Cosmos SDK so all subcommands accept it. The wrapper also
    # ensures every CLI lookup (keys, tx, query) hits the suite's keyring and
    # client.toml — meaning --node is read from the patched client.toml.
    cat > "$bin/sparkdreamd" <<EOF
#!/bin/bash
exec "$REAL_SPARKDREAMD" --home "$home" "\$@"
EOF
    chmod +x "$bin/sparkdreamd"

    SUITE_HOMES+=("$home")
    SUITE_BINS+=("$bin")
    SUITE_RPCS+=($((26657 + offset)))
    SUITE_P2PS+=($((26656 + offset)))
    SUITE_GRPCS+=($((9090 + offset)))
    SUITE_GRPC_WEBS+=($((9091 + offset)))
    SUITE_APIS+=($((1317 + offset)))
}

# ----------------------------------------------------------------------------
# Snapshot freshness check.
# Returns 0 if the module has a post-setup snapshot whose recorded setup_hash
# matches the current SHA-256 of setup_test_accounts.sh. Returns 1 if the
# snapshot is missing, incomplete, or stale.
# Mirrors the logic in test/_auto_snapshot.sh::_snapshot_is_fresh.
# ----------------------------------------------------------------------------
module_snapshot_is_fresh() {
    local module="$1"
    local snap="$TEST_DIR/$module/snapshots/post-setup"
    local setup="$TEST_DIR/$module/setup_test_accounts.sh"

    [ -d "$snap/sparkdream_data" ] || return 1

    if [ -f "$setup" ]; then
        [ -f "$snap/setup_hash" ] || return 1
        local current stored
        current=$(sha256sum "$setup" 2>/dev/null | cut -d' ' -f1)
        stored=$(cat "$snap/setup_hash" 2>/dev/null)
        [ -n "$current" ] || return 1
        [ "$current" = "$stored" ] || return 1
    fi
    return 0
}

# ----------------------------------------------------------------------------
# Restore a module's post-setup snapshot into a suite home directory.
# Skips the snapshot's bundled restore.sh (which does its own pkill / chain
# stop / backup dance — irrelevant when restoring into a fresh suite home).
# Also resets priv_validator_state.json so CometBFT can sign after restore.
# Drops a `.from_snapshot` marker so run_suite knows to skip setup_test_accounts.sh.
# ----------------------------------------------------------------------------
restore_from_snapshot_into_home() {
    local module="$1"
    local home="$2"
    local snap="$TEST_DIR/$module/snapshots/post-setup"

    # Guard: $home must be inside our PARALLEL_DIR so an empty/misconfigured
    # variable can't reach the user's filesystem. safe_rmtree adds further
    # guards (no /, no $HOME, must be absolute).
    case "$home" in
        "$PARALLEL_DIR"/*) ;;
        *) echo "restore_from_snapshot_into_home: refusing — $home outside $PARALLEL_DIR" >&2; return 1 ;;
    esac
    safe_rmtree "$home" || return 1
    mkdir -p "$(dirname "$home")"
    cp -r "$snap/sparkdream_data" "$home" || return 1

    # Reset validator state — recorded height may exceed app state, which
    # CometBFT would refuse to sign past. Safe on a single-validator dev chain.
    local pvs="$home/data/priv_validator_state.json"
    if [ -f "$pvs" ]; then
        cat > "$pvs" <<'EOF'
{
  "height": "0",
  "round": 0,
  "step": 0
}
EOF
    fi

    # Marker so run_suite skips setup_test_accounts.sh.
    : > "$home/.from_snapshot"
    return 0
}

# ----------------------------------------------------------------------------
# Fresh `ignite chain init` into a suite home directory.
# Backs up any pre-existing ~/.sparkdream during the swap.
#
# CONCURRENCY CONSTRAINT: ignite chain init writes to $HOME/.sparkdream — a
# single global path — so two parallel invocations would clobber each
# other's output. We serialize calls behind a global flock on a sentinel
# file under $E2E_ROOT. The current call sites (prepare_suite_chain in
# Step 1's serial loop) already serialize on the caller side, but the lock
# is cheap belt-and-braces in case a future change moves it into the
# parallel batch loop.
# ----------------------------------------------------------------------------
ignite_init_into_home() {
    local home="$1"

    # Serialize via flock when available — see CONCURRENCY CONSTRAINT above.
    local _ignite_lock="$E2E_ROOT/ignite_init.lock"
    local _have_flock=0
    if command -v flock >/dev/null 2>&1; then
        _have_flock=1
        exec 8>"$_ignite_lock"
        flock 8
    fi

    local backup=""
    if [ -e "$HOME/.sparkdream" ]; then
        backup="$HOME/.sparkdream.parallel-bak.$$"
        mv "$HOME/.sparkdream" "$backup"
    fi

    local rc=0
    (cd "$PROJECT_DIR" && ignite chain init -y --build.tags testparams) \
        > "$PARALLEL_DIR/ignite-init-$(basename "$home").log" 2>&1 || rc=$?

    if [ $rc -ne 0 ] || [ ! -d "$HOME/.sparkdream" ]; then
        echo "ERROR: ignite chain init failed (rc=$rc) — see $PARALLEL_DIR/ignite-init-*.log" >&2
        # Clean partial init output so the next caller starts from a known
        # state. safe_rmtree refuses /, $HOME, empty, or non-absolute paths.
        safe_rmtree "$HOME/.sparkdream"
        [ -n "$backup" ] && mv "$backup" "$HOME/.sparkdream"
        return 1
    fi

    # Guard: $home must be inside PARALLEL_DIR (defense-in-depth on top of
    # safe_rmtree's own checks).
    case "$home" in
        "$PARALLEL_DIR"/*) ;;
        *) echo "ignite_init_into_home: refusing — $home outside $PARALLEL_DIR" >&2; return 1 ;;
    esac
    safe_rmtree "$home" || return 1
    mkdir -p "$(dirname "$home")"
    mv "$HOME/.sparkdream" "$home"
    [ -n "$backup" ] && mv "$backup" "$HOME/.sparkdream"
    return 0
}

# ----------------------------------------------------------------------------
# Patch ports in a chain home's TOML configs. Idempotent against any chain
# home freshly initialized OR restored from a default-port snapshot — both
# carry 26656/26657/9090/9091/1317 in their TOMLs.
# ----------------------------------------------------------------------------
patch_suite_ports() {
    local home="$1"
    local rpc="$2" p2p="$3" grpc="$4" grpc_web="$5" api="$6"

    local cfg="$home/config/config.toml"
    local app="$home/config/app.toml"
    local cli="$home/config/client.toml"

    # All sed patterns rewrite ONLY the explicit listen-address forms ignite
    # produces (tcp://0.0.0.0:PORT, tcp://127.0.0.1:PORT, tcp://localhost:PORT,
    # and bare 0.0.0.0:PORT / 127.0.0.1:PORT / localhost:PORT in app.toml's
    # gRPC blocks — Cosmos SDK ships the gRPC entry as `localhost:9090`).
    # We intentionally do NOT use a bare ":PORT|:NEW" pattern because that
    # would also rewrite a comment, a future telemetry URL, or any other
    # incidental occurrence of the digit string.

    # config.toml: rpc.laddr, p2p.laddr (proxy_app stays in-process)
    sed -i \
        -e "s|tcp://127.0.0.1:26657|tcp://127.0.0.1:$rpc|g" \
        -e "s|tcp://0.0.0.0:26657|tcp://0.0.0.0:$rpc|g" \
        -e "s|tcp://0.0.0.0:26656|tcp://0.0.0.0:$p2p|g" \
        -e "s|tcp://127.0.0.1:26656|tcp://127.0.0.1:$p2p|g" \
        "$cfg"
    sed -i 's|^prometheus = true|prometheus = false|g'           "$cfg" 2>/dev/null || true
    sed -i 's|^pprof_laddr = "localhost:6060"|pprof_laddr = ""|g' "$cfg" 2>/dev/null || true
    # Mempool recheck timeout: under parallel load, the application server can
    # be CPU-starved and a recheck (run after every block commit) can blow past
    # the 1s default — CometBFT then silently evicts the pending tx. Bumping
    # to 5s eliminates this class of flake; the cost is 5s of stuck mempool
    # in the (already broken) case where the app truly hangs.
    sed -i 's|^recheck_timeout = "1s"|recheck_timeout = "5s"|g' "$cfg" 2>/dev/null || true

    # app.toml: gRPC, gRPC-Web, API/REST
    sed -i \
        -e "s|0.0.0.0:9090|0.0.0.0:$grpc|g" \
        -e "s|127.0.0.1:9090|127.0.0.1:$grpc|g" \
        -e "s|localhost:9090|localhost:$grpc|g" \
        -e "s|0.0.0.0:9091|0.0.0.0:$grpc_web|g" \
        -e "s|127.0.0.1:9091|127.0.0.1:$grpc_web|g" \
        -e "s|localhost:9091|localhost:$grpc_web|g" \
        -e "s|tcp://0.0.0.0:1317|tcp://0.0.0.0:$api|g" \
        -e "s|tcp://localhost:1317|tcp://localhost:$api|g" \
        "$app"

    # client.toml: node URL — critical so test scripts (which don't pass
    # --node) hit the right port via client.toml lookup.
    sed -i \
        -e "s|tcp://127.0.0.1:26657|tcp://127.0.0.1:$rpc|g" \
        -e "s|tcp://localhost:26657|tcp://127.0.0.1:$rpc|g" \
        "$cli" 2>/dev/null || true

    # Post-write verification: confirm every patched file has dropped its
    # original port references. A silent sed no-op (e.g. on a stray
    # already-rewritten file or a config.toml whose listen-address line
    # uses a form we don't anchor on) would put two suites on the same
    # RPC port at runtime, which produces opaque "address already in use"
    # crashes. Fail loudly instead.
    local _err=0
    if grep -qE 'tcp://(0\.0\.0\.0|127\.0\.0\.1):26657' "$cfg" 2>/dev/null; then
        echo "  ERROR: $cfg still references RPC port 26657 after patch" >&2
        _err=1
    fi
    if grep -qE 'tcp://(0\.0\.0\.0|127\.0\.0\.1):26656' "$cfg" 2>/dev/null; then
        echo "  ERROR: $cfg still references P2P port 26656 after patch" >&2
        _err=1
    fi
    if grep -qE '^\s*address = "(0\.0\.0\.0|127\.0\.0\.1|localhost):9090"' "$app" 2>/dev/null; then
        echo "  ERROR: $app still binds gRPC :9090 after patch" >&2
        _err=1
    fi
    if grep -qE 'tcp://(0\.0\.0\.0|localhost):1317' "$app" 2>/dev/null; then
        echo "  ERROR: $app still binds REST 1317 after patch" >&2
        _err=1
    fi
    return $_err
}

# ----------------------------------------------------------------------------
# Prepare a suite's chain home: prefer a fresh snapshot when available,
# otherwise fall back to ignite chain init + setup. Always patches ports.
# Echoes a status line describing which path was taken.
# ----------------------------------------------------------------------------
prepare_suite_chain() {
    local module="$1"
    local home="$2"
    local rpc="$3" p2p="$4" grpc="$5" grpc_web="$6" api="$7"

    local kind
    kind=$(module_kind "$module")

    case "$kind" in
        multichain)
            # multichain manages its own chain-a (default ports 26657/26656/
            # 9090/1317) + chain-b (+10000 offset → 36657/36656/9190/1417)
            # inside test/federation/multichain/data/. No standard chain
            # home/ports needed for the suite slot — just keep the empty
            # $home dir so downstream paths still resolve. The +200-stride
            # parallel suite ports (26857..29457 across 14 suites) sit
            # comfortably below chain-b's 36xxx so multichain can run
            # concurrently with normal suites without collision.
            mkdir -p "$home"
            echo "    → multichain pseudo-module: skipping standard chain init (run_all_multichain_tests.sh provisions its own)"
            return 0
            ;;
        legacy)
            # legacy scripts (ecosystem_spend, split/*, gov/inflation_immutable)
            # depend on the genesis-bootstrapped state (council policies,
            # community pool, etc.) but NOT on a setup_test_accounts.sh.
            # Always do a fresh init — there's no snapshot to consume.
            echo "    → legacy pseudo-module — fresh ignite chain init (no setup_test_accounts.sh)"
            ignite_init_into_home "$home" || return 1
            patch_suite_ports "$home" "$rpc" "$p2p" "$grpc" "$grpc_web" "$api" \
                || { echo "ERROR: port patch failed for legacy module $module" >&2; return 1; }
            return 0
            ;;
    esac

    # Normal modules: prefer snapshot, fall back to fresh init.
    local snap_dir="$TEST_DIR/$module/snapshots/post-setup"

    if module_snapshot_is_fresh "$module"; then
        echo "    → reusing snapshot ($snap_dir) — will skip setup_test_accounts.sh"
        restore_from_snapshot_into_home "$module" "$home" \
            || { echo "ERROR: snapshot restore failed for $module" >&2; return 1; }
    else
        if [ -d "$snap_dir/sparkdream_data" ]; then
            echo "    → snapshot stale (setup_test_accounts.sh changed since save) — fresh init"
        else
            echo "    → no snapshot — fresh ignite chain init + setup"
        fi
        ignite_init_into_home "$home" || return 1
    fi

    patch_suite_ports "$home" "$rpc" "$p2p" "$grpc" "$grpc_web" "$api" \
        || { echo "ERROR: port patch failed for $module" >&2; return 1; }
    return 0
}

# ----------------------------------------------------------------------------
# Run one suite: start chain → setup → all *_test.sh → stop chain.
# Output is redirected to $logfile; caller tails it concurrently.
# Returns the number of failed steps (0 = all pass).
# ----------------------------------------------------------------------------
run_suite() {
    local module="$1"
    local home="$2"
    local bin="$3"
    local rpc="$4"
    local logfile="$5"
    local module_dir="$TEST_DIR/$module"

    # Per-suite wall-clock timing. Captured here, before the subshell, so
    # we can write the side-channel file ($logfile.timing) regardless of
    # how the inner block exits — the outer `}` runs in this same shell
    # and a trap on EXIT/RETURN inside a function isn't reliable.
    local _suite_start _suite_start_human _suite_end _suite_end_human _suite_dur _suite_rc

    _suite_start=$(timing_now_epoch)
    _suite_start_human=$(timing_now_human)

    local kind
    kind=$(module_kind "$module")

    # ----------------------------------------------------------------
    # multichain pseudo-module: skip standard chain start; delegate to
    # run_all_multichain_tests.sh, which provisions its own chains and
    # hermes relayer. No --home wrapper needed since the multichain
    # script invokes the GOPATH binary directly with explicit --home
    # flags pointing at test/federation/multichain/data/chain-{a,b}.
    # ----------------------------------------------------------------
    if [ "$kind" = "multichain" ]; then
        {
            echo "=== [$module] starting at $_suite_start_human (multichain pseudo-module) ==="
            local mc_runner="$TEST_DIR/federation/multichain/run_all_multichain_tests.sh"
            if [ ! -x "$mc_runner" ]; then
                echo "[$module] FAIL: $mc_runner not found or not executable"
                return 1
            fi
            if ! command -v hermes >/dev/null 2>&1; then
                echo "[$module] FAIL: hermes not on PATH (required for multichain tests)"
                return 1
            fi
            echo "[$module] --- run_all_multichain_tests.sh ---"
            local rc=0
            (cd "$TEST_DIR/federation/multichain" && bash ./run_all_multichain_tests.sh) || rc=$?
            if [ $rc -eq 0 ]; then
                echo "[$module] === SUITE DONE: 1 passed, 0 failed ==="
            else
                echo "[$module] === SUITE DONE: 0 passed, 1 failed ==="
                echo "[$module] Failed:"
                echo "[$module]   - run_all_multichain_tests"
            fi
            return $rc
        } > "$logfile" 2>&1
        _suite_rc=$?

        _suite_end=$(timing_now_epoch)
        _suite_end_human=$(timing_now_human)
        _suite_dur=$(timing_format_duration $((_suite_end - _suite_start)))
        {
            echo "[$module] === SUITE TIMING ==="
            echo "[$module] Started:  $_suite_start_human"
            echo "[$module] Ended:    $_suite_end_human"
            echo "[$module] Duration: $_suite_dur"
        } >> "$logfile"
        echo "$_suite_start $_suite_end" > "$logfile.timing"
        return $_suite_rc
    fi

    {
        export PATH="$bin:$PATH"
        export CHAIN_HOME="$home"   # not used by existing scripts but handy

        echo "=== [$module] starting at $_suite_start_human (home=$home rpc=$rpc) ==="

        # Start chain via wrapper (--home injected). Log to suite's own file.
        sparkdreamd start > "$home/chain.log" 2>&1 &
        local chain_pid=$!
        echo "[$module] chain pid=$chain_pid (log: $home/chain.log)"

        # Wait for the chain to produce blocks (height > 0)
        local up=0
        for i in $(seq 1 90); do
            local h
            h=$(curl -s --max-time 2 "http://127.0.0.1:$rpc/status" 2>/dev/null \
                | jq -r '.result.sync_info.latest_block_height // "0"' 2>/dev/null)
            if [ -n "$h" ] && [ "$h" -gt 0 ] 2>/dev/null; then
                echo "[$module] chain producing blocks (height=$h) after ${i}s"
                up=1
                break
            fi
            # Bail early if chain crashed
            if ! kill -0 $chain_pid 2>/dev/null; then
                echo "[$module] ERROR: chain process exited unexpectedly"
                echo "[$module] last 30 lines of chain.log:"
                tail -30 "$home/chain.log" 2>/dev/null | sed 's/^/[chain] /'
                return 99
            fi
            sleep 1
        done

        if [ "$up" != "1" ]; then
            echo "[$module] ERROR: chain did not produce blocks within 90s"
            kill $chain_pid 2>/dev/null
            wait $chain_pid 2>/dev/null
            return 99
        fi

        # Second readiness check: wait for the cosmos-SDK auth-query path
        # to be live. CometBFT RPC (above) typically starts BEFORE the SDK's
        # gRPC server and api-server (the chain log shows "Starting RPC HTTP
        # server" several lines before "starting gRPC server"). Without this
        # second poll, tests that broadcast a tx in the gap can hit a race:
        # the CLI's `--from <key>` auto-fetch silently fails, falls back to
        # account_number=0 sequence=0, the chain rejects with
        # `signature verification failed; please verify account number (0)`,
        # and the test sees an unexplained CheckTx code=4. Symptom seen in
        # season/profile_test (May 9 2026, parallel run 11:03).
        #
        # We probe the same path the CLI uses for --from auto-fetch: the
        # auth params query, routed through ABCI -> gRPC router -> auth
        # keeper. Once `query auth params` succeeds, account fetches will
        # too. Using the wrapper means --home + per-suite client.toml's
        # `node = tcp://127.0.0.1:<rpc>` are picked up automatically.
        local sdk_up=0
        for i in $(seq 1 30); do
            if sparkdreamd query auth params --output json > /dev/null 2>&1; then
                echo "[$module] cosmos-SDK auth-query path responding after ${i}s"
                sdk_up=1
                break
            fi
            if ! kill -0 $chain_pid 2>/dev/null; then
                echo "[$module] ERROR: chain process exited unexpectedly during SDK readiness wait"
                tail -30 "$home/chain.log" 2>/dev/null | sed 's/^/[chain] /'
                return 99
            fi
            sleep 1
        done

        if [ "$sdk_up" != "1" ]; then
            echo "[$module] ERROR: cosmos-SDK auth-query path not responding within 30s"
            echo "[$module] (CometBFT RPC was up but auth ABCI query never succeeded — tests would race)"
            kill $chain_pid 2>/dev/null
            wait $chain_pid 2>/dev/null
            return 99
        fi

        # Run setup_test_accounts.sh — UNLESS the chain was restored from a
        # post-setup snapshot, in which case the accounts/membership are
        # already provisioned in the LevelDB and re-running setup would either
        # be wasteful or actively fail (e.g. "invitation already exists").
        # Pseudo-modules (legacy) skip setup entirely — their tests rely on
        # genesis-bootstrapped state only.
        local pass=0 fail=0
        local -a failed=()

        if [ "$kind" = "legacy" ]; then
            echo "[$module] (legacy pseudo-module — skipping setup_test_accounts.sh)"
        elif [ -f "$home/.from_snapshot" ]; then
            echo "[$module] (chain restored from snapshot — skipping setup_test_accounts.sh)"
            # If the snapshot saved a .test_env in the module dir alongside
            # setup_test_accounts.sh, it's already on disk and the test
            # scripts will source it directly (they always read it relative
            # to their own SCRIPT_DIR). Nothing to do here.
        elif [ -f "$module_dir/setup_test_accounts.sh" ]; then
            echo "[$module] --- setup_test_accounts.sh ---"
            if (cd "$module_dir" && bash ./setup_test_accounts.sh); then
                pass=$((pass + 1))
                echo "[$module] PASS: setup_test_accounts"
            else
                fail=$((fail + 1))
                failed+=("setup_test_accounts")
                echo "[$module] FAIL: setup_test_accounts (aborting suite)"
                kill $chain_pid 2>/dev/null
                wait $chain_pid 2>/dev/null
                return 1
            fi
        fi

        # Build the per-suite test list. Normal modules parse run_all_tests.sh
        # for ordering; the `legacy` pseudo-module mirrors the legacy block in
        # test/run_all_tests.sh (ecosystem → split → security). Each entry is
        # a path relative to $TEST_DIR — the test loop below cd's into the
        # script's own dir before running so SCRIPT_DIR-based sourcing works.
        local -a tests=()
        local t
        if [ "$kind" = "legacy" ]; then
            tests=(
                "ecosystem/ecosystem_spend.sh"
                "split/accounts.sh"
                "split/autodivert.sh"
                "ecosystem/ecosystem_security_test.sh"
                "gov/inflation_immutable_test.sh"
            )
        else
            # Run each *_test.sh in the order declared by the module's
            # run_all_tests.sh (parsed from `run_test "..." "<file>.sh"` calls).
            # Falls back to alphabetical glob when the runner can't be parsed.
            # Modules with ordering dependencies (e.g. x/reveal — cancel_reject
            # leaves the contributor on a proposal cooldown that breaks later
            # propose-heavy tests) rely on this canonical order to pass.
            while IFS= read -r t; do
                [ -n "$t" ] && tests+=("$t")
            done < <(get_module_test_order "$module")
        fi

        if [ ${#tests[@]} -eq 0 ]; then
            echo "[$module] WARN: no test scripts found"
        fi

        local test_name test_script test_dir
        for t in "${tests[@]}"; do
            if [ "$kind" = "legacy" ]; then
                # legacy entries are paths relative to $TEST_DIR
                test_script="$TEST_DIR/$t"
                test_dir="$(dirname "$test_script")"
            else
                test_script="$module_dir/$t"
                test_dir="$module_dir"
            fi
            if [ ! -f "$test_script" ]; then
                echo "[$module] WARN: $t not found at $test_script — skipping"
                continue
            fi
            test_name=$(basename "$test_script" .sh)
            echo "[$module] --- $test_name ---"
            if (cd "$test_dir" && bash "$test_script"); then
                pass=$((pass + 1))
                echo "[$module] PASS: $test_name"
            else
                fail=$((fail + 1))
                failed+=("$test_name")
                echo "[$module] FAIL: $test_name"
            fi
        done

        echo "[$module] === SUITE DONE: $pass passed, $fail failed ==="
        if [ ${#failed[@]} -gt 0 ]; then
            echo "[$module] Failed:"
            for t in "${failed[@]}"; do
                echo "[$module]   - $t"
            done
        fi

        # Stop chain cleanly
        kill $chain_pid 2>/dev/null
        wait $chain_pid 2>/dev/null

        return $fail
    } > "$logfile" 2>&1
    _suite_rc=$?

    _suite_end=$(timing_now_epoch)
    _suite_end_human=$(timing_now_human)
    _suite_dur=$(timing_format_duration $((_suite_end - _suite_start)))

    # Append a timing footer to the suite's own log so per-module log
    # readers can see when their suite started/ended without consulting
    # the aggregator.
    {
        echo "[$module] === SUITE TIMING ==="
        echo "[$module] Started:  $_suite_start_human"
        echo "[$module] Ended:    $_suite_end_human"
        echo "[$module] Duration: $_suite_dur"
    } >> "$logfile"

    # Side-channel file consumed by the FINAL RESULTS block in the parent
    # shell. Keep the format simple ("<start_epoch> <end_epoch>") so it's
    # trivial to read with `read -r` and resilient to log truncation.
    echo "$_suite_start $_suite_end" > "$logfile.timing"

    return $_suite_rc
}

# ----------------------------------------------------------------------------
# Main
# ----------------------------------------------------------------------------

# Allocate one suite per module
for ((i=0; i<NUM_SUITES; i++)); do
    allocate_suite $((i + 1)) "${MODULES[$i]}"
done

# Overall run wall-clock — captured before suites kick off so the FINAL
# RESULTS block can compare against it for total elapsed time.
RUN_START_EPOCH=$(timing_now_epoch)
RUN_START_HUMAN=$(timing_now_human)

{
    echo "================================================================"
    echo "PARALLEL E2E TEST RUNNER ($NUM_SUITES suites)"
    echo "Started at: $RUN_START_HUMAN"
    echo "================================================================"
    for ((i=0; i<NUM_SUITES; i++)); do
        printf "Suite %d: %s\n" "$((i + 1))" "${MODULES[$i]}"
        printf "  home : %s\n" "${SUITE_HOMES[$i]}"
        printf "  ports: rpc=%d p2p=%d grpc=%d grpc_web=%d api=%d\n" \
            "${SUITE_RPCS[$i]}" "${SUITE_P2PS[$i]}" \
            "${SUITE_GRPCS[$i]}" "${SUITE_GRPC_WEBS[$i]}" "${SUITE_APIS[$i]}"
    done
    echo ""
    echo "Workdir: $PARALLEL_DIR"
    echo "================================================================"
    echo ""
    echo "Step 1: Preparing chain homes (sequential — ignite chain init touches"
    echo "        ~/.sparkdream; snapshot restores are also serialized to keep"
    echo "        filesystem I/O simple)"
}

# Step 1: prepare each suite's chain home (snapshot or fresh init), serially
for ((i=0; i<NUM_SUITES; i++)); do
    echo "  Suite $((i + 1)) (${MODULES[$i]}):"
    prepare_suite_chain "${MODULES[$i]}" "${SUITE_HOMES[$i]}" \
        "${SUITE_RPCS[$i]}" "${SUITE_P2PS[$i]}" "${SUITE_GRPCS[$i]}" \
        "${SUITE_GRPC_WEBS[$i]}" "${SUITE_APIS[$i]}" \
        || { echo "FATAL: failed to prepare suite $((i + 1)) (${MODULES[$i]})" >&2; exit 1; }
done

# Pre-create empty per-module log files so the tails can latch onto them
LOGS=()
for ((i=0; i<NUM_SUITES; i++)); do
    log="$PARALLEL_DIR/${MODULES[$i]}.log"
    LOGS+=("$log")
    : > "$log"
done

# Determine the effective batch size. CONCURRENCY=0 (the default) means
# "no limit" — launch every suite at once. Otherwise launch up to that many
# suites concurrently and wait for the batch before starting the next.
EFFECTIVE_CONCURRENCY=$CONCURRENCY
if [ $EFFECTIVE_CONCURRENCY -le 0 ] || [ $EFFECTIVE_CONCURRENCY -gt $NUM_SUITES ]; then
    EFFECTIVE_CONCURRENCY=$NUM_SUITES
fi

# NUM_BATCHES is a lower-bound estimate. The actual count may be higher
# because the multichain pseudo-module is forced into its own batch (see
# the isolation rule in the batch loop), so a 14-suite + concurrency=7
# layout that "should" be 2 batches becomes 3 (batch 1: 7 normal, batch 2:
# 6 normal, batch 3: multichain alone).
NUM_BATCHES=$(( (NUM_SUITES + EFFECTIVE_CONCURRENCY - 1) / EFFECTIVE_CONCURRENCY ))
HAS_MULTICHAIN=false
for m in "${MODULES[@]}"; do
    if [ "$(module_kind "$m")" = "multichain" ]; then HAS_MULTICHAIN=true; break; fi
done

echo ""
# Pick singular/plural based on suite count so a single-module run reads
# naturally ("Launching 1 suite", not "Launching 1 suites").
if [ "$NUM_SUITES" -eq 1 ]; then
    SUITE_NOUN="suite"
else
    SUITE_NOUN="suites"
fi
if [ $EFFECTIVE_CONCURRENCY -ge $NUM_SUITES ] && [ "$HAS_MULTICHAIN" != true ]; then
    if [ "$NUM_SUITES" -eq 1 ]; then
        echo "Step 2: Launching the single $SUITE_NOUN"
    else
        echo "Step 2: Launching all $NUM_SUITES $SUITE_NOUN in parallel"
    fi
else
    if [ "$HAS_MULTICHAIN" = true ]; then
        echo "Step 2: Launching $NUM_SUITES $SUITE_NOUN in batch(es) of up to $EFFECTIVE_CONCURRENCY (multichain isolated to its own batch)"
    else
        echo "Step 2: Launching $NUM_SUITES $SUITE_NOUN in $NUM_BATCHES batch(es) of up to $EFFECTIVE_CONCURRENCY"
    fi
fi
for ((i=0; i<NUM_SUITES; i++)); do
    echo "  Suite $((i + 1)) (${MODULES[$i]}) log: ${LOGS[$i]}"
done
echo ""

# Pre-allocate per-suite tracking arrays. SUITE_PIDS[i] / SUITE_RCS[i] map
# 1:1 to MODULES[i] regardless of which batch the suite ran in, so the final
# results section below stays a simple linear walk.
SUITE_PIDS=()
SUITE_RCS=()
for ((i=0; i<NUM_SUITES; i++)); do
    SUITE_PIDS+=(0)
    SUITE_RCS+=(0)
done

batch_num=0
batch_start=0
while [ $batch_start -lt $NUM_SUITES ]; do
    batch_num=$((batch_num + 1))
    batch_end=$((batch_start + EFFECTIVE_CONCURRENCY))
    [ $batch_end -gt $NUM_SUITES ] && batch_end=$NUM_SUITES

    # Multichain isolation rule: run multichain alone in its own batch.
    # Why: run_all_multichain_tests.sh -> init_chains.sh runs `ignite chain
    # init`, which under the hood does `ignite chain build` and walks the
    # entire project tree (including e2e/parallel-PID/suite-N/home/data/).
    # If any other parallel suite is concurrently writing LevelDB temp
    # files in their data dirs, ignite's walker can lstat a write-file-
    # atomic-* path that has already been renamed away, aborting with
    # "cannot build app: lstat ...: no such file or directory". Easiest
    # robust fix: don't share a batch with multichain.
    for ((i=batch_start; i<batch_end; i++)); do
        if [ "$(module_kind "${MODULES[$i]}")" = "multichain" ]; then
            if [ $i -eq $batch_start ]; then
                # multichain is first in this batch — make it the only one
                batch_end=$((i + 1))
            else
                # multichain comes later in the batch — end the batch just
                # before multichain so this run is multichain-free, then
                # the next iteration picks up multichain alone
                batch_end=$i
            fi
            break
        fi
    done

    if [ $NUM_BATCHES -gt 1 ]; then
        echo "================================================================"
        echo "Batch $batch_num: launching suites $((batch_start + 1))-$batch_end"
        echo "================================================================"
    fi

    # Launch every suite in this batch
    BATCH_TAIL_PIDS=()
    for ((i=batch_start; i<batch_end; i++)); do
        run_suite "${MODULES[$i]}" "${SUITE_HOMES[$i]}" "${SUITE_BINS[$i]}" \
            "${SUITE_RPCS[$i]}" "${LOGS[$i]}" &
        SUITE_PIDS[$i]=$!
        CHILD_PIDS+=("${SUITE_PIDS[$i]}")
    done

    # Tails for the batch — only tailing live suites avoids tying up file
    # handles on logs whose suites haven't started yet.
    # Tail PIDs are NOT added to CHILD_PIDS: tails are short-lived (one batch),
    # we kill them explicitly at end-of-batch, and adding them to CHILD_PIDS
    # would let the array grow unboundedly across batches with stale PIDs.
    for ((i=batch_start; i<batch_end; i++)); do
        ( tail -F -n +1 "${LOGS[$i]}" 2>/dev/null | sed -u "s/^/[${MODULES[$i]}] /" ) &
        BATCH_TAIL_PIDS+=($!)
    done

    # Wait for each suite in the batch; capture its rc by suite index
    for ((i=batch_start; i<batch_end; i++)); do
        wait "${SUITE_PIDS[$i]}"
        SUITE_RCS[$i]=$?
    done

    # Drain tails before moving on. SIGTERM the whole tree (tail | sed
    # subshell + tail child) so the sed isn't left dangling on stdin EOF.
    sleep 1
    for tpid in "${BATCH_TAIL_PIDS[@]}"; do
        kill_tree "$tpid" TERM 2>/dev/null || true
    done
    # If any tail survives (rare, but the bash builtin `kill` is async), give
    # it 200ms then SIGKILL.
    sleep 0.2 2>/dev/null || sleep 1
    for tpid in "${BATCH_TAIL_PIDS[@]}"; do
        kill_tree "$tpid" KILL 2>/dev/null || true
    done

    batch_start=$batch_end
done

# Final summary
{
    RUN_END_EPOCH=$(timing_now_epoch)
    RUN_END_HUMAN=$(timing_now_human)
    RUN_DURATION=$(timing_format_duration $((RUN_END_EPOCH - RUN_START_EPOCH)))

    echo ""
    echo "================================================================"
    echo "FINAL RESULTS"
    echo "================================================================"
    printf "Started:  %s\n" "$RUN_START_HUMAN"
    printf "Ended:    %s\n" "$RUN_END_HUMAN"
    printf "Duration: %s  (wall-clock; suites run concurrently)\n" "$RUN_DURATION"
    echo "----------------------------------------------------------------"
    total_failures=0

    # Pre-compute per-suite durations for column alignment. Suites whose
    # .timing file is missing (e.g. crashed before run_suite finished)
    # show a "?" but don't break the layout.
    declare -a _suite_durs=()
    declare -a _suite_dur_strs=()
    declare -a _suite_starts=()
    declare -a _suite_ends=()
    max_dur_w=0
    for ((i=0; i<NUM_SUITES; i++)); do
        if [ -f "${LOGS[$i]}.timing" ]; then
            read -r s_start s_end < "${LOGS[$i]}.timing" || { s_start=""; s_end=""; }
            if [[ "$s_start" =~ ^[0-9]+$ ]] && [[ "$s_end" =~ ^[0-9]+$ ]]; then
                _suite_durs+=($((s_end - s_start)))
                _suite_dur_strs+=("$(timing_format_duration $((s_end - s_start)))")
                _suite_starts+=("$s_start")
                _suite_ends+=("$s_end")
            else
                _suite_durs+=(0)
                _suite_dur_strs+=("?")
                _suite_starts+=("")
                _suite_ends+=("")
            fi
        else
            _suite_durs+=(0)
            _suite_dur_strs+=("?")
            _suite_starts+=("")
            _suite_ends+=("")
        fi
        if [ ${#_suite_dur_strs[$i]} -gt $max_dur_w ]; then
            max_dur_w=${#_suite_dur_strs[$i]}
        fi
    done

    # Per-suite results table: status + duration + module name + log path.
    # Duration column is right-aligned to match other timing tables.
    for ((i=0; i<NUM_SUITES; i++)); do
        rc="${SUITE_RCS[$i]}"
        if [ "$rc" -eq 0 ]; then
            status="PASS"
        else
            status="FAIL"
            total_failures=$((total_failures + 1))
        fi
        printf "Suite %d  [%-4s]  %*s  %s\n" \
            "$((i + 1))" "$status" \
            "$max_dur_w" "${_suite_dur_strs[$i]}" \
            "${MODULES[$i]}"
        if [ "$rc" -ne 0 ]; then
            printf "                    -> %d failures\n" "$rc"
        fi
        printf "                    log: %s\n" "${LOGS[$i]}"
        printf "                         %s/%s.log  (latest symlink)\n" \
            "$LATEST_LINK" "${MODULES[$i]}"
    done
    echo "================================================================"
    echo ""
    # Frame the headline positively: how many suites came back PASS, with the
    # failure count surfaced alongside only when there is something to flag.
    # The script's exit code (set later from $total_failures) still drives CI;
    # this is purely about the human-facing summary line.
    suites_passed=$((NUM_SUITES - total_failures))
    if [ "$total_failures" -gt 0 ]; then
        echo "Suites passed: $suites_passed / $NUM_SUITES  (failed: $total_failures)"
    else
        echo "Suites passed: $suites_passed / $NUM_SUITES"
    fi
    echo "Total elapsed: $RUN_DURATION"
    echo ""
}

# Disable cleanup's exit so we exit with the aggregate code
trap - EXIT
cleanup_silently() {
    for pid in "${CHILD_PIDS[@]:-}"; do
        kill "$pid" 2>/dev/null || true
    done
}
cleanup_silently

# Post-run pruning: --keep N retains only the N most recent run dirs (always
# preserving the current run, regardless of pass/fail status — failure logs
# stay accessible, only OLDER runs get pruned). Live-PID dirs are always
# skipped, so concurrent runs against the same E2E_ROOT can't be clobbered.
if [ -n "$KEEP_N" ]; then
    echo ""
    echo "================================================================"
    echo "--keep $KEEP_N: pruning older run dirs..."
    echo "================================================================"
    e2e_keep_n "$KEEP_N" "$PARALLEL_DIR"
fi

# Exit nonzero if any suite failed
for rc in "${SUITE_RCS[@]}"; do
    [ "$rc" -ne 0 ] && exit 1
done
exit 0
