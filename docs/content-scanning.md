# Content Scanning: Media Labels, Scanner Verdicts and Operator Rewards

Status: **chain side of every phase implemented (§14 maps the code); reference worker in `cmd/contentscan`; client integration lives in sparkdream-ui.** Supersedes the "content gateway" proxy design that sparkdream-ui 1.0.82 integrated against (see [Migration from the gateway contract](#10-migration-from-the-gateway-contract)).

## 1. Problem

Spark Dream content is public and permanent. Anyone can run a frontend that reads it, and some of what gets posted will be abusive: illegal media, spam, material a frontend operator cannot legally serve. Three facts shape the design:

- **Filtering cannot be enforced.** Independent operators read the chain directly. Any filter binds only the frontends that opt in, so the design serves willing operators and does not pretend otherwise. The only moderation that binds everyone is the onchain kind (`*_STATUS_HIDDEN`).
- **Media is where the risk is, and some of it is onchain.** Bodies can carry inline `data:` URIs or compressed blobs, and they arrive with the record in every query response, before any client-side check can run. Off-chain references (IPFS, Arweave, Filecoin, Jackal, collection URIs) are safer only because the client controls the fetch.
- **Hash lists are not enough.** Generative models can produce unlimited novel abuse material, which no list of known hashes will match. First detection needs a classifier.

## 2. Overview

```
            ┌──────────────────────────── chain ────────────────────────────┐
  author ──►│ Msg handler labels media (no decompression, no URL parsing)   │
            │ queries return metadata; flagged bodies only via body query   │
            │ x/service: content-scanner operators, bonds, checkpoints, pay │
            └──────────────┬────────────────────────────────▲───────────────┘
                           │ reads blocks                    │ checkpoint (height, root)
                           ▼                                 │
               scanner workers (independent, any host) ──────┘
                           │ publish signed verdict lists
                           ▼
               static hosting / mirrors / anyone's merged file
                           │
                           ▼
          client: show plain text at once; fetch media, flagged bodies
          and links only after the combined verdict says clean
```

Four parts:

1. **Chain media labels** (§3). The chain labels every content record at write time and withholds flagged bodies from list and show queries.
2. **Scanner workers and the verdict feed** (§4, §5). Workers follow the chain, check flagged content and external URIs, and publish signed verdicts. No request from a reader ever goes through a scanner.
3. **Client behavior** (§6). Clients combine the verdicts of trusted workers and fetch only what has cleared.
4. **Operators and rewards** (§8). Workers are `x/service` operators: bonded, paid through commons, slashable.

## 3. Chain: media labels

### 3.1. Rules

The chain labels content from information it already has, plus one cheap, deterministic scan of uncompressed text. It **never decompresses** `GZIP`/`ZSTD` bodies and **never parses or fetches external links**. Those are the scanner's job.

New in `common/v1`:

```proto
// MediaFlag bits, OR-ed into a record's media_flags.
enum MediaFlag {
  MEDIA_FLAG_NONE         = 0;
  MEDIA_FLAG_INLINE_DATA  = 1; // a text body contains a data URI (§3.2)
  MEDIA_FLAG_COMPRESSED   = 2; // content_type is GZIP or ZSTD; never inspected
  MEDIA_FLAG_OFFCHAIN_REF = 4; // content_type is IPFS / ARWEAVE / FILECOIN / JACKAL
  MEDIA_FLAG_EXTERNAL_URI = 8; // a structured URI field is set (collect items, collections)
}
```

| Record | Labelled from | Flags |
|---|---|---|
| blog `Post`, blog `Reply`, forum `Post` with `TEXT` / `MARKDOWN` / `HTML` | data URI scan of `body` / `content` (§3.2) | `INLINE_DATA` or none |
| same records with `GZIP` / `ZSTD` | `content_type` only | `COMPRESSED` |
| same records with `IPFS` / `ARWEAVE` / `FILECOIN` / `JACKAL` | `content_type` only | `OFFCHAIN_REF` |
| collect `Item` | `image_uri` set, `reference_type` is `LINK` or `NFT` | `EXTERNAL_URI` |
| collect `Collection` | `cover_uri` set | `EXTERNAL_URI` |
| federation `FederatedContent` | `body` scanned as text; `content_uri` set | `INLINE_DATA` / `EXTERNAL_URI` |

URLs inside text bodies (for example `<img src="https://...">` in HTML) are **not** labelled. The chain does not look for them. Clients treat every external URL in a rendered body as unchecked until it has a verdict (§6).

Collect records never carry inline bytes: a data URI in any collect string field is **rejected** at message validation (§3.3), so collect labels come from which fields are set, never from a scan.

### 3.2. Data URI rule

A data URI has the shape `data:[<mediatype>][;base64],<data>` (RFC 2397). A plain substring search for `data:` would also match ordinary prose ("metadata:", "Data: 5 records") and hold back harmless posts, so rules version 1 matches the URI shape instead:

```
(?i)(^|[^a-z0-9+.-])data:[^\s,]{0,256},
```

`data:` must not follow a URI-scheme character (letter, digit, `+`, `.`, `-`), and must reach a comma with no whitespace in between. That matches every data URI, including `data:,hello` and `data:image/png;base64,…`, and none of the prose examples. Any media type is flagged, not only `image/` and its relatives. The cost is the same, and a text post flagged by mistake only waits for the scanner. A missed image is the worse failure.

The same rule is used for rejection in collect fields (§3.3).

### 3.3. New fields

Each labelled record gets:

```proto
uint32 media_flags         = N;   // OR of MediaFlag bits
uint32 media_rules_version = N+1; // labelling rules that produced media_flags; 1 for this spec
bytes  body_hash           = N+2; // sha256 of the body exactly as stored (post-encoding, pre-decompression)
```

`body_hash` lets a scanner key its verdict by content rather than by record, and lets a client verify that the body it fetches is the one that was scanned. `FederatedContent` already carries `content_hash`; it gains only the two label fields.

Labels are recomputed on every edit (`MsgUpdatePost` and equivalents) and on inbound federation receive. They are never set by the author.

Collect messages that create or update items and collections reject a data URI (§3.2) in `image_uri`, `cover_uri`, `link.uri`, `nft.token_uri`, `custom.value`, `custom.extra` values and `attributes` values. Those fields point at things elsewhere or hold short text; nothing legitimate embeds bytes there, and collect records are not withheld (§3.4), so rejection is the only way to keep inline bytes out of them. The chain still does not look for links in these fields. Clients render attribute and custom values as plain text, and treat any value they turn into a link as an unchecked URL.

### 3.4. Withholding flagged bodies

List and show queries keep their signatures, but return the body as `""` whenever `media_flags != 0`. A new body query per module returns the full body for one record:

| Module | Queries that withhold | New query |
|---|---|---|
| blog | `ShowPost`, `ListPost`, `ListPostsByCreator`, `ListPostsByTag`, `ShowReply`, `ListReplies`, `ListExpiringContent` | `PostBody(id)`, `ReplyBody(id)` |
| forum | `GetPost`, `AllPost`, `Posts`, `Thread`, `UserPosts` | `PostContent(id)` |
| federation | queries returning `FederatedContent` | `FederatedContentBody(id)` |

Plain-text content, which will be most content, behaves exactly as today. Only flagged content costs the client a second request, made after the verdict check. Collect records are not withheld: they carry URI strings, never media bytes (§3.3).

### 3.5. Cost and determinism

The data URI rule is a single linear pass over a body that is already size-limited (`max_body_length` in blog, `max_content_size` in forum), so its gas is bounded. Implement it as a hand-written scanner or with Go's `regexp` (RE2, linear time, no backtracking); either gives the same result on every node. `body_hash` is one sha256 per write.

### 3.6. Migration

Every module's `InitGenesis` recomputes labels for the records it imports instead of trusting the file, so a network relaunched from a genesis exported by an older binary comes up labelled under rules version 1 with `body_hash` filled. It needs no decompression or network access. If live in-place upgrades are ever introduced, the same recomputation runs as a store iteration in the upgrade handler. Existing collect records that already hold a data URI cannot be rejected after the fact: relabelling gives them `MEDIA_FLAG_INLINE_DATA`, and clients do not render the affected fields.

## 4. Verdicts

### 4.1. Attestation format

Each worker signs every verdict it issues:

```jsonc
{
  "v": 1,
  "worker": "sprkdrm1...",            // x/service operator address
  "subject": {
    "chain_id": "sparkdream-1",
    "record": "blog/post/123",        // module/type/id; absent for bare-hash subjects
    "sha256": "…",                    // body_hash, or sha256 of the exact bytes fetched
    "cid": "bafy…",                   // for off-chain references
    "uri": "https://…",               // for external URIs
    "pdq": "…"                        // perceptual hash for images, when computed
  },
  "verdict": "clean | held | removed",
  "category": "csam | illegal | sexual | violence | spam | test | none",
  "ruleset": {
    "version": "2026.10.1",
    "models": { "image-safety": "sha256:…", "age-estimate": "sha256:…" },
    "thresholds": { "sexual": 0.8, "minor": 0.6 }
  },
  "scanned_height": 1234567,
  "issued_at": "2026-10-09T12:00:00Z",
  "expires_at": "2026-11-08T12:00:00Z", // required for plain-URI subjects (content can change)
  "sig": "…"                             // ed25519, over the canonical JSON of everything above
}
```

**Canonical JSON** (what `sig` covers): the attestation without `sig`, object keys sorted lexicographically, no insignificant whitespace, no HTML escaping, numbers as written, timestamps RFC 3339 UTC. Optional subject fields and `expires_at` are omitted when empty. The test vector in `tools/contentscan/contentscan_test.go` (`TestSigningBytesAreCanonical`) pins the exact bytes; every client implementation must reproduce it.

Verdicts are signed with a dedicated **ed25519 verdict key**, never the operator's wallet key. On decentralized compute the hosting provider can read the worker's memory, so the key a worker holds must not be able to move funds. ed25519 is fast and deterministic and has libraries everywhere clients run.

Including the hash of the exact bytes checked is essential. If a host serves different files to different workers, their hashes differ, so the trick can never be counted as agreement.

### 4.2. Verdict states

- **clean**: checked and nothing found. Clients may show and fetch it.
- **held**: a classifier or rule scored above threshold. Hidden until reviewed, but not removed. A false positive costs a delay, not a takedown.
- **removed**: confirmed by hash match, by review, or by a mandatory category.

### 4.3. Categories

- **Mandatory** (`csam`, `illegal`): an operator applying this system is expected to honour them unconditionally.
- **Advisory** (`sexual`, `violence`, `spam`): each operator chooses whether to hide, blur or ignore.
- **`test`**: canary items (§8.4). Never shown to readers either way.

### 4.4. Combining verdicts

Only verdicts from workers that are currently `ACTIVE` `content-scanner` operators count (§8.1). For a given subject:

1. **removed** if any trusted worker says removed.
2. otherwise **held** if any trusted worker says held.
3. otherwise **clean** if at least `Q` independent trusted workers say clean **for the same hash**, where `Q` is the service type's `attestation_quorum` (§8.1).
4. otherwise **unchecked**.

The rule is asymmetric on purpose. A false "removed" only hides content, which is the safe failure. A false "clean" can put abuse in front of readers, so it needs agreement from several independent workers, and a single bad worker or provider cannot cause it.

A client's effective scanned height is the lowest `scanned_height` among the `Q` most advanced trusted workers. Flagged content above that height is unchecked.

When workers disagree (removed against clean), removed wins and the conflict is logged for council review (§7).

## 5. Scanner workers

### 5.1. What a worker does

1. Follows blocks and indexes every record with `media_flags != 0`, plus external URLs found in rendered text bodies.
2. Fetches the material: flagged bodies through the body queries, off-chain references through storage gateways, external URIs over HTTP.
3. Decompresses, decodes and hashes it in a sandbox.
4. Runs detection (§5.2) and signs a verdict.
5. Appends the verdict to its feed, and periodically posts a checkpoint onchain (§8.2).

A worker reads only public data and serves no reader traffic. It can run on a personal machine, a server or decentralized compute (Akash). Where it runs is a deployment choice; what it publishes and how its verdicts combine is protocol.

### 5.2. Detection, layered

No commercial detection services. Open tools only:

| Layer | Tool | Catches | Output |
|---|---|---|---|
| Exact hash | sha256, CID | resubmission of anything already removed | removed |
| Perceptual hash | Meta's PDQ (images), TMK+PDQF (video), open source | resized or recompressed copies of removed material, on this chain or peers | removed |
| Known-abuse hash lists | NCMEC / IWF lists, if membership is obtained later | known material on first appearance | removed |
| Classifier | pinned open-weight image safety models (e.g. ShieldGemma 2, Llama Guard vision variants) | novel material, including AI-generated | held, or advisory category |
| CSAM heuristic | sexual-content score combined with an age estimate | likely CSAM | held, category `csam` |
| Text | open toxicity / spam classifiers | abusive text | advisory only |

The takedown hash list is built from the system's own removed verdicts and published with the feed. Publishing hashes, never media, is standard practice.

Classifiers are probabilistic, so a verdict carries the exact model hashes and thresholds that produced it, and workers run inference reproducibly (CPU, fixed precision). Two workers scoring the same bytes with the same ruleset then agree, which the quorum rule needs. Classifier output alone never yields **removed**: at most **held**, pending review.

Without access to known-abuse hash lists, known material is caught only after someone reports it. That gap is accepted for now and closes if a membership is obtained. The list's terms will decide whether it may sit on decentralized workers or only on workers the council controls.

### 5.3. Worker safety

- **No media at rest.** Fetched files are hashed and classified in memory and never written to disk. Possessing illegal material is the central legal risk of running a worker.
- **Egress through a VPN or proxy**, and refuse private and link-local address ranges, so a posted URL cannot reveal the worker's IP or reach its local network (SSRF).
- **Sandbox all decoding.** Image, video and archive parsers run on attacker-chosen input.
- **Limits** on fetch size, decompressed size and time.

### 5.4. Feed layout

```
<feed-root>/manifest.json        // worker, latest segment, scanned_height, merkle root, signing key
<feed-root>/segments/000001.jsonl // append-only signed attestations
<feed-root>/takedown-hashes.json  // sha256 + PDQ hashes of removed subjects
```

Feeds are static files on any hosting and can be mirrored freely. Anyone may publish a **merged** file, the concatenation of several workers' attestations. The merger needs no trust, because clients verify each signature themselves. Lightweight clients fetch one merged file instead of one feed per worker.

## 6. Clients

1. **Trusted workers** are read from the chain: `OperatorsByServiceType("content-scanner")`, `ACTIVE` only, signing keys from operator metadata.
2. **Verdicts** come from a merged file or the workers' own feeds, signatures checked, combined per §4.4.
3. **Display policy:**

| Content | Before a clean verdict | Held | Removed |
|---|---|---|---|
| plain text (`media_flags == 0`) | shown immediately (fail open) | placeholder | "Not available on this site" |
| flagged body | "Being checked" placeholder; body not fetched | placeholder | "Not available on this site" |
| off-chain reference, image URI | never fetched | never fetched | never fetched |
| link | URL text shown, not clickable | not clickable | not shown |
| external URL inside a rendered body | not loaded | not loaded | not loaded |

4. **Verify what is fetched.** A body fetched through a body query must match `body_hash`. A link with `content_hash` must match it. On mismatch, treat as unchecked.
5. **Freshness.** If the newest verdict data is older than an operator-configured limit, flagged content stays unchecked. Text keeps working, so a stale feed degrades the site but never takes it down.

Encrypted collections (`encrypted_data`) cannot be scanned: only members can decrypt them. Their contents are the members' responsibility, and clients render decrypted media under the same hold-until-checked policy where the client controls the fetch.

## 7. Human review

- Categories other than `csam` go through the existing flag, sentinel and jury flows.
- **Likely CSAM is never shown to people.** Not to sentinels, not to randomly drawn jurors: that harms them and is illegal in most places. For this category, one trusted worker's removal stands alone, and any review uses only metadata (account, scores, hashes, the workers' signed verdicts).
- Some jurisdictions oblige platforms to report suspected CSAM. A classifier hit may trigger that for operators subject to it.

## 8. Operators and rewards

Scanning is real, ongoing work with real costs, and the quorum rule needs several independent workers to keep running. The chain pays for it, building on `x/service`, which exists for "off-chain agents performing work the chain cannot natively verify".

### 8.1. Registration

| Need | Mechanism |
|---|---|
| Register a worker | `x/service` operator with `service_type = "content-scanner"`, SPARK bond per its `ServiceTypeConfig` |
| Trusted keys | operator `metadata`: ed25519 verdict public key (§4.1), feed URL, ruleset version and model hashes. Trust follows operator status: an operator that is slashed, unbonding or retired stops counting. No separate key registry. |
| Quorum `Q` | generic `ServiceTypeConfig.attestation_quorum` (uint32, x/gov), meaning "how many operators must agree" for any service whose operators attest to something; 0 means the type does not attest. `elevated_attestation_quorum` (0 = same as `Q`, else ≥ `Q`) is the bar for media from low-trust authors (§9). Clients read both from the chain, so every frontend sees the same baseline; a frontend may require more. |
| Metadata format | JSON `{"v":1,"verdict_key":"<base64 ed25519>","feed_url":"…","ruleset_version":"…","models":{…}}` (`contentscan.OperatorMetadata`; `contentscan metadata` prints it). |
| Hiring | the operator's `controller`, a commons council |
| Pay | `MsgScheduleRecurringSpend` from commons to the operator |
| Penalties | `x/service` two-tier slashing: council for routine lapses, jury for serious ones |

### 8.2. Checkpoints (new)

The chain cannot see feeds, so each worker periodically submits:

```proto
message MsgSubmitCheckpoint {
  string operator     = 1;
  string service_type = 2;
  int64  height       = 3; // scanned up to
  bytes  root         = 4; // merkle root of the operator's feed up to this point
}
```

This belongs in `x/service` as a generic, service-agnostic message: other operator types can use it too. One cheap transaction:

- makes liveness measurable onchain, so `x/service` system reports can fire automatically when a worker falls behind,
- makes the feed tamper-evident: a worker cannot rewrite past verdicts,
- gives performance pay something to measure.

Rules: the signer is the operator (ACTIVE or UNDERFUNDED under `service_type`), or a session key acting for it — `MsgSubmitCheckpoint` is in the default x/session allowlist, so the worker host never holds the bond key. `height` must exceed the operator's previous checkpoint and must not exceed the current block; `root` is 32 bytes. Each submission replaces the previous `Checkpoint` record; `Checkpoint` and `CheckpointsByServiceType` serve them, and they survive genesis export.

**Feed root.** `root` is the RFC 6962 merkle tree hash over the feed's attestations in order, each leaf `sha256(0x00 ‖ canonical JSON including sig)`, interior nodes `sha256(0x01 ‖ left ‖ right)`, empty feed `sha256("")` (`contentscan.MerkleRoot`). Anyone holding the feed recomputes the root over its first N entries and compares it to a past checkpoint; a mismatch proves the feed was rewritten.

**Liveness.** `ServiceTypeConfig.checkpoint_max_lag_blocks` (0 = off) is how far an operator's checkpoint height may trail the chain; it is measured from registration until the first checkpoint. When it is exceeded the x/service EndBlocker files a system report against the operator (reporter: the x/service module account; proposed slash: the type's `challenge_default_slash_bps`, which must be > 0 when liveness is on) and the controller council resolves it like any report. A worker that stays down is reported once per lag window, never once per block. Changing the lag reschedules every live operator of the type at once.

