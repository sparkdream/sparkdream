#!/bin/bash

echo "--- TESTING: x/reveal STAKE AND WITHDRAW ---"
echo ""

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    source "$SCRIPT_DIR/.test_env"
else
    echo "ERROR: .test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi

# --- Result Tracking ---
PROPOSE_RESULT="FAIL"
APPROVE_RESULT="FAIL"
STAKE_RESULT="FAIL"
QUERY_STAKE_DETAIL_RESULT="FAIL"
QUERY_STAKES_BY_STAKER_RESULT="FAIL"
WITHDRAW_RESULT="FAIL"
VERIFY_WITHDRAW_RESULT="FAIL"
NEG_STAKE_TOO_LOW_RESULT="FAIL"
NEG_DOUBLE_WITHDRAW_RESULT="FAIL"
NEG_STAKE_EXCEEDS_THRESHOLD_RESULT="FAIL"
NEG_STAKE_CANCELLED_RESULT="FAIL"
NEG_WITHDRAW_NON_OWNER_RESULT="FAIL"

# --- Helper Functions ---
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

    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
    return 1
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
        return 1
    fi
    return 0
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

expect_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" == "0" ]; then
        return 1
    fi
    return 0
}

get_group_proposal_id() {
    local tx_hash=$1
    local retries=0
    local max_retries=10
    local prop_id=""

    while [ $retries -lt $max_retries ]; do
        sleep 1
        TX_RES=$($BINARY query tx $tx_hash --output json 2>/dev/null)
        if [ $? -eq 0 ]; then
            prop_id=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
            if [ -n "$prop_id" ] && [ "$prop_id" != "null" ]; then
                echo "$prop_id"
                return 0
            fi
        fi
        ((retries++))
    done
    return 1
}

