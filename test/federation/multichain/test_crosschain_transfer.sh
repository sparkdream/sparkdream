#!/bin/bash
# ------------------------------------------------------------------
# Cross-Chain ICS-20 Transfer Tests
#
# Tokens moving between the two chains over the `transfer` channel the
# relayer opened beside the federation channel, and the voucher metadata
# x/federation pre-registers for them.
#
#   1. A -> B: alice's SPARK arrives on chain-b as the voucher whose trace is
#      chain-b's end of the TRANSFER channel
#   2. that voucher carries the metadata MsgRegisterPeer wrote from
#      ibc_transfer_channel_id + peer_identity (symbol "<peer>.ibc"), and
#      nothing was written under the federation channel's trace
#   3. B -> A, the other direction on the same channel
#   4. the voucher returns home: sending it back to chain-a unwinds to the
#      native denom (escrow released, no double voucher)
#
# Skips (passing) when no transfer channel exists: the host-hermes suite
# (setup_ibc.sh) opens only the federation channel. Run the suite with
# --relayer=container to exercise it.
#
# Prerequisites: chains running, peers set up by setup_peers.sh, relayer up.
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/lib_multichain.sh"

PASS_COUNT=0; FAIL_COUNT=0; SKIP_COUNT=0; RESULTS=(); TEST_NAMES=()

echo "--- TESTING: CROSS-CHAIN ICS-20 TRANSFER ---"
echo ""

[ -f "$IBC_ENV" ] && source "$IBC_ENV"
if [ -z "${TRANSFER_CHANNEL_A:-}" ] || [ -z "${TRANSFER_CHANNEL_B:-}" ]; then
    echo "  SKIP: no transfer channel (run the suite with --relayer=container)"
    exit 0
fi
echo "  transfer channel: A=$TRANSFER_CHANNEL_A  B=$TRANSFER_CHANNEL_B"
echo "  federation channel: A=$CHANNEL_A  B=$CHANNEL_B"
echo ""

# ibc/<SHA256(port/channel/base) upper hex>, as ibc-go's Denom.IBCDenom()
voucher() {  # <receiving-side channel> <base denom>
    echo "ibc/$(printf '%s' "transfer/$1/$2" | sha256sum | cut -d' ' -f1 | tr 'a-f' 'A-F')"
}
balance_of() {  # <a|b> <address> <denom>
    qcli_$1 bank balances "$2" | jq -r --arg d "$3" '[.balances[]? | select(.denom == $d) | .amount][0] // "0"'
}
metadata_symbol() {  # <a|b> <denom>
    qcli_$1 bank denom-metadata "$2" | jq -r '.metadata.symbol // empty'
}
transfer() {  # <a|b> <channel> <to> <amount+denom> <label>
    local TX
    TX=$(cli_$1 tx ibc-transfer transfer transfer "$2" "$3" "$4" \
        --from alice -y --fees 5000${BOND_DENOM} --output json)
    submit_and_wait_$1 "$TX" "$5"
}

ALICE_A=$(keys_a show alice -a)
ALICE_B=$(keys_b show alice -a)
SYMBOL_A=$(qcli_a identity chain-identity | jq -r '.identity.bond_display_symbol')
SYMBOL_B=$(qcli_b identity chain-identity | jq -r '.identity.bond_display_symbol')
AMOUNT=12345

# ====================================================================
# TEST 1 + 2: A -> B, voucher denom and its metadata
# ====================================================================
echo "--- TEST 1: transfer A -> B ---"
VOUCHER_ON_B=$(voucher "$TRANSFER_CHANNEL_B" "$BOND_DENOM")
BEFORE=$(balance_of b "$ALICE_B" "$VOUCHER_ON_B")
if transfer a "$TRANSFER_CHANNEL_A" "$ALICE_B" "${AMOUNT}${BOND_DENOM}" "transfer A->B" \
    && wait_for_ibc_delivery \
        "qcli_b bank balances $ALICE_B" \
        ".balances | map(select(.denom == \"$VOUCHER_ON_B\")) | .[0].amount // \"0\" | tonumber > $BEFORE" \
        90; then
    GOT=$(( $(balance_of b "$ALICE_B" "$VOUCHER_ON_B") - BEFORE ))
    echo "  chain-b received $GOT of $VOUCHER_ON_B"
    if [ "$GOT" -eq "$AMOUNT" ]; then record_result "Transfer A->B" "PASS"; else record_result "Transfer A->B" "FAIL"; fi
