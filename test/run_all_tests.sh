#!/bin/bash

# ============================================================================
# SPARK DREAM - MASTER E2E TEST SUITE
# ============================================================================
# Runs all integration tests across all modules.
#
# This script:
#   1. Stops any running chain
#   2. Builds the binary (optional)
#   3. Initializes a fresh chain
#   4. Starts the chain
#   5. Verifies test params are active (not production)
#   6. Runs legacy module tests (ecosystem, split, gov)
#   7. Runs per-module tests (commons, name, futarchy, gnovm, rep, blog,
#      forum, collect, shield, reveal, federation, season) — each with its
#      own run_all_tests.sh
#   8. Runs destructive commons tests last (tech upgrade, fire council)
#   9. Reports results
#
# Usage:
#   ./run_all_tests.sh                # Full suite: build + all tests
#   ./run_all_tests.sh --no-build     # Skip build (use existing binary)
#   ./run_all_tests.sh --no-legacy    # Skip legacy tests (ecosystem, split, gov)
#   ./run_all_tests.sh --no-modules   # Skip per-module tests
#   ./run_all_tests.sh --only rep     # Only run one module
#   ./run_all_tests.sh --help         # Show usage
# ============================================================================

set -e

# ============================================================================
# Configuration
# ============================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$( cd "$SCRIPT_DIR/.." && pwd )"
BINARY="sparkdreamd"
CHAIN_HOME="$HOME/.sparkdream"
LOG_FILE="/tmp/sparkdreamd-e2e.log"

# Shared timing helpers (timing_now_epoch, timing_format_duration, ...).
source "$SCRIPT_DIR/_timing.sh"
# Shared safe-rmtree helper (CLAUDE.md forbids `rm -rf`).
source "$SCRIPT_DIR/_safe_rm.sh"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Flags
RUN_BUILD=true
RUN_LEGACY=true
RUN_MODULES=true
RUN_DESTRUCTIVE=true
ONLY_MODULE=""

# Modules in DESCENDING WALL-CLOCK ORDER (longest suite first).
#
# This list is consumed in two places:
#  1. The sequential master runner (this file): order is purely cosmetic
#     — the chain is reinitialized between modules, so there are no
#     state dependencies across modules.
#  2. test/run_parallel.sh: the order IS load-bearing. Suites are split
#     into batches of `--concurrency` size in this order, and each batch's
#     wall time is its slowest member. Putting the longest suites first
#     keeps the slow tail concentrated in the first batch and lets later
#     batches finish quickly.
#
# Empirical durations (May 2026, snapshot-restored chains, 12-suite run):
#   forum 39m  rep 25m  federation 24m  season 20m  commons 18m  collect 15m
#   blog 12m   shield 10m  futarchy 9m  name 6m  reveal 5m  gnovm 1m
#
# With concurrency=6 and this order, batch 1 = forum/rep/federation/season/
# commons/collect (bottleneck forum 39m), batch 2 = blog/shield/futarchy/
# name/reveal/gnovm (bottleneck blog 12m) → projected wall time ~51m vs
# the 88m alphabetical/concurrency-4 baseline.
#
# Re-measure and reorder when adding a new module or after major changes
# to a slow suite. The simplest measurement source is the per-module log
# mtimes in e2e/latest/ after a full parallel run.
MODULE_ORDER="forum rep federation season commons collect blog shield futarchy name reveal gnovm"

# Track results
PASSED_TESTS=()
FAILED_TESTS=()
SKIPPED_TESTS=()

# Per-test timing (parallel arrays, indexed together).
TIMED_NAMES=()        # display name (script path or "x/<module>")
TIMED_RESULTS=()      # PASS / FAIL / SKIP
TIMED_DURATIONS_S=()  # integer seconds

START_TIME=$(timing_now_epoch)
START_TIME_HUMAN=$(timing_now_human)

