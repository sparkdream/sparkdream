#!/bin/bash
# ============================================================================
# X/VAULT E2E: create, signer consent (accept / leave), propose, approve,
# execute, rotate, expire, prune, personhood refusal, spending allowance from
# the vault, session keys refused.
# ============================================================================
echo "--- TESTING: X/VAULT ---"
echo ""

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="${BINARY:-sparkdreamd}"
CHAIN_ID="${CHAIN_ID:-sparkdream}"
WORK="${TMPDIR:-/tmp}/sparkdream-vault-e2e-$$"
mkdir -p "$WORK"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    TEST_NAMES+=("$1"); RESULTS+=("$2")
    if [ "$2" == "PASS" ]; then PASS_COUNT=$((PASS_COUNT + 1)); else FAIL_COUNT=$((FAIL_COUNT + 1)); fi
    echo "  => $2"; echo ""
}

wait_for_tx() {
    local TXHASH=$1 ATTEMPT=0
    while [ $ATTEMPT -lt 30 ]; do
        RESULT=$($BINARY q tx "$TXHASH" --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    echo '{"code":98,"raw_log":"tx not found"}'; return 1
}

# tx <key> <args...> -> TX_RESULT (delivered result)
run_tx() {
    local KEY=$1; shift
    local TX_RES TXHASH
    TX_RES=$($BINARY tx "$@" --from "$KEY" --chain-id $CHAIN_ID --keyring-backend test \
        --gas 400000 --fees 50000${BOND_DENOM} -y --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$TXHASH" ]; then TX_RESULT=$(jq -n --arg log "$TX_RES" '{code:99, raw_log:$log}'); return 0; fi
    if [ "$(echo "$TX_RES" | jq -r '.code // "0"')" != "0" ]; then TX_RESULT="$TX_RES"; return 0; fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
}
tx_ok()  { [ "$(echo "$1" | jq -r '.code')" = "0" ]; }
tx_log() { echo "$1" | jq -r '.raw_log // "unknown"' 2>/dev/null | cut -c1-220; }
event_attr() { echo "$1" | jq -r --arg t "$2" --arg k "$3" '[.events[] | select(.type==$t) | .attributes[] | select(.key==$k) | .value][0] // empty'; }

ensure_key() {
    if ! $BINARY keys show "$1" --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add "$1" --keyring-backend test --output json > /dev/null 2>&1
    fi
}
# poll until a jq expression over a query is true, or N attempts pass
wait_until() { # <attempts> <sleep> <cmd...> ; the command must exit 0 when satisfied
    local N=$1 S=$2; shift 2
    for _ in $(seq 1 "$N"); do if "$@" > /dev/null 2>&1; then return 0; fi; sleep "$S"; done
    return 1
}

BOND_DENOM=$($BINARY q identity bond-denom --output json | jq -r '.denom // .bond_denom')
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
ensure_key vault_carol; ensure_key vault_dave; ensure_key vault_hot
CAROL_ADDR=$($BINARY keys show vault_carol -a --keyring-backend test)
DAVE_ADDR=$($BINARY keys show vault_dave -a --keyring-backend test)
HOT_ADDR=$($BINARY keys show vault_hot -a --keyring-backend test)
for A in "$CAROL_ADDR" "$DAVE_ADDR" "$HOT_ADDR"; do
    run_tx alice bank send "$ALICE_ADDR" "$A" 50000000${BOND_DENOM}
    tx_ok "$TX_RESULT" || { echo "funding $A failed: $(tx_log "$TX_RESULT")"; exit 1; }
done
MIN_TTL=$($BINARY q vault params --output json > /dev/null 2>&1 && echo 10)
echo "  alice=$ALICE_ADDR bob=$BOB_ADDR carol=$CAROL_ADDR dave=$DAVE_ADDR"
echo ""

# ----------------------------------------------------------------- TEST 1 ---
echo "--- TEST 1: create a 2-of-3 vault (fee burned, keyless account) ---"
FEE=$($BINARY q vault params --output json | jq -r '.params.creation_fee_amount // "0"')
SUPPLY_BEFORE=$($BINARY q bank total-supply-of "$BOND_DENOM" --output json 2>/dev/null | jq -r '.amount.amount // empty')
run_tx alice vault create-vault "$ALICE_ADDR:1,$BOB_ADDR:1,$CAROL_ADDR:1" 2 60 "phoenix treasury"
VAULT_ID=$(event_attr "$TX_RESULT" vault_created vault_id)
VAULT_ADDR=$(event_attr "$TX_RESULT" vault_created address)
SUPPLY_AFTER=$($BINARY q bank total-supply-of "$BOND_DENOM" --output json 2>/dev/null | jq -r '.amount.amount // empty')
if tx_ok "$TX_RESULT" && [ -n "$VAULT_ID" ] && [ -n "$VAULT_ADDR" ]; then
    V=$($BINARY q vault get-vault "$VAULT_ID" --output json)
    BYADDR=$($BINARY q vault vault-by-address "$VAULT_ADDR" --output json | jq -r '.vault.vault_id // "0"')
    if [ "$(echo "$V" | jq -r '.vault.threshold')" = "2" ] && [ "$BYADDR" = "$VAULT_ID" ] \
       && { [ -z "$SUPPLY_BEFORE" ] || [ "$SUPPLY_BEFORE" -gt "$SUPPLY_AFTER" ]; }; then
        echo "  vault #$VAULT_ID at $VAULT_ADDR (fee $FEE burned)"
        record_result "create 2-of-3 vault" "PASS"
    else
        echo "  unexpected: $V by_address=$BYADDR supply $SUPPLY_BEFORE -> $SUPPLY_AFTER"
        record_result "create 2-of-3 vault" "FAIL"
    fi
else
    echo "  create failed: $(tx_log "$TX_RESULT")"; record_result "create 2-of-3 vault" "FAIL"; exit 1
fi
run_tx alice bank send "$ALICE_ADDR" "$VAULT_ADDR" 10000000${BOND_DENOM}
tx_ok "$TX_RESULT" || { echo "funding the vault failed"; exit 1; }

# ---------------------------------------------------------------- TEST 1b ---
echo "--- TEST 1b: listed signers accept; until then they have no weight ---"
# alice created the vault, so only her entry starts accepted.
ACC=$($BINARY q vault get-vault "$VAULT_ID" --output json | jq -r '[.vault.signers[] | select(.accepted // false)] | length')
INVITED=$($BINARY q vault vaults-by-signer "$BOB_ADDR" --invited --output json | jq -r --arg id "$VAULT_ID" '[.vaults[]? | select((.vault_id // "0") == $id)] | length')
NOT_YET=$($BINARY q vault vaults-by-signer "$BOB_ADDR" --output json | jq -r --arg id "$VAULT_ID" '[.vaults[]? | select((.vault_id // "0") == $id)] | length')
run_tx bob vault accept-vault-signer "$VAULT_ID"
BOB_OK=$(tx_ok "$TX_RESULT" && echo yes || echo no)
run_tx bob vault accept-vault-signer "$VAULT_ID"
TWICE_REFUSED=$(! tx_ok "$TX_RESULT" && echo yes || echo no)
run_tx vault_carol vault accept-vault-signer "$VAULT_ID"
CAROL_OK=$(tx_ok "$TX_RESULT" && echo yes || echo no)
ACC_AFTER=$($BINARY q vault get-vault "$VAULT_ID" --output json | jq -r '[.vault.signers[] | select(.accepted // false)] | length')
if [ "$ACC" = "1" ] && [ "$INVITED" = "1" ] && [ "$NOT_YET" = "0" ] && [ "$BOB_OK" = yes ] \
   && [ "$TWICE_REFUSED" = yes ] && [ "$CAROL_OK" = yes ] && [ "$ACC_AFTER" = "3" ]; then
    echo "  alice accepted at create; bob and carol accepted their listings"
    record_result "signers accept their listing" "PASS"
else
    echo "  accepted=$ACC->$ACC_AFTER invited=$INVITED not_yet=$NOT_YET bob=$BOB_OK twice_refused=$TWICE_REFUSED carol=$CAROL_OK log=$(tx_log "$TX_RESULT")"
    record_result "signers accept their listing" "FAIL"
fi

# ----------------------------------------------------------------- TEST 2 ---
echo "--- TEST 2: propose a send, approve with the hash, execute ---"
cat > "$WORK/send.json" <<JSON
{"messages":[{"@type":"/cosmos.bank.v1beta1.MsgSend","from_address":"$VAULT_ADDR","to_address":"$DAVE_ADDR","amount":[{"denom":"$BOND_DENOM","amount":"1000000"}]}],"memo":"pay dave"}
JSON
run_tx alice vault propose "$VAULT_ID" "$WORK/send.json"
PID=$(event_attr "$TX_RESULT" vault_proposal_created proposal_id)
HASH=$(event_attr "$TX_RESULT" vault_proposal_created messages_hash)
TALLY=$($BINARY q vault tally "$PID" --output json 2>/dev/null)
echo "  proposal #$PID hash=$HASH tally=$(echo "$TALLY" | jq -c .)"
# wrong hash refused
run_tx bob vault approve "$PID" "00"
WRONG_REFUSED=$(! tx_ok "$TX_RESULT" && echo yes || echo no)
# a non-signer cannot approve
run_tx vault_dave vault approve "$PID" "$HASH"
OUTSIDER_REFUSED=$(! tx_ok "$TX_RESULT" && echo yes || echo no)
# execute below threshold refused
run_tx alice vault execute "$PID"
EARLY_REFUSED=$(! tx_ok "$TX_RESULT" && echo yes || echo no)
DAVE_BEFORE=$($BINARY q bank balance "$DAVE_ADDR" "$BOND_DENOM" --output json | jq -r '.balance.amount')
run_tx bob vault approve "$PID" "$HASH" --execute
DAVE_AFTER=$($BINARY q bank balance "$DAVE_ADDR" "$BOND_DENOM" --output json | jq -r '.balance.amount')
STATUS=$($BINARY q vault get-vault-proposal "$PID" --output json | jq -r '.vault_proposal.status')
if tx_ok "$TX_RESULT" && [ "$STATUS" = "PROPOSAL_STATUS_EXECUTED" ] && [ $((DAVE_AFTER - DAVE_BEFORE)) -eq 1000000 ] \
   && [ "$WRONG_REFUSED" = yes ] && [ "$OUTSIDER_REFUSED" = yes ] && [ "$EARLY_REFUSED" = yes ]; then
    echo "  executed on the second approval; wrong hash / outsider / early execute refused"
    record_result "propose, approve, execute" "PASS"
else
    echo "  status=$STATUS dave=$DAVE_BEFORE->$DAVE_AFTER wrong=$WRONG_REFUSED outsider=$OUTSIDER_REFUSED early=$EARLY_REFUSED log=$(tx_log "$TX_RESULT")"
    record_result "propose, approve, execute" "FAIL"
fi

# ----------------------------------------------------------------- TEST 3 ---
echo "--- TEST 3: rotate a signer; other pending proposals are INVALIDATED ---"
run_tx bob vault propose "$VAULT_ID" "$WORK/send.json"
VICTIM=$(event_attr "$TX_RESULT" vault_proposal_created proposal_id)
cat > "$WORK/rotate.json" <<JSON
{"messages":[{"@type":"/sparkdream.vault.v1.MsgUpdateVault","vault":"$VAULT_ADDR","signers":[{"address":"$ALICE_ADDR","weight":"1"},{"address":"$BOB_ADDR","weight":"1"},{"address":"$DAVE_ADDR","weight":"2"}],"threshold":"3","proposal_ttl":"20","label":"rotated"}],"memo":"carol out, dave in"}
JSON
run_tx alice vault propose "$VAULT_ID" "$WORK/rotate.json"
RPID=$(event_attr "$TX_RESULT" vault_proposal_created proposal_id)
RHASH=$(event_attr "$TX_RESULT" vault_proposal_created messages_hash)
run_tx vault_carol vault approve "$RPID" "$RHASH" --execute
V=$($BINARY q vault get-vault "$VAULT_ID" --output json)
VER=$(echo "$V" | jq -r '.vault.config_version')
THR=$(echo "$V" | jq -r '.vault.threshold')
VSTATUS=$($BINARY q vault get-vault-proposal "$VICTIM" --output json | jq -r '.vault_proposal.status')
RSTATUS=$($BINARY q vault get-vault-proposal "$RPID" --output json | jq -r '.vault_proposal.status')
# dave was added by the vault: he must accept before his weight counts
# (threshold 3 needs him).
DAVE_INVITED=$($BINARY q vault vaults-by-signer "$DAVE_ADDR" --invited --output json | jq -r --arg id "$VAULT_ID" '[.vaults[]? | select((.vault_id // "0") == $id)] | length')
run_tx vault_dave vault accept-vault-signer "$VAULT_ID"
tx_ok "$TX_RESULT" || echo "  dave accept failed: $(tx_log "$TX_RESULT")"
BYSIGNER=$($BINARY q vault vaults-by-signer "$DAVE_ADDR" --output json | jq -r --arg id "$VAULT_ID" '[.vaults[]? | select((.vault_id // "0") == $id)] | length')
run_tx vault_carol vault propose "$VAULT_ID" "$WORK/send.json"
CAROL_REFUSED=$(! tx_ok "$TX_RESULT" && echo yes || echo no)
if [ "$VER" = "2" ] && [ "$THR" = "3" ] && [ "$VSTATUS" = "PROPOSAL_STATUS_INVALIDATED" ] && [ "$RSTATUS" = "PROPOSAL_STATUS_EXECUTED" ] \
   && [ "$DAVE_INVITED" = "1" ] && [ "$BYSIGNER" = "1" ] && [ "$CAROL_REFUSED" = yes ]; then
    echo "  config_version=2 threshold=3; #$VICTIM invalidated, rotation itself executed; carol is out, dave accepted and is in"
    record_result "rotation invalidates pending" "PASS"
else
    echo "  ver=$VER thr=$THR victim=$VSTATUS rot=$RSTATUS dave_invited=$DAVE_INVITED by_signer=$BYSIGNER carol_refused=$CAROL_REFUSED"
    record_result "rotation invalidates pending" "FAIL"
fi

# ----------------------------------------------------------------- TEST 4 ---
echo "--- TEST 4: an expired proposal cannot execute; it is pruned after retention ---"
run_tx alice vault propose "$VAULT_ID" "$WORK/send.json"
EPID=$(event_attr "$TX_RESULT" vault_proposal_created proposal_id)
EHASH=$(event_attr "$TX_RESULT" vault_proposal_created messages_hash)
EXP=$($BINARY q vault get-vault-proposal "$EPID" --output json | jq -r '.vault_proposal.expires_at')
echo "  proposal #$EPID expires_at=$EXP (ttl 20s); waiting on block time"
is_expired() { [ "$($BINARY q vault get-vault-proposal "$EPID" --output json 2>/dev/null | jq -r '.vault_proposal.status')" = "PROPOSAL_STATUS_EXPIRED" ]; }
wait_until 40 2 is_expired
ESTATUS=$($BINARY q vault get-vault-proposal "$EPID" --output json | jq -r '.vault_proposal.status')
run_tx bob vault approve "$EPID" "$EHASH" --execute
LATE_REFUSED=$(! tx_ok "$TX_RESULT" && echo yes || echo no)
is_pruned() { ! $BINARY q vault get-vault-proposal "$EPID" --output json > /dev/null 2>&1; }
wait_until 60 2 is_pruned
PRUNED=$(is_pruned && echo yes || echo no)
if [ "$ESTATUS" = "PROPOSAL_STATUS_EXPIRED" ] && [ "$LATE_REFUSED" = yes ] && [ "$PRUNED" = yes ]; then
    echo "  expired by the EndBlocker, refused afterwards, pruned after retention"
    record_result "expiry and retention" "PASS"
else
    echo "  status=$ESTATUS late_refused=$LATE_REFUSED pruned=$PRUNED"
    record_result "expiry and retention" "FAIL"
fi

# ----------------------------------------------------------------- TEST 5 ---
echo "--- TEST 5: a vault has no personhood: it cannot be invited ---"
REQ=$($BINARY q rep required-invitation-stake "$ALICE_ADDR" --output json 2>/dev/null | jq -r '.required_stake // "100000000"')
run_tx alice rep invite-member "$VAULT_ADDR" "$REQ"
if ! tx_ok "$TX_RESULT" && echo "$(tx_log "$TX_RESULT")" | grep -qi "vault"; then
    echo "  refused: $(tx_log "$TX_RESULT")"
    record_result "vault refused as invitee" "PASS"
else
    echo "  code=$(echo "$TX_RESULT" | jq -r .code) log=$(tx_log "$TX_RESULT")"
    record_result "vault refused as invitee" "FAIL"
fi

# ----------------------------------------------------------------- TEST 6 ---
echo "--- TEST 6: a vault grants a SpendingAllowance to a signer, who pulls it ---"
EXPIRES=$(date -u -d "+2 hours" +"%Y-%m-%dT%H:%M:%SZ")
cat > "$WORK/allowance.json" <<JSON
{"messages":[{"@type":"/sparkdream.session.v1.MsgCreateGrant","granter":"$VAULT_ADDR","grantee":"$DAVE_ADDR","expires_at":"$EXPIRES","note":"dave's allowance",
  "spending_allowance":{"max_per_period":{"denom":"$BOND_DENOM","amount":"500000"},"period_seconds":"3600","current_period_start":"0","spent_in_current_period":{"denom":"$BOND_DENOM","amount":"0"},"allowed_recipients":[],"denom":"$BOND_DENOM"}}],"memo":"allowance"}
JSON
run_tx alice vault propose "$VAULT_ID" "$WORK/allowance.json"
APID=$(event_attr "$TX_RESULT" vault_proposal_created proposal_id)
AHASH=$(event_attr "$TX_RESULT" vault_proposal_created messages_hash)
run_tx vault_dave vault approve "$APID" "$AHASH" --execute
GRANT_ID=$($BINARY q session grants-by-grantee "$DAVE_ADDR" --output json 2>/dev/null | jq -r '[.grants[]? | select(.type=="GRANT_TYPE_SPENDING_ALLOWANCE" and .granter=="'"$VAULT_ADDR"'")][0].id // empty')
if [ -n "$GRANT_ID" ]; then
    HOT_BEFORE=$($BINARY q bank balance "$HOT_ADDR" "$BOND_DENOM" --output json | jq -r '.balance.amount')
    run_tx vault_dave session pull-allowance "$GRANT_ID" "$HOT_ADDR" 100000${BOND_DENOM}
    HOT_AFTER=$($BINARY q bank balance "$HOT_ADDR" "$BOND_DENOM" --output json | jq -r '.balance.amount')
    if tx_ok "$TX_RESULT" && [ $((HOT_AFTER - HOT_BEFORE)) -eq 100000 ]; then
        echo "  grant #$GRANT_ID created by the vault; dave pulled 0.1 SPARK to the hot key"
        record_result "spending allowance from a vault" "PASS"
    else
        echo "  pull failed: $(tx_log "$TX_RESULT")"; record_result "spending allowance from a vault" "FAIL"
    fi
else
    echo "  grant missing: $(tx_log "$TX_RESULT")"; record_result "spending allowance from a vault" "FAIL"
fi

# ----------------------------------------------------------------- TEST 7 ---
echo "--- TEST 7: vault messages are never session-delegable ---"
ALLOWED=$($BINARY q session allowed-msg-types --output json 2>/dev/null | jq -r '.. | strings' | grep -c "sparkdream.vault" || true)
EXPIRATION=$(date -u -d "+1 hours" +"%Y-%m-%dT%H:%M:%SZ")
run_tx bob session create-session "$HOT_ADDR" "/sparkdream.vault.v1.MsgApprove" "1000000${BOND_DENOM}" "$EXPIRATION" "3"
if [ "$ALLOWED" = "0" ] && ! tx_ok "$TX_RESULT"; then
    echo "  session with MsgApprove refused: $(tx_log "$TX_RESULT")"
    record_result "vault msgs not delegable" "PASS"
else
    echo "  allowed_matches=$ALLOWED code=$(echo "$TX_RESULT" | jq -r .code)"
    record_result "vault msgs not delegable" "FAIL"
fi

# ----------------------------------------------------------------- TEST 8 ---
echo "--- TEST 8: proposer cancels; a non-proposer cannot ---"
run_tx bob vault propose "$VAULT_ID" "$WORK/send.json"
CPID=$(event_attr "$TX_RESULT" vault_proposal_created proposal_id)
run_tx alice vault cancel-proposal "$CPID"
OTHER_REFUSED=$(! tx_ok "$TX_RESULT" && echo yes || echo no)
run_tx bob vault cancel-proposal "$CPID"
CSTATUS=$($BINARY q vault get-vault-proposal "$CPID" --output json | jq -r '.vault_proposal.status')
if [ "$OTHER_REFUSED" = yes ] && [ "$CSTATUS" = "PROPOSAL_STATUS_CANCELLED" ]; then
    echo "  cancelled by the proposer only"
    record_result "cancel by proposer" "PASS"
else
    echo "  other_refused=$OTHER_REFUSED status=$CSTATUS"
    record_result "cancel by proposer" "FAIL"
fi

# ----------------------------------------------------------------- TEST 9 ---
echo "--- TEST 9: an invitee declines with leave-vault; the last signer cannot leave ---"
run_tx alice vault create-vault "$ALICE_ADDR:1,$CAROL_ADDR:1" 1 60 "aurora"
LV_ID=$(event_attr "$TX_RESULT" vault_created vault_id)
run_tx vault_carol vault leave-vault "$LV_ID"
DECLINED=$(tx_ok "$TX_RESULT" && echo yes || echo no)
LV_SIGNERS=$($BINARY q vault get-vault "$LV_ID" --output json | jq -r '.vault.signers | length')
LV_VER=$($BINARY q vault get-vault "$LV_ID" --output json | jq -r '.vault.config_version')
run_tx alice vault leave-vault "$LV_ID"
LAST_REFUSED=$(! tx_ok "$TX_RESULT" && echo yes || echo no)
if [ -n "$LV_ID" ] && [ "$DECLINED" = yes ] && [ "$LV_SIGNERS" = "1" ] && [ "$LV_VER" = "1" ] && [ "$LAST_REFUSED" = yes ]; then
    echo "  carol declined vault #$LV_ID (no config bump); alice, the last signer, cannot leave"
    record_result "decline and leave" "PASS"
else
    echo "  vault=$LV_ID declined=$DECLINED signers=$LV_SIGNERS ver=$LV_VER last_refused=$LAST_REFUSED log=$(tx_log "$TX_RESULT")"
    record_result "decline and leave" "FAIL"
fi

# ---------------------------------------------------------------- TEST 10 ---
echo "--- TEST 10: an unanswered invitation expires (InviteTTL) and the threshold clamps ---"
# 2-of-{alice, dave}: alice's entry is accepted at create, dave never answers.
run_tx alice vault create-vault "$ALICE_ADDR:1,$DAVE_ADDR:1" 2 60 "zenith"
EX_ID=$(event_attr "$TX_RESULT" vault_created vault_id)
EX_EXPIRES=$($BINARY q vault get-vault "$EX_ID" --output json | jq -r --arg d "$DAVE_ADDR" '.vault.signers[] | select(.address==$d) | .invite_expires_at // "0"')
EX_ALICE_EXPIRES=$($BINARY q vault get-vault "$EX_ID" --output json | jq -r --arg a "$ALICE_ADDR" '.vault.signers[] | select(.address==$a) | .invite_expires_at // "0"')
# The test build's InviteTTL is 45 s: poll block time past the expiry, then the EndBlocker.
expired() { [ "$($BINARY q vault get-vault "$EX_ID" --output json | jq -r '.vault.signers | length')" = "1" ]; }
wait_until 60 2 expired
EX_SIGNERS=$($BINARY q vault get-vault "$EX_ID" --output json | jq -r '.vault.signers | length')
EX_THR=$($BINARY q vault get-vault "$EX_ID" --output json | jq -r '.vault.threshold')
EX_VER=$($BINARY q vault get-vault "$EX_ID" --output json | jq -r '.vault.config_version')
EX_DAVE_INVITED=$($BINARY q vault vaults-by-signer "$DAVE_ADDR" --invited --output json | jq -r --arg id "$EX_ID" '[.vaults[]? | select((.vault_id // "0") == $id)] | length')
if [ -n "$EX_ID" ] && [ "$EX_EXPIRES" != "0" ] && [ "$EX_ALICE_EXPIRES" = "0" ] && [ "$EX_SIGNERS" = "1" ] && [ "$EX_THR" = "1" ] && [ "$EX_VER" = "2" ] && [ "$EX_DAVE_INVITED" = "0" ]; then
    echo "  dave's invitation to vault #$EX_ID expired at $EX_EXPIRES; threshold 2 -> 1, config_version 2, alice can act alone"
    record_result "invite expiry clamps threshold" "PASS"
else
    echo "  vault=$EX_ID dave_expires=$EX_EXPIRES alice_expires=$EX_ALICE_EXPIRES signers=$EX_SIGNERS thr=$EX_THR ver=$EX_VER dave_invited=$EX_DAVE_INVITED"
    record_result "invite expiry clamps threshold" "FAIL"
fi

# ---------------------------------------------------------------- TEST 11 ---
echo "--- TEST 11: a vault whose only entry is an unaccepted stranger dissolves when they decline ---"
# The creator need not be a signer (spec 3.4): alice lists bob alone, so the
# vault has no accepted signer. Bob's decline (leave-vault on an unaccepted
# entry) leaves no signer behind; with no balance and no proposal record the
# vault is deleted outright (vault_deleted) instead of lingering signer-less.
run_tx alice vault create-vault "$BOB_ADDR:1" 1 60 "stray listing"
SL_ID=$(event_attr "$TX_RESULT" vault_created vault_id)
SL_ADDR=$(event_attr "$TX_RESULT" vault_created address)
SL_ACC=$($BINARY q vault get-vault "$SL_ID" --output json 2>/dev/null | jq -r '[.vault.signers[]? | select(.accepted // false)] | length')
SL_BOB_INVITED=$($BINARY q vault vaults-by-signer "$BOB_ADDR" --invited --output json | jq -r --arg id "$SL_ID" '[.vaults[]? | select((.vault_id // "0") == $id)] | length')
run_tx bob vault leave-vault "$SL_ID"
SL_DECLINED=$(tx_ok "$TX_RESULT" && echo yes || echo no)
SL_DELETED_ID=$(event_attr "$TX_RESULT" vault_deleted vault_id)
SL_GONE=$(! $BINARY q vault get-vault "$SL_ID" --output json > /dev/null 2>&1 && echo yes || echo no)
SL_BYADDR_GONE=$(! $BINARY q vault vault-by-address "$SL_ADDR" --output json > /dev/null 2>&1 && echo yes || echo no)
SL_BOB_AFTER=$($BINARY q vault vaults-by-signer "$BOB_ADDR" --invited --output json | jq -r --arg id "$SL_ID" '[.vaults[]? | select((.vault_id // "0") == $id)] | length')
if [ -n "$SL_ID" ] && [ "$SL_ACC" = "0" ] && [ "$SL_BOB_INVITED" = "1" ] && [ "$SL_DECLINED" = yes ] \
   && [ "$SL_DELETED_ID" = "$SL_ID" ] && [ "$SL_GONE" = yes ] && [ "$SL_BYADDR_GONE" = yes ] && [ "$SL_BOB_AFTER" = "0" ]; then
    echo "  [ OK ] bob declined vault #$SL_ID (no accepted signer); vault_deleted emitted, get-vault and vault-by-address report not found"
    record_result "sole unaccepted entry declines, vault deleted" "PASS"
else
    echo "  [FAIL] vault=$SL_ID accepted=$SL_ACC bob_invited=$SL_BOB_INVITED declined=$SL_DECLINED deleted_event=$SL_DELETED_ID gone=$SL_GONE by_addr_gone=$SL_BYADDR_GONE bob_after=$SL_BOB_AFTER log=$(tx_log "$TX_RESULT")"
    record_result "sole unaccepted entry declines, vault deleted" "FAIL"
fi

rm -rf "$WORK"
echo "============================================"
echo "X/VAULT TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"; done
echo ""; echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"; echo ""
if [ $FAIL_COUNT -gt 0 ]; then echo ">>> SOME TESTS FAILED <<<"; exit 1; else echo ">>> ALL TESTS PASSED <<<"; exit 0; fi
