#!/bin/bash
#
# setup-archive-mirror.sh
#
# Installs the off-fleet block archive mirror (archive-mirror.sh) on this
# machine as two systemd user timers:
#
#   sparkdream-archive-sync-<chain-id>.timer      archive-mirror sync
#   sparkdream-archive-rebuild-<chain-id>.timer   archive-mirror rebuild
#
# The scripts are copied into <vault>/bin, so the timers keep working
# whatever happens to the repo checkout they were installed from. Re-run
# this script after pulling a newer version to update the copies.
#
# Run it on the archive machine, as the user that owns the vault. That
# machine must not be part of the fleet: the point is that losing every
# node loses no blocks.
#
# Usage:
#   setup-archive-mirror.sh <vault_dir>              # Install or update
#   setup-archive-mirror.sh <vault_dir> --status     # Timers and last results
#   setup-archive-mirror.sh <vault_dir> --disable    # Remove the timers
#
# On first install <vault_dir>/mirror.env is written from these environment
# variables; afterwards edit that file (see archive-mirror.sh for every
# setting, including failure notifications):
#   CHAIN_ID          - Chain id, e.g. sparkdream-test-1 (required)
#   RPC_URLS          - Space-separated RPC endpoints (required)
#   NETWORK           - Build tag for rebuild checks (default: testnet)
#   SRC_REPO          - Chain repo clone with the release tags
#                       (default: the repo this script is run from)
#
# Schedules (systemd OnCalendar expressions), read on every install:
#   SYNC_SCHEDULE     - default "hourly"
#   REBUILD_SCHEDULE  - default "Sun *-*-* 03:30:00"
#
set -eo pipefail

usage() {
    sed -n '2,/^set -eo/{ /^#/s/^# \?//p }' "$0"
    exit "${1:-0}"
}

VAULT_DIR=""
ACTION="enable"
while [ $# -gt 0 ]; do
    case "$1" in
        --disable) ACTION="disable"; shift ;;
        --status) ACTION="status"; shift ;;
        -h|--help) usage 0 ;;
        -*) echo "ERROR: Unknown argument '$1' (try --help)." >&2; exit 1 ;;
        *) VAULT_DIR="$1"; shift ;;
    esac
done
[ -n "$VAULT_DIR" ] || usage 1
[ -d "$VAULT_DIR" ] || { echo "ERROR: Vault directory '$VAULT_DIR' does not exist." >&2; exit 1; }
VAULT_DIR="$(cd "$VAULT_DIR" && pwd)"

command -v systemctl >/dev/null 2>&1 || { echo "ERROR: systemd is required." >&2; exit 1; }
# systemctl --user needs the runtime dir, which a non-login shell (ssh host
# 'command') does not set.
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ENV_FILE="$VAULT_DIR/mirror.env"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

# The chain id names the units, so one machine can mirror several networks.
if [ -f "$ENV_FILE" ]; then
    CHAIN_ID="$(. "$ENV_FILE" && echo "$CHAIN_ID")"
fi
[ -n "$CHAIN_ID" ] || { echo "ERROR: CHAIN_ID is not set and $ENV_FILE does not exist." >&2; exit 1; }
SYNC_UNIT="sparkdream-archive-sync-${CHAIN_ID}"
REBUILD_UNIT="sparkdream-archive-rebuild-${CHAIN_ID}"

if [ "$ACTION" = "status" ]; then
    systemctl --user list-timers --all --no-pager "${SYNC_UNIT}.timer" "${REBUILD_UNIT}.timer" || true
    echo ""
    "$VAULT_DIR/bin/archive-mirror" status -c "$ENV_FILE"
    exit 0
fi

if [ "$ACTION" = "disable" ]; then
    for unit in "$SYNC_UNIT" "$REBUILD_UNIT"; do
        systemctl --user disable --now "${unit}.timer" 2>/dev/null || true
        rm -f "$UNIT_DIR/${unit}.timer" "$UNIT_DIR/${unit}.service"
    done
    systemctl --user daemon-reload
    echo "Archive mirror timers for ${CHAIN_ID} removed. The vault is untouched."
    exit 0
fi