# ============================================================================
# Parse Arguments
# ============================================================================
while [[ $# -gt 0 ]]; do
    case $1 in
        --no-build)
            RUN_BUILD=false
            shift
            ;;
        --no-legacy)
            RUN_LEGACY=false
            shift
            ;;
        --no-modules)
            RUN_MODULES=false
            shift
            ;;
        --no-destructive)
            RUN_DESTRUCTIVE=false
            shift
            ;;
        --only)
            ONLY_MODULE="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-build       Skip building the binary (use existing)"
            echo "  --no-legacy      Skip legacy tests (ecosystem, split, gov)"
            echo "  --no-modules     Skip per-module tests (commons, name, futarchy, rep, ...)"
            echo "  --no-destructive Skip the final destructive commons phase"
            echo "                   (tech upgrade + fire council)"
            echo "  --only <module>  Run only one module (commons, name, futarchy, rep, blog, ...)"
            echo "  --help           Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0                          # Full suite (incl. destructive)"
            echo "  $0 --no-build               # Skip rebuild"
            echo "  $0 --no-legacy              # Skip legacy tests; modules + destructive still run"
            echo "  $0 --no-destructive         # Skip the destructive commons phase"
            echo "  $0 --no-legacy --no-destructive  # Per-module tests only"
            echo "  $0 --only blog              # Only blog module"
            echo "  $0 --only commons           # Only commons module (non-destructive phases)"
            echo "  $0 --no-modules             # Only legacy tests"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# ============================================================================
# Helper Functions
# ============================================================================

# Run a single legacy test script (relative to test/)
run_test() {
    local script="$1"
    local full_path="$SCRIPT_DIR/$script"

    echo -e "\n${BLUE}======================================================${NC}"
    echo -e "${BLUE}>>> RUNNING: $script${NC}"
    echo -e "${BLUE}======================================================${NC}"

    if [ ! -f "$full_path" ]; then
        echo -e "${RED}>>> ERROR: File not found: $full_path${NC}"
        FAILED_TESTS+=("$script (not found)")
        TIMED_NAMES+=("$script")
        TIMED_RESULTS+=("FAIL")
        TIMED_DURATIONS_S+=(0)
        return 1
    fi

    chmod +x "$full_path"

    local _t0 _t1 _dur_s _dur
    _t0=$(timing_now_epoch)
    if (cd "$SCRIPT_DIR" && ./"$script"); then
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        echo -e "${GREEN}>>> PASSED: $script ($_dur)${NC}"
        PASSED_TESTS+=("$script ($_dur)")
        TIMED_NAMES+=("$script")
        TIMED_RESULTS+=("PASS")
        TIMED_DURATIONS_S+=("$_dur_s")
    else
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        echo -e "${RED}>>> FAILED: $script ($_dur)${NC}"
        FAILED_TESTS+=("$script ($_dur)")
        TIMED_NAMES+=("$script")
        TIMED_RESULTS+=("FAIL")
        TIMED_DURATIONS_S+=("$_dur_s")
    fi

    sleep 2
}