### 8.3. Evidence

Signed verdicts are self-incriminating. A worker that signed **clean** on a hash later confirmed as abusive has proven its own failure. A jury can decide from the signed verdicts, the hashes and the other workers' verdicts, without anyone viewing the content.

### 8.4. Performance pay

Paying per verdict would reward rubber-stamping "clean" cheaply. Instead:

- **Base pay for coverage:** keeping up with flagged content within a target delay, measured from checkpoints.
- **Accuracy multiplier:** upheld versus overturned verdicts on appeal, over a rolling window (the design proposed for sentinels in [sentinel-accuracy-window.md](sentinel-accuracy-window.md)).
- **Canary items:** the council posts harmless test content whose correct verdict (category `test`) is known in advance, for example images whose hashes are on a test list. A worker that misses them is provably not running its pipeline, a slashable lapse. Canaries are only ever harmless material.

How the pieces are computed (decided): the chain stores no verdicts, so the controller council computes the multiplier off-chain from the operator's signed feed, the checkpoints (coverage and delay) and appeal outcomes (§8.3 evidence), and applies it by sizing the operator's recurring spend for the next period (`MsgCancelRecurringSpend` + a new `MsgScheduleRecurringSpend`). The council distributes the canary hash list to workers (`CS_CANARY_LIST`); a worker can only match a canary by actually fetching and hashing the content, which is what the check proves. Missing a canary is filed as an ordinary `MsgReportOperator` with the canary's post and the worker's verdict (or its absence from a feed whose checkpoint covers that height) as evidence.

