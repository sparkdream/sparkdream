# x/reveal Module Specification

## Overview

The `x/reveal` module enables progressive open-sourcing of existing closed-source code through:
- Any trusted member can propose code for reveal (not limited to founders)
- Tranched conviction staking with DREAM from the community
- Contributor bond to guarantee follow-through
- Code reveal after stake threshold met
- Stake-weighted verification voting (contributor excluded to prevent self-dealing)
- Dispute resolution via Commons Council vote (prevents single-member collusion)
- Payout holdback until all tranches complete (prevents abandon-after-profit)
- Transition to community-owned x/rep project post-reveal

## Access Control

All participants must be active x/rep members:
- **Proposing**: Requires `TRUST_LEVEL_ESTABLISHED` or higher
- **Staking**: Requires `TRUST_LEVEL_PROVISIONAL` or higher (any active member). **Contributors cannot stake to their own contributions** (prevents self-staking → self-verification loop).
- **Voting on verification**: Only stakers for that tranche may vote. **Contributors cannot vote on their own contributions** (prevents self-dealing).
- **Approving proposals**: Requires a **Commons Council group proposal** (verified via `x/commons`). An Operations Committee member submits `MsgApprove` which creates a council proposal; the council votes to approve or reject. This prevents single-member collusion with contributors.
- **Resolving disputes**: Requires a **Commons Council group proposal** (verified via `x/commons`). Same mechanism as approvals — prevents a single committee member from unilaterally resolving disputes in a contributor's favor.

## State

### Contribution

A contribution represents a body of existing code being progressively revealed to the community.

```protobuf
message Contribution {
  uint64 id = 1;
  string contributor = 2;          // address of the member proposing the code
  string project_name = 3;
  string description = 4;

  repeated RevealTranche tranches = 5;
  uint32 current_tranche = 6;

  string total_valuation = 7 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Bond posted by contributor at proposal time; slashed on failure to reveal
  string bond_amount = 8 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  // Remaining bond after any partial slashes (starts equal to bond_amount)
  string bond_remaining = 9 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  string initial_license = 10;     // license before full reveal (e.g., "Source Available")
  string final_license = 11;       // license after all tranches verified (e.g., "Apache 2.0")

  bool transitioned_to_project = 12;
  uint64 project_id = 13;          // x/rep Project ID after transition

  ContributionStatus status = 14;
  uint64 council_id = 15;          // Commons Council that approved (used for project transition)
  string approved_by = 16;         // Operations Committee member who initiated approval
  int64 approved_at = 17;
  int64 created_at = 18;

  // DREAM held back from tranche payouts until all tranches complete
  string holdback_amount = 19 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Epoch at which a rejected contributor may re-propose (0 if not rejected)
  int64 proposal_eligible_at = 20;
}

enum ContributionStatus {
  CONTRIBUTION_STATUS_PROPOSED = 0;
  CONTRIBUTION_STATUS_IN_PROGRESS = 1;  // set on approval; tranche 0 starts STAKING
  CONTRIBUTION_STATUS_COMPLETED = 2;
  CONTRIBUTION_STATUS_CANCELLED = 3;    // rejected, cancelled, or any tranche failed
}
```

### RevealTranche

Each tranche represents a discrete chunk of code to be backed by community stakes, revealed, and verified independently. Tranches are stored inline on the `Contribution` since the count is bounded by `max_tranches` (default 10).

```protobuf
message RevealTranche {
  uint32 id = 1;
  string name = 2;
  string description = 3;
  repeated string components = 4;  // logical components included in this tranche

  // DREAM that must be staked to back this tranche; also the mint amount on payout
  // Invariant: sum(stake_threshold) across all tranches == contribution.total_valuation
  string stake_threshold = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string dream_staked = 6 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  string preview_uri = 7;          // demo, screenshot, or redacted preview
  string code_uri = 8;             // populated on reveal (e.g., IPFS CID)
  string docs_uri = 9;             // populated on reveal
  string commit_hash = 10;         // git commit hash for integrity verification

  int64 stake_deadline = 11;     // enforced by EndBlocker
  int64 reveal_deadline = 12;      // enforced by EndBlocker
  int64 verification_deadline = 13;// enforced by EndBlocker

  TrancheStatus status = 14;
  int64 backed_at = 15;
  int64 revealed_at = 16;
  int64 verified_at = 17;
}

enum TrancheStatus {
  TRANCHE_STATUS_LOCKED = 0;       // waiting for previous tranche to complete
  TRANCHE_STATUS_STAKING = 1;      // open for conviction stakes
  TRANCHE_STATUS_BACKED = 2;       // dream_staked >= stake_threshold; community has shown conviction; awaiting reveal
  TRANCHE_STATUS_REVEALED = 3;     // code submitted; verification period active
  TRANCHE_STATUS_VERIFIED = 4;     // verification passed; payout complete
  TRANCHE_STATUS_DISPUTED = 5;     // verification failed; routed to council
  TRANCHE_STATUS_CANCELLED = 6;    // stake deadline expired, or contribution cancelled
  TRANCHE_STATUS_FAILED = 7;       // dispute verdict: REJECT (or auto-REJECT on timeout)
}
```

### RevealStake

Stakes are stored in their own indexed collection keyed by `(contribution_id, tranche_id, staker)` to avoid loading all stakes when reading a tranche.

