#!/bin/bash
#
# Unit test for archive-download.sh.
#
# Tests argument validation, manifest parsing, mode filtering, dry-run,
# and skip-existing logic using synthetic manifests. No network calls.
#
# Live download round-trips are tested in each service's own test
# (storacha_test.sh, pinata_test.sh, filebase_test.sh).
#
source "$(dirname "$0")/helpers.sh"

echo "=== Archive Manifest Test ==="
echo ""

DOWNLOAD_SCRIPT="$SCRIPTS_DIR/archive-download.sh"

TEST_DIR=$(mktemp -d)
DOWNLOAD_DIR="$TEST_DIR/downloads"
trap "rm -rf '$TEST_DIR'" EXIT

mkdir -p "$DOWNLOAD_DIR"

# -------------------------------------------------------------------------
# Create synthetic manifests for testing
# -------------------------------------------------------------------------

# Storacha-style manifest (file,from_block,to_block,cid,gateway_url,uploaded_at)
cat > "$TEST_DIR/storacha-manifest.csv" <<'EOF'
file,from_block,to_block,cid,gateway_url,uploaded_at
blocks_1_to_100.jsonl.gz,1,100,bafyfake1,https://bafyfake1.ipfs.w3s.link,2026-01-01T00:00:00Z
blocks_101_to_200.jsonl.gz,101,200,bafyfake2,https://bafyfake2.ipfs.w3s.link,2026-01-01T00:01:00Z
blocks_201_to_300.jsonl.gz,201,300,bafyfake3,https://bafyfake3.ipfs.w3s.link,2026-01-01T00:02:00Z
EOF

# Arweave-style manifest (file,from_block,to_block,tx_id,arweave_url,file_size_bytes,uploaded_at)
cat > "$TEST_DIR/arweave-manifest.csv" <<'EOF'
file,from_block,to_block,tx_id,arweave_url,file_size_bytes,uploaded_at
blocks_1_to_100.jsonl.gz,1,100,txid_fake_1,https://arweave.net/txid_fake_1,1024,2026-01-01T00:00:00Z
blocks_101_to_200.jsonl.gz,101,200,txid_fake_2,https://arweave.net/txid_fake_2,2048,2026-01-01T00:01:00Z
EOF

# Pinata-style manifest (file,from_block,to_block,cid,gateway_url,file_size,uploaded_at)
cat > "$TEST_DIR/pinata-manifest.csv" <<'EOF'
file,from_block,to_block,cid,gateway_url,file_size,uploaded_at
blocks_1_to_100.jsonl.gz,1,100,QmFakePinata1,https://gateway.pinata.cloud/ipfs/QmFakePinata1,1024,2026-01-01T00:00:00Z
blocks_101_to_200.jsonl.gz,101,200,QmFakePinata2,https://gateway.pinata.cloud/ipfs/QmFakePinata2,2048,2026-01-01T00:01:00Z
EOF

# Filebase-style manifest (file,from_block,to_block,cid,gateway_url,file_size,uploaded_at)
cat > "$TEST_DIR/filebase-manifest.csv" <<'EOF'
file,from_block,to_block,cid,gateway_url,file_size,uploaded_at
blocks_1_to_100.jsonl.gz,1,100,QmFakeFilebase1,https://ipfs.filebase.io/ipfs/QmFakeFilebase1,1024,2026-01-01T00:00:00Z
blocks_101_to_200.jsonl.gz,101,200,QmFakeFilebase2,https://ipfs.filebase.io/ipfs/QmFakeFilebase2,2048,2026-01-01T00:01:00Z
EOF

# Jackal-style manifest (file,from_block,to_block,fid,file_size,uploaded_at)
cat > "$TEST_DIR/jackal-manifest.csv" <<'EOF'
file,from_block,to_block,fid,file_size,uploaded_at
blocks_1_to_100.jsonl.gz,1,100,jkl_fid_fake_1,1024,2026-01-01T00:00:00Z
blocks_101_to_200.jsonl.gz,101,200,jkl_fid_fake_2,2048,2026-01-01T00:01:00Z
EOF

