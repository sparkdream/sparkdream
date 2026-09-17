#!/bin/bash
# ------------------------------------------------------------------
# Verify both relayer keys are ready before bringup.sh runs.
#
# Checks three things per chain, because each fails differently:
#
#   1. hermes knows the key       -> otherwise it cannot sign at all
#   2. the address has a balance  -> in that chain's own denom, which differs
#                                    between the two chains
#   3. auth has a BaseAccount     -> bank.SendCoins does NOT create one, so a
#                                    freshly funded address exists in bank state
#                                    while `query auth account` returns NotFound.
#                                    Hermes needs the account for its sequence
#                                    number and aborts with
#                                    "account <addr> not found". A self-send
#                                    signed by the key itself materialises it
#                                    from the tx's own pubkey.
#
# Read-only: it never signs or sends anything.
#
# Usage: ./fund_check.sh
# ------------------------------------------------------------------
set -uo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
HERMES="${HERMES:-hermes}"
HERMES_CONFIG="${HERMES_CONFIG:-$SCRIPT_DIR/hermes_config.toml}"

# key_name | chain_id | lcd | denom | minimum whole tokens we consider "enough"
ROWS="
relayer-dev|sparkdream-dev-1|https://api-dev.sparkdream.io|usparz.sparkdreamdev|1
relayer-test|sparkdream-test-1|https://api-test.sparkdream.io|uspark.sparkdreamtest|1
"

READY=1

printf "%-14s %-18s %-14s %-12s %s\n" KEY CHAIN BALANCE AUTH-ACCOUNT STATUS
printf -- "------------------------------------------------------------------------------\n"

while IFS='|' read -r NAME CHAIN LCD DENOM MIN; do
    [ -z "${NAME:-}" ] && continue

    ADDR=$("$HERMES" --config "$HERMES_CONFIG" keys list --chain "$CHAIN" 2>/dev/null \
        | grep -oE 'sprkdrm1[a-z0-9]+' | head -1)
    if [ -z "$ADDR" ]; then
        printf "%-14s %-18s %-14s %-12s %s\n" "$NAME" "$CHAIN" "-" "-" "NO KEY (run ./keys.sh)"
        READY=0
        continue
    fi

    BAL=$(curl -sf -m 15 "$LCD/cosmos/bank/v1beta1/balances/$ADDR" 2>/dev/null \
        | python3 -c "
import sys, json
try:
    b = json.load(sys.stdin).get('balances', [])
except Exception:
    print('?'); raise SystemExit
print(next((c['amount'] for c in b if c['denom'] == '$DENOM'), '0'))
" 2>/dev/null)
    BAL="${BAL:-?}"

    # 404 here is the NotFound case described above, not a transport error.
    ACCT_CODE=$(curl -s -o /dev/null -w '%{http_code}' -m 15 \
        "$LCD/cosmos/auth/v1beta1/accounts/$ADDR" 2>/dev/null)
    if [ "$ACCT_CODE" = "200" ]; then ACCT="yes"; else ACCT="NO"; fi

    if [ "$BAL" = "?" ]; then
        WHOLE="?"; ENOUGH=0
    else
        WHOLE=$(python3 -c "print(f'{int($BAL)/1e6:,.2f}')" 2>/dev/null || echo "?")
        ENOUGH=$(python3 -c "print(1 if int($BAL) >= $MIN*10**6 else 0)" 2>/dev/null || echo 0)
    fi

    if [ "$ENOUGH" = "1" ] && [ "$ACCT" = "yes" ]; then
        STATUS="ready"
    elif [ "$ENOUGH" != "1" ]; then
        STATUS="UNDERFUNDED (need >= $MIN)"
        READY=0
    else
        STATUS="needs a self-send to create the auth account"
        READY=0
    fi

    printf "%-14s %-18s %-14s %-12s %s\n" "$NAME" "$CHAIN" "$WHOLE" "$ACCT" "$STATUS"
    printf "%-14s %s\n" "" "$ADDR"
done <<< "$ROWS"

echo ""
if [ "$READY" = "1" ]; then
    echo "Both relayers are ready. Next: ./bringup.sh"
    exit 0
fi
echo "Not ready. Fund the addresses above in their own denom (they differ), and"
echo "send one self-transfer from each key so auth materialises the account."
exit 1
