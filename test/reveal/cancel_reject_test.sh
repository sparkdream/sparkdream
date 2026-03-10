#!/bin/bash

echo "--- TESTING: x/reveal CANCEL AND REJECT FLOWS ---"
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
PROPOSE_FOR_CANCEL_RESULT="FAIL"
CANCEL_BY_CONTRIBUTOR_RESULT="FAIL"
VERIFY_CANCELLED_RESULT="FAIL"
PROPOSE_FOR_REJECT_RESULT="FAIL"
REJECT_BY_COUNCIL_RESULT="FAIL"
VERIFY_REJECTED_RESULT="FAIL"
NEG_SELF_STAKE_RESULT="FAIL"
NEG_CANCEL_BACKED_RESULT="FAIL"

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

    echo "  Alice voting YES..."
    $BINARY tx commons vote-proposal $prop_id yes \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 3

    echo "  Bob voting YES..."
    $BINARY tx commons vote-proposal $prop_id yes \
        --from bob -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 3

    echo "  Executing proposal..."
    EXEC_RES=$($BINARY tx commons execute-proposal $prop_id \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')
    sleep 6

    EXEC_TX_JSON=$(wait_for_tx $EXEC_TX_HASH)
    if check_tx_success "$EXEC_TX_JSON"; then
        echo "  Execution successful"
        return 0
    else
        echo "  Execution may have failed"
        return 1
    fi
}

# ========================================================================
# PART A: CONTRIBUTOR CANCEL
# ========================================================================
echo "=== PART A: CONTRIBUTOR CANCEL ==="
echo ""

# Step 1: Propose a contribution to cancel
echo "--- TEST 1: PROPOSE FOR CANCEL ---"

TRANCHE_CANCEL='{"name":"Module Alpha","description":"Alpha module","components":["alpha.go"],"stakeThreshold":"500","previewUri":""}'

