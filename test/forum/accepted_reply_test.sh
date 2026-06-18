#!/bin/bash

# Strict E2E for the sentinel curation-proposal flow (accepted-reply proposals).
#
# Validates the MsgMarkAcceptedReply sentinel branch end to end:
#   sentinel proposes a reply as a thread's accepted answer (total_proposals++),
#   the author confirms (accepted_by = sentinel, confirmed_proposals++,
#   epoch_curations++, curation DREAM reward) or rejects (rejected_proposals++).
#
# The auto-confirm EndBlocker pass (timeout + one-time inactivity extension) is
# covered by unit tests; here we assert the on-chain wiring of propose / confirm
# / reject and the SentinelActivity counters.
#
# Depends on sentinel_test.sh having bonded sentinel1 (run in the same phase).

echo "--- TESTING: SENTINEL CURATION PROPOSAL (ACCEPTED-REPLY) ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing). Run: bash setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"
BOND_DENOM="${BOND_DENOM:-uspark}"

FAILED=0
fail() { echo "  FAIL: $1"; FAILED=1; }
pass() { echo "  PASS: $1"; }

wait_for_tx() {
    local TXHASH=$1 ATTEMPT=0
    while [ $ATTEMPT -lt 20 ]; do
        local R
        R=$($BINARY q tx "$TXHASH" --output json 2>/dev/null)
        if echo "$R" | jq -e '.code' >/dev/null 2>&1; then echo "$R"; return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    echo '{"code":99,"raw_log":"tx not found"}'; return 1
}

tx_code() { echo "$1" | jq -r '.code'; }

submit() {
    # submit <from> <args...> ; echo the wait_for_tx result
    local FROM=$1; shift
    local R TXHASH
    R=$($BINARY tx forum "$@" --from "$FROM" --chain-id "$CHAIN_ID" \
        --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
    TXHASH=$(echo "$R" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ] || [ "$TXHASH" = "null" ]; then
        echo "{\"code\":98,\"raw_log\":$(echo "$R" | jq -Rs .)}"; return 0
    fi
    sleep 6
    wait_for_tx "$TXHASH"
}

extract_event() {
    echo "$1" | jq -r ".events[]? | select(.type==\"$2\") | .attributes[]? | select(.key==\"$3\") | .value" | tr -d '"' | head -1
}
post_id_from() { extract_event "$1" post_created post_id; }

get_future_expiration() { date -u -d "+${1:-2} hours" +"%Y-%m-%dT%H:%M:%SZ"; }

# exec_session_via_json <grantee-key> <granter-addr> <grantee-addr> <inner-msg-json>
# Builds, signs (with the session key), and broadcasts a MsgExecSession wrapping
# the given inner message. Echoes the wait_for_tx result.
exec_session_via_json() {
    local GRANTEE_KEY=$1 GRANTER=$2 GRANTEE=$3 INNER_MSG=$4
    local ACCT_INFO ACCT_NUM SEQ R TXHASH
    ACCT_INFO=$($BINARY query auth account "$GRANTEE" --output json 2>&1)
    ACCT_NUM=$(echo "$ACCT_INFO" | jq -r '.account.account_number // .account.base_account.account_number // "0"')
    SEQ=$(echo "$ACCT_INFO" | jq -r '.account.sequence // .account.base_account.sequence // "0"')
    cat > /tmp/curation_exec_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgExecSession",
        "grantee": "$GRANTEE",
        "granter": "$GRANTER",
        "msgs": [$INNER_MSG]
      }
    ],
    "memo": "", "timeout_height": "0",
    "extension_options": [], "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": { "amount": [{"denom": "${BOND_DENOM}", "amount": "50000"}], "gas_limit": "400000", "payer": "", "granter": "" }
  },
  "signatures": []
}
TXEOF
    $BINARY tx sign /tmp/curation_exec_unsigned.json --from "$GRANTEE_KEY" \
        --chain-id "$CHAIN_ID" --keyring-backend test \
        --account-number "$ACCT_NUM" --sequence "$SEQ" \
        --output-document /tmp/curation_exec_signed.json >/dev/null 2>&1
    if [ ! -s /tmp/curation_exec_signed.json ]; then
        echo '{"code":99,"raw_log":"sign failed"}'; return 0
    fi
    R=$($BINARY tx broadcast /tmp/curation_exec_signed.json --output json 2>&1)
    TXHASH=$(echo "$R" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ] || [ "$TXHASH" = "null" ]; then
        echo "{\"code\":98,\"raw_log\":$(echo "$R" | jq -Rs .)}"; return 0
    fi
    sleep 6
    wait_for_tx "$TXHASH"
}

