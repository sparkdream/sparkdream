#!/bin/bash

# ============================================================================
# X/SERVICE MODULE E2E TEST SUITE
# ============================================================================
# This script runs all service module e2e tests in sequence.
#
# Usage:
#   ./run_all_tests.sh                  # Run all tests
#   ./run_all_tests.sh --no-setup       # Skip account setup
#   ./run_all_tests.sh --no-register    # Skip register tests
#   ./run_all_tests.sh --no-lifecycle   # Skip lifecycle tests
#   ./run_all_tests.sh --no-report      # Skip report tests
#
# Prerequisites:
#   - sparkdreamd chain running locally (built with `testparams` tag)
#   - Alice account with SPARK + DREAM (genesis member)
#   - x/rep, x/commons, x/distribution functional
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../check_testparams.sh"
source "$SCRIPT_DIR/../_timing.sh"
BINARY="sparkdreamd"

SUITE_START_EPOCH=$(timing_now_epoch)
SUITE_START_HUMAN=$(timing_now_human)

# Parse command line arguments
RUN_SETUP=true
RUN_REGISTER=true
RUN_LIFECYCLE=true
RUN_REPORT=true
RUN_TESTS=true
SAVE_SETUP=false
RESTORE_SETUP=false
AUTO_SNAPSHOT=true

for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-register)
            RUN_REGISTER=false
            ;;
        --no-lifecycle)
            RUN_LIFECYCLE=false
            ;;
        --no-report)
            RUN_REPORT=false
            ;;
        --only-setup)
            RUN_REGISTER=false
            RUN_LIFECYCLE=false
            RUN_REPORT=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_REGISTER=false
            RUN_LIFECYCLE=false
            RUN_REPORT=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_TESTS=false
            RUN_REGISTER=false
            RUN_LIFECYCLE=false
            RUN_REPORT=false
            ;;
        --no-auto-snapshot)
            AUTO_SNAPSHOT=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup           Skip account setup"
            echo "  --no-register        Skip register tests"
            echo "  --no-lifecycle       Skip lifecycle tests"
            echo "  --no-report          Skip report tests"
            echo "  --only-setup         Run only setup (skip all tests)"
            echo "  --save-setup         Run setup, save chain state, then exit"
            echo "  --restore-setup      Restore saved setup state, then run tests"
            echo "  --no-auto-snapshot   Disable auto-snapshot (run setup every time, no caching)"
            echo "  --no-tests           Skip all tests (use with --restore-setup for manual testing)"
            echo "  --help, -h           Show this help message"
            echo ""
            echo "Workflow for fast iteration:"
            echo "  1. bash $0 --save-setup      # One-time: run setup and save state"
            echo "  2. bash $0 --restore-setup   # Restore and run tests (repeatable)"
            echo ""
            echo "Workflow for manual testing:"
            echo "  bash $0 --restore-setup --no-tests  # Restore state, start chain, exit"
            exit 0
            ;;
        *)
            echo "Unknown option: $arg"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Auto-snapshot integration (see test/_auto_snapshot.sh).
source "$SCRIPT_DIR/../_auto_snapshot.sh"
auto_snapshot_pre

echo "============================================================================"
echo "                    X/SERVICE MODULE E2E TEST SUITE"
echo "============================================================================"
echo ""

# ============================================================================
# Pre-flight checks
# ============================================================================
echo "--- PRE-FLIGHT CHECKS ---"
echo ""

if ! command -v $BINARY &> /dev/null; then
    echo "ERROR: $BINARY not found in PATH"
    exit 1
fi
echo "  Binary: OK ($BINARY)"

if [ "$RESTORE_SETUP" = true ]; then
    echo "  Restore mode: Chain will be stopped and restarted during restore"
else
    if ! $BINARY status &> /dev/null; then
        echo "ERROR: Chain is not running"
        echo "  Start chain: $BINARY start"
        exit 1
    fi
    echo "  Chain: OK (running)"

    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null || echo "")
    if [ -z "$ALICE_ADDR" ]; then
        echo "ERROR: Alice account not found"
        echo "  Create: $BINARY keys add alice --keyring-backend test"
        exit 1
    fi
    echo "  Alice: OK ($ALICE_ADDR)"

    ALICE_BALANCE=$($BINARY query bank balances $ALICE_ADDR --output json 2>/dev/null | jq -r '.balances[] | select(.denom=="uspark") | .amount' || echo "0")
    if [ "$ALICE_BALANCE" -lt 1000000 ]; then
        echo "WARNING: Alice has low SPARK balance: $ALICE_BALANCE uspark"
    fi
    echo "  Balance: $ALICE_BALANCE uspark"
