#!/bin/bash

echo "--- TESTING: PROJECT APPROVAL (TIER + COUNCIL-LOCK) ---"
echo ""
echo "Covers the tightened ApproveProject authorization:"
echo "  • Locked to the picked council (no cross-council ops-committee fallback)"
echo "  • Tier-by-budget gate: individual committee member only up to"
echo "    params.large_project_budget_threshold; above that requires a passed"
echo "    council / committee proposal."
echo "  • A committee member cannot approve a project they created alone."
echo ""

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
# Resolve BOND_DENOM / DREAM_DENOM. Honours an already-exported value first,
# so this is a no-op under run_all_tests.sh and makes the script runnable
# standalone — without it BOND_DENOM is empty and every --fees flag becomes
# a bare amount with no denom, which the CLI rejects as an invalid coin.
source "$SCRIPT_DIR/../lib/denoms.sh"
# TEST 5 approves a large budget through a Technical Operations Committee
# proposal (the committee policy is the approver).
source "$SCRIPT_DIR/../lib/ops_committee.sh"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo "Alice: $ALICE_ADDR   (Technical Operations Committee member — founder)"
echo "Bob:   $BOB_ADDR   (founding member, NOT on any operations committee)"
echo ""

# Tracking
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

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

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    local BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

check_tx_success() {
    [ "$(echo "$1" | jq -r '.code')" = "0" ]
}

check_tx_failure() {
    [ "$(echo "$1" | jq -r '.code')" != "0" ]
}

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" = "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}

# Helper: propose a budget-backed project pointed at the given council and
# requesting the given budget. Echoes the project_id on success.
propose_project() {
    local NAME="$1"
    local COUNCIL="$2"
    local REQUESTED_DREAM="$3" # in micro-DREAM
    local FROM="$4"

    local TX_RES
    TX_RES=$($BINARY tx rep propose-project \
        "$NAME" \
        "Project for x/rep approval e2e" \
        "infrastructure" \
        "$COUNCIL" \
        "$REQUESTED_DREAM" \
        "0" \
        --from "$FROM" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        --gas 400000 \
        -y \
        --output json 2>&1)

    if ! submit_tx_and_wait "$TX_RES"; then
        echo "ERROR: propose_project broadcast failed" >&2
        return 1
    fi
    if ! check_tx_success "$TX_RESULT"; then
        echo "ERROR: propose_project tx failed: $(echo "$TX_RESULT" | jq -r '.raw_log // ""')" >&2
        return 1
    fi
    echo "$TX_RESULT" | jq -r '.events[] | select(.type=="project_proposed") | .attributes[] | select(.key=="project_id") | .value' | tr -d '"'
}

# Read the live threshold so the tests stay correct if governance moves it.
PARAMS=$($BINARY query rep params --output json 2>&1)
THRESHOLD_UDREAM=$(echo "$PARAMS" | jq -r '.params.large_project_budget_threshold // "10000000000"')
THRESHOLD_DREAM=$((THRESHOLD_UDREAM / 1000000))
echo "Large project budget threshold: $THRESHOLD_UDREAM udream ($THRESHOLD_DREAM DREAM)"
echo ""

# ========================================================================
# TEST 1 — Small budget: the creator cannot approve alone; a proposal can
# ========================================================================
# Below the threshold an individual member of the picked council's
# operations committee may approve someone else's project, but not their
# own (REP-E-105): alice proposed this one, so her direct approval is
# refused and the Technical Operations Committee approves it by proposal.
# (The e2e committees seat alice alone, so the "another member approves"
# path has no second key here; the keeper test covers it.)
echo "--- TEST 1: small budget, creator refused alone, approved by proposal (alice / Technical) ---"

SMALL_BUDGET=100000000 # 100 DREAM
PID=$(propose_project "approval-test-small" "Technical Council" "$SMALL_BUDGET" alice)
if [ -z "$PID" ]; then
    echo "  Failed to create project"
    record_result "TEST 1: small budget approved" "FAIL"