# Run a module's full test suite via its run_all_tests.sh.
# Reinitializes the chain before each module to ensure test isolation.
# Any extra args after the module name are forwarded to run_all_tests.sh
# (e.g. `run_module commons --only-destructive`). When extra args are
# present, a label suffix in parentheses is appended to result entries so
# multiple passes of the same module don't collide in the summary.
run_module() {
    local module="$1"
    shift
    local extra_args=("$@")
    local module_dir="$SCRIPT_DIR/$module"
    local runner="$module_dir/run_all_tests.sh"
    local label="x/$module"
    if [ ${#extra_args[@]} -gt 0 ]; then
        label="x/$module (${extra_args[*]})"
    fi

    echo -e "\n${BLUE}===========================================================${NC}"
    echo -e "${BLUE}>>> MODULE: $label${NC}"
    echo -e "${BLUE}===========================================================${NC}"

    if [ ! -f "$runner" ]; then
        echo -e "${YELLOW}>>> SKIP: $module/run_all_tests.sh not found${NC}"
        SKIPPED_TESTS+=("$label")
        TIMED_NAMES+=("$label")
        TIMED_RESULTS+=("SKIP")
        TIMED_DURATIONS_S+=(0)
        return 0
    fi

    # Reinitialize chain for module isolation
    echo -e "${BLUE}>>> Reinitializing chain for $label...${NC}"
    stop_chain
    safe_rmtree "$CHAIN_HOME" || {
        echo -e "${RED}>>> FAILED: failed to remove $CHAIN_HOME for $label${NC}"
        FAILED_TESTS+=("$label (chain home cleanup)")
        return 1
    }
    (cd "$PROJECT_DIR" && ignite chain init -y --build.tags testparams) || {
        echo -e "${RED}>>> FAILED: chain init for $label${NC}"
        FAILED_TESTS+=("$label (chain init)")
        return 1
    }
    # Append to LOG_FILE rather than truncating: per-module crash logs from
    # earlier modules stay readable in the post-mortem instead of being
    # overwritten by the next module's startup output.
    $BINARY start --home "$CHAIN_HOME" >> "$LOG_FILE" 2>&1 &
    CHAIN_PID=$!
    wait_for_chain || {
        echo -e "${RED}>>> FAILED: chain start for $label${NC}"
        FAILED_TESTS+=("$label (chain start)")
        return 1
    }

    chmod +x "$runner"

    # Time only the actual run_all_tests.sh execution — not the chain init
    # above, which is overhead that would muddy module-level timing.
    local _t0 _t1 _dur_s _dur
    _t0=$(timing_now_epoch)
    if (cd "$module_dir" && bash run_all_tests.sh "${extra_args[@]}"); then
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        echo -e "${GREEN}>>> PASSED: $label ($_dur)${NC}"
        PASSED_TESTS+=("$label ($_dur)")
        TIMED_NAMES+=("$label")
        TIMED_RESULTS+=("PASS")
        TIMED_DURATIONS_S+=("$_dur_s")
    else
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        echo -e "${RED}>>> FAILED: $label ($_dur)${NC}"
        FAILED_TESTS+=("$label ($_dur)")
        TIMED_NAMES+=("$label")
        TIMED_RESULTS+=("FAIL")
        TIMED_DURATIONS_S+=("$_dur_s")
    fi

    sleep 2
}

stop_chain() {
    echo "Stopping any running chain..."
    # Scope kills to processes bound to THIS runner's CHAIN_HOME so a
    # concurrent test/run_parallel.sh run (whose chains live under
    # e2e/parallel-*/...) is not collateral damage. The default-home path
    # also kills bare `sparkdreamd start` invocations that lack --home so
    # legacy snapshots / orphan processes still get cleaned up.
    pkill -f "sparkdreamd .*--home $CHAIN_HOME" 2>/dev/null || true
    pkill -f "sparkdreamd start.* --home $CHAIN_HOME" 2>/dev/null || true
    if [ "$CHAIN_HOME" = "$HOME/.sparkdream" ]; then
        # Default-home: also clean up any orphaned sparkdreamd that lacks an
        # explicit --home arg (defaults to ~/.sparkdream so it'd collide with
        # what we're about to start).
        pkill -f "sparkdreamd start" 2>/dev/null || true
    fi
    sleep 3
}

wait_for_chain() {
    echo "Waiting for chain to produce blocks..."
    local retries=60
    for i in $(seq 1 $retries); do
        if $BINARY status 2>&1 | jq -e '.sync_info.latest_block_height | tonumber > 1' &>/dev/null; then
            local height=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
            echo -e "${GREEN}Chain is running (block height: $height)${NC}"
            return 0
        fi
        sleep 1
    done
    echo -e "${RED}Chain failed to start within ${retries}s${NC}"
    echo "Last 20 lines of log:"
    tail -20 "$LOG_FILE" 2>/dev/null || true
    return 1
}

# ============================================================================
# Verify Test Parameters
# ============================================================================
# Checks that the binary was built with test params, not production params.
# Test params have dramatically different values (1s vs 72h, 10 rep vs 50 rep)
# so false positives are impossible.
verify_test_params() {
    echo -e "\n${BLUE}=== VERIFYING TEST PARAMETERS ===${NC}"

    local failed=false

    # Check 1: x/rep params — provisional_min_rep should be 10 (test) not 50 (production)
    echo -n "Checking x/rep trust level config... "
    local rep_params
    rep_params=$($BINARY query rep params --output json 2>/dev/null) || {
        echo -e "${YELLOW}SKIP (x/rep query failed)${NC}"
        return 0
    }

    local provisional_min_rep
    provisional_min_rep=$(echo "$rep_params" | jq -r '.params.trust_level_config.provisional_min_rep // empty' 2>/dev/null)

    if [ -z "$provisional_min_rep" ]; then
        echo -e "${YELLOW}SKIP (field not found)${NC}"
    else
        # LegacyDec values may come back as integer representation (e.g. "10000000000000000000" for 10)
        # or as plain "10" depending on query format. Check both.
        if [ "$provisional_min_rep" = "10" ] || [ "$provisional_min_rep" = "10.000000000000000000" ] || [ "$provisional_min_rep" = "10000000000000000000" ]; then
            echo -e "${GREEN}OK (provisional_min_rep = test value)${NC}"
        elif [ "$provisional_min_rep" = "50" ] || [ "$provisional_min_rep" = "50.000000000000000000" ] || [ "$provisional_min_rep" = "50000000000000000000" ]; then
            echo -e "${RED}FAIL${NC}"
            echo -e "${RED}  provisional_min_rep = $provisional_min_rep (PRODUCTION value)${NC}"
            echo -e "${RED}  Expected test value (10). Check x/rep/types/params_vals.go${NC}"
            echo -e "${RED}  Ensure the TESTING VALUES section is uncommented and PRODUCTION VALUES is commented out.${NC}"
            failed=true
        else
            echo -e "${YELLOW}WARN (unexpected value: $provisional_min_rep)${NC}"
        fi
    fi

    # Check 2: Commons Council DecisionPolicy.min_execution_period — should be
    # 1s (testparams) not 259200s (production, 72h). Two-step query: first
    # find the council's policy_address via get-group, then read the
    # DecisionPolicy via the get-decision-policy RPC. Both are cheap reads
    # against bootstrapped genesis state, so this works on any chain that's
    # past block height 1.
    echo -n "Checking x/commons policy execution period... "
    local commons_policy
    commons_policy=$($BINARY query commons get-group "Commons Council" --output json 2>/dev/null \
        | jq -r '.group.policy_address // empty' 2>/dev/null)

    if [ -z "$commons_policy" ]; then
        echo -e "${YELLOW}SKIP (Commons Council not bootstrapped)${NC}"
    else
        local min_exec
        min_exec=$($BINARY query commons get-decision-policy "$commons_policy" --output json 2>/dev/null \
            | jq -r '.decision_policy.min_execution_period // empty' 2>/dev/null)

        if [ -z "$min_exec" ]; then
            echo -e "${YELLOW}SKIP (decision-policy query returned nothing)${NC}"
        elif [ "$min_exec" -le 60 ] 2>/dev/null; then
            echo -e "${GREEN}OK (min_execution_period = ${min_exec}s, test value)${NC}"
        else
            echo -e "${RED}FAIL${NC}"
            echo -e "${RED}  min_execution_period = ${min_exec}s (PRODUCTION value)${NC}"
            echo -e "${RED}  Expected ≤60s. Rebuild with --build.tags testparams.${NC}"
            failed=true
        fi
    fi

    if [ "$failed" = true ]; then
        echo ""
        echo -e "${RED}TEST PARAM VERIFICATION FAILED${NC}"
        echo -e "${RED}The binary appears to be built with production parameters.${NC}"
        echo -e "${RED}E2E tests require test parameters (fast timeouts, low thresholds).${NC}"
        echo ""
        echo "Rebuild with: ignite chain build -y --build.tags testparams"
        echo ""
        echo "The build tag selects:"
        echo "  x/rep/types/params_vals_testparams.go      — trust level thresholds"
        echo "  x/commons/keeper/genesis_vals_testparams.go — governance timeouts"
        return 1
    fi

    echo -e "${GREEN}Test parameters verified.${NC}"
}

# ============================================================================
# Print Summary
# ============================================================================
print_summary() {
    local end_time
    end_time=$(timing_now_epoch)
    local end_time_human
    end_time_human=$(timing_now_human)

    echo ""
    echo -e "${BLUE}===========================================================${NC}"
    echo -e "${BLUE}  TEST SUITE SUMMARY${NC}"
    echo -e "${BLUE}===========================================================${NC}"
    echo ""
    timing_print_summary_block "$START_TIME" "$end_time" \
        "$START_TIME_HUMAN" "$end_time_human"
    echo ""

    # Per-test timings table (sorted in execution order — easier to spot
    # which step in the pipeline got slow).
    if [ ${#TIMED_NAMES[@]} -gt 0 ]; then
        timing_print_per_test_table TIMED_RESULTS TIMED_DURATIONS_S TIMED_NAMES
        echo ""
    fi

    if [ ${#PASSED_TESTS[@]} -gt 0 ]; then
        echo -e "${GREEN}PASSED (${#PASSED_TESTS[@]}):${NC}"
        for t in "${PASSED_TESTS[@]}"; do
            echo -e "  ${GREEN}  $t${NC}"
        done
        echo ""
    fi

    if [ ${#SKIPPED_TESTS[@]} -gt 0 ]; then
        echo -e "${YELLOW}SKIPPED (${#SKIPPED_TESTS[@]}):${NC}"
        for t in "${SKIPPED_TESTS[@]}"; do
            echo -e "  ${YELLOW}  $t${NC}"
        done
        echo ""
    fi

    if [ ${#FAILED_TESTS[@]} -gt 0 ]; then
        echo -e "${RED}FAILED (${#FAILED_TESTS[@]}):${NC}"
        for t in "${FAILED_TESTS[@]}"; do
            echo -e "  ${RED}  $t${NC}"
        done
        echo ""
        echo -e "${RED}SOME TESTS FAILED${NC}"
        return 1
    fi

    echo -e "${GREEN}ALL TESTS PASSED${NC}"
}

# ============================================================================
# MAIN
# ============================================================================

echo -e "${GREEN}===========================================================${NC}"
echo -e "${GREEN}  SPARK DREAM - MASTER E2E TEST SUITE${NC}"
echo -e "${GREEN}===========================================================${NC}"
echo ""
echo "Started at:    $START_TIME_HUMAN"
echo ""
echo "Configuration:"
echo "  Build:        $([ "$RUN_BUILD" = true ] && echo "yes" || echo "skip")"
echo "  Legacy:       $([ "$RUN_LEGACY" = true ] && echo "yes" || echo "skip")"
echo "  Modules:      $([ "$RUN_MODULES" = true ] && echo "yes" || echo "skip")"
echo "  Destructive:  $([ "$RUN_DESTRUCTIVE" = true ] && echo "yes" || echo "skip")"
if [ -n "$ONLY_MODULE" ]; then
    echo "  Only:         $ONLY_MODULE"
fi
echo ""

# --- Step 1: Stop any running chain ---
echo -e "${BLUE}=== STEP 1: STOP CHAIN ===${NC}"
stop_chain

# --- Step 2: Build binary ---
if [ "$RUN_BUILD" = true ]; then
    echo -e "\n${BLUE}=== STEP 2: BUILD CHAIN ===${NC}"

    # Clean stale binaries
    echo "Cleaning stale binaries..."
    rm -f "$PROJECT_DIR/sparkdreamd" "$PROJECT_DIR/build/sparkdreamd" /tmp/sparkdreamd
    rm -f "$HOME/.ignite/local-chains/sparkdream/exported_genesis.json"

    echo "Building..."
    (cd "$PROJECT_DIR" && ignite chain build -y --build.tags testparams)
    echo -e "${GREEN}Build complete.${NC}"

    # Verify binary is at expected location
    if ! command -v $BINARY &>/dev/null; then
        echo -e "${RED}Binary not found in PATH after build.${NC}"
        echo "Expected at: $(go env GOPATH)/bin/$BINARY"
        exit 1
    fi
else
    echo -e "\n${BLUE}=== STEP 2: BUILD (skipped) ===${NC}"
    if ! command -v $BINARY &>/dev/null; then
        echo -e "${RED}Binary not found. Run without --no-build or build manually.${NC}"
        exit 1
    fi
fi

# --- Step 3: Init fresh chain ---
echo -e "\n${BLUE}=== STEP 3: INIT CHAIN ===${NC}"
safe_rmtree "$CHAIN_HOME" || {
    echo -e "${RED}Failed to clear $CHAIN_HOME. Aborting.${NC}"
    exit 1
}
(cd "$PROJECT_DIR" && ignite chain init -y --build.tags testparams)
echo -e "${GREEN}Chain initialized with testparams.${NC}"

# --- Step 4: Start chain ---
echo -e "\n${BLUE}=== STEP 4: START CHAIN ===${NC}"
# Truncate the log on the FIRST chain start of the run only — subsequent
# starts (run_module reinits) append so we keep the full multi-module
# timeline in one file for post-mortem.
: > "$LOG_FILE"
$BINARY start --home "$CHAIN_HOME" >> "$LOG_FILE" 2>&1 &
CHAIN_PID=$!
echo "Chain PID: $CHAIN_PID (log: $LOG_FILE)"

wait_for_chain || {
    echo -e "${RED}Failed to start chain. Aborting.${NC}"
    exit 1
}

# --- Step 5: Verify test params ---
echo -e "\n${BLUE}=== STEP 5: VERIFY TEST PARAMS ===${NC}"
verify_test_params || {
    stop_chain
    exit 1
}

# --- Step 6: Legacy tests ---
# commons, name, and futarchy used to live here. They now have their own
# per-module run_all_tests.sh (see Step 7). Only ecosystem, split, and gov
# remain as direct legacy invocations.
if [ "$RUN_LEGACY" = true ]; then
    echo -e "\n${BLUE}=== LEGACY: ECOSYSTEM ===${NC}"
    run_test "ecosystem/ecosystem_spend.sh"

    echo -e "\n${BLUE}=== LEGACY: SPLIT ===${NC}"
    run_test "split/accounts.sh"
    run_test "split/autodivert.sh"

    echo -e "\n${BLUE}=== LEGACY: SECURITY ===${NC}"
    run_test "ecosystem/ecosystem_security_test.sh"
    run_test "gov/inflation_immutable_test.sh"
fi

# --- Step 7: Newer module tests ---
if [ "$RUN_MODULES" = true ]; then
    echo -e "\n${BLUE}=== NEWER MODULE TESTS ===${NC}"

    # Federation has an optional multichain IBC suite that requires Hermes.
    # Pass --multichain to its runner only when Hermes is on PATH; otherwise
    # the multichain phase would hard-fail the federation module on hosts
    # without Hermes installed (CI workers, contributors who haven't set it
    # up yet). Single-chain federation tests still run either way.
    federation_extra_args=()
    if command -v hermes &> /dev/null; then
        federation_extra_args+=(--multichain)
        echo -e "${BLUE}>>> Hermes detected ($(command -v hermes)) — federation will include multichain tests${NC}"
    else
        echo -e "${YELLOW}>>> Hermes not on PATH — skipping federation multichain tests${NC}"
        echo -e "${YELLOW}    Install: see test/federation/multichain/README.md${NC}"
    fi

    if [ -n "$ONLY_MODULE" ]; then
        # Run only the specified module
        if [ "$ONLY_MODULE" = "federation" ]; then
            run_module "$ONLY_MODULE" "${federation_extra_args[@]}"
        else
            run_module "$ONLY_MODULE"
        fi
    else
        # Run all modules in dependency order
        for module in $MODULE_ORDER; do
            if [ "$module" = "federation" ]; then
                run_module "$module" "${federation_extra_args[@]}"
            else
                run_module "$module"
            fi
        done
    fi
fi

# --- Step 8: Destructive commons tests (LAST) ---
# These tests permanently rewrite council membership (tech council upgrade,
# firing the commons council). They must run after all other modules have
# finished so they can't pollute earlier tests. The commons module's own
# run_all_tests.sh skips them by default; we invoke it with
# --only-destructive here, getting a fresh chain init via run_module.
# Gated by its own --no-destructive flag (independent of --no-legacy).
if [ "$RUN_DESTRUCTIVE" = true ]; then
    echo -e "\n${BLUE}=== FINAL: COMMONS DESTRUCTIVE TESTS ===${NC}"
    run_module commons --only-destructive
else
    echo -e "\n${YELLOW}=== FINAL: COMMONS DESTRUCTIVE TESTS (skipped) ===${NC}"
fi

# --- Step 9: Stop chain & report ---
echo -e "\n${BLUE}=== CLEANUP ===${NC}"
stop_chain

print_summary
