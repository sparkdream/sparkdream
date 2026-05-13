#!/bin/bash

echo "========================================================================="
echo "  X/REP INTEGRATION TESTS - MASTER TEST RUNNER"
echo "========================================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../check_testparams.sh"
source "$SCRIPT_DIR/../_timing.sh"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Wall-clock timing for the suite (Started/Ended/Duration in summary).
SUITE_START_EPOCH=$(timing_now_epoch)
SUITE_START_HUMAN=$(timing_now_human)

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
RUN_OPERATIONAL_PARAMS_TEST=true
RUN_CONTENT_CHALLENGE_TEST=true
RUN_BOND_LOCKED_TEST=true
RUN_BONDED_ROLE_TEST=true
# Master gate for the entire test-execution loop. The per-test RUN_*_TEST
# flags above only cover steps 3-15 (which were authored with explicit
# gating); steps 16-24 (staking_errors, validation, anon_challenge,
# trust_level, member_report, gov_action_appeal, jury_participation,
# tag_budget, tag_moderation) are gated only on `--no-tests` setting this
# master flag to false. Without this, `--restore-setup --no-tests` would
# still execute the un-gated tests and drift the freshly-restored state
# before the user could run a specific test manually.
RUN_TESTS=true
FUND_ALICE=true
RESET_CHAIN=false
SAVE_SETUP=false
RESTORE_SETUP=false

AUTO_SNAPSHOT=true
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
        --no-operational-params)
            RUN_OPERATIONAL_PARAMS_TEST=false
            shift
            ;;
        --no-content-challenge)
            RUN_CONTENT_CHALLENGE_TEST=false
            shift
            ;;
        --no-bond-locked)
            RUN_BOND_LOCKED_TEST=false
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
            RUN_OPERATIONAL_PARAMS_TEST=false
            RUN_CONTENT_CHALLENGE_TEST=false
            RUN_BOND_LOCKED_TEST=false
            shift
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            shift
            ;;
        --no-tests)
            RUN_TESTS=false
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
            RUN_OPERATIONAL_PARAMS_TEST=false
            RUN_CONTENT_CHALLENGE_TEST=false
            RUN_BOND_LOCKED_TEST=false
            RUN_BONDED_ROLE_TEST=false
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
            echo "  --no-operational-params  Skip operational_params_test.sh"
            echo "  --no-content-challenge   Skip content_challenge_test.sh"
            echo "  --no-funding     Skip funding Alice with extra DREAM"
            echo "  --no-tests       Skip all tests (use with --restore-setup for manual testing)"
            echo "  --reset-chain    Reset chain before running tests (requires manual restart)"
            echo "  --save-setup     Run setup, save chain state, then exit"
            echo "  --restore-setup  Restore saved setup state, then run tests"
            echo "  --no-auto-snapshot Disable auto-snapshot (run setup every time, no caching)"
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
        --no-auto-snapshot)
            AUTO_SNAPSHOT=false
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done


# Auto-snapshot: when no explicit save/restore flag is passed, reuse an
# existing fresh snapshot or save one after setup. See test/_auto_snapshot.sh.
source "$SCRIPT_DIR/../_auto_snapshot.sh"
auto_snapshot_pre
# ========================================================================
# Pre-flight Checks
# ========================================================================
echo "=== PRE-FLIGHT CHECKS ==="

# Skip chain running check for restore-setup (it will start the chain)
if [ "$RESTORE_SETUP" = true ]; then
    echo "[INFO] Restore mode: Chain will be stopped and restarted during restore"
else
    # Check if chain is running
    if ! $BINARY status &> /dev/null; then
        echo "[FAIL] Chain is not running!"
        echo ""
        echo "Please start the chain first:"
        echo "  cd /home/chill/cosmos/sparkdream/sparkdream"
        echo "  ignite chain serve"
        echo ""
        exit 1
    fi

    BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
    echo "[ OK ] Chain is running (block height: $BLOCK_HEIGHT)"
fi

