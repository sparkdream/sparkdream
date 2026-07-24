# x/identity Module Specification

## 1. Abstract

The `x/identity` module is the **single source of truth for a federated Spark Dream chain's identity**: its display name, native gas/staking denom (SPARK), and native reputation denom (DREAM). Once set at genesis, the identity is **immutable**: it cannot be changed by governance, the Operations Committee, or any in-band mechanism. Renaming a chain's native token post-launch is destructive in practice (every wallet, every IBC voucher hash, every analytics platform, every signed transaction history references it), so this spec encodes the impossibility honestly rather than offering a knob that should never be turned.

**Motivation.** Federated Spark Dream chains are sovereign. Phoenix Spark is not Aurora Spark; they have different inflation, different supply, different price discovery. The codebase must support per-chain naming without forking source per chain, and downstream modules (x/bank, x/staking, x/mint, x/distribution, x/rep, x/commons, x/dex, x/federation) must all read denoms from one place rather than each hardcoding `"uspark"` and `"dream"`. This module is that place.

**Scope.**
- Defines the `ChainIdentity` record (denoms, display symbols, names, decimals).
- Reads identity from genesis JSON exactly once.
- Propagates the chosen denoms to standard SDK module params (x/staking `bond_denom`, x/mint `mint_denom`, x/distribution community pool denom) at genesis only.
- Registers initial `x/bank` `DenomMetadata` for the two native tokens.
- Exposes a keeper accessor used by every other module that needs the canonical denom string.
- Provides the data structure shipped to peer chains during [x/federation](x-federation-spec.md) `MsgRegisterPeer`.

**Out of scope (deliberate non-features).**
- No `MsgUpdateIdentity` of any kind. No governance override. No upgrade-handler reroute. Code review must reject any future PR adding mutation paths.
- No per-chain key prefix changes (those are an `app.go` concern, not a runtime parameter).
- No identity rotation, no successor-chain support, no chain rebranding. Fork a new chain instead.

x/identity is a **leaf module**: it depends on nothing and is read by everything. Zero cycle risk.

### 1.1. Primer: how `upspk.phoenix` flows in practice

Before the formal sections, a conceptual orientation. If you're trying to understand *where, when, and to whom* a chain-specific denom identifier actually appears, read this first.

#### Three layers

Each chain's identity defines three related strings that describe the same token at different layers:

| Layer            | Phoenix value     | Where it lives                                 | Audience                              |
|------------------|-------------------|------------------------------------------------|---------------------------------------|
| Internal denom   | `upspk.phoenix`   | bank store keys, `Coin.Denom`, tx amounts      | machines, operators, indexers         |
| Display symbol   | `PSPK`            | `bank.DenomMetadata.Symbol`                    | wallet UIs, block explorers           |
| Display name     | `Phoenix Spark`   | `bank.DenomMetadata.Name`                      | dropdowns, marketing                  |

`upspk.phoenix` is the *machine* identifier. Users do not type it in a Keplr wallet; Keplr resolves `1.0 PSPK` <-> `1000000 upspk.phoenix` via the metadata's `DenomUnits` (exponent 6).

#### Where it appears

On the Phoenix chain itself:

```bash
$ sparkdreamd query bank balances alice
balances:
- amount: "1000000"
  denom: upspk.phoenix                # internal denom in bank state

$ sparkdreamd tx bank send alice bob 100upspk.phoenix --fees 5000upspk.phoenix

$ sparkdreamd query staking params | grep bond_denom
bond_denom: upspk.phoenix             # set at genesis via sentinel rewrite, §7.3
```

On Aurora (a federation peer), when 100 PSPK is IBC-transferred Phoenix -> Aurora through `channel-7`:

```bash
$ sparkdreamd query bank balances alice --node tcp://aurora-rpc
balances:
- amount: "100000000"
  denom: ibc/A1B2C3D4E5F6...          # opaque hash on the receiving chain

$ sparkdreamd query ibc-transfer denom-trace A1B2C3D4E5F6...
denom_trace:
  path: transfer/channel-7
  base_denom: upspk.phoenix           # source chain readable without external lookup
```

With the federation extension (§9.2), Aurora pre-registers metadata so wallets render `PSPK.ibc` instead of `ibc/A1B2C3...`.

#### Who sees what

| Audience                            | What they see                                                                                          |
|-------------------------------------|--------------------------------------------------------------------------------------------------------|
| **Phoenix end user (wallet)**       | `1.0 PSPK`. Never types `upspk.phoenix`.                                                               |
| **Phoenix end user (CLI/scripts)**  | `1000000upspk.phoenix` when writing raw txs.                                                           |
| **Phoenix validator**               | Stakes/earns `upspk.phoenix`; sees it in delegation/reward queries.                                    |
| **Aurora end user (wallet)**        | Pre-extension: `1.0 ibc/A1B2...`. Post-extension: `1.0 PSPK.ibc`.                                      |
| **Aurora validator**                | Does not touch it; only Aurora's own `uaspk.aurora` matters for Aurora's consensus.                    |
| **Indexer/explorer (any chain)**    | `upspk.phoenix` as a typed string; source chain readable from the suffix.                              |
| **Off-chain market maker**          | Treats `upspk.phoenix` and `uaspk.aurora` as **different assets** (sovereign chains, separate supply). |

#### What scope it does NOT have

The design is deliberately narrow:

1. **No global registry.** Nothing prevents another chain from also calling its denom `upspk.phoenix`. Collision prevention is bilateral: when Aurora registers Phoenix as a peer, Aurora's operator commits to that denom for that channel (§14.4).
2. **No cross-chain unification.** Phoenix SPARK and Aurora SPARK are *not* the same token. Independent inflation, independent supply, independent price discovery.
3. **No identity attestation.** The `.phoenix` suffix is self-declared. Mitigation against impersonation is multi-signer council approval at peer-registration time, not protocol enforcement (§14.3).
4. **Not a unified cross-chain asset.** A future `x/dex` (speculative) on Phoenix would trade `upspk.phoenix` against IBC-received `ibc/A1B2...`, not `PSPK/ASPK` as a unified pair.

#### When you encounter it in your work

As the chain's developer / operator:

- **Genesis construction**: `genesis identity init --bond-symbol PSPK --chain-name Phoenix` writes it once (§15).
- **Sentinel rewrite at chain start**: every `staking.bond_denom`, `mint.mint_denom`, `gov.min_deposit[].denom`, every Spark Dream module param holding a denom gets `%BOND_DENOM%` substituted to `upspk.phoenix` before any `InitGenesis` runs (§7.2-§7.3).
- **In source code post-migration**: `coin := sdk.NewCoin(k.identityKeeper.BondDenom(ctx), amount)` resolves to `upspk.phoenix` at runtime (§13.1).
- **In test scripts post-migration**: `$BOND_DENOM` shell var from `sparkdreamd query identity bond-denom` (§13.3).

As an end user: raw CLI output, IBC denom traces, and not much else. Wallet UI shows `PSPK`.

The narrow scope is the point. `upspk.phoenix` is a machine identifier whose entire purpose is to make IBC vouchers self-describing across federated chains. The "federated chain" suffix is the cheapest possible primitive that gives IBC tooling a chance to display source-chain context without consulting an external registry.

---

## 2. Dependencies

| Module | Direction | Purpose |
|--------|-----------|---------|
| `x/bank` | x/identity writes | Set `DenomMetadata` for native SPARK and DREAM at genesis |
| `x/staking` | x/identity writes | Set `Params.bond_denom` at genesis |
| `x/mint` | x/identity writes | Set `Params.mint_denom` at genesis |
| `x/distribution` | x/identity writes | (Cosmos SDK v0.50+: derives community pool denom from staking; nothing to write directly) |
| `x/federation` | x/identity reads | Peer `ChainIdentity` carried in peer registry (extension to existing peer record) |
| Every other module | x/identity reads | Accessors: `BondDenom`, `DreamDenom` |

