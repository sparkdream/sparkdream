#!/bin/bash

# ============================================================================
# X/NAME MODULE E2E TEST SUITE
# ============================================================================
# This script runs all name module e2e tests in sequence.
#
# Usage:
#   ./run_all_tests.sh                    # Run all tests
#   ./run_all_tests.sh --no-setup         # Skip account setup
#   ./run_all_tests.sh --no-genesis       # Skip genesis-handles test
#   ./run_all_tests.sh --no-registration  # Skip name registration test
#   ./run_all_tests.sh --no-primary       # Skip primary-name test
#   ./run_all_tests.sh --no-target        # Skip target & transfer test
#   ./run_all_tests.sh --no-display       # Skip display-name test
#   ./run_all_tests.sh --no-dispute       # Skip dispute resolution test
#   ./run_all_tests.sh --no-params        # Skip operational params test
#   ./run_all_tests.sh --save-setup       # Run setup, save chain state, exit
#   ./run_all_tests.sh --restore-setup    # Restore saved state, run tests
#   ./run_all_tests.sh --help
#
# Prerequisites:
#   - sparkdreamd binary in PATH (built with testparams build tag)
#   - Genesis accounts alice, bob, carol in keyring-test
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../check_testparams.sh"
source "$SCRIPT_DIR/../_timing.sh"
BINARY="sparkdreamd"

# Wall-clock timing for the suite — captured here so the summary's
# "Started" line reflects when the runner was invoked, not when the
# first test fired.
SUITE_START_EPOCH=$(timing_now_epoch)
SUITE_START_HUMAN=$(timing_now_human)

# ============================================================================
# Default flags
# ============================================================================
RUN_SETUP=true
RUN_GENESIS=true
RUN_REGISTRATION=true
RUN_PRIMARY=true
RUN_TARGET=true
RUN_DISPLAY=true
RUN_DISPUTE=true
RUN_PARAMS=true
SAVE_SETUP=false
RESTORE_SETUP=false
AUTO_SNAPSHOT=true

for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-genesis)
            RUN_GENESIS=false
            ;;
        --no-registration)
            RUN_REGISTRATION=false
            ;;
        --no-primary)
            RUN_PRIMARY=false
            ;;
        --no-target)
            RUN_TARGET=false
            ;;
        --no-display)
            RUN_DISPLAY=false
            ;;
        --no-dispute)
            RUN_DISPUTE=false
            ;;
        --no-params)
            RUN_PARAMS=false
            ;;
        --only-setup)
            RUN_GENESIS=false
            RUN_REGISTRATION=false
            RUN_PRIMARY=false
            RUN_TARGET=false
            RUN_DISPLAY=false
            RUN_DISPUTE=false
            RUN_PARAMS=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_GENESIS=false
            RUN_REGISTRATION=false
            RUN_PRIMARY=false
            RUN_TARGET=false
            RUN_DISPLAY=false
            RUN_DISPUTE=false
            RUN_PARAMS=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_GENESIS=false
            RUN_REGISTRATION=false
            RUN_PRIMARY=false
            RUN_TARGET=false
            RUN_DISPLAY=false
            RUN_DISPUTE=false
            RUN_PARAMS=false
            ;;
        --no-auto-snapshot)
            AUTO_SNAPSHOT=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup            Skip setup_test_accounts.sh"
            echo "  --no-genesis          Skip genesis_handles_test.sh"
            echo "  --no-registration     Skip name_registration_test.sh"
            echo "  --no-primary          Skip primary_name_test.sh"
            echo "  --no-target           Skip target_and_transfer_test.sh"
            echo "  --no-display          Skip display_name_test.sh"
            echo "  --no-dispute          Skip dispute_resolution_test.sh"
            echo "  --no-params           Skip operational_params_test.sh"
            echo "  --only-setup          Run only setup (skip all tests)"
            echo "  --no-tests            Skip every test (use with --restore-setup)"
            echo ""
            echo "Snapshot flags:"
            echo "  --save-setup          Run setup, save chain state, then exit"
            echo "  --restore-setup       Restore saved state, then run tests"
            echo "  --no-auto-snapshot    Disable auto-snapshot (run setup every time)"
            echo ""
            echo "  --help, -h            Show this help message"
            echo ""
            echo "Workflow for fast iteration:"
            echo "  1. bash $0 --save-setup      # One-time: run setup and save state"
            echo "  2. bash $0 --restore-setup   # Restore and run tests (repeatable)"
            exit 0
            ;;
        *)
            echo "Unknown option: $arg"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# ============================================================================
# Auto-snapshot
# ============================================================================
source "$SCRIPT_DIR/../_auto_snapshot.sh"
auto_snapshot_pre

