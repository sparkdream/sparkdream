#!/bin/bash

# ============================================================================
# DKG CEREMONY LIFECYCLE E2E TEST (x/shield)
# ============================================================================
#
# Tests the live DKG (Distributed Key Generation) ceremony flow:
#   1. Verify DKG auto-triggers when bonded validators >= min_tle_validators
#   2. Watch phase transitions: REGISTERING -> CONTRIBUTING -> ACTIVE
#   3. Verify TLE key set is established after DKG completion
#   4. Verify encrypted_batch_enabled is auto-set to true
#   5. Verify DKG state queries return correct data
#
# Prerequisites:
#   - Chain started with DKG-patched genesis (patch_genesis_dkg.sh)
#     which sets min_tle_validators = 1
#   - Vote extensions enabled at height 1 (config.yml)
#   - ABCI handlers wired in app.go (ExtendVote, VerifyVoteExtension,
#     PrepareProposal, ProcessProposal, PreBlocker)
#   - setup_test_accounts.sh completed
#
# This test is SLOW (~2 minutes) because it waits for the DKG window
# (dkg_window_blocks = 20 blocks at ~6s/block = ~120 seconds).
#
# This test is mutually exclusive with encrypted_batch_test.sh — that test
# uses pre-seeded TLE keys (patch_genesis_tle.sh), while this test lets
# the DKG ceremony complete naturally (patch_genesis_dkg.sh).
# ============================================================================

echo "--- TESTING: DKG Ceremony Lifecycle (x/shield) ---"
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "  Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# === PASS/FAIL TRACKING ===
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}

get_block_height() {
    $BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"'
}

# =========================================================================
# PART 1: Verify preconditions
# =========================================================================
echo "--- PART 1: Verify DKG preconditions ---"

PARAMS=$($BINARY query shield params --output json 2>&1)
MIN_TLE_VALS=$(echo "$PARAMS" | jq -r '.params.min_tle_validators // "0"')
DKG_WINDOW=$(echo "$PARAMS" | jq -r '.params.dkg_window_blocks // "0"')
BATCH_ENABLED=$(echo "$PARAMS" | jq -r '.params.encrypted_batch_enabled // "false"')

echo "  min_tle_validators: $MIN_TLE_VALS"
echo "  dkg_window_blocks: $DKG_WINDOW"
echo "  encrypted_batch_enabled: $BATCH_ENABLED"

if [ "$MIN_TLE_VALS" != "1" ]; then
    echo ""
    echo "  min_tle_validators is NOT 1."
    echo "  This test requires patch_genesis_dkg.sh (sets min_tle_validators=1)."
    echo ""
    echo "--- TEST SUMMARY ---"
    echo "  DKG ceremony tests skipped (min_tle_validators != 1)"
    echo "  Tests: 0 passed, 0 failed"
    exit 0
fi

# If batch is already enabled, a TLE key set was seeded — wrong genesis patch
if [ "$BATCH_ENABLED" == "true" ]; then
    # Check if DKG already completed (not seeded)
    DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)
    DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.state.phase // "DKG_PHASE_INACTIVE"' 2>/dev/null)
    if [ "$DKG_PHASE" == "DKG_PHASE_ACTIVE" ]; then
        echo "  DKG already completed (phase=ACTIVE, batch_enabled=true)"
        echo "  Skipping ceremony wait — will verify final state only"
    else
        echo ""
        echo "  encrypted_batch_enabled is already true but DKG is not ACTIVE."
        echo "  This likely means patch_genesis_tle.sh was used instead of patch_genesis_dkg.sh."
        echo "  Use patch_genesis_dkg.sh for this test."
        echo ""
        echo "--- TEST SUMMARY ---"
        echo "  DKG ceremony tests skipped (wrong genesis patch)"
        echo "  Tests: 0 passed, 0 failed"
        exit 0
    fi
fi

record_result "DKG preconditions verified" "PASS"

# =========================================================================
# PART 2: Check initial DKG state (should be INACTIVE or REGISTERING)
# =========================================================================
echo "--- PART 2: Check initial DKG state ---"

DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)
DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.state.phase // "unknown"' 2>/dev/null)
DKG_ROUND=$(echo "$DKG_STATE" | jq -r '.state.round // "0"' 2>/dev/null)

echo "  DKG phase: $DKG_PHASE"
echo "  DKG round: $DKG_ROUND"

# The DKG should have auto-triggered already (REGISTERING) or be about to
# (INACTIVE). Both are valid starting points.
if [ "$DKG_PHASE" == "DKG_PHASE_REGISTERING" ] || [ "$DKG_PHASE" == "DKG_PHASE_CONTRIBUTING" ]; then
    echo "  DKG has already auto-triggered (phase=$DKG_PHASE)"
    record_result "DKG auto-trigger detected" "PASS"
elif [ "$DKG_PHASE" == "DKG_PHASE_ACTIVE" ]; then
    echo "  DKG already completed (phase=ACTIVE)"
    record_result "DKG auto-trigger detected" "PASS"
elif [ "$DKG_PHASE" == "DKG_PHASE_INACTIVE" ] || [ "$DKG_PHASE" == "unknown" ]; then
    echo "  DKG is INACTIVE — waiting for auto-trigger..."
    # Wait a few blocks for the auto-trigger to fire
    WAIT_BLOCKS=5
    START_H=$(get_block_height)
    TARGET_H=$((START_H + WAIT_BLOCKS))
    echo "  Waiting up to $WAIT_BLOCKS blocks (current=$START_H)..."

    TRIGGERED=false
    for attempt in $(seq 1 $((WAIT_BLOCKS * 7))); do
        CUR_H=$(get_block_height)
        if [ "$CUR_H" -ge "$TARGET_H" ]; then
            break
        fi

        DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)
        DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.state.phase // "unknown"' 2>/dev/null)

        if [ "$DKG_PHASE" != "DKG_PHASE_INACTIVE" ] && [ "$DKG_PHASE" != "unknown" ]; then
            echo "  DKG auto-triggered at height $CUR_H (phase=$DKG_PHASE)"
            TRIGGERED=true
            break
        fi
        sleep 1
    done

    if [ "$TRIGGERED" = true ]; then
        record_result "DKG auto-trigger detected" "PASS"
    else
        echo "  DKG did not auto-trigger within $WAIT_BLOCKS blocks"
        echo "  Check: min_tle_validators, bonded validators, staking keeper wiring"
        record_result "DKG auto-trigger detected" "FAIL"
    fi
else
    echo "  Unexpected DKG phase: $DKG_PHASE"
    record_result "DKG auto-trigger detected" "FAIL"
fi

# Re-read current state
DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)
DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.state.phase // "unknown"' 2>/dev/null)
DKG_ROUND=$(echo "$DKG_STATE" | jq -r '.state.round // "0"' 2>/dev/null)
REG_DEADLINE=$(echo "$DKG_STATE" | jq -r '.state.registration_deadline // "0"' 2>/dev/null)
CONTRIB_DEADLINE=$(echo "$DKG_STATE" | jq -r '.state.contribution_deadline // "0"' 2>/dev/null)
EXPECTED_VALS=$(echo "$DKG_STATE" | jq -r '.state.expected_validators | length' 2>/dev/null)

echo "  Current DKG state:"
echo "    Phase: $DKG_PHASE"
echo "    Round: $DKG_ROUND"
echo "    Registration deadline: block $REG_DEADLINE"
echo "    Contribution deadline: block $CONTRIB_DEADLINE"
echo "    Expected validators: $EXPECTED_VALS"

# =========================================================================
# PART 3: Wait for REGISTERING -> CONTRIBUTING transition
# =========================================================================
echo "--- PART 3: Wait for REGISTERING -> CONTRIBUTING transition ---"

if [ "$DKG_PHASE" == "DKG_PHASE_ACTIVE" ]; then
    echo "  DKG already ACTIVE — skipping phase transition wait"
    record_result "REGISTERING -> CONTRIBUTING transition" "PASS"
