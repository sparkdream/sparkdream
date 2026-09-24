# P3.1 — standing up a test instance

Concrete setup for the ActivityPub server the Mastodon live link fetches
from. Written against Docker 29.x / Compose v5.x on WSL2.

Everything here is local. Nothing needs a public domain, a real TLS
certificate, or inbound firewall rules, because **nothing federates
Mastodon-to-Mastodon in this milestone** — the bridge and verifier are our
own code, fetching AS2 objects over HTTP from a server we control.

## Use Mastodon, not GoToSocial

GoToSocial looks like an easy stand-in (single binary, same wire protocol).
For *this* milestone it is not usable, and the reason is worth knowing before
you spend an hour on it.

**GoToSocial requires signed fetches and has no way to turn that off.**
Verified against v0.22.1: an AS2 GET with no HTTP signature returns

```
HTTP/1.1 401 Unauthorized
{"error":"Unauthorized: http request wasn't signed or http signature was invalid: (verifier)"}
```

There is no `AUTHORIZED_FETCH`-style option in its config — secure mode is
mandatory. Mastodon's `AUTHORIZED_FETCH=false` is precisely the affordance
P4 depends on, and only Mastodon has it.

Worse, the obvious workaround does not work either. Signing a fetch means
the *server* resolves your `keyId` URL to fetch your public key — so the
requester has to publish a reachable actor document. The bridge is
inbound-only and serves nothing; outbound is deferred to P9. So a signed
fetch against GoToSocial needs infrastructure this milestone does not build.

GoToSocial becomes useful later, as a CI stand-in (phase P8) and
as a way to exercise the P5 HTTP-signature path against a server that
*demands* signatures. Not now.

So: **go straight to Mastodon.**

---

## Mastodon

Verified against the v4.7.2 compose file (current release, 2026-09-15).

**Clone at the release tag, not `main`.** The compose file carries a pinned
`image: ghcr.io/mastodon/mastodon:v4.7.2`, so checking out the tag gets a
compose file and an image that agree. A plain clone of `main` picks up
whatever version `main` currently points at.

```bash
git clone --depth 1 --branch v4.7.2 https://github.com/mastodon/mastodon.git ~/mastodon
cd ~/mastodon
```

The prebuilt image means no local build — that is most of the setup time
saved. (`build: .` is present but commented out, for local code changes.)

**Create an empty `.env.production` before running anything.** Every service
in the compose file declares `env_file: .env.production`, so
`docker compose run` fails outright if the file is missing — including the
commands below that exist to generate its contents. Chicken and egg:

```bash
touch .env.production
```

Now generate the secrets Mastodon refuses to boot without. The one-off
commands run in the **`web`** service — the compose file defines exactly
`db`, `redis`, `web`, `streaming` and `sidekiq`, and there is no `shell`
service:

```bash
docker compose run --rm web bin/rails secret    # SECRET_KEY_BASE
docker compose run --rm web bin/rails secret    # OTP_SECRET (run twice; different values)
docker compose run --rm web bundle exec rake mastodon:webpush:generate_vapid_key
docker compose run --rm web bin/rails db:encryption:init   # 3 ACTIVE_RECORD_ENCRYPTION_* values
```

**`rake secret` no longer exists.** Rails removed the rake task; it is a
Rails *command* now. `bundle exec rake secret` fails with
`Don't know how to build task 'secret'`. Much of the Mastodon setup
documentation still shows the old form.

The other two are still rake tasks (`rake --tasks` in the v4.7.2 image lists
`mastodon:webpush:generate_vapid_key`), and `db:encryption:init` is a Rails
command. So the mix of `bin/rails` and `bundle exec rake` above is correct,
not an inconsistency.

Each command prints the value(s) to paste below. `db:encryption:init` prints
all three `ACTIVE_RECORD_ENCRYPTION_*` lines ready to copy; the VAPID task
prints both keys as `KEY=value` lines.

All four were run against `ghcr.io/mastodon/mastodon:v4.7.2` to confirm.

Write `.env.production`:

```ini
LOCAL_DOMAIN=localhost:3000
LOCAL_HTTPS=false
ALTERNATE_DOMAINS=localhost

# CRITICAL for this milestone, and the reason GoToSocial cannot be used.
# With secure mode ON, an unsigned AS2 GET returns 401, which drags
# HTTP-Signature signing forward into the manual rehearsal (P4) -- exactly
# what P4 is designed to avoid. Signing is P5 work, where it is needed
# anyway for real-world peers.
AUTHORIZED_FETCH=false

DB_HOST=db
DB_USER=postgres
DB_NAME=postgres
DB_PASS=
DB_PORT=5432
REDIS_HOST=redis
REDIS_PORT=6379

SECRET_KEY_BASE=<first rake secret>
OTP_SECRET=<second rake secret>
VAPID_PRIVATE_KEY=<from the vapid task>
VAPID_PUBLIC_KEY=<from the vapid task>
ACTIVE_RECORD_ENCRYPTION_DETERMINISTIC_KEY=<from db:encryption:init>
ACTIVE_RECORD_ENCRYPTION_KEY_DERIVATION_SALT=<from db:encryption:init>
ACTIVE_RECORD_ENCRYPTION_PRIMARY_KEY=<from db:encryption:init>

# No SMTP. Accounts get confirmed with tootctl instead (below).
SMTP_DELIVERY_METHOD=file
```

**`LOCAL_HTTPS=false` is ignored in production on v4.7.x.** Both switches
are hardcoded: `config/initializers/1_hosts.rb` computes
`https = Rails.env.production? || ENV['LOCAL_HTTPS'] == 'true'`, and
`config/environments/production.rb` sets `config.force_ssl = true`. Without
an override, every request except `/health` gets a 301 to
`https://localhost:3000/...`, and every AS2 `id` is stamped `https://`. A
Go client can reach neither without a real certificate. Undo both with one
extra initializer, bind-mounted so the checkout stays clean. It sorts
directly after `1_hosts.rb` and only acts when `LOCAL_HTTPS=false` is set
explicitly:

```bash
mkdir -p local-overrides
cat > local-overrides/1_hosts_local_http.rb <<'EOF'
# frozen_string_literal: true

# Local test-instance override (sparkdream federation harness, not upstream).
# v4.7.x forces https whenever RAILS_ENV=production, ignoring LOCAL_HTTPS.
# Loaded right after 1_hosts.rb; only acts when LOCAL_HTTPS=false is explicit.
if ENV['LOCAL_HTTPS'] == 'false'
  Rails.application.configure do
    web_host = config.x.web_domain

    config.force_ssl   = false
    config.x.use_https = false
    config.action_mailer.default_url_options = { host: web_host, protocol: 'http://', trailing_slash: false }
    config.x.streaming_api_base_url = ENV.fetch('STREAMING_API_BASE_URL') { "ws://#{web_host.split(':').first}:4000" }
  end
end
EOF
cat > compose.override.yml <<'EOF'
services:
  web:
    volumes:
      - ./local-overrides/1_hosts_local_http.rb:/opt/mastodon/config/initializers/1_hosts_local_http.rb:ro
  sidekiq:
    volumes:
      - ./local-overrides/1_hosts_local_http.rb:/opt/mastodon/config/initializers/1_hosts_local_http.rb:ro
EOF
```

Mastodon builds every URL, AS2 ids included, from
`ActionMailer::Base.default_url_options` (see `app/helpers/routing_helper.rb`),
so resetting that one option fixes the `id` scheme as well as the redirect.
`docker compose` picks up `compose.override.yml` automatically.

Bring it up. The web service publishes `127.0.0.1:3000:3000`, which is why
`LOCAL_DOMAIN=localhost:3000` matches — change one and you must change both:

```bash
docker compose run --rm web bundle exec rake db:setup
docker compose up -d
docker compose logs -f web        # wait for "Listening on 0.0.0.0:3000"
```

**Give the media directory to the container user.** Docker creates the
`./public/system` bind mount as root, but Mastodon runs as uid 991, so the
first media upload fails with a 500 (`Errno::EACCES ... dir_s_mkdir -
/opt/mastodon/public/system/media_attachments`). No sudo is needed:

```bash
docker compose exec -u root web chown -R mastodon:mastodon /opt/mastodon/public/system
```

Create a confirmed account without email, then **approve it**:

```bash
docker compose run --rm web bin/tootctl accounts create alice \
  --email alice@localhost --confirmed --role Owner
docker compose run --rm web bin/tootctl accounts approve alice
```

`accounts create` prints a generated password. Log in at `http://localhost:3000`.

`--confirmed` does not approve the account. The default registration mode
requires approval, and an unapproved account 404s on both `/@alice` and its
AS2 actor, even though `/api/v1/accounts/lookup` still returns it.

**`LOCAL_DOMAIN=localhost:3000` is the load-bearing choice.** It is what
Mastodon stamps into every AS2 `id`, so ids come out as
`http://localhost:3000/users/alice/statuses/N` — fetchable by the daemons
with no DNS and no certificate. A hosts-file domain would need real TLS,
because Go rejects self-signed certificates and `apcanon` has no
skip-verify escape hatch (deliberately).

