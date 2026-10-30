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
  `EpochSpending` bucket via the `SessionClaimHook` (PreCheck reads the
  same gates as `CheckSpendPreconditions`; PostCommit performs the bucket
  debit atomically with the bank send), so they cannot side-step the
  per-epoch ceiling.
- **Recurring spends (pull model, hosted in [x/session](x-session-spec.md))**:
  councils pre-approve a schedule via `MsgScheduleRecurringSpend` (wrapped
  in a proposal). Each elapsed period the **recipient** submits
  `MsgClaimRecurringSpend` to pull one disbursement. The four commons
  msg types (`Schedule` / `Claim` / `Cancel` / `Decline`) are thin
  wrappers over the unified [x/session](x-session-spec.md) grant
  registry — the schedule lives as a `RECURRING_PULL` grant whose
  granter is the council policy and whose grantee is the recipient.
  Term-expired councils auto-pause (claims reject with
  `ErrGroupExpired` via the `SessionClaimHook.PreCheck`); term
  renewal auto-resumes. Schedules can be cancelled via a follow-up
  council proposal (`MsgCancelRecurringSpend`) or declined
  unilaterally by the recipient (`MsgDeclineRecurringSpend`).
  Cancelled / declined schedules return `NotFound` from
  `QueryGetRecurringSpend` post-migration (the grant is deleted from
  session; audit trail is in the `grant_revoked` / `grant_declined`
  events).
- **Anonymous governance**: `MsgSubmitAnonymousProposal` is accepted only
  from the `x/shield` module account. The shield module's ZK proof attests to
  trust-tree membership; the commons handler skips the membership check but
  still enforces every other gate (term, allowed-messages).
- **Secret ballots**: a group's `ballot_mode` (OPEN by default, SECRET or
  PROPOSER_CHOICE) decides whether its proposals are voted openly or by
  secret ballot. A secret ballot snapshots the group's members' ZK keys into
  a voter tree when it opens; members vote with `MsgAnonymousVoteProposal`
  through x/shield, proven against that tree, once each; open votes are
  refused on it (§11.2).
- **Polls**: non-binding single-choice questions any member can put to their
  group for a chosen duration, voted openly or by secret ballot (§11.4).
- **Veto by policy version bump**: `MsgVetoGroupProposals` does not iterate
  proposals — it bumps the policy's `version`. Every accepted proposal
  records the version at submit time; on execute, a mismatch flips the
  proposal to `VETOED` instead of running.

---

## 2. Dependencies

