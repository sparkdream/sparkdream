#!/bin/bash

echo "=========================================="
echo "Snapshot: Copying Chain Data Directory"
echo "=========================================="
echo ""

# Usage: snapshot_datadir.sh [snapshot_name] [output_dir]
# If output_dir not provided, uses test/snapshots
#
# Honors $CHAIN_HOME if set (used by parallel runners); defaults to
# ~/.sparkdream. The pkill/check is scoped to the active CHAIN_HOME so
# concurrent suites against different home dirs don't disturb each other.
CHAIN_HOME_DIR="${CHAIN_HOME:-$HOME/.sparkdream}"

# Shared safe-rmtree helper (CLAUDE.md forbids `rm -rf`).
SCRIPT_DIR_FOR_HELPERS="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
# shellcheck source=./_safe_rm.sh
source "$SCRIPT_DIR_FOR_HELPERS/_safe_rm.sh"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
SNAPSHOT_NAME="${1:-datadir_$TIMESTAMP}"
SNAPSHOT_DIR="${2:-$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/snapshots}"
SNAPSHOT_PATH="$SNAPSHOT_DIR/$SNAPSHOT_NAME"

# Create snapshots directory
mkdir -p "$SNAPSHOT_PATH"

echo "Creating snapshot: $SNAPSHOT_NAME"
echo "Chain home: $CHAIN_HOME_DIR"
echo ""

# Get current block height (use --home so we hit the right chain when CHAIN_HOME is set)
BLOCK_HEIGHT=$(sparkdreamd status --home "$CHAIN_HOME_DIR" 2>&1 | jq -r '.sync_info.latest_block_height // "unknown"')
echo "Current block height: $BLOCK_HEIGHT"
echo ""

# Stop the chain gracefully (SIGTERM allows LevelDB to flush). Scope the kill
# to the active CHAIN_HOME so other parallel suites are unaffected.
echo "→ Stopping chain for consistent snapshot (home=$CHAIN_HOME_DIR)..."
if [ "$CHAIN_HOME_DIR" = "$HOME/.sparkdream" ]; then
    # Default-home path: preserve original broad-kill behavior (covers ignite
    # processes and any orphaned sparkdreamd lacking explicit --home).
    pkill ignite 2>/dev/null
    pkill sparkdreamd 2>/dev/null
else
    # Parallel-suite path: only kill processes bound to this chain home.
    pkill -f "sparkdreamd .*--home $CHAIN_HOME_DIR" 2>/dev/null
fi
# Wait for graceful shutdown (up to 15 seconds)
for i in $(seq 1 15); do
    if [ "$CHAIN_HOME_DIR" = "$HOME/.sparkdream" ]; then
        pgrep -x sparkdreamd > /dev/null 2>&1 || break
    else
        pgrep -f "sparkdreamd .*--home $CHAIN_HOME_DIR" > /dev/null 2>&1 || break
    fi
    sleep 1
done
# Force kill only if graceful shutdown failed
if [ "$CHAIN_HOME_DIR" = "$HOME/.sparkdream" ]; then
    if pgrep -x sparkdreamd > /dev/null 2>&1; then
        echo "  → Graceful shutdown timed out, forcing..."
        pkill -9 sparkdreamd 2>/dev/null
        sleep 2
    fi
else
    if pgrep -f "sparkdreamd .*--home $CHAIN_HOME_DIR" > /dev/null 2>&1; then
        echo "  → Graceful shutdown timed out, forcing..."
        pkill -9 -f "sparkdreamd .*--home $CHAIN_HOME_DIR" 2>/dev/null
        sleep 2
    fi
fi

# Copy the entire data directory (remove existing to prevent nesting).
# Also remove any stray symlink at the same path (the `-d` test alone
# wouldn't catch a dangling symlink, and `cp -r` would then nest under it).
echo "→ Copying $CHAIN_HOME_DIR to snapshot..."
if [ -e "$SNAPSHOT_PATH/sparkdream_data" ] || [ -L "$SNAPSHOT_PATH/sparkdream_data" ]; then
    echo "  → Removing existing snapshot data..."
    safe_rmtree "$SNAPSHOT_PATH/sparkdream_data" || {
        echo "  [FAIL] Failed to remove existing snapshot at $SNAPSHOT_PATH/sparkdream_data"
        exit 1
    }
