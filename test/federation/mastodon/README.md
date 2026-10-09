# Mastodon Live Link — harness directory

Scripts and fixtures for the live ActivityPub link between a Mastodon
instance and Spark Dream. The on-chain half is covered by the simulated
suite in [test/federation/](../); this directory owns everything that
touches a real Mastodon instance. The durable design (hash rules, bridge
and verifier conventions, provenance, edits, media) lives in
[docs/x-federation-spec.md](../../../docs/x-federation-spec.md) under
`SubmitFederatedContent`.

## Mastodon link status

Phase labels (P0–P9) appear in these scripts and in code comments; this
table defines them.

| Phase | What | Status |
|---|---|---|
| **P0** | Chain prerequisites: **P0.1** release the verifier's committed bond on every exit; **P0.2** daemon messages in the x/session ceiling; **P0.3** stale bond comments; **P0.4** `ListFederatedContent` filters | Done. P0.1 confirmed live by the local P4 run; P0.2 takes effect on devnet only with the P3.0 reset |
| **P1** | `tools/apcanon`: the `ap-canonical-v1` and `ap-canonical-v2` hash rules and CLI | Done |
| **P2** | Determinism: hash stability off-chain before any transaction | Done for v1 and v2 ([accepted/](accepted/)). Second vantage point not run; upgrade check skipped (instance on the current stable release) |
| **P3** | **P3.0** devnet release + reset; **P3.1** Mastodon instance ([INSTANCE_SETUP.md](INSTANCE_SETUP.md)); **P3.2** governance proposal; **P3.3** peer, policy, bridge and verifier setup | Done on a local chain. Devnet waits on P3.0 |
| **P4** | Manual on-chain rehearsal against a real post, happy path | Done on a local chain (`rehearsal.sh` PASS). Devnet waits on P3 |
| **P5** | Bridge daemon `cmd/sdapbridge`. **P5.1** discovery (home timeline) separate from ingestion (AS2 fetch); **P5.3** keys, HTTP signatures, metadata | Done, run unattended locally |
| **P6** | Verifier runner `cmd/sdapverify` | Done, run unattended locally |
| **P7** | Dispute exercise on a throwaway devnet: a doctored hash is caught, arbiters converge, every bond returns to its pre-dispute value | Not started |
| **P8** | Spec amendments, runnable harness | Spec and harness done; a GoToSocial CI stand-in is open |
| **P9** | Outbound federation and identity linking | Deferred |

## Contents

| Thing | Phase | What it does |
|---|---|---|
| `determinism_check.sh` | P2 | Prove ap-canonical hash stability (v1 or v2, `HASH_RULE`) off-chain before any tx: repeated fetches, second vantage point (optional SSH), edit classes, instance upgrade. Run logs + captured AS2 fixtures land in `runs/<timestamp>/`; promote the passing run to `accepted/`. |
| `governance_p32.sh` | P3.2 | The one gov proposal carrying both full-replacement messages (federation windows/quorum + bridge unbonding), built from current chain state so no field is zeroed. |
| `peer_setup_p33.sh` | P3.3 | Local-chain peer setup: register + activate the peer, inbound policy, bond the bridge operator and the verifier. Idempotent. |
| `rehearsal.sh` | P4 | The manual on-chain happy path against a real status: hash → submit → verify → wait out the challenge window → confirm VERIFIED + bond released (the live proof of P0.1) → one moderation. |
| `runs/` | P2 | Scratch output of every `determinism_check.sh` run (gitignored). Reruns and aborted runs stay here. |
| `accepted/` | P2 | The run that satisfies P2, promoted out of `runs/` by hand. **Commit this**: the acceptance is a captured log (its `run.log` is re-included past the global `*.log` ignore), and P1's golden tests can reuse the fixtures. |

The daemons live in the main tree, not here: `cmd/sdapbridge` (inbound
bridge, P5) and `cmd/sdapverify` (verifier runner, P6), sharing
`tools/apcanon` (the canonicalizer, P1) and `internal/sdaptx` (the LCD
tx client). Both sign either with the account's own key (`SDA_MNEMONIC`,
fine on your own machine) or through an x/session key
(`SDA_SESSION_KEY_FILE` + `SDA_GRANTER`, for any host you do not
control). See the daemon conventions in
[docs/x-federation-spec.md](../../../docs/x-federation-spec.md).

