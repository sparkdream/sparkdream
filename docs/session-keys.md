# Session Keys: Fluid On-Chain Interactions

## Problem

Every on-chain action (post, reply, react, upvote) requires a wallet-signed transaction. For content-heavy modules like `x/blog` and `x/forum`, constant wallet popups destroy the user experience and block adoption.

## Solution

Use an x/session **SessionKey** grant: a scoped, time-limited delegation with integrated fee delegation. x/session is the chain's clean-room replacement for `x/authz` + `x/feegrant` (neither is wired); spec: [x-session-spec.md](x-session-spec.md).

### How It Works

1. User connects wallet to the frontend
2. Frontend generates an **ephemeral session key** (keypair stored in browser memory)
3. User signs **one** `MsgCreateGrant` with a `session_key` payload (or the legacy `MsgCreateSession`) — the **only wallet popup** for the session. It names the message types the key may send (a subset of the chain's `allowed_msg_types`), a fee `spend_limit`, a `max_exec_count` and an expiration (at most `max_expiration`, 7 days by default; a key naming at most 2 message types without `allow_self_revoke` may run to `narrow_session_max_expiration`, 30 days by default)
4. For the rest of the session the frontend wraps each action in `MsgExecSession { granter, grantee, msgs }` and signs it with the session key — **zero wallet popups**. The chain rewrites each inner message's signer field to the granter
5. On logout the user revokes the grant (`MsgRevokeGrant`); on expiry it stops working by itself

### Flow Diagram

```
User Wallet                           Session Key (browser)
    |                                        |
    |-- MsgCreateGrant{session_key} -------->|  <-- only wallet approval
    |                                        |
    |                                        |-- MsgExecSession[MsgCreateReply] (auto-signed)
    |                                        |-- MsgExecSession[MsgReact]       (auto-signed)
    |                                        |-- MsgExecSession[MsgUpvotePost]  (auto-signed)
    |                                        |-- MsgExecSession[MsgUpdateReply] (auto-signed)
    |                                        |-- ... (no popups)
    |                                        |
    |                                        |  (grant expires or user revokes)
```

---

## Grant Scoping

The SessionKey grant is scoped to specific message types. This means the session key can only perform actions the user explicitly authorized — it cannot drain funds, delete accounts, or perform governance actions.

### Recommended Grants by Module

#### x/blog

**Safe for session key (high-frequency, low-risk):**

| Message | Rationale |
|---------|-----------|
| `MsgCreatePost` | Content creation |
| `MsgUpdatePost` | Editing own posts |
| `MsgCreateReply` | Replying to posts |
| `MsgUpdateReply` | Editing own replies |
| `MsgReact` | Adding/changing reactions |
| `MsgRemoveReaction` | Removing own reactions |
| `MsgHidePost` / `MsgUnhidePost` | Author-only self-curation, reversible, no bond or funds. In the allowlist so a moderation bot running on a session key can hide and unhide the author's own content |
| `MsgHideReply` / `MsgUnhideReply` | Same: post author only, reversible. Bots may hide |
| `MsgPinReply` / `MsgUnpinReply` | Display-only, reversible; the trust-level gate is re-checked against the granter at dispatch |

**Keep behind main wallet (destructive or irreversible):**

| Message | Rationale |
|---------|-----------|
| `MsgDeletePost` | Permanent tombstone (clears title/body; replies and reactions preserved) |
| `MsgDeleteReply` | Permanent tombstone (clears body; child replies preserved) |
| `MsgMakePostPermanent` / `MsgMakeReplyPermanent` | Irreversible promotion that burns the granter's per-day quota |

#### x/forum

**Safe for session key (high-frequency, low-risk):**

| Message | Rationale |
|---------|-----------|
| `MsgCreatePost` | Creating posts and replies |
| `MsgEditPost` | Editing own content |
| `MsgUpvotePost` | Reacting to content |
| `MsgDownvotePost` | Reacting to content |
| `MsgFollowThread` | Thread subscription |
| `MsgUnfollowThread` | Thread unsubscription |
| `MsgMarkAcceptedReply` | Author marks a solution; a bonded sentinel may instead *propose* one (sentinel bond re-checked at dispatch) |
| `MsgConfirmProposedReply` | Author confirms a sentinel's accepted-reply proposal |
| `MsgRejectProposedReply` | Author rejects a sentinel's accepted-reply proposal |
| `MsgHidePost` / `MsgUnhidePost` | Sentinel moderation — admitted as an exception; the msg server re-checks the granter's sentinel bond at dispatch, so the session key grants no power the granter doesn't already hold |

> **Sentinel-privilege exception.** `MsgMarkAcceptedReply` (sentinel-propose branch), `MsgHidePost`, and `MsgUnhidePost` are the only session-allowed forum messages that touch a bonded-role privilege. Each re-checks the granter's sentinel `BondedRole` status at dispatch, so a limited or stolen session key can never act as a sentinel unless the granter already is one. Curation rewards / moderation bonds accrue to the granter, not the session key.

**Keep behind main wallet (financial or irreversible):**

| Message | Rationale |
|---------|-----------|
| `MsgDeletePost` | Permanent deletion |
| `MsgBondRole` (x/rep) | Locks DREAM tokens against a bonded role (sentinel / curator / verifier) |
| `MsgUnbondRole` (x/rep) | Unlocks DREAM tokens from a bonded role |
| `MsgCreateBounty` | Escrows DREAM |
| `MsgAwardBounty` | Transfers escrowed DREAM |
| `MsgFlagPost` | A compromised session key could mass-flag to grief creators |
| `MsgAppealPost` | Initiates dispute resolution |

---

## Fee Delegation

The grant's `spend_limit` is a fee budget paid by the granter: the session key never holds funds. `SessionFeeDecorator` moves each `MsgExecSession` tx fee from the granter to the fee collector and debits the session's `spent`. Rules a client must follow:

- **The fee must meet the node's minimum gas prices**, exactly as for any other tx. A zero or token fee is refused in CheckTx.
- A tx may only contain `MsgExecSession` messages, all for the same granter, each with at least one inner message.
- The fee is in the spend-limit denom and must fit the remaining budget. An exhausted budget, or a batch that would exceed `max_exec_count`, is refused before anything is charged.
- A failing inner message still costs the fee (the ante has already charged it), as for any tx.

---

## Security Considerations

### Session Key Lifecycle

| Phase | Action |
|-------|--------|
| **Create** | Frontend generates keypair; private key stored in browser memory (not localStorage for maximum security) |
| **Activate** | User signs one MsgCreateGrant (session_key payload) |
| **Use** | Frontend auto-signs with session key; no wallet interaction |
| **Expire** | Grant has time-based expiration (e.g., 24h); session key becomes useless |
| **Revoke** | User can explicitly revoke via MsgRevokeGrant at any time |
| **Logout** | Frontend discards session key from memory |

### Threat Model

| Threat | Mitigation |
|--------|------------|
| Session key stolen (XSS) | Grant is time-limited and message-type-scoped; attacker can only post/reply as the user, not drain funds |
| Session key used after logout | Grant expiration ensures automatic invalidation |
| Malicious frontend abuses grant | User reviews grant scope before signing; destructive actions excluded |
| Fee drain via gas spam | spend_limit caps the fee budget; fees must meet the node's minimum gas prices; max_exec_count caps the number of executions |

### Best Practices

- **Expiration**: Default 24 hours; configurable by user
- **Memory storage**: Prefer in-memory over localStorage (cleared on tab close)
- **Scope minimally**: Only grant the message types the user actually needs for their current activity
- **Revoke on logout**: Call MsgRevokeGrant when the user explicitly logs out (don't rely solely on expiration)
- **Display active grants**: Show the user what their session key can do and when it expires

## Multisig Accounts as Granters

A native k-of-n multisig account ([multisig-accounts.md](multisig-accounts.md))
is the recommended cold root for a session key: the multisig signs one grant
(threshold-signed through the CLI multisign flow), and the hot key acts day to
day with no further signature collection. Two rules follow from the account
being a multisig:

- **Identity messages are never delegable.** `MsgRegisterZkPublicKey` and
  `MsgClearZkPublicKey` join the member-key-migration messages on rep's
  identity list (`reptypes.IdentityMsgTypeURLs()`), which x/session's
  `ForbiddenAllowlistMsgs` is built from: a hot key must not be able to
  re-point the member's anonymous identity around the account's threshold.
- **Sign-in goes through a `SignIn` grant.** sdaplogin's ADR-036 sign-in
  accepts a single secp256k1 key only, so a multisig member grants `SignIn` to a
  single-key grantee for the service and signs in with that key.

Multisig signers should pass `--gas 300000` on governance txs: a 3-of-5 tx
costs about 25k gas more than a single-key tx.

---

## Implementation Notes

x/session implements the chain side: grant storage, the allowlist (`allowed_msg_types` ⊆ the upgrade-only `max_allowed_msg_types`), signer rewriting (from each message's `cosmos.msg.v1.signer` proto option), DREAM-field stripping and fee delegation. Messages guarded by an ante-only rule (commons `MsgSubmitProposal`, gnovm VM messages) and identity / vault messages can never be delegated.

The frontend is responsible for:
1. Generating the session keypair
2. Constructing and broadcasting the grant transaction
3. Storing the session private key securely
4. Wrapping actions in `MsgExecSession` and auto-signing them with the session key, with a fee at the node's minimum gas price
5. Revoking on logout

CLI equivalents: `sparkdreamd tx session create-grant [grantee] [expires-at] [payload.json]` and `sparkdreamd tx session exec-session [granter] [msgs.json]`.
