#!/bin/bash
# ------------------------------------------------------------------
# End-to-end test of wallet sign-in: the launcher's Mastodon image and the
# sdaplogin sidecar (sdap image), wired the way the launcher renders them,
# with the real OpenID Connect round trip between them. Only the chain and
# the wallet are faked: an nginx "LCD" serves x/rep member and x/name
# reverse_resolve answers from files, and walletsign produces what Keplr's
# signArbitrary would.
#
# Both hosts sit behind one TLS "edge" (a throwaway CA the web container
# trusts through SSL_CERT_FILE): Mastodon's OIDC client insists on an https
# issuer, and its session cookie is Secure.
#
# Checks:
#   1. Mastodon's sign-in page offers the provider; its request phase
#      discovers sdaplogin and redirects to /authorize with PKCE and a nonce
#   2. the login page lists the chain synced through mastodon-bootstrap
#   3. a non-member's signature is refused
#   4. a member's signature returns a code; Mastodon's callback redeems it
#      (token, id_token checked against the JWKS, userinfo) and signs in
#   5. the account is named from the x/name (phoenix-one -> phoenix_one),
#      keyed by the address hex, confirmed, and registrations stay closed
#   6. once the member is gone from the chain, the sweep asks sdaplogin and
#      disables the login
#
# Usage: test/federation/mastodon/wallet_login_e2e.sh
#   MASTODON_IMAGE=<img>  use that image instead of building one
#   SDAP_IMAGE=<img>      use that image instead of building one
#   KEEP=1                leave the containers running afterwards
# Needs docker, curl, jq, openssl, python3, go.
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$( cd "$SCRIPT_DIR/../../.." && pwd )"
P=sd-wallet-login
NET=$P-net
DOMAIN=masto.example
LOGIN=login.$DOMAIN
EDGE_PORT="${EDGE_PORT:-18543}"
IMAGE="${MASTODON_IMAGE:-sparkdream-mastodon:e2e}"
SDAP="${SDAP_IMAGE:-sparkdream-sdap:e2e}"
FAILS=0
WORK=$(mktemp -d)

ok()   { echo "[ OK ] $*"; }
bad()  { echo "[FAIL] $*"; FAILS=$((FAILS + 1)); }

cleanup() {
    [ "${KEEP:-0}" = "1" ] && { echo "KEEP=1: containers left running (docker rm -f \$(docker ps -aq --filter name=$P)); work dir $WORK"; return; }
    docker rm -f $(docker ps -aq --filter "name=$P") >/dev/null 2>&1 || true
    docker volume rm $P-db $P-redis $P-media $P-lcd $P-edge >/dev/null 2>&1 || true
    docker network rm $NET >/dev/null 2>&1 || true
    rm -rf "$WORK"
}
trap cleanup EXIT
cleanup
WORK=$(mktemp -d)

if [ -z "${MASTODON_IMAGE:-}" ]; then
    echo "=== building $IMAGE ==="
    docker build -q -f "$REPO_ROOT/deploy/docker/Dockerfile-mastodon" -t "$IMAGE" "$REPO_ROOT" >/dev/null
fi
if [ -z "${SDAP_IMAGE:-}" ]; then
    echo "=== building $SDAP ==="
    docker build -q -f "$REPO_ROOT/deploy/docker/Dockerfile-sdap" -t "$SDAP" "$REPO_ROOT" >/dev/null
fi
(cd "$REPO_ROOT" && go build -o "$WORK/walletsign" ./test/federation/mastodon/walletsign)

# --- a throwaway CA and one certificate for both hosts --------------------
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj "/CN=wallet-login e2e CA" \
    -keyout "$WORK/ca.key" -out "$WORK/ca.pem" 2>/dev/null
openssl req -newkey rsa:2048 -nodes -subj "/CN=$DOMAIN" -keyout "$WORK/edge.key" -out "$WORK/edge.csr" 2>/dev/null
printf 'subjectAltName=DNS:%s,DNS:%s\n' "$DOMAIN" "$LOGIN" > "$WORK/san.ext"
openssl x509 -req -in "$WORK/edge.csr" -CA "$WORK/ca.pem" -CAkey "$WORK/ca.key" -CAcreateserial -days 2 \
    -extfile "$WORK/san.ext" -out "$WORK/edge.pem" 2>/dev/null

# --- secrets, generated the way the launcher does -------------------------
hex() { openssl rand -hex "$1"; }
alnum() { openssl rand -base64 48 | tr -dc 'A-Za-z0-9' | head -c "$1"; }
b64url() { base64 -w0 | tr '+/' '-_'; }
VAPID_DER=$(openssl ecparam -name prime256v1 -genkey -noout -outform DER | base64 -w0)
VAPID_PRIVATE=$(echo "$VAPID_DER" | base64 -d | openssl ec -inform DER -text -noout 2>/dev/null \
    | sed -n '/priv:/,/pub:/p' | grep -v 'priv:\|pub:' | tr -d ' :\n' | xxd -r -p | b64url)
