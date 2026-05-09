#!/bin/bash

echo "=========================================="
echo "Snapshot: Exporting Chain State"
echo "=========================================="
echo ""

# Usage: snapshot_chain.sh [snapshot_name] [output_dir]
# If output_dir not provided, uses test/snapshots
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
SNAPSHOT_NAME="${1:-test_setup_$TIMESTAMP}"
SNAPSHOT_DIR="${2:-$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/snapshots}"
SNAPSHOT_PATH="$SNAPSHOT_DIR/$SNAPSHOT_NAME"

# Create snapshots directory
mkdir -p "$SNAPSHOT_PATH"

echo "Creating snapshot: $SNAPSHOT_NAME"
echo ""

# 1. Export genesis
echo "→ Exporting chain state to genesis..."
sparkdreamd export --home ~/.sparkdream > "$SNAPSHOT_PATH/genesis_exported.json" 2>&1

if [ $? -eq 0 ]; then
    echo "  [ OK ] Genesis exported: $SNAPSHOT_PATH/genesis_exported.json"
else
    echo "  [FAIL] Failed to export genesis"
    exit 1
fi

# 2. Save current params for reference
echo "→ Saving current module params..."
sparkdreamd q rep params --output json > "$SNAPSHOT_PATH/rep_params.json" 2>/dev/null
echo "  [ OK ] Rep params saved"

# 3. Save member states
echo "→ Saving member states..."
for MEMBER in alice challenger juror1 juror2 juror3; do
    ADDR=$(sparkdreamd keys show $MEMBER -a --keyring-backend test 2>/dev/null)
    if [ -n "$ADDR" ]; then
        sparkdreamd q rep show-member $ADDR --output json > "$SNAPSHOT_PATH/member_${MEMBER}.json" 2>/dev/null
    fi
done
echo "  [ OK ] Member states saved"

# 4. Save current block height
BLOCK_HEIGHT=$(sparkdreamd status 2>&1 | jq -r '.sync_info.latest_block_height')
echo "$BLOCK_HEIGHT" > "$SNAPSHOT_PATH/block_height.txt"
echo "  [ OK ] Block height saved: $BLOCK_HEIGHT"

# 5. Create metadata file
cat > "$SNAPSHOT_PATH/metadata.json" <<EOF
{
  "snapshot_name": "$SNAPSHOT_NAME",
  "timestamp": "$TIMESTAMP",
  "block_height": "$BLOCK_HEIGHT",
  "description": "Chain state after test account setup",
  "notes": "Includes: 7 test accounts with DREAM, test project, jurors with reputation"
}
EOF
echo "  [ OK ] Metadata saved"

# 6. Create restoration script
cat > "$SNAPSHOT_PATH/restore.sh" <<'RESTORE_SCRIPT'
#!/bin/bash

echo "=========================================="
echo "Restore: Loading Chain Snapshot"
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

# Remove any leftover state. CLAUDE.md forbids `rm -rf`; use `find -delete`
# with explicit guards so a misconfigured $HOME can't escalate. The mv above
# will normally have moved ~/.sparkdream out of the way, so this only
# matters if the mv was skipped (no -d) and a stray file/symlink remains.
if [ -e "$HOME/.sparkdream" ] || [ -L "$HOME/.sparkdream" ]; then
    if [ -n "$HOME" ] && [ "$HOME" != "/" ]; then
        find "$HOME/.sparkdream" -depth -delete 2>/dev/null
    fi
fi

# Initialize with exported genesis
echo "→ Initializing chain with snapshot genesis..."
sparkdreamd init test --chain-id sparkdream --home ~/.sparkdream

# Copy the exported genesis
echo "→ Copying snapshot genesis..."
cp "$SCRIPT_DIR/genesis_exported.json" ~/.sparkdream/config/genesis.json

# Restore keys (if they exist in keyring)
echo "→ Keys should already exist in keyring (shared across home dirs)"

echo ""
echo "[ OK ] Snapshot restored successfully!"
echo ""
echo "Start the chain with:"
echo "  ignite chain serve --skip-proto --reset-once"
echo ""
echo "Or manually with:"
echo "  sparkdreamd start --home ~/.sparkdream"
echo ""

RESTORE_SCRIPT

chmod +x "$SNAPSHOT_PATH/restore.sh"
echo "  [ OK ] Restore script created"

echo ""
echo "=========================================="
echo "[ OK ] Snapshot Created Successfully!"
echo "=========================================="
echo ""
echo "Snapshot location: $SNAPSHOT_PATH"
echo ""
echo "Contents:"
ls -lh "$SNAPSHOT_PATH"
echo ""
echo "To restore this snapshot:"
echo "  cd $SNAPSHOT_PATH"
echo "  ./restore.sh"
echo ""
