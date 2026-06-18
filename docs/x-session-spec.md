# x/session — Delegated Authorization Registry

## 1. Abstract

The `x/session` module is the chain's **unified registry for delegated authorization** — any "I authorize you to do X on my behalf, under these constraints" primitive. One `Grant` record carries four typed payload variants, sharing one storage backbone, one revocation surface, and one anti-recursion denylist:

| Variant | Use case | Action message |
|---|---|---|
| `SessionKey` | Ephemeral keys for fluid UI (post / reply / react / vote without wallet popups) | `MsgExecSession` |
| `RecurringPull` | Periodic SendCoins from granter to grantee (subscriptions, salaries, allowances) | `MsgClaimRecurringPull` |
| `SpendingAllowance` | Refilling per-period cap; grantee picks recipient + amount within budget | `MsgPullAllowance` |
| `ScheduledOneshot` | Single fire-once action at `fire_at` (Transfer or any allowlisted msg) | EndBlocker-fired |

Inspired by [EIP-7702 (Set Code for EOAs)](https://eips.ethereum.org/EIPS/eip-7702) and the [ERC-7710 / ERC-7715](https://eips.ethereum.org/EIPS/eip-7710) permission-delegation standards converging on the EVM side; Cosmos accounts can't host code so we host the policies in a module-resident registry instead.

Universal lifecycle messages: `MsgCreateGrant` (payload-dispatching umbrella), `MsgRevokeGrant`, `MsgDeclineGrant`. Type-specific actions per the table above plus `MsgRetryScheduledOneshot` for the paused-oneshot recovery path. Legacy `MsgCreateSession` / `MsgRevokeSession` remain as wire-compatible facades, internally writing a SESSION_KEY-type `Grant`.

**Design philosophy:** Build exactly what delegated authorization needs, nothing more. Unlike `x/authz` (general-purpose authorization with recursive execution, typed authorizations, and complex grant hierarchies), x/session is purpose-built for the four authorization patterns above.

**Why not x/authz + x/feegrant?**
- **Licensing risk**: Both are extracted Go modules (`cosmossdk.io/x/authz`, `cosmossdk.io/x/feegrant`) that can be independently relicensed, as has already happened with at least one other extracted SDK module.
- **Overengineered for session keys**: The session-key use case alone uses ~10% of authz's surface area. No need for `GenericAuthorization`, `TypedAuthorization`, `SendAuthorization`, `StakeAuthorization`, recursive `MsgExec`, or the full grant interface hierarchy.
- **Separate fee module**: x/feegrant is a separate module with its own state, params, and pruning — unnecessary complexity when fee delegation is a single field on the session-key payload.
- **Security surface**: x/authz's recursive `MsgExec` is explicitly blocked in `x/commons` `ForbiddenMessages` because it bypasses council permission filters. x/session's `MsgExecSession` is non-recursive by design and uses an allowlist-only model with no blocklist to maintain.
- **Four authorization shapes in one place**: x/feegrant only addresses fee-on-behalf; ERC-7710-style spending allowances and EIP-7702-style scheduled actions have no first-class SDK equivalent. Hosting all four variants under one `Grant` record collapses what would otherwise be four separate modules into one.

> **Design history.** The refactor from session-keys-only to the unified registry is specified in [docs/x-session-grant-registry-plan.md](x-session-grant-registry-plan.md) (Rev 4, P1-P8 phased rollout). This spec describes the post-refactor module surface. Sections 3-10 below preserve the SessionKey-variant detail from the original spec; sections 11-14 cover the new variants. Sections 15-17 are the events stability declaration, CLI reference, and migration / deprecation notes.

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/bank` | Fee deduction from granter's account (gas delegation) |
| `x/auth` | Account lookup and address validation |
| `baseapp` | `MsgServiceRouter` for dispatching inner messages to target module handlers |

x/session is a **leaf module** — no other module depends on it. Zero cycle risk.

---

## 3. Core Concepts

### 3.1. Session Lifecycle

```
User Wallet (granter)                    Session Key (grantee, in browser)
    |                                              |
    |-- signs MsgCreateSession ------------------>  |  (only wallet popup)
    |   {grantee, allowed_msgs, spend_limit, exp}  |
    |                                              |
    |                                              |-- MsgExecSession {granter, [MsgCreatePost]}
    |                                              |-- MsgExecSession {granter, [MsgReact]}
    |                                              |-- MsgExecSession {granter, [MsgCreateReply]}
    |                                              |-- ... (no popups, granter pays gas)
    |                                              |
    |  (session expires or user logs out)          |
    |-- signs MsgRevokeSession ------------------>  |  (optional explicit cleanup)
```

### 3.2. Bounded Allowlist Model

Session keys use a **bounded allowlist** with two tiers:

1. **Ceiling** (`max_allowed_msg_types`): The maximum set of message types that *could ever* be session-delegable. Set at genesis, **only expandable via chain upgrade** (binary change). This is the security boundary — reviewed once, locked in code.

2. **Active list** (`allowed_msg_types`): The currently active subset of the ceiling. Governance can **remove** types (emergency disable). The Operations Committee can **re-add** types, but only from the ceiling (restore after emergency). Neither can add a type that isn't in the ceiling.

```
                    ┌─────────────────────────────────────┐
                    │      max_allowed_msg_types           │
                    │      (ceiling — chain upgrade only)  │
                    │                                     │
                    │   ┌─────────────────────────────┐   │
                    │   │   allowed_msg_types          │   │
                    │   │   (active — gov can shrink,  │   │
                    │   │    ops committee can restore) │   │
                    │   └─────────────────────────────┘   │
                    └─────────────────────────────────────┘
```

- **Default-deny**: Any message type not in the active `allowed_msg_types` cannot be delegated.
- **No forbidden list to maintain** — security comes from the ceiling being a finite, reviewed set. Financial, governance, DREAM, params, and infrastructure messages are simply never in the ceiling.
- **New modules** require a chain upgrade to add their messages to the ceiling. This is the right ceremony — the same review process as deploying the module itself.
- **Per-session scoping**: Each session specifies a subset of the active allowlist. A session for blogging doesn't need forum permissions.

### 3.3. Integrated Fee Delegation

Instead of a separate x/feegrant module, fee delegation is built into the session:

- Each session has an optional `spend_limit` (max gas budget in `uspark`)
- The `SessionFeeDecorator` (ante handler) detects `MsgExecSession` and overrides the fee payer to the granter
- Gas fees are deducted from the granter's account and tracked against the session's `spent` counter
- When `spent >= spend_limit`, the session can no longer pay fees (grantee must fund their own gas or session is effectively dead)

If `spend_limit` is zero/empty, no fee delegation occurs — the grantee must have their own funds for gas.

### 3.4. Grantee Account Lifecycle

The grantee (ephemeral session key) does **not** need to pre-exist on chain or hold any funds:

- The frontend generates a fresh keypair and derives a bech32 address
- `MsgCreateSession` only requires the granter's signature — the grantee address is stored as a string
- The first `MsgExecSession` signed by the grantee creates the on-chain account automatically (via `SetPubKeyDecorator` in the ante handler)
- Gas fees come from the granter (via `SessionFeeDecorator`), so the grantee never needs a balance

**Orphan accounts:** Expired sessions leave behind grantee accounts with zero balance. This is acceptable — accounts are cheap in Cosmos SDK, and no cleanup is needed.

### 3.5. Non-Recursive Execution

`MsgExecSession` **cannot contain** another `MsgExecSession`. The message server explicitly rejects nested session execution. This eliminates the entire class of recursion attacks that forced `x/commons` to block `MsgExec` in `ForbiddenMessages`.

---

## 4. State Objects (Protobuf)

### 4.1. Session

```protobuf
message Session {
  string granter = 1;                          // Main wallet address (pays fees, "owns" the session)
  string grantee = 2;                          // Ephemeral session key address
  repeated string allowed_msg_types = 3;       // Scoped message type URLs (subset of global allowlist)
  cosmos.base.v1beta1.Coin spend_limit = 4;    // Max gas budget (0 = no fee delegation)
  cosmos.base.v1beta1.Coin spent = 5;          // Gas consumed so far
  google.protobuf.Timestamp expiration = 6;    // Auto-invalidation time
  google.protobuf.Timestamp created_at = 7;    // When the session was created
  google.protobuf.Timestamp last_used_at = 8;  // Last successful MsgExecSession
  uint64 exec_count = 9;                       // Total successful executions
  uint64 max_exec_count = 10;                  // Execution cap (0 = unlimited)
}
```

**Primary key:** `(granter, grantee)` — one session per pair. To change scope, revoke and recreate.

**Secondary indexes:**
- `grantee → [(granter, grantee)]` — for ante decorator lookup (grantee signs the tx)
- `granter → [(granter, grantee)]` — for "list my active sessions" queries

### 4.2. Params

```protobuf
message Params {
  // Ceiling: the maximum set of message types that could ever be session-delegable.
  // Set at genesis. Only expandable via chain upgrade (MsgUpdateParams from x/gov
  // can shrink this list but CANNOT add types not already present).
  // This is the security boundary.
  repeated string max_allowed_msg_types = 1;

  // Active allowlist: the currently delegable subset of max_allowed_msg_types.
  // Governance (MsgUpdateParams) can remove types. Operations Committee
  // (MsgUpdateOperationalParams) can re-add types, but only from the ceiling.
  repeated string allowed_msg_types = 2;

  // Maximum concurrent active sessions per granter.
  // Prevents a single account from creating unbounded session state.
  uint64 max_sessions_per_granter = 3;          // Default: 10

  // Maximum message types per individual session.
  // Prevents overly broad session grants.
  uint64 max_msg_types_per_session = 4;          // Default: 20

  // Maximum session duration.
  // Prevents permanent delegations (use x/commons governance for that).
  google.protobuf.Duration max_expiration = 5;   // Default: 7 days

  // Maximum gas budget per session.
  // Caps the financial exposure from a compromised session key.
  cosmos.base.v1beta1.Coin max_spend_limit = 6;  // Default: 100_000_000 uspark (100 SPARK)
}
```

### 4.3. SessionOperationalParams

Subset of `Params` updateable by the Commons Council Operations Committee without a full governance proposal.

```protobuf
message SessionOperationalParams {
  // Operations Committee can re-add types to the active allowlist, but ONLY
  // from max_allowed_msg_types (the ceiling). Cannot expand beyond the ceiling.
  // Use case: restore a type that governance emergency-disabled.
  repeated string allowed_msg_types = 1;

  uint64 max_sessions_per_granter = 2;
  uint64 max_msg_types_per_session = 3;
  google.protobuf.Duration max_expiration = 4;
  cosmos.base.v1beta1.Coin max_spend_limit = 5;
}
```

`max_allowed_msg_types` (the ceiling) is **upgrade-only** — neither governance nor the Operations Committee can expand it. The Operations Committee can modify `allowed_msg_types` but only within the ceiling.

---

## 5. Storage Schema

Using Cosmos SDK collections framework:

| Collection | Key | Value | Purpose |
|------------|-----|-------|---------|
| `Sessions` | `(granter, grantee)` | `Session` | Primary session lookup |
| `SessionsByGranter` | `(granter, grantee)` | — | Index: list sessions by granter |
| `SessionsByGrantee` | `(grantee, granter)` | — | Index: ante decorator lookup by grantee |
| `SessionsByExpiration` | `(expiration, granter, grantee)` | — | Index: efficient pruning iterator (avoids full table scan) |
| `Params` | — | `Params` | Module parameters |

---

## 6. Messages

### 6.1. CreateSession

Create a new session key delegation. Signed by the granter (main wallet).

```protobuf
message MsgCreateSession {
  string granter = 1;                          // Main wallet (signer)
  string grantee = 2;                          // Ephemeral session key address
  repeated string allowed_msg_types = 3;       // Message types to delegate
  cosmos.base.v1beta1.Coin spend_limit = 4;    // Gas budget (optional, 0 = no fee delegation)
  google.protobuf.Timestamp expiration = 5;    // When the session expires
  uint64 max_exec_count = 6;                   // Execution cap (optional, 0 = unlimited)
}
```

**Validation:**
1. `granter != grantee` (cannot delegate to self)
2. No existing active session for `(granter, grantee)` pair
3. Granter has not exceeded `max_sessions_per_granter`
4. `len(allowed_msg_types) <= max_msg_types_per_session`
5. Every type in `allowed_msg_types` is in `Params.allowed_msg_types`
6. No type in `allowed_msg_types` is in `NonDelegableSessionMsgs`
7. `expiration > current_time`
8. `expiration - current_time <= max_expiration`
9. `spend_limit <= max_spend_limit` (if non-zero)
10. `spend_limit.denom == "uspark"` (if non-zero)

**Logic:**
1. Validate all fields
2. Create Session with `spent = 0`, `exec_count = 0`, `created_at = block_time`
3. Store Session and update indexes
4. Emit `session_created` event

### 6.2. RevokeSession

Revoke an active session. Signed by the granter.

```protobuf
message MsgRevokeSession {
  string granter = 1;  // Main wallet (signer)
  string grantee = 2;  // Session key to revoke
}
```

**Logic:**
1. Verify session exists for `(granter, grantee)`
2. Delete Session and indexes
3. Emit `session_revoked` event

> **Note:** There is no `MsgRevokeSessionByGrantee`. A compromised session key should not be able to revoke itself (the attacker would just re-grant). The granter revokes from their main wallet, or the session expires naturally.

### 6.3. ExecSession

Execute messages using a session key. Signed by the grantee.

```protobuf
message MsgExecSession {
  string grantee = 1;                 // Session key (signer)
  string granter = 2;                 // Main wallet being acted on behalf of
  repeated google.protobuf.Any msgs = 3;  // Inner messages to execute
}
```

**Validation:**
1. Active session exists for `(granter, grantee)`
2. Session not expired (`expiration > block_time`)
3. `exec_count < max_exec_count` (if max_exec_count > 0)
4. `spent < spend_limit` (if spend_limit > 0 and fee delegation active)
5. All inner messages have type URLs in **both** the session's `allowed_msg_types` **and** the current `Params.allowed_msg_types` (dual validation — allows governance to emergency-revoke a message type across all sessions immediately)
6. No inner message is a session module message (`NonDelegableSessionMsgs` — non-recursive, hardcoded)
7. Every inner message has **exactly one signer** (per `cosmos.msg.v1.signer` proto annotation). Multi-signer messages are rejected — signer rewriting only supports single-signer messages.
8. `len(msgs) > 0` and `len(msgs) <= 10` (batch cap per execution)

**Logic:**
1. Validate session and inner messages per rules above
2. For each inner message:
   a. Unpack the `Any` to a concrete `sdk.Msg`
   b. Verify the message has exactly one signer via `msg.GetSigners()`
   c. Replace the signer/creator field with the granter address (see [Section 17.1](#171-message-signer-rewriting))
   d. **Strip DREAM-related optional fields**: If the message type has optional fields that commit DREAM tokens (e.g., `author_bond` on `MsgCreatePost`, `MsgCreateReply`), zero them out before dispatch. This prevents session keys from creating DREAM commitments even when the base message type is allowlisted for content creation.
   e. Dispatch via `MsgServiceRouter` with granter as the execution context
3. If any inner message fails, the entire `MsgExecSession` reverts (atomic)
4. Update session: `exec_count++`, `last_used_at = block_time`
5. Emit `session_executed` event with executed message type URLs

### 6.4. UpdateParams

Governance parameter update.

```protobuf
message MsgUpdateParams {
  string authority = 1;  // Must be x/gov module account
  Params params = 2;
}
```

**Validation (ceiling enforcement):**
1. `params.max_allowed_msg_types` must be a **subset of** the current `Params.max_allowed_msg_types`. Governance can shrink the ceiling but cannot expand it — expanding requires a chain upgrade that modifies the genesis default or uses a migration handler.
2. `params.allowed_msg_types` must be a **subset of** `params.max_allowed_msg_types`. The active list can never exceed the ceiling.
3. No entry in `max_allowed_msg_types` is in `NonDelegableSessionMsgs`.

### 6.5. UpdateOperationalParams

Operational parameter update by Commons Council Operations Committee.

```protobuf
message MsgUpdateOperationalParams {
  string authority = 1;                              // Operations Committee member or governance authority
  SessionOperationalParams operational_params = 2;
}
```

**Validation (ceiling enforcement):**
1. `operational_params.allowed_msg_types` must be a **subset of** the current `Params.max_allowed_msg_types`. The Operations Committee can restore types to the active list, but only from the ceiling — it cannot introduce types that were never in the ceiling or that governance permanently removed from it.
2. No entry in `allowed_msg_types` is in `NonDelegableSessionMsgs`.

---

## 7. Queries

### 7.1. Session

```protobuf
message QuerySessionRequest {
  string granter = 1;
  string grantee = 2;
}

message QuerySessionResponse {
  Session session = 1;
}
```

### 7.2. SessionsByGranter

```protobuf
message QuerySessionsByGranterRequest {
  string granter = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QuerySessionsByGranterResponse {
  repeated Session sessions = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

### 7.3. SessionsByGrantee

```protobuf
message QuerySessionsByGranteeRequest {
  string grantee = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}

message QuerySessionsByGranteeResponse {
  repeated Session sessions = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

### 7.4. Params

```protobuf
message QueryParamsRequest {}

message QueryParamsResponse {
  Params params = 1;
}
```

### 7.5. AllowedMsgTypes

Convenience query to list both the ceiling and currently active message types.

```protobuf
message QueryAllowedMsgTypesRequest {}

message QueryAllowedMsgTypesResponse {
  repeated string max_allowed_msg_types = 1;  // Ceiling (upgrade-only)
  repeated string allowed_msg_types = 2;      // Currently active (subset of ceiling)
}
```

---

## 8. Security

### 8.1. Bounded Allowlist Security Model

Security relies on a **two-tier allowlist** (see [Section 3.2](#32-bounded-allowlist-model)):

| Tier | Field | Who can shrink | Who can expand | Who sets initially |
|------|-------|---------------|----------------|-------------------|
| **Ceiling** | `max_allowed_msg_types` | Governance (MsgUpdateParams) | Chain upgrade only | Genesis / migration handler |
| **Active** | `allowed_msg_types` | Governance (MsgUpdateParams) | Operations Committee (MsgUpdateOperationalParams), but only from ceiling | Genesis (starts equal to ceiling) |

**Why this works:**

1. The ceiling was reviewed at genesis (or at each chain upgrade that expanded it). Every message in the ceiling is a low-risk content operation. `MsgSend`, `MsgStake`, `MsgTransferDream`, governance messages, etc. are simply **never in the ceiling** — no governance proposal or committee action can add them.

2. Governance can **disable** a message type (remove from active list) as an emergency response. This takes effect immediately across all sessions (Section 6.3 step 5).

3. The Operations Committee can **restore** a disabled type (re-add to active list from ceiling). This is routine operations — no governance proposal needed to recover from a false alarm.

4. **New modules** require a chain upgrade to add their messages to the ceiling. This is the right ceremony — the same review process as deploying the module code itself.

**The one hardcoded rule — anti-recursion:**

The session module's own messages can never appear in the ceiling or active list. This is a structural invariant, not a policy decision:

```go
// Hardcoded: session module messages are never delegable.
// This prevents recursive execution (MsgExecSession containing MsgExecSession)
// and session-key self-management (creating/revoking sessions via session key).
// Enforced in MsgUpdateParams and MsgUpdateOperationalParams handlers.
var NonDelegableSessionMsgs = map[string]bool{
    "/sparkdream.session.v1.MsgCreateSession": true,
    "/sparkdream.session.v1.MsgRevokeSession": true,
    "/sparkdream.session.v1.MsgExecSession":   true,
    "/sparkdream.session.v1.MsgUpdateParams":            true,
    "/sparkdream.session.v1.MsgUpdateOperationalParams": true,
}
```

**Emergency response flow:**

```
1. Problem discovered: MsgFoo is being abused via session keys
2. Governance submits MsgUpdateParams removing MsgFoo from allowed_msg_types
   (or Operations Committee acts faster via MsgUpdateOperationalParams with
    allowed_msg_types excluding MsgFoo)
3. All sessions immediately lose ability to execute MsgFoo
   (ExecSession validates against current allowed_msg_types at execution time)
4. Later, if safe: Operations Committee re-adds MsgFoo from the ceiling
```

### 8.2. Threat Model

| Threat | Mitigation |
|--------|------------|
| **Session key stolen (XSS)** | Grant is time-limited, message-type-scoped, and spend-limited. Attacker can only perform low-risk actions (post/reply/react) with capped gas. Cannot drain funds, governance-vote, or escalate privileges. DREAM fields are stripped from allowlisted messages (Section 6.3 step 2d). |
| **Session key used after logout** | Expiration enforced on-chain. Even if the key persists in browser storage, the session becomes invalid after expiry. Explicit `MsgRevokeSession` on logout provides immediate invalidation. |
| **Malicious frontend** | User reviews the `MsgCreateSession` grant before signing. Only allowlisted content messages can be delegated — financial, governance, and DREAM operations are not on the allowlist. |
| **Gas drain / spam** | `spend_limit` caps total gas. `max_exec_count` caps total executions. Both are enforced per-session. |
| **Recursion / privilege escalation** | `MsgExecSession` cannot contain `MsgExecSession` or `MsgCreateSession`. Hardcoded in `NonDelegableSessionMsgs`, not parameterized. |
| **Permanent delegation** | `max_expiration` parameter (default: 7 days) prevents indefinite sessions. For longer-term delegation, use `x/commons` governance. |
| **State bloat** | `max_sessions_per_granter` limits active sessions. EndBlocker prunes expired sessions (100/block cap). |
| **Fee payer confusion** | `SessionFeeDecorator` only activates for transactions containing exclusively `MsgExecSession` messages. Mixed transactions rejected. Multiple `MsgExecSession` with different granters rejected. |
| **DREAM token exposure** | DREAM financial messages (transfer, stake, bond, challenge, bounty, etc.) are not in the ceiling (`max_allowed_msg_types`) — no governance proposal or committee action can add them. Optional DREAM fields on allowlisted messages (e.g., `author_bond`) are zeroed at dispatch. |
| **Governance tries to add dangerous message** | Impossible. `MsgUpdateParams` can only shrink the ceiling, never expand it. The ceiling is set at genesis/upgrade and contains only reviewed content messages. Adding `MsgSend` or `MsgStake` to the ceiling requires a chain upgrade — the same review process as changing the module code itself. |
| **Allowlist shrinkage** | `ExecSession` validates inner messages against the **current** global allowlist at execution time (Section 6.3 step 5). Governance can emergency-revoke a message type and all sessions lose access immediately — no per-session migration needed. |
| **Multi-signer message injection** | Inner messages with multiple signers are rejected (Section 6.3 step 7). Signer rewriting only supports single-signer messages to prevent ambiguous authorization. |
| **Content griefing via mass-flagging** | `MsgFlagPost` excluded from default allowlist. A compromised session key cannot mass-flag forum content. |

### 8.3. Parameter Ceilings: Why 7 Days and 20 Types

Two ceiling parameters bound how *long* and how *broadly* a single session can act if its key is compromised. These values are deliberately conservative; this section records the reasoning so future governance proposals or chain upgrades can be evaluated against the original design intent rather than re-litigated from scratch.

#### Why `max_expiration` is 7 days

The ceiling is the **worst case**, not the default — granters pick any duration ≤ ceiling when calling `MsgCreateSession`. Raising it does not improve UX for short sessions; it only enlarges the window in which a leaked key can act.

Arguments for keeping 7 days:

1. **Compromise damage scales linearly with the window.** A leaked session key remains usable until expiration. At 7 days a granter is forced into a re-auth checkpoint roughly weekly; at 30 days, the attacker has a month to exhaust the spend budget, mass-spam content, and mass-mutate guild membership. The financial blast radius is bounded by `max_spend_limit`, but the *non-financial* damage (spam, social griefing) scales directly with duration.
2. **Granters retain full discretion below the ceiling.** A kiosk login can request a 1-hour session; a daily-driver wallet can request 7 days. Raising the ceiling changes only the maximum, not the median.
3. **The spec already routes long-term delegation to governance.** Section 8.2 ("Permanent delegation") notes that anything outliving a week should go through `x/commons`, where it gets explicit deliberation rather than a single-signature grant.
4. **Mobile and long-lived devices are exactly the wrong case for a longer ceiling.** Stolen-phone scenarios are the most common physical-compromise vector. A short ceiling forces theft-recovery via re-auth instead of leaving keys live for weeks.

Acceptable upper bound if revisited: **14 days**. Anything longer should be a chain-upgrade decision with explicit governance review, not a parameter change.

#### Why `max_msg_types_per_session` is 20

This is a least-privilege lever, not a UX cap. With 41 types in the genesis ceiling, a per-session limit of 20 forces apps to scope sessions by *purpose*: a blog client needs ~6 types, a guild admin tool needs ~10, a season player needs ~15. A single session that mashes all 41 types together is exactly the failure mode the cap prevents — one compromised key would span content, identity, and social modules.

Arguments for keeping 20:

1. **Per-session scope = compromise blast radius.** A 6-type compromise affects one feature; a 41-type compromise touches five modules. Capping per-session size mechanically limits how broad any single key can be.
2. **`max_sessions_per_granter = 10` already covers power users.** A granter can run 10 concurrent purpose-scoped sessions. Total addressable types per granter = 10 × 20 = 200, far above the 41-type ceiling. There is no real "ran out of types" problem to solve by raising the per-session cap.
3. **Storage cost.** Each session's `allowed_msg_types` list is stored on chain (and indexed three ways via `SessionsByGranter`, `SessionsByGrantee`, `SessionsByExpiration`). A 20-entry list is roughly 1KB per session; doubling it to 41 doubles that cost with no UX benefit, because no legitimate single app needs all 41 types.
4. **Design intent: one session per app/feature.** The "single sign-on for everything" pattern is the case the bounded-allowlist model explicitly discourages, in favor of narrow, short-lived, purpose-scoped grants.

The recent ceiling expansion from 19 → 41 types **strengthens** the case for keeping the per-session cap well below the ceiling, rather than weakening it. Future ceiling expansions should not automatically trigger raising this cap.

### 8.4. Interaction with x/commons ForbiddenMessages

The `x/commons` `ForbiddenMessages` map blocks certain message types from being used in council `AllowedMessages`. For consistency, session messages should be added — councils should not be able to create or execute session keys via proposals:

```go
// Add to x/commons/types/params.go ForbiddenMessages
"/sparkdream.session.v1.MsgCreateSession": true,
"/sparkdream.session.v1.MsgRevokeSession": true,
"/sparkdream.session.v1.MsgExecSession":   true,
```

---

## 9. Ante Handler: SessionFeeDecorator

### 9.1. Existing Ante Chain

The current ante handler chain (in `app/app.go`) already has a pattern for module-paid gas via x/shield:

```
 1. SetUpContextDecorator
 2. ExtensionOptionsDecorator
 3. ValidateBasicDecorator
 4. TxTimeoutHeightDecorator
 5. ValidateMemoDecorator
 6. ConsumeGasForTxSizeDecorator
 7. ShieldGasDecorator            ← x/shield: pays gas from module account, sets ContextKeyFeePaid
 8. SkipIfFeePaidDecorator         ← wraps DeductFeeDecorator, skips if flag set
    └─ DeductFeeDecorator          ← standard fee deduction (FeegrantKeeper = nil)
 9. SetPubKeyDecorator
10. ValidateSigCountDecorator
11. SigGasConsumeDecorator
12. SigVerificationDecorator
13. IncrementSequenceDecorator
14. ProposalFeeDecorator           ← x/commons: min fee for proposals
15. GnoVMAnteHandler
```

The `SessionFeeDecorator` follows the same context-flag pattern as `ShieldGasDecorator`.

### 9.2. SessionFeeDecorator Logic

Inserted **between `ShieldGasDecorator` and `SkipIfFeePaidDecorator`** (position 7.5):

```
SessionFeeDecorator.AnteHandle(ctx, tx):
  1. Scan tx.GetMsgs() for MsgExecSession
  2. If no MsgExecSession found → pass through (next decorator)
  3. If tx contains ANY non-MsgExecSession messages → reject with ErrMixedTransaction
     (mixed transactions not allowed — prevents fee payer ambiguity)
  4. If tx contains multiple MsgExecSession with different granter values
     → reject with ErrMultipleGranters
     (fee payer must be unambiguous)
  5. Extract the single granter address from the MsgExecSession message(s)
  6. For each MsgExecSession:
     a. Look up Session by (granter, grantee)
     b. Verify session exists and is not expired
     c. If session.spend_limit > 0:
        - Verify session.spent + tx_fee <= session.spend_limit
  7. If any session has spend_limit > 0 (fee delegation active):
     a. Transfer tx_fee from granter account → fee_collector module account
     b. Set context flag: ContextKeyFeePaid = true
        (SkipIfFeePaidDecorator will skip the inner DeductFeeDecorator)
  8. Pass to next decorator
```

> **Note:** During `CheckTx` (mempool validation), state changes from step 7a are not persisted. This means concurrent `MsgExecSession` txs from the same session may all pass `CheckTx` seeing the same `spent` value, but will be sequentially validated during `DeliverTx`. This is the same behavior as the standard `DeductFeeDecorator` — no special handling needed.

### 9.3. Post-Handler (Spend Tracking)

After successful transaction execution, the `SessionPostHandler` updates the session's `spent` counter:

```
SessionPostHandler.PostHandle(ctx, tx):
  1. For each MsgExecSession in tx:
     a. Look up Session by (granter, grantee)
     b. session.spent += fee_charged
     c. Store updated Session
```

### 9.4. Ante Handler Chain Integration

```go
// In app/app.go (updated chain)
anteDecorators := []sdk.AnteDecorator{
    ante.NewSetUpContextDecorator(),
    ante.NewExtensionOptionsDecorator(anteOptions.ExtensionOptionChecker),
    ante.NewValidateBasicDecorator(),
    ante.NewTxTimeoutHeightDecorator(),
    ante.NewValidateMemoDecorator(app.AccountKeeper),
    ante.NewConsumeGasForTxSizeDecorator(app.AccountKeeper),
    shieldante.NewShieldGasDecorator(app.ShieldKeeper, app.BankKeeper),
    sessionante.NewSessionFeeDecorator(app.SessionKeeper, app.BankKeeper), // NEW
    shieldante.NewSkipIfFeePaidDecorator(
        ante.NewDeductFeeDecorator(app.AccountKeeper, app.BankKeeper, nil, anteOptions.TxFeeChecker),
    ),
    ante.NewSetPubKeyDecorator(app.AccountKeeper),
    ante.NewValidateSigCountDecorator(app.AccountKeeper),
    ante.NewSigGasConsumeDecorator(app.AccountKeeper, anteOptions.SigGasConsumer),
    ante.NewSigVerificationDecorator(app.AccountKeeper, anteOptions.SignModeHandler),
    ante.NewIncrementSequenceDecorator(app.AccountKeeper),
    commonsante.NewProposalFeeDecorator(app.CommonsKeeper),
    gnovm.NewGnoVMAnteHandler(app.GnoVMKeeper),
}
```

### 9.5. Grantee Account Lifecycle

The grantee (ephemeral key) does **not** need to pre-exist on chain. The account lifecycle is:

1. `SessionFeeDecorator` runs — only needs the **granter** account to exist (for fee transfer). Grantee account may not exist yet.
2. `SetPubKeyDecorator` runs — creates the grantee account and stores its public key (standard SDK behavior for first-time signers).
3. `SigVerificationDecorator` runs — verifies the grantee's signature.

This means the frontend can generate a fresh ephemeral keypair and immediately use it in `MsgExecSession` without any funding or account-creation step.

**Orphan accounts:** Over time, expired sessions leave grantee accounts with zero balance that will never be used again. This is acceptable — accounts are cheap in Cosmos SDK, and the same pattern occurs with any temporary address usage. No cleanup is needed.

---

## 10. EndBlocker

### 10.1. Session Pruning

Expired sessions are pruned every block using the `SessionsByExpiration` index for efficient range queries (no full table scan).

```
EndBlocker(ctx):
  1. Iterate SessionsByExpiration where expiration <= block_time
     (ordered range scan — stops at first non-expired entry)
  2. For each expired session:
     a. Delete from Sessions (primary)
     b. Delete from SessionsByGranter index
     c. Delete from SessionsByGrantee index
     d. Delete from SessionsByExpiration index
     e. Emit session_expired event
  3. Cap iterations at 100 per block to bound gas usage
     (remaining expired sessions are cleaned up in subsequent blocks)
```

The 100-per-block cap ensures EndBlocker gas is predictable even if a large batch of sessions expires simultaneously (e.g., after a popular event where many users created 24h sessions at the same time).

---

## 11. Events

### 11.1. session_created

| Attribute | Value |
|-----------|-------|
| `granter` | Granter address |
| `grantee` | Grantee address |
| `allowed_msg_types` | Comma-separated type URLs |
| `spend_limit` | Gas budget (e.g., `1000000uspark`) |
| `expiration` | RFC3339 timestamp |

### 11.2. session_revoked

| Attribute | Value |
|-----------|-------|
| `granter` | Granter address |
| `grantee` | Grantee address |
| `exec_count` | Total executions before revocation |
| `spent` | Total gas spent before revocation |

### 11.3. session_executed

| Attribute | Value |
|-----------|-------|
| `granter` | Granter address |
| `grantee` | Grantee address |
| `msg_type_urls` | Comma-separated inner message type URLs |
| `exec_count` | Updated execution count |

### 11.4. session_expired

| Attribute | Value |
|-----------|-------|
| `granter` | Granter address |
| `grantee` | Grantee address |
| `exec_count` | Total executions over session lifetime |
| `spent` | Total gas spent over session lifetime |

---

## 12. Genesis Allowlist (Ceiling)

The following message types form the genesis ceiling (`max_allowed_msg_types`) and initial active list (`allowed_msg_types`). This set is the immutable security boundary — it can only be expanded via chain upgrade. Each message was reviewed as low-risk, high-frequency content operations safe for ephemeral key delegation:

### 12.1. x/blog (Content Creation)

| Message Type | Rationale |
|-------------|-----------|
| `/sparkdream.blog.v1.MsgCreatePost` | High-frequency content creation |
| `/sparkdream.blog.v1.MsgUpdatePost` | Editing own posts |
| `/sparkdream.blog.v1.MsgCreateReply` | Replying to posts |
| `/sparkdream.blog.v1.MsgEditReply` | Editing own replies |
| `/sparkdream.blog.v1.MsgReact` | Adding reactions |
| `/sparkdream.blog.v1.MsgRemoveReaction` | Removing own reactions |

> **Implementation note:** `MsgCreatePost` and `MsgCreateReply` have optional `author_bond` fields that lock DREAM. The `ExecSession` handler **must zero out** these fields before dispatch (see [Section 6.3](#63-execsession) step 2d). This allows session keys to create content without accidentally committing DREAM.

**Excluded** (destructive — require main wallet):
- `MsgDeletePost` — permanent tombstone
- `MsgDeleteReply` — permanent tombstone
- `MsgHideReply` / `MsgUnhideReply` — moderation actions

### 12.2. x/forum (Discussion)

| Message Type | Rationale |
|-------------|-----------|
| `/sparkdream.forum.v1.MsgCreatePost` | Creating posts and replies |
| `/sparkdream.forum.v1.MsgEditPost` | Editing own content |
| `/sparkdream.forum.v1.MsgUpvotePost` | Reacting to content |
| `/sparkdream.forum.v1.MsgDownvotePost` | Reacting to content |
| `/sparkdream.forum.v1.MsgFollowThread` | Thread subscription |
| `/sparkdream.forum.v1.MsgUnfollowThread` | Thread unsubscription |
| `/sparkdream.forum.v1.MsgMarkAcceptedReply` | Author marks a solution; a bonded sentinel may instead *propose* one (bond re-checked at dispatch) |
| `/sparkdream.forum.v1.MsgConfirmProposedReply` | Author confirms a sentinel's accepted-reply proposal |
| `/sparkdream.forum.v1.MsgRejectProposedReply` | Author rejects a sentinel's accepted-reply proposal |
| `/sparkdream.forum.v1.MsgHidePost` / `MsgUnhidePost` | Sentinel moderation — admitted as an exception (msg server re-checks the granter's sentinel bond at dispatch, so the session key grants nothing the granter doesn't already hold) |

> **Implementation note:** `forum.MsgCreatePost` also has an optional `author_bond` field that locks DREAM. Like the blog equivalents, this field is zeroed out at dispatch via `DreamFieldsToStrip`.
>
> **Sentinel-privilege exception:** `MsgMarkAcceptedReply` (sentinel-propose branch), `MsgHidePost`, and `MsgUnhidePost` are the only allowlisted forum messages that touch a bonded-role privilege. Each re-checks the granter's sentinel `BondedRole` status at dispatch, so a stolen/limited session key can never act as a sentinel unless the granter already is one, and the curation reward / moderation bond accrue to the granter.

**Excluded** (financial, irreversible, or abuse-prone — require main wallet):
- `MsgDeletePost` — permanent deletion
- `MsgBondRole` / `MsgUnbondRole` (x/rep) — locks/unlocks DREAM against a bonded role (sentinel / curator / verifier)
- `MsgCreateBounty` / `MsgAwardBounty` — escrows DREAM
- `MsgAppealPost` / `MsgAppealThreadLock` / `MsgAppealThreadMove` — initiate dispute resolution and escrow appeal fees
- `MsgFlagPost` — a compromised session key could mass-flag content to grief creators; flagging is deliberate enough to warrant main wallet
- `MsgPinPost` / `MsgUnpinPost` / `MsgPinReply` / `MsgUnpinReply` — governance/sentinel-privileged
- `MsgDisputePin` — initiates a dispute initiative
- `MsgLockThread` / `MsgUnlockThread` / `MsgFreezeThread` / `MsgMoveThread` / `MsgUnarchiveThread` / `MsgDismissFlags` — sentinel actions

### 12.3. x/name (Identity Metadata)

| Message Type | Rationale |
|-------------|-----------|
| `/sparkdream.name.v1.MsgSetPrimary` | Changing primary display name |
| `/sparkdream.name.v1.MsgUpdateName` | Updating name metadata |
| `/sparkdream.name.v1.MsgSetDisplayName` | Setting per-name display label |
| `/sparkdream.name.v1.MsgSetTarget` | Pointing a name at a routing target (consent-gated) |
| `/sparkdream.name.v1.MsgAcceptTarget` | Accepting a name pointed at the grantee |

**Excluded** (governance-gated, fund-locking, or identity-significant):
- `MsgRegisterName` — requires council membership, pays fee
- `MsgTransferName` — transfers ownership of a name (rare, identity-significant)
- `MsgFileDispute` / `MsgContestDispute` / `MsgResolveDispute` — locks DREAM / privileged

### 12.4. x/collect (Collections — Limited)

| Message Type | Rationale |
|-------------|-----------|
| `/sparkdream.collect.v1.MsgReact` | Reacting to collections |
| `/sparkdream.collect.v1.MsgRemoveReaction` | Removing reactions |
| `/sparkdream.collect.v1.MsgUpvoteContent` | Member-only upvote, no funds moved |
| `/sparkdream.collect.v1.MsgUpdateItem` | Metadata edit on own item |
| `/sparkdream.collect.v1.MsgReorderItem` | Reordering own collection items |
| `/sparkdream.collect.v1.MsgSetSeekingEndorsement` | Toggling own collection's discovery flag |

**Excluded** (escrow SPARK deposits, lock DREAM, burn fees, or require bonded role):
- `MsgCreateCollection` / `MsgUpdateCollection` / `MsgDeleteCollection` — escrow / refund / burn SPARK deposits
- `MsgAddItem` / `MsgAddItems` / `MsgRemoveItem` / `MsgRemoveItems` — per-item SPARK deposit + spam tax
- `MsgDownvoteContent` — burns SPARK
- `MsgFlagContent` — non-member spam tax + moderation grief vector
- `MsgEndorseCollection` / `MsgRequestSponsorship` / `MsgSponsorCollection` / `MsgAppealHide` — lock or escrow tokens
- `MsgRateCollection` — requires bonded `ROLE_TYPE_COLLECT_CURATOR`
- `MsgPinCollection` — burns held deposits, trust-level gated
- `MsgAddCollaborator` / `MsgRemoveCollaborator` / `MsgUpdateCollaboratorRole` — affect access control of others; safe but rare, kept out of the ceiling for now

### 12.5. x/season (Gamification UX)

| Message Type | Rationale |
|-------------|-----------|
| `/sparkdream.season.v1.MsgJoinGuild` | Member joins a public guild |
| `/sparkdream.season.v1.MsgLeaveGuild` | Member leaves a guild |
| `/sparkdream.season.v1.MsgAcceptGuildInvite` | Accepting a guild invite |
| `/sparkdream.season.v1.MsgInviteToGuild` | Founder/officer invites a member |
| `/sparkdream.season.v1.MsgRevokeGuildInvite` | Founder/officer/invitee cancels an invite |
| `/sparkdream.season.v1.MsgKickFromGuild` | Founder/officer removes a member |
| `/sparkdream.season.v1.MsgUpdateGuildDescription` | Founder edits guild description |
| `/sparkdream.season.v1.MsgSetGuildInviteOnly` | Founder toggles invite-only mode |
| `/sparkdream.season.v1.MsgPromoteToOfficer` | Founder promotes a member |
| `/sparkdream.season.v1.MsgDemoteOfficer` | Founder demotes an officer |
| `/sparkdream.season.v1.MsgSetDisplayName` | Setting season-scoped display name (cooldown-gated) |
| `/sparkdream.season.v1.MsgSetDisplayTitle` | Equipping an earned title |
| `/sparkdream.season.v1.MsgStartQuest` | Beginning a quest |
| `/sparkdream.season.v1.MsgAbandonQuest` | Abandoning quest progress |
| `/sparkdream.season.v1.MsgClaimQuestReward` | Claiming XP from a completed quest |

**Excluded** (DREAM-burning, fund-locking, identity-significant, or admin-only):
- `MsgCreateGuild` — burns DREAM (guild creation cost)
- `MsgSetUsername` — burns DREAM, reserves a name in x/name
- `MsgReportDisplayName` / `MsgAppealDisplayNameModeration` — lock DREAM
- `MsgClaimGuildFounder` / `MsgTransferGuildFounder` / `MsgDissolveGuild` — rare, identity-significant
- `Msg{Create,Update,Deactivate}Quest`, `Msg{Create,Update,Delete}Achievement`, `Msg{Create,Update,Delete}Title`, `MsgResolveDisplayNameAppeal`, `MsgResolveUnappealedModeration`, `Msg{SetNextSeasonInfo,SkipTransitionPhase,ExtendSeason,RetrySeasonTransition,AbortSeasonTransition}` — admin/governance-only

### 12.6. Modules with no session-delegable messages

x/rep, x/reveal, x/futarchy, x/commons, x/federation, x/shield, x/ecosystem, x/split, x/sparkdream — every signer Msg in these modules either moves SPARK/DREAM, locks a bond, requires bonded-role / committee / council privilege, or is governance/admin infrastructure. They are deliberately not in the ceiling and adding any of them requires a chain upgrade.

---

## 13. Genesis

### 13.1. GenesisState

```protobuf
message GenesisState {
  Params params = 1;
  repeated Session sessions = 2;  // Typically empty at genesis
}
```

### 13.2. Default Genesis Params

At genesis, `max_allowed_msg_types` and `allowed_msg_types` are identical — all ceiling messages are active. The ceiling can only be expanded via chain upgrade.

**`InitGenesis` validation:**
1. `allowed_msg_types` is a subset of `max_allowed_msg_types`
2. No entry in either list is in `NonDelegableSessionMsgs`
3. No duplicate entries in either list
4. Both lists are non-empty (a chain with zero delegable messages means the module is useless — reject)

At genesis the ceiling and active list are identical. Both lists below are abbreviated for readability — see [Section 12](#12-default-allowed-message-types) for the full annotated breakdown by module. The 41 entries are: blog (6), forum (9), name (5), collect (6), season (15).

```json
{
  "params": {
    "max_allowed_msg_types": [
      "/sparkdream.blog.v1.MsgCreatePost",
      "/sparkdream.blog.v1.MsgUpdatePost",
      "/sparkdream.blog.v1.MsgCreateReply",
      "/sparkdream.blog.v1.MsgEditReply",
      "/sparkdream.blog.v1.MsgReact",
      "/sparkdream.blog.v1.MsgRemoveReaction",
      "/sparkdream.forum.v1.MsgCreatePost",
      "/sparkdream.forum.v1.MsgEditPost",
      "/sparkdream.forum.v1.MsgUpvotePost",
      "/sparkdream.forum.v1.MsgDownvotePost",
      "/sparkdream.forum.v1.MsgFollowThread",
      "/sparkdream.forum.v1.MsgUnfollowThread",
      "/sparkdream.forum.v1.MsgMarkAcceptedReply",
      "/sparkdream.forum.v1.MsgConfirmProposedReply",
      "/sparkdream.forum.v1.MsgRejectProposedReply",
      "/sparkdream.name.v1.MsgSetPrimary",
      "/sparkdream.name.v1.MsgUpdateName",
      "/sparkdream.name.v1.MsgSetDisplayName",
      "/sparkdream.name.v1.MsgSetTarget",
      "/sparkdream.name.v1.MsgAcceptTarget",
      "/sparkdream.collect.v1.MsgReact",
      "/sparkdream.collect.v1.MsgRemoveReaction",
      "/sparkdream.collect.v1.MsgUpvoteContent",
      "/sparkdream.collect.v1.MsgUpdateItem",
      "/sparkdream.collect.v1.MsgReorderItem",
      "/sparkdream.collect.v1.MsgSetSeekingEndorsement",
      "/sparkdream.season.v1.MsgJoinGuild",
      "/sparkdream.season.v1.MsgLeaveGuild",
      "/sparkdream.season.v1.MsgAcceptGuildInvite",
      "/sparkdream.season.v1.MsgInviteToGuild",
      "/sparkdream.season.v1.MsgRevokeGuildInvite",
      "/sparkdream.season.v1.MsgKickFromGuild",
      "/sparkdream.season.v1.MsgUpdateGuildDescription",
      "/sparkdream.season.v1.MsgSetGuildInviteOnly",
      "/sparkdream.season.v1.MsgPromoteToOfficer",
      "/sparkdream.season.v1.MsgDemoteOfficer",
      "/sparkdream.season.v1.MsgSetDisplayName",
      "/sparkdream.season.v1.MsgSetDisplayTitle",
      "/sparkdream.season.v1.MsgStartQuest",
      "/sparkdream.season.v1.MsgAbandonQuest",
      "/sparkdream.season.v1.MsgClaimQuestReward"
    ],
    "allowed_msg_types": "<same 41 entries as max_allowed_msg_types>",
    "max_sessions_per_granter": 10,
    "max_msg_types_per_session": 20,
    "max_expiration": "604800s",
    "max_spend_limit": { "denom": "uspark", "amount": "100000000" }
  },
  "sessions": []
}
```

---

## 14. Client Integration

### 14.1. Session Setup (Frontend)

The frontend performs the following on login:

```
1. Generate ephemeral keypair (Ed25519 or Secp256k1)
   - Store private key in browser sessionStorage (cleared on tab close)
   - Derive bech32 address for grantee

2. Construct MsgCreateSession:
   - granter: user's connected wallet address
   - grantee: ephemeral key address
   - allowed_msg_types: based on current page context
     (e.g., blog page → blog messages only)
   - spend_limit: {denom: "uspark", amount: "1000000"}  // 1 SPARK
   - expiration: now + 24 hours

3. User signs and broadcasts (single wallet popup)

4. For all subsequent actions:
   - Construct the target message (e.g., MsgCreatePost)
     with creator = granter address
   - Wrap in MsgExecSession {grantee, granter, [msg]}
   - Sign with ephemeral key
   - Broadcast (no wallet popup)
```

### 14.2. Session Teardown (Frontend)

```
On logout:
  1. Construct MsgRevokeSession {granter, grantee}
  2. Sign with main wallet (wallet popup, but user is already logging out)
  3. Broadcast
  4. Clear ephemeral key from sessionStorage

On tab close (no revoke possible):
  - Session expires naturally on-chain
  - sessionStorage is cleared by browser
```

### 14.3. Single-Message Setup

Unlike `x/authz` + `x/feegrant` which required two separate messages (`MsgGrant` + `MsgGrantAllowance`), x/session needs only **one message**: `MsgCreateSession`. The session IS the authorization AND the fee grant.

The session creation tx is signed by the granter (main wallet). The first `MsgExecSession` using the session key is a **separate transaction** — the session must exist on-chain before it can be used.

```
Tx 1 (granter signs):  MsgCreateSession { granter, grantee, ... }
Tx 2 (grantee signs):  MsgExecSession { grantee, granter, [MsgCreatePost{...}] }
```

### 14.4. CosmJS Integration

Since x/session is a custom module, the frontend needs a thin wrapper around CosmJS:

```typescript
// Simplified example
async function execSession(
  sessionKey: DirectSecp256k1Wallet,
  granter: string,
  msgs: EncodeObject[]
): Promise<DeliverTxResponse> {
  const execMsg = {
    typeUrl: "/sparkdream.session.v1.MsgExecSession",
    value: MsgExecSession.fromPartial({
      grantee: sessionKeyAddress,
      granter: granter,
      msgs: msgs.map(m => Any.fromPartial({
        typeUrl: m.typeUrl,
        value: registry.encode(m),
      })),
    }),
  };
  return client.signAndBroadcast(sessionKeyAddress, [execMsg], fee);
}
```

---

## 15. Error Codes

| Code | Name | Description |
|------|------|-------------|
| 1 | `ErrSessionExists` | Session already exists for (granter, grantee) pair |
| 2 | `ErrSessionNotFound` | No active session for (granter, grantee) pair |
| 3 | `ErrSessionExpired` | Session has passed its expiration time |
| 4 | `ErrMsgTypeNotAllowed` | Message type not in session's allowed list |
| 5 | `ErrMsgTypeForbidden` | Message type is a session module message (`NonDelegableSessionMsgs`) |
| 6 | `ErrMsgTypeNotInGlobalAllowlist` | Message type not in current `Params.allowed_msg_types` |
| 7 | `ErrSpendLimitExceeded` | Session gas budget exhausted |
| 8 | `ErrExecCountExceeded` | Session execution cap reached |
| 9 | `ErrMaxSessionsExceeded` | Granter has too many active sessions |
| 10 | `ErrMaxMsgTypesExceeded` | Too many message types in session grant |
| 11 | `ErrExpirationTooLong` | Requested expiration exceeds `max_expiration` |
| 12 | `ErrSpendLimitTooHigh` | Requested spend limit exceeds `max_spend_limit` |
| 13 | `ErrSelfDelegation` | Cannot create session where granter == grantee |
| 14 | `ErrNestedExec` | MsgExecSession cannot contain MsgExecSession |
| 15 | `ErrEmptyMsgs` | MsgExecSession must contain at least one inner message |
| 16 | `ErrTooManyMsgs` | MsgExecSession contains too many inner messages (max 10) |
| 17 | `ErrMixedTransaction` | Transaction contains MsgExecSession mixed with other message types |
| 18 | `ErrInvalidExpiration` | Expiration is in the past |
| 19 | `ErrMultipleGranters` | Transaction contains MsgExecSession messages with different granters |
| 20 | `ErrMultipleSigners` | Inner message has multiple signers (only single-signer messages supported) |
| 21 | `ErrInvalidDenom` | spend_limit denom is not `uspark` |
| 22 | `ErrCeilingExpansion` | `MsgUpdateParams` attempted to add a type to `max_allowed_msg_types` not already in the current ceiling |
| 23 | `ErrExceedsCeiling` | `allowed_msg_types` contains a type not in `max_allowed_msg_types` |

---

## 16. Comparison with x/authz + x/feegrant

| Aspect | x/authz + x/feegrant | x/session |
|--------|---------------------|-----------|
| **Modules** | 2 separate modules | 1 unified module |
| **Licensing** | Extractable Go modules (relicensable) | Owned by this project |
| **Setup messages** | MsgGrant + MsgGrantAllowance (2 msgs) | MsgCreateSession (1 msg) |
| **Execution** | MsgExec (recursive) | MsgExecSession (non-recursive) |
| **Authorization types** | Generic, Typed, Send, Stake, etc. | Message type URL list (simple) |
| **Fee delegation** | Separate state, separate params, separate pruning | Integrated into session (spend_limit field) |
| **Scope** | General-purpose delegation for any use case | Purpose-built for session keys |
| **Approx. code size** | ~5,000 lines (combined) | ~700-900 lines |
| **Security surface** | Large (recursion, type coercion, generic grants, blocklist maintenance) | Small (flat execution, bounded allowlist with upgrade-only ceiling) |
| **CosmJS support** | Built-in | Thin custom wrapper needed |

---

## 17. Implementation Notes

### 17.1. Message Signer Rewriting

When dispatching inner messages, the message server must rewrite the signer field. In Cosmos SDK v0.53, the signer is determined by the `cosmos.msg.v1.signer` proto annotation (read via `msg.GetSigners()`). The session handler needs to:

1. Unpack the `Any` to a concrete `sdk.Msg`
2. Call `msg.GetSigners()` — **reject if len != 1** (multi-signer messages are not supported)
3. Identify the signer field name from the `cosmos.msg.v1.signer` proto option
4. Set that field to the granter address using proto reflection (`msg.ProtoReflect()`)
5. Validate the rewritten message (`msg.ValidateBasic()`)
6. Dispatch via `MsgServiceRouter.Handler(msg)`

This is the same pattern used by `x/gov`'s proposal execution. Using proto reflection (not Go struct reflection or type assertion) ensures correctness across all message types — the `cosmos.msg.v1.signer` annotation is the canonical source of truth for which field identifies the signer.

**DREAM field stripping** (Section 6.3 step 2d): After signer rewriting but before dispatch, the handler checks for known DREAM-commitment fields and zeros them. This is a short allowlist of (message_type, field_name) pairs maintained in the session module:

```go
var DreamFieldsToStrip = map[string][]string{
    "/sparkdream.blog.v1.MsgCreatePost":  {"author_bond"},
    "/sparkdream.blog.v1.MsgCreateReply": {"author_bond"},
    // Add new entries as modules add optional DREAM fields to allowlisted messages
}
```

### 17.2. Depinject Wiring

x/session has no cross-module keeper dependencies beyond bank, auth, and the msg router. No cycle risk. Standard depinject wiring:

```go
type ModuleInputs struct {
    depinject.In

    Cdc          codec.Codec
    StoreService store.KVStoreService
    AccountKeeper types.AccountKeeper  // x/auth
    BankKeeper    types.BankKeeper     // x/bank
    Router        baseapp.MessageRouter
}
```

### 17.3. Ante Handler Registration

See [Section 9.4](#94-ante-handler-chain-integration) for the full ante handler chain with `SessionFeeDecorator` placement. The `SessionPostHandler` is registered as a post-handler for spend tracking.

### 17.4. Future Extensions

The following features are **not in scope** for v1 but could be added later:

- **Per-message-type rate limits**: Limit reactions to N per hour, posts to M per day within a single session
- **Session activity log query**: Return the last N executions for a session (useful for frontend "session activity" display)
- **Session key rotation**: Replace the grantee key without revoking and recreating (extends session continuity)
- **Multi-granter sessions**: One grantee key authorized by multiple granters (for shared accounts)

---

## 18. Module Invariants

Registered with the `InvariantRegistry` for detection via `crisis` module:

### 18.1. SpendLimitInvariant

For every session where `spend_limit.Amount > 0`: `spent.Amount <= spend_limit.Amount`. Violation indicates a bug in the `SessionPostHandler` spend tracking.

### 18.2. ExecCountInvariant

For every session where `max_exec_count > 0`: `exec_count <= max_exec_count`. Violation indicates a bug in the `ExecSession` handler.

### 18.3. ExpirationInvariant

For every session: `expiration > created_at`. Violation indicates a bug in `CreateSession` validation.

### 18.4. AllowlistSubsetInvariant

`Params.allowed_msg_types` is a subset of `Params.max_allowed_msg_types`. Violation indicates a bug in `MsgUpdateParams` or `MsgUpdateOperationalParams` validation.

### 18.5. SessionAllowlistSubsetInvariant

For every session, every entry in `allowed_msg_types` was in `Params.allowed_msg_types` at session creation time. Note: this invariant **can drift** if governance removes a type from the active allowlist after session creation. This is expected and safe — `ExecSession` validates against the current active allowlist at execution time (Section 6.3 step 5), so drifted sessions simply cannot execute the removed type.

### 18.6. IndexConsistencyInvariant

Every entry in `SessionsByGranter`, `SessionsByGrantee`, and `SessionsByExpiration` has a corresponding entry in the primary `Sessions` collection, and vice versa. Violation indicates a bug in session create/delete/prune logic.

---

## 19. CLI Commands

### 19.1. Transactions

```
sparkdreamd tx session create-session [grantee] [msg-types] [spend-limit] [expiration] --from [granter]

  # Example: 24h session for blog posting with 1 SPARK gas budget
  sparkdreamd tx session create-session \
    sprkdrm1grantee... \
    "/sparkdream.blog.v1.MsgCreatePost,/sparkdream.blog.v1.MsgCreateReply,/sparkdream.blog.v1.MsgReact" \
    1000000uspark \
    24h \
    --from alice

sparkdreamd tx session revoke-session [grantee] --from [granter]

  # Example: revoke session for grantee
  sparkdreamd tx session revoke-session sprkdrm1grantee... --from alice

sparkdreamd tx session exec-session [granter] [msg-json-file] --from [grantee]

  # Example: post via session key
  sparkdreamd tx session exec-session sprkdrm1granter... ./post-msg.json --from session-key
```

### 19.2. Queries

```
sparkdreamd query session session [granter] [grantee]
sparkdreamd query session sessions-by-granter [granter]
sparkdreamd query session sessions-by-grantee [grantee]
sparkdreamd query session params
sparkdreamd query session allowed-msg-types
```

---

# Part 2: Unified Grant Registry

Sections 1-19 above describe the SessionKey variant — the original module surface, preserved for the post-refactor SESSION_KEY-type grant. Sections 20-26 below describe the other three payload variants (RecurringPull, SpendingAllowance, ScheduledOneshot), the unified lifecycle messages (`MsgCreateGrant`, `MsgRevokeGrant`, `MsgDeclineGrant`), the events stability declaration, the full CLI reference, and the migration/deprecation policy.

## 20. The `Grant` record

One record carries every variant. Type is inferred from the payload oneof:

```protobuf
message Grant {
  uint64 id = 1;                            // Auto-incremented from GrantSeq
  string granter = 2;
  string grantee = 3;
  GrantType type = 4;
  GrantStatus status = 5;
  google.protobuf.Timestamp created_at = 6;
  google.protobuf.Timestamp expires_at = 7;
  reserved 8;                                // Was an explicit replay-nonce in Rev 1; SDK account sequence prevents replay.
  string note = 9;                           // 256-char cap

  oneof payload {
    SessionKeyPayload         session_key        = 20;
    RecurringPullPayload      recurring_pull     = 21;
    SpendingAllowancePayload  spending_allowance = 22;
    ScheduledOneshotPayload   scheduled_oneshot  = 23;
  }
}

enum GrantType {
  GRANT_TYPE_UNSPECIFIED = 0;
  GRANT_TYPE_SESSION_KEY = 1;
  GRANT_TYPE_RECURRING_PULL = 2;
  GRANT_TYPE_SPENDING_ALLOWANCE = 3;
  GRANT_TYPE_SCHEDULED_ONESHOT = 4;
}

enum GrantStatus {
  GRANT_STATUS_UNSPECIFIED = 0;
  GRANT_STATUS_ACTIVE = 1;
  GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS = 2;
  GRANT_STATUS_DECLINED = 3;
  GRANT_STATUS_REVOKED = 4;
  GRANT_STATUS_COMPLETED = 5;
  GRANT_STATUS_FIRED = 6;
}
```

**Storage** (from [x/session/keeper/keeper.go](../x/session/keeper/keeper.go)):

| Collection | Key | Value | Purpose |
|---|---|---|---|
| `Grants` | `uint64` (grant_id) | `Grant` | Primary store |
| `GrantSeq` | — | `uint64` | ID allocator (1-indexed via `nextGrantID`) |
| `GrantsByGranter` | `Pair[string, uint64]` | empty | `(granter, id)` |
| `GrantsByGrantee` | `Pair[string, uint64]` | empty | `(grantee, id)` |
| `GrantsByExpiration` | `Pair[int64, uint64]` | empty | `(expires_at_unix, id)` — pruning iterator |
| `GrantsByTypeAndGranter` | `Triple[GrantType, string, uint64]` | empty | Per-type listings + caps |
| `ActiveGrantCountByType` | `Pair[string, GrantType]` | `uint32` | O(1) per-(granter, type) active counter |
| `SessionKeyByPair` | `Pair[string, string]` | `uint64` | Legacy `(granter, grantee) → grant_id` for the SessionKey one-per-pair invariant |
| `EpochSpendByGrant` | `Pair[uint64, int64]` | `string` (sdk.Int) | Per-grant UTC-day spend buckets for RecurringPull's `max_per_epoch_uspark` |
| `OneshotGasDeposit` | `uint64` (grant_id) | `Coin` | SPARK deposit escrow for ScheduledOneshot grants |

## 21. RecurringPull variant

A granter-authorized periodic SendCoins to a grantee. Modeled after `x/commons` RecurringSpend but anchored to a user account.

```protobuf
message RecurringPullPayload {
  cosmos.base.v1beta1.Coin amount_per_period = 1;
  int64 period_seconds = 2;
  int64 start_time = 3;
  int64 last_claim_advance = 4;     // Logical clock; advances by exactly period_seconds per claim
  uint64 claims_made = 5;
  string max_per_epoch_uspark = 6;  // sdk.Int as string; UTC-day ceiling
}
```

**Action:** `MsgClaimRecurringPull { grantee, grant_id }` — signed by the grantee on file.

**Flow:**
1. First claim eligible at `start_time + period_seconds` (council vote authorizes payment AFTER one period of work, not immediately).
2. Logical clock `last_claim_advance` advances by exactly `period_seconds` per claim — catch-up requires multiple txs (each rate-limited independently).
3. Per-grant `max_per_epoch_uspark` self-throttle: each claim adds `amount_per_period` to the current UTC-day bucket; rejected if it would breach `max_per_epoch_uspark`. **Epoch = UTC calendar day**, `floor(block_time / 86400)` — no dependency on any other module's epoch concept. Stale day-buckets lazily pruned.
4. `bankKeeper.SendCoins(granter → grantee)` — failure flips to `PAUSED_INSUFFICIENT_FUNDS` with a `grant_paused_underfunded` event; next successful retry flips back to `ACTIVE` atomically (no separate resume message).
5. COMPLETED when `last_claim_advance + period_seconds > expires_at`.

**Caps:** `min_recurring_period_seconds` (default 86_400 / 1d), `max_recurring_duration_seconds` (default 31_536_000 / 1y), `max_recurring_pulls_per_granter` (default 50).

**Denom:** must be in `params.allowed_denoms`. DREAM permanently rejected at the handler level regardless of params (defense-in-depth).

## 22. SpendingAllowance variant

A refilling per-period cap; the grantee picks the recipient and amount of each pull within the cap and (optional) whitelist.

```protobuf
message SpendingAllowancePayload {
  cosmos.base.v1beta1.Coin max_per_period = 1;
  int64 period_seconds = 2;
  int64 current_period_start = 3;
  cosmos.base.v1beta1.Coin spent_in_current_period = 4;
  repeated string allowed_recipients = 5;   // Empty = unrestricted whitelist
  string denom = 6;                          // Locks denom for the grant
}
```

**Action:** `MsgPullAllowance { grantee, grant_id, recipient, amount }` — signed by the grantee on file.

**Order of operations:**
1. **Authorization checks**: `recipient != granter` (anti self-roundtrip), `amount.denom == grant.denom` and in `params.allowed_denoms`, `amount >= params.min_pull_amount`, `recipient ∈ allowed_recipients` (skip if empty).
2. **In-memory rolling-window reset** if `block_time >= current_period_start + period_seconds`. **Only committed on successful pull** — a failed pull leaves `current_period_start` untouched, so a malicious recipient cannot trigger a no-op reset to wipe the granter's used budget (Rev 2 fix).
3. **Per-period budget check**: `spent_in_current_period + amount <= max_per_period`.
4. `bankKeeper.SendCoins(granter → recipient)` — failure flips to `PAUSED_INSUFFICIENT_FUNDS`; next successful retry flips back to `ACTIVE`.
5. `spent_in_current_period += amount`; emit `allowance_pulled`.

**Caps:** `min_allowance_period_seconds` (default 3_600 / 1h), `max_allowances_per_granter` (default 20), `max_allowance_recipient_list` (default 50), `min_pull_amount` (default "1000" uspark / 0.001 SPARK).

## 23. ScheduledOneshot variant

Fires once at `fire_at`, EndBlocker-driven.

```protobuf
message ScheduledOneshotPayload {
  oneof action {
    OneshotTransfer transfer = 1;            // Simple bank SendCoins(granter, recipient, amount)
    OneshotExec     exec     = 2;            // Dispatches an Any-encoded msg as if signed by granter
  }
  int64 fire_at = 10;
  string fire_error = 11;                    // Captured failure reason (Exec only)
}

message OneshotTransfer {
  string recipient = 1;
  cosmos.base.v1beta1.Coin amount = 2;
}

message OneshotExec {
  google.protobuf.Any msg = 1;
  uint64 gas_limit = 2;                       // Per-fire gas cap
}
```

**Action:** no external message — fires automatically in the EndBlocker fire pass.

### 23.1. Creation-time invariants

Enforced in `validateScheduledOneshotPayload` ([x/session/keeper/grant_validation.go](../x/session/keeper/grant_validation.go)):

- `fire_at >= block_time + params.min_schedule_delay_seconds` (default 60s) — closes front-running edge cases.
- `fire_at <= block_time + params.max_schedule_horizon_seconds` (default 1y).
- `fire_at + params.fire_to_expiry_buffer_seconds <= expires_at` (default buffer 1h). Without this, an oneshot can be scheduled to fire at the same instant the grant expires; whichever EndBlocker pass wins is ambiguous. Forcing a buffer makes the fire-vs-expire race impossible.
- Per-granter cap: `max_pending_oneshots_per_granter + max_paused_oneshots_per_granter` (defaults 100 + 20).

**Transfer variant:** denom must be in `params.allowed_denoms`; `dream` always rejected.

**Exec variant:**
- `msg.type_url ∈ params.allowed_msg_types`.
- `msg.type_url ∉ NonDelegableSessionMsgs` (anti-recursion denylist).
- `msg.type_url != "/sparkdream.session.v1.MsgRevokeSession"` (Rev 2 §7.3 defense-in-depth).
- `gas_limit ∈ [params.min_oneshot_exec_gas, params.max_oneshot_exec_gas]` (defaults 30_000 / 200_000).
- `late.router != nil` — fail loud at admin time, not at fire time.

### 23.2. Gas-deposit escrow

Both variants post a deposit to the session module account at `MsgCreateGrant` time. Formula (Rev 4):

```
OneshotExec:     deposit = max(ceil(gas_limit * oneshot_gas_price_uspark) + oneshot_creation_fee_uspark, min_oneshot_deposit_uspark)
OneshotTransfer: deposit = max(oneshot_creation_fee_uspark, min_oneshot_deposit_uspark)
```

**Rounding is strictly ceiling at every layer** — flooring at any layer would create a sub-uspark slot exploitable at `gas_limit = 1`.

Defaults: `oneshot_gas_price_uspark = "0.0025"` (100× typical `min_gas_price`), `oneshot_creation_fee_uspark = 1000`, `min_oneshot_deposit_uspark = 1000`. Worst-case per-granter pre-funding across 100 OneshotExec slots at the default cap: `100 × (ceil(200_000 × 0.0025) + 1000) = 150_000 uspark = 0.15 SPARK`.

**Refund matrix:**
- `MsgRevokeGrant` on ACTIVE: deposit refunded to granter.
- `MsgDeclineGrant` (grantee veto): deposit refunded to granter.
- EndBlocker auto-revoke after `paused_oneshot_ttl_seconds` (default 7d): deposit refunded.
- EndBlocker expire-without-fire: deposit refunded.
- FIRED (success or error): deposit moved to fee collector (`auth/fee_collector`) — pays for the gas attempted, regardless of inner success.

### 23.3. Fire-time containment

The fire path runs in a child `sdk.Context` via `CacheContext()` with a fresh `sdk.GasMeter` capped at `gas_limit`, wrapped in an **unconditional `defer recover()`** (Rev 3 §H3). Containment guarantee:

> A buggy or malicious downstream handler on the OneshotExec allowlist can, in the worst case: (a) consume up to `gas_limit` gas (the granter's deposit pays for this), (b) trigger the `defer recover()` and produce a FIRED-with-error grant, (c) waste one of the EndBlocker's `max_endblocker_dispatches_per_pass` slots. It **cannot** halt the chain, corrupt parent-context state, or escape to other grants in the same pass.

**The OneshotExec security contract is the audited `params.allowed_msg_types` allowlist, not a runtime flag.** `ContextKeySessionFireInProgress` exists for telemetry/event/re-entrance-detection only — downstream handlers MUST NOT branch on it to skip ante-equivalent logic.

A handler is **OneshotExec-safe** iff it satisfies all of:
- No-ante-dependent (no assumption that sigverify, mempool fee check, gas-price check, or `ctx.TxBytes()` ran first).
- Idempotent or single-effect (a panic between SendCoins and state write does not leave the chain half-committed).
- Typical gas fits well below `params.max_oneshot_exec_gas`.
- No external network dep (no IBC ack, oracle, external timer).
- No DREAM mutation that isn't in [`DreamFieldsToStrip`](../x/session/types/keys.go#L60).

### 23.4. Pause / retry

If the Transfer variant's `bankKeeper.SendCoins` fails at fire time, the grant pauses (status → `PAUSED_INSUFFICIENT_FUNDS`). `MsgRetryScheduledOneshot { caller, grant_id }` (caller = granter OR grantee) sets `fire_at = block_time` and flips back to ACTIVE.

Retry errors are distinct sentinels (Rev 3 §M1):

| Sentinel | Trigger |
|---|---|
| `ErrGrantTypeMismatch` | Caller targeted a non-SCHEDULED_ONESHOT grant |
| `ErrGrantNotPaused` | Grant is `ACTIVE` |
| `ErrGrantTerminal` | Grant is `REVOKED` / `DECLINED` / `COMPLETED` / `FIRED` |
| `ErrUnauthorizedRetry` | Caller is neither granter nor grantee |

The EndBlocker auto-revoke pass drops paused oneshots older than `paused_oneshot_ttl_seconds` (default 7d) and refunds the deposit.

## 24. Anti-recursion + `allow_self_revoke`

### 24.1. Hard denylist (cannot be overridden)

`NonDelegableSessionMsgs` ([x/session/types/keys.go](../x/session/types/keys.go)) — every x/session signer Msg defaults to denylisted:

```go
var NonDelegableSessionMsgs = map[string]bool{
    "/sparkdream.session.v1.MsgCreateSession":           true,
    "/sparkdream.session.v1.MsgRevokeSession":           true,
    "/sparkdream.session.v1.MsgExecSession":             true,
    "/sparkdream.session.v1.MsgCreateGrant":             true,
    "/sparkdream.session.v1.MsgDeclineGrant":            true,
    "/sparkdream.session.v1.MsgClaimRecurringPull":      true,
    "/sparkdream.session.v1.MsgPullAllowance":           true,
    "/sparkdream.session.v1.MsgRetryScheduledOneshot":   true,
    "/sparkdream.session.v1.MsgUpdateParams":            true,
    "/sparkdream.session.v1.MsgUpdateOperationalParams": true,
}
```

No payload-level allowlist or session-key allowlist can re-enable any msg type here.

### 24.2. `MsgRevokeGrant` is intentionally NOT on the hard denylist

Replaced with an opt-in per-grant flag (Rev 2 §7.2):

```protobuf
message SessionKeyPayload {
  bool allow_self_revoke = 7;  // If true, this session key MAY revoke same-granter grants
}
```

- Default false: a session key cannot revoke anything.
- If true at creation: the session-key holder can call `MsgRevokeGrant` against grants where `granter == this_session_key.granter`. The msg-server further pins the target via the `SessionKeyByPair(target.granter, caller)` lookup — cross-granter revocation is impossible.
- Including `MsgRevokeGrant` in `allowed_msg_types` without the flag → `ErrSelfRevokeNotPermitted` at creation.

A compromised session key with `allow_self_revoke=true` can revoke other grants of the same granter (e.g., stop a recurring pull). This is the trade-off; it's strictly less dangerous than allowing the session key to make transfers and is auditable on-chain.

## 25. Events (stability declaration)

Every state-changing operation emits a typed event. Event types and attribute keys are **append-only** once published: never renamed, never repurposed, never have their value encoding changed. New attributes may be added to existing events; indexers should tolerate unknown keys.

### 25.1. Attribute encoding contract

| Attribute proto type | Event attribute encoding |
|---|---|
| `google.protobuf.Timestamp` (`expires_at`, `created_at`) | RFC3339 with nanosecond precision (`time.Time.Format(time.RFC3339Nano)`) |
| `int64` unix time (`fire_at`, `new_fire_at`) | RFC3339Nano (converted via `time.Unix(v, 0).UTC()` before formatting) |
| `uint64` / `int64` non-time integers (`id`, `grant_id`, `gas_used`, `claim_index`) | Decimal string, no leading zeros, no `+` prefix |
| `cosmos.base.v1beta1.Coin` (`amount`, `refund_amount`, `attempted_amount`) | `Coin.String()` e.g. `1000uspark` |
| `GrantType` enum (`type`) | Lowercase enum suffix: `session_key`, `recurring_pull`, `spending_allowance`, `scheduled_oneshot`. `unspecified` is **never emitted** (creation rejects `GRANT_TYPE_UNSPECIFIED`); emitting one is a chain bug |
| Free-form enum-like strings (`result`, `variant`) | Lowercase ASCII tokens: `result ∈ {success, error}`, `variant ∈ {transfer, exec}`. Never `Success`/`SUCCESS`/`OK` |
| `string` (`granter`, `grantee`, `recipient`, `fire_error`) | The string itself, untransformed. Empty permitted; absent attribute is not (always emit) |
| `bool` (`fee_paid_by_granter`) | `"true"` / `"false"` |

### 25.2. Event types

| Event | Trigger | Attributes |
|---|---|---|
| `grant_created` | `MsgCreateGrant` success | `id`, `type`, `granter`, `grantee`, `expires_at` |
| `grant_revoked` | `MsgRevokeGrant` success | `id`, `type`, `granter`, `grantee`, `refund_amount` |
| `grant_declined` | `MsgDeclineGrant` success | `id`, `type`, `granter`, `grantee`, `refund_amount` |
| `grant_expired` | EndBlocker expire pass (non-SessionKey) | `id`, `type`, `granter`, `grantee` |
| `session_expired` | EndBlocker expire pass (SessionKey, legacy) | `granter`, `grantee`, `exec_count`, `spent`, `grant_id` |
| `grant_paused_underfunded` | RecurringPull / SpendingAllowance bank-send failure | `id`, `type`, `granter`, `grantee`, `attempted_amount` |
| `grant_resumed` | PAUSED→ACTIVE on successful retry/claim | `id`, `type`, `granter`, `grantee` |
| `grant_auto_revoked` | Paused-TTL auto-revoke pass | `id`, `granter`, `grantee`, `refund_amount` |
| `session_created` | `MsgCreateSession` (legacy) | `granter`, `grantee`, `expiration`, `grant_id` |
| `session_revoked` | `MsgRevokeSession` (legacy) | `granter`, `grantee`, `exec_count`, `spent`, `grant_id` |
| `session_executed` | `MsgExecSession` | `granter`, `grantee`, `msg_type_urls`, `exec_count`, `grant_id` |
| `recurring_pull_claimed` | `MsgClaimRecurringPull` success | `grant_id`, `granter`, `grantee`, `amount`, `claim_index` |
| `allowance_pulled` | `MsgPullAllowance` success | `grant_id`, `granter`, `grantee`, `recipient`, `amount`, `spent_in_period`, `max_per_period` |
| `oneshot_fired` | EndBlocker fire pass | `grant_id`, `granter`, `grantee`, `variant`, `result`, `fire_error` |
| `oneshot_retry_requested` | `MsgRetryScheduledOneshot` success | `grant_id`, `caller` |

## 26. CLI reference (post-refactor)

### 26.1. New universal commands

```
sparkdreamd tx session revoke-grant [grant-id] --from [granter]
  # Or, with allow_self_revoke=true, signed by the session-key grantee
  # against another grant of the same granter.

sparkdreamd tx session decline-grant [grant-id] --from [grantee]
  # One-way; refunds any held oneshot deposit to the granter.

sparkdreamd tx session retry-oneshot [grant-id] --from [caller]
  # Caller must be granter OR grantee.
```

### 26.2. Variant-specific actions

```
sparkdreamd tx session claim-recurring-pull [grant-id] --from [grantee]
sparkdreamd tx session pull-allowance [grant-id] [recipient] [amount] --from [grantee]
```

### 26.3. Legacy commands (deprecated; still functional)

```
sparkdreamd tx session create-session [grantee] [msg-types] [spend-limit] [expiration] [max-exec-count] --from [granter]
sparkdreamd tx session revoke-session [grantee] --from [granter]
sparkdreamd tx session exec-session [...]
```

These continue to work — `MsgCreateSession` / `MsgRevokeSession` internally write/read a SESSION_KEY-type `Grant`. The plan (Rev 3 §C2) calls for outright deletion at P6 to prevent the legacy alias from bypassing `allow_self_revoke`; deferred in implementation because `MsgRevokeSession` looks up via `SessionKeyByPair(granter, grantee)` which by construction only matches grants of the calling granter, so the alias-as-backdoor argument doesn't bite. Future deletion is non-breaking once test fixtures migrate.

### 26.4. Queries

```
sparkdreamd query session params
sparkdreamd query session allowed-msg-types
sparkdreamd query session grant [id]
sparkdreamd query session grants-by-granter [granter]    # any type
sparkdreamd query session grants-by-grantee [grantee]    # any type
sparkdreamd query session session [granter] [grantee]     # legacy: SessionKey only
sparkdreamd query session sessions-by-granter [granter]   # legacy: SessionKey only
sparkdreamd query session sessions-by-grantee [grantee]   # legacy: SessionKey only
```

The new `Grant` queries return any payload variant; the legacy `Session` queries project SESSION_KEY grants back to the legacy `Session` shape for indexer compatibility.

## 27. Cross-references

- [docs/x-session-grant-registry-plan.md](x-session-grant-registry-plan.md) — Refactor plan (Rev 4, P1-P8). Authoritative source for the design decisions referenced above.
- [docs/session-keys.md](session-keys.md) — Original session-key UX pattern (predates the registry refactor; still relevant for the SessionKey variant).
- [x/session/keeper/](../x/session/keeper/) — Reference implementation.

## 28. Module-bypass keeper entrypoints (P8 foundation)

The module exposes a small surface of keeper-to-keeper entrypoints that skip signature + tx-sequence verification, gated by an explicit governance allowlist. The shipped consumer is the x/commons `Msg*RecurringSpend` wrappers (M5–M8 of the migration; see [§29.6](#296-xcommons-migration-target--done)) — council policy addresses are module accounts that cannot sign user-style transactions, so they host their recurring obligations in the unified registry via these entrypoints. The foundation is general-purpose: any future module that hosts authorizations whose granter is a module account can be added to the allowlist by gov proposal.

The full surface consists of two creation/revocation entrypoints (§28.2–28.3) plus three additional helpers landed for the wrappers in M2: `DeclineGrantInternal`, `ClaimRecurringPullForGrantee`, and the read-side `GetGrant` / `ListGrantsByGranter` / `ListGrantsByGrantee` (lifted from internal collection walks).

### 28.1. Authorization gate

```protobuf
message Params {
  // ...
  // Bech32 addresses of module accounts authorized to call the bypass.
  // Default empty. Add only after a security review — each entry is a
  // strict trust grant that lets the named caller synthesize arbitrary
  // grants.
  repeated string authorized_grant_creators = 60;
}
```

- The list is **gov-only**: it's not present on `SessionOperationalParams`, so the Operations Committee cannot edit it. Only a `MsgUpdateParams` proposal (or chain upgrade) can change the allowlist.
- The list is validated at param-update time: each entry must be a syntactically valid bech32, duplicates rejected.
- An empty list **disables the bypass entirely**: every bypass entrypoint (`CreateGrantOnBehalfOf`, `RevokeGrantInternal`, `DeclineGrantInternal`, `ClaimRecurringPullForGrantee`) returns `ErrBypassDisabled`. Callers cannot "stumble into" the bypass via a misconfiguration.
- **Genesis default** (M3 of the RecurringSpend migration): `DefaultAuthorizedGrantCreators` seeds the list with `authtypes.NewModuleAddress("commons").String()` so the x/commons wrappers work from block 0. The deterministic seed avoids a post-launch gov race against in-flight schedules. See [x/session/types/params.go](../x/session/types/params.go).

### 28.2. `CreateGrantOnBehalfOf`

```go
func (k Keeper) CreateGrantOnBehalfOf(
    ctx context.Context,
    callerModuleAddr string,
    msg *types.MsgCreateGrant,
) (uint64, error)
```

Construct the `*types.MsgCreateGrant` exactly as if it were going to be sent as a user tx — same payload oneof wrappers (`&MsgCreateGrant_RecurringPull{...}` etc.). The bypass runs the same shared validation (`validateGrantCommon` + per-payload validator) and emits the standard `grant_created` event with an extra `source=module_bypass` and `caller_module=<addr>` attribute for auditing.

What's skipped: signature verification, account sequence, and the user-side fee deduction. What's NOT skipped: every payload-level invariant (denom allow-list, dream-denom hard reject, scheduling buffer, gas-deposit escrow, per-granter cap). A buggy or malicious allowlisted caller cannot, for example, mint a DREAM-denominated RecurringPull or schedule a oneshot with `fire_at < min_schedule_delay`.

### 28.3. `RevokeGrantInternal`

```go
func (k Keeper) RevokeGrantInternal(
    ctx context.Context,
    callerModuleAddr string,
    grantID uint64,
) (sdk.Coin, error)
```

Counterpart to `CreateGrantOnBehalfOf` for lifecycle closure: a module that creates a grant on behalf of granter X must also be able to revoke for X, otherwise its EndBlocker can't tear down state when the underlying authorization ends (e.g. a council's term expires). Same allowlist gate.

Refunds any held `OneshotGasDeposit` to the grant's granter (returning the deposit to the granter's account — which is typically the calling module's own account, leaving deposit handling at the caller's discretion).

Returns the refund amount so the caller can attribute it for accounting purposes. Emits `grant_revoked` with `source=module_bypass` and `caller_module=<addr>`.

### 28.4. Trust model

Each address listed in `authorized_grant_creators` is a **strict trust grant**. A compromised or buggy allowlisted module could:

- Synthesize grants from module accounts the calling module controls — bounded by what the named module's own auth posture allows.
- Synthesize grants from arbitrary user addresses by guessing the bech32 — but those grants have no funding behind them since the granter never signed, so they're either inert (zero balance fails at first claim) or instantly revertible via the user's own `MsgRevokeGrant`.

The defense is the gov-only allowlist + the security review at the time of adding an entry, not a technical containment. This matches Cosmos SDK convention for module-to-module trust (e.g. x/bank's module-account minting permissions list).

### 28.5. Privileged claim + decline helpers (M2)

```go
func (k Keeper) DeclineGrantInternal(
    ctx context.Context, callerModuleAddr string, grantID uint64, grantee string,
) (sdk.Coin, error)

func (k Keeper) ClaimRecurringPullForGrantee(
    ctx context.Context, callerModuleAddr string, grantID uint64, grantee string,
) (*types.MsgClaimRecurringPullResponse, error)
```

Added in M2 of the RecurringSpend migration to support the D3.a
wrappers in M7 and M8. Same `authorized_grant_creators` allowlist
gate as `CreateGrantOnBehalfOf` / `RevokeGrantInternal`. Both methods
also re-check `grant.Grantee == grantee` (defense in depth: the
calling wrapper has already verified the outer signer matches, but
the keeper method enforces it too so a wrapper bug can't decline /
claim against the wrong recipient's grant).

`ClaimRecurringPullForGrantee` delegates to the shared
`claimRecurringPullCommon` helper that the msg-server's
`ClaimRecurringPull` also calls — both paths run identical period
checks, hook PreCheck / PostCommit, bank send, status transitions,
and event emission.

### 28.6. Read-side helpers (M2)

```go
func (k Keeper) GetGrant(ctx context.Context, id uint64) (Grant, error)
func (k Keeper) ListGrantsByGranter(ctx context.Context, granter string, filterType GrantType) ([]Grant, error)
func (k Keeper) ListGrantsByGrantee(ctx context.Context, grantee string, filterType GrantType) ([]Grant, error)
```

Lifted from internal `Grants.Get` + index walks so cross-module
consumers (currently the x/commons wrappers and `CancelActiveSchedulesForRecipient`
service-hook helper) don't reach into collections directly. The
filter argument allows narrowing to a single `GrantType`;
`GRANT_TYPE_UNSPECIFIED` disables type filtering.

These read helpers are NOT gated by the bypass allowlist (they're
read-only) — they're part of the narrow `SessionKeeper` interface
that x/commons consumes (defined in
[x/commons/types/expected_keepers.go](../x/commons/types/expected_keepers.go)).

## 29. Grant-claim hooks (P8 foundation)

Downstream modules can register a `GrantClaimHook` to gate
on-the-wire transfers — claims, allowance pulls, and oneshot-transfer
fires — against module-specific preconditions. The hook is the
extensibility point that lets the x/commons RecurringSpend migration
apply group activation, term-expiry, and per-epoch rate limits to
claims whose granter is a council policy address, without x/session
having to know anything about councils.

### 29.1. Interface

The interface is two-method: `PreCheck` (pre-send veto, must not
mutate state) and `PostCommit` (post-send side effects, **errors
halt the surrounding tx**).

```go
package types

type GrantClaimHook interface {
    // PreCheck runs before bank send. A non-nil error vetoes the
    // operation. Idempotent — may run twice on pause/resume retries.
    PreCheck(ctx context.Context, grant Grant, amount sdk.Coins) error

    // PostCommit runs after a successful bankKeeper.SendCoins. State-
    // mutating side effects (e.g. per-epoch budget debit) go here so
    // they are atomic with the disbursement. A non-nil error HALTS
    // the tx — the SDK rolls back the bank send, the grant update,
    // and any state touched in PreCheck or PostCommit.
    PostCommit(ctx context.Context, grant Grant, amount sdk.Coins) error
}

// NoOpPostCommitHook embeddable helper for hooks that only need
// PreCheck — provides a no-op PostCommit so the halting-on-error
// contract is harmless.
type NoOpPostCommitHook struct{}
func (NoOpPostCommitHook) PostCommit(_ context.Context, _ Grant, _ sdk.Coins) error { return nil }

// GrantClaimMultiHook composes multiple hooks. Both PreCheck and
// PostCommit fan out in registration order and short-circuit on the
// first error in either pass.
type GrantClaimMultiHook []GrantClaimHook
```

### 29.2. Wiring

Hooks are wired via late-binding from `app.go` post-depinject, matching the pattern used for `SetRouter` / `SetCommonsKeeper`:

```go
// app.go (illustrative; lives in the x/commons migration PR):
app.SessionKeeper.SetClaimHooks(
    commonskeeper.NewSessionClaimHook(app.CommonsKeeper),
    // additional hooks from other modules can append here…
)
```

`SetClaimHooks` replaces the entire list (no partial updates). Pass an empty list to clear. Order is registration order; the first hook that returns non-nil short-circuits the rest of the pass.

### 29.3. Invocation points

Each invocation site runs PreCheck **before** `bankKeeper.SendCoins` and PostCommit **after** the state writes that must be atomic with the disbursement.

| Site | Source file | PreCheck position | PostCommit position |
|---|---|---|---|
| `MsgClaimRecurringPull` | [msg_server_claim_recurring_pull.go](../x/session/keeper/msg_server_claim_recurring_pull.go) | before `bankKeeper.SendCoins` | after `addEpochSpend` + `Grants.Set` (post-status-transition) |
| `MsgPullAllowance` | [msg_server_pull_allowance.go](../x/session/keeper/msg_server_pull_allowance.go) | before `bankKeeper.SendCoins` | after period clock + `spent_in_current_period` commit |
| `fireScheduledOneshot` (Transfer variant) | [oneshot.go](../x/session/keeper/oneshot.go) | inside the `CacheContext`, before bank send | inside the `CacheContext`, after bank send — failures discard the CacheContext, so the bank send is rolled back along with the rest |

The Exec variant of ScheduledOneshot is **intentionally NOT hooked.** OneshotExec's security contract is the audited `params.allowed_msg_types` allowlist; arbitrary inner-msg dispatch already runs the destination handler's own validation. Layering a session-level hook on top would double-charge for handlers that do their own gating.

### 29.4. Hook contract

Hook authors should ensure:

- **PreCheck is idempotent** — it may be invoked twice for the same `(grant, amount)` in pause/resume edge cases. Do not mutate state in PreCheck; defer side effects to PostCommit so they are atomic with the disbursement.
- **PostCommit failures HALT the tx** — by SDK contract, a non-nil error returned from a Msg handler discards the cache context. PostCommit should return errors only when the failure indicates that the disbursement should not have happened (e.g. an internal write failure that would have left a rate-limit budget desynced); see §29.5.
- **Bounded gas** — hooks run synchronously inside the claim tx and contribute to its gas cost.
- **No re-entrance into x/session** — hooks must not call `ClaimRecurringPull` / `PullAllowance` / `CreateGrantOnBehalfOf` from inside themselves; the registry makes no re-entrance guarantee.
- **Wrap errors with `errorsmod.Wrap`** so CLI / indexer surfaces can attribute the failure to the right module.

### 29.5. PostCommit halting rationale

A naive single-method hook would force the downstream module to either (a) skip the atomic side-effect write (rate limits leak), or (b) write the side effect before the bank send and risk debiting against a failed disbursement (double-debit on retry, see x/commons migration plan §3.2).

The two-method split lets PreCheck do the "would this be allowed?" check and PostCommit do the "record that it happened" write. If the PostCommit write itself fails — collections backing error, OOM, concurrent map mutation — the SDK rollback discards the bank send and the disbursement is **not** recorded. The retry runs PreCheck and the bank send fresh.

PreCheck-only failures behave as ordinary validation errors: no state mutation has happened, the tx rolls back cleanly without rollback being a concern.

### 29.6. x/commons migration target — **DONE**

The x/commons RecurringSpend migration is shipped. Summary of what
landed:

1. `authtypes.NewModuleAddress("commons").String()` is seeded into
   `params.authorized_grant_creators` at genesis
   ([x/session/types/params.go](../x/session/types/params.go) —
   `DefaultAuthorizedGrantCreators`).
2. `MsgScheduleRecurringSpend.handler` constructs a session
   `MsgCreateGrant` carrying a `RecurringPullPayload` and calls
   `sessionKeeper.CreateGrantOnBehalfOf`. No commons-side storage
   write.
3. `commonskeeper.SessionClaimHook` wraps `checkSpendGates` (PreCheck)
   + `recordEpochSpend` (PostCommit). Registered via
   `app.SessionKeeper.SetClaimHooks(...)` in
   [app/app.go](../app/app.go). Non-council grants pass through as
   no-ops.
4. `MsgCancelRecurringSpend.handler` calls `RevokeGrantInternal`;
   `MsgDeclineRecurringSpend.handler` calls `DeclineGrantInternal`;
   `MsgClaimRecurringSpend.handler` calls `ClaimRecurringPullForGrantee`.
   All four wire shapes preserved.
5. The pre-migration `RecurringSpends*` collections, the three
   duplicated commons params (`MinRecurringPeriodSeconds` /
   `MaxRecurringDurationSeconds` / `MaxActiveRecurringSpendsPerGroup`),
   and the simulation file are removed (-~1100 LoC net).

Two privileged keeper methods (`DeclineGrantInternal`,
`ClaimRecurringPullForGrantee`) were added in M2 to support the
wrappers — see [§28](#28-module-bypass-keeper-entrypoints-p8-foundation).