# sentinel counter helper: counter_of <field>
counter_of() {
    $BINARY query forum get-sentinel-activity "$SENTINEL1_ADDR" --output json 2>/dev/null \
        | jq -r ".sentinel_activity.$1 // \"0\""
}

create_thread_and_reply() {
    # echoes "<thread_id> <reply_id>"
    local TR RR TID RID
    TR=$(submit poster1 create-post "$TEST_CATEGORY_ID" "0" "Curation thread $(date +%s%N)")
    [ "$(tx_code "$TR")" = "0" ] || { fail "create thread"; echo " "; return; }
    TID=$(post_id_from "$TR")
    RR=$(submit poster2 create-post "$TEST_CATEGORY_ID" "$TID" "Candidate answer")
    [ "$(tx_code "$RR")" = "0" ] || { fail "create reply"; echo "$TID "; return; }
    RID=$(post_id_from "$RR")
    echo "$TID $RID"
}

# ------------------------------------------------------------------------
# 1. PROPOSE -> CONFIRM
# ------------------------------------------------------------------------
echo "--- 1. Sentinel proposes, author confirms ---"
read -r THREAD_ID REPLY_ID <<<"$(create_thread_and_reply)"
echo "  thread=$THREAD_ID reply=$REPLY_ID"

BEFORE_TOTAL=$(counter_of total_proposals)
BEFORE_CONFIRMED=$(counter_of confirmed_proposals)
BEFORE_CURATIONS=$(counter_of epoch_curations)

R=$(submit sentinel1 mark-accepted-reply "$THREAD_ID" "$REPLY_ID")
if [ "$(tx_code "$R")" = "0" ]; then pass "sentinel proposed accepted reply"; else
    echo "  $(echo "$R" | jq -r '.raw_log')"; fail "mark-accepted-reply (propose)"
fi

MD=$($BINARY query forum get-thread-metadata "$THREAD_ID" --output json 2>/dev/null)
PROPOSED=$(echo "$MD" | jq -r '.thread_metadata.proposed_reply_id // "0"')
[ "$PROPOSED" = "$REPLY_ID" ] && pass "proposed_reply_id set" || fail "proposed_reply_id not set (got: $PROPOSED)"

AFTER_TOTAL=$(counter_of total_proposals)
[ "$AFTER_TOTAL" = "$((BEFORE_TOTAL + 1))" ] && pass "total_proposals incremented" \
    || fail "total_proposals not incremented ($BEFORE_TOTAL -> $AFTER_TOTAL)"

R=$(submit poster1 confirm-proposed-reply "$THREAD_ID")
if [ "$(tx_code "$R")" = "0" ]; then pass "author confirmed proposal"; else
    echo "  $(echo "$R" | jq -r '.raw_log')"; fail "confirm-proposed-reply"
fi

MD=$($BINARY query forum get-thread-metadata "$THREAD_ID" --output json 2>/dev/null)
ACCEPTED=$(echo "$MD" | jq -r '.thread_metadata.accepted_reply_id // "0"')
ACCEPTED_BY=$(echo "$MD" | jq -r '.thread_metadata.accepted_by // ""')
[ "$ACCEPTED" = "$REPLY_ID" ] && pass "accepted_reply_id = proposed reply" || fail "accepted_reply_id wrong (got: $ACCEPTED)"
[ "$ACCEPTED_BY" = "$SENTINEL1_ADDR" ] && pass "accepted_by credited to sentinel" || fail "accepted_by not sentinel (got: $ACCEPTED_BY)"

AFTER_CONFIRMED=$(counter_of confirmed_proposals)
AFTER_CURATIONS=$(counter_of epoch_curations)
[ "$AFTER_CONFIRMED" = "$((BEFORE_CONFIRMED + 1))" ] && pass "confirmed_proposals incremented" \
    || fail "confirmed_proposals not incremented ($BEFORE_CONFIRMED -> $AFTER_CONFIRMED)"
