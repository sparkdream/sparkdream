#!/bin/bash
#
# sparkdream-filebase-upload.sh
#
# Uploads archived block batches to Filebase for IPFS pinning via the
# S3-compatible API. Tracks which files have already been uploaded to
# avoid re-uploading. Maintains a manifest file mapping block ranges
# to IPFS CIDs.
#
# Filebase provides S3-compatible storage that automatically pins
# uploads to IPFS. The free tier covers 5 GiB. Files are accessible
# via Filebase's IPFS gateway or any public gateway.
#
# NOTE: This script requires the AWS CLI, which is NOT included in
# the Docker image. It is intended to be run from your local machine
# (not inside the container). Install with:
#   - macOS:  brew install awscli
#   - Linux:  apt install awscli / apk add aws-cli
#   - pip:    pip install awscli
#
# Prerequisites:
#   1. Create a Filebase account at https://console.filebase.com
#   2. Create an Access Key and Secret Key
#   3. Create a bucket with IPFS as the storage network
#   4. Install the AWS CLI (see above)
#
# Usage:
#   ./sparkdream-filebase-upload.sh [archive_directory]
#
# Environment variables:
#   AWS_ACCESS_KEY_ID       - Filebase access key (required)
#   AWS_SECRET_ACCESS_KEY   - Filebase secret key (required)
#   FILEBASE_BUCKET         - Bucket name (required)
#   ARCHIVE_DIR             - Directory containing .jsonl.gz files (default: ./sparkdream-archives)
#   MANIFEST_FILE           - Path to the CID manifest (default: $ARCHIVE_DIR/filebase-manifest.csv)
#   UPLOADED_FILE           - Tracks already-uploaded files (default: $ARCHIVE_DIR/.filebase-uploaded)
#   FILEBASE_ENDPOINT       - S3 endpoint (default: https://s3.filebase.com)
#   FILEBASE_GATEWAY        - IPFS gateway (default: https://ipfs.filebase.io)
#   DRY_RUN                 - Set to "true" to show what would be uploaded without uploading
#
set -e

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
ARCHIVE_DIR="${1:-${ARCHIVE_DIR:-./sparkdream-archives}}"
MANIFEST_FILE="${MANIFEST_FILE:-${ARCHIVE_DIR}/filebase-manifest.csv}"
UPLOADED_FILE="${UPLOADED_FILE:-${ARCHIVE_DIR}/.filebase-uploaded}"
FILEBASE_ENDPOINT="${FILEBASE_ENDPOINT:-https://s3.filebase.com}"
FILEBASE_GATEWAY="${FILEBASE_GATEWAY:-https://ipfs.filebase.io}"

# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------
if ! command -v aws >/dev/null 2>&1; then
    echo "ERROR: 'aws' CLI is not installed." >&2
    echo "Install it with: apk add aws-cli  (or: pip install awscli)" >&2
    exit 1
fi

if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
    echo "ERROR: AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are required." >&2
    echo "Create credentials at: https://console.filebase.com/keys" >&2
    exit 1
fi

if [ -z "$FILEBASE_BUCKET" ]; then
    echo "ERROR: FILEBASE_BUCKET is required." >&2
    echo "Create an IPFS bucket at: https://console.filebase.com/buckets" >&2
    exit 1
fi

if [ ! -d "$ARCHIVE_DIR" ]; then
    echo "ERROR: Archive directory not found: $ARCHIVE_DIR" >&2
    exit 1
fi

# Verify bucket access
echo "Verifying Filebase bucket access..."
if ! aws --endpoint-url "$FILEBASE_ENDPOINT" s3 ls "s3://${FILEBASE_BUCKET}" >/dev/null 2>&1; then
    echo "ERROR: Cannot access bucket '${FILEBASE_BUCKET}'." >&2
    echo "Check credentials and bucket name." >&2
    exit 1
fi
echo "  Bucket '${FILEBASE_BUCKET}' is accessible."
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

    # Upload to Filebase via S3 API with metadata tags
    S3_KEY="sparkdream-archives/${FILENAME}"
    UPLOAD_OUTPUT=$(aws --endpoint-url "$FILEBASE_ENDPOINT" \
        s3 cp "$ARCHIVE_FILE" "s3://${FILEBASE_BUCKET}/${S3_KEY}" \
        --metadata "app=sparkdream-block-archive,chain-id=sparkdream-1,block-range-from=${FROM_BLOCK},block-range-to=${TO_BLOCK}" \
        --content-type "application/gzip" \
        2>&1) || {
        echo "  ERROR: Upload failed for $FILENAME" >&2
        echo "  Output: $UPLOAD_OUTPUT" >&2
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
        continue
    }

    # Retrieve the IPFS CID from the object metadata
    # Filebase returns it in the x-amz-meta-cid header
    CID=$(aws --endpoint-url "$FILEBASE_ENDPOINT" \
        s3api head-object \
        --bucket "$FILEBASE_BUCKET" \
        --key "$S3_KEY" \
        --query 'Metadata.cid' \
        --output text 2>/dev/null)

    if [ -z "$CID" ] || [ "$CID" = "None" ]; then
        echo "  WARNING: Upload succeeded but could not retrieve CID." >&2
        echo "  The file may still be processing. Check Filebase console." >&2
        # Still mark as uploaded to avoid re-uploading
        echo "$FILENAME" >> "$UPLOADED_FILE"
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
        continue
    fi

    GATEWAY_URL="${FILEBASE_GATEWAY}/ipfs/${CID}"
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
echo "Filebase upload complete"
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
    echo "Files are pinned to IPFS and available via:"
    echo "  Filebase:  ${FILEBASE_GATEWAY}/ipfs/<CID>"
    echo "  Public:    https://ipfs.io/ipfs/<CID>"
    echo "  dweb:      https://<CID>.ipfs.dweb.link"
    echo ""
    echo "Manage files at: https://console.filebase.com/buckets/${FILEBASE_BUCKET}"
fi
