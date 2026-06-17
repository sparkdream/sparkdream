#!/bin/bash

# Strict E2E for the sentinel reply-pin dispute flow.
#
# Validates the REPLY_PIN moderation-appeal wiring end to end:
#   sentinel pins a (permanent) reply -> thread author disputes -> the dispute is
#   folded into x/rep's unified GovActionAppeal path (ActionType REPLY_PIN,
#   ActionTarget = reply id) and the pin record is marked disputed.
#
# Resolution (jury / committee) is covered by unit tests; here we assert the
# on-chain wiring a dispute produces.

echo "--- TESTING: SENTINEL REPLY-PIN DISPUTE (REPLY_PIN GOV ACTION APPEAL) ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
BOND_DENOM="${BOND_DENOM:-uspark}"

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

post_id_from() { extract_event "$1" post_created post_id; }
extract_event() {
    echo "$1" | jq -r ".events[]? | select(.type==\"$2\") | .attributes[]? | select(.key==\"$3\") | .value" | tr -d '"' | head -1
}

# ------------------------------------------------------------------------
# 1. Thread (poster1) + reply (poster2)
# ------------------------------------------------------------------------
echo "--- 1. Create thread + reply ---"
R=$(submit poster1 create-post "$TEST_CATEGORY_ID" "0" "Pin-dispute thread $(date +%s)")
[ "$(tx_code "$R")" = "0" ] || fail "create thread"
THREAD_ID=$(post_id_from "$R")
[ -z "$THREAD_ID" ] && THREAD_ID=$($BINARY query forum list-post --output json 2>/dev/null | jq -r '.post[-1].id // empty')
echo "  thread=$THREAD_ID"

R=$(submit poster2 create-post "$TEST_CATEGORY_ID" "$THREAD_ID" "Reply to be pinned")
[ "$(tx_code "$R")" = "0" ] || fail "create reply"
REPLY_ID=$(post_id_from "$R")
[ -z "$REPLY_ID" ] && REPLY_ID=$($BINARY query forum list-post --output json 2>/dev/null | jq -r '.post[-1].id // empty')
echo "  reply=$REPLY_ID"

# ------------------------------------------------------------------------
# 2. Make the reply permanent (pins require permanent replies). alice is a
#    high-trust genesis member and may promote another member's post.
# ------------------------------------------------------------------------
echo "--- 2. Make reply permanent ---"
R=$(submit alice make-post-permanent "$REPLY_ID")
if [ "$(tx_code "$R")" = "0" ]; then pass "reply made permanent"; else
    echo "  $(echo "$R" | jq -r '.raw_log')"; fail "make-post-permanent"
fi

# ------------------------------------------------------------------------
# 3. Sentinel pins the reply; verify the pin record.
# ------------------------------------------------------------------------
echo "--- 3. Sentinel pins reply ---"
R=$(submit sentinel1 pin-reply "$THREAD_ID" "$REPLY_ID")
if [ "$(tx_code "$R")" = "0" ]; then pass "sentinel pinned reply"; else
    echo "  $(echo "$R" | jq -r '.raw_log')"; fail "pin-reply"
fi

MD=$($BINARY query forum get-thread-metadata "$THREAD_ID" --output json 2>/dev/null)
IS_SENTINEL=$(echo "$MD" | jq -r --argjson rid "$REPLY_ID" '.thread_metadata.pinned_records[]? | select((.post_id|tonumber)==$rid) | .is_sentinel_pin' 2>/dev/null)
[ "$IS_SENTINEL" = "true" ] && pass "pin record is_sentinel_pin=true" || fail "pin record missing / not sentinel pin (got: $IS_SENTINEL)"

# ------------------------------------------------------------------------
# 4. Thread author disputes the pin (funded for the 10 SPARK appeal bond).
# ------------------------------------------------------------------------
echo "--- 4. Thread author disputes the pin ---"
FR=$($BINARY tx bank send alice "$POSTER1_ADDR" 20000000${BOND_DENOM} --chain-id "$CHAIN_ID" \
    --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
FH=$(echo "$FR" | jq -r '.txhash // empty'); [ -n "$FH" ] && { sleep 6; wait_for_tx "$FH" >/dev/null; }

R=$(submit poster1 dispute-pin "$THREAD_ID" "$REPLY_ID" "pin is off-topic")
if [ "$(tx_code "$R")" = "0" ]; then pass "dispute filed"; else
    echo "  $(echo "$R" | jq -r '.raw_log')"; fail "dispute-pin"
fi

# ------------------------------------------------------------------------
# 5. A REPLY_PIN GovActionAppeal must now exist for this reply (PENDING).
# ------------------------------------------------------------------------
echo "--- 5. Verify REPLY_PIN gov action appeal created ---"
APPEALS=$($BINARY query rep list-gov-action-appeal --output json 2>/dev/null)
MATCH=$(echo "$APPEALS" | jq -r --arg rid "$REPLY_ID" '
    .gov_action_appeal[]? | select(.action_target==$rid) |
    "\(.action_type)|\(.status)"' 2>/dev/null | head -1)
echo "  matched appeal (action_type|status): ${MATCH:-<none>}"
if echo "$MATCH" | grep -qiE "REPLY_PIN|(^|[|])8[|]"; then
    pass "REPLY_PIN appeal created for reply $REPLY_ID"
else
    fail "no REPLY_PIN gov action appeal for reply $REPLY_ID"
fi
if echo "$MATCH" | grep -qiE "PENDING|[|]1$"; then
    pass "appeal status PENDING"
else
    fail "appeal not PENDING (got: $MATCH)"
fi

# ------------------------------------------------------------------------
# 6. The pin record must be marked disputed.
# ------------------------------------------------------------------------
echo "--- 6. Verify pin record marked disputed ---"
MD2=$($BINARY query forum get-thread-metadata "$THREAD_ID" --output json 2>/dev/null)
DISPUTED=$(echo "$MD2" | jq -r --argjson rid "$REPLY_ID" '.thread_metadata.pinned_records[]? | select((.post_id|tonumber)==$rid) | .disputed' 2>/dev/null)
[ "$DISPUTED" = "true" ] && pass "pin record disputed=true" || fail "pin record not disputed (got: $DISPUTED)"

echo ""
if [ "$FAILED" = "0" ]; then
    echo "--- PIN DISPUTE TEST: ALL CHECKS PASSED ---"
    exit 0
else
    echo "--- PIN DISPUTE TEST: FAILURES PRESENT ---"
    exit 1
fi
