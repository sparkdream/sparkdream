# Session Keys: Fluid On-Chain Interactions

## Problem

Every on-chain action (post, reply, react, upvote) requires a wallet-signed transaction. For content-heavy modules like `x/blog` and `x/forum`, constant wallet popups destroy the user experience and block adoption.

## Solution

Use `x/authz` session keys with optional `x/feegrant` fee delegation. Both are standard Cosmos SDK modules — no custom module code required.

### How It Works

1. User connects wallet to the frontend
2. Frontend generates an **ephemeral session key** (keypair stored in browser memory or localStorage)
3. User signs **one** `MsgGrant` (`x/authz`) — this is the **only wallet popup** for the session
4. The grant gives the session key permission to send specific message types on the user's behalf, with a time expiration (e.g., 24 hours)
5. For the rest of the session, the frontend auto-signs transactions using the session key — **zero wallet popups**
6. On logout or expiry, the grant is automatically invalid

### Flow Diagram

```
User Wallet                           Session Key (browser)
    |                                        |
    |-- MsgGrant (x/authz) ------------------>|  <-- only wallet approval
    |-- MsgGrantAllowance (x/feegrant) ------>|  <-- optional: subsidize gas
    |                                        |
    |                                        |-- MsgCreateReply (auto-signed)
    |                                        |-- MsgReact (auto-signed)
    |                                        |-- MsgUpvotePost (auto-signed)
    |                                        |-- MsgEditReply (auto-signed)
    |                                        |-- MsgCreatePost (auto-signed)
    |                                        |-- ... (no popups)
    |                                        |
    |                                        |  (grant expires or user logs out)
```

---

## Grant Scoping

The `x/authz` grant is scoped to specific message types. This means the session key can only perform actions the user explicitly authorized — it cannot drain funds, delete accounts, or perform governance actions.

### Recommended Grants by Module

#### x/blog

**Safe for session key (high-frequency, low-risk):**

| Message | Rationale |
|---------|-----------|
| `MsgCreatePost` | Content creation |
| `MsgUpdatePost` | Editing own posts |
| `MsgCreateReply` | Replying to posts |
| `MsgEditReply` | Editing own replies |
| `MsgReact` | Adding/changing reactions |
| `MsgRemoveReaction` | Removing own reactions |

**Keep behind main wallet (destructive or irreversible):**

| Message | Rationale |
|---------|-----------|
| `MsgDeletePost` | Permanent tombstone (clears title/body; replies and reactions preserved) |
| `MsgDeleteReply` | Permanent tombstone (clears body; child replies preserved) |
| `MsgHideReply` | Moderation action (reversible, but deliberate) |
| `MsgUnhideReply` | Moderation reversal |

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
| `MsgFlagPost` | Community moderation |
| `MsgMarkAcceptedReply` | Thread author marks solution |
| `MsgConfirmProposedReply` | Confirming sentinel proposals |
| `MsgRejectProposedReply` | Rejecting sentinel proposals |

**Keep behind main wallet (financial or irreversible):**

| Message | Rationale |
|---------|-----------|
| `MsgDeletePost` | Permanent deletion |
| `MsgBondRole` (x/rep) | Locks DREAM tokens against a bonded role (sentinel / curator / verifier) |
| `MsgUnbondRole` (x/rep) | Unlocks DREAM tokens from a bonded role |
| `MsgCreateBounty` | Escrows DREAM |
| `MsgAwardBounty` | Transfers escrowed DREAM |
| `MsgHidePost` | Sentinel moderation (requires bond) |
| `MsgAppealPost` | Initiates dispute resolution |

---

## Fee Delegation with x/feegrant

Without fee delegation, the session key address needs funds to pay gas. This creates friction (user must transfer tokens to a temporary address). `x/feegrant` solves this:

```
MsgGrantAllowance {
  granter: "user_main_wallet",
  grantee: "session_key_address",
  allowance: BasicAllowance {
    spend_limit: [{ denom: "uspark", amount: "1000000" }],  // 1 SPARK budget
    expiration: "2026-02-21T00:00:00Z"                       // 24h from now
  }
}
```

The session key never holds funds directly. Gas fees are deducted from the granter's (user's main wallet) balance. The spend limit caps the total gas the session key can consume.

### Combined Setup (Single Approval)

Both the authz grant and the feegrant allowance can be bundled in a **single multi-message transaction**, so the user sees only one wallet popup:

```
Tx {
  messages: [
    MsgGrant { ... },          // authz: session key can send blog/forum msgs
    MsgGrantAllowance { ... }  // feegrant: main wallet pays gas for session key
  ]
}
```

---

## Security Considerations

### Session Key Lifecycle

| Phase | Action |
|-------|--------|
| **Create** | Frontend generates keypair; private key stored in browser memory (not localStorage for maximum security) |
| **Activate** | User signs MsgGrant + MsgGrantAllowance (single tx) |
| **Use** | Frontend auto-signs with session key; no wallet interaction |
| **Expire** | Grant has time-based expiration (e.g., 24h); session key becomes useless |
| **Revoke** | User can explicitly revoke via MsgRevoke (x/authz) at any time |
| **Logout** | Frontend discards session key from memory |

### Threat Model

| Threat | Mitigation |
|--------|------------|
| Session key stolen (XSS) | Grant is time-limited and message-type-scoped; attacker can only post/reply as the user, not drain funds |
| Session key used after logout | Grant expiration ensures automatic invalidation |
| Malicious frontend abuses grant | User reviews grant scope before signing; destructive actions excluded |
| Fee drain via gas spam | Feegrant spend_limit caps total gas budget |

### Best Practices

- **Expiration**: Default 24 hours; configurable by user
- **Memory storage**: Prefer in-memory over localStorage (cleared on tab close)
- **Scope minimally**: Only grant the message types the user actually needs for their current activity
- **Revoke on logout**: Call MsgRevoke when the user explicitly logs out (don't rely solely on expiration)
- **Display active grants**: Show the user what their session key can do and when it expires

---

## Implementation Notes

This is purely a **client-side pattern**. No custom module code is needed — `x/authz` and `x/feegrant` are standard Cosmos SDK modules included in every SDK chain.

The frontend is responsible for:
1. Generating the session keypair
2. Constructing and broadcasting the grant transaction
3. Storing the session private key securely
4. Auto-signing subsequent transactions with the session key
5. Cleaning up on logout or expiry

Cosmos SDK wallet libraries (e.g., CosmJS, Telescope) support `x/authz` exec transactions out of the box.
