#!/bin/bash
#
# Integration test for Jackal upload script.
# Supports both modes: vault (JACKAL_MNEMONIC) and pin (JACKAL_API_KEY).
# Auto-detects mode from available credentials; skips if neither is set.
#
source "$(dirname "$0")/helpers.sh"

echo "=== Jackal Upload Integration Test ==="
echo ""

# Auto-detect mode
if [ -n "$JACKAL_MNEMONIC" ]; then
    MODE="vault"

    if ! command -v node >/dev/null 2>&1; then
        skip "Node.js not installed — skipping Jackal vault tests"
        exit 0
    fi

    NODE_VERSION=$(node -v | sed 's/v//' | cut -d. -f1)
    if [ "$NODE_VERSION" -lt 20 ]; then
        skip "Node.js 20+ required (found v${NODE_VERSION}) — skipping Jackal vault tests"
        exit 0
    fi

    export NODE_PATH="${NODE_PATH:+${NODE_PATH}:}$(npm root -g)"

    if ! node -e "require('@jackallabs/jackal.js')" 2>/dev/null; then
        skip "@jackallabs/jackal.js not installed — skipping Jackal vault tests"
        exit 0
    fi

    echo "Testing vault mode (jackal.js SDK)"
elif [ -n "$JACKAL_API_KEY" ]; then
    MODE="pin"
    echo "Testing pin mode (Jackal Pin API)"

    JACKAL_API_URL="${JACKAL_API_URL:-https://pinapi.jackalprotocol.com}"
    HTTP_CODE=$(curl -sf -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer ${JACKAL_API_KEY}" \
        "${JACKAL_API_URL}/test" 2>/dev/null || echo "000")

    if [ "$HTTP_CODE" != "200" ]; then
        skip "Jackal Pin API returned HTTP $HTTP_CODE — skipping Jackal tests"
        exit 0
    fi
else
    skip "Neither JACKAL_MNEMONIC nor JACKAL_API_KEY set — skipping Jackal tests"
    exit 0
fi

echo ""

TEST_DIR=$(mktemp -d)
echo "$TEST_DIR"
#trap "cleanup_test_dir '$TEST_DIR'; rm -rf '$TEST_DIR'" EXIT

ARCHIVE_FILE=$(create_test_archive "$TEST_DIR")

# -------------------------------------------------------------------------
# Test 1: Upload succeeds and records to manifest
# -------------------------------------------------------------------------
echo "Test 1: Upload archive to Jackal ($MODE mode)"

#if "$SCRIPTS_DIR/jackal-upload.sh" "$TEST_DIR" > "$TEST_DIR/upload.log" 2>&1; then
#    pass "Upload script completed"
#else
#    fail "Upload script exited with error"
#    cat "$TEST_DIR/upload.log"
#fi

# Check manifest was created with an identifier (path or CID)
if [ -f "$TEST_DIR/jackal-manifest.csv" ]; then
    ID=$(tail -1 "$TEST_DIR/jackal-manifest.csv" | cut -d',' -f4)
    if [ -n "$ID" ] && [ "$ID" != "jackal_path" ] && [ "$ID" != "cid" ]; then
        pass "Manifest contains identifier: $ID"
    else
        fail "Manifest has no upload identifier"
    fi
else
    fail "Manifest file not created"
fi

# Check uploaded tracker
if grep -q "blocks_1_to_3.jsonl.gz" "$TEST_DIR/.jackal-uploaded" 2>/dev/null; then
    pass "Uploaded tracker recorded the file"
else
    fail "Uploaded tracker missing entry"
fi

# -------------------------------------------------------------------------
# Test 2: Re-run skips already uploaded file
# -------------------------------------------------------------------------
echo ""
echo "Test 2: Re-run skips already uploaded"

OUTPUT=$("$SCRIPTS_DIR/jackal-upload.sh" "$TEST_DIR" 2>&1)

if echo "$OUTPUT" | grep -q "Skipped:.*1"; then
    pass "Re-run correctly skipped the file"
else
    fail "Re-run did not skip"
    echo "$OUTPUT"
fi

# -------------------------------------------------------------------------
# Test 3: CID is accessible via IPFS (pin mode only)
# -------------------------------------------------------------------------
if [ "$MODE" = "pin" ] && [ -n "$ID" ] && [ "$ID" != "cid" ]; then
    echo ""
    echo "Test 3: Verify CID is accessible via IPFS"

    HTTP_CODE=$(curl -sf -o /dev/null -w "%{http_code}" \
        "https://ipfs.io/ipfs/${ID}" 2>/dev/null || echo "000")

    if [ "$HTTP_CODE" = "200" ]; then
        pass "File accessible via IPFS (HTTP $HTTP_CODE)"
    else
        skip "IPFS gateway returned HTTP $HTTP_CODE (file may still be propagating)"
    fi
fi

finish
