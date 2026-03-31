#!/bin/bash
#
# Integration test for Arweave upload script.
# Requires: arkb CLI installed, ARWEAVE_WALLET in .env pointing to a
# funded wallet file.
#
# NOTE: This test uploads to Arweave which is PERMANENT and costs AR
# tokens. The test file is tiny (<1KB) so the cost is negligible, but
# be aware that the upload cannot be undone.
#
source "$(dirname "$0")/helpers.sh"

echo "=== Arweave Upload Integration Test ==="
echo ""

# Check CLI
if ! command -v arkb >/dev/null 2>&1; then
    skip "arkb CLI not installed — skipping Arweave tests"
    exit 0
fi

# Check wallet
if [ -z "$ARWEAVE_WALLET" ]; then
    skip "ARWEAVE_WALLET not set in .env — skipping Arweave tests"
    exit 0
fi

if [ ! -f "$ARWEAVE_WALLET" ]; then
    skip "Wallet file not found: $ARWEAVE_WALLET — skipping Arweave tests"
    exit 0
fi

TEST_DIR=$(mktemp -d)
trap "cleanup_test_dir '$TEST_DIR'; rm -rf '$TEST_DIR'" EXIT

ARCHIVE_FILE=$(create_test_archive "$TEST_DIR")

# -------------------------------------------------------------------------
# Test 1: Upload succeeds and returns a TX ID
# -------------------------------------------------------------------------
echo "Test 1: Upload archive to Arweave"

"$SCRIPTS_DIR/arweave-upload.sh" -w "$ARWEAVE_WALLET" "$TEST_DIR" > "$TEST_DIR/upload.log" 2>&1

if [ $? -ne 0 ]; then
    fail "Upload script exited with error"
    cat "$TEST_DIR/upload.log"
else
    pass "Upload script completed"
fi

# Check manifest was created with a TX ID
if [ -f "$TEST_DIR/arweave-manifest.csv" ]; then
    TX_ID=$(tail -1 "$TEST_DIR/arweave-manifest.csv" | cut -d',' -f4)
    if [ -n "$TX_ID" ] && [ "$TX_ID" != "tx_id" ]; then
        pass "Manifest contains TX ID: $TX_ID"
    else
        fail "Manifest has no TX ID"
    fi
else
    fail "Manifest file not created"
fi

# Check uploaded tracker
if grep -q "blocks_1_to_3.jsonl.gz" "$TEST_DIR/.arweave-uploaded" 2>/dev/null; then
    pass "Uploaded tracker recorded the file"
else
    fail "Uploaded tracker missing entry"
fi

# -------------------------------------------------------------------------
# Test 2: Re-run skips already uploaded file
# -------------------------------------------------------------------------
echo ""
echo "Test 2: Re-run skips already uploaded"

OUTPUT=$("$SCRIPTS_DIR/arweave-upload.sh" -w "$ARWEAVE_WALLET" "$TEST_DIR" 2>&1)

if echo "$OUTPUT" | grep -q "Skipped:.*1"; then
    pass "Re-run correctly skipped the file"
else
    fail "Re-run did not skip"
    echo "$OUTPUT"
fi

# -------------------------------------------------------------------------
# Test 3: TX is queryable (may take time to confirm)
# -------------------------------------------------------------------------
if [ -n "$TX_ID" ] && [ "$TX_ID" != "tx_id" ]; then
    echo ""
    echo "Test 3: Verify TX is queryable"

    HTTP_CODE=$(curl -sf -o /dev/null -w "%{http_code}" \
        "https://arweave.net/${TX_ID}" 2>/dev/null || echo "000")

    if [ "$HTTP_CODE" = "200" ]; then
        pass "TX accessible at gateway (HTTP $HTTP_CODE)"
    elif [ "$HTTP_CODE" = "202" ]; then
        pass "TX pending confirmation (HTTP 202) — this is expected for new uploads"
    else
        skip "Gateway returned HTTP $HTTP_CODE (transactions take ~10-20 min to confirm)"
    fi
fi

echo ""
echo "NOTE: Arweave uploads are permanent. TX ID: ${TX_ID:-unknown}"

finish
