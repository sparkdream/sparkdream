# P2 acceptance: Mastodon v4.7.2, 2026-09-23

Local instance per [INSTANCE_SETUP.md](../../INSTANCE_SETUP.md)
(`AUTHORIZED_FETCH=false`, http override, fetched with `ALLOW_PRIVATE=1`).

| Check | Result | Evidence |
|---|---|---|
| 1b. Stable across repeated fetches under `ap-canonical-v2` (media file digests) | PASS: 3 statuses (two with an image, one plain) x 8 fetches, 30 min apart, 2026-09-23T21:30-2026-09-24T00:52 UTC, zero drift; each image file downloaded and hashed every fetch | `v2-baseline/run.log`, `v2-baseline/baseline.txt` |
| 1. Stable across repeated fetches | PASS: 5 statuses (plain, CW, media, reply, edit target) x 8 fetches, 30 min apart, 13:52-17:11 UTC, zero drift | `baseline/run.log`, `baseline/baseline.txt` |
| 2. Stable across vantage points | Not run (single host) | |
| 3. Hash flips on every edit class | PASS: 7/7 | `edits/run.log` |
| 4. Survives an instance upgrade | Skipped by decision: the instance runs v4.7.2, the current stable release; revisit when a peer runs a different version | |

Edit classes, each checked against the hash taken just before that edit,
with the canonical fields that moved:

| Class | Fields moved |
|---|---|
| body | content, updated |
| CW add | summary, updated |
| CW remove | summary, updated |
| media add | attachment, updated |
| media alt-text | attachment, updated |
| media remove | attachment, updated |
| poll options | updated |

Poll options are not in the hashed set, so a poll edit is caught only
through `updated`.

Mastodon caches the AS2 body for 3 minutes, keyed without `updated_at`,
so an edit is invisible over AS2 for up to ~180 s. `edits/edits_driver.sh`
therefore waits for AS2 `updated` to match the API's `edited_at` before
hashing. It was run from a scratch directory: adjust the paths and supply an
API token to rerun it.

`*/fixtures/*.as2.json` are the raw AS2 objects captured at baseline, for
P1's golden tests.
