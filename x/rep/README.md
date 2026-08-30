# `x/rep`

The `x/rep` module is the core coordination engine of Spark Dream, implementing a reputation-based task system with DREAM token economics, conviction voting, human-verified accountability, and progressive trust levels.

## Overview

This module provides:

- **Member lifecycle** — invitation-based onboarding with accountability, five trust levels, "zeroing" instead of permanent bans
- **DREAM token** — internal earned token with minting, burning, limited transfers, lazy unstaked decay
- **Reputation system** — per-tag scores with seasonal resets, lifetime archive, and anti-gaming caps
- **Invitation system** — staked, accountable invitations with referral rewards
- **Projects and initiatives** — council-approved budgets with tiered, conviction-based work completion
- **Conviction staking** — time-weighted stakes on initiatives, projects, members, tags, and content
- **Challenge system** — challenges with jury resolution (anonymous challenges via x/shield)
- **Content challenges** — cross-module quality assurance via author bonds
- **Interim work** — fixed-rate delegated duties (jury duty, moderation, expert review)
- **MasterChef staking rewards** — epoch-based reward pools for member, tag, and project staking
- **ZK trust tree** — persistent sparse Merkle tree for `x/shield` ZK proof validation
- **Tag registry** — permissionless `MsgCreateTag` (trust-gated, fee-burned), `Tag`/`ReservedTag` storage, expiry GC
- **Tag moderation** — `TagReport` + resolve flow
- **Tag budgets** — `TagBudget` / `TagBudgetAward` reward pools per tag
- **Sentinel accountability** — generic bond/bond-status/activity record shared across content modules; forum holds the per-action counters
- **Sentinel reward pool** — SPARK pool funded by forum spam-tax splits and UPHELD appeal bonds; accuracy-weighted epoch distribution (see "Sentinel Accountability")
- **Automatic bonded-role funding** — one capped daily claim on the community pool in `BeginBlock`, divided across the bonded-role SPARK pools by headroom (see "Automatic Bonded-Role Funding")
- **Gov-action appeal resolution** — Operations-Committee `MsgResolveGovActionAppeal` + EndBlocker timeout path; verdicts drive appellant-bond burn/refund and, on OVERTURNED, slash the sentinel by the exact bond the action reserved (its per-action committed amount, read back from the forum record — slash == reserved)
- **Member accountability** — `MemberReport`, `MemberWarning`, `GovActionAppeal`, `JuryParticipation`; salvation counters live on the Member proto

## Concepts

### Members and Trust Levels

Members progress through five trust levels by earning reputation and completing interim work:

| Level | Min Reputation | Min Interims | Invitation Credits |
|-------|---------------|-------------|-------------------|
| NEW | 0 | 0 | 0 |
| PROVISIONAL | 50 | 3 | 1 |
| ESTABLISHED | 200 | 10 | 3 |
| TRUSTED | 500 | 1 season | 5 |
| CORE | 1,000 | 2 seasons | 10 |

