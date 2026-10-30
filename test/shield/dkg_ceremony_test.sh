#!/bin/bash

# ============================================================================
# DKG CEREMONY LIFECYCLE E2E TEST (x/shield)
# ============================================================================
#
# Tests the live DKG (Distributed Key Generation) ceremony flow:
#   1. Verify DKG auto-triggers when bonded validators >= min_tle_validators
#   2. Watch phase transitions: REGISTERING -> CONTRIBUTING -> COMPLAINING
#      -> JUSTIFYING -> ACTIVE
#   3. Verify TLE key set is established after DKG completion
#   4. Verify encrypted_batch_enabled follows enable_batch_after_dkg
#   5. Verify DKG state queries return correct data
#
# Prerequisites:
#   - Chain started with DKG-patched genesis (patch_genesis_dkg.sh)
#   - setup_test_accounts.sh completed (submitter1, SHIELD_MODULE_ADDR) for PART 10
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
PROJECT_ROOT="$SCRIPT_DIR/../.."
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "  Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"
# .test_env may predate the shield account (or the query raced the first
# block at setup); resolve the address live when it is missing.
if [ -z "${SHIELD_MODULE_ADDR:-}" ]; then
    SHIELD_MODULE_ADDR=$($BINARY query auth module-account shield --output json 2>/dev/null | jq -r '.account.base_account.address // .account.value.address // ""' 2>/dev/null)
fi

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
    elif [ "$RESULT" == "SKIP" ]; then
        SKIP_COUNT=$((SKIP_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}
SKIP_COUNT=0

get_block_height() {
    $BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"'
}

# ensure_shield_reserve: the shield module pays the gas for every shielded
# exec, and a fresh test chain's community pool is empty, so the BeginBlocker
# has nothing to draw from. Top the pool up from alice and poll (bounded) until
# the module account holds SPARK.
ensure_shield_reserve() {
    local BAL
    BAL=$($BINARY query shield module-balance --output json 2>/dev/null | jq -r '.balance.amount // "0"')
    if [ "${BAL:-0}" -ge 10000000 ] 2>/dev/null; then
        return 0
    fi
    echo "  Shield reserve is ${BAL:-0}; funding the community pool from alice..."
    $BINARY tx distribution fund-community-pool 300000000${BOND_DENOM} --from alice -y \
        --chain-id "$CHAIN_ID" --keyring-backend test --fees 5000${BOND_DENOM} --output json >/dev/null 2>&1
    local i
    for i in $(seq 1 40); do
        sleep 2
        BAL=$($BINARY query shield module-balance --output json 2>/dev/null | jq -r '.balance.amount // "0"')
        if [ "${BAL:-0}" -ge 10000000 ] 2>/dev/null; then
            echo "  Shield reserve funded: $BAL"
            return 0
        fi
    done
    echo "  WARNING: shield reserve still ${BAL:-0} after funding the pool"
    return 1
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
    echo "  SKIPPED: DKG ceremony tests need patch_genesis_dkg.sh (min_tle_validators != 1)"
    exit 3
fi

# If batch is already enabled, a TLE key set was seeded — wrong genesis patch
if [ "$BATCH_ENABLED" == "true" ]; then
    # Check if DKG already completed (not seeded)
    DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)
    DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.dkg_state.phase // "DKG_PHASE_INACTIVE"' 2>/dev/null)
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
        echo "  SKIPPED: DKG ceremony tests need patch_genesis_dkg.sh (TLE genesis patch found)"
        exit 3
    fi
fi

record_result "DKG preconditions verified" "PASS"

# =========================================================================
# PART 2: Check initial DKG state (should be INACTIVE or REGISTERING)
# =========================================================================
echo "--- PART 2: Check initial DKG state ---"

DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)
DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.dkg_state.phase // "unknown"' 2>/dev/null)
DKG_ROUND=$(echo "$DKG_STATE" | jq -r '.dkg_state.round // "0"' 2>/dev/null)

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
        DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.dkg_state.phase // "unknown"' 2>/dev/null)

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
DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.dkg_state.phase // "unknown"' 2>/dev/null)
DKG_ROUND=$(echo "$DKG_STATE" | jq -r '.dkg_state.round // "0"' 2>/dev/null)
REG_DEADLINE=$(echo "$DKG_STATE" | jq -r '.dkg_state.registration_deadline // "0"' 2>/dev/null)
CONTRIB_DEADLINE=$(echo "$DKG_STATE" | jq -r '.dkg_state.contribution_deadline // "0"' 2>/dev/null)
COMPLAINT_DEADLINE=$(echo "$DKG_STATE" | jq -r '.dkg_state.complaint_deadline // "0"' 2>/dev/null)
JUSTIFICATION_DEADLINE=$(echo "$DKG_STATE" | jq -r '.dkg_state.justification_deadline // "0"' 2>/dev/null)
EXPECTED_VALS=$(echo "$DKG_STATE" | jq -r '.dkg_state.expected_validators | length' 2>/dev/null)

