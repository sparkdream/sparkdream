#!/bin/bash

# ============================================================================
# X/BLOG MODULE E2E TEST SUITE
# ============================================================================
# This script runs all blog module e2e tests in sequence.
#
# Usage:
#   ./run_all_tests.sh              # Run all tests
#   ./run_all_tests.sh --no-setup   # Skip account setup
#   ./run_all_tests.sh --no-post    # Skip post tests
#   ./run_all_tests.sh --no-reply   # Skip reply tests
#   ./run_all_tests.sh --no-reaction # Skip reaction tests
#
# Prerequisites:
#   - sparkdreamd chain running locally
#   - Alice account with SPARK and DREAM
#   - x/rep module functional (for membership)
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../check_testparams.sh"
source "$SCRIPT_DIR/../_timing.sh"
BINARY="sparkdreamd"
source "$SCRIPT_DIR/../lib/denoms.sh"

# Wall-clock timing for the suite — captured here so the summary's
# "Started" line reflects when the runner was invoked, not when the
# first test fired.
SUITE_START_EPOCH=$(timing_now_epoch)
SUITE_START_HUMAN=$(timing_now_human)

# Parse command line arguments
RUN_SETUP=true
RUN_POST=true
RUN_REPLY=true
RUN_REACTION=true
RUN_PIN=true
RUN_MAKE_PERMANENT=true
RUN_PROMOTION_QUEUE=true
RUN_ANON=true
RUN_TAG=true
# Master gate for the entire test-execution loop. The per-test RUN_* flags
# above only cover the gated `run_test` blocks; three later invocations
# (content_status_test, expiry_test, rate_limit_test) sit at column 0 with
# no per-test gate, so without this master flag `--restore-setup --no-tests`
# would still run them and drift the freshly-restored chain state before
# the user could run a specific test manually.
RUN_TESTS=true
SAVE_SETUP=false
RESTORE_SETUP=false

AUTO_SNAPSHOT=true
for arg in "$@"; do
    case $arg in
        --no-setup)
            RUN_SETUP=false
            ;;
        --no-post)
            RUN_POST=false
            ;;
        --no-reply)
            RUN_REPLY=false
            ;;
        --no-reaction)
            RUN_REACTION=false
            ;;
        --no-pin)
            RUN_PIN=false
            ;;
        --no-make-permanent)
            RUN_MAKE_PERMANENT=false
            ;;
        --no-promotion-queue)
            RUN_PROMOTION_QUEUE=false
            ;;
        --no-anon)
            RUN_ANON=false
            ;;
        --no-tag)
            RUN_TAG=false
            ;;
        --only-setup)
            RUN_POST=false
            RUN_REPLY=false
            RUN_REACTION=false
            RUN_PIN=false
            RUN_MAKE_PERMANENT=false
            RUN_PROMOTION_QUEUE=false
            RUN_ANON=false
            RUN_TAG=false
            ;;
        --save-setup)
            SAVE_SETUP=true
            RUN_SETUP=true
            RUN_POST=false
            RUN_REPLY=false
            RUN_REACTION=false
            RUN_PIN=false
            RUN_MAKE_PERMANENT=false
            RUN_PROMOTION_QUEUE=false
            RUN_ANON=false
            RUN_TAG=false
            ;;
        --restore-setup)
            RESTORE_SETUP=true
            RUN_SETUP=false
            ;;
        --no-tests)
            RUN_TESTS=false
            RUN_POST=false
            RUN_REPLY=false
            RUN_REACTION=false
            RUN_PIN=false
            RUN_MAKE_PERMANENT=false
            RUN_PROMOTION_QUEUE=false
            RUN_ANON=false
            RUN_TAG=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-setup       Skip account setup"
            echo "  --no-post        Skip post tests"
            echo "  --no-reply       Skip reply tests"
            echo "  --no-reaction    Skip reaction tests"
            echo "  --no-pin         Skip pin post/reply tests"
            echo "  --no-make-permanent     Skip MakePostPermanent/MakeReplyPermanent tests"
            echo "  --no-promotion-queue    Skip membership-driven promotion-queue tests"
            echo "  --no-anon        Skip anonymous action tests (via x/shield)"
            echo "  --no-tag         Skip tag validation and list-by-tag tests"
            echo "  --only-setup     Run only setup (skip all tests)"
            echo "  --save-setup     Run setup, save chain state, then exit"
            echo "  --restore-setup  Restore saved setup state, then run tests"
            echo "  --no-auto-snapshot Disable auto-snapshot (run setup every time, no caching)"
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
        --no-auto-snapshot)
            AUTO_SNAPSHOT=false
            ;;
        *)
            echo "Unknown option: $arg"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done