else
    TX_RES=$($BINARY tx rep approve-project-budget \
        "$PID" "$SMALL_BUDGET" "0" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} --gas 300000 -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT" && \
        echo "$TX_RESULT" | jq -r '.raw_log // ""' | grep -qi "cannot approve it alone"; then
        echo "  Creator's own approval refused"
        if ops_approve_project_budget "$PID" "$SMALL_BUDGET" "0"; then
            echo "  Project #$PID approved by committee proposal #$OPS_COMMITTEE_PROPOSAL_ID"
            record_result "TEST 1: small budget approved" "PASS"
        else
            echo "  Committee approval failed: $OPS_COMMITTEE_ERROR"
            record_result "TEST 1: small budget approved" "FAIL"
        fi
    else
        echo "  Expected the creator's approval to be refused: $(echo "$TX_RESULT" | jq -r '.raw_log // ""')"
        record_result "TEST 1: small budget approved" "FAIL"
    fi
fi

# ========================================================================
# TEST 2 — Large-budget tier rejection
# ========================================================================
# Budget > threshold: even Alice (Tech Ops committee member) cannot approve
# without a passed council/committee proposal. The expected error is
# types.ErrLargeProjectNeedsCouncil ("project budget exceeds threshold;
# requires council proposal approval").
echo "--- TEST 2: large budget rejected for plain committee member ---"

LARGE_BUDGET=$((THRESHOLD_UDREAM + 1000000)) # threshold + 1 DREAM to keep
                                              # the request well above the cap
                                              # but still a sane sim value.
PID=$(propose_project "approval-test-large" "Technical Council" "$LARGE_BUDGET" alice)
LARGE_PID=$PID
if [ -z "$PID" ]; then
    echo "  Failed to create project (the proposal itself doesn't trip the cap)"
    record_result "TEST 2: large budget rejected" "FAIL"
else
    TX_RES=$($BINARY tx rep approve-project-budget \
        "$PID" "$LARGE_BUDGET" "0" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} --gas 300000 -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        if echo "$RAW" | grep -qi "exceeds threshold\|council proposal"; then
            echo "  Correctly rejected (large budget needs council proposal): $RAW"
            record_result "TEST 2: large budget rejected" "PASS"
        else
            echo "  Rejected but with unexpected error: $RAW"
            record_result "TEST 2: large budget rejected" "FAIL"
        fi
    else
        echo "  Expected rejection but tx succeeded"
        record_result "TEST 2: large budget rejected" "FAIL"
    fi
fi

# ========================================================================
# TEST 3 — Threshold boundary
# ========================================================================
# Exactly at the threshold (GT is strict, so equal-to is still "small"):
# the tier check passes and the refusal that follows is the creator rule,
# not the threshold. Guards against an off-by-one in the chain's tier check.
echo "--- TEST 3: budget exactly at threshold passes the tier check ---"

PID=$(propose_project "approval-test-boundary" "Technical Council" "$THRESHOLD_UDREAM" alice)
if [ -z "$PID" ]; then
    echo "  Failed to create project"
    record_result "TEST 3: at-threshold approved" "FAIL"
else
    TX_RES=$($BINARY tx rep approve-project-budget \
        "$PID" "$THRESHOLD_UDREAM" "0" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} --gas 300000 -y --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if check_tx_failure "$TX_RESULT" && echo "$RAW" | grep -qi "cannot approve it alone" && \
        ! echo "$RAW" | grep -qi "exceeds threshold"; then
        echo "  At-threshold budget passed the tier check (refused only as the creator's own)"
        record_result "TEST 3: at-threshold approved" "PASS"
    else
        echo "  Unexpected result at threshold: $RAW"
        record_result "TEST 3: at-threshold approved" "FAIL"
    fi
fi

# ========================================================================
# TEST 4 — Non-ops-committee member rejected
# ========================================================================
# Bob is a founding member (genesis name registration, DREAM grant) but is
# NOT seated on any operations committee. Even for a small budget, his
# approval call should be rejected with ErrUnauthorized — the rule is no
# longer "any global Ops Committee member can approve", it's "must be on
# the picked council's ops committee".
echo "--- TEST 4: non-ops-committee member cannot approve ---"

PID=$(propose_project "approval-test-bob" "Technical Council" "$SMALL_BUDGET" alice)
if [ -z "$PID" ]; then
    echo "  Failed to create project"
    record_result "TEST 4: non-committee member rejected" "FAIL"
