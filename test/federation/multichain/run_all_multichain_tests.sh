#!/bin/bash
# ------------------------------------------------------------------
# Multi-Chain Federation E2E Test Suite — Master Orchestrator
#
# Provisions two sparkdreamd chains, establishes IBC, configures
# peers, policies, and chain-specific keys, starts hermes, then runs
# cross-chain content, identity, and reputation tests.
#
# Usage:
#   ./run_all_multichain_tests.sh                # Full provision + test (auto-creates snapshot)
#   ./run_all_multichain_tests.sh --no-provision # Tests only (chains already running)
#   ./run_all_multichain_tests.sh --tests-only   # Same as --no-provision
#   ./run_all_multichain_tests.sh --no-cleanup   # Leave chains/relayer running on exit
#   ./run_all_multichain_tests.sh --skip-prereqs # Skip host prereq check
#   ./run_all_multichain_tests.sh --no-auto-snapshot
#                                                # Fail instead of auto-creating a missing snapshot
#
# Environment:
#   BINARY    path to sparkdreamd (default: sparkdreamd)
#   HERMES    path to hermes (default: hermes on PATH)
#   SNAPSHOT  path to post-setup snapshot (default: ../snapshots/post-setup/sparkdream_data)
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Shared timing helpers (timing_now_epoch, timing_format_duration, ...).
# Located two levels up at test/_timing.sh.
source "$SCRIPT_DIR/../../_timing.sh"

# Wall-clock timing for the suite (Started/Ended/Duration in summary).
SUITE_START_EPOCH=$(timing_now_epoch)
SUITE_START_HUMAN=$(timing_now_human)

# Per-phase timing — populated by run_phase, consumed by the summary block.
declare -a TIMED_NAMES=()
declare -a TIMED_RESULTS=()
declare -a TIMED_DURATIONS_S=()

RUN_PROVISION=true
RUN_CLEANUP=true
SKIP_PREREQS=false
AUTO_SNAPSHOT=true

for arg in "$@"; do
    case $arg in
        --no-provision|--tests-only) RUN_PROVISION=false ;;
        --no-cleanup)                RUN_CLEANUP=false ;;
        --skip-prereqs)              SKIP_PREREQS=true ;;
        --no-auto-snapshot)          AUTO_SNAPSHOT=false ;;
        --help|-h)
            sed -n '4,21p' "$0" | sed 's/^# //; s/^#//'
            exit 0
            ;;
        *)
            echo "Unknown option: $arg"
            exit 1
            ;;
    esac
done

GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

PHASES_PASSED=0
PHASES_FAILED=0
FAILED_PHASES=()

run_phase() {
    local PHASE_NAME=$1; local PHASE_SCRIPT=$2
    echo ""
    echo -e "${BLUE}============================================================${NC}"
    echo -e "${BLUE}>>> PHASE: $PHASE_NAME${NC}"
    echo -e "${BLUE}============================================================${NC}"
    echo ""

    if [ ! -f "$SCRIPT_DIR/$PHASE_SCRIPT" ]; then
        echo -e "${RED}  Script not found: $PHASE_SCRIPT${NC}"
        PHASES_FAILED=$((PHASES_FAILED + 1))
        FAILED_PHASES+=("$PHASE_NAME")
        TIMED_NAMES+=("$PHASE_NAME")
        TIMED_RESULTS+=("FAIL")
        TIMED_DURATIONS_S+=(0)
        return 1
    fi

    chmod +x "$SCRIPT_DIR/$PHASE_SCRIPT"
    local _t0 _t1 _dur_s _dur
    _t0=$(timing_now_epoch)
    if bash "$SCRIPT_DIR/$PHASE_SCRIPT"; then
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        echo ""
        echo -e "${GREEN}>>> PHASE PASSED: $PHASE_NAME ($_dur)${NC}"
        PHASES_PASSED=$((PHASES_PASSED + 1))
        TIMED_NAMES+=("$PHASE_NAME")
        TIMED_RESULTS+=("PASS")
        TIMED_DURATIONS_S+=("$_dur_s")
        return 0
    else
        _t1=$(timing_now_epoch)
        _dur_s=$((_t1 - _t0))
        _dur=$(timing_format_duration "$_dur_s")
        echo ""
        echo -e "${RED}>>> PHASE FAILED: $PHASE_NAME ($_dur)${NC}"
        PHASES_FAILED=$((PHASES_FAILED + 1))
        FAILED_PHASES+=("$PHASE_NAME")
        TIMED_NAMES+=("$PHASE_NAME")
        TIMED_RESULTS+=("FAIL")
        TIMED_DURATIONS_S+=("$_dur_s")
        return 1
    fi
}