## Order of operations

1. **P0 binary + devnet reset (P3.0)** — the reset wipes state: replay
   any devnet state you rely on afterwards, and re-run
   `deploy/relayer/relink.sh`. P0.2's session allowlist entries only take
   effect at genesis.
2. **Instance (P3.1)** — see [INSTANCE_SETUP.md](INSTANCE_SETUP.md) for a
   concrete, working setup (GoToSocial first to smoke-test the toolchain,
   then real Mastodon for the run that counts). Self-host official
   docker-compose (Postgres + Redis + Ruby, ~2–4 GB RAM). **Set
   `AUTHORIZED_FETCH=false`** and record it: secure mode returns 401 to
   unsigned AS2 GETs and would drag HTTP-Signature signing into the
   rehearsal. GoToSocial is an acceptable CI stand-in, not the live run.
   A LOCAL instance needs `ALLOW_PRIVATE=1` / `--allow-private` to get
   past apcanon's SSRF guard — bring-up only, never a real peer.
3. **Governance (P3.2)** — one proposal, TWO messages (the windows are
   not OpsComm-editable): x/federation `MsgUpdateParams`
   (`verification_window`/`challenge_window` ≈ 24h, `arbiter_quorum` 2)
   plus x/service `MsgUpdateServiceTypeConfig` (short unbonding on
   `federation-bridge-activitypub`). Both are FULL-REPLACEMENT — write
   every field or the rest zero out. `content_ttl` is OpsComm-editable
   via `MsgUpdateOperationalParams` (also full-replacement; three fields
   reject zero).
4. **Peer setup (P3.3)** — OpsComm member: `register-peer <domain>
   "Mastodon Test" '{}' ""` (four positionals; the empty fourth is the
   IBC channel), committee proposal → `resume-peer`, then
   `update-peer-policy` with `inbound_content_types
   =["blog_post","blog_reply"]` as a `--policy` JSON flag (every
   field), then `register-bridge <domain> activitypub <endpoint>
   1000000000` (**one billion**, not 1000000000000). Size the policy's
   `inbound_rate_limit_per_epoch` to the follow set
   (`INBOUND_RATE_LIMIT` in `peer_setup_p33.sh`; the formula is in its
   header). Verifier:
   `tx rep bond-role federation-verifier <≥500 DREAM>` from an
   ESTABLISHED persona.
5. **P2 determinism run** — `determinism_check.sh --phase baseline
   <uris>` for several hours; then `--phase edits`, then `--phase
   upgrade` after a minor-version bump. If (1) or (2) fails, STOP.
6. **P4 rehearsal** — `rehearsal.sh` with keyring names for the
   operator / verifier / OpsComm member and the status URI (the AS2 id,
   `/ap/users/<id>/statuses/<id>` on Mastodon 4.7+).
7. **Daemons (P5/P6)** — bridge on the operator key, verifier on its
   own key, ideally its own host. See each `main.go` header and
   `loadConfig` for env vars; a local instance also needs
   `SDA_ALLOW_PRIVATE_HOSTS=1` on both. `SDA_PEER_IDS` takes a
   comma-separated list (one peer per source instance, all bound to the
   operator). **Consent (`SDA_CONSENT`, default `opt-in`):** only authors
   who follow the bridge account are anchored, and only posts published
   after the bridge first saw that follow; `#nobridge`/`#nobot` in a
   profile always refuses. For a local test, have the test account follow
   the bridge account first. `indexable` is the looser mode; `none` is for
   test instances only. **Public domain only:** a post is anchored only
   when its text carries `#cc0` (claimed as `CC0-1.0`) or `#publicdomain`
   (`PDM-1.0`), whatever the consent mode; the chain refuses anything else
   (code 2389) and the verifier refuses a record whose post lacks the tag.
   Test statuses need the hashtag too, and the bridge account's bio should
   say so (see [docs/content-license.md](../../../docs/content-license.md)).
   `apcanon license <uri>` shows what a status declares. **Hash rule (`SDA_HASH_RULE`, default
   `ap-canonical-v2`):** v2 also hashes every attachment's file, so both
   daemons download media (up to 128 MiB per file); the verifier checks
   each record under the rule it names, and also refuses records whose
   displayed text, links or author do not match the post. A file over
   128 MiB is recorded as `oversize`, described but not linked on-chain.
   Downloads get `SDA_MEDIA_TIMEOUT` (default 2m) plus the declared size
   at `SDA_MEDIA_MIN_RATE` (default 524288 bytes/s); raise the one or
   lower the other if large files on a slow link keep timing out. The bridge finds edits with a
   revisit sweep (`SDA_REVISIT`, `SDA_REVISIT_WINDOW`), not discovery.
   **Replies need the reconcile sweep** (`SDA_RECONCILE`, default 15m):
   Mastodon's home feed drops a reply unless the reader follows the
   account being replied to, so home-timeline discovery never sees a
   followed author's reply to anyone else. The sweep reads each followed
   account's posts directly. The bridge account does not need periodic
   web sign-ins: token use refreshes its sign-in at most daily, and a
   lapsed account's feed is rebuilt on its next request.
