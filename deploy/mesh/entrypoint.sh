#!/bin/sh
set -e

CONFIG=/etc/headscale/config.yaml
DEFAULT_CONFIG=/opt/headscale/default-config.yaml

if [ ! -f "$CONFIG" ]; then
  echo "==> No config found, copying default config..."
  cp "$DEFAULT_CONFIG" "$CONFIG"
  echo "==> Config written to $CONFIG"
  echo "==> IMPORTANT: Update server_url in $CONFIG with your Akash provider URI"
else
  echo "==> Existing config found, using it."
fi

mkdir -p /var/run/headscale

exec headscale serve