else
    record_result "Transfer A->B" "FAIL"
fi

echo "--- TEST 2: voucher metadata keyed on the transfer channel ---"
SYM=$(metadata_symbol b "$VOUCHER_ON_B")
echo "  metadata symbol on chain-b for $VOUCHER_ON_B: '${SYM}' (expected ${SYMBOL_A}.ibc)"
if [ "$SYM" = "${SYMBOL_A}.ibc" ]; then
    WRONG=""
    if [ "$CHANNEL_B" != "$TRANSFER_CHANNEL_B" ]; then
        WRONG=$(metadata_symbol b "$(voucher "$CHANNEL_B" "$BOND_DENOM")")
    fi
    if [ -z "$WRONG" ]; then
        record_result "Voucher metadata on transfer channel" "PASS"
    else
        echo "  metadata also written under the federation channel's trace: '$WRONG'"
        record_result "Voucher metadata on transfer channel" "FAIL"
    fi
else
    record_result "Voucher metadata on transfer channel" "FAIL"
fi

# ====================================================================
# TEST 3: B -> A
# ====================================================================
echo "--- TEST 3: transfer B -> A ---"
VOUCHER_ON_A=$(voucher "$TRANSFER_CHANNEL_A" "$BOND_DENOM")
BEFORE=$(balance_of a "$ALICE_A" "$VOUCHER_ON_A")
if transfer b "$TRANSFER_CHANNEL_B" "$ALICE_A" "${AMOUNT}${BOND_DENOM}" "transfer B->A" \
    && wait_for_ibc_delivery \
        "qcli_a bank balances $ALICE_A" \
        ".balances | map(select(.denom == \"$VOUCHER_ON_A\")) | .[0].amount // \"0\" | tonumber > $BEFORE" \
        90; then
    SYM=$(metadata_symbol a "$VOUCHER_ON_A")
    echo "  chain-a voucher $VOUCHER_ON_A, metadata symbol '${SYM}'"
    if [ "$SYM" = "${SYMBOL_B}.ibc" ]; then record_result "Transfer B->A" "PASS"; else record_result "Transfer B->A" "FAIL"; fi
else
    record_result "Transfer B->A" "FAIL"
fi

# ====================================================================
# TEST 4: the voucher goes home and unwinds to the native denom
# ====================================================================
echo "--- TEST 4: voucher returns B -> A as native ---"
NATIVE_BEFORE=$(balance_of a "$ALICE_A" "$BOND_DENOM")
VOUCHER_BEFORE=$(balance_of b "$ALICE_B" "$VOUCHER_ON_B")
if transfer b "$TRANSFER_CHANNEL_B" "$ALICE_A" "${AMOUNT}${VOUCHER_ON_B}" "return voucher B->A" \
    && wait_for_ibc_delivery \
        "qcli_a bank balances $ALICE_A" \
        ".balances | map(select(.denom == \"$BOND_DENOM\")) | .[0].amount | tonumber > $NATIVE_BEFORE" \
        90; then
    VOUCHER_AFTER=$(balance_of b "$ALICE_B" "$VOUCHER_ON_B")
    echo "  chain-b voucher $VOUCHER_BEFORE -> $VOUCHER_AFTER; chain-a native rose"
    if [ $((VOUCHER_BEFORE - VOUCHER_AFTER)) -eq "$AMOUNT" ]; then
        record_result "Voucher unwinds to native" "PASS"
    else
        record_result "Voucher unwinds to native" "FAIL"
    fi
else
    record_result "Voucher unwinds to native" "FAIL"
fi

print_summary "CROSS-CHAIN TRANSFER TEST RESULTS"
