# x/vault — Shared Accounts with Rotatable Signers

**Status:** Implemented (2026-10-25). Design history and the security review that shaped it:
`docs/untracked/multisig-accounts-plan.md` §4 and §8.

## 1. Purpose

A **vault** is a SPARK-holding account that is *not one person*: a founding group's custody, a
project or guild treasury, a collective that owns an artifact class, a validator team's reward
address. It has a **stable, keyless, module-derived address**, a **weighted signer set that
rotates in place**, and an on-chain approval flow in which every signer acts with an ordinary
single-signer tx, so Keplr and Ledger work.

Native k-of-n multisig ([multisig-accounts.md](multisig-accounts.md)) remains the tool for one
person protecting their own account: it can be a member, but its signers are fixed for the life of
the address and its signatures are collected off chain. A vault is the opposite on both counts, and
**a vault can never hold member identity** (§6).

Why a separate leaf module and not a "private" x/commons group: commons groups are governance
bodies (hierarchy, terms, x/split shares, futarchy confidence votes, `AllowedMessages`,
`IsCouncilBodyPolicy` trust). A private-group flag would have to opt out of every coupling point,
and one missed opt-out would hand a treasury governance trust. x/vault is never recognised as a
council body, depends only on x/bank, x/auth and the msg router, and is late-wired to x/rep for the
alias contract and the personhood gate.

## 2. State

```protobuf
message Vault {
  uint64 vault_id = 1;
  // address = sdkaddress.Module("vault", "vault/" || bigEndian(vault_id)); no key exists for it.
  string address = 2;
  // Weighted signers; no duplicates, every weight in [1, MaxSignerWeight].
  repeated VaultSigner signers = 3;
  // Sum of approving weights needed to execute; 1 <= threshold <= sum(weights).
  uint64 threshold = 4;
  // Bumped on every signer/threshold change; pending proposals created under an
  // older version are INVALIDATED (approvals never combine across signer sets).
  uint64 config_version = 5;
  // Seconds a new proposal stays PENDING; MinProposalTTL <= proposal_ttl <= MaxProposalTTL.
  int64 proposal_ttl = 6;
  // Display only, <= MaxLabelLength chars.
  string label = 7;
  // Block time (unix seconds) of creation.
  int64 created_at = 8;
}

message VaultSigner {
  string address = 1;
  uint64 weight = 2;
}

enum ProposalStatus {
  PROPOSAL_STATUS_UNSPECIFIED = 0;
  PROPOSAL_STATUS_PENDING = 1;      // collecting approvals
  PROPOSAL_STATUS_EXECUTED = 2;     // every inner message ran
  PROPOSAL_STATUS_CANCELLED = 3;    // by the proposer or the vault itself
  PROPOSAL_STATUS_EXPIRED = 4;      // expires_at passed (EndBlocker)
  PROPOSAL_STATUS_INVALIDATED = 5;  // the vault's config changed after creation
}

message VaultProposal {
  uint64 proposal_id = 1;
  uint64 vault_id = 2;
  // Canonical (x/rep alias) address of the proposer.
  string proposer = 3;
  // Inner messages; each GetSigners() must be exactly [vault.address]; at most
  // MaxMessagesPerProposal; denylisted types are refused at propose and at execute.
  repeated google.protobuf.Any messages = 4;
  // sha256 over the deterministic encoding of `messages`; MsgApprove must repeat it.
  bytes messages_hash = 5;
  // Vault config_version at creation; a mismatch means INVALIDATED.
  uint64 config_version = 6;
  // Canonical addresses of approvers (the proposer approves implicitly).
  repeated string approvals = 7;
  ProposalStatus status = 8;
  // Unix seconds after which a PENDING proposal becomes EXPIRED.
  int64 expires_at = 9;
  // Display only, <= MaxMemoLength chars.
  string memo = 10;
  // Unix seconds when the proposal left PENDING; drives retention pruning.
  int64 resolved_at = 11;
}
```

