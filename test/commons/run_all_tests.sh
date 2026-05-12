#!/bin/bash

# ============================================================================
# X/COMMONS MODULE E2E TEST SUITE
# ============================================================================
# This script runs all commons module e2e tests in sequence.
#
# Tests are grouped into 5 phases:
#   1. LIFECYCLE     — interim council, group lifecycle, treasury, fees
#   2. CATEGORY      — content category CRUD and access control
#   3. SECURITY      — group/policy security, unauthorized actions, anon
#   4. VETOS         — executive / social / parent veto flows
#   5. DESTRUCTIVE   — tech upgrade & fire council (skipped by default)
#
# IMPORTANT: The DESTRUCTIVE phase rewrites council membership. It is
# off by default so that re-running the suite during iteration leaves the
# chain in a working state. Pass --with-destructive (or --only-destructive)
# to run them; the master test/run_all_tests.sh runs them at the very end
# of the full suite.
#
# Usage:
#   ./run_all_tests.sh                    # All phases except destructive
#   ./run_all_tests.sh --no-setup         # Skip account setup
#   ./run_all_tests.sh --no-lifecycle     # Skip lifecycle phase
#   ./run_all_tests.sh --no-category      # Skip category phase
#   ./run_all_tests.sh --no-security      # Skip security phase
#   ./run_all_tests.sh --no-vetos         # Skip vetos phase
#   ./run_all_tests.sh --with-destructive # Include destructive phase
#   ./run_all_tests.sh --only-destructive # Run only the destructive phase
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
RUN_LIFECYCLE=true
RUN_CATEGORY=true
RUN_SECURITY=true
RUN_VETOS=true
# Destructive phase OFF by default — see header comment.
RUN_DESTRUCTIVE=false
SAVE_SETUP=false
RESTORE_SETUP=false
AUTO_SNAPSHOT=true

for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-lifecycle)
            RUN_LIFECYCLE=false
            ;;
        --no-category)
            RUN_CATEGORY=false
            ;;
        --no-security)
            RUN_SECURITY=false
            ;;
        --no-vetos)
            RUN_VETOS=false
            ;;
        --with-destructive)
            RUN_DESTRUCTIVE=true
            ;;
        --only-destructive)
            # Run just the destructive phase. Setup is still required so the
            # auto-snapshot machinery has something to reuse, but we skip
            # every non-destructive phase.
            RUN_LIFECYCLE=false
            RUN_CATEGORY=false
            RUN_SECURITY=false
            RUN_VETOS=false
            RUN_DESTRUCTIVE=true
            ;;
        --only-setup)
            RUN_LIFECYCLE=false
            RUN_CATEGORY=false
            RUN_SECURITY=false
            RUN_VETOS=false
            RUN_DESTRUCTIVE=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_LIFECYCLE=false
            RUN_CATEGORY=false
            RUN_SECURITY=false
            RUN_VETOS=false
            RUN_DESTRUCTIVE=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_LIFECYCLE=false
            RUN_CATEGORY=false
            RUN_SECURITY=false
            RUN_VETOS=false
            RUN_DESTRUCTIVE=false
            ;;
        --no-auto-snapshot)
            AUTO_SNAPSHOT=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Phase flags (default: all non-destructive phases run):"
            echo "  --no-setup            Skip setup_test_accounts.sh"
            echo "  --no-lifecycle        Skip lifecycle phase"
            echo "  --no-category         Skip category phase"
            echo "  --no-security         Skip security phase"
            echo "  --no-vetos            Skip vetos phase"
            echo "  --with-destructive    Include the destructive phase"
            echo "                        (tech upgrade + fire council)"
            echo "  --only-destructive    Run only the destructive phase"
            echo "  --only-setup          Run only setup (skip all test phases)"
            echo "  --no-tests            Skip every test phase (use with --restore-setup)"
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
            echo ""
            echo "The destructive phase is OFF by default because it permanently"
            echo "rewrites council membership. The master test/run_all_tests.sh"
            echo "runs it at the very end of the full suite."
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
echo "                   X/COMMONS MODULE E2E TEST SUITE"
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
        echo "  bash test/commons/run_all_tests.sh --restore-setup"
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
# Phase 1: Lifecycle
# ============================================================================
if [ "$RUN_LIFECYCLE" = true ]; then
    echo ">>> PHASE 1: LIFECYCLE <<<"
    run_test "Interim Council"            "interim_council_test.sh"
    run_test "Group Lifecycle"            "group_lifecycle_test.sh"
    run_test "Group Member Update"        "group_member_update_test.sh"
    run_test "Treasury Spend"             "treasury_spend.sh"
    run_test "Fee Update"                 "fee_update_test.sh"
else
    echo "Skipping lifecycle phase (--no-lifecycle)"
    echo ""
fi

# ============================================================================
# Phase 2: Category
# ============================================================================
if [ "$RUN_CATEGORY" = true ]; then
    echo ">>> PHASE 2: CATEGORY <<<"
    run_test "Category"                   "category_test.sh"
    run_test "Category Delete"            "category_delete_test.sh"
else
    echo "Skipping category phase (--no-category)"
    echo ""
fi

# ============================================================================
# Phase 3: Security
# ============================================================================
if [ "$RUN_SECURITY" = true ]; then
    echo ">>> PHASE 3: SECURITY <<<"
    run_test "Group Security"             "group_security_test.sh"
    run_test "Policy Lifecycle Security"  "policy_lifecycle_security_test.sh"
    run_test "Policy Permissions"         "policy_permissions_test.sh"
    run_test "Unauthorized Spend Msg"     "unauthorized_spend_msg.sh"
    run_test "Unauthorized Handover"      "unauthorized_handover.sh"
    run_test "Anonymous Action"           "anon_test.sh"
else
    echo "Skipping security phase (--no-security)"
    echo ""
fi

# ============================================================================
# Phase 4: Vetos
# ============================================================================
if [ "$RUN_VETOS" = true ]; then
    echo ">>> PHASE 4: VETOS <<<"
    run_test "Executive Veto"             "executive_veto_test.sh"
    run_test "Social Veto Vote"           "social_veto_vote_test.sh"
    run_test "Parent Veto"                "parent_veto_test.sh"
    run_test "Veto Vote"                  "veto_vote_test.sh"
else
    echo "Skipping vetos phase (--no-vetos)"
    echo ""
fi

# ============================================================================
# Phase 5: Destructive (off by default)
# ============================================================================
if [ "$RUN_DESTRUCTIVE" = true ]; then
    echo ">>> PHASE 5: DESTRUCTIVE <<<"
    echo "  WARNING: These tests rewrite council membership."
    echo ""
    run_test "Tech Upgrade Golden Share"  "tech_upgrade_golden_share.sh"
    run_test "Fire Council"               "fire_council_test.sh"
else
    echo "Skipping destructive phase (default: OFF — pass --with-destructive to enable)"
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