```protobuf
message RevealStake {
  uint64 id = 1;
  string staker = 2;
  uint64 contribution_id = 3;
  uint32 tranche_id = 4;
  string amount = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 staked_at = 6;
}
```

### VerificationVote

Votes are stored in their own indexed collection keyed by `(contribution_id, tranche_id, voter)` to avoid loading all votes when reading a tranche.

```protobuf
message VerificationVote {
  string voter = 1;
  uint64 contribution_id = 2;
  uint32 tranche_id = 3;
  bool value_confirmed = 4;        // does the code deliver what was promised?
  uint32 quality_rating = 5;       // 1-5; used to calculate reputation granted to contributor
  string comments = 6;
  string stake_weight = 7 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 voted_at = 8;
}
```

**`quality_rating` purpose**: The stake-weighted average quality rating determines the reputation bonus granted to the contributor on payout. A rating of 5 grants full reputation; lower ratings scale linearly (e.g., avg 3/5 = 60% of max rep). This incentivizes high-quality code, not just technically passing code.

## Params

```protobuf
message Params {
  int64 stake_deadline_epochs = 1;     // max epochs a tranche stays in STAKING
  int64 reveal_deadline_epochs = 2;      // max epochs after BACKED to submit code
  int64 verification_period_epochs = 3;  // duration of verification voting window
  int64 dispute_resolution_epochs = 4;   // max epochs for council to resolve a dispute

  string verification_threshold = 5 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  uint32 min_verification_votes = 6;     // base minimum; scales with tranche valuation

  uint32 max_tranches = 7;
  string max_tranche_valuation = 8 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Bond = bond_rate * total_valuation; slashed if contributor fails to reveal
  string bond_rate = 9 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];

  // Minimum trust level required to propose a contribution
  uint32 min_proposer_trust_level = 10;  // maps to TrustLevel enum; default ESTABLISHED (2)

  // Maximum total valuation across all tranches for a single contribution
  string max_total_valuation = 11 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Minimum DREAM stake amount (prevents dust stakes used for vote griefing)
  string min_stake_amount = 12 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Percentage of each tranche payout held back until all tranches complete
  // Prevents abandon-after-profit: contributor must finish all tranches to collect full payout
  string payout_holdback_rate = 13 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];

  // Epochs a rejected contributor must wait before re-proposing (prevents spam)
  int64 proposal_cooldown_epochs = 14;
}
```

### Verification Vote Scaling

The `min_verification_votes` param is a **base minimum** (default 3). The effective minimum scales with tranche valuation to ensure larger payouts receive proportionally more scrutiny:

```
effective_min_votes = max(min_verification_votes, tranche_stake_threshold / 5000)
```

For example: a 15,000 DREAM tranche requires at least 3 votes (15000/5000 = 3), while a 50,000 DREAM tranche requires at least 10.

## Messages

```protobuf
service Msg {
  rpc Propose(MsgPropose) returns (MsgProposeResponse);
  rpc Approve(MsgApprove) returns (MsgApproveResponse);
  rpc Reject(MsgReject) returns (MsgRejectResponse);
  rpc Stake(MsgStake) returns (MsgStakeResponse);
  rpc Withdraw(MsgWithdraw) returns (MsgWithdrawResponse);
  rpc Reveal(MsgReveal) returns (MsgRevealResponse);
  rpc Verify(MsgVerify) returns (MsgVerifyResponse);
  rpc Cancel(MsgCancel) returns (MsgCancelResponse);
  rpc ResolveDispute(MsgResolveDispute) returns (MsgResolveDisputeResponse);
}

// Any member with sufficient trust level can propose.
// Validation:
// - total_valuation <= max_total_valuation
// - len(tranches) <= max_tranches
// - Each tranche: stake_threshold <= max_tranche_valuation
// - sum(stake_threshold) across all tranches == total_valuation
// - Contributor has no active proposal_cooldown
// - Bond (bond_rate * total_valuation) deducted and locked
message MsgPropose {
  string contributor = 1;
  string project_name = 2;
  string description = 3;
  string total_valuation = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  repeated TrancheDef tranches = 5;
  string initial_license = 6;
  string final_license = 7;
}

message TrancheDef {
  string name = 1;
  string description = 2;
  repeated string components = 3;
  // DREAM that must be staked to back this tranche; also the mint amount on payout
  string stake_threshold = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string preview_uri = 5;
}

// Operations Committee member initiates approval; routed as Commons Council proposal.
// The council votes to approve — prevents single-member collusion with contributors.
// Accept-or-reject only (no term modification by committee or council).
message MsgApprove {
  string authority = 1;            // Commons Council group policy account (executed via proposal)
  string proposer = 2;            // Operations Committee member who initiated the proposal
  uint64 contribution_id = 3;
}

// Operations Committee member initiates rejection; routed as Commons Council proposal.
// Contributor may re-propose after proposal_cooldown_epochs.
message MsgReject {
  string authority = 1;            // Commons Council group policy account (executed via proposal)
  string proposer = 2;            // Operations Committee member who initiated the proposal
  uint64 contribution_id = 3;
  string reason = 4;
}

// Any active member stakes DREAM toward a tranche to show conviction.
// Staked DREAM is temporarily locked and returned after verification — it is NOT payment.
// The contributor is paid via freshly minted DREAM on tranche verification.
// Validation:
// - Staker must NOT be the contribution's contributor (prevents self-staking loop)
// - Amount must be >= min_stake_amount (prevents dust stake griefing)
// - Total staked must not exceed stake_threshold (excess stakes rejected)
message MsgStake {
  string staker = 1;
  uint64 contribution_id = 2;
  uint32 tranche_id = 3;
  string amount = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

// Withdraw a stake (only allowed before verification period)
message MsgWithdraw {
  string staker = 1;
  uint64 stake_id = 2;
}

// Contributor reveals the code for a backed tranche
message MsgReveal {
  string contributor = 1;
  uint64 contribution_id = 2;
  uint32 tranche_id = 3;
  string code_uri = 4;
  string docs_uri = 5;
  string commit_hash = 6;
}

// Staker votes on whether revealed code matches promises.
// Voter must NOT be the contribution's contributor (prevents self-verification).
message MsgVerify {
  string voter = 1;
  uint64 contribution_id = 2;
  uint32 tranche_id = 3;
  bool value_confirmed = 4;
  uint32 quality_rating = 5;       // 1-5
  string comments = 6;
}

// Contributor cancels their own contribution (only if no tranche is BACKED or beyond)
// Operations Committee can cancel any contribution at any time
message MsgCancel {
  string authority = 1;            // contributor or committee member
  uint64 contribution_id = 2;
  string reason = 3;
}

// Dispute resolution routed as Commons Council proposal (same as approvals).
// Prevents a single committee member from unilaterally siding with a contributor.
message MsgResolveDispute {
  string authority = 1;            // Commons Council group policy account (executed via proposal)
  string proposer = 2;            // Operations Committee member who initiated the proposal
  uint64 contribution_id = 3;
  uint32 tranche_id = 4;
  DisputeVerdict verdict = 5;
  string reason = 6;              // required for IMPROVE/REJECT; feedback for contributor
}

enum DisputeVerdict {
  DISPUTE_VERDICT_UNSPECIFIED = 0; // proto default; rejected by message handler
  DISPUTE_VERDICT_ACCEPT = 1;     // code is acceptable; proceed to payout
  DISPUTE_VERDICT_IMPROVE = 2;    // code has merit but needs work; contributor may re-reveal
  DISPUTE_VERDICT_REJECT = 3;     // unacceptable or bad faith; hard fail
}
```

