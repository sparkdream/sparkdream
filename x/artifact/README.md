# x/artifact

Spark Dream's NFT module: mint, hold, transfer, trade and burn non-fungible tokens ("artifacts"). The full specification is [docs/x-artifact-spec.md](../../docs/x-artifact-spec.md). This module is a clean-room implementation of that spec. The Cosmos SDK `x/nft` module is not wired in, and no code from it or from any other NFT implementation was consulted.

## Concepts

- **Class**: groups tokens issued under one set of rules. Its flags (`transferable`, `burn_authorization`, `token_metadata_mutable`) are fixed at creation. `burn_authorization` is `HOLDER` (default), `ISSUER`, `HOLDER_OR_ISSUER` or `NEITHER`; the issuer may burn only on soulbound classes, and a token its holder cannot burn (`ISSUER`, `NEITHER`) always arrives through the inbox. A `NEITHER` token is permanent, so its deposit is burned as a fee instead of held. The royalty can be split across up to 10 recipients by weight. Everything an issuer can still do to holders is visible on the class and can only be reduced: freeze metadata, lower the cap, close minting, lower the royalty.
- **Token**: `<class_id>/<token_id>`. Ids are sequential and never reused. Each live token is backed by a SPARK storage deposit, refunded to whoever burns it.
- **Receive policy**: `OPEN`, `MEMBERS` (the default) or `INBOX`. Under `MEMBERS`, tokens from non-members wait in the recipient's inbox until accepted. Inbox items are capped per origin and per recipient, burn a small fee, and expire.
- **Market**: fixed-price listings in SPARK only, with no escrow. `MsgBuy` must match the listing's exact price and nonce, and can pin the metadata hash. The sale fee goes to the community pool; the royalty is paid only on market sales.
- **Offers and auctions**: `MsgMakeOffer` escrows SPARK for one token or, with token id 0, any token of a class (capped per bidder, expiring); the holder sells with `MsgAcceptOffer`. `MsgStartAuction` `AUCTION`-locks a token for an English auction: escrowed bids (`MsgBid`, each beating the last by `min_increment_bps`), a late bid extending the end by `auction_extension`, then `MsgSettleAuction` (anyone) or the EndBlocker. Every outbid, cancelled or expired bid goes back to its bidder. Neither works on Harberger or edition classes.
- **Moderation**: content sentinels (x/rep `BondedRole`) or the Commons Operations authority hide classes or tokens. Hidden content is withheld from queries, but ownership never changes.
  - An unappealed hide ends with the metadata scrubbed from state.
  - `MsgAppealHide` (token holder, or the class owner for class and edition hides) opens an x/rep moderation appeal: a jury, the Commons Operations Committee (`x/rep` `MsgResolveGovActionAppeal`) or the timeout decides.
- **Issuance and attestation**: `MsgIssue` mints one metadata to many recipients (attendance badges). A `mutual` class makes every mint wait for the recipient's acceptance, and either party may burn it.
- **Expiry and redemption**: a class `expiry` policy gives tokens a term; `MARK` tokens are flagged expired (no listing or redemption until `MsgRenew`), `BURN` tokens are burned by the EndBlocker with the deposit refunded. `redeemable` classes let the owner or a minter `MsgRedeem` tickets without moving ownership. `MsgLockRoyalty` freezes the royalty split forever.
- **Price curves and commit-reveal**: public mints can follow a `LINEAR_SUPPLY` or `DUTCH` curve, fixed once minting opens. Blind classes commit a Merkle root over per-token metadata and reveal with `MsgReveal`; generative classes commit `SHA-256(seed)` and reveal with `MsgRevealSeed`. Prices are escrowed until reveal and refunded to holders who burn an unrevealed token after the deadline. `cmd/artifactcommit` builds commitments.
- **Presentation**: a metadata `schedule` fixed at mint, holder-accepted media versions (`MsgProposeMedia` / `MsgAcceptMedia` / `MsgSetDisplayedMedia`), and class renderers: a pure Gno package `x/gnovm` evaluates in the `Token` query, falling back to stored metadata on any error. Stored state never changes; `TokenView.displayed_metadata` says what to show and `display_source` why.
- **Editions and impact certificates**: a `TOKEN_MODEL_EDITIONS` class issues editions (`MsgCreateEdition`) whose identical units are held as balances: `MsgMintUnits`, `MsgTransferUnits`, `MsgBurnUnits`, `MsgRevokeUnits`, and partial-quantity unit listings (`MsgListUnits` / `MsgBuyUnits` / `MsgUpdateUnitListing` / `MsgDelistUnits`). One deposit per balance record, not per unit. Unit inbox items use the ref `(0, pending_id)` with the existing accept / reject / cancel messages. An edition with an `ImpactClaim` (scope, contributors, period, optionally a completed x/rep initiative issued by its assignee) is an impact certificate: `MsgFundEdition` pays SPARK to its holders pro rata and `MsgClaimEditionRewards` pays it out. DREAM never touches a certificate.
- **Crafting**: a class owner adds recipes (`MsgAddCraftRecipe`); `MsgCraft` burns the inputs (tokens or edition units) and mints the output to the crafter. No recipe can be added after the first craft.
- **Rentals**: `MsgOfferRental` / `MsgAcceptRental` grant a paid, time-limited user right without moving ownership; the token is `RENTED`-locked until `MsgEndRental` or expiry, and the owner may end early only if the offer said so.
- **Nesting**: `MsgNest` puts a token inside another (a character holding its equipment); the tree moves as one, a sale pins its contents hash (`MsgBuy.expected_contents_hash`), `MsgDetach` takes a child out.
- **Harberger tokens**: a class `harberger` policy makes every holder self-assess a price (`MsgSetHarbergerPrice`) and pay a yearly tax on it through an x/session RecurringPull granted to the artifact module; anyone may `MsgForceBuy` at the current price, which falls to zero during foreclosure (no grant after the grace period, or a failed pull).
- **Renderer state**: a renderer with `state_version = 1` exports `RenderState` and receives the token's own state (owner, lock, renter, parent, ...) and its owner's x/rep standing as a versioned JSON argument. `state_version = 2` adds the current x/season season, the owner's season level and XP, the token's direct children and its contents hash.
- **Licensing**: all content is CC0. Creating a class or minting requires `accepted_content_license = "CC0-1.0"`. A token confers no copyright.

