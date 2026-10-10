# Technical Specification: `x/artifact` (NFTs)

Status: **implemented** in [x/artifact](../x/artifact/) (unit, simulation and e2e coverage in `x/artifact/...` and [test/artifact/](../test/artifact/)).

## 1. Abstract

The `x/artifact` module lets accounts on Spark Dream mint, hold, transfer, trade and burn non-fungible tokens (NFTs). An NFT is a unique, ownable on-chain record, `<class_id>/<token_id>`, that points at media and carries a small amount of on-chain metadata. A **class** groups tokens issued under one set of rules: who may mint, how many can exist, whether tokens can change hands, and whether metadata can still change.

Uses on this chain:

- **Art and media editions.** A member issues numbered or open editions of their own CC0 work. Collectors support the artist through primary sales and royalties on resale.
- **Credentials and badges.** A council issues non-transferable ("soulbound") tokens: event attendance, completed initiatives, committee service.
- **Provenance and patronage.** A token is a public, transferable record that "this address supported this work first", independent of any copyright.
- **Collection items.** `x/collect` can reference a local NFT and show its live owner instead of an unverified external pointer.

### 1.1. Design Goals

1. **Minimal, auditable core.** Mint, transfer and burn are small and hard to get wrong. Trading, royalties and moderation are separate layers on top of that core.
2. **Holders' property is protected from issuers.** Every power an issuer keeps over issued tokens (editing metadata, revoking, minting more) is visible on the class, fixed or reducible only, and never extensible after the fact.
3. **No blanket approvals.** There is no "operator for all my tokens" approval, the primitive behind most NFT wallet drains elsewhere. Delegation goes through `x/session` (scoped, expiring, revocable, one registry), and the value-moving messages stay outside its ceiling.
4. **Spam costs the spammer.** Every live token is backed by a SPARK storage deposit, refunded to whoever burns it. Unsolicited tokens from unaccountable senders wait in a capped inbox, and each inbox item burns a small non-refundable fee.
5. **Consistent with the chain's commitments.** CC0 content, SPARK-only pricing (DREAM is never tradeable), human moderation, no oracles, no cross-chain tokens.

### 1.2. Non-Goals (v1)

- **No IBC transfer of NFTs** (no ICS-721). Federation's sovereignty rule is "no cross-chain tokens" ([x-federation-spec.md](x-federation-spec.md)). See §16.
- **No licensing layer.** Owning a token grants no copyright or usage rights. Everything on the chain is CC0 ([content-license.md](content-license.md)), and the referenced media must be too (§7.10).
- **No smart-contract or transfer hooks.** Classes run no code on transfer.
- **No fractionalization, NFT-collateralized lending or token-bound accounts.** A token never holds fungible value beyond its own storage deposit. Renting out usage rights is a separate matter (§16.1).
- **No anonymous ownership via x/shield.** Addresses are already pseudonymous, and shielded custody would need a note-based design of its own (§16).
- **No auctions or escrowed bids.** Fixed-price listings only (§16).

### 1.3. Clean-Room Provenance

This module is a **clean-room implementation**, the same posture as `x/session` and the speculative `x/dex`. This specification was written from first principles, from Spark Dream's own module conventions, and from the general concept of a non-fungible token as public standards documents describe it. Implementers:

- **MUST NOT** read, copy, port or translate source code from the Cosmos SDK `x/nft` module, IRISnet `nft`, CosmWasm `cw721`/`cw-nfts`, Stargaze, OmniFlix, or any EVM NFT or marketplace contract (ERC-721 reference implementations, OpenZeppelin, Seaport and similar), whatever their license.
- **MAY** use this document, Spark Dream's own modules (`x/collect`, `x/name` and `x/session` for conventions), and Cosmos SDK *framework* APIs the chain already depends on (`collections`, `bank`, `auth`, `msgservice`).
- The Cosmos SDK `x/nft` module **is not and must not be** wired into the app (it is absent from `go.mod` and `app_config.go`). To rule out any store-key or name collision with it, this module is named `artifact` (Ignite reserves `nft` and every `nft*` prefix): module name and store key `artifact`, proto package `sparkdream.artifact.v1`, CLI `sparkdreamd tx|query artifact ...`. "Artifact" is Spark Dream's name for an NFT.
- The implementing PR states in its description that it was written from this spec without consulting the sources listed above.

---

## 2. Dependencies

| Module | Usage |
|--------|-------|
| `x/auth` | Address codec, module account (`artifact`, `Burner` permission only, for fee burns) |
| `x/bank` | Storage deposits (escrow + refund), creation/inbox fee burns, sale settlement, blocked-address check |
| `x/identity` | `BondDenom(ctx)` (the only accepted denom for prices, fees and deposits) and `DreamDenom(ctx)` (explicitly rejected) |
| `x/rep` | Membership and trust-level gates; content-sentinel `BondedRole`; jury for hide appeals; `RoleActivity` reporting |
| `x/commons` | Operational-params authority; council-body policy check (trusted senders, council-owned classes) |
| `x/distribution` | Sale fee routed to the community pool with `FundCommunityPool`, where x/split distributes it. Wired through a distribution adapter (§11.5) |
| `x/common` (package) | `MediaFlag` bits, `ContainsDataURI`, `ModerationReason`, content-license constants |

**Dependents:** `x/collect`, optionally (§11.4). Nothing else depends on `x/artifact`. Cross-module keepers are late-wired in `app.go`, never added as optional depinject inputs (CLAUDE.md, depinject cycles).

---

## 3. State Objects

All amounts are `cosmossdk.io/math.Int` in the bond denom's base unit (`uspark` on the main network). User-facing windows (mint windows, listings, inbox expiry, class handover) use **block time in unix seconds**. Moderation deadlines use **block heights**, matching `x/collect` (§7.11).

### 3.1. Class

```protobuf
message Class {
  uint64 id = 1;                         // Auto-assigned, monotonically increasing, never reused
  string owner = 2;                      // Current class owner (account or group policy address)
  string creator = 3;                    // Original creator; immutable
  string name = 4;                       // ≤ max_name_length; NOT unique
  string symbol = 5;                     // ≤ max_symbol_length; [A-Z0-9]; NOT unique
  string description = 6;                // ≤ max_description_length
  string uri = 7;                        // Class metadata / cover media (§7.9); optional
  string uri_hash = 8;                   // Optional lowercase hex SHA-256 of the bytes at uri
  string token_uri_base = 9;             // Optional; clients resolve token_uri_base + token_id when a token has no uri
  ClassFlags flags = 10;                 // Immutable after creation (§3.2)
  bool metadata_frozen = 11;             // One-way: false → true
  uint64 max_supply = 12;                // 0 = unlimited; may only be lowered, or set from 0 to N (§7.4)
  bool minting_closed = 13;              // One-way: false → true
  uint64 next_token_id = 14;             // Next id to assign; starts at 1
  uint64 supply = 15;                    // Live tokens
  uint64 reserved_supply = 16;           // Pending mints (§3.7)
  uint64 burned = 17;                    // Lifetime burned + revoked count
  repeated string minters = 18;          // ≤ max_minters_per_class; owner is implicitly a minter; stored inline
  MintPolicy mint_policy = 19;           // Public mint configuration (§3.3)
  string payout_address = 20;            // Receives public-mint proceeds; defaults to creator
  uint32 royalty_bps = 21;               // ≤ max_royalty_bps; may only be lowered (§7.8)
  repeated RoyaltyShare royalty_recipients = 22; // How the royalty divides; weights sum to 10000; defaults to the creator alone
  ContentStatus status = 23;             // ACTIVE | HIDDEN (§7.11)
  uint32 media_flags = 24;               // sparkdream.common.v1.MediaFlag bits (§7.9)
  uint32 media_rules_version = 25;
  int64 created_at = 26;
  int64 updated_at = 27;
}
```

**Supply accounting.** Three counters cover every token that exists or might: `supply` (live), `burned` (gone) and `reserved_supply` (pending acceptance). The cap counts tokens **ever issued**:

```
issued = supply + burned + reserved_supply
mint allowed iff max_supply == 0 || issued + n ≤ max_supply
```

Burning frees no capacity, so an edition of 100 stays an edition of 100. A rejected or expired pending mint *does* free its slot (`reserved_supply--` without `burned++`), since that token never existed.

**Ids are identifiers, not edition numbers.** A rejected pending mint consumes its id permanently (ids are never reused, so an indexer never sees one id name two different tokens). A capped class can therefore have token `#105` in an edition of 100. Clients that show "N of M" use mint order, not the id.

### 3.2. ClassFlags

Set at creation and **immutable for the life of the class**. Clients show them prominently, because they define what the issuer can still do to holders.

```protobuf
message ClassFlags {
  bool transferable = 1;                       // false = soulbound: no transfer, listing or pending transfer
  BurnAuthorization burn_authorization = 2;    // Who may burn tokens; ISSUER and HOLDER_OR_ISSUER require transferable == false
  bool token_metadata_mutable = 3;             // true = class owner may edit token metadata until frozen; false = fixed at mint
}

enum BurnAuthorization {
  BURN_AUTHORIZATION_HOLDER = 0;               // Holder only (MsgBurn). The default
  BURN_AUTHORIZATION_ISSUER = 1;               // Class owner only (MsgRevoke); the holder cannot burn
  BURN_AUTHORIZATION_HOLDER_OR_ISSUER = 2;     // Either one, alone
}

message RoyaltyShare {
  string address = 1;                          // Recipient rule applies (§5)
  uint32 weight_bps = 2;                       // Share of the royalty; a class's weights sum to 10000
}
```

The zero value of `burn_authorization` is holder-only, so a class that sets nothing behaves as a plain holder-burnable class. A `NEITHER` mode (nobody may burn) is deliberately absent: nobody could ever reclaim its deposits. It can be appended in an upgrade once it has a deposit rule (§16.2).

**Invariant:** an issuer who can burn (`ISSUER` or `HOLDER_OR_ISSUER`) implies `!transferable`. An issuer-burnable, transferable token would let an issuer sell a token and then destroy it in the buyer's hands. Revocation exists only for credentials, which are never sold.

**Consent for unburnable tokens.** Under `ISSUER` the holder cannot get rid of the token, so nobody may be given one without accepting it: every `MsgMint` of an `ISSUER` class to another address goes to the recipient's inbox, whatever their receive policy and whoever the minter is, council bodies included (§7.6).

### 3.3. MintPolicy

Controls **public minting** (`MsgPublicMint`). Minting by the owner and listed minters (`MsgMint`) ignores this policy.

```protobuf
message MintPolicy {
  bool public_mint_enabled = 1;
  cosmos.base.v1beta1.Coin price = 2;     // Per token; denom MUST be bond_denom; amount may be 0
  int64 start_time = 3;                   // 0 = immediately
  int64 end_time = 4;                     // 0 = no end; must be > start_time when both set
  uint32 per_address_limit = 5;           // 0 = unlimited; counted per (class, address) for the class's life
}
```

`per_address_limit` only limits casual over-minting, since anyone can create new addresses. Classes that need sybil resistance should use minter-only issuance.

### 3.4. Token

```protobuf
message Token {
  uint64 class_id = 1;
  uint64 id = 2;                          // Assigned from Class.next_token_id
  string owner = 3;
  string minter = 4;                      // Address that signed the mint; immutable
  TokenMetadata metadata = 5;
  bool metadata_frozen = 6;               // One-way. Always true when the class has token_metadata_mutable == false
  TokenLock lock = 7;                     // NONE | LISTED | PENDING_TRANSFER (§7.2)
  uint64 listing_seq = 8;                 // Monotonic per token; bumped on every MsgList / MsgUpdateListing (§7.7)
  string deposit = 9;                     // math.Int in the bond denom; held in the module account; refunded on burn/revoke
  ContentStatus status = 10;              // ACTIVE | HIDDEN (own hide only; class hide is checked separately)
  uint32 media_flags = 11;
  uint32 media_rules_version = 12;
  int64 minted_at = 13;
}

message TokenMetadata {
  string name = 1;                        // Optional; ≤ max_name_length
  string description = 2;                 // Optional; ≤ max_description_length
  string uri = 3;                         // Optional media / metadata URI (§7.9)
  string uri_hash = 4;                    // Optional lowercase hex SHA-256
  repeated Attribute attributes = 5;      // ≤ max_attributes
}

message Attribute {
  string key = 1;    // [a-z0-9_]{1,max_attribute_key_length}; unique within the token
  string value = 2;  // ≤ max_attribute_value_length
}

message TokenRef {
  uint64 class_id = 1;
  uint64 token_id = 2;
}

enum TokenLock {                          // One exclusive lock at a time; future locks are appended (§16.2)
  TOKEN_LOCK_NONE = 0;
  TOKEN_LOCK_LISTED = 1;
  TOKEN_LOCK_PENDING_TRANSFER = 2;
}

enum ContentStatus {
  CONTENT_STATUS_UNSPECIFIED = 0;
  CONTENT_STATUS_ACTIVE = 1;
  CONTENT_STATUS_HIDDEN = 2;
}
```