VAPID_PUBLIC=$(echo "$VAPID_DER" | base64 -d | openssl ec -inform DER -pubout -outform DER 2>/dev/null | tail -c 65 | b64url)
DB_PASS=$(hex 16)
CLIENT_SECRET=$(hex 24)
SIGNING_KEY=$(openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 2>/dev/null | openssl pkcs8 -topk8 -nocrypt -outform DER | base64 -w0)

WEB_ENV=(
    -e RAILS_ENV=production -e NODE_ENV=production
    -e LOCAL_DOMAIN=$DOMAIN -e LOCAL_HTTPS=true -e SPARKDREAM_ASSUME_SSL=true
    -e AUTHORIZED_FETCH=false -e RAILS_SERVE_STATIC_FILES=true
    -e DB_HOST=db -e DB_PORT=5432 -e DB_USER=postgres -e DB_NAME=mastodon -e DB_PASS=$DB_PASS
    -e REDIS_HOST=redis -e REDIS_PORT=6379
    -e SECRET_KEY_BASE=$(hex 64) -e OTP_SECRET=$(hex 64)
    -e VAPID_PRIVATE_KEY=$VAPID_PRIVATE -e VAPID_PUBLIC_KEY=$VAPID_PUBLIC
    -e ACTIVE_RECORD_ENCRYPTION_DETERMINISTIC_KEY=$(alnum 32)
    -e ACTIVE_RECORD_ENCRYPTION_KEY_DERIVATION_SALT=$(alnum 32)
    -e ACTIVE_RECORD_ENCRYPTION_PRIMARY_KEY=$(alnum 32)
    -e SMTP_DELIVERY_METHOD=file
    -e SPARKDREAM_WALLET_LOGIN=true -e SPARKDREAM_LOGIN_URL=https://$LOGIN
    -e OIDC_ENABLED=true "-e" "OIDC_DISPLAY_NAME=Spark Dream wallet" -e OIDC_ISSUER=https://$LOGIN
    -e OIDC_DISCOVERY=true -e OIDC_SCOPE=openid,profile,email -e OIDC_UID_FIELD=sub
    -e OIDC_CLIENT_ID=mastodon -e OIDC_CLIENT_SECRET=$CLIENT_SECRET -e OIDC_USE_PKCE=true
    -e OIDC_REDIRECT_URI=https://$DOMAIN/auth/auth/openid_connect/callback
    -e OIDC_SECURITY_ASSUME_EMAIL_IS_VERIFIED=true
    # trust the throwaway CA for the discovery / token / userinfo calls
    -e SSL_CERT_FILE=/e2e/ca.pem
)

echo "=== starting the stack ==="
docker network create $NET >/dev/null
docker volume create $P-edge >/dev/null
docker run --rm -v $P-edge:/e2e -v "$WORK":/w:ro alpine sh -c 'cp /w/ca.pem /w/edge.pem /w/edge.key /e2e/ && chmod 644 /e2e/*'
docker run -d --name $P-db --network $NET --network-alias db -v $P-db:/var/lib/postgresql/data \
    -e POSTGRES_PASSWORD=$DB_PASS -e PGDATA=/var/lib/postgresql/data/pgdata postgres:14-alpine >/dev/null
docker run -d --name $P-redis --network $NET --network-alias redis -v $P-redis:/data redis:7-alpine >/dev/null
docker run -d --name $P-web --network $NET --network-alias web -v $P-media:/opt/mastodon/public/system \
    -v $P-edge:/e2e:ro "${WEB_ENV[@]}" "$IMAGE" >/dev/null
# sdaplogin with the env the launcher renders: both sides reach each other
# through their public (edge) domains, as on Akash
docker run -d --name $P-login --network $NET --network-alias login -v $P-edge:/e2e:ro -e SSL_CERT_FILE=/e2e/ca.pem \
    -e LOGIN_ISSUER=https://$LOGIN -e LOGIN_CLIENT_ID=mastodon -e LOGIN_CLIENT_SECRET=$CLIENT_SECRET \
    -e LOGIN_REDIRECT_URI=https://$DOMAIN/auth/auth/openid_connect/callback \
    -e LOGIN_SIGNING_KEY=$SIGNING_KEY \
    -e LOGIN_CHAINS_URL=https://$DOMAIN/sparkdream/login-chains.json \
    -e LOGIN_REFRESH=2s \
    "$SDAP" sdaplogin >/dev/null
