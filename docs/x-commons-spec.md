# Technical Specification: `x/commons`

## 1. Abstract

The `x/commons` module is the orchestrator of Spark Dream's **"Three Pillars"
governance**. It owns the council registry, the native proposal lifecycle
(submit → vote → execute), the per-policy permission system that scopes what
each council can do, the spending governor that meters council treasuries,
and the bootstrap pipeline that materialises the entire governance graph at
genesis. It also acts as the integration hub between governance and three
sibling modules: `x/futarchy` (elastic tenure via prediction markets),
`x/split` (automated revenue distribution to council policy addresses), and
`x/shield` (anonymous proposals and votes routed through ZK proofs).

Key principles:

- **Native proposal system**: purpose-built `Group` + `DecisionPolicy` +
  `Proposal` model owned end-to-end by this module. `x/commons` is the
  single source of truth for council identity, voting rules, and proposal
  state — no external proposal module is involved.
- **Three Pillars hierarchy**: at genesis, three top-level councils (Commons,
  Technical, Ecosystem) plus a Supervisory Board are wired together with a
  parent-child trust chain (`Gov → Council → Committee`). Cycles are
  detected and rejected. Each council/committee owns a deterministic
  policy address derived from its council ID.
- **Two policies per council**: a *standard* policy (normal voting) and an
  optional *veto* policy (cross-council kill switch). Each carries its own
  `DecisionPolicy` (threshold, voting period, min execution period) and its
  own `PolicyPermissions` (allowed message types).
- **Allowlist permissions**: a council can only execute message types
  explicitly listed in its `PolicyPermissions.allowed_messages`. Any
  message in a proposal whose type URL isn't on the list rejects with
  `ErrUnauthorized` at **submit time** (`MsgSubmitProposal` /
  `MsgSubmitAnonymousProposal`), so the proposal never enters the voting
  store. A duplicate check runs at execute time as defence-in-depth in
  case the policy's allowlist was narrowed via `MsgUpdatePolicyPermissions`
  while the proposal was in flight.
- **Authority validation per inner message**: when a proposal executes, every
  inner message has its declared `signer` field cross-checked against the
  policy address — closing the privilege-escalation gap where a malicious
  proposer might forge `authority: "x/gov"` on a message bound for a council.
- **Elastic tenure via futarchy hook**: when a confidence-vote market
  resolves, `Keeper.AfterMarketResolved` extends the council's term by 20%
  on YES, slashes it by 50% on NO, or no-ops on INVALID. Cap: a council
  cannot live more than two full terms past `now`.
- **Cumulative spending limit**: each policy's `MaxSpendPerEpoch` is enforced
  cumulatively per UTC day, not per transaction — multiple `MsgSpendFromCommons`
  in the same epoch sum together. Recurring-spend claims share the same
  gating helper (`CheckSpendPreconditions`) and therefore cannot side-step
  the per-epoch ceiling.
- **Recurring spends (pull model)**: councils can pre-approve a schedule
  via `MsgScheduleRecurringSpend` (wrapped in a proposal). Each elapsed
  period the **recipient** submits `MsgClaimRecurringSpend` to pull one
  disbursement. Term-expired councils auto-pause (claims reject with
  `ErrGroupExpired`); term renewal auto-resumes. Schedules can be
  cancelled via a follow-up council proposal
  (`MsgCancelRecurringSpend`) or declined unilaterally by the recipient
  (`MsgDeclineRecurringSpend`) — e.g. when leaving a role.
- **Anonymous governance**: `MsgSubmitAnonymousProposal` and
  `MsgAnonymousVoteProposal` are accepted only from the `x/shield` module
  account. The shield module's ZK proof attests to voter registration; the
  commons handler skips the membership check but still enforces every other
  gate (term, allowed-messages, threshold). Anonymous votes have uniform
  weight=1 and are tallied separately, then folded into the threshold check.
- **Veto by policy version bump**: `MsgVetoGroupProposals` does not iterate
  proposals — it bumps the policy's `version`. Every accepted proposal
  records the version at submit time; on execute, a mismatch flips the
  proposal to `VETOED` instead of running.

---

## 2. Dependencies

| Module | Purpose |
|---|---|
| `x/auth` | Address codec, module account resolution, account iteration during genesis to seed founding-member lookups |
| `x/bank` | Council treasury sends (`SpendFromCommons`, `ClaimRecurringSpend`), proposal-fee transfers from the proposer |
| `x/gov` | Source of the immutable `authority` (gov module account) used as the chain's `--authority` for `MsgUpdateParams`, `MsgEmergencyCancelGovProposal`, etc.; concrete keeper accessed via the narrow `GovKeeper` interface for the emergency-cancel path |
| `x/upgrade` | `MsgForceUpgrade` calls `UpgradeKeeper.ScheduleUpgrade` |
| `x/futarchy` | `CreateMarketInternal` (initiated by commons for confidence-vote markets); commons registers as a `FutarchyHooks` consumer for elastic tenure on resolution. Injected directly via depinject |
| `x/split` | `SetShareByAddress` registers the policy address with the revenue split when a `Group` is created. Injected directly via depinject |
| `x/shield` | Anonymous proposal and vote routing — the `IsShieldCompatible` adapter advertises `MsgSubmit/AnonymousVoteProposal` to the shield router. No direct keeper dependency |
| `x/name` | *(optional)* Genesis bootstrap calls `SetDisplayName` and `ClaimName` to seed founding-member display names and reserve canonical handles. Wired post-depinject via `SetNameKeeper`; missing keeper is logged and skipped |
| `x/rep` | *(reverse direction)* `x/rep` reads `IsCommitteeMember`, `IsCouncilAuthorized`, `IsGroupPolicyAddress`, `IsGroupPolicyMember`. `x/commons` does **not** import `x/rep` |

A small set of keepers (`gov`, the message router, `name`) are wired
post-depinject because they are constructed AFTER `depinject.Inject` runs
(or because optional-depinject would re-introduce a cycle). Those three
live in a shared `lateKeepers` pointer struct so that value-copies of
`Keeper` (in the msg server and EndBlocker) all see the late assignment.
`futarchy` and `split` are NOT in `lateKeepers` — they're direct depinject
fields on the `Keeper` struct, available from construction.

---

## 3. State Objects

### 3.1. Group

The single record describing a council or committee — identity, lineage,
funding share, lifecycle, and futarchy enablement.

