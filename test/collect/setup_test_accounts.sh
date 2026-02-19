#!/bin/bash
# Setup test accounts for x/collect integration tests
#
# Creates: collector1 (member), collector2 (member), nonmember1 (not a member)
# Funds each with SPARK, invites collector1/collector2 as x/rep members

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"

echo "========================================================================="
echo "  X/COLLECT - SETUP TEST ACCOUNTS"
echo "========================================================================="
echo ""

ALICE_ADDR=$(get_address alice)
if [ -z "$ALICE_ADDR" ]; then
    echo "ERROR: Alice account not found in keyring"
    exit 1
fi
echo "Alice address: $ALICE_ADDR"

# =========================================================================
# Step 1: Create test account keys
# =========================================================================
echo ""
echo "--- Step 1: Creating test account keys ---"

for ACCT in collector1 collector2 nonmember1; do
    EXISTING=$($BINARY keys show $ACCT -a --keyring-backend $KEYRING 2>/dev/null || true)
    if [ -n "$EXISTING" ]; then
        echo "  $ACCT already exists: $EXISTING"
    else
        $BINARY keys add $ACCT --keyring-backend $KEYRING 2>/dev/null
        ADDR=$($BINARY keys show $ACCT -a --keyring-backend $KEYRING 2>/dev/null)
        echo "  Created $ACCT: $ADDR"
    fi
done

COLLECTOR1_ADDR=$(get_address collector1)
COLLECTOR2_ADDR=$(get_address collector2)
NONMEMBER1_ADDR=$(get_address nonmember1)

echo ""
echo "  collector1: $COLLECTOR1_ADDR"
echo "  collector2: $COLLECTOR2_ADDR"
echo "  nonmember1: $NONMEMBER1_ADDR"

# =========================================================================
# Step 2: Fund accounts with SPARK
# =========================================================================
echo ""
echo "--- Step 2: Funding accounts with SPARK ---"

FUND_AMOUNT="200000000uspark"  # 200 SPARK each

for ACCT in collector1 collector2 nonmember1; do
    ADDR=$(get_address $ACCT)
    echo "  Sending $FUND_AMOUNT to $ACCT ($ADDR)..."
    TX_RES=$(send_tx bank send alice "$ADDR" "$FUND_AMOUNT" --from alice)
    TXHASH=$(get_txhash "$TX_RES")
    if [ -z "$TXHASH" ]; then
        echo "  ERROR: Failed to send to $ACCT"
        echo "  Output: $TX_RES"
        exit 1
    fi
    sleep $TX_WAIT
    echo "  Funded $ACCT"
done

echo "  All accounts funded"

# =========================================================================
# Step 3: Invite collector1 and collector2 as x/rep members
# =========================================================================
echo ""
echo "--- Step 3: Inviting collector1 and collector2 to x/rep ---"

for ACCT in collector1 collector2; do
    ADDR=$(get_address $ACCT)

    # Check if already a member
    MEMBER_CHECK=$($BINARY query rep get-member "$ADDR" -o json 2>/dev/null || echo '{}')
    IS_MEMBER=$(echo "$MEMBER_CHECK" | jq -r '.member.address // empty' 2>/dev/null)
    if [ -n "$IS_MEMBER" ]; then
        echo "  $ACCT is already an x/rep member"
        continue
    fi

    # Check if already invited (pending invitation)
    INVITATION_ID=""
    INVITE_LIST=$($BINARY query rep list-invitation -o json 2>/dev/null || echo '{"invitation":[]}')
    INVITATION_ID=$(echo "$INVITE_LIST" | jq -r --arg addr "$ADDR" \
        '.invitation[] | select(.invitee_address==$addr) | .id // empty' 2>/dev/null | head -1)

    if [ -z "$INVITATION_ID" ]; then
        # Alice invites (invite-member [invitee-address] [staked-dream])
        echo "  Alice inviting $ACCT..."
        TX_RES=$(send_tx rep invite-member "$ADDR" 100 --from alice)
        TXHASH=$(get_txhash "$TX_RES")
        if [ -z "$TXHASH" ]; then
            echo "  WARNING: Failed to invite $ACCT"
            continue
        fi
        sleep $TX_WAIT

        # Query the invitation ID
        INVITE_LIST=$($BINARY query rep list-invitation -o json 2>/dev/null || echo '{"invitation":[]}')
        INVITATION_ID=$(echo "$INVITE_LIST" | jq -r --arg addr "$ADDR" \
            '.invitation[] | select(.invitee_address==$addr) | .id // empty' 2>/dev/null | head -1)
    else
        echo "  $ACCT already has pending invitation (ID=$INVITATION_ID)"
    fi

    if [ -z "$INVITATION_ID" ]; then
        echo "  WARNING: Could not find invitation ID for $ACCT"
        continue
    fi

    # Accept invitation with invitation ID
    echo "  $ACCT accepting invitation (ID=$INVITATION_ID)..."
    TX_RES=$(send_tx rep accept-invitation "$INVITATION_ID" --from "$ACCT")
    TXHASH=$(get_txhash "$TX_RES")
    if [ -z "$TXHASH" ]; then
        echo "  WARNING: Failed to accept invitation for $ACCT"
    else
        sleep $TX_WAIT
    fi

    # Verify membership
    MEMBER_CHECK=$($BINARY query rep get-member "$ADDR" -o json 2>/dev/null || echo '{}')
    IS_MEMBER=$(echo "$MEMBER_CHECK" | jq -r '.member.address // empty' 2>/dev/null)
    if [ -n "$IS_MEMBER" ]; then
        echo "  $ACCT is now an x/rep member"
    else
        echo "  WARNING: $ACCT may not be a member yet"
    fi