**If a start fails partway, reset the database before retrying.** A run
that dies mid-migration leaves a half-migrated schema that will not recover
on the next attempt — it fails with a missing-table error from a migration
that already "ran". `docker compose down -v` then `rake db:setup` again.
(Seen for real on GoToSocial's SQLite during the evaluation above; the same
hazard applies to Postgres here.)

Verify the instance serves AS2 **unsigned** before going further — this is
the single check that says `AUTHORIZED_FETCH=false` took effect:

```bash
curl -sH 'Accept: application/activity+json' http://localhost:3000/users/alice | jq -r .id
# -> http://localhost:3000/ap/users/<numeric account id>
```

A `401` with "request wasn't signed" means secure mode is still on. A `301`
to `https://` means the http override above is not loaded. A `404` means
the account has not been approved.

---

## Getting the right URI

The bridge anchors, and the verifier re-fetches, the **AS2 `id`** — never
the web permalink. They are different URLs for the same post:

| | Mastodon v4.7 | GoToSocial |
|---|---|---|
| web permalink (`url`) | `http://host/@alice/123` | `http://host/@alice/statuses/123` |
| **AS2 id** | `http://host/ap/users/<acct id>/statuses/123` | `http://host/users/alice/statuses/123` |

Mastodon v4.7 mints **numeric** AS2 ids (`/ap/users/<account id>/...`) for
new accounts. The username path `/users/alice/statuses/123` still resolves,
but it is not the `id`, so never anchor it. The API returns the id directly
as `uri` on every status, and webfinger's `rel=self` link gives the actor id.

To get the id from a permalink:

```bash
curl -sH 'Accept: application/activity+json' 'http://localhost:3000/@alice/123' | jq -r .id
```

A permalink only resolves to AS2 because Mastodon content-negotiates, which
no other ActivityPub server guarantees — and a *signed* fetch of a permalink
redirects, invalidating a signature bound to `(request-target)`.

---

## Why `--allow-private` is needed, and when it is not

`apcanon.Fetch` refuses `http://`, loopback and RFC1918 destinations by
default, re-checks every redirect hop, and drops signature headers on a
cross-host redirect. That guard exists because both daemons fetch URIs that
originate in inbound federation traffic, so a cooperating remote instance
could otherwise redirect them into the daemon host's own network or at cloud
metadata.

A local test instance is exactly the case the guard blocks, so bring-up
opts out explicitly:

```bash
go run ./tools/apcanon/cmd/apcanon hash --allow-private http://localhost:3000/...
ALLOW_PRIVATE=1 ./determinism_check.sh --phase baseline <uris>
ALLOW_PRIVATE=1 ./rehearsal.sh
SDA_ALLOW_PRIVATE_HOSTS=1 ./sdapbridge          # daemons read an env var
```

**Never set it against a real peer.** It is a bring-up affordance, not a
configuration option.

Note that `apcanon` flags precede the target (`hash --base64 <uri>`, not
`hash <uri> --base64`) — Go's flag package stops parsing at the first
non-flag argument.

---

## What to post for the P2 determinism run

P2 phase 3 requires the hash to move on every edit class, so create one
status per case and keep the AS2 ids:

1. plain text
2. with a content warning (`summary`)
3. with a media attachment
4. a reply to another status (`inReplyTo`)
5. one you will edit mid-run (body edit, then CW add/remove, then media
   add/remove) — each edit must flip the hash, and `updated` must be
   populated afterwards

Then:

```bash
cd test/federation/mastodon
ALLOW_PRIVATE=1 ./determinism_check.sh --phase baseline \
  http://localhost:3000/ap/users/<acct id>/statuses/<id1> \
  http://localhost:3000/ap/users/<acct id>/statuses/<id2>
```

Leave status 5 unedited until the baseline finishes. The edits phase
compares against `baseline.txt`, so pass it only the URI you edited.

The baseline phase re-fetches over several hours. If the hash is not stable
across repeated fetches, **stop** — the rule needs revision before anything
is spent on-chain, which is the entire reason P2 gates P4.

---

## Resource notes

Mastodon wants ~2–4 GB RAM across web + sidekiq + streaming + Postgres +
Redis — comfortable on this machine (25 GB total). The first `db:setup` and
asset build take the bulk of the setup time.

Pick a port nothing else is using. `ss -tlnp` under-reports listeners on
WSL2, so a port can be occupied while `ss` shows it free; the failure mode
is a bind panic at startup. If 3000 is taken, change both the compose port
mapping and `LOCAL_DOMAIN` together — they must agree, because
`LOCAL_DOMAIN` is what ends up inside every AS2 `id`.
