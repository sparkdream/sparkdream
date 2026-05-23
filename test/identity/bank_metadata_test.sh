#!/bin/bash
# ============================================================================
# X/IDENTITY MODULE: BANK METADATA TESTS
# ============================================================================
# Asserts that x/identity correctly seeded x/bank DenomMetadata for both
# native tokens (SPARK and DREAM) at genesis. See docs/x-identity-spec.md §8.
# ============================================================================

set -e

BINARY="sparkdreamd"

echo "=================================================="
echo "TEST: x/identity bank metadata"
echo "=================================================="
echo ""

BOND_DENOM=$($BINARY q identity bond-denom --output json | jq -r '.denom')
DREAM_DENOM=$($BINARY q identity dream-denom --output json | jq -r '.denom')
IDENTITY=$($BINARY q identity chain-identity --output json | jq -r '.identity')
BOND_SYMBOL=$(echo "$IDENTITY" | jq -r '.bond_display_symbol')
DREAM_SYMBOL=$(echo "$IDENTITY" | jq -r '.dream_display_symbol')

echo "Checking bank metadata for $BOND_DENOM (Symbol=$BOND_SYMBOL)"
# The CLI's `q bank denom-metadata <denom>` returns a single `metadata` object;
# the legacy `metadatas[]` listing query is `denom-metadata-by-query-string`.
SPARK_META=$($BINARY q bank denom-metadata "$BOND_DENOM" --output json 2>/dev/null | jq '.metadata // empty')
if [ -z "$SPARK_META" ] || [ "$SPARK_META" = "null" ]; then
    echo "FAIL: no bank metadata found for $BOND_DENOM"
    exit 1
fi
echo "$SPARK_META" | jq .

GOT_SYMBOL=$(echo "$SPARK_META" | jq -r '.symbol')
if [ "$GOT_SYMBOL" != "$BOND_SYMBOL" ]; then
    echo "FAIL: bank metadata Symbol $GOT_SYMBOL != identity bond_display_symbol $BOND_SYMBOL"
    exit 1
fi
echo "PASS: SPARK metadata present and consistent"
echo ""

echo "Checking bank metadata for $DREAM_DENOM (Symbol=$DREAM_SYMBOL)"
DREAM_META=$($BINARY q bank denom-metadata "$DREAM_DENOM" --output json 2>/dev/null | jq '.metadata // empty')
if [ -z "$DREAM_META" ] || [ "$DREAM_META" = "null" ]; then
    echo "FAIL: no bank metadata found for $DREAM_DENOM"
    exit 1
fi
echo "$DREAM_META" | jq .

GOT_DREAM_SYMBOL=$(echo "$DREAM_META" | jq -r '.symbol')
if [ "$GOT_DREAM_SYMBOL" != "$DREAM_SYMBOL" ]; then
    echo "FAIL: bank metadata Symbol $GOT_DREAM_SYMBOL != identity dream_display_symbol $DREAM_SYMBOL"
    exit 1
fi
# DREAM metadata description must call out non-transferability per spec §8.2
GOT_DESC=$(echo "$DREAM_META" | jq -r '.description')
if ! echo "$GOT_DESC" | grep -q "non-transferable"; then
    echo "FAIL: DREAM metadata description does not mention non-transferability"
    echo "Description was: $GOT_DESC"
    exit 1
fi
echo "PASS: DREAM metadata present, consistent, and notes non-transferability"
echo ""

# Legacy literals must not be present.
echo "Checking that legacy denom metadata was purged at genesis"
for LEGACY in uspark dream stake udream; do
    if [ "$LEGACY" = "$BOND_DENOM" ] || [ "$LEGACY" = "$DREAM_DENOM" ]; then
        continue
    fi
    # Single-denom query: empty stdout (or .metadata=null) means "no metadata
    # registered for this denom" — exactly what we want for legacy literals.
    LEGACY_META=$($BINARY q bank denom-metadata "$LEGACY" --output json 2>/dev/null | jq '.metadata // empty')
    if [ -n "$LEGACY_META" ] && [ "$LEGACY_META" != "null" ] && [ "$LEGACY_META" != "{}" ]; then
        # An empty {} object means the chain has no metadata for this denom;
        # only treat it as a failure if non-trivial fields are present.
        HAS_BASE=$(echo "$LEGACY_META" | jq -r '.base // empty')
        if [ -n "$HAS_BASE" ]; then
            echo "FAIL: legacy metadata for $LEGACY survived genesis purge"
            echo "$LEGACY_META"
            exit 1
        fi
    fi
done
echo "PASS: legacy metadata absent"
echo ""

echo "=================================================="
echo "TEST PASSED: x/identity bank metadata"
echo "=================================================="
