#!/bin/bash
#
# Integration test for Storacha upload and download scripts.
# Requires: storacha CLI installed and authenticated (storacha login)
#
source "$(dirname "$0")/helpers.sh"

echo "=== Storacha Integration Test ==="
echo ""

# Check CLI and auth
if ! command -v storacha >/dev/null 2>&1; then
    skip "storacha CLI not installed — skipping Storacha tests"
    exit 0
fi

if ! storacha whoami >/dev/null 2>&1; then
    skip "storacha not authenticated — run 'storacha login' first"
    exit 0
fi

TEST_DIR=$(mktemp -d)
trap "cleanup_test_dir '$TEST_DIR'; rm -rf '$TEST_DIR'" EXIT

ARCHIVE_FILE=$(create_test_archive "$TEST_DIR")

# -------------------------------------------------------------------------
# Test 1: Upload succeeds and returns a CID
# -------------------------------------------------------------------------
echo "Test 1: Upload archive to Storacha"

"$SCRIPTS_DIR/storacha-upload.sh" "$TEST_DIR" > "$TEST_DIR/upload.log" 2>&1

if [ $? -ne 0 ]; then
    fail "Upload script exited with error"
    cat "$TEST_DIR/upload.log"
else
    pass "Upload script completed"
fi

# Check manifest was created with a CID
if [ -f "$TEST_DIR/storacha-manifest.csv" ]; then
    CID=$(tail -1 "$TEST_DIR/storacha-manifest.csv" | cut -d',' -f4)
    if [ -n "$CID" ] && [ "$CID" != "cid" ]; then
        pass "Manifest contains CID: $CID"
    else
        fail "Manifest has no CID"
    fi
else
    fail "Manifest file not created"
fi

# Check uploaded tracker
if grep -q "blocks_1_to_3.jsonl.gz" "$TEST_DIR/.storacha-uploaded" 2>/dev/null; then
    pass "Uploaded tracker recorded the file"
else
    fail "Uploaded tracker missing entry"
fi

# -------------------------------------------------------------------------
# Test 2: Re-run skips already uploaded file
# -------------------------------------------------------------------------
echo ""
echo "Test 2: Re-run skips already uploaded"

OUTPUT=$("$SCRIPTS_DIR/storacha-upload.sh" "$TEST_DIR" 2>&1)

if echo "$OUTPUT" | grep -q "Skipped:.*1"; then
    pass "Re-run correctly skipped the file"
else
    fail "Re-run did not skip"
    echo "$OUTPUT"
fi

# -------------------------------------------------------------------------
# Test 3: CID is retrievable via gateway
# -------------------------------------------------------------------------
if [ -n "$CID" ] && [ "$CID" != "cid" ]; then
    echo ""
    echo "Test 3: Verify CID is accessible"

    HTTP_CODE=$(curl -sf -o /dev/null -w "%{http_code}" \
        "https://${CID}.ipfs.w3s.link" 2>/dev/null || echo "000")

    if [ "$HTTP_CODE" = "200" ]; then
        pass "File accessible at gateway (HTTP $HTTP_CODE)"
    else
        skip "Gateway returned HTTP $HTTP_CODE (may need time to propagate)"
    fi
fi

# -------------------------------------------------------------------------
# Test 4: Download round-trip via archive-download.sh
# -------------------------------------------------------------------------
if [ -n "$CID" ] && [ "$CID" != "cid" ]; then
    echo ""
    echo "Test 4: Download archive via archive-download.sh"

    RESTORE_DIR="$TEST_DIR/restored"
    mkdir -p "$RESTORE_DIR"

    "$SCRIPTS_DIR/archive-download.sh" storacha -a \
        -m "$TEST_DIR/storacha-manifest.csv" \
        -d "$RESTORE_DIR" > "$TEST_DIR/download.log" 2>&1
    DOWNLOAD_EXIT=$?

    if [ $DOWNLOAD_EXIT -ne 0 ]; then
        fail "Download script exited with error"
        cat "$TEST_DIR/download.log"
    elif [ -f "$RESTORE_DIR/blocks_1_to_3.jsonl.gz" ]; then
        pass "Downloaded file exists"

        ORIG_HASH=$(md5sum "$TEST_DIR/blocks_1_to_3.jsonl.gz" | cut -d' ' -f1)
        DOWN_HASH=$(md5sum "$RESTORE_DIR/blocks_1_to_3.jsonl.gz" | cut -d' ' -f1)

        if [ "$ORIG_HASH" = "$DOWN_HASH" ]; then
            pass "Downloaded file matches original (md5: $ORIG_HASH)"
        else
            fail "Downloaded file differs from original (orig: $ORIG_HASH, down: $DOWN_HASH)"
        fi
    else
        fail "Downloaded file not found"
        cat "$TEST_DIR/download.log"
    fi

    # Test 5: Re-download skips existing file
    echo ""
    echo "Test 5: Re-download skips existing file"

    OUTPUT=$("$SCRIPTS_DIR/archive-download.sh" storacha -a \
        -m "$TEST_DIR/storacha-manifest.csv" \
        -d "$RESTORE_DIR" 2>&1 || true)

    if echo "$OUTPUT" | grep -q "SKIP.*already exists"; then
        pass "Re-download correctly skipped existing file"
    else
        fail "Re-download did not skip existing file"
        echo "$OUTPUT"
    fi
fi

# -------------------------------------------------------------------------
# Cleanup: remove test upload from Storacha space
# -------------------------------------------------------------------------
if [ -n "$CID" ] && [ "$CID" != "cid" ]; then
    echo ""
    echo "Cleanup: Removing test upload $CID"
    if storacha rm "$CID" >/dev/null 2>&1; then
        pass "Removed test upload from space"
    else
        skip "Could not remove test upload (may need manual cleanup)"
    fi
fi

finish
