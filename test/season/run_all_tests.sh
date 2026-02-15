#!/bin/bash

echo "========================================================================="
echo "  X/SEASON INTEGRATION TESTS - MASTER TEST RUNNER"
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
RUN_PROFILE_TEST=true
RUN_GUILD_TEST=true
RUN_GUILD_ADVANCED_TEST=true
RUN_QUEST_TEST=true
RUN_SEASON_TEST=true
RUN_MODERATION_TEST=true
RUN_XP_TRACKING_TEST=true
RUN_OPERATIONAL_PARAMS_TEST=true
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
        --no-profile)
            RUN_PROFILE_TEST=false
            shift
            ;;
        --no-guild)
            RUN_GUILD_TEST=false
            shift
            ;;
        --no-guild-advanced)
            RUN_GUILD_ADVANCED_TEST=false
            shift
            ;;
        --no-quest)
            RUN_QUEST_TEST=false
            shift
            ;;
        --no-season)
            RUN_SEASON_TEST=false
            shift
            ;;
        --no-moderation)
            RUN_MODERATION_TEST=false
            shift
            ;;
        --no-xp-tracking)
            RUN_XP_TRACKING_TEST=false
            shift
            ;;
        --no-operational-params)
            RUN_OPERATIONAL_PARAMS_TEST=false
            shift
            ;;
        --only-setup)
            RUN_PROFILE_TEST=false
            RUN_GUILD_TEST=false
            RUN_GUILD_ADVANCED_TEST=false
            RUN_QUEST_TEST=false
            RUN_SEASON_TEST=false
            RUN_MODERATION_TEST=false
            RUN_XP_TRACKING_TEST=false
            RUN_OPERATIONAL_PARAMS_TEST=false
            shift
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_PROFILE_TEST=false
            RUN_GUILD_TEST=false
            RUN_GUILD_ADVANCED_TEST=false
            RUN_QUEST_TEST=false
            RUN_SEASON_TEST=false
            RUN_MODERATION_TEST=false
            RUN_XP_TRACKING_TEST=false
            RUN_OPERATIONAL_PARAMS_TEST=false
            shift
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            shift
            ;;
        --no-tests)
            RUN_PROFILE_TEST=false
            RUN_GUILD_TEST=false
            RUN_GUILD_ADVANCED_TEST=false
            RUN_QUEST_TEST=false
            RUN_SEASON_TEST=false
            RUN_MODERATION_TEST=false
            RUN_XP_TRACKING_TEST=false
            RUN_OPERATIONAL_PARAMS_TEST=false
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup          Skip setup_test_accounts.sh"
            echo "  --no-profile        Skip profile_test.sh"
            echo "  --no-guild          Skip guild_test.sh"
            echo "  --no-guild-advanced Skip guild_advanced_test.sh"
            echo "  --no-quest          Skip quest_test.sh"
            echo "  --no-season         Skip season_test.sh"
            echo "  --no-moderation     Skip display_name_moderation_test.sh"
            echo "  --no-xp-tracking    Skip xp_tracking_test.sh"
            echo "  --no-operational-params  Skip operational_params_test.sh"
            echo "  --only-setup        Run only setup (skip all tests)"
            echo "  --save-setup        Run setup, save chain state, then exit"
            echo "  --restore-setup     Restore saved setup state, then run tests"
            echo "  --no-tests          Skip all tests (use with --restore-setup for manual testing)"
            echo "  --help              Show this help message"
            echo ""
            echo "Default: Run full test suite with setup"
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
    echo "Restore mode: Chain will be stopped and restarted during restore"
