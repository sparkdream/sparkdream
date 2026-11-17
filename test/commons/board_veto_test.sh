#!/bin/bash
# Supervisory Board veto through the member councils' veto policies
# (docs/x-commons-spec.md, "Veto seats").
#
# The Commons Supervisory Board's two members are the Technical and Ecosystem
# Council policies, with an absolute threshold of 2. Each council's veto
# policy (0.49, no execution wait) may propose a pure MsgVetoGroupProposals
# to the board and cast its council's seat on it. This test files an accepted
# Commons Governance Committee proposal, has both councils' veto policies
# pass a board veto, and checks the committee proposal ends VETOED.

echo "--- TESTING: SUPERVISORY BOARD VETO VIA COUNCIL VETO SEATS ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
FEES="5000${BOND_DENOM}"
TXFLAGS="-y --chain-id $CHAIN_ID --keyring-backend test --output json"

PASS=0
FAIL=0
pass() { echo "  [ OK ] $1"; PASS=$((PASS + 1)); }
fail() { echo "  [FAIL] $1"; FAIL=$((FAIL + 1)); }

policy_of() { $BINARY query commons get-group "$1" --output json 2>/dev/null | jq -r ".group.$2 // empty"; }
proposal_status() { $BINARY query commons get-proposal "$1" --output json 2>/dev/null | jq -r '.proposal.status // empty'; }

# wait_tx <hash>: the delivered tx JSON.
wait_tx() {
    local i res
    for i in $(seq 1 20); do
        res=$($BINARY query tx "$1" --output json 2>/dev/null)
        if echo "$res" | jq -e '.code' > /dev/null 2>&1; then echo "$res"; return 0; fi
        sleep 1
    done
    echo '{"code":-1,"raw_log":"tx not found"}'
}

# submit <file>: submits as alice, prints the new proposal id.
submit() {
    local res hash
    res=$($BINARY tx commons submit-proposal "$1" --from alice --gas 600000 --fees "$PROPOSAL_FEE" $TXFLAGS 2>&1)
    hash=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
    [ -z "$hash" ] && { echo "submit failed: $res" >&2; return 1; }
    wait_tx "$hash" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | head -1
}

# pass_and_execute <id>: alice and bob vote yes (whoever holds a seat), then
# alice executes. Prints the execution tx JSON.
pass_and_execute() {
    local id=$1 voter res hash
    for voter in alice bob; do
        $BINARY tx commons vote-proposal "$id" yes --from $voter --fees "$FEES" $TXFLAGS > /dev/null 2>&1
        sleep 3
    done
    sleep 2
    res=$($BINARY tx commons execute-proposal "$id" --gas 3000000 --from alice --fees "5000000${BOND_DENOM}" $TXFLAGS 2>&1)
    hash=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
    [ -z "$hash" ] && { echo "{\"code\":-1,\"raw_log\":$(echo "$res" | jq -Rs .)}"; return 1; }
    wait_tx "$hash"
}

# --- 1. Discovery ----------------------------------------------------------
echo "Step 1: policies"
HR_NAME="Commons Governance Committee"
BOARD_NAME="Commons Supervisory Board"
HR_POLICY=$(policy_of "$HR_NAME" policy_address)
BOARD_POLICY=$(policy_of "$BOARD_NAME" policy_address)
TECH_POLICY=$(policy_of "Technical Council" policy_address)
TECH_VETO=$(policy_of "Technical Council" veto_policy_address)
ECO_POLICY=$(policy_of "Ecosystem Council" policy_address)
ECO_VETO=$(policy_of "Ecosystem Council" veto_policy_address)
PROPOSAL_FEE=$($BINARY query commons params --output json 2>/dev/null | jq -r '.params.proposal_fee')
echo "  HR=$HR_POLICY"; echo "  board=$BOARD_POLICY"
echo "  tech veto=$TECH_VETO"; echo "  eco veto=$ECO_VETO"
for v in "$HR_POLICY" "$BOARD_POLICY" "$TECH_POLICY" "$TECH_VETO" "$ECO_POLICY" "$ECO_VETO"; do
    [ -z "$v" ] && { echo "[FAIL] SETUP ERROR: a policy address is missing"; exit 1; }
done

# --- 2. The committee proposal to stop -------------------------------------
echo "Step 2: the committee accepts a proposal"
jq -n --arg p "$HR_POLICY" '{policy_address:$p, messages:[], metadata:"e2e: a committee decision the board will veto"}' > "$PROPOSAL_DIR/board_veto_target.json"
TARGET_ID=$(submit "$PROPOSAL_DIR/board_veto_target.json")
[ -n "$TARGET_ID" ] && pass "committee proposal $TARGET_ID submitted" || { fail "committee proposal not submitted"; exit 1; }
for voter in alice bob; do
    $BINARY tx commons vote-proposal "$TARGET_ID" yes --from $voter --fees "$FEES" $TXFLAGS > /dev/null 2>&1
    sleep 3
done
[ "$(proposal_status $TARGET_ID)" = "PROPOSAL_STATUS_ACCEPTED" ] && pass "committee proposal accepted" || fail "committee proposal is $(proposal_status $TARGET_ID)"