## Queries

```protobuf
service Query {
  // Single contribution by ID
  rpc Contribution(QueryContributionRequest) returns (QueryContributionResponse);

  // All contributions with pagination
  rpc Contributions(QueryContributionsRequest) returns (QueryContributionsResponse);

  // Contributions by contributor address
  rpc ContributionsByContributor(QueryContributionsByContributorRequest) returns (QueryContributionsByContributorResponse);

  // Contributions filtered by status
  rpc ContributionsByStatus(QueryContributionsByStatusRequest) returns (QueryContributionsByStatusResponse);

  // Single tranche detail (includes current vote tally)
  rpc Tranche(QueryTrancheRequest) returns (QueryTrancheResponse);

  // Verification tally for a tranche (aggregated totals without full vote list)
  rpc TrancheTally(QueryTrancheTallyRequest) returns (QueryTrancheTallyResponse);

  // All stakes for a tranche
  rpc TrancheStakes(QueryTrancheStakesRequest) returns (QueryTrancheStakesResponse);

  // Single stake by ID
  rpc StakeDetail(QueryStakeDetailRequest) returns (QueryStakeDetailResponse);

  // All stakes by a specific staker
  rpc StakesByStaker(QueryStakesByStakerRequest) returns (QueryStakesByStakerResponse);

  // All verification votes by a specific voter
  rpc VotesByVoter(QueryVotesByVoterRequest) returns (QueryVotesByVoterResponse);

  // Module params
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);
}
```

## Keeper Interface

```go
type Keeper interface {
    // Contribution management
    ProposeContribution(ctx context.Context, contributor sdk.AccAddress, msg *MsgPropose) (uint64, error)
    // Approve sets status → IN_PROGRESS, tranche 0 → STAKING, records council_id
    ApproveContribution(ctx context.Context, councilID uint64, proposer sdk.AccAddress, contributionID uint64) error
    RejectContribution(ctx context.Context, councilID uint64, proposer sdk.AccAddress, contributionID uint64, reason string) error
    CancelContribution(ctx context.Context, authority sdk.AccAddress, contributionID uint64, reason string) error
    GetContribution(ctx context.Context, contributionID uint64) (Contribution, error)

    // Staking (enforces: staker != contributor, amount >= min_stake, no over-staking)
    StakeTranche(ctx context.Context, staker sdk.AccAddress, contributionID uint64, trancheID uint32, amount math.Int) (uint64, error)
    WithdrawStake(ctx context.Context, staker sdk.AccAddress, stakeID uint64) error

    // Reveal
    RevealTranche(ctx context.Context, contributor sdk.AccAddress, contributionID uint64, trancheID uint32, codeURI, docsURI, commitHash string) error

    // Verification (enforces: voter != contributor)
    SubmitVerificationVote(ctx context.Context, voter sdk.AccAddress, contributionID uint64, trancheID uint32, vote VerificationVote) error
    TallyVerificationVotes(ctx context.Context, contributionID uint64, trancheID uint32) (bool, error)
    ConfirmTrancheValue(ctx context.Context, contributionID uint64, trancheID uint32) error

    // Dispute resolution (executed via council proposal)
    // Verdict: ACCEPT (payout), IMPROVE (re-reveal + clear votes), REJECT (hard fail)
    // IMPROVE: deletes all verification votes for the tranche, resets to BACKED with new reveal_deadline
    ResolveDispute(ctx context.Context, councilID uint64, proposer sdk.AccAddress, contributionID uint64, trancheID uint32, verdict DisputeVerdict, reason string) error

    // Transition
    TransitionToProject(ctx context.Context, contributionID uint64) (uint64, error)

    // Holdback release (called when all tranches complete)
    ReleaseHoldback(ctx context.Context, contributionID uint64) error

    // EndBlocker
    ProcessDeadlines(ctx context.Context) error
}
```

