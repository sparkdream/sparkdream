#!/bin/bash
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
#         Each line is a block JSON object (block_id + block, RPC envelope stripped).
#
# Usage:
#   ./sparkdream-block-archiver.sh
#
# Environment variables (all optional, with defaults):
#   RPC_URL         - Node RPC endpoint (default: http://localhost:26657)
#   OUTPUT_DIR      - Where to save archives (default: /root/.sparkdream/archives)
#   STATE_FILE      - Progress tracker file (default: $OUTPUT_DIR/.last_archived_height)
#   START_HEIGHT    - Starting height on first run (default: 1)
#   BATCH_SIZE      - Max blocks per archive file (default: 10000)
#   SLEEP_MS        - Milliseconds between RPC calls to avoid overload (default: 10)
#
set -eo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
RPC_URL="${RPC_URL:-http://localhost:26657}"
OUTPUT_DIR="${OUTPUT_DIR:-/root/.sparkdream/archives}"
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
# Lock file to prevent concurrent runs
# ---------------------------------------------------------------------------
LOCK_FILE="${OUTPUT_DIR}/.block-archiver.lock"

cleanup_lock() {
    rm -f "$LOCK_FILE"
}

if [ -f "$LOCK_FILE" ]; then
    LOCK_PID=$(cat "$LOCK_FILE" 2>/dev/null)
    if [ -n "$LOCK_PID" ] && kill -0 "$LOCK_PID" 2>/dev/null; then
        echo "ERROR: Another instance is running (PID ${LOCK_PID}). Exiting." >&2
        exit 1
    else
        echo "WARNING: Stale lock file found (PID ${LOCK_PID} not running). Removing." >&2
        rm -f "$LOCK_FILE"
    fi
fi

echo $$ > "$LOCK_FILE"
trap cleanup_lock EXIT

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
        echo "$TO" > "$STATE_FILE"
        FROM=$(( TO + 1 ))
        continue
    fi

    echo "Archiving blocks ${FROM} to ${TO}..."

    # Fetch each block (and optionally block_results) and append as one JSON line
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

        # Fetch block_results for this height (needed for replay)
        RESULTS_JSON=$(curl -s "${RPC_URL}/block_results?height=${h}")
        RESULTS_CHECK=$(echo "$RESULTS_JSON" | jq -r '.result.height // empty' 2>/dev/null)
        if [ -z "$RESULTS_CHECK" ]; then
            echo "ERROR: Failed to fetch block_results for height ${h}. Stopping." >&2
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

        # Fetch commit for this height (needed to save last block to block store during replay)
        COMMIT_JSON=$(curl -s "${RPC_URL}/commit?height=${h}")
        COMMIT_CHECK=$(echo "$COMMIT_JSON" | jq -r '.result.signed_header.commit.height // empty' 2>/dev/null)
        if [ -z "$COMMIT_CHECK" ]; then
            echo "WARNING: Failed to fetch commit for height ${h}, continuing without it" >&2
            COMMIT_JSON='{"result":{"signed_header":{"commit":null}}}'
        fi

        # Append as {block_id, block, block_results, commit} per line
        jq -c --argjson block "$BLOCK_JSON" --argjson results "$RESULTS_JSON" --argjson commit "$COMMIT_JSON" \
            -n '{block_id: $block.result.block_id, block: $block.result.block, block_results: $results.result, commit: $commit.result.signed_header.commit}' >> "$BATCH_FILE"

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
