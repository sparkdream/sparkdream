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
#   6. wallet sign-in (zz_sparkdream_wallet_login.rb), with registrations
#      none: login-chain sync is served at /sparkdream/login-chains.json; a
#      provider sign-in creates a confirmed, approved account named from the
#      x/name nickname (never the address uid); a clash gets a suffix; no
#      nickname creates nothing; the sweep disables a non-member and
#      re-enables them, and leaves an admin-disabled account alone; the
#      bridge-only wallet_addresses endpoint (follow-back). A stub
#      stands in for sdaplogin's /membership endpoint (sdaplogin itself is
#      covered by go test ./cmd/sdaplogin)
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
    docker volume rm $P-db $P-redis $P-media $P-stub >/dev/null 2>&1 || true
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
    # wallet sign-in, as the launcher renders it; the provider is never
    # reached here (sign-ins are simulated below), the stub answers the sweep
    -e SPARKDREAM_WALLET_LOGIN=true -e SPARKDREAM_LOGIN_URL=http://login:8080
    -e OIDC_ENABLED=true -e OIDC_DISPLAY_NAME=Spark-Dream -e OIDC_ISSUER=https://login.$DOMAIN
    -e OIDC_DISCOVERY=true -e OIDC_SCOPE=openid,profile,email -e OIDC_UID_FIELD=sub
    -e OIDC_CLIENT_ID=mastodon -e OIDC_CLIENT_SECRET=$(hex 16) -e OIDC_USE_PKCE=true
    -e OIDC_REDIRECT_URI=https://$DOMAIN/auth/auth/openid_connect/callback
    -e OIDC_SECURITY_ASSUME_EMAIL_IS_VERIFIED=true
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
# stand-in for sdaplogin's /membership/<uid>: answers whatever status.json holds
docker volume create $P-stub >/dev/null
docker run --rm -v $P-stub:/srv alpine sh -c 'echo "{\"status\":\"active\"}" > /srv/status.json'
docker run -d --name $P-login --network $NET --network-alias login -v $P-stub:/srv nginx:alpine sh -c "cat > /etc/nginx/conf.d/default.conf <<'CONF'
server {
  listen 8080;
  location /membership/ { default_type application/json; root /srv; try_files /status.json =404; }
}
CONF
exec nginx -g 'daemon off;'" >/dev/null
stub() { docker run --rm -v $P-stub:/srv alpine sh -c "echo '{\"status\":\"$1\"}' > /srv/status.json"; }

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
# the bridge profile states the open-content rule before anyone opts in
CREDS=$(get -H "Authorization: Bearer $T1" "http://127.0.0.1:$PROXY_PORT/api/v1/accounts/verify_credentials")
if jq -r '.note // empty' <<<"$CREDS" | grep -qi 'cc0' \
    && [ "$(jq -r '[.fields[]? | select(.name == "License")] | length' <<<"$CREDS")" = "1" ]; then
    ok "bridge bio and License field state the CC0 rule"
else bad "bridge profile lacks the CC0 rule: $(jq -c '{note, fields}' <<<"$CREDS")"; fi

