#!/bin/bash
# Treasury staking (docs/x-commons-spec.md, "Treasury staking").
#
# The Commons Operations Committee delegates treasury SPARK to a validator
# through the x/commons wrapper messages. The test checks:
#   - a delegation to a validator operated by a committee member is refused
#     (alice runs the chain's validator and sits on the committee);
#   - a delegation to an outside validator works, and shows in x/staking and
#     in `query commons treasury-stake`;
#   - a delegation above the treasury stake cap is refused;
#   - the raw staking message cannot be proposed;
#   - anyone can pay the treasury's staking rewards into the treasury;
#   - the committee can undelegate.
#
# The outside validator is created here with a 1 SPARK self-bond and a
# consensus key nobody runs. Its power is far too small to matter to
# consensus; the chain jails it for downtime after a while, which the
# delegation survives. A treasury may only delegate to a bonded validator,
# so the delegation in step 3 is made right after the validator is created;
# the later steps do not need it bonded (the cap is checked before the
# validator, and a redelegation is refused for the member rule either way).

echo "--- TESTING: TREASURY STAKING (COMMONS OPERATIONS COMMITTEE) ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"
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
finish() {
    echo ""
    echo "Treasury staking: $PASS passed, $FAIL failed"
    [ $FAIL -eq 0 ] && exit 0 || exit 1
}

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

# send <tx args...>: broadcasts and prints the delivered tx JSON.
send() {
    local res hash
    res=$($BINARY tx "$@" $TXFLAGS 2>&1)
    hash=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
    [ -z "$hash" ] && { echo "{\"code\":-1,\"raw_log\":$(echo "$res" | jq -Rs .)}"; return 1; }
    wait_tx "$hash"
}

# submit <file>: submits as alice, prints the delivered tx JSON.
submit() {
    send commons submit-proposal "$1" --from alice --gas 600000 --fees "$PROPOSAL_FEE"
}
proposal_id() {
    echo "$1" | jq -r '.events[]? | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | head -1
}

# pass_and_execute <id>: alice and bob vote yes, alice executes. Prints the
# execution tx JSON.
pass_and_execute() {
    local id=$1 voter
    for voter in alice bob; do
        $BINARY tx commons vote-proposal "$id" yes --from $voter --fees "$FEES" $TXFLAGS > /dev/null 2>&1
        sleep 3
    done
    sleep 2
    send commons execute-proposal "$id" --gas 3000000 --from alice --fees "5000000${BOND_DENOM}"
}

# run_proposal <label> <messages json array>: submit, pass, execute. Prints
# the execution tx JSON.
run_proposal() {
    local file="$PROPOSAL_DIR/treasury_staking_$1.json" sub id
    jq -n --arg p "$POLICY" --arg m "e2e treasury staking: $1" --argjson msgs "$2" \
        '{policy_address:$p, messages:$msgs, metadata:$m}' > "$file"
    sub=$(submit "$file")
    id=$(proposal_id "$sub")
    [ -z "$id" ] && { echo "$sub"; return 1; }
    pass_and_execute "$id"
}

stake_msg() { # <type> <validator> <amount>
    jq -n --arg t "/sparkdream.commons.v1.$1" --arg p "$POLICY" --arg v "$2" --arg a "$3" --arg d "$BOND_DENOM" \
        '[{"@type":$t, authority:$p, validator_address:$v, amount:{denom:$d, amount:$a}}]'
}
treasury_stake() { $BINARY query commons treasury-stake --output json 2>/dev/null | jq -r ".$1 // \"0\""; }
delegated_to() { # <validator>: the policy's delegation balance, 0 if none
    $BINARY query staking delegation "$POLICY" "$1" --output json 2>/dev/null | jq -r '.delegation_response.balance.amount // "0"'
}

# --- 1. Discovery and funding ------------------------------------------------
echo "Step 1: committee, validators, treasury"
GROUP_NAME="Commons Operations Committee"
POLICY=$($BINARY query commons get-group "$GROUP_NAME" --output json 2>/dev/null | jq -r '.group.policy_address // empty')
[ -z "$POLICY" ] && { echo "[FAIL] SETUP ERROR: '$GROUP_NAME' not found"; exit 1; }
PROPOSAL_FEE=$($BINARY query commons params --output json 2>/dev/null | jq -r '.params.proposal_fee')
ALICE=$($BINARY keys show alice -a --keyring-backend test)
ALICE_VAL=$($BINARY keys show alice --bech val -a --keyring-backend test)
CAROL=$($BINARY keys show carol -a --keyring-backend test)
echo "  policy=$POLICY"; echo "  member validator=$ALICE_VAL"