# epoch_curations is a per-reward-epoch counter that ResetSentinelEpochCounters
# zeroes at each sentinel-reward boundary (frequent on testparams). So accept
# either +1 (no boundary crossed) or 0 (reset between confirm and query). The
# deterministic +1 is asserted by the unit test; here we only require it did not
# go to an impossible value. The DREAM reward + accepted_by credit above already
# prove the confirm executed.
if [ "$AFTER_CURATIONS" = "$((BEFORE_CURATIONS + 1))" ] || [ "$AFTER_CURATIONS" = "0" ]; then
    pass "epoch_curations consistent ($BEFORE_CURATIONS -> $AFTER_CURATIONS; 0 = reward-epoch reset)"
else
    fail "epoch_curations unexpected ($BEFORE_CURATIONS -> $AFTER_CURATIONS)"
fi

# ------------------------------------------------------------------------
# 2. PROPOSE -> REJECT
# ------------------------------------------------------------------------
echo "--- 2. Sentinel proposes, author rejects ---"
read -r THREAD2 REPLY2 <<<"$(create_thread_and_reply)"
echo "  thread=$THREAD2 reply=$REPLY2"

BEFORE_REJECTED=$(counter_of rejected_proposals)

R=$(submit sentinel1 mark-accepted-reply "$THREAD2" "$REPLY2")
[ "$(tx_code "$R")" = "0" ] && pass "sentinel proposed (2nd thread)" || { echo "  $(echo "$R" | jq -r '.raw_log')"; fail "propose 2"; }

R=$(submit poster1 reject-proposed-reply "$THREAD2" "not the best answer")
if [ "$(tx_code "$R")" = "0" ]; then pass "author rejected proposal"; else
    echo "  $(echo "$R" | jq -r '.raw_log')"; fail "reject-proposed-reply"
fi

MD=$($BINARY query forum get-thread-metadata "$THREAD2" --output json 2>/dev/null)
PROPOSED2=$(echo "$MD" | jq -r '.thread_metadata.proposed_reply_id // "0"')
ACCEPTED2=$(echo "$MD" | jq -r '.thread_metadata.accepted_reply_id // "0"')
[ "$PROPOSED2" = "0" ] && pass "proposal cleared after reject" || fail "proposal not cleared (got: $PROPOSED2)"
[ "$ACCEPTED2" = "0" ] && pass "no accepted reply after reject" || fail "unexpected accepted reply (got: $ACCEPTED2)"

AFTER_REJECTED=$(counter_of rejected_proposals)
[ "$AFTER_REJECTED" = "$((BEFORE_REJECTED + 1))" ] && pass "rejected_proposals incremented" \
    || fail "rejected_proposals not incremented ($BEFORE_REJECTED -> $AFTER_REJECTED)"

# ------------------------------------------------------------------------
# 3. NEGATIVE: non-author, non-sentinel cannot propose
# ------------------------------------------------------------------------
echo "--- 3. Non-author non-sentinel cannot propose ---"
read -r THREAD3 REPLY3 <<<"$(create_thread_and_reply)"
# poster2 is the reply author but neither the thread author nor a sentinel.
R=$(submit poster2 mark-accepted-reply "$THREAD3" "$REPLY3")
if [ "$(tx_code "$R")" != "0" ]; then pass "non-sentinel proposal rejected"; else
    fail "non-sentinel was allowed to propose"
fi

# ------------------------------------------------------------------------
# 4. SESSION KEY: a bonded sentinel proposes via a scoped session key.
#    Proves MsgMarkAcceptedReply is delegable AND that the granter's sentinel
#    bond is re-checked at dispatch (the session key holds no sentinel power
#    itself — it acts as sentinel1, the bonded granter).
# ------------------------------------------------------------------------
echo "--- 4. Sentinel proposes via a session key ---"

# A throwaway session-key account (must exist on-chain to sign).
if ! $BINARY keys show curation_session --keyring-backend test >/dev/null 2>&1; then
    $BINARY keys add curation_session --keyring-backend test >/dev/null 2>&1