# Skip Alice checks for restore-setup (chain not running yet)
if [ "$RESTORE_SETUP" != true ]; then
    # Check if Alice exists
    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null)
    if [ -z "$ALICE_ADDR" ]; then
        echo "[FAIL] Alice account not found in keyring"
        echo "   Make sure the chain is initialized with genesis accounts"
        exit 1
    fi
    echo "[ OK ] Alice account found: $ALICE_ADDR"

    # Check Alice's current DREAM balance
    ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
    if [ -z "$ALICE_MEMBER" ] || [ "$ALICE_MEMBER" == "null" ]; then
        echo "[WARN] Alice is not a member in x/rep (genesis may not be loaded)"
        ALICE_DREAM=0
    else
        ALICE_DREAM=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // 0')
        ALICE_CREDITS=$(echo "$ALICE_MEMBER" | jq -r '.member.invitation_credits // 0')
        ALICE_DREAM_DISPLAY=$(echo "scale=2; $ALICE_DREAM / 1000000" | bc 2>/dev/null || echo "0")
        echo "[ OK ] Alice DREAM balance: $ALICE_DREAM_DISPLAY DREAM"
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
    echo "[WARN] To reset the chain:"
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
        echo "[FAIL] Snapshot 'post-setup' not found at: $SNAPSHOT_PATH"
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
        echo "[FAIL] Failed to restore setup state (exit code: $RESTORE_EXIT_CODE)"
        exit 1
    fi

    echo ""
    echo "[ OK ] Setup state restored successfully"
    echo ""

    # Load .test_env from restored state
    if [ -f "$SCRIPT_DIR/.test_env" ]; then
        source "$SCRIPT_DIR/.test_env"
        echo "[ OK ] Loaded test environment from restored snapshot"
    else
        echo "[WARN] Warning: .test_env not found in restored snapshot"
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
            echo "[ OK ] Chain is running (block height: $BLOCK_HEIGHT)"
            break
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    # Final check
    if ! $BINARY status &> /dev/null; then
        echo "[FAIL] Chain failed to start after 30 seconds"
        echo "   Check logs: tail -f /tmp/chain_after_restore.log"
        exit 1
    fi

    echo ""

    # If `--restore-setup --no-tests` was passed, the user wants a fresh
    # post-setup chain to run a specific test against — don't fall through
    # into the test-execution loop, which would drift the state via the
    # un-gated steps 16-24 (staking_errors, validation, anon_challenge,
    # trust_level, member_report, gov_action_appeal, jury_participation,
    # tag_budget, tag_moderation) before the user's manual test could run.
    if [ "$RUN_TESTS" != true ]; then
        BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
        echo "========================================================================="
        echo "  RESTORE COMPLETE — TEST EXECUTION SKIPPED (--no-tests)"
        echo "========================================================================="
        echo ""
        echo "  Chain home:   $HOME/.sparkdream"
        echo "  Block height: $BLOCK_HEIGHT"
        echo "  Test env:     $SCRIPT_DIR/.test_env  (already sourced)"
        echo "  Chain log:    /tmp/chain_after_restore.log"
        echo ""
        echo "  Run any specific test against the freshly restored state:"
        echo "    bash $SCRIPT_DIR/challenge_test.sh"
        echo "    bash $SCRIPT_DIR/<other_test>.sh"
        echo ""
        echo "  Stop the chain when done:"
        echo "    pkill -f 'sparkdreamd start --home $HOME/.sparkdream'"
        echo ""
        exit 0
    fi
fi

# ========================================================================
# Test Execution Plan
# ========================================================================
echo "=== TEST EXECUTION PLAN ==="
if [ "$SAVE_SETUP" = true ]; then
    echo ""
    echo "[SAVE] SAVE-SETUP MODE"
    echo "   → Running setup, saving chain state, then exiting"
    echo ""
elif [ "$RESTORE_SETUP" = true ]; then
    echo ""
    echo "[RESTORE] RESTORE-SETUP MODE"
    echo "   → Restored saved setup state, now running tests"
    echo ""
