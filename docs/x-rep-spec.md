# x/rep Module Specification

## Overview

The `x/rep` module is the core coordination layer for Spark Dream, managing:
- Member lifecycle and trust levels
- DREAM token mechanics
- Reputation scores (per-tag, seasonal)
- Invitation system with accountability
- Projects (budget-backed or permissionless)
- Initiatives (conviction-based, self-selected work)
- Interims (fixed-rate, delegated duties)
- Stakes and time-weighted conviction
- Challenges and jury resolution
- Content staking: community conviction and author bonds (cross-module quality signals)

## Dependencies

| Module | Usage |
|--------|-------|
| `x/auth` | Address codec, account lookups (simulation) |
| `x/bank` | Coin transfers |
| `x/commons` | Council/committee authorization for operations, HR, governance |
| `x/season` | Current season state, display name appeal resolution |
| `x/shield` | Unified privacy layer: anonymous challenges are submitted via `MsgShieldedExec` wrapping `MsgCreateChallenge`. x/shield owns ZK proof verification, nullifier checking, and module-paid gas. x/rep maintains the trust tree (MiMC Merkle tree over member ZK public keys + trust levels) that x/shield uses for proof root validation. |

## State

### Member

```protobuf
message Member {
  string address = 1;

  // DREAM balance
  string dream_balance = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string staked_dream = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string lifetime_earned = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string lifetime_burned = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Reputation (current season)
  map<string, string> reputation_scores = 6; // tag -> score as decimal string

  // Reputation (lifetime archive)
  map<string, string> lifetime_reputation = 7;

  // Trust
  TrustLevel trust_level = 8;
  int64 trust_level_updated_at = 9;

  // Invitation info
  uint32 joined_season = 10;
  int64 joined_at = 11;
  string invited_by = 12;
  repeated string invitation_chain = 13; // ancestors, max 5
  uint32 invitation_credits = 14;

  // Status
  MemberStatus status = 15;
  int64 zeroed_at = 16;
  uint32 zeroed_count = 17;

  // Lazy decay tracking
  int64 last_decay_epoch = 18;

  // Tip rate limiting
  uint32 tips_given_this_epoch = 19;
  int64 last_tip_epoch = 20;

  // Cached counts for performance (avoid full table scans for trust level checks)
  uint32 completed_interims_count = 21;
  uint32 completed_initiatives_count = 22;

  // Gift rate limiting
  string gifts_sent_this_epoch = 23 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 last_gift_epoch = 24;

  // Invitation credit tracking for lazy seasonal reset
  int64 last_credit_reset_season = 25;

  // Per-epoch reputation gain cap tracking
  map<string, string> reputation_gained_this_epoch = 26;
  int64 last_rep_gain_epoch = 27;

  // ZK public key for anonymous operations (trust tree leaf computation).
  // Set via MsgRegisterZkPublicKey. Used by the persistent Merkle tree
  // to build leaves as MiMC(zk_public_key, trust_level).
  bytes zk_public_key = 28;
}

enum TrustLevel {
  TRUST_LEVEL_NEW = 0;
  TRUST_LEVEL_PROVISIONAL = 1;
  TRUST_LEVEL_ESTABLISHED = 2;
  TRUST_LEVEL_TRUSTED = 3;
  TRUST_LEVEL_CORE = 4;
}

enum MemberStatus {
  MEMBER_STATUS_ACTIVE = 0;
  MEMBER_STATUS_INACTIVE = 1;
  MEMBER_STATUS_ZEROED = 2;
}
```

### GiftRecord

Tracks per-recipient cooldown for gift transfers:

```protobuf
message GiftRecord {
  string sender = 1;
  string recipient = 2;
  int64 last_gift_block = 3; // Block height when last gift was sent
}
```

### Invitation

```protobuf
message Invitation {
  uint64 id = 1;
  string inviter = 2;
  string invitee_address = 3;

  string staked_dream = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  repeated string vouched_tags = 5;

  int64 accountability_end = 6;
  string referral_rate = 7 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  int64 referral_end = 8;
  string referral_earned = 9 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  InvitationStatus status = 10;
  int64 created_at = 11;
  int64 accepted_at = 12;
}

enum InvitationStatus {
  INVITATION_STATUS_PENDING = 0;
  INVITATION_STATUS_ACCEPTED = 1;
  INVITATION_STATUS_EXPIRED = 2;
  INVITATION_STATUS_REVOKED = 3;
}
```

### Project

Projects come in two flavors:

- **Budget-backed**: Created via `MsgProposeProject` with `requested_budget > 0` or `requested_spark > 0`. Requires Operations Committee approval before becoming ACTIVE. Initiatives draw from the approved budget allocation.
- **Permissionless**: Created via `MsgProposeProject` with zero budget. Creator burns a protocol fee (`ProjectCreationFee`) and the project becomes ACTIVE immediately — no committee approval needed. Initiatives under permissionless projects are capped at STANDARD tier and their rewards are minted on conviction completion (no pre-allocated budget).

```protobuf
message Project {
  uint64 id = 1;
  string name = 2;
  string description = 3;
  string creator = 4;
  repeated string tags = 5;
  ProjectCategory category = 6;
  string council = 7;

  string approved_budget = 8 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string allocated_budget = 9 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string spent_budget = 10 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string approved_spark = 11 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string spent_spark = 12 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  VerificationPolicy verification_policy = 13;
  ProjectStatus status = 14;
  string approved_by = 15;
  int64 approved_at = 16;
  int64 completed_at = 17;

  // Permissionless projects skip committee approval.
  // Zero budget, APPRENTICE/STANDARD initiatives only, rewards minted on completion.
  bool permissionless = 18;
}

enum ProjectCategory {
  PROJECT_CATEGORY_INFRASTRUCTURE = 0;
  PROJECT_CATEGORY_ECOSYSTEM = 1;
  PROJECT_CATEGORY_CREATIVE = 2;
  PROJECT_CATEGORY_RESEARCH = 3;
  PROJECT_CATEGORY_OPERATIONS = 4;
}

enum ProjectStatus {
  PROJECT_STATUS_PROPOSED = 0;
  PROJECT_STATUS_ACTIVE = 1;
  PROJECT_STATUS_COMPLETED = 2;
  PROJECT_STATUS_CANCELLED = 3;
}

message VerificationPolicy {
  ReviewProcess default_review = 1;
  bool requires_domain_rep = 2;
  string min_verifier_reputation = 3 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  uint32 min_verifier_count = 4;
  int64 review_period_epochs = 5;
  int64 challenge_period_epochs = 6;
  bool requires_creator_approval = 7;
}

enum ReviewProcess {
  REVIEW_PROCESS_CONVICTION_ONLY = 0;
  REVIEW_PROCESS_CREATOR_APPROVAL = 1;
  REVIEW_PROCESS_PEER_REVIEW = 2;
  REVIEW_PROCESS_COMMITTEE_REVIEW = 3;
}
```

### Initiative

Initiatives are project work that any qualified member can claim. Completion is verified through conviction voting (community stakes DREAM to signal confidence in the work).

Under **permissionless projects**, initiatives are capped at STANDARD tier (max 500 DREAM). The creator burns an `InitiativeCreationFee` (scaled by tier) and the budget represents DREAM minted on conviction completion — no pre-allocated project budget is consumed. Under **budget-backed projects**, the existing flow applies: initiative budgets are allocated from the project's approved budget.

```protobuf
message Initiative {
  uint64 id = 1;
  uint64 project_id = 2;
  string title = 3;
  string description = 4;
  repeated string tags = 5;
  InitiativeTier tier = 6;
  InitiativeCategory category = 7;
  string template_id = 8;

  string budget = 9 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  string assignee = 10;
  string apprentice = 11;
  int64 assigned_at = 12;

  string deliverable_uri = 13;
  int64 submitted_at = 14;

  string required_conviction = 15 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string current_conviction = 16 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string external_conviction = 17 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  int64 conviction_last_updated = 18;

  int64 review_period_end = 19;
  int64 challenge_period_end = 20;
  repeated string approvals = 21;

  InitiativeStatus status = 22;
  int64 created_at = 23;
  int64 completed_at = 24;

  string propagated_conviction = 25 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // Conviction propagated from linked content
}

enum InitiativeTier {
  INITIATIVE_TIER_APPRENTICE = 0;
  INITIATIVE_TIER_STANDARD = 1;
  INITIATIVE_TIER_EXPERT = 2;
  INITIATIVE_TIER_EPIC = 3;
}

enum InitiativeCategory {
  INITIATIVE_CATEGORY_FEATURE = 0;
  INITIATIVE_CATEGORY_BUGFIX = 1;
  INITIATIVE_CATEGORY_REFACTOR = 2;
  INITIATIVE_CATEGORY_TESTING = 3;
  INITIATIVE_CATEGORY_SECURITY = 4;
  INITIATIVE_CATEGORY_DOCUMENTATION = 5;
  INITIATIVE_CATEGORY_DESIGN = 6;
  INITIATIVE_CATEGORY_RESEARCH = 7;
  INITIATIVE_CATEGORY_REVIEW = 8;
  INITIATIVE_CATEGORY_OTHER = 9;
}

enum InitiativeStatus {
  INITIATIVE_STATUS_OPEN = 0;
  INITIATIVE_STATUS_ASSIGNED = 1;
  INITIATIVE_STATUS_SUBMITTED = 2;
  INITIATIVE_STATUS_IN_REVIEW = 3;
  INITIATIVE_STATUS_CHALLENGED = 4;
  INITIATIVE_STATUS_COMPLETED = 5;
  INITIATIVE_STATUS_REJECTED = 6;
  INITIATIVE_STATUS_ABANDONED = 7;
}
```

### Interim

Interims are fixed-rate duties delegated to specific members. These include jury duty, expert testimony, administrative reviews, and other governance work. Compensation is based on complexity, not conviction.