Collections: `Vault[vault_id]`, `VaultProposal[proposal_id]`, `VaultSeq`, `ProposalSeq`,
`VaultByAddress[address] → vault_id`, `VaultsBySigner[(signer, vault_id)]`,
`PendingByVault[(vault_id, proposal_id)]`, `PendingByProposer[(vault_id, proposer, proposal_id)]`,
`ExpiryQueue[(expires_at, proposal_id)]`, `RetentionQueue[(resolved_at, proposal_id)]`. Genesis
carries only the two value maps; every index, queue and sequence is rebuilt at import.

`messages_hash` = SHA-256 over, for each `Any` in order, the big-endian uint32 length of the type
URL, the URL bytes, the big-endian uint32 length of the value and the value bytes
(`types.MessagesHash`). The `vault_proposal_created` event carries it as hex.

### Compiled bounds

They bound gas (every pass over a vault's signers, approvals, pending proposals or messages is
capped by them), so they are constants, not params ([x/vault/types/types.go](../x/vault/types/types.go)).

| Constant | Value | Why |
|---|---|---|
| `MaxSigners` | 10 | tally and index passes |
| `MaxSignerWeight` | 1000 | the weight sum can never overflow |
| `MaxMessagesPerProposal` | 16 | execution pass |
| `MaxPendingProposalsPerProposer` | 8 | per signer per vault, see §4 anti-lockout |
| `MaxPendingUpdateProposalsPerProposer` | 2 | the rotation exemption is itself bounded |
| `MinProposalTTL` | 1 h (network), 10 s (test build) | `bounds_vals_*.go` |
| `MaxProposalTTL` | 30 days | |
| `ProposalRetention` | 7 days (network), 60 s (test build) | resolved proposals stay queryable |
| `MaxLabelLength` / `MaxMemoLength` | 64 / 256 | |
| `MaxEndBlockerPassItems` | 50 | per EndBlocker pass |

The vault-wide pending count is therefore at most `MaxSigners × 10`, which bounds the invalidation
pass a rotation runs inside a tx.

### Params

| Param | Default | Meaning |
|---|---|---|
| `creation_enabled` | `true` | Gates `MsgCreateVault` **only**. Approve, revoke, execute, cancel and update keep working for existing vaults whatever its value, so governance can never freeze funds already in a vault. |
| `creation_fee_amount` | `10000000` (10 SPARK) | Burned by `MsgCreateVault`, in bond-denom micro-units (the same spam pricing as tag creation). |

## 3. Messages

All are single-signer and carry `(amino.name)`. **Every x/vault message is in x/session's
`ForbiddenAllowlistMsgs`** (built from `vaulttypes.NonDelegableMsgTypeURLs()`): a session hot key
must never cast a co-signer's approval, open or execute a proposal, or create a vault. Guard:
[x/vault/types/allowlist_guard_test.go](../x/vault/types/allowlist_guard_test.go) walks the Msg service.

| Msg | Signer | Effect |
|---|---|---|
| `MsgCreateVault{creator, signers, threshold, proposal_ttl, label}` | anyone | Burns `creation_fee_amount`; validates the config (§3.1); creates the keyless `BaseAccount` (an existing keyless base account at the derived address, left by an early bank send, is accepted; any other account type is refused); emits `vault_created`. The creator need not be a signer. |
| `MsgPropose{signer, vault_id, messages, memo, execute}` | a current signer | Validates every inner message (§4), records the proposer's approval, emits `vault_proposal_created` (with `messages_hash`), and executes at once when `execute` is set and the threshold is already met. Refused when the proposer has `MaxPendingProposalsPerProposer` pending proposals in this vault; a proposal whose only message is `MsgUpdateVault` counts against `MaxPendingUpdateProposalsPerProposer` instead. |
| `MsgApprove{signer, proposal_id, messages_hash, execute}` | a current signer | `messages_hash` must equal the proposal's (the CLI requires it as an argument, so a hardware wallet shows what is approved rather than a bare id). Optional immediate execute. |
| `MsgRevokeApproval{signer, proposal_id}` | an approver | Withdraws the approval before execution. |
| `MsgExecute{signer, proposal_id}` | any current signer | Executes once the tally meets the threshold; the executor pays gas. A failing inner message fails the tx, which reverts everything and leaves the proposal PENDING for a retry until it expires. |
| `MsgCancelProposal{signer, proposal_id}` | the proposer, or the vault address through an executed proposal | CANCELLED. With `MsgUpdateVault` it is one of the two x/vault messages allowed as an inner message, so a threshold of signers can clear a proposal nobody wants. |
| `MsgUpdateVault{vault, signers, threshold, proposal_ttl, label}` | **the vault address**, so only via an executed proposal | Replaces the configuration, re-indexes `VaultsBySigner`, bumps `config_version` and INVALIDATES every other pending proposal. |
| `MsgUpdateParams` | gov | |