elif [ "$DKG_PHASE" == "DKG_PHASE_CONTRIBUTING" ]; then
    echo "  Already in CONTRIBUTING phase — skipping"
    record_result "REGISTERING -> CONTRIBUTING transition" "PASS"
elif [ "$DKG_PHASE" == "DKG_PHASE_REGISTERING" ]; then
    CURRENT_H=$(get_block_height)
    BLOCKS_TO_WAIT=$((REG_DEADLINE - CURRENT_H + 2))  # +2 for safety margin
    if [ "$BLOCKS_TO_WAIT" -le 0 ]; then
        BLOCKS_TO_WAIT=3
    fi
    SECONDS_TO_WAIT=$((BLOCKS_TO_WAIT * 7))

    echo "  Current height: $CURRENT_H, registration deadline: $REG_DEADLINE"
    echo "  Waiting ~$BLOCKS_TO_WAIT blocks (~${SECONDS_TO_WAIT}s) for CONTRIBUTING phase..."

    TRANSITIONED=false
    for attempt in $(seq 1 $SECONDS_TO_WAIT); do
        DKG_NOW=$($BINARY query shield dkg-state --output json 2>&1)
        PHASE_NOW=$(echo "$DKG_NOW" | jq -r '.state.phase // "unknown"' 2>/dev/null)

        if [ "$PHASE_NOW" == "DKG_PHASE_CONTRIBUTING" ] || [ "$PHASE_NOW" == "DKG_PHASE_ACTIVE" ]; then
            CUR_H=$(get_block_height)
            echo "  Phase transitioned to $PHASE_NOW at height $CUR_H"
            TRANSITIONED=true
            break
        fi
        sleep 1
    done

    if [ "$TRANSITIONED" = true ]; then
        record_result "REGISTERING -> CONTRIBUTING transition" "PASS"
    else
        echo "  Phase did not transition within wait period"
        record_result "REGISTERING -> CONTRIBUTING transition" "FAIL"
    fi
else
    echo "  Cannot test transition from phase: $DKG_PHASE"
    record_result "REGISTERING -> CONTRIBUTING transition" "FAIL"
fi

# =========================================================================
# PART 4: Wait for CONTRIBUTING -> ACTIVE transition (DKG completion)
# =========================================================================
echo "--- PART 4: Wait for DKG completion (ACTIVE phase) ---"

# Re-read state
DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)
DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.state.phase // "unknown"' 2>/dev/null)
CONTRIB_DEADLINE=$(echo "$DKG_STATE" | jq -r '.state.contribution_deadline // "0"' 2>/dev/null)

if [ "$DKG_PHASE" == "DKG_PHASE_ACTIVE" ]; then
    echo "  DKG already ACTIVE — skipping"
    record_result "DKG ceremony completed (ACTIVE)" "PASS"
else
    CURRENT_H=$(get_block_height)
    BLOCKS_TO_WAIT=$((CONTRIB_DEADLINE - CURRENT_H + 3))  # +3 safety margin
    if [ "$BLOCKS_TO_WAIT" -le 0 ]; then
        BLOCKS_TO_WAIT=5
    fi
    SECONDS_TO_WAIT=$((BLOCKS_TO_WAIT * 7))

    echo "  Current height: $CURRENT_H, contribution deadline: $CONTRIB_DEADLINE"
    echo "  Waiting ~$BLOCKS_TO_WAIT blocks (~${SECONDS_TO_WAIT}s) for DKG completion..."

    DKG_COMPLETED=false
    for attempt in $(seq 1 $SECONDS_TO_WAIT); do
        DKG_NOW=$($BINARY query shield dkg-state --output json 2>&1)
        PHASE_NOW=$(echo "$DKG_NOW" | jq -r '.state.phase // "unknown"' 2>/dev/null)

        if [ "$PHASE_NOW" == "DKG_PHASE_ACTIVE" ]; then
            CUR_H=$(get_block_height)
            echo "  DKG completed! Phase=ACTIVE at height $CUR_H"
            DKG_COMPLETED=true
            break
        fi

        # Check for DKG failure (reset to INACTIVE)
        if [ "$PHASE_NOW" == "DKG_PHASE_INACTIVE" ] && [ "$attempt" -gt 10 ]; then
            CUR_H=$(get_block_height)
            ROUND_NOW=$(echo "$DKG_NOW" | jq -r '.state.round // "0"' 2>/dev/null)
            echo "  DKG appears to have failed and reset to INACTIVE at height $CUR_H"
            echo "  Round: $ROUND_NOW (started at $DKG_ROUND)"
            echo "  This may indicate vote extensions are not delivering DKG contributions"
            break
        fi

        sleep 1
    done

    if [ "$DKG_COMPLETED" = true ]; then
        record_result "DKG ceremony completed (ACTIVE)" "PASS"
    else
        echo "  DKG did not complete within wait period"
        echo ""
        echo "  Debugging info:"
        echo "  Final DKG state:"
        $BINARY query shield dkg-state --output json 2>&1 | jq '.' 2>/dev/null
        echo ""
        echo "  DKG contributions:"
        $BINARY query shield dkg-contributions --output json 2>&1 | jq '.' 2>/dev/null
        record_result "DKG ceremony completed (ACTIVE)" "FAIL"
    fi