send bank send "$ALICE" "$POLICY" "50000000${BOND_DENOM}" --from alice --fees "$FEES" > /dev/null

CAP_BPS=$(treasury_stake cap_bps)
HEADROOM=$(treasury_stake headroom)
[ "$CAP_BPS" = "2000" ] && pass "treasury stake cap is 2000 bps" || fail "cap_bps is $CAP_BPS"
[ "$HEADROOM" != "0" ] && pass "headroom under the cap: $HEADROOM" || { fail "no headroom under the cap"; finish; }

# --- 2. A member's validator is refused --------------------------------------
echo "Step 2: delegation to a member's validator"
EXEC=$(run_proposal member_validator "$(stake_msg MsgDelegateTreasury "$ALICE_VAL" 2000000)")
if [ "$(echo "$EXEC" | jq -r '.code')" != "0" ] && echo "$EXEC" | jq -r '.raw_log' | grep -q "operated by"; then
    pass "delegation to a validator operated by a committee member is refused"
else
    fail "member validator: code=$(echo "$EXEC" | jq -r '.code') $(echo "$EXEC" | jq -r '.raw_log' | head -c 300)"
fi

# --- 3. An outside validator --------------------------------------------------
echo "Step 3: an outside validator"
$BINARY keys show treasuryval -a --keyring-backend test > /dev/null 2>&1 || \
    $BINARY keys add treasuryval --keyring-backend test > /dev/null 2>&1
OUT_ADDR=$($BINARY keys show treasuryval -a --keyring-backend test)
OUT_VAL=$($BINARY keys show treasuryval --bech val -a --keyring-backend test)
if [ -z "$($BINARY query staking validator "$OUT_VAL" --output json 2>/dev/null | jq -r '.validator.operator_address // empty')" ]; then
    send bank send "$ALICE" "$OUT_ADDR" "5000000${BOND_DENOM}" --from alice --fees "$FEES" > /dev/null
    jq -n --arg k "$(openssl rand -base64 32)" --arg a "1000000${BOND_DENOM}" '{
        pubkey: {"@type":"/cosmos.crypto.ed25519.PubKey", key:$k}, amount:$a, moniker:"e2e-treasury-outside",
        "commission-rate":"0.1", "commission-max-rate":"0.2", "commission-max-change-rate":"0.01", "min-self-delegation":"1"}' \
        > "$PROPOSAL_DIR/treasury_staking_validator.json"
    RES=$(send staking create-validator "$PROPOSAL_DIR/treasury_staking_validator.json" --from treasuryval --gas 400000 --fees "$FEES")
    [ "$(echo "$RES" | jq -r '.code')" = "0" ] || { fail "create-validator: $(echo "$RES" | jq -r '.raw_log' | head -c 300)"; finish; }
fi
pass "outside validator $OUT_VAL"

EXEC=$(run_proposal delegate "$(stake_msg MsgDelegateTreasury "$OUT_VAL" 2000000)")
[ "$(echo "$EXEC" | jq -r '.code')" = "0" ] && pass "treasury delegation executed" || { fail "delegate: $(echo "$EXEC" | jq -r '.raw_log' | head -c 300)"; finish; }
echo "$EXEC" | jq -e '.events[] | select(.type=="treasury_delegated")' > /dev/null && pass "treasury_delegated event" || fail "no treasury_delegated event"
# A downtime slash may have shaved the balance; it never grows past 2 SPARK.
DEL=$(delegated_to "$OUT_VAL")
[ "$DEL" -gt 1900000 ] && [ "$DEL" -le 2000000 ] && pass "x/staking holds the delegation ($DEL)" || fail "delegation balance is $DEL"
STAKED=$(treasury_stake treasury_staked)
[ "$STAKED" -ge "$DEL" ] && pass "treasury-stake reports $STAKED staked" || fail "treasury_staked is $STAKED, delegation $DEL"