fi
echo "  1. Setup test accounts:      $([ "$RUN_SETUP" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo "  2. Fund Alice (if needed):   $([ "$FUND_ALICE" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo "  3. Member lifecycle test:    $([ "$RUN_MEMBER_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo "  4. Content challenge test:   $([ "$RUN_CONTENT_CHALLENGE_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo "  5. Invitation test:          $([ "$RUN_INVITATION_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo "  6. DREAM token test:         $([ "$RUN_DREAM_TOKEN_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo "  7. Initiative flow test:     $([ "$RUN_INITIATIVE_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo "  8. Staking mechanics test:   $([ "$RUN_STAKING_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo "  9. Interim test:             $([ "$RUN_INTERIM_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo " 10. Challenge test:           $([ "$RUN_CHALLENGE_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo " 11. Complex scenarios test:   $([ "$RUN_COMPLEX_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo " 12. Edge cases test:          $([ "$RUN_EDGE_CASES_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo " 13. EndBlocker test:          $([ "$RUN_ENDBLOCKER_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo " 14. Operational params test:  $([ "$RUN_OPERATIONAL_PARAMS_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo " 15. Bond locked test (P0):   $([ "$RUN_BOND_LOCKED_TEST" = true ] && echo "[ OK ] YES" || echo "[SKIP] SKIP")"
echo ""

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
        echo "[FAIL] Setup failed with exit code: $SETUP_EXIT_CODE"
        echo "   Cannot proceed with tests"
        exit 1
    fi

    echo ""
    echo "[ OK ] Setup completed successfully"
    echo ""

    # Auto-save the post-setup snapshot if AUTO_SNAPSHOT was set and
    # no fresh snapshot existed at the start of this run.
    auto_snapshot_post

    # If --save-setup mode, save chain state and exit
    if [ "$SAVE_SETUP" = true ]; then
        echo "========================================================================="
        echo "SAVING CHAIN STATE"
        echo "========================================================================="
        echo ""

        SNAPSHOT_SCRIPT="$SCRIPT_DIR/../snapshot_datadir.sh"
        if [ ! -f "$SNAPSHOT_SCRIPT" ]; then
            echo "[FAIL] snapshot_datadir.sh not found at $SNAPSHOT_SCRIPT"
            echo "   Cannot save chain state"
            exit 1
        fi

        echo "Saving chain state to 'post-setup' snapshot..."
        bash "$SNAPSHOT_SCRIPT" post-setup "$SCRIPT_DIR/snapshots"
        SAVE_EXIT_CODE=$?

        if [ $SAVE_EXIT_CODE -ne 0 ]; then
            echo "[FAIL] Failed to save chain state (exit code: $SAVE_EXIT_CODE)"
            exit 1
        fi

        echo ""
        echo "========================================================================="
        echo "SAVE-SETUP MODE COMPLETE"
        echo "========================================================================="
        echo ""
        echo "[ OK ] Setup completed and chain state saved to 'post-setup' snapshot"
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
        echo "[FAIL] Test environment not found (.test_env missing)"
        echo "   Run without --no-setup flag to create it"
        exit 1
    fi
    echo "[ OK ] Using existing test environment"
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

        echo "[WARN] Alice needs at least $DREAM_NEEDED_FOR_TESTS DREAM for tests"
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
                    echo "  [FAIL] Failed to send tip"
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
                echo "  [ OK ] Funded Alice"
            else
                echo "  [FAIL] Failed to fund Alice"
            fi
        fi

        # Verify new balance
        ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
        ALICE_DREAM_NEW=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // 0')
        ALICE_DREAM_NEW_DISPLAY=$(echo "scale=2; $ALICE_DREAM_NEW / 1000000" | bc 2>/dev/null || echo "0")

        echo ""
        echo "Alice new balance: $ALICE_DREAM_NEW_DISPLAY DREAM"
        echo "[ OK ] Funding complete"
    else
        echo "[ OK ] Alice has sufficient DREAM ($ALICE_DREAM_DISPLAY >= $DREAM_NEEDED_FOR_TESTS)"
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
        echo "[ OK ] Member lifecycle test completed"
    else
        echo "[FAIL] Member lifecycle test exited with code: $MEMBER_EXIT_CODE"
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
# Step 4: Run Content Challenge Test (early to avoid juror reputation decay)
# ========================================================================
if [ "$RUN_CONTENT_CHALLENGE_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 4: CONTENT CHALLENGE TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/content_challenge_test.sh"
    CONTENT_CHALLENGE_EXIT_CODE=$?

    echo ""
    if [ $CONTENT_CHALLENGE_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Content challenge test completed"
    else
        echo "[FAIL] Content challenge test exited with code: $CONTENT_CHALLENGE_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 4: CONTENT CHALLENGE TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 5: Run Invitation Test
# ========================================================================
if [ "$RUN_INVITATION_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 5: INVITATION TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/invitation_test.sh"
    INVITATION_EXIT_CODE=$?

    echo ""
    if [ $INVITATION_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Invitation test completed"
    else
        echo "[FAIL] Invitation test exited with code: $INVITATION_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 5: INVITATION TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 6: Run DREAM Token Test
# ========================================================================
if [ "$RUN_DREAM_TOKEN_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 6: DREAM TOKEN TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/dream_token_test.sh"
    DREAM_EXIT_CODE=$?

    echo ""
    if [ $DREAM_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] DREAM token test completed"
    else
        echo "[FAIL] DREAM token test exited with code: $DREAM_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 6: DREAM TOKEN TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 7: Run Initiative Flow Test
# ========================================================================
if [ "$RUN_INITIATIVE_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 7: INITIATIVE FLOW TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/initiative_test.sh"
    INITIATIVE_EXIT_CODE=$?

    echo ""
    if [ $INITIATIVE_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Initiative flow test completed"
    else
        echo "[FAIL] Initiative flow test exited with code: $INITIATIVE_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 7: INITIATIVE FLOW TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 8: Run Staking Mechanics Test
# ========================================================================
if [ "$RUN_STAKING_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 8: STAKING MECHANICS TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/staking_test.sh"
    STAKING_EXIT_CODE=$?

    echo ""
    if [ $STAKING_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Staking mechanics test completed"
    else
        echo "[FAIL] Staking mechanics test exited with code: $STAKING_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 8: STAKING MECHANICS TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 9: Run Interim Compensation Test
# ========================================================================
if [ "$RUN_INTERIM_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 9: INTERIM COMPENSATION TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/interim_test.sh"
    INTERIM_EXIT_CODE=$?

    echo ""
    if [ $INTERIM_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Interim compensation test completed"
    else
        echo "[FAIL] Interim compensation test exited with code: $INTERIM_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 9: INTERIM COMPENSATION TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 10: Run Challenge Test
# ========================================================================
if [ "$RUN_CHALLENGE_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 10: CHALLENGE TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/challenge_test.sh"
    CHALLENGE_EXIT_CODE=$?

    echo ""
    if [ $CHALLENGE_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Challenge test completed"
    else
        echo "[FAIL] Challenge test exited with code: $CHALLENGE_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 10: CHALLENGE TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 11: Run Complex Scenarios Test
# ========================================================================
if [ "$RUN_COMPLEX_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 11: COMPLEX SCENARIOS TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/complex_scenarios_test.sh"
    COMPLEX_EXIT_CODE=$?

    echo ""
    if [ $COMPLEX_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Complex scenarios test completed"
    else
        echo "[FAIL] Complex scenarios test exited with code: $COMPLEX_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 11: COMPLEX SCENARIOS TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 12: Run Edge Cases Test
# ========================================================================
if [ "$RUN_EDGE_CASES_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 12: EDGE CASES TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/edge_cases_test.sh"
    EDGE_CASES_EXIT_CODE=$?

    echo ""
    if [ $EDGE_CASES_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Edge cases test completed"
    else
        echo "[FAIL] Edge cases test exited with code: $EDGE_CASES_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 12: EDGE CASES TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 13: Run EndBlocker Test
# ========================================================================
if [ "$RUN_ENDBLOCKER_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 13: ENDBLOCKER TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/endblocker_test.sh"
    ENDBLOCKER_EXIT_CODE=$?

    echo ""
    if [ $ENDBLOCKER_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] EndBlocker test completed"
    else
        echo "[FAIL] EndBlocker test exited with code: $ENDBLOCKER_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 13: ENDBLOCKER TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 14: Run Operational Params Test
# ========================================================================
if [ "$RUN_OPERATIONAL_PARAMS_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 14: OPERATIONAL PARAMS TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/operational_params_test.sh"
    OPERATIONAL_PARAMS_EXIT_CODE=$?

    echo ""
    if [ $OPERATIONAL_PARAMS_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Operational params test completed"
    else
        echo "[FAIL] Operational params test exited with code: $OPERATIONAL_PARAMS_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 14: OPERATIONAL PARAMS TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 15: Run Bond Locked Test (P0 Security)
# ========================================================================
if [ "$RUN_BOND_LOCKED_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 15: BOND LOCKED TEST (P0 Security)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/bond_locked_test.sh"
    BOND_LOCKED_EXIT_CODE=$?

    echo ""
    if [ $BOND_LOCKED_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Bond locked test completed"
    else
        echo "[FAIL] Bond locked test exited with code: $BOND_LOCKED_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 15: BOND LOCKED TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 15b: Run Bonded Role Test (generic MsgBondRole / MsgUnbondRole + queries)
# ========================================================================
if [ "$RUN_BONDED_ROLE_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 15b: BONDED ROLE TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/bonded_role_test.sh"
    BONDED_ROLE_EXIT_CODE=$?

    echo ""
    if [ $BONDED_ROLE_EXIT_CODE -eq 0 ]; then
        echo "[ OK ] Bonded role test completed"
    else
        echo "[FAIL] Bonded role test exited with code: $BONDED_ROLE_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 15b: BONDED ROLE TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Steps 16-24: Always-on tests (file-presence gated, no per-test RUN_*).
# Wrapped in a single RUN_TESTS guard so `--no-tests` skips them too —
# without this, `--restore-setup --no-tests` would fall through and drift
# the freshly restored chain state before the user could run a specific
# test manually. The matching `fi` is right before the Summary block.
# ========================================================================
if [ "$RUN_TESTS" = true ]; then

# ========================================================================
# Step 16: Run Staking Errors Test (P1)
# ========================================================================
echo "========================================================================="
echo "STEP 16: STAKING ERRORS TEST (P1)"
echo "========================================================================="
echo ""

bash "$SCRIPT_DIR/staking_errors_test.sh"
STAKING_ERRORS_EXIT_CODE=$?

echo ""
if [ $STAKING_ERRORS_EXIT_CODE -eq 0 ]; then
    echo "Staking errors test completed"
else
    echo "[FAIL] Staking errors test exited with code: $STAKING_ERRORS_EXIT_CODE"
fi
echo ""
sleep 2

# ========================================================================
# Step 17: Run Validation Test (P2)
# ========================================================================
if [ -f "$SCRIPT_DIR/validation_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 17: VALIDATION TEST (P2)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/validation_test.sh"
    VALIDATION_EXIT_CODE=$?

    echo ""
    if [ $VALIDATION_EXIT_CODE -eq 0 ]; then
        echo "Validation test completed"
    else
        echo "[FAIL] Validation test exited with code: $VALIDATION_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

# ========================================================================
# Step 18: Run Anonymous Challenge Test (Shield integration)
# ========================================================================
if [ -f "$SCRIPT_DIR/anon_challenge_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 18: ANONYMOUS CHALLENGE TEST (Shield integration)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/anon_challenge_test.sh"
    ANON_CHALLENGE_EXIT_CODE=$?

    echo ""
    if [ $ANON_CHALLENGE_EXIT_CODE -eq 0 ]; then
        echo "Anonymous challenge test completed"
    else
        echo "[FAIL] Anonymous challenge test exited with code: $ANON_CHALLENGE_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

# ========================================================================
# Step 19: Run Trust Level Test (P2)
# ========================================================================
if [ -f "$SCRIPT_DIR/trust_level_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 19: TRUST LEVEL TEST (P2)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/trust_level_test.sh"
    TRUST_LEVEL_EXIT_CODE=$?

    echo ""
    if [ $TRUST_LEVEL_EXIT_CODE -eq 0 ]; then
        echo "Trust level test completed"
    else
        echo "[FAIL] Trust level test exited with code: $TRUST_LEVEL_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

# ========================================================================
# Step 20: Run Member Report Test (accountability)
# ========================================================================
if [ -f "$SCRIPT_DIR/member_report_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 20: MEMBER REPORT TEST (accountability)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/member_report_test.sh"
    MEMBER_REPORT_EXIT_CODE=$?

    echo ""
    if [ $MEMBER_REPORT_EXIT_CODE -eq 0 ]; then
        echo "Member report test completed"
    else
        echo "[FAIL] Member report test exited with code: $MEMBER_REPORT_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

# ========================================================================
# Step 21: Run Gov Action Appeal Test (accountability)
# ========================================================================
if [ -f "$SCRIPT_DIR/gov_action_appeal_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 21: GOV ACTION APPEAL TEST (accountability)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/gov_action_appeal_test.sh"
    GOV_ACTION_APPEAL_EXIT_CODE=$?

    echo ""
    if [ $GOV_ACTION_APPEAL_EXIT_CODE -eq 0 ]; then
        echo "Gov action appeal test completed"
    else
        echo "[FAIL] Gov action appeal test exited with code: $GOV_ACTION_APPEAL_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

# ========================================================================
# Step 22: Run Jury Participation Test (accountability)
# ========================================================================
if [ -f "$SCRIPT_DIR/jury_participation_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 22: JURY PARTICIPATION TEST (accountability)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/jury_participation_test.sh"
    JURY_PARTICIPATION_EXIT_CODE=$?

    echo ""
    if [ $JURY_PARTICIPATION_EXIT_CODE -eq 0 ]; then
        echo "Jury participation test completed"
    else
        echo "[FAIL] Jury participation test exited with code: $JURY_PARTICIPATION_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

# ========================================================================
# Step 23: Run Tag Budget Test (tag primitives)
# ========================================================================
if [ -f "$SCRIPT_DIR/tag_budget_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 23: TAG BUDGET TEST (tag primitives)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/tag_budget_test.sh"
    TAG_BUDGET_EXIT_CODE=$?

    echo ""
    if [ $TAG_BUDGET_EXIT_CODE -eq 0 ]; then
        echo "Tag budget test completed"
    else
        echo "[FAIL] Tag budget test exited with code: $TAG_BUDGET_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

# ========================================================================
# Step 24: Run Tag Moderation Test (tag primitives)
# ========================================================================
if [ -f "$SCRIPT_DIR/tag_moderation_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 24: TAG MODERATION TEST (tag primitives)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/tag_moderation_test.sh"
    TAG_MODERATION_EXIT_CODE=$?

    echo ""
    if [ $TAG_MODERATION_EXIT_CODE -eq 0 ]; then
        echo "Tag moderation test completed"
    else
        echo "[FAIL] Tag moderation test exited with code: $TAG_MODERATION_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

# ========================================================================
# Step 25: Run Project Approval Test (tier + council-lock)
# ========================================================================
if [ -f "$SCRIPT_DIR/project_approval_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 25: PROJECT APPROVAL TEST (tier + council-lock)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/project_approval_test.sh"
    PROJECT_APPROVAL_EXIT_CODE=$?

    echo ""
    if [ $PROJECT_APPROVAL_EXIT_CODE -eq 0 ]; then
        echo "Project approval test completed"
    else
        echo "[FAIL] Project approval test exited with code: $PROJECT_APPROVAL_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

# ========================================================================
# Step 26: Run Project Lifecycle Test (proposal caps + TTL expiry)
# ========================================================================
# Covers the proposal-time hard caps on requested_budget/requested_spark and
# the EndBlocker-driven TTL on PROPOSED projects. The TTL portion temporarily
# lowers proposed_project_expiry_blocks via an op-params council proposal and
# restores it afterwards, so this test is safe to leave at any position in
# the suite — but it does take ~90s end-to-end (two op-params proposals
# bracket a ~35s sleep), so we run it last.
if [ -f "$SCRIPT_DIR/project_lifecycle_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 26: PROJECT LIFECYCLE TEST (proposal caps + TTL expiry)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/project_lifecycle_test.sh"
    PROJECT_LIFECYCLE_EXIT_CODE=$?

    echo ""
    if [ $PROJECT_LIFECYCLE_EXIT_CODE -eq 0 ]; then
        echo "Project lifecycle test completed"
    else
        echo "[FAIL] Project lifecycle test exited with code: $PROJECT_LIFECYCLE_EXIT_CODE"
    fi
    echo ""
    sleep 2
fi

fi  # end of RUN_TESTS guard wrapping steps 16-24

# ========================================================================
# Summary
# ========================================================================
echo "========================================================================="
echo "  TEST SUITE SUMMARY"
echo "========================================================================="
echo ""
SUITE_END_EPOCH=$(timing_now_epoch)
SUITE_END_HUMAN=$(timing_now_human)
timing_print_summary_block "$SUITE_START_EPOCH" "$SUITE_END_EPOCH" \
    "$SUITE_START_HUMAN" "$SUITE_END_HUMAN"
echo ""
# ========================================================================
# Aggregate results across every sub-test. A test "counts" toward failure
# only if it was actually selected to run (RUN_*=true). Skipped tests stay
# neutral. Steps without an RUN_* gate (e.g. STAKING_ERRORS) are checked
# unconditionally with `true` as the enabled flag.
#
# IMPORTANT: this runner USED to print "[WARN] X exited with code: N" and
# then exit 0, which masked real failures from the parent suite (the
# sequential full-suite run reported `>>> PASSED: x/rep` while the inner
# jury-review test was failing). Every sub-step is now [FAIL]'d on
# non-zero exit AND aggregated below so the parent suite sees a non-zero
# exit. The summary table previously only listed 13 of the 22 sub-tests;
# the missing ones (steps 14-24) are now included here too.
# ========================================================================
SUITE_FAILED=0
declare -a FAILED_TESTS=()

check_test() {
    local enabled=$1
    local code=$2
    local label=$3
    if [ "$enabled" = true ] && [ "${code:-1}" -ne 0 ]; then
        SUITE_FAILED=1
        FAILED_TESTS+=("$label (exit ${code:-1})")
    fi
}

print_row() {
    local enabled=$1
    local code=$2
    local label=$3
    if [ "$enabled" != true ]; then
        printf "  %-26s [SKIP] Skipped\n" "$label"
    elif [ "${code:-1}" -eq 0 ]; then
        printf "  %-26s [PASS] Passed\n" "$label"
    else
        printf "  %-26s [FAIL] Issues (exit ${code:-1})\n" "$label"
    fi
}

echo "Results:"
echo "  Setup:                     $([ "$RUN_SETUP" = true ] && echo "[ OK ] Completed" || echo "[SKIP] Skipped")"
echo "  Alice Funding:             $([ "$FUND_ALICE" = true ] && echo "[ OK ] Completed" || echo "[SKIP] Skipped")"
print_row "$RUN_MEMBER_TEST"              "${MEMBER_EXIT_CODE:-1}"              "Member Test"
print_row "$RUN_CONTENT_CHALLENGE_TEST"   "${CONTENT_CHALLENGE_EXIT_CODE:-1}"   "Content Challenge"
print_row "$RUN_INVITATION_TEST"          "${INVITATION_EXIT_CODE:-1}"          "Invitation Test"
print_row "$RUN_DREAM_TOKEN_TEST"         "${DREAM_EXIT_CODE:-1}"               "DREAM Token Test"
print_row "$RUN_INITIATIVE_TEST"          "${INITIATIVE_EXIT_CODE:-1}"          "Initiative Test"
print_row "$RUN_STAKING_TEST"             "${STAKING_EXIT_CODE:-1}"             "Staking Test"
print_row "$RUN_INTERIM_TEST"             "${INTERIM_EXIT_CODE:-1}"             "Interim Test"
print_row "$RUN_CHALLENGE_TEST"           "${CHALLENGE_EXIT_CODE:-1}"           "Challenge Test"
print_row "$RUN_COMPLEX_TEST"             "${COMPLEX_EXIT_CODE:-1}"             "Complex Test"
print_row "$RUN_EDGE_CASES_TEST"          "${EDGE_CASES_EXIT_CODE:-1}"          "Edge Cases Test"
print_row "$RUN_ENDBLOCKER_TEST"          "${ENDBLOCKER_EXIT_CODE:-1}"          "EndBlocker Test"
print_row "$RUN_OPERATIONAL_PARAMS_TEST"  "${OPERATIONAL_PARAMS_EXIT_CODE:-1}"  "Op Params Test"
print_row "$RUN_BOND_LOCKED_TEST"         "${BOND_LOCKED_EXIT_CODE:-1}"         "Bond Locked Test"
print_row "${RUN_BONDED_ROLE_TEST:-true}" "${BONDED_ROLE_EXIT_CODE:-0}"         "Bonded Role Test"
# Steps below are gated only on the script file being present (no RUN_*).
# Defaulting EXIT_CODE to 0 means "did not run" is treated as neutral, not
# failed — matching the existing behaviour of those `if [ -f ... ]` blocks.
print_row true                            "${STAKING_ERRORS_EXIT_CODE:-1}"      "Staking Errors Test"
print_row true                            "${VALIDATION_EXIT_CODE:-0}"          "Validation Test"
print_row true                            "${ANON_CHALLENGE_EXIT_CODE:-0}"      "Anonymous Challenge Test"
print_row true                            "${TRUST_LEVEL_EXIT_CODE:-0}"         "Trust Level Test"
print_row true                            "${MEMBER_REPORT_EXIT_CODE:-0}"       "Member Report Test"
print_row true                            "${GOV_ACTION_APPEAL_EXIT_CODE:-0}"   "Gov Action Appeal Test"
print_row true                            "${JURY_PARTICIPATION_EXIT_CODE:-0}"  "Jury Participation Test"
print_row true                            "${TAG_BUDGET_EXIT_CODE:-0}"          "Tag Budget Test"
print_row true                            "${TAG_MODERATION_EXIT_CODE:-0}"      "Tag Moderation Test"
print_row true                            "${PROJECT_APPROVAL_EXIT_CODE:-0}"    "Project Approval"
print_row true                            "${PROJECT_LIFECYCLE_EXIT_CODE:-0}"   "Project Lifecycle"
echo ""

check_test "$RUN_MEMBER_TEST"              "${MEMBER_EXIT_CODE:-1}"              "Member Test"
check_test "$RUN_CONTENT_CHALLENGE_TEST"   "${CONTENT_CHALLENGE_EXIT_CODE:-1}"   "Content Challenge"
check_test "$RUN_INVITATION_TEST"          "${INVITATION_EXIT_CODE:-1}"          "Invitation Test"
check_test "$RUN_DREAM_TOKEN_TEST"         "${DREAM_EXIT_CODE:-1}"               "DREAM Token Test"
check_test "$RUN_INITIATIVE_TEST"          "${INITIATIVE_EXIT_CODE:-1}"          "Initiative Test"
check_test "$RUN_STAKING_TEST"             "${STAKING_EXIT_CODE:-1}"             "Staking Test"
check_test "$RUN_INTERIM_TEST"             "${INTERIM_EXIT_CODE:-1}"             "Interim Test"
check_test "$RUN_CHALLENGE_TEST"           "${CHALLENGE_EXIT_CODE:-1}"           "Challenge Test"
check_test "$RUN_COMPLEX_TEST"             "${COMPLEX_EXIT_CODE:-1}"             "Complex Test"
check_test "$RUN_EDGE_CASES_TEST"          "${EDGE_CASES_EXIT_CODE:-1}"          "Edge Cases Test"
check_test "$RUN_ENDBLOCKER_TEST"          "${ENDBLOCKER_EXIT_CODE:-1}"          "EndBlocker Test"
check_test "$RUN_OPERATIONAL_PARAMS_TEST"  "${OPERATIONAL_PARAMS_EXIT_CODE:-1}"  "Op Params Test"
check_test "$RUN_BOND_LOCKED_TEST"         "${BOND_LOCKED_EXIT_CODE:-1}"         "Bond Locked Test"
check_test "${RUN_BONDED_ROLE_TEST:-true}" "${BONDED_ROLE_EXIT_CODE:-0}"         "Bonded Role Test"
check_test true                            "${STAKING_ERRORS_EXIT_CODE:-1}"      "Staking Errors Test"
check_test true                            "${VALIDATION_EXIT_CODE:-0}"          "Validation Test"
check_test true                            "${ANON_CHALLENGE_EXIT_CODE:-0}"      "Anonymous Challenge Test"
check_test true                            "${TRUST_LEVEL_EXIT_CODE:-0}"         "Trust Level Test"
check_test true                            "${MEMBER_REPORT_EXIT_CODE:-0}"       "Member Report Test"
check_test true                            "${GOV_ACTION_APPEAL_EXIT_CODE:-0}"   "Gov Action Appeal Test"
check_test true                            "${JURY_PARTICIPATION_EXIT_CODE:-0}"  "Jury Participation Test"
check_test true                            "${TAG_BUDGET_EXIT_CODE:-0}"          "Tag Budget Test"
check_test true                            "${TAG_MODERATION_EXIT_CODE:-0}"      "Tag Moderation Test"
check_test true                            "${PROJECT_APPROVAL_EXIT_CODE:-0}"    "Project Approval"
check_test true                            "${PROJECT_LIFECYCLE_EXIT_CODE:-0}"   "Project Lifecycle"

# Final Alice balance
ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
ALICE_DREAM_FINAL=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // 0')
ALICE_DREAM_FINAL_DISPLAY=$(echo "scale=2; $ALICE_DREAM_FINAL / 1000000" | bc 2>/dev/null || echo "0")
echo "Final Alice balance: $ALICE_DREAM_FINAL_DISPLAY DREAM"
echo ""
echo "========================================================================="
echo "TEST SUITE EXECUTION COMPLETED"
echo "========================================================================="

if [ $SUITE_FAILED -ne 0 ]; then
    echo ""
    echo "RESULT: x/rep suite has failures — exiting non-zero."
    for f in "${FAILED_TESTS[@]}"; do
        echo "  [FAIL] $f"
    done
    exit 1
fi
exit 0
