#!/bin/bash

echo "=================================================="
echo "SETUP: Initializing Test Accounts for x/reveal Tests"
echo "=================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Get alice and bob addresses (genesis members / council members)
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo "Genesis member (Alice): $ALICE_ADDR"
echo "Genesis member (Bob):   $BOB_ADDR"
echo ""

# Delete stale .test_env so it is regenerated from the current keyring
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Removing stale .test_env (will be regenerated at end of setup)..."
    rm -f "$SCRIPT_DIR/.test_env"
fi

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
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
        return 1
    fi
    return 0
}

# ========================================================================
# 1. Create Test Account Keys (if not exist)
# ========================================================================
echo "Step 1: Creating test account keys..."

ACCOUNTS=("staker1" "staker2" "staker3")

for ACCOUNT in "${ACCOUNTS[@]}"; do
    if ! $BINARY keys show $ACCOUNT --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add $ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
        echo "  Created key: $ACCOUNT"
    else
        echo "  Key exists: $ACCOUNT"
    fi
done

# Get addresses
STAKER1_ADDR=$($BINARY keys show staker1 -a --keyring-backend test)
STAKER2_ADDR=$($BINARY keys show staker2 -a --keyring-backend test)
STAKER3_ADDR=$($BINARY keys show staker3 -a --keyring-backend test)

echo ""

# ========================================================================
# 2. Fund Test Accounts with SPARK (for gas fees)
# ========================================================================
echo "Step 2: Funding test accounts with SPARK for gas fees..."

for ADDR in $STAKER1_ADDR $STAKER2_ADDR $STAKER3_ADDR; do
    echo "  Sending 10 SPARK to $ADDR..."
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
        echo "  Failed to send SPARK: no txhash"
        continue
    fi

    sleep 6
done

echo "  All accounts funded with SPARK"
echo ""

# ========================================================================
# 3. Invite Test Accounts to x/rep (Required for staking)
# ========================================================================
echo "Step 3: Inviting test accounts to become x/rep members..."

INVITATION_IDS=()

for i in "${!ACCOUNTS[@]}"; do
    ACCOUNT="${ACCOUNTS[$i]}"

    case "$ACCOUNT" in
        "staker1") ADDR=$STAKER1_ADDR ;;
        "staker2") ADDR=$STAKER2_ADDR ;;
        "staker3") ADDR=$STAKER3_ADDR ;;
        *) echo "Unknown account: $ACCOUNT"; continue ;;
    esac

    # Check if already a member
    MEMBER_INFO=$($BINARY query rep get-member $ADDR --output json 2>&1)
    if ! echo "$MEMBER_INFO" | grep -q "not found"; then
        echo "  $ACCOUNT is already a member, skipping invitation"
        INVITATION_IDS+=("")
        continue
    fi

    echo "  Inviting $ACCOUNT ($ADDR)..."

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
        echo "  Failed to invite $ACCOUNT: no txhash"
        INVITATION_IDS+=("")
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        INVITATION_ID=$(extract_event_value "$TX_RESULT" "create_invitation" "invitation_id")
        if [ -z "$INVITATION_ID" ]; then
            echo "  Could not extract invitation_id, using index: $((i + 1))"
            INVITATION_ID=$((i + 1))
        fi
        INVITATION_IDS+=($INVITATION_ID)
        echo "  Invited $ACCOUNT (invitation #$INVITATION_ID)"
    else
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "invitation already exists"; then
            echo "  $ACCOUNT already has an invitation"
            INVITATION_IDS+=("")
        else
            echo "  Failed to invite $ACCOUNT: $RAW_LOG"
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
        echo "  Skipping $ACCOUNT (no invitation ID or already member)"
        continue
    fi

    echo "  $ACCOUNT accepting invitation #$INVITATION_ID..."

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
        echo "  Failed to accept invitation: no txhash"
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  $ACCOUNT is now a member"
    else
        echo "  Failed: $ACCOUNT could not accept invitation"
    fi
done

echo ""

