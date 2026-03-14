#!/bin/bash

# ============================================================================
# PATCH GENESIS FOR TLE-ENABLED ENCRYPTED BATCH TESTING
# ============================================================================
#
# This script patches ~/.sparkdream/config/genesis.json with pre-generated
# TLE key material so that x/shield encrypted batch mode works without
# requiring a multi-validator DKG ceremony.
#
# Usage:
#   1. ignite chain build && ignite chain init
#   2. bash test/shield/patch_genesis_tle.sh
#   3. sparkdreamd start --home ~/.sparkdream
#
# The script:
#   - Queries the validator operator address from genesis
#   - Generates TLE key material using tools/zk/cmd/seed-tle
#   - Patches shield genesis state: encrypted_batch_enabled, tle_key_set,
#     decryption_keys, shield_epoch_state, min_tle_validators=1
#   - Saves the key file for later use by encrypted_batch_test.sh
#
# Prerequisites:
#   - Chain initialized (genesis.json exists)
#   - Go build tools available (go run)
#   - Chain NOT running (we're modifying genesis)
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$SCRIPT_DIR/../.."
GENESIS="$HOME/.sparkdream/config/genesis.json"
KEY_FILE="$SCRIPT_DIR/.tle_keys.json"
NUM_EPOCHS=10

echo "============================================================================"
echo "  PATCHING GENESIS FOR TLE-ENABLED ENCRYPTED BATCH MODE"
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
# 2. Extract validator operator address from genesis
# ========================================================================
echo "  Extracting validator address from genesis..."

# The validator operator address is in the staking genesis state
VALOPER_ADDR=$(jq -r '.app_state.staking.validators[0].operator_address // empty' "$GENESIS")

if [ -z "$VALOPER_ADDR" ]; then
    echo "ERROR: No validator found in genesis"
    exit 1
fi

echo "  Validator: $VALOPER_ADDR"

# ========================================================================
# 3. Generate TLE key material
# ========================================================================
echo "  Generating TLE key material ($NUM_EPOCHS epochs)..."

cd "$PROJECT_ROOT"
go run ./tools/zk/cmd/seed-tle keygen \
    --validator-addr="$VALOPER_ADDR" \
    --epochs="$NUM_EPOCHS" \
    --output="$KEY_FILE" 2>&1

if [ ! -f "$KEY_FILE" ]; then
    echo "ERROR: Key generation failed"
    exit 1
fi

echo "  Key material saved to: $KEY_FILE"

# ========================================================================
# 4. Read key material for genesis patching
# ========================================================================
MASTER_PUB_B64=$(jq -r '.master_public_key_b64' "$KEY_FILE")
PUB_SHARE_B64=$(jq -r '.public_share_b64' "$KEY_FILE")
SHARE_INDEX=$(jq -r '.share_index' "$KEY_FILE")

echo ""
echo "  Patching genesis.json..."

# ========================================================================
# 5. Build the jq patch
# ========================================================================
# Build decryption_keys array
DEC_KEYS_JSON="["
for epoch in $(seq 0 $((NUM_EPOCHS - 1))); do
    DEC_KEY_B64=$(jq -r ".decryption_keys.\"$epoch\".decryption_key_b64" "$KEY_FILE")
    if [ "$epoch" -gt 0 ]; then
        DEC_KEYS_JSON="$DEC_KEYS_JSON,"
    fi
    DEC_KEYS_JSON="$DEC_KEYS_JSON{\"epoch\":\"$epoch\",\"decryption_key\":\"$DEC_KEY_B64\"}"
done
DEC_KEYS_JSON="$DEC_KEYS_JSON]"

# ========================================================================
# 6. Apply the patch using jq
# ========================================================================
# We patch:
# - shield.params.encrypted_batch_enabled = true
# - shield.params.min_tle_validators = 1 (single validator)
# - shield.tle_key_set = { master_public_key, threshold 1/1, validator_shares }
# - shield.decryption_keys = [ pre-computed keys for epochs 0..N-1 ]
# - shield.shield_epoch_state = { current_epoch: 0, epoch_start_height: 1 }

TMP_GENESIS="${GENESIS}.tmp"

jq --arg mpk "$MASTER_PUB_B64" \
   --arg valaddr "$VALOPER_ADDR" \
   --arg pubshare "$PUB_SHARE_B64" \
   --argjson sidx "$SHARE_INDEX" \
   --argjson deckeys "$DEC_KEYS_JSON" \
   '
   # Enable encrypted batch mode
   .app_state.shield.params.encrypted_batch_enabled = true |
   # Lower min_tle_validators to 1 for single-validator testing
   .app_state.shield.params.min_tle_validators = 1 |

   # Set TLE key set with single validator share
   .app_state.shield.tle_key_set = {
     "master_public_key": $mpk,
     "threshold_numerator": "1",
     "threshold_denominator": "1",
     "validator_shares": [{
       "validator_address": $valaddr,
       "public_share": $pubshare,
       "share_index": $sidx
     }],
     "created_at_height": "0"
   } |

   # Pre-seed decryption keys for first N epochs
   .app_state.shield.decryption_keys = $deckeys |

   # Initialize epoch state
   .app_state.shield.shield_epoch_state = {
     "current_epoch": "0",
     "epoch_start_height": "1"
   }
   ' "$GENESIS" > "$TMP_GENESIS"

if [ $? -ne 0 ]; then
    echo "ERROR: jq patch failed"
    rm -f "$TMP_GENESIS"
    exit 1
fi

mv "$TMP_GENESIS" "$GENESIS"

echo ""
echo "  Genesis patched successfully:"
echo "    - encrypted_batch_enabled: true"
echo "    - min_tle_validators: 1"
echo "    - TLE key set: 1 validator, threshold 1/1"
echo "    - Decryption keys: epochs 0-$((NUM_EPOCHS - 1))"
echo "    - Epoch state: epoch 0, start height 1"

# ========================================================================
# 7. Verify the patch
# ========================================================================
echo ""
echo "  Verifying patch..."

BATCH_ENABLED=$(jq -r '.app_state.shield.params.encrypted_batch_enabled' "$GENESIS")
TLE_MPK=$(jq -r '.app_state.shield.tle_key_set.master_public_key' "$GENESIS")
DK_COUNT=$(jq -r '.app_state.shield.decryption_keys | length' "$GENESIS")
EPOCH_STATE=$(jq -r '.app_state.shield.shield_epoch_state.current_epoch' "$GENESIS")

if [ "$BATCH_ENABLED" != "true" ]; then
    echo "ERROR: encrypted_batch_enabled not set"
    exit 1
fi
if [ -z "$TLE_MPK" ] || [ "$TLE_MPK" == "null" ]; then
    echo "ERROR: TLE master public key not set"
    exit 1
fi
if [ "$DK_COUNT" -lt 1 ]; then
    echo "ERROR: No decryption keys in genesis"
    exit 1
fi

echo "    encrypted_batch_enabled: $BATCH_ENABLED"
echo "    TLE master public key: ${TLE_MPK:0:20}..."
echo "    Decryption keys: $DK_COUNT"
echo "    Current epoch: $EPOCH_STATE"

echo ""
echo "============================================================================"
echo "  GENESIS PATCH COMPLETE"
echo "============================================================================"
echo ""
echo "  Next steps:"
echo "    sparkdreamd start --home ~/.sparkdream"
echo ""
echo "  Key file for tests: $KEY_FILE"
echo ""
