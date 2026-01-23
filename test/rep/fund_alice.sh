#!/bin/bash

echo "========================================================================="
echo "  FUND ALICE WITH DREAM FOR TESTING"
echo "========================================================================="
echo ""

# ========================================================================
# Configuration
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Default amount: 300 DREAM (enough for all tests)
AMOUNT_DREAM=${1:-300}
AMOUNT_MICRO=$((AMOUNT_DREAM * 1000000))

# ========================================================================
# Load Test Environment
# ========================================================================
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "❌ Test environment not found"
    echo "   Run setup_test_accounts.sh first"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# Get Alice address
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)

# ========================================================================
# Check Current Balance
# ========================================================================
echo "Checking Alice's current DREAM balance..."
ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
ALICE_DREAM=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // 0')
ALICE_DREAM_DISPLAY=$(echo "scale=2; $ALICE_DREAM / 1000000" | bc 2>/dev/null || echo "0")

echo "  Current: $ALICE_DREAM_DISPLAY DREAM"
echo "  Target:  $AMOUNT_DREAM DREAM"
echo ""

# ========================================================================
# Determine Funding Source
# ========================================================================
echo "Checking test account balances for funding source..."

BEST_SOURCE=""
BEST_BALANCE=0

for ACCOUNT in "challenger" "assignee" "juror1" "juror2" "juror3" "expert"; do
    ADDR=""
    case "$ACCOUNT" in
        "challenger") ADDR=$CHALLENGER_ADDR ;;
        "assignee") ADDR=$ASSIGNEE_ADDR ;;
        "juror1") ADDR=$JUROR1_ADDR ;;
        "juror2") ADDR=$JUROR2_ADDR ;;
        "juror3") ADDR=$JUROR3_ADDR ;;
        "expert") ADDR=$EXPERT_ADDR ;;
    esac

    if [ -n "$ADDR" ]; then
        MEMBER=$($BINARY query rep get-member $ADDR -o json 2>/dev/null)
        BALANCE=$(echo "$MEMBER" | jq -r '.member.dream_balance // 0')

        if [ "$BALANCE" -gt "$BEST_BALANCE" ]; then
            BEST_BALANCE=$BALANCE
            BEST_SOURCE=$ACCOUNT
        fi
    fi
done

if [ -z "$BEST_SOURCE" ] || [ "$BEST_BALANCE" -eq 0 ]; then
    echo "❌ No test accounts with DREAM found"
    echo "   Run setup_test_accounts.sh to create funded test accounts"
    exit 1
fi

BEST_BALANCE_DISPLAY=$(echo "scale=2; $BEST_BALANCE / 1000000" | bc 2>/dev/null || echo "0")
echo "  Best source: $BEST_SOURCE with $BEST_BALANCE_DISPLAY DREAM"
echo ""

# ========================================================================
# Calculate Amount to Transfer
# ========================================================================
if [ "$ALICE_DREAM" -ge "$AMOUNT_MICRO" ]; then
    echo "✅ Alice already has sufficient DREAM"
    exit 0
fi

NEEDED=$((AMOUNT_MICRO - ALICE_DREAM))
NEEDED_DISPLAY=$(echo "scale=2; $NEEDED / 1000000" | bc 2>/dev/null || echo "0")

echo "Need to transfer: $NEEDED_DISPLAY DREAM"
echo ""

# Check if source has enough
if [ "$BEST_BALANCE" -lt "$NEEDED" ]; then
    echo "⚠️  Warning: $BEST_SOURCE only has $BEST_BALANCE_DISPLAY DREAM"
    echo "   Will transfer what's available"
    NEEDED=$BEST_BALANCE
fi

# ========================================================================
# Transfer DREAM via Tips
# ========================================================================
echo "Transferring DREAM to Alice..."
echo ""

# Tip max is 100 DREAM, so we may need multiple tips
MAX_TIP=100000000  # 100 DREAM in micro-DREAM
REMAINING=$NEEDED
TIP_COUNT=0

while [ $REMAINING -gt 0 ]; do
    TIP_AMOUNT=$((REMAINING < MAX_TIP ? REMAINING : MAX_TIP))
    TIP_AMOUNT_DISPLAY=$(echo "scale=2; $TIP_AMOUNT / 1000000" | bc 2>/dev/null || echo "0")
    TIP_COUNT=$((TIP_COUNT + 1))

    echo "  Tip #$TIP_COUNT: Transferring $TIP_AMOUNT_DISPLAY DREAM from $BEST_SOURCE to Alice..."

    TX_RES=$($BINARY tx rep transfer-dream \
        $ALICE_ADDR \
        "$TIP_AMOUNT" \
        "tip" \
        "Test funding - Tip #$TIP_COUNT" \
        --from $BEST_SOURCE \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  ❌ Failed to send tip (no txhash)"
        CODE=$(echo "$TX_RES" | jq -r '.code // "unknown"')
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // "unknown"' | head -1)
        echo "     Code: $CODE"
        echo "     Error: $RAW_LOG"
        break
    fi

    sleep 2

    # Verify transaction success
    TX_DETAIL=$($BINARY query tx $TXHASH -o json 2>&1)
    TX_CODE=$(echo "$TX_DETAIL" | jq -r '.code // 1')

    if [ "$TX_CODE" == "0" ]; then
        echo "  ✅ Tip #$TIP_COUNT successful"
        REMAINING=$((REMAINING - TIP_AMOUNT))
    else
        echo "  ❌ Tip #$TIP_COUNT failed"
        RAW_LOG=$(echo "$TX_DETAIL" | jq -r '.raw_log // "unknown"')
        echo "     Error: $RAW_LOG"
        break
    fi

    # Prevent hitting tip limit (10 tips per epoch)
    if [ $TIP_COUNT -ge 10 ]; then
        echo "  ⚠️  Reached max tips per epoch (10)"
        break
    fi
done

echo ""

# ========================================================================
# Verify Final Balance
# ========================================================================
echo "Verifying Alice's new balance..."
sleep 2

ALICE_MEMBER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null)
ALICE_DREAM_NEW=$(echo "$ALICE_MEMBER" | jq -r '.member.dream_balance // 0')
ALICE_DREAM_NEW_DISPLAY=$(echo "scale=2; $ALICE_DREAM_NEW / 1000000" | bc 2>/dev/null || echo "0")

echo "  Alice new balance: $ALICE_DREAM_NEW_DISPLAY DREAM"
echo ""

if [ "$ALICE_DREAM_NEW" -ge "$AMOUNT_MICRO" ]; then
    echo "✅ Alice has been successfully funded"
else
    STILL_NEEDED=$((AMOUNT_MICRO - ALICE_DREAM_NEW))
    STILL_NEEDED_DISPLAY=$(echo "scale=2; $STILL_NEEDED / 1000000" | bc 2>/dev/null || echo "0")
    echo "⚠️  Alice still needs $STILL_NEEDED_DISPLAY more DREAM"
    echo "   This may be due to:"
    echo "     - Transfer tax (3% burned on each tip)"
    echo "     - Tip limit (max 10 tips per epoch)"
    echo "     - Source account insufficient balance"
fi

echo ""
echo "========================================================================="
echo "  FUNDING COMPLETE"
echo "========================================================================="
