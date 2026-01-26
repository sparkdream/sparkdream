#!/bin/bash

echo "========================================================================="
echo "  X/FUTARCHY INTEGRATION TESTS - MASTER TEST RUNNER"
echo "========================================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Test execution flags
RUN_SETUP=true
RUN_MARKET_LIFECYCLE_TEST=true
RUN_GOVERNANCE_INTEGRATION_TEST=true
RUN_PARAMS_UPDATE_TEST=true
RUN_LIQUIDITY_WITHDRAWAL_TEST=true
RUN_EMERGENCY_CANCEL_TEST=true
RESET_CHAIN=false
SAVE_SETUP=false
RESTORE_SETUP=false

# ========================================================================
# Parse Arguments
# ========================================================================
while [[ $# -gt 0 ]]; do
    case $1 in
        --no-setup)
            RUN_SETUP=false
            shift
            ;;
        --no-market-lifecycle)
            RUN_MARKET_LIFECYCLE_TEST=false
            shift
            ;;
        --no-governance-integration)
            RUN_GOVERNANCE_INTEGRATION_TEST=false
            shift
            ;;
        --no-params-update)
            RUN_PARAMS_UPDATE_TEST=false
            shift
            ;;
        --no-liquidity-withdrawal)
            RUN_LIQUIDITY_WITHDRAWAL_TEST=false
            shift
            ;;
        --no-emergency-cancel)
            RUN_EMERGENCY_CANCEL_TEST=false
            shift
            ;;
        --reset-chain)
            RESET_CHAIN=true
            shift
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_MARKET_LIFECYCLE_TEST=false
            RUN_GOVERNANCE_INTEGRATION_TEST=false
            RUN_PARAMS_UPDATE_TEST=false
            RUN_LIQUIDITY_WITHDRAWAL_TEST=false
            RUN_EMERGENCY_CANCEL_TEST=false
            shift
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            shift
            ;;
        --no-tests)
            RUN_MARKET_LIFECYCLE_TEST=false
            RUN_GOVERNANCE_INTEGRATION_TEST=false
            RUN_PARAMS_UPDATE_TEST=false
            RUN_LIQUIDITY_WITHDRAWAL_TEST=false
            RUN_EMERGENCY_CANCEL_TEST=false
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup                    Skip chain setup"
            echo "  --no-market-lifecycle         Skip market_lifecycle_test.sh"
            echo "  --no-governance-integration   Skip governance_integration_test.sh"
            echo "  --no-params-update            Skip params_update_test.sh"
            echo "  --no-liquidity-withdrawal     Skip liquidity_withdrawal_test.sh"
            echo "  --no-emergency-cancel         Skip emergency_cancel_test.sh"
            echo "  --no-tests                    Skip all tests (use with --restore-setup)"
            echo "  --reset-chain                 Reset chain before running tests"
            echo "  --save-setup                  Run setup, save chain state, then exit"
            echo "  --restore-setup               Restore saved setup state, then run tests"
            echo "  --help                        Show this help message"
            echo ""
            echo "Default: Run full test suite"
            echo ""
            echo "Workflow for fast iteration:"
            echo "  1. bash $0 --save-setup      # One-time: run setup and save state"
            echo "  2. bash $0 --restore-setup   # Restore and run tests (repeatable)"
            echo ""
            echo "Workflow for manual testing:"
            echo "  1. bash $0 --restore-setup --no-tests  # Restore state, start chain, exit"
            echo "  2. bash ./market_lifecycle_test.sh     # Run specific test manually"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# ========================================================================
# Pre-flight Checks
# ========================================================================
echo "=== PRE-FLIGHT CHECKS ==="

# Skip chain running check for restore-setup (it will start the chain)
if [ "$RESTORE_SETUP" = true ]; then
    echo "  Restore mode: Chain will be stopped and restarted during restore"
else
    # Check if chain is running
    if ! $BINARY status &> /dev/null; then
        echo "  Chain is not running!"
        echo ""
        echo "Please start the chain first:"
        echo "  cd /home/chill/cosmos/sparkdream/sparkdream"
        echo "  ignite chain serve"
        echo ""
        exit 1
    fi

    BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
    echo "  Chain is running (block height: $BLOCK_HEIGHT)"
fi

# Skip account checks for restore-setup (chain not running yet)
if [ "$RESTORE_SETUP" != true ]; then
    # Check if Alice exists
    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null)
    if [ -z "$ALICE_ADDR" ]; then
        echo "  Alice account not found in keyring"
        echo "   Make sure the chain is initialized with genesis accounts"
        exit 1
    fi
    echo "  Alice account found: $ALICE_ADDR"

    # Check if Bob exists
    BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test 2>/dev/null)
    if [ -z "$BOB_ADDR" ]; then
        echo "  Bob account not found in keyring"
        exit 1
    fi
    echo "  Bob account found: $BOB_ADDR"

    # Check if Carol exists
    CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test 2>/dev/null)
    if [ -z "$CAROL_ADDR" ]; then
        echo "  Carol account not found in keyring"
        exit 1
    fi
    echo "  Carol account found: $CAROL_ADDR"

    echo ""