8. **P7 dispute exercise** — throwaway devnet only, gated on P0.1:
   operator submits a doctored hash, verifier catches it, arbiters
   converge (quorum 2 from P3.2, or the anonymous x/shield path),
   content → REJECTED, and every bond returns to its pre-dispute value.

## Notes

- **Local chain run (2026-09-23).** P3.2 → P3.3 → P4 → daemons all ran
  against a local `testparams` chain: peer `localhost` (peer ids reject
  ports), operator1 for the rehearsal, a separate operator for the bridge
  daemon, and bob (ESTABLISHED at genesis) as the verifier — invited
  accounts start at trust level 0 and cannot bond. Post → anchored ~10 s
  → verified ~7 s; edit → re-anchored with `supersedes_content_id` ~2.5
  min later (the AS2 cache), verified seconds after.

- Each source instance is its own `peer_id` (register + proposal +
  policy), but additional bindings share one operator's 1000 SPARK bond.
- The identified arbiter path costs 3 arbiters + 1 submitter = 4000
  SPARK in bonds (one bond per ADDRESS, not per binding).
- The daemons sign with raw keys in v1; switch to x/session keys scoped
  to the P0.2-allowlisted messages after the reset.

## The launcher's Mastodon image

[image_smoke_test.sh](image_smoke_test.sh) runs [Dockerfile-mastodon](../../../deploy/docker/Dockerfile-mastodon) the way the chain launcher deploys it: postgres, redis, web + sidekiq and upstream streaming as separate containers, behind a proxy that forwards plain HTTP with `X-Forwarded-Proto: http` like an Akash ingress fronted by Cloudflare. It checks that the schema prepares itself, that there is no `force_ssl` redirect loop, that the AS2 actor has an https id, that the bootstrap commands (owner, registrations, bridge token, wallet sign-in chains) are idempotent, and that the root-owned media volume is taken over. It also covers the Mastodon half of wallet sign-in against a stub membership endpoint: the chain-list route, handles taken from the x/name rather than the address uid, and the membership sweep. It needs docker, curl, jq and openssl; `MASTODON_IMAGE=<image>` tests a published image instead of building one.

### Wallet sign-in

Members of a linked chain sign in to the launcher's Mastodon with Keplr through `cmd/sdaplogin`, an OpenID Connect provider shipped in the sdap image. The launcher's DESIGN.md (Mastodon, "Wallet sign-in") describes the deployment.

[wallet_login_e2e.sh](wallet_login_e2e.sh) runs the real round trip between the Mastodon image and the sdap image, wired the way the launcher renders them, behind a TLS edge with a throwaway CA. The chain's LCD is an nginx serving files. [walletsign](walletsign/) stands in for Keplr, producing the ADR-036 `signArbitrary` result for a test seed. The script checks:
- Mastodon's request phase discovers the provider;
- a non-member is refused;
- a member's signature ends in a signed-in Mastodon session, with the account named from the x/name, confirmed, and with registrations still closed;
- the hourly sweep disables the member once they leave the chain.

It needs docker, curl, jq, openssl, python3 and go; `MASTODON_IMAGE` and `SDAP_IMAGE` skip the builds. The provider's own logic (signature checks, trust floor, codes, PKCE, tokens) is unit-tested in `go test ./cmd/sdaplogin`, against a sign-doc vector made with cosmjs.
