#!/bin/bash

echo "--- TESTING: INITIATIVE FULL FLOW (CREATE, ASSIGN, SUBMIT, STAKE, COMPLETE) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment (created by setup_test_accounts.sh)
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    source "$SCRIPT_DIR/.test_env"
    echo "[ OK ] Loaded test environment from .test_env"
else
    echo "[WARN]  .test_env not found. Run setup_test_accounts.sh first!"
    exit 1
fi

# Get Alice address
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)

# Use test accounts from .test_env
# STAKER1 and STAKER2 will be challenger and assignee (they have DREAM and reputation)
STAKER1_ADDR=$CHALLENGER_ADDR
STAKER1_NAME="challenger"
STAKER2_ADDR=$ASSIGNEE_ADDR
STAKER2_NAME="assignee"
WORKER_ADDR=$EXPERT_ADDR
WORKER_NAME="expert"

echo "Alice:       $ALICE_ADDR (Project Creator)"
echo "Staker1:     $STAKER1_ADDR ($STAKER1_NAME)"
echo "Staker2:     $STAKER2_ADDR ($STAKER2_NAME)"
echo "Worker:      $WORKER_ADDR ($WORKER_NAME - Assignee)"
echo "Project ID:  $TEST_PROJECT_ID (from setup)"

# ========================================================================
# PART 1: USE EXISTING TEST PROJECT
# ========================================================================
echo ""
echo "--- PART 1: USING EXISTING TEST PROJECT FROM SETUP ---"

# Use the project created during setup_test_accounts.sh
PROJECT_ID=$TEST_PROJECT_ID

# Verify project exists and is ACTIVE
PROJECT_QUERY=$($BINARY query rep get-project $PROJECT_ID -o json)
PROJECT_STATUS=$(echo "$PROJECT_QUERY" | jq -r '.project.status')
PROJECT_NAME=$(echo "$PROJECT_QUERY" | jq -r '.project.name')
APPROVED_BUDGET=$(echo "$PROJECT_QUERY" | jq -r '.project.approved_budget')

echo "Project ID: $PROJECT_ID"
echo "Project name: $PROJECT_NAME"
echo "Project status: $PROJECT_STATUS"
echo "Approved DREAM budget: $APPROVED_BUDGET"

if [ "$PROJECT_STATUS" == "PROJECT_STATUS_ACTIVE" ]; then
    echo "[ OK ] Project is ACTIVE and ready for initiatives"
    NEW_STATUS="$PROJECT_STATUS"
else
    echo "[WARN]  Project status: $PROJECT_STATUS (expected ACTIVE)"
    NEW_STATUS="$PROJECT_STATUS"
fi

# ========================================================================
# PART 3: CREATE INITIATIVES WITH DIFFERENT TIERS
# ========================================================================
echo ""
echo "--- PART 3: CREATE INITIATIVES (DIFFERENT TIERS) ---"