else
    # Check if chain is running
    if ! $BINARY status &> /dev/null; then
        echo "Chain is not running!"
        echo ""
        echo "Please start the chain first:"
        echo "  cd /home/chill/cosmos/sparkdream/sparkdream"
        echo "  ignite chain serve"
        echo ""
        exit 1
    fi

    BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
    echo "Chain is running (block height: $BLOCK_HEIGHT)"

    # Check if Alice exists
    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null)
    if [ -z "$ALICE_ADDR" ]; then
        echo "Alice account not found in keyring"
        echo "   Make sure the chain is initialized with genesis accounts"
        exit 1
    fi
    echo "Alice account found: $ALICE_ADDR"

    # Check Alice's balance
    ALICE_SPARK=$($BINARY query bank balances $ALICE_ADDR --output json 2>/dev/null | jq -r '[.balances[] | select(.denom=="uspark") | .amount] | if length > 0 then .[0] else "0" end')
    echo "Alice SPARK balance: $ALICE_SPARK uspark"

    # Check if Alice is a member in x/rep
    ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
    if [ -z "$ALICE_MEMBER" ] || [ "$ALICE_MEMBER" == "null" ]; then
        echo "Alice is not a member in x/rep (genesis may not be loaded)"
    else
        ALICE_DREAM=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // 0')
        ALICE_DREAM_DISPLAY=$(echo "scale=2; $ALICE_DREAM / 1000000" | bc 2>/dev/null || echo "0")
        echo "Alice DREAM balance: $ALICE_DREAM_DISPLAY DREAM"
    fi
fi

echo ""

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

# ========================================================================
# Test Execution Plan
# ========================================================================
echo "=== TEST EXECUTION PLAN ==="
if [ "$SAVE_SETUP" = true ]; then
    echo ""
    echo "SAVE-SETUP MODE"
    echo "   -> Running setup, saving chain state, then exiting"
    echo ""
elif [ "$RESTORE_SETUP" = true ]; then
    echo ""
    echo "RESTORE-SETUP MODE"
    echo "   -> Restored saved setup state, now running tests"
    echo ""