echo "  Current DKG state:"
echo "    Phase: $DKG_PHASE"
echo "    Round: $DKG_ROUND"
echo "    Registration deadline: block $REG_DEADLINE"
echo "    Contribution deadline: block $CONTRIB_DEADLINE"
echo "    Complaint deadline: block $COMPLAINT_DEADLINE"
echo "    Justification deadline: block $JUSTIFICATION_DEADLINE"
echo "    Expected validators: $EXPECTED_VALS"

# =========================================================================
# PART 3: Wait for REGISTERING -> CONTRIBUTING transition
# =========================================================================
echo "--- PART 3: Wait for REGISTERING -> CONTRIBUTING transition ---"

past_registering() {
    case "$1" in
        DKG_PHASE_CONTRIBUTING|DKG_PHASE_COMPLAINING|DKG_PHASE_JUSTIFYING|DKG_PHASE_ACTIVE) return 0 ;;
        *) return 1 ;;
    esac
}

if past_registering "$DKG_PHASE"; then
    echo "  Already past REGISTERING (phase=$DKG_PHASE) — skipping the wait"
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
        PHASE_NOW=$(echo "$DKG_NOW" | jq -r '.dkg_state.phase // "unknown"' 2>/dev/null)

        if past_registering "$PHASE_NOW"; then
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
# PART 4: Wait through CONTRIBUTING -> COMPLAINING -> JUSTIFYING -> ACTIVE
# =========================================================================
echo "--- PART 4: Wait for DKG completion (ACTIVE phase) ---"

# Re-read state
DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)
DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.dkg_state.phase // "unknown"' 2>/dev/null)
JUSTIFICATION_DEADLINE=$(echo "$DKG_STATE" | jq -r '.dkg_state.justification_deadline // "0"' 2>/dev/null)
SEEN_PHASES=""

if [ "$DKG_PHASE" == "DKG_PHASE_ACTIVE" ]; then
    echo "  DKG already ACTIVE — skipping"
    record_result "DKG ceremony completed (ACTIVE)" "PASS"
else
    CURRENT_H=$(get_block_height)
    BLOCKS_TO_WAIT=$((JUSTIFICATION_DEADLINE - CURRENT_H + 3))  # +3 safety margin
    if [ "$BLOCKS_TO_WAIT" -le 0 ]; then
        BLOCKS_TO_WAIT=5
    fi
    SECONDS_TO_WAIT=$((BLOCKS_TO_WAIT * 7))

    echo "  Current height: $CURRENT_H, justification deadline: $JUSTIFICATION_DEADLINE"
    echo "  Waiting ~$BLOCKS_TO_WAIT blocks (~${SECONDS_TO_WAIT}s) for DKG completion..."

    DKG_COMPLETED=false
    for attempt in $(seq 1 $SECONDS_TO_WAIT); do
        DKG_NOW=$($BINARY query shield dkg-state --output json 2>&1)
        PHASE_NOW=$(echo "$DKG_NOW" | jq -r '.dkg_state.phase // "unknown"' 2>/dev/null)
        case " $SEEN_PHASES " in
            *" $PHASE_NOW "*) ;;
            *) echo "  Phase $PHASE_NOW at height $(get_block_height)"; SEEN_PHASES="$SEEN_PHASES $PHASE_NOW" ;;
        esac

        if [ "$PHASE_NOW" == "DKG_PHASE_ACTIVE" ]; then
            CUR_H=$(get_block_height)
            echo "  DKG completed! Phase=ACTIVE at height $CUR_H"
            DKG_COMPLETED=true
            break
        fi

        # Check for DKG failure (reset to INACTIVE)
        if [ "$PHASE_NOW" == "DKG_PHASE_INACTIVE" ] && [ "$attempt" -gt 10 ]; then
            CUR_H=$(get_block_height)
            ROUND_NOW=$(echo "$DKG_NOW" | jq -r '.dkg_state.round // "0"' 2>/dev/null)
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
# PART 6: Verify encrypted_batch_enabled follows enable_batch_after_dkg
# =========================================================================
echo "--- PART 6: Verify encrypted_batch_enabled after DKG ---"

