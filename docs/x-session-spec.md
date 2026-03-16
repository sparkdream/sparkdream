# x/session Module Specification

## 1. Abstract

The `x/session` module provides **session key management with integrated fee delegation**, replacing the need for `x/authz` and `x/feegrant`. It enables fluid on-chain interactions by allowing users to delegate scoped, time-limited transaction authority to ephemeral browser-generated keys — eliminating wallet popups for routine actions like posting, replying, and reacting.

**Design philosophy:** Build exactly what session keys need, nothing more. Unlike `x/authz` (which provides general-purpose authorization with recursive execution, typed authorizations, and complex grant hierarchies), x/session is purpose-built for the session key pattern described in `docs/session-keys.md`.

**Why not x/authz + x/feegrant?**
- **Licensing risk**: Both are extracted Go modules (`cosmossdk.io/x/authz`, `cosmossdk.io/x/feegrant`) that can be independently relicensed, as happened with `x/group`.
- **Overengineered**: Session keys use ~10% of authz's surface area. No need for `GenericAuthorization`, `TypedAuthorization`, `SendAuthorization`, `StakeAuthorization`, recursive `MsgExec`, or the full grant interface hierarchy.
- **Separate fee module**: x/feegrant is a separate module with its own state, params, and pruning — unnecessary complexity when fee delegation is a single field on the session.
- **Security surface**: x/authz's recursive `MsgExec` is explicitly blocked in `x/commons` `ForbiddenMessages` because it bypasses council permission filters. x/session's `MsgExecSession` is non-recursive by design and uses an allowlist-only model with no blocklist to maintain.

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

### 8.3. Interaction with x/commons ForbiddenMessages

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
| `/sparkdream.forum.v1.MsgMarkAcceptedReply` | Thread author marks solution |
| `/sparkdream.forum.v1.MsgConfirmProposedReply` | Confirming sentinel proposals |
| `/sparkdream.forum.v1.MsgRejectProposedReply` | Rejecting sentinel proposals |

**Excluded** (financial, irreversible, or abuse-prone — require main wallet):
- `MsgDeletePost` — permanent deletion
- `MsgBondSentinel` / `MsgUnbondSentinel` — locks/unlocks DREAM
- `MsgCreateBounty` / `MsgAwardBounty` — escrows DREAM
- `MsgHidePost` — sentinel moderation (requires bond)
- `MsgAppealPost` — initiates dispute resolution
- `MsgFlagPost` — a compromised session key could mass-flag content to grief creators; flagging is deliberate enough to warrant main wallet

### 12.3. x/name (Identity — Limited)

| Message Type | Rationale |
|-------------|-----------|
| `/sparkdream.name.v1.MsgSetPrimary` | Changing primary display name |
| `/sparkdream.name.v1.MsgUpdateName` | Updating name metadata |

**Excluded** (governance-gated or financial):
- `MsgRegisterName` — requires council membership, pays fee
- `MsgFileDispute` / `MsgContestDispute` — locks DREAM

### 12.4. x/collect (Collections — Limited)

| Message Type | Rationale |
|-------------|-----------|
| `/sparkdream.collect.v1.MsgReact` | Reacting to collections |
| `/sparkdream.collect.v1.MsgRemoveReaction` | Removing reactions |

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
      "/sparkdream.collect.v1.MsgReact",
      "/sparkdream.collect.v1.MsgRemoveReaction"
    ],
    "allowed_msg_types": [
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
      "/sparkdream.collect.v1.MsgReact",
      "/sparkdream.collect.v1.MsgRemoveReaction"
    ],
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
