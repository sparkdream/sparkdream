#!/bin/bash

# ============================================================================
# X/REVEAL MODULE E2E TEST SUITE
# ============================================================================
# This script runs all reveal module e2e tests in sequence.
#
# Usage:
#   ./run_all_tests.sh              # Run all tests
#   ./run_all_tests.sh --no-setup   # Skip account setup
#   ./run_all_tests.sh --no-params  # Skip params tests
#   ./run_all_tests.sh --no-propose # Skip propose tests
#   ./run_all_tests.sh --no-lifecycle # Skip lifecycle tests
#   ./run_all_tests.sh --no-cancel  # Skip cancel/reject tests
#   ./run_all_tests.sh --no-stake   # Skip stake/withdraw tests
#   ./run_all_tests.sh --no-query   # Skip query tests
#
# Prerequisites:
#   - sparkdreamd chain running locally
#   - Alice account with SPARK and DREAM (genesis founder)
#   - Bob account (genesis council member)
#   - x/rep module functional (for membership)
#   - x/commons module functional (for council proposals)
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"

# Parse command line arguments
RUN_SETUP=true
RUN_PARAMS=true
RUN_PROPOSE=true
RUN_LIFECYCLE=true
RUN_CANCEL=true
RUN_STAKE=true
RUN_QUERY=true
SAVE_SETUP=false
RESTORE_SETUP=false

for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-params)
            RUN_PARAMS=false
            ;;
        --no-propose)
            RUN_PROPOSE=false
            ;;
        --no-lifecycle)
            RUN_LIFECYCLE=false
            ;;
        --no-cancel)
            RUN_CANCEL=false
            ;;
        --no-stake)
            RUN_STAKE=false
            ;;
        --no-query)
            RUN_QUERY=false
            ;;
        --only-setup)
            RUN_PARAMS=false
            RUN_PROPOSE=false
            RUN_LIFECYCLE=false
            RUN_CANCEL=false
            RUN_STAKE=false
            RUN_QUERY=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_PARAMS=false
            RUN_PROPOSE=false
            RUN_LIFECYCLE=false
            RUN_CANCEL=false
            RUN_STAKE=false
            RUN_QUERY=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_PARAMS=false
            RUN_PROPOSE=false
            RUN_LIFECYCLE=false
            RUN_CANCEL=false
            RUN_STAKE=false
            RUN_QUERY=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup       Skip account setup"
            echo "  --no-params      Skip params tests"
            echo "  --no-propose     Skip propose tests"
            echo "  --no-lifecycle   Skip lifecycle tests"
            echo "  --no-cancel      Skip cancel/reject tests"
            echo "  --no-stake       Skip stake/withdraw tests"
            echo "  --no-query       Skip query tests"
            echo "  --only-setup     Run only setup (skip all tests)"
            echo "  --save-setup     Run setup, save chain state, then exit"
            echo "  --restore-setup  Restore saved setup state, then run tests"
            echo "  --no-tests       Skip all tests (use with --restore-setup for manual testing)"
            echo "  --help, -h       Show this help message"
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

echo "============================================================================"
echo "                    X/REVEAL MODULE E2E TEST SUITE"
echo "============================================================================"
echo ""

# ============================================================================
# Pre-flight checks
# ============================================================================
echo "--- PRE-FLIGHT CHECKS ---"
echo ""

# Check if binary exists
if ! command -v $BINARY &> /dev/null; then
    echo "ERROR: $BINARY not found in PATH"
    exit 1
fi
echo "  Binary: OK ($BINARY)"

# Skip chain running check for restore-setup (it will start the chain)
if [ "$RESTORE_SETUP" = true ]; then
    echo "  Restore mode: Chain will be stopped and restarted during restore"
