#!/bin/bash
# ------------------------------------------------------------------
# Container CMD for the launcher's Mastodon image: web (puma) and sidekiq
# in one container, because on Akash a persistent volume belongs to one
# service and both processes write uploaded media to public/system.
#
#   1. hand the media volume to the mastodon user (Akash mounts it root-owned)
#   2. wait for postgres and redis (sibling services, reached by name)
#   3. rails db:prepare: creates + loads the schema on a fresh database,
#      migrates an existing one -- so an image upgrade migrates itself
#   4. run sidekiq and puma; if either exits, exit, and the provider restarts
#      the container (a half-dead instance is worse than a restart)
#
# Runs as root only for step 1; everything Mastodon runs as uid 991.
# ------------------------------------------------------------------
set -euo pipefail

cd /opt/mastodon
MEDIA=/opt/mastodon/public/system
as_mastodon() { setpriv --reuid=991 --regid=991 --init-groups env HOME=/opt/mastodon "$@"; }

# 1. media volume ownership (chown -R only when needed: a big media tree
#    makes every restart slow otherwise)
mkdir -p "$MEDIA"
if [ "$(stat -c %u "$MEDIA")" != "991" ]; then
    echo "mastodon-run: taking ownership of $MEDIA"
    chown -R 991:991 "$MEDIA"
fi

# 2. dependencies
wait_tcp() {  # <host> <port> <what>
    for i in $(seq 1 120); do
        if (exec 3<>"/dev/tcp/$1/$2") 2>/dev/null; then return 0; fi
        [ "$((i % 15))" -eq 1 ] && echo "mastodon-run: waiting for $3 at $1:$2"
        sleep 2
    done
    echo "mastodon-run: $3 at $1:$2 never answered" >&2
    return 1
}
wait_tcp "${DB_HOST:-db}" "${DB_PORT:-5432}" postgres
wait_tcp "${REDIS_HOST:-redis}" "${REDIS_PORT:-6379}" redis

# 3. schema
echo "mastodon-run: rails db:prepare"
as_mastodon bundle exec rails db:prepare

# 4. processes
as_mastodon bundle exec sidekiq &
SIDEKIQ=$!
as_mastodon bundle exec puma -C config/puma.rb &
PUMA=$!
trap 'kill -TERM "$SIDEKIQ" "$PUMA" 2>/dev/null' TERM INT
set +e
wait -n "$SIDEKIQ" "$PUMA"
rc=$?
echo "mastodon-run: a process exited ($rc); stopping the other so the container restarts"
kill -TERM "$SIDEKIQ" "$PUMA" 2>/dev/null
wait
exit "$rc"
