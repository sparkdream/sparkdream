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
#   6. Runs legacy module tests (commons, name, ecosystem, split, futarchy, gov)
#   7. Runs newer module tests (rep, blog, forum, collect, vote, reveal, season)
#   8. Runs destructive tests last (tech upgrade, fire council)
#   9. Reports results
#
# Usage:
#   ./run_all_tests.sh                # Full suite: build + all tests
#   ./run_all_tests.sh --no-build     # Skip build (use existing binary)
#   ./run_all_tests.sh --no-legacy    # Skip legacy tests (commons, name, etc.)
#   ./run_all_tests.sh --no-modules   # Skip newer module tests
#   ./run_all_tests.sh --only rep     # Only run one newer module
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
ONLY_MODULE=""

# Newer modules in dependency order:
#   rep first (foundation), then content modules, reveal (needs commons council),
#   season last (transitions are heavy)
MODULE_ORDER="gnovm rep blog forum collect shield reveal federation season"

# Track results
PASSED_TESTS=()
FAILED_TESTS=()
SKIPPED_TESTS=()
START_TIME=$(date +%s)

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
        --only)
            ONLY_MODULE="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --no-build       Skip building the binary (use existing)"
            echo "  --no-legacy      Skip legacy tests (commons, name, ecosystem, split, futarchy, gov)"
            echo "  --no-modules     Skip newer module tests (rep, blog, forum, etc.)"
            echo "  --only <module>  Run only one newer module (rep, blog, forum, collect, vote, reveal, season)"
            echo "  --help           Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0                        # Full suite"
            echo "  $0 --no-build             # Skip rebuild"
            echo "  $0 --no-legacy            # Only newer modules"
            echo "  $0 --only blog            # Only blog module"
            echo "  $0 --no-modules           # Only legacy tests"
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
        return 1
    fi

    chmod +x "$full_path"

    if (cd "$SCRIPT_DIR" && ./"$script"); then
        echo -e "${GREEN}>>> PASSED: $script${NC}"
        PASSED_TESTS+=("$script")
    else
        echo -e "${RED}>>> FAILED: $script${NC}"
        FAILED_TESTS+=("$script")
    fi

    sleep 2
}

# Run a newer module's full test suite via its run_all_tests.sh
# Reinitializes the chain before each module to ensure test isolation.
run_module() {
    local module="$1"
    local module_dir="$SCRIPT_DIR/$module"
    local runner="$module_dir/run_all_tests.sh"

    echo -e "\n${BLUE}===========================================================${NC}"
    echo -e "${BLUE}>>> MODULE: x/$module${NC}"
    echo -e "${BLUE}===========================================================${NC}"

    if [ ! -f "$runner" ]; then
        echo -e "${YELLOW}>>> SKIP: $module/run_all_tests.sh not found${NC}"
        SKIPPED_TESTS+=("x/$module")
        return 0
    fi

    # Reinitialize chain for module isolation
    echo -e "${BLUE}>>> Reinitializing chain for x/$module...${NC}"
    stop_chain
    rm -rf "$CHAIN_HOME"
    (cd "$PROJECT_DIR" && ignite chain init -y --build.tags testparams) || {
        echo -e "${RED}>>> FAILED: chain init for x/$module${NC}"
        FAILED_TESTS+=("x/$module (chain init)")
        return 1
    }
    $BINARY start --home "$CHAIN_HOME" > "$LOG_FILE" 2>&1 &
    CHAIN_PID=$!
    wait_for_chain || {
        echo -e "${RED}>>> FAILED: chain start for x/$module${NC}"
        FAILED_TESTS+=("x/$module (chain start)")
        return 1
    }

    chmod +x "$runner"

    if (cd "$module_dir" && bash run_all_tests.sh); then
        echo -e "${GREEN}>>> PASSED: x/$module${NC}"
        PASSED_TESTS+=("x/$module")
    else
        echo -e "${RED}>>> FAILED: x/$module${NC}"
        FAILED_TESTS+=("x/$module")
    fi

    sleep 2
}

stop_chain() {
    echo "Stopping any running chain..."
    pkill -f "sparkdreamd start" 2>/dev/null || true
    pkill -f sparkdreamd 2>/dev/null || true
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

    # Check 2: Commons group policy — min_execution_period should be 1s (test) not 259200s (production)
    echo -n "Checking x/commons policy execution period... "
    local policy_info
    policy_info=$($BINARY query group group-policies-by-group 1 --output json 2>/dev/null) || {
        echo -e "${YELLOW}SKIP (group query failed)${NC}"
        if [ "$failed" = true ]; then
            return 1
        fi
        return 0
    }

    local min_exec
    min_exec=$(echo "$policy_info" | jq -r '.group_policies[] | select(.metadata=="standard") | .decision_policy.windows.min_execution_period' 2>/dev/null | head -1)

    if [ -z "$min_exec" ]; then
        echo -e "${YELLOW}SKIP (standard policy not found)${NC}"
    else
        if [ "$min_exec" = "1s" ] || [ "$min_exec" = "0s" ]; then
            echo -e "${GREEN}OK (min_execution_period = $min_exec, test value)${NC}"
        else
            # Extract numeric seconds for comparison
            local secs
            secs=$(echo "$min_exec" | grep -oP '^\d+' || echo "0")
            if [ "$secs" -gt 60 ]; then
                echo -e "${RED}FAIL${NC}"
                echo -e "${RED}  min_execution_period = $min_exec (PRODUCTION value)${NC}"
                echo -e "${RED}  Expected 1s (test). Check x/commons/keeper/genesis_vals.go${NC}"
                echo -e "${RED}  Ensure the TESTING VALUES section is uncommented and PRODUCTION VALUES is commented out.${NC}"
                failed=true
            else
                echo -e "${GREEN}OK (min_execution_period = $min_exec)${NC}"
            fi
        fi
    fi

    if [ "$failed" = true ]; then
        echo ""
        echo -e "${RED}TEST PARAM VERIFICATION FAILED${NC}"
        echo -e "${RED}The binary appears to be built with production parameters.${NC}"
        echo -e "${RED}E2E tests require test parameters (fast timeouts, low thresholds).${NC}"
        echo ""
        echo "Files to check:"
        echo "  x/commons/keeper/genesis_vals.go  — governance timeouts"
        echo "  x/rep/types/params_vals.go        — trust level thresholds"
        echo ""
        echo "In each file, ensure the TESTING VALUES section is active (uncommented)"
        echo "and the PRODUCTION VALUES section is commented out."
        return 1
    fi

    echo -e "${GREEN}Test parameters verified.${NC}"
}

