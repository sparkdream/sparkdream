#!/bin/bash

echo "--- TESTING: x/reveal FULL LIFECYCLE ---"
echo "  Flow: Propose -> Approve (council) -> Stake -> Reveal -> Verify"
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
TRANCHE_STAKING_RESULT="FAIL"
STAKE1_RESULT="FAIL"
STAKE2_RESULT="FAIL"
TRANCHE_BACKED_RESULT="FAIL"
QUERY_STAKES_RESULT="FAIL"
REVEAL_RESULT="FAIL"
TRANCHE_REVEALED_RESULT="FAIL"
VERIFY1_RESULT="FAIL"
VERIFY2_RESULT="FAIL"
TALLY_RESULT="FAIL"
VOTES_BY_VOTER_RESULT="FAIL"
NEG_DOUBLE_VOTE_RESULT="FAIL"
NEG_SELF_VOTE_RESULT="FAIL"
NEG_INVALID_QUALITY_LOW_RESULT="FAIL"
NEG_INVALID_QUALITY_HIGH_RESULT="FAIL"
NEG_NON_STAKER_VOTE_RESULT="FAIL"
NEG_NON_CONTRIBUTOR_REVEAL_RESULT="FAIL"

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

# Helper: extract commons proposal ID from tx hash
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

# Helper: vote + execute a commons proposal (council requires 2 of 3 votes)
vote_and_execute() {
    local prop_id=$1

    echo "  Alice voting YES on proposal #$prop_id..."
    $BINARY tx commons vote-proposal $prop_id yes \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 3

    echo "  Bob voting YES on proposal #$prop_id..."
    $BINARY tx commons vote-proposal $prop_id yes \
        --from bob -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json > /dev/null 2>&1
    sleep 3

    echo "  Executing proposal #$prop_id..."
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
        echo "  Execution result:"
        echo "$EXEC_TX_JSON" | jq -r '.raw_log' 2>/dev/null | head -c 500
        echo "  Execution may have failed"
        return 1
    fi
}

# ========================================================================
# STEP 1: Propose a contribution (single tranche for simpler lifecycle)
# ========================================================================
echo "--- STEP 1: PROPOSE CONTRIBUTION ---"

# Single tranche, camelCase JSON, one per --tranches flag
TRANCHE1='{"name":"Core Module","description":"Core functionality","components":["module.go","handler.go"],"stakeThreshold":"1000","previewUri":"https://example.com/preview-core"}'

echo "  Proposing: Project Aurora (1 tranche, 1000 DREAM)..."

TX_RES=$($BINARY tx reveal propose \
    "Project Aurora" \
    "A lifecycle test project" \
    "1000" \
    "MIT" \
    "Apache-2.0" \
    --tranches "$TRANCHE1" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  FAIL: No txhash returned"
    echo "  Raw: $TX_RES"
    echo "  Cannot proceed with lifecycle test"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx $TXHASH)

if check_tx_success "$TX_RESULT"; then
    CONTRIB_ID=$(extract_event_value "$TX_RESULT" "contribution_proposed" "contribution_id")
    if [ -z "$CONTRIB_ID" ]; then
        # Fallback: get from query (status null = PROPOSED in proto3)
        CONTRIB_ID=$($BINARY query reveal contributions-by-contributor $ALICE_ADDR --output json 2>&1 | \
            jq -r '.contributions | map(select(.status == "CONTRIBUTION_STATUS_PROPOSED" or .status == null)) | last | .id // empty')
    fi

    if [ -n "$CONTRIB_ID" ]; then
        PROPOSE_RESULT="PASS"
        echo "  PASS: Contribution #$CONTRIB_ID proposed"
    else
        echo "  FAIL: Could not determine contribution ID"
        exit 1
    fi
else
    echo "  FAIL: Propose transaction failed"
    exit 1
fi
echo ""

# ========================================================================
# STEP 2: Approve via Commons Council proposal
# ========================================================================
echo "--- STEP 2: APPROVE VIA COUNCIL PROPOSAL ---"

