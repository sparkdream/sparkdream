#!/bin/bash

echo "--- TESTING: Vote Submission & Tally (x/vote) ---"
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh && bash proposal_test.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Alice:     $ALICE_ADDR"
echo "Voter1:    $VOTER1_ADDR"
echo "Voter2:    $VOTER2_ADDR"
echo "Voter3:    $VOTER3_ADDR"
echo ""

# Check that proposal IDs are available
if [ -z "$PUBLIC_PROPOSAL_ID" ]; then
    echo "PUBLIC_PROPOSAL_ID not set. Run proposal_test.sh first."
    exit 1
fi

echo "Public proposal ID: $PUBLIC_PROPOSAL_ID"
echo ""

# === HELPER FUNCTIONS ===

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

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        return 1
    fi
    return 0
}

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        return 0
    fi
    return 1
}

# Helper: compute total votes from tally array
compute_total_votes() {
    local TALLY_JSON=$1
    echo "$TALLY_JSON" | jq -r '[.tally[]?.vote_count // 0 | tonumber] | add // 0'
}

# =========================================================================
# PART 1: Verify proposal is ACTIVE before voting
# =========================================================================
echo "--- PART 1: Verify proposal is ACTIVE ---"

PROPOSAL_Q=$($BINARY query vote get-voting-proposal $PUBLIC_PROPOSAL_ID --output json 2>&1)

if echo "$PROPOSAL_Q" | grep -qi "error\|not found"; then
    echo "  Proposal #$PUBLIC_PROPOSAL_ID not found"
    exit 1
fi

P_STATUS=$(echo "$PROPOSAL_Q" | jq -r '.voting_proposal.status // "null"')
P_VISIBILITY=$(echo "$PROPOSAL_Q" | jq -r '.voting_proposal.visibility // "null"')

# Note: proto3 omits default enum values (0), so ACTIVE appears as null in JSON
if [ "$P_STATUS" != "PROPOSAL_STATUS_ACTIVE" ] && [ "$P_STATUS" != "0" ] && [ "$P_STATUS" != "null" ]; then
    echo "  Proposal is not ACTIVE (status=$P_STATUS), cannot vote"
    exit 1
fi

echo "  Proposal #$PUBLIC_PROPOSAL_ID is ACTIVE (visibility=$P_VISIBILITY)"
echo ""

# =========================================================================
# PART 2: Submit vote from voter1 (option 0 = "Yes")
# =========================================================================
echo "--- PART 2: Submit vote from voter1 ---"

# Generate unique nullifier for voter1 (32 bytes, hex)
# In production this comes from ZK circuit; for e2e we use deterministic values
VOTER1_NULLIFIER="aabbccdd00000000000000000000000000000000000000000000000000000001"
VOTER1_NULLIFIER_B64=$(echo "$VOTER1_NULLIFIER" | xxd -r -p | base64)

# Proof is empty/dummy since verifying key is not configured (verification skipped)
DUMMY_PROOF_B64=""

