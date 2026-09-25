#!/bin/bash
# ------------------------------------------------------------------
# Report, per chain in $RELAYER_DIR/relayer.json, whether the relayer
# key is ready to sign. Containerised port of deploy/relayer/fund_check.sh.
#
# Three checks, because each fails differently:
#   key      hermes has the key          -> otherwise it cannot sign at all
#   balance  gas denom balance > 0       -> in that chain's own gas denom
#   account  auth has a BaseAccount      -> bank.SendCoins does NOT create
#            one, so a freshly funded address can still make hermes abort
#            with "account <addr> not found". Checked only when the chain
#            entry carries an "lcd" URL; null otherwise.
#
# Import keys first (relayer-bringup does, or this script does if
# mnemonics are waiting). Read-only against the chains.
#
# Prints a JSON array: [{chain, address, balance, denom, account, ready}]
# Exits 0 when every chain is ready, 1 otherwise.
# ------------------------------------------------------------------
set -uo pipefail

RELAYER_DIR="${RELAYER_DIR:-/data/relayer}"
CONFIG="$RELAYER_DIR/config.toml"
MANIFEST="$RELAYER_DIR/relayer.json"

hj() {
    local line
    line=$(hermes --json --config "$CONFIG" "$@" 2>/dev/null \
        | jq -c 'select(type == "object" and has("status"))' 2>/dev/null | tail -1)
    [ -n "$line" ] && [ "$(jq -r '.status' <<<"$line")" = "success" ] || return 1
    jq -c '.result' <<<"$line"
}

out="[]"
all=1
for chain in $(jq -r '.chains[].id' "$MANIFEST"); do
    lcd=$(jq -r --arg c "$chain" '.chains[] | select(.id == $c) | .lcd // empty' "$MANIFEST")
    addr=$(hj keys list --chain "$chain" | jq -r '[.. | strings | select(test("^[a-z]+1[02-9ac-hj-np-z]{38,}$"))][0] // empty')
    bal=$(hj keys balance --chain "$chain" | jq -r '.amount // empty' 2>/dev/null)
    denom=$(hj keys balance --chain "$chain" | jq -r '.denom // empty' 2>/dev/null)
    account=null
    if [ -n "$lcd" ] && [ -n "$addr" ]; then
        code=$(curl -s -o /dev/null -w '%{http_code}' -m 15 "$lcd/cosmos/auth/v1beta1/accounts/$addr")
        if [ "$code" = "200" ]; then account=true; else account=false; fi
    fi
    ready=true
    if [ -z "$addr" ] || [ -z "$bal" ] || [ "$bal" = "0" ] || [ "$account" = "false" ]; then
        ready=false
        all=0
    fi
    out=$(jq -c --arg c "$chain" --arg a "$addr" --arg b "${bal:-0}" --arg d "$denom" \
        --argjson acct "$account" --argjson r "$ready" \
        '. + [{chain: $c, address: $a, balance: $b, denom: $d, account: $acct, ready: $r}]' <<<"$out")
done

jq . <<<"$out"
[ "$all" -eq 1 ]