## Lifecycle Flow

### Complete Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    PROGRESSIVE REVEAL LIFECYCLE                         │
└─────────────────────────────────────────────────────────────────────────┘

PHASE 1: PROPOSAL
┌─────────────────────────────────┐
│ Member proposes contribution     │
│ - Must be ESTABLISHED or higher │
│ - Project name/description      │
│ - Total valuation               │
│   (≤ max_total_valuation)       │
│ - Tranche breakdown             │
│ - License terms                 │
│ - Bond auto-locked (10% of val) │
└──────────────┬──────────────────┘
               │
               ▼
┌─────────────────────────────────┐
│ Commons Council votes           │
│ (initiated by Ops Committee)    │
│ - Evaluate valuation            │
│ - Review tranche structure      │
│ - Council votes to accept/reject│
│ - If rejected, contributor may  │
│   re-propose after cooldown     │
│   (proposal_cooldown_epochs) │
│ - Bond returned on rejection    │
└──────────────┬──────────────────┘
               │
          ┌────┴────┐
          │         │
       Approved  Rejected
          │         │
          │         ▼
          │    ┌─────────────┐
          │    │ Bond returned│
          │    │ Status set   │
          │    │ CANCELLED    │
          │    │ Cooldown set │
          │    └─────────────┘
          │
          ▼
┌─────────────────────────────────┐
│ Contribution → IN_PROGRESS      │
│ Tranche 0 → STAKING             │
│ (subsequent tranches stay LOCKED)│
└──────────────┬──────────────────┘
               │
               ▼
PHASE 2: STAKING (per tranche, sequential)
┌─────────────────────────────────┐
│ Tranche N opens for staking     │
│                                 │
│ Community stakes DREAM to show  │
│ conviction (returned after      │
│ verification — not payment):    │
│ - Contributor CANNOT stake to  │
│   own contribution              │
│ - Min stake: min_stake_amount │
│ - Lock DREAM in module account  │
│ - Stake weight = amount         │
│ - Rejects excess (no overstake)│
│ - Can withdraw before           │
│   verification period starts    │
│                                 │
│ Deadline: 60 epochs (~2 months) │
└──────────────┬──────────────────┘
               │
          ┌────┴────┐
          │         │
          ▼         ▼
      Backed    Not Backed (EndBlocker)
          │         │
          │         ▼
          │    ┌─────────────┐
          │    │ Stakes      │
          │    │ returned     │
          │    │ Tranche →    │
          │    │ CANCELLED    │
          │    │ Remaining    │
          │    │ tranches     │
          │    │ also cancel  │
          │    └─────────────┘
          │
          ▼
PHASE 3: REVEAL
┌─────────────────────────────────┐
│ Contributor reveals code        │
│ - Upload to IPFS or hosted repo │
│ - Submit code URI               │
│ - Submit docs URI               │
│ - Include commit hash           │
│                                 │
│ Deadline: 14 epochs (~2 weeks)  │
└──────────────┬──────────────────┘
               │
          ┌────┴────┐
          │         │
          ▼         ▼
     Revealed   Not Revealed (EndBlocker)
          │         │
          │         ▼
          │    ┌──────────────────┐
          │    │ Stakes returned  │
          │    │ Bond slashed      │
          │    │ (burned)          │
          │    │ Contributor loses │
          │    │ reputation        │
          │    │ Tranche → FAILED  │
          │    └──────────────────┘
          │
          ▼
PHASE 4: VERIFICATION
┌─────────────────────────────────┐
│ Stakers verify value           │
│                                 │
│ Only stakers for this tranche  │
│ may vote (skin in the game):    │
│ - Contributor CANNOT vote on    │
│   own contribution              │
│ - Review revealed code          │
│ - Vote: value confirmed? (Y/N) │
│ - Rate quality (1-5)            │
│ - Provide comments              │
│                                 │
│ Vote weight = stake amount     │
│ Withdrawals blocked during this │
│ period to preserve vote weights │
│                                 │
│ Period: 14 epochs (~2 weeks)    │
└──────────────┬──────────────────┘
               │
               ▼
