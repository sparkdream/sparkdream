#!/bin/bash

# ============================================================================
# X/COLLECT MODULE E2E TEST SUITE
# ============================================================================
# This script runs all collect module e2e tests in sequence.
#
# Usage:
#   ./run_all_tests.sh                # Run all tests
#   ./run_all_tests.sh --no-setup     # Skip account setup
#   ./run_all_tests.sh --no-collection # Skip collection tests
#   ./run_all_tests.sh --save-setup   # Run setup, save chain state, then exit
#   ./run_all_tests.sh --restore-setup # Restore saved state, then run tests
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
RUN_COLLECTION_TEST=true
RUN_ITEM_TEST=true
RUN_COLLABORATOR_TEST=true
RUN_VOTING_TEST=true
RUN_ENDORSEMENT_TEST=true
RUN_SPONSORSHIP_TEST=true
RUN_QUERY_TEST=true
RUN_ADV_COLLECTION_TEST=true
RUN_ADV_COLLABORATOR_TEST=true
RUN_ADV_VOTING_TEST=true
RUN_CURATION_TEST=true
RUN_IMMUTABILITY_TEST=true
RUN_SPONSORSHIP_FLOW_TEST=true
RUN_ANON=true
SAVE_SETUP=false
RESTORE_SETUP=false

for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-collection)
            RUN_COLLECTION_TEST=false
            ;;
        --no-item)
            RUN_ITEM_TEST=false
            ;;
        --no-collaborator)
            RUN_COLLABORATOR_TEST=false
            ;;
        --no-voting)
            RUN_VOTING_TEST=false
            ;;
        --no-endorsement)
            RUN_ENDORSEMENT_TEST=false
            ;;
        --no-sponsorship)
            RUN_SPONSORSHIP_TEST=false
            ;;
        --no-query)
            RUN_QUERY_TEST=false
            ;;
        --no-adv-collection)
            RUN_ADV_COLLECTION_TEST=false
            ;;
        --no-adv-collaborator)
            RUN_ADV_COLLABORATOR_TEST=false
            ;;
        --no-adv-voting)
            RUN_ADV_VOTING_TEST=false
            ;;
        --no-curation)
            RUN_CURATION_TEST=false
            ;;
        --no-immutability)
            RUN_IMMUTABILITY_TEST=false
            ;;
        --no-sponsorship-flow)
            RUN_SPONSORSHIP_FLOW_TEST=false
            ;;
        --no-anon)
            RUN_ANON=false
            ;;
        --only-setup)
            RUN_COLLECTION_TEST=false
            RUN_ITEM_TEST=false
            RUN_COLLABORATOR_TEST=false
            RUN_VOTING_TEST=false
            RUN_ENDORSEMENT_TEST=false
            RUN_SPONSORSHIP_TEST=false
            RUN_QUERY_TEST=false
            RUN_ADV_COLLECTION_TEST=false
            RUN_ADV_COLLABORATOR_TEST=false
            RUN_ADV_VOTING_TEST=false
            RUN_CURATION_TEST=false
            RUN_IMMUTABILITY_TEST=false
            RUN_SPONSORSHIP_FLOW_TEST=false
            RUN_ANON=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_COLLECTION_TEST=false
            RUN_ITEM_TEST=false
            RUN_COLLABORATOR_TEST=false
            RUN_VOTING_TEST=false
            RUN_ENDORSEMENT_TEST=false
            RUN_SPONSORSHIP_TEST=false
            RUN_QUERY_TEST=false
            RUN_ADV_COLLECTION_TEST=false
            RUN_ADV_COLLABORATOR_TEST=false
            RUN_ADV_VOTING_TEST=false
            RUN_CURATION_TEST=false
            RUN_IMMUTABILITY_TEST=false
            RUN_SPONSORSHIP_FLOW_TEST=false
            RUN_ANON=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_COLLECTION_TEST=false
            RUN_ITEM_TEST=false
            RUN_COLLABORATOR_TEST=false
            RUN_VOTING_TEST=false
            RUN_ENDORSEMENT_TEST=false
            RUN_SPONSORSHIP_TEST=false
            RUN_QUERY_TEST=false
            RUN_ADV_COLLECTION_TEST=false
            RUN_ADV_COLLABORATOR_TEST=false
            RUN_ADV_VOTING_TEST=false
            RUN_CURATION_TEST=false
            RUN_IMMUTABILITY_TEST=false
            RUN_SPONSORSHIP_FLOW_TEST=false
            RUN_ANON=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup         Skip setup_test_accounts.sh"
            echo "  --no-collection    Skip collection_test.sh"
            echo "  --no-item          Skip item_test.sh"
            echo "  --no-collaborator  Skip collaborator_test.sh"
            echo "  --no-voting        Skip voting_test.sh"
            echo "  --no-endorsement   Skip endorsement_test.sh"
            echo "  --no-sponsorship   Skip sponsorship_test.sh"
            echo "  --no-query         Skip query_test.sh"
            echo "  --no-adv-collection    Skip advanced_collection_test.sh"
            echo "  --no-adv-collaborator  Skip advanced_collaborator_test.sh"
            echo "  --no-adv-voting        Skip advanced_voting_test.sh"
            echo "  --no-curation          Skip curation_test.sh"
            echo "  --no-immutability      Skip immutability_test.sh"
            echo "  --no-sponsorship-flow  Skip sponsorship_flow_test.sh"
            echo "  --no-anon              Skip anonymous action tests (via x/shield)"
            echo "  --only-setup       Run only setup (skip all tests)"
            echo "  --save-setup       Run setup, save chain state, then exit"
            echo "  --restore-setup    Restore saved setup state, then run tests"
            echo "  --no-tests         Skip all tests (use with --restore-setup for manual testing)"
            echo "  --help, -h         Show this help message"
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

