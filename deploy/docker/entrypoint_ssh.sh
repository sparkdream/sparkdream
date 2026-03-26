#!/bin/sh
set -e

# 1. Unlock root account (Alpine locks it by default; sshd rejects locked accounts)
sed -i 's/^root:!:/root:*:/' /etc/shadow

# 2. Ensure host keys exist (regenerate if missing at runtime)
ssh-keygen -A 2>/dev/null

# 3. Inject the SSH public key from the environment variable
if [ -n "$SSH_PUBLIC_KEY" ]; then
    mkdir -p /root/.ssh
    echo "$SSH_PUBLIC_KEY" > /root/.ssh/authorized_keys
    chmod 700 /root/.ssh
    chmod 600 /root/.ssh/authorized_keys
    echo "SSH public key injected."
else
    echo "WARNING: SSH_PUBLIC_KEY not set. SSH will not be available."
fi

# 4. Start the SSH server in the background
echo "Starting sshd..."
/usr/sbin/sshd -e -p 2222 || echo "ERROR: sshd failed to start"

# 5. Start Tailscale daemon if HEADSCALE_URL and TS_AUTHKEY are set
if [ -n "$HEADSCALE_URL" ] && [ -n "$TS_AUTHKEY" ]; then
    echo "Starting Tailscale daemon (userspace networking)..."

    # Use persistent storage for Tailscale state if available
    TS_STATE_DIR="${TS_STATE_DIR:-/var/lib/tailscale}"
    mkdir -p "$TS_STATE_DIR"

    # Start tailscaled in userspace networking mode (no TUN device needed)
    tailscaled \
        --tun=userspace-networking \
        --state="${TS_STATE_DIR}/tailscaled.state" \
        --socket="${TS_STATE_DIR}/tailscaled.sock" \
        &>/var/log/tailscaled.log &

    # Wait for daemon to be ready
    sleep 3

    # Join the Headscale network
    TS_HOSTNAME="${TS_HOSTNAME:-sparkdream-node}"
    tailscale up \
        --login-server="$HEADSCALE_URL" \
        --authkey="$TS_AUTHKEY" \
        --hostname="$TS_HOSTNAME" \
        --accept-dns=false \
        && echo "Tailscale connected as ${TS_HOSTNAME}" \
        || echo "WARNING: Tailscale failed to connect"

    # Show Tailscale IP for reference
    TS_IP=$(tailscale ip -4 2>/dev/null || echo "unknown")
    echo "Tailscale IP: ${TS_IP}"
elif [ -n "$HEADSCALE_URL" ] || [ -n "$TS_AUTHKEY" ]; then
    echo "WARNING: Both HEADSCALE_URL and TS_AUTHKEY must be set for Tailscale. Skipping."
else
    echo "Tailscale not configured (HEADSCALE_URL and TS_AUTHKEY not set)."
fi

# 6. If WAIT_FOR_CONFIG is set, keep the container alive without starting the node.
#    This lets you SSH in, upload config/data, then manually start the node or
#    redeploy with WAIT_FOR_CONFIG removed.
if [ "${WAIT_FOR_CONFIG}" = "true" ]; then
    echo "============================================"
    echo "WAIT_FOR_CONFIG=true"
    echo "Container is alive. SSH in to upload chain"
    echo "config and data to /root/.sparkdream/"
    echo ""
    if [ -n "$HEADSCALE_URL" ]; then
        echo "Tailscale status:"
        tailscale status 2>/dev/null || echo "  (not connected)"
        echo ""
    fi
    echo "Once ready, either:"
    echo "  1. Run: sparkdreamd start --home /root/.sparkdream"
    echo "  2. Or redeploy with WAIT_FOR_CONFIG removed"
    echo "============================================"
    # Sleep forever — keeps the container (and sshd/tailscale) running
    exec tail -f /dev/null
fi

# 7. Normal mode: start the Spark Dream blockchain node
echo "Starting sparkdreamd with args: $@"
exec "$@"