**Metadata hash.** `metadata_hash(m) = SHA-256(deterministic proto encoding of TokenMetadata m)`, lowercase hex. Queries return it with every token and pending mint. `MsgAcceptIncoming` and `MsgBuy` can pin it (§12.2).

The deposit is recorded **per token** at mint, so a later change to `token_deposit` never changes what an existing token refunds. The module account always holds at least the sum of recorded deposits (§13).

### 3.5. ReceivePolicy

```protobuf
enum ReceivePolicy {
  RECEIVE_POLICY_UNSPECIFIED = 0;  // Use params.default_receive_policy
  RECEIVE_POLICY_OPEN = 1;         // Accept every incoming token immediately
  RECEIVE_POLICY_MEMBERS = 2;      // Accept immediately from trusted senders (§7.6); others go to the inbox
  RECEIVE_POLICY_INBOX = 3;        // Every incoming token goes to the inbox
}
```

Stored per address only when set. A missing entry means `params.default_receive_policy`.

### 3.6. PendingTransfer

An existing token on its way to a recipient who has not yet accepted it. The token stays owned by the sender with `lock = PENDING_TRANSFER` until it resolves.

```protobuf
message PendingTransfer {
  uint64 class_id = 1;
  uint64 token_id = 2;
  string from = 3;
  string to = 4;
  int64 created_at = 5;
  int64 expires_at = 6;      // created_at + pending_ttl
}
```

### 3.7. PendingMint

A mint to a recipient whose policy sends it to the inbox. The token id is reserved and the deposit escrowed, but no `Token` exists until the recipient accepts. A soulbound token is therefore never owned by anyone except its final holder.

```protobuf
message PendingMint {
  uint64 class_id = 1;
  uint64 token_id = 2;                    // Reserved id
  string minter = 3;                      // Signer and deposit payer
  string to = 4;
  TokenMetadata metadata = 5;
  string deposit = 6;                     // math.Int in the bond denom
  int64 created_at = 7;
  int64 expires_at = 8;
}

enum InboxKind {
  INBOX_KIND_UNSPECIFIED = 0;
  INBOX_KIND_TRANSFER = 1;
  INBOX_KIND_MINT = 2;
}
```

**Inbox origin.** Inbox caps are keyed by *origin*, not by signer. For a transfer the origin is the sender's address. For a mint it is the class (`"class/<id>"`), so a class owner cannot get past the per-pair cap by rotating minters.

### 3.8. Listing

A fixed-price offer to sell. **No funds or tokens are escrowed.** The token stays with the seller, locked, and settlement is atomic in `MsgBuy`.

```protobuf
message Listing {
  uint64 class_id = 1;
  uint64 token_id = 2;
  string seller = 3;
  cosmos.base.v1beta1.Coin price = 4;     // denom MUST be bond_denom; amount > 0
  int64 created_at = 5;
  int64 expires_at = 6;                   // ≤ now + max_listing_duration
  uint64 nonce = 7;                       // = Token.listing_seq at the last list/update; MsgBuy must match it
}
```

### 3.9. PendingClassOwner

```protobuf
message PendingClassOwner {
  uint64 class_id = 1;
  string proposed_owner = 2;
  int64 expires_at = 3;                   // proposed_at + pending_ttl
}
```

### 3.10. HideRecord

The moderation record. Its lifecycle follows `x/collect`'s `HideRecord` ([x-collect-spec.md](x-collect-spec.md) §3.21); the shape is artifact-specific.

```protobuf
enum HideTargetKind {
  HIDE_TARGET_KIND_UNSPECIFIED = 0;
  HIDE_TARGET_KIND_CLASS = 1;
  HIDE_TARGET_KIND_TOKEN = 2;
}

message HideRecord {
  uint64 id = 1;
  HideTargetKind target_kind = 2;
  uint64 class_id = 3;
  uint64 token_id = 4;                    // 0 for CLASS
  string sentinel = 5;                    // "" for a council hide (gov-hide marker, as in collect)
  string committed_amount = 6;            // DREAM locked from the sentinel bond (math.Int); 0 for council
  sparkdream.common.v1.ModerationReason reason = 7;
  string reason_text = 8;
  int64 hidden_at = 9;                    // Block height
  int64 appeal_deadline = 10;             // Block height of the hide expiry (unappealed hides)
  string appellant = 11;                  // Set on appeal
  bool appealed = 12;
  uint64 appeal_id = 13;                  // x/rep GovActionAppeal id (ARTIFACT_HIDE) once appealed
  HideOutcome outcome = 14;
}

enum HideOutcome {
  HIDE_OUTCOME_UNSPECIFIED = 0;
  HIDE_OUTCOME_PENDING = 1;          // Hidden; awaiting self-correct, appeal or expiry
  HIDE_OUTCOME_UNHIDDEN = 2;         // Sentinel self-correct or council unhide
  HIDE_OUTCOME_OVERTURNED = 3;       // x/rep verdict OVERTURNED; content restored
  HIDE_OUTCOME_UPHELD = 4;           // x/rep verdict UPHELD; metadata scrubbed
  HIDE_OUTCOME_EXPIRED = 5;          // No appeal by hide_expiry_blocks; metadata scrubbed
  HIDE_OUTCOME_APPEAL_TIMEOUT = 6;   // x/rep appeal timed out without a verdict; restored
  HIDE_OUTCOME_TARGET_BURNED = 7;    // Holder burned the token before resolution
}
```

### 3.11. Params

Governance-controlled. `MsgUpdateParams` validates the whole set, including the hard bounds in §3.13.

```protobuf
message Params {
  // --- Gates (GOVERNANCE) ---
  rep.v1.TrustLevel min_trust_level_create_class = 1;  // default PROVISIONAL
  uint32 max_classes_per_creator = 2;                  // Live classes created by the address; default 50
  ReceivePolicy default_receive_policy = 3;            // default MEMBERS; must not be UNSPECIFIED

  // --- Size limits (GOVERNANCE) ---
  uint32 max_name_length = 4;                          // default 128
  uint32 max_symbol_length = 5;                        // default 16
  uint32 max_description_length = 6;                   // default 2048
  uint32 max_uri_length = 7;                           // default 512
  uint32 max_attributes = 8;                           // default 16
  uint32 max_attribute_key_length = 9;                 // default 64
  uint32 max_attribute_value_length = 10;              // default 256
  uint32 max_minters_per_class = 11;                   // default 10
  uint32 max_batch_size = 12;                          // Entries per mint/transfer/accept/reject/burn; default 50
  repeated string allowed_uri_schemes = 13;            // default ["ipfs", "ar", "https"]

  // --- Market bounds (GOVERNANCE) ---
  uint32 max_royalty_bps = 14;                         // default 1000 (10%)
  uint32 sale_fee_bps = 15;                            // default 100 (1%); to community pool

  // --- Operational (also in ArtifactOperationalParams) ---
  string class_creation_fee = 16;                      // SPARK, burned; default 10 SPARK
  string token_deposit = 17;                           // SPARK, held per token; default 0.1 SPARK
  string inbox_fee = 18;                               // SPARK, burned per inbox item created; default 0.02 SPARK
  int64 pending_ttl = 19;                              // Seconds; default 7 days
  int64 max_listing_duration = 20;                     // Seconds; default 90 days
  uint32 max_pending_per_pair = 21;                    // Open inbox items per (origin, recipient); default 5
  uint32 max_inbox_per_recipient = 22;                 // Open inbox items per recipient; default 100
  uint32 max_expirations_per_block = 23;               // EndBlocker cap per pass; default 200
  bool market_enabled = 24;                            // Circuit breaker for MsgList/MsgUpdateListing/MsgBuy; default true
  bool public_mint_enabled = 25;                       // Circuit breaker for MsgPublicMint; default true

  // --- Moderation (OPERATIONAL; block heights, mirroring x/collect names) ---
  uint32 max_hides_per_sentinel_per_day = 26;          // default 50
  int64 sentinel_unhide_window_blocks = 27;            // Sentinel self-correct window; default ~24h
  int64 hide_expiry_blocks = 28;                       // Unappealed hide → metadata scrub; default ~7 days
  string sentinel_hide_commit_dream = 29;              // DREAM locked per hide; default 100
}
// Appeal bond and deadline are x/rep's (DefaultAppealBondAmount, DefaultAppealDeadline).
```

### 3.12. ArtifactOperationalParams

Fields 16–29 of `Params`, in the same order. They are applied by `MsgUpdateOperationalParams`, which **replaces all of them at once**, so clients must send every field (see the `OperationalParams` full-replacement convention in other modules). Gates, size limits, URI schemes and market bounds stay governance-only.

### 3.13. Hard Bounds

Compiled constants, checked by both `Params.Validate()` and `ArtifactOperationalParams.Validate()`. Neither governance nor the Operations Committee can set values outside them; only a chain upgrade can change them. The ops authority includes individual committee members (§5.6), so these bounds are the real safety net for the operational fields.

| Field | Min | Max |
|-------|-----|-----|
| `max_royalty_bps` | 0 | 2500 |
| `sale_fee_bps` | 0 | 1000 |
| `class_creation_fee` | 1 SPARK | 10,000 SPARK |
| `token_deposit` | 0.01 SPARK | 100 SPARK |
| `inbox_fee` | 0.001 SPARK | 10 SPARK |
| `pending_ttl` | 1 day | 30 days |
| `max_listing_duration` | 1 day | 365 days |
| `max_pending_per_pair` | 1 | 50 |
| `max_inbox_per_recipient` | 10 | 1000 |
| `max_expirations_per_block` | 10 | 1000 |
| `max_batch_size` | 1 | 100 |
| `max_hides_per_sentinel_per_day` | 1 | 500 |
| `hide_expiry_blocks` | ~1 day | ~90 days |

`max_royalty_bps + sale_fee_bps ≤ 10000` holds by construction. `allowed_uri_schemes` must be non-empty and must never contain `data`, `javascript`, `file`, `blob` or `http`.

---

## 4. Storage Schema

Cosmos SDK `collections`:

| Collection | Key | Value | Purpose |
|------------|-----|-------|---------|
| `Params` | — | `Params` | |
| `ClassSeq` | — | `uint64` | Next class id (starts at 1) |
| `Classes` | `class_id` | `Class` | |
| `ClassesByOwner` | `(owner, class_id)` | — | Owner index |
| `ClassCountByCreator` | `creator` | `uint32` | `max_classes_per_creator` |
| `Tokens` | `(class_id, token_id)` | `Token` | |
| `TokensByOwner` | `(owner, class_id, token_id)` | — | Wallet listing |
| `PublicMintCount` | `(class_id, address)` | `uint32` | `per_address_limit` |
| `ReceivePolicies` | `address` | `ReceivePolicy` | Explicit settings only |
| `PendingTransfers` | `(class_id, token_id)` | `PendingTransfer` | At most one per token |
| `PendingMints` | `(class_id, token_id)` | `PendingMint` | |
| `InboxByRecipient` | `(to, class_id, token_id)` | `InboxKind` | Recipient view |
| `OutboxBySender` | `(signer, class_id, token_id)` | `InboxKind` | Sender view (transfer `from`, mint `minter`) |
| `PendingPairCount` | `(origin, to)` | `uint32` | `max_pending_per_pair` |
| `InboxCount` | `to` | `uint32` | `max_inbox_per_recipient` |
| `PendingExpiry` | `(expires_at, class_id, token_id)` | — | EndBlocker |
| `Listings` | `(class_id, token_id)` | `Listing` | Class-prefix iterable |
| `ListingsBySeller` | `(seller, class_id, token_id)` | — | |
| `ListingExpiry` | `(expires_at, class_id, token_id)` | — | EndBlocker |
| `ClassCancelQueue` | `class_id` | `uint64` (cursor token id) | Listing drain after a class hide (§7.11) |
| `ClassScrubQueue` | `class_id` | `uint64` (cursor token id) | Token-metadata scrub after a class hide expires (§7.11) |
| `PendingClassOwners` | `class_id` | `PendingClassOwner` | |
| `PendingOwnerExpiry` | `(expires_at, class_id)` | — | EndBlocker |
| `HideSeq` | — | `uint64` | |
| `HideRecords` | `hide_id` | `HideRecord` | |
| `HideByTarget` | `(class_id, token_id)` | `hide_id` | Active record per target (token_id 0 = class) |
| `HideExpiry` | `(height, hide_id)` | — | EndBlocker |
| `SentinelDailyHides` | `(sentinel, day)` | `uint32` | Rate limit |

