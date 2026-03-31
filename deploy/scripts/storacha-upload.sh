#!/bin/bash
#
# sparkdream-storacha-upload.sh
#
# Uploads archived block batches to Storacha (web3.storage) via the storacha CLI.
# Tracks which files have already been uploaded to avoid re-uploading.
# Maintains a manifest file mapping block ranges to IPFS CIDs.
#
# NOTE: This script requires Node.js and the storacha CLI, which are
# NOT included in the Docker image. It is intended to be run from your
# local machine (not inside the container).
#
# Prerequisites:
#   1. Install Node.js 18+ and the storacha CLI:
#      npm install -g @storacha/cli
#
#   2. Authenticate and create a space (one-time setup):
#      storacha login your@email.com
#      storacha space create "sparkdream-archives"
#
#   For headless/CI usage, generate a key and delegation proof:
#      storacha key create --json
#      storacha delegation create <AGENT_DID> \
#        -c space/blob/add -c space/index/add \
#        -c filecoin/offer -c upload/add --base64
#      Then set STORACHA_PRINCIPAL and STORACHA_PROOF env vars.
#
# Usage:
#   ./sparkdream-storacha-upload.sh [archive_directory]
#
# Environment variables (all optional):
#   ARCHIVE_DIR     - Directory containing .jsonl.gz files (default: ./sparkdream-archives)
#   MANIFEST_FILE   - Path to the CID manifest (default: $ARCHIVE_DIR/storacha-manifest.csv)
#   UPLOADED_FILE   - Tracks already-uploaded files (default: $ARCHIVE_DIR/.storacha-uploaded)
#   DRY_RUN         - Set to "true" to show what would be uploaded without uploading
#
set -e

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
ARCHIVE_DIR="${1:-${ARCHIVE_DIR:-./sparkdream-archives}}"
MANIFEST_FILE="${MANIFEST_FILE:-${ARCHIVE_DIR}/storacha-manifest.csv}"
UPLOADED_FILE="${UPLOADED_FILE:-${ARCHIVE_DIR}/.storacha-uploaded}"

# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------
if ! command -v storacha >/dev/null 2>&1; then
    echo "ERROR: 'storacha' CLI is not installed." >&2
    echo "Install it with: npm install -g @storacha/cli" >&2
    echo "Then run: storacha login your@email.com" >&2
    exit 1
fi

if [ ! -d "$ARCHIVE_DIR" ]; then
    echo "ERROR: Archive directory not found: $ARCHIVE_DIR" >&2
    exit 1
fi

# Verify storacha is authenticated
if ! storacha whoami >/dev/null 2>&1; then
    echo "ERROR: storacha is not authenticated." >&2
    echo "Run: storacha login your@email.com" >&2
    exit 1
fi

# Initialize manifest with header if it doesn't exist
if [ ! -f "$MANIFEST_FILE" ]; then
    echo "file,from_block,to_block,cid,gateway_url,uploaded_at" > "$MANIFEST_FILE"
    echo "Created manifest: $MANIFEST_FILE"
fi

# Initialize uploaded tracker
touch "$UPLOADED_FILE"

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

    FILE_SIZE=$(du -h "$ARCHIVE_FILE" | cut -f1)
    echo "Uploading: $FILENAME ($FILE_SIZE) [blocks ${FROM_BLOCK}-${TO_BLOCK}]"

    if [ "${DRY_RUN}" = "true" ]; then
        echo "  [DRY RUN] Would upload $FILENAME"
        continue
    fi

    # Upload to Storacha
    UPLOAD_OUTPUT=$(storacha up "$ARCHIVE_FILE" 2>&1) || {
        echo "  ERROR: Upload failed for $FILENAME" >&2
        echo "  Output: $UPLOAD_OUTPUT" >&2
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
        continue
    }

    # Extract CID from output
    CID=$(echo "$UPLOAD_OUTPUT" | grep -o 'bafy[a-zA-Z0-9]*' | head -1)

    if [ -z "$CID" ]; then
        echo "  WARNING: Could not extract CID from upload output" >&2
        echo "  Output: $UPLOAD_OUTPUT" >&2
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
        continue
    fi

    GATEWAY_URL="https://${CID}.ipfs.w3s.link"
    TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Record in manifest
    echo "${FILENAME},${FROM_BLOCK},${TO_BLOCK},${CID},${GATEWAY_URL},${TIMESTAMP}" >> "$MANIFEST_FILE"

    # Mark as uploaded
    echo "$FILENAME" >> "$UPLOADED_FILE"

    echo "  CID: $CID"
    echo "  URL: $GATEWAY_URL"

    UPLOAD_COUNT=$(( UPLOAD_COUNT + 1 ))
done

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "========================================"
echo "Storacha upload complete"
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
