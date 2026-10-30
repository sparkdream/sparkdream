# x/guardian

Authority proxy that owns the `Authority` address for sensitive SDK modules (auth, bank, mint, staking, distribution, gov, slashing, consensus) and routes gov-submitted `MsgUpdateParams` (and friends) through per-msg-type field filters before dispatch.

## Why

Cosmos SDK modules expect a single `Authority` address — typically the gov module — that can mutate their params. That model is too coarse:

- `mint.MsgUpdateParams` can rewrite `inflation_min` / `inflation_max` / `goal_bonded`, or the `blocks_per_year` divisor of per-block issuance, breaking the chain's monetary policy promises with a single passing gov proposal.
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
| `AllowedMsgs` | Sorted list of the inner msg type URLs guardian recognizes. Every entry is routed through its filter except `distribution.MsgCommunityPoolSpend`, which is listed so the community-pool policy is auditable and is always refused |

## Per-Msg-Type Filters

All bounds are compiled constants in [keeper/msg_server.go](keeper/msg_server.go); changing one needs a chain upgrade.

| Inner Msg | Behavior |
|---|---|
| `mint.MsgUpdateParams` | `inflation_min` / `inflation_max` / `inflation_rate_change` / `goal_bonded` / `mint_denom` locked (`ErrImmutableField`). `blocks_per_year` divides per-block issuance, so it is bounded: inside `[3_153_600, 63_072_000]` (10 s to 0.5 s blocks) and at most ±50% of the current value, at most once per 7 days (the step compares with the stored value, so without the cooldown several messages in one proposal would compound it; the last change time is guardian's only state) |
| `staking.MsgUpdateParams` | `bond_denom` locked. `unbonding_time` in `[7 d, 60 d]` and at least the current consensus `evidence.max_age_duration` (a validator that leaves at once escapes double-sign slashing; a years-long period strands every delegator who unbonds before it is reverted); `max_validators ≥ 20`; `max_entries ≥ 7`; `historical_entries ≤ 100,000`; `min_commission_rate ≤ 0.2` |
| `bank.MsgSetSendEnabled` | Disabling send on, or resetting to default (`use_default_for`), the native bond/dream denoms is rejected; foreign denoms pass |
| `bank.MsgUpdateParams` | `default_send_enabled` must stay `true` (the networks ship `send_enabled: []`, so the native denoms read the default). Legacy `params.send_enabled` entries that disable a denom are rejected |
| `distribution.MsgUpdateParams` | `community_tax` clamped to `[0.05, 0.25]` (required) |
| `distribution.MsgCommunityPoolSpend` | **Hard reject.** Community pool spending must flow through x/split. |
| `gov.MsgUpdateParams` | `voting_period` in `[6h, 28d]`, `expedited_voting_period` in `[1h, 28d]`, `max_deposit_period ≤ 28d`; `quorum` in `[0.2, 0.5]`, `threshold` in `[0.5, 0.67]`, `veto_threshold` in `[0.2, 0.5]`, `expedited_threshold ≥ 0.667` (gov cannot freeze itself or weaken expedited votes); `min_deposit_ratio` a decimal in `[0.001, 1]` (the SDK parses it on every submission and deposit but never validates it) and `min_initial_deposit_ratio` in `[0, 1]`; `min_deposit` at least 5,000,000 base units of the sealed bond denom, `expedited_min_deposit` at least `min_deposit` |
| `slashing.MsgUpdateParams` | `slash_fraction_double_sign` in `[0.01, 0.33]`, `slash_fraction_downtime` in `[1e-5, 0.05]`, `min_signed_per_window` in `[0.05, 0.95]`, `downtime_jail_duration` in `[10m, 30d]`, `signed_blocks_window` in `[100, 1M]` |
| `auth.MsgUpdateParams` | `tx_size_cost_per_byte` in `[1, 100]`, `sig_verify_cost_ed25519` in `[100, 10,000]`, `sig_verify_cost_secp256k1` in `[100, 20,000]` (a cost no signed tx can pay would stop every tx, Technical Council votes included, so not even an on-chain upgrade could repair it), `tx_sig_limit` in `[7, 100]` (every sub-key of a multisig account counts, so a lower limit would lock out existing native-multisig members), `max_memo_characters ≤ 10,000` |
| `consensus.MsgUpdateParams` | `block.max_bytes ≥ 200k`; `block.max_gas` in `[1M, 1B]` (`-1`, unlimited, is refused); `evidence.max_age_num_blocks ≥ 1k`; `evidence.max_age_duration` in `[1h, staking unbonding_time]`; `validator.pub_key_types` must include `ed25519` (every validator key is ed25519, and CometBFT refuses a validator update whose key type is not listed) |

On accept, the filter overwrites the inner msg's `Authority` field with guardian's module address and dispatches via the message router. Failures inside the filter return typed errors and roll back the tx. A decimal field left out of an inner msg is refused (`... is required`) rather than reaching the comparison and panicking.

`bank.MsgSetDenomMetadata` is intentionally absent — Cosmos SDK v0.53.6 doesn't expose it as gov-routable. Native-denom Symbol/Display protection is handled by `BankKeeperWithIdentityGuard` plus x/identity invariant 4 instead.

## State

One item: the time of the last accepted `blocks_per_year` change, for its cooldown. It is not in genesis (guardian has an empty genesis); an export/import only restarts the cooldown. `ConsensusVersion=1`, no invariants. Enforcement is purely through the Msg server.

## Errors

| Code | Name | When |
|---|---|---|
| 1300 | `ErrUnauthorized` | `MsgExec.authority` is not the configured authority (gov) |
| 1301 | `ErrInnerMsgNotAllowed` | Inner msg type URL not on the hardcoded allowlist, or `distribution.MsgCommunityPoolSpend` (hard reject) |
| 1302 | `ErrInnerMsgInvalid` | Inner `Any` is nil or cannot be unpacked |
| 1303 | `ErrImmutableField` | Inner msg changes a locked field or sets a bounded field outside its range; also returned when a needed mint/staking keeper is not wired |
| 1304 | `ErrRoutingFailed` | No handler for the inner msg, or the target handler returned an error |
| 1305 | `ErrIdentityNotSealed` | A filter that needs the sealed identity (bank send-enabled, gov deposit) ran without it |

## Authority

The configured `authority` for guardian is `authtypes.NewModuleAddress(govtypes.ModuleName)` — i.e., the gov module. **Authority is passed as the bare module name `"guardian"`** (not a pre-computed bech32 string) so depinject resolves it *after* `sdk.SetBech32PrefixForAccount` seals the chain's address prefix. Pre-computing the bech32 string at package-var init produced a cosmos-prefix literal that the SDK couldn't parse and silently broke every `MsgUpdateParams` until the wiring was fixed.

## Dependencies

Late-wired (write-once setters) from app.go:

| Setter | Read by |
|---|---|
| `SetIdentityKeeper` | bank `MsgSetSendEnabled` filter (native denoms) and gov filter (deposit denom) |
| `SetMintKeeper` | mint filter (locked fields, `blocks_per_year` ±50% band) |
| `SetStakingKeeper` | staking filter (`bond_denom`) and consensus filter (`evidence.max_age_duration ≤ unbonding_time`) |
| `SetDistrKeeper`, `SetGovKeeper`, `SetSlashingKeeper`, `SetAuthKeeper` | **Not read.** The distribution, gov, slashing and auth filters are stateless clamps against compiled bounds, so no filter depends on current values there. The setters are wired for a future diff-based check |

Most non-identity keepers go through app-layer adapters (`NewMintKeeperAdapter`, `NewDistrKeeperAdapterForGuardian`, etc.) because the upstream keepers expose params via `collections.Item` rather than methods.

**Fail-closed:** filters that need the identity, mint or staking keeper return `ErrIdentityNotSealed` / `ErrImmutableField` until wired. Guardian cannot be bypassed by delaying keeper wiring.

## Outside guardian

Guardian covers only the eight modules above. Gov keeps direct authority, with no guardian filter, over:

- **x/upgrade**: inherently all-powerful (a software upgrade can change anything, guardian included).
- **x/evidence**.
- **IBC core, IBC transfer, and the ICA host and controller** (`app/ibc.go`, gov module address). The ICA host `allow_messages` list decides which msgs a foreign chain's interchain account may run here without any ante decorator, so a change to it deserves the same scrutiny as a guardian-filtered param. It can only narrow the compiled ceiling `app.ICAHostAllowedMsgs` (plain bank / staking / distribution / IBC transfer msgs): the host's router refuses any other type even if the param says `"*"`.
- **Legacy x/params `ParameterChangeProposal`s** (the params module's gov handler).

## No BeginBlocker / EndBlocker

Pure Msg server. No periodic work.

## No Ante Handler

Authority enforcement is in the Msg server. The ante handler stack is untouched.

## CLI

```bash
# Query the allowed inner msg types
sparkdreamd query guardian allowed-msgs

# There is no guardian tx command: MsgExec is signed by the gov authority,
# so it is always the message of a gov proposal.
sparkdreamd tx gov submit-proposal proposal.json --from <key>
```

The typical flow is: write a gov proposal containing a `MsgExec` whose inner `Any` is e.g. `mint.MsgUpdateParams`. Gov tallies, then on `ExecuteProposal` guardian receives the wrapped Msg, filters the params change, and forwards it to x/mint.
