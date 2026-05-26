# x/guardian

Authority proxy that owns the `Authority` address for sensitive SDK modules (auth, bank, mint, staking, distribution, gov, slashing, consensus) and routes gov-submitted `MsgUpdateParams` (and friends) through per-msg-type field filters before dispatch.

## Why

Cosmos SDK modules expect a single `Authority` address — typically the gov module — that can mutate their params. That model is too coarse:

- `mint.MsgUpdateParams` can rewrite `inflation_min` / `inflation_max` / `goal_bonded`, breaking the chain's monetary policy promises with a single passing gov proposal.
- `distribution.MsgCommunityPoolSpend` lets gov spend directly from the community pool, bypassing x/split's allocation rules.
- `staking.MsgUpdateParams` can rewrite `bond_denom`, breaking the identity contract from x/identity.

`x/guardian` interposes: it becomes the authority on each of those modules, and gov can only reach them via `MsgExec` through guardian. Each routed inner msg goes through a per-msg-type filter that rejects forbidden fields and clamps tunable ones.

## Messages

| Msg | Signer | Purpose |
|---|---|---|
| `MsgExec` | gov authority | Wrap an inner `google.protobuf.Any` Msg, run it through the per-type filter, then dispatch via `baseapp.MessageRouter` with guardian's address overwritten as the inner `Authority`. |

The inner Msg's response is packed back into `Any` in `MsgExecResponse`.

**Allowlist, not denylist.** Unknown inner msg types are rejected with `ErrInnerMsgNotAllowed`. The allowlist is hardcoded — chain upgrade required to add new routable types.

## Queries

| Query | Description |
|---|---|
| `AllowedMsgs` | Sorted list of routable inner msg type URLs |

## Per-Msg-Type Filters

| Inner Msg | Behavior |
|---|---|
| `mint.MsgUpdateParams` | `inflation_min` / `inflation_max` / `inflation_rate_change` / `goal_bonded` / `mint_denom` rejected (`ErrImmutableField`); `blocks_per_year` passes through |
| `staking.MsgUpdateParams` | `bond_denom` rejected; rest pass |
| `bank.MsgSetSendEnabled` | Toggles disabling send on native bond/dream denoms rejected; foreign denoms pass |
| `bank.MsgUpdateParams` | Passthrough |
| `distribution.MsgUpdateParams` | `community_tax` clamped to `[0.05, 0.25]` |
| `distribution.MsgCommunityPoolSpend` | **Hard reject.** Community pool spending must flow through x/split. |
| `gov.MsgUpdateParams` | Floors: `voting_period ≥ 6h`, `expedited_voting_period ≥ 1h`, `quorum ≥ 0.2`, `threshold ≥ 0.5`, `veto_threshold ≥ 0.2` |
| `slashing.MsgUpdateParams` | Floors: `slash_fraction_double_sign ≥ 0.01`, `slash_fraction_downtime ≥ 1e-5`. Ceiling: `signed_blocks_window ≤ 1M` |
| `auth.MsgUpdateParams` | Floors on `tx_size_cost_per_byte`, `sig_verify_cost_ed25519`, `sig_verify_cost_secp256k1` |
| `consensus.MsgUpdateParams` | Floors: `block.max_bytes ≥ 200k`, `block.max_gas` (1M or `-1`), `evidence.max_age_num_blocks ≥ 1k`, `evidence.max_age_duration ≥ 1h` |

On accept, the filter overwrites the inner msg's `Authority` field with guardian's module address and dispatches via the message router. Failures inside the filter return typed errors and roll back the tx.

`bank.MsgSetDenomMetadata` is intentionally absent — Cosmos SDK v0.53.6 doesn't expose it as gov-routable. Native-denom Symbol/Display protection is handled by `BankKeeperWithIdentityGuard` plus x/identity invariant 4 instead.

## State

**None.** Empty genesis, `ConsensusVersion=1`, no invariants. Enforcement is purely through the Msg server.

## Errors

| Code | Name | When |
|---|---|---|
| 1 | `ErrInvalidSigner` | Tx signer is not the configured authority |
| 2 | `ErrInnerMsgNotAllowed` | Inner msg type URL not on the hardcoded allowlist |
| 3 | `ErrImmutableField` | Inner msg sets an immutable field (e.g. `inflation_min`) |
| 4 | `ErrIdentityNotSealed` | Identity-dependent filter ran before genesis |
| 5 | `ErrFilterBoundsViolation` | Inner msg tries to set a tunable field outside the clamp range |
| 6 | `ErrCommunityPoolSpendForbidden` | `distribution.MsgCommunityPoolSpend` hard-rejected |

## Authority

The configured `authority` for guardian is `authtypes.NewModuleAddress(govtypes.ModuleName)` — i.e., the gov module. **Authority is passed as the bare module name `"guardian"`** (not a pre-computed bech32 string) so depinject resolves it *after* `sdk.SetBech32PrefixForAccount` seals the chain's address prefix. Pre-computing the bech32 string at package-var init produced a cosmos-prefix literal that the SDK couldn't parse and silently broke every `MsgUpdateParams` until the wiring was fixed.

## Dependencies

Late-wired (write-once setters) from app.go:

| Setter | Used by filter for |
|---|---|
| `SetIdentityKeeper` | Reading sealed identity (bank-send-enabled filter) |
| `SetMintKeeper` | Reading current mint params (diff-based filter) |
| `SetStakingKeeper` | Reading current staking params |
| `SetDistrKeeper` | Reading current distribution params |
| `SetGovKeeper` | Reading current gov params (floor enforcement) |
| `SetSlashingKeeper` | Reading current slashing params |
| `SetAuthKeeper` | Reading current auth params |

Most non-identity keepers go through app-layer adapters (`NewMintKeeperAdapter`, `NewDistrKeeperAdapterForGuardian`, etc.) because the upstream keepers expose params via `collections.Item` rather than methods.

**Fail-closed:** filters that need an identity/mint/staking keeper return `ErrIdentityNotSealed` / `ErrImmutableField` until wired. Guardian cannot be bypassed by delaying keeper wiring.

## No BeginBlocker / EndBlocker

Pure Msg server. No periodic work.

## No Ante Handler

Authority enforcement is in the Msg server. The ante handler stack is untouched.

## CLI

```bash
# Query the allowed inner msg types
sparkdreamd query guardian allowed-msgs

# Execute an inner msg (requires custom Any encoding; typically used via gov proposal JSON)
sparkdreamd tx guardian exec [path-to-inner-msg.json] --from <gov-policy>
```

The typical flow is: write a gov proposal containing a `MsgExec` whose inner `Any` is e.g. `mint.MsgUpdateParams`. Gov tallies, then on `ExecuteProposal` guardian receives the wrapped Msg, filters the params change, and forwards it to x/mint.
