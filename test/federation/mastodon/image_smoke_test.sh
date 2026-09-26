#!/bin/bash
# ------------------------------------------------------------------
# Smoke test for the launcher's Mastodon image (deploy/docker/Dockerfile-
# mastodon), run the way the launcher deploys it on Akash: postgres, redis,
# web+sidekiq (the derived image) and upstream streaming as separate
# containers, behind a reverse proxy that -- like an Akash ingress fronted by
# Cloudflare -- speaks plain HTTP to the pod and sets X-Forwarded-Proto: http.
#
# Checks:
#   1. mastodon-run prepares the schema by itself and serves /health
#   2. no redirect loop behind the http-only proxy (SPARKDREAM_ASSUME_SSL):
#      a normal page answers without a redirect to https://
#   3. the AS2 actor is fetchable with an https id (what the bridge anchors)
#   4. mastodon-bootstrap: owner created once (password printed once),
#      registrations set, the bridge token stable across calls, and the token
#      reads /api/v1/accounts/verify_credentials
#   5. the media volume, mounted root-owned as Akash does, was taken over
#
# Usage: test/federation/mastodon/image_smoke_test.sh
#   MASTODON_IMAGE=<img>  test that image instead of building one
#   KEEP=1                leave the containers running afterwards
# Needs docker, curl, jq, openssl.
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$( cd "$SCRIPT_DIR/../../.." && pwd )"
P=sd-masto-smoke
NET=$P-net
DOMAIN=masto.example
PROXY_PORT="${PROXY_PORT:-18480}"
IMAGE="${MASTODON_IMAGE:-sparkdream-mastodon:e2e}"
STREAMING_IMAGE="${STREAMING_IMAGE:-ghcr.io/mastodon/mastodon-streaming:v4.7.2}"
FAILS=0

ok()   { echo "[ OK ] $*"; }
bad()  { echo "[FAIL] $*"; FAILS=$((FAILS + 1)); }

