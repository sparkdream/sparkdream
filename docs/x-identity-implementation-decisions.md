# x/identity — implementation decisions and deviations

Companion log to [docs/x-identity-spec.md](x-identity-spec.md). Captures the
notable judgment calls made during the initial implementation, deviations
from the spec, and follow-up items that fell outside the scope of the first
commit. Read top-to-bottom; entries are ordered by importance, not by
section number.

## 1. Additive deployment: permissive when identity is absent (v1 DEVIATION)

**Decision.** When `app_state.identity` is missing from genesis (or present
but with a zero-valued `ChainIdentity`), both the sentinel-rewrite hook and
the keeper's `InitGenesis` become no-ops. The chain starts cleanly without
any identity state, queries return `NotFound`, and no SDK params are
rewritten.

**Reason.** The current `config.yml` uses bare `uspark` / `dream` denoms in
~59 source-level literals and 247 test scripts (per spec §13). The spec
rejects these as denoms (the strict regex requires the `u<sym>.<chain>`
shape). Shipping the strict spec interpretation would break every running
chain instance and require the full migration to land in the same commit,
which is not realistic.

**Effect.** Existing deployments keep working with hardcoded literals.
Federated deployments opt in via `sparkdreamd genesis identity init`, which
both populates `app_state.identity` and wires sentinels into the SDK module
params. The strict regex and sealed-identity invariant remain in force the
moment any identity is provided.

**Follow-up.** Once the chain has migrated all 59 hardcoded literals to
`identityKeeper.GetBondDenom(ctx)` / `GetDreamDenom(ctx)` calls and the 247
test scripts to `$BOND_DENOM` / `$DREAM_DENOM` shell vars (spec §13), this
permissive behavior should be reverted: missing-identity should be a hard
genesis error so federated chains cannot accidentally launch without one.

**Spec sections affected.** §6.2 (InitGenesis), §7.2-§7.3 (sentinel rewrite),
§13 (migration).

## 2. `sdk.DefaultBondDenom = "uspark"` left in place (v1 DEVIATION)