## Wiring

`app.go` late-wires `IdentityKeeper`, `RepKeeper`, `CommonsKeeper`, a `FundCommunityPool` distribution adapter, the `GnoVMKeeper` renderer adapter (`app/gnovm_adapter.go`, which evaluates through x/gnovm's own `Query/Eval` handler so renderers deployed after node start work), the `SessionKeeper` Harberger-tax adapter (`app/artifact_session_adapter.go`) and the `SeasonKeeper` renderer-state adapter (`app/artifact_season_adapter.go`); x/season gets the artifact keeper for certificate nominations. The keeper holds them behind a shared pointer, so the AppModule's value copy sees the wiring. The EndBlocker drains expiries (inbox, listings, ownership proposals, hides, rentals, rental offers, unit listings, burning tokens, offers), the class queues (hide drains, escrow release after a seed reveal), Harberger tax pulls and ended auctions. Every pass is capped per block.

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

# Waves 3-4
sparkdreamd tx artifact create-class "Potions" CC0-1.0 --flags '{"transferable":true}' --token-model TOKEN_MODEL_EDITIONS --from alice
sparkdreamd tx artifact create-edition 6 CC0-1.0 --metadata '{"name":"healing potion"}' --max-supply 1000 --from alice
sparkdreamd tx artifact mint-units 6 1 --entries '{"recipient":"<bob>","amount":"10"}' --from alice
sparkdreamd tx artifact list-units 6 1 5 2000000uspark 86400 --from bob
sparkdreamd tx artifact buy-units 1 2 2000000uspark --from carol
sparkdreamd tx artifact create-edition 6 CC0-1.0 --metadata '{"name":"docs rewrite"}' \
  --claim '{"scope":"rewrote the handbook","initiative_id":"12"}' --from <assignee>
sparkdreamd tx artifact fund-edition 6 2 100000000uspark --from funder
sparkdreamd tx artifact claim-edition-rewards --refs '{"class_id":"6","edition_id":"2"}' --from bob
sparkdreamd tx artifact add-craft-recipe 7 CC0-1.0 --inputs '{"class_id":"3","quantity":"2"}' --output-metadata '{"name":"relic"}' --from alice
sparkdreamd tx artifact craft 7 1 --input-tokens '{"class_id":"3","token_id":"1"}' --input-tokens '{"class_id":"3","token_id":"2"}' --from bob
sparkdreamd tx artifact offer-rental 1 1 1000000uspark 86400 604800 --from bob
sparkdreamd tx artifact accept-rental 1 1 1000000uspark <nonce> --from carol
sparkdreamd tx artifact nest --child '{"class_id":"8","token_id":"1"}' --parent '{"class_id":"1","token_id":"1"}' --from bob
sparkdreamd query artifact children 1 1                         # contents_hash for MsgBuy
sparkdreamd tx artifact set-harberger-price 9 1 100000000 --grant-id <recurring-pull-id> --from bob
sparkdreamd tx artifact force-buy 9 1 100000000uspark --from carol
sparkdreamd tx session create-recurring-pull <artifact-module-addr> 1000000uspark 86400 +8760h --from bob   # the tax grant

# Offers, auctions, unit listing updates
sparkdreamd tx artifact make-offer 1 1 5000000uspark 86400 --from carol        # token offer
sparkdreamd tx artifact make-offer 1 0 4000000uspark 86400 --from carol        # collection offer
sparkdreamd tx artifact accept-offer <offer-id> 1 5000000uspark --from bob
sparkdreamd tx artifact cancel-offer <offer-id> --from carol
sparkdreamd tx artifact start-auction 1 2 1000000uspark 86400 --min-increment-bps 500 --from bob
sparkdreamd tx artifact bid <auction-id> 1000000uspark --from carol
sparkdreamd query artifact auction <auction-id>                 # min_next_bid
sparkdreamd tx artifact settle-auction <auction-id> --from anyone
sparkdreamd tx artifact update-unit-listing <listing-id> 8 1500000uspark 86400 --from bob
```

## Tests

- Unit: `go test ./x/artifact/...`
- Simulation: the module registers weighted operations, exercised by `go test ./app -run TestFullAppSimulation`
- E2E: `bash test/artifact/run_all_tests.sh`
