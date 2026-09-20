# x/federation Peer Authorization Hardening Plan

Status: **IMPLEMENTED.** The devnet↔testnet link was proven end-to-end first
(content flowing both ways, identity link verified), which was the stated
precondition — the permissive gate is what made that bring-up cheap, and
hardening after live mainnet peers existed would have been a migration rather
than an edit. Decisions 1–5 are all landed; the sections below are kept as the
rationale, with implementation notes where reality differed from the plan.

Goal: make activating a federation peer an M-of-N committee decision rather
than something any single Operations Committee member — or a third-party bridge
operator — can do alone, while keeping registration and suspension cheap.

## The current state

Every peer-lifecycle handler shares one gate:

```go
commonsKeeper.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations")
```

Handlers using it: `register_peer`, `resume_peer`, `suspend_peer`,
`remove_peer`, `update_peer_policy`, `update_bridge`, `moderate_content`,
`update_operational_params`, `prune_orphan_bindings`, `resync_bridge_count`,
`resolve_escalated_challenge`.

[`IsCouncilAuthorized`](../x/commons/keeper/keeper_helpers.go) passes on any one
of four things:

1. the gov authority
2. the Commons Council policy address (a passed council vote)
3. the Operations Committee policy address (a passed committee vote)
4. **any individual Operations Committee member, signing directly**

Option 4 is what actually applies in practice, so peer activation is 1-of-N
over a committee of 2–5 (`MinMembers: 2`, `MaxMembers: 5`, hard-coded in
`genesis_bootstrap.go`). No Commons Council vote is required anywhere in the
peer lifecycle.

This is worth changing specifically because the M-of-N path already exists one
branch above — option 3 — and is being skipped. The codebase also already has
the stricter primitive `IsCouncilPolicyOrGov`, whose doc comment says it is
"for handlers that require an actual council vote… individual committee
membership does NOT satisfy this check". No federation handler uses it.

### The second activation path

`MsgRegisterBridge` is **operator-signed**, not council-gated, and flips a
PENDING peer to ACTIVE on first binding — in two places,
[msg_server_register_bridge.go](../x/federation/keeper/msg_server_register_bridge.go)
around the `Step 6: peer PENDING → ACTIVE transition on first binding` comment
and again after the `BindingsByOperator` write. Both emit `EventTypePeerActivated`.

This produces two activation paths whose trust models are inverted relative to
each other:

| peer kind | trust model | who can activate |
|---|---|---|
| IBC sister chain | cryptographic, trust-minimized | governance actor, via `ResumePeer` only |
| Bridge peer (ActivityPub, ATProto, NOSTR, Lens) | operator honesty | **the operator themselves** |

The lower-trust integration has the easier activation. Any hardening of
`ResumePeer` is void for external protocols while this holds.

## Why activation is the line

A PENDING peer is inert — `FederateContent`, `SubmitFederatedContent` and
`RequestReputationAttestation` all check `PEER_STATUS_ACTIVE`. Registration is
effectively tabling a proposal; activation is the trust decision.

The blast radius of a bad peer is real but bounded by design: reputation is
advisory, heavily discounted, capped at PROVISIONAL and TTL'd at 30 days.
Suspension is available under the same gate, so a mistake is reversible —
though imported content persists, and attestations persist until TTL. Bounded
and reversible is why this warrants a committee vote rather than a council one.

## The docs already claim the stricter model

[x/federation/README.md](../x/federation/README.md) documents an access model
the code does not implement:

> Commons Council registers/removes peers. Operations Committee manages policies.

and its message table lists `MsgRegisterPeer`, `MsgRemovePeer`,
`MsgSuspendPeer` / `MsgResumePeer` as **Commons Council**, with only
`MsgUpdatePeerPolicy` as Operations Committee.

So this is not a proposal to tighten an intentional design — it is a proposal
to close a gap between documented intent and shipped behaviour. There are three
positions in play and only one can survive:

| source | who may activate a peer |
|---|---|
| `x/federation/README.md` | Commons Council |
| shipped code | any single Operations Committee member |
| this plan | Operations Committee policy address (M-of-N) |

This plan argues for the middle position on the grounds below, but **whoever
implements this must pick one and make the README match**. Leaving the README
claiming Council while the code does something else is worse than either choice,
because it is the document a reviewer checks the code against.

RESOLVED: the middle position shipped, and
[x/federation/README.md](../x/federation/README.md) was rewritten to match —
`MsgResumePeer` is listed as requiring the committee policy, everything else in
the lifecycle as a single member's signature.

## Decisions

**1. `RegisterPeer` stays 1-of-N. No change.**
A PENDING peer can do nothing. Making registration harder buys no safety and
adds friction to the thing that should be cheap.

