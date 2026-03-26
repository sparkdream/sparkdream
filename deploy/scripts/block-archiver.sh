#!/bin/sh
#
# sparkdream-block-archiver.sh
#
# Incrementally archives new blocks from a Cosmos SDK node's RPC endpoint.
# Each run produces a compressed file containing only the blocks since the
# last archived height, ensuring zero overlap between backups.
#
# The script tracks progress via a simple state file that records the last
# archived block height. On first run, it starts from block 1 (or a
# configurable starting height).
#
# Output: gzipped JSONL files named blocks_<from>_to_<to>.jsonl.gz
#         Each line is a full block JSON response.
#
# Usage:
#   ./sparkdream-block-archiver.sh
#
# Environment variables (all optional, with defaults):
#   RPC_URL         - Node RPC endpoint (default: http://localhost:26657)
#   OUTPUT_DIR      - Where to save archives (default: /tmp/sparkdream-archives)
#   STATE_FILE      - Progress tracker file (default: $OUTPUT_DIR/.last_archived_height)
#   START_HEIGHT    - Starting height on first run (default: 1)
#   BATCH_SIZE      - Max blocks per archive file (default: 10000)
#   SLEEP_MS        - Milliseconds between RPC calls to avoid overload (default: 10)
#
set -e

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
RPC_URL="${RPC_URL:-http://localhost:26657}"
OUTPUT_DIR="${OUTPUT_DIR:-/tmp/sparkdream-archives}"
STATE_FILE="${STATE_FILE:-${OUTPUT_DIR}/.last_archived_height}"
START_HEIGHT="${START_HEIGHT:-1}"
BATCH_SIZE="${BATCH_SIZE:-10000}"
SLEEP_MS="${SLEEP_MS:-10}"

# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------
for cmd in curl jq gzip; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "ERROR: '$cmd' is required but not installed." >&2
        exit 1
    fi
done

mkdir -p "$OUTPUT_DIR"

# ---------------------------------------------------------------------------
# Determine the range to archive
# ---------------------------------------------------------------------------

# Read last archived height from state file, or use START_HEIGHT - 1
if [ -f "$STATE_FILE" ]; then
    LAST_ARCHIVED=$(cat "$STATE_FILE")
else
    LAST_ARCHIVED=$(( START_HEIGHT - 1 ))
fi

# Get the current chain height
CURRENT_HEIGHT=$(curl -s "${RPC_URL}/status" | jq -r '.result.sync_info.latest_block_height')

if [ -z "$CURRENT_HEIGHT" ] || [ "$CURRENT_HEIGHT" = "null" ]; then
    echo "ERROR: Could not fetch current block height from ${RPC_URL}/status" >&2
    exit 1
fi

NEXT_HEIGHT=$(( LAST_ARCHIVED + 1 ))

if [ "$NEXT_HEIGHT" -gt "$CURRENT_HEIGHT" ]; then
    echo "Already up to date. Last archived: ${LAST_ARCHIVED}, chain height: ${CURRENT_HEIGHT}"
    exit 0
fi

echo "Chain height: ${CURRENT_HEIGHT}"
echo "Last archived: ${LAST_ARCHIVED}"
echo "Blocks to archive: $(( CURRENT_HEIGHT - LAST_ARCHIVED ))"
echo ""

# ---------------------------------------------------------------------------
# Archive in batches
# ---------------------------------------------------------------------------
FROM=$NEXT_HEIGHT

while [ "$FROM" -le "$CURRENT_HEIGHT" ]; do
    TO=$(( FROM + BATCH_SIZE - 1 ))
    if [ "$TO" -gt "$CURRENT_HEIGHT" ]; then
        TO=$CURRENT_HEIGHT
    fi

    BATCH_FILE="${OUTPUT_DIR}/blocks_${FROM}_to_${TO}.jsonl"
    BATCH_FILE_GZ="${BATCH_FILE}.gz"

    # Skip if this batch was already archived (e.g., interrupted previous run)
    if [ -f "$BATCH_FILE_GZ" ]; then
        echo "Batch ${FROM}-${TO} already exists, skipping."
        FROM=$(( TO + 1 ))
        continue
    fi

    echo "Archiving blocks ${FROM} to ${TO}..."

    # Fetch each block and append as one JSON line
    h=$FROM
    while [ "$h" -le "$TO" ]; do
        BLOCK_JSON=$(curl -s "${RPC_URL}/block?height=${h}")

        # Validate we got a proper response
        BLOCK_HEIGHT_CHECK=$(echo "$BLOCK_JSON" | jq -r '.result.block.header.height // empty' 2>/dev/null)
        if [ -z "$BLOCK_HEIGHT_CHECK" ]; then
            echo "WARNING: Failed to fetch block ${h}, retrying in 2s..." >&2
            sleep 2
            BLOCK_JSON=$(curl -s "${RPC_URL}/block?height=${h}")
            BLOCK_HEIGHT_CHECK=$(echo "$BLOCK_JSON" | jq -r '.result.block.header.height // empty' 2>/dev/null)
            if [ -z "$BLOCK_HEIGHT_CHECK" ]; then
                echo "ERROR: Failed to fetch block ${h} after retry. Stopping." >&2
                # Save progress up to the last successful block
                if [ "$h" -gt "$FROM" ]; then
                    PARTIAL_TO=$(( h - 1 ))
                    PARTIAL_FILE="${OUTPUT_DIR}/blocks_${FROM}_to_${PARTIAL_TO}.jsonl"
                    mv "$BATCH_FILE" "$PARTIAL_FILE"
                    gzip "$PARTIAL_FILE"
                    echo "$PARTIAL_TO" > "$STATE_FILE"
                    echo "Saved partial batch ${FROM}-${PARTIAL_TO}"
                fi
                exit 1
            fi
        fi

        # Append the compact block JSON as a single line
        echo "$BLOCK_JSON" | jq -c '.' >> "$BATCH_FILE"

        # Progress indicator every 500 blocks
        if [ $(( h % 500 )) -eq 0 ]; then
            echo "  ... fetched block ${h}"
        fi

        # Rate limiting
        if [ "$SLEEP_MS" -gt 0 ]; then
            # Use awk for sub-second sleep since Alpine's sleep supports fractions
            sleep "$(echo "$SLEEP_MS" | awk '{printf "%.3f", $1/1000}')"
        fi

        h=$(( h + 1 ))
    done

    # Compress the batch
    gzip "$BATCH_FILE"
    echo "  Saved: ${BATCH_FILE_GZ}"

    # Update state file after each successful batch
    echo "$TO" > "$STATE_FILE"

    FROM=$(( TO + 1 ))
done

echo ""
echo "Archival complete."
echo "Archived up to block: ${CURRENT_HEIGHT}"
echo "Files in: ${OUTPUT_DIR}"
echo ""
echo "Archive inventory:"
ls -lh "${OUTPUT_DIR}"/blocks_*.jsonl.gz 2>/dev/null || echo "  (no files)"
echo ""
echo "Total size:"
du -sh "${OUTPUT_DIR}"
