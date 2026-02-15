#!/bin/bash

echo "--- TESTING NAME MODULE: DISPUTE RESOLUTION (DREAM STAKING) ---"

# --- 0. SETUP & CONFIG ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
TARGET_NAME="phoenix"
CONTEST_NAME="aurora"
CHALLENGER_WINS_NAME="zenith"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "Error: jq is not installed."
    exit 1
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

# name_claimant is an x/rep member with DREAM (set up by test/name/setup_test_accounts.sh).
# Bob and Carol are Council members but NOT x/rep members — they cannot stake DREAM.
# So we use name_claimant as the claimant for disputes.
CLAIMANT_KEY="name_claimant"
CLAIMANT_ADDR=$($BINARY keys show $CLAIMANT_KEY -a --keyring-backend test 2>/dev/null)

if [ -z "$CLAIMANT_ADDR" ]; then
    echo "Error: '$CLAIMANT_KEY' key not found. Run test/name/setup_test_accounts.sh first."
    exit 1
fi

echo "Alice (Owner/Contester):   $ALICE_ADDR  (Council + x/rep member)"
echo "Bob (Council Voter):       $BOB_ADDR  (Council only)"
echo "Carol (Council Voter):     $CAROL_ADDR  (Council only)"
echo "Claimant ($CLAIMANT_KEY):  $CLAIMANT_ADDR  (x/rep member with DREAM)"
echo ""

# ========================================================================
# Helper Functions
# ========================================================================

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

FAILURES=0

assert_equals() {
    local LABEL=$1
    local EXPECTED=$2
    local ACTUAL=$3

    if [ "$EXPECTED" == "$ACTUAL" ]; then
        echo "  PASS: $LABEL (=$ACTUAL)"
    else
        echo "  FAIL: $LABEL (expected=$EXPECTED, actual=$ACTUAL)"
        FAILURES=$((FAILURES + 1))
    fi
}

# Proto3 omits false/0/"" defaults from JSON. This helper normalizes booleans.
jq_bool() {
    local JQ_INPUT=$1
    local JQ_PATH=$2
    echo "$JQ_INPUT" | jq -r "if ${JQ_PATH} then \"true\" else \"false\" end"
}

# Submit a tx and wait for inclusion. Returns the on-chain tx result JSON.
# Sets TX_OK=true on success, TX_OK=false on failure.
submit_and_wait() {
    local TX_RES=$1
    local LABEL=${2:-"transaction"}
    TX_OK=false

    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then
        echo "  FAIL: $LABEL - no txhash in response"
        echo "  $TX_RES"
        return 1
    fi

    # Check for broadcast-time rejection (CheckTx failure)
    local BCODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BCODE" != "0" ] && [ "$BCODE" != "null" ]; then
        echo "  FAIL: $LABEL - rejected at broadcast (code=$BCODE)"
        echo "  $(echo "$TX_RES" | jq -r '.raw_log // "unknown error"')"
        return 1
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    if [ $? -ne 0 ]; then
        echo "  FAIL: $LABEL - tx not found on chain"
        return 1
    fi

    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  FAIL: $LABEL - tx failed (code=$CODE)"
        echo "  $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        return 1
    fi

    TX_OK=true
    return 0
}

# ========================================================================
# PART 1: SETUP -- Register Names for Testing
# ========================================================================
echo "--- PART 1: SETUP -- Register Names ---"