**2. `ResumePeer` requires the committee *policy* address (M-of-N).** DONE.
Depends on Decision 5: with the absolute threshold of 1 the committees used
to carry, this change buys an audit trail and a delay but no multi-party
review at any committee size.
Not the Commons Council: a 12h testnet / 5d mainnet council vote to form a
bilateral link fights the spec's own sovereignty model, where cheap bilateral
relationships are the point. Operations Committee min-execution is 5 min
(devnet), 10 min (testnet), 24h (mainnet) — the right shape for an
outward-facing but bounded decision.

**3. `SuspendPeer` and `RemovePeer` stay 1-of-N. Deliberately.**
This asymmetry is the point: hard to start a trust relationship, easy to stop
one. A single member must be able to pull the emergency brake without
assembling a quorum. Do not "fix" this for consistency.

**4. `RegisterBridge` stops auto-activating.** DONE.
Require the peer to already be ACTIVE before a binding is accepted. Governance
activates in both cases; operators bind to something already approved.

**5. Committee decision policies are `percentage` 0.5, not an absolute
threshold of 1.** DONE — landed ahead of the rest of this plan, because it is
what makes Decision 2 worth doing.

Measured on the live chains before the change, every committee ran
`policy_type: "threshold", threshold: "1"` — an *absolute count*. One member
carries a vote however large the committee grows, so routing `ResumePeer`
through the committee policy would have bought an audit record and a delay but
never multi-party review. Decision 2's stated benefit did not exist.

`checkThreshold` evaluates a percentage as yes-weight over **total committee
weight** with `>=` — non-voters count against, abstaining equals opposing. At
weight 1 per member, 0.5 is `ceil(n/2)`:

| members | yes needed |
|---|---|
| 1 | 1 |
| 2 | 1 |
| 3 | 2 |
| 4 | 2 |
| 5 | 3 |

0.5 rather than 0.51: both leave a single-member committee unblocked, which
matters for bringing a chain up, but 0.51 is `ceil((n+1)/2)` and makes a
two-member committee unanimous — one absent member blocks everything on a body
capped at five. The accepted cost is that n=2 still passes on one vote; the
remedy is a third member, not a stricter ratio.

The **Commons Supervisory Board keeps its absolute threshold of 2**. It is a
deliberately higher bar and 0.5 would lower it at n=2.

Timing, also measured: with a single member the 5-day `voting_period` never
applies, because a vote crossing the threshold triggers *early acceptance* and
recomputes `execution_time = now + min_execution_period`. So a hardened
`ResumePeer` costs 3 transactions plus **5 minutes on devnet, 10 on testnet,
24 hours on mainnet** — not days.

Applied in `genesis_bootstrap.go` to all six committees, so it takes effect on
the next chain reset. Existing deployed chains would need a governance change
to their decision policies. Pinned by `TestCommitteePercentageMajority` and
`TestSupervisoryBoardKeepsAbsoluteThreshold`.

## The second grant, which this plan originally missed

Authorizing a governance message on this chain takes **two independent grants,
in different modules**, and satisfying only one makes the message unexecutable
rather than merely unauthorized:

1. **The module keeper must accept the caller.** `MsgResumePeer` uses
   `IsCouncilOrCommitteePolicy`, which accepts a committee policy address.
2. **x/commons must let that policy carry the message.** A commons proposal is
   checked against the target policy's `AllowedMessages`
   ([msg_server_proposals.go](../x/commons/keeper/msg_server_proposals.go)) and
   rejected with `ErrUnauthorized` otherwise.

Decision 2 did (1) and forgot (2). `genesis_bootstrap.go` grants the peer
lifecycle -- `RegisterPeer`, `RemovePeer`, `SuspendPeer`, `ResumePeer` -- to the
**Commons Council** policy, and gives the Operations Committee only the
operational chores. So after the hardening the committee was the only body that
could pass the vote at n=1, and the only body forbidden from executing the
result.

That is a hard deadlock, not a slow path. The Council holds the permission but
needs 0.51 of its *entire* membership (4 of 6 on devnet, 5 of 9 on testnet),
which no single operator can muster; and the committee cannot grant itself the
message, because `MsgUpdatePolicyPermissions` is not in its allowlist either.
It blocked the live devnet/testnet bring-up until `MsgResumePeer` was added to
the committee's `AllowedMessages`.

`ResumePeer` deliberately stays on the Council's list too. Both are valid
callers in the keeper, and leaving it there keeps an escalation path if a
committee is ever unable to act.

