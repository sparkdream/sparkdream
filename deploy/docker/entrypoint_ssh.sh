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
    TS_SOCKET="${TS_STATE_DIR}/tailscaled.sock"

    # Remove stale socket so tailscaled can bind cleanly, but preserve
    # tailscaled.state — that file holds the node identity and stable IP.
    rm -f "$TS_SOCKET"

    tailscaled \
        --tun=userspace-networking \
        --state="${TS_STATE_DIR}/tailscaled.state" \
        --socket="${TS_SOCKET}" \
        &>/var/log/tailscaled.log &
    TAILSCALED_PID=$!

    # Wait for daemon to be ready by testing the socket is alive, not just present.
    # This avoids the stale-socket race on persistent storage.
    echo "Waiting for tailscaled..."
    for i in $(seq 1 30); do
        tailscale --socket="$TS_SOCKET" status &>/dev/null && break
        # Also check tailscaled hasn't exited
        kill -0 $TAILSCALED_PID 2>/dev/null || { echo "ERROR: tailscaled exited. Check /var/log/tailscaled.log"; break; }
        sleep 1
    done

    # Join the Headscale network
    TS_HOSTNAME="${TS_HOSTNAME:-sparkdream-node}"
    tailscale --socket="$TS_SOCKET" up \
        --login-server="$HEADSCALE_URL" \
        --authkey="$TS_AUTHKEY" \
        --hostname="$TS_HOSTNAME" \
        --accept-dns=false \
        && echo "Tailscale connected as ${TS_HOSTNAME}" \
        || echo "WARNING: Tailscale failed to connect"

    # Show Tailscale IP for reference
    TS_IP=$(tailscale --socket="$TS_SOCKET" ip -4 2>/dev/null || echo "unknown")
    echo "Tailscale IP: ${TS_IP}"

    # 5b. Set up socat TCP tunnels for Tailscale userspace networking.
    # Akash containers lack NET_ADMIN, so tailscaled runs in userspace mode where
    # the Tailscale IP is not a real kernel interface. Other Tailscale nodes cannot
    # connect to local TCP ports via the Tailscale IP directly. socat bridges this
    # by forwarding a local port through "tailscale nc" which uses the userspace stack.
    #
    # TS_TUNNEL_* env vars define tunnels as "local_port:remote_tailscale_ip:remote_port"
    # Example: TS_TUNNEL_1="16656:100.64.0.10:26656" forwards localhost:16656 to
    #          the validator's 26656 via Tailscale.
    for var in $(env | grep '^TS_TUNNEL_' | sort); do
        TUNNEL_SPEC="${var#*=}"
        LOCAL_PORT=$(echo "$TUNNEL_SPEC" | cut -d: -f1)
        REMOTE_IP=$(echo "$TUNNEL_SPEC" | cut -d: -f2)
        REMOTE_PORT=$(echo "$TUNNEL_SPEC" | cut -d: -f3)
        if [ -n "$LOCAL_PORT" ] && [ -n "$REMOTE_IP" ] && [ -n "$REMOTE_PORT" ]; then
            echo "Tailscale tunnel: localhost:${LOCAL_PORT} -> ${REMOTE_IP}:${REMOTE_PORT}"
            socat TCP-LISTEN:${LOCAL_PORT},fork,reuseaddr \
                EXEC:"tailscale --socket=${TS_SOCKET} nc ${REMOTE_IP} ${REMOTE_PORT}" &
        fi
    done
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
        tailscale --socket="${TS_STATE_DIR}/tailscaled.sock" status 2>/dev/null || echo "  (not connected)"
        echo ""
    fi
    echo "Once ready, either:"
    echo "  1. Run: sparkdreamd start --home /root/.sparkdream"
    echo "  2. Or redeploy with WAIT_FOR_CONFIG removed"
    echo "============================================"
    # Sleep forever — keeps the container (and sshd/tailscale) running
    exec tail -f /dev/null
fi

# 7. Optional startup delay to allow Tailscale mesh and TMKMS connections to
#    establish before the node begins signing. Without this, the node can panic
#    on the first block if the external signer isn't reachable yet, causing
#    Akash to restart the container in an endless crash loop.
#    Set STARTUP_DELAY to the number of seconds to wait (default: 0 = no delay).
STARTUP_DELAY="${STARTUP_DELAY:-0}"
if [ "$STARTUP_DELAY" -gt 0 ] 2>/dev/null; then
    echo "Waiting ${STARTUP_DELAY}s for network/signer readiness..."
    sleep "$STARTUP_DELAY"
    echo "Startup delay complete."
fi

# 8. Normal mode: start the Spark Dream blockchain node
echo "Starting sparkdreamd with args: $@"
exec "$@"