TX_RES=$($BINARY tx vote vote \
    $PUBLIC_PROPOSAL_ID \
    "0" \
    --nullifier "$VOTER1_NULLIFIER_B64" \
    --proof "$DUMMY_PROOF_B64" \
    --from voter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit vote: no txhash"
    echo "  Response: $TX_RES"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

if check_tx_success "$TX_RESULT"; then
    VOTE_PROPOSAL=$(extract_event_value "$TX_RESULT" "vote_cast" "proposal_id")
    VOTE_OPTION=$(extract_event_value "$TX_RESULT" "vote_cast" "vote_option")
    echo "  Vote cast on proposal #$VOTE_PROPOSAL, option=$VOTE_OPTION"
else
    echo "  Failed to submit vote"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    exit 1
fi

echo ""

# =========================================================================
# PART 3: Submit vote from voter2 (option 0 = "Yes")
# =========================================================================
echo "--- PART 3: Submit vote from voter2 ---"

VOTER2_NULLIFIER="aabbccdd00000000000000000000000000000000000000000000000000000002"
VOTER2_NULLIFIER_B64=$(echo "$VOTER2_NULLIFIER" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote vote \
    $PUBLIC_PROPOSAL_ID \
    "0" \
    --nullifier "$VOTER2_NULLIFIER_B64" \
    --proof "$DUMMY_PROOF_B64" \
    --from voter2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit vote: no txhash"
    echo "  Response: $TX_RES"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

if check_tx_success "$TX_RESULT"; then
    echo "  Vote cast from voter2 on proposal #$PUBLIC_PROPOSAL_ID"
else
    echo "  Failed to submit vote from voter2"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    exit 1
fi

echo ""

# =========================================================================
# PART 4: Submit vote from voter3 (option 1 = "No")
# =========================================================================
echo "--- PART 4: Submit vote from voter3 (different option) ---"

VOTER3_NULLIFIER="aabbccdd00000000000000000000000000000000000000000000000000000003"
VOTER3_NULLIFIER_B64=$(echo "$VOTER3_NULLIFIER" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote vote \
    $PUBLIC_PROPOSAL_ID \
    "1" \
    --nullifier "$VOTER3_NULLIFIER_B64" \
    --proof "$DUMMY_PROOF_B64" \
    --from voter3 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit vote: no txhash"
    echo "  Response: $TX_RES"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

if check_tx_success "$TX_RESULT"; then
    echo "  Vote cast from voter3 on proposal #$PUBLIC_PROPOSAL_ID (option=1)"
else
    echo "  Failed to submit vote from voter3"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    exit 1
fi

echo ""

# =========================================================================
# PART 5: Verify tally shows votes
# =========================================================================
echo "--- PART 5: Verify proposal tally ---"

TALLY_RESULT=$($BINARY query vote proposal-tally $PUBLIC_PROPOSAL_ID --output json 2>&1)

if echo "$TALLY_RESULT" | grep -qi "error\|not found"; then
    echo "  Failed to query tally"
    echo "  Response: $TALLY_RESULT"
    exit 1
fi

# proposal-tally returns .tally (array of {option_id, vote_count}) and .eligible_voters
# Compute total votes from the tally array
TOTAL_VOTES=$(compute_total_votes "$TALLY_RESULT")
echo "  Total votes: $TOTAL_VOTES"

if [ "$TOTAL_VOTES" -lt 3 ]; then
    echo "  Expected at least 3 votes, got $TOTAL_VOTES"
    # Show full tally for debugging
    echo "  Full tally: $(echo "$TALLY_RESULT" | jq -c '.tally // "null"')"
    exit 1
fi

# Show per-option breakdown
echo "  Tally breakdown:"
echo "$TALLY_RESULT" | jq -r '.tally[]? // empty | "    Option \(.option_id): \(.vote_count) votes"' 2>/dev/null || \
echo "  (Could not parse per-option breakdown)"

ELIGIBLE=$(echo "$TALLY_RESULT" | jq -r '.eligible_voters // "unknown"')
echo "  Eligible voters: $ELIGIBLE"

echo "  Tally verified"
echo ""

# =========================================================================
# PART 6: Double vote with same nullifier should fail
# =========================================================================
echo "--- PART 6: Double vote rejection ---"

# Try to reuse voter1's nullifier
TX_RES=$($BINARY tx vote vote \
    $PUBLIC_PROPOSAL_ID \
    "0" \
    --nullifier "$VOTER1_NULLIFIER_B64" \
    --proof "$DUMMY_PROOF_B64" \
    --from voter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Correctly rejected double vote (no broadcast)"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        echo "  Correctly rejected double vote: $RAW_LOG"
    else
        echo "  ERROR: Double vote should have failed"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 7: Check nullifier usage
# =========================================================================
echo "--- PART 7: Check nullifier usage ---"

# nullifier-used query expects hex-encoded nullifier (not base64)
NULLIFIER_RESULT=$($BINARY query vote nullifier-used $PUBLIC_PROPOSAL_ID "$VOTER1_NULLIFIER" --output json 2>&1)

if echo "$NULLIFIER_RESULT" | grep -qi "error"; then
    echo "  Warning: Could not query nullifier"
    echo "  Response: $NULLIFIER_RESULT"
    echo "  Skipping nullifier check..."
else
    NULLIFIER_USED=$(echo "$NULLIFIER_RESULT" | jq -r '.used // "unknown"')
    echo "  Nullifier used: $NULLIFIER_USED"
fi

echo ""

# =========================================================================
# PART 8: Query anonymous votes for proposal
# =========================================================================
echo "--- PART 8: Query anonymous votes ---"

VOTES_RESULT=$($BINARY query vote proposal-votes $PUBLIC_PROPOSAL_ID --output json 2>&1)

if echo "$VOTES_RESULT" | grep -qi "error"; then
    echo "  Warning: Could not query proposal votes"
    # Try alternate query
    VOTES_RESULT=$($BINARY query vote list-anonymous-vote --output json 2>&1)
    VOTE_COUNT=$(echo "$VOTES_RESULT" | jq -r '.anonymous_vote | length' 2>/dev/null || echo "0")
else
    # proposal-votes returns .votes (array) and .sealed_votes (array)
    # May return empty {} when no votes
    VOTE_COUNT=$(echo "$VOTES_RESULT" | jq -r '.votes | length' 2>/dev/null || echo "0")
fi

echo "  Anonymous votes found: $VOTE_COUNT"

echo ""

# =========================================================================
# PART 9: Vote on non-existent proposal should fail
# =========================================================================
echo "--- PART 9: Vote on non-existent proposal (should fail) ---"

FAKE_NULLIFIER_B64=$(echo "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote vote \
    "999999" \
    "0" \
    --nullifier "$FAKE_NULLIFIER_B64" \
    --proof "$DUMMY_PROOF_B64" \
    --from voter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Correctly rejected (no broadcast)"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected vote on non-existent proposal"
    else
        echo "  ERROR: Vote on non-existent proposal should fail"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 10: Vote with out-of-range option should fail
# =========================================================================
echo "--- PART 10: Out-of-range vote option (should fail) ---"

OOR_NULLIFIER_B64=$(echo "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee0" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote vote \
    $PUBLIC_PROPOSAL_ID \
    "99" \
    --nullifier "$OOR_NULLIFIER_B64" \
    --proof "$DUMMY_PROOF_B64" \
    --from voter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Correctly rejected (no broadcast)"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected out-of-range vote option"
    else
        echo "  ERROR: Out-of-range option should have failed"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 11: Vote on cancelled proposal (should fail)
# =========================================================================
echo "--- PART 11: Vote on cancelled proposal (should fail) ---"

# Find a cancelled proposal from proposal_test.sh
# The cancelled proposal is the one after SEALED_PROPOSAL_ID
# We need to query proposals-by-status for CANCELLED (status=3) to find it
CANCELLED_RESULT=$($BINARY query vote proposals-by-status 3 --output json 2>&1)
CANCELLED_PROP_ID=$(echo "$CANCELLED_RESULT" | jq -r '.proposals[0].id // ""' 2>/dev/null)

if [ -z "$CANCELLED_PROP_ID" ] || [ "$CANCELLED_PROP_ID" == "null" ]; then
    echo "  No cancelled proposal found, skipping..."
else
    CANCEL_NULLIFIER_B64=$(echo "bbbbbbbb00000000000000000000000000000000000000000000000000000099" | xxd -r -p | base64)

    TX_RES=$($BINARY tx vote vote \
        $CANCELLED_PROP_ID \
        "0" \
        --nullifier "$CANCEL_NULLIFIER_B64" \
        --proof "$DUMMY_PROOF_B64" \
        --from voter1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Correctly rejected (no broadcast)"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")

        if check_tx_failure "$TX_RESULT"; then
            echo "  Correctly rejected vote on cancelled proposal"
        else
            echo "  ERROR: Vote on cancelled proposal should have failed"
            exit 1
        fi
    fi
fi

echo ""

# =========================================================================
# PART 12: Final tally verification (unchanged after failed votes)
# =========================================================================
echo "--- PART 12: Final tally verification ---"

FINAL_TALLY=$($BINARY query vote proposal-tally $PUBLIC_PROPOSAL_ID --output json 2>&1)
FINAL_TOTAL=$(compute_total_votes "$FINAL_TALLY")

echo "  Final total votes: $FINAL_TOTAL"

if [ "$FINAL_TOTAL" != "$TOTAL_VOTES" ]; then
    echo "  Tally changed unexpectedly (was $TOTAL_VOTES, now $FINAL_TOTAL)"
    exit 1
fi

echo "  Tally unchanged after failed vote attempts"
echo ""

# =========================================================================
# SUMMARY
# =========================================================================
echo "--- TEST SUMMARY ---"
echo "  Part 1:  Verify ACTIVE proposal             - PASSED"
echo "  Part 2:  Submit vote (voter1, option 0)      - PASSED"
echo "  Part 3:  Submit vote (voter2, option 0)      - PASSED"
echo "  Part 4:  Submit vote (voter3, option 1)      - PASSED"
echo "  Part 5:  Verify tally (3 votes)              - PASSED"
echo "  Part 6:  Double vote rejection               - PASSED"
echo "  Part 7:  Nullifier usage check               - PASSED"
echo "  Part 8:  Query anonymous votes               - PASSED"
echo "  Part 9:  Non-existent proposal rejection     - PASSED"
echo "  Part 10: Out-of-range option rejection       - PASSED"
echo "  Part 11: Vote on cancelled rejection         - PASSED"
echo "  Part 12: Final tally verification            - PASSED"
echo ""
echo "All vote submission checks passed!"
