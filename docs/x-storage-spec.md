# Technical Specification: `x/storage`

## 1. Abstract

The `x/storage` module lets a member pay to have a **permanent copy** of an off-chain file made on a storage network where nothing can be deleted (Arweave). The chain never holds the file. It records the request, holds the fee, and tracks the request until a bonded operator says where the copy is.

It exists because permanent copies are useful to every content module (blog, forum, collect, artifact) and to files that belong to none of them, so the request should not live in any one of them.

Key principles:

- **The file is named by its hash.** A request carries the file's sha256 (written as 64 lowercase hex characters everywhere in this module), its size and a public address to fetch it from. Everything after that is checked against the hash, so a copy can only ever be the requested file.
- **Two off-chain roles, both `x/service` operators.** *Storage operators* make the copies. *Content scanners* say what must not be copied. Bond, reports and slashing come from `x/service`.
- **Both roles are hired, not self-appointed.** Registering in `x/service` is open to anyone with the bond and names a controller without asking it. An operator or a scanner counts in this module only after its controller council, or governance, has approved it (§3.5, §3.6).
- **Nothing an operator says is trusted.** A completion names where the copy is. Anyone can fetch it and check the hash.
- **Scanned before it is stored.** A file of a scanned type is copied only after a clean verdict from approved scanners ([content-scanning.md](content-scanning.md)).
- **No operator is obliged to store a file.** A request nobody takes expires and is refunded.
- **The operator gains nothing from a completion.** The fee goes to the council that funds the storage. That council does gain, so it is never the only party that can stop a bad completion (§3.5).
- **Rare cases go to governance, not to more mechanism.** The module has one path for each ordinary outcome. Anything unusual is a council or governance decision (§3.9).

**Scope.** Hosting (paid by the period, removable) was designed with this module and set aside. Backends keep a `kind` field so it can be added later.