Indexes are updated in the same function that writes the primary record. `PendingPairCount` and `InboxCount` are derived counters and are rebuilt at genesis import (§14).

---

## 5. Messages

Every signer message carries `option (cosmos.msg.v1.signer)` and `option (amino.name) = "sparkdream/x/artifact/Msg<Name>"`, for Ledger amino-JSON signing (CLAUDE.md). Add a service-walking guard modelled on [x/collect/types/amino_name_test.go](../x/collect/types/amino_name_test.go) so new messages are covered automatically.

General validation that applies to every message:

- Addresses decode with the chain's bech32 prefix.
- **Recipient rule.** Every address that receives a token, funds or class ownership (token recipient, `payout_address`, every `royalty_recipients` address, proposed class owner) must **not** be a bank-blocked (module) address, or the token or funds would be stranded (`ErrInvalidRecipient`). `MsgTransfer` and `MsgProposeClassOwner` also reject recipient == signer. Minting to yourself is allowed.
- **Coin rule.** Every `Coin` must be in `BondDenom`. A `DreamDenom` coin is rejected with `ErrDreamNotAccepted`, any other denom with `ErrInvalidDenom`.
- Every metadata string passes the metadata rules (§7.9).
- Batches hold 1..`max_batch_size` entries with no duplicate `(class_id, token_id)` (`ErrInvalidBatch`). A batch is all-or-nothing.

### 5.1. Class Lifecycle

#### 5.1.1. MsgCreateClass

```protobuf
message MsgCreateClass {
  string creator = 1;
  string name = 2;
  string symbol = 3;
  string description = 4;
  string uri = 5;
  string uri_hash = 6;
  string token_uri_base = 7;
  ClassFlags flags = 8;
  uint64 max_supply = 9;
  MintPolicy mint_policy = 10;
  string payout_address = 11;            // Empty → creator
  uint32 royalty_bps = 12;
  repeated RoyaltyShare royalty_recipients = 13; // Empty → the creator takes the whole royalty
  repeated string minters = 14;
  string accepted_content_license = 15;  // MUST equal "CC0-1.0" (§7.10)
}
```

**Logic:**
1. The creator is an active `x/rep` member with trust level ≥ `min_trust_level_create_class`, **or** a council-body policy address (§7.6), so councils can issue badges by proposal. Otherwise `ErrInsufficientTrust`.
2. `ClassCountByCreator[creator] < max_classes_per_creator` (`ErrTooManyClasses`).
3. `accepted_content_license == "CC0-1.0"` (`ErrContentLicenseNotAccepted`).
4. Flags: `burn_authorization` is a known value, and an issuer who can burn implies `!transferable` (`ErrInvalidFlags`).
5. `royalty_bps ≤ max_royalty_bps` (`ErrRoyaltyTooHigh`). A non-transferable class must have `royalty_bps == 0` and a public mint price of 0, since soulbound tokens cannot be sold (`ErrInvalidFlags`).
6. The royalty split validates: 1 to 10 distinct addresses (`MaxRoyaltyRecipients`), each weight in (0, 10000], weights summing to exactly 10000 (`ErrInvalidRoyaltySplit`), and every address passes the recipient rule.
7. The mint policy validates (§3.3). `minters` is deduplicated, ≤ `max_minters_per_class`, and excludes the creator.
8. Burn `class_creation_fee` from the creator. The fee is a non-refundable anti-spam cost.
9. Assign `id = ClassSeq++`, `owner = creator`, `next_token_id = 1`, `status = ACTIVE`. Compute media flags. Write the class and its indexes, and increment `ClassCountByCreator`.
10. Emit `artifact_class_created`.

#### 5.1.2. MsgUpdateClass

Owner only. Updates `name`, `description`, `uri`, `uri_hash`, `token_uri_base`, `payout_address`, `royalty_recipients` and `royalty_bps`. Fields absent from the message are left unchanged; the update mask lists them.

- Rejected when `metadata_frozen` and any of `name`, `description`, `uri`, `uri_hash` or `token_uri_base` changes (`ErrMetadataFrozen`). `payout_address` and `royalty_recipients` stay editable after a freeze, because they are payment routing, not content.
- `royalty_bps` may only **decrease** (`ErrRoyaltyIncrease`).
- `symbol` and `flags` never change.
- Rejected while the class is HIDDEN (`ErrContentHidden`), so a hide cannot be dodged by editing.
- Media flags are recomputed. Emits `artifact_class_updated` with the changed field names.

#### 5.1.3. MsgFreezeClassMetadata

Owner only. Sets `metadata_frozen = true`, which freezes `token_uri_base` and **every token's metadata** in the class. This step cannot be undone. Emits `artifact_class_metadata_frozen`.

#### 5.1.4. MsgSetMintPolicy

Owner only. Replaces `mint_policy`. Rejected when `minting_closed`, when the class is HIDDEN, or for a non-transferable class with a non-zero price. A price change takes effect immediately; buyers are protected by `max_price` in `MsgPublicMint`. Emits `artifact_mint_policy_set`.

#### 5.1.5. MsgSetMinters

Owner only. `{add[], remove[]}` applied atomically. The resulting set must be ≤ `max_minters_per_class` and must not contain the owner. Removing a minter leaves tokens it already minted and its pending mints untouched. The class owner can cancel those pending mints with `MsgCancelOutgoing`. Emits `artifact_minters_set`.

#### 5.1.6. MsgSetMaxSupply

Owner only. Rejected when `minting_closed`. The new value must be **≥ `supply + burned + reserved_supply`** and must either:
- be strictly lower than the current non-zero `max_supply`, or
- be non-zero when the current value is 0 (unlimited → capped).

Raising or removing a cap is never allowed (`ErrSupplyIncrease`). Collectors rely on edition sizes.

#### 5.1.7. MsgCloseMinting

Owner only. Sets `minting_closed = true` permanently. No further `MsgMint` or `MsgPublicMint` succeeds. Mints already pending can still be accepted until they expire: the id was reserved before minting closed. Emits `artifact_minting_closed`.

#### 5.1.8. MsgProposeClassOwner / MsgAcceptClassOwner / MsgCancelClassOwner

A two-step ownership handover, so a typo cannot send a class to an address nobody controls.

- `MsgProposeClassOwner{owner, class_id, proposed_owner}` writes `PendingClassOwner`, replacing any earlier proposal and its expiry entry.
- `MsgAcceptClassOwner{proposed_owner, class_id}` must arrive before `expires_at` (`ErrPendingOwnerNotFound` otherwise). On accept:
  - `owner` moves and `ClassesByOwner` is updated.
  - **`minters` is cleared and `mint_policy.public_mint_enabled` set to false**, so the previous owner cannot keep a minting backdoor through a minter key they planted.
  - `payout_address` is reset to the new owner and `royalty_recipients` to the new owner alone, **so revenue never silently keeps flowing to the seller**, so revenue never silently keeps flowing to the seller.
  - Pending mints of the class stay valid. The new owner can cancel them.
- `MsgCancelClassOwner{owner, class_id}` deletes the proposal.

The new owner needs no membership: the creator's membership gated the class's creation, not its ownership. `creator` and `ClassCountByCreator` never change.

#### 5.1.9. MsgDeleteClass

Owner only. Requires `supply == 0`, `reserved_supply == 0` and no PENDING hide record on the class (`ErrClassNotEmpty` / `ErrContentHidden`). Deletes the class (including its inline minters and mint policy), its `ClassesByOwner` entry, its `PublicMintCount` entries, any `PendingClassOwner` and its expiry entry, and any `ClassCancelQueue`/`ClassScrubQueue` entry. Decrements `ClassCountByCreator[creator]`. The class id is never reused, and the creation fee stays burned.

### 5.2. Minting

#### 5.2.1. MsgMint

```protobuf
message MsgMint {
  string minter = 1;
  uint64 class_id = 2;
  repeated MintEntry entries = 3;          // 1..max_batch_size
  string accepted_content_license = 4;     // MUST equal "CC0-1.0"
}

message MintEntry {
  string recipient = 1;                    // Empty → minter
  TokenMetadata metadata = 2;
}
```

**Logic** (all or nothing):
1. The class exists and is ACTIVE, and minting is not closed.
2. The minter is the class owner or is listed in `minters` (`ErrNotMinter`).
3. Supply check (§3.1) for `len(entries)`.
4. For each entry, in order:
   1. Validate the metadata (§7.9). A class with `token_metadata_mutable == false`, or with `metadata_frozen`, mints with `metadata_frozen = true`.
   2. Collect `token_deposit` from the minter into the module account.
   3. Assign `token_id = next_token_id++`.
   4. Resolve the recipient's receive policy (§7.6), with the minter as sender:
      - **Accepted:** create the `Token` owned by the recipient. `supply++`.
      - **Inbox:** check both inbox caps with origin `"class/<id>"` (`ErrInboxFull`). Burn `inbox_fee` from the minter. Create a `PendingMint`. `reserved_supply++`.
5. Emit `artifact_minted` (one per live token) or `artifact_mint_pending`.

**Response:** for each entry, the assigned token id and whether it is live or pending.

#### 5.2.2. MsgPublicMint

```protobuf
message MsgPublicMint {
  string buyer = 1;
  uint64 class_id = 2;
  uint32 quantity = 3;                     // 1..max_batch_size
  cosmos.base.v1beta1.Coin max_price = 4;  // Per token; reject if the current price is higher
}
```

**Logic:**
1. `params.public_mint_enabled` and `class.mint_policy.public_mint_enabled`. The class is ACTIVE and not closed, and the block time is in `[start_time, end_time)` (`ErrMintWindow`).
2. `mint_policy.price ≤ max_price` (`ErrPriceExceeded`). This protects the buyer against an owner raising the price while the tx is in the mempool.
3. `PublicMintCount[(class, buyer)] + quantity ≤ per_address_limit` when the limit is non-zero (`ErrPerAddressLimit`). Run the supply check.
4. Payment for `total = quantity × price`, with `fee = floor(total × sale_fee_bps / 10000)`:
   - `fee` goes to the community pool.
   - `total − fee` goes to `payout_address`.
   - No royalty applies on primary sales. Zero amounts are skipped.
5. Collect `quantity × token_deposit` from the buyer.
6. Mint `quantity` tokens **to the buyer** with empty per-token metadata. Clients render class metadata and `token_uri_base + token_id`. The buyer's own receive policy does not apply; they asked for the tokens.
7. Emit `artifact_public_minted` with the ids, the price paid and the fee.

### 5.3. Ownership

#### 5.3.1. MsgTransfer

```protobuf
message MsgTransfer {
  string sender = 1;
  repeated TransferEntry entries = 2;      // 1..max_batch_size
}

message TransferEntry {
  uint64 class_id = 1;
  uint64 token_id = 2;
  string recipient = 3;
}
```

**Logic** (all or nothing), per entry:
1. The token exists, `owner == sender`, `lock == NONE` (`ErrTokenLocked`), and the class is `transferable` (`ErrNotTransferable`).
2. The recipient rule (§5).
3. Resolve the recipient's receive policy (§7.6):
   - **Accepted:** move ownership (§7.1).
   - **Inbox:** check both inbox caps with origin = sender. Burn `inbox_fee` from the sender. Write a `PendingTransfer` and set `lock = PENDING_TRANSFER`.
4. Emit `artifact_transferred` or `artifact_transfer_pending`.

HIDDEN tokens, and tokens of HIDDEN classes, can still be transferred. Moderation governs what clients display, not who owns what (§7.11).

#### 5.3.2. MsgAcceptIncoming / MsgRejectIncoming

```protobuf
message MsgAcceptIncoming {
  string recipient = 1;
  repeated AcceptEntry entries = 2;        // 1..max_batch_size
}
message AcceptEntry {
  TokenRef ref = 1;
  string expected_metadata_hash = 2;       // Optional; if set, must equal the current metadata_hash (§3.4)
}
message MsgRejectIncoming {
  string recipient = 1;
  repeated TokenRef refs = 2;
}
```

Each ref must be in the signer's inbox (`ErrPendingNotFound`). **Expiry is checked at use:** an item with `expires_at ≤ now` is not accepted (`ErrPendingExpired`); it is resolved as expired on the spot, or by the EndBlocker. Rejecting an expired item succeeds and resolves it as expired.