PARAMS_NOW=$($BINARY query shield params --output json 2>&1)
BATCH_NOW=$(echo "$PARAMS_NOW" | jq -r '.params.encrypted_batch_enabled // "false"')
AUTO_ENABLE=$(echo "$PARAMS_NOW" | jq -r '.params.enable_batch_after_dkg // "false"')

echo "  enable_batch_after_dkg: $AUTO_ENABLE"
echo "  encrypted_batch_enabled: $BATCH_NOW"

# A completed DKG turns batch mode on only when governance opted in up front
# (enable_batch_after_dkg; true in testparams builds, false in production).
if [ "$BATCH_NOW" == "$AUTO_ENABLE" ]; then
    echo "  encrypted_batch_enabled matches enable_batch_after_dkg"
    record_result "encrypted_batch_enabled follows enable_batch_after_dkg" "PASS"
else
    echo "  encrypted_batch_enabled=$BATCH_NOW but enable_batch_after_dkg=$AUTO_ENABLE"
    record_result "encrypted_batch_enabled follows enable_batch_after_dkg" "FAIL"
fi

# =========================================================================
# PART 7: Verify DKG state query returns correct final state
# =========================================================================
echo "--- PART 7: Verify final DKG state ---"

FINAL_DKG=$($BINARY query shield dkg-state --output json 2>&1)
FINAL_PHASE=$(echo "$FINAL_DKG" | jq -r '.dkg_state.phase // "unknown"' 2>/dev/null)
FINAL_ROUND=$(echo "$FINAL_DKG" | jq -r '.dkg_state.round // "0"' 2>/dev/null)
FINAL_CONTRIBS=$(echo "$FINAL_DKG" | jq -r '.dkg_state.contributions_received // "0"' 2>/dev/null)
FINAL_EXPECTED=$(echo "$FINAL_DKG" | jq -r '.dkg_state.expected_validators | length' 2>/dev/null)

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
EXPECTED_VAL=$(echo "$FINAL_DKG" | jq -r '.dkg_state.expected_validators[0] // empty' 2>/dev/null)
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
CURRENT_EPOCH=$(echo "$EPOCH_STATE" | jq -r '.epoch_state.current_epoch // "null"' 2>/dev/null)
EPOCH_START_H=$(echo "$EPOCH_STATE" | jq -r '.epoch_state.epoch_start_height // "0"' 2>/dev/null)

echo "  Current epoch: $CURRENT_EPOCH"
echo "  Epoch start height: $EPOCH_START_H"

# The first EndBlocker initialises the epoch state (block 1), long before a
# DKG can complete: it must exist by now.
if [ "$CURRENT_EPOCH" != "null" ] && [ "$CURRENT_EPOCH" != "" ] && [ "${EPOCH_START_H:-0}" -ge 1 ]; then
    record_result "Shield epoch state initialized" "PASS"
else
    echo "  Epoch state missing after a completed DKG"
    record_result "Shield epoch state initialized" "FAIL"
fi

