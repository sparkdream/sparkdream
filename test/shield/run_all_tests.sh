#!/bin/bash

# ============================================================================
# X/SHIELD MODULE E2E TEST SUITE
# ============================================================================
# This script runs all shield module e2e tests in sequence.
#
# Usage:
#   ./run_all_tests.sh                      # Run all tests
#   ./run_all_tests.sh --no-setup           # Skip account setup
#   ./run_all_tests.sh --no-registration    # Skip registration tests
#   ./run_all_tests.sh --no-query           # Skip query tests
#   ./run_all_tests.sh --no-tle            # Skip TLE tests
#   ./run_all_tests.sh --no-funding        # Skip funding tests
#   ./run_all_tests.sh --no-exec           # Skip shielded exec tests
#   ./run_all_tests.sh --no-epoch          # Skip epoch lifecycle tests
#   ./run_all_tests.sh --no-governance     # Skip governance operation tests
#   ./run_all_tests.sh --no-errors         # Skip error paths tests
#
# Prerequisites:
#   - sparkdreamd chain running locally
#   - Alice account with SPARK and DREAM
#   - x/rep module functional (for membership)
#   - x/shield module enabled
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../check_testparams.sh"
BINARY="sparkdreamd"

# Parse command line arguments
RUN_SETUP=true
RUN_REGISTRATION=true
RUN_QUERY=true
RUN_TLE=true
RUN_FUNDING=true
RUN_EXEC=true
RUN_EPOCH=true
RUN_GOVERNANCE=true
RUN_ERRORS=true
RUN_ANON_VOTING=true
RUN_SECURITY_CONFIG=true
RUN_EXEC_MODE=true
RUN_RATE_LIMIT=true
RUN_ENCRYPTED_BATCH=true
RUN_DKG_CEREMONY=true
SAVE_SETUP=false
RESTORE_SETUP=false

for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-registration)
            RUN_REGISTRATION=false
            ;;
        --no-query)
            RUN_QUERY=false
            ;;
        --no-tle)
            RUN_TLE=false
            ;;
        --no-funding)
            RUN_FUNDING=false
            ;;
        --no-exec)
            RUN_EXEC=false
            ;;
        --no-epoch)
            RUN_EPOCH=false
            ;;
        --no-governance)
            RUN_GOVERNANCE=false
            ;;
        --no-errors)
            RUN_ERRORS=false
            ;;
        --no-anon-voting)
            RUN_ANON_VOTING=false
            ;;
        --no-security-config)
            RUN_SECURITY_CONFIG=false
            ;;
        --no-exec-mode)
            RUN_EXEC_MODE=false
            ;;
        --no-rate-limit)
            RUN_RATE_LIMIT=false
            ;;
        --no-encrypted-batch)
            RUN_ENCRYPTED_BATCH=false
            ;;
        --no-dkg-ceremony)
            RUN_DKG_CEREMONY=false
            ;;
        --only-setup)
            RUN_REGISTRATION=false
            RUN_QUERY=false
            RUN_TLE=false
            RUN_FUNDING=false
            RUN_EXEC=false
            RUN_EPOCH=false
            RUN_GOVERNANCE=false
            RUN_ERRORS=false
            RUN_ANON_VOTING=false
            RUN_SECURITY_CONFIG=false
            RUN_EXEC_MODE=false
            RUN_RATE_LIMIT=false
            RUN_ENCRYPTED_BATCH=false
            RUN_DKG_CEREMONY=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_REGISTRATION=false
            RUN_QUERY=false
            RUN_TLE=false
            RUN_FUNDING=false
            RUN_EXEC=false
            RUN_EPOCH=false
            RUN_GOVERNANCE=false
            RUN_ERRORS=false
            RUN_ANON_VOTING=false
            RUN_SECURITY_CONFIG=false
            RUN_EXEC_MODE=false
            RUN_RATE_LIMIT=false
            RUN_ENCRYPTED_BATCH=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_REGISTRATION=false
            RUN_QUERY=false
            RUN_TLE=false
            RUN_FUNDING=false
            RUN_EXEC=false
            RUN_EPOCH=false
            RUN_GOVERNANCE=false
            RUN_ERRORS=false
            RUN_ANON_VOTING=false
            RUN_SECURITY_CONFIG=false
            RUN_EXEC_MODE=false
            RUN_RATE_LIMIT=false
            RUN_ENCRYPTED_BATCH=false
            RUN_DKG_CEREMONY=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup          Skip account setup"
            echo "  --no-registration   Skip shielded operation registration tests"
            echo "  --no-query          Skip query endpoint tests"
            echo "  --no-tle            Skip TLE validator tests"
            echo "  --no-funding        Skip auto-funding tests"
            echo "  --no-exec           Skip shielded execution tests"
            echo "  --no-epoch          Skip epoch lifecycle tests"
            echo "  --no-governance     Skip governance operation tests (slow: ~5 min)"
            echo "  --no-errors         Skip error paths tests"
            echo "  --no-exec-mode      Skip execution mode enforcement tests"
            echo "  --no-rate-limit     Skip rate limit exhaustion tests"
            echo "  --no-encrypted-batch Skip encrypted batch lifecycle tests (requires TLE genesis)"
            echo "  --no-dkg-ceremony   Skip DKG ceremony lifecycle tests (slow: ~2 min, requires DKG genesis)"
            echo "  --no-anon-voting    Skip anonymous voting tests"
            echo "  --only-setup        Run only setup (skip all tests)"
            echo "  --save-setup        Run setup, save chain state, then exit"
            echo "  --restore-setup     Restore saved setup state, then run tests"
            echo "  --no-tests          Skip all tests (use with --restore-setup for manual testing)"
            echo "  --help, -h          Show this help message"
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
echo "                    X/SHIELD MODULE E2E TEST SUITE"
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

    # Check Alice balance
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

    # Run the restore script (stops chain, restores data, but doesn't restart)
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

    # Start chain directly with sparkdreamd (not ignite, to avoid interactive UI issues)
    $BINARY start --home ~/.sparkdream > /tmp/chain_after_restore.log 2>&1 &
    CHAIN_PID=$!

    echo "   Chain starting in background (PID: $CHAIN_PID)"
    echo "   Waiting for chain to be ready..."

    # Wait for chain to be accessible (max 30 seconds)
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

    # Final check
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
        echo "  bash test/shield/run_all_tests.sh --restore-setup"
        echo ""
        echo "The restore-setup option will:"
        echo "  1. Stop the chain and restore the saved state"
        echo "  2. Restart the chain automatically"
        echo "  3. Run all integration tests"
        echo "  4. Can be repeated for fast iteration"
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

