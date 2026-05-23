#!/bin/bash
# ============================================================================
# X/IDENTITY MODULE E2E TEST SUITE
# ============================================================================
# Runs all identity module e2e tests in sequence.
#
# Identity is genesis-only immutable and has no on-chain mutation surface;
# the suite primarily verifies queries, bank metadata seeding, SDK params
# alignment (sentinel rewrite), and the genesis CLI helpers.
#
# Usage:
#   ./run_all_tests.sh                  # Run everything
#   ./run_all_tests.sh --no-query       # Skip query tests
#   ./run_all_tests.sh --no-bank        # Skip bank metadata test
#   ./run_all_tests.sh --no-staking     # Skip SDK params alignment test
#   ./run_all_tests.sh --no-cli         # Skip genesis CLI test
#
# Prerequisites:
#   - sparkdreamd binary in PATH
#   - Chain running with x/identity module wired (any genesis identity is OK)
# ============================================================================
# TEST_ORDER: query_test.sh bank_metadata_test.sh staking_alignment_test.sh genesis_cli_test.sh federated_chain_test.sh

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"

RUN_QUERY=true
RUN_BANK=true
RUN_STAKING=true
RUN_CLI=true
RUN_FEDERATED=true

for arg in "$@"; do
    case $arg in
        --no-query)     RUN_QUERY=false ;;
        --no-bank)      RUN_BANK=false ;;
        --no-staking)   RUN_STAKING=false ;;
        --no-cli)       RUN_CLI=false ;;
        --no-federated) RUN_FEDERATED=false ;;
        --help)
            grep '^# ' "$0" | sed 's/^# //'
            exit 0
            ;;
        *)
            echo "unknown flag: $arg"
            exit 2
            ;;
    esac
done

PASS=0
FAIL=0
FAILED_TESTS=()
SKIPPED_TESTS=()

# Detect whether the running chain has identity configured. The CLI test
# (which uses its own --home) is always runnable; the chain-state tests
# (query, bank, staking) only run if `q identity chain-identity` returns a
# populated record.
CHAIN_HAS_IDENTITY=false
if $BINARY q identity chain-identity --output json 2>/dev/null | jq -e '.identity.bond_denom // ""' >/dev/null 2>&1; then
    BOND_DENOM_PROBE=$($BINARY q identity chain-identity --output json 2>/dev/null | jq -r '.identity.bond_denom // ""')
    if [ -n "$BOND_DENOM_PROBE" ] && [ "$BOND_DENOM_PROBE" != "null" ]; then
        CHAIN_HAS_IDENTITY=true
    fi
fi
echo "chain has identity configured: $CHAIN_HAS_IDENTITY"

run_test() {
    local NAME=$1
    local PATH_=$2
    echo ""
    echo "######################################################################"
    echo "# RUNNING: $NAME"
    echo "######################################################################"
    if bash "$PATH_"; then
        PASS=$((PASS+1))
    else
        FAIL=$((FAIL+1))
        FAILED_TESTS+=("$NAME")
    fi
}

skip_test() {
    local NAME=$1
    local REASON=$2
    echo "SKIP: $NAME ($REASON)"
    SKIPPED_TESTS+=("$NAME: $REASON")
}

if [ "$RUN_QUERY" = "true" ]; then
    if [ "$CHAIN_HAS_IDENTITY" = "true" ]; then
        run_test "queries" "$SCRIPT_DIR/query_test.sh"
    else
        skip_test "queries" "chain has no identity configured (single-chain legacy mode)"
    fi
fi
if [ "$RUN_BANK" = "true" ]; then
    if [ "$CHAIN_HAS_IDENTITY" = "true" ]; then
        run_test "bank-metadata" "$SCRIPT_DIR/bank_metadata_test.sh"
    else
        skip_test "bank-metadata" "chain has no identity configured"
    fi
fi
if [ "$RUN_STAKING" = "true" ]; then
    if [ "$CHAIN_HAS_IDENTITY" = "true" ]; then
        run_test "sdk-params-alignment" "$SCRIPT_DIR/staking_alignment_test.sh"
    else
        skip_test "sdk-params-alignment" "chain has no identity configured"
    fi
fi
if [ "$RUN_CLI" = "true" ]; then
    # The CLI test uses its own --home so it's independent of the running chain.
    run_test "genesis-cli" "$SCRIPT_DIR/genesis_cli_test.sh"
fi
if [ "$RUN_FEDERATED" = "true" ]; then
    # Full federated-chain end-to-end (also self-contained, isolated --home).
    run_test "federated-chain" "$SCRIPT_DIR/federated_chain_test.sh"
fi

echo ""
echo "=========================================="
echo "SUMMARY: $PASS passed, $FAIL failed, ${#SKIPPED_TESTS[@]} skipped"
if [ "${#SKIPPED_TESTS[@]}" -gt 0 ]; then
    echo "Skipped tests:"
    for t in "${SKIPPED_TESTS[@]}"; do
        echo "  - $t"
    done
fi
if [ "$FAIL" -gt 0 ]; then
    echo "Failed tests:"
    for t in "${FAILED_TESTS[@]}"; do
        echo "  - $t"
    done
    exit 1
fi
echo "=========================================="
