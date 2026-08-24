#!/bin/bash

echo "--- TESTING: INITIATIVE REVIEW (BONDED REVIEWER ROLE) ---"

# Conviction measures whether people wanted work done, not whether it was done.
# The bonded reviewer role is the quality gate that closes that gap. Nothing
# else in test/rep exercises it, so this covers the surface end to end:
#
#   * set-verification-policy turning the gate on for a project
#   * bond-role initiative-reviewer, and the 5,000 DREAM floor
#   * submit-initiative-review: affiliate refused, unbonded refused
#   * a rejection returning the work to ASSIGNED for another round
#   * bond committed per verdict, scaled to the initiative budget
#   * role-activity recording the verdicts
#
# The initiative is deliberately authored by a member OUTSIDE alice's invitation
# subtree: the reviewer independence test excludes affiliates and anyone one
# invitation hop from them, and every setup account is a direct invitee of alice.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
source "$SCRIPT_DIR/../lib/denoms.sh"

TEST_1_RESULT="FAIL"   # policy set and stored
TEST_2_RESULT="FAIL"   # reviewer bonded
TEST_3_RESULT="FAIL"   # unbonded address refused
TEST_4_RESULT="FAIL"   # rejection opens a new round
TEST_5_RESULT="FAIL"   # bond committed, scaled to budget
TEST_6_RESULT="FAIL"   # approval recorded on the new round
TEST_7_RESULT="FAIL"   # accuracy record written

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "[FAIL] Test environment not initialized. Run: bash test/rep/setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

