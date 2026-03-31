#!/bin/bash
#
# Integration test for Filebase upload and download scripts.
# Requires: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, FILEBASE_BUCKET in .env
#
source "$(dirname "$0")/helpers.sh"

echo "=== Filebase Integration Test ==="
echo ""

# Check credentials
if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ] || [ -z "$FILEBASE_BUCKET" ]; then
    skip "Filebase credentials not set in .env — skipping Filebase tests"
    exit 0
fi

if ! command -v aws >/dev/null 2>&1; then
    skip "AWS CLI not installed — skipping Filebase tests"
    exit 0
fi

TEST_DIR=$(mktemp -d)
trap "cleanup_test_dir '$TEST_DIR'; rm -rf '$TEST_DIR'" EXIT

ARCHIVE_FILE=$(create_test_archive "$TEST_DIR")
S3_KEY="sparkdream-archives/blocks_1_to_3.jsonl.gz"

# -------------------------------------------------------------------------
# Test 1: Upload succeeds and returns a CID
# -------------------------------------------------------------------------
echo "Test 1: Upload archive to Filebase"

FILEBASE_BUCKET="$FILEBASE_BUCKET" \
    "$SCRIPTS_DIR/filebase-upload.sh" "$TEST_DIR" > "$TEST_DIR/upload.log" 2>&1

if [ $? -ne 0 ]; then
    fail "Upload script exited with error"
    cat "$TEST_DIR/upload.log"
else
    pass "Upload script completed"
fi

# Check manifest was created with a CID
if [ -f "$TEST_DIR/filebase-manifest.csv" ]; then
    CID=$(tail -1 "$TEST_DIR/filebase-manifest.csv" | cut -d',' -f4)
    if [ -n "$CID" ] && [ "$CID" != "cid" ] && [ "$CID" != "None" ]; then
        pass "Manifest contains CID: $CID"
    else
        # CID may not be immediately available
        skip "CID not yet available (Filebase may still be processing)"
    fi
else
    fail "Manifest file not created"
fi

# Check uploaded tracker
if grep -q "blocks_1_to_3.jsonl.gz" "$TEST_DIR/.filebase-uploaded" 2>/dev/null; then
    pass "Uploaded tracker recorded the file"
else
    fail "Uploaded tracker missing entry"
fi

# -------------------------------------------------------------------------
# Test 2: Re-run skips already uploaded file
# -------------------------------------------------------------------------
echo ""
echo "Test 2: Re-run skips already uploaded"

OUTPUT=$(FILEBASE_BUCKET="$FILEBASE_BUCKET" \
    "$SCRIPTS_DIR/filebase-upload.sh" "$TEST_DIR" 2>&1)

if echo "$OUTPUT" | grep -q "Skipped:.*1"; then
    pass "Re-run correctly skipped the file"
else
    fail "Re-run did not skip"
    echo "$OUTPUT"
fi

# -------------------------------------------------------------------------
# Test 3: Verify object exists in S3
# -------------------------------------------------------------------------
echo ""
echo "Test 3: Verify object exists in bucket"

if aws --endpoint-url "https://s3.filebase.com" \
    s3api head-object --bucket "$FILEBASE_BUCKET" --key "$S3_KEY" >/dev/null 2>&1; then
    pass "Object exists in bucket at $S3_KEY"
else
    fail "Object not found in bucket"
fi

# -------------------------------------------------------------------------
# Test 4: Download round-trip via archive-download.sh
# -------------------------------------------------------------------------
if [ -n "$CID" ] && [ "$CID" != "cid" ] && [ "$CID" != "None" ]; then
    echo ""
    echo "Test 4: Download archive via archive-download.sh"

    RESTORE_DIR="$TEST_DIR/restored"
    mkdir -p "$RESTORE_DIR"

    FILEBASE_BUCKET="$FILEBASE_BUCKET" \
        "$SCRIPTS_DIR/archive-download.sh" filebase -a \
        -m "$TEST_DIR/filebase-manifest.csv" \
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

    OUTPUT=$(FILEBASE_BUCKET="$FILEBASE_BUCKET" \
        "$SCRIPTS_DIR/archive-download.sh" filebase -a \
        -m "$TEST_DIR/filebase-manifest.csv" \
        -d "$RESTORE_DIR" 2>&1 || true)

    if echo "$OUTPUT" | grep -q "SKIP.*already exists"; then
        pass "Re-download correctly skipped existing file"
    else
        fail "Re-download did not skip existing file"
        echo "$OUTPUT"
    fi
else
    skip "CID not available — skipping download test"
fi

# -------------------------------------------------------------------------
# Cleanup: Delete the test object
# -------------------------------------------------------------------------
echo ""
echo "Cleanup: Deleting test object"

if aws --endpoint-url "https://s3.filebase.com" \
    s3 rm "s3://${FILEBASE_BUCKET}/${S3_KEY}" >/dev/null 2>&1; then
    pass "Deleted test object from bucket"
else
    skip "Could not delete test object (may need manual cleanup)"
fi

finish
