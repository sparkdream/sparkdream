#!/bin/sh
set -e

CONFIG=/etc/headscale/config.yaml
DEFAULT_CONFIG=/opt/headscale/default-config.yaml
DB=/var/lib/headscale/db.sqlite

if [ ! -f "$CONFIG" ]; then
  echo "==> No config found, copying default config..."
  cp "$DEFAULT_CONFIG" "$CONFIG"
  echo "==> Config written to $CONFIG"
  echo "==> IMPORTANT: Update server_url in $CONFIG with your Akash provider URI"
else
  echo "==> Existing config found, using it."
fi

mkdir -p /var/run/headscale /var/lib/headscale

# Litestream wraps headscale: every WAL frame is shipped to S3-compatible
# object storage. If the persistent volume is empty (new provider after
# disaster), the DB is restored from the replica before headscale starts.
# If LITESTREAM_S3_BUCKET is unset, we fall through and run headscale alone
# so the same image still works for local/dev deployments without backup.
if [ -n "$LITESTREAM_S3_BUCKET" ] && [ -n "$LITESTREAM_S3_ACCESS_KEY_ID" ]; then
  echo "==> Litestream enabled (bucket=$LITESTREAM_S3_BUCKET endpoint=$LITESTREAM_S3_ENDPOINT)"
  if [ ! -f "$DB" ]; then
    echo "==> No local DB found, attempting restore from replica..."
    if litestream restore -if-replica-exists -config /etc/litestream.yml "$DB"; then
      echo "==> Restore complete: $(stat -c %s "$DB" 2>/dev/null || echo 0) bytes"
    else
      echo "==> No replica found (or restore failed), starting fresh."
    fi
  else
    echo "==> Local DB already exists ($(stat -c %s "$DB") bytes), skipping restore."
  fi
  exec litestream replicate -exec "headscale serve" -config /etc/litestream.yml
else
  echo "==> Litestream disabled (LITESTREAM_S3_BUCKET unset). Running headscale standalone."
  exec headscale serve
fi
