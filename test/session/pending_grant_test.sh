#!/bin/bash
# Grantee consent for access-class grants (docs/x-session-spec.md §31,
# review finding SES-102).
#
# A session key or sign-in from a granter the grantee has not allowed is
# created PENDING: listed under the granter only, never under the grantee,
# with a refundable deposit escrowed from the granter. The grantee accepts
# it with `accept-grant` (or, for a session key, its first exec-session),
# which allows the granter for good; `disallow-granter` undoes that. A
# granter holds at most 4 pending grants.
#
# Accounts: session_granter (funded member, from setup_test_accounts.sh),
# consent_grantee (created and funded here).

echo "--- TESTING: PENDING GRANTS (GRANTEE CONSENT) ---"
echo ""

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=${2:-30}
    local ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        local RESULT
        RESULT=$($BINARY query tx "$TXHASH" --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$RESULT" ]; then
            echo "$RESULT"
            return 0
        fi
        sleep 1
        ATTEMPT=$((ATTEMPT + 1))
    done
    echo '{"code":99,"raw_log":"tx not found after timeout"}'
    return 1
}

# tx_ok: deliver a tx from a key and set TX_RESULT; returns the delivered code.
tx_ok() {
    local KEY=$1; shift
    local RES
    RES=$($BINARY tx "$@" --from "$KEY" --chain-id $CHAIN_ID --keyring-backend test \
        --fees 50000${BOND_DENOM} --gas 300000 -y --output json 2>&1)
    local TXHASH
    TXHASH=$(echo "$RES" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$TXHASH" ]; then
        TX_RESULT="$RES"
        return 1
    fi
    if [ "$(echo "$RES" | jq -r '.code // "0"')" != "0" ]; then
        TX_RESULT="$RES"
        return 1
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    [ "$(echo "$TX_RESULT" | jq -r '.code // "99"')" = "0" ]
}

tx_log() { echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null; }

record_result() {
    TEST_NAMES+=("$1"); RESULTS+=("$2")
    if [ "$2" == "PASS" ]; then PASS_COUNT=$((PASS_COUNT + 1)); else FAIL_COUNT=$((FAIL_COUNT + 1)); fi
    echo "  => $2"; echo ""
}

future() { date -u -d "+$1 hours" +"%Y-%m-%dT%H:%M:%SZ"; }

grant_status() { $BINARY query session grant "$1" --output json 2>/dev/null | jq -r '.grant.status // empty'; }

# Newest grant of the granter naming a grantee, by status.
granter_grant_id() {
    $BINARY query session grants-by-granter "$GRANTER_ADDR" --output json 2>/dev/null \
        | jq -r --arg g "$1" --arg s "$2" '[.grants[]? | select(.grantee==$g and .status==$s)] | sort_by(.id | tonumber) | last | .id // empty'
}

grantee_count() {
    $BINARY query session grants-by-grantee "$1" --output json 2>/dev/null | jq -r '.grants | length'
}

allowed_count() {
    $BINARY query session allowed-granters "$1" --output json 2>/dev/null \
        | jq -r --arg g "$GRANTER_ADDR" '[.granters[]? | select(.==$g)] | length'
}

balance() {
    $BINARY query bank balances "$1" --output json 2>/dev/null \
        | jq -r --arg d "$BOND_DENOM" '.balances[] | select(.denom==$d) | .amount // "0"'
}

# ---------------------------------------------------------------- setup ---
GRANTER_ADDR=$($BINARY keys show session_granter -a --keyring-backend test)
if ! $BINARY keys show consent_grantee --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add consent_grantee --keyring-backend test > /dev/null 2>&1
fi
GRANTEE_ADDR=$($BINARY keys show consent_grantee -a --keyring-backend test)
tx_ok alice bank send alice "$GRANTEE_ADDR" 20000000${BOND_DENOM} || { echo "funding failed: $(tx_log)"; exit 1; }
DEPOSIT=$($BINARY query session params --output json 2>/dev/null | jq -r '.params.pending_grant_deposit // "0"')
echo "  granter=$GRANTER_ADDR grantee=$GRANTEE_ADDR pending_grant_deposit=$DEPOSIT"
echo ""

# Any pending grants left by an earlier script would count against the cap.
for ID in $($BINARY query session grants-by-granter "$GRANTER_ADDR" --output json 2>/dev/null \
    | jq -r '.grants[]? | select(.status=="GRANT_STATUS_PENDING") | .id'); do
    tx_ok session_granter session revoke-grant "$ID" > /dev/null || true
done

# ---------------------------------------------------------------- TEST 1 ---
echo "--- TEST 1: a sign-in from an unknown granter is PENDING and invisible to the grantee ---"
BEFORE=$(balance "$GRANTER_ADDR")
cat > /tmp/consent_signin.json <<JSON
{"sign_in":{"service":"login.example.test"}}
JSON
if tx_ok session_granter session create-grant "$GRANTEE_ADDR" "$(future 24)" /tmp/consent_signin.json; then
    SIGNIN_ID=$(granter_grant_id "$GRANTEE_ADDR" "GRANT_STATUS_PENDING")
    STATUS=$(grant_status "$SIGNIN_ID")
    BY_GRANTEE=$(grantee_count "$GRANTEE_ADDR")
    AFTER=$(balance "$GRANTER_ADDR")
    # fees 50000 + deposit escrowed
    ESCROWED=$((BEFORE - AFTER - 50000))
    if [ -n "$SIGNIN_ID" ] && [ "$STATUS" = "GRANT_STATUS_PENDING" ] && [ "$BY_GRANTEE" = "0" ] && [ "$ESCROWED" = "$DEPOSIT" ]; then
        echo "  grant $SIGNIN_ID is PENDING, grants-by-grantee empty, $ESCROWED escrowed"
        record_result "unknown granter's sign-in is PENDING" "PASS"
    else
        echo "  id=$SIGNIN_ID status=$STATUS by_grantee=$BY_GRANTEE escrowed=$ESCROWED"
        record_result "unknown granter's sign-in is PENDING" "FAIL"
    fi
else
    echo "  create-grant failed: $(tx_log)"
    record_result "unknown granter's sign-in is PENDING" "FAIL"
fi

# ---------------------------------------------------------------- TEST 2 ---
echo "--- TEST 2: accept-grant activates it, allows the granter and refunds the deposit ---"
BEFORE=$(balance "$GRANTER_ADDR")
if tx_ok consent_grantee session accept-grant "$SIGNIN_ID"; then
    STATUS=$(grant_status "$SIGNIN_ID")
    BY_GRANTEE=$(grantee_count "$GRANTEE_ADDR")
    ALLOWED=$(allowed_count "$GRANTEE_ADDR")
    REFUND=$(( $(balance "$GRANTER_ADDR") - BEFORE ))
    if [ "$STATUS" = "GRANT_STATUS_ACTIVE" ] && [ "$BY_GRANTEE" = "1" ] && [ "$ALLOWED" = "1" ] && [ "$REFUND" = "$DEPOSIT" ]; then
        echo "  ACTIVE, listed under the grantee, granter allowed, $REFUND refunded"
        record_result "accept-grant" "PASS"
    else
        echo "  status=$STATUS by_grantee=$BY_GRANTEE allowed=$ALLOWED refund=$REFUND"
        record_result "accept-grant" "FAIL"
    fi
else
    echo "  accept-grant failed: $(tx_log)"
    record_result "accept-grant" "FAIL"
fi

# ---------------------------------------------------------------- TEST 3 ---
echo "--- TEST 3: an allowed granter's session key is ACTIVE at creation ---"
if tx_ok session_granter session create-session "$GRANTEE_ADDR" "/sparkdream.blog.v1.MsgCreatePost" \
    "50000000${BOND_DENOM}" "$(future 1)" "10"; then
    KEY_ID=$(granter_grant_id "$GRANTEE_ADDR" "GRANT_STATUS_ACTIVE")
    if [ -n "$KEY_ID" ] && [ "$(grantee_count "$GRANTEE_ADDR")" = "2" ]; then
        echo "  session key $KEY_ID active at creation"
        record_result "allowed granter skips PENDING" "PASS"
    else
        echo "  key_id=$KEY_ID by_grantee=$(grantee_count "$GRANTEE_ADDR")"
        record_result "allowed granter skips PENDING" "FAIL"
    fi
else
    echo "  create-session failed: $(tx_log)"
    record_result "allowed granter skips PENDING" "FAIL"
fi

# ---------------------------------------------------------------- TEST 4 ---
echo "--- TEST 4: disallow-granter; the next grant waits again; decline refunds ---"
tx_ok session_granter session revoke-grant "$KEY_ID" > /dev/null || true
if tx_ok consent_grantee session disallow-granter "$GRANTER_ADDR" \
    && [ "$(allowed_count "$GRANTEE_ADDR")" = "0" ] \
    && [ "$(grant_status "$SIGNIN_ID")" = "GRANT_STATUS_ACTIVE" ] \
    && tx_ok session_granter session create-session "$GRANTEE_ADDR" "/sparkdream.blog.v1.MsgCreatePost" \
        "50000000${BOND_DENOM}" "$(future 1)" "10"; then
    PENDING_ID=$(granter_grant_id "$GRANTEE_ADDR" "GRANT_STATUS_PENDING")
    BEFORE=$(balance "$GRANTER_ADDR")
    if [ -n "$PENDING_ID" ] && tx_ok consent_grantee session decline-grant "$PENDING_ID" \
        && [ "$(( $(balance "$GRANTER_ADDR") - BEFORE ))" = "$DEPOSIT" ] \
        && [ -z "$(grant_status "$PENDING_ID")" ] \
        && [ "$(allowed_count "$GRANTEE_ADDR")" = "0" ]; then
        echo "  disallowed, existing sign-in untouched, new key pending, declined with refund, still not allowed"
        record_result "disallow-granter and decline" "PASS"
    else
        echo "  pending_id=$PENDING_ID log=$(tx_log) allowed=$(allowed_count "$GRANTEE_ADDR")"
        record_result "disallow-granter and decline" "FAIL"
    fi
else
    echo "  failed: $(tx_log) allowed=$(allowed_count "$GRANTEE_ADDR") signin=$(grant_status "$SIGNIN_ID")"
    record_result "disallow-granter and decline" "FAIL"
fi

# ---------------------------------------------------------------- TEST 5 ---
echo "--- TEST 5: a granter holds at most 4 pending grants; revoke frees a slot ---"
PENDING_IDS=()
OK=true
for I in 1 2 3 4; do
    cat > /tmp/consent_signin_$I.json <<JSON
{"sign_in":{"service":"s$I.example.test"}}
JSON
    if tx_ok session_granter session create-grant "$GRANTEE_ADDR" "$(future 24)" /tmp/consent_signin_$I.json; then
        PENDING_IDS+=("$(granter_grant_id "$GRANTEE_ADDR" "GRANT_STATUS_PENDING")")
    else
        echo "  pending $I refused: $(tx_log)"; OK=false
    fi
done
cat > /tmp/consent_signin_5.json <<JSON
{"sign_in":{"service":"s5.example.test"}}
JSON
if $OK && ! tx_ok session_granter session create-grant "$GRANTEE_ADDR" "$(future 24)" /tmp/consent_signin_5.json \
    && tx_log | grep -q "too many pending grants" \
    && tx_ok session_granter session revoke-grant "${PENDING_IDS[0]}" \
    && tx_ok session_granter session create-grant "$GRANTEE_ADDR" "$(future 24)" /tmp/consent_signin_5.json; then
    echo "  5th pending refused; after a revoke the 5th is accepted"
    record_result "per-granter pending cap" "PASS"
else
    echo "  failed: $(tx_log)"
    record_result "per-granter pending cap" "FAIL"
fi

# Cleanup: no pending grants left for the scripts that follow.
for ID in $($BINARY query session grants-by-granter "$GRANTER_ADDR" --output json 2>/dev/null \
    | jq -r '.grants[]? | select(.status=="GRANT_STATUS_PENDING") | .id'); do
    tx_ok session_granter session revoke-grant "$ID" > /dev/null || true
done
tx_ok consent_grantee session decline-grant "$SIGNIN_ID" > /dev/null || true
rm -f /tmp/consent_signin*.json

# -------------------------------------------------------------- results ---
echo "============================================"
echo "PENDING GRANT TEST RESULTS"
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
fi
echo ">>> ALL TESTS PASSED <<<"