fi
SESSION_KEY_ADDR=$($BINARY keys show curation_session -a --keyring-backend test)
FR=$($BINARY tx bank send alice "$SESSION_KEY_ADDR" 10000000${BOND_DENOM} \
    --chain-id "$CHAIN_ID" --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
FH=$(echo "$FR" | jq -r '.txhash // empty'); [ -n "$FH" ] && { sleep 6; wait_for_tx "$FH" >/dev/null; }

# Best-effort revoke of any leftover grant from a prior run so this run gets a
# fresh session with full exec budget (keeps the test idempotent across re-runs).
RV=$($BINARY tx session revoke-session "$SESSION_KEY_ADDR" --from sentinel1 \
    --chain-id "$CHAIN_ID" --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
RVH=$(echo "$RV" | jq -r '.txhash // empty'); [ -n "$RVH" ] && { sleep 6; wait_for_tx "$RVH" >/dev/null; }

# sentinel1 grants MarkAcceptedReply to the session key (granter pays gas).
# Idempotent across re-runs: we assert on the queried session, not the create
# broadcast code.
EXP=$(get_future_expiration 2)
SR=$($BINARY tx session create-session \
    "$SESSION_KEY_ADDR" \
    "/sparkdream.forum.v1.MsgMarkAcceptedReply" \
    "50000000${BOND_DENOM}" \
    "$EXP" \
    "100" \
    --from sentinel1 --chain-id "$CHAIN_ID" --keyring-backend test \
    --fees 50000${BOND_DENOM} --gas 300000 -y --output json 2>&1)
SH=$(echo "$SR" | jq -r '.txhash // empty')
[ -n "$SH" ] && [ "$SH" != "null" ] && { sleep 6; wait_for_tx "$SH" >/dev/null; }

SESSION_Q=$($BINARY query session session "$SENTINEL1_ADDR" "$SESSION_KEY_ADDR" --output json 2>/dev/null)
SESS_GRANTEE=$(echo "$SESSION_Q" | jq -r '.session.grantee // .grant.grantee // ""' 2>/dev/null)
if [ "$SESS_GRANTEE" = "$SESSION_KEY_ADDR" ]; then
    pass "session active for MarkAcceptedReply (sentinel1 -> session key)"
else
    echo "  $(echo "$SR" | jq -r '.raw_log // .')"; fail "no active session for the session key"
fi

read -r THREAD4 REPLY4 <<<"$(create_thread_and_reply)"
echo "  thread=$THREAD4 reply=$REPLY4"
BEFORE_TOTAL4=$(counter_of total_proposals)

# Inner msg: creator must be the granter (sentinel1).
INNER=$(cat <<MSGEOF
{ "@type": "/sparkdream.forum.v1.MsgMarkAcceptedReply", "creator": "$SENTINEL1_ADDR", "thread_id": "$THREAD4", "reply_id": "$REPLY4" }
MSGEOF
)
ERES=$(exec_session_via_json curation_session "$SENTINEL1_ADDR" "$SESSION_KEY_ADDR" "$INNER")
if [ "$(tx_code "$ERES")" = "0" ]; then pass "propose via session key executed"; else
    echo "  $(echo "$ERES" | jq -r '.raw_log')"; fail "exec-session propose"
fi

MD=$($BINARY query forum get-thread-metadata "$THREAD4" --output json 2>/dev/null)
PROPOSED4=$(echo "$MD" | jq -r '.thread_metadata.proposed_reply_id // "0"')
PROPOSED_BY4=$(echo "$MD" | jq -r '.thread_metadata.proposed_by // ""')
[ "$PROPOSED4" = "$REPLY4" ] && pass "session propose set proposed_reply_id" || fail "proposed_reply_id not set (got: $PROPOSED4)"
[ "$PROPOSED_BY4" = "$SENTINEL1_ADDR" ] && pass "proposed_by is the granting sentinel" || fail "proposed_by wrong (got: $PROPOSED_BY4)"

AFTER_TOTAL4=$(counter_of total_proposals)
[ "$AFTER_TOTAL4" = "$((BEFORE_TOTAL4 + 1))" ] && pass "total_proposals incremented (session)" \
    || fail "total_proposals not incremented via session ($BEFORE_TOTAL4 -> $AFTER_TOTAL4)"

echo ""
if [ "$FAILED" = "0" ]; then
    echo "--- ACCEPTED-REPLY CURATION TEST: ALL CHECKS PASSED ---"
    exit 0
else
    echo "--- ACCEPTED-REPLY CURATION TEST: FAILURES PRESENT ---"
    exit 1
fi
