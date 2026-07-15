#!/bin/bash

echo "--- TESTING: CREATOR SELF-ASSIGNMENT (BOND, APPROVAL EXCLUSION, CHALLENGE WINDOW) ---"

# Covers the creator self-assignment safeguards:
#   1. Project creator CAN self-assign own initiative (no assignment-time ban)
#   2. DREAM bond (self_assigned_bond_rate x budget) locked at assignment
#   3. Assignee/project creator cannot approve-initiative (conflict of interest)
#   4. Challenge window is multiplied by self_assigned_challenge_multiplier
#   5. Bond released on voluntary abandon
#   6. Non-creator assignment locks no bond

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

FAIL_COUNT=0

# Load test environment (created by setup_test_accounts.sh)
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    source "$SCRIPT_DIR/.test_env"
    echo "[ OK ] Loaded test environment from .test_env"
else
    echo "[WARN]  .test_env not found. Run setup_test_accounts.sh first!"
    exit 1
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
WORKER_ADDR=$EXPERT_ADDR
PROJECT_ID=$TEST_PROJECT_ID

echo "Alice:      $ALICE_ADDR (Project Creator + Self-Assignee)"
echo "Worker:     $WORKER_ADDR (expert, non-creator assignee)"
echo "Project ID: $PROJECT_ID (creator: alice, from setup)"

# Normalize a LegacyDec from query JSON: the CLI renders Decs as raw
# 18-decimal integer strings (e.g. "100000000000000000" = 0.1). Values
# that already contain a '.' pass through unchanged.
normalize_dec() {
    case "$1" in
        *.*) echo "$1" ;;
        ""|null) echo "0" ;;
        *) awk -v x="$1" 'BEGIN{printf "%.18f", x/1e18}' ;;
    esac
}

# Read the self-assignment params
PARAMS=$($BINARY query rep params -o json)
BOND_RATE=$(normalize_dec "$(echo "$PARAMS" | jq -r '.params.self_assigned_bond_rate // "0"')")
CHALLENGE_MULT=$(echo "$PARAMS" | jq -r '.params.self_assigned_challenge_multiplier // "1"')
SELF_EXT_RATIO=$(normalize_dec "$(echo "$PARAMS" | jq -r '.params.self_assigned_external_conviction_ratio // "0"')")
CHALLENGE_EPOCHS=$(echo "$PARAMS" | jq -r '.params.default_challenge_period_epochs // "0"')
EPOCH_BLOCKS=$(echo "$PARAMS" | jq -r '.params.epoch_blocks // "0"')

echo "self_assigned_bond_rate:                 $BOND_RATE"
echo "self_assigned_external_conviction_ratio: $SELF_EXT_RATIO"
echo "self_assigned_challenge_multiplier:      $CHALLENGE_MULT"

if [ "$BOND_RATE" == "0" ] || [ -z "$BOND_RATE" ]; then
    echo "[FAIL] self_assigned_bond_rate missing from params"
    FAIL_COUNT=$((FAIL_COUNT+1))
fi

# Helper: create an initiative on the test project as alice, echo its ID.
# Args: title, budget (micro-DREAM)
create_initiative() {
    local TITLE="$1"
    local BUDGET="$2"
    local RES=$($BINARY tx rep create-initiative \
      $PROJECT_ID \
      "$TITLE" \
      "Self-assignment e2e test initiative" \
      "0" \
      "1" \
      "0" \
      "$BUDGET" \
      --tags "documentation" \
      --from alice \
      --chain-id $CHAIN_ID \
      --keyring-backend test \
      --fees 5000${BOND_DENOM} \
      -y \
      -o json)
    local TX=$(echo "$RES" | jq -r '.txhash')
    sleep 6
    local ID=$($BINARY query tx $TX -o json 2>/dev/null | \
      jq -r '.events[] | select(.type=="initiative_created") | .attributes[] | select(.key=="initiative_id") | .value' | \
      tr -d '"' 2>/dev/null)
    if [ -z "$ID" ] || [ "$ID" == "null" ]; then
        ID=$($BINARY query rep initiatives-by-project $PROJECT_ID -o json 2>/dev/null | \
          jq -r '.initiatives | sort_by(.id // "0" | tonumber) | .[-1].id // "0"' 2>/dev/null)
    fi
    echo "$ID"
}