| Module | Purpose |
|---|---|
| `x/auth` | Address codec, module account resolution, account iteration during genesis to seed founding-member lookups |
| `x/bank` | Council treasury sends (`SpendFromCommons`, `ClaimRecurringSpend`), proposal-fee transfers from the proposer. The commons module account has no `Minter` or `Burner` permission: commons never creates SPARK, and its market subsidies come from SPARK already sent to the account |
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
  int64 last_config_update = 17;          // unix seconds of the parent's last MsgUpdateGroupConfig
  BallotMode ballot_mode = 18;            // OPEN (0 = UNSPECIFIED reads as OPEN) | SECRET | PROPOSER_CHOICE
}
```

`ballot_mode` decides how the group's proposals are voted (§11.2): `OPEN`
(the default) with `MsgVoteProposal`; `SECRET`, every proposal a secret
ballot; `PROPOSER_CHOICE`, the proposer sets `secret_ballot` per proposal.
It is set by `MsgRegisterGroup` and changed by `MsgUpdateGroupConfig`
(parent or gov). Polls ignore it: their creator chooses (§11.4).

`current_term_expiration == 0` means "no expiry" everywhere: submit,
execute, anonymous submit, renewal and the spend gates share one helper
(`isTermExpired`). `max_spend_per_epoch` of 0 (or unset) means "no cap"; only
x/gov may create or configure an uncapped group (§7). The cap is denominated
in the bond denom, so a capped group cannot move any other denom (an IBC asset
in its treasury) through `MsgSpendFromCommons` or a session claim.

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

Weights are positive `LegacyDec` strings; threshold tallies sum them as
decimals. A group created by `MsgRegisterGroup` may give its members any
positive weight, fractional included. The ten genesis governance bodies (§4)
are one member, one vote:

- **Weight 1.** Unless the signer is x/gov, a member added to (or renewed
  in) a genesis body gets weight `1`, or keeps the weight they already hold.
  That keeps the founder's genesis weight-3 seat on the Technical and
  Ecosystem Councils; only x/gov may give anyone else a weight above 1.
- **Golden share.** On the Technical and Ecosystem Councils the Commons
  Council policy holds a seat whose weight always equals the summed weight
  of every other member, recomputed after each membership change. The
  Commons Council therefore holds half of either council's votes however
  many members join, and nothing passes the 66% threshold without its yes.
  Its veto policy (49%) passes on the Commons seat alone. Only x/gov may
  remove the seat (`ErrGoldenShareSeat`); a weight given for it is ignored.

The genesis bodies are a compiled list (`equalWeightGroups` in
`x/commons/keeper/member_weights.go`), changed only by a chain upgrade.

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
  bool secret_ballot = 13;               // voted only with MsgAnonymousVoteProposal (§11.2)
  bytes voter_root = 14;                 // root of the voter snapshot (secret ballots)
  uint64 ballot_member_count = 15;       // group member count at submit (secret ballots)
  string ballot_member_weight = 16;      // the members' equal weight at submit (secret ballots)
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
| `EXPIRED` | Execution window closed without a successful execute (set by the EndBlocker) |

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

### 3.7. RecurringSpend (migrated to x/session)

A council-approved disbursement schedule. The record is created when a
proposal carrying `MsgScheduleRecurringSpend` executes; each elapsed
period the recipient submits `MsgClaimRecurringSpend` to pull one
`amount_per_period` from the council's policy account.

**Storage migration (M5–M10):** schedules no longer live in a
commons-local `RecurringSpends` collection. They live in
[x/session](x-session-spec.md) as `RECURRING_PULL` grants, with the
council policy as the grant's `granter` and the recipient as the
`grantee`. The four `Msg*RecurringSpend` types stay as thin wrappers
so existing council `StandardPermissions` configs, the recipient CLI
(`tx commons claim-recurring-spend [id]`), and the
`MsgScheduleRecurringSpendResponse` shape are unchanged.

The migration deliberately introduces one semantic break: cancelled
and declined schedules return `NotFound` on
`QueryGetRecurringSpend(id)` because x/session deletes a grant on
Revoke / Decline rather than tombstoning it. The audit trail for
those terminal transitions lives in the `grant_revoked` /
`grant_declined` events (see §3.9 below). COMPLETED schedules ARE
retained and remain queryable.

The wire shape of the `RecurringSpend` proto is unchanged for
`MsgScheduleRecurringSpendResponse` and `QueryGetRecurringSpendResponse`;
field `created_via_proposal_id` is permanently zero post-migration
(the proto field is dead — see migration plan §6).

```protobuf
message RecurringSpend {
  uint64 id = 1;                           // session grant_id
  string authority = 2;                    // session grant.granter (council policy address)
  string recipient = 3;                    // session grant.grantee
  repeated cosmos.base.v1beta1.Coin amount_per_period = 4;  // exactly 1 coin post-M11 (D1.a)
  int64 period_seconds = 5;                // >= session.params.min_recurring_period_seconds
  int64 start_time = 6;                    // unix seconds
  int64 end_time = 7;                      // <= start_time + session.params.max_recurring_duration_seconds
  int64 last_claim_advance = 8;            // schedule's logical clock
  uint64 claims_made = 9;
  RecurringSpendStatus status = 10;        // ACTIVE | COMPLETED (CANCELED/DECLINED return NotFound)
  uint64 created_via_proposal_id = 11;     // dead field — always 0 post-migration
  string note = 12;                        // capped at 256 chars
}
```

Lifecycle:

| Transition | Trigger | Visibility |
|---|---|---|
| `→ ACTIVE` | `MsgScheduleRecurringSpend` executes (council proposal) | Session emits `grant_created` with `source=module_bypass`, `caller_module=<commons module address>` |
| `ACTIVE → (deleted)` | `MsgCancelRecurringSpend` executes (same authority, another council proposal) | Session emits `grant_revoked`; subsequent `Query` returns NotFound |
| `ACTIVE → (deleted)` | `MsgDeclineRecurringSpend` (signer = recipient, direct tx) | Session emits `grant_declined`; subsequent `Query` returns NotFound |
| `ACTIVE → COMPLETED` | `MsgClaimRecurringSpend` advances `last_claim_advance` past `end_time - period_seconds` (final claim was disbursed) | Row retained; status flips to COMPLETED |

Notable design choices (post-migration):

- **Council policy gates apply via `SessionClaimHook`** (registered by
  `app.go` post-depinject). When session fires a claim/pull/oneshot
  whose granter is a registered council policy, `SessionClaimHook.PreCheck`
  runs `CheckSpendPreconditions` (activation + term + per-epoch CHECK)
  and `SessionClaimHook.PostCommit` runs the per-epoch DEBIT. The
  PreCheck/PostCommit split closes a double-debit window the legacy
  single-method check + debit would have left open on bank-send retry
  after `PAUSED_INSUFFICIENT_FUNDS`. See migration plan §3.2.
- **Term-pause** is still derived at claim time. The `SessionClaimHook`
  reuses the existing `CheckSpendPreconditions` semantics; a claim
  against a term-expired council rejects with `ErrGroupExpired`. On
  renewal the same code path passes again. No new persistent paused
  flag.
- **Logical clock**: `last_claim_advance` advances by exactly
  `period_seconds` per claim, anchored to the schedule, not block time.
  A recipient who skips can catch up by submitting multiple claim txs,
  each rate-limited independently against the council's
  `MaxSpendPerEpoch`.
- **First claim opens at `start_time + period_seconds`** — the council
  vote authorises payment after one period of work, not immediately.
- **Single-coin amounts** (D1.a). The wrapper rejects multi-coin
  `amount_per_period`; council pay channels needing multiple denoms
  must use multiple schedules.
- **Self-payment rejected**: `authority == recipient` is now refused
  with `ErrSelfDelegation` — a behavioral tightening from the
  pre-migration handler (§4 / §8 of migration plan).

### 3.8. Indexes and counters

| Collection | Key → Value | Purpose |
|---|---|---|
| `PolicyToName` | `policy_address → council_name` | Reverse lookup; used by every msg-server that authenticates a council action |
| `MarketToGroup` | `market_id → council_name` | Confidence-vote market linkage; consumed by `AfterMarketResolved` |
| `MarketTriggerQueue` | `(trigger_block, council_name)` | Future market triggers (currently unused beyond bootstrap scaffolding) |
| `ProposalsByCouncil` | `(council_name, proposal_id)` | Pagination index for `ListProposals` |
| `VetoPolicies` | `council_name → veto_policy_address` | Cross-council veto resolution |
| `PolicyVersion` | `policy_address → uint64` | Veto-by-bump invalidation token |
| `EpochSpending` | `(policy_address, epoch_day) → string(uspark)` | Cumulative `MsgSpendFromCommons` **and** council-policy RECURRING_PULL claims per UTC day, debited by `SessionClaimHook.PostCommit` |
| `ProposalVoterTrees` | `proposal_id → VoterTree` | A secret ballot's voter snapshot, deleted when voting closes (§11.2) |
| `Polls` | `poll_id → Poll` | Polls (§11.4) |
| `PollVotes` | `(poll_id, voter) → PollVote` | Open votes on polls |
| `PollsByCouncil` | `(council_name, poll_id)` | Pagination index for `ListPolls` |
| `OpenPollsByCouncil` | `(council_name, poll_id)` | Open polls only; the per-group and per-creator caps |
| `PollQueue` | `(due_time, poll_id)` | Deadline of an open poll, end of retention of a closed or cancelled one |
| `PollVoterTrees` | `poll_id → VoterTree` | A secret poll's voter snapshot, deleted when the poll closes |
| `ProposalSeq`, `CouncilSeq`, `CategorySeq`, `PollSeq` | sequences | Auto-increment ID generators (poll IDs start at 1) |

The pre-migration `RecurringSpends*` collections (`RecurringSpends`,
`RecurringSpendsByAuthority`, `RecurringSpendsByRecipient`,
`ActiveRecurringSpendCount`, `RecurringSpendSeq`) were deleted by
M10. Their equivalents live in
[x/session](x-session-spec.md): `Grants`, `GrantsByGranter`,
`GrantsByGrantee`, `ActiveGrantCountByType`, `GrantSeq`.

### 3.8a. Poll, PollVote, VoterTree

```protobuf
enum PollStatus {
  POLL_STATUS_UNSPECIFIED = 0;
  POLL_STATUS_OPEN = 1;                  // voting until the deadline
  POLL_STATUS_CLOSED = 2;                // deadline passed; tallies final
  POLL_STATUS_CANCELLED = 3;             // withdrawn by the creator before any vote
}

message Poll {
  uint64 id = 1;
  string council_name = 2;               // the group whose members vote
  string creator = 3;
  string question = 4;
  repeated string options = 5;
  bool secret_ballot = 6;                // voted with MsgAnonymousVotePoll against the snapshot
  int64 opened_at = 7;
  int64 deadline = 8;                    // unix time voting closes
  PollStatus status = 9;
  repeated uint64 tallies = 10;          // tallies[i] counts votes for options[i]
  bytes voter_root = 11;                 // root of the voter snapshot (secret polls)
  uint64 eligible_voters = 12;           // group size (open) or snapshot leaf count (secret)
  int64 closed_at = 13;                  // when it closed or was cancelled (0 while open)
}

message PollVote {
  uint64 poll_id = 1;
  string voter = 2;
  uint64 option = 3;                     // index into the poll's options
  int64 voted_at = 4;
}

// Voter snapshot of a secret ballot; the shape of x/rep's trust_tree query.
message VoterTree {
  bytes root = 1;
  uint32 depth = 2;                      // the shield circuit's tree depth (20)
  repeated VoterTreeLeaf leaves = 3;     // MiMC(zk_public_key, 0), member-address order
  uint64 member_count = 4;               // group member count, keyless members included
  string member_weight = 5;              // the members' equal weight
}

