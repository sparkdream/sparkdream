#!/bin/bash
# Seed the S3 replica from an existing local Headscale db.sqlite.
# Runs `litestream replicate` for ~30s to write the initial encrypted snapshot,
# then stops. After this, deploy headscale.sdl.yaml on Akash and the new
# container will restore from the seeded snapshot when it sees an empty PVC.
#
# Usage:
#   # populate env vars (same values as in headscale.sdl.yaml)
#   export LITESTREAM_S3_ENDPOINT=...
#   export LITESTREAM_S3_BUCKET=...
#   export LITESTREAM_S3_PATH=archive
#   export LITESTREAM_S3_REGION=...
#   export LITESTREAM_S3_ACCESS_KEY_ID=...
#   export LITESTREAM_S3_SECRET_ACCESS_KEY=...
#   export AGE_RECIPIENT=age1...
#   export AGE_IDENTITY=AGE-SECRET-KEY-1...
#
#   ./deploy/mesh/seed-replica.sh /path/to/recovered/db.sqlite
#
# Requires: litestream (>=0.3.13), sqlite3.
#   apt:  apt install sqlite3 && curl ... # see Dockerfile-headscale-alpine for litestream URL
#   brew: brew install litestream sqlite

set -euo pipefail

DB_PATH="${1:-}"
if [ -z "$DB_PATH" ] || [ ! -f "$DB_PATH" ]; then
  echo "usage: $0 /path/to/db.sqlite" >&2
  exit 1
fi
DB_ABS=$(realpath "$DB_PATH")

for var in LITESTREAM_S3_ENDPOINT LITESTREAM_S3_BUCKET LITESTREAM_S3_PATH \
           LITESTREAM_S3_REGION LITESTREAM_S3_ACCESS_KEY_ID \
           LITESTREAM_S3_SECRET_ACCESS_KEY AGE_RECIPIENT AGE_IDENTITY; do
  if [ -z "${!var:-}" ]; then
    echo "error: \$$var is not set" >&2
    exit 1
  fi
done

command -v litestream >/dev/null || { echo "error: litestream not in PATH" >&2; exit 1; }
command -v sqlite3   >/dev/null  || { echo "error: sqlite3 not in PATH"   >&2; exit 1; }

echo "==> Checkpointing any WAL frames into main DB..."
sqlite3 "$DB_ABS" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null

echo "==> Verifying DB integrity..."
result=$(sqlite3 "$DB_ABS" "PRAGMA integrity_check;")
if [ "$result" != "ok" ]; then
  echo "error: integrity_check failed: $result" >&2
  exit 1
fi
echo "    integrity: ok ($(stat -c %s "$DB_ABS" 2>/dev/null || stat -f %z "$DB_ABS") bytes)"

# Build a temp config that points at the local DB but mirrors the deployed one.
SCRIPT_DIR=$(dirname "$(realpath "$0")")
CONFIG=$(mktemp /tmp/litestream-seed-XXXXXX.yml)
trap 'rm -f "$CONFIG"' EXIT
sed "s|/var/lib/headscale/db.sqlite|$DB_ABS|" "$SCRIPT_DIR/litestream.yml" > "$CONFIG"

echo "==> Running litestream replicate for 30s to upload initial encrypted snapshot..."
timeout --preserve-status 30s litestream replicate -config "$CONFIG" || true

echo
echo "==> Snapshots now in bucket:"
litestream snapshots -config "$CONFIG" "$DB_ABS"

echo
echo "==> Generations:"
litestream generations -config "$CONFIG" "$DB_ABS"

echo
echo "==> Seed complete. If at least one snapshot is listed above, your bucket"
echo "    is ready. Deploy headscale.sdl.yaml on Akash; the container's"
echo "    entrypoint will detect an empty PVC and restore from this snapshot."