### 8.5. Funding

Commons recurring spend covers base pay. A **scanning fee** (`media_scan_fee` in blog and forum params, default 0) is charged on every write the chain labels as media: whoever adds scanning work pays for it, and media spam costs more. Plain text stays free.

Decided: the fee is **burned**, like the storage fee, rather than routed into a scanner pool. A pool would need its own claim path, and paying workers from per-item fees would recreate the per-verdict incentive §8.4 rejects. The fee prices the work; the council funds the workers.

### 8.6. Independence

The chain cannot verify that two workers run on different providers or belong to different people. One party running several workers to control the quorum is the residual risk. Bonds and member identity raise its cost; the hiring council must judge independence when it approves operators.

## 9. Trust and cost for media

An open classifier can be defeated: anyone can download the weights and tune generated images until they pass. Generation is cheap; getting past a bonded, invite-only identity system repeatedly is not. The rules, all governance parameters:

- **Higher quorum for low-trust media.** Media authored by `TRUST_LEVEL_NEW` or `TRUST_LEVEL_PROVISIONAL` members needs `elevated_attestation_quorum` clean verdicts (x/service, content-scanner type) instead of `attestation_quorum`. Clients look up the author's trust level and apply `contentscan.QuorumFor`.
- **Minimum trust or a bond to post media.** blog and forum each carry `media_min_trust_level` (default 1, PROVISIONAL) and `media_author_bond_min` (default 100 DREAM; 0 disables the bond path). A write the chain labels as media (`media_flags != 0`) is accepted only from an active member at that trust level, or, at creation, one who attaches an author bond of at least `media_author_bond_min`. Edits that introduce media have no bond path. Non-members cannot post media. Rejections are `ErrMediaNotPermitted` (blog 1232, forum 2500).
- **No anonymous media.** Shield-routed (anonymous) content can never carry media: it would combine the riskiest content with the least accountability, and no bond can attach to it.
- **A confirmed abuse verdict costs the author their bond and standing** through the existing author-bond slash and member-accountability flows (§7).

