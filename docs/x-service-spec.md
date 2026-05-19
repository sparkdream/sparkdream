# Technical Specification: `x/service`

## 1. Abstract

The `x/service` module provides a generic, SPARK-staked accountability primitive for **off-chain agents performing work the chain cannot natively verify**. It exists because some council-funded work has to happen on systems outside the chain — funding an Akash deployment, maintaining a Filecoin or Jackal storage pinning agreement, operating a federation bridge daemon, paying for an external RPC — and the chain needs a uniform way to (a) hold an operator accountable, (b) slash misbehavior, and (c) reuse a single audit and dispute pipeline across all such services.

Key principles:

- **Service-agnostic.** No hardcoded knowledge of any particular external system. Service types are free-form strings gated by a governance allowlist. Service-specific configuration (target deployment ID, storage CID set, bridge endpoint, etc.) is opaque metadata to the module.
- **SPARK-bonded, not DREAM-bonded.** External-service operators bond SPARK because they are paid in SPARK (via `MsgScheduleRecurringSpend`) and because the work they perform incurs SPARK-denominated costs to the chain when it fails (a missed Akash top-up costs SPARK to recover, not DREAM). DREAM-bonded roles ([x/rep `BondedRole`](x/rep/types/bonded_role.proto)) remain the right primitive for in-system moderation/curation; `x/service` complements rather than replaces it.
- **Two-tier slashing.** The hiring council can slash up to a small cap unilaterally (routine SLA enforcement). Larger slashes require an x/rep jury (significant accusations, prevents controller capture).
- **No payment primitive.** Operators are paid via the existing `MsgScheduleRecurringSpend` in `x/commons` targeted at the operator's address. `x/service` owns *accountability*, not compensation.
- **Reusable by federation.** `x/federation`'s bridge-operator concept is implemented on top of this primitive (one service_type per bridge protocol: `federation-bridge-activitypub`, `federation-bridge-atproto`) rather than duplicating it. Federation no longer hosts its own bond/slash/unbond state.

---

## 2. Dependencies

| Module | Purpose |
|--------|---------|
| `x/bank` | SPARK bond escrow, slash transfer to community pool |
| `x/commons` | Controller authorization (a council is typically an operator's controller); recurring payment via `MsgScheduleRecurringSpend` |
| `x/rep` | Jury system for tier-2 slash adjudication; reputation feedback in the `service-operator` tag |
| `x/session` | Operator key delegation to hot-wallet session keys for routine on-chain ops |
| `x/gov` | Governance authority for the service-type allowlist and per-type bond minimums |
| `x/distribution` | Slashed SPARK is sent to the community pool |

**Depended on by:** `x/federation` (for bridge-operator accountability). Otherwise a leaf module.

---

## 3. Core Concepts

### 3.1. Operators

An `Operator` is a triple `(address, service_type, controller)` plus a SPARK bond:

```
Operator {
  address                    // bech32, Spark Dream address (the wallet doing the work)
  service_type               // free-form string from the governance allowlist (e.g. "akash-funding")
  controller                 // x/commons Group address that hired this operator (must NOT equal address)
  bond                       // SPARK currently locked (in module account, not yet escrowed for contest)
  metadata                   // opaque bytes — service-specific config (e.g. "{\"akash_dseq\": \"12345\"}")
  status                     // ACTIVE | UNDERFUNDED | UNBONDING | SLASHED | RETIRED
  underfunded_since          // height at which bond first fell below current min_bond (0 if never)
  unbond_complete_at         // height at which UNBONDING completes (0 if not unbonding)
  tier1_slashed_in_window    // sum of tier-1 slashes (SPARK) within rolling tier1_window_blocks
  tier1_window_start         // height the current rolling window started
  tier1_window_start_bond    // bond snapshot at window open; aggregate-cap denominator (§3.4.3)
  registered_at
  retired_at                 // height the record transitioned to RETIRED (0 if not retired)
  total_lifetime_bond_blocks // running sum, for reputation accrual (see §6.6) — updated lazily
  last_bond_block_update_at  // height of last total_lifetime_bond_blocks update; used by lazy accrual
}
```

**Status semantics:**

| Status        | Meaning                                                          | Reportable? | Can top-up? | In live store?   |
|---------------|------------------------------------------------------------------|-------------|-------------|------------------|
| `ACTIVE`      | Bond ≥ min_bond, eligible for service                            | Yes         | Yes         | `Operators`      |
| `UNDERFUNDED` | Bond fell below min_bond, in grace period to top up              | Yes         | Yes         | `Operators`      |
| `UNBONDING`   | Operator initiated exit (or forced); clock running               | Yes         | No          | `Operators`      |
| `SLASHED`     | Terminal. Dissolved by jury, remaining bond confiscated          | No (no bond)| No          | `ArchivedOperators` |
| `RETIRED`     | Terminal. Voluntary exit completed (`MsgClaimUnbondedBond`)      | No          | No          | `ArchivedOperators` |

An operator is **discoverably active** (`IsActiveOperator(addr, service_type)` returns true) iff status is `ACTIVE`. Off-chain consumers (relayer software, monitoring dashboards) MUST check this flag before treating an address as a working operator — `UNDERFUNDED` operators are still on-chain records but represent a failing state.

**Underfunded grace.** If a slash drops `bond` below the current `min_bond`, status auto-transitions to `UNDERFUNDED` and `underfunded_since` is set. The operator has `underfunded_grace_blocks` (default 7 days) to either `MsgTopUpBond` back to compliance (returns to `ACTIVE`) or be force-unbonded by the EndBlocker sweep (transitions to `UNBONDING` with the standard unbonding clock). See §3.6.

**Terminal states and the live/archive split.** `SLASHED` and `RETIRED` records are removed from the live `Operators` store and moved to `ArchivedOperators` (key: `(address, service_type, retired_at)`) at the terminal transition. **Archived records always have `bond = 0`**: RETIRED records have had their bond returned to the operator at claim; SLASHED records have had remaining bond transferred to the community pool at dissolution. The archive is for audit history (who held what role, when, with what fate), not active SPARK accounting. This frees the live `(address, service_type)` key for re-registration if the address chooses to enter again. Two consequences:

1. **`SLASHED` blocks re-registration of the same `(address, service_type)` pair.** The keeper checks `ArchivedOperators` for any SLASHED record before allowing a new registration. The address MAY register the same service_type from a *different* address, or a *different* service_type from the same address.
2. **`RETIRED` does NOT block re-registration.** A voluntarily-exited operator can re-enter the same `(address, service_type)` by submitting a fresh `MsgRegisterOperator`. The prior `RETIRED` record stays in archive for audit; the new live record starts with a **fresh bond and ALL counters reset to zero or current height** — `tier1_slashed_in_window`, `tier1_window_start`, `tier1_window_start_bond`, `underfunded_since`, `unbond_complete_at`, `total_lifetime_bond_blocks`, `last_bond_block_update_at`, `retired_at` are all zero/current at registration, and the `Tier1LastSlash` history for `(any controller, address, service_type)` was already pruned at the prior archive (§4.1). The new operator inherits no state from prior incarnations.

**Multiple concurrent registrations.** A single address can register as multiple operators if it provides multiple service types — but each `(address, service_type)` pair has at most one live record at a time. Service-type rows are not unique by controller; one council can have multiple operators of the same type (e.g., redundancy across operators).

### 3.2. Service Type Registry

A governance-managed allowlist of permitted service types:

```
ServiceTypeConfig {
  service_type                // string key, e.g. "akash-funding"; [a-z0-9-]{1,64}
  description                 // human-readable purpose; <= 512 chars
  min_bond                    // minimum SPARK to register or remain ACTIVE
  unbonding_period_blocks     // duration in blocks; defaults to ~14 days
  unilateral_slash_cap_bps    // per-slash cap; default 500 = 5%
  tier1_window_blocks         // rolling window for aggregate tier-1 cap; default ~90 days
  tier1_aggregate_cap_bps     // max cumulative tier-1 slash within window; default 1500 = 15%
  tier1_cooldown_blocks       // min interval between tier-1 slashes against same operator; default ~7 days
  underfunded_grace_blocks    // time to top up after dropping below min_bond; default ~7 days
  enabled                     // governance can disable without disturbing existing operators
  report_timeout_action       // ReportTimeoutAction enum: DISMISS (default) | ESCALATE — drives EndBlocker auto-action when a PENDING report ages past max_pending_blocks (§3.4.5)
  challenge_default_slash_bps // proposed slash (basis points) attached to system reports opened via OpenSystemReport when the caller passes slash_bps=0; capped at unilateral_slash_cap_bps via cross-field validation. Controllers can still adjust within the cap at MsgResolveReport time.
}
```

Adding, modifying, or disabling a service type is done via `MsgUpdateServiceTypeConfig`, authority = x/gov.

**Validation rules** (enforced in `ValidateBasic` and in the keeper before write):

- `service_type` matches `^[a-z0-9-]{1,64}$`; cannot be modified once created (only `description` / numeric fields / `enabled` / `report_timeout_action` / `challenge_default_slash_bps` can change).
- `min_bond > 0` in `uspark`.
- `unilateral_slash_cap_bps`, `tier1_aggregate_cap_bps` each in `(0, 10000]`.
- `tier1_aggregate_cap_bps ≥ unilateral_slash_cap_bps` (otherwise one tier-1 slash could exceed the aggregate cap).
- `unbonding_period_blocks ≥ report_contest_window_blocks` (otherwise an operator could unbond before they could contest).
- `tier1_cooldown_blocks > 0` (a value of zero would re-enable the drainage attack the cap is designed to prevent).
- `report_timeout_action ∈ {REPORT_TIMEOUT_ACTION_DISMISS, REPORT_TIMEOUT_ACTION_ESCALATE}`.
- `challenge_default_slash_bps ≤ unilateral_slash_cap_bps`, enforced **both directions** on every `MsgUpdateServiceTypeConfig`: raising the default above the cap is rejected, and lowering the cap below the existing default is also rejected. Governance must therefore split a cap-lowering update into two proposals (lower the default first, then the cap), or set both fields in a single update. Prevents the "lower the cap, default silently exceeds it" state.

**Grandfathering & raised minimums.** When governance raises `min_bond` for an existing service type via `MsgUpdateServiceTypeConfig`:

1. Existing operators with `bond ≥ new_min_bond` are unaffected.
2. Operators now below `new_min_bond` transition to `UNDERFUNDED` and gain `underfunded_grace_blocks` to top up via `MsgTopUpBond`.
3. After the grace period, the EndBlocker sweep moves them to `UNBONDING` (forced unbond). They retain their bond, just lose ACTIVE eligibility.
4. New registrations under the type must meet the new minimum.

Governance can never *lower* `min_bond` below `1 uspark` and SHOULD avoid sudden 10×+ raises (would mass-force-unbond legitimate operators). The intent is gradual calibration, not retroactive eviction.

**Disabled-service-type semantics.** When `enabled = false`:

| Operation                  | Allowed against an existing operator of a disabled type? |
|----------------------------|----------------------------------------------------------|
| New `MsgRegisterOperator`  | No                                                       |
| `MsgTopUpBond`             | Yes                                                      |
| `MsgUpdateMetadata`        | Yes                                                      |
| `MsgUnbondOperator`        | Yes                                                      |
| `MsgClaimUnbondedBond`     | Yes                                                      |
| `MsgReportOperator`        | Yes                                                      |
| `MsgResolveReport` (T1/T2) | Yes                                                      |

The intent of disabling a type is "stop new entrants" while letting the existing population wind down or continue under accountability. Disabling does not slash, does not force unbond, does not freeze metadata updates. Governance can re-enable later.

**Why free-form strings?** Hardcoded enums create cross-module coupling: every new service type would need a proto bump. Free-form strings + governance allowlist gives flexibility (any future module can propose a new type) without spam risk (gov must approve).

**Why per-type bond minimums?** A storage-pinning operator handling 100 GB needs less skin in the game than an Akash funder routing 50K SPARK/month through their wallet. Per-type minimums let governance calibrate.

### 3.3. Controller

The `controller` field is the address that hired the operator. It MUST be:

- An x/commons `Group` address (a registered council/committee, verified at registration via `commonsKeeper.IsGroupAddress(ctx, controller)`).
- **Not** equal to the operator's own `address` (`controller != creator` enforced at registration).

These two constraints together close the self-controller bypass: an operator cannot register themselves as their own controller (would shield them from all controller-tier accountability), and they cannot use a friend's individual wallet (which would let them rotate "controllers" arbitrarily without on-chain governance review). A real council with its own membership and voting rules is the only valid controller.

The controller is set at registration and immutable except via:

- **Operator-initiated handover:** operator unbonds (full `unbonding_period_blocks`), then re-registers under a new controller. Forces a cooling-off period; prevents instant defection.
- **Jury override (`MsgOpenControllerTransferCase` → `MsgFinalizeControllerTransfer`):** see §5.4 — used when the controller council is dissolved, captured, or itself slashed and the operator needs reassignment.

**Dissolved-controller detection.** A controller can become inert in three ways: (a) its underlying `Group` is deleted, (b) its membership drops to zero, or (c) its decision policy is updated such that no proposal can ever pass. None of these are detected by x/service — the keeper only knows that `MsgResolveReport` and `MsgScheduleRecurringSpend` from that address stop arriving. To prevent operators from being stranded:

- The **operator** themselves can open a controller-transfer jury case via `MsgOpenControllerTransferCase` (most natural — they have skin in the game). See §5.4 for the trigger surface.
- Any **member of x/commons** (passing `min_reporter_trust_level`) can open one, subject to the same `report_deposit` as `MsgReportOperator` (anti-spam).

The controller has two powers:

1. Submit `MsgScheduleRecurringSpend` (existing x/commons surface) targeted at the operator's address. This is how the operator gets paid.
2. Slash up to `unilateral_slash_cap_bps` of the operator's bond via `MsgResolveReport` (see §3.4), subject to the aggregate cap and cooldown, without invoking the jury.

The controller has no power to seize the operator's bond fully, force operator handover, or unilaterally alter metadata. The operator is not the council's vassal — they are a contractor with a slashable performance guarantee.

### 3.4. Slashing & Reports

#### 3.4.1. Two-tier model

**Tier 1: Controller-resolved (routine).** Anyone meeting `min_reporter_trust_level` and posting `report_deposit` (see below) can file `MsgReportOperator`. The controller can resolve the report via `MsgResolveReport` with a slash up to `unilateral_slash_cap_bps` of *current* bond, subject to the aggregate cap and cooldown below. Use case: missed periodic deposit, late response, minor SLA breach.

**Tier 2: Jury-resolved (significant).** If the controller proposes a slash exceeding `unilateral_slash_cap_bps`, or the operator contests a tier-1 slash within the contest window, the report escalates to an x/rep jury. Use case: outright fraud (operator pocketed funds and disappeared), gross negligence (deployment dropped), or controller-operator dispute.

The jury verdict (returned by x/rep via `MsgResolveReportByJury`) is one of:

- **Accept** — slash up to the requested amount; optionally `dissolve = true` to transition the operator directly to `SLASHED` (terminal) regardless of remaining bond. Reporter's deposit is refunded.
- **Reduce** — slash less than requested; reporter's deposit is refunded.
- **Reject** — dismiss the report; reporter's deposit is forfeited to community pool.

#### 3.4.2. Slash math

`slash_bps` is always interpreted as basis points of the operator's **current bond at the moment of slash resolution**, not of original/registered bond. Compounding semantics:

```
slash_amount = floor(current_bond * slash_bps / 10000)
new_bond     = current_bond - slash_amount
```

A 5% slash on a 1000-SPARK bond removes 50 SPARK (leaving 950); a subsequent 5% slash removes 47.5 (truncated to 47, leaving 903). This guarantees no slash can ever exceed remaining bond and that repeated small slashes asymptote rather than zero out instantly.

#### 3.4.3. Aggregate tier-1 cap (drainage protection)

Per service type, governance configures a rolling-window aggregate cap. For each operator:

- `tier1_slashed_in_window` tracks the SPARK total of all tier-1 slashes that have landed since `tier1_window_start`.
- The window slides: when the keeper processes a new tier-1 slash, if `current_height >= tier1_window_start + tier1_window_blocks`, the window resets (`tier1_window_start = current_height`, `tier1_slashed_in_window = 0`) **before** the new slash is applied.
- A tier-1 resolution is rejected (`ErrTier1AggregateCapExceeded`) if `tier1_slashed_in_window + slash_amount > bond_at_window_start * tier1_aggregate_cap_bps / 10000`. The controller must escalate to tier-2 (jury) for additional slashing within the window.

**Bond-at-window-start** is snapshotted when the window opens — this prevents the controller from gaming the cap by repeatedly slashing as bond decreases (each new slash measured against original would otherwise allow more aggregate damage).

#### 3.4.4. Tier-1 cooldown

A controller cannot apply a tier-1 slash to the same operator more than once per `tier1_cooldown_blocks`. The keeper enforces this by tracking the height of the last tier-1 slash per `(controller, operator, service_type)` and rejecting earlier resolutions with `ErrTier1CooldownActive`. Reports filed during a cooldown still accumulate; the controller must wait, then resolve, OR escalate the latest report to tier-2 (jury bypasses cooldown).

#### 3.4.5. Report lifecycle and max-pending age

```
PENDING --(controller resolves T1, no contest within window)--> RESOLVED_T1
PENDING --(controller proposes T2)--> ESCALATED
PENDING --(no resolution within max_pending_blocks, config.report_timeout_action=DISMISS)--> AUTO_DISMISSED
PENDING --(no resolution within max_pending_blocks, config.report_timeout_action=ESCALATE)--> ESCALATED (auto-jury opened)
PENDING --(operator dissolved)--> CLOSED_OPERATOR_DISSOLVED
RESOLVED_T1 --(operator contests within report_contest_window)--> ESCALATED
ESCALATED --(jury verdict)--> RESOLVED_T2
ESCALATED --(no jury verdict within max_escalated_blocks)--> AUTO_TIMEOUT
ESCALATED --(operator dissolved)--> CLOSED_OPERATOR_DISSOLVED
```

- `max_pending_blocks` (default ~30 days) bounds how long a report can sit unresolved. After expiry, the EndBlocker consults the operator's `ServiceTypeConfig.report_timeout_action`:
  - **`DISMISS`** (default): the report auto-dismisses with reporter's deposit **refunded** (not the reporter's fault the controller didn't engage). A `report_auto_dismissed` event is emitted, and a `RefileCooldowns` entry blocks the same controller from re-filing the same allegation within `report_refile_cooldown_blocks` (default ~30 days) — prevents the "withhold then re-file forever" attack on bond return.
  - **`ESCALATE`**: the report transitions to `ESCALATED` and an x/rep jury case is opened automatically (the proposed slash is `report.proposed_slash_bps` if set, otherwise falls back to `config.challenge_default_slash_bps`). Used by `federation-bridge-*` service types so a silent or captured controller cannot stall a slash past one timeout window. The threat of auto-escalation also disciplines controllers — they have a strict deadline to either resolve or relay the dispute to jury themselves. If neither a usable `proposed_slash_bps` nor `challenge_default_slash_bps` is available, the EndBlocker falls back to `DISMISS` behavior for that report (no slash can be proposed in good faith without one).
- `max_escalated_blocks` (default ~60 days) bounds how long an ESCALATED report can wait for a jury verdict. If exceeded, the EndBlocker auto-resolves as if the jury had returned REJECT: any contested-T1 escrow returns to operator's bond, reporter/opener deposit is refunded, status becomes `AUTO_TIMEOUT` (distinct from `RESOLVED_T2` so audit can distinguish jury-decided rejections from inability-to-decide). Treats jury inaction as inability-to-convict — consistent with the no-bounty design (reporters shouldn't be punished for x/rep being unable to produce a verdict). The window is intentionally longer than `max_pending_blocks` (juries take time to deliberate).
- A contested tier-1 slash is **fully reverted** by returning the escrowed slash amount (§3.4.7) to the operator's `bond`; `tier1_slashed_in_window` is decremented. The jury verdict re-applies whatever slash it concludes (from the same escrow if still within window, otherwise from `bond` directly).
- A report against an `UNBONDING` operator is allowed and **pauses the unbonding clock** until resolved (see §3.5).