┌─────────────────────────────────┐
│ Tally (EndBlocker at deadline)  │
│                                 │
│ Pass if:                        │
│ - ≥ 60% stake-weighted YES     │
│ - ≥ effective_min_votes (scaled │
│   by tranche valuation)         │
│                                 │
│ If min_votes not met:           │
│ - Extend by verification_period │
│ - Max 1 extension               │
└──────────────┬──────────────────┘
               │
          ┌────┴────┐
          │         │
          ▼         ▼
     Confirmed   Disputed
          │         │
          │         ▼
          │    ┌──────────────────────┐
          │    │ Tranche → DISPUTED    │
          │    │ Routed to Commons     │
          │    │ Council for vote      │
          │    │ (initiated by Ops     │
          │    │  Committee member)    │
          │    │                       │
          │    │ Council has           │
          │    │ dispute_resolution    │
          │    │ _epochs to decide     │
          │    │                       │
          │    │ Verdict:              │
          │    │ ├─ ACCEPT: proceed    │
          │    │ │  to payout          │
          │    │ ├─ IMPROVE: back to   │
          │    │ │  BACKED for re-     │
          │    │ │  reveal (no slash)  │
          │    │ └─ REJECT: FAILED,    │
          │    │    50% bond slashed,  │
          │    │    holdback burned,   │
          │    │    remaining tranches │
          │    │    cancelled          │
          │    │                       │
          │    │ Timeout → auto-REJECT │
          │    └──────────────────────┘
          │
          ▼
PHASE 5: PAYOUT (with holdback)
┌─────────────────────────────────┐
│ Tranche confirmed (VERIFIED)    │
│                                 │
│ - Mint DREAM to contributor     │
│   (amount = stake_threshold     │
│    minus holdback portion)      │
│ - Holdback (payout_holdback_rate│
│   e.g., 20%) retained in module │
│   account until ALL tranches    │
│   complete                      │
│ - Return stakes to stakers    │
│ - Grant reputation to           │
│   contributor (scaled by avg    │
│   quality rating)               │
│ - Unlock next tranche for       │
│   staking                       │
│ - Emit EventTrancheVerified     │
└──────────────┬──────────────────┘
               │
               ▼
        [Repeat for each tranche]
               │
               ▼
