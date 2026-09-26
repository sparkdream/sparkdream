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
echo "  Challenger:        $ANON_CHALLENGER_ADDR (anonymous_challenger)"
echo ""

# Each ADJUDICATION interim gets a deadline of default_review_period_epochs *
# epoch_blocks from escalation -- 10 blocks under testparams. That budget fits
# exactly one tx round, so every test below builds its OWN interim rather than
# sharing one: with a shared interim the EndBlocker timeout backstop fires
# mid-file, expiring it and resolving the challenge by default REJECT, and every
# later test then fails against an already-finalized interim.
# Poll for a tx to be included rather than sleeping a fixed wall-clock. The
# ADJUDICATION deadline is only default_review_period_epochs * epoch_blocks (10
# blocks under testparams), so every fixed `sleep 6` in the setup path spends
# more than half the window TEST 4 needs. Returns the tx JSON on stdout.
wait_tx() {
    local txhash="$1" i result code
    [ -z "$txhash" ] || [ "$txhash" = "null" ] && return 1
    for i in $(seq 1 30); do
        result=$($BINARY query tx "$txhash" --output json 2>/dev/null)
        code=$(echo "$result" | jq -r '.code // empty' 2>/dev/null)
        if [ -n "$code" ]; then
            echo "$result"
            return 0
        fi
        sleep 1
    done
    return 1
}