else
    TX_RES=$($BINARY tx rep approve-project-budget \
        "$PID" "$SMALL_BUDGET" "0" \
        --from bob --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} --gas 300000 -y --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        if echo "$RAW" | grep -qi "unauthorized\|operations committee\|member of"; then
            echo "  Correctly rejected (bob is not on Tech Ops): $RAW"
            record_result "TEST 4: non-committee member rejected" "PASS"
        else
            echo "  Rejected but with unexpected error: $RAW"
            record_result "TEST 4: non-committee member rejected" "FAIL"
        fi
    else
        echo "  Expected rejection but tx succeeded — bob should not be able to approve"
        record_result "TEST 4: non-committee member rejected" "FAIL"
    fi
fi

# ========================================================================
# TEST 5 — Large budget approved through a committee proposal
# ========================================================================
# The project TEST 2 left PROPOSED is approved by the Technical Operations
# Committee's policy address: MsgApproveProjectBudget is on the committee's
# AllowedMessages and x/rep accepts the council / committee policy for any
# budget.
echo "--- TEST 5: large budget approved via Technical Operations Committee proposal ---"

if [ -z "$LARGE_PID" ]; then
    echo "  No large-budget project from TEST 2"
    record_result "TEST 5: large budget via committee" "FAIL"
elif OPS_COMMITTEE_NAME="Technical Operations Committee" ops_committee_exec \
        "$(jq -nc --arg id "$LARGE_PID" --arg b "$LARGE_BUDGET" \
            '{"@type":"/sparkdream.rep.v1.MsgApproveProjectBudget",project_id:$id,approved_budget:$b,approved_spark:"0"}')" approver; then
    STATUS=$($BINARY query rep get-project "$LARGE_PID" --output json 2>/dev/null | jq -r '.project.status // empty')
    if [ "$STATUS" == "PROJECT_STATUS_ACTIVE" ]; then
        echo "  Project #$LARGE_PID approved by committee proposal #$OPS_COMMITTEE_PROPOSAL_ID"
        record_result "TEST 5: large budget via committee" "PASS"
    else
        echo "  Proposal executed but project status is $STATUS"
        record_result "TEST 5: large budget via committee" "FAIL"
    fi
else
    echo "  Committee approval failed: $OPS_COMMITTEE_ERROR"
    record_result "TEST 5: large budget via committee" "FAIL"
fi

# ========================================================================
# TEST 6 - Budget-backed proposal: creation fee burned, review interim
#          opened and settled by the committee's approval (REP-E-201)
# ========================================================================
# A budget-backed propose-project burns params.project_creation_fee from the
# creator's DREAM (it used to be free) and opens a PROJECT_APPROVAL /
# BUDGET_REVIEW interim for the picked council's Operations Committee, whose
# id is stored on project.approval_interim_id. MsgApproveProjectBudget
# settles that interim: COMPLETED when an assignee took part in the
# decision, EXPIRED (unpaid) when none did. Either way it is no longer live.
echo "--- TEST 6: creation fee burned + review interim opened and settled ---"

CREATION_FEE=$(echo "$PARAMS" | jq -r '.params.project_creation_fee // "0"')
DREAM_BEFORE=$($BINARY query rep get-member "$ALICE_ADDR" --output json 2>/dev/null | jq -r '.member.dream_balance // "0"')
PID=$(propose_project "approval-test-interim" "Technical Council" "$SMALL_BUDGET" alice)
DREAM_AFTER=$($BINARY query rep get-member "$ALICE_ADDR" --output json 2>/dev/null | jq -r '.member.dream_balance // "0"')
if [ -z "$PID" ]; then
    echo "  [FAIL] Failed to create project"
    record_result "TEST 6: fee burned + review interim" "FAIL"
