#!/bin/bash

echo "--- TESTING: STAKING ERROR PATHS (SELF-STAKE, CIRCULAR, NON-MEMBER, UNSTAKE ERRORS) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo "Alice:      $ALICE_ADDR"
echo "Bob:        $BOB_ADDR"
echo "Challenger: $CHALLENGER_ADDR"
echo "Assignee:   $ASSIGNEE_ADDR"
echo ""

# ========================================================================
# Result Tracking & Helpers
# ========================================================================
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

submit_tx_and_wait() {
    local TX_RES="$1"

    # If the broadcast didn't return parseable JSON (e.g. CLI printed a
    # plain-text validation error), treat the response itself as the result.
    # This avoids noisy `jq: parse error` output on expected-rejection paths.
    if ! echo "$TX_RES" | jq -e . > /dev/null 2>&1; then
        TX_RESULT="$TX_RES"
        return 1
    fi

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    local BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

check_tx_success() {
    local TX_RESULT=$1
    if ! echo "$TX_RESULT" | jq -e . > /dev/null 2>&1; then
        return 1
    fi
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    [ "$CODE" == "0" ]
}

expect_tx_failure() {
    local TX_RES="$1"
    local EXPECTED_ERR="$2"
    local TEST_NAME="$3"

    if ! submit_tx_and_wait "$TX_RES"; then
        if echo "$TX_RES" | grep -qi "$EXPECTED_ERR"; then
            record_result "$TEST_NAME" "PASS"
        else
            echo "  Broadcast rejection did not contain expected error: $EXPECTED_ERR"
            echo "  Response: $(echo "$TX_RES" | head -c 300)"
            record_result "$TEST_NAME" "FAIL"
        fi
        return
    fi

    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        local RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "$EXPECTED_ERR"; then
            echo "  Failed as expected (code: $CODE)"
            record_result "$TEST_NAME" "PASS"
        else
            echo "  Failed but unexpected error: $RAW_LOG"
            echo "  Expected: $EXPECTED_ERR"
            record_result "$TEST_NAME" "FAIL"
        fi
    else
        echo "  ERROR: Transaction succeeded when it should have failed!"
        record_result "$TEST_NAME" "FAIL"
    fi
}

# ========================================================================
# TEST 1: Self-member-stake prevention (ErrSelfMemberStake)
# ========================================================================
echo "--- TEST 1: Self-member-stake prevention ---"
echo "  Alice stakes on herself (target_type=member)..."

# target-type=member, target-id=0, target-identifier=self, amount=1000
TX_RES=$($BINARY tx rep stake \
    "stake-target-member" 0 "1000000" \
    --target-identifier "$ALICE_ADDR" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "cannot stake on yourself" "Self-member-stake prevention"

# ========================================================================
# TEST 2: Circular member stake (ErrCircularMemberStake)
# ========================================================================
echo "--- TEST 2: Circular member stake ---"
echo "  First: Alice stakes on Bob..."

# Alice → Bob (should succeed) — use minimal amount to avoid balance issues
TX_RES=$($BINARY tx rep stake \
    "stake-target-member" 0 "100" \
    --target-identifier "$BOB_ADDR" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

ALICE_BOB_STAKED=false
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    echo "  Alice → Bob stake succeeded"
    ALICE_BOB_STAKE_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="stake_created") | .attributes[] | select(.key=="stake_id") | .value' | tr -d '"' | head -1)
    echo "  Stake ID: $ALICE_BOB_STAKE_ID"
    ALICE_BOB_STAKED=true
else
    # Check if Alice already has a stake on Bob from a previous run
    EXISTING_STAKES=$($BINARY query rep stakes-by-staker $ALICE_ADDR --output json 2>&1)
    EXISTING_ALICE_BOB=$(echo "$EXISTING_STAKES" | jq -r '.stakes[]? | select(.target_identifier=="'"$BOB_ADDR"'") | .id' 2>/dev/null | head -1)
    if [ -n "$EXISTING_ALICE_BOB" ] && [ "$EXISTING_ALICE_BOB" != "null" ]; then
        echo "  Alice already has a stake on Bob (stake ID: $EXISTING_ALICE_BOB)"
        ALICE_BOB_STAKED=true
        ALICE_BOB_STAKE_ID="$EXISTING_ALICE_BOB"
    else
        echo "  Alice → Bob stake failed: $(echo "$TX_RESULT" | jq -r '.raw_log' | head -c 200)"
    fi
fi

if [ "$ALICE_BOB_STAKED" = true ]; then
    echo "  Now: Bob stakes on Alice (should fail — circular)..."

    TX_RES=$($BINARY tx rep stake \
        "stake-target-member" 0 "100" \
        --target-identifier "$ALICE_ADDR" \
        --from bob \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "circular" "Circular member stake"
else
    echo "  Skipped (could not establish Alice→Bob stake prerequisite)"
    record_result "Circular member stake" "FAIL"
fi

# Cleanup: remove the Alice→Bob stake if we created it
if [ -n "$ALICE_BOB_STAKE_ID" ] && [ "$ALICE_BOB_STAKE_ID" != "null" ]; then
    echo "  Cleaning up: removing Alice→Bob stake..."
    TX_RES=$($BINARY tx rep unstake "$ALICE_BOB_STAKE_ID" "100" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
fi

# ========================================================================
# TEST 3: Non-member stake attempt
# ========================================================================
echo "--- TEST 3: Non-member stake attempt ---"

# Check if dave exists and has funds but is NOT a member
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test 2>/dev/null)

if [ -n "$DAVE_ADDR" ]; then
    echo "  Dave ($DAVE_ADDR) is not a member, attempting to stake..."

    TX_RES=$($BINARY tx rep stake \
        "stake-target-tag" 0 "1000000" \
        --target-identifier "backend" \
        --from dave \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "not a member\|member not found\|not found" "Non-member stake attempt"
else
    echo "  Dave account not available, skipping"
    record_result "Non-member stake attempt" "FAIL"
fi

# ========================================================================
# TEST 4: Unstake non-existent stake (ErrStakeNotFound)
# ========================================================================
echo "--- TEST 4: Unstake non-existent stake ---"

TX_RES=$($BINARY tx rep unstake \
    "99999" "1000000" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "not found" "Unstake non-existent stake"

# ========================================================================
# TEST 5: Invalid stake target type
# ========================================================================
echo "--- TEST 5: Invalid stake target type ---"

TX_RES=$($BINARY tx rep stake \
    "999" 0 "1000000" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

# This may fail at CLI parsing or at handler level
if echo "$TX_RES" | grep -qi "invalid\|unknown\|error"; then
    echo "  Rejected with invalid target type"
    record_result "Invalid stake target type" "PASS"
else
    expect_tx_failure "$TX_RES" "invalid" "Invalid stake target type"
fi

# ========================================================================
# TEST 6: DREAM transfer to self (ErrCannotTransferToSelf)
# ========================================================================
echo "--- TEST 6: DREAM transfer to self ---"

TX_RES=$($BINARY tx rep transfer-dream \
    "$ALICE_ADDR" "1000000" "tip" "self-tip test" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "cannot transfer to self" "DREAM transfer to self"

# ========================================================================
# TEST 7: DREAM transfer invalid purpose
# ========================================================================
echo "--- TEST 7: DREAM transfer invalid purpose ---"

TX_RES=$($BINARY tx rep transfer-dream \
    "$BOB_ADDR" "1000000" "invalid_purpose" "bad purpose test" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "invalid transfer purpose\|invalid\|unknown" "DREAM transfer invalid purpose"

# ========================================================================
# TEST 8: Claim staking rewards before min_stake_duration_seconds
# ========================================================================
# Rewards are only collectable after min_stake_duration_seconds (24h by
# default). Rejecting an early claim forfeits nothing — reward_debt is left
# untouched so the rewards keep accruing until the staker is eligible.
echo "--- TEST 8: Claim rewards before minimum stake duration ---"

MIN_DURATION=$($BINARY query rep params --output json 2>/dev/null | jq -r '.params.min_stake_duration_seconds // "0"')
echo "  min_stake_duration_seconds: $MIN_DURATION"

if [ "$MIN_DURATION" = "0" ]; then
    echo "  [WARN] min_stake_duration_seconds is 0 on this chain; early-claim gate is disabled"
    echo "  Skipped (params make this unreachable)"
else
    TX_RES=$($BINARY tx rep stake \
        "stake-target-member" 0 "100" \
        --target-identifier "$BOB_ADDR" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    EARLY_CLAIM_STAKE_ID=""
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EARLY_CLAIM_STAKE_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="stake_created") | .attributes[] | select(.key=="stake_id") | .value' | tr -d '"' | head -1)
    fi

    if [ -n "$EARLY_CLAIM_STAKE_ID" ] && [ "$EARLY_CLAIM_STAKE_ID" != "null" ]; then
        echo "  Stake ID: $EARLY_CLAIM_STAKE_ID (created this block, well inside the minimum)"
        TX_RES=$($BINARY tx rep claim-staking-rewards \
            --stake-id "$EARLY_CLAIM_STAKE_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)

        expect_tx_failure "$TX_RES" "minimum stake duration" "Claim before minimum stake duration"

        # Cleanup: principal is always returnable, only the rewards are forfeited.
        TX_RES=$($BINARY tx rep unstake "$EARLY_CLAIM_STAKE_ID" "100" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)
        submit_tx_and_wait "$TX_RES"
    else
        echo "  [WARN] Could not create the member stake prerequisite"
        record_result "Claim before minimum stake duration" "FAIL"
    fi
fi

# ========================================================================
# TEST 9: Compound an initiative stake (only member/tag stakes may compound)
# ========================================================================
# Growing an initiative stake's principal in place would give the added DREAM
# the conviction maturity of the original created_at — the exploit that separate
# stake tranches exist to prevent. Those stakers claim and re-stake instead.
echo "--- TEST 9: Compound an initiative stake ---"

ERR_INIT_ID=$($BINARY query rep list-initiative --output json 2>/dev/null | jq -r '.initiative[-1].id // ""')

if [ -z "$ERR_INIT_ID" ] || [ "$ERR_INIT_ID" = "null" ]; then
    echo "  [WARN] No initiative available on this chain; skipping"
else
    echo "  Using initiative #$ERR_INIT_ID"
    TX_RES=$($BINARY tx rep stake \
        "stake-target-initiative" "$ERR_INIT_ID" "100" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    COMPOUND_STAKE_ID=""
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        COMPOUND_STAKE_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="stake_created") | .attributes[] | select(.key=="stake_id") | .value' | tr -d '"' | head -1)
    fi

    if [ -n "$COMPOUND_STAKE_ID" ] && [ "$COMPOUND_STAKE_ID" != "null" ]; then
        TX_RES=$($BINARY tx rep compound-staking-rewards \
            --stake-id "$COMPOUND_STAKE_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)

        expect_tx_failure "$TX_RES" "compounding is not supported" "Compound an initiative stake"

        TX_RES=$($BINARY tx rep unstake "$COMPOUND_STAKE_ID" "100" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)
        submit_tx_and_wait "$TX_RES"
    else
        echo "  [WARN] Could not create the initiative stake prerequisite"
        record_result "Compound an initiative stake" "FAIL"
    fi
fi

# ========================================================================
# TEST 10: Per-target stake tranche cap
# ========================================================================
# Stakes are never merged (each keeps its own maturity clock and reward-debt
# baseline), so the record count is bounded instead. Without the cap, a member
# could impose permanent per-block conviction work for a refundable cost.
echo "--- TEST 10: Per-target stake tranche cap ---"

TRANCHE_INIT_ID=$($BINARY query rep list-initiative --output json 2>/dev/null | jq -r '.initiative[-1].id // ""')

if [ -z "$TRANCHE_INIT_ID" ] || [ "$TRANCHE_INIT_ID" = "null" ]; then
    echo "  [WARN] No initiative available on this chain; skipping"
else
    # Count what bob already holds on this target so the loop tops it up to the
    # cap rather than assuming a clean slate.
    # target-type 0 = STAKE_TARGET_INITIATIVE (this query takes the numeric enum)
    EXISTING_TRANCHES=$($BINARY query rep stakes-by-target 0 "$TRANCHE_INIT_ID" --output json 2>/dev/null \
        | jq -r --arg s "$BOB_ADDR" '[.stakes[]? | select(.staker==$s)] | length' 2>/dev/null)
    EXISTING_TRANCHES=${EXISTING_TRANCHES:-0}
    echo "  Bob already holds $EXISTING_TRANCHES tranche(s) on initiative #$TRANCHE_INIT_ID"

    TRANCHE_CAP=10   # types.MaxStakeTranchesPerTarget
    TRANCHE_IDS=()
    FILL_OK=true
    for ((i=EXISTING_TRANCHES; i<TRANCHE_CAP; i++)); do
        TX_RES=$($BINARY tx rep stake \
            "stake-target-initiative" "$TRANCHE_INIT_ID" "100" \
            --from bob \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)
        if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
            TID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="stake_created") | .attributes[] | select(.key=="stake_id") | .value' | tr -d '"' | head -1)
            [ -n "$TID" ] && [ "$TID" != "null" ] && TRANCHE_IDS+=("$TID")
        else
            echo "  [WARN] Could not fill tranche $i: $(echo "$TX_RESULT" | jq -r '.raw_log // empty' | head -c 200)"
            FILL_OK=false
            break
        fi
    done

    if [ "$FILL_OK" = true ]; then
        echo "  Cap reached; the next stake on this target must be rejected"
        TX_RES=$($BINARY tx rep stake \
            "stake-target-initiative" "$TRANCHE_INIT_ID" "100" \
            --from bob \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)

        expect_tx_failure "$TX_RES" "max number of separate stakes" "Per-target stake tranche cap"
    else
        echo "  Skipped (could not reach the tranche cap)"
    fi

    # Cleanup: return every tranche this test created.
    for TID in "${TRANCHE_IDS[@]}"; do
        TX_RES=$($BINARY tx rep unstake "$TID" "100" \
            --from bob \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)
        submit_tx_and_wait "$TX_RES" > /dev/null 2>&1
    done
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================================================"
echo "  REP STAKING ERRORS TEST SUMMARY"
echo "============================================================================"
echo ""
echo "  Tests Run:    $((PASS_COUNT + FAIL_COUNT))"
echo "  Tests Passed: $PASS_COUNT"
echo "  Tests Failed: $FAIL_COUNT"
echo ""

for i in "${!TEST_NAMES[@]}"; do
    echo "  ${RESULTS[$i]}: ${TEST_NAMES[$i]}"
done

echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
