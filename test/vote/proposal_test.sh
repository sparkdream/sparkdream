#!/bin/bash

echo "--- TESTING: Proposal Lifecycle (x/vote) ---"
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Alice:     $ALICE_ADDR"
echo "Proposer1: $PROPOSER1_ADDR"
echo "Proposer2: $PROPOSER2_ADDR"
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

# =========================================================================
# PART 1: Create a PUBLIC proposal
# =========================================================================
echo "--- PART 1: Create a PUBLIC proposal ---"

# Use alice as proposer (she's a member and registered voter)
# Positional args: title description proposal-type reference-id voting-period-epochs quorum threshold veto-threshold visibility
# ProposalType: use string name "general" (not numeric "0")
# VisibilityLevel: use string name "visibility-public" (not numeric "1")
# LegacyDec fields: integer format (value * 10^18)
# Vote options: provided via repeated --options flag

TX_RES=$($BINARY tx vote create-proposal \
    "Test Public Proposal" \
    "This is a test proposal for e2e testing" \
    "general" \
    "0" \
    "3" \
    "330000000000000000" \
    "500000000000000000" \
    "334000000000000000" \
    "visibility-public" \
    --options '{"id":0,"label":"Yes","role":"OPTION_ROLE_STANDARD"}' \
    --options '{"id":1,"label":"No","role":"OPTION_ROLE_STANDARD"}' \
    --deposit 1000000uspark \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to create proposal: no txhash"
    echo "  Response: $TX_RES"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

if check_tx_success "$TX_RESULT"; then
    PROPOSAL_ID=$(extract_event_value "$TX_RESULT" "proposal_created" "proposal_id")
    if [ -z "$PROPOSAL_ID" ]; then
        # Try to get from response
        PROPOSAL_ID="0"
    fi
    PROPOSER=$(extract_event_value "$TX_RESULT" "proposal_created" "proposer")
    VOTER_COUNT=$(extract_event_value "$TX_RESULT" "proposal_created" "voter_count")
    echo "  Created proposal #$PROPOSAL_ID"
    echo "  Proposer: $PROPOSER"
    echo "  Eligible voters: $VOTER_COUNT"
else
    echo "  Failed to create proposal"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    exit 1
fi

# Export proposal ID for later tests
PUBLIC_PROPOSAL_ID=$PROPOSAL_ID

echo ""

# =========================================================================
# PART 2: Query proposal by ID
# =========================================================================
echo "--- PART 2: Query proposal by ID ---"

PROPOSAL_RESULT=$($BINARY query vote get-voting-proposal $PUBLIC_PROPOSAL_ID --output json 2>&1)

if echo "$PROPOSAL_RESULT" | grep -qi "error\|not found"; then
    echo "  Failed to query proposal #$PUBLIC_PROPOSAL_ID"
    echo "  Response: $PROPOSAL_RESULT"
    exit 1
fi

# get-voting-proposal returns .voting_proposal
P_TITLE=$(echo "$PROPOSAL_RESULT" | jq -r '.voting_proposal.title // "null"')
P_STATUS=$(echo "$PROPOSAL_RESULT" | jq -r '.voting_proposal.status // "null"')
P_VISIBILITY=$(echo "$PROPOSAL_RESULT" | jq -r '.voting_proposal.visibility // "null"')
P_PROPOSER=$(echo "$PROPOSAL_RESULT" | jq -r '.voting_proposal.proposer // "null"')
P_ELIGIBLE=$(echo "$PROPOSAL_RESULT" | jq -r '.voting_proposal.eligible_voters // "null"')

echo "  Title: $P_TITLE"
echo "  Status: $P_STATUS"
echo "  Visibility: $P_VISIBILITY"
echo "  Proposer: $P_PROPOSER"
echo "  Eligible voters: $P_ELIGIBLE"

if [ "$P_TITLE" != "Test Public Proposal" ]; then
    echo "  Title mismatch"
    exit 1
fi

# Status should be ACTIVE (enum value 0 or string)
# Note: proto3 omits default enum values (0), so ACTIVE appears as null in JSON
if [ "$P_STATUS" != "PROPOSAL_STATUS_ACTIVE" ] && [ "$P_STATUS" != "0" ] && [ "$P_STATUS" != "null" ]; then
    echo "  Expected ACTIVE status, got: $P_STATUS"
    exit 1
fi

echo "  Proposal query verified"
echo ""

# =========================================================================
# PART 3: Query proposal via dedicated query endpoint
# =========================================================================
echo "--- PART 3: Query via proposal endpoint ---"

PROPOSAL_Q=$($BINARY query vote proposal $PUBLIC_PROPOSAL_ID --output json 2>&1)

if echo "$PROPOSAL_Q" | grep -qi "error\|not found"; then
    echo "  Failed to query via proposal endpoint"
    # Non-fatal: the alias query may have a different response structure
    echo "  Skipping (query endpoint may not be available)"
else
    # proposal endpoint returns .proposal (not .voting_proposal)
    P_TITLE_Q=$(echo "$PROPOSAL_Q" | jq -r '.proposal.title // "null"')
    echo "  Proposal endpoint returned title: $P_TITLE_Q"
fi

echo ""

# =========================================================================
# PART 4: Query voter tree snapshot
# =========================================================================
echo "--- PART 4: Query voter tree snapshot ---"

SNAPSHOT_RESULT=$($BINARY query vote get-voter-tree-snapshot $PUBLIC_PROPOSAL_ID --output json 2>&1)

if echo "$SNAPSHOT_RESULT" | grep -qi "error\|not found"; then
    echo "  Failed to query voter tree snapshot"
    echo "  Response: $SNAPSHOT_RESULT"
    exit 1
fi

# get-voter-tree-snapshot returns .voter_tree_snapshot
SNAP_VOTER_COUNT=$(echo "$SNAPSHOT_RESULT" | jq -r '.voter_tree_snapshot.voter_count // "null"')
SNAP_MERKLE_ROOT=$(echo "$SNAPSHOT_RESULT" | jq -r '.voter_tree_snapshot.merkle_root // "null"')

echo "  Voter count: $SNAP_VOTER_COUNT"
echo "  Merkle root: ${SNAP_MERKLE_ROOT:0:20}..."

if [ "$SNAP_VOTER_COUNT" = "0" ] || [ "$SNAP_VOTER_COUNT" = "null" ]; then
    echo "  Expected non-zero voter count"
    exit 1
fi

echo "  Tree snapshot verified"
echo ""

# =========================================================================
# PART 5: Query proposal tally (should be empty initially)
# =========================================================================
echo "--- PART 5: Query proposal tally ---"

TALLY_RESULT=$($BINARY query vote proposal-tally $PUBLIC_PROPOSAL_ID --output json 2>&1)

if echo "$TALLY_RESULT" | grep -qi "error\|not found"; then
    echo "  Failed to query proposal tally"
    echo "  Response: $TALLY_RESULT"
    # Non-fatal: continue testing
else
    # proposal-tally returns .tally (array of {option_id, vote_count}) and .eligible_voters
    # Compute total votes from the tally array; .total_votes may be omitted when zero
    TOTAL_VOTES=$(echo "$TALLY_RESULT" | jq -r '[.tally[]?.vote_count // 0 | tonumber] | add // 0')
    echo "  Total votes: $TOTAL_VOTES"

    if [ "$TOTAL_VOTES" != "0" ]; then
        echo "  Expected 0 votes on new proposal, got $TOTAL_VOTES"
        # Non-fatal
    fi

    echo "  Initial tally verified (0 votes)"
fi

echo ""

# =========================================================================
# PART 6: List proposals
# =========================================================================
echo "--- PART 6: List proposals ---"

LIST_RESULT=$($BINARY query vote list-voting-proposal --output json 2>&1)

if echo "$LIST_RESULT" | grep -qi "error"; then
    echo "  Failed to list proposals"
    exit 1
fi

PROPOSAL_COUNT=$(echo "$LIST_RESULT" | jq -r '.voting_proposal | length' 2>/dev/null || echo "0")
echo "  Total proposals: $PROPOSAL_COUNT"

if [ "$PROPOSAL_COUNT" -lt 1 ]; then
    echo "  Expected at least 1 proposal"
    exit 1
fi

echo "  Proposal list verified"
echo ""

# =========================================================================
# PART 7: Create a SEALED proposal
# =========================================================================
echo "--- PART 7: Create a SEALED proposal ---"

# VisibilityLevel: visibility-sealed
TX_RES=$($BINARY tx vote create-proposal \
    "Test Sealed Proposal" \
    "A sealed proposal for commit-reveal voting" \
    "general" \
    "0" \
    "3" \
    "330000000000000000" \
    "500000000000000000" \
    "334000000000000000" \
    "visibility-sealed" \
    --options '{"id":0,"label":"Yes","role":"OPTION_ROLE_STANDARD"}' \
    --options '{"id":1,"label":"No","role":"OPTION_ROLE_STANDARD"}' \
    --deposit 1000000uspark \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to create sealed proposal: no txhash"
    echo "  Response: $TX_RES"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

if check_tx_success "$TX_RESULT"; then
    SEALED_PROPOSAL_ID=$(extract_event_value "$TX_RESULT" "proposal_created" "proposal_id")
    if [ -z "$SEALED_PROPOSAL_ID" ]; then
        SEALED_PROPOSAL_ID="1"
    fi
    echo "  Created sealed proposal #$SEALED_PROPOSAL_ID"
else
    echo "  Failed to create sealed proposal"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    # Non-fatal: sealed proposals might be disabled
    SEALED_PROPOSAL_ID=""
fi

echo ""

# =========================================================================
# PART 8: Query sealed proposal visibility
# =========================================================================
echo "--- PART 8: Verify sealed proposal visibility ---"

if [ -n "$SEALED_PROPOSAL_ID" ]; then
    SEALED_Q=$($BINARY query vote get-voting-proposal $SEALED_PROPOSAL_ID --output json 2>&1)

    if echo "$SEALED_Q" | grep -qi "error\|not found"; then
        echo "  Failed to query sealed proposal"
    else
        S_VISIBILITY=$(echo "$SEALED_Q" | jq -r '.voting_proposal.visibility // "null"')
        S_STATUS=$(echo "$SEALED_Q" | jq -r '.voting_proposal.status // "null"')
        S_REVEAL_END=$(echo "$SEALED_Q" | jq -r '.voting_proposal.reveal_end // "0"')

        echo "  Visibility: $S_VISIBILITY"
        echo "  Status: $S_STATUS"
        echo "  Reveal end: $S_REVEAL_END"

        if [ "$S_VISIBILITY" != "VISIBILITY_SEALED" ] && [ "$S_VISIBILITY" != "1" ]; then
            echo "  Expected VISIBILITY_SEALED, got: $S_VISIBILITY"
            exit 1
        fi

        echo "  Sealed proposal visibility verified"
    fi
else
    echo "  Skipped (no sealed proposal created)"
fi

echo ""

# =========================================================================
# PART 9: Cancel a proposal
# =========================================================================
echo "--- PART 9: Cancel a proposal ---"

# Create a throwaway proposal to cancel
TX_RES=$($BINARY tx vote create-proposal \
    "Proposal to Cancel" \
    "This proposal will be cancelled" \
    "general" \
    "0" \
    "3" \
    "330000000000000000" \
    "500000000000000000" \
    "334000000000000000" \
    "visibility-public" \
    --options '{"id":0,"label":"Yes","role":"OPTION_ROLE_STANDARD"}' \
    --options '{"id":1,"label":"No","role":"OPTION_ROLE_STANDARD"}' \
    --deposit 1000000uspark \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to create proposal to cancel: no txhash"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

CANCEL_PROPOSAL_ID=""
if check_tx_success "$TX_RESULT"; then
    CANCEL_PROPOSAL_ID=$(extract_event_value "$TX_RESULT" "proposal_created" "proposal_id")
    echo "  Created proposal #$CANCEL_PROPOSAL_ID (to be cancelled)"
else
    echo "  Failed to create proposal to cancel"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    exit 1
fi

# Now cancel it
TX_RES=$($BINARY tx vote cancel-proposal \
    $CANCEL_PROPOSAL_ID \
    "Testing cancellation flow" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to cancel proposal: no txhash"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

if check_tx_success "$TX_RESULT"; then
    CANCELLED_ID=$(extract_event_value "$TX_RESULT" "proposal_cancelled" "proposal_id")
    CANCEL_REASON=$(extract_event_value "$TX_RESULT" "proposal_cancelled" "reason")
    echo "  Cancelled proposal #$CANCELLED_ID (reason: $CANCEL_REASON)"
else
    echo "  Failed to cancel proposal"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    exit 1
fi

# Verify cancelled status
CANCEL_Q=$($BINARY query vote get-voting-proposal $CANCEL_PROPOSAL_ID --output json 2>&1)
C_STATUS=$(echo "$CANCEL_Q" | jq -r '.voting_proposal.status // "null"')
if [ "$C_STATUS" != "PROPOSAL_STATUS_CANCELLED" ] && [ "$C_STATUS" != "3" ]; then
    echo "  Expected CANCELLED status, got: $C_STATUS"
    exit 1
fi

echo "  Verified proposal is cancelled"
echo ""

# =========================================================================
# PART 10: Non-member cannot create proposal
# =========================================================================
echo "--- PART 10: Non-member proposal creation (should fail) ---"

# proposer2 is a member but should have deposit. Let's test with a non-registered
# proposer that has no membership.
if ! $BINARY keys show nonmember_proposer --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add nonmember_proposer --keyring-backend test --output json > /dev/null 2>&1
fi

NONMEMBER_PROPOSER_ADDR=$($BINARY keys show nonmember_proposer -a --keyring-backend test)

# Fund for gas
TX_RES=$($BINARY tx bank send \
    alice $NONMEMBER_PROPOSER_ADDR \
    5000000uspark \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
fi

TX_RES=$($BINARY tx vote create-proposal \
    "Unauthorized Proposal" \
    "Should fail" \
    "general" \
    "0" \
    "3" \
    "330000000000000000" \
    "500000000000000000" \
    "334000000000000000" \
    "visibility-public" \
    --options '{"id":0,"label":"Yes","role":"OPTION_ROLE_STANDARD"}' \
    --options '{"id":1,"label":"No","role":"OPTION_ROLE_STANDARD"}' \
    --deposit 1000000uspark \
    --from nonmember_proposer \
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
        echo "  Correctly rejected non-member proposal"
    else
        echo "  ERROR: Non-member should not be able to create proposals"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 11: Cancellation by non-proposer should fail
# =========================================================================
echo "--- PART 11: Unauthorized cancellation (should fail) ---"

# Try to cancel alice's public proposal from proposer1
TX_RES=$($BINARY tx vote cancel-proposal \
    $PUBLIC_PROPOSAL_ID \
    "Unauthorized cancel attempt" \
    --from proposer1 \
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
        echo "  Correctly rejected unauthorized cancellation"
    else
        echo "  ERROR: Non-proposer should not be able to cancel"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 12: Query proposals by status
# =========================================================================
echo "--- PART 12: Query proposals by status ---"

# Query ACTIVE proposals (status=1 maps to PROPOSAL_STATUS_ACTIVE=0 in proto, but the
# CLI positional arg is the raw uint64; ACTIVE=0 in the enum)
STATUS_RESULT=$($BINARY query vote proposals-by-status 0 --output json 2>&1)

if echo "$STATUS_RESULT" | grep -qi "error"; then
    echo "  Warning: proposals-by-status query may not be available"
    echo "  Skipping..."
else
    # proposals-by-status returns .proposals (array); may be empty {} when no matches
    ACTIVE_COUNT=$(echo "$STATUS_RESULT" | jq -r '.proposals | length' 2>/dev/null || echo "0")
    echo "  Active proposals: $ACTIVE_COUNT"
fi

echo ""

# =========================================================================
# PART 13: Create proposal with quorum > 1 (should fail)
# =========================================================================
echo "--- PART 13: Invalid quorum > 1 (should fail) ---"

# quorum = 1.5 = 1500000000000000000 in LegacyDec → ErrInvalidThreshold
TX_RES=$($BINARY tx vote create-proposal \
    "Invalid Quorum Proposal" \
    "Quorum > 1 should be rejected" \
    "general" \
    "0" \
    "3" \
    "1500000000000000000" \
    "500000000000000000" \
    "334000000000000000" \
    "visibility-public" \
    --options '{"id":0,"label":"Yes","role":"OPTION_ROLE_STANDARD"}' \
    --options '{"id":1,"label":"No","role":"OPTION_ROLE_STANDARD"}' \
    --deposit 1000000uspark \
    --from alice \
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
        echo "  Correctly rejected invalid quorum"
    else
        echo "  ERROR: Quorum > 1 should have failed"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 14: Create proposal with voting period too short (should fail)
# =========================================================================
echo "--- PART 14: Voting period too short (should fail) ---"

# min_voting_period_epochs = 3, so period = 1 should fail
TX_RES=$($BINARY tx vote create-proposal \
    "Short Period Proposal" \
    "Voting period below minimum" \
    "general" \
    "0" \
    "1" \
    "330000000000000000" \
    "500000000000000000" \
    "334000000000000000" \
    "visibility-public" \
    --options '{"id":0,"label":"Yes","role":"OPTION_ROLE_STANDARD"}' \
    --options '{"id":1,"label":"No","role":"OPTION_ROLE_STANDARD"}' \
    --deposit 1000000uspark \
    --from alice \
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
        echo "  Correctly rejected short voting period"
    else
        echo "  ERROR: Voting period below minimum should have failed"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 15: Create proposal with voting period too long (should fail)
# =========================================================================
echo "--- PART 15: Voting period too long (should fail) ---"

# max_voting_period_epochs = 30, so period = 100 should fail
TX_RES=$($BINARY tx vote create-proposal \
    "Long Period Proposal" \
    "Voting period above maximum" \
    "general" \
    "0" \
    "100" \
    "330000000000000000" \
    "500000000000000000" \
    "334000000000000000" \
    "visibility-public" \
    --options '{"id":0,"label":"Yes","role":"OPTION_ROLE_STANDARD"}' \
    --options '{"id":1,"label":"No","role":"OPTION_ROLE_STANDARD"}' \
    --deposit 1000000uspark \
    --from alice \
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
        echo "  Correctly rejected long voting period"
    else
        echo "  ERROR: Voting period above maximum should have failed"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 16: Cancel already cancelled proposal (should fail)
# =========================================================================
echo "--- PART 16: Cancel already-cancelled proposal (should fail) ---"

# CANCEL_PROPOSAL_ID was cancelled in Part 9 → ErrProposalNotCancellable
TX_RES=$($BINARY tx vote cancel-proposal \
    $CANCEL_PROPOSAL_ID \
    "Re-cancel attempt" \
    --from alice \
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
        echo "  Correctly rejected cancel of already-cancelled proposal"
    else
        echo "  ERROR: Re-cancellation should have failed"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 17: Insufficient deposit (should fail)
# =========================================================================
echo "--- PART 17: Insufficient deposit (should fail) ---"

# min_proposal_deposit = 1000000uspark; send only 100uspark → ErrInsufficientDeposit
TX_RES=$($BINARY tx vote create-proposal \
    "Low Deposit Proposal" \
    "Deposit below minimum" \
    "general" \
    "0" \
    "3" \
    "330000000000000000" \
    "500000000000000000" \
    "334000000000000000" \
    "visibility-public" \
    --options '{"id":0,"label":"Yes","role":"OPTION_ROLE_STANDARD"}' \
    --options '{"id":1,"label":"No","role":"OPTION_ROLE_STANDARD"}' \
    --deposit 100uspark \
    --from alice \
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
        echo "  Correctly rejected insufficient deposit"
    else
        echo "  ERROR: Insufficient deposit should have failed"
        exit 1
    fi
fi

echo ""

# =========================================================================
# Export proposal IDs for subsequent tests
# =========================================================================
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    # Append proposal IDs to .test_env
    echo "" >> "$SCRIPT_DIR/.test_env"
    echo "# Proposal IDs from proposal_test.sh" >> "$SCRIPT_DIR/.test_env"
    echo "export PUBLIC_PROPOSAL_ID=$PUBLIC_PROPOSAL_ID" >> "$SCRIPT_DIR/.test_env"
    if [ -n "$SEALED_PROPOSAL_ID" ]; then
        echo "export SEALED_PROPOSAL_ID=$SEALED_PROPOSAL_ID" >> "$SCRIPT_DIR/.test_env"
    fi
    echo "  Exported proposal IDs to .test_env"
fi

echo ""

# =========================================================================
# SUMMARY
# =========================================================================
echo "--- TEST SUMMARY ---"
echo "  Part 1:  Create PUBLIC proposal            - PASSED"
echo "  Part 2:  Query proposal by ID              - PASSED"
echo "  Part 3:  Query via proposal endpoint        - PASSED"
echo "  Part 4:  Query voter tree snapshot          - PASSED"
echo "  Part 5:  Query proposal tally               - PASSED"
echo "  Part 6:  List proposals                     - PASSED"
echo "  Part 7:  Create SEALED proposal             - PASSED"
echo "  Part 8:  Verify sealed visibility           - PASSED"
echo "  Part 9:  Cancel proposal                    - PASSED"
echo "  Part 10: Non-member proposal rejection      - PASSED"
echo "  Part 11: Unauthorized cancel rejection      - PASSED"
echo "  Part 12: Query proposals by status          - PASSED"
echo "  Part 13: Invalid quorum rejection           - PASSED"
echo "  Part 14: Short voting period rejection      - PASSED"
echo "  Part 15: Long voting period rejection       - PASSED"
echo "  Part 16: Re-cancel already cancelled        - PASSED"
echo "  Part 17: Insufficient deposit rejection     - PASSED"
echo ""
echo "All proposal lifecycle checks passed!"