- **Accept a transfer:** clear the lock and move ownership from `from` to the recipient (§7.1). The class must still be transferable, which is always true because flags are immutable.
- **Accept a mint:** create the `Token` from the `PendingMint`. `reserved_supply--`, `supply++`. Acceptance works even if minting has since closed or the class has been hidden, because the id was reserved first. A hidden class's token arrives hidden in queries, as any token of that class would. The escrowed deposit becomes the token's deposit.
- **Reject or expire a transfer:** clear the lock. The token stays with the sender.
- **Reject or expire a mint:** `reserved_supply--` (no `burned++`). Refund the deposit to the minter. The id stays consumed. The `inbox_fee` is never refunded.

All resolutions delete the pending record and its indexes, and decrement `PendingPairCount` and `InboxCount`.

#### 5.3.3. MsgCancelOutgoing

`{signer, repeated TokenRef refs}`. The `from` of a pending transfer, the `minter` of a pending mint, or the **current class owner** for any pending mint of their class may withdraw it. The effect is the same as a rejection.

#### 5.3.4. MsgSetReceivePolicy

`{owner, policy}`. `UNSPECIFIED` deletes the entry, so the default applies again. Existing inbox items are not resolved. Emits `artifact_receive_policy_set`.

#### 5.3.5. MsgBurn

`{owner, repeated TokenRef refs}`. The signer owns each token.

- A `LISTED` token is delisted first. This is allowed and implicit.
- A `PENDING_TRANSFER` token must be cancelled first (`ErrTokenLocked`), so a recipient never sees an inbox item vanish without a resolution event.
- Soulbound, HIDDEN and hidden-class tokens can all be burned: **a holder can always get rid of a token**, except under `burn_authorization = ISSUER` (`ErrHolderBurnNotAllowed`). Those tokens only ever reach a holder who accepted them from the inbox, or who minted them to themselves (§3.2).

Effects:
- Delete the token and its owner index. `supply--`, `burned++`.
- Refund `token.deposit` **to the burner**.
- If the token has a PENDING hide record, resolve it as `TARGET_BURNED` (§7.11).
- Emit `artifact_burned`.

The refund goes to the holder, not the original minter, so a sender of unwanted tokens pays the recipient to clean them up (§12.3).

#### 5.3.6. MsgRevoke

`{owner (class owner), class_id, token_ids[], reason}`. `reason` is ≤ 256 characters. Requires `burn_authorization` of `ISSUER` or `HOLDER_OR_ISSUER` (`ErrNotRevocable`). The effects match `MsgBurn`, except that **the deposit is refunded to the holder**, never to the issuer, so revocation never pays the revoker. An issuer-burnable class is necessarily non-transferable, so no listing or pending transfer can exist on its tokens. Emits `artifact_revoked` with the holder and reason.

#### 5.3.7. MsgUpdateToken / MsgFreezeTokenMetadata

`MsgUpdateToken{owner (class owner), class_id, token_id, metadata}` replaces a token's `TokenMetadata` wholesale and recomputes media flags. It requires:
- `flags.token_metadata_mutable`
- `!class.metadata_frozen` and `!token.metadata_frozen` (`ErrMetadataFrozen`)
- neither the token nor its class is HIDDEN (`ErrContentHidden`)

It also works on a pending mint, by updating `PendingMint.metadata`. The recipient can pin `expected_metadata_hash` at acceptance, so a swap in the meantime makes the acceptance fail instead of delivering something else (§12.2).

`MsgFreezeTokenMetadata{signer, refs[]}` freezes individual tokens (one-way). Either the **class owner** or the **token holder** may sign. A buyer can lock in what they bought, so the issuer can no longer change it.

### 5.4. Market

The market is a convenience layer for trustless sales. It holds no funds. Royalties are enforced **only** on sales through it (§7.8).

#### 5.4.1. MsgList

`{seller, class_id, token_id, price, duration}`.

1. `params.market_enabled` (`ErrMarketDisabled`).
2. The class is `transferable`, and neither the token nor its class is HIDDEN.
3. `owner == seller` and `lock == NONE`.
4. `price.denom == BondDenom` and `price.amount > 0`.
5. `0 < duration ≤ max_listing_duration`.

Effects: `token.listing_seq++`. Write the `Listing` with `nonce = listing_seq`, set `lock = LISTED`, and add the expiry and seller indexes. Emit `artifact_listed`.

#### 5.4.2. MsgUpdateListing

`{seller, class_id, token_id, price, duration}`. It runs the same checks as `MsgList`, including the hidden check and the circuit breaker. It changes the price, resets `expires_at = now + duration`, sets `token.listing_seq++`, and sets `nonce = listing_seq`. Emits `artifact_listing_updated`.

#### 5.4.3. MsgDelist

`{seller, class_id, token_id}`. Deletes the listing and its indexes and clears the lock. Emits `artifact_delisted`. Always allowed, even when the market is disabled or the token is hidden, so a seller can always get their token back.

#### 5.4.4. MsgBuy

```protobuf
message MsgBuy {
  string buyer = 1;
  uint64 class_id = 2;
  uint64 token_id = 3;
  cosmos.base.v1beta1.Coin expected_price = 4;  // Must equal listing.price exactly
  uint64 expected_nonce = 5;                    // Must equal listing.nonce
  string expected_metadata_hash = 6;            // Optional; must equal the token's current metadata_hash if set
}
```

**Logic:**
1. `params.market_enabled`. The listing exists, `expires_at > now` (`ErrListingExpired`), and neither the token nor its class is HIDDEN.
2. The buyer is not the seller and passes the recipient rule.
3. `expected_price == listing.price` and `expected_nonce == listing.nonce` (`ErrListingChanged`). If `expected_metadata_hash` is set, it must match (`ErrMetadataChanged`).
4. Settlement (§7.7): pay the fee, royalty and proceeds directly from the buyer.
5. Delete the listing and its indexes, clear the lock, and move ownership to the buyer (§7.1). The buyer's receive policy does not apply.
6. Emit `artifact_sold`.

All of this runs in one message, under the SDK's transactional state. A failure at any step reverts everything.

### 5.5. Moderation

```protobuf
message MsgHideContent {
  string authority = 1;                    // Sentinel address, or council policy / gov
  HideTargetKind target_kind = 2;
  uint64 class_id = 3;
  uint64 token_id = 4;
  sparkdream.common.v1.ModerationReason reason = 5;
  string reason_text = 6;
}
message MsgUnhideContent     { string authority = 1; uint64 hide_id = 2; }
message MsgAppealHide { string appellant = 1; uint64 hide_id = 2; string reason = 3; }  // response: x/rep appeal_id
```

The authority paths match `x/collect` §5.25:
- **Sentinel path.** The signer passes `x/rep` `EligibleForRole(ROLE_TYPE_CONTENT_SENTINEL)`. It is checked against the shared overturn cooldown and the per-day hide cap, and reserves `sentinel_hide_commit_dream` on the sentinel bond.
- **Council path.** Otherwise the signer must be the Commons Operations authority, `IsCouncilAuthorized(ctx, authority, "commons", "operations")`: gov, the council or committee policy, or an Ops Committee member, the same check collect uses. Council hides have `sentinel = ""` and commit nothing.

`MsgUnhideContent` (before any appeal) is either:
- the hiding sentinel within `sentinel_unhide_window_blocks` (self-correct, no verdict recorded), or
- the Commons Operations authority at any time (an overturn, recorded with `RecordRoleOutcome(..., false)`).

Both release the sentinel's committed bond.

The appellant is the **token holder** for a token hide and the **class owner** for a class hide, both resolved at appeal time. `MsgAppealHide` opens the appeal in x/rep (`GOV_ACTION_TYPE_ARTIFACT_HIDE`), the same machinery forum and collect use. x/rep charges its standard appeal bond, seats a jury and owns the deadline. The case is resolved by the jury, by the Commons Operations Committee through `x/rep` `MsgResolveGovActionAppeal`, or by timeout. §7.11 describes the effects.

### 5.6. Administration

- `MsgUpdateParams`: the `x/gov` authority. Runs the full validation, including §3.13.
- `MsgUpdateOperationalParams{authority, operational_params}`: authorized by `commonsKeeper.IsCouncilAuthorized(ctx, authority, "commons", "operations")`, the same convention as `x/collect` and `x/federation`. That admits gov, the Commons Council policy, the Operations Committee policy **and any individual Operations Committee member**. Because a single member can change these fields, every one of them is clamped by the hard bounds in §3.13. The message replaces all operational fields and validates the merged result.

---

## 6. Queries

All list queries are paginated (`cosmos.base.query.v1beta1.PageRequest`) with a server-side page limit of 100. Every class and token response includes `withheld` (bool, §7.11). Every token and pending-mint response includes `metadata_hash`.

| Query | Request | Response |
|-------|---------|----------|
| `Params` | — | `Params` |
| `Class` | `class_id` | `Class` |
| `Classes` | pagination | `[]Class` |
| `ClassesByOwner` | `owner` | `[]Class` |
| `Token` | `class_id, token_id` | `Token` + `listing` if LISTED + `class_status` |
| `Tokens` | `class_id` | `[]Token` |
| `TokensByOwner` | `owner`, optional `class_id` | `[]Token` |
| `Owner` | `class_id, token_id` | `owner` (cheap lookup for clients and other modules) |
| `Supply` | `class_id` | `supply, burned, reserved_supply, max_supply, minting_closed` |
| `Listing` | `class_id, token_id` | `Listing` |
| `Listings` | optional `class_id` | `[]Listing` (excludes expired listings) |
| `ListingsBySeller` | `seller` | `[]Listing` |
| `Inbox` | `address` | `[]PendingTransfer`, `[]PendingMint` (excludes expired items), with `inbox_count` |
| `Outbox` | `address` | same |
| `ReceivePolicy` | `address` | effective policy + `is_default` |
| `PendingClassOwner` | `class_id` | `PendingClassOwner`, or not-found when expired |
| `HideRecord` / `HideRecordsByTarget` | id / `(class_id, token_id)` | `HideRecord` |
| `PublicMintQuote` | `class_id, buyer, quantity` | total price, fee, deposit, remaining per-address allowance, mint-window state |

Queries never write state. Expired records the EndBlocker has not yet drained are filtered from query responses.

---

## 7. Business Logic

### 7.1. Ownership Move

The single internal function `moveToken(token, newOwner)` is the only code path that changes `Token.owner` on an existing token. It requires `lock == NONE` (the caller clears any lock first), deletes `TokensByOwner[(old, class, id)]`, writes `TokensByOwner[(new, class, id)]`, and sets the owner. Transfers, inbox acceptances and purchases all go through it. Token creation (mint, pending-mint acceptance) writes the owner index through the single `createToken` function. The §13 owner-index invariant catches any write that bypasses both.

### 7.2. Locks

A token holds **at most one** lock:

| Lock | Set by | Cleared by | While set, rejects |
|------|--------|-----------|---------------------|
| `LISTED` | `MsgList` | `MsgDelist`, `MsgBuy`, listing expiry, burn (implicit delist), a hide of the token or its class | `MsgTransfer`, `MsgList` |
| `PENDING_TRANSFER` | `MsgTransfer` to an inbox | accept, reject, cancel, expiry | `MsgTransfer`, `MsgList`, `MsgBurn` |

Metadata edits and freezes do not depend on lock state. Buyers pin `expected_metadata_hash` instead (§12.2). Every lock has its record (`Listing` / `PendingTransfer`), and the reverse holds too (§13).

### 7.3. Identifiers

- **Class ids** are sequential `uint64`. They are not names, so there is nothing to squat. `name` and `symbol` are deliberately **not unique**: two classes may both be called "Phoenix". Clients must show the class id and the owner's `x/name` handle next to a class name, and must not treat a matching name or symbol as proof of identity (§12.6).
- **Token ids** are sequential `uint64` per class, starting at 1, never reused. The canonical reference is `<class_id>/<token_id>`.
- There are no caller-chosen string ids anywhere. That rules out collisions, Unicode confusables and injection into keys.

### 7.4. Supply Rules

- `max_supply` counts tokens **ever issued** (§3.1), so burning cannot be used to mint past an edition size.
- Lowering is one-way. `0 → N` is allowed (capping an open class). `N → 0` and `N → N+k` are not.
- `minting_closed` is one-way and stops all future mints. It is the strongest commitment an issuer can make after creation.

### 7.5. Issuer Powers (what holders must check)

