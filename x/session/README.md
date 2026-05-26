# x/session

Unified delegated-authorization registry. One `Grant` record covers four typed payload variants: `SessionKey`, `RecurringPull`, `SpendingAllowance`, `ScheduledOneshot`. Clean-room replacement for `x/authz` + `x/feegrant`. Inspired by EIP-7702 + ERC-7710/7715 on the EVM side.

Full spec: [docs/x-session-spec.md](../../docs/x-session-spec.md). Refactor plan + migration history: [docs/x-session-grant-registry-plan.md](../../docs/x-session-grant-registry-plan.md).

## Overview

A `Grant` is a granter-issued authorization for a grantee to perform some constrained action on the granter's behalf. The payload variant determines what the action is and how it is metered:

| Variant | Action | Metering |
|---|---|---|
| `SessionKey` | Grantee signs scoped txs that are dispatched as the granter | `spend_limit` (gas budget), `expires_at`, `max_exec_count`, per-msg-type allowlist |
| `RecurringPull` | Grantee pulls a fixed periodic SendCoins from granter | Logical period clock with catch-up; per-grant `max_per_epoch_uspark` UTC-day throttle |
| `SpendingAllowance` | Grantee picks recipient+amount per pull within a rolling cap | Refilling per-period cap, `min_pull_amount` floor, optional whitelist |
| `ScheduledOneshot` | Fires once at `fire_at` (Transfer or Exec) | EndBlocker-driven; granter pre-funds a gas-deposit escrow |

Key design properties:
- **Anti-recursion**: hardcoded `NonDelegableSessionMsgs` denylist absolute — no payload-level allowlist can re-enable. Every new x/session signer Msg defaults to denylisted.
- **Bounded allowlist** for `SessionKey`/`ScheduledOneshot.Exec` inner msg types: ceiling (`max_allowed_msg_types`) set at genesis, only expandable via chain upgrade; active list can be narrowed by gov and restored by Ops Committee.
- **Module-bypass entrypoints** gated by `params.authorized_grant_creators` (gov-only allowlist) let trusted modules create/revoke/decline/claim grants on behalf of module accounts that cannot sign user-style txs. Genesis seeds the commons module address so the `Msg*RecurringSpend` wrappers work from block 0.
- **`GrantClaimHook`** (two-method: `PreCheck` + `PostCommit`) lets downstream modules veto claims and atomically record side effects. `PostCommit` is **tx-halting** on error — closes a double-debit window the single-method design would have left open on bank-send retry.
- **Leaf module**: depends only on `x/bank`, `x/auth`, msg router.

## Messages

### Universal lifecycle

| Msg | Signer | Purpose |
|---|---|---|
| `MsgCreateGrant` | granter | Create any grant variant (payload-dispatching umbrella) |
| `MsgRevokeGrant` | granter (or grantee if `SessionKey.allow_self_revoke`) | Cancel grant; refund any held oneshot deposit |
| `MsgDeclineGrant` | grantee | Grantee refuses an offered grant; refund deposit |

### Type-specific actions

| Msg | Signer | Variant | Notes |
|---|---|---|---|
| `MsgExecSession` | grantee | `SessionKey` | Dispatch scoped inner msgs as granter; gas paid from `spend_limit` |
| `MsgClaimRecurringPull` | grantee | `RecurringPull` | Advances logical clock; catch-up across multiple txs |
| `MsgPullAllowance` | grantee | `SpendingAllowance` | Pick recipient + amount within rolling cap |
| `MsgRetryScheduledOneshot` | grantee | `ScheduledOneshot` | Retry after `PAUSED_INSUFFICIENT_FUNDS`; not for triggering FIRED |

### Legacy facades (wire-compatible)

| Msg | Maps to |
|---|---|
| `MsgCreateSession` | `MsgCreateGrant` with `SessionKey` payload |
| `MsgRevokeSession` | `MsgRevokeGrant` |
| `MsgExecSession` | (also accepted directly, see above) |

### Governance

| Msg | Signer | Purpose |
|---|---|---|
| `MsgUpdateParams` | gov authority | Can shrink ceiling but not expand it (chain upgrade required) |
| `MsgUpdateOperationalParams` | gov / ops committee | Active allowlist within ceiling; whitelist of `authorized_grant_creators` |

## Variants in Detail

### `SessionKey`

