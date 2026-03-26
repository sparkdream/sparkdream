#!/bin/bash
#
# sparkdream-arweave-upload.sh
#
# Uploads archived block batches to Arweave for permanent storage using arkb.
# Tracks which files have already been uploaded to avoid re-uploading.
# Maintains a manifest file mapping block ranges to Arweave transaction IDs.
#
# Prerequisites:
#   1. Install Node.js 18+ and arkb:
#      npm install -g arkb
#
#   2. Have an Arweave wallet JSON file with AR tokens:
#      Export from ArConnect or generate with arweave-js
#
# Usage:
#   ./sparkdream-arweave-upload.sh -w /path/to/arweave-wallet.json [archive_directory]
#
# Environment variables (all optional):
#   ARCHIVE_DIR       - Directory containing .jsonl.gz files (default: ./sparkdream-archives)
#   MANIFEST_FILE     - Path to the Arweave manifest (default: $ARCHIVE_DIR/arweave-manifest.csv)
#   UPLOADED_FILE     - Tracks already-uploaded files (default: $ARCHIVE_DIR/.arweave-uploaded)
#   ARWEAVE_WALLET    - Path to wallet JSON (alternative to -w flag)
#   ARWEAVE_GATEWAY   - Gateway URL (default: https://arweave.net)
#   USE_BUNDLER       - Bundler service URL (recommended for reliability)
#   FEE_MULTIPLIER    - Fee multiplier for faster confirmation (default: 1)
#   DRY_RUN           - Set to "true" to show what would be uploaded without uploading
#
set -e

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
WALLET_PATH=""

while getopts "w:" opt; do
    case $opt in
        w) WALLET_PATH="$OPTARG" ;;
        *) echo "Usage: $0 -w <wallet_path> [archive_directory]" >&2; exit 1 ;;
    esac
done
shift $((OPTIND - 1))

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
ARCHIVE_DIR="${1:-${ARCHIVE_DIR:-./sparkdream-archives}}"
MANIFEST_FILE="${MANIFEST_FILE:-${ARCHIVE_DIR}/arweave-manifest.csv}"
UPLOADED_FILE="${UPLOADED_FILE:-${ARCHIVE_DIR}/.arweave-uploaded}"
WALLET_PATH="${WALLET_PATH:-${ARWEAVE_WALLET:-}}"
ARWEAVE_GATEWAY="${ARWEAVE_GATEWAY:-https://arweave.net}"
FEE_MULTIPLIER="${FEE_MULTIPLIER:-1}"

# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------
if ! command -v arkb >/dev/null 2>&1; then
    echo "ERROR: 'arkb' CLI is not installed." >&2
    echo "Install it with: npm install -g arkb" >&2
    exit 1
fi

if [ -z "$WALLET_PATH" ]; then
    echo "ERROR: Arweave wallet path is required." >&2
    echo "Usage: $0 -w /path/to/arweave-wallet.json [archive_directory]" >&2
    echo "Or set ARWEAVE_WALLET environment variable." >&2
    exit 1
fi

if [ ! -f "$WALLET_PATH" ]; then
    echo "ERROR: Wallet file not found: $WALLET_PATH" >&2
    exit 1
fi

if [ ! -d "$ARCHIVE_DIR" ]; then
    echo "ERROR: Archive directory not found: $ARCHIVE_DIR" >&2
    exit 1
fi

# Check wallet balance
echo "Checking Arweave wallet balance..."
BALANCE_OUTPUT=$(arkb balance --wallet "$WALLET_PATH" --gateway "$ARWEAVE_GATEWAY" 2>&1) || true
echo "  $BALANCE_OUTPUT"
echo ""

# Initialize manifest with header if it doesn't exist
if [ ! -f "$MANIFEST_FILE" ]; then
    echo "file,from_block,to_block,tx_id,arweave_url,file_size_bytes,uploaded_at" > "$MANIFEST_FILE"
    echo "Created manifest: $MANIFEST_FILE"
fi

# Initialize uploaded tracker
touch "$UPLOADED_FILE"

# ---------------------------------------------------------------------------
# Build arkb options
# ---------------------------------------------------------------------------
ARKB_OPTS="--wallet $WALLET_PATH --gateway $ARWEAVE_GATEWAY --auto-confirm"
ARKB_OPTS="$ARKB_OPTS --content-type application/gzip"
ARKB_OPTS="$ARKB_OPTS --fee-multiplier $FEE_MULTIPLIER"