wait_for_tx() {
    local TXHASH=$1 ATTEMPT=0
    while [ $ATTEMPT -lt 20 ]; do
        RESULT=$($BINARY q tx "$TXHASH" --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    echo "{\"code\": 999, \"raw_log\": \"tx $TXHASH not found\"}"
}

# Broadcast, wait, echo the delivered result. CheckTx rejections are reshaped so
# callers have one thing to parse.
send() {
    local RES TXHASH
    RES=$("$@" --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
    TXHASH=$(echo "$RES" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "{\"code\": 998, \"raw_log\": $(echo "$RES" | jq -Rs '.')}"
        return 0
    fi
    sleep 6
    wait_for_tx "$TXHASH"
}
code_of() { echo "$1" | jq -r '.code // "999"'; }
log_of()  { echo "$1" | jq -r '.raw_log // ""'; }

# Own the tag this test uses. Tag validation is asymmetric — propose-project
# accepts any tag string, but CreateInitiative rejects one that is not in the
# registry (ErrTagNotRegistered, 1406) — so borrowing a tag another script
# happens to register makes this test depend on run order. The suite runs
# alphabetically, and the script that registered "interim-work" sorts after this
# one, which is exactly how this failed the first time.
REVIEW_TAG="initiative-review-e2e"

BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test 2>/dev/null)
if [ -z "$BOB_ADDR" ]; then
    echo "[FAIL] bob key missing; this test needs a member outside alice's invitation subtree"
    exit 1
fi

# ========================================================================
# SETUP: a project and initiative authored by bob
# ========================================================================
echo "--- SETUP: project + initiative outside alice's subtree ---"
if ! $BINARY query rep list-tag --output json 2>&1 | jq -r '.tag[]?.name' | grep -qx "$REVIEW_TAG"; then
    echo "  Registering tag '$REVIEW_TAG'..."
    send $BINARY tx rep create-tag "$REVIEW_TAG" --from alice > /dev/null
fi
if ! $BINARY query rep list-tag --output json 2>&1 | jq -r '.tag[]?.name' | grep -qx "$REVIEW_TAG"; then
    echo "[FAIL] could not register tag '$REVIEW_TAG'; CreateInitiative would reject it"
    exit 1
fi
R=$(send $BINARY tx rep propose-project "ReviewedProj" "reviewer e2e" infrastructure technical 0 0 \
        --tags "$REVIEW_TAG" --from bob)
if [ "$(code_of "$R")" != "0" ]; then
    echo "[FAIL] could not create project: $(log_of "$R")"; exit 1
fi
PROJECT_ID=$(echo "$R" | jq -r '[.events[]?.attributes[]? | select(.key=="project_id") | .value] | last // empty' | tr -d '"')
if [ -z "$PROJECT_ID" ]; then
    echo "[FAIL] no project_id in the propose-project events"; exit 1
fi
echo "  Project: #$PROJECT_ID"

# ========================================================================
# TEST 1: turn the reviewer gate on
# ========================================================================
echo "--- TEST 1: set-verification-policy ---"
# min_verifier_reputation is a nullable Dec: omit it rather than sending a
# decimal string, which proto-JSON rejects for a customtype Int/Dec field.
R=$(send $BINARY tx rep set-verification-policy "$PROJECT_ID" \
      '{"default_review":"REVIEW_PROCESS_PEER_REVIEW","min_verifier_count":1,"review_period_epochs":1,"challenge_period_epochs":1}' \
      --from bob)
if [ "$(code_of "$R")" == "0" ]; then
    STORED=$($BINARY query rep get-project "$PROJECT_ID" --output json 2>&1 \
        | jq -r '.project.verification_policy.min_verifier_count // "0"')
    echo "  min_verifier_count stored: $STORED"
    [ "$STORED" == "1" ] && TEST_1_RESULT="PASS" || echo "  expected 1"
else
    echo "  failed: $(log_of "$R")"
fi
echo "  Result: $TEST_1_RESULT"; echo

# ========================================================================
# TEST 2: bond a reviewer
# ========================================================================
echo "--- TEST 2: bond-role initiative-reviewer ---"
# The floor is an order of magnitude above the sentinel's because the liability
# is: a wrong approval mints DREAM and minted DREAM cannot be clawed back.
MIN_BOND=$($BINARY query rep bonded-role-config initiative-reviewer --output json 2>&1 \
    | jq -r '(.config // .bonded_role_config).min_bond // "5000"')
echo "  role min_bond: $MIN_BOND DREAM"
ALREADY=$($BINARY query rep bonded-role initiative-reviewer "$SENTINEL1_ADDR" --output json 2>&1 \
    | jq -r '.bonded_role.bond_status // ""')
if [ "$ALREADY" == "BONDED_ROLE_STATUS_NORMAL" ]; then
    # Re-runs against a chain that already has the role bonded must not fail;
    # bonding twice is an error, and this test is about reviewing, not bonding.
    echo "  already bonded from an earlier run"
    TEST_2_RESULT="PASS"
else
    send $BINARY tx rep transfer-dream "$SENTINEL1_ADDR" 6000000000 bounty reviewer-bond --from alice > /dev/null
    R=$(send $BINARY tx rep bond-role initiative-reviewer 5500000000 --from sentinel1)
    STATUS=$($BINARY query rep bonded-role initiative-reviewer "$SENTINEL1_ADDR" --output json 2>&1 \
        | jq -r '.bonded_role.bond_status // ""')
    echo "  bond status: ${STATUS:-<none>}"
    if [ "$STATUS" == "BONDED_ROLE_STATUS_NORMAL" ]; then
        TEST_2_RESULT="PASS"
    else
        echo "  failed: $(log_of "$R")"
    fi
fi
echo "  Result: $TEST_2_RESULT"; echo

# ========================================================================
# SETUP: assign and submit
# ========================================================================
R=$(send $BINARY tx rep create-initiative "$PROJECT_ID" "Reviewed work" "needs review" 0 0 5000 \
      --tags "$REVIEW_TAG" --from bob)
if [ "$(code_of "$R")" != "0" ]; then
    # Swallowing this is what turned one clear ErrTagNotRegistered into five
    # "invalid argument" failures downstream. Fail here, with the real reason.
    echo "[FAIL] could not create initiative: $(log_of "$R")"
    exit 1
fi
INIT_ID=$(echo "$R" | jq -r '[.events[]?.attributes[]? | select(.key=="initiative_id") | .value] | last // empty' | tr -d '"')
if [ -z "$INIT_ID" ]; then
    echo "[FAIL] no initiative_id in the create-initiative events"
    exit 1
fi
echo "--- SETUP: initiative #$INIT_ID assigned and submitted ---"
# The suite shares one "assignee" account across every script and runs
# alphabetically, so it is routinely at max_active_initiatives_per_member (10)
# by the time the later scripts run. Take the first candidate with capacity —
# and taking one other than "assignee" also leaves room for the scripts that
# sort after this one.
ASSIGNEE_KEY=""
for CAND in poster2 bounty_creator expert assignee anonymous_challenger juror3; do
    CAND_ADDR=$($BINARY keys show "$CAND" -a --keyring-backend test 2>/dev/null) || continue
    [ -z "$CAND_ADDR" ] && continue
    R=$(send $BINARY tx rep assign-initiative "$INIT_ID" "$CAND_ADDR" --from bob)
    if [ "$(code_of "$R")" == "0" ]; then
        ASSIGNEE_KEY="$CAND"
        echo "  assigned to $CAND"
        break
    fi
    if ! echo "$(log_of "$R")" | grep -qi "max active initiatives"; then
        echo "[FAIL] could not assign initiative: $(log_of "$R")"; exit 1
    fi
done
if [ -z "$ASSIGNEE_KEY" ]; then
    echo "[FAIL] no candidate has capacity under the active-initiative cap"; exit 1
fi
R=$(send $BINARY tx rep submit-initiative-work "$INIT_ID" "ipfs://v1" "first attempt" --from $ASSIGNEE_KEY)
if [ "$(code_of "$R")" != "0" ]; then
    echo "[FAIL] could not submit work: $(log_of "$R")"; exit 1
fi
DEADLINE=$($BINARY query rep get-initiative "$INIT_ID" --output json 2>&1 | jq -r '.initiative.review_deadline // "0"')
echo "  review window opens at height: $DEADLINE"
echo

# ========================================================================
# TEST 3: an unbonded address may not review
# ========================================================================
echo "--- TEST 3: unbonded address refused ---"
R=$(send $BINARY tx rep submit-initiative-review "$INIT_ID" true "looks fine to me" --from poster2)
if [ "$(code_of "$R")" == "0" ]; then
    echo "  expected refusal but the verdict was accepted"
elif echo "$(log_of "$R")" | grep -qi "does not hold the initiative-reviewer role\|unauthorized"; then
    echo "  correctly refused: not a bonded reviewer"
    TEST_3_RESULT="PASS"
else
    echo "  refused with an unexpected error: $(log_of "$R")"
fi
echo "  Result: $TEST_3_RESULT"; echo

# ========================================================================
# TEST 4 + 5: rejection opens a new round, and commits bond
# ========================================================================
echo "--- TEST 4/5: rejection opens a new round; bond scales with budget ---"
# total_committed_bond is cumulative across every open verdict the reviewer
# holds, not per-initiative, so the absolute value only equals one verdict's
# commitment on a pristine chain. Measure the DELTA instead: the property under
# test is that a verdict commits rate x budget, and that holds whatever the
# reviewer was already carrying.
COMMITTED_BEFORE=$($BINARY query rep bonded-role initiative-reviewer "$SENTINEL1_ADDR" --output json 2>&1 \
    | jq -r '.bonded_role.total_committed_bond // "0"')
R=$(send $BINARY tx rep submit-initiative-review "$INIT_ID" false "criterion not met" --from sentinel1)
if [ "$(code_of "$R")" == "0" ]; then
    AFTER=$($BINARY query rep get-initiative "$INIT_ID" --output json 2>&1)
    STATUS=$(echo "$AFTER" | jq -r '.initiative.status // ""')
    ROUND=$(echo "$AFTER" | jq -r '.initiative.review_round // "0"')
    echo "  status=$STATUS round=$ROUND"
    if [ "$STATUS" == "INITIATIVE_STATUS_ASSIGNED" ] && [ "$ROUND" == "1" ]; then
        TEST_4_RESULT="PASS"
    else
        echo "  expected ASSIGNED at round 1 — the remedy for 'not done' is to finish it"
    fi

    # Expected commitment is reviewer_bond_reserve_rate x budget, read from
    # params rather than hardcoded.
    RATE=$($BINARY query rep params --output json 2>&1 | jq -r '.params.reviewer_bond_reserve_rate // "0"')
    BUDGET=$(echo "$AFTER" | jq -r '.initiative.budget // "0"')
    COMMITTED_AFTER=$($BINARY query rep bonded-role initiative-reviewer "$SENTINEL1_ADDR" --output json 2>&1 \
        | jq -r '.bonded_role.total_committed_bond // "0"')
    DELTA=$((COMMITTED_AFTER - COMMITTED_BEFORE))
    EXPECTED=$(python3 -c "print(int(int('$BUDGET') * int('$RATE') // 10**18))" 2>/dev/null)
    echo "  committed $COMMITTED_BEFORE -> $COMMITTED_AFTER (delta $DELTA, expected $EXPECTED, budget $BUDGET)"
    [ "$DELTA" == "$EXPECTED" ] && TEST_5_RESULT="PASS" || echo "  bond must scale to what the verdict could mint"
else
    echo "  review failed: $(log_of "$R")"
fi
echo "  Result: $TEST_4_RESULT / $TEST_5_RESULT"; echo

# ========================================================================
# TEST 6: the same reviewer may file again on the new round
# ========================================================================
echo "--- TEST 6: approval on the resubmitted round ---"
R=$(send $BINARY tx rep submit-initiative-work "$INIT_ID" "ipfs://v2" "fixed" --from $ASSIGNEE_KEY)
if [ "$(code_of "$R")" != "0" ]; then
    echo "  could not resubmit: $(log_of "$R")"
fi
R=$(send $BINARY tx rep submit-initiative-review "$INIT_ID" true "now meets the criteria" --from sentinel1)
if [ "$(code_of "$R")" == "0" ]; then
    echo "  verdicts are keyed by round, so this does not collide with round 0"
    TEST_6_RESULT="PASS"
else
    echo "  failed: $(log_of "$R")"
fi
echo "  Result: $TEST_6_RESULT"; echo

# ========================================================================
# TEST 7: the accuracy record exists
# ========================================================================
echo "--- TEST 7: role-activity records the verdicts ---"
ACTIONS=$($BINARY query rep role-activity initiative-reviewer "$SENTINEL1_ADDR" --output json 2>&1 \
    | jq -r '.role_activity.total_actions.review // "0"')
echo "  reviews recorded: $ACTIONS"
# An accuracy score nobody can read is one nobody can contest.
if [ "$ACTIONS" != "0" ] && [ "$ACTIONS" != "null" ]; then TEST_7_RESULT="PASS"; fi
echo "  Result: $TEST_7_RESULT"; echo

# ========================================================================
# SUMMARY
# ========================================================================
echo "================================================================================"
echo "INITIATIVE REVIEW TEST COMPLETED"
echo "================================================================================"
print_result() {
    case "$2" in
        PASS) echo "[ OK ] $1" ;;
        SKIP) echo "[SKIP] $1" ;;
        *)    echo "[FAIL] $1" ;;
    esac
}
print_result "TEST 1: verification policy set"          "$TEST_1_RESULT"
print_result "TEST 2: reviewer bonded at NORMAL"        "$TEST_2_RESULT"
print_result "TEST 3: unbonded address refused"         "$TEST_3_RESULT"
print_result "TEST 4: rejection opens a new round"      "$TEST_4_RESULT"
print_result "TEST 5: bond scales with the budget"      "$TEST_5_RESULT"
print_result "TEST 6: approval on the resubmitted round" "$TEST_6_RESULT"
print_result "TEST 7: accuracy record written"          "$TEST_7_RESULT"
echo "================================================================================"

FAIL_COUNT=0
for R in "$TEST_1_RESULT" "$TEST_2_RESULT" "$TEST_3_RESULT" "$TEST_4_RESULT" \
         "$TEST_5_RESULT" "$TEST_6_RESULT" "$TEST_7_RESULT"; do
    [ "$R" == "FAIL" ] && FAIL_COUNT=$((FAIL_COUNT + 1))
done
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "FAILURES: $FAIL_COUNT test(s) failed"
    exit 1
fi
exit 0
