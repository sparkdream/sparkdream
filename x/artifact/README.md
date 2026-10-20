# x/artifact

Spark Dream's NFT module: mint, hold, transfer, trade and burn non-fungible tokens ("artifacts"). The full specification is [docs/x-artifact-spec.md](../../docs/x-artifact-spec.md). This module is a clean-room implementation of that spec. The Cosmos SDK `x/nft` module is not wired in, and no code from it or from any other NFT implementation was consulted.

## Concepts

- **Class**: groups tokens issued under one set of rules. Its flags (`transferable`, `burn_authorization`, `token_metadata_mutable`) are fixed at creation. `burn_authorization` is `HOLDER` (default), `ISSUER` or `HOLDER_OR_ISSUER`; the issuer may burn only on soulbound classes, and an `ISSUER` token (which its holder cannot burn) always arrives through the inbox. The royalty can be split across up to 10 recipients by weight. Everything an issuer can still do to holders is visible on the class and can only be reduced: freeze metadata, lower the cap, close minting, lower the royalty.
- **Token**: `<class_id>/<token_id>`. Ids are sequential and never reused. Each live token is backed by a SPARK storage deposit, refunded to whoever burns it.
- **Receive policy**: `OPEN`, `MEMBERS` (the default) or `INBOX`. Under `MEMBERS`, tokens from non-members wait in the recipient's inbox until accepted. Inbox items are capped per origin and per recipient, burn a small fee, and expire.
- **Market**: fixed-price listings in SPARK only, with no escrow. `MsgBuy` must match the listing's exact price and nonce, and can pin the metadata hash. The sale fee goes to the community pool; the royalty is paid only on market sales.
- **Moderation**: content sentinels (x/rep `BondedRole`) or the Commons Operations authority hide classes or tokens. Hidden content is withheld from queries, but ownership never changes.
  - An unappealed hide ends with the metadata scrubbed from state.
  - Appeals are resolved by the Commons Operations authority (`MsgResolveHideAppeal`). If nobody resolves one in time, the appellant wins.
- **Issuance and attestation**: `MsgIssue` mints one metadata to many recipients (attendance badges). A `mutual` class makes every mint wait for the recipient's acceptance, and either party may burn it.
- **Expiry and redemption**: a class `expiry` policy gives tokens a term; `MARK` tokens are flagged expired (no listing or redemption until `MsgRenew`), `BURN` tokens are burned by the EndBlocker with the deposit refunded. `redeemable` classes let the owner or a minter `MsgRedeem` tickets without moving ownership. `MsgLockRoyalty` freezes the royalty split forever.
- **Price curves and commit-reveal**: public mints can follow a `LINEAR_SUPPLY` or `DUTCH` curve, fixed once minting opens. Blind classes commit a Merkle root over per-token metadata and reveal with `MsgReveal`; generative classes commit `SHA-256(seed)` and reveal with `MsgRevealSeed`. Prices are escrowed until reveal and refunded to holders who burn an unrevealed token after the deadline. `cmd/artifactcommit` builds commitments.
- **Presentation**: a metadata `schedule` fixed at mint, holder-accepted media versions (`MsgProposeMedia` / `MsgAcceptMedia` / `MsgSetDisplayedMedia`), and class renderers: a pure Gno package `x/gnovm` evaluates in the `Token` query, falling back to stored metadata on any error. Stored state never changes; `TokenView.displayed_metadata` says what to show and `display_source` why.
- **Licensing**: all content is CC0. Creating a class or minting requires `accepted_content_license = "CC0-1.0"`. A token confers no copyright.

## Wiring

`app.go` late-wires `IdentityKeeper`, `RepKeeper`, `CommonsKeeper`, a `FundCommunityPool` distribution adapter and the `GnoVMKeeper` renderer adapter (`app/gnovm_adapter.go`). The keeper holds them behind a shared pointer, so the AppModule's value copy sees the wiring. The EndBlocker drains expiries (inbox, listings, ownership proposals, hides, burning tokens) and the class queues (hide drains, escrow release after a seed reveal). Every pass is capped per block.

## CLI examples

```bash
sparkdreamd tx artifact create-class "Phoenix Editions" CC0-1.0 --symbol PHX \
  --flags '{"transferable":true}' --royalty-bps 500 \
  --royalty-recipients '{"address":"<alice>","weight_bps":7000}' \
  --royalty-recipients '{"address":"<carol>","weight_bps":3000}' --from alice
sparkdreamd tx artifact mint 1 CC0-1.0 \
  --entries '{"recipient":"<addr>","metadata":{"name":"aurora","uri":"ipfs://bafy..."}}' --from alice
sparkdreamd tx artifact list 1 1 10000000uspark 86400 --from bob
sparkdreamd tx artifact buy 1 1 10000000uspark 1 --from carol
sparkdreamd query artifact tokens-by-owner <addr>
sparkdreamd query artifact inbox <addr>

# Wave 1-2
sparkdreamd tx artifact issue 1 CC0-1.0 --recipients <a> --recipients <b> --metadata '{"name":"attended"}' --from alice
sparkdreamd tx artifact create-class "Pass" CC0-1.0 --flags '{"transferable":true}' \
  --expiry '{"mode":"EXPIRY_MODE_MARK","term":"2592000","renewal_price":{"denom":"uspark","amount":"1000000"}}' --from alice
sparkdreamd tx artifact renew 2 1 --max-price 1000000uspark --from bob
sparkdreamd tx artifact redeem 3 --token-ids 1 --from venue
sparkdreamd tx artifact lock-royalty 1 --from alice
go run ./cmd/artifactcommit tree <next-class-id> entries.json      # root + proofs for a blind mint
sparkdreamd tx artifact reveal 4 1 <salt> --metadata '{"name":"#1"}' --proof <h1> --proof <h2> --from alice
sparkdreamd tx artifact reveal-seed 5 <hex-seed> --from alice
go run ./cmd/artifactcommit source-hash ./pkg                      # renderer source hash
sparkdreamd tx artifact create-class "Rendered" CC0-1.0 --flags '{"transferable":true}' \
  --renderer '{"pkg_path":"gno.land/p/demo/aurorarender","source_hash":"<hash>"}' --from alice
sparkdreamd tx artifact propose-media 1 1 --metadata '{"name":"remaster"}' --from alice
sparkdreamd tx artifact accept-media 1 1 1 --display --from bob
```

## Tests

- Unit: `go test ./x/artifact/...`
- Simulation: the module registers weighted operations, exercised by `go test ./app -run TestFullAppSimulation`
- E2E: `bash test/artifact/run_all_tests.sh`