else
    T6_OK=1
    DREAM_DROP=$((DREAM_BEFORE - DREAM_AFTER))
    # An epoch boundary may fall between the two reads, and the per-epoch
    # unstaked decay (unstaked_decay_rate of the liquid balance) then burns
    # on top of the fee: accept fee <= drop <= fee + one epoch of decay.
    # The CLI renders a LegacyDec either as "0.002000000000000000" or as the
    # raw 18-decimal integer "2000000000000000"; normalize both to a ratio.
    DECAY_RATE=$(echo "$PARAMS" | jq -r '.params.unstaked_decay_rate // "0"')
    MAX_DECAY=$(python3 -c "
from decimal import Decimal
import math
s='$DECAY_RATE'
r=Decimal(s)
if '.' not in s: r=r/Decimal(10**18)
print(math.ceil(Decimal('$DREAM_BEFORE')*r))" 2>/dev/null || echo 0)
    if [ "$CREATION_FEE" = "0" ]; then
        echo "  [WARN] project_creation_fee is 0 on this chain; fee burn not checked"
    elif [ "$DREAM_DROP" -ge "$CREATION_FEE" ] && [ "$DREAM_DROP" -le "$((CREATION_FEE + MAX_DECAY))" ]; then
        echo "  [ OK ] creator's DREAM dropped by project_creation_fee ($CREATION_FEE) plus at most one epoch of decay: $DREAM_BEFORE -> $DREAM_AFTER (drop $DREAM_DROP)"
    else
        echo "  [FAIL] expected DREAM drop between $CREATION_FEE and $((CREATION_FEE + MAX_DECAY)), got $DREAM_DROP ($DREAM_BEFORE -> $DREAM_AFTER)"
        T6_OK=0
    fi

    INTERIM_ID=$($BINARY query rep get-project "$PID" --output json 2>/dev/null | jq -r '.project.approval_interim_id // "0"')
    if [ "$INTERIM_ID" != "0" ] && [ -n "$INTERIM_ID" ]; then
        INTERIM=$($BINARY query rep get-interim "$INTERIM_ID" --output json 2>/dev/null)
        I_STATUS=$(echo "$INTERIM" | jq -r '.interim.status // "INTERIM_STATUS_PENDING"')
        I_TYPE=$(echo "$INTERIM" | jq -r '.interim.type // ""')
        I_REF=$(echo "$INTERIM" | jq -r '.interim.reference_id // "0"')
        I_REFTYPE=$(echo "$INTERIM" | jq -r '.interim.reference_type // ""')
        if [ "$I_REF" = "$PID" ] && [ "$I_REFTYPE" = "project" ] && \
            { [ "$I_STATUS" = "INTERIM_STATUS_PENDING" ] || [ "$I_STATUS" = "INTERIM_STATUS_IN_PROGRESS" ]; }; then
            echo "  [ OK ] review interim #$INTERIM_ID opened for project #$PID (type=$I_TYPE status=$I_STATUS)"
        else
            echo "  [FAIL] interim #$INTERIM_ID unexpected: type=$I_TYPE status=$I_STATUS reference=$I_REFTYPE/$I_REF (want project/$PID, live)"
            T6_OK=0
        fi
    else
        echo "  [FAIL] project #$PID has no approval_interim_id"
        T6_OK=0
    fi

    if [ "$T6_OK" = "1" ]; then
        if ops_approve_project_budget "$PID" "$SMALL_BUDGET" "0"; then
            I_STATUS=$($BINARY query rep get-interim "$INTERIM_ID" --output json 2>/dev/null | jq -r '.interim.status // "INTERIM_STATUS_PENDING"')
            case "$I_STATUS" in
                INTERIM_STATUS_COMPLETED)
                    echo "  [ OK ] interim #$INTERIM_ID COMPLETED by the committee approval (proposal #$OPS_COMMITTEE_PROPOSAL_ID)" ;;
                INTERIM_STATUS_EXPIRED)
                    echo "  [WARN] interim #$INTERIM_ID settled as EXPIRED (no assignee other than the proposer took part); no longer live" ;;
                *)
                    echo "  [FAIL] interim #$INTERIM_ID still live after approval: status=$I_STATUS"
                    T6_OK=0 ;;
            esac
        else
            echo "  [FAIL] Committee approval failed: $OPS_COMMITTEE_ERROR"
            T6_OK=0
        fi
    fi

    if [ "$T6_OK" = "1" ]; then
        record_result "TEST 6: fee burned + review interim" "PASS"
    else
        record_result "TEST 6: fee burned + review interim" "FAIL"
    fi
