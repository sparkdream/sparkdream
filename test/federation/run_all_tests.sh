#!/bin/bash

# ============================================================================
# X/FEDERATION MODULE E2E TEST SUITE
# ============================================================================
# This script runs all federation module e2e tests in sequence.
#
# Usage:
#   ./run_all_tests.sh              # Run all tests
#   ./run_all_tests.sh --no-setup   # Skip account setup
#   ./run_all_tests.sh --no-params  # Skip params tests
#   ./run_all_tests.sh --no-peer    # Skip peer lifecycle tests
#   ./run_all_tests.sh --no-policy  # Skip peer policy tests
#   ./run_all_tests.sh --no-bridge  # Skip bridge operator tests
#   ./run_all_tests.sh --no-content # Skip content federation tests
#   ./run_all_tests.sh --no-identity # Skip identity link tests
#   ./run_all_tests.sh --no-verifier # Skip verifier tests
#   ./run_all_tests.sh --no-query   # Skip query tests
#
# Prerequisites:
#   - sparkdreamd chain running locally
#   - Alice account with SPARK and DREAM (genesis founder)
#   - Bob/Carol accounts (genesis council members)
#   - x/rep module functional (for membership)
#   - x/commons module functional (for council proposals)
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../check_testparams.sh"
BINARY="sparkdreamd"

# Parse command line arguments
RUN_SETUP=true
RUN_PARAMS=true
RUN_PEER=true
RUN_POLICY=true
RUN_BRIDGE=true
RUN_CONTENT=true
RUN_IDENTITY=true
RUN_VERIFIER=true
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
        --no-peer)
            RUN_PEER=false
            ;;
        --no-policy)
            RUN_POLICY=false
            ;;
        --no-bridge)
            RUN_BRIDGE=false
            ;;
        --no-content)
            RUN_CONTENT=false
            ;;
        --no-identity)
            RUN_IDENTITY=false
            ;;
        --no-verifier)
            RUN_VERIFIER=false
            ;;
        --no-query)
            RUN_QUERY=false
            ;;
        --only-setup)
            RUN_PARAMS=false
            RUN_PEER=false
            RUN_POLICY=false
            RUN_BRIDGE=false
            RUN_CONTENT=false
            RUN_IDENTITY=false
            RUN_VERIFIER=false
            RUN_QUERY=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_PARAMS=false
            RUN_PEER=false
            RUN_POLICY=false
            RUN_BRIDGE=false
            RUN_CONTENT=false
            RUN_IDENTITY=false
            RUN_VERIFIER=false
            RUN_QUERY=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_PARAMS=false
            RUN_PEER=false
            RUN_POLICY=false
            RUN_BRIDGE=false
            RUN_CONTENT=false
            RUN_IDENTITY=false
            RUN_VERIFIER=false
            RUN_QUERY=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup       Skip account setup"
            echo "  --no-params      Skip params tests"
            echo "  --no-peer        Skip peer lifecycle tests"
            echo "  --no-policy      Skip peer policy tests"
            echo "  --no-bridge      Skip bridge operator tests"
            echo "  --no-content     Skip content federation tests"
            echo "  --no-identity    Skip identity link tests"
            echo "  --no-verifier    Skip verifier tests"
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
echo "                    X/FEDERATION MODULE E2E TEST SUITE"
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
    bash "$RESTORE_SCRIPT"

    if [ $? -ne 0 ]; then
        echo "Failed to restore setup state"
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

    # NOTE: Policy addresses are populated after the chain starts (needs live queries)

    echo ""
    echo "Starting chain..."

    $BINARY start --home ~/.sparkdream > /tmp/chain_after_restore.log 2>&1 &
    CHAIN_PID=$!

    echo "   Chain starting in background (PID: $CHAIN_PID)"
    echo "   Waiting for chain to be ready..."

    MAX_ATTEMPTS=60
    ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        if $BINARY status &> /dev/null; then
            BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
            if [ "$BLOCK_HEIGHT" != "0" ] && [ "$BLOCK_HEIGHT" != "null" ] && [ -n "$BLOCK_HEIGHT" ]; then
                echo "   Chain is running (block height: $BLOCK_HEIGHT)"
                break
            fi
            # Chain process is up but hasn't produced a block yet
            if [ $((ATTEMPT % 5)) -eq 0 ]; then
                echo "   Waiting for first block (height: $BLOCK_HEIGHT)..."
            fi
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    # Final check: chain must be running AND have produced at least one block
    BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height' 2>/dev/null || echo "0")
    if [ "$BLOCK_HEIGHT" = "0" ] || [ "$BLOCK_HEIGHT" = "null" ] || [ -z "$BLOCK_HEIGHT" ]; then
        echo "Chain failed to produce first block after 60 seconds"
        echo "   Check logs: tail -f /tmp/chain_after_restore.log"
        exit 1
    fi

    # Populate missing policy addresses from live chain queries
    if [ -z "$COMMONS_POLICY" ] || [ "$COMMONS_POLICY" == "null" ]; then
        echo "   Looking up Commons Council policy address..."
        COMMONS_INFO=$($BINARY query commons get-group "Commons Council" --output json 2>&1)
        COMMONS_POLICY=$(echo "$COMMONS_INFO" | jq -r '.group.policy_address // empty')
        if [ -n "$COMMONS_POLICY" ]; then
            echo "   Commons Council Policy: $COMMONS_POLICY"
            export COMMONS_POLICY
        else
            echo "   WARNING: Could not look up Commons Council policy"
        fi
    fi

    if [ -z "$OPS_POLICY" ] || [ "$OPS_POLICY" == "null" ]; then
        echo "   Looking up Operations Committee policy address..."
        OPS_INFO=$($BINARY query commons get-group "Commons Operations Committee" --output json 2>&1)
        OPS_POLICY=$(echo "$OPS_INFO" | jq -r '.group.policy_address // empty')
        if [ -n "$OPS_POLICY" ]; then
            echo "   Operations Committee Policy: $OPS_POLICY"
            export OPS_POLICY
        else
            echo "   WARNING: Could not look up Operations Committee policy"
        fi
    fi

    # Update .test_env with resolved policy addresses
    if [ -f "$SCRIPT_DIR/.test_env" ]; then
        sed -i "s|^export COMMONS_POLICY=.*|export COMMONS_POLICY=$COMMONS_POLICY|" "$SCRIPT_DIR/.test_env"
        sed -i "s|^export OPS_POLICY=.*|export OPS_POLICY=$OPS_POLICY|" "$SCRIPT_DIR/.test_env"
        echo "   Updated .test_env with policy addresses"
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
            exit 1
        fi

        echo "Saving chain state to 'post-setup' snapshot..."
        bash "$SNAPSHOT_SCRIPT" post-setup "$SCRIPT_DIR/snapshots"

        if [ $? -ne 0 ]; then
            echo "Failed to save chain state"
            exit 1
        fi

        echo ""
        echo "============================================================================"
        echo "SAVE-SETUP MODE COMPLETE"
        echo "============================================================================"
        echo ""
        echo "Setup completed and chain state saved to 'post-setup' snapshot"
        echo "Snapshot location: $SCRIPT_DIR/snapshots/post-setup"
        echo ""
        echo "To run tests from this saved state:"
        echo "  bash test/federation/run_all_tests.sh --restore-setup"
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