### 3.1 Configuration rules (create and update)

`1 ≤ len(signers) ≤ MaxSigners`; no duplicate signer; every weight in `[1, MaxSignerWeight]`;
`1 ≤ threshold ≤ Σ weights`; `MinProposalTTL ≤ proposal_ttl ≤ MaxProposalTTL`; label within
length; every signer address valid and **personal**: not a vault, and (through x/rep's
`IsPersonalAddress`) not a module account, group policy or reserved address. A native multisig
address is a fine signer. There is no way to produce an empty signer set or an unreachable
threshold, so a vault can never brick itself. Invalid rotations are refused at execution, so a
proposal carrying one simply fails and stays pending.

### 3.2 Tally

Σ weight of signers who are *current* at tally time and whose canonical address is among the
approvals. Approvals are recorded under the approver's canonical x/rep address, so an approval
given before a key migration still counts once after it, and a removed signer's approval never
counts. `MsgApprove`, `MsgExecute` and the `tally` query all re-tally live.

### 3.3 Why INVALIDATED on any config change

Approvals given under one signer set must never combine with another; `config_version` is the
vault's equivalent of a Safe nonce. Re-proposing is cheap. Ordering: the executing proposal is
marked EXECUTED and removed from the pending indexes *before* its inner messages run, so a
`MsgUpdateVault` inside it never invalidates the proposal that carries it, and a proposal that
bundles a rotation with other messages executes atomically.

## 4. Execution and the inner-message rules

1. Unpack each `Any`; each inner message's `GetSigners()` must be exactly `[vault.address]`
   (`ErrInnerSignerMismatch`), so a proposal can never escalate to another signer.
2. Refuse the denylist, at propose *and* at execute (the list may change between them):
   - every x/rep **identity** message (`reptypes.IdentityMsgTypeURLs()`: the member-key-migration
     messages, `MsgRegisterZkPublicKey`, `MsgClearZkPublicKey`) — a vault has no personhood,
     and this holds even if a wiring step were ever missed;
   - `shield.MsgShieldedExec` (anonymous execution);
   - `session.MsgExecSession` (a vault may *grant* session keys but never *exec* as a grantee);
   - every x/vault message except `MsgUpdateVault` and `MsgCancelProposal` (no vault-in-vault
     recursion).
3. Run every message through the baseapp msg router inside a `CacheContext`, all or nothing; inner
   events are re-emitted on the parent context; emit `vault_proposal_executed`.

