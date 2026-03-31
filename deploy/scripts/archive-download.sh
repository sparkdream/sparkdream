#!/bin/bash
#
# sparkdream-archive-download.sh
#
# Downloads block archive files from any configured storage service
# by reading the corresponding manifest CSV.
#
# Supports: storacha, pinata, filebase, arweave, jackal
#
# Usage:
#   ./archive-download.sh <service> [options]
#
# Download modes (pick one):
#   -b <block>          Download the archive containing this block height
#   -r <from>-<to>      Download archives covering this block range
#   -f <filename>       Download a specific archive file by name
#   -a                  Download all archives from the manifest
#
# Options:
#   -m <manifest>       Path to manifest CSV (default: auto-detect from archive dir)
#   -d <output_dir>     Output directory (default: ./sparkdream-archives)
#   -n                  Dry run — show what would be downloaded
#   -h                  Show this help
#
# Examples:
#   ./archive-download.sh storacha -b 5000
#   ./archive-download.sh arweave -r 1-20000
#   ./archive-download.sh pinata -f blocks_1_to_10000.jsonl.gz
#   ./archive-download.sh filebase -a -d /data/restore
#   ./archive-download.sh jackal -b 15000 -m /path/to/jackal-manifest.csv
#
# Environment variables:
#   ARCHIVE_DIR         - Default output directory (default: /root/.sparkdream/restored-archives)
#   JACKAL_API_URL      - Jackal API base URL (default: http://localhost:3535)
#   FILEBASE_BUCKET     - Filebase S3 bucket name (required for filebase)
#   FILEBASE_ENDPOINT   - Filebase S3 endpoint (default: https://s3.filebase.com)
#
set -e

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
usage() {
    sed -n '2,/^set -e/{ /^#/s/^# \?//p }' "$0"
    exit "${1:-0}"
}

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
SERVICE=""
MODE=""
BLOCK=""
RANGE_FROM=""
RANGE_TO=""
TARGET_FILE=""
DRY_RUN="false"
OUTPUT_DIR="${ARCHIVE_DIR:-/root/.sparkdream/restored-archives}"
MANIFEST=""
JACKAL_API_URL="${JACKAL_API_URL:-http://localhost:3535}"
FILEBASE_ENDPOINT="${FILEBASE_ENDPOINT:-https://s3.filebase.com}"

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
SERVICE="${1:-}"
if [ -z "$SERVICE" ]; then
    echo "ERROR: Service name is required." >&2
    echo "Usage: $0 <storacha|pinata|filebase|arweave|jackal> [options]" >&2
    exit 1
fi
shift

case "$SERVICE" in
    storacha|pinata|filebase|arweave|jackal) ;;
    -h|--help) usage 0 ;;
    *) echo "ERROR: Unknown service '$SERVICE'. Use: storacha, pinata, filebase, arweave, jackal" >&2; exit 1 ;;
esac

while getopts "b:r:f:m:d:nah" opt; do
    case $opt in
        b) MODE="block"; BLOCK="$OPTARG" ;;
        r) MODE="range"
           RANGE_FROM=$(echo "$OPTARG" | cut -d'-' -f1)
           RANGE_TO=$(echo "$OPTARG" | cut -d'-' -f2)
           ;;
        f) MODE="file"; TARGET_FILE="$OPTARG" ;;
        a) MODE="all" ;;
        m) MANIFEST="$OPTARG" ;;
        d) OUTPUT_DIR="$OPTARG" ;;
        n) DRY_RUN="true" ;;
        h) usage 0 ;;
        *) usage 1 ;;
    esac
done

if [ -z "$MODE" ]; then
    echo "ERROR: Specify a download mode: -b <block>, -r <from>-<to>, -f <file>, or -a" >&2
    exit 1
fi

# Auto-detect manifest path
if [ -z "$MANIFEST" ]; then
    MANIFEST="${OUTPUT_DIR}/${SERVICE}-manifest.csv"
fi

if [ ! -f "$MANIFEST" ]; then
    echo "ERROR: Manifest not found: $MANIFEST" >&2
    echo "Upload archives first or specify manifest path with -m" >&2
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

# ---------------------------------------------------------------------------
# Manifest parsing
# ---------------------------------------------------------------------------

# Read manifest rows matching the requested mode.
# Each service has slightly different column layouts, but all share:
#   col1=file, col2=from_block, col3=to_block
# Service-specific ID is col4 (cid, tx_id, or fid).
get_matching_rows() {
    # Skip the header line
    tail -n +2 "$MANIFEST" | while IFS=',' read -r file from_block to_block rest; do
        case "$MODE" in
            all)
                echo "$file,$from_block,$to_block,$rest"
                ;;
            file)
                if [ "$file" = "$TARGET_FILE" ]; then
                    echo "$file,$from_block,$to_block,$rest"
                fi
                ;;
            block)
                if [ "$BLOCK" -ge "$from_block" ] && [ "$BLOCK" -le "$to_block" ]; then
                    echo "$file,$from_block,$to_block,$rest"
                fi
                ;;
            range)
                # Include any archive that overlaps with the requested range
                if [ "$from_block" -le "$RANGE_TO" ] && [ "$to_block" -ge "$RANGE_FROM" ]; then
                    echo "$file,$from_block,$to_block,$rest"
                fi
                ;;
        esac
    done
}

# ---------------------------------------------------------------------------
# Download functions (one per service)
# ---------------------------------------------------------------------------

