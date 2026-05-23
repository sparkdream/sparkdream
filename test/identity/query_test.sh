#!/bin/bash
# ============================================================================
# X/IDENTITY MODULE: QUERY TESTS
# ============================================================================
# Validates the three read-only queries exposed by x/identity:
#   - chain-identity   full record
#   - bond-denom       convenience accessor
#   - dream-denom      convenience accessor
#
# Identity is genesis-only immutable, so the test does not attempt to mutate;
# it only asserts the chain returns the values that were placed in genesis.
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"

echo "=================================================="
echo "TEST: x/identity queries"
echo "=================================================="
echo ""

# ----------------------------------------------------------------------
# 1. chain-identity
# ----------------------------------------------------------------------
echo "[1/3] query identity chain-identity"
IDENTITY_JSON=$($BINARY q identity chain-identity --output json)
echo "$IDENTITY_JSON" | jq .

BOND_DENOM_FROM_FULL=$(echo "$IDENTITY_JSON" | jq -r '.identity.bond_denom')
DREAM_DENOM_FROM_FULL=$(echo "$IDENTITY_JSON" | jq -r '.identity.dream_denom')

if [ -z "$BOND_DENOM_FROM_FULL" ] || [ "$BOND_DENOM_FROM_FULL" = "null" ]; then
    echo "FAIL: chain-identity returned empty bond_denom"
    exit 1
fi
if [ -z "$DREAM_DENOM_FROM_FULL" ] || [ "$DREAM_DENOM_FROM_FULL" = "null" ]; then
    echo "FAIL: chain-identity returned empty dream_denom"
    exit 1
fi
echo "PASS: chain-identity returned bond_denom=$BOND_DENOM_FROM_FULL, dream_denom=$DREAM_DENOM_FROM_FULL"
echo ""

# ----------------------------------------------------------------------
# 2. bond-denom convenience query
# ----------------------------------------------------------------------
echo "[2/3] query identity bond-denom"
BOND_DENOM=$($BINARY q identity bond-denom --output json | jq -r '.denom')
echo "bond_denom: $BOND_DENOM"

if [ "$BOND_DENOM" != "$BOND_DENOM_FROM_FULL" ]; then
    echo "FAIL: convenience bond-denom ($BOND_DENOM) != chain-identity bond_denom ($BOND_DENOM_FROM_FULL)"
    exit 1
fi
echo "PASS: bond-denom matches chain-identity"
echo ""

# ----------------------------------------------------------------------
# 3. dream-denom convenience query
# ----------------------------------------------------------------------
echo "[3/3] query identity dream-denom"
DREAM_DENOM=$($BINARY q identity dream-denom --output json | jq -r '.denom')
echo "dream_denom: $DREAM_DENOM"

if [ "$DREAM_DENOM" != "$DREAM_DENOM_FROM_FULL" ]; then
    echo "FAIL: convenience dream-denom ($DREAM_DENOM) != chain-identity dream_denom ($DREAM_DENOM_FROM_FULL)"
    exit 1
fi
echo "PASS: dream-denom matches chain-identity"
echo ""

# ----------------------------------------------------------------------
# 4. Cross-check shape constraints
# ----------------------------------------------------------------------
echo "[4/4] x/identity record shape"

# bond_denom must match the strict regex u[a-z]{2,5}\.[a-z][a-z0-9-]{2,15}
# (relaxed from {2,4} so bond-symbols of length 5 like "spark" fit; see
# docs/x-identity-implementation-decisions.md §M2)
if ! echo "$BOND_DENOM" | grep -qE '^u[a-z]{2,5}\.[a-z][a-z0-9-]{2,15}$'; then
    echo "FAIL: bond_denom $BOND_DENOM does not match the identity regex"
    exit 1
fi
# dream_denom: udream\.<chain>
if ! echo "$DREAM_DENOM" | grep -qE '^udream\.[a-z][a-z0-9-]{2,15}$'; then
    echo "FAIL: dream_denom $DREAM_DENOM does not match the identity regex"
    exit 1
fi
echo "PASS: denom shapes are strict"
echo ""

echo "=================================================="
echo "TEST PASSED: x/identity queries"
echo "=================================================="
