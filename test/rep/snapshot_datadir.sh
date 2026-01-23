#!/bin/bash

echo "=========================================="
echo "Snapshot: Copying Chain Data Directory"
echo "=========================================="
echo ""

SNAPSHOT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/snapshots"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
SNAPSHOT_NAME="${1:-datadir_$TIMESTAMP}"
SNAPSHOT_PATH="$SNAPSHOT_DIR/$SNAPSHOT_NAME"

# Create snapshots directory
mkdir -p "$SNAPSHOT_PATH"

echo "Creating snapshot: $SNAPSHOT_NAME"
echo ""

# Get current block height
BLOCK_HEIGHT=$(sparkdreamd status 2>&1 | jq -r '.sync_info.latest_block_height // "unknown"')
echo "Current block height: $BLOCK_HEIGHT"
echo ""

# Stop the chain first
echo "→ Stopping chain for consistent snapshot..."
pkill -9 ignite
pkill -9 sparkdreamd
sleep 3

# Copy the entire data directory
echo "→ Copying ~/.sparkdream to snapshot..."
cp -r ~/.sparkdream "$SNAPSHOT_PATH/sparkdream_data"

if [ $? -eq 0 ]; then
    echo "  ✅ Data directory copied"
else
    echo "  ❌ Failed to copy data directory"
    exit 1
fi

# Save metadata
cat > "$SNAPSHOT_PATH/metadata.json" <<EOF
{
  "snapshot_name": "$SNAPSHOT_NAME",
  "timestamp": "$TIMESTAMP",
  "block_height": "$BLOCK_HEIGHT",
  "description": "Chain data directory after test account setup",
  "data_path": "$SNAPSHOT_PATH/sparkdream_data",
  "notes": "Includes: 7 test accounts with DREAM, test project, jurors with reputation"
}
EOF
echo "  ✅ Metadata saved"

# Save current test environment variables
if [ -f "$( dirname "${BASH_SOURCE[0]}" )/.test_env" ]; then
    cp "$( dirname "${BASH_SOURCE[0]}" )/.test_env" "$SNAPSHOT_PATH/test_env.bak"
    echo "  ✅ Test environment variables saved"
fi

# Get data directory size
DIR_SIZE=$(du -sh "$SNAPSHOT_PATH/sparkdream_data" | cut -f1)

# Create restoration script
cat > "$SNAPSHOT_PATH/restore.sh" <<'RESTORE_SCRIPT'
#!/bin/bash

echo "=========================================="
echo "Restore: Loading Chain Data Directory"
echo "=========================================="
echo ""

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Stop any running chain
echo "→ Stopping any running chain..."
pkill -9 ignite
pkill -9 sparkdreamd
sleep 2

# Backup current state (optional)
if [ -d ~/.sparkdream ]; then
    BACKUP_NAME="backup_$(date +%Y%m%d_%H%M%S)"
    echo "→ Backing up current state to ~/.sparkdream_$BACKUP_NAME"
    mv ~/.sparkdream ~/.sparkdream_$BACKUP_NAME
fi

# Restore from snapshot
echo "→ Restoring data directory from snapshot..."
cp -r "$SCRIPT_DIR/sparkdream_data" ~/.sparkdream

if [ $? -eq 0 ]; then
    echo "  ✅ Data directory restored"
else
    echo "  ❌ Failed to restore data directory"
    exit 1
fi

# Restore test environment (if exists)
if [ -f "$SCRIPT_DIR/test_env.bak" ]; then
    cp "$SCRIPT_DIR/test_env.bak" "$( cd "$( dirname "${BASH_SOURCE[0]}" )" && cd ../.. && pwd )/test/rep/.test_env"
    echo "  ✅ Test environment restored"
fi

echo ""
echo "✅ Snapshot restored successfully!"
echo ""
echo "Start the chain with:"
echo "  ignite chain serve --skip-proto"
echo ""
echo "Or manually with:"
echo "  sparkdreamd start --home ~/.sparkdream"
echo ""

# Show snapshot metadata
if [ -f "$SCRIPT_DIR/metadata.json" ]; then
    echo "Snapshot info:"
    cat "$SCRIPT_DIR/metadata.json" | jq '.'
fi

RESTORE_SCRIPT

chmod +x "$SNAPSHOT_PATH/restore.sh"
echo "  ✅ Restore script created"

# Restart the chain
echo ""
echo "→ Restarting chain..."
cd "$(dirname "${BASH_SOURCE[0]}")/../.." && ignite chain serve --skip-proto > /tmp/chain_after_snapshot.log 2>&1 &
sleep 5

echo ""
echo "=========================================="
echo "✅ Snapshot Created Successfully!"
echo "=========================================="
echo ""
echo "Snapshot location: $SNAPSHOT_PATH"
echo "Data directory size: $DIR_SIZE"
echo ""
echo "Contents:"
ls -lh "$SNAPSHOT_PATH"
echo ""
echo "To restore this snapshot:"
echo "  cd $SNAPSHOT_PATH"
echo "  ./restore.sh"
echo ""
echo "To create a new snapshot with a custom name:"
echo "  ./snapshot_datadir.sh my_custom_name"
echo ""