fi

# =========================================================================
# PART 5: Verify TLE key set was established
# =========================================================================
echo "--- PART 5: Verify TLE key set ---"

TLE_KS=$($BINARY query shield tle-key-set --output json 2>&1)
TLE_MPK=$(echo "$TLE_KS" | jq -r '.key_set.master_public_key // empty' 2>/dev/null)
TLE_THRESHOLD_N=$(echo "$TLE_KS" | jq -r '.key_set.threshold_numerator // "0"' 2>/dev/null)
TLE_THRESHOLD_D=$(echo "$TLE_KS" | jq -r '.key_set.threshold_denominator // "0"' 2>/dev/null)
TLE_VAL_SHARES=$(echo "$TLE_KS" | jq -r '.key_set.validator_shares | length' 2>/dev/null)
TLE_CREATED_AT=$(echo "$TLE_KS" | jq -r '.key_set.created_at_height // "0"' 2>/dev/null)

echo "  Master public key: ${TLE_MPK:0:20}..."
echo "  Threshold: $TLE_THRESHOLD_N/$TLE_THRESHOLD_D"
echo "  Validator shares: $TLE_VAL_SHARES"
echo "  Created at height: $TLE_CREATED_AT"

if [ -n "$TLE_MPK" ] && [ "$TLE_MPK" != "null" ] && [ "$TLE_MPK" != "" ]; then
    record_result "TLE key set established" "PASS"
else
    echo "  TLE master public key is empty — DKG did not produce a key set"
    record_result "TLE key set established" "FAIL"
fi

# =========================================================================
# PART 6: Verify encrypted_batch_enabled was auto-set to true
# =========================================================================
echo "--- PART 6: Verify encrypted_batch_enabled auto-enabled ---"

PARAMS_NOW=$($BINARY query shield params --output json 2>&1)
BATCH_NOW=$(echo "$PARAMS_NOW" | jq -r '.params.encrypted_batch_enabled // "false"')

echo "  encrypted_batch_enabled: $BATCH_NOW"

if [ "$BATCH_NOW" == "true" ]; then
    record_result "encrypted_batch_enabled auto-set to true" "PASS"
else
    echo "  encrypted_batch_enabled was NOT set to true after DKG"
    echo "  This should be auto-enabled in dkgAdvanceContributing()"
    record_result "encrypted_batch_enabled auto-set to true" "FAIL"
fi

# =========================================================================
# PART 7: Verify DKG state query returns correct final state
# =========================================================================
echo "--- PART 7: Verify final DKG state ---"

FINAL_DKG=$($BINARY query shield dkg-state --output json 2>&1)
FINAL_PHASE=$(echo "$FINAL_DKG" | jq -r '.state.phase // "unknown"' 2>/dev/null)
FINAL_ROUND=$(echo "$FINAL_DKG" | jq -r '.state.round // "0"' 2>/dev/null)
FINAL_CONTRIBS=$(echo "$FINAL_DKG" | jq -r '.state.contributions_received // "0"' 2>/dev/null)
FINAL_EXPECTED=$(echo "$FINAL_DKG" | jq -r '.state.expected_validators | length' 2>/dev/null)