fi

echo ""
echo "Pre-flight checks passed!"
echo ""

# ============================================================================
# Restore Setup (if requested)
# ============================================================================
if [ "$RESTORE_SETUP" = true ]; then
    echo "============================================================================"
    echo "RESTORING SAVED SETUP STATE"
    echo "============================================================================"
    echo ""

    SNAPSHOT_PATH="$SCRIPT_DIR/snapshots/post-setup"
    RESTORE_SCRIPT="$SNAPSHOT_PATH/restore.sh"

    if [ ! -f "$RESTORE_SCRIPT" ]; then
        echo "Snapshot 'post-setup' not found at: $SNAPSHOT_PATH"
        echo "   Run with --save-setup first to create the snapshot"
        exit 1
    fi

    echo "Restoring chain state from 'post-setup' snapshot..."
    echo "Snapshot location: $SNAPSHOT_PATH"
    echo ""

    bash "$RESTORE_SCRIPT"
    RESTORE_EXIT_CODE=$?
    if [ $RESTORE_EXIT_CODE -ne 0 ]; then
        echo "Failed to restore setup state (exit code: $RESTORE_EXIT_CODE)"
        exit 1
    fi

    echo ""
    echo "Setup state restored successfully"
    echo ""

    if [ -f "$SCRIPT_DIR/.test_env" ]; then
        source "$SCRIPT_DIR/.test_env"
        echo "Loaded test environment from restored snapshot"
    else
        echo "Warning: .test_env not found in restored snapshot"
    fi

    echo ""
    echo "Starting chain..."

    $BINARY start --home ~/.sparkdream > /tmp/chain_after_restore.log 2>&1 &
    CHAIN_PID=$!

    echo "   Chain starting in background (PID: $CHAIN_PID)"
    echo "   Waiting for chain to be ready..."

    MAX_ATTEMPTS=30
    ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        if $BINARY status &> /dev/null; then
            BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
            echo "Chain is running (block height: $BLOCK_HEIGHT)"
            break
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    if ! $BINARY status &> /dev/null; then
        echo "Chain failed to start after 30 seconds"
        echo "   Check logs: tail -f /tmp/chain_after_restore.log"
        exit 1
    fi

    echo ""

    if [ "$RUN_TESTS" != true ]; then
        BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
        echo "============================================================================"
        echo "  RESTORE COMPLETE — TEST EXECUTION SKIPPED (--no-tests)"
        echo "============================================================================"
        echo ""
        echo "  Chain home:   $HOME/.sparkdream"
        echo "  Block height: $BLOCK_HEIGHT"
        echo "  Test env:     $SCRIPT_DIR/.test_env  (already sourced)"
        echo "  Chain log:    /tmp/chain_after_restore.log"
        echo ""
        echo "  Run any specific test against the freshly restored state:"
        echo "    bash $SCRIPT_DIR/register_test.sh"
        echo "    bash $SCRIPT_DIR/lifecycle_test.sh"
        echo "    bash $SCRIPT_DIR/report_test.sh"
        echo ""
        echo "  Stop the chain when done:"
        echo "    pkill -f 'sparkdreamd start --home $HOME/.sparkdream'"
        echo ""
        exit 0
    fi
fi

# ============================================================================
# Test Results Tracking
# ============================================================================
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
declare -a FAILED_TESTS

declare -a TIMED_NAMES=()
declare -a TIMED_RESULTS=()
declare -a TIMED_DURATIONS_S=()

run_test() {
    local TEST_NAME=$1
    local TEST_SCRIPT=$2

    echo "============================================================================"
    echo "RUNNING: $TEST_NAME"
    echo "============================================================================"
    echo ""

    TESTS_RUN=$((TESTS_RUN + 1))

    local _t0 _t1 _dur_s _dur
    _t0=$(timing_now_epoch)
    if bash "$SCRIPT_DIR/$TEST_SCRIPT"; then
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        TESTS_PASSED=$((TESTS_PASSED + 1))
        TIMED_NAMES+=("$TEST_NAME")
        TIMED_RESULTS+=("PASS")
        TIMED_DURATIONS_S+=("$_dur_s")
        echo ""
        echo ">>> $TEST_NAME: PASSED ($_dur) <<<"
    else
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        TESTS_FAILED=$((TESTS_FAILED + 1))
        FAILED_TESTS+=("$TEST_NAME")
        TIMED_NAMES+=("$TEST_NAME")
        TIMED_RESULTS+=("FAIL")
        TIMED_DURATIONS_S+=("$_dur_s")
        echo ""
        echo ">>> $TEST_NAME: FAILED ($_dur) <<<"
    fi

    echo ""
    sleep 2
}

