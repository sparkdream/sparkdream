# x/session

Purpose-built session key module replacing `x/authz` + `x/feegrant`. Provides ephemeral session keys with scoped message-type delegation, time expiration, execution caps, and integrated fee delegation.

## Overview

Session keys allow a main wallet (granter) to delegate limited transaction authority to an ephemeral key (grantee). This enables UX patterns like browser-based dApps executing transactions without requiring wallet confirmation for every action.

Key design properties:
- **Non-recursive**: `MsgExecSession` cannot nest — avoids the recursion attacks that plague `x/authz`
- **Bounded allowlist**: A genesis ceiling caps delegable message types; only expandable via chain upgrade
- **Integrated fee delegation**: Granter pays gas via `spend_limit` on the session — no separate feegrant module
- **Leaf module**: Depends only on `x/bank`, `x/auth`, and the message router — no cycle risk

## State

### Storage (Collections-based)

| Collection | Prefix | Key | Description |
|---|---|---|---|
| Params | `p_session` | Item | Module parameters |
| Sessions | `0` | `(granter, grantee)` | Primary session store |
| SessionsByGranter | `1` | `(granter, grantee)` | Index for granter queries |
| SessionsByGrantee | `2` | `(grantee, granter)` | Index for grantee queries and ante handler |
| SessionsByExpiration | `3` | `(expiration_unix, granter, grantee)` | Index for EndBlocker pruning |

### Session

```protobuf
message Session {
  string granter = 1;
  string grantee = 2;
  repeated string allowed_msg_types = 3;
  cosmos.base.v1beta1.Coin spend_limit = 4;
  cosmos.base.v1beta1.Coin spent = 5;
  google.protobuf.Timestamp expiration = 6;
  google.protobuf.Timestamp created_at = 7;
  google.protobuf.Timestamp last_used_at = 8;
  uint64 exec_count = 9;
  uint64 max_exec_count = 10;
}
```

## Messages

### MsgCreateSession

Creates a new session granting scoped authority from granter to grantee.

**Signer**: granter

| Field | Type | Description |
|---|---|---|
| `granter` | string | Main wallet address |
| `grantee` | string | Ephemeral session key address |
| `allowed_msg_types` | []string | Message types the grantee can execute |
| `spend_limit` | Coin | Max gas budget in uspark (zero = no fee delegation) |
| `expiration` | Timestamp | When the session auto-invalidates |
| `max_exec_count` | uint64 | Execution cap (0 = unlimited) |

**Validations**:
- Granter cannot equal grantee (no self-delegation)
- No existing session for the (granter, grantee) pair
- Granter has not exceeded `max_sessions_per_granter`
- `len(allowed_msg_types) <= max_msg_types_per_session`
- Every message type must be in the current active `allowed_msg_types`
- No non-delegable session messages in the allowlist
- Expiration must be in the future and within `max_expiration` from now
- Spend limit must not exceed `max_spend_limit`
- Spend limit denom must be `uspark` (if positive)

### MsgExecSession

Executes one or more inner messages on behalf of the granter.

**Signer**: grantee

| Field | Type | Description |
|---|---|---|
| `grantee` | string | Session key address |
| `granter` | string | Main wallet address to act on behalf of |
| `msgs` | []Any | Inner messages to execute (1-10) |

**Validations**:
- Session must exist and not be expired
- Execution count must not exceed `max_exec_count` (if max > 0)
- 1 to 10 inner messages allowed
- No nested `MsgExecSession` (anti-recursion)
- Every inner message type must be in both the session's and global active allowlist
- Each inner message must have exactly one signer
- Fee budget: `session.spent + tx_fee <= session.spend_limit`

**Processing**:
- Signer fields on inner messages are rewritten to the granter (supports `Creator`, `Authority`, `Granter`, `Sender` fields)
- DREAM-related fields are stripped from inner messages (e.g., `author_bond` on blog posts) to prevent unintended DREAM commits
- Messages are dispatched atomically via the message router
- Session `exec_count`, `last_used_at`, and `spent` are updated

### MsgRevokeSession

Revokes an existing session, deleting it and all indexes.

**Signer**: granter

| Field | Type | Description |
|---|---|---|
| `granter` | string | Main wallet address |
| `grantee` | string | Session key to revoke |

### MsgUpdateParams

Updates module parameters via governance. Can shrink the ceiling but not expand it.

**Signer**: governance authority

### MsgUpdateOperationalParams

Updates the active allowlist within the ceiling bounds. Intended for the Operations Committee (currently governance-only until commons integration is complete).

**Signer**: governance authority

**Constraints**: `allowed_msg_types` must be a subset of the current ceiling.

## Queries

| Endpoint | Description |
|---|---|
| `Params` | Module parameters (ceiling + active allowlist) |
| `Session` | Single session by (granter, grantee) |
| `SessionsByGranter` | All sessions for a granter (paginated) |
| `SessionsByGrantee` | All sessions for a grantee (paginated) |
| `AllowedMsgTypes` | Ceiling and active message type allowlists |

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `max_sessions_per_granter` | 10 | Max sessions a granter can have |
| `max_msg_types_per_session` | 20 | Max message types per session |
| `max_expiration` | 7 days | Maximum session duration |
| `max_spend_limit` | 100 SPARK | Maximum fee delegation budget |
| `max_allowed_msg_types` | 19 types | Ceiling — only shrinkable via governance, expandable only via chain upgrade |
| `allowed_msg_types` | 19 types | Active allowlist — must be subset of ceiling |