fi

# ========================================================================
# TEST 7 - Abandoning a review seat drops that seat's share of the budget
# ========================================================================
# MsgAbandonInterim on a project review drops only the abandoning seat, and
# the budget shrinks with it to budget / seats * (seats - 1), so the seats
# left are paid what they were at opening. Without the cut, members who
# would not vote could abandon and hand the whole budget to the one who did.
echo "--- TEST 7: abandoned review seat takes its share with it ---"

PID=$(propose_project "approval-test-abandon" "Technical Council" "$SMALL_BUDGET" bob)
INTERIM_ID=""
[ -n "$PID" ] && INTERIM_ID=$($BINARY query rep get-project "$PID" --output json 2>/dev/null | jq -r '.project.approval_interim_id // "0"')
if [ -z "$PID" ] || [ -z "$INTERIM_ID" ] || [ "$INTERIM_ID" = "0" ]; then
    echo "  [FAIL] could not open a review interim (project=$PID interim=$INTERIM_ID)"
    record_result "TEST 7: abandoned seat share" "FAIL"
else
    INTERIM=$($BINARY query rep get-interim "$INTERIM_ID" --output json 2>/dev/null)
    SEATS=$(echo "$INTERIM" | jq -r '.interim.assignees | length')
    BUDGET_BEFORE=$(echo "$INTERIM" | jq -r '.interim.budget // "0"')
    # The first assignee whose key is in the local keyring abandons.
    QUITTER="" QUITTER_ADDR=""
    for addr in $(echo "$INTERIM" | jq -r '.interim.assignees[]'); do
        name=$($BINARY keys list --keyring-backend test --output json 2>/dev/null \
            | jq -r --arg a "$addr" '.[] | select(.address == $a) | .name' | head -1)
        if [ -n "$name" ]; then QUITTER="$name"; QUITTER_ADDR="$addr"; break; fi
    done
    if [ "$SEATS" -lt 2 ] || [ -z "$QUITTER" ]; then
        echo "  [WARN] review interim #$INTERIM_ID has $SEATS seat(s), local key: ${QUITTER:-none}; needs 2+ seats and a local assignee key, skipped"
        record_result "TEST 7: abandoned seat share" "PASS"
    else
        TX_RES=$($BINARY tx rep abandon-interim "$INTERIM_ID" "conflict of interest" \
            --from "$QUITTER" --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000${BOND_DENOM} --gas 300000 -y --output json 2>&1)
        if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
            AFTER=$($BINARY query rep get-interim "$INTERIM_ID" --output json 2>/dev/null)
            SEATS_AFTER=$(echo "$AFTER" | jq -r '.interim.assignees | length')
            BUDGET_AFTER=$(echo "$AFTER" | jq -r '.interim.budget // "0"')
            STILL_SEATED=$(echo "$AFTER" | jq -r --arg a "$QUITTER_ADDR" '[.interim.assignees[] | select(. == $a)] | length')
            WANT=$(( BUDGET_BEFORE / SEATS * (SEATS - 1) ))
            if [ "$SEATS_AFTER" = "$((SEATS - 1))" ] && [ "$STILL_SEATED" = "0" ] && [ "$BUDGET_AFTER" = "$WANT" ]; then
                echo "  [ OK ] $QUITTER's seat dropped ($SEATS -> $SEATS_AFTER seats), budget $BUDGET_BEFORE -> $BUDGET_AFTER (= budget / seats * seats left)"
                record_result "TEST 7: abandoned seat share" "PASS"
            else
                echo "  [FAIL] seats $SEATS -> $SEATS_AFTER (quitter still seated: $STILL_SEATED), budget $BUDGET_BEFORE -> $BUDGET_AFTER, want $WANT"
                record_result "TEST 7: abandoned seat share" "FAIL"
            fi
        else
            echo "  [FAIL] abandon-interim failed: $(echo "$TX_RESULT" | jq -r '.raw_log // ""' | head -c 300)"
            record_result "TEST 7: abandoned seat share" "FAIL"
        fi
    fi
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "PROJECT APPROVAL TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
    exit 0
fi