# ========================================================================
# 5. Transfer DREAM to Test Accounts (needed for staking)
# ========================================================================
echo "Step 5: Transferring DREAM to test accounts..."

for ACCOUNT in "${ACCOUNTS[@]}"; do
    case "$ACCOUNT" in
        "staker1") ADDR=$STAKER1_ADDR ;;
        "staker2") ADDR=$STAKER2_ADDR ;;
        "staker3") ADDR=$STAKER3_ADDR ;;
        *) continue ;;
    esac

    DREAM_AMOUNT="500000000"  # 500 DREAM
    echo "  Sending 500 DREAM to $ACCOUNT (for staking)..."

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
        echo "  Failed to send DREAM to $ACCOUNT: no txhash"
        continue
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Transferred DREAM to $ACCOUNT"
    else
        echo "  Failed to transfer DREAM to $ACCOUNT"
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
    case "$ACCOUNT" in
        "staker1") ADDR=$STAKER1_ADDR ;;
        "staker2") ADDR=$STAKER2_ADDR ;;
        "staker3") ADDR=$STAKER3_ADDR ;;
        *) continue ;;
    esac

    MEMBER_INFO=$($BINARY query rep get-member $ADDR --output json 2>&1)

    if echo "$MEMBER_INFO" | grep -q "not found"; then
        echo "  $ACCOUNT is NOT a member"
        ALL_SUCCESS=false
    else
        DREAM_BALANCE=$(echo "$MEMBER_INFO" | jq -r '.member.dream_balance')
        echo "  $ACCOUNT: $DREAM_BALANCE micro-DREAM"
    fi
done

echo ""

# ========================================================================
# 7. Verify Alice DREAM Balance (she will be the contributor)
# ========================================================================
echo "Step 7: Checking Alice DREAM balance (contributor)..."

ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR --output json 2>&1)
if echo "$ALICE_MEMBER" | grep -q "not found"; then
    echo "  WARNING: Alice is not an x/rep member!"
    ALL_SUCCESS=false
else
    ALICE_DREAM=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance')
    ALICE_TRUST=$(echo "$ALICE_MEMBER" | jq -r '.member.trust_level')
    echo "  Alice DREAM: $ALICE_DREAM micro-DREAM"
    echo "  Alice Trust:  $ALICE_TRUST"
fi

echo ""

# ========================================================================
# 8. Look up Commons Council (needed for approve/reject)
# ========================================================================
echo "Step 8: Looking up Commons Council policy address..."

COUNCIL_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json 2>&1)
COUNCIL_POLICY=$(echo "$COUNCIL_INFO" | jq -r '.extended_group.policy_address')

if [ -z "$COUNCIL_POLICY" ] || [ "$COUNCIL_POLICY" == "null" ]; then
    echo "  WARNING: Could not find Commons Council policy address"
    COUNCIL_POLICY=""
else
    echo "  Council Policy: $COUNCIL_POLICY"
fi

echo ""

# ========================================================================
# 9. Grant Reveal Permissions to Council Policy (via governance proposal)
# ========================================================================
echo "Step 9: Granting reveal permissions to council policy..."

