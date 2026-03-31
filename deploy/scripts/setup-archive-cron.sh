#!/bin/bash
#
# setup-archive-cron.sh
#
# Installs a cron job on the sentry node to automatically archive new
# blocks every 6 hours. Run once via SSH after deploying the sentry.
#
# Requires dcron (Dillon's cron), which is installed in the Docker
# image via `apk add dcron`. The script verifies dcron is available
# before proceeding.
#
# The archiver output is stored on the persistent volume alongside
# the chain data, so it survives container restarts.
#
# Usage:
#   setup-archive-cron                  # Enable (default: every 6 hours)
#   setup-archive-cron --disable        # Disable and remove the cron job
#   setup-archive-cron --status         # Show current cron job status
#
# Remote usage:
#   ssh -p <port> root@<sentry_provider> setup-archive-cron
#   ssh -p <port> root@<sentry_provider> setup-archive-cron --disable
#
# Environment variables (all optional, ignored with --disable/--status):
#   ARCHIVE_INTERVAL  - Cron schedule (default: "0 */6 * * *" = every 6 hours)
#   RPC_URL           - Node RPC endpoint (default: http://localhost:26657)
#   OUTPUT_DIR        - Archive output directory (default: /root/.sparkdream/archives)
#
set -e

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
ACTION="enable"
case "${1:-}" in
    --disable) ACTION="disable" ;;
    --status)  ACTION="status" ;;
    --help|-h)
        echo "Usage: setup-archive-cron [--disable|--status]"
        echo "  (no args)   Install/update the block archiver cron job"
        echo "  --disable   Remove the cron job and stop dcron if idle"
        echo "  --status    Show whether the cron job is active"
        exit 0
        ;;
    "") ;;
    *)
        echo "Unknown option: $1" >&2
        echo "Usage: setup-archive-cron [--disable|--status]" >&2
        exit 1
        ;;
esac

# ---------------------------------------------------------------------------
# Verify dcron is installed
# ---------------------------------------------------------------------------
if ! command -v crond >/dev/null 2>&1; then
    echo "ERROR: 'crond' not found. Install dcron: apk add dcron" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# --status: show current state and exit
# ---------------------------------------------------------------------------
if [ "$ACTION" = "status" ]; then
    if crontab -l 2>/dev/null | grep -q 'block-archiver'; then
        echo "Block archiver cron job is ACTIVE:"
        crontab -l 2>/dev/null | grep 'block-archiver'
    else
        echo "Block archiver cron job is NOT installed."
    fi
    if pgrep -x crond >/dev/null 2>&1; then
        echo "crond (dcron) is running (PID $(pgrep -x crond))."
    else
        echo "crond (dcron) is not running."
    fi
    exit 0
fi

# ---------------------------------------------------------------------------
# --disable: remove cron job and exit
# ---------------------------------------------------------------------------
if [ "$ACTION" = "disable" ]; then
    if crontab -l 2>/dev/null | grep -q 'block-archiver'; then
        crontab -l 2>/dev/null | grep -v 'block-archiver' | crontab -
        echo "Block archiver cron job removed."
    else
        echo "Block archiver cron job was not installed."
    fi
    # Stop dcron if no other cron jobs remain
    REMAINING=$(crontab -l 2>/dev/null | grep -cv '^$' || true)
    if [ "$REMAINING" -eq 0 ] && pgrep -x crond >/dev/null 2>&1; then
        pkill -x crond
        echo "Stopped dcron (no remaining cron jobs)."
    fi
    exit 0
fi

# ---------------------------------------------------------------------------
# Enable: install cron job
# ---------------------------------------------------------------------------
ARCHIVE_INTERVAL="${ARCHIVE_INTERVAL:-0 */6 * * *}"
RPC_URL="${RPC_URL:-http://localhost:26657}"
OUTPUT_DIR="${OUTPUT_DIR:-/root/.sparkdream/archives}"
LOG_FILE="/var/log/block-archiver.log"

# Verify the archiver is available
if ! command -v block-archiver >/dev/null 2>&1; then
    echo "ERROR: 'block-archiver' not found on PATH." >&2
    echo "This script expects the sparkdreamd Docker image which includes it." >&2
    exit 1
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Build the cron entry
CRON_CMD="RPC_URL=${RPC_URL} OUTPUT_DIR=${OUTPUT_DIR} block-archiver >> ${LOG_FILE} 2>&1"
CRON_LINE="${ARCHIVE_INTERVAL} ${CRON_CMD}"

# Install the cron job (replace any existing block-archiver entry)
( crontab -l 2>/dev/null | grep -v 'block-archiver' ; echo "$CRON_LINE" ) | crontab -

# Start dcron if not already running
if ! pgrep -x crond >/dev/null 2>&1; then
    crond -S -l info
    echo "Started dcron."
fi

echo "Block archiver cron job installed:"
echo "  Schedule:  ${ARCHIVE_INTERVAL}"
echo "  RPC:       ${RPC_URL}"
echo "  Output:    ${OUTPUT_DIR}"
echo "  Log:       ${LOG_FILE}"
echo ""
echo "Current crontab:"
crontab -l
echo ""
echo "To run immediately:  RPC_URL=${RPC_URL} OUTPUT_DIR=${OUTPUT_DIR} block-archiver"
echo "To view logs:        tail -f ${LOG_FILE}"
echo "To disable:          setup-archive-cron --disable"
