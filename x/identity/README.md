# x/identity

Single source of truth for a federated Spark Dream chain's identity — bond_denom, dream_denom, display symbols, decimals, founded_at. **Genesis-only immutable**: zero `Msg*` mutation surface; the token names a chain launches with are the names it has forever.

Full spec: [docs/x-identity-spec.md](../../docs/x-identity-spec.md). Implementation decisions: [docs/x-identity-implementation-decisions.md](../../docs/x-identity-implementation-decisions.md).

## Why

Federated Spark Dream chains have distinct human-readable tickers (Phoenix Spark = `PSPK` / `uspark.phoenix`, Aurora Spark = `ASPK` / `uspark.aurora`) without forking source per chain. Each federated chain's SPARK is **not** 1:1 interchangeable with other chains' SPARK — they're independent sovereign tokens with independent inflation and supply. x/identity is what makes that work without scattering ~59 hardcoded `"uspark"` / `"dream"` literals across keepers.

## State

Two `collections.Item[ChainIdentity]`:

| Item | Description |
|---|---|
| `identity` | Live record. Mutable in principle; `canonical-fields` invariant requires it stays byte-identical to `sealed_identity`. |
| `sealed_identity` | Immutable reference written exactly once at `InitGenesis`. Used by invariants and the `BankKeeperWithIdentityGuard` wrapper. |

### `ChainIdentity` Fields

- `chain_human_name` / `chain_ticker_prefix` (e.g. `Sparkdream` / `SDR`; the human name is checked against the consensus `chain_id` by the soft `ValidateAgainstChainID` check, bypassable with `allow_chain_id_mismatch`, which defaults to false on network builds and true on local/test builds)
- `bond_denom` (e.g. `uspark.sparkdream`)
- `dream_denom` (e.g. `udream.sparkdream`; same `u<2-5 letters>.<chain suffix>` shape rule as `bond_denom` — the genesis CLI derives `u<dream-symbol>.<chainname>` by default, exactly like the bond denom, so a chain fully brands its internal token, e.g. `uwish.aurora`)
- `bond_display_symbol` / `dream_display_symbol` (display tickers; e.g. `SPARK` / `DREAM`)
- `bond_display_name` / `dream_display_name` (bank metadata `Name`)
- `bond_display_decimals` / `dream_display_decimals` (typically `6`)
- `founded_at` (unix seconds)

## Messages

**None.** `RegisterInterfaces` is intentionally a no-op. There is no Msg service. `MsgUpdateIdentity` is architecturally forbidden — renaming a chain's native token post-launch is structurally impossible.

## Queries

| Query | Description |
|---|---|
| `ChainIdentity` | Full record |
| `BondDenom` | `bond_denom` only (for cheap lookups) |
| `DreamDenom` | `dream_denom` only |

## Genesis

`InitGenesis` is the only write path:

1. Validates the genesis `ChainIdentity` record.
2. Writes both `identity` and `sealed_identity` (the seal is one-way).
3. Calls `bank.SetDenomMetaData` for bond and dream denoms so wallets render `SPARK` / `DREAM` instead of raw denom strings. It is the only writer of the native metadata: the pre-init rewrite drops a canonical native `denom_metadata` entry from the bank genesis and refuses a non-canonical one, because bank's own InitGenesis runs afterwards through its raw keeper and would overwrite the seal.
4. Runs the soft `ValidateAgainstChainID` consistency check.

The SDK params (`staking.bond_denom`, `mint.mint_denom`, ...) are not written by identity: the pre-init rewrite substitutes the `%BOND_DENOM%` / `%DREAM_DENOM%` sentinels in the genesis JSON before any module's InitGenesis runs, and invariant `sdk-params-aligned` checks the result.

`ExportGenesis` errors if the live `identity` has drifted from `sealed_identity` — a guard against `Msg*UpdateParams` reaching in through a back door.

### Pre-Init Sentinel Substitution

A standalone `genesisinit` package runs **before any module's `InitGenesis`**. It:

- Rewrites `%BOND_DENOM%` / `%DREAM_DENOM%` placeholders in the raw genesis JSON (chosen because `%` is outside the SDK denom regex, so they cannot collide with real denoms).
- Purges legacy `uspark` / `udream` / `dream` / `stake` entries from `bank.denom_metadata`.

This lets the same `config.yml` template produce per-chain genesis files (devnet/testnet/mainnet) without manual denom rewrites.

### Init Ordering

`x/identity` is ordered between `auth` and `bank` so the sealed record exists before bank's first `SetDenomMetaData`.

### Identity is mandatory

A genesis without `app_state.identity`, or with an empty identity, is refused: by the pre-init rewrite at InitChain, by `GenesisState.Validate`, and by `sparkdreamd genesis identity validate`.

## Invariants

Five invariants registered via `RegisterInvariants` (run by `app/invariants_test.go`; x/crisis is not wired):

| Invariant | Severity | Description |
|---|---|---|
| `identity-initialized` | hard | Sealed record exists and is non-zero |
| `canonical-fields` | hard | Live `identity` matches `sealed_identity` on every canonical field (`EqualCanonical`: all fields, display names included) |
| `bank-metadata-present` | hard | `DenomMetadata` exists for both bond and dream denoms |
| `bank-metadata-canonical` | hard | Base, Symbol, Display, Name and denom units (decimals) of the native denoms still match the sealed record |
| `sdk-params-aligned` | warning | `staking.bond_denom` and `mint.mint_denom` still match the sealed bond denom (a read failure is reported as broken) |

## Bank Guard

The app layer wraps `bankkeeper.Keeper` with `BankKeeperWithIdentityGuard` (see [app/bank_guard.go](../../app/bank_guard.go)). Its `SetDenomMetaData` panics with `ErrIdentityImmutable` if any post-seal write changes a sealed field of the native bond/dream metadata (Base, Symbol, Display, Name, denom units and exponents; Description and URI stay free). This closes the §14.6 attack where a gov upgrade-handler could rewrite the user-visible token name post-launch by going around `MsgUpdateParams`.

## Keeper Surface

| Method | Returns | Panics if |
|---|---|---|
| `BondDenom(ctx)` | bond denom string | called pre-genesis |
| `DreamDenom(ctx)` | dream denom string | called pre-genesis |
| `GetChainIdentity(ctx)` | full record | record not sealed |
| `IsIdentityKeeper()` | marker | — |

Pre-genesis panics are deliberate — they prevent silent fallback to hardcoded literals like `"uspark"` if the keeper is queried before `InitGenesis` completes.

## Dependencies

| Module | Usage |
|---|---|
| `x/bank` | `SetDenomMetaData` at genesis; invariants 3 & 4 read it back |
| `x/staking` | invariant 5 only (optional) |
| `x/mint` | invariant 5 only (optional) |

Late-wired via `SetBankKeeper(*Keeper)` / `SetStakingKeeper(*Keeper)` / `SetMintKeeper(*Keeper)` from app.go. The keeper is emitted as `*Keeper` (not value) so the late-bound references propagate to invariants and queries — avoids the AppModule value-copy bug.

## Sentinel Constants

| Constant | Used for |
|---|---|
| `%BOND_DENOM%` | Pre-init genesis JSON substitution |
| `%DREAM_DENOM%` | Pre-init genesis JSON substitution |

Both chosen because `%` is outside the Cosmos SDK denom regex, so collision with a real denom is impossible.

## Errors

| Code | Name | When |
|---|---|---|
| 1200-1211 | `ErrInvalid*`, `ErrDenomCollision`, `ErrSymbolCollision`, `ErrInvalidFoundedAt` | Intrinsic validation of a `ChainIdentity` field |
| 1212 | `ErrIdentityNotInitialized` | Keeper read before `InitGenesis`, or an empty identity |
| 1213 | `ErrIdentityImmutable` | Post-seal write attempted (raised by the bank guard) |
| 1214 | `ErrIdentityAlreadySealed` | `InitGenesis` on a store that already holds a seal |
| 1215 | `ErrChainNameInconsistent` | `chain_human_name` not derivable from the consensus `chain_id` (soft, bypassable) |