# -------------------------------------------------------------------------
# Test 1: Errors on missing service argument
# -------------------------------------------------------------------------
echo "Test 1: Error on missing service argument"

OUTPUT=$("$DOWNLOAD_SCRIPT" 2>&1 || true)
if echo "$OUTPUT" | grep -q "ERROR.*Service name is required"; then
    pass "Exits with error when no service given"
else
    fail "Did not report missing service"
fi

# -------------------------------------------------------------------------
# Test 2: Errors on unknown service
# -------------------------------------------------------------------------
echo "Test 2: Error on unknown service"

OUTPUT=$("$DOWNLOAD_SCRIPT" fakeservice -a -m "$TEST_DIR/storacha-manifest.csv" -d "$DOWNLOAD_DIR" 2>&1 || true)
if echo "$OUTPUT" | grep -q "ERROR.*Unknown service"; then
    pass "Exits with error for unknown service"
else
    fail "Did not report unknown service"
fi

# -------------------------------------------------------------------------
# Test 3: Errors on missing mode
# -------------------------------------------------------------------------
echo "Test 3: Error on missing download mode"

OUTPUT=$("$DOWNLOAD_SCRIPT" storacha -m "$TEST_DIR/storacha-manifest.csv" -d "$DOWNLOAD_DIR" 2>&1 || true)
if echo "$OUTPUT" | grep -q "ERROR.*Specify a download mode"; then
    pass "Exits with error when no mode given"
else
    fail "Did not report missing mode"
fi

# -------------------------------------------------------------------------
# Test 4: Errors on missing manifest
# -------------------------------------------------------------------------
echo "Test 4: Error on missing manifest"

OUTPUT=$("$DOWNLOAD_SCRIPT" storacha -a -m "$TEST_DIR/nonexistent.csv" -d "$DOWNLOAD_DIR" 2>&1 || true)
if echo "$OUTPUT" | grep -q "ERROR.*Manifest not found"; then
    pass "Exits with error for missing manifest"
else
    fail "Did not report missing manifest"
fi

# -------------------------------------------------------------------------
# Test 5: Dry-run all mode lists all entries
# -------------------------------------------------------------------------
echo ""
echo "Test 5: Dry-run -a lists all manifest entries"

OUTPUT=$("$DOWNLOAD_SCRIPT" storacha -a -n -m "$TEST_DIR/storacha-manifest.csv" -d "$DOWNLOAD_DIR" 2>&1)

ENTRY_COUNT=$(echo "$OUTPUT" | grep -c "DRY RUN" || true)
if [ "$ENTRY_COUNT" -eq 3 ]; then
    pass "Dry-run listed all 3 entries"
else
    fail "Expected 3 dry-run entries, got $ENTRY_COUNT"
    echo "$OUTPUT"
fi

# -------------------------------------------------------------------------
# Test 6: Dry-run block mode finds correct archive
# -------------------------------------------------------------------------
echo ""
echo "Test 6: Dry-run -b finds correct archive for block 150"

OUTPUT=$("$DOWNLOAD_SCRIPT" storacha -b 150 -n -m "$TEST_DIR/storacha-manifest.csv" -d "$DOWNLOAD_DIR" 2>&1)

if echo "$OUTPUT" | grep -q "blocks_101_to_200"; then
    pass "Found correct archive for block 150"
else
    fail "Did not find blocks_101_to_200 for block 150"
    echo "$OUTPUT"
fi

# Ensure it did NOT match the other files
if echo "$OUTPUT" | grep -q "blocks_1_to_100"; then
    fail "Incorrectly matched blocks_1_to_100"
else
    pass "Correctly excluded non-matching archives"
fi

# -------------------------------------------------------------------------
# Test 7: Dry-run range mode finds overlapping archives
# -------------------------------------------------------------------------
echo ""
echo "Test 7: Dry-run -r finds overlapping archives for range 50-150"

OUTPUT=$("$DOWNLOAD_SCRIPT" storacha -r 50-150 -n -m "$TEST_DIR/storacha-manifest.csv" -d "$DOWNLOAD_DIR" 2>&1)

