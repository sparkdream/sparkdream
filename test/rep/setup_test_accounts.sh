#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/rep Tests"
echo "=================================================="
echo ""
echo "NOTE: Full setup takes ~10 minutes (builds reputation via EPIC"
echo "      interims for sentinels/poster/moderator, conviction windows)."
echo ""

# ========================================================================
# Configuration
# ========================================================================
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Get alice address (genesis member)
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)

echo "Genesis member (Alice): $ALICE_ADDR"
echo ""

# Delete stale .test_env so it is regenerated from the current keyring
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Removing stale .test_env (will be regenerated at end of setup)..."
    rm -f "$SCRIPT_DIR/.test_env"
fi

# ========================================================================
# Helper Functions
# ========================================================================

# Wait for transaction and extract specific event attribute
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
        echo "❌ Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
        return 1
    fi
    return 0
}

# ========================================================================
# 1. Create Test Account Keys (if not exist)
# ========================================================================
echo "Step 1: Creating test account keys..."

ACCOUNTS=("challenger" "anonymous_challenger" "assignee" "juror1" "juror2" "juror3" "expert" "sentinel1" "sentinel2" "poster1" "poster2" "moderator" "bounty_creator")

for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
        echo "  ✅ Created key: $ACCOUNT"
    else
        echo "  ⏭️  Key exists: $ACCOUNT"
    fi
done

# Get addresses
CHALLENGER_ADDR=$($BINARY keys show challenger -a --keyring-backend test)
ANON_CHALLENGER_ADDR=$($BINARY keys show anonymous_challenger -a --keyring-backend test)
ASSIGNEE_ADDR=$($BINARY keys show assignee -a --keyring-backend test)
JUROR1_ADDR=$($BINARY keys show juror1 -a --keyring-backend test)
JUROR2_ADDR=$($BINARY keys show juror2 -a --keyring-backend test)
JUROR3_ADDR=$($BINARY keys show juror3 -a --keyring-backend test)
EXPERT_ADDR=$($BINARY keys show expert -a --keyring-backend test)
SENTINEL1_ADDR=$($BINARY keys show sentinel1 -a --keyring-backend test)
SENTINEL2_ADDR=$($BINARY keys show sentinel2 -a --keyring-backend test)
POSTER1_ADDR=$($BINARY keys show poster1 -a --keyring-backend test)
POSTER2_ADDR=$($BINARY keys show poster2 -a --keyring-backend test)
MODERATOR_ADDR=$($BINARY keys show moderator -a --keyring-backend test)
BOUNTY_CREATOR_ADDR=$($BINARY keys show bounty_creator -a --keyring-backend test)

echo ""

# ========================================================================
# 2. Fund Test Accounts with SPARK (for gas fees)
# ========================================================================
echo "Step 2: Funding test accounts with SPARK for gas fees..."