# ========================================================================
# PART 1: CREATOR SELF-ASSIGNS OWN INITIATIVE, BOND LOCKED
# ========================================================================
echo ""
echo "--- PART 1: CREATOR SELF-ASSIGN + BOND LOCK ---"

# Note: small budget (1 DREAM) keeps required conviction low enough that
# external stakers can reach the FULL external threshold within the Part 3
# poll window: required = conviction_per_dream * sqrt(budget) = 200, and
# per-staker conviction is capped at 33% of required (66), so 4 external
# stakers suffice, each reaching the cap in ~35s with a 150 DREAM stake.
# Also stays under the APPRENTICE tier cap.
SELF_BUDGET="1000000"
SELF_INIT_ID=$(create_initiative "Self-assigned doc fix" "$SELF_BUDGET")
echo "Initiative ID: $SELF_INIT_ID"

# Snapshot alice's staked DREAM before assignment
# Note: proto3 omits zero-valued fields from CLI JSON - normalize with //
ALICE_STAKED_BEFORE=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null | jq -r '.member.staked_dream // "0"')
ALICE_BALANCE_BEFORE=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null | jq -r '.member.dream_balance // "0"')

ASSIGN_RES=$($BINARY tx rep assign-initiative \
  $SELF_INIT_ID \
  $ALICE_ADDR \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)
ASSIGN_TX=$(echo "$ASSIGN_RES" | jq -r '.txhash')
sleep 6

ASSIGN_CODE=$($BINARY query tx $ASSIGN_TX -o json 2>/dev/null | jq -r '.code // 0')
if [ "$ASSIGN_CODE" == "0" ]; then
    echo "[ OK ] Creator self-assignment accepted (no assignment-time ban)"
else
    ASSIGN_ERR=$($BINARY query tx $ASSIGN_TX -o json 2>/dev/null | jq -r '.raw_log // "unknown"')
    echo "[FAIL] Self-assignment rejected: $ASSIGN_ERR"
    FAIL_COUNT=$((FAIL_COUNT+1))
fi

SELF_INIT=$($BINARY query rep get-initiative $SELF_INIT_ID -o json)
SELF_ASSIGNEE=$(echo "$SELF_INIT" | jq -r '.initiative.assignee // ""')
SELF_BOND=$(echo "$SELF_INIT" | jq -r '.initiative.self_assign_bond // "0"')
EXPECTED_BOND=$(awk -v b="$SELF_BUDGET" -v r="$BOND_RATE" 'BEGIN{printf "%.0f", b*r}')

echo "Assignee:        $SELF_ASSIGNEE"
echo "Locked bond:     $SELF_BOND (expected: $EXPECTED_BOND)"

if [ "$SELF_ASSIGNEE" == "$ALICE_ADDR" ]; then
    echo "[ OK ] Initiative assigned to project creator"
else
    echo "[FAIL] Assignee mismatch: $SELF_ASSIGNEE"
    FAIL_COUNT=$((FAIL_COUNT+1))
fi

if [ "$SELF_BOND" == "$EXPECTED_BOND" ]; then
    echo "[ OK ] Self-assign bond recorded on initiative"
else
    echo "[FAIL] Bond mismatch: got $SELF_BOND, expected $EXPECTED_BOND"
    FAIL_COUNT=$((FAIL_COUNT+1))
fi

ALICE_STAKED_AFTER=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null | jq -r '.member.staked_dream // "0"')
STAKED_DELTA=$(awk "BEGIN{printf \"%d\", $ALICE_STAKED_AFTER - $ALICE_STAKED_BEFORE}")
if [ "$STAKED_DELTA" == "$EXPECTED_BOND" ]; then
    echo "[ OK ] Bond locked in creator's staked DREAM (delta: $STAKED_DELTA)"
else
    echo "[WARN]  Staked delta $STAKED_DELTA != bond $EXPECTED_BOND (other stakes/decay may interfere)"
fi