```protobuf
message Group {
  string index = 1;                       // council/committee name (primary key)
  uint64 group_id = 2;                    // monotonically-increasing integer ID
  string policy_address = 3;              // standard policy (deterministic from group_id)
  string parent_policy_address = 4;       // governance parent (Gov for top-level councils)
  string electoral_policy_address = 5;    // optional: who can renew/replace members
  uint64 funding_weight = 6;              // share of x/split revenue (basis points or weight)
  string max_spend_per_epoch = 7;         // cosmossdk.io/math.Int — 0 = no cap
  int64 update_cooldown = 8;              // min seconds between MsgUpdateGroupConfig calls
  uint64 min_members = 9;
  uint64 max_members = 10;
  int64 term_duration = 11;               // seconds; future term length on renewal
  int64 current_term_expiration = 12;     // unix seconds; 0 = no expiry
  int64 activation_time = 13;             // unix seconds; 0 = active immediately
  int64 last_parent_update = 14;          // unix seconds; throttles parent meddling
  bool futarchy_enabled = 15;             // confidence-vote markets allowed?
  string veto_policy_address = 16;        // optional second policy for cross-council vetoes
}
```

Lifecycle states (computed, not stored):

| State | Condition |
|---|---|
| Shell (pre-launch) | `activation_time > 0 && now < activation_time` |
| Active | `now >= activation_time && now <= current_term_expiration` (or `current_term_expiration == 0`) |
| Expired (zombie) | `current_term_expiration > 0 && now > current_term_expiration` — loses spending power until parent calls `MsgRenewGroup` |

### 3.2. Member

A council/committee member, keyed by `(council_name, address)`.

```protobuf
message Member {
  string address = 1;
  string weight = 2;                     // string-encoded LegacyDec
  string metadata = 3;                   // optional; e.g. "Founder Tier 1"
  int64 added_at = 4;                    // unix seconds
}
```

Weights are `LegacyDec` strings to support fractional or symbolic weights
(e.g., the founder's golden share gets weight `3`, ordinary members get
`1`). Threshold tallies sum these weights as decimals.

### 3.3. DecisionPolicy

Voting rules attached to a policy address. Stored separately from `Group`
because a single council has both a *standard* and a *veto* policy address,
each with its own voting rules.

```protobuf
message DecisionPolicy {
  string policy_type = 1;                // "percentage" or "threshold"
  string threshold = 2;                  // "0.51" for percentage; "3" for threshold
  int64 voting_period = 3;               // seconds the proposal stays open
  int64 min_execution_period = 4;        // seconds after acceptance before executable
}
```

`policy_type` is plain string, not an enum. Two values:

- `"percentage"` — `yes_weight / total_member_weight >= threshold` (with
  `threshold` parsed as `LegacyDec` in `[0,1]`).
- `"threshold"` — `yes_weight >= threshold` (with `threshold` parsed as a
  positive integer; the absolute weight required).

`min_execution_period` is what the test-params verifier reads to
distinguish testparams (1 s) from production builds (259200 s = 72 h).

### 3.4. PolicyPermissions

The allowlist of message type URLs a policy may execute.

```protobuf
message PolicyPermissions {
  string policy_address = 1;
  repeated string allowed_messages = 2;  // type URLs e.g. "/cosmos.bank.v1beta1.MsgSend"
}
```

Permissions are *additive on create*, *replaceable on update*, and
*deletable*. Every change increments the policy version (see §3.7).

### 3.5. Proposal, Vote, AnonVoteTally

```protobuf
message Proposal {
  uint64 id = 1;
  string council_name = 2;
  string policy_address = 3;             // standard or veto policy this proposal targets
  string proposer = 4;
  repeated google.protobuf.Any messages = 5;
  ProposalStatus status = 6;
  int64 submit_time = 7;
  int64 voting_deadline = 8;             // submit_time + DecisionPolicy.voting_period
  uint64 policy_version = 9;             // snapshot at submit; mismatch on execute = VETOED
  string metadata = 10;
  int64 execution_time = 11;             // earliest execute; set when ACCEPTED
  string failed_reason = 12;             // populated on FAILED/VETOED/REJECTED
}

message Vote {
  string voter = 1;
  VoteOption option = 2;                 // YES | NO | ABSTAIN | NO_WITH_VETO
  string metadata = 3;
  int64 submit_time = 4;
}

message AnonVoteTally {
  uint64 yes_count = 1;
  uint64 no_count = 2;
  uint64 abstain_count = 3;
  uint64 no_with_veto_count = 4;
}
```

Status enum:

| Status | Meaning |
|---|---|
| `SUBMITTED` | Open for voting |
| `ACCEPTED` | Threshold met; awaiting execution after `execution_time` |
| `REJECTED` | Voting deadline passed without meeting threshold |
| `EXECUTED` | All inner messages ran successfully |
| `FAILED` | Inner-message handler returned an error during execute (state reverts) |
| `VETOED` | Policy version was bumped between accept and execute |
| `EXPIRED` | Reserved (proposals stay `SUBMITTED` until EndBlocker finalises) |

### 3.6. Category

Governance-curated content categories shared across modules (blog, forum,
collect):

```protobuf
message Category {
  uint64 category_id = 1;
  string title = 2;
  string description = 3;
  bool members_only_write = 4;
  bool admin_only_write = 5;
}
```