The gate is the same function in both modules (`commontypes.CheckMediaPermitted`); each module owns its params, because each already owns its posting gates and author bonds.

## 10. Migration from the gateway contract

sparkdream-ui 1.0.82 integrated against a filtering proxy: an `X-Content-Policy-Version` header, `REQUIRE_CONTENT_GATEWAY=true` to fail closed, `451` for removed records and `_moderation` stubs in lists. Under this design:

- the header check becomes the freshness check of §6,
- `451` and stubs become client-side results of combining verdicts (the UI's "Not available on this site" copy is kept),
- no proxy sits in the request path, and the UI's README section on the gateway is rewritten.

## 11. Rollout

| Phase | Scope | Status |
|---|---|---|
| 0 | sparkdream-ui stops auto-loading collection item images; click to load. | done |
| 1 | Chain: `MediaFlag`, label fields, `body_hash`, body queries, withholding, migration. sparkdreamjs regenerated (amino checklist in sparkdream-ui `CLAUDE.md` applies). | chain done; sparkdreamjs regeneration pending |
| 2 | `content-scanner` service type with `attestation_quorum = 1`; one worker; feed format; clients consume verdicts; recurring pay. With one worker the protection against a bad worker does not exist yet, so that worker should be one the council controls directly. | chain + reference worker done; UI consumption pending; registering the first worker and its recurring spend are council actions |
| 3 | `MsgSubmitCheckpoint`; several workers; `attestation_quorum` raised to 2 or more; merged feeds. | chain done (checkpoints, liveness reports); raising `Q` is a gov proposal once independent workers run |
| 4 | Accuracy pay, canaries, scanning fee, trust rules for media. | chain rules done (trust/bond gate, anonymous ban, scan fee, elevated quorum); accuracy pay and canaries are council procedures (§8.4) supported by the worker |

## 12. Threats

| Threat | Mitigation |
|---|---|
| Abuse media inline in a body | labelled `INLINE_DATA` / `COMPRESSED`, body withheld until clean |
| Host serves scanners and readers different files | verdicts keyed by hash of bytes fetched; client verifies hashes; plain-URI verdicts expire |
| Novel AI-generated material | classifier layer; held pending review |
| Adversarially tuned media | trust/bond rules for posting media; quorum for clean |
| Malicious or compromised worker or provider | quorum for clean; signed verdicts as evidence; slashing |
| One party controls the quorum | bonds, identity, council judgement (§8.6) |
| Worker's IP exposed, SSRF | proxied egress; private ranges refused |
| Parser exploits | sandboxed decoding |
| Stale feeds | freshness limit; text unaffected |
| Rubber-stamping "clean" | canaries; accuracy multiplier; coverage-based pay |

## 13. Decisions and open questions

Decided:

1. **Data URIs in collect fields are rejected** (§3.3), not labelled.
2. **Collect `custom` and `attributes` values** get the data URI check (rejection) but no link detection. Clients render them as plain text.
3. **`Q` is `ServiceTypeConfig.attestation_quorum`**, a generic x/service field set by governance; the bar for low-trust media is the sibling `elevated_attestation_quorum` (§9).
4. **Verdicts are signed with ed25519**, using a key separate from the operator wallet.
5. **Short profile and proposal text** (no `content_type`) stays unlabelled while clients render it as plain text.
6. **Trust rules for media are owned by each content module** (blog, forum params), sharing one gate function; the quorum bump lives with the quorum in x/service (§9).
7. **Anonymous content cannot carry media** (§9).
8. **The scanning fee is burned**, not pooled (§8.5).
9. **Liveness is enforced by x/service system reports** from a checkpoint-lag deadline queue (§8.2); accuracy pay and canary checks stay council procedures because verdicts live off-chain (§8.4).
10. **Migration relabels in `InitGenesis`** (§3.6).

Open:

1. **Known-abuse hash lists:** whether and how to obtain NCMEC or IWF membership once there is real media traffic, and whether its terms allow the list on decentralized workers. The worker already accepts such a list (`CS_TAKEDOWN_LIST`).
2. **Perceptual hashing:** no Go PDQ / TMK+PDQF implementation is vendored yet; until one is, resized copies of removed material are caught only by the classifier (§5.2).

## 14. Implementation map

| Piece | Where |
|---|---|
| Labelling rule, `MediaFlag`, media gate | [x/common/types/media_labels.go](../x/common/types/media_labels.go), `proto/sparkdream/common/v1/media_flag.proto` |
| Labels, withholding, body queries, media gate | `x/blog/keeper/media_labels.go`, `x/forum/keeper/media_labels.go`, `x/federation/keeper/media_labels.go`; `PostBody` / `ReplyBody`, `PostContent`, `FederatedContentBody` |
| Collect data-URI rejection and labels | `x/collect/keeper/media_labels.go` (`ErrDataURINotAllowed`, 1271) |
| `content-scanner` type, quorum, checkpoints, liveness | `x/service/types/genesis.go` (seed), `x/service/keeper/checkpoints.go`, `msg_server_submit_checkpoint.go`, `query_checkpoint*.go` |
| Verdict format, combine rule, feed, metadata, fetch guard, detection | [tools/contentscan](../tools/contentscan) |
| Reference worker | [cmd/contentscan](../cmd/contentscan) (shipped in the `sparkdreamnft/sdap` image: `docker run … sdap:<ver> contentscan`) |
| Verifier compatibility (withheld federated bodies) | `cmd/sdapverify` reads `federated_content_body` for labelled rows |

Running a worker: `contentscan keygen verdict.key`, then `contentscan metadata` (with `CS_FEED_URL`) for the operator metadata; register under `content-scanner` with a council as controller; grant the worker host an x/session key covering `MsgSubmitCheckpoint`; run `contentscan` with `CS_SESSION_KEY_FILE`/`CS_GRANTER`, a classifier (`CS_CLASSIFIER_URL`) and the council's canary list; publish `CS_FEED_DIR` on static hosting.