TX_RES=$($BINARY tx reveal propose \
    "Project Zenith" \
    "Will be cancelled by contributor" \
    "500" \
    "MIT" \
    "MIT" \
    --tranches "$TRANCHE_CANCEL" \
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
        CANCEL_CONTRIB_ID=$(extract_event_value "$TX_RESULT" "contribution_proposed" "contribution_id")
        if [ -z "$CANCEL_CONTRIB_ID" ]; then
            CANCEL_CONTRIB_ID=$($BINARY query reveal contributions-by-contributor $ALICE_ADDR --output json 2>&1 | \
                jq -r '[.contributions[] | select(.project_name == "Project Zenith")] | last | .id // empty')
        fi

        if [ -n "$CANCEL_CONTRIB_ID" ]; then
            PROPOSE_FOR_CANCEL_RESULT="PASS"
            echo "  PASS: Contribution #$CANCEL_CONTRIB_ID proposed for cancel test"
        else
            echo "  FAIL: Could not get contribution ID"
        fi
    else
        echo "  FAIL: Propose transaction failed"
    fi
else
    echo "  FAIL: No txhash returned"
    echo "  Raw: $TX_RES"
fi
echo ""

# Step 2: Cancel by contributor (before BACKED)
echo "--- TEST 2: CANCEL BY CONTRIBUTOR ---"

if [ "$PROPOSE_FOR_CANCEL_RESULT" == "PASS" ]; then
    echo "  Alice cancelling contribution #$CANCEL_CONTRIB_ID..."
    TX_RES=$($BINARY tx reveal cancel \
        $CANCEL_CONTRIB_ID "Changed my mind" \
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
            CANCEL_BY_CONTRIBUTOR_RESULT="PASS"
            echo "  PASS: Contribution cancelled by contributor"
        else
            echo "  FAIL: Cancel transaction failed"
        fi
    else
        echo "  FAIL: No txhash for cancel"
        echo "  Raw: $TX_RES"
    fi
else
    echo "  SKIP: No contribution to cancel"
fi
echo ""

# Step 3: Verify contribution is CANCELLED
echo "--- TEST 3: VERIFY CONTRIBUTION IS CANCELLED ---"

if [ "$CANCEL_BY_CONTRIBUTOR_RESULT" == "PASS" ]; then
    CONTRIB_JSON=$($BINARY query reveal contribution $CANCEL_CONTRIB_ID --output json 2>&1)
    STATUS=$(echo "$CONTRIB_JSON" | jq -r '.contribution.status')
    echo "  Status: $STATUS"

    if [ "$STATUS" == "CONTRIBUTION_STATUS_CANCELLED" ]; then
        VERIFY_CANCELLED_RESULT="PASS"
        echo "  PASS: Contribution is CANCELLED"
    else
        echo "  FAIL: Expected CANCELLED, got $STATUS"
    fi
else
    echo "  SKIP: Cancel failed"
fi
echo ""

# ========================================================================
# PRE-PROPOSE: Create contributions for later tests BEFORE any rejection
# (Rejection triggers cooldown, blocking new proposals from same contributor)
# ========================================================================
echo "=== PRE-PROPOSE: Creating contributions for later tests ==="
echo ""

# Pre-propose for negative tests (Test 7: self-stake, Test 8: cancel-after-backed)
TRANCHE_NEG='{"name":"Test Module","description":"For negative tests","components":["test.go"],"stakeThreshold":"500","previewUri":""}'

echo "  Pre-proposing Project Sigma for negative tests..."
TX_RES=$($BINARY tx reveal propose \
    "Project Sigma" \
    "For negative testing" \
    "500" \
    "MIT" \
    "MIT" \
    --tranches "$TRANCHE_NEG" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
NEG_CONTRIB_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        NEG_CONTRIB_ID=$(extract_event_value "$TX_RESULT" "contribution_proposed" "contribution_id")
        if [ -z "$NEG_CONTRIB_ID" ]; then
            NEG_CONTRIB_ID=$($BINARY query reveal contributions-by-contributor $ALICE_ADDR --output json 2>&1 | \
                jq -r '[.contributions[] | select(.project_name == "Project Sigma")] | last | .id // empty')
        fi
        echo "  Pre-proposed Project Sigma (ID: $NEG_CONTRIB_ID)"
    else
        echo "  WARNING: Could not pre-propose Project Sigma"
    fi
fi
echo ""

# ========================================================================
# PART B: COUNCIL REJECTION
# ========================================================================
echo "=== PART B: COUNCIL REJECTION ==="
echo ""

# Step 4: Propose a contribution to reject
echo "--- TEST 4: PROPOSE FOR REJECTION ---"

TRANCHE_REJECT='{"name":"Bad Module","description":"Will be rejected","components":["bad.go"],"stakeThreshold":"500","previewUri":""}'

TX_RES=$($BINARY tx reveal propose \
    "Project Nebula" \
    "Will be rejected by council" \
    "500" \
    "MIT" \
    "MIT" \
    --tranches "$TRANCHE_REJECT" \
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
        REJECT_CONTRIB_ID=$(extract_event_value "$TX_RESULT" "contribution_proposed" "contribution_id")
        if [ -z "$REJECT_CONTRIB_ID" ]; then
            REJECT_CONTRIB_ID=$($BINARY query reveal contributions-by-contributor $ALICE_ADDR --output json 2>&1 | \
                jq -r '[.contributions[] | select(.project_name == "Project Nebula")] | last | .id // empty')
        fi

        if [ -n "$REJECT_CONTRIB_ID" ]; then
            PROPOSE_FOR_REJECT_RESULT="PASS"
            echo "  PASS: Contribution #$REJECT_CONTRIB_ID proposed for reject test"
        else
            echo "  FAIL: Could not get contribution ID"
        fi
    else
        echo "  FAIL: Propose transaction failed"
    fi
else
    echo "  FAIL: No txhash returned"
    echo "  Raw: $TX_RES"
fi
echo ""

# Step 5: Reject via council proposal
echo "--- TEST 5: REJECT VIA COUNCIL PROPOSAL ---"

if [ "$PROPOSE_FOR_REJECT_RESULT" == "PASS" ] && [ -n "$COUNCIL_POLICY" ]; then
    jq -n \
        --arg policy "$COUNCIL_POLICY" \
        --arg alice "$ALICE_ADDR" \
        --arg contrib_id "$REJECT_CONTRIB_ID" \
    '{
        policy_address: $policy,
        metadata: "Reject contribution for test",
        messages: [{
            "@type": "/sparkdream.reveal.v1.MsgReject",
            authority: $policy,
            proposer: $alice,
            contribution_id: $contrib_id,
            reason: "Insufficient quality for reveal program"
        }]
    }' > "$PROPOSAL_DIR/reject_contribution.json"

    echo "  Submitting council rejection proposal..."
    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/reject_contribution.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

    PROPOSAL_ID=$(get_group_proposal_id $TX_HASH)

    if [ -n "$PROPOSAL_ID" ] && [ "$PROPOSAL_ID" != "null" ]; then
        echo "  Council Proposal ID: $PROPOSAL_ID"
        vote_and_execute $PROPOSAL_ID
        if [ $? -eq 0 ]; then
            REJECT_BY_COUNCIL_RESULT="PASS"
            echo "  PASS: Contribution rejected via council"
        else
            echo "  FAIL: Council rejection failed"
        fi
    else
        echo "  FAIL: Could not submit rejection proposal"
        sleep 3
        TX_DETAIL=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
        if [ -n "$TX_DETAIL" ]; then
            echo "  TX code: $(echo $TX_DETAIL | jq -r '.code')"
            echo "  TX log:  $(echo $TX_DETAIL | jq -r '.raw_log' | head -c 500)"
        fi
    fi
else
    echo "  SKIP: No contribution to reject or no council policy"
fi
echo ""

# Step 6: Verify rejection and cooldown
echo "--- TEST 6: VERIFY REJECTION AND COOLDOWN ---"

if [ "$REJECT_BY_COUNCIL_RESULT" == "PASS" ]; then
    CONTRIB_JSON=$($BINARY query reveal contribution $REJECT_CONTRIB_ID --output json 2>&1)
    STATUS=$(echo "$CONTRIB_JSON" | jq -r '.contribution.status')
    ELIGIBLE_AT=$(echo "$CONTRIB_JSON" | jq -r '.contribution.proposal_eligible_at')
    echo "  Status: $STATUS"
    echo "  Proposal eligible at: $ELIGIBLE_AT"

    if [ "$STATUS" == "CONTRIBUTION_STATUS_CANCELLED" ] && [ "$ELIGIBLE_AT" != "0" ]; then
        VERIFY_REJECTED_RESULT="PASS"
        echo "  PASS: Contribution rejected with cooldown set"
    else
        echo "  FAIL: Expected CANCELLED with non-zero cooldown"
    fi
else
    echo "  SKIP: Rejection failed"
fi
echo ""

# ========================================================================
# PART C: NEGATIVE TESTS
# ========================================================================
echo "=== PART C: NEGATIVE TESTS ==="
echo ""

# Test 7: Self-stake prevention (contributor cannot stake on own contribution)
echo "--- TEST 7: NEGATIVE - SELF-STAKE PREVENTION ---"

# NEG_CONTRIB_ID was pre-proposed above (before rejection triggered cooldown)

if [ -n "$NEG_CONTRIB_ID" ] && [ -n "$COUNCIL_POLICY" ]; then
    # Approve it first
    jq -n \
        --arg policy "$COUNCIL_POLICY" \
        --arg alice "$ALICE_ADDR" \
        --arg contrib_id "$NEG_CONTRIB_ID" \
    '{
        policy_address: $policy,
        metadata: "Approve to test self-stake prevention",
        messages: [{
            "@type": "/sparkdream.reveal.v1.MsgApprove",
            authority: $policy,
            proposer: $alice,
            contribution_id: $contrib_id
        }]
    }' > "$PROPOSAL_DIR/approve_neg_test.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/approve_neg_test.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
    PROPOSAL_ID=$(get_group_proposal_id $TX_HASH)

    if [ -n "$PROPOSAL_ID" ]; then
        vote_and_execute $PROPOSAL_ID > /dev/null 2>&1

        # Now try self-staking (alice is the contributor, should be rejected)
        echo "  Alice trying to stake on her own contribution #$NEG_CONTRIB_ID..."
        TX_RES=$($BINARY tx reveal stake \
            $NEG_CONTRIB_ID 0 "200" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            NEG_SELF_STAKE_RESULT="PASS"
            echo "  PASS: Self-stake rejected at submission"
        else
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)

            if expect_tx_failure "$TX_RESULT"; then
                NEG_SELF_STAKE_RESULT="PASS"
                echo "  PASS: Self-stake rejected on-chain"
            else
                echo "  FAIL: Self-stake was accepted (should have been rejected)"
            fi
        fi
    else
        echo "  SKIP: Could not approve contribution for negative test"
    fi
else
    echo "  SKIP: Could not create contribution for negative test"
fi
echo ""

# Test 8: Cancel after BACKED (should fail for contributor)
echo "--- TEST 8: NEGATIVE - CANCEL AFTER BACKED ---"

# We need the NEG_CONTRIB_ID to be BACKED. Stake on it first.
if [ -n "$NEG_CONTRIB_ID" ]; then
    # Have staker1 and staker2 stake to reach threshold
    TX_RES=$($BINARY tx reveal stake \
        $NEG_CONTRIB_ID 0 "300" \
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

    TX_RES=$($BINARY tx reveal stake \
        $NEG_CONTRIB_ID 0 "200" \
        --from staker2 \
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

    # Verify tranche is BACKED
    TRANCHE_JSON=$($BINARY query reveal tranche $NEG_CONTRIB_ID 0 --output json 2>&1)
    TRANCHE_STATUS=$(echo "$TRANCHE_JSON" | jq -r '.tranche.status')
    echo "  Tranche status: $TRANCHE_STATUS"

    if [ "$TRANCHE_STATUS" == "TRANCHE_STATUS_BACKED" ]; then
        # Now try to cancel as contributor (should fail)
        echo "  Alice trying to cancel after BACKED..."
        TX_RES=$($BINARY tx reveal cancel \
            $NEG_CONTRIB_ID "Want to cancel after backed" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            NEG_CANCEL_BACKED_RESULT="PASS"
            echo "  PASS: Cancel after BACKED rejected at submission"
        else
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)

            if expect_tx_failure "$TX_RESULT"; then
                NEG_CANCEL_BACKED_RESULT="PASS"
                echo "  PASS: Cancel after BACKED rejected on-chain"
            else
                echo "  FAIL: Cancel after BACKED was accepted (should have been rejected)"
            fi
        fi
    else
        echo "  SKIP: Tranche not BACKED (status: $TRANCHE_STATUS)"
        # If not backed, the cancel would succeed, which isn't what we want to test
        # Mark as PASS if tranche isn't BACKED (setup issue, not test failure)
    fi
else
    echo "  SKIP: No contribution for negative test"
fi
echo ""

# --- RESULTS SUMMARY ---
echo "============================================================================"
echo "  REVEAL CANCEL/REJECT TEST RESULTS"
echo "============================================================================"
echo ""

TOTAL_COUNT=0
PASS_COUNT=0
FAIL_COUNT=0

for RESULT in "$PROPOSE_FOR_CANCEL_RESULT" "$CANCEL_BY_CONTRIBUTOR_RESULT" "$VERIFY_CANCELLED_RESULT" "$PROPOSE_FOR_REJECT_RESULT" "$REJECT_BY_COUNCIL_RESULT" "$VERIFY_REJECTED_RESULT" "$NEG_SELF_STAKE_RESULT" "$NEG_CANCEL_BACKED_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

echo "  1. Propose for Cancel:            $PROPOSE_FOR_CANCEL_RESULT"
echo "  2. Cancel by Contributor:          $CANCEL_BY_CONTRIBUTOR_RESULT"
echo "  3. Verify Cancelled:               $VERIFY_CANCELLED_RESULT"
echo "  4. Propose for Rejection:          $PROPOSE_FOR_REJECT_RESULT"
echo "  5. Reject via Council:             $REJECT_BY_COUNCIL_RESULT"
echo "  6. Verify Rejection + Cooldown:    $VERIFY_REJECTED_RESULT"
echo "  7. Neg: Self-Stake Prevention:     $NEG_SELF_STAKE_RESULT"
echo "  8. Neg: Cancel After BACKED:       $NEG_CANCEL_BACKED_RESULT"
echo ""
echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