| Power | Controlled by | Can it be given up later? |
|-------|---------------|---------------------------|
| Edit class metadata | `metadata_frozen` | Yes: freeze (one-way) |
| Edit token metadata | `flags.token_metadata_mutable` | Yes: freeze the class (all tokens), or freeze one token (the issuer **or the holder** can) |
| Mint more | `max_supply`, `minting_closed` | Yes: lower the cap or close minting |
| Revoke | `flags.burn_authorization` (soulbound only) | No: fixed at creation |
| Change royalty | `royalty_bps` | Only downward |
| Block transfers | `flags.transferable` | No: fixed at creation; cannot be toggled |

Issuers can never move a holder's token, list it, or stop its transfer. Clients should show "Frozen", "Closed supply", "Revocable" and (for `ISSUER`) "Cannot be discarded" badges from these fields.

### 7.6. Receive Policy Resolution

**Trusted sender.** A sender is trusted when it is either:
- an active `x/rep` member (any trust level), or
- a **council-body policy address**: the gov authority, or the policy address of one of the three councils (Commons, Technical, Ecosystem) or of one of their standing committees.

This needs a new, narrow commons helper, `IsCouncilBodyPolicy(ctx, addr) bool`, implemented as `IsCouncilOrCommitteePolicy` over each council and standing committee. It deliberately does **not** use `IsGroupPolicyAddress`, which also matches every sub-group a council registers, including groups with arbitrary members.

**Resolution.** Given a sender (transfer `sender` or mint `minter`) and a recipient:

1. The effective policy is the stored one, or `default_receive_policy` if none is stored.
2. `OPEN` → accept. `MEMBERS` → accept if the sender is trusted, otherwise inbox. `INBOX` → inbox.
3. Purchases (`MsgBuy`) and public mints (`MsgPublicMint`) always deliver directly, because the recipient is the signer.
4. A `MsgMint` of a `burn_authorization = ISSUER` class to anyone but the minter always goes to the inbox, overriding steps 1 and 2: the holder could never burn it, so it needs their consent (§3.2).

**Inbox caps.** An inbox item is refused (`ErrInboxFull`) when either cap is reached:
- `PendingPairCount[(origin, to)] ≥ max_pending_per_pair`, where origin is the sender address or `"class/<id>"` (§3.7);
- `InboxCount[to] ≥ max_inbox_per_recipient`.

Each item burns `inbox_fee` from the signer and expires after `pending_ttl`. Trusted senders never touch the inbox under `MEMBERS`, so filling a victim's inbox cap blocks only other untrusted senders. That is the intended outcome for an address under attack.

### 7.7. Sale Settlement

For a sale price `P` (an `Int` in the bond denom):

```
fee       = floor(P × sale_fee_bps / 10000)
royalty   = floor(P × min(class.royalty_bps, params.max_royalty_bps) / 10000)
proceeds  = P − fee − royalty            // ≥ 0 because the hard bounds keep max_royalty_bps + sale_fee_bps ≤ 3500
```

