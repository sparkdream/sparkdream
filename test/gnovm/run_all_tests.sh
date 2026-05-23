#!/bin/bash

# ============================================================================
# X/GNOVM MODULE E2E TEST SUITE
# ============================================================================
# Runs all GnoVM module e2e tests in sequence.
#
# Usage:
#   ./run_all_tests.sh              # Run all tests
#   ./run_all_tests.sh --no-setup   # Skip account setup
#
# Prerequisites:
#   - sparkdreamd chain running locally (with testparams tag)
#   - Alice account with SPARK funds
#   - GnoVM module configured (sysnames_pkgpath empty in config.yml)
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"
source "$SCRIPT_DIR/../check_testparams.sh"
source "$SCRIPT_DIR/../_timing.sh"
BINARY="sparkdreamd"

# Wall-clock timing for the suite (Started/Ended/Duration in summary).
SUITE_START_EPOCH=$(timing_now_epoch)
SUITE_START_HUMAN=$(timing_now_human)

# Parse command line arguments
RUN_SETUP=true
RUN_COUNTER=true

for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-counter)
            RUN_COUNTER=false
            ;;
    esac
done

echo "=============================================="
echo "  X/GNOVM E2E TEST SUITE"
echo "=============================================="
echo ""

TOTAL_PASS=0
TOTAL_FAIL=0

# --- Setup ---
if [ "$RUN_SETUP" = true ]; then
    echo ">>> Running setup..."
    bash "$SCRIPT_DIR/setup_test_accounts.sh"
    echo ""
fi

# --- Counter tests ---
if [ "$RUN_COUNTER" = true ]; then
    echo ">>> Running counter tests..."
    if bash "$SCRIPT_DIR/counter_test.sh"; then
        echo "  >> counter_test.sh: ALL PASSED"
    else
        echo "  >> counter_test.sh: SOME FAILED"
        TOTAL_FAIL=$((TOTAL_FAIL + 1))
    fi
    echo ""
fi

echo "=============================================="
echo "  X/GNOVM E2E TEST SUITE COMPLETE"
echo "=============================================="
echo ""
SUITE_END_EPOCH=$(timing_now_epoch)
SUITE_END_HUMAN=$(timing_now_human)
timing_print_summary_block "$SUITE_START_EPOCH" "$SUITE_END_EPOCH" \
    "$SUITE_START_HUMAN" "$SUITE_END_HUMAN"
echo ""

if [ $TOTAL_FAIL -gt 0 ]; then
    echo "  SOME TEST FILES FAILED"
    exit 1
fi
echo "  ALL TEST FILES PASSED"
exit 0