```protobuf
message Interim {
  uint64 id = 1;
  InterimType type = 2;

  // Who is responsible
  repeated string assignees = 3;
  string committee = 4; // optional - for committee-level interims

  // Reference to related entity
  uint64 reference_id = 5; // JuryReview ID, Project ID, etc.
  string reference_type = 6; // "jury_review", "project", "contribution", etc.

  // Compensation
  InterimComplexity complexity = 7;
  string budget = 8 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Timing
  int64 deadline = 9;
  int64 created_at = 10;
  int64 completed_at = 11;

  // Status
  InterimStatus status = 12;
  string completion_notes = 13;
}

enum InterimType {
  // Jury and dispute resolution
  INTERIM_TYPE_JURY_DUTY = 0;
  INTERIM_TYPE_EXPERT_TESTIMONY = 1;
  INTERIM_TYPE_DISPUTE_MEDIATION = 2;

  // Administrative reviews
  INTERIM_TYPE_PROJECT_APPROVAL = 3;
  INTERIM_TYPE_BUDGET_REVIEW = 4;
  INTERIM_TYPE_CONTRIBUTION_REVIEW = 5;
  INTERIM_TYPE_EXCEPTION_REQUEST = 6;

  // Verification
  INTERIM_TYPE_TRANCHE_VERIFICATION = 7;

  // Future extensibility
  INTERIM_TYPE_AUDIT = 8;
  INTERIM_TYPE_MODERATION = 9;
  INTERIM_TYPE_MENTORSHIP = 10;
  INTERIM_TYPE_OTHER = 11;

  // Escalation (added after OTHER to preserve numbering)
  INTERIM_TYPE_ADJUDICATION = 12; // Inconclusive jury verdict escalation to committee
}

enum InterimComplexity {
  INTERIM_COMPLEXITY_SIMPLE = 0;    // ~50 DREAM
  INTERIM_COMPLEXITY_STANDARD = 1;  // ~150 DREAM
  INTERIM_COMPLEXITY_COMPLEX = 2;   // ~400 DREAM
  INTERIM_COMPLEXITY_EXPERT = 3;    // ~1000 DREAM
  INTERIM_COMPLEXITY_EPIC = 4;      // ~2500 DREAM (for critical disputes)
}

enum InterimStatus {
  INTERIM_STATUS_PENDING = 0;
  INTERIM_STATUS_IN_PROGRESS = 1;
  INTERIM_STATUS_COMPLETED = 2;
  INTERIM_STATUS_EXPIRED = 3;
  INTERIM_STATUS_ESCALATED = 4;
}
```

### Committee Incentive Structure

Technical committee members are incentivized through a tiered system that compensates routine governance work while ensuring escalated decisions remain principled.

#### Compensated Work (DREAM Rewards)

Committee members receive fixed-rate DREAM compensation for the following interim types:

| Interim Type | Description | Typical Complexity |
|--------------|-------------|-------------------|
| `JURY_DUTY` | Serving on challenge juries | STANDARD (150 DREAM) |
| `EXPERT_TESTIMONY` | Providing expert witness input | COMPLEX (400 DREAM) |
| `PROJECT_APPROVAL` | Reviewing project proposals | STANDARD (150 DREAM) |
| `BUDGET_REVIEW` | Evaluating large budget requests | COMPLEX (400 DREAM) |
| `CONTRIBUTION_REVIEW` | Reviewing founder contributions | STANDARD (150 DREAM) |

**Compensation Budgets by Complexity:**
- Simple: 50 DREAM
- Standard: 150 DREAM
- Complex: 400 DREAM
- Expert: 1,000 DREAM
- Epic: 2,500 DREAM (critical disputes)
- Solo Expert Bonus: +50% (when single assignee handles expert-level work)

#### Uncompensated Work (Civic Duty)

**ADJUDICATION interims do NOT receive DREAM rewards.** This is an intentional design decision.

When a jury review results in an inconclusive verdict, an `INTERIM_TYPE_ADJUDICATION` interim is created and assigned to the Technical Operations Committee. These escalated decisions:
- Have no direct financial compensation
- Rely on committee members' civic responsibility
- Prevent conflicts of interest in high-stakes rulings

```go
// From interim.go - ADJUDICATION interims skip payment
if len(interim.Assignees) > 0 && interim.Type != types.InterimType_INTERIM_TYPE_ADJUDICATION {
    // Only non-ADJUDICATION interims receive DREAM rewards
    paymentPerAssignee := interim.Budget.QuoRaw(int64(len(interim.Assignees)))
    // ... mint DREAM to assignees ...
}
```

#### Indirect Incentives

Committee members also benefit from:

1. **Trust Level Advancement** - Completed interims count toward trust level progression
   - Established requires 10+ completed interims (production)
   - Higher trust unlocks more governance participation

2. **Member Staking Revenue** - Committee members can receive stakes from others
   - 5% of their earnings flow to those who stake on them
   - Being a recognized committee member makes them attractive staking targets

3. **Reputation Building** - Active participation builds community standing

#### Design Rationale

This tiered approach ensures:
- **Routine governance is sustainable** - Members are compensated for regular duties
- **Critical decisions are principled** - Escalated adjudication isn't financially motivated
- **Participation is rewarded** - Trust levels and staking create long-term incentives
- **Accountability is maintained** - Committee membership requires demonstrated commitment

### Stake

Stakes represent locked DREAM committed to targets (initiatives, projects, members, tags, content, or author bonds). Stakes earn rewards through different mechanisms depending on target type.

```protobuf
message Stake {
  uint64 id = 1;
  string staker = 2 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  StakeTargetType target_type = 3;
  uint64 target_id = 4;              // For INITIATIVE/PROJECT/CONTENT/AUTHOR_BOND: the entity ID
  string target_identifier = 5;      // For MEMBER: address; For TAG: tag name
  string amount = 6 [(gogoproto.customtype) = "cosmossdk.io/math.Int", (gogoproto.nullable) = false];
  int64 created_at = 7;
  int64 last_claimed_at = 8;         // Last reward claim timestamp (lazy calculation)
  string reward_debt = 9 [(gogoproto.customtype) = "cosmossdk.io/math.Int", (gogoproto.nullable) = false]; // MasterChef accounting
}

enum StakeTargetType {
  STAKE_TARGET_INITIATIVE = 0;           // Conviction voting, rewards on completion
  STAKE_TARGET_PROJECT = 1;              // APY while active, bonus on completion
  STAKE_TARGET_MEMBER = 2;               // Revenue share from member's earnings
  STAKE_TARGET_TAG = 3;                  // Revenue share from tagged initiatives

  // Content conviction staking (no DREAM rewards, conviction signal only)
  STAKE_TARGET_BLOG_CONTENT = 4;         // Community conviction on x/blog posts/replies
  STAKE_TARGET_FORUM_CONTENT = 5;        // Community conviction on x/forum posts
  STAKE_TARGET_COLLECTION_CONTENT = 6;   // Community conviction on x/collect collections

  // Author bonds (no DREAM rewards, slashable on moderation)
  STAKE_TARGET_BLOG_AUTHOR_BOND = 7;     // Author bond on x/blog content
  STAKE_TARGET_FORUM_AUTHOR_BOND = 8;    // Author bond on x/forum content
  STAKE_TARGET_COLLECTION_AUTHOR_BOND = 9; // Author bond on x/collect content
}
```

### Stake Pools

Pool tracking enables O(1) reward calculations using the MasterChef pattern.

```protobuf
// Tracks aggregate staking on a member
message MemberStakePool {
  string member = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string total_staked = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int", (gogoproto.nullable) = false];
  string pending_revenue = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int", (gogoproto.nullable) = false];
  string acc_reward_per_share = 4 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", (gogoproto.nullable) = false];
  int64 last_updated = 5;
}

// Tracks aggregate staking on a tag
message TagStakePool {
  string tag = 1;
  string total_staked = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int", (gogoproto.nullable) = false];
  string acc_reward_per_share = 3 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", (gogoproto.nullable) = false];
  int64 last_updated = 4;
}

// Tracks project staking totals
message ProjectStakeInfo {
  uint64 project_id = 1;
  string total_staked = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int", (gogoproto.nullable) = false];
  string completion_bonus_pool = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int", (gogoproto.nullable) = false];
}

```

> **Note:** There is no `ContentStakePool` collection. Content conviction is calculated on-demand from individual Stake records with content target types (BLOG_CONTENT, FORUM_CONTENT, COLLECTION_CONTENT). Author bond information is similarly computed from Stake records with author bond target types (BLOG_AUTHOR_BOND, FORUM_AUTHOR_BOND, COLLECTION_AUTHOR_BOND).

### Reward Mechanics by Target Type

| Target | Reward Source | Calculation | Resolution |
|--------|--------------|-------------|------------|
| Initiative | Seasonal reward pool (pro-rata) | `(stake / total_staked) * epoch_reward_slice` | On completion or unstake |
| Project | Seasonal reward pool (pro-rata) + bonus | Pool share while ACTIVE, bonus on completion | On claim or completion |
| Member | Revenue share | `member_earnings * revenue_share_rate` | Accumulated on initiative completion, claimed anytime |
| Tag | Revenue share | `tagged_initiative_earnings * tag_share_rate` | Accumulated per-tag, claimed anytime |
| Blog/Forum/Collection Content | None (conviction only) | Time-weighted conviction score | DREAM returned on unstake after cooldown |
| Blog/Forum/Collection Author Bond | None (signal only) | Flat bond amount (no conviction score) | DREAM returned on unstake, or slashed on moderation |

**Seasonal Reward Pool**: At the start of each season, `MaxStakingRewardsPerSeason` DREAM is allocated as the staking reward budget. Each epoch, `pool / remaining_epochs` is distributed pro-rata to all initiative and project stakers. When the pool is exhausted, no more staking rewards are minted until the next season. This makes effective APY self-adjusting: more total staked DREAM → lower per-unit yield; less staked → higher yield.

**Staked Decay**: All staked DREAM decays at `StakedDecayRate` (0.05%/epoch, ~18% annualized). This ensures idle stakes erode over time even though the rate is lower than unstaked decay (0.2%/epoch). Active stakers earning from the seasonal pool easily outpace the staked decay, but abandoned stakes are gradually burned.

**New Member Grace Period**: Members who joined fewer than `NewMemberDecayGraceEpochs` (30 epochs, ~1 month) ago are exempt from both unstaked and staked decay, giving them time to earn and stake DREAM before decay applies.

### Challenge

```protobuf
message Challenge {
  uint64 id = 1;
  uint64 initiative_id = 2;
  string challenger = 3;

  string reason = 4;
  repeated string evidence = 5;
  string staked_dream = 6 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Fields 7-10 reserved (anonymous challenge fields removed;
  // anonymous challenges are handled entirely by x/shield via MsgShieldedExec)

  ChallengeStatus status = 11;
  int64 created_at = 12;
  int64 resolved_at = 13;
  int64 response_deadline = 14;   // Block height; auto-uphold if assignee doesn't respond
}

enum ChallengeStatus {
  CHALLENGE_STATUS_ACTIVE = 0;
  CHALLENGE_STATUS_IN_JURY_REVIEW = 1;
  CHALLENGE_STATUS_UPHELD = 2;
  CHALLENGE_STATUS_REJECTED = 3;
}
```