for ADDR in $CHALLENGER_ADDR $ANON_CHALLENGER_ADDR $ASSIGNEE_ADDR $JUROR1_ADDR $JUROR2_ADDR $JUROR3_ADDR $EXPERT_ADDR $SENTINEL1_ADDR $SENTINEL2_ADDR $POSTER1_ADDR $POSTER2_ADDR $MODERATOR_ADDR $BOUNTY_CREATOR_ADDR; do
    echo "  → Sending 10 SPARK to $ADDR..."
    TX_RES=$($BINARY tx bank send \
        alice $ADDR \
        10000000uspark \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  ❌ Failed to send SPARK: no txhash"
        continue
    fi

    sleep 6
done

echo "  ✅ All accounts funded with SPARK"
echo ""

# ========================================================================
# 3. Invite Test Accounts to x/rep
# ========================================================================
echo "Step 3: Inviting test accounts to become members..."

INVITATION_IDS=()

for i in "${!ACCOUNTS[@]}"; do
    ACCOUNT="${ACCOUNTS[$i]}"

    # Get address based on account name
    case "$ACCOUNT" in
        "challenger") ADDR=$CHALLENGER_ADDR ;;
        "anonymous_challenger") ADDR=$ANON_CHALLENGER_ADDR ;;
        "assignee") ADDR=$ASSIGNEE_ADDR ;;
        "juror1") ADDR=$JUROR1_ADDR ;;
        "juror2") ADDR=$JUROR2_ADDR ;;
        "juror3") ADDR=$JUROR3_ADDR ;;
        "expert") ADDR=$EXPERT_ADDR ;;
        "sentinel1") ADDR=$SENTINEL1_ADDR ;;
        "sentinel2") ADDR=$SENTINEL2_ADDR ;;
        "poster1") ADDR=$POSTER1_ADDR ;;
        "poster2") ADDR=$POSTER2_ADDR ;;
        "moderator") ADDR=$MODERATOR_ADDR ;;
        "bounty_creator") ADDR=$BOUNTY_CREATOR_ADDR ;;
        *) echo "Unknown account: $ACCOUNT"; continue ;;
    esac

    # Skip inviting if already a member (idempotent rerun)
    EXISTING_MEMBER=$($BINARY query rep get-member $ADDR --output json 2>&1)
    if ! echo "$EXISTING_MEMBER" | grep -q "not found"; then
        echo "  ⏭️  $ACCOUNT is already a member, skipping invitation"
        INVITATION_IDS+=("")
        continue
    fi

    echo "  → Inviting $ACCOUNT ($ADDR)..."

    # Stake 100 DREAM (100000000 micro-DREAM) on the invitation
    TX_RES=$($BINARY tx rep invite-member \
        $ADDR \
        "100000000" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  ❌ Failed to invite $ACCOUNT: no txhash"
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        INVITATION_ID=$(extract_event_value "$TX_RESULT" "create_invitation" "invitation_id")
        if [ -z "$INVITATION_ID" ]; then
            echo "  ⚠️  Could not extract invitation_id, using index: $((i + 1))"
            INVITATION_ID=$((i + 1))
        fi
        INVITATION_IDS+=($INVITATION_ID)
        echo "  ✅ Invited $ACCOUNT (invitation #$INVITATION_ID)"
    else
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "invitation already exists"; then
            echo "  ⏭️  $ACCOUNT already has an invitation"
            INVITATION_IDS+=("")
        else
            echo "  ❌ Failed to invite $ACCOUNT: $RAW_LOG"
            INVITATION_IDS+=("")
        fi
    fi
done

echo ""

# ========================================================================
# 4. Accept Invitations
# ========================================================================
echo "Step 4: Accepting invitations..."

for i in "${!ACCOUNTS[@]}"; do
    ACCOUNT="${ACCOUNTS[$i]}"
    INVITATION_ID="${INVITATION_IDS[$i]}"

    if [ -z "$INVITATION_ID" ]; then
        echo "  ⏭️  Skipping $ACCOUNT (no invitation ID)"
        continue
    fi

    echo "  → $ACCOUNT accepting invitation #$INVITATION_ID..."

    TX_RES=$($BINARY tx rep accept-invitation \
        $INVITATION_ID \
        --from $ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  ❌ Failed to accept invitation: no txhash"
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  ✅ $ACCOUNT is now a member"
    else
        echo "  ❌ Failed: $ACCOUNT could not accept invitation"
    fi
done

echo ""

# ========================================================================
# 5. Transfer DREAM to Test Accounts
# ========================================================================
echo "Step 5: Transferring DREAM to test accounts..."
echo "  NOTE: DREAM transfer rate limiting enforced:"
echo "    - Max 500 DREAM per gift (500000000 micro-DREAM)"
echo "    - Cooldown: 5 blocks (~30 sec test) / 1 day (production) per recipient"
echo "    - Epoch limit: 2000 DREAM total per epoch across all recipients"
echo "  → Sending 250 DREAM to each account (single gift per account)"
echo "  → This provides sufficient DREAM for tests while preserving Alice's balance"

for ACCOUNT in "${ACCOUNTS[@]}"; do
    # Get address based on account name
    case "$ACCOUNT" in
        "challenger") ADDR=$CHALLENGER_ADDR ;;
        "anonymous_challenger") ADDR=$ANON_CHALLENGER_ADDR ;;
        "assignee") ADDR=$ASSIGNEE_ADDR ;;
        "juror1") ADDR=$JUROR1_ADDR ;;
        "juror2") ADDR=$JUROR2_ADDR ;;
        "juror3") ADDR=$JUROR3_ADDR ;;
        "expert") ADDR=$EXPERT_ADDR ;;
        "sentinel1") ADDR=$SENTINEL1_ADDR ;;
        "sentinel2") ADDR=$SENTINEL2_ADDR ;;
        "poster1") ADDR=$POSTER1_ADDR ;;
        "poster2") ADDR=$POSTER2_ADDR ;;
        "moderator") ADDR=$MODERATOR_ADDR ;;
        "bounty_creator") ADDR=$BOUNTY_CREATOR_ADDR ;;
        *) echo "Unknown account: $ACCOUNT"; continue ;;
    esac

    # Assignee needs more DREAM for staking tests (used heavily across test suite)
    # sentinel1 needs 500 to cover 100 DREAM bond + 3% tax + working capital
    if [ "$ACCOUNT" == "assignee" ]; then
        DREAM_AMOUNT="500000000"  # 500 DREAM
        echo "  → Sending 500 DREAM to $ACCOUNT (extra for staking tests)..."
    elif [ "$ACCOUNT" == "sentinel1" ]; then
        DREAM_AMOUNT="500000000"  # 500 DREAM
        echo "  → Sending 500 DREAM to $ACCOUNT (for sentinel bonding + working capital)..."
    elif [ "$ACCOUNT" == "sentinel2" ] || [ "$ACCOUNT" == "poster1" ] || [ "$ACCOUNT" == "moderator" ]; then
        DREAM_AMOUNT="250000000"  # 250 DREAM
        echo "  → Sending 250 DREAM to $ACCOUNT..."
    else
        DREAM_AMOUNT="250000000"  # 250 DREAM
        echo "  → Sending 250 DREAM to $ACCOUNT..."
    fi

    # Gift DREAM to the new member
    TX_RES=$($BINARY tx rep transfer-dream \
        $ADDR \
        "$DREAM_AMOUNT" \
        "gift" \
        "Test setup funding" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  ❌ Failed to send DREAM to $ACCOUNT: no txhash"
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        DREAM_DISPLAY=$((DREAM_AMOUNT / 1000000))
        echo "  ✅ Transferred $DREAM_DISPLAY DREAM to $ACCOUNT"
    else
        echo "  ❌ Failed to transfer DREAM to $ACCOUNT"
        echo "     $(echo "$TX_RESULT" | jq -r '.raw_log')"
    fi
done

echo ""

# ========================================================================
# 6. Verify All Members
# ========================================================================
echo "Step 6: Verifying all test accounts are members..."

ALL_SUCCESS=true

for ACCOUNT in "${ACCOUNTS[@]}"; do
    # Get address based on account name
    case "$ACCOUNT" in
        "challenger") ADDR=$CHALLENGER_ADDR ;;
        "anonymous_challenger") ADDR=$ANON_CHALLENGER_ADDR ;;
        "assignee") ADDR=$ASSIGNEE_ADDR ;;
        "juror1") ADDR=$JUROR1_ADDR ;;
        "juror2") ADDR=$JUROR2_ADDR ;;
        "juror3") ADDR=$JUROR3_ADDR ;;
        "expert") ADDR=$EXPERT_ADDR ;;
        "sentinel1") ADDR=$SENTINEL1_ADDR ;;
        "sentinel2") ADDR=$SENTINEL2_ADDR ;;
        "poster1") ADDR=$POSTER1_ADDR ;;
        "poster2") ADDR=$POSTER2_ADDR ;;
        "moderator") ADDR=$MODERATOR_ADDR ;;
        "bounty_creator") ADDR=$BOUNTY_CREATOR_ADDR ;;
        *) echo "Unknown account: $ACCOUNT"; continue ;;
    esac

    MEMBER_INFO=$($BINARY query rep get-member $ADDR --output json 2>&1)

    if echo "$MEMBER_INFO" | grep -q "not found"; then
        echo "  ❌ $ACCOUNT is NOT a member"
        ALL_SUCCESS=false
    else
        DREAM_BALANCE=$(echo "$MEMBER_INFO" | jq -r '.member.dream_balance')
        echo "  ✅ $ACCOUNT: $DREAM_BALANCE DREAM"
    fi