# ============================================================================
# Run Tests
# ============================================================================

# Setup (always first if enabled)
if [ "$RUN_SETUP" = true ]; then
    run_test "Account Setup" "setup_test_accounts.sh"

    # Auto-save the post-setup snapshot if AUTO_SNAPSHOT was set and
    # no fresh snapshot existed at the start of this run.
    auto_snapshot_post

    if [ "$SAVE_SETUP" = true ]; then
        echo "============================================================================"
        echo "SAVING CHAIN STATE"
        echo "============================================================================"
        echo ""

        SNAPSHOT_SCRIPT="$SCRIPT_DIR/../snapshot_datadir.sh"
        if [ ! -f "$SNAPSHOT_SCRIPT" ]; then
            echo "snapshot_datadir.sh not found at $SNAPSHOT_SCRIPT"
            echo "   Cannot save chain state"
            exit 1
        fi

        echo "Saving chain state to 'post-setup' snapshot..."
        bash "$SNAPSHOT_SCRIPT" post-setup "$SCRIPT_DIR/snapshots"
        SAVE_EXIT_CODE=$?
        if [ $SAVE_EXIT_CODE -ne 0 ]; then
            echo "Failed to save chain state (exit code: $SAVE_EXIT_CODE)"
            exit 1
        fi

        echo ""
        echo "============================================================================"
        echo "SAVE-SETUP MODE COMPLETE"
        echo "============================================================================"
        echo ""
        echo "Setup completed and chain state saved to 'post-setup' snapshot"
        echo ""
        echo "Snapshot location: $SCRIPT_DIR/snapshots/post-setup"
        echo ""
        echo "To run tests from this saved state:"
        echo "  bash test/service/run_all_tests.sh --restore-setup"
        echo ""
        exit 0
    fi
else
    echo "Skipping account setup (--no-setup)"
    echo ""

    if [ "$RESTORE_SETUP" != true ] && [ ! -f "$SCRIPT_DIR/.test_env" ]; then
        echo "Warning: Test environment not found (.test_env missing)"
        echo "   Run without --no-setup flag to create it"
    fi
fi

# Order matters:
#   register_test  — leaves operator1 + operator2 ACTIVE
#   lifecycle_test — exercises operator2 through UNBOND → RETIRED
#   report_test    — files reports against operator1 (still ACTIVE)

if [ "$RUN_REGISTER" = true ]; then
    run_test "Register Tests" "register_test.sh"
else
    echo "Skipping register tests (--no-register)"
    echo ""
fi

if [ "$RUN_LIFECYCLE" = true ]; then
    run_test "Lifecycle Tests" "lifecycle_test.sh"
else
    echo "Skipping lifecycle tests (--no-lifecycle)"
    echo ""
fi

if [ "$RUN_REPORT" = true ]; then
    run_test "Report Tests" "report_test.sh"
else
    echo "Skipping report tests (--no-report)"
    echo ""
fi

# ============================================================================
# Final Summary
# ============================================================================
echo "============================================================================"
echo "                         TEST SUITE SUMMARY"
echo "============================================================================"
echo ""

SUITE_END_EPOCH=$(timing_now_epoch)
SUITE_END_HUMAN=$(timing_now_human)
timing_print_summary_block "$SUITE_START_EPOCH" "$SUITE_END_EPOCH" \
    "$SUITE_START_HUMAN" "$SUITE_END_HUMAN"
echo ""

echo "  Tests Run:    $TESTS_RUN"
echo "  Tests Passed: $TESTS_PASSED"
echo "  Tests Failed: $TESTS_FAILED"
echo ""

if [ ${#TIMED_NAMES[@]} -gt 0 ]; then
    timing_print_per_test_table TIMED_RESULTS TIMED_DURATIONS_S TIMED_NAMES
    echo ""
fi

if [ $TESTS_FAILED -gt 0 ]; then
    echo "Failed Tests:"
    for test in "${FAILED_TESTS[@]}"; do
        echo "  - $test"
    done
    echo ""
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi

echo ""
echo "============================================================================"
echo ""