**Important:** other modules consume x/identity via a thin keeper interface, not via a global package variable. The deprecated `sdk.DefaultBondDenom` global ([app/app.go:150](app/app.go#L150)) is replaced by a keeper read; see [§13 Denom Migration Checklist](#13-denom-migration-checklist).

---

## 3. Core Concepts

### 3.1. The ChainIdentity Record

A single record per chain, set at genesis, immutable thereafter. Fields:

| Field | Example (Phoenix) | Example (Aurora) | Constraint |
|---|---|---|---|
| `chain_human_name` | `Phoenix` | `Aurora` | 1-32 chars; printable ASCII; must not contain whitespace at start/end |
| `chain_ticker_prefix` | `PHX` | `AUR` | 2-5 uppercase ASCII letters; uniquely names the chain in human-readable contexts |
| `bond_denom` | `upspk.phoenix` | `uaspk.aurora` | Cosmos SDK denom regex; must match `u[a-z]{2,5}\.[a-z][a-z0-9-]{2,15}` |
| `bond_display_symbol` | `PSPK` | `ASPK` | 3-8 uppercase ASCII letters/digits |
| `bond_display_name` | `Phoenix Spark` | `Aurora Spark` | 1-64 chars; printable ASCII |
| `bond_display_decimals` | `6` | `6` | typically 6 (micro-units); allowed range `[0, 18]` |
| `dream_denom` | `udream.phoenix` | `uwish.aurora` | matches `u[a-z]{2,5}\.[a-z][a-z0-9-]{2,15}` (same shape as `bond_denom`; `udream.<chainname>` is the conventional default) |
| `dream_display_symbol` | `PDRM` | `ADRM` | 3-8 uppercase ASCII letters/digits |
| `dream_display_name` | `Phoenix Dream` | `Aurora Dream` | 1-64 chars |
| `dream_display_decimals` | `6` | `6` | `[0, 18]` |
| `founded_at` | `1735689600` | `1735689600` | Unix seconds; set by genesis writer; informational |

The `chain_ticker_prefix` and `chain_human_name` are not enforced as unique across the federation (no global coordinator can enforce that), but federation peer registration surfaces them so operators can avoid collisions bilaterally.

### 3.2. Why `.<chainname>` Suffix on Internal Denoms

The internal denom format `u<lowercase-bond-symbol>.<chain_name>` (rather than bare `uspark`) is chosen specifically so that **IBC-received vouchers retain source-chain readability through every layer of tooling**. When Aurora receives Phoenix Spark via IBC, the local denom is `ibc/<hash>` representing the source denom `upspk.phoenix`. Resolving the IBC trace (CLI, REST, indexer) reveals the source denom string directly:

```
$ sparkdreamd query ibc-transfer denom-trace <hash>
{
  "denom_trace": {
    "path": "transfer/channel-7",
    "base_denom": "upspk.phoenix"   # (chain origin is obvious without further lookup)
  }
}
```

If both chains used bare `uspark`, the trace would be ambiguous without joining `path -> channel -> counterparty_chain_id` through an external registry. The suffix is the cheapest possible source-tagging primitive.

### 3.3. Why DREAM Also Gets a Per-Chain Denom

DREAM is non-IBC-transferable (enforced in [x/rep](x-rep-spec.md)), so cross-chain confusion in production is impossible. But developers running multiple federated chains side-by-side (testnet, mainnet, peer chain in dev, simulator) constantly confuse them. The cost of giving DREAM the same per-chain treatment as SPARK is small (mostly the same migration), and the dev-ergonomics gain is real.

The dream denom follows the *same* shape rule as the bond denom (`u<2-5 letters>.<chain suffix>`) rather than a fixed `udream.` prefix: a sovereign chain can fully brand its internal token (Aurora's `uwish.aurora` with display symbol `WISH`), exactly as it brands its bond token. `udream.<chainname>` remains the conventional default derived by the genesis CLI and tooling when no explicit dream denom is given. Nothing at runtime pattern-matches the prefix; every consumer resolves the denom via `IdentityKeeper.DreamDenom(ctx)` (§3.5), and the reputation module's non-transferability is what marks the token as internal, not its name.

### 3.4. Genesis-Only Immutability

Four layers of enforcement:

1. **No `Msg*` types.** The module's protobuf service surface has zero mutation messages, `Query` only. There is literally no on-chain path to call `SetChainIdentity` after genesis.
2. **Keeper `Set` is `internal`.** The keeper's `SetChainIdentity` is unexported (`setChainIdentity`) and called only from `InitGenesis`. No other module can import and call it.
3. **Sealed genesis record.** A separate `SealedGenesisIdentity` storage record is written exactly once by `InitGenesis` and protected by an additional write-once check inside `sealGenesisIdentity` (returns `ErrIdentityAlreadySealed` if it already exists). This record is the canonical reference for the invariant: it is *not* the same record served by `query identity chain-identity`. On a node restart, the sealed record is re-read from disk, so the invariant comparison is between two independent storage locations (mutable `Identity` vs. sealed `SealedGenesisIdentity`), not a self-comparison.
4. **Invariant.** `x/identity InvariantCheck` reads `SealedGenesisIdentity` and panics if the live `Identity.bond_denom`, `dream_denom`, `bond_display_symbol`, or `dream_display_symbol` differ. A chain that reaches this panic has a bug in the upgrade pipeline. See [§16](#16-module-invariants).

A defense-in-depth bank-keeper wrapper enforces that the canonical `Symbol`/`Display` of native denoms cannot be altered post-genesis, even via governance; see [§14.6](#146-bank-denommetadata-guard-keeper-wrapper-approach).

Future spec extensions (description tweaks, multilingual display names) could add a narrow `MsgUpdateNonCanonicalMetadata` that only touches `bond_display_name` / `dream_display_name` / descriptions, never the denom strings or symbols. **Not in v1.** See [§18 Future Extensions](#18-future-extensions).

### 3.5. Single Source of Truth, Multiple Consumers

Every other module reads denoms via the x/identity keeper:

```go
type IdentityKeeper interface {
    // BondDenom returns the chain's native gas/staking denom. The convenience
    // accessors panic on missing identity because the identity is set at genesis
    // and never removed; absence on a live chain is corrupt state, not a runtime
    // condition consumers should handle. Pre-genesis callers (e.g., CLI handlers
    // invoked on a partially-constructed genesis) should use GetChainIdentity
    // and handle the error.
    BondDenom(ctx context.Context) string
    DreamDenom(ctx context.Context) string

    // GetChainIdentity returns the full mutable identity record, or
    // (zero, ErrIdentityNotInitialized) if InitGenesis has not yet run.
    GetChainIdentity(ctx context.Context) (types.ChainIdentity, error)

    // GetSealedIdentity returns the immutable sealed-genesis record (§3.4,
    // §6.5), or (zero, ErrIdentityNotInitialized) before InitGenesis runs.
    // The bank-keeper wrapper (§14.6) and the §16 invariants 2-4 read from
    // this record rather than the mutable Identity collection, so that the
    // invariant is a comparison between independent storage entries.
    GetSealedIdentity(ctx context.Context) (types.ChainIdentity, error)
}
```

The two convenience accessors panic to keep callsites ergonomic in production hot paths (the alternative is propagating an error through every `sdk.NewCoin` site, which would defeat the purpose of having an accessor). `GetChainIdentity` returns an error and is the supported path for genesis-tooling code that may run before identity is populated.

For modules that hold `bond_denom` in their own params (x/staking, x/mint), those params are still respected by their own logic; the sentinel rewrite (§7.3) ensures they hold the identity-supplied denom. This preserves compatibility with stock SDK tooling (REST, gRPC, wallet metadata queries) that expects to find `bond_denom` in `/cosmos/staking/v1beta1/params`.

### 3.6. Federation Peer Identities

When a peer chain registers via [x/federation](x-federation-spec.md) `MsgRegisterPeer`, the registration payload includes the peer's full `ChainIdentity`. The receiving chain stores it alongside the existing `FederationPeer` record. Two downstream effects:

1. **DenomMetadata pre-registration.** On peer activation, the local x/bank gets a `DenomMetadata` record for the expected IBC voucher denom (computed deterministically from the IBC channel + the peer's `bond_denom`), with `Symbol = <peer>SPK.ibc` and a human-readable description. Wallets and explorers render the peer's tokens correctly the first time they appear, instead of showing raw `ibc/<hash>` strings.
2. **Federation queries.** `query federation peer phoenix-1` returns the peer's ticker prefix, display name, and bond/dream denoms, useful for any UI that lists federated chains.

---

## 4. State Objects (Protobuf)

### 4.1. ChainIdentity

```protobuf
// proto/sparkdream/identity/v1/chain_identity.proto
syntax = "proto3";
package sparkdream.identity.v1;

option go_package = "sparkdream/x/identity/types";

message ChainIdentity {
  // Human-readable chain identity
  string chain_human_name      = 1;   // e.g., "Phoenix"
  string chain_ticker_prefix   = 2;   // e.g., "PHX"; 2-5 uppercase ASCII

  // SPARK (gas/staking)
  string bond_denom            = 3;   // internal denom, e.g., "upspk.phoenix"
  string bond_display_symbol   = 4;   // wallet ticker, e.g., "PSPK"
  string bond_display_name     = 5;   // e.g., "Phoenix Spark"
  uint32 bond_display_decimals = 6;   // typically 6 (micro-units)

  // DREAM (reputation/internal)
  string dream_denom            = 7;  // e.g., "udream.phoenix"
  string dream_display_symbol   = 8;  // e.g., "PDRM"
  string dream_display_name     = 9;  // e.g., "Phoenix Dream"
  uint32 dream_display_decimals = 10; // typically 6

  // Founding metadata (informational; surfaces in federation peer queries)
  int64  founded_at             = 11; // Unix seconds, set at genesis writer
}
```

### 4.2. GenesisState

```protobuf
// proto/sparkdream/identity/v1/genesis.proto
message GenesisState {
  ChainIdentity identity = 1;

  // allow_chain_id_mismatch bypasses the soft consistency check between
  // `identity.chain_human_name` and the consensus-level `chain_id` (§11.1).
  // Init-time only: this field is read by InitGenesis and never persisted to
  // state. Default false. Set to true only for the rare legitimate case
  // where a chain reuses an existing chain_id from a forked predecessor.
  bool allow_chain_id_mismatch = 2;
}
```

There are no module `Params`. The `ChainIdentity` *is* the parameters, and it's immutable. This is intentional: a `Params` message implies updatability via standard SDK governance, and x/identity has no such concept.

---

## 5. Storage Schema

Cosmos SDK collections framework:

| Collection | Key prefix | Value | Purpose |
|------------|------------|-------|---------|
| `Identity` | `0x00` | `ChainIdentity` | The mutable record served by queries. Today nothing mutates it post-genesis, but storing it as an `Item` (not an `unexported const`) keeps the design open to the future `MsgUpdateNonCanonicalMetadata` of §18 without restructuring storage. |
| `SealedGenesisIdentity` | `0x01` | `ChainIdentity` | Written exactly once by `sealGenesisIdentity` during `InitGenesis`. Subsequent writes return `ErrIdentityAlreadySealed`. Read by the invariant in §16 to detect any post-genesis drift of `Identity`. |

Two distinct `collections.Item[ChainIdentity]` instances under different key prefixes so the invariant has an independent reference point (never self-compares). No indexes, no maps, no pagination; exactly one of each.

---

## 6. Genesis

### 6.1. Genesis JSON Shape

```json
{
  "app_state": {
    "identity": {
      "identity": {
        "chain_human_name": "Phoenix",
        "chain_ticker_prefix": "PHX",
        "bond_denom": "upspk.phoenix",
        "bond_display_symbol": "PSPK",
        "bond_display_name": "Phoenix Spark",
        "bond_display_decimals": 6,
        "dream_denom": "udream.phoenix",
        "dream_display_symbol": "PDRM",
        "dream_display_name": "Phoenix Dream",
        "dream_display_decimals": 6,
        "founded_at": 1735689600
      }
    }
  }
}
```

### 6.2. InitGenesis Flow

By the time `InitGenesis` runs, the sentinel rewrite in [§7.3](#73-implementation-surface) has already substituted denoms throughout the genesis JSON. The keeper's job is now narrowly scoped to persisting the canonical record, sealing it for invariant enforcement, and seeding bank metadata.

```go
// x/identity/keeper/genesis.go
//
// Convention: validate ALL input before any state mutation. Steps 1-2 are
// pure validation; if either fails, no state is written. Steps 3-5 perform
// the actual writes.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) error {
    // 1. Validate intrinsic fields. The sentinel-rewrite hook (§7.3) already
    //    validated, but defense in depth is cheap here.
    if err := gs.Identity.Validate(); err != nil {
        return fmt.Errorf("invalid chain identity: %w", err)
    }

    // 2. Validate against the consensus chain_id. This check cannot live in
    //    Validate() because it needs ctx.ChainID() (§11.1).
    if err := gs.Identity.ValidateAgainstChainID(ctx.ChainID(), gs.AllowChainIdMismatch); err != nil {
        return err
    }

    // 3. Persist the identity (mutable Item, kept for queries).
    if err := k.setChainIdentity(ctx, gs.Identity); err != nil {
        return err
    }

    // 4. Persist the *sealed* identity (separate immutable record used by
    //    the invariant). See §3.4 and §16.
    if err := k.sealGenesisIdentity(ctx, gs.Identity); err != nil {
        return err
    }

    // 5. Register x/bank DenomMetadata for both native tokens. Legacy metadata
    //    has already been purged by the sentinel-rewrite hook (§7.3 / §8.1),
    //    BEFORE bank.InitGenesis loaded its slice. So no purge-after-the-fact
    //    is needed here.
    if err := k.registerNativeDenomMetadata(ctx, gs.Identity); err != nil {
        return err
    }

    return nil
}
```

Notes:

- No `propagateToSDKParams` step exists. That work is done by the sentinel rewrite (§7.3) before any module's `InitGenesis` is invoked. Centralizing propagation there guarantees identity-derived denoms are visible to staking, mint, gov, crisis, *and* genutil's gentx validation pass.
- No `purgeCollidingBankMetadata` step exists. That work is done by `purgeLegacyBankMetadata` inside the sentinel-rewrite hook (§7.3), before bank's own `InitGenesis` loads its slice. See §8.1 for why that is the only ordering that works (running the purge from `identity.InitGenesis` is either too late or a no-op, depending on relative ordering with bank).
- `sealGenesisIdentity` is idempotent under restoration (see §6.5). It returns `ErrIdentityAlreadySealed` only when the existing sealed record differs from the genesis identity being supplied.
- **Canonicalization invariant.** The value sealed in step 4 must be byte-identical to the value passed to `registerNativeDenomMetadata` in step 5; the bank-keeper wrapper (§14.6) checks that metadata writes match the sealed values. v1 does not canonicalize `ChainIdentity` (no lowercase normalization, no whitespace trimming beyond Validate's rejection). If a future change introduces canonicalization (e.g., a `Normalize()` pass), it must run BEFORE step 3 so all three writes operate on the same canonical form. A test (`TestInitGenesisSealedMatchesMetadataWrite`) asserts this invariant by snapshotting both writes during InitGenesis and comparing them field-by-field.

### 6.3. Module InitGenesis Ordering

Sentinel rewrite (§7.3) decouples *denom propagation* from `InitGenesis` ordering: by the time any module's `InitGenesis` runs, the genesis JSON already holds canonical denoms. Ordering still matters, but only for the narrower concern of when x/identity's *own* keeper state (sealed identity, bank metadata) is in place relative to consumers that read it.

The project's existing `InitGenesis` slice lives in the `appconfig.WrapAny(&runtimev1alpha1.Module{...})` declaration. Edit [app/app_config.go:201-236](app/app_config.go#L201-L236) to insert `identity` immediately after `authtypes`:

```go
// app/app_config.go (excerpt; diff against existing slice)
InitGenesis: []string{
    consensustypes.ModuleName,
    authtypes.ModuleName,
+   identitytypes.ModuleName,       // NEW: seal + chain-id check + native DenomMetadata writes
    banktypes.ModuleName,           // bank loads its (already-rewritten) slice; legacy metadata
                                    //   was stripped by the sentinel-rewrite hook before this runs
    distrtypes.ModuleName,
    stakingtypes.ModuleName,
    slashingtypes.ModuleName,
    govtypes.ModuleName,
    minttypes.ModuleName,
    genutiltypes.ModuleName,
    // ... rest of SDK modules ...
    // ... Spark Dream chain modules ...
},
```

Two ordering invariants this preserves:

1. **identity before bank.** `registerNativeDenomMetadata` (§8.2) writes through `BankKeeperWithIdentityGuard` (§14.6) during `identity.InitGenesis`. The wrapped bank keeper exists from the depinject-construction phase, which runs before any `InitGenesis`, so the write target is valid even though bank's own `InitGenesis` runs second. Bank's `InitGenesis` then loads its (already-rewritten, already-purged) slice on top, and since identity's metadata writes have the same `Base` keys that bank would have written, the net state is correct regardless of which one wrote a particular key last.
2. **identity before genutil.** `genutil`'s gentx validation reads `staking.params.bond_denom`, which the sentinel hook rewrote *before any `InitGenesis`*. The identity-vs-genutil ordering is therefore not load-bearing; both depend on the pre-hook substitution having already happened.

Verified by two unit tests:
- `TestGenesisOrderingPlacesIdentityImmediatelyAfterAuth`: asserts the slice contains the new entry at index 2.
- `TestSentinelRewriteHappensBeforeInitGenesis`: constructs a genesis with `staking.params.bond_denom = "%BOND_DENOM%"`, runs the full init flow via `app.InitChain`, and asserts `app.StakingKeeper.BondDenom(ctx) == "upspk.phoenix"` afterward.

### 6.4. ExportGenesis Flow

```go
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
    id, err := k.GetChainIdentity(ctx)
    if err != nil {
        // Live chain must have a mutable Identity; its absence indicates
        // corrupt state. Wrap so operator logs show the call site clearly.
        panic(fmt.Errorf("ExportGenesis: mutable Identity missing on a live chain (corrupt state): %w", err))
    }
    sealed, err := k.GetSealedIdentity(ctx)
    if err != nil {
        // Sealed identity must also exist; its absence means the seal was
        // dropped after InitGenesis, which the §16 invariants should have
        // caught long before reaching export.
        panic(fmt.Errorf("ExportGenesis: SealedGenesisIdentity missing on a live chain (corrupt state): %w", err))
    }
    if !sealed.EqualCanonical(id) {
        // Cannot recover here. The invariant should have caught this; if it
        // didn't, refuse to export rather than launder corrupt state into a
        // new genesis. EqualCanonical (§6.5) ignores informational fields
        // like founded_at and compares only the immutable denom/symbol set.
        panic(fmt.Sprintf("sealed identity disagrees with live identity in canonical fields; diff: %s",
            sealed.DiffCanonical(id)))
    }
    return &types.GenesisState{
        Identity:              id,
        AllowChainIdMismatch:  false, // never re-export the bypass; new chain must consciously re-enable
    }
}
```

The sealed record itself is **not** exported in the genesis JSON. The next chain's `InitGenesis` calls `sealGenesisIdentity` against the exported `Identity`, which seals freshly under the new chain's storage. This is intentional:

- It prevents an attacker who tampered with `Identity` from also pre-baking the matching tampered seal into the export.
- It forces the next chain start to re-establish its own sealed reference, which (combined with the consistency check above) means a tampered `Identity` at export time would have been visible BEFORE export via the §16 invariant.

### 6.5. Sealing semantics

`sealGenesisIdentity` is idempotent under restoration so that genesis re-import (chain halt, governance fork, state-sync replay) does not require special-case handling, while still blocking any tampering attempt:

```go
import (
    "errors"

    "cosmossdk.io/collections"  // for collections.ErrNotFound
    errorsmod "cosmossdk.io/errors"
)

func (k Keeper) sealGenesisIdentity(ctx sdk.Context, id types.ChainIdentity) error {
    existing, err := k.sealedIdentity.Get(ctx)
    switch {
    case errors.Is(err, collections.ErrNotFound):
        // First seal. Write and return.
        return k.sealedIdentity.Set(ctx, id)
    case err != nil:
        return err
    case existing.EqualCanonical(id):
        // Restoration (e.g., state-sync replay against the same chain).
        // Idempotent no-op.
        return nil
    default:
        // A sealed record already exists AND differs from the supplied
        // identity in a canonical (denom/symbol) field. This is the
        // tampering case. Refuse with a field-diff error message so the
        // operator can see exactly which field changed.
        return errorsmod.Wrapf(types.ErrIdentityAlreadySealed,
            "sealed identity conflicts with InitGenesis identity; diff: %s",
            existing.DiffCanonical(id))
    }
}
```

`ChainIdentity.EqualCanonical(other)` compares the immutable canonical fields:

- `bond_denom`, `dream_denom`
- `bond_display_symbol`, `dream_display_symbol`
- `bond_display_decimals`, `dream_display_decimals`
- `chain_human_name`, `chain_ticker_prefix`
- `founded_at`

All nine `ChainIdentity` fields are treated as canonical, with the exception of `bond_display_name` and `dream_display_name` which the future `MsgUpdateNonCanonicalMetadata` (§18) is intended to allow updating. `founded_at` is included so that a state-migration handler cannot silently default it to `0` and leak that divergence into the mutable-vs-sealed gap. Migration handlers must preserve `founded_at` through restorations; one extra line of code in any migration handler buys complete consistency between the live and sealed records.

`DiffCanonical(other)` returns a human-readable diff of those same fields for the error message (e.g., `"bond_denom: upspk.phoenix -> upspk.firebird"`).

---

## 7. Identity Propagation

At genesis, x/identity propagates the chosen denom strings into the standard SDK module params so that off-the-shelf clients (REST, gRPC, wallets) continue to discover the chain's denom in the conventional places.

### 7.1. Why post-`InitGenesis` writes do not work

The naive design (`identity.InitGenesis` writes `staking.Params.bond_denom` via `SetParams`) is unsound because Cosmos SDK module `InitGenesis` functions unconditionally re-set their own params from their genesis-JSON slice. The actual project init order (verified at [app/app_config.go:201-236](app/app_config.go#L201-L236)) is:

```
consensus -> auth -> bank -> distr -> staking -> slashing -> gov -> mint -> ... -> identity -> ... -> federation
```

Even if `x/identity` were moved earlier and ran before `staking`/`mint`/`gov`/`crisis`, the staking module's own `InitGenesis(gs)` calls `SetParams(gs.Params)` from its slice, silently overwriting any identity-side write. Re-ordering identity *after* every consumer is equally broken because staking/mint/gov have already initialized state derived from the stale denom (validator delegations, gov deposit accounting, inflation accumulators).

### 7.2. Canonical mechanism: pre-`InitGenesis` sentinel rewrite

The propagation mechanism is **rewriting the raw genesis JSON in memory before any module's `InitGenesis` runs**, performed by a pre-init hook installed in `app.go`. The hook reads `app_state.identity.identity.bond_denom` and `dream_denom`, then walks the rest of the genesis JSON, replacing the literal sentinel strings `"%BOND_DENOM%"` and `"%DREAM_DENOM%"` with the chain-specific values.

| Target field (genesis JSON path) | Rewrite from sentinel |
|---|---|
| `app_state.staking.params.bond_denom` | `%BOND_DENOM%` -> `identity.bond_denom` |
| `app_state.mint.params.mint_denom` | `%BOND_DENOM%` -> `identity.bond_denom` |
| `app_state.crisis.constant_fee.denom` | `%BOND_DENOM%` -> `identity.bond_denom` |
| `app_state.gov.params.min_deposit[*].denom` | `%BOND_DENOM%` -> `identity.bond_denom` |
| `app_state.gov.params.expedited_min_deposit[*].denom` | `%BOND_DENOM%` -> `identity.bond_denom` |
| `app_state.<sparkdream-module>.params.*denom` (e.g. `rep`, `name`, `forum`, `futarchy`, `federation`, `commons`) | both sentinels -> identity values |
| `x/distribution` | no rewrite needed (community pool is denom-agnostic) |

DREAM is **not** written to any standard SDK module. It is rewritten only in Spark Dream module params that store it (mostly `x/rep`).

**`app_state.genutil.gen_txs` is intentionally excluded from the rewrite.** Gen-txs are signed transactions; substituting bytes inside an encoded tx would invalidate the validator's signature. The expected operator flow is:

1. `sparkdreamd genesis identity init …` (fixes the chain's denoms).
2. `sparkdreamd gentx … <amount><resolved-bond-denom>` (each validator signs against the resolved denom).
3. `sparkdreamd genesis collect-gentxs` (collects signed txs into `app_state.genutil`).

The rewrite hook (§7.3) *defends* against operator error here by halting if it finds a sentinel inside `app_state.genutil`. Operators who use sentinel literals in gentxs would otherwise produce a chain whose first block immediately fails signature verification on every validator.

### 7.3. Implementation surface

Sentinel constants live in `x/identity/types` (alongside `ChainIdentity`) so every module's `DefaultGenesis()` can reference them without importing the larger `genesisinit` package:

```go
// x/identity/types/sentinels.go
package types

const (
    BondDenomSentinel  = "%BOND_DENOM%"
    DreamDenomSentinel = "%DREAM_DENOM%"
)
```

The `%` character is not in the Cosmos SDK denom regex (`[a-zA-Z][a-zA-Z0-9/:._-]{2,127}`), so the sentinels cannot collide with a legitimate denom value. They also cannot be constructed via `sdk.NewCoin` (which calls `ValidateDenom` and panics); see [§13.2](#132-genesis-defaults-handling) for the raw-struct construction pattern that `DefaultGenesis()` must use.

The rewrite itself is a separate package, invoked from `app.go` between genesis JSON load and the existing `BaseApp` `InitChainer`:

```go
// x/identity/genesisinit/rewrite.go
package genesisinit

import (
    "encoding/json"
    "fmt"
    "strings"

    banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
    "sparkdream/x/identity/types"
)

// RewriteSentinels walks the genesis app_state and replaces the bond/dream
// denom sentinels with the values from app_state.identity.identity. It also
// strips any pre-existing app_state.bank.denom_metadata entries that collide
// with the chosen native denoms (legacy `uspark`/`dream` metadata copied from
// starter genesis), before bank's own InitGenesis loads them; see §8.1 for
// why this is the only safe ordering.
//
// Returns the rewritten app_state AND the resolved ChainIdentity so callers
// can stash it for logging / metrics without re-parsing.
func RewriteSentinels(rawAppState json.RawMessage) (json.RawMessage, types.ChainIdentity, error) {
    var as map[string]json.RawMessage
    if err := json.Unmarshal(rawAppState, &as); err != nil {
        return nil, types.ChainIdentity{}, fmt.Errorf("unmarshal app_state: %w", err)
    }
    idRaw, ok := as[types.ModuleName]
    if !ok {
        return nil, types.ChainIdentity{}, fmt.Errorf("app_state.%s missing; identity must be present in genesis", types.ModuleName)
    }
    var idGS types.GenesisState
    if err := json.Unmarshal(idRaw, &idGS); err != nil {
        return nil, types.ChainIdentity{}, fmt.Errorf("unmarshal identity genesis: %w", err)
    }
    // Intrinsic-only validation here. The chain-id consistency check (§11.1)
    // happens in InitGenesis where ctx.ChainID() is available.
    if err := idGS.Identity.Validate(); err != nil {
        return nil, types.ChainIdentity{}, fmt.Errorf("invalid chain identity (sentinel rewrite must reject early): %w", err)
    }

    // Defense: refuse to rewrite anything inside app_state.genutil. Gen-txs are
    // signed; substituting bytes invalidates signatures. Operators must run
    // `genesis identity init` BEFORE `gentx`, so sentinels in gentxs are an
    // operator error worth halting on.
    if genutilRaw, ok := as["genutil"]; ok {
        if strings.Contains(string(genutilRaw), BondDenomSentinel) ||
            strings.Contains(string(genutilRaw), DreamDenomSentinel) {
            return nil, types.ChainIdentity{}, fmt.Errorf(
                "sentinel found inside app_state.genutil; gentxs must be generated AFTER `genesis identity init` so they sign against the resolved denom")
        }
    }

    // Substitute everywhere else. Bytewise deterministic across validators
    // provided each validator has byte-identical genesis.json (see §7.4).
    s := string(rawAppState)
    s = strings.ReplaceAll(s, BondDenomSentinel, idGS.Identity.BondDenom)
    s = strings.ReplaceAll(s, DreamDenomSentinel, idGS.Identity.DreamDenom)
    if strings.Contains(s, BondDenomSentinel) || strings.Contains(s, DreamDenomSentinel) {
        return nil, types.ChainIdentity{}, fmt.Errorf("sentinel survived rewrite; logic bug")
    }

    // Re-parse to operate on bank metadata at the structured level.
    if err := json.Unmarshal([]byte(s), &as); err != nil {
        return nil, types.ChainIdentity{}, fmt.Errorf("re-unmarshal after substitution: %w", err)
    }
    if err := purgeLegacyBankMetadata(as, idGS.Identity); err != nil {
        return nil, types.ChainIdentity{}, err
    }
    out, err := json.Marshal(as)
    if err != nil {
        return nil, types.ChainIdentity{}, fmt.Errorf("re-marshal: %w", err)
    }
    return out, idGS.Identity, nil
}

// purgeLegacyBankMetadata removes legacy-denom DenomMetadata entries from
// app_state.bank.denom_metadata before bank.InitGenesis loads them. The
// alternative (running a purge from identity.InitGenesis against the live
// bank store) is broken because either ordering choice fails; see §8.1.
//
// Operates as a structured rewrite over app_state.bank, using map[string]
// json.RawMessage so balances/supply/params/send_enabled/etc. pass through
// unchanged. ONLY the denom_metadata key is rewritten. The strict regex in
// §14.2 makes "stake" impossible as a Spark Dream native denom, so its
// metadata is always safely purged.
//
// The legacy literal set is defined as a package-level var so adding
// future literals is visible in code review (see legacyDenomLiterals).
// legacyDenomLiterals is the canonical list of legacy bond/dream denom
// strings to purge from app_state.bank.denom_metadata at genesis. Adding to
// this list requires a spec update; deliberately kept narrow.
var legacyDenomLiterals = []string{"uspark", "udream", "dream", "stake"}

func purgeLegacyBankMetadata(as map[string]json.RawMessage, id types.ChainIdentity) error {
    bankRaw, ok := as["bank"]
    if !ok {
        return nil
    }
    var bank map[string]json.RawMessage
    if err := json.Unmarshal(bankRaw, &bank); err != nil {
        return fmt.Errorf("unmarshal bank genesis: %w", err)
    }
    metaRaw, ok := bank["denom_metadata"]
    if !ok || len(metaRaw) == 0 || string(metaRaw) == "null" {
        return nil // nothing to purge
    }
    var entries []banktypes.Metadata
    if err := json.Unmarshal(metaRaw, &entries); err != nil {
        return fmt.Errorf("unmarshal denom_metadata: %w", err)
    }
    legacy := make(map[string]struct{}, len(legacyDenomLiterals))
    for _, d := range legacyDenomLiterals {
        legacy[d] = struct{}{}
    }
    delete(legacy, id.BondDenom)  // never purge the chosen denoms even if they coincide with a legacy literal
    delete(legacy, id.DreamDenom)
    // Safe in-place filter: `entries` is fresh from json.Unmarshal and has
    // no other references, so reusing its backing array for `kept` is safe.
    kept := entries[:0]
    for _, m := range entries {
        if _, isLegacy := legacy[m.Base]; isLegacy {
            continue
        }
        kept = append(kept, m)
    }
    newMeta, err := json.Marshal(kept)
    if err != nil {
        return fmt.Errorf("re-marshal denom_metadata: %w", err)
    }
    bank["denom_metadata"] = newMeta
    newBank, err := json.Marshal(bank)
    if err != nil {
        return fmt.Errorf("re-marshal bank genesis: %w", err)
    }
    as["bank"] = newBank
    return nil
}
```

The hook is wired in `app.go`. Two ordering rules govern its placement:

1. **The capture must happen AFTER the existing `app.SetInitChainer(app.InitChainer)` call** that installs the App's own module-init `InitChainer`. If the capture runs earlier, `app.InitChainer()` returns `nil` (no chainer installed yet), and the wrapper would deref a nil function value on InitChain.
2. **The wrapper must capture the original before reassigning.** `SetInitChainer` overwrites the field; reading `app.InitChainer()` inside the closure after the reassignment loops back into the closure itself.

`(*baseapp.BaseApp).InitChainer()` is the exported getter that returns the currently-installed `sdk.InitChainer` (see [baseapp/baseapp.go in cosmos-sdk v0.50+](https://github.com/cosmos/cosmos-sdk/blob/main/baseapp/baseapp.go)). The pattern:

```go
// app/app.go (excerpt). Place IMMEDIATELY after the existing
// app.SetInitChainer(app.InitChainer) line.
origInitChainer := app.InitChainer()
if origInitChainer == nil {
    // Defensive: if this fires, the identity sentinel hook was wired before
    // the App's own InitChainer was installed. Move the block down in app.go.
    panic("identity sentinel hook installed before app.SetInitChainer(app.InitChainer); reorder app.go")
}
app.SetInitChainer(func(ctx sdk.Context, req abci.RequestInitChain) (abci.ResponseInitChain, error) {
    rewritten, resolved, err := genesisinit.RewriteSentinels(req.AppStateBytes)
    if err != nil {
        panic(fmt.Sprintf("identity sentinel rewrite failed: %v", err))
    }
    req.AppStateBytes = rewritten
    app.Logger().Info("identity sentinel rewrite complete",
        "bond_denom", resolved.BondDenom,
        "dream_denom", resolved.DreamDenom)
    return origInitChainer(ctx, req)
})
```

This runs **once at chain start**, before any module's `InitGenesis`. After that, the SDK params are simply the standard SDK params; governance can update them, but the canonical denom decision was already locked in via the sentinel rewrite.

### 7.4. Why a JSON string-replace and not a typed AST walk

A typed AST walk would need to know the schema of every module's params and every gentx structure. Since the sentinels are guaranteed non-conforming as Cosmos SDK denoms (the `%` character is not in `[a-zA-Z][a-zA-Z0-9/:._-]{2,127}`), a pure string substitution over the JSON bytes is both simpler and strictly safe. `RewriteSentinels` asserts the sentinels are gone post-rewrite, so any sentinel that slips past the substitution (e.g., in a nested escaped string) halts genesis instead of silently shipping.

**Determinism.** The rewrite is bytewise deterministic across validators provided each validator has byte-identical `genesis.json` before `InitChain`. This is the normal case (validators sync genesis from a canonical source). If one validator hand-edits its local genesis (e.g., to bump a peer's bond), it gets a divergent post-rewrite app state and will fail consensus at the first commit. This is a desirable failure mode: silent divergence is worse than loud halt. The mechanism inherits its determinism guarantee from `genesis.json` itself; the rewrite does not weaken it.

### 7.5. Depinject and keeper wiring

Per the manual-wiring pattern in [development-conventions.md](development-conventions.md), `x/identity` does **not** take `staking`/`mint`/`crisis`/`gov` keepers via depinject inputs. The sentinel-rewrite path operates on raw JSON before keepers exist; no cross-keeper calls are needed at runtime. This keeps `x/identity` a true leaf in the depinject graph and avoids the cycle risk that has bitten other modules in this project.

Downstream modules (`x/rep`, `x/commons`, `x/name`, etc.) get the `IdentityKeeper` via depinject (it has no inputs of its own, so it can be constructed first) and read denoms on demand via `BondDenom` / `DreamDenom`.

---

## 8. DenomMetadata Bootstrap

### 8.1. Cleanup of pre-existing metadata

Operator-supplied genesis JSON often inherits `app_state.bank.denom_metadata` entries from legacy starter genesis files (e.g., a `uspark` / `dream` / `stake` entry). If those entries reach the bank store unchanged, identity's later metadata write produces **two** entries per native denom slot (legacy with zero supply, native with real supply). Wallets and explorers render both and users get confused.

The cleanup is performed by `purgeLegacyBankMetadata` inside the sentinel-rewrite hook ([§7.3](#73-implementation-surface)), operating on raw `app_state.bank.denom_metadata` before bank's own `InitGenesis` loads its slice. This is the only ordering that works:

- Running it *from* `identity.InitGenesis` *after* `bank.InitGenesis` would require ordering identity after bank, but bank.InitGenesis writes its metadata first, so the purge would need to know exactly which entries identity will later overwrite. Solvable but fragile.
- Running it *from* `identity.InitGenesis` *before* `bank.InitGenesis` would operate on an empty bank store (bank hasn't loaded the legacy entries yet) and silently do nothing.
- Running it as a `BeginBlocker` of block 1 leaves the legacy entries visible to clients between InitChain and the first commit, which is observable corruption.

The rewrite-hook approach has none of these problems: the legacy entries simply never enter the bank store. The purge is conservative: only the literal set `{uspark, udream, dream, stake}` is removed, and an entry whose `Base` happens to equal the chain's chosen denom is preserved.

The companion `validate-genesis` helper (§15) emits a hard error when `app_state.bank.denom_metadata` contains a known-legacy literal that differs from the chosen denoms, so operators see the problem at genesis-construction time rather than relying on the silent runtime purge.

### 8.2. Registering native DenomMetadata

At genesis, x/identity registers `x/bank` `DenomMetadata` for both native tokens:

```go
func (k Keeper) registerNativeDenomMetadata(ctx context.Context, id types.ChainIdentity) error {
    sparkMeta := banktypes.Metadata{
        Description: fmt.Sprintf("%s, native gas and staking token of the %s federated Spark Dream chain",
            id.BondDisplayName, id.ChainHumanName),
        DenomUnits: []*banktypes.DenomUnit{
            {Denom: id.BondDenom, Exponent: 0, Aliases: []string{fmt.Sprintf("micro%s", strings.ToLower(id.BondDisplaySymbol))}},
            {Denom: strings.ToLower(id.BondDisplaySymbol), Exponent: id.BondDisplayDecimals},
        },
        Base:    id.BondDenom,
        Display: strings.ToLower(id.BondDisplaySymbol),
        Name:    id.BondDisplayName,
        Symbol:  id.BondDisplaySymbol,
    }
    k.bankKeeper.SetDenomMetaData(ctx, sparkMeta)

    dreamMeta := banktypes.Metadata{
        Description: fmt.Sprintf("%s, internal reputation token of the %s federated Spark Dream chain (non-transferable, non-IBC)",
            id.DreamDisplayName, id.ChainHumanName),
        DenomUnits: []*banktypes.DenomUnit{
            {Denom: id.DreamDenom, Exponent: 0, Aliases: []string{fmt.Sprintf("micro%s", strings.ToLower(id.DreamDisplaySymbol))}},
            {Denom: strings.ToLower(id.DreamDisplaySymbol), Exponent: id.DreamDisplayDecimals},
        },
        Base:    id.DreamDenom,
        Display: strings.ToLower(id.DreamDisplaySymbol),
        Name:    id.DreamDisplayName,
        Symbol:  id.DreamDisplaySymbol,
    }
    k.bankKeeper.SetDenomMetaData(ctx, dreamMeta)

    return nil
}
```

Notes:
- DREAM metadata description explicitly calls out non-transferability, which surfaces in any wallet that reads `Description`, so users don't try to IBC-send DREAM and get confused when it fails.
- After genesis, x/bank owns the metadata records like any other denom metadata. Governance *can* update descriptions and unit aliases via standard x/bank operations. **`Base`, `Symbol`, and `Display` of native denoms are enforced immutable by the bank-keeper wrapper in §14.6**, regardless of writer (direct tx, gov proposal, upgrade handler). x/identity registers the canonical values once at genesis and the wrapper rejects any post-genesis alteration of those three fields.

---

## 9. Federation Peer Identity Extension

Proposed extension to [x/federation](x-federation-spec.md), adding `ChainIdentity` to the `FederationPeer` record:

```protobuf
// extension to proto/sparkdream/federation/v1/peer.proto
message FederationPeer {
  string chain_id                                 = 1;
  sparkdream.identity.v1.ChainIdentity identity   = 2;  // (NEW)
  string ibc_channel                              = 3;
  PeerStatus status                               = 4;
  // ... (existing fields) ...
}
```

### 9.1. Peer Registration Carries Identity

`MsgRegisterPeer` payload gets a new `identity` field, mandatory:

```protobuf
message MsgRegisterPeer {
  string authority                                = 1;  // local Operations Committee
  string peer_chain_id                            = 2;
  sparkdream.identity.v1.ChainIdentity peer_identity = 3;  // (NEW)
  string ibc_channel                              = 4;
  // ...
}
```

The peer's `ChainIdentity` is supplied by the local operator (read from the peer chain's `query identity chain-identity` RPC and pasted into the proposal). The peer chain itself doesn't sign or attest; this is purely informational metadata, set by the local council registering the peer.

### 9.2. Peer-Side DenomMetadata Pre-Registration

On peer activation (after `MsgRegisterPeer` succeeds + IBC channel is ACTIVE), x/federation calls into x/bank to pre-register `DenomMetadata` for the expected IBC vouchers:

```go
// In x/federation keeper, on peer activation. The path is the canonical
// single-hop ICS-20 path; multi-hop is intentionally out of scope (see below).
// Uses ibc-go v10's Denom type (DenomTrace is deprecated upstream).
localIdentity, err := k.identityKeeper.GetChainIdentity(ctx)
if err != nil {
    return err // identity must be initialized; if not, federation activation cannot proceed
}
denom := ibctransfertypes.Denom{
    Base:  peer.Identity.BondDenom,
    Trace: []ibctransfertypes.Hop{{PortId: "transfer", ChannelId: peer.IBCChannel}},
}
ibcSparkDenom := denom.IBCDenom() // "ibc/" + uppercase-hex(sha256(denom.Path()))

bankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
    Description: fmt.Sprintf("%s (IBC voucher on %s), sourced from peer chain %s via %s",
        peer.Identity.BondDisplayName, localIdentity.ChainHumanName, peer.ChainID, peer.IBCChannel),
    DenomUnits: []*banktypes.DenomUnit{
        {Denom: ibcSparkDenom, Exponent: 0},
        {Denom: strings.ToLower(peer.Identity.BondDisplaySymbol) + ".ibc",
         Exponent: peer.Identity.BondDisplayDecimals},
    },
    Base:    ibcSparkDenom,
    Display: strings.ToLower(peer.Identity.BondDisplaySymbol) + ".ibc",
    Name:    peer.Identity.BondDisplayName + " (IBC)",
    Symbol:  peer.Identity.BondDisplaySymbol + ".ibc",  // e.g., "PSPK.ibc"
})
```

The `.ibc` suffix convention signals "wrapped foreign asset, not native local SPARK." Wallets render `PSPK.ibc` instead of `ibc/A1B2C3…`. DREAM is not pre-registered as an IBC denom because DREAM is non-IBC-transferable, so no voucher will ever exist.

**Single-hop only in v1.** Pre-registration covers only the canonical direct-link voucher denom `ibc/HASH(transfer/<channel>/u<bond-symbol>.<peer>)`. If the same peer SPARK arrives via a multi-hop relay path (`transfer/A/transfer/B/u<bond-symbol>.<peer>`), the resulting hash differs and no metadata exists for it; wallets will fall back to rendering `ibc/<hash>`. This is consistent with Spark Dream's bilateral-only sovereignty model, where federation peers connect directly rather than through relay chains. Multi-hop metadata pre-registration is deferred to [§18](#18-future-extensions) and would require either (a) operator-supplied alternate paths in `MsgRegisterPeer`, or (b) a packet-receive observer that opportunistically registers metadata on first sighting.

**Channel re-binding cleanup.** If a peer is re-registered against a new IBC channel (operator response to a channel timeout or relayer rotation), the old `ibcSparkDenom` metadata becomes stale (still indexed under the old hash, no future supply). The re-registration handler in `x/federation` is responsible for unsetting the prior metadata before pre-registering the new one. Implementing this as a no-op when the prior channel is unset preserves the first-registration path.

### 9.3. Peer Identity Updates

Peer identity is **also immutable post-registration on the receiving chain**. If a peer chain rebrands (somehow, see §3.4 for why this is essentially impossible), the local Operations Committee must un-register and re-register the peer at the federation layer.

Un-registration severs the *federation-level* peer record (suspends bridge bindings, removes the peer from federation queries, stops accepting attestation IBC packets at the federation handler level). It does **not** close the underlying ICS-04 channel, which is owned by `ibc-go` and managed independently. In-flight ICS-20 transfers continue to settle so that user funds are never stranded mid-transfer. If the operator also wants to close the channel (e.g., to definitively prevent any future voucher arrivals under the legacy identity), they must explicitly submit `MsgChannelCloseInit` against the channel as a separate, deliberate action.

The cost asymmetry is intentional: re-registration is a multi-message governance dance (new `MsgRegisterPeer` + DenomMetadata pre-registration of the new voucher denom + optional channel close), which forces explicit reconsideration of the bilateral relationship rather than letting peer-identity changes propagate silently.

---

## 10. Queries

Proto file: `proto/sparkdream/identity/v1/query.proto`. The `Query` service:

```protobuf
service Query {
  // ChainIdentity returns the chain's immutable identity record.
  rpc ChainIdentity(QueryChainIdentityRequest) returns (QueryChainIdentityResponse);

  // BondDenom is a convenience wrapper returning just the native gas/staking denom.
  rpc BondDenom(QueryBondDenomRequest) returns (QueryBondDenomResponse);

  // DreamDenom is a convenience wrapper returning just the native DREAM denom.
  rpc DreamDenom(QueryDreamDenomRequest) returns (QueryDreamDenomResponse);
}
```

The convenience wrappers exist because clients commonly want just a denom string, not the full identity record. They are pure projections, no separate storage, no caching concerns.

There is no `Params` query, x/identity has no params (see §4.2).

### 10.1. Pre-`InitGenesis` query behavior

CLI helpers (e.g., `sparkdreamd genesis identity show`) can run against a partially-constructed genesis where x/identity's `InitGenesis` has not yet been invoked. In that state, the `Identity` collection is empty. The keeper accessor contract (§3.5):

- `BondDenom` / `DreamDenom`: panic. These are designed for hot-path use after genesis; calling them pre-init is a programming error.
- `GetChainIdentity`: returns `(types.ChainIdentity{}, ErrIdentityNotInitialized)`. CLI tooling that legitimately runs pre-init uses this accessor.

The query RPCs (`ChainIdentity`, `BondDenom`, `DreamDenom`) all call `GetChainIdentity` internally and return gRPC `NotFound` (with `ErrIdentityNotInitialized` as the wrapped detail) if identity is not yet initialized. They never panic in response to a query. This shields CLI / REST clients from process-level crashes when querying a stale or unfinished genesis.

---

## 11. Validation Rules

Validation is split across two methods. `ChainIdentity.Validate()` covers all intrinsic field rules and is callable from anywhere (including the sentinel-rewrite hook in §7.3, which runs before `InitGenesis`). `ChainIdentity.ValidateAgainstChainID(chainID string, allowMismatch bool) error` covers the chain-id consistency check (§11.1) and is callable only from `InitGenesis`, where `ctx.ChainID()` is available.

```go
// Intrinsic-field validation (table below).
func (c ChainIdentity) Validate() error { ... }

// Cross-check against consensus chain_id (§11.1). Returns nil if
// allowMismatch is true OR the consistency check passes.
func (c ChainIdentity) ValidateAgainstChainID(chainID string, allowMismatch bool) error { ... }
```

Intrinsic rules, enforced by `Validate()` and called from both the sentinel-rewrite hook and `InitGenesis`:

| Field | Rule | Error |
|---|---|---|
| `chain_human_name` | 1-32 chars, printable ASCII; `s == strings.TrimSpace(s)` enforced (no leading/trailing whitespace) | `ErrInvalidChainName` |
| `chain_ticker_prefix` | regex `^[A-Z]{2,5}$` | `ErrInvalidTickerPrefix` |
| `bond_denom` | matches SDK denom regex AND `^u[a-z]{2,5}\.[a-z][a-z0-9-]{2,15}$` (covers bond-symbols of length 3-5 under the §15 derivation) | `ErrInvalidBondDenom` |
| `bond_display_symbol` | regex `^[A-Z0-9]{3,8}$`; must start with a letter | `ErrInvalidBondSymbol` |
| `bond_display_name` | 1-64 chars, printable ASCII; `s == strings.TrimSpace(s)` enforced | `ErrInvalidBondDisplayName` |
| `bond_display_decimals` | `[0, 18]` | `ErrInvalidDecimals` |
| `dream_denom` | matches SDK denom regex AND `^u[a-z]{2,5}\.[a-z][a-z0-9-]{2,15}$` (same shape rule as `bond_denom`) | `ErrInvalidDreamDenom` |
| `dream_display_symbol` | regex `^[A-Z0-9]{3,8}$`; must start with a letter | `ErrInvalidDreamSymbol` |
| `dream_display_name` | 1-64 chars, printable ASCII; `s == strings.TrimSpace(s)` enforced | `ErrInvalidDreamDisplayName` |
| `dream_display_decimals` | `[0, 18]` | `ErrInvalidDecimals` |
| `bond_denom != dream_denom` | distinct denoms | `ErrDenomCollision` |
| `bond_display_symbol != dream_display_symbol` | distinct symbols | `ErrSymbolCollision` |
| `founded_at` | `> 0` | `ErrInvalidFoundedAt` |

The denom regexes (bond and dream share one shape rule) are **deliberately strict**: they require the `u` prefix (signals micro-unit base denom) and a dot-prefixed chain suffix. This prevents bare `uspark`/`udream` and other ambiguous-with-Cosmos-defaults values from being accepted. A chain that genuinely wants a different base format (e.g., no `u` prefix) must update this spec and the validation regex in the same PR; they should not slip in by lax validation.

"Printable ASCII" for `chain_human_name`, `bond_display_name`, and `dream_display_name` is enforced byte-by-byte (not rune-by-rune): a `for i := 0; i < len(s); i++ { c := s[i]; if c < 0x20 || c > 0x7e { reject } }` loop. This explicitly rejects multi-byte UTF-8 sequences, control characters, and DEL (0x7f). The byte-loop rather than rune-loop is deliberate: it matches what wallet/explorer display layers tend to assume about identity strings.

### 11.1. Chain-id consistency check

Cosmos SDK's `chain_id` (set by `app.Init` from genesis JSON, exposed via `ctx.ChainID()`) is the canonical chain identifier at the consensus layer; `chain_human_name` is the display identifier owned by x/identity. They are independent strings, but in practice they refer to the same chain and operators routinely typo one or the other.

`ChainIdentity.ValidateAgainstChainID(chainID, allowMismatch)` is called from `InitGenesis` (after `Validate()` succeeds) and performs a soft consistency check:

```go
// chainIDStripRE captures common Cosmos chain-id suffix patterns. Group 1
// is the "base" name with any "-<word>" and "-<digits>" suffixes removed:
//   phoenix-1              -> phoenix
//   aurora-mainnet-1       -> aurora
//   cosmoshub-4            -> cosmoshub
//   osmosis-test-5         -> osmosis
//   dydx-mainnet-1         -> dydx
//   noble                  -> noble       (no suffix)
// Chain-ids that don't conform to these patterns fall through with the
// full input as base, which then trips the substring check unless
// allow_chain_id_mismatch is set.
var chainIDStripRE = regexp.MustCompile(`^(.+?)(-[a-z]+)?(-\d+)?$`)

func chainIDBase(s string) string {
    m := chainIDStripRE.FindStringSubmatch(s)
    if len(m) < 2 {
        return s
    }
    return m[1]
}

func (c ChainIdentity) ValidateAgainstChainID(chainID string, allowMismatch bool) error {
    if allowMismatch {
        return nil
    }
    a := strings.ToLower(chainIDBase(chainID))
    b := strings.ToLower(c.ChainHumanName)
    if a == b || strings.Contains(a, b) || strings.Contains(b, a) {
        return nil
    }
    return errorsmod.Wrapf(ErrChainNameInconsistent,
        "chain_human_name=%q must be derivable from chain_id=%q (set GenesisState.allow_chain_id_mismatch=true to override)",
        c.ChainHumanName, chainID)
}
```

The regex is heuristic and intentionally permissive. The override flag (`GenesisState.allow_chain_id_mismatch`, §4.2) exists for legitimate weird-naming cases (e.g., a chain reusing an existing chain_id from a forked predecessor, or operators who prefer a marketing name that doesn't match the chain-id). The flag requires an explicit operator choice rather than a silent typo passing through.

**The substring check is intentionally permissive.** A single-letter `chain_human_name` like `"P"` would pass (`strings.Contains("phoenix", "p") == true`) because the intrinsic `Validate()` already accepts any 1-character name. The check's purpose is catching *typos and copy-paste errors*, not enforcing a minimum-length name relationship. Operators who want a stricter relationship (e.g., "name must equal base") should pick names accordingly; the spec deliberately does not enforce more than the typo-catching minimum because federated chains will have legitimate name/chain-id divergence and the override is the escape hatch for that.

Examples that pass:
- `chain_id = phoenix-1`, `chain_human_name = Phoenix`
- `chain_id = aurora-mainnet-1`, `chain_human_name = Aurora`
- `chain_id = cosmoshub-4`, `chain_human_name = Cosmos Hub` -> fails the substring test; needs `allow_chain_id_mismatch = true`

Examples that fail (without the override):
- `chain_id = phoenix-1`, `chain_human_name = Pheonix` (typo)
- `chain_id = phoenix-1`, `chain_human_name = Aurora` (likely copy-paste error)

---

## 12. Errors

```go
// x/identity/types/errors.go
var (
    ErrInvalidChainName        = errorsmod.Register(ModuleName, 1200, "invalid chain_human_name")
    ErrInvalidTickerPrefix     = errorsmod.Register(ModuleName, 1201, "invalid chain_ticker_prefix")
    ErrInvalidBondDenom        = errorsmod.Register(ModuleName, 1202, "invalid bond_denom")
    ErrInvalidBondSymbol       = errorsmod.Register(ModuleName, 1203, "invalid bond_display_symbol")
    ErrInvalidBondDisplayName  = errorsmod.Register(ModuleName, 1204, "invalid bond_display_name")
    ErrInvalidDreamDenom       = errorsmod.Register(ModuleName, 1205, "invalid dream_denom")
    ErrInvalidDreamSymbol      = errorsmod.Register(ModuleName, 1206, "invalid dream_display_symbol")
    ErrInvalidDreamDisplayName = errorsmod.Register(ModuleName, 1207, "invalid dream_display_name")
    ErrInvalidDecimals         = errorsmod.Register(ModuleName, 1208, "decimals out of range [0, 18]")
    ErrDenomCollision          = errorsmod.Register(ModuleName, 1209, "bond_denom and dream_denom must differ")
    ErrSymbolCollision         = errorsmod.Register(ModuleName, 1210, "bond_display_symbol and dream_display_symbol must differ")
    ErrInvalidFoundedAt        = errorsmod.Register(ModuleName, 1211, "founded_at must be > 0")
    ErrIdentityNotInitialized  = errorsmod.Register(ModuleName, 1212, "chain identity not initialized; genesis bug")
    ErrIdentityImmutable       = errorsmod.Register(ModuleName, 1213, "chain identity is immutable post-genesis")
    ErrIdentityAlreadySealed   = errorsmod.Register(ModuleName, 1214, "sealed genesis identity already present; cannot overwrite")
    ErrChainNameInconsistent   = errorsmod.Register(ModuleName, 1215, "chain_human_name must be derivable from cosmos chain_id")
)
```

`ErrIdentityImmutable` is defensive only; there is no on-chain code path that can return it (mutation methods don't exist). It exists in the error list so that any future PR adding a mutation message must explicitly choose to ignore the error, surfacing intent to reviewers.

---

## 13. Denom Migration Checklist

Migrating the existing codebase to read denoms from x/identity rather than hardcoded literals. Survey at time of writing (Spark Dream `main` branch):

- **59** hardcoded denom literals (`"uspark"` / `"dream"`) in `x/` and `app/` (excluding tests and generated `.pb.go`).
- **247** test scripts under `test/` referencing `uspark` or `udream`.
- **1** global default at [app/app.go:150](app/app.go#L150) (`sdk.DefaultBondDenom = "uspark"`).

### 13.1. Source-code migration

Each hardcoded literal becomes a keeper read. Concrete pattern:

```go
// BEFORE
coin := sdk.NewCoin("uspark", math.NewInt(1_000_000_000))

// AFTER
coin := sdk.NewCoin(k.identityKeeper.BondDenom(ctx), math.NewInt(1_000_000_000))
```

Module-by-module work, in dependency order (modules that other modules depend on first):

| Module | Files / lines (approx) | Notes |
|---|---|---|
| `x/identity` | new module | Source of truth |
| `app/app.go` | line 150 (within `func init()`) | Delete `sdk.DefaultBondDenom = "uspark"`; rely on the sentinel rewrite and per-keeper reads instead (see §14.5) |
| `x/rep` | `types/denoms.go`, `types/key_tag_budget.go`, multiple keeper files | DREAM mint/burn/decay/transfer-tax all read `DreamDenom`; reward denom reads `BondDenom` |
| `x/commons` | `keeper/governance_logic.go`, `keeper/spend_preconditions.go`, `keeper/msg_server_register_group.go`, `keeper/msg_server_update_group_config.go`, `simulation/recurring_spend.go`, `simulation/register_group.go` | Council subsidies, group max-spend coercion, proposal validation |
| `x/name` | `types/keys.go` (`DefaultRegistrationFee`), `simulation/register_name.go`, `simulation/update_name.go` | Registration fee defaults |
| `x/forum` | `types/params.go` (`DefaultFeeDenom`) | Forum fee denom default |
| `x/futarchy` | `keeper/msg_server_create_market.go`, `simulation/create_market.go`, `simulation/redeem.go` | Market liquidity denom |
| `x/federation` | `types/genesis_vals_devnet.go`, `genesis_vals_mainnet.go`, `genesis_vals_testparams.go`, `simulation/helpers.go` | Bridge stake, challenge fee, escalation fee defaults |
| Genesis defaults | every `DefaultGenesis()` function | Replace hardcoded `"uspark"` / `"dream"` with the sentinel literals `identitytypes.BondDenomSentinel` / `DreamDenomSentinel` (rewritten at chain start by §7.3). Use raw `sdk.Coin{Denom: sentinel, ...}` struct construction; see §13.2. |

### 13.2. Genesis defaults handling

Modules whose `Params` carry a denom field (e.g., x/name `RegistrationFee.Denom`, x/forum `FeeDenom`) face a sequencing problem: `DefaultGenesis()` is called before any keeper exists, so it can't read from x/identity.

The canonical solution is the sentinel rewrite specified in [§7.2-§7.3](#72-canonical-mechanism-pre-initgenesis-sentinel-rewrite). Each module's `DefaultGenesis()` ships denom-bearing params with the sentinel literals `%BOND_DENOM%` / `%DREAM_DENOM%`. The chain-start hook (installed in `app.go` as the wrapper around `InitChainer`) rewrites them to the identity-chosen denoms before any module's `InitGenesis` runs. Concretely:

```go
// In each module's DefaultGenesis. Note the raw struct construction:
// sdk.NewCoin would call ValidateDenom and panic on the sentinel literal
// because `%` is not in the SDK's denom regex.
import (
    "cosmossdk.io/math"
    sdk "github.com/cosmos/cosmos-sdk/types"
    identitytypes "sparkdream/x/identity/types"
)

func DefaultGenesis() *GenesisState {
    return &GenesisState{
        Params: Params{
            RegistrationFee: sdk.Coin{
                Denom:  identitytypes.BondDenomSentinel,
                Amount: math.NewInt(1_000_000),
            },
        },
    }
}
```

The module's keeper never sees the sentinel: by the time `InitGenesis` parses the params, the sentinel has been substituted with the real denom. The keeper's runtime code path reads denoms exclusively through `IdentityKeeper.BondDenom(ctx)` (§3.5), so the param-stored denom is informational metadata for tooling / introspection and need not be re-read at every callsite.

`sdk.Coin{Denom: sentinel, ...}` skips the `ValidateDenom` panic that `sdk.NewCoin` would trigger. Any module that *constructs* a Coin from sentinel-bearing params (rather than reading them as opaque storage) must use the same raw-struct pattern. In practice this only matters for `DefaultGenesis()`; runtime construction of Coins always reads the resolved denom from the identity keeper.

**Alternative considered and rejected.** An earlier draft proposed shipping `DefaultGenesis()` with hardcoded `"uspark"` / `"dream"` for backward compatibility, expecting operators to edit `genesis.json` for federated chains. Rejected because it makes the federation case an edge case rather than the default-supported flow, and leaves operators free to forget the migration entirely. The sentinel approach above is the one canonical mechanism the spec commits to.

### 13.3. Test script migration

The ~247 test scripts under `test/` overwhelmingly use `uspark` / `udream` as inline string literals in `jq`, `grep`, and `sparkdreamd` invocations. Migration approach:

1. Each test directory's `common.sh` (or equivalent) sources from a top-level `test/lib/denoms.sh` that defines `BOND_DENOM` and `DREAM_DENOM` shell variables.
2. `test/lib/denoms.sh` reads from the running chain's `query identity chain-identity` output, with a hardcoded fallback for the default `uspark`/`dream` testing chain.
3. Scripts use `$BOND_DENOM` and `$DREAM_DENOM` throughout.

```bash
# test/lib/denoms.sh
BOND_DENOM="${BOND_DENOM:-$(sparkdreamd q identity bond-denom -o json 2>/dev/null | jq -r .denom 2>/dev/null || echo uspark)}"
DREAM_DENOM="${DREAM_DENOM:-$(sparkdreamd q identity dream-denom -o json 2>/dev/null | jq -r .denom 2>/dev/null || echo dream)}"
export BOND_DENOM DREAM_DENOM
```

A mechanical pass over the test scripts (sed-style replace) handles the bulk; review and manual fix-up for the inevitable awkward constructions.

### 13.4. Estimated effort

- New `x/identity` module: **2-3 days** (small surface area, mostly genesis logic).
- Module-by-module denom migration: **1-2 weeks** of careful work with regression test coverage.
- Test script migration: **3-5 days** mechanical + manual review.
- Federation peer identity extension: **2-3 days** (mostly a proto change + keeper plumbing).
- **Total: ~3-4 weeks** of focused work for the full migration.

This is a meaningful refactor. It is more valuable to do once, before federated chains are live, than to retrofit each federated chain spawn.

---

## 14. Security Considerations

### 14.1. Genesis-time-only is a security property, not just a UX choice

If `ChainIdentity` were mutable, every module that caches denom values (for performance) would need invalidation logic, every wallet that has cached metadata would need re-sync paths, and every off-chain indexer would need denom-change observability. The mutable path has dozens of subtle ways to leave a chain in a half-renamed state: some addresses think the bond denom is `upspk.phoenix`, others think it's `upspk.firebird`, and reconciliation is invisible to users. Genesis-only removes the entire class of bugs.

### 14.2. Validation strictness is intentional

The bond-denom regex `^u[a-z]{2,5}\.[a-z][a-z0-9-]{2,15}$` rejects bare `uspark`, `spark`, `SPARK`, `uatom`, etc. This is more restrictive than the Cosmos SDK base denom regex. The intent is to make it impossible to accidentally ship a federated chain that still uses the default `uspark`; operators must consciously pick a chain-specific denom or genesis init fails. The 2-5 chars allowed between `u` and `.` covers bond-symbols of length 3-5 under the §15 derivation; longer symbols require an explicit `--bond-denom`.

### 14.3. Federation peer identity is locally chosen, not attested

A peer chain's `ChainIdentity` carried in `MsgRegisterPeer` is supplied by the *local* operator, not signed by the peer chain. A malicious or mistaken operator could register Phoenix as having the wrong ticker. Mitigations:

- Registration requires Operations Committee approval (multi-signer).
- Public verification: anyone can run `query identity chain-identity` against the peer chain and compare to the registered values. A mismatch is observable and grounds for re-registration.
- The IBC channel itself is the cryptographic anchor; denom matters for display, not for value.

This trade-off matches [x/federation](x-federation-spec.md)'s overall sovereignty-first philosophy: each chain decides what to call its peers locally.

### 14.4. Display-symbol collisions across federation

Two peer chains could pick the same `chain_ticker_prefix` or `bond_display_symbol` ("PSPK" twice). This is not prevented at the protocol level (no global coordinator). It surfaces as a visible UX collision in any chain that registers both peers: wallets show two unrelated tokens with the same name. The local Operations Committee should refuse to register a peer whose identity collides with an already-registered peer. Implementing this as a soft validation warning at registration time is a reasonable extension.

### 14.5. Cosmos SDK `sdk.DefaultBondDenom` global

The current code sets `sdk.DefaultBondDenom = "uspark"` in [app/app.go:150](app/app.go#L150) from inside `func init()`. Go-level `init()` runs at process start, before `main()`, before any genesis file is parsed. There is no way to "set DefaultBondDenom from genesis at init() time"; the genesis path is not even known yet. The spec therefore commits to the strictest option:

**v1 strategy: stop reading `sdk.DefaultBondDenom` from Spark Dream code entirely.**

1. **Remove the `init()` assignment.** Delete the `sdk.DefaultBondDenom = "uspark"` line at [app/app.go:150](app/app.go#L150). The SDK default reverts to the upstream string (`stake`), which is benign as long as nothing in Spark Dream's hot path reads it.
2. **Hot-path audit (script-enforced).** Add a CI grep check that fails on any reference to `sdk.DefaultBondDenom` under `x/`, `app/`, or `cmd/`. Every read becomes a keeper call: `k.identityKeeper.BondDenom(ctx)`. The check enforces the rule for new code without depending on reviewer vigilance.
3. **Third-party leak tolerance.** Third-party SDK modules and tooling may still read `sdk.DefaultBondDenom`. For most consumers (REST genesis-default fallbacks, simulation default coins) the value is informational and harmless. One place to verify by hand: `genutil`'s `validate-genesis` denom-shape checks read the value out of `staking.params.bond_denom` (already correctly rewritten by §7.3) rather than the global, so no action needed in v1. The project does not currently ship a `simapp`-style simulation harness under `cmd/sparkdreamd/` (verified by `find cmd/ -name '*sim*'`), so the simulation-helper concern is N/A; if such a harness is added later, audit it for raw `sdk.DefaultBondDenom` reads and replace with a per-test fixture.
4. **Tests.** Provide a small helper that sets `sdk.DefaultBondDenom` for unit tests that genuinely need it (typically table-test fixtures with raw `Coin` literals predating the migration). Document the helper as "test-only; never call from production code."

### 14.6. Bank DenomMetadata guard (keeper-wrapper approach)

x/bank `Metadata` is governance-mutable through standard x/bank paths. Without intervention, governance could rewrite a description, a display name, or even (technically) the `Symbol` of an x/identity-managed denom, undermining the spec's "immutable" guarantee.

**An ante-handler decorator is insufficient.** Ante decorators run on the tx envelope. Governance-routed messages execute through `msgServiceRouter.Handler(msg).Process(ctx, msg)` from inside the gov module's proposal-execution path; they do not re-traverse the ante chain. So a `bank.MsgSetDenomMetadata` wrapped inside a `MsgExecLegacyContent` or a v0.50 `MsgSubmitProposal` would bypass any ante guard. Since the gov-routed path is the only realistic attack vector (no one can directly sign a `MsgSetDenomMetadata` against the bank authority outside gov), an ante decorator catches nothing real.

**v1 ships a bank-keeper wrapper, defined at the app layer.** The wrapper type lives in `app/bank_guard.go` (alongside [app/service_adapters.go](app/service_adapters.go), which sets the project's convention for keeper-wrapping). It is **not** part of `x/identity` itself: if it were, x/identity would depend on `bankkeeper.Keeper`, violating the leaf-module claim of §1 / §7.5. By living at the app layer, the wrapper composes the bank and identity keepers without either module taking a dependency on the other.

**Import direction:** `app/bank_guard.go` imports `sparkdream/x/identity/keeper`. `sparkdream/x/identity/keeper` must NOT import `app` (or any sibling of `app`), or the package graph cycles. This is the standard leaf-module discipline already followed by the rest of `x/identity` and is enforced by the existing CI `go vet` step.

```go
// app/bank_guard.go
package app

import (
    "context"
    "errors"
    "fmt"
    "strings"

    "cosmossdk.io/collections"
    errorsmod "cosmossdk.io/errors"
    bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
    banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

    identitykeeper "sparkdream/x/identity/keeper"
    identitytypes "sparkdream/x/identity/types"
)

// BankKeeperWithIdentityGuard wraps a bank Keeper and rejects DenomMetadata
// writes that would alter the canonical Symbol/Display/Base of x/identity-
// managed denoms. Description and unit-alias edits are allowed.
//
// Implements bankkeeper.Keeper (the project's chosen interface for bank-keeper
// consumers). All methods other than SetDenomMetaData are forwarded unchanged
// via the embedded interface.
//
// NOTE: this wrapper covers only SetDenomMetaData. If a future Cosmos SDK
// release adds SetDenomMetaDataBatch / UpdateDenomMetadata / etc., those
// methods must also be intercepted; the §16 invariants 3 and 4 catch the
// drift in steady state but the wrapper is the first line of defense. See
// TestBankKeeperHasNoOtherMetadataWriterMethods (§19) for the reflection-
// based assertion that detects new upstream methods.
type BankKeeperWithIdentityGuard struct {
    bankkeeper.Keeper
    identity identitykeeper.Keeper
}

func WrapBankKeeperWithIdentityGuard(bk bankkeeper.Keeper, idk identitykeeper.Keeper) BankKeeperWithIdentityGuard {
    return BankKeeperWithIdentityGuard{Keeper: bk, identity: idk}
}

func (w BankKeeperWithIdentityGuard) SetDenomMetaData(ctx context.Context, meta banktypes.Metadata) {
    sealed, err := w.identity.GetSealedIdentity(ctx)
    switch {
    case errors.Is(err, collections.ErrNotFound):
        // Truly pre-InitGenesis: identity hasn't sealed yet. The only
        // legitimate writer at this stage is identity's own InitGenesis,
        // which is about to write the sealed record + native metadata as a
        // coordinated pair. Pass through.
        w.Keeper.SetDenomMetaData(ctx, meta)
        return
    case err != nil:
        // A real storage error (disk, corruption, etc.). Do not silently
        // pass through; that would let a flaky read launder writes past the
        // guard. Halt instead.
        panic(fmt.Errorf("identity guard: sealed-identity lookup failed: %w", err))
    }
    if meta.Base != sealed.BondDenom && meta.Base != sealed.DreamDenom {
        w.Keeper.SetDenomMetaData(ctx, meta)
        return
    }
    wantSym := sealed.BondDisplaySymbol
    wantDisp := strings.ToLower(sealed.BondDisplaySymbol)
    if meta.Base == sealed.DreamDenom {
        wantSym = sealed.DreamDisplaySymbol
        wantDisp = strings.ToLower(sealed.DreamDisplaySymbol)
    }
    if meta.Symbol != wantSym || meta.Display != wantDisp {
        // Panic, not return-error: SetDenomMetaData has no error return in
        // the bank Keeper interface. Gov's executeProposal wraps msg
        // execution in defer-recover and marks the proposal failed on
        // panic; upgrade handlers halt and surface the bug. This matches
        // how the staking module guards bond_denom writes when called from
        // outside its own genesis path.
        panic(errorsmod.Wrapf(identitytypes.ErrIdentityImmutable,
            "denom %q: cannot alter Symbol (%q -> %q) or Display (%q -> %q) of x/identity-managed denom",
            meta.Base, wantSym, meta.Symbol, wantDisp, meta.Display))
    }
    // identity's own InitGenesis writes match (because both derive from `id`),
    // so the seed values pass cleanly here.
    w.Keeper.SetDenomMetaData(ctx, meta)
}
```

#### Wiring strategy: app-layer wrap + invariant 4 + x/guardian

Native-denom metadata immutability is enforced by three coordinated mechanisms in the v1 chain:

**1. Bank-keeper wrapper at `app.BankKeeper` (app/bank_guard.go).** Catches `SetDenomMetaData` calls that go through `app.BankKeeper`. In practice this covers identity's own genesis-time seed (with the pre-seal branch allowing it) and any Spark Dream chain module wired via the project's `SetBankKeeper` shim pattern. Does NOT catch upstream SDK modules that received `bankkeeper.BaseKeeper` directly via depinject (gov, distribution, etc.) — a `depinject.Provide` approach that would intercept them is not implementable without forking x/bank, see [docs/x-identity-implementation-decisions.md §3](x-identity-implementation-decisions.md) for the full rationale.

**2. `x/guardian` authority gate (x/guardian/keeper/msg_server.go).** Bank, mint, and staking authorities are set to `x/guardian`'s module address at genesis instead of gov's. Gov can no longer invoke `MsgUpdateParams` / `MsgSetSendEnabled` directly on these modules: the authority check fails. Gov instead submits `MsgExec` to guardian with the inner msg packed as `Any`; guardian applies per-msg-type field filters and routes only allowed mutations. Field filters in v1:
  - `mint.MsgUpdateParams`: rejects changes to `inflation_min`, `inflation_max`, `goal_bonded`, `inflation_rate_change`.
  - `staking.MsgUpdateParams`: rejects changes to `bond_denom`.
  - `bank.MsgUpdateParams`, `bank.MsgSetSendEnabled`: no field filter (gov retains full control).

Note: cosmos-sdk v0.53.6's bank module does not expose `MsgSetDenomMetadata` as a gov-routable msg, so a guardian filter for native-denom Symbol/Display protection is unnecessary in this SDK version. The wrapper (1) and invariant (3) cover all reachable write paths.

**3. Invariant 4 (`BankMetadataCanonicalInvariant`, §16) defense in depth.** Compares every native denom's `Metadata.Symbol` and `Metadata.Display` against the sealed identity on every block via the crisis module's `BeginBlocker`. Catches any tampering that bypasses both (1) and (2) — for example a future cosmos-sdk release adding a metadata-writing msg type guardian's allowlist doesn't yet know about. Trips with `stop = true`, halting the chain with a clear diff message.

#### Practical wiring

```go
// app/app.go (excerpt) — after depinject.Inject returns
app.BankKeeper = WrapBankKeeperWithIdentityGuard(app.BankKeeper, app.IdentityKeeper)
app.IdentityKeeper.SetBankKeeper(app.BankKeeper)
app.IdentityKeeper.SetStakingKeeper(app.StakingKeeper)
app.IdentityKeeper.SetMintKeeper(NewMintKeeperAdapter(app.MintKeeper))

app.GuardianKeeper.SetIdentityKeeper(app.IdentityKeeper)
app.GuardianKeeper.SetMintKeeper(NewMintKeeperAdapter(app.MintKeeper))
app.GuardianKeeper.SetStakingKeeper(app.StakingKeeper)
```

```go
// app/app_config.go (excerpt) — bank, mint, staking authority routed via guardian
{
    Name: banktypes.ModuleName,
    Config: appconfig.WrapAny(&bankmodulev1.Module{
        BlockedModuleAccountsOverride: blockAccAddrs,
        Authority: guardianAuthority, // x/guardian module address
    }),
},
{
    Name: stakingtypes.ModuleName,
    Config: appconfig.WrapAny(&stakingmodulev1.Module{
        Authority: guardianAuthority,
    }),
},
{
    Name: minttypes.ModuleName,
    Config: appconfig.WrapAny(&mintmodulev1.Module{
        Authority: guardianAuthority,
    }),
},
```

The guardian module ([x/guardian/](../x/guardian/)) is a small generic authority gate (~250 LOC); it's not specific to identity. The same mechanism protects mint inflation parameters (replacing the previous burn-address authority pattern documented in [security-hardening.md](security-hardening.md) "Immutable Parameters") and staking `bond_denom`. New use cases register a new per-msg-type filter in `msg_server.go`'s switch statement; this is the only file that needs changing to add a new gated msg.

#### Trade-off accepted

The wrapper makes the `bankkeeper.Keeper` value depinject hands to consumers a non-vanilla concrete type (`BankKeeperWithIdentityGuard` instead of `*bankkeeper.BaseKeeper`). Any tooling that type-asserts to `*bankkeeper.BaseKeeper` would see the assertion fail. In practice no such consumer exists in this project (verified by `grep -r "bankkeeper.BaseKeeper" .`); the interface `bankkeeper.Keeper` is what every consumer uses, and the wrapper satisfies it.

The `app.BankKeeper` field in the App struct (whatever the project's existing naming convention; substitute the local field name when implementing) ends up holding the wrapped value, so direct app-level callers also see the guarded version.

### 14.7. `bond_display_decimals` cannot be migrated in-place

`bond_display_decimals` is part of `ChainIdentity` and therefore covered by genesis-only immutability. Real Cosmos chains have historically migrated decimals (Akash did 6 to 18); Spark Dream's design rules this out by construction. The trade-off is intentional, but operators should be aware:

- Picking the wrong `bond_display_decimals` at genesis is unrecoverable without a chain fork.
- The same applies to `dream_display_decimals`.
- `bond_display_decimals = 6` (micro-units) is the universal Cosmos convention and should be chosen unless there is a specific reason not to.
- If a federated chain genuinely needs different decimals (e.g., to match a peer's representation), the genesis tooling (§15) is the single point at which this decision can be made; the helper should refuse non-default values without a `--confirm-non-default-decimals` flag to surface the irrevocability of the choice.

### 14.8. Pre-existing bank balances under collision-prone denoms

The migration assumes a fresh chain start, where `app_state.bank.balances` contains entries only for the identity-chosen denoms (or for unrelated tokens like genesis IBC allocations). If an operator pastes balances under a legacy denom string (e.g., `"uspark"` after picking `bond_denom = upspk.phoenix`), those balances become orphaned: the supply exists in bank state but is not the chain's native staking denom, and no inflation will accrue under that key.

`InitGenesis` does **not** auto-migrate balances. The recommended path is the `validate-genesis` helper (§15) which emits a hard error when `app_state.bank.balances[*].coins[*].denom` references a known-legacy literal that differs from the chosen denoms. Migrating balances after a chain has started is destructive (it requires a coordinated SendCoins / community-pool grant pattern) and is out of scope for v1.


---

## 15. CLI Commands

x/identity has no transactions, only queries:

```
sparkdreamd query identity chain-identity        # full ChainIdentity record (JSON or YAML)
sparkdreamd query identity bond-denom            # convenience: just the bond denom string
sparkdreamd query identity dream-denom           # convenience: just the dream denom string
```

Example output:

```yaml
$ sparkdreamd query identity chain-identity
identity:
  chain_human_name: Phoenix
  chain_ticker_prefix: PHX
  bond_denom: upspk.phoenix
  bond_display_symbol: PSPK
  bond_display_name: Phoenix Spark
  bond_display_decimals: 6
  dream_denom: udream.phoenix
  dream_display_symbol: PDRM
  dream_display_name: Phoenix Dream
  dream_display_decimals: 6
  founded_at: 1735689600
```

For genesis tooling, a helper command (under `sparkdreamd genesis identity ...`) sets the identity in a `genesis.json` being constructed:

```
sparkdreamd genesis identity init \
    --chain-name Phoenix \
    --ticker-prefix PHX \
    --bond-symbol PSPK \
    --dream-symbol PDRM \
    --decimals 6
# Writes app_state.identity.identity into genesis.json and exits.

sparkdreamd genesis identity show
# Prints the identity record currently in genesis.json (or "not initialized").

sparkdreamd genesis identity validate
# Runs ChainIdentity.Validate() against the in-genesis record, plus the
# chain-id consistency check (§11.1) against the top-level chain_id field.
```

`init` semantics:

- Refuses to overwrite an existing `app_state.identity.identity` by default. Overwrite requires **both** `--force` and `--i-mean-it`, with `--i-mean-it` printing a warning to stderr explaining the chain-restart implications. Neither flag is aliased to `-f` / `-y`, so accidental confirmation prompts cannot trigger the path.
- Derives `bond_denom` = `u<lowercase(bond-symbol)>.<lowercase(chain-name)>` and `dream_denom` = `udream.<lowercase(chain-name)>` if `--bond-denom` / `--dream-denom` are not explicitly passed. The bond-symbol must be length 3-5 for the derivation to fit the `bond_denom` regex (2-5 chars between `u` and `.`); longer symbols (6-8) require `--bond-denom` explicitly.
- Sets `founded_at` to the current Unix time unless `--founded-at <unix>` is passed.
- Requires `--confirm-non-default-decimals` when `--decimals` is anything other than `6`, to surface the irreversibility of the choice (see §14.7).

`init` side-effects in the same edit:

- If `app_state.staking.params.bond_denom` is unset or equal to `"uspark"` / `"stake"`, sets it to `identitytypes.BondDenomSentinel` (so the sentinel rewrite at chain start does the right thing). Same for `app_state.mint.params.mint_denom`, `app_state.crisis.constant_fee.denom`, and the denoms in `app_state.gov.params.min_deposit` / `expedited_min_deposit`.
- Does **not** auto-set sentinels in Spark Dream module params (operator must edit those, or use a future `genesis identity wire-sparkdream-modules` subcommand if they prefer mechanical edits).

`validate` exit codes follow Cosmos `validate-genesis` convention: `0` on success, `1` on any failure (hard validation error or chain-id mismatch without `--allow-chain-id-mismatch`). The chain-id consistency warning surfaces in stderr at exit-`1` time so CI logs show the cause without requiring a separate exit code to distinguish "definitely broken" from "probably broken but might be intentional." Operators who legitimately need the mismatch pass `--allow-chain-id-mismatch` to the `validate` subcommand (which sets `GenesisState.allow_chain_id_mismatch = true` in the genesis being validated).

---

## 16. Module Invariants

For `x/identity InvariantCheck`:

1. **Identity initialized.** Both the mutable `Identity` and the `SealedGenesisIdentity` collections contain exactly one record. Empty state on a running chain is a corruption indicator.
2. **Canonical fields never changed.** `Identity.EqualCanonical(SealedGenesisIdentity)` returns true, covering all nine canonical fields enumerated in §6.5 (`bond_denom`, `dream_denom`, `bond_display_symbol`, `dream_display_symbol`, `bond_display_decimals`, `dream_display_decimals`, `chain_human_name`, `chain_ticker_prefix`, `founded_at`). The two collections live under different key prefixes (§5), so this is a comparison between independent storage entries, not a self-comparison. Violation triggers a panic (`stop = true`) and dumps the field-level diff via `Identity.DiffCanonical(sealed)` so operators see exactly which field drifted.
3. **DenomMetadata present.** `x/bank` contains `Metadata` records keyed by `SealedGenesisIdentity.bond_denom` and `SealedGenesisIdentity.dream_denom`. Surfaces an upgrade-handler bug if the metadata is somehow dropped.
4. **DenomMetadata Symbol/Display match sealed values.** For each native denom's `Metadata`, `Symbol == sealed.<BondOr Dream>DisplaySymbol` and `Display == lowercase(sealed.<BondOr Dream>DisplaySymbol)`. Catches any path that bypassed the bank-keeper wrapper from §14.6 (e.g., a future PR that wires raw `bankKeeper` to a new module instead of the guarded version, or forgets to call `SetBankKeeper`).
5. **SDK params aligned.** `staking.Params.bond_denom == sealed.bond_denom` AND `mint.Params.mint_denom == sealed.bond_denom`. Surfaces a governance-update bug that re-pointed staking or mint to a different denom.

   This invariant is *warning-grade*. Governance technically *can* update these params and might do so legitimately during an upgrade (e.g., a coordinated denom rename across staking and identity). Cosmos SDK invariants signal severity through their return value: `func(ctx sdk.Context) (string, bool)`, where the bool is `stop`. Invariant 5 returns `stop = false` on the params-drift case, which causes the crisis module to log the violation and emit it as an event but *not* halt the chain. Invariants 1-4 return `stop = true` because their violations represent actual state corruption or a defeated guard. Operators wire the `invariant_broken` event into their alerting pipeline to surface invariant-5 violations without chain halt.

   **Split-severity within invariant 5.** The implementation below returns `stop = true` only when the sealed-identity lookup itself fails (an actual corruption: the seal collection should always exist on a live chain). For the params-drift case it returns `stop = false`. This keeps the invariant warning-grade for the case governance can legitimately cause, while preserving panic-grade response to actual state corruption.

   ```go
   // x/identity/keeper/invariants.go (sketch)
   func SDKParamsAlignedInvariant(k Keeper, sk StakingKeeper, mk MintKeeper) sdk.Invariant {
       return func(ctx sdk.Context) (string, bool) {
           sealed, err := k.GetSealedIdentity(ctx)
           if err != nil {
               return "identity: sealed lookup failed", true
           }
           bondDenom, _ := sk.BondDenom(ctx)
           mintParams, _ := mk.GetParams(ctx)
           if bondDenom == sealed.BondDenom && mintParams.MintDenom == sealed.BondDenom {
               return "", false
           }
           return fmt.Sprintf("staking.bond_denom=%q or mint.mint_denom=%q diverged from sealed bond_denom=%q",
               bondDenom, mintParams.MintDenom, sealed.BondDenom), false  // stop=false: warn only
       }
   }
   ```

---

## 17. Comparison with Prior Approach (hardcoded `uspark`/`dream`)

| Aspect | Hardcoded literals | x/identity |
|---|---|---|
| Per-chain ticker | Requires source fork for each federated chain | Genesis-time configurable |
| Single source of truth | No: denom is restated in ~59 places | Yes: one record, accessor-only reads |
| Wallet display | Default `uspark` everywhere, all federated chains look identical | Per-chain symbol via `DenomMetadata` |
| IBC voucher origin | Indistinguishable across chains using bare `uspark` | Source chain visible in IBC trace via `u<bond-symbol>.<chain>` suffix |
| Test script portability | Hardcoded `uspark`/`udream` everywhere | `$BOND_DENOM` / `$DREAM_DENOM` shell vars |
| Migration cost | n/a | ~3-4 weeks one-time refactor |
| Risk of post-launch denom rename | Catastrophic in either model, mitigated only by **never renaming** | Same, enforced architecturally by genesis-only |

The prior approach is correct *for a single-chain deployment*. The federation model makes it incorrect. This spec is the migration target.

---

## 18. Future Extensions

Tracked for later spec work; not in v1:

1. **`MsgUpdateNonCanonicalMetadata`**: narrow governance message allowing updates to `bond_display_name`, `dream_display_name`, descriptions, and possibly secondary aliases, but explicitly **not** the denoms or primary symbols. Would also be propagated to `x/bank` `DenomMetadata` via the same write path as genesis. Justification needed: a real use case (e.g., translation, branding refresh) outweighing the complexity cost.
2. **Multilingual display names**: extend `DenomMetadata` aliases with locale tags.
3. **Peer identity attestation**: peer chain signs its `ChainIdentity` with a chain-controlled key (validator-set-derived or genesis-key), so the registering operator carries a verifiable identity blob rather than a self-asserted one. Useful if peer chains start being adversarially mis-registered.
4. **Multi-hop IBC voucher metadata pre-registration**: extend federation peer registration to optionally accept alternate relay paths, or add a packet-receive observer that opportunistically registers metadata for first-seen vouchers. See §9.2.
5. **Identity-driven UI hints**: extend `ChainIdentity` with optional fields like `primary_color_hex`, `logo_ipfs_cid`, `home_url`, surfaced to clients that want chain-customized rendering. Risk: feature creep, limit aggressively.

---

## 19. Test plan

Tests are scattered through the spec by feature; this section is the central index. Implementers should write one Go test (or test group) per row before considering the module complete.

### 19.1. Genesis & Sealing

| Test | Asserts | Spec ref |
|---|---|---|
| `TestGenesisOrderingPlacesIdentityImmediatelyAfterAuth` | `app_config.go` InitGenesis slice contains `identity` at index 2 | §6.3 |
| `TestSentinelRewriteHappensBeforeInitGenesis` | Constructing a genesis with `staking.params.bond_denom = "%BOND_DENOM%"`, running full init, asserts `app.StakingKeeper.BondDenom(ctx) == "upspk.phoenix"` | §6.3 |
| `TestInitGenesisSealedMatchesMetadataWrite` | Snapshots both the sealed identity (step 4) and the bank metadata write (step 5); asserts byte-identical denom/symbol/decimals fields | §6.2 |
| `TestSealGenesisIdempotentOnRestoration` | Calling `sealGenesisIdentity` twice with the same identity succeeds (no-op second call) | §6.5 |
| `TestSealGenesisRejectsTamperedReimport` | Calling `sealGenesisIdentity` with a different `bond_denom` returns `ErrIdentityAlreadySealed` with a field-diff message | §6.5 |
| `TestExportGenesisRefusesOnSealDivergence` | Manually corrupting the mutable `Identity` and calling `ExportGenesis` panics with a canonical-diff message | §6.4 |

### 20.2. Sentinel rewrite hook

| Test | Asserts | Spec ref |
|---|---|---|
| `TestRewriteSentinelsHaltsOnGenutilSentinel` | A genesis with `%BOND_DENOM%` inside `app_state.genutil` fails the rewrite with the "sentinels in gentxs are an operator error" message | §7.3 |
| `TestRewriteSentinelsPreservesAllBankFields` | A genesis with `app_state.bank` containing `balances`, `supply`, `params`, `send_enabled`, and `denom_metadata` survives the rewrite with all non-`denom_metadata` fields byte-identical | §7.3 / §8.1 |
| `TestPurgeLegacyBankMetadataRemovesOnlyLegacy` | A genesis with metadata for `uspark`, `dream`, `uatom`, and the chosen `upspk.phoenix` keeps `uatom` and `upspk.phoenix`, removes `uspark` and `dream` | §8.1 |
| `TestPurgeLegacyBankMetadataPreservesIfChosen` | When `bond_denom = "uspark"` (degenerate test config), the `uspark` metadata is preserved | §8.1 |
| `TestRewriteSentinelsHaltsOnSurvivingSentinel` | A genesis with a sentinel buried in a place the substitution can't reach (synthetic) fails the post-substitution assertion | §7.3 / §7.4 |

### 20.3. Validation

| Test | Asserts | Spec ref |
|---|---|---|
| `TestChainIdentityValidateRejectsBadDenoms` | Table-driven: `uspark`, `spark`, `SPARK`, `uatom`, `dream`, missing dot, missing `u` prefix all rejected with `ErrInvalidBondDenom` | §11 |
| `TestValidateAcceptsCustomDreamPrefix` | Dream denoms with a non-`udream` prefix but the shared shape (`uwish.phoenix`, `udrmz.phoenix`, `uab.phoenix`) validate cleanly | §3.3 / §11 |
| `TestChainIdentityValidateRejectsWhitespace` | `chain_human_name = " Phoenix "` fails with `ErrInvalidChainName` | §11 |
| `TestChainIdentityValidateRejectsCollisions` | `bond_denom == dream_denom` fails with `ErrDenomCollision`; symbol collision fails with `ErrSymbolCollision` | §11 |
| `TestValidateAgainstChainIDAcceptsCommonShapes` | `(phoenix-1, Phoenix)`, `(aurora-mainnet-1, Aurora)` pass | §11.1 |
| `TestValidateAgainstChainIDRejectsTypos` | `(phoenix-1, Pheonix)`, `(phoenix-1, Aurora)` fail with `ErrChainNameInconsistent` | §11.1 |
| `TestValidateAgainstChainIDRespectsBypass` | Same typo cases pass when `allow_chain_id_mismatch = true` | §11.1 |

### 20.4. Bank-keeper wrapper (§14.6)

| Test | Asserts | Spec ref |
|---|---|---|
| `TestWrapperRejectsNativeSymbolTamper` | Directly calling wrapped `SetDenomMetaData` with `meta.Base = sealed.BondDenom` and altered `Symbol` panics with `ErrIdentityImmutable` | §14.6 |
| `TestWrapperAllowsDescriptionEdit` | Same call with original `Symbol`/`Display` but altered `Description` succeeds | §14.6 |
| `TestWrapperAllowsForeignDenom` | Setting metadata for a non-native denom (e.g., an IBC voucher) is unrestricted | §14.6 |
| `TestWrapperPanicsOnStorageError` | A mock identity keeper returning a non-`NotFound` error from `GetSealedIdentity` causes `SetDenomMetaData` to panic (not silently pass through) | §14.6 |
| `TestWrapperPassesThroughPreSeal` | Before `InitGenesis` runs, any metadata write succeeds (the legitimate seed window) | §14.6 |
| **`TestGovProposalCannotAlterIdentityDenomSymbol`** | A passing gov proposal containing `bank.MsgSetDenomMetadata` with the chain's `bond_denom` and a tampered `Symbol` fails proposal execution AND leaves the bank store unchanged. **This is the load-bearing test for the wrapper's security claim.** | §14.6 |
| `TestWrapperSatisfiesAllUpstreamBankInterfaces` | Reflection check: for each consumer module in `go.mod`, its locally-declared bank interface is satisfied by `BankKeeperWithIdentityGuard` | §14.6 |
| `TestBankKeeperHasNoOtherMetadataWriterMethods` | Reflection check: the wrapped `bankkeeper.Keeper` interface contains exactly one method whose name starts with `SetDenomMetaData` (catches upstream additions) | §14.6 |

### 20.5. Invariants

| Test | Asserts | Spec ref |
|---|---|---|
| `TestInvariantsPassAtFreshGenesis` | All five invariants return clean immediately after `InitGenesis` | §16 |
| `TestInvariantDenomDriftPanics` | Manually mutating `Identity.bond_denom` away from sealed causes invariant 2 to return `stop=true` with a panic-grade message | §16 |
| `TestInvariantSymbolDriftPanics` | Bypassing the wrapper and writing a tampered `Symbol` to bank metadata causes invariant 4 to return `stop=true` | §16 |
| `TestInvariantSDKParamsDriftWarns` | A gov-update that re-points `staking.bond_denom` causes invariant 5 to return `stop=false` (logs/emits event, does NOT halt) | §16 |

### 20.6. Federation peer identity (§9)

| Test | Asserts | Spec ref |
|---|---|---|
| `TestPeerActivationRegistersIBCDenomMetadata` | After `MsgRegisterPeer` succeeds and channel activates, bank metadata for the computed `ibc/<hash>` denom exists with `Symbol = "PSPK.ibc"` | §9.2 |
| `TestPeerReRegistrationUnsetsOldMetadata` | Re-registering a peer against a new channel removes the old `ibc/<hash>` metadata before writing the new entry | §9.2 |

### 20.7. CLI helpers (§15)

| Test | Asserts | Spec ref |
|---|---|---|
| `TestGenesisIdentityInitRefusesOverwrite` | `genesis identity init` without `--force --i-mean-it` against a populated genesis exits non-zero | §15 |
| `TestGenesisIdentityInitDerivesDenoms` | `--chain-name Phoenix --bond-symbol PSPK` produces `bond_denom = upspk.phoenix`, `dream_denom = udream.phoenix` | §15 |
| `TestGenesisIdentityValidateExitCodes` | Valid genesis exits 0; bad genesis exits 1; chain-id mismatch without override exits 1 with stderr warning | §15 |
| `TestGenesisIdentityInitWiresSentinels` | After `init`, `app_state.staking.params.bond_denom == BondDenomSentinel` and friends | §15 |

### 20.8. Migration & smoke tests

| Test | Asserts | Spec ref |
|---|---|---|
| `TestNoSDKDefaultBondDenomReads` | CI grep over `x/`, `app/`, `cmd/` finds no references to `sdk.DefaultBondDenom`. Failing on new code is the intended enforcement. | §14.5 |
| `TestAllDefaultGenesisUseSentinels` | Reflection over every module's `DefaultGenesis()` output finds no bare `"uspark"` / `"dream"` denom strings in any `Params` field | §13.2 |
| `TestE2EFreshChainStartsWithFederatedDenoms` | End-to-end: build a genesis with `bond_denom = utest.testnet`, start the chain, send a transfer, verify the denom appears correctly in bank balances and staking | §13 |

The test plan totals roughly 33 named tests. Estimated effort to write all of them: **3-4 days of focused work** on top of the migration estimate in §13.4.

---

## 20. Cross-references

- [x-identity-implementation-decisions.md](x-identity-implementation-decisions.md): as-implemented deviations from this spec, follow-up tracking
- [x-federation-spec.md](x-federation-spec.md): peer registration (extended with `ChainIdentity`)
- [x-rep-spec.md](x-rep-spec.md): DREAM denom consumer (mint/burn/decay/transfer-tax)
- [x-commons-spec.md](x-commons-spec.md): SPARK denom consumer (council treasury, governance)
- [x-dex-spec.md](x-dex-spec.md): `dream_family_denoms` parameter should canonical-source from `IdentityKeeper.DreamDenom`
- [x-dex-design-space.md](x-dex-design-space.md): federated DEX design context
- [docs/architecture.md](architecture.md): overall system architecture
- [docs/tokenomics.md](tokenomics.md): token economics (currently uses bare `SPARK`/`DREAM`; should reference per-chain identity for federated context)