PHASE 6: TRANSITION
┌─────────────────────────────────┐
│ All tranches verified           │
│                                 │
│ - Code fully under final_license│
│ - Release all holdback DREAM    │
│   to contributor                │
│ - Return contributor bond       │
│ - Create community x/rep Project│
│   - council = council_id (the   │
│     Commons Council that        │
│     approved)                   │
│   - creator = contributor       │
│   - category = from contribution│
│   - budget = 0 (starts fresh)   │
│   - owned by the community, NOT │
│     the contributor             │
│ - Future work via standard      │
│   initiative system; anyone can │
│   propose initiatives           │
│ - Contributor has no special    │
│   privileges on the project     │
│ - Status → COMPLETED            │
└─────────────────────────────────┘
```

## EndBlocker

The EndBlocker runs every block and enforces all deadline-based transitions. It iterates over contributions with status `IN_PROGRESS` and checks each active tranche:

```go
func (k Keeper) ProcessDeadlines(ctx context.Context) error {
    // For each IN_PROGRESS contribution:
    //   For each tranche:
    //     1. STAKING + past stake_deadline → cancel tranche + all subsequent tranches,
    //        return all stakes, cancel contribution (partial completion not supported).
    //        Holdback: burn any accumulated holdback (contribution did not complete).
    //     2. BACKED + past reveal_deadline → fail tranche, return stakes,
    //        slash remaining bond (full), burn accumulated holdback, deduct reputation.
    //     3. REVEALED + past verification_deadline → auto-tally votes
    //        (using scaled min_verification_votes);
    //        if pass → confirm + payout (with holdback); if fail → mark DISPUTED
    //     4. DISPUTED + past dispute_resolution deadline → auto-REJECT
    //        (council missed window), slash 50% of remaining bond,
    //        burn accumulated holdback, cancel remaining tranches.
}
```

### Deadline summary

| Phase | Deadline param | Default | Triggered by | On expiry |
|-------|---------------|---------|-------------|-----------|
| Staking | `stake_deadline_epochs` | 60 (~2 mo) | Tranche enters STAKING | Cancel tranche, return stakes, burn holdback |
| Reveal | `reveal_deadline_epochs` | 14 (~2 wk) | Tranche enters BACKED | Fail tranche, return stakes, slash all remaining bond, burn holdback |
| Verification | `verification_period_epochs` | 14 (~2 wk) | Tranche enters REVEALED | Auto-tally; pass or dispute |
| Dispute | `dispute_resolution_epochs` | 30 (~1 mo) | Tranche enters DISPUTED | Auto-REJECT: slash 50% of remaining bond, burn holdback, cancel remaining tranches |

## Withdrawal Rules

Stake withdrawal is time-gated to prevent gaming of verification weights:

| Tranche status | Withdrawal allowed? | Notes |
|---------------|-------------------|-------|
| STAKING | Yes | Freely withdraw at any time |
| BACKED | Yes | Waiting for reveal; contributor may not deliver |
| REVEALED | **No** | Verification in progress; vote weights must be stable |
| VERIFIED | N/A | Stakes auto-returned on payout |
| DISPUTED | **No** | Awaiting council resolution; if IMPROVE verdict, tranche returns to BACKED (withdrawal allowed again) |
| CANCELLED | N/A | Stakes auto-returned by EndBlocker |
| FAILED | N/A | Stakes auto-returned by EndBlocker |

## Contributor Bond

The contributor must lock a bond of `bond_rate * total_valuation` DREAM at proposal time. The `bond_remaining` field tracks the current unslashed balance. This bond:
- Is locked in the module account when `MsgPropose` is submitted (`bond_remaining = bond_amount`)
- Is returned in full when all tranches are verified (COMPLETED)
- Is returned if the council rejects the proposal
- Is **fully slashed** (all of `bond_remaining` burned) if the contributor fails to reveal after a tranche is backed
- Is **50% of `bond_remaining` slashed** (burned) if a dispute verdict is REJECT. The other 50% remains locked for subsequent tranches (if any). ACCEPT and IMPROVE verdicts do not slash the bond.
- Is **returned** (`bond_remaining`) if the Operations Committee cancels the contribution (not contributor's fault)
- Is returned (`bond_remaining`) if the contributor cancels before any tranche reaches BACKED

### Holdback fate

The `holdback_amount` accumulates as tranches are verified (`payout_holdback_rate` × `stake_threshold` per tranche). Its fate depends on how the contribution ends:

| Outcome | Holdback | Rationale |
|---------|----------|-----------|
| All tranches verified (COMPLETED) | **Returned** to contributor | Contributor delivered everything |
| Contributor fails to reveal | **Burned** | Contributor at fault |
| Dispute verdict: ACCEPT | **Preserved** (tranche proceeds to payout, adds to holdback) | Council accepted the code |
| Dispute verdict: IMPROVE | **Preserved** (no change; tranche returns to BACKED) | Code has merit; contributor may fix |
| Dispute verdict: REJECT | **Burned** | Contributor at fault |
| Dispute auto-timeout (council missed window) | **Burned** | Default protects stakers (auto-REJECT) |
| Contributor cancels (no tranche BACKED+) | N/A | No holdback exists yet |
| Committee cancels | **Returned** to contributor | Not contributor's fault |
| Stake deadline expires (tranche not backed) | **Burned** | Contributor couldn't attract conviction |

**Bond + holdback tracking example**: Contributor posts 5,000 DREAM bond. Tranche 0 (15,000) verified → paid 12,000 immediate, 3,000 holdback retained. Dispute on tranche 1 with REJECT verdict → 2,500 bond burned (`bond_remaining` = 2,500), 3,000 holdback burned (`holdback_amount` = 0). Remaining tranches cancelled. (Had the verdict been IMPROVE, no slash — tranche returns to BACKED for re-reveal.)

## Dispute Resolution

When verification fails (< 60% stake-weighted YES or < effective min votes after extension), the tranche enters `DISPUTED`:

1. **Automatic notification**: Event emitted alerting the Operations Committee and Commons Council
2. **Council votes**: An Operations Committee member initiates a `MsgResolveDispute` which is routed as a Commons Council proposal. The full council votes on the resolution, preventing any single member from colluding with the contributor.
3. **Resolution verdicts** (three-way):
   - **ACCEPT**: Code is acceptable. Tranche proceeds to payout (with holdback) as if verified. Council overrules the staker vote.
   - **IMPROVE**: Code has merit but needs work. Tranche returns to BACKED with a new `reveal_deadline`. Contributor may re-reveal improved code. No bond slash. No holdback burn. Stakes remain locked.
   - **REJECT**: Unacceptable or bad faith. Tranche marked FAILED. Stakes returned. **50% of `bond_remaining`** slashed (burned). Accumulated `holdback_amount` burned. Contributor loses reputation. All subsequent LOCKED tranches cancelled. Contribution → CANCELLED.
4. **Timeout**: If the council does not resolve within `dispute_resolution_epochs`, the dispute auto-resolves as **REJECT** (staker protection). 50% of `bond_remaining` is slashed. Accumulated `holdback_amount` burned. Remaining tranches cancelled.
5. **IMPROVE cycle**: After an IMPROVE verdict, the tranche transitions DISPUTED → BACKED → REVEALED → verification as normal. **All previous verification votes for the tranche are deleted** so the next round starts clean (prevents stale votes from non-participating voters corrupting the tally). There is no limit on IMPROVE cycles, but each cycle costs the contributor time and the council review bandwidth. The council may issue REJECT on subsequent disputes if progress is insufficient.

## Events

```protobuf
// Emitted when a new contribution is proposed
message EventContributionProposed {
  uint64 contribution_id = 1;
  string contributor = 2;
  string project_name = 3;
  string total_valuation = 4;
  string bond_amount = 5;
}

// Emitted when council approves a contribution
message EventContributionApproved {
  uint64 contribution_id = 1;
  uint64 council_id = 2;
  string proposed_by = 3;          // Ops Committee member who initiated
}

// Emitted when council rejects a contribution
message EventContributionRejected {
  uint64 contribution_id = 1;
  uint64 council_id = 2;
  string proposed_by = 3;          // Ops Committee member who initiated
  string reason = 4;
  int64 proposal_eligible_at = 5;
}

// Emitted when a contribution is cancelled
message EventContributionCancelled {
  uint64 contribution_id = 1;
  string cancelled_by = 2;
  string reason = 3;
}

// Emitted when all tranches are verified and project is created
message EventContributionCompleted {
  uint64 contribution_id = 1;
  uint64 project_id = 2;
}