Ephemeral grantee key that signs `MsgExecSession` for scoped delegation with integrated fee delegation.

- `spend_limit` (Coin, uspark only): gas budget; granter pays gas via `SessionFeeDecorator`.
- `expires_at` (Timestamp): hard expiry.
- `max_exec_count` (uint64): 0 = unlimited.
- `allowed_msg_types` ([]string): subset of the global active allowlist; checked at exec time.
- `allow_self_revoke` (bool, Rev 2): when true, the grantee can revoke other grants under the **same granter** (cross-granter revoke remains impossible).
- Inner msgs cannot nest `MsgExecSession`; signer fields on inner msgs are rewritten to the granter; DREAM-related fields are stripped to prevent unintended commits.

### `RecurringPull`

Granter authorizes a fixed `amount` SendCoins every `period_seconds` to the grantee.

- `next_claim_at` advances by `period_seconds` per successful claim; catch-up across multiple txs is supported.
- Per-grant `max_per_epoch_uspark` self-throttle backed by UTC-day buckets (`epoch = floor(block_time / 86400)`) — no dependency on any other module's epoch.
- Status flips to `PAUSED_INSUFFICIENT_FUNDS` on bank-send failure; back to `ACTIVE` on first successful retry.
- The shipped consumer is x/commons's `Msg*RecurringSpend` wrappers (council policy = granter).

### `SpendingAllowance`

Refilling per-period cap; grantee picks the recipient + amount per pull within the rolling window.

- `cap_per_period` (Coin), `period_seconds`, `min_pull_amount`, optional recipient `whitelist`.
- Period clock advances only on a **successful** pull — a failed validation does not reset the granter's used budget.
- Anti-griefing: recipient ≠ granter, `min_pull_amount` floor.

### `ScheduledOneshot` (Transfer + Exec)

Fires once at `fire_at`, EndBlocker-driven.

- **Gas-deposit escrow** (Rev 4): granter pre-funds `max(ceil(gas_limit * gas_price) + creation_fee, min_deposit)` SPARK to the session module account at creation. Refunded on Revoke/Decline/auto-revoke; sent to fee collector on FIRED.
- Fire path runs in a child `CacheContext` with a fresh gas meter capped at `gas_limit` and **unconditional `defer recover()`** — a buggy or malicious handler can at most consume `gas_limit` gas and one EndBlocker slot. Cannot halt the chain.
- **Exec containment** is the audited `params.allowed_msg_types` allowlist, not a runtime flag — `ContextKeySessionFireInProgress` is informational only.

## Module-Bypass Entrypoints

For trusted modules whose granters are module accounts (e.g., council policies cannot sign user-style txs):

| Keeper method | Purpose |
|---|---|
| `CreateGrantOnBehalfOf` | Create a grant where the granter is a module-controlled address |
| `RevokeGrantInternal` | Internal revoke |
| `DeclineGrantInternal` | Internal decline |
| `ClaimRecurringPullForGrantee` | Internal claim |

All four are gated by the `params.authorized_grant_creators` allowlist. Gov-only (not ops-editable). Genesis default seeds the commons module address.

## `GrantClaimHook` Interface

Two-method interface (`PreCheck` + `PostCommit`) invoked at every on-the-wire transfer (RecurringPull claim, SpendingAllowance pull, ScheduledOneshot fire-transfer).

- `PreCheck` runs before the bank send; an error vetoes the transfer atomically.
- `PostCommit` runs after the bank send; an error is **tx-halting** — closes a double-debit window.
- Exec variant of `ScheduledOneshot` is intentionally not hooked (the bounded allowlist is the contract).

The shipped consumer is x/commons's `SessionClaimHook`, which re-applies `CheckSpendGates` (PreCheck) + `recordEpochSpend` (PostCommit) to council-policy grants — re-using the same activation / term-expiry / per-epoch budget logic that gates direct council spends.

## State (Collections)

| Collection | Key | Description |
|---|---|---|
| `Params` | Item | Module parameters (ceiling + active allowlist + authorized_grant_creators) |
| `Grants` | `grant_id` | Primary grant store |
| `GrantsByGranter` | `(granter, grant_id)` | Granter-side index |
| `GrantsByGrantee` | `(grantee, grant_id)` | Grantee-side index + ante handler lookup |
| `GrantsByExpiration` | `(expires_at_unix, grant_id)` | EndBlocker expiry sweep |
| `OneshotsByFireAt` | `(fire_at_unix, grant_id)` | EndBlocker oneshot fire queue |