echo "========================================================================="
echo "  X/COLLECT MODULE E2E TEST SUITE"
echo "========================================================================="
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
        echo "  Start chain: ignite chain serve"
        exit 1
    fi
    echo "  Chain: OK (running)"

    # Check if Alice account exists
    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null || echo "")
    if [ -z "$ALICE_ADDR" ]; then
        echo "ERROR: Alice account not found"
        exit 1
    fi
    echo "  Alice: OK ($ALICE_ADDR)"

    # Check Alice balance
    ALICE_BALANCE=$($BINARY query bank balances $ALICE_ADDR --output json 2>/dev/null | jq -r '.balances[] | select(.denom=="uspark") | .amount' || echo "0")
    echo "  Balance: $ALICE_BALANCE uspark"
fi

echo ""
echo "Pre-flight checks passed!"
echo ""

# ============================================================================
# Restore Setup (if requested)
# ============================================================================
if [ "$RESTORE_SETUP" = true ]; then
    echo "========================================================================="
    echo "RESTORING SAVED SETUP STATE"
    echo "========================================================================="
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
        # Reset .test_env to only address variables to avoid stale collection IDs from prior runs
        cat > "$SCRIPT_DIR/.test_env" <<ENVEOF
# Auto-generated by setup_test_accounts.sh
ALICE_ADDR=$ALICE_ADDR
COLLECTOR1_ADDR=$COLLECTOR1_ADDR
COLLECTOR2_ADDR=$COLLECTOR2_ADDR
NONMEMBER1_ADDR=$NONMEMBER1_ADDR
ENVEOF
        echo "   Reset test environment (cleared stale IDs from previous runs)"
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

    echo "========================================================================="
    echo "RUNNING: $TEST_NAME"
    echo "========================================================================="
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
        echo "========================================================================="
        echo "SAVING CHAIN STATE"
        echo "========================================================================="
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
        echo "========================================================================="
        echo "SAVE-SETUP MODE COMPLETE"
        echo "========================================================================="
        echo ""
        echo "Setup completed and chain state saved to 'post-setup' snapshot"
        echo ""
        echo "Snapshot location: $SCRIPT_DIR/snapshots/post-setup"
        echo ""
        echo "To run tests from this saved state:"
        echo "  bash test/collect/run_all_tests.sh --restore-setup"
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

