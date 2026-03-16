#!/bin/bash

# ============================================================================
# X/SESSION MODULE E2E TEST SUITE
# ============================================================================
# Usage:
#   ./run_all_tests.sh                 # Run all tests
#   ./run_all_tests.sh --no-setup      # Skip account setup
#   ./run_all_tests.sh --save-setup    # Run setup, save chain state, exit
#   ./run_all_tests.sh --restore-setup # Restore saved state, run tests
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../check_testparams.sh"
BINARY="sparkdreamd"

# Parse command line arguments
RUN_SETUP=true
RUN_CREATE=true
RUN_QUERY=true
RUN_EXEC=true
RUN_REVOKE=true
RUN_FEE=true
RUN_EXPIRATION=true
RUN_BATCH=true
RUN_OPPARAMS=true
SAVE_SETUP=false
RESTORE_SETUP=false

for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-create)
            RUN_CREATE=false
            ;;
        --no-query)
            RUN_QUERY=false
            ;;
        --no-exec)
            RUN_EXEC=false
            ;;
        --no-revoke)
            RUN_REVOKE=false
            ;;
        --no-fee)
            RUN_FEE=false
            ;;
        --no-expiration)
            RUN_EXPIRATION=false
            ;;
        --no-batch)
            RUN_BATCH=false
            ;;
        --no-opparams)
            RUN_OPPARAMS=false
            ;;
        --only-setup)
            RUN_CREATE=false
            RUN_QUERY=false
            RUN_EXEC=false
            RUN_REVOKE=false
            RUN_FEE=false
            RUN_EXPIRATION=false
            RUN_BATCH=false
            RUN_OPPARAMS=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_CREATE=false
            RUN_QUERY=false
            RUN_EXEC=false
            RUN_REVOKE=false
            RUN_FEE=false
            RUN_EXPIRATION=false
            RUN_BATCH=false
            RUN_OPPARAMS=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_CREATE=false
            RUN_QUERY=false
            RUN_EXEC=false
            RUN_REVOKE=false
            RUN_FEE=false
            RUN_EXPIRATION=false
            RUN_BATCH=false
            RUN_OPPARAMS=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup       Skip account setup"
            echo "  --no-create      Skip create session tests"
            echo "  --no-query       Skip query tests"
            echo "  --no-exec        Skip exec session tests"
            echo "  --no-revoke      Skip revoke session tests"
            echo "  --no-fee         Skip fee delegation tests"
            echo "  --no-expiration  Skip expiration/pruning tests"
            echo "  --no-batch       Skip batch execution tests"
            echo "  --no-opparams    Skip operational params tests"
            echo "  --only-setup     Run only setup (skip all tests)"
            echo "  --save-setup     Run setup, save chain state, then exit"
            echo "  --restore-setup  Restore saved setup state, then run tests"
            echo "  --no-tests       Skip all tests"
            echo "  --help, -h       Show this help message"
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

echo "============================================================================"
echo "                    X/SESSION MODULE E2E TEST SUITE"
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

# Setup
if [ "$RUN_SETUP" = true ]; then
    run_test "Account Setup" "setup_test_accounts.sh"

    if [ "$SAVE_SETUP" = true ]; then
        echo "============================================================================"
        echo "SAVING CHAIN STATE"
        echo "============================================================================"
        echo ""

        SNAPSHOT_SCRIPT="$SCRIPT_DIR/../snapshot_datadir.sh"
        if [ ! -f "$SNAPSHOT_SCRIPT" ]; then
            echo "snapshot_datadir.sh not found at $SNAPSHOT_SCRIPT"
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
        echo "To run tests from this saved state:"
        echo "  bash test/session/run_all_tests.sh --restore-setup"
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

# Create session tests
if [ "$RUN_CREATE" = true ]; then
    run_test "Create Session Tests" "create_session_test.sh"
else
    echo "Skipping create session tests (--no-create)"
    echo ""
fi

# Query tests
if [ "$RUN_QUERY" = true ]; then
    run_test "Query Tests" "query_test.sh"
else
    echo "Skipping query tests (--no-query)"
    echo ""
fi

# Exec session tests
if [ "$RUN_EXEC" = true ]; then
    run_test "Exec Session Tests" "exec_session_test.sh"
else
    echo "Skipping exec session tests (--no-exec)"
    echo ""
fi

# Revoke session tests
if [ "$RUN_REVOKE" = true ]; then
    run_test "Revoke Session Tests" "revoke_session_test.sh"
else
    echo "Skipping revoke session tests (--no-revoke)"
    echo ""
fi

# Fee delegation tests
if [ "$RUN_FEE" = true ]; then
    run_test "Fee Delegation Tests" "fee_delegation_test.sh"
else
    echo "Skipping fee delegation tests (--no-fee)"
    echo ""
fi

# Expiration & EndBlocker pruning tests
if [ "$RUN_EXPIRATION" = true ]; then
    run_test "Expiration Tests" "expiration_test.sh"
else
    echo "Skipping expiration tests (--no-expiration)"
    echo ""
fi

# Batch execution edge case tests
if [ "$RUN_BATCH" = true ]; then
    run_test "Batch Exec Tests" "batch_exec_test.sh"
else
    echo "Skipping batch execution tests (--no-batch)"
    echo ""
fi

# Operational params & governance tests
if [ "$RUN_OPPARAMS" = true ]; then
    run_test "Operational Params Tests" "operational_params_test.sh"
else
    echo "Skipping operational params tests (--no-opparams)"
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