fi

# ========================================================================
# Chain Reset (if requested)
# ========================================================================
if [ "$RESET_CHAIN" = true ]; then
    echo "=== CHAIN RESET REQUESTED ==="
    echo ""
    echo "  To reset the chain:"
    echo "   1. Stop the running chain (Ctrl+C in ignite terminal)"
    echo "   2. Run: cd /home/chill/cosmos/sparkdream/sparkdream && ignite chain serve --reset-once"
    echo "   3. Wait for chain to start"
    echo "   4. Re-run this script"
    echo ""
    read -p "Have you completed the reset? (yes/no): " RESET_DONE
    if [ "$RESET_DONE" != "yes" ]; then
        echo "Exiting. Please reset chain and try again."
        exit 0
    fi
    echo ""
fi

# ========================================================================
# Restore Setup (if requested)
# ========================================================================
if [ "$RESTORE_SETUP" = true ]; then
    echo "========================================================================="
    echo "RESTORING SAVED SETUP STATE"
    echo "========================================================================="
    echo ""

    SNAPSHOT_PATH="$SCRIPT_DIR/snapshots/post-setup"
    RESTORE_SCRIPT="$SNAPSHOT_PATH/restore.sh"

    if [ ! -f "$RESTORE_SCRIPT" ]; then
        echo "  Snapshot 'post-setup' not found at: $SNAPSHOT_PATH"
        echo "   Run with --save-setup first to create the snapshot"
        echo ""
        echo "   Alternatively, ensure the chain is running and re-run without --restore-setup"
        exit 1
    fi

    echo "Restoring chain state from 'post-setup' snapshot..."
    echo "Snapshot location: $SNAPSHOT_PATH"
    echo ""

    # Run the restore script (stops chain, restores data, but doesn't restart)
    bash "$RESTORE_SCRIPT"
    RESTORE_EXIT_CODE=$?

    if [ $RESTORE_EXIT_CODE -ne 0 ]; then
        echo "  Failed to restore setup state (exit code: $RESTORE_EXIT_CODE)"
        exit 1
    fi

    echo ""
    echo "  Setup state restored successfully"
    echo ""

    echo "  Starting chain..."

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
            echo "  Chain is running (block height: $BLOCK_HEIGHT)"
            break
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    # Final check
    if ! $BINARY status &> /dev/null; then
        echo "  Chain failed to start after 30 seconds"
        echo "   Check logs: tail -f /tmp/chain_after_restore.log"
        exit 1
    fi

    echo ""
fi

# ========================================================================
# Test Execution Plan
# ========================================================================
echo "=== TEST EXECUTION PLAN ==="
if [ "$SAVE_SETUP" = true ]; then
    echo ""
    echo "  SAVE-SETUP MODE"
    echo "    Running setup, saving chain state, then exiting"
    echo ""
elif [ "$RESTORE_SETUP" = true ]; then
    echo ""
    echo "  RESTORE-SETUP MODE"
    echo "    Restored saved setup state, now running tests"
    echo ""