# Collection CRUD tests
if [ "$RUN_COLLECTION_TEST" = true ]; then
    run_test "Collection CRUD Tests" "collection_test.sh"
else
    echo "Skipping collection tests (--no-collection)"
    echo ""
fi

# Item CRUD tests
if [ "$RUN_ITEM_TEST" = true ]; then
    run_test "Item CRUD Tests" "item_test.sh"
else
    echo "Skipping item tests (--no-item)"
    echo ""
fi

# Collaborator tests
if [ "$RUN_COLLABORATOR_TEST" = true ]; then
    run_test "Collaborator Tests" "collaborator_test.sh"
else
    echo "Skipping collaborator tests (--no-collaborator)"
    echo ""
fi

# Voting & flagging tests
if [ "$RUN_VOTING_TEST" = true ]; then
    run_test "Voting & Flagging Tests" "voting_test.sh"
else
    echo "Skipping voting tests (--no-voting)"
    echo ""
fi

# Endorsement tests
if [ "$RUN_ENDORSEMENT_TEST" = true ]; then
    run_test "Endorsement Tests" "endorsement_test.sh"
else
    echo "Skipping endorsement tests (--no-endorsement)"
    echo ""
fi

# Sponsorship tests
if [ "$RUN_SPONSORSHIP_TEST" = true ]; then
    run_test "Sponsorship Tests" "sponsorship_test.sh"
else
    echo "Skipping sponsorship tests (--no-sponsorship)"
    echo ""
fi

# Query tests
if [ "$RUN_QUERY_TEST" = true ]; then
    run_test "Query Tests" "query_test.sh"
else
    echo "Skipping query tests (--no-query)"
    echo ""
fi

# Advanced collection tests
if [ "$RUN_ADV_COLLECTION_TEST" = true ]; then
    run_test "Advanced Collection Tests" "advanced_collection_test.sh"
else
    echo "Skipping advanced collection tests (--no-adv-collection)"
    echo ""
fi

# Advanced collaborator tests
if [ "$RUN_ADV_COLLABORATOR_TEST" = true ]; then
    run_test "Advanced Collaborator Tests" "advanced_collaborator_test.sh"
else
    echo "Skipping advanced collaborator tests (--no-adv-collaborator)"
    echo ""
fi

# Advanced voting tests
if [ "$RUN_ADV_VOTING_TEST" = true ]; then
    run_test "Advanced Voting Tests" "advanced_voting_test.sh"
else
    echo "Skipping advanced voting tests (--no-adv-voting)"
    echo ""
fi

# Curation tests
if [ "$RUN_CURATION_TEST" = true ]; then
    run_test "Curation Tests" "curation_test.sh"
else
    echo "Skipping curation tests (--no-curation)"
    echo ""
fi

# Immutability tests
if [ "$RUN_IMMUTABILITY_TEST" = true ]; then
    run_test "Immutability Tests" "immutability_test.sh"
else
    echo "Skipping immutability tests (--no-immutability)"
    echo ""
fi

# Sponsorship flow tests
if [ "$RUN_SPONSORSHIP_FLOW_TEST" = true ]; then
    run_test "Sponsorship Flow Tests" "sponsorship_flow_test.sh"
else
    echo "Skipping sponsorship flow tests (--no-sponsorship-flow)"
    echo ""
fi

# Anonymous action tests (via x/shield)
if [ "$RUN_ANON" = true ]; then
    run_test "Anonymous Action Tests" "anon_test.sh"
else
    echo "Skipping anonymous action tests (--no-anon)"
    echo ""
fi

# ============================================================================
# Final Summary
# ============================================================================
echo "========================================================================="
echo "                         TEST SUITE SUMMARY"
echo "========================================================================="
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
echo "========================================================================="
echo ""