# ---------------------------------------------------------------------------
# Install
# ---------------------------------------------------------------------------
for cmd in curl jq gzip sha256sum flock base64 git; do
    command -v "$cmd" >/dev/null 2>&1 || { echo "ERROR: '$cmd' is required but not installed." >&2; exit 1; }
done
command -v go >/dev/null 2>&1 || echo "WARNING: 'go' not found: rebuild checks need it to build the chain binary (or set SPARKDREAMD_BIN)." >&2

if [ ! -f "$ENV_FILE" ]; then
    [ -n "$RPC_URLS" ] || { echo "ERROR: RPC_URLS is not set." >&2; exit 1; }
    cat > "$ENV_FILE" <<EOF
# Block archive mirror for ${CHAIN_ID}. Settings: see bin/archive-mirror --help
VAULT_DIR="${VAULT_DIR}"
CHAIN_ID="${CHAIN_ID}"
RPC_URLS="${RPC_URLS}"
NETWORK="${NETWORK:-testnet}"
SRC_REPO="${SRC_REPO:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
# Failure notifications (optional): an ntfy topic URL, and a token if needed
#NTFY_URL=""
#NTFY_TOKEN=""
EOF
    chmod 600 "$ENV_FILE"
    echo "Wrote $ENV_FILE"
fi

mkdir -p "$VAULT_DIR/bin" "$VAULT_DIR/logs" "$UNIT_DIR"
install -m 755 "$SCRIPT_DIR/archive-mirror.sh" "$VAULT_DIR/bin/archive-mirror"
install -m 755 "$SCRIPT_DIR/block-archiver.sh" "$VAULT_DIR/bin/block-archiver"

SYNC_SCHEDULE="${SYNC_SCHEDULE:-hourly}"
REBUILD_SCHEDULE="${REBUILD_SCHEDULE:-Sun *-*-* 03:30:00}"
# The user manager's PATH is minimal; rebuild checks need go and git.
UNIT_PATH="$(dirname "$(command -v go 2>/dev/null || echo /usr/bin/go)"):/usr/local/bin:/usr/bin:/bin"

write_units() {
    # write_units <unit> <command> <description> <schedule>
    cat > "$UNIT_DIR/$1.service" <<EOF
[Unit]
Description=$3 (${CHAIN_ID})

[Service]
Type=oneshot
Environment=PATH=${UNIT_PATH}
ExecStart=${VAULT_DIR}/bin/archive-mirror $2 -c ${ENV_FILE}
Nice=10
EOF
    cat > "$UNIT_DIR/$1.timer" <<EOF
[Unit]
Description=$3 (${CHAIN_ID})

[Timer]
OnCalendar=$4
Persistent=true
RandomizedDelaySec=300

[Install]
WantedBy=timers.target
EOF
}

write_units "$SYNC_UNIT" sync "Block archive sync" "$SYNC_SCHEDULE"
write_units "$REBUILD_UNIT" rebuild "Block archive rebuild check" "$REBUILD_SCHEDULE"

systemctl --user daemon-reload
systemctl --user enable --now "${SYNC_UNIT}.timer" "${REBUILD_UNIT}.timer"

# Without lingering the user manager, and the timers with it, stop when the
# user logs out.
if [ "$(loginctl show-user "$(id -un)" -p Linger --value 2>/dev/null)" != "yes" ]; then
    echo ""
    echo "WARNING: lingering is off, so the timers only run while $(id -un) is logged in."
    echo "         Enable it with: sudo loginctl enable-linger $(id -un)"
fi

echo ""
echo "Archive mirror installed for ${CHAIN_ID}:"
echo "  Vault:    ${VAULT_DIR}"
echo "  Sync:     ${SYNC_SCHEDULE}"
echo "  Rebuild:  ${REBUILD_SCHEDULE}"
echo "  Config:   ${ENV_FILE}"
echo ""
systemctl --user list-timers --all --no-pager "${SYNC_UNIT}.timer" "${REBUILD_UNIT}.timer" || true
echo ""
echo "Run one now:  systemctl --user start ${SYNC_UNIT}.service"
echo "Follow it:    journalctl --user -u ${SYNC_UNIT}.service -f"