Created via `MsgCreateCategory` by gov, the Commons Council, or the Commons
Operations Committee. Per-module restrictions (e.g. "blog category 1 is
read-only") layer on top.

### 3.7. RecurringSpend

A council-approved disbursement schedule. The record is created when a
proposal carrying `MsgScheduleRecurringSpend` executes; each elapsed
period the recipient submits `MsgClaimRecurringSpend` to pull one
`amount_per_period` from the council's policy account.

```protobuf
message RecurringSpend {
  uint64 id = 1;
  string authority = 2;                    // council policy address
  string recipient = 3;
  repeated cosmos.base.v1beta1.Coin amount_per_period = 4;
  int64 period_seconds = 5;                // >= Params.min_recurring_period_seconds
  int64 start_time = 6;                    // unix seconds
  int64 end_time = 7;                      // <= start_time + Params.max_recurring_duration_seconds
  int64 last_claim_advance = 8;            // schedule's logical clock
  uint64 claims_made = 9;
  RecurringSpendStatus status = 10;        // ACTIVE | RECIPIENT_DECLINED | CANCELED | COMPLETED
  uint64 created_via_proposal_id = 11;
  string note = 12;                        // capped at 256 chars
}
```

Lifecycle:

| Transition | Trigger |
|---|---|
| `→ ACTIVE` | `MsgScheduleRecurringSpend` executes (wrapped in a council proposal) |
| `ACTIVE → CANCELED` | `MsgCancelRecurringSpend` executes (same authority, another council proposal) |
| `ACTIVE → RECIPIENT_DECLINED` | `MsgDeclineRecurringSpend` (signer = recipient, direct tx) |
| `ACTIVE → COMPLETED` | `MsgClaimRecurringSpend` advances `last_claim_advance` past `end_time - period_seconds` (final claim was disbursed) |

Notable design choices:

- **No `PAUSED` state**. Term-pause is *derived* at claim time by routing
  through `CheckSpendPreconditions`. If the council's term has expired
  the claim rejects with `ErrGroupExpired`; on renewal the same code path
  passes again. Storing a paused flag would risk drifting out of sync
  with the group's actual expiration.
- **Logical clock**: `last_claim_advance` advances by exactly
  `period_seconds` per claim, anchored to the schedule, not block time. A
  recipient who skips can catch up by submitting multiple claim txs,
  each rate-limited independently against `MaxSpendPerEpoch`.
- **First claim opens at `start_time + period_seconds`** — the council
  vote authorises payment after one period of work, not immediately.
- **Created-via-proposal-id** is currently informational (the value is
  reserved on the proto for traceability but not populated by the
  handler; future work).

### 3.8. Indexes and counters

| Collection | Key → Value | Purpose |
|---|---|---|
| `PolicyToName` | `policy_address → council_name` | Reverse lookup; used by every msg-server that authenticates a council action |
| `MarketToGroup` | `market_id → council_name` | Confidence-vote market linkage; consumed by `AfterMarketResolved` |
| `MarketTriggerQueue` | `(trigger_block, council_name)` | Future market triggers (currently unused beyond bootstrap scaffolding) |
| `ProposalsByCouncil` | `(council_name, proposal_id)` | Pagination index for `ListProposals` |
| `VetoPolicies` | `council_name → veto_policy_address` | Cross-council veto resolution |
| `PolicyVersion` | `policy_address → uint64` | Veto-by-bump invalidation token |
| `EpochSpending` | `(policy_address, epoch_day) → string(uspark)` | Cumulative `MsgSpendFromCommons` **and** `MsgClaimRecurringSpend` total per UTC day |
| `RecurringSpends` | `id → RecurringSpend` | The schedule record |
| `RecurringSpendsByAuthority` | `(authority, id) → ∅` | Per-council schedule listing |
| `RecurringSpendsByRecipient` | `(recipient, id) → ∅` | Per-wallet "what can I claim?" listing |
| `ActiveRecurringSpendCount` | `authority → uint32` | O(1) cap check on `MsgScheduleRecurringSpend` (decremented on Cancel/Decline/Completed; recomputed at genesis import) |
| `ProposalSeq`, `CouncilSeq`, `CategorySeq`, `RecurringSpendSeq` | sequences | Auto-increment ID generators |

### 3.9. Params

```protobuf
message Params {
  string proposal_fee = 1;                            // e.g. "5000000uspark" — charged on every MsgSubmitProposal
  int64  min_recurring_period_seconds = 2;            // default 86_400  (1 day)
  int64  max_recurring_duration_seconds = 3;          // default 31_536_000 (365 days)
  uint32 max_active_recurring_spends_per_group = 4;   // default 50
}
```

A `MsgSubmitProposal` deducts `proposal_fee` from the proposer's balance and
sends it to the policy address being proposed against. (This is the fee that
genesis-bootstrapped councils fall short on — many tests pre-fund the
council policy from `alice` before submitting a register-group proposal.)

The recurring-spend params bound the attack surface a captured council
can carve out with `MsgScheduleRecurringSpend`. `Validate()` cross-checks
that `max_recurring_duration_seconds >= min_recurring_period_seconds` so
the configured window can always fit at least one period.

---

## 4. Genesis Bootstrap

`Keeper.BootstrapGovernance` runs during `InitGenesis` and materialises the
Three Pillars graph from the build-tag-selected `genesis_vals_*.go` constants
(`mainnet`, `testnet`, `devnet`, `testparams`). The bootstrap is idempotent
— re-running on a non-empty store is a no-op for already-created councils.

The chain ships **ten** governance bodies (`Group` records) post-bootstrap:

1. **Commons Council** (parent: gov) — top-level coordination, anonymous
   proposal target.
2. **Technical Council** (parent: gov) — implementation/upgrades.
3. **Ecosystem Council** (parent: gov) — ecosystem programs.
4. **Commons Supervisory Board** (parent: gov) — meta-governance over
   commons committees.
5. **Technical Operations Committee** (parent: Technical Council).
6. **Technical Governance Committee** (parent: Technical Council).
7. **Ecosystem Operations Committee** (parent: Ecosystem Council).
8. **Ecosystem Governance Committee** (parent: Ecosystem Council).
9. **Commons Operations Committee** (parent: Commons Council).
10. **Commons Governance Committee** (parent: Commons Council).

Each top-level council additionally has a **veto policy address** — a
second policy on the same `Group` record, with its own `DecisionPolicy`
and `PolicyPermissions`. The veto policies are NOT separate `Group`
records, so they don't count toward the body total above; they're the
golden-share emergency channel attached to Commons / Technical /
Ecosystem councils.

For each `Group`, bootstrap runs a 10-step sequence in `createGroup`:

1. Allocate a `council_id` from `CouncilSeq`.
2. Derive a deterministic `policy_address` from `council_id` (so the address
   is reproducible across re-init for the same chain config).
3. Insert each member into `Members[(council_name, address)]`.
4. Insert the `DecisionPolicy` for the standard policy.
5. Initialise `PolicyVersion[policy_address] = 0`. (`MsgUpdatePolicyPermissions` and `MsgVetoGroupProposals` increment from there; submit-time snapshots and execute-time re-checks compare versions, so the initial value just has to be a reproducible starting point.)
6. Register `PolicyPermissions[policy_address]` from the bootstrap's
   `AllowedMessages`.
7. If `VetoMinExecution > 0`, derive a second `veto_policy_address` and
   register a separate `DecisionPolicy` + `PolicyPermissions` for it,
   stored in `VetoPolicies[council_name]`.
8. Call `splitKeeper.SetShareByAddress(policy_address, funding_weight)` if
   the split keeper is wired.
9. Store the `Group` keyed by name.
10. Insert the reverse index `PolicyToName[policy_address] = council_name`.

Per-environment value selection (testparams snippet):

| Constant | testparams | mainnet |
|---|---|---|
| `CommonsCouncilStandardMinExecution` | `1 * time.Second` | `72 * time.Hour` |
| `TechCouncilStandardMinExecution` | `1 * time.Second` | `72 * time.Hour` |
| `WindowCouncil` (voting period) | `120 * time.Hour` | `120 * time.Hour` |
| `TermDuration5Months` | `12_960_000` (constant) | `12_960_000` |
| `TermDuration1Year` | `31_536_000` | `31_536_000` |

The voting windows and term durations are fixed across all builds; only the
*minimum execution period* differs, so the `verify_test_params` helper uses
that single field as the canary signal.

After the council graph, bootstrap optionally:

- Seeds `OwnerInfo.display_name` for each genesis member via
  `nameKeeper.SetDisplayName`.
- Reserves canonical handles (e.g. `alice` → alice's address) via
  `nameKeeper.ClaimName` so squatters cannot snipe a founder's identity in
  the open registration window.

Both `name`-keeper hooks are best-effort: a missing `nameKeeper` (e.g. in
unit tests that don't wire it) is logged and skipped.

---

## 5. Authority Model

`x/commons` answers "who can do this?" via two helper functions.

### 5.1. `IsCouncilAuthorized(addr, council, committee)`

Returns true if `addr` is any of:

- The gov module account (`k.authority`).
- The named council's standard policy address.
- If `committee` is non-empty: the policy address of `<council>'s
  <committee>` (resolved via `resolveCommitteeName`).
- A direct member of the named committee (`IsCommitteeMember`).

This is the broad "authorized" check: passes for both committee members
*and* council policy addresses. Use when either is acceptable (e.g.
"Operations Committee or higher can update operational params").

Cross-council veto authorization is handled separately by `IsSiblingPolicy`
(used by `MsgVetoGroupProposals`); it is not part of `IsCouncilAuthorized`.

### 5.2. `IsCouncilPolicyOrGov(addr, council)`

Tighter: returns true *only* for gov or the council's standard/veto policy
address — never an individual member. Used where a passed council vote is
required, such as approving / rejecting an x/reveal contribution.

The auth-tightening migration in `9e0a679` switched several handlers from
`IsCouncilAuthorized` to `IsCouncilPolicyOrGov` so a single Operations
Committee member can no longer unilaterally approve actions that should
require a full council vote.

### 5.3. Cycle detection

`MsgRegisterGroup` and `MsgUpdateGroupConfig` both call
`Keeper.DetectCycle(child, parent)` before mutating state. The check walks
up the parent chain from the proposed parent, refusing the operation if it
encounters the child. This prevents the user from constructing a parent →
child → parent loop that would make the trust hierarchy unenforceable.

### 5.4. Signer cross-check on inner messages

`Keeper.validateMsgAuthority(sdkMsg, policyAddress)` runs on every inner
message during `ExecuteProposal`:

```
signers, _, err := k.cdc.GetMsgV1Signers(sdkMsg)
for each signer s:
    if s != policyAddress: REJECT
```

This closes the privilege-escalation gap where a malicious proposer might
embed a message claiming `authority: gov` inside a council proposal.
Whatever the signer field is named on the inner message
(`authority`, `voter`, `proposer`, `creator`, `staker`, `juror`, ...), the
check works uniformly because it pulls signers from the proto's
`cosmos.msg.v1.signer` option.

---

## 6. Native Proposal Lifecycle

```
SubmitProposal ───▶ SUBMITTED
       │
       ├── (early acceptance: threshold met during voting)
       │       │
       │       └──▶ ACCEPTED  (sets execution_time = block_time + min_execution_period)
       │
       ├── (voting deadline reached, EndBlocker)
       │       │
       │       ├── threshold met ──▶ ACCEPTED
       │       └── threshold not met ──▶ REJECTED
       │
       └── ExecuteProposal (anyone, after execution_time)
               │
               ├── policy_version mismatch ──▶ VETOED (no execution)
               ├── inner-msg handler ok      ──▶ EXECUTED
               └── inner-msg handler error   ──▶ FAILED (no state changes persist)
```

### 6.1. Submit

`MsgSubmitProposal{proposer, policy_address, messages, metadata}`:

1. Authenticate proposer is a member of the council that owns
   `policy_address` (lookup via `PolicyToName`).
2. Verify the council has not expired (`now <= current_term_expiration`).
3. The proposal fee is enforced by the `GroupPolicyMinFeeDecorator` ante decorator (`x/commons/ante/group_policy.go`). For every `MsgSubmitProposal` (and `MsgVoteProposal`), the decorator requires the tx's `--fees` to meet `Params.proposal_fee`; the fee is paid via the standard Cosmos SDK fee path (to validators), not transferred to `policy_address`. Emergency actions (`MsgEmergencyCancelGovProposal`, `MsgVetoGroupProposals`) and shielded variants (`MsgSubmitAnonymousProposal`, `MsgAnonymousVoteProposal`) are exempt from the decorator — shield handles its own funding via the privacy module's community-pool subsidy.
4. For each inner message, check its type URL is in
   `PolicyPermissions[policy_address].allowed_messages`.
5. Snapshot `policy_version` at submit time — used later to detect a veto
   that bumped the version mid-flight.
6. Set `voting_deadline = now + DecisionPolicy.voting_period`.
7. Store `Proposal` (status `SUBMITTED`); index in `ProposalsByCouncil`.

### 6.2. Vote

`MsgVoteProposal{voter, proposal_id, option, metadata}`:

1. Proposal must be in `SUBMITTED` status.
2. Voter must be a current member of the council (re-checked on every vote
   so a removed member's pending vote stops counting).
3. Store `Vote[(proposal_id, voter)]`.
4. Run `checkThreshold` — if it now passes, flip status to `ACCEPTED` and
   set `execution_time = block_time + min_execution_period`. This is the
   "early acceptance" path: a quorum is enough; you don't have to wait out
   the full voting window.

### 6.3. Threshold check (`checkThreshold`)

Two policy types, both folding in anonymous votes:

```
yesWeight    = sum(member_weight for each YES vote) + anon.yes_count
totalWeight  = sum(member_weight for each vote)     + (anon.yes + anon.no + anon.abstain + anon.veto)

if policy_type == "percentage":
    groupWeight = sum(member_weight for every council member)
                + (anon.yes + anon.no + anon.abstain + anon.veto)
    accepted    = yesWeight / groupWeight >= threshold

if policy_type == "threshold":
    accepted = yesWeight >= threshold   // threshold parsed as integer
```

Anonymous votes (`AnonVoteTally`) get uniform weight=1 each. They expand
both numerator (their YES count) and denominator (their total participation)
without giving any individual anon voter outsized influence over a council
member's weighted vote.

### 6.4. Execute

`MsgExecuteProposal{executor, proposal_id}`:

1. Status must be `ACCEPTED`.
2. `now >= execution_time` (else `ErrInvalidRequest "min execution period not elapsed"`).
3. Re-check `policy_version`: if mismatched (i.e. someone vetoed by bumping
   the version), flip to `VETOED` and return `ErrUnauthorized`.
4. For councils whose term has now expired, allow only `MsgRenewGroup`
   inside the proposal — every other inner message is rejected with
   `"TERM EXPIRED"`. This is the unique escape hatch that lets an expired
   council reactivate itself by replacing its members.
5. For each inner message, in order:
   - Unpack the `Any`.
   - `validateMsgAuthority(msg, policy_address)`.
   - Look up the handler in the msg router; reject if missing.
   - Invoke the handler. On error, set status `FAILED` with the reason and
     return — the entire tx reverts, including the `FAILED` write itself
     (so the proposal stays `ACCEPTED`; this is intentional, the
     transaction failure is the canonical record).
6. On success, set status `EXECUTED` and emit a finalisation event.

`executor` need not be a council member — anyone can pay gas to execute an
already-accepted proposal.

### 6.5. EndBlocker finalisation

`EndBlockProposals` walks the proposal store once per block, looking for
`SUBMITTED` proposals past their `voting_deadline`. For each:

- Tally votes.
- If threshold met → `ACCEPTED` (status flip only; `execution_time` was
  set at submit time as `now + voting_period + min_execution_period` and
  is **not** recomputed here). The early-acceptance path inside
  `MsgVoteProposal` overrides `execution_time` to `block_time +
  min_execution_period` because in that path we'd otherwise wait
  out the rest of the voting window for no reason.
- Else → `REJECTED` with reason `"threshold not met at voting deadline"`.

Proposals in terminal states (`EXECUTED`, `REJECTED`, `EXPIRED`, `FAILED`,
`VETOED`) are skipped during the walk to keep cost bounded as historical
proposals accumulate.

---

## 7. Messages

| Msg | Authority | Purpose |
|---|---|---|
| `MsgUpdateParams` | gov | Replace `Params.proposal_fee` |
| `MsgSpendFromCommons` | a registered group policy | Send funds from the council policy to a recipient, subject to `MaxSpendPerEpoch` cumulative cap |
| `MsgEmergencyCancelGovProposal` | A signer whose `PolicyPermissions` contain `MsgEmergencyCancelGovProposal` (genesis grants this only on the **veto policies** of Commons/Technical/Ecosystem councils — i.e. the founder's golden-share channel) | Cancel a pending `x/gov` proposal — the council emergency-stop on rogue gov votes |
| `MsgCreatePolicyPermissions` | gov OR the policy address itself (self-registration) | Create the allowlist for a new policy address |
| `MsgUpdatePolicyPermissions` | gov OR the policy address itself | Replace the allowlist (bumps `PolicyVersion`); ratchet rules in `ValidatePermissions` block adding `Forbidden` messages |
| `MsgDeletePolicyPermissions` | gov OR the policy address itself | Remove the allowlist record |
| `MsgRegisterGroup` | gov OR Commons Council | Create a new sub-committee under a parent policy; fails on cycle, missing parent, or invalid member counts |
| `MsgRenewGroup` | parent policy OR a registered electoral policy | Replace members and reset `current_term_expiration` to `now + term_duration`. Permitted on expired councils (this is the escape hatch the executor honours) |
| `MsgUpdateGroupMembers` | parent policy OR the group's electoral policy | Add and/or remove members without resetting the term. The `update_cooldown` is enforced ONLY when the signer is the electoral policy; direct parent or gov calls bypass the cooldown |
| `MsgUpdateGroupConfig` | parent policy | Mutate group-config knobs (`max_spend_per_epoch`, `vote_threshold`, `voting_period`, `min_execution_period`, `futarchy_enabled`, `min_members`, `max_members`, `term_duration`, `policy_type`, `electoral_policy_address`). Cycle-checked on parent change. Bumps `PolicyVersion`. Throttled by `update_cooldown` |
| `MsgDeleteGroup` | parent policy OR gov | Remove a group from the registry. Members, decision policy, permissions, and policy-version are cleared |
| `MsgVetoGroupProposals` | parent policy OR a sibling veto policy OR gov | Bump `PolicyVersion` for the target group's policy — every still-`ACCEPTED` proposal will fail to execute with `VETOED` |
| `MsgForceUpgrade` | Technical Council policy (via proposal) | Schedule a chain upgrade through `x/upgrade` |
| `MsgSubmitProposal` | a council member | Open a new proposal (subject to allowlist + fee + term expiration) |
| `MsgVoteProposal` | a current council member | Cast `YES`/`NO`/`ABSTAIN`/`NO_WITH_VETO`; triggers early acceptance if threshold met |
| `MsgExecuteProposal` | anyone | Run an accepted proposal's inner messages after `execution_time` |
| `MsgSubmitAnonymousProposal` | x/shield module account only | Same as `MsgSubmitProposal` but skips the membership check (ZK proof attests registration); identical fee, allowlist, and term gates |
| `MsgAnonymousVoteProposal` | x/shield module account only | Increments `AnonVoteTally` for the proposal; weight=1, tallied separately from member votes |
| `MsgCreateCategory` | gov OR Commons Council OR Commons Operations Committee | Register a new shared content category |
| `MsgScheduleRecurringSpend` | a registered group policy | Create a recurring-disbursement schedule from the council's treasury. Wrapped in a `MsgSubmitProposal`; counts against `Params.max_active_recurring_spends_per_group` |
| `MsgCancelRecurringSpend` | the schedule's authority (same group policy) | Terminate an active schedule. Wrapped in a `MsgSubmitProposal`; rejects with `ErrRecurringSpendUnauthorized` if the caller is a different council |
| `MsgClaimRecurringSpend` | the schedule's recipient | Pull one period of an active schedule. Routes through `CheckSpendPreconditions` so the per-epoch rate-limit, term expiration, and activation gate all apply identically to one-off spends |
| `MsgDeclineRecurringSpend` | the schedule's recipient | Permanently opt out of future claims (graceful exit when leaving a role). No proposal required |

The amino type names follow the pattern `sparkdream/x/commons/Msg<Name>`,
declared on every signer message via `option (amino.name)` so Keplr+Ledger
amino-JSON signing works (see `docs/HANDOFF_LEDGER_AMINO_NAMES.md`).

#### 7.1. Recurring-spend flow

```
Council                   Recipient                  Chain
   │                          │                          │
   │ MsgSubmitProposal(       │                          │
   │   MsgScheduleRecurringSpend)                        │
   │─────────────────────────────────────────────────────▶│
   │                          │                  Validates period/window/cap
   │ … votes, executes …      │                          │
   │                          │                  Allocates id, status=ACTIVE
   │                          │                  emits recurring_spend_scheduled
   │                          │                          │
   │                          │ MsgClaimRecurringSpend(id)│
   │                          │─────────────────────────▶│
   │                          │                  CheckSpendPreconditions
   │                          │                  bankKeeper.SendCoins
   │                          │                  last_claim_advance += period
   │                          │                  emits recurring_spend_claimed
   │                          │                          │
   │ MsgSubmitProposal(       │                          │
   │   MsgCancelRecurringSpend)                          │
   │─────────────────────────────────────────────────────▶│
   │ … votes, executes …      │                  status=CANCELED
   │                          │                  emits recurring_spend_canceled
```

Failure modes worth calling out:

- **Period not elapsed** (`ErrRecurringSpendNotDue`) — the recipient is
  claiming faster than `period_seconds`. Each claim advances by exactly
  one period from `last_claim_advance`, anchored to the schedule.
- **Council term expired** (`ErrGroupExpired`) — the auto-pause
  semantic. The schedule remains `ACTIVE`; the next claim after the
  parent calls `MsgRenewGroup` succeeds.
- **Per-epoch rate limit** (`ErrRateLimitExceeded`) — catch-up claims
  hit the same `MaxSpendPerEpoch` ceiling as one-off `SpendFromCommons`.
- **Window closed without a final claim** — when the recipient never
  claims and the schedule's logical clock can no longer advance another
  period before `end_time`, the next claim attempt (or none, until
  someone reads it) flips status to `COMPLETED` and rejects.

---

## 8. Queries

| RPC | CLI | Purpose |
|---|---|---|
| `Params` | `q commons params` | Returns `Params{proposal_fee}` |
| `GetPolicyPermissions` | `q commons get-policy-permissions [policy-address]` | Allowed-messages list for a single policy |
| `ListPolicyPermissions` | `q commons list-policy-permissions` | Paginated allowlist for every policy |
| `GetDecisionPolicy` | `q commons get-decision-policy [policy-address]` | `policy_type`, `threshold`, `voting_period`, `min_execution_period` for a single policy |
| `ListDecisionPolicies` | `q commons list-decision-policies` | Paginated `DecisionPolicyEntry{policy_address, decision_policy}` for every policy. Useful for end-to-end audits and UIs that render every council's voting rules |
| `GetGroup` | `q commons get-group [name]` | The full `Group` record |
| `ListGroups` | `q commons list-group` | Paginated `Group` list |
| `GetCouncilMembers` | `q commons get-council-members [name]` | Members for a council |
| `GetProposal` | `q commons get-proposal [id]` | `Proposal` + its `votes` + `tally` (computed) |
| `ListProposals` | `q commons list-proposals` | Paginated proposals, filterable by council |
| `GetProposalVotes` | `q commons get-proposal-votes [id]` | All votes cast on a proposal |
| `GetCategory` | `q commons get-category [id]` | Single shared content category |
| `ListCategory` | `q commons list-category` | Paginated categories |
| `GetRecurringSpend` | `q commons get-recurring-spend [id]` | Single recurring spend schedule |
| `ListRecurringSpends` | `q commons list-recurring-spends` | Paginated schedules; mutually-exclusive `--authority` or `--recipient` filters use the matching secondary index |

The `Get`/`List` distinction is mechanical: `Get` returns one record by key,
`List` paginates the whole map. `NotFound` is a clean gRPC code, so HTTP and
gRPC clients can distinguish "no record" from "empty record".

`GetDecisionPolicy` and `ListDecisionPolicies` complete the council
configuration trifecta: every council has a `Group` (identity),
`PolicyPermissions` (capabilities), and a `DecisionPolicy` (voting rules) —
all three are now public read-only queries.

---

## 9. EndBlocker

A single phase per block:

1. **Proposal finalisation** (`EndBlockProposals`) — walk all proposals,
   finalise any whose `voting_deadline < now` and that are still in
   `SUBMITTED` status. Sets `ACCEPTED` (with `execution_time`) or
   `REJECTED`. Emits a `proposal_finalized` event.

The walk skips proposals already in terminal states (`EXECUTED`,
`REJECTED`, `EXPIRED`, `FAILED`, `VETOED`) to keep cost bounded as
historical proposals accumulate.

The module registers a BeginBlocker, but it is a no-op (`return nil`) —
the previous market-trigger queue logic was removed when futarchy switched
to keeper-initiated programmatic creation. The `module.go` registration is
kept so the appmodule interface is satisfied, but it does no work.

---

## 10. Hooks

`x/commons` implements `futarchytypes.FutarchyHooks`. The single hook is
`AfterMarketResolved(ctx, market_id, winner)`:

1. Look up the council linked to `market_id` via `MarketToGroup`. If
   unlinked, return silently — it's a vanilla market, not a confidence vote.
2. Apply elastic tenure to the linked `Group`:
   - **`yes` (confidence)**: `current_term_expiration += term_duration / 5`
     (+20%). Cap: max 2 full terms past `now`.
   - **`no` (no confidence)**: `current_term_expiration -= term_duration / 2`
     (-50%). Floor: never expire in the past — clamp to `now` so a
     re-election window opens immediately rather than retroactively
     invalidating prior actions.
   - **`invalid` (no quorum)**: emit a marker event, no term change.
3. Remove the `MarketToGroup` link (one-shot per market).
4. Persist the updated `Group`.

This is the only hook x/commons publishes. Reverse direction (x/futarchy
calling commons): `Keeper.CreateMarketInternal` is invoked by commons to
schedule a confidence vote on demand, but that's a normal call, not a hook.

---

## 11. Anonymous Governance via x/shield

x/shield routes shielded transactions through `MsgShieldedExec`. Modules
register a `ShieldAware` interface to advertise which message types they
accept anonymously. `x/commons` registers two:

```go
func (k Keeper) IsShieldCompatible(_ context.Context, msg sdk.Msg) bool {
    switch msg.(type) {
    case *types.MsgSubmitAnonymousProposal:  return true
    case *types.MsgAnonymousVoteProposal:    return true
    default:                                  return false
    }
}
```

When a shielded execution unwraps either of these, the `proposer` (or
`voter`) field is set to the **x/shield module account address** before
dispatch. The commons handler enforces this:

```
shieldModuleAddr := k.authKeeper.GetModuleAddress("shield")
if msg.Proposer != shieldModuleAddr.String():
    return ErrUnauthorized
```

Anyone calling these messages directly (without going through shield's
`MsgShieldedExec`) gets rejected because they cannot impersonate the shield
module account.

What the shield ZK proof guarantees:

- The submitter is a registered, eligible voter (from the shield-managed
  voter set). Identity is *not* leaked.
- Per-domain nullifiers prevent double-voting on the same proposal.

What the commons handler still enforces:

- The target policy address exists.
- The council has not expired.
- Every inner message is on the policy's `allowed_messages` allowlist.
- The proposal fee is paid (charged from the shield module account, which
  the shield module pre-funds via its community-pool integration).

Anonymous votes accumulate into `AnonVoteTally` (uniform weight=1 per vote)
and are folded into `checkThreshold` alongside member votes — the threshold
check sees the combined tally so anonymous voters genuinely influence
outcomes without leaking individual identities.

---

## 12. Errors

| Code | Name | Meaning |
|---|---|---|
| 1100 | `ErrInvalidSigner` | Expected gov account as the sole signer for a proposal-bound message |
| 1600 | `ErrGroupNotFound` | Council/committee name not in the registry |
| 1601 | `ErrInvalidGroupSize` | Member count outside `[min_members, max_members]` |
| 1602 | `ErrRateLimitExceeded` | `MsgSpendFromCommons` or `MsgClaimRecurringSpend` exceeds `MaxSpendPerEpoch` cumulatively, or `MsgUpdateGroupConfig` violates `update_cooldown` |
| 1603 | `ErrGroupNotActive` | Group is in pre-launch (shell) phase; `activation_time > now` |
| 1604 | `ErrGroupExpired` | `current_term_expiration < now` for non-renewal operations (also fires for `MsgClaimRecurringSpend` — the auto-pause path) |
| 1700 | `ErrRecurringSpendNotFound` | No schedule with the given id |
| 1701 | `ErrRecurringSpendInactive` | Schedule is not `ACTIVE` (cancelled/declined/completed/zero) — covers claim/cancel/decline after a terminal flip |
| 1702 | `ErrRecurringSpendNotDue` | `now < last_claim_advance + period_seconds` |
| 1703 | `ErrRecurringSpendWindowClosed` | `last_claim_advance + period_seconds > end_time` — last claim window passed |
| 1704 | `ErrRecurringSpendInvalidPeriod` | `period_seconds < Params.min_recurring_period_seconds` |
| 1705 | `ErrRecurringSpendInvalidWindow` | `start_time`/`end_time` malformed (past, out-of-order, exceeds duration cap, shorter than one period) |
| 1706 | `ErrRecurringSpendCapReached` | Authority already has `Params.max_active_recurring_spends_per_group` active schedules |
| 1707 | `ErrRecurringSpendUnauthorized` | Caller is not the schedule's authority (cancel) or recipient (claim/decline) |

Other errors raised inline with `cosmossdk.io/errors`'s sentinel set:

- `ErrUnauthorized` — authority/signer mismatch, anonymous-message non-shield
  caller, allowlist violation, term-expired with non-`MsgRenewGroup` content,
  policy-version mismatch (veto), `validateMsgAuthority` failure.
- `ErrNotFound` — proposal/group/category lookup miss.
- `ErrInvalidRequest` — `min_execution_period` not yet elapsed, malformed
  amount, missing required field.
- `ErrInvalidAddress` — `addressCodec.StringToBytes` rejection.
- `ErrLogic` — internal invariants (msg router not wired, codec failures).

---

## 13. Events

`x/commons` emits the following events (non-exhaustive sample — every
msg-server also emits a generic `message` event, and not every event
attribute is enumerated here; consult the keeper for the source of truth):

| Event | Attributes | Emitter |
|---|---|---|
| `submit_proposal` | `proposal_id`, `policy_address`, `proposer` | `MsgSubmitProposal` |
| `vote_proposal` | `proposal_id`, `voter`, `option` | `MsgVoteProposal` |
| `execute_proposal` | `proposal_id`, `executor`, `status` | `MsgExecuteProposal` |
| `submit_anonymous_proposal` | `proposal_id`, `policy_address` | `MsgSubmitAnonymousProposal` |
| `anonymous_vote_proposal` | `proposal_id`, `option` | `MsgAnonymousVoteProposal` |
| `proposal_finalized` | `proposal_id`, `status` | EndBlocker |
| `group_proposals_vetoed` | `group_name`, `executor`, `child_policy` | `MsgVetoGroupProposals` |
| `gov_proposal_emergency_cancelled` | `proposal_id`, `authority` | `MsgEmergencyCancelGovProposal` |
| `category_created` | `category_id`, `name` | `MsgCreateCategory` |
| `elastic_tenure` | `group`, `action` (`extended` \| `shortened`), `seconds` | `AfterMarketResolved` |
| `market_invalid_no_quorum` | `group`, `action` (`no quorum`) | `AfterMarketResolved` |
| `recurring_spend_scheduled` | `id`, `authority`, `recipient`, `period_seconds`, `start_time`, `end_time` | `MsgScheduleRecurringSpend` |
| `recurring_spend_claimed` | `id`, `authority`, `recipient`, `amount`, `claim_number`, `last_claim_advance` | `MsgClaimRecurringSpend` |
| `recurring_spend_canceled` | `id`, `authority`, `recipient` | `MsgCancelRecurringSpend` |
| `recurring_spend_declined` | `id`, `authority`, `recipient` | `MsgDeclineRecurringSpend` |

---

## 14. Genesis State

Exported by `ExportGenesis`, consumed by `InitGenesis`:

```protobuf
message GenesisState {
  Params params = 1;
  repeated PolicyPermissions policy_permissions_map = 2;
  repeated Group group_map = 3;
  repeated CouncilMembers council_members = 4;          // grouped by council_name
  repeated PolicyWithAddress decision_policies = 5;     // (policy_address, DecisionPolicy)
  repeated Proposal proposals = 6;
  uint64 next_proposal_id = 7;
  uint64 next_council_id = 8;
  repeated PolicyVersionEntry policy_versions = 9;      // (policy_address, version)
  repeated ProposalVotes proposal_votes = 10;           // grouped by proposal_id
  repeated Category category_map = 11;
  uint64 next_category_id = 12;
  repeated RecurringSpend recurring_spends = 13;
  uint64 next_recurring_spend_id = 14;
}
```

`InitGenesis` runs in three stages:

1. **Restore** — every state object above is written back into its
   collection. Sequences are reset to `next_*_id`. For
   `recurring_spends`, both secondary indexes
   (`RecurringSpendsByAuthority`, `RecurringSpendsByRecipient`) are
   re-populated, and `ActiveRecurringSpendCount` is **recomputed** from
   the imported status fields rather than trusted from the export — this
   keeps the cap-check counter self-consistent across upgrades.
2. **Bootstrap** — `BootstrapGovernance` runs only if the council registry
   is empty (i.e. fresh chain start), wiring the Three Pillars graph from
   the build-tag-selected constants. On an exported-then-imported chain
   the registry is non-empty and bootstrap no-ops.
3. **Optional name seeding** — if the `name` keeper is wired, founding
   members get display names and canonical handles.

---

## 15. Security Considerations

- **Single proposal pipeline**: all proposal/voting/policy state lives in
  `x/commons`, with no parallel proposal module routing transactions.
  Submit/Vote/Execute always go through this module's keeper and ante
  decorator, so authority and permission checks can't be sidestepped via a
  separate path.
- **Per-message signer cross-check**: `validateMsgAuthority` runs on every
  inner message in `ExecuteProposal`. A malicious proposer cannot embed a
  `MsgUpdateParams{authority: gov}` inside a Commons Council proposal to
  impersonate gov.
- **Cumulative spending limit**: `EpochSpending[(policy_address, epoch_day)]`
  accumulates within a UTC day. A council that spent up to its limit
  yesterday cannot resume spending today via dust-sized proposals — every
  `MsgSpendFromCommons` re-reads the cumulative total.
- **Policy-version-bump veto**: avoids a quadratic scan of
  `ProposalsByCouncil` to invalidate all pending proposals. The next
  `ExecuteProposal` whose snapshotted `policy_version` doesn't match the
  current value flips to `VETOED` automatically.
- **Term-expired escape hatch**: an expired council can still execute a
  proposal whose only inner message is `MsgRenewGroup`. Any other content
  in the same proposal makes the entire execute reject. This prevents an
  expired-but-still-accepted proposal from sneaking a payload through
  while the parent's renewal is pending.
- **Cycle detection at register-group / update-group-config**: parent
  pointers are validated against the existing graph before persisting.
  The chain cannot end up in a state where governance authority loops
  back on itself.
- **Anonymous-proposal funnel**: `MsgSubmit/AnonymousVoteProposal` are
  *only* accepted when the on-tx signer is the x/shield module account.
  This is the gateway condition — no individual user can directly call
  these messages, even if they own the shield-bound DREAM stake.
- **Proposal fee disincentive**: every `MsgSubmitProposal` (anonymous or
  not) charges `Params.proposal_fee` to the proposer (or the shield
  module account, in the anonymous case). This is the spam tax that
  forces serious consideration before opening a proposal.
- **Two-policy split limits blast radius**: a compromised standard policy
  cannot itself bump or reuse its veto policy. The veto policy is
  separately governed — its `DecisionPolicy` can require the founder's
  golden share and a different threshold.
- **Recurring spends cannot bypass per-epoch rate limit**:
  `MsgClaimRecurringSpend` shares the `CheckSpendPreconditions` helper
  with `MsgSpendFromCommons`, so every claim increments the same
  `EpochSpending[(policy_address, epoch_day)]` total. A 100-year
  schedule cannot drain treasury faster than `MaxSpendPerEpoch` per
  UTC day, and catch-up claims (multi-period in one block) self-throttle
  against the same ceiling.
- **Recurring spends auto-pause on term expiration**: a council whose
  term has expired can no longer fund claims (`ErrGroupExpired`). The
  schedule remains `ACTIVE` in storage; renewal via `MsgRenewGroup`
  re-opens the gate. This matches the spend-power semantics of the
  expired-zombie state — recurring authority is not a way to spend past
  a confidence-vote slashing.
- **Recurring spend duration cap**: `Params.max_recurring_duration_seconds`
  defaults to 1 year, aligning with the typical council term. A captured
  council cannot plant 99-year commitments; the next council either
  inherits the schedule (and can cancel it via proposal) or, more
  commonly, has to re-approve a fresh schedule.
- **Per-authority schedule cap**: `Params.max_active_recurring_spends_per_group`
  bounds state-bloat from a fan-out of dust schedules. Cap is enforced
  by an O(1) counter (`ActiveRecurringSpendCount`) that decrements on
  Cancel / Decline / Completed.
- **Recipient escape hatch**: `MsgDeclineRecurringSpend` lets a
  recipient permanently opt out without involving the council — useful
  when someone leaves a role. The council can re-schedule (under a new
  id) to designate a successor.
- **Cancel requires same-council proposal**: `MsgCancelRecurringSpend`
  rejects with `ErrRecurringSpendUnauthorized` if the signer is not the
  schedule's authority. This means a captured *peer* council cannot
  unilaterally kill another council's commitments — only the same body
  that voted for the schedule (or its parent via group-config) can.

---

## 16. Testing

E2E coverage lives in `test/commons/`:

- `setup_test_accounts.sh` — provisions Alice/Bob/Carol/Dave keys and
  funds the Commons Council policy with proposal-fee headroom.
- `interim_council_test.sh` — installs a "dictator" via gov, then
  restores the original council, exercising emergency replacement.
- `group_lifecycle_test.sh` — creates a 60-second-term sub-committee,
  updates its config, waits past expiration, renews. Confirms the
  expired-council escape hatch (only `MsgRenewGroup` permitted).
- `group_member_update_test.sh` — adds/removes members; verifies
  `update_cooldown` throttling.
- `group_security_test.sh` — drives `MsgVetoGroupProposals` and the
  veto-policy version-bump path.
- `policy_lifecycle_security_test.sh`, `policy_permissions_test.sh` —
  permission allowlist invariants and the unauthorised-message rejection
  path.
- `unauthorized_handover.sh`, `unauthorized_spend_msg.sh` — drive
  privilege-escalation attacks against `validateMsgAuthority`.
- `executive_veto_test.sh`, `social_veto_vote_test.sh`,
  `parent_veto_test.sh`, `veto_vote_test.sh` — every veto pathway.
- `tech_upgrade_golden_share.sh` — Tech Council `MsgForceUpgrade` requires
  the founder's golden-share veto-policy approval; verifies the upgrade
  plan is staged via `x/upgrade`.
- `fire_council_test.sh` — gov-driven full council replacement.
- `anon_test.sh` — anonymous proposal/vote round-trip via shield.
- `treasury_spend.sh` — `MsgSpendFromCommons` with cumulative cap
  enforcement.
- `recurring_spend_test.sh` — full lifecycle: gov-lowers the period min,
  schedules via Commons Operations Committee proposal, waits one period,
  recipient claims, verifies cadence enforcement (claim-too-soon), exercises
  the `--authority`/`--recipient` query filters, cancels via follow-up
  proposal, and confirms post-cancel claim is rejected.
- `recurring_spend_validation_test.sh` — schedule-time validation
  matrix: period below `min_recurring_period_seconds`, end_time before
  start_time, duration over `max_recurring_duration_seconds`, window
  shorter than one period, note over the 256-char cap. Each failure is
  verified by submitting the malformed proposal and inspecting the
  execution failure.
- `recurring_spend_security_test.sh` — authority/recipient invariants:
  wrong-recipient `MsgClaimRecurringSpend` rejects with
  `ErrRecurringSpendUnauthorized`, a peer council's
  `MsgCancelRecurringSpend` against another council's schedule rejects,
  and `MsgDeclineRecurringSpend` from a non-recipient rejects. Also
  exercises the recipient-decline graceful-exit path.
- `category_test.sh` — `MsgCreateCategory` permission matrix.
- `fee_update_test.sh` — `MsgUpdateParams` round-trip.

The verifier `test/run_all_tests.sh::verify_test_params` reads the Commons
Council's `min_execution_period` via `q commons get-decision-policy` to
detect test-vs-production builds before any suite runs (see
`docs/testing-params-setup.md`).

Unit tests cover keeper logic at `x/commons/keeper/*_test.go`, including
`end_block_proposals_test.go` for both percentage and threshold policy
types and `msg_server_proposals_test.go` for the early-acceptance path.
Recurring-spend coverage lives in
`msg_server_recurring_spend_test.go` (validation matrix, happy path,
catch-up + rate-limit interaction, term-expiry auto-pause/auto-resume,
cancel/decline, cap enforcement, completion flip),
`spend_preconditions_test.go` (the shared gating helper in isolation),
`query_recurring_spend_test.go` (filter mutually-exclusive enforcement
and index-backed pagination), and `genesis_test.go` (round-trip of
schedules + recomputation of `ActiveRecurringSpendCount`).

Simulation operations are registered in
`x/commons/simulation/recurring_spend.go` for fuzz-style coverage in
sim test runs (`make test-sim-nondeterminism`).