if [ -z "$COUNCIL_POLICY" ]; then
    echo "  FAIL: No council policy address available"
    echo "  Cannot proceed with lifecycle test"
    exit 1
fi

# Build council proposal to approve contribution
jq -n \
    --arg policy "$COUNCIL_POLICY" \
    --arg alice "$ALICE_ADDR" \
    --arg contrib_id "$CONTRIB_ID" \
'{
    policy_address: $policy,
    metadata: "Approve contribution for lifecycle test",
    messages: [{
        "@type": "/sparkdream.reveal.v1.MsgApprove",
        authority: $policy,
        proposer: $alice,
        contribution_id: $contrib_id
    }]
}' > "$PROPOSAL_DIR/approve_contribution.json"

echo "  Submitting council approval proposal..."
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/approve_contribution.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "  Submitted tx: $TX_HASH"
PROPOSAL_ID=$(get_group_proposal_id $TX_HASH)

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    echo "  FAIL: Could not submit approval proposal"
    sleep 3
    TX_DETAIL=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
    if [ -n "$TX_DETAIL" ]; then
        echo "  TX code: $(echo $TX_DETAIL | jq -r '.code')"
        echo "  TX log:  $(echo $TX_DETAIL | jq -r '.raw_log' | head -c 500)"
    fi
else
    echo "  Council Proposal ID: $PROPOSAL_ID"
    vote_and_execute $PROPOSAL_ID
    if [ $? -eq 0 ]; then
        APPROVE_RESULT="PASS"
        echo "  PASS: Contribution approved via council"
    else
        echo "  FAIL: Council approval failed"
    fi
fi
echo ""

# ========================================================================
# STEP 2b: Verify tranche 0 is now STAKING
# ========================================================================
echo "--- STEP 2b: VERIFY TRANCHE 0 IS STAKING ---"

if [ "$APPROVE_RESULT" == "PASS" ]; then
    TRANCHE_JSON=$($BINARY query reveal tranche $CONTRIB_ID 0 --output json 2>&1)
    TRANCHE_STATUS=$(echo "$TRANCHE_JSON" | jq -r '.tranche.status')
    echo "  Tranche 0 status: $TRANCHE_STATUS"

    if [ "$TRANCHE_STATUS" == "TRANCHE_STATUS_STAKING" ]; then
        TRANCHE_STAKING_RESULT="PASS"
        echo "  PASS: Tranche 0 is in STAKING status"
    else
        echo "  FAIL: Expected STAKING, got $TRANCHE_STATUS"
    fi
else
    echo "  SKIP: Approval failed"
fi
echo ""

# ========================================================================
# STEP 3: Stake DREAM on tranche 0
# ========================================================================
echo "--- STEP 3: STAKE DREAM ON TRANCHE 0 ---"

STAKE1_ID=""
STAKE2_ID=""

