#!/bin/bash
# ------------------------------------------------------------------
# Container CMD for the Hermes relayer image.
#
# Waits until the launcher has uploaded a config and finished
# relayer-bringup (which touches $RELAYER_DIR/ready), then execs
# `hermes start`. Because the marker lives on the persistent /data
# volume, a container restart goes straight back to relaying without
# anyone SSHing in.
#
# Until then the container stays up with sshd + tailscale running (the
# entrypoint started them), so the launcher can reach it to upload.
# ------------------------------------------------------------------
set -euo pipefail

RELAYER_DIR="${RELAYER_DIR:-/data/relayer}"
CONFIG="$RELAYER_DIR/config.toml"

waited=0
until [ -f "$RELAYER_DIR/ready" ] && [ -f "$CONFIG" ]; do
    if [ $((waited % 300)) -eq 0 ]; then
        echo "relayer-run: waiting for $RELAYER_DIR/ready (run relayer-bringup first)"
    fi
    sleep 5
    waited=$((waited + 5))
done

echo "relayer-run: starting hermes with $CONFIG"
exec hermes --config "$CONFIG" start