**Two reputation pools, asymmetric ladder.** Verified-work rep (`reputation_scores`, from completed interims/initiatives/reveals) always counts. **Forum rep** (`forum_rep_per_tag`, from conviction-staked endorsements on x/forum content) is summed in **only for the NEW→PROVISIONAL and PROVISIONAL→ESTABLISHED** transitions — early participation is a legitimate signal at the lower rungs. It is **excluded from ESTABLISHED→TRUSTED and TRUSTED→CORE**, which compare verified-work rep alone, because those tiers gate council eligibility and initiative conviction-completion where discourse popularity would be too cheap to farm. A member can reach ESTABLISHED on forum rep alone but never TRUSTED without verified work. See [Reputation System](#reputation-system) and the [spec](../../docs/x-rep-spec.md#trust-level-progression-and-forum-reputation).

Member statuses: ACTIVE, INACTIVE, ZEROED. Zeroing burns all DREAM, zeroes reputation, and resets trust level — but the person can restart with a new address and new invitation ("punish position, not person").

### DREAM Token

DREAM is the internal earned token:

- **Minting**: initiative completion (primary), staking rewards, interim compensation, retroactive public goods
- **Burning**: slashing, failed challenges, failed invitations, unstaked decay (1%/epoch), transfer tax (3%)
- **Transfers**: tips (max 100 DREAM, 10/epoch), gifts (max 500 DREAM, invitees only, cooldown per recipient), bounties (escrowed)
- **No external trading**, no IBC transfer

**Lazy decay**: unstaked DREAM decays at 1%/epoch, applied lazily via `GetMember()` for O(1) scaling.

### Reputation System

- **Per-tag scores**: members earn reputation in specific domain tags (e.g., "smart-contracts", "governance")
- **Seasonal reset**: reputation resets at start of each season (~5 months); lifetime archive preserved
- **Decay**: 0.5% per epoch during season (applied lazily)
- **Anti-gaming cap**: max 50 reputation per tag per epoch

**Forum reputation** (`forum_rep_per_tag`) is a second, separate per-tag pool earned through conviction-staked endorsements on x/forum posts/replies — kept apart from `reputation_scores` so the trust ladder can count it toward PROVISIONAL/ESTABLISHED but exclude it from TRUSTED/CORE (see [Members and Trust Levels](#members-and-trust-levels)). x/forum drives it through four keeper methods on the `RepKeeper` surface:

- `AddForumRep(member, tag, amount)` — credits forum rep (ACTIVE members only); x/forum's `PostConvictionStake` accrual loop records the exact amount on each stake's `accrued_rep_per_tag`.
- `DeductForumRep(member, tag, amount)` — claws back forum rep, **floored at zero**; x/forum's `SlashStakesForPost` / `ExpireHiddenPosts` deduct precisely each stake's `accrued_rep_per_tag` when a post is finalized as hidden.
- `GetForumRep(member, tag)` / `SumForumRep(member)` — query a single tag or the total; `SumForumRep` feeds the PROVISIONAL/ESTABLISHED thresholds.

On season transition, `ArchiveSeasonalReputation` folds `forum_rep_per_tag` into the display-only `lifetime_forum_rep_per_tag` then clears it (mirroring `reputation_scores` → `lifetime_reputation`); the lifetime forum map is never read by the trust ladder.

### Invitation System

Inviters stake DREAM to create accountable invitations:

- Stake locked from inviter, returned (minus 10% burn) when invitee accepts
- If invitee is zeroed during accountability period (~5 months), inviter is slashed
- Inviter receives 5% of invitee's earnings during referral period
- Invitation credits reset per season based on trust level
- Cost multiplier: 1.1x per additional invitation

### Projects and Initiatives

**Projects**: council-approved budgets (DREAM + SPARK) with categories and tags.

**Initiatives**: self-selected work under projects with four tiers:

| Tier | Max Budget | Min Rep | Rep Cap | Reward Multiplier |
|------|-----------|---------|---------|-------------------|
| Apprentice | 100 DREAM | 0 | 25 | 0.5x |
| Standard | 500 DREAM | 25 | 100 | 1.0x |
| Expert | 2,000 DREAM | 100 | 500 | 1.5x |
| Epic | 10,000 DREAM | 250 | 1,000 | 2.0x |

### Conviction-Based Completion

Initiatives complete when:

- Total conviction >= threshold (`0.2 * sqrt(budget)`)
- External conviction >= 50% (non-affiliated stakers)
- No active challenges
- Challenge period passed

**Conviction formula**: `sqrt(total_stakes * time * reputation)` with 7-epoch half-life. Older stakes decay exponentially to prevent "set and forget" dominance.

### Stakes

Stakes lock DREAM on various targets to signal conviction:

| Target Type | Reward source | Description |
|-------------|---------------|-------------|
| Initiative | Seasonal pool (shared) | Signal belief in work quality; conviction-weighted completion bonus (1/10th of budget) |
| Project | Seasonal pool (shared) | Support active projects (`project_completion_bonus_rate` bonus on completion) |
| Member | Member pool (revenue share) | Peer support (circular A↔B blocked, no self-stake) |
| Tag | Tag pool (revenue share) | Domain expertise signal |
| Content | None | Blog/forum/collection conviction signal only |

There is no per-target APY. Initiative and project stakes draw pro-rata from one shared seasonal pool (`max_staking_rewards_per_season`), so effective yield self-adjusts with total staked; member and tag stakes earn a revenue share accumulated when the target earns. All four use MasterChef accounting — a per-stake `reward_debt` baseline taken at join and rebased on every settlement — routed through a single `settleStake` helper. See the reward-accounting section of [docs/x-rep-spec.md](../../docs/x-rep-spec.md).

- **Min stake duration**: 24 hours (`min_stake_duration_seconds`). Gates reward *collection*, not principal return. Claiming or compounding earlier is rejected with `ErrMinStakeDuration` and forfeits nothing. Unstaking earlier always returns the principal but forfeits accrued rewards (the DREAM is never minted and stays in the pool).
- **Max conviction share per member**: 33% (prevents single-member dominance)
- **Content stakes** capped at 10K DREAM per member per item
- **Tranche cap**: at most `types.MaxStakeTranchesPerTarget` (10) separate stake records per member per target. Stakes are never merged — each keeps its own `created_at` maturity clock and `reward_debt` baseline — so the cap is what bounds the record count instead.
- **Compounding** is available for member and tag stakes only. Initiative and project stakes are rejected with `ErrCompoundNotSupported`: growing the principal in place would give the added DREAM the maturity of the original `created_at`. Those stakers claim and re-stake, which starts a fresh tranche.

### Challenge System

Members can challenge initiative work quality:

- **Named challenges**: min 50 DREAM stake, identity public
- **Anonymous challenges**: via `x/shield`'s `MsgShieldedExec` (only `MsgCreateChallenge` is shield-compatible), no DREAM stake, identity hidden, module-paid gas

**Jury resolution**: 5 jurors (odd, configurable), weighted by reputation in relevant tags, 67% supermajority, min 50 reputation to serve. Auto-uphold if assignee doesn't respond within 3 epochs. Successful challenger receives 20% of initiative budget.

### Content Challenges

Cross-module quality assurance via author bonds:

- Content creators stake DREAM to bond their reputation to posts/collections (max 1,000 DREAM)
- Members challenge bonded content; same jury system as initiatives
- Successful challenger gets 50% of slashed bond (minted)
- 10% of content conviction propagates to linked initiatives
- Author bonds slashed on content moderation actions

### Interim Work

Fixed-rate delegated duties:

| Complexity | Compensation |
|-----------|-------------|
| SIMPLE | 50 DREAM |
| STANDARD | 150 DREAM |
| COMPLEX | 400 DREAM |
| EXPERT | 1,000 DREAM |

Types: JURY_DUTY, EXPERT_TESTIMONY, DISPUTE_MEDIATION, PROJECT_APPROVAL, BUDGET_REVIEW, MODERATION. Solo expert bonus: +50%. 7-epoch deadline. Capped reputation per tag per epoch prevents grinding.

### ZK Trust Tree

Persistent KV-based sparse Merkle tree for `x/shield` ZK proof validation:

- Leaves = `MiMC(zk_public_key, trust_level)` for each member with a registered ZK key
- Built incrementally via EndBlocker `MaybeRebuildTrustTree()` (dirty member tracking for O(depth) updates)
- Exposes `GetTrustTreeRoot()` and `GetPreviousTrustTreeRoot()` for stale-proof tolerance

### Tag Registry

> **Note:** x/forum imports `sparkdream.rep.v1.Tag` and calls into x/rep for existence/validation.

- `Tag` and `ReservedTag` storage lives in x/rep (`proto/sparkdream/rep/v1/tag.proto`, `reserved_tag.proto`).
- `MsgCreateTag` is permissionless, gated on `TRUST_LEVEL_ESTABLISHED` and the `max_total_tags` ceiling.
- `TagCreationFee` DREAM is deducted from the creator and fully burned. DREAM is an internal, non-transferable token, so burn is the only viable fee destination — splitting to a community pool isn't a design option.
- Tags expire after `tag_expiration` of non-use (reserved tags are exempt). Expiry GC runs in the x/rep EndBlocker.
- `ReservedTag` entries are created by governance/council via `MsgResolveTagReport` (action = RESERVE) and persist outside expiry.

### Tag Moderation

- `MsgReportTag` files a report against a tag; multiple reporters can cosign. `TagReport` tracks `total_bond`, reporters, and review status.
- `MsgResolveTagReport` (authority) applies one of several actions: ignore, reserve, ban/remove, or restore. When a tag is removed, x/rep calls `ForumKeeper.PruneTagReferences` to strip the tag name from forum posts that reference it.

### Tag Budgets

Per-tag reward pools that incentivize quality posts carrying a specific tag:

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateTagBudget` | Create an inactive pool with optional initial funding, scoped to one tag | Members |
| `MsgTopUpTagBudget` | Add SPARK to an existing pool | Budget creator |
| `MsgAwardFromTagBudget` | Pay out from the pool to a forum post's author | Budget creator |
| `MsgToggleTagBudget` | Enable/disable awards without withdrawing | Budget creator |
| `MsgWithdrawTagBudget` | Close pool and return remaining SPARK | Budget creator |

Award validation delegates to x/forum via `ForumKeeper.GetPostAuthor` / `GetPostTags` — x/rep verifies the target post exists and carries the budget's tag, but does not own post state.

### Initiative Review

Conviction measures whether people *wanted* work done, not whether it *was*
done — so nothing in the completion path ever reads the deliverable. A bonded
reviewer role closes that gap, modelled on content sentinels but a distinct
`RoleType`: the competence differs, a wrong approval **mints DREAM** rather than
hiding a post, and pooling the two would mix their bond and accuracy records.

- Opt-in per project via `MsgSetVerificationPolicy`. `min_verifier_count` 0 is
  the genesis default and means conviction-only, so nothing changes for a
  project that never turns it on.
- A reviewer must hold the role at `NORMAL`, clear the reputation bar, and be
  independent of the work — the same affiliates-plus-one-invitation-hop test
  that gates external conviction. A staker on the initiative may not review it,
  and a reviewer may not afterwards stake on it.
- Reviewing pays **per verdict filed, never per approval**; making pay depend on
  the verdict would rebuild the bias the role exists to remove.
- Pay has two parts: a DREAM fee per verdict filed, and an accuracy-gated SPARK
  pool that pays for reviewing *well*. The pool is auto-funded from the
  community pool (see "Automatic bonded-role funding"); it is also an ordinary
  bank sub-address, so a council can top it up with a plain send. An unfunded
  pool simply distributes nothing, and the DREAM fee stands alone.
- A rejection returns the work to `ASSIGNED` for another round, bounded by
  `max_review_rounds`. Verdicts are keyed by round.
- If nobody reviews, the round escalates to the Operations Committee, and
  committee silence resolves to *pass* — the initiative proceeds on conviction
  alone. Silence must never wedge an initiative, and silence must never mint.
- Accuracy comes from challenge outcomes and feeds the shared `RoleActivity`
  record, queryable with `query rep role-activity`.
- The role's eligibility and exit terms (`min_reviewer_bond` and the six params
  beside it) are **x/rep's own params**, projected onto the reviewer's
  `BondedRoleConfig` on `InitGenesis` and on every param update. Every other
  bonded role is written through by the module that owns it; this one x/rep owns
  itself, so the Operations Committee can retune it like any other role's rather
  than needing a chain upgrade.
- The 500 DREAM floor is a deliberately **low barrier to entry** — per-verdict
  exposure is the reserve (`reviewer_bond_reserve_rate` x budget), not the
  floor, so a small floor costs nothing in accountability. Reviewers raise their
  own ceiling by bonding more: free bond above open reserves decides which
  initiatives they can take and how many at once, and a top-up is just another
  `MsgBondRole` on the same record, usable immediately. At the default 10% rate
  the floor covers budgets to ~5,000 DREAM; an EPIC initiative reserves 1,000.

See the Initiative Review section of
[docs/x-rep-spec.md](../../docs/x-rep-spec.md).

### Automatic Bonded-Role Funding

Bonded-role SPARK pay does not wait on a council transfer. In `BeginBlock`,
x/rep draws **one** capped claim on the community pool into a single intake
sub-address and immediately divides it across the per-role pools (content
sentinel, initiative reviewer, collect curator, federation verifier) in
proportion to each pool's headroom (`max(0, cap - balance)`).

Caps set the relative share: reviewer 150,000 SPARK, sentinel, curator and
verifier 100,000 each.

Federation verifiers are funded here like every other bonded role. They used
to be the exception — paid a flat DREAM mint from x/federation's own
EndBlocker — which made the role structurally SPARK-negative to perform: a
verifier fetches a peer's content off-chain and pays SPARK gas per submission,
and was compensated in a token that cannot buy gas. They now draw from a SPARK
pool on the same terms as the other roles, plus the DREAM stipend. See
[Federation Verifier Pay](#federation-verifier-pay).

- The draw is bounded per UTC day by a share of inflation --
  `annual_provisions * community_tax * role_reward_inflation_share / 365` --
  ledgered in state so the allowance survives restarts, and additionally bounded
  by total headroom and by the community pool's balance. A fixed amount would
  take its largest share of the pool when the pool is poorest; a share is
  counter-cyclical. The base is inflation, not the pool balance, which holds the
  councils' 95M SPARK genesis allocation. Zero disables it.
- Headroom-proportional division needs no per-role funding parameter: a full
  pool draws nothing, so an idle role costs the community pool nothing, and a
  new bonded role inherits funding by being listed in `fundedRolePools`.
- x/rep must run before x/split in `BeginBlockers` — x/split distributes
  whatever remains in the community pool to the councils in full.
- Pool caps and the daily draw are bounded above by `RoleRewardPoolCeiling()`
  (1e18 uspark). The caps feed a multiplication and `math.Int` panics past 256
  bits, so an unbounded committee-editable cap would make a mistyped proposal a
  chain-halt bug.
- The day ledger and the review-escalation set are both exported in genesis.
- Inspect it with `query rep role-reward-pools`.

See the Automatic funding section of
[docs/x-rep-spec.md](../../docs/x-rep-spec.md).

### Federation Verifier Pay

Bonded federation verifiers (`ROLE_TYPE_FEDERATION_VERIFIER`) are paid a SPARK
pool share plus a flat DREAM stipend, distributed together every
`verifier_reward_epoch_blocks`.

The distribution lives in x/rep rather than x/federation because the accuracy
it scores comes from the shared `RoleActivity` record x/rep owns, and because
paying resets that record's per-epoch counters. Two modules distributing for
one role on two independently-editable cadences would each reset those
counters and neither would read a coherent window. x/federation reports the
actions and the challenge verdicts; x/rep pays.

**Score:** `1 + accuracy * sqrt(epoch_appeals_resolved)`.

The flat 1 is the point. The obvious formula — `verified_count * accuracy` —
is the one x/rep already rejected for collect curators, and it is worse here:
verification is mechanical hash-matching, a verifier with no decided challenge
scores as fully accurate, and challenges are rare. Paying per verification on
that curve pays most for high-volume rubber-stamping, which is precisely the
failure the role exists to prevent. So volume enters only as the
`min_epoch_verifications` floor, never as a weight: the flat term buys
availability and covers gas, and the contested term rewards judgment somebody
actually tested. A verifier doing the minimum and one doing ten times the
minimum earn the same base, deliberately.

**Eligibility gates**, in order: bond status NORMAL or RECOVERY; at least
`min_epoch_verifications` verifications this epoch; windowed accuracy at least
`min_verifier_accuracy`; and no slash stamped in the window being paid for.

That last gate is a window test, not an equality test. The distribution runs at
the boundary of epoch N, but the counters it pays for accrued during epoch
N-1, so a slash anywhere in that window stamps N-1. It accepts either N-1 or N.

**Cadence.** `verifier_reward_epoch_blocks` is its own dial rather than reusing
the sentinel cadence, set to one full federation `challenge_window` in blocks.
An epoch shorter than the challenge window scores a verifier's accuracy before
the challenges against that epoch's work can resolve, so the accuracy ring
would always be reading a stale verdict count. Testparams breaks this rule
deliberately for wall-time reasons, and relaxes `min_verifier_accuracy` to 0 to
compensate.

**RECOVERY auto-bond.** SPARK is paid straight out even in RECOVERY — it
reimburses gas already spent, and withholding it would make recovery
self-defeating. The DREAM stipend instead auto-bonds the portion needed to
restore `min_verifier_bond`.

See the Federation Verifier Pay section of
[docs/x-rep-spec.md](../../docs/x-rep-spec.md) and
[docs/x-federation-spec.md](../../docs/x-federation-spec.md) §3.9.

### Sentinel Accountability

> **Note:** The generic bond/identity lives here (x/rep); forum-specific action counters live in x/forum.

- `sparkdream.rep.v1.SentinelActivity` holds the 8 accountability fields: `address`, `bond_status`, `current_bond`, `total_committed_bond`, `last_active_epoch`, `consecutive_inactive_epochs`, `demotion_cooldown_until`, `cumulative_rewards`.
- `sparkdream.forum.v1.SentinelActivity` holds 29 forum-specific counters (hides/locks/moves/pins/proposals, per-epoch and cumulative tallies, local cooldowns).
- Generic `MsgBondRole` / `MsgUnbondRole` live in x/rep and operate on the rep record only, keyed by `(role_type, address)`.
- `BondedRoleStatus` enum lives in x/rep: `NORMAL` / `RECOVERY` / `DEMOTED` / `UNBONDING`.

**Queued unbond.** `MsgUnbondRole` does not release DREAM immediately when the role's `BondedRoleConfig.UnbondCooldown` is positive (the default for every current role: 14 days for FORUM_SENTINEL, FEDERATION_VERIFIER and INITIATIVE_REVIEWER, 7 days for COLLECT_CURATOR). Instead it sets `pending_unbond_amount`, `unbond_completion_time = block_time + UnbondCooldown`, and flips status to `UNBONDING`. DREAM stays locked and slashable through the cooldown. **Role authority during `UNBONDING` is a bond-*quantity* decision, not a blanket deauthorization**: owning modules judge the holder on its *staying* bond (`current_bond - pending_unbond_amount`), so a small partial unbond that leaves the staying bond above the role's floor keeps the role usable — only the withdrawn portion is treated as gone. This falls out for free because `GetAvailableBond` / `ReserveBond` are pending-aware (below). **Bond modifications are incremental — none of them require waiting out the cooldown first.** A `MsgUnbondRole` while already `UNBONDING` accumulates into `pending_unbond_amount` (bounded by `current_bond - total_committed_bond`) and resets the single `unbond_completion_time` to `now + cooldown` (the staying bond is locked at least the full cooldown, never less). A `MsgBondRole` top-up mid-unbond is also allowed — a bond only adds slashable collateral, so it raises `current_bond` immediately while the queued withdrawal keeps maturing. Both keep the role `UNBONDING`. A `MsgCancelUnbondRole` walks a withdrawal back: it reduces `pending_unbond_amount` (no DREAM moves — pending is only an earmark on still-locked bond), and cancelling all of it clears the clock and returns the role to active status (`NORMAL` / `RECOVERY` per the unchanged `current_bond`). So holders can bond / unbond / cancel / rebond in as many increments as they like (correct a mistyped amount, grow or shrink a withdrawal) without serializing on a 14-day wait. The rep EndBlocker's `MatureUnbonds` finalizes at maturity: unlocks remaining DREAM (**never releasing earmarked committed bond** — capped at `current_bond - total_committed_bond`, deferring any remainder to a later block) and recomputes status from the final `current_bond` against the role's thresholds (NORMAL if ≥ `min_bond`, RECOVERY between thresholds, DEMOTED with `demotion_cooldown` gating re-bonding if below `demotion_threshold`). Partial unbonds that keep the role active stay NORMAL. Setting `UnbondCooldown == 0` reverts to legacy immediate-unlock for that role.

Keeper methods exposed to consumers (content modules call these):

| Method | Purpose |
|--------|---------|
| `IsBondedRole(ctx, roleType, addr)` | Boolean existence check |
| `GetBondedRole(ctx, roleType, addr)` | Fetch the rep-side record |
| `GetAvailableBond(ctx, roleType, addr)` | Returns the bond that can back a new action: `current_bond - total_committed_bond - pending_unbond_amount` (pending-aware, so bond on its way out cannot back new reservations) |
| `ReserveBond(ctx, roleType, addr, amt)` | Increment committed bond; errors if available (pending-aware) < amt |
| `ReleaseBond(ctx, roleType, addr, amt)` | Decrement committed bond (saturating) |
| `SlashBond(ctx, roleType, addr, amt, reason)` | Unlock + burn DREAM, decrement current + committed; caps `pending_unbond_amount` at new `current_bond` during UNBONDING (status stays UNBONDING) |
| `RecordActivity(ctx, roleType, addr)` | Stamp last-active-epoch, reset consecutive-inactive counter |
| `SetBondStatus(ctx, roleType, addr, status, cooldown)` | Update bond-status and demotion cooldown |
| `MatureUnbonds(ctx)` | EndBlocker: finalize matured pending unbonds (unlock DREAM up to `current_bond - total_committed_bond`, recompute status from the final bond, start demotion cooldown only if it lands DEMOTED) |

Forum content-action handlers (hide / lock / move / pin / dismiss-flags) authenticate via `GetBondedRole` behind the shared `eligibleSentinel` quantity gate (NORMAL/RECOVERY, or UNBONDING with staying bond ≥ floor) and manage commitment via `ReserveBond` / `ReleaseBond` / `SlashBond`; they still update their own forum-side counters locally. Because `ReserveBond` is pending-aware, a slash reserved during an unbond can only draw on the staying, uncommitted bond — so the action stays fully backed through its appeal window even as the unbond drains.

**Reward distribution.** Active sentinels earn from an x/rep-owned SPARK reward pool (`uspark`) fed by 50% of forum non-member spam/edit/flag/reaction fees (via `AddToSentinelRewardPool`) and 50% of `UPHELD` appeal bonds (remainder burned); pool capped at `max_sentinel_reward_pool` with overflow burn per `sentinel_reward_pool_overflow_burn_ratio`. Every `sentinel_reward_epoch_blocks` the rep EndBlocker distributes the pool pro-rata on an accuracy-weighted score (`accuracy_rate * sqrt(epoch_appeals_resolved)` plus small bonuses per hide/lock/move) to sentinels that clear the eligibility gates (`min_appeals_for_accuracy`, `min_epoch_activity_for_reward`, `min_appeal_rate`, `min_sentinel_accuracy`, not `DEMOTED`). Payouts update `cumulative_rewards` + `last_reward_epoch` on the rep-side record and forum-side per-epoch counters are reset for all sentinels. See [docs/x-rep-spec.md](../../docs/x-rep-spec.md#sentinel-rewards) for the full spec.

### Member Accountability

> **Note:** Five messages, four state objects.

| Object | Description |
|--------|-------------|
| `MemberReport` | Community report with evidence post IDs, cosigners, optional defense, recommended `GovActionType` |
| `MemberWarning` | Issued as a resolution outcome; warning count feeds auto-demotion threshold |
| `GovActionAppeal` | Appeal filed against a governance action (warning, demotion, zeroing, tag removal, forum pause, thread lock/move) |
| `JuryParticipation` | Per-juror history (assigned / voted / timeouts / excluded) |

Messages:

| Message | Description | Access |
|---------|-------------|--------|
| `MsgReportMember` | File a member report with recommended action | Members |
| `MsgCosignMemberReport` | Cosign an existing report (threshold gates escalation) | Members |
| `MsgDefendMemberReport` | Reported member submits defense | Reported member |
| `MsgResolveMemberReport` | Authority resolves (warn / demote / zero / dismiss) | Governance / sentinel |
| `MsgAppealGovAction` | Appeal an applied action; creates appeal initiative | Affected member |

Member salvation state is absorbed into the `Member` proto (`epoch_salvations`, `last_salvation_epoch`) rather than a standalone message.

**Keeper-internal warning issuance.** Beyond `MsgResolveMemberReport`, other modules can record a `MemberWarning` directly via the keeper method `IssueWarning(ctx, member, issuedBy, reason, evidencePostIDs)`. It allocates a global id, derives a **per-member** `warning_number` (matching the resolve-report numbering so queries stay consistent), and emits `member_warning_issued`. `issuedBy` is the caller's module account address (auditable origin) and `reason` is a stable short identifier (e.g. `"promoted_hidden_content"`). It does not touch salvation counters — the accumulated warning count feeds the auto-demotion threshold. x/forum's `ExpireHiddenPosts` uses it to warn a post's promoter (when distinct from the author) on an unappealed hide; x/collect may use it for analogous accountability.

Enums: `GovActionType`, `MemberReportStatus`, `GovAppealStatus`.

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `Member` | `member/value/{address}` | Balance, reputation, trust level, decay tracking, ZK public key |
| `Invitation` | `invitation/value/{id}` | Pending/accepted invitations with accountability |
| `Project` | `project/value/{id}` | Council-approved project budgets |
| `Initiative` | `initiative/value/{id}` | Self-selected work units with conviction tracking |
| `Stake` | `stake/value/{id}` | Conviction/content/author bond stakes |
| `Challenge` | `challenge/value/{id}` | Initiative challenges with jury reference |
| `JuryReview` | `juryreview/value/{id}` | Jury voting on challenges |
| `Interim` | `interim/value/{id}` | Fixed-rate delegated work |
| `InitiativeReview` | `initiativereview/value/{initiative_id}/{round}/{reviewer}` | A bonded reviewer's verdict on one round of submitted work |
| `ContentChallenge` | `contentchallenge/value/{id}` | Challenges on bonded content |
| `GiftRecord` | `giftrecord/{sender}/{recipient}` | Gift cooldown tracking |
| `MemberStakePool` | `stake/member_pool/{address}` | Aggregate member stake pool for rewards |
| `TagStakePool` | `stake/tag_pool/{tag}` | Aggregate tag stake pool for rewards |
| `ProjectStakeInfo` | `stake/project_info/{id}` | Project-level stake aggregation |
| `Tag` | `tag/value/{name}` | Tag registry entry with usage/expiry metadata |
| `ReservedTag` | `reserved_tag/value/{name}` | Governance-reserved tag with authority |
| `TagReport` | `tagreport/value/{name}` | Pending report against a tag |
| `TagBudget` | `tagbudget/value/{id}` | Reward pool scoped to a single tag |
| `TagBudgetAward` | `tagbudgetaward/value/{id}` | Award record emitted from a `TagBudget` |
| `SentinelActivity` | `sentinel/value/{address}` | Generic sentinel record: bond, status, activity stamps |
| `MemberReport` | `memberreport/value/{address}` | Community report against a member (with cosigners, defense) |
| `MemberWarning` | `memberwarning/value/{id}` | Warning issued to a member |
| `GovActionAppeal` | `govactionappeal/value/{id}` | Appeal against a governance action |
| `JuryParticipation` | `jurypart/value/{address}` | Jury service participation record |

### Indexes

| Index | Purpose |
|-------|---------|
| `InitiativesByStatus` | Filter by OPEN/SUBMITTED/IN_REVIEW/COMPLETED/etc. |
| `InterimsByStatus` | Filter by PENDING/ASSIGNED/SUBMITTED/COMPLETED/etc. |
| `JuryReviewsByVerdict` | Filter jury reviews by verdict |
| `StakesByTarget` | All stakes on a specific target (type, id) |
| `ChallengesByStatus` | Filter by ACTIVE/IN_JURY_REVIEW/UPHELD/DISMISSED |
| `ContentChallengesByStatus` | Active/resolved content challenges |
| `ContentChallengesByTarget` | Active challenge per content item (type, id) |
| `ContentInitiativeLinks` | Content → initiative conviction propagation |
| `InitiativesByContent` | Reverse of `ContentInitiativeLinks`; lets a content stake reschedule the initiatives it propagates into |
| `ConvictionQueue` | Due-time-ordered `(due_at, initiative_id)` work list for bounded conviction refresh |
| `ConvictionScheduledAt` | Initiative → its current due time, so a reschedule can replace its queue entry |

### Initiative Status Lifecycle

```
OPEN → ASSIGNED → SUBMITTED → IN_REVIEW → COMPLETED
 ↑        │           │           │
 │        │           │           └── CHALLENGED → REJECTED
 │        │           │
 └────────┴───────────┘   MsgUnassignInitiative (back to OPEN, not terminal)

any live status ─────────→ CLOSED   (project creator or Operations Committee)
```

Releasing an assignment is **not** a terminal state, which is the distinction
worth holding on to. `MsgUnassignInitiative` returns the work item to `OPEN`
with its budget, conviction and stakes intact, so the demand the community
built up survives a change of hands. `MsgCloseInitiative` is the terminal one:
the project side stops funding the work and the reserved budget goes back.

Releasing is self-service from `ASSIGNED`, Operations-Committee-only from
`SUBMITTED` and `IN_REVIEW` (otherwise an assignee could submit, draw reviewer
effort, release and resubmit on a fresh round, minting review fees each lap),
and impossible from `CHALLENGED` in either direction — re-entering through a
new assignee would launder a live challenge. Closing is blocked from
`CHALLENGED` for the same reason.

**Cancelling the parent project** cascade-terminates *every* non-terminal
initiative under it (`OPEN`…`CHALLENGED` → `CLOSED`): each one's reserved
budget is returned, its self-assign bond released, and any active challenge
voided (refunding the challenger's stake in full). See the "Cancelling a
Project" section of [docs/x-rep-spec.md](../../docs/x-rep-spec.md).

## Messages

### Membership

| Message | Description | Access |
|---------|-------------|--------|
| `MsgInviteMember` | Create invitation, lock DREAM stake | Members with invitation credits |
| `MsgAcceptInvitation` | Accept invitation, create new member | Invitee |
| `MsgRegisterZkPublicKey` | Register ZK public key for anonymous operations | Any member |

### DREAM Transfers

| Message | Description | Access |
|---------|-------------|--------|
| `MsgTransferDream` | Tip/gift with purpose validation and rate limiting | Members |

### Projects

| Message | Description | Access |
|---------|-------------|--------|
| `MsgProposeProject` | Propose project with budget and tags | Any member |
| `MsgApproveProjectBudget` | Approve and fund project | Committee authority |
| `MsgCancelProject` | Cancel project with reason | Project creator or council Operations Committee |

### Initiatives

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateInitiative` | Create initiative under project | Any member |
| `MsgAssignInitiative` | Assign to worker (creator self-assign allowed; stricter completion gates apply) | Project authority |
| `MsgSubmitInitiativeWork` | Submit deliverable | Assignee |
| `MsgApproveInitiative` | Record an advisory verdict; disapproval closes the initiative | Approval: any staker or committee. Disapproval: Operations Committee only |
| `MsgSubmitInitiativeReview` | File a bonded reviewer's verdict on submitted work | Bonded initiative reviewer, independent of the work and not a staker on it |
| `MsgFundReviewBounty` | Escrow DREAM against an initiative to attract reviewers | Members |
| `MsgReclaimReviewBounty` | Withdraw your own unpaid bounty (before any verdict) | Funder |
| `MsgSetVerificationPolicy` | Configure how a project's initiatives are reviewed | Project creator or Operations Committee |
| `MsgResolveReviewEscalation` | Settle a review round that hit its deadline | Operations Committee |
| `MsgUnassignInitiative` | Release an assignment; the initiative returns to OPEN, keeping its budget and conviction | Assignee, or Operations Committee for work stalled in review |
| `MsgCloseInitiative` | Retire an initiative outright; returns reserved budget to the project | Project creator or Operations Committee |

Both exits refuse with a registered error rather than a generic failure, and
which one tells the caller what to do next: `ErrUnauthorized` (1304) means the
same call from another signer would succeed (releasing work that is under
review — ask the Operations Committee), while `ErrInvalidInitiativeStatus`
(1402) means no signer can do it from this status (an open challenge must
resolve first; a terminal initiative is already resolved).
| `MsgCompleteInitiative` | Finalize after challenge period, mint rewards | Authority |

Creator self-assignment is allowed but hardened: a raised external-conviction ratio (75% of required, against 50% for externally-assigned work — which is a floor of three independent stakers rather than two, given the 33% per-member conviction cap), extended challenge window, DREAM bond on budget-backed projects (returned on completion or release, burned on upheld challenge), and neither creator nor assignee may approve. See the self-assignment section of [docs/x-rep-spec.md](../../docs/x-rep-spec.md).

### Staking

| Message | Description | Access |
|---------|-------------|--------|
| `MsgStake` | Create conviction/content/author bond stake (max 10 tranches per target) | Members |
| `MsgUnstake` | Partial/full unstake; principal always returned, rewards forfeited before 24h | Stake owner |
| `MsgClaimStakingRewards` | Claim accumulated staking rewards (rejected before 24h) | Stake owner |
| `MsgCompoundStakingRewards` | Re-stake accumulated rewards in place; member/tag stakes only | Stake owner |

### Challenges

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateChallenge` | Challenge initiative work (named or anonymous via x/shield) | Members |
| `MsgRespondToChallenge` | Respond to prevent auto-uphold | Assignee |
| `MsgSubmitJurorVote` | Cast jury vote with verdict and confidence (PENDING reviews only) | Selected juror |
| `MsgSubmitExpertTestimony` | Provide expert context during review | Domain experts |

Challenges resolve to `UPHELD` or `REJECTED` — or `VOIDED` when the parent
project is cancelled out from under the dispute: the challenger's stake is
refunded in full, any pending jury review closes `INCONCLUSIVE`, and the voided
challenge is sealed against late votes and resolution. See the "Cancelling a
Project" section of [docs/x-rep-spec.md](../../docs/x-rep-spec.md).

### Content Challenges

| Message | Description | Access |
|---------|-------------|--------|
| `MsgChallengeContent` | Challenge bonded content | Members |
| `MsgRespondToContentChallenge` | Author responds to challenge | Content author |

### Interims

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateInterim` | Create delegated work | Committee authority |
| `MsgAssignInterim` | Assign to worker | Authority |
| `MsgSubmitInterimWork` | Submit deliverable | Assignee |
| `MsgApproveInterim` | Approve completion | Authority |
| `MsgAbandonInterim` | Abandon assigned interim | Assignee |
| `MsgCompleteInterim` | Finalize, mint rewards, grant reputation | Authority |

### Tag Registry and Moderation

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateTag` | Create a new tag in the shared registry (trust-gated, fee-burned) | ESTABLISHED+ members |
| `MsgReportTag` | Report a tag as problematic | Members |
| `MsgResolveTagReport` | Resolve report (ignore / reserve / remove / restore) | Authority |

### Tag Budgets

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateTagBudget` | Create a reward pool for quality posts with a specific tag | Members |
| `MsgTopUpTagBudget` | Add SPARK to an existing pool | Budget creator |
| `MsgAwardFromTagBudget` | Award SPARK to a forum post's author | Budget creator |
| `MsgToggleTagBudget` | Enable/disable awards without withdrawing | Budget creator |
| `MsgWithdrawTagBudget` | Close pool, return remaining SPARK | Budget creator |

### Bonded-Role Accountability (generic primitive)

| Message | Description | Access |
|---------|-------------|--------|
| `MsgBondRole` | Stake DREAM to register as an accountable role-holder (`role_type` = `ROLE_TYPE_CONTENT_SENTINEL` / `ROLE_TYPE_COLLECT_CURATOR` / `ROLE_TYPE_FEDERATION_VERIFIER`) | Members |
| `MsgUnbondRole` | Withdraw role bond (subject to committed/pending constraints; respects per-role unbonding window; incremental while UNBONDING) | Bonded role-holder |
| `MsgCancelUnbondRole` | Cancel part or all of an in-flight unbond, returning it to active bond without waiting out the cooldown | Bonded role-holder |

DREAM-bonded role primitive only. SPARK-staked roles (e.g. federation bridge operators) keep their own primitives in x/service. See [docs/bonded-role-generalization.md](../../docs/bonded-role-generalization.md).

### Member Accountability

| Message | Description | Access |
|---------|-------------|--------|
| `MsgReportMember` | File a report against a member | Members |
| `MsgCosignMemberReport` | Cosign an existing report (threshold for escalation) | Members |
| `MsgDefendMemberReport` | Reported member submits defense | Reported member |
| `MsgResolveMemberReport` | Apply a resolution (warn / demote / zero / dismiss) | Authority |
| `MsgAppealGovAction` | Appeal a governance action (creates appeal initiative) | Affected member |
| `MsgResolveGovActionAppeal` | Resolve a gov-action appeal (UPHELD / OVERTURNED) | Operations Committee on commons council |

### Parameter Updates

| Message | Description | Access |
|---------|-------------|--------|
| `MsgUpdateParams` | Update all parameters | `x/gov` authority |
| `MsgUpdateOperationalParams` | Update operational parameters | Committee authority |

## Queries

### Core Lookups

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `GetMember` / `ListMember` | Member with lazy decay/reputation applied |
| `MembersByTrustLevel` | Filter by trust level |
| `GetInvitation` / `ListInvitation` | Invitation lookup/list |
| `InvitationsByInviter` | Invitations sent by member |

### Projects and Initiatives

| Query | Description |
|-------|-------------|
| `GetProject` / `ListProject` | Project lookup/list |
| `ProjectsByCouncil` | Projects approved by council |
| `ProjectsByCreator` | Projects proposed by member |
| `GetInitiative` / `ListInitiative` | Initiative lookup/list |
| `InitiativesByProject` | Initiatives under a project |
| `InitiativesByAssignee` | Member's assigned initiatives |
| `InitiativesByCreator` | Initiatives scoped by member |
| `AvailableInitiatives` | Open initiatives to claim |
| `InitiativeConviction` | Current conviction score (time-weighted) |

`Project.creator` and `Initiative.creator` record the address that
submitted the creating message, so authorship is answerable from a node
query rather than an off-chain replay of `project_created` /
`initiative_created` events. Creator is distinct from
`Initiative.assignee` — the member who scoped the work is usually not the
member who takes it on.

The list queries above accept an optional `sort_by` field (`--sort-by` on
the CLI). Project keys: `id`, `name`, `budget`, `status`. Initiative keys:
`id`, `title`, `status`, `budget`, `tier`, `conviction`. Direction follows
`pagination.reverse`; sorted results are offset-paginated (`next_key` is a
decimal offset, not a store key). See the Sorted List Pagination section of
[docs/x-rep-spec.md](../../docs/x-rep-spec.md) for details.

### Staking

| Query | Description |
|-------|-------------|
| `GetStake` / `ListStake` | Stake lookup/list |
| `StakesByStaker` | Stakes placed by member |
| `StakesByTarget` | Stakes on specific target |
| `Reputation` | Member's reputation in a specific tag |

### Challenges

| Query | Description |
|-------|-------------|
| `GetChallenge` / `ListChallenge` | Challenge lookup/list |
| `ChallengesByInitiative` | Challenges on initiative |
| `GetJuryReview` / `ListJuryReview` | Jury review lookup/list |

### Content

| Query | Description |
|-------|-------------|
| `ContentConviction` | Conviction score on content |
| `AuthorBond` | Author bond stake for content |
| `GetContentChallenge` / `ListContentChallenge` | Content challenge lookup/list |
| `ContentChallengesByTarget` | Active challenges on content |
| `ContentByInitiative` | Content linked to initiative |

### Interim Work

| Query | Description |
|-------|-------------|
| `GetInterim` / `ListInterim` | Interim lookup/list |
| `InterimsByAssignee` | Interim work assigned to member |
| `InterimsByType` | Interim work filtered by type |
| `InterimsByReference` | Interim work linked to content |
| `RoleActivity` | A bonded role holder's accountability record (counters, streaks, accuracy ring) |
| `JuryReviewsByJuror` | Jury summons seated on an address |
| `RoleRewardPools` | Funding state of every bonded-role SPARK pool, plus today's community-pool draw |
| `InitiativeReviews` | All rounds' reviewer verdicts on an initiative, plus whether the current round meets the gate |
| `ReviewBounty` | DREAM escrowed against an initiative and when each contribution becomes reclaimable |
| `EscalatedReviews` | Review rounds awaiting an Operations Committee decision |

## Parameters

### Governance-Only (via `MsgUpdateParams`)

These parameters are excluded from `RepOperationalParams` and can only be changed via `x/gov`:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `apprentice_tier` / `standard_tier` / `expert_tier` / `epic_tier` | See table above | Initiative tier definitions (budget, reputation, multiplier) |
| `completer_share` | 90% | Initiative reward to completer |
| `treasury_share` | 10% | Initiative reward to treasury |
| `trust_level_config` | See trust levels table | Trust level thresholds and invitation credits |

### Operational (via `MsgUpdateOperationalParams`)

#### Time Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `epoch_blocks` | uint64 | 14,400 | Blocks per epoch (~1 day) |
| `season_duration_epochs` | uint64 | 150 | Epochs per season (~5 months) |

#### DREAM Economics

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `staking_apy` | LegacyDec | 10% | On staked DREAM |
| `unstaked_decay_rate` | LegacyDec | 1% | Per epoch on unstaked DREAM |
| `transfer_tax_rate` | LegacyDec | 3% | Burned on transfers |
| `max_tip_amount` | Int | 100 DREAM | Per tip |
| `max_tips_per_epoch` | uint64 | 10 | Rate limit |
| `max_gift_amount` | Int | 500 DREAM | Per gift (invitees only) |
| `gift_only_to_invitees` | bool | true | Restrict gifts to invitees |
| `gift_cooldown_blocks` | int64 | 14,400 | Cooldown per recipient (1 day) |
| `max_gifts_per_sender_epoch` | Int | 2,000 DREAM | Total gifts per sender per epoch |

#### Conviction

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `conviction_per_dream` | LegacyDec | 0.2 | Sqrt scaling factor |
| `conviction_half_life_epochs` | uint64 | 7 | Exponential decay rate |
| `external_conviction_ratio` | LegacyDec | 50% | Required from non-affiliated stakers |
| `max_conviction_share_per_member` | LegacyDec | 33% | Prevents single-member dominance |

#### Self-Assignment Safeguards (creator-assigned initiatives)

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `self_assigned_bond_rate` | LegacyDec | 10% | Of budget, locked as DREAM bond on budget-backed projects; burned on upheld challenge |
| `self_assigned_external_conviction_ratio` | LegacyDec | 75% | Replaces `external_conviction_ratio` when assignee == project creator. Divided by `max_conviction_share_per_member`, it is also the floor on independent stakers: 3 |
| `self_assigned_challenge_multiplier` | int64 | 2 | Challenge-window multiplier for creator-assigned initiatives |

#### Challenges

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `min_challenge_stake` | Int | 50 DREAM | Minimum to file challenge |
| `challenger_reward_rate` | LegacyDec | 20% | Of initiative budget |
| `jury_size` | uint32 | 5 | Odd, and must exceed the seated-jury floor (3) |
| `jury_super_majority` | LegacyDec | 67% | To uphold/reject |
| `min_juror_reputation` | uint64 | 50 | Reputation required to serve |
| `juror_reward_rate` | LegacyDec | 25% | Of the disputed budget, split across seats |
| `min_juror_reward` | Int | 5 DREAM | Pay floor; the whole rate for content challenges and appeals, which have no budget |
| `abandoned_jury_seat_penalty` | LegacyDec | 10 | Reputation charged for abandoning an accepted seat |
| `jury_acceptance_window_ratio` | LegacyDec | 25% | Of the review period; how long to answer a summons |
| `max_jury_redraws` | uint32 | 1 | Replacement rounds per review; validated against the window |
| `min_juror_selection_weight` | LegacyDec | 0.1 | Floor on the responsiveness draw multiplier |
| `min_jury_seatings_for_weighting` | uint64 | 3 | Seatings before responsiveness applies |
| `challenge_response_deadline_epochs` | uint64 | 3 | Auto-uphold if no response |
| `max_active_challenges_per_committee` | uint64 | 3 | Rate limit |
| `max_new_challenges_per_epoch` | uint64 | 2 | Rate limit |
| `challenge_queue_max_size` | uint64 | 10 | Queue size limit |

#### Initiative Review

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `reviewer_bond_reserve_rate` | LegacyDec | 10% | Of the initiative budget, committed per verdict and slashed on overturn |
| `review_fee_rate` | LegacyDec | 5% | Of the budget, x the tier multiplier, split across the reviewers who filed |
| `review_required_above_budget` | Int | 100 DREAM | Above this, completion needs a verdict regardless of project policy |
| `review_bounty_reclaim_delay` | uint64 | 14400 (~1 day) | Blocks before a funder may reclaim an unpaid bounty |
| `permissionless_min_review_bounty_rate` | LegacyDec | 10% | Of budget, escrowed in existing DREAM at permissionless creation — only when the budget is above the review gate |
| `max_review_rounds` | uint32 | 3 | Rejection returns the work for another round; the last one closes the initiative |
| `min_reviewer_bond` | Int | 500 DREAM | Bond floor to hold the role; a low entry price, not the per-verdict exposure |
| `reviewer_demotion_threshold` | Int | 250 DREAM | Free bond below which the role is demoted; half the floor |
| `min_reviewer_trust_level` | string | `TRUST_LEVEL_ESTABLISHED` | Eligibility gate; required (empty would disable the check) |
| `min_reviewer_rep_tier` | uint64 | 0 | Off — the trust ladder already encodes reputation |
| `min_reviewer_age_blocks` | int64 | 0 | Off — no bonded-age wait before reviewing |
| `reviewer_demotion_cooldown` | int64 | 604800 (7 days) | Seconds a demoted reviewer waits before re-bonding |
| `reviewer_unbond_cooldown` | int64 | 1209600 (14 days) | Seconds bond stays locked and slashable after unbonding |
| `max_reviewer_reward_pool` | Int | 150,000 SPARK | Cap on the reviewer SPARK pool; 1.5x sentinel/curator |
| `reviewer_reward_pool_overflow_burn_ratio` | LegacyDec | 50% | Fraction of overflow burned each epoch |
| `reviewer_reward_epoch_blocks` | uint64 | 14400 (~1 day) | Distribution cadence |
| `min_reviewer_accuracy` | LegacyDec | 0.70 | Windowed accuracy needed to earn a pool share |
| `reviewer_accuracy_window_epochs` | uint64 | 6 | Epochs of history the accuracy ring scores |
| `max_curator_reward_pool` | Int | 100,000 SPARK | Cap on the curator SPARK pool; equal to the sentinel pool |
| `curator_reward_pool_overflow_burn_ratio` | LegacyDec | 50% | Fraction of overflow burned each epoch |
| `curator_reward_epoch_blocks` | uint64 | 14400 (~1 day) | Distribution cadence |
| `min_curator_accuracy` | LegacyDec | 0.70 | Windowed accuracy needed to earn a pool share |
| `curator_accuracy_window_epochs` | uint64 | 6 | Epochs of history the accuracy ring scores |
| `max_verifier_reward_pool` | Int | 100,000 SPARK | Cap on the federation-verifier SPARK pool; equal to the sentinel pool |
| `verifier_reward_pool_overflow_burn_ratio` | LegacyDec | 50% | Fraction of overflow burned each epoch |
| `verifier_reward_epoch_blocks` | uint64 | One federation `challenge_window` (100800 mainnet, 43200 testnet, 1200 devnet, 10 testparams) | Distribution cadence. Its own dial rather than the sentinel's — see [Federation Verifier Pay](#federation-verifier-pay) |
| `min_verifier_accuracy` | LegacyDec | 0.80 (0 in testparams) | Windowed accuracy needed to earn a pool share; higher than the sentinel/curator 0.70 because a hash mismatch is objective |
| `verifier_accuracy_window_epochs` | uint64 | 6 | Epochs of history the accuracy ring scores |
| `min_epoch_verifications` | uint32 | 3 (1 in testparams) | Per-epoch verification floor. A floor, never a weight — see below |
| `verifier_dream_reward` | Int | 5 DREAM | Flat per-eligible-verifier stipend, minted alongside the SPARK share |
| `max_verifier_dream_mint_per_epoch` | Int | 100 DREAM (7 in testparams) | Cap on total DREAM minted per epoch; eligible verifiers scale down pro-rata |
| `role_reward_inflation_share` | LegacyDec | 0.5 | Share of the community pool's inflation income drawn per UTC day, split across all bonded-role pools by headroom; 0 disables |
| `initiative_completion_bonus_rate` | LegacyDec | 10% | Of the budget, to external stakers on completion |

Per-project review is configured by `MsgSetVerificationPolicy`;
`min_verifier_count` 0 (the genesis default) is conviction-only.

#### Content Conviction

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `content_conviction_half_life_epochs` | uint64 | 14 | Slower than initiatives |
| `max_content_stake_per_member` | Int | 10,000 DREAM | Per content item |
| `max_author_bond_per_content` | Int | 1,000 DREAM | Bond cap |
| `author_bond_slash_on_moderation` | bool | true | Slash bonds on moderation |
| `content_challenge_reward_share` | LegacyDec | 50% | Minted to successful challenger |
| `conviction_propagation_ratio` | LegacyDec | 10% | Content → initiative conviction |

#### Review Periods

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `default_review_period_epochs` | uint64 | 7 | Initiative review window |
| `default_challenge_period_epochs` | uint64 | 7 | Post-review challenge window |

#### Invitations

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `min_invitation_stake` | Int | 100 DREAM | Min stake per invitation |
| `invitation_accountability_epochs` | uint64 | 150 | Accountability period (~1 season) |
| `referral_reward_rate` | LegacyDec | 5% | Inviter receives from invitee earnings |
| `invitation_cost_multiplier` | LegacyDec | 1.1x | Cost increase per additional invitation |
| `invitation_stake_burn_rate` | LegacyDec | 10% | Burned on acceptance |

#### Extended Staking

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `project_staking_apy` | LegacyDec | 8% | While project active |
| `project_completion_bonus_rate` | LegacyDec | 5% | On project completion |
| `member_stake_revenue_share` | LegacyDec | 5% | Revenue share to member stakers |
| `tag_stake_revenue_share` | LegacyDec | 2% | Per tag revenue share |
| `min_stake_duration_seconds` | int64 | 86,400 | 24 hours minimum |
| `allow_self_member_stake` | bool | false | Cannot self-stake |

#### Interim Work

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `simple_complexity_budget` | Int | 50 DREAM | Simple task compensation |
| `standard_complexity_budget` | Int | 150 DREAM | Standard task compensation |
| `complex_complexity_budget` | Int | 400 DREAM | Complex task compensation |
| `expert_complexity_budget` | Int | 1,000 DREAM | Expert task compensation |
| `solo_expert_bonus_rate` | LegacyDec | 50% | Bonus for solo expert work |
| `interim_deadline_epochs` | uint64 | 7 | Deadline in epochs |

#### Slashing

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `minor_slash_penalty` | LegacyDec | 5% | Minor infraction |
| `moderate_slash_penalty` | LegacyDec | 15% | Moderate infraction |
| `severe_slash_penalty` | LegacyDec | 30% | Severe infraction |
| `zeroing_slash_penalty` | LegacyDec | 100% | Complete zeroing |

#### Anti-Gaming

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `reputation_decay_rate` | LegacyDec | 0.5% | Per epoch |
| `max_reputation_gain_per_epoch` | uint64 | 50 | Per tag |
| `max_tags_per_initiative` | uint64 | 3 | Prevents tag stuffing |
| `min_reputation_multiplier` | LegacyDec | 10% | Floor for reputation-based calculations |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Address codec, account lookups |
| `x/bank` | Yes | DREAM token operations, SPARK transfers |
| `x/commons` | Yes | Committee/council authorization checks |
| `x/season` | No | Current season number for reputation resets |
| `x/forum` | No | Narrow `ForumKeeper` surface (`PruneTagReferences`, `GetPostAuthor`, `GetPostTags`) for tag moderation + tag-budget award validation; x/rep now owns tag storage itself |

### Shield-Aware Messages

Only `MsgCreateChallenge` is shield-compatible, enabling anonymous challenge creation via `x/shield`'s `MsgShieldedExec`.

### Cyclic Dependency Breaking

Cross-module keepers are wired manually in `app.go` via shared `lateKeepers` struct:
- `SetForumKeeper()` — rep tag-moderation / tag-budget flow calls back into forum for post lookup + tag pruning
- `SetSeasonKeeper()` — season ↔ rep cycle

## EndBlocker

1. **Drain the conviction queue** — recompute conviction for initiatives whose refresh is due, capped at `MaxConvictionStakeUpdatesPerBlock` (500) stake-level updates per block, with leftover work rolling to later blocks
2. **Check completion thresholds** for submitted initiatives
3. **Finalize unchallenged** initiatives after challenge period expires (run in a cache context so a mid-payout error cannot persist partial mints)
4. **Process expired challenge responses** (auto-uphold if no response by deadline)
5. **Process expired content challenge responses**
6. **Tally jury review votes** when deadline reached
7. **Process interim deadlines** (expire if deadline passes)
8. **Distribute epoch staking rewards** from the seasonal pool — gated on `IsEpochEnd`
9. **Process invitation accountability** (slash inviters if invitee zeroed)
10. **Distribute bonded-role pay** — sentinel, initiative reviewer, collect
    curator and federation verifier, each on its own `*_reward_epoch_blocks`
    cadence and each resetting only its own role's per-epoch counters
11. **Rebuild member trust tree** if dirty (for `x/shield` ZK proofs)

Lazy operations (applied on-demand via `GetMember()`):
- DREAM decay, reputation decay, invitation credit resets, trust level updates

## Events

All state-changing operations emit typed events for indexing and client notification.

## Client

### CLI

```bash
# Membership
sparkdreamd tx rep invite-member [invitee] [stake] --from alice
sparkdreamd tx rep accept-invitation [invitation_id] --from bob
sparkdreamd tx rep register-zk-public-key [hex_key] --from alice

# Initiatives
sparkdreamd tx rep create-initiative [project_id] --title "..." --tier STANDARD --from alice
sparkdreamd tx rep submit-initiative-work [initiative_id] --deliverable-uri "..." --from bob
sparkdreamd tx rep stake initiative [initiative_id] [amount] --from carol

# Challenges
sparkdreamd tx rep create-challenge [initiative_id] --reason "..." --stake 100 --from dave

# Staking rewards
sparkdreamd tx rep claim-rewards --from alice

# Queries
sparkdreamd q rep get-member [address]
sparkdreamd q rep initiative-conviction [initiative_id]
sparkdreamd q rep reputation [address] [tag]
sparkdreamd q rep params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/rep/v1/query.proto` for the full API surface.