# --- 3. The Technical Council's veto policy proposes the board veto --------
echo "Step 3: the Technical Council's veto policy files the board veto"
jq -n --arg tv "$TECH_VETO" --arg b "$BOARD_POLICY" --arg g "$HR_NAME" '{policy_address:$tv, metadata:"e2e: file a board veto", messages:[
    {"@type":"/sparkdream.commons.v1.MsgSubmitProposal","proposer":$tv,"policy_address":$b,"metadata":"e2e: veto the committee",
     "messages":[{"@type":"/sparkdream.commons.v1.MsgVetoGroupProposals","authority":$b,"group_name":$g}]}]}' > "$PROPOSAL_DIR/board_veto_file.json"
FILE_ID=$(submit "$PROPOSAL_DIR/board_veto_file.json")
EXEC=$(pass_and_execute "$FILE_ID")
BOARD_ID=$(echo "$EXEC" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | head -1)
if [ "$(echo "$EXEC" | jq -r '.code')" = "0" ] && [ -n "$BOARD_ID" ]; then
    pass "board proposal $BOARD_ID filed by the veto policy"
else
    fail "filing the board veto: $(echo "$EXEC" | jq -r '.raw_log' | head -c 300)"
    echo "Board veto: $PASS passed, $FAIL failed"; exit 1
fi

# A veto policy may not file anything but a pure veto.
jq -n --arg tv "$TECH_VETO" --arg b "$BOARD_POLICY" --arg g "$HR_NAME" '{policy_address:$tv, metadata:"e2e: not a veto", messages:[
    {"@type":"/sparkdream.commons.v1.MsgSubmitProposal","proposer":$tv,"policy_address":$b,"metadata":"e2e: delete the committee",
     "messages":[{"@type":"/sparkdream.commons.v1.MsgDeleteGroup","authority":$b,"group_name":$g}]}]}' > "$PROPOSAL_DIR/board_veto_notveto.json"
NOT_ID=$(submit "$PROPOSAL_DIR/board_veto_notveto.json")
EXEC=$(pass_and_execute "$NOT_ID")
[ "$(echo "$EXEC" | jq -r '.code')" != "0" ] && pass "a veto policy cannot file a non-veto board proposal" || fail "a non-veto board proposal was filed by a veto policy"

# --- 4. Both councils' veto policies cast their seats ----------------------
# cast <veto policy> <label>
cast() {
    jq -n --arg v "$1" --arg id "$BOARD_ID" '{policy_address:$v, metadata:"e2e: cast the board seat", messages:[
        {"@type":"/sparkdream.commons.v1.MsgVoteProposal","voter":$v,"proposal_id":$id,"option":"VOTE_OPTION_YES","metadata":"veto"}]}' > "$PROPOSAL_DIR/board_veto_cast.json"
    local id; id=$(submit "$PROPOSAL_DIR/board_veto_cast.json")
    local exec; exec=$(pass_and_execute "$id")
    [ "$(echo "$exec" | jq -r '.code')" = "0" ] && pass "$2 seat cast" || fail "$2 seat: $(echo "$exec" | jq -r '.raw_log' | head -c 300)"
}
echo "Step 4: the two seats"
cast "$TECH_VETO" "Technical Council"
[ "$(proposal_status $BOARD_ID)" = "PROPOSAL_STATUS_SUBMITTED" ] && pass "one seat of two does not pass the veto" || fail "board proposal is $(proposal_status $BOARD_ID) after one seat"
VOTERS=$($BINARY query commons get-proposal "$BOARD_ID" --output json 2>/dev/null | jq -r '[.votes[]?.voter] | join(",")')
[ "$VOTERS" = "$TECH_POLICY" ] && pass "the vote is recorded for the council's seat" || fail "votes recorded for: $VOTERS"
cast "$ECO_VETO" "Ecosystem Council"
[ "$(proposal_status $BOARD_ID)" = "PROPOSAL_STATUS_ACCEPTED" ] && pass "both seats pass the veto" || fail "board proposal is $(proposal_status $BOARD_ID) after two seats"

# --- 5. The veto executes and kills the committee proposal ------------------
echo "Step 5: the veto executes"
sleep 3
RES=$($BINARY tx commons execute-proposal "$BOARD_ID" --gas 2000000 --from alice --fees "$FEES" $TXFLAGS 2>&1)
wait_tx "$(echo "$RES" | jq -r '.txhash // empty')" > /dev/null
[ "$(proposal_status $BOARD_ID)" = "PROPOSAL_STATUS_EXECUTED" ] && pass "board veto executed" || fail "board proposal is $(proposal_status $BOARD_ID)"

RES=$($BINARY tx commons execute-proposal "$TARGET_ID" --gas 2000000 --from alice --fees "$FEES" $TXFLAGS 2>&1)
wait_tx "$(echo "$RES" | jq -r '.txhash // empty')" > /dev/null
[ "$(proposal_status $TARGET_ID)" = "PROPOSAL_STATUS_VETOED" ] && pass "the committee proposal is VETOED" || fail "the committee proposal is $(proposal_status $TARGET_ID)"

echo ""
echo "Board veto: $PASS passed, $FAIL failed"
[ $FAIL -eq 0 ] && exit 0 || exit 1