if [ "$TRANCHE_STAKING_RESULT" == "PASS" ]; then
    # Staker1 stakes 500 DREAM
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
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            STAKE1_ID=$(extract_event_value "$TX_RESULT" "staked" "stake_id")
            STAKE1_RESULT="PASS"
            echo "  PASS: Staker1 staked (stake ID: $STAKE1_ID)"
        else
            echo "  FAIL: Staker1 stake failed"
        fi
    else
        echo "  FAIL: No txhash for staker1 stake"
        echo "  Raw: $TX_RES"
    fi

    # Staker2 stakes 500 DREAM (should push total to 1000 = threshold, auto-transition to BACKED)
    echo "  Staker2 staking 500 DREAM (should reach threshold)..."
    TX_RES=$($BINARY tx reveal stake \
        $CONTRIB_ID 0 "500" \
        --from staker2 \
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
            STAKE2_ID=$(extract_event_value "$TX_RESULT" "staked" "stake_id")
            STAKE2_RESULT="PASS"
            echo "  PASS: Staker2 staked (stake ID: $STAKE2_ID)"

            # Check if tranche_backed event was emitted
            BACKED_EVENT=$(extract_event_value "$TX_RESULT" "tranche_backed" "tranche_id")
            if [ -n "$BACKED_EVENT" ]; then
                echo "  Auto-transition: tranche_backed event detected"
            fi
        else
            echo "  FAIL: Staker2 stake failed"
        fi
    else
        echo "  FAIL: No txhash for staker2 stake"
        echo "  Raw: $TX_RES"
    fi
else
    echo "  SKIP: Tranche not in STAKING status"
fi
echo ""

# ========================================================================
# STEP 3b: Verify tranche is now BACKED
# ========================================================================
echo "--- STEP 3b: VERIFY TRANCHE 0 IS BACKED ---"

if [ "$STAKE2_RESULT" == "PASS" ]; then
    TRANCHE_JSON=$($BINARY query reveal tranche $CONTRIB_ID 0 --output json 2>&1)
    TRANCHE_STATUS=$(echo "$TRANCHE_JSON" | jq -r '.tranche.status')
    DREAM_STAKED=$(echo "$TRANCHE_JSON" | jq -r '.tranche.dream_staked')
    echo "  Tranche 0 status: $TRANCHE_STATUS"
    echo "  DREAM staked: $DREAM_STAKED"

    if [ "$TRANCHE_STATUS" == "TRANCHE_STATUS_BACKED" ]; then
        TRANCHE_BACKED_RESULT="PASS"
        echo "  PASS: Tranche 0 is now BACKED"
    else
        echo "  FAIL: Expected BACKED, got $TRANCHE_STATUS"
    fi
else
    echo "  SKIP: Staking not complete"
fi
echo ""

# ========================================================================
# STEP 3c: Query tranche stakes
# ========================================================================
echo "--- STEP 3c: QUERY TRANCHE STAKES ---"

STAKES_JSON=$($BINARY query reveal tranche-stakes $CONTRIB_ID 0 --output json 2>&1)

STAKE_COUNT=$(echo "$STAKES_JSON" | jq -r '.stakes // [] | length')
echo "  Found $STAKE_COUNT stakes for tranche 0"

if [ "$STAKE_COUNT" -ge 2 ]; then
    QUERY_STAKES_RESULT="PASS"
    echo "  PASS: Tranche stakes query works"
else
    echo "  FAIL: Expected at least 2 stakes, got $STAKE_COUNT"
    echo "  Response: $STAKES_JSON"
fi
echo ""

# ========================================================================
# STEP 4: Reveal code for tranche 0
# ========================================================================
echo "--- STEP 4: REVEAL CODE FOR TRANCHE 0 ---"

if [ "$TRANCHE_BACKED_RESULT" == "PASS" ]; then
    echo "  Alice revealing code for tranche 0..."
    TX_RES=$($BINARY tx reveal reveal \
        $CONTRIB_ID 0 \
        "https://ipfs.io/ipfs/QmTestHash123456" \
        "https://docs.example.com/aurora/core" \
        "abc123def456" \
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
            REVEAL_RESULT="PASS"
            echo "  PASS: Code revealed"
        else
            echo "  FAIL: Reveal transaction failed"
        fi
    else
        echo "  FAIL: No txhash for reveal"
        echo "  Raw: $TX_RES"
    fi
else
    echo "  SKIP: Tranche not backed"
fi
echo ""

# ========================================================================
# STEP 4b: Verify tranche is now REVEALED
# ========================================================================
echo "--- STEP 4b: VERIFY TRANCHE 0 IS REVEALED ---"

if [ "$REVEAL_RESULT" == "PASS" ]; then
    TRANCHE_JSON=$($BINARY query reveal tranche $CONTRIB_ID 0 --output json 2>&1)
    TRANCHE_STATUS=$(echo "$TRANCHE_JSON" | jq -r '.tranche.status')
    CODE_URI=$(echo "$TRANCHE_JSON" | jq -r '.tranche.code_uri')
    COMMIT_HASH=$(echo "$TRANCHE_JSON" | jq -r '.tranche.commit_hash')
    echo "  Tranche 0 status: $TRANCHE_STATUS"
    echo "  Code URI: $CODE_URI"
    echo "  Commit Hash: $COMMIT_HASH"

    if [ "$TRANCHE_STATUS" == "TRANCHE_STATUS_REVEALED" ] && [ "$CODE_URI" == "https://ipfs.io/ipfs/QmTestHash123456" ]; then
        TRANCHE_REVEALED_RESULT="PASS"
        echo "  PASS: Tranche 0 is REVEALED with correct data"
    else
        echo "  FAIL: Expected REVEALED with correct URI"
    fi
else
    echo "  SKIP: Reveal failed"
fi
echo ""

# ========================================================================
# STEP 5: Verification votes
# ========================================================================
echo "--- STEP 5: CAST VERIFICATION VOTES ---"

if [ "$TRANCHE_REVEALED_RESULT" == "PASS" ]; then
    # Staker1 votes YES with quality 4
    echo "  Staker1 voting: confirmed=true, quality=4..."
    TX_RES=$($BINARY tx reveal verify \
        $CONTRIB_ID 0 true 4 "Good quality code" \
        --from staker1 \
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
            VERIFY1_RESULT="PASS"
            echo "  PASS: Staker1 verification vote recorded"
        else
            echo "  FAIL: Staker1 vote failed"
        fi
    else
        echo "  FAIL: No txhash for staker1 vote"
        echo "  Raw: $TX_RES"
    fi

    # Staker2 votes YES with quality 5
    echo "  Staker2 voting: confirmed=true, quality=5..."
    TX_RES=$($BINARY tx reveal verify \
        $CONTRIB_ID 0 true 5 "Excellent work" \
        --from staker2 \
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
            VERIFY2_RESULT="PASS"
            echo "  PASS: Staker2 verification vote recorded"
        else
            echo "  FAIL: Staker2 vote failed"
        fi
    else
        echo "  FAIL: No txhash for staker2 vote"
        echo "  Raw: $TX_RES"
    fi
else
    echo "  SKIP: Tranche not revealed"
fi
echo ""

# ========================================================================
# STEP 5b: Query tranche tally
# ========================================================================
echo "--- STEP 5b: QUERY TRANCHE TALLY ---"

if [ "$VERIFY1_RESULT" == "PASS" ] || [ "$VERIFY2_RESULT" == "PASS" ]; then
    TALLY_JSON=$($BINARY query reveal tranche-tally $CONTRIB_ID 0 --output json 2>&1)

    if echo "$TALLY_JSON" | jq -e '.yes_weight' > /dev/null 2>&1; then
        YES_WEIGHT=$(echo "$TALLY_JSON" | jq -r '.yes_weight')
        NO_WEIGHT=$(echo "$TALLY_JSON" | jq -r '.no_weight')
        VOTE_COUNT=$(echo "$TALLY_JSON" | jq -r '.vote_count')

        echo "  Yes Weight: $YES_WEIGHT"
        echo "  No Weight:  $NO_WEIGHT"
        echo "  Vote Count: $VOTE_COUNT"

        if [ "$VOTE_COUNT" -ge 1 ]; then
            TALLY_RESULT="PASS"
            echo "  PASS: Tally shows votes recorded"
        else
            echo "  FAIL: Expected at least 1 vote in tally"
        fi
    else
        echo "  FAIL: Could not query tally"
        echo "  Response: $TALLY_JSON"
    fi
else
    echo "  SKIP: No votes recorded"
fi
echo ""

# ========================================================================
# STEP 5c: Query votes by voter
# ========================================================================
echo "--- STEP 5c: QUERY VOTES BY VOTER ---"

if [ "$VERIFY1_RESULT" == "PASS" ]; then
    VOTES_JSON=$($BINARY query reveal votes-by-voter $STAKER1_ADDR --output json 2>&1)

    VOTE_COUNT=$(echo "$VOTES_JSON" | jq -r '.votes // [] | length')
    echo "  Found $VOTE_COUNT votes by staker1"

    if [ "$VOTE_COUNT" -ge 1 ]; then
        FIRST_VOTE_QUALITY=$(echo "$VOTES_JSON" | jq -r '.votes[0].quality_rating')
        echo "  First vote quality rating: $FIRST_VOTE_QUALITY"

        VOTES_BY_VOTER_RESULT="PASS"
        echo "  PASS: Votes by voter query works"
    else
        echo "  FAIL: Expected at least 1 vote"
        echo "  Response: $VOTES_JSON"
    fi
else
    echo "  SKIP: No votes to query"
fi
echo ""

# ========================================================================
# STEP 6: Negative verify tests
# ========================================================================
echo "--- STEP 6: NEGATIVE VERIFY TESTS ---"
echo ""

# 6a: Double vote (staker1 already voted, try again)
echo "--- TEST 6a: NEGATIVE - DOUBLE VOTE ---"

if [ "$VERIFY1_RESULT" == "PASS" ]; then
    echo "  Staker1 trying to vote again..."
    TX_RES=$($BINARY tx reveal verify \
        $CONTRIB_ID 0 true 3 "Second vote attempt" \
        --from staker1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        NEG_DOUBLE_VOTE_RESULT="PASS"
        echo "  PASS: Double vote rejected at submission"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if expect_tx_failure "$TX_RESULT"; then
            NEG_DOUBLE_VOTE_RESULT="PASS"
            echo "  PASS: Double vote rejected on-chain"
        else
            echo "  FAIL: Double vote was accepted (should have been rejected)"
        fi
    fi
else
    echo "  SKIP: First vote not recorded"
fi
echo ""

# 6b: Self-vote (contributor tries to vote on own contribution)
echo "--- TEST 6b: NEGATIVE - SELF VOTE ---"

if [ "$TRANCHE_REVEALED_RESULT" == "PASS" ]; then
    echo "  Alice (contributor) trying to vote on her own contribution..."
    TX_RES=$($BINARY tx reveal verify \
        $CONTRIB_ID 0 true 5 "Self vote attempt" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        NEG_SELF_VOTE_RESULT="PASS"
        echo "  PASS: Self-vote rejected at submission"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if expect_tx_failure "$TX_RESULT"; then
            NEG_SELF_VOTE_RESULT="PASS"
            echo "  PASS: Self-vote rejected on-chain"
        else
            echo "  FAIL: Self-vote was accepted (should have been rejected)"
        fi
    fi
else
    echo "  SKIP: Tranche not revealed"
fi
echo ""

# 6c: Invalid quality rating (0 - below minimum)
echo "--- TEST 6c: NEGATIVE - INVALID QUALITY RATING (0) ---"

if [ "$TRANCHE_REVEALED_RESULT" == "PASS" ]; then
    echo "  Staker3 voting with quality=0 (invalid, must be 1-5)..."
    TX_RES=$($BINARY tx reveal verify \
        $CONTRIB_ID 0 true 0 "Zero quality" \
        --from staker3 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        NEG_INVALID_QUALITY_LOW_RESULT="PASS"
        echo "  PASS: Quality=0 rejected at submission"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if expect_tx_failure "$TX_RESULT"; then
            NEG_INVALID_QUALITY_LOW_RESULT="PASS"
            echo "  PASS: Quality=0 rejected on-chain"
        else
            echo "  FAIL: Quality=0 was accepted (should have been rejected)"
        fi
    fi
else
    echo "  SKIP: Tranche not revealed"
fi
echo ""

# 6d: Invalid quality rating (6 - above maximum)
echo "--- TEST 6d: NEGATIVE - INVALID QUALITY RATING (6) ---"

if [ "$TRANCHE_REVEALED_RESULT" == "PASS" ]; then
    echo "  Staker3 voting with quality=6 (invalid, must be 1-5)..."
    TX_RES=$($BINARY tx reveal verify \
        $CONTRIB_ID 0 true 6 "Too high quality" \
        --from staker3 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        NEG_INVALID_QUALITY_HIGH_RESULT="PASS"
        echo "  PASS: Quality=6 rejected at submission"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if expect_tx_failure "$TX_RESULT"; then
            NEG_INVALID_QUALITY_HIGH_RESULT="PASS"
            echo "  PASS: Quality=6 rejected on-chain"
        else
            echo "  FAIL: Quality=6 was accepted (should have been rejected)"
        fi
    fi
else
    echo "  SKIP: Tranche not revealed"
fi
echo ""

# 6e: Non-staker voting (staker3 didn't stake on this contribution)
echo "--- TEST 6e: NEGATIVE - NON-STAKER VOTING ---"

if [ "$TRANCHE_REVEALED_RESULT" == "PASS" ]; then
    echo "  Staker3 (did not stake on this tranche) trying to vote..."
    TX_RES=$($BINARY tx reveal verify \
        $CONTRIB_ID 0 true 3 "Non-staker vote" \
        --from staker3 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        NEG_NON_STAKER_VOTE_RESULT="PASS"
        echo "  PASS: Non-staker vote rejected at submission"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if expect_tx_failure "$TX_RESULT"; then
            NEG_NON_STAKER_VOTE_RESULT="PASS"
            echo "  PASS: Non-staker vote rejected on-chain"
        else
            echo "  FAIL: Non-staker vote was accepted (should have been rejected)"
        fi
    fi
else
    echo "  SKIP: Tranche not revealed"
fi
echo ""

# 6f: Non-contributor trying to reveal (use a second contribution for this)
echo "--- TEST 6f: NEGATIVE - NON-CONTRIBUTOR REVEAL ---"

# Propose a second contribution, approve it, stake to BACKED, then try reveal from staker1
echo "  Setting up: Proposing Project Ember for non-contributor reveal test..."
TRANCHE_EMBER='{"name":"Ember Core","description":"Test module","components":["ember.go"],"stakeThreshold":"200","previewUri":""}'

TX_RES=$($BINARY tx reveal propose \
    "Project Ember" \
    "For non-contributor reveal test" \
    "200" \
    "MIT" \
    "MIT" \
    --tranches "$TRANCHE_EMBER" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
EMBER_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        EMBER_ID=$(extract_event_value "$TX_RESULT" "contribution_proposed" "contribution_id")
    fi
fi

if [ -n "$EMBER_ID" ]; then
    # Approve via council
    jq -n \
        --arg policy "$COUNCIL_POLICY" \
        --arg alice "$ALICE_ADDR" \
        --arg contrib_id "$EMBER_ID" \
    '{
        policy_address: $policy,
        metadata: "Approve for non-contributor reveal test",
        messages: [{
            "@type": "/sparkdream.reveal.v1.MsgApprove",
            authority: $policy,
            proposer: $alice,
            contribution_id: $contrib_id
        }]
    }' > "$PROPOSAL_DIR/approve_ember.json"

    SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/approve_ember.json" \
        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000000uspark --output json)
    TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
    EMBER_PROP_ID=$(get_group_proposal_id $TX_HASH)

    if [ -n "$EMBER_PROP_ID" ]; then
        vote_and_execute $EMBER_PROP_ID > /dev/null 2>&1

        # Stake to BACKED with staker1 (200 = threshold)
        TX_RES=$($BINARY tx reveal stake \
            $EMBER_ID 0 "200" \
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

        # Verify backed
        TRANCHE_JSON=$($BINARY query reveal tranche $EMBER_ID 0 --output json 2>&1)
        TRANCHE_STATUS=$(echo "$TRANCHE_JSON" | jq -r '.tranche.status')

        if [ "$TRANCHE_STATUS" == "TRANCHE_STATUS_BACKED" ]; then
            # Now staker1 (not the contributor) tries to reveal
            echo "  Staker1 (non-contributor) trying to reveal on contribution #$EMBER_ID..."
            TX_RES=$($BINARY tx reveal reveal \
                $EMBER_ID 0 \
                "https://ipfs.io/ipfs/QmFakeHash" \
                "https://docs.example.com/fake" \
                "deadbeef1234" \
                --from staker1 \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000uspark \
                -y \
                --output json 2>&1)

            TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

            if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
                NEG_NON_CONTRIBUTOR_REVEAL_RESULT="PASS"
                echo "  PASS: Non-contributor reveal rejected at submission"
            else
                sleep 6
                TX_RESULT=$(wait_for_tx $TXHASH)

                if expect_tx_failure "$TX_RESULT"; then
                    NEG_NON_CONTRIBUTOR_REVEAL_RESULT="PASS"
                    echo "  PASS: Non-contributor reveal rejected on-chain"
                else
                    echo "  FAIL: Non-contributor reveal was accepted (should have been rejected)"
                fi
            fi
        else
            echo "  SKIP: Could not get Ember to BACKED status ($TRANCHE_STATUS)"
        fi
    else
        echo "  SKIP: Could not approve Ember contribution"
    fi
else
    echo "  SKIP: Could not propose Ember contribution"
fi
echo ""

# --- RESULTS SUMMARY ---
echo "============================================================================"
echo "  REVEAL LIFECYCLE TEST RESULTS"
echo "============================================================================"
echo ""

TOTAL_COUNT=0
PASS_COUNT=0
FAIL_COUNT=0

for RESULT in "$PROPOSE_RESULT" "$APPROVE_RESULT" "$TRANCHE_STAKING_RESULT" "$STAKE1_RESULT" "$STAKE2_RESULT" "$TRANCHE_BACKED_RESULT" "$QUERY_STAKES_RESULT" "$REVEAL_RESULT" "$TRANCHE_REVEALED_RESULT" "$VERIFY1_RESULT" "$VERIFY2_RESULT" "$TALLY_RESULT" "$VOTES_BY_VOTER_RESULT" "$NEG_DOUBLE_VOTE_RESULT" "$NEG_SELF_VOTE_RESULT" "$NEG_INVALID_QUALITY_LOW_RESULT" "$NEG_INVALID_QUALITY_HIGH_RESULT" "$NEG_NON_STAKER_VOTE_RESULT" "$NEG_NON_CONTRIBUTOR_REVEAL_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

echo "  1.  Propose Contribution:      $PROPOSE_RESULT"
echo "  2.  Approve via Council:        $APPROVE_RESULT"
echo "  3.  Tranche 0 STAKING:          $TRANCHE_STAKING_RESULT"
echo "  4.  Staker1 Stake:              $STAKE1_RESULT"
echo "  5.  Staker2 Stake:              $STAKE2_RESULT"
echo "  6.  Tranche 0 BACKED:           $TRANCHE_BACKED_RESULT"
echo "  7.  Query Tranche Stakes:       $QUERY_STAKES_RESULT"
echo "  8.  Reveal Code:                $REVEAL_RESULT"
echo "  9.  Tranche 0 REVEALED:         $TRANCHE_REVEALED_RESULT"
echo "  10. Verify Vote 1:              $VERIFY1_RESULT"
echo "  11. Verify Vote 2:              $VERIFY2_RESULT"
echo "  12. Query Tranche Tally:        $TALLY_RESULT"
echo "  13. Query Votes by Voter:       $VOTES_BY_VOTER_RESULT"
echo "  14. Neg: Double Vote:           $NEG_DOUBLE_VOTE_RESULT"
echo "  15. Neg: Self Vote:             $NEG_SELF_VOTE_RESULT"
echo "  16. Neg: Invalid Quality (0):   $NEG_INVALID_QUALITY_LOW_RESULT"
echo "  17. Neg: Invalid Quality (6):   $NEG_INVALID_QUALITY_HIGH_RESULT"
echo "  18. Neg: Non-Staker Vote:       $NEG_NON_STAKER_VOTE_RESULT"
echo "  19. Neg: Non-Contributor Reveal: $NEG_NON_CONTRIBUTOR_REVEAL_RESULT"
echo ""
echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