# ============================================================================
# Print Summary
# ============================================================================
print_summary() {
    local end_time=$(date +%s)
    local elapsed=$((end_time - START_TIME))
    local minutes=$((elapsed / 60))
    local seconds=$((elapsed % 60))

    echo ""
    echo -e "${BLUE}===========================================================${NC}"
    echo -e "${BLUE}  TEST SUITE SUMMARY${NC}"
    echo -e "${BLUE}===========================================================${NC}"
    echo ""
    echo "Duration: ${minutes}m ${seconds}s"
    echo ""

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
echo "Configuration:"
echo "  Build:   $([ "$RUN_BUILD" = true ] && echo "yes" || echo "skip")"
echo "  Legacy:  $([ "$RUN_LEGACY" = true ] && echo "yes" || echo "skip")"
echo "  Modules: $([ "$RUN_MODULES" = true ] && echo "yes" || echo "skip")"
if [ -n "$ONLY_MODULE" ]; then
    echo "  Only:    $ONLY_MODULE"
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
    (cd "$PROJECT_DIR" && ignite chain build --build.tags testparams)
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
rm -rf "$CHAIN_HOME"
(cd "$PROJECT_DIR" && ignite chain init -y --build.tags testparams)
echo -e "${GREEN}Chain initialized with testparams.${NC}"

# --- Step 4: Start chain ---
echo -e "\n${BLUE}=== STEP 4: START CHAIN ===${NC}"
$BINARY start --home "$CHAIN_HOME" > "$LOG_FILE" 2>&1 &
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

# --- Step 6: Legacy tests (Phases 1-5) ---
if [ "$RUN_LEGACY" = true ]; then
    echo -e "\n${BLUE}=== PHASE 1-2: COMMONS LIFECYCLE ===${NC}"
    run_test "commons/interim_council_test.sh"
    run_test "commons/group_lifecycle_test.sh"
    run_test "commons/group_member_update_test.sh"
    run_test "commons/treasury_spend.sh"
    run_test "commons/fee_update_test.sh"

    echo -e "\n${BLUE}=== PHASE 3: FEATURE MODULES ===${NC}"
    # Name module
    run_test "name/setup_test_accounts.sh"
    run_test "name/name_registration_test.sh"
    run_test "name/primary_name_test.sh"
    run_test "name/dispute_resolution_test.sh"
    run_test "name/operational_params_test.sh"

    # Ecosystem
    run_test "ecosystem/ecosystem_spend.sh"

    # Split
    run_test "split/accounts.sh"
    run_test "split/autodivert.sh"

    # Futarchy
    run_test "futarchy/market_lifecycle_test.sh"
    run_test "futarchy/governance_integration_test.sh"
    run_test "futarchy/params_update_test.sh"
    run_test "futarchy/liquidity_withdrawal_test.sh"
    run_test "futarchy/emergency_cancel_test.sh"
    run_test "futarchy/operational_params_test.sh"

    echo -e "\n${BLUE}=== PHASE 4: SECURITY ===${NC}"
    run_test "commons/group_security_test.sh"
    run_test "commons/policy_lifecycle_security_test.sh"
    run_test "commons/policy_permissions_test.sh"
    run_test "commons/unauthorized_spend_msg.sh"
    run_test "commons/unauthorized_handover.sh"
    run_test "ecosystem/ecosystem_security_test.sh"
    run_test "gov/inflation_immutable_test.sh"
    run_test "commons/anon_test.sh"

    echo -e "\n${BLUE}=== PHASE 5: VETOS ===${NC}"
    run_test "commons/executive_veto_test.sh"
    run_test "commons/social_veto_vote_test.sh"
    run_test "commons/parent_veto_test.sh"
    run_test "commons/veto_vote_test.sh"
fi

# --- Step 7: Newer module tests ---
if [ "$RUN_MODULES" = true ]; then
    echo -e "\n${BLUE}=== NEWER MODULE TESTS ===${NC}"

    if [ -n "$ONLY_MODULE" ]; then
        # Run only the specified module
        run_module "$ONLY_MODULE"
    else
        # Run all modules in dependency order
        for module in $MODULE_ORDER; do
            run_module "$module"
        done
    fi
fi

# --- Step 8: Destructive legacy tests (LAST) ---
if [ "$RUN_LEGACY" = true ]; then
    echo -e "\n${BLUE}=== PHASE 6: INFRASTRUCTURE ===${NC}"
    run_test "commons/tech_upgrade_golden_share.sh"

    echo -e "\n${BLUE}=== PHASE 7: DESTRUCTIVE TESTS ===${NC}"
    run_test "commons/fire_council_test.sh"
fi

# --- Step 9: Stop chain & report ---
echo -e "\n${BLUE}=== CLEANUP ===${NC}"
stop_chain

print_summary
