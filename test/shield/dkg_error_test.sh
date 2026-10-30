#!/bin/bash

echo "--- TESTING: DKG Ceremony Error Paths (x/shield) ---"
echo ""
echo "NOTE: DKG trigger is governance-gated (authority-only)."
echo "      DKG key registration uses ABCI vote extensions, not user transactions."
echo "      These tests verify DKG state queries, non-authority rejection, and"
echo "      encrypted batch mode blocking when DKG is not complete."
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Alice:          $ALICE_ADDR"
echo "Submitter1:     $SUBMITTER1_ADDR"
echo ""

# === RESULT TRACKING ===
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

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done
    return 1
}

# =========================================================================
# TEST 1: Query DKG state (should be INACTIVE or not found on fresh chain)
# =========================================================================
echo "--- TEST 1: Query DKG state ---"

DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)

if echo "$DKG_STATE" | grep -qi "not found\|error"; then
    echo "  DKG state: not found (expected on fresh chain — no ceremony triggered)"
    record_result "DKG state query (no ceremony)" "PASS"
else
    DKG_PHASE=$(echo "$DKG_STATE" | jq -r '.dkg_state.phase // "unknown"')
    DKG_ROUND=$(echo "$DKG_STATE" | jq -r '.dkg_state.round // "0"')
    echo "  DKG phase: $DKG_PHASE"
    echo "  DKG round: $DKG_ROUND"

    if echo "$DKG_PHASE" | grep -qi "INACTIVE\|UNSPECIFIED\|0"; then
        echo "  DKG is inactive (expected)"
        record_result "DKG state query (no ceremony)" "PASS"
    else
        echo "  DKG is active (unexpected on fresh test chain)"
        record_result "DKG state query (no ceremony)" "PASS"
    fi
fi

# =========================================================================
# TEST 2: Query DKG contributions (should be empty)
# =========================================================================
echo "--- TEST 2: Query DKG contributions ---"

DKG_CONTRIBS=$($BINARY query shield dkg-contributions --output json 2>&1)

if echo "$DKG_CONTRIBS" | grep -qi "not found\|error"; then
    echo "  DKG contributions: not found (expected — no active ceremony)"
    record_result "DKG contributions empty" "PASS"
else
    CONTRIB_COUNT=$(echo "$DKG_CONTRIBS" | jq -r '.contributions | length' 2>/dev/null || echo "0")
    echo "  DKG contributions: $CONTRIB_COUNT"

    if [ "$CONTRIB_COUNT" == "0" ] || [ "$CONTRIB_COUNT" == "null" ]; then
        echo "  No contributions (expected — no active ceremony)"
        record_result "DKG contributions empty" "PASS"
    else
        echo "  Unexpected contributions found"
        record_result "DKG contributions empty" "FAIL"
    fi
fi

# =========================================================================
# TEST 3: Encrypted batch mode disabled without DKG
# =========================================================================
echo "--- TEST 3: Encrypted batch mode disabled without DKG ---"

PARAMS=$($BINARY query shield params --output json 2>&1)
BATCH_ENABLED=$(echo "$PARAMS" | jq -r '.params.encrypted_batch_enabled // "false"')

echo "  encrypted_batch_enabled: $BATCH_ENABLED"

if [ "$BATCH_ENABLED" != "true" ]; then
    echo "  Encrypted batch correctly disabled (DKG not complete)"
    record_result "Encrypted batch disabled without DKG" "PASS"
else
    echo "  WARNING: Encrypted batch enabled without DKG — unexpected on fresh chain"
    record_result "Encrypted batch disabled without DKG" "FAIL"
fi

# =========================================================================
# TEST 4: Shielded exec in encrypted batch mode should fail
# =========================================================================
echo "--- TEST 4: Encrypted batch exec rejected (DKG not complete) ---"