done

# =========================================================================
# Step 4: Distribute DREAM to members (from Alice's genesis allocation)
# =========================================================================
echo ""
echo "--- Step 4: Distributing DREAM to members ---"

# Endorsement requires EndorsementDreamStake (100 DREAM). Transfer enough
# to cover the 3% transfer tax: 200 raw → ~194 net after tax.
DREAM_AMOUNT=200

for ACCT in collector1 collector2; do
    ADDR=$(get_address $ACCT)
    DREAM_BAL=$($BINARY query rep get-member "$ADDR" -o json 2>/dev/null | jq -r '.member.dream_balance // "0"')
    if [ "$DREAM_BAL" -ge 100 ] 2>/dev/null; then
        echo "  $ACCT already has $DREAM_BAL DREAM"
        continue
    fi

    echo "  Alice gifting $DREAM_AMOUNT DREAM to $ACCT..."
    TX_RES=$(send_tx rep transfer-dream "$ADDR" "$DREAM_AMOUNT" gift "test-setup" --from alice)
    TXHASH=$(get_txhash "$TX_RES")
    if [ -z "$TXHASH" ]; then
        echo "  WARNING: Failed to transfer DREAM to $ACCT"
    else
        sleep $TX_WAIT
        DREAM_BAL=$($BINARY query rep get-member "$ADDR" -o json 2>/dev/null | jq -r '.member.dream_balance // "0"')
        echo "  $ACCT now has $DREAM_BAL DREAM"
    fi
done

# =========================================================================
# Step 5: Export test environment
# =========================================================================
echo ""
echo "--- Step 5: Exporting test environment ---"

cat > "$SCRIPT_DIR/.test_env" <<EOF
# Auto-generated by setup_test_accounts.sh
ALICE_ADDR=$ALICE_ADDR
COLLECTOR1_ADDR=$COLLECTOR1_ADDR
COLLECTOR2_ADDR=$COLLECTOR2_ADDR
NONMEMBER1_ADDR=$NONMEMBER1_ADDR
EOF

echo "  Saved to $SCRIPT_DIR/.test_env"

# =========================================================================
# Verify final state
# =========================================================================
echo ""
echo "--- Final State ---"
for ACCT in alice collector1 collector2 nonmember1; do
    ADDR=$(get_address $ACCT)
    BAL=$($BINARY query bank balance "$ADDR" uspark --output json 2>/dev/null | jq -r '.balance.amount // "0"')
    BAL_DISPLAY=$(echo "scale=2; $BAL / 1000000" | bc 2>/dev/null || echo "?")
    DREAM_BAL=$($BINARY query rep get-member "$ADDR" -o json 2>/dev/null | jq -r '.member.dream_balance // "n/a"')
    echo "  $ACCT: $BAL_DISPLAY SPARK, $DREAM_BAL DREAM"
done

echo ""
echo "Setup complete."
