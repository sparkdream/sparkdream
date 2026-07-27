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
# Finalize mode (--finalize) additionally writes the blocks *after* the last
# clean boundary as a tail file named blocks_<from>_to_<to>.partial.jsonl.gz.
# The state file is deliberately NOT advanced, so the next normal run still
# produces the clean, boundary-aligned blocks_<from>_to_<boundary>.jsonl.gz
# covering the same starting height. The two files overlap on purpose;
# `sparkdreamd replay-from-archive` skips already-applied blocks, and this
# script deletes a superseded local tail once the complete file is written.
#
# Finalize is for cold restores — rebuilding a sentry or validator from block
# history alone, with no live peer to catch up from. A node that can reach the
# network does not need it: replay to the last boundary, then p2p sync the
# remainder. Keep the cron on the default (non-finalize) path.
#
# Usage:
#   ./sparkdream-block-archiver.sh              # normal incremental run
#   ./sparkdream-block-archiver.sh --finalize   # ... plus a tail file
#
# Environment variables (all optional, with defaults):
#   RPC_URL         - Node RPC endpoint (default: http://localhost:26657)
#   OUTPUT_DIR      - Where to save archives (default: /root/.sparkdream/archives)
#   STATE_FILE      - Progress tracker file (default: $OUTPUT_DIR/.last_archived_height)
#   START_HEIGHT    - Starting height on first run (default: 1)
#   BATCH_SIZE      - Max blocks per archive file (default: 10000)
#   SLEEP_MS        - Milliseconds between RPC calls to avoid overload (default: 10)
#   FINALIZE        - Set to "true" to write the tail file (same as --finalize)
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
FINALIZE="${FINALIZE:-false}"

while [ $# -gt 0 ]; do
    case "$1" in
        --finalize) FINALIZE="true"; shift ;;
        -h|--help)
            sed -n '2,/^set -eo/{ /^#/s/^# \?//p }' "$0"
            exit 0
            ;;
        *) echo "ERROR: Unknown argument '$1' (try --help)." >&2; exit 1 ;;
    esac
done

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
# Block fetching
# ---------------------------------------------------------------------------

# Highest height successfully appended by the last fetch_blocks_into call.
LAST_FETCHED=0

# fetch_blocks_into <from> <to> <outfile>
#
# Appends one JSON line per height to <outfile>. Returns 0 when the whole
# range was fetched, 1 when a height could not be retrieved. On failure
# LAST_FETCHED holds the last height actually written (from - 1 if none),
# so callers can decide whether to keep what was fetched.
fetch_blocks_into() {
    local from="$1" to="$2" outfile="$3"
    local h="$from"

    LAST_FETCHED=$(( from - 1 ))

    while [ "$h" -le "$to" ]; do
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
                return 1
            fi
        fi

        # Fetch block_results for this height (needed for replay)
        RESULTS_JSON=$(curl -s "${RPC_URL}/block_results?height=${h}")
        RESULTS_CHECK=$(echo "$RESULTS_JSON" | jq -r '.result.height // empty' 2>/dev/null)
        if [ -z "$RESULTS_CHECK" ]; then
            echo "ERROR: Failed to fetch block_results for height ${h}. Stopping." >&2
            return 1
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
            -n '{block_id: $block.result.block_id, block: $block.result.block, block_results: $results.result, commit: $commit.result.signed_header.commit}' >> "$outfile"

        LAST_FETCHED=$h

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

    return 0
}