echo "============================================================================"
echo "                    X/NAME MODULE E2E TEST SUITE"
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
        echo "  Start chain: $BINARY start --home ~/.sparkdream"
        exit 1
    fi
    echo "  Chain: OK (running)"

    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null || echo "")
    if [ -z "$ALICE_ADDR" ]; then
        echo "ERROR: Alice account not found"
        exit 1
    fi
    echo "  Alice: OK ($ALICE_ADDR)"
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
    bash "$RESTORE_SCRIPT"
    RESTORE_EXIT_CODE=$?

    if [ $RESTORE_EXIT_CODE -ne 0 ]; then
        echo "Failed to restore setup state (exit code: $RESTORE_EXIT_CODE)"
        exit 1
    fi

    echo "Setup state restored successfully"
    echo ""

    if [ -f "$SCRIPT_DIR/.test_env" ]; then
        source "$SCRIPT_DIR/.test_env"
        echo "Loaded test environment from restored snapshot"
    fi

    echo ""
    echo "Starting chain..."
    $BINARY start --home ~/.sparkdream > /tmp/chain_after_restore.log 2>&1 &
    CHAIN_PID=$!
    echo "   Chain starting in background (PID: $CHAIN_PID)"

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
fi

# ============================================================================
# Test Results Tracking
# ============================================================================
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
declare -a FAILED_TESTS

# Per-test timing — populated by run_test, consumed by the summary block.
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
# Setup
# ============================================================================
if [ "$RUN_SETUP" = true ]; then
    run_test "Account Setup" "setup_test_accounts.sh"
    auto_snapshot_post

    if [ "$SAVE_SETUP" = true ]; then
        echo "============================================================================"
        echo "SAVING CHAIN STATE"
        echo "============================================================================"

        SNAPSHOT_SCRIPT="$SCRIPT_DIR/../snapshot_datadir.sh"
        if [ ! -f "$SNAPSHOT_SCRIPT" ]; then
            echo "snapshot_datadir.sh not found at $SNAPSHOT_SCRIPT"
            exit 1
        fi

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
        echo "Snapshot location: $SCRIPT_DIR/snapshots/post-setup"
        echo ""
        echo "To run tests from this saved state:"
        echo "  bash test/name/run_all_tests.sh --restore-setup"
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

# ============================================================================
# Run Tests
# ============================================================================
if [ "$RUN_GENESIS" = true ]; then
    run_test "Genesis Handles"            "genesis_handles_test.sh"
else
    echo "Skipping genesis-handles test (--no-genesis)"
    echo ""
fi

if [ "$RUN_REGISTRATION" = true ]; then
    run_test "Name Registration"          "name_registration_test.sh"
else
    echo "Skipping name-registration test (--no-registration)"
    echo ""
fi

if [ "$RUN_PRIMARY" = true ]; then
    run_test "Primary Name"               "primary_name_test.sh"
else
    echo "Skipping primary-name test (--no-primary)"
    echo ""
fi

if [ "$RUN_TARGET" = true ]; then
    run_test "Target & Transfer"          "target_and_transfer_test.sh"
else
    echo "Skipping target/transfer test (--no-target)"
    echo ""
fi

if [ "$RUN_DISPLAY" = true ]; then
    run_test "Display Name"               "display_name_test.sh"
else
    echo "Skipping display-name test (--no-display)"
    echo ""
fi

if [ "$RUN_DISPUTE" = true ]; then
    run_test "Dispute Resolution"         "dispute_resolution_test.sh"
else
    echo "Skipping dispute-resolution test (--no-dispute)"
    echo ""
fi

if [ "$RUN_PARAMS" = true ]; then
    run_test "Operational Params"         "operational_params_test.sh"
else
    echo "Skipping operational-params test (--no-params)"
    echo ""
fi

# ============================================================================
# Final Summary
# ============================================================================
echo "============================================================================"
echo "                         TEST SUITE SUMMARY"
echo "============================================================================"
echo ""

# Wall-clock summary (Started/Ended/Duration), captured by the helper.
SUITE_END_EPOCH=$(timing_now_epoch)
SUITE_END_HUMAN=$(timing_now_human)
timing_print_summary_block "$SUITE_START_EPOCH" "$SUITE_END_EPOCH" \
    "$SUITE_START_HUMAN" "$SUITE_END_HUMAN"
echo ""

echo "  Tests Run:    $TESTS_RUN"
echo "  Tests Passed: $TESTS_PASSED"
echo "  Tests Failed: $TESTS_FAILED"
echo ""

# Per-test timings table (in execution order).
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