# Add custom tags for discoverability
ARKB_OPTS="$ARKB_OPTS --tag-name App-Name --tag-value sparkdream-block-archive"
ARKB_OPTS="$ARKB_OPTS --tag-name Chain-ID --tag-value sparkdream-1"

if [ -n "$USE_BUNDLER" ]; then
    ARKB_OPTS="$ARKB_OPTS --use-bundler $USE_BUNDLER"
fi

# ---------------------------------------------------------------------------
# Find and upload new archive files
# ---------------------------------------------------------------------------
UPLOAD_COUNT=0
SKIP_COUNT=0
FAIL_COUNT=0

# Sort files by block range for orderly processing
for ARCHIVE_FILE in $(ls "${ARCHIVE_DIR}"/blocks_*.jsonl.gz 2>/dev/null | sort -t_ -k2 -n); do
    FILENAME=$(basename "$ARCHIVE_FILE")

    # Skip if already uploaded
    if grep -qF "$FILENAME" "$UPLOADED_FILE" 2>/dev/null; then
        SKIP_COUNT=$(( SKIP_COUNT + 1 ))
        continue
    fi

    # Extract block range from filename (blocks_1_to_10000.jsonl.gz)
    FROM_BLOCK=$(echo "$FILENAME" | sed 's/blocks_\([0-9]*\)_to_.*/\1/')
    TO_BLOCK=$(echo "$FILENAME" | sed 's/blocks_[0-9]*_to_\([0-9]*\)\.jsonl\.gz/\1/')

    FILE_SIZE=$(stat -f%z "$ARCHIVE_FILE" 2>/dev/null || stat -c%s "$ARCHIVE_FILE" 2>/dev/null)
    FILE_SIZE_HUMAN=$(du -h "$ARCHIVE_FILE" | cut -f1)

    echo "Uploading: $FILENAME ($FILE_SIZE_HUMAN) [blocks ${FROM_BLOCK}-${TO_BLOCK}]"

    if [ "${DRY_RUN}" = "true" ]; then
        echo "  [DRY RUN] Would upload $FILENAME"
        continue
    fi

    # Upload to Arweave with block range tags
    UPLOAD_OUTPUT=$(arkb deploy "$ARCHIVE_FILE" \
        $ARKB_OPTS \
        --tag-name Block-Range-From --tag-value "$FROM_BLOCK" \
        --tag-name Block-Range-To --tag-value "$TO_BLOCK" \
        2>&1) || {
        echo "  ERROR: Upload failed for $FILENAME" >&2
        echo "  Output: $UPLOAD_OUTPUT" >&2
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
        continue
    }

    # Extract transaction ID from arkb output
    # arkb outputs the URL like https://arweave.net/<TX_ID>
    TX_ID=$(echo "$UPLOAD_OUTPUT" | grep -oE '[a-zA-Z0-9_-]{43}' | head -1)

    if [ -z "$TX_ID" ]; then
        echo "  WARNING: Could not extract TX ID from upload output" >&2
        echo "  Output: $UPLOAD_OUTPUT" >&2
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
        continue
    fi

    ARWEAVE_URL="${ARWEAVE_GATEWAY}/${TX_ID}"
    TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Record in manifest
    echo "${FILENAME},${FROM_BLOCK},${TO_BLOCK},${TX_ID},${ARWEAVE_URL},${FILE_SIZE},${TIMESTAMP}" >> "$MANIFEST_FILE"

    # Mark as uploaded
    echo "$FILENAME" >> "$UPLOADED_FILE"

    echo "  TX ID: $TX_ID"
    echo "  URL:   $ARWEAVE_URL"

    UPLOAD_COUNT=$(( UPLOAD_COUNT + 1 ))
done

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "========================================"
echo "Arweave upload complete"
echo "  New uploads:  $UPLOAD_COUNT"
echo "  Skipped:      $SKIP_COUNT"
echo "  Failed:       $FAIL_COUNT"
echo "  Manifest:     $MANIFEST_FILE"
echo "========================================"

if [ "$UPLOAD_COUNT" -gt 0 ] || [ "$SKIP_COUNT" -gt 0 ]; then
    echo ""
    echo "Manifest contents:"
    echo ""
    column -t -s',' "$MANIFEST_FILE" 2>/dev/null || cat "$MANIFEST_FILE"
fi

# ---------------------------------------------------------------------------
# Verify pending transactions
# ---------------------------------------------------------------------------
if [ "$UPLOAD_COUNT" -gt 0 ]; then
    echo ""
    echo "Note: Arweave transactions take ~10-20 minutes to be mined."
    echo "Check status with:"
    echo "  arkb status <TX_ID> --gateway $ARWEAVE_GATEWAY"
fi