done

echo ""

# ========================================================================
# 7. Create and Approve Test Project
# ========================================================================
echo "Step 7: Creating test project for challenge tests..."

# Request 100,000 DREAM (100000000000 micro-DREAM) + 5 SPARK (5000000 uspark)
TX_RES=$($BINARY tx rep propose-project \
    "Challenge Test Project" \
    "Project for testing challenge and jury resolution mechanics" \
    "research" \
    "Technical Council" \
    "100000000000" \
    "5000000" \
    --tags "testing","challenges","jury" \
    --deliverables "Test challenge resolution system" \
    --milestones "Phase 1: Basic challenges,Phase 2: Jury system,Phase 3: Verdict processing" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "❌ Failed to create project: no txhash"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx $TXHASH)

if ! check_tx_success "$TX_RESULT"; then
    echo "❌ Failed to create project"
    exit 1
fi

PROJECT_ID=$(extract_event_value "$TX_RESULT" "project_proposed" "project_id")
if [ -z "$PROJECT_ID" ] || [ "$PROJECT_ID" == "null" ]; then
    echo "⚠️  Could not extract project_id, assuming ID 1"
    PROJECT_ID="1"
fi

echo "✅ Project created: #$PROJECT_ID"

# Approve project budget
echo "  → Approving project budget..."

# Approve 5,000 DREAM (5000000000 micro-DREAM) + 0 SPARK
# Budget must be under large_project_budget_threshold (10,000 DREAM) for direct committee approval
TX_RES=$($BINARY tx rep approve-project-budget \
    $PROJECT_ID \
    "5000000000" \
    "0" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "❌ Failed to approve project: no txhash"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx $TXHASH)