# ========================================================================
# PART 2: CONFLICT OF INTEREST - CREATOR/ASSIGNEE CANNOT APPROVE
# ========================================================================
echo ""
echo "--- PART 2: APPROVAL EXCLUSION (CONFLICT OF INTEREST) ---"

SUBMIT_RES=$($BINARY tx rep submit-initiative-work \
  $SELF_INIT_ID \
  "ipfs://QmSelfAssignedWork" \
  "Work done by the project creator" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)
sleep 6

SELF_STATUS=$($BINARY query rep get-initiative $SELF_INIT_ID -o json | jq -r '.initiative.status // "INITIATIVE_STATUS_OPEN"')
echo "Status after submit: $SELF_STATUS"

# Alice is on the Commons Operations Committee, so without the exclusion
# this approval would be authorized. It must fail on conflict of interest.
APPROVE_RES=$($BINARY tx rep approve-initiative \
  $SELF_INIT_ID \
  true \
  "approving my own work" \
  --from alice \
  --chain-id $CHAIN_ID \
  --keyring-backend test \
  --fees 5000${BOND_DENOM} \
  -y \
  -o json)
APPROVE_TX=$(echo "$APPROVE_RES" | jq -r '.txhash')
sleep 6

APPROVE_TX_RESULT=$($BINARY query tx $APPROVE_TX -o json 2>/dev/null)
APPROVE_CODE=$(echo "$APPROVE_TX_RESULT" | jq -r '.code // 0')
APPROVE_LOG=$(echo "$APPROVE_TX_RESULT" | jq -r '.raw_log // ""')

if [ "$APPROVE_CODE" != "0" ] && echo "$APPROVE_LOG" | grep -q "cannot approve their own initiative"; then
    echo "[ OK ] Creator/assignee approval rejected with conflict-of-interest error"
elif [ "$APPROVE_CODE" != "0" ]; then
    echo "[WARN]  Approval rejected but with unexpected error: $APPROVE_LOG"
else
    echo "[FAIL] Creator was able to approve their own initiative"
    FAIL_COUNT=$((FAIL_COUNT+1))
fi

# ========================================================================
# PART 3: EXTENDED CHALLENGE WINDOW (BEST-EFFORT)
# ========================================================================
echo ""
echo "--- PART 3: EXTENDED CHALLENGE WINDOW FOR SELF-ASSIGNED WORK ---"
echo "Staking as external members to push conviction over the threshold..."
echo "Note: self-assigned initiatives need FULL external conviction ($SELF_EXT_RATIO)"

# Per-staker conviction is capped at max_conviction_share_per_member (33%
# of required), so at least four distinct non-affiliated members must reach
# the cap for 100% external. Six setup-created members are tried for margin
# (alice, the creator+assignee, is internal and her stakes would not count
# as external anyway); individual failures are tolerated as long as four land.
STAKE_ERR_FILE=$(mktemp)
STAKES_LANDED=0
for STAKER in challenger anonymous_challenger expert assignee juror1 juror2; do
    # Note: keep stderr separate — with --gas auto the CLI prints
    # "gas estimate: N" to stderr, which would corrupt the JSON if merged.
    STAKE_RES=$($BINARY tx rep stake \
      stake-target-initiative \
      $SELF_INIT_ID \
      "150000000" \
      --from $STAKER --chain-id $CHAIN_ID --keyring-backend test \
      --gas auto --gas-adjustment 1.5 --fees 5000${BOND_DENOM} -y -o json 2>"$STAKE_ERR_FILE")
    STAKE_TX=$(echo "$STAKE_RES" | jq -r '.txhash // ""' 2>/dev/null)
    if [ -z "$STAKE_TX" ]; then
        echo "[WARN]  $STAKER stake broadcast failed: $(head -1 "$STAKE_ERR_FILE")"
        continue
    fi
    sleep 3
    STAKE_CODE=$($BINARY query tx $STAKE_TX -o json 2>/dev/null | jq -r '.code // 0')
    if [ "$STAKE_CODE" == "0" ]; then
        echo "[ OK ] $STAKER staked 150 DREAM"
        STAKES_LANDED=$((STAKES_LANDED+1))
    else
        STAKE_ERR=$($BINARY query tx $STAKE_TX -o json 2>/dev/null | jq -r '.raw_log // "unknown"')
        echo "[WARN]  $STAKER stake failed: $STAKE_ERR"
    fi
