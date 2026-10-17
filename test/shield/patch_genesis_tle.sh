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
#   - Patches shield genesis state: encrypted_batch_enabled, tle_key_set
#     (G1 + G2 master keys, one validator share with its G2 twin),
#     decryption_keys, shield_epoch_state, min_tle_validators=1, and a TLE
#     liveness window the seeded validator cannot exhaust (it holds no DKG
#     key store, so it never serves a share and every demanded epoch is a
#     miss for it; with the default tolerance the EndBlocker would jail the
#     chain's only validator after a handful of batches)
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
# SPARKDREAM_HOME overrides the chain home (test/run_parallel.sh patches a
# per-suite home); `sparkdreamd` on PATH must then point at the same home.
GENESIS="${SPARKDREAM_HOME:-$HOME/.sparkdream}/config/genesis.json"
KEY_FILE="$SCRIPT_DIR/.tle_keys.json"
# Blocks run ~1s apart in the test build (10-block epochs roll every ~10s), so
# seed enough epoch keys for a long session; a key is 64 bytes.
NUM_EPOCHS=3000

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

# `ignite chain init` genesis carries the validator in its gentx (genutil),
# not in staking.validators; accept either.
VALOPER_ADDR=$(jq -r '.app_state.staking.validators[0].operator_address // empty' "$GENESIS")
if [ -z "$VALOPER_ADDR" ]; then
    VALOPER_ADDR=$(jq -r '.app_state.genutil.gen_txs[0].body.messages[0].validator_address // empty' "$GENESIS")
fi

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
MASTER_PUB_G2_B64=$(jq -r '.master_public_key_g2_b64' "$KEY_FILE")
PUB_SHARE_B64=$(jq -r '.public_share_b64' "$KEY_FILE")
PUB_SHARE_G2_B64=$(jq -r '.public_share_g2_b64' "$KEY_FILE")
SHARE_INDEX=$(jq -r '.share_index' "$KEY_FILE")

echo ""
echo "  Patching genesis.json..."

# ========================================================================
# 5. Build the jq patch
# ========================================================================
# Build the decryption_keys array in a file: thousands of keys do not fit
# in a jq argument (E2BIG), so they are read with --slurpfile.
DEC_KEYS_FILE="${GENESIS}.deckeys.json"
jq --argjson n "$NUM_EPOCHS" '[.decryption_keys | to_entries[] | select((.key|tonumber) < $n)
   | {epoch: .key, decryption_key: .value.decryption_key_b64}] | sort_by(.epoch|tonumber)' "$KEY_FILE" > "$DEC_KEYS_FILE"

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
   --arg mpkg2 "$MASTER_PUB_G2_B64" \
   --arg valaddr "$VALOPER_ADDR" \
   --arg pubshare "$PUB_SHARE_B64" \
   --arg pubshareg2 "$PUB_SHARE_G2_B64" \
   --argjson sidx "$SHARE_INDEX" \
   --slurpfile deckeys "$DEC_KEYS_FILE" \
   '
   # Enable encrypted batch mode
   .app_state.shield.params.encrypted_batch_enabled = true |
   # Lower min_tle_validators to 1 for single-validator testing
   .app_state.shield.params.min_tle_validators = 1 |
   # The seeded validator has no local DKG share, so it misses every
   # demanded epoch: keep the liveness judge from jailing the only validator.
   .app_state.shield.params.tle_miss_window = "100000" |
   .app_state.shield.params.tle_miss_tolerance = "100000" |

   # Set TLE key set with single validator share
   # Clients encrypt to master_public_key_g2; decryption shares are
   # verified against public_share_g2 (see tools/tle).
   .app_state.shield.tle_key_set = {
     "master_public_key": $mpk,
     "master_public_key_g2": $mpkg2,
     "threshold_numerator": "1",
     "threshold_denominator": "1",
     "validator_shares": [{
       "validator_address": $valaddr,
       "public_share": $pubshare,
       "share_index": $sidx,
       "public_share_g2": $pubshareg2
     }],
     "created_at_height": "0"
   } |

   # Pre-seed decryption keys for first N epochs
   .app_state.shield.decryption_keys = $deckeys[0] |

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
rm -f "$DEC_KEYS_FILE"

echo ""
echo "  Genesis patched successfully:"
echo "    - encrypted_batch_enabled: true"
echo "    - min_tle_validators: 1"
echo "    - tle_miss_window/tolerance: 100000 (seeded validator never serves shares)"
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