- `fee` → the community pool via `FundCommunityPool(ctx, fee, buyer)`, which x/split distributes. Skipped when 0.
- `royalty` → divided across `class.royalty_recipients`: each share is `floor(royalty × weight_bps / 10000)` and the remainder goes to the first share, so the shares sum to `royalty` exactly. Zero shares are skipped, and a share is paid even when its recipient is the seller.
- `proceeds` → the seller. Skipped when 0.
- Rounding dust stays with the seller. Integer math only, no `LegacyDec`.
- The royalty is clamped to the **current** `max_royalty_bps`, so governance can lower the ceiling for existing classes without a migration.
- Payments go directly from the buyer via `SendCoins`, and the module account never holds sale funds.
- If a royalty recipient has become a blocked address (impossible through this module's own validation, but checked anyway), its share is paid to the seller instead and `artifact_sold` records `royalty_redirected = true`. A sale never fails because of the issuer's routing.

**Nonce.** `listing_seq` lives on the token and only ever grows, across delist and relist too. A signed `MsgBuy` can therefore never match a later listing of the same token, even at the same price.

### 7.8. Royalties Are Market-Only

A royalty is collected only when a token is sold through `MsgBuy`. A `MsgTransfer` carries no price, so the chain cannot tell a gift from an off-chain sale, and it does not try. The spec says so openly instead of pretending to enforce it:

- Clients describe royalties as "paid on sales through the Spark Dream market".
- Off-chain sales settled with `MsgTransfer` pay no royalty.
- There is no transfer tax on NFTs, unlike DREAM's 3%. A tax on gifts and wallet moves would hit honest users, and anyone could avoid it by moving tokens without the market.

### 7.9. Metadata Rules and Media Labels

Every metadata string (class `name`, `symbol`, `description`, `uri`, `uri_hash`, `token_uri_base`; every `TokenMetadata` field; attribute keys and values; the revoke `reason`; hide `reason_text`):

1. Valid UTF-8 within its length limit, with no control characters except `\n` and `\t` in descriptions.
2. **No data URIs anywhere.** `x/common` `ContainsDataURI` (the rule in [content-scanning.md](content-scanning.md) §3.2) rejects them at validation (`ErrInlineData`), as `x/collect` does. Media is always referenced, never embedded.
3. `uri` and `token_uri_base` must be absolute URIs whose scheme is in `allowed_uri_schemes` (default `ipfs`, `ar`, `https`). Also refused: userinfo (`user@host`), non-ASCII or IP-literal hosts (so no homoglyph or internal-network hosts), and whitespace.
4. `uri_hash`, when set, is exactly 64 lowercase hex characters. Clients that fetch `uri` should verify it and warn on mismatch. Content-addressed schemes (`ipfs`, `ar`) are self-verifying.
5. Attribute keys match `[a-z0-9_]{1,max_attribute_key_length}` and are unique within a token.

**Media labels.** Class and token carry `media_flags` and `media_rules_version`, computed on every write. `MEDIA_FLAG_EXTERNAL_URI` is set when `uri` is set, or, for a class, when `token_uri_base` is set. `INLINE_DATA` can never occur, because data URIs are rejected.
- The labelling helpers are **new code**: `LabelNftClass` / `LabelNftToken`. Put them in `x/common/types/media_labels.go` beside `LabelBody`, or keep them in `x/artifact/keeper/media_labels.go` as collect keeps its private `applyItemMediaLabels`.
- NFT records belong in the **URI-only, not-withheld** category of [content-scanning.md](content-scanning.md) §3, like collect items. On-chain text is always returned; clients follow §6 (fetch media only after a clean scanner verdict).
- When implemented, add `x/artifact` `Class` and `Token` rows to the content-scanning §3.1 table.
- Do **not** add NFT kinds to `sparkdream.common.v1.ContentType`. That enum describes body *encodings* (TEXT, GZIP, IPFS…), not record kinds. Scanners identify NFT records by module and `HideTargetKind`.

### 7.10. Content License

Spark Dream content is CC0 ([content-license.md](content-license.md)), and an NFT does not change that:

- On-chain metadata (names, descriptions, attributes) is published content and is CC0, like any other record.
- **Referenced media must also be CC0 or already public domain.** `MsgCreateClass` and `MsgMint` require `accepted_content_license = "CC0-1.0"`, mirroring `MsgAcceptInvitation`. The signer affirms that the work is theirs to dedicate or is already public domain. Public mints carry no new media (they use class metadata), so they need no affirmation.
- **A token is not a copyright.** Holding `7/42` means holding the token: its provenance, its place in the class, its resale value, and whatever off-chain perks an issuer attaches. Anyone may copy, print or remix the image. Clients state this on every token page.
- No field selects a license. New content surfaces must not introduce license choices (CLAUDE.md). The `ContentLicense` query covers this module unchanged.

Content minted without the right to dedicate it is a moderation matter (§7.11), as in every other module.

### 7.11. Moderation

The hide authority, sentinel bond commitment, daily rate limit, pre-appeal self-correct window, x/rep appeal and `RoleActivity` reporting follow `x/collect` ([x-collect-spec.md](x-collect-spec.md) §3.21, §5.24–5.26). Because tokens are property, the effects differ:

| Aspect | `x/collect` | `x/artifact` |
|--------|-------------|---------|
| Effect of hiding a token | HIDDEN | `token.status = HIDDEN`; its listing is cancelled immediately |
| Effect of hiding a class | HIDDEN | `class.status = HIDDEN`; minting, mint-policy changes, listing and buying blocked for the class; its listings drained via `ClassCancelQueue` |
| Ownership | n/a | **Unchanged.** Transfer and burn stay possible |
| Queries while hidden | content withheld | Content fields blanked (class `name`, `description`, `uri`, `uri_hash`, `token_uri_base`; token `metadata`), with `withheld = true`. **A class hide withholds every token of the class too.** Ids, owner, flags, supply and status stay visible |
| Unappealed after `hide_expiry_blocks` | target **deleted**, deposits refunded | Metadata **scrubbed**: content fields set to empty in state and frozen. For a class, every token's metadata is also scrubbed via `ClassScrubQueue`. Nothing is deleted; owners keep their tokens and deposits. Status stays HIDDEN |
| x/rep verdict OVERTURNED (appellant wins) | restored | Status ACTIVE; record `OVERTURNED`. x/rep refunds the appeal bond and slashes the sentinel by the committed amount. Scrubbing happens only after an unappealed expiry, so nothing needs restoring |
| x/rep verdict UPHELD (hide stands) | deleted | Scrub immediately; record `UPHELD`. x/rep splits the appeal bond (half burned, half to the sentinel reward pool) and releases the sentinel's bond |
| x/rep appeal times out (no verdict) | restored | Restored; record `APPEAL_TIMEOUT`. x/rep refunds half the bond; artifact releases the sentinel bond without slash |
| Target burned while PENDING | (n/a) | `TARGET_BURNED`. The sentinel bond is released without slash or vindication, and the hide does not count toward accuracy in `RoleActivity`. An appeal in flight still settles its bond in x/rep, but every artifact callback becomes a no-op and reports no sentinel, so nothing moves twice |
| Author penalty | author bond slash + per-tag rep penalty | None by default. Repeat abuse goes through `x/rep` member reports |

**Class-hide drain.** Hiding a class writes `ClassCancelQueue[class_id] = 0`. Each block, the EndBlocker walks the `Listings` class prefix from the cursor, cancels up to `max_expirations_per_block` listings, and saves the cursor. When the walk reaches the end, the queue entry is deleted. `MsgBuy` and `MsgList` check the class status directly, so nothing can be sold or newly listed while the drain is running. **Unhiding the class deletes its queue entry.** Listings that were already cancelled stay cancelled; the sellers relist. `ClassScrubQueue` works the same way over the `Tokens` class prefix.

**Appeal resolution.** Hide appeals run through x/rep's moderation-appeal machinery, the same path as forum post hides and x/collect hides:
- `MsgAppealHide` calls `x/rep` `CreateGovActionAppeal(GOV_ACTION_TYPE_ARTIFACT_HIDE, "<hide_id>", appellant, reason)` and stores the returned `appeal_id`. It removes the hide from artifact's own expiry index, since x/rep now owns the deadline.
- x/rep calls back into artifact through `RepAppealTarget` ([x/artifact/keeper/appeal_target.go](../x/artifact/keeper/appeal_target.go)), registered in `app.go` with `RepKeeper.RegisterModerationAppealTarget`:
  - `GetActionSentinel` / `GetActionCommittedAmount`: the hiding sentinel and the reserved bond, so x/rep can release or slash it, exclude the sentinel from the jury, and record the `RoleActivity` verdict.
  - `ReverseSentinelAction` restores the content on OVERTURNED.
  - `OnAppealOutcome` scrubs on UPHELD, and restores plus releases the sentinel bond on TIMEOUT.
- x/rep's `MsgAppealGovAction` rejects `ARTIFACT_HIDE` and `COLLECT_HIDE`. The owning module must file these appeals, because only it can check who may appeal and mark its own record.
- `RoleActivity` action kinds `artifact_hide` and `artifact_appeal_filed` live in [x/rep/types/role_activity_kinds.go](../x/rep/types/role_activity_kinds.go). They count toward the sentinel activity gate, the appeal-rate gate, the score weights and the overturn cooldown, exactly like `collect_hide`.

### 7.12. EndBlocker

Six ordered passes. Each is capped at `max_expirations_per_block` and walks an ordered index, so each pass is O(cap):

1. **Pending expiry.** For `PendingExpiry` entries with `expires_at ≤ now`, resolve as expired (§5.3.2).
2. **Listing expiry.** For `ListingExpiry` entries with `expires_at ≤ now`, delete the listing and clear the lock.
3. **Class-hide listing drain.** Advance `ClassCancelQueue` cursors (§7.11).
4. **Pending owner expiry.** For `PendingOwnerExpiry` entries with `expires_at ≤ now`, delete the proposal.
5. **Hide expiry.** For PENDING, unappealed hide records past `hide_expiry_blocks`, scrub per §7.11. Appealed records are not in this index; x/rep owns their deadline.
6. **Class scrub drain.** Advance `ClassScrubQueue` cursors.

No pass is needed for safety. Every message checks expiry and hidden status at use, so a backlog only delays the release of locks, deposits and records. The owner of an expired item can always resolve it directly (`MsgDelist`, `MsgCancelOutgoing`, `MsgRejectIncoming`).

Each record is processed in a cached context. A failure is logged, emits `artifact_expiry_error`, and skips that record by moving its index entry to a `FailedExpiry` set for manual inspection, so it never blocks the queue. The EndBlocker never halts the chain.

### 7.13. Delegation (x/session)

There are no NFT-native approvals. To let another key act for them, a holder grants an `x/session` SessionKey scoped to specific message types.

The session ceiling (`max_allowed_msg_types`) and active list (`allowed_msg_types`) gate **both** SessionKey execution and ScheduledOneshot `Exec`; there is no separate oneshot allowlist. So anything put in the ceiling can also be scheduled to fire later. Recommended ceiling entries:

| Delegable | Message types | Why |
|-----------|---------------|-----|
| **Yes** | `MsgAcceptIncoming`, `MsgRejectIncoming`, `MsgSetReceivePolicy` | Inbox triage. These move no value out of the account. The worst a leaked key can do is accept unwanted tokens, which the holder can burn for their deposits, or reject wanted ones, which the sender can resend |
| **No** | Every other `x/artifact` message | Anything that spends SPARK, moves, prices, destroys, irreversibly changes or reroutes payment for a token or class needs the owner's key. **`MsgMint` is in this set**: each mint takes `token_deposit` from the signer and the burn refund goes to the holder, so a leaked minting key could mint to an attacker who burns the tokens and keeps the deposits |

For bulk issuance, a class owner adds a dedicated **minter key** to the class (`MsgSetMinters`). That key pays deposits from its own balance, so the exposure is limited to what it holds, and the owner can remove it at any time.

### 7.14. Council-Owned Classes

A council policy can own a class: for example, the Commons Council issuing "Season 3 Initiative Contributor" badges, created by a council proposal (§5.1.1). A proposal can only execute messages in its policy's `PolicyPermissions.allowed_messages`. Genesis bootstrap ([x/commons/keeper/genesis_bootstrap.go](../x/commons/keeper/genesis_bootstrap.go)) grants:

| Policy | Artifact messages |
|--------|-------------------|
| Commons Council | `MsgCreateClass`, `MsgMint`, `MsgRevoke`, `MsgSetMinters`, `MsgCloseMinting`, `MsgCancelOutgoing` |
| Commons Operations Committee | the same six, plus `MsgHideContent`, `MsgUnhideContent`, `MsgUpdateOperationalParams` (appeals are resolved through `x/rep` `MsgResolveGovActionAppeal`) |

`TestCouncilsCanRunArtifactBadges` pins both lists. Other councils can be granted the same set with `MsgUpdatePolicyPermissions`.

Routine issuance without a proposal per badge: the owning council passes **one** proposal executing `MsgSetMinters` to add a bot minter key, or the Operations Committee policy, and later removes it the same way. Beyond minting as a listed minter, the Operations Committee has no power over a class it does not own. In particular, only the class owner can revoke.

**Group deletion.** If `MsgDeleteGroup` removes a council body that owns classes, those classes have no one left to manage them. Their tokens remain fully owned and transferable by their holders. Before deleting a group, transfer its classes with `MsgProposeClassOwner` (the implementation should make `MsgDeleteGroup` warn when the policy owns classes). The same applies to tokens and inbox items held by the policy address.

---

## 8. Default Parameters

| Parameter | Default | Authority |
|-----------|---------|-----------|
| `min_trust_level_create_class` | `PROVISIONAL` | gov |
| `max_classes_per_creator` | 50 | gov |
| `default_receive_policy` | `MEMBERS` | gov |
| `max_name_length` / `max_symbol_length` | 128 / 16 | gov |
| `max_description_length` | 2048 | gov |
| `max_uri_length` | 512 | gov |
| `max_attributes` / key / value | 16 / 64 / 256 | gov |
| `max_minters_per_class` | 10 | gov |
| `max_batch_size` | 50 | gov |
| `allowed_uri_schemes` | `ipfs`, `ar`, `https` | gov |
| `max_royalty_bps` | 1000 (10%) | gov |
| `sale_fee_bps` | 100 (1%) | gov |
| `class_creation_fee` | 10 SPARK (burned) | ops |
| `token_deposit` | 0.1 SPARK (refundable) | ops |
| `inbox_fee` | 0.02 SPARK (burned) | ops |
| `pending_ttl` | 7 days | ops |
| `max_listing_duration` | 90 days | ops |
| `max_pending_per_pair` / `max_inbox_per_recipient` | 5 / 100 | ops |
| `max_expirations_per_block` | 200 | ops |
| `market_enabled` / `public_mint_enabled` | true / true | ops |
| `max_hides_per_sentinel_per_day` | 50 | ops |
| `sentinel_unhide_window_blocks` | ~24 h | ops |
| `hide_expiry_blocks` | ~7 days | ops |
| `sentinel_hide_commit_dream` | 100 DREAM | ops |

Block-height defaults use the chain's block-time constant, as collect does. Per the config.yml pin policy, networks pin these only when they differ from the code defaults.

**Example.** A member issuing a 100-piece edition to themselves pays 10 SPARK, burned, plus 10 SPARK in deposits, held. The 10 SPARK comes back if all 100 tokens are ever burned. Airdropping the edition to 100 non-members whose policy is `MEMBERS` costs the same, plus 2 SPARK in burned inbox fees.

---

## 9. Error Codes

Codespace `artifact`. Codes start at 1100 (1100 is the scaffolded `ErrInvalidSigner`); the names below are authoritative, the numbers are assigned in [x/artifact/types/errors.go](../x/artifact/types/errors.go).

| Code | Name | Meaning |
|------|------|---------|
| 2 | `ErrClassNotFound` | |
| 3 | `ErrTokenNotFound` | |
| 4 | `ErrNotClassOwner` | |
| 5 | `ErrNotTokenOwner` | |
| 6 | `ErrNotMinter` | |
| 7 | `ErrInsufficientTrust` | Creator not a member at the required trust level, nor a council-body policy |
| 8 | `ErrTooManyClasses` | |
| 9 | `ErrInvalidFlags` | Issuer-burnable with `transferable`, unknown `burn_authorization`, or soulbound with royalty/price |
| 10 | `ErrSupplyExceeded` | |
| 11 | `ErrSupplyIncrease` | Raising or removing a cap |
| 12 | `ErrMintingClosed` | |
| 13 | `ErrMintWindow` | Outside the window, or public mint disabled |
| 14 | `ErrPerAddressLimit` | |
| 15 | `ErrPriceExceeded` | Public-mint price above `max_price` |
| 16 | `ErrMetadataFrozen` | |
| 17 | `ErrNotTransferable` | |
| 18 | `ErrTokenLocked` | |
| 19 | `ErrInvalidRecipient` | Self (where disallowed), blocked address, or malformed |
| 20 | `ErrPendingNotFound` | |
| 21 | `ErrPendingExpired` | |
| 22 | `ErrInboxFull` | Pair or recipient cap |
| 23 | `ErrListingNotFound` | |
| 24 | `ErrListingExpired` | |
| 25 | `ErrListingChanged` | Price or nonce mismatch |
| 26 | `ErrMetadataChanged` | `expected_metadata_hash` mismatch |
| 27 | `ErrMarketDisabled` | |
| 28 | `ErrRoyaltyTooHigh` | |
| 29 | `ErrRoyaltyIncrease` | |
| 30 | `ErrInvalidDenom` | |
| 31 | `ErrDreamNotAccepted` | |
| 32 | `ErrInvalidMetadata` | Length, charset, scheme, host, hash format, duplicate attribute |
| 33 | `ErrInlineData` | Data URI in any field |
| 34 | `ErrContentLicenseNotAccepted` | |
| 35 | `ErrContentHidden` | Action not allowed on a hidden class or token |
| 36 | `ErrClassNotEmpty` | |
| 37 | `ErrNotRevocable` | `burn_authorization` does not allow the issuer |
| 38 | `ErrInvalidBatch` | Empty, too large, or duplicate entries |
| 39 | `ErrPendingOwnerNotFound` | Missing or expired |
| 40 | `ErrNotAuthorized` | Wrong authority for params or moderation |
| 41 | `ErrHideNotFound` | |
| 42 | `ErrAlreadyHidden` | |
| 43 | `ErrSentinelRateLimit` | |
| 44 | `ErrNotAppellant` | Not the token holder (token hide) or class owner (class hide) |
| 45 | `ErrAppealWindowClosed` | Already appealed, past the deadline, or already resolved |
| 46 | `ErrInvalidParams` | Includes hard-bound violations (§3.13) |
| 47 | `ErrInsufficientFunds` | |
| 48 | `ErrSentinelCooldown` | |
| 49 | `ErrInsufficientBond` | |
| 50 | `ErrInvalidReason` | Unknown moderation reason |
| 51 | `ErrHolderBurnNotAllowed` | `burn_authorization = ISSUER` |
| 52 | `ErrInvalidRoyaltySplit` | Empty, more than 10 recipients, duplicate, zero weight, or weights not summing to 10000 |

---

## 10. Events

Every state change emits an event. Attributes always include `class_id`, plus `token_id` where relevant.

| Event | Extra attributes |
|-------|------------------|
| `artifact_class_created` | `creator`, `transferable`, `burn_authorization`, `token_metadata_mutable`, `max_supply` |
| `artifact_class_updated` | `fields` |
| `artifact_class_metadata_frozen` | |
| `artifact_mint_policy_set` | `enabled`, `price`, `start_time`, `end_time`, `per_address_limit` |
| `artifact_minters_set` | `added`, `removed` |
| `artifact_max_supply_set` | `old`, `new` |
| `artifact_minting_closed` | |
| `artifact_class_owner_proposed` / `_accepted` / `_cancelled` / `_expired` | `from`, `to` |
| `artifact_class_deleted` | |
| `artifact_minted` | `minter`, `owner`, `deposit` |
| `artifact_mint_pending` | `minter`, `to`, `expires_at`, `inbox_fee` |
| `artifact_public_minted` | `buyer`, `token_ids`, `price`, `fee` |
| `artifact_transferred` | `from`, `to`, `via` (`transfer` / `accept` / `sale`) |
| `artifact_transfer_pending` | `from`, `to`, `expires_at`, `inbox_fee` |
| `artifact_pending_resolved` | `kind`, `outcome` (`accepted` / `rejected` / `cancelled` / `expired`), `refund` |
| `artifact_receive_policy_set` | `address`, `policy` |
| `artifact_burned` | `owner`, `refund` |
| `artifact_revoked` | `holder`, `reason`, `refund` |
| `artifact_token_updated` / `artifact_token_metadata_frozen` | `by`, `metadata_hash` |
| `artifact_listed` / `artifact_listing_updated` | `seller`, `price`, `expires_at`, `nonce` |
| `artifact_delisted` | `seller`, `reason` (`seller` / `expired` / `burned` / `hidden` / `sold`) |
| `artifact_sold` | `seller`, `buyer`, `price`, `fee`, `royalty`, `royalty_recipients` (`addr=amount,...`, paid shares only, after redirects), `royalty_redirected` |
| `artifact_hidden` | `hide_id`, `target_kind`, `authority`, `reason` |
| `artifact_unhidden` | `hide_id`, `authority` |
| `artifact_hide_appealed` | `hide_id`, `appellant`, `appeal_id` |
| `artifact_hide_resolved` | `hide_id`, `outcome` |
| `artifact_metadata_scrubbed` | `hide_id`, `target_kind` |
| `artifact_class_drain_progress` | `queue` (`cancel` / `scrub`), `cursor`, `done` |
| `artifact_expiry_error` | `pass`, `key`, `error` |

---

## 11. Integration Points

### 11.1. x/identity

```go
k.identityKeeper.BondDenom(ctx)   // the only accepted denom
k.identityKeeper.DreamDenom(ctx)  // explicitly rejected with ErrDreamNotAccepted
```

No `"uspark"` or `"dream"` literals anywhere in the module.

### 11.2. x/rep

```go
k.repKeeper.IsActiveMember(ctx, addr)              // trusted sender, class creation
k.repKeeper.GetTrustLevel(ctx, addr)               // class creation gate
// moderation: BondedRole(ROLE_TYPE_CONTENT_SENTINEL) bond commit/release/slash,
// RoleActivity reporting (new kinds artifact_hide, artifact_appeal_filed),
// CreateGovActionAppeal(ARTIFACT_HIDE, ...) to open an appeal; x/rep calls back via RepAppealTarget
```

Zeroing a member does **not** touch their NFTs or classes. Tokens are property, and "punish position, not person" applies. A zeroed creator's classes keep working; spam through them is handled by hiding the class. Zeroing does take away trusted-sender status, so a zeroed member's transfers to `MEMBERS` recipients go to the inbox.

Season resets do not touch NFTs.

### 11.3. x/commons

```go
k.commonsKeeper.IsCouncilBodyPolicy(ctx, addr)                                   // NEW helper (§7.6)
k.commonsKeeper.IsCouncilAuthorized(ctx, authority, "commons", "operations")     // MsgUpdateOperationalParams
k.commonsKeeper.IsCouncilPolicyOrGov(ctx, authority, "commons")                  // council hide path
```

The council-owned class permissions are in §7.14.

### 11.4. x/collect

A collect item with `reference_type = REFERENCE_TYPE_NFT` and

```
NftReference{chain_id = <this chain's id>, token_standard = "sparkdream-artifact-v1",
             contract_address = "<class_id>", token_id = "<token_id>", token_uri = ""}
```

refers to a local token. Collect **may** call `ArtifactKeeper.GetToken` when the item is added, to reject references to tokens that do not exist. Clients resolve the live owner and media through `x/artifact` instead of any stored `token_uri`. Local references to tokens burned later are left as they are, like any other dead reference.

Read-only interface exposed to other modules:

```go
type ArtifactKeeper interface {
    GetClass(ctx context.Context, classID uint64) (artifacttypes.Class, bool, error)
    GetToken(ctx context.Context, classID, tokenID uint64) (artifacttypes.Token, bool, error)
    GetOwner(ctx context.Context, classID, tokenID uint64) (sdk.AccAddress, error)
}
```

The only write entry point is `RepAppealTarget`, which x/rep calls with appeal verdicts (§7.11).

### 11.5. x/distribution

Sale fees use `FundCommunityPool`. Rep's `DistrKeeperAdapter` (app/distr_keeper_adapter.go) does **not** expose it. x/artifact follows x/service instead: it receives a distribution adapter exposing `FundCommunityPool`, either by reusing `NewServiceDistributionAdapter(app.DistrKeeper)` or a twin of it, wired in `app.go` through a `SetCrossModuleKeepers`-style setter after `depinject.Inject()`.

### 11.6. x/session

See §7.13. `x/artifact` itself never inspects session grants.

### 11.7. x/federation

None in v1. NFTs are neither federated content nor bridged. Each federated chain runs its own `x/artifact` with its own denoms, via x/identity.

---

## 12. Security Considerations

### 12.1. Approval Phishing

On other chains, one signed "approve all" lets an attacker drain every token in a wallet in a single transaction. `x/artifact` has **no approval primitive**. A token moves only through:
- a message signed by its owner;
- a purchase of a listing the owner created, at a price the owner set;
- a session key, but only for the three inbox-triage messages in the recommended ceiling, none of which moves a token out (§7.13).

A phishing page can still ask a user to sign `MsgTransfer` directly. Wallets must render NFT messages in human-readable amino JSON (the `amino.name` requirement ensures that), and clients should show "you are giving away N tokens" before signing.

### 12.2. Front-Running and Bait-and-Switch

- **Listings.** `MsgBuy` must name the exact price and the nonce. The nonce comes from a per-token monotonic counter, so a seller who changes the price or relists between the buyer's signature and inclusion makes the buy fail; the buyer is never overcharged.
- **Metadata swaps.** On a token with mutable metadata, the issuer could swap the media between a buyer's (or inbox recipient's) review and inclusion. `expected_metadata_hash` on `MsgBuy` and `MsgAcceptIncoming` pins what the user saw. Clients **must** set it whenever the token's metadata is not frozen.
- **Public mint.** `max_price` bounds what the buyer pays.
- **Atomicity.** The token moves only if every payment succeeds, and payment happens only if the token can move.

