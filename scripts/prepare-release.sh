#!/bin/bash
# ============================================================================
# PREPARE-RELEASE — orchestrate the prerelease checklist
# ============================================================================
# Runs the four mechanical steps that go into every version bump:
#
#   1. BUMP DEPLOY VERSIONS — rewrite v<old> -> v<new> in the seven deploy
#      files (3× sentry.sdl.yaml, 3× validator.sdl.yaml, deploy/docs/DEPLOYMENT.md).
#   2. AUDIT CONFIGS        — run deploy/scripts/audit-configs.py to catch any
#      stale/drifted module parameters. Aborts on ERROR; INFO is shown but
#      doesn't block.
#   3. REGEN GENESIS        — run deploy/scripts/regenerate-network-genesis.py
#      to refresh devnet/testnet/mainnet genesis.json files from the current
#      params_vals_<network>.go and config.yml files.
#   4. VERIFY GENESIS       — run `make verify-genesis` to run the per-network
#      and cross-network audit tests.
#   5. REFRESH E2E SNAPSHOTS — if root config.yml changed in this working tree
#      (vs HEAD), invalidate test/*/snapshots/post-setup so the next E2E run
#      rebuilds against the new config. The auto-snapshot helper in
#      test/_auto_snapshot.sh keys off setup_test_accounts.sh's SHA-256 only,
#      so config-only changes need explicit invalidation.
#
# This script does NOT commit. It prints a diff summary and a suggested commit
# message at the end so you can stage and commit yourself.
#
# Usage:
#   scripts/prepare-release.sh <new-version>             # full pipeline
#   scripts/prepare-release.sh <new-version> --no-bump   # skip step 1 (re-run from step 2)
#   scripts/prepare-release.sh <new-version> --no-regen  # skip step 3+4
#   scripts/prepare-release.sh <new-version> --strict    # treat audit WARN as failure
#   scripts/prepare-release.sh --help
#
# Examples:
#   scripts/prepare-release.sh v1.0.7
#   scripts/prepare-release.sh v1.0.7 --no-bump          # versions already bumped manually
#
# Environment:
#   SKIP_DIRTY_CHECK=1   bypass the "working tree dirty" pre-flight check.
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"
cd "$REPO_ROOT"

# --- color helpers ---------------------------------------------------------
if [ -t 1 ]; then
    BOLD='\033[1m'; RED='\033[31m'; GRN='\033[32m'; YLW='\033[33m'; CYN='\033[36m'; RST='\033[0m'
else
    BOLD=''; RED=''; GRN=''; YLW=''; CYN=''; RST=''
fi
section() { printf "\n${BOLD}${CYN}=== %s ===${RST}\n" "$*"; }
ok()      { printf "${GRN}[OK]${RST} %s\n" "$*"; }
warn()    { printf "${YLW}[WARN]${RST} %s\n" "$*"; }
fail()    { printf "${RED}[FAIL]${RST} %s\n" "$*" >&2; }
note()    { printf "${CYN}[NOTE]${RST} %s\n" "$*"; }

# --- arg parsing -----------------------------------------------------------
NEW_VERSION=""
DO_BUMP=true
DO_REGEN=true
STRICT=false

usage() {
    # Print every leading `# ...` comment line after the shebang, stripped
    # of the leading `# `. Stops at the first non-comment line.
    awk '/^#!/ {next} /^[^#]/ {exit} /^#/ {print}' "$0" | sed 's/^#\s\?//'
    exit 0
}

while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help) usage ;;
        --no-bump) DO_BUMP=false ;;
        --no-regen) DO_REGEN=false ;;
        --strict) STRICT=true ;;
        v[0-9]*.[0-9]*.[0-9]*)
            NEW_VERSION="$1"
            ;;
        *)
            fail "unknown argument: $1"
            echo "  run with --help for usage" >&2
            exit 2
            ;;
    esac
    shift
done

if [ -z "$NEW_VERSION" ] && [ "$DO_BUMP" = true ]; then
    fail "no version supplied (e.g. v1.0.7)"
    echo "  if you've already bumped versions manually, add --no-bump" >&2
    exit 2
fi

# --- pre-flight ------------------------------------------------------------
section "PRE-FLIGHT"

# Working tree dirty?
if [ -z "$SKIP_DIRTY_CHECK" ]; then
    if ! git diff --quiet --exit-code 2>/dev/null; then
        warn "working tree has unstaged changes — they'll be mixed in with this release's diff"
        echo "    (set SKIP_DIRTY_CHECK=1 to silence)"
    fi
    if ! git diff --cached --quiet --exit-code 2>/dev/null; then
        warn "you have STAGED changes — they'll be mixed in with this release's diff"
        echo "    (set SKIP_DIRTY_CHECK=1 to silence)"
    fi
else
    note "SKIP_DIRTY_CHECK=1 — skipping working-tree cleanliness check"
fi

# Any sparkdreamd running? Genesis regen builds binaries to /tmp; running chains
# don't conflict, but a parallel run could fight us if it tries to use ~/.sparkdream.
if pgrep -af 'sparkdreamd start --home /home' >/dev/null 2>&1 || \
   pgrep -af 'sparkdreamd start$' >/dev/null 2>&1; then
    if pgrep -af 'sparkdreamd start --home '"$HOME"'/.sparkdream' >/dev/null 2>&1; then
        warn "a chain is running with --home ~/.sparkdream — regen will fight it"
        echo "    stop it first: pkill -f 'sparkdreamd start --home $HOME/.sparkdream'"
    else
        note "sparkdreamd processes detected but not on ~/.sparkdream — fine"
    fi
fi

