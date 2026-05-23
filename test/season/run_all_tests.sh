#!/bin/bash

echo "========================================================================="
echo "  X/SEASON INTEGRATION TESTS - MASTER TEST RUNNER"
echo "========================================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"
source "$SCRIPT_DIR/../check_testparams.sh"
source "$SCRIPT_DIR/../_timing.sh"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Wall-clock timing for the suite (Started/Ended/Duration in summary).
SUITE_START_EPOCH=$(timing_now_epoch)
SUITE_START_HUMAN=$(timing_now_human)

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
RUN_NOMINATION_TEST=true
RUN_GUILD_ERRORS_TEST=true
RUN_QUEST_ERRORS_TEST=true
# Master gate for the entire test-execution loop. The validation_test step
# is gated only on file presence (`if [ -f ... ]`), so without this flag
# `--restore-setup --no-tests` would still run it and drift the freshly
# restored chain state before the user could run a specific test manually.
RUN_TESTS=true
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
        --no-nomination)
            RUN_NOMINATION_TEST=false
            shift
            ;;
        --no-guild-errors)
            RUN_GUILD_ERRORS_TEST=false
            shift
            ;;
        --no-quest-errors)
            RUN_QUEST_ERRORS_TEST=false
            shift
            ;;
        --only-setup)
            RUN_PROFILE_TEST=false
            RUN_GUILD_TEST=false
            RUN_GUILD_ADVANCED_TEST=false
            RUN_GUILD_ERRORS_TEST=false
            RUN_QUEST_TEST=false
            RUN_QUEST_ERRORS_TEST=false
            RUN_SEASON_TEST=false
            RUN_MODERATION_TEST=false
            RUN_XP_TRACKING_TEST=false
            RUN_OPERATIONAL_PARAMS_TEST=false
            RUN_NOMINATION_TEST=false
            shift
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_PROFILE_TEST=false
            RUN_GUILD_TEST=false
            RUN_GUILD_ADVANCED_TEST=false
            RUN_GUILD_ERRORS_TEST=false
            RUN_QUEST_TEST=false
            RUN_QUEST_ERRORS_TEST=false
            RUN_SEASON_TEST=false
            RUN_MODERATION_TEST=false
            RUN_XP_TRACKING_TEST=false
            RUN_OPERATIONAL_PARAMS_TEST=false
            RUN_NOMINATION_TEST=false
            shift
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            shift
            ;;
        --no-tests)
            RUN_TESTS=false
            RUN_PROFILE_TEST=false
            RUN_GUILD_TEST=false
            RUN_GUILD_ADVANCED_TEST=false
            RUN_GUILD_ERRORS_TEST=false
            RUN_QUEST_TEST=false
            RUN_QUEST_ERRORS_TEST=false
            RUN_SEASON_TEST=false
            RUN_MODERATION_TEST=false
            RUN_XP_TRACKING_TEST=false
            RUN_OPERATIONAL_PARAMS_TEST=false
            RUN_NOMINATION_TEST=false
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
            echo "  --no-guild-errors   Skip guild_errors_test.sh"
            echo "  --no-quest          Skip quest_test.sh"
            echo "  --no-quest-errors   Skip quest_errors_test.sh"
            echo "  --no-season         Skip season_test.sh"
            echo "  --no-moderation     Skip display_name_moderation_test.sh"
            echo "  --no-xp-tracking    Skip xp_tracking_test.sh"
            echo "  --no-operational-params  Skip operational_params_test.sh"
            echo "  --no-nomination     Skip nomination_test.sh"
            echo "  --only-setup        Run only setup (skip all tests)"
            echo "  --save-setup        Run setup, save chain state, then exit"
            echo "  --restore-setup     Restore saved setup state, then run tests"
            echo "  --no-auto-snapshot Disable auto-snapshot (run setup every time, no caching)"
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