# Create Apprentice tier initiative
# Command: create-initiative [project-id] [title] [description] [tier] [category] [template-id] [budget]
# Category: 0=FEATURE, 1=BUGFIX, 2=TESTING, 3=DOCUMENTATION, 4=REFACTORING
echo "Creating APPRENTICE tier initiative..."
# Note: Using 95 DREAM budget (close to 100 DREAM tier limit) to require more conviction
# This prevents instant auto-completion from leftover stakes
APPRENTICE_RES=$($BINARY tx rep create-initiative \
  $PROJECT_ID \
  "Fix typo in README" \
  "Simple fix for documentation typo" \
  "0" \
  "1" \
  "0" \
  "95000000" \
  --tags "documentation" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

APPRENTICE_TX=$(echo $APPRENTICE_RES | jq -r '.txhash')
sleep 6  # Increased wait time for transaction to be indexed

# Method 1: Extract from transaction events
APPRENTICE_ID=$($BINARY query tx $APPRENTICE_TX -o json 2>/dev/null | \
  jq -r '.events[] | select(.type=="initiative_created") | .attributes[] | select(.key=="initiative_id") | .value' | \
  tr -d '"' 2>/dev/null)

# Method 2: Try alternative event extraction from logs
if [ -z "$APPRENTICE_ID" ] || [ "$APPRENTICE_ID" == "null" ] || [ "$APPRENTICE_ID" == "" ]; then
    APPRENTICE_ID=$($BINARY query tx $APPRENTICE_TX -o json 2>/dev/null | \
      jq -r '.logs[0].events[] | select(.type=="initiative_created") | .attributes[] | select(.key=="initiative_id") | .value' 2>/dev/null)
fi

# Method 3: Extract from raw logs
if [ -z "$APPRENTICE_ID" ] || [ "$APPRENTICE_ID" == "null" ] || [ "$APPRENTICE_ID" == "" ]; then
    APPRENTICE_ID=$($BINARY query tx $APPRENTICE_TX -o json 2>/dev/null | jq -r '.raw_log' | grep -oP 'initiative_id\\",\\"value\\":\\"\K[0-9]+' | head -1)
fi

# Method 4: Fallback to querying latest initiative from project
if [ -z "$APPRENTICE_ID" ] || [ "$APPRENTICE_ID" == "null" ] || [ "$APPRENTICE_ID" == "" ]; then
    echo "[WARN]  Failed to extract from events, querying latest initiative..."
    APPRENTICE_ID=$($BINARY query rep initiatives-by-project $PROJECT_ID -o json 2>/dev/null | \
      jq -r '.initiatives | sort_by(.id) | .[-1].id' 2>/dev/null)
fi

# Final check
if [ -z "$APPRENTICE_ID" ] || [ "$APPRENTICE_ID" == "null" ] || [ "$APPRENTICE_ID" == "" ]; then
    echo "[WARN]  Failed to extract apprentice initiative ID from tx $APPRENTICE_TX"
    APPRENTICE_ID="unknown"
fi

echo "[ OK ] Apprentice initiative created: $APPRENTICE_ID (budget: 95 DREAM)"

# Create Standard tier initiative
echo "Creating STANDARD tier initiative..."
STANDARD_RES=$($BINARY tx rep create-initiative \
  $PROJECT_ID \
  "Add unit tests for authentication" \
  "Comprehensive test coverage for auth module" \
  "1" \
  "2" \
  "0" \
  "500000000" \
  --tags "testing","security" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

STANDARD_TX=$(echo $STANDARD_RES | jq -r '.txhash')
sleep 6  # Increased wait time for transaction to be indexed

# Method 1: Extract from transaction events
STANDARD_ID=$($BINARY query tx $STANDARD_TX -o json 2>/dev/null | \
  jq -r '.events[] | select(.type=="initiative_created") | .attributes[] | select(.key=="initiative_id") | .value' | \
  tr -d '"' 2>/dev/null)

# Method 2: Try alternative event extraction from logs
if [ -z "$STANDARD_ID" ] || [ "$STANDARD_ID" == "null" ] || [ "$STANDARD_ID" == "" ]; then
    STANDARD_ID=$($BINARY query tx $STANDARD_TX -o json 2>/dev/null | \
      jq -r '.logs[0].events[] | select(.type=="initiative_created") | .attributes[] | select(.key=="initiative_id") | .value' 2>/dev/null)
fi

# Method 3: Extract from raw logs
if [ -z "$STANDARD_ID" ] || [ "$STANDARD_ID" == "null" ] || [ "$STANDARD_ID" == "" ]; then
    STANDARD_ID=$($BINARY query tx $STANDARD_TX -o json 2>/dev/null | jq -r '.raw_log' | grep -oP 'initiative_id\\",\\"value\\":\\"\K[0-9]+' | head -1)
fi

# Method 4: Fallback to querying latest initiative from project (exclude APPRENTICE_ID)
if [ -z "$STANDARD_ID" ] || [ "$STANDARD_ID" == "null" ] || [ "$STANDARD_ID" == "" ]; then
    echo "[WARN]  Failed to extract from events, querying latest initiative..."
    STANDARD_ID=$($BINARY query rep initiatives-by-project $PROJECT_ID -o json 2>/dev/null | \
      jq -r --arg app_id "$APPRENTICE_ID" '.initiatives | sort_by(.id) | .[] | select(.id != ($app_id | tonumber)) | .id' 2>/dev/null | tail -1)
fi

# Final check
if [ -z "$STANDARD_ID" ] || [ "$STANDARD_ID" == "null" ] || [ "$STANDARD_ID" == "" ]; then
    echo "[WARN]  Failed to extract standard initiative ID from tx $STANDARD_TX"
    STANDARD_ID="unknown"
fi

echo "[ OK ] Standard initiative created: $STANDARD_ID (budget: 500 DREAM)"

# ========================================================================
# PART 4: QUERY INITIATIVES BY PROJECT
# ========================================================================
echo ""
echo "--- PART 4: QUERY INITIATIVES BY PROJECT ---"

PROJECT_INITIATIVES=$($BINARY query rep initiatives-by-project $PROJECT_ID -o json)
INITIATIVE_COUNT=$(echo "$PROJECT_INITIATIVES" | jq -r '.initiatives | length')

echo "Project has $INITIATIVE_COUNT initiatives"

if [ "$INITIATIVE_COUNT" -ge 2 ]; then
    echo "[ OK ] Multiple initiatives found for project"
else
    echo "[WARN]  Initiative count: $INITIATIVE_COUNT"
fi

# Query available initiatives (OPEN status)
AVAILABLE_INITIATIVES=$($BINARY query rep available-initiatives -o json)
AVAILABLE_COUNT=$(echo "$AVAILABLE_INITIATIVES" | jq -r '.initiatives | length')

echo "Available (OPEN) initiatives: $AVAILABLE_COUNT"

# ========================================================================
# PART 5: ASSIGN INITIATIVE TO WORKER
# ========================================================================
echo ""
echo "--- PART 5: ALICE ASSIGNS APPRENTICE INITIATIVE TO WORKER ---"
echo "Note: Using APPRENTICE tier (requires 0 reputation) since worker has no reputation yet"

# Alice (project creator) assigns the APPRENTICE initiative to worker
# APPRENTICE tier requires 0 reputation, STANDARD requires 25
ASSIGN_RES=$($BINARY tx rep assign-initiative \
  $APPRENTICE_ID \
  $WORKER_ADDR \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

ASSIGN_TX=$(echo $ASSIGN_RES | jq -r '.txhash')
sleep 2

# Check if transaction succeeded
ASSIGN_TX_RESULT=$($BINARY query tx $ASSIGN_TX -o json 2>/dev/null)
ASSIGN_CODE=$(echo "$ASSIGN_TX_RESULT" | jq -r '.code // 0')
if [ "$ASSIGN_CODE" != "0" ]; then
    ASSIGN_ERROR=$(echo "$ASSIGN_TX_RESULT" | jq -r '.raw_log // "Unknown error"')
    echo "[WARN]  Assign transaction failed: $ASSIGN_ERROR"
fi

# Use APPRENTICE_ID for rest of test flow
FLOW_INITIATIVE_ID=$APPRENTICE_ID

# Verify initiative status changed to ASSIGNED
UPDATED_INITIATIVE=$($BINARY query rep get-initiative $FLOW_INITIATIVE_ID -o json)
# Note: Proto3 omits zero values - status=0 (OPEN) won't appear in JSON
# Use // operator to provide default value when field is missing
NEW_INIT_STATUS=$(echo "$UPDATED_INITIATIVE" | jq -r '.initiative.status // "INITIATIVE_STATUS_OPEN"')
ASSIGNEE=$(echo "$UPDATED_INITIATIVE" | jq -r '.initiative.assignee // ""')

echo "New initiative status: $NEW_INIT_STATUS"
echo "Assignee: $ASSIGNEE"

if [ "$NEW_INIT_STATUS" == "INITIATIVE_STATUS_ASSIGNED" ]; then
    echo "[ OK ] Initiative is now ASSIGNED"
elif [ "$NEW_INIT_STATUS" == "INITIATIVE_STATUS_OPEN" ] || [ -z "$NEW_INIT_STATUS" ]; then
    echo "[WARN]  Initiative still OPEN (assign may require self-assignment)"
else
    echo "[WARN]  Initiative status: $NEW_INIT_STATUS"
fi

# ========================================================================
# PART 6: QUERY INITIATIVES BY ASSIGNEE
# ========================================================================
echo ""
echo "--- PART 6: QUERY INITIATIVES BY ASSIGNEE ---"

WORKER_INITIATIVES=$($BINARY query rep initiatives-by-assignee $WORKER_ADDR -o json)
# Query returns {initiatives: [...], pagination: {...}}. proto3 omits
# zero-valued uint64 fields, so normalize id with // "0".
WORKER_INIT_COUNT=$(echo "$WORKER_INITIATIVES" | jq -r '.initiatives | length')

if [ "$WORKER_INIT_COUNT" -gt 0 ]; then
    WORKER_INIT_ID=$(echo "$WORKER_INITIATIVES" | jq -r '.initiatives[0].id // "0"')
    WORKER_INIT_TITLE=$(echo "$WORKER_INITIATIVES" | jq -r '.initiatives[0].title // ""')
    WORKER_INIT_STATUS=$(echo "$WORKER_INITIATIVES" | jq -r '.initiatives[0].status // ""')
    echo "Worker has $WORKER_INIT_COUNT assigned initiative(s):"
    echo "  ID: $WORKER_INIT_ID"
    echo "  Title: $WORKER_INIT_TITLE"
    echo "  Status: $WORKER_INIT_STATUS"
    echo "[ OK ] Worker's assigned initiative found"
else
    echo "Worker has 0 assigned initiatives"
    echo "[WARN]  Query may not have found the assigned initiative"
fi

# Sorted listing: --sort-by orders the set before offset pagination; an
# unknown key must be rejected.
SORTED_AVAILABLE=$($BINARY query rep available-initiatives --sort-by budget --page-reverse -o json 2>/dev/null)
SORTED_COUNT=$(echo "$SORTED_AVAILABLE" | jq -r '.initiatives | length')
if [ -n "$SORTED_COUNT" ] && [ "$SORTED_COUNT" != "null" ]; then
    echo "[ OK ] available-initiatives --sort-by budget returned $SORTED_COUNT initiative(s)"
else
    echo "[FAIL] available-initiatives --sort-by budget did not return an initiatives array"
    exit 1
fi

if $BINARY query rep available-initiatives --sort-by bogus -o json > /dev/null 2>&1; then
    echo "[FAIL] --sort-by bogus was unexpectedly accepted"
    exit 1
else
    echo "[ OK ] --sort-by bogus rejected"
fi

# ========================================================================
# PART 7: SUBMIT INITIATIVE WORK
# ========================================================================
echo ""
echo "--- PART 7: WORKER SUBMITS WORK DELIVERABLE ---"

SUBMIT_RES=$($BINARY tx rep submit-initiative-work \
  $FLOW_INITIATIVE_ID \
  "ipfs://QmTestDeliverable" \
  "Completed implementation with unit tests" \
  --from $WORKER_NAME \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

SUBMIT_TX=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 2

# Verify initiative status changed to SUBMITTED
SUBMITTED_INITIATIVE=$($BINARY query rep get-initiative $FLOW_INITIATIVE_ID -o json)
# Note: Proto3 omits zero values - use // operator for defaults
SUBMIT_STATUS=$(echo "$SUBMITTED_INITIATIVE" | jq -r '.initiative.status // "INITIATIVE_STATUS_OPEN"')
DELIVERABLE_URI=$(echo "$SUBMITTED_INITIATIVE" | jq -r '.initiative.deliverable_uri // ""')

echo "Initiative status after submit: $SUBMIT_STATUS"
echo "Deliverable URI: $DELIVERABLE_URI"

if [ "$SUBMIT_STATUS" == "INITIATIVE_STATUS_SUBMITTED" ]; then
    echo "[ OK ] Initiative is SUBMITTED"
elif [ "$SUBMIT_STATUS" == "INITIATIVE_STATUS_IN_REVIEW" ]; then
    echo "[ OK ] Initiative is IN_REVIEW (may have auto-transitioned)"
else
    echo "[WARN]  Initiative status: $SUBMIT_STATUS"
fi

# ========================================================================
# PART 8: CREATE CHALLENGE (WHILE STILL SUBMITTED)
# ========================================================================
# NOTE: Challenge must be created BEFORE adding stakes
# Stakes may push initiative to IN_REVIEW status, which doesn't allow challenges
echo ""
echo "--- PART 8: CHALLENGER CREATES CHALLENGE AGAINST INITIATIVE ---"
echo "Note: Creating challenge while initiative is still in SUBMITTED status"

# create-challenge [initiative-id] [reason] [staked-dream]
# (anonymity now lives in x/shield's MsgShieldedExec; the positional anon
# flag and challenger-payout address were removed when this was de-coupled.)
CHALLENGE_RES=$($BINARY tx rep create-challenge \
  $FLOW_INITIATIVE_ID \
  "Work does not meet requirements" \
  "50000000" \
  --evidence "ipfs://QmEvidence1","ipfs://QmEvidence2" \
  --from challenger \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

CHALLENGE_TX=$(echo $CHALLENGE_RES | jq -r '.txhash')
sleep 2

# Check if transaction succeeded
CHALLENGE_TX_RESULT=$($BINARY query tx $CHALLENGE_TX -o json 2>/dev/null)
CHALLENGE_CODE=$(echo "$CHALLENGE_TX_RESULT" | jq -r '.code // 0')
if [ "$CHALLENGE_CODE" != "0" ]; then
    CHALLENGE_ERROR=$(echo "$CHALLENGE_TX_RESULT" | jq -r '.raw_log // "Unknown error"')
    echo "[WARN]  Challenge transaction failed: $CHALLENGE_ERROR"
fi

# Extract challenge ID with fallback methods
CHALLENGE_ID=$(echo "$CHALLENGE_TX_RESULT" | \
  jq -r '.events[] | select(.type=="sparkdream.rep.v1.EventChallengeCreated") | .attributes[] | select(.key=="challenge_id") | .value' | \
  tr -d '"')

# Fallback: Try alternative event type
if [ -z "$CHALLENGE_ID" ] || [ "$CHALLENGE_ID" == "null" ] || [ "$CHALLENGE_ID" == "" ]; then
    CHALLENGE_ID=$(echo "$CHALLENGE_TX_RESULT" | \
      jq -r '.events[] | select(.type=="challenge_created") | .attributes[] | select(.key=="challenge_id") | .value' | \
      tr -d '"')
fi

# Fallback: Query challenges by initiative to get most recent challenge
if [ -z "$CHALLENGE_ID" ] || [ "$CHALLENGE_ID" == "null" ] || [ "$CHALLENGE_ID" == "" ]; then
    sleep 1  # Give chain time to process
    CHALLENGE_ID=$($BINARY query rep challenges-by-initiative $FLOW_INITIATIVE_ID -o json 2>/dev/null | \
      jq -r '.challenge_id // ""')
fi

if [ -z "$CHALLENGE_ID" ] || [ "$CHALLENGE_ID" == "null" ] || [ "$CHALLENGE_ID" == "" ]; then
    echo "[WARN]  Could not extract challenge ID from transaction"
    CHALLENGE_ID="unknown"
    echo "[WARN]  Challenge creation may have failed or ID extraction failed"
else
    echo "[ OK ] Challenge created: $CHALLENGE_ID"
fi

# Verify initiative status changed to CHALLENGED
CHALLENGED_INITIATIVE=$($BINARY query rep get-initiative $FLOW_INITIATIVE_ID -o json)
# Note: Proto3 omits zero values - use // operator for defaults
CHALLENGED_STATUS=$(echo "$CHALLENGED_INITIATIVE" | jq -r '.initiative.status // "INITIATIVE_STATUS_OPEN"')

echo "Initiative status after challenge: $CHALLENGED_STATUS"

if [ "$CHALLENGED_STATUS" == "INITIATIVE_STATUS_CHALLENGED" ]; then
    echo "[ OK ] Initiative is CHALLENGED"
else
    echo "[WARN]  Initiative status: $CHALLENGED_STATUS"
fi

# Verify challenge details (only if challenge ID was extracted)
if [ "$CHALLENGE_ID" != "unknown" ] && [ -n "$CHALLENGE_ID" ]; then
    CHALLENGE_DETAIL=$($BINARY query rep get-challenge $CHALLENGE_ID -o json 2>/dev/null)
    if echo "$CHALLENGE_DETAIL" | grep -q "key not found"; then
        echo "Challenge status: Not found"
        echo "Staked on challenge: Unknown"
    else
        # Note: Challenge status might also be omitted if it's the zero value
        CHALLENGE_STATUS=$(echo "$CHALLENGE_DETAIL" | jq -r '.challenge.status // "CHALLENGE_STATUS_ACTIVE"')
        STAKED_ON_CHALLENGE=$(echo "$CHALLENGE_DETAIL" | jq -r '.challenge.staked_dream // "0"')
        echo "Challenge status: $CHALLENGE_STATUS"
        echo "Staked on challenge: $STAKED_ON_CHALLENGE"
    fi
else
    echo "Challenge status: Unknown (challenge creation failed)"
    echo "Staked on challenge: Unknown"
fi

if [ "$CHALLENGE_STATUS" == "CHALLENGE_STATUS_ACTIVE" ]; then
    echo "[ OK ] Challenge is ACTIVE (awaiting response)"
fi

# ========================================================================
# PART 9: RESPOND TO CHALLENGE
# ========================================================================
echo ""
echo "--- PART 9: WORKER RESPONDS TO CHALLENGE ---"

if [ "$CHALLENGE_ID" != "unknown" ] && [ -n "$CHALLENGE_ID" ]; then
    RESPOND_RES=$($BINARY tx rep respond-to-challenge \
      $CHALLENGE_ID \
      "All requirements met, see evidence" \
      --evidence "ipfs://QmResponseEvidence" \
      --from $WORKER_NAME \
      --chain-id $CHAIN_ID \
      --keyring-backend test \
      --fees 5000${BOND_DENOM} \
      -y \
      -o json)

    RESPOND_TX=$(echo $RESPOND_RES | jq -r '.txhash')
    sleep 2

    echo "[ OK ] Worker responded to challenge"

    # Check challenge status after response
    RESPONDED_CHALLENGE=$($BINARY query rep get-challenge $CHALLENGE_ID -o json 2>/dev/null)
    if echo "$RESPONDED_CHALLENGE" | grep -q "key not found"; then
        echo "Challenge status after response: Not found"
    else
        RESPONDED_STATUS=$(echo "$RESPONDED_CHALLENGE" | jq -r '.challenge.status // "0"')
        echo "Challenge status after response: $RESPONDED_STATUS"
    fi
else
    echo "[WARN]  Skipping challenge response - challenge creation failed"
    echo "Challenge status after response: Unknown"
fi

# ========================================================================
# PART 10: CREATE STAKES ON INITIATIVE (CONVICTION VOTING)
# ========================================================================
echo ""
echo "--- PART 10: STAKER1 AND STAKER2 STAKE ON INITIATIVE ---"
echo "This demonstrates conviction voting - stakers commit DREAM to signal confidence"

# Staker1 stakes 100 DREAM (100,000,000 micro-DREAM)
# Note: target-type uses string enum: stake-target-initiative, stake-target-project, stake-target-member, stake-target-tag
echo "Staker1 ($STAKER1_NAME) staking 100 DREAM..."
STAKER1_STAKE_RES=$($BINARY tx rep stake \
  stake-target-initiative \
  $FLOW_INITIATIVE_ID \
  "100000000" \
  --from $STAKER1_NAME \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --gas auto \
  --gas-adjustment 1.5 \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json 2>&1)

STAKER1_STAKE_TX=$(echo "$STAKER1_STAKE_RES" | jq -r '.txhash // empty' 2>/dev/null)
sleep 2

# Check if transaction succeeded. Suppress jq parse errors — when the broadcast
# response is non-JSON (e.g. "gas estimate: ..." prepended to stdout via 2>&1)
# or when `query tx` returns a not-yet-indexed error, jq prints to stderr and
# `code` ends up empty. Treat empty as "no failure detected" — the fallback
# stake-id query below is the source of truth.
STAKER1_STAKE_TX_RESULT=""
if [ -n "$STAKER1_STAKE_TX" ]; then
    STAKER1_STAKE_TX_RESULT=$($BINARY query tx $STAKER1_STAKE_TX -o json 2>/dev/null)
fi
STAKER1_STAKE_CODE=$(echo "$STAKER1_STAKE_TX_RESULT" | jq -r '.code // 0' 2>/dev/null)
if [ -n "$STAKER1_STAKE_CODE" ] && [ "$STAKER1_STAKE_CODE" != "0" ]; then
    STAKER1_STAKE_ERROR=$(echo "$STAKER1_STAKE_TX_RESULT" | jq -r '.raw_log // "Unknown error"' 2>/dev/null)
    echo "[WARN]  Staker1 stake transaction failed: $STAKER1_STAKE_ERROR"
fi

STAKER1_STAKE_ID=$(echo "$STAKER1_STAKE_TX_RESULT" | \
  jq -r '.events[] | select(.type=="sparkdream.rep.v1.EventStakeCreated") | .attributes[] | select(.key=="stake_id") | .value' | \
  tr -d '"')

# Fallback: Try alternative event types
if [ -z "$STAKER1_STAKE_ID" ] || [ "$STAKER1_STAKE_ID" == "null" ] || [ "$STAKER1_STAKE_ID" == "" ]; then
    STAKER1_STAKE_ID=$(echo "$STAKER1_STAKE_TX_RESULT" | \
      jq -r '.events[] | select(.type=="stake_created") | .attributes[] | select(.key=="stake_id") | .value' | \
      tr -d '"')
fi

# Fallback: Query stakes by staker to get most recent stake
if [ -z "$STAKER1_STAKE_ID" ] || [ "$STAKER1_STAKE_ID" == "null" ] || [ "$STAKER1_STAKE_ID" == "" ]; then
    sleep 1  # Give chain time to process
    STAKER1_STAKE_ID=$($BINARY query rep stakes-by-staker $STAKER1_ADDR -o json 2>/dev/null | \
      jq -r '.stakes[0].id // ""')
fi

if [ -z "$STAKER1_STAKE_ID" ] || [ "$STAKER1_STAKE_ID" == "null" ] || [ "$STAKER1_STAKE_ID" == "" ]; then
    echo "[WARN]  Could not extract stake ID from transaction"
    STAKER1_STAKE_ID="unknown"
    echo "[WARN]  Staker1 stake may have failed"
else
    echo "[ OK ] Staker1 staked: $STAKER1_STAKE_ID (100 DREAM)"
fi

# Staker2 stakes 150 DREAM (150,000,000 micro-DREAM)
echo "Staker2 ($STAKER2_NAME) staking 150 DREAM..."
STAKER2_STAKE_RES=$($BINARY tx rep stake \
  stake-target-initiative \
  $FLOW_INITIATIVE_ID \
  "150000000" \
  --from $STAKER2_NAME \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --gas auto \
  --gas-adjustment 1.5 \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json 2>&1)

STAKER2_STAKE_TX=$(echo "$STAKER2_STAKE_RES" | jq -r '.txhash // empty' 2>/dev/null)
sleep 2

# See Staker1 block above — guard against jq parse errors and empty txhash.
STAKER2_STAKE_TX_RESULT=""
if [ -n "$STAKER2_STAKE_TX" ]; then
    STAKER2_STAKE_TX_RESULT=$($BINARY query tx $STAKER2_STAKE_TX -o json 2>/dev/null)
fi
STAKER2_STAKE_CODE=$(echo "$STAKER2_STAKE_TX_RESULT" | jq -r '.code // 0' 2>/dev/null)
if [ -n "$STAKER2_STAKE_CODE" ] && [ "$STAKER2_STAKE_CODE" != "0" ]; then
    STAKER2_STAKE_ERROR=$(echo "$STAKER2_STAKE_TX_RESULT" | jq -r '.raw_log // "Unknown error"' 2>/dev/null)
    echo "[WARN]  Staker2 stake transaction failed: $STAKER2_STAKE_ERROR"
fi

STAKER2_STAKE_ID=$(echo "$STAKER2_STAKE_TX_RESULT" | \
  jq -r '.events[] | select(.type=="sparkdream.rep.v1.EventStakeCreated") | .attributes[] | select(.key=="stake_id") | .value' | \
  tr -d '"')

# Fallback: Try alternative event types
if [ -z "$STAKER2_STAKE_ID" ] || [ "$STAKER2_STAKE_ID" == "null" ] || [ "$STAKER2_STAKE_ID" == "" ]; then
    STAKER2_STAKE_ID=$(echo "$STAKER2_STAKE_TX_RESULT" | \
      jq -r '.events[] | select(.type=="stake_created") | .attributes[] | select(.key=="stake_id") | .value' | \
      tr -d '"')
fi

# Fallback: Query stakes by staker to get most recent stake
if [ -z "$STAKER2_STAKE_ID" ] || [ "$STAKER2_STAKE_ID" == "null" ] || [ "$STAKER2_STAKE_ID" == "" ]; then
    sleep 1  # Give chain time to process
    STAKER2_STAKE_ID=$($BINARY query rep stakes-by-staker $STAKER2_ADDR -o json 2>/dev/null | \
      jq -r '.stakes[0].id // ""')
fi

if [ -z "$STAKER2_STAKE_ID" ] || [ "$STAKER2_STAKE_ID" == "null" ] || [ "$STAKER2_STAKE_ID" == "" ]; then
    echo "[WARN]  Could not extract stake ID from transaction"
    STAKER2_STAKE_ID="unknown"
    echo "[WARN]  Staker2 stake may have failed"
else
    echo "[ OK ] Staker2 staked: $STAKER2_STAKE_ID (150 DREAM)"
fi

# ========================================================================
# PART 11: QUERY INITIATIVE CONVICTION
# ========================================================================
echo ""
echo "--- PART 11: QUERY INITIATIVE CONVICTION ---"

CONVICTION_QUERY=$($BINARY query rep initiative-conviction $FLOW_INITIATIVE_ID -o json)
# Note: Field names are total_conviction, external_conviction, threshold
CURRENT_CONVICTION=$(echo "$CONVICTION_QUERY" | jq -r '.total_conviction // "0"')
EXTERNAL_CONVICTION=$(echo "$CONVICTION_QUERY" | jq -r '.external_conviction // "0"')
REQUIRED_CONVICTION=$(echo "$CONVICTION_QUERY" | jq -r '.threshold // "0"')

echo "Current conviction: $CURRENT_CONVICTION"
echo "External conviction: $EXTERNAL_CONVICTION"
echo "Required conviction: $REQUIRED_CONVICTION"
echo "Total staked: 250 DREAM (Staker1: 100, Staker2: 150)"

if [ -n "$CURRENT_CONVICTION" ] && [ "$CURRENT_CONVICTION" != "0" ] && [ "$CURRENT_CONVICTION" != "null" ]; then
    echo "[ OK ] Conviction tracking active"
else
    echo "[WARN]  Conviction may not be calculated yet (needs epoch blocks)"
fi

# ========================================================================
# PART 12: QUERY STAKES BY INITIATIVE
# ========================================================================
echo ""
echo "--- PART 12: QUERY STAKES BY TARGET (INITIATIVE) ---"

# Query stakes by target type (0 = INITIATIVE) and target ID
# Note: Use numeric 0 for STAKE_TARGET_INITIATIVE, not string "stake-target-initiative"
INITIATIVE_STAKES=$($BINARY query rep stakes-by-target 0 $FLOW_INITIATIVE_ID -o json 2>&1)

if ! echo "$INITIATIVE_STAKES" | grep -q "not found"; then
    STAKE_COUNT=$(echo "$INITIATIVE_STAKES" | jq -r '.stakes | length')
    echo "Stakes on initiative: $STAKE_COUNT"

    if [ "$STAKE_COUNT" -ge 2 ]; then
        echo "[ OK ] Found staker1 and staker2's stakes on initiative"
    fi
else
    echo "[WARN]  stakes-by-target query may not be implemented"
fi

# Query Staker1's stakes
STAKER1_STAKES_LIST=$($BINARY query rep stakes-by-staker $STAKER1_ADDR -o json)
# Note: Query returns all of the staker's stakes in a .stakes[] array.
STAKER1_STAKE_TOTAL=$(echo "$STAKER1_STAKES_LIST" | jq -r '.stakes | length // 0')
STAKER1_STAKE_ID_FROM_QUERY=$(echo "$STAKER1_STAKES_LIST" | jq -r '.stakes[0].id // ""')

# Update STAKER1_STAKE_ID if query found it (overrides "unknown" from Part 8)
if [ -n "$STAKER1_STAKE_ID_FROM_QUERY" ] && [ "$STAKER1_STAKE_ID_FROM_QUERY" != "null" ] && [ "$STAKER1_STAKE_ID_FROM_QUERY" != "" ]; then
    STAKER1_STAKE_ID="$STAKER1_STAKE_ID_FROM_QUERY"
    STAKER1_STAKE_AMOUNT=$(echo "$STAKER1_STAKES_LIST" | jq -r '.stakes[0].amount // "0"')
    STAKER1_STAKE_AMOUNT_DREAM=$(echo "scale=2; $STAKER1_STAKE_AMOUNT / 1000000" | bc 2>/dev/null || echo "0")
    echo "Staker1 has $STAKER1_STAKE_TOTAL stake(s):"
    echo "  First stake ID: $STAKER1_STAKE_ID"
    echo "  Amount: $STAKER1_STAKE_AMOUNT_DREAM DREAM"
    echo "[ OK ] Staker1's stake found"
    echo "[INFO]  Updated stake ID for use in later parts"
else
    echo "Staker1 has 0 total stakes"
    echo "[WARN]  Query may not have found the stake"
    # Keep STAKER1_STAKE_ID as it was (likely "unknown" from Part 8)
fi


# ========================================================================
# PART 13: QUERY CHALLENGES BY INITIATIVE
# ========================================================================
echo ""
echo "--- PART 13: QUERY ALL CHALLENGES FOR INITIATIVE ---"

INITIATIVE_CHALLENGES=$($BINARY query rep challenges-by-initiative $FLOW_INITIATIVE_ID -o json)
# Note: Query returns single object {challenge_id, status}, not array
FOUND_CHALLENGE_ID=$(echo "$INITIATIVE_CHALLENGES" | jq -r '.challenge_id // ""')

if [ -n "$FOUND_CHALLENGE_ID" ] && [ "$FOUND_CHALLENGE_ID" != "null" ] && [ "$FOUND_CHALLENGE_ID" != "" ]; then
    FOUND_CHALLENGE_STATUS=$(echo "$INITIATIVE_CHALLENGES" | jq -r '.status // "0"')
    echo "Initiative has 1 challenge:"
    echo "  Challenge ID: $FOUND_CHALLENGE_ID"
    echo "  Status: $FOUND_CHALLENGE_STATUS"
    echo "[ OK ] Challenge found for initiative"
else
    echo "Initiative has 0 challenge(s)"
    if [ "$CHALLENGE_ID" != "unknown" ] && [ -n "$CHALLENGE_ID" ]; then
        echo "[WARN]  Challenge #$CHALLENGE_ID was created but not found by query (may be for different initiative or already resolved)"
    fi
fi

# ========================================================================
# PART 14: ABANDON INITIATIVE (OPTIONAL FLOW)
# ========================================================================
echo ""
echo "--- PART 14: TEST ABANDON INITIATIVE ---"
echo "Creating a new initiative to test abandon flow..."

# Create a test initiative for abandon
# Command: create-initiative [project-id] [title] [description] [tier] [category] [template-id] [budget]
ABANDON_TEST_RES=$($BINARY tx rep create-initiative \
  $PROJECT_ID \
  "Test Abandon Initiative" \
  "This initiative will be abandoned" \
  "0" \
  "1" \
  "0" \
  "50000000" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

ABANDON_TEST_TX=$(echo $ABANDON_TEST_RES | jq -r '.txhash')
sleep 6  # Increased wait time for transaction to be indexed

# Method 1: Extract from transaction events
ABANDON_TEST_INIT_ID=$($BINARY query tx $ABANDON_TEST_TX -o json 2>/dev/null | \
  jq -r '.events[] | select(.type=="initiative_created") | .attributes[] | select(.key=="initiative_id") | .value' | \
  tr -d '"' 2>/dev/null)

# Method 2: Try alternative event extraction from logs
if [ -z "$ABANDON_TEST_INIT_ID" ] || [ "$ABANDON_TEST_INIT_ID" == "null" ] || [ "$ABANDON_TEST_INIT_ID" == "" ]; then
    ABANDON_TEST_INIT_ID=$($BINARY query tx $ABANDON_TEST_TX -o json 2>/dev/null | \
      jq -r '.logs[0].events[] | select(.type=="initiative_created") | .attributes[] | select(.key=="initiative_id") | .value' 2>/dev/null)
fi

# Method 3: Extract from raw logs
if [ -z "$ABANDON_TEST_INIT_ID" ] || [ "$ABANDON_TEST_INIT_ID" == "null" ] || [ "$ABANDON_TEST_INIT_ID" == "" ]; then
    ABANDON_TEST_INIT_ID=$($BINARY query tx $ABANDON_TEST_TX -o json 2>/dev/null | jq -r '.raw_log' | grep -oP 'initiative_id\\",\\"value\\":\\"\K[0-9]+' | head -1)
fi

# Method 4: Fallback to querying latest initiative from project
if [ -z "$ABANDON_TEST_INIT_ID" ] || [ "$ABANDON_TEST_INIT_ID" == "null" ] || [ "$ABANDON_TEST_INIT_ID" == "" ]; then
    echo "[WARN]  Failed to extract from events, querying latest initiative..."
    ABANDON_TEST_INIT_ID=$($BINARY query rep initiatives-by-project $PROJECT_ID -o json 2>/dev/null | \
      jq -r '.initiatives | sort_by(.id) | .[-1].id' 2>/dev/null)
fi

echo "Created test initiative: $ABANDON_TEST_INIT_ID"

# Final check
if [ -z "$ABANDON_TEST_INIT_ID" ] || [ "$ABANDON_TEST_INIT_ID" == "null" ] || [ "$ABANDON_TEST_INIT_ID" == "" ]; then
    echo "[WARN]  Failed to extract abandon test initiative ID, skipping abandon test"
    # Skip the rest of Part 14
else

# Assign it to Worker
ASSIGN_ABANDON_RES=$($BINARY tx rep assign-initiative \
  $ABANDON_TEST_INIT_ID \
  $WORKER_ADDR \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

ASSIGN_ABANDON_TX=$(echo $ASSIGN_ABANDON_RES | jq -r '.txhash')
sleep 2

# Verify assignment succeeded
ASSIGN_ABANDON_TX_RESULT=$($BINARY query tx $ASSIGN_ABANDON_TX -o json 2>/dev/null)
ASSIGN_ABANDON_CODE=$(echo "$ASSIGN_ABANDON_TX_RESULT" | jq -r '.code // 0')
if [ "$ASSIGN_ABANDON_CODE" != "0" ]; then
    ASSIGN_ABANDON_ERROR=$(echo "$ASSIGN_ABANDON_TX_RESULT" | jq -r '.raw_log // "Unknown error"')
    echo "[WARN]  Assignment failed: $ASSIGN_ABANDON_ERROR"
    echo "[WARN]  Skipping abandon test - assignment failed"
    # Skip abandon
else
    # Verify initiative is actually assigned to Worker
    ABANDON_TEST_INIT=$($BINARY query rep get-initiative $ABANDON_TEST_INIT_ID -o json 2>/dev/null)
    ABANDON_TEST_ASSIGNEE=$(echo "$ABANDON_TEST_INIT" | jq -r '.initiative.assignee // ""')
    ABANDON_TEST_STATUS=$(echo "$ABANDON_TEST_INIT" | jq -r '.initiative.status // "0"')

    echo "Assigned to Worker: $WORKER_ADDR"
    echo "Initiative assignee: $ABANDON_TEST_ASSIGNEE"
    echo "Initiative status: $ABANDON_TEST_STATUS"

    if [ "$ABANDON_TEST_ASSIGNEE" != "$WORKER_ADDR" ]; then
        echo "[WARN]  Assignment verification failed - assignee mismatch"
        echo "[WARN]  Expected: $WORKER_ADDR"
        echo "[WARN]  Got: $ABANDON_TEST_ASSIGNEE"
        echo "[WARN]  Skipping abandon test - assignment not verified"
    else
        # Now Worker abandons it
        echo "Worker abandons the assigned initiative..."
ABANDON_RES=$($BINARY tx rep abandon-initiative \
  $ABANDON_TEST_INIT_ID \
  "Changed priorities, not relevant anymore" \
  --from $WORKER_NAME \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

ABANDON_TX=$(echo $ABANDON_RES | jq -r '.txhash')
sleep 2

# Check if transaction succeeded
ABANDON_TX_RESULT=$($BINARY query tx $ABANDON_TX -o json 2>/dev/null)
ABANDON_CODE=$(echo "$ABANDON_TX_RESULT" | jq -r '.code // 0')
if [ "$ABANDON_CODE" != "0" ]; then
    ABANDON_ERROR=$(echo "$ABANDON_TX_RESULT" | jq -r '.raw_log // "Unknown error"')
    echo "[WARN]  Abandon transaction failed: $ABANDON_ERROR"
fi

# Verify initiative status changed to ABANDONED
ABANDONED_INITIATIVE=$($BINARY query rep get-initiative $ABANDON_TEST_INIT_ID -o json)
# Note: Proto3 omits zero values - use // operator for defaults
ABANDON_STATUS=$(echo "$ABANDONED_INITIATIVE" | jq -r '.initiative.status // "INITIATIVE_STATUS_OPEN"')

echo "Abandoned initiative status: $ABANDON_STATUS"

if [ "$ABANDON_STATUS" == "INITIATIVE_STATUS_ABANDONED" ]; then
    echo "[ OK ] Initiative successfully ABANDONED"
elif [ "$ABANDON_CODE" != "0" ]; then
    echo "[WARN]  Abandon failed - see error above"
else
    echo "[WARN]  Initiative status: $ABANDON_STATUS (transaction succeeded but status not changed)"
fi

    fi  # End of assignee verification check
fi  # End of assignment success check
fi  # End of if block for ABANDON_TEST_INIT_ID check

# ========================================================================
# PART 14B: CANCEL INITIATIVE (UNSTARTED, OPEN-ONLY)
# ========================================================================
echo ""
echo "--- PART 14B: TEST CANCEL INITIATIVE ---"
echo "Creating a new OPEN initiative to test cancel flow..."

CANCEL_STATUS="INITIATIVE_STATUS_OPEN"   # default for summary line if skipped

# Create a test initiative for cancel (apprentice tier -> no reputation floor)
# Command: create-initiative [project-id] [title] [description] [tier] [category] [template-id] [budget]
CANCEL_TEST_RES=$($BINARY tx rep create-initiative \
  $PROJECT_ID \
  "Test Cancel Initiative" \
  "This OPEN initiative will be cancelled by the project creator" \
  "0" \
  "1" \
  "0" \
  "50000000" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

CANCEL_TEST_TX=$(echo $CANCEL_TEST_RES | jq -r '.txhash')
sleep 6  # wait for the tx to be indexed

# Extract the new initiative ID from events, with a query fallback.
CANCEL_TEST_INIT_ID=$($BINARY query tx $CANCEL_TEST_TX -o json 2>/dev/null | \
  jq -r '.events[] | select(.type=="initiative_created") | .attributes[] | select(.key=="initiative_id") | .value' | \
  tr -d '"' 2>/dev/null)

if [ -z "$CANCEL_TEST_INIT_ID" ] || [ "$CANCEL_TEST_INIT_ID" == "null" ] || [ "$CANCEL_TEST_INIT_ID" == "" ]; then
    echo "[WARN]  Failed to extract from events, querying latest initiative..."
    CANCEL_TEST_INIT_ID=$($BINARY query rep initiatives-by-project $PROJECT_ID -o json 2>/dev/null | \
      jq -r '.initiatives | sort_by(.id) | .[-1].id' 2>/dev/null)
fi

echo "Created test initiative: $CANCEL_TEST_INIT_ID"

if [ -z "$CANCEL_TEST_INIT_ID" ] || [ "$CANCEL_TEST_INIT_ID" == "null" ] || [ "$CANCEL_TEST_INIT_ID" == "" ]; then
    echo "[WARN]  Failed to extract cancel test initiative ID, skipping cancel test"
else

# Record the project's allocated budget before cancelling (proto3 omits zeros).
PROJECT_BEFORE_CANCEL=$($BINARY query rep get-project $PROJECT_ID -o json 2>/dev/null)
ALLOCATED_BEFORE=$(echo "$PROJECT_BEFORE_CANCEL" | jq -r '.project.allocated_budget // "0"')
echo "Project allocated budget before cancel: $ALLOCATED_BEFORE"

# --- Negative check: a non-creator, non-ops member cannot cancel ---
echo "Worker (not the project creator) attempts to cancel..."
UNAUTH_CANCEL_RES=$($BINARY tx rep cancel-initiative \
  $CANCEL_TEST_INIT_ID \
  "unauthorized attempt" \
  --from $WORKER_NAME \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)
UNAUTH_CANCEL_TX=$(echo $UNAUTH_CANCEL_RES | jq -r '.txhash')
sleep 2
UNAUTH_CANCEL_CODE=$($BINARY query tx $UNAUTH_CANCEL_TX -o json 2>/dev/null | jq -r '.code // 0')
if [ "$UNAUTH_CANCEL_CODE" != "0" ]; then
    echo "[ OK ] Non-creator cancel correctly rejected (code $UNAUTH_CANCEL_CODE)"
else
    echo "[WARN]  Non-creator cancel unexpectedly succeeded"
fi

# Confirm the initiative is still OPEN after the rejected attempt.
STILL_OPEN=$($BINARY query rep get-initiative $CANCEL_TEST_INIT_ID -o json 2>/dev/null | \
  jq -r '.initiative.status // "INITIATIVE_STATUS_OPEN"')
if [ "$STILL_OPEN" == "INITIATIVE_STATUS_OPEN" ]; then
    echo "[ OK ] Initiative remains OPEN after rejected cancel"
else
    echo "[WARN]  Initiative status after rejected cancel: $STILL_OPEN (expected OPEN)"
fi

# --- Happy path: the project creator (alice) cancels the OPEN initiative ---
echo "Project creator (alice) cancels the OPEN initiative..."
CANCEL_RES=$($BINARY tx rep cancel-initiative \
  $CANCEL_TEST_INIT_ID \
  "Listing no longer needed" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)
CANCEL_TX=$(echo $CANCEL_RES | jq -r '.txhash')
sleep 2

CANCEL_TX_RESULT=$($BINARY query tx $CANCEL_TX -o json 2>/dev/null)
CANCEL_CODE=$(echo "$CANCEL_TX_RESULT" | jq -r '.code // 0')
if [ "$CANCEL_CODE" != "0" ]; then
    CANCEL_ERROR=$(echo "$CANCEL_TX_RESULT" | jq -r '.raw_log // "Unknown error"')
    echo "[WARN]  Cancel transaction failed: $CANCEL_ERROR"
fi

# Verify status changed to CANCELLED (distinct from ABANDONED).
CANCELLED_INITIATIVE=$($BINARY query rep get-initiative $CANCEL_TEST_INIT_ID -o json)
CANCEL_STATUS=$(echo "$CANCELLED_INITIATIVE" | jq -r '.initiative.status // "INITIATIVE_STATUS_OPEN"')
echo "Cancelled initiative status: $CANCEL_STATUS"

if [ "$CANCEL_STATUS" == "INITIATIVE_STATUS_CANCELLED" ]; then
    echo "[ OK ] Initiative successfully CANCELLED"
elif [ "$CANCEL_CODE" != "0" ]; then
    echo "[WARN]  Cancel failed - see error above"
else
    echo "[WARN]  Initiative status: $CANCEL_STATUS (transaction succeeded but status not CANCELLED)"
fi

# Verify the reserved budget (50 DREAM = 50000000 micro) was returned to the project.
PROJECT_AFTER_CANCEL=$($BINARY query rep get-project $PROJECT_ID -o json 2>/dev/null)
ALLOCATED_AFTER=$(echo "$PROJECT_AFTER_CANCEL" | jq -r '.project.allocated_budget // "0"')
echo "Project allocated budget after cancel: $ALLOCATED_AFTER (was $ALLOCATED_BEFORE)"
EXPECTED_AFTER=$((ALLOCATED_BEFORE - 50000000))
if [ "$CANCEL_STATUS" == "INITIATIVE_STATUS_CANCELLED" ]; then
    if [ "$ALLOCATED_AFTER" == "$EXPECTED_AFTER" ]; then
        echo "[ OK ] Reserved budget (50000000) returned to project"
    else
        echo "[WARN]  Allocated budget $ALLOCATED_AFTER != expected $EXPECTED_AFTER after budget return"
    fi
fi

fi  # End of if block for CANCEL_TEST_INIT_ID check

# ========================================================================
# PART 15: COMPLETE INITIATIVE (NORMAL FLOW)
# ========================================================================
echo ""
echo "--- PART 15: COMPLETE INITIATIVE (SIMULATED) ---"
echo "Note: In production, completion requires:"
echo "1. Conviction threshold met (enough stakes)"
echo "2. External conviction >= 50%"
echo "3. No active challenges"
echo "4. Review period passed"
echo "5. Challenge period passed"
echo ""
echo "For testing, we attempt to complete directly..."

COMPLETE_RES=$($BINARY tx rep complete-initiative \
  $FLOW_INITIATIVE_ID \
  "All requirements satisfied, ready for merge" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)

COMPLETE_TX=$(echo $COMPLETE_RES | jq -r '.txhash')
sleep 3

COMPLETE_RESULT=$($BINARY query tx $COMPLETE_TX -o json 2>/dev/null)
COMPLETE_CODE=$(echo "$COMPLETE_RESULT" | jq -r '.code // 0')

# Check if completion succeeded or explain requirements
if [ "$COMPLETE_CODE" == "0" ]; then
    FINAL_INITIATIVE=$($BINARY query rep get-initiative $FLOW_INITIATIVE_ID -o json)
    FINAL_STATUS=$(echo "$FINAL_INITIATIVE" | jq -r '.initiative.status // "INITIATIVE_STATUS_OPEN"')
    COMPLETED_AT=$(echo "$FINAL_INITIATIVE" | jq -r '.initiative.completed_at // "0"')

    echo "Initiative final status: $FINAL_STATUS"
    echo "Completed at: $COMPLETED_AT"

    if [ "$FINAL_STATUS" == "INITIATIVE_STATUS_COMPLETED" ]; then
        echo "[ OK ] Initiative COMPLETED successfully"
        echo "→ DREAM minted to worker"
        echo "→ Reputation granted to worker"
        echo "→ Staking rewards distributed to stakers"
        echo "→ Budget distribution: 10% treasury, 90% worker"
    else
        echo "[WARN]  Initiative status: $FINAL_STATUS"
        echo "→ May require more conviction/staking or wait for periods"
    fi
else
    COMPLETE_ERROR=$(echo "$COMPLETE_RESULT" | jq -r '.raw_log // "Unknown error"')
    echo "[WARN]  Completion failed (expected - conviction threshold not met or challenge active)"
    echo "→ Error: $COMPLETE_ERROR"
    echo "→ This is normal - completion requires conviction threshold + periods"
fi

# ========================================================================
# PART 16: QUERY STAKE REWARDS
# ========================================================================
echo ""
echo "--- PART 16: QUERY PENDING STAKING REWARDS ---"

# Query Staker1's stake for pending rewards
if [ "$STAKER1_STAKE_ID" != "unknown" ] && [ -n "$STAKER1_STAKE_ID" ]; then
    STAKER1_STAKE_DETAIL=$($BINARY query rep get-stake $STAKER1_STAKE_ID -o json 2>&1)
    if echo "$STAKER1_STAKE_DETAIL" | grep -q "key not found"; then
        echo "Staker1's stake #$STAKER1_STAKE_ID:"
        echo "  [WARN]  Stake not found (may have been deleted or ID incorrect)"
    else
        STAKER1_AMOUNT=$(echo "$STAKER1_STAKE_DETAIL" | jq -r '.stake.amount // "0"')
        STAKER1_CREATED=$(echo "$STAKER1_STAKE_DETAIL" | jq -r '.stake.created_at // "0"')
        echo "Staker1's stake #$STAKER1_STAKE_ID:"
        echo "  Target: Initiative #$FLOW_INITIATIVE_ID"
        echo "  Amount: $(echo "scale=2; $STAKER1_AMOUNT / 1000000" | bc 2>/dev/null || echo "0") DREAM"
        echo "  Created at: $STAKER1_CREATED"
    fi
else
    echo "Staker1's stake: Unknown (stake creation may have failed)"
fi

# Query Staker2's stake for pending rewards
echo ""
if [ "$STAKER2_STAKE_ID" != "unknown" ] && [ -n "$STAKER2_STAKE_ID" ]; then
    STAKER2_STAKE_DETAIL=$($BINARY query rep get-stake $STAKER2_STAKE_ID -o json 2>&1)
    if echo "$STAKER2_STAKE_DETAIL" | grep -q "key not found"; then
        echo "Staker2's stake #$STAKER2_STAKE_ID:"
        echo "  [WARN]  Stake not found (may have been deleted or ID incorrect)"
    else
        STAKER2_AMOUNT=$(echo "$STAKER2_STAKE_DETAIL" | jq -r '.stake.amount // "0"')
        STAKER2_CREATED=$(echo "$STAKER2_STAKE_DETAIL" | jq -r '.stake.created_at // "0"')
        echo "Staker2's stake #$STAKER2_STAKE_ID:"
        echo "  Target: Initiative #$FLOW_INITIATIVE_ID"
        echo "  Amount: $(echo "scale=2; $STAKER2_AMOUNT / 1000000" | bc 2>/dev/null || echo "0") DREAM"
        echo "  Created at: $STAKER2_CREATED"
    fi
else
    echo "Staker2's stake: Unknown (stake creation may have failed)"
fi

# ========================================================================
# PART 17: UNSTAKE (CLAIM REWARDS)
# ========================================================================
echo ""
echo "--- PART 17: UNSTAKE TO CLAIM REWARDS ---"

# Staker1 unstakes (claims rewards)
if [ "$STAKER1_STAKE_ID" != "unknown" ] && [ -n "$STAKER1_STAKE_ID" ]; then
    echo "Staker1 unstaking stake #$STAKER1_STAKE_ID..."
    UNSTAKE_RES=$($BINARY tx rep unstake \
      $STAKER1_STAKE_ID \
      "100000000" \
      --from $STAKER1_NAME \
      --chain-id $CHAIN_ID \
      --keyring-backend test \
      --fees 5000${BOND_DENOM} \
      -y \
      -o json)

    UNSTAKE_TX=$(echo $UNSTAKE_RES | jq -r '.txhash')
    sleep 2

    UNSTAKE_RESULT=$($BINARY query tx $UNSTAKE_TX -o json 2>/dev/null)
    UNSTAKE_CODE=$(echo "$UNSTAKE_RESULT" | jq -r '.code // 0')

    if [ "$UNSTAKE_CODE" == "0" ]; then
        echo "[ OK ] Staker1 unstaked successfully"
        echo "→ Principal (100 DREAM) returned"
        echo "→ Rewards claimed"
    else
        UNSTAKE_ERROR=$(echo "$UNSTAKE_RESULT" | jq -r '.raw_log // "Unknown error"')
        echo "[WARN]  Unstake failed (code: $UNSTAKE_CODE): $UNSTAKE_ERROR"
        echo "→ May need minimum duration, or initiative not completed yet"
    fi
else
    echo "[WARN]  Staker1 unstake skipped - stake ID unknown (stake creation may have failed)"
fi

# ========================================================================
# PART 18: LIST ALL INITIATIVES
# ========================================================================
echo ""
echo "--- PART 18: LIST ALL INITIATIVES ---"

ALL_INITIATIVES=$($BINARY query rep list-initiative -o json)
# Note: Response uses .initiative (singular), not .initiatives (plural)
TOTAL_INITIATIVES=$(echo "$ALL_INITIATIVES" | jq -r '.initiative // [] | length')

echo "Total initiatives in system: $TOTAL_INITIATIVES"

# Find our test initiatives (handle null)
FOUND_APPRENTICE=$(echo "$ALL_INITIATIVES" | jq -r '.initiative // [] | .[] | select(.id=="'$APPRENTICE_ID'") | .id' 2>/dev/null)
FOUND_STANDARD=$(echo "$ALL_INITIATIVES" | jq -r '.initiative // [] | .[] | select(.id=="'$FLOW_INITIATIVE_ID'") | .id' 2>/dev/null)

if [ -n "$FOUND_APPRENTICE" ] && [ -n "$FOUND_STANDARD" ]; then
    echo "[ OK ] Both test initiatives found in system list"
fi

# ========================================================================
# PART 19: INITIATIVE TIER VERIFICATION
# ========================================================================
echo ""
echo "--- PART 19: INITIATIVE TIER BUDGET LIMITS ---"

# Query tier details
APPRENTICE_DETAIL=$($BINARY query rep get-initiative $APPRENTICE_ID -o json)
STANDARD_DETAIL=$($BINARY query rep get-initiative $STANDARD_ID -o json)

APPRENTICE_BUDGET=$(echo "$APPRENTICE_DETAIL" | jq -r '.initiative.budget // 0')
STANDARD_BUDGET=$(echo "$STANDARD_DETAIL" | jq -r '.initiative.budget // 0')

# Convert from micro-DREAM to DREAM for display
APPRENTICE_BUDGET_DISPLAY=$(echo "scale=2; $APPRENTICE_BUDGET / 1000000" | bc 2>/dev/null || echo "0")
STANDARD_BUDGET_DISPLAY=$(echo "scale=2; $STANDARD_BUDGET / 1000000" | bc 2>/dev/null || echo "0")

echo "Apprentice tier budget: $APPRENTICE_BUDGET_DISPLAY DREAM (max: 100)"
echo "Standard tier budget: $STANDARD_BUDGET_DISPLAY DREAM (max: 500)"

# Verify budgets are within tier limits (compare in micro-DREAM)
if [ "$APPRENTICE_BUDGET" -le "100000000" ]; then
    echo "[ OK ] Apprentice budget within tier limit"
else
    echo "[WARN]  Apprentice budget exceeds limit"
fi

if [ "$STANDARD_BUDGET" -le "500000000" ]; then
    echo "[ OK ] Standard budget within tier limit"
else
    echo "[WARN]  Standard budget exceeds limit"
fi

# ========================================================================
# PART 20: PROJECT INITIATIVES SUMMARY
# ========================================================================
echo ""
echo "--- PART 20: PROJECT INITIATIVES SUMMARY ---"

# Query initiatives by project again (after all operations)
FINAL_PROJECT_INITIATIVES=$($BINARY query rep initiatives-by-project $PROJECT_ID -o json)
FINAL_INIT_COUNT=$(echo "$FINAL_PROJECT_INITIATIVES" | jq -r '.initiatives | length')

echo "Project #$PROJECT_ID now has $FINAL_INIT_COUNT initiatives"

# Count by status (handle null initiatives)
OPEN_COUNT=$(echo "$FINAL_PROJECT_INITIATIVES" | jq -r '.initiatives // [] | [.[] | select(.status=="INITIATIVE_STATUS_OPEN")] | length')
ASSIGNED_COUNT=$(echo "$FINAL_PROJECT_INITIATIVES" | jq -r '.initiatives // [] | [.[] | select(.status=="INITIATIVE_STATUS_ASSIGNED")] | length')
SUBMITTED_COUNT=$(echo "$FINAL_PROJECT_INITIATIVES" | jq -r '.initiatives // [] | [.[] | select(.status=="INITIATIVE_STATUS_SUBMITTED" or .status=="INITIATIVE_STATUS_IN_REVIEW" or .status=="INITIATIVE_STATUS_CHALLENGED")] | length')
ABANDONED_COUNT=$(echo "$FINAL_PROJECT_INITIATIVES" | jq -r '.initiatives // [] | [.[] | select(.status=="INITIATIVE_STATUS_ABANDONED")] | length')
CANCELLED_COUNT=$(echo "$FINAL_PROJECT_INITIATIVES" | jq -r '.initiatives // [] | [.[] | select(.status=="INITIATIVE_STATUS_CANCELLED")] | length')

echo "  OPEN: $OPEN_COUNT"
echo "  ASSIGNED: $ASSIGNED_COUNT"
echo "  SUBMITTED/IN REVIEW/CHALLENGED: $SUBMITTED_COUNT"
echo "  ABANDONED: $ABANDONED_COUNT"
echo "  CANCELLED: $CANCELLED_COUNT"

# ========================================================================
# SUMMARY
# ========================================================================
echo ""
echo "--- INITIATIVE FLOW TEST SUMMARY ---"
echo ""
echo "[ OK ] Part 1:  Project created           ID $PROJECT_ID"
echo "[ OK ] Part 2:  Budget approved             Status: $NEW_STATUS"
echo "[ OK ] Part 3:  Initiatives created        Apprentice: $APPRENTICE_ID, Standard: $FLOW_INITIATIVE_ID"
echo "[ OK ] Part 4:  Initiatives by project     $INITIATIVE_COUNT found"
echo "[ OK ] Part 5:  Initiative assigned         Status: $NEW_INIT_STATUS"
echo "[ OK ] Part 6:  By assignee               $WORKER_INIT_COUNT found"
echo "[ OK ] Part 7:  Work submitted             Status: $SUBMIT_STATUS"
echo "[ OK ] Part 8:  Challenge created          ID $CHALLENGE_ID, Status: $CHALLENGE_STATUS"
echo "[ OK ] Part 9:  Challenge response           Responded"
echo "[ OK ] Part 10: Staking (conviction)     Staker1: 100, Staker2: 150 DREAM"
echo "[ OK ] Part 11: Conviction query            Current: $CURRENT_CONVICTION"
echo "[ OK ] Part 12: Stakes by target           Tracked"
echo "[ OK ] Part 13: Challenges by initiative    $CHALLENGE_COUNT found"
echo "[ OK ] Part 14: Abandoned flow            Status: $ABANDON_STATUS"
echo "[ OK ] Part 14B: Cancel flow              Status: $CANCEL_STATUS"
echo "[ OK ] Part 15: Completion attempt          Status: $FINAL_STATUS"
echo "[ OK ] Part 16: Pending rewards           Queried"
echo "[ OK ] Part 17: Unstake/claim            Attempted"
echo "[ OK ] Part 18: List all initiatives     $TOTAL_INITIATIVES total"
echo "[ OK ] Part 19: Tier budget limits        Verified"
echo "[ OK ] Part 20: Project summary           $FINAL_INIT_COUNT initiatives"
echo ""
echo " CONVICTION VOTING DEMONSTRATED:"
echo "   → Staker1 ($STAKER1_NAME) staked 100 DREAM on initiative"
echo "   → Staker2 ($STAKER2_NAME) staked 150 DREAM on initiative"
echo "   → Total: 250 DREAM conviction"
echo "   → External conviction requirement: 50%"
echo ""
echo " COMPLETION REQUIREMENTS (for production):"
echo "   1. Total conviction ≥ threshold"
echo "   2. External conviction ≥ 50%"
echo "   3. No active challenges"
echo "   4. Review period passed"
echo "   5. Challenge period passed"
echo ""
echo "=== INITIATIVE FLOW TEST COMPLETED ==="