# Required tools
for cmd in python3 jq go make; do
    command -v "$cmd" >/dev/null 2>&1 || { fail "missing required tool: $cmd"; exit 2; }
done
python3 -c 'import yaml' 2>/dev/null || { fail "missing required Python module: yaml (pip install pyyaml)"; exit 2; }
ok "tools available"

# --- 1. bump deploy versions ------------------------------------------------
if [ "$DO_BUMP" = true ]; then
    section "1. BUMP DEPLOY VERSIONS"

    # The seven files that the prior version-bump commits (ea6ff66, 328e357,
    # 81c2741, 8ef40d6) all touched in lockstep.
    DEPLOY_FILES=(
        deploy/config/network/devnet/sentry.sdl.yaml
        deploy/config/network/devnet/validator.sdl.yaml
        deploy/config/network/mainnet/sentry.sdl.yaml
        deploy/config/network/mainnet/validator.sdl.yaml
        deploy/config/network/testnet/sentry.sdl.yaml
        deploy/config/network/testnet/validator.sdl.yaml
        deploy/docs/DEPLOYMENT.md
    )

    # Detect the current version from DEPLOYMENT.md so we don't blindly run a
    # global v*.* substitution (which would corrupt unrelated version strings).
    OLD_VERSION=$(grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' deploy/docs/DEPLOYMENT.md | head -1)
    if [ -z "$OLD_VERSION" ]; then
        fail "could not detect current version from deploy/docs/DEPLOYMENT.md"
        exit 1
    fi

    if [ "$OLD_VERSION" = "$NEW_VERSION" ]; then
        warn "current version is already $NEW_VERSION — skipping bump"
    else
        echo "  $OLD_VERSION -> $NEW_VERSION"
        for f in "${DEPLOY_FILES[@]}"; do
            if ! grep -qF "$OLD_VERSION" "$f"; then
                warn "  $f doesn't contain $OLD_VERSION — possibly already bumped, skipping"
                continue
            fi
            sed -i "s|${OLD_VERSION}|${NEW_VERSION}|g" "$f"
            echo "    [OK] $f"
        done

        # Sanity sweep: any stragglers?
        STRAGGLERS=$(grep -rln "$OLD_VERSION" deploy/ 2>/dev/null || true)
        if [ -n "$STRAGGLERS" ]; then
            fail "still found $OLD_VERSION in:"
            echo "$STRAGGLERS" | sed 's/^/      /' >&2
            echo "    fix manually or extend DEPLOY_FILES in this script" >&2
            exit 1
        fi
        ok "all deploy files bumped to $NEW_VERSION"
    fi
fi

# --- 2. audit configs -------------------------------------------------------
section "2. AUDIT CONFIGS"

AUDIT_ARGS=(--check-snapshots)
[ "$STRICT" = true ] && AUDIT_ARGS+=(--strict)

if deploy/scripts/audit-configs.py "${AUDIT_ARGS[@]}"; then
    ok "audit clean"
else
    rc=$?
    fail "audit reported issues (exit $rc) — fix before proceeding"
    echo "    re-run after fixing:  scripts/prepare-release.sh $NEW_VERSION --no-bump" >&2
    exit 1
fi

# --- 3. regenerate genesis --------------------------------------------------
if [ "$DO_REGEN" = true ]; then
    section "3. REGENERATE GENESIS"

    # The regenerate script builds 3 network-tagged binaries to /tmp and runs
    # `sparkdreamd init` per network. Takes 1-2 min total.
    if deploy/scripts/regenerate-network-genesis.py; then
        ok "genesis files regenerated"
    else
        fail "genesis regeneration failed"
        exit 1
    fi

    # --- 4. verify genesis --------------------------------------------------
    section "4. VERIFY GENESIS"

    if make verify-genesis; then
        ok "all per-network and cross-network audit tests pass"
    else
        fail "make verify-genesis failed — inspect output above"
        exit 1
    fi
fi

# --- 5. refresh e2e snapshots ----------------------------------------------
# Local E2E `post-setup` snapshots are gated on setup_test_accounts.sh's
# SHA-256 (test/_auto_snapshot.sh), not on config.yml — so config-only
# changes silently reuse stale snapshots. Invalidate when root config.yml
# was modified in this working tree relative to HEAD; otherwise keep
# snapshots warm so post-release E2E iteration stays fast.
section "5. REFRESH E2E SNAPSHOTS"

if git diff --quiet HEAD -- config.yml 2>/dev/null; then
    note "root config.yml unchanged vs HEAD — local E2E snapshots stay fresh, skipping clean"
else
    echo "  root config.yml changed — invalidating local E2E post-setup snapshots"
    if scripts/clean-test-snapshots.sh --quiet; then
        ok "snapshots invalidated; next per-module run_all_tests.sh will regenerate lazily"
    else
        warn "snapshot cleanup reported errors — see above; continuing"
    fi
fi

# --- summary ----------------------------------------------------------------
section "SUMMARY"

CHANGED=$(git status --short -- deploy/ 2>/dev/null | wc -l)
echo "  files changed in deploy/: $CHANGED"
git status --short -- deploy/ 2>/dev/null | sed 's/^/    /'

echo ""
ok "release $NEW_VERSION prepared. Suggested commit messages (split or combine):"
echo ""
if [ "$DO_BUMP" = true ]; then
    echo -e "  ${BOLD}Bump deploy images to $NEW_VERSION across all networks.${RST}"
fi
if [ "$DO_REGEN" = true ]; then
    echo -e "  ${BOLD}Regenerate genesis files to match $NEW_VERSION.${RST}"
fi
echo ""
note "NOT committing — stage and commit yourself."