# --- 4. The cap ---------------------------------------------------------------
echo "Step 4: the treasury stake cap"
OVER=$(echo "$(treasury_stake headroom) + 1" | bc)
EXEC=$(run_proposal over_cap "$(stake_msg MsgDelegateTreasury "$OUT_VAL" "$OVER")")
if [ "$(echo "$EXEC" | jq -r '.code')" != "0" ] && echo "$EXEC" | jq -r '.raw_log' | grep -q "treasury stake cap"; then
    pass "a delegation above the cap is refused"
else
    fail "over cap: code=$(echo "$EXEC" | jq -r '.code') $(echo "$EXEC" | jq -r '.raw_log' | head -c 300)"
fi

# --- 5. The raw staking message ----------------------------------------------
echo "Step 5: the raw staking message"
jq -n --arg p "$POLICY" --arg v "$OUT_VAL" --arg d "$BOND_DENOM" '{policy_address:$p, metadata:"e2e: raw MsgDelegate", messages:[
    {"@type":"/cosmos.staking.v1beta1.MsgDelegate", delegator_address:$p, validator_address:$v, amount:{denom:$d, amount:"1000000"}}]}' \
    > "$PROPOSAL_DIR/treasury_staking_raw.json"
SUB=$(submit "$PROPOSAL_DIR/treasury_staking_raw.json")
[ "$(echo "$SUB" | jq -r '.code')" != "0" ] && pass "a proposal carrying the SDK MsgDelegate is refused" || fail "the SDK MsgDelegate was accepted in a proposal"

# --- 6. Rewards, by anyone ----------------------------------------------------
echo "Step 6: rewards"
RES=$(send commons withdraw-treasury-rewards "$POLICY" "$OUT_VAL" --from carol --gas 400000 --fees "$FEES")
if [ "$(echo "$RES" | jq -r '.code')" = "0" ] && echo "$RES" | jq -e '.events[] | select(.type=="treasury_rewards_withdrawn")' > /dev/null; then
    pass "a non-member paid the treasury's rewards into the treasury"
else
    fail "withdraw rewards: $(echo "$RES" | jq -r '.raw_log' | head -c 300)"
fi
RES=$(send commons withdraw-treasury-rewards "$CAROL" "$OUT_VAL" --from carol --gas 400000 --fees "$FEES")
[ "$(echo "$RES" | jq -r '.code')" != "0" ] && pass "rewards of a non-treasury account cannot be withdrawn this way" || fail "withdraw for a non-treasury account succeeded"

# --- 7. Redelegation to a member's validator, undelegation --------------------
echo "Step 7: redelegate and undelegate"
REDEL=$(jq -n --arg p "$POLICY" --arg s "$OUT_VAL" --arg t "$ALICE_VAL" --arg d "$BOND_DENOM" \
    '[{"@type":"/sparkdream.commons.v1.MsgRedelegateTreasury", authority:$p, src_validator_address:$s, dst_validator_address:$t, amount:{denom:$d, amount:"500000"}}]')
EXEC=$(run_proposal redelegate_member "$REDEL")
[ "$(echo "$EXEC" | jq -r '.code')" != "0" ] && pass "redelegation to a member's validator is refused" || fail "redelegation to a member's validator executed"

EXEC=$(run_proposal undelegate "$(stake_msg MsgUndelegateTreasury "$OUT_VAL" 1000000)")
[ "$(echo "$EXEC" | jq -r '.code')" = "0" ] && pass "undelegation executed" || fail "undelegate: $(echo "$EXEC" | jq -r '.raw_log' | head -c 300)"
UNBONDING=$($BINARY query staking unbonding-delegation "$POLICY" "$OUT_VAL" --output json 2>/dev/null | jq -r '[.unbond.entries[]?.balance | tonumber] | add // 0')
[ "$UNBONDING" -gt 900000 ] && pass "x/staking holds the unbonding entry ($UNBONDING)" || fail "unbonding balance is $UNBONDING"
AFTER=$(delegated_to "$OUT_VAL")
[ "$AFTER" -lt "$DEL" ] && pass "the delegation shrank to $AFTER" || fail "delegation is still $AFTER"

finish