if [ -n "$COUNCIL_POLICY" ] && [ "$COUNCIL_POLICY" != "null" ]; then
    # Get current allowed messages
    CURRENT_PERMS=$($BINARY query commons get-policy-permissions $COUNCIL_POLICY --output json 2>&1 | \
        jq -r '.policy_permissions.allowed_messages // []')

    # Check if reveal permissions already exist
    if echo "$CURRENT_PERMS" | grep -q "sparkdream.reveal.v1"; then
        echo "  Reveal permissions already granted, skipping"
    else
        # Build the full list: existing + reveal messages
        FULL_PERMS=$(echo "$CURRENT_PERMS" | jq '. + [
            "/sparkdream.reveal.v1.MsgApprove",
            "/sparkdream.reveal.v1.MsgReject",
            "/sparkdream.reveal.v1.MsgResolveDispute"
        ] | unique')

        # Get the x/gov module address dynamically
        GOV_ADDR=$($BINARY query auth module-account gov --output json 2>/dev/null | \
            jq -r '.account.value.address // empty')
        if [ -z "$GOV_ADDR" ]; then
            echo "  WARNING: Could not determine x/gov module address"
        fi

        # Create governance proposal JSON
        GOV_PROPOSAL_FILE="$SCRIPT_DIR/proposals/grant_reveal_permissions.json"
        jq -n \
            --arg gov_addr "$GOV_ADDR" \
            --arg policy "$COUNCIL_POLICY" \
            --argjson msgs "$FULL_PERMS" \
        '{
            messages: [{
                "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
                authority: $gov_addr,
                policy_address: $policy,
                allowed_messages: $msgs
            }],
            deposit: "50000000uspark",
            title: "Grant reveal permissions to Commons Council",
            summary: "Allow council to approve/reject/resolve-dispute reveal contributions"
        }' > "$GOV_PROPOSAL_FILE"

        echo "  Submitting governance proposal..."
        SUBMIT_RES=$($BINARY tx gov submit-proposal "$GOV_PROPOSAL_FILE" \
            --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
            --gas 500000 \
            --fees 5000000uspark --output json 2>&1)

        TXHASH=$(echo "$SUBMIT_RES" | jq -r '.txhash')
        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            echo "  WARNING: Failed to submit gov proposal: $SUBMIT_RES"
        else
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)

            if check_tx_success "$TX_RESULT"; then
                # Extract proposal ID
                GOV_PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
                if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
                    # Fallback: query latest proposal
                    GOV_PROP_ID=$($BINARY query gov proposals --status voting_period --output json 2>&1 | \
                        jq -r '.proposals[-1].id // empty')
                fi

                echo "  Gov Proposal ID: $GOV_PROP_ID"

                if [ -n "$GOV_PROP_ID" ]; then
                    # Vote YES from alice (validator with all stake)
                    echo "  Alice voting YES..."
                    $BINARY tx gov vote $GOV_PROP_ID yes \
                        --from alice -y --chain-id $CHAIN_ID --keyring-backend test \
                        --fees 5000uspark --output json > /dev/null 2>&1
                    sleep 6

                    # Wait for the voting period to end (1 minute) and proposal to pass
                    echo "  Waiting for governance voting period (60s)..."
                    sleep 60

                    # Verify proposal passed
                    PROP_STATUS=$($BINARY query gov proposal $GOV_PROP_ID --output json 2>&1 | jq -r '.proposal.status')
                    echo "  Proposal status: $PROP_STATUS"

                    if [ "$PROP_STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
                        echo "  Reveal permissions granted to council"
                    else
                        echo "  WARNING: Governance proposal did not pass (status: $PROP_STATUS)"
                    fi
                else
                    echo "  WARNING: Could not determine gov proposal ID"
                fi
            else
                echo "  WARNING: Gov proposal tx failed"
            fi
        fi
    fi
else
    echo "  WARNING: No council policy, skipping permission grant"
fi

echo ""

# ========================================================================
# Export Environment Variables
# ========================================================================
cat > "$SCRIPT_DIR/.test_env" <<EOF
# Test environment variables for x/reveal tests
export ALICE_ADDR=$ALICE_ADDR
export BOB_ADDR=$BOB_ADDR
export STAKER1_ADDR=$STAKER1_ADDR
export STAKER2_ADDR=$STAKER2_ADDR
export STAKER3_ADDR=$STAKER3_ADDR
export COUNCIL_POLICY=$COUNCIL_POLICY
EOF

echo "=================================================="
echo "SETUP COMPLETE"
echo "=================================================="
echo ""
echo "Test environment ready:"
echo "  3 staker accounts created and funded"
echo "  Alice = contributor (genesis founder, high trust)"
echo "  Bob = council member (for voting)"
echo "  Council Policy: $COUNCIL_POLICY"
echo ""
echo "Environment variables saved to: $SCRIPT_DIR/.test_env"
echo ""

if [ "$ALL_SUCCESS" = false ]; then
    echo "Some accounts may not be properly initialized"
    echo "Review the output above for errors"
    exit 1
fi