# Auto-snapshot: when no explicit save/restore flag is passed, reuse an
# existing fresh snapshot or save one after setup. See test/_auto_snapshot.sh.
source "$SCRIPT_DIR/../_auto_snapshot.sh"
auto_snapshot_pre
echo "============================================================================"
echo "                     X/BLOG MODULE E2E TEST SUITE"
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
    ALICE_BALANCE=$($BINARY query bank balances $ALICE_ADDR --output json 2>/dev/null | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount' || echo "0")
    if [ "$ALICE_BALANCE" -lt 1000000 ]; then
        echo "WARNING: Alice has low SPARK balance: $ALICE_BALANCE $BOND_DENOM"
    fi
    echo "  Balance: $ALICE_BALANCE $BOND_DENOM"
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

    # `--restore-setup --no-tests`: the user wants a fresh post-setup chain
    # to run a specific test against — don't fall through into the test
    # loop, which would drift the state via the un-gated tests further
    # down (content_status, expiry, rate_limit).
    if [ "$RUN_TESTS" != true ]; then
        BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
        echo "============================================================================"
        echo "  RESTORE COMPLETE — TEST EXECUTION SKIPPED (--no-tests)"
        echo "============================================================================"
        echo ""
        echo "  Chain home:   $HOME/.sparkdream"
        echo "  Block height: $BLOCK_HEIGHT"
        echo "  Test env:     $SCRIPT_DIR/.test_env  (already sourced)"
        echo "  Chain log:    /tmp/chain_after_restore.log"
        echo ""
        echo "  Run any specific test against the freshly restored state:"
        echo "    bash $SCRIPT_DIR/post_test.sh"
        echo "    bash $SCRIPT_DIR/<other_test>.sh"
        echo ""
        echo "  Stop the chain when done:"
        echo "    pkill -f 'sparkdreamd start --home $HOME/.sparkdream'"
        echo ""
        exit 0
    fi
fi

# ============================================================================
# Test Results Tracking
# ============================================================================
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
declare -a FAILED_TESTS

# Per-test timing — populated by run_test, consumed by the summary block.
declare -a TIMED_NAMES=()
declare -a TIMED_RESULTS=()
declare -a TIMED_DURATIONS_S=()

run_test() {
    local TEST_NAME=$1
    local TEST_SCRIPT=$2

    echo "============================================================================"
    echo "RUNNING: $TEST_NAME"
    echo "============================================================================"
    echo ""

    TESTS_RUN=$((TESTS_RUN + 1))

    local _t0 _t1 _dur_s _dur
    _t0=$(timing_now_epoch)
    if bash "$SCRIPT_DIR/$TEST_SCRIPT"; then
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        TESTS_PASSED=$((TESTS_PASSED + 1))
        TIMED_NAMES+=("$TEST_NAME")
        TIMED_RESULTS+=("PASS")
        TIMED_DURATIONS_S+=("$_dur_s")
        echo ""
        echo ">>> $TEST_NAME: PASSED ($_dur) <<<"
    else
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        TESTS_FAILED=$((TESTS_FAILED + 1))
        FAILED_TESTS+=("$TEST_NAME")
        TIMED_NAMES+=("$TEST_NAME")
        TIMED_RESULTS+=("FAIL")
        TIMED_DURATIONS_S+=("$_dur_s")
        echo ""
        echo ">>> $TEST_NAME: FAILED ($_dur) <<<"
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

    # Auto-save the post-setup snapshot if AUTO_SNAPSHOT was set and
    # no fresh snapshot existed at the start of this run.
    auto_snapshot_post

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
        echo "  bash test/blog/run_all_tests.sh --restore-setup"
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

# Reply tests
if [ "$RUN_REPLY" = true ]; then
    run_test "Reply Tests" "reply_test.sh"
else
    echo "Skipping reply tests (--no-reply)"
    echo ""
fi

# Reaction tests
if [ "$RUN_REACTION" = true ]; then
    run_test "Reaction Tests" "reaction_test.sh"
else
    echo "Skipping reaction tests (--no-reaction)"
    echo ""
fi

# Pin post/reply tests
if [ "$RUN_PIN" = true ]; then
    run_test "Pin Post/Reply Tests" "pin_test.sh"
else
    echo "Skipping pin tests (--no-pin)"
    echo ""
fi

# MakePostPermanent / MakeReplyPermanent tests (separated from Pin in the
# strict-separation rework — preservation lifecycle, not display marker).
if [ "$RUN_MAKE_PERMANENT" = true ]; then
    run_test "MakePostPermanent / MakeReplyPermanent Tests" "make_permanent_test.sh"
else
    echo "Skipping make-permanent tests (--no-make-permanent)"
    echo ""
fi

# Membership-driven promotion-queue tests (AfterMemberAdmitted hook + EndBlocker drain).
if [ "$RUN_PROMOTION_QUEUE" = true ]; then
    run_test "Promotion Queue Tests" "promotion_queue_test.sh"
else
    echo "Skipping promotion-queue tests (--no-promotion-queue)"
    echo ""
fi

# Anonymous action tests (via x/shield)
if [ "$RUN_ANON" = true ]; then
    run_test "Anonymous Action Tests" "anon_test.sh"
else
    echo "Skipping anonymous action tests (--no-anon)"
    echo ""
fi

# Tag validation and list-by-tag tests
if [ "$RUN_TAG" = true ]; then
    run_test "Tag Tests" "tag_test.sh"
else
    echo "Skipping tag tests (--no-tag)"
    echo ""
fi

# Always-on tests below have no per-test --no-X flag. Wrap in the master
# RUN_TESTS guard so `--no-tests` skips them too — the early exit above
# already handles the `--restore-setup --no-tests` case, but this defends
# against `--no-tests` without `--restore-setup`.
if [ "$RUN_TESTS" = true ]; then
    # Content status gates tests (P2)
    run_test "Content Status Gates Tests" "content_status_test.sh"

    # Content expiry tests (P3)
    run_test "Content Expiry Tests" "expiry_test.sh"

    # Rate limit tests (P1)
    run_test "Rate Limit Tests" "rate_limit_test.sh"
fi

# ============================================================================
# Final Summary
# ============================================================================
echo "============================================================================"
echo "                         TEST SUITE SUMMARY"
echo "============================================================================"
echo ""

# Wall-clock summary (Started/Ended/Duration), captured by the helper.
SUITE_END_EPOCH=$(timing_now_epoch)
SUITE_END_HUMAN=$(timing_now_human)
timing_print_summary_block "$SUITE_START_EPOCH" "$SUITE_END_EPOCH" \
    "$SUITE_START_HUMAN" "$SUITE_END_HUMAN"
echo ""

echo "  Tests Run:    $TESTS_RUN"
echo "  Tests Passed: $TESTS_PASSED"
echo "  Tests Failed: $TESTS_FAILED"
echo ""

# Per-test timings table (in execution order).
if [ ${#TIMED_NAMES[@]} -gt 0 ]; then
    timing_print_per_test_table TIMED_RESULTS TIMED_DURATIONS_S TIMED_NAMES
    echo ""
fi

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