### 12.3. Airdrop Spam and Malicious Media

Unsolicited tokens carrying scam URIs ("claim your reward at…") are a common phishing channel. The defenses:
- **Inbox by default.** The default receive policy is `MEMBERS`: tokens from untrusted senders wait in the inbox, and only members (accountable through reputation) and council bodies deliver directly.
- **Spam costs the sender.** Every inbox item burns `inbox_fee` for good, and is capped per origin and per recipient.
  - Delivered tokens carry a deposit the recipient can **burn and keep**, so direct spam pays its victims.
  - Inbox spam is never refunded its fee, and minters cannot be rotated to get around the per-pair cap (§3.7).
- **Bounded issuers.** Only members at `min_trust_level_create_class`, or council bodies, can create classes, and every class burns a fee.
- **No inline media.** Data URIs are rejected; media is external and passes through the scanner verdict feed before clients fetch it.
- **Client rules.** Clients should not render URIs from inbox items, or from classes without a clean verdict, as clickable links.

### 12.4. Issuer Rug-Pulls

Metadata swaps ("your NFT is now a blank image"), surprise extra mints and revocations are bounded by §7.5:
- Every issuer power is visible on the class, and every reduction is one-way.
- Revocation exists only on non-transferable classes, so nobody can buy a revocable token. A token its holder cannot burn is only ever delivered with the holder's consent (§3.2).
- Holders can freeze their own token's metadata.
- A class handover clears the minters, resets payout routing and disables public mint (§5.1.8), so a seller cannot keep a hidden minting or revenue backdoor.

### 12.5. State Bloat and DoS

- Every live token and pending mint is backed by a refundable deposit, and every inbox item by a burned fee. Classes cost a burned fee and are capped per creator.
- Strings, batches and pages are capped.
- Expiry work is index-ordered and capped per block. It is never needed for safety, since every message checks expiry and hidden status at use.
- Hiding a class with thousands of listings or tokens is drained by cursor over subsequent blocks (§7.11), never in one block.
- Ops-tunable fields are clamped by hard bounds (§3.13). A single Operations Committee member, who can sign operational updates, cannot zero the deposit or fee, or lift the caps without limit.

### 12.6. Impersonation

Names and symbols are neither unique nor reserved, so anyone can name a class "Commons Council Badge". Mitigations:
- Clients identify a class by **id + owner**. They show the owner's `x/name` handle and, for council-body policies, the council name. A matching display name proves nothing.
- Sentinels hide impersonating classes; repeat impersonators face `x/rep` member reports.
- Councils publish their class ids (for example in a pinned forum post), and clients may ship a list of verified class ids.
- URI host rules (§7.9) rule out homoglyph and userinfo tricks in on-chain links.

### 12.7. Royalty and Fee Arithmetic

- `sale_fee_bps ≤ 1000` and `max_royalty_bps ≤ 2500` are hard bounds.
- The royalty is clamped to the current ceiling at settlement.
- All arithmetic uses `math.Int` with floor division, so `proceeds ≥ 0` and `fee + royalty + proceeds == P` exactly.
- Zero-amount sends are skipped (bank rejects empty coin sends).
- A royalty recipient that has become unpayable redirects to the seller instead of blocking the sale.

### 12.8. DREAM Isolation

DREAM is never a price, fee, deposit or payout in this module. Every `Coin` field is checked against `BondDenom`, and DREAM gets its own error so a misconfigured client fails loudly. The only DREAM involved is sentinel moderation bonds, and those live in `x/rep`.

### 12.9. Stranded Tokens and Funds

- Recipients, payout addresses and royalty recipients cannot be module (blocked) addresses.
- Class handover is two-step.
- Pending transfers and mints always resolve, by action or by expiry, and expiry is checked at use, so a pending item never stays live past its expiry.
- A seller can always delist, and a holder can always burn.
- The one gap is a council policy owner being deleted (§7.14). It is documented, and the commons implementation should warn about it.

### 12.10. Re-entrancy and Hooks

The module calls out only to bank (sends and burns), distribution (`FundCommunityPool`) and read-only rep/commons queries. None of them calls back into `x/artifact`. The only inbound write is the `x/rep` jury callback, which touches nothing but hide state. There are no transfer hooks, so a token move cannot trigger code. If hooks are ever added (§16), state must be fully written before they run, as in federation's `MsgRegisterBridge`.

### 12.11. Hidden-Content Persistence

Scrubbing removes abusive URIs and text from the *current* state that nodes serve over queries. Committed blocks still contain the original transactions, and archive nodes can replay them. This matches every other content module, and it is why clients must apply the scanner verdict feed rather than rely on hide status alone.

---

## 13. Invariants

Registered with `crisis`:

1. **Owner index:** every token has exactly one `TokensByOwner[(owner, class, id)]`, and every index entry points at a token with that owner.
2. **Supply:**
   - For every class, `supply == count(Tokens in class)` and `reserved_supply == count(PendingMints in class)`.
   - When `max_supply > 0`, `supply + burned + reserved_supply ≤ max_supply`.
3. **Ids:** every token id and pending-mint id in a class is `< next_token_id`, and no token shares its id with a pending mint.
4. **Locks:**
   - `lock == LISTED ⇔ Listing exists`, and `lock == PENDING_TRANSFER ⇔ PendingTransfer exists`.
   - The record's `seller` / `from` equals the token owner.
   - `Listing.nonce == Token.listing_seq`.
5. **Escrow:** the module account's balance in `BondDenom` ≥ Σ `Token.deposit` + Σ `PendingMint.deposit`. It is `≥`, not `==`, because anyone can send coins to a module address.
6. **Flags:** no issuer-burnable class is `transferable`, and every class has a valid royalty split (§5.1.1 step 6). No non-transferable class has a listing, a pending transfer, a non-zero royalty or a non-zero mint price.
7. **Inbox counters:** `PendingPairCount` and `InboxCount` equal the counts derived from the pending stores.
8. **No DREAM:** no listing, mint-policy price or deposit is in `DreamDenom`.
9. **Hide consistency:** every HIDDEN class or token has a hide record that is not `UNHIDDEN`/`OVERTURNED`. Every `HideByTarget` entry points at a PENDING record.

---

## 14. Genesis

`GenesisState` exports and imports:

| Field | Notes |
|-------|-------|
| `params` | Validated, including hard bounds |
| `class_seq`, `hide_seq` | Must exceed every stored id |
| `classes` | With inline minters and mint policy |
| `class_count_by_creator` | Recomputed and checked against `classes` |
| `tokens` | Owner index rebuilt on import |
| `public_mint_counts` | |
| `receive_policies` | |
| `pending_transfers`, `pending_mints` | Inbox/outbox/expiry indexes and both counters rebuilt on import |
| `listings` | Seller and expiry indexes rebuilt |
| `pending_class_owners` | Expiry index rebuilt |
| `hide_records` | `HideByTarget`/`HideExpiry` rebuilt |
| `class_cancel_queue`, `class_scrub_queue` | Cursors |

Derived indexes are not exported. `ValidateGenesis` checks every §13 invariant except escrow (balances live in bank genesis; the invariant runs after the first block). Default genesis is empty except for params. Nothing is seeded, and no config.yml pins are needed unless a network departs from the defaults.

---

## 15. Testing Requirements

- **Unit (keeper):**
  - The happy path and every error code for each message.
  - The one-way ratchets: freeze, supply, royalty, close.
  - Lock exclusivity.
  - Class handover clears minters and payout routing.
  - Receive-policy matrix: policy × trusted/untrusted sender × mint/transfer/buy/public mint × cap reached.
  - Expiry checked at use for accept, buy and owner acceptance.
  - Settlement arithmetic, table-driven over prices from 1 to 10^18 and every fee/royalty combination, asserting `fee + royalty + proceeds == P`; royalty splits sum to the royalty exactly.
  - Burn authorization: each mode × `MsgBurn`/`MsgRevoke`; an `ISSUER` mint always lands in the inbox, even from a council body.