download_storacha() {
    local file="$1" cid="$2"
    local url="https://${cid}.ipfs.w3s.link/${file}"
    local output="${OUTPUT_DIR}/${file}"

    if [ -f "$output" ]; then
        echo "  SKIP: $file (already exists)"
        return 0
    fi

    if [ "$DRY_RUN" = "true" ]; then
        echo "  [DRY RUN] Would download: $url"
        return 0
    fi

    echo "  Downloading: $file"
    curl -sL --fail --max-time 120 -o "$output" "$url" || {
        echo "  ERROR: Failed to download $file from Storacha" >&2
        rm -f "$output"
        return 1
    }
    echo "  Saved: $output"
}

download_pinata() {
    local file="$1" cid="$2"
    local url="https://gateway.pinata.cloud/ipfs/${cid}"
    local output="${OUTPUT_DIR}/${file}"

    if [ -f "$output" ]; then
        echo "  SKIP: $file (already exists)"
        return 0
    fi

    if [ "$DRY_RUN" = "true" ]; then
        echo "  [DRY RUN] Would download: $url"
        return 0
    fi

    echo "  Downloading: $file"
    curl -sL --fail --max-time 120 -o "$output" "$url" || {
        echo "  ERROR: Failed to download $file from Pinata" >&2
        rm -f "$output"
        return 1
    }
    echo "  Saved: $output"
}

download_filebase() {
    local file="$1" cid="$2"
    local output="${OUTPUT_DIR}/${file}"

    if [ -f "$output" ]; then
        echo "  SKIP: $file (already exists)"
        return 0
    fi

    if [ -z "$FILEBASE_BUCKET" ]; then
        echo "ERROR: FILEBASE_BUCKET is required for Filebase downloads." >&2
        echo "Set it with: export FILEBASE_BUCKET=your-bucket-name" >&2
        exit 1
    fi

    if [ "$DRY_RUN" = "true" ]; then
        echo "  [DRY RUN] Would download: s3://${FILEBASE_BUCKET}/sparkdream-archives/${file}"
        return 0
    fi

    echo "  Downloading: $file"
    aws s3 cp "s3://${FILEBASE_BUCKET}/sparkdream-archives/${file}" "$output" \
        --endpoint-url "$FILEBASE_ENDPOINT" --quiet || {
        echo "  ERROR: Failed to download $file from Filebase" >&2
        rm -f "$output"
        return 1
    }
    echo "  Saved: $output"
}

download_arweave() {
    local file="$1" tx_id="$2"
    local url="https://arweave.net/${tx_id}"
    local output="${OUTPUT_DIR}/${file}"

    if [ -f "$output" ]; then
        echo "  SKIP: $file (already exists)"
        return 0
    fi

    if [ "$DRY_RUN" = "true" ]; then
        echo "  [DRY RUN] Would download: $url"
        return 0
    fi

    echo "  Downloading: $file"
    curl -sL --fail --max-time 120 -o "$output" "$url" || {
        echo "  ERROR: Failed to download $file from Arweave" >&2
        rm -f "$output"
        return 1
    }
    echo "  Saved: $output"
}

download_jackal() {
    local file="$1" fid="$2"
    local url="${JACKAL_API_URL}/fid/download/${fid}"
    local output="${OUTPUT_DIR}/${file}"

    if [ -f "$output" ]; then
        echo "  SKIP: $file (already exists)"
        return 0
    fi

    if [ "$DRY_RUN" = "true" ]; then
        echo "  [DRY RUN] Would download: $url"
        return 0
    fi

    echo "  Downloading: $file"
    curl -sL --fail --max-time 120 -o "$output" "$url" || {
        echo "  ERROR: Failed to download $file from Jackal" >&2
        rm -f "$output"
        return 1
    }
    echo "  Saved: $output"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
echo "Service:   $SERVICE"
echo "Manifest:  $MANIFEST"
echo "Output:    $OUTPUT_DIR"
echo "Mode:      $MODE"
case "$MODE" in
    block) echo "Block:     $BLOCK" ;;
    range) echo "Range:     ${RANGE_FROM}-${RANGE_TO}" ;;
    file)  echo "File:      $TARGET_FILE" ;;
esac
echo ""

ROWS=$(get_matching_rows)

if [ -z "$ROWS" ]; then
    echo "No matching archives found in manifest."
    exit 0
fi

DOWNLOAD_COUNT=0
SKIP_COUNT=0
FAIL_COUNT=0

echo "$ROWS" | while IFS=',' read -r file from_block to_block id _rest; do
    echo "[blocks ${from_block}-${to_block}] $file"

    case "$SERVICE" in
        storacha)  download_storacha "$file" "$id" ;;
        pinata)    download_pinata "$file" "$id" ;;
        filebase)  download_filebase "$file" "$id" ;;
        arweave)   download_arweave "$file" "$id" ;;
        jackal)    download_jackal "$file" "$id" ;;
    esac

    status=$?
    if [ $status -eq 0 ]; then
        if [ -f "${OUTPUT_DIR}/${file}" ]; then
            DOWNLOAD_COUNT=$(( DOWNLOAD_COUNT + 1 ))
        else
            SKIP_COUNT=$(( SKIP_COUNT + 1 ))
        fi
    else
        FAIL_COUNT=$(( FAIL_COUNT + 1 ))
    fi
done

echo ""
echo "========================================"
echo "Download complete"
echo "  Output dir: $OUTPUT_DIR"
echo "========================================"
