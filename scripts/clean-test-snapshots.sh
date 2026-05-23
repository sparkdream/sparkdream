#!/usr/bin/env bash
# ============================================================================
# CLEAN-TEST-SNAPSHOTS — purge per-module E2E `post-setup` snapshots.
# ============================================================================
# Removes test/<module>/snapshots/post-setup/ directories so the next run of
# any module's run_all_tests.sh forces a fresh setup_test_accounts.sh + save.
#
# Use this after changes that affect the snapshot's chain state but leave
# setup_test_accounts.sh unchanged (so the SHA-256 hash gate in
# test/_auto_snapshot.sh would otherwise reuse a stale snapshot). Examples:
#   - config.yml category_map / commons params changes
#   - module DefaultParams() / DefaultGenesis() changes
#   - new genesis-handle assignments in x/name
#   - x/session ceiling expansions
#   - any deploy/config/network/*/config.yml change that ripples through
#     deploy/scripts/regenerate-network-genesis.py
#
# Snapshots regenerate lazily — this script does NOT rebuild them. The next
# `bash test/<module>/run_all_tests.sh` invocation (without --restore-setup)
# will pick up the missing snapshot, run setup, and save fresh.
#
# Usage:
#   scripts/clean-test-snapshots.sh                  # all modules
#   scripts/clean-test-snapshots.sh forum rep        # only listed modules
#   scripts/clean-test-snapshots.sh --dry-run        # show what would be removed
#   scripts/clean-test-snapshots.sh --include-stale  # also purge post-setup.stale-*
#   scripts/clean-test-snapshots.sh --quiet          # no per-dir log lines
#   scripts/clean-test-snapshots.sh --help
#
# Exit status:
#   0 — success (including the no-snapshots-present case)
#   1 — invalid arguments or safe_rmtree failure
#
# Honors the project's "Never use rm -rf" convention (see
# docs/development-conventions.md) via test/_safe_rm.sh's safe_rmtree.
# ============================================================================

set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"

# shellcheck source=../test/_safe_rm.sh
source "$REPO_ROOT/test/_safe_rm.sh"

# --- color helpers (only when stdout is a TTY) -----------------------------
if [ -t 1 ]; then
    BOLD=$'\033[1m'; DIM=$'\033[2m'; RED=$'\033[31m'; GRN=$'\033[32m'; YLW=$'\033[33m'; RST=$'\033[0m'
else
    BOLD=''; DIM=''; RED=''; GRN=''; YLW=''; RST=''
fi

# --- defaults --------------------------------------------------------------
DRY_RUN=false
QUIET=false
INCLUDE_STALE=false
MODULES=()

usage() {
    # Print every leading `#`-comment line (skipping the shebang) until we
    # hit the first non-comment line. Strips the leading `# ` so the help
    # text reads as plain prose.
    awk 'NR==1{next} /^[^#]/{exit} {sub(/^# ?/, ""); print}' "$0"
    exit 0
}

# --- argument parsing ------------------------------------------------------
while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help)
            usage
            ;;
        -n|--dry-run)
            DRY_RUN=true
            shift
            ;;
        -q|--quiet)
            QUIET=true
            shift
            ;;
        --include-stale)
            INCLUDE_STALE=true
            shift
            ;;
        --)
            shift
            while [ $# -gt 0 ]; do MODULES+=("$1"); shift; done
            ;;
        -*)
            echo "${RED}error:${RST} unknown flag '$1' (use --help)" >&2
            exit 1
            ;;
        *)
            MODULES+=("$1")
            shift
            ;;
    esac
done

# --- discover snapshot dirs ------------------------------------------------
# Build a list of paths to remove. We glob from REPO_ROOT (not cwd) so the
# script works correctly regardless of where it's invoked from.
declare -a TARGETS=()

add_if_dir() {
    local d="$1"
    if [ -d "$d" ]; then
        TARGETS+=("$d")
    fi
    # Always return 0 so the for-loop's `set -e` doesn't abort when an
    # unmatched glob leaves us with a literal pattern that's not a directory.
    return 0
}

if [ ${#MODULES[@]} -eq 0 ]; then
    # No module filter — every test/<module>/snapshots/post-setup/ dir.
    for d in "$REPO_ROOT"/test/*/snapshots/post-setup; do
        add_if_dir "$d"
    done
    if $INCLUDE_STALE; then
        for d in "$REPO_ROOT"/test/*/snapshots/post-setup.stale-*; do
            add_if_dir "$d"
        done
    fi
else
    # Module filter — accept names with or without `test/` prefix.
    for m in "${MODULES[@]}"; do
        m="${m#test/}"
        m="${m%/}"
        if [ ! -d "$REPO_ROOT/test/$m" ]; then
            echo "${RED}error:${RST} no such module: test/$m" >&2
            exit 1
        fi
        add_if_dir "$REPO_ROOT/test/$m/snapshots/post-setup"
        if $INCLUDE_STALE; then
            for d in "$REPO_ROOT/test/$m"/snapshots/post-setup.stale-*; do
                add_if_dir "$d"
            done
        fi
    done
fi

# --- act -------------------------------------------------------------------
if [ ${#TARGETS[@]} -eq 0 ]; then
    $QUIET || echo "${DIM}No snapshot directories found — nothing to clean.${RST}"
    exit 0
fi

if $DRY_RUN; then
    echo "${BOLD}Would remove ${#TARGETS[@]} snapshot dir(s):${RST}"
    for d in "${TARGETS[@]}"; do
        echo "  ${DIM}${d#$REPO_ROOT/}${RST}"
    done
    exit 0
fi

removed=0
failed=0
for d in "${TARGETS[@]}"; do
    rel="${d#$REPO_ROOT/}"
    if safe_rmtree "$d"; then
        removed=$((removed + 1))
        $QUIET || echo "  ${GRN}removed${RST} $rel"
    else
        failed=$((failed + 1))
        echo "  ${RED}failed${RST}  $rel" >&2
    fi
done

if [ "$failed" -gt 0 ]; then
    echo "${RED}${failed} dir(s) failed to remove; ${removed} succeeded.${RST}" >&2
    exit 1
fi

$QUIET || echo "${BOLD}Removed ${removed} snapshot dir(s).${RST} Next per-module run_all_tests.sh will regenerate lazily."