# the chain's LCD: answers are files under /srv, a missing file is a 404
docker volume create $P-lcd >/dev/null
docker run -d --name $P-lcd --network $NET --network-alias lcd -v $P-lcd:/srv nginx:alpine sh -c "cat > /etc/nginx/conf.d/default.conf <<'CONF'
server { listen 80; root /srv; default_type application/json; location / { try_files \$uri =404; } }
CONF
exec nginx -g 'daemon off;'" >/dev/null
# the edge: TLS for both hosts, as Cloudflare in front of the Akash ingress
docker run -d --name $P-edge --network $NET --network-alias $DOMAIN --network-alias $LOGIN \
    -v $P-edge:/e2e:ro -p 127.0.0.1:$EDGE_PORT:443 nginx:alpine sh -c "cat > /etc/nginx/conf.d/default.conf <<'CONF'
server {
  listen 443 ssl; server_name $DOMAIN;
  ssl_certificate /e2e/edge.pem; ssl_certificate_key /e2e/edge.key;
  location / { proxy_pass http://web:3000; proxy_set_header Host $DOMAIN; proxy_set_header X-Forwarded-Proto https; }
}
server {
  listen 443 ssl; server_name $LOGIN;
  ssl_certificate /e2e/edge.pem; ssl_certificate_key /e2e/edge.key;
  location / { proxy_pass http://login:8080; proxy_set_header Host $LOGIN; }
}
CONF
exec nginx -g 'daemon off;'" >/dev/null

JAR="$WORK/cookies"
C() { curl -s -m 30 --cacert "$WORK/ca.pem" --resolve "$DOMAIN:$EDGE_PORT:127.0.0.1" --resolve "$LOGIN:$EDGE_PORT:127.0.0.1" -b "$JAR" -c "$JAR" "$@"; }
# a URL the services hand out (default port) as the test reaches it
viaEdge() { sed -E "s#^https://($DOMAIN|$LOGIN)/#https://\\1:$EDGE_PORT/#"; }
lcd_put() { docker run --rm -v $P-lcd:/srv alpine sh -c "mkdir -p \$(dirname /srv/$1) && echo '$2' > /srv/$1"; }
lcd_rm()  { docker run --rm -v $P-lcd:/srv alpine rm -f "/srv/$1"; }

echo "=== waiting for /health (db:prepare on first start) ==="
for i in $(seq 1 90); do
    [ "$(C -o /dev/null -w '%{http_code}' "https://$DOMAIN:$EDGE_PORT/health")" = "200" ] && break
    sleep 4
done
[ "$(C "https://$DOMAIN:$EDGE_PORT/health")" = "OK" ] || { bad "web never became healthy"; docker logs --tail 40 $P-web; exit 1; }
B() { docker exec $P-web mastodon-bootstrap "$@" 2>/dev/null | tail -1; }
B registrations none >/dev/null
B login-chain sync '{"fleet-phoenix":{"chainId":"phoenix-1","chainName":"Phoenix","rpc":"https://rpc.phoenix.example","rest":"http://lcd","bech32Prefix":"sprkdrm","denom":"uspark","displayDenom":"SPARK","decimals":6,"gasPrice":0.025}}' >/dev/null

MEMBER=$("$WORK/walletsign" address phoenix-member sprkdrm)
STRANGER=$("$WORK/walletsign" address phoenix-stranger sprkdrm)
lcd_put "sparkdream/rep/v1/member/$MEMBER" '{"member":{"address":"'$MEMBER'","status":"MEMBER_STATUS_ACTIVE","trust_level":"TRUST_LEVEL_PROVISIONAL"}}'
lcd_put "sparkdream/name/v1/reverse_resolve/$MEMBER" '{"name":"phoenix-one"}'

# sdaplogin polls the chain list every 2s
sleep 5

# 1. Mastodon's request phase
PAGE=$(C "https://$DOMAIN:$EDGE_PORT/auth/sign_in")
CSRF=$(grep -o 'name="csrf-token" content="[^"]*"' <<<"$PAGE" | sed 's/.*content="//; s/"$//' || true)
grep -q '/auth/auth/openid_connect' <<<"$PAGE" && ok "sign-in page offers the wallet provider" || bad "no openid_connect button on /auth/sign_in"
AUTHZ=$(C -o /dev/null -w '%{redirect_url}' -X POST --data-urlencode "authenticity_token=$CSRF" \
    "https://$DOMAIN:$EDGE_PORT/auth/auth/openid_connect")
if [[ "$AUTHZ" == https://$LOGIN/authorize\?* ]] && [[ "$AUTHZ" == *code_challenge=* ]] && [[ "$AUTHZ" == *nonce=* ]]; then
    ok "request phase discovered sdaplogin and redirected with PKCE + nonce"
else
    bad "request phase redirected to '$AUTHZ'"; docker logs --tail 30 $P-web; exit 1
fi

# 2. the login page
LPAGE=$(C "$(viaEdge <<<"$AUTHZ")")
PJSON=$(python3 -c 'import sys,re; m=re.search(r"<script id=\"page\" type=\"application/json\">(.*?)</script>", sys.stdin.read(), re.S); print(m.group(1) if m else "{}")' <<<"$LPAGE")
REQ=$(jq -r .request <<<"$PJSON"); CHALLENGE=$(jq -r .challenge <<<"$PJSON")
[ "$(jq -r '.chains[0].chainId' <<<"$PJSON")" = "phoenix-1" ] && ok "login page lists the synced chain" || bad "login page JSON: $PJSON"

verify() {  # <seed> -> response body; the status code goes to $WORK/status
    local signed body
    signed=$("$WORK/walletsign" sign "$1" sprkdrm "$CHALLENGE")
    body=$(jq -c --arg r "$REQ" '{request:$r, chain:"fleet-phoenix", address:.address, signature:.signature}' <<<"$signed")
    C -o "$WORK/verify.json" -w '%{http_code}' -H 'Content-Type: application/json' -d "$body" \
        "https://$LOGIN:$EDGE_PORT/authorize/verify" > "$WORK/status"
    cat "$WORK/verify.json"
}

# 3. a stranger is refused
OUT=$(verify phoenix-stranger)
[ "$(cat "$WORK/status")" = "403" ] && grep -q "not an active member" <<<"$OUT" \
    && ok "a non-member's signature is refused" || bad "stranger: $(cat "$WORK/status") $OUT"

# 4. the member signs in; Mastodon redeems the code
OUT=$(verify phoenix-member)
BACK=$(jq -r '.redirect // empty' <<<"$OUT")
if [[ "$BACK" == https://$DOMAIN/auth/auth/openid_connect/callback\?code=* ]]; then ok "member's signature returns a code"
else bad "member verify: $(cat "$WORK/status") $OUT"; exit 1; fi
LANDING=$(C -o /dev/null -w '%{http_code} %{redirect_url}' "$(viaEdge <<<"$BACK")")
if [[ "$LANDING" == 302\ https://$DOMAIN/* ]] && [[ "$LANDING" != *"/auth/sign_in"* ]] && [[ "$LANDING" != *"/auth/setup"* ]]; then
    ok "callback redeemed the code and signed in ($LANDING)"
else
    bad "callback answered '$LANDING'"; docker logs --tail 40 $P-web
fi
WHO=$(C "https://$DOMAIN:$EDGE_PORT/settings/profile" -o /dev/null -w '%{http_code}')
[ "$WHO" = "200" ] && ok "the session is signed in (settings page 200)" || bad "settings page answered $WHO"

# 5. the account
R() { docker exec -u 991 -w /opt/mastodon $P-web bundle exec rails runner "$1" 2>/dev/null | tail -1; }
SUB=$("$WORK/walletsign" hex phoenix-member sprkdrm)
ACCT=$(R 'i = Identity.find_by(provider: "openid_connect", uid: "'$SUB'"); u = i&.user
  puts({ username: u&.account&.username, confirmed: u&.confirmed?, approved: u&.approved?,
         registrations: Setting.registrations_mode }.to_json)')
[ "$(jq -r .username <<<"$ACCT")" = "phoenix_one" ] && ok "account @phoenix_one keyed by the address hex" || bad "account: $ACCT"
[ "$(jq -r '[.confirmed,.approved]|all' <<<"$ACCT")" = "true" ] && [ "$(jq -r .registrations <<<"$ACCT")" = "none" ] \
    && ok "confirmed and approved while registrations stay closed" || bad "account state: $ACCT"

# 6. the member leaves the chain: the sweep asks sdaplogin and disables them
lcd_rm "sparkdream/rep/v1/member/$MEMBER"
GONE=$(R 'SparkdreamMembershipSweepWorker.new.perform
  puts({ disabled: Identity.find_by(provider: "openid_connect", uid: "'$SUB'").user.disabled? }.to_json)')
[ "$(jq -r .disabled <<<"$GONE")" = "true" ] && ok "sweep (through sdaplogin) disabled the former member" || bad "sweep: $GONE"

echo ""
if [ "$FAILS" -gt 0 ]; then echo ">>> $FAILS CHECK(S) FAILED <<<"; exit 1; fi
echo ">>> ALL CHECKS PASSED <<<"