cleanup() {
    [ "${KEEP:-0}" = "1" ] && { echo "KEEP=1: containers left running (docker rm -f \$(docker ps -aq --filter name=$P))"; return; }
    docker rm -f $(docker ps -aq --filter "name=$P") >/dev/null 2>&1 || true
    docker volume rm $P-db $P-redis $P-media >/dev/null 2>&1 || true
    docker network rm $NET >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

if [ -z "${MASTODON_IMAGE:-}" ]; then
    echo "=== building $IMAGE ==="
    docker build -q -f "$REPO_ROOT/deploy/docker/Dockerfile-mastodon" -t "$IMAGE" "$REPO_ROOT" >/dev/null
fi

# secrets, generated the way the launcher does (random, not rake tasks)
hex() { openssl rand -hex "$1"; }
alnum() { openssl rand -base64 48 | tr -dc 'A-Za-z0-9' | head -c "$1"; }
b64url() { base64 -w0 | tr '+/' '-_'; }
VAPID_DER=$(openssl ecparam -name prime256v1 -genkey -noout -outform DER | base64 -w0)
VAPID_PRIVATE=$(echo "$VAPID_DER" | base64 -d | openssl ec -inform DER -text -noout 2>/dev/null \
    | sed -n '/priv:/,/pub:/p' | grep -v 'priv:\|pub:' | tr -d ' :\n' | xxd -r -p | b64url)
VAPID_PUBLIC=$(echo "$VAPID_DER" | base64 -d | openssl ec -inform DER -pubout -outform DER 2>/dev/null | tail -c 65 | b64url)
DB_PASS=$(hex 16)

ENV=(
    -e RAILS_ENV=production -e NODE_ENV=production
    -e LOCAL_DOMAIN=$DOMAIN -e LOCAL_HTTPS=true -e SPARKDREAM_ASSUME_SSL=true
    -e AUTHORIZED_FETCH=false -e RAILS_SERVE_STATIC_FILES=true
    -e STREAMING_API_BASE_URL=wss://streaming.$DOMAIN
    -e DB_HOST=db -e DB_PORT=5432 -e DB_USER=postgres -e DB_NAME=mastodon -e DB_PASS=$DB_PASS
    -e REDIS_HOST=redis -e REDIS_PORT=6379
    -e SECRET_KEY_BASE=$(hex 64) -e OTP_SECRET=$(hex 64)
    -e VAPID_PRIVATE_KEY=$VAPID_PRIVATE -e VAPID_PUBLIC_KEY=$VAPID_PUBLIC
    -e ACTIVE_RECORD_ENCRYPTION_DETERMINISTIC_KEY=$(alnum 32)
    -e ACTIVE_RECORD_ENCRYPTION_KEY_DERIVATION_SALT=$(alnum 32)
    -e ACTIVE_RECORD_ENCRYPTION_PRIMARY_KEY=$(alnum 32)
    -e SMTP_DELIVERY_METHOD=file
)

echo "=== starting the stack ==="
docker network create $NET >/dev/null
docker volume create $P-media >/dev/null
docker run -d --name $P-db --network $NET --network-alias db -v $P-db:/var/lib/postgresql/data \
    -e POSTGRES_PASSWORD=$DB_PASS -e PGDATA=/var/lib/postgresql/data/pgdata postgres:14-alpine >/dev/null
docker run -d --name $P-redis --network $NET --network-alias redis -v $P-redis:/data redis:7-alpine >/dev/null
# the media volume starts root-owned, as an Akash persistent volume does
docker run --rm -v $P-media:/m alpine sh -c 'chown 0:0 /m && chmod 755 /m'
docker run -d --name $P-web --network $NET --network-alias web -v $P-media:/opt/mastodon/public/system \
    "${ENV[@]}" "$IMAGE" >/dev/null
docker run -d --name $P-streaming --network $NET --network-alias streaming \
    "${ENV[@]}" "$STREAMING_IMAGE" node ./streaming/index.js >/dev/null
# the "ingress": plain HTTP to the pod, X-Forwarded-Proto rewritten to http
docker run -d --name $P-proxy --network $NET -p 127.0.0.1:$PROXY_PORT:80 nginx:alpine sh -c "cat > /etc/nginx/conf.d/default.conf <<'CONF'
server {
  listen 80;
  location / {
    proxy_pass http://web:3000;
    proxy_set_header Host \$host;
    proxy_set_header X-Forwarded-Proto http;
    proxy_set_header X-Forwarded-For \$remote_addr;
  }
}
CONF
exec nginx -g 'daemon off;'" >/dev/null

get() { curl -s -m 20 -H "Host: $DOMAIN" "$@"; }

echo "=== waiting for /health (db:prepare on first start) ==="
for i in $(seq 1 90); do
    [ "$(get -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PROXY_PORT/health")" = "200" ] && break
    sleep 4
done
if [ "$(get "http://127.0.0.1:$PROXY_PORT/health")" = "OK" ]; then ok "web answers /health through the proxy"; else
    bad "web never became healthy"; docker logs --tail 40 $P-web; exit 1; fi

# 2. no redirect loop
CODE=$(get -o /dev/null -w '%{http_code} %{redirect_url}' "http://127.0.0.1:$PROXY_PORT/about")
if [[ "$CODE" == 200* ]]; then ok "/about serves 200 behind the http-only proxy"
else bad "/about answered '$CODE' (a redirect to https:// here is the force_ssl loop)"; fi

# 4. bootstrap
B() { docker exec $P-web mastodon-bootstrap "$@" 2>/dev/null | tail -1; }
FIRST=$(B owner admin admin@$DOMAIN)
SECOND=$(B owner admin admin@$DOMAIN)
if [ "$(jq -r .created <<<"$FIRST")" = "true" ] && [ -n "$(jq -r '.password // empty' <<<"$FIRST")" ] \
    && [ "$(jq -r .created <<<"$SECOND")" = "false" ]; then ok "owner created once, password printed once"
else bad "owner bootstrap: first=$FIRST second=$SECOND"; fi

[ "$(jq -r .registrations <<<"$(B registrations approved)")" = "approved" ] \
    && ok "registrations set to approved" || bad "registrations not set"

T1=$(jq -r .token <<<"$(B bridge-token bridge bridge@$DOMAIN)")
T2=$(jq -r .token <<<"$(B bridge-token bridge bridge@$DOMAIN)")
if [ -n "$T1" ] && [ "$T1" = "$T2" ]; then ok "bridge token issued and stable"; else bad "bridge token: '$T1' vs '$T2'"; fi
WHO=$(get -H "Authorization: Bearer $T1" "http://127.0.0.1:$PROXY_PORT/api/v1/accounts/verify_credentials" | jq -r '.username // empty')
[ "$WHO" = "bridge" ] && ok "the token reads as @bridge" || bad "verify_credentials answered '$WHO'"

# 3. AS2 actor with https ids
ACTOR=$(get -H 'Accept: application/activity+json' "http://127.0.0.1:$PROXY_PORT/users/admin" | jq -r '.id // empty')
if [[ "$ACTOR" == https://$DOMAIN/* ]]; then ok "AS2 actor id $ACTOR"; else bad "AS2 actor id '$ACTOR'"; fi

# 5. media volume
OWNER=$(docker exec $P-web stat -c %u /opt/mastodon/public/system)
[ "$OWNER" = "991" ] && ok "media volume taken over by uid 991" || bad "media volume owned by uid $OWNER"

# streaming process up (reachable only inside the network here)
if docker exec $P-web curl -s -m 5 http://streaming:4000/api/v1/streaming/health | grep -q OK; then ok "streaming healthy"
else bad "streaming not healthy"; fi

echo ""
if [ "$FAILS" -gt 0 ]; then echo ">>> $FAILS CHECK(S) FAILED <<<"; exit 1; fi
echo ">>> ALL CHECKS PASSED <<<"