The executor pays the gas. A vault that wants to sponsor its signers' gas grants them an x/session
`SessionKey` with fee delegation and an empty `allowed_msg_types`; a vault `SpendingAllowance` to a
signer is a per-person spending cap. Both are existing x/session primitives (the Safe "allowance
module" with no new code); `test/vault/vault_test.sh` exercises the allowance path.

**Anti-lockout.** A vault-wide pending cap would let one rogue signer fill the queue so that the
others could not even propose the rotation that removes them. The cap is therefore per proposer,
and update-only proposals have their own small cap, so rotation is always possible and the
exemption cannot bloat state. Rotating a signer out invalidates every one of their proposals.

## 5. EndBlocker

Two capped passes (`MaxEndBlockerPassItems` each), the same bounded-pass pattern as x/session:

1. `ExpiryQueue` entries with `expires_at ≤ now`: PENDING → EXPIRED (`vault_proposal_expired`).
2. `RetentionQueue` entries with `resolved_at ≤ now − ProposalRetention`: the proposal record is
   deleted (`vault_proposal_pruned`); the events carry the audit trail.

Approve and execute already refuse a proposal past `expires_at` or with a stale
`config_version` before the EndBlocker reaches it.

## 6. No personhood

A vault is never a member and is never trusted as a governance body:

- **x/rep** refuses a vault everywhere an identity is created or moved. `IsPersonalAddress`
  (module account, group policy, reserved address, vault) gates `CreateInvitation`,
  `AcceptInvitationWithRecovery` (re-run at acceptance, so a vault created in between, or an
  executed proposal signing as the invitee, is still refused) and migration-target freshness
  (`checkTargetFresh`). x/rep learns vault addresses through the late-wired `SetVaultChecker`
  (`app.go`), which reads state; rep's `AddReservedAddress` is an in-memory list fixed at wiring and
  cannot learn addresses created at runtime. One gate covers bonded roles, juries, trust levels,
  DREAM, x/name and every other member-gated surface.
- **Not a council body:** `IsCouncilBodyPolicy` is false for a vault, so it is not an x/artifact
  trusted sender (`MEMBERS`-policy recipients route its sends through their inbox) and the x/session
  claim hook ignores it.
- **x/service:** a vault cannot be an operator (operators sign checkpoints as hot keys) or a
  controller (controllers are commons groups); the existing checks already refuse it.
- **Signers** may not be vaults or other non-personal addresses (§3.1).

## 7. Signer key migration

A signer who is a member may migrate `A → B`. x/vault does **not** re-key eagerly (one signer may
sit in any number of vaults, and migration finalization is a bounded EndBlocker); it follows the
x/rep alias contract like every module with unbounded state:

- signer authorization is `CanActAs(signer, stored)`, so `B` acts for a stored `A` and a
  tombstoned `A` never passes;
- approvals are recorded and tallied by canonical address, so `A` and `B` cannot both approve;
- `vaults-by-signer` unions over `AllAddressesOf`.

Signer entries stay as written, `config_version` does not move, pending approvals stay valid. A
non-member signer has no alias and every helper degrades to equality.

## 8. Queries and CLI

`get-vault [id]`, `list-vault`, `vault-by-address [addr]`, `vaults-by-signer [addr]` (paginated,
alias-aware), `get-vault-proposal [id]`, `list-vault-proposal`, `proposals-by-vault [vault-id]
[--status]` (paginated), `proposals-by-proposer [addr] [--status]` (paginated, alias-aware, across
vaults: shows a signer's use of the per-proposer pending caps), `tally [proposal-id]` (`approved_weight`, `threshold`, `met`), `params`.

Hand-written tx commands (autocli cannot build signer sets or `repeated Any`):
`tx vault create-vault <addr:weight,...> <threshold> <ttl-seconds> [label]`,
`tx vault propose <vault-id> <messages.json> [--execute] [--memo]` where the file is
`{"messages":[{"@type":"/cosmos.bank.v1beta1.MsgSend", ...}], "memo":""}` and every message is
signed by the vault address, and `tx vault approve <proposal-id> <messages-hash-hex> [--execute]`.
Autocli builds `revoke-approval`, `execute`, `cancel-proposal`; `update-vault` is not exposed (only
reachable as an inner message).

## 9. Events

`vault_created{vault_id, address, creator, threshold}`,
`vault_proposal_created{vault_id, proposal_id, proposer, messages_hash, expires_at}`,
`vault_proposal_approved{proposal_id, approver, approved_weight}`,
`vault_approval_revoked{proposal_id, approver}`,
`vault_proposal_executed{vault_id, proposal_id, executor}`,
`vault_proposal_cancelled{proposal_id, by}`, `vault_updated{vault_id, config_version, threshold,
signers}`, `vault_proposal_invalidated{proposal_id}`, `vault_proposal_expired{vault_id,
proposal_id}`, `vault_proposal_pruned{proposal_id, status}`.

## 10. Errors

| Code | Name | When |
|---|---|---|
| 1100 | `ErrInvalidSigner` | `MsgUpdateParams` not signed by the authority |
| 1101 | `ErrCreationDisabled` | `creation_enabled = false` |
| 1102 | `ErrInvalidSignerSet` | empty set, too many, duplicate, bad weight, bad address |
| 1103 | `ErrInvalidThreshold` | 0 or above Σ weights |
| 1104 | `ErrInvalidProposalTTL` | outside `[MinProposalTTL, MaxProposalTTL]` |
| 1105 | `ErrInvalidLabel` | above `MaxLabelLength` |
| 1106 | `ErrVaultNotFound` | |
| 1107 | `ErrNotSigner` | signer (alias-resolved) is not in the current set |
| 1108 | `ErrProposalNotFound` | |
| 1109 | `ErrProposalNotPending` | approve/revoke/execute/cancel on a resolved proposal |
| 1110 | `ErrTooManyPending` | proposer at a pending cap |
| 1111 | `ErrNoMessages` | |
| 1112 | `ErrTooManyMessages` | above `MaxMessagesPerProposal` |
| 1113 | `ErrInnerMsgDenied` | denylisted type (§4) |
| 1114 | `ErrInnerSignerMismatch` | an inner message is not signed by exactly the vault |
| 1115 | `ErrMessagesHashMismatch` | `MsgApprove.messages_hash` differs |
| 1116 | `ErrAlreadyApproved` | |
| 1117 | `ErrNotApproved` | revoke without an approval |
| 1118 | `ErrThresholdNotMet` | execute below threshold |
| 1119 | `ErrNotProposer` | cancel by someone other than the proposer or the vault |
| 1120 | `ErrNotPersonalAddress` | a signer is a vault / module account / policy / reserved |
| 1121 | `ErrInvalidMemo` | above `MaxMemoLength` |
| 1122 | `ErrProposalExpired` | `expires_at` passed (before the EndBlocker marked it) |
| 1123 | `ErrConfigChanged` | proposal `config_version` is stale (before the EndBlocker/rotation marked it) |
| 1124 | `ErrAddressOccupied` | the derived address holds a non-base or keyed account |

## 11. Invariants and tests

Invariants (run by `go test ./app -run TestInvariants`): `vault-indexes` (`VaultByAddress`,
`VaultsBySigner` and `Vault` agree; every address derives from its id; every threshold is
reachable) and `proposal-indexes` (every pending index and the expiry queue point at PENDING
proposals and agree in count; the retention queue points at resolved ones; every proposal belongs
to a vault).

Unit: [x/vault/keeper/vault_test.go](../x/vault/keeper/vault_test.go) (creation and refusals,
propose/approve/revoke/execute, cancel by proposer and by the vault, expiry and retention, rotation
atomicity and invalidation, the anti-lockout caps, the denylist, the alias contract, genesis round
trip, queries), [x/vault/types/allowlist_guard_test.go](../x/vault/types/allowlist_guard_test.go),
[x/rep/keeper/personal_address_test.go](../x/rep/keeper/personal_address_test.go). Simulation ops
in `x/vault/simulation` open update-only proposals (always executable, no balance needed) and
approve, execute, revoke and cancel them. E2E: [test/vault/vault_test.sh](../test/vault/vault_test.sh).

## 12. Non-goals

Shared member identity; nested vaults or vaults as signers; programmable per-tx policies inside the
vault (use x/session grants from the vault; build them in only if those prove insufficient);
account abstraction in the ante handler (a vault paying its own fees as a tx signer); a refundable
creation deposit with close semantics (a closed vault may still own artifacts, names or grants
elsewhere, so vaults are permanent and the fee is burned).