- **Invariants:** run every §13 invariant after each unit-test mutation helper.
- **Params:** every hard bound rejected at both the gov and ops entry points; ops full replacement rejects zeroed fields.
- **Genesis:** export → import round-trip, including pending items, listings, queues, sequences and hide records. `ValidateGenesis` rejects every invariant violation.
- **Amino:** the service-walking `amino_name_test.go` covers every `Msg` type.
- **Simulation:** random mint, transfer, list, buy, burn, hide and expiry operations, with invariants checked every block.
- **E2E (`test/artifact/`):**
  - A member creates an edition, and a non-member receives it through the inbox.
  - A public mint with a mid-flight price change fails with `ErrPriceExceeded`.
  - A relist between sign and broadcast fails with `ErrListingChanged`.
  - A metadata swap before acceptance fails with `ErrMetadataChanged`.
  - A soulbound badge is issued by a council proposal and then revoked.
  - A class hide drains listings, the class owner appeals, the jury verdict lands, and the class is restored.
  - Expiry passes, polled against block time rather than wall-clock sleeps.

---

## 16. Future Considerations

- **IBC transfer (ICS-721).** This conflicts with federation's "no cross-chain tokens" rule today. If it is ever adopted:
  - Implement the public ICS-721 *protocol* clean-room.
  - Everything that crosses the wire falls under the append-only proto rule (CLAUDE.md).
  - A received class must never claim the identity of a local class.
- **Escrowed offers and auctions.** SPARK bids on a specific token or on any token in a class, escrowed in the module, with expiry and per-bidder caps.
- **Shielded ownership via x/shield.** Needs note commitments rather than an address owner field.
- **x/season achievements as soulbound tokens.** Season would become an issuer through a narrow `MintSoulbound` keeper interface.
- **Per-recipient trusted-sender allowlists**, beyond the `MEMBERS` default.
- **Spending-capped session minting.** If `x/session` gains per-grant spend caps for module-collected fees and deposits, `MsgMint` could become delegable.
- **Transfer hooks** for issuer-defined rules. Deliberately left out: hooks are where most NFT exploits live.

### 16.1. Token Designs

Candidate extensions, grouped by what they change. Every one must keep the §1.1 principles: issuer powers visible on the class, fixed or one-way; no blanket approvals; SPARK-only pricing; CC0 content. All are implemented clean-room (§1.3), from the concept, never from a reference contract. Token-bound accounts and fractionalization stay non-goals (§1.2).

**Ownership**

- **Semi-fungible tokens.** A token id held as a per-address balance instead of by a single owner, for tickets, game items and large editions where minting N identical tokens costs N records and N deposits. It needs its own ownership model, and decisions on how deposits, the inbox, listings (partial quantities), the soulbound/revocable flags and moderation apply to balances.
- **Rentals (a separate user role).** The owner grants an address a time-limited `user` right on one token (play the item, attend the event) without moving ownership. The right expires through an EndBlocker queue. While it runs the token is locked against transfer and listing, and the owner cannot revoke it early unless the grant says so; otherwise the owner could end a rental by moving the token to another address, and a renter would have no guarantee. Close in spirit to an `x/session` grant scoped to one token.
- **Nested tokens.** A token owns other tokens, e.g. a game character holding its equipment, and the whole set moves as one. Children hold no fungible value, so the §1.2 rule is untouched. Rules: a child is locked while nested (no transfer, listing or burn on its own) and the parent's holder can detach it; a sale pins the full contents (a hash over the tree in `MsgBuy`), so a seller cannot strip children before settlement; depth and children per parent are capped by params; a hide of the parent withholds the tree, and a burn or revocation of the parent detaches children to its holder instead of burning them; a soulbound child can nest only in a non-transferable parent, so nesting never makes it transferable; child deposits stay with the child.

**Lifecycle**

- **Expiring tokens.** Memberships and passes carry an `expires_at`, renewable by the holder (paying the class) or by the issuer. Whether an expired token is burned, kept as a record, or only marked expired is a class flag, fixed at creation.
- **Redeemable tokens.** A one-way `redeemed` state for tickets and vouchers, set by the class owner or a listed minter (the venue). Redemption never moves ownership. A class flag decides whether redeemed tokens stay transferable as souvenirs.
- **Burn authorization: `NEITHER`.** `HOLDER`, `ISSUER` and `HOLDER_OR_ISSUER` ship at launch (§3.2). A fourth mode in which nobody may burn needs a deposit answer, since nobody can ever reclaim it (for example, a non-refundable fee in place of the deposit). It is appended to the enum when that is decided.
- **Crafting.** Burn a set of tokens to mint one new token, under a recipe fixed on the target class (input classes and quantities). Burned deposits refund to the crafter and the new token takes a fresh deposit. Recipes can be removed but never added after the first craft, so holders of the inputs know what their tokens can become.

**Presentation**

- **Multi-asset tokens.** The issuer proposes additional media versions for a token. The holder accepts or rejects each one and picks which accepted version is displayed. The issuer can add options but never changes what the holder shows, which makes this the holder-safe form of a changing token. Proposals go through the same media validation and moderation as metadata (§7.9, §7.11), and pending proposals are capped per token.
- **Dynamic tokens.** Tokens whose displayed metadata depends on time or chain state, with no write path and no hooks:
  - *Schedules.* The minter commits a list of `(from_time, metadata)` stages at mint, and queries return the stage in effect at block time. The token's whole future is visible at purchase.
  - *Renderers.* The class names a Gno package that `x/gnovm` evaluates at query time to produce the displayed metadata. Stored state never changes. Rules: the renderer is a pure `p/` package (no persistent state, cannot import realms; `private` packages are refused); its path and source hash are fixed at class creation; its chain-state inputs are read-only native bindings added to the gnovm module (`github.com/sparkdream/gnovm`), versioned and append-only; rendered output passes the metadata rules and moderation like stored metadata; and a render that panics or runs out of gas falls back to stored metadata. Gno refuses to redeploy a public package path, so the rule a collector reads at purchase is the rule that runs. `expected_metadata_hash` still pins stored metadata only, so clients show the renderer path and hash beside the token.
- **Blind mints with reveal.** Metadata is committed by hash at mint and revealed later, so early minters cannot pick rare items. The reveal must match the commitment, and an unrevealed token past a deadline becomes burnable by the holder with a full refund of what they paid.
- **Generative tokens.** Art derived from a seed fixed at mint. A seed from block data can be predicted or ground by minters and validators, so a fair seed needs commit-reveal; there is no oracle randomness (no oracles).

**Economics**

- **Locked royalty splits.** Royalty splits ship at launch (§3.2, §7.7), but the class owner can re-split at any time, so a collaborator's share is only as safe as the owner's word. A one-way class flag could freeze the split, the same way `metadata_frozen` freezes content.
- **Curve and declining-price mints.** `MintPolicy` pricing that follows a bonding curve over supply, or a Dutch auction over time. The curve is fixed when public mint opens.
- **Partial common ownership (Harberger tokens).** The holder sets a public price, anyone may buy at that price at any time, and the holder pays a periodic tax on it to the community pool, collected through an `x/session` RecurringPull. Missing a payment opens the token to purchase at a falling price. It discourages hoarding of shared assets and funds the commons. Needs care around forced sales of soulbound or hidden tokens (both excluded) and around the market circuit breaker.

**Attestation and impact**

- **Attendance and participation badges.** Soulbound proof of presence or contribution, issued by event organizers or councils. Mostly existing primitives (soulbound, revocable, council-owned classes §7.14); the addition is cheap batch issuance.
- **Impact certificates.** Semi-fungible claims that describe a piece of work — scope, contributors, time period — which funders can hold shares of and retroactive funders reward later. The natural pairing is `x/season` retroactive public-goods funding and `x/rep` initiatives: a completed initiative issues its certificate, and retro rewards flow to its holders. Builds on semi-fungible tokens. DREAM never touches the certificate itself (§12.8).
- **Mutual attestations.** A token that exists only when both issuer and recipient sign — endorsements, co-authorship, vouching. The recipient's signature replaces the inbox, and either party may burn it. Related to x/rep invitations and accountability.

### 16.2. Rolling Out in Upgrades

None of §16.1 has to ship at launch. Each item can arrive in its own chain upgrade, under these rules:

- **Defaults preserve behaviour.** Every new class field's zero value means "exactly as before the upgrade". Existing classes keep their rules, and only classes created afterwards can opt in. An upgrade never turns on a new issuer power for a class that already exists, because holders acquired their tokens under the old rules (§1.1).
- **Additive wire changes.** New fields go on the end of their messages and new enum values on the end of their enums. After launch the dense-renumbering proto rule no longer applies to live state: a removed field is `reserved`, never renumbered.
- **Fail closed on old clients.** Where a feature adds a protection field to a message (for example a contents hash on `MsgBuy` for nested tokens), a request that leaves it empty on a token that needs it is refused, not settled.
- **Mechanics.** Bump the module's `ConsensusVersion`, register a store migration that sets defaults for any new params (the op-params full-replacement rule means e2e builders must list them too), and add an upgrade handler. The first upgrade after launch also has to add the upgrade-handler plumbing to `app/`, which does not exist yet.

**Decided at launch, so later upgrades need no migration:**

- `burn_authorization` replaced the `revocable` bool before launch, so `NEITHER` is one appended enum value rather than a second flag that every code path would have to reconcile with the first.
- `royalty_recipients` is a list from launch, so collaborative splits need no migration of every class.
- **The single-lock model stays.** `TokenLock` remains one exclusive lock per token. Rentals and nesting each add one value: a rented token cannot be listed, transferred or nested, and a nested child cannot be rented, listed or burned on its own. A rented parent can still hold nested children, since the children carry their own `NESTED` lock. No planned feature needs two locks on one token, so no migration of every token is needed.

**Build order.** Mission-aligned additive work first, the lock and market changes last. Demand can reorder items between waves, but never against a dependency.

| Wave | Items, in order | What changes |
|------|-----------------|--------------|
| 1. Additive, mission-aligned | Attendance batch issuance; mutual attestations; expiring and redeemable tokens; locked royalty splits | A class flag or new messages; no existing records rewritten. Pairs with the first upgrade, which also builds the upgrade-handler plumbing |
| 2. Minting and presentation | Curve and Dutch mints; blind mints → generative; schedules → renderers; multi-asset | Mint-policy variants, commit-reveal, query-time display. Renderers call x/gnovm, which is already wired into the app with a query-time `Eval`; they need a late-wired gnovm interface in x/artifact, a gas-capped render with fallback to stored metadata, and the pure-package and source-hash checks at class creation. Renderers that read only time and block height work with today's bindings |
| 3. New ownership model | Semi-fungible tokens → impact certificates; crafting | A parallel balance store with its own messages, not a retrofit of `Token`; certificates also need the x/season and x/rep integrations |
| 4. Locks and market | Rentals → nesting; Harberger tokens | New `TokenLock` values, a contents-hash pin on `MsgBuy`, x/session RecurringPull, forced sales |
| Any time | `NEITHER` burn mode; renderer chain-state bindings | `NEITHER` waits only on a deposit rule. Bindings that read Spark Dream state (a token's own fields, x/rep, x/season) are added in the gnovm repo one at a time, versioned and append-only |

Fixed dependencies: blind mints before generative tokens, schedules before renderers, semi-fungible tokens before impact certificates, rentals before nesting.

---

## 17. File References (planned)

| Path | Contents |
|------|----------|
| `proto/sparkdream/artifact/v1/{class,token,market,moderation,params,genesis,tx,query}.proto` | State, messages, queries |
| `x/artifact/keeper/move.go` | `moveToken` / `createToken` (§7.1) |
| `x/artifact/keeper/settlement.go` | §7.7 |
| `x/artifact/keeper/receive_policy.go` | §7.6 |
| `x/artifact/keeper/moderation.go` | Hide lifecycle, drain queues, jury callback (§7.11) |
| `x/artifact/keeper/endblock.go` | §7.12 |
| `x/artifact/keeper/invariants.go` | §13 |
| `x/artifact/keeper/msg_server_*.go` | One per message |
| `x/artifact/types/params.go` | Hard bounds (§3.13) |
| `x/artifact/types/amino_name_test.go` | Service-walking amino guard |
| `x/commons/keeper/keeper_helpers.go` | New `IsCouncilBodyPolicy` |
| `x/rep/types/role_activity_kinds.go` | New `artifact_hide`, `artifact_appeal_filed` kinds |
| `test/artifact/` | E2E scripts |

Scaffolded with `ignite scaffold module artifact --dep bank,auth -y` and `ignite scaffold message <name> ... --module artifact --signer <signer> -y`. Then late-wire `RepKeeper`, `CommonsKeeper`, `IdentityKeeper` and the distribution adapter in `app.go`.