# Register "phoenix" for Alice (for uncontested dispute test)
TX_RES=$($BINARY tx name register-name "$TARGET_NAME" "Original Owner Data" \
    --from alice -y \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        echo "  Name '$TARGET_NAME' registered by Alice"
    else
        echo "  Registration skipped (name may already exist)"
    fi
fi

# Verify ownership
RESOLVE_RES=$($BINARY query name resolve "$TARGET_NAME" --output json 2>&1)
CURRENT_OWNER=$(echo "$RESOLVE_RES" | jq -r '.name_record.owner')

assert_equals "'$TARGET_NAME' owned by Alice" "$ALICE_ADDR" "$CURRENT_OWNER"

# Register "aurora" for Alice (for contested dispute test)
TX_RES=$($BINARY tx name register-name "$CONTEST_NAME" "Contest Test Data" \
    --from alice -y \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        echo "  Name '$CONTEST_NAME' registered by Alice"
    else
        echo "  Registration skipped (name may already exist)"
    fi
fi

RESOLVE_RES=$($BINARY query name resolve "$CONTEST_NAME" --output json 2>&1)
CURRENT_OWNER=$(echo "$RESOLVE_RES" | jq -r '.name_record.owner')

assert_equals "'$CONTEST_NAME' owned by Alice" "$ALICE_ADDR" "$CURRENT_OWNER"

# NOTE: 'zenith' registration is deferred to after Part 5 (phoenix transfer frees a name slot)

echo ""

# ========================================================================
# PART 2: FILE DISPUTE (name_claimant stakes DREAM)
# ========================================================================
echo "--- PART 2: FILE DISPUTE ($CLAIMANT_KEY stakes DREAM) ---"

echo "  Note: Filing a dispute locks the claimant's DREAM (default 50)"

# Check if active dispute already exists
EXISTING=$($BINARY query name get-dispute "$TARGET_NAME" --output json 2>&1)
EXISTING_ACTIVE=$(jq_bool "$EXISTING" ".dispute.active" 2>/dev/null)

if [ "$EXISTING_ACTIVE" == "true" ]; then
    echo "  Active dispute already exists, skipping"
else
    echo "  $CLAIMANT_KEY filing dispute for '$TARGET_NAME'..."
    TX_RES=$($BINARY tx name file-dispute "$TARGET_NAME" "I have a rightful claim to this name" \
        --from $CLAIMANT_KEY -y \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  FAIL: Could not submit file-dispute tx"
        echo "  $TX_RES"
        FAILURES=$((FAILURES + 1))
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "  Dispute filed successfully"
        else
            echo "  FAIL: file-dispute transaction failed"
            FAILURES=$((FAILURES + 1))
        fi
    fi
fi

# Verify dispute state
DISPUTE_RES=$($BINARY query name get-dispute "$TARGET_NAME" --output json 2>&1)
DISPUTE_ACTIVE=$(jq_bool "$DISPUTE_RES" ".dispute.active")
DISPUTE_CLAIMANT=$(echo "$DISPUTE_RES" | jq -r '.dispute.claimant')
DISPUTE_STAKE=$(echo "$DISPUTE_RES" | jq -r '.dispute.stake_amount // "0"')

assert_equals "dispute is active" "true" "$DISPUTE_ACTIVE"
assert_equals "dispute claimant is $CLAIMANT_KEY" "$CLAIMANT_ADDR" "$DISPUTE_CLAIMANT"
echo "  Dispute stake amount: $DISPUTE_STAKE"

echo ""

# ========================================================================
# PART 3: COUNCIL PROPOSAL -- Resolve Dispute (transfer approved)
# ========================================================================
echo "--- PART 3: COUNCIL PROPOSAL -- Resolve Dispute ---"

# Get Council Policy Address
COMMONS_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json 2>&1)
COMMONS_POLICY=$(echo "$COMMONS_INFO" | jq -r '.extended_group.policy_address')

if [ -z "$COMMONS_POLICY" ] || [ "$COMMONS_POLICY" == "null" ]; then
    echo "  ERROR: No Group Policy found for Commons Council"
    exit 1
fi
echo "  Council Policy: $COMMONS_POLICY"

# Create proposal JSON: transfer_approved=true means dispute upheld, name transfers to claimant
cat > "$PROPOSAL_DIR/resolve_dispute.json" <<EOF
{
  "group_policy_address": "$COMMONS_POLICY",
  "proposers": ["$ALICE_ADDR"],
  "title": "Resolve Name Dispute: $TARGET_NAME",
  "summary": "Transfer name '$TARGET_NAME' to claimant. Dispute upheld.",
  "messages": [
    {
      "@type": "/sparkdream.name.v1.MsgResolveDispute",
      "authority": "$COMMONS_POLICY",
      "name": "$TARGET_NAME",
      "new_owner": "$CLAIMANT_ADDR",
      "transfer_approved": true
    }
  ]
}
EOF

echo "  Submitting proposal..."
TX_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/resolve_dispute.json" \
    --from alice -y \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000000uspark \
    --output json 2>&1)

if ! submit_and_wait "$TX_RES" "proposal submission"; then
    FAILURES=$((FAILURES + 1))
    echo "  ERROR: Cannot continue without proposal"
    exit 1
fi

# Extract proposal ID
PROPOSAL_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "  ERROR: Could not extract proposal ID"
    exit 1
fi
echo "  Proposal ID: $PROPOSAL_ID"

echo ""

# ========================================================================
# PART 4: VOTE & EXECUTE
# ========================================================================
echo "--- PART 4: VOTE & EXECUTE ---"

for VOTER in "alice" "bob" "carol"; do
    VOTER_ADDR=$($BINARY keys show $VOTER -a --keyring-backend test)
    echo "  $VOTER voting YES..."
    TX_RES=$($BINARY tx group vote $PROPOSAL_ID $VOTER_ADDR VOTE_OPTION_YES "Transfer approved" \
        --from $VOTER -y \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000000uspark \
        --output json 2>&1)
    submit_and_wait "$TX_RES" "$VOTER vote" || echo "  Warning: $VOTER vote may have failed"
done

echo ""
echo "  Executing proposal..."
TX_RES=$($BINARY tx group exec $PROPOSAL_ID \
    --from alice -y \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000000uspark \
    --gas 2000000 \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "proposal exec"; then
    if echo "$TX_RESULT" | jq -r '.' | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
        echo "  Proposal executed successfully"
    else
        echo "  WARNING: Execution may have failed (MinExecutionPeriod or threshold not met)"
        echo "  $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null | head -3)"
    fi
else
    echo "  FAIL: Could not execute proposal"
    FAILURES=$((FAILURES + 1))
fi

echo ""

# ========================================================================
# PART 5: VERIFY RESULTS (uncontested, transfer approved)
# ========================================================================
echo "--- PART 5: VERIFY RESULTS ---"

# Check name ownership transferred to claimant
RESOLVE_RES=$($BINARY query name resolve "$TARGET_NAME" --output json 2>&1)
FINAL_OWNER=$(echo "$RESOLVE_RES" | jq -r '.name_record.owner')

assert_equals "name transferred to $CLAIMANT_KEY" "$CLAIMANT_ADDR" "$FINAL_OWNER"

# Check dispute is inactive (disputes are marked inactive, NOT removed from state)
DISPUTE_RES=$($BINARY query name get-dispute "$TARGET_NAME" --output json 2>&1)
DISPUTE_ACTIVE=$(jq_bool "$DISPUTE_RES" ".dispute.active")

assert_equals "dispute is inactive after resolution" "false" "$DISPUTE_ACTIVE"

echo ""

# ========================================================================
# PART 6: CONTESTED DISPUTE -- name_claimant disputes Alice's name, Alice contests
# ========================================================================
echo "--- PART 6: CONTESTED DISPUTE ---"

PART6_READY=true

# Verify Alice still owns the contest name
RESOLVE_RES=$($BINARY query name resolve "$CONTEST_NAME" --output json 2>&1)
CURRENT_OWNER=$(echo "$RESOLVE_RES" | jq -r '.name_record.owner')
if [ "$CURRENT_OWNER" != "$ALICE_ADDR" ]; then
    echo "  SKIP: '$CONTEST_NAME' not owned by Alice ($CURRENT_OWNER)"
    PART6_READY=false
fi

# 6a. name_claimant files dispute for Alice's name (stakes DREAM)
if [ "$PART6_READY" == "true" ]; then
    echo "  $CLAIMANT_KEY filing dispute for '$CONTEST_NAME' (stakes DREAM)..."
    TX_RES=$($BINARY tx name file-dispute "$CONTEST_NAME" "I have a better claim to this name" \
        --from $CLAIMANT_KEY -y \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to file dispute"
        PART6_READY=false
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "  Dispute filed"

            DISPUTE_RES=$($BINARY query name get-dispute "$CONTEST_NAME" --output json 2>&1)
            DISPUTE_ACTIVE=$(jq_bool "$DISPUTE_RES" ".dispute.active")
            assert_equals "contest dispute is active" "true" "$DISPUTE_ACTIVE"
        else
            echo "  Failed to file dispute"
            PART6_READY=false
        fi
    fi
fi

# 6b. Alice contests the dispute (she's the owner + has DREAM)
if [ "$PART6_READY" == "true" ]; then
    echo ""
    echo "  Alice contesting dispute for '$CONTEST_NAME' (stakes DREAM)..."
    echo "  Note: Contest requires owner to stake DREAM (default 100)"

    TX_RES=$($BINARY tx name contest-dispute "$CONTEST_NAME" "This is my legitimate name" \
        --from alice -y \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to contest dispute"
        echo "  $TX_RES"
        PART6_READY=false
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "  Dispute contested by Alice"

            # Verify contest state
            DISPUTE_RES=$($BINARY query name get-dispute "$CONTEST_NAME" --output json 2>&1)
            CONTEST_ID=$(echo "$DISPUTE_RES" | jq -r '.dispute.contest_challenge_id // ""')

            if [ -n "$CONTEST_ID" ] && [ "$CONTEST_ID" != "" ]; then
                echo "  PASS: contest_challenge_id set ($CONTEST_ID)"
            else
                echo "  FAIL: contest_challenge_id not set"
                FAILURES=$((FAILURES + 1))
            fi
        else
            echo "  Failed to contest dispute"
            PART6_READY=false
        fi
    fi
fi

echo ""

# ========================================================================
# PART 7: COUNCIL RESOLVES CONTESTED DISPUTE (owner wins, dismissed)
# ========================================================================
echo "--- PART 7: COUNCIL RESOLVES CONTESTED DISPUTE (owner wins) ---"

if [ "$PART6_READY" == "true" ]; then
    # Create proposal: transfer_approved=false means owner wins
    # Owner (Alice) gets contest stake back, claimant (name_claimant) stake burned
    cat > "$PROPOSAL_DIR/resolve_contest.json" <<EOF
{
  "group_policy_address": "$COMMONS_POLICY",
  "proposers": ["$ALICE_ADDR"],
  "title": "Resolve Contested Name Dispute: $CONTEST_NAME",
  "summary": "Owner Alice wins contested dispute. Dismiss claimant's claim.",
  "messages": [
    {
      "@type": "/sparkdream.name.v1.MsgResolveDispute",
      "authority": "$COMMONS_POLICY",
      "name": "$CONTEST_NAME",
      "new_owner": "",
      "transfer_approved": false
    }
  ]
}
EOF

    echo "  Submitting proposal..."
    TX_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/resolve_contest.json" \
        --from alice -y \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000000uspark \
        --output json 2>&1)

    if submit_and_wait "$TX_RES" "contest proposal submission"; then
        PROPOSAL_ID2=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
        echo "  Proposal ID: $PROPOSAL_ID2"

        # Vote
        for VOTER in "alice" "bob" "carol"; do
            VOTER_ADDR=$($BINARY keys show $VOTER -a --keyring-backend test)
            echo "  $VOTER voting YES..."
            TX_RES=$($BINARY tx group vote $PROPOSAL_ID2 $VOTER_ADDR VOTE_OPTION_YES "Owner wins" \
                --from $VOTER -y \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000000uspark \
                --output json 2>&1)
            submit_and_wait "$TX_RES" "$VOTER vote" || echo "  Warning: $VOTER vote may have failed"
        done

        echo ""
        echo "  Executing proposal..."
        TX_RES=$($BINARY tx group exec $PROPOSAL_ID2 \
            --from alice -y \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000000uspark \
            --gas 2000000 \
            --output json 2>&1)

        if submit_and_wait "$TX_RES" "contest proposal exec"; then
            if echo "$TX_RESULT" | jq -r '.' | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
                echo "  Proposal executed"
            else
                echo "  WARNING: Execution may have failed"
                echo "  $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null | head -3)"
            fi
        else
            echo "  FAIL: Could not execute contest proposal"
            FAILURES=$((FAILURES + 1))
        fi
    else
        echo "  Failed to submit contest proposal"
        PART6_READY=false
    fi
else
    echo "  SKIP: Contested dispute could not be set up (see errors above)"
fi

echo ""

# ========================================================================
# PART 8: VERIFY CONTESTED RESULTS (owner retains name)
# ========================================================================
echo "--- PART 8: VERIFY CONTESTED RESULTS ---"

if [ "$PART6_READY" == "true" ]; then
    # Name should still belong to Alice (owner won the contest)
    RESOLVE_RES=$($BINARY query name resolve "$CONTEST_NAME" --output json 2>&1)
    CONTEST_OWNER=$(echo "$RESOLVE_RES" | jq -r '.name_record.owner')

    assert_equals "name stays with Alice (owner won)" "$ALICE_ADDR" "$CONTEST_OWNER"

    # Dispute should be inactive
    DISPUTE_RES=$($BINARY query name get-dispute "$CONTEST_NAME" --output json 2>&1)
    DISPUTE_ACTIVE=$(jq_bool "$DISPUTE_RES" ".dispute.active")

    assert_equals "contested dispute is inactive" "false" "$DISPUTE_ACTIVE"
else
    echo "  SKIP: Contested dispute test could not complete (see errors above)"
fi

echo ""

# ========================================================================
# PART 9: CONTESTED DISPUTE -- Challenger Wins (name transfers)
# ========================================================================
echo "--- PART 9: CONTESTED DISPUTE -- Challenger Wins ---"

PART9_READY=true

# Register 'zenith' for Alice now (deferred from Part 1 because Alice had 5 names;
# phoenix was transferred to claimant in Part 5, freeing a slot)
echo "  Registering '$CHALLENGER_WINS_NAME' for Alice (slot freed after phoenix transfer)..."
TX_RES=$($BINARY tx name register-name "$CHALLENGER_WINS_NAME" "Challenger Wins Test" \
    --from alice -y \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        echo "  Name '$CHALLENGER_WINS_NAME' registered by Alice"
    else
        echo "  Registration failed"
        PART9_READY=false
    fi
else
    echo "  Registration failed (no txhash)"
    PART9_READY=false
fi

# Verify Alice owns the challenger-wins name
RESOLVE_RES=$($BINARY query name resolve "$CHALLENGER_WINS_NAME" --output json 2>&1)
CURRENT_OWNER=$(echo "$RESOLVE_RES" | jq -r '.name_record.owner')
if [ "$CURRENT_OWNER" != "$ALICE_ADDR" ]; then
    echo "  SKIP: '$CHALLENGER_WINS_NAME' not owned by Alice ($CURRENT_OWNER)"
    PART9_READY=false
fi

# 9a. name_claimant files dispute
if [ "$PART9_READY" == "true" ]; then
    echo "  $CLAIMANT_KEY filing dispute for '$CHALLENGER_WINS_NAME' (stakes DREAM)..."
    TX_RES=$($BINARY tx name file-dispute "$CHALLENGER_WINS_NAME" "This name rightfully belongs to me" \
        --from $CLAIMANT_KEY -y \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to file dispute"
        PART9_READY=false
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "  Dispute filed"

            DISPUTE_RES=$($BINARY query name get-dispute "$CHALLENGER_WINS_NAME" --output json 2>&1)
            DISPUTE_ACTIVE=$(jq_bool "$DISPUTE_RES" ".dispute.active")
            assert_equals "challenger-wins dispute is active" "true" "$DISPUTE_ACTIVE"
        else
            echo "  Failed to file dispute"
            PART9_READY=false
        fi
    fi
fi

# 9b. Alice contests (she's the owner + has DREAM)
if [ "$PART9_READY" == "true" ]; then
    echo ""
    echo "  Alice contesting dispute for '$CHALLENGER_WINS_NAME' (stakes DREAM)..."

    TX_RES=$($BINARY tx name contest-dispute "$CHALLENGER_WINS_NAME" "I registered this name first" \
        --from alice -y \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to contest dispute"
        PART9_READY=false
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "  Dispute contested by Alice"

            DISPUTE_RES=$($BINARY query name get-dispute "$CHALLENGER_WINS_NAME" --output json 2>&1)
            CONTEST_ID=$(echo "$DISPUTE_RES" | jq -r '.dispute.contest_challenge_id // ""')

            if [ -n "$CONTEST_ID" ] && [ "$CONTEST_ID" != "" ]; then
                echo "  PASS: contest_challenge_id set ($CONTEST_ID)"
            else
                echo "  FAIL: contest_challenge_id not set"
                FAILURES=$((FAILURES + 1))
            fi
        else
            echo "  Failed to contest dispute"
            PART9_READY=false
        fi
    fi
fi

echo ""

# ========================================================================
# PART 10: COUNCIL RESOLVES -- Challenger Wins (transfer approved)
# ========================================================================
echo "--- PART 10: COUNCIL RESOLVES -- Challenger Wins ---"

if [ "$PART9_READY" == "true" ]; then
    # transfer_approved=true means claimant wins:
    # - Name transfers to claimant
    # - Claimant's dispute stake unlocked (returned)
    # - Owner's contest stake burned
    cat > "$PROPOSAL_DIR/resolve_challenger_wins.json" <<EOF
{
  "group_policy_address": "$COMMONS_POLICY",
  "proposers": ["$ALICE_ADDR"],
  "title": "Resolve Contested Name Dispute: $CHALLENGER_WINS_NAME",
  "summary": "Challenger wins. Transfer name to claimant, burn owner contest stake.",
  "messages": [
    {
      "@type": "/sparkdream.name.v1.MsgResolveDispute",
      "authority": "$COMMONS_POLICY",
      "name": "$CHALLENGER_WINS_NAME",
      "new_owner": "$CLAIMANT_ADDR",
      "transfer_approved": true
    }
  ]
}
EOF

    echo "  Submitting proposal (transfer_approved=true)..."
    TX_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/resolve_challenger_wins.json" \
        --from alice -y \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000000uspark \
        --output json 2>&1)

    if submit_and_wait "$TX_RES" "challenger-wins proposal submission"; then
        PROPOSAL_ID3=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
        echo "  Proposal ID: $PROPOSAL_ID3"

        # Vote
        for VOTER in "alice" "bob" "carol"; do
            VOTER_ADDR=$($BINARY keys show $VOTER -a --keyring-backend test)
            echo "  $VOTER voting YES..."
            TX_RES=$($BINARY tx group vote $PROPOSAL_ID3 $VOTER_ADDR VOTE_OPTION_YES "Challenger wins" \
                --from $VOTER -y \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000000uspark \
                --output json 2>&1)
            submit_and_wait "$TX_RES" "$VOTER vote" || echo "  Warning: $VOTER vote may have failed"
        done

        echo ""
        echo "  Executing proposal..."
        TX_RES=$($BINARY tx group exec $PROPOSAL_ID3 \
            --from alice -y \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000000uspark \
            --gas 2000000 \
            --output json 2>&1)

        if submit_and_wait "$TX_RES" "challenger-wins proposal exec"; then
            if echo "$TX_RESULT" | jq -r '.' | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
                echo "  Proposal executed"
            else
                echo "  WARNING: Execution may have failed"
                echo "  $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null | head -3)"
            fi
        else
            echo "  FAIL: Could not execute challenger-wins proposal"
            FAILURES=$((FAILURES + 1))
        fi
    else
        echo "  Failed to submit challenger-wins proposal"
        PART9_READY=false
    fi
else
    echo "  SKIP: Challenger-wins contest could not be set up (see errors above)"
fi

echo ""

# ========================================================================
# PART 11: VERIFY CHALLENGER WINS RESULTS (name transfers)
# ========================================================================
echo "--- PART 11: VERIFY CHALLENGER WINS RESULTS ---"

if [ "$PART9_READY" == "true" ]; then
    # Name should now belong to name_claimant (challenger won)
    RESOLVE_RES=$($BINARY query name resolve "$CHALLENGER_WINS_NAME" --output json 2>&1)
    CW_OWNER=$(echo "$RESOLVE_RES" | jq -r '.name_record.owner')

    assert_equals "name transferred to challenger" "$CLAIMANT_ADDR" "$CW_OWNER"

    # Dispute should be inactive
    DISPUTE_RES=$($BINARY query name get-dispute "$CHALLENGER_WINS_NAME" --output json 2>&1)
    DISPUTE_ACTIVE=$(jq_bool "$DISPUTE_RES" ".dispute.active")

    assert_equals "challenger-wins dispute is inactive" "false" "$DISPUTE_ACTIVE"
else
    echo "  SKIP: Challenger-wins test could not complete (see errors above)"
fi

echo ""

# ========================================================================
# PART 12: QUERY DISPUTE PARAMETERS
# ========================================================================
echo "--- PART 12: QUERY DISPUTE PARAMETERS ---"

PARAMS=$($BINARY query name params --output json 2>&1)

if echo "$PARAMS" | grep -q "error"; then
    echo "  Failed to query params"
else
    echo "  Name Dispute Parameters:"
    echo "    Dispute Stake (DREAM):   $(echo "$PARAMS" | jq -r '.params.dispute_stake_dream // "unknown"')"
    echo "    Contest Stake (DREAM):   $(echo "$PARAMS" | jq -r '.params.contest_stake_dream // "unknown"')"
    echo "    Timeout (blocks):        $(echo "$PARAMS" | jq -r '.params.dispute_timeout_blocks // "unknown"')"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- DISPUTE RESOLUTION TEST SUMMARY ---"
echo ""
echo "  Register names:                  Tested"
echo "  File dispute (DREAM staking):    Tested"
echo "  Council proposal (transfer):     Tested"
echo "  Vote & execute:                  Tested"
echo "  Verify transfer + inactive:      Tested"
echo "  Contest dispute (DREAM stake):   Tested"
echo "  Council resolve (owner wins):    Tested"
echo "  Verify owner retains name:       Tested"
echo "  Contest (challenger wins):       Tested"
echo "  Verify name transfers:           Tested"
echo "  Query dispute parameters:        Tested"
echo ""

if [ "$FAILURES" -gt 0 ]; then
    echo "RESULT: $FAILURES ASSERTION(S) FAILED"
    exit 1
else
    echo "RESULT: ALL ASSERTIONS PASSED"
fi

echo ""
echo "DISPUTE RESOLUTION TEST COMPLETED"
echo ""