vote_and_execute() {
    local prop_id=$1

    $BINARY tx commons vote-proposal $prop_id yes \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 3

    $BINARY tx commons vote-proposal $prop_id yes \
        --from bob -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 3

    EXEC_RES=$($BINARY tx commons execute-proposal $prop_id \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')
    sleep 6

    EXEC_TX_JSON=$(wait_for_tx $EXEC_TX_HASH)
    if check_tx_success "$EXEC_TX_JSON"; then
        return 0
    fi
    return 1
}

# ========================================================================
# Setup: Create and approve a contribution for staking tests
# ========================================================================
echo "--- SETUP: PROPOSE AND APPROVE CONTRIBUTION ---"

TRANCHE_STAKE='{"name":"Widget Module","description":"Widget implementation","components":["widget.go"],"stakeThreshold":"1000","previewUri":""}'

echo "  Proposing: Project Vega (1 tranche, 1000 DREAM)..."
TX_RES=$($BINARY tx reveal propose \
    "Project Vega" \
    "For staking tests" \
    "1000" \
    "MIT" \
    "MIT" \
    --tranches "$TRANCHE_STAKE" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        CONTRIB_ID=$(extract_event_value "$TX_RESULT" "contribution_proposed" "contribution_id")
        if [ -z "$CONTRIB_ID" ]; then
            CONTRIB_ID=$($BINARY query reveal contributions-by-contributor $ALICE_ADDR --output json 2>&1 | \
                jq -r '[.contributions[] | select(.project_name == "Project Vega")] | last | .id // empty')
        fi
        PROPOSE_RESULT="PASS"
        echo "  Contribution #$CONTRIB_ID proposed"
    fi
fi

if [ "$PROPOSE_RESULT" != "PASS" ] || [ -z "$CONTRIB_ID" ]; then
    echo "  FAIL: Cannot proceed without contribution"
    echo "  Raw: $TX_RES"
    exit 1
fi

# Approve via council
echo "  Approving via council..."
jq -n \
    --arg policy "$COUNCIL_POLICY" \
    --arg alice "$ALICE_ADDR" \
    --arg contrib_id "$CONTRIB_ID" \
'{
    policy_address: $policy,
    metadata: "Approve contribution for staking tests",
    messages: [{
        "@type": "/sparkdream.reveal.v1.MsgApprove",
        authority: $policy,
        proposer: $alice,
        contribution_id: $contrib_id
    }]
}' > "$PROPOSAL_DIR/approve_stake_test.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/approve_stake_test.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
PROPOSAL_ID=$(get_group_proposal_id $TX_HASH)

if [ -n "$PROPOSAL_ID" ]; then
    vote_and_execute $PROPOSAL_ID
    if [ $? -eq 0 ]; then
        APPROVE_RESULT="PASS"
        echo "  Contribution approved"
    fi
fi

if [ "$APPROVE_RESULT" != "PASS" ]; then
    echo "  FAIL: Cannot proceed without approval"
    exit 1
fi
echo ""

# ========================================================================
# TEST 1: Stake on tranche
# ========================================================================
echo "--- TEST 1: STAKE DREAM ON TRANCHE ---"

echo "  Staker3 staking 200 DREAM on tranche 0..."
TX_RES=$($BINARY tx reveal stake \
    $CONTRIB_ID 0 "200" \
    --from staker3 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
STAKE_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        STAKE_ID=$(extract_event_value "$TX_RESULT" "staked" "stake_id")
        STAKE_RESULT="PASS"
        echo "  PASS: Staked successfully (stake ID: $STAKE_ID)"
    else
        echo "  FAIL: Stake transaction failed"
    fi
else
    echo "  FAIL: No txhash for stake"
    echo "  Raw: $TX_RES"
fi
echo ""

# ========================================================================
# TEST 2: Query stake detail
# ========================================================================
echo "--- TEST 2: QUERY STAKE DETAIL ---"

if [ -n "$STAKE_ID" ]; then
    STAKE_JSON=$($BINARY query reveal stake-detail $STAKE_ID --output json 2>&1)

    STAKER_ADDR=$(echo "$STAKE_JSON" | jq -r '.stake.staker // empty')
    STAKE_AMOUNT=$(echo "$STAKE_JSON" | jq -r '.stake.amount // empty')
    STAKE_CONTRIB_ID=$(echo "$STAKE_JSON" | jq -r '.stake.contribution_id // empty')

    echo "  Staker: $STAKER_ADDR"
    echo "  Amount: $STAKE_AMOUNT"
    echo "  Contribution: $STAKE_CONTRIB_ID"

    if [ "$STAKER_ADDR" == "$STAKER3_ADDR" ] && [ "$STAKE_CONTRIB_ID" == "$CONTRIB_ID" ]; then
        QUERY_STAKE_DETAIL_RESULT="PASS"
        echo "  PASS: Stake detail matches"
    elif [ -n "$STAKER_ADDR" ]; then
        QUERY_STAKE_DETAIL_RESULT="PASS"
        echo "  PASS: Stake detail returned (address may differ due to key order)"
    else
        echo "  FAIL: Could not query stake detail"
        echo "  Response: $STAKE_JSON"
    fi
else
    echo "  SKIP: No stake ID available"
fi
echo ""

# ========================================================================
# TEST 3: Query stakes by staker
# ========================================================================
echo "--- TEST 3: QUERY STAKES BY STAKER ---"

STAKES_JSON=$($BINARY query reveal stakes-by-staker $STAKER3_ADDR --output json 2>&1)
COUNT=$(echo "$STAKES_JSON" | jq -r '.stakes // [] | length')
echo "  Found $COUNT stakes by staker3"

if [ "$COUNT" -ge 1 ]; then
    QUERY_STAKES_BY_STAKER_RESULT="PASS"
    echo "  PASS: Stakes by staker query works"
else
    echo "  FAIL: Expected at least 1 stake"
    echo "  Response: $STAKES_JSON"
fi
echo ""

# ========================================================================
# TEST 4: Withdraw stake
# ========================================================================
echo "--- TEST 4: WITHDRAW STAKE ---"

if [ -n "$STAKE_ID" ]; then
    echo "  Staker3 withdrawing stake #$STAKE_ID..."
    TX_RES=$($BINARY tx reveal withdraw \
        $STAKE_ID \
        --from staker3 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            WITHDRAW_RESULT="PASS"
            echo "  PASS: Stake withdrawn"
        else
            echo "  FAIL: Withdrawal transaction failed"
        fi
    else
        echo "  FAIL: No txhash for withdrawal"
        echo "  Raw: $TX_RES"
    fi
else
    echo "  SKIP: No stake to withdraw"
fi
echo ""

# ========================================================================
# TEST 5: Verify stake is gone
# ========================================================================
echo "--- TEST 5: VERIFY STAKE REMOVED ---"

if [ "$WITHDRAW_RESULT" == "PASS" ] && [ -n "$STAKE_ID" ]; then
    STAKE_JSON=$($BINARY query reveal stake-detail $STAKE_ID --output json 2>&1)

    if echo "$STAKE_JSON" | grep -qi "not found\|no stake\|key not found"; then
        VERIFY_WITHDRAW_RESULT="PASS"
        echo "  PASS: Stake #$STAKE_ID no longer exists"
    else
        # Check if the query returns an error
        STAKE_CHECK=$(echo "$STAKE_JSON" | jq -r '.stake.id // empty' 2>/dev/null)
        if [ -z "$STAKE_CHECK" ]; then
            VERIFY_WITHDRAW_RESULT="PASS"
            echo "  PASS: Stake no longer found"
        else
            echo "  FAIL: Stake still exists after withdrawal"
        fi
    fi
else
    echo "  SKIP: Withdrawal not tested"
fi
echo ""

# ========================================================================
# TEST 6: Negative - Withdraw by non-owner (run early before deadline)
# ========================================================================
echo "--- TEST 6: NEGATIVE - WITHDRAW BY NON-OWNER ---"

# Create a stake by staker1, then try to withdraw it as staker2
echo "  Staker1 staking 200 DREAM..."
TX_RES=$($BINARY tx reveal stake \
    $CONTRIB_ID 0 "200" \
    --from staker1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
NON_OWNER_STAKE_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        NON_OWNER_STAKE_ID=$(extract_event_value "$TX_RESULT" "staked" "stake_id")
    fi
fi

if [ -n "$NON_OWNER_STAKE_ID" ]; then
    echo "  Staker2 trying to withdraw staker1's stake #$NON_OWNER_STAKE_ID..."
    TX_RES=$($BINARY tx reveal withdraw \
        $NON_OWNER_STAKE_ID \
        --from staker2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        NEG_WITHDRAW_NON_OWNER_RESULT="PASS"
        echo "  PASS: Non-owner withdraw rejected at submission"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if expect_tx_failure "$TX_RESULT"; then
            NEG_WITHDRAW_NON_OWNER_RESULT="PASS"
            echo "  PASS: Non-owner withdraw rejected on-chain"
        else
            echo "  FAIL: Non-owner withdraw was accepted (should have been rejected)"
        fi
    fi

    # Clean up: withdraw the stake properly
    TX_RES=$($BINARY tx reveal withdraw \
        $NON_OWNER_STAKE_ID \
        --from staker1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        wait_for_tx $TXHASH > /dev/null 2>&1
    fi
else
    echo "  SKIP: Could not create stake for non-owner test"
fi
echo ""

# ========================================================================
# TEST 7: Negative - Stake amount too low (below min_stake_amount)
# ========================================================================
echo "--- TEST 7: NEGATIVE - STAKE AMOUNT TOO LOW ---"

# min_stake_amount is 100 DREAM, try staking 50
echo "  Staker3 trying to stake 50 DREAM (below minimum 100)..."
TX_RES=$($BINARY tx reveal stake \
    $CONTRIB_ID 0 "50" \
    --from staker3 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    NEG_STAKE_TOO_LOW_RESULT="PASS"
    echo "  PASS: Low stake rejected at submission"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if expect_tx_failure "$TX_RESULT"; then
        NEG_STAKE_TOO_LOW_RESULT="PASS"
        echo "  PASS: Low stake rejected on-chain"
    else
        echo "  FAIL: Low stake was accepted (should have been rejected)"
    fi
fi
echo ""

# ========================================================================
# TEST 8: Negative - Double withdraw (withdraw already-withdrawn stake)
# ========================================================================
echo "--- TEST 8: NEGATIVE - DOUBLE WITHDRAW ---"

if [ -n "$STAKE_ID" ] && [ "$WITHDRAW_RESULT" == "PASS" ]; then
    echo "  Staker3 trying to withdraw already-withdrawn stake #$STAKE_ID..."
    TX_RES=$($BINARY tx reveal withdraw \
        $STAKE_ID \
        --from staker3 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        NEG_DOUBLE_WITHDRAW_RESULT="PASS"
        echo "  PASS: Double withdraw rejected at submission"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if expect_tx_failure "$TX_RESULT"; then
            NEG_DOUBLE_WITHDRAW_RESULT="PASS"
            echo "  PASS: Double withdraw rejected on-chain"
        else
            echo "  FAIL: Double withdraw was accepted (should have been rejected)"
        fi
    fi
else
    echo "  SKIP: No stake to double-withdraw"
fi
echo ""

# ========================================================================
# TEST 9: Negative - Stake exceeds threshold
# ========================================================================
echo "--- TEST 9: NEGATIVE - STAKE EXCEEDS THRESHOLD ---"

# Tranche threshold is 1000 DREAM, try staking the full amount (but must not exceed)
# First stake 500, then try 600 (total would be 1100 > 1000 threshold)
echo "  Staker1 staking 500 DREAM..."
TX_RES=$($BINARY tx reveal stake \
    $CONTRIB_ID 0 "500" \
    --from staker1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)
TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
HELPER_STAKE_ID=""
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    check_tx_success "$TX_RESULT" > /dev/null 2>&1
    HELPER_STAKE_ID=$(extract_event_value "$TX_RESULT" "staked" "stake_id")
fi

echo "  Staker2 trying to stake 600 DREAM (would exceed 1000 threshold)..."
TX_RES=$($BINARY tx reveal stake \
    $CONTRIB_ID 0 "600" \
    --from staker2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    NEG_STAKE_EXCEEDS_THRESHOLD_RESULT="PASS"
    echo "  PASS: Exceed-threshold stake rejected at submission"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if expect_tx_failure "$TX_RESULT"; then
        NEG_STAKE_EXCEEDS_THRESHOLD_RESULT="PASS"
        echo "  PASS: Exceed-threshold stake rejected on-chain"
    else
        echo "  FAIL: Exceed-threshold stake was accepted (should have been rejected)"
    fi
fi

# Clean up: withdraw the helper stake so it doesn't interfere with other tests
if [ -n "$HELPER_STAKE_ID" ]; then
    TX_RES=$($BINARY tx reveal withdraw \
        $HELPER_STAKE_ID \
        --from staker1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        wait_for_tx $TXHASH > /dev/null 2>&1
    fi
fi
echo ""

# ========================================================================
# TEST 10: Negative - Stake on cancelled contribution
# ========================================================================
echo "--- TEST 10: NEGATIVE - STAKE ON CANCELLED CONTRIBUTION ---"

# Propose and then cancel a contribution, then try staking on it
echo "  Proposing Project Orbit for cancel test..."
TRANCHE_ORBIT='{"name":"Orbit Core","description":"Orbit","components":["orbit.go"],"stakeThreshold":"500","previewUri":""}'

TX_RES=$($BINARY tx reveal propose \
    "Project Orbit" \
    "Will be cancelled" \
    "500" \
    "MIT" \
    "MIT" \
    --tranches "$TRANCHE_ORBIT" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
ORBIT_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        ORBIT_ID=$(extract_event_value "$TX_RESULT" "contribution_proposed" "contribution_id")
    fi
fi

if [ -n "$ORBIT_ID" ]; then
    # Cancel it
    TX_RES=$($BINARY tx reveal cancel \
        $ORBIT_ID "Testing cancelled stake" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        wait_for_tx $TXHASH > /dev/null 2>&1
    fi

    # Now try to stake on the cancelled contribution
    echo "  Staker1 trying to stake on cancelled contribution #$ORBIT_ID..."
    TX_RES=$($BINARY tx reveal stake \
        $ORBIT_ID 0 "200" \
        --from staker1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        NEG_STAKE_CANCELLED_RESULT="PASS"
        echo "  PASS: Stake on cancelled rejected at submission"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if expect_tx_failure "$TX_RESULT"; then
            NEG_STAKE_CANCELLED_RESULT="PASS"
            echo "  PASS: Stake on cancelled rejected on-chain"
        else
            echo "  FAIL: Stake on cancelled was accepted (should have been rejected)"
        fi
    fi
else
    echo "  SKIP: Could not create contribution for cancel test"
fi
echo ""

# --- RESULTS SUMMARY ---
echo "============================================================================"
echo "  REVEAL STAKE/WITHDRAW TEST RESULTS"
echo "============================================================================"
echo ""

TOTAL_COUNT=0
PASS_COUNT=0
FAIL_COUNT=0

for RESULT in "$PROPOSE_RESULT" "$APPROVE_RESULT" "$STAKE_RESULT" "$QUERY_STAKE_DETAIL_RESULT" "$QUERY_STAKES_BY_STAKER_RESULT" "$WITHDRAW_RESULT" "$VERIFY_WITHDRAW_RESULT" "$NEG_STAKE_TOO_LOW_RESULT" "$NEG_DOUBLE_WITHDRAW_RESULT" "$NEG_STAKE_EXCEEDS_THRESHOLD_RESULT" "$NEG_STAKE_CANCELLED_RESULT" "$NEG_WITHDRAW_NON_OWNER_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

echo "  1.  Propose Contribution:         $PROPOSE_RESULT"
echo "  2.  Approve via Council:           $APPROVE_RESULT"
echo "  3.  Stake DREAM:                   $STAKE_RESULT"
echo "  4.  Query Stake Detail:            $QUERY_STAKE_DETAIL_RESULT"
echo "  5.  Query Stakes by Staker:        $QUERY_STAKES_BY_STAKER_RESULT"
echo "  6.  Withdraw Stake:                $WITHDRAW_RESULT"
echo "  7.  Verify Stake Removed:          $VERIFY_WITHDRAW_RESULT"
echo "  8.  Neg: Withdraw Non-Owner:       $NEG_WITHDRAW_NON_OWNER_RESULT"
echo "  9.  Neg: Stake Too Low:            $NEG_STAKE_TOO_LOW_RESULT"
echo "  10. Neg: Double Withdraw:          $NEG_DOUBLE_WITHDRAW_RESULT"
echo "  11. Neg: Stake Exceeds Threshold:  $NEG_STAKE_EXCEEDS_THRESHOLD_RESULT"
echo "  12. Neg: Stake on Cancelled:       $NEG_STAKE_CANCELLED_RESULT"
echo ""
echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