### Default Allowed Message Types

The genesis ceiling includes message types from:
- **x/blog**: `MsgCreatePost`, `MsgUpdatePost`, `MsgCreateReply`, `MsgEditReply`, `MsgReact`, `MsgRemoveReaction`
- **x/forum**: `MsgCreatePost`, `MsgEditPost`, `MsgUpvotePost`, `MsgDownvotePost`, `MsgFollowThread`, `MsgUnfollowThread`, `MsgMarkAcceptedReply`, `MsgConfirmProposedReply`, `MsgRejectProposedReply`
- **x/name**: `MsgSetPrimary`, `MsgUpdateName`
- **x/collect**: `MsgReact`, `MsgRemoveReaction`

### Non-Delegable Messages

These message types are hardcoded as never-delegable (anti-recursion):
- `/sparkdream.session.v1.MsgCreateSession`
- `/sparkdream.session.v1.MsgRevokeSession`
- `/sparkdream.session.v1.MsgExecSession`
- `/sparkdream.session.v1.MsgUpdateParams`
- `/sparkdream.session.v1.MsgUpdateOperationalParams`

## Ante Handler

`SessionFeeDecorator` intercepts transactions containing `MsgExecSession`:

1. Rejects mixed transactions (only `MsgExecSession` messages allowed in the tx)
2. Validates all `MsgExecSession` messages reference the same granter
3. Verifies each session exists and is not expired
4. Checks spend budget is sufficient
5. If any session has a positive `spend_limit`, transfers fees from granter to `fee_collector` and sets a context flag so `SkipIfFeePaidDecorator` skips standard fee deduction

## EndBlocker

Prunes expired sessions each block:
- Range-scans `SessionsByExpiration` for entries where `expiration_unix <= block_time`
- Deletes the session and all 3 indexes
- Rate-limited to 100 sessions per block to prevent block stalls
- Emits `session_expired` event for each pruned session

## Events

| Event | Attributes |
|---|---|
| `session_created` | `granter`, `grantee`, `expiration` |
| `session_executed` | `granter`, `grantee`, `msg_type_urls`, `exec_count` |
| `session_revoked` | `granter`, `grantee`, `exec_count`, `spent` |
| `session_expired` | `granter`, `grantee`, `exec_count`, `spent` |

## CLI

### Queries

```bash
sparkdreamd query session params
sparkdreamd query session session [granter] [grantee]
sparkdreamd query session sessions-by-granter [granter]
sparkdreamd query session sessions-by-grantee [grantee]
sparkdreamd query session allowed-msg-types
```

### Transactions

```bash
sparkdreamd tx session create-session [grantee] [allowed-msg-types] [spend-limit] [expiration] [max-exec-count]
sparkdreamd tx session revoke-session [grantee]
```

`MsgExecSession` requires custom Any-encoded message construction and is not exposed as a simple CLI command.

## Error Codes

| Code | Name | Description |
|---|---|---|
| 1100 | ErrInvalidSigner | Expected governance account as signer |
| 1101 | ErrSessionExists | Session already exists for (granter, grantee) |
| 1102 | ErrSessionNotFound | No active session found |
| 1103 | ErrSessionExpired | Session past expiration |
| 1104 | ErrMsgTypeNotAllowed | Message type not in session's allowlist |
| 1105 | ErrMsgTypeForbidden | Message type is non-delegable |
| 1106 | ErrMsgTypeNotInAllowlist | Message type not in global active allowlist |
| 1107 | ErrSpendLimitExceeded | Fee delegation budget exhausted |
| 1108 | ErrExecCountExceeded | Execution cap reached |
| 1109 | ErrMaxSessionsExceeded | Granter has too many sessions |
| 1110 | ErrMaxMsgTypesExceeded | Too many message types in session |
| 1111 | ErrExpirationTooLong | Expiration exceeds max_expiration |
| 1112 | ErrSpendLimitTooHigh | Spend limit exceeds max_spend_limit |
| 1113 | ErrSelfDelegation | Granter and grantee are the same address |
| 1114 | ErrNestedExec | MsgExecSession contains MsgExecSession |
| 1115 | ErrEmptyMsgs | No inner messages provided |
| 1116 | ErrTooManyMsgs | More than 10 inner messages |
| 1117 | ErrMixedTransaction | MsgExecSession mixed with other message types in tx |
| 1118 | ErrInvalidExpiration | Expiration is in the past |
| 1119 | ErrMultipleGranters | Different granters in batch |
| 1120 | ErrMultipleSigners | Inner message has multiple signers |
| 1121 | ErrInvalidDenom | Spend limit denom is not uspark |
| 1122 | ErrCeilingExpansion | Attempted to expand ceiling via governance |
| 1123 | ErrExceedsCeiling | Message type not in ceiling |

## Dependencies

| Module | Usage |
|---|---|
| `x/auth` | Account lookup for signer validation |
| `x/bank` | Fee transfer for delegation |
| Message Router | Inner message dispatch (late-wired via `SetRouter()`) |

No modules depend on x/session — it is a leaf module with zero cycle risk.
