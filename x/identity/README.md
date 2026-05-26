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

- `bond_denom` (e.g. `uspark.sparkdream`)
- `dream_denom` (e.g. `udream.sparkdream`)
- `bond_symbol` / `dream_symbol` (display tickers; e.g. `SPARK` / `DREAM`)
- `bond_display` / `dream_display` (display denom names)
- `bond_decimals` / `dream_decimals` (typically `6`)
- `chain_id` (validated against `ctx.ChainID()` via a soft `ValidateAgainstChainID` check — bypassable with `allow_chain_id_mismatch`)
- `founded_at` (Timestamp)

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
3. Calls `bank.SetDenomMetaData` for bond and dream denoms so wallets render `SPARK` / `DREAM` instead of raw denom strings.
4. Writes through to SDK params: `staking.bond_denom` and `mint.mint_denom`. Off-the-shelf wallets and REST tooling work unchanged.
5. Runs the soft `ValidateAgainstChainID` consistency check.

`ExportGenesis` errors if the live `identity` has drifted from `sealed_identity` — a guard against `Msg*UpdateParams` reaching in through a back door.

### Pre-Init Sentinel Substitution

A standalone `genesisinit` package runs **before any module's `InitGenesis`**. It:

- Rewrites `%BOND_DENOM%` / `%DREAM_DENOM%` placeholders in the raw genesis JSON (chosen because `%` is outside the SDK denom regex, so they cannot collide with real denoms).
- Purges legacy `uspark` / `udream` / `dream` / `stake` entries from `bank.denom_metadata`.

This lets the same `config.yml` template produce per-chain genesis files (devnet/testnet/mainnet) without manual denom rewrites.

### Init Ordering

`x/identity` is ordered between `auth` and `bank` so the sealed record exists before bank's first `SetDenomMetaData`.

### V1 Migration Note

Empty identity in genesis is a no-op (legacy single-chain mode supported during migration). On a fresh chain with a real identity record, the module is strict.

## Invariants

Five invariants registered with `crisis`:

| Invariant | Severity | Description |
|---|---|---|
| `identity-initialized` | hard | Sealed record exists and is non-zero |
| `canonical-fields` | hard | Live `identity` matches `sealed_identity` byte-for-byte |
| `bank-metadata-present` | hard | `DenomMetadata` exists for both bond and dream denoms |
| `bank-metadata-canonical` | hard | Symbol/Display of native denoms still match the sealed record |
| `sdk-params-aligned` | warning | `staking.bond_denom` and `mint.mint_denom` still match the sealed bond denom |

## Bank Guard

The app layer wraps `bankkeeper.Keeper` with `BankKeeperWithIdentityGuard` (see [app/bank_guard.go](../../app/bank_guard.go)). Its `SetDenomMetaData` panics with `ErrIdentityImmutable` if any post-seal write mutates the native bond/dream `Symbol` or `Display`. This closes the §14.6 attack where a gov upgrade-handler could rewrite the user-visible token name post-launch by going around `MsgUpdateParams`.

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
| 1 | `ErrIdentityNotSealed` | Keeper read before `InitGenesis` |
| 2 | `ErrIdentityImmutable` | Post-seal write attempted (raised by the bank guard) |
| 3 | `ErrIdentityDrift` | Live record differs from sealed (raised by `ExportGenesis`) |
| 4 | `ErrChainIDMismatch` | Sealed `chain_id` doesn't match `ctx.ChainID()` (soft, bypassable) |