> **Note:** Anonymous challenges no longer carry ZK proof fields (`is_anonymous`, `payout_address`, `membership_proof`, `nullifier`) on the Challenge proto. Anonymous challenge submission is handled entirely by x/shield: the challenger submits `MsgShieldedExec` wrapping `MsgCreateChallenge`, and x/shield handles ZK proof verification, nullifier management, and module-paid gas. The resulting Challenge stored in x/rep is structurally identical to a non-anonymous challenge (the `challenger` field is set to x/shield's module address).

### ContentChallenge

ContentChallenge defines a challenge against bonded content (author bonds). Any member can challenge content that has an author bond, routing through the jury system for resolution.

```protobuf
message ContentChallenge {
  uint64 id = 1;

  // Target content identification (author bond type: 7=BLOG, 8=FORUM, 9=COLLECTION)
  StakeTargetType target_type = 2;
  uint64 target_id = 3;

  // Challenger info
  string challenger = 4 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string reason = 5;
  repeated string evidence = 6;
  string staked_dream = 7 [
    (cosmos_proto.scalar) = "cosmos.Int",
    (gogoproto.customtype) = "cosmossdk.io/math.Int",
    (gogoproto.nullable) = false
  ];

  // Content author (resolved from author bond at challenge creation time)
  string author = 8 [(cosmos_proto.scalar) = "cosmos.AddressString"];

  // Status tracking
  ContentChallengeStatus status = 9;
  int64 created_at = 10;   // block height
  int64 resolved_at = 11;  // block height (0 if unresolved)
  int64 response_deadline = 12; // block height
  uint64 jury_review_id = 13;   // 0 if not yet in jury review

  // Author response (set when author responds)
  string author_response = 14;
  repeated string author_evidence = 15;

  // Bond amount snapshot (for reward calculation even after bond removal)
  string bond_amount = 16 [
    (cosmos_proto.scalar) = "cosmos.Int",
    (gogoproto.customtype) = "cosmossdk.io/math.Int",
    (gogoproto.nullable) = false
  ];
}

enum ContentChallengeStatus {
  CONTENT_CHALLENGE_STATUS_ACTIVE = 0;
  CONTENT_CHALLENGE_STATUS_IN_JURY_REVIEW = 1;
  CONTENT_CHALLENGE_STATUS_UPHELD = 2;
  CONTENT_CHALLENGE_STATUS_REJECTED = 3;
}
```

### ContentInitiativeLink

ContentInitiativeLink defines a link between content and an initiative for conviction propagation. Stored in a KeySet indexed by `(initiativeID, (targetType, targetID))`, enabling prefix scan by initiative to find all linked content items. Used in genesis export/import.

```protobuf
message ContentInitiativeLink {
  uint64 initiative_id = 1;
  int32 target_type = 2;  // StakeTargetType (4=BLOG_CONTENT, 5=FORUM_CONTENT)
  uint64 target_id = 3;   // Content ID
}
```

### JuryReview

JuryReview tracks the deliberation process for a challenge. When a JuryReview is created, an Interim with type `JURY_DUTY` is created for each selected juror.

```protobuf
message JuryReview {
  uint64 id = 1;
  uint64 challenge_id = 2;
  uint64 initiative_id = 3;

  repeated string jurors = 4;
  uint32 required_votes = 5;
  repeated string expert_witnesses = 6;
  repeated ExpertTestimony testimonies = 7;

  string review_deliverable = 8;
  string challenger_claim = 9;
  string assignee_response = 10;

  repeated JurorVote votes = 11;
  int64 deadline = 12;
  Verdict verdict = 13;
  string reasoning = 14;
}

message JurorVote {
  string juror = 1;
  repeated CriteriaVote criteria_votes = 2;
  Verdict verdict = 3;
  string confidence = 4 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string reasoning = 5;
  int64 submitted_at = 6;
}

message ExpertTestimony {
  string expert = 1;
  string opinion = 2;
  string reasoning = 3;
  int64 submitted_at = 4;
}

message CriteriaVote {
  string criteria_id = 1;
  bool passed = 2;
  uint32 score = 3;
  string notes = 4;
}

enum Verdict {
  VERDICT_PENDING = 0;
  VERDICT_UPHOLD_CHALLENGE = 1;
  VERDICT_REJECT_CHALLENGE = 2;
  VERDICT_INCONCLUSIVE = 3;
}
```

### InterimTemplate

```protobuf
message InterimTemplate {
  string id = 1;
  string name = 2;
  repeated string tags = 3;
  repeated VerificationCriteria criteria = 4;
  string verification_guide = 5;
}

message VerificationCriteria {
  string id = 1;
  string question = 2;
  CriteriaType type = 3;
  bool required = 4;
  string how_to_verify = 5;
  string evidence = 6;
}

enum CriteriaType {
  CRITERIA_TYPE_BINARY = 0;
  CRITERIA_TYPE_SCALE = 1;
  CRITERIA_TYPE_TEXT = 2;
}
```

## Messages

### Governance Messages

```protobuf
// Full parameter update (x/gov authority only)
message MsgUpdateParams {
  option (cosmos.msg.v1.signer) = "authority";
  string authority = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  Params params = 2 [(gogoproto.nullable) = false];
}

// Operational parameter update (council/committee authority)
message MsgUpdateOperationalParams {
  option (cosmos.msg.v1.signer) = "authority";
  string authority = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  RepOperationalParams operational_params = 2 [(gogoproto.nullable) = false];
}
```

### Member Messages

```protobuf
message MsgInviteMember {
  option (cosmos.msg.v1.signer) = "inviter";
  string inviter = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string invitee_address = 2;
  string staked_dream = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  repeated string vouched_tags = 4;
}

message MsgAcceptInvitation {
  option (cosmos.msg.v1.signer) = "invitee";
  string invitee = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 invitation_id = 2;
}

message MsgTransferDream {
  option (cosmos.msg.v1.signer) = "sender";
  string sender = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string recipient = 2;
  string amount = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  TransferPurpose purpose = 4;
  string reference = 5;
}

enum TransferPurpose {
  TRANSFER_PURPOSE_TIP = 0;
  TRANSFER_PURPOSE_GIFT = 1;
  TRANSFER_PURPOSE_BOUNTY = 2;
}

// Register a ZK public key for anonymous operations.
// The key is stored on the Member proto (field 28) and used by the trust tree
// to build leaves as MiMC(zk_public_key, trust_level). Once registered,
// the member can participate in anonymous operations via x/shield's MsgShieldedExec.
message MsgRegisterZkPublicKey {
  option (cosmos.msg.v1.signer) = "member";
  string member = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  bytes zk_public_key = 2;
}
```

### Project Messages

```protobuf
message MsgProposeProject {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string name = 2;
  string description = 3;
  repeated string tags = 4;
  ProjectCategory category = 5;
  string council = 6;
  string requested_budget = 7 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string requested_spark = 8 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  repeated string deliverables = 9;
  repeated string milestones = 10;
}

message MsgApproveProjectBudget {
  option (cosmos.msg.v1.signer) = "approver";
  string approver = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 project_id = 2;
  string approved_budget = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string approved_spark = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

message MsgCancelProject {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 project_id = 2;
  string reason = 3;
}
```

#### Permissionless Project Creation Flow

`MsgProposeProject` supports two paths based on the requested budget:

| Condition | Path | Result |
|-----------|------|--------|
| `requested_budget == 0` AND `requested_spark == 0` | **Permissionless** | ACTIVE immediately |
| `requested_budget > 0` OR `requested_spark > 0` | **Budget-backed** | PROPOSED → awaits `MsgApproveProjectBudget` |

**Permissionless path** handler logic:
1. Validate creator is ESTABLISHED+ trust level (`ErrInsufficientTrustLevel`)
2. Burn `ProjectCreationFee` DREAM from creator's balance (`ErrInsufficientBalance` if short)
3. Create project with `permissionless = true`, `status = ACTIVE`, all budget fields zero
4. No `PROJECT_APPROVAL` interim is created — committee is not involved
5. Emit `project_created` event (distinct from `project_proposed`)

**Budget-backed path** (existing behavior, unchanged):
1. Validate creator is a member
2. Create project with `status = PROPOSED`
3. Trigger `PROJECT_APPROVAL` interim for Operations Committee
4. Await `MsgApproveProjectBudget` → transitions to ACTIVE

### Initiative Messages

```protobuf
message MsgCreateInitiative {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 project_id = 2;
  string title = 3;
  string description = 4;
  uint64 tier = 5;
  uint64 category = 6;
  string template_id = 7;
  repeated string tags = 8;
  string budget = 9 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

message MsgAssignInitiative {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 initiative_id = 2;
  string assignee = 3 [(cosmos_proto.scalar) = "cosmos.AddressString"];
}

message MsgSubmitInitiativeWork {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 initiative_id = 2;
  string deliverable_uri = 3;
  string comments = 4;
}

message MsgApproveInitiative {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 initiative_id = 2;
  repeated CriteriaVote criteria_votes = 3;
  bool approved = 4;
  string comments = 5;
}

message MsgAbandonInitiative {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 initiative_id = 2;
  string reason = 3;
}

message MsgCompleteInitiative {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 initiative_id = 2;
  string completion_notes = 3;
}
```

#### Initiative Creation Under Permissionless Projects

`MsgCreateInitiative` branches based on the parent project type:

| Parent Project | Allowed Tiers | Budget Source | Fee |
|----------------|---------------|---------------|-----|
| Budget-backed | All tiers | Allocated from project budget | None |
| Permissionless | APPRENTICE, STANDARD only | Minted on conviction completion | `InitiativeCreationFee` burned |

**Permissionless path** handler logic:
1. Validate parent project is ACTIVE and `permissionless == true`
2. Validate tier is APPRENTICE or STANDARD (`ErrPermissionlessTierExceeded`)
3. Validate creator trust level: PROVISIONAL+ for APPRENTICE, ESTABLISHED+ for STANDARD
4. Burn `InitiativeCreationFee` DREAM from creator (fee scaled by tier — see params)
5. Skip `AllocateBudget` (no project budget to draw from)
6. Create initiative normally — budget represents DREAM minted on conviction completion
7. Conviction threshold, challenge period, and completion flow are identical to budget-backed initiatives

**Budget-backed path** (existing behavior, unchanged):
1. Validate parent project is ACTIVE
2. Validate tier budget limits
3. Allocate budget from project's approved budget
4. Create initiative

### Interim Messages

```protobuf
message MsgCreateInterim {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  InterimType interim_type = 2;
  uint64 reference_id = 3;
  string reference_type = 4;
  InterimComplexity complexity = 5;
  int64 deadline = 6;
}

message MsgAssignInterim {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 interim_id = 2;
  string assignee = 3 [(cosmos_proto.scalar) = "cosmos.AddressString"];
}

message MsgSubmitInterimWork {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 interim_id = 2;
  string deliverable_uri = 3;
  string comments = 4;
}

message MsgApproveInterim {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 interim_id = 2;
  bool approved = 3;
  string comments = 4;
}

message MsgAbandonInterim {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 interim_id = 2;
  string reason = 3;
}

message MsgCompleteInterim {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 interim_id = 2;
  string completion_notes = 3;
}
```

### Stake Messages

Content conviction staking uses the same `MsgStake`/`MsgUnstake` with module-specific target types (`STAKE_TARGET_BLOG_CONTENT`, `STAKE_TARGET_FORUM_CONTENT`, `STAKE_TARGET_COLLECTION_CONTENT`) and `target_id` set to the content item's ID. Author bonds use `STAKE_TARGET_BLOG_AUTHOR_BOND`, `STAKE_TARGET_FORUM_AUTHOR_BOND`, or `STAKE_TARGET_COLLECTION_AUTHOR_BOND`. Author bonds are created via keeper methods (called by content modules during content creation) and released via `MsgUnstake`.

```protobuf
message MsgStake {
  option (cosmos.msg.v1.signer) = "staker";
  string staker = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  StakeTargetType target_type = 2;
  uint64 target_id = 3;             // For INITIATIVE/PROJECT/CONTENT/AUTHOR_BOND
  string target_identifier = 4;     // For MEMBER (address) or TAG (name)
  string amount = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

message MsgStakeResponse {
  uint64 stake_id = 1;
}

message MsgUnstake {
  option (cosmos.msg.v1.signer) = "staker";
  string staker = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 stake_id = 2;
  string amount = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

message MsgUnstakeResponse {
  string returned_amount = 1 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string reward_amount = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

message MsgClaimStakingRewards {
  option (cosmos.msg.v1.signer) = "staker";
  string staker = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 stake_id = 2;
}

message MsgClaimStakingRewardsResponse {
  string claimed_amount = 1 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

message MsgCompoundStakingRewards {
  option (cosmos.msg.v1.signer) = "staker";
  string staker = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 stake_id = 2;
}

message MsgCompoundStakingRewardsResponse {
  string compounded_amount = 1 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string new_stake_amount = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}
```

### Challenge Messages

```protobuf
message MsgCreateChallenge {
  option (cosmos.msg.v1.signer) = "challenger";
  string challenger = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 initiative_id = 2;
  string reason = 3;
  repeated string evidence = 4;
  string staked_dream = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}

message MsgRespondToChallenge {
  option (cosmos.msg.v1.signer) = "assignee";
  string assignee = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 challenge_id = 2;
  string response = 3;
  repeated string evidence = 4;
}

message MsgSubmitJurorVote {
  option (cosmos.msg.v1.signer) = "juror";
  string juror = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 jury_review_id = 2;
  repeated CriteriaVote criteria_votes = 3;
  Verdict verdict = 4;
  string confidence = 5 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string reasoning = 6;
}

message MsgSubmitExpertTestimony {
  option (cosmos.msg.v1.signer) = "expert";
  string expert = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 jury_review_id = 2;
  string opinion = 3;
  string reasoning = 4;
}
```

#### Anonymous Challenge Flow (via x/shield)

Anonymous challenges are not submitted directly to x/rep with per-module ZK proof verification. Instead, they go through x/shield's unified privacy layer:

1. **Submission**: The challenger submits `MsgShieldedExec` to x/shield, wrapping a standard `MsgCreateChallenge`. x/shield handles ZK proof verification (PLONK over BN254), nullifier checking (domain 41, GLOBAL scope), and module-paid gas.
2. **Proof verification**: x/shield verifies the ZK proof against the trust tree root maintained by x/rep (`GetTrustTreeRoot()`). The proof demonstrates membership and sufficient trust level without revealing the challenger's identity.
3. **Execution**: x/shield unwraps and dispatches the inner `MsgCreateChallenge` to x/rep's message server. The `challenger` field is set to x/shield's module address (not the real challenger).
4. **Batch mode**: Anonymous challenges use ENCRYPTED_ONLY batch mode -- the inner message is TLE-encrypted and only decrypted/executed after epoch key revelation, providing maximum privacy.

x/rep's `IsShieldCompatible()` method (in `shield_aware.go`) identifies `MsgCreateChallenge` as eligible for shielded execution.

### Content Challenge Messages

```protobuf
message MsgChallengeContent {
  option (cosmos.msg.v1.signer) = "challenger";
  string challenger = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 target_type = 2; // StakeTargetType (7=BLOG_AUTHOR_BOND, 8=FORUM_AUTHOR_BOND, 9=COLLECTION_AUTHOR_BOND)
  uint64 target_id = 3;
  string reason = 4;
  repeated string evidence = 5;
  string staked_dream = 6 [
    (cosmos_proto.scalar) = "cosmos.Int",
    (gogoproto.customtype) = "cosmossdk.io/math.Int",
    (gogoproto.nullable) = true
  ];
}

message MsgChallengeContentResponse {
  uint64 content_challenge_id = 1;
}

message MsgRespondToContentChallenge {
  option (cosmos.msg.v1.signer) = "author";
  string author = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 content_challenge_id = 2;
  string response = 3;
  repeated string evidence = 4;
}

message MsgRespondToContentChallengeResponse {}
```

## Queries

```protobuf
service Query {
  // Params
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);

  // Members
  rpc GetMember(QueryGetMemberRequest) returns (QueryGetMemberResponse);
  rpc ListMember(QueryAllMemberRequest) returns (QueryAllMemberResponse);
  rpc MembersByTrustLevel(QueryMembersByTrustLevelRequest) returns (QueryMembersByTrustLevelResponse);

  // Invitations
  rpc GetInvitation(QueryGetInvitationRequest) returns (QueryGetInvitationResponse);
  rpc ListInvitation(QueryAllInvitationRequest) returns (QueryAllInvitationResponse);
  rpc InvitationsByInviter(QueryInvitationsByInviterRequest) returns (QueryInvitationsByInviterResponse);

  // Projects
  rpc GetProject(QueryGetProjectRequest) returns (QueryGetProjectResponse);
  rpc ListProject(QueryAllProjectRequest) returns (QueryAllProjectResponse);
  rpc ProjectsByCouncil(QueryProjectsByCouncilRequest) returns (QueryProjectsByCouncilResponse);

  // Initiatives
  rpc GetInitiative(QueryGetInitiativeRequest) returns (QueryGetInitiativeResponse);
  rpc ListInitiative(QueryAllInitiativeRequest) returns (QueryAllInitiativeResponse);
  rpc InitiativesByProject(QueryInitiativesByProjectRequest) returns (QueryInitiativesByProjectResponse);
  rpc InitiativesByAssignee(QueryInitiativesByAssigneeRequest) returns (QueryInitiativesByAssigneeResponse);
  rpc AvailableInitiatives(QueryAvailableInitiativesRequest) returns (QueryAvailableInitiativesResponse);
  rpc InitiativeConviction(QueryInitiativeConvictionRequest) returns (QueryInitiativeConvictionResponse);

  // Interims
  rpc GetInterim(QueryGetInterimRequest) returns (QueryGetInterimResponse);
  rpc ListInterim(QueryAllInterimRequest) returns (QueryAllInterimResponse);
  rpc GetInterimTemplate(QueryGetInterimTemplateRequest) returns (QueryGetInterimTemplateResponse);
  rpc ListInterimTemplate(QueryAllInterimTemplateRequest) returns (QueryAllInterimTemplateResponse);
  rpc InterimsByAssignee(QueryInterimsByAssigneeRequest) returns (QueryInterimsByAssigneeResponse);
  rpc InterimsByType(QueryInterimsByTypeRequest) returns (QueryInterimsByTypeResponse);
  rpc InterimsByReference(QueryInterimsByReferenceRequest) returns (QueryInterimsByReferenceResponse);

  // Stakes
  rpc GetStake(QueryGetStakeRequest) returns (QueryGetStakeResponse);
  rpc ListStake(QueryAllStakeRequest) returns (QueryAllStakeResponse);
  rpc StakesByStaker(QueryStakesByStakerRequest) returns (QueryStakesByStakerResponse);
  rpc StakesByTarget(QueryStakesByTargetRequest) returns (QueryStakesByTargetResponse);
  rpc PendingStakeRewards(QueryPendingStakeRewardsRequest) returns (QueryPendingStakeRewardsResponse);
  rpc GetMemberStakePool(QueryGetMemberStakePoolRequest) returns (QueryGetMemberStakePoolResponse);
  rpc GetTagStakePool(QueryGetTagStakePoolRequest) returns (QueryGetTagStakePoolResponse);
  rpc GetProjectStakeInfo(QueryGetProjectStakeInfoRequest) returns (QueryGetProjectStakeInfoResponse);

  // Content Staking
  rpc ContentConviction(QueryContentConvictionRequest) returns (QueryContentConvictionResponse);
  rpc AuthorBond(QueryAuthorBondRequest) returns (QueryAuthorBondResponse);

  // Challenges
  rpc GetChallenge(QueryGetChallengeRequest) returns (QueryGetChallengeResponse);
  rpc ListChallenge(QueryAllChallengeRequest) returns (QueryAllChallengeResponse);
  rpc ChallengesByInitiative(QueryChallengesByInitiativeRequest) returns (QueryChallengesByInitiativeResponse);

  // Content Challenges
  rpc GetContentChallenge(QueryGetContentChallengeRequest) returns (QueryGetContentChallengeResponse);
  rpc ListContentChallenge(QueryAllContentChallengeRequest) returns (QueryAllContentChallengeResponse);
  rpc ContentChallengesByTarget(QueryContentChallengesByTargetRequest) returns (QueryContentChallengesByTargetResponse);

  // Content-Initiative Links
  rpc ContentByInitiative(QueryContentByInitiativeRequest) returns (QueryContentByInitiativeResponse);

  // Jury Reviews
  rpc GetJuryReview(QueryGetJuryReviewRequest) returns (QueryGetJuryReviewResponse);
  rpc ListJuryReview(QueryAllJuryReviewRequest) returns (QueryAllJuryReviewResponse);

  // Reputation
  rpc Reputation(QueryReputationRequest) returns (QueryReputationResponse);

  // Economic Health (governance monitoring)
  rpc DreamSupplyStats(QueryDreamSupplyStatsRequest) returns (QueryDreamSupplyStatsResponse);
  rpc MintBurnRatio(QueryMintBurnRatioRequest) returns (QueryMintBurnRatioResponse);
  rpc EffectiveApy(QueryEffectiveApyRequest) returns (QueryEffectiveApyResponse);
  rpc TreasuryStatus(QueryTreasuryStatusRequest) returns (QueryTreasuryStatusResponse);
}

// Economic health query messages
message QueryDreamSupplyStatsRequest {}
message QueryDreamSupplyStatsResponse {
  string total_minted = 1 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];     // all-time minted
  string total_burned = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];     // all-time burned
  string circulating = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];      // member balances
  string total_staked = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];     // locked in stakes
  string treasury_balance = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"]; // module treasury
  string staked_ratio = 6 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // staked / circulating
}

message QueryMintBurnRatioRequest {}
message QueryMintBurnRatioResponse {
  string season_minted = 1 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string season_burned = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string ratio = 3 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // minted/burned (>1 = inflationary)
  uint32 season = 4;
}

message QueryEffectiveApyRequest {}
message QueryEffectiveApyResponse {
  string seasonal_pool_total = 1 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string seasonal_pool_remaining = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string total_staked = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string effective_apy = 4 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // pool_remaining / total_staked annualized
}

message QueryTreasuryStatusRequest {}
message QueryTreasuryStatusResponse {
  string balance = 1 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string max_balance = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string season_inflow = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];   // initiative treasury shares this season
  string season_outflow = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];  // interims + retro PGF funded from treasury
  string season_burned = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];   // excess burned
}
```

## Expected Keeper Interfaces

```go
// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
    AddressCodec() address.Codec
    GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
    SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
    SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
    SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
    BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// CommonsKeeper defines the expected interface for the Commons module.
type CommonsKeeper interface {
    IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)
    GetCommitteeGroupInfo(ctx context.Context, council string, committee string) (interface{}, error)
    IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
}

// SeasonKeeper defines the expected interface for the Season module.
type SeasonKeeper interface {
    GetCurrentSeason(ctx context.Context) (seasontypes.Season, error)
    ResolveDisplayNameAppealInternal(ctx context.Context, member string, appealSucceeded bool) error
}

// Note: x/rep no longer depends on a VoteKeeper. Anonymous challenge submission
// (ZK proof verification, nullifier checking, module-paid gas) is handled entirely
// by x/shield via MsgShieldedExec. x/rep exports trust tree roots for x/shield to
// use during proof verification — see GetTrustTreeRoot() and GetPreviousTrustTreeRoot()
// in merkle_trees.go.
```

## Keeper Struct

```go
type Keeper struct {
    // Core
    storeService corestore.KVStoreService
    cdc          codec.Codec
    addressCodec address.Codec
    authority    []byte

    // Schema and Params
    Schema collections.Schema
    Params collections.Item[types.Params]

    // External keepers
    authKeeper    types.AuthKeeper
    bankKeeper    types.BankKeeper
    commonsKeeper types.CommonsKeeper
    late          *lateKeepers // shared across value copies (tagKeeper, seasonKeeper)

    // Primary collections
    Member          collections.Map[string, types.Member]
    InvitationSeq   collections.Sequence
    Invitation      collections.Map[uint64, types.Invitation]
    ProjectSeq      collections.Sequence
    Project         collections.Map[uint64, types.Project]
    InitiativeSeq   collections.Sequence
    Initiative      collections.Map[uint64, types.Initiative]
    StakeSeq        collections.Sequence
    Stake           collections.Map[uint64, types.Stake]
    ChallengeSeq    collections.Sequence
    Challenge       collections.Map[uint64, types.Challenge]
    JuryReviewSeq   collections.Sequence
    JuryReview      collections.Map[uint64, types.JuryReview]
    InterimSeq      collections.Sequence
    Interim         collections.Map[uint64, types.Interim]
    InterimTemplate collections.Map[string, types.InterimTemplate]
    GiftRecord      collections.Map[collections.Pair[string, string], types.GiftRecord]

    // Secondary indexes (avoid full table scans in EndBlocker)
    InitiativesByStatus  collections.KeySet[collections.Pair[int32, uint64]]    // (status, id)
    InterimsByStatus     collections.KeySet[collections.Pair[int32, uint64]]    // (status, id)
    JuryReviewsByVerdict collections.KeySet[collections.Pair[int32, uint64]]    // (verdict, id)
    StakesByTarget       collections.KeySet[collections.Triple[int32, uint64, uint64]] // (targetType, targetID, stakeID)
    ChallengesByStatus   collections.KeySet[collections.Pair[int32, uint64]]    // (status, id)

    // Extended staking pools (O(1) reward distribution)
    MemberStakePool  collections.Map[string, types.MemberStakePool]  // member address -> pool
    TagStakePool     collections.Map[string, types.TagStakePool]     // tag name -> pool
    ProjectStakeInfo collections.Map[uint64, types.ProjectStakeInfo] // project ID -> info

    // Content challenges
    ContentChallengeSeq       collections.Sequence
    ContentChallenge          collections.Map[uint64, types.ContentChallenge]
    ContentChallengesByStatus collections.KeySet[collections.Pair[int32, uint64]]
    // (targetType, targetID) -> challengeID -- enforces one active challenge per content item
    ContentChallengesByTarget collections.Map[collections.Pair[int32, uint64], uint64]

    // Content-initiative links for conviction propagation
    // Key: (initiativeID, (targetType, targetID)) -- enables prefix scan by initiative
    ContentInitiativeLinks collections.KeySet[collections.Pair[uint64, collections.Pair[int32, uint64]]]
}
```

## Permissionless Creation Model

### Design Rationale

Committee approval is essential when treasury funds are at stake — it prevents budget abuse and ensures resource allocation aligns with council priorities. But for organic, self-funded work, the approval step becomes a participation tax: members with ideas must wait for committee review before they can even start coordinating.

The permissionless model removes this bottleneck for zero-budget work. Members burn a protocol fee (anti-spam) and the conviction mechanism handles quality control — if nobody stakes on the work, it never completes and no DREAM gets minted. The committee's attention is reserved for budget allocation decisions where it adds real value.

### Security Properties

- **Anti-spam**: Creation fees are burned, making spam directly costly to the spammer and deflationary for everyone else
- **Tier cap**: Permissionless initiatives are capped at STANDARD (500 DREAM max reward), limiting the maximum DREAM that can be minted without committee oversight
- **Trust gate**: Only ESTABLISHED+ members (200+ rep, 10+ interims) can create permissionless projects, filtering out new or unproven accounts
- **Conviction filter**: Even with a permissionless project, initiatives still require community conviction to complete — the community votes with its DREAM
- **Challenge period**: All initiatives (permissionless or not) pass through the standard challenge period before completion
- **No treasury exposure**: Permissionless projects have zero budget — no pre-allocated funds can be misused

### Summary Table

| Dimension | Budget-backed | Permissionless |
|-----------|---------------|----------------|
| Creation gate | Any member | ESTABLISHED+ trust level |
| Approval | Operations Committee | None (fee burned) |
| Project budget | Committee-approved amount | Zero |
| Initiative tiers | All (APPRENTICE → EPIC) | APPRENTICE and STANDARD only |
| Initiative budget source | Allocated from project | Minted on conviction completion |
| Initiative creation fee | None | 1 DREAM (apprentice), 3 DREAM (standard) |
| Max minting per initiative | 10,000 DREAM (EPIC) | 500 DREAM (STANDARD) |
| Conviction/challenge flow | Standard | Identical |

## Interim Creation Triggers

Interims are created automatically by the system in response to events:

| Trigger | Interim Type | Assignees | Reference |
|---------|-------------------|-----------|-----------|
| JuryReview created | JURY_DUTY | Selected jurors | JuryReview ID |
| Expert requested | EXPERT_TESTIMONY | Invited expert | JuryReview ID |
| Budget-backed project proposed | PROJECT_APPROVAL | Operations committee | Project ID |
| Large budget request | BUDGET_REVIEW | Operations committee | Project ID |
| *(Permissionless projects skip PROJECT_APPROVAL — no interim created)* | | | |
| Tranche revealed | TRANCHE_VERIFICATION | Tranche stakers | Tranche ID |
| Founder contribution proposed | CONTRIBUTION_REVIEW | Operations committee | Contribution ID |
| Dispute filed | DISPUTE_MEDIATION | Assigned mediator | Dispute ID |
| Inconclusive jury verdict | ADJUDICATION | Technical operations committee | Challenge ID |

## EndBlocker Logic

Located in `x/rep/keeper/abci.go`:

```go
func (k Keeper) EndBlocker(ctx context.Context) error {
    // 1. Update conviction for all active initiative stakes
    k.IterateActiveInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
        _ = k.UpdateInitiativeConviction(ctx, initiative.Id)
        return false
    })

    // 2. Check initiative completion thresholds
    k.IterateSubmittedInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
        canComplete, err := k.CanCompleteInitiative(ctx, initiative.Id)
        if err == nil && canComplete {
            _ = k.TransitionToChallengePeriod(ctx, initiative.Id)
        }
        return false
    })

    // 3. Finalize unchallenged initiatives
    k.IteratePendingCompletionInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
        if sdkCtx.BlockHeight() >= initiative.ChallengePeriodEnd {
            _ = k.CompleteInitiative(ctx, initiative.Id)
        }
        return false
    })

    // 4. DREAM decay (unstaked AND staked) is applied lazily via GetMember/GetBalance
    // Unstaked decay: 0.2%/epoch. Staked decay: 0.05%/epoch.
    // New members (joined < NewMemberDecayGraceEpochs ago) are exempt from both.
    // No bulk decay — calculated on-demand when members are accessed.
    // Scales O(1) per block instead of O(n) where n = member count

    // 5. Process expired challenge responses
    // If assignee doesn't respond within deadline, challenge is auto-upheld
    k.IterateActiveChallenges(ctx, func(index int64, challenge types.Challenge) bool {
        if challenge.ResponseDeadline > 0 && sdkCtx.BlockHeight() >= challenge.ResponseDeadline {
            _ = k.UpholdChallenge(ctx, challenge.Id)
        }
        return false
    })

    // 6. Process jury review deadlines
    k.IterateActiveJuryReviews(ctx, func(index int64, review types.JuryReview) bool {
        if sdkCtx.BlockHeight() >= review.Deadline {
            _ = k.TallyJuryVotes(ctx, review.Id)
        }
        return false
    })

    // 7. Process assigned initiative deadlines (interims)
    k.IteratePendingInterims(ctx, func(index int64, interim types.Interim) bool {
        if sdkCtx.BlockHeight() >= interim.Deadline {
            _ = k.ExpireInterim(ctx, interim.Id)
        }
        return false
    })

    // 8. Distribute staking rewards from seasonal pool
    // Rewards are pro-rata from MaxStakingRewardsPerSeason, split across
    // all epochs in the season. Once the pool is exhausted, no more rewards
    // are minted until the next season. Effective APY is self-adjusting:
    // more staked → lower per-unit yield; less staked → higher yield.
    k.DistributeEpochStakingRewards(ctx)

    // 9. Treasury overflow check (once per epoch boundary)
    // If treasury balance > MaxTreasuryBalance, excess DREAM is burned.
    k.EnforceTreasuryBalance(ctx)

    // 10. Trust levels are updated lazily at trigger points:
    //     - When a member completes an interim (reputation gained)
    //     - When reputation is granted/reduced
    //     - When a new season starts
    // Scales O(1) per block instead of O(n*m)

    // 11. Process invitation accountability
    k.ProcessExpiredAccountability(ctx)

    // 12. Invitation credits are reset lazily via EnsureInvitationCreditsReset
    // When a member tries to invite, credits are reset if current season > last reset season
    // Scales O(1) per block instead of O(n)

    // 12. Rebuild trust tree incrementally if any members are dirty
    // Only updates leaves for members whose ZK public key or trust level changed
    k.MaybeRebuildTrustTree(ctx)

    return nil
}
```

## Trust Tree (MiMC Merkle Tree)

x/rep maintains a persistent KV-based sparse Merkle tree used by x/shield for ZK proof verification. This is the **trust tree** — a binary tree where each leaf represents a member's anonymous identity and trust level.

### Leaf Construction

Each leaf is computed as:

```
leaf = MiMC(zk_public_key, trust_level)
```

where `zk_public_key` is the member's registered ZK public key (field 28 on Member proto) and `trust_level` is their current trust level as an integer (0-4). Members without a registered ZK public key are excluded from the tree.

### Tree Structure

- **Depth**: 20 (supports up to 2^20 = ~1M members)
- **Hash function**: MiMC (BN254-compatible, same as the ZK circuits)
- **Storage**: Persistent KV store with prefixed keys for nodes, member-to-index mappings, and dirty member tracking
- **Root**: Stored at `trust_tree/root`, previous root at `trust_tree/prev_root`

### Incremental Updates

The tree is rebuilt incrementally in EndBlocker via `MaybeRebuildTrustTree()`:

1. **Dirty tracking**: When a member's ZK public key or trust level changes, they are marked dirty via `MarkMemberDirty(ctx, address)`. This is O(1) per change.
2. **Batch update**: EndBlocker iterates only dirty members, recomputes their leaf hash, and updates the affected path from leaf to root. This is O(dirty_count * tree_depth) per block.
3. **Previous root preserved**: Before updating, the current root is saved as `previous_root`. x/shield accepts proofs against either the current or previous root (handles race conditions where a proof was generated against a slightly stale root).
4. **Full rebuild**: On genesis import or upgrade, a full rebuild flag triggers recomputation of all leaves.

### Exported API (used by x/shield)

```go
// GetTrustTreeRoot returns the current trust tree Merkle root.
// Used by x/shield for ZK proof root validation (PROOF_DOMAIN_TRUST_TREE).
func (k Keeper) GetTrustTreeRoot(ctx context.Context) ([]byte, error)

// GetPreviousTrustTreeRoot returns the previous trust tree Merkle root.
// Used by x/shield to accept proofs generated against slightly stale roots.
func (k Keeper) GetPreviousTrustTreeRoot(ctx context.Context) ([]byte, error)
```

These are thin wrappers (in `merkle_trees.go`) over the underlying `GetMemberTrustTreeRoot()` and `GetPreviousMemberTrustTreeRoot()` methods in `trust_tree.go`.

### Shield Compatibility

x/rep implements the `IsShieldCompatible()` method (in `shield_aware.go`) which identifies `MsgCreateChallenge` as eligible for shielded execution. This allows x/shield to route anonymous challenges through x/rep's message server.

## Genesis State

```protobuf
message GenesisState {
  Params params = 1 [(gogoproto.nullable) = false];
  repeated Member member_map = 2 [(gogoproto.nullable) = false];
  repeated Invitation invitation_list = 3 [(gogoproto.nullable) = false];
  uint64 invitation_count = 4;
  repeated Project project_list = 5 [(gogoproto.nullable) = false];
  uint64 project_count = 6;
  repeated Initiative initiative_list = 7 [(gogoproto.nullable) = false];
  uint64 initiative_count = 8;
  repeated Stake stake_list = 9 [(gogoproto.nullable) = false];
  uint64 stake_count = 10;
  repeated Challenge challenge_list = 11 [(gogoproto.nullable) = false];
  uint64 challenge_count = 12;
  repeated JuryReview jury_review_list = 13 [(gogoproto.nullable) = false];
  uint64 jury_review_count = 14;
  repeated Interim interim_list = 15 [(gogoproto.nullable) = false];
  uint64 interim_count = 16;
  repeated InterimTemplate interim_template_map = 17 [(gogoproto.nullable) = false];

  // Stake pools
  repeated MemberStakePool member_stake_pool_list = 18 [(gogoproto.nullable) = false];
  repeated TagStakePool tag_stake_pool_list = 19 [(gogoproto.nullable) = false];
  repeated ProjectStakeInfo project_stake_info_list = 20 [(gogoproto.nullable) = false];

  // Content challenges
  repeated ContentChallenge content_challenge_list = 21 [(gogoproto.nullable) = false];
  uint64 content_challenge_count = 22;

  // Content initiative links for conviction propagation
  repeated ContentInitiativeLink content_initiative_links = 23 [(gogoproto.nullable) = false];
}
```

## Default Parameters

All `math.Int` values are in **micro-DREAM** (1 DREAM = 1,000,000 micro-DREAM) unless noted.

```go
var DefaultParams = Params{
    // Time
    EpochBlocks:          14400, // ~1 day (14400 blocks * 6s = 86400s)
    SeasonDurationEpochs: 150,   // ~5 months (150 days)

    // DREAM economics
    MaxStakingRewardsPerSeason: math.NewInt(25_000_000_000_000), // 25,000 DREAM seasonal pool
    // Effective APY = MaxStakingRewardsPerSeason / total_staked (self-adjusting)
    // No fixed StakingApy — replaced by seasonal pool to cap inflationary minting
    UnstakedDecayRate:       math.LegacyNewDecWithPrec(2, 3),    // 0.2% per epoch (~73% annualized)
    StakedDecayRate:         math.LegacyNewDecWithPrec(5, 4),    // 0.05% per epoch (~18% annualized)
    NewMemberDecayGraceEpochs: 30,                                // ~1 month grace period (no decay)
    TransferTaxRate:         math.LegacyNewDecWithPrec(3, 2),    // 3%
    MaxTipAmount:            math.NewInt(100_000_000),            // 100 DREAM
    MaxTipsPerEpoch:         10,
    MaxGiftAmount:           math.NewInt(500_000_000),            // 500 DREAM
    GiftOnlyToInvitees:      true,

    // Permissionless creation fees (burned on creation — anti-spam + deflationary pressure)
    ProjectCreationFee:               math.NewInt(5_000_000),   // 5 DREAM — burned when creating a permissionless project
    InitiativeCreationFeeApprentice:  math.NewInt(1_000_000),   // 1 DREAM — burned for apprentice initiative under permissionless project
    InitiativeCreationFeeStandard:    math.NewInt(3_000_000),   // 3 DREAM — burned for standard initiative under permissionless project
    PermissionlessMinTrustLevel:      2,                         // ESTABLISHED — minimum trust level to create a permissionless project
    PermissionlessMaxTier:            1,                         // STANDARD — highest tier allowed in permissionless projects

    // Treasury management
    MaxTreasuryBalance: math.NewInt(100_000_000_000_000), // 100,000 DREAM — excess burned
    TreasuryFundsInterims: true,  // interims paid from treasury first, mint only if empty
    TreasuryFundsRetroPgf: true,  // retro PGF paid from treasury first, mint remainder

    // Initiative rewards
    CompleterShare:          math.LegacyNewDecWithPrec(90, 2), // 90%
    TreasuryShare:           math.LegacyNewDecWithPrec(10, 2), // 10%
    MinReputationMultiplier: math.LegacyNewDecWithPrec(10, 2), // 10%

    // Initiative tiers
    ApprenticeTier: TierConfig{
        MaxBudget:        math.NewInt(100_000_000),             // 100 DREAM
        MinReputation:    math.LegacyZeroDec(),
        ReputationCap:    math.LegacyNewDec(25),
        RewardMultiplier: math.LegacyNewDecWithPrec(50, 2),    // 0.5x
    },
    StandardTier: TierConfig{
        MaxBudget:        math.NewInt(500_000_000),             // 500 DREAM
        MinReputation:    math.LegacyNewDec(25),
        ReputationCap:    math.LegacyNewDec(100),
        RewardMultiplier: math.LegacyOneDec(),                  // 1.0x
    },
    ExpertTier: TierConfig{
        MaxBudget:        math.NewInt(2_000_000_000),           // 2000 DREAM
        MinReputation:    math.LegacyNewDec(100),
        ReputationCap:    math.LegacyNewDec(500),
        RewardMultiplier: math.LegacyNewDecWithPrec(150, 2),   // 1.5x
    },
    EpicTier: TierConfig{
        MaxBudget:        math.NewInt(10_000_000_000),          // 10000 DREAM
        MinReputation:    math.LegacyNewDec(250),
        ReputationCap:    math.LegacyNewDec(1000),
        RewardMultiplier: math.LegacyNewDec(2),                 // 2.0x
    },

    // Conviction (sqrt scaling on both sides)
    // Formula: required_conviction = ConvictionPerDream * sqrt(budget)
    //          actual_conviction = sqrt(total_stakes * time * rep)
    ConvictionHalfLifeEpochs: 7,                                 // 7 days half-life
    ExternalConvictionRatio:  math.LegacyNewDecWithPrec(50, 2),  // 50%
    ConvictionPerDream:       math.LegacyNewDecWithPrec(20, 2),  // 0.20

    // Review periods
    DefaultReviewPeriodEpochs:    7,  // ~1 week
    DefaultChallengePeriodEpochs: 7,  // ~1 week

    // Invitations
    MinInvitationStake:             math.NewInt(100),
    InvitationAccountabilityEpochs: 150,                                // 1 season
    ReferralRewardRate:             math.LegacyNewDecWithPrec(5, 2),    // 5%
    InvitationCostMultiplier:       math.LegacyNewDecWithPrec(110, 2), // 1.1x

    // Trust levels (production values)
    TrustLevelConfig: TrustLevelConfig{
        ProvisionalMinRep:              math.LegacyNewDec(50),
        ProvisionalMinInterims:         3,
        EstablishedMinRep:              math.LegacyNewDec(200),
        EstablishedMinInterims:         10,
        TrustedMinRep:                  math.LegacyNewDec(500),
        TrustedMinSeasons:              1,
        CoreMinRep:                     math.LegacyNewDec(1000),
        CoreMinSeasons:                 2,
        // Invitation credits per trust level (max per season)
        NewInvitationCredits:           0,
        ProvisionalInvitationCredits:   1,
        EstablishedInvitationCredits:   3,
        TrustedInvitationCredits:       5,
        CoreInvitationCredits:          10,
    },

    // Challenges
    MinChallengeStake:                  math.NewInt(50),
    ChallengerRewardRate:               math.LegacyNewDecWithPrec(20, 2),  // 20%
    JurySize:                           5,
    JurySuperMajority:                  math.LegacyNewDecWithPrec(67, 2),  // 67%
    MinJurorReputation:                 math.LegacyNewDec(50),
    ChallengeResponseDeadlineEpochs:    3,                                  // ~3 days

    // Interim compensation (micro-DREAM)
    SimpleComplexityBudget:   math.NewInt(50_000_000),             // 50 DREAM
    StandardComplexityBudget: math.NewInt(150_000_000),            // 150 DREAM
    ComplexComplexityBudget:  math.NewInt(400_000_000),            // 400 DREAM
    ExpertComplexityBudget:   math.NewInt(1_000_000_000),          // 1000 DREAM
    SoloExpertBonusRate:      math.LegacyNewDecWithPrec(50, 2),   // 50%
    InterimDeadlineEpochs:    7,                                    // ~1 week

    // Rate limits
    MaxActiveChallengesPerCommittee: 3,
    MaxNewChallengesPerEpoch:        2,
    ChallengeQueueMaxSize:           10,

    // Slashing
    MinorSlashPenalty:    math.LegacyNewDecWithPrec(5, 2),  // 5%
    ModerateSlashPenalty: math.LegacyNewDecWithPrec(15, 2), // 15%
    SevereSlashPenalty:   math.LegacyNewDecWithPrec(30, 2), // 30%
    ZeroingSlashPenalty:  math.LegacyOneDec(),               // 100%

    // Extended staking (project/member/tag)
    // Project staking draws from the same seasonal reward pool (no separate APY)
    ProjectCompletionBonusRate: math.LegacyNewDecWithPrec(5, 2),  // 5% bonus on completion
    MemberStakeRevenueShare:    math.LegacyNewDecWithPrec(5, 2),  // 5% revenue share
    TagStakeRevenueShare:       math.LegacyNewDecWithPrec(2, 2),  // 2% per tag
    MinStakeDurationSeconds:    86400,                              // 24 hours
    AllowSelfMemberStake:       false,

    // Gift rate limiting
    GiftCooldownBlocks:     14400,                          // 1 day
    MaxGiftsPerSenderEpoch: math.NewInt(2_000_000_000),     // 2000 DREAM per epoch

    // Content staking (set MaxContentStakePerMember to 0 to disable)
    MaxContentStakePerMember:           math.NewInt(10_000_000_000), // 10000 DREAM per content item
    ContentConvictionHalfLifeEpochs:    14,                          // 14 days (slower decay than initiatives)

    // Author bond staking (set MaxAuthorBondPerContent to 0 to disable)
    MaxAuthorBondPerContent:            math.NewInt(1_000_000_000),  // 1000 DREAM per content item
    AuthorBondSlashOnModeration:        true,                        // slash bond if content is moderated/removed
}
```

## RepOperationalParams

Council-gated operational parameter updates (same pattern as x/blog and x/collect). These are day-to-day tuning knobs that do not affect core economic incentives or tier structures.

The `RepOperationalParams` message mirrors most `Params` fields except governance-only fields (tier configs, slashing penalties, trust level thresholds). See `proto/sparkdream/rep/v1/params.proto` for the full field list.

**Governance-only fields** (NOT in RepOperationalParams):
- `ApprenticeTier`, `StandardTier`, `ExpertTier`, `EpicTier`
- `MinorSlashPenalty`, `ModerateSlashPenalty`, `SevereSlashPenalty`, `ZeroingSlashPenalty`
- `TrustLevelConfig`
- `CompleterShare`, `TreasuryShare`
- `PermissionlessMinTrustLevel`, `PermissionlessMaxTier` (structural access control — governance only)

**Operational fields** (council-tunable, included in RepOperationalParams):
- `ProjectCreationFee`, `InitiativeCreationFeeApprentice`, `InitiativeCreationFeeStandard` (fee amounts are tuning knobs)

## Error Codes

| Code | Name | Description |
|------|------|-------------|
| 1100 | `ErrInvalidSigner` | Expected gov account as signer for proposal |
| 1101 | `ErrInvalidAmount` | Invalid amount |
| 1102 | `ErrMemberNotFound` | Member not found |
| 1103 | `ErrInsufficientBalance` | Insufficient DREAM balance |
| 1104 | `ErrInsufficientStake` | Insufficient staked DREAM |
| 1105 | `ErrCannotTransferToSelf` | Cannot transfer to self |
| 1106 | `ErrInvalidTransferPurpose` | Invalid transfer purpose |
| 1107 | `ErrExceedsMaxTipAmount` | Exceeds maximum tip amount |
| 1108 | `ErrExceedsMaxTipsPerEpoch` | Exceeds maximum tips per epoch |
| 1109 | `ErrRecipientNotActive` | Recipient is not active |
| 1110 | `ErrExceedsMaxGiftAmount` | Exceeds maximum gift amount |
| 1111 | `ErrGiftOnlyToInvitees` | Gifts only allowed to invitees |
| 1112 | `ErrGiftCooldownNotMet` | Gift cooldown period not met |
| 1113 | `ErrExceedsEpochGiftLimit` | Exceeds maximum gifts per epoch |
| 1201 | `ErrNoInvitationCredits` | No invitation credits available |
| 1202 | `ErrMemberAlreadyExists` | Member already exists |
| 1203 | `ErrInvitationAlreadyExists` | Invitation already exists for this address |
| 1204 | `ErrInvitationNotFound` | Invitation not found |
| 1205 | `ErrInvitationNotPending` | Invitation is not pending |
| 1206 | `ErrInviteeAddressMismatch` | Invitee address mismatch |
| 1207 | `ErrNotMember` | Address is not a member |
| 1301 | `ErrProjectNotFound` | Project not found |
| 1302 | `ErrInvalidProjectStatus` | Invalid project status |
| 1303 | `ErrInsufficientBudget` | Insufficient budget |
| 1304 | `ErrUnauthorized` | Unauthorized: insufficient permissions |
| 1401 | `ErrInitiativeNotFound` | Initiative not found |
| 1402 | `ErrInvalidInitiativeStatus` | Invalid initiative status |
| 1403 | `ErrInsufficientReputation` | Insufficient reputation for tier |
| 1404 | `ErrSelfAssignment` | Cannot self-assign initiative |
| 1405 | `ErrNotAssignee` | Not the assignee of this initiative |
| 1501 | `ErrStakeNotFound` | Stake not found |
| 1502 | `ErrNotStakeOwner` | Not the owner of this stake |
| 1503 | `ErrMinStakeDuration` | Minimum stake duration not met |
| 1504 | `ErrSelfMemberStake` | Cannot stake on yourself |
| 1505 | `ErrInvalidTargetType` | Invalid stake target type |
| 1506 | `ErrStakePoolNotFound` | Stake pool not found |
| 1600 | `ErrInvalidRequest` | Invalid request |
| 1701 | `ErrChallengeNotFound` | Challenge not found |
| 1702 | `ErrChallengeNotPending` | Challenge is not pending |
| 1703 | `ErrNotChallengeParty` | Not a party to this challenge |
| 1801 | `ErrMemberAlreadyZeroed` | Member is already zeroed |
| 1802 | `ErrMemberNotActive` | Member is not active |
| 1803 | `ErrCannotZeroCore` | Cannot zero a core member without governance vote |
| 1901 | `ErrInsufficientTrustLevel` | Trust level too low for permissionless creation |
| 1902 | `ErrPermissionlessTierExceeded` | Tier exceeds maximum allowed for permissionless projects |
| 1903 | `ErrInsufficientCreationFee` | Insufficient DREAM balance for creation fee |

## Content Staking

Content staking provides two complementary mechanisms for economic quality signals on content (blog posts, forum threads, collections). Both are centralized in x/rep so any content module can use them.

| Mechanism | Who | Signal | Rewards |
|-----------|-----|--------|---------|
| **Community Conviction** | Any member (except author) | "We believe this is valuable" | None (conviction score only) |
| **Author Bond** | Content author only | "I stand behind this" | None (DREAM returned or slashed) |

### Motivation

Traditional upvote/like systems are free and therefore low-signal. Content staking creates real economic cost to signal quality: members must lock DREAM tokens with a time commitment. This produces four layers of engagement:
1. **Reactions** (free) — casual social signals
2. **Author bonds** (DREAM locked by author) — creator skin-in-the-game
3. **Community conviction stakes** (DREAM locked by others) — economic quality signals with time-weighted conviction
4. **Tips/gifts** (DREAM transferred) — direct creator compensation

### Content Identification

Both mechanisms identify content items via a `(target_type, target_id)` pair using module-specific `StakeTargetType` enum values:
- `(STAKE_TARGET_BLOG_CONTENT, 42)` — blog post #42 (community conviction)
- `(STAKE_TARGET_FORUM_CONTENT, 7)` — forum thread #7 (community conviction)
- `(STAKE_TARGET_COLLECTION_CONTENT, 3)` — collection #3 (community conviction)
- `(STAKE_TARGET_BLOG_AUTHOR_BOND, 42)` — author bond on blog post #42
- `(STAKE_TARGET_FORUM_AUTHOR_BOND, 7)` — author bond on forum thread #7
- `(STAKE_TARGET_COLLECTION_AUTHOR_BOND, 3)` — author bond on collection #3

### Community Conviction Staking

Community conviction staking allows any active member to stake DREAM on content items as a quality signal. Unlike other stake types, content stakes do not earn DREAM rewards — they exist purely to signal conviction through economic commitment.

**How it works:**

1. Member calls `MsgStake` with `target_type = STAKE_TARGET_BLOG_CONTENT` (or `FORUM_CONTENT`/`COLLECTION_CONTENT`) and `target_id = 42`
2. DREAM is locked from the member's balance (same as initiative staking)
3. Conviction builds over time using the same half-life formula as initiative conviction, but with a separate `content_conviction_half_life_epochs` parameter (default 14 epochs = 2 weeks, slower decay than initiative conviction's 7 epochs)
4. Any module can query `ContentConviction(target_type, target_id)` to get the current score
5. When the member unstakes, DREAM is returned after `min_stake_duration_seconds` cooldown
6. No DREAM rewards are minted — conviction is the only output

**Constraints:**

- **Active members only** — must be an active member to stake
- **No self-staking** — authors cannot stake on their own content (use author bonds instead)
- **Per-member cap** — `max_content_stake_per_member` caps how much one member can stake on a single content item (default 10,000 DREAM)
- **Min duration** — same `min_stake_duration_seconds` cooldown as other stakes (24 hours)
- **Conviction decay** — conviction decays with `content_conviction_half_life_epochs` half-life; stops growing when staker unstakes

**Conviction formula:**

```
conviction(t) = stake_amount * (1 - 2^(-t / half_life))
```

Where `t` is the time in epochs since the stake was created. Total conviction for a content item is the sum of all individual stake convictions. Author bonds do NOT contribute to conviction score — the two signals are kept separate.

### Author Bond Staking

Author bond staking allows content creators to lock DREAM on their own content as a skin-in-the-game signal. This is the "I stand behind this" mechanism — the author puts up economic collateral backing the quality of their content.

**How it works:**

1. Author creates content via a content module (x/blog, x/forum, x/collect) with an optional `author_bond` amount in the creation message
2. The content module calls `repKeeper.CreateAuthorBond(ctx, author, targetType, targetID, amount)` during content creation
3. DREAM is locked from the author's balance
4. The bond amount is visible on the content item (queryable via `GetAuthorBond`)
5. Author can release the bond after `min_stake_duration_seconds` by calling `MsgUnstake` on the bond's stake ID
6. If the content is moderated or removed (e.g., via x/forum sentinel system), the bond can be slashed

**Constraints:**

- **Author only** — only the content creator can create an author bond (enforced by the content module calling the keeper method, not by MsgStake directly)
- **One bond per content item** — an author can only have one active bond per content item
- **Per-content cap** — `max_author_bond_per_content` caps the bond at 1,000 DREAM per content item
- **No conviction contribution** — author bonds do NOT contribute to the content's conviction score (keeps community signal separate from author signal)
- **No DREAM rewards** — bonds are returned on unstake, not rewarded
- **Slashable** — if `author_bond_slash_on_moderation` is true (default), the bond is burned when content is moderated or removed

**Slashing integration:**

Content modules that support moderation (x/forum sentinel system, x/collect curation) can call `repKeeper.SlashAuthorBond(ctx, targetType, targetID)` when content is removed. This burns the bonded DREAM. The flow:

1. x/forum sentinel flags content for removal
2. If appeal fails or no appeal filed, x/forum calls `repKeeper.SlashAuthorBond(ctx, STAKE_TARGET_FORUM_AUTHOR_BOND, 7)`
3. x/rep burns the bonded DREAM and marks the stake as resolved
4. If the author had already unstaked (bond released), there is nothing to slash

**Why keeper methods instead of MsgStake:**

Author bonds are created via keeper methods called by content modules — not via `MsgStake` directly. This is because authorship verification naturally lives in the content module (x/blog knows who authored post #42, x/rep does not). The content module verifies the caller is the author, then delegates the staking logic to x/rep. This avoids x/rep needing keeper dependencies on every content module.

### Integration with Other Modules

Each module that wants to use content staking adds a `RepKeeper` interface:

```go
// In x/blog/types/expected_keepers.go, x/forum/types/expected_keepers.go, x/collect/types/expected_keepers.go
type RepKeeper interface {
    // Community conviction
    GetContentConviction(ctx context.Context, targetType int32, targetID uint64) (math.LegacyDec, error)

    // Author bonds
    CreateAuthorBond(ctx context.Context, author sdk.AccAddress, targetType int32, targetID uint64, amount math.Int) (uint64, error)
    SlashAuthorBond(ctx context.Context, targetType int32, targetID uint64) error
    GetAuthorBond(ctx context.Context, targetType int32, targetID uint64) (math.Int, error)

    // Membership checks
    IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool
    GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (types.TrustLevel, error)
}
```

Modules use these for:
- **x/blog**: Surface high-conviction posts, display author bond amounts, slash bonds on content removal
- **x/forum**: Weight thread ranking by conviction, slash bonds via sentinel moderation
- **x/collect**: Rank collections by community conviction, slash bonds on curation removal

### Security Considerations

- **Sybil resistance**: Staking requires DREAM, which requires membership and reputation. Multiple accounts would need to be individually invited and earn DREAM independently.
- **No flash-staking**: Time-weighted conviction prevents flash-staking attacks on content renewal. A stake placed moments before a renewal check has near-zero conviction (`conviction(t) = stake_amount * (1 - 2^(-t / half_life))` with `t ≈ 0`). Sustaining content through renewal requires stakes that have been held for a meaningful duration relative to the half-life.
- **Free unstaking**: Stakers can unstake at any time. If conviction drops below the renewal threshold, the content expires at the next renewal check. No lock-in mechanism is needed because time-weighting already ensures that only sustained commitment produces meaningful conviction.
- **Conviction decay**: Old stakes lose conviction over time, preventing "set and forget" manipulation. Active, sustained community conviction is the only signal that persists.
- **No rewards**: Neither content conviction staking nor author bonds earn DREAM rewards, eliminating yield-farming incentives. The only motivation is genuine belief in content quality.
- **Separate signals**: Author bonds do NOT contribute to conviction score, preventing authors from inflating their own community signal. The two metrics are displayed and queried independently.
- **Author exclusion from conviction**: Authors cannot create community conviction stakes on their own content — they must use author bonds instead.
- **Cap per member**: The `max_content_stake_per_member` parameter prevents whale domination of any single content item's conviction score.
- **Bond slashing**: Author bonds create real accountability — if moderation determines content violates guidelines, the bond is burned. This discourages low-quality content with high bonds (attempting to game perceived quality).
- **No orphaned state**: Content conviction is queried on demand by content module EndBlockers. x/rep stores stakes and computes conviction — no cross-module state (like floors or locks) needs cleanup.

## Dependency Injection

Located in `x/rep/module/depinject.go`:

```go
type ModuleInputs struct {
    depinject.In
    Config       *types.Module
    StoreService store.KVStoreService
    Cdc          codec.Codec
    AddressCodec address.Codec
    AuthKeeper   types.AuthKeeper
    BankKeeper   types.BankKeeper
    CommonsKeeper types.CommonsKeeper `optional:"true"`
    // SeasonKeeper is wired manually in app.go via SetSeasonKeeper to break
    // the cyclic dependency: rep -> season -> collect/blog/forum -> rep.
    // TagKeeper is wired manually via SetTagKeeper to break: forum -> rep -> forum.
}
```

## File References

- `proto/sparkdream/rep/v1/params.proto` — Params and RepOperationalParams definitions
- `proto/sparkdream/rep/v1/member.proto` — Member, GiftRecord, TrustLevel, MemberStatus
- `proto/sparkdream/rep/v1/challenge.proto` — Challenge, ChallengeStatus
- `proto/sparkdream/rep/v1/content_challenge.proto` — ContentChallenge, ContentChallengeStatus
- `proto/sparkdream/rep/v1/interim.proto` — Interim, InterimType, InterimComplexity, InterimStatus
- `proto/sparkdream/rep/v1/stake.proto` — Stake, StakeTargetType, MemberStakePool, TagStakePool, ProjectStakeInfo
- `proto/sparkdream/rep/v1/tx.proto` — All Msg definitions
- `proto/sparkdream/rep/v1/query.proto` — All Query definitions
- `proto/sparkdream/rep/v1/genesis.proto` — GenesisState
- `x/rep/keeper/keeper.go` — Keeper struct and NewKeeper
- `x/rep/keeper/abci.go` — EndBlocker logic
- `x/rep/keeper/trust_tree.go` — Persistent MiMC Merkle tree implementation (MaybeRebuildTrustTree, incremental updates)
- `x/rep/keeper/merkle_trees.go` — Exported API wrappers (GetTrustTreeRoot, GetPreviousTrustTreeRoot) used by x/shield
- `x/rep/keeper/shield_aware.go` — IsShieldCompatible() for x/shield integration
- `x/rep/keeper/content_challenge.go` — Content challenge creation, response, and resolution logic
- `x/rep/keeper/msg_server_register_zk_public_key.go` — MsgRegisterZkPublicKey handler
- `x/rep/types/params.go` — DefaultParams, Validate, ApplyOperationalParams
- `x/rep/types/params_vals.go` — TrustLevelConfig (production vs testing values)
- `x/rep/types/errors.go` — All error codes
- `x/rep/types/expected_keepers.go` — External keeper interfaces
- `x/rep/module/depinject.go` — Dependency injection wiring