// Emitted when a tranche reaches its stake threshold (community has shown conviction)
message EventTrancheBacked {
  uint64 contribution_id = 1;
  uint32 tranche_id = 2;
  string dream_staked = 3;
}

// Emitted when contributor submits code for a tranche
message EventTrancheRevealed {
  uint64 contribution_id = 1;
  uint32 tranche_id = 2;
  string code_uri = 3;
  string commit_hash = 4;
}

// Emitted when verification passes and payout is distributed
message EventTrancheVerified {
  uint64 contribution_id = 1;
  uint32 tranche_id = 2;
  string avg_quality_rating = 3;
  string dream_paid = 4;
}

// Emitted when verification fails and tranche enters dispute
message EventTrancheDisputed {
  uint64 contribution_id = 1;
  uint32 tranche_id = 2;
  string yes_weight = 3;
  string no_weight = 4;
  uint32 vote_count = 5;
}

// Emitted when a tranche is cancelled (deadline expired)
message EventTrancheCancelled {
  uint64 contribution_id = 1;
  uint32 tranche_id = 2;
  string reason = 3;
}

// Emitted when a tranche fails (reveal missed or dispute lost)
message EventTrancheFailed {
  uint64 contribution_id = 1;
  uint32 tranche_id = 2;
  string bond_slashed = 3;
  string reason = 4;
}

// Emitted when a dispute is resolved by council vote
message EventDisputeResolved {
  uint64 contribution_id = 1;
  uint32 tranche_id = 2;
  DisputeVerdict verdict = 3;      // ACCEPT, IMPROVE, or REJECT
  uint64 council_id = 4;
  string proposed_by = 5;          // Ops Committee member who initiated
  string reason = 6;
  string bond_slashed = 7;         // amount slashed (0 for ACCEPT/IMPROVE)
}

// Emitted when a member stakes DREAM toward a tranche
message EventStaked {
  uint64 stake_id = 1;
  string staker = 2;
  uint64 contribution_id = 3;
  uint32 tranche_id = 4;
  string amount = 5;
}

// Emitted when a member withdraws their stake
message EventWithdrawn {
  uint64 stake_id = 1;
  string staker = 2;
  string amount = 3;
}
```

## Default Parameters

```go
var DefaultParams = Params{
    StakeDeadlineEpochs:    60,    // ~2 months
    RevealDeadlineEpochs:     14,    // ~2 weeks after backed
    VerificationPeriodEpochs: 14,    // ~2 weeks
    DisputeResolutionEpochs:  30,    // ~1 month for council

    VerificationThreshold: math.LegacyNewDecWithPrec(60, 2), // 60%
    MinVerificationVotes:  3,        // base minimum; effective = max(3, stake_threshold/5000)

    MaxTranches:         10,
    MaxTrancheValuation: math.NewInt(50000),
    MaxTotalValuation:   math.NewInt(50000),  // valuation cap per contribution

    BondRate: math.LegacyNewDecWithPrec(10, 2), // 10% of total valuation

    MinProposerTrustLevel: 2, // TRUST_LEVEL_ESTABLISHED

    MinStakeAmount:          math.NewInt(100),  // 100 DREAM minimum per stake
    PayoutHoldbackRate:       math.LegacyNewDecWithPrec(20, 2), // 20% held back per tranche
    ProposalCooldownEpochs: 14,               // ~2 weeks after rejection
}
```

## Example: NFT Gallery Contribution

```go
// NOTE: With max_total_valuation = 50000, the tranche breakdown must sum to ≤ 50000.
var NFTGalleryContribution = Contribution{
    Contributor:    "sprkdrm1contributor...",
    ProjectName:    "NFT Gallery",
    Description:    "Comprehensive NFT gallery and collection management system",
    TotalValuation: math.NewInt(50000),
    BondAmount:     math.NewInt(5000),  // 10% of 50000
    BondRemaining:  math.NewInt(5000),  // no slashes yet
    InitialLicense: "Source Available",
    FinalLicense:   "Apache 2.0",
    CouncilID:      1,                  // Commons Council that approved
    HoldbackAmount: math.ZeroInt(),     // accumulates as tranches are verified
    Tranches: []RevealTranche{
        {
            ID:            0,
            Name:          "Foundation",
            Description:   "Core architecture, rendering engine, basic UI",
            Components:    []string{"viewer", "renderer", "metadata-parser", "basic-ui"},
            StakeThreshold: math.NewInt(15000),
            PreviewURI:    "ipfs://Qm.../foundation-demo",
            // Payout: 12000 immediate (80%), 3000 held back (20%)
        },
        {
            ID:            1,
            Name:          "Features",
            Description:   "Collection management, search, multi-chain support",
            Components:    []string{"collections", "search", "filters", "chain-adapters"},
            StakeThreshold: math.NewInt(20000),
            PreviewURI:    "ipfs://Qm.../features-demo",
            // Payout: 16000 immediate, 4000 held back
        },
        {
            ID:            2,
            Name:          "Polish",
            Description:   "Advanced UI, performance optimization, documentation",
            Components:    []string{"advanced-ui", "optimizations", "docs", "deployment"},
            StakeThreshold: math.NewInt(15000),
            PreviewURI:    "ipfs://Qm.../polish-demo",
            // Payout: 12000 immediate, 3000 held back
            // On completion: all 10000 holdback released + 5000 bond returned
        },
    },
    Status: ContributionStatus_InProgress,
}
```

## Genesis State

```protobuf
message GenesisState {
  Params params = 1;
  repeated Contribution contributions = 2;
  repeated RevealStake stakes = 3;
  repeated VerificationVote votes = 4;
  uint64 next_contribution_id = 5;
  uint64 next_stake_id = 6;
}
```

## Cross-Module Integration

### x/reveal → x/rep
```go
// Check membership and trust level
k.repKeeper.GetMember(ctx, address)              // verify active member
k.repKeeper.GetTrustLevel(ctx, address)           // verify sufficient trust