message VoterTreeLeaf {
  uint64 index = 1;
  bytes hash = 2;
}
```

### 3.9. Params

```protobuf
message Params {
  string proposal_fee = 1;  // e.g. "5000000uspark": min tx fee per MsgSubmitProposal
}
```

`proposal_fee` is a **minimum transaction fee**, not a transfer: the
`ProposalFeeDecorator` ante decorator (§6.1) requires the tx's `--fees` to
cover `proposal_fee` once per fee-requiring `MsgSubmitProposal` in the tx (N
proposals in one tx need N times the fee). The fee goes to validators like
any tx fee. `MsgRegisterGroup` signed by a council additionally transfers
`proposal_fee` from the signing policy to the commons module (a registration
fee). The recurring-spend params live on x/session.

---

## 4. Genesis Bootstrap

`Keeper.BootstrapGovernance` runs during `InitGenesis` and materialises the
Three Pillars graph from the build-tag-selected `genesis_vals_*.go` constants
(`mainnet`, `testnet`, `devnet`, `testparams`). It runs **only when the
genesis `group_map` is empty** (a fresh launch). An imported genesis that
carries groups is a restore: re-running the bootstrap would re-seat founders
removed since genesis and re-create permissions x/gov deleted, because the
import only adds rows.

Only the Commons Council holds `MsgRegisterGroup` among the bootstrap
allowlists (spec §7: gov or Commons Council). The Technical Council does not
hold `/cosmos.gov.v1.MsgUpdateParams`: its signer must be the gov authority,
which a council policy never is.

The chain ships **ten** governance bodies (`Group` records) post-bootstrap:

1. **Commons Council** (parent: gov) — top-level coordination, anonymous
   proposal target. Holds the golden-share seat on the Technical and
   Ecosystem Councils (§3.2).
2. **Technical Council** (parent: gov) — implementation/upgrades.
3. **Ecosystem Council** (parent: gov) — ecosystem programs.
4. **Commons Supervisory Board** (parent: gov) — meta-governance over
   commons committees.
5. **Technical Operations Committee** (parent: Technical Council).
6. **Technical Governance Committee** (parent: Technical Council).
7. **Ecosystem Operations Committee** (parent: Ecosystem Council).
8. **Ecosystem Governance Committee** (parent: Ecosystem Council).
9. **Commons Operations Committee** (parent: Commons Council). It has no electoral delegation: the Commons Council elects it (`MsgUpdateGroupMembers` / `MsgRenewGroup` from the council policy), so a committee proposal can never add, remove or swap the committee's own members.
10. **Commons Governance Committee** (parent: Commons Council). Besides `MsgUpdateGroupMembers`, its bootstrap permissions include x/rep's `MsgForceMigration` and `MsgCancelMigration`: it is the HR authority of the member key migration (council path), see docs/x-rep-spec.md "Member key migration". `MsgRegisterGroup` and `MsgUpdateGroupConfig` refuse a `voting_period` longer than x/rep's `zk_key_activation_delay` (in seconds, network builds only), so a council window can never outlive the anonymous double-vote protection.

The three Operations Committees (Commons, Technical, Ecosystem) each carry
x/rep's project lifecycle messages in their bootstrap permissions:
`MsgApproveProjectBudget`, `MsgCancelProject` and `MsgCompleteProject`. x/rep
resolves authority against the project's `council`, accepting that council's
or its Operations Committee's policy address (an executed proposal) or gov;
a single committee member may only approve a budget up to
`large_project_budget_threshold` directly, and may not cancel or complete a
project. See docs/x-rep-spec.md "Approving a Project Budget", "Cancelling a
Project" and "Completing a Project".

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

### 5.1a. `IsCouncilOrCommitteePolicy(addr, council, committee)`

Returns true for gov, the council's policy address, or the named
committee's policy address — never an individual member. This is **the
helper for value-moving, allowlist and clock-setting operations** in other
modules (operational params with dangerous values, dispute or appeal
resolution, budget moves): such an action must be an executed proposal of
the committee (or council), never one member's signature. x/name's
`MsgUpdateOperationalParams` and `MsgResolveDispute` use it.
`IsCouncilAuthorized` (§5.1) accepts a single member and is only for
low-stakes actions.

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
               ├── policy_version mismatch ──▶ refused (tx error; see 6.4)
               ├── inner-msg handler ok      ──▶ EXECUTED
               └── inner-msg handler error   ──▶ refused (tx error; see 6.4)

ACCEPTED ──(execution window of 14 days past execution_time closes, EndBlocker)──▶ EXPIRED
```

### 6.1. Submit

`MsgSubmitProposal{proposer, policy_address, messages, metadata, secret_ballot}`:

1. Authenticate proposer is a member of the council that owns
   `policy_address` (lookup via `PolicyToName`).
2. Verify the council has not expired (`now <= current_term_expiration`).
3. The proposal fee is enforced by the `ProposalFeeDecorator` ante decorator (`x/commons/ante/group_policy.go`). For every top-level `MsgSubmitProposal` the decorator requires the tx's `--fees` to cover `Params.proposal_fee` once per fee-requiring proposal in the tx; the fee is paid via the standard Cosmos SDK fee path (to validators), not transferred to `policy_address`. `MsgVoteProposal` is **not** fee-gated (a vote costs only gas). Proposals whose every inner message is an emergency action (`MsgEmergencyCancelGovProposal`, `MsgVetoGroupProposals`) are exempt. `MsgSubmitAnonymousProposal` pays no fee (the shield module pays gas); instead each council accepts at most `MaxAnonymousProposalsPerCouncilPerDay` (20) anonymous proposals per UTC day.
3a. A proposal carries at most 20 messages and 10,000 bytes of metadata.
4. For each inner message, check its type URL is in
   `PolicyPermissions[policy_address].allowed_messages`. A signaling proposal
   (no inner messages) is allowed but must carry non-empty `metadata`
   (`ErrInvalidRequest`, "signaling proposal requires non-empty metadata");
   `MsgSubmitAnonymousProposal` applies the same rule.
5. Snapshot `policy_version` at submit time — used later to detect a veto
   that bumped the version mid-flight.
6. Set `voting_deadline = now + DecisionPolicy.voting_period`.
6a. Decide the ballot from the group's `ballot_mode` (§11.2): a secret
    ballot snapshots the voters (`ErrSecretBallotUnavailable` when the
    members' weights differ or too few have a ZK key; `ErrBallotMode` when
    an OPEN group is asked for one). `MsgSubmitAnonymousProposal` applies
    the same rule.
7. Store `Proposal` (status `SUBMITTED`); index in `ProposalsByCouncil`.

### 6.2. Vote

`MsgVoteProposal{voter, proposal_id, option, metadata}`:

1. Proposal must be in `SUBMITTED` status and not a secret ballot
   (`ErrBallotMode`; a secret ballot takes only anonymous votes, §11.2).
