#!/bin/bash

echo "========================================================================="
echo "  X/REP INTEGRATION TESTS - MASTER TEST RUNNER"
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
RUN_CHALLENGE_TEST=true
RUN_INVITATION_TEST=true
RUN_MEMBER_TEST=true
RUN_DREAM_TOKEN_TEST=true
RUN_INITIATIVE_TEST=true
RUN_INTERIM_TEST=true
RUN_STAKING_TEST=true
RUN_COMPLEX_TEST=true
RUN_EDGE_CASES_TEST=true
RUN_ENDBLOCKER_TEST=true
FUND_ALICE=true
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
        --no-challenge)
            RUN_CHALLENGE_TEST=false
            shift
            ;;
        --no-invitation)
            RUN_INVITATION_TEST=false
            shift
            ;;
        --no-member)
            RUN_MEMBER_TEST=false
            shift
            ;;
        --no-dream)
            RUN_DREAM_TOKEN_TEST=false
            shift
            ;;
        --no-initiative)
            RUN_INITIATIVE_TEST=false
            shift
            ;;
        --no-interim)
            RUN_INTERIM_TEST=false
            shift
            ;;
        --no-staking)
            RUN_STAKING_TEST=false
            shift
            ;;
        --no-complex)
            RUN_COMPLEX_TEST=false
            shift
            ;;
        --no-edge-cases)
            RUN_EDGE_CASES_TEST=false
            shift
            ;;
        --no-endblocker)
            RUN_ENDBLOCKER_TEST=false
            shift
            ;;
        --no-funding)
            FUND_ALICE=false
            shift
            ;;
        --reset-chain)
            RESET_CHAIN=true
            shift
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_CHALLENGE_TEST=false
            RUN_INVITATION_TEST=false
            RUN_MEMBER_TEST=false
            RUN_DREAM_TOKEN_TEST=false
            RUN_INITIATIVE_TEST=false
            RUN_INTERIM_TEST=false
            RUN_STAKING_TEST=false
            RUN_COMPLEX_TEST=false
            RUN_EDGE_CASES_TEST=false
            RUN_ENDBLOCKER_TEST=false
            shift
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            shift
            ;;
        --no-tests)
            RUN_CHALLENGE_TEST=false
            RUN_INVITATION_TEST=false
            RUN_MEMBER_TEST=false
            RUN_DREAM_TOKEN_TEST=false
            RUN_INITIATIVE_TEST=false
            RUN_INTERIM_TEST=false
            RUN_STAKING_TEST=false
            RUN_COMPLEX_TEST=false
            RUN_EDGE_CASES_TEST=false
            RUN_ENDBLOCKER_TEST=false
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup       Skip setup_test_accounts.sh"
            echo "  --no-challenge   Skip challenge_test.sh"
            echo "  --no-invitation  Skip invitation_test.sh"
            echo "  --no-member      Skip member_test.sh"
            echo "  --no-dream       Skip dream_token_test.sh"
            echo "  --no-initiative  Skip initiative_test.sh"
            echo "  --no-interim     Skip interim_test.sh"
            echo "  --no-staking     Skip staking_test.sh"
            echo "  --no-complex     Skip complex_scenarios_test.sh"
            echo "  --no-edge-cases  Skip edge_cases_test.sh"
            echo "  --no-endblocker  Skip endblocker_test.sh"
            echo "  --no-funding     Skip funding Alice with extra DREAM"
            echo "  --no-tests       Skip all tests (use with --restore-setup for manual testing)"
            echo "  --reset-chain    Reset chain before running tests (requires manual restart)"
            echo "  --save-setup     Run setup, save chain state, then exit"
            echo "  --restore-setup  Restore saved setup state, then run tests"
            echo "  --help           Show this help message"
            echo ""
            echo "Default: Run full test suite with setup and funding"
            echo ""
            echo "Workflow for fast iteration:"
            echo "  1. bash $0 --save-setup      # One-time: run setup and save state"
            echo "  2. bash $0 --restore-setup   # Restore and run tests (repeatable)"
            echo ""
            echo "Workflow for manual testing:"
            echo "  1. bash $0 --restore-setup --no-tests  # Restore state, start chain, exit"
            echo "  2. bash ./committee_escalation_test.sh  # Run specific test manually"
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
    echo "ℹ️  Restore mode: Chain will be stopped and restarted during restore"