MATCH_COUNT=$(echo "$OUTPUT" | grep -c "DRY RUN" || true)
if [ "$MATCH_COUNT" -eq 2 ]; then
    pass "Found 2 overlapping archives for range 50-150"
else
    fail "Expected 2 overlapping archives, got $MATCH_COUNT"
    echo "$OUTPUT"
fi

if echo "$OUTPUT" | grep -q "blocks_201_to_300"; then
    fail "Incorrectly matched blocks_201_to_300"
else
    pass "Correctly excluded non-overlapping archive"
fi

# -------------------------------------------------------------------------
# Test 8: Dry-run file mode finds specific file
# -------------------------------------------------------------------------
echo ""
echo "Test 8: Dry-run -f finds specific file"

OUTPUT=$("$DOWNLOAD_SCRIPT" storacha -f blocks_201_to_300.jsonl.gz -n -m "$TEST_DIR/storacha-manifest.csv" -d "$DOWNLOAD_DIR" 2>&1)

if echo "$OUTPUT" | grep -q "blocks_201_to_300"; then
    pass "Found specific file by name"
else
    fail "Did not find blocks_201_to_300.jsonl.gz"
    echo "$OUTPUT"
fi

MATCH_COUNT=$(echo "$OUTPUT" | grep -c "DRY RUN" || true)
if [ "$MATCH_COUNT" -eq 1 ]; then
    pass "Only matched the one requested file"
else
    fail "Expected 1 match, got $MATCH_COUNT"
fi

# -------------------------------------------------------------------------
# Test 9: No matches returns gracefully
# -------------------------------------------------------------------------
echo ""
echo "Test 9: No matches for out-of-range block"

OUTPUT=$("$DOWNLOAD_SCRIPT" storacha -b 99999 -n -m "$TEST_DIR/storacha-manifest.csv" -d "$DOWNLOAD_DIR" 2>&1)

if echo "$OUTPUT" | grep -q "No matching archives"; then
    pass "Gracefully reports no matches"
else
    fail "Did not report no matches"
    echo "$OUTPUT"
fi

# -------------------------------------------------------------------------
# Test 10: Dry-run works with all service manifest formats
# -------------------------------------------------------------------------
echo ""
echo "Test 10: Dry-run works with all service manifest formats"

for SERVICE in arweave pinata filebase jackal; do
    OUTPUT=$("$DOWNLOAD_SCRIPT" "$SERVICE" -a -n -m "$TEST_DIR/${SERVICE}-manifest.csv" -d "$DOWNLOAD_DIR" 2>&1)

    ENTRY_COUNT=$(echo "$OUTPUT" | grep -c "DRY RUN" || true)
    if [ "$ENTRY_COUNT" -eq 2 ]; then
        pass "$SERVICE: dry-run listed all 2 entries"
    else
        fail "$SERVICE: expected 2 dry-run entries, got $ENTRY_COUNT"
        echo "$OUTPUT"
    fi
done

# -------------------------------------------------------------------------
# Test 11: Skip-existing logic
# -------------------------------------------------------------------------
echo ""
echo "Test 11: Skips files that already exist in output dir"

# Create a fake file that looks like it was already downloaded
touch "$DOWNLOAD_DIR/blocks_1_to_100.jsonl.gz"

# In dry-run the skip-existing check happens inside the download function,
# which is only called in non-dry-run mode. Verify with a real (but fake-URL) attempt:
OUTPUT=$("$DOWNLOAD_SCRIPT" storacha -f blocks_1_to_100.jsonl.gz -m "$TEST_DIR/storacha-manifest.csv" -d "$DOWNLOAD_DIR" 2>&1 || true)

if echo "$OUTPUT" | grep -q "SKIP.*already exists"; then
    pass "Skipped already-existing file"
else
    fail "Did not skip existing file"
    echo "$OUTPUT"
fi

rm -f "$DOWNLOAD_DIR/blocks_1_to_100.jsonl.gz"

finish
