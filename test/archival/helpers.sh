#!/bin/bash
#
# Shared helpers for archival integration tests.
#
# Source this file at the top of each test script:
#   source "$(dirname "$0")/helpers.sh"

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SCRIPTS_DIR="$PROJECT_ROOT/deploy/scripts"

# Load .env from project root if it exists
if [ -f "$PROJECT_ROOT/.env" ]; then
    set -a
    source "$PROJECT_ROOT/.env"
    set +a
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

pass() { echo -e "${GREEN}PASS${NC}: $1"; }
fail() { echo -e "${RED}FAIL${NC}: $1"; FAILED=1; }
skip() { echo -e "${YELLOW}SKIP${NC}: $1"; }

FAILED=0

# Create a small synthetic archive file for testing.
# Produces a valid blocks_1_to_3.jsonl.gz in the given directory.
create_test_archive() {
    local dir="$1"
    local archive="$dir/blocks_1_to_3.jsonl.gz"

    mkdir -p "$dir"

    # Minimal valid block JSON (matches the archiver's output format)
    {
        echo '{"block_id":{"hash":"AA","parts":{"total":1,"hash":"BB"}},"block":{"header":{"height":"1","time":"2025-01-01T00:00:00Z","chain_id":"test","version":{"block":"11"}},"data":{"txs":null},"evidence":{"evidence":null},"last_commit":null},"block_results":{"height":"1"}}'
        echo '{"block_id":{"hash":"CC","parts":{"total":1,"hash":"DD"}},"block":{"header":{"height":"2","time":"2025-01-01T00:00:01Z","chain_id":"test","version":{"block":"11"}},"data":{"txs":null},"evidence":{"evidence":null},"last_commit":null},"block_results":{"height":"2"}}'
        echo '{"block_id":{"hash":"EE","parts":{"total":1,"hash":"FF"}},"block":{"header":{"height":"3","time":"2025-01-01T00:00:02Z","chain_id":"test","version":{"block":"11"}},"data":{"txs":null},"evidence":{"evidence":null},"last_commit":null},"block_results":{"height":"3"}}'
    } | gzip > "$archive"

    echo "$archive"
}

# Clean up test artifacts from a directory (uploaded trackers, manifests).
cleanup_test_dir() {
    local dir="$1"
    rm -f "$dir"/*.jsonl.gz
    rm -f "$dir"/*-manifest.csv "$dir"/.*-uploaded
    rm -f "$dir"/.last_archived_height "$dir"/.block-archiver.lock
}

# Exit with appropriate code at end of test script.
finish() {
    if [ "$FAILED" -ne 0 ]; then
        echo ""
        echo -e "${RED}Some tests failed.${NC}"
        exit 1
    else
        echo ""
        echo -e "${GREEN}All tests passed.${NC}"
        exit 0
    fi
}
