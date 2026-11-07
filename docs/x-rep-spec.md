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
- Tag registry and tag moderation (`Tag`, `ReservedTag`, `TagReport`)
- Tag budgets (reward pools scoped per tag)
- Bonded-role accountability primitives (bond, bond status, activity stamps) — generic `BondedRole(role_type, address)` shared by forum sentinels, collect curators, federation verifiers
- Member accountability (reports, warnings, appeals, jury participation)

## Dependencies

| Module | Usage |
|--------|-------|
| `x/auth` | Address codec, account lookups (simulation) |
| `x/bank` | Coin transfers |
| `x/commons` | Council/committee authorization for operations, HR, governance |
| `x/season` | Current season state, display name appeal resolution |
| `x/shield` | Unified privacy layer: anonymous challenges are submitted via `MsgShieldedExec` wrapping `MsgCreateChallenge`. x/shield owns ZK proof verification, nullifier checking, and module-paid gas. x/rep maintains the trust tree (MiMC Merkle tree over member ZK public keys + trust levels) that x/shield uses for proof root validation. |
| `x/forum` | Narrow `ForumKeeper` surface (3 methods): `PruneTagReferences(ctx, name)` called from `MsgResolveTagReport` when a tag is removed; `GetPostAuthor(ctx, postID)` and `GetPostTags(ctx, postID)` used by `MsgAwardFromTagBudget` to validate the target post and its tag set. Wired manually in `app.go` via `SetForumKeeper()` (the reverse `forum → rep` edge is via `RepKeeper`). |

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

  // Tip allowance: DREAM tipped this epoch (an amount, not a count)
  string tips_sent_this_epoch = 19 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 last_tip_epoch = 20;

  // Cached counts for performance (avoid full table scans for trust level checks)
  uint32 completed_interims_count = 21;
  uint32 completed_initiatives_count = 22;

  // Recipient-side transfer limit: DREAM received through tips and initiative
  // bounty payouts this epoch (see DREAM Transfer Limits)
  string transfer_received_this_epoch = 23 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 last_transfer_received_epoch = 24;

  // Invitation credit tracking for lazy seasonal reset
  int64 last_credit_reset_season = 25;

  // Per-epoch reputation gain cap tracking
  map<string, string> reputation_gained_this_epoch = 26;
  int64 last_rep_gain_epoch = 27;

  // ZK public key for anonymous operations (trust tree leaf computation).
  // Set via MsgRegisterZkPublicKey. Used by the persistent Merkle tree
  // to build leaves as MiMC(zk_public_key, trust_level).
  bytes zk_public_key = 28;

  // Salvation counters (per-epoch window for the accountability salvation path).
  uint32 epoch_salvations = 29;
  int64 last_salvation_epoch = 30;

  // Forum-earned reputation (current season). Per-tag rep earned through
  // conviction-staked endorsements on x/forum posts/replies. Stored SEPARATELY
  // from reputation_scores so the trust-level ladder can count it toward
  // PROVISIONAL and ESTABLISHED while EXCLUDING it from TRUSTED and CORE —
  // forum participation is a legitimate early-stage signal but cheaper to game
  // than verified initiative output, so it must not gate the council-adjacent
  // tiers. Slashable on confirmed hide of the originating post. Map value is a
  // decimal string (math.LegacyDec).
  map<string, string> forum_rep_per_tag = 31;

  // Forum-earned reputation (lifetime archive). On season transition,
  // forum_rep_per_tag is added here then cleared (mirrors lifetime_reputation /
  // reputation_scores). Display-only — never read by the trust-level ladder, so
  // forum rep accrued in an early season cannot ride into TRUSTED/CORE later.
  map<string, string> lifetime_forum_rep_per_tag = 32;

  // Height-domain twin of joined_at (a unix timestamp). The new-member decay
  // grace window is denominated in epochs — height / epoch_blocks — so it has
  // to be measured against a height. Dividing the timestamp by epoch_blocks
  // produced an astronomically large join epoch, a deeply negative member age,
  // and a grace window that never expired for invited members. Records written
  // before this field carry 0, which reads as "joined at genesis" — the same
  // behaviour genesis-seeded members have always had.
  int64 joined_at_height = 33;

  // Aggregate DREAM held in content-conviction stakes across ALL content items
  // (blog, forum, collection), backing max_total_content_stake_per_member.
  // Maintained by updateStakePoolTotals, the single choke point for
  // stake-amount mutations, and recomputed by ReconcileStakePoolTotals on
  // genesis import. Author bonds are excluded — slashable escrow with its own
  // per-item cap. Nil on older records; readers must treat nil as zero.
  string content_staked_dream = 34 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Season-scale counterpart of transfer_received_this_epoch
  string transfer_received_this_season = 35 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 last_transfer_received_season = 36;

  // DREAM escrowed into initiative bounties this epoch, bounded by
  // max_initiative_bounty_per_funder_epoch
  string initiative_bounty_funded_this_epoch = 37 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  int64 last_initiative_bounty_epoch = 38;

  // The content license this member agreed to publish under (see The
  // membership agreement). Set from MsgAcceptInvitation, or for genesis
  // members from the genesis file, and never changed afterwards: not by
  // zeroing, not by status changes. The agreement was made at joined_at /
  // joined_at_height.
  string content_license = 39;

  // Member key migration (see "Member key migration"). Finalization times of
  // KEY-path migrations inside key_migration_window (rolling cap), and the
  // time of the last migration into this address (cooldown).
  repeated int64 key_migration_times = 40;
  int64 last_migrated_at = 41;
  // ZK key change discipline: a changed key waits in pending_zk_public_key
  // until zk_key_activates_at_epoch; one change per season unless a
  // RECOVERY/COUNCIL migration cleared the key (zk_key_cooldown_exempt).
  bytes pending_zk_public_key = 42;
  int64 zk_key_activates_at_epoch = 43;
  int64 last_zk_key_change_season = 44;
  bool zk_key_changed = 45;
  bool zk_key_cooldown_exempt = 46;
  // zk_key_cleared is set by MsgClearZkPublicKey (leaked shield secret). While
  // set, MsgRegisterZkPublicKey never takes the immediate first-registration
  // path: the new key waits out zk_key_activation_delay and the once-per-season
  // change cap still applies. Cleared when a key activates.
  bool zk_key_cleared = 47;
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

#### Trust-Level Progression and Forum Reputation

`UpdateTrustLevel` (in `keeper/member.go`) promotes a member one tier at a time
by comparing two inputs against the per-tier thresholds in `TrustLevelConfig`:

- **Verified-work rep** — the sum of `reputation_scores` across all tags. This
  is the rep earned from completed interims, initiatives, and reveals.
- **Forum rep** — the sum of `forum_rep_per_tag` across all tags
  (`SumForumRep`). This is rep earned through conviction-staked endorsements on
  x/forum content.

Whether forum rep counts depends on the target tier — this asymmetry is the
core anti-gaming mechanism:

| Transition | Reputation input compared to threshold | Other gate |
|-----------|------------------------------------------|------------|
| NEW → PROVISIONAL | `sum(reputation_scores) + sum(forum_rep_per_tag)` | `completed_interims >= ProvisionalMinInterims` |
| PROVISIONAL → ESTABLISHED | `sum(reputation_scores) + sum(forum_rep_per_tag)` | `completed_interims >= EstablishedMinInterims` |
| ESTABLISHED → TRUSTED | `sum(reputation_scores)` only — **forum rep excluded** | min seasons since join |
| TRUSTED → CORE | `sum(reputation_scores)` only — **forum rep excluded** | min seasons since join |

Forum rep counts toward **PROVISIONAL and ESTABLISHED** because early
participation and conviction-staked endorsement are legitimate "you're plugged
in" signals at the lower rungs. It is **excluded from TRUSTED and CORE** because
those tiers gate council eligibility and conviction-completion of initiatives,
where discourse popularity would be too cheap to farm — those rungs must be
earned exclusively through shipped, verified initiative output. A member can
therefore reach ESTABLISHED on forum rep alone (when interim counts are met) but
can never cross into TRUSTED without sufficient verified-work rep.

#### Forum Reputation Keeper Surface

x/forum credits and slashes forum rep through four exported keeper methods on
x/rep (`keeper/forum_rep.go`), wired into x/forum's `RepKeeper` interface:

| Method | Semantics |
|--------|-----------|
| `AddForumRep(ctx, memberAddr, tag, amount)` | Adds `amount` to `forum_rep_per_tag[tag]`. No-op on nil/zero, rejects negative (`ErrInvalidAmount`). Requires an ACTIVE member. Emits `forum_rep_added`. Called by x/forum's `PostConvictionStake` accrual loop, which records the credited amount on each stake's `accrued_rep_per_tag` so a later slash can be exact. |
| `DeductForumRep(ctx, memberAddr, tag, amount)` | Subtracts `amount` from `forum_rep_per_tag[tag]`, **floored at zero** (never negative); deletes the tag entry when it hits zero. No-op on nil/zero or missing tag, rejects negative. Emits `forum_rep_deducted`. Called by x/forum's `SlashStakesForPost` / `ExpireHiddenPosts` to claw back exactly the rep each conviction stake produced (using its `accrued_rep_per_tag`) when the originating post is finalized as hidden. |
| `GetForumRep(ctx, memberAddr, tag)` | Returns the member's forum rep in one tag (zero if absent or unregistered). |
| `SumForumRep(ctx, memberAddr)` | Returns total forum rep across all tags. Used by `UpdateTrustLevel` for the PROVISIONAL/ESTABLISHED thresholds above. |

On **season transition**, `ArchiveSeasonalReputation` (in
`keeper/season_integration.go`) folds `forum_rep_per_tag` into
`lifetime_forum_rep_per_tag` per tag, then clears `forum_rep_per_tag` —
mirroring the `reputation_scores` → `lifetime_reputation` archive. On that
archive, x/season's `baseline_reputation` restarts only the tags an ACTIVE
member held with a positive score; a tag deducted to zero, and every tag of a
ZEROED or INACTIVE member, is cleared instead (REP-E-204: a zeroed member keeps
their tag keys at 0 and must not be handed baseline reputation in each of
them by the rollover). The lifetime
forum map is display-only and is never read by the trust ladder, so forum rep
accrued in an early season cannot be ridden into TRUSTED/CORE in a later one.
The `reputation_archived` event gains a `forum_tags_archived` attribute.

### GiftRecord

The lifetime ledger of what a sender has gifted one of their invitees, bounded
by `max_gift_per_invitee`:

```protobuf
message GiftRecord {
  string sender = 1;
  string recipient = 2;
  string total_gifted = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
}
```

### The membership agreement

`MsgAcceptInvitation` is the only way to become a member at runtime, and it
carries the member's signed agreement to publish under CC0
(`accepted_content_license = "CC0-1.0"`). Genesis members (the founders) are
the people who set the policy; they sign no message and instead state the
agreement in the genesis file (below). Non-members who post
ephemeral content are bound by the same dedication through the act of
submitting it, which every client and CLI composer states.

**The agreement is kept in state.** The signed transaction and its
`content_license_accepted` event prove the agreement, but nodes prune both, so
after pruning only an archive node could show it. The member record therefore
stores what was agreed to, in `Member.content_license`:

- `AcceptInvitation` writes `content_license = msg.accepted_content_license`
  when it creates the member. The handler has already refused any other value,
  so the field always holds the chain's content license
  (`commontypes.ChainContentLicense`).
- When it was agreed is not stored separately. An address can join only once
  (`ErrMemberAlreadyExists`), so the agreement and the join are the same moment:
  `joined_at` and `joined_at_height`.
- The field is never rewritten. A CC0 dedication cannot be revoked, so zeroing,
  inactivity and every other status change leave it alone. A zeroed person who
  rejoins from a new address signs a new acceptance on a new record.
- Genesis members state their agreement in the genesis file itself: every
  entry in `member_map` carries `content_license: "CC0-1.0"`, and
  `GenesisState.Validate` refuses a member whose field is anything other than
  the chain's content license, empty included. The chain never fills the field
  in on a member's behalf. The founders of each chain, including a federated
  sister chain launched from this code, write the agreement into the genesis
  every validator checks before starting, and a config that leaves it out fails
  at init instead of recording an agreement nobody made. `InitGenesis` imports
  the field as-is, so an export/import round trip changes nothing.
- The `Member` query returns the field, so clients and indexers can show "agreed
  to CC0 at block N" without replaying history.

The field stores the license id, not the wording of the dedication statement.
The id is what the member signed and what the chain checks; the statement
clients show comes from the `x/sparkdream` `ContentLicense` query
([content-license.md](content-license.md)).

### The new-member on-ramp

A member created by `AcceptInvitation` starts with **zero of everything**: zero
DREAM, zero SPARK, zero reputation, and `TRUST_LEVEL_NEW`. That is deliberate,
and the reasoning is recorded here because it is repeatedly mistaken for a
cold-start bug.

**Capital is not required to participate.** The self-assignment bond fires only
on `IsSelfAssigned`; a member *assigned* work by someone else needs no DREAM at
all. The intended path is therefore: be invited → be assigned work and complete
interims → earn DREAM and reputation → reach `PROVISIONAL` → commission your own
work. Zero balance blocks only the last step.

**The creation fee is not what gates that last step — trust level is.**
Permissionless creation requires `PROVISIONAL` at minimum, which on mainnet
means 50 reputation and 3 completed interims. Interims pay DREAM, so by the time
the 1 DREAM apprentice creation fee is even reachable, the member has been paid
three times. Waiving that fee for "new" members would subsidise a state that
cannot occur.

**The inviter is the intended funding channel, and the protocol builds one for
it.** `GiftOnlyToInvitees` (default true) restricts gifts to a sender's own
invitees, up to `max_gift_per_invitee` (500 DREAM) per invitee for life (see
[DREAM Transfer Limits](#dream-transfer-limits)). The check is
`recipientMember.InvitedBy == sender`, a relationship written once at member
creation and never cleared, so an inviter can seed a member at any later point
rather than only at invitation time. Tips (10 DREAM each, 50 per sender per
epoch) are a second channel and carry no invitee restriction, so a third party
can top someone up too.

This places trust and exposure with the same party: the invitation stake is
slashable if the invitee misbehaves, and the same inviter decides how much to
seed them. Both are that member's judgment.

**Why there is no protocol-wide welcome grant.** A minted grant would replace
that judgment with a flat entitlement, and the invitation economics cannot price
it. An invitation costs the inviter `min_invitation_stake` (100 DREAM, returned
unless slashed) and an irreversible burn of `invitation_stake_burn_rate` (10%,
so 10 DREAM), escalating by `invitation_cost_multiplier` (1.1x) per invitation.
A grant larger than that burn makes inviting **net-profitable** and turns the
invitation system into a DREAM faucet: at 50 DREAM the first ~17 invitations
from every member each net a profit, roughly +427 DREAM over 20 invites. Any
future grant must therefore be bounded by the burn — which at 10 DREAM buys ten
apprentice initiatives and is comfortably covered by the gift channel that
already exists.

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
  int64 created_epoch = 13;             // acceptance window counted from here
  int64 accountability_end_epoch = 14;  // last epoch of the accountability / referral window
}

enum InvitationStatus {
  INVITATION_STATUS_PENDING = 0;
  INVITATION_STATUS_ACCEPTED = 1;
  INVITATION_STATUS_EXPIRED = 2;
  INVITATION_STATUS_REVOKED = 3;
}
```

**Invitation rules.**

- Only an ACTIVE member invites (`ErrMemberNotActive`): a zeroed member keeps
  its record, and credits returned by an old invitation must not let it vouch
  again.
- `vouched_tags` holds at most `MaxVouchedTags` (10) distinct registered tags
  (`ErrTooManyVouchedTags`, 2410); each becomes a reputation key on the new
  member.
- Acceptance burns `invitation_stake_burn_rate` of the stake out of the
  invitation's own lock (`BurnLockedDREAM`) and unlocks the rest. The inviter
  is found through the alias contract, so an inviter that migrated still has
  its invitation accepted, revoked or expired normally.
- A credit returned by expiry or revocation goes back only to an ACTIVE
  inviter and never above the trust level's maximum.
- The inviter-accountability penalty (an invitee zeroed inside the
  accountability window) is the original stake, capped at the inviter's
  **unlocked** balance. It used to burn up to the whole balance, locks
  included, which left the inviter's own escrows without DREAM behind them.
  The `inviter_accountability` event carries `penalty` and `burned`.
- `PendingInvitations` indexes PENDING invitations by `(created_at, id)` for the
  expiry sweep; it and the invitee index (rebuilt for PENDING and ACCEPTED
  invitations only) are derived at InitGenesis.
- A RECOVERY or COUNCIL migration that opens with a PENDING invitation to its
  target voids the invitation (`REVOKED`, stake unlocked, credit restored,
  slot freed, event `invitation_voided_by_migration`); see "Member key
  migration".

### Project

Projects come in two flavors:

- **Budget-backed**: Created via `MsgProposeProject` with `requested_budget > 0` or `requested_spark > 0`. Requires Operations Committee approval before becoming ACTIVE. Initiatives draw from the approved budget allocation. The creator must be an ACTIVE member, burns the same `ProjectCreationFee` as the permissionless path, and may hold at most `max_proposed_projects_per_creator` (default 3) proposals in PROPOSED at once, counted across their recent aliases (`ErrTooManyProposedProjects`, 2423): each proposal opens a paid review interim for the picked council's Operations Committee (see Project Review Interims), so proposing was never free work for the committee. The requested amounts are stored (`requested_budget`, `requested_spark`) and bound the approval.
- **Permissionless**: Created via `MsgProposeProject` with zero budget. Creator (ACTIVE, ESTABLISHED+) burns the protocol fee (`ProjectCreationFee`) and the project becomes ACTIVE immediately — no committee approval needed. Initiatives under permissionless projects are capped at STANDARD tier and their rewards are minted on conviction completion (no pre-allocated budget).

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

  // Block height at which a PROPOSED project is expired by the EndBlocker
  // (creation height + proposed_project_expiry_blocks); zero for
  // permissionless projects and once the project leaves PROPOSED.
  int64 expiry_block_height = 19;

  // Id of the PROJECT_APPROVAL / BUDGET_REVIEW interim opened for the
  // picked council's Operations Committee; zero when none was opened.
  uint64 approval_interim_id = 20;

  // What the proposal asked for. MsgApproveProjectBudget may approve at most
  // these amounts (ErrApprovedBudgetExceedsRequested): the review interim's
  // type is priced from the request. Zero on permissionless projects.
  string requested_budget = 21 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  string requested_spark = 22 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
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
  // Set by the EndBlocker when a PROPOSED project was not approved before
  // its expiry_block_height. Terminal; kept for the audit trail.
  PROJECT_STATUS_EXPIRED = 4;
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

**Who may create an initiative.** Creating one under a **budget-backed** project is restricted to that project's creator, or the Operations Committee as an administrative escape hatch — the same standing that lets it assign and close (`ErrUnauthorized`). **Permissionless** projects are deliberately open to any member meeting the trust-level and tier gates, since open contribution is the point of that mode.

> The gate is about the *allocation*, not the initiative record. A project's `approved_budget` is a ceiling a council voted for, and `CreateInitiative` draws against it through `AllocateBudget`, which validates only that the project is ACTIVE and has room remaining. Without the check, any member could commission work against somebody else's council-approved budget until it was exhausted.

The check sits in the message server, after the membership check and before the keeper call, so that a non-member still gets `ErrNotMember` rather than an authorization error.

**Self-assignment.** A member MAY be the assignee of an initiative they commissioned themselves (solo builders are the common legitimate case). Conflict-of-interest is handled at the judging layer, not the assignment layer (mirroring x/reveal's contributor exclusion — conflicted parties may do the work, never judge it). Four safeguards apply.

An initiative counts as **self-assigned** when the assignee authored the initiative **or** created the parent project (`IsSelfAssigned`). Testing only the latter left the safeguards trivially avoidable: `CreateInitiative` does not require the author to own the project it sits under, and `MsgAssignInitiative` authorises every self-assignment, so creating an initiative under somebody else's active project and taking it yourself cleared all four at once.

- **Raised external conviction**: for self-assigned work the external-conviction requirement rises from `external_conviction_ratio` (default 50%) to `self_assigned_external_conviction_ratio` (default 75%) of `required_conviction` — the community must supply the large majority of the vouching. Because `max_conviction_share_per_member` (default 35%) caps what any one member can contribute, this ratio is also a floor on the number of *independent* stakers who must show up: `ceil(0.75 / 0.35) = 3`, against `ceil(0.50 / 0.35) = 2` for externally assigned work.

  Read the ratio as choosing a staker count, not a magnitude. Every value in `(0.70, 1.05]` yields the same floor of 3, so the choice within that band is only about how much headroom those three get: at 0.75 they clear the gate averaging 0.25 apiece, where 1.00 would need 0.34 of a 0.35 cap.

  **This ratio is not the only conviction gate, and it is not always the binding one.** `CanCompleteInitiative` also requires `current_conviction >= required_conviction`, and that total gate counts affiliated stake too — the assignee, the project creator, and everyone one invitation hop from them add to `current_conviction` but not to `external_conviction`, and nothing prevents them staking (the self-stake guard in `CreateStake` covers member targets only). So:

  - the ratio sets the floor on *external* stakers, `ceil(ratio / cap)`;
  - the total gate additionally requires those same members to reach `1.00` whenever no affiliate stakes alongside them, i.e. `ceil(1.00 / cap)`.

  The second floor is the one that bites on a small chain, and missing it is what made the earlier tuning ineffective: at a 0.33 cap, three unaided external stakers cleared the 0.75 ratio and then stalled at 0.99 of required, so lowering the ratio from 1.00 to 0.75 relaxed only the gate that was not binding for them. Raising the cap from 0.33 to 0.35 is what actually made three unaided stakers sufficient (`3 x 0.35 = 1.05`).

  Conviction propagated from linked content is added to both totals *uncapped*, so these are floors on direct stakers; propagation can supply part of a threshold without them.

  The coupling cuts both ways, and only one half of it is operationally tunable — the Operations Committee owns the cap, governance owns the ratios. `Params.Validate` therefore enforces the band directly, on both the governance and the operational-merge paths: the cap must be at least `1/3` (below that, three fully committed stakers cannot complete anything), and when governance asks for a stricter self-assigned ratio the cap must not let the same number of members clear both gates — which at a 0.75 ratio means staying under `0.375`. `TestSelfAssignedGatesImplyThreeIndependentStakers` pins the defaults against the same band.
- **Extended challenge window**: `TransitionToChallengePeriod` multiplies the challenge duration by `self_assigned_challenge_multiplier` (default 2).
- **Approval exclusion**: neither the assignee nor the project creator may sign `MsgApproveInitiative` for the initiative, regardless of stake or Operations Committee membership (`ErrConflictOfInterest`, code 1404).
- **DREAM bond**: at assignment a fraction of the initiative budget is locked from the assignee via `LockDREAM` and recorded in `initiative.self_assign_bond`. The bond is returned on completion, voluntary abandonment, or staker disapproval, and **burned** when a challenge is upheld. Zeroing the assignee clears `self_assign_bond` on every initiative they hold without unlocking it, since `ZeroMember` already burned the staked DREAM behind it; a later abandonment, closure or project cancellation then has no bond left to return, rather than failing on staked DREAM that no longer exists. The rate depends on where the DREAM comes from:
  - budget-backed project → `self_assigned_bond_rate` (default 10%)
  - permissionless project → `permissionless_self_assigned_bond_rate` (default 25%)

> Permissionless self-assignment used to be **exempt** from the bond, on the reasoning that no treasury budget was at stake. That reads the exposure backwards. A budget-backed initiative *moves* DREAM a council already approved; a permissionless one *mints* DREAM nobody approved, and the dilution lands on every holder. The exemption disabled the main economic deterrent precisely where no counterparty exists. Because `completer_share + treasury_share` is validated to equal 1, the initiative budget already **is** the amount minted — so the minting case needs a heavier rate, not a different base.

**Insider exclusion.** `InitiativeAffiliates` is the canonical set of addresses with an insider stake in an initiative's outcome: the assignee, the apprentice, the **author**, and the parent project's creator. Stakes from these addresses build total conviction but never count toward the external floor.

**Why these safeguards cluster here.** A payout has four independent brakes on
it, and they are unevenly distributed. Budget-backed work that somebody else
assigned has all four; permissionless work you assigned to yourself originally
had none:

| Brake | Budget-backed, externally assigned | Permissionless + self-assigned |
|---|---|---|
| Committee approval | Required before the project is ACTIVE | None — fee burn, immediately ACTIVE |
| Self-assign bond | 10% of budget locked | 25% of the amount to be minted |
| Budget | Finite, pre-allocated, already approved | An instruction to mint |
| Assigner | Someone else | Yourself |

The bond row is the one that was inverted: it used to be waived exactly where
the counterparty is missing. A budget-backed initiative **moves** DREAM
governance already approved; a permissionless one **creates** DREAM nobody
approved, and the dilution is borne by every holder. What guards that quadrant
today is the raised external-conviction floor (100%), the doubled challenge
window, the season mint cap, and the bond — which is why the definition of
"external" carries so much weight below.

**One invitation hop is excluded too.** `IsStakerExternal` also rejects a staker who is one invitation edge from an affiliate, in either direction: an account an affiliate invited, or the account that invited an affiliate. Membership on this chain comes from an invitation, and an invitation is a vouching relationship with a staked bond behind it, so neither party is an independent voice on the other's work.

This closes the cheapest route past the external floor, which was never to defeat it but to manufacture the electorate: invite a few accounts, gift them DREAM — `GiftOnlyToInvitees` (default true) permits exactly the inviter → own-invitee direction — and have them vouch for a self-assigned mint. The identity test alone counted every one of them as external.

Exactly **one** hop is excluded, deliberately. A one-hop test is an O(1) read of the `Member.invited_by` field that already exists, needing no new index and no walk of invitation records. A full subtree walk would be unbounded per-block work that any member can inflate for the price of more invitations — the same class of problem the conviction queue exists to bound. Two accounts sharing an inviter are two hops apart and still count as external.

Mechanically, `InvitationNeighborhoodOf` resolves the affiliates' inviters once per target and `IsStakerExternalTo` answers per staker against that, so the cost does not scale with staker count. The same test guards content-layer conviction (`GetExternalContentConvictionIn`), or routing conviction through linked content would bypass it.

What this still is **not**: it resists the invitation edge, not sybils generally. A ring assembled through an unrelated inviter is untouched, and staking has no trust-level gate — any member may stake, and membership comes from an invitation.

**Open (not built): weight external conviction by trust level.** A `NEW` account invited last week counts exactly as much toward the external floor as an `ESTABLISHED` member who has been earning for a season. Scaling each staker's contribution by trust level composes with the invitation-hop rule rather than replacing it — the hop rule removes voices that are structurally dependent, trust weighting discounts voices that are merely new. It is the cheapest remaining hardening after the hop, because trust level is already on the member record and already drives invitation credits and tier eligibility.

**The cost model this is defending against.** Worth recording, because it sets the priority. With default parameters, one cycle of the permissionless self-assigned loop looks like:

| Quantity | Value |
|---|---|
| Initiative creation fee (burned) | 3 DREAM |
| Refundable stake across ~4 backers | ~8.7 DREAM |
| Self-assign bond (25% of budget, returned on completion) | 125 DREAM |
| Minted to the assignee on completion | 450 DREAM |

Setup — the project fee plus the invitations needed to create backers — is roughly 45 DREAM and is paid **once**; the invited accounts stay members and can back all ten of one member's concurrent initiatives. The season cap (`MaxInitiativeRewardsPerSeason`, 100,000 DREAM) is the ceiling.

Two things about that table are easy to misread:

- **The bond is a capital requirement, not a cost.** It is refunded on completion, so against an attacker who expects to succeed it raises the bar and enlarges a challenger's burn target, but it does not raise the expected cost of a *successful* ring. That is what the external floor, the invitation-hop rule and trust weighting are for.
- **The rate limiter is time, not money.** A stake reaches full conviction weight only after roughly two half-lives (~14 days at defaults), which makes this a slow drain rather than a fast one. It is also not a defence: the wait is unattended.

The arithmetic is derived from the parameter defaults and was checked against a live chain (an 80 DREAM initiative shows exactly `0.2 x sqrt(80e6) = 1789` required conviction), but the full loop has not been run end to end on a devnet. Run it once before letting these numbers drive a priority decision.

```protobuf
message Initiative {
  uint64 id = 1;
  uint64 project_id = 2;
  string title = 3;
  string description = 4;
  repeated string tags = 5;
  InitiativeTier tier = 6;
  InitiativeCategory category = 7;

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
  repeated string approvals = 21;      // Advisory endorsements
  repeated string disapprovals = 28;   // Stake-weighted votes against

  InitiativeStatus status = 22;
  int64 created_at = 23;
  int64 completed_at = 24;

  string propagated_conviction = 25 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // Conviction propagated from linked content

  // DREAM locked by a self-assigning assignee (the author of the initiative or
  // of the parent project). Rate depends on whether the parent project is
  // budget-backed or permissionless. Returned on completion/abandonment,
  // burned on upheld challenge.
  string self_assign_bond = 26 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Address that submitted MsgCreateInitiative. Recorded on state so
  // authorship is answerable from a node query instead of only from the
  // initiative_created event. Immutable once set.
  string creator = 27;
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
  INITIATIVE_STATUS_CLOSED = 7;
}
```

`CLOSED` is the project side's terminal exit: the work is not being done and
its budget has gone back to the project. It carries no claim about the work
itself, which is what separates it from `COMPLETED` (delivered, gated, paid)
and `REJECTED` (judged and failed).

There is no terminal state for an assignee stepping down. That is
`MsgUnassignInitiative`, and it returns the initiative to `OPEN` rather than
retiring it — see [Releasing an assignment](#releasing-an-assignment).

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

  // The committee's verdict, set when an ADJUDICATION interim is completed.
  // Unset on every other type. Structured rather than parsed out of
  // completion_notes: a free-text keyword that fails to match (or matches the
  // wrong branch) used to leave the challenge it was raised to settle
  // unresolved forever, with the interim completed and nothing left to retry.
  AdjudicationDecision decision = 14;
}

// AdjudicationDecision is the Operations Committee's verdict on the challenge
// an ADJUDICATION interim was raised to settle — the terminal path for an
// inconclusive jury, and for challenges escalated when no jury could be seated.
enum AdjudicationDecision {
  ADJUDICATION_DECISION_UNSPECIFIED = 0;
  // The challenge stands: the work failed. Resolves the challenge as UPHELD.
  ADJUDICATION_DECISION_UPHOLD = 1;
  // The challenge fails: the work stands. Resolves the challenge as REJECTED.
  ADJUDICATION_DECISION_REJECT = 2;
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
| `JURY_DUTY` | Not created any more: jurors are paid per vote by `RewardJurors` at the tally (see Juror Pay) | n/a |
| `EXPERT_TESTIMONY` | Testifying as an expert witness a juror called (`MsgInviteExpertWitness`) and a second juror endorsed (`MsgEndorseExpertWitness`); system-only, paid when the review reaches its verdict if the endorsed expert testified | COMPLEX (400 DREAM) |
| `PROJECT_APPROVAL` | Reviewing a budget-backed project proposal; system-only, opened by `MsgProposeProject` for the picked council's Operations Committee, paid by `MsgApproveProjectBudget` to the members who took part in the approval (see Project Review Interims) | STANDARD (150 DREAM) |
| `BUDGET_REVIEW` | Same, for a proposal whose requested DREAM is at or above `budget_review_threshold` (default 5,000 DREAM) | COMPLEX (400 DREAM) |
| `CONTRIBUTION_REVIEW` | Reviewing founder contributions | STANDARD (150 DREAM) |

**Compensation Budgets by Complexity:**
- Simple: 50 DREAM
- Standard: 150 DREAM
- Complex: 400 DREAM
- Expert: 1,000 DREAM
- Epic: 2,500 DREAM (critical disputes). There is no `epic_complexity_budget`
  param: EPIC is derived as 2.5 x `expert_complexity_budget`, so it follows
  every change to the expert budget. (It used to return a hardcoded 2,500
  micro-DREAM, 0.0025 DREAM, while granting 100 reputation.)
- Solo Expert Bonus: +50% (when single assignee handles expert-level work)

**Who pays an interim.** Every interim except ADJUDICATION, EXPERT_TESTIMONY
and the two project reviews is paid only through
`MsgApproveInterim`, signed by the Commons Operations Committee policy address
(an executed committee proposal), the Commons Council policy or x/gov, never by
a single committee member (`ErrNotGovAuthority`); an approver that is one of the
assignees is refused (`ErrSelfApproval`, 2404). `MsgCompleteInterim` settles ADJUDICATION
interims only and refuses every other type (`ErrInterimNeedsApproval`, 2403). A
member may still propose paid work for themselves with `MsgCreateInterim` and
submit it with `MsgSubmitInterimWork`; the committee decides whether it is paid.
The ADJUDICATION, JURY_DUTY, EXPERT_TESTIMONY, PROJECT_APPROVAL and
BUDGET_REVIEW types are module-only: `MsgCreateInterim` refuses
them (`ErrInterimTypeSystemOnly`, 2401), refuses a deadline that is not after
the current height (`ErrInterimDeadlineInvalid`, 2402; a deadline of 0 asks for
the default, `interim_deadline_epochs × epoch_blocks` from now), and requires an
ACTIVE member. An ADJUDICATION interim resolves a challenge (on completion or on
expiry) only when the module raised it: its only assignee is the module
authority. Before this, a member could create an ADJUDICATION interim naming a
challenged initiative with a past deadline and force-reject the challenge on the
next EndBlock, or create an EXPERT interim and collect 1,500 DREAM (and EPIC
reputation up the trust ladder) by completing it in the next transaction.
`MsgAbandonInterim` accepts only PENDING or IN_PROGRESS non-ADJUDICATION
interims and emits `interim_abandoned`; on a project review it drops only the
signer's own seat (see Project Review Interims), on any other interim it
expires the whole interim.

**What counts against `max_active_interims_per_member`.** Only the member's
own live interims (`MsgCreateInterim` work, every non-system type). System
interims — a PROJECT_APPROVAL / BUDGET_REVIEW seat, an EXPERT_TESTIMONY
invitation, ADJUDICATION, JURY_DUTY — are work the module gave the member,
not work they took, and never count (REP-E-201: counting committee seats let
anyone fill the whole Operations Committee's cap with cheap proposals and
refuse every `MsgCreateInterim` and `MsgInviteExpertWitness` naming a
member of it). The count reads the `InterimsByAssignee` index (one prefix
walk per recent alias), not the pending set.

#### Project Review Interims

A budget-backed `MsgProposeProject` (`requested_budget > 0` or
`requested_spark > 0`) asks the Operations Committee of the council it is
pointed at (`project.council`, the committee that approves it) to review it,
and that review is paid through one system-settled interim whose id is stored
on `project.approval_interim_id`:

- **Opened at proposal time**, assigned to every current member of that
  council's Operations Committee (`ListCommitteeMembers(project.council,
  "operations")`); `reference_type = "project"`, `reference_id` = project id,
  `committee = "<council>_operations"`, deadline = the proposal's
  `expiry_block_height` (the review lives exactly as long as the proposal, so
  a decision anywhere in the window pays; `interim_deadline_epochs` backs
  only a record with no expiry). The type is `PROJECT_APPROVAL` (STANDARD
  budget), or `BUDGET_REVIEW` (COMPLEX budget) when `requested_budget >=
  budget_review_threshold` (governance-only param, default 5,000 DREAM). A
  committee seat is not a choice the member makes per interim, so
  `max_active_interims_per_member` is not applied to the assignees and the
  seat does not count against their cap. An empty committee skips the interim
  (`project_approval_interim_skipped`); any other failure is logged and
  emitted (`project_approval_interim_failed`) and never fails the proposal,
  which is the member's action, not the committee's pay.
- **Paid by `MsgApproveProjectBudget`, to the members who decided.** The
  approval is the committee's work, and only the assignees who took part in
  it are paid (each `budget / len(assignees)`; an unpaid seat's share is not
  redistributed and not minted): the ACTIVE assignees who voted (any option)
  on the council or committee proposal that executed the approval (x/commons
  marks the executing proposal on the context, `ExecutingProposalID`; rep asks
  `HasVoted` for each of the assignee's recent aliases; a secret ballot
  reports every member as a voter), or, for a small-budget approval signed by
  a single committee member, that member alone. The project's own creator is
  never paid, even when they sit on the committee (`SameMember`). Each skipped
  seat emits `interim_assignee_skipped` with its reason. The payout goes
  through the same path every interim uses (season cap
  `max_interim_rewards_per_season`, `treasury_funds_interims`, reputation,
  completed count); the interim becomes COMPLETED and
  `project_approval_interim_settled` (`paid_assignees`) is emitted. An
  approval nobody on the committee took part in (the gov authority) ends the
  interim EXPIRED unpaid. A refused payout (season cap reached, store error)
  is logged and emitted (`project_approval_interim_settlement_failed`) and
  never fails the approval; the interim stays live and the deadline sweep
  expires it unpaid.
- **A seat can be given up, the review cannot be voided.** `MsgAbandonInterim`
  by an assignee drops only that assignee's seat (`interim_assignee_removed`)
  and its share of the budget: the budget becomes `budget / seats` times the
  seats left, so each remaining seat is paid what it was before the abandon.
  Without the cut, members who would not vote could abandon before the
  approval and hand the whole budget to the one who did. The review stays
  live for the rest; only the last seat abandoning expires it. Zeroing an
  assignee drops their seat without the cut: the budget is split over the
  seats left at settlement (see Zeroing), which a member cannot trigger at
  will.
- **Ends unpaid without an approval**: a proposal cancelled by its creator or
  a council body (`MsgCancelProject`) or expired by the EndBlocker
  (`proposed_project_expiry_blocks`) moves its live review interim to EXPIRED
  (`project_approval_interim_cancelled`). A review that reached no decision
  earns nothing. The interim deadline sweep (`ExpireInterim`) expiring the
  review first changes nothing on the project: a later approval still goes
  through, and the expired review is not paid.
- **Members never settle one**: `MsgCreateInterim`, `MsgSubmitInterimWork`,
  `MsgApproveInterim` and `MsgCompleteInterim` all refuse the two types
  (`ErrInterimTypeSystemOnly`, 2401).

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
  STAKE_TARGET_BLOG_CONTENT = 4;         // Community conviction on x/blog posts
  STAKE_TARGET_FORUM_CONTENT = 5;        // Community conviction on x/forum posts
  STAKE_TARGET_COLLECTION_CONTENT = 6;   // Community conviction on x/collect collections

  // Author bonds (no DREAM rewards, slashable on moderation)
  STAKE_TARGET_BLOG_AUTHOR_BOND = 7;     // Author bond on x/blog content
  STAKE_TARGET_FORUM_AUTHOR_BOND = 8;    // Author bond on x/forum content
  STAKE_TARGET_COLLECTION_AUTHOR_BOND = 9; // Author bond on x/collect content
  STAKE_TARGET_BLOG_REPLY_AUTHOR_BOND = 10;  // Author bond on x/blog replies (own id sequence)
  STAKE_TARGET_BLOG_REPLY_CONTENT = 11;      // Community conviction on x/blog replies (own id sequence;
                                             // the self-stake check resolves BlogKeeper.GetReplyAuthor)
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

**Seasonal Reward Pool**: At the start of each season a staking reward budget is allocated, sized from what the chain actually produced rather than from the calendar:

```
activity = staking_pool_mint_share * (season_minted - season_staking_rewards_minted)
schedule = staking_pool_cap_base * (season + 1) * staking_pool_cap_rate
budget   = min(activity, schedule, max_staking_rewards_per_season)
```

The counters hold the outgoing season's totals when `InitSeasonalPool` runs, so `activity` is last season's production — known and fixed at the moment the budget must be set. Staking rewards are subtracted from that base so a season's emission cannot fund the next season's. A chain with no mint history at all (genesis) falls back to the schedule ceiling.

Each epoch distributes `min(pool / remaining_epochs, staking_reward_yield_per_epoch * total_staked)` pro-rata to initiative and project stakers. **Both halves of that minimum are load-bearing.** Per-unit yield is `pool / total_staked`, which does not self-adjust downward as the staked base shrinks — it diverges. A devnet running 1.47 DREAM as its entire staked base handed the whole season budget to three dust stakes; `acc_per_share` climbed until settling any one of them exceeded `max_dream_mint_per_epoch`, at which point every path that settles a stake reverted — claim, unstake, and initiative completion alike, stranding an initiative in `IN_REVIEW` past its challenge window. The per-epoch yield cap is what bounds that. What it withholds stays in `SeasonalPoolRemaining` rather than being distributed or dropped, so a season that opens quiet can still pay out in full if staking picks up before it closes. Likewise, an epoch with nothing staked leaves its slice in the pool rather than spending it into a void.

**Which half of that minimum binds, and what follows.** The two terms cross at
`total_staked = budget / (season_duration_epochs * staking_reward_yield_per_epoch)`
— with the default 25,000 DREAM budget over 150 epochs at 0.05%/epoch, that is
**333,333 DREAM staked**, roughly 13x the genesis DREAM supply. Below it the
yield cap binds and emission is simply `staking_reward_yield_per_epoch *
total_staked`: a flat interest rate, not a divided pool, with no dilution
between stakers and no competition for a fixed budget. `seasonalPoolBudget`'s
three sizing terms (`max_staking_rewards_per_season`, `staking_pool_mint_share`,
`staking_pool_cap_base`/`_rate`) therefore do not affect the outcome at any
staked base this chain will see soon. What they withhold now carries over:
`InitSeasonalPool` adds the outgoing season's unspent `SeasonalPoolRemaining`
to the incoming budget and re-caps the total at `max_staking_rewards_per_season`,
so a season that opened quiet pays out what was withheld once staking picks up,
in that season or the next, instead of the budget being discarded at the
rollover. They are a ceiling held in reserve for a chain
whose staked base has grown past the crossover; until then
`staking_reward_yield_per_epoch` is the only live knob. Net of
`staked_decay_rate` (0.025%/epoch) the real yield is ~0.025%/epoch — 3.75% over
a 150-epoch season.

**Why both were halved.** The gradient that actually drives staking is
`yield - staked_decay + unstaked_decay`. At the original 0.1/0.05/0.2 that is
0.25%/epoch, of which `unstaked_decay` supplies 0.2 — **80% of the reason to
stake is the penalty for not staking, not the reward for staking**. Halving the
staked pair to 0.05/0.025 moves the gradient to 0.225%/epoch, a 10% cut in the
incentive, while halving gross emission: delivering a real +0.05% previously
required minting 0.1% and burning 0.05%, twice the mint volume through
`max_dream_mint_per_epoch` for the same net position. The emission freed this
way went to `max_completion_bonus_stake_multiple` (2.0 -> 3.0), the one payout
conditioned on work actually shipping — the flat seasonal yield pays the same
whether a stake backs a live initiative or a dormant tag, so it cannot be the
lever that encourages work.

The pool is seeded in `InitGenesis` and refilled by x/season at the end of each season transition (`RepKeeper.InitSeasonalPool`). The epoch slice is taken in `EndBlocker` step 8, gated on `IsEpochEnd` — the distribution call itself is per-block, so without that gate a season's whole budget would drain `epoch_blocks` times too fast. The slice's `remaining_epochs` denominator is anchored to the epoch stored at pool initialization (`SeasonalPoolStartEpoch`, written by `InitSeasonalPool`), not to a modulo of the raw epoch counter: seasons are driven by x/season, whose `SeasonDurationEpochs` is a separate param from x/rep's, and the anchor makes the drain schedule follow wherever the boundary actually fell, re-anchoring at every rollover.

**Reward accounting (MasterChef)**: All four reward-bearing target types share one settlement path, `settleStake` in [x/rep/keeper/stake_settlement.go](../x/rep/keeper/stake_settlement.go). Every site that mutates a stake's amount or pays it out — create, claim, compound, unstake, initiative completion — goes through it, and it dispatches on target type to the accumulator that owns the stake: the shared seasonal `acc_per_share` for `INITIATIVE` and `PROJECT`, the per-target `MemberStakePool` / `TagStakePool` accumulator for `MEMBER` and `TAG`. Two invariants make the pattern correct and are both maintained there:

- `reward_debt` is set to `amount × acc_per_share` at join and rebased to `new_amount × acc_per_share` on every settlement. This is what makes a new staker's pending balance start at zero rather than at the pool's whole accumulated history, and it is why a stake placed just before a distribution earns nothing from it. A partial withdrawal rebases against the *remaining* amount, so a shrunken stake does not sit under a debt sized for its original principal.
- `total_staked` moves in lockstep with `stake.Amount` in both directions, via `updateStakePoolTotals`. A project stake moves two denominators (its own `ProjectStakeInfo` and the seasonal pool). `ReconcileStakePoolTotals` recomputes every denominator from live stakes and runs at genesis import.

**Accumulator travels with genesis; the rebase is a legacy fallback.** `acc_per_share` is exported in `EconomicState.seasonal_pool_acc_per_share` alongside each stake's `reward_debt`, so an import restores both and pending seasonal rewards survive the round trip exactly. Only a genesis that does not carry the accumulator (older exports) resumes with it back at zero while every stake still holds a debt taken against the old one; for that case alone `RebaseStakeRewardDebt` runs from `InitGenesis` (after `ReconcileStakePoolTotals`) and re-measures every initiative and project stake against the accumulator it will actually earn from. Without it a stake with positive debt silently earns nothing until the accumulator climbs back past its stale figure, and a stake whose debt is zero because it predates any distribution keeps claiming from zero against whatever the accumulator later becomes.

**Accumulator is monotonic across seasons.** `InitSeasonalPool` re-sizes `SeasonalPoolRemaining` but does *not* reset `acc_per_share`. Since each stake's `reward_debt` is a snapshot of the accumulator, zeroing it at a season boundary would leave every surviving stake holding a debt larger than the accumulator it is measured against, silently paying nothing until the new season climbed back past the old value. Per-season budgeting is enforced by `SeasonalPoolRemaining` alone.

**Minimum holding period.** `min_stake_duration_seconds` (24 hours) gates reward collection, not principal return. Claiming or compounding earlier is rejected with `ErrStakeMinDurationNotMet` and forfeits nothing — the debt is untouched, so rewards keep accruing. Unstaking earlier always returns the principal but forfeits the accrued rewards: the debt is rebased without minting, so the DREAM is never created and simply stays in the pool for the remaining stakers.

**It must fit inside a season.** Set longer than `epoch_blocks *
season_duration_epochs * 6` seconds, the gate stops being a holding period and
becomes an unconditional forfeiture: no stake opened in a season can ever claim
within it, and every stake closed within it loses everything it accrued. Devnet
shipped that way — a 24h gate over a 15h season — because both values were
copied from production independently and nothing related them. Devnet now pins
1800s and the local `config.yml` 60s; `TestRepMinStakeDurationFitsSeason` in the
cross-network invariants enforces the relation on every network.

**Compounding is member/tag only.** `MsgCompoundStakingRewards` rejects `INITIATIVE` and `PROJECT` stakes with `ErrCompoundNotSupported`. Growing those principals in place would give the added DREAM the conviction maturity of the original `created_at` — the exact exploit that separate stake tranches exist to prevent. Those stakers claim and re-stake, which routes the new DREAM through `CreateStake`'s per-member cap and starts it on a fresh maturity clock.

**Tranche cap.** Stakes are never merged: each carries its own `created_at` maturity clock and its own `reward_debt` baseline, and averaging two joins made at different times or accumulator values would break both. `types.MaxStakeTranchesPerTarget` (10) bounds how many records one member may hold on a single target, which also keeps `CreateStake`'s O(n) per-member cap check from making n tranches cost O(n²) to accumulate.

### Conviction refresh scheduling

Initiative conviction is recomputed from a due-time-ordered work queue rather than by sweeping every stake of every active initiative on every block. The old sweep made per-block validator work scale linearly with a stake count any member could inflate for a fully refundable amount of DREAM — a one-time transaction fee bought permanent, unmetered per-block work on every validator. See [x/rep/keeper/conviction_queue.go](../x/rep/keeper/conviction_queue.go).

Three things drive the queue:

- **Event-driven.** Anything that changes conviction inputs reschedules the affected initiative to "due now". A stake on the initiative itself is additionally recomputed synchronously, since that work is gas-metered on the staker's own transaction and they should see the effect immediately. A stake on *linked content* is not recomputed inline — it can fan out to many initiatives — so those are marked due via the `InitiativesByContent` reverse index and picked up by the next EndBlocker. That reverse index is what makes an incremental refresh correct: without it, propagated conviction would silently stop tracking content stakes.
- **Self-rearming.** Conviction also drifts with no external event, because each stake's time weighting climbs until it matures. After every recompute an initiative re-arms itself: at `half_life / 8` while any of its stakes is still maturing (so conviction is sampled ~16 times across the maturity window regardless of how `conviction_half_life_epochs` and `epoch_blocks` are configured), and at `ConvictionStableRefreshSeconds` (6 hours) once they all have. A matured initiative's conviction only moves via reputation decay, so a multi-hour cadence keeps the error negligible — and it removes dust stakes from the per-block cost model entirely once they mature.
- **Bounded per block.** `DrainConvictionQueue` scans only the due prefix and stops once `MaxConvictionStakeUpdatesPerBlock` (500) stake-level recomputations are spent. An initiative is always processed whole, since conviction is meaningless from a partial stake set, so the budget is checked between initiatives. Leftover work rolls to the next block.

Consequences worth knowing:

- A saturated queue delays conviction *freshness*; it cannot inflate block time. That is the intended failure mode.
- `CanCompleteInitiative` (EndBlocker) reads stored conviction, so an initiative can cross its threshold up to one refresh interval late — by construction, ~1/16th of the maturity window. The `InitiativeConviction` query recomputes on demand, so a caller who asks directly never sees a stale figure.
- Initiatives that leave the active status set are dropped from the queue by the drainer itself, so completion, cancellation, abandonment and expiry are all covered without a hook in each transition.
- The queue and the `InitiativesByContent` reverse index are derived state and are not exported in genesis. `InitGenesis` rebuilds the reverse index from `ContentInitiativeLinks` and calls `RearmConvictionQueue`, which is also available as a recovery hatch if the queue is ever suspected of having drifted.

**Every derived index must be rebuilt on import.** Derived indexes are not exported, so `InitGenesis` rebuilds all of them from the primary collections: `Project`, `Initiative` and `Challenge` by status, `JuryReview` by verdict and by juror, `Stake` by target, `Interim` by status, `ContentChallenge` by status and by target, and `Invitation` by invitee. Covered by `TestGenesisRebuildsDerivedIndexes` and `TestGenesisRebuildsStakeAndInvitationIndexes`. The verdict index is the one exception to "rebuilt from the records alone": content-challenge reviews and `CreateAppealInitiative` reviews (gov-action appeals, migration objections, forum appeals) are deliberately kept out of it, so the export lists them in `jury_reviews_outside_verdict_index` and `InitGenesis` indexes every other review (a hand-written genesis without the list indexes them all).

**Genesis round trip is exact.** An export followed by an import reproduces the store except for state documented as not carried: the trust tree (marked dirty and rebuilt in the next EndBlock), the conviction queue (`RearmConvictionQueue` re-arms every accruing initiative as due), the rolling `SubmittedCheckCursor`, the decay-pass cursors, and stale entries in the lazily drained `due/` deadline queues (import enqueues only live initiatives and challenges; an entry left behind by an early resolution would be dropped by the sweep anyway). In particular:
- `ExportGenesis` starts from `DefaultGenesis`, so it resets `reserved_tag_map` before walking the live set: a removed default reserved tag stays removed, a kept one is not listed twice.
- The per-(budget, post) award cooldown travels as `tag_budget_award_cooldown_list` with its heights; only an award without an entry restarts its cooldown at the import height.
- The per-awarder epoch spend ledger (`TagBudgetAwarderEpoch`) travels as `tag_budget_awarder_epoch_list`, so an import inside an epoch keeps each awarder's epoch cap.
- `EconomicState`'s epoch fields are imported even when zero if their partner is set (`seasonal_pool_season` / `seasonal_pool_start_epoch` with `seasonal_pool_remaining`, `epoch_minted_epoch` with `epoch_minted_amount`), so a pool opened, or a mint made, in epoch 0 round-trips.
Covered by `TestGenesis_RoundTripMatchesLiveState` and the app-level `TestAppImportExport`.

> The last four were missing while the first four carried comments explaining why they mattered. Their absence was not cosmetic:
>
> - **Stake by target** is the worst. Conviction is recomputed from `GetInitiativeStakes`, which reads this index, so every imported stake was invisible — initiatives could never reach their threshold, and `CompleteInitiative` settled nothing, leaving the principal stranded in records no payout path could see.
> - **Invitation by invitee** is what `ProcessInviterAccountability` resolves an invitee's invitation through, so an unrebuilt index silently disables the invitation slash on top of failing the duplicate-invitation guard open.
> - **Interim by status** is walked by the EndBlocker expiry sweep; without it no imported interim ever expires.
> - **ContentChallenge by status** is walked by the unanswered-challenge sweep, and **by target** enforces one live challenge per content item, which otherwise fails open. Only unresolved statuses (`ACTIVE`, `IN_JURY_REVIEW`) reclaim the target slot — `isLiveContentChallenge` — since every terminal transition frees it.

**The economic ledger travels in genesis.** x/rep keeps DREAM outside x/bank, so
its treasury, season counters, seasonal pool and per-epoch mint allowance are
recoverable from nowhere else. `GenesisState.economic_state` (field 43) carries
them; `GiftRecordEntry` (field 44) carries the per-recipient gift cooldowns, and
the long-present pool fields 17-19 are finally populated. Without this an import
was not a partial restore but a different economy: zero treasury, zero season
counters (re-opening every per-season cap mid-season), a reset mint allowance,
and a decay clock that re-applied an extra epoch. The decay pass in flight
(`decay_pass_epoch`, `decay_pass_member_cursor`, `decay_pass_stake_phase`,
`decay_pass_stake_cursor`, EconomicState fields 16-19) travels too, so an
export taken mid-pass resumes it instead of decaying the already-visited stake
records a second time (REP-E-108); `Validate` refuses cursors without a pass.

It also made `InitGenesis`'s "only seed an uninitialised pool" guard dead code —
`SeasonalPoolRemaining` always read zero on import, so every import refilled a
whole season's budget over whatever the exporting chain had left. `InitGenesis`
now restores the ledger *before* that guard and skips seeding when a pool with a
remaining budget was imported.

Two halves are deliberately NOT carried, because they are derived and
`ReconcileStakePoolTotals` owns them: the pool `total_staked` denominators
(recomputed from live stake records) and `SeasonalPoolTotalStaked`. The
accumulators are the half that cannot be recomputed from anything, which is why
they have to travel. Regressions: `TestGenesisRoundTripsEconomicLedger`,
`TestGenesisRoundTripsMemberAndTagPools`.

**Registered invariants.** `RegisterInvariants` registers four checks
(run by `app/invariants_test.go`; x/crisis is not wired), where the module previously registered none — so a drifted pool
denominator, an inverted staked/balance relation, a negative treasury, or an
emission counter past its cap were all undetectable on-chain:
>
> | Invariant | Catches | Grade |
> |---|---|---|
> | `seasonal-pool-denominator` | `SeasonalPoolTotalStaked` drifting from the live initiative+project stake sum — every seasonal payout divides by it | halting |
> | `member-staked-within-balance` | `staked_dream` exceeding `dream_balance` (it is a subset) or either going negative | halting |
> | `treasury-non-negative` | a treasury spend path failing to clamp | halting |
> | `season-caps-not-exceeded` | an emission counter above the cap that gates it, i.e. a payout that minted without charging the gate | warning — a governance cap cut mid-season legitimately leaves the counter above it |

> This is a correctness requirement, not housekeeping. `HasActiveChallenges` reads the challenge-by-status index and `CanCompleteInitiative` reads that, so an unrebuilt challenge index makes an unresolved challenge **invisible** — and a challenged initiative pays out after a genesis restart. Only `Project` was rebuilt originally; the other three were not. Any new derived index has to be added here at the same time it is added to the keeper.

`MaxConvictionStakeUpdatesPerBlock` and `ConvictionStableRefreshSeconds` are compile-time constants rather than governance params — they bound unmetered EndBlocker work, so a value set too high is itself a liveness risk, and changing one belongs with a change to the sweep's cost model in a chain upgrade. This matches the local convention (`maxTagExpirations` in the same module) rather than the param-driven batching used by x/collect and x/federation.

**Completion bonus is for external stakers only.** The bonus rewards independent vouching, so it is paid only to stakers who pass the same test the external-conviction floor uses — `InitiativeAffiliates` plus the one invitation hop. It previously excluded the assignee and apprentice alone, which made it the third and last place in the module defining affiliation differently from the other two: the initiative's own author and the parent project's creator were paid as though they were arm's-length backers, on top of the completer share the assignee already receives. Insiders staking on their own commission are not vouching for it. Their principal is untouched — stakes are settled and returned regardless — and the withheld share is simply never minted rather than redistributed, so excluding insiders reduces emission instead of concentrating it.

The pool is `initiative_completion_bonus_rate` (default 0.1) of the budget, **capped
at `max_completion_bonus_stake_multiple` (default 3.0) times the external DREAM
actually staked behind the initiative**:

```
bonus_pool = min(initiative_completion_bonus_rate * budget,
                 max_completion_bonus_stake_multiple * external_stake)
```

The rate was a hardcoded 1/10 divisor while the project-side
`project_completion_bonus_rate` was already a parameter — the same economic knob,
tunable on one side and fixed on the other.

**Why the bonus cannot be priced off the budget alone.** `required_conviction`
is `conviction_per_dream * sqrt(budget)` and each staker supplies `sqrt(stake)`,
so N stakers clear the gate with `conviction_per_dream^2 * budget / N` between
them — 4% of the budget at one staker, 1.33% at three, 0.4% at ten. The capital
needed falls as `1/N` while a fixed share of the budget does not, which made
each staker's return `2.5 * N` times their own stake: 7.5x at three stakers,
25x at ten, 62.5x at twenty-five, risk-free on any initiative that completes,
bounded only by neighborhood independence and `max_initiative_rewards_per_season`.

Capping against capital at risk holds the return at
`max_completion_bonus_stake_multiple` regardless of how many stakers split it,
and restores the ~4% stake-to-budget ratio the conviction formula was designed
around: below roughly `bonus_rate / multiple` of the budget staked, the stake
term binds and the bonus scales down with the capital behind it. The falling
capital-per-participant is the intended quadratic-funding shape and is left
alone — only the payout is repriced.

**The bonus is weighted on the same conviction the gate counts.** Raw conviction
is aggregated per staker, the sqrt is taken once on that aggregate, and the
result is capped at `max_conviction_share_per_member` — exactly what
`updateInitiativeConvictionWithStakes` does. Taking the sqrt per *stake record*
instead, which the payout did originally, pays a member who splits one position
across `k` tranches `sqrt(k)` times as much for identical capital: ten tranches
of 0.049 DREAM weigh 2,214 where one 0.49 DREAM stake weighs 700. The completion
gate has guarded against stake splitting since it was written; the payout beside
it did not, so the exploit the gate refuses to reward was rewarded one function
later. Regression: `TestCompletionBonus_SplittingAStakeGivesNoAdvantage`, and
`TestCompletionBonus_CapsAtAMultipleOfTheStakeBehindIt` for the cap.

Applying the per-member conviction cap to the payout also means a staker whose
conviction was capped for the purpose of unlocking the budget is paid on the
capped figure, not an uncapped one — two stakers both above the cap are paid
equally no matter how far above it they are.

**Minimum stake.** `min_stake_amount` (default 1,000 micro-DREAM = 0.001 DREAM)
floors every stake `MsgStake` can open. There was no floor, and every weighting
a stake feeds is either per-record or sqrt-scaled, so a member could open the
ten tranches `MaxStakeTranchesPerTarget` allows at one micro-DREAM each and
still count as a participant everywhere conviction is weighed. The default is
the point below which a stake cannot earn at all — an epoch slice is
`staking_reward_yield_per_epoch * total_staked` truncated to an integer, which
is zero under 1,000 micro-DREAM at the default yield — so it is state hygiene
rather than an economic gate. What bounds the return on a small position is
`max_completion_bonus_stake_multiple`.

**Season cap gate.** `CompleteInitiative` checks `MaxInitiativeRewardsPerSeason` against *every* DREAM the completion will create — completer share, treasury share, the conviction-weighted staker bonus, and the reviewers' fee — not just the first two. All four are freshly minted inside the same function; the last two used to be minted *after* the gate and counted only afterwards, so a completion could be admitted and then mint past the cap it had just cleared, overrunning by up to ~15% of that initiative's budget.

Both added projections are upper bounds and are skipped when the payout cannot happen: the staker bonus is projected only when the initiative has stakes, the review fee only when the round has verdicts. Where they do apply the estimate can still exceed the actual mint, since per-recipient shares truncate and the bonus pays nothing if no external staker holds conviction. The gate is therefore conservative — it can refuse a completion that would have fitted, never admit one that does not. For a cap, that is the correct direction to err.

**Completion bonus ordering.** `CompleteInitiative` distributes the conviction-weighted completion bonus (`initiative_completion_bonus_rate`, default 1/10th of budget) *before* the payout loop deletes the stake records — the weighting is derived from `stake.created_at`, so running it afterwards leaves it nothing to weight. The bonus mint is tracked against `MaxInitiativeRewardsPerSeason` alongside the completer and treasury shares, **and is projected at the cap gate before any minting** — see "Season cap gate" below. The project-side equivalent (`ProjectCompletionBonusRate`) mints directly to stakers; `ProjectStakeInfo.completion_bonus_pool` is vestigial and stays at zero, since nothing is escrowed.

**Staked Decay**: Reward-bearing stake records (initiative, project, member, tag, content conviction) decay at `StakedDecayRate` (0.025%/epoch, ~8.7% annualized compounded), applied once per epoch by `decayStakes` in the EndBlocker bulk decay pass. Each stake's `Amount`, its pool denominators (seasonal pool, per-project info, member/tag pools), its `reward_debt` (scaled with the principal so the pending claim shrinks proportionally instead of clamping to zero), and the staker's `staked_dream`/`dream_balance`/`lifetime_burned` all move in lockstep, so the member aggregate always equals the sum of the obligations backing it and every unlock can be paid in full. This replaces the earlier design that decayed only the `member.StakedDream` aggregate: that left every obligation at face value against a shrinking aggregate, and `UnlockDREAM`'s cap-to-actual fallback dumped the whole shortfall on whoever unlocked last. Escrow-style locks — invitation stakes, challenge stakes, bonded roles, review bounties, report bonds, author bonds — do not decay; a bond that must be slashable at full face value cannot erode. Idle stakes still erode (active stakers earning from the seasonal pool outpace 0.025%), but escrow does not.

**Locked DREAM backs exactly one escrow.** `staked_dream` is the sum of every
escrow the member holds (stake records, challenge stakes, invitation stakes,
bonded-role bonds, author bonds, bounty and report bonds), and the DREAM
primitives keep it backed:

- `BurnDREAM` (fees and every other plain burn) spends only the **unlocked**
  balance, `dream_balance - staked_dream`. A fee paid out of locked DREAM left
  the balance below the locks, and the escrow's later slash failed and wedged
  the challenge or initiative waiting on it.
- `BurnLockedDREAM(addr, amount)` slashes an escrow: it releases `amount` from
  the lock and burns it in one step. Every in-module slash uses it (rejected
  challenge and content-challenge stakes, slashed bonded-role, author and
  self-assign bonds, review-bounty draws, the invitation acceptance burn, a
  dismissed tag report's bonds). Other modules keep the "unlock, then burn"
  pair, which `internal/dreamutil.Ops.BurnLocked` wraps.
- `UnlockDREAM` is strict: with less than `amount` locked it fails with
  `ErrInsufficientStake` and releases nothing. It used to cap the release at
  the aggregate, which on a shortfall released other escrows' locks.
- `LockDREAM` refuses a member that is not ACTIVE (`ErrMemberNotActive`). A
  zeroed member therefore never locks again, so any escrow record still naming
  it unlocks against nothing instead of against a lock that backs something
  else.
- `decayStakes` charges each staker through the alias (records keep the
  address they were opened under) and floors the aggregate charge at zero.

The `member-staked-within-balance` invariant (`staked_dream <= dream_balance`)
checks this in `TestInvariants`.

**Content conviction stakes decay too**, and originally did not. They were exempt on the stated grounds that content conviction "already time-decays via the conviction half-life" — it does not. Both `CalculateContentConviction` and `CalculateRawStakeConviction` compute `time_factor = t / (2 * half_life)` capped at 1.0: a linear ramp to a permanent maximum, not a half-life, despite the names `ContentConvictionHalfLifeEpochs` and `ConvictionHalfLifeEpochs`. Content stakes are locked (so exempt from the 0.2%/epoch unstaked decay), earn no DREAM, and propagate conviction into initiative conviction — which made them a costless shelter strictly better than holding DREAM, with a governance benefit attached. Decaying the principal is also what makes content conviction genuinely erode, since conviction is `amount * time_factor` and only the amount can carry the decay. `updateStakePoolTotals` routes these through `adjustMemberContentStaked`, so the per-member content aggregate moves in lockstep. Regression: `TestDecayStakes_DecaysContentConviction`.

**Terminal-project stakes decay; PROPOSED-project stakes do not.** Staked decay is the cost of holding a staked position, not merely a brake on compounding, so a stake on a completed or cancelled project keeps eroding — its principal is freely withdrawable and decay is the nudge to withdraw it, and exempting it would create a decay-free store of value dominating both unstaked DREAM and live stakes as a place to park. A stake on a *PROPOSED* project is the opposite case: `stakeAccruing` freezes it out of the seasonal pool, so charging it decay is a pure levy on backing work at the earliest and least certain moment — exactly the conviction the system exists to buy. That window is bounded (approval starts accrual and rebases the debt; rejection ends the stake), so the exemption creates no lasting shelter. Regression: `TestDecayStakes_ExemptsProposedProjectStakes`.

**New Member Grace Period**: Members who joined fewer than `NewMemberDecayGraceEpochs` (30 epochs, ~1 month) ago are exempt from both unstaked and staked decay, giving them time to earn and stake DREAM before decay applies. The window is measured from `Member.joined_at_height` — the height-domain twin of the `joined_at` unix timestamp — as an honest epoch count (`current_epoch − joined_at_height/epoch_blocks < grace_epochs`). It used to be derived by dividing the `joined_at` timestamp by `epoch_blocks` as though it were a block height, which computed a hugely negative age for every invited member and made the grace window perpetual; members restored from state written before `joined_at_height` existed carry 0, which reads as "joined at genesis" and exits grace after the same 30 epochs.

### Challenge

```protobuf
message Challenge {
  uint64 id = 1;
  uint64 initiative_id = 2;
  string challenger = 3;

  string reason = 4;
  repeated string evidence = 5;
  string staked_dream = 6 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  ChallengeStatus status = 7;
  int64 created_at = 8;
  int64 resolved_at = 9;
  int64 response_deadline = 10;   // Block height; auto-uphold if assignee doesn't respond

  // The acceptance criterion this challenge says the work fails, when the
  // initiative declared any. Optional — a challenge may still be a free-form
  // claim — but naming one turns "the work is bad" into a question the jury can
  // actually adjudicate against a standard the author agreed to up front.
  string criteria_id = 11;

  // The initiative status to restore if this challenge is rejected. Challenges
  // may be filed while the work is still SUBMITTED — before its challenge
  // window has opened — and rejecting such a challenge must not skip that
  // window: restoring SUBMITTED hands the initiative back to the ordinary
  // transition, which sets a fresh review + challenge period. Challenges filed
  // on IN_REVIEW work restore IN_REVIEW, whose window has already run. The
  // zero value (OPEN, on chains predating the snapshot) also restores
  // IN_REVIEW, the historical behaviour.
  InitiativeStatus status_before_challenge = 12;
}

enum ChallengeStatus {
  CHALLENGE_STATUS_ACTIVE = 0;
  CHALLENGE_STATUS_IN_JURY_REVIEW = 1;
  CHALLENGE_STATUS_UPHELD = 2;
  CHALLENGE_STATUS_REJECTED = 3;
  CHALLENGE_STATUS_VOIDED = 4;
}
```

**The assignee cannot challenge their own initiative** (`ErrSelfChallenge`,
1408). Challenges are filed on `SUBMITTED` or `IN_REVIEW` work by any member
*except* the assignee, and the gate runs before the stake is locked so a
rejected self-challenge costs nothing. The rule closes a self-dealing loop
verified live on the devnet: an assignee who had decided to fail could
challenge their own work, respond with an empty string (auto-upheld — an
admission of fault), pocket the 20% challenger reward minted on uphold, and
re-earn the slashed reputation on the next completion — a net-positive payout
for failing. This mirrors the content-challenge rule
(`ErrCannotChallengeOwnContent`, 2004); the initiative path had simply never
gotten the same guard. The project *creator* remains eligible to challenge
work they did not assign to themselves: an upheld challenge burns the
assignee's standing and the creator still has to survive the jury or
committee to collect.

Note that the reward is freshly *minted* DREAM, not a transfer out of the
initiative budget — it is sized at `budget × challenger_reward_rate`, and the
budget itself is returned to the parent project untouched (skipped for
permissionless projects, which never pre-allocated one). So an upheld challenge
is paid by inflation and the budget only sets the scale.

**Limits on the challenger reward.** Two accounts could run the loop the
self-challenge gate closed: one assigns itself the other's initiative, the
other challenges, and the assignee never answers, so the challenge is
auto-upheld and 20% of a self-declared budget is minted. The reward is now:

- capped at the self-assign bond the assignee forfeits when the challenge is
  *conceded* rather than decided: an empty response (`TriageAutoApprove`) or no
  response by the deadline (`UpholdConcededChallenge`). An externally assigned
  initiative has no bond, so a conceded challenge returns the stake and mints
  nothing;
- zero when the challenger is an insider of the work (assignee, apprentice,
  author or project creator, compared canonically) or one of the challenger
  and the assignee invited the other;
- booked against `max_initiative_rewards_per_season` and clamped to what is
  left of it.

A jury or committee verdict (`UpholdChallenge`) pays the full reward subject to
the last two rules. The `challenge_upheld` event carries `challenger_reward`
and `conceded`.

**Scope: the gate covers the directly-signed path only.** It compares
`initiative.assignee` against the message signer, and on the shielded path
that signer is x/shield's module address, not the real challenger — x/shield
*requires* the inner `MsgCreateChallenge` to be signed by its own module
account (`ErrInvalidInnerMessageSigner`), so the comparison can never match.
A shielded self-challenge is therefore not rejected by this rule. It does not
currently succeed either, for an unrelated reason: `CreateChallenge` locks the
challenger's stake via `LockDREAM`, which requires a `Member` record, and the
shield module address is not seeded as a member — so the message fails with
`ErrMemberNotFound` at the stake lock. Anonymous challenges are specified but
not reachable end-to-end today; `test/rep/anon_challenge_test.sh` asserts only
the x/shield registration metadata and that immediate mode is refused, and
never drives a shielded challenge to completion, so the above is from code
inspection rather than an executed path. **If that path is ever completed, the
self-challenge rule must be re-derived from the shielded identity rather than
the signer**, or the loop this section closes reopens behind the shield.

`CHALLENGE_STATUS_VOIDED` terminates a challenge without a verdict when the
underlying initiative is discarded out from under it — currently only when the
parent project is cancelled (see the "Cancelling a Project" section). The
challenger's stake is refunded in full (no burn, no reward) and any pending
jury review is closed `INCONCLUSIVE`.

**A rejected challenge restores the pre-challenge status.** `CreateChallenge`
snapshots the initiative's status into `Challenge.status_before_challenge`, and
`RejectChallenge` puts the initiative back where it was:

- challenged while `IN_REVIEW` (the ordinary case — the window has run)
  restores `IN_REVIEW`, ready to complete;
- challenged while `SUBMITTED` (legal, but the challenge window has **not**
  opened — `ChallengePeriodEnd` is still 0, since only
  `TransitionToChallengePeriod` sets it) restores `SUBMITTED`, re-entering the
  ordinary transition so a real window opens. The zero-value snapshot (chains
  predating the field) restores `IN_REVIEW` like the historical behaviour.

The SUBMITTED case is the load-bearing one: before the snapshot, a rejected
challenge flipped the initiative to `IN_REVIEW` with `ChallengePeriodEnd == 0`,
the completion sweep read `height >= 0` as "window elapsed", and the work paid
out in the next block — skipping the entire review + challenge window the
completion guard promises, for the price of one refundable challenge stake.
Two defenses back the snapshot up: `CompleteInitiative` treats
`ChallengePeriodEnd == 0` in `IN_REVIEW` as an unopened window
(`ErrChallengePeriodActive`), and the EndBlocker's completion sweep **adopts**
an initiative in that shape by opening a full window (event
`initiative_challenge_window_opened`) rather than completing or freezing it.

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

**Upholding slashes the bond that is there.** `UpholdContentChallenge` loads
the author bond record and burns its own lock (`BurnLockedDREAM`, capped at
`bond_amount`); the challenger's reward is `content_challenge_reward_share` of
what was actually burned. When the bond is already gone (a sentinel hide
slashed it while the jury deliberated, or the author was zeroed), nothing more
is burned and no reward is minted. The snapshot used to be unlocked from the
author's aggregate lock regardless, which burned another escrow of theirs (an
initiative stake) and minted the challenger a reward with no bond behind it.
`bond_amount` stays as the record of what was at stake when the challenge was
filed; the `content_challenge_upheld` event's `bond_slashed` is the amount
actually burned.

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
  // ... (content_challenge_id = 15 through pending_seats = 20; the queued
  // draw itself is PendingJuryDraw{jury_review_id, seed_epoch,
  // queued_at_height}, see "Jury Selection")
  // The EXPERT_TESTIMONY interim each invitation opened, parallel to
  // expert_witnesses; the verdict settles exactly these ids.
  repeated uint64 expert_interim_ids = 21;
  // The seat that issued each invitation and the seat that endorsed it
  // ("" while unendorsed), both parallel to expert_witnesses. Only an
  // endorsed invitation testifies or is paid (REP-E-103).
  repeated string expert_inviters = 22;
  repeated string expert_endorsers = 23;
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

### VerificationCriteria

An initiative's definition of done, declared at creation and immutable after.
Lives in `acceptance_criteria.proto`. It previously sat beside an
`InterimTemplate` registry, deleted because no message ever created one, all
three networks shipped zero, and `Initiative.template_id` resolved against it.

```protobuf

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

### Tag and ReservedTag

`DefaultGenesis()` seeds `reserved_tag_map` with `DefaultReservedTags()`: today the single `service-operator` tag that x/service writes operator reputation to (x-service-spec §6.6), so no member can create or squat it through `MsgCreateTag`. A network's `config.yml` may add reserved tags but should keep these.

```protobuf
// proto/sparkdream/rep/v1/tag.proto
message Tag {
  string name              = 1;  // lowercase alphanumeric + hyphens
  uint64 usage_count       = 2;  // total uses across modules
  int64  created_at        = 3;
  int64  last_used_at      = 4;  // drives GC: tag expires at last_used_at + DefaultTagExpiration; 0 = permanent
}

// proto/sparkdream/rep/v1/reserved_tag.proto
message ReservedTag {
  string name             = 1;
  string authority        = 2;  // empty = council-only
  bool   members_can_use  = 3;
}
```

Validation: `ValidateTagFormat` / `ValidateTagLength` from `x/common/types` (pure functions, no state).

### TagReport

```protobuf
// proto/sparkdream/rep/v1/tag_report.proto
message TagReport {
  string   tag_name         = 1;
  string   total_bond       = 2;
  int64    first_report_at  = 3;
  bool     under_review     = 4;
  repeated string reporters = 10;
}
```

### TagBudget and TagBudgetAward

```protobuf
// proto/sparkdream/rep/v1/tag_budget.proto
message TagBudget {
  uint64 id              = 1;
  string group_account   = 2;
  string tag             = 3;
  string pool_balance    = 4;
  bool   members_only    = 5;
  int64  created_at      = 6;
  bool   active          = 7;
}

// proto/sparkdream/rep/v1/tag_budget_award.proto
// (award record schema — one row per `MsgAwardFromTagBudget` call)
```

### BondedRole (generic accountability primitive)

> **Note:** Phase 1–4 of the bonded-role generalization (see [bonded-role-generalization.md](untracked/bonded-role-generalization.md)) replaced the former standalone `SentinelActivity` proto with a role-typed `BondedRole`. Per-module action counters (hides, reviews, verifications) stay in the owning module.

```protobuf
// proto/sparkdream/rep/v1/bonded_role.proto
enum RoleType {
  ROLE_TYPE_UNSPECIFIED         = 0;
  ROLE_TYPE_CONTENT_SENTINEL      = 1;
  ROLE_TYPE_COLLECT_CURATOR     = 2;
  ROLE_TYPE_FEDERATION_VERIFIER = 3;
}

enum BondedRoleStatus {
  BONDED_ROLE_STATUS_UNSPECIFIED = 0;
  BONDED_ROLE_STATUS_NORMAL      = 1;
  BONDED_ROLE_STATUS_RECOVERY    = 2;
  BONDED_ROLE_STATUS_DEMOTED     = 3;
}

message BondedRole {
  string           address                       = 1;
  RoleType         role_type                     = 2;
  BondedRoleStatus bond_status                   = 3;
  string           current_bond                  = 4;
  string           total_committed_bond          = 5;
  int64            registered_at                 = 6;
  int64            last_active_epoch             = 7;
  uint64           consecutive_inactive_epochs   = 8;
  int64            demotion_cooldown_until       = 9;
  string           cumulative_rewards            = 10;
  int64            last_reward_epoch             = 11;
}

message BondedRoleConfig {
  RoleType role_type                   = 1;
  string   min_bond                    = 2;  // math.Int string, udream
  uint64   min_rep_tier                = 3;  // 0 = no rep-tier gate
  string   min_trust_level             = 4;  // enum name; empty = no trust gate
  int64    min_age_blocks              = 5;  // enforced by EligibleForRole at action time (ErrRoleTooNew, 2422)
  int64    demotion_cooldown           = 6;  // seconds a DEMOTED role waits before re-bonding
  string   demotion_threshold          = 7;  // RECOVERY -> DEMOTED floor
  int64    unbond_cooldown             = 8;  // seconds bond stays locked and slashable after unbond
  // Verdict-streak policy, applied by x/rep in RecordRoleOutcome.
  uint64   upheld_to_reset_overturns   = 9;
  int64    overturn_base_cooldown      = 10;
  bool     overturn_cooldown_escalates = 11;
}
```

**Storage** (single Map keyed by the `(role_type, address)` pair, plus a per-role config Map):
- `BondedRoles collections.Map[Pair[int32, string], BondedRole]`
- `BondedRoleConfigs collections.Map[int32, BondedRoleConfig]`

**rep/owning-module boundary:** rep owns the generic bond/status/activity state; the owning module owns per-role action counters in its own proto:

| Role | Module | Owning-module activity proto |
|------|--------|------------------------------|
| `ROLE_TYPE_CONTENT_SENTINEL` | x/forum | `sparkdream.forum.v1.SentinelActivity` (hides, locks, moves, pins, epoch tallies) |
| `ROLE_TYPE_COLLECT_CURATOR` | x/collect | `sparkdream.collect.v1.CuratorActivity` (total/challenged/upheld/overturned reviews, streak counters) |
| `ROLE_TYPE_FEDERATION_VERIFIER` | x/federation | `sparkdream.federation.v1.VerifierActivity` (verifications, upheld/overturned/unchallenged, slash count, overturn cooldown) |
| `ROLE_TYPE_INITIATIVE_REVIEWER` | x/rep | none — rep owns the role outright, so its verdict counters live in the shared `RoleActivity` with nothing split out |

**Config write-through:** each role's owning module keeps the operational params (min bond, trust level, demotion cooldown, etc.) in its own proto and calls `SetBondedRoleConfig` on update + `InitGenesis` so rep's enforcement state stays in sync. The initiative reviewer's owning module is **x/rep itself**: `SyncReviewerBondedRoleConfig` projects rep's own `min_reviewer_*` / `reviewer_*` params onto the config from `InitGenesis`, `MsgUpdateParams` and `MsgUpdateOperationalParams`. See "The bond floor, and who owns it" under Initiative Review.

**Owning-module → rep API surface** (content-module handlers call these on the rep keeper):

| Method | Usage |
|--------|-------|
| `IsBondedRole(ctx, role_type, addr) (bool, error)` | Permission check before accepting a role-gated action |
| `GetBondedRole(ctx, role_type, addr) (BondedRole, error)` | Fetch the rep-side record for status + bond inspection |
| `GetAvailableBond(ctx, role_type, addr) (math.Int, error)` | `current_bond - total_committed_bond - pending_unbond_amount`, saturating at zero (pending-aware: bond queued to unbond cannot back new reservations) |
| `ReserveBond(ctx, role_type, addr, amount)` | Commit bond against a pending action (hide, review, verification); draws only on staying, uncommitted bond (pending-aware) |
| `ReleaseBond(ctx, role_type, addr, amount)` | Release commitment on successful/expired action |
| `SlashBond(ctx, role_type, addr, amount, reason)` | Unlock + burn DREAM; decrement both `current_bond` and `total_committed_bond` |
| `RecordActivity(ctx, role_type, addr)` | Stamp `last_active_epoch`, reset `consecutive_inactive_epochs` |
| `SetBondStatus(ctx, role_type, addr, status, cooldown_until)` | Transition NORMAL / RECOVERY / DEMOTED with cooldown |
| `SetBondedRoleConfig(ctx, cfg)` | Write-through from the owning module's operational params |
| `GetBondedRoleConfig(ctx, role_type) (BondedRoleConfig, error)` | Read the per-role policy snapshot |

**Scope (non-goals).** BondedRole is DREAM-bond only and reputation-gated. SPARK-staked economic actors (cosmos-sdk validators via x/staking, federation bridge operators) use their own primitives — different unbonding semantics, different slash destinations, different eligibility signals.

### RoleActivity (shared per-role accountability record)

`RoleActivity(role_type, address)` is the rep-owned companion to `BondedRole`: everything that is a property of the *role holder* rather than of any one module surface. Owning modules REPORT actions and jury verdicts; x/rep applies the consequences — including streak demotion, which is internal since rep owns the bond being demoted.

```protobuf
// proto/sparkdream/rep/v1/role_activity.proto
message RoleActivity {
  RoleType role_type = 1;
  string address = 2;
  uint64 consecutive_upheld = 3;      // verdict streaks, shared across surfaces
  uint64 consecutive_overturns = 4;   // crossing the threshold demotes the bond
  int64 overturn_cooldown_until = 5;  // moderation-action circuit breaker
  uint64 epoch_appeals_resolved = 6;  // reward score sqrt term; reset each epoch
  repeated RoleAccuracyBucket accuracy_window = 7;  // rolling ring, RoleAccuracyRingSize slots
  map<string, uint64> epoch_actions = 8;      // per-kind, reset each reward epoch
  map<string, uint64> total_actions = 9;      // per-kind lifetime
  map<string, uint64> upheld_actions = 10;
  map<string, uint64> overturned_actions = 11;
  int64 last_slash_epoch = 12;        // stamped by SlashBond, in the role's own epoch units
}
```

**Action kinds** are rep-owned string constants with policy tables ([x/rep/types/role_activity_kinds.go](../x/rep/types/role_activity_kinds.go)):

| kind | reported by | activity gate | score weight | cooldown on overturn |
|---|---|---|---|---|
| `forum_hide` | forum MsgHidePost | yes | 0.01 | yes |
| `forum_lock` | forum MsgLockThread | yes | 0.05 | yes |
| `forum_move` | forum MsgMoveThread | yes | 0.03 | yes |
| `forum_pin` | forum MsgPinReply | yes | 0 | yes |
| `forum_curation` | forum proposal confirm/reject | yes | 0.02 | **n/a** — a rejection arrives via `RecordRoleRejection` (accuracy miss only); it never reaches the cooldown or the streaks |
| `collect_hide` | collect MsgHideContent | yes | 0.01 | yes |
| `collect_curation` | collect challenge resolution | — | — | — |
| `federation_verify` | federation MsgVerifyContent + challenge resolution | yes | **none** — verifier pay is flat plus a contested-accuracy bonus, never per verification | yes, and it **escalates** (see below) |
| `forum_appeal_filed` / `collect_appeal_filed` | appeal messages | no (appeals against the holder are not their work) | — | — |

**Per-role verdict-streak policy.** Three `BondedRoleConfig` fields let a role
depart from the moderation defaults, written through by the owning module:

| field | default | federation verifier |
|---|---|---|
| `upheld_to_reset_overturns` | 1 — one good call wipes the slate | 3 — the streak is sticky, so alternating wrong/right cannot hold a holder permanently one overturn short of demotion |
| `overturn_base_cooldown` | `DefaultRoleOverturnCooldown` (24h) | `verifier_overturn_base_cooldown` |
| `overturn_cooldown_escalates` | off — flat lockout per overturn | on — `base * 2^(streak-1)`, capped at `MaxRoleOverturnCooldown` (7 days) |

**Upheld verdicts that keep the streak (`UpheldKeepsStreak`).** An upheld
`forum_curation` outcome (a confirmed accepted-reply proposal) counts in the
accuracy ring and the per-kind upheld counter, but does not bump
`consecutive_upheld` and never clears `consecutive_overturns`. A confirmation
is the thread author's say-so, not a verdict on a contested action; letting it
reset the streak would let a sentinel undo every overturned hide with one
confirmed curation. The mirror case is not a verdict either: an author
rejecting the proposal is reported through `RecordRoleRejection`, which books
an accuracy-ring miss, `epoch_appeals_resolved` and the per-kind overturned
counter but moves neither streak, starts no overturn cooldown and never
demotes (REP-R-207). Routine rejection still lowers the sentinel's reward
accuracy; it cannot cost them the role the way three overturned hides do.

The verifier's departure is not incidental: an overturned hide is a contested
judgment call, while an overturned verification means the holder attested to a
hash that was false. Making it config rather than a rep constant keeps the
knobs governance-editable through the owning module's params, which is what
`BondedRoleConfig` is for.

**Keeper surface**: `RecordRoleAction(role_type, addr, kind)`; `RecordRoleOutcome(role_type, addr, kind, upheld)` (verdict maps + streaks + ring at the recording role's own reward epoch + cooldown per the kind table + internal streak demotion); `RoleOverturnCooldownUntil`; `RoleEpochActionCount` (forum's `max_*_per_epoch` caps read this — single source of truth, no module-local counter copies); `GetRoleWindowedAccuracy`; `ResetRoleEpochCounters` (reward-epoch boundaries); `BumpRoleEpochAppealsResolved` (forum flag-dismissals feed the score's sqrt term without an accuracy tick); `GetRoleActivity`.

`SlashBond` stamps `last_slash_epoch` on this record for **every** role type, in that role's own reward-epoch units. Reward distributions may read it as a "no pay in the window you were slashed in" gate; only the federation verifier does today, and the sentinel/curator/reviewer gates were deliberately left unchanged rather than silently tightened as a side effect of the stamp existing.

**The gate is a window test, not an equality test.** A distribution runs at height `N * epoch_blocks` and so labels itself epoch `N`, but the counters it is paying for accrued over heights `[(N-1) * epoch_blocks, N * epoch_blocks)` — every one of which stamps `N-1`. Matching `last_slash_epoch == N` therefore catches only a slash landing in the boundary block itself and lets an entire epoch of slashes collect pay. The gate accepts `N-1` or `N`. Note also that `last_slash_epoch` is a plain `int64` with no "never slashed" encoding, so a slash in epoch 0 is indistinguishable from an unslashed role; that only affects the first two distributions on a fresh chain.

**Streak demotion outranks the bond amount.** `RecordRoleOutcome` sets `bond_status = DEMOTED` once the overturn streak crosses the threshold, and `SlashBond` recomputes `bond_status` from the remaining bond. A role demoted for a streak usually still holds a bond above `demotion_threshold`, so a naive recompute would hand them `RECOVERY` back and erase the demotion — and whether it did would depend on whether the owning module happened to call `SlashBond` before or after `RecordRoleOutcome`. `SlashBond` therefore takes the **harsher** of its recomputed status and the existing one. A slash can always deepen a status; it can never soften one. Call order is not load-bearing in either direction.

**Consequences of the shared record**:
- The sentinel reward distribution is fully rep-internal: eligibility gates, cross-surface activity, the Gate 4 appeal rate (`(forum_appeal_filed + collect_appeal_filed) / (forum_hide + collect_hide)`), and windowed accuracy all read RoleActivity. A collect-only moderator is reward-eligible.
- Overturn streaks and the cooldown span surfaces: losing appeals in collect demotes the same as in forum, and the cooldown blocks new hides on both.
- The same split applies to the **federation verifier**: x/federation reports `federation_verify` actions and verdicts and keeps only `unchallenged_verifications` in a slim `sparkdream.federation.v1.VerifierActivity`, with its `verifier-activity` query serving a read-through `VerifierActivityView` projection over both records. Its `slash_count` is derived from the overturned count rather than stored — an upheld challenge slashes exactly once.
- Module-local bookkeeping stays home: forum keeps `pending_hide_count`, `unchallenged_hides`, and curation-proposal lifecycle counters in a slim `sparkdream.forum.v1.SentinelActivity`; forum's `get-sentinel-activity` query serves a read-through projection composing both records into the legacy response shape (the projected fields are never persisted in forum state). A role holder who has only acted on other surfaces (no forum-local record) is still served by the single-address `get-sentinel-activity` query via the projection, but does not appear in `list-sentinel-activity`, which paginates forum-local records only.
- Jury resolution (`MsgResolveGovActionAppeal`) records the verdict on RoleActivity directly and calls forum's narrow `OnSentinelActionResolved` hook for the one forum-local effect (pending-hide decrement).
- **Escalation markers are genesis state.** `EscalatedReviews` is the only record that a review round is already with the committee — `ReviewEscalation` is reset to `NONE` on escalation — so it is exported and re-imported. Dropping it would re-escalate every open round on the next sweep, extending each deadline by another full committee window, and leave silent escalations with nothing to resolve them to `PASSED`.
- **The accuracy ring is stamped in the units of the recording role's own reward epoch.** The ring is written when a verdict resolves and read back as a window at distribution time, so the stamp and the window must agree on what an epoch is. Each role with its own reward pool has an independently committee-editable cadence (`sentinel_reward_epoch_blocks`, `reviewer_reward_epoch_blocks`, `curator_reward_epoch_blocks`, `verifier_reward_epoch_blocks` — and the verifier's is set independently, to one full federation `challenge_window`, since a verification stays challengeable far longer than a hide stays appealable; on production networks that makes it the longest of the four, though not on every network — devnet compresses its challenge window harder than its sentinel epoch), so deriving the stamp from any one role's dial zeroes another role's pay the moment the two are set apart — the reviewer pool's window would find no in-range verdicts and pay nobody, silently, with no error anywhere. Roles without a pool of their own share the sentinel clock.

### MemberReport, MemberWarning, GovActionAppeal, JuryParticipation

> **Note:** Salvation counters live on the `Member` proto (fields 29-30: `epoch_salvations`, `last_salvation_epoch`).

```protobuf
// proto/sparkdream/rep/v1/accountability.proto
enum GovActionType {
  GOV_ACTION_TYPE_UNSPECIFIED = 0;
  GOV_ACTION_TYPE_WARNING     = 1;
  GOV_ACTION_TYPE_DEMOTION    = 2;
  GOV_ACTION_TYPE_ZEROING     = 3;
  GOV_ACTION_TYPE_TAG_REMOVAL = 4;
  GOV_ACTION_TYPE_FORUM_PAUSE = 5;
  GOV_ACTION_TYPE_THREAD_LOCK = 6;
  GOV_ACTION_TYPE_THREAD_MOVE = 7;
  GOV_ACTION_TYPE_REPLY_PIN   = 8;  // sentinel reply pin (ActionTarget = reply id)
  GOV_ACTION_TYPE_POST_HIDE   = 9;  // sentinel post hide
}

enum MemberReportStatus {
  MEMBER_REPORT_STATUS_UNSPECIFIED   = 0;
  MEMBER_REPORT_STATUS_PENDING       = 1;
  MEMBER_REPORT_STATUS_ESCALATED     = 2;
  MEMBER_REPORT_STATUS_RESOLVED      = 3;
  MEMBER_REPORT_STATUS_META_APPEALED = 4;
}

enum GovAppealStatus {
  GOV_APPEAL_STATUS_UNSPECIFIED = 0;
  GOV_APPEAL_STATUS_PENDING     = 1;
  GOV_APPEAL_STATUS_UPHELD      = 2;
  GOV_APPEAL_STATUS_OVERTURNED  = 3;
  GOV_APPEAL_STATUS_TIMEOUT     = 4;
}

// proto/sparkdream/rep/v1/member_report.proto
message MemberReport {
  string             member               = 1;
  string             reason               = 2;
  GovActionType      recommended_action   = 3;
  string             total_bond           = 4;
  int64              created_at           = 5;
  MemberReportStatus status               = 6;
  string             defense              = 7;
  int64              defense_submitted_at = 8;
  repeated string    reporters            = 9;
  repeated uint64    evidence_post_ids    = 10;
  repeated uint64    defense_post_ids     = 11;
  repeated ReporterBondEntry reporter_bonds = 12;
  GovActionType      resolved_action      = 13; // action MsgResolveMemberReport applied (UNSPECIFIED = dismissed)
  int64              resolved_at          = 14; // block time of the resolution
}

// proto/sparkdream/rep/v1/member_warning.proto
message MemberWarning {
  uint64          id                 = 1;
  string          member             = 2;
  string          reason             = 3;
  int64           issued_at          = 4;
  string          issued_by          = 5;
  uint64          warning_number     = 6;
  repeated uint64 evidence_post_ids  = 10;
}
```

#### Member report rules

- A report names the member's **canonical** address: a report filed against a
  tombstone lands on the successor, trips `HasOpenMemberReport` for it (the KEY
  migration path is then refused) and is defended by the successor. Only an
  existing, non-zeroed member can be reported, so reporter bonds cannot be
  parked against arbitrary addresses. `MsgCosignMemberReport` and
  `MsgResolveMemberReport` resolve the alias the same way; a tombstone cannot
  defend.
- One **open** report per member (PENDING, ESCALATED, META_APPEALED). A
  resolved report is history and gives way to a new one, except while the
  member's appeal window on its resolution is still open
  (`resolved_at + DefaultAppealDeadline`), since the new report overwrites the
  record that opens that window, or while an appeal of that resolution is
  pending (its `MemberActionRestoration` still exists).
- Reporting and co-signing need `DefaultMinSentinelBond` (500 DREAM) of staked
  DREAM, and each reporter and co-signer locks the lesser of its staked DREAM
  and `DefaultMaxMemberReportBond` (100 DREAM). Both were micro-DREAM-scale
  before (500 and 1000 micro-DREAM, co-signers uncapped).
- `MsgResolveMemberReport` records `resolved_action` and `resolved_at`.
- `MsgResolveMemberReport` can zero a member, so its `creator` must be the
  Commons Operations Committee policy address (an executed committee
  proposal), the Commons Council policy or x/gov (`isCouncilOrCommitteePolicy`
  over `IsCouncilOrCommitteePolicy`); a single committee member is refused
  with `ErrNotGovAuthority`. The message is in the committee's bootstrap
  `AllowedMessages`.

#### Keeper-internal warning issuance (`IssueWarning`)

Besides the committee-proposal `MsgResolveMemberReport` path, x/rep exposes a
keeper method other modules call directly from their EndBlocker / ABCI paths
(via their `RepKeeper` interface):

```go
func (k Keeper) IssueWarning(ctx context.Context, member string, issuedBy string, reason string, evidencePostIDs []uint64) error
```

Semantics (`keeper/member_warning.go`):

- Validates `member` is a parseable address and `reason` is non-empty
  (`ErrEmptyWarningReason`, code 2201); `issuedBy` is supplied by the caller —
  typically the caller module's account address so the warning's origin is
  auditable. `reason` should be a stable short identifier (e.g.
  `"promoted_hidden_content"`) rather than free-form prose so consumers can
  filter on it.
- Allocates a global `id` from `MemberWarningSeq` and computes a **per-member**
  `warning_number` by counting existing warnings for that member and assigning
  N+1 — the same numbering `MsgResolveMemberReport` uses, so
  `ListMemberWarning` / `GetMemberStanding` read a consistent sequence
  regardless of which path produced the warning. (The count is currently an
  O(N) scan over all warnings.)
- Stores the warning and emits `member_warning_issued`. It does **not** touch
  the salvation counters (`epoch_salvations` / `last_salvation_epoch`); like all
  warnings, the accumulated warning count feeds the existing auto-demotion
  threshold rather than the salvation window.

**Consumers.** x/forum's `ExpireHiddenPosts` calls `IssueWarning` to warn the
post **promoter** (the member who called `MsgMakePostPermanent`, when distinct
from the author — self-promotion is not a vouching act) with reason
`"promoted_hidden_content"` and the post ID as evidence, when a hidden post
times out unappealed. x/collect may use the same surface for analogous
curator/promoter accountability.

#### Remaining accountability state

```protobuf
// proto/sparkdream/rep/v1/gov_action_appeal.proto
message GovActionAppeal {
  uint64          id                   = 1;
  string          appellant            = 2;
  GovActionType   action_type          = 3;
  string          action_target        = 4;
  string          original_reason      = 5;
  string          appeal_reason        = 6;
  string          appeal_bond          = 7;
  int64           created_at           = 8;
  int64           deadline             = 9;
  uint64          initiative_id        = 10;
  GovAppealStatus status               = 11;
  uint64          original_category_id = 12;
}
//
// GovActionAppeal is the SINGLE moderation/governance appeal mechanism, covering
// both sentinel actions (hide/lock/move/pin) and committee actions. All x/forum
// appeal entry points (MsgAppealPost, MsgAppealThreadLock, MsgAppealThreadMove,
// MsgDisputePin), x/collect MsgAppealHide and x/artifact MsgAppealHide are
// facades that call CreateGovActionAppeal; it charges the
// refundable appeal bond and queues a jury draw (parties recorded in
// excluded_jurors; DrawPendingJuries seats the jury in the next BeginBlocker). Resolution runs applyGovActionAppealVerdict, reached either:
//   - automatically by JURY VERDICT — TallyJuryVotes when a supermajority of
//     votes is cast, or at the deadline via TimeoutExpiredAppeals (which tallies
//     if a quorum of the seated jury voted, else TIMEOUT); the no-quorum guard
//     means juror inaction never resolves an appeal; OR
//   - manually by a MsgResolveGovActionAppeal override from the Operations
//     Committee policy (an executed committee proposal), the Commons Council
//     policy or x/gov.
// The two paths share applyGovActionAppealVerdict and are idempotent on status,
// so they cannot double-apply. On OVERTURNED the underlying content action is
// reversed (ReverseSentinelAction) and the sentinel is slashed; on UPHELD the
// sentinel's reserved bond is released and the appeal bond is half-burned /
// half-routed to the sentinel reward pool. See docs/x-forum-appeal-reconciliation.md.

// proto/sparkdream/rep/v1/jury_participation.proto
message JuryParticipation {
  string juror             = 1;
  uint64 total_assigned    = 2;
  uint64 total_voted       = 3;
  uint64 total_timeouts    = 4;
  int64  last_assigned_at  = 5;
  bool   excluded          = 6;
}
```

### Zeroing (`ZeroMember`)

Zeroing burns the member's whole DREAM balance, locks included, so every
escrow that claims part of the lock ends with it. Before the balance goes,
`terminateEscrowsForZeroing` (keeper/zeroing.go), over every address the
member ever held:

- **forfeits** the member's open initiative challenges (`RejectChallenge`: the
  stake is burned, the initiative restored, a seated jury closed
  INCONCLUSIVE) and open content challenges (`RejectContentChallenge`, jury
  closed), and its PENDING invitations (stake burned, invitee slot freed, no
  credit back, event `invitation_forfeited`);
- **drops** its review-bounty contributions, tag-report bonds and open
  member-report bonds, and empties its bonded-role bonds (status DEMOTED);
- **ends its claim on every open interim it is assigned to** (read through
  the `InterimsByAssignee` index, so the cost is the member's own interims):
  an interim it held alone (member work awaiting approval, an expert
  witness's testimony interim) is expired (event `interim_expired`, reason
  `assignee zeroed`); one shared with other assignees (a project review
  seat, co-signed work) loses only the zeroed seat (`interim_assignee_removed`)
  and stays live for the rest, who split the budget over the seats left when
  it settles (REP-E-203). So a zeroed member is not paid for work opened
  before the zeroing (REP-E-104). Both payout paths (`ApproveInterim`,
  `CompleteInterimDirectly`) also skip an assignee whose member is not ACTIVE
  (event `interim_assignee_skipped`), `reissueDREAM` (review bounties, author
  bond restores) refuses a non-ACTIVE recipient (REP-E-208: a zeroed
  reviewer's bounty share goes back to the funders, a zeroed author's bond is
  not restored, `author_bond_restore_skipped`), and a zeroed expert cannot
  testify;
- releases its stake records (author bonds included) through the by-staker
  index, and drops its initiative-bounty contributions and self-assign bonds,
  as before.

Each of these used to stay behind: its later `UnlockDREAM` failed against the
zeroed aggregate and wedged whatever waited on it, a challenged initiative for
good. After zeroing, `LockDREAM` refuses the member, so its `staked_dream`
stays zero.

Modules that hold their own lock records drop the zeroed member's through
`MemberZeroingHook.AfterMemberZeroed(ctx, member, addresses)`, registered in
app.go with `AddMemberZeroingHook`. The contract: a hook drops **every** lock
record the module holds for `addresses`, without an unlock, and closes or
cancels whatever the record backed. A record left behind is unsafe both ways:
while the zeroing stands its unlock fails, and after an OVERTURNED appeal,
which re-mints the burned balance as liquid DREAM and does not restore
`staked_dream`, its unlock succeeds against whatever the member locked since
and releases an unrelated lock. x/reveal drops all of the member's stakes
(a STAKING tranche's total is adjusted, a BACKED+ tranche keeps the weight)
and cancels the member's own PROPOSED/IN_PROGRESS contributions; x/season
drops nomination stakes (recomputing the nominations) and display-name report
and appeal stakes (the case stays open and settles only the other side);
x/forum closes post-conviction stakes; x/name closes the disputes the member
filed (returning a contesting owner's stake) and zeroes the member's contest
stakes (the dispute stays with the council and settles only the claimant).
Each hook runs in its own cache
context; an error is logged and never blocks the zeroing. Release paths keep
a fallback for a record a hook missed: x/reveal's `unlockOrGone`, and
`internal/dreamutil` `Ops.UnlockOrGone` / `BurnLockedOrGone` /
`SettleStakesOrGone` for modules on the shared DREAM ops, treat x/rep's
`ErrInsufficientStake` / `ErrMemberNotFound` as "lock already gone".
`app.TestZeroingClosesEveryModulesLocks` zeroes a member holding locks in
season, reveal and name through the real keepers and drives each resolution path,
including the overturn.

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

// Joining is agreeing to the chain's open-content commitment: everything the
// member publishes is dedicated to the public domain (docs/content-license.md).
// accepted_content_license must equal the chain's content license, "CC0-1.0",
// or the acceptance is refused with ErrContentLicenseNotAccepted (1208) and
// nobody is admitted. It is a signed field, not a UI checkbox, so the
// agreement is on-chain and a Ledger shows it. The accepted license is stored
// on the new member record (Member.content_license). Emits
// content_license_accepted (member, license).
message MsgAcceptInvitation {
  option (cosmos.msg.v1.signer) = "invitee";
  string invitee = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 invitation_id = 2;
  string accepted_content_license = 3;
}

message MsgTransferDream {
  option (cosmos.msg.v1.signer) = "sender";
  string sender = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string recipient = 2;
  string amount = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];
  TransferPurpose purpose = 4;
  string reference = 5;
}

// Any other value is rejected. Bounties are escrowed against an initiative
// with MsgFundInitiativeBounty, not sent as a transfer.
enum TransferPurpose {
  TRANSFER_PURPOSE_TIP = 0;
  TRANSFER_PURPOSE_GIFT = 1;
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

```protobuf
// MsgRevokeInvitation withdraws a PENDING invitation. Only the inviter may
// revoke; accepted, expired or already-revoked invitations are rejected
// (ErrInvitationNotPending). Closing unlocks the stake, restores the
// invitation credit and frees the invitee address, exactly like expiry; the
// record is kept with status REVOKED and an `invitation_revoked` event is
// emitted. CLI: `tx rep revoke-invitation [invitation-id]`.
message MsgRevokeInvitation {
  string inviter = 1;
  uint64 invitation_id = 2;
}
```

`MsgRegisterZkPublicKey` requires `zk_public_key` to be exactly 32 bytes and a
canonical BN254 scalar: its big-endian value must be below the field modulus,
or the message fails with `ErrInvalidRequest`. The key is hashed into the trust
tree as a field element, so an out-of-range value would produce a leaf no proof
can open and lock the member out of every anonymous action.

```protobuf
// MsgClearZkPublicKey zeroes zk_public_key and pending_zk_public_key at once
// and registers nothing in their place (leaked shield secret). No cooldown, no
// per-season limit. Sets zk_key_cleared. CLI: `tx rep clear-zk-public-key`.
message MsgClearZkPublicKey {
  option (cosmos.msg.v1.signer) = "member";
  string member = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
}
```

`MsgClearZkPublicKey` is the remedy for a leaked shield secret: a key *change*
is capped at one per season, so a member who already rotated and then leaks
would otherwise have no remedy until the season ends. Clearing only takes
capability away, so it is unlimited; it removes any `ZkKeyActivationQueue`
entry, marks the member dirty and emits `rep.zk_public_key_cleared`. It refuses
tombstones and members with no key. See the ZK key change discipline below for
what the next registration may do.

#### DREAM Transfer Limits

> These replaced the original tip and gift limits (100 DREAM per tip, 10 tips
> per epoch, 500 DREAM per gift, 2,000 DREAM gifted per epoch, a one-day
> per-recipient gift cooldown) and the `BOUNTY` transfer purpose, now
> [initiative bounties](#initiative-bounties).

DREAM is not meant to be monetized. No chain rule can stop an off-chain sale,
so the defence is to keep DREAM off every market venue and to make any route by
which DREAM moves between members too slow to be worth buying through.

##### Why the original limits did not do that

**They were sized for a mature economy, not this one.** The genesis DREAM
supply is 25,000. A single tip could be 100 DREAM — the entire budget ceiling
of an APPRENTICE initiative — and a member could tip 1,000 DREAM a day to
anyone, all to one recipient, since tips were capped by count rather than
amount. A founder could empty their balance in two to five days.

**They capped the wrong side.** Every limit bound the *sender*. A seller can
only send their own DREAM, but a buyer can receive from many sellers at once: at
any per-sender cap, twenty cooperating sellers move twenty times that into one
account. Tightening sender caps only raises the number of sellers needed.

What a buyer wants is a *position*. Trust level derives from reputation, not
balance, so bought DREAM does not buy rank directly — but it does buy conviction
(which pushes initiatives to completion and mints), invitation stakes (more
invitations, so more controlled accounts), and bonds for paid roles. All of
those scale with what one account holds. The binding limit therefore belongs on
the **recipient**.

##### The limits

| Limit | Default | Applies to |
|---|---|---|
| `max_transfer_received_per_season` | 1,000 DREAM | Tips received + initiative bounty payouts |
| `max_transfer_received_per_epoch` | 100 DREAM | Tips received + initiative bounty payouts |
| `max_tip_amount` | 10 DREAM | Per tip |
| `max_tips_sent_per_epoch` | 50 DREAM | Per sender, by **amount** (replaces the count-based `max_tips_per_epoch`) |
| `max_gift_per_invitee` | 500 DREAM | Lifetime total from an inviter to each of their invitees |

Both sides of a tip or gift must be ACTIVE members: a ZEROED sender is refused
with `ErrMemberNotActive`, a ZEROED recipient with `ErrRecipientNotActive`
(REP-E-109).

The guarantee this buys: accumulating a founder-sized position (5,000 DREAM)
through transfers takes at least five seasons, about two years, **however many
senders cooperate** — and whatever is done with it in the meantime is slow and
visible. That is the property that matters; how fast the whole supply could
churn is not, because no one can move DREAM that is not theirs.

**Tips are gratitude, so a token amount suffices.** Ten DREAM is a meaningful
thank-you against an APPRENTICE budget of 100; it is not a payment channel.

**Gifts become a one-time seed.** A gift exists to help one invitee get started
(see [the new-member on-ramp](#the-new-member-on-ramp)), which needs doing once,
not every day. A lifetime per-invitee total replaces the per-gift cap, the
per-sender epoch cap and the per-recipient cooldown, all of which were
approximating it. Gifts do **not** count toward the recipient limits: each
member has exactly one inviter, so gifts can bring any account at most 500 DREAM
ever, and multiplying that across invitees costs the inviter an escalating,
partly burned invitation stake per account.

**Initiative bounty payouts do count.** Otherwise a buyer takes many small
initiatives and routes around the recipient limit. When a completing
initiative's bounty would push the assignee over either limit, the assignee is
paid up to the limit and the excess is refunded to the funders pro rata,
untaxed. The initiative still completes; only the bounty is clipped.

Transfers the recipient limit refuses fail with a new
`ErrExceedsTransferReceiveLimit` and are not partially applied — a tip either
lands whole or not at all.

##### State

The recipient counters live on `Member`:
`transfer_received_this_epoch` / `last_transfer_received_epoch` and
`transfer_received_this_season` / `last_transfer_received_season`. Each window
resets lazily on the first receipt in a new epoch or season
(`rollTransferReceivedWindows`), and proto3 omits them for members who never
receive a transfer. `TransferReceiveHeadroom` reports the smaller of the two
remaining allowances; the initiative bounty payout clips to it.

On the sending side `Member.tips_sent_this_epoch` counts DREAM tipped (an
amount), and `GiftRecord.total_gifted` is the lifetime ledger per
(sender, invitee).

**Zeroing keeps the recipient counters.** `ZeroMember` clears the tip
allowance but not what the member has received this season: resetting it would
let a zeroed account buy its position straight back.

**Every check runs before any write.** A refused tip or gift leaves no trace —
no gift ledger entry, no tip allowance consumed — even when `TransferDREAM` is
called from another module outside a transaction.

**The test chain pins these loose.** The root `config.yml` raises every
transfer limit so the e2e setup scripts can fund test accounts from alice by
tip; the production values are pinned by the keeper unit tests.

##### Purpose validation

`TransferDREAM` accepts exactly `TIP` and `GIFT`; any other value, including an
undefined enum number on a hand-built transaction, fails with
`ErrInvalidTransferPurpose`. The old code switched on `TIP` and `GIFT` with no
default, so every other value — including `BOUNTY` — was an uncapped send.
`TRANSFER_PURPOSE_BOUNTY` is removed from the enum. The unread `reference`
field stays as a free-text memo, echoed into the `transfer_dream` event.

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

message MsgCompleteProject {
  option (cosmos.msg.v1.signer) = "creator";
  option (amino.name) = "sparkdream/x/rep/MsgCompleteProject";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 project_id = 2;
}
```

#### Permissionless Project Creation Flow

`MsgProposeProject` supports two paths based on the requested budget:

| Condition | Path | Result |
|-----------|------|--------|
| `requested_budget == 0` AND `requested_spark == 0` | **Permissionless** | ACTIVE immediately |
| `requested_budget > 0` OR `requested_spark > 0` | **Budget-backed** | PROPOSED → awaits `MsgApproveProjectBudget` |

**Permissionless path** handler logic:
1. Validate creator is an ACTIVE member of ESTABLISHED+ trust level (`ErrMemberNotActive`, `ErrInsufficientTrustLevel`)
2. Burn `ProjectCreationFee` DREAM from creator's balance (`ErrInsufficientBalance` if short)
3. Create project with `permissionless = true`, `status = ACTIVE`, all budget fields zero
4. No `PROJECT_APPROVAL` interim is created — committee is not involved
5. Emit `project_created` event (distinct from `project_proposed`)

**Budget-backed path**:
1. Validate creator is an ACTIVE member (`ErrMemberNotActive`)
2. Refuse when the creator already holds `max_proposed_projects_per_creator`
   PROPOSED projects across their recent aliases
   (`ErrTooManyProposedProjects`, 2423; `ProposedProjectsByCreator` index)
3. Burn `ProjectCreationFee` DREAM from the creator's balance
   (`ErrInsufficientCreationFee` if short) — the same fee as the
   permissionless path
4. Create project with `status = PROPOSED`, `requested_budget` /
   `requested_spark` stored, `expiry_block_height` set
5. Open a `PROJECT_APPROVAL` interim (STANDARD) for the picked council's
   Operations Committee, or a `BUDGET_REVIEW` one (COMPLEX) when
   `requested_budget >= budget_review_threshold`; its id is stored on
   `project.approval_interim_id`. Skipped when the committee has no members;
   never fails the proposal (see Project Review Interims)
6. Await `MsgApproveProjectBudget` → transitions to ACTIVE and pays the review
   interim; a cancelled or expired proposal ends it unpaid

#### Approving a Project Budget

`MsgApproveProjectBudget` moves a `PROPOSED` project to `ACTIVE` with the
approved DREAM and SPARK budgets. Neither may exceed what the proposal
requested (`ErrApprovedBudgetExceedsRequested`, 2424): the review interim was
priced from the request (`budget_review_threshold`), so a larger approval
would have been reviewed at the wrong rate. Authority is resolved against
`project.council` (a member or policy of another council's committee is
refused):

- **Any budget**: the gov authority, or the policy address of the project's
  council or of that council's Operations Committee
  (`IsCouncilOrCommitteePolicy(project.council, "operations")`, an executed
  proposal). The message is on the `AllowedMessages` of the Commons, Technical
  and Ecosystem Operations Committees.
- **Budget ≤ `large_project_budget_threshold`**: a single member of the
  project council's Operations Committee may also approve directly, unless
  they created the project (`SameMember`, so a migrated creator too): a
  creator's own project needs a committee or council proposal at any budget
  (`ErrUnauthorized`, REP-E-105).
- **Budget > `large_project_budget_threshold`**: a single member is refused
  with `ErrLargeProjectNeedsCouncil`; the approval must come through a
  committee or council proposal (or gov).

Otherwise `ErrUnauthorized`.

#### Cancelling a Project

`MsgCancelProject` moves a project to `CANCELLED` from any non-terminal status
(`PROPOSED` or `ACTIVE`). Cancelling an approved project closes its
initiatives (returning their budget, releasing self-assign bonds, voiding
active challenges) and settles every project stake, so it is a decision of
the body that approved the project, never of one of its members. The handler
accepts:

- the **gov authority**, or the **policy address of the project's council or
  of that council's Operations Committee**
  (`IsCouncilOrCommitteePolicy(project.council, "operations")`, an executed
  proposal) — for any live project. A single committee member is refused.
  `MsgCancelProject` is on the `AllowedMessages` of the Commons, Technical and
  Ecosystem Operations Committees.
- the **project creator** (matched through the alias contract, `CanActAs`, so
  it survives a key migration), with a narrower right of withdrawal:
  - a `PROPOSED` project: nothing has been approved or funded and no
    initiative can exist under it, so withdrawing the proposal touches nobody
    else;
  - an `ACTIVE` **permissionless** project (no council budget behind it), only
    once every initiative under it is terminal and no challenge on one is
    active (`ErrProjectHasOpenWork` otherwise) — so the creator can neither
    close other members' open work nor void a challenge on their own
    initiative (which would release a self-assign bond an upheld challenge
    would burn).

  The creator alone can never cancel an `ACTIVE` **budget-backed** project
  (`ErrUnauthorized`): the council approved and funded it, and only that
  council's body can retire it.

Anyone else gets `ErrUnauthorized`. Cancellation flips the by-status index off `PROPOSED`/`ACTIVE` so
the EndBlocker expiry sweep skips the project, and emits `project_cancelled`.
Cancellation is only permitted from the two live states (`PROPOSED`, `ACTIVE`):
the three terminal states — `COMPLETED`, `CANCELLED`, and `EXPIRED` — are
rejected. In particular an `EXPIRED` project (one that was never approved before
its TTL) cannot be cancelled, since relabeling it `CANCELLED` would erase the
"expired through inaction" signal.

**Effect on the project's initiatives.** Cancellation **cascade-terminates
every non-terminal initiative** under the project — `OPEN` listings, `ASSIGNED`
work, `SUBMITTED` deliverables, `IN_REVIEW` work, and `CHALLENGED` work all move
to `INITIATIVE_STATUS_CLOSED`, emitting one `initiative_closed` event apiece.
Initiatives already in a terminal state (`COMPLETED`, `REJECTED`, `CLOSED`) are
left untouched. For each terminated initiative:

- its **reserved budget is returned** to the project (skipped for permissionless
  projects; clamped to the project's remaining allocation so the cascade can
  never drive `allocated_budget` negative);
- any **self-assign bond is released** back to the assignee (not burned — no
  upheld challenge occurred);
- any **active challenge is voided** (see below); and
- any **review escalation entry is dropped**. The escalation sweep walks its own
  keyset rather than the status index, so an entry left behind would time out
  later and hand `rejectReviewRound` a dead initiative — putting it back to
  `ASSIGNED` with its budget already returned and its bond already released.

The cascade runs *before* the project's own status is persisted so every budget
return lands on the still-live project; the project is then re-read so the
status write does not clobber the returned budget.

**Voiding an active challenge.** A `CHALLENGED` initiative carries an unresolved
challenge holding the challenger's staked DREAM, and possibly a pending jury
review. Terminating it moves the challenge to `CHALLENGE_STATUS_VOIDED` and:

- **refunds the challenger's stake in full** — unlocked, never burned and never
  rewarded, because the dispute was never adjudicated; and
- closes any pending jury review with an `INCONCLUSIVE` verdict, removing it
  from the pending index so the EndBlocker jury resolver never tallies a verdict
  on (and thereby resurrects) a cancelled initiative.

Each voided challenge emits a `challenge_voided` event carrying the challenge id,
initiative id, and the refunded stake amount.

A voided challenge is **sealed**: `MsgSubmitJurorVote` rejects votes on any
review that is no longer `VERDICT_PENDING`, and `UpholdChallenge` /
`RejectChallenge` reject any challenge that is not `ACTIVE` /
`IN_JURY_REVIEW`. Without these guards, a juror voting after the void could
re-trigger the tally and resolve the dead dispute — double-refunding or
retro-burning the challenger's already-returned stake and resurrecting the
`CANCELLED` initiative.

This is required for correctness, not just tidiness: a live challenge left on a
cancelled initiative would be auto-upheld or jury-tallied by a later EndBlocker
pass, overwriting the `CANCELLED` status and double-returning budget.

New initiatives cannot be added to a cancelled project (`MsgCreateInitiative`
requires an `ACTIVE` parent). As defence in depth, `CanCompleteInitiative`
returns false and `CompleteInitiative` errors for any initiative whose parent
project is `CANCELLED`, so no DREAM can ever be minted from a cancelled
project's work even if an initiative reached a terminal-payout path by another
route.

#### Completing a Project

`MsgCompleteProject` (CLI `tx rep complete-project [project-id]`) records an
`ACTIVE` project as `COMPLETED`. It is the only path to `COMPLETED`; no
EndBlocker completes projects automatically.

- **Authorization.** The signer must be the gov authority or the policy
  address of the project's council or of that council's Operations Committee
  (`IsCouncilOrCommitteePolicy(project.council, "operations")`) — an executed
  proposal, never an individual committee member, because completion mints the
  staker completion bonus and settles every project stake. Otherwise
  `ErrUnauthorized`. The message is on the `AllowedMessages` of the Commons,
  Technical and Ecosystem Operations Committees, so each can execute it for
  projects under its council; a council executes it through its own policy only
  if its permissions list it, and gov always can. A project whose `council`
  names no known council can be completed only by gov.
- **Preconditions.** The project must be `ACTIVE` (`ErrInvalidProjectStatus`).
  Every initiative of the project must be terminal (`COMPLETED`, `REJECTED` or
  `CLOSED`) and no challenge on any of them may be `ACTIVE` or
  `IN_JURY_REVIEW`; otherwise `ErrProjectHasOpenWork` (2415). Unfinished work
  is closed first (`MsgCloseInitiative`), or the project is cancelled instead.
- **Effects.** Pays the staker completion bonus (`project_completion_bonus_rate`
  of `spent_budget`, external stakers only, capped by
  `max_completion_bonus_stake_multiple` and the season's initiative reward
  headroom, booked against that cap), settles and releases every project stake
  (below), sets `COMPLETED`, moves the by-status index entry and emits
  `project_completed` with `spent_budget`. Not delegable through x/session.

**Release of the project's own stakes.** Both terminal transitions that leave
`ACTIVE` — `MsgCancelProject` and `MsgCompleteProject` — run
`releaseProjectStakes` *before* the status flips, while `settleStake` still sees
the project as accruing. Every project stake is harvested against the seasonal
accumulator and paid out (`forfeit=false`, since the staker did not choose to
exit early), its principal is unlocked, both denominators it was diluting are
shrunk, and the record is deleted. Each released stake emits a
`project_stake_released` event.

Settling before the flip is what makes the rewards reachable at all: past the
flip the frozen branch of `settleStake` deliberately pays nothing (the shared
accumulator keeps advancing on the strength of live stakers, and a frozen stake
must not credit that growth), so anything accrued up to the transition would be
stranded — the stakes stayed on the books, returned principal on unstake, and
could never collect.

Returning the principal is the other half, and it closes the same ratchet
`releaseInitiativeStakes` closes for the four initiative transitions. A terminal
project's stakes cannot earn and cannot signal, yet the settle-only version left
their DREAM inside both `ProjectStakeInfo.total_staked` and the seasonal
`total_staked` divisor indefinitely — retired work diluting the yield of
everyone still backing live work, until each staker happened to unstake by hand.
`updateStakePoolTotals` shrinks both for a `PROJECT` stake.

On completion the release runs *below* `DistributeProjectCompletionBonus` and
still above the status flip. Both bounds are load-bearing: the bonus is weighted
by each stake's principal and reads the records the release deletes, while
`stakeAccruing` stops paying the moment the status goes terminal.
`CompleteInitiative` orders its own release the same way for the same two
reasons.

The per-stake failure split matches `releaseInitiativeStakes` exactly: a settle
failure against the per-epoch mint cap is returned (transient — the retry pays
in full), any other settle failure forfeits that stake's pending reward and
still returns its principal, an unlock failure leaves the record in place with
its debt rebased for manual `RemoveStake` recovery, and a store write failure is
returned. Both callers are msg-server paths, so a returned error rolls the whole
transition back.

**Who may stake on a project.** `MsgStake` accepts project stakes while the
project is `PROPOSED` or `ACTIVE` and rejects the three terminal states with
`ErrProjectTerminal` — a terminal project can never accrue again, so fresh DREAM
locked against one could only ever be withdrawn as principal. Stakes placed
while `PROPOSED` are early conviction: approval (`MsgApproveProjectBudget`)
rebases their `reward_debt` to the accumulator at approval time, so they accrue
only from approval onward rather than harvesting the whole PROPOSED-window
growth retroactively at settlement. Nothing is owed at the rebase moment — the
project was frozen since each stake was placed — so the rebase forfeits nothing.

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
  bool approved = 3;
  string comments = 4;
}

message MsgUnassignInitiative {
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

message MsgCloseInitiative {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 initiative_id = 2;
  string reason = 3;
}
```

#### Submitting Work

`MsgSubmitInitiativeWork` moves an `ASSIGNED` initiative to `SUBMITTED` and
records `deliverable_uri` — the pointer to the work.

The URI must be non-empty after trimming, and is bounded by
`MaxDeliverableURILength` (512). It is stored trimmed, so the value that was
validated is the value reviewers read.

> Nothing on the happy path reads the deliverable: completion turns on
> conviction, and the only mechanism that inspects the work is the challenge
> system, which requires someone to bond and initiate. An empty URI accepted
> here therefore rides through the review window, past the challenge window and
> into a payout, having given stakers, challengers and jurors nothing to judge.
> Clients require text, but that is a client-side courtesy, not a chain rule.
>
> Enforced in `Keeper.SubmitInitiativeWork` rather than a `ValidateBasic`. SDK
> 0.50+ deprecates `ValidateBasic` in favour of keeper-side validation, and
> x/rep has none anywhere in its types package.

Error: `ErrEmptyDeliverable` (1710).

#### Reviewing an Initiative

`MsgApproveInitiative` records one reviewer's verdict on submitted work. It is
accepted while the initiative is `SUBMITTED` **or** `IN_REVIEW` — the latter is
the review period, and excluding it left well-backed work reviewable for as
little as one block, since the EndBlocker transitions an initiative out of
`SUBMITTED` as soon as its conviction gates are met.

- **Standing**: an active stake on the initiative, or a seat on the Commons
  Operations Committee. The assignee and the parent project's creator are
  excluded outright (`ErrConflictOfInterest`, code 1404).
- **Approval is advisory.** The approver is appended to `initiative.approvals`
  so the endorsement is visible, but no completion logic consults the list —
  conviction remains the only gate on payout. Re-signing is idempotent.
- **Disapproval is committee-only.** The Operations Committee abandons the
  initiative outright; a non-committee disapproval is refused with
  `ErrUnauthorized`.

The stake-weighted staker veto that used to sit here is retired. Stakers are paid
on completion, so the veto was held by exactly the people who lost money using
it — and backing a proposal is a different judgement from evaluating a
deliverable, made earlier and often without the expertise. Quality is the bonded
reviewer's question now (see [Initiative Review](#initiative-review)); conviction
is the stakers'.

Closure by disapproval is not an upheld challenge: the self-assign bond is
returned and the reserved budget goes back to the project. The assignee simply
is not paid. It routes through `CloseInitiative` rather than writing the status
inline, so the live review round is settled the same way every other terminal
exit settles it.

**Every terminal transition releases stakes in full.** `releaseInitiativeStakes`
is the single teardown shared by all four: `CompleteInitiative`,
`CloseInitiative`, the challenge-UPHELD path, and the project-cancel cascade. It
runs *before* the status flips — `stakeAccruing` stops paying the moment the
status is terminal, so settling afterwards would harvest nothing. Each stake is
harvested against the seasonal accumulator with `forfeit=false` (the staker did
not choose to exit early), its principal is unlocked, every pool denominator it
was diluting is shrunk, and the record is deleted. Completion emits
`stake_completed`; the three retirement paths emit `stake_released`.

This replaces `settleInitiativeStakes`, which harvested rewards but left the
principal locked and the record in place on the `CLOSED` and `REJECTED` paths.
That principal could not earn and could not signal, yet it stayed inside
`seasonal_pool`/`total_staked` indefinitely, diluting the yield of everyone
still backing live work — the exact ratchet `updateStakePoolTotals` exists to
prevent, and only the completion path was ever protected from it.

Failures are isolated per stake, and the line between them is whether retrying
can help:

- A settle failure against the **per-epoch mint cap** is returned, aborting the
  whole transition. The cap clears on its own, so the initiative retires intact
  an epoch later. Forfeiting instead would destroy a payable reward over a busy
  block, order-dependently — stakes earlier in the slice get paid, later ones
  do not. Nothing pre-checks this: `CompleteInitiative`'s projected-mint gate
  covers the season cap only, and the completer reward, treasury share,
  completion bonus and review fees have already drawn on the epoch's budget by
  the time the stakes are settled.
- **Any other settle failure** — a missing member record, a drifted
  `StakedDream` aggregate — forfeits that stake's pending reward and still
  returns its principal. Past the status flip the reward is unrecoverable either
  way, so holding the principal hostage to it buys the staker nothing, and
  retrying forever would strand the initiative. That is the failure mode that
  left devnet initiative #1 in `IN_REVIEW` for ~6,000 blocks.
- An **unlock failure** skips the stake, persisting its rebased reward debt and
  leaving the record in place. `RemoveStake` carries no initiative-status guard,
  so its owner withdraws manually whenever they choose; the rebased debt is what
  stops that withdrawal from minting the transition's reward a second time.

Because a returned error must not leave the stakes half-released with the
initiative still live, every caller needs a rollback boundary. The three
msg-server paths get one from tx rollback; the EndBlocker paths are wrapped in a
`CacheContext` at their sweep (`abci.go` steps 5 and 5b, and
`resolveSilentEscalations`, which discards a failed timeout so the id stays in
`EscalatedReviews` and the next block retries it). The boundary is load-bearing
rather than defensive: `CloseInitiative` returns the initiative's budget before
it reaches the stakes, and that return is not idempotent, so a retry over
committed partial writes fails on unrelated grounds.

**Terminal initiatives stop accruing.** `stakeAccruing` freezes initiative
stakes at `COMPLETED`, `REJECTED` or `CLOSED` (the set `types.IsInitiativeTerminal`
owns), matching the long-standing project rule. It had no initiative branch at
all, and the gap was not symmetric with the project one: `CompleteInitiative`
deletes the stakes it pays out, but `CloseInitiative` and the challenge-REJECTED
path leave theirs in place. Those stakes drew the seasonal yield **forever**
against work that had been retired or thrown out, and their principal went on
diluting `total_staked` for everyone still backing live work — nothing
distinguished a shipping initiative from an abandoned one. The
challenge-UPHELD path releases through `releaseInitiativeStakes` too: a
rejection does not retroactively unearn rewards from the window the position was
live, which is what slashing is for.

`MsgStake` rejects new stakes on all three terminal statuses with
`ErrInitiativeTerminal`, mirroring `ErrProjectTerminal` — fresh DREAM locked
against an initiative that can never accrue again could only ever be withdrawn
as principal. Regressions:
`TestSettlement_ClosedInitiativeReleasesStake`,
`TestSettlement_UpheldChallengeReleasesStake`,
`TestSettlement_ReleaseIsolatesPerStakeFailure`,
`TestSettlement_LeftBehindStakeCannotDoubleClaimReward`,
`TestSettlement_MintCapAbortsReleaseAndRetryPaysInFull` and
`TestCreateStake_RejectsTerminalInitiative`.

Stakers keep a real exit regardless: conviction is recomputed from **live** stake
records and completion needs both the total and external thresholds, so
withdrawing a stake blocks completion within about one refresh interval. Voting
with your feet is a functioning veto, not a gesture.

Event: `initiative_disapproved` with `resolved_by` `operations_committee`.

#### Acceptance Criteria

An initiative may declare `repeated VerificationCriteria acceptance_criteria` at
creation — a definition of done, fixed before any work starts and immutable
afterwards. Criteria agreed *after* submission are the author marking their own
homework, so pre-commitment is the whole value.

Criteria are **not** a completion gate of their own. Making them one requires an
electorate to judge every submission, and neither available electorate is
suitable: stakers are paid only on completion (so they are paid to pass the
work), and a lot-drawn jury costs more in participation rewards than a STANDARD
initiative's entire budget. Instead criteria arm the one actor whose incentives
are already two-sided — the challenger, who stakes DREAM that is burned if they
lose — with an objective standard the author agreed to up front.

> That reasoning rules out *stakers* and *per-submission juries* as judges — not
> judging as such. [Initiative Review](#initiative-review-design-not-yet-built)
> designs the third option both were standing in for: a bonded, accuracy-measured
> role paid for reviewing rather than for approving. Criteria are the standard it
> would judge against, which is why they are declared before work starts.

They are consumed in two places:

- **`MsgCreateChallenge.criteria_id`** — the challenger may name the criterion
  the work fails. Validated against the initiative and stored on
  `Challenge.criteria_id`, turning "the work is bad" into a question a jury can
  adjudicate. Validated *before* the stake is locked, so a typo costs nothing.
- **`MsgSubmitJurorVote.criteria_votes`** — a juror's per-item verdicts must
  answer criteria the initiative actually declared. Until `acceptance_criteria`
  existed these ids resolved to nothing, which is what left `CriteriaVote`
  decorative.

`CriteriaVote` appears on `MsgSubmitJurorVote` and nowhere else.
`MsgApproveInitiative` used to carry the same field and read it nowhere; it has
been removed rather than left in place, because a field the handler silently
discards is worse than no field — clients populate it and users believe it did
something. A reviewer's verdict is the `approved` flag plus `comments`; the
per-criterion form belongs to the jury, which is the body that adjudicates
against the standard.

Citing a criterion is optional; an initiative that declared none can only be
challenged free-form. But citing one that does not exist is an error
(`ErrUnknownAcceptanceCriterion`, 1706) rather than a silently ignored field.
Malformed criteria at creation are rejected with `ErrInvalidAcceptanceCriteria`
(1705): ids must be unique and non-empty, every criterion needs a question, and
the set is bounded by `MaxAcceptanceCriteria` (20).

Both fields are reachable from the CLI: `create-initiative --acceptance-criteria`
takes the criteria set as JSON, and `create-challenge --criteria-id` names the
criterion. Without them the feature would be reachable only by a client that
builds the proto message directly.

Criteria are declared per-initiative. The `InterimTemplate` registry they might
otherwise have referenced is gone: no message ever created one and all three
networks shipped zero, so the reference would have pointed into an empty store.

#### Inconclusive Juries Have a Terminal Path

A jury that fails to reach quorum (`len(jurors)/2 + 1`) returns
`VERDICT_INCONCLUSIVE`, which does **not** resolve the challenge. `TallyJuryVotes`
raises an `ADJUDICATION` interim for the Operations Committee and leaves the
challenge in `IN_JURY_REVIEW` — a status `HasActiveChallenges` counts as active,
so `CanCompleteInitiative` never returns true again.

**Committee action is a structured verdict.** Completing an `ADJUDICATION`
interim needs the same policy signer as an approval (Commons Operations
Committee policy, Commons Council policy or x/gov; `ErrNotGovAuthority` for a
single member) and requires `MsgCompleteInterim.decision` — `ADJUDICATION_DECISION_UPHOLD`
resolves the challenge as upheld (work fails), `ADJUDICATION_DECISION_REJECT`
as rejected (work stands); the verdict is recorded on the interim for the audit
trail. The decision used to be parsed out of the free-text completion notes,
with resolution errors discarded: a keyword that failed to match left the
challenge unresolved "awaiting a clearer decision" while the interim went
`COMPLETED` — out of every sweep, nothing left to retry, the initiative frozen
forever. A missing decision is now an error while the interim is still live,
and a failed resolution propagates and rolls the whole completion back, so the
same decision can simply be re-sent.

**`MsgApproveInterim` refuses adjudications outright.** It shares the Operations
Committee gate with `MsgCompleteInterim` but carries only a bool, and "approved"
cannot express a verdict — approving the committee's *work* is a different
question from whether the *challenge* is upheld. Left open, it was the same
freeze through the other door: `approved: false` finalized the interim to
`EXPIRED` with no decision and no resolution, and `EXPIRED` leaves
`IteratePendingInterims`, so the default-REJECT backstop below never fired and
the challenge sat in `IN_JURY_REVIEW` with nothing left to retry it. The refusal
keys on the interim *type*, not on whether a `reference_id` is set, because the
payout branch would also pay an `ADJUDICATION` interim's budget — which
`CompleteInterimDirectly` deliberately skips, committee adjudication earning no
DREAM. Error: `ErrInvalidRequest`.

If the committee never acts, `ExpireInterim` resolves the challenge by
**default REJECT**: the challenger's stake is burned, the challenge is closed,
and the initiative returns to its pre-challenge status (see the snapshot rules
above). Event: `challenge_resolved_by_timeout`. The expiry and the resolution
it triggers commit together or not at all — `RejectChallenge` has real mid-way
failure modes (stake unlocked, burn failed), and the expiry used to be marked
first, putting the interim out of the pending sweep with the resolution
half-done and unretryable. A failed expiry now leaves the interim `PENDING` for
the next block.

> The module authority is the assignee of record on every `ADJUDICATION`
> interim, so it is exempt from `MaxActiveInterimsPerMember` — counting it
> turned the per-member anti-monopolization cap into a chain-wide concurrency
> limit on committee escalations, where the 11th concurrent inconclusive jury
> failed to raise its interim at all.

> The direction is deliberate. Defaulting to UPHOLD would pay a challenger for a
> jury that never sat, making it profitable to challenge good work and wait for
> apathy. REJECT burns the stake instead, so engineering a stalled jury costs the
> person who engineered it. It does mean unreviewed work can be paid — but
> conviction thresholds and the challenge window still apply, and an unbounded
> freeze is the worse failure.

Without this, a below-quorum jury froze the initiative permanently, with the
challenger's stake, every staker's conviction DREAM, and the assignee's
self-assign bond locked inside it — recoverable only by a manual committee
action that nothing scheduled or prompted.

**Every resolution path commits all-or-nothing.** The EndBlocker's
jury-deadline sweep (`ResolveExpiredChallengeJuryReviews`) runs each
`TallyJuryVotes` in a child `CacheContext` and commits only on success:
`TallyJuryVotes` writes the verdict, charges no-shows and de-indexes the review
*before* it resolves the challenge, so a mid-resolution failure on deliver
state used to commit the de-indexing — the sweep would never revisit the
review — while the challenge stayed `IN_JURY_REVIEW` and the initiative
`CHALLENGED` forever. A discarded branch keeps the review `PENDING`, so the
next block retries it. (The vote-triggered tally needs no wrap:
`MsgSubmitJurorVote` is a msg-server path and gets its rollback from `runTx`;
the EndBlocker's auto-uphold and completion sweeps carry the same wrap — see
the EndBlocker section.)

#### Finding Your Jury Duty

Jury duty is drawn by lot, pays `StandardComplexityBudget`, and arrives without
warning. `JuryReviewsByJuror` indexes seatings by juror address, and the query of
the same name (`jury-reviews-by-juror [juror] [--pending-only]`) lets a juror or
their monitoring client find outstanding summons without paging through every
review on the chain. The `jury_review_created` event carries the seated
addresses and the deadline, so a lightweight notifier can work off an event
filter alone.

An unnoticed summons is the main way a jury loses quorum, which is why discovery
is part of the accountability story rather than a client-side nicety.

#### Accepting or Declining Jury Duty

Jurors are conscripted by sortition — nobody volunteers for a specific dispute —
so the consequences for absence only stay fair if saying "not me" is free.

- **`MsgAcceptJuryDuty`** turns a seat drawn by lot into a commitment to vote.
  Idempotent.
- **`MsgDeclineJuryDuty`** releases the seat immediately. It is **never**
  recorded as a no-show, and the seat is subtracted from the participation
  rate's denominator via `total_declined`. Ignoring the summons is what costs
  the seat and counts against the rate.

> The denominator adjustment is the substance, not a detail. `RecordJurySeating`
> counts a seat the moment the lot draws it, so a declined seat left in the
> denominator would make declining cost exactly as much as silence — three
> declines then one genuine miss reads as 0/4 rather than 0/1, excluding a juror
> on their first actual lapse. Declines are tracked separately rather than by
> decrementing `total_assigned` so a juror who declines everything stays visible
> as such: they are behaving correctly and must never be excluded for it, but
> they are a seat the lot keeps wasting, which is the signal a future
> selection-weight adjustment would read.

An explicit decline is worth little as a *speed* optimisation — it saves a small
fraction of the review window. Its value is the **record**: without it,
"unavailable" and "ignoring the summons" are the same event, and the
responsiveness weight would discount honest unavailability. It is also the
consent primitive any future slashing depends on, since penalising an accepted
seat is only fair when refusing one was free.

**A decline is never refused for the jury's convenience.** The only rejections
are the three in `jurorSeatGuard`: the review already has a verdict
(`ErrJuryReviewResolved`, 1707), the address is not seated
(`ErrNotSeatedJuror`, 1708), or the juror already voted
(`ErrJurorAlreadyVoted`, 1709). Refusing a decline because the jury would get
too small is **not** in that class, and must not be added.

> The reason is worth stating, because "block the decline that breaks quorum"
> looks like an obvious guard. A refused decline leaves the juror two options:
> serve when they may be unavailable, unqualified or unwilling, or say nothing.
> Saying nothing is free — `RecordJuryNoShows` charges reputation only to jurors
> who *accepted*. But the two are not equivalent in the record: a decline counts
> toward `answered` in the responsiveness weight, while silence increments
> `total_timeouts` and does not. So refusing a decline converts an honest,
> early, informative "not me" into a silent no-show, **costs the juror draw odds
> they would otherwise have kept**, and destroys the signal — to solve a quorum
> problem that is not the juror's fault.
>
> It would also collapse the justification for the abandoned-seat penalty, which
> is fair *only* because handing the seat back was free and immediate.
>
> The jury adapts to the juror, never the reverse. When a roster shrinks past
> viability the right outcome is that it loses the power to decide — see the
> quorum floor below — not that a conscript loses the power to leave.

#### Jury Selection

Every jury (initiative challenge, content challenge, gov-action appeal,
migration objection, external `CreateSeatedAppeal` case) is drawn by one
routine, `SelectJury(ctx, review, seats)`, under two rules (REP-R-102,
REP-E-102):

- **No draw inside a party's transaction, and no seed anyone can choose.**
  The transaction that opens a review (the assignee's `MsgRespondToChallenge`,
  the author's content response, an appeal, `MsgObjectMigration`, an external
  case) or frees a seat (`MsgDeclineJuryDuty`) only records `pending_seats`
  and queues a `PendingJuryDraw{jury_review_id, seed_epoch, queued_at_height}`
  in `PendingJuryDraws`. The seed is the x/shield **randomness beacon**
  (`docs/x-shield-spec.md` "Randomness beacon"): `seed_epoch` is the shield
  epoch after the one open when the seats were queued, and its beacon value
  (the epoch's TLE key, a threshold BLS signature over the epoch number) does
  not exist until that epoch has closed and a threshold of validators has
  released their shares. Neither the sender, nor the proposer of any block,
  nor any validator coalition below the threshold knows it when the review
  opens, and the value is unique, so nobody can bias it either. A coalition
  holding `threshold` DKG shares can compute every future epoch's value
  ahead of time (inherent to a threshold-BLS beacon; x/shield "Randomness
  beacon", trust assumption), so the draw's unpredictability rests on the
  same honest-threshold assumption as TLE secrecy: such a coalition could
  know a seed in advance, never choose it.
  `DrawPendingJuries` runs in x/rep's BeginBlocker, before the block's
  transactions, so seats queued in a block (including those the EndBlocker's
  unanswered-seat sweep vacates) are drawn in the next one. The beacon value
  is rebuilt in x/shield's PreBlocker from the previous block's vote
  extensions, which anyone can read before the block's transactions are
  chosen; drawn in the EndBlocker, a proposer who computed the outcome could
  include transactions that change the juror pool or its weights and move
  who is seated. Drawn in the BeginBlocker, the pool is the state at the end
  of the previous block, settled before any validator signed the shares.
  Per queued draw it asks the beacon (`BeaconStatus(seed_epoch)`):
  `AVAILABLE` seats the review with SHA-256 over the domain
  `sparkdream/jury-draw/beacon/v1`, the beacon key and the review id (a pure
  function of key and review: two reviews seeded from one epoch do not share
  a stream); `PENDING` leaves the draw queued for a later block; `UNAVAILABLE`
  (beacon off, no DKG, a re-key retired the epoch, liveness judged without a
  key) falls back at once. A draw still `PENDING`
  `JuryBeaconWaitBlocks` after `queued_at_height` (compiled: 7 shield epoch
  intervals, one more than `max_pending_epochs` so a sub-minimum batch holding
  the key never forces a fallback; `JuryBeaconWaitEpochs` in `params_vals_*.go`, read against
  shield's `shield_epoch_interval`) falls back too. The fallback seed is the
  floor every draw had before the beacon: SHA-256 over a domain tag, the hash
  of the block being finalized (`HeaderHash`), the previous app hash, the
  height and the review id, which the sender cannot know when signing but the
  block proposer can bias. A review queued while no beacon value is in
  prospect (x/shield unwired, no DKG yet, beacon disabled) records
  `seed_epoch = 0` and takes that floor in the next EndBlocker, so a chain
  that has not run its DKG (every e2e snapshot) seats juries exactly as
  before, in the same block. The old seed (`AppHash XOR id`, drawn in the
  transaction) was known a block ahead, so the assignee or objector could
  simulate the draw and send only when it seated accomplices. The pool is
  still checked in the opening transaction (`MinSeatedJurors` for an
  initiative challenge, a full jury for a content challenge) so the escalation
  paths keep working. A `jury_seated` event (`jury_review_id`, `drawn`,
  `jurors`, `seats_requested`, `seed_source: beacon|block_hash`) lists the
  seats; every fallback emits `jury_seed_fallback` (`jury_review_id`,
  `seed_epoch`, `reason: unavailable|wait_exceeded`); `jury_review_created`
  carries `seats_pending` instead of the jurors. A refill on a review already
  queued joins the waiting draw and keeps its seed epoch. Seats queued by a
  module whose EndBlocker runs after x/rep's are drawn in the next block.
  Genesis exports `pending_jury_draw_list`; a review with `pending_seats` and
  no entry is queued with `seed_epoch = 0` at `InitGenesis`. An imported
  entry is clamped to the chain it lands on (REP-R-206): a `queued_at_height`
  ahead of the import height becomes the import height at `InitGenesis`, and
  a `seed_epoch` past the next beacon epoch is re-pointed at the next epoch
  (0 when that epoch yields no beacon) by the first draw pass that reads it,
  so a draw exported from a longer-running chain cannot sit unseated until
  the new chain catches up. The epoch is not clamped at import: x/rep's
  `InitGenesis` runs before x/shield's, so the beacon would read epoch 0 and
  no value for every epoch and send every imported draw to the block hash. The beacon is
  late-wired (`SetBeaconSource` in `app.go`, since x/shield depends on
  x/rep); a chain without it keeps the block-hash floor. With batch mode on,
  an epoch carrying a batch under `min_batch_size` releases its value only
  when the batch is demanded (at most `max_pending_epochs`), so a draw seeded
  from such an epoch may wait longer or fall back.
- **The case decides who is excluded, never the caller.** `SelectJury` derives
  the exclusions from the review: everyone seated, the expert witnesses, the
  initiative challenger and every initiative affiliate (assignee, apprentice,
  creator, project creator), a content challenge's challenger and author, and
  `excluded_jurors`, which holds the parties recorded when an appeal-style
  case opened (appellant, action target, hiding sentinel; migration from/to;
  an external case's parties) plus every juror who declined or was vacated for
  silence. All are compared as canonical members. Every draw (the first one,
  a decline refill, a sweep redraw) therefore excludes the same people, and
  `InviteExpertWitness` refuses anyone on the same list. The redraw paths used
  to pass only the current seats, so one decline could seat the challenger on
  their own jury.

Weights are integers (REP-R-107): a candidate's reputation total times
`JurorResponsivenessWeight` (both `LegacyDec`), taken as the decimal's
18-digit integer, and the draw picks `r` uniformly in `[0, total)` with
`big.Int.Rand` on the seeded PRNG. No floating point is involved.

#### Replacing Unanswered Seats

`SweepUnansweredJurySeats` runs each block. For a `PENDING` review past its
`acceptance_deadline`, every juror who neither accepted nor voted loses the
seat, the silence is charged to their participation record, the juror joins
`excluded_jurors`, and a replacement is queued for the draw later in the same
EndBlocker.

**The window counts from the seating.** `acceptance_deadline` is first set
when the review opens, but the seats are filled by a later BeginBlocker draw,
with the beacon pending up to `JuryBeaconWaitBlocks` later. Whenever the draw
seats at least one juror on an initiative jury that still has redraws left,
it restarts the window from that block (`restartAcceptanceWindow`, clamped
strictly inside the vote deadline). Before this (REP-R-201) a window shorter
than the draw latency elapsed with nobody seated, the sweep found no silent
juror and stood the clock down for good, and the jurors seated afterwards
were never swept: a no-show jury ran to its vote deadline and INCONCLUSIVE.

> **Replacement, not reinforcement.** Quorum is `len(jurors)/2 + 1` and
> `required_votes` is `jury_super_majority × len(jurors)`, both computed from
> the **seated** list. *Adding* jurors to a stalling jury therefore raises the
> bar it is already failing to clear. Seats are swapped one-for-one instead, so
> jury size and quorum hold steady; `required_votes` is recomputed against the
> new roster.

Vacating stops at `MinSeatedJurors` (3). Quorum is `len(jurors)/2 + 1`, so if
replacements cannot be drawn every vacated seat shrinks the jury *and* its
quorum — an unguarded sweep could leave one juror holding a quorum of one, able
to uphold a challenge and burn the assignee's bond alone. At the floor the
remaining silent jurors keep their seats, quorum holds, and the review falls
through to its deadline tally (and from there to the terminal path) rather than
being decided by a rump.

##### Seat-lifecycle defects found and fixed

Three problems, found together and fixed together. The first two were live
correctness bugs; the third is why they were not caught by use. Recorded because
the reasoning constrains anything that touches seat handling again.

**The floor guards the involuntary path only.** `DeclineJuryDuty` calls
`vacateJurySeat` directly, with no `MinSeatedJurors` check and no
`required_votes` recompute — both of which the sweep does. The guard was added
to the path where seats are taken (silence) and not to the path where they are
given back, which is the path the design actively encourages as free.

The consequence is reachable by ordinary behaviour. On a three-juror jury, two
jurors decline — entirely normal, since declining is meant to cost nothing —
leaving one. Quorum is `1/2 + 1 = 1`, so at the deadline tally that juror votes,
`rejectVotes > totalVotes/2` holds at `1 > 0`, and they **single-handedly reject
the challenge and burn the challenger's stake**. The uphold direction is blocked
only by accident: `required_votes` is stale at the original roster's
supermajority, so one vote cannot clear it. That asymmetry is leftover state,
not a decision.

**The sweep is inert at `jury_size = 3`.** `vacatable` is
`len(jurors) - MinSeatedJurors`, and devnet and testnet both ship
`jury_size = 3` against a floor of 3 — so it is always zero, and the sweep
vacates nothing, redraws nothing and returns. The acceptance window,
`MaxJuryRedraws` and replacement selection do nothing on those networks. Only
mainnet's `jury_size = 5` leaves the mechanism two seats of headroom.

**The acceptance window is a fixed constant against a variable review period.**
`JuryAcceptanceWindowBlocks` is 1200 blocks (~2 hours) regardless of network:

| Network | Review deadline | Acceptance window | Ratio |
|---|---|---|---|
| mainnet | `7 x 14400` = 100,800 blocks (~7 days) | 1,200 (~2h) | 1.2% |
| devnet | `3 x 300` = 900 blocks (~1.5h) | 1,200 | 133% |

On devnet and the test configuration the acceptance deadline falls *after* the
vote deadline, so the sweep can never fire before the review resolves. The
constant is simultaneously far too short for a human on mainnet and longer than
the entire review everywhere else.

Two hours also contradicts this document's own reasoning about no-shows: absence
goes unpenalised precisely because a member cannot be expected to monitor the
chain continuously for an event reaching them roughly once a year — and then the
seat is withdrawn if they do not answer within two hours. Both cannot be right.
The window should be sized to how long it takes someone to read a notification,
not to react to a block.

##### How they were fixed

- **The decision threshold is floored, never the roster.** `TallyJuryVotes`
  raises quorum to at least what a minimum-size jury would require
  (`MinSeatedJurors/2 + 1` = 2), so no verdict is ever binding on fewer than two
  concurring jurors. A roster thinned past that returns `INCONCLUSIVE` and takes
  the terminal path its review type already has — adjudication interim for
  initiative challenges, `ResolveInconclusiveContentChallenge` for content,
  `TimeoutExpiredAppeals` for appeals. The rump loses the power to decide; the
  juror keeps the power to leave. A two-juror jury drawn from a thin pool still
  decides, so this does not wedge young chains.
- **`required_votes` is recomputed on every seat change** through a shared
  `recomputeRequiredVotes`, used by both the decline path and the sweep, so the
  supermajority always tracks the live roster rather than leaving stale state to
  protect one verdict direction by accident.
- **A decline refills immediately.** It is the earliest vacancy signal there is
  and therefore the one with the most review time left to act on; it used to
  vacate and draw nobody, waiting for a sweep that at `jury_size = 3` never ran.
  The decline queues the replacement and the EndBlocker of the same block draws
  it when the pool allows (never inside the decliner's own transaction, see
  "Jury Selection" below); the quorum floor covers the case where it does not.
  The `jury_duty_declined` event carries `seats_pending` and the resulting
  `seated` count; the replacements appear on that block's `jury_seated` event.
- **The acceptance window is now a fraction of the review period.**
  `jury_acceptance_window_ratio` (Params-only, default **0.25**) replaces the
  fixed `JuryAcceptanceWindowBlocks` constant, clamped into
  `[1, reviewPeriod - 1]` so it is always answerable and always strictly inside
  the vote deadline. Mainnet gives ~42 hours to notice a summons; short-period
  networks scale down instead of overshooting their own deadline. Validation
  rejects values outside `(0,1)`: zero would sweep every seat on the block it
  was drawn, and one or more restores the unreachable-sweep bug.
  `MaxJuryRedraws` drops to **1** at that width, so redraw rounds cannot consume
  the time replacement jurors need to read the work. Discovery already supports
  the wider window: `jury-reviews-by-juror` and the juror addresses on the
  `jury_seated` event.
- **Jury candidates come from a reputation index.** `Member` is an
  `IndexedMap` with a `MaxRep` multi-index keyed by the integer part of the
  member's highest tag reputation, maintained on every member write.
  `SelectJury` (the one draw every review type uses) reads it in descending
  order down to
  `floor(MinJurorReputation)`, at most 1,000 candidates (`maxJurorCandidates`,
  the most reputable), sort them by address and then apply the exact
  eligibility rule and the weighted draw as before. No draw walks the whole
  member store (REP-E-10). Only `MEMBER_STATUS_ACTIVE` records are seated;
  the status check is a belt over the index (a zeroed member's cleared
  scores already drop them below the floor, a tombstone has no row).
- **Jury selection shrinks to fit instead of refusing outright.** `SelectJury`
  used to error whenever the eligible pool was smaller than `jury_size`, so a
  pool one juror short produced *no* jury at all and escalated to the committee
  — a worse outcome than a slightly smaller jury, and a sharp cliff the moment
  `jury_size` was raised. It now seats what the pool allows; an empty pool is
  still an error, preserving the escalation path that matches on that message.
  This is safe only because quorum is floored: a short jury cannot return a rump
  verdict. `CreateJuryReview` enforces `MinSeatedJurors` itself and escalates
  below it, since a jury under the floor could never reach quorum and seating
  one would strand the challenge until its deadline. Replacement draws take
  whatever they get, as they always did.

- **The sweep has headroom on every network.** devnet and testnet `jury_size`
  goes 3 → 5 (mainnet was already 5), so `vacatable` is no longer pinned at
  zero. Raised rather than lowering `MinSeatedJurors`, since that floor is what
  stands between a thinned jury and a rump verdict.

- **`jury_size` is validated against that floor**, on both `Params` and
  `RepOperationalParams`. This is the one that made the rest durable: `jury_size`
  is **committee-editable**, and validation checked only that it was odd — so an
  operational-params update could set it back to 3 and silently re-disable the
  sweep without a governance vote, or set it to 1 (also odd) and leave every
  jury unable to reach quorum. The genesis change alone was a value anyone could
  revert past. Covered by `TestJurySizeMustExceedSeatedJuryFloor`.

- **The window and the redraw cap are validated together.** Each redraw round
  costs one acceptance window out of the review period, so
  `jury_acceptance_window_ratio x (max_jury_redraws + 1)` must stay below 1 — a
  wide window and several rounds cannot both be configured, or replacement
  jurors inherit no time to read the work.

> Content-challenge and moderation-appeal juries were checked before the floor
> went in, since they use their own selection routines and can be seated small
> from a thin pool. All three review types have a terminal `INCONCLUSIVE` path
> already, so the floor cannot wedge any of them — it converts a rump verdict
> into an escalation rather than into a stall. Refilling on decline stays
> initiative-challenge-only, because the other two are selected against targets
> a challenge review does not carry.

Bounded twice: `MaxJuryRedraws` (2) rounds per review, and
`maxJuryDeadlinesPerBlock` reviews per block. A review at the cap falls through
to its deadline tally. Only initiative-challenge juries are redrawn — content
and appeal juries use their own selection routines, so their unanswered seats
are simply vacated, which lowers quorum rather than stranding it against absent
jurors.

Charging the no-show at vacate time is load-bearing: `RecordJuryNoShows` reads
the seated list at tally time, so an un-seated juror would otherwise escape the
record entirely.

#### Juror Vote Domain

`MsgSubmitJurorVote` accepts only a vote that takes a side: `verdict` must be
`VERDICT_UPHOLD_CHALLENGE` or `VERDICT_REJECT_CHALLENGE`, and `confidence` must
lie in `[0, 1]`; anything else fails with `ErrInvalidJurorVote` (2414) before
the seat, the vote list or participation is touched. `VERDICT_PENDING` and
`VERDICT_INCONCLUSIVE` are outcomes the tally records, never votes. The check
runs in the message server and again in `Keeper.SubmitJurorVote`, and applies to
every case type (initiative and content challenges, moderation appeals,
external cases opened by other modules), which are all decided on the same two
verdicts. Without it a seated juror could file a non-deciding vote that used up
the seat, counted toward the tally trigger and the supermajority denominator
while supporting neither side (forcing an `INCONCLUSIVE` result), and still
counted as having voted for participation and juror pay.

#### Juror Pay

Juror pay scales with the amount in dispute: each seat is worth
`initiative.budget × juror_reward_rate / len(jurors)`, floored at
`min_juror_reward` (5 DREAM) and capped at `StandardComplexityBudget`.

Both halves are parameters, and the floor is the one that carries most of the
weight: content challenges and moderation appeals have no initiative budget to
scale against, so for those the floor **is** the entire rate. Leaving it a
compile-time constant meant the single number setting juror pay for two of the
three review types could only move in a chain upgrade.

It is a fixed **per-seat** share rather than a pool split among whoever turned
up, so a juror never earns more because their colleagues stayed home — the
unclaimed shares are simply never minted. Content challenges and moderation
appeals carry no initiative budget and pay the floor.

> Pay was previously a flat `StandardComplexityBudget` regardless of what was in
> dispute, so a challenge over a 100 DREAM APPRENTICE initiative minted 750
> DREAM in juror fees to settle it — several times the value of the work. A jury
> should cost a fraction of what it is judging.

This per-seat share is the only juror pay. Seating a jury no longer creates a
`JURY_DUTY` interim per seat: those paid a flat STANDARD budget on top, and a
seated juror could complete theirs at once and then decline the seat or never
vote.

#### Expert Witnesses

A juror seated on a pending initiative jury review can call an expert witness
with `MsgInviteExpertWitness`, and a second juror must endorse the call with
`MsgEndorseExpertWitness` before it becomes an engagement. Jurors are drawn by
lot and often lack the domain knowledge a deliverable needs; the expert
supplies it, and the jurors still decide.

- **Who invites**: any juror seated on the review (`ErrNotSeatedJuror`). The
  review must be an initiative review (not a content challenge or moderation
  appeal, which carry no tags to measure expertise against), still `PENDING`
  and before its deadline (`ErrNotInitiativeReview`).
- **Who endorses**: a *different* juror seated on the same review who has
  answered the summons, meaning they accepted jury duty or already voted (the
  same definition the redraw sweep uses for an answered seat), while the review
  is still `PENDING` and before its deadline. The inviting juror can never
  endorse their own invitation, a silent seat cannot endorse, and an invitation
  is endorsed at most once (`ErrInvalidEndorsement`; a non-juror gets
  `ErrNotSeatedJuror`). The review records both seats (`expert_inviters`,
  `expert_endorsers`, parallel to `expert_witnesses`; the endorser is `""`
  while pending) and emits `expert_witness_endorsed`. Until endorsed, the
  invitation is **pending**: the expert's testimony is refused
  (`ErrExpertNotEndorsed`), and when the review reaches its verdict or is
  voided the invitation's interim is expired unpaid. This is what closes
  REP-E-103's residual: expert pay needs two jurors, so a lone juror can no
  longer mint 400 DREAM to an accomplice. A review always seats at least
  `MinSeatedJurors` (3) jurors, so a second juror exists; if every other juror
  stays silent, declines or is vacated, the invitation simply expires unpaid
  with the verdict and the jurors decide without the expert. Pending
  invitations count against the per-review cap below: an unendorsed call still
  occupies a slot until the review closes, so a juror cannot fill the review
  with speculative calls either way.
- **Who can testify**: an active `ESTABLISHED`+ member with positive reputation
  in at least one of the initiative's tags, who is not the challenger, anyone
  affiliated with the initiative (assignee, apprentice, creator, project
  creator), a juror on the review or a juror who declined or was vacated
  (`juryExclusions`, the list jury draws use), nor an **invitation neighbour**
  (inviter or invitee) of anyone on that list, the inviting juror included
  (`excludedAsExpert`), and is not already listed (`ErrExpertIneligible`). The
  neighbour rule is expert-only: one juror directs this pay, while jury draws
  are random.
- **How many**: at most `MaxExpertWitnessesPerReview` (2, a compiled constant)
  per review, endorsed and pending invitations together
  (`ErrTooManyExpertWitnesses`): each invitation is potential paid work.
- **Testimony**: once per endorsed expert, while the review is still
  `PENDING`, by an expert who is still an active member. The opinion is
  `MinExpertOpinionLength`..`MaxExpertOpinionLength` bytes (64..4096,
  whitespace-trimmed) and the reasoning at most `MaxExpertReasoningLength`
  (4096) bytes (`ErrInvalidTestimony`): the floor makes the paid work
  non-trivial, the ceilings bound the review record every vote re-encodes.
- **Pay**: the invitation opens an `EXPERT_TESTIMONY` interim assigned to the
  expert (COMPLEX budget, `reference_id` = the review id, deadline = the review
  deadline), subject to the expert's `max_active_interims_per_member` cap
  (counted over the expert's own interims; system seats do not count), and
  records its id on the review (`expert_interim_ids`, parallel to
  `expert_witnesses`). `EXPERT_TESTIMONY` is system-only: `MsgCreateInterim`
  refuses it, and `MsgApproveInterim` refuses to approve it. Testifying pays
  nothing. When the review reaches its verdict (`TallyJuryVotes`, any verdict)
  each expert's own interim, looked up by that id, is settled: completed and
  paid (charged to the season's interim reward cap like any interim) if the
  invitation was endorsed and the expert testified, expired if not (the
  settlement re-checks the endorsement, so a testimony on record never pays an
  unendorsed invitation). A review voided before its verdict (the
  challenge voided with its project, or its challenger zeroed) expires every
  expert interim unpaid. A payment failure never blocks the verdict: it is
  reported as an `expert_testimony_unpaid` event and the interim is expired.
  The EndBlocker's interim deadline sweep skips an expert interim while its
  review is still `PENDING`, so a deadline tally deferred by the per-block cap
  still pays. An expert who abandons the interim (`MsgAbandonInterim`)
  forfeits the pay.

#### Juror Accountability

**Ignoring a summons is not penalised.** It briefly was — an unanswered seat
cost a participation-rate mark and eventually a timed exclusion from selection —
but two changes removed the harm that was pricing: the adjudication terminal
path resolves an inconclusive jury safely, and the redraw sweep replaces an
unanswered seat within the acceptance window. What remains of a no-show is some
delay in a week-long review, and pricing that would oblige every eligible member
to monitor the chain continuously for an event that reaches them roughly once a
year. Under broad sortition, non-response is the expected default of a
population that never volunteered; penalising the default punishes an accident
of the draw.

> This reasoning is the reason the acceptance window has to be sized in days
> rather than hours, and the reason a decline can never be refused. A design
> that excuses non-response because nobody watches the chain continuously cannot
> then withdraw a seat for not answering within two hours, or trap a juror who
> answers honestly. See [Known defects in the seat
> lifecycle](#known-defects-in-the-seat-lifecycle-not-yet-fixed).

`JuryParticipation` still records seatings, votes, declines and timeouts, and
the record still does work — as **selection weight**:

- `JurorResponsivenessWeight` multiplies a juror's reputation-derived selection
  weight by `(voted + declined) / assigned`.
- Declines count as answering. A prompt decline frees the seat, which is exactly
  the behaviour the lot wants to keep drawing.
- Below `MinJurySeatingsForWeighting` (3) seatings there is no meaningful record
  and the juror is drawn at full weight.
- The multiplier is floored at `MinJurorSelectionWeight` (0.1). A juror who
  never answers is drawn less often, **never not at all** — a zero-weight
  address is excluded in all but name, with no way to earn its way back since it
  would never be drawn again to demonstrate otherwise.

This fixes the pool-efficiency problem exclusion was really solving, without
taking anything from anyone and without removing an address from the lot.

> **Both are guesses, and both are now tunable.** `min_juror_selection_weight`
> (0.1) and `min_jury_seatings_for_weighting` (3) were chosen for shape, not
> fitted to anything, so they are parameters rather than compile-time constants
> — a value you intend to revisit against observed response rates should not
> need a chain upgrade to move. The floor in particular trades pool breadth
> against the cost of repeatedly drawing an address that never answers. The
> `Default*` constants of the same name survive only as genesis seeds and as the
> fallback for a chain whose stored params predate the fields.

**Abandoning an accepted seat is penalised.** A juror who signs
`MsgAcceptJuryDuty` and then lets the seat lapse is charged
`abandoned_jury_seat_penalty` reputation (default 10) against each tag of the
disputed initiative, and the seat is counted in `total_abandoned`.

> This is fair precisely because declining is free and immediate. The juror was
> drawn, was told, could have handed the seat back at no cost, and instead took
> it and held it empty until the deadline. Reputation is the right currency
> because it is what qualifies a juror: at `min_juror_reputation` 50, four
> abandoned seats cost an otherwise qualified member their eligibility in that
> tag, so the penalty is self-limiting rather than unbounded.

Reviews with no initiative (content challenges, moderation appeals) have no tags
to charge against and are skipped rather than charged arbitrarily. Setting the
penalty to 0 disables it.


#### Initiative Review

**The gap.** Nothing in the happy path reads the deliverable. Completion turns
on conviction, which measures whether people *wanted the work done* — not
whether it *was* done. Acceptance criteria gave a challenger a standard to cite;
they gave nobody a reason to look in the first place.

Both obvious electorates are disqualified, and the reasons are worth keeping
because they constrain any future redesign:

- **Stakers are the wrong judges twice over.** They are paid on completion, so
  they are paid to pass the work. And backing is simply not reviewing: a staker
  says *I want this to exist*, which is a judgement about a proposal, made
  early, often by someone with no expertise in the deliverable. They cannot even
  express a verdict when they stake — `MsgApproveInitiative` opens only in
  `SUBMITTED` / `IN_REVIEW`, so for the whole `OPEN` → `ASSIGNED` phase, when
  conviction is accruing, there is nothing to vote on.
- **A lot-drawn jury per submission** conscripts reviewers for undisputed work
  and prices every completion at a jury.

The answer is a third role that does nothing else.

##### A bonded role, modelled on content sentinels

`ROLE_TYPE_INITIATIVE_REVIEWER`, owned by x/rep, reusing `BondedRole`,
`BondedRoleConfig` and `RoleActivity` unchanged. Bonding and unbonding are the
existing `MsgBondRole` / `MsgUnbondRole` with the new role type — no new
lifecycle messages.

**Why not simply let content sentinels do it.** Role types are module-scoped and
a distinct job gets a distinct type (see
[bonded-role-generalization.md](untracked/bonded-role-generalization.md)). Four reasons
apply here specifically:

- **Different competence.** Sentinel work is policy application — abusive,
  off-topic, miscategorised — and is largely domain-independent. Review is
  technical evaluation against acceptance criteria and needs expertise in the
  initiative's own tags, which is exactly what `requires_domain_rep` and
  `min_verifier_reputation` exist to express.
- **Liability differs by orders of magnitude.** A wrong hide costs a post some
  visibility. A wrong approval **mints DREAM** — up to 10,000 for an EPIC
  initiative — and minted DREAM cannot be clawed back. Bond must scale to
  liability, and a single `min_bond` cannot serve both.
- **Slash isolation.** Merged, a sentinel overturned on a forum hide would lose
  bond that was simultaneously backing their initiative reviews. Independent
  bond pools per role type is a deliberate property of the primitive.
- **Accuracy denominators.** `RoleActivity` is keyed `(role_type, address)`
  precisely so a high-volume accurate sentinel cannot carry a poor reviewer
  record. Pool them and accuracy-gated pay stops meaning anything.

**The population objection dissolves.** The one real argument for merging is
that a young chain may not have enough members for two rosters — but
`BondedRole`'s key is `(role_type, address)`, so **one address can already hold
both roles**. If the pool is thin the same people bond both, and none of the
bonds, configs, slash surfaces or accuracy records get coupled. Merging to share
people is unnecessary.

##### Who may review what

`VerificationPolicy` on the parent project is the config, and this is what its
fields are for:

| Field | Role |
|---|---|
| `default_review` | which process this project uses |
| `min_verifier_count` | approvals required before completion; **0 = conviction-only** |
| `min_verifier_reputation` | the bar to review here; defaults to the initiative tier's own `min_reputation` |
| `requires_domain_rep` | that reputation must be in the initiative's tags |
| `requires_creator_approval` | the project creator must sign off as well |
| `review_period_epochs` / `challenge_period_epochs` | per-project windows |

A reviewer qualifies for a given initiative when they pass the shared
`EligibleForRole` gate (tombstone, `min_age_blocks`, status, staying bond,
demotion cooldown; `QualifiedReviewer` does not re-implement any of it,
REP-R-204) **and** hold the role at `NORMAL` status (RECOVERY and UNBONDING
pass the shared gate but back no new liability), clear the reputation bar,
and are **independent of the work** — the same `InitiativeAffiliates` plus
one-invitation-hop test that gates external conviction, reused rather than
reinvented.

**A staker on an initiative may not review it.** Holding conviction on the
outcome is the conflict this whole role exists to remove; permitting it would
reintroduce the problem one layer down.

**Windows may only lengthen.** `review_period_epochs` and
`challenge_period_epochs` are clamped to `max(global, project)`. Without that, a
permissionless project creator could shrink their own contest window toward zero
and walk past the brake the self-assignment safeguards depend on. A project may
be more conservative than the chain default, never less — which is why the
override needs no approval.

##### Configuring it

`MsgSetVerificationPolicy(creator, project_id, policy)` — project creator or
Operations Committee, while the project is `ACTIVE`. Settable rather than fixed
at creation because the reviewer roster grows over time: a project has to be
able to turn review on once reviewers exist, and creation-time-only would strand
every project made before then on conviction-only permanently. The policy's
windows are clamped to `max(global, project)` on write, and `min_verifier_count`
is bounded by `MaxVerifierCount` (10) — a project demanding more approvals than
the roster can supply stalls every initiative under it.

##### The completion gate

`CanCompleteInitiative` gains one condition when `min_verifier_count > 0`:
approvals from qualified reviewers must reach `min_verifier_count`, plus the
creator's approval when `requires_creator_approval`. Conviction gates, the
challenge window and the no-active-challenges rule all still apply — review is
an **additional** brake, never a substitute for one.

`MsgSubmitInitiativeReview(reviewer, initiative_id, approved, criteria_votes,
comments)` records a verdict, reserving bond against it the way a sentinel
action does. This is also the correct home for `CriteriaVote` — a per-criterion
verdict belongs to someone accountable for getting it right, which is why
removing the field from `MsgApproveInitiative` (where nothing read it) was right
rather than merely tidy.

##### Pay: two components, and only one of them is optional

**Reviewing must pay per completed review, never per approval.** If approving
pays and rejecting does not, the role rebuilds "paid to say yes" one layer down.
This is the single constraint the reward design cannot trade away.

- **DREAM per review, from the initiative budget**, scaled by the tier's
  existing `TierConfig.reward_multiplier` (0.5 / 1.0 / 1.5 / 2.0 across
  APPRENTICE → EPIC — no new parameter needed). Self-funding and available from
  day one. It mirrors juror pay, which already scales against the disputed
  budget. On rejection the budget returns to the project **minus** the review
  fees of every round: the project pays for having had the work evaluated,
  which is where that cost belongs.
- **SPARK from an accuracy-gated pool**, distributed on the sentinel schedule
  with the same cap-and-overflow-burn treatment. This is the quality component —
  the reason to hold the role rather than merely fill it.

> **Funding the SPARK pool.** The pool is fed automatically from the community
> pool — see [Automatic funding for bonded-role pools](#automatic-funding-for-bonded-role-pools)
> below. Unlike the sentinel pool, it is deliberately *not* also fed by
> forfeited bonds: that arrangement has a perverse property even for sentinels,
> since good moderation means few appeals means an empty pool, and for reviewers
> it would be worse — good reviewing means few challenges, so the pay would dry
> up for exactly the expert labour it exists to compensate.
>
> `ReviewerRewardPoolAddress()` remains an ordinary bank sub-address, so a
> council can still top it up with a plain send; automatic funding is a floor,
> not a monopoly. The DREAM fee also stands alone if the pool is somehow empty:
> reviewing still pays, so the roster fills and nothing falls through to the
> committee for want of compensation. An empty pool distributes nothing and is
> not an error.

Tier scaling is a **score weight within a fixed pool** for the SPARK half, and a
genuine per-review fee for the DREAM half. Absolute scaling only comes from the
component that scales with what is being reviewed.

> `TierConfig` is creator-set, so inflating tier to attract reviewers is
> conceivable. It is bounded — tier gates creation on the creator's own
> reputation, and permissionless work caps at STANDARD — but budget is the
> harder-to-fake signal if the multiplier ever looks gameable in practice.

##### Accountability, in both directions

Reviewer accuracy comes from challenge outcomes, exactly as sentinel accuracy
does, and it has to work symmetrically:

- **A completion approval is challengeable** — "reviewers passed bad work" —
  through the existing challenge flow. If the jury upholds the challenge, the
  approving reviewers are recorded as overturned and their reserved bond is
  slashed.
- **A rejection sends the work back.** `REJECTED` returns the initiative to
  `ASSIGNED` with the deliverable cleared and the round incremented, so the
  assignee fixes it and resubmits — the natural remedy for "not done". Bounded
  by `max_review_rounds` (3); the last rejection abandons the initiative through
  the existing clean exit. Verdicts are keyed by round, so a reviewer may file
  again on the new round without colliding with the one already on record.
  `GOV_ACTION_TYPE_REVIEW_REJECTION` is reserved for appealing a rejection as an
  *action* rather than resubmitting, on the `GovActionAppeal` shape.

Either way the outcome writes to `RoleActivity`'s existing upheld / overturned
counters and accuracy ring, gates the reviewer's share of the SPARK pool, and
feeds the existing consecutive-overturn demotion. Unchallenged reviews release
their reserved bond when the challenge window closes.

##### The stall path, which is the dominant risk

With the staker veto retired (below), reviewers are load-bearing: if nobody
reviews, nothing completes. Every part of this path exists to guarantee
liveness.

1. Review runs until `review_deadline` = submission + `review_period_epochs`.
2. If approvals are still short of `min_verifier_count`, the initiative
   escalates to the Operations Committee, which has three options:
   - **approve** — satisfies the reviewer gate,
   - **reject** — rejects the round; the assignee may appeal as above,
   - **let the challenge period run** — decline to substitute judgement; the
     initiative proceeds on conviction alone.
3. **Committee inaction rejects the round.** When the committee's window closes
   without a decision, the escalation-timeout sweep (`applyEscalationTimeout`)
   calls `rejectReviewRound`: the work goes back to the assignee (`ASSIGNED`,
   deliverable cleared) for a new round, the reviewers of the rejected round are
   paid their fee, and the assignee may resubmit. Rounds are bounded by
   `max_review_rounds`; when the last round is rejected the initiative closes
   cleanly through `CloseInitiative` — budget returned to the project net of
   every round's review fees, self-assign bond released, stakes settled,
   nothing minted to the assignee. Silence never passes and never mints: an
   earlier design resolved silence to PASSED, which let a gated initiative
   complete with no verdict on it at all. Silence also never wedges: this module
   has already been bitten once by an escalation that expired and touched
   nothing, freezing an initiative permanently with the challenger's stake,
   every staker's conviction and the assignee's bond locked inside it. Every
   timeout ends in a resubmittable round or a close.

**All three committee outcomes still run the challenge window.** Committee
approval satisfies the reviewer requirement and nothing else — it is not a
bypass around the one brake that does not depend on somebody showing up.
Committee approval writes **no** `RoleActivity` entry, since the committee holds
no bond and carries no accuracy record, and emits a distinct event so
committee-approved completions stay auditable.

**Bootstrap.** `min_verifier_count` defaults to **0** at genesis, which is
exactly today's conviction-only behaviour — a project that asks for no reviewers
is always satisfied and no review window opens at all. Turning the role on is a
per-project decision taken once reviewers exist, so nothing can wedge while the
roster is still filling.

##### The bond floor, and who owns it

`min_reviewer_bond` is **500 DREAM**, set deliberately low so that taking up
reviewing is within reach of an ordinary member rather than gated on holding a
large balance. An earlier 5,000 was argued from "a wrong approval mints DREAM,
up to an EPIC initiative's 10,000, and minted DREAM cannot be clawed back" —
true about the role, but not a description of what the floor does.
`SlashReviewersOnOverturn` charges `BondReserved`, the per-verdict reserve of
`reviewer_bond_reserve_rate` x the initiative's budget, never the floor.
Liability already scales with what the review could mint, whatever the floor is.
The floor's job is narrower: price entry to the role, and give demotion a
threshold to sit under.

**Capacity is a separate decision, and reviewers make it by bonding more.** What
limits a reviewer is free bond above their open reserves — `current_bond -
total_committed_bond - pending_unbond_amount`. At the default 10% reserve rate
the floor alone backs work up to roughly 5,000 DREAM of budget; an EPIC
initiative at the 10,000 cap reserves 1,000 per verdict, so a reviewer who wants
that work bonds past the floor for it. Raising the ceiling is simply another
`MsgBondRole` against the same `(role_type, address)` record: it adds to
`current_bond`, is reservable on the very next verdict with no waiting period,
and is permitted even while an unbond is in flight (a bond only ever adds
slashable collateral). Start small, grow into the role.

The trade this makes is explicit. A floor-bonded reviewer cannot file on the
largest initiatives, and if nobody with sufficient free bond files at all, the
round escalates to the Operations Committee and an unanswered escalation
resolves to `PASSED` — so thin reviewer capacity degrades toward unreviewed
completion rather than toward a wedge.

**The floor is not an availability mechanism, and raising it is not the remedy
when reviewers are scarce.** A higher floor guarantees nothing: it would seat one
EPIC verdict only for a reviewer holding no other open reserves, and bond is one
of three constraints anyway — the reviewer must also pass the independence test
(affiliates-plus-one-hop, no stake on the initiative) and must actually want the
work. `min_verifier_count > 1` compounds all three. Since a high floor cannot
manufacture willing, independent reviewers, its only reliable effect is to shrink
the roster it is drawn from, which is the opposite of what scarcity calls for.

The levers that do move availability are `reviewer_bond_reserve_rate` (lower
reserves let the existing roster cover more and larger work at once), the pay —
`review_fee_rate` and the accuracy-gated SPARK pool — and per-initiative
`MsgFundReviewBounty` escrow to attract attention to specific work. A low
`min_reviewer_bond` supports all of them by keeping the roster easy to join.

**The reviewer's policy is owned by x/rep's own params.** The seven fields
(`min_reviewer_bond`, `reviewer_demotion_threshold`, `min_reviewer_trust_level`,
`min_reviewer_rep_tier`, `min_reviewer_age_blocks`,
`reviewer_demotion_cooldown`, `reviewer_unbond_cooldown`) live in `Params` and
in `RepOperationalParams`, and `SyncReviewerBondedRoleConfig` writes them
through to the `BondedRoleConfig` for `ROLE_TYPE_INITIATIVE_REVIEWER` on
`InitGenesis`, on `MsgUpdateParams`, and on `MsgUpdateOperationalParams`. This
is the same shape x/forum uses for the sentinel and x/collect for the curator.
Before it, the reviewer was the one bonded role no module owned: its config was
whatever genesis seeded, reachable only by editing genesis or shipping an
upgrade. It is now council-tunable like every other role's.

**No verdict-streak knobs, by design (REP-R-106).** The projection writes the
streak-policy fields (`upheld_to_reset_overturns`, `overturn_base_cooldown`,
`overturn_cooldown_escalates`) as zero, which `RecordRoleOutcome` reads as the
moderation defaults (one upheld verdict clears an overturn streak; a kind that
sets an overturn cooldown uses `DefaultRoleOverturnCooldown`, not escalating): a
reviewer's approval or rejection is a contested judgment call, like a
sentinel's hide, not an assertion of fact like a federation verifier's, and rep
params carry no reviewer-specific streak knobs. Any value seeded in genesis is
overwritten by the next sync. A reviewer overturn cooldown would need new
params (and a `ReviewerBondPolicy` field each) before it can be set.

The write-through is a **straight projection** — no field is defaulted on the
way across. Reading a zero as "unset" would put the params and the enforced
config back out of step, which is the drift the write-through exists to close: a
council that votes `reviewer_unbond_cooldown: 0` gets 0, not a silently
restored 14 days. `ReviewerBondPolicy.Validate` is what makes that safe, so it
constrains every field, and it is stricter than the sentinel's in one place:
`min_reviewer_trust_level` may not be empty. `BondRole` skips the trust gate
entirely on an empty string, so an omitted level would quietly open the one role
whose approvals mint DREAM. An ungated roster is still expressible — as
`TRUST_LEVEL_NEW` — it just has to be said out loud.

Eligibility is the trust level alone; `min_reviewer_rep_tier` stays 0. The trust
ladder already encodes reputation, so a tier check is a second, stricter copy of
the same gate rather than an independent one — and because genesis seeds trust
levels but ships no reputation scores, a non-zero tier made the role
unqualifiable on a fresh chain.

##### Review fees are paid per round

Every review round that collects verdicts pays its reviewers once, whatever
the outcome:

- **A round a rejection sends back** (a reviewer's reject verdict, a committee
  rejection, or the escalation timeout) is paid in `rejectReviewRound` before
  `review_round` advances. The round's fee pool (`review_fee_rate` × tier
  multiplier × budget) is split evenly across its verdicts and minted under the
  same per-season initiative reward cap that gates completion. If minting it
  would pass that cap or the per-epoch mint ceiling, the round's fee is
  forfeited with an `initiative_review_fee_forfeited` event instead of blocking
  the rejection: a reviewer must always be able to reject.
- **The round that resolves the initiative** is paid on its terminal path:
  completion, close (including close on round exhaustion), the project-cancel
  cascade, or an upheld challenge.

Each paid verdict records its fee in `InitiativeReview.fee_paid`, and a verdict
with a fee recorded is never paid again, so every verdict is paid at most once
across all paths (a failed transaction rolls the record back with the mint).
Close, cancel cascade and upheld challenge return the initiative budget to the
project net of `ReviewFeesPaid` — the sum over every round — not just the final
round's fees. Fees emit `initiative_review_fee_paid` with the round.

A round discarded by `MsgUnassignInitiative` (verdicts filed, but the round
neither resolved nor rejected) is not paid: rounds advanced that way are not
bounded by `max_review_rounds`, so paying them would turn assignment churn into
a mint loop.

##### What shipped, and what did not

Both halves are built: the role and its `BondedRoleConfig`, per-verdict
bond scaled by `reviewer_bond_reserve_rate`, the completion gate, the
round/resubmit cycle, the committee escalation whose silence rejects the round
(bounded by `max_review_rounds`, clean close on exhaustion), the DREAM fee via
`review_fee_rate` x the tier multiplier paid for every round (see
[Review fees are paid per round](#review-fees-are-paid-per-round)), the accuracy wiring into `RoleActivity` from both challenge
directions, and the accuracy-gated SPARK pool below.

##### The SPARK pool

Paid on `reviewer_reward_epoch_blocks`, weighted by windowed accuracy against
the square root of decided verdicts, to reviewers at `NORMAL` status clearing
`min_reviewer_accuracy` (0.70) over `reviewer_accuracy_window_epochs`. Capped at
`max_reviewer_reward_pool`, with `reviewer_reward_pool_overflow_burn_ratio` of
the excess burned each epoch so an over-funded pool cannot become a standing
prize worth farming.

Every knob is separate from the sentinel equivalent, and the pools are separate
bank sub-addresses. That is the same reasoning that made this a distinct role
type: a wrong approval mints DREAM that cannot be clawed back, where a wrong
hide costs a post some visibility, so neither role may draw on the other's funds
or be tuned by the other's bar.

**A reviewer with no contested verdict in the window earns nothing here.**
Unchallenged work is not evidence of accuracy, and counting it would pay most
for reviewing whatever nobody bothers to challenge — the DREAM fee already
covers turning up.

##### Automatic funding for bonded-role pools

Bonded-role pay does not depend on anyone remembering to send SPARK. In
`BeginBlock`, x/rep takes **one** capped claim on the community pool and divides
it across every bonded-role reward pool, in the same way x/shield funds its gas
reserve.

Pay that arrives only when a committee schedules a transfer arrives
unpredictably, and unpredictable pay does not hold a roster. That matters most
for initiative reviewers, who became load-bearing for completion when the
staker veto was retired — a review gate nobody staffs is a stalled queue, and
routing the shortfall to committee arbitration converts a funding failure into
governance work.

**Which pools.** Content sentinel, initiative reviewer, collect curator and
federation verifier.

The verifier was excluded at first on the grounds that a flat DREAM mint per
epoch already paid the role, so a SPARK share would pay it twice. That
reasoning was wrong, for a reason specific to this role: the verifier is the
only bonded role whose work has an **off-chain cost**. It fetches a peer's
content, hashes it, and pays SPARK gas per submission, while the other three
act on on-chain state that is free to read. Paying it only in DREAM — which
cannot be sold and cannot buy gas — made verifying structurally SPARK-negative
for the holder, even as the bridge operator on the other side of the same
exchange is paid in SPARK. The DREAM stipend was never redundant with SPARK
pay; it does a different job (bond recovery in RECOVERY status), which is why
both now run from the same distribution rather than one replacing the other.

Its distribution moved into x/rep along with the pool, because the accuracy it
scores comes from `RoleActivity` and a distribution resets that record's
per-epoch counters — two modules distributing for one role on two
independently-editable cadences would both reset those counters and neither
would read a coherent window.

Caps set the relative share, since the division is headroom-proportional:
reviewer 150,000 SPARK against 100,000 each for sentinel, curator and
verifier. Reviewers are paid ~1.5x because a wrong approval mints DREAM that
cannot be clawed back, where a wrong hide or a wrong rating costs some
visibility. Sentinel and curator are equal because hiding a post and rating a
collection are comparable calls on comparable evidence. The verifier matches
them at an equal cap rather than on a bespoke sizing story: headroom-
proportional funding means an idle pool draws nothing, so an equal cap costs
the community pool nothing while the roster is small. Note that the verifier
pool drains on a **longer cadence** than the other three, so an equal cap is
not an equal per-epoch spend.

**One intake, divided internally.** The skim lands in `RoleRewardIntakeAddress()`
and is immediately placed into the per-role pools, so the community pool sees a
single auditable claim from this module regardless of how many roles exist. The
intake holds no balance between blocks; it is a conduit, not an account.

**The division is proportional to headroom** — each pool's `max(0, cap −
balance)` against the total. This needs no per-role funding parameter: a pool
already at its cap draws nothing, so an idle role costs the community pool
nothing, and adding a bonded role means adding it to `fundedRolePools` and no
new funding parameter anywhere — as the federation verifier's addition
demonstrated. The last pool in the division takes the
remainder, so integer truncation cannot strand dust in the intake.

**Bounds.** The draw per UTC day is capped by a **share of inflation** rather
than a fixed amount:

```
daily_allowance = annual_provisions * community_tax * role_reward_inflation_share / 365
```

Default share 0.5 — half the community pool's inflation income. A fixed nominal
draw takes its *largest* share of the pool exactly when the pool is *poorest*:
inflation floats between 2% and 5%, so on a 100M supply the pool takes in
822–2,055 SPARK/day, and a constant 1,000/day would be 49% of that at the top of
the range and 122% at the bottom. Since x/rep skims before x/split, at the bottom
the councils would get nothing. A share is counter-cyclical — it takes less when
there is less — so the councils' remainder is structural rather than residual.
It also tracks supply growth without anyone periodically retuning a number.

**The base is the inflation rate, not the community pool balance.** The balance
holds the 95M SPARK genesis allocation that x/split exists to hand to the
councils, plus any direct `fund-community-pool` deposit; neither is income, and
a share of the balance would raid both. `annual_provisions` also comes from
x/mint, whose authority is the burn address — so the funding rate is anchored to
a number no committee or proposal can move, which is a stronger guarantee than
the previous committee-editable absolute amount.

The draw is additionally bounded by total pool headroom and by the community
pool's actual balance — the latter because a draw larger than the pool would
fail `DistributeFromFeePool` and, in `BeginBlock`, take the block with it. The
day ledger persists in state, so the allowance bounds a day rather than a block
and cannot be reset by producing more blocks. Setting the share to zero disables
automatic funding entirely; a share above 1 is rejected by validation, since it
would mean intending to leave the councils nothing.

A funding failure is logged, never returned: it must not be able to halt the
chain. A failed draw also does not consume the day's allowance, so a transient
distribution error costs a block of funding rather than a day of it.

The placement step sweeps the intake's whole balance rather than only the block's
draw, so SPARK left behind by a placement that failed on an earlier block is
picked up on the next one instead of being stranded.

The day ledger is exported and re-imported with genesis. Without that, an
export/import round-trip hands the chain a fresh allowance, and the daily cap
would bound a day only until the next export.

**Ordering.** x/rep runs before x/split in `BeginBlockers`, alongside x/shield.
x/split distributes whatever remains in the community pool to the councils in
full, so a module that skims must skim first or find an empty pool.

##### The completion gate

Above `review_required_above_budget` (default 100 DREAM — the APPRENTICE
ceiling) an initiative cannot complete without at least one reviewer verdict,
whatever its parent project's policy says.

The gate keys on **how much the completion mints**, not on whether the project
is budget-backed. A permissionless initiative mints against a self-declared
number with no treasury behind it, capped only by tier, so the funded/unfunded
axis gets the risk ordering backwards: the mode with no council vouching for it
was the one with no mandatory review. Mint size is the figure that matters and
it exists on both paths. Apprentice work stays exempt because it is small and it
is the on-ramp where reviewer scarcity would hurt newcomers most.

`RequiredVerifiersFor` takes the **maximum** of two sources read differently.
The per-project policy comes from the initiative's snapshot, because the project
creator owns that policy and — for self-assigned work — is also the party the
gate constrains; read live, they could switch it off over work already
submitted. The chain-wide threshold is read **live**, because its setter is
governance or the Operations Committee rather than the constrained party, and
snapshotting it would mean a committee raising the threshold in response to a
farm in progress could not touch anything already submitted. Taking the max
means neither source can be used to weaken the other.

**Silence rejects the round rather than passing it.** An escalation that reaches
its deadline with no verdict and no committee decision used to resolve to
PASSED, which made the gate advisory for anyone patient enough: wait out the
reviewers, wait out the committee, mint. It now calls `rejectReviewRound` — the
assignee resubmits and gets another window, and when `max_review_rounds` is
exhausted the initiative is abandoned cleanly, budget returned and nothing
minted. Silence must never mint; it must also never wedge, which is why the
terminal state is abandonment rather than an indefinite hold.

**Reading the gate.** Three queries cover state that would otherwise be
write-only. `initiative-reviews [id]` returns **every** round's verdicts plus
`approvals`, `required` and `satisfied` for the current one — `satisfied` is
reported rather than left to the caller because `approvals >= required` is not
the whole rule: a committee escalation can settle the gate on its own.

> It takes no round selector on purpose. The house convention for an optional
> numeric filter is a plain field where zero means unset
> (`QueryPostsRequest.category_id`, `QuerySeasonStatsRequest.season`,
> `QueryShieldRequest.epoch`), which is safe in those domains because none of
> them has a valid zero. Review rounds number from 0, so the same convention
> would make the first round unaddressable — and the first round is exactly what
> someone wants to read after a bounce. `max_review_rounds` bounds the set at 3,
> so returning all of them costs nothing and keeps the convention intact rather
> than carving out an exception for one message. `review-bounty [id]`
returns the escrow and, per contribution, the height at which it matures and
whether it is reclaimable *now*; that flag folds in the committed check, so it
never tells a funder they can withdraw something the handler will refuse.
`escalated-reviews` lists the committee's queue — escalation lives in its own
set because `ReviewEscalation` is reset to `NONE` when a round escalates, so it
cannot be derived from the initiative and the committee would otherwise have no
way to find the decisions waiting on it.

**Initiatives that come under the gate late are adopted, not stranded.** An
initiative submitted while ungated has `ReviewDeadline == 0`. Once the threshold
moves under its budget it can no longer complete, and the escalation sweep would
have skipped it forever for want of a deadline — leaving it unable to pass,
bounce or abandon. The sweep therefore opens a review window for any gated
initiative with no deadline, so it gets a full window under the rules that now
apply rather than one that expired before anyone knew of it.

##### Review bounties

```protobuf
message ReviewBounty {
  uint64 initiative_id = 1;
  // Total DREAM currently escrowed and unpaid.
  string amount = 2;
  // Per-funder contributions, so a reclaim or an end-without-verdict refund
  // returns each funder's own DREAM rather than a pro-rata approximation.
  repeated ReviewBountyContribution contributions = 3;
  // True once any verdict has been filed. Reclaim is barred from that point.
  bool committed = 4;
}

message ReviewBountyContribution {
  string funder = 1;
  string amount = 2;
  int64 funded_at = 3;  // block height, for the reclaim delay
}
```


`MsgFundReviewBounty` escrows DREAM against one initiative to bid reviewer
attention toward it; `MsgReclaimReviewBounty` withdraws an unpaid contribution.
Anyone may fund and contributions are additive — the amount should express how
much the work matters to the people who want it checked, not what one person can
spare.

Payment is **per verdict filed**, split across the resolving round's reviewers,
exactly like the DREAM fee. A bounty released on successful completion would be
a bribe to approve with extra steps.

Two rules keep funding honest in the other direction. Reclaim requires
`review_bounty_reclaim_delay` blocks, so advertising a bounty and pulling it in
the same breath is not free. And reclaim is barred outright from the moment any
verdict is filed: reviewers commit bond and do the reading on the strength of
what was advertised, so a later withdrawal would waste their collateral. An
initiative that ends with no verdict refunds every contribution rather than
forfeiting it — funding must not be a gamble on someone else's behaviour.

Every terminal path settles the bounty: `CompleteInitiative`, `CloseInitiative`,
the upheld-challenge path (`REJECTED`) and the project-cancel cascade. The last
two once skipped it, which stranded committed bounties on their funders for
good; the cascade also skipped the review fees and never released the
reviewers' reserved bonds. It now settles the round exactly as
`CloseInitiative` does, returning the budget net of review fees.

The review **fee** follows the same rule on the upheld-challenge path: paid per
verdict filed, with the project's budget returned net of it. Withholding it
there made the fee contingent on nobody later challenging the work, and cost
the reviewers who rejected it — the ones the jury proved right — as much as the
approvers. The approvers are answered by the overturn slash (10% of budget per
verdict), which outweighs their share of a 5% fee pool.

DREAM lives on the member record rather than in bank, so the escrow is a lock on
the funder's own balance plus the claim recorded against it, the same shape as a
challenge stake. Payout draws the total down from the funders and reissues the
same amount to the reviewers (`reissueDREAM`): supply is unchanged, and it never
routes through the transfer tax, which exists to throttle peer-to-peer gifting
rather than to skim earned pay. The reissue counts in the season's minted total
(the draw counted as a burn) but, unlike `MintDREAM`, is outside the per-epoch
mint ceiling (a net-zero move must not fail a completion) and pays no referral
reward to the reviewer's inviter (REP-E-107). Restoring a slashed author bond
after a reversal (`RestoreAuthorBond`) reissues the same way.

**Gated permissionless initiatives must escrow a minimum** —
`permissionless_min_review_bounty_rate` (default 10%) of budget, in existing
DREAM, at creation.

The charge tracks the gate: it applies only when the budget exceeds
`review_required_above_budget`. The bounty pays for *mandatory* review, so
charging it where no review is required would take DREAM for a service that is
never delivered. Since the threshold equals the APPRENTICE ceiling, apprentice
work carries no bounty at all — which matters because that tier is the on-ramp,
reachable at `PROVISIONAL`, and members join holding **zero** DREAM
(`Member.DreamBalance` starts at zero on invitation accept). An unconditional
charge would have made a newcomer'"'"'s first initiative cost 11x its 1 DREAM
creation fee for a review that could never happen. Their review fee is minted like everything else about them,
so without this the reviewers of permissionless work are funded purely by
dilution — precisely the outcome the funded path's budget-netting exists to
prevent. A creator-funded bounty prices that attention onto whoever consumes it,
is non-inflationary because it moves DREAM that already exists, and scales the
spam brake with the amount being minted instead of leaving it at a flat creation
fee. It fails creation outright if the creator cannot cover it: the brake is
meant to bite when the work is commissioned, not to be discovered later.

##### What this retires

The **stake-weighted disapproval veto** is gone: `initiative_disapproval_threshold`
(both `Params` and `RepOperationalParams`), `Initiative.disapprovals`, and the
staker branch of `MsgApproveInitiative`, which now refuses a non-committee
disapproval outright with `ErrUnauthorized`. Approval remains advisory and
recorded; disapproval is committee-only.

Stakers still hold a real exit. Conviction is recomputed from **live** stake
records and completion requires both the total and external thresholds, so
withdrawing a stake genuinely blocks completion, with roughly one refresh
interval of lag. Voting with your feet is a functioning veto, not a gesture.

What makes retiring correct is not that the conflict disappears — unstaking
forfeits the completion bonus and seasonal staking rewards, so it costs the
staker just as disapproving did. It is that **the staker's opinion stops being
load-bearing**: conviction measures demand, reviewers measure quality, and each
electorate is now asked only the question it is competent to answer.

> Accept the consequence: because conflicted stakers rarely withdraw, conviction
> becomes effectively monotonic — it ratchets toward completion and seldom
> retreats. That is tolerable only while reviewers are genuinely the quality
> gate. Reviewer coverage and accuracy are therefore not refinements of this
> design; they are the thing holding it up.

#### Initiative Bounties

> Replaces the old `TRANSFER_PURPOSE_BOUNTY`, which was never more than an
> enum value: `TransferDREAM` gave it no case of its own, so a "bounty" transfer
> was an uncapped member-to-member send with only the 3% tax, bypassing every
> tip and gift limit. Keeper: [initiative_bounty.go](../x/rep/keeper/initiative_bounty.go).

An initiative bounty lets members put DREAM they already hold toward work they
want done, on top of the initiative's budget. It is escrowed against one
initiative and paid to the assignee only if the initiative completes. It is the
non-inflationary way to signal demand: the budget is minted, the bounty moves
DREAM that already exists.

It is distinct from a [review bounty](#review-bounties), which pays reviewers
per verdict filed and is never contingent on approval. An initiative bounty is
contingent on completion by design — that is what makes it pay for the work —
so everything below exists to keep it from becoming a DREAM transfer channel
wearing the costume of a work order.

##### The threat it has to survive

DREAM is not meant to be monetized. No chain rule can stop two members agreeing
off-chain to swap SPARK or cash for DREAM, so the defence is to keep DREAM off
every market venue (no IBC, no DEX) and to throttle every route by which DREAM
moves between members. Tips and gifts are throttled by per-transfer and
per-epoch caps. A completion-released bounty is another route, and the attack
on it is direct: a seller funds a bounty on a trivial initiative, the buyer is
assigned and "completes" it, the bounty releases.

What stands in the way is completion itself — conviction with at least 50%
from non-affiliated stakers, a clean challenge period, and mandatory review
above `review_required_above_budget`. The rules below make that gate the
binding constraint and bound how much any one pass through it can carry, so the
bounty is never a cheaper route to a chosen recipient than a tip.

##### State

```protobuf
message InitiativeBounty {
  uint64 initiative_id = 1;
  // Total DREAM currently escrowed and unpaid.
  string amount = 2;
  // Per-funder contributions, so refunds return each funder's own DREAM.
  repeated InitiativeBountyContribution contributions = 3;
}

message InitiativeBountyContribution {
  string funder = 1;
  string amount = 2;
  int64 funded_at = 3;  // block height, for the reclaim delay
}
```

Escrow is a lock on the funder's own balance (`LockDREAM`) plus the claim
recorded here — the same shape as a review bounty or a challenge stake. Locked
DREAM does not decay and cannot be spent.

Per-funder epoch spend lives on `Member`
(`initiative_bounty_funded_this_epoch` / `last_initiative_bounty_epoch`) and
resets on the first funding of a new epoch.

##### Messages

```protobuf
message MsgFundInitiativeBounty {
  option (cosmos.msg.v1.signer) = "funder";
  option (amino.name) = "sparkdream/x/rep/MsgFundInitiativeBounty";
  string funder = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 initiative_id = 2;
  string amount = 3;
}

message MsgReclaimInitiativeBounty {
  option (cosmos.msg.v1.signer) = "funder";
  option (amino.name) = "sparkdream/x/rep/MsgReclaimInitiativeBounty";
  string funder = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 initiative_id = 2;
}
```

Neither is in x/session's `allowed_msg_types`, so a session key cannot fund
or reclaim a bounty unless governance adds them; nothing about funding a bounty
needs one. Query: `initiative-bounty [initiative-id]` returns the escrow and each
contribution's reclaim state.

##### Funding rules

`MsgFundInitiativeBounty` fails unless all of these hold:

1. **The initiative is `OPEN` or `ASSIGNED`.** Funding after work is submitted
   is paying for a known deliverable to a known person — a directed transfer,
   not a bid for work.
2. **The funder is not affiliated.** Not the assignee, apprentice, initiative
   creator or project creator (`InitiativeAffiliates`). The converse is
   enforced too: `MsgAssignInitiative` refuses an assignee who holds a live
   contribution to that initiative's bounty, so a funder cannot fund and then
   take the work.
3. **Total bounty ≤ `initiative_bounty_max_budget_ratio` × budget** (default
   1.0). The budget is already bounded by tier, trust level and review, so the
   bounty cannot outweigh the work backing it. This is the rule that bounds
   laundering volume: every DREAM moved must ride on an initiative that real,
   non-affiliated conviction pushed over the line, at no more than 1:1 with
   what that initiative was worth.
4. **The funder's spend this epoch plus `amount` ≤
   `max_initiative_bounty_per_funder_epoch`.** The sender-side counterpart to
   the recipient limits in [DREAM Transfer Limits](#dream-transfer-limits).
5. **`amount` ≥ `min_initiative_bounty_contribution`** and the bounty has fewer
   than `max_initiative_bounty_contributions` entries. Bounds the state and the
   payout loop.
6. **The funder can lock `amount`** of unlocked DREAM.

##### Reclaim

`MsgReclaimInitiativeBounty` returns the funder's own matured contributions
(older than `initiative_bounty_reclaim_delay` blocks) **only while the
initiative is `OPEN`**. Once someone is assigned they are working on the
strength of the advertised bounty, and pulling it is a bait-and-switch — the
same reasoning that commits a review bounty once a verdict is filed.

If the assignment is released (`MsgUnassignInitiative`, or the stall path) and
the initiative returns to `OPEN`, reclaim becomes available again. The bounty
otherwise stays escrowed and carries over to the next assignee.

##### Settlement

| Terminal status | Effect |
|---|---|
| `COMPLETED` | Paid to the assignee, net of the transfer tax |
| `CLOSED` (including the project-cancel cascade) | Refunded to each funder in full, no tax |
| `REJECTED` (challenge upheld) | Refunded to each funder in full, no tax |

On completion, settlement draws the total down off the funders' locked
balances, burns `transfer_tax_rate` of it (tracked as a burn), and credits the
remainder to the assignee. Supply falls by exactly the tax, which is charged to
the funders' `lifetime_burned` in proportion to what each paid in — as
`TransferDREAM` charges it to the sender — so the member ledger and
`SeasonBurned` move together. It is a balance
move, not burn-and-mint like the review bounty: the DREAM already exists, so it
must not count against the epoch mint cap (which could otherwise fail a
completion) or the season's minted total. The tax applies
here and not to review bounties because this is a member choosing to move DREAM
to another member — precisely what the tax exists to throttle — whereas a
review bounty pays for a service rendered to the network.

The payout counts toward the assignee's `max_transfer_received_per_epoch` and
`max_transfer_received_per_season` (see
[DREAM Transfer Limits](#dream-transfer-limits)). Whatever would exceed them is
refunded to the funders pro rata, untaxed; the initiative still completes.

The payout is **not** initiative reward: it does not count toward
`max_initiative_rewards_per_season`, the treasury share, the completion bonus,
reputation, or conviction. Buying a bounty buys DREAM movement, never standing.

**Zeroing drops a funder's contributions.** `ZeroMember` burns the member's
whole balance, the DREAM locked behind a bounty contribution included, so it
removes their contributions from every initiative bounty rather than leaving
claims no settlement can unlock. The remaining funders' escrow is untouched.

Every terminal path settles the bounty. The hook lives beside the review
bounty's in `CompleteInitiative`, `CloseInitiative`, the upheld-challenge path
in `challenge.go`, and the project-cancel cascade
(`terminateInitiativeForProjectCancel`).

##### Parameters

| Param | Default | Purpose |
|---|---|---|
| `initiative_bounty_max_budget_ratio` | 1.0 | Cap on total bounty relative to budget |
| `max_initiative_bounty_per_funder_epoch` | 100 DREAM | Per-funder epoch cap, sized to the recipient epoch limit |
| `min_initiative_bounty_contribution` | 1 DREAM | Dust floor |
| `max_initiative_bounty_contributions` | 20 | Bounds state and payout loop |
| `initiative_bounty_reclaim_delay` | 14400 blocks | Mirrors `review_bounty_reclaim_delay` |

All operational (Operations Committee editable). A ratio of zero disables
funding.

##### Open question: vesting the payout

Paying the bounty as locked DREAM that unlocks linearly over N epochs would stop
a buyer from immediately spending or re-routing what they received, at the cost
of a vesting record per payout. Not in the first cut; revisit if bounty volume
turns out to concentrate on a few recipients.

#### Jury verdicts are final

A jury verdict on a challenge or an appeal **cannot be overturned**. There is no
appeal against a jury.

This is a design decision, not a missing feature. The jury is the terminal
fact-finder: any appellate body would itself need to be measured, and whatever
measured *it* would need measuring in turn. Finality is what stops the regress,
and it is what lets every other accountable role in the module — sentinels,
curators, verifiers, and reviewers above — treat a jury outcome as ground truth
for their accuracy records.

**Committee arbitration is not an exception.** When a jury fails to reach
quorum, the Operations Committee arbitrates — but that substitutes for a jury
that never formed rather than reviewing one that ruled. Slower, and eventual,
but never a second opinion on a delivered verdict.

**The consequence for jurors: reliability is measurable, accuracy is not.**
Without an overturn signal there is no way to score whether a juror judged
*well*, and none should be invented — the available proxies all punish
principled dissent and reward voting with the herd. Jurors are therefore held to
whether they **show up**: seatings, votes, declines and abandoned seats, feeding
`JurorResponsivenessWeight` and the abandoned-seat reputation penalty documented
above. That is the whole of juror accountability by design, and it is already
built.


#### Completing an Initiative

Payout is the one irreversible step in the initiative lifecycle: it mints to the
completer and the treasury, settles every staker, and deletes the stake records.
`MsgCompleteInitiative` is therefore gated on the contest window having actually
elapsed, not merely on the conviction thresholds being met:

- **Authority**: the initiative's assignee, or the Operations Committee.
- **Preconditions**: status is `IN_REVIEW`, the challenge window has actually
  been opened (`challenge_period_end > 0`) and the current block height is at
  or past it, the parent project is not `CANCELLED`, and
  `CanCompleteInitiative` passes (both conviction gates, no open challenges).
- **Errors**: `ErrInvalidInitiativeStatus` (1402) before the initiative reaches
  review, `ErrChallengePeriodActive` (1704) while the window is still open —
  including the `challenge_period_end == 0` case, an `IN_REVIEW` initiative
  whose window was never set (the pre-snapshot rejected-challenge shape; the
  EndBlocker's completion sweep heals it by opening a window). Neither error is
  permanent — the same call succeeds once the window opens and closes.

`SUBMITTED` was accepted here previously, which let an assignee skip the
challenge period entirely: submit, wait for the EndBlocker to observe the
conviction thresholds met, then call `MsgCompleteInitiative` before a single
block of the window had run. Because the EndBlocker finalises unchallenged
initiatives on its own at `challenge_period_end`, the manual message is now a
retry path rather than the normal route to payout — it matters when an
EndBlocker completion failed for a recoverable reason (the season mint cap in
`ErrInitiativeRewardCapReached`, say) and the call is worth repeating later.

#### Releasing an assignment

`MsgUnassignInitiative` releases an assignment and returns the initiative to
`OPEN` so somebody else can pick it up. It is **not** a terminal exit, and that
is the whole point: conviction, its stakes and the funding all stay attached to
the initiative, which keeps accruing conviction because `OPEN` is an active
status. The demand the community staked on the work is a property of the work,
not of whoever happened to be holding it, and destroying it on a change of hands
would make stakers pay for someone else stepping down.

- **Authority**: the assignee stepping down, or the Operations Committee freeing
  work that has stalled. Deliberately **not** the project creator, unlike
  `MsgAssignInitiative` — the creator is an interested party and pulling an
  assignment back would be a rug-pull on work in flight. Their lever is
  `MsgCloseInitiative`.
- **Status rules**:
  - `ASSIGNED` is always releasable. The current round holds no verdicts there,
    so nothing is owed to a reviewer and nothing is minted.
  - `SUBMITTED` and `IN_REVIEW` are Operations-Committee-only. Verdicts can be
    filed in either state, so a self-service release here would let an assignee
    submit, draw reviewer effort, release, and resubmit on a fresh round —
    minting review fees each lap at no cost to anyone but the token supply.
  - `CHALLENGED` is never releasable, by anyone. Walking away from a live
    challenge and re-entering through a new assignee would launder it.

  The two refusals carry **different registered errors**, and the distinction is
  load-bearing for clients. A status only the committee can release from returns
  `ErrUnauthorized` (1304) — the identical call from another signer succeeds, so
  the caller is being told to ask someone else. A status nobody can release from
  returns `ErrInvalidInitiativeStatus` (1402) — no signer can do it, so the
  caller is being told to wait. Releasing an initiative that has no assignee is
  also 1402.
- **Effects**: clears everything tied to the holder (`assignee`, `apprentice`,
  `assigned_at`, `deliverable_uri`, `submitted_at`, `approvals`,
  `required_verifiers`, the review and challenge deadlines) and releases the
  self-assign bond and every review bond still committed against a verdict. The
  budget stays allocated. Any review bounty stays funded and in escrow, since
  the initiative it was posted against is still live.
- **The review round counter is never reset.** Verdict records are keyed
  `(initiative, round, reviewer)` and outlive the assignment, so rewinding to
  round 0 would hand the next submission the previous holder's verdicts: stale
  approvals would satisfy the review gate for work nobody looked at,
  `PayReviewFees` would mint to their authors a second time, and those authors
  would be locked out of filing a real verdict. The counter instead advances
  past a round that actually collected verdicts, so the next submission starts
  on a clean key range. A round with no verdicts is already clean and is left
  alone — self-assigning and releasing costs nothing but gas, so consuming a
  round there would let anyone burn an initiative's `max_review_rounds` budget
  from the outside.

#### Closing an Initiative

`MsgCloseInitiative` retires an initiative and returns its budget to the parent
project. This is the project side deciding the work is not going to happen, and
it is terminal.

- **Authority**: the parent project's creator, or the Operations Committee.
  Never the assignee — their exit is `MsgUnassignInitiative`.
- **Precondition**: any live status, assigned or not. A project must be able to
  stop funding work whose assignee has gone silent, without needing that
  assignee's cooperation. `CHALLENGED` is the one exception — the challenge
  decides whether the work was delivered, and closing out from under it would
  let the project side void a challenge that was about to be upheld. Both that
  refusal and closing an already-terminal initiative return
  `ErrInvalidInitiativeStatus` (1402); an unauthorized signer gets
  `ErrUnauthorized` (1304) from the message server before the keeper is reached.
- **Effects**: settles any live review round (bounty paid, review fees paid,
  review bonds released), returns the reserved budget net of what review cost
  (skipped for permissionless projects, which allocate no budget up front),
  releases the self-assign bond, drops any review escalation entry, releases the
  initiative's conviction stakes (`releaseInitiativeStakes`, before the flip),
  moves the initiative to `CLOSED`, and emits `initiative_closed`.
- **Reviewers are paid whether the initiative completed or closed.** A fee that
  depended on the outcome would rebuild the bias the role exists to remove.
- **Stakes**: `releaseInitiativeStakes` pays the accrued rewards, returns the
  principal and deletes the records, before the status flips — `stakeAccruing`
  stops paying on a terminal initiative, so settling afterwards would strand
  everything earned. Closing is a retirement, not a confiscation: nothing here
  is entitled to the staked DREAM. A stake whose unlock fails is left in place
  with its reward debt rebased, and `RemoveStake` (which has no status gate)
  remains the recovery path. The terminal status also drops the initiative out
  of `IterateActiveInitiatives` so its conviction stops being recomputed.

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

**Budget-backed path**:
1. Validate the caller is the parent project's **creator** or on the Operations
   Committee (`ErrUnauthorized`)
2. Validate parent project is ACTIVE
3. Validate tier budget limits
4. Allocate budget from project's approved budget
5. Create initiative

**Standing on budget-backed projects.** Step 1 is the only asymmetry between the
two paths' authorization, and it exists because `AllocateBudget` validates only
that a project is ACTIVE and has budget remaining — it has no creator guard. Any
member could therefore commission work under somebody else's budget-backed
project and draw down its council-approved ceiling, then self-assign it and be
paid from a pool they were never granted. Permissionless projects stay open to
any member meeting the trust-level and tier gates, because there is no approved
pool to consume — the DREAM is minted against conviction or not at all.

Three placement details that mattered:

- **The gate lives in the message server, not the keeper**, which is where
  authorization lives throughout this module and keeps the keeper callable as
  trusted internal API.
- **It runs *after* the membership check**, so a non-member still gets a
  membership error rather than an authorization one.
- It was chosen over adding an "open to contributions" flag on `Project`
  because it needs no proto field and no genesis value, and it matches the
  standing model already used by cancel-project, close-initiative and
  assign-initiative.

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
  // Required when the interim is an ADJUDICATION: the committee's verdict on
  // the challenge the interim was raised to settle. Ignored for every other
  // interim type.
  AdjudicationDecision decision = 4;
}
```

**Interim authorization and the emission ceiling.** Interims are the module's
self-service paid-work path, and all three of their authorization gates were
missing — together they composed into an unbounded DREAM self-mint:

| Message | Gate | What it was before |
|---|---|---|
| `MsgCreateInterim` | ACTIVE member; not ADJUDICATION or JURY_DUTY; deadline after the current height | none at all — any address could commission a complexity-derived budget for itself, since `CreateInterimWork` makes the creator the sole assignee |
| `MsgAssignInterim` | Operations Committee | none at all — anyone could add themselves to anyone's PENDING interim and collect on completion |
| `MsgCompleteInterim` | ADJUDICATION only (Commons Operations Committee policy, explicit decision); interim must not be finalized | none — `CompleteInterimDirectly` paid on *every* call, so re-sending the same message minted the budget again indefinitely; and until the 2026-10 fix pass any assignee could complete (and be paid for) its own interim |
| `MsgApproveInterim` | Commons Operations Committee policy (or council / gov); interim must not be an `ADJUDICATION` | none — a bool cannot carry a verdict, so approving or rejecting an adjudication finalized it undecided and stranded the challenge it was raised to settle |

Beyond authorization, the path needed a ceiling. An interim was self-assigned by
its creator and self-completed by its assignee, so
`max_active_interims_per_member` bounds only how many are open at once, not how
many a member can complete and re-create in a season. That left the interim path
as the one DREAM-creating flow limited solely by `max_dream_mint_per_epoch` —
250,000 DREAM/day against a 25,000 DREAM genesis supply, with no seasonal bound
at all while initiatives had `max_initiative_rewards_per_season`.

`max_interim_rewards_per_season` (default 50,000 DREAM) closes that.
`chargeInterimRewardCap` projects an interim's whole payout and books it
*before* any minting, so the cap can refuse a payout but never be overrun by
one — the same conservative direction `CompleteInitiative`'s season gate errs
in. Both payout paths charge it: committee approval authorizes the work, it does
not exempt it from the ceiling. An unset or zero cap closes the path rather than
uncapping it, since a param that has never been written is not a decision to
mint freely. Counter: `SeasonInterimRewardsMinted` (`econ/season_interim_rewards`),
reset with the other per-season counters in `InitSeasonalPool`. Regressions:
`TestInterim_CompletingTwiceDoesNotPayTwice`,
`TestInterim_SeasonCapBoundsTotalEmission`.

### Stake Messages

**Stake limits.** A member holds at most `MaxStakeRecordsPerMember` (500)
stake records across every target (`ErrTooManyStakes`, 2409): reward-bearing
records are decayed and rewritten every epoch, so at the `min_stake_amount`
floor an uncapped count bought tens of thousands of per-epoch writes for a
refundable 100 DREAM. MEMBER and TAG stakes now get the same
`MaxStakeTranchesPerTarget` (10) as the other types, counted per target member
or tag (they share target id 0, so the per-id count never applied). A TAG
stake must name a registered tag. A content stake requires the content to
exist (`ErrContentNotFound`, 2408): a failed author lookup used to let the
stake through, so a member could stake on the next sequential id and then
post the content, holding conviction on their own item. The by-staker index
`StakesByStaker` (`(staker as stored, stake id)`, maintained with
`StakesByTarget`, derived at InitGenesis) serves the record cap, the circular
member-stake check and zeroing, which used to walk every stake on the chain.

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

// MsgInviteExpertWitness lets a juror seated on a pending initiative jury
// review call an expert witness. The expert must be an active ESTABLISHED+
// member with reputation in one of the initiative's tags and no stake in the
// dispute (not the challenger, assignee, initiative creator or a juror, nor an
// invitation neighbour of any of them). The invitation opens an
// EXPERT_TESTIMONY interim, bound to it by id, that the review's verdict pays
// if the expert testified and expires otherwise. At most
// MaxExpertWitnessesPerReview experts per review.
message MsgInviteExpertWitness {
  option (cosmos.msg.v1.signer) = "juror";
  option (amino.name) = "sparkdream/x/rep/MsgInviteExpertWitness";
  string juror = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 jury_review_id = 2;
  string expert = 3 [(cosmos_proto.scalar) = "cosmos.AddressString"];
}

// MsgInviteExpertWitnessResponse returns the EXPERT_TESTIMONY interim opened
// for the expert.
message MsgInviteExpertWitnessResponse {
  uint64 interim_id = 1;
}

// MsgEndorseExpertWitness endorses a pending expert invitation. The signer
// must be a juror seated on the review who has answered the summons
// (accepted jury duty or already voted) and is not the inviting juror; an
// invitation is endorsed once. Until endorsed the expert cannot testify and
// the invitation is never paid.
message MsgEndorseExpertWitness {
  option (cosmos.msg.v1.signer) = "juror";
  option (amino.name) = "sparkdream/x/rep/MsgEndorseExpertWitness";
  string juror = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 jury_review_id = 2;
  string expert = 3 [(cosmos_proto.scalar) = "cosmos.AddressString"];
}

message MsgEndorseExpertWitnessResponse {}

// An expert listed on the review's expert_witnesses (seated by
// MsgInviteExpertWitness and endorsed by a second juror through
// MsgEndorseExpertWitness) testifies once, while the review is still PENDING.
// The opinion is 64..4096 bytes, the reasoning at most 4096. Testifying pays
// nothing by itself: the verdict pays the expert's EXPERT_TESTIMONY interim.
message MsgSubmitExpertTestimony {
  option (cosmos.msg.v1.signer) = "expert";
  string expert = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 jury_review_id = 2;
  string opinion = 3;
  string reasoning = 4;
}
```

#### Anonymous Challenge Flow (via x/shield)

> **Not live.** The x/shield op table registers `MsgCreateChallenge` **inactive**: x/rep refuses a challenge whose creator is the shield module until it can derive the challenger from the member's identity (see the note on the Challenge proto and `TestShieldedChallengePathFailsClosed`). Governance activates the registration once that exists. The flow below is the target design.

Anonymous challenges are not submitted directly to x/rep with per-module ZK proof verification. Instead, they go through x/shield's unified privacy layer:

1. **Submission**: The challenger submits `MsgShieldedExec` to x/shield, wrapping a standard `MsgCreateChallenge`. x/shield handles ZK proof verification (Groth16 over BN254), nullifier checking (domain 41, GLOBAL scope), and module-paid gas.
2. **Proof verification**: x/shield verifies the ZK proof against the trust tree root maintained by x/rep (`GetTrustTreeRoot()`). The proof demonstrates membership and sufficient trust level without revealing the challenger's identity.
3. **Execution**: x/shield unwraps and dispatches the inner `MsgCreateChallenge` to x/rep's message server. The `challenger` field is set to x/shield's module address (not the real challenger).
4. **Batch mode**: Anonymous challenges use ENCRYPTED_ONLY batch mode -- the inner message is TLE-encrypted and only decrypted/executed after epoch key revelation, providing maximum privacy.

x/rep's `IsShieldCompatible()` method (in `shield_aware.go`) identifies `MsgCreateChallenge` as eligible for shielded execution.

### Content Challenge Messages

**Content jury reviews have their own timeout path.** They are deliberately
excluded from the shared PENDING verdict index that
`ResolveExpiredChallengeJuryReviews` sweeps, because that sweep applies
initiative-challenge semantics and resolved an in-review content challenge out
from under its callers. Nothing replaced it, so a content jury that never voted
— or missed the supermajority at its deadline — left the review PENDING with no
sweep able to reach it: the challenger's stake, the author's bond, and the
target's one-challenge-at-a-time slot were locked **permanently**, and the juror
seats never vacated.

`SweepExpiredContentJuryReviews` (EndBlocker step 6a) is the replacement. It
walks the *content-challenge* status index rather than the verdict index, so it
cannot interfere with the initiative sweep, and resolves anything past its
review deadline through `ResolveInconclusiveContentChallenge` — which preserves
the status quo and returns the challenger's stake without penalty, exactly as a
vote-driven inconclusive tally does. Capped at 50 per block and
collect-then-resolve, since resolution mutates the index being walked.

`UpholdContentChallenge`, `RejectContentChallenge` and
`ResolveInconclusiveContentChallenge` also gained the terminal-status guard
their initiative-side twins always had. Every branch of all three moves DREAM
(burn, refund, or slash-and-reward), so a second call paid or burned a second
time — and the new sweep gives them one more caller.

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

### Tag Registry Messages

> **Note:** Tag creation is an explicit, trust-gated, fee-burning action (`MsgCreateTag`). Trust floor is `TRUST_LEVEL_ESTABLISHED`.

```protobuf
// Permissionless tag creation, gated on TRUST_LEVEL_ESTABLISHED.
// Deducts `params.tag_creation_fee` DREAM from the creator.
message MsgCreateTag {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string name    = 2;  // validated via ValidateTagFormat + ValidateTagLength
}
message MsgCreateTagResponse { string name = 1; }
```

**Limits.** The registry holds `DefaultMaxTotalTags` (10,000) tags, counted by
the `TagCount` item (`tag/count`) that `SetTag`/`RemoveTag` maintain, so the cap
check reads one value instead of walking the registry. Members may fill only
90% of it: the last `CouncilTagHeadroomBps` (10%) is reserved for council and
committee policy addresses and the gov authority, which create tags without the
member gates and without a fee. A member must be ACTIVE and ESTABLISHED and may
create at most `MaxTagCreationsPerEpoch` (5) tags per epoch (`TagCreationWindow`,
keyed by canonical address; `ErrTagCreationRateLimited`, 2407). The default
`tag_creation_fee` is 10 DREAM. Before this, 10,000 tags at the old 100
micro-DREAM fee cost about 1 DREAM and blocked tag creation chain-wide for 30
days.

**Fee destination.** 100% of the tag-creation fee is burned via `k.BurnDREAM(ctx, creatorAddr, params.TagCreationFee)` in `msg_server_create_tag.go`. Burn is the only viable destination: DREAM is an internal, non-transferable reputation-backed token with no bank-module community-pool flow, so splitting the fee to the community pool isn't a design option. The burn contributes to DREAM scarcity in the same way other anti-spam DREAM burns do (unstaked/staked decay, failed-challenge slashing, transfer tax).

### Tag Moderation Messages

```protobuf
message MsgReportTag {
  option (cosmos.msg.v1.signer) = "creator";
  string creator  = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string tag_name = 2;
  string reason   = 3;
}
message MsgReportTagResponse {}

// Authority-only. `action` is 0 ignore (reporters' bonds forfeited), 1 remove,
// 2 reserve, 3 restore (release a reserved tag back to public use; refused
// for a tag that is not reserved). Actions 1-3 refund the reporters' bonds.
// For action 2, reserve_authority is empty or a bech32 address (a council
// policy, the gov account, a member); anything else is refused before the
// report is touched (ErrInvalidTagReportAction). MsgReportTag accepts a tag
// that has a Tag row OR a reservation, so a reserved tag without a row (seeded
// at genesis, or reserved after its row expired) can be reported and then
// restored (REP-R-203). A TagReport is under_review from the first report
// until it is resolved; the flag is constant while the record exists and is
// only echoed by the query.
// On REMOVE, the handler calls ForumKeeper.PruneTagReferences, which queues
// the tag; the forum EndBlocker strips it from posts in capped batches.
message MsgResolveTagReport {
  option (cosmos.msg.v1.signer) = "creator";
  string creator                 = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string tag_name                = 2;
  uint64 action                  = 3;
  string reserve_authority       = 4;  // for action=RESERVE
  bool   reserve_members_can_use = 5;  // for action=RESERVE
}
message MsgResolveTagReportResponse {}
```

**Report bonds.** Each reporter locks `DefaultTagReportBond` (1 DREAM, a
compiled constant). Resolution `remove` (1) and `reserve` (2) refund the bonds;
`dismiss` (0) burns them, so an unfounded report costs its bond. A refund or
burn that fails (the reporter was zeroed) emits
`tag_report_bond_settlement_failed` instead of vanishing. `ExpireTags` does not
collect a tag with an open report, and a report whose tag is already gone still
resolves (remove is then a no-op), so reporter bonds are never stranded.
Reporting requires an ACTIVE member.

### Tag Budget Messages

All five messages (`MsgCreateTagBudget`, `MsgTopUpTagBudget`, `MsgAwardFromTagBudget`, `MsgToggleTagBudget`, `MsgWithdrawTagBudget`) live in `proto/sparkdream/rep/v1/tx.proto`. `MsgAwardFromTagBudget` delegates post lookup to x/forum via `ForumKeeper.GetPostAuthor` and `ForumKeeper.GetPostTags`; the payout handler verifies the post carries the budget's tag before awarding. A group member cannot award to a post they wrote (`ErrSelfAward`, 2406, compared canonically): one member acting alone could otherwise take `1 - 0.75^k` of the pool with k posts of their own, the 25% per-call cap and the per-post cooldown notwithstanding. A members-only budget requires an ACTIVE author. The per-(budget, post) cooldown (`TagBudgetAwardByPost`) is exported with its heights (`tag_budget_award_cooldown_list`); an award the genesis gives no entry restarts its cooldown at the import height. On top of the per-call cap, one group member may award at most `tag_budget_awarder_epoch_cap_bps` (default 2500 = 25%, `0` = the default, at most 10000) of the pool as it stood before their own awards in the current epoch, summed over every post they award from that budget (`ErrTagBudgetAwarderCap`, 2413). The running total lives in `TagBudgetAwarderEpoch (budget_id, canonical awarder, epoch)`, one row per (budget, awarder): an award in a later epoch drops the earlier row. It is exported at genesis (`tag_budget_awarder_epoch_list`), so an import keeps every allowance. `MsgToggleTagBudget` refuses to resume a paused budget while the same group has another active budget for that tag (`ErrTagBudgetAlreadyExists`), the rule `MsgCreateTagBudget` applies.

### Bonded-Role Messages

`MsgBondRole` / `MsgUnbondRole` / `MsgCancelUnbondRole` are the generic bonding entry points for every DREAM-bonded role (forum sentinels, collect curators, federation verifiers, future roles). The `role_type` field selects the role; per-role eligibility is enforced against the corresponding `BondedRoleConfig`. Bond modifications are incremental and reversible (see "Queued unbond + cooldown").

```protobuf
message MsgBondRole {
  option (cosmos.msg.v1.signer) = "creator";
  string   creator   = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  RoleType role_type = 2;
  string   amount    = 3;
}
message MsgBondRoleResponse {}

message MsgUnbondRole {
  option (cosmos.msg.v1.signer) = "creator";
  string   creator   = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  RoleType role_type = 2;
  string   amount    = 3;
}
message MsgUnbondRoleResponse {}

message MsgCancelUnbondRole {
  option (cosmos.msg.v1.signer) = "creator";
  string   creator   = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  RoleType role_type = 2;
  string   amount    = 3;  // amount of pending_unbond_amount to cancel
}
message MsgCancelUnbondRoleResponse {}
```

#### Queued unbond + cooldown

When the role's `BondedRoleConfig.UnbondCooldown` is positive (the default for all three current roles), `MsgUnbondRole` does **not** release DREAM immediately. Instead it:

1. Sets `BondedRole.PendingUnbondAmount` to the requested amount and `BondedRole.UnbondCompletionTime = block_time + UnbondCooldown`.
2. Flips `BondedRole.BondStatus` to `BONDED_ROLE_STATUS_UNBONDING`.
3. Leaves `BondedRole.CurrentBond` unchanged — DREAM stays locked on the member and remains slashable through the cooldown window.

While `UNBONDING`:

- Owning modules gate role authority on a bond-**quantity** rule, not a blanket refusal. The holder may keep acting as long as its *staying* bond (`current_bond - pending_unbond_amount`) covers the role's floor; only the portion being withdrawn is treated as already gone. x/forum's `MsgHidePost` / `MsgLockThread` / `MsgMoveThread` / `MsgPinReply` / `MsgDismissFlags` route through a shared `eligibleSentinel` helper that returns `ErrSentinelUnbonding` only when the staying bond drops below `min_sentinel_bond`. The liability tail is closed not by the status flag but by `ReserveBond` being pending-aware (below): a slash reserved during an unbond draws only on staying, uncommitted bond, so any action taken mid-unbond stays fully backed (and slashable) through its whole appeal window. (`MsgRateCollection` in x/collect and federation verifier actions apply the same staying-bond logic via the same primitives.)
- A second `MsgUnbondRole` while already `UNBONDING` is **allowed and incremental**: it adds to `PendingUnbondAmount` (bounded by `CurrentBond - TotalCommittedBond - PendingUnbondAmount`, so the pending total can never exceed the staying bond) and resets the single `UnbondCompletionTime` to `block_time + UnbondCooldown`. This lets a holder correct or grow a withdrawal without first waiting out the cooldown. Resetting the one clock only ever lengthens the lock (never shortens it), so the liability tail is preserved.
- `MsgBondRole` top-ups are **allowed** during `UNBONDING`. A bond only adds slashable collateral, so it increases `CurrentBond` immediately while leaving the queued withdrawal (`PendingUnbondAmount` / `UnbondCompletionTime`) untouched; status stays `UNBONDING` (it is deliberately not recomputed, since flipping to NORMAL would orphan the pending unbond). Holders can bond / unbond / rebond incrementally without waiting out the cooldown for each change.
- `MsgCancelUnbondRole` **cancels** part or all of the in-flight unbond: it reduces `PendingUnbondAmount` by `amount` (≤ the pending total). No DREAM moves — a queued unbond never unlocked any, so cancelling simply removes the earmark. Cancelling the entire pending amount clears `UnbondCompletionTime` and recomputes `BondStatus` from the unchanged `CurrentBond` (the role returns to `NORMAL` / `RECOVERY`); a partial cancel leaves the role `UNBONDING` with the remainder on its existing clock (shrinking a withdrawal never needs a longer tail). This gives holders bidirectional, cooldown-free control over an in-flight withdrawal. Rejected with `ErrInvalidRequest` if no unbond is in flight or `amount` exceeds the pending total.
- `SlashBond` continues to operate on `CurrentBond` and caps `PendingUnbondAmount` at the new `CurrentBond`. Status stays `UNBONDING` through slashes — only `MatureUnbonds` flips it.

The EndBlocker calls `MatureUnbonds` every block. For any record whose `UnbondCompletionTime <= block_time`:

1. Unlock the matured DREAM on the member, **capped at `CurrentBond - TotalCommittedBond`** so maturity never releases bond earmarked to back an outstanding action (whatever survived mid-cooldown slashes). With pending-aware `ReserveBond`/`GetAvailableBond` the invariant `CurrentBond ≥ TotalCommittedBond + PendingUnbondAmount` holds by construction, so the release equals the full pending in practice; the cap is the regression guard that keeps rep and forum decoupled regardless of EndBlocker ordering.
2. Reduce `CurrentBond` by the released amount, zero `PendingUnbondAmount` and `UnbondCompletionTime`. If committed bond blocked part of the withdrawal, the remainder stays `PendingUnbondAmount` and the role stays `UNBONDING` for a later block to retry once the reservation releases or is slashed.
3. Recompute status from the new `CurrentBond` against the role's thresholds (the same mapping as `BondRole` / immediate-unlock): `NORMAL` if `≥ MinBond`, `RECOVERY` if in `[DemotionThreshold, MinBond)`, `DEMOTED` if below. Only when the matured unbond actually drops the holder into `DEMOTED` do we set `DemotionCooldownUntil = block_time + DemotionCooldown`. **Partial unbonds that keep the bond at or above `MinBond` stay `NORMAL`** — the holder reduced their stake but did not exit the role.

When `UnbondCooldown == 0` the handler falls back to the legacy immediate-unlock path: DREAM is released in the same transaction and status is recomputed from the new bond. Tests opt into this for the rare cases that want to exercise the synchronous flow.

**A cancelled unbond cannot launder away a demotion.** `computeBondStatus`
derives status from bond size alone, which made the demotion punishment
escapable in a single round trip: unbond a token amount (status → `UNBONDING`),
then cancel it in full. `CancelUnbondRole` recomputed status from the bond, saw
it above `MinBond`, and wrote `NORMAL` — voiding a `DemotionCooldownUntil` that
had not elapsed. `MsgBondRole` checks the cooldown before re-bonding, but
nothing checked it on the way back from `UNBONDING`.
`clampStatusToDemotionCooldown` now holds the role at `DEMOTED` for as long as
the cooldown runs, whatever the bond says. Regression:
`TestBondedRole_CancelUnbondCannotLaunderADemotion`.

See the "BondedRole (generic accountability primitive)" state section above for the keeper API used by content-module handlers.

### Member Accountability Messages

> **Note:** Five messages; resolution authority is either `sentinel` or a `commons` council proposal depending on severity.

```protobuf
message MsgReportMember {
  option (cosmos.msg.v1.signer) = "creator";
  string creator            = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string member             = 2;
  string reason             = 3;
  uint64 recommended_action = 4;  // GovActionType
}
message MsgReportMemberResponse {}

message MsgCosignMemberReport {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string member  = 2;
}
message MsgCosignMemberReportResponse {}

message MsgDefendMemberReport {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string defense = 2;
}
message MsgDefendMemberReportResponse {}

message MsgResolveMemberReport {
  option (cosmos.msg.v1.signer) = "creator";
  string creator = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  string member  = 2;
  uint64 action  = 3;  // GovActionType
  string reason  = 4;
}
message MsgResolveMemberReportResponse {}

message MsgAppealGovAction {
  option (cosmos.msg.v1.signer) = "creator";
  string creator       = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];
  uint64 action_type   = 2;  // GovActionType
  string action_target = 3;
  string appeal_reason = 4;
}
message MsgAppealGovActionResponse {}
```

`MsgAppealGovAction` creates a `GovActionAppeal` record and an appeal initiative (via `CreateAppealInitiative`) with deadline = `now + DefaultAppealDeadline`. It accepts only the member actions `MsgResolveMemberReport` applies (`MemberActionAppealTypes`: WARNING, DEMOTION, ZEROING), and only:

- from the affected member: `action_target` is the member's address and the signer must pass `CanActAs(signer, action_target)` (a migrated member appeals an action recorded against its old address; a tombstone never can);
- for an action actually applied: the member's report is RESOLVED with `resolved_action == action_type`;
- inside the filing window: `now <= resolved_at + DefaultAppealDeadline`;
- once per resolution (any earlier appeal of the same action created at or after `resolved_at` refuses a new one, and the action's `MemberActionRestoration` record must exist and not be marked `appealed`).

Everything else is refused with `ErrAppealNotAllowed` (1957). Every module-owned type (`ModuleOwnedAppealTypes`: forum POST_HIDE, THREAD_LOCK, THREAD_MOVE, REPLY_PIN, collect and artifact hides) is refused with `ErrInvalidReasonCode`: those appeals are filed through the owning module (`MsgAppealPost`, `MsgAppealThreadLock`, `MsgAppealThreadMove`, `MsgDisputePin`, `MsgAppealHide`), which checks who may appeal and marks its own record appealed. Filed through rep, they would do nothing to the forum record while the one-pending-appeal rule blocked the affected author's own appeal (FRM-02). TAG_REMOVAL, FORUM_PAUSE and REVIEW_REJECTION have no on-chain action record to appeal and are refused. An OVERTURNED member-action appeal reverses the action (see "Member-action reversal" below). Appeal resolution is handled via the two paths described in "Appeal Resolution" below.

#### Member-action reversal

`MsgResolveMemberReport` writes a `MemberActionRestoration` for every WARNING, DEMOTION or ZEROING it applies, keyed by the member's canonical address (`MemberActionRestorations`, re-keyed by a migration finalization like the report itself). It holds what an overturn needs:

| Action | Recorded at resolution | OVERTURNED reverses |
|---|---|---|
| WARNING | `warning_id` of the warning created | the warning is deleted |
| DEMOTION | `reputation`: per tag, the reputation the `severe_slash_penalty` slash took | the slashed amounts are added back to `reputation_scores` (to `lifetime_reputation` if a season reset came in between, where the reset would have archived them). A demotion is a reputation slash and never lowers the trust level, so there is no trust level to restore. |
| ZEROING | prior status, trust level and invitation credits, per-tag seasonal reputation, `burned_dream` (the liquid DREAM the final burn took) and the `zeroed_count` the zeroing produced | status, trust level, credits and reputation are restored (in the same season the scores come back out of the lifetime archive the zeroing added them to; in a later season they stay archived), `burned_dream` is re-minted onto the balance (counted in the season's minted total, outside the per-epoch mint ceiling: a restoration, not issuance), `lifetime_burned` is reduced by it, `zeroed_count` drops by one and the trust tree leaf is rebuilt |

What a zeroing forfeited is **not** restored: open challenge stakes, pending invitation stakes, bonded-role, report and bounty bonds, self-assign bonds and stake positions released before the burn went to counterparties or were burned (released stake principal is part of `burned_dream`), the inviter's accountability slash stands, and other modules' zeroing-hook cleanup (x/season, forum, collect and reveal lock records) is not undone.

A reversal only applies to the state the action left. A DEMOTION is not reversed onto a member who is no longer ACTIVE (zeroed meanwhile for another reason: no resurrection), and a ZEROING is reversed only while the member is still ZEROED with the recorded `zeroed_count`. Otherwise, and on any reversal error, nothing is written, the verdict and bond refund stand, and the `member_action_reversed` event (`appeal_id`, `member`, `action`, `result`) says `skipped: …` or `failed: …` instead of `reversed`. The reversal runs in a child cache context.

The record is bounded: it exists only while the action is appealable. Filing an appeal marks it `appealed` (with `appeal_id`); the appeal's terminal outcome (UPHELD, OVERTURNED or TIMEOUT, through `MsgResolveGovActionAppeal`, the jury tally or the timeout sweep) drops it. With no appeal, the EndBlocker drops it once the filing window `resolved_at + DefaultAppealDeadline` has passed, reading the `MemberActionRestorationExpiry` index `(window close, member)` up to the block time, at most 50 per block. A new report against the member is refused while the record exists. Records are exported in genesis (`member_action_restoration_list`); the expiry index is rebuilt at import.

Every `GovActionAppeal` write goes through `SetGovActionAppeal`, which maintains three indexes so no path scans the never-pruned appeal history: by `(action_type, action_target, id)` (the one-pending-appeal rule and the member-action dedup), PENDING appeals by `(deadline, id)` (the EndBlock timeout reads the due prefix, at most 50 per block) and by jury review id (the tally's verdict dispatch). They are rebuilt at genesis import.

### Appeal Resolution

`MsgAppealGovAction` charges `DefaultAppealBondAmount` (10 SPARK, in `uspark`) from the appellant and writes `AppealBond` + `Deadline` onto the `GovActionAppeal` record (`status = GOV_APPEAL_STATUS_PENDING`). Two resolution paths exist:

- **Council resolution** — `MsgResolveGovActionAppeal(resolver, appeal_id, verdict, reason)` with `verdict ∈ {UPHELD, OVERTURNED}`. A verdict slashes bonds, so the signer must be the Commons Operations Committee policy address (an executed committee proposal), the Commons Council policy or x/gov: `commonsKeeper.IsCouncilOrCommitteePolicy(ctx, resolver, "commons", "operations")` or the gov authority. A single committee member is refused (`ErrNotGovAuthority`). The message is in the committee's bootstrap `AllowedMessages`.
- **Timeout** — the rep EndBlocker transitions `PENDING` appeals past their `Deadline` to `TIMEOUT` (up to 50 per block).

Verdict effects:

| Verdict | Appellant bond | Sentinel bond | Forum counters |
|---|---|---|---|
| `UPHELD` | 50% burned, 50% stays in the sentinel reward pool | committed amount released | `RecordSentinelActionUpheld` increments `upheld_*`, resets `consecutive_overturns` |
| `OVERTURNED` | 100% refunded | `SlashBond(committed, "appeal_overturned")` where `committed = forumKeeper.GetActionCommittedAmount(...)` — the exact bond the action reserved, so slash == reserved (and the reservation is cleared by the same slash). Mirrors the UPHELD branch's committed-amount release. | `RecordSentinelActionOverturned` increments `overturned_*` and `consecutive_overturns`; at threshold (3) → `SetBondStatus(DEMOTED)` |
| `TIMEOUT` | 50% refunded, 50% burned | unchanged | no counter update |

### Module-Owned Appeal Targets

Each action type's verdict effects on content are applied by the module that owns that content, through a `ModerationAppealTarget` (`x/rep/types/expected_keepers.go`):
- `GetActionSentinel` and `GetActionCommittedAmount` supply the sentinel to exclude from the jury and the bond to release or slash.
- `ReverseSentinelAction` reverses the action on OVERTURNED.
- `OnSentinelActionResolved` handles module-local bookkeeping.

Forum's action types (`ForumAppealTypes`: POST_HIDE, THREAD_LOCK, THREAD_MOVE, REPLY_PIN) fall back to the wired `ForumKeeper`; no other type does, so a rep member-action appeal never reaches the forum. Other modules register a target per action type from `app.go` with `RegisterModerationAppealTarget`. A target whose actions are taken by a bonded role other than the content sentinel implements the optional `ModerationAppealRole.AppealRoleType(action_type)`, and the verdict releases, slashes and scores that role's bond (`appealRoleType`; content sentinel by default):

| Action type | Owner | `action_target` | Filed by |
|---|---|---|---|
| `GOV_ACTION_TYPE_COLLECT_HIDE` (11) | x/collect `RepAppealTarget` | collect HideRecord id | x/collect `MsgAppealHide` |
| `GOV_ACTION_TYPE_ARTIFACT_HIDE` (12) | x/artifact `RepAppealTarget` | artifact HideRecord id | x/artifact `MsgAppealHide` |

Rules:
- A target may also implement `ModerationAppealOutcomeHandler.OnAppealOutcome`. x/rep calls it after applying its own effects for UPHELD, OVERTURNED and TIMEOUT. Collect deletes and artifact scrubs on UPHELD; on TIMEOUT both restore the content and release the sentinel bond themselves, since x/rep does not touch it on a timeout. Hook failures are logged and never undo x/rep's verdict.
- Both are `ModuleOwnedAppealTypes` (with forum's four types). `MsgAppealGovAction` rejects them, because only the owning module can check who may appeal and mark its record appealed.
- An unregistered module-owned type has no target and never falls back to the forum keeper, so a collect hide id is never reversed as a forum post.
- A target whose record the module already closed (e.g. the content was deleted) reports no sentinel and no committed amount, so a late verdict cannot release or slash a bond twice.
- `ActionKindForGovAction` maps the two types to `collect_hide` and `artifact_hide`, so `RecordRoleOutcome` lands on the right RoleActivity kind.

These amounts/thresholds are sourced as compile-time constants (not operational params) from `x/rep/types/accountability_defaults.go`:

- `DefaultAppealBondAmount` (10 SPARK, uspark)
- `DefaultSentinelOverturnSlash` (100 DREAM, microDREAM) — legacy flat amount; the OVERTURNED slash now reads the per-action committed amount instead (the two coincide at defaults, since the forum `sentinel_slash_amount` an action reserves also defaults to 100 DREAM). Retained as the default the committed snapshot equals.
- `DefaultMaxConsecutiveOverturnsBeforeDemotion` (3)
- `DefaultSentinelDemotionCooldown` (7 days)

## Sentinel Rewards

Sentinels (forum moderators registered via `MsgBondRole` with `role_type = ROLE_TYPE_CONTENT_SENTINEL`) receive SPARK payouts for accurate, active moderation work. Implemented across Stages A/B/D of the sentinel-accountability feature.

### Reward Pool (SPARK)

- Lives at `SentinelRewardPoolAddress()` — a derived sub-address owned by x/rep,
  denominated in `uspark`. **Not the x/rep module account**, which it used to be:
  the pool moved to a sub-address so it could not be confused with the DREAM
  escrow and burn traffic sharing that account. Nothing reads the module
  account's SPARK balance, so anything sent there is stranded rather than
  pooled.
- **Contributions go through `AddToSentinelRewardPool`, never a direct send to
  the address.** Cross-module callers must not name the pool address at all.
  When the pool moved, x/forum's spam tax kept targeting the old destination and
  the money silently stopped arriving — for the length of a refactor, sentinels
  were underpaid by exactly what forum users had paid to reward them, with no
  error anywhere. Routing through the keeper method keeps the address x/rep's to
  change.
- **Funding sources:**
  - 50% of forum non-member spam taxes and edit fees — `spam_tax`, `reaction_spam_tax`, `flag_spam_tax`, `edit_fee`. The other 50% is burned.
  - 50% of appeal bonds on `UPHELD` verdicts (see "Appeal Resolution"). Other 50% burned.
  - Its share of the automatic community-pool draw — see [Automatic funding for bonded-role pools](#automatic-funding-for-bonded-role-pools).

### Curator Reward Pool (SPARK)

Held at `CuratorRewardPoolAddress()`, capped by `max_curator_reward_pool`
(100,000 SPARK — equal to the sentinel pool), distributed every
`curator_reward_epoch_blocks` to bonded curators at `NORMAL` clearing
`min_curator_accuracy` over `curator_accuracy_window_epochs`, weighted by
accuracy against the square root of decided ratings. Overflow above the cap is
partially burned per `curator_reward_pool_overflow_burn_ratio`.

Its only funding source is the automatic community-pool draw.

**Why it exists.** Before it, the curator role was pure downside: a curator
posted a slashable DREAM bond, earned nothing for rating a collection, and on
*winning* a challenge got back only their own committed bond while the
challenger's deposit was burned. The single economic signal attached to the role
was punishment, which does not staff expert judgment work.

**Accuracy comes from x/collect**, which reports challenge outcomes on both
branches of `ResolveChallengeResult` into the shared `RoleActivity` under
`collect_curation`. Reporting only the overturned side would make every curator
read as 0% accurate and the pool would pay nobody. collect keeps its own
`CuratorActivity` counters for its local demotion streak; the pool reads the
shared record, the same split as forum's sentinel bookkeeping.
- **Cap:** `max_sentinel_reward_pool` (default 100,000 SPARK). Overflow is partially burned per `sentinel_reward_pool_overflow_burn_ratio` (default 50%) each block by the rep EndBlocker.

### Federation Verifier Reward Pool (SPARK + DREAM stipend)

Held at `VerifierRewardPoolAddress()`, capped by `max_verifier_reward_pool`
(100,000 SPARK — equal to the sentinel and curator pools), distributed every
`verifier_reward_epoch_blocks` to bonded federation verifiers. Overflow above
the cap is partially burned per `verifier_reward_pool_overflow_burn_ratio`. Its
only funding source is the automatic community-pool draw.

Each eligible verifier also receives a flat `verifier_dream_reward` (5 DREAM)
mint, with the epoch total capped at `max_verifier_dream_mint_per_epoch` and all
recipients scaled down pro-rata when the cap binds.

**Why it exists.** The federation verifier was the one bonded role paid in DREAM
alone, which made doing the job structurally SPARK-negative: a verifier fetches a
peer's content off-chain, hashes it, and pays SPARK gas per submission, and was
compensated in a token that cannot be sold and cannot buy gas. The bridge
operator on the other side of the same exchange is paid SPARK. Sentinels,
curators and reviewers act on on-chain state, which is free to read, and are paid
SPARK too.

**Why it lives in x/rep.** The accuracy it scores comes from the shared
`RoleActivity` record x/rep owns, and a distribution resets that record's
per-epoch counters. Two modules distributing for one role on two independently
editable cadences would both reset those counters and neither would read a
coherent window. x/federation reports actions (`federation_verify`) and challenge
verdicts; x/rep pays.

**Score:** `1 + accuracy * sqrt(epoch_appeals_resolved)`.

The flat 1 is load-bearing. The obvious formula — `verified_count * accuracy` —
is the one already rejected for curators, and is worse here: verification is
mechanical hash-matching, a verifier with no decided challenge scores as fully
accurate, and challenges are rare. Paying per verification on that curve pays
most for high-volume rubber-stamping, which is the exact failure the role exists
to prevent. Volume therefore enters only as the `min_epoch_verifications` floor,
never as a weight. A verifier doing the minimum and one doing ten times the
minimum earn the same base, deliberately; it mirrors the DREAM stipend, which has
always been flat per eligible verifier. If challenges never materialise every
eligible verifier scores 1 and the pool splits evenly, so the pool is never
stranded, and an empty roster draws nothing at all because funding is
headroom-proportional.

This is the opposite choice from bridge-operator pay, which *is* volume-weighted.
The asymmetry is deliberate: an operator's submission count is attested by
somebody else (verifiers increment it), while a verifier self-certifies.

**Eligibility gates**, applied in order:

1. Bond status `NORMAL` or `RECOVERY`.
2. `epoch_actions["federation_verify"] >= min_epoch_verifications`.
3. Windowed accuracy over `verifier_accuracy_window_epochs` at least
   `min_verifier_accuracy` (0.80 — higher than the sentinel/curator 0.70, since a
   hash mismatch is objective in a way a moderation call is not). A verifier with
   no *decided* challenge in the window has not been demonstrated wrong and is
   treated as fully accurate; most verifications are never challenged.
4. No slash stamped in the window being paid for — see the window-test note in
   the RoleActivity section.

**Cadence.** `verifier_reward_epoch_blocks` is its own dial rather than a reuse of
the sentinel cadence, and is set to **one full federation `challenge_window`** in
blocks: 100800 on mainnet (7d), 43200 on testnet (3d), 1200 on devnet (2h). An
epoch shorter than the challenge window scores a verifier's accuracy before the
challenges against that epoch's work can resolve, so the accuracy ring would
always be reading a stale verdict count. Testparams deliberately breaks the rule
(10 blocks, against a 50-block challenge window) because the RECOVERY auto-bond
test must span two whole epochs, and relaxes `min_verifier_accuracy` to 0 so the
bar cannot fire on the stale count that shortcut produces.

**RECOVERY auto-bond.** SPARK is paid straight out even in `RECOVERY` — it
reimburses gas already spent, and withholding it would make recovery
self-defeating. The DREAM stipend instead auto-bonds the portion needed to
restore `min_verifier_bond`.

### Epoch Distribution

Runs in the rep EndBlocker every `sentinel_reward_epoch_blocks` blocks (default 14,400 = ~1 day at 6 s blocks; 20 blocks under testparams).

**Eligibility gates** (evaluated per sentinel, in order):

1. Counter presence — a forum-side `SentinelActivityCounters` record exists and is non-zero.
2. `min_appeals_for_accuracy` — total upheld + overturned decisions meets the floor.
3. `min_epoch_activity_for_reward` — epoch actions (hides + locks + moves + pins) meet the floor.
4. `min_appeal_rate` — if `epoch_hides > 0`, `epoch_appeals_filed / epoch_hides` ≥ floor.
5. `min_sentinel_accuracy` — `upheld / (upheld + overturned)` ≥ floor.
6. Bond status is not `DEMOTED`.

**Score:**

```
score = accuracy_rate * sqrt(epoch_appeals_resolved)
      + epoch_hides * 0.01
      + epoch_locks * 0.05
      + epoch_moves * 0.03
```

**Distribution:** pro-rata against total score, each allocation truncated to integer `uspark`. Residual dust stays in the pool for the next epoch.

**Payout side-effects:**

- Rep-side `BondedRole.cumulative_rewards` (for the sentinel's `ROLE_TYPE_CONTENT_SENTINEL` record) is incremented and `last_reward_epoch` is updated.
- A `sentinel_reward_distributed` event is emitted.

**Per-epoch counter reset:** Regardless of distribution outcome (pool empty, no eligible sentinels, or normal payout), the forum-side per-epoch counters (`epoch_hides`, `epoch_locks`, `epoch_moves`, `epoch_pins`, `epoch_appeals_filed`, `epoch_appeals_resolved`) are reset for every sentinel in the registry.

### Operational Parameters

All seven live on `Params` and `RepOperationalParams` (council-tunable via `MsgUpdateOperationalParams`):

| Parameter | Default | Role |
|-----------|---------|------|
| `max_sentinel_reward_pool` | 100,000 SPARK (`uspark`) | Pool cap; excess is burned per overflow ratio |
| `sentinel_reward_pool_overflow_burn_ratio` | 0.50 | Fraction of over-cap pool burned each block |
| `sentinel_reward_epoch_blocks` | 14,400 (testparams: 20) | Distribution cadence |
| `min_sentinel_accuracy` | 0.70 | Upheld / decided floor |
| `min_appeals_for_accuracy` | 10 | Min decisions before accuracy gate applies |
| `min_epoch_activity_for_reward` | 1 | Min epoch actions required |
| `min_appeal_rate` | 0.05 | Min `appeals_filed / hides` ratio when hides > 0 |

## Queries

### Sorted List Pagination

The project/initiative list queries (`ListProject`, `ListInitiative`,
`ProjectsByCouncil`, `ProjectsByCreator`, `InitiativesByProject`,
`InitiativesByAssignee`, `InitiativesByCreator`,
`AvailableInitiatives`) accept an optional `sort_by` request field that
orders the full (filtered) set before pagination is applied:

| Query family | Sort keys |
|---|---|
| Projects | `id` (default), `name` (case-insensitive), `budget` (approved budget), `status` |
| Initiatives | `id` (default), `title` (case-insensitive), `status`, `budget`, `tier`, `conviction` (current/required completion ratio; initiatives with no required conviction sort last regardless of direction) |

Direction follows `pagination.reverse` for every key; ties fall back to id
in the same direction so pages stay deterministic. An unknown key is
rejected with `InvalidArgument`.

A sorted page holds at most 100 items: a larger `pagination.limit` is clamped,
and the end offset is computed so a limit near `MaxUint64` cannot overflow.

A sorted query cannot page by store key (the sort order isn't the store
order), so when `sort_by` is set — and always, for the filtered queries —
pagination is offset-based: `next_key` carries the next offset as a decimal
string, and clients either echo it back via `pagination.key` or set
`pagination.offset` directly. `ListProject`/`ListInitiative` keep the
efficient key-paginated store walk when `sort_by` is empty.

The `ProjectsByCouncil`, `InitiativesByAssignee`, and
`AvailableInitiatives` responses return `repeated` full objects plus a
`PageResponse`. Prior revisions declared singular id/title/status fields
that could never carry more than one match; the fields were replaced (not
extended) because no client could have used them meaningfully.

### Authorship

Both `Project.creator` and `Initiative.creator` record the address that
submitted the creating message, so "who created this?" is answerable from
a node query alone — no off-chain indexer replaying `project_created` /
`initiative_created` events. `ProjectsByCreator` and
`InitiativesByCreator` invert the lookup ("what has this member
created?"). Creator is immutable once set and is independent of
`Initiative.assignee`: the member who scoped the work is often not the
member who takes it on, so `InitiativesByCreator` and
`InitiativesByAssignee` answer different questions.

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
  rpc ProjectsByCreator(QueryProjectsByCreatorRequest) returns (QueryProjectsByCreatorResponse);

  // Initiatives
  rpc GetInitiative(QueryGetInitiativeRequest) returns (QueryGetInitiativeResponse);
  rpc ListInitiative(QueryAllInitiativeRequest) returns (QueryAllInitiativeResponse);
  rpc InitiativesByProject(QueryInitiativesByProjectRequest) returns (QueryInitiativesByProjectResponse);
  rpc InitiativesByAssignee(QueryInitiativesByAssigneeRequest) returns (QueryInitiativesByAssigneeResponse);
  rpc InitiativesByCreator(QueryInitiativesByCreatorRequest) returns (QueryInitiativesByCreatorResponse);
  rpc AvailableInitiatives(QueryAvailableInitiativesRequest) returns (QueryAvailableInitiativesResponse);
  rpc InitiativeConviction(QueryInitiativeConvictionRequest) returns (QueryInitiativeConvictionResponse);

  // Interims
  rpc GetInterim(QueryGetInterimRequest) returns (QueryGetInterimResponse);
  rpc ListInterim(QueryAllInterimRequest) returns (QueryAllInterimResponse);
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

  // Trust tree export for anonymous (x/shield) clients
  rpc TrustTree(QueryTrustTreeRequest) returns (QueryTrustTreeResponse);

  // Economic Health (governance monitoring)
  rpc DreamSupplyStats(QueryDreamSupplyStatsRequest) returns (QueryDreamSupplyStatsResponse);
  rpc MintBurnRatio(QueryMintBurnRatioRequest) returns (QueryMintBurnRatioResponse);
  rpc EffectiveApy(QueryEffectiveApyRequest) returns (QueryEffectiveApyResponse);
  rpc TreasuryStatus(QueryTreasuryStatusRequest) returns (QueryTreasuryStatusResponse);
}

// Economic health query messages
message QueryDreamSupplyStatsRequest {}
message QueryDreamSupplyStatsResponse {
  string season_minted = 1 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];    // this season, resets at rollover
  string season_burned = 2 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];    // this season, resets at rollover
  string circulating = 3 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];      // member balances, staked included
  string total_staked = 4 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];     // the locked subset of circulating
  string treasury_balance = 5 [(gogoproto.customtype) = "cosmossdk.io/math.Int"]; // module treasury
  string staked_ratio = 6 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // staked / circulating
}
```

**Fields 1-2 are season figures, not all-time ones.** They were named
`total_minted` / `total_burned` and documented as "all-time" while reading
`SeasonMinted` / `SeasonBurned`, which `InitSeasonalPool` resets at every
rollover — so the query reported a season's flow under a name that reads as
lifetime supply. Renamed to match what they hold, and to match the same pair on
`QueryMintBurnRatioResponse`.

There are no all-time counters to point them at instead. Treasury flows bypass
the member ledgers entirely — `MintToTreasury` advances `TrackMint` without
touching any member, and the treasury-overflow burn touches no member's
`LifetimeBurned` — so summing member lifetimes would undercount both sides, and
real all-time figures would need two new chain-level counters plus genesis
carriage. They would also be largely redundant: `circulating` sums every
member's `DreamBalance` with the staked portion included (`LockDREAM` does not
reduce the balance; it adds to `StakedDream` alongside it), so
`circulating + treasury_balance` is already the live DREAM supply that all-time
minted-minus-burned would only approximate.

```protobuf
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

**Every DREAM burn advances `SeasonBurned`.** `TrackBurn` used to have exactly
one call site — the treasury-overflow burn — so the counter behind
`MintBurnRatio` and `DreamSupplyStats` reported one minor line as the chain's
entire destruction: no slashing, no failed challenges or invitations, no
creation fees, no bonds, no decay, no transfer tax, no zeroing. This is the same
defect shape as the pre-fix `TrackMint`, which counted only the protocol's 10%
treasury share.

Tracking now sits at each of the six paths that destroy DREAM:

| Path | Where tracked |
|---|---|
| Every module burn — creation fees, slashing, failed challenges and invitations, bonds, and the x/forum, x/collect, x/reveal, x/name and x/season burns | `BurnDREAM`, the choke point they all route through |
| Unstaked decay | `ApplyPendingDecay` — tracked there rather than in the bulk walker, because write paths also call it lazily |
| Staked decay | `decayStakes`, once per pass against the total it already accumulates for its event |
| Transfer tax (3%) | `TransferDREAM` |
| Zeroing | `ZeroMember`, which writes the member record directly |
| Treasury overflow | `BurnTreasuryOverflow` — a module ledger, not a member balance; this was the original and only call site |

The invariant, and what `TestTrackBurn_CoversEveryDreamDestructionPath` asserts
per path: `SeasonBurned` moves by exactly the total movement in members'
`LifetimeBurned`. SPARK burns — the bonded-role reward-pool overflows — are
deliberately excluded, since `SeasonBurned` counts DREAM.

## Keeper Surface for Other Modules (additions)

- `AddReputationCapped(ctx, addr, tag, amount) (granted, error)`: the
  address-based form of `GrantReputationCapped`. Resolves the alias, refuses a
  member that is not ACTIVE, applies `max_reputation_gain_per_epoch` per tag
  and returns what was granted. x/reveal grants tranche reputation through it.
- `MembersAfter(ctx, after, limit) (addresses, done, error)`: pages member
  records in address order, strictly after `after`. x/season's
  RESET_REPUTATION phase walks it, so a member without a season profile is
  reset too.
- `AddMemberZeroingHook(MemberZeroingHook)`: see Zeroing.
- `BurnLockedDREAM(ctx, addr, amount)`: see "Locked DREAM backs exactly one
  escrow".

**Input validation.** Optional amount fields (`staked_dream`, `amount`,
`approved_budget`/`approved_spark`, `budget`, `requested_budget`/
`requested_spark`) are refused when missing or negative instead of being
dereferenced (`ErrInvalidAmount`); `ApproveProject` refuses negative
approvals. Challenge and response evidence holds at most
`MaxChallengeEvidence` (20) entries of `MaxChallengeEvidenceLength` (1,024)
bytes (`ErrTooMuchEvidence`, 2411). The required conviction of a new
initiative is `conviction_per_dream x sqrt(budget)` computed with
`LegacyDec.ApproxSqrt` (no float in consensus code). The invitation
acceptance and accountability windows are counted in epochs (block height):
`Invitation.created_epoch` and `accountability_end_epoch` (created epoch +
`invitation_accountability_epochs`) drive expiry, the inviter penalty and the
referral window. `accountability_end` / `referral_end` remain unix-time
estimates for display at a nominal 6 s per block (`NominalBlockSeconds`); a
record without `accountability_end_epoch` falls back to them.

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
    GiftRecord      collections.Map[collections.Pair[string, string], types.GiftRecord]

    // Secondary indexes (avoid full table scans in EndBlocker)
    InitiativesByStatus  collections.KeySet[collections.Pair[int32, uint64]]    // (status, id)
    InterimsByStatus     collections.KeySet[collections.Pair[int32, uint64]]    // (status, id)
    InterimsByAssignee   collections.KeySet[collections.Pair[string, uint64]]   // (assignee, id), live interims only
    ProposedProjectsByCreator collections.KeySet[collections.Pair[string, uint64]] // (creator, id), PROPOSED only
    InterimDeadlineDue   collections.KeySet[collections.Pair[int64, uint64]]    // (due height, id) sweep hints
    ProjectExpiryDue     collections.KeySet[collections.Pair[int64, uint64]]    // (due height, id) sweep hints
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

Interims are created automatically by the system in response to events. The
ADJUDICATION, EXPERT_TESTIMONY, PROJECT_APPROVAL and BUDGET_REVIEW rows are
built today (the other system triggers are not wired; see the completeness
notes), and JURY_DUTY interims are no longer created at all: jurors are paid by
`RewardJurors` for the votes they cast.

| Trigger | Interim Type | Assignees | Reference |
|---------|-------------------|-----------|-----------|
| JuryReview created | ~~JURY_DUTY~~ (not created) | n/a | n/a |
| Juror calls an expert (`MsgInviteExpertWitness`) | EXPERT_TESTIMONY | Invited expert | JuryReview ID |
| Budget-backed project proposed (`requested_budget < budget_review_threshold`) | PROJECT_APPROVAL | Picked council's Operations Committee members | Project ID (settled by `MsgApproveProjectBudget`, paid to the members who took part) |
| Large budget request (`requested_budget >= budget_review_threshold`) | BUDGET_REVIEW | Picked council's Operations Committee members | Project ID (settled by `MsgApproveProjectBudget`, paid to the members who took part) |
| *(Permissionless projects skip PROJECT_APPROVAL — no interim created)* | | | |
| Tranche revealed | TRANCHE_VERIFICATION | Tranche stakers | Tranche ID |
| Founder contribution proposed | CONTRIBUTION_REVIEW | Operations committee | Contribution ID |
| Dispute filed | DISPUTE_MEDIATION | Assigned mediator | Dispute ID |
| Inconclusive jury verdict | ADJUDICATION | Technical operations committee | Initiative ID (the challenge is the one on that initiative still `IN_JURY_REVIEW`) |

## EndBlocker Logic

Located in `x/rep/keeper/abci.go`:

**Pending-invitation expiry.** `ProcessExpiredAccountability` runs at every epoch-end block and reads the `PendingInvitations` index (`(created_epoch, id)`, PENDING only) in creation order, stopping at the first invitation still inside its window, for records whose `created_epoch + invitation_acceptance_epochs` is behind the current epoch (default 30 epochs, counted in block height, independent of block time; 0 disables expiry). Each one is closed in its own cache context: the inviter's staked DREAM is unlocked first (found through the alias if the inviter migrated), then the invitation becomes `EXPIRED` and its `InvitationsByInvitee` entry is removed so the address can be invited again, and the credit it consumed is restored to an ACTIVE inviter, never above the trust level's maximum. The record is kept for history and an `invitation_expired` event carries `invitation_id`, `inviter`, `invitee`, `stake_unlocked`. `MsgRevokeInvitation` closes a pending invitation the same way on the inviter's request (status `REVOKED`, event `invitation_revoked`), so every `InvitationStatus` is now reachable. Accountability for accepted invitations stays passive (applied when the invitee is zeroed).

**Maintenance-mode gate.** While x/season reports `IsInMaintenanceMode` (reputation archive/reset and XP reset phases of a season transition), `MsgStake`, `MsgUnstake`, `MsgCreateInitiative`, `MsgCompleteInitiative` and `MsgCreateChallenge` are refused up front with `ErrMaintenanceMode` (2112). Tips, gifts, governance, invitations and queries stay open. x/season gates its own quest and guild messages the same way.

```go
func (k Keeper) EndBlocker(ctx context.Context) error {
    // 1. Recompute conviction for initiatives whose refresh is due, under a
    // per-block work budget. Replaces a sweep over every stake of every active
    // initiative on every block. See "Conviction refresh scheduling".
    k.DrainConvictionQueue(ctx)

    // 2. Check initiative completion thresholds (CheckSubmittedInitiatives):
    // a round-robin over the SUBMITTED status index, at most 100 initiatives
    // per block, resuming after the id it stopped at (SubmittedCheckCursor,
    // not exported, restarts at 0) and starting over at the end. Each
    // SUBMITTED initiative is checked at least once every ceil(n / 100)
    // blocks; conviction only grows, so a later check never misses a
    // completion, it can only land a few blocks later (REP-E-10).
    k.CheckSubmittedInitiatives(ctx)

    // 3. Finalize unchallenged initiatives (SweepDueInitiativeCompletions).
    // The per-block live-item sweeps read due queues (REP-E-10):
    // InitiativeCompletionDue / ChallengeResponseDue /
    // ContentChallengeResponseDue, keyed (due height, id). The status-index
    // helpers write an entry whenever an item enters its live status (IN_REVIEW
    // initiative, ACTIVE challenge, ACTIVE content challenge): at its deadline
    // when the record carries it, at 0 otherwise. Each sweep reads only entries
    // due at or below the current height, at most 100 per queue per block, and
    // checks the record: an item that left the live status is dropped, one not
    // yet due is re-queued at its real deadline, one whose processing fails is
    // re-queued at the next height (behind everything already due, so failing
    // items cannot starve the rest). The queues are not exported; InitGenesis
    // rebuilds them through the status-index helpers.
    //
    // Run in a child cache context, committed only on success: CompleteInitiative
    // mints to the completer, treasury and every staker before deleting the
    // stakes, and is not idempotent. EndBlocker writes straight to the deliver
    // state, so a mid-function error would persist those mints and pay them
    // again on the retry.
    //
    // An IN_REVIEW initiative with ChallengePeriodEnd == 0 never had its window
    // opened (legacy / pre-snapshot state); it is adopted — a full window opens
    // (initiative_challenge_window_opened) and the entry is re-queued at its
    // end — rather than completing vacuously or freezing.
    k.SweepDueInitiativeCompletions(ctx)

    // 4. DREAM decay runs once per epoch from the top of EndBlocker
    // (MaybeApplyBulkDecay) as a pass split across blocks: at most 500
    // member + stake records per block, resuming at a persisted cursor
    // (DecayPassEpoch / DecayPassMemberCursor / DecayPassStakeCursor), so no
    // block scales with the member or stake count. Members first (the lazy
    // per-member ApplyPendingDecay keeps unvisited members exact on every
    // read), then stake records, decayed by the epochs since the previous
    // pass (catch-up, capped at 500) so a pass outliving its epoch neither
    // skips nor repeats decay. DecayLastProcessedEpoch is written when the
    // pass completes. The pass cursor is not exported at genesis: an export
    // mid-pass restarts it (idempotent for members; stakes already visited
    // in that pass decay once more).
    // Unstaked decay: 0.2%/epoch, per member. Staked decay: 0.05%/epoch,
    // per reward-bearing stake record (decayStakes), moving the stake, its
    // pool denominators, its reward debt, and the member aggregates in
    // lockstep. New members (joined < NewMemberDecayGraceEpochs ago) are
    // exempt from both.

    // 5. Process expired challenge responses
    // If assignee doesn't respond within deadline, challenge is auto-upheld
    // as conceded: the challenger's reward is capped at the forfeited
    // self-assign bond (see Challenge).
    // Same cache-context containment as step 3: the uphold marks the
    // challenge resolved before it releases the stakes, and a resolved
    // challenge never re-enters this sweep.
    k.SweepDueChallengeResponses(ctx)

    // 5b. Same for ACTIVE content challenges past their response deadline
    // (UpholdContentChallenge), from ContentChallengeResponseDue.
    k.SweepDueContentChallengeResponses(ctx)

    // 6. Process jury review deadlines. Each tally is applied all-or-nothing:
    // TallyJuryVotes de-indexes the review before it resolves the challenge,
    // so a failed branch is discarded — the review stays PENDING and the next
    // block retries it.
    for _, id := range dueReviewIds {
        cacheCtx, writeCache := sdkCtx.CacheContext()
        if err := k.TallyJuryVotes(cacheCtx, id); err == nil {
            writeCache()
        }
    }

    // 7. Expire interims past their deadline (SweepDueInterimDeadlines) from
    // the InterimDeadlineDue queue, same model as step 3: at most 100 per
    // block, entry written at creation (and at InitGenesis) at the deadline.
    // The walk over every live interim this replaced grew with every
    // budget-backed proposal (REP-E-201). An EXPERT_TESTIMONY interim whose
    // review is still PENDING is re-queued at the review's deadline: the
    // verdict settles it. ExpireInterim is itself cache-wrapped: expiry and
    // the adjudication resolution it triggers commit together or not at all,
    // so a failed resolution leaves the interim PENDING and re-queued for the
    // next block.
    k.SweepDueInterimDeadlines(ctx)

    // 7b. Expire PROPOSED projects past expiry_block_height
    // (SweepDueProjectExpiries) from the ProjectExpiryDue queue, same model;
    // ExpireProject ends the review interim unpaid and runs in a child cache
    // context, retried next block on failure.
    k.SweepDueProjectExpiries(ctx)

    // 8. Distribute staking rewards from seasonal pool.
    // Rewards are pro-rata from MaxStakingRewardsPerSeason, split across
    // all epochs in the season. Once the pool is exhausted, no more rewards
    // are minted until the next season. Effective APY is self-adjusting:
    // more staked → lower per-unit yield; less staked → higher yield.
    // Internally gated on IsEpochEnd — this is called every block, and each
    // call takes remaining/remainingEpochs.
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

### TrustTree Query

`TrustTree` (`GET /sparkdream/rep/v1/trust_tree`, CLI `sparkdreamd q rep trust-tree`,
paginated) exports the tree for anonymous clients:

```protobuf
message QueryTrustTreeResponse {
  bytes root = 1;        // current root; empty before the tree is first built
  uint32 depth = 2;      // levels above the leaves
  uint64 leaf_count = 3; // leaf slots allocated so far
  repeated TrustTreeLeaf leaves = 4; // non-empty leaves {index, hash}, index order
  cosmos.base.query.v1beta1.PageResponse pagination = 5;
}
```

Empty leaves (the zero hash) are never stored, so they are not returned; a
client fills the gaps with zeros. A client downloads every page, rebuilds the
tree, finds its own leaf `MiMC(zk_public_key, trust_level)` and computes the
Merkle path locally. Because every client fetches the same whole tree, the
query reveals nothing about who is asking or which leaf they hold. Keeper:
[query_trust_tree.go](../x/rep/keeper/query_trust_tree.go).

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

## Member key migration

A member can move their on-chain identity from address `A` to a fresh address
`B` without loss of standing and without opening an accountability-evasion
path. Design rationale and threat model: the member-key-migration plan
(`docs/untracked/member-key-migration-plan.md`); this section is the
implemented contract.

### Model: eager core + forward alias

- **Eager core.** At finalization a bounded set of authority-bearing state is
  re-keyed from `A` to `B`: the `Member` record (DREAM balance, staked DREAM,
  reputation, lifetime archive, trust level, per-epoch/season counters,
  `key_migration_times`, `last_migrated_at`), the trust-tree leaf index,
  `InvitationsByInvitee`, `BondedRoles` and `RoleActivities` for every role type,
  `JuryParticipation`, `MemberStakePool`, an open `MemberReport`, the gift
  record from the inviter, and the `RecoveryCommitments` set. Module hooks
  (`MemberMigrationHooks`, wired in `app.go`) re-key their own bounded state:
  commons seats and votes on open proposals, names and `OwnerInfo`, the season
  profile / registration / guild references / open-epoch XP tracker / quest
  progress, session grants (path-aware, below). Federation drops identity links,
  attestations and pending challenges (peers attested `A`; `B` re-links). Service
  vetoes a KEY migration while `A` is a live (ACTIVE, UNDERFUNDED or UNBONDING)
  operator; on RECOVERY and COUNCIL it forces `A`'s ACTIVE and UNDERFUNDED
  operators into UNBONDING at finalization instead, and `MsgSweepTombstone`
  later claims the matured exit bond for `B`. Artifact never vetoes (a
  class-owner handover that names `A` is accepted by `B` through `CanActAs`).
- **Bonded roles follow the alias.** Every bonded-role and `RoleActivity`
  primitive (`GetBondedRole`, `IsBondedRole`, `GetAvailableBond`,
  `ReserveBond`, `ReleaseBond`, `IncreaseBond`, `SlashBond`, `RecordActivity`,
  `SetBondStatus`, `RecordRewardPayout`, `RecordRoleAction`,
  `RecordRoleOutcome`, the cooldown / epoch-count / accuracy reads, the slash
  stamp) resolves `Canonical` before touching the record. Owning modules pass
  the address stored on their action record (a hide's sentinel, a review's
  curator, a verification's verifier), which stays `A`; without the
  resolution a role holder would escape a slash, a demotion or a cooldown by
  migrating, and an honest holder's committed bond would be stranded.
  `EligibleForRole`, the action-time gate, refuses a tombstone.
- **Forward alias.** `A` becomes a permanent `Tombstone{canonical, path,
  migration_id, finalized_epoch}`. Everything unbounded (authorship, stakes,
  votes, escrows, listings) keeps storing `A` and is read through the alias
  contract. Tombstones are path-compressed: when `B` later migrates to `C`,
  every predecessor is rewritten to `C`, so `Canonical` is one read.
- **Value follows identity.** Every DREAM primitive (`MintDREAM`, `CreditDREAM`,
  `BurnDREAM`, `LockDREAM`, `UnlockDREAM`, the recipient of `TransferDREAM`) and
  every value lookup (`GetMember`, `GetTrustLevel`, `GetReputationTier`,
  `AddReputation`, `DeductReputation`, `SlashReputation`, `ZeroMember`,
  `DemoteMember`, the season-integration getters) resolve the alias first, so a
  liability recorded against `A` settles against `B`. A bank
  `SendRestrictionFn` (`Keeper.SendRestriction`, appended in `app.go`) rewrites
  any transfer recipient that is a tombstone to its canonical address, so SPARK
  escrow refunds, bounty refunds and role pay reach `B`. Gates
  (`IsMember`, `IsActiveMember`, `CanActAs`) refuse tombstones.

### Alias contract (keeper API, bech32 strings)

```go
Canonical(ctx, addr) string              // current address; addr itself if never migrated
IsTombstoned(ctx, addr) bool
CanActAs(ctx, signer, stored) bool       // !IsTombstoned(signer) && Canonical(stored) == signer
SameMember(ctx, a, b) bool               // Canonical(a) == Canonical(b)      (self-dealing bans)
RecentAddressesOf(ctx, member) []string  // canonical + predecessors inside AliasDedupHorizonEpochs, <= MaxRecentAliases+1
AllAddressesOf(ctx, member) []string     // canonical + every predecessor, <= MaxLifetimeAliases+1
```

Every module replaced `stored != signer` authorization comparisons with
`!CanActAs(signer, stored)`, dedup checks ("already voted / reacted / staked")
with a lookup over `RecentAddressesOf`, self-dealing bans with `SameMember`,
and by-address list queries with a union over `AllAddressesOf`. Gates that
grant member treatment use `IsActiveMember` (false for a ZEROED member), never
`IsMember`. `x/rep/types/alias_guard_test.go` walks every keeper and ante file
under `x/` and fails on a raw `stored == signer` comparison of a member-identity
field, a raw `Member.Get` by a stored address, or another module gating on
rep's `IsMember`, outside an allowlist that names the reason for each
exception (an entry the code no longer matches also fails).
`RecentAddressesOf` is the only form allowed inside gas-tight transactions
(a council vote has ~50k gas of headroom); x/commons re-keys votes on open
proposals eagerly instead. Compiled constants (`x/rep/types/migration.go`):
`AliasDedupHorizonEpochs` 240 (test build: effectively unbounded, so the e2e
season pin stays valid), `MaxRecentAliases` 8, `AliasRecoveryReserve` 2,
`MaxLifetimeAliases` 64, `MaxRecoveryCommitments` 4. The x/season params
validator keeps `season_duration_epochs + season_transition_epochs +
max_extension_epochs` inside the horizon.

### Paths and precedence

| Path | Opened by | Delay | Cancellable by |
|---|---|---|---|
| KEY | `MsgRequestMigration` signed by `A` | `key_migration_delay` (7d) | `A`, `B`, HR committee policy, gov |
| RECOVERY | `MsgRevealRecovery` (any account) against an active commitment | `recovery_migration_delay` (2d) | gov only |
| COUNCIL | `MsgForceMigration` by the Commons Governance (HR) committee policy or gov | `council_migration_delay` (21d) | HR committee policy, gov |

Every path ends with `MsgAcceptMigration` signed by `B` within
`migration_accept_window` (7d; a multisig `B` multisigns it), which sets
`executes_at` and enqueues finalization. Precedence is RECOVERY > COUNCIL >
KEY: opening a higher-ranked migration cancels a pending lower-ranked one, a
lower-ranked request is refused while a higher-ranked one is pending, and a
member has at most one pending migration.

KEY-path request checks: `migration_enabled` and `key_path_enabled` (the
"Q-day switch": gov turns the key path off if the account signature scheme is
considered broken; recovery and council keep working), `A` is an active
non-zeroed member, no open `MemberReport` against `A`, `migration_cooldown`
elapsed since the last migration, fewer than `max_key_migrations_per_window`
finalizations inside `key_migration_window` (rolling, stored on the member),
fewer than `MaxRecentAliases - AliasRecoveryReserve` recent aliases, `B`
fresh. RECOVERY and COUNCIL skip the report, cooldown and window checks and
use the full `MaxRecentAliases`.

**Freshness of `B`** (`checkTargetFresh` + the `BeforeMemberMigration` hooks):
not a member, no pending invitation (KEY path; RECOVERY and COUNCIL void it,
below), not a tombstone, not the target of another
*accepted* migration, not a reserved address (every module account and the
x/shield public submitter, registered from `app.go`), not a module account,
not an interchain account, not a group policy address, not a vault, no x/name
owner record or names, no x/season profile, no x/session grants as granter
(grantee-side fee grants and allowances are allowed: they are how a fresh `B`
pays gas), no live x/service operator record, no federation identity links.
Zeroed members cannot migrate on any path; a zeroing during the delay cancels
the migration at finalization.

**Nobody but the owner can make a recovery or council migration fail.** The
design intent is that RECOVERY outranks anything a thief holding `A`'s key can
do, and that a stolen key cannot block a council migration. Every condition a
third party (or the thief through `A`) could create is therefore either
impossible or ignored on those paths:

- A request does not reserve its target. `PendingMigrationByTarget` is written
  only when `B` accepts (it proves control then); REQUESTED migrations sit in
  the non-reserving `MigrationRequestsByTarget` index, so a KEY request naming
  the victim's recovery target squats nothing. Several requests may name one
  target; the first one `B` accepts wins and the others fail freshness at
  their own accept.
- `IsReservedMigrationTarget(B)` (an accepted migration into `B`, or a pending
  RECOVERY / COUNCIL request naming it) refuses `CreateInvitation` and
  `AcceptInvitation` for `B`, so nobody can invite the target to make it "not
  fresh". KEY requests do not reserve: anyone can name any address in one.
- Before the request exists the target is already public (a recovery reveal
  sits in the mempool, a forced migration in a commons proposal), so an
  invitation to `B` can be filed in that window. On the RECOVERY and COUNCIL
  paths a PENDING invitation to `B` therefore does not fail freshness: once
  every other check and hook has passed, the open voids it as `REVOKED`
  exactly like `MsgRevokeInvitation` (the inviter's stake is unlocked and the
  invitation credit restored, the invitee slot is freed) and emits
  `invitation_voided_by_migration` (`invitation_id`, `inviter`, `invitee`,
  `stake_unlocked`, `migration_id`). The front-run costs the victim nothing,
  where it used to consume a recovery commitment per attempt. The KEY path
  keeps refusing a target with a pending invitation.
- Module hooks veto from-side state only on the KEY path: an active x/name
  dispute on one of `A`'s names (a resolved dispute never blocks; on the other
  paths the dispute follows the name to `B`), a live x/service operator on `A`
  (forced into UNBONDING instead, above), a vault co-signed by `A` and `B`
  (x/vault's `Tally` counts the merged member once, at its largest entry
  weight, and a threshold the merge puts out of reach is clamped to the
  reachable weight), an open x/season display-name moderation on `A` (on the other paths
  it moves to `B` with its report stake, REP-R-101). Target-side checks apply
  on every path. `TestMigrationHooksNeverBlockRecoveryOrCouncil`
  (app/migration_hooks_guard_test.go) walks every wired hook: each must have an
  entry naming its from-side vetoes, each veto must trip on KEY, and none may
  trip on RECOVERY or COUNCIL.

**Season maintenance.** No migration finalizes while x/season is in
maintenance mode (archiving or resetting reputation and XP): the season walk
keys on member addresses. Due migrations wait in the queue and finalize once
maintenance ends.

### Recovery commitments

`RecoveryCommitmentSet` holds up to `MaxRecoveryCommitments` SHA-256
commitments (`SHA-256("sparkdream/recovery/commit/v1" || preimage)`). Every
commitment set, replaced or removed by an existing member
(`MsgSetRecoveryCommitment`, `MsgRemoveRecoveryCommitment`) waits out
`recovery_change_delay` (30d, validated longer than `key_migration_delay`);
only the commitment carried by `MsgAcceptInvitation.recovery_commitment` is
immediate. Reveal is commit-reveal: `MsgCommitRecovery` stores
`sealed = SHA-256("sparkdream/recovery/seal/v1" || preimage || A || B)` against
`recovery_commit_deposit` (refunded when a reveal consumes it, burned when it
expires after `recovery_commit_ttl`); `MsgRevealRecovery` is valid once the
seal has aged `recovery_reveal_min_blocks`, consumes the matched commitment,
voids every pending commitment change, cancels any pending KEY or COUNCIL
migration and opens a RECOVERY migration. Nothing on this path relies on a
signature by `A`.

Once the preimage and the aged seal check out, the commitment and the seal are
consumed **even if the migration cannot be opened** (for example because `B`
became a member in the meantime). The migration is opened in a child cache
context and only its writes are dropped; the transaction succeeds with
`migration_id = 0` and `failure_reason` set in `MsgRevealRecoveryResponse`,
and a `recovery_reveal_failed` event is emitted. A reverted reveal would leave
the commitment active while its preimage sat in the mempool and the block, and
whoever induced the failure could re-seal the preimage to an address of their
own. The member reveals another commitment.

### Council path and objections

`MsgObjectMigration`, signed by `A` while the COUNCIL migration is ACCEPTED,
posts `migration_objection_bond` and opens an x/rep jury review
(`ChallengerClaim = "member_migration_objection"`, jurors seated like a
moderation appeal, parties excluded). The objector is the challenger:
REJECT_CHALLENGE upholds the migration (finalizes at the next EndBlock, bond
paid to `B`); UPHOLD_CHALLENGE cancels it (bond refunded). The bond is never
burned: on the stolen-key path it is the victim's SPARK.

**Inconclusive objections escalate to x/gov.** INCONCLUSIVE, including a
deadline without quorum or a jury that could not be seated at all, neither
cancels nor refunds: if it did, a thief holding `A` on a chain too young or
quiet to seat a jury could stall every HR filing for free. The migration stays
OBJECTED (out of the finalization queue), the objector's bond stays in escrow,
`escalated_until = now + MigrationObjectionEscalationWindow` (compiled and
build-tagged with the other migration timings: 30 days on network builds,
90 seconds in the test build so e2e can reach the expiry path) is set on the migration and a `member_migration_objection_escalated`
event is emitted. Governance decides with the existing messages, no new Msg:

- `MsgCancelMigration` signed by x/gov upholds the objection: the migration is
  cancelled and the bond refunded to the objector (cancelling any OBJECTED
  migration refunds the objector; the HR committee may also withdraw its own
  filing this way).
- `MsgForceMigration{authority = x/gov, member = A, new_address = B}` naming
  the escalated migration's addresses rejects the objection: the bond is paid
  to `B` and the migration finalizes at the next EndBlock
  (`member_migration_objection_rejected`, `by = gov`). While an escalation is
  open the HR committee cannot use `MsgForceMigration` on `A` (it filed the
  migration and cannot overrule the objection to it), and x/gov cannot point
  it at another address without cancelling first.
- If governance has not decided by `escalated_until`, the EndBlocker rejects
  the objection the same way (`by = escalation_expired`), so nothing waits
  forever. A late jury verdict on an escalated review is ignored.

OBJECTED migrations are tracked in `ObjectedMigrations`, so the deadline and
escalation sweep walks only those.

### Finalization and tombstones

`ProcessMigrations` runs at the end of the x/rep EndBlocker with capped,
contained passes: finalize due migrations (each in a child cache context; a
failure marks the migration FAILED with the reason and frees the pending slot),
expire unaccepted requests, tally overdue objection juries and reject
escalated objections past `escalated_until`, burn expired seal
deposits, activate pending ZK keys. Finalization re-verifies the preconditions
and hooks, re-keys the core, writes and path-compresses the alias, runs
`AfterMemberMigration`, sweeps `A`'s spendable SPARK to `B` and sets `A`'s
x/distribution withdraw address to `B`. On the KEY path delegations stay at
`A` (the member still holds the key). On RECOVERY and COUNCIL the old key is
lost or stolen, so finalization begins undelegating every delegation of `A`
(at most `MaxTombstoneUndelegations` = 50, each in its own cache context; a
failure such as a validator's unbonding-entry cap emits
`tombstone_undelegation_failed` and the rest proceed). The normal unbonding
period and slashing apply; rewards withdrawn by the unbond go to `B` through
the withdraw address. x/staking pays the matured SPARK to `A` through
`UndelegateCoins`, which credits the account directly, so the bank send
restriction that redirects transfers to a tombstone does not apply.
`MsgSweepTombstone` (signed by the canonical address) collects it: on
RECOVERY and COUNCIL tombstones it first starts unbonding any delegation still
at `A` (one finalization left behind), then runs every registered tombstone
sweeper (`AddTombstoneSweeper`, wired from `app.go`; x/service claims `A`'s
matured operator exit bonds, which the send restriction delivers to `B`), then
moves `A`'s spendable SPARK to `B`. Once the unbonding matures, one more sweep
collects it. The staking keeper is late-wired (`SetStakingKeeper` in
`app.go`). Undelegating a validator operator's self-delegation can jail that
validator, as any undelegation below `min_self_delegation` would. The
REQUESTED accept deadline is stored on the migration (`accept_by`), so a later
change of `migration_accept_window` never moves a deadline or orphans its
queue entry.

The ZK key is path-aware: KEY keeps `zk_public_key` (the leaf is unchanged, so
proofs and nullifiers stay continuous); RECOVERY and COUNCIL clear it and set
`zk_key_cooldown_exempt`, because a signature-derived shield secret is
compromised with `A`. Session grants with `A` as granter are re-keyed on the
KEY path and revoked on the others; session keys with `A` as grantee are
revoked on every path, other grantee-side grants are re-keyed.

`TombstonedSignerDecorator` (`x/rep/ante`, early in the ante chain) refuses any
transaction signed by a tombstone unless every message is on its allowlist:
`MsgUndelegate`, `MsgWithdrawDelegatorReward` and
`MsgWithdrawValidatorCommission` on every path (they only move value toward
the member: unbonded coins are swept from the tombstone, rewards and
commission go to the withdraw address finalization pointed at `B`), and
`bank.MsgSend`, `MsgBeginRedelegate` and `MsgCancelUnbondingDelegation` only
when the tombstoning migration was a KEY migration. On RECOVERY and COUNCIL
the old key is presumed stolen; with redelegation a thief could move the
victim's stake to a validator they run at 100% commission, and with
unbonding cancellation hold the principal hostage. This is the backstop for
any authorization site the alias conversion missed. A tombstone can never be
invited again or register a ZK key. A lost key no longer leaves `A`'s
delegations bonded: RECOVERY and COUNCIL finalization unbond them (above).

### ZK key change discipline

Independently of migration, a change of an existing `zk_public_key` zeroes the
old leaf at once and parks the new key in `pending_zk_public_key` until
`zk_key_activates_at_epoch = now + zk_key_activation_delay` (14 epochs); the
EndBlocker activates it (`ZkKeyActivationQueue`). At most one change per season
(`last_zk_key_change_season`), except the first registration after a migration
cleared the key, which still waits out the delay. After `MsgClearZkPublicKey`
(`zk_key_cleared` set) the next registration is never the immediate
first-registration path: it goes through the activation delay and still honours
the season cap, so a member who already used their change stays without a key
until the next season rather than gaining an instant swap. Activation consumes
the flag.

The delay exists because an activated new key yields a fresh nullifier: on
any anonymous "once per item" action proven against the live member tree,
a member could act with the old key and again with the new one if the
item's window were still open when the new key activates. The delay must
therefore outlast every such window. Today the only one with a closing
window is x/federation's arbiter vote (`arbiter_resolution_window`); the
bound is the build-tagged `MaxAnonymousActionWindow` (mainnet 7 d, testnet
1 d, devnet 6 h, none in the test build): x/federation refuses an arbiter
window above it, and `Params.Validate` refuses a `zk_key_activation_delay`
whose wall time (`delay * epoch_blocks * 6 s`) is under it.
`TestZkKeyDelayOutlastsArbiterWindow` checks every shipped network
genesis. Anonymous forum, collect and blog votes and reactions have no
closing window, so no delay can cover them; they carry no reputation, DREAM,
XP or promotion weight, and the once-per-season limit on key changes bounds
the excess at one extra vote per member per season. Council voting periods
are not tied to the delay: x/commons secret ballots vote against a voter
snapshot taken when the ballot opens, so a key change during a window adds
no voter. (An earlier cap, which refused commons voting periods longer than
the delay and refused `MsgUpdateParams` lowering it below the longest one,
was removed: it protected nothing once ballots used snapshots, and the
genesis bootstrap's own windows broke it on every network build, CMN-101.)

### Personal addresses

Membership is a person. `IsPersonalAddress(addr)` fails for a reserved module or system address,
a module account, an interchain account (driven by a controller chain's module, not a key holder
here), a council or committee policy and an x/vault shared account (learned through the
late-wired `SetVaultChecker`). It gates `CreateInvitation`, `AcceptInvitationWithRecovery` (re-run
at acceptance, so an address that became a vault or policy in between, or an executed proposal
signing as the invitee, is still refused with error 1600) and migration-target freshness
(`checkTargetFresh`, error `ErrMigrationTargetNotFresh`). x/vault runs the same gate on its signer
sets. Guard: `x/rep/keeper/personal_address_test.go`.

### Migration messages

`MsgRequestMigration{member, new_address}`, `MsgAcceptMigration{new_address,
migration_id}`, `MsgCancelMigration{signer, migration_id}`,
`MsgSetRecoveryCommitment{member, commitment, replaces}`,
`MsgRemoveRecoveryCommitment{member, commitment}`, `MsgCommitRecovery{submitter,
sealed}`, `MsgRevealRecovery{revealer, member, new_address, preimage}`,
`MsgForceMigration{authority, member, new_address, evidence_uri}`,
`MsgObjectMigration{member, migration_id, reason}`, `MsgSweepTombstone{signer,
tombstone}`. All carry `(amino.name)`, none is shield-compatible, and all are in
x/session's `ForbiddenAllowlistMsgs`, which is built from the wider **identity
list** `reptypes.IdentityMsgTypeURLs()` = the migration messages plus
`MsgRegisterZkPublicKey` and `MsgClearZkPublicKey` (a session hot key that could
register or clear a ZK key would bypass a multisig account's threshold on the
anonymous identity; [docs/multisig-accounts.md](multisig-accounts.md)). Guarded
by `x/rep/types/migration_guard_test.go`, which walks the Msg service and checks
params validation refuses every identity message in either session list.
The Commons Governance Committee's bootstrap permissions include
`MsgForceMigration` and `MsgCancelMigration`. No fee on any of them: safety
actions pay gas only.

### Queries

`Canonical(address)` → canonical, tombstoned, path; `Aliases(member)` →
canonical, addresses (all), recent; `MemberMigration(id)`;
`MemberMigrations` (paginated); `PendingMigration(member)` (by the old
address); `PendingMigrationByTarget(address)` (by the new address, so the
new key can find the migration it must accept, including a recovery reveal
someone else submitted); `RecoveryCommitments(member)`; `RecoveryCommit(sealed_hex)` and
`RecoveryCommitsBySubmitter(submitter)` (paginated) return unexpired sealed
commits with their height and expiry, so a recovery can resume from any
device. Both pending queries
return NotFound when nothing is pending. `GetMember` and x/season's
`GetMemberProfile` resolve a predecessor address to the canonical record.

### Parameters

| Param | Default | Test build |
|---|---|---|
| `migration_enabled` / `key_path_enabled` | true | true |
| `key_migration_delay` | 7d | 20s |
| `recovery_migration_delay` | 2d | 10s |
| `council_migration_delay` | 21d | 40s |
| `migration_accept_window` | 7d | 120s |
| `migration_cooldown` | 30d | 30s |
| `max_key_migrations_per_window` / `key_migration_window` | 2 / 365d (>= 2 x cooldown) | 2 / 60s |
| `recovery_change_delay` | 30d (> key delay) | 30s |
| `recovery_reveal_min_blocks` / `recovery_commit_ttl` | 20 / 7d | 2 / 60s |
| `recovery_commit_deposit` / `migration_objection_bond` | 1 SPARK / 10 SPARK | same |
| `zk_key_activation_delay` | 14 epochs | 2 epochs |

Genesis exports `tombstone_list`, `member_migration_list`,
`member_migration_count`, `recovery_commitment_list`, `recovery_commit_list`;
the reverse alias index, pending pointers, the request-by-target index, the
objected set and the queues are rebuilt at import.
Events: `member_migration_requested/accepted/cancelled/failed/objected`,
`member_migrated`, `recovery_commitment_set`, `recovery_commit`,
`recovery_revealed`, `recovery_reveal_failed`, `tombstone_swept`, `rep.zk_public_key_change_scheduled`,
`rep.zk_public_key_activated`, `rep.zk_public_key_cleared`, plus `council_seat_migrated`, `names_migrated`,
`season_profile_migrated`, `grant_migrated` from the hooks.


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

### Micro-DREAM scaling — resolved

Four defaults carried values a thousand times larger than the comment beside them stated. Every other `math.Int` default in the module scales correctly (`MaxTipAmount` 100 DREAM = 100,000,000, `MaxInitiativeStakePerMember` 50,000 DREAM = 50,000,000,000, the tier budgets), which is what marked these as extra zeros rather than a choice. For scale, the genesis DREAM supply is 25,000 DREAM on testnet and mainnet and 85,000 on devnet, so every one of these ceilings sat far above any state the chain could reach — they could not bind, which made them decorative rather than protective.

| Param | Was (micro-DREAM) | Now | Reads as |
|---|---|---|---|
| `MaxStakingRewardsPerSeason` | 25,000,000,000,000 | 25,000,000,000 | 25,000 DREAM per season |
| `MaxTreasuryBalance` | 100,000,000,000,000 | 100,000,000,000 | 100,000 DREAM, excess burned |
| `MaxInitiativeRewardsPerSeason` | 100,000,000,000,000 | 100,000,000,000 | 100,000 DREAM per season |
| `MaxDreamMintPerEpoch` | 10,000,000,000,000 | 250,000,000,000 | 250,000 DREAM per epoch |

The first three are restored to the figure their comments always claimed. All three now bind on a chain of this size, which is the point of having them: `MaxTreasuryBalance` starts burning treasury overflow at 100,000 DREAM, `MaxInitiativeRewardsPerSeason` stops completion payouts for the remainder of a season at 100,000 DREAM, and `MaxStakingRewardsPerSeason` is the outer ceiling above the mint-share and schedule terms that size the seasonal pool.

`MaxDreamMintPerEpoch` is the exception: its documented intent — 10,000 DREAM per epoch, ~1.5M per season at 150 epochs — is **not restorable**, and the naive 1000x correction would be a bug. x/season's retro PGF distribution pays out an entire season's budget in the season-transition block, up to `retro_reward_budget_max` = 75,000 DREAM, and every DREAM of it is minted through this same ceiling (`PayRetroPgfReward` → `MintDREAM` → `CheckAndTrackEpochMint`). A 10,000 DREAM epoch ceiling would fail that distribution every season. The value is instead derived from the largest legitimate single-epoch mint: 75,000 DREAM of retro PGF plus a season's worth of initiative rewards (100,000 DREAM) landing in the same epoch, rounded up for headroom. At 250,000 DREAM per epoch the ceiling is ~10x the genesis DREAM supply — loose enough never to bite legitimate traffic, tight enough to bound pathological rubber-stamping, where the previous value was ~400x supply per epoch and bounded nothing.

The related failure mode is worth stating plainly, because it is the one this module has already hit: a mint ceiling that binds does not merely cap a payout, it **reverts the transaction that tried to exceed it**, and everything bundled into it. That is how an unbounded seasonal accumulator (see [Reward Mechanics by Target Type](#reward-mechanics-by-target-type)) took initiative completion down with it — `CompleteInitiative` settles stakes, settling a stake mints, and the mint exceeded the cap. Ceilings on this path should be set from the largest legitimate burst, never from a round number.

```go
var DefaultParams = Params{
    // Time
    EpochBlocks:          14400, // ~1 day (14400 blocks * 6s = 86400s)
    SeasonDurationEpochs: 150,   // ~5 months (150 days)

    // DREAM economics
    MaxStakingRewardsPerSeason: math.NewInt(25_000_000_000),     // 25,000 DREAM — absolute ceiling on the seasonal pool
    StakingRewardYieldPerEpoch: math.LegacyNewDecWithPrec(5, 4),  // 0.05%/epoch on the staked base
    StakingPoolMintShare:       math.LegacyNewDecWithPrec(5, 2),  // 5% of last season's non-staking mints
    StakingPoolCapBase:         math.NewInt(25_000_000_000),      // 25,000 DREAM — the genesis supply
    StakingPoolCapRate:         math.LegacyNewDecWithPrec(5, 2),  // 5% of the base per season elapsed
    // Effective per-unit yield is bounded by StakingRewardYieldPerEpoch, NOT by
    // pool/total_staked — that ratio diverges as the staked base shrinks.
    // No fixed StakingApy — replaced by seasonal pool to cap inflationary minting
    UnstakedDecayRate:       math.LegacyNewDecWithPrec(2, 3),    // 0.2% per epoch (~51.9% annualized compounded)
    StakedDecayRate:         math.LegacyNewDecWithPrec(25, 5),   // 0.025% per epoch (~8.7% annualized)
    NewMemberDecayGraceEpochs: 30,                                // ~1 month grace period (no decay)
    TransferTaxRate:         math.LegacyNewDecWithPrec(3, 2),    // 3%
    MaxTipAmount:            math.NewInt(10_000_000),             // 10 DREAM
    MaxTipsSentPerEpoch:     math.NewInt(50_000_000),             // 50 DREAM per sender per epoch
    MaxGiftPerInvitee:       math.NewInt(500_000_000),            // 500 DREAM per invitee, lifetime
    GiftOnlyToInvitees:      true,

    // Staking floors and caps
    MaxInterimRewardsPerSeason:      math.NewInt(50_000_000_000),     // 50,000 DREAM/season — the interim path's ceiling
    MinStakeAmount:                  math.NewInt(2_000),              // 0.002 DREAM — anti-dust floor, = 1/StakingRewardYieldPerEpoch
    MaxCompletionBonusStakeMultiple: math.LegacyNewDec(3),            // completion bonus <= 3x the external stake behind it
    MaxContentStakePerMember:        math.NewInt(10_000_000_000),     // 10,000 DREAM on any one content item
    MaxTotalContentStakePerMember:   math.NewInt(20_000_000_000),     // 20,000 DREAM across all content items

    // Permissionless creation fees (burned on creation — anti-spam + deflationary pressure)
    ProjectCreationFee:               math.NewInt(5_000_000),   // 5 DREAM — burned when creating a permissionless project
    InitiativeCreationFeeApprentice:  math.NewInt(1_000_000),   // 1 DREAM — burned for apprentice initiative under permissionless project
    InitiativeCreationFeeStandard:    math.NewInt(3_000_000),   // 3 DREAM — burned for standard initiative under permissionless project
    PermissionlessMinTrustLevel:      2,                         // ESTABLISHED — minimum trust level to create a permissionless project
    PermissionlessMaxTier:            1,                         // STANDARD — highest tier allowed in permissionless projects

    // Treasury management
    MaxTreasuryBalance: math.NewInt(100_000_000_000), // 100,000 DREAM — excess burned
    TreasuryFundsInterims: true,  // interims paid from treasury first, mint only if empty
    TreasuryFundsRetroPgf: true,  // retro PGF paid from treasury first, mint remainder

    // Initiative rewards
    CompleterShare:          math.LegacyNewDecWithPrec(90, 2), // 90%
    TreasuryShare:           math.LegacyNewDecWithPrec(10, 2), // 10%

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
    // Per-member cap on conviction, as a share of required_conviction. Sets
    // the staker floors together with the two ratios above: three members at
    // the cap cover a whole threshold, two do not. Validate holds it in
    // [1/3, 0.375) -- see the self-assignment safeguards section.
    MaxConvictionSharePerMember: math.LegacyNewDecWithPrec(35, 2), // 35%

    // Review periods
    DefaultReviewPeriodEpochs:    7,  // ~1 week
    DefaultChallengePeriodEpochs: 7,  // ~1 week

    // Self-assignment safeguards (creator-assigned initiatives)
    SelfAssignedBondRate:                math.LegacyNewDecWithPrec(10, 2), // 10% of budget
    SelfAssignedExternalConvictionRatio: math.LegacyNewDecWithPrec(75, 2), // 75% external
    SelfAssignedChallengeMultiplier:     2,                                // 2x challenge window

    // Invitations
    MinInvitationStake:             math.NewInt(100),
    InvitationAccountabilityEpochs: 150,                                // 1 season
    InvitationAcceptanceEpochs:     30,                                 // PENDING invitations expire after ~1 month (0 = never)
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


    // Slashing
    SevereSlashPenalty:   math.LegacyNewDecWithPrec(30, 2), // 30%

    // Extended staking (project/member/tag)
    // Project staking draws from the same seasonal reward pool (no separate APY)
    ProjectCompletionBonusRate: math.LegacyNewDecWithPrec(5, 2),  // 5% bonus on completion
    MemberStakeRevenueShare:    math.LegacyNewDecWithPrec(5, 2),  // 5% revenue share
    TagStakeRevenueShare:       math.LegacyNewDecWithPrec(2, 2),  // 2% per tag
    MinStakeDurationSeconds:    86400,                              // 24 hours
    AllowSelfMemberStake:       false,

    // Recipient-side transfer limits (tips + initiative bounty payouts)
    MaxTransferReceivedPerEpoch:  math.NewInt(100_000_000),   // 100 DREAM
    MaxTransferReceivedPerSeason: math.NewInt(1_000_000_000), // 1,000 DREAM

    // Content staking (set MaxContentStakePerMember to 0 to disable)
    MaxContentStakePerMember:           math.NewInt(10_000_000_000), // 10000 DREAM per content item
    ContentConvictionHalfLifeEpochs:    14,                          // 14 days (slower decay than initiatives)

    // Author bond staking (set MaxAuthorBondPerContent to 0 to disable)
    MaxAuthorBondPerContent:            math.NewInt(1_000_000_000),  // 1000 DREAM per content item
    AuthorBondSlashOnModeration:        true,                        // slash bond if content is moderated/removed

    // Initiative review: the gate, the reviewers' pay, and the bounties that
    // fund attention on the work that needs it.
    ReviewRequiredAboveBudget:          math.NewInt(100_000_000),    // 100 DREAM — the APPRENTICE ceiling
    ReviewerBondReserveRate:            math.LegacyNewDecWithPrec(1, 1),  // 10% of budget, committed per verdict
    ReviewFeeRate:                      math.LegacyNewDecWithPrec(5, 2),  // 5% of budget, x tier multiplier
    MaxReviewRounds:                    3,                           // last rejection abandons
    ReviewBountyReclaimDelay:           14400,                       // ~1 day before a funder may reclaim
    PermissionlessMinReviewBountyRate:  math.LegacyNewDecWithPrec(1, 1),  // 10% of budget, in existing DREAM

    // Initiative bounties (paid to the assignee on completion)
    InitiativeBountyMaxBudgetRatio:    math.LegacyOneDec(),         // total <= budget
    MaxInitiativeBountyPerFunderEpoch: math.NewInt(100_000_000),    // 100 DREAM
    MinInitiativeBountyContribution:   math.NewInt(1_000_000),      // 1 DREAM
    MaxInitiativeBountyContributions:  20,
    InitiativeBountyReclaimDelay:      14400,                       // ~1 day

    // Reviewer bonded-role policy. Projected onto the BondedRoleConfig for
    // ROLE_TYPE_INITIATIVE_REVIEWER by SyncReviewerBondedRoleConfig; these
    // params are the source of truth, the genesis config list is a seed.
    MinReviewerBond:                    math.NewInt(500_000_000),    // 500 DREAM — level with sentinel and curator
    ReviewerDemotionThreshold:          math.NewInt(250_000_000),    // 250 DREAM — half the floor
    MinReviewerTrustLevel:              "TRUST_LEVEL_ESTABLISHED",   // required; the whole eligibility gate
    MinReviewerRepTier:                 0,                           // trust level already encodes reputation
    MinReviewerAgeBlocks:               0,
    ReviewerDemotionCooldown:           604800,                      // 7 days
    ReviewerUnbondCooldown:             1209600,                     // 14 days — open verdicts age out slashable

    // Bonded-role SPARK pools. Caps set the relative share, since the daily
    // draw is divided in proportion to each pool's headroom.
    MaxSentinelRewardPool:              math.NewInt(100_000_000_000), // 100,000 SPARK
    MaxCuratorRewardPool:               math.NewInt(100_000_000_000), // 100,000 SPARK — equal to sentinel
    MaxVerifierRewardPool:              math.NewInt(100_000_000_000), // 100,000 SPARK — equal, but drains on a longer cadence
    MaxReviewerRewardPool:              math.NewInt(150_000_000_000), // 150,000 SPARK — 1.5x the others
    RoleRewardInflationShare:           math.LegacyNewDecWithPrec(5, 1), // 0.5 of the pool's inflation income
    MinSentinelAccuracy:                math.LegacyNewDecWithPrec(70, 2), // 0.70
    MinCuratorAccuracy:                 math.LegacyNewDecWithPrec(70, 2), // 0.70
    MinReviewerAccuracy:                math.LegacyNewDecWithPrec(70, 2), // 0.70
    MinVerifierAccuracy:                math.LegacyNewDecWithPrec(80, 2), // 0.80 — carried over from federation

    // Federation-verifier pay. The distribution lives here, not in
    // x/federation: it is scored from RoleActivity and it resets that
    // record's per-epoch counters.
    VerifierRewardEpochBlocks:          getVerifierRewardEpochBlocks(),   // ~7 days on mainnet
    VerifierAccuracyWindowEpochs:       6,
    MinEpochVerifications:              3,                            // a floor on volume, never a weight
    VerifierDreamReward:                math.NewInt(5_000_000),       // 5 DREAM per eligible verifier
    MaxVerifierDreamMintPerEpoch:       math.NewInt(100_000_000),     // 100 DREAM
}
```

> The block above is illustrative rather than exhaustive — `Params` carries 128
> fields and `DefaultParams()` in
> [x/rep/types/params.go](../x/rep/types/params.go) is the source of truth. Only
> the economically load-bearing dials are reproduced here.

## RepOperationalParams

Council-gated operational parameter updates (same pattern as x/blog and x/collect). These are day-to-day tuning knobs that do not affect core economic incentives or tier structures.

The `RepOperationalParams` message mirrors most `Params` fields except governance-only fields (tier configs, slashing penalties, trust level thresholds). See `proto/sparkdream/rep/v1/params.proto` for the full field list.

**Epoch clocks.** `epoch_blocks` is fixed at genesis: `MsgUpdateParams` (x/gov
included) and `MsgUpdateOperationalParams` both refuse a change
(`ErrParamImmutable`, 2412), so it moves only with a chain upgrade. Every epoch clock in the module is
`height / epoch_blocks`; one committee member lowering it applied up to 500
epochs of decay to every member in one block (about 63% of liquid DREAM) and
made every block an epoch boundary, and x/season's epoch must match it.
`season_duration_epochs` is still in the message but only governance may
change it (`ErrParamGovernanceOnly`, 2405). Resending the current values, as a
full operational-params update does, is accepted.

**Governance-only fields** (NOT in RepOperationalParams):
- `ApprenticeTier`, `StandardTier`, `ExpertTier`, `EpicTier`
- `SevereSlashPenalty` (the one reputation slash rate: lost challenge, demotion; the former minor/moderate/zeroing tiers were removed as unused, and zeroing burns everything regardless)
- `TrustLevelConfig`
- `CompleterShare`, `TreasuryShare`
- `PermissionlessMinTrustLevel`, `PermissionlessMaxTier` (structural access control — governance only)
- `BudgetReviewThreshold` (requested DREAM at or above which a project proposal opens a BUDGET_REVIEW interim instead of a PROJECT_APPROVAL one; it prices the committee's own review work, so a committee must not set it)
- `MaxProposedProjectsPerCreator` (uint32, default 3, must be positive: how many budget-backed proposals one member may hold in PROPOSED at once across their recent aliases; each opens a paid committee review interim)

**Validation shared by both sets.** `interim_deadline_epochs` must be in
`[1, MaxInterimDeadlineEpochs]` (3650) and `interim_deadline_epochs ×
epoch_blocks` must fit a block height (REP-E-205): 0 made every
default-deadline `MsgCreateInterim` fail, and an overflowing product wrapped
the deadline negative so the interim expired on the next block.

**Operational fields** (council-tunable, included in RepOperationalParams):
- `ProjectCreationFee`, `InitiativeCreationFeeApprentice`, `InitiativeCreationFeeStandard` (fee amounts are tuning knobs)

**Cross-field constraint.** `MaxConvictionSharePerMember` is operational, but the
staker floors it sets are shared with `SelfAssignedExternalConvictionRatio` and
`ExternalConvictionRatio`, which are governance-only. `Params.Validate` therefore
holds the cap inside `[1/3, 0.375)`, so a council update cannot retune a floor
governance owns. The lower edge needs no governance field and is checked in
`RepOperationalParams.Validate` as well, failing before the merge with an error
naming the field the committee actually set.

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
| 1108 | `ErrExceedsMaxTipsPerEpoch` | Exceeds maximum DREAM tipped per epoch |
| 1109 | `ErrRecipientNotActive` | Recipient is not active |
| 1110 | `ErrExceedsGiftAllowance` | Exceeds lifetime gift allowance for this invitee |
| 1111 | `ErrGiftOnlyToInvitees` | Gifts only allowed to invitees |
| 1112 | `ErrExceedsTransferReceiveLimit` | Recipient has reached their transfer receive limit |
| 1113 | `ErrExceedsInitiativeBountyLimit` | Exceeds initiative bounty limit |
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
| 1305 | `ErrLargeProjectNeedsCouncil` | Project budget exceeds `large_project_budget_threshold` and the approver is not a council / Operations Committee policy (or gov); requires a proposal |
| 1306 | `ErrProjectTerminal` | Project is COMPLETED, CANCELLED or EXPIRED and can no longer accept stakes |
| 1401 | `ErrInitiativeNotFound` | Initiative not found |
| 1402 | `ErrInvalidInitiativeStatus` | Invalid initiative status |
| 1403 | `ErrInsufficientReputation` | Insufficient reputation for tier |
| 1404 | `ErrConflictOfInterest` | Assignee and project creator cannot judge their own initiative |
| 1405 | `ErrNotAssignee` | Not the assignee of this initiative |
| 1406 | `ErrTagNotRegistered` | Tag is not registered in the tag registry |
| 1407 | `ErrTooManyTags` | Too many tags on initiative |
| 1408 | `ErrSelfChallenge` | Assignee cannot challenge their own initiative |
| 1501 | `ErrStakeNotFound` | Stake not found |
| 1502 | `ErrNotStakeOwner` | Not the owner of this stake |
| 1503 | `ErrMinStakeDuration` | Minimum stake duration not met — returned by claim and compound before `min_stake_duration_seconds` has elapsed |
| 1504 | `ErrSelfMemberStake` | Cannot stake on yourself |
| 1505 | `ErrInvalidTargetType` | Invalid stake target type |
| 1506 | `ErrStakePoolNotFound` | Stake pool not found |
| 1507 | `ErrSelfContentStake` | Cannot stake conviction on own content |
| 1508 | `ErrContentStakeCap` | Exceeds max content stake per member for this content |
| 1509 | `ErrAuthorBondCap` | Exceeds max author bond per content item |
| 1510 | `ErrAuthorBondExists` | Author bond already exists for this content item |
| 1511 | `ErrAuthorBondNotFound` | No author bond found for this content item |
| 1512 | `ErrNotContentTargetType` | Target type is not a content conviction type |
| 1513 | `ErrNotAuthorBondType` | Target type is not an author bond type |
| 1514 | `ErrAuthorBondViaMsg` | Author bonds must be created via content module, not `MsgStake` |
| 1515 | `ErrInitiativeStakeCap` | Exceeds max initiative stake per member for this target |
| 1516 | `ErrInitiativeRewardCapReached` | Season initiative reward minting cap reached |
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
| 2106 | `ErrCompoundNotSupported` | Compounding is not supported for this stake target type; claim and re-stake instead |
| 2107 | `ErrTooManyStakeTranches` | Member has reached the max number of separate stakes on this target (`types.MaxStakeTranchesPerTarget`) |
| 2108 | `ErrStakeBelowMinimum` | Stake is below `min_stake_amount` |
| 2109 | `ErrInitiativeTerminal` | Initiative is COMPLETED, REJECTED or CLOSED and can no longer accept stakes |
| 2110 | `ErrInvalidInterimStatus` | Interim is not in a state that allows this action (e.g. completing an already-finalized one) |
| 2111 | `ErrInterimRewardCapReached` | Paying this interim would exceed `max_interim_rewards_per_season` |
| 2112 | `ErrMaintenanceMode` | x/season is archiving or resetting reputation/XP; stake, unstake, initiative and challenge messages are refused until the transition phase ends |

> **Note:** this table is not exhaustive. `x/rep/types/errors.go` registers 120 errors; the rows above cover the ranges documented so far. The 2001–2007 (content challenge), 2101–2105 (activity caps, mint cap, proposal-time caps) and 2201 blocks are registered but not yet tabulated here.

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
- **Aggregate cap** — `max_total_content_stake_per_member` caps one member's content conviction stakes across *every* item combined. The per-item cap alone only set the granularity of parking DREAM here, not the total; the aggregate is what bounds it. Tracked on `Member.content_staked_dream` via `updateStakePoolTotals` and rebuilt by `ReconcileStakePoolTotals` at genesis import. Author bonds are not counted — they are slashable escrow with their own per-item cap.
- **Minimum stake** — `min_stake_amount` floors every content stake, as it does every other `MsgStake`
- **Min duration** — same `min_stake_duration_seconds` cooldown as other stakes
- **Content stakes decay** — they earn no DREAM but are charged `staked_decay_rate` like every other staked position, so parking DREAM here is no longer free. See the Staked Decay section: the exemption they used to have rested on a conviction half-life that does not exist.
- **Conviction growth** — conviction *ramps* rather than decays: `time_factor = t / (2 * content_conviction_half_life_epochs)` capped at 1.0, so a stake matures to a permanent maximum. Despite the parameter name there is no half-life. What makes content conviction erode over time is the decay applied to the principal, since conviction is `amount * time_factor` and only the amount can carry it.

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
    // ForumKeeper is wired manually via SetForumKeeper to break the
    // forum -> rep -> forum cycle (rep's tag-moderation / tag-budget award
    // flow calls back into forum for post lookup + tag pruning).
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
- `x/rep/keeper/msg_server_clear_zk_public_key.go` — MsgClearZkPublicKey handler (leaked shield secret; sets `zk_key_cleared`)
- `x/rep/types/params.go` — DefaultParams, Validate, ApplyOperationalParams
- `x/rep/types/params_vals.go` — TrustLevelConfig (production vs testing values)
- `x/rep/types/errors.go` — All error codes
- `x/rep/types/expected_keepers.go` — External keeper interfaces
- `x/rep/module/depinject.go` — Dependency injection wiring

| 2300 | `ErrMigrationDisabled` | Member migration is disabled |
| 2301 | `ErrKeyPathDisabled` | Key migration path disabled |
| 2302 | `ErrMemberTombstoned` | Address migrated away |
| 2303 | `ErrMigrationZeroedMember` | Zeroed members cannot migrate |
| 2304 | `ErrMigrationOpenReport` | Key path refused while a member report is open |
| 2305 | `ErrMigrationCooldown` | Migration cooldown not elapsed |
| 2306 | `ErrMigrationWindowCap` | Rolling-window key migration cap reached |
| 2307 | `ErrMigrationAliasCap` | Alias ceiling reached |
| 2308 | `ErrMigrationPending` | Equal or higher precedence migration pending |
| 2309 | `ErrMigrationTargetNotFresh` | New address is not fresh |
| 2310 | `ErrMigrationNotFound` | Migration not found |
| 2311 | `ErrMigrationWrongStatus` | Migration status does not allow this action |
| 2312 | `ErrMigrationNotAuthorized` | Signer may not act on the migration |
| 2313 | `ErrMigrationVetoed` | A module vetoed the migration |
| 2320 | `ErrRecoveryCommitmentInvalid` | Recovery commitment is invalid |
| 2321 | `ErrRecoveryCommitmentExists` | Recovery commitment already registered |
| 2322 | `ErrRecoveryCommitmentNotFound` | Recovery commitment not found |
| 2323 | `ErrRecoveryCommitmentCap` | Maximum number of recovery commitments held |
| 2324 | `ErrRecoveryCommitmentInactive` | No active recovery commitment matches the preimage |
| 2325 | `ErrRecoverySealInvalid` | Sealed recovery commit is invalid |
| 2326 | `ErrRecoverySealExists` | Sealed recovery commit already exists |
| 2327 | `ErrRecoverySealNotFound` | No sealed commit binds this preimage and address pair |
| 2328 | `ErrRecoverySealTooYoung` | Sealed commit has not aged recovery_reveal_min_blocks |
| 2329 | `ErrRecoverySealExpired` | Sealed recovery commit has expired |
| 2330 | `ErrMigrationObjectionBond` | Objection bond could not be posted |
| 2331 | `ErrNotTombstoneOwner` | Signer is not the tombstone's canonical address |
| 2332 | `ErrTombstoneMsgForbidden` | Message not allowed from a tombstoned signer |
| 2340 | `ErrZkKeyCooldown` | ZK key changed once per season |
| 2341 | `ErrZkKeyPending` | ZK key change already waiting for activation |
| 2401 | `ErrInterimTypeSystemOnly` | ADJUDICATION, JURY_DUTY, EXPERT_TESTIMONY, PROJECT_APPROVAL and BUDGET_REVIEW interims are created by the module only; the two project reviews are also refused by `MsgSubmitInterimWork`, `MsgApproveInterim` and `MsgCompleteInterim` (settled by `MsgApproveProjectBudget`) |
| 2402 | `ErrInterimDeadlineInvalid` | Interim deadline must be after the current height |
| 2403 | `ErrInterimNeedsApproval` | Only ADJUDICATION interims complete through MsgCompleteInterim; others are paid by MsgApproveInterim |
| 2404 | `ErrSelfApproval` | An assignee cannot approve their own interim |
| 2405 | `ErrParamGovernanceOnly` | Parameter can only be changed by governance |
| 2406 | `ErrSelfAward` | A budget group member cannot award to their own post |
| 2407 | `ErrTagCreationRateLimited` | Per-member per-epoch tag creation limit reached |
| 2408 | `ErrContentNotFound` | Content stake target does not exist |
| 2409 | `ErrTooManyStakes` | Member holds `MaxStakeRecordsPerMember` stake records |
| 2410 | `ErrTooManyVouchedTags` | More than `MaxVouchedTags` vouched tags |
| 2411 | `ErrTooMuchEvidence` | Evidence list longer than `MaxChallengeEvidence` or an entry over `MaxChallengeEvidenceLength` bytes |
| 2412 | `ErrParamImmutable` | Parameter is fixed at genesis |
| 2413 | `ErrTagBudgetAwarderCap` | Awarder reached their per-epoch award cap for this tag budget |
| 2414 | `ErrInvalidJurorVote` | Juror vote must be UPHOLD or REJECT with confidence in [0, 1] |
| 2415 | `ErrProjectHasOpenWork` | Project still has open initiatives or active challenges (`MsgCompleteProject`) |
| 2416 | `ErrNotInitiativeReview` | Expert witnesses can be called only on a pending initiative jury review |
| 2417 | `ErrTooManyExpertWitnesses` | Jury review already has the maximum number of expert witnesses |
| 2418 | `ErrExpertIneligible` | Expert witness must be an active ESTABLISHED+ member with reputation in the initiative's tags and no stake in the dispute |
| 2419 | `ErrInvalidTestimony` | Expert testimony opinion or reasoning is outside its length bounds |
| 2420 | `ErrExpertNotEndorsed` | Expert invitation has not been endorsed by a second juror (`MsgSubmitExpertTestimony`) |
| 2421 | `ErrInvalidEndorsement` | Expert invitation can be endorsed only once, by a juror who answered the summons and is not the inviter (`MsgEndorseExpertWitness`) |
| 2422 | `ErrRoleTooNew` | A bonded role acted before its config's `min_age_blocks` had passed since registration (`EligibleForRole`, every bonded role) |
| 2423 | `ErrTooManyProposedProjects` | Creator already holds `max_proposed_projects_per_creator` budget-backed projects in PROPOSED, across their recent aliases (`MsgProposeProject`) |
| 2424 | `ErrApprovedBudgetExceedsRequested` | `MsgApproveProjectBudget` approved more DREAM or SPARK than the proposal requested |