# Params tests (basic smoke test)
if [ "$RUN_PARAMS" = true ]; then
    run_test "Params Tests" "params_test.sh"
else
    echo "Skipping params tests (--no-params)"
    echo ""
fi

# Peer lifecycle tests (must run before bridge/content tests)
if [ "$RUN_PEER" = true ]; then
    run_test "Peer Lifecycle Tests" "peer_lifecycle_test.sh"
else
    echo "Skipping peer lifecycle tests (--no-peer)"
    echo ""
fi

# Peer policy tests
if [ "$RUN_POLICY" = true ]; then
    run_test "Peer Policy Tests" "peer_policy_test.sh"
else
    echo "Skipping peer policy tests (--no-policy)"
    echo ""
fi

# Bridge operator tests (requires active peer from peer_lifecycle)
if [ "$RUN_BRIDGE" = true ]; then
    run_test "Bridge Operator Tests" "bridge_operator_test.sh"
else
    echo "Skipping bridge operator tests (--no-bridge)"
    echo ""
fi

# Content federation tests (requires bridge from bridge_operator)
if [ "$RUN_CONTENT" = true ]; then
    run_test "Content Federation Tests" "content_federation_test.sh"
else
    echo "Skipping content federation tests (--no-content)"
    echo ""
fi

# Identity link tests (requires active peer)
if [ "$RUN_IDENTITY" = true ]; then
    run_test "Identity Link Tests" "identity_link_test.sh"
else
    echo "Skipping identity link tests (--no-identity)"
    echo ""
fi

# Verifier tests (requires content from content_federation)
if [ "$RUN_VERIFIER" = true ]; then
    run_test "Verifier Tests" "verifier_test.sh"
else
    echo "Skipping verifier tests (--no-verifier)"
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
