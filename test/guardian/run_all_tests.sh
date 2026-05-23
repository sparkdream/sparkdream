#!/bin/bash
# ============================================================================
# X/GUARDIAN MODULE E2E TEST SUITE
# ============================================================================
# Runs all guardian filter tests in sequence on a live chain. Each test
# submits a batch of gov proposals, waits ONE voting cycle (~75s), and
# checks the resulting proposal statuses. Total wall time scales with the
# number of *_test.sh files, not with the number of proposals per file.
#
# Usage:
#   ./run_all_tests.sh                # Run everything
#   ./run_all_tests.sh --only-allowlist
#   ./run_all_tests.sh --only-mint
#   ./run_all_tests.sh --no-consensus  # Skip the consensus test
#
# Prerequisites:
#   - sparkdreamd binary in PATH (testparams build for fast voting period)
#   - Chain running, alice + bob in keyring with stake
#   - x/identity wired (resolves BOND_DENOM / DREAM_DENOM)
#   - x/guardian wired (resolves GUARDIAN_ADDR, owns the allowlist)
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Tests in declaration order. Each entry is the script's basename
# (without the .sh). All tests share the same chain instance.
TESTS=(
    allowlist
    mint_filter
    staking_filter
    bank_filter
    distribution_filter
    gov_filter
    slashing_filter
    auth_filter
    consensus_filter
)

# Parse flags. --only-NAME runs exactly that one; --no-NAME excludes it.
ONLY=""
declare -A SKIP=()
for arg in "$@"; do
    case "$arg" in
        --only-*)  ONLY="${arg#--only-}" ;;
        --no-*)    SKIP["${arg#--no-}"]=1 ;;
        --help|-h)
            grep '^# ' "$0" | sed 's/^# //'
            exit 0
            ;;
        *)
            echo "unknown flag: $arg" >&2
            exit 2
            ;;
    esac
done

PASS=0
FAIL=0
FAILED=()

run_one() {
    local name="$1"
    local path="$SCRIPT_DIR/${name}_test.sh"
    if [ ! -f "$path" ]; then
        echo "[skip] $name: $path missing"
        return 0
    fi
    echo ""
    echo "######################################################################"
    echo "# RUNNING: guardian/$name"
    echo "######################################################################"
    if bash "$path"; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        FAILED+=("$name")
    fi
}

for t in "${TESTS[@]}"; do
    if [ -n "$ONLY" ] && [ "$ONLY" != "$t" ]; then
        continue
    fi
    if [ -n "${SKIP[$t]:-}" ]; then
        echo "[skip] guardian/$t (--no-$t)"
        continue
    fi
    run_one "$t"
done

echo ""
echo "=========================================="
echo "GUARDIAN SUITE: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
    echo "Failed tests:"
    for f in "${FAILED[@]}"; do
        echo "  - $f"
    done
    exit 1
fi
echo "=========================================="
