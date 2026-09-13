#!/bin/bash
# ============================================================================
# REST GATEWAY E2E TEST SUITE
# ============================================================================
# Covers the LCD surface produced by app/gateway_fix.go: that every wired
# module's routes reach the gateway mux, that the block pre-intercept
# answers, and that response field spelling matches what LCD clients parse.
#
# There is no module here -- this suite tests app-level API wiring, so it
# needs no accounts and broadcasts no transactions. It only needs a running
# chain with [api] enable = true, and skips with a warning otherwise.
#
# Usage:
#   ./run_all_tests.sh              # Run everything
#   ./run_all_tests.sh --no-lcd     # Skip the LCD route test
# ============================================================================
# TEST_ORDER: lcd_routes_test.sh

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

RUN_LCD=true

for arg in "$@"; do
    case $arg in
        --no-lcd) RUN_LCD=false ;;
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

if [ "$RUN_LCD" = "true" ]; then
    run_test "lcd-routes" "$SCRIPT_DIR/lcd_routes_test.sh"
fi

echo ""
echo "=========================================="
echo "SUMMARY: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
    echo "Failed tests:"
    for t in "${FAILED_TESTS[@]}"; do
        echo "  - $t"
    done
    exit 1
fi
echo "=========================================="