else
    # Check if chain is running
    if ! $BINARY status &> /dev/null; then
        echo "ERROR: Chain is not running"
        echo "  Start chain: $BINARY start"
        exit 1
    fi
    echo "  Chain: OK (running)"

    # Check if Alice account exists
    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null || echo "")
    if [ -z "$ALICE_ADDR" ]; then
        echo "ERROR: Alice account not found"
        echo "  Create: $BINARY keys add alice --keyring-backend test"
        exit 1
    fi
    echo "  Alice: OK ($ALICE_ADDR)"

    # Check if Bob account exists
    BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test 2>/dev/null || echo "")
    if [ -z "$BOB_ADDR" ]; then
        echo "WARNING: Bob account not found (needed for council votes)"
    else
        echo "  Bob: OK ($BOB_ADDR)"
    fi

    # Check Alice balance
    ALICE_BALANCE=$($BINARY query bank balances $ALICE_ADDR --output json 2>/dev/null | jq -r '.balances[] | select(.denom=="uspark") | .amount' || echo "0")
    if [ "$ALICE_BALANCE" -lt 1000000 ] 2>/dev/null; then
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

    # Load .test_env from restored state
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
fi

# ============================================================================
# Test Results Tracking
# ============================================================================
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
declare -a FAILED_TESTS

run_test() {
    local TEST_NAME=$1
    local TEST_SCRIPT=$2

    echo "============================================================================"
    echo "RUNNING: $TEST_NAME"
    echo "============================================================================"
    echo ""

    TESTS_RUN=$((TESTS_RUN + 1))

    if bash "$SCRIPT_DIR/$TEST_SCRIPT"; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        echo ""
        echo ">>> $TEST_NAME: PASSED <<<"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        FAILED_TESTS+=("$TEST_NAME")
        echo ""
        echo ">>> $TEST_NAME: FAILED <<<"
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

    # If --save-setup mode, save chain state and exit
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
        echo "  bash test/reveal/run_all_tests.sh --restore-setup"
        echo ""
        exit 0
    fi
else
    echo "Skipping account setup (--no-setup)"
    echo ""

    # Verify .test_env exists if we're not restoring
    if [ "$RESTORE_SETUP" != true ] && [ ! -f "$SCRIPT_DIR/.test_env" ]; then
        echo "Warning: Test environment not found (.test_env missing)"
        echo "   Run without --no-setup flag to create it"
    fi
fi

# Params tests (basic smoke test, run first)
if [ "$RUN_PARAMS" = true ]; then
    run_test "Params Tests" "params_test.sh"
else
    echo "Skipping params tests (--no-params)"
    echo ""
fi

# Propose tests
if [ "$RUN_PROPOSE" = true ]; then
    run_test "Propose Tests" "propose_test.sh"
else
    echo "Skipping propose tests (--no-propose)"
    echo ""
fi

# Lifecycle tests (most comprehensive: propose -> approve -> stake -> reveal -> verify)
if [ "$RUN_LIFECYCLE" = true ]; then
    run_test "Lifecycle Tests" "lifecycle_test.sh"
else
    echo "Skipping lifecycle tests (--no-lifecycle)"
    echo ""
fi

# Cancel/reject tests
if [ "$RUN_CANCEL" = true ]; then
    run_test "Cancel/Reject Tests" "cancel_reject_test.sh"
else
    echo "Skipping cancel/reject tests (--no-cancel)"
    echo ""
fi

# Stake/withdraw tests
if [ "$RUN_STAKE" = true ]; then
    run_test "Stake/Withdraw Tests" "stake_withdraw_test.sh"
else
    echo "Skipping stake/withdraw tests (--no-stake)"
    echo ""
fi

# Query tests (runs after other tests have created data)
if [ "$RUN_QUERY" = true ]; then
    run_test "Query Tests" "query_test.sh"
else
    echo "Skipping query tests (--no-query)"
    echo ""
fi

# ============================================================================
# Final Summary
# ============================================================================
echo "============================================================================"
echo "                         TEST SUITE SUMMARY"
echo "============================================================================"
echo ""
echo "  Tests Run:    $TESTS_RUN"
echo "  Tests Passed: $TESTS_PASSED"
echo "  Tests Failed: $TESTS_FAILED"
echo ""

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
