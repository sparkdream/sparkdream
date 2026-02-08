#!/bin/bash

# Stop execution immediately if any command returns a non-zero exit code
set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Helper function to run a test and print status
run_test() {
    local script="$1"
    echo -e "\n${BLUE}======================================================${NC}"
    echo -e "${BLUE}>>> RUNNING: $script${NC}"
    echo -e "${BLUE}======================================================${NC}"
    
    # Check if file exists and is executable
    if [ -f "$script" ]; then
        # Grant execute permissions just in case
        chmod +x "$script"
        
        # Run the script
        ./"$script"
        
        echo -e "${GREEN}>>> PASSED: $script${NC}"
    else
        echo -e "${RED}>>> ERROR: File not found: $script${NC}"
        exit 1
    fi
    
    # Small sleep to let chain state settle/commit between test suites
    sleep 2
}

echo -e "${GREEN}STARTING SPARK DREAM INTEGRATION TEST SUITE${NC}"

# --- PHASE 1: BOOTSTRAP & GOVERNANCE SETUP ---
# These must run first to establish the Council authority.
#run_test "commons/group_setup.sh"
# Note: This test is deprecated. Governance groups are now bootstrapped directly in the genesis block.

# --- PHASE 2: COMMONS LIFECYCLE & LOGIC ---
# Standard operations for the group module.
run_test "commons/interim_council_test.sh"
run_test "commons/group_lifecycle_test.sh"
run_test "commons/group_member_update_test.sh"
run_test "commons/treasury_spend.sh"
run_test "commons/fee_update_test.sh"

# --- PHASE 3: FEATURE MODULES ---
# These modules likely rely on the Council established in Phase 1.

# Name Module
# Registration must happen before Primary/Disputes
run_test "name/setup_test_accounts.sh"
run_test "name/name_registration_test.sh"
run_test "name/primary_name_test.sh"
run_test "name/dispute_resolution_test.sh" 

# Ecosystem Module
run_test "ecosystem/ecosystem_spend.sh"

# Split Module
run_test "split/accounts.sh"
run_test "split/autodivert.sh"

# Futarchy Module
# Test prediction market creation, trading, and resolution
run_test "futarchy/market_lifecycle_test.sh"
run_test "futarchy/governance_integration_test.sh"
run_test "futarchy/params_update_test.sh"
run_test "futarchy/liquidity_withdrawal_test.sh"
run_test "futarchy/emergency_cancel_test.sh"

# --- PHASE 4: SECURITY & PERMISSIONS ---
# Testing attacks and unauthorized attempts.
run_test "commons/group_security_test.sh"
run_test "commons/policy_lifecycle_security_test.sh"
run_test "commons/policy_permissions_test.sh"
run_test "commons/unauthorized_spend_msg.sh"
run_test "commons/unauthorized_handover.sh"
run_test "ecosystem/ecosystem_security_test.sh"

# Governance Security
# CRITICAL: Test that inflation parameters cannot be changed via governance
run_test "gov/inflation_immutable_test.sh"

# --- PHASE 5: ADVANCED GOVERNANCE (VETOS) ---
run_test "commons/executive_veto_test.sh"
run_test "commons/social_veto_vote_test.sh"
run_test "commons/veto_vote_test.sh"

# --- PHASE 6: INFRASTRUCTURE & UPGRADES ---
# This schedules a chain halt/upgrade. It must run BEFORE firing the council,
# because the Council is required to authorize the upgrade (via Golden Share).
run_test "commons/tech_upgrade_golden_share.sh"

# --- PHASE 7: DESTRUCTIVE TESTS ---
# Run these LAST.
# Note: If the upgrade in Phase 6 halts the chain immediately, this test might not run.
run_test "commons/fire_council_test.sh"

echo -e "\n${GREEN}******************************************************${NC}"
echo -e "${GREEN}ALL TESTS PASSED SUCCESSFULLY! 🚀${NC}"
echo -e "${GREEN}******************************************************${NC}"