// Mint DREAM on tranche payout
k.repKeeper.MintDream(ctx, contributor, amount)

// Grant reputation on verified tranche (scaled by quality rating)
k.repKeeper.AddReputation(ctx, contributor, tag, score)

// Deduct reputation on failed reveal / lost dispute
k.repKeeper.DeductReputation(ctx, contributor, tag, penalty)

// Create project on transition
k.repKeeper.CreateProject(ctx, project)
```

### x/reveal → x/commons
```go
// Verify Operations Committee membership (initiator must be on committee)
k.commonsKeeper.IsCommitteeMember(ctx, address, council, "operations")

// Route approvals, rejections, and dispute resolutions as Commons Council proposals
// The council votes to approve — prevents single-member collusion
k.commonsKeeper.CreateProposal(ctx, councilID, proposal)

// Verify council proposal passed before executing approval/rejection/dispute resolution
k.commonsKeeper.GetProposalResult(ctx, proposalID)
```

### x/reveal → x/bank
```go
// Lock/return DREAM stakes and bonds via module account
k.bankKeeper.SendCoinsFromAccountToModule(ctx, staker, "reveal", stakeCoins)
k.bankKeeper.SendCoinsFromModuleToAccount(ctx, "reveal", staker, stakeCoins)

// Burn slashed bond
k.bankKeeper.BurnCoins(ctx, "reveal", slashedCoins)
```

## Security Hardening

This section documents the attack vectors addressed by the spec and the mitigations applied.

### Self-Staking / Self-Verification Prevention

**Attack**: A contributor stakes to their own tranches via sybil accounts, then votes to verify their own code — effectively minting DREAM for free.

**Mitigation**: Contributors are explicitly barred from staking on or voting on their own contributions. Enforced at the message handler level (`MsgStake` and `MsgVerify` reject if `sender == contribution.contributor`). This is a hard rule — no governance override.

### Payout Holdback (Abandon-After-Profit Prevention)

**Attack**: With a 10% bond on 50,000 total valuation (5,000 bond), a contributor could complete tranche 0 (15,000 payout), pocket 10,000 net profit, then abandon remaining tranches and forfeit the bond.

**Mitigation**: `payout_holdback_rate` (default 20%) of each tranche payout is retained in the module account until **all** tranches are verified. On completion, the full holdback is released alongside the bond. On abandonment, the holdback is burned along with the bond — making early abandonment unprofitable.

**Example**: 50,000 total, 3 tranches. Tranche 0 pays 15,000 × 80% = 12,000 immediate + 3,000 held. After all 3 tranches: 10,000 holdback released + 5,000 bond returned. If contributor abandons after tranche 0: they received 12,000 but lose 5,000 bond + 3,000 holdback = net 4,000 (vs 15,000 with old spec).

### Council-Gated Approvals (Single-Member Collusion Prevention)

**Attack**: A single Operations Committee member colludes with a contributor — approving inflated valuations or resolving disputes in their favor.

**Mitigation**: All approvals, rejections, and dispute resolutions (ACCEPT/IMPROVE/REJECT) are routed as Commons Council group proposals. The Operations Committee member *initiates* the proposal, but the full council votes on it. This requires majority consensus for any action that moves DREAM or changes tranche state.

### Dust Stake Griefing Prevention

**Attack**: An attacker makes trivial (1 udream) stakes to gain voting rights, then votes NO to force disputes — griefing legitimate contributors with council review overhead.

**Mitigation**: `min_stake_amount` (default 100 DREAM) prevents trivially cheap voting rights. Combined with scaled `min_verification_votes`, larger tranches require proportionally more genuine engagement.

### Overstaking Protection

**Attack**: Stakes exceeding `stake_threshold` lock excess user DREAM until payout, creating unnecessary capital lockup.

**Mitigation**: `MsgStake` rejects any stake that would cause `dream_staked` to exceed `stake_threshold`. If the remaining capacity is less than the stake amount, the stake is rejected (not partially filled — to avoid surprise amounts).

### Valuation Cap

**Attack**: Contributor proposes an absurdly high valuation (e.g., 10 tranches × 50,000 = 500,000 DREAM) that could destabilize the DREAM economy if backed.

**Mitigation**: `max_total_valuation` (default 50,000 DREAM) caps the total valuation per contribution. This is independent of `max_tranche_valuation` — both must be satisfied.

### Re-Proposal Spam Prevention

**Attack**: A rejected contributor immediately re-proposes the same content, wasting council review time.

**Mitigation**: `proposal_cooldown_epochs` (default 14, ~2 weeks) enforced per-contributor. After rejection, `proposal_eligible_at` is set on the contribution, and `MsgPropose` checks that the contributor has no active cooldown.
