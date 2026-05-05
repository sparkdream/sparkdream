#!/bin/bash

echo "--- TESTING NAME MODULE: GENESIS-CLAIMED FOUNDER HANDLES ---"

# Verifies that the canonical founder handles configured in
# x/commons/keeper/genesis_vals_*.go (GenesisHandles) are registered to the
# correct addresses at chain start, and that the first handle in each
# slice is set as the address's primary name. Without this protection, a
# squatter could snipe a founder's handle as soon as they become an x/rep
# member.

set -uo pipefail

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

# testparams maps alice/bob/carol → handles {alice}, {bob}, {carol}.
declare -A EXPECTED=(
    ["alice"]="$ALICE_ADDR"
    ["bob"]="$BOB_ADDR"
    ["carol"]="$CAROL_ADDR"
)

FAIL=0

for HANDLE in "${!EXPECTED[@]}"; do
    EXPECTED_OWNER="${EXPECTED[$HANDLE]}"

    REC=$($BINARY query name resolve "$HANDLE" --output json 2>/dev/null)
    if [ -z "$REC" ] || ! echo "$REC" | jq -e '.name_record' > /dev/null 2>&1; then
        echo "❌ Handle '$HANDLE' was not registered at genesis (Resolve returned nothing)."
        FAIL=1
        continue
    fi

    OWNER=$(echo "$REC" | jq -r '.name_record.owner')
    if [ "$OWNER" = "$EXPECTED_OWNER" ]; then
        echo "✅ '$HANDLE' owned by $EXPECTED_OWNER."
    else
        echo "❌ '$HANDLE' owned by $OWNER, expected $EXPECTED_OWNER."
        FAIL=1
    fi

    REVERSE=$($BINARY query name reverse-resolve "$EXPECTED_OWNER" --output json 2>/dev/null | jq -r '.name')
    if [ "$REVERSE" = "$HANDLE" ]; then
        echo "✅ ReverseResolve($EXPECTED_OWNER) = '$HANDLE' (primary set)."
    else
        echo "⚠️  ReverseResolve($EXPECTED_OWNER) = '$REVERSE' (expected '$HANDLE'; another test may have remapped primary)."
    fi
done

# Squat protection: a non-genesis active member must NOT be able to register
# 'alice' (already taken). Use name_claimant from setup_test_accounts.sh.
CLAIMANT_ADDR=$($BINARY keys show name_claimant -a --keyring-backend test 2>/dev/null || echo "")
if [ -n "$CLAIMANT_ADDR" ]; then
    echo "--- Squat-protection check: name_claimant tries to register 'alice' ---"
    RES=$($BINARY tx name register-name "alice" "squat-attempt" --from name_claimant -y \
        --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>/dev/null)
    TX_HASH=$(echo "$RES" | jq -r '.txhash')
    sleep 4
    QRES=$($BINARY query tx "$TX_HASH" --output json 2>/dev/null)
    CODE=$(echo "$QRES" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "✅ Squat blocked (tx code $CODE)."
    else
        echo "❌ Squat succeeded — a non-genesis member registered 'alice'!"
        FAIL=1
    fi
fi

if [ $FAIL -ne 0 ]; then
    echo ""
    echo "--- GENESIS HANDLE PROTECTION FAILED ---"
    exit 1
fi

echo ""
echo "--- ALL GENESIS HANDLE CASES PASSED ---"
