#!/bin/bash

# ============================================================================
# NATIVE MULTISIG ACCOUNT E2E (docs/multisig-accounts.md)
# ============================================================================
# A 2-of-3 LegacyAminoPubKey account is a supported account type: it holds
# SPARK, joins x/rep as a member, registers / clears / re-registers its shield
# key with the co-signer fingerprint check, and acts day to day through an
# x/session hot key (cold root + hot key). Every multisig tx goes through the
# CLI flow: --generate-only, tx sign --multisig (k times), tx multisign,
# tx broadcast. Lives in the session suite because the hot-key pattern is the
# recommended way to use a multisig, and alice (ESTABLISHED) can invite it.
# ============================================================================

echo "--- TESTING: NATIVE MULTISIG ACCOUNT ---"
echo ""

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
WORK="${TMPDIR:-/tmp}/sparkdream-multisig-e2e-$$"
mkdir -p "$WORK"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

# ---------------------------------------------------------------- helpers ---
wait_for_tx() {
    local TXHASH=$1 ATTEMPT=0
    while [ $ATTEMPT -lt 30 ]; do
        RESULT=$($BINARY q tx "$TXHASH" --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done
    echo '{"code":98,"raw_log":"tx not found"}'
    return 1
}

# Broadcast result -> delivered result in TX_RESULT.
submit_tx_and_wait() {
    local TX_RES="$1"
    local TXHASH
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$TXHASH" ]; then
        TX_RESULT=$(jq -n --arg log "$TX_RES" '{code:99, raw_log:$log}')
        return 1
    fi
    local BROADCAST_CODE
    BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

tx_ok()   { [ "$(echo "$1" | jq -r '.code')" = "0" ]; }
tx_log()  { echo "$1" | jq -r '.raw_log // "unknown"' 2>/dev/null | cut -c1-200; }

record_result() {
    TEST_NAMES+=("$1"); RESULTS+=("$2")
    if [ "$2" == "PASS" ]; then PASS_COUNT=$((PASS_COUNT + 1)); else FAIL_COUNT=$((FAIL_COUNT + 1)); fi
    echo "  => $2"; echo ""
}

get_future_expiration() { date -u -d "+${1:-1} hours" +"%Y-%m-%dT%H:%M:%SZ"; }

# Single-key tx from a keyring account, waits for delivery.
send_single() {
    local TX_RES
    TX_RES=$($BINARY tx "$@" --chain-id $CHAIN_ID --keyring-backend test -y --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
}

# Collect k co-signatures on an unsigned tx file and broadcast it.
# Usage: multisign_broadcast <unsigned.json> <signer_key>... ; sets TX_RESULT.
multisign_broadcast() {
    local UNSIGNED=$1; shift
    local SIGS=() KEY SIGFILE
    for KEY in "$@"; do
        SIGFILE="$WORK/sig_${KEY}_$RANDOM.json"
        if ! $BINARY tx sign "$UNSIGNED" --multisig ms_acct --from "$KEY" \
                --chain-id $CHAIN_ID --keyring-backend test \
                --output-document "$SIGFILE" > "$WORK/sign.log" 2>&1; then
            TX_RESULT=$(jq -n --arg log "sign($KEY): $(cat "$WORK/sign.log")" '{code:97, raw_log:$log}')
            return 1
        fi
        SIGS+=("$SIGFILE")
    done
    local SIGNED="$WORK/signed_$RANDOM.json"
    if ! $BINARY tx multisign "$UNSIGNED" ms_acct "${SIGS[@]}" \
            --chain-id $CHAIN_ID --keyring-backend test \
            --output-document "$SIGNED" > "$WORK/multisign.log" 2>&1; then
        TX_RESULT=$(jq -n --arg log "multisign: $(cat "$WORK/multisign.log")" '{code:96, raw_log:$log}')
        return 1
    fi
    local TX_RES
    TX_RES=$($BINARY tx broadcast "$SIGNED" --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
}

# Build an unsigned multisig tx with the CLI (generate-only) into a file.
# Usage: ms_generate <out.json> <module> <cmd> [args/flags...]
ms_generate() {
    local OUT=$1; shift
    $BINARY tx "$@" --from ms_acct --generate-only \
        --chain-id $CHAIN_ID --keyring-backend test \
        --gas 300000 --fees 50000${BOND_DENOM} > "$OUT" 2> "$WORK/generate.log"
}

ensure_key() {
    if ! $BINARY keys show "$1" --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add "$1" --keyring-backend test --output json > /dev/null 2>&1
    fi
}

# ------------------------------------------------------------------ setup ---
echo "--- SETUP: 2-of-3 multisig account ---"
for K in ms_k1 ms_k2 ms_k3 ms_hot; do ensure_key "$K"; done
if ! $BINARY keys show ms_acct --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add ms_acct --multisig ms_k1,ms_k2,ms_k3 --multisig-threshold 2 \
        --keyring-backend test --output json > /dev/null 2>&1
fi
MS_ADDR=$($BINARY keys show ms_acct -a --keyring-backend test)
HOT_ADDR=$($BINARY keys show ms_hot -a --keyring-backend test)
echo "  multisig: $MS_ADDR"
echo "  hot key:  $HOT_ADDR"

send_single bank send alice "$MS_ADDR" 500000000${BOND_DENOM} --fees 5000${BOND_DENOM}
tx_ok "$TX_RESULT" || { echo "  could not fund the multisig: $(tx_log "$TX_RESULT")"; exit 1; }
send_single bank send alice "$HOT_ADDR" 20000000${BOND_DENOM} --fees 5000${BOND_DENOM}
tx_ok "$TX_RESULT" || { echo "  could not fund the hot key: $(tx_log "$TX_RESULT")"; exit 1; }
echo ""

# ----------------------------------------------------------------- TEST 1 ---
echo "--- TEST 1: 2-of-3 bank send (sign x2, multisign, broadcast) ---"
ms_generate "$WORK/send.json" bank send "$MS_ADDR" "$ALICE_ADDR" 1000000${BOND_DENOM}
BEFORE=$($BINARY query bank balance "$ALICE_ADDR" "$BOND_DENOM" --output json | jq -r '.balance.amount')
multisign_broadcast "$WORK/send.json" ms_k1 ms_k3
AFTER=$($BINARY query bank balance "$ALICE_ADDR" "$BOND_DENOM" --output json | jq -r '.balance.amount')
if tx_ok "$TX_RESULT" && [ "$AFTER" -gt "$BEFORE" ]; then
    echo "  SPARK moved with two of three signatures"
    record_result "2-of-3 bank send" "PASS"
else
    echo "  TX failed: $(tx_log "$TX_RESULT")"
    record_result "2-of-3 bank send" "FAIL"
fi

# ----------------------------------------------------------------- TEST 2 ---
echo "--- TEST 2: 1-of-3 is refused ---"
ms_generate "$WORK/send1.json" bank send "$MS_ADDR" "$ALICE_ADDR" 1000000${BOND_DENOM}
multisign_broadcast "$WORK/send1.json" ms_k2
if tx_ok "$TX_RESULT"; then
    echo "  a single signature was accepted"
    record_result "1-of-3 refused" "FAIL"
else
    echo "  refused: $(tx_log "$TX_RESULT")"
    record_result "1-of-3 refused" "PASS"
fi

# ----------------------------------------------------------------- TEST 3 ---
echo "--- TEST 3: the multisig becomes an x/rep member ---"
REQUIRED_STAKE=$($BINARY query rep required-invitation-stake "$ALICE_ADDR" --output json 2>/dev/null | jq -r '.required_stake // "100000000"')
send_single rep invite-member --gas 400000 "$MS_ADDR" "$REQUIRED_STAKE" --from alice --fees 5000${BOND_DENOM}
INV_ID=""
if tx_ok "$TX_RESULT"; then
    INV_ID=$(echo "$TX_RESULT" | jq -r '[.events[] | select(.type=="create_invitation") | .attributes[] | select(.key=="invitation_id") | .value][0] // empty')
fi
if [ -z "$INV_ID" ]; then
    echo "  invitation failed: $(tx_log "$TX_RESULT")"
    record_result "multisig accepts invitation" "FAIL"
else
    ms_generate "$WORK/accept.json" rep accept-invitation "$INV_ID" CC0-1.0 --gas 400000
    multisign_broadcast "$WORK/accept.json" ms_k1 ms_k2
    # MEMBER_STATUS_ACTIVE is the enum's zero value, so proto3 JSON omits it.
    MEMBER_JSON=$($BINARY query rep get-member "$MS_ADDR" --output json 2>/dev/null)
    MEMBER_ADDR=$(echo "$MEMBER_JSON" | jq -r '.member.address // empty')
    STATUS=$(echo "$MEMBER_JSON" | jq -r '.member.status // "MEMBER_STATUS_ACTIVE"')
    if tx_ok "$TX_RESULT" && [ "$MEMBER_ADDR" = "$MS_ADDR" ] && [ "$STATUS" = "MEMBER_STATUS_ACTIVE" ]; then
        echo "  multisig is an active member (invitation #$INV_ID)"
        record_result "multisig accepts invitation" "PASS"
    else
        echo "  accept failed: $(tx_log "$TX_RESULT") status=$STATUS"
        record_result "multisig accepts invitation" "FAIL"
    fi
fi

# ----------------------------------------------------------------- TEST 4 ---
echo "--- TEST 4: ZK key registration with the co-signer fingerprint check ---"
# The seed-holding device derives the key; a co-signer without the seed
# fingerprints the tx and compares. Both must agree before anyone signs.
SEED_OUT=$($BINARY zk-key-fingerprint seed "e2e-multisig-shield-seed-$RANDOM" 2>&1)
ZK_HEX=$(echo "$SEED_OUT" | awk '/^key/ {print $2}')
FP_SEED=$(echo "$SEED_OUT" | sed -n 's/^fingerprint *//p')
ms_generate "$WORK/zk.json" rep register-zk-public-key "$ZK_HEX"
FP_TX=$($BINARY zk-key-fingerprint tx "$WORK/zk.json" 2>&1 | sed -n 's/^  fingerprint *//p')
echo "  seed fingerprint: $FP_SEED"
echo "  tx fingerprint:   $FP_TX"
if [ -n "$FP_SEED" ] && [ "$FP_SEED" = "$FP_TX" ]; then
    multisign_broadcast "$WORK/zk.json" ms_k2 ms_k3
    ZK_NOW=$($BINARY query rep get-member "$MS_ADDR" --output json 2>/dev/null | jq -r '.member.zk_public_key // empty')
    if tx_ok "$TX_RESULT" && [ -n "$ZK_NOW" ]; then
        echo "  ZK key registered by threshold (first registration is immediate)"
        record_result "ZK key registered after fingerprint match" "PASS"
    else
        echo "  registration failed: $(tx_log "$TX_RESULT")"
        record_result "ZK key registered after fingerprint match" "FAIL"
    fi
else
    echo "  fingerprint mismatch or helper failed: $SEED_OUT"
    record_result "ZK key registered after fingerprint match" "FAIL"
fi

# ----------------------------------------------------------------- TEST 5 ---
echo "--- TEST 5: clear a leaked ZK key; the next registration is not immediate ---"
ms_generate "$WORK/clear.json" rep clear-zk-public-key
multisign_broadcast "$WORK/clear.json" ms_k1 ms_k2
M=$($BINARY query rep get-member "$MS_ADDR" --output json 2>/dev/null)
ZK_AFTER=$(echo "$M" | jq -r '.member.zk_public_key // empty')
CLEARED=$(echo "$M" | jq -r '.member.zk_key_cleared // false')
if tx_ok "$TX_RESULT" && [ -z "$ZK_AFTER" ] && [ "$CLEARED" = "true" ]; then
    echo "  key cleared at once, zk_key_cleared set"
    ZK2_HEX=$($BINARY zk-key-fingerprint seed "e2e-multisig-shield-seed-2-$RANDOM" 2>&1 | awk '/^key/ {print $2}')
    ms_generate "$WORK/zk2.json" rep register-zk-public-key "$ZK2_HEX"
    multisign_broadcast "$WORK/zk2.json" ms_k1 ms_k3
    M=$($BINARY query rep get-member "$MS_ADDR" --output json 2>/dev/null)
    ZK_LIVE=$(echo "$M" | jq -r '.member.zk_public_key // empty')
    ZK_PENDING=$(echo "$M" | jq -r '.member.pending_zk_public_key // empty')
    if tx_ok "$TX_RESULT" && [ -z "$ZK_LIVE" ] && [ -n "$ZK_PENDING" ]; then
        echo "  re-registration parked in pending_zk_public_key (waits the activation delay)"
        record_result "clear then register is not a free swap" "PASS"
    else
        echo "  unexpected: code=$(echo "$TX_RESULT" | jq -r .code) live=$ZK_LIVE pending=$ZK_PENDING"
        record_result "clear then register is not a free swap" "FAIL"
    fi
else
    echo "  clear failed: $(tx_log "$TX_RESULT") zk=$ZK_AFTER cleared=$CLEARED"
    record_result "clear then register is not a free swap" "FAIL"
fi

# ----------------------------------------------------------------- TEST 6 ---
echo "--- TEST 6: cold root + hot key: the multisig grants a session key ---"
EXPIRATION=$(get_future_expiration 2)
ms_generate "$WORK/session.json" session create-session "$HOT_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost" "50000000${BOND_DENOM}" "$EXPIRATION" "3"
multisign_broadcast "$WORK/session.json" ms_k2 ms_k3
if tx_ok "$TX_RESULT"; then
    ACCT=$($BINARY query auth account "$HOT_ADDR" --output json 2>&1)
    ACCT_NUM=$(echo "$ACCT" | jq -r '.account.account_number // .account.base_account.account_number // "0"')
    SEQ=$(echo "$ACCT" | jq -r '.account.sequence // .account.base_account.sequence // "0"')
    cat > "$WORK/exec.json" <<TXEOF
{"body":{"messages":[{"@type":"/sparkdream.session.v1.MsgExecSession","grantee":"$HOT_ADDR","granter":"$MS_ADDR",
 "msgs":[{"@type":"/sparkdream.blog.v1.MsgCreatePost","creator":"$MS_ADDR","title":"Multisig hot key post","body":"posted by the hot key on behalf of a 2-of-3 multisig"}]}],
 "memo":"","timeout_height":"0","extension_options":[],"non_critical_extension_options":[]},
 "auth_info":{"signer_infos":[],"fee":{"amount":[{"denom":"${BOND_DENOM}","amount":"50000"}],"gas_limit":"300000","payer":"","granter":""}},
 "signatures":[]}
TXEOF
    $BINARY tx sign "$WORK/exec.json" --from ms_hot --chain-id $CHAIN_ID --keyring-backend test \
        --account-number "$ACCT_NUM" --sequence "$SEQ" --output-document "$WORK/exec_signed.json" > /dev/null 2>&1
    TX_RES=$($BINARY tx broadcast "$WORK/exec_signed.json" --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    EXEC_COUNT=$($BINARY query session session "$MS_ADDR" "$HOT_ADDR" --output json 2>/dev/null | jq -r '.session.exec_count // "0"')
    if tx_ok "$TX_RESULT" && [ "$EXEC_COUNT" = "1" ]; then
        echo "  hot key posted as the multisig (exec_count=1)"
        record_result "hot key acts through a multisig session" "PASS"
    else
        echo "  exec failed: $(tx_log "$TX_RESULT") exec_count=$EXEC_COUNT"
        record_result "hot key acts through a multisig session" "FAIL"
    fi
else
    echo "  create-session failed: $(tx_log "$TX_RESULT")"
    record_result "hot key acts through a multisig session" "FAIL"
fi

# ----------------------------------------------------------------- TEST 7 ---
echo "--- TEST 7: a session key can never register a ZK key ---"
# MsgRegisterZkPublicKey is on the rep identity list, so it is forbidden from
# every session allowlist: a hot key must not be able to re-point the
# member's anonymous identity around the threshold.
ALLOWED=$($BINARY query session params --output json 2>/dev/null | jq -r '.params.allowed_msg_types[]?' | grep -c "MsgRegisterZkPublicKey\|MsgClearZkPublicKey" || true)
cat > "$WORK/exec_zk.json" <<TXEOF
{"body":{"messages":[{"@type":"/sparkdream.session.v1.MsgExecSession","grantee":"$HOT_ADDR","granter":"$MS_ADDR",
 "msgs":[{"@type":"/sparkdream.rep.v1.MsgRegisterZkPublicKey","member":"$MS_ADDR","zk_public_key":"AQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}]}],
 "memo":"","timeout_height":"0","extension_options":[],"non_critical_extension_options":[]},
 "auth_info":{"signer_infos":[],"fee":{"amount":[{"denom":"${BOND_DENOM}","amount":"50000"}],"gas_limit":"300000","payer":"","granter":""}},
 "signatures":[]}
TXEOF
ACCT=$($BINARY query auth account "$HOT_ADDR" --output json 2>&1)
ACCT_NUM=$(echo "$ACCT" | jq -r '.account.account_number // .account.base_account.account_number // "0"')
SEQ=$(echo "$ACCT" | jq -r '.account.sequence // .account.base_account.sequence // "0"')
$BINARY tx sign "$WORK/exec_zk.json" --from ms_hot --chain-id $CHAIN_ID --keyring-backend test \
    --account-number "$ACCT_NUM" --sequence "$SEQ" --output-document "$WORK/exec_zk_signed.json" > /dev/null 2>&1
TX_RES=$($BINARY tx broadcast "$WORK/exec_zk_signed.json" --output json 2>&1)
submit_tx_and_wait "$TX_RES"
if [ "$ALLOWED" = "0" ] && ! tx_ok "$TX_RESULT"; then
    echo "  refused: $(tx_log "$TX_RESULT")"
    record_result "session key cannot register a ZK key" "PASS"
else
    echo "  allowed_msg_types matches=$ALLOWED code=$(echo "$TX_RESULT" | jq -r .code)"
    record_result "session key cannot register a ZK key" "FAIL"
fi

# ----------------------------------------------------------------- TEST 8 ---
echo "--- TEST 8: SignIn grant to a single key (ADR-036 sign-in path for multisig members) ---"
EXPIRES=$(get_future_expiration 24)
cat > "$WORK/signin.json" <<TXEOF
{"body":{"messages":[{"@type":"/sparkdream.session.v1.MsgCreateGrant","granter":"$MS_ADDR","grantee":"$HOT_ADDR",
 "expires_at":"$EXPIRES","note":"sign in for the multisig","sign_in":{"service":"login.example.test"}}],
 "memo":"","timeout_height":"0","extension_options":[],"non_critical_extension_options":[]},
 "auth_info":{"signer_infos":[],"fee":{"amount":[{"denom":"${BOND_DENOM}","amount":"50000"}],"gas_limit":"300000","payer":"","granter":""}},
 "signatures":[]}
TXEOF
multisign_broadcast "$WORK/signin.json" ms_k1 ms_k2
SIGNIN=$($BINARY query session grants-by-grantee "$HOT_ADDR" --output json 2>/dev/null \
    | jq -r '[.grants[]? | select(.type=="GRANT_TYPE_SIGN_IN" and .granter=="'"$MS_ADDR"'")] | length')
if tx_ok "$TX_RESULT" && [ "$SIGNIN" = "1" ]; then
    echo "  SignIn grant on file for the hot key"
    record_result "SignIn grant from a multisig" "PASS"
else
    echo "  failed: $(tx_log "$TX_RESULT") signin_grants=$SIGNIN"
    record_result "SignIn grant from a multisig" "FAIL"
fi

rm -rf "$WORK"

# ---------------------------------------------------------------- results ---
echo "============================================"
echo "NATIVE MULTISIG ACCOUNT TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
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