The legacy `Sessions`-keyed collections remain for wire-compatible reads via `MsgCreateSession` / `MsgRevokeSession` / `MsgExecSession`. Internally these dispatch through the unified `Grant` storage.

## EndBlocker

Three ordered, independently-capped passes:

1. **Fire scheduled oneshots** in `(fire_at_unix ASC, grant_id ASC)` order. Each fire runs in a child `CacheContext` with capped gas meter + `defer recover()`.
2. **Auto-revoke paused oneshots** past TTL (default 7d) and refund deposits.
3. **Expire grants** past `expires_at` and refund any held oneshot deposit.

Each pass is rate-limited; cap is configurable to bound block-time impact.

## Ante Handler

`SessionFeeDecorator` intercepts `MsgExecSession`:
1. Rejects mixed transactions (only `MsgExecSession` msgs allowed in the tx)
2. All `MsgExecSession` msgs in the tx must reference the same granter
3. Every session is non-expired and within `max_exec_count`
4. Fee budget sufficient
5. If `spend_limit > 0`, transfers fees from granter to `fee_collector` and sets a context flag so `SkipIfFeePaidDecorator` skips standard fee deduction

## Non-Delegable Messages

Hardcoded denylist — absolute, no payload-level allowlist can re-enable:

- `/sparkdream.session.v1.MsgCreateSession`
- `/sparkdream.session.v1.MsgCreateGrant`
- `/sparkdream.session.v1.MsgExecSession`
- `/sparkdream.session.v1.MsgClaimRecurringPull`
- `/sparkdream.session.v1.MsgPullAllowance`
- `/sparkdream.session.v1.MsgRetryScheduledOneshot`
- `/sparkdream.session.v1.MsgUpdateParams`
- `/sparkdream.session.v1.MsgUpdateOperationalParams`

`MsgRevokeGrant` / `MsgRevokeSession` is intentionally NOT denylisted; gated by `SessionKeyPayload.allow_self_revoke` instead.

## Queries

| Endpoint | Description |
|---|---|
| `Params` | Module parameters |
| `Session` | Single session by `(granter, grantee)` (legacy facade) |
| `SessionsByGranter` | All sessions for a granter (paginated) |
| `SessionsByGrantee` | All sessions for a grantee (paginated) |
| `Grant` | Single grant by `grant_id` |
| `GrantsByGranter` | All grants for a granter (paginated) |
| `GrantsByGrantee` | All grants for a grantee (paginated) |
| `AllowedMsgTypes` | Ceiling and active message-type allowlists |

## CLI

```bash
# Queries
sparkdreamd query session params
sparkdreamd query session grant <grant-id>
sparkdreamd query session grants-by-granter <granter>
sparkdreamd query session grants-by-grantee <grantee>
sparkdreamd query session allowed-msg-types

# Legacy session facades
sparkdreamd query session session <granter> <grantee>
sparkdreamd query session sessions-by-granter <granter>
sparkdreamd query session sessions-by-grantee <grantee>

# Tx (legacy SessionKey path; full grant CLIs require custom Any encoding)
sparkdreamd tx session create-session <grantee> <allowed-msg-types> <spend-limit> <expiration> <max-exec-count>
sparkdreamd tx session revoke-session <grantee>
```

`MsgCreateGrant` / `MsgExecSession` / `MsgClaimRecurringPull` / `MsgPullAllowance` / `MsgRetryScheduledOneshot` require custom Any-encoded message construction and are not exposed as simple CLI commands.

## Dependencies

| Module | Usage |
|---|---|
| `x/auth` | Account lookup for signer validation |
| `x/bank` | Fee transfers, oneshot deposit escrow, claim transfers |
| Message Router | Inner message dispatch (late-wired via `SetRouter`) |
| `x/commons` (via `SessionClaimHook`) | Late-wired via `SetClaimHooks` so council-policy grants flow through `CheckSpendGates` / `recordEpochSpend` |

No modules depend on x/session — it is a leaf module with zero cycle risk. The `SessionClaimHook` wiring is a downstream-to-upstream injection: x/session declares the interface; x/commons supplies the implementation.
