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

    # 5d. Watchdog: supervise the two helpers the node cannot sign without.
    #
    # sparkdreamd is PID 1's exec target and gets container restarts, but
    # tailscaled and the privval socat are background jobs with no supervisor.
    # When one of them wedges — tailscaled loses the control session or its
    # DERP path and stops delivering inbound connections, or the socat parent
    # dies — the privval listener sees zero dials and the signer can never
    # reconnect: observed live as minutes of "SignerListener: Error accepting
    # connection: i/o timeout" against a perfectly healthy listener, fixed
    # only by a container restart.
    #
    # This loop is the supervisor. It restarts a dead socat, restarts an
    # unreachable tailscaled, and re-runs `tailscale up` when tailscaled's
    # OWN health signals are broken (control session not Running, no DERP
    # relay reachable). It never keys on "no signer session": a healthy mesh
    # with no signer connected is a valid state (await-signer, signer stopped
    # on purpose), and the signer machine cannot be probed anyway (its
    # firewall drops inbound). Sentry p2p tunnels (TS_TUNNEL_*) are not
    # supervised here on purpose: their targets are rewired over SSH after
    # boot, so restarting env-defined ones could resurrect stale targets.
    #
    # Remote-signer validators (PRIVVAL_KEEPALIVE_PORT set) also get a stall
    # rule. When a mesh blip outlasts CometBFT's sign retries, the node's own
    # proposal/vote fails and is never retried, and prevote/precommit
    # timeouts only start once +2/3 of the power has voted. A validator whose
    # missing vote blocks that quorum (always, on a single-validator chain)
    # then waits forever, even after tmkms reconnects: observed live on
    # devnet 2026-09-28 as a 30h halt at a frozen height with the signer
    # session up and tmkms receiving no sign requests. A restart starts a
    # fresh round that asks for signatures again, and tmkms's double-sign
    # state keeps that safe. So: height frozen for NODE_STALL_SECS (default
    # 180, 0 disables) with the signer session established, and the node not
    # deliberately halted (app.toml halt-height) → restart sparkdreamd, at
    # most once per NODE_RESTART_COOLDOWN (default 600).
    (
        TS_SOCKET="${TS_STATE_DIR}/tailscaled.sock"
        REUP_COOLDOWN=300    # seconds between tailscale re-up attempts
        NETCHECK_EVERY=10    # DERP probe every Nth cycle (it is slow and chatty)
        NODE_STALL_SECS="${NODE_STALL_SECS:-180}"
        NODE_RESTART_COOLDOWN="${NODE_RESTART_COOLDOWN:-600}"
        NODE_RPC="${NODE_RPC:-http://127.0.0.1:26657}"
        NODE_HOME_DIR=/root/.sparkdream
        cycle=0
        netcheck_fails=0
        last_reup=0
        stall_height=""
        stall_since=0
        last_node_restart=0
        tailscale_up() {
            tailscale --socket="$TS_SOCKET" up \
                --login-server="$HEADSCALE_URL" \
                --authkey="$TS_AUTHKEY" \
                --hostname="$TS_HOSTNAME" \
                --accept-dns=false
        }
        # established privval sessions on the backend port (tmkms holds one
        # through the keepalive proxy); same probe the launcher uses
        signer_connected() {
            n=$({ netstat -tn 2>/dev/null || ss -tn 2>/dev/null; } \
                | grep -cE ":${PRIVVAL_BACKEND_PORT}.*ESTABLISHED|ESTAB.*:${PRIVVAL_BACKEND_PORT}" || true)
            [ "${n:-0}" -ge 1 ]
        }
        # restart a running `sparkdreamd start`, keeping its own command line
        # and log target. Two shapes exist: sparkdreamd as PID 1 (entrypoint
        # exec, so stopping it restarts the container and the restart starts
        # the node), or a detached child the launcher started over SSH, which
        # has to be started again here.
        restart_node() {
            pid=$(pgrep -xo sparkdreamd) || return 1
            if [ "$pid" = 1 ]; then
                kill -TERM 1
                return 0
            fi
            cmd=$(tr '\0' ' ' < "/proc/$pid/cmdline")
            out=$(readlink "/proc/$pid/fd/1" 2>/dev/null || true)
            case "$out" in /*) ;; *) out="${NODE_HOME_DIR}/sparkdreamd.log" ;; esac
            kill -TERM "$pid" || true
            for i in $(seq 1 30); do
                kill -0 "$pid" 2>/dev/null || break
                sleep 1
            done
            kill -0 "$pid" 2>/dev/null && kill -KILL "$pid"
            sleep 1
            # cmdline is re-split on purpose (node args carry no spaces)
            nohup $cmd >>"$out" 2>&1 &
        }
        while true; do
            sleep 30
            cycle=$((cycle + 1))

            # tailscaled unreachable: the socket answering `status` is the
            # only liveness signal that survives PID games (a zombie still
            # passes kill -0, and PID 1 is sparkdreamd, not an init)
            if ! tailscale --socket="$TS_SOCKET" status >/dev/null 2>&1; then
                echo "watchdog: tailscaled not answering, restarting it"
                rm -f "$TS_SOCKET"
                tailscaled \
                    --tun=userspace-networking \
                    --state="${TS_STATE_DIR}/tailscaled.state" \
                    --socket="${TS_SOCKET}" \
                    >>/var/log/tailscaled.log 2>&1 &
                sleep 5
                tailscale_up && echo "watchdog: tailscale rejoined the mesh" \
                    || echo "watchdog: tailscale up failed after daemon restart"
                last_reup=$(date +%s)
                continue
            fi

            now=$(date +%s)

            # control session broken (e.g. headscale rotated its noise key
            # under us): the fix is the one applied by hand, re-run tailscale up
            state=$(tailscale --socket="$TS_SOCKET" status --json 2>/dev/null \
                | sed -n 's/.*"BackendState"[^A-Za-z]*\([A-Za-z]*\).*/\1/p' | head -1)
            if [ "$state" != "Running" ] && [ $((now - last_reup)) -ge "$REUP_COOLDOWN" ]; then
                echo "watchdog: tailscale session state '${state:-unknown}', re-running tailscale up"
                tailscale_up && echo "watchdog: tailscale rejoined the mesh" \
                    || echo "watchdog: tailscale up failed, retrying in ${REUP_COOLDOWN}s"
                last_reup=$now
                continue
            fi

            # data plane: every Nth cycle confirm a DERP relay answers (every
            # mesh flow here relays, so no relay = no signer path). Two
            # consecutive failures trigger the same re-up.
            if [ $((cycle % NETCHECK_EVERY)) -eq 0 ]; then
                if tailscale --socket="$TS_SOCKET" netcheck 2>/dev/null \
                    | grep -Eq '^[[:space:]]*-[[:space:]]*[a-z0-9-]+:[[:space:]]*[0-9.]+ms'; then
                    netcheck_fails=0
                else
                    netcheck_fails=$((netcheck_fails + 1))
                    echo "watchdog: no DERP relay answered (failure $netcheck_fails)"
                    if [ "$netcheck_fails" -ge 2 ] && [ $((now - last_reup)) -ge "$REUP_COOLDOWN" ]; then
                        echo "watchdog: relay unreachable across $netcheck_fails probes, re-running tailscale up"
                        tailscale_up && echo "watchdog: tailscale rejoined the mesh" \
                            || echo "watchdog: tailscale up failed, retrying in ${REUP_COOLDOWN}s"
                        last_reup=$now
                        netcheck_fails=0
                    fi
                fi
            fi

            # privval socat parent dead: nothing logs that anywhere visible;
            # the only symptom is zero inbound dials on the privval listener
            if [ -n "$PRIVVAL_KEEPALIVE_PORT" ] \
                && ! pgrep -f "^socat (-d )?TCP-LISTEN:${PRIVVAL_KEEPALIVE_PORT}," >/dev/null 2>&1; then
                echo "watchdog: privval proxy (port ${PRIVVAL_KEEPALIVE_PORT}) not running, restarting it"
                socat -d \
                    TCP-LISTEN:${PRIVVAL_KEEPALIVE_PORT},fork,reuseaddr,${SOCAT_OPTS} \
                    TCP:127.0.0.1:${PRIVVAL_BACKEND_PORT},${SOCAT_OPTS} \
                    >>/var/log/socat-privval.log 2>&1 &
            fi

            # consensus stalled on a failed own vote (see the stall rule above)
            if [ -n "$PRIVVAL_KEEPALIVE_PORT" ] && [ "$NODE_STALL_SECS" -gt 0 ] 2>/dev/null; then
                pid=$(pgrep -xo sparkdreamd || true)
                # only a running `start` serving RPC counts; anything else
                # (WAIT_FOR_CONFIG, replay/export tools, a node the launcher
                # stopped on purpose) resets the baseline and is left alone
                h=""
                if [ -n "$pid" ] && tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -q ' start'; then
                    home=$(tr '\0' '\n' < "/proc/$pid/cmdline" 2>/dev/null | sed -n '/^--home$/{n;p;}' | head -1)
                    NODE_HOME_DIR="${home:-/root/.sparkdream}"
                    h=$(curl -s -m 5 "${NODE_RPC}/status" 2>/dev/null \
                        | jq -r 'select(.result.sync_info.catching_up == false)
                                 | .result.sync_info.latest_block_height // empty' 2>/dev/null || true)
                fi
                if [ -z "$h" ]; then
                    stall_height=""
                elif [ "$h" != "$stall_height" ]; then
                    stall_height=$h
                    stall_since=$now
                elif [ $((now - stall_since)) -ge "$NODE_STALL_SECS" ] \
                    && [ $((now - last_node_restart)) -ge "$NODE_RESTART_COOLDOWN" ] \
                    && ! grep -Eq '^halt-height *= *"?[1-9]' "${NODE_HOME_DIR}/config/app.toml" 2>/dev/null \
                    && signer_connected; then
                    echo "watchdog: height stuck at ${h} for $((now - stall_since))s with the signer connected, restarting sparkdreamd"
                    restart_node && echo "watchdog: sparkdreamd restarted" \
                        || echo "watchdog: sparkdreamd restart failed"
                    last_node_restart=$now
                    stall_since=$now
                fi
            fi
        done
    ) &
    echo "watchdog: supervising tailscaled and the privval proxy (30s cycle)"
    if [ -n "$PRIVVAL_KEEPALIVE_PORT" ] && [ "${NODE_STALL_SECS:-180}" -gt 0 ] 2>/dev/null; then
        echo "watchdog: restarts sparkdreamd after ${NODE_STALL_SECS:-180}s of frozen height with the signer connected"
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

# 6b. Launcher hold: a maintenance task that needs the node stopped (the
#     launcher's chain-data backup copies the data directory, which must not
#     change under it) writes a unix-time deadline to this file on the data
#     volume and restarts the container. Until the file is removed or the
#     deadline passes, sshd and the mesh run but the node does not start;
#     then the node starts here as usual. The deadline is what brings the
#     node back if the launcher never returns to remove the file.
HOLD_FILE=/root/.sparkdream/.launcher-hold
hold_active() {
    [ -f "$HOLD_FILE" ] || return 1
    deadline=$(cat "$HOLD_FILE" 2>/dev/null)
    [ "$deadline" -gt "$(date +%s)" ] 2>/dev/null
}
if [ -f "$HOLD_FILE" ]; then
    echo "launcher hold: node not started until $(cat "$HOLD_FILE" 2>/dev/null) (unix time) or until the hold is removed"
    while hold_active; do sleep 15; done
    rm -f "$HOLD_FILE"
    echo "launcher hold released: starting the node"
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

# 8. Normal mode: exec the image CMD (sparkdreamd for node images,
#    relayer-run for the Hermes image)
echo "Starting: $@"
exec "$@"
