#!/bin/bash

# x/session create-recurring-pull CLI: a RECURRING_PULL grant created from
# the command line (autocli cannot build the payload oneof), queried,
# refused before its first period, and revoked.

echo "--- TESTING: RECURRING PULL CLI ---"
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

pass() { echo "  [ OK ] $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "  [FAIL] $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
check() { if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (expected '$3', got '$2')"; fi; }

wait_for_tx() {
    local hash=$1 attempt=0 res
    while [ $attempt -lt 30 ]; do
        res=$($BINARY q tx "$hash" --output json 2>/dev/null)
        if echo "$res" | jq -e '.code' > /dev/null 2>&1; then
            echo "$res"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    echo '{"code": -1, "raw_log": "tx not found"}'
}

# submit <args...>: runs `tx session <args>` and echoes the delivered tx.
submit() {
    local res hash
    res=$($BINARY tx session "$@" --chain-id "$CHAIN_ID" --keyring-backend test \
        --gas 500000 --fees 60000"$BOND_DENOM" -y --output json 2>&1)
    hash=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$hash" ]; then
        echo "{\"code\": -1, \"raw_log\": $(echo "$res" | jq -Rs .)}"
        return
    fi
    if [ "$(echo "$res" | jq -r '.code // 0')" != "0" ]; then
        echo "$res"
        return
    fi
    wait_for_tx "$hash"
}

echo "=== Create through the CLI ==="
TX=$(submit create-recurring-pull "$GRANTEE1_ADDR" "1000000${BOND_DENOM}" 86400 +240h --grant-note "cli rent" --from session_granter)
check "create-recurring-pull delivers" "$(echo "$TX" | jq -r '.code')" "0"
GRANT=$(echo "$TX" | jq -r '[.events[] | select(.type=="grant_created") | .attributes[] | select(.key=="id") | .value] | first // empty')
check "a grant id came back" "$([ -n "$GRANT" ] && echo yes || echo no)" "yes"
G=$($BINARY query session grant "$GRANT" --output json 2>/dev/null)
check "the grant is a RecurringPull" "$(echo "$G" | jq -r '.grant.type')" "GRANT_TYPE_RECURRING_PULL"
check "it is active" "$(echo "$G" | jq -r '.grant.status')" "GRANT_STATUS_ACTIVE"
check "the amount per period" "$(echo "$G" | jq -r '.grant.Payload.value.recurring_pull.amount_per_period.amount')" "1000000"
check "the period" "$(echo "$G" | jq -r '.grant.Payload.value.recurring_pull.period_seconds')" "86400"
check "max_per_epoch defaults to the amount" "$(echo "$G" | jq -r '.grant.Payload.value.recurring_pull.max_per_epoch')" "1000000"
check "the note" "$(echo "$G" | jq -r '.grant.note')" "cli rent"

echo ""
echo "=== Validation ==="
MIN_PERIOD=$($BINARY query session params --output json 2>/dev/null | jq -r '.params.min_recurring_period_seconds')
TX=$(submit create-recurring-pull "$GRANTEE1_ADDR" "1000000${BOND_DENOM}" $((MIN_PERIOD - 1)) +240h --from session_granter)
check "a period below the minimum is refused" "$(echo "$TX" | jq -r 'if .code == 0 then "accepted" else "refused" end')" "refused"
OUT=$($BINARY tx session create-recurring-pull "$GRANTEE1_ADDR" "1000000${BOND_DENOM}" 86400 tomorrow --from session_granter \
    --chain-id "$CHAIN_ID" --keyring-backend test -y 2>&1)
check "a malformed expiry is refused locally" "$(echo "$OUT" | grep -q "RFC 3339" && echo refused || echo accepted)" "refused"

echo ""
echo "=== Claim and revoke ==="
TX=$(submit claim-recurring-pull "$GRANT" --from session_grantee1)
check "a claim before the first period is refused" "$(echo "$TX" | jq -r '.raw_log // ""' | grep -qi "not due\|next claim" && echo refused || echo other)" "refused"
TX=$(submit revoke-grant "$GRANT" --from session_granter)
check "the granter revokes it" "$(echo "$TX" | jq -r '.code')" "0"
check "the grant is gone or revoked" \
    "$( ($BINARY query session grant "$GRANT" --output json 2>/dev/null || echo '{}') | jq -r '.grant.status // "GONE"' | grep -qE 'REVOKED|GONE' && echo yes || echo no)" "yes"

echo ""
echo "  Passed: $PASS_COUNT  Failed: $FAIL_COUNT"
[ "$FAIL_COUNT" -eq 0 ]