**Pinned by `TestOpsCommitteeCanExecuteCommitteeGatedFederationMsgs`**, which
derives the committee-gated set from the handlers rather than hardcoding it, so
a future message gated on `IsCouncilOrCommitteePolicy` fails a test instead of a
live chain. The lesson generalizes: whenever a handler starts requiring a policy
address, check that the policy is permitted to carry the message.

## Implementation

1. **Add `IsCouncilOrCommitteePolicy(ctx, addr, council, committee) bool` to
   x/commons.** Gov authority, council policy address, or committee policy
   address — *excluding* individual membership. Neither existing helper covers
   this: `IsCouncilAuthorized` includes individuals, `IsCouncilPolicyOrGov`
   excludes committee policies. Put it next to the other two in
   `keeper_helpers.go` and document why it sits between them.

2. **Extend the federation `CommonsKeeper` expected-keeper interface** with the
   new method (`x/federation/types/expected_keepers.go`).

3. **Switch `msg_server_resume_peer.go`** to the new helper. Error message
   should name the committee policy address explicitly — "signed by a committee
   member, needs a committee vote" is the confusing case and deserves a clear
   message.

4. **Remove both PENDING → ACTIVE blocks from `msg_server_register_bridge.go`**
   and reject a binding whose peer is not ACTIVE, with a typed error. Keep
   `EventTypePeerActivated` — it is still emitted by `ResumePeer`.

5. **Leave `register_peer`, `suspend_peer`, `remove_peer` untouched.**

Deliberately NOT changed: `update_peer_policy`, `update_bridge`,
`moderate_content`, `update_operational_params` and the recovery messages
(`prune_orphan_bindings`, `resync_bridge_count`). They are either reversible
operational chores or already-scoped corrections. Revisit only with a specific
reason.

## Tests

- `ResumePeer` accepted from the committee policy address; **rejected** from an
  individual committee member. That second case is the whole point of the
  change and must be asserted explicitly, not implied.
- `ResumePeer` still accepted from gov authority and from the council policy
  address.
- `SuspendPeer` still accepted from an individual member — pins the asymmetry
  so a later uniformity pass has to argue with a failing test.
- `RegisterBridge` against a PENDING peer is rejected, and leaves the peer
  PENDING.
- `RegisterBridge` against an ACTIVE peer still succeeds.
- Existing multichain E2E (`test/federation/multichain/`) needs its peer setup
  updated to activate via a committee vote — `setup_peers.sh`. (It turned out
  already to activate via the council policy; see Migration.)

All of the above are in
[x/federation/keeper/msg_server_peer_authorization_test.go](../x/federation/keeper/msg_server_peer_authorization_test.go).
The fixture's commons stub authorizes every address by default, so each test
there replaces it with one that tells the committee policy, the council policy
and an individual member apart — the rejected-member case asserts against the
*same* address that `SuspendPeer` is asserted to accept.

## Migration

Any peer already ACTIVE stays ACTIVE; this changes who may *perform* a
transition, not stored state, so no state migration is needed. Peers sitting
PENDING at upgrade time need a committee vote to activate rather than a single
signature — worth announcing rather than discovering.

The claim above that the bring-up scripts "assume single-signature activation"
was only half right, and the half it got wrong is the more interesting one:

- `test/federation/multichain/setup_peers.sh` and `test/federation/peer_fixtures.sh`
  already activated through a **Commons Council policy proposal**, which
  satisfies the new gate unchanged. Nothing to do.
- `deploy/relayer/setup_peers.sh` did sign `resume-peer` directly, and was
  rewritten to submit → vote → execute an Operations Committee proposal. It
  reads `execution_time` off the proposal rather than assuming a
  `min_execution_period`, so the same script works on devnet (5 min) and
  testnet (10 min). A side with no local key gets the submit emitted unsigned
  and is told to finish the vote and execute itself.

What actually broke was **Decision 4**, not Decision 2: every fixture that
bound a bridge relied on `RegisterBridge` flipping its PENDING peer to ACTIVE
as a side effect. Both the Go keeper fixtures and the shell fixtures now walk
the real lifecycle (register → activate → bind) instead.

## Rejected alternatives

- **Escalate to Commons Council.** Council latency (5d mainnet) makes routine
  federation operations painful and contradicts "bilateral relationships only,
  no supergovernment". Rejected.
- **Harden everything uniformly.** Destroys the start-hard/stop-easy asymmetry
  that makes the emergency brake usable. Rejected.
- **Keep auto-activation but gate `RegisterBridge` on governance.** Bridge
  registration is operator-initiated by design and requires a bond; forcing a
  governance signature on it changes the role rather than the authorization.
  Rejected in favour of requiring ACTIVE first.