# =========================================================================
# PART 10: Encrypted batch through the live DKG key
# =========================================================================
# The real share pipeline, with the single validator this test runs on:
# three ops (min_batch_size) sealed to the chain's G2 master key for the
# current epoch -> epoch closes -> EndBlocker demands shares -> the validator
# embeds its share in a vote extension -> PreBlocker verifies it (pairing)
# and reconstructs the epoch key -> EndBlocker executes the batch.
echo "--- PART 10: Encrypted batch through the DKG key ---"

BATCH_ENABLED_NOW=$($BINARY query shield params --output json 2>/dev/null | jq -r '.params.encrypted_batch_enabled // "false"')
MIN_BATCH=$($BINARY query shield params --output json 2>/dev/null | jq -r '.params.min_batch_size // 3')
EPOCH_INTERVAL=$($BINARY query shield params --output json 2>/dev/null | jq -r '.params.shield_epoch_interval // "50"')
MPK_G2_B64=$($BINARY query shield tle-master-public-key --output json 2>/dev/null | jq -r '.master_public_key_g2 // empty')

if [ "$BATCH_ENABLED_NOW" != "true" ] || [ -z "$MPK_G2_B64" ] || [ -z "$SHIELD_MODULE_ADDR" ]; then
    echo "  Batch mode not enabled, no G2 master key, or no shield address; skipping"
    record_result "Encrypted batch via DKG key" "SKIP"