2. Voter must be a current member of the council (re-checked on every vote
   so a removed member's pending vote stops counting).
3. Store `Vote[(proposal_id, voter)]`.
4. Run `checkThreshold` — if it now passes, flip status to `ACCEPTED` and
   set `execution_time = block_time + min_execution_period`. This is the
   "early acceptance" path: a quorum is enough; you don't have to wait out
   the full voting window.

### 6.3. Threshold check (`checkThreshold`)

Two policy types, counting member votes only:

```
yesWeight    = sum(member_weight for each YES vote)
totalWeight  = sum(member_weight for each vote)

if policy_type == "percentage":
    groupWeight = sum(member_weight for every council member)
    accepted    = yesWeight / groupWeight >= threshold

if policy_type == "threshold":
    accepted = yesWeight >= threshold   // threshold parsed as integer
```

**Membership changes need a strict majority.** A proposal carrying
`MsgUpdateGroupMembers` or `MsgRenewGroup` is accepted only when
`2 * yesWeight > groupWeight` (of the proposing body's whole weight), on top
of its policy threshold. Under the committees' percentage 0.5 policy a single
member of a two-member committee passes ordinary proposals alone (by design,
see `genesis_bootstrap.go`), but cannot replace their peer with a second key
of their own, or pack a council the committee elects: that would capture the
body (or the council above it) in one vote.

`NO_WITH_VETO` has no tally effect beyond withholding a YES; `TallyProposal`
reports it as `no_with_veto_weight`.

A secret ballot (§11.2) is checked the same way on its anonymous tally:
each anonymous vote counts the snapshot's equal member weight, and
`groupWeight` is the snapshot's member count times that weight (members
without a ZK key count as members who did not vote). An open proposal never
has an anonymous tally.

### 6.4. Execute

`MsgExecuteProposal{executor, proposal_id}`:

1. Status must be `ACCEPTED`.
2. `now >= execution_time` (else `ErrInvalidRequest "min execution period not elapsed"`),
   and `now <= execution_time + ProposalExecutionWindow` (14 days). Past the
   window the EndBlocker marks the proposal `EXPIRED`, so an accepted
   proposal cannot be run months later on the authority of a body whose
   members have since changed.
3. Re-check `policy_version`: if mismatched (i.e. someone vetoed by bumping
   the version, or the body was renewed), return `ErrUnauthorized`. The
   proposal record stays `ACCEPTED` (the tx error discards the write) and
   expires with its window.
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

A failed execution is reported by the tx error, not a persisted `FAILED` or
`VETOED` status: the SDK discards a handler's writes when it returns an
error. Persisting `FAILED` would need a success return on inner failure,
which changes the client contract every script relies on (exit code =
outcome). The execution window bounds how long such a proposal stays
runnable. This is the decided behaviour: a failed or vetoed execution stays a
tx error, and the 14-day execution window (`ProposalExecutionWindow`) is what
retires a proposal that can never run.

### 6.5. EndBlocker finalisation

`EndBlockProposals` walks only the due entries of the `ProposalQueue` index
`(due_time, proposal_id)`: a `SUBMITTED` proposal is queued at its
`voting_deadline`, an `ACCEPTED` one at the end of its execution window. For
each due `SUBMITTED` proposal:

- Tally votes.
- If threshold met → `ACCEPTED` (status flip only; `execution_time` was
  set at submit time as `now + voting_period + min_execution_period` and
  is **not** recomputed here). The early-acceptance path inside
  `MsgVoteProposal` overrides `execution_time` to `block_time +
  min_execution_period` because in that path we'd otherwise wait
  out the rest of the voting window for no reason.
- Else → `REJECTED` with reason `"threshold not met at voting deadline"`.

A due `ACCEPTED` proposal becomes `EXPIRED` ("execution window closed").
Terminal proposals are never in the queue, so the per-block cost follows
open proposals, not proposal history. `ListProposals` is paginated.

At most `MaxProposalTransitionsPerBlock` (20) due queue entries are
processed per block, tallies and expiries together (stale entries count
too). A tally walks every vote outside transaction gas metering, so a burst
of proposals sharing a deadline (a backlog, or anonymous proposals across
councils) is spread over several blocks: the remaining entries stay queued
and drain oldest first, in `(due_time, proposal_id)` order. The delay is
harmless: voting is closed at the deadline whether or not the tally has
run, and execution checks the status.

---

## 7. Messages

| Msg | Authority | Purpose |
|---|---|---|
| `MsgUpdateParams` | gov | Replace `Params.proposal_fee` |
| `MsgSpendFromCommons` | a registered group policy | Send funds from the council policy to a recipient, subject to `MaxSpendPerEpoch` cumulative cap |
| `MsgEmergencyCancelGovProposal` | A signer whose `PolicyPermissions` contain `MsgEmergencyCancelGovProposal` (genesis grants this only on the **veto policies** of Commons/Technical/Ecosystem councils — i.e. the founder's golden-share channel) | Cancel a pending `x/gov` proposal — the council emergency-stop on rogue gov votes. Refused for a gov proposal that disciplines the canceller's own council or a body under it (`MsgDeleteGroup`, `MsgUpdateGroupMembers`, `MsgUpdateGroupConfig`, `MsgRenewGroup`, `MsgVetoGroupProposals`, permission messages): gov is the remedy against a captured council. A deposit-period proposal is also removed from the gov inactive queue |
| `MsgCreatePolicyPermissions` | gov OR the policy address itself (self-registration, only for a registered group policy) | Create the allowlist for a new policy address. `ForbiddenMessages` are refused for every signer, `RestrictedMessages` for every signer but gov |
| `MsgUpdatePolicyPermissions` | gov OR the policy address itself | Replace the allowlist. Bumps `PolicyVersion` only when a message is removed, so pending proposals carrying a now-forbidden message die at execution while a pure expansion (gov adding powers) leaves them alone; ratchet rules in `ValidatePermissions` block adding `Forbidden` messages |
| `MsgDeletePolicyPermissions` | gov OR the policy address itself | Remove the allowlist record; bumps `PolicyVersion` (the strongest narrowing) |
| `MsgRegisterGroup` | gov OR the Commons Council policy (any other signer, including other councils, is refused) | Create a new sub-committee under a parent policy; fails on cycle, missing parent, invalid member counts, or a non-positive weight. For every signer: the allowlist passes `ValidatePermissions` (forbidden / restricted), the vote threshold is present and valid (percentage in (0, 1], absolute in [1, total initial weight]), and a standing committee name keeps the compiled [2,5] size bounds. A council signer is further bounded by its own powers: the child's allowlist must be a subset of the council's own, `funding_weight` must be 0 (x/split shares are gov-only), `max_spend_per_epoch` must be positive and at most the council's own cap, `min_members >= 2`, a percentage threshold at least 0.5, an `electoral_policy_address` empty or a registered group policy other than the child itself, and the name must not be a reserved canonical name (the ten genesis bodies, "Operations Committee", "Governance Committee", "Supervisory Board", case-insensitive) |
| `MsgRenewGroup` | parent policy OR a registered electoral policy (throttled by its `update_cooldown`) | Replace members and reset `current_term_expiration` to `now + term_duration`. Permitted on expired councils (this is the escape hatch the executor honours). The member count must fit `[min_members, max_members]` for every signer, gov included. Bumps `PolicyVersion`: proposals accepted by the old members cannot execute. Weight rules of §3.2 apply; leaving out the golden-share seat is refused unless the signer is gov |
| `MsgUpdateGroupMembers` | parent policy OR the group's electoral policy | Add and/or remove members without resetting the term. The `update_cooldown` is enforced ONLY when the signer is the electoral policy; direct parent or gov calls bypass the cooldown. Weight rules of §3.2 apply; removing the golden-share seat is refused unless the signer is gov |
| `MsgUpdateGroupConfig` | parent policy OR gov | Mutate group-config knobs (`max_spend_per_epoch`, `vote_threshold`, `voting_period`, `min_execution_period`, `futarchy_enabled`, `min_members`, `max_members`, `term_duration`, `policy_type`, `electoral_policy_address`, `funding_weight`, `ballot_mode`; `BALLOT_MODE_UNSPECIFIED` leaves the mode unchanged). Every change bumps `PolicyVersion`. The parent is throttled by `update_cooldown` from `last_config_update`; gov is not. Gov only: `min_members`/`max_members` (the standing committees' compiled [2,5] holds even for gov) and `funding_weight` (replaces the x/split share). A parent may set `max_spend_per_epoch` only to a positive value within its own cap. A new `electoral_policy_address` must be a registered group policy (parent signer) and is cycle-checked. Turning `futarchy_enabled` on queues the first confidence market |
| `MsgDeleteGroup` | parent policy OR a sibling veto policy OR gov | Remove a group from the registry. Refused while another group names it as its parent or electoral authority (`ErrGroupHasDependents`, 1615; delete or re-parent the dependents first, gov included). Members, decision policy, permissions, the x/split share are cleared and the policy version is bumped. Every x/session grant the group's standard or veto policy issued is revoked through `RevokeGrantInternal` (nobody can sign for a deleted policy to cancel them), the policies are tombstoned in `DeletedPolicies` (the session claim hook refuses any grant from a former group policy), and the residual treasury is swept to the parent policy when the parent is a group (with a gov parent the funds stay put) |
| `MsgVetoGroupProposals` | parent policy OR a sibling veto policy OR gov | Bump `PolicyVersion` for the target group's policy — every still-`ACCEPTED` proposal will fail to execute with `VETOED` |
| `MsgForceUpgrade` | Technical Council policy (via proposal) | Schedule a chain upgrade through `x/upgrade` |
| `MsgSubmitProposal` | a council member | Open a new proposal (subject to allowlist + fee + term expiration); `secret_ballot` asks for a secret ballot in a PROPOSER_CHOICE group (§11.2) |
| `MsgVoteProposal` | a current council member | Cast `YES`/`NO`/`ABSTAIN`/`NO_WITH_VETO`; triggers early acceptance if threshold met. Refused on a secret ballot |
| `MsgExecuteProposal` | anyone | Run an accepted proposal's inner messages after `execution_time` |
| `MsgSubmitAnonymousProposal` | x/shield module account only | Same as `MsgSubmitProposal` but skips the membership check (ZK proof attests registration); identical allowlist and term gates, no fee, at most 20 per council per UTC day |
| `MsgAnonymousVoteProposal` | x/shield module account only | One secret-ballot vote (§11.2): proven against the proposal's voter snapshot, once per member; refused on an open proposal (`ErrBallotMode`). Triggers early acceptance like an open vote |
| `MsgOpenPoll` | a member of the group | Open a single-choice poll (§11.4): 2-10 options, a duration within the bounds, open or `secret_ballot` |
| `MsgVotePoll` | a current member of the poll's group | One final open vote on an open poll; refused on a secret poll |
| `MsgAnonymousVotePoll` | x/shield module account only | One secret-ballot poll vote, proven against the poll's voter snapshot |
| `MsgCancelPoll` | the poll's creator | Withdraw an open poll nobody has voted on |
| `MsgCreateCategory` | gov OR Commons Council OR Commons Operations Committee | Register a new shared content category |
| `MsgScheduleRecurringSpend` | a registered group policy | Create a recurring-disbursement schedule from the council's treasury. Wrapped in a `MsgSubmitProposal`. **Wrapper over [x/session](x-session-spec.md)** — constructs a session `MsgCreateGrant` carrying a `RecurringPullPayload` and calls `sessionKeeper.CreateGrantOnBehalfOf`. Per-granter active-grant cap is `session.params.max_recurring_pulls_per_granter` |
| `MsgCancelRecurringSpend` | the schedule's authority (same group policy) | Terminate an active schedule. Wrapped in a `MsgSubmitProposal`; rejects with `ErrRecurringSpendUnauthorized` if the caller is a different council. **Wrapper over [x/session](x-session-spec.md)** — calls `sessionKeeper.RevokeGrantInternal` which DELETES the underlying grant (no tombstone). `ErrGrantNotFound`/`ErrGrantTerminal` are translated to `ErrRecurringSpendInactive` |
| `MsgClaimRecurringSpend` | the schedule's recipient | Pull one period of an active schedule. **Wrapper over [x/session](x-session-spec.md)** — calls `sessionKeeper.ClaimRecurringPullForGrantee`. The `SessionClaimHook` (registered by `app.go`) re-applies `CheckSpendPreconditions` as `PreCheck` (activation + term + per-epoch CHECK) and `PostCommit` (per-epoch DEBIT) for council-policy grants, so per-epoch rate-limit, term expiration, and activation gate apply identically to one-off `MsgSpendFromCommons` |
| `MsgDeclineRecurringSpend` | the schedule's recipient | Permanently opt out of future claims (graceful exit when leaving a role). **Wrapper over [x/session](x-session-spec.md)** — calls `sessionKeeper.DeclineGrantInternal` which DELETES the underlying grant. No proposal required. `ErrGrantNotFound`/`ErrGrantTerminal` are translated to `ErrRecurringSpendInactive` |

The amino type names follow the pattern `sparkdream/x/commons/Msg<Name>`,
declared on every signer message via `option (amino.name)` so Keplr+Ledger
amino-JSON signing works (see the amino-names section of `docs/development-conventions.md`).

#### 7.1. Recurring-spend flow (post-migration)

The wrappers dispatch into x/session; only the council-side state
(group, EpochSpending) lives in x/commons. Schedule rows live in
session.Grants.

```
Council                   Recipient                  Chain (x/commons → x/session)
   │                          │                          │
   │ MsgSubmitProposal(       │                          │
   │   MsgScheduleRecurringSpend)                        │
   │─────────────────────────────────────────────────────▶│
   │                          │   commons: validate authority/single-coin/window
   │ … votes, executes …      │   commons: → CreateGrantOnBehalfOf(commonsModule, msg)
   │                          │   session: validateRecurringPullPayload (period, denom, cap)
   │                          │   session: writeGrant; status=ACTIVE
   │                          │   session emits grant_created
   │                          │              (source=module_bypass, caller_module=<commons>)
   │                          │                          │
   │                          │ MsgClaimRecurringSpend(id)│
   │                          │─────────────────────────▶│
   │                          │   commons: → ClaimRecurringPullForGrantee
   │                          │   session: SessionClaimHook.PreCheck
   │                          │            (commons checkSpendGates: activation/term/rate)
   │                          │   session: bankKeeper.SendCoins
   │                          │   session: SessionClaimHook.PostCommit
   │                          │            (commons recordEpochSpend: bucket DEBIT)
   │                          │   session: last_claim_advance += period
   │                          │   session emits recurring_pull_claimed
   │                          │                          │
   │ MsgSubmitProposal(       │                          │
   │   MsgCancelRecurringSpend)                          │
   │─────────────────────────────────────────────────────▶│
   │ … votes, executes …      │   commons: → RevokeGrantInternal
   │                          │   session: DELETE the grant (no tombstone)
   │                          │   session emits grant_revoked
```

`MsgDeclineRecurringSpend` is the recipient-signed twin of cancel
(`DeclineGrantInternal` → `grant_declined`); same lifecycle, no
proposal flow.

Failure modes worth calling out (errors flow back through the wrapper
mostly unchanged; commons-specific translations are noted):

- **Period not elapsed** (`session.ErrRecurringPullNotDue`) — the
  recipient is claiming faster than `period_seconds`. Each claim
  advances by exactly one period from `last_claim_advance`, anchored
  to the schedule. Bubbles up from session unchanged.
- **Council term expired** (`commons.ErrGroupExpired`, vetoed by
  `SessionClaimHook.PreCheck`) — the auto-pause semantic. The grant
  remains `GRANT_STATUS_ACTIVE` in session (the hook veto does not
  auto-pause); the next claim after the parent calls `MsgRenewGroup`
  succeeds.
- **Per-epoch rate limit** (`commons.ErrRateLimitExceeded`, vetoed by
  `SessionClaimHook.PreCheck`) — claims hit the same
  `MaxSpendPerEpoch` ceiling as one-off `SpendFromCommons` because
  both paths read/write the same `EpochSpending` bucket (PostCommit
  debits the bucket atomically with the bank send).
- **Window closed without a final claim** (`session.ErrRecurringPullWindowClosed`)
  — when the recipient never claims and the schedule's logical clock
  can no longer advance another period before `expires_at`, the next
  claim attempt flips the grant to `COMPLETED` and rejects.
- **Double-cancel / double-decline** (`commons.ErrRecurringSpendInactive`)
  — the wrapper translates session's `ErrGrantNotFound` and
  `ErrGrantTerminal` to `ErrRecurringSpendInactive` so the public
  error contract is preserved.
- **Self-payment** (`session.ErrSelfDelegation`) — `authority == recipient`
  is rejected at schedule time. New behavior post-migration (§4 / §8
  of the migration plan).
- **Multi-coin amount** (`sdkerrors.ErrInvalidRequest`) — schedules
  must specify exactly one coin (D1.a restriction). The wrapper
  rejects multi-coin `amount_per_period` before calling session.

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
| `GetProposal` | `q commons get-proposal [id]` | `Proposal` + its `votes` + `tally` (computed) + `anon_tally` (`AnonVoteTally` of anonymous votes; zeros when none) |
| `ListProposals` | `q commons list-proposals` | Paginated proposals, filterable by council |
| `GetProposalVotes` | `q commons get-proposal-votes [id]` | All member votes cast on a proposal, plus `tally` and `anon_tally` |
| `GetProposalVoterTree` | `q commons get-proposal-voter-tree [id]` | A secret-ballot proposal's voter snapshot (`VoterTree{root, depth, leaves, member_count, member_weight}`) while voting is open; `NotFound` afterwards |
| `GetPoll` | `q commons get-poll [id]` | A poll with its tallies |
| `ListPolls` | `q commons list-polls [--council-name] [--status]` | Paginated polls in ID order (`--reverse` for newest first), filterable by group and status |
| `GetPollVotes` | `q commons get-poll-votes [id]` | The open votes on a poll (a secret poll has none, only tallies) |
| `GetPollVoterTree` | `q commons get-poll-voter-tree [id]` | A secret poll's voter snapshot while it is open |
| `GetCategory` | `q commons get-category [id]` | Single shared content category |
| `ListCategory` | `q commons list-category` | Paginated categories |
| `GetRecurringSpend` | `q commons get-recurring-spend [id]` | Single recurring spend schedule, projected from `session.Grants`. Returns `NotFound` for cancelled/declined schedules (the grant is deleted on terminal transitions — audit trail in events). COMPLETED schedules are retained and queryable |
| `ListRecurringSpends` | `q commons list-recurring-spends --authority [addr] \| --recipient [addr]` | Paginated schedules, projected from `session.ListGrantsBy{Granter,Grantee}(*, RECURRING_PULL)`. **At least one filter is required** (M9 deliberate semantic break: cross-granter pagination is not supported because session has no efficient iterator across all RECURRING_PULL grants). Both filters at once is also rejected. Pagination is in-memory offset+limit over the filtered slice |

The `Get`/`List` distinction is mechanical: `Get` returns one record by key,
`List` paginates the whole map. `NotFound` is a clean gRPC code, so HTTP and
gRPC clients can distinguish "no record" from "empty record".

`GetDecisionPolicy` and `ListDecisionPolicies` complete the council
configuration trifecta: every council has a `Group` (identity),
`PolicyPermissions` (capabilities), and a `DecisionPolicy` (voting rules) —
all three are now public read-only queries.

---

## 9. EndBlocker

Three phases per block:

1. **Proposal finalisation** (`EndBlockProposals`) — walk the due entries
   of `ProposalQueue`: finalise `SUBMITTED` proposals past their
   `voting_deadline` (`ACCEPTED` or `REJECTED`) and expire `ACCEPTED`
   proposals past their execution window, at most 20 entries per block
   (`MaxProposalTransitionsPerBlock`, §6.5). Emits `proposal_finalized`.
   A secret ballot's voter snapshot is deleted once its proposal leaves
   `SUBMITTED` (the root stays on the proposal).
2. **Polls** (`EndBlockPolls`) — walk the due entries of `PollQueue`, at
   most 20 per block: close open polls past their deadline (emits
   `poll_closed` with the final tallies, drops a secret poll's snapshot)
   and prune closed or cancelled polls, with their votes, once
   `PollRetention` has passed.
3. **Confidence-vote markets** (`EndBlockMarketTriggers`) — fire every due
   `MarketTriggerQueue` entry (at most 10 per block) for a group that still
   exists and has `futarchy_enabled`. Each trigger runs in a cache context;
   a failure (for example an unfunded 1,000 SPARK subsidy in the commons
   module account) commits nothing and is retried after one day, so one
   failure no longer ends the group's loop. A successful trigger links the
   market (`MarketToGroup`) and queues the next one at half a term.

**Dependency on x/futarchy.** Wiring the loop makes the elastic-tenure hook
(§10) reachable. Its safety depends on the x/futarchy fixes for FUT-01 (the
last trader decides a market resolved on its final pool), FUT-02 (the
subsidy stranded per market) and FUT-08 (a failed trigger ending the loop,
handled here by the retry). No genesis body enables futarchy and the
top-level councils run 1000-year terms, so the loop only runs for groups a
council or gov opts in.

The module's BeginBlock is a no-op.

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

### 11.1. Anonymous proposals

x/shield routes shielded transactions through `MsgShieldedExec`. Modules
register a `ShieldAware` interface to advertise which message types they
accept anonymously. `x/commons` registers three:

```go
func (k Keeper) IsShieldCompatible(_ context.Context, msg sdk.Msg) bool {
    switch msg.(type) {
    case *types.MsgSubmitAnonymousProposal,
        *types.MsgAnonymousVoteProposal,
        *types.MsgAnonymousVotePoll:
        return true
    default:
        return false
    }
}
```

`MsgSubmitAnonymousProposal` is proven against the x/rep trust tree (any
member may propose anonymously); the two votes against a ballot's voter
snapshot (§11.2).

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
- Every inner message is on the policy's `allowed_messages` allowlist; a
  signaling proposal (no messages) needs non-empty `metadata`, as for
  `MsgSubmitProposal`.
- At most 20 anonymous proposals per council per UTC day (no proposal fee;
  the shield module pays gas).

### 11.2. Secret ballots

A group's `ballot_mode` decides how its proposals are voted: `OPEN` (the
default) with `MsgVoteProposal`, `SECRET` (every proposal a secret ballot) or
`PROPOSER_CHOICE` (`MsgSubmitProposal.secret_ballot` per proposal; asking an
OPEN group for one is `ErrBallotMode`). A proposal is one or the other, never
both: `MsgVoteProposal` is refused on a secret ballot and
`MsgAnonymousVoteProposal` on an open one. That closes the old double-vote
hole, where an anonymous vote proven only against the x/rep trust tree could
be cast by any rep member, and by a council member on top of their open vote.

**Voter snapshot.** When a secret-ballot proposal is submitted, commons
builds a `VoterTree` of the group's members: one leaf `MiMC(zk_public_key, 0)`
per member with an active x/rep ZK key, in member-address order, in a
depth-20 sparse Merkle tree with the shield circuit's hash. It is refused
(`ErrSecretBallotUnavailable`) when the members' weights differ (an anonymous
vote carries no weight; one member, one vote) or fewer than
`MinSecretBallotVoters` members have a key (5 on networks, 2 in the test
build: below that the tally all but names each vote). The proposal records
the root, the member count and the equal weight; the tree is stored until
voting closes and served by `GetProposalVoterTree`. Later membership or key
changes do not affect a ballot already open.

**Voting.** A member proves with the existing shield circuit (no new circuit
or ceremony): membership of the snapshot (leaf metadata 0, `min_trust_level`
0) and a nullifier scoped to the proposal (domain 32), bound to the inner
message. The x/shield registration uses `PROOF_DOMAIN_GROUP_TREE`: shield asks
commons (`ShieldMerkleRoot`, the `ShieldRootResolver` interface) for the
proposal's root and refuses any other, so a trust-tree proof or a proof
against a stale or made-up tree fails, and the nullifier lets each member
vote once. The registration is immediate-mode only: a batch submission is
checked before its payload is decrypted, so its root could not be. The
commons handler checks the signer is the shield module account, the proposal
is an open secret ballot, and adds the vote to `AnonVoteTally`; the threshold
check (§6.3) then runs as for an open vote. The tally shows counts, never who
voted.

**Client flow.** Fetch `GetProposalVoterTree` whole and build the Merkle path
locally (asking a node for one member's path would tell it who is about to
vote); the tree has the shape of x/rep's `trust_tree` query, so the shield
wasm `prove()` takes it unchanged with `min_trust_level` 0. Submit
`MsgShieldedExec` with `proof_domain = 2`, the inner
`MsgAnonymousVoteProposal{voter: shield module account, proposal_id, option}`
and the scope `proposal_id`.

**Limits.** A small group gives little anonymity even above the minimum
(a unanimous tally names everyone's vote). A member whose ZK key leaks
before a ballot opens can be impersonated on that ballot; clearing the key
(`MsgClearZkPublicKey`) only protects ballots opened afterwards. A SECRET
group whose members stop meeting the requirements (unequal weights, too few
keys) cannot pass proposals until its parent or gov changes its
`ballot_mode` or membership.

### 11.3. Queries of anonymous tallies

`GetProposal` and `GetProposalVotes` return `anon_tally`; `tally` reports a
secret ballot's counts at the member weight.

### 11.4. Polls

A poll is a non-binding question put to a group's members; nothing executes.

- `MsgOpenPoll{creator, council_name, question, options, duration_seconds,
  secret_ballot}`: the creator must be a member and the group's term not
  expired. The question is 1-500 bytes; 2-10 options, each 1-100 bytes and
  distinct; the duration between `MinPollDuration` (1 hour on networks, 10 s
  in the test build) and 30 days. At most 20 open polls per group and 3 per
  creator (alias-aware). A secret poll snapshots the voters exactly like a
  secret-ballot proposal (same requirements); its `eligible_voters` is the
  snapshot's leaf count, an open poll's the group's member count. Polls do
  not follow the group's `ballot_mode`: the creator chooses.
- `MsgVotePoll{voter, poll_id, option}`: a current member of the group, one
  vote per member (a migrated key cannot vote again), final; refused on a
  secret poll. Voting is open through the deadline second.
- `MsgAnonymousVotePoll{voter, poll_id, option}`: through x/shield, proven
  against the poll's snapshot (`PROOF_DOMAIN_GROUP_TREE`, nullifier domain
  33, scope `poll_id`); refused on an open poll.
- `MsgCancelPoll{creator, poll_id}`: the creator withdraws an open poll that
  has no votes (`ErrPollHasVotes` otherwise).

The EndBlocker closes a poll after its deadline (status `CLOSED`, tallies
final) and prunes a closed or cancelled poll with its votes after
`PollRetention` (30 days on networks, 2 minutes in the test build). Polls
are fee-free and grant no XP. Votes are one member, one vote: member weights
do not apply.

---

## 12. Errors

| Code | Name | Meaning |
|---|---|---|
| 1100 | `ErrInvalidSigner` | Expected gov account as the sole signer for a proposal-bound message |
| 1600 | `ErrGroupNotFound` | Council/committee name not in the registry |
| 1601 | `ErrInvalidGroupSize` | Member count outside `[min_members, max_members]` |
| 1602 | `ErrRateLimitExceeded` | Vetoed by `SessionClaimHook.PreCheck` (council-policy claim) OR raised inline by `MsgSpendFromCommons` — both paths share the same `MaxSpendPerEpoch` ceiling via the EpochSpending bucket. Also raised by `MsgUpdateGroupConfig` on `update_cooldown` violations |
| 1603 | `ErrGroupNotActive` | Group is in pre-launch (shell) phase; `activation_time > now`. Vetoed by `SessionClaimHook.PreCheck` for recurring claims |
| 1604 | `ErrGroupExpired` | `current_term_expiration < now`. Vetoed by `SessionClaimHook.PreCheck` for recurring claims (the auto-pause path — schedule stays ACTIVE in session; on renewal the next claim passes) |
| 1605 | `ErrInvalidMemberWeight` | A member weight is not a positive number, or a genesis governance body was given a weight other than 1 by a signer other than gov (§3.2) |
| 1606 | `ErrGoldenShareSeat` | A membership change by a signer other than gov would remove the Commons Council's golden-share seat from the Technical or Ecosystem Council |
| 1607 | `ErrBallotMode` | An open vote on a secret ballot, an anonymous vote on an open one, or a secret ballot asked of an OPEN group |
| 1608 | `ErrSecretBallotUnavailable` | A secret ballot needs equal member weights and at least `MinSecretBallotVoters` members with a registered ZK key |
| 1609 | `ErrPollNotFound` | No such poll |
| 1610 | `ErrPollNotOpen` | The poll is closed, cancelled or past its deadline |
| 1611 | `ErrInvalidPoll` | Question, options, duration or vote option out of bounds |
| 1612 | `ErrPollLimit` | 20 open polls in the group, or 3 by this creator |
| 1613 | `ErrAlreadyVoted` | The member already voted on this poll |
| 1614 | `ErrPollHasVotes` | A poll with votes cannot be cancelled |
| 1700 | `ErrRecurringSpendNotFound` | Used internally; the wrapper layer now surfaces `codes.NotFound` for cancelled/declined schedules via projection (M9). Reserved for legacy use sites |
| 1701 | `ErrRecurringSpendInactive` | Schedule is in a terminal state. Post-migration this is the translated error for session's `ErrGrantNotFound` and `ErrGrantTerminal` returned from cancel/decline on a grant that's been deleted or already COMPLETED |
| 1705 | `ErrRecurringSpendInvalidWindow` | `start_time`/`end_time` malformed (past, out-of-order, shorter than one period). Other window errors (period below min, duration over max) bubble up as session errors — the wrapper does NOT translate them |
| 1707 | `ErrRecurringSpendUnauthorized` | Caller is not the schedule's authority (cancel) or recipient (decline). For claim mismatches the session-side `ErrRecurringPullUnauthorized` bubbles up unchanged |

Errors **removed by M10** (proto-reserved field numbers retained for
wire-compat, but no longer raised by any handler):

- `ErrRecurringSpendNotDue` (1702) — superseded by `session.ErrRecurringPullNotDue`
- `ErrRecurringSpendWindowClosed` (1703) — superseded by `session.ErrRecurringPullWindowClosed`
- `ErrRecurringSpendInvalidPeriod` (1704) — superseded by `session.ErrPeriodTooShort` / `ErrDurationTooLong`
- `ErrRecurringSpendCapReached` (1706) — superseded by `session.ErrMaxRecurringPullsExceeded`

Other errors raised inline with `cosmossdk.io/errors`'s sentinel set:

- `ErrUnauthorized` — authority/signer mismatch, anonymous-message non-shield
  caller, allowlist violation, term-expired with non-`MsgRenewGroup` content,
  policy-version mismatch (veto), `validateMsgAuthority` failure.
- `ErrNotFound` — proposal/group/category lookup miss.
- `ErrInvalidRequest` — `min_execution_period` not yet elapsed, malformed
  amount, missing required field, signaling proposal (direct or anonymous)
  with empty `metadata`.
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
| `poll_opened` | `poll_id`, `council_name`, `creator`, `deadline`, `secret_ballot` | `MsgOpenPoll` |
| `poll_vote` | `poll_id`, `voter`, `option` | `MsgVotePoll` |
| `anonymous_poll_vote` | `poll_id`, `option` | `MsgAnonymousVotePoll` |
| `poll_cancelled` | `poll_id` | `MsgCancelPoll` |
| `poll_closed` | `poll_id`, `council_name`, `tallies` (comma-separated, in option order) | EndBlocker |
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
  repeated FoundingMember founding_members = 13;        // launch-only override, not exported
  repeated EpochSpendingEntry epoch_spending = 14;      // (policy_address, epoch_day, amount)
  repeated MarketToGroupEntry market_to_group = 15;     // (market_id, group_name)
  repeated MarketTriggerEntry market_trigger_queue = 16;// (trigger_time, group_name)
  repeated string deleted_policies = 17;                // tombstoned former group policies
  repeated AnonVoteTallyEntry anon_vote_tallies = 18;   // (proposal_id, AnonVoteTally)
  repeated AnonProposalCountEntry anon_proposal_counts = 19; // (council_name, utc_day, count)
  repeated Poll polls = 20;                             // open, and closed/cancelled within retention
  repeated PollVote poll_votes = 21;                    // open votes on stored polls
  uint64 next_poll_id = 22;                             // 0 primes to 1 at import
  repeated ProposalVoterTreeEntry proposal_voter_trees = 23; // snapshots of open secret ballots
  repeated PollVoterTreeEntry poll_voter_trees = 24;    // snapshots of open secret polls
}
```

The poll indexes (`PollsByCouncil`, `OpenPollsByCouncil`) and `PollQueue`
are rebuilt from `polls` at import. Validation refuses duplicate polls or
votes, a poll ID outside `[1, next_poll_id)`, a tally count that differs from
the option count, an open vote on a secret poll, and a voter snapshot for a
poll or proposal that is not an open secret ballot.

Recurring spends live in x/session and round-trip through its genesis;
commons genesis carries no recurring-spend state.

`InitGenesis`:

1. **Bootstrap** — `BootstrapGovernance` (with the optional x/name display
   names and handles) runs only when `group_map` is empty, i.e. a fresh
   launch.
2. **Restore** — every state object above is written back into its
   collection. Sequences are reset to `next_*_id`. Groups are keyed by
   `Group.index`, their name; an empty index is refused. `PolicyToName`
   (standard and veto policies) and `VetoPolicies` are rebuilt from
   `group_map`, and the `ProposalQueue` from the open proposals, so a
   restored group is fully usable (proposals, spend gates, session claim
   hook). The anonymous vote tallies and the per-(council, UTC day)
   anonymous-proposal counters are restored with their full key, so a
   round trip neither hides tallies from proposal queries nor grants a
   second daily allowance.

`ExportGenesis` emits members and votes in store order (deterministic). The
`Groups` collection key is authoritative: each exported group's `index` is
set from its key (both group constructors also store it).

`GenesisState.Validate` checks, when `group_map` is non-empty: no empty
group index, no policy or veto address claimed by two groups (it would
collide in `PolicyToName`), and every `council_members` entry names an
imported group. It also rejects duplicate anonymous tallies and counters.
A launch genesis (empty `group_map`) has nothing to cross-check: the
bootstrap builds the groups.

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
  current value is refused; the proposal then expires with its execution
  window.
- **Invariants** (`x/commons/keeper/invariants.go`, run by app's
  `TestInvariants`): every group policy (and veto policy) indexes back to its
  group and every index entry names a live group; every group has between 1
  and `max_members` members; a golden-share seat weighs exactly the sum of
  the other members.
- **Term-expired escape hatch**: an expired council can still execute a
  proposal whose only inner message is `MsgRenewGroup`. Any other content
  in the same proposal makes the entire execute reject. This prevents an
  expired-but-still-accepted proposal from sneaking a payload through
  while the parent's renewal is pending.
- **Cycle detection at register-group / update-group-config**: parent
  pointers are validated against the existing graph before persisting.
  The chain cannot end up in a state where governance authority loops
  back on itself.
- **Anonymous-proposal funnel**: `MsgSubmitAnonymousProposal` is
  *only* accepted when the on-tx signer is the x/shield module account.
  This is the gateway condition — no individual user can directly call
  these messages, even if they own the shield-bound DREAM stake.
- **Proposal fee disincentive**: every top-level `MsgSubmitProposal` needs
  `Params.proposal_fee` in tx fees, once per proposal in the tx.
  Anonymous proposals are fee-free but capped at 20 per council per day.
- **Forbidden messages**: `ForbiddenMessages` (refused in every allowlist,
  even by gov) covers the delegation escapes: x/session `MsgCreateGrant`,
  `MsgCreateSession`, `MsgExecSession`, every x/vault message,
  x/shield `MsgShieldedExec`, x/guardian `MsgExec`, plus the authz/group
  and consensus entries. A policy holding one could hand its powers or
  treasury to a single key and act without a proposal per action.
- **Group registration is bounded**: only gov or the Commons Council
  registers groups, and a council can delegate only what it holds (§7).
- **Deleted groups stop paying**: `MsgDeleteGroup` revokes the group's
  session grants, tombstones its policies (the claim hook fails closed) and
  sweeps its treasury to the parent.
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
  the Commons Council's golden-share vote; verifies the upgrade plan is
  staged via `x/upgrade`.
- `fire_council_test.sh` — gov-driven full council replacement.
- `anon_test.sh` — anonymous proposal via shield; anonymous votes refused.
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
