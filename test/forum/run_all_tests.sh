#!/bin/bash

# ============================================================================
# X/FORUM MODULE E2E TEST SUITE
# ============================================================================
# This script runs all forum module e2e tests in sequence.
#
# Usage:
#   ./run_all_tests.sh              # Run all tests
#   ./run_all_tests.sh --no-setup   # Skip account setup
#   ./run_all_tests.sh --no-post    # Skip post tests
#   ./run_all_tests.sh --no-sentinel # Skip sentinel tests
#   ./run_all_tests.sh --no-bounty   # Skip bounty tests
#   ./run_all_tests.sh --no-moderation # Skip moderation tests
#   ./run_all_tests.sh --no-tag-budget # Skip tag budget tests
#
# Prerequisites:
#   - sparkdreamd chain running locally
#   - Alice account with SPARK and DREAM
#   - x/rep module functional (for membership)
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../check_testparams.sh"
BINARY="sparkdreamd"

# Parse command line arguments
RUN_SETUP=true
RUN_POST=true
RUN_SENTINEL=true
RUN_BOUNTY=true
RUN_MODERATION=true
RUN_TAG_BUDGET=true
RUN_APPEALS=true
RUN_ADVANCED=true
RUN_ARCHIVE=true
RUN_OPERATIONAL_PARAMS=true
RUN_ANON=true
SAVE_SETUP=false
RESTORE_SETUP=false

for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-post)
            RUN_POST=false
            ;;
        --no-sentinel)
            RUN_SENTINEL=false
            ;;
        --no-bounty)
            RUN_BOUNTY=false
            ;;
        --no-moderation)
            RUN_MODERATION=false
            ;;
        --no-tag-budget)
            RUN_TAG_BUDGET=false
            ;;
        --no-appeals)
            RUN_APPEALS=false
            ;;
        --no-advanced)
            RUN_ADVANCED=false
            ;;
        --no-archive)
            RUN_ARCHIVE=false
            ;;
        --no-operational-params)
            RUN_OPERATIONAL_PARAMS=false
            ;;
        --no-anon)
            RUN_ANON=false
            ;;
        --only-setup)
            RUN_POST=false
            RUN_SENTINEL=false
            RUN_BOUNTY=false
            RUN_MODERATION=false
            RUN_TAG_BUDGET=false
            RUN_APPEALS=false
            RUN_ADVANCED=false
            RUN_ARCHIVE=false
            RUN_OPERATIONAL_PARAMS=false
            RUN_ANON=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_POST=false
            RUN_SENTINEL=false
            RUN_BOUNTY=false
            RUN_MODERATION=false
            RUN_TAG_BUDGET=false
            RUN_APPEALS=false
            RUN_ADVANCED=false
            RUN_ARCHIVE=false
            RUN_OPERATIONAL_PARAMS=false
            RUN_ANON=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_POST=false
            RUN_SENTINEL=false
            RUN_BOUNTY=false
            RUN_MODERATION=false
            RUN_TAG_BUDGET=false
            RUN_APPEALS=false
            RUN_ADVANCED=false
            RUN_ARCHIVE=false
            RUN_OPERATIONAL_PARAMS=false
            RUN_ANON=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup       Skip account setup"
            echo "  --no-post        Skip post tests"
            echo "  --no-sentinel    Skip sentinel tests"
            echo "  --no-bounty      Skip bounty tests"
            echo "  --no-moderation  Skip moderation tests"
            echo "  --no-tag-budget  Skip tag budget tests"
            echo "  --no-appeals     Skip appeals tests"
            echo "  --no-advanced    Skip advanced tests"
            echo "  --no-archive     Skip archive tests"
            echo "  --no-operational-params  Skip operational params tests"
            echo "  --no-anon        Skip anonymous action tests (via x/shield)"
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
echo "                    X/FORUM MODULE E2E TEST SUITE"
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
        echo "  bash test/forum/run_all_tests.sh --restore-setup"
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


# Post tests
if [ "$RUN_POST" = true ]; then
    run_test "Post Tests" "post_test.sh"
else
    echo "Skipping post tests (--no-post)"
    echo ""
fi

# Sentinel tests
if [ "$RUN_SENTINEL" = true ]; then
    run_test "Sentinel Tests" "sentinel_test.sh"
    run_test "Sentinel Limit Tests" "sentinel_limit_test.sh"
else
    echo "Skipping sentinel tests (--no-sentinel)"
    echo ""
fi

# Bounty tests
if [ "$RUN_BOUNTY" = true ]; then
    run_test "Bounty Tests" "bounty_test.sh"
else
    echo "Skipping bounty tests (--no-bounty)"
    echo ""
fi

# Moderation tests
if [ "$RUN_MODERATION" = true ]; then
    run_test "Moderation Tests" "moderation_test.sh"
else
    echo "Skipping moderation tests (--no-moderation)"
    echo ""
fi

# Tag budget tests
if [ "$RUN_TAG_BUDGET" = true ]; then
    run_test "Tag Budget Tests" "tag_budget_test.sh"
else
    echo "Skipping tag budget tests (--no-tag-budget)"
    echo ""
fi

# Appeals tests
if [ "$RUN_APPEALS" = true ]; then
    run_test "Appeals Tests" "appeals_test.sh"
else
    echo "Skipping appeals tests (--no-appeals)"
    echo ""
fi

# Advanced tests
if [ "$RUN_ADVANCED" = true ]; then
    run_test "Advanced Tests" "advanced_test.sh"
else
    echo "Skipping advanced tests (--no-advanced)"
    echo ""
fi

# Archive tests
if [ "$RUN_ARCHIVE" = true ]; then
    run_test "Archive Tests" "archive_test.sh"
else
    echo "Skipping archive tests (--no-archive)"
    echo ""
fi

# Operational params tests
if [ "$RUN_OPERATIONAL_PARAMS" = true ]; then
    run_test "Operational Params Tests" "operational_params_test.sh"
else
    echo "Skipping operational params tests (--no-operational-params)"
    echo ""
fi

# Anonymous action tests (via x/shield)
if [ "$RUN_ANON" = true ]; then
    run_test "Anonymous Action Tests" "anon_test.sh"
else
    echo "Skipping anonymous action tests (--no-anon)"
    echo ""
fi

# Pause flags tests (P1)
run_test "Pause Flags Tests" "pause_flags_test.sh"

# Content status gates tests (P2)
run_test "Content Status Gates Tests" "content_status_test.sh"

# Archive cycle limit tests (P3)
run_test "Archive Cycle Limit Tests" "archive_cycle_test.sh"

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
