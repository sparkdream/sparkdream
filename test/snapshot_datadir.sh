#!/bin/bash

echo "=========================================="
echo "Snapshot: Copying Chain Data Directory"
echo "=========================================="
echo ""

# Usage: snapshot_datadir.sh [snapshot_name] [output_dir]
# If output_dir not provided, uses test/snapshots
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
SNAPSHOT_NAME="${1:-datadir_$TIMESTAMP}"
SNAPSHOT_DIR="${2:-$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/snapshots}"
SNAPSHOT_PATH="$SNAPSHOT_DIR/$SNAPSHOT_NAME"

# Create snapshots directory
mkdir -p "$SNAPSHOT_PATH"

echo "Creating snapshot: $SNAPSHOT_NAME"
echo ""

# Get current block height
BLOCK_HEIGHT=$(sparkdreamd status 2>&1 | jq -r '.sync_info.latest_block_height // "unknown"')
echo "Current block height: $BLOCK_HEIGHT"
echo ""

# Stop the chain gracefully (SIGTERM allows LevelDB to flush)
echo "→ Stopping chain for consistent snapshot..."
pkill ignite 2>/dev/null
pkill sparkdreamd 2>/dev/null
# Wait for graceful shutdown (up to 15 seconds)
for i in $(seq 1 15); do
    if ! pgrep -x sparkdreamd > /dev/null 2>&1; then break; fi
    sleep 1
done
# Force kill only if graceful shutdown failed
if pgrep -x sparkdreamd > /dev/null 2>&1; then
    echo "  → Graceful shutdown timed out, forcing..."
    pkill -9 sparkdreamd 2>/dev/null
    sleep 2
fi

# Copy the entire data directory (remove existing to prevent nesting)
echo "→ Copying ~/.sparkdream to snapshot..."
if [ -d "$SNAPSHOT_PATH/sparkdream_data" ]; then
    echo "  → Removing existing snapshot data..."
    rm -rf "$SNAPSHOT_PATH/sparkdream_data"
fi
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
  "description": "Chain data directory snapshot",
  "data_path": "$SNAPSHOT_PATH/sparkdream_data"
}
EOF
echo "  ✅ Metadata saved"

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
pkill sparkdreamd 2>/dev/null
for i in $(seq 1 10); do
    if ! pgrep -x sparkdreamd > /dev/null 2>&1; then break; fi
    sleep 1
done
pkill -9 sparkdreamd 2>/dev/null
sleep 1

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

# Reset priv_validator_state.json to prevent height regression errors.
# On a single-validator dev chain, the validator state file may record a
# height higher than the restored app state (e.g. due to hard kills during
# snapshot capture). CometBFT refuses to sign at a lower height as a
# double-sign safety measure. Resetting it is safe for local dev chains.
PVS_FILE="$HOME/.sparkdream/data/priv_validator_state.json"
if [ -f "$PVS_FILE" ]; then
    echo "→ Resetting priv_validator_state.json..."
    cat > "$PVS_FILE" <<PVSTATE
{
  "height": "0",
  "round": 0,
  "step": 0
}
PVSTATE
    echo "  ✅ Validator state reset"
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