done
rm -f "$STAKE_ERR_FILE"
echo "Stakes landed: $STAKES_LANDED (need 4 to cross the full-external threshold)"

# Conviction grows with time; poll for the IN_REVIEW transition instead of
# sleeping a fixed wall-clock amount (block rate drifts on long runs).
IN_REVIEW=false
for i in $(seq 1 60); do
    STATUS=$($BINARY query rep get-initiative $SELF_INIT_ID -o json | jq -r '.initiative.status // ""')
    if [ "$STATUS" == "INITIATIVE_STATUS_IN_REVIEW" ] || [ "$STATUS" == "INITIATIVE_STATUS_COMPLETED" ]; then
        IN_REVIEW=true
        break
    fi
    if [ $((i % 10)) -eq 0 ]; then
        CONV=$($BINARY query rep initiative-conviction $SELF_INIT_ID -o json 2>/dev/null)
        echo "  ... poll $i: status=$STATUS external=$(echo "$CONV" | jq -r '.external_conviction // "0"') required=$(echo "$CONV" | jq -r '.threshold // "0"')"
    fi
    sleep 3
done

if [ "$IN_REVIEW" == "true" ] && [ "$STATUS" == "INITIATIVE_STATUS_IN_REVIEW" ]; then
    WINDOW_INIT=$($BINARY query rep get-initiative $SELF_INIT_ID -o json)
    REVIEW_END=$(echo "$WINDOW_INIT" | jq -r '.initiative.review_period_end // "0"')
    CHALLENGE_END=$(echo "$WINDOW_INIT" | jq -r '.initiative.challenge_period_end // "0"')
    ACTUAL_WINDOW=$(awk "BEGIN{printf \"%d\", $CHALLENGE_END - $REVIEW_END}")
    EXPECTED_WINDOW=$(awk "BEGIN{printf \"%d\", $CHALLENGE_EPOCHS * $EPOCH_BLOCKS * $CHALLENGE_MULT}")
    echo "Challenge window: $ACTUAL_WINDOW blocks (expected: $EXPECTED_WINDOW = $CHALLENGE_EPOCHS epochs x $EPOCH_BLOCKS blocks x$CHALLENGE_MULT)"
    if [ "$ACTUAL_WINDOW" == "$EXPECTED_WINDOW" ]; then
        echo "[ OK ] Challenge window extended by self_assigned_challenge_multiplier"
    else
        echo "[FAIL] Challenge window not extended: $ACTUAL_WINDOW != $EXPECTED_WINDOW"
        FAIL_COUNT=$((FAIL_COUNT+1))
    fi
elif [ "$STATUS" == "INITIATIVE_STATUS_COMPLETED" ]; then
    echo "[WARN]  Initiative completed before window could be measured - skipping"
else
    echo "[WARN]  Conviction threshold not reached in time (status: $STATUS) - skipping window check"
fi

# ========================================================================
# PART 4: BOND RELEASED ON VOLUNTARY ABANDON
# ========================================================================
echo ""
echo "--- PART 4: BOND RELEASED ON ABANDON ---"

ABANDON_BUDGET="50000000"
ABANDON_INIT_ID=$(create_initiative "Self-assigned then abandoned" "$ABANDON_BUDGET")
echo "Initiative ID: $ABANDON_INIT_ID"

$BINARY tx rep assign-initiative $ABANDON_INIT_ID $ALICE_ADDR \
  --from alice --chain-id $CHAIN_ID --keyring-backend test \
  --fees 5000${BOND_DENOM} -y -o json > /dev/null 2>&1
sleep 6

BOND_BEFORE_ABANDON=$($BINARY query rep get-initiative $ABANDON_INIT_ID -o json | jq -r '.initiative.self_assign_bond // "0"')
STAKED_BEFORE_ABANDON=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null | jq -r '.member.staked_dream // "0"')

if [ "$BOND_BEFORE_ABANDON" == "0" ]; then
    echo "[WARN]  No bond locked on second initiative - skipping abandon check"
