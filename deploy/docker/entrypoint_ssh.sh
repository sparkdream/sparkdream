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

    # 5c. Privval keepalive proxy.
    #
    # When a remote signer (tmkms) connects to the validator's privval port over a
    # tailscale-userspace tunnel + DERP, the TCP connection is fragile: anything in
    # the path that idles-out TCP connections (the tailscale userspace forwarder,
    # the DERP relay, NAT) will tear it down between sign requests, which arrive
    # seconds apart. CometBFT's built-in heartbeat (~1s ping, ~3s timeout) is too
    # slow to keep the connection warm and too coarse to recover gracefully — every
    # reconnect costs a fresh handshake (5-30s) and any signs requested during that
    # window time out, causing CometBFT to vote nil and miss the round.
    #
    # The fix is to insert socat as a keepalive-enforcing proxy: sparkdreamd binds
    # privval on a private port (PRIVVAL_BACKEND_PORT, default 26660 on 127.0.0.1)
    # while socat owns the public-facing port (PRIVVAL_KEEPALIVE_PORT, default
    # 26659). socat sets SO_KEEPALIVE + tight TCP_KEEPIDLE/INTVL/CNT on both legs,
    # so the kernel sends real TCP keepalive packets through the tunnel before any
    # idle-timer can fire.
    #
    # Gated on Tailscale being configured because the privval-drop problem only
    # exists when traffic transits the tailscale-userspace+DERP path. With
    # kernel-mode tailscale or no tunnel at all, the kernel TCP stack already
    # honors SO_KEEPALIVE end-to-end and this proxy is dead weight.
    #
    # To enable: set PRIVVAL_KEEPALIVE_PORT in the SDL env, and set
    #   priv_validator_laddr = "tcp://127.0.0.1:26660"
    # in $HOME/.sparkdream/config/config.toml so sparkdreamd binds to the backend
    # port instead of the public one. Pi-side tmkms keeps dialing the validator's
    # tailnet IP on PRIVVAL_KEEPALIVE_PORT — nothing changes there.
    #
    # Knobs (all optional):
    #   PRIVVAL_KEEPALIVE_PORT   public-facing port (default 26659)
    #   PRIVVAL_BACKEND_PORT     localhost port sparkdreamd binds (default 26660)
    #   PRIVVAL_KEEPIDLE         seconds idle before first keepalive (default 10)
    #   PRIVVAL_KEEPINTVL        seconds between keepalive retries (default 5)
    #   PRIVVAL_KEEPCNT          retries before dropping connection (default 3)
    if [ -n "$PRIVVAL_KEEPALIVE_PORT" ]; then
        PRIVVAL_BACKEND_PORT="${PRIVVAL_BACKEND_PORT:-26660}"
        PRIVVAL_KEEPIDLE="${PRIVVAL_KEEPIDLE:-10}"
        PRIVVAL_KEEPINTVL="${PRIVVAL_KEEPINTVL:-5}"
        PRIVVAL_KEEPCNT="${PRIVVAL_KEEPCNT:-3}"
        SOCAT_OPTS="keepalive,keepidle=${PRIVVAL_KEEPIDLE},keepintvl=${PRIVVAL_KEEPINTVL},keepcnt=${PRIVVAL_KEEPCNT}"

        echo "Starting privval keepalive proxy: 0.0.0.0:${PRIVVAL_KEEPALIVE_PORT} -> 127.0.0.1:${PRIVVAL_BACKEND_PORT}"
        echo "  (keepidle=${PRIVVAL_KEEPIDLE}s intvl=${PRIVVAL_KEEPINTVL}s cnt=${PRIVVAL_KEEPCNT}; dead-connection detection ~$((PRIVVAL_KEEPIDLE + PRIVVAL_KEEPINTVL * PRIVVAL_KEEPCNT))s)"
        echo "  REMINDER: priv_validator_laddr in config.toml must be tcp://127.0.0.1:${PRIVVAL_BACKEND_PORT}"

        socat -d \
            TCP-LISTEN:${PRIVVAL_KEEPALIVE_PORT},fork,reuseaddr,${SOCAT_OPTS} \
            TCP:127.0.0.1:${PRIVVAL_BACKEND_PORT},${SOCAT_OPTS} \
            &>/var/log/socat-privval.log &
        SOCAT_PID=$!
        echo "  socat pid=${SOCAT_PID} (logs at /var/log/socat-privval.log)"
    fi
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