# 3. AS2 actor with https ids
ACTOR=$(get -H 'Accept: application/activity+json' "http://127.0.0.1:$PROXY_PORT/users/admin" | jq -r '.id // empty')
if [[ "$ACTOR" == https://$DOMAIN/* ]]; then ok "AS2 actor id $ACTOR"; else bad "AS2 actor id '$ACTOR'"; fi

# 5. media volume
OWNER=$(docker exec $P-web stat -c %u /opt/mastodon/public/system)
[ "$OWNER" = "991" ] && ok "media volume taken over by uid 991" || bad "media volume owned by uid $OWNER"

# streaming process up (reachable only inside the network here)
if docker exec $P-web curl -s -m 5 http://streaming:4000/api/v1/streaming/health | grep -q OK; then ok "streaming healthy"
else bad "streaming not healthy"; fi

# 6. wallet sign-in
echo "=== wallet sign-in ==="
[ "$(jq -r .registrations <<<"$(B registrations none)")" = "none" ] \
    && ok "registrations closed (none)" || bad "registrations not closed"

CHAINS='{"fleet-phoenix":{"chainId":"phoenix-1","chainName":"Phoenix","rest":"https://api.phoenix.example","bech32Prefix":"sprkdrm"}}'
[ "$(jq -r .chains <<<"$(B login-chain sync "$CHAINS")")" = "1" ] && ok "login-chain sync stored one chain" || bad "login-chain sync"
SERVED=$(get "http://127.0.0.1:$PROXY_PORT/sparkdream/login-chains.json" | jq -r '."fleet-phoenix".chainId // empty')
[ "$SERVED" = "phoenix-1" ] && ok "/sparkdream/login-chains.json serves the stored chains" || bad "login-chains.json served '$SERVED'"
if docker exec $P-web mastodon-bootstrap login-chain sync '[1,2]' >/dev/null 2>&1; then bad "login-chain sync accepted a non-object"
else ok "login-chain sync refuses a non-object"; fi

SCHED=$(docker exec -w /opt/mastodon $P-web ruby -ryaml -rerb -e \
    'puts YAML.load(ERB.new(File.read("config/sidekiq.yml")).result)[:scheduler][:schedule]["sparkdream_membership_sweep"]["class"]' 2>&1)
[ "$SCHED" = "SparkdreamMembershipSweepWorker" ] && ok "sweep scheduled in sidekiq.yml" || bad "sweep schedule: $SCHED"

# sign-ins as the callback controller makes them (User.find_for_omniauth on
# the strategy's auth hash), then the sweep against the stub
R() { docker exec -u 991 -w /opt/mastodon $P-web bundle exec rails runner "$1" 2>/dev/null | tail -1; }
SIGNIN='
  def signin(uid, nick)
    auth = OmniAuth::AuthHash.new(provider: "openid_connect", uid: uid,
      info: { nickname: nick, name: nick, email: "#{uid}@wallet.invalid", email_verified: true })
    User.find_for_omniauth(auth)
  end
'
UID_A=4e9be94a169918bba094cbf07be47d41975580b4
UID_B=1111111111111111111111111111111111111111
UID_C=2222222222222222222222222222222222222222
OUT=$(R "$SIGNIN"'
  a  = signin("'$UID_A'", "phoenix-one")
  a2 = signin("'$UID_A'", "phoenix-renamed")
  b  = signin("'$UID_B'", "phoenix-one")
  blank = begin; signin("'$UID_C'", ""); "created"; rescue ActiveRecord::RecordInvalid; "refused"; end
  puts({ a: a.account.username, confirmed: a.confirmed?, approved: a.approved?, external: a.external,
         same: a.id == a2.id, b: b.account.username, blank: blank,
         c_exists: Identity.exists?(provider: "openid_connect", uid: "'$UID_C'") }.to_json)')
[ "$(jq -r .a <<<"$OUT")" = "phoenix_one" ] && ok "handle phoenix_one from x/name phoenix-one (not the uid)" || bad "sign-in: $OUT"
[ "$(jq -r '[.confirmed,.approved,.external]|all' <<<"$OUT")" = "true" ] \
    && ok "confirmed and approved with registrations closed" || bad "account state: $OUT"
[ "$(jq -r .same <<<"$OUT")" = "true" ] && ok "same uid signs in to the same account after a rename" || bad "rename: $OUT"
[ "$(jq -r .b <<<"$OUT")" = "phoenix_one_1" ] && ok "a clashing handle gets a suffix" || bad "clash: $OUT"
[ "$(jq -r '.blank + " " + (.c_exists|tostring)' <<<"$OUT")" = "refused false" ] \
    && ok "no x/name, no account" || bad "blank nickname: $OUT"

SWEEP='SparkdreamMembershipSweepWorker.new.perform
  state = ->(uid) { Identity.find_by(provider: "openid_connect", uid: uid).user.disabled? }
  puts({ a: state.("'$UID_A'"), b: state.("'$UID_B'") }.to_json)'
# B is disabled by an admin, so the sweep must never re-enable it
R 'Identity.find_by(provider: "openid_connect", uid: "'$UID_B'").user.disable!' >/dev/null
stub inactive
# guard: a stub that never answers would pass the checks below vacuously
STUB=$(docker exec $P-web curl -s -m 5 http://login:8080/membership/$UID_A | jq -r '.status // empty')
[ "$STUB" = "inactive" ] || { bad "membership stub answered '$STUB'"; }
[ "$(R "$SWEEP" | jq -c .)" = '{"a":true,"b":true}' ] && ok "sweep disables a non-member" || bad "sweep (inactive)"
stub active
[ "$(R "$SWEEP" | jq -c .)" = '{"a":false,"b":true}' ] \
    && ok "sweep re-enables its own, leaves the admin's disabled" || bad "sweep (active)"
stub unknown
[ "$(R "$SWEEP" | jq -c .)" = '{"a":false,"b":true}' ] && ok "sweep leaves accounts alone on an unknown answer" || bad "sweep (unknown)"

# the bridge's follow-back reads its wallet-account followers' addresses:
# only with a bridge token, only for accounts that follow that bridge
# its last JSON line: a first follow! logs the jobs it enqueues after it
RJ() { docker exec -u 991 -w /opt/mastodon $P-web bundle exec rails runner "$1" 2>/dev/null | grep '^{' | tail -1; }
IDS=$(RJ 'a = Account.find_local("phoenix_one"); b = Account.find_local("phoenix_one_1"); a.follow!(Account.find_local("bridge"))
  puts({ a: a.id.to_s, b: b.id.to_s }.to_json)')
A_ID=$(jq -r .a <<<"$IDS"); B_ID=$(jq -r .b <<<"$IDS")
WA="http://127.0.0.1:$PROXY_PORT/api/v1/sparkdream/wallet_addresses?id%5B%5D=$A_ID&id%5B%5D=$B_ID"
GOT=$(get -H "Authorization: Bearer $T1" "$WA" | jq -c .)
[ "$GOT" = "{\"$A_ID\":\"$UID_A\"}" ] && ok "bridge token reads its wallet followers' addresses, and no one else's" \
    || bad "wallet_addresses with the bridge token: $GOT"
[ "$(get -o /dev/null -w '%{http_code}' "$WA")" = "401" ] && ok "wallet_addresses refuses a request with no token" || bad "wallet_addresses answered without a token"
OTHER=$(R 'app = Doorkeeper::Application.find_or_create_by!(name: "other app") { |a| a.redirect_uri = "urn:ietf:wg:oauth:2.0:oob"; a.scopes = "read" }
  puts Doorkeeper::AccessToken.create!(application: app, resource_owner_id: Account.find_local("phoenix_one").user.id, scopes: "read").token')
[ "$(get -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $OTHER" "$WA")" = "401" ] \
    && ok "wallet_addresses refuses a token of any other app" || bad "wallet_addresses answered another app's token"

echo ""
if [ "$FAILS" -gt 0 ]; then echo ">>> $FAILS CHECK(S) FAILED <<<"; exit 1; fi
echo ">>> ALL CHECKS PASSED <<<"