fi
cp -r "$CHAIN_HOME_DIR" "$SNAPSHOT_PATH/sparkdream_data"

if [ $? -eq 0 ]; then
    echo "  [ OK ] Data directory copied"
else
    echo "  [FAIL] Failed to copy data directory"
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
echo "  [ OK ] Metadata saved"

# Get data directory size
DIR_SIZE=$(du -sh "$SNAPSHOT_PATH/sparkdream_data" | cut -f1)

# Create restoration script.
# Using a single-quoted heredoc so $CHAIN_HOME / $HOME stay literal and are
# evaluated at restore-time — letting the same snapshot land in any chain
# home (parallel suites set CHAIN_HOME; legacy callers get ~/.sparkdream).
cat > "$SNAPSHOT_PATH/restore.sh" <<'RESTORE_SCRIPT'
#!/bin/bash

echo "=========================================="
echo "Restore: Loading Chain Data Directory"
echo "=========================================="
echo ""

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
CHAIN_HOME_DIR="${CHAIN_HOME:-$HOME/.sparkdream}"
echo "Restoring into: $CHAIN_HOME_DIR"

# Stop any running chain — scoped to this home dir when CHAIN_HOME is set.
echo "→ Stopping any running chain..."
if [ "$CHAIN_HOME_DIR" = "$HOME/.sparkdream" ]; then
    pkill sparkdreamd 2>/dev/null
    for i in $(seq 1 10); do
        if ! pgrep -x sparkdreamd > /dev/null 2>&1; then break; fi
        sleep 1
    done
    pkill -9 sparkdreamd 2>/dev/null
else
    pkill -f "sparkdreamd .*--home $CHAIN_HOME_DIR" 2>/dev/null
    for i in $(seq 1 10); do
        if ! pgrep -f "sparkdreamd .*--home $CHAIN_HOME_DIR" > /dev/null 2>&1; then break; fi
        sleep 1
    done
    pkill -9 -f "sparkdreamd .*--home $CHAIN_HOME_DIR" 2>/dev/null
fi
sleep 1

# Backup current state (optional)
if [ -d "$CHAIN_HOME_DIR" ]; then
    BACKUP_NAME="backup_$(date +%Y%m%d_%H%M%S)"
    echo "→ Backing up current state to ${CHAIN_HOME_DIR}_$BACKUP_NAME"
    mv "$CHAIN_HOME_DIR" "${CHAIN_HOME_DIR}_$BACKUP_NAME"
fi

# Restore from snapshot
echo "→ Restoring data directory from snapshot..."
mkdir -p "$(dirname "$CHAIN_HOME_DIR")"
cp -r "$SCRIPT_DIR/sparkdream_data" "$CHAIN_HOME_DIR"

if [ $? -eq 0 ]; then
    echo "  [ OK ] Data directory restored"
else
    echo "  [FAIL] Failed to restore data directory"
    exit 1
fi

# Reset priv_validator_state.json to prevent height regression errors.
# On a single-validator dev chain, the validator state file may record a
# height higher than the restored app state (e.g. due to hard kills during
# snapshot capture). CometBFT refuses to sign at a lower height as a
# double-sign safety measure. Resetting it is safe for local dev chains.
PVS_FILE="$CHAIN_HOME_DIR/data/priv_validator_state.json"
if [ -f "$PVS_FILE" ]; then
    echo "→ Resetting priv_validator_state.json..."
    cat > "$PVS_FILE" <<PVSTATE
{
  "height": "0",
  "round": 0,
  "step": 0
}
PVSTATE
    echo "  [ OK ] Validator state reset"
fi

echo ""
echo "[ OK ] Snapshot restored successfully!"
echo ""
echo "Start the chain with:"
echo "  ignite chain serve --skip-proto"
echo ""
echo "Or manually with:"
echo "  sparkdreamd start --home $CHAIN_HOME_DIR"
echo ""

# Show snapshot metadata
if [ -f "$SCRIPT_DIR/metadata.json" ]; then
    echo "Snapshot info:"
    cat "$SCRIPT_DIR/metadata.json" | jq '.'
fi

RESTORE_SCRIPT

chmod +x "$SNAPSHOT_PATH/restore.sh"
echo "  [ OK ] Restore script created"

echo ""
echo "=========================================="
echo "[ OK ] Snapshot Created Successfully!"
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