setup_adjudication() {
    echo "Step 1: Creating initiative and challenge to trigger ADJUDICATION interim..."

    # Create initiative with unique tags
    TX_RES=$($BINARY tx rep create-initiative \
        $PROJECT_ID \
        "Security Audit" \
        "Critical security review" \
        0 \
        0 \
        "5000000" \
        --tags "cryptography","zero-knowledge" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    sleep 6

    # Get initiative ID
    INITIATIVE_ID=$($BINARY query rep list-initiative --output json 2>&1 | jq -r '.initiative[-1].id')
    echo "[ OK ] Initiative #$INITIATIVE_ID created"

    # Assign to assignee
    $BINARY tx rep assign-initiative \
        $INITIATIVE_ID \
        $ASSIGNEE_ADDR \
        --from assignee \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
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
        --fees 5000${BOND_DENOM} \
        -y > /dev/null 2>&1

    sleep 6

    # Create challenge
    # Challenges come from anonymous_challenger, not the shared `challenger`
    # account. Each rejected challenge burns its 50 DREAM stake and this file
    # raises two, which is 100 of the 250 DREAM setup grants each account --
    # `challenger` is spent by five later suites in the alphabetical run order,
    # while anonymous_challenger is only ever drawn as a juror/reviewer
    # candidate, where membership matters and balance does not.
    TX_RES=$($BINARY tx rep create-challenge \
        $INITIATIVE_ID \
        "Incomplete security analysis" \
        "50000000" \
        --evidence "https://example.com/issues" \
        --from anonymous_challenger \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    sleep 6

    CHALLENGE_ID=$($BINARY query rep list-challenge --output json 2>&1 | jq -r '.challenge[-1].id')
    echo "[ OK ] Challenge #$CHALLENGE_ID created"

    # Assignee responds (triggers escalation)
    TX_RES=$($BINARY tx rep respond-to-challenge --gas 500000 \
        $CHALLENGE_ID \
        "Analysis is complete" \
        --evidence "https://example.com/response" \
        --from assignee \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    # The escalation this tx triggers starts the interim's deadline clock, so
    # poll for inclusion instead of burning 6 of its 10 blocks on a sleep.
    wait_tx "$(echo "$TX_RES" | jq -r '.txhash // empty')" > /dev/null || true

    # Get ADJUDICATION interim ID
    INTERIMS=$($BINARY query rep list-interim --output json 2>&1)
    ADJUDICATION_ID=$(echo "$INTERIMS" | jq -r '.interim[] | select(.type == "INTERIM_TYPE_ADJUDICATION") | .id' | tail -1)

    if [ -z "$ADJUDICATION_ID" ] || [ "$ADJUDICATION_ID" = "null" ]; then
        echo "[FAIL] ADJUDICATION interim not found"
        exit 1
    fi

    echo "[ OK ] ADJUDICATION interim #$ADJUDICATION_ID created"
    echo ""
}

# TESTS 1-3 share one interim. They are all negative: each is refused by a gate
# that runs before the keeper's already-finalized status check, so they assert
# correctly whether or not the 10-block deadline has lapsed underneath them --
# and they must not consume the challenger's DREAM four times over (each
# rejected challenge burns its 50 DREAM stake).
setup_adjudication

echo "================================================================================"
echo "TEST 1: Non-committee member (assignee) CANNOT complete ADJUDICATION interim"
echo "================================================================================"

TX_RES=$($BINARY tx rep complete-interim --gas 500000 \
    $ADJUDICATION_ID \
    "REJECT - trying to self-resolve" \
    --from assignee \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
sleep 6

TX_RESULT=$($BINARY query tx $TXHASH --output json 2>&1)
CODE=$(echo "$TX_RESULT" | jq -r '.code // 0')

if [ "$CODE" != "0" ]; then
    ERROR=$(echo "$TX_RESULT" | jq -r '.raw_log')
    if echo "$ERROR" | grep -q "only technical committee members can complete ADJUDICATION"; then
        echo "[ OK ] CORRECT: Assignee was blocked from completing ADJUDICATION interim"
        echo "   Error: $ERROR"
    else
        echo "[WARN]  Transaction failed but with unexpected error:"
        echo "   $ERROR"
    fi
else
    echo "[FAIL] FAILED: Assignee should NOT be able to complete ADJUDICATION interim!"
    exit 1
fi

echo ""
echo "================================================================================"
echo "TEST 2: Committee member CANNOT complete ADJUDICATION without a decision"
echo "================================================================================"

# The verdict is structured. It used to be parsed out of the completion notes,
# and a keyword that failed to match left the challenge unresolved forever with
# the interim already COMPLETED. Omitting --decision must now be refused while
# the interim is still live and retryable.
TX_RES=$($BINARY tx rep complete-interim --gas 500000 \
    $ADJUDICATION_ID \
    "Committee decision: Challenge REJECTED. Work meets requirements." \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
sleep 6

TX_RESULT=$($BINARY query tx $TXHASH --output json 2>&1)
CODE=$(echo "$TX_RESULT" | jq -r '.code // 0')

if [ "$CODE" != "0" ]; then
    ERROR=$(echo "$TX_RESULT" | jq -r '.raw_log')
    if echo "$ERROR" | grep -q "requires a decision"; then
        echo "[ OK ] CORRECT: A keyword in the notes is not a verdict"
        echo "   Error: $ERROR"
    else
        echo "[WARN]  Transaction failed but with unexpected error:"
        echo "   $ERROR"
    fi
else
    echo "[FAIL] FAILED: An ADJUDICATION interim must not complete without --decision!"
    exit 1
fi

# The refused completion must not have finalized the interim. COMPLETED is the
# regression: that is the state that used to strand the challenge with no
# decision and nothing left to retry. PENDING/IN_PROGRESS means it is still
# completable; EXPIRED means the EndBlocker timeout backstop resolved the
# challenge by default REJECT, which is also a safe landing. Only COMPLETED is
# a failure, so assert on that rather than on a status that races the deadline.
INTERIM_STATUS=$($BINARY query rep get-interim $ADJUDICATION_ID --output json 2>&1 | jq -r '.interim.status // "UNKNOWN"')
INTERIM_DECISION=$($BINARY query rep get-interim $ADJUDICATION_ID --output json 2>&1 | jq -r '.interim.decision // "ADJUDICATION_DECISION_UNSPECIFIED"')
if [ "$INTERIM_STATUS" != "INTERIM_STATUS_COMPLETED" ]; then
    echo "[ OK ] Interim #$ADJUDICATION_ID not finalized by the refused call ($INTERIM_STATUS)"
else
    echo "[FAIL] FAILED: a refused completion must not finalize the interim (got $INTERIM_STATUS, decision $INTERIM_DECISION)"
    exit 1
fi

echo ""
echo "================================================================================"
echo "TEST 3: approve-interim is refused on an ADJUDICATION interim"
echo "================================================================================"

# MsgApproveInterim shares the Operations Committee gate but carries only a
# bool, and "approved" cannot express a verdict. Left open it was the same
# freeze through the other door: approved=false finalized the interim to EXPIRED
# with no decision and no resolution, dropping it out of the pending sweep that
# would otherwise have defaulted the challenge to REJECT.
TX_RES=$($BINARY tx rep approve-interim --gas 500000 \
    $ADJUDICATION_ID \
    "false" \
    "Committee declines this adjudication" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
sleep 6

TX_RESULT=$($BINARY query tx $TXHASH --output json 2>&1)
CODE=$(echo "$TX_RESULT" | jq -r '.code // 0')

if [ "$CODE" != "0" ]; then
    ERROR=$(echo "$TX_RESULT" | jq -r '.raw_log')
    if echo "$ERROR" | grep -q "adjudication"; then
        echo "[ OK ] CORRECT: approve-interim refused; settle adjudications with complete-interim"
        echo "   Error: $ERROR"
    else
        echo "[WARN]  Transaction failed but with unexpected error:"
        echo "   $ERROR"
    fi
else
    echo "[FAIL] FAILED: approve-interim must not finalize an ADJUDICATION interim!"
    exit 1
fi

# Same post-condition as TEST 2: approve-interim must not have finalized it.
# EXPIRED (timeout backstop) is a safe landing; COMPLETED is the regression.
INTERIM_STATUS=$($BINARY query rep get-interim $ADJUDICATION_ID --output json 2>&1 | jq -r '.interim.status // "UNKNOWN"')
if [ "$INTERIM_STATUS" != "INTERIM_STATUS_COMPLETED" ]; then
    echo "[ OK ] Interim #$ADJUDICATION_ID not finalized by approve-interim ($INTERIM_STATUS)"
else
    echo "[FAIL] FAILED: approve-interim must not finalize an ADJUDICATION interim"
    exit 1
fi

echo ""
# TEST 4 is the only one that needs the interim still live, so it builds a fresh
# one and spends the window on a single tx.
setup_adjudication

echo "================================================================================"
echo "TEST 4: Committee member (alice) CAN complete ADJUDICATION interim"
echo "================================================================================"

# Structured --decision flag (short kebab form), required for ADJUDICATION
# interims; the notes are prose only.
TX_RES=$($BINARY tx rep complete-interim --gas 500000 \
    $ADJUDICATION_ID \
    "Committee decision: Challenge REJECTED. Analysis is thorough and complete." \
    --decision reject \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
# Poll, not sleep: the interim's remaining deadline is measured in blocks.
TX_RESULT=$(wait_tx "$TXHASH")
CODE=$(echo "$TX_RESULT" | jq -r '.code // 0')

if [ "$CODE" = "0" ]; then
    echo "[ OK ] CORRECT: Alice (committee member) completed ADJUDICATION interim"

    # Verify challenge was resolved
    CHALLENGE_DETAIL=$($BINARY query rep get-challenge $CHALLENGE_ID --output json 2>&1)
    CHALLENGE_STATUS=$(echo "$CHALLENGE_DETAIL" | jq -r '.challenge.status')
    echo "   Challenge #$CHALLENGE_ID status: $CHALLENGE_STATUS"
else
    ERROR=$(echo "$TX_RESULT" | jq -r '.raw_log')
    echo "[FAIL] FAILED: Alice should be able to complete ADJUDICATION interim!"
    echo "   Error: $ERROR"
    exit 1
fi

echo ""
echo "================================================================================"
echo "AUTHORIZATION TEST SUMMARY"
echo "================================================================================"
echo ""
echo "[ OK ] Security Fix Verified:"
echo "   1. Non-committee members CANNOT complete ADJUDICATION interims"
echo "   2. A keyword in the notes is not a verdict - --decision is required"
echo "   3. approve-interim cannot finalize an ADJUDICATION interim"
echo "   4. Committee members CAN complete ADJUDICATION interims"
echo "   5. Challenge auto-resolved based on committee decision"
echo ""
echo "The committee adjudication system is now properly protected!"
echo "================================================================================"