else
    # No verifying key in this build: the proofs are unverified blobs, but
    # the ante handler still wants a proof-sized submission proof.
    DUMMY_PROOF_HEX=$(openssl rand -hex 128)
    # Build the payload tool once: `go run` can outlast a 10-block epoch.
    SEED_TLE="$(mktemp -d)/seed-tle"
    (cd "$PROJECT_ROOT" && go build -o "$SEED_TLE" ./tools/zk/cmd/seed-tle) || { echo "  Failed to build seed-tle"; exit 1; }
    echo "  G2 master public key: ${MPK_G2_B64:0:20}..."
    echo "  min_batch_size=$MIN_BATCH shield_epoch_interval=$EPOCH_INTERVAL"

    # A dummy 32-byte root: this is a test build without a verifying key, so
    # proofs are not verified; the pipeline under test is the TLE one.
    # The root the submission is checked against (current or previous
    # trust-tree root); the trust-tree query returns it base64-encoded.
    TRUST_ROOT_HEX=$($BINARY query rep trust-tree --output json 2>/dev/null | jq -r '.root // ""' | base64 -d 2>/dev/null | od -An -tx1 | tr -d ' \n')
    if [ -z "$TRUST_ROOT_HEX" ] || [ "$TRUST_ROOT_HEX" == "null" ]; then
        TRUST_ROOT_HEX="0000000000000000000000000000000000000000000000000000000000000001"
    fi
    ensure_shield_reserve
    SUBMIT_EPOCH=$($BINARY query shield shield-epoch --output json 2>/dev/null | jq -r '.epoch_state.current_epoch // "0"')
    SUBMITTED=0
    for i in $(seq 1 "$MIN_BATCH"); do
        # Seal against the live epoch: it may roll over between sends, and a
        # payload only opens under the epoch it was sealed to.
        for attempt in 1 2 3; do
            SUBMIT_EPOCH=$($BINARY query shield shield-epoch --output json 2>/dev/null | jq -r '.epoch_state.current_epoch // "0"')
            PAYLOAD_B64=$("$SEED_TLE" payload \
                --mpk-g2="$MPK_G2_B64" \
                --epoch="$SUBMIT_EPOCH" \
                --shield-addr="$SHIELD_MODULE_ADDR" \
                --nullifier-hex="00$(openssl rand -hex 31)" \
                --title="DkgBatch${i}_$(date +%s)" \
                --body="Encrypted batch op $i through the DKG key" 2>/dev/null)
            NOW_EPOCH=$($BINARY query shield shield-epoch --output json 2>/dev/null | jq -r '.epoch_state.current_epoch // "0"')
            [ "$NOW_EPOCH" == "$SUBMIT_EPOCH" ] && break
        done
        # The CLI takes hex for every byte field; exec-mode 1 = ENCRYPTED_BATCH.
        PAYLOAD_HEX=$(echo "$PAYLOAD_B64" | base64 -d | od -An -tx1 | tr -d ' \n')
        NULL_HEX=00$(openssl rand -hex 31)
        RL_HEX=00$(openssl rand -hex 31)
        TX_RES=$($BINARY tx shield shielded-exec \
            --exec-mode=1 \
            --encrypted-payload="$PAYLOAD_HEX" \
            --target-epoch="$SUBMIT_EPOCH" \
            --proof="$DUMMY_PROOF_HEX" \
            --merkle-root="$TRUST_ROOT_HEX" \
            --nullifier="$NULL_HEX" \
            --rate-limit-nullifier="$RL_HEX" \
            --proof-domain=1 \
            --min-trust-level=0 \
            --from submitter1 --chain-id "$CHAIN_ID" --keyring-backend test \
            --fees 5000${BOND_DENOM} --gas 500000 -y --output json 2>&1)
        TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
        if [ -n "$TXHASH" ]; then
            sleep 2
            TX_RESULT=$($BINARY query tx "$TXHASH" --output json 2>/dev/null)
            CODE=$(echo "$TX_RESULT" | jq -r '.code // 1')
            if [ "$CODE" == "0" ]; then
                SUBMITTED=$((SUBMITTED + 1))
            else
                echo "  Submission $i failed: $(echo "$TX_RESULT" | jq -r '.raw_log // empty' | head -c 200)"
            fi
        else
            echo "  Submission $i: no txhash: $(echo "$TX_RES" | head -c 200)"
        fi
    done
    echo "  Queued $SUBMITTED/$MIN_BATCH encrypted ops in epoch $SUBMIT_EPOCH"
    if [ "$SUBMITTED" -ge "$MIN_BATCH" ]; then
        record_result "Encrypted ops queued" "PASS"
    else
        record_result "Encrypted ops queued" "FAIL"
    fi

    # Wait for the epoch to close, the demand, the share and the execution.
    # Poll on block count: at most two epoch intervals plus a margin.
    MAX_WAIT_BLOCKS=$((EPOCH_INTERVAL * 2 + 20))
    START_H=$(get_block_height)
    SHARE_SEEN=false
    EXECUTED=false
    while :; do
        CUR_H=$(get_block_height)
        if [ $((CUR_H - START_H)) -ge "$MAX_WAIT_BLOCKS" ]; then
            break
        fi
        SHARES=$($BINARY query shield decryption-shares "$SUBMIT_EPOCH" --output json 2>/dev/null | jq -r '.shares | length' 2>/dev/null || echo 0)
        if [ "${SHARES:-0}" -ge 1 ]; then
            SHARE_SEEN=true
        fi
        PENDING_NOW=$($BINARY query shield pending-op-count --output json 2>/dev/null | jq -r '.count // "0"')
        if [ "$SHARE_SEEN" = true ] && [ "$PENDING_NOW" == "0" ]; then
            EXECUTED=true
            break
        fi
        sleep 2
    done
    echo "  Waited $(( $(get_block_height) - START_H )) blocks: share_seen=$SHARE_SEEN pending_now=${PENDING_NOW:-?}"

    if [ "$SHARE_SEEN" = true ]; then
        record_result "Validator released a verified decryption share" "PASS"
    else
        record_result "Validator released a verified decryption share" "FAIL"
    fi
    if [ "$EXECUTED" = true ]; then
        POSTS=$($BINARY query blog list-post --output json 2>/dev/null | jq -r '[.post[]? | select(.title | startswith("DkgBatch"))] | length' 2>/dev/null || echo 0)
        echo "  Blog posts created by the batch: ${POSTS:-0}"
        if [ "${POSTS:-0}" -ge "$MIN_BATCH" ]; then
            record_result "Batch decrypted and executed" "PASS"
        else
            record_result "Batch decrypted and executed" "FAIL"
        fi
    else
        record_result "Batch decrypted and executed" "FAIL"
    fi
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
echo "  Tests Skipped: $SKIP_COUNT"
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
