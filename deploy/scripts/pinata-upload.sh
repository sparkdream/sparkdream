#!/bin/bash
#
# sparkdream-pinata-upload.sh
#
# Uploads archived block batches to Pinata for IPFS pinning.
# Tracks which files have already been uploaded to avoid re-uploading.
# Maintains a manifest file mapping block ranges to IPFS CIDs.
#
# Pinata is an IPFS pinning service that ensures your files remain
# available on the IPFS network. Files are accessible via any IPFS
# gateway or Pinata's dedicated gateway.
#
# NOTE: This script is intended to be run from your local machine
# (not inside the container). Do not store API credentials on Akash
# providers.
#
# Prerequisites:
#   1. Create a Pinata account at https://app.pinata.cloud
#   2. Generate an API key (JWT) at https://app.pinata.cloud/developers/api-keys
#   3. Set the PINATA_JWT environment variable with your JWT token
#
# Usage:
#   ./sparkdream-pinata-upload.sh [archive_directory]
#
# Environment variables:
#   PINATA_JWT        - Pinata JWT API key (required)
#   ARCHIVE_DIR       - Directory containing .jsonl.gz files (default: ./sparkdream-archives)
#   MANIFEST_FILE     - Path to the CID manifest (default: $ARCHIVE_DIR/pinata-manifest.csv)
#   UPLOADED_FILE     - Tracks already-uploaded files (default: $ARCHIVE_DIR/.pinata-uploaded)
#   PINATA_API_URL    - Pinata API base URL (default: https://api.pinata.cloud)
#   PINATA_GATEWAY    - Pinata gateway for retrieval (default: https://gateway.pinata.cloud)
#   DRY_RUN           - Set to "true" to show what would be uploaded without uploading
#
set -e

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
ARCHIVE_DIR="${1:-${ARCHIVE_DIR:-./sparkdream-archives}}"
MANIFEST_FILE="${MANIFEST_FILE:-${ARCHIVE_DIR}/pinata-manifest.csv}"
UPLOADED_FILE="${UPLOADED_FILE:-${ARCHIVE_DIR}/.pinata-uploaded}"
PINATA_API_URL="${PINATA_API_URL:-https://api.pinata.cloud}"
PINATA_GATEWAY="${PINATA_GATEWAY:-https://gateway.pinata.cloud}"

# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------
for cmd in curl jq; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "ERROR: '$cmd' is required but not installed." >&2
        exit 1
    fi
done

if [ -z "$PINATA_JWT" ]; then
    echo "ERROR: PINATA_JWT environment variable is required." >&2
    echo "Generate one at: https://app.pinata.cloud/developers/api-keys" >&2
    exit 1
fi

if [ ! -d "$ARCHIVE_DIR" ]; then
    echo "ERROR: Archive directory not found: $ARCHIVE_DIR" >&2
    exit 1
fi

# Verify authentication
echo "Verifying Pinata authentication..."
AUTH_RESPONSE=$(curl -sf \
    -H "Authorization: Bearer ${PINATA_JWT}" \
    "${PINATA_API_URL}/data/testAuthentication" 2>&1) || {
    echo "ERROR: Pinata authentication failed." >&2
    echo "Check your PINATA_JWT token." >&2
    exit 1
}
echo "  Authenticated as: $(echo "$AUTH_RESPONSE" | jq -r '.message // "OK"')"
echo ""

# Initialize manifest with header if it doesn't exist
if [ ! -f "$MANIFEST_FILE" ]; then
    echo "file,from_block,to_block,cid,gateway_url,file_size,uploaded_at" > "$MANIFEST_FILE"
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

    # Build Pinata metadata for discoverability
    PINATA_METADATA=$(jq -nc \
        --arg name "$FILENAME" \
        --arg from "$FROM_BLOCK" \
        --arg to "$TO_BLOCK" \
        '{
            name: $name,
            keyvalues: {
                app: "sparkdream-block-archive",
                chain_id: "sparkdream-1",
                block_range_from: $from,
                block_range_to: $to
            }
        }')

    # Upload to Pinata
    UPLOAD_RESPONSE=$(curl -sf \
        -X POST \
        -H "Authorization: Bearer ${PINATA_JWT}" \
        -F "file=@${ARCHIVE_FILE}" \
        -F "pinataMetadata=${PINATA_METADATA}" \
        -F "pinataOptions={\"cidVersion\":1}" \
        "${PINATA_API_URL}/pinning/pinFileToIPFS" 2>&1) || {
        echo "  ERROR: Upload failed for $FILENAME" >&2
        echo "  Response: $UPLOAD_RESPONSE" >&2
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
        continue
    }

    # Extract CID from response
    CID=$(echo "$UPLOAD_RESPONSE" | jq -r '.IpfsHash // empty')

    if [ -z "$CID" ]; then
        echo "  WARNING: Could not extract CID from upload response" >&2
        echo "  Response: $UPLOAD_RESPONSE" >&2
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
        continue
    fi

    GATEWAY_URL="${PINATA_GATEWAY}/ipfs/${CID}"
    TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Record in manifest
    echo "${FILENAME},${FROM_BLOCK},${TO_BLOCK},${CID},${GATEWAY_URL},${FILE_SIZE},${TIMESTAMP}" >> "$MANIFEST_FILE"

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
echo "Pinata upload complete"
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

if [ "$UPLOAD_COUNT" -gt 0 ]; then
    echo ""
    echo "Files are pinned and available via any IPFS gateway:"
    echo "  Pinata:    ${PINATA_GATEWAY}/ipfs/<CID>"
    echo "  Public:    https://ipfs.io/ipfs/<CID>"
    echo "  dweb:      https://<CID>.ipfs.dweb.link"
    echo ""
    echo "Manage pins at: https://app.pinata.cloud/pinmanager"
fi
