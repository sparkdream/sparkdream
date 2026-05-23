#!/bin/bash
# ============================================================================
# X/IDENTITY MODULE: SDK PARAMS ALIGNMENT TESTS
# ============================================================================
# The sentinel rewrite hook (§7.3) substitutes bond/dream denoms in SDK
# module params at chain start. This test verifies staking and mint params
# end up aligned with the identity-supplied bond_denom, so off-the-shelf
# clients (REST, gRPC, wallets) read the right denom in the conventional
# places.
# ============================================================================

set -e

BINARY="sparkdreamd"

echo "=================================================="
echo "TEST: x/identity SDK params alignment"
echo "=================================================="
echo ""

BOND_DENOM=$($BINARY q identity bond-denom --output json | jq -r '.denom')
echo "identity.bond_denom: $BOND_DENOM"

STAKING_BOND_DENOM=$($BINARY q staking params --output json | jq -r '.params.bond_denom // .bond_denom')
echo "staking.params.bond_denom: $STAKING_BOND_DENOM"
if [ "$STAKING_BOND_DENOM" != "$BOND_DENOM" ]; then
    echo "FAIL: staking.bond_denom $STAKING_BOND_DENOM != identity.bond_denom $BOND_DENOM"
    exit 1
fi
echo "PASS: staking.bond_denom aligned"
echo ""

MINT_DENOM=$($BINARY q mint params --output json | jq -r '.params.mint_denom // .mint_denom')
echo "mint.params.mint_denom: $MINT_DENOM"
if [ "$MINT_DENOM" != "$BOND_DENOM" ]; then
    echo "FAIL: mint.mint_denom $MINT_DENOM != identity.bond_denom $BOND_DENOM"
    exit 1
fi
echo "PASS: mint.mint_denom aligned"
echo ""

# Crisis module's constant_fee.denom should also be the identity bond denom.
CRISIS_DENOM=$($BINARY q crisis constant-fee --output json 2>/dev/null | jq -r '.constant_fee.denom // empty' || echo "")
if [ -n "$CRISIS_DENOM" ]; then
    echo "crisis.constant_fee.denom: $CRISIS_DENOM"
    if [ "$CRISIS_DENOM" != "$BOND_DENOM" ]; then
        echo "FAIL: crisis.constant_fee.denom $CRISIS_DENOM != identity.bond_denom $BOND_DENOM"
        exit 1
    fi
    echo "PASS: crisis.constant_fee.denom aligned"
else
    echo "SKIP: crisis module not present or constant_fee unset"
fi
echo ""

echo "=================================================="
echo "TEST PASSED: x/identity SDK params alignment"
echo "=================================================="