fi
echo "  1. Setup test accounts:       $([ "$RUN_SETUP" = true ] && echo "YES" || echo "SKIP")"
echo "  2. Profile test:              $([ "$RUN_PROFILE_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  3. Guild test:                $([ "$RUN_GUILD_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  4. Guild advanced test:       $([ "$RUN_GUILD_ADVANCED_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  5. Quest test:                $([ "$RUN_QUEST_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  6. Display name moderation:   $([ "$RUN_MODERATION_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  7. XP tracking test:          $([ "$RUN_XP_TRACKING_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  8. Operational params test:   $([ "$RUN_OPERATIONAL_PARAMS_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  9. Season test (last):        $([ "$RUN_SEASON_TEST" = true ] && echo "YES" || echo "SKIP")"
echo ""

if [ "$SAVE_SETUP" != true ] && [ "$RESTORE_SETUP" != true ]; then
    read -p "Proceed with test execution? (yes/no): " PROCEED
    if [ "$PROCEED" != "yes" ]; then
        echo "Aborted."
        exit 0
    fi
    echo ""
fi

# Initialize exit code variables
SETUP_EXIT_CODE=0
PROFILE_EXIT_CODE=0
GUILD_EXIT_CODE=0
GUILD_ADVANCED_EXIT_CODE=0
QUEST_EXIT_CODE=0
SEASON_EXIT_CODE=0
MODERATION_EXIT_CODE=0
XP_TRACKING_EXIT_CODE=0

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
        echo "Setup failed with exit code: $SETUP_EXIT_CODE"
        echo "   Cannot proceed with tests"
        exit 1
    fi

    echo ""
    echo "Setup completed successfully"
    echo ""

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
        echo "  bash test/season/run_all_tests.sh --restore-setup"
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
        echo "Test environment not found (.test_env missing)"
        echo "   Run without --no-setup flag to create it"
        exit 1
    fi
    echo "Using existing test environment"
    echo ""
fi

# Load test environment
source "$SCRIPT_DIR/.test_env"

# ========================================================================
# Step 2: Profile Test
# ========================================================================
if [ "$RUN_PROFILE_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 2: PROFILE TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/profile_test.sh"
    PROFILE_EXIT_CODE=$?

    echo ""
    if [ $PROFILE_EXIT_CODE -eq 0 ]; then
        echo "Profile test completed"
    else
        echo "Profile test exited with code: $PROFILE_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 2: PROFILE TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 3: Guild Test
# ========================================================================
if [ "$RUN_GUILD_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 3: GUILD TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/guild_test.sh"
    GUILD_EXIT_CODE=$?

    echo ""
    if [ $GUILD_EXIT_CODE -eq 0 ]; then
        echo "Guild test completed"
    else
        echo "Guild test exited with code: $GUILD_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 3: GUILD TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 4: Guild Advanced Test
# ========================================================================
if [ "$RUN_GUILD_ADVANCED_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 4: GUILD ADVANCED TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/guild_advanced_test.sh"
    GUILD_ADVANCED_EXIT_CODE=$?

    echo ""
    if [ $GUILD_ADVANCED_EXIT_CODE -eq 0 ]; then
        echo "Guild advanced test completed"
    else
        echo "Guild advanced test exited with code: $GUILD_ADVANCED_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 4: GUILD ADVANCED TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 5: Quest Test
# ========================================================================
if [ "$RUN_QUEST_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 5: QUEST TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/quest_test.sh"
    QUEST_EXIT_CODE=$?

    echo ""
    if [ $QUEST_EXIT_CODE -eq 0 ]; then
        echo "Quest test completed"
    else
        echo "Quest test exited with code: $QUEST_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 5: QUEST TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 6: Display Name Moderation Test
# ========================================================================
if [ "$RUN_MODERATION_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 6: DISPLAY NAME MODERATION TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/display_name_moderation_test.sh"
    MODERATION_EXIT_CODE=$?

    echo ""
    if [ $MODERATION_EXIT_CODE -eq 0 ]; then
        echo "Display name moderation test completed"
    else
        echo "Display name moderation test exited with code: $MODERATION_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 6: DISPLAY NAME MODERATION TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 7: XP Tracking Test
# ========================================================================
if [ "$RUN_XP_TRACKING_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 7: XP TRACKING TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/xp_tracking_test.sh"
    XP_TRACKING_EXIT_CODE=$?

    echo ""
    if [ $XP_TRACKING_EXIT_CODE -eq 0 ]; then
        echo "XP tracking test completed"
    else
        echo "XP tracking test exited with code: $XP_TRACKING_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 7: XP TRACKING TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 8: Operational Params Test
# ========================================================================
if [ "$RUN_OPERATIONAL_PARAMS_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 8: OPERATIONAL PARAMS TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/operational_params_test.sh"
    OPERATIONAL_PARAMS_EXIT_CODE=$?

    echo ""
    if [ $OPERATIONAL_PARAMS_EXIT_CODE -eq 0 ]; then
        echo "Operational params test completed"
    else
        echo "Operational params test exited with code: $OPERATIONAL_PARAMS_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 8: OPERATIONAL PARAMS TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 9: Season Test (runs LAST - needs to wait for season transition)
# ========================================================================
if [ "$RUN_SEASON_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 9: SEASON TEST (transition testing)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/season_test.sh"
    SEASON_EXIT_CODE=$?

    echo ""
    if [ $SEASON_EXIT_CODE -eq 0 ]; then
        echo "Season test completed"
    else
        echo "Season test exited with code: $SEASON_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 9: SEASON TEST (SKIPPED)"
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
echo "  Setup:                  $([ "$RUN_SETUP" = true ] && echo "Completed" || echo "Skipped")"
echo "  Profile Test:           $([ "$RUN_PROFILE_TEST" = true ] && ([ $PROFILE_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Guild Test:             $([ "$RUN_GUILD_TEST" = true ] && ([ $GUILD_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Guild Advanced Test:    $([ "$RUN_GUILD_ADVANCED_TEST" = true ] && ([ $GUILD_ADVANCED_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Quest Test:             $([ "$RUN_QUEST_TEST" = true ] && ([ $QUEST_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Moderation Test:        $([ "$RUN_MODERATION_TEST" = true ] && ([ $MODERATION_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  XP Tracking Test:       $([ "$RUN_XP_TRACKING_TEST" = true ] && ([ $XP_TRACKING_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Op Params Test:         $([ "$RUN_OPERATIONAL_PARAMS_TEST" = true ] && ([ ${OPERATIONAL_PARAMS_EXIT_CODE:-1} -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Season Test:            $([ "$RUN_SEASON_TEST" = true ] && ([ $SEASON_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo ""
echo "========================================================================="
echo "TEST SUITE EXECUTION COMPLETED"
echo "========================================================================="