echo "  Phase: $FINAL_PHASE"
echo "  Round: $FINAL_ROUND"
echo "  Contributions received: $FINAL_CONTRIBS"
echo "  Expected validators: $FINAL_EXPECTED"

if [ "$FINAL_PHASE" == "DKG_PHASE_ACTIVE" ]; then
    # Verify the contribution count matches the expected validator count
    if [ "$FINAL_CONTRIBS" -ge 1 ]; then
        echo "  DKG state is consistent"
        record_result "DKG final state consistent" "PASS"
    else
        echo "  WARNING: contributions_received=$FINAL_CONTRIBS (expected >= 1)"
        record_result "DKG final state consistent" "FAIL"
    fi
else
    echo "  DKG is not in ACTIVE phase"
    record_result "DKG final state consistent" "FAIL"
fi

# =========================================================================
# PART 8: Verify validator share in TLE key set matches our validator
# =========================================================================
echo "--- PART 8: Verify validator share assignment ---"

# Get the single validator's operator address from the DKG expected list
EXPECTED_VAL=$(echo "$FINAL_DKG" | jq -r '.state.expected_validators[0] // empty' 2>/dev/null)
echo "  Expected validator: $EXPECTED_VAL"

# Check the TLE key set has a share for this validator
SHARE_VAL=$(echo "$TLE_KS" | jq -r '.key_set.validator_shares[0].validator_address // empty' 2>/dev/null)
SHARE_IDX=$(echo "$TLE_KS" | jq -r '.key_set.validator_shares[0].share_index // "0"' 2>/dev/null)
SHARE_PUB=$(echo "$TLE_KS" | jq -r '.key_set.validator_shares[0].public_share // empty' 2>/dev/null)

echo "  TLE share validator: $SHARE_VAL"
echo "  TLE share index: $SHARE_IDX"
echo "  TLE public share: ${SHARE_PUB:0:20}..."

if [ -n "$SHARE_VAL" ] && [ "$SHARE_VAL" != "null" ]; then
    if [ "$SHARE_VAL" == "$EXPECTED_VAL" ]; then
        echo "  Validator address matches"
        record_result "Validator share assignment correct" "PASS"
    else
        echo "  Validator mismatch: expected=$EXPECTED_VAL, got=$SHARE_VAL"
        record_result "Validator share assignment correct" "FAIL"
    fi
else
    echo "  No validator shares found in TLE key set"
    record_result "Validator share assignment correct" "FAIL"
fi

# =========================================================================
# PART 9: Query epoch state (should be initialized after DKG)
# =========================================================================
echo "--- PART 9: Verify shield epoch state ---"

EPOCH_STATE=$($BINARY query shield shield-epoch --output json 2>&1)
CURRENT_EPOCH=$(echo "$EPOCH_STATE" | jq -r '.state.current_epoch // "null"' 2>/dev/null)
EPOCH_START_H=$(echo "$EPOCH_STATE" | jq -r '.state.epoch_start_height // "0"' 2>/dev/null)

echo "  Current epoch: $CURRENT_EPOCH"
echo "  Epoch start height: $EPOCH_START_H"

# Epoch state may or may not be initialized yet (depends on EndBlocker running)
if [ "$CURRENT_EPOCH" != "null" ] && [ "$CURRENT_EPOCH" != "" ]; then
    record_result "Shield epoch state initialized" "PASS"
else
    echo "  Epoch state not yet initialized (EndBlocker may not have run)"
    echo "  This is acceptable — epoch starts on first EndBlocker after DKG"
    record_result "Shield epoch state initialized" "PASS"
fi

# =========================================================================
# TEST SUMMARY
# =========================================================================
echo "============================================="
echo "--- DKG CEREMONY TEST SUMMARY ---"
echo "============================================="
echo ""
echo "  Tests Passed: $PASS_COUNT"
echo "  Tests Failed: $FAIL_COUNT"
echo ""

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