# ============================================================================
# Parallel-runner test order manifest (read by test/run_parallel.sh).
#
# `test/run_parallel.sh::get_module_test_order` parses the single-line
# `# TEST_ORDER:` comment below to discover the canonical execution
# order for the parallel runner. Comment-based manifest is preferred over
# the previous "no-op run_test stub + actual run_test calls" pattern
# because there is nowhere for the manifest to drift to — it isn't shell
# code, so a future contributor can't accidentally execute it in the
# wrong scope or duplicate it later in the file.
#
# Without this manifest the parallel runner falls back to alphabetical
# ordering, which breaks order-dependent suites (e.g. xp_tracking_test
# must run BEFORE season_test transitions the season; guild_errors_test
# must run BEFORE guild_test consumes the cooldown on display_user).
# ============================================================================
# TEST_ORDER: profile_test.sh guild_test.sh guild_advanced_test.sh guild_errors_test.sh quest_test.sh quest_errors_test.sh display_name_moderation_test.sh xp_tracking_test.sh operational_params_test.sh nomination_test.sh season_test.sh validation_test.sh

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
    ALICE_SPARK=$($BINARY query bank balances $ALICE_ADDR --output json 2>/dev/null | jq -r --arg denom "$BOND_DENOM" '[.balances[] | select(.denom==$denom) | .amount] | if length > 0 then .[0] else "0" end')
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

    # `--restore-setup --no-tests`: the user wants a fresh post-setup chain
    # to run a specific test against — don't fall through into the test
    # loop, which would drift state via validation_test (file-presence
    # gated, so per-test --no-* flags don't reach it).
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
        echo "    bash $SCRIPT_DIR/guild_errors_test.sh"
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
echo " 4b. Guild errors test:         $([ "$RUN_GUILD_ERRORS_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  5. Quest test:                $([ "$RUN_QUEST_TEST" = true ] && echo "YES" || echo "SKIP")"
echo " 5b. Quest errors test:         $([ "$RUN_QUEST_ERRORS_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  6. Display name moderation:   $([ "$RUN_MODERATION_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  7. XP tracking test:          $([ "$RUN_XP_TRACKING_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  8. Operational params test:   $([ "$RUN_OPERATIONAL_PARAMS_TEST" = true ] && echo "YES" || echo "SKIP")"
echo "  9. Nomination test:           $([ "$RUN_NOMINATION_TEST" = true ] && echo "YES" || echo "SKIP")"
echo " 10. Season test (last):        $([ "$RUN_SEASON_TEST" = true ] && echo "YES" || echo "SKIP")"
echo ""

# Initialize exit code variables
SETUP_EXIT_CODE=0
PROFILE_EXIT_CODE=0
GUILD_EXIT_CODE=0
GUILD_ADVANCED_EXIT_CODE=0
GUILD_ERRORS_EXIT_CODE=0
QUEST_EXIT_CODE=0
QUEST_ERRORS_EXIT_CODE=0
SEASON_EXIT_CODE=0
MODERATION_EXIT_CODE=0
NOMINATION_EXIT_CODE=0
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
        echo "[FAIL] Profile test exited with code: $PROFILE_EXIT_CODE"
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
        echo "[FAIL] Guild test exited with code: $GUILD_EXIT_CODE"
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
        echo "[FAIL] Guild advanced test exited with code: $GUILD_ADVANCED_EXIT_CODE"
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
# Step 4b: Guild Errors Test
# ========================================================================
if [ "$RUN_GUILD_ERRORS_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 4b: GUILD ERRORS TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/guild_errors_test.sh"
    GUILD_ERRORS_EXIT_CODE=$?

    echo ""
    if [ $GUILD_ERRORS_EXIT_CODE -eq 0 ]; then
        echo "Guild errors test completed"
    else
        echo "[FAIL] Guild errors test exited with code: $GUILD_ERRORS_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 4b: GUILD ERRORS TEST (SKIPPED)"
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
        echo "[FAIL] Quest test exited with code: $QUEST_EXIT_CODE"
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
# Step 5b: Quest Errors Test
# ========================================================================
if [ "$RUN_QUEST_ERRORS_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 5b: QUEST ERRORS TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/quest_errors_test.sh"
    QUEST_ERRORS_EXIT_CODE=$?

    echo ""
    if [ $QUEST_ERRORS_EXIT_CODE -eq 0 ]; then
        echo "Quest errors test completed"
    else
        echo "[FAIL] Quest errors test exited with code: $QUEST_ERRORS_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 5b: QUEST ERRORS TEST (SKIPPED)"
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
        echo "[FAIL] Display name moderation test exited with code: $MODERATION_EXIT_CODE"
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
        echo "[FAIL] XP tracking test exited with code: $XP_TRACKING_EXIT_CODE"
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
        echo "[FAIL] Operational params test exited with code: $OPERATIONAL_PARAMS_EXIT_CODE"
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
# Step 9: Nomination Test
# ========================================================================
if [ "$RUN_NOMINATION_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 9: NOMINATION TEST"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/nomination_test.sh"
    NOMINATION_EXIT_CODE=$?

    echo ""
    if [ $NOMINATION_EXIT_CODE -eq 0 ]; then
        echo "Nomination test completed"
    else
        echo "[FAIL] Nomination test exited with code: $NOMINATION_EXIT_CODE"
    fi
    echo ""
    sleep 2
else
    echo "========================================================================="
    echo "STEP 9: NOMINATION TEST (SKIPPED)"
    echo "========================================================================="
    echo ""
fi

# ========================================================================
# Step 10: Season Test (runs LAST - needs to wait for season transition)
# ========================================================================
if [ "$RUN_SEASON_TEST" = true ]; then
    echo "========================================================================="
    echo "STEP 10: SEASON TEST (transition testing)"
    echo "========================================================================="
    echo ""

    bash "$SCRIPT_DIR/season_test.sh"
    SEASON_EXIT_CODE=$?

    echo ""
    if [ $SEASON_EXIT_CODE -eq 0 ]; then
        echo "Season test completed"
    else
        echo "[FAIL] Season test exited with code: $SEASON_EXIT_CODE"
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
# Step 10: Run Validation Test (P2)
# ========================================================================
# Gated on RUN_TESTS too — file-presence alone would let `--no-tests`
# bypass the per-test flags and drift the chain state. The early exit in
# the restore branch handles `--restore-setup --no-tests`; this defends
# against `--no-tests` without `--restore-setup`.
if [ "$RUN_TESTS" = true ] && [ -f "$SCRIPT_DIR/validation_test.sh" ]; then
    echo "========================================================================="
    echo "STEP 10: VALIDATION TEST (P2)"
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
echo "Results:"
echo "  Setup:                  $([ "$RUN_SETUP" = true ] && echo "Completed" || echo "Skipped")"
echo "  Profile Test:           $([ "$RUN_PROFILE_TEST" = true ] && ([ $PROFILE_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Guild Test:             $([ "$RUN_GUILD_TEST" = true ] && ([ $GUILD_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Guild Advanced Test:    $([ "$RUN_GUILD_ADVANCED_TEST" = true ] && ([ $GUILD_ADVANCED_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Guild Errors Test:      $([ "$RUN_GUILD_ERRORS_TEST" = true ] && ([ ${GUILD_ERRORS_EXIT_CODE:-1} -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Quest Test:             $([ "$RUN_QUEST_TEST" = true ] && ([ $QUEST_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Quest Errors Test:      $([ "$RUN_QUEST_ERRORS_TEST" = true ] && ([ ${QUEST_ERRORS_EXIT_CODE:-1} -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Moderation Test:        $([ "$RUN_MODERATION_TEST" = true ] && ([ $MODERATION_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  XP Tracking Test:       $([ "$RUN_XP_TRACKING_TEST" = true ] && ([ $XP_TRACKING_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Op Params Test:         $([ "$RUN_OPERATIONAL_PARAMS_TEST" = true ] && ([ ${OPERATIONAL_PARAMS_EXIT_CODE:-1} -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Nomination Test:        $([ "$RUN_NOMINATION_TEST" = true ] && ([ $NOMINATION_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo "  Season Test:            $([ "$RUN_SEASON_TEST" = true ] && ([ $SEASON_EXIT_CODE -eq 0 ] && echo "Passed" || echo "Issues") || echo "Skipped")"
echo ""
echo "========================================================================="
echo "TEST SUITE EXECUTION COMPLETED"
echo "========================================================================="

# ========================================================================
# Aggregate exit codes so the parent test/run_all_tests.sh records FAILED
# when any sub-test reports issues. Without this, the master suite saw
# `>>> PASSED: x/season` even though inner assertions failed (e.g. Bob
# XP mismatch, display-name moderation stake cleanup), masking real bugs.
#
# A test "counts" toward failure only if it was actually selected to run
# (RUN_*=true). Skipped tests stay neutral. We default each EXIT_CODE to
# 1 for selected-but-not-yet-set so a missing assignment also fails loudly.
# ========================================================================
SUITE_FAILED=0
check_test() {
    local enabled=$1
    local code=$2
    local label=$3
    if [ "$enabled" = true ] && [ "${code:-1}" -ne 0 ]; then
        echo "  [FAIL] $label exited with code ${code:-1}"
        SUITE_FAILED=1
    fi
}

# Setup is special: $? lives in $SETUP_EXIT_CODE if tracked, otherwise we
# infer from RUN_SETUP + the absence of an early `exit 1`.
check_test "$RUN_SETUP"               "${SETUP_EXIT_CODE:-0}"               "Setup"
check_test "$RUN_PROFILE_TEST"        "$PROFILE_EXIT_CODE"                  "Profile Test"
check_test "$RUN_GUILD_TEST"          "$GUILD_EXIT_CODE"                    "Guild Test"
check_test "$RUN_GUILD_ADVANCED_TEST" "$GUILD_ADVANCED_EXIT_CODE"           "Guild Advanced Test"
check_test "$RUN_GUILD_ERRORS_TEST"   "${GUILD_ERRORS_EXIT_CODE:-1}"        "Guild Errors Test"
check_test "$RUN_QUEST_TEST"          "$QUEST_EXIT_CODE"                    "Quest Test"
check_test "$RUN_QUEST_ERRORS_TEST"   "${QUEST_ERRORS_EXIT_CODE:-1}"        "Quest Errors Test"
check_test "$RUN_MODERATION_TEST"     "$MODERATION_EXIT_CODE"               "Moderation Test"
check_test "$RUN_XP_TRACKING_TEST"    "$XP_TRACKING_EXIT_CODE"              "XP Tracking Test"
check_test "$RUN_OPERATIONAL_PARAMS_TEST" "${OPERATIONAL_PARAMS_EXIT_CODE:-1}" "Op Params Test"
check_test "$RUN_NOMINATION_TEST"     "$NOMINATION_EXIT_CODE"               "Nomination Test"
check_test "$RUN_SEASON_TEST"         "$SEASON_EXIT_CODE"                   "Season Test"
# VALIDATION_EXIT_CODE is set under STEP 10 if the validation step ran;
# guard it the same way as the optional-but-tracked tests above.
check_test "${RUN_VALIDATION_TEST:-true}" "${VALIDATION_EXIT_CODE:-0}"      "Validation Test"

if [ $SUITE_FAILED -ne 0 ]; then
    echo ""
    echo "RESULT: x/season suite has failures — exiting non-zero."
    exit 1
fi