# prune_superseded_partials <from>
#
# Deletes any tail file starting at <from>. Called once the complete,
# boundary-aligned archive covering that starting height exists, so the
# archive directory never accumulates redundant tails. Only local copies
# are removed — anything already uploaded stays where it is, which is why
# replay tolerates overlapping ranges.
prune_superseded_partials() {
    local from="$1"
    local partial
    for partial in "${OUTPUT_DIR}/blocks_${from}_to_"*.partial.jsonl.gz; do
        [ -f "$partial" ] || continue
        echo "  Removing superseded tail: $(basename "$partial")"
        rm -f "$partial"
    done
}

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
    # Align TO to the next clean BATCH_SIZE boundary (e.g., 10000, 20000, ...).
    # If FROM is mid-boundary (e.g., 42081), the first batch will be smaller
    # to "catch up" (42081-50000), then subsequent batches resume clean alignment.
    # IMPORTANT: Never create a partial batch — wait until the chain passes the
    # boundary so every archive file ends on a clean multiple of BATCH_SIZE
    # (except the catch-up batch which ends on the next boundary).
    NEXT_BOUNDARY=$(( ((FROM - 1) / BATCH_SIZE + 1) * BATCH_SIZE ))
    TO=$NEXT_BOUNDARY
    if [ "$TO" -gt "$CURRENT_HEIGHT" ]; then
        echo "Waiting for chain to reach boundary ${TO} (current: ${CURRENT_HEIGHT}). Nothing to archive yet."
        break
    fi

    BATCH_FILE="${OUTPUT_DIR}/blocks_${FROM}_to_${TO}.jsonl"
    BATCH_FILE_GZ="${BATCH_FILE}.gz"

    # Skip if this batch was already archived (e.g., interrupted previous run)
    if [ -f "$BATCH_FILE_GZ" ]; then
        echo "Batch ${FROM}-${TO} already exists, skipping."
        echo "$TO" > "$STATE_FILE"
        prune_superseded_partials "$FROM"
        FROM=$(( TO + 1 ))
        continue
    fi

    # If an uncompressed file exists from an interrupted run, compress it and move on.
    # The state file was NOT updated (gzip hadn't run yet), so the block range is complete.
    if [ -f "$BATCH_FILE" ]; then
        LINE_COUNT=$(wc -l < "$BATCH_FILE")
        EXPECTED_COUNT=$(( TO - FROM + 1 ))
        if [ "$LINE_COUNT" -eq "$EXPECTED_COUNT" ]; then
            echo "Found complete uncompressed batch ${FROM}-${TO} (${LINE_COUNT} lines). Compressing..."
            gzip "$BATCH_FILE"
            echo "$TO" > "$STATE_FILE"
            prune_superseded_partials "$FROM"
            FROM=$(( TO + 1 ))
            continue
        else
            echo "WARNING: Found incomplete batch ${FROM}-${TO} (${LINE_COUNT}/${EXPECTED_COUNT} lines). Removing and re-fetching."
            rm -f "$BATCH_FILE"
        fi
    fi

    echo "Archiving blocks ${FROM} to ${TO}..."

    if ! fetch_blocks_into "$FROM" "$TO" "$BATCH_FILE"; then
        # Save progress up to the last successful block. The state file is
        # advanced to match, so the next run resumes cleanly from there and
        # this short file never overlaps anything.
        if [ "$LAST_FETCHED" -ge "$FROM" ]; then
            PARTIAL_TO=$LAST_FETCHED
            PARTIAL_FILE="${OUTPUT_DIR}/blocks_${FROM}_to_${PARTIAL_TO}.jsonl"
            mv "$BATCH_FILE" "$PARTIAL_FILE"
            gzip "$PARTIAL_FILE"
            echo "$PARTIAL_TO" > "$STATE_FILE"
            echo "Saved partial batch ${FROM}-${PARTIAL_TO}"
            prune_superseded_partials "$FROM"
        fi
        exit 1
    fi

    # Compress the batch
    gzip "$BATCH_FILE"
    echo "  Saved: ${BATCH_FILE_GZ}"

    # Update state file after each successful batch
    echo "$TO" > "$STATE_FILE"

    # A tail file starting at this height (if any) is now redundant
    prune_superseded_partials "$FROM"

    FROM=$(( TO + 1 ))
done

# ---------------------------------------------------------------------------
# Finalize: write the sub-boundary tail
# ---------------------------------------------------------------------------
# Everything above only ever emits boundary-aligned files, so after a normal
# run the blocks between the last boundary and the chain tip exist nowhere in
# the archive. Finalize captures exactly that remainder.
#
# The state file is NOT advanced. That is the whole point: the next normal run
# still fetches from the same starting height and produces the clean
# blocks_<from>_to_<boundary>.jsonl.gz, at which point the tail is pruned.
if [ "$FINALIZE" = "true" ]; then
    echo ""
    echo "Finalizing..."

    LAST_COMPLETE=$(cat "$STATE_FILE" 2>/dev/null || echo $(( START_HEIGHT - 1 )))
    TAIL_FROM=$(( LAST_COMPLETE + 1 ))
    TAIL_TO=$CURRENT_HEIGHT

    if [ "$TAIL_FROM" -gt "$TAIL_TO" ]; then
        echo "  Nothing to finalize — complete archives already cover block ${LAST_COMPLETE}."
    elif [ -f "${OUTPUT_DIR}/blocks_${TAIL_FROM}_to_${TAIL_TO}.partial.jsonl.gz" ]; then
        echo "  Tail blocks_${TAIL_FROM}_to_${TAIL_TO}.partial.jsonl.gz already exists, skipping."
    else
        echo "  Writing tail for blocks ${TAIL_FROM} to ${TAIL_TO}..."

        # Fetch into a dot-prefixed temp file and rename only once complete, so
        # an interrupted finalize never leaves a truncated tail that looks whole.
        TAIL_TMP="${OUTPUT_DIR}/.blocks_${TAIL_FROM}_to_${TAIL_TO}.partial.jsonl.tmp"
        rm -f "$TAIL_TMP" "${TAIL_TMP}.gz"

        if ! fetch_blocks_into "$TAIL_FROM" "$TAIL_TO" "$TAIL_TMP"; then
            if [ "$LAST_FETCHED" -ge "$TAIL_FROM" ]; then
                # Keep what we got — a shorter tail is still a valid tail.
                echo "  WARNING: Tail truncated at block ${LAST_FETCHED} (wanted ${TAIL_TO})." >&2
                TAIL_TO=$LAST_FETCHED
            else
                echo "  ERROR: Could not fetch any tail blocks." >&2
                rm -f "$TAIL_TMP"
                exit 1
            fi
        fi

        gzip "$TAIL_TMP"

        # Drop any older tail from the same starting height — keep exactly one.
        prune_superseded_partials "$TAIL_FROM"

        TAIL_FILE="${OUTPUT_DIR}/blocks_${TAIL_FROM}_to_${TAIL_TO}.partial.jsonl.gz"
        mv "${TAIL_TMP}.gz" "$TAIL_FILE"
        echo "  Saved: ${TAIL_FILE}"
        echo "  State file left at ${LAST_COMPLETE} — normal runs still emit aligned ranges."
    fi
fi

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
