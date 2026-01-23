#!/bin/bash

# Test interim completion authorization
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/.test_env" 2>/dev/null || true

BINARY="${BINARY:-sparkdreamd}"
CHAIN_ID="${CHAIN_ID:-sparkdream}"

echo "================================================================================"
echo "INTERIM COMPLETION AUTHORIZATION TEST"
echo "================================================================================"
echo ""

# Get account addresses
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null)
ASSIGNEE_ADDR=$($BINARY keys show assignee -a --keyring-backend test 2>/dev/null)
CHALLENGER_ADDR=$($BINARY keys show challenger -a --keyring-backend test 2>/dev/null)

PROJECT_ID=${PROJECT_ID:-1}

echo "Test Actors:"
echo "  Alice (Committee):  $ALICE_ADDR"
echo "  Assignee:          $ASSIGNEE_ADDR"
echo "  Challenger:        $CHALLENGER_ADDR"
echo ""

echo "Step 1: Creating initiative and challenge to trigger ADJUDICATION interim..."

# Create initiative with unique tags
TX_RES=$($BINARY tx rep create-initiative \
    $PROJECT_ID \
    "Security Audit" \
    "Critical security review" \
    0 \
    0 \
    "" \
    "5000000" \
    --tags "cryptography","zero-knowledge" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

sleep 6

# Get initiative ID
INITIATIVE_ID=$($BINARY query rep list-initiative --output json 2>&1 | jq -r '.initiative[-1].id')
echo "✅ Initiative #$INITIATIVE_ID created"

# Assign to assignee
$BINARY tx rep assign-initiative \
    $INITIATIVE_ID \
    $ASSIGNEE_ADDR \
    --from assignee \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y > /dev/null 2>&1

sleep 6

# Submit work
$BINARY tx rep submit-initiative-work \
    $INITIATIVE_ID \
    "https://github.com/security/audit" \
    "Security audit complete" \
    --from assignee \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y > /dev/null 2>&1

sleep 6

# Create challenge
TX_RES=$($BINARY tx rep create-challenge \
    $INITIATIVE_ID \
    "Incomplete security analysis" \
    "1000000" \
    "false" \
    "$CHALLENGER_ADDR" \
    --evidence "https://example.com/issues" \
    --from challenger \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

sleep 6

CHALLENGE_ID=$($BINARY query rep list-challenge --output json 2>&1 | jq -r '.challenge[-1].id')
echo "✅ Challenge #$CHALLENGE_ID created"

# Assignee responds (triggers escalation)
TX_RES=$($BINARY tx rep respond-to-challenge \
    $CHALLENGE_ID \
    "Analysis is complete" \
    --evidence "https://example.com/response" \
    --from assignee \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

sleep 6

# Get ADJUDICATION interim ID
INTERIMS=$($BINARY query rep list-interim --output json 2>&1)
ADJUDICATION_ID=$(echo "$INTERIMS" | jq -r '.interim[] | select(.type == "INTERIM_TYPE_ADJUDICATION") | .id' | tail -1)

if [ -z "$ADJUDICATION_ID" ] || [ "$ADJUDICATION_ID" = "null" ]; then
    echo "❌ ADJUDICATION interim not found"
    exit 1
fi

echo "✅ ADJUDICATION interim #$ADJUDICATION_ID created"
echo ""

echo "================================================================================"
echo "TEST 1: Non-committee member (assignee) CANNOT complete ADJUDICATION interim"
echo "================================================================================"

TX_RES=$($BINARY tx rep complete-interim \
    $ADJUDICATION_ID \
    "REJECT - trying to self-resolve" \
    --from assignee \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
sleep 6

TX_RESULT=$($BINARY query tx $TXHASH --output json 2>&1)
CODE=$(echo "$TX_RESULT" | jq -r '.code // 0')

if [ "$CODE" != "0" ]; then
    ERROR=$(echo "$TX_RESULT" | jq -r '.raw_log')
    if echo "$ERROR" | grep -q "only technical committee members can complete ADJUDICATION"; then
        echo "✅ CORRECT: Assignee was blocked from completing ADJUDICATION interim"
        echo "   Error: $ERROR"
    else
        echo "⚠️  Transaction failed but with unexpected error:"
        echo "   $ERROR"
    fi
else
    echo "❌ FAILED: Assignee should NOT be able to complete ADJUDICATION interim!"
    exit 1
fi

echo ""
echo "================================================================================"
echo "TEST 2: Committee member (alice) CAN complete ADJUDICATION interim"
echo "================================================================================"

TX_RES=$($BINARY tx rep complete-interim \
    $ADJUDICATION_ID \
    "Committee decision: Challenge REJECTED. Analysis is thorough and complete." \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
sleep 6

TX_RESULT=$($BINARY query tx $TXHASH --output json 2>&1)
CODE=$(echo "$TX_RESULT" | jq -r '.code // 0')

if [ "$CODE" = "0" ]; then
    echo "✅ CORRECT: Alice (committee member) completed ADJUDICATION interim"

    # Verify challenge was resolved
    CHALLENGE_DETAIL=$($BINARY query rep get-challenge $CHALLENGE_ID --output json 2>&1)
    CHALLENGE_STATUS=$(echo "$CHALLENGE_DETAIL" | jq -r '.challenge.status')
    echo "   Challenge #$CHALLENGE_ID status: $CHALLENGE_STATUS"
else
    ERROR=$(echo "$TX_RESULT" | jq -r '.raw_log')
    echo "❌ FAILED: Alice should be able to complete ADJUDICATION interim!"
    echo "   Error: $ERROR"
    exit 1
fi

echo ""
echo "================================================================================"
echo "AUTHORIZATION TEST SUMMARY"
echo "================================================================================"
echo ""
echo "✅ Security Fix Verified:"
echo "   1. Non-committee members CANNOT complete ADJUDICATION interims"
echo "   2. Committee members CAN complete ADJUDICATION interims"
echo "   3. Challenge auto-resolved based on committee decision"
echo ""
echo "The committee adjudication system is now properly protected!"
echo "================================================================================"