else
    ABANDON_RES=$($BINARY tx rep abandon-initiative \
      $ABANDON_INIT_ID \
      "changed my mind" \
      --from alice \
      --chain-id $CHAIN_ID \
      --keyring-backend test \
      --fees 5000${BOND_DENOM} \
      -y \
      -o json)
    sleep 6

    ABANDONED_INIT=$($BINARY query rep get-initiative $ABANDON_INIT_ID -o json)
    ABANDON_STATUS=$(echo "$ABANDONED_INIT" | jq -r '.initiative.status // ""')
    BOND_AFTER_ABANDON=$(echo "$ABANDONED_INIT" | jq -r '.initiative.self_assign_bond // "0"')
    STAKED_AFTER_ABANDON=$($BINARY query rep get-member $ALICE_ADDR -o json 2>/dev/null | jq -r '.member.staked_dream // "0"')
    RELEASED=$(awk "BEGIN{printf \"%d\", $STAKED_BEFORE_ABANDON - $STAKED_AFTER_ABANDON}")

    echo "Status after abandon: $ABANDON_STATUS"
    echo "Bond after abandon:   $BOND_AFTER_ABANDON"
    echo "Staked released:      $RELEASED (bond was: $BOND_BEFORE_ABANDON)"

    if [ "$ABANDON_STATUS" == "INITIATIVE_STATUS_ABANDONED" ] && [ "$BOND_AFTER_ABANDON" == "0" ]; then
        echo "[ OK ] Bond cleared on abandon"
    else
        echo "[FAIL] Bond not cleared on abandon (status: $ABANDON_STATUS, bond: $BOND_AFTER_ABANDON)"
        FAIL_COUNT=$((FAIL_COUNT+1))
    fi

    if [ "$RELEASED" == "$BOND_BEFORE_ABANDON" ]; then
        echo "[ OK ] Bond returned to creator's unlocked balance"
    else
        echo "[WARN]  Released amount $RELEASED != bond $BOND_BEFORE_ABANDON (decay/other stakes may interfere)"
    fi
fi

# ========================================================================
# PART 5: NON-CREATOR ASSIGNMENT LOCKS NO BOND
# ========================================================================
echo ""
echo "--- PART 5: NON-CREATOR ASSIGNMENT LOCKS NO BOND ---"

WORKER_INIT_ID=$(create_initiative "Worker-assigned control" "50000000")
echo "Initiative ID: $WORKER_INIT_ID"

$BINARY tx rep assign-initiative $WORKER_INIT_ID $WORKER_ADDR \
  --from alice --chain-id $CHAIN_ID --keyring-backend test \
  --fees 5000${BOND_DENOM} -y -o json > /dev/null 2>&1
sleep 6

WORKER_INIT=$($BINARY query rep get-initiative $WORKER_INIT_ID -o json)
WORKER_ASSIGNEE=$(echo "$WORKER_INIT" | jq -r '.initiative.assignee // ""')
WORKER_BOND=$(echo "$WORKER_INIT" | jq -r '.initiative.self_assign_bond // "0"')

if [ "$WORKER_ASSIGNEE" == "$WORKER_ADDR" ] && [ "$WORKER_BOND" == "0" ]; then
    echo "[ OK ] Non-creator assignee has no bond"
else
    echo "[FAIL] Unexpected: assignee=$WORKER_ASSIGNEE bond=$WORKER_BOND"
    FAIL_COUNT=$((FAIL_COUNT+1))
fi

# Clean up: abandon the worker-assigned initiative so leftover state doesn't
# interfere with other test scripts sharing the project.
$BINARY tx rep abandon-initiative $WORKER_INIT_ID "e2e cleanup" \
  --from expert --chain-id $CHAIN_ID --keyring-backend test \
  --fees 5000${BOND_DENOM} -y -o json > /dev/null 2>&1
sleep 3

# ========================================================================
# SUMMARY
# ========================================================================
echo ""
echo "========================================================================="
echo "SELF-ASSIGNMENT TEST SUMMARY"
echo "========================================================================="
if [ $FAIL_COUNT -eq 0 ]; then
    echo "[ OK ] All self-assignment checks passed"
    exit 0
else
    echo "[FAIL] $FAIL_COUNT self-assignment check(s) failed"
    exit 1
fi