fi
echo "  1. Market lifecycle test:         $([ "$RUN_MARKET_LIFECYCLE_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  2. Governance integration test:   $([ "$RUN_GOVERNANCE_INTEGRATION_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  3. Params update test:            $([ "$RUN_PARAMS_UPDATE_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  4. Liquidity withdrawal test:     $([ "$RUN_LIQUIDITY_WITHDRAWAL_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  5. Emergency cancel test:         $([ "$RUN_EMERGENCY_CANCEL_TEST" = true ] && echo "YES" || echo "SKIP")"
echo ""

if [ "$SAVE_SETUP" != true ] && [ "$RESTORE_SETUP" != true ]; then
    read -p "Proceed with test execution? (yes/no): " PROCEED
    if [ "$PROCEED" != "yes" ]; then
        echo "Aborted."
        exit 0
    fi
    echo ""
fi

# ========================================================================
# Save Setup Mode
# ========================================================================
if [ "$SAVE_SETUP" = true ]; then
    echo "========================================================================="
    echo "SAVING CHAIN STATE"
    echo "========================================================================="
    echo ""

    # Create snapshot directory
    SNAPSHOT_DIR="$SCRIPT_DIR/snapshots/post-setup"
    mkdir -p "$SNAPSHOT_DIR"

    # For futarchy, we don't need special setup - just save the current chain state
    # The chain should already be running with genesis accounts

    SNAPSHOT_SCRIPT="$SCRIPT_DIR/../snapshot_datadir.sh"
    if [ -f "$SNAPSHOT_SCRIPT" ]; then
        echo "Using common snapshot_datadir.sh..."
        bash "$SNAPSHOT_SCRIPT" post-setup "$SCRIPT_DIR/snapshots"
        SAVE_EXIT_CODE=$?
    else
        echo "  ERROR: snapshot_datadir.sh not found at $SNAPSHOT_SCRIPT"
        exit 1
    fi

    if [ $SAVE_EXIT_CODE -ne 0 ]; then
        echo "  Failed to save chain state (exit code: $SAVE_EXIT_CODE)"
        exit 1
    fi

    echo ""
    echo "========================================================================="
    echo "SAVE-SETUP MODE COMPLETE"
    echo "========================================================================="
    echo ""
    echo "  Setup completed and chain state saved to 'post-setup' snapshot"
    echo ""
    echo "Snapshot location: $SNAPSHOT_DIR"
    echo ""
    echo "To run tests from this saved state:"
    echo "  bash test/futarchy/run_all_tests.sh --restore-setup"
    echo ""
    exit 0
fi

# ========================================================================
# Step 1: Market Lifecycle Test
# ========================================================================
if [ "$RUN_MARKET_LIFECYCLE_TEST" = true ]; then
    echo "========================================================================="
    echo "TEST 1: MARKET LIFECYCLE TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/market_lifecycle_test.sh"
    MARKET_LIFECYCLE_EXIT_CODE=$?

    echo ""
    if [ $MARKET_LIFECYCLE_EXIT_CODE -eq 0 ]; then
        echo "  Market lifecycle test completed"
    else
        echo "  Market lifecycle test exited with code: $MARKET_LIFECYCLE_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "TEST 1: MARKET LIFECYCLE TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 2: Governance Integration Test
# ========================================================================
if [ "$RUN_GOVERNANCE_INTEGRATION_TEST" = true ]; then
    echo "========================================================================="
    echo "TEST 2: GOVERNANCE INTEGRATION TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/governance_integration_test.sh"
    GOVERNANCE_INTEGRATION_EXIT_CODE=$?

    echo ""
    if [ $GOVERNANCE_INTEGRATION_EXIT_CODE -eq 0 ]; then
        echo "  Governance integration test completed"
    else
        echo "  Governance integration test exited with code: $GOVERNANCE_INTEGRATION_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "TEST 2: GOVERNANCE INTEGRATION TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 3: Params Update Test
# ========================================================================
if [ "$RUN_PARAMS_UPDATE_TEST" = true ]; then
    echo "========================================================================="
    echo "TEST 3: PARAMS UPDATE TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/params_update_test.sh"
    PARAMS_UPDATE_EXIT_CODE=$?

    echo ""
    if [ $PARAMS_UPDATE_EXIT_CODE -eq 0 ]; then
        echo "  Params update test completed"
    else
        echo "  Params update test exited with code: $PARAMS_UPDATE_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "TEST 3: PARAMS UPDATE TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 4: Liquidity Withdrawal Test
# ========================================================================
if [ "$RUN_LIQUIDITY_WITHDRAWAL_TEST" = true ]; then
    echo "========================================================================="
    echo "TEST 4: LIQUIDITY WITHDRAWAL TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/liquidity_withdrawal_test.sh"
    LIQUIDITY_WITHDRAWAL_EXIT_CODE=$?

    echo ""
    if [ $LIQUIDITY_WITHDRAWAL_EXIT_CODE -eq 0 ]; then
        echo "  Liquidity withdrawal test completed"
    else
        echo "  Liquidity withdrawal test exited with code: $LIQUIDITY_WITHDRAWAL_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "TEST 4: LIQUIDITY WITHDRAWAL TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 5: Emergency Cancel Test
# ========================================================================
if [ "$RUN_EMERGENCY_CANCEL_TEST" = true ]; then
    echo "========================================================================="
    echo "TEST 5: EMERGENCY CANCEL TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/emergency_cancel_test.sh"
    EMERGENCY_CANCEL_EXIT_CODE=$?

    echo ""
    if [ $EMERGENCY_CANCEL_EXIT_CODE -eq 0 ]; then
        echo "  Emergency cancel test completed"
    else
        echo "  Emergency cancel test exited with code: $EMERGENCY_CANCEL_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "TEST 5: EMERGENCY CANCEL TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Summary
# ========================================================================
echo "========================================================================="
echo "  TEST SUITE SUMMARY"
echo "========================================================================="
echo ""
echo "Results:"
echo "  Market Lifecycle:        $([ "$RUN_MARKET_LIFECYCLE_TEST" = true ] && ([ ${MARKET_LIFECYCLE_EXIT_CODE:-1} -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Governance Integration:  $([ "$RUN_GOVERNANCE_INTEGRATION_TEST" = true ] && ([ ${GOVERNANCE_INTEGRATION_EXIT_CODE:-1} -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Params Update:           $([ "$RUN_PARAMS_UPDATE_TEST" = true ] && ([ ${PARAMS_UPDATE_EXIT_CODE:-1} -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Liquidity Withdrawal:    $([ "$RUN_LIQUIDITY_WITHDRAWAL_TEST" = true ] && ([ ${LIQUIDITY_WITHDRAWAL_EXIT_CODE:-1} -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Emergency Cancel:        $([ "$RUN_EMERGENCY_CANCEL_TEST" = true ] && ([ ${EMERGENCY_CANCEL_EXIT_CODE:-1} -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo ""
echo "========================================================================="
echo "  TEST SUITE EXECUTION COMPLETED"
echo "========================================================================="