Terms. "Storage fee" already means the per-byte charge for onchain bytes in blog and forum. This module's charges are the **copy fee** and the **request fee**. A **permanent copy** is an off-chain copy that cannot be deleted.

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/bank` | Fee escrow, refunds, payout, burning |
| `x/service` | Operator status, controller and service-type lookup for both roles; system reports |
| `x/rep` | Membership, trust level, the alias contract for migrated members |
| `x/identity` | The bond denom every fee is charged in |
| `x/commons` | Whether an address is a council (payouts, approvals); the Operations Committee check for prices |
| `x/session` | Operators and scanners sign with a session key |
| `x/distribution` | Community pool, the fallback destination for a payout or refund that cannot be delivered |

**Depended on by:** none. Content modules do not call it; clients look a copy up by hash (§9). `x/bank` and `x/auth` are depinject inputs; every other keeper above is late-wired in `app.go` (`Set*Keeper`).

---

## 3. Core Concepts

### 3.1. Backends

A governance-managed registry, one entry per storage network:

```
Backend {
  name                    // "arweave"; [a-z0-9-]{1,32}; immutable
  service_type            // x/service type whose operators serve it; immutable
  kind                    // BACKEND_KIND_PERMANENT, the only kind defined; immutable
  locator_format          // LOCATOR_FORMAT_ARWEAVE_TX; immutable
  enabled                 // false = no new requests; open ones run to their end
  price_per_byte          // bond-denom micro-units per byte
  min_fee                 // floor for the copy fee
  min_price_per_byte, max_price_per_byte   // governance bounds for price_per_byte
  min_min_fee, max_min_fee                 // governance bounds for min_fee
  declines_to_close       // how many different members must decline before a request closes (§3.7)
  types[]                 // { content_type, scanned, max_size }
}
```

`locator_format` is checked by code: an Arweave transaction id is 43 base64url characters. A data item inside a bundle (ANS-104) has the same shape and is accepted; what the operator waits for differs (§10).

`price_per_byte` and `min_fee` are set by the Commons Operations Committee inside the governance bounds (§4.2). The floors matter as much as the ceilings: without them the committee could set both to zero and have the funding council pay for every copy.

**Content types.** A type not listed is refused. The launch list is `image/png` and `image/jpeg`, both `scanned`, each with a `max_size` of 10 MiB, the most the reference operator worker posts in one Arweave transaction. Listing a type with `scanned = false` is an explicit governance decision to store it unchecked. A client converts any other image to one of the two before it computes the hash (§9).

Launch backend: `arweave`, service type `storage-arweave`.

### 3.2. Requests

```
Request {
  id
  requester               // pays, receives any refund, may cancel while pending
  requester_trust_level   // recorded at creation; fixes the scanner quorum (§3.5)
  backend, sha256, size_bytes, content_type
  scanned                 // the type's flag, recorded at creation
  sources[]               // { url, added_by, added_at }
  copy_fee                // the amount this request holds in escrow; 0 once paid out, refunded or burned
  status, closed_reason
  created_at
  opened_at               // start of the current attempt: creation, or the last void or reopen (§3.5)
  deadline                // height after which a PENDING request expires
  operator, controller    // set by a claim (§3.5)
  claimed_at, claim_expires_at, claim_extended
  required_attestations   // set by a claim: the scanner quorum this completion needs
  locator, attestations[], verdicts_locator, completed_at   // set by a completion
  settle_at, disputed
  paid_to, paid_amount    // set at settlement
  closed_at               // height the request reached a terminal status; fixes its pruning time
  forfeit_at              // set while a flagged request's fee is held (§3.6)
  declined_by[]           // { operator, member, reason }
  barred[]                // operators that may not claim: declined, voided, or let a claim lapse
}
```

| Status | Meaning | `copy_fee` |
|---|---|---|
| `PENDING` | waiting for an operator | in escrow |
| `CLAIMED` | one operator is working on it | in escrow |
| `COMPLETED` | operator posted a locator; inside the settle window | in escrow |
| `SETTLED` | the copy stands | paid out |
| `REJECTED` | terminal; `closed_reason` is `FLAGGED`, `DECLINED` or `VOIDED` | refunded, except a request flagged after a claim: held, then burned (§3.6) |
| `EXPIRED` | terminal: nobody stored it by the deadline | refunded |
| `CANCELLED` | terminal: the requester withdrew it | refunded |

**Address rules** (each source `url`): scheme `https`, a host, no user or password part, no fragment, only RFC 3986 characters (no spaces, quotes, angle brackets or control characters), at most `max_url_bytes`. It is untrusted text: clients never render it as a link and fetch it only under the scanner display policy. It is public and permanent in transaction history; clients say so, and warn before sending an address with a query string.

**One request per `(backend, sha256)`.** A second request for a file whose request is `PENDING`, `CLAIMED`, `COMPLETED` or `SETTLED` is refused (`ErrAlreadyRequested`, `ErrAlreadyStored`). One copy serves everyone who links the file.

**Sources.** The requester's address is the first source. Any member who could have made the request (§3.3) may add another with `MsgAddSource` while the request is `PENDING` or `CLAIMED`, paying the request fee; one per member, at most `max_sources_per_request`. The hash decides, so an added source can never change what is copied. Sources exist because of the one-request rule: without them anyone who knows a file's hash could request it first with a dead address and block the real requester. Added sources are dropped at `SETTLED`: the copy is found by hash, and a settled request is kept for good, so nobody can pin text to someone else's record.

### 3.3. Who may request

An active member at `min_requester_trust_level` or above (default `TRUST_LEVEL_PROVISIONAL`, as the media gate in blog and forum). The trust level is recorded on the request, so a later promotion or demotion never changes the quorum an operator had to wait for.

**Migrated members.** Nothing here is re-keyed. The stored `requester` is read through the alias contract: cancel and the free dispute check `CanActAs(signer, requester)`, the per-requester and per-source limits count the canonical member, and `RequestsByRequester` reads `AllAddressesOf`. A refund to a tombstoned address is redirected by the bank send restriction.

### 3.4. Fees

Both are in the chain's bond denom, read from `x/identity`:

- **Request fee** (`request_fee`): burned at once, never refunded. It prices the scanning and fetching every request causes. Adding a source and a non-requester's dispute cost the same.
- **Copy fee**: `max(min_fee, size × price_per_byte)`, escrowed and fixed at creation.

The message carries `max_fee`; if the total at execution exceeds it the request fails.

**Payout.** At `SETTLED` the copy fee goes to the request's `controller`: the council recorded at the claim, which hired the operator and funds its storage wallet. A payout or refund that cannot be delivered goes to the community pool and emits `payout_redirected`. A send never fails a block.

### 3.5. Claims, completion and settlement

**Approved operators.** Each backend keeps a list, at most `max_operators_per_backend`:

- `MsgApproveOperator { authority, backend, operator }` is signed by the operator's controller, as `x/service` records it at that moment, or by governance. The operator must be a live operator of the backend's `service_type`. The approval records the controller.
- `MsgRemoveOperator` is signed by the controller recorded on the approval, or by governance.
- An approval counts only while `x/service` still names the recorded controller for that operator. After a controller transfer the new controller approves again. An approval is per address.

**Claim.** `MsgClaimRequest` takes a `PENDING` request for `claim_window_blocks`. It exists so two operators never both pay the storage network for the same file.

- The operator is approved, `ACTIVE`, an active `x/rep` member, not on the request's `barred` list (the check reads its earlier addresses, `RecentAddressesOf`), under `max_open_claims_per_operator` and under the lapse limit.
- **No self-dealing.** The operator is not the same member as the requester (`SameMember`). This stops the cheap case of one account requesting and completing; two member accounts are stopped by the invitation gate, not here.
- Once `barred` holds `max_barred_per_request` operators nobody may claim; the request waits out its deadline.
- **The claim fixes the quorum.** For a `scanned` request it records `required_attestations`: the `content-scanner` type's `attestation_quorum`, or its `elevated_attestation_quorum` when that is non-zero and `requester_trust_level` is `NEW` or `PROVISIONAL`. A quorum of 0 means "this type does not attest" in `x/service`; here it refuses the claim (`ErrNoScannerQuorum`), and so does having fewer approved `ACTIVE` scanners than the quorum. A new request of a scanned type is refused on the same two conditions.
- **The claim fixes the controller**, from the approval. That council is paid at settlement and may void.
- **One extension.** `MsgExtendClaim`, before the claim expires, adds one `claim_window_blocks`, once per claim.
- **A claim pauses the deadline; it never moves it.** Only `PENDING` requests expire. A request that returns to `PENDING` from a lapse or a decline keeps its deadline and expires at once if it has passed.
- **Lapses.** If the window passes, the EndBlocker returns the request to `PENDING`, bars the operator and counts a lapse. More than `max_lapses_per_window` lapses stops the operator from claiming until the window rolls, and files one system report through `x/service`.

**An operator that stops being one.** Status is read from `x/service` at each step; the module registers no service hooks.

- `UNDERFUNDED` or `UNBONDING`: may complete what it claimed, not claim. Its bond is still held.
- No live record (retired or slashed; `x/service` archives both): can neither claim nor complete. Its claims lapse without a lapse count.
- **At settlement**, a completion whose operator has no live record and has a slashed archive record (`HasSlashedRecord`) is voided instead of settled, as a governance void would (below). A partial slash leaves the operator live, and its other completions settle unless someone disputes or voids them.
- **The bond outlasts the completion.** Two rules together keep the bond held while a completion can still be voided.
  - The `storage-arweave` unbonding period must exceed `2 × claim_window_blocks + 2 × (settle_window_blocks + dispute_window_blocks) + flag_hold_blocks` (§4.2). An operator that starts to unbond can no longer claim, so what it needs is the time to finish a claim it holds (the claim and its one extension), then settle and dispute. The rest is margin.
  - A lift restores a completion only while its operator is `ACTIVE` or `UNDERFUNDED` (§3.6). No window bounds how long flags hold a request: every later flag restarts the hold, and a reopened completion can be flagged again. So the length of the hold is not what protects the bond; the check at the lift is.

**Completion.** `MsgCompleteRequest { operator, request_id, locator, attestations[], verdicts_locator }`, by the claimant while its status is `ACTIVE`, `UNDERFUNDED` or `UNBONDING` and the claim has not expired, at most `max_completions_per_operator_per_window` per window. The chain checks the locator's format. It cannot check the copy.

For a `scanned` request the completion lists the hash of each signed clean verdict relied on (`contentscan.AttestationHash`: the sha256 of the signed attestation's canonical JSON, signature included; distinct, hex, at least `required_attestations`, at most `max_attestations_per_completion`) and a `verdicts_locator`: a second transaction on the storage network holding those signed verdicts as a JSON array. Both must be empty for a request that is not scanned. The chain stores them and verifies nothing else. A scanned completion is **sound** when:

- the `verdicts_locator` serves signed verdicts whose sha256 hashes are exactly the listed `attestations`;
- each is a **clean** verdict whose subject is this request: `chain_id` is this chain's, `record` is `storage/request/<id>`, `sha256` is the file's, and `scanned_height` is at or after the request's `opened_at`;
- each is from a different scanner, and at one same height `h` from `claimed_at` to `completed_at` every one of them was an `ACTIVE` `content-scanner` operator, approved here, and the signature verifies under the `verdict_key` in that scanner's `x/service` metadata at `h`;
- no verdict is past its `expires_at` at the completion's block time.

A completion that fails any of these is evidence for a report against the operator (`MsgReportOperator` in `x/service`). `opened_at` moves on every void and reopen, so verdicts from an earlier attempt never count again.

**Settlement.** A completion sets `settle_at = now + settle_window_blocks`. At that height the EndBlocker moves the request to `SETTLED`, pays the controller, and drops the added sources.

**Dispute.** `MsgDisputeCompletion`, once per completion while `COMPLETED` and before `settle_at`, moves `settle_at` to `now + dispute_window_blocks`. The requester disputes for nothing; any other eligible member pays the request fee. A dispute decides nothing: it gives governance time to void when the controller will not. `disputed` is cleared whenever a request enters `COMPLETED`, so every completion, including one reopened after a flag, can be disputed once.

**Voiding.** `MsgVoidCompletion { authority, request_id, reason }` is the remedy when the copy is missing or wrong. It is refused while the hash is flagged.

| Request is | Who | Effect |
|---|---|---|
| `COMPLETED` | the request's `controller`, or governance | back to `PENDING`, fee still in escrow |
| `SETTLED` | the council in `paid_to` | that council returns `paid_amount` to escrow in the same message; back to `PENDING` |
| `SETTLED` | governance | closed as `REJECTED` / `VOIDED`; the hash is free to be requested again; `settled_copy_voided` records `paid_to` and `paid_amount`, which that council owes the requester |

A void that returns a request to `PENDING` sets a fresh deadline and `opened_at`, clears `barred` and `declined_by`, bars the voided operator, and clears every claim and completion field. The `completion_voided` event carries the voided operator, locator, attestations and verdicts locator as evidence.

### 3.6. Scanning and flags

**Operators wait for a clean verdict.** For a scanned request an operator copies only when the combined verdict of the **approved** scanners for `storage/request/<id>` is clean at the quorum the claim will record. Unapproved scanners are ignored entirely, for held and removed as well as clean: otherwise a stranger with the scanner bond could stall every request with one `held`.

| Combined verdict | What happens |
|---|---|
| clean | operators may claim and copy |
| unchecked or held | operators wait; the request expires and is refunded if never cleared |
| removed | the scanner flags the hash (below) |

**Approved scanners.** One list for every backend, at most `max_approved_scanners`. `MsgApproveScanner` and `MsgRemoveScanner` follow the operator rules of §3.5, with the `content-scanner` service type. Only an approved, `ACTIVE` scanner may flag, and only an approved scanner's verdict counts toward a completion. Approving a scanner is where the council judges that it is independent of the others.

**Flags.** `MsgFlagRemoved { scanner, sha256, attestation_hash }` records the hash in `RemovedHashes` and reaches the one live or settled request each backend holds for it (`LiveByHash`), so its cost depends on the number of backends, never on how many requests the hash has had.

- `PENDING` → `REJECTED` / `FLAGGED`, refunded.
- `CLAIMED` or `COMPLETED` → `REJECTED` / `FLAGGED`; the fee is neither refunded nor paid. It is held until `forfeit_at = now + flag_hold_blocks` and then burned. Refunding would give a free copy to anyone whose file is flagged after the upload; paying the council would let a council with a scanner and an operator complete falsely, flag and keep the fee.
- `SETTLED` stays as a record; clients never fetch it.
- New requests for the hash are refused, and queries withhold its addresses and locator (§11).

One scanner's flag is enough: refusing wrongly is the safe failure. Each scanner flags a hash once, at most `max_flags_per_scanner_per_window` per window; a hash holds at most `max_flags_per_hash` flags. A later flag restarts the hold on a held fee.

**Lifting.** `MsgUnflagRemoved { authority, hashes[] }` (up to `max_hashes_per_unflag`; all or nothing): a council removes the flags of scanners it controls in `x/service`; governance removes all. When the last flag on a hash is removed, the hash leaves `RemovedHashes` and each held request is **reopened** with `opened_at = now`:

- closed while `CLAIMED` → `PENDING` with a fresh deadline; the claim is cleared and the former claimant is not barred;
- closed while `COMPLETED`, operator `ACTIVE` or `UNDERFUNDED` → `COMPLETED` with `settle_at = now + settle_window_blocks` and `disputed` cleared;
- closed while `COMPLETED`, operator `UNBONDING` or without a live record → `PENDING`, as if it had only been claimed. Its bond could be released before the restored completion can be voided (§3.5), so the completion is dropped and another operator makes the copy. The fee stays in escrow.

A fee already burned is gone. `flag_hold_blocks` is the time there is to lift a wrong flag, so it must be longer than a governance vote takes. A governance proposal that both lifts a flag and voids the reopened completion carries the two messages together.

### 3.7. Declines

`MsgDeclineRequest { operator, request_id, reason }` says this operator cannot store the file. The operator is approved, `ACTIVE`, an active member and not barred; the request is `PENDING`, or `CLAIMED` by this operator.

| Reason | Meaning |
|---|---|
| `DECLINE_REASON_UNREACHABLE` | no source served bytes with the declared hash. May be sent only after `min_unreachable_blocks` from the newest source |
| `DECLINE_REASON_MISMATCH` | the operator holds bytes with the declared hash, and they are not the declared size or type, or fail the file profile (§3.8) |

A mismatch is a fact about the file, so anyone holding the file can check it; a false one is reportable. Bytes with another hash prove nothing about the file and count as unreachable.

A decline releases the operator's claim without a lapse and bars it. The request closes (`REJECTED` / `DECLINED`, refunded in full) when `declines_to_close` different **members** have declined: each decline stores the operator's canonical member, and a decliner whose earlier address is already on the list counts once. An operator has one decline per request and separate per-window caps for the two reasons.

### 3.8. The file profile

A scanned type is stored only in a narrow profile, checked by one function, `contentscan.FileProfile(content_type, bytes)` (the rule itself is the standard-library-only package `tools/fileprofile`, which the chain binary's CLI links too), that clients, scanners and operators all call and that is pinned by test vectors. A client re-encodes every image into the profile before hashing (§9), so the profile can be strict.

| Type | Accepted | Refused |
|---|---|---|
| `image/png` | signature; `IHDR` with bit depth 8, colour type 0, 2 or 3, no interlace; `PLTE` for colour type 3 only; `sRGB`; consecutive `IDAT` whose zlib stream ends exactly at the end of the last one and inflates to exactly the bytes the header needs; `IEND`; valid CRCs | alpha (colour types 4 and 6, `tRNS`); 16-bit; interlace; `gAMA`, `cHRM`, `iCCP`; text, time and EXIF chunks; animation chunks; any other chunk; bytes after `IEND` |
| `image/jpeg` | `SOI`; one optional 16-byte JFIF `APP0` with no thumbnail; `DQT`, `DHT`, `DRI`; one `SOF0` or `SOF2` frame with 8-bit samples and 1 or 3 components; its scans; `EOI`; each segment starting where the last ended | every other `APPn` (EXIF, XMP, ICC, Adobe); JFXX; `COM`; arithmetic, lossless and hierarchical frames; 4 components; a second frame; bytes after `EOI` |

What the profile guarantees: the file is one still image with no metadata, no colour management and no transparency, so the pixels a scanner's model sees are the pixels a viewer sees. It does not make hidden data impossible. Pixel values are free data, and so are bytes inside a JPEG's entropy-coded data after the last block and tables no scan uses. Only the per-type `max_size` bounds those.

Metadata is refused for a second reason: a camera writes a location there, and a permanent copy would publish it for good.

### 3.9. What is left to governance

These cases have no mechanism of their own. Each is rare, and each earlier attempt to automate one added more attack surface than it removed.

- **A wrong `size` or `content_type`.** The request is declined as a mismatch and closes, or expires; the file can then be requested correctly. The declarer loses the request fee.
- **A settled copy that is not there, when the paid council will not void.** Governance voids; the request closes and the event records what the council owes.
- **A council that dismisses reports against its own operator.** `x/service` lets the controller resolve a report. The remedy is a controller-transfer case, which goes to a jury, or a governance void and removal of the operator's approval.
- **An upload that outlives its claim and its extension.** The claim lapses and the requester may cancel. The council bore one upload.

### 3.10. Privacy

A request ties the requester's address to a file hash, in public and for good. Clients say so before a member pays and never offer it for anonymous content. No message of this module is shield-compatible (guard test, §7).

---

## 4. State

### 4.1. Stores

| Store | Key → Value | Notes |
|---|---|---|
| `Params` | | |
| `Backends` | `name → Backend` | |
| `ApprovedOperators` | `(backend, operator) → {controller, height}` | |
| `ApprovedScanners` | `scanner → {controller, height}` | |
| `Requests` | `id → Request` | |
| `RequestSeq` | `→ uint64` | first id is 1 |
| `LiveByHash` | `(backend, sha256) → id` | `PENDING`, `CLAIMED`, `COMPLETED`, `SETTLED` |
| `RemovedHashes` | `sha256 → {flags[]: {scanner, attestation_hash, height}, held[]: ids}` | never pruned |
| `ByStatus` | `(backend, status, id)` | |
| `ByRequester` | `(requester, id)` | |
| `OpenByRequester` | `(requester, id)` | `PENDING`, `CLAIMED`, `COMPLETED`; backs the per-requester cap |
| `ByOperator` | `(operator, id)` | `CLAIMED` and `COMPLETED` |
| `ClaimsByOperator` | `(operator, id)` | `CLAIMED`; backs the open-claims cap |
| `Deadlines` | `(height, id, kind)` | expiry, claim expiry, settlement, forfeit, pruning |
| `Counters` | `(address, kind) → {window_start, count}` | lapses, the two declines, completions, flags |

Terminal requests other than `SETTLED` are pruned `terminal_retention_blocks` after `closed_at`.

### 4.2. Params

Governance (`MsgUpdateParams`). The windows are long on a network and short in the test build (`params_vals_*.go`).

| Param | Default | Meaning |
|---|---|---|
| `enabled` | true | false refuses new requests and nothing else |
| `min_requester_trust_level` | PROVISIONAL | §3.3 |
| `request_fee` | 1 SPARK | burned per request, added source and non-requester dispute |
| `request_ttl_blocks` | ~7 days | deadline, set at creation, by a void and by a reopen to `PENDING` |
| `claim_window_blocks` | ~2 hours | |
| `settle_window_blocks` | ~1 day | |
| `dispute_window_blocks` | ~14 days | longer than a governance vote |
| `flag_hold_blocks` | ~14 days | longer than a governance vote |
| `min_unreachable_blocks` | ~1 hour | |
| `terminal_retention_blocks` | ~30 days | |
| `counter_window_blocks` | ~1 day | |
| `deadline_retry_blocks` | ~1 hour | |
| `max_sources_per_request` | 4 | |
| `max_attestations_per_completion` | 8 | |
| `max_operators_per_backend` | 16 | |
| `max_approved_scanners` | 16 | |
| `max_open_claims_per_operator` | 50 | |
| `max_lapses_per_window` | 10 | |
| `max_mismatch_declines_per_operator_per_window` | 20 | |
| `max_unreachable_declines_per_operator_per_window` | 20 | |
| `max_completions_per_operator_per_window` | 100 | |
| `max_flags_per_scanner_per_window` | 50 | |
| `max_flags_per_hash` | 8 | |
| `max_hashes_per_unflag` | 50 | |
| `max_url_bytes` | 512 | |
| `max_reason_bytes` | 256 | |
| `max_barred_per_request` | 32 | |
| `max_open_requests_per_requester` | 20 | counts `PENDING`, `CLAIMED`, `COMPLETED`; checked at creation |
| `max_deadlines_per_block` | 200 | |

`Params.Validate` refuses a window or count of zero (except `request_fee`), `claim_window_blocks >= request_ttl_blocks`, `min_unreachable_blocks >= request_ttl_blocks`, `dispute_window_blocks < settle_window_blocks`, and `terminal_retention_blocks <= flag_hold_blocks`. The `MsgUpdateParams` and `MsgUpdateBackend` handlers and `InitGenesis` also refuse, reading other state:

- `2 × claim_window_blocks + 2 × (settle_window_blocks + dispute_window_blocks) + flag_hold_blocks` at or above the unbonding period of any backend's service type;
- `max_operators_per_backend` or `max_approved_scanners` below what is already approved;
- a backend whose `declines_to_close` is 0 or above `max_barred_per_request`.

`MsgUpdateBackend { authority, backend }` (governance) creates the backend when the name is new and otherwise replaces its mutable fields; a change to `name`, `service_type`, `kind` or `locator_format` is refused. Its own validation: floors not above ceilings, prices inside them, no type listed twice, `max_size > 0`, the service type exists. A backend is never deleted, only disabled.

`MsgUpdateOperationalParams { authority, backend, price_per_byte, min_fee }` (Commons Operations Committee or governance) writes those two fields inside the governance bounds.

---

## 5. Messages

All signer messages carry `option (amino.name) = "sparkdream/x/storage/Msg<Name>"`. Protos are append-only and every enum has an `UNSPECIFIED = 0` value that messages refuse.

**Members** (not session-delegable; they move the member's funds or hold a council's):

- `MsgRequestPermanentCopy { requester, backend, sha256, size_bytes, content_type, url, max_fee }` → `request_id`.
- `MsgAddSource { member, request_id, url, max_fee }`.
- `MsgCancelRequest { requester, request_id }`. Only while `PENDING`.
- `MsgDisputeCompletion { member, request_id, max_fee }`.

**Storage operators** (session-delegable; none moves the operator's funds): `MsgClaimRequest`, `MsgExtendClaim`, `MsgCompleteRequest`, `MsgDeclineRequest`.

**Scanners** (session-delegable): `MsgFlagRemoved`.

**Councils and governance** (not session-delegable): `MsgVoidCompletion`, `MsgUnflagRemoved`, `MsgApproveOperator`, `MsgRemoveOperator`, `MsgApproveScanner`, `MsgRemoveScanner`, `MsgUpdateBackend`, `MsgUpdateParams`, `MsgUpdateOperationalParams`. A council signs through a proposal, so the message must be in its `AllowedMessages` (§7).

### 5.1. Transition table

Every change of status, with the money it moves and the fields it resets. "Claim fields" are `operator`, `controller`, `claimed_at`, `claim_expires_at`, `claim_extended`, `required_attestations`. "Completion fields" are `locator`, `attestations`, `verdicts_locator`, `completed_at`, `settle_at`, `disputed`.

| From | Trigger | To | Money | Fields |
|---|---|---|---|---|
| — | `MsgRequestPermanentCopy` | `PENDING` | request fee burned; copy fee escrowed | `opened_at = created_at = now`; `deadline = now + ttl` |
| `PENDING` | `MsgClaimRequest` | `CLAIMED` | | claim fields set |
| `PENDING` | deadline (EndBlocker) | `EXPIRED` | refund | `closed_at` |
| `PENDING` | `MsgCancelRequest` | `CANCELLED` | refund | `closed_at` |
| `PENDING` | decline reaching the quorum | `REJECTED` / `DECLINED` | refund | `closed_at` |
| `PENDING` | `MsgFlagRemoved` | `REJECTED` / `FLAGGED` | refund | `closed_at` |
| `CLAIMED` | `MsgCompleteRequest` | `COMPLETED` | | completion fields set; `disputed = false` |
| `CLAIMED` | claim expiry (EndBlocker) | `PENDING`, or `EXPIRED` if past the deadline | refund if expired | claim fields cleared; operator barred |
| `CLAIMED` | claimant's decline | `PENDING`, or `REJECTED` / `DECLINED` at the quorum, or `EXPIRED` if past the deadline | refund if closed | claim fields cleared; operator barred |
| `CLAIMED` | `MsgFlagRemoved` | `REJECTED` / `FLAGGED` | held | `closed_at`, `forfeit_at`; id on the hash's `held` list |
| `COMPLETED` | `settle_at` (EndBlocker) | `SETTLED` | paid to `controller` | `paid_to`, `paid_amount`; `copy_fee = 0`; added sources dropped |
| `COMPLETED` | `settle_at`, operator slashed | `PENDING` | | as a void |
| `COMPLETED` | `MsgDisputeCompletion` | `COMPLETED` | request fee burned unless the requester | `settle_at = now + dispute window`; `disputed = true` |
| `COMPLETED` | `MsgVoidCompletion` | `PENDING` | | claim and completion fields cleared; `barred`, `declined_by` cleared; operator barred; `opened_at = now`; fresh deadline |
| `COMPLETED` | `MsgFlagRemoved` | `REJECTED` / `FLAGGED` | held | `closed_at`, `forfeit_at`; `held` list |
| `SETTLED` | void by `paid_to` | `PENDING` | `paid_amount` returned to escrow | as a void; `paid_to`, `paid_amount` cleared |
| `SETTLED` | void by governance | `REJECTED` / `VOIDED` | none | `closed_at` |
| `REJECTED` / `FLAGGED`, held | last flag lifted, was `CLAIMED` | `PENDING` | still in escrow | claim fields cleared; `closed_at`, `forfeit_at` cleared; `opened_at = now`; fresh deadline |
| `REJECTED` / `FLAGGED`, held | last flag lifted, was `COMPLETED`, operator `ACTIVE` or `UNDERFUNDED` | `COMPLETED` | still in escrow | `settle_at = now + settle window`; `disputed = false`; `closed_at`, `forfeit_at` cleared; `opened_at = now` |
| `REJECTED` / `FLAGGED`, held | last flag lifted, was `COMPLETED`, operator `UNBONDING` or not live | `PENDING` | still in escrow | claim and completion fields cleared; `closed_at`, `forfeit_at` cleared; `opened_at = now`; fresh deadline |
| `REJECTED` / `FLAGGED`, held | `forfeit_at` (EndBlocker) | same | fee burned | `copy_fee = 0`; `forfeit_at` cleared; off the `held` list |
| `REJECTED` / `FLAGGED`, held | another scanner's flag | same | | `forfeit_at = now + hold`; pruning moves with it |
| terminal, not `SETTLED` | `closed_at + retention` (EndBlocker) | deleted | | |

Whether a held request was `CLAIMED` or `COMPLETED` is read from `completed_at`. Every status change updates `ByStatus`, `LiveByHash`, `ByOperator` and the request's `Deadlines` entries, so a request has exactly one entry per pending timer.

---

## 6. EndBlocker

One pass over `Deadlines` up to the current height, in key order, at most `max_deadlines_per_block` entries. Each entry runs in its own cached context and never fails the block; one that fails emits `deadline_failed` and moves to `now + deadline_retry_blocks`. It is never dropped.

**Messages go by height, not by whether the EndBlocker has reached an entry.** A completion, an extension or a claimant's decline is refused once `claim_expires_at` has passed; a claim or an added source once the `deadline` has passed; a dispute at or after `settle_at`. A flag treats a request still `COMPLETED` past `settle_at` as `COMPLETED`.

The module's EndBlocker runs after `x/service`'s, so a slash applied in a block is seen by that block's settlements. It has no BeginBlocker.

---

## 7. Genesis, module account and upgrade

**Genesis.** A chain whose genesis file predates the module has no storage state at all; the EndBlocker and the invariants treat that as nothing to do. Otherwise it is exported and imported in full: params, backends, approvals, requests, `RemovedHashes`, `Counters`, `RequestSeq`. The indexes and `Deadlines` are rebuilt from the requests (`deadline`, `claim_expires_at`, `settle_at`, `forfeit_at`, `closed_at`). `Validate` checks ids against `RequestSeq`, one live request per `(backend, sha256)`, that no open (`PENDING`, `CLAIMED`, `COMPLETED`) request's hash is flagged (a settled copy's hash may be), that every `held` id names a `FLAGGED` request with a `forfeit_at`, and every cap. `InitGenesis` checks that the module balance covers the escrow total ("at least", never "equal").

**Module account** `storage`, with the `Burner` permission, on the bank blocked list.

**The upgrade that introduces the module** folds into the release's one entry in `upgrades.All`: the store key, default params and the `arweave` backend (the module's `InitGenesis`, which a new module runs at the upgrade), plus these changes to stored state elsewhere, each a module migration on a bumped `ConsensusVersion`:

- `x/service`: the `storage-arweave` `ServiceTypeConfig` (also added to the genesis seed), and `"storage"` in the compiled `allowedSystemCallers`.
- `x/session`: the operator and scanner messages join `allowed_msg_types` and the compiled ceiling; every other message of the module joins `ForbiddenAllowlistMsgs`.
- `x/commons`: `MsgApproveOperator`, `MsgRemoveOperator`, `MsgApproveScanner`, `MsgRemoveScanner`, `MsgVoidCompletion` and `MsgUnflagRemoved` join the `AllowedMessages` of the Commons Council and the Commons Operations Committee, the bodies that already act as controllers of `x/service` operators, and their standard sets. Other bodies do not receive them: granting them is the decision about which bodies may hire operators and scanners. `MsgUpdateOperationalParams` joins the Commons Operations Committee's list.

No operator or scanner is approved by the handler.

**Guard tests:** every message has a recorded session decision; none is shield-compatible; every signer message has an amino name.

---

## 8. Integration points

### 8.1. x/service

The seeded `storage-arweave` config: `min_bond_amount` 1000 SPARK, `unbonding_period_blocks` ~60 days (§3.5), `report_timeout_action = ESCALATE`, `challenge_default_slash_bps` 100, everything else the defaults.

**Checked against the code.** Each line names what this spec relies on and where it is.

| Claim | Where |
|---|---|
| Registration needs only a bond and any group policy as controller; the group is not asked | `keeper/msg_server_register_operator.go` |
| `IsActiveOperator(addr, type)`: live, `ACTIVE`, bond at or above the minimum | `keeper/public_api.go` |
| `GetOperator` returns only live records; retired and slashed operators move to an archive | `keeper/state.go` (`ArchiveOperator`) |
| `HasSlashedRecord(addr, type)` reads the archive | `keeper/state.go` |
| A slashed address can never register for that type again | `keeper/public_api.go` (`RegisterOperator`) |
| The controller changes only through a jury-decided transfer case | `keeper/msg_server_finalize_controller_transfer.go` |
| `MsgClaimUnbondedBond` is refused while a `PENDING` or `ESCALATED` report is open | `keeper/reports.go` (`HasOpenReports`) |
| The controller resolves a report at tier 1; the reporter's deposit is refunded then, and only the operator can contest | `keeper/msg_server_resolve_report.go`, `msg_server_contest_slash.go` |
| `OpenSystemReport` accepts only module accounts in `allowedSystemCallers` | `keeper/public_api_system_report.go` |
| `attestation_quorum` 0 means the type does not attest; `elevated_attestation_quorum` 0 means the base quorum applies | `proto/.../service_type_config.proto` |
| A scanner's verdict key is in its operator metadata | [content-scanning.md](content-scanning.md) §8.1 |

Operator **metadata** is JSON: `{"v":1,"backend":"arweave","wallets":["<arweave address>", …]}`.

### 8.2. x/commons, x/rep

A council is the operator's controller and funds it with a recurring spend; copy fees return to it at settlement. `min_requester_trust_level` is checked through the rep keeper. Operators must be active members. The module registers no `MemberMigrationHooks`.

### 8.3. Content scanners

The reference worker (`cmd/contentscan`, `CS_STORAGE`) does the following; it polls the request lists, not the events: follow `storage.request_created`, `source_added`, `completion_voided` and `request_reopened`; scan each scanned request as the subject `storage/request/<id>`, trying each source until one serves bytes with the requested hash; apply `FileProfile` and sign nothing for a file that fails it; sign a fresh verdict for every storage subject and after every void or reopen; when signing **removed** for a hash, send `MsgFlagRemoved`. The worker keeps the flag as owed, in its state file, until it reads its own flag back from `RemovedHash`: an accepted broadcast is not a recorded flag. A flag not on chain is sent again every 10 minutes (the chain may refuse it for a while, for example at the per-window cap), and is given up only when no request names the hash any more.

---

## 9. Clients

- **Offer:** read `Backends` and `QuoteFee`. Say that the request and its address are public and permanent.
- **Prepare the file:** re-encode the image into the profile (§3.8): strip metadata, flatten transparency onto a background, convert to sRGB, take the first frame of an animation, and say what was changed. Check with `FileProfile` before hashing.
- **Already requested:** offer `MsgAddSource` instead of a second request.
- **After a completion:** fetch the locator for the member's own requests; if it does not serve the file, offer `MsgDisputeCompletion` before `settle_at`.
- **Find a copy:** `RequestByHash(backend, sha256)`. Fetch the locator through any gateway and show it only if the bytes hash to the sha256 and the scanner display policy allows. The chain record is a pointer, never proof.

## 10. Storage operator (reference worker)

The reference worker is [cmd/storageworker](../cmd/storageworker/main.go) in this repository. An operator runs it on a machine of their own with an Arweave wallet file they fund themselves and, for the chain, a session key (x-session-spec.md §12.8), so that neither the operator's account key nor more AR than the wallet holds is on that machine. It posts ordinary Arweave transactions signed by that wallet through any gateway ([tools/arweave](../tools/arweave/tx.go), no bundler and no third-party account). `storageworker wallet <file>` creates a wallet file or prints its address and balance.

Per pass: page `PENDING` requests; fetch each file under the scanner worker's safety rules, reading each source up to the type's `max_size`; on bytes with the right hash, check size, type and `FileProfile`; skip flagged hashes; read the approved scanners' feeds and combine their verdicts for this request (`contentscan.StorageVerdict`: same chain, this request's record, this hash, scanned at or after `opened_at`); decline, wait or claim; post the file and the verdicts transaction; complete.

- It claims only what it can finish: a file larger than one transaction carries (10 MiB) is left to another operator.
- It completes only after both transactions have `STW_MIN_CONFIRMATIONS` (default 10) and the gateway serves bytes with the right hash, and only while the combined verdict is still clean and the hash is not flagged.
- It extends a claim when the transactions are posted but not confirmed near the claim's end.
- It tags the copy `SHA-256: <hex>`, `Content-Type` and `App-Name: SparkDream`, and the verdicts transaction also `Type: verdicts` and `Request-Id: <id>`. The verdicts transaction holds the signed verdicts as a JSON array; the completion's attestation hashes are their `AttestationHash`, so anyone can check one against the other.
- It keeps the signed transactions (without the file's bytes) in a state file. After a restart, or when the network loses a transaction, it sends the same transaction again under the same id and never pays for a second upload. A note is dropped only when the chain shows that the attempt is over in a way a node behind the claim cannot show: a later `opened_at`, a completed or closed request, or this operator on the `barred` list. A request that merely looks unclaimed, or missing, may come from a node that has not caught up, and the note is kept.
- A request whose sources serve nothing is left to expire and refund; `STW_DECLINE_UNREACHABLE=true` declines it instead after `min_unreachable_blocks`.
- Every request it leaves alone is logged once with its reason.

## 11. Queries

`Params`, `Backend`, `Backends`, `ApprovedOperators(backend)`, `ApprovedScanners`, `OperatorStanding(backend, operator)`, `Request`, `RequestByHash(backend, sha256)`, `RequestsByStatus(backend, status)`, `RequestsByRequester`, `RequestsByOperator`, `RemovedHash(sha256)`, `QuoteFee(backend, content_type, size_bytes)`, `RequestLocation(id)`. List queries are paginated.

Every query that returns a request returns its source addresses and locators as `""` while the hash is flagged. A single-request response then sets `withheld`, and a list response names those requests in `withheld_ids`; the stored request carries no such field. `RequestLocation(id)` returns them regardless, for scanners and reviewers. The withholding is a default for clients, not secrecy.

## 12. Events

`storage.operator_approved`, `operator_removed`, `scanner_approved`, `scanner_removed`, `backend_updated`, `operational_params.updated`, `request_created`, `source_added`, `request_claimed`, `claim_extended`, `claim_lapsed`, `request_declined`, `request_completed`, `completion_disputed`, `completion_voided`, `settled_copy_voided`, `request_settled`, `request_reopened`, `fee_forfeited`, `request_rejected`, `request_expired`, `request_cancelled`, `hash_flagged`, `hash_unflagged`, `payout_redirected`, `deadline_failed`. Each request event carries `request_id`, `backend` and `sha256`.

## 13. Invariants

- The module's bond-denom balance is at least the sum of `copy_fee` over all requests.
- `copy_fee` is zero unless the request is `PENDING`, `CLAIMED`, `COMPLETED`, or `REJECTED` / `FLAGGED` with a `forfeit_at`.
- `LiveByHash` holds exactly the `PENDING`, `CLAIMED`, `COMPLETED` and `SETTLED` requests.
- No `PENDING`, `CLAIMED` or `COMPLETED` request's hash is in `RemovedHashes`.
- Every `held` id names a `FLAGGED` request for that hash with a `forfeit_at`, and every such request is on a `held` list.
- Every request has exactly the `Deadlines` entries its status calls for.
- Every list is inside its cap.

They are evaluated by `go test ./app -run TestInvariants`.

## 14. Testing

| What | Where |
|---|---|
| Every message, every row of the transition table, and the abuse cases found in review | `x/storage/keeper/*_test.go` |
| Random interleavings of every message across many blocks, with the invariants and coin conservation checked after every step | `x/storage/keeper/interleaving_test.go` |
| Genesis round trip with a request in every status; the EndBlocker under a backlog; a failed timer retried | `x/storage/keeper/lifecycle_test.go` |
| Params, backend, address, hash and locator validation; the session decision of every message | `x/storage/types/*_test.go` |
| No message is shield-compatible | `x/storage/keeper/query_test.go` |
| File profile vectors | `tools/fileprofile/fileprofile_test.go` |
| Migrations in the modules the upgrade touches | `x/service`, `x/session`, `x/commons` `keeper/migrations_test.go` |
| Full-app simulation: real transactions, council messages through executed proposals, the invariants on the final state | `x/storage/simulation`, `app/sim_test.go` |
| The reference worker: a stored request from claim to completion, every reason to wait or decline, restart, lost transactions and a node that is behind without a second payment | `cmd/storageworker/worker_test.go` |
| Arweave transactions: signing and data roots checked against real transactions | `tools/arweave/tx_test.go` (`live_test.go` reads a gateway when `ARWEAVE_LIVE_TX_IDS` is set) |
| The verdict rule for a request and feed reading | `tools/contentscan/storage_test.go` |
| End to end, with the short test-build windows | `test/storage/` |
| The reference worker against a running chain: real queries, real messages through a session key, a stand-in Arweave gateway | `test/storage/worker_test.sh` (runs `cmd/storageworker/live_test.go`) |

A CLI request starts from a local file with `sparkdreamd tx storage request-file-copy`, which computes the hash and size and refuses a file outside the profile before anything is paid.

## 15. Launching with one party

The founder can hold both roles: a member address registered as the `storage-arweave` operator, a `content-scanner` on another address, each approved by its controller council, and `declines_to_close = 1`. The operator and the requester must be different members (§3.5).

What stays real with one party: hash checks by every client, scanning before copying, fee accounting, the dispute, and the per-window caps. What is nominal: the decline quorum, the independence of scanner and operator, the verdict quorum, and slashing, since reports are resolved by councils the founder sits on. Raise `declines_to_close` to 2 when a second independent operator is approved.

## 16. Open questions

1. **The request fee's size.** 1 SPARK is a placeholder.
2. **A file with no public address.** The UI's upload bucket provides one for images today.
3. **A dispute nobody answers** settles to the council. Should a jury decide instead of governance?
4. **More types.** Each needs a profile row, vectors and scanner support.

## 17. File references

- [x-service-spec.md](x-service-spec.md), [content-scanning.md](content-scanning.md), [x-commons-spec.md](x-commons-spec.md), [x-rep-spec.md](x-rep-spec.md), [x-session-spec.md](x-session-spec.md), [x-identity-spec.md](x-identity-spec.md), [chain-upgrades.md](chain-upgrades.md), [development-conventions.md](development-conventions.md)