#### 3.4.6. Report submission deposit & reporter rate-limit

`MsgReportOperator` requires `report_deposit` SPARK (a param, default 10 SPARK) escrowed at filing.

**Per-reporter rate-limit.** A single reporter may file at most `max_reports_per_reporter_per_operator_per_window` reports (default 3) against the same `(operator, service_type)` within the most recent `reporter_rate_limit_window_blocks` (default ~30 days). Exceeding this triggers `ErrReporterRateLimitExceeded`. The counter resets per operator (so an industrious reporter can still file across many operators), but cannot be used to flood-file against a single target.

**Window is sliding, not fixed.** The keeper stores up to `max_reports_per_reporter_per_operator_per_window + 1` filing-height timestamps in a small ring buffer per `(reporter, operator, service_type)` and admits a new filing iff fewer than `max_reports...` of those timestamps fall within `[current_height - reporter_rate_limit_window_blocks, current_height]`. This is strictly stricter than a fixed window: there's no boundary-burst (filing 3 at the end of one window then 3 at the start of the next is rejected by a sliding window). The ring buffer caps storage at a small constant; older entries are overwritten on each new filing past the cap.

**Reporter not in controller group.** The reporter's address MUST NOT be a member of the operator's controller `Group` at filing time, checked via `commonsKeeper.IsGroupMember(ctx, reporter, operator.controller)`. This prevents the controller from routing reports through its own members for sustained drainage — the controller can still file via a non-member ally, but coordination across two parties increases visibility and slows the attack.

**Deposit outcomes:**

| Outcome                                     | Deposit goes to |
|---------------------------------------------|-----------------|
| RESOLVED_T1 (any slash > 0)                 | Reporter        |
| RESOLVED_T1 (slash = 0, controller dismiss) | Reporter        |
| RESOLVED_T2 verdict Accept or Reduce        | Reporter        |
| RESOLVED_T2 verdict Reject                  | Community pool  |
| AUTO_DISMISSED (controller stall)           | Reporter        |

All filings count toward the rate-limit at submission time regardless of outcome — the rate-limit is on filing velocity, not on success rate.

Reporter is paid no bounty (no fraction of slashed bond) — bounties bias toward speculative reports. Deposit is purely anti-griefing: legitimate reports cost nothing, frivolous reports lose the deposit on jury rejection.

#### 3.4.7. Tier-1 slash escrow (contest reversibility)

When a tier-1 slash resolves, the slashed SPARK is **not** immediately transferred to the community pool — it would be irrecoverable if the operator contests. Instead:

1. At `MsgResolveReport` (T1_SLASH) time: SPARK is moved from `bond` to a dedicated **tier-1 escrow** sub-pool inside the module account, tagged with `(report_id, release_at = current_height + report_contest_window_blocks)`.
2. If the operator contests within the window: escrowed SPARK is returned to `bond` and the report transitions to `ESCALATED`.
3. If the contest window passes uncontested: the EndBlocker tier-1 escrow sweep (§3.6) transfers the escrowed SPARK to the community pool via `FundCommunityPool` and removes the escrow entry.

Tier-2 jury slashes (`MsgResolveReportByJury`) skip the escrow entirely and transfer directly to the community pool — jury verdicts are final and not contestable.

The module account balance therefore decomposes into four pools tracked separately (see §4):

- **Bond pool** — sum of `Operator.bond` across all live records.
- **Report deposit pool** — sum of `Report.deposit` across PENDING + ESCALATED reports.
- **Tier-1 escrow pool** — sum of escrow entries in `Tier1Escrow` store.
- **Controller-transfer case deposit pool** — sum of `ControllerTransferCases[].deposit` across open cases (escrowed at `MsgOpenControllerTransferCase`, released at `MsgFinalizeControllerTransfer` — §5.4).

#### 3.4.8. Slashed funds destination

Slashed SPARK is transferred to the community pool via `x/distribution.FundCommunityPool` — after the contest window for tier-1, immediately for tier-2 and dissolution. The controller council never receives slash proceeds (eliminates incentive to slash for revenue — see §16 Design Notes).

#### 3.4.9. SLASHED operator handling

When a tier-2 jury verdict carries `dissolve = true`, OR when `current_bond` after a slash reaches zero:

- Status transitions to `SLASHED` (terminal).
- Any remaining bond is transferred to community pool.
- Live record is moved from `Operators` to `ArchivedOperators` (§3.1).
- `(address, service_type)` pair is permanently blocked from re-registration (verified via `ArchivedOperators` lookup).
- All open reports against the operator (status `PENDING` or `ESCALATED`) are closed with the distinct status `CLOSED_OPERATOR_DISSOLVED` (NOT `RESOLVED_T2`, which would misleadingly suggest a jury verdict happened) — deposits refunded to reporters. Any ESCALATED reports leave their underlying x/rep `JuryReview` row in place; x/rep does not expose a cancel API, and an INCONCLUSIVE-or-PENDING JuryReview on a dissolved operator is harmless (the resolver would no longer have an operator to apply the verdict to). Audit history can distinguish dissolution-closures from jury rejections via the `CLOSED_OPERATOR_DISSOLVED` status on the x/service `Report` record.
- The operator's reputation in the `service-operator` tag is deducted (see §6.6).
- A `service.operator_dissolved` event is emitted; x/commons subscribes via the `OperatorDissolutionHook` (§6.1) and auto-cancels any active `RecurringSpend` schedules targeting the operator's address. **This auto-cancellation IS in v1 — leaving slashed operators on the payroll would silently leak SPARK to known bad actors.**

### 3.5. Unbonding

The operator initiates wind-down via `MsgUnbondOperator`, which:

1. Transitions status from `ACTIVE` (or `UNDERFUNDED`) to `UNBONDING`.
2. Sets `unbond_complete_at = current_height + unbonding_period_blocks`.
3. Immediately marks the operator as non-discoverable (`IsActiveOperator` returns false). Off-chain consumers should stop relying on the operator from this point.

Unbonding is **not a shield from slashing.** An `UNBONDING` operator remains fully subject to `MsgReportOperator`, tier-1 controller slashes, and tier-2 jury slashes. The bond stays in the module account throughout the unbonding period — only `MsgClaimUnbondedBond` (after `unbond_complete_at`, with no open reports) actually moves SPARK back to the operator.

**Unbonding clock pausing.** The unbonding clock pauses while *any* of these conditions hold:

- An `ESCALATED` report against the operator is pending jury verdict.
- A `RESOLVED_T1` report is within its `report_contest_window` (operator may still escalate).

The `unbond_complete_at` height is extended by the duration of each pause. This prevents the operator from running out the unbonding clock during a jury case to claim their bond before the verdict applies.

**Forced unbonding.** Two paths can force-transition an operator into `UNBONDING` without operator consent:

- **EndBlocker sweep** of `UNDERFUNDED` operators past `underfunded_grace_blocks` (see §3.6). Auto-unbond with the standard clock; bond returned at end if no open reports.
- **Tier-2 jury verdict** with `dissolve = true` — but this goes straight to `SLASHED`, not `UNBONDING`. Bond is confiscated to community pool immediately.

The controller cannot force unbonding directly. Operators are contractors with a slashable performance guarantee, not council vassals. If the controller wants the operator gone, they must (a) stop the recurring payment via x/commons, and (b) pursue tier-1/tier-2 slashing for actual misconduct.

**Claim eligibility.** `MsgClaimUnbondedBond` succeeds iff all of:

- Operator status is `UNBONDING`.
- `current_height >= unbond_complete_at` (with any pause extensions applied).
- No `PENDING` or `ESCALATED` reports exist against the operator.
- No tier-1 escrow entries for this operator with `release_at > current_height` (genuinely still within their contest window).

When the claim handler runs, it first **eagerly processes** any of this operator's `Tier1Escrow` entries with `release_at <= current_height` that the EndBlocker sweep hasn't yet picked up — releasing them to community pool inline. This prevents the operator from being blocked by an unswept-but-expired escrow due to EndBlocker queue saturation; the operator pays the small gas cost for this in-line processing in exchange for not waiting for the next sweep.

On successful claim:

1. The bond is sent to the operator's address.
2. The live record is **removed from the `Operators` store** and moved to `ArchivedOperators` with `status = RETIRED` and `retired_at = current_height`. The live `(address, service_type)` key is freed for potential future re-registration (§3.1).
3. The reputation grant from `total_lifetime_bond_blocks` is applied (subject to the §6.6 anti-gaming cap).
4. A `service.operator_unbond_completed` event is emitted.

The address may immediately submit a fresh `MsgRegisterOperator` for the same `(address, service_type)` if they wish to re-enter — the RETIRED archive does not block re-registration (only SLASHED does).

---

### 3.6. State Transitions & EndBlocker

State transitions are split between **eager** (applied during message processing) and **EndBlocker sweeps** (applied across batches of records each block).

**Eager transitions (in message handlers):**

- `MsgRegisterOperator` → create record at `ACTIVE` (or reject if bond < min_bond, or if `(address, service_type)` exists in `ArchivedOperators` with status SLASHED).
- `MsgTopUpBond` → `UNDERFUNDED → ACTIVE` if new bond ≥ min_bond.
- `MsgUnbondOperator` → `ACTIVE | UNDERFUNDED → UNBONDING`.
- `MsgClaimUnbondedBond` → bond returned; live record removed; archived as `RETIRED` (§3.5).
- `MsgResolveReport` (T1_SLASH verdict) → slash amount moved from `bond` to `Tier1Escrow`; if remaining bond < min_bond → `ACTIVE → UNDERFUNDED`; if remaining bond = 0 → eager move to `SLASHED` and archive (§3.4.9).
- `MsgResolveReport` (ESCALATE_TO_JURY verdict) → report → `ESCALATED`; `escalated_at = current_height` set on the report; `EscalatedReportsQueue` index entry added; jury case opened in x/rep.
- `MsgContestSlash` → escrow entry for this `(operator, report_id)` is released back to `bond`; `tier1_slashed_in_window` decremented; report → `ESCALATED`; `escalated_at = current_height`; queue entry added.
- `MsgResolveReportByJury` (Accept/Reduce) → escrow entry consumed (if still within window) or bond debited directly; transferred to community pool; if `dissolve=true` OR bond → 0, eager move to `SLASHED` and archive.
- `MsgResolveReportByJury` (Reject) → if a prior T1 slash was in escrow for this report, release back to bond; report → `RESOLVED_T2`.
- `MsgOpenControllerTransferCase` → `ControllerTransferCases` row created; `OpenControllerTransferByOperator` index entry added; opener's `report_deposit` escrowed to the controller-transfer case deposit pool (§3.4.7); x/rep jury case opened.
- `MsgFinalizeControllerTransfer` (ACCEPT, re-check passes) → `operator.controller` updated; opener's deposit refunded from module account; `ControllerTransferCases` row + index entry deleted.
- `MsgFinalizeControllerTransfer` (REJECT, or ACCEPT with failed re-check) → opener's deposit forfeited to community pool via `FundCommunityPool`; case row + index entry deleted.

**EndBlocker sweeps (gas-bounded, deterministic order):**

Each block, the EndBlocker processes up to `endblocker_sweep_limit` records (default 100 per category) from each of these queues:

1. **Underfunded sweep.** Iterate `UNDERFUNDED` operators where `current_height >= underfunded_since + underfunded_grace_blocks`. Force-transition to `UNBONDING` with the standard clock.
2. **Pending report sweep.** Iterate `PENDING` reports where `current_height >= filed_at + max_pending_blocks`. Mark `AUTO_DISMISSED`, refund deposit to reporter, record refile-cooldown entry.
3. **Escalated timeout sweep.** Iterate `ESCALATED` reports where `current_height >= escalated_at + max_escalated_blocks`. Mark `AUTO_TIMEOUT`, release any contested-T1 escrow back to bond AND **delete the `Tier1Escrow` row + its release-queue entry** (so sweep 4 doesn't double-process it), refund the reporter/opener deposit. The parallel x/rep `JuryReview` is left in place — x/rep does not expose a cancel API, and an INCONCLUSIVE-or-PENDING JuryReview against an AUTO_TIMEOUT'd report is harmless (the resolver path checks the x/service report's status before applying any verdict). For controller-transfer cases, also delete the `ControllerTransferCases` row and its `OpenControllerTransferByOperator` index entry.
4. **Tier-1 escrow release sweep.** Iterate `Tier1Escrow` entries where `current_height >= release_at` AND the parent report is in a terminal state (`RESOLVED_T1` finalized after contest window with no contest, `RESOLVED_T2`, or `AUTO_TIMEOUT`). For each entry: transfer escrowed SPARK to community pool via `FundCommunityPool` (unless `AUTO_TIMEOUT` — those return to bond), delete the escrow entry.
5. **Reporter rate-limit pruning.** Lazy — entries are checked-and-pruned at next `MsgReportOperator` from the same reporter. A small EndBlocker sweep also walks the `(reporter, last_filed_at)` index and prunes expired counters to keep the index bounded.
6. **Unbond completion sweep — NOT performed.** Unbonding completion is **lazy** — the operator must call `MsgClaimUnbondedBond`. This avoids unbounded EndBlocker work and is the same pattern used by x/staking. The `unbond_complete_at` field is informational; nothing happens at that height except that subsequent claim calls succeed.

**Gas-bounded ordering.** Sweeps iterate in deterministic key order (e.g., by `(underfunded_since, address)` for queue 1, by `(release_at, escrow_id)` for queue 3) and stop after `endblocker_sweep_limit` items. If more records are eligible than the limit allows, the remainder is processed next block. This bounds per-block gas and prevents an operator-spam attack from stalling the chain.

**Service type config changes.** `MsgUpdateServiceTypeConfig` does not eagerly transition affected operators. Operators whose bond is now below a newly-raised `min_bond` transition to `UNDERFUNDED` lazily — the next message that touches the operator (any message, including a report or top-up) checks `bond < current_min_bond` and updates status. The EndBlocker queue 1 then picks them up if they remain underfunded past the grace. This avoids an O(N) EndBlocker pass on every param change.

---

### 3.7. System Reports (module-filed)

Allowlisted consumer modules can file reports on behalf of the chain via the keeper-level `OpenSystemReport` API (no signed `Msg`, no SPARK deposit, no user-level rate limit). This is how `x/federation` files a slash report against a bridge operator when an arbiter quorum upholds a content challenge (`docs/x-federation-spec.md` §3.9.7 / §10.4) — the federation module account is the recorded reporter, but the controller resolution path is identical to a community-filed report.

```go
// keeper public API
func (k Keeper) OpenSystemReport(
    ctx context.Context,
    callerModuleAddr sdk.AccAddress,
    operator sdk.AccAddress,
    serviceType string,
    slashBps uint32,                // 0 = use ServiceTypeConfig.challenge_default_slash_bps
    evidenceURI string,
    dedupeKey []byte,
) (reportID uint64, err error)
```

#### 3.7.1. Caller authorization

The OpenSystemReport surface is privilege-gated by a **sorted slice allowlist** (`allowedSystemCallers = []string{"federation"}` at launch) plus a forward-derive auth check against the x/auth keeper:

1. Iterate the allowlist in deterministic order.
2. For each name, look up the module's account address via `authKeeper.GetModuleAddress(name)`.
3. First match where `derivedAddr.Equals(callerModuleAddr)` wins; the matched module name is recorded for events, rate-limit keying, and dedupe scoping.
4. If no match: return `ErrUnauthorizedSystemCaller`.

**Why forward-derive instead of reverse lookup?** Cosmos SDK's auth keeper does not expose `address → module name`. The forward derivation is the SDK-native pattern. The allowlist is tiny so the O(allowlist_size) per call is irrelevant.

**Why a sorted slice instead of a map?** Go map iteration is non-deterministic. With "first match wins," map iteration could produce different `matchedModule` values across nodes once the list grows beyond one entry — a consensus break. Slice iteration is deterministic. Adding new callers requires a source patch (intentional privilege-escalation friction).

**Authorization model.** The allowlist + auth-keeper lookup is **defense-in-depth + auditability**, not the primary authorization barrier. Real authorization is keeper-wiring discipline: only the modules listed in `allowedSystemCallers` receive a `ServiceKeeper` reference at app wiring. The address-then-name lookup additionally prevents accidental misuse and forces any spoofer to explicitly impersonate a named module account, which shows up in the `caller_module` attribute on the emitted event and is detectable.

#### 3.7.2. Idempotency

The `dedupeKey` (caller-supplied, typically a deterministic hash of a unique upstream identifier like `MimcHash(challenge.id, challenge.evidence_hash)`) keys a per-`(matchedModule, dedupeKey)` entry in the `SystemReportDedup` store mapped to the originating `report_id`.

- A second call with the same key returns the **existing** `report_id` without opening a new report. Used to make federation's challenge-resolution path safe against re-org replays or in-flight retries.
- Once the underlying report reaches a terminal status (`AUTO_DISMISSED`, `RESOLVED_T1` finalized, `RESOLVED_T2`, `AUTO_TIMEOUT`, `CLOSED_OPERATOR_DISSOLVED`), the dedupe entry remains, but the caller must use a **fresh `dedupeKey`** to open a follow-up report — the TTL of the dedupe entry IS the lifecycle of the report it points at.
- Idempotent re-calls do **not** count against the per-caller rate limit.
- `dedupeKey` MUST be non-empty (`ErrInvalidDedupeKey` otherwise).

#### 3.7.3. Rate limiting

Per `(matchedModule, sliding window)`, OpenSystemReport admits at most `max_system_reports_per_caller_per_window` (default 50) **new** reports within `rate_limit_window_blocks` (default ~1 day). The window is sliding (ring buffer of recent filing heights, capped at `cap+1` for brief overshoot absorption); admission is based on counting heights within `[current_height - window, current_height]`.

If the cap is exceeded the call returns `ErrSystemReportRateLimited`, emits a `system_report_rate_limited` event for off-chain alerting, and does **not** mutate state. Idempotent re-calls (same dedupe key) bypass the cap entirely.

Tunable post-launch: the default 50/day is a starting guess. Calibrate from observed federation challenge volume — most days should be well under the cap, and a sustained spike indicates either real abuse upstream or a bug in the consumer module (both should page an operator).

#### 3.7.4. Slash amount + report shape

- If `slashBps == 0`, the keeper substitutes `ServiceTypeConfig.challenge_default_slash_bps` for the proposed slash. The cross-field validation at config-write time guarantees this value is ≤ `unilateral_slash_cap_bps`, so the controller can always resolve via T1_SLASH (or downgrade) without escalation. An explicit non-zero `slashBps` is also capped at `unilateral_slash_cap_bps`.
- The proposed slash is stored on `Report.proposed_slash_bps`. The controller adjusts the actual slash at `MsgResolveReport` time (downgrade for borderline evidence, upgrade up to the cap for egregious cases).
- `Report.reporter` is set to `callerModuleAddr` (no member required, no deposit escrowed).
- `Report.reason` is `system:<caller_module>:<evidenceURI>` (truncated to `max_reason_bytes`).
- The standard report state machine applies from there: PENDING → T1 / ESCALATED. The operator's `ServiceTypeConfig.report_timeout_action` controls what happens at `max_pending_blocks` expiry.

#### 3.7.5. Accountability asymmetry vs. member-filed reports

Module accounts cannot be slashed for false reports and have no reputation. The controller's tier-1 review is the only immediate check on a system report. `ReportTimeoutAction=ESCALATE` (paired with system-reporting consumers) ensures the jury is the effective failsafe.

---

## 4. State

### 4.1. KV Stores

**Live operator stores:**

- `Operators` — primary, keyed by `(address, service_type)`. Holds only non-terminal records (`ACTIVE | UNDERFUNDED | UNBONDING`).
- `OperatorsByController` — secondary index, key `(controller, address, service_type)`. Enables `Operators(by_controller=)` query without scanning.
- `OperatorsByServiceType` — secondary index, key `(service_type, address)`. Enables `Operators(by_service_type=)` query without scanning.
- `UnderfundedQueue` — index of `UNDERFUNDED` operators keyed by `(underfunded_since, address, service_type)` for EndBlocker sweep ordering.

**Archive store (terminal records):**

- `ArchivedOperators` — keyed by `(address, service_type, retired_at)` to allow multiple terminal records over time. Holds `SLASHED` and `RETIRED` records moved out of `Operators` at the terminal transition. The triple-key shape lets `(address, service_type)` host one current live record AND any number of past terminal records without collision.

**Service type registry:**

- `ServiceTypes` — registry keyed by `service_type` string.

**Report stores:**

- `Reports` — keyed by `report_id` (auto-incrementing uint64); contains `operator_address, service_type, reporter, reason, filed_at, escalated_at, status, proposed_slash_bps, slash_amount, deposit, jury_case_id`. `escalated_at` is set when the report transitions to `ESCALATED` (0 otherwise) and feeds the auto-timeout sweep.
- `ReportsByOperator` — secondary index, key `(operator_address, service_type, report_id)`. Enables "all reports against operator" queries.
- `PendingReportsQueue` — index of `PENDING` reports keyed by `(filed_at, report_id)` for EndBlocker auto-dismiss sweep.
- `EscalatedReportsQueue` — index of `ESCALATED` reports keyed by `(escalated_at, report_id)` for EndBlocker auto-timeout sweep.
- `RefileCooldowns` — keyed by `(controller, operator_address, service_type, dismissed_at)`; entries expire after `report_refile_cooldown_blocks` (lazy expiry).
- `ReporterRateLimit` — keyed by `(reporter, operator_address, service_type)` → ring buffer of up to `max_reports_per_reporter_per_operator_per_window + 1` recent filing heights; admits a new filing iff fewer than the cap fall within the trailing window (§3.4.6 sliding-window semantics).

**Controller-transfer cases:**

- `ControllerTransferCases` — keyed by `jury_case_id` (uint64), value `{ operator_address, service_type, opener, proposed_new_controller, deposit, opened_at }`. Records who opened each case and the deposit amount, so `MsgFinalizeControllerTransfer` (§5.4) can refund/forfeit correctly. Deleted on finalize. At most one open case per `(operator_address, service_type)` (enforced via secondary index `OpenControllerTransferByOperator`).
- `OpenControllerTransferByOperator` — secondary index keyed by `(operator_address, service_type)` → `jury_case_id`. Enforces the "one open case at a time" rule from §5.4.

**Tier-1 escrow & slash history:**

- `Tier1Escrow` — keyed by `escrow_id` (auto-incrementing uint64), value `{ report_id, operator_address, service_type, amount, release_at }`. Holds slashed SPARK pending contest-window expiry.
- `Tier1EscrowByOperator` — secondary index, key `(operator_address, service_type, escrow_id)`. Used by `MsgClaimUnbondedBond` and contest paths.
- `Tier1EscrowReleaseQueue` — index keyed by `(release_at, escrow_id)` for EndBlocker release sweep.
- `Tier1LastSlash` — keyed by `(controller, operator_address, service_type)` → last tier-1 slash height; consulted for cooldown enforcement. Pruned when the operator record archives (BOTH `SLASHED` and `RETIRED` transitions), so a re-registered operator never inherits cooldown state from a prior incarnation.

**System-report stores** (consumed by `OpenSystemReport`, §3.7):

- `SystemReportDedup` — keyed by `(caller_module, dedupe_key)` → `report_id`. Implements the idempotency guarantee: re-calls with the same dedupe key return the existing report_id instead of opening a duplicate. Lifetime = lifetime of the report it points at.
- `SystemReportRateLimit` — keyed by `caller_module` → `SystemReportRateLimit { recent_filing_heights []int64 }`. Ring buffer of the most recent filing heights, capped at `max_system_reports_per_caller_per_window + 1`; older entries are overwritten on each new filing past the cap.

**Counters:**

- `NextReportID` — singleton uint64.
- `NextEscrowID` — singleton uint64.

### 4.2. Module Params (x/gov-mutable)

| Param                              | Default            | Notes |
|------------------------------------|--------------------|-------|
| `default_unbonding_period_blocks`  | ~14 days in blocks | Per-type override available |
| `default_unilateral_slash_cap_bps` | 500 (5%)           | Per-type override available |
| `default_tier1_window_blocks`      | ~90 days           | Per-type override available |
| `default_tier1_aggregate_cap_bps`  | 1500 (15%)         | Per-type override available |
| `default_tier1_cooldown_blocks`    | ~7 days            | Per-type override available |
| `default_underfunded_grace_blocks` | ~7 days            | Per-type override available |
| `report_contest_window_blocks`     | ~24 hours          | Global; how long operator has to escalate T1 (also tier-1 escrow release delay) |
| `max_pending_blocks`               | ~30 days           | Global; auto-dismiss horizon for PENDING reports |
| `max_escalated_blocks`             | ~60 days           | Global; auto-timeout horizon for ESCALATED reports awaiting jury verdict (§3.4.5) |
| `report_refile_cooldown_blocks`    | ~30 days           | Global; controller cannot re-file dismissed allegation |
| `report_deposit`                   | 10 SPARK in uspark | Global; required deposit to file `MsgReportOperator` |
| `min_reporter_trust_level`         | `TRUST_LEVEL_ESTABLISHED` | Global; gates who can file reports |
| `max_reports_per_reporter_per_operator_per_window` | 3 | Global; per-reporter cap against single operator (§3.4.6) |
| `reporter_rate_limit_window_blocks`| ~30 days           | Global; sliding window for the above cap |
| `endblocker_sweep_limit`           | 100                | Global; per-queue per-block cap |
| `max_metadata_bytes`               | 4096               | Global; cap on `Operator.metadata` size |
| `max_reason_bytes`                 | 512                | Global; cap on `Report.reason` size |
| `max_active_operators_per_address` | 16                 | Global; counts live records in any of `{ACTIVE, UNDERFUNDED, UNBONDING}` (terminal records in `ArchivedOperators` don't count); prevents reputation gaming via mass micro-registration |
| `reputation_grant_per_bond_block`  | tiny `math.LegacyDec` | Global; reputation accrued per SPARK-block of active bond (capped by §6.6 rule); `sdk.Dec` alias is deprecated in current SDK |
| `default_pagination_limit`         | 100                | Global; default page size for queries when client omits `pagination.limit` |
| `max_pagination_limit`             | 1000               | Global; hard cap on `pagination.limit` to bound query gas |
| `max_system_reports_per_caller_per_window` | 50         | Global; per-`caller_module` sliding-window cap on `OpenSystemReport` filings (§3.7.3); idempotent re-calls do not count |
| `rate_limit_window_blocks`         | ~1 day in blocks   | Global; window size for the system-report rate limit. x/service has no native epoch concept, so this is an explicit block-count param rather than reusing season/shield epochs |

**Param validation rules** (`Params.Validate`):

- All `*_blocks` params > 0.
- `default_tier1_aggregate_cap_bps ≥ default_unilateral_slash_cap_bps`.
- All `*_bps` params in `(0, 10000]`.
- `default_unbonding_period_blocks ≥ report_contest_window_blocks`.
- `max_metadata_bytes ≤ 65536` and `max_reason_bytes ≤ 4096` (proto / event sanity).
- `report_deposit.denom == "uspark"` and `report_deposit.amount > 0`.
- `max_reports_per_reporter_per_operator_per_window >= 1` and `reporter_rate_limit_window_blocks > 0`.
- `report_refile_cooldown_blocks >= max_pending_blocks` — otherwise a hostile controller can stall a report to auto-dismiss, then re-file before the refile-cooldown bites; bounding cooldown >= pending window ensures the refile protection is the active limiter.
- `max_escalated_blocks > max_pending_blocks` — juries take longer than controllers (deliberation overhead), so the escalated-timeout window must be the looser of the two.
- `default_pagination_limit > 0`, `max_pagination_limit >= default_pagination_limit`, `max_pagination_limit <= 10000`.
- `max_system_reports_per_caller_per_window >= 0`; when the cap is non-zero, `rate_limit_window_blocks > 0` (a zero window with a non-zero cap is meaningless). A zero cap effectively disables system reports.

All params are x/gov-mutable. None are hardened at module level. **High-impact params worth hardening if governance integrity becomes a concern:**

- `report_deposit` — if lowered to zero, every member of x/commons can mass-file reports against any operator at no cost; deposit pool griefing.
- `min_reporter_trust_level` — if lowered to `TRUST_LEVEL_NONE`, Sybil addresses can flood reports.
- `tier1_aggregate_cap_bps` and `tier1_cooldown_blocks` — if raised/lowered respectively, the drainage protection in §3.4.3/§3.4.4 collapses; captured controllers can drain operator bonds with no jury check.
- `max_reports_per_reporter_per_operator_per_window` — if raised, single-reporter spam against one operator becomes viable.
- `report_contest_window_blocks` — if lowered to zero, tier-1 slashes are immediately final, eliminating the contest path entirely.
- `max_system_reports_per_caller_per_window` — if raised, a buggy or compromised allowlisted consumer module can DoS controllers by flooding system reports. If lowered to zero, federation can no longer file challenge-derived slashes (effectively breaks federation's accountability layer until the cap is restored).

To harden, follow the existing immutable-parameter pattern from x/mint: authority = burn address (`sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe`); `MsgUpdateParams` then becomes unreachable via gov and only chain upgrades can modify. Defer hardening until launch experience shows whether governance can be trusted with these levers.

---

## 5. Messages

All signer Msgs MUST carry `option (amino.name) = "sparkdream/x/service/Msg<Name>"` so Keplr+Ledger users can sign via `SIGN_MODE_LEGACY_AMINO_JSON` (see [CLAUDE.md](CLAUDE.md) and the regression guard at [x/commons/types/amino_name_test.go](x/commons/types/amino_name_test.go) — a sibling test SHOULD be added under `x/service/types`).

### 5.1. Registration & lifecycle (signed by operator)

- `MsgRegisterOperator { creator, service_type, controller, bond, metadata }` — `creator` is signer = operator address. **MUST be signed by the `creator` key directly — not via x/authz `MsgExec` and not via x/session `MsgExecSession`.** The msg-server `ValidateBasic`/keeper checks `creator == tx_signer[0]` and rejects any indirection. Rationale: the SPARK-bond commitment is the operator's foundational accountability act; allowing delegation lets a session/authz key bind someone else's SPARK to obligations they didn't personally accept. The same constraint applies to `MsgUnbondOperator` (§6.5). Validation:
  - service type exists and `enabled`;
  - `bond.denom == "uspark"` and `bond.amount >= service_type.min_bond`;
  - `controller != creator` (`ErrSelfController`);
  - `commonsKeeper.IsGroupAddress(ctx, controller) == true` (`ErrControllerNotGroup`) — the controller MUST be a registered x/commons Group, not an arbitrary EOA;
  - no SLASHED record exists for `(creator, service_type)` in `ArchivedOperators` (`ErrOperatorPreviouslySlashed`);
  - no live record exists for `(creator, service_type)` in `Operators` (`ErrOperatorAlreadyExists`); a prior RETIRED record in `ArchivedOperators` does NOT block re-registration;
  - address has not exceeded `max_active_operators_per_address` (counting any record in `Operators` regardless of status; archived records don't count) (`ErrTooManyOperatorsForAddress`).

  Bond is escrowed to the module account's **bond pool** (§3.4.7) at registration. New-operator field initialization: `status = ACTIVE`, `registered_at = current_height`, `last_bond_block_update_at = current_height` (anchor for lazy bond-block accrual — §6.6), `tier1_window_start = current_height`, `tier1_window_start_bond = bond.amount`, all other counters zero.

  **Concurrent-registration race:** if two `MsgRegisterOperator` messages from the same address with the same `service_type` land in the same block, message processing is serialized within the block (standard SDK behavior), so the second message observes the first's write at execute time and fails with `ErrOperatorAlreadyExists`. No special locking required.
- `MsgUpdateMetadata { operator, new_metadata }` — signer = operator. `len(new_metadata) <= max_metadata_bytes`.
- `MsgUnbondOperator { operator }` — signer = operator. Transitions to `UNBONDING`; sets clock.
- `MsgClaimUnbondedBond { operator }` — signer = operator. Bond returned iff conditions in §3.5 met; live record is archived as `RETIRED`.
- `MsgTopUpBond { operator, additional_bond }` — signer = operator. `additional_bond.denom == "uspark"`. Updates bond and clears `UNDERFUNDED` if applicable.

### 5.2. Reports & slashing

- `MsgReportOperator { reporter, operator, service_type, reason }` — signer = `reporter`. Validation:
  - reporter meets `min_reporter_trust_level` (`ErrReporterTrustLevelTooLow`);
  - `len(reason) <= max_reason_bytes` (`ErrInvalidReasonSize`);
  - operator exists in `Operators` (not archived) — `ErrOperatorNotFound` for missing, `ErrOperatorSlashed` if found in `ArchivedOperators` as SLASHED;
  - reporter is NOT a member of `operator.controller` Group: `!commonsKeeper.IsGroupMember(ctx, reporter, operator.controller)` (`ErrReporterIsControllerMember`);
  - reporter has not exceeded `max_reports_per_reporter_per_operator_per_window` for this `(operator, service_type)` in the current `reporter_rate_limit_window_blocks` (`ErrReporterRateLimitExceeded`);
  - no active `RefileCooldowns` entry blocks this filing.

  Module escrows `report_deposit` from reporter into the **report deposit pool**. Returns `report_id`.
- `MsgResolveReport { controller, report_id, verdict, slash_bps }` — signer = the operator's `controller` address. `verdict` ∈ `{T1_SLASH, T1_DISMISS, ESCALATE_TO_JURY}`. Validation:
  - signer == `operator.controller` (`ErrUnauthorizedController`);
  - report status is `PENDING` (`ErrReportAlreadyResolved`);
  - tier-1 cooldown not active for this `(controller, operator, service_type)` if verdict is `T1_SLASH` (`ErrTier1CooldownActive`);
  - aggregate cap not exceeded if verdict is `T1_SLASH` (`ErrTier1AggregateCapExceeded`);
  - `slash_bps <= unilateral_slash_cap_bps` if verdict is `T1_SLASH` (`ErrSlashCapExceeded` — controller must use `ESCALATE_TO_JURY` for larger slashes);
  - `slash_bps == 0` if verdict is `T1_DISMISS`.

  On `ESCALATE_TO_JURY`: the proposed `slash_bps` is stored on the report as `proposed_slash_bps`, the report transitions to `ESCALATED`, and an x/rep jury case is opened with the proposed slash as the upper bound (§6.2). On `T1_SLASH`: slash amount moved to `Tier1Escrow` (§3.4.7), pending the contest window.
- `MsgContestSlash { operator, report_id }` — signer = operator. Valid only on a `RESOLVED_T1` report whose resolution is within `report_contest_window_blocks` (`ErrContestWindowExpired` otherwise). On success: the corresponding `Tier1Escrow` entry is released back to `bond`, `tier1_slashed_in_window` is decremented, status is reverted to its pre-slash value, and the report transitions to `ESCALATED` (jury case opened with `proposed_slash_bps` set to the original slash_bps). The operator pays no deposit to contest (already bonded).
- `MsgResolveReportByJury { resolver, report_id, verdict, slash_bps, dissolve }` — signer = a member of the **Commons Council Operations Committee** (matches the existing project-wide jury-resolver pattern; see [`MsgResolveGovActionAppeal`](x/rep/keeper/msg_server_resolve_gov_action_appeal.go) for the canonical example). x/service additionally cross-checks the resolver's verdict against the underlying x/rep `JuryReview` row to ensure they're proposing a verdict consistent with the jury's actual tally (see §6.2). `verdict` ∈ `{ACCEPT, REDUCE, REJECT}`. Verdict-to-slash_bps relationship:

  | Verdict  | `slash_bps` constraint                                                     | Effect                                                          |
  |----------|----------------------------------------------------------------------------|-----------------------------------------------------------------|
  | `ACCEPT` | `slash_bps == report.proposed_slash_bps`                                   | Apply full proposed slash to bond (or consume Tier1Escrow entry if a prior T1 slash was contested); transfer to community pool |
  | `REDUCE` | `0 < slash_bps < report.proposed_slash_bps`                                | Apply reduced slash; if reducing a contested T1 slash, the difference between escrow amount and new slash returns to bond |
  | `REJECT` | `slash_bps == 0`                                                           | No slash; release any contested-T1 escrow back to bond          |

  Reporter deposit handling per §3.4.6 (refund on ACCEPT/REDUCE, forfeit to community pool on REJECT). If `dissolve == true` (only valid with `ACCEPT`), the operator transitions to `SLASHED` regardless of remaining bond. This message is the **only** path to apply a tier-2 slash; submission by a non-Operations-Committee resolver is rejected with `ErrUnauthorizedCouncilResolver`, and a verdict that doesn't match the underlying JuryReview is rejected with `ErrJuryVerdictMismatch`.

### 5.3. Service type registry (gov-authority)

- `MsgUpdateServiceTypeConfig { authority, config }` — signer = x/gov module account address (`authtypes.NewModuleAddress(govtypes.ModuleName).String()`). Validates config (§3.2 validation rules); creates or updates the registry entry. Setting `enabled = false` on an existing type disables new registrations but does not affect existing operators (§3.2 disabled-service-type semantics).

### 5.4. Controller transfer (jury-authority)

Transferring an operator's controller without operator unbond+re-register requires a jury verdict. The path is two-step:

- `MsgOpenControllerTransferCase { opener, operator, service_type, proposed_new_controller, reason }` — signer = `opener`. Validation:
  - `opener` is either (a) the operator themselves, or (b) any member of x/commons meeting `min_reporter_trust_level`;
  - `len(reason) <= max_reason_bytes`;
  - `proposed_new_controller != operator.controller` and `commonsKeeper.IsGroupAddress(ctx, proposed_new_controller)`;
  - no other controller-transfer case is currently open for this `(operator, service_type)` (`ErrControllerTransferCaseAlreadyOpen`).

  Module escrows `report_deposit` from `opener` (same anti-spam mechanism as `MsgReportOperator`; refunded on ACCEPT, forfeited on REJECT — controller-transfer verdicts are binary, no REDUCE; see §6.2). Opens an x/rep jury case with `caseType = service.controller_transfer`. A new store row in `ControllerTransferCases` records `{ jury_case_id, opener, deposit }` so the apply path can refund the right address.

  Returns `jury_case_id`.
- `MsgFinalizeControllerTransfer { resolver, jury_case_id, verdict, new_controller }` — signer = a member of the **Commons Council Operations Committee** (matching `MsgResolveReportByJury`'s authority model). `verdict ∈ {ACCEPT, REJECT}`. `new_controller` is set on ACCEPT (must match the value stored on the case row) and ignored on REJECT. The keeper cross-checks the resolver's verdict against the x/rep `JuryReview` row via `repKeeper.GetJuryReview(jury_case_id)`; mismatches return `ErrJuryVerdictMismatch` (see §6.2).

  On `ACCEPT`, the keeper:
  1. Looks up the `ControllerTransferCases` row by `jury_case_id` (`ErrControllerTransferCaseNotFound` if missing).
  2. Verifies `msg.new_controller == case_row.proposed_new_controller`.
  3. **Re-checks** `commonsKeeper.IsGroupPolicyAddress(ctx, new_controller)` AND that the target Group has at least one active member at apply time (`commonsKeeper.GroupPolicyMemberCount(ctx, new_controller) >= 1`) — the target Group could have been dissolved or emptied during the jury case (`ErrControllerNoLongerEligible`). On failure, the case is treated as REJECT (opener's deposit forfeited) rather than left dangling.
  4. Updates `operator.controller` to `new_controller`.
  5. Refunds the opener's deposit from the module account to the address recorded on the case row.
  6. Deletes the case row.

  On `REJECT`, the keeper:
  1. Looks up the case row.
  2. Forfeits the opener's deposit to the community pool via `FundCommunityPool`.
  3. Deletes the case row.

This is the **only** mechanism by which `controller` changes outside of operator unbond+re-register.

**Authority + verdict-integrity model.** The resolver is a human Operations Committee member, NOT the x/rep module account. This matches every other appeal-resolution path in this codebase (forum's POST_HIDE_APPEAL / THREAD_LOCK_APPEAL / etc., and rep's gov_action_appeal). The committee member observes the jury's verdict off-chain (or via the `Query/JuryReview` RPC) and submits a matching verdict. The on-chain integrity check is the `GetJuryReview` cross-check: if the resolver submits a verdict that doesn't match the jury's tallied result, the message is rejected. This means a single rogue Operations Committee member cannot forge a verdict — they can only at worst delay or refuse to relay a legitimate one, in which case the EndBlocker auto-timeout sweep (§3.6 queue 3) eventually resolves the case as REJECT-equivalent (AUTO_TIMEOUT).

**Operator defense surface.** The case carries only the opener's `reason` string. The operator has no first-class on-chain way to argue against a transfer (e.g., "this council has been working fine, the opener has a personal grievance"). Defense happens off-chain via the standard x/rep jury UI — jurors can request additional context from any party before voting. For v1 this is acceptable; if controller-transfer abuse becomes a real issue, a future revision could add `MsgRespondToControllerTransferCase` for structured operator rebuttal.

The case-opening surface is intentionally narrow (operator + ESTABLISHED members) but always available — without this path, an operator whose controller has been silently dissolved would be permanently stranded.

### 5.5. Authority model summary

| Authority             | Address derivation                                              | Used in                                       |
|-----------------------|-----------------------------------------------------------------|-----------------------------------------------|
| Operator              | The operator's own bech32 address                               | §5.1, `MsgContestSlash`                       |
| Controller            | `Operator.controller` field (typically a council group address) | `MsgResolveReport`                            |
| Reporter              | Any address meeting `min_reporter_trust_level`                  | `MsgReportOperator`                           |
| x/gov module          | `authtypes.NewModuleAddress("gov")`                             | `MsgUpdateServiceTypeConfig`                  |
| Commons Ops Committee | Member of `commons/operations` committee                         | `MsgResolveReportByJury`, `MsgFinalizeControllerTransfer` |

The "jury resolver" is a **Commons Council Operations Committee member**, not the x/rep module account. This matches every other appeal-resolution path in this codebase (`MsgResolveGovActionAppeal`, `MsgResolvePostHideAppeal`, etc.): the committee member observes the jury's verdict off-chain (or via x/rep's `Query/JuryReview` RPC) and submits a matching verdict on the originating module. The on-chain integrity guarantee is the cross-check against `repKeeper.GetJuryReview(...)` — a resolver cannot forge a verdict the jurors didn't actually reach. The committee acts as a low-trust relayer, not the verdict source.

---

## 6. Integration Points

### 6.1. x/commons (recurring payments + dissolution hook)

**Payments path (no new code).** Existing `MsgScheduleRecurringSpend` already accepts an arbitrary recipient address; the council schedules a periodic SPARK payment to the operator's address.

**Operator-lifecycle hooks.** x/service exposes a `ServiceHooks` interface with four callbacks. x/commons subscribes to the dissolution hook; x/federation (added in Phase 0 of the federation→service migration) subscribes to all four:

```go
type ServiceHooks interface {
    AfterOperatorDissolved(ctx sdk.Context, operator sdk.AccAddress, serviceType string)
    AfterOperatorRetired(ctx sdk.Context, operator sdk.AccAddress, serviceType string)
    AfterOperatorUnderfunded(ctx sdk.Context, operator sdk.AccAddress, serviceType string)
    AfterOperatorReFunded(ctx sdk.Context, operator sdk.AccAddress, serviceType string)
}
```

**Firing semantics:**

| Hook | Fires from | Semantics |
|------|-----------|-----------|
| `AfterOperatorDissolved` | `MsgResolveReportByJury` with `dissolve=true`, OR any slash that drops bond to zero | Status transitioned to terminal `SLASHED`. x/commons cancels matching `RecurringSpend` schedules so slashed operators stop being paid (this auto-cancellation is IN v1 — leaving slashed operators on the payroll silently leaks SPARK to known bad actors). x/federation prunes all `BridgeBinding` records for this `(operator, service_type)` and decrements peer state. |
| `AfterOperatorRetired` | `MsgClaimUnbondedBond` success path | Voluntary unbond completed; bond was returned to operator. x/commons is a no-op (controller decides whether to keep paying a rotating/sabbatical operator). x/federation prunes bindings (same as Dissolved, no punitive side effects). |
| `AfterOperatorUnderfunded` | `MsgResolveReport(T1_SLASH)` or `MsgResolveReportByJury` (ACCEPT/REDUCE) when post-slash bond falls below `min_bond` | Does **not** fire if the operator is already in `UNBONDING` (they're exiting anyway). Lets consumers gate operator-active checks without polling status. x/federation marks all of that operator's bindings under the service_type as `suspended=true`. |
| `AfterOperatorReFunded` | `MsgTopUpBond` (or contested-T1 escrow returning to bond, or AUTO_TIMEOUT escrow return) when an UNDERFUNDED operator crosses back above `min_bond` | No-op for `UNBONDING` operators (top-ups during unbonding don't reactivate). x/federation clears `suspended` on all bindings under the service_type. |

All four hooks fire **after** x/service has written the state mutation, so subscribers iterating live indexes see the post-transition state.

**MultiServiceHooks and ordering.** Multiple subscribers are aggregated via `NewMultiServiceHooks(...)`; the slice order determines invocation order. In `app.go`:

```go
app.ServiceKeeper.SetHooks(
    servicetypes.NewMultiServiceHooks(
        NewFederationServiceHooks(app.FederationKeeper),  // federation FIRST
        NewCommonsServiceHooks(app.CommonsKeeper),        // commons SECOND
    ),
)
```

**Why federation before commons on `AfterOperatorDissolved`/`Retired`:** federation cleans bindings first, then commons cancels recurring spends. Federation's cleanup is cheaper, deterministic, and shouldn't depend on commons state; commons' recurring-spend cancellation is unrelated to binding state. Both follow the fail-soft `defer recover` pattern (a bug in federation's hook must never roll back a slash chain-wide, since rolling back the slash would brick all bridge accountability). The `federation_hook_failure` event (federation-side) signals swallowed panics to off-chain monitors.

### 6.2. x/rep (jury)

Tier-2 slashing and controller-transfer cases reuse the existing
[x/rep `CreateAppealInitiative` API](x/rep/keeper/jury.go) — the same
generic-jury entry-point already used by forum (POST_HIDE_APPEAL,
THREAD_LOCK_APPEAL, THREAD_MOVE_APPEAL, pin_dispute, sentinel_appeal,
moderation_appeal) and by rep's own gov_action_appeal.

**Opening a jury case.** From the handlers that need a jury verdict
(`MsgResolveReport(ESCALATE_TO_JURY)`, `MsgContestSlash`,
`MsgOpenControllerTransferCase`), x/service calls:

```go
juryReviewID, err := repKeeper.CreateAppealInitiative(
    ctx,
    initiativeType,   // "service.slash" or "service.controller_transfer"
    payload,          // JSON-encoded case-specific data
    deadline,         // current_height + max_escalated_blocks
)
```

The returned `juryReviewID` is stored on the originating record
(`Report.jury_case_id` or `ControllerTransferCase.jury_case_id`).
x/rep then handles juror selection, vote intake (`MsgSubmitJurorVote`),
expert testimony (`MsgSubmitExpertTestimony`), and final tallying
(`TallyJuryVotes` — runs eagerly once `RequiredVotes` is hit, or via
x/rep's deadline-driven EndBlocker for inconclusive cases).

**Verdict shape.** x/rep's `Verdict` enum is:
`VERDICT_PENDING | VERDICT_UPHOLD_CHALLENGE | VERDICT_REJECT_CHALLENGE
| VERDICT_INCONCLUSIVE`. x/service maps these to its own outcomes at
resolve time:

| x/rep `Verdict`              | `service.slash` outcome  | `service.controller_transfer` outcome |
|------------------------------|--------------------------|---------------------------------------|
| `VERDICT_UPHOLD_CHALLENGE`   | ACCEPT (apply slash)     | ACCEPT (apply transfer)               |
| `VERDICT_REJECT_CHALLENGE`   | REJECT (no slash)        | REJECT (deposit forfeit)              |
| `VERDICT_INCONCLUSIVE`       | REJECT-equivalent        | REJECT-equivalent                     |
| `VERDICT_PENDING`            | n/a — resolver rejected  | n/a — resolver rejected               |

(`REDUCE` is x/service's third outcome but isn't a distinct x/rep
verdict — the resolver expresses it on `MsgResolveReportByJury` with
`verdict=REDUCE, slash_bps < proposed_slash_bps`. The cross-check
allows REDUCE only when the jury voted UPHOLD; a REDUCE submitted
against a REJECT-verdict jury is rejected with `ErrJuryVerdictMismatch`.)

**Verdict delivery.** There is **no automatic callback** from x/rep
into x/service. Instead, a member of the Commons Council Operations
Committee observes the resolved JuryReview (off-chain or via
`Query/JuryReview`) and submits the matching verdict back to x/service
via `MsgResolveReportByJury` or `MsgFinalizeControllerTransfer`. This
is the same pattern used by every other appeal-resolution flow in
this codebase (see [`MsgResolveGovActionAppeal`](x/rep/keeper/msg_server_resolve_gov_action_appeal.go)
for the canonical example).

x/service's resolver-side handlers enforce two integrity gates:

1. **Authority gate.** `commonsKeeper.IsCouncilAuthorized(resolver,
   "commons", "operations")` must return true; otherwise
   `ErrUnauthorizedCouncilResolver`.
2. **Verdict cross-check.** `repKeeper.GetJuryReview(jury_case_id)`
   must return a `JuryReview` whose `Verdict` is consistent with the
   resolver's submitted verdict per the table above; otherwise
   `ErrJuryVerdictMismatch`.

Together these two gates mean a single rogue Operations Committee
member cannot forge a verdict the jurors didn't actually reach. The
committee acts as a low-trust relayer, not the verdict source.

**Timeout handling.** If a JuryReview never reaches a tallied
verdict (jurors don't vote, jury deadline expires), x/service's own
EndBlocker queue 3 (§3.6) auto-resolves the ESCALATED report as
`AUTO_TIMEOUT` after `max_escalated_blocks`. The corresponding
JuryReview is left in whatever state x/rep's own deadline machinery
puts it (typically `INCONCLUSIVE` or `PENDING`); x/service does not
need to actively cancel it. The `report_contest_window_blocks` /
`max_escalated_blocks` params are tuned to be longer than x/rep's
typical jury cycle so the JuryReview should always reach a state
before x/service times out.

### 6.3. x/federation (migration complete)

`x/federation` previously embedded its own bridge-operator economic primitive (bond escrow, status state machine, unbonding queue, slashing). That has been migrated onto x/service. Current state:

1. **Two service types are seeded at x/service genesis**: `federation-bridge-activitypub` and `federation-bridge-atproto` (see §7 — these come from x/service's own `DefaultGenesis`, NOT from federation's genesis). Each is configured with `report_timeout_action = ESCALATE` so a silent or captured peer controller cannot stall a slash past one timeout window.
2. **Federation's `MsgRegisterBridge` is operator-signed** and orchestrates: (a) federation peer/policy checks, (b) `serviceKeeper.RegisterOperator` (first registration under `(address, service_type)`) or (c) `serviceKeeper.TopUpBond` (re-registration under an existing operator for a different peer — one operator address shares one bond across multiple peer bindings within the same protocol).
3. **Federation has no slash messages.** Misbehavior reports go through `MsgReportOperator` (community-filed) or `OpenSystemReport` (federation-filed on challenge-quorum-upheld, §3.7). The peer's controller (`peer.controller_group` if set, Operations Committee by default) resolves via `MsgResolveReport` within the per-`ServiceTypeConfig` cap; larger slashes escalate to jury via `MsgResolveReportByJury`.
4. **Federation subscribes to all four hooks** (§6.1). Hooks are wrapped in `defer recoverHookPanic` (fail-soft pattern) — a bug in federation must never roll back an x/service slash.
5. **Federation owns** the per-binding endpoint, content statistics, and `suspended` flag. **x/service owns** bond, status, unbonding, slashing history, controller, reputation accrual.

See `docs/x-federation-spec.md` §3.6 / §10.4 / §14.2 for the federation-side details.

**Adding a new federation protocol** (e.g., Nostr): governance enables a new `federation-bridge-<protocol>` service type via `MsgUpdateServiceTypeConfig`; federation's peer-type-to-service-type mapping is the only federation-side change. No x/service code change required (free-form `service_type` strings, §3.2).

### 6.4. x/bank, x/distribution

Bond escrow: SPARK held in the module account. Slashing: `SendCoinsFromModuleToAccount` to the community pool (via `x/distribution` standard `FundCommunityPool`).

### 6.5. x/session (operator key delegation)

Operators can delegate hot-wallet automation via `x/session`. The intent is that the registered operator address (the SPARK-bonded identity) stays cold, and routine on-chain operations are performed by a short-lived session key.

Delegable from the operator key:
- `MsgUpdateMetadata`
- `MsgClaimUnbondedBond`
- `MsgContestSlash` (time-sensitive — within the contest window)
- `MsgTopUpBond`

**Not delegable** (must be signed by the operator key directly — **both x/session AND x/authz blocked**):
- `MsgRegisterOperator` — initial bond commitment, requires cold signature
- `MsgUnbondOperator` — significant lifecycle decision, prevents a compromised session key from forcing exit

The session module's existing message-type allowlist (`max_allowed_msg_types` ceiling) is the enforcement mechanism for x/session — operators just create a session with the delegable subset above. For x/authz, x/service enforces the block at the msg-server level by checking `creator == tx_signer[0]` (no `MsgExec` indirection). This avoids depending on whether the chain admin happens to exclude these msgs from authz's `ForbiddenMessages`; the safety is enforced by the module itself.

### 6.6. x/rep (reputation feedback)

Operator performance accrues reputation in a dedicated x/rep tag `service-operator`. Three trigger points:

- **Successful unbond completion** with no slashing history: positive reputation grant computed from accrued bond-blocks (see anti-gaming rule below).
- **Tier-1 slash resolved (final, not contested)**: reputation deduction proportional to slash size, sent to `RepKeeper.DeductReputation`.
- **Tier-2 slash by jury verdict (Accept)**: larger reputation deduction; verdict `Reject` produces no reputation movement.

**Bond-block accrual (lazy, O(1) per event — NOT per-block).** The naive approach — incrementing `total_lifetime_bond_blocks += current_bond` for every ACTIVE operator every block — is an O(N) scan per block and infeasible at any scale. Instead, accrual is updated only at events that change `current_bond` or the ACTIVE status:

```
// Called by the keeper before any mutation to operator.bond, operator.status, or at query/claim time.
func settleBondBlocks(op *Operator, currentHeight int64) {
    if op.status == ACTIVE {
        elapsed := currentHeight - op.last_bond_block_update_at
        op.total_lifetime_bond_blocks += elapsed * op.bond
    }
    // UNDERFUNDED, UNBONDING, terminal: do not accrue (operator is not in good standing)
    op.last_bond_block_update_at = currentHeight
}
```

Settle points (any operation that changes `bond` OR changes whether the operator is in ACTIVE status):

- `MsgTopUpBond` — settle, then add to bond. If status was UNDERFUNDED and new bond ≥ min_bond, settle again at the ACTIVE transition (no-op since UNDERFUNDED doesn't accrue, but the field update keeps semantics consistent).
- `MsgResolveReport` (T1_SLASH) — settle, then subtract slash. If transition to UNDERFUNDED, the settle has already captured ACTIVE-period accrual; the new UNDERFUNDED period accrues nothing.
- `MsgResolveReportByJury` (ACCEPT/REDUCE) — same as T1_SLASH but for tier-2 slashes.
- `MsgContestSlash` — settle, then return escrow to bond (status may transition back to ACTIVE if it was UNDERFUNDED solely due to the contested slash).
- `MsgUnbondOperator` — settle (final ACTIVE-period accrual captured), then transition to UNBONDING which accrues nothing.
- `MsgClaimUnbondedBond` — settle one last time (will be a no-op since UNBONDING doesn't accrue, but keeps the timestamp current), then compute and apply the reputation grant.
- **EndBlocker queue 1** (UNDERFUNDED → forced UNBONDING) — settle (no-op for already-UNDERFUNDED but keeps timestamp current).
- **EndBlocker queue 3** (ESCALATED → AUTO_TIMEOUT with escrow return) — settle if escrow returns to bond (the bond change might re-cross the min_bond threshold and flip status).
- `OperatorReputationSnapshot` query — settle in-memory (do NOT write), return current values.

The single rule: any code path that mutates `operator.bond` or `operator.status` MUST call `settleBondBlocks(op, currentHeight)` first.

At successful unbond claim, settle one last time then compute the grant:

```
grant = total_lifetime_bond_blocks * reputation_grant_per_bond_block
```

**Anti-gaming cap.** Reputation accrual is computed against the **single largest active operator record** per address, not summed across records. If address X holds three operators with bond-blocks `(10000, 5000, 5000)`, the effective bond-block total for reputation purposes is `10000` only, not `20000`. This is enforced at unbond-claim time by the keeper:

```
effective_bond_blocks = max(bond_blocks_of_this_operator,
                            max(bond_blocks of any other ACTIVE operator with same address))
grant = effective_bond_blocks * reputation_grant_per_bond_block
```

Multiple registrations remain useful for service redundancy (each one is independently slashable, independently reportable), but they do not multiply reputation gain. Combined with `max_active_operators_per_address` (default 16), this bounds the reputation any single address can extract via the bond-block channel.

The `service-operator` tag is registered in genesis as a reserved tag (operators do not need to pay the tag-creation fee). Tag CRUD lives in x/rep per the existing tag-registry consolidation; x/service calls `RepKeeper.AddReputation(ctx, address, "service-operator", score)` / `DeductReputation(...)`.

Operators with high `service-operator` reputation are visible to councils evaluating candidates for new contracts — turns operator history into a discoverable signal rather than a council-private record.

---

### 6.7. Keeper API surface

The following methods are exposed on `service.Keeper` for consumption by other modules (notably x/federation). All read methods are pure; mutating methods emit the events listed in §12 and respect the state machine in §3.6.

```go
// Read API
GetOperator(ctx, addr sdk.AccAddress, serviceType string) (Operator, bool)              // live store only
GetArchivedOperators(ctx, addr sdk.AccAddress, serviceType string) []Operator           // SLASHED + RETIRED history; returned records share the live Operator proto type with bond=0 and retired_at != 0
HasSlashedRecord(ctx, addr sdk.AccAddress, serviceType string) (bool, error)            // for re-registration gate
GetServiceTypeConfig(ctx, serviceType string) (ServiceTypeConfig, bool)
IsActiveOperator(ctx, addr sdk.AccAddress, serviceType string) bool                     // status == ACTIVE
GetAvailableBond(ctx, addr sdk.AccAddress, serviceType string) sdk.Coin                 // current bond, excludes Tier1Escrow
ListOperatorsByController(ctx, controller sdk.AccAddress, pagination *Pagination) []Operator
ListOperatorsByServiceType(ctx, serviceType string, pagination *Pagination) []Operator
ListPendingReportsAgainst(ctx, addr sdk.AccAddress, serviceType string) []Report

// Mutating API (intended for inter-module use; signed-msg paths in §5 wrap these)
RegisterOperator(ctx, creator, serviceType, controller string, bond sdk.Coin, metadata []byte, source SlashSource) (Operator, error)
TopUpBond(ctx, operator sdk.AccAddress, serviceType string, additionalBond sdk.Coin) error    // keeper-level top-up; fires AfterOperatorReFunded if it crosses min_bond; allowed for ACTIVE/UNDERFUNDED, rejected for UNBONDING/terminal
SlashOperator(ctx, addr sdk.AccAddress, serviceType string, slashBps uint32, reason string, source SlashSource) (slashed sdk.Coin, err error)
OpenSystemReport(ctx, callerModuleAddr sdk.AccAddress, operator sdk.AccAddress, serviceType string, slashBps uint32, evidenceURI string, dedupeKey []byte) (reportID uint64, err error)  // §3.7
TerminateOperator(ctx, addr sdk.AccAddress, serviceType string, reason string) error  // direct SLASHED transition; jury-only via MsgResolveReportByJury

// Hook setter (called once during app wiring)
SetHooks(hooks ServiceHooks) *Keeper                                                    // see §6.1 for ServiceHooks
```

`SlashSource` is an enum (`TIER1`, `TIER2_JURY`, `MIGRATION`) so the keeper can apply the right invariants (tier-1 cap/cooldown/escrow for `TIER1`, deposit refund logic for `TIER2_JURY`, no checks for `MIGRATION` which is reserved for chain-upgrade reconciliation only — see §15). `RegisterOperator` also takes a `source` so consumer modules registering an operator on the user's behalf (federation's `MsgRegisterBridge`) use `ServiceSourceNormal` while chain-upgrade handlers pass `MIGRATION` to bypass `IsGroupAddress` checks on legacy state.

**Consumer module pattern (federation example):** x/federation receives a `ServiceKeeper` reference via depinject and an adapter (`app/service_adapters.go`). Federation's `MsgRegisterBridge` orchestrates `serviceKeeper.RegisterOperator` (first registration) OR `serviceKeeper.TopUpBond` (re-registration for an additional peer under an existing operator, Decision 1a of the migration plan). Federation's challenge-resolution code calls `serviceKeeper.OpenSystemReport` to file slash reports under module privilege. Federation never touches x/service state directly; the keeper API is the only consumer surface.

**Caller allowlist** (§3.7.1): `OpenSystemReport` and `RegisterOperator(source=...)` are gated by a sorted-slice allowlist of allowed module names. At launch the allowlist is `[]string{"federation"}`. Adding a new caller requires editing this constant in source — intentional privilege-gate friction.

---

## 7. Genesis

```
GenesisState {
  params: Params
  service_types: [ServiceTypeConfig]      // initial registry
  operators: [Operator]                   // live records (ACTIVE | UNDERFUNDED | UNBONDING)
  archived_operators: [Operator]          // SLASHED + RETIRED records (for audit retention)
  reports: [Report]                       // open and recently-resolved reports
  tier1_escrow: [Tier1EscrowEntry]        // in-flight tier-1 slashes awaiting contest window
  controller_transfer_cases: [ControllerTransferCase]  // open controller-transfer cases (with escrowed opener deposits)
  reporter_rate_limits: [ReporterRateLimit]
  refile_cooldowns: [RefileCooldown]
  tier1_last_slash: [Tier1LastSlash]      // per-operator tier-1 history
  next_report_id: uint64                  // counter restore
  next_escrow_id: uint64                  // counter restore
}
```

**Default genesis seeds the two federation-bridge service types.** `DefaultGenesis()` in `x/service/types/genesis.go` populates `service_types` with:

- `federation-bridge-activitypub` — `enabled = true`, `min_bond = 1000 SPARK`, `report_timeout_action = ESCALATE`, `challenge_default_slash_bps = 100` (1%), all other knobs at module-defaults.
- `federation-bridge-atproto` — same settings.

Both are seeded by **x/service**, NOT by x/federation. The rationale (Decision 1 / Phase 2 init-order constraint of the migration plan): federation's genesis loads `BridgeBinding` records that reference `service.Operator`s, so the operator's `ServiceTypeConfig` must already exist when x/federation's `InitGenesis` runs. Init order: x/gov → x/bank → x/commons → **x/service** (seeds these configs + any genesis operators) → x/federation (consumes via keeper API).

**Other service types still require governance enablement.** Adding `akash-funding`, `storage-pinning`, etc., requires a `MsgUpdateServiceTypeConfig` proposal post-launch.

**Pre-enabled operators via genesis import** (e.g., a chain launched with pre-bonded bridge operators) are supported: include `Operator` records under `operators` with `service_type` referencing one of the seeded configs. `IsGroupAddress(controller)` is deferred to first-message processing because x/commons may not be fully initialized at the moment `service.InitGenesis` runs (depends on init order).

**Chain upgrade migration** is handled in §15.

**Genesis validation** (`GenesisState.Validate`):

- `params.Validate()` (§4.2 rules).
- Every `service_type` referenced by any record MUST exist in `service_types`.
- Every `operators[].controller` MUST be a valid bech32 address AND `controller != operators[].address` (self-controller is rejected at genesis just as at runtime).
- Note: the `IsGroupAddress` check on controllers is deferred to first message touching the operator — x/commons may not be fully initialized at the moment `service.InitGenesis` runs (depends on init-order), so a strict genesis-time check would create a brittle ordering dependency. The check IS enforced for any new `MsgRegisterOperator` post-genesis.
- `next_report_id > max(reports[].id)` (so freshly-generated IDs don't collide).
- `next_escrow_id > max(tier1_escrow[].escrow_id)`.
- For each live operator, `bond.denom == "uspark"` and `bond.amount > 0` (live records must hold real bond).
- For each archived operator: `bond.amount == 0`, `status ∈ {SLASHED, RETIRED}`, `retired_at > 0` (archived records must have a valid terminal status and a non-zero archive height).
- Live `operators[]` MUST NOT contain duplicate `(address, service_type)` pairs.
- `archived_operators[]` MUST NOT collide with live `operators[]` on `(address, service_type)` (the live record is authoritative until terminal transition).
- Every `tier1_escrow[].report_id` MUST reference a report in `reports[]` with matching `operator_address` and `service_type`; orphan escrow rows fail genesis.
- Every `tier1_escrow[].operator_address + service_type` MUST resolve to either a live `operators[]` entry or an archived one (a slash escrow without any operator record is a state corruption).
- Every report with `status == ESCALATED` MUST have `escalated_at > 0` and `jury_case_id != 0`.
- Every `tier1_last_slash[].operator_address + service_type` SHOULD resolve to a live or archived operator; orphan entries are a soft validation warning (harmless but suggests a prior bug — log on InitGenesis).
- **Module account balance invariant:** `bank.GetBalance(serviceModuleAddr, uspark).Amount == sum(operators[].bond) + sum(reports where status ∈ {PENDING, ESCALATED}[].deposit) + sum(controller_transfer_cases[].deposit) + sum(tier1_escrow[].amount)`. Mismatch fails chain start.

---

## 8. Queries

Standard gRPC query service exposed on `sparkdream.service.v1.Query`:

| RPC                          | Request                                                   | Response                                | Notes                                              |
|------------------------------|-----------------------------------------------------------|-----------------------------------------|----------------------------------------------------|
| `Params`                     | `{}`                                                      | `{ params }`                            | Module params snapshot                             |
| `ServiceType`                | `{ service_type }`                                        | `{ config }`                            | One config row                                     |
| `ServiceTypes`               | `{ pagination, enabled_only }`                            | `{ configs, pagination }`               | Paginated registry list                            |
| `Operator`                   | `{ address, service_type }`                               | `{ operator }`                          | Single record                                      |
| `Operators`                  | `{ pagination }`                                          | `{ operators, pagination }`             | Global paginated list                              |
| `OperatorsByController`      | `{ controller, pagination, status_filter }`               | `{ operators, pagination }`             | Uses `OperatorsByController` index                 |
| `OperatorsByServiceType`     | `{ service_type, pagination, status_filter }`             | `{ operators, pagination }`             | Uses `OperatorsByServiceType` index                |
| `Report`                     | `{ report_id }`                                           | `{ report }`                            | Single report                                      |
| `ReportsByOperator`          | `{ operator_address, service_type, pagination, status }`  | `{ reports, pagination }`               | All reports against an operator                    |
| `OperatorReputationSnapshot` | `{ address }`                                             | `{ bond_blocks, effective_bond_blocks }`| Convenience: bond-block accrual visibility (§6.6)  |

REST endpoints follow Cosmos SDK conventions (`/sparkdream/service/v1/...`).

**Pagination.** All paginated RPCs accept the standard `cosmos.base.query.v1beta1.PageRequest`. If `pagination.limit` is omitted, the keeper applies `default_pagination_limit` (default 100). Requests with `pagination.limit > max_pagination_limit` (default 1000) are rejected with `ErrPaginationLimitExceeded` — this bounds query gas and prevents an OOM via an unbounded scan.

---

## 9. Errors

Standard SDK `Errors` registered under codespace `service`:

| Code  | Name                              | Trigger                                                                  |
|-------|-----------------------------------|--------------------------------------------------------------------------|
| 1     | `ErrServiceTypeNotFound`          | Referenced `service_type` does not exist                                 |
| 2     | `ErrServiceTypeDisabled`          | `MsgRegisterOperator` against a service type with `enabled = false`      |
| 3     | `ErrInvalidServiceType`           | Service type string fails `^[a-z0-9-]{1,64}$`                            |
| 4     | `ErrOperatorNotFound`             | `(address, service_type)` has no record                                  |
| 5     | `ErrOperatorAlreadyExists`        | Re-registration attempt of an existing `(address, service_type)`         |
| 6     | `ErrOperatorSlashed`              | Operation rejected because operator is `SLASHED`                         |
| 7     | `ErrOperatorNotActive`            | Operation requires `ACTIVE` status (e.g., contest after window)          |
| 8     | `ErrOperatorUnbonding`            | Operation invalid during `UNBONDING` (e.g., `MsgTopUpBond`)              |
| 9     | `ErrInsufficientBond`             | `bond < service_type.min_bond` at registration or after raised minimum   |
| 10    | `ErrBondDenomMismatch`            | `bond.denom != "uspark"`                                                 |
| 11    | `ErrUnbondingPeriodNotElapsed`    | `MsgClaimUnbondedBond` before `unbond_complete_at`                       |
| 12    | `ErrOpenReports`                  | `MsgClaimUnbondedBond` while reports are PENDING/ESCALATED               |
| 13    | `ErrReportNotFound`               | `report_id` does not exist                                               |
| 14    | `ErrReportAlreadyResolved`        | `MsgResolveReport` against non-PENDING report                            |
| 15    | `ErrReporterTrustLevelTooLow`     | Reporter below `min_reporter_trust_level`                                |
| 16    | `ErrInsufficientReportDeposit`    | Reporter balance < `report_deposit`                                      |
| 17    | `ErrSlashCapExceeded`             | `slash_bps > unilateral_slash_cap_bps` on tier-1 resolution              |
| 18    | `ErrTier1AggregateCapExceeded`    | Slash would exceed `tier1_aggregate_cap_bps` of window-start bond        |
| 19    | `ErrTier1CooldownActive`          | Tier-1 slash within `tier1_cooldown_blocks` of previous slash            |
| 20    | `ErrContestWindowExpired`         | `MsgContestSlash` after `report_contest_window_blocks`                   |
| 21    | `ErrUnauthorizedController`       | `MsgResolveReport` from address ≠ operator's controller                  |
| 22    | `ErrUnauthorizedCouncilResolver`  | `MsgResolveReportByJury` / `MsgFinalizeControllerTransfer` signer is not a Commons Ops Committee member |
| 23    | `ErrUnauthorizedGovAuthority`     | `MsgUpdateServiceTypeConfig` from non-x/gov signer                       |
| 24    | `ErrInvalidMetadataSize`          | `len(metadata) > max_metadata_bytes`                                     |
| 25    | `ErrInvalidReasonSize`            | `len(reason) > max_reason_bytes`                                         |
| 26    | `ErrTooManyOperatorsForAddress`   | Address already at `max_active_operators_per_address`                    |
| 27    | `ErrRefileCooldownActive`         | Same controller re-filing within `report_refile_cooldown_blocks`         |
| 28    | `ErrInvalidVerdict`               | Verdict enum unknown or invalid for context                              |
| 29    | `ErrInvalidParams`                | `Params.Validate` failed                                                 |
| 30    | `ErrInvalidServiceTypeConfig`     | `ServiceTypeConfig.Validate` failed                                      |
| 31    | `ErrSelfController`               | `MsgRegisterOperator` with `controller == creator`                       |
| 32    | `ErrControllerNotGroup`           | `controller` is not a registered x/commons Group address                  |
| 33    | `ErrOperatorPreviouslySlashed`    | `(address, service_type)` has SLASHED record in `ArchivedOperators`      |
| 34    | `ErrReporterIsControllerMember`   | Reporter is a member of operator's controller Group                      |
| 35    | `ErrReporterRateLimitExceeded`    | Reporter exceeded `max_reports_per_reporter_per_operator_per_window`     |
| 36    | `ErrControllerTransferCaseAlreadyOpen` | A controller-transfer jury case is already open for this operator   |
| 37    | `ErrEscrowStillActive`            | `MsgClaimUnbondedBond` while tier-1 escrow entries with `release_at > current_height` remain (still within contest window; expired-but-unswept entries are eagerly processed by the claim handler — §3.5) |
| 38    | `ErrPaginationLimitExceeded`      | Query `pagination.limit > max_pagination_limit`                          |
| 39    | `ErrControllerNoLongerEligible`   | `MsgFinalizeControllerTransfer` apply-time re-check failed (Group dissolved or empty) |
| 40    | `ErrControllerTransferCaseNotFound` | `MsgFinalizeControllerTransfer` references unknown `jury_case_id`      |
| 41    | `ErrJuryVerdictMismatch`            | Resolver's submitted verdict doesn't match the x/rep `JuryReview` (cross-check fails; §6.2) |
| 42    | `ErrUnauthorizedSystemCaller`       | `OpenSystemReport` caller is not in the allowlist or the supplied `callerModuleAddr` does not match any allowlisted module account (§3.7.1) |
| 43    | `ErrSystemReportRateLimited`        | `OpenSystemReport` per-caller sliding-window cap exceeded; emit `system_report_rate_limited` event and reject without state change (§3.7.3) |
| 44    | `ErrInvalidDedupeKey`               | `OpenSystemReport` called with empty `dedupe_key`; idempotency requires a non-empty key (§3.7.2) |

---

## 10. CLI

Transaction subcommands under `sparkdreamd tx service ...`:

| Command                                                                                             | Maps to                          |
|-----------------------------------------------------------------------------------------------------|----------------------------------|
| `register-operator <service-type> <controller> <bond> --metadata <hex-or-file>`                     | `MsgRegisterOperator`            |
| `update-metadata <service-type> --metadata <hex-or-file>`                                           | `MsgUpdateMetadata`              |
| `unbond <service-type>`                                                                             | `MsgUnbondOperator`              |
| `claim-bond <service-type>`                                                                         | `MsgClaimUnbondedBond`           |
| `top-up <service-type> <amount>`                                                                    | `MsgTopUpBond`                   |
| `report <operator> <service-type> --reason <text>`                                                  | `MsgReportOperator`              |
| `resolve-report <report-id> <verdict> [--slash-bps N]`                                              | `MsgResolveReport`               |
| `contest-slash <service-type> <report-id>`                                                          | `MsgContestSlash`                |
| `open-controller-transfer-case <operator> <service-type> <proposed-controller> --reason <text>`     | `MsgOpenControllerTransferCase`  |

Query subcommands under `sparkdreamd query service ...`:

| Command                                                       | Maps to                          |
|---------------------------------------------------------------|----------------------------------|
| `params`                                                      | `Query/Params`                   |
| `service-type <service-type>`                                 | `Query/ServiceType`              |
| `service-types [--enabled-only]`                              | `Query/ServiceTypes`             |
| `operator <address> <service-type>`                           | `Query/Operator`                 |
| `operators [--by-controller A] [--by-service-type T]`         | dispatches to relevant RPC       |
| `report <report-id>`                                          | `Query/Report`                   |
| `reports <operator> <service-type> [--status S]`              | `Query/ReportsByOperator`        |
| `reputation-snapshot <address>`                               | `Query/OperatorReputationSnapshot` |

Gov-authority msgs (`MsgUpdateServiceTypeConfig`) are submitted via `sparkdreamd tx gov submit-proposal` with the message wrapped in a `MsgUpdateServiceTypeConfig` proposal content; no dedicated CLI subcommand.

Jury-authority msgs are not exposed as user-facing CLI — they are emitted by x/rep internally on jury verdict resolution.

---

## 11. Testing Strategy

| Layer                | Coverage                                                                                                                              |
|----------------------|---------------------------------------------------------------------------------------------------------------------------------------|
| Unit (keeper)        | All state transitions in §3.6, all error codes in §9, all slash math edge cases (zero bond, fractional bps, bond < slash_amount), all index consistency invariants |
| Unit (msg server)    | Auth path for each message in §5, signer-vs-field mismatch rejection, validation-rule rejection                                       |
| Simulation           | Fuzzed register/unbond/slash flows; invariants: module account balance == sum(operator bonds), no operator can exit with bond < 0     |
| Integration (Go)     | x/service ↔ x/rep jury verdict roundtrip; x/service ↔ x/bank module account behavior; x/service ↔ x/distribution community pool deposit |
| E2E (shell)          | Full register-fund-slash-contest-jury-verdict cycle via CLI against running chain; gov-proposal flow for `MsgUpdateServiceTypeConfig`; tier-1 aggregate cap + cooldown rejection paths; reporter deposit refund/forfeit paths; controller-transfer flow (open case → jury ACCEPT → `MsgFinalizeControllerTransfer` applied → controller updated, deposit refunded; also test apply-time `IsGroupAddress` re-check failure path); operator re-registration after RETIRED (allowed) vs SLASHED (blocked) |
| Regression           | Sibling of [x/commons/types/amino_name_test.go](x/commons/types/amino_name_test.go) covering every signer Msg in §5                   |

---

## 12. Events

- `service.operator_registered { address, service_type, controller, bond }`
- `service.operator_topped_up { address, service_type, additional_bond, new_bond }`
- `service.operator_underfunded { address, service_type, current_bond, min_bond, grace_until }`
- `service.operator_unbond_started { address, service_type, complete_at, source }` (source ∈ VOLUNTARY | FORCED_UNDERFUNDED)
- `service.operator_unbond_completed { address, service_type, returned_bond }`
- `service.operator_archived { address, service_type, final_status, retired_at }` (final_status ∈ RETIRED | SLASHED)
- `service.operator_slashed { address, service_type, slash_amount, new_bond, tier, report_id }`
- `service.operator_dissolved { address, service_type, confiscated_bond, report_id }` (consumed by x/commons OperatorDissolutionHook — §6.1)
- `service.tier1_escrow_locked { escrow_id, report_id, address, service_type, amount, release_at }`
- `service.tier1_escrow_released { escrow_id, report_id, address, service_type, amount, destination }` (destination ∈ COMMUNITY_POOL | BOND_RESTORED)
- `service.report_filed { report_id, reporter, operator, service_type, deposit }`
- `service.report_resolved_t1 { report_id, verdict, slash_amount }`
- `service.report_escalated { report_id, jury_case_id, proposed_slash_bps }`
- `service.report_resolved_t2 { report_id, verdict, slash_amount, dissolve }`
- `service.report_auto_dismissed { report_id, deposit_refunded }`
- `service.report_auto_timeout { report_id, deposit_refunded, escrow_returned_to_bond }`
- `service.report_closed_dissolved { report_id, deposit_refunded }` (emitted when an open report is closed because the operator was SLASHED — §3.4.9)
- `service.report_contested { report_id, restored_bond }`
- `service.controller_transfer_case_opened { operator, service_type, opener, proposed_new_controller, jury_case_id }`
- `service.controller_transfer_case_finalized { jury_case_id, verdict, deposit_destination }` (deposit_destination ∈ OPENER_REFUNDED | COMMUNITY_POOL)
- `service.metadata_updated { address, service_type }`
- `service.controller_transferred { address, service_type, new_controller, jury_case_id }` (emitted only on ACCEPT finalize)
- `service.service_type_updated { service_type, enabled, changed_fields }`
- `service.system_report_opened { report_id, caller_module, operator, service_type, evidence_uri, dedupe_key, idempotent }` (§3.7) — `idempotent=true` indicates the call returned an existing `report_id` for a repeated `dedupe_key`; `idempotent=false` indicates a new report was allocated.
- `service.system_report_rate_limited { caller_module, operator, service_type }` (§3.7) — emitted on rejection when the per-caller sliding-window cap is exceeded; no state change accompanies this event.

The `operator_underfunded` and an `operator_refunded` companion (emitted from the `TopUpBond` recovery path) carry `{ address, service_type, current_bond, min_bond }` so off-chain consumers can react to the hooks without polling status.

---

## 13. Invariants

Registered via `RegisterInvariants` (consulted by `simd q crisis invariant <module>/<name>` and by simulation):

- **`bond-pool-accounting`** — `bank.GetBalance(serviceModule, uspark).Amount == sum(operators[].bond) + sum(reports where status ∈ {PENDING, ESCALATED}[].deposit) + sum(controller_transfer_cases[].deposit) + sum(tier1_escrow[].amount)`. The module account holds four categorically distinct pools (see §3.4.7) and their sum must equal the bank balance.
- **`live-archive-disjoint`** — for every `(address, service_type)` pair, at most one record exists in `Operators`; any number may exist in `ArchivedOperators` but none with `retired_at` more recent than the live record's `registered_at`.
- **`controller-by-controller-index-consistency`** — every entry in `OperatorsByController` resolves to a record in `Operators` with matching controller field; no orphan index entries.
- **`service-type-index-consistency`** — every entry in `OperatorsByServiceType` resolves to a record in `Operators` with matching service_type field.
- **`underfunded-queue-consistency`** — every entry in `UnderfundedQueue` resolves to a record in `Operators` with status `UNDERFUNDED`; conversely every `UNDERFUNDED` record has a queue entry.
- **`pending-report-queue-consistency`** — same as above for `PendingReportsQueue` ↔ PENDING reports.
- **`tier1-escrow-queue-consistency`** — every entry in `Tier1EscrowReleaseQueue` resolves to a `Tier1Escrow` row.
- **`report-state-machine-sanity`** — for every report, `status` is one of the documented values (`PENDING | RESOLVED_T1 | ESCALATED | RESOLVED_T2 | AUTO_DISMISSED | AUTO_TIMEOUT | CLOSED_OPERATOR_DISSOLVED`); if `status == ESCALATED`, `jury_case_id != 0` AND `escalated_at != 0`; if `status == RESOLVED_T2`, `verdict` and `slash_amount` are set; if `status == CLOSED_OPERATOR_DISSOLVED`, the referenced operator MUST exist in `ArchivedOperators` with `status == SLASHED`.
- **`reputation-cap-soundness`** — at any given height, `effective_bond_blocks` per address (§6.6) equals `max(bond_blocks across all live ACTIVE operators of that address)`.

Invariants are checked in simulation runs at every step and exposed via the crisis module for runtime auditing.

---

## 14. Telemetry

Standard `cosmossdk.io/telemetry` metrics emitted from the keeper:

| Metric                                              | Type      | Labels                              |
|-----------------------------------------------------|-----------|-------------------------------------|
| `service.operator.registered`                       | counter   | `service_type`                      |
| `service.operator.unbond_started`                   | counter   | `service_type, source`              |
| `service.operator.dissolved`                        | counter   | `service_type, tier`                |
| `service.operator.live_count`                       | gauge     | `service_type, status`              |
| `service.bond.locked`                               | gauge     | (uspark, summed across operators)   |
| `service.report.filed`                              | counter   | `service_type`                      |
| `service.report.resolved`                           | counter   | `service_type, tier, verdict`       |
| `service.tier1_escrow.in_flight`                    | gauge     | (uspark, summed)                    |
| `service.slash.amount`                              | counter   | `service_type, tier`                |
| `service.controller_transfer_case.opened`           | counter   | `service_type`                      |
| `service.controller_transfer_case.finalized`        | counter   | `service_type, verdict`             |
| `service.endblocker.sweep_duration_ms`              | histogram | `queue`                             |
| `service.endblocker.swept_records`                  | counter   | `queue`                             |

EndBlocker durations should be tracked per-queue so operators can detect when a queue is consistently saturating `endblocker_sweep_limit` and governance can raise the limit.

---

## 15. Upgrade Migration

**Pre-mainnet posture.** The federation→service migration landed pre-mainnet, so no live state migration was required: federation's previous `BridgeOperator` proto, federation-local slash/unbond messages, and federation-local bond escrow were simply deleted in source and replaced by the x/service surface described in this spec. New federation `BridgeBinding` records reference `service.Operator` records via `(address, service_type)`; chain restart from genesis (or testparams reset) is the deployment path. No upgrade-handler reconciliation logic exists in `x/service` today because none was needed.

**If x/service is added to an already-deployed chain via an upgrade** (hypothetical future scenario, NOT the path that produced the current implementation), the upgrade handler would need to:

1. **Register the module account** with the auth keeper (`authtypes.NewModuleAccount(...)`). No special permissions (`Minter`, `Burner`, `Staking`) are required — slashes and forfeitures move SPARK to the community pool via `distribution.FundCommunityPool`, which is a `SendCoinsFromModuleToModule` to the distribution module's pool account, not a burn.
2. **Set initial `Params`** via `Keeper.SetParams` with the validated default block.
3. **Seed `ServiceTypeConfig`s** for any consumer module that already has live bonded operators. For federation, this means `federation-bridge-activitypub` and `federation-bridge-atproto` (matching `DefaultGenesis` — §7).
4. **Migrate existing bonded-operator state from the consumer module** (if applicable):
   - For each pre-migration record: compute the bond amount, move SPARK from the source module account to the service module account via `bank.SendCoinsFromModuleToModule`, then call `Keeper.RegisterOperator(ctx, ..., source = MIGRATION)`. The `MIGRATION` source bypasses `IsGroupAddress(controller)`, `min_bond`, and `max_active_operators_per_address` because we're recording existing state, not vetting a fresh registration.
   - Delete the consumer-side bonded-operator records and zero the consumer module account's bond balance.
5. **Initialize `NextReportID = 1` and `NextEscrowID = 1`** (no historic reports are migrated; pre-existing slash records are left as consumer-side events).
6. **Verify the module account balance invariant** (§13 `bond-pool-accounting`) before completing the upgrade.

A v1 → v2 schema migration of x/service itself (after launch) follows the standard SDK consensus-version migration pattern: bump `ConsensusVersion()`, register a handler in `RegisterMigrations`, transform existing state in-place. No special considerations beyond standard SDK upgrade hygiene.

---

## 16. Design Notes

The following choices were resolved during the spec phase and are noted here in case future-readers wonder about alternatives:

- **Slash distribution.** Always to community pool. The alternative — sending a fraction to the controller's treasury — was rejected because it creates a controller incentive to slash even when slashing isn't warranted. Keeping the community pool as the sole destination ensures the controller has no direct financial gain from slashing their own operators.
- **Multi-operator redundancy.** Each operator record is single-service, single-address. Redundancy is achieved by creating multiple registrations (different operator addresses, same `metadata`), not by allowing one record to be co-owned. Keeps the record model simple — no shared bonds, no per-operator share tracking, no joint-and-several slashing semantics to design.
- **Controller transfer requires jury.** Considered allowing direct controller-to-controller handoff via a `MsgTransferControllerDirect`. Rejected because it would allow an outgoing council to assign their operators to an arbitrary successor without member consent, including potentially to a captured/hostile council. Jury-mediation forces external review.
- **Tier-1 drainage acceptance envelope.** The aggregate cap + cooldown limit drainage *speed* (~15% per 90-day window, ~60%/year worst case under sustained controller-side hostility). They do NOT limit total damage over multi-year horizons. The acceptable assumption is that operator-initiated contests and the x/rep jury are the real safety floor — sustained captured-controller scenarios should trigger `MsgOpenControllerTransferCase` long before bond exhaustion. If this proves inadequate in practice, governance can tighten `tier1_aggregate_cap_bps` per service type without code changes.
- **RETIRED does not block re-registration; SLASHED does.** Voluntary exit should not be punitive — an operator might unbond to rotate keys, take a sabbatical, or temporarily reduce exposure. Re-entry under the same `(address, service_type)` is allowed. SLASHED is permanent because it represents proven misconduct.
- **No reporter bounty even on successful slash.** Reporters get their deposit back, not a fraction of slashed bond. Bounties create perverse incentives toward speculative reports against marginal operators; the deposit-refund-or-forfeit model gives reporters skin in the game without making reporting a revenue activity.
- **Launch-period trust-level may need to be lowered.** Default `min_reporter_trust_level = TRUST_LEVEL_ESTABLISHED` is calibrated for steady-state — but at chain start most members are at `PROVISIONAL` (only the founder and a handful of others reach ESTABLISHED). If x/federation bridge operators or early Akash-funding operators need accountability from day one, genesis params SHOULD set `min_reporter_trust_level = TRUST_LEVEL_PROVISIONAL` for the first season, then governance raises it to ESTABLISHED once the active population can sustain it. The tradeoff is more potential spam early (mitigated by `report_deposit`) vs. operators being effectively unaccountable to the broader membership during the bootstrap window. Document the chosen launch value in `deploy/config/network/<network>/config.yml`.

---

## 17. File References

- [docs/x-federation-spec.md](docs/x-federation-spec.md) — bridge-operator primitive that this generalizes
- [docs/x-rep-spec.md](docs/x-rep-spec.md) — jury system used for tier-2 slashing; DREAM-bonded roles (`BondedRole`) that complement (don't compete with) `x/service`
- [docs/x-commons-spec.md](docs/x-commons-spec.md) — `MsgScheduleRecurringSpend` for operator compensation; Group address registry for controller validation
- [CLAUDE.md](CLAUDE.md) — amino-name requirement for signer Msgs; immutable-parameter pattern reference
- [x/commons/types/amino_name_test.go](x/commons/types/amino_name_test.go) — pattern for the per-module amino-name regression guard called out in §5