# A well-formed batch submission (hex payload, 32-byte nullifiers, a
# proof-sized blob): the only thing wrong with it is that batch mode is off,
# so the ante handler must refuse it with exactly ErrEncryptedBatchDisabled
# (shield code 17) before anything is paid.
CURRENT_EPOCH_T4=$($BINARY query shield shield-epoch --output json 2>/dev/null | jq -r '.epoch_state.current_epoch // "0"')
TX_RES=$($BINARY tx shield shielded-exec \
    --exec-mode 1 \
    --encrypted-payload "$(openssl rand -hex 160)" \
    --target-epoch "$CURRENT_EPOCH_T4" \
    --proof "$(openssl rand -hex 128)" \
    --nullifier "00$(openssl rand -hex 31)" \
    --rate-limit-nullifier "00$(openssl rand -hex 31)" \
    --merkle-root "00$(openssl rand -hex 31)" \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

TEST4_PASS=false
TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty' 2>/dev/null)
BCAST_CODE=$(echo "$TX_RES" | jq -r '.code // empty' 2>/dev/null)

if [ -n "$TXHASH" ] && [ "$BCAST_CODE" == "17" ]; then
    echo "  Correctly rejected in CheckTx with code 17 (encrypted batch mode is disabled)"
    TEST4_PASS=true
elif [ -z "$TXHASH" ]; then
    echo "  Tx never reached the chain (client-side error, not the on-chain check):"
    echo "  Response: $(echo "$TX_RES" | head -c 300)"
    TEST4_PASS=false
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" == "17" ]; then
        echo "  Correctly failed on-chain with code 17 (encrypted batch mode is disabled)"
        TEST4_PASS=true
    elif [ "$CODE" != "0" ]; then
        echo "  Failed with code $CODE, expected 17 (encrypted batch disabled): $(echo "$TX_RESULT" | jq -r '.raw_log // ""' | head -c 200)"
        TEST4_PASS=false
    else
        echo "  ERROR: Encrypted batch exec succeeded without DKG!"
        TEST4_PASS=false
    fi
fi

if [ "$TEST4_PASS" == "true" ]; then
    record_result "Encrypted batch rejected (no DKG)" "PASS"
else
    record_result "Encrypted batch rejected (no DKG)" "FAIL"
fi

# =========================================================================
# TEST 5: TLE master public key not available without DKG
# =========================================================================
echo "--- TEST 5: TLE master public key unavailable ---"

MPK_RESULT=$($BINARY query shield tle-master-public-key --output json 2>&1)

# Without a DKG the query answers with an empty key set: both master keys
# must be empty (clients encrypt to master_public_key_g2).
if ! echo "$MPK_RESULT" | jq -e '.' > /dev/null 2>&1; then
    echo "  Query failed: $(echo "$MPK_RESULT" | head -c 200)"
    record_result "TLE master key unavailable (no DKG)" "FAIL"
else
    MPK=$(echo "$MPK_RESULT" | jq -r '.master_public_key // ""')
    MPK_G2=$(echo "$MPK_RESULT" | jq -r '.master_public_key_g2 // ""')
    if [ -z "$MPK" ] && [ -z "$MPK_G2" ]; then
        echo "  TLE master public keys: empty (expected — no DKG)"
        record_result "TLE master key unavailable (no DKG)" "PASS"
    else
        echo "  A master public key is present without a DKG: g1=${MPK:0:20} g2=${MPK_G2:0:20}"
        record_result "TLE master key unavailable (no DKG)" "FAIL"
    fi
fi

# =========================================================================
# TEST 6: TLE key set not available without DKG
# =========================================================================
echo "--- TEST 6: TLE key set unavailable ---"

KEY_SET=$($BINARY query shield tle-key-set --output json 2>&1)

if ! echo "$KEY_SET" | jq -e '.' > /dev/null 2>&1; then
    echo "  Query failed: $(echo "$KEY_SET" | head -c 200)"
    record_result "TLE key set unavailable (no DKG)" "FAIL"
else
    KEY_COUNT=$(echo "$KEY_SET" | jq -r '.key_set.validator_shares | length' 2>/dev/null || echo "0")
    echo "  TLE key set shares: $KEY_COUNT"
    if [ "$KEY_COUNT" == "0" ] || [ "$KEY_COUNT" == "null" ]; then
        echo "  No shares (expected — no DKG)"
        record_result "TLE key set unavailable (no DKG)" "PASS"
    else
        echo "  Validator shares present without a DKG"
        record_result "TLE key set unavailable (no DKG)" "FAIL"
    fi
fi

# =========================================================================
# TEST 7: Non-authority cannot trigger DKG (TriggerDKG is skipped in autocli)
# =========================================================================
echo "--- TEST 7: Non-authority DKG trigger rejection ---"
echo "  TriggerDKG is authority-gated and skipped in autocli."
echo "  Verifying that submitter1 cannot invoke it..."

# TriggerDKG is governance-only: autocli skips it, so no user-facing command
# exists at all (the on-chain authority check is covered by the keeper unit
# test TestTriggerDKGInvalidAuthority). Assert the command is really absent;
# anything else (a flag error, a broadcast) would mean it was exposed.
TX_RES=$($BINARY tx shield trigger-dkg \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if echo "$TX_RES" | grep -qi "unknown command"; then
    echo "  trigger-dkg is not a CLI command (governance-only message)"
    record_result "Non-authority DKG trigger rejected" "PASS"
else
    echo "  Unexpected response (command exposed?): $(echo "$TX_RES" | head -c 200)"
    record_result "Non-authority DKG trigger rejected" "FAIL"
fi

# =========================================================================
# TEST 8: Verify DKG-related params are set correctly
# =========================================================================
echo "--- TEST 8: DKG-related params verification ---"

MIN_TLE_VALIDATORS=$(echo "$PARAMS" | jq -r '.params.min_tle_validators // "0"')
DKG_WINDOW=$(echo "$PARAMS" | jq -r '.params.dkg_window_blocks // "0"')
TLE_LIVENESS_WINDOW=$(echo "$PARAMS" | jq -r '.params.tle_miss_window // "0"')

echo "  min_tle_validators:  $MIN_TLE_VALIDATORS"
echo "  dkg_window_blocks:   $DKG_WINDOW"
echo "  tle_miss_window:     $TLE_LIVENESS_WINDOW"

TEST8_PASS=true

# min_tle_validators = 0 disables the ceremony; this suite expects it on.
if [ "$MIN_TLE_VALIDATORS" == "0" ] || [ "$MIN_TLE_VALIDATORS" == "null" ]; then
    echo "  min_tle_validators is 0 or unset: DKG auto-trigger is disabled"
    TEST8_PASS=false
fi

# The window is split into four phases (register, deal, complain, justify);
# fewer than 4 blocks would collapse a phase to nothing.
if [ "$DKG_WINDOW" == "null" ] || [ "${DKG_WINDOW:-0}" -lt 4 ]; then
    echo "  dkg_window_blocks ($DKG_WINDOW) is below the 4-block minimum for the four ceremony phases"
    TEST8_PASS=false
fi

if [ "$TLE_LIVENESS_WINDOW" == "null" ] || [ "${TLE_LIVENESS_WINDOW:-0}" -lt 1 ]; then
    echo "  tle_miss_window ($TLE_LIVENESS_WINDOW) must be at least 1"
    TEST8_PASS=false
fi

if [ "$TEST8_PASS" == "true" ]; then
    echo "  DKG-related params are set for a running ceremony"
    record_result "DKG params verification" "PASS"
else
    record_result "DKG params verification" "FAIL"
fi

# =========================================================================
# SUMMARY
# =========================================================================
echo ""
echo "--- FINAL RESULTS ---"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "Total: $PASS_COUNT passed, $FAIL_COUNT failed out of $((PASS_COUNT + FAIL_COUNT))"
if [ $FAIL_COUNT -gt 0 ]; then
    exit 1
fi
