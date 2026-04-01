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
trap "cleanup_test_dir '$TEST_DIR'; rm -rf '$TEST_DIR'" EXIT

ARCHIVE_FILE=$(create_test_archive "$TEST_DIR")

# Pre-clean: delete the test file from the vault if it exists from a prior run
if [ "$MODE" = "vault" ]; then
    echo "Pre-clean: removing stale test file from vault..."
    "$SCRIPTS_DIR/jackal-upload.sh" delete-file blocks_1_to_3.jsonl.gz > "$TEST_DIR/preclean.log" 2>&1 || true
fi

# -------------------------------------------------------------------------
# Test 1: Upload succeeds and records to manifest
# -------------------------------------------------------------------------
echo "Test 1: Upload archive to Jackal ($MODE mode)"

if "$SCRIPTS_DIR/jackal-upload.sh" "$TEST_DIR" > "$TEST_DIR/upload.log" 2>&1; then
    pass "Upload script completed"
else
    fail "Upload script exited with error"
    cat "$TEST_DIR/upload.log"
fi

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

if echo "$OUTPUT" | grep -qi "skipped.*1"; then
    pass "Re-run correctly skipped the file"
else
    fail "Re-run did not skip"
    echo "$OUTPUT"
fi

# -------------------------------------------------------------------------
# Test 3: Download round-trip via archive-download.sh
# -------------------------------------------------------------------------
if [ "$MODE" = "vault" ]; then
    echo ""
    echo "Test 3: Download archive via archive-download.sh (merkle hash)"

    # Verify manifest has a merkle hash (vault manifest col5)
    MERKLE=$(tail -1 "$TEST_DIR/jackal-manifest.csv" | cut -d',' -f5)
    if [ -z "$MERKLE" ] || [ "$MERKLE" = "merkle" ]; then
        fail "Manifest has no merkle hash — download requires merkle"
    else
        pass "Manifest contains merkle hash: ${MERKLE:0:16}..."

        RESTORE_DIR="$TEST_DIR/restored"
        mkdir -p "$RESTORE_DIR"

        "$SCRIPTS_DIR/archive-download.sh" jackal -a \
            -m "$TEST_DIR/jackal-manifest.csv" \
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

        # Test 4: Re-download skips existing file
        echo ""
        echo "Test 4: Re-download skips existing file"

        OUTPUT=$("$SCRIPTS_DIR/archive-download.sh" jackal -a \
            -m "$TEST_DIR/jackal-manifest.csv" \
            -d "$RESTORE_DIR" 2>&1 || true)

        if echo "$OUTPUT" | grep -q "SKIP.*already exists"; then
            pass "Re-download correctly skipped existing file"
        else
            fail "Re-download did not skip existing file"
            echo "$OUTPUT"
        fi
    fi
elif [ "$MODE" = "pin" ] && [ -n "$ID" ] && [ "$ID" != "cid" ]; then
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

# -------------------------------------------------------------------------
# Cleanup: delete the test file from the vault
# -------------------------------------------------------------------------
if [ "$MODE" = "vault" ]; then
    echo ""
    echo "Cleanup: Deleting test file from vault"
    if "$SCRIPTS_DIR/jackal-upload.sh" delete-file blocks_1_to_3.jsonl.gz > "$TEST_DIR/cleanup.log" 2>&1; then
        pass "Deleted test file from vault"
    else
        skip "Could not delete test file (may need manual cleanup)"
    fi
fi

finish