if check_tx_success "$TX_RESULT"; then
    echo "✅ Project #$PROJECT_ID approved and ready for initiatives"
else
    echo "❌ Failed to approve project"
    exit 1
fi

echo ""

# ========================================================================
# 7b. Ensure a Forum Category Exists (for tag_budget_test.sh)
# ========================================================================
# tag_budget_test.sh creates a forum post to award from a tag budget. It
# requires a valid category ID. Prefer existing categories (genesis or
# previously created); fall back to creating one as alice (Commons Ops
# Committee member can create via IsCouncilAuthorized).
echo "Step 7b: Ensuring a forum category exists..."

FIRST_CATEGORY=""
CATEGORIES=$($BINARY query commons list-category --output json 2>&1)
CATEGORY_COUNT=$(echo "$CATEGORIES" | jq -r '.category | length' 2>/dev/null || echo "0")

if [ "$CATEGORY_COUNT" -gt 0 ] 2>/dev/null; then
    FIRST_CATEGORY=$(echo "$CATEGORIES" | jq -r '.category[0].category_id')
    echo "  ⏭️  Using existing category ID: $FIRST_CATEGORY"
else
    echo "  → Creating a test category via alice (Commons Ops Committee)..."
    TX_RES=$($BINARY tx commons create-category \
        "General Discussion" \
        "A category for general forum discussions" \
        "false" \
        "false" \
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
            FIRST_CATEGORY=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
            [ -z "$FIRST_CATEGORY" ] && FIRST_CATEGORY="1"
            echo "  ✅ Category created: $FIRST_CATEGORY"
        else
            echo "  ⚠️  Category creation tx failed; defaulting to ID 1"
            FIRST_CATEGORY="1"
        fi
    else
        echo "  ⚠️  Failed to submit category creation; defaulting to ID 1"
        FIRST_CATEGORY="1"
    fi
fi

echo ""

# ========================================================================
# 8. Build Juror Reputation (Automatic)
# ========================================================================
echo "Step 8: Building juror reputation on test tags..."

# Query the actual minimum juror reputation requirement from chain
MIN_JUROR_REP=$($BINARY query rep params --output json 2>/dev/null | jq -r '.params.min_juror_reputation')
if [ -z "$MIN_JUROR_REP" ] || [ "$MIN_JUROR_REP" == "null" ]; then
    echo "  ⚠️  Could not query min_juror_reputation, using default 20"
    MIN_JUROR_REP_DEC="20"
else
    # LegacyDec values have 18 decimals when serialized
    # Convert from base units (e.g., 20000000000000000000) to decimal (20.0)
    MIN_JUROR_REP_DEC=$(python3 -c "print(int(int('$MIN_JUROR_REP') / (10**18)))" 2>/dev/null)
    # Fallback if python fails or returns empty
    if [ -z "$MIN_JUROR_REP_DEC" ]; then
        echo "  ⚠️  Conversion failed, using default 20"
        MIN_JUROR_REP_DEC="20"
    fi
fi

echo "  Chain requirement: ${MIN_JUROR_REP_DEC} reputation minimum for jury duty"
echo "  Strategy: Build APPRENTICE initiatives (25 rep cap) until requirement met"
echo "  Using fast test params: conviction in ~2 min, auto-completion via EndBlocker"
echo ""

TEST_TAGS=("challenge" "test" "jury")
JUROR_ACCOUNTS=("juror1" "juror2" "juror3")
JUROR_REP_SUCCESS=true

for JUROR in "${JUROR_ACCOUNTS[@]}"; do
    case "$JUROR" in
        "juror1") JUROR_ADDR=$JUROR1_ADDR ;;
        "juror2") JUROR_ADDR=$JUROR2_ADDR ;;
        "juror3") JUROR_ADDR=$JUROR3_ADDR ;;
    esac

    echo "  → Building reputation for $JUROR to reach ${MIN_JUROR_REP_DEC}..."

    # Check current reputation
    CURRENT_REP=0
    MEMBER_INFO=$($BINARY query rep get-member $JUROR_ADDR --output json 2>/dev/null)
    if [ $? -eq 0 ]; then
        JURY_REP=$(echo "$MEMBER_INFO" | jq -r '.member.reputation_scores.jury // "0"')
        if [ ! -z "$JURY_REP" ] && [ "$JURY_REP" != "null" ] && [ "$JURY_REP" != "0" ]; then
            # LegacyDec: Convert from 18-decimal precision to integer
            CURRENT_REP=$(python3 -c "print(int(float('$JURY_REP')))" 2>/dev/null)
            [ -z "$CURRENT_REP" ] && CURRENT_REP=0
        fi
    fi

    if [ "$CURRENT_REP" -ge "$MIN_JUROR_REP_DEC" ] 2>/dev/null; then
        echo "    ✅ $JUROR already has $CURRENT_REP reputation (requirement: $MIN_JUROR_REP_DEC)"
        continue
    fi

    # Calculate how many initiatives needed (APPRENTICE gives 25 rep each)
    REP_NEEDED=$((MIN_JUROR_REP_DEC - CURRENT_REP))
    INITIATIVES_NEEDED=$(( (REP_NEEDED + 24) / 25 ))  # Round up

    echo "    Current: ${CURRENT_REP} rep, Need: ${REP_NEEDED} more, Building: ${INITIATIVES_NEEDED} initiatives"

    # Build initiatives until requirement is met
    for ((i=1; i<=INITIATIVES_NEEDED; i++)); do
        echo "    → Initiative $i/$INITIATIVES_NEEDED for $JUROR..."

    # Create APPRENTICE tier initiative to build juror reputation
    # APPRENTICE: tier=0, min_rep=0, cap=25
    # Budget: 0.25 DREAM (250000 micro) → rep grant = 25 per tag
    # Required conviction = conviction_per_dream × sqrt(budget) = 0.2 × 500 = 100
    # Per-member cap = 33% of required = 33 per staker
    # 4 stakers × 33 cap = 132 > 100 required ✓
    # External conviction: 3 external × 33 = 99 > 50 required ✓
    TX_RES=$($BINARY tx rep create-initiative \
        $PROJECT_ID \
        "Reputation builder for $JUROR" \
        "APPRENTICE tier to build juror reputation" \
        "0" \
        "0" \
        "" \
        "250000" \
        --tags "challenge","test","jury" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "    ⚠️  Failed to create initiative for $JUROR"
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if ! check_tx_success "$TX_RESULT"; then
        echo "    ⚠️  Failed to create initiative for $JUROR"
        continue
    fi

    INIT_ID=$(extract_event_value "$TX_RESULT" "initiative_created" "initiative_id")
    if [ -z "$INIT_ID" ] || [ "$INIT_ID" == "null" ]; then
        echo "    ⚠️  Could not extract initiative_id from event, querying latest initiative..."
        QUERY_RESULT=$($BINARY query rep list-initiative --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$QUERY_RESULT" ]; then
            INIT_ID=$(echo "$QUERY_RESULT" | jq -r '.initiative[-1].id // empty')
            if [ -z "$INIT_ID" ]; then
                echo "    ❌ No initiatives found in query result"
                continue
            fi
        else
            echo "    ❌ Failed to query initiatives"
            continue
        fi
    fi

    echo "    → Initiative #$INIT_ID created, assigning to $JUROR..."

    # Assign to juror
    TX_RES=$($BINARY tx rep assign-initiative \
        $INIT_ID \
        $JUROR_ADDR \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "    ❌ Failed to assign initiative to $JUROR"
        continue
    fi

    sleep 6

    # Submit work as juror
    TX_RES=$($BINARY tx rep submit-initiative-work \
        $INIT_ID \
        "https://example.com/deliverable" \
        "Reputation building work" \
        --from $JUROR \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "    ⚠️  Failed to submit work for $JUROR"
        continue
    fi

    sleep 6

    echo "    → Adding stakes for conviction..."

    # MaxConvictionSharePerMember = 33%, so each staker is capped at 33% of required.
    # Need 4 stakers to exceed 100% (4 × 33% = 132%).
    # Alice is project creator (NOT external). Need 3 external stakers for
    # external conviction >= 50% requirement (3 × 33% = 99% > 50%).
    # Stakers: alice (internal), challenger, anonymous_challenger, expert (all external)
    STAKER_ACCOUNTS=("alice" "challenger" "anonymous_challenger" "expert")

    for STAKER in "${STAKER_ACCOUNTS[@]}"; do
        TX_RES=$($BINARY tx rep stake \
            "stake-target-initiative" \
            $INIT_ID \
            "5000000" \
            --from $STAKER \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --gas 300000 \
            --fees 5000uspark \
            -y --output json 2>&1)

        if echo "$TX_RES" | jq -e '.' >/dev/null 2>&1; then
            TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
            if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
                echo "    ⚠️  Failed to create $STAKER stake (no txhash)"
            else
                sleep 6
                TX_RESULT=$(wait_for_tx $TXHASH)
                if check_tx_success "$TX_RESULT"; then
                    echo "    ✅ $STAKER staked 5 DREAM on initiative #$INIT_ID"
                else
                    echo "    ⚠️  $STAKER stake transaction failed"
                fi
            fi
        else
            echo "    ⚠️  $STAKER stake failed (invalid JSON response)"
        fi
    done

    # Wait for conviction to accumulate
    # With test params: 2 minutes for full conviction, ~7 seconds for 1% conviction
    # We'll wait 2.5 minutes to ensure full conviction is reached
    echo "    → Waiting 2.5 minutes for conviction to accumulate (test params: full conviction = 2 minutes)..."
    sleep 150

    # Debug: Check initiative state
    echo "    → Checking initiative state..."
    INIT_INFO=$($BINARY query rep get-initiative $INIT_ID --output json 2>/dev/null)
    if [ $? -eq 0 ]; then
        STATUS=$(echo "$INIT_INFO" | jq -r '.initiative.status')
        CURRENT_CONV=$(echo "$INIT_INFO" | jq -r '.initiative.current_conviction // "0"')
        REQUIRED_CONV=$(echo "$INIT_INFO" | jq -r '.initiative.required_conviction // "0"')
        EXTERNAL_CONV=$(echo "$INIT_INFO" | jq -r '.initiative.external_conviction // "0"')
        echo "       Status: $STATUS"
        echo "       Conviction: $CURRENT_CONV / $REQUIRED_CONV (external: $EXTERNAL_CONV)"

        # If already COMPLETED (auto-completed by EndBlocker), skip manual completion
        if [ "$STATUS" == "INITIATIVE_STATUS_COMPLETED" ]; then
            echo "    ✅ Initiative #$INIT_ID auto-completed by EndBlocker for $JUROR"
            continue
        fi

        # If already IN_REVIEW, wait for challenge period to end
        if [ "$STATUS" == "INITIATIVE_STATUS_IN_REVIEW" ]; then
            echo "    → Initiative is in review (challenge period active)"

            # Calculate how long to wait based on challenge_period_end
            CHALLENGE_END=$(echo "$INIT_INFO" | jq -r '.initiative.challenge_period_end // "0"')
            CURRENT_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"')

            if [ "$CHALLENGE_END" != "0" ] && [ "$CURRENT_HEIGHT" != "0" ]; then
                BLOCKS_REMAINING=$((CHALLENGE_END - CURRENT_HEIGHT))
                if [ $BLOCKS_REMAINING -gt 0 ]; then
                    SECONDS_REMAINING=$((BLOCKS_REMAINING * 6 + 30))  # 6 sec/block + 30 sec buffer
                    echo "    → Challenge ends at block $CHALLENGE_END (current: $CURRENT_HEIGHT, remaining: $BLOCKS_REMAINING blocks)"
                    echo "    → Waiting $SECONDS_REMAINING seconds for challenge period to end..."
                    sleep $SECONDS_REMAINING
                else
                    echo "    → Challenge period should have ended, waiting 30 seconds for next block..."
                    sleep 30
                fi
            else
                # Fallback: wait for review period (2 epochs) + challenge period (2 epochs) = 4 epochs
                echo "    → Waiting 4.5 minutes for review + challenge periods (4 epochs = 4 minutes)..."
                sleep 270
            fi

            # Check final status
            FINAL_INFO=$($BINARY query rep get-initiative $INIT_ID --output json 2>/dev/null)
            FINAL_STATUS=$(echo "$FINAL_INFO" | jq -r '.initiative.status')
            echo "    → Final status: $FINAL_STATUS"

            if [ "$FINAL_STATUS" == "INITIATIVE_STATUS_COMPLETED" ]; then
                echo "    ✅ Initiative #$INIT_ID completed for $JUROR"
                continue
            fi

            # EndBlocker didn't complete it — try manual completion as fallback
            echo "    → EndBlocker didn't auto-complete, trying manual completion..."
            TX_RES=$($BINARY tx rep approve-initiative \
                $INIT_ID "true" "Manual approval for reputation building" \
                --from alice --chain-id $CHAIN_ID --keyring-backend test \
                --fees 5000uspark -y --output json 2>&1)
            sleep 6
            TX_RES=$($BINARY tx rep complete-initiative \
                $INIT_ID "Manual completion for reputation building" \
                --from alice --chain-id $CHAIN_ID --keyring-backend test \
                --fees 5000uspark -y --output json 2>&1)
            sleep 6
            # Verify
            VERIFY_INFO=$($BINARY query rep get-initiative $INIT_ID --output json 2>/dev/null)
            VERIFY_STATUS=$(echo "$VERIFY_INFO" | jq -r '.initiative.status')
            if [ "$VERIFY_STATUS" == "INITIATIVE_STATUS_COMPLETED" ]; then
                echo "    ✅ Initiative #$INIT_ID manually completed for $JUROR"
            else
                echo "    ⚠️  Initiative #$INIT_ID still not completed (status: $VERIFY_STATUS)"
            fi
            continue
        fi
    fi

    echo "    → Initiative is in $STATUS status, attempting manual completion..."
    echo "    → Approving initiative..."

    # Approve the initiative to complete it and award reputation
    TX_RES=$($BINARY tx rep approve-initiative \
        $INIT_ID \
        "true" \
        "Approved for reputation building" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y --output json 2>&1)

    sleep 6

    echo "    → Completing initiative to award reputation..."

    # Complete the initiative to award reputation
    TX_RES=$($BINARY tx rep complete-initiative \
        $INIT_ID \
        "Completed for reputation building" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    # Debug: Check if TX_RES is valid JSON
    if ! echo "$TX_RES" | jq empty 2>/dev/null; then
        echo "    ⚠️  Failed to complete initiative for $JUROR"
        echo "       Raw output: $TX_RES"
        continue
    fi

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ ! -z "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "    ✅ $JUROR completed initiative #$INIT_ID and earned reputation"
        else
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "    ⚠️  Failed to complete initiative for $JUROR"
            echo "       Error: $RAW_LOG"
        fi
    else
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // "unknown error"')
        echo "    ⚠️  Failed to submit complete-initiative tx for $JUROR"
        echo "       Error: $RAW_LOG"
    fi
    done  # End initiatives loop

    # Verify final reputation
    sleep 3
    FINAL_MEMBER_INFO=$($BINARY query rep get-member $JUROR_ADDR --output json 2>/dev/null)
    if [ $? -eq 0 ]; then
        FINAL_JURY_REP=$(echo "$FINAL_MEMBER_INFO" | jq -r '.member.reputation_scores.jury // "0"')
        if [ ! -z "$FINAL_JURY_REP" ] && [ "$FINAL_JURY_REP" != "null" ] && [ "$FINAL_JURY_REP" != "0" ]; then
            FINAL_REP=$(python3 -c "print(int(float('$FINAL_JURY_REP')))" 2>/dev/null)
            [ -z "$FINAL_REP" ] && FINAL_REP=0
        else
            FINAL_REP=0
        fi
        if [ "$FINAL_REP" -ge "$MIN_JUROR_REP_DEC" ] 2>/dev/null; then
            echo "    ✅ $JUROR final reputation: ${FINAL_REP} (meets ${MIN_JUROR_REP_DEC} requirement)"
        else
            echo "    ⚠️  $JUROR final reputation: ${FINAL_REP} (below ${MIN_JUROR_REP_DEC} requirement)"
            JUROR_REP_SUCCESS=false
        fi
    fi
done  # End jurors loop

echo ""
if [ "$JUROR_REP_SUCCESS" = true ]; then
    echo "✅ Juror reputation building complete"
    echo "   All jurors meet minimum requirement: ${MIN_JUROR_REP_DEC} reputation"
else
    echo "⚠️  Juror reputation building incomplete"
    echo "   Some jurors did not meet minimum requirement: ${MIN_JUROR_REP_DEC} reputation"
    echo "   Challenge tests requiring jury may fail"
fi
echo "   Tags used: ${TEST_TAGS[@]}"

echo ""

# ========================================================================
# 9. Bootstrap Sentinel/Poster/Moderator Reputation
# ========================================================================
# Sentinel operations, tag moderation, and member-report resolution all
# require reputation tiers. Moved accountability tests (formerly x/forum)
# depend on these tiers:
#   - sentinel1: tier 4 (500+ rep) for thread locking / high-authority actions
#   - sentinel2: tier 3 (200+ rep) for bonding
#   - poster1:   tier 2 (100+ rep) for comfortable post creation
#   - moderator: tier 3 (200+ rep) for council operations
# Each EPIC interim grants 100 rep, but the per-epoch cap is 50 rep/tag/epoch
# (epoch_blocks=10, ~10s per epoch). With ~12s per interim, most interims span
# one epoch, so each gives ~50-100 rep.
echo "Step 9: Bootstrapping reputation for sentinel/poster/moderator..."

bootstrap_reputation() {
    local ACCOUNT=$1
    local COUNT=$2
    echo "  Bootstrapping $COUNT EPIC interims for $ACCOUNT..."

    for i in $(seq 1 $COUNT); do
        # Create EPIC interim (type=other, ref_id=0, ref_type=test, complexity=epic, deadline=999999999)
        TX_RES=$($BINARY tx rep create-interim other 0 "test-$i" epic 999999999 \
            --from $ACCOUNT \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)
        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            echo "    ⚠️  Failed to create interim $i"
            return 1
        fi
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if ! check_tx_success "$TX_RESULT"; then
            echo "    ⚠️  Failed to create interim $i"
            return 1
        fi
        INTERIM_ID=$(extract_event_value "$TX_RESULT" "interim_created" "interim_id")

        # Complete the interim
        TX_RES=$($BINARY tx rep complete-interim $INTERIM_ID "Completed for test setup" \
            --from $ACCOUNT \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)
        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            echo "    ⚠️  Failed to complete interim $i"
            return 1
        fi
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if ! check_tx_success "$TX_RESULT"; then
            echo "    ⚠️  Failed to complete interim $i"
            return 1
        fi
        echo "    ✅ Completed interim $i/$COUNT (ID: $INTERIM_ID)"
    done
    echo "  ✅ Reputation bootstrapped for $ACCOUNT"
    return 0
}

# sentinel1: tier 4 (500+ rep) → 11 EPIC interims
bootstrap_reputation sentinel1 11
echo ""

# sentinel2: tier 3 (200+ rep) → 5 EPIC interims
bootstrap_reputation sentinel2 5
echo ""

# poster1: tier 2 (100+ rep) → 3 EPIC interims
bootstrap_reputation poster1 3
echo ""

# moderator: tier 3 (200+ rep) → 5 EPIC interims
bootstrap_reputation moderator 5
echo ""

# ========================================================================
# 10. Bond Sentinels
# ========================================================================
# Accountability tests expect sentinel1 and sentinel2 to be bonded with
# 100 DREAM (100000000 micro-DREAM) so they can cosign reports and take
# sentinel actions. Skip if already bonded (idempotent rerun).
echo "Step 10: Bonding sentinels..."

bond_sentinel_if_needed() {
    local ACCOUNT=$1
    local AMOUNT="100000000"  # 100 DREAM

    # Check existing bond via bonded-role query (exit non-zero if no record)
    STATUS_JSON=$($BINARY query rep bonded-role forum-sentinel $($BINARY keys show $ACCOUNT -a --keyring-backend test) --output json 2>&1)
    EXISTING_BOND=$(echo "$STATUS_JSON" | jq -r '.bonded_role.current_bond // "0"' 2>/dev/null)
    if [ -n "$EXISTING_BOND" ] && [ "$EXISTING_BOND" != "0" ] && [ "$EXISTING_BOND" != "null" ]; then
        echo "  ⏭️  $ACCOUNT already bonded ($EXISTING_BOND micro-DREAM), skipping"
        return 0
    fi

    echo "  → Bonding $ACCOUNT with 100 DREAM..."
    TX_RES=$($BINARY tx rep bond-role forum-sentinel $AMOUNT \
        --from $ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  ⚠️  Failed to bond $ACCOUNT: no txhash"
        return 1
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        echo "  ✅ $ACCOUNT bonded"
    else
        echo "  ⚠️  Failed to bond $ACCOUNT"
    fi
}

bond_sentinel_if_needed sentinel1
bond_sentinel_if_needed sentinel2

echo ""

# ========================================================================
# Export Environment Variables
# ========================================================================
cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables
export ALICE_ADDR=$ALICE_ADDR
export CHALLENGER_ADDR=$CHALLENGER_ADDR
export ANON_CHALLENGER_ADDR=$ANON_CHALLENGER_ADDR
export ASSIGNEE_ADDR=$ASSIGNEE_ADDR
export JUROR1_ADDR=$JUROR1_ADDR
export JUROR2_ADDR=$JUROR2_ADDR
export JUROR3_ADDR=$JUROR3_ADDR
export EXPERT_ADDR=$EXPERT_ADDR
export SENTINEL1_ADDR=$SENTINEL1_ADDR
export SENTINEL2_ADDR=$SENTINEL2_ADDR
export POSTER1_ADDR=$POSTER1_ADDR
export POSTER2_ADDR=$POSTER2_ADDR
export MODERATOR_ADDR=$MODERATOR_ADDR
export BOUNTY_CREATOR_ADDR=$BOUNTY_CREATOR_ADDR
export TEST_PROJECT_ID=$PROJECT_ID
export TEST_CATEGORY_ID=$FIRST_CATEGORY
EOF

echo "=================================================="
echo "✅✅✅ SETUP COMPLETE ✅✅✅"
echo "=================================================="
echo ""
echo "Test environment ready:"
echo "  → 13 test accounts created and funded"
echo "  → All accounts are x/rep members with DREAM"
echo "  → Sentinels bonded, poster/moderator reputation bootstrapped"
echo "  → Test project #$PROJECT_ID created and approved"
echo "  → Test forum category #$FIRST_CATEGORY ready"
echo ""
echo "Environment variables saved to: $SCRIPT_DIR/.test_env"
echo "Source this file in your tests: source .test_env"
echo ""

if [ "$ALL_SUCCESS" = false ]; then
    echo "⚠️  Some accounts may not be properly initialized"
    echo "Review the output above for errors"
    exit 1
fi