# Registration tests (shielded operation registrations)
if [ "$RUN_REGISTRATION" = true ]; then
    run_test "Shielded Operation Registration Tests" "registration_test.sh"
else
    echo "Skipping registration tests (--no-registration)"
    echo ""
fi

# Query tests (all query endpoints)
if [ "$RUN_QUERY" = true ]; then
    run_test "Query Endpoint Tests" "query_test.sh"
else
    echo "Skipping query tests (--no-query)"
    echo ""
fi

# TLE tests (validator share registration, DKG)
if [ "$RUN_TLE" = true ]; then
    run_test "TLE Validator Tests" "tle_test.sh"
else
    echo "Skipping TLE tests (--no-tle)"
    echo ""
fi

# Funding tests (auto-funding, module balance)
if [ "$RUN_FUNDING" = true ]; then
    run_test "Auto-Funding Tests" "funding_test.sh"
else
    echo "Skipping funding tests (--no-funding)"
    echo ""
fi

# Shielded execution tests
if [ "$RUN_EXEC" = true ]; then
    run_test "Shielded Execution Tests" "shielded_exec_test.sh"
else
    echo "Skipping shielded execution tests (--no-exec)"
    echo ""
fi

# Epoch lifecycle tests
if [ "$RUN_EPOCH" = true ]; then
    run_test "Epoch Lifecycle Tests" "epoch_test.sh"
else
    echo "Skipping epoch lifecycle tests (--no-epoch)"
    echo ""
fi

# Error paths tests
if [ "$RUN_ERRORS" = true ]; then
    run_test "Error Paths Tests" "error_paths_test.sh"
else
    echo "Skipping error paths tests (--no-errors)"
    echo ""
fi

# Security config tests (rate limits, mode enforcement, config verification)
if [ "$RUN_SECURITY_CONFIG" = true ]; then
    run_test "Security Config Tests" "security_config_test.sh"
else
    echo "Skipping security config tests (--no-security-config)"
    echo ""
fi

# Execution mode enforcement tests
if [ "$RUN_EXEC_MODE" = true ]; then
    run_test "Execution Mode Enforcement Tests" "execution_mode_test.sh"
else
    echo "Skipping execution mode tests (--no-exec-mode)"
    echo ""
fi

# Rate limit exhaustion tests
if [ "$RUN_RATE_LIMIT" = true ]; then
    run_test "Rate Limit Tests" "rate_limit_test.sh"
else
    echo "Skipping rate limit tests (--no-rate-limit)"
    echo ""
fi

# Encrypted batch lifecycle tests (requires TLE-patched genesis)
if [ "$RUN_ENCRYPTED_BATCH" = true ]; then
    run_test "Encrypted Batch Lifecycle Tests" "encrypted_batch_test.sh"
else
    echo "Skipping encrypted batch tests (--no-encrypted-batch)"
    echo ""
fi

# DKG ceremony lifecycle tests (requires DKG-patched genesis, slow ~2 min)
if [ "$RUN_DKG_CEREMONY" = true ]; then
    run_test "DKG Ceremony Lifecycle Tests" "dkg_ceremony_test.sh"
else
    echo "Skipping DKG ceremony tests (--no-dkg-ceremony)"
    echo ""
fi

# Anonymous voting tests (shielded exec + commons anonymous vote)
if [ "$RUN_ANON_VOTING" = true ]; then
    run_test "Anonymous Voting Tests" "anonymous_voting_test.sh"
else
    echo "Skipping anonymous voting tests (--no-anon-voting)"
    echo ""
fi

# DKG ceremony error paths tests (P3)
run_test "DKG Error Paths Tests" "dkg_error_test.sh"

# Cross-module shield-aware integration tests (P3)
run_test "Cross-Module Shield-Aware Tests" "cross_module_test.sh"

# Governance operation tests (slow: requires proposal voting periods)
if [ "$RUN_GOVERNANCE" = true ]; then
    run_test "Governance Operation Tests" "governance_test.sh"
else
    echo "Skipping governance tests (--no-governance)"
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
