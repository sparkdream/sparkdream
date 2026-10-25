#!/bin/bash
# ============================================================================
# X/VAULT MODULE E2E TEST SUITE
# ============================================================================
# Shared vaults (docs/x-vault-spec.md): keyless module-derived accounts with
# weighted rotatable signers; proposals approved on chain by ordinary txs.
# Runs on a live chain with alice and bob in the test keyring; no setup
# script and no snapshot (every test creates its own vaults).
#
# Usage:
#   ./run_all_tests.sh                # Run everything
#   ./run_all_tests.sh --only-vault   # Run one test by basename
#   ./run_all_tests.sh --no-vault     # Skip one test
#
# Prerequisites:
#   - sparkdreamd binary in PATH (testparams build: MinProposalTTL=10s,
#     ProposalRetention=60s so expiry and pruning are observable)
#   - Chain running, alice + bob in keyring with stake
# ============================================================================

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

TESTS=(
    vault
)

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
    echo "# RUNNING: vault/$name"
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
        echo "[skip] vault/$t (--no-$t)"
        continue
    fi
    run_one "$t"
done

echo ""
echo "=========================================="
echo "VAULT SUITE: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
    echo "Failed tests:"
    for f in "${FAILED[@]}"; do
        echo "  - $f"
    done
    exit 1
fi
echo "=========================================="