**Decision.** The `init()` assignment at [app/app.go:152](app/app.go#L152)
is left untouched in v1.

**Reason.** The spec (§14.5) prescribes deleting this line and replacing
every `sdk.DefaultBondDenom` read with a keeper call. That migration touches
many call sites across SDK and project code, and without the full migration
in place, removing the default would cause every test fixture that depends
on the global to break.

**Follow-up.** When the migration in §13 lands, delete the init() assignment
and add the CI grep rule that fails on any `sdk.DefaultBondDenom` reference
under `x/`, `app/`, `cmd/`.

## 3. Bank-keeper guard + x/guardian authority gate

**Decision.** Native-denom metadata immutability is enforced by three
coordinated mechanisms: (a) `BankKeeperWithIdentityGuard` wrapping
`app.BankKeeper` post-depinject; (b) the new `x/guardian` module that
owns bank/mint/staking module authorities and field-filters
authority-routed msgs; (c) invariant 4 as defense-in-depth backstop.

**Important caveat re: bank.MsgSetDenomMetadata.** Investigation
during implementation revealed that cosmos-sdk v0.53.6's bank module
does NOT expose `MsgSetDenomMetadata` as a gov-routable msg (no rpc
in `bank/v1beta1/tx.proto`). The "gov-routed metadata tamper attack
vector" the spec originally worried about does not exist in this SDK
version. The wrapper handles direct app-layer callers; invariant 4
provides backstop coverage if a future SDK release adds a gov-routable
metadata writer.

**`depinject.Provide` for the bank guard.** The spec's preferred
approach (route every consumer through the wrapper via depinject) is
**not implementable without forking x/bank**.

**Reason this is structural, not just deferred work.** Upstream
cosmos-sdk x/bank emits `keeper.BaseKeeper` as its sole `ModuleOutputs`
field. Consumers (x/gov, x/distribution, x/slashing, every Spark Dream
chain module) declare their bank input as an interface like
`govtypes.BankKeeper`. Depinject resolves the interface input by finding
ANY emitted value satisfying it. The `BaseKeeper` satisfies the interface.

If we added a second provider emitting `BankKeeperWithIdentityGuard`
(which also satisfies the interface via the embedded `bankkeeper.Keeper`),
depinject would see two candidate satisfiers and **error with an
ambiguity panic at app construction**. There is no built-in mechanism to
prefer one provider over another by satisfaction precedence; depinject's
`ProvideInModule` scopes providers to specific modules but cannot
retroactively scope a provider to "every consumer except bank itself".

The only path to make `Provide` work would require:
- Forking x/bank to emit a marker type (e.g., `RawBankKeeper`) that
  consumers do NOT see, then have our provider transform raw -> guarded.
- Rewriting every consumer's `ModuleInputs.BankKeeper` declaration to use
  a project-specific interface type.

Both options carry maintenance debt (upstream drift, every cosmos-sdk
upgrade requires re-applying the fork) that exceeds the security benefit.
Decision: **do not pursue**.

**Effect.** The guard catches `SetDenomMetaData` calls made through
`app.BankKeeper`: direct app-layer callers (ante decorators, identity's
own seed write, future Spark Dream modules wired via the project's manual
`SetBankKeeper` pattern), but NOT calls that upstream SDK modules make
through their own depinject-injected bank reference.

**Defense in depth (this is the actual steady-state guarantee).**
Invariant 4 (§16, `BankMetadataCanonicalInvariant`) catches drift on the
same block, regardless of which writer escaped the guard. **Post-C1 fix
(AppModule value-copy bug), invariant 4 actually runs every block** via
the crisis module BeginBlocker, so the gov-routed attack window is at
most one block. For v1, this is the security claim the spec actually
delivers; the spec's "first line of defense" framing in §14.6 should be
read as "the wrapper plus the invariant, working together".

**Spec deviation.** Spec §14.6 should be amended to reflect that the
`Provide` indirection is impractical and that invariant 4 carries the
load. Tracked as a spec edit, not an implementation gap.

## 4. Sentinel rewrite combined with the existing InitChainer

**Decision.** The InitChainer set at the end of `app.New` does both jobs in
one closure: it runs the sentinel rewrite first, then the
`UpgradeKeeper.SetModuleVersionMap` + `app.App.InitChainer` flow.

**Reason.** The spec (§7.3) suggests capturing the original InitChainer via
`app.InitChainer()` (a getter on baseapp). That getter does not exist on
`*baseapp.BaseApp` in cosmos-sdk v0.53.6 (verified: the `initChainer` field
is private and only `SetInitChainer` is exposed). Wrapping by capture is
therefore not implementable without a fork.

**Effect.** The combined closure has the same observable behavior: the
sentinel rewrite happens once, before `runtime.App.InitChainer` runs the
ordered `InitGenesis` chain.

## 5. Invariant 5 (`SDKParamsAlignedInvariant`): CLOSED in second-pass

**Status.** Originally skipped in v1; closed in the second-pass cleanup
(see §16). Staking and mint readers now live as fields on the Keeper
struct (`stakingKeeper types.StakingKeeper`, `mintKeeper types.MintKeeper`)
with write-once `Set*Keeper` setters. The interfaces moved from
`keeper/invariants.go` to `types/expected_keepers.go` and match upstream
`context.Context` signatures.

**Wiring.** `app.go` after depinject:

```go
app.IdentityKeeper.SetStakingKeeper(app.StakingKeeper)
app.IdentityKeeper.SetMintKeeper(NewMintKeeperAdapter(app.MintKeeper))
```

Mint goes through `MintKeeperAdapter` (in `app/bank_guard.go`) because
upstream mint exposes params via `Params collections.Item[types.Params]`
rather than a `GetParams` method.

**Effect.** Invariant 5 now runs in production wiring as designed: warns
(stop=false) on governance-update drift of staking.bond_denom / mint.mint_denom
away from the sealed identity. Returns clean no-op when staking/mint
keepers were never wired (test harnesses).

## 6. `bond_display_symbol` regex: 3-8 chars, leading letter required

**Decision.** The regex `^[A-Z][A-Z0-9]{2,7}$` requires a leading letter and
2-7 additional alphanumeric characters, for a total of 3-8 characters. This
matches the spec text (§11 table: "3-8 uppercase ASCII letters/digits;
must start with a letter") exactly.

**Test coverage.** `TestValidateRejectsBadDisplaySymbols` in
[types_test.go](../x/identity/types/types_test.go) exercises both the
under-min (`PS`) and over-max (`PSPKABCDX`) cases and the leading-digit
rejection. Length-3 symbols like `PSP` are accepted by the regex; the
happy-path fixtures use 4-character symbols (PSPK, PDRM) but the regex
itself is tested across the full 3-8 range.

## 7. Empty `Msg` surface: `codec.RegisterInterfaces` is a no-op

**Decision.** `types/codec.go` registers no implementations. The codec call
site remains in `module.go` for symmetry with other modules, so that a
future `MsgUpdateNonCanonicalMetadata` (spec §18) can be added without
changing the registration plumbing.

**Effect.** No `tx identity` subcommand exists; autocli generates only the
query namespace.

## 8. Storage scheme: collections with byte-prefix keys, no schema collision

**Decision.** `IdentityKey = collections.NewPrefix(0x00)` and
`SealedIdentityKey = collections.NewPrefix(0x01)`. Both are
`collections.Item[ChainIdentity]` under distinct prefixes.

**Reason.** Identity's storage is two singletons; the collections framework
is overkill for a single Item but matches the project's existing pattern
and gives us free serialization, type safety, and store-decoder support.
Using two distinct prefixes guarantees the invariant comparison
(`identity.EqualCanonical(sealed)`) is between independent storage entries
per spec §3.4 / §5.

## 9. CLI: genesis-edit `--force --i-mean-it` rejects single-flag
   confirmation

**Decision.** `sparkdreamd genesis identity init` against an existing
identity requires BOTH `--force` AND `--i-mean-it` to overwrite. Neither is
aliased to `-f` or `-y`.

**Reason.** Identity rebranding is destructive (every wallet, every IBC
voucher hash, every signed tx history is affected). The spec (§15) wants
"accidental confirmation prompts" to be impossible. Two distinct flags
prevent muscle-memory `-f -y` shortcuts from triggering the destructive
path; a deliberate operator types both phrases.

**Effect.** Operators see a clear "pass --force --i-mean-it" rejection on
the first attempt, with a stderr warning explaining the chain-restart
implications.

## 10. Test plan coverage and gaps

The implementation covers spec §19's named tests in spirit if not by exact
name (Go test names are slightly different to flow naturally with table
patterns). Verified by direct read-through:

| Spec test                                          | Implemented as                                  |
|----------------------------------------------------|-------------------------------------------------|
| ChainIdentity validation                           | `TestChainIdentityValidateRejectsBadDenoms`, `TestValidateRejectsWhitespaceAndNonASCII`, `TestValidateRejectsCollisions`, etc. |
| Chain-ID consistency check                         | `TestValidateAgainstChainID`                    |
| Sentinel rewrite                                   | `TestRewriteSentinelsHappyPath`, `TestRewriteSentinelsHaltsOnGenutilSentinel`, `TestPurgeLegacyBankMetadataRemovesOnlyLegacy` |
| Sealing semantics                                  | `TestSealGenesisIdempotentOnRestoration`, `TestSealGenesisRejectsTamperedReimport` |
| Bank guard                                         | `TestWrapperRejectsNativeSymbolTamper`, `TestWrapperAllowsDescriptionEditOnNativeDenom`, `TestWrapperPassesThroughPreSeal`, `TestWrapperAllowsForeignDenom` |
| Invariants                                         | `TestInvariantsPassAtFreshGenesis`, `TestInvariantInitializedFailsPreGenesis`, `TestInvariantSDKParamsDriftIsWarningGrade` |
| Init genesis ordering (identity at index 2)        | Verified by reading app_config.go directly; no automated test |
| E2E federated chain start                          | `test/identity/federated_chain_test.sh` (uses `ENABLE_CHAIN_START=true` for full run; otherwise validates genesis-level changes only) |

**Gaps acknowledged.**

- `TestGovProposalCannotAlterIdentityDenomSymbol` — the spec's load-bearing
  bank-guard test. Not implemented in v1 because it requires the
  depinject-Provide wiring (decision #3 above) for the gov-routed path to
  reach the wrapper. The keeper-level wrapper test (`TestWrapperRejects*`)
  exercises the panic path directly.
- `TestPeerActivationRegistersIBCDenomMetadata` — federation peer identity
  extension (spec §9) is deferred to a separate commit when the federation
  module is touched.
- `TestNoSDKDefaultBondDenomReads` / `TestAllDefaultGenesisUseSentinels` —
  CI grep tests for the migration. Not added in v1 because the migration
  itself hasn't been done yet (decision #1).

## 11. Federation peer identity extension (spec §9): CLOSED in second-pass

**Status.** Originally deferred in v1; closed in the second-pass cleanup
(see §16). Proto field 12 on `federation.v1.Peer` and proto field 8 on
`MsgRegisterPeer` both now carry `sparkdream.identity.v1.ChainIdentity
peer_identity`. The `RegisterPeer` handler persists it and pre-registers
IBC voucher metadata for `PEER_TYPE_SPARK_DREAM` peers via
`preRegisterIBCVoucherMetadata`.

**IBC denom computation.** Uses ibc-go v10's `Denom{Trace: []Hop{...}}`
API (spec §9.2). Single-hop only; multi-hop deferred to spec §18.

**Failure handling.** Metadata pre-registration failures emit a
`federation_peer_metadata_skipped` event but do not fail peer
registration. Metadata is informational; the peer record is the source
of truth for federation logic.

**Bank keeper interface.** Federation's `types.BankKeeper` extended with
`SetDenomMetaData` / `GetDenomMetaData`. The wrapped `app.BankKeeper`
already implements these (it's a `bankkeeper.Keeper`); the federation
mock bank keeper in tests was updated to match.

## 12. CLI `genesis identity init` ergonomics

**Decisions.**

- Both bond_display_name and dream_display_name are auto-derived from the
  chain name (e.g., `Phoenix Spark`, `Phoenix Dream`). No flags expose
  these; modify the genesis JSON by hand if needed for v1.
- Decimals default to 6 (Cosmos convention). Non-6 values require an
  explicit `--confirm-non-default-decimals` flag per spec §14.7.
- `founded_at` defaults to current Unix time; can be overridden with
  `--founded-at <unix>` for reproducible testnet genesis builds.
- `--bond-denom` and `--dream-denom` are derivable from `chain-name` and
  `ticker-prefix` but can be overridden explicitly.

**Tested.** `test/identity/genesis_cli_test.sh` exercises overwrite refusal,
non-default decimals refusal, and the derived-denom happy path against an
isolated `--home`.

## 13. Order of writes in `InitGenesis`: identity first, bank metadata last

**Decision.** Within `keeper.InitGenesis`:

1. Validate
2. `setChainIdentity(...)` — mutable record
3. `sealGenesisIdentity(...)` — sealed record
4. `registerNativeDenomMetadata(...)` — bank seed

**Reason.** The sealed record must exist before the bank-keeper guard can
distinguish "pre-seal pass-through" from "post-seal canonical-fields
enforcement." If we wrote bank metadata first, the guard would treat the
write as pre-seal (correct), but the steady-state invariant would briefly
see the mismatch between the (just-set) mutable identity and the
(not-yet-set) sealed record.

**Verified by `TestInitGenesisSealedMatchesMetadataWrite`** which seals
first then re-reads bank metadata to confirm Symbol/Display/Base match the
sealed canonical fields byte-for-byte.

## 14. `Module` proto Authority field unused

**Decision.** The `Module` proto generated by ignite includes an
`authority` field. x/identity ignores it: there is no authority because
there are no `MsgUpdateParams`-style governance hooks.

**Effect.** Setting `Authority` in app_config has no behavioral effect on
x/identity.

## 15. Follow-up cleanup applied (post initial review)

After the initial review, the following items from the review's "must fix
before merge" list were addressed in-place:

- **C1 (AppModule value-copy):** `AppModule.keeper` is now `*keeper.Keeper`,
  `ProvideModule` returns `*keeper.Keeper`, and the invariant constructors
  take `*Keeper` so closures read live keeper fields on every invocation. A
  late-bound bank keeper set via `Keeper.SetBankKeeper` from app.go is now
  visible to invariants 3-4 on the very next block. Matches the
  AppModule-Value-Copy pattern documented in [development-conventions.md](development-conventions.md).
- **C2 (bond_denom derivation):** CLI now derives
  `bond_denom = "u" + lowercase(--bond-symbol) + "." + lowercase(--chain-name)`,
  matching the spec §3.1 example pattern (`PSPK` -> `upspk.phoenix`). The
  bond_denom regex was relaxed from `{2,4}` to `{2,5}` so all bond-symbols
  of length 3-5 derive cleanly; longer symbols (6-8) require an explicit
  `--bond-denom`. Test fixtures, federation/IBC examples, and the
  `usim.simchain` simulation genesis all updated to follow the new
  convention.
- **H1 (require bankKeeper for non-empty identity InitGenesis):** A
  non-empty identity in InitGenesis now errors if `k.bankKeeper == nil`.
  Defends against accidental future `app.go` regressions that drop the
  `SetBankKeeper` call. The `if k.bankKeeper == nil` shortcut in
  `registerNativeDenomMetadata` is removed.
- **H2 (atomic genesis writes):** `saveAppState` now writes via
  `os.CreateTemp` + fsync + rename so a crashed `genesis identity init`
  cannot corrupt `genesis.json`. Original file mode is preserved.
- **H4 (SetBankKeeper write-once):** `SetBankKeeper` now panics on second
  call. Prevents accidental swap of the guarded keeper for the raw one
  mid-flight.
- **M2 (chainIDBase coverage):** `TestChainIDBase` table-tests the regex
  against the seven canonical Cosmos chain-id shapes (phoenix-1,
  aurora-mainnet-1, cosmoshub-4, osmosis-test-5, dydx-mainnet-1, noble,
  akashnet).
- **M4 (empty chain_id rejection):** `ValidateAgainstChainID` now rejects
  empty chain_id explicitly. Previously passed silently because
  `strings.Contains(x, "")` always returns true.
- **L1 (--allow-chain-id-mismatch on init):** The flag is now accepted by
  `genesis identity init`, parity with `genesis identity validate`.

## 16. x/guardian: generic authority gate (third-pass addition)

**Status.** Built in the third-pass cleanup as the prevention mechanism
for gov-routed authority msgs that the bank-keeper wrapper alone could
not catch. Resolves the "depinject Provide not feasible" gap from
decision #3 by going through a different vector: instead of trying to
make every consumer see the wrapped bank keeper, we move the bank/mint/
staking authority to a module we control and filter msgs before they
reach the target.

**Architecture.** A small (~250 LOC) module at [x/guardian/](../x/guardian/)
with one universal `MsgExec(authority, inner Any)` wrapper. Gov calls
`MsgExec` with the inner msg packed as Any; guardian's switch-statement
allowlist + per-msg-type filter validates field constraints; guardian
overwrites the inner Authority with its own module address; routes via
`msgServiceRouter.Handler(inner)` to the target module's handler.

**v1 allowlist + filters.**
- `bank.MsgSetSendEnabled`: no field filter (gov retains full control;
  legitimate emergency freezes).
- `bank.MsgUpdateParams`: no field filter.
- `mint.MsgUpdateParams`: filter rejects changes to `inflation_min`,
  `inflation_max`, `goal_bonded`, `inflation_rate_change`. Replaces
  the burn-address authority pattern that previously locked these
  fields (decision moves from app_config.go comments to reviewable
  code in `x/guardian/keeper/msg_server.go filterMintUpdateParams`).
- `staking.MsgUpdateParams`: filter rejects changes to `bond_denom`.
  This was previously only warning-grade (identity invariant 5);
  guardian promotes it to hard reject at msg layer.
- `bank.MsgSetDenomMetadata`: NOT in the allowlist because cosmos-sdk
  v0.53.6 does not expose it as a gov-routable msg. If a future
  release adds it, register the filter and add the case to the
  switch.
- Any msg type not in the allowlist is rejected with
  `ErrInnerMsgNotAllowed`.

**Wiring.**
- `app_config.go`: bank, mint, staking module configs all set
  `Authority: guardianAuthority` (where `guardianAuthority =
  authtypes.NewModuleAddress(guardianmoduletypes.ModuleName).String()`).
- `app.go` post-depinject: `app.GuardianKeeper.SetIdentityKeeper(...)`,
  `SetMintKeeper(NewMintKeeperAdapter(app.MintKeeper))`,
  `SetStakingKeeper(app.StakingKeeper)`.
- Identity is one-way dependency (guardian -> identity); no cycle.
- Mint goes through the existing `MintKeeperAdapter` (in
  `app/bank_guard.go`).
- Staking matches the guardian interface directly (no adapter).

**Why not x/circuit.** x/circuit is ante-handler based; gov-routed msgs
bypass ante. x/circuit doesn't actually solve the gov-route attack
vector. Confirmed via inspection of upstream code.

**Tests.** Per-filter rejection tests in
`x/guardian/keeper/msg_server_test.go`: rejects wrong authority,
rejects unknown inner msg, rejects each of the four immutable mint
fields independently, rejects staking bond_denom change. Positive
path (routing succeeds when no immutable field is touched) requires
the full app fixture and is deferred to e2e tests.

**Trade-offs accepted.**
- Gov proposals that touch bank/mint/staking params must be wrapped in
  `MsgExec`. Operators construct these via gov-proposal-creation
  tooling; the spec doesn't ship a CLI wrapper for it. Acceptable
  because authority-required mutations are rare.
- Authority addresses for bank/mint/staking are module accounts
  (guardian's) rather than gov's. Any third-party tooling that hardcodes
  "the bank authority is the gov address" breaks. None such found in
  this project.

## 18. Fourth-pass: source-code denom-literal migration (consumer modules)

**Status.** Migrated 8 consumer modules to read from x/identity at runtime
instead of using hardcoded `"uspark"` / `"dream"` / `"udream"` literals.

**Modules migrated.**
- `x/rep`: 30+ literal usages of `types.RewardDenom` and
  `types.TagBudgetFeeDenom` replaced with `k.BondDenom(ctx)` helper. New
  `DreamDenom` const added as fallback.
- `x/forum`: 4 keeper files; `types.DefaultFeeDenom` runtime usage
  replaced with `k.BondDenom(ctx)`.
- `x/commons`: 4 keeper files; 5 literals replaced.
- `x/futarchy`: 1 keeper file (`msg_server_create_market.go`).
- `x/collect`: `helpers.go` (3 callsites).
- `x/shield`: `begin_block.go` (3 callsites), `query_shield.go` (1
  callsite using `q.k.BondDenom(ctx)` since the query server holds a
  pointer to the keeper).
- `x/session`: 7 keeper files; 14 literals (both `"uspark"` and
  `"dream"`) replaced.

**Modules NOT migrated.**
- `x/name`: runtime is already param-driven (reads from
  `params.RegistrationFee`); literals only appear in
  `DefaultRegistrationFee` which is the genesis fallback. Federated
  chains must override the param at genesis. The sentinel-rewrite
  pattern doesn't work here because `sdk.Coin.IsValid()` rejects
  sentinel-form denoms during `Validate()`, and that runs before the
  rewrite hook executes.
- `x/federation`, `x/service`: literals are in genesis defaults only
  (no runtime keeper code uses them). Federated chains override at
  genesis. Same constraint as x/name.

**Helper pattern.** Each consumer keeper has a `BondDenom(ctx)` (and
where relevant `DreamDenom(ctx)`) helper that:
1. Panics if `identityKeeper == nil` — wiring is mandatory, missing
   wiring is a programmer error, not a recovery path.
2. Returns `identityKeeper.BondDenom(ctx)` — which itself panics if
   identity isn't initialized in genesis.

There is no fallback to a hardcoded literal. The pre-mainnet directive
(`docs/x-identity-implementation-decisions.md` §17, project memory
`project_premainnet_status.md`) explicitly forbids fallback paths that
re-introduce the mixed-state class of bug the identity module was
created to remove.

**Identity is mandatory.** Earlier drafts allowed "permissive missing-
identity" mode (consumer helpers fell back to `"uspark"` / `"dream"`
literals) so legacy single-chain operation kept working without
`genesis identity init`. That mode was deleted along with the
`noidentity` build tag and the `BondDenomSafe` / `DreamDenomSafe`
non-panicking accessors. Identity initialization happens automatically
via the build-tag-baked `DefaultGenesis()` (see §17a below), so every
chain — single-chain or federated — starts with a populated identity.

**`sdk.DefaultBondDenom` tracks identity.** Set at `init()` time in
[app/app.go](app/app.go) to `identitytypes.DefaultChainIdentity().BondDenom`,
so SDK-level fallback denoms (used by gentx tooling, staking InitGenesis
hints) automatically match whichever identity is baked in by the
current build tag (mainnet / testnet / devnet).

**Chain behavior post-migration.**
- `sparkdreamd init` writes a genesis whose `app_state.identity.identity`
  is populated from the build-tag default. Bank denom_metadata is seeded
  by identity's InitGenesis; staking/mint/gov authorities go through
  guardian; identity invariants enforce immutability.
- All consumer modules' `BondDenom(ctx)` / `DreamDenom(ctx)` resolve to
  the chain's denom (`uspark.sparkdream` for mainnet,
  `uspark.sparkdreamtest` for testnet, `uspark.sparkdreamdev` for devnet).
- IBC vouchers from peer chains are source-tagged (`uspark.<peer>`).
- Operators wanting an identity OTHER than the build-tag default can
  override at genesis-construction time with
  `sparkdreamd genesis identity init --chain-name X --bond-symbol Y ...`
  (still sealed at chain start; still immutable thereafter).

**Verification.** Full `go test ./...` clean, `go vet ./...` clean.
Blog e2e suite (10 test files, ~16 min) green end-to-end against a
freshly-built mainnet-identity chain.

## 17a. Build-tag default identity (fifth-pass addition)

**Status.** `DefaultGenesis()` now returns a real chain identity baked into
the binary via build tag, replacing the empty placeholder. `sparkdreamd
init` produces a working federated genesis with no manual `genesis
identity init` step.

**Files added.**
- `x/identity/types/default_identity.go` (`//go:build !testnet`): the
  canonical Sparkdream mainnet chain identity:
  - `chain_human_name`: `Sparkdream`
  - `chain_ticker_prefix`: `SDR`
  - `bond_denom`: `uspark.sparkdream`
  - `bond_display_symbol` / `bond_display_name`: `SPARK` / `Sparkdream Spark`
  - `dream_denom`: `udream.sparkdream`
  - `dream_display_symbol` / `dream_display_name`: `DREAM` / `Sparkdream Dream`
  - `bond_display_decimals` / `dream_display_decimals`: 6
  - `founded_at`: 1735689600 (2025-01-01 UTC, deterministic across builds)
- `x/identity/types/default_identity_testnet.go` (`//go:build testnet`):
  the testnet variant (for chain-id `sparkdream-test-1`):
  - `chain_human_name`: `SparkdreamTest`
  - `chain_ticker_prefix`: `SDT`
  - `bond_denom`: `uspark.sparkdreamtest`
  - `bond_display_symbol` / `bond_display_name`: `SPARK` / `Sparkdream Test Spark`
  - `dream_denom`: `udream.sparkdreamtest`
  - `dream_display_symbol` / `dream_display_name`: `DREAM` / `Sparkdream Test Dream`
  - same decimals + `founded_at` as mainnet

**Defaults selection.** Two build modes:

| Build command | Default identity | Use case |
|---|---|---|
| `ignite chain build` (no tags) | `uspark.sparkdream` / `udream.sparkdream` | mainnet |
| `go build -tags testnet ./...` | `uspark.sparkdreamtest` / `udream.sparkdreamtest` | testnet (`sparkdream-test-1`) |

The previously-existing `noidentity` tag (empty identity for legacy
bash tests that hardcoded `uspark`/`dream`) was deleted once every
consumer module was migrated to read its denom from identity at
runtime and all bash test scripts switched to `$BOND_DENOM` /
`$DREAM_DENOM`. Identity is now mandatory — `BondDenom(ctx)` panics
if called before genesis.

**Testnet setup recipe.** To bring up a fresh testnet with the testnet
identity baked in:

```bash
# 1. Build with the testnet tag (overwrites $GOPATH/bin/sparkdreamd)
go install -tags testnet ./...

# 2. Init genesis with the testnet chain-id
sparkdreamd init my-validator --chain-id sparkdream-test-1 --home ~/.sparkdream-test

# 3. Wire sentinels into staking/mint/etc. params (one-time)
sparkdreamd genesis identity init --force --i-mean-it --home ~/.sparkdream-test

# 4. Add genesis accounts + gentxs in TSPARK denom
sparkdreamd genesis add-genesis-account $VAL_ADDR 1000000000uspark.sparkdreamtest --home ~/.sparkdream-test
sparkdreamd genesis gentx my-validator 100000000uspark.sparkdreamtest \
    --chain-id sparkdream-test-1 --home ~/.sparkdream-test
sparkdreamd genesis collect-gentxs --home ~/.sparkdream-test

# 5. Start
sparkdreamd start --home ~/.sparkdream-test
```

To override the testnet identity (e.g., spin up a one-off testnet with
a different ticker without changing source), use the mainnet binary
and pass `genesis identity init` overrides:

```bash
sparkdreamd init my-validator --chain-id sparkdream-test-1 --home ~/.sparkdream-adhoc
sparkdreamd genesis identity init \
    --chain-name SparkdreamAdhoc \
    --ticker-prefix SDA \
    --bond-symbol ASPK \
    --dream-symbol ADRM \
    --force --i-mean-it \
    --home ~/.sparkdream-adhoc
```

The CLI override path writes the identity directly to genesis.json,
overriding whatever the build-tag default would have placed there.

**Forking for other federated chains.** A federation chain other than
Sparkdream (Phoenix, Aurora, etc.) ships a vendored
`x/identity/types/default_identity.go` with its own identity values, OR
builds with custom values via a wrapper build. The single-binary
many-chains model is supported but not optimized for; most federation
deployments will ship per-chain binaries (matching how Cosmos Hub /
Osmosis / dYdX ship), each with its own baked-in identity.

**`AllowChainIdMismatch` default switched to `true`.** Previously
`false`, which required `ChainHumanName` to be derivable from
`chain_id`. With a baked-in default identity, the genesis-time
`chain_id` may not match (e.g., test harnesses use random chain ids);
defaulting `AllowChainIdMismatch=true` avoids genesis failure for the
common case. Operators with proper chain_id can leave it; chains that
want the strict check pass `--allow-chain-id-mismatch=false` to the
init CLI or override in genesis.json.

**Still required: staking/mint/etc. param wiring.** `DefaultGenesis()`
for x/identity now returns a real identity, but the upstream
`x/staking` / `x/mint` / `x/crisis` / `x/gov` modules' `DefaultGenesis()`
return their own SDK defaults (`stake` for bond_denom, etc.). Without
the sentinel-rewrite hook substituting these to match identity, those
modules' params would diverge from identity's chosen denom. Two paths
to resolve, both compatible with the build-tag default:

1. **Operator runs `genesis identity init --force --i-mean-it`**:
   writes sentinels into staking/mint/etc. params (the existing CLI
   behavior). Sentinel-rewrite hook substitutes at chain start. Manual
   one-time step. Documented as the operator path until path (2)
   lands.
2. **Future: `sparkdreamd init` PostRun hook auto-wires sentinels**:
   wraps the cosmos-sdk init command to call the identity CLI's
   sentinel-write logic right after init. Eliminates the manual step.
   Not implemented in this pass; tracked as follow-up.

Until path (2) lands, `sparkdreamd init` produces a genesis with
identity baked in but staking/mint/etc. params still on SDK defaults.
A chain started in that state would have a denom mismatch (consumer
modules see `uspark.sparkdream`, staking sees `stake`). Operators must
run `genesis identity init --force --i-mean-it` to wire the sentinels.

**Verification.** Full `go test ./...` clean, `go vet ./...` clean.
Also verified `go build` and `go test ./x/identity/...` clean under
both build modes (default, `-tags testnet`).

## 17. Second-pass closures (post initial review v2)

A second pass addressed the remaining "must track in follow-up" items
identified in §15. Status:

- **Invariant 5 wiring (decision #5): CLOSED.** Staking and mint readers
  are now late-bound onto the Keeper struct via
  `Keeper.SetStakingKeeper` / `Keeper.SetMintKeeper`, called from app.go
  after depinject. Mint goes through a small `MintKeeperAdapter` in
  `app/bank_guard.go` because upstream mint exposes params via a
  collection Item rather than a `GetParams` method. The local
  `StakingKeeper` / `MintKeeper` interfaces moved from
  `keeper/invariants.go` to `types/expected_keepers.go` and now match
  upstream `context.Context` signatures directly. Tests in
  `keeper/invariants_test.go` cover both the aligned and drift cases
  plus a no-op case for when keepers are not wired.

- **Federation peer-identity extension (decision #11): CLOSED.** Proto
  field `peer_identity` added to both `federation.v1.Peer` and
  `MsgRegisterPeer`. `RegisterPeer` handler persists it on the peer
  record and, for `PEER_TYPE_SPARK_DREAM` peers with a non-empty
  identity and ibc_channel_id, pre-registers IBC voucher metadata via
  `preRegisterIBCVoucherMetadata` so wallets render `<SYMBOL>.ibc`
  instead of `ibc/<hash>`. Single-hop only (per spec §9.2);
  multi-hop deferred. Failures emit a `federation_peer_metadata_skipped`
  event but do not fail registration (metadata is informational).
  Federation's `BankKeeper` interface extended with `SetDenomMetaData`
  / `GetDenomMetaData`; mock bank keeper in federation tests updated to
  match.

- **Depinject `Provide` wiring for bank guard (decision #3): DEFERRED
  INDEFINITELY.** See decision #3 (rewritten). Not implementable
  without forking x/bank because depinject would produce ambiguous
  satisfier errors. Spec §14.6 wiring section rewritten to match
  reality: the app-layer wrap is the canonical mechanism, and
  invariant 4 (now functioning post-C1) provides the steady-state
  guarantee with a worst-case one-block detection window.

- **Source-code denom-literal migration (decision #1): NOT STARTED.**
  Survey: 81 hardcoded `"uspark"` / `"dream"` / `"udream"` literals
  across 41 files in `x/` and `app/`. The migration breaks down into:
  - **Genesis-default consts** (e.g., `forum.DefaultFeeDenom`,
    `service.BondDenom`, `name.DefaultRegistrationFee`, all
    `federation.genesis_vals_*` fees): these consts are consumed by
    keeper code at runtime via `sdk.NewCoin(types.DefaultX, amount)`.
    Naively replacing with `identitytypes.BondDenomSentinel` panics
    `sdk.NewCoin`'s `ValidateDenom`. Each consumer needs refactoring
    to either (a) read from its module Params (which the sentinel
    rewrite resolves at chain start) or (b) read directly from
    `identityKeeper.GetBondDenom(ctx)` (requires adding the keeper as
    a dependency to that module).
  - **Runtime keeper code** in x/commons, x/futarchy, x/collect,
    x/shield, x/session, x/service: hardcoded `"uspark"` in
    transaction-building paths. Migration requires adding
    `IdentityKeeper` as a depinject input (x/identity is a leaf so no
    cycle) and switching to `k.identityKeeper.GetBondDenom(ctx)`.
  - **Simulation generators**: hardcoded `"uspark"` in random-amount
    Coin construction. Lowest-risk migration (test-only impact).

  Estimated effort per the spec is ~1-2 weeks of focused work plus
  regression testing. Cannot be safely compressed into a single
  session without significant breakage risk. The current "permissive
  missing-identity" deviation (decision #1) keeps the chain working
  in single-chain mode with the bare literals.

- **247 test script migration (decision #1, second piece): NOT
  STARTED.** Blocked on source-code migration. Mechanical sed pass
  per the spec's §13.3 approach.

- **Removal of permissive missing-identity behavior (decision #1):
  BLOCKED on source-code migration complete.**

- **Removal of `sdk.DefaultBondDenom = "uspark"` (decision #2):
  BLOCKED on source-code migration complete.**

The chain works correctly in its current state:

- Single-chain deployments work unchanged via the permissive missing-
  identity path (the bare literals are still in use).
- Federated deployments: x/identity is fully usable via the
  `genesis identity init` flow. Identity-aware code paths (queries,
  invariants, sentinel rewrite, bank metadata, federation peer
  identity) are all functional. What remains is migrating every
  hardcoded literal in consumer modules to read from
  `identityKeeper.GetBondDenom(ctx)` so federated chains see their
  chain-specific denoms throughout.

Recommended next step: tackle source-code migration module-by-module
in dedicated PRs. Order suggested by the spec: x/rep (DREAM
consumer) -> x/commons (SPARK consumer) -> x/name -> x/forum ->
x/futarchy -> x/federation -> x/service -> x/shield -> x/session ->
x/collect. After each module migrates and tests, the corresponding
test scripts can be updated. Once all are done, remove the
permissive deviation and delete the `sdk.DefaultBondDenom`
assignment.

## 19. Files added vs. files modified (initial commit)

**Files added:**

- `proto/sparkdream/identity/v1/chain_identity.proto`, `genesis.proto`, `query.proto`
- `proto/sparkdream/identity/module/v1/module.proto`
- `x/identity/types/` — keys.go, errors.go, codec.go, expected_keepers.go, genesis.go, types.go, sentinels.go, plus generated `.pb.go`
- `x/identity/keeper/` — keeper.go, genesis.go, query.go, invariants.go
- `x/identity/module/` — module.go, depinject.go, autocli.go, simulation.go
- `x/identity/genesisinit/` — rewrite.go (sentinel hook)
- `x/identity/client/cli/genesis.go` — `genesis identity init/show/validate`
- `app/bank_guard.go` — BankKeeperWithIdentityGuard
- `test/identity/` — bash E2E suite

**Files modified:**

- `app/app.go` — depinject inject of identity keeper, wrap bank keeper, set bank keeper on identity, install sentinel-rewrite InitChainer
- `app/app_config.go` — identity module config + InitGenesis ordering (identity at index 2, right after auth)
- `cmd/sparkdreamd/cmd/commands.go` — wire `genesis identity` subtree

No edits to any other module, by design — x/identity is a leaf.