cleanup() {
    if [ "$RUN_CLEANUP" = true ]; then
        echo ""
        echo "=== Cleanup ==="
        bash "$SCRIPT_DIR/stop_chains.sh" 2>/dev/null || true
    else
        echo ""
        echo -e "${YELLOW}--no-cleanup: chains and relayer left running.${NC}"
        echo "  Stop manually with: bash $SCRIPT_DIR/stop_chains.sh"
    fi
}

# Always cleanup on exit unless --no-cleanup
trap cleanup EXIT

echo "============================================================================"
echo "           X/FEDERATION — MULTI-CHAIN E2E TEST SUITE"
echo "============================================================================"
echo ""
echo "  Chain A: fedtest-a (RPC: 26657)"
echo "  Chain B: fedtest-b (RPC: 36657)"
echo "  hermes:  ${HERMES:-hermes}"
echo "  binary:  ${BINARY:-sparkdreamd}"
echo ""

# ==========================================================================
# Phase 0.0: Prereq check
# ==========================================================================
if [ "$SKIP_PREREQS" != "true" ]; then
    run_phase "Prerequisite Checks" "check_prereqs.sh" || exit 1
fi

# NOTE: the multichain suite no longer needs the post-setup snapshot;
# init_chains.sh now bootstraps each chain freshly via `ignite chain init`.
# The AUTO_SNAPSHOT flag is retained for backward CLI compatibility but is
# a no-op here. The parent runner (../run_all_tests.sh) still uses the
# snapshot for fast iteration of the single-chain federation tests.

# ==========================================================================
# Provisioning
# ==========================================================================
if [ "$RUN_PROVISION" = true ]; then
    echo ""
    echo "============================================================================"
    echo "  PROVISIONING: CHAINS + IBC + RELAYER + PEERS + POLICIES + KEYS"
    echo "============================================================================"

    # Clean slate (also stops any bootstrap chain left from auto-snapshot)
    bash "$SCRIPT_DIR/stop_chains.sh" 2>/dev/null || true
    pkill -f "sparkdreamd.*$HOME/.sparkdream" 2>/dev/null || true
    sleep 1

    run_phase "Init Chains"             "init_chains.sh"      || exit 1
    run_phase "Start Chains"            "start_chains.sh"     || exit 1
    run_phase "IBC Setup"               "setup_ibc.sh"        || exit 1
    run_phase "Start Hermes Relayer"    "start_relayer.sh"    || exit 1
    run_phase "Peer Setup"              "setup_peers.sh"      || exit 1
    run_phase "Policy Setup"            "setup_policies.sh"   || exit 1
    run_phase "Chain-Specific Keys"     "setup_chain_keys.sh" || exit 1

    echo ""
    echo -e "${GREEN}=== PROVISIONING COMPLETE ===${NC}"
fi

# ==========================================================================
# Tests
# ==========================================================================
echo ""
echo "============================================================================"
echo "  RUNNING CROSS-CHAIN TESTS"
echo "============================================================================"

run_phase "Cross-Chain Content"    "test_crosschain_content.sh"    || true
run_phase "Cross-Chain Identity"   "test_crosschain_identity.sh"   || true
run_phase "Cross-Chain Reputation" "test_crosschain_reputation.sh" || true

# ==========================================================================
# Final Summary
# ==========================================================================
echo ""
echo "============================================================================"
echo "           MULTI-CHAIN TEST SUITE — FINAL SUMMARY"
echo "============================================================================"
echo ""
SUITE_END_EPOCH=$(timing_now_epoch)
SUITE_END_HUMAN=$(timing_now_human)
timing_print_summary_block "$SUITE_START_EPOCH" "$SUITE_END_EPOCH" \
    "$SUITE_START_HUMAN" "$SUITE_END_HUMAN"
echo ""
echo "  Phases Run:    $((PHASES_PASSED + PHASES_FAILED))"
echo "  Phases Passed: $PHASES_PASSED"
echo "  Phases Failed: $PHASES_FAILED"
echo ""

# Per-phase timings (in execution order).
if [ ${#TIMED_NAMES[@]} -gt 0 ]; then
    timing_print_per_test_table TIMED_RESULTS TIMED_DURATIONS_S TIMED_NAMES
    echo ""
fi

if [ $PHASES_FAILED -gt 0 ]; then
    echo -e "${RED}Failed Phases:${NC}"
    for phase in "${FAILED_PHASES[@]}"; do
        echo "  - $phase"
    done
    echo ""
    echo -e "${RED}>>> SOME PHASES FAILED <<<${NC}"
    exit 1
fi

echo -e "${GREEN}>>> ALL PHASES PASSED <<<${NC}"
echo ""