else
    # Check if chain is running
    if ! $BINARY status &> /dev/null; then
        echo "❌ Chain is not running!"
        echo ""
        echo "Please start the chain first:"
        echo "  cd /home/chill/cosmos/sparkdream/sparkdream"
        echo "  ignite chain serve"
        echo ""
        exit 1
    fi

    BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
    echo "✅ Chain is running (block height: $BLOCK_HEIGHT)"
fi

# Skip Alice checks for restore-setup (chain not running yet)
if [ "$RESTORE_SETUP" != true ]; then
    # Check if Alice exists
    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null)
    if [ -z "$ALICE_ADDR" ]; then
        echo "❌ Alice account not found in keyring"
        echo "   Make sure the chain is initialized with genesis accounts"
        exit 1
    fi
    echo "✅ Alice account found: $ALICE_ADDR"

    # Check Alice's current DREAM balance
    ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
    if [ -z "$ALICE_MEMBER" ] || [ "$ALICE_MEMBER" == "null" ]; then
        echo "⚠️  Alice is not a member in x/rep (genesis may not be loaded)"
        ALICE_DREAM=0
    else
        ALICE_DREAM=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // 0')
        ALICE_CREDITS=$(echo "$ALICE_MEMBER" | jq -r '.member.invitation_credits // 0')
        ALICE_DREAM_DISPLAY=$(echo "scale=2; $ALICE_DREAM / 1000000" | bc 2>/dev/null || echo "0")
        echo "✅ Alice DREAM balance: $ALICE_DREAM_DISPLAY DREAM"
        echo "   Alice invitation credits: $ALICE_CREDITS"
    fi

    echo ""
fi

# ========================================================================
# Chain Reset (if requested)
# ========================================================================
if [ "$RESET_CHAIN" = true ]; then
    echo "=== CHAIN RESET REQUESTED ==="
    echo ""
    echo "⚠️  To reset the chain:"
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
        echo "❌ Snapshot 'post-setup' not found at: $SNAPSHOT_PATH"
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
        echo "❌ Failed to restore setup state (exit code: $RESTORE_EXIT_CODE)"
        exit 1
    fi

    echo ""
    echo "✅ Setup state restored successfully"
    echo ""

    # Load .test_env from restored state
    if [ -f "$SCRIPT_DIR/.test_env" ]; then
        source "$SCRIPT_DIR/.test_env"
        echo "✅ Loaded test environment from restored snapshot"
    else
        echo "⚠️  Warning: .test_env not found in restored snapshot"
    fi

    echo ""
    echo "→ Starting chain..."

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
            echo "✅ Chain is running (block height: $BLOCK_HEIGHT)"
            break
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    # Final check
    if ! $BINARY status &> /dev/null; then
        echo "❌ Chain failed to start after 30 seconds"
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
    echo "💾 SAVE-SETUP MODE"
    echo "   → Running setup, saving chain state, then exiting"
    echo ""
elif [ "$RESTORE_SETUP" = true ]; then
    echo ""
    echo "♻️  RESTORE-SETUP MODE"
    echo "   → Restored saved setup state, now running tests"
    echo ""
