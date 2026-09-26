#!/bin/sh
# ------------------------------------------------------------------
# sdap image entrypoint: ready /data, then run the command as "sdap".
#
# Akash mounts a fresh persistent volume owned by root, so the
# unprivileged daemon could write neither its state file nor read a
# session key there. Starting as root, this hands /data to sdap and
# drops privileges before exec'ing the daemon. Run as a non-root user
# (docker run --user ...), it just execs the command.
#
# Keep the image's user root (no USER line): a provider's lease-shell
# runs as the image user, and the launcher writes the session key
# (SDA_SESSION_KEY_FILE) into /data through it, chowning it to sdap.
# ------------------------------------------------------------------
set -eu

if [ "$(id -u)" = "0" ]; then
    mkdir -p /data
    chown sdap /data
    # files an earlier run (or the launcher) left behind
    find /data -maxdepth 1 ! -user sdap -exec chown sdap {} + 2>/dev/null || true
    exec su-exec sdap "$@"
fi
exec "$@"
