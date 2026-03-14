#!/bin/bash

# ============================================================================
# PATCH GENESIS FOR LIVE DKG CEREMONY TESTING
# ============================================================================
#
# This script patches ~/.sparkdream/config/genesis.json to allow the DKG
# ceremony to auto-trigger and complete in a single-validator environment.
#
# Unlike patch_genesis_tle.sh (which seeds TLE keys directly), this script
# ONLY lowers min_tle_validators to 1. The DKG ceremony must then complete
# naturally via vote extensions over ~20 blocks.
#
# Usage:
#   1. ignite chain build && ignite chain init
#   2. bash test/shield/patch_genesis_dkg.sh
#   3. sparkdreamd start --home ~/.sparkdream
#
# The script:
#   - Sets min_tle_validators = 1 (allows single-validator DKG)
#   - Does NOT seed any TLE keys or decryption keys
#   - Does NOT enable encrypted_batch_enabled (DKG completion does this)
#
# Prerequisites:
#   - Chain initialized (genesis.json exists)
#   - Chain NOT running (we're modifying genesis)
#   - Vote extensions enabled at height 1 (config.yml)
#
# Mutually exclusive with patch_genesis_tle.sh — use one or the other.
# ============================================================================

set -e

GENESIS="$HOME/.sparkdream/config/genesis.json"

echo "============================================================================"
echo "  PATCHING GENESIS FOR LIVE DKG CEREMONY"
echo "============================================================================"
echo ""

# ========================================================================
# 1. Verify genesis exists and chain is not running
# ========================================================================
if [ ! -f "$GENESIS" ]; then
    echo "ERROR: Genesis file not found at $GENESIS"
    echo "  Run: ignite chain build && ignite chain init"
    exit 1
fi

if sparkdreamd status &> /dev/null; then
    echo "ERROR: Chain appears to be running. Stop it before patching genesis."
    exit 1
fi

echo "  Genesis file: $GENESIS"

# ========================================================================
# 2. Patch shield params: only min_tle_validators
# ========================================================================
echo "  Patching min_tle_validators = 1..."

TMP_GENESIS="${GENESIS}.tmp"

jq '
   # Lower min_tle_validators to 1 for single-validator DKG
   .app_state.shield.params.min_tle_validators = 1
   ' "$GENESIS" > "$TMP_GENESIS"

if [ $? -ne 0 ]; then
    echo "ERROR: jq patch failed"
    rm -f "$TMP_GENESIS"
    exit 1
fi

mv "$TMP_GENESIS" "$GENESIS"

# ========================================================================
# 3. Verify the patch
# ========================================================================
echo ""
echo "  Verifying patch..."

MIN_TLE=$(jq -r '.app_state.shield.params.min_tle_validators' "$GENESIS")
BATCH_ENABLED=$(jq -r '.app_state.shield.params.encrypted_batch_enabled' "$GENESIS")
DKG_WINDOW=$(jq -r '.app_state.shield.params.dkg_window_blocks' "$GENESIS")

echo "    min_tle_validators: $MIN_TLE"
echo "    encrypted_batch_enabled: $BATCH_ENABLED (should be false — DKG will enable it)"
echo "    dkg_window_blocks: $DKG_WINDOW"

if [ "$MIN_TLE" != "1" ]; then
    echo "ERROR: min_tle_validators not set to 1"
    exit 1
fi

echo ""
echo "============================================================================"
echo "  GENESIS PATCH COMPLETE"
echo "============================================================================"
echo ""
echo "  DKG will auto-trigger when the chain starts (1 bonded validator >= min 1)."
echo "  DKG timeline (~$DKG_WINDOW blocks = ~$((DKG_WINDOW * 6))s at 6s/block):"
echo "    Block N:    REGISTERING phase begins"
echo "    Block N+$((DKG_WINDOW / 2)): CONTRIBUTING phase begins"
echo "    Block N+$DKG_WINDOW: ACTIVE phase (DKG complete)"
echo ""
echo "  Next steps:"
echo "    sparkdreamd start --home ~/.sparkdream"
echo ""