fi
echo "  1. Setup test accounts:      $([ "$RUN_SETUP" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo "  2. Fund Alice (if needed):   $([ "$FUND_ALICE" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo "  3. Member lifecycle test:    $([ "$RUN_MEMBER_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo "  4. Invitation test:          $([ "$RUN_INVITATION_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo "  5. DREAM token test:         $([ "$RUN_DREAM_TOKEN_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo "  6. Initiative flow test:     $([ "$RUN_INITIATIVE_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo "  7. Staking mechanics test:   $([ "$RUN_STAKING_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo "  8. Interim test:             $([ "$RUN_INTERIM_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo "  9. Challenge test:           $([ "$RUN_CHALLENGE_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo " 10. Complex scenarios test:   $([ "$RUN_COMPLEX_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo " 11. Edge cases test:          $([ "$RUN_EDGE_CASES_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
echo " 12. EndBlocker test:          $([ "$RUN_ENDBLOCKER_TEST" = true ] && echo "✅ YES" || echo "⏭️  SKIP")"
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
# Step 1: Setup Test Accounts
# ========================================================================
if [ "$RUN_SETUP" = true ]; then
    echo "========================================================================="
    echo "STEP 1: SETUP TEST ACCOUNTS"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/setup_test_accounts.sh"
    SETUP_EXIT_CODE=$?

    if [ $SETUP_EXIT_CODE -ne 0 ]; then
        echo ""
        echo "❌ Setup failed with exit code: $SETUP_EXIT_CODE"
        echo "   Cannot proceed with tests"
        exit 1
    fi

    echo ""
    echo "✅ Setup completed successfully"
    echo ""

    # If --save-setup mode, save chain state and exit
    if [ "$SAVE_SETUP" = true ]; then
        echo "========================================================================="
        echo "SAVING CHAIN STATE"
        echo "========================================================================="
        echo ""

        SNAPSHOT_SCRIPT="$SCRIPT_DIR/../snapshot_datadir.sh"
        if [ ! -f "$SNAPSHOT_SCRIPT" ]; then
            echo "❌ snapshot_datadir.sh not found at $SNAPSHOT_SCRIPT"
            echo "   Cannot save chain state"
            exit 1
        fi

        echo "Saving chain state to 'post-setup' snapshot..."
        bash "$SNAPSHOT_SCRIPT" post-setup "$SCRIPT_DIR/snapshots"
        SAVE_EXIT_CODE=$?

        if [ $SAVE_EXIT_CODE -ne 0 ]; then
            echo "❌ Failed to save chain state (exit code: $SAVE_EXIT_CODE)"
            exit 1
        fi

        echo ""
        echo "========================================================================="
        echo "SAVE-SETUP MODE COMPLETE"
        echo "========================================================================="
        echo ""
        echo "✅ Setup completed and chain state saved to 'post-setup' snapshot"
        echo ""
        echo "Snapshot location: $SCRIPT_DIR/snapshots/post-setup"
        echo ""
        echo "To run tests from this saved state:"
        echo "  bash test/rep/run_all_tests.sh --restore-setup"
        echo ""
        echo "The restore-setup option will:"
        echo "  1. Stop the chain and restore the saved state"
        echo "  2. Restart the chain automatically"
        echo "  3. Run all integration tests"
        echo "  4. Can be repeated for fast iteration"
        echo ""
        exit 0
    fi

    sleep 2
else
    echo "========================================================================="
    echo "STEP 1: SETUP (SKIPPED)"
    echo "========================================================================="
    echo ""

    # Verify .test_env exists
    if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
        echo "❌ Test environment not found (.test_env missing)"
        echo "   Run without --no-setup flag to create it"
        exit 1
    fi
    echo "✅ Using existing test environment"
    echo ""
fi

# ========================================================================
# Step 2: Fund Alice (if needed)
# ========================================================================
if [ "$FUND_ALICE" = true ]; then
    echo "========================================================================="
    echo "STEP 2: FUND ALICE WITH ADDITIONAL DREAM"
    echo "========================================================================="
    echo ""

    # Load test environment to get test account addresses
    source "$SCRIPT_DIR/.test_env"

    # Get Alice's address if not already set
    if [ -z "$ALICE_ADDR" ]; then
        ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null)
    fi

    # Check Alice's current balance
    ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
    ALICE_DREAM=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // "0"')

    # Ensure ALICE_DREAM is not empty or null
    if [ -z "$ALICE_DREAM" ] || [ "$ALICE_DREAM" == "null" ]; then
        ALICE_DREAM="0"
    fi

    ALICE_DREAM_DISPLAY=$(echo "scale=2; $ALICE_DREAM / 1000000" | bc 2>/dev/null || echo "0")

    echo "Alice current balance: $ALICE_DREAM_DISPLAY DREAM"

    # Determine if funding is needed
    DREAM_NEEDED_FOR_TESTS=300  # 100 for transfer + 50 for tip + 200 for gift
    DREAM_NEEDED_MICRO=$((DREAM_NEEDED_FOR_TESTS * 1000000))

    # Ensure ALICE_DREAM is numeric before comparison
    if ! [[ "$ALICE_DREAM" =~ ^[0-9]+$ ]]; then
        ALICE_DREAM="0"
    fi

    if [ "$ALICE_DREAM" -lt "$DREAM_NEEDED_MICRO" ]; then
        DREAM_TO_ADD=$((DREAM_NEEDED_MICRO - ALICE_DREAM))
        DREAM_TO_ADD_DISPLAY=$(echo "scale=2; $DREAM_TO_ADD / 1000000" | bc 2>/dev/null || echo "0")

        echo "⚠️  Alice needs at least $DREAM_NEEDED_FOR_TESTS DREAM for tests"
        echo "   Funding Alice with $DREAM_TO_ADD_DISPLAY DREAM from challenger..."
        echo ""

        # Use challenger account to tip Alice (challenger has 250 DREAM from setup + 100 from challenge test)
        # Tip instead of gift since Alice doesn't invite herself
        FUNDING_AMOUNT=$DREAM_TO_ADD
        if [ $FUNDING_AMOUNT -gt 100000000 ]; then
            # Tip max is 100 DREAM, so do multiple tips if needed
            REMAINING=$FUNDING_AMOUNT
            TIP_COUNT=0

            while [ $REMAINING -gt 0 ]; do
                TIP_AMOUNT=$((REMAINING < 100000000 ? REMAINING : 100000000))
                TIP_COUNT=$((TIP_COUNT + 1))

                echo "  Tip #$TIP_COUNT: Sending $(echo "scale=2; $TIP_AMOUNT / 1000000" | bc) DREAM..."

                TX_RES=$($BINARY tx rep transfer-dream \
                    $ALICE_ADDR \
                    "$TIP_AMOUNT" \
                    "tip" \
                    "Funding for tests" \
                    --from challenger \
                    --chain-id $CHAIN_ID \
                    --keyring-backend test \
                    --fees 5000uspark \
                    -y \
                    --output json 2>&1)

                TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
                if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
                    sleep 2
                    REMAINING=$((REMAINING - TIP_AMOUNT))
                else
                    echo "  ❌ Failed to send tip"
                    break
                fi
            done
        else
            # Single tip is enough
            TX_RES=$($BINARY tx rep transfer-dream \
                $ALICE_ADDR \
                "$FUNDING_AMOUNT" \
                "tip" \
                "Funding for tests" \
                --from challenger \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000uspark \
                -y \
                --output json 2>&1)

            TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
            if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
                sleep 2
                echo "  ✅ Funded Alice"
            else
                echo "  ❌ Failed to fund Alice"
            fi
        fi

        # Verify new balance
        ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
        ALICE_DREAM_NEW=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // 0')
        ALICE_DREAM_NEW_DISPLAY=$(echo "scale=2; $ALICE_DREAM_NEW / 1000000" | bc 2>/dev/null || echo "0")

        echo ""
        echo "Alice new balance: $ALICE_DREAM_NEW_DISPLAY DREAM"
        echo "✅ Funding complete"
    else
        echo "✅ Alice has sufficient DREAM ($ALICE_DREAM_DISPLAY >= $DREAM_NEEDED_FOR_TESTS)"
    fi

    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 2: FUNDING (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 3: Run Member Lifecycle Test
# ========================================================================
if [ "$RUN_MEMBER_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 3: MEMBER LIFECYCLE TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/member_test.sh"
    MEMBER_EXIT_CODE=$?

    echo ""
    if [ $MEMBER_EXIT_CODE -eq 0 ]; then
        echo "✅ Member lifecycle test completed"
    else
        echo "⚠️  Member lifecycle test exited with code: $MEMBER_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 3: MEMBER LIFECYCLE TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 4: Run Invitation Test
# ========================================================================
if [ "$RUN_INVITATION_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 4: INVITATION TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/invitation_test.sh"
    INVITATION_EXIT_CODE=$?

    echo ""
    if [ $INVITATION_EXIT_CODE -eq 0 ]; then
        echo "✅ Invitation test completed"
    else
        echo "⚠️  Invitation test exited with code: $INVITATION_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 4: INVITATION TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 5: Run DREAM Token Test
# ========================================================================
if [ "$RUN_DREAM_TOKEN_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 5: DREAM TOKEN TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/dream_token_test.sh"
    DREAM_EXIT_CODE=$?

    echo ""
    if [ $DREAM_EXIT_CODE -eq 0 ]; then
        echo "✅ DREAM token test completed"
    else
        echo "⚠️  DREAM token test exited with code: $DREAM_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 5: DREAM TOKEN TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 6: Run Initiative Flow Test
# ========================================================================
if [ "$RUN_INITIATIVE_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 6: INITIATIVE FLOW TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/initiative_test.sh"
    INITIATIVE_EXIT_CODE=$?

    echo ""
    if [ $INITIATIVE_EXIT_CODE -eq 0 ]; then
        echo "✅ Initiative flow test completed"
    else
        echo "⚠️  Initiative flow test exited with code: $INITIATIVE_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 6: INITIATIVE FLOW TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 7: Run Staking Mechanics Test
# ========================================================================
if [ "$RUN_STAKING_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 7: STAKING MECHANICS TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/staking_test.sh"
    STAKING_EXIT_CODE=$?

    echo ""
    if [ $STAKING_EXIT_CODE -eq 0 ]; then
        echo "✅ Staking mechanics test completed"
    else
        echo "⚠️  Staking mechanics test exited with code: $STAKING_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 7: STAKING MECHANICS TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 8: Run Interim Compensation Test
# ========================================================================
if [ "$RUN_INTERIM_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 8: INTERIM COMPENSATION TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/interim_test.sh"
    INTERIM_EXIT_CODE=$?

    echo ""
    if [ $INTERIM_EXIT_CODE -eq 0 ]; then
        echo "✅ Interim compensation test completed"
    else
        echo "⚠️  Interim compensation test exited with code: $INTERIM_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 8: INTERIM COMPENSATION TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 9: Run Challenge Test
# ========================================================================
if [ "$RUN_CHALLENGE_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 9: CHALLENGE TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/challenge_test.sh"
    CHALLENGE_EXIT_CODE=$?

    echo ""
    if [ $CHALLENGE_EXIT_CODE -eq 0 ]; then
        echo "✅ Challenge test completed"
    else
        echo "⚠️  Challenge test exited with code: $CHALLENGE_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 9: CHALLENGE TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 10: Run Complex Scenarios Test
# ========================================================================
if [ "$RUN_COMPLEX_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 10: COMPLEX SCENARIOS TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/complex_scenarios_test.sh"
    COMPLEX_EXIT_CODE=$?

    echo ""
    if [ $COMPLEX_EXIT_CODE -eq 0 ]; then
        echo "✅ Complex scenarios test completed"
    else
        echo "⚠️  Complex scenarios test exited with code: $COMPLEX_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 10: COMPLEX SCENARIOS TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 11: Run Edge Cases Test
# ========================================================================
if [ "$RUN_EDGE_CASES_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 11: EDGE CASES TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/edge_cases_test.sh"
    EDGE_CASES_EXIT_CODE=$?

    echo ""
    if [ $EDGE_CASES_EXIT_CODE -eq 0 ]; then
        echo "✅ Edge cases test completed"
    else
        echo "⚠️  Edge cases test exited with code: $EDGE_CASES_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 11: EDGE CASES TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 12: Run EndBlocker Test
# ========================================================================
if [ "$RUN_ENDBLOCKER_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 12: ENDBLOCKER TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/endblocker_test.sh"
    ENDBLOCKER_EXIT_CODE=$?

    echo ""
    if [ $ENDBLOCKER_EXIT_CODE -eq 0 ]; then
        echo "✅ EndBlocker test completed"
    else
        echo "⚠️  EndBlocker test exited with code: $ENDBLOCKER_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 12: ENDBLOCKER TEST (SKIPPED)"
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
echo "  Setup:              $([ "$RUN_SETUP" = true ] && echo "✅ Completed" || echo "⏭️  Skipped")"
echo "  Alice Funding:      $([ "$FUND_ALICE" = true ] && echo "✅ Completed" || echo "⏭️  Skipped")"
echo "  Member Test:        $([ "$RUN_MEMBER_TEST" = true ] && ([ $MEMBER_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo "  Invitation Test:    $([ "$RUN_INVITATION_TEST" = true ] && ([ $INVITATION_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo "  DREAM Token Test:   $([ "$RUN_DREAM_TOKEN_TEST" = true ] && ([ $DREAM_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo "  Initiative Test:    $([ "$RUN_INITIATIVE_TEST" = true ] && ([ $INITIATIVE_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo "  Staking Test:       $([ "$RUN_STAKING_TEST" = true ] && ([ $STAKING_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo "  Interim Test:       $([ "$RUN_INTERIM_TEST" = true ] && ([ $INTERIM_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo "  Challenge Test:     $([ "$RUN_CHALLENGE_TEST" = true ] && ([ $CHALLENGE_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo "  Complex Test:       $([ "$RUN_COMPLEX_TEST" = true ] && ([ $COMPLEX_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo "  Edge Cases Test:    $([ "$RUN_EDGE_CASES_TEST" = true ] && ([ $EDGE_CASES_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo "  EndBlocker Test:    $([ "$RUN_ENDBLOCKER_TEST" = true ] && ([ $ENDBLOCKER_EXIT_CODE -eq 0 ] && echo "✅ Passed" || echo "⚠️  Issues") || echo "⏭️  Skipped")"
echo ""

# Final Alice balance
ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
ALICE_DREAM_FINAL=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // 0')
ALICE_DREAM_FINAL_DISPLAY=$(echo "scale=2; $ALICE_DREAM_FINAL / 1000000" | bc 2>/dev/null || echo "0")
echo "Final Alice balance: $ALICE_DREAM_FINAL_DISPLAY DREAM"
echo ""
echo "========================================================================="
echo "✅ TEST SUITE EXECUTION COMPLETED"
echo "========================================================================="